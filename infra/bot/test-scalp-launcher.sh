#!/usr/bin/env bash
# infra/bot/test-scalp-launcher.sh — shell-level test for scalp-launcher.sh (task 146).
# Spins a stubbed API (python3, no Spring, no DB) plus a fake scalp-session bin and
# proves the claim-then-launch ordering: GET pending -> POST claim (200) -> session
# bin invoked -> POST finish with the session's exit code. No `claude` invocation.
#
# Usage: ./test-scalp-launcher.sh   (exit 0 = all checks passed)
set -euo pipefail
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
TMP="$(mktemp -d)"
STUB_PORT=18099
ORDER_FILE="${TMP}/order.log"
FAKE_BIN="${TMP}/fake-scalp-session.sh"
LAUNCHER_LOG="${TMP}/launcher.log"
SESSION_LOG="${TMP}/session.log"
LOCK_FILE="${TMP}/launcher.lock"

pass=0; fail=0
check() { # $1 = description, $2... = command that must succeed
  local desc="$1"; shift
  if "$@" >/dev/null 2>&1; then pass=$((pass+1)); echo "PASS: ${desc}";
  else fail=$((fail+1)); echo "FAIL: ${desc}"; fi
}

cleanup() { kill "${STUB_PID:-}" 2>/dev/null || true; rm -rf "${TMP}"; }
trap cleanup EXIT

# ── 1. syntax ─────────────────────────────────────────────────────────────
check "launcher bash -n" bash -n "${SCRIPT_DIR}/scalp-launcher.sh"

# ── 2. stub API: PENDING -> claim(200) -> RUNNING -> finish(200) -> DONE ────
cat >"${TMP}/stub.py" <<EOF
import json
from http.server import BaseHTTPRequestHandler, HTTPServer
ORDER = ${ORDER_FILE@Q}
state = {"status": "PENDING"}
def log(line):
    with open(ORDER, "a") as f:
        f.write(line + "\n")
class H(BaseHTTPRequestHandler):
    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)
    def do_GET(self):
        if self.path == "/api/bot/trades/pending-scalp":
            log("GET pending")
            if state["status"] == "PENDING":
                self._send(200, {"data": [{"id": 101, "scalpSessionStatus": "PENDING"}], "meta": {}})
            else:
                self._send(200, {"data": [], "meta": {}})
        else:
            self._send(404, {"errors": []})
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(n).decode() if n else ""
        if self.path == "/api/bot/trades/101/scalp-claim":
            if state["status"] == "PENDING":
                state["status"] = "RUNNING"
                log("POST claim 101 -> 200")
                self._send(200, {"data": {"id": 101, "scalpSessionStatus": "RUNNING"}, "meta": {}})
            else:
                log("POST claim 101 -> 409")
                self._send(409, {"errors": [{"code": "CONFLICT"}]})
        elif self.path == "/api/bot/trades/101/scalp-finish":
            try:
                code = json.loads(raw).get("exitCode")
            except Exception:
                code = None
            if state["status"] == "RUNNING":
                state["status"] = "DONE"
                log(f"POST finish 101 exitCode={code} -> 200")
                self._send(200, {"data": {"id": 101, "scalpSessionStatus": "DONE"}, "meta": {}})
            else:
                log(f"POST finish 101 exitCode={code} -> 409")
                self._send(409, {"errors": [{"code": "CONFLICT"}]})
        elif self.path == "/__reset":
            state["status"] = "PENDING"
            self._send(200, {})
        else:
            self._send(404, {"errors": []})
    def log_message(self, *a):
        pass
HTTPServer(("127.0.0.1", ${STUB_PORT}), H).serve_forever()
EOF
python3 "${TMP}/stub.py" & STUB_PID=$!
for i in $(seq 1 50); do
  curl -s -o /dev/null "http://127.0.0.1:${STUB_PORT}/api/bot/trades/pending-scalp" && break
  sleep 0.1
done

cat >"${FAKE_BIN}" <<EOF
#!/usr/bin/env bash
echo "SESSION_START \$1" >>${ORDER_FILE@Q}
echo "SESSION_END \$1" >>${ORDER_FILE@Q}
exit 3
EOF
chmod +x "${FAKE_BIN}"

export BOT_API_BASE_URL="http://127.0.0.1:${STUB_PORT}/api"
export BOT_INGEST_TOKEN="test-token"
export SCALP_SESSION_BIN="${FAKE_BIN}"
export SCALP_LAUNCHER_LOCK="${LOCK_FILE}"
export SCALP_LAUNCHER_LOG="${LAUNCHER_LOG}"
export SCALP_SESSION_LOG="${SESSION_LOG}"
export SCALP_POLL_SECONDS=1
LAUNCHER="${SCRIPT_DIR}/scalp-launcher.sh"

wait_for() { # $1 = fixed string, $2 = timeout seconds
  for _ in $(seq 1 "$(( $2 * 10 ))"); do
    grep -Fq "$1" "${ORDER_FILE}" 2>/dev/null && return 0
    sleep 0.1
  done
  return 1
}

# ── 3. dry-run: real GET + real claim, no launch, no finish ────────────────
: >"${ORDER_FILE}"
"${LAUNCHER}" --dry-run
check "dry-run claims (200)" wait_for "POST claim 101 -> 200" 10
# (real check: no SESSION marker and no finish POST)
if grep -Fq "SESSION_START" "${ORDER_FILE}" || grep -Fq "POST finish" "${ORDER_FILE}"; then
  fail=$((fail+1)); echo "FAIL: dry-run launched or finished"
else
  pass=$((pass+1)); echo "PASS: dry-run launches nothing, posts no finish"
fi
curl -s -X POST "http://127.0.0.1:${STUB_PORT}/__reset" -o /dev/null

# ── 4. once: claim -> launch -> finish(exitCode=3), in order ───────────────
: >"${ORDER_FILE}"
"${LAUNCHER}" --once
check "once claims (200)" wait_for "POST claim 101 -> 200" 10
check "once launches session with id" wait_for "SESSION_START 101" 10
check "once posts finish with exit code 3" wait_for "POST finish 101 exitCode=3 -> 200" 15
# ordering: claim line number < session line number < finish line number
claim_ln=$(grep -Fn "POST claim 101 -> 200" "${ORDER_FILE}" | head -1 | cut -d: -f1)
sess_ln=$(grep -Fn "SESSION_START 101" "${ORDER_FILE}" | head -1 | cut -d: -f1)
fin_ln=$(grep -Fn "POST finish 101" "${ORDER_FILE}" | head -1 | cut -d: -f1)
if [[ "${claim_ln}" -lt "${sess_ln}" && "${sess_ln}" -lt "${fin_ln}" ]]; then
  pass=$((pass+1)); echo "PASS: claim -> launch -> finish ordering"
else
  fail=$((fail+1)); echo "FAIL: ordering claim=${claim_ln} session=${sess_ln} finish=${fin_ln}"
fi

# ── 5. second run: DONE row -> claim 409 path not even attempted (empty pending) ──
: >"${ORDER_FILE}"
"${LAUNCHER}" --once
sleep 1
if grep -Fq "SESSION_START" "${ORDER_FILE}"; then
  fail=$((fail+1)); echo "FAIL: second run launched a session for a DONE trade"
else
  pass=$((pass+1)); echo "PASS: second run launches nothing (single-flight)"
fi

# ── 6. flock: a held lock makes --once exit 1 ──────────────────────────────
flock -n "${LOCK_FILE}" sleep 5 &
HOLDER=$!
sleep 0.3
if "${LAUNCHER}" --once >/dev/null 2>&1; then
  fail=$((fail+1)); echo "FAIL: launcher ran despite held lock"
else
  pass=$((pass+1)); echo "PASS: held lock refuses second launcher"
fi
kill "${HOLDER}" 2>/dev/null || true
wait "${HOLDER}" 2>/dev/null || true

echo "---"
echo "pass=${pass} fail=${fail}"
[[ "${fail}" -eq 0 ]]
