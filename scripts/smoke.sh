#!/usr/bin/env bash
# End-to-end smoke test for Pageturner.
# Usage: scripts/smoke.sh [port]
set -euo pipefail

PORT="${1:-8090}"
BASE="http://localhost:${PORT}"
JAR="$(mktemp)"
LOG="$(mktemp)"
cleanup() { [[ -n "${PID:-}" ]] && kill "$PID" 2>/dev/null || true; rm -f "$JAR" "$LOG"; }
trap cleanup EXIT

echo "== building =="
go build -o bin/pageturner ./cmd/server

echo "== starting server on :${PORT} =="
PORT="$PORT" ./bin/pageturner >"$LOG" 2>&1 &
PID=$!
for _ in $(seq 1 30); do
  curl -sf "$BASE/healthz" >/dev/null 2>&1 && break
  sleep 0.5
done

pass() { echo "  ✓ $1"; }
fail() { echo "  ✗ $1"; kill "$PID"; exit 1; }

echo "== health =="
curl -sf "$BASE/healthz" | grep -q '"status":"ok"' && pass "healthz returns ok" || fail "healthz"

echo "== register =="
curl -sf -c "$JAR" "$BASE/register" -o /tmp/pt-reg.html
CSRF=$(grep -o 'name="csrf" value="[^"]*"' /tmp/pt-reg.html | head -1 | sed 's/.*value="//;s/"$//')
[[ -n "$CSRF" ]] && pass "register page has CSRF token" || fail "no CSRF on register page"
curl -sfL -b "$JAR" -c "$JAR" \
  -d "csrf=${CSRF}&email=smoke-$(date +%s)@test.dev&password=password123" \
  "$BASE/register" -o /tmp/pt-reg2.html -w "%{http_code}" | grep -q 200 \
  && pass "register succeeds" || fail "register"

echo "== dashboard (authed) =="
curl -sf -b "$JAR" "$BASE/" | grep -q "Welcome back" && pass "dashboard renders" || fail "dashboard"

echo "== add book via HTMX =="
NEWCSRF=$(awk '$6=="csrf"{print $7}' "$JAR")
RESP=$(curl -sf -b "$JAR" -H "HX-Request: true" -H "X-CSRF-Token: ${NEWCSRF}" \
  -d "title=Dune&author=Frank Herbert&status=reading" "$BASE/books")
echo "$RESP" | grep -q 'id="book-' && pass "book card fragment returned" || fail "add book fragment"
echo "$RESP" | grep -q 'hx-swap-oob="true"' && pass "OOB stats refresh included" || fail "OOB stats"

echo "== live search =="
curl -sf -b "$JAR" -H "HX-Request: true" "$BASE/books/search?q=dune" \
  | grep -q "Dune" && pass "search finds book" || fail "search"

echo "== status change =="
BOOKID=$(echo "$RESP" | grep -o 'id="book-[0-9]*"' | head -1 | grep -o '[0-9]*')
[[ -n "$BOOKID" ]] || fail "could not extract book id from response"
curl -sf -b "$JAR" -H "HX-Request: true" -H "X-CSRF-Token: ${NEWCSRF}" \
  -X PATCH -d "status=finished" "$BASE/books/${BOOKID}/status" \
  | grep -q 'hx-swap-oob="true"' && pass "status patch returns OOB updates" || fail "status patch"

echo "== reading goal =="
curl -sf -b "$JAR" -H "HX-Request: true" -H "X-CSRF-Token: ${NEWCSRF}" \
  -d "goal=12" "$BASE/goal" | grep -q "12" && pass "goal saved and panel re-rendered" || fail "goal"

echo "== static assets =="
curl -sf "$BASE/static/htmx.min.js" | head -c 20 | grep -q "htmx" && pass "htmx served locally" || fail "htmx asset"
curl -sf "$BASE/static/logs.js" | head -c 30 | grep -q "Live backend log viewer" && pass "logs.js served" || fail "logs.js asset"

echo "== live log stream (SSE) =="
# The stream stays open; read 2s of it, then check it emitted SSE frames.
curl -sfN -b "$JAR" --max-time 2 "$BASE/logs/stream" > /tmp/pt-logs.txt 2>/dev/null || true
grep -q "^data:" /tmp/pt-logs.txt && pass "log stream emits SSE events" || fail "log stream"

echo "== CSRF rejected without token =="
CODE=$(curl -s -o /dev/null -w "%{http_code}" -b "$JAR" -d "title=X&author=Y" "$BASE/books")
[[ "$CODE" == "403" ]] && pass "CSRF protection blocks tokenless POST (got $CODE)" || fail "csrf ($CODE)"

echo ""
echo "ALL SMOKE CHECKS PASSED ✅"
