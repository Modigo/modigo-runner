package executor

import (
	"context"
	"log"
	"strings"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
)

// studentContainerFilter matches every container this runner family creates:
// new containers carry the "modigo-runner" label; legacy containers (created
// before labeling existed) are matched by their image name prefix, which lab
// target containers (target-web, target-api, ...) also share. Lab targets are
// excluded by name below, since they legitimately live up to 30 minutes.
func studentContainerFilter(prefix string) filters.Args {
	f := filters.NewArgs()
	f.Add("label", "modigo-runner")
	if prefix != "" {
		f.Add("ancestor", prefix+"python")
		f.Add("ancestor", prefix+"javascript")
		f.Add("ancestor", prefix+"c")
		f.Add("ancestor", prefix+"cpp")
		f.Add("ancestor", prefix+"go")
		f.Add("ancestor", prefix+"java")
		f.Add("ancestor", prefix+"rust")
		f.Add("ancestor", prefix+"php")
	}
	return f
}

// studentImageLangs are the language image suffixes appended to the image prefix.
var studentImageLangs = []string{"python", "javascript", "c", "cpp", "go", "java", "rust", "php"}

// listStudentContainers returns all containers matching the student filter.
// A single OR-group filter can't be expressed via the label+ancestor combo,
// so we query each ancestor separately and de-duplicate.
func listStudentContainers(ctx context.Context, docker *DockerClient, prefix string) ([]dockertypes.Container, error) {
	seen := make(map[string]bool)
	var all []dockertypes.Container

	list := func(f filters.Args) error {
		cs, err := docker.client.ContainerList(ctx, container.ListOptions{All: true, Filters: f})
		if err != nil {
			return err
		}
		for _, c := range cs {
			if !seen[c.ID] {
				seen[c.ID] = true
				all = append(all, c)
			}
		}
		return nil
	}

	labeled := filters.NewArgs()
	labeled.Add("label", "modigo-runner")
	if err := list(labeled); err != nil {
		return nil, err
	}
	if prefix != "" {
		for _, lang := range studentImageLangs {
			byImage := filters.NewArgs()
			byImage.Add("ancestor", prefix+lang)
			if err := list(byImage); err != nil {
				return nil, err
			}
		}
	}
	return all, nil
}

// isStudentContainerImage reports whether the container's image is a student
// runtime image (modigo-runner-<lang>) as opposed to a lab target image
// (modigo-runner-target-*). Used to protect live labs from cleanup.
func isStudentContainerImage(c dockertypes.Container, prefix string) bool {
	if c.Image == "" {
		return false
	}
	if prefix != "" && strings.HasPrefix(c.Image, prefix) {
		return !strings.HasPrefix(c.Image, prefix+"target-")
	}
	// No prefix configured: fall back to matching "target-" anywhere so lab
	// targets (e.g. ".../target-web") are still excluded.
	return !strings.Contains(c.Image, "target-")
}

// CleanupOrphanedContainers removes containers left behind by a previous crash.
// Runs once at startup: everything student-related still alive belongs to a
// dead process (or a previous one) and will never be cleaned up otherwise.
// Matches both the new "modigo-runner" label and legacy unlabeled containers
// via image prefix, so a pre-label deployment's leak is also swept.
func CleanupOrphanedContainers(docker *DockerClient) {
	cleanupAged(docker, 2*time.Minute, "cleanup")
}

// cleanupAged is the shared sweep used by both startup cleanup and the
// periodic zombie reaper. ageThreshold: only containers older than this are
// removed (short-lived containers are normal — they're in-progress runs).
// tag appears in log lines so startup sweeps vs periodic reaps are distinguishable.
func cleanupAged(docker *DockerClient, ageThreshold time.Duration, tag string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	containers, err := listStudentContainers(ctx, docker, docker.prefix)
	if err != nil {
		log.Printf("[%s] failed to list candidate containers: %v", tag, err)
		return
	}

	if len(containers) == 0 {
		log.Printf("[%s] no candidate containers found", tag)
		return
	}

	removed := 0
	for _, c := range containers {
		// Lab target containers share the image prefix — skip them; the lab
		// manager owns their lifecycle (up to 30 min legitimately).
		if !isStudentContainerImage(c, docker.prefix) {
			continue
		}
		created := time.Unix(c.Created, 0)
		if time.Since(created) < ageThreshold {
			continue
		}
		id := c.ID
		if len(id) > 12 {
			id = id[:12]
		}
		log.Printf("[%s] removing container %s (image %s, age %s > %s)", tag, id, c.Image, time.Since(created).Round(time.Second), ageThreshold)
		docker.Remove(ctx, c.ID)
		removed++
	}

	log.Printf("[%s] removed %d container(s) out of %d candidates", tag, removed, len(containers))
}
