package web

import (
	"net/http"
	"time"

	"pageturner/internal/auth"
	"pageturner/internal/models"
)

// statusRecorder captures the response status code for logging.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap exposes the underlying ResponseWriter so http.ResponseController
// (streaming flush, write-deadline control) works through the middleware.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// recoverPanic converts panics into 500s instead of crashing the process.
func (s *Server) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// requestLog emits one structured log line per request.
func (s *Server) requestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", r.RemoteAddr,
		)
	})
}

// csrf protects state-changing requests using the double-submit pattern: the
// token lives in the "csrf" cookie and must also arrive in the X-CSRF-Token
// header (set globally by htmx) or a "csrf" form field. For every request it
// also exposes the current token to handlers via the request context, so forms
// re-rendered after an error keep a valid token for the next submit.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			// Safe methods pass through, but make sure a token exists (creating
			// the cookie if needed) so the rendered page can put it in forms.
			token := csrfCookieValue(r)
			if token == "" {
				token = newCSRFOrEmpty()
				http.SetCookie(w, s.csrfCookie(token, s.ttlSeconds()))
			}
			next.ServeHTTP(w, r.WithContext(contextWithCSRF(r.Context(), token)))
			return
		}

		cookie, err := r.Cookie("csrf")
		if err != nil {
			http.Error(w, "missing CSRF token", http.StatusForbidden)
			return
		}
		submitted := r.Header.Get("X-CSRF-Token")
		if submitted == "" {
			submitted = r.FormValue("csrf")
		}
		if !auth.ConstantTimeEqual(cookie.Value, submitted) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		// Expose the validated token so error re-renders keep the same token
		// in the form instead of an empty one that would fail the next submit.
		next.ServeHTTP(w, r.WithContext(contextWithCSRF(r.Context(), cookie.Value)))
	})
}

// requireAuth loads the session user and attaches them to the request context.
// Public routes (home/login/health/static) pass through untouched.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// Public routes.
	switch {
	case r.URL.Path == "/" || r.URL.Path == "/login":
			next.ServeHTTP(w, r)
			return
		case r.URL.Path == "/healthz":
			next.ServeHTTP(w, r)
			return
		case len(r.URL.Path) >= 8 && r.URL.Path[:8] == "/static/":
			next.ServeHTTP(w, r)
			return
		}

		cookie, err := r.Cookie("session")
		if err != nil {
			s.redirectLogin(w, r)
			return
		}
		user, err := s.store.UserForSession(r.Context(), auth.HashToken(cookie.Value))
		if err != nil {
			s.redirectLogin(w, r)
			return
		}

		// The csrf middleware (which wraps this one) ensures the token cookie
		// exists and exposes it via context for every request.
		ctx := contextWithUser(r.Context(), user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// redirectLogin sends a normal or htmx-aware redirect to the login page.
func (s *Server) redirectLogin(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/login")
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// csrfCookieValue reads the csrf cookie value, if present.
func csrfCookieValue(r *http.Request) string {
	if c, err := r.Cookie("csrf"); err == nil && c.Value != "" {
		return c.Value
	}
	return ""
}

// requireUser returns the authenticated user, or nil if somehow absent.
func (s *Server) requireUser(r *http.Request) (models.User, bool) {
	u := userFrom(r)
	if u.ID == 0 {
		s.log.Warn("authenticated route reached without user in context", "path", r.URL.Path)
		return models.User{}, false
	}
	return u, true
}
