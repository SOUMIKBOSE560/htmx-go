package logs

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestHubHistoryBounded(t *testing.T) {
	h := NewHub(3)
	for i := 0; i < 5; i++ {
		h.Publish("line-" + string(rune('a'+i)))
	}
	got := h.History()
	want := []string{"line-c", "line-d", "line-e"}
	if len(got) != len(want) {
		t.Fatalf("History() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("History() = %v, want %v", got, want)
		}
	}
}

func TestHubSubscribeReceivesAndCloses(t *testing.T) {
	h := NewHub(10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch := h.Subscribe(ctx)
	h.Publish("hello")

	select {
	case line := <-ch:
		if line != "hello" {
			t.Fatalf("got %q, want hello", line)
		}
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive published line")
	}

	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel should be closed after context cancel")
		}
	case <-time.After(time.Second):
		t.Fatal("channel not closed after context cancel")
	}
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := NewHub(10)
	// A subscriber that never reads: Publish must still return promptly.
	h.Subscribe(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 1000; i++ {
			h.Publish("x")
		}
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Publish blocked on a slow subscriber")
	}
}

func TestStreamHandlerPublishesAndForwards(t *testing.T) {
	h := NewHub(10)
	var buf strings.Builder
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	logger := slog.New(NewStreamHandler(inner, h))

	logger.Info("database ready", "synced_at", 180820260136)

	history := h.History()
	if len(history) != 1 {
		t.Fatalf("hub history = %v, want 1 line", history)
	}
	line := history[0]
	if !strings.Contains(line, "INFO") || !strings.Contains(line, "database ready") {
		t.Fatalf("stream line = %q, want INFO database ready", line)
	}
	if !strings.Contains(line, "synced_at=180820260136") {
		t.Fatalf("stream line = %q, want synced_at attr", line)
	}
	if buf.Len() == 0 {
		t.Fatal("inner handler did not receive the record")
	}
}
