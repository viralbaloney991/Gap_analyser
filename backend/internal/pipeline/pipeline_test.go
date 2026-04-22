package pipeline_test

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"coralogix-alert-analyzer/internal/pipeline"
)

func TestSemaphore_AcquireRelease(t *testing.T) {
	sem := pipeline.NewSemaphore(2)

	// Can acquire up to cap.
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := sem.Acquire(context.Background()); err != nil {
		t.Fatalf("second acquire: %v", err)
	}

	// Third acquire should block; release one slot and verify it unblocks.
	released := make(chan struct{})
	acquired := make(chan struct{})
	go func() {
		<-released
		sem.Release()
	}()
	go func() {
		if err := sem.Acquire(context.Background()); err != nil {
			t.Errorf("third acquire after release: %v", err)
		}
		close(acquired)
	}()

	// Give the goroutine time to block on Acquire.
	time.Sleep(20 * time.Millisecond)
	close(released) // triggers Release

	select {
	case <-acquired:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("third acquire did not unblock after Release")
	}
}

func TestSemaphore_ContextCancellation(t *testing.T) {
	sem := pipeline.NewSemaphore(1)
	// Drain the one slot.
	_ = sem.Acquire(context.Background())

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- sem.Acquire(ctx)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled ctx, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Errorf("expected context.Canceled, got %v", err)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Acquire did not return after ctx cancellation")
	}
}

func TestSemaphore_Cap(t *testing.T) {
	sem := pipeline.NewSemaphore(7)
	if sem.Cap() != 7 {
		t.Fatalf("Cap() = %d, want 7", sem.Cap())
	}
}

func TestRun_AdaptiveWorkerCount(t *testing.T) {
	cases := []struct {
		inputs  int
		batch   int
		cap     int
		wantMin int
		wantMax int
	}{
		{inputs: 5, batch: 10, cap: 20, wantMin: 2, wantMax: 2},    // floor: 5/10=0 → min 2
		{inputs: 50, batch: 10, cap: 20, wantMin: 5, wantMax: 5},   // 50/10 = 5
		{inputs: 200, batch: 10, cap: 20, wantMin: 20, wantMax: 20}, // cap: 200/10=20
		{inputs: 500, batch: 10, cap: 20, wantMin: 20, wantMax: 20}, // cap: 500/10=50 → capped at 20
	}
	for _, tc := range cases {
		sem := pipeline.NewSemaphore(tc.cap)
		var maxInFlight atomic.Int64
		var current atomic.Int64

		inputs := make([]int, tc.inputs)
		errs := pipeline.Run(context.Background(), sem, inputs, tc.batch, func(_ context.Context, _ int) error {
			cur := current.Add(1)
			if cur > maxInFlight.Load() {
				maxInFlight.Store(cur)
			}
			time.Sleep(5 * time.Millisecond)
			current.Add(-1)
			return nil
		})
		if len(errs) > 0 {
			t.Errorf("inputs=%d: unexpected errors: %v", tc.inputs, errs)
		}
		got := maxInFlight.Load()
		if got < int64(tc.wantMin) || got > int64(tc.wantMax) {
			t.Errorf("inputs=%d batch=%d cap=%d: max in-flight = %d, want [%d, %d]",
				tc.inputs, tc.batch, tc.cap, got, tc.wantMin, tc.wantMax)
		}
	}
}

func TestRun_ErrorCollection(t *testing.T) {
	sem := pipeline.NewSemaphore(5)
	inputs := []int{1, 2, 3, 4, 5}

	errs := pipeline.Run(context.Background(), sem, inputs, 1, func(_ context.Context, n int) error {
		if n%2 == 0 {
			return fmt.Errorf("even: %d", n)
		}
		return nil
	})
	// 2 and 4 fail → 2 errors
	if len(errs) != 2 {
		t.Fatalf("want 2 errors, got %d: %v", len(errs), errs)
	}
}

func TestRun_ConcurrencyBound(t *testing.T) {
	const cap = 4
	sem := pipeline.NewSemaphore(cap)

	var inFlight atomic.Int64
	inputs := make([]int, 40)

	errs := pipeline.Run(context.Background(), sem, inputs, 2, func(_ context.Context, _ int) error {
		cur := inFlight.Add(1)
		if cur > cap {
			t.Errorf("in-flight %d exceeds cap %d", cur, cap)
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
		return nil
	})
	if len(errs) > 0 {
		t.Errorf("unexpected errors: %v", errs)
	}
}

func TestRun_EmptyInputs(t *testing.T) {
	sem := pipeline.NewSemaphore(5)
	errs := pipeline.Run(context.Background(), sem, []int{}, 10, func(_ context.Context, _ int) error {
		t.Fatal("fn should not be called for empty inputs")
		return nil
	})
	if len(errs) != 0 {
		t.Fatalf("want no errors for empty inputs, got %v", errs)
	}
}
