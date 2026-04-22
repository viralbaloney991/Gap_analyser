package pipeline

import (
	"context"
	"sync"
)

const minWorkers = 2

// Run fans out fn over inputs using an adaptive worker count derived from input size.
//
// Worker count formula: clamp(len(inputs)/batch, minWorkers=2, sem.Cap())
//
// Each worker acquires a semaphore slot before calling fn and releases it after.
// Errors are collected per-item; the batch never aborts on partial failure.
// Returns nil if inputs is empty.
func Run[T any](ctx context.Context, sem *Semaphore, inputs []T, batch int, fn func(context.Context, T) error) []error {
	if len(inputs) == 0 {
		return nil
	}
	if batch <= 0 {
		batch = 1
	}

	workers := len(inputs) / batch
	if workers < minWorkers {
		workers = minWorkers
	}
	if workers > sem.Cap() {
		workers = sem.Cap()
	}

	jobs := make(chan T, len(inputs))
	for _, inp := range inputs {
		jobs <- inp
	}
	close(jobs)

	var mu sync.Mutex
	var errs []error

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for inp := range jobs {
				if err := sem.Acquire(ctx); err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
					continue
				}
				err := fn(ctx, inp)
				sem.Release()
				if err != nil {
					mu.Lock()
					errs = append(errs, err)
					mu.Unlock()
				}
			}
		}()
	}
	wg.Wait()
	return errs
}
