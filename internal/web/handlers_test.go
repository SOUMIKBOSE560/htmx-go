package web

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"pageturner/internal/config"
	"pageturner/internal/database"
	"pageturner/internal/logs"
	"pageturner/internal/models"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
}

// TestHealthUnreachable verifies /healthz reports 503 when the DB is down,
// which exercises the wiring without needing a live database. The pool is
// built directly (pgxpool defers connecting) to simulate a broken backend.
func TestHealthUnreachable(t *testing.T) {
	cfg := config.Load()
	cfg.DatabaseURL = "postgres://nobody:nothing@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1"

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(cfg, models.NewStore(pool), testLogger(), logs.NewHub(200))
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
	cfg := config.Load()
	pool, err := database.Connect(context.Background(), cfg.DatabaseURL)
	if err != nil {
		t.Skipf("no database available: %v", err)
	}
	defer pool.Close()

	srv := NewServer(cfg, models.NewStore(pool), testLogger(), logs.NewHub(200))
	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /login = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Welcome back") {
		t.Fatal("login page missing heading")
	}
	if !strings.Contains(body, `name="csrf"`) {
		t.Fatal("login page missing CSRF field")
	}
	if !strings.Contains(body, "/static/htmx.min.js") {
		t.Fatal("login page missing vendored htmx script")
	}
}

// TestFullFlow is a DB-backed integration test of the data layer, mirroring the
// real user journey. It runs only when TEST_DATABASE_URL is set, e.g.:
//
//	TEST_DATABASE_URL=postgres://pageturner:pageturner@localhost:5432/pageturner?sslmode=disable go test ./...
func TestFullFlow(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	ctx := context.Background()

	pool, err := database.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := database.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := models.NewStore(pool)
	stamp := time.Now().Format("150405.000000000")
	email := "it-" + stamp + "@test.dev"
	user, err := store.CreateUser(ctx, email, "fake-hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Session round-trip.
	tok := "tok-" + stamp
	if err := store.CreateSession(ctx, tok, user.ID, time.Hour); err != nil {
		t.Fatalf("create session: %v", err)
	}
	got, err := store.UserForSession(ctx, tok)
	if err != nil || got.ID != user.ID {
		t.Fatalf("UserForSession = (%v, %v), want user %d", got, err, user.ID)
	}

	// Books: create, list, update, stats.
	b1, err := store.CreateBook(ctx, user.ID, "The Name of the Wind", "Patrick Rothfuss", models.StatusReading)
	if err != nil {
		t.Fatalf("create book 1: %v", err)
	}
	if _, err := store.CreateBook(ctx, user.ID, "Dune", "Frank Herbert", models.StatusFinished); err != nil {
		t.Fatalf("create book 2: %v", err)
	}
	if err := store.UpdateBook(ctx, user.ID, b1.ID, "The Name of the Wind", "Patrick Rothfuss", models.StatusFinished, intPtr(5), strPtr("A masterpiece.")); err != nil {
		t.Fatalf("update book: %v", err)
	}

	books, err := store.ListBooks(ctx, user.ID, models.BookFilter{})
	if err != nil || len(books) != 2 {
		t.Fatalf("ListBooks = %d books, err %v; want 2", len(books), err)
	}
	filtered, err := store.ListBooks(ctx, user.ID, models.BookFilter{Status: models.StatusFinished})
	if err != nil || len(filtered) != 2 {
		t.Fatalf("filtered list = %d, err %v; want 2 finished", len(filtered), err)
	}
	search, err := store.ListBooks(ctx, user.ID, models.BookFilter{Search: "dune"})
	if err != nil || len(search) != 1 {
		t.Fatalf("search = %d, err %v; want 1", len(search), err)
	}

	year := time.Now().Year()
	stats, err := store.StatsFor(ctx, user.ID, year)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 2 || stats.Finished != 2 || stats.FinishedYear != 2 {
		t.Fatalf("stats = %+v, want total=2 finished=2 finishedYear=2", stats)
	}

	// Goal.
	if err := store.UpsertGoal(ctx, user.ID, year, 12); err != nil {
		t.Fatalf("upsert goal: %v", err)
	}
	stats, err = store.StatsFor(ctx, user.ID, year)
	if err != nil || stats.Goal != 12 {
		t.Fatalf("stats after goal = %+v, err %v; want goal 12", stats, err)
	}
	if stats.Progress() != 16 { // 2/12 = 16%
		t.Fatalf("Progress = %d, want 16", stats.Progress())
	}

	// Delete.
	if err := store.DeleteBook(ctx, user.ID, b1.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := store.BookByID(ctx, user.ID, b1.ID); err != models.ErrNotFound {
		t.Fatalf("BookByID after delete = %v, want ErrNotFound", err)
	}
}

// TestRegisterCSFTSurvivesValidationError is a regression test for the register
// form rejecting resubmits with "invalid CSRF token": after a validation error
// the re-rendered form must keep the token in its hidden field, otherwise the
// next submit no longer matches the cookie. The public-route flow never touches
// the database, so the pool is built lazily against an unreachable host (pgx
// defers connecting).
func TestRegisterCSFTSurvivesValidationError(t *testing.T) {
	cfg := config.Load()
	cfg.DatabaseURL = "postgres://nobody:nothing@127.0.0.1:1/nope?sslmode=disable&connect_timeout=1"

	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		t.Fatalf("parse dsn: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		t.Fatalf("create pool: %v", err)
	}
	defer pool.Close()

	srv := NewServer(cfg, models.NewStore(pool), testLogger(), logs.NewHub(200))

	// GET /register: the cookie and the form's hidden field must match.
	get := httptest.NewRequest(http.MethodGet, "/register", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, get)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /register = %d, want 200", rec.Code)
	}
	token := csrfCookieFromResponse(t, rec)
	if !strings.Contains(rec.Body.String(), `name="csrf" value="`+token+`"`) {
		t.Fatalf("register form token %q does not match the csrf cookie", token)
	}

	// POST with an invalid password: the error page must re-render the SAME token.
	form := url.Values{"csrf": {token}, "email": {"reader@example.com"}, "password": {"short"}}
	post := httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(&http.Cookie{Name: "csrf", Value: token})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, post)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /register (bad password) = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Password must be at least 8 characters") {
		t.Fatalf("expected password validation error, got: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `name="csrf" value="`+token+`"`) {
		t.Fatal("error re-render lost the CSRF token; the next submit would be rejected with 403")
	}

	// Resubmit with the same token (as the browser would after fixing the
	// password): it must NOT be rejected as an invalid CSRF token. The store
	// call fails here (unreachable database), which still exercises the full
	// middleware chain.
	form = url.Values{"csrf": {token}, "email": {"reader@example.com"}, "password": {"longenough123"}}
	post = httptest.NewRequest(http.MethodPost, "/register", strings.NewReader(form.Encode()))
	post.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	post.AddCookie(&http.Cookie{Name: "csrf", Value: token})
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, post)
	if rec.Code == http.StatusForbidden {
		t.Fatal("resubmit after a validation error was rejected with 403 (CSRF token was lost)")
	}
}

// csrfCookieFromResponse extracts the csrf cookie value from a recorded response.
func csrfCookieFromResponse(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == "csrf" && c.Value != "" {
			return c.Value
		}
	}
	t.Fatal("response did not set a csrf cookie")
	return ""
}

func intPtr(n int) *int       { return &n }
func strPtr(s string) *string { return &s }
