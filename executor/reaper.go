package executor

import (
	"context"
	"log"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// zombieReapInterval is how often the background reaper scans for leaked containers.
const zombieReapInterval = 5 * time.Minute

// zombieMaxAge is the age at which a runner container is considered leaked.
// It must comfortably exceed the longest legitimate in-band lifetime:
//   - run containers: killed at TimeoutForLang (30s default, 120s cyber)
//   - shell containers: killed by the shell idle reaper after 10 min idle
//     (and reset on every keystroke, so an actively-typed shell can in theory
//     live as long as its WebSocket)
//   - WebSocket read deadline (90s) ends dead-connection sessions
//
// 20 minutes therefore only ever fires on genuinely leaked containers — e.g.
// a container created right before a process crash, where the in-band timers
// died with the process and startup cleanup only runs once at boot.
const zombieMaxAge = 20 * time.Minute

// StartZombieReaper runs CleanupOrphanedContainers-style removal in the
// background for the lifetime of the process, with a longer age threshold so
// it never touches in-flight sessions.
func StartZombieReaper(docker *DockerClient) {
	go func() {
		ticker := time.NewTicker(zombieReapInterval)
		defer ticker.Stop()
		for range ticker.C {
			reapZombieContainers(docker, zombieMaxAge)
		}
	}()
	log.Printf("[reaper] zombie container reaper started (interval %s, max age %s)", zombieReapInterval, zombieMaxAge)
}

// reapZombieContainers removes modigo-runner containers older than maxAge.
// A container that old cannot be legitimate: every path that creates one
// arms a kill timer well below this threshold.
func reapZombieContainers(docker *DockerClient, maxAge time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "modigo-runner")

	containers, err := docker.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		log.Printf("[reaper] failed to list containers: %v", err)
		return
	}

	removed := 0
	for _, c := range containers {
		created := time.Unix(c.Created, 0)
		if time.Since(created) < maxAge {
			continue
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		log.Printf("[reaper] removing leaked container %s (age %s exceeds max %s)", id, time.Since(created).Round(time.Second), maxAge)
		docker.Remove(ctx, c.ID)
		removed++
	}
	if removed > 0 {
		log.Printf("[reaper] removed %d leaked container(s)", removed)
	}
}
