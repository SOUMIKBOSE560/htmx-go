package selfping

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPingOnceSuccess(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	pingOnce(testLogger(), srv.URL, time.Now())
	if hits.Load() != 1 {
		t.Fatalf("expected 1 hit, got %d", hits.Load())
	}
}

func TestPingOnceUnhealthyDoesNotPanic(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	// Must return (and log a warning) rather than propagate an error.
	pingOnce(testLogger(), srv.URL, time.Now())
}

func TestPingOnceBadURL(t *testing.T) {
	// Unreachable port: must log and return, never block past the timeout.
	pingOnce(testLogger(), "http://127.0.0.1:1/healthz", time.Now())
}

func TestRunTicksAndStops(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, testLogger(), srv.URL, 20*time.Millisecond)
	}()
	time.Sleep(120 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancel")
	}
	if hits.Load() == 0 {
		t.Fatal("expected at least one tick while context was live")
	}
}

func TestRunIdleOnEmptyURL(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		Run(ctx, testLogger(), "", time.Minute)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cancel()
		t.Fatal("Run with empty URL did not return immediately")
	}
	cancel()
}
