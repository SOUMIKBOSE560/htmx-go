// Package selfping keeps a hosting platform (e.g. a Hugging Face Space,
// which sleeps after inactivity) warm by GET-ing the app's own public URL
// on a fixed interval. Traffic goes through the platform's public proxy, so
// each tick counts as incoming activity.
//
// The scheduler is opt-in: see SELF_PING_ENABLED / SELF_PING_URL /
// SELF_PING_INTERVAL in internal/config. A failed ping is logged and the
// schedule continues; Run returns only when ctx is cancelled.
package selfping

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// requestTimeout bounds a single ping so one slow probe can never overlap
// the next tick.
const requestTimeout = 15 * time.Second

// Run pings url every interval until ctx is cancelled. The first probe fires
// after one interval (not immediately) so the server is listening by then.
func Run(ctx context.Context, log *slog.Logger, url string, interval time.Duration) {
	if url == "" {
		log.Warn("self-ping idle: no URL configured")
		return
	}
	if interval <= 0 {
		log.Warn("self-ping idle: non-positive interval", "interval", interval)
		return
	}
	log.Info("self-ping scheduler armed", "url", url, "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Info("self-ping scheduler stopped")
			return
		case at := <-ticker.C:
			pingOnce(log, url, at)
		}
	}
}

// pingOnce performs a single GET and logs the outcome (status only, the body
// is discarded). It is a separate function so tests can drive it directly.
func pingOnce(log *slog.Logger, url string, at time.Time) {
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		log.Error("self-ping request build failed", "error", err)
		return
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	latency := time.Since(start)
	if err != nil {
		log.Warn("self-ping failed", "url", url, "latency", latency, "error", err)
		return
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Warn("self-ping unhealthy", "url", url, "status", resp.StatusCode, "latency", latency, "tick", at.UTC().Format(time.RFC3339))
		return
	}
	log.Info("self-ping ok", "url", url, "status", resp.StatusCode, "latency", latency)
}
