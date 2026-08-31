#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export CLAIRVEIL_HOME="${CLAIRVEIL_HOME:-/tmp/clairveil-dapp-local}"
export CHAIN_ID="${CHAIN_ID:-clairveil-local-2}"

node_api_address="${CLAIRVEIL_API_ADDRESS:-tcp://127.0.0.1:1317}"
prover_listen="${CLAIRVEIL_PROVER_LISTEN:-127.0.0.1:8080}"
deposit_prover_listen="${CLAIRVEIL_DEPOSIT_PROVER_LISTEN:-127.0.0.1:8090}"
deposit_proof_url="${CLAIRVEIL_DEPOSIT_PROOF_URL:-http://${deposit_prover_listen}/v1/prove}"
dapp_host="${CLAIRVEIL_DAPP_HOST:-0.0.0.0}"
dapp_port="${PORT:-${CLAIRVEIL_DAPP_PORT:-5173}}"

resolve_gobin() {
	local gobin
	gobin="${GOBIN:-"$(go env GOBIN)"}"
	if [[ -z "$gobin" ]]; then
		gobin="$(go env GOPATH)/bin"
	fi
	printf '%s\n' "$gobin"
}

resolve_binary() {
	local env_value="$1"
	local name="$2"
	local gobin="$3"

	if [[ -n "$env_value" ]]; then
		printf '%s\n' "$env_value"
		return 0
	fi
	if [[ -x "$gobin/$name" ]]; then
		printf '%s\n' "$gobin/$name"
		return 0
	fi
	if command -v "$name" >/dev/null 2>&1; then
		command -v "$name"
		return 0
	fi

	echo "missing $name; run 'make install' first" >&2
	exit 1
}

wait_for_http() {
	local name="$1"
	local url="$2"
	local pid="$3"
	local attempt
	for attempt in {1..40}; do
		if ! kill -0 "$pid" 2>/dev/null; then
			wait "$pid" || true
			echo "$name stopped before becoming ready" >&2
			exit 1
		fi
		if curl --fail --silent --show-error --max-time 2 "$url" >/dev/null 2>&1; then
			return 0
		fi
		sleep 0.25
	done
	echo "timed out waiting for $name at $url" >&2
	exit 1
}

pids=()

cleanup() {
	for pid in "${pids[@]}"; do
		kill "$pid" 2>/dev/null || true
	done
}

trap cleanup INT TERM EXIT

cd "$repo_root"
make init

gobin="$(resolve_gobin)"
clairveild_bin="$(resolve_binary "${CLAIRVEILD_BIN:-}" clairveild "$gobin")"
proverd_bin="$(resolve_binary "${CLAIRVEIL_PROVERD_BIN:-}" clairveil-proverd "$gobin")"
if [[ -n "${CLAIRVEIL_DEPOSIT_PROVERD_BIN:-}" ]]; then
	deposit_proverd_bin="$CLAIRVEIL_DEPOSIT_PROVERD_BIN"
else
	deposit_proverd_bin="$CLAIRVEIL_HOME/bin/clairveil-deposit-proverd"
	mkdir -p "$(dirname "$deposit_proverd_bin")"
	go build -o "$deposit_proverd_bin" ./examples/clairveil-dapp/deposit-prover
fi

# shellcheck disable=SC1091
source "$CLAIRVEIL_HOME/clairveil.env"

python3 - <<'PY'
import os
import re
from pathlib import Path

p = Path(os.environ["CLAIRVEIL_HOME"]) / "config/config.toml"
s = p.read_text()
s = re.sub(r"(?m)^cors_allowed_origins = .*", 'cors_allowed_origins = ["*"]', s)
p.write_text(s)
PY

"$clairveild_bin" start \
	--home "$CLAIRVEIL_HOME" \
	--minimum-gas-prices 0uclair \
	--api.enable \
	--api.enabled-unsafe-cors \
	--api.address "$node_api_address" &
pids+=("$!")

"$proverd_bin" -listen "$prover_listen" &
pids+=("$!")

"$deposit_proverd_bin" -listen "$deposit_prover_listen" &
deposit_proverd_pid="$!"
pids+=("$deposit_proverd_pid")
wait_for_http "local deposit prover" "http://${deposit_prover_listen}/healthz" "$deposit_proverd_pid"

npm --prefix "$repo_root/examples/clairveil-dapp" ci --ignore-scripts

(
	cd "$repo_root/examples/clairveil-dapp"
	# The reference prover intentionally has no browser CORS policy. Route local
	# loopback browser requests through the narrowly scoped same-origin proxy so
	# `make dapp-local` remains usable without weakening the prover transport.
	CLAIRVEIL_HOME="$CLAIRVEIL_HOME" CHAIN_ID="$CHAIN_ID" CLAIRVEIL_DAPP_LOCAL_TEST_MODE=1 CLAIRVEIL_PROVER_PROXY_ENABLED=1 CLAIRVEIL_DEPOSIT_PROOF_URL="$deposit_proof_url" npm start -- --host "$dapp_host" --port "$dapp_port"
) &
pids+=("$!")

echo "DApp: http://127.0.0.1:${dapp_port}"
echo "Deposit prover: ${deposit_proof_url}"
echo "Stop: Ctrl+C"

wait
