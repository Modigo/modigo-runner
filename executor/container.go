package executor

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"

	"github.com/modigo/runner/protocol"
)

// DockerClient wraps the Docker SDK client.
type DockerClient struct {
	client *client.Client
	prefix string
	limits *Limits
}

// NewDockerClient creates a new Docker client connected to the local daemon.
func NewDockerClient(socketPath, imagePrefix string, limits *Limits) (*DockerClient, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &DockerClient{
		client: cli,
		prefix: imagePrefix,
		limits: limits,
	}, nil
}

// LanguageToImage maps a language identifier to the full Docker image name.
func (d *DockerClient) LanguageToImage(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "python", "py":
		return d.prefix + "python"
	case "javascript", "js", "node", "nodejs":
		return d.prefix + "javascript"
	case "c":
		return d.prefix + "c"
	case "cpp", "c++":
		return d.prefix + "cpp"
	case "go", "golang":
		return d.prefix + "go"
	case "java":
		return d.prefix + "java"
	case "rust", "rs":
		return d.prefix + "rust"
	case "php":
		return d.prefix + "php"
	case "cyber", "cybersecurity":
		return d.prefix + "cyber"
	default:
		return d.prefix + lang
	}
}

// LanguageToMainFile maps a language to its default main file name.
func LanguageToMainFile(lang string) string {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "python", "py":
		return "main.py"
	case "javascript", "js", "node", "nodejs":
		return "index.js"
	case "c":
		return "main.c"
	case "cpp", "c++":
		return "main.cpp"
	case "go", "golang":
		return "main.go"
	case "java":
		return "Main.java"
	case "rust", "rs":
		return "main.rs"
	case "php":
		return "index.php"
	case "cyber", "cybersecurity":
		return "main.py"
	default:
		return "main.txt"
	}
}

// isCyberLang returns true if the language requires network access and elevated resources.
func isCyberLang(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	return lang == "cyber" || lang == "cybersecurity"
}

// isCompiledLang returns true for languages that require a compilation step.
// These get a higher CPU quota so the compile phase feels snappier.
func isCompiledLang(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "go", "golang", "java", "rust", "rs", "c", "cpp", "c++":
		return true
	}
	return false
}

// Ping checks if the Docker daemon is reachable.
func (d *DockerClient) Ping(ctx context.Context) error {
	_, err := d.client.Ping(ctx)
	return err
}

// networkModeForLang returns the Docker network mode for a given language.
func networkModeForLang(lang string) container.NetworkMode {
	if isCyberLang(lang) {
		return container.NetworkMode("bridge")
	}
	return container.NetworkMode("none")
}

// networkModeOrLab returns the network mode for the student container.
// If labNetworkID is non-empty, the container joins the lab network.
// Otherwise, uses the default per-language mode.
func networkModeOrLab(lang string, labNetworkID string) container.NetworkMode {
	if labNetworkID != "" {
		return container.NetworkMode(labNetworkID)
	}
	return networkModeForLang(lang)
}

// limitsForLang returns resource limits appropriate for the language.
// Cybersecurity containers get higher limits for scanning and tool usage.
// Compiled languages (Go, Java, Rust, C, C++) get extra CPU to speed up
// the compile step.
func (d *DockerClient) limitsForLang(lang string) (int64, int64, int64, int64) {
	if isCyberLang(lang) {
		return parseMemory("512m"), 100000, 80000, 128 // 512MB, 0.8 CPU, 128 PIDs
	}
	if isCompiledLang(lang) {
		// Use the configured memory and PIDs, but bump CPU to 0.8 for the compile phase
		return d.limits.Memory, 100000, 80000, int64(d.limits.PidsLimit)
	}
	return d.limits.Memory, d.limits.CPUPeriod, d.limits.CPUQuota, int64(d.limits.PidsLimit)
}

// TimeoutForLang returns the execution timeout for a given language.
func (d *DockerClient) TimeoutForLang(lang string) time.Duration {
	if isCyberLang(lang) {
		return 120 * time.Second // 2 minutes for cyber tasks (scanning, brute force, etc.)
	}
	return time.Duration(d.limits.TimeoutSecs) * time.Second
}

// InteractiveSession represents a live container with a PTY attached.
type InteractiveSession struct {
	ContainerID string
	Conn        io.ReadWriteCloser // for writing stdin
	Reader      io.Reader          // for reading stdout/stderr (buffered)
	Cancel      context.CancelFunc
	TempDir     string // code directory — caller must clean up via RemoveTempDir
}

// RemoveTempDir removes a temporary code directory. Safe to call with empty string.
func RemoveTempDir(dir string) {
	if dir != "" {
		os.RemoveAll(dir)
	}
}

// CreateInteractive creates and starts a container with a PTY attached for interactive use.
// The returned session has a bidirectional stream ready for bridging to a WebSocket.
func (d *DockerClient) CreateInteractive(ctx context.Context, lang, code string, labNetworkID string, extraFiles []protocol.File) (*InteractiveSession, error) {
	ctx, cancel := context.WithCancel(ctx)
	image := d.LanguageToImage(lang)

	// Ensure image exists (pull if needed)
	if err := d.ensureImage(ctx, image); err != nil {
		cancel()
		return nil, fmt.Errorf("ensure image %s: %w", image, err)
	}

	// Write code to a temp directory
	mainFile := LanguageToMainFile(lang)
	codeDir, err := os.MkdirTemp("", "modigo-runner-*")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	codePath := filepath.Join(codeDir, mainFile)
	if err := os.WriteFile(codePath, []byte(code), 0644); err != nil {
		os.RemoveAll(codeDir)
		cancel()
		return nil, fmt.Errorf("write code: %w", err)
	}

	// Write additional files for multi-file projects
	for _, f := range extraFiles {
		// Sanitize: block ".." to prevent traversal; block empty names
		if f.Name == "" || strings.Contains(f.Name, "..") {
			log.Printf("[container] skipping unsafe extra file name: %q", f.Name)
			continue
		}

		// Ensure filePath is still inside codeDir (prevent traversal)
		filePath := filepath.Join(codeDir, f.Name)
		if !strings.HasPrefix(filepath.Clean(filePath), filepath.Clean(codeDir)) {
			log.Printf("[container] skipping unsafe extra file path: %q", f.Name)
			continue
		}

		// Don't overwrite the main entry file
		if filePath != codePath {
			// Ensure parent directory exists
			if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
				log.Printf("[container] warning: could not create dir for %s: %v", f.Name, err)
				continue
			}
			if err := os.WriteFile(filePath, []byte(f.Content), 0644); err != nil {
				log.Printf("[container] warning: could not write extra file %s: %v", f.Name, err)
			}
		}
	}

	// Auto-create go.mod for Go projects that don't have one.
	// This allows local package imports (e.g. import "utils") to work.
	if isCompiled(lang) && (lang == "go" || lang == "golang") {
		hasGoMod := false
		for _, f := range extraFiles {
			if f.Name == "go.mod" {
				hasGoMod = true
				break
			}
		}
		if !hasGoMod {
			goModPath := filepath.Join(codeDir, "go.mod")
			if _, err := os.Stat(goModPath); os.IsNotExist(err) {
				// Detect module name from imports in the main file
				os.WriteFile(goModPath, []byte("module modigo\n\ngo 1.22\n"), 0644)
				log.Printf("[container] auto-created go.mod for Go project")
			}
		}
	}

	// For compiled languages, wrap code in a compile-and-run script
	wrapperPath := ""
	if isCompiled(lang) {
		wrapperPath, err = d.createCompileWrapper(codeDir, lang, mainFile)
		if err != nil {
			os.RemoveAll(codeDir)
			cancel()
			return nil, fmt.Errorf("compile wrapper: %w", err)
		}
	}

	// Container config: TTY enabled, stdin open.
	// PS1/PS2 are blanked and TERM=dumb so the shell never emits a prompt
	// character (e.g. "# ") into the PTY stream before the user's process
	// takes over. Without this, root's default prompt leaks into xterm output.
	containerCfg := &container.Config{
		Image:        image,
		Tty:          true,
		OpenStdin:    true,
		StdinOnce:    false,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   "/code",
		Env: []string{
			"PS1=",
			"PS2=",
			"TERM=xterm-256color",
		},
	}

	// If we have a wrapper (compiled language), override the command
	if wrapperPath != "" {
		wrapperName := filepath.Base(wrapperPath)
		containerCfg.Cmd = []string{"/bin/sh", "/code/" + wrapperName}
	}

	// Host config: resource limits, network mode, tmpfs for /tmp
	memLimit, cpuPeriod, cpuQuota, pidsLimit := d.limitsForLang(lang)
	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			Memory:    memLimit,
			CPUPeriod: cpuPeriod,
			CPUQuota:  cpuQuota,
			PidsLimit: &pidsLimit,
		},
		NetworkMode:    networkModeOrLab(lang, labNetworkID),
		Privileged:     false,
		ReadonlyRootfs: false,
		AutoRemove:     false,
		SecurityOpt:    []string{"no-new-privileges"},
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   codeDir,
				Target:   "/code",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					Options: [][]string{{"exec"}},
				},
			},
		},
	}

	// Create the container
	resp, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		os.RemoveAll(codeDir)
		cancel()
		return nil, fmt.Errorf("container create: %w", err)
	}
	containerID := resp.ID

	// Start the container
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		d.Remove(context.Background(), containerID)
		os.RemoveAll(codeDir)
		cancel()
		return nil, fmt.Errorf("container start: %w", err)
	}

	// Attach to the container for PTY I/O
	attachResp, err := d.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		d.Remove(context.Background(), containerID)
		os.RemoveAll(codeDir)
		cancel()
		return nil, fmt.Errorf("container attach: %w", err)
	}

	// CRITICAL: Read from attachResp.Reader (buffered), write to attachResp.Conn.
	// The Docker SDK buffers some data into Reader during attach.
	// Reading from Conn directly would miss that buffered data — output disappears.
	log.Printf("[container] created %s (lang=%s, id=%s)", mainFile, lang, containerID[:12])

	return &InteractiveSession{
		ContainerID: containerID,
		Conn:        attachResp.Conn,
		Reader:      attachResp.Reader,
		Cancel:      cancel,
		TempDir:     codeDir,
	}, nil
}

// CreateShell creates a persistent container running an interactive shell (bash/sh).
// Unlike CreateInteractive, it does NOT write code or use a wrapper — the container
// just runs a shell. Used for the terminal shell mode where the user types commands.
//
// Security: the shell is jailed to /code via PS1 cwd restriction and a cd wrapper
// that prevents navigation above /code. Users see a clean workspace, not the container fs.
func (d *DockerClient) CreateShell(ctx context.Context, lang string, labNetworkID string) (*InteractiveSession, error) {
	ctx, cancel := context.WithCancel(ctx)
	image := d.LanguageToImage(lang)

	// Ensure image exists (pull if needed)
	if err := d.ensureImage(ctx, image); err != nil {
		cancel()
		return nil, fmt.Errorf("ensure image %s: %w", image, err)
	}

	// We write a startup ENV file to /tmp and exec the best available shell.
	// The script is written via base64 so no quoting issues exist.
	//
	// POSIX sh compatibility notes:
	//   - `local` is POSIX in practice but not POSIX strictly; all target images
	//     ship busybox sh or dash which both support `local`.
	//   - `builtin` is bash-only. In sh we just call `command cd` to skip our override.
	//   - PROMPT_COMMAND is bash-only. In sh we set PS1 directly with \w-style escapes.
	//   - Colors: ANSI escapes work in xterm-256color for both bash and sh.
	//
	// The startup script:
	//   1. Jails cd to /code
	//   2. Sets a colored prompt showing path relative to /code
	//   3. Enables color aliases (ls, grep, diff)

	// sh-compatible profile (works in dash/ash/busybox sh AND bash)
	shProfile := `
export HOME=/code
export TERM=xterm-256color
export COLORTERM=truecolor
export LANG=C.UTF-8
export LC_ALL=C.UTF-8

# ── CD jail: prevent navigation outside /code ──────────────────────────
cd() {
    local dest resolved
    if [ $# -eq 0 ] || [ "$1" = "~" ]; then
        command cd /code
        return $?
    fi
    dest="$1"
    case "$dest" in
        /*) ;;
        *) dest="$(pwd)/$dest" ;;
    esac
    if command -v realpath >/dev/null 2>&1; then
        resolved="$(realpath -m "$dest" 2>/dev/null)"
    else
        resolved="$(command cd "$dest" 2>/dev/null && pwd)"
    fi
    if [ -z "$resolved" ] || \
       { [ "$resolved" != "/code" ] && [ "${resolved#/code/}" = "$resolved" ]; }; then
        printf '\033[0;31mcd: permission denied: cannot navigate outside workspace\033[0m\n' >&2
        return 1
    fi
    command cd "$resolved"
}

# ── Colour aliases ──────────────────────────────────────────────────────
alias ls='ls --color=auto 2>/dev/null || ls'
alias ll='ls -lah --color=auto 2>/dev/null || ls -lah'
alias la='ls -la --color=auto 2>/dev/null || ls -la'
alias grep='grep --color=auto'
alias diff='diff --color=auto 2>/dev/null || diff'

# ── Prompt (sh-compatible, no PROMPT_COMMAND) ──────────────────────────
# We compute the relative path inline via parameter expansion.
# This gets re-evaluated each time PS1 is displayed.
PS1='\[\033[01;32m\]workspace\[\033[00m\]:\[\033[01;34m\]$(p="${PWD#/code}"; printf "%s" "${p:-/}")\[\033[00m\]\$ '
`

	// bash-specific additions — only sourced when bash is the shell
	bashExtra := `
# bash-only: better prompt via PROMPT_COMMAND, history settings
_modigo_ps1() {
    local rel
    rel="${PWD#/code}"
    printf '\[\033[01;32m\]workspace\[\033[00m\]:\[\033[01;34m\]%s\[\033[00m\]\$ ' "${rel:-/}"
}
PROMPT_COMMAND='PS1="$(_modigo_ps1)"'
HISTSIZE=1000
HISTCONTROL=ignoredups
`

	encodedProfile := base64.StdEncoding.EncodeToString([]byte(shProfile))
	encodedBashExtra := base64.StdEncoding.EncodeToString([]byte(bashExtra))

	// The outer /bin/sh script:
	//  1. Decodes and writes the shared profile to /tmp/.profile
	//  2. If bash exists: appends bash extras and execs bash --rcfile /tmp/.profile
	//  3. Otherwise: execs sh with ENV=/tmp/.profile (POSIX sh reads $ENV on startup)
	shellCmd := fmt.Sprintf(
		`/bin/sh -c 'echo "%s" | base64 -d > /tmp/.profile && `+
			`if command -v bash >/dev/null 2>&1; then `+
			`echo "%s" | base64 -d >> /tmp/.profile && exec bash --rcfile /tmp/.profile; `+
			`else ENV=/tmp/.profile exec sh; fi'`,
		encodedProfile, encodedBashExtra,
	)

	containerCfg := &container.Config{
		Image:        image,
		Tty:          true,
		OpenStdin:    true,
		StdinOnce:    false,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		WorkingDir:   "/code",
		Cmd:          []string{"/bin/sh", "-c", shellCmd},
		Env: []string{
			"TERM=xterm-256color",
			"LANG=C.UTF-8",
			"LC_ALL=C.UTF-8",
			"HOME=/code",
		},
	}

	// Host config: resource limits, no network (unless lab), writable /code via tmpfs
	memLimit, cpuPeriod, cpuQuota, pidsLimit := d.limitsForLang(lang)
	networkMode := "none"
	if labNetworkID != "" {
		networkMode = "container:" + labNetworkID
	}
	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			Memory:    memLimit,
			CPUPeriod: cpuPeriod,
			CPUQuota:  cpuQuota,
			PidsLimit: &pidsLimit,
		},
		NetworkMode:    container.NetworkMode(networkMode),
		Privileged:     false,
		ReadonlyRootfs: false,
		AutoRemove:     false,
		SecurityOpt:    []string{"no-new-privileges"},
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					Options: [][]string{{"exec"}},
				},
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/code",
				TmpfsOptions: &mount.TmpfsOptions{
					SizeBytes: 64 * 1024 * 1024, // 64 MiB writable workspace
					Options:   [][]string{{"exec"}},
				},
			},
		},
	}

	// Create the container
	resp, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		cancel()
		return nil, fmt.Errorf("container create: %w", err)
	}
	containerID := resp.ID

	// Start the container
	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		d.Remove(context.Background(), containerID)
		cancel()
		return nil, fmt.Errorf("container start: %w", err)
	}

	// Attach to the container for PTY I/O
	attachResp, err := d.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		d.Remove(context.Background(), containerID)
		cancel()
		return nil, fmt.Errorf("container attach: %w", err)
	}

	log.Printf("[container] shell started (lang=%s, id=%s)", lang, containerID[:12])

	return &InteractiveSession{
		ContainerID: containerID,
		Conn:        attachResp.Conn,
		Reader:      attachResp.Reader,
		Cancel:      cancel,
		TempDir:     "",
	}, nil
}

// RunNonInteractive executes code in a container and captures stdout/stderr.
// Used by the REST API endpoint.
func (d *DockerClient) RunNonInteractive(ctx context.Context, req protocol.RESTRequest) (*protocol.RESTResponse, error) {
	startTime := time.Now()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.limits.TimeoutSecs)*time.Second)
	defer cancel()

	image := d.LanguageToImage(req.Language)
	if err := d.ensureImage(ctx, image); err != nil {
		return &protocol.RESTResponse{
			Status:   "Error",
			Stderr:   fmt.Sprintf("Image not found: %s", image),
			ExitCode: 1,
		}, nil
	}

	// Prepare code files in a temp directory
	codeDir, err := os.MkdirTemp("", "modigo-rest-*")
	if err != nil {
		return nil, fmt.Errorf("temp dir: %w", err)
	}
	defer os.RemoveAll(codeDir)

	// Determine main file and write files
	mainFile := LanguageToMainFile(req.Language)
	if len(req.Files) > 0 {
		mainFile = req.Files[0].Name
	}

	// Write all provided files
	for _, f := range req.Files {
		fPath := filepath.Join(codeDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(fPath), 0755); err != nil {
			return nil, fmt.Errorf("mkdir: %w", err)
		}
		if err := os.WriteFile(fPath, []byte(f.Content), 0644); err != nil {
			return nil, fmt.Errorf("write file: %w", err)
		}
	}

	// If single code string provided (no files), write it as the main file
	if len(req.Files) == 0 && req.Code != "" {
		if err := os.WriteFile(filepath.Join(codeDir, mainFile), []byte(req.Code), 0644); err != nil {
			return nil, fmt.Errorf("write code: %w", err)
		}
	}

	// Build container config — non-interactive (Tty=false for stdcopy demux)
	containerCfg := &container.Config{
		Image:        image,
		WorkingDir:   "/code",
		Tty:          false,
		OpenStdin:    true,
		StdinOnce:    true,
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
	}

	// For compiled languages, add a compile+run wrapper
	if isCompiled(req.Language) {
		wrapperPath, err := d.createCompileWrapper(codeDir, req.Language, mainFile)
		if err != nil {
			return nil, fmt.Errorf("compile wrapper: %w", err)
		}
		containerCfg.Cmd = []string{"/bin/sh", "/code/" + filepath.Base(wrapperPath)}
	}

	// Resource limits
	memLimit := d.limits.Memory
	pidsLimit := int64(d.limits.PidsLimit)
	hostCfg := &container.HostConfig{
		Resources: container.Resources{
			Memory:    memLimit,
			CPUPeriod: d.limits.CPUPeriod,
			CPUQuota:  d.limits.CPUQuota,
			PidsLimit: &pidsLimit,
		},
		NetworkMode:    container.NetworkMode("none"),
		Privileged:     false,
		SecurityOpt:    []string{"no-new-privileges"},
		ReadonlyRootfs: false,
		Mounts: []mount.Mount{
			{
				Type:     mount.TypeBind,
				Source:   codeDir,
				Target:   "/code",
				ReadOnly: true,
			},
			{
				Type:   mount.TypeTmpfs,
				Target: "/tmp",
				TmpfsOptions: &mount.TmpfsOptions{
					Options: [][]string{{"exec"}},
				},
			},
		},
	}

	resp, err := d.client.ContainerCreate(ctx, containerCfg, hostCfg, nil, nil, "")
	if err != nil {
		return nil, fmt.Errorf("container create: %w", err)
	}
	containerID := resp.ID
	defer d.Remove(context.Background(), containerID)

	if err := d.client.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return nil, fmt.Errorf("container start: %w", err)
	}

	// Attach to pipe stdin
	attachResp, err := d.client.ContainerAttach(ctx, containerID, container.AttachOptions{
		Stream: true,
		Stdin:  true,
		Stdout: true,
		Stderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("container attach: %w", err)
	}

	// Pipe stdin if provided
	if req.Stdin != "" {
		go func() {
			defer attachResp.Close()
			attachResp.Conn.Write([]byte(req.Stdin))
		}()
	} else {
		// Close stdin so the process knows there's no input
		attachResp.Conn.Close()
	}

	// Wait for container to finish
	statusCh, errCh := d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)

	var exitCode int64
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("container wait: %w", err)
		}
	case status := <-statusCh:
		exitCode = status.StatusCode
	}

	// Get logs (after container stops)
	logReader, err := d.client.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
	})
	if err != nil {
		return nil, fmt.Errorf("container logs: %w", err)
	}
	defer logReader.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, logReader); err != nil {
		// Fallback: treat everything as stdout
		io.Copy(&stdout, logReader)
	}

	execTime := time.Since(startTime).Milliseconds()
	status := "success"
	if exitCode != 0 {
		status = "error"
	}

	return &protocol.RESTResponse{
		Status:        status,
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
		ExitCode:      int(exitCode),
		ExecutionTime: execTime,
	}, nil
}

// WriteFileToContainer writes a file into the container's /code/ directory via exec.
func (d *DockerClient) WriteFileToContainer(ctx context.Context, containerID, path, content string) error {
	// Sanitize: ensure path is relative and inside /code/
	if strings.Contains(path, "..") {
		return fmt.Errorf("invalid path: %s", path)
	}
	fullPath := "/code/" + path

	// Create parent directory if needed
	dir := filepath.Dir(fullPath)
	mkdirCmd := []string{"/bin/sh", "-c", "mkdir -p " + dir}
	if _, err := d.execQuiet(ctx, containerID, mkdirCmd); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Write file content via cat with heredoc
	// Use base64 encoding to avoid shell escaping issues
	encoded := base64Encode(content)
	writeCmd := []string{"/bin/sh", "-c", "echo '" + encoded + "' | base64 -d > " + fullPath}
	if _, err := d.execQuiet(ctx, containerID, writeCmd); err != nil {
		return fmt.Errorf("write %s: %w", fullPath, err)
	}

	return nil
}

// ReadFilesFromContainer reads all files (and directories) from /code/ and returns them.
// Returns both files and a list of directory paths relative to /code/.
func (d *DockerClient) ReadFilesFromContainer(ctx context.Context, containerID string) ([]protocol.File, []string, error) {
	// List all files recursively, excluding:
	//   - dotfiles (hidden files like .ash_history, .bash_history, .profile, .bashrc)
	//   - .keep sentinel
	//   - compiled artifacts (*.o, *.class, *.pyc, __pycache__, target/, node_modules/)
	listCmd := []string{"/bin/sh", "-c",
		`find /code -type f \
		! -name '.*' \
		! -name '*.o' \
		! -name '*.pyc' \
		! -path '*/__pycache__/*' \
		! -path '*/node_modules/*' \
		! -path '*/target/*' \
		! -path '*/.git/*' \
		| sort`,
	}
	listOutput, err := d.execQuiet(ctx, containerID, listCmd)
	if err != nil {
		return nil, nil, fmt.Errorf("list files: %w", err)
	}

	// List all non-hidden directories under /code (exclude /code itself and dotdirs)
	dirCmd := []string{"/bin/sh", "-c",
		`find /code -mindepth 1 -type d \
		! -name '.*' \
		! -path '*/__pycache__' \
		! -path '*/node_modules' \
		! -path '*/node_modules/*' \
		! -path '*/target' \
		! -path '*/target/*' \
		! -path '*/.git' \
		! -path '*/.git/*' \
		| sort`,
	}
	dirOutput, err := d.execQuiet(ctx, containerID, dirCmd)
	if err != nil {
		// Non-fatal — just skip directory sync
		dirOutput = ""
	}

	var files []protocol.File
	lines := strings.Split(strings.TrimSpace(listOutput), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Strip /code/ prefix to get relative path
		relPath := strings.TrimPrefix(line, "/code/")

		// Skip dotfiles, shell artifacts, and compiled output — double-check
		// in Go in case the find flags differ between busybox and GNU find.
		if shouldSkipFile(relPath) {
			continue
		}

		// Read file content
		readCmd := []string{"/bin/sh", "-c", "cat " + line}
		content, err := d.execQuiet(ctx, containerID, readCmd)
		if err != nil {
			log.Printf("[container] warning: could not read %s: %v", line, err)
			continue
		}

		files = append(files, protocol.File{
			Name:    relPath,
			Content: content,
		})
	}

	var folders []string
	if dirOutput != "" {
		dirLines := strings.Split(strings.TrimSpace(dirOutput), "\n")
		for _, line := range dirLines {
			line = strings.TrimSpace(line)
			if line == "" || line == "/code" {
				continue
			}
			relPath := strings.TrimPrefix(line, "/code/")
			// Skip hidden directories and build artifact dirs
			if relPath == "" || shouldSkipDir(relPath) {
				continue
			}
			folders = append(folders, relPath)
		}
	}

	return files, folders, nil
}

// execQuiet runs a command in the container and returns stdout as a string.
func (d *DockerClient) execQuiet(ctx context.Context, containerID string, cmd []string) (string, error) {
	execCfg := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmd,
	}

	execResp, err := d.client.ContainerExecCreate(ctx, containerID, execCfg)
	if err != nil {
		return "", err
	}

	hijack, err := d.client.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", err
	}
	defer hijack.Close()

	// Read output
	var buf bytes.Buffer
	if _, err := stdcopy.StdCopy(&buf, io.Discard, hijack.Reader); err != nil {
		return "", err
	}

	return buf.String(), nil
}

// base64Encode encodes a string to base64 (no newlines).
func base64Encode(s string) string {
	return base64.StdEncoding.EncodeToString([]byte(s))
}

// Remove forcefully removes a container.
func (d *DockerClient) Remove(ctx context.Context, containerID string) {
	opts := container.RemoveOptions{
		Force:         true,
		RemoveVolumes: true,
	}
	if err := d.client.ContainerRemove(ctx, containerID, opts); err != nil {
		log.Printf("[container] remove %s: %v", containerID[:12], err)
	} else {
		log.Printf("[container] removed %s", containerID[:12])
	}
}

// ResizePTY resizes the PTY of a running container.
// Called whenever the client sends a "resize" message so that the container's
// PTY dimensions stay in sync with the xterm.js viewport.
func (d *DockerClient) ResizePTY(ctx context.Context, containerID string, cols, rows uint16) error {
	return d.client.ContainerResize(ctx, containerID, container.ResizeOptions{
		Width:  uint(cols),
		Height: uint(rows),
	})
}

// WaitContainer waits for a container to stop and returns its exit status.
func (d *DockerClient) WaitContainer(ctx context.Context, containerID string) (<-chan container.WaitResponse, <-chan error) {
	return d.client.ContainerWait(ctx, containerID, container.WaitConditionNotRunning)
}

// ExitReason represents why a container stopped.
type ExitReason string

const (
	ExitReasonNormal  ExitReason = "normal"  // exit code 0
	ExitReasonError   ExitReason = "error"   // non-zero exit code
	ExitReasonOOM     ExitReason = "oom"     // killed by Docker OOM killer
	ExitReasonTimeout ExitReason = "timeout" // killed by our timeout timer
	ExitReasonCrash   ExitReason = "crash"   // container crashed / died unexpectedly
)

// InspectContainer checks a stopped container for OOM kills and other details.
// Returns the exit reason based on Docker's inspection data.
func (d *DockerClient) InspectContainer(ctx context.Context, containerID string, exitCode int, wasTimedOut bool) ExitReason {
	if wasTimedOut {
		return ExitReasonTimeout
	}

	info, err := d.client.ContainerInspect(ctx, containerID)
	if err != nil {
		log.Printf("[container] inspect failed for %s: %v", containerID[:12], err)
		if exitCode == 0 {
			return ExitReasonNormal
		}
		return ExitReasonCrash
	}

	// Check OOMKilled
	if info.State != nil && info.State.OOMKilled {
		log.Printf("[container] %s was OOM-killed (memory limit exceeded)", containerID[:12])
		return ExitReasonOOM
	}

	if exitCode == 0 {
		return ExitReasonNormal
	}

	// Non-zero exit code that wasn't OOM or timeout
	return ExitReasonError
}

// ensureImage checks if a Docker image exists locally, pulls it if not.
func (d *DockerClient) ensureImage(ctx context.Context, imageName string) error {
	_, _, err := d.client.ImageInspectWithRaw(ctx, imageName)
	if err == nil {
		return nil // image exists
	}

	// Retry image pull up to 2 times
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		log.Printf("[image] pulling %s (attempt %d)...", imageName, attempt)
		reader, pullErr := d.client.ImagePull(ctx, imageName, image.PullOptions{})
		if pullErr != nil {
			lastErr = pullErr
			log.Printf("[image] pull attempt %d failed: %v", attempt, pullErr)
			continue
		}

		// Read the pull output (needed to complete the pull)
		buf := make([]byte, 4096)
		for {
			_, readErr := reader.Read(buf)
			if readErr != nil {
				break
			}
		}
		reader.Close()

		log.Printf("[image] pulled %s", imageName)
		return nil
	}

	return fmt.Errorf("image %s not available (pull failed after 2 attempts: %w) — make sure the Docker image is built locally", imageName, lastErr)
}

// createCompileWrapper generates a shell script that compiles and runs code for compiled languages.
func (d *DockerClient) createCompileWrapper(codeDir, lang, mainFile string) (string, error) {
	var script string
	binary := "/tmp/a.out"

	switch strings.ToLower(lang) {
	case "c":
		script = fmt.Sprintf("#!/bin/sh\ngcc -o %s /code/*.c && chmod +x %s && %s\n", binary, binary, binary)
	case "cpp", "c++":
		script = fmt.Sprintf("#!/bin/sh\ng++ -o %s /code/*.cpp -std=c++17 && chmod +x %s && %s\n", binary, binary, binary)
	case "go", "golang":
		goModPath := filepath.Join(codeDir, "go.mod")
		if _, err := os.Stat(goModPath); os.IsNotExist(err) {
			os.WriteFile(goModPath, []byte("module main\n\ngo 1.22\n"), 0644)
		}
		script = "#!/bin/sh\ncd /code && go run .\n"
	case "java":
		classFile := strings.TrimSuffix(mainFile, ".java")
		script = fmt.Sprintf("#!/bin/sh\ncd /code && javac *.java && java %s\n", classFile)
	case "rust", "rs":
		binary = "/tmp/main"
		script = fmt.Sprintf("#!/bin/sh\ncd /code && rustc %s -o %s && %s\n", mainFile, binary, binary)
	default:
		return "", fmt.Errorf("unsupported compiled language: %s", lang)
	}

	wrapperPath := filepath.Join(codeDir, "run.sh")
	if err := os.WriteFile(wrapperPath, []byte(script), 0755); err != nil {
		return "", err
	}
	return wrapperPath, nil
}

// isCompiled returns true if the language requires a compile step.
func isCompiled(lang string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	switch lang {
	case "c", "cpp", "c++", "go", "golang", "java", "rust", "rs":
		return true
	default:
		return false
	}
}

// shouldSkipFile returns true for files that should never appear in the IDE file tree:
// dotfiles, shell history/config, compiled artifacts, and build tool noise.
func shouldSkipFile(relPath string) bool {
	// Get just the filename (last segment)
	base := filepath.Base(relPath)

	// All dotfiles (hidden files) — .ash_history, .bash_history, .profile, .bashrc, etc.
	if strings.HasPrefix(base, ".") {
		return true
	}

	// Compiled object / bytecode files
	switch filepath.Ext(base) {
	case ".o", ".a", ".so", ".pyc", ".pyo", ".class":
		return true
	}

	// Files inside hidden or build-artifact directories anywhere in the path
	return shouldSkipDir(filepath.Dir(relPath))
}

// shouldSkipDir returns true for directories that should never appear in the IDE file tree.
func shouldSkipDir(relPath string) bool {
	// Check every segment of the path
	for _, segment := range strings.Split(relPath, "/") {
		if segment == "" || segment == "." {
			continue
		}
		// Hidden directories
		if strings.HasPrefix(segment, ".") {
			return true
		}
		// Well-known build / dependency dirs
		switch segment {
		case "__pycache__", "node_modules", "target", "vendor", "dist", "build", ".git":
			return true
		}
	}
	return false
}
