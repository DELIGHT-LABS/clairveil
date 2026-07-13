#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
home="${CLAIRVEIL_HOME:-"$(mktemp -d)"}"
keep_home="${KEEP_HOME:-0}"
start_seconds="${START_SECONDS:-6}"
chain_id="${CHAIN_ID:-clairveil-local-1}"
key_name="${KEY_NAME:-alice}"
fund_amount="${FUND_AMOUNT:-1000000000000000000000uclair}"
stake_amount="${STAKE_AMOUNT:-1000000000000000000uclair}"
rpc_port="${RPC_PORT:-26657}"
p2p_port="${P2P_PORT:-26656}"
abci_port="${ABCI_PORT:-26658}"
grpc_port="${GRPC_PORT:-9090}"
api_port="${API_PORT:-1317}"
pprof_port="${PPROF_PORT:-6060}"

if [[ -n "${CLAIRVEILD_BIN:-}" ]]; then
	clairveild="$CLAIRVEILD_BIN"
else
	clairveild="$home/clairveild-smoke"
	(cd "$repo_root" && go build -o "$clairveild" ./cmd/clairveild)
fi
if [[ -n "${CLAIRVEIL_SETUP_BIN:-}" ]]; then
	clairveil_setup="$CLAIRVEIL_SETUP_BIN"
else
	clairveil_setup="$home/clairveil-setup-smoke"
	(cd "$repo_root" && go build -o "$clairveil_setup" ./cmd/clairveil-setup)
fi

if [[ -n "${CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR:-}" ]]; then
	artifacts="$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR"
	generate_artifacts=0
else
	artifacts="$home/artifacts/privacy"
	generate_artifacts=1
fi

cleanup() {
	if [[ "$keep_home" != "1" && -z "${CLAIRVEIL_HOME:-}" ]]; then
		rm -rf "$home"
	fi
}
trap cleanup EXIT

run() {
	"$clairveild" "$@"
}

patch_ports() {
	python3 - "$home" "$rpc_port" "$p2p_port" "$abci_port" "$grpc_port" "$api_port" "$pprof_port" <<'PY'
import sys
from pathlib import Path

home = Path(sys.argv[1])
rpc_port, p2p_port, abci_port, grpc_port, api_port, pprof_port = sys.argv[2:]

config_path = home / "config" / "config.toml"
config = config_path.read_text()
config = config.replace('proxy_app = "tcp://127.0.0.1:26658"', f'proxy_app = "tcp://127.0.0.1:{abci_port}"')
config = config.replace('laddr = "tcp://127.0.0.1:26657"', f'laddr = "tcp://127.0.0.1:{rpc_port}"')
config = config.replace('laddr = "tcp://0.0.0.0:26656"', f'laddr = "tcp://127.0.0.1:{p2p_port}"')
config = config.replace('pprof_laddr = "localhost:6060"', f'pprof_laddr = "localhost:{pprof_port}"')
config_path.write_text(config)

app_path = home / "config" / "app.toml"
app = app_path.read_text()
app = app.replace('address = "tcp://localhost:1317"', f'address = "tcp://127.0.0.1:{api_port}"')
app = app.replace('address = "localhost:9090"', f'address = "127.0.0.1:{grpc_port}"')
app_path.write_text(app)
PY
}

echo "smoke home: $home"
if [[ "$generate_artifacts" == "1" ]]; then
	"$clairveil_setup" --out "$artifacts" >"$home/setup.stdout" 2>"$home/setup.stderr"
elif [[ ! -f "$artifacts/privacy_zk_manifest.json" ]]; then
	echo "missing privacy artifact manifest: $artifacts/privacy_zk_manifest.json" >&2
	exit 1
fi
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR="$artifacts"
run init local --chain-id "$chain_id" --home "$home" >"$home/init.stdout" 2>"$home/init.stderr"
patch_ports
grep -q '"bond_denom": "uclair"' "$home/config/genesis.json"
grep -q '"base": "uclair"' "$home/config/genesis.json"
grep -q '"name": "Clairveil"' "$home/config/genesis.json"

run keys add "$key_name" --keyring-backend test --home "$home" --output json >"$home/key.json"
address="$(run keys show "$key_name" -a --keyring-backend test --home "$home")"
echo "validator account: $address"

run add-genesis-account "$key_name" "$fund_amount" --keyring-backend test --home "$home" >"$home/add-genesis-account.stdout" 2>"$home/add-genesis-account.stderr"
run gentx "$key_name" "$stake_amount" --chain-id "$chain_id" --keyring-backend test --home "$home" >"$home/gentx.stdout" 2>"$home/gentx.stderr"
run collect-gentxs --home "$home" >"$home/collect-gentxs.stdout" 2>"$home/collect-gentxs.stderr"
run validate --home "$home" >"$home/validate.stdout" 2>"$home/validate.stderr"

CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE="${CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE:-warn}" \
	run start --home "$home" >"$home/start.log" 2>&1 &
pid=$!

sleep "$start_seconds"
if ! kill -0 "$pid" >/dev/null 2>&1; then
	cat "$home/start.log" >&2
	exit 1
fi

kill "$pid" >/dev/null 2>&1 || true
wait "$pid" >/dev/null 2>&1 || true

grep -Eq "starting node|finalizing commit|executed block" "$home/start.log"
echo "localnet smoke passed"
if [[ "$keep_home" == "1" || -n "${CLAIRVEIL_HOME:-}" ]]; then
	echo "home retained: $home"
fi
