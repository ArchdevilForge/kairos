package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fakePipeline drives run() without a real engine.
type fakePipeline struct {
	startCh  chan error // Start blocks until it can read a result or ctx ends
	stopped  atomic.Int32
	closed   atomic.Int32
	stopFunc func(*fakePipeline)
}

func (f *fakePipeline) Start(ctx context.Context) error {
	select {
	case err := <-f.startCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakePipeline) Stop() {
	f.stopped.Add(1)
	if f.stopFunc != nil {
		f.stopFunc(f)
	}
}

func (f *fakePipeline) Close() { f.closed.Add(1) }

func TestRun_PipelineEarlyErrorExitsNonNil(t *testing.T) {
	f := &fakePipeline{startCh: make(chan error, 1)}
	wantErr := errors.New("ws feed dead")
	f.startCh <- wantErr

	err := run(context.Background(), f)
	if !errors.Is(err, wantErr) {
		t.Fatalf("run must surface early pipeline error, got %v", err)
	}
	if f.stopped.Load() == 0 || f.closed.Load() == 0 {
		t.Fatalf("stop/close not called: stopped=%d closed=%d", f.stopped.Load(), f.closed.Load())
	}
}

func TestRun_PipelineNilReturnIsStillFatal(t *testing.T) {
	f := &fakePipeline{startCh: make(chan error, 1)}
	f.startCh <- nil // pipeline "finished" cleanly while daemon should run forever

	err := run(context.Background(), f)
	if !errors.Is(err, errPipelineExited) {
		t.Fatalf("clean early exit must still be fatal for the daemon, got %v", err)
	}
}

func TestRun_SignalShutdownIsClean(t *testing.T) {
	f := &fakePipeline{startCh: make(chan error)}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- run(ctx, f) }()

	time.Sleep(10 * time.Millisecond) // let Start block
	cancel()                          // simulated SIGTERM

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("signal shutdown must be clean, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
	if f.stopped.Load() == 0 || f.closed.Load() == 0 {
		t.Fatalf("stop/close not called: stopped=%d closed=%d", f.stopped.Load(), f.closed.Load())
	}
}

func TestRun_StopOrderIsStopWaitClose(t *testing.T) {
	// Close must not run before Start has returned (exchanges are still in
	// use until then).
	f := &fakePipeline{startCh: make(chan error)}
	f.stopFunc = func(fp *fakePipeline) {
		// Simulate the pipeline unwinding after Stop.
		go func() {
			time.Sleep(20 * time.Millisecond)
			fp.startCh <- context.Canceled
		}()
	}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- run(ctx, f) }()
	time.Sleep(10 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("expected clean shutdown, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run hung")
	}
	if f.closed.Load() != 1 {
		t.Fatalf("close count: %d", f.closed.Load())
	}
}
