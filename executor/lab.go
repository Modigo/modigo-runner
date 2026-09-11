package executor

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// LabTarget describes a target container to spin up alongside the student's code container.
type LabTarget struct {
	// Type is the target type: "vulnerable-web", "vulnerable-api", "tcp-server", "custom"
	Type string `json:"type"`
	// Image overrides the default image for this target type. If empty, uses the default.
	Image string `json:"image,omitempty"`
	// Port is the port the target listens on inside its container.
	Port int `json:"port,omitempty"`
	// Env is additional environment variables for the target container.
	Env []string `json:"env,omitempty"`
}

// LabSession represents a running lab: an isolated Docker network with a student
// container and one or more target containers connected to it.
type LabSession struct {
	SessionID      string
	NetworkID      string
	StudentID      string   // student container ID
	TargetIDs      []string // target container IDs
	TargetHostname string   // DNS name the student uses to reach the target (e.g. "target")
	ExpiresAt      time.Time
	cancel         context.CancelFunc
}

// LabManager manages isolated lab sessions with student↔target networking.
type LabManager struct {
	client       *client.Client
	imagePrefix  string
	networkCount int64
}

// NewLabManager creates a new lab manager connected to the Docker daemon.
func NewLabManager(socketPath, imagePrefix string) (*LabManager, error) {
	cli, err := client.NewClientWithOpts(
		client.WithHost("unix://"+socketPath),
		client.WithAPIVersionNegotiation(),
	)
	if err != nil {
		return nil, fmt.Errorf("lab manager docker client: %w", err)
	}
	return &LabManager{
		client:      cli,
		imagePrefix: imagePrefix,
	}, nil
}

// StartLab creates an isolated Docker network, spins up a target container,
// and returns the session info. The student container is added later by the
// WebSocket handler (it creates the student container on this network).
//
// Flow:
//  1. Create Docker network "lab-{sessionID}"
//  2. Start target container on that network with hostname "target"
//  3. Spawn a background goroutine that kills the lab when timeout elapses
//  4. Return LabSession — caller adds the student container to the network
func (lm *LabManager) StartLab(ctx context.Context, sessionID string, target LabTarget, timeout time.Duration) (*LabSession, error) {
	// 1. Create isolated network
	networkName := fmt.Sprintf("lab-%s", sessionID)
	netResp, err := lm.client.NetworkCreate(ctx, networkName, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"modigo-managed": "true",
			"modigo-session": sessionID,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("create lab network: %w", err)
	}
	log.Printf("[lab] network %s created (%s)", networkName, netResp.ID[:12])

	// cancelCtx is cancelled when the lab expires or is manually stopped,
	// whichever comes first.  StopLab calls lab.cancel() to cancel early.
	labCtx, labCancel := context.WithCancel(context.Background())

	lab := &LabSession{
		SessionID:      sessionID,
		NetworkID:      netResp.ID,
		TargetHostname: "target",
		ExpiresAt:      time.Now().Add(timeout),
		cancel:         labCancel,
	}

	// 2. Start target container
	targetImage := lm.TargetImage(target.Type, target.Image)
	targetPort := lm.TargetPort(target.Type, target.Port)

	targetCfg := &container.Config{
		Image: targetImage,
		Tty:   false,
		Labels: map[string]string{
			"modigo-managed": "true",
			"modigo-session": sessionID,
			"modigo-role":    "target",
		},
	}
	if len(target.Env) > 0 {
		targetCfg.Env = target.Env
	}

	targetHostCfg := &container.HostConfig{
		NetworkMode:   container.NetworkMode("default"),
		Privileged:    false,
		SecurityOpt:   []string{"no-new-privileges"},
		RestartPolicy: container.RestartPolicy{Name: "no"},
		PortBindings:  nil, // no host port bindings — only accessible within the lab network
	}

	targetResp, err := lm.client.ContainerCreate(ctx, targetCfg, targetHostCfg, nil, nil, "")
	if err != nil {
		labCancel()
		lm.destroyNetwork(ctx, netResp.ID)
		return nil, fmt.Errorf("create target container: %w", err)
	}
	lab.TargetIDs = append(lab.TargetIDs, targetResp.ID)

	// Connect target to network with DNS alias "target"
	// Database targets also get the alias "db" so student code can use
	// standard connection strings like postgres://student:student@db:5432/modigo
	aliases := []string{"target"}
	switch strings.ToLower(target.Type) {
	case "postgres", "postgresql", "mysql":
		aliases = append(aliases, "db")
	}
	if err := lm.client.NetworkConnect(ctx, netResp.ID, targetResp.ID, &network.EndpointSettings{
		Aliases: aliases,
	}); err != nil {
		labCancel()
		lm.destroyNetwork(ctx, netResp.ID)
		return nil, fmt.Errorf("connect target to lab network: %w", err)
	}

	// Start the target container
	if err := lm.client.ContainerStart(ctx, targetResp.ID, container.StartOptions{}); err != nil {
		labCancel()
		lm.destroyNetwork(ctx, netResp.ID)
		return nil, fmt.Errorf("start target container: %w", err)
	}

	log.Printf("[lab] target %s started on network %s (image=%s, port=%d, expires=%s)",
		targetResp.ID[:12], networkName, targetImage, targetPort, lab.ExpiresAt.Format(time.RFC3339))

	// 3. Background expiry goroutine — fires when timeout elapses OR when
	//    StopLab cancels labCtx (whichever comes first).
	go func() {
		select {
		case <-time.After(timeout):
			log.Printf("[lab] session %s expired after %s — cleaning up", sessionID, timeout)
			lm.StopLab(context.Background(), lab)
		case <-labCtx.Done():
			// Cancelled early by StopLab — cleanup already handled there.
		}
	}()

	return lab, nil
}

// AddStudentContainer connects an existing student container to the lab network.
// The student container can then reach the target at "http://target:<port>".
func (lm *LabManager) AddStudentContainer(ctx context.Context, lab *LabSession, studentContainerID string) error {
	if lab == nil {
		return nil
	}

	// Connect student container to the lab network
	if err := lm.client.NetworkConnect(ctx, lab.NetworkID, studentContainerID, &network.EndpointSettings{
		Aliases: []string{"student"},
	}); err != nil {
		return fmt.Errorf("connect student to lab network: %w", err)
	}

	lab.StudentID = studentContainerID
	log.Printf("[lab] student %s connected to network %s", studentContainerID[:12], lab.NetworkID[:12])
	return nil
}

// StopLab destroys all containers and the network for a lab session.
// It also cancels the expiry goroutine so it doesn't fire after manual cleanup.
func (lm *LabManager) StopLab(ctx context.Context, lab *LabSession) {
	if lab == nil {
		return
	}

	// Cancel the expiry goroutine — safe to call multiple times.
	if lab.cancel != nil {
		lab.cancel()
	}

	// Remove student container (caller handles PTY cleanup)
	if lab.StudentID != "" {
		lm.removeContainer(ctx, lab.StudentID)
	}

	// Remove target containers
	for _, id := range lab.TargetIDs {
		lm.removeContainer(ctx, id)
	}

	// Remove the network (this also disconnects any remaining containers)
	lm.destroyNetwork(ctx, lab.NetworkID)

	log.Printf("[lab] session %s destroyed (network=%s, targets=%d)",
		lab.SessionID, lab.NetworkID[:12], len(lab.TargetIDs))
}

// CleanupExpiredLab is called by the kill timer when a lab session expires.
func (lm *LabManager) CleanupExpiredLab(ctx context.Context, lab *LabSession) {
	if time.Now().Before(lab.ExpiresAt) {
		return
	}
	log.Printf("[lab] session %s expired, cleaning up", lab.SessionID)
	lm.StopLab(ctx, lab)
}

// CleanupOrphanedLabs removes Docker networks and containers from crashed lab sessions.
func (lm *LabManager) CleanupOrphanedLabs(ctx context.Context) {
	// Find all modigo-managed networks
	networks, err := lm.client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("label", "modigo-managed=true")),
	})
	if err != nil {
		log.Printf("[lab] failed to list orphaned labs: %v", err)
		return
	}

	for _, net := range networks {
		log.Printf("[lab] removing orphaned lab network %s (%s)", net.Name, net.ID[:12])
		lm.destroyNetwork(ctx, net.ID)
	}
}

func (lm *LabManager) destroyNetwork(ctx context.Context, networkID string) {
	if err := lm.client.NetworkRemove(ctx, networkID); err != nil {
		log.Printf("[lab] failed to remove network %s: %v", networkID[:12], err)
	} else {
		log.Printf("[lab] network %s removed", networkID[:12])
	}
}

func (lm *LabManager) removeContainer(ctx context.Context, containerID string) {
	opts := container.RemoveOptions{Force: true, RemoveVolumes: true}
	if err := lm.client.ContainerRemove(ctx, containerID, opts); err != nil {
		if !client.IsErrNotFound(err) {
			log.Printf("[lab] failed to remove container %s: %v", containerID[:12], err)
		}
	} else {
		log.Printf("[lab] container %s removed", containerID[:12])
	}
}

// targetImage returns the Docker image for a target type.
func (lm *LabManager) TargetImage(targetType, override string) string {
	if override != "" {
		return override
	}
	switch strings.ToLower(targetType) {
	case "vulnerable-web", "web":
		return lm.imagePrefix + "target-web"
	case "vulnerable-api", "api":
		return lm.imagePrefix + "target-api"
	case "tcp-server", "tcp":
		return lm.imagePrefix + "target-tcp"
	// Database targets
	case "postgres", "postgresql":
		return lm.imagePrefix + "target-postgres"
	case "mysql":
		return lm.imagePrefix + "target-mysql"
	default:
		return lm.imagePrefix + "target-" + targetType
	}
}

// targetPort returns the default port for a target type.
func (lm *LabManager) TargetPort(targetType string, override int) int {
	if override > 0 {
		return override
	}
	switch strings.ToLower(targetType) {
	case "vulnerable-web", "web":
		return 80
	case "vulnerable-api", "api":
		return 3000
	case "tcp-server", "tcp":
		return 9000
	// Database targets
	case "postgres", "postgresql":
		return 5432
	case "mysql":
		return 3306
	default:
		return 8080
	}
}

// LabTargetImageMap maps target types to their default ports (for documentation).
var LabTargetImageMap = map[string]int{
	"vulnerable-web": 80,
	"vulnerable-api": 3000,
	"tcp-server":     9000,
	"postgres":       5432,
	"mysql":          3306,
}
