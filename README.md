# 📖 Pageturner

A production-ready **book review & reading tracker** built with **Go + HTMX + SQLite** — server-rendered, zero frontend framework, shipped as a single static binary.

Inspired by the "Book Review and Reading Tracker App" idea (item #10) from [Fively's 2026 web app ideas](https://fively.dev/ideas/web-app-ideas/). It's Goodreads reimagined, built the hypermedia way.

## Features

- **Accounts** — register/login with bcrypt password hashing, DB-backed sessions, CSRF protection
- **Library** — add, edit, delete books with title, author, status (to-read / reading / finished), 1–5★ rating and review
- **Live search & filtering** — type to filter by title/author, or filter by status; results swap in via HTMX (debounced, no page reload)
- **Dashboard** — shelf stats and a yearly reading goal with a progress bar
- **HTMX throughout** — forms submit via `hx-post`/`hx-put`/`hx-patch`/`hx-delete`, counters and progress bars refresh with `hx-swap-oob`, status changes are one click
- **Production-ready plumbing** — embedded SQL migrations (run on boot), structured logging (`log/slog`), graceful shutdown, `/healthz` health check, secure cookies, Docker image that is a ~15 MB distroless static binary running as non-root
- **Sync-on-the-fly DDL** — a `ddl/` folder of timestamped SQL scripts (`DDMMYYYYHHMM_name.sql`) is applied incrementally against a `last_synced` marker table on every boot; only scripts newer than the marker run
- **Live backend logs** — a 📡 Logs button streams the server's log output into an overlay terminal over SSE (`GET /logs/stream`)

## Quick start (Docker)

```bash
# Start the app with a persistent SQLite volume
docker compose up -d --build

# Open http://localhost:8080 and create an account
```

To rebuild just the app image (e.g. after pulling code changes) and restart it:

```bash
docker compose up -d --build app
```

## Local development

```bash
docker compose up -d --build
make test      # unit + integration tests
make vet       # go vet ./...
```

### Integration tests

The data-layer integration test runs only when `TEST_DATABASE_URL` is set:

```bash
TEST_DATABASE_URL="file::memory:?cache=shared" go test ./...
```

## Configuration (environment variables)

| Variable        | Default                                                          | Description                                  |
|-----------------|------------------------------------------------------------------|----------------------------------------------|
| `DATABASE_URL`  | `file:data/pageturner.db?...` | SQLite database path |
| `PORT`          | `8080`                                                           | HTTP listen port                             |
| `SESSION_SECRET`| dev placeholder (warns)                                         | **Set a long random value in production**    |
| `SESSION_TTL`   | `168h`                                                           | Session lifetime                             |
| `LOG_FORMAT`    | `text`                                                           | `text` or `json`                             |
| `COOKIE_SECURE` | `false`                                                          | Set `true` when serving over HTTPS           |

See [.env.example](.env.example).

## Architecture

```
cmd/server/                 entrypoint: config, logging, DB, migrations, graceful shutdown
internal/
  config/                   env-based configuration
  database/                 SQLite connection + embedded migration runner
                             + DDL sync runner (timestamped ddl/ scripts vs last_synced marker)
  models/                   data access layer (users, sessions, books, goals, stats)
  auth/                     bcrypt, session tokens, CSRF helpers
  web/                      stdlib router, middleware, handlers
    templates/              html/template layouts, pages and HTMX fragments
internal/web/static/        vendored htmx.min.js + style.css (embedded into the binary)
migrations and ddl/ scripts are embedded via go:embed — the binary is fully self-contained
```

Key design points:

- **Zero runtime dependencies** — templates, static assets and migrations are embedded at compile time; the deployable is one static binary.
- **HTMX + server-rendered HTML** — no client framework, no bundler, no Node in production. Interactive fragments swap in-place; counters and progress bars update out-of-band.
- **Security** — bcrypt passwords, sessions stored as SHA-256 hashes with expiry, double-submit CSRF (cookie + `X-CSRF-Token` header set globally via `hx-headers`), `HttpOnly`/`SameSite=Lax` cookies, non-root container.
- **Operational** — `/healthz` pings the DB, structured request logs, panic recovery, bounded connection pool, graceful shutdown.

## Production checklist

1. Set a strong `SESSION_SECRET` and `COOKIE_SECURE=true` behind TLS.
2. Run with `LOG_FORMAT=json` for structured logging.
3. Deploy the container from `docker compose build app`; SQLite is persisted in the `sqlite-data` Docker volume.
4. Point `/healthz` at your orchestrator's health check.
