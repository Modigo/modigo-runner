package executor

import (
	"context"
	"log"
	"sync"
	"time"
)

// warmedContainer is a pre-started container ready for instant use.
type warmedContainer struct {
	containerID string
	language    string
	createdAt   time.Time
	codeDir     string // temp directory for cleanup
}

// Pool manages pre-warmed Docker images for fast container startup.
// Actual containers are created on-demand (we pre-pull images instead).
type Pool struct {
	mu     sync.Mutex
	docker *DockerClient
	warm   map[string][]*warmedContainer
	size   int
	langs  []string
	done   chan struct{}
	ready  chan struct{}
	readyOnce sync.Once
}

// NewPool creates a new pool and starts background image pre-pulling.
func NewPool(docker *DockerClient, size int, langs []string) *Pool {
	p := &Pool{
		docker: docker,
		warm:   make(map[string][]*warmedContainer),
		size:   size,
		langs:  langs,
		done:   make(chan struct{}),
		ready:  make(chan struct{}),
	}
	go p.warmUpLoop()
	return p
}

// Get returns a pre-warmed container for the given language, or nil if none available.
func (p *Pool) Get(lang string) *warmedContainer {
	p.mu.Lock()
	defer p.mu.Unlock()

	q, ok := p.warm[lang]
	if !ok || len(q) == 0 {
		return nil
	}

	w := q[0]
	p.warm[lang] = q[1:]

	// Discard stale (> 5 min old)
	if time.Since(w.createdAt) > 5*time.Minute {
		log.Printf("[pool] discarding stale container %s (lang=%s)", w.containerID[:12], lang)
		go p.docker.Remove(context.Background(), w.containerID)
		return p.Get(lang)
	}

	log.Printf("[pool] served container %s for lang=%s (remaining: %d)", w.containerID[:12], lang, len(p.warm[lang]))
	return w
}

// Return discards a used container and triggers replenishment.
func (p *Pool) Return(w *warmedContainer) {
	go p.docker.Remove(context.Background(), w.containerID)
}

// Stop stops the background goroutine.
func (p *Pool) Stop() {
	close(p.done)
}

// Stats returns the current pool size per language.
func (p *Pool) Stats() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := make(map[string]int)
	for lang, q := range p.warm {
		stats[lang] = len(q)
	}
	return stats
}

// IsReady returns true if the pool has pre-pulled all images.
func (p *Pool) IsReady() bool {
	select {
	case <-p.ready:
		return true
	default:
		return false
	}
}

// warmUpLoop pre-pulls Docker images so the first container creation is fast.
func (p *Pool) warmUpLoop() {
	// Pre-pull all language images on startup
	for _, lang := range p.langs {
		image := p.docker.LanguageToImage(lang)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := p.docker.ensureImage(ctx, image); err != nil {
			log.Printf("[pool] image pull failed for %s: %v", lang, err)
		} else {
			log.Printf("[pool] image ready: %s", image)
		}
		cancel()
	}

	// Signal ready
	p.readyOnce.Do(func() { close(p.ready) })

	// Periodic check — re-pull if images were removed
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			for _, lang := range p.langs {
				image := p.docker.LanguageToImage(lang)
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				p.docker.ensureImage(ctx, image)
				cancel()
			}
		}
	}
}
