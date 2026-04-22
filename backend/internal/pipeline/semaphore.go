package pipeline

import (
	"context"
	"fmt"
)

// Semaphore is a counting semaphore backed by a buffered channel.
// It enforces a global cap on concurrent LLM calls across all pipeline stages.
// One instance should be created at server startup and shared across all stages.
type Semaphore struct {
	slots chan struct{}
	cap   int
}

// NewSemaphore creates a Semaphore with the given capacity. Panics if cap <= 0.
func NewSemaphore(cap int) *Semaphore {
	if cap <= 0 {
		panic(fmt.Sprintf("pipeline: semaphore cap must be > 0, got %d", cap))
	}
	s := &Semaphore{
		slots: make(chan struct{}, cap),
		cap:   cap,
	}
	for i := 0; i < cap; i++ {
		s.slots <- struct{}{}
	}
	return s
}

// Acquire blocks until a slot is available or ctx is cancelled.
// Returns ctx.Err() if the context is cancelled before a slot is obtained.
func (s *Semaphore) Acquire(ctx context.Context) error {
	select {
	case <-s.slots:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Release returns a slot to the semaphore. Must be called once per successful Acquire.
// Panics if called more times than Acquire, preventing a silent deadlock.
func (s *Semaphore) Release() {
	select {
	case s.slots <- struct{}{}:
	default:
		panic("pipeline: semaphore Release called without a matching Acquire")
	}
}

// Cap returns the total capacity of the semaphore.
func (s *Semaphore) Cap() int {
	return s.cap
}
