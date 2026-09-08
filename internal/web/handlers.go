package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"pageturner/internal/auth"
	"pageturner/internal/models"
)

// pageData carries everything templates need.
type pageData struct {
	User  models.User
	CSRF  string
	Error string
	Email string
	// Bare renders the page chromeless (no nav, no footer) for full-viewport
	// app screens like /markitdown.
	Bare bool
}

// --- Health ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.store.Ping(ctx); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"status": "unhealthy", "error": err.Error()}) //nolint:errcheck
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"}) //nolint:errcheck
}

// --- Home (public landing) ---

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	render(w, r, "home", pageData{User: userFrom(r), CSRF: csrfFrom(r)})
}

// --- Auth ---

func (s *Server) handleLoginGet(w http.ResponseWriter, r *http.Request) {
	if _, err := r.Cookie("session"); err == nil {
		http.Redirect(w, r, "/markitdown", http.StatusSeeOther)
		return
	}
	render(w, r, "login", pageData{CSRF: csrfFrom(r)})
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")

	user, err := s.store.UserByEmail(r.Context(), strings.ToLower(email))
	if err != nil || !auth.CheckPassword(user.PasswordHash, password) {
		render(w, r, "login", pageData{CSRF: csrfFrom(r), Email: email, Error: "Invalid email or password."})
		return
	}
	s.startSession(w, r, user)
	http.Redirect(w, r, "/markitdown", http.StatusSeeOther)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("session"); err == nil {
		_ = s.store.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	http.SetCookie(w, s.sessionCookie("", -1))
	http.SetCookie(w, s.csrfCookie("", -1))
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// startSession creates a DB session, rotates the CSRF token, and sets cookies.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, user models.User) {
	token, err := auth.NewToken()
	if err != nil {
		render(w, r, "login", pageData{CSRF: csrfFrom(r), Email: user.Email, Error: "Could not create session. Please try again."})
		return
	}
	if err := s.store.CreateSession(r.Context(), auth.HashToken(token), user.ID, s.cfg.SessionTTL); err != nil {
		render(w, r, "login", pageData{CSRF: csrfFrom(r), Email: user.Email, Error: "Could not create session. Please try again."})
		return
	}
	http.SetCookie(w, s.sessionCookie(token, s.ttlSeconds()))

	csrf := newCSRFOrEmpty()
	http.SetCookie(w, s.csrfCookie(csrf, s.ttlSeconds()))
}

// --- Markitdown (authenticated) ---

func (s *Server) handleMarkitdown(w http.ResponseWriter, r *http.Request) {
	user, ok := s.requireUser(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	render(w, r, "markitdown", pageData{User: user, CSRF: csrfFrom(r), Bare: true})
}

// sseChunk mimics one OpenAI chat-completion streaming delta.
type sseChunk struct {
	Delta string `json:"delta"`
	Done  bool   `json:"done"`
}

// handleMarkitdownStream streams a dummy "OpenAI" response as SSE: an invoice
// extracted as markdown, chunked word-by-word so the right panel can render
// it live like a PDF extraction.
func (s *Server) handleMarkitdownStream(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireUser(r); !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	rc := http.NewResponseController(w)
	_ = rc.SetWriteDeadline(time.Time{})

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	raw := strings.TrimSpace(r.URL.Query().Get("text"))
	md := dummyInvoiceMarkdown(raw)

	// Chunk by words to feel like token streaming.
	words := strings.SplitAfter(md, " ")
	flusher, _ := w.(http.Flusher)
	for _, wd := range words {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		payload, _ := json.Marshal(sseChunk{Delta: wd})
		fmt.Fprintf(w, "data: %s\n\n", payload)
		if flusher != nil {
			flusher.Flush()
		} else {
			_ = rc.Flush()
		}
		time.Sleep(45 * time.Millisecond)
	}
	done, _ := json.Marshal(sseChunk{Done: true})
	fmt.Fprintf(w, "data: %s\n\n", done)
	if flusher != nil {
		flusher.Flush()
	} else {
		_ = rc.Flush()
	}
}

// dummyInvoiceMarkdown builds a deterministic invoice-style markdown doc from
// whatever the user pasted, so the demo always shows a plausible extraction.
func dummyInvoiceMarkdown(raw string) string {
	source := strings.TrimSpace(raw)
	if len(source) > 140 {
		source = source[:140] + "…"
	}
	if source == "" {
		source = "Pasted PDF text"
	}
	lines := []string{
		"# INVOICE",
		"",
		"**Acme Corp.** · 548 Market St, San Francisco, CA",
		"Invoice **#INV-2026-0847** · Issued Aug 18, 2026 · Due Sep 01, 2026",
		"",
		"---",
		"",
		"## Bill to",
		"",
		"Globex Inc. — accounts-payable@globex.example",
		"",
		"> Source excerpt: \"" + source + "\"",
		"",
		"## Line items",
		"",
		"| Item | Qty | Unit | Amount |",
		"| --- | ---: | ---: | ---: |",
		"| Design system audit | 12 hrs | $150.00 | $1,800.00 |",
		"| Markitdown extraction API | 4,200 pages | $0.04 | $168.00 |",
		"| Priority support | 1 mo | $99.00 | $99.00 |",
		"",
		"## Summary",
		"",
		"- Subtotal: **$2,067.00**",
		"- Tax (8.5%): **$175.70**",
		"- **Total due: $2,242.70**",
		"",
		"---",
		"",
		"_Extracted with Markitdown · confidence 0.98 · 3 pages · model markitdown-dummy-1_",
		"",
	}
	return strings.Join(lines, "\n")
}
