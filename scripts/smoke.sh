#!/usr/bin/env bash
# End-to-end smoke test for Markitdown.
# Usage: scripts/smoke.sh [port]
set -euo pipefail

PORT="${1:-8090}"
BASE="http://localhost:${PORT}"
JAR="$(mktemp)"
LOG="$(mktemp)"
cleanup() { [[ -n "${PID:-}" ]] && kill "$PID" 2>/dev/null || true; rm -f "$JAR" "$LOG"; }
trap cleanup EXIT

echo "== building =="
go build -o bin/markitdown ./cmd/server

echo "== starting server on :${PORT} =="
PORT="$PORT" ./bin/markitdown >"$LOG" 2>&1 &
PID=$!
for _ in $(seq 1 30); do
  curl -sf "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.5
done

pass() { echo "  ✓ $1"; }
fail() { echo "  ✗ $1"; kill "$PID"; exit 1; }

echo "== health =="
curl -sf "$BASE/healthz" | grep -q '"status":"ok"' && pass "healthz returns ok" || fail "healthz"

echo "== home =="
curl -sf "$BASE/" | grep -q "Markitdown" && pass "home renders" || fail "home"

echo "== login =="
curl -sf -c "$JAR" "$BASE/login" -o /tmp/pt-login.html
grep -q 'name="csrf" value="' /tmp/pt-login.html && pass "login page has CSRF token" || fail "no CSRF on login page"
grep -q "Welcome back." /tmp/pt-login.html && pass "login heading renders" || fail "login heading"

echo "== markitdown requires auth =="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/markitdown")
[[ "$CODE" == "303" ]] && pass "markitdown redirects anonymous to login (got $CODE)" || fail "markitdown auth ($CODE)"

echo "== stream requires auth =="
CODE=$(curl -s -o /dev/null -w "%{http_code}" "$BASE/markitdown/stream?text=hello")
[[ "$CODE" == "303" ]] && pass "stream redirects anonymous (got $CODE)" || fail "stream auth ($CODE)"

echo "== static assets =="
curl -sf "$BASE/static/htmx.min.js" | head -c 20 | grep -q "htmx" && pass "htmx served locally" || fail "htmx asset"
curl -sf "$BASE/static/style.css" | grep -q "nav-pill" && pass "theme css served" || fail "css asset"
curl -sf "$BASE/static/markitdown.js" | grep -q "EventSource" && pass "converter js served" || fail "converter asset"

echo "== CSRF rejected without token =="
CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$JAR" -d "email=x@y.z&password=secret123" "$BASE/login")
[[ "$CODE" == "403" ]] && pass "CSRF protection blocks tokenless POST (got $CODE)" || fail "csrf ($CODE)"

echo ""
echo "ALL SMOKE CHECKS PASSED ✅"
