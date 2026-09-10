package executor

import (
	"context"
	"log"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// CleanupOrphanedContainers removes containers left behind by a previous crash.
// On startup, we find all containers with our label and remove them — they belong
// to a dead process and will never be cleaned up otherwise.
func CleanupOrphanedContainers(docker *DockerClient) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Find all containers created by this runner (by image prefix)
	filterArgs := filters.NewArgs()
	filterArgs.Add("label", "modigo-runner")

	containers, err := docker.client.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filterArgs,
	})
	if err != nil {
		log.Printf("[cleanup] failed to list orphaned containers: %v", err)
		return
	}

	if len(containers) == 0 {
		log.Printf("[cleanup] no orphaned containers found")
		return
	}

	removed := 0
	for _, c := range containers {
		id := c.ID[:12]
		// Only remove containers that have been running for more than 2 minutes
		// (short-lived containers are normal — they're from in-progress runs)
		created := time.Unix(c.Created, 0)
		if time.Since(created) < 2*time.Minute {
			continue
		}

		log.Printf("[cleanup] removing orphaned container %s (age: %s)", id, time.Since(created).Round(time.Second))
		docker.Remove(ctx, c.ID)
		removed++
	}

	log.Printf("[cleanup] removed %d orphaned containers out of %d found", removed, len(containers))
}
