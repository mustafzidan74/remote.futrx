//go:build !windows

package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	servicedrain "github.com/futrx-com/remote.futrx.com/internal/service/drain"
)

// fakeRuns reports one run in flight until finish is called.
type fakeRuns struct{ active atomic.Int32 }

func (f *fakeRuns) ActiveRuns() int { return int(f.active.Load()) }

func (f *fakeRuns) WaitIdle(ctx context.Context) error {
	for f.active.Load() > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
	return nil
}

func TestSIGTERMWaitsForRunsInFlightThenStopsServing(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})}
	served := make(chan error, 1)
	go func() { served <- server.Serve(listener) }()

	gate := servicedrain.NewGate(nil)
	runs := &fakeRuns{}
	runs.active.Store(1)
	go drainOnSignal(server, gate, runs, time.Minute)
	time.Sleep(100 * time.Millisecond) // let signal.Notify register

	if err := syscall.Kill(syscall.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !gate.Draining() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if !gate.Blocked() {
		t.Fatal("new runs were not refused after SIGTERM")
	}

	// Still serving while the run finishes, so its watchers see the end.
	time.Sleep(300 * time.Millisecond)
	response, err := http.Get("http://" + listener.Addr().String())
	if err != nil {
		t.Fatalf("server stopped serving before the run finished: %v", err)
	}
	response.Body.Close()
	select {
	case err := <-served:
		t.Fatalf("server shut down with a run in flight: %v", err)
	default:
	}

	runs.active.Store(0)
	select {
	case err := <-served:
		if !errors.Is(err, http.ErrServerClosed) {
			t.Fatalf("serve ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server kept running after the last run finished")
	}
}
