package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// sseLogEvent is the JSON payload sent down the log stream.
type sseLogEvent struct {
	Text  string `json:"text"`
	Color string `json:"color"`
	Ts    int64  `json:"ts"`
}

// handleLogStream streams backend log lines to the browser over
// Server-Sent Events: history is replayed first, then new lines are pushed
// as they are emitted. Idle connections get a comment heartbeat so proxies
// don't close them.
func (s *Server) handleLogStream(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// The server's WriteTimeout would kill a long-lived stream; clear it.
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	s.log.Info("log stream opened")
	for _, line := range s.hub.History() {
		writeLogEvent(w, line)
	}
	if err := rc.Flush(); err != nil {
		s.log.Warn("log stream flush", "error", err)
		return
	}

	ch := s.hub.Subscribe(r.Context())
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case line := <-ch:
			writeLogEvent(w, line)
			_ = rc.Flush()
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			_ = rc.Flush()
		}
	}
}

// writeLogEvent sends one line as an SSE data frame.
func writeLogEvent(w http.ResponseWriter, line string) {
	event, _ := json.Marshal(sseLogEvent{Text: line, Color: classifyLogLine(line), Ts: time.Now().UnixMilli()})
	fmt.Fprintf(w, "data: %s\n\n", event)
}

// classifyLogLine picks a terminal color class from the line's level token.
func classifyLogLine(line string) string {
	switch {
	case strings.Contains(line, "ERROR"):
		return "error"
	case strings.Contains(line, "WARN"):
		return "warn"
	case strings.Contains(line, "DEBUG"):
		return "dim"
	default:
		return "default"
	}
}
