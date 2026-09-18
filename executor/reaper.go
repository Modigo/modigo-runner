package executor

import (
	"log"
	"time"
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
			cleanupAged(docker, zombieMaxAge, "reaper")
		}
	}()
	log.Printf("[reaper] zombie container reaper started (interval %s, max age %s)", zombieReapInterval, zombieMaxAge)
}
