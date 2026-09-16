package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/modigo/runner/auth"
	"github.com/modigo/runner/config"
	"github.com/modigo/runner/executor"
	"github.com/modigo/runner/protocol"
)

// newUpgrader creates a WebSocket upgrader that validates the Origin header
// against the configured allowed origins. In dev mode (all origins allowed),
// it permits any origin.
func newUpgrader(allowedOrigins []string) websocket.Upgrader {
	allowAll := false
	for _, o := range allowedOrigins {
		if o == "*" {
			allowAll = true
			break
		}
	}

	return websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		CheckOrigin: func(r *http.Request) bool {
			if allowAll {
				return true
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // non-browser clients
			}
			for _, allowed := range allowedOrigins {
				if origin == allowed {
					return true
				}
			}
			log.Printf("[ws] rejected origin: %s (allowed: %v)", origin, allowedOrigins)
			return false
		},
	}
}

// activeSessions tracks the number of active WebSocket sessions.
var (
	activeSessions   int64
	activeSessionsMu sync.Mutex

	// userContainers tracks active container count per user for per-user limits.
	userContainers   = make(map[string]int)
	userContainersMu sync.Mutex
)

// MaxContainersPerUser is the maximum concurrent containers a single user can have.
const MaxContainersPerUser = 3

// incUserContainers increments the container count for a user. Returns false if limit exceeded.
func incUserContainers(userID string) bool {
	userContainersMu.Lock()
	defer userContainersMu.Unlock()
	if userContainers[userID] >= MaxContainersPerUser {
		return false
	}
	userContainers[userID]++
	return true
}

// decUserContainers decrements the container count for a user.
func decUserContainers(userID string) {
	userContainersMu.Lock()
	defer userContainersMu.Unlock()
	userContainers[userID]--
	if userContainers[userID] <= 0 {
		delete(userContainers, userID)
	}
}

// shutdownOnce ensures the shutdown drain runs only once.
var shutdownOnce sync.Once

// shutdownCh is closed when the server is shutting down.
var shutdownCh = make(chan struct{})

// GetActiveSessions returns the current number of active WebSocket sessions.
func GetActiveSessions() int64 {
	activeSessionsMu.Lock()
	defer activeSessionsMu.Unlock()
	return activeSessions
}

// sessionState holds the mutable state for a single WebSocket session.
type sessionState struct {
	mu         sync.Mutex
	session    *executor.InteractiveSession
	generation uint64               // incremented on each new "run" — prevents stale goroutines from killing new sessions
	tempDir    string               // code directory to clean up
	lab        *executor.LabSession // active lab session (nil if no lab)
	userID     string               // authenticated user ID for per-user limits
	timedOut   bool                 // true if the last run was killed by timeout
}

// WebSocketHandler handles WS connections for interactive terminal sessions.
// Auth is already handled by the middleware — this just upgrades and runs.
func WebSocketHandler(docker *executor.DockerClient, cfg *config.Config, sem *executor.Semaphore, labMgr *executor.LabManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Panic isolation: a panic in any per-connection path (malformed
		// message, race in pool/container code, etc.) used to unwind past the
		// goroutine root and kill the ENTIRE process — disconnecting every
		// connected user at once. Contain it to this connection instead.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[ws] PANIC in websocket session (recovered, connection closed): %v\n%s", rec, debug.Stack())
			}
		}()

		// Reject connections during shutdown
		select {
		case <-shutdownCh:
			http.Error(w, `{"error":"server shutting down"}`, http.StatusServiceUnavailable)
			return
		default:
		}

		// Auth already passed via middleware. Log who connected.
		userID := auth.GetUserID(r)
		plan := auth.GetPlan(r)
		log.Printf("[ws] connection from user=%s plan=%s", userID, plan)

		// Echo back Sec-WebSocket-Protocol so the browser accepts the handshake.
		var responseHeader http.Header
		if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			responseHeader = http.Header{"Sec-WebSocket-Protocol": {proto}}
		}

		// Upgrade to WebSocket with origin validation
		upgrader := newUpgrader(cfg.AllowedOrigins)
		conn, err := upgrader.Upgrade(w, r, responseHeader)
		if err != nil {
			log.Printf("[ws] upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		// Track active sessions
		activeSessionsMu.Lock()
		activeSessions++
		sessions := activeSessions
		activeSessionsMu.Unlock()
		log.Printf("[ws] session opened (active: %d)", sessions)

		defer func() {
			activeSessionsMu.Lock()
			activeSessions--
			sessions := activeSessions
			activeSessionsMu.Unlock()
			log.Printf("[ws] session closed (active: %d)", sessions)
		}()

		// Main message loop
		handleWebSocketSession(conn, docker, cfg, sem, labMgr, userID)
	}
}

// handleWebSocketSession processes messages for a single WebSocket connection.
func handleWebSocketSession(conn *websocket.Conn, docker *executor.DockerClient, cfg *config.Config, sem *executor.Semaphore, labMgr *executor.LabManager, userID string) {
	st := &sessionState{userID: userID}

	// Cleanup function — called when the WebSocket closes
	defer func() {
		st.mu.Lock()
		defer st.mu.Unlock()

		// Clean up student container
		if st.session != nil {
			st.session.Cancel()
			if st.session.Conn != nil {
				st.session.Conn.Close()
			}
			docker.Remove(context.Background(), st.session.ContainerID)
			st.session = nil
		}

		// Clean up temp directory
		if st.tempDir != "" {
			executor.RemoveTempDir(st.tempDir)
		}

		// Clean up lab session (target container + network)
		if st.lab != nil && labMgr != nil {
			log.Printf("[ws] cleaning up lab session %s", st.lab.SessionID)
			labMgr.StopLab(context.Background(), st.lab)
			st.lab = nil
		}
	}()

	// Read messages from client
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				log.Printf("[ws] read error: %v", err)
			}
			return
		}

		var msg protocol.ClientMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			sendError(conn, "Invalid message format")
			continue
		}

		switch msg.Type {
		case "run":
			handleRunMessage(conn, docker, cfg, st, &msg, sem, labMgr)

		case "input":
			handleInputMessage(st, msg)

		case "resize":
			handleResizeMessage(docker, st, msg)

		case "lab_start":
			handleLabStart(conn, docker, cfg, st, &msg, labMgr)

		case "lab_stop":
			handleLabStop(conn, st, labMgr)

		case "shell":
			handleShellMessage(conn, docker, cfg, st, &msg, sem)

		case "write":
			handleWriteMessage(conn, docker, st, &msg)

		case "read_files":
			handleReadFilesMessage(conn, docker, st)

		case "ping":
			// Client keepalive. Intermediaries (Cloudflare) drop idle WebSocket
			// paths after ~100s; a lightweight reply keeps the path alive.
			sendMessage(conn, protocol.ServerMessage{Type: "pong"})

		default:
			sendError(conn, "Unknown message type: "+msg.Type)
		}
	}
}

// handleLabStart creates an isolated Docker lab with a target container.
// The student's next "run" message will create their code container on the lab network.
func handleLabStart(conn *websocket.Conn, docker *executor.DockerClient, cfg *config.Config, st *sessionState, msg *protocol.ClientMessage, labMgr *executor.LabManager) {
	if labMgr == nil {
		sendError(conn, "Lab mode not available")
		return
	}

	// Clean up any existing lab
	st.mu.Lock()
	if st.lab != nil {
		labMgr.StopLab(context.Background(), st.lab)
		st.lab = nil
	}
	st.mu.Unlock()

	if msg.Lab == nil {
		sendError(conn, "lab config is required for lab_start")
		return
	}

	// Parse timeout
	labTimeout := 5 * time.Minute
	if msg.Lab.Timeout != "" {
		if d, err := time.ParseDuration(msg.Lab.Timeout); err == nil {
			labTimeout = d
		}
	}

	// Cap lab timeout at 30 minutes for safety
	if labTimeout > 30*time.Minute {
		labTimeout = 30 * time.Minute
	}

	// Generate session ID
	sessionID := fmt.Sprintf("lab-%d-%d", time.Now().UnixNano(), st.generation)

	// Start the lab
	lab, err := labMgr.StartLab(context.Background(), sessionID, executor.LabTarget{
		Type:  msg.Lab.Type,
		Image: msg.Lab.Image,
		Port:  msg.Lab.Port,
		Env:   msg.Lab.Env,
	}, labTimeout)

	if err != nil {
		log.Printf("[ws] lab start failed: %v", err)
		sendError(conn, "Failed to start lab: "+err.Error())
		return
	}

	// Store lab session
	st.mu.Lock()
	st.lab = lab
	st.mu.Unlock()

	// Determine target URL
	targetPort := labMgr.TargetPort(msg.Lab.Type, msg.Lab.Port)
	targetURL := fmt.Sprintf("http://target:%d", targetPort)

	// Send lab info to client
	sendMessage(conn, protocol.ServerMessage{
		Type: "lab_started",
		LabInfo: &protocol.LabInfo{
			TargetURL:  targetURL,
			TargetType: msg.Lab.Type,
			ExpiresAt:  lab.ExpiresAt.Format(time.RFC3339),
		},
	})

	log.Printf("[ws] lab started: session=%s target=%s timeout=%s", sessionID, targetURL, labTimeout)
}

// handleLabStop destroys the current lab session.
func handleLabStop(conn *websocket.Conn, st *sessionState, labMgr *executor.LabManager) {
	st.mu.Lock()
	lab := st.lab
	st.lab = nil
	st.mu.Unlock()

	if lab == nil {
		sendError(conn, "No active lab session")
		return
	}

	if labMgr != nil {
		labMgr.StopLab(context.Background(), lab)
	}

	sendMessage(conn, protocol.ServerMessage{
		Type:    "lab_stopped",
		Message: "Lab session ended",
	})

	log.Printf("[ws] lab stopped: session=%s", lab.SessionID)
}

// handleRunMessage creates a new container and starts execution.
func handleRunMessage(conn *websocket.Conn, docker *executor.DockerClient, cfg *config.Config, st *sessionState, msg *protocol.ClientMessage, sem *executor.Semaphore, labMgr *executor.LabManager) {
	// Validate
	if msg.Language == "" {
		sendError(conn, "language is required")
		return
	}

	// Resolve code and entry point from either files[] or legacy code field
	code := msg.Code
	if len(msg.Files) > 0 {
		// Multi-file: entry point is files[0], code is files[0].Content
		code = msg.Files[0].Content
	}
	if code == "" {
		sendError(conn, "code or files is required")
		return
	}

	// Check per-user container limit
	if !incUserContainers(st.userID) {
		sendError(conn, fmt.Sprintf("Too many concurrent programs (max %d per user). Close other tabs or wait for a run to finish.", MaxContainersPerUser))
		return
	}

	// Clean up any existing session
	st.mu.Lock()
	if st.session != nil {
		st.session.Cancel()
		if st.session.Conn != nil {
			st.session.Conn.Close()
		}
		docker.Remove(context.Background(), st.session.ContainerID)
		st.session = nil
	}
	// Increment generation — old exit goroutines will see this and bail
	st.generation++
	currentGen := st.generation
	st.timedOut = false

	// Get lab network ID (if lab is active)
	var labNetworkID string
	if st.lab != nil {
		labNetworkID = st.lab.NetworkID
	}
	st.mu.Unlock()

	log.Printf("[ws] run: lang=%s code_len=%d gen=%d lab=%v user=%s", msg.Language, len(code), currentGen, labNetworkID != "", st.userID)

	// Acquire concurrency slot — bounded wait so queued runs fail fast with
	// a clear message instead of hanging the connection while at capacity.
	acquireCtx, cancelAcquire := context.WithTimeout(context.Background(), 45*time.Second)
	if !sem.AcquireContext(acquireCtx) {
		cancelAcquire()
		sendError(conn, "Service is at capacity right now — please try again in a moment.")
		return
	}
	defer cancelAcquire()

	// Create container — use context.Background() (no timeout context).
	// The Docker attach connection (hijacked socket) references the context; cancelling
	// it closes the PTY stream and all output disappears.
	// Execution timeout is handled separately by the kill timer below.
	var extraFiles []protocol.File
	if len(msg.Files) > 1 {
		extraFiles = msg.Files[1:]
	}
	sess, err := docker.CreateInteractive(context.Background(), msg.Language, code, labNetworkID, extraFiles)

	if err != nil {
		sem.Release()
		decUserContainers(st.userID)
		log.Printf("[ws] container creation failed: %v", err)
		// Provide user-friendly error message
		errMsg := "Failed to start execution environment."
		if strings.Contains(err.Error(), "not available") || strings.Contains(err.Error(), "pull failed") {
			errMsg = fmt.Sprintf("Language runtime not available: %s. Please contact support.", msg.Language)
		} else if strings.Contains(err.Error(), "Cannot connect") {
			errMsg = "Runner service is not running. Please try again later."
		} else {
			errMsg = "Failed to start execution environment: " + err.Error()
		}
		sendError(conn, errMsg)
		return
	}

	// Update lab session with student container ID for cleanup
	if labMgr != nil && st.lab != nil {
		st.lab.StudentID = sess.ContainerID
	}

	// Store session and temp dir
	st.mu.Lock()
	st.session = sess
	st.tempDir = sess.TempDir
	st.mu.Unlock()

	// Send started message
	sendMessage(conn, protocol.ServerMessage{Type: "started"})

	// Bridge container output → WebSocket
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, readErr := sess.Reader.Read(buf)
			if n > 0 {
				// Check if this session is still current before sending
				st.mu.Lock()
				isCurrent := st.session != nil && st.generation == currentGen
				st.mu.Unlock()

				if !isCurrent {
					return
				}

				sendMessage(conn, protocol.ServerMessage{
					Type: "output",
					Data: string(buf[:n]),
				})
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("[ws] container read error: %v", readErr)
				}
				return
			}
		}
	}()

	// Auto-kill timer — use per-language timeout (cyber gets 120s, others get default 30s)
	killTimeout := docker.TimeoutForLang(msg.Language)
	killTimer := time.AfterFunc(killTimeout, func() {
		log.Printf("[ws] timeout after %s, killing container (gen=%d)", killTimeout, currentGen)

		st.mu.Lock()
		defer st.mu.Unlock()
		if st.session != nil && st.generation == currentGen {
			st.timedOut = true
			st.session.Cancel()
		}
	})

	// Wait for output goroutine to finish (container exited), then send exit
	go func() {
		// Release concurrency slot when container is destroyed
		defer sem.Release()
		defer decUserContainers(st.userID)

		<-outputDone

		// Stop the kill timer
		killTimer.Stop()

		// Only send exit if this is still the current generation
		st.mu.Lock()
		isCurrent := st.session != nil && st.generation == currentGen
		wasTimedOut := st.timedOut
		if !isCurrent {
			st.mu.Unlock()
			return
		}
		s := st.session
		st.mu.Unlock()

		if s == nil {
			return
		}

		// Get the actual exit code from Docker
		exitCode := 0
		statusCh, errCh := docker.WaitContainer(context.Background(), s.ContainerID)
		select {
		case status := <-statusCh:
			exitCode = int(status.StatusCode)
		case <-errCh:
			// Container already removed or error — assume 0
		case <-time.After(2 * time.Second):
			// Don't wait too long
		}

		// Inspect container for OOM kill and determine exit reason
		reason := docker.InspectContainer(context.Background(), s.ContainerID, exitCode, wasTimedOut)

		// Double-check generation before sending exit
		st.mu.Lock()
		if st.generation != currentGen {
			st.mu.Unlock()
			return
		}
		st.mu.Unlock()

		// Read all files from the container before cleanup (sync terminal-created files)
		if fileErr := (func() error {
			files, folders, readErr := docker.ReadFilesFromContainer(context.Background(), s.ContainerID)
			if readErr != nil {
				return readErr
			}
			if len(files) > 0 || len(folders) > 0 {
				sendMessage(conn, protocol.ServerMessage{
					Type:    "files",
					Files:   files,
					Folders: folders,
				})
			}
			return nil
		})(); fileErr != nil {
			log.Printf("[ws] failed to read files from container: %v", fileErr)
		}

		// Build exit message with reason
		exitMsg := protocol.ServerMessage{
			Type:   "exit",
			Code:   &exitCode,
			Reason: string(reason),
		}
		switch reason {
		case executor.ExitReasonTimeout:
			msg := "Execution timed out (30s limit)"
			exitMsg.Message = msg
		case executor.ExitReasonOOM:
			msg := "Program used too much memory and was terminated"
			exitMsg.Message = msg
		case executor.ExitReasonCrash:
			msg := "Program crashed unexpectedly"
			exitMsg.Message = msg
		}
		sendMessage(conn, exitMsg)

		// Cleanup container
		st.mu.Lock()
		if st.session != nil && st.generation == currentGen {
			st.session.Cancel()
			if st.session.Conn != nil {
				st.session.Conn.Close()
			}
			docker.Remove(context.Background(), st.session.ContainerID)
			st.session = nil
		}
		st.mu.Unlock()
	}()
}

// handleInputMessage forwards stdin from the WebSocket client to the container.
func handleInputMessage(st *sessionState, msg protocol.ClientMessage) {
	st.mu.Lock()
	s := st.session
	st.mu.Unlock()

	if s == nil || s.Conn == nil {
		return
	}

	if err := writeAll(s.Conn, []byte(msg.Data)); err != nil {
		log.Printf("[ws] container write error: %v", err)
	}
}

// handleResizeMessage resizes the container's PTY to match the client terminal dimensions.
// This keeps line-wrapping, cursor positioning, and TUI layouts correct when the
// user resizes the IDE panel or toggles fullscreen.
func handleResizeMessage(docker *executor.DockerClient, st *sessionState, msg protocol.ClientMessage) {
	if msg.Cols == 0 || msg.Rows == 0 {
		return
	}

	st.mu.Lock()
	s := st.session
	st.mu.Unlock()

	if s == nil {
		return
	}

	if err := docker.ResizePTY(context.Background(), s.ContainerID, msg.Cols, msg.Rows); err != nil {
		log.Printf("[ws] pty resize failed (cols=%d rows=%d): %v", msg.Cols, msg.Rows, err)
	}
}

// handleWriteMessage writes a file into the running container's /code/ directory.
func handleWriteMessage(conn *websocket.Conn, docker *executor.DockerClient, st *sessionState, msg *protocol.ClientMessage) {
	st.mu.Lock()
	s := st.session
	st.mu.Unlock()

	if s == nil {
		sendError(conn, "No active container — run code first")
		return
	}

	if msg.Path == "" {
		sendError(conn, "path is required for write")
		return
	}

	// Sanitize path — prevent traversal by resolving and checking prefix
	pathClean := strings.TrimPrefix(msg.Path, "/")
	pathClean = strings.TrimPrefix(pathClean, "./")
	if strings.Contains(pathClean, "..") {
		sendError(conn, "invalid path")
		return
	}
	// Ensure the resolved path stays under /code
	resolvedPath := "/code/" + pathClean
	if resolvedPath == "/code/" || strings.HasPrefix(resolvedPath, "/code/../") {
		sendError(conn, "invalid path")
		return
	}

	// Write the file into the container via exec
	err := docker.WriteFileToContainer(context.Background(), s.ContainerID, pathClean, msg.Data)
	if err != nil {
		sendError(conn, "Failed to write file: "+err.Error())
		return
	}

	log.Printf("[ws] wrote file: %s (len=%d)", msg.Path, len(msg.Data))
}

// handleReadFilesMessage reads all files from the running container's /code/ directory
// and sends them back to the client. Used after execution to sync terminal-created files.
func handleReadFilesMessage(conn *websocket.Conn, docker *executor.DockerClient, st *sessionState) {
	st.mu.Lock()
	s := st.session
	st.mu.Unlock()

	if s == nil {
		sendError(conn, "No active container")
		return
	}

	files, folders, err := docker.ReadFilesFromContainer(context.Background(), s.ContainerID)
	if err != nil {
		sendError(conn, "Failed to read files: "+err.Error())
		return
	}

	sendMessage(conn, protocol.ServerMessage{
		Type:    "files",
		Files:   files,
		Folders: folders,
	})

	log.Printf("[ws] read %d files, %d folders from container", len(files), len(folders))
}

// handleShellMessage starts a persistent interactive shell container.
// Unlike handleRunMessage, it does not execute code — it just runs bash/sh.
// The shell stays alive until the client disconnects or sends a "run" (which replaces it).
func handleShellMessage(conn *websocket.Conn, docker *executor.DockerClient, cfg *config.Config, st *sessionState, msg *protocol.ClientMessage, sem *executor.Semaphore) {
	// Clean up any existing session
	st.mu.Lock()
	if st.session != nil {
		st.session.Cancel()
		if st.session.Conn != nil {
			st.session.Conn.Close()
		}
		docker.Remove(context.Background(), st.session.ContainerID)
		st.session = nil
	}
	st.generation++
	currentGen := st.generation

	var labNetworkID string
	if st.lab != nil {
		labNetworkID = st.lab.NetworkID
	}
	st.mu.Unlock()

	lang := msg.Language
	if lang == "" {
		lang = "python"
	}

	log.Printf("[ws] shell: lang=%s gen=%d lab=%v", lang, currentGen, labNetworkID != "")

	// Acquire concurrency slot — bounded wait (see run path comment).
	shellAcquireCtx, cancelShellAcquire := context.WithTimeout(context.Background(), 45*time.Second)
	if !sem.AcquireContext(shellAcquireCtx) {
		cancelShellAcquire()
		sendError(conn, "Service is at capacity right now — please try again in a moment.")
		return
	}
	defer cancelShellAcquire()

	sess, err := docker.CreateShell(context.Background(), lang, labNetworkID)
	if err != nil {
		sem.Release()
		log.Printf("[ws] shell creation failed: %v", err)
		sendError(conn, "Failed to create shell: "+err.Error())
		return
	}

	st.mu.Lock()
	st.session = sess
	st.mu.Unlock()

	// Send started message
	sendMessage(conn, protocol.ServerMessage{Type: "started"})

	// Bridge container output → WebSocket
	outputDone := make(chan struct{})
	go func() {
		defer close(outputDone)
		buf := make([]byte, 4096)
		for {
			n, readErr := sess.Reader.Read(buf)
			if n > 0 {
				st.mu.Lock()
				isCurrent := st.session != nil && st.generation == currentGen
				st.mu.Unlock()

				if !isCurrent {
					return
				}

				sendMessage(conn, protocol.ServerMessage{
					Type: "output",
					Data: string(buf[:n]),
				})
			}
			if readErr != nil {
				if readErr != io.EOF {
					log.Printf("[ws] shell read error: %v", readErr)
				}
				return
			}
		}
	}()

	// Wait for output goroutine to finish (shell exited or replaced)
	go func() {
		defer sem.Release()
		<-outputDone

		st.mu.Lock()
		isCurrent := st.session != nil && st.generation == currentGen
		if !isCurrent {
			st.mu.Unlock()
			return
		}
		s := st.session
		st.mu.Unlock()

		if s == nil {
			return
		}

		exitCode := 0
		statusCh, errCh := docker.WaitContainer(context.Background(), s.ContainerID)
		select {
		case status := <-statusCh:
			exitCode = int(status.StatusCode)
		case <-errCh:
		case <-time.After(2 * time.Second):
		}

		st.mu.Lock()
		if st.session != nil && st.generation == currentGen {
			st.session.Cancel()
			if st.session.Conn != nil {
				st.session.Conn.Close()
			}
			docker.Remove(context.Background(), st.session.ContainerID)
			st.session = nil
		}
		st.mu.Unlock()

		// Shell exited — don't send exit message to client; just let it go silent.
		// The client can start a new shell or run code at any time.
		_ = exitCode
	}()
}

// TriggerShutdown signals all WebSocket handlers to stop accepting new runs
// and cleans up active sessions. Called from main during SIGTERM.
func TriggerShutdown(docker *executor.DockerClient) {
	shutdownOnce.Do(func() {
		close(shutdownCh)
		log.Println("[ws] shutdown signaled — rejecting new connections")
	})
}

func sendMessage(conn *websocket.Conn, msg protocol.ServerMessage) {
	conn.WriteJSON(msg)
}

func sendError(conn *websocket.Conn, message string) {
	sendMessage(conn, protocol.ServerMessage{
		Type:    "error",
		Message: message,
	})
}

func intPtr(i int) *int {
	return &i
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		data = data[n:]
	}
	return nil
}
