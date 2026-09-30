#!/bin/sh
# Post-deploy smoke test for the two-service HelloCalc. Requires only sh and curl.
#
#   BASE_URL=http://localhost:8080 ./scripts/smoke-test.sh
#
# Everything is checked through the public frontend, which exercises the
# frontend -> backend service-to-service path.
#
# Environment:
#   BASE_URL          frontend address (default http://localhost:8080)
#   BACKEND_BASE_URL  if set, also check the backend directly (usually internal-only)
#   WAIT_SECONDS      how long to wait for /readyz before testing (default 10)
#   TIMEOUT           per-request timeout in seconds (default 5)
#   EXPECTED_VERSION  if set, both services must report this version
#   EXPECTED_COMMIT   if set, both services must report this commit
#
# Exits 0 when every check passes, 1 otherwise.
set -u

BASE_URL=${BASE_URL:-http://localhost:8080}
BASE_URL=${BASE_URL%/}
BACKEND_BASE_URL=${BACKEND_BASE_URL:-}
BACKEND_BASE_URL=${BACKEND_BASE_URL%/}
WAIT_SECONDS=${WAIT_SECONDS:-10}
TIMEOUT=${TIMEOUT:-5}
EXPECTED_VERSION=${EXPECTED_VERSION:-}
EXPECTED_COMMIT=${EXPECTED_COMMIT:-}

if ! command -v curl >/dev/null 2>&1; then
  echo "smoke-test: curl is required" >&2
  exit 1
fi

body_file=$(mktemp)
header_file=$(mktemp)
trap 'rm -f "$body_file" "$header_file"' EXIT
trap 'exit 1' INT TERM

total=0
failed=0

# request METHOD URL [JSON_BODY] -> sets $status; body in $body_file.
request() {
  if [ $# -ge 3 ]; then
    status=$(curl -sS --max-time "$TIMEOUT" -o "$body_file" -D "$header_file" -w '%{http_code}' \
      -X "$1" -H 'Content-Type: application/json' -H "X-Request-ID: $request_id" \
      --data "$3" "$2") || status=000
  else
    status=$(curl -sS --max-time "$TIMEOUT" -o "$body_file" -D "$header_file" -w '%{http_code}' \
      -X "$1" -H "X-Request-ID: $request_id" "$2") || status=000
  fi
}

pass() {
  total=$((total + 1))
  printf 'PASS  %s\n' "$1"
}

fail() {
  total=$((total + 1))
  failed=$((failed + 1))
  printf 'FAIL  %s: %s\n' "$1" "$2"
  printf '      body: %s\n' "$(head -c 300 "$body_file" | tr '\n' ' ')"
}

# check NAME METHOD URL WANT_STATUS BODY_REGEX [JSON_BODY]
check() {
  name=$1 method=$2 url=$3 want=$4 pattern=$5
  shift 5
  request "$method" "$url" "$@"
  if [ "$status" != "$want" ]; then
    fail "$name" "HTTP $status, want $want"
  elif ! grep -Eq -- "$pattern" "$body_file"; then
    fail "$name" "body does not match /$pattern/"
  else
    pass "$name"
  fi
}

# check_count NAME REGEX MIN: the last response body matches REGEX at least MIN times.
check_count() {
  n=$(grep -Eo -- "$2" "$body_file" | wc -l | tr -d ' ')
  if [ "$n" -ge "$3" ]; then
    pass "$1"
  else
    fail "$1" "found $n of $3 expected matches of /$2/"
  fi
}

request_id="smoke-$$-$(date +%s)"
echo "HelloCalc (frontend + backend) smoke test against $BASE_URL"

# Wait for readiness; the frontend is ready only once it can reach the backend.
elapsed=0
while :; do
  request GET "$BASE_URL/readyz"
  [ "$status" = 200 ] && break
  if [ "$elapsed" -ge "$WAIT_SECONDS" ]; then
    echo "      /readyz not ready after ${WAIT_SECONDS}s (last status $status: $(tr -d '\n' <"$body_file"))"
    break
  fi
  sleep 1
  elapsed=$((elapsed + 1))
done

F=$BASE_URL
check "frontend GET / serves the UI"           GET  "$F/"        200 '<title>HelloCalc</title>'
check "frontend GET /health"                   GET  "$F/health"  200 '"status": *"ok"'
check "frontend GET /readyz (backend reachable)" GET "$F/readyz" 200 '"status": *"ready"'
check "frontend GET /version reports backend"  GET  "$F/version" 200 '"backend": *\{[^}]*"name": *"hellocalc-backend"'
printf '      %s\n' "$(tr -d '\n' <"$body_file")"
if [ -n "$EXPECTED_VERSION" ]; then
  check_count "both services at version $EXPECTED_VERSION" "\"version\": *\"$EXPECTED_VERSION\"" 2
fi
if [ -n "$EXPECTED_COMMIT" ]; then
  check_count "both services at commit $EXPECTED_COMMIT" "\"commit\": *\"$EXPECTED_COMMIT\"" 2
fi

check "via frontend: 12.5 * 4"                 POST "$F/api/calculate" 200 '"result": *50[,}]' '{"left":12.5,"operator":"*","right":4}'
check "via frontend: 10 / 4"                   POST "$F/api/calculate" 200 '"result": *2\.5[,}]' '{"left":10,"operator":"/","right":4}'
check "via frontend: -7 * 3"                   POST "$F/api/calculate" 200 '"result": *-21[,}]' '{"left":-7,"operator":"*","right":3}'
check "via frontend: 1 / 0 -> 422"             POST "$F/api/calculate" 422 '"error": *"division by zero"' '{"left":1,"operator":"/","right":0}'
check "via frontend: bad JSON -> 400"          POST "$F/api/calculate" 400 '"error"' '{"left":'

# The last response came from the backend through the proxy; it must carry our ID.
if grep -iq "^x-request-id: *$request_id" "$header_file"; then
  pass "X-Request-ID propagated through frontend and backend"
else
  fail "X-Request-ID propagated through frontend and backend" "response did not echo $request_id"
fi

if [ -n "$BACKEND_BASE_URL" ]; then
  B=$BACKEND_BASE_URL
  check "backend GET /health"                  GET  "$B/health"  200 '"status": *"ok"'
  check "backend GET /readyz"                  GET  "$B/readyz"  200 '"status": *"ready"'
  check "backend GET /version"                 GET  "$B/version" 200 '"name": *"hellocalc-backend"'
  check "backend POST /api/calculate"          POST "$B/api/calculate" 200 '"result": *3[,}]' '{"left":1,"operator":"+","right":2}'
fi

if [ "$failed" -ne 0 ]; then
  echo "FAILED: $failed of $total checks failed"
  exit 1
fi
echo "OK: all $total checks passed"
