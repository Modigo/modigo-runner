package executor

import (
	"log"
	"sync"
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
