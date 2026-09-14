#!/usr/bin/env bash
set -Eeuo pipefail

runtime_dir="${XDG_RUNTIME_DIR:-/tmp/smallctl-runtime}"
mkdir -p "$runtime_dir"

smallctl serve --config /etc/smallctl/config.yaml &
server_pid=$!

cleanup() {
  if [[ -n "${server_pid:-}" ]] && kill -0 "$server_pid" 2>/dev/null; then
    kill -TERM "$server_pid"
    wait "$server_pid" || true
  fi
}
trap cleanup EXIT

for _ in $(seq 1 50); do
  if smallctl env get >/dev/null 2>&1; then
    break
  fi
  sleep 0.1
done

if ! smallctl env get >/dev/null 2>&1; then
  echo "smallctl server did not become ready" >&2
  exit 1
fi

if [[ "$(smallctl invoke greet --args name=integration)" != "hello integration" ]]; then
  echo "fallback command returned unexpected output" >&2
  exit 1
fi

if [[ "$(smallctl env set container)" != "container" ]]; then
  echo "environment was not set to container" >&2
  exit 1
fi

if [[ "$(smallctl invoke greet)" != "hello from container" ]]; then
  echo "environment-specific command returned unexpected output" >&2
  exit 1
fi

smallctl shutdown

set +e
wait "$server_pid"
server_status=$?
set -e
server_pid=""

if [[ $server_status -ne 0 ]]; then
  echo "smallctl server exited with status $server_status" >&2
  exit "$server_status"
fi
