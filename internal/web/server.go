package web

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"pageturner/internal/auth"
	"pageturner/internal/config"
	"pageturner/internal/logs"
	"pageturner/internal/models"
)

type ctxKey int

const (
	userKey ctxKey = iota
	csrfKey
)

// Server wires routes, middleware and handlers together.
type Server struct {
	cfg     config.Config
	store   *models.Store
	handler http.Handler
	log     *slog.Logger
	hub     *logs.Hub
}

func NewServer(cfg config.Config, store *models.Store, log *slog.Logger, hub *logs.Hub) *Server {
	s := &Server{
		cfg:   cfg,
		store: store,
		log:   log,
		hub:   hub,
	}
	templateLog = log
	s.routes(http.NewServeMux())
	return s
}

func (s *Server) routes(mux *http.ServeMux) {
	// Public
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /{$}", s.handleHome)
	mux.HandleFunc("GET /login", s.handleLoginGet)
	mux.HandleFunc("POST /login", s.handleLoginPost)
	mux.Handle("GET /static/", http.StripPrefix("/static/", staticFiles()))

	// Authenticated
	mux.HandleFunc("POST /logout", s.handleLogout)
	mux.HandleFunc("GET /markitdown", s.handleMarkitdown)
	mux.HandleFunc("GET /markitdown/stream", s.handleMarkitdownStream)
	mux.HandleFunc("GET /logs/stream", s.handleLogStream)

	// Wrap the whole router; the last middleware listed runs first, so the
	// execution order is: requireAuth → csrf → requestLog → recoverPanic.
	s.handler = wrap(mux, s.recoverPanic, s.requestLog, s.csrf, s.requireAuth)
}

// Handler returns the fully wrapped http.Handler.
func (s *Server) Handler() http.Handler { return s.handler }

// wrap nests middlewares inside-out: each one wraps the previous result, so
// the last listed middleware runs first for every request.
func wrap(h http.Handler, mws ...func(http.Handler) http.Handler) http.Handler {
	for _, mw := range mws {
		h = mw(h)
	}
	return h
}

// --- context helpers ---

func contextWithUser(ctx context.Context, u models.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func userFrom(r *http.Request) models.User {
	u, _ := r.Context().Value(userKey).(models.User)
	return u
}

func contextWithCSRF(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, csrfKey, token)
}

func csrfFrom(r *http.Request) string {
	t, _ := r.Context().Value(csrfKey).(string)
	return t
}

// --- small helpers ---

func (s *Server) sessionCookie(token string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func (s *Server) csrfCookie(token string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     "csrf",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure(),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func (s *Server) ttlSeconds() int {
	return int(s.cfg.SessionTTL / time.Second)
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true"
}

func newCSRFOrEmpty() string {
	t, err := auth.NewCSRF()
	if err != nil {
		return ""
	}
	return t
}
