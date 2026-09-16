package executor

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSemaphore_BasicAcquireRelease(t *testing.T) {
	sem := NewSemaphore(3)

	sem.Acquire()
	sem.Acquire()
	sem.Acquire()

	if sem.Current() != 3 {
		t.Fatalf("expected 3 current, got %d", sem.Current())
	}
	if sem.Available() != 0 {
		t.Fatalf("expected 0 available, got %d", sem.Available())
	}

	sem.Release()

	if sem.Current() != 2 {
		t.Fatalf("expected 2 current after release, got %d", sem.Current())
	}
	if sem.Available() != 1 {
		t.Fatalf("expected 1 available after release, got %d", sem.Available())
	}
}

func TestSemaphore_AcquireContext_deadline(t *testing.T) {
	sem := NewSemaphore(1)
	sem.Acquire() // fill the only slot

	// At capacity: a bounded acquire must return false after the deadline,
	// not block forever (Cloudflare 524 protection).
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if got := sem.AcquireContext(ctx); got {
		t.Fatal("expected AcquireContext to fail at capacity after deadline")
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("AcquireContext waited too long: %v", elapsed)
	}

	// Free the slot, then the acquire must succeed immediately.
	sem.Release()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel2()
	if !sem.AcquireContext(ctx2) {
		t.Fatal("expected AcquireContext to succeed when a slot is free")
	}
	sem.Release()

	// A cancelled context must abort even while waiting.
	sem.Acquire()
	ctx3, cancel3 := context.WithCancel(context.Background())
	cancel3()
	if got := sem.AcquireContext(ctx3); got {
		sem.Release()
		t.Fatal("expected AcquireContext to fail on a cancelled context")
	}
	sem.Release()
}

func TestSemaphore_blocksAtCapacity(t *testing.T) {
	sem := NewSemaphore(2)

	sem.Acquire()
	sem.Acquire()

	acquired := make(chan struct{})
	go func() {
		sem.Acquire() // Should block
		close(acquired)
		sem.Release()
	}()

	select {
	case <-acquired:
		t.Fatal("expected Acquire to block at capacity")
	case <-time.After(100 * time.Millisecond):
		// Good — it's blocking
	}

	sem.Release() // Free a slot

	select {
	case <-acquired:
		// Good — it unblocked
	case <-time.After(1 * time.Second):
		t.Fatal("expected Acquire to unblock after Release")
	}
}

func TestSemaphore_ConcurrentAccess(t *testing.T) {
	sem := NewSemaphore(10)
	var maxConcurrent int64
	var current int64

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem.Acquire()
			defer sem.Release()

			c := atomic.AddInt64(&current, 1)
			// Track max concurrent
			for {
				old := atomic.LoadInt64(&maxConcurrent)
				if c <= old || atomic.CompareAndSwapInt64(&maxConcurrent, old, c) {
					break
				}
			}
			time.Sleep(10 * time.Millisecond)
			atomic.AddInt64(&current, -1)
		}()
	}

	wg.Wait()

	if maxConcurrent > 10 {
		t.Fatalf("max concurrent %d exceeded limit of 10", maxConcurrent)
	}
	if maxConcurrent < 1 {
		t.Fatal("expected at least 1 concurrent access")
	}
}

func TestSemaphore_QueueLen(t *testing.T) {
	sem := NewSemaphore(1)
	sem.Acquire()

	var wg sync.WaitGroup
	var queueLen int

	wg.Add(1)
	go func() {
		defer wg.Done()
		sem.Acquire() // This blocks
		sem.Release()
	}()

	// Wait for the goroutine to start waiting
	time.Sleep(100 * time.Millisecond)
	queueLen = sem.QueueLen()

	if queueLen != 1 {
		t.Fatalf("expected queue length 1, got %d", queueLen)
	}

	sem.Release()
	wg.Wait()
}
