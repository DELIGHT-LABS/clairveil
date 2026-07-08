#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
work_dir="${CLAIRVEIL_PAYROLL_LIVE_WORK_DIR:-"$repo_root/tmp/reference-payroll-live-localnet"}"
chain_id="${CHAIN_ID:-clairveil-local-1}"
node_name="${NODE_NAME:-payroll-local}"
rpc_port="${RPC_PORT:-26657}"
p2p_port="${P2P_PORT:-26656}"
abci_port="${ABCI_PORT:-26658}"
grpc_port="${GRPC_PORT:-9090}"
api_port="${API_PORT:-1317}"
pprof_port="${PPROF_PORT:-6060}"
tx_wait_attempts="${TX_WAIT_ATTEMPTS:-60}"
tx_wait_sleep_seconds="${TX_WAIT_SLEEP_SECONDS:-2}"
payroll_item_count="${PAYROLL_ITEM_COUNT:-2}"
payroll_amount="${PAYROLL_ITEM_AMOUNT:-1}"
payroll_chunk_size="${PAYROLL_CHUNK_SIZE:-$payroll_item_count}"
payroll_seed_notes="${PAYROLL_SEED_NOTES:-0}"
transfer_batch_gas="${PAYROLL_TRANSFER_BATCH_GAS:-$((payroll_chunk_size * 9000000 + 3000000))}"
gas_prices="${GAS_PRICES:-8500000000uclair}"
node="tcp://127.0.0.1:${rpc_port}"

if ! [[ "$payroll_item_count" =~ ^[1-9][0-9]*$ ]]; then
	echo "PAYROLL_ITEM_COUNT must be a positive integer" >&2
	exit 1
fi
if ! [[ "$payroll_amount" =~ ^[1-9][0-9]*$ ]]; then
	echo "PAYROLL_ITEM_AMOUNT must be a positive integer" >&2
	exit 1
fi
if ! [[ "$payroll_chunk_size" =~ ^[1-9][0-9]*$ ]]; then
	echo "PAYROLL_CHUNK_SIZE must be a positive integer" >&2
	exit 1
fi
if [[ "$payroll_seed_notes" != "0" && "$payroll_seed_notes" != "1" ]]; then
	echo "PAYROLL_SEED_NOTES must be 0 or 1" >&2
	exit 1
fi

rm -rf "$work_dir"
mkdir -p "$work_dir"

if [[ -n "${CLAIRVEILD_BIN:-}" ]]; then
	clairveild="$CLAIRVEILD_BIN"
else
	clairveild="$work_dir/clairveild-payroll-live"
fi
if [[ -n "${CLAIRVEIL_SETUP_BIN:-}" ]]; then
	clairveil_setup="$CLAIRVEIL_SETUP_BIN"
else
	clairveil_setup="$work_dir/clairveil-setup-payroll-live"
fi
if [[ -n "${PAYROLL_BIN:-}" ]]; then
	clairveil_payroll="$PAYROLL_BIN"
else
	clairveil_payroll="$work_dir/clairveil-payroll-live"
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
if [[ -z "${PAYROLL_BIN:-}" ]]; then
	(cd "$repo_root" && go build -o "$clairveil_payroll" ./cmd/clairveil-payroll)
fi

echo "reference payroll live localnet work dir: $work_dir"
"$clairveil_setup" --out "$artifacts" >"$out/setup.stdout" 2>"$out/setup.stderr"

run keys add alice --keyring-backend test --home "$home" --output json >"$out/alice-key.json"
run keys add bob --keyring-backend test --home "$home" --output json >"$out/bob-key.json"
run keys add auditor --keyring-backend test --home "$home" --output json >"$out/auditor-key.json"
run keys show -a alice --keyring-backend test --home "$home" >"$out/alice-address.txt"
run keys show -a bob --keyring-backend test --home "$home" >"$out/bob-address.txt"

run tx privacy show-address --from alice --keyring-backend test --home "$home" --output json >"$out/alice-shielded.json"
python3 - "$out/alice-shielded.json" "$out/alice-shielded-address.txt" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
address = data["address"]
if not address.startswith("clairs1"):
    raise SystemExit(f"unexpected shielded address: {address}")
Path(sys.argv[2]).write_text(address + "\n")
PY

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

if [[ "$payroll_seed_notes" == "1" ]]; then
	"$clairveil_payroll" seed-localnet-notes \
		-genesis "$home/config/genesis.json" \
		-wallet-home "$home" \
		-owner-address "$(cat "$out/alice-address.txt")" \
		-shielded-address "$(cat "$out/alice-shielded-address.txt")" \
		-count "$payroll_item_count" \
		-amount "$payroll_amount" \
		-denom uclair \
		-notes-out "$out/alice-notes.json" \
		-out "$out/seed-localnet-notes.json"
fi

run validate --home "$home" >"$out/validate.stdout" 2>"$out/validate.stderr"

set -a
source "$artifacts/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE="${CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE:-strict}"

run start --home "$home" --minimum-gas-prices 0uclair >"$log_file" 2>&1 &
node_pid=$!
wait_for_node

run tx privacy show-address --from bob --keyring-backend test --home "$home" --output json >"$out/bob-shielded.json"
python3 - "$out/bob-shielded.json" "$out/bob-shielded-address.txt" <<'PY'
import json
import sys
from pathlib import Path

data = json.loads(Path(sys.argv[1]).read_text())
address = data["address"]
if not address.startswith("clairs1"):
    raise SystemExit(f"unexpected shielded address: {address}")
Path(sys.argv[2]).write_text(address + "\n")
PY

run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/bob-notes-before.json"

if [[ "$payroll_seed_notes" == "0" ]]; then
	for i in $(seq 1 "$payroll_item_count"); do
		run tx privacy deposit "${payroll_amount}uclair" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 2500000 --gas-prices "$gas_prices" --yes --output json >"$out/deposit-payroll-${i}.json"
		write_txhash "$out/deposit-payroll-${i}.json" "$out/deposit-payroll-${i}.txhash"
		wait_tx "$(cat "$out/deposit-payroll-${i}.txhash")" "$out/deposit-payroll-${i}-query.json"

		run tx privacy deposit 0uclair --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 2500000 --gas-prices "$gas_prices" --yes --output json >"$out/deposit-payroll-${i}-dummy.json"
		write_txhash "$out/deposit-payroll-${i}-dummy.json" "$out/deposit-payroll-${i}-dummy.txhash"
		wait_tx "$(cat "$out/deposit-payroll-${i}-dummy.txhash")" "$out/deposit-payroll-${i}-dummy-query.json"
	done

	run tx privacy list-notes --from alice --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/alice-notes.json"
fi

python3 - "$out/payroll-template.json" "$out/bob-shielded-address.txt" "$payroll_item_count" "$payroll_amount" "$payroll_chunk_size" <<'PY'
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
recipient = Path(sys.argv[2]).read_text().strip()
count = int(sys.argv[3])
amount = sys.argv[4]
chunk_size = int(sys.argv[5])
doc = {
    "company_id": "company-live-localnet",
    "payroll_id": "payroll-live-localnet",
    "batch_id": "run-001",
    "denom": "uclair",
    "max_messages_per_tx": chunk_size,
    "default_disclosure_policy": {
        "user_privacy_policy": "all-private",
        "user_disclosure_mode": "none",
    },
    "items": [
        {
            "item_id": f"item-{i:03d}",
            "employee_id": f"employee-{i:03d}",
            "recipient_address": recipient,
            "amount": amount,
        }
        for i in range(1, count + 1)
    ],
}
out.write_text(json.dumps(doc, indent=2) + "\n")
PY

"$clairveil_payroll" build-input-from-notes -template "$out/payroll-template.json" -notes "$out/alice-notes.json" -owner-key-id alice -lookup-key-id localnet-scan -out "$out/payroll-input.json"
"$clairveil_payroll" validate -input "$out/payroll-input.json" -out "$out/payroll-validation.json"
"$clairveil_payroll" prepare-notes -input "$out/payroll-input.json" -out "$out/payroll-note-preparation.json"
"$clairveil_payroll" plan -input "$out/payroll-input.json" -out "$out/payroll-plan.json"
"$clairveil_payroll" run -plan "$out/payroll-plan.json" -state "$out/payroll-reservation-state.json" -out "$out/payroll-confirmed-plan.json"
"$clairveil_payroll" run -plan "$out/payroll-plan.json" -state "$out/payroll-reservation-state.json" -out "$out/payroll-confirmed-plan-retry.json"

chunk_index=0
item_start=0
while (( item_start < payroll_item_count )); do
	chunk_index=$((chunk_index + 1))
	remaining=$((payroll_item_count - item_start))
	item_limit="$payroll_chunk_size"
	if (( item_limit > remaining )); then
		item_limit="$remaining"
	fi
	chunk_label="$(printf "%03d" "$chunk_index")"
	batch_args=()
	for _ in $(seq 1 "$item_limit"); do
		batch_args+=("${payroll_amount}uclair")
	done

	run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/bob-notes-before-chunk-${chunk_label}.json"
	if [[ "$payroll_seed_notes" == "0" ]]; then
		run tx privacy transfer-batch "$(cat "$out/bob-shielded-address.txt")" "${batch_args[@]}" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas "$transfer_batch_gas" --gas-prices "$gas_prices" --yes --rescan-wallet --output json >"$out/payroll-transfer-batch-${chunk_label}.json"
	else
		run tx privacy transfer-batch "$(cat "$out/bob-shielded-address.txt")" "${batch_args[@]}" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas "$transfer_batch_gas" --gas-prices "$gas_prices" --yes --output json >"$out/payroll-transfer-batch-${chunk_label}.json"
	fi
	write_txhash "$out/payroll-transfer-batch-${chunk_label}.json" "$out/payroll-transfer-batch-${chunk_label}.txhash"
	wait_tx "$(cat "$out/payroll-transfer-batch-${chunk_label}.txhash")" "$out/payroll-transfer-batch-${chunk_label}-query.json"
	run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/bob-notes-after-chunk-${chunk_label}.json"

	"$clairveil_payroll" settle-transfer-batch \
		-plan "$out/payroll-plan.json" \
		-state "$out/payroll-reservation-state.json" \
		-tx "$out/payroll-transfer-batch-${chunk_label}.json" \
		-recipient-before "$out/bob-notes-before-chunk-${chunk_label}.json" \
		-recipient-after "$out/bob-notes-after-chunk-${chunk_label}.json" \
		-item-start "$item_start" \
		-item-limit "$item_limit" \
		-out "$out/payroll-settle-report-${chunk_label}.json"
	item_start=$((item_start + item_limit))
done

run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/bob-notes-after.json"
"$clairveil_payroll" status -state "$out/payroll-reservation-state.json" -out "$out/payroll-status-after-settle.json"
"$clairveil_payroll" export-report -plan "$out/payroll-plan.json" -state "$out/payroll-reservation-state.json" -out "$out/payroll-final-report.json"

python3 - "$out/payroll-final-report.json" "$out/payroll-status-after-settle.json" "$payroll_item_count" "$payroll_chunk_size" "$chunk_index" "$out/rehearsal-summary.json" "$payroll_seed_notes" <<'PY'
import json
import sys
from pathlib import Path

report = json.loads(Path(sys.argv[1]).read_text())
status = json.loads(Path(sys.argv[2]).read_text())
count = int(sys.argv[3])
chunk_size = int(sys.argv[4])
chunk_count = int(sys.argv[5])
summary_path = Path(sys.argv[6])
seeded_notes = sys.argv[7] == "1"
if report["status"] != "Confirmed":
    raise SystemExit(f"payroll report status is {report['status']}, expected Confirmed")
if report["summary"]["ConfirmedItems"] != count:
    raise SystemExit("not all payroll items are confirmed")
if status["operations_by_status"].get("Succeeded") != count:
    raise SystemExit("not all operations succeeded")
if status["reservations_by_status"].get("ConfirmedSpent") != count * 2:
    raise SystemExit("not all input reservations are confirmed spent")
summary = {
    "schema_version": "clairveil.reference_payroll_live_localnet_rehearsal.v1",
    "seeded_notes": seeded_notes,
    "payroll_item_count": count,
    "payroll_item_amount": report["items"][0]["amount"] if report["items"] else "",
    "chunk_size": chunk_size,
    "chunk_count": chunk_count,
    "final_payroll_status": report["status"],
    "confirmed_items": report["summary"]["ConfirmedItems"],
    "succeeded_operations": status["operations_by_status"].get("Succeeded", 0),
    "confirmed_spent_reservations": status["reservations_by_status"].get("ConfirmedSpent", 0),
}
summary_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
PY

cat <<EOF
Reference payroll live localnet tutorial passed.

Work dir:              $work_dir
Payroll input:         $out/payroll-input.json
Payroll plan:          $out/payroll-plan.json
Reservation state:     $out/payroll-reservation-state.json
Confirmed retry plan:  $out/payroll-confirmed-plan-retry.json
Transfer batch chunks: $chunk_index
Rehearsal summary:    $out/rehearsal-summary.json
Final status:          $out/payroll-status-after-settle.json
Final payroll report:  $out/payroll-final-report.json
Node log:              $log_file
EOF
