package web

import (
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"pageturner/internal/config"
	"pageturner/internal/logs"
	"pageturner/internal/models"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

func testServerNoDB(t *testing.T) *Server {
	t.Helper()
	cfg := config.Load()
	cfg.DatabaseURL = "file:/path-that-does-not-exist/pageturner.db?mode=ro"

	db, err := sql.Open("sqlite", cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewServer(cfg, models.NewStore(db), testLogger(), logs.NewHub(200))
}

// TestHealthUnreachable verifies /healthz reports 503 when the DB is down.
func TestHealthUnreachable(t *testing.T) {
	srv := testServerNoDB(t)
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("GET /healthz with DB down = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if !strings.Contains(rec.Body.String(), "unhealthy") {
		t.Fatalf("body = %q, want unhealthy JSON", rec.Body.String())
	}
}

// TestLoginPageRenders verifies the public login page renders the CSRF form.
func TestLoginPageRenders(t *testing.T) {
	srv := testServerNoDB(t)
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Welcome back.") {
		t.Fatal("login page missing heading")
	}
	if !strings.Contains(body, `name="csrf"`) {
		t.Fatal("login page missing CSRF field")
	}
	if !strings.Contains(body, "/static/htmx.min.js") {
		t.Fatal("login page missing vendored htmx script")
	}
}

// TestHomeRenders verifies the public landing page renders without a database.
func TestHomeRenders(t *testing.T) {
	srv := testServerNoDB(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Markitdown") {
		t.Fatal("home page missing brand")
	}
	if !strings.Contains(body, "/markitdown") {
		t.Fatal("home page missing converter link")
	}
}

// TestMarkitdownRequiresAuth verifies /markitdown redirects anonymous users.
func TestMarkitdownRequiresAuth(t *testing.T) {
	srv := testServerNoDB(t)
	req := httptest.NewRequest(http.MethodGet, "/markitdown", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("GET /markitdown anonymous = %d, want 303 to /login", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "/login" {
		t.Fatalf("redirect = %q, want /login", loc)
	}
}

// TestMarkitdownStreamRequiresAuth verifies the SSE endpoint rejects anonymous users.
func TestMarkitdownStreamRequiresAuth(t *testing.T) {
	srv := testServerNoDB(t)
	req := httptest.NewRequest(http.MethodGet, "/markitdown/stream?text=hello", nil)
	req.Header.Set("HX-Request", "true")
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("GET /markitdown/stream anonymous = %d, want 401", rec.Code)
	}
}

// TestDummyInvoiceMarkdown verifies the dummy extraction always yields an invoice doc.
func TestDummyInvoiceMarkdown(t *testing.T) {
	md := dummyInvoiceMarkdown("ACME billed Globex $10")
	for _, want := range []string{"# INVOICE", "| Item |", "Total due"} {
		if !strings.Contains(md, want) {
			t.Fatalf("invoice markdown missing %q:\n%s", want, md)
		}
	}
	if got := dummyInvoiceMarkdown(""); !strings.Contains(got, "# INVOICE") {
		t.Fatal("empty input should still yield an invoice doc")
	}
}
