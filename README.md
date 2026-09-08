# Markitdown

A **paste-to-markdown converter** built with **Go + HTMX + SQLite** — server-rendered, Mobbin-style monochrome UI, shipped as a single static binary. Three pages: `/` (landing), `/login`, `/markitdown` (streaming converter).

## Features

- **Accounts** — login with bcrypt password hashing, DB-backed sessions, CSRF protection
- **Markitdown converter** — paste text on the left, press the Convert pill, watch a dummy OpenAI-style response stream an invoice markdown doc live on the right (SSE deltas at `GET /markitdown/stream`)
- **HTMX shell** — server-rendered pages with vendored htmx, tiny vanilla JS only for the SSE stream renderer
- **Production-ready plumbing** — embedded SQL migrations (run on boot), structured logging (`log/slog`), graceful shutdown, `/healthz` health check, secure cookies, Docker image that is a ~15 MB distroless static binary running as non-root
- **Sync-on-the-fly DDL** — a `ddl/` folder of timestamped SQL scripts (`DDMMYYYYHHMM_name.sql`) is applied incrementally against a `last_synced` marker table on every boot; only scripts newer than the marker run
- **Live backend logs** — a 📡 Logs button streams the server's log output into an overlay terminal over SSE (`GET /logs/stream`)

## Quick start (Docker — single image, no compose)

```bash
# Copy .env.example to .env and set SESSION_SECRET + SEED_PASSWORD, then:
make docker-build
make docker-run

# Open http://localhost:8909 and log in with SEED_EMAIL / SEED_PASSWORD
```

To rebuild after code changes and restart it:

```bash
make docker-build
make docker-stop
make docker-run
```

Equivalent raw commands:

```bash
docker build -t markitdown .
docker run -d --name markitdown -p 8909:8909 --env-file .env \
  -e DATABASE_URL="file:/data/markitdown.db?_busy_timeout=5000&_foreign_keys=on" \
  -v markitdown-data:/data --restart unless-stopped markitdown
```

## Local development

```bash
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
| `DATABASE_URL`  | `file:data/pageturner.db?...` (local) / `file:/data/markitdown.db?...` (container) | SQLite database path |
| `PORT`          | `8909`                                                           | HTTP listen port                             |
| `SESSION_SECRET`| dev placeholder (warns)                                         | **Set a long random value in production**    |
| `SESSION_TTL`   | `168h`                                                           | Session lifetime                             |
| `LOG_FORMAT`    | `text`                                                           | `text` or `json`                             |
| `COOKIE_SECURE` | `false`                                                          | Set `true` when serving over HTTPS           |
| `SEED_EMAIL` / `SEED_PASSWORD` | unset (skipped)                                   | Demo login seeded into SQLite on boot        |

See [.env.example](.env.example).

## Architecture

```
cmd/server/                 entrypoint: config, logging, DB, migrations, graceful shutdown
internal/
  config/                   env-based configuration
  database/                 SQLite connection + embedded migration runner
                             + DDL sync runner (timestamped ddl/ scripts vs last_synced marker)
  models/                   data access layer (users, sessions)
  auth/                     bcrypt, session tokens, CSRF helpers
  web/                      stdlib router, middleware, handlers
    templates/              html/template layouts, pages and HTMX fragments
internal/web/static/        vendored htmx.min.js (embedded into the binary)
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
3. Deploy the container from `make docker-build`; SQLite is persisted in the `markitdown-data` Docker volume.
4. Point `/healthz` at your orchestrator's health check.
