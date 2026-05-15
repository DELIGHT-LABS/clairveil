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

if [[ -n "${CLAIRVEILD_BIN:-}" ]]; then
	clairveild="$CLAIRVEILD_BIN"
else
	clairveild="$home/clairveild-smoke"
	(cd "$repo_root" && go build -o "$clairveild" ./cmd/clairveild)
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

echo "smoke home: $home"
run init local --chain-id "$chain_id" --home "$home" >"$home/init.stdout" 2>"$home/init.stderr"
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
