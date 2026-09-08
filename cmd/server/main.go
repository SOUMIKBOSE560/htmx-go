package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pageturner/internal/auth"
	"pageturner/internal/config"
	"pageturner/internal/database"
	"pageturner/internal/logs"
	"pageturner/internal/models"
	"pageturner/internal/web"
)

func main() {
	cfg := config.Load()
	hub := logs.NewHub(200) // last 200 log lines, replayed to new stream viewers
	log := newLogger(cfg.LogFormat, hub)
	slog.SetDefault(log)

	ctx := context.Background()

	pool, err := database.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := database.Migrate(ctx, pool); err != nil {
		log.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	if err := database.SyncDDL(ctx, pool); err != nil {
		log.Error("ddl sync failed", "error", err)
		os.Exit(1)
	}
	log.Info("database ready, migrations applied and ddl synced")

	store := models.NewStore(pool)
	seedUser(ctx, store, log)
	srv := web.NewServer(cfg, store, log, hub)

	httpServer := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		log.Info("Pageturner listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Graceful shutdown on Ctrl-C / SIGTERM.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "error", err)
	}
	pool.Close()
	log.Info("bye")
}

func newLogger(format string, hub *logs.Hub) *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var inner slog.Handler
	if format == "json" {
		inner = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		inner = slog.NewTextHandler(os.Stdout, opts)
	}
	// Output goes to stdout as before, but every line is also published to the
	// hub so the frontend log viewer can stream it live.
	return slog.New(logs.NewStreamHandler(inner, hub))
}

// seedUser creates the SEED_EMAIL account on boot when SEED_EMAIL and
// SEED_PASSWORD are set (see .env). It is a no-op when the user exists,
// so restarts are safe.
func seedUser(ctx context.Context, store *models.Store, log *slog.Logger) {
	email := strings.ToLower(strings.TrimSpace(os.Getenv("SEED_EMAIL")))
	if email == "" {
		return
	}
	password := os.Getenv("SEED_PASSWORD")
	if password == "" {
		log.Error("SEED_EMAIL is set but SEED_PASSWORD is empty; skipping seed user")
		return
	}
	if _, err := store.UserByEmail(ctx, email); err == nil {
		log.Info("seed user already exists", "email", email)
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Error("seed user hashing failed", "error", err)
		return
	}
	if _, err := store.CreateUser(ctx, email, hash); err != nil {
		log.Error("seed user creation failed", "error", err)
		return
	}
	log.Info("seed user created", "email", email)
}
