#!/usr/bin/env bash
set -euo pipefail

AGENT="${1:-build/linux-bundle/pal-agent}"
if [[ ! -x "${AGENT}" ]]; then
  echo "Linux Agent is missing or not executable: ${AGENT}" >&2
  exit 1
fi

ROOT="$(mktemp -d)"
PORT="${PALSERVER_SMOKE_PORT:-18210}"
PASSWORD="SmokeTest!12345"
PID=""

cleanup() {
  if [[ -n "${PID}" ]] && kill -0 "${PID}" 2>/dev/null; then
    kill -TERM "${PID}" 2>/dev/null || true
    wait "${PID}" 2>/dev/null || true
  fi
  rm -rf -- "${ROOT}"
}
trap cleanup EXIT

mkdir -p "${ROOT}/servers" "${ROOT}/home"
PALSERVER_LAUNCHER_HOME="${ROOT}/data" \
PALSERVER_ALLOWED_SERVER_ROOTS="${ROOT}/servers" \
HOME="${ROOT}/home" \
  "${AGENT}" --self-test --auth-file "${ROOT}/admin-auth.json" >"${ROOT}/self-test.json"
grep -q '"ok":true' "${ROOT}/self-test.json"
PALSERVER_LAUNCHER_HOME="${ROOT}/data" \
PALSERVER_ALLOWED_SERVER_ROOTS="${ROOT}/servers" \
HOME="${ROOT}/home" \
  "${AGENT}" \
  --listen "127.0.0.1:${PORT}" \
  --auth-file "${ROOT}/admin-auth.json" \
  >"${ROOT}/agent.log" 2>&1 &
PID="$!"

for _ in {1..50}; do
  if curl --fail --silent --show-error "http://127.0.0.1:${PORT}/api/v1/health" >"${ROOT}/health.json"; then
    break
  fi
  if ! kill -0 "${PID}" 2>/dev/null; then
    cat "${ROOT}/agent.log" >&2
    exit 1
  fi
  sleep 0.1
done

grep -q '"ok":true' "${ROOT}/health.json"
grep -q '"setupRequired":true' "${ROOT}/health.json"
curl --fail --silent --show-error \
  -c "${ROOT}/cookies" \
  -H 'Content-Type: application/json' \
  --data "{\"password\":\"${PASSWORD}\"}" \
  "http://127.0.0.1:${PORT}/api/v1/setup" >"${ROOT}/setup.json"
grep -q '"ok":true' "${ROOT}/setup.json"
test -s "${ROOT}/admin-auth.json"
if grep -q "${PASSWORD}" "${ROOT}/admin-auth.json"; then
  echo "administrator password was stored in plaintext" >&2
  exit 1
fi

curl --fail --silent --show-error \
  -b "${ROOT}/cookies" \
  -H 'Content-Type: application/json' \
  --data '{"args":[]}' \
  "http://127.0.0.1:${PORT}/api/v1/rpc/GetLauncherVersion" >"${ROOT}/rpc.json"
grep -q '"result"' "${ROOT}/rpc.json"

curl --fail --silent --show-error \
  -b "${ROOT}/cookies" \
  -H 'Content-Type: application/json' \
  --data "{\"args\":[\"${ROOT}/server\"]}" \
  "http://127.0.0.1:${PORT}/api/v1/rpc/GetSetupEnvironment" >"${ROOT}/environment.json"
grep -q '"platform":"linux"' "${ROOT}/environment.json"
grep -q '"cpuCores"' "${ROOT}/environment.json"

kill -TERM "${PID}"
wait "${PID}"
PID=""

echo "Linux Agent self-test, health, login, authenticated RPC, and graceful shutdown passed."
