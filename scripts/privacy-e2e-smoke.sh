#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="${CLAIRVEIL_E2E_WORK_DIR:-"$(mktemp -d)"}"
keep_work_dir="${KEEP_WORK_DIR:-0}"
chain_id="${CHAIN_ID:-clairveil-local-1}"
node_name="${NODE_NAME:-local}"
rpc_port="${RPC_PORT:-26657}"
p2p_port="${P2P_PORT:-26656}"
abci_port="${ABCI_PORT:-26658}"
grpc_port="${GRPC_PORT:-9090}"
api_port="${API_PORT:-1317}"
pprof_port="${PPROF_PORT:-6060}"
tx_wait_attempts="${TX_WAIT_ATTEMPTS:-60}"
tx_wait_sleep_seconds="${TX_WAIT_SLEEP_SECONDS:-2}"
node="tcp://127.0.0.1:${rpc_port}"

if [[ -n "${CLAIRVEILD_BIN:-}" ]]; then
	clairveild="$CLAIRVEILD_BIN"
else
	clairveild="$work_dir/clairveild-e2e"
fi

if [[ -n "${CLAIRVEIL_SETUP_BIN:-}" ]]; then
	clairveil_setup="$CLAIRVEIL_SETUP_BIN"
else
	clairveil_setup="$work_dir/clairveil-setup-e2e"
fi

home="$work_dir/home"
out="$work_dir/out"
artifacts="$work_dir/artifacts"
log_file="$work_dir/clairveild.log"
node_pid=""

cleanup() {
	if [[ -n "$node_pid" ]]; then
		kill "$node_pid" >/dev/null 2>&1 || true
		wait "$node_pid" >/dev/null 2>&1 || true
	fi
	if [[ "$keep_work_dir" != "1" && -z "${CLAIRVEIL_E2E_WORK_DIR:-}" ]]; then
		rm -rf "$work_dir"
	fi
}
trap cleanup EXIT

run() {
	"$clairveild" "$@"
}

wait_tx() {
	local tx_hash="$1"
	local query_file="${2:-}"
	local tx_json
	for _ in $(seq 1 "$tx_wait_attempts"); do
		if tx_json="$(run query tx "$tx_hash" --node "$node" --output json 2>/dev/null)"; then
			if [[ -n "$query_file" ]]; then
				printf '%s\n' "$tx_json" >"$query_file"
			fi
			return 0
		fi
		sleep "$tx_wait_sleep_seconds"
	done
	echo "timed out waiting for tx inclusion: $tx_hash" >&2
	return 1
}

write_txhash() {
	local json_file="$1"
	local hash_file="$2"
	python3 - "$json_file" "$hash_file" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
Path(sys.argv[2]).write_text(data["txhash"] + "\n")
PY
}

write_submitted_at() {
	local marker_file="$1"
	date -u +%Y-%m-%dT%H:%M:%SZ >"$marker_file"
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

wait_for_node() {
	for _ in $(seq 1 30); do
		if run status --node "$node" 2>/dev/null | python3 -c 'import json, sys; data=json.load(sys.stdin); sys.exit(0 if int(data["sync_info"]["latest_block_height"]) >= 1 else 1)' >/dev/null 2>&1; then
			return 0
		fi
		sleep 1
	done
	cat "$log_file" >&2
	return 1
}

mkdir -p "$home" "$out"

if [[ -z "${CLAIRVEILD_BIN:-}" ]]; then
	(cd "$repo_root" && go build -o "$clairveild" ./cmd/clairveild)
fi
if [[ -z "${CLAIRVEIL_SETUP_BIN:-}" ]]; then
	(cd "$repo_root" && go build -o "$clairveil_setup" ./cmd/clairveil-setup)
fi

echo "privacy e2e work dir: $work_dir"
"$clairveil_setup" --out "$artifacts" >"$out/setup.stdout" 2>"$out/setup.stderr"

run keys add alice --keyring-backend test --home "$home" --output json >"$out/alice-key.json"
run keys add bob --keyring-backend test --home "$home" --output json >"$out/bob-key.json"
run keys add relayer --keyring-backend test --home "$home" --output json >"$out/relayer-key.json"
run keys add auditor --keyring-backend test --home "$home" --output json >"$out/auditor-key.json"

run keys show -a alice --keyring-backend test --home "$home" >"$out/alice-address.txt"
run keys show -a bob --keyring-backend test --home "$home" >"$out/bob-address.txt"
run keys show -a relayer --keyring-backend test --home "$home" >"$out/relayer-address.txt"
run keys show -a auditor --keyring-backend test --home "$home" >"$out/auditor-address.txt"

run tx privacy show-disclosure-pubkey --from auditor --keyring-backend test --home "$home" --output json >"$out/auditor-disclosure.json"
python3 - "$out/auditor-disclosure.json" "$out/auditor-disclosure.hex" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
Path(sys.argv[2]).write_text(data["public_key_hex"] + "\n")
PY

run init "$node_name" --chain-id "$chain_id" --home "$home" >"$out/init.stdout" 2>"$out/init.stderr"
patch_ports

run add-genesis-account alice 100000000000000000000uclair --keyring-backend test --home "$home" >"$out/add-alice.stdout" 2>"$out/add-alice.stderr"
run add-genesis-account bob 100000000000000000000uclair --keyring-backend test --home "$home" >"$out/add-bob.stdout" 2>"$out/add-bob.stderr"
run add-genesis-account relayer 100000000000000000000uclair --keyring-backend test --home "$home" >"$out/add-relayer.stdout" 2>"$out/add-relayer.stderr"
run add-genesis-account auditor 100000000000000000000uclair --keyring-backend test --home "$home" >"$out/add-auditor.stdout" 2>"$out/add-auditor.stderr"

run gentx alice 9000000000000000000uclair --chain-id "$chain_id" --keyring-backend test --home "$home" >"$out/gentx.stdout" 2>"$out/gentx.stderr"
run collect-gentxs --home "$home" >"$out/collect-gentxs.stdout" 2>"$out/collect-gentxs.stderr"

python3 - "$home" "$out/auditor-disclosure.hex" <<'PY'
import base64
import json
import sys
from pathlib import Path

home = Path(sys.argv[1])
auditor_hex = Path(sys.argv[2]).read_text().strip()
genesis_path = home / "config" / "genesis.json"
doc = json.loads(genesis_path.read_text())
doc["app_state"]["privacy"]["audit_master_pubkey"] = base64.b64encode(bytes.fromhex(auditor_hex)).decode()
genesis_path.write_text(json.dumps(doc, indent=2))
PY

run validate --home "$home" >"$out/validate.stdout" 2>"$out/validate.stderr"

set -a
source "$artifacts/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE="${CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE:-strict}"

run start --home "$home" --minimum-gas-prices 0uclair >"$log_file" 2>&1 &
node_pid=$!
wait_for_node

run tx privacy show-address --from alice --keyring-backend test --home "$home" --output json >"$out/alice-shielded.json"
run tx privacy show-address --from bob --keyring-backend test --home "$home" --output json >"$out/bob-shielded.json"
run tx privacy show-view-key --from alice --keyring-backend test --home "$home" --output json >"$out/alice-view-key.json"
run tx privacy show-disclosure-pubkey --from bob --keyring-backend test --home "$home" --output json >"$out/bob-disclosure.json"

python3 - "$out" <<'PY'
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
for src, dst in [("alice-shielded.json", "alice-shielded-address.txt"), ("bob-shielded.json", "bob-shielded-address.txt")]:
    data = json.loads((out / src).read_text())
    address = data["address"]
    if not address.startswith("clairs1"):
        raise SystemExit(f"unexpected shielded address: {address}")
    (out / dst).write_text(address + "\n")

data = json.loads((out / "bob-disclosure.json").read_text())
(out / "bob-disclosure.hex").write_text(data["public_key_hex"] + "\n")
PY

for amount in 11 10 7; do
	run tx privacy deposit "${amount}uclair" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 2500000 --gas-prices 8500000000uclair --yes --output json >"$out/deposit-${amount}.json"
	write_submitted_at "$out/deposit-${amount}.submitted-at"
	write_txhash "$out/deposit-${amount}.json" "$out/deposit-${amount}.txhash"
	wait_tx "$(cat "$out/deposit-${amount}.txhash")" "$out/deposit-${amount}-query.json"
done

run tx privacy deposit 0uclair --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 2500000 --gas-prices 8500000000uclair --yes --output json >"$out/deposit-dummy.json"
write_submitted_at "$out/deposit-dummy.submitted-at"
write_txhash "$out/deposit-dummy.json" "$out/deposit-dummy.txhash"
wait_tx "$(cat "$out/deposit-dummy.txhash")" "$out/deposit-dummy-query.json"

run tx privacy list-notes --from alice --keyring-backend test --home "$home" --node "$node" --json >"$out/alice-notes.json"
python3 - "$out/alice-notes.json" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
amounts = {note["amount"] for note in doc["notes"] if note["status"] == "spendable"}
required = {"11", "10", "7", "0"}
if not required.issubset(amounts):
    raise SystemExit(f"missing alice notes: {required - amounts}")
PY

run tx privacy transfer "$(cat "$out/bob-shielded-address.txt")" 11uclair --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 9000000 --gas-prices 8500000000uclair --yes --output json >"$out/transfer-private.json"
write_submitted_at "$out/transfer-private.submitted-at"
write_txhash "$out/transfer-private.json" "$out/transfer-private.txhash"
wait_tx "$(cat "$out/transfer-private.txhash")" "$out/transfer-private-query.json"

run tx privacy transfer "$(cat "$out/bob-shielded-address.txt")" 7uclair --privacy-policy amount --disclosure-mode public --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 9000000 --gas-prices 8500000000uclair --yes --output json >"$out/transfer-public.json"
write_submitted_at "$out/transfer-public.submitted-at"
write_txhash "$out/transfer-public.json" "$out/transfer-public.txhash"
wait_tx "$(cat "$out/transfer-public.txhash")" "$out/transfer-public-query.json"

run tx privacy decode-transfer-disclosure --tx-hash "$(cat "$out/transfer-public.txhash")" --disclosure-plane public --node "$node" --report >"$out/transfer-public-report.json"

run tx privacy transfer "$(cat "$out/bob-shielded-address.txt")" 10uclair --privacy-policy amount-from-to --disclosure-mode recipient-encrypted --disclosure-pubkey "$(cat "$out/bob-disclosure.hex")" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 10000000 --gas-prices 8500000000uclair --yes --output json >"$out/transfer-recipient.json"
write_submitted_at "$out/transfer-recipient.submitted-at"
write_txhash "$out/transfer-recipient.json" "$out/transfer-recipient.txhash"
wait_tx "$(cat "$out/transfer-recipient.txhash")" "$out/transfer-recipient-query.json"

run tx privacy decode-transfer-disclosure --tx-hash "$(cat "$out/transfer-recipient.txhash")" --disclosure-plane recipient --from bob --keyring-backend test --home "$home" --node "$node" --report >"$out/transfer-recipient-user-report.json"
run tx privacy decode-transfer-disclosure --tx-hash "$(cat "$out/transfer-recipient.txhash")" --disclosure-plane audit --from auditor --keyring-backend test --home "$home" --node "$node" --report >"$out/transfer-recipient-audit-report.json"

python3 - "$out" <<'PY'
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
public = json.loads((out / "transfer-public-report.json").read_text())
user = json.loads((out / "transfer-recipient-user-report.json").read_text())
audit = json.loads((out / "transfer-recipient-audit-report.json").read_text())

checks = [
    public["summary"]["delivery"] == "public",
    public["summary"]["amount"] == "7",
    public["summary"]["asset_denom"] == "uclair",
    user["verification"]["verified"] is True,
    user["summary"]["delivery"] == "recipient-encrypted",
    user["summary"]["amount"] == "10",
    audit["verification"]["verified"] is True,
    audit["summary"]["delivery"] == "audit-encrypted",
    audit["summary"]["amount"] == "10",
]
if not all(checks):
    raise SystemExit("disclosure verification failed")
PY

run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --json >"$out/bob-notes.json"
python3 - "$out/bob-notes.json" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
amounts = {note["amount"] for note in doc["notes"] if note["status"] == "spendable"}
required = {"11", "7", "10"}
if not required.issubset(amounts):
    raise SystemExit(f"missing bob notes: {required - amounts}")
PY

run tx privacy withdraw 11uclair --recipient "$(cat "$out/alice-address.txt")" --from bob --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 3500000 --gas-prices 8500000000uclair --yes --output json >"$out/withdraw-direct.json"
write_submitted_at "$out/withdraw-direct.submitted-at"
write_txhash "$out/withdraw-direct.json" "$out/withdraw-direct.txhash"
wait_tx "$(cat "$out/withdraw-direct.txhash")" "$out/withdraw-direct-query.json"

run tx privacy prepare-withdraw 7uclair --recipient "$(cat "$out/alice-address.txt")" --from bob --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --out "$out/withdraw-payload.json" --output json >"$out/withdraw-payload.stdout.json"
python3 - "$out/withdraw-payload.stdout.json" "$out/withdraw-payload.json" <<'PY'
import json
import sys
from pathlib import Path

stdout_payload = json.loads(Path(sys.argv[1]).read_text())
file_payload = json.loads(Path(sys.argv[2]).read_text())
if stdout_payload != file_payload:
    raise SystemExit("prepare-withdraw stdout payload differs from file payload")
PY

run tx privacy relay-withdraw "$out/withdraw-payload.json" --from relayer --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 3500000 --gas-prices 8500000000uclair --yes --output json >"$out/withdraw-relayed.json"
write_submitted_at "$out/withdraw-relayed.submitted-at"
write_txhash "$out/withdraw-relayed.json" "$out/withdraw-relayed.txhash"
wait_tx "$(cat "$out/withdraw-relayed.txhash")" "$out/withdraw-relayed-query.json"

run query bank balances "$(cat "$out/alice-address.txt")" --node "$node" --output json >"$out/alice-balances.json"
run query privacy reserve uclair --node "$node" --output json >"$out/reserve-uclair.json"
run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --json >"$out/bob-notes-final.json"

python3 - "$out/bob-notes-final.json" "$out/reserve-uclair.json" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
amounts = {note["amount"] for note in doc["notes"] if note["status"] == "spendable"}
if "10" not in amounts:
    raise SystemExit(f"expected Bob to retain a 10uclair note, got {sorted(amounts)}")

reserve = json.loads(Path(sys.argv[2]).read_text())
if reserve.get("invariant_holds") is not True:
    raise SystemExit(f"reserve invariant failed: {reserve}")
PY

echo "privacy e2e smoke passed"
if [[ "$keep_work_dir" == "1" || -n "${CLAIRVEIL_E2E_WORK_DIR:-}" ]]; then
	echo "work dir retained: $work_dir"
fi
