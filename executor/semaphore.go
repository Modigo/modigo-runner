package executor

import (
	"context"
	"log"
	"sync"
	"time"
)

// Semaphore provides a concurrency limiter for Docker containers.
// At most maxConcurrent containers can run at the same time.
// Additional requests block until a slot opens up.
type Semaphore struct {
	mu       sync.Mutex
	cond     *sync.Cond
	current  int
	max      int
	queueLen int
}

// NewSemaphore creates a new semaphore with the given concurrency limit.
func NewSemaphore(max int) *Semaphore {
	s := &Semaphore{max: max}
	s.cond = sync.NewCond(&s.mu)
	return s
}

// Acquire blocks until a slot is available, then increments the counter.
func (s *Semaphore) Acquire() {
	s.mu.Lock()
	defer s.mu.Unlock()

	for s.current >= s.max {
		s.queueLen++
		log.Printf("[semaphore] at capacity (%d/%d), waiting... (queue: %d)", s.current, s.max, s.queueLen)
		s.cond.Wait()
		s.queueLen--
	}

	s.current++
	log.Printf("[semaphore] acquired slot (%d/%d)", s.current, s.max)
}

// AcquireContext blocks until a slot is available, the context is cancelled,
// or the context deadline expires. It reports whether a slot was acquired.
//
// Callers behind Cloudflare MUST bound their wait: Cloudflare kills any HTTP
// request that doesn't respond within ~100s with a 524, so queueing forever
// just converts overload into user-visible timeouts.
func (s *Semaphore) AcquireContext(ctx context.Context) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.current < s.max {
		s.current++
		log.Printf("[semaphore] acquired slot (%d/%d)", s.current, s.max)
		return true
	}

	// sync.Cond has no context awareness: a cancelled ctx is only observed
	// when the cond is woken. A ticker goroutine broadcasts periodically so
	// waiters re-check ctx.Err() at least twice a second.
	wake := make(chan struct{})
	defer close(wake)
	go func() {
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				s.mu.Lock()
				s.cond.Broadcast()
				s.mu.Unlock()
			case <-wake:
				return
			}
		}
	}()

	s.queueLen++
	log.Printf("[semaphore] at capacity (%d/%d), waiting with deadline... (queue: %d)", s.current, s.max, s.queueLen)
	defer func() { s.queueLen-- }()

	for s.current >= s.max {
		if ctx.Err() != nil {
			log.Printf("[semaphore] wait aborted: %v", ctx.Err())
			return false
		}
		s.cond.Wait()
	}

	s.current++
	log.Printf("[semaphore] acquired slot (%d/%d)", s.current, s.max)
	return true
}

// Release frees a slot and wakes one waiting goroutine.
func (s *Semaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.current--
	s.cond.Signal()
	log.Printf("[semaphore] released slot (%d/%d)", s.current, s.max)
}

// Current returns the number of active slots.
func (s *Semaphore) Current() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.current
}

// Available returns the number of free slots.
func (s *Semaphore) Available() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	free := s.max - s.current
	if free < 0 {
		return 0
	}
	return free
}

// QueueLen returns the number of waiting requests.
func (s *Semaphore) QueueLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queueLen
}
