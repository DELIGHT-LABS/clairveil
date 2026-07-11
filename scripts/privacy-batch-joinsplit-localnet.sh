#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$repo_root/x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json"
run_localnet="${RUN_LOCALNET:-0}"
work_dir="${CLAIRVEIL_BATCH_LOCALNET_WORK_DIR:-$repo_root/tmp/privacy-batch-joinsplit-localnet}"
artifact_override="${CLAIRVEIL_BATCH_ARTIFACT_DIR:-}"
chain_id="${CHAIN_ID:-clairveil-batch-local-1}"
rpc_port="${RPC_PORT:-26657}"
p2p_port="${P2P_PORT:-26656}"
abci_port="${ABCI_PORT:-26658}"
grpc_port="${GRPC_PORT:-9090}"
api_port="${API_PORT:-1317}"
pprof_port="${PPROF_PORT:-6060}"
proverd_port="${PROVERD_PORT:-18080}"
node="tcp://127.0.0.1:${rpc_port}"
gas_prices="${GAS_PRICES:-8500000000uclair}"
batch_gas="${BATCH_GAS:-80000000}"
expires_in="${BATCH_EXPIRES_IN:-7200}"
tx_wait_attempts="${TX_WAIT_ATTEMPTS:-90}"
tx_wait_sleep_seconds="${TX_WAIT_SLEEP_SECONDS:-2}"

python3 - "$fixture" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
assert doc["schema_version"] == "clairveil.batch-transfer.session3b.v1"
assert doc["prover_route"] == "/v1/proofs/batch-transfer"
assert doc["max_inputs"] == 16 and doc["max_outputs"] == 32
want = {
    "one-input-one-payment",
    "three-input-four-output-mixed-disclosure",
    "thirty-one-payments-plus-change",
    "exact-thirty-two-payments",
    "explicit-zero-padding",
}
cases = {case["id"]: case for case in doc["cases"]}
assert set(cases) == want
for case in cases.values():
    assert 1 <= len(case["input_amounts"]) <= 16
    assert 1 <= len(case["expected_output_roles"]) <= 32
    assert len(case["expected_output_roles"]) == len(case["disclosure_modes"])
    assert sum(case["payment_amounts"]) <= sum(case["input_amounts"])
assert len(cases["three-input-four-output-mixed-disclosure"]["expected_output_roles"]) == 4
assert cases["three-input-four-output-mixed-disclosure"]["disclosure_modes"] == ["none", "public", "recipient-encrypted", "none"]
assert cases["thirty-one-payments-plus-change"]["expected_output_roles"].count("payment") == 31
assert cases["thirty-one-payments-plus-change"]["expected_output_roles"][-1] == "change"
assert cases["exact-thirty-two-payments"]["expected_output_roles"].count("payment") == 32
assert cases["explicit-zero-padding"]["expected_output_roles"].count("padding") == 31
retry = doc["restart_retry"]
assert retry["reuse_signed_tx_bytes"] and retry["reconcile_tx_hash_first"]
assert retry["reconcile_nullifiers_before_resign"]
assert not retry["automatic_multi_prover_failover"]
print("Session 3B batch fixture validation passed.")
PY

(cd "$repo_root" && go test ./x/privacy/client/sdk/conformance -run TestSession3BBatchTransferContract -count=1)

if [[ "$run_localnet" == "0" ]]; then
	echo "Static Session 3B validation passed. Set RUN_LOCALNET=1 for the actual node/prover workflow."
	exit 0
fi
if [[ "$run_localnet" != "1" ]]; then
	echo "RUN_LOCALNET must be 0 or 1" >&2
	exit 1
fi

rm -rf "$work_dir"
mkdir -p "$work_dir/out" "$work_dir/home"
home="$work_dir/home"
out="$work_dir/out"
artifacts="${artifact_override:-$work_dir/artifacts}"
node_log="$work_dir/clairveild.log"
proverd_log="$work_dir/clairveil-proverd.log"
node_pid=""
proverd_pid=""

clairveild="${CLAIRVEILD_BIN:-$work_dir/clairveild-batch-localnet}"
clairveil_setup="${CLAIRVEIL_SETUP_BIN:-$work_dir/clairveil-setup-batch-localnet}"
clairveil_proverd="${CLAIRVEIL_PROVERD_BIN:-$work_dir/clairveil-proverd-batch-localnet}"

cleanup() {
	if [[ -n "$proverd_pid" ]]; then kill "$proverd_pid" >/dev/null 2>&1 || true; wait "$proverd_pid" >/dev/null 2>&1 || true; fi
	if [[ -n "$node_pid" ]]; then kill "$node_pid" >/dev/null 2>&1 || true; wait "$node_pid" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

if [[ -z "${CLAIRVEILD_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveild" ./cmd/clairveild); fi
if [[ -z "$artifact_override" && -z "${CLAIRVEIL_SETUP_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveil_setup" ./cmd/clairveil-setup); fi
if [[ -z "${CLAIRVEIL_PROVERD_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveil_proverd" ./cmd/clairveil-proverd); fi

run() { "$clairveild" "$@"; }

patch_ports() {
	python3 - "$home" "$rpc_port" "$p2p_port" "$abci_port" "$grpc_port" "$api_port" "$pprof_port" <<'PY'
import sys
from pathlib import Path
home = Path(sys.argv[1])
rpc, p2p, abci, grpc, api, pprof = sys.argv[2:]
config_path = home / "config" / "config.toml"
config = config_path.read_text()
config = config.replace('proxy_app = "tcp://127.0.0.1:26658"', f'proxy_app = "tcp://127.0.0.1:{abci}"')
config = config.replace('laddr = "tcp://127.0.0.1:26657"', f'laddr = "tcp://127.0.0.1:{rpc}"')
config = config.replace('laddr = "tcp://0.0.0.0:26656"', f'laddr = "tcp://127.0.0.1:{p2p}"')
config = config.replace('pprof_laddr = "localhost:6060"', f'pprof_laddr = "localhost:{pprof}"')
config_path.write_text(config)
app_path = home / "config" / "app.toml"
app = app_path.read_text()
app = app.replace('address = "tcp://localhost:1317"', f'address = "tcp://127.0.0.1:{api}"')
app = app.replace('address = "localhost:9090"', f'address = "127.0.0.1:{grpc}"')
app_path.write_text(app)
PY
}

wait_for_node() {
	for _ in $(seq 1 60); do
		if run status --node "$node" 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); raise SystemExit(0 if int(d["sync_info"]["latest_block_height"]) >= 1 else 1)' >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	tail -200 "$node_log" >&2
	return 1
}

wait_for_proverd() {
	for _ in $(seq 1 120); do
		if curl --fail --silent "http://127.0.0.1:${proverd_port}/readyz" >"$out/proverd-ready.json" 2>/dev/null; then return 0; fi
		sleep 1
	done
	tail -200 "$proverd_log" >&2
	return 1
}

wait_tx() {
	local tx_hash="$1" query_file="$2" tx_json
	for _ in $(seq 1 "$tx_wait_attempts"); do
		if tx_json="$(run query tx "$tx_hash" --node "$node" --output json 2>/dev/null)"; then printf '%s\n' "$tx_json" >"$query_file"; return 0; fi
		sleep "$tx_wait_sleep_seconds"
	done
	echo "timed out waiting for tx inclusion: $tx_hash" >&2
	return 1
}

tx_hash_from_file() {
	python3 - "$1" <<'PY'
import json,sys
print(json.load(open(sys.argv[1]))["txhash"])
PY
}

if [[ -z "$artifact_override" ]]; then
	echo "Generating all prover/validator artifacts; the 16x32 setup is intentionally resource intensive."
	"$clairveil_setup" --out "$artifacts" >"$out/setup.stdout" 2>"$out/setup.stderr"
else
	echo "Using pre-generated development artifacts from $artifacts"
	test -f "$artifacts/privacy_zk_checksums.env"
	test -f "$artifacts/privacy_batch_joinsplit_16x32_r1cs.bin"
	test -f "$artifacts/privacy_batch_joinsplit_16x32_pk.bin"
	test -f "$artifacts/privacy_batch_joinsplit_16x32_vk.bin"
fi
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR="$artifacts"

run keys add alice --keyring-backend test --home "$home" --output json >"$out/alice-key.json"
run keys add bob --keyring-backend test --home "$home" --output json >"$out/bob-key.json"
run keys add auditor --keyring-backend test --home "$home" --output json >"$out/auditor-key.json"
run keys show -a alice --keyring-backend test --home "$home" >"$out/alice-address.txt"
run tx privacy show-disclosure-pubkey --from auditor --keyring-backend test --home "$home" --output json >"$out/auditor-disclosure.json"
run init batch-local --chain-id "$chain_id" --home "$home" >"$out/init.stdout" 2>"$out/init.stderr"
patch_ports
run add-genesis-account alice 100000000000000000000uclair --keyring-backend test --home "$home" >/dev/null
run add-genesis-account bob 100000000000000000000uclair --keyring-backend test --home "$home" >/dev/null
run add-genesis-account auditor 100000000000000000000uclair --keyring-backend test --home "$home" >/dev/null
run gentx alice 9000000000000000000uclair --chain-id "$chain_id" --keyring-backend test --home "$home" >/dev/null
run collect-gentxs --home "$home" >/dev/null

python3 - "$home/config/genesis.json" "$out/auditor-disclosure.json" <<'PY'
import base64,json,sys
from pathlib import Path
genesis = Path(sys.argv[1])
doc = json.loads(genesis.read_text())
key = json.loads(Path(sys.argv[2]).read_text())["public_key_hex"]
doc["app_state"]["privacy"]["audit_master_pubkey"] = base64.b64encode(bytes.fromhex(key)).decode()
doc["app_state"]["privacy"]["audit_key_id"] = "master"
doc["app_state"]["privacy"]["audit_key_epoch"] = "1"
genesis.write_text(json.dumps(doc, indent=2))
PY

run validate --home "$home" >/dev/null
set -a
source "$artifacts/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict

run start --home "$home" --minimum-gas-prices 0uclair >"$node_log" 2>&1 & node_pid=$!
wait_for_node
"$clairveil_proverd" --listen "127.0.0.1:${proverd_port}" >"$proverd_log" 2>&1 & proverd_pid=$!
wait_for_proverd

run tx privacy show-address --from bob --keyring-backend test --home "$home" --output json >"$out/bob-shielded.json"
run tx privacy show-disclosure-pubkey --from bob --keyring-backend test --home "$home" --output json >"$out/bob-disclosure.json"
bob_address="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["address"])' "$out/bob-shielded.json")"
bob_disclosure="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["public_key_hex"])' "$out/bob-disclosure.json")"

deposit_notes() {
	local label="$1"; shift
	local i=0 amount tx_file tx_hash
	for amount in "$@"; do
		i=$((i+1)); tx_file="$out/${label}-deposit-${i}.json"
		run tx privacy deposit "${amount}uclair" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 3000000 --gas-prices "$gas_prices" --yes --output json >"$tx_file"
		tx_hash="$(tx_hash_from_file "$tx_file")"
		wait_tx "$tx_hash" "$out/${label}-deposit-${i}-query.json"
	done
	run tx privacy list-notes --from alice --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/${label}-alice-notes.json"
}

select_input_args() {
	local notes_file="$1"; shift
	python3 - "$notes_file" "$@" <<'PY'
import json,sys
notes = json.load(open(sys.argv[1]))["notes"]
required = list(map(str, sys.argv[2:]))
available = [n for n in notes if n["status"] == "spendable"]
selected = []
for amount in required:
    match = next((n for n in available if str(n["amount"]) == amount), None)
    if match is None: raise SystemExit(f"missing spendable input amount {amount}")
    available.remove(match); selected.append(match)
for note in selected:
    print(f'--input-index={note["index"]}')
PY
}

prepare_prove_broadcast() {
	local label="$1" output_mode="$2" self_view="$3"; shift 3
	local split="$1"; shift
	local -a input_amounts=("${@:1:split}")
	shift "$split"
	local -a payments=("$@") input_args=() prepare_args=()
	while IFS= read -r input_arg; do
		input_args+=("$input_arg")
	done < <(select_input_args "$out/${label}-alice-notes.json" "${input_amounts[@]}")
	for payment in "${payments[@]}"; do prepare_args+=(--payment "$payment"); done
	prepare_args+=("${input_args[@]}" --output-mode "$output_mode" --prepared-out "$out/${label}-prepared.json" --expires-in "$expires_in" --rescan-wallet)
	if [[ "$self_view" == "disabled" ]]; then prepare_args+=(--no-self-view); fi
	run tx privacy prepare-batch-transfer "${prepare_args[@]}" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --output json >"$out/${label}-prepare-command.json"
	run tx privacy prove-batch-transfer "$out/${label}-prepared.json" --proof-out "$out/${label}-proof.json" --prover-url "http://127.0.0.1:${proverd_port}" --output json >"$out/${label}-prove-command.json"
	run tx privacy broadcast-batch-transfer "$out/${label}-prepared.json" "$out/${label}-proof.json" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas "$batch_gas" --gas-prices "$gas_prices" --yes --output json >"$out/${label}-broadcast.json"
	local tx_hash="$(tx_hash_from_file "$out/${label}-broadcast.json")"
	wait_tx "$tx_hash" "$out/${label}-broadcast-query.json"
	printf '%s\n' "$tx_hash" >"$out/${label}.txhash"
}

deposit_notes one-input-one-payment 7
prepare_prove_broadcast one-input-one-payment compact enabled 1 7 "${bob_address},7uclair"

deposit_notes three-input-four-output-mixed-disclosure 5 7 9
prepare_prove_broadcast three-input-four-output-mixed-disclosure compact enabled 3 5 7 9 \
	"${bob_address},4uclair" \
	"${bob_address},5uclair,amount,public" \
	"${bob_address},6uclair,amount-from-to,recipient-encrypted,${bob_disclosure}"

sixteen_hundreds=(); for _ in $(seq 1 16); do sixteen_hundreds+=(100); done
thirty_one_payments=(); for _ in $(seq 1 31); do thirty_one_payments+=("${bob_address},50uclair"); done
deposit_notes thirty-one-payments-plus-change "${sixteen_hundreds[@]}"
prepare_prove_broadcast thirty-one-payments-plus-change compact disabled 16 "${sixteen_hundreds[@]}" "${thirty_one_payments[@]}"

sixteen_sixty_fours=(); for _ in $(seq 1 16); do sixteen_sixty_fours+=(64); done
thirty_two_payments=(); for _ in $(seq 1 32); do thirty_two_payments+=("${bob_address},32uclair"); done
deposit_notes exact-thirty-two-payments "${sixteen_sixty_fours[@]}"
prepare_prove_broadcast exact-thirty-two-payments compact enabled 16 "${sixteen_sixty_fours[@]}" "${thirty_two_payments[@]}"

deposit_notes explicit-zero-padding 5
prepare_prove_broadcast explicit-zero-padding exact32 disabled 1 5 "${bob_address},5uclair"

# Restart both processes, then reconcile the already-included exact32 tx by
# hash. Re-broadcasting the consumed-nullifier payload must fail closed rather
# than create a second effect.
kill "$proverd_pid"; wait "$proverd_pid" || true; proverd_pid=""
"$clairveil_proverd" --listen "127.0.0.1:${proverd_port}" >"$proverd_log.restart" 2>&1 & proverd_pid=$!
wait_for_proverd
kill "$node_pid"; wait "$node_pid" || true; node_pid=""
run start --home "$home" --minimum-gas-prices 0uclair >"$node_log.restart" 2>&1 & node_pid=$!
wait_for_node
run query tx "$(cat "$out/exact-thirty-two-payments.txhash")" --node "$node" --output json >"$out/restart-tx-hash-reconcile.json"
set +e
run tx privacy broadcast-batch-transfer "$out/exact-thirty-two-payments-prepared.json" "$out/exact-thirty-two-payments-proof.json" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas "$batch_gas" --gas-prices "$gas_prices" --yes --output json >"$out/retry-broadcast.json" 2>"$out/retry-broadcast.stderr"
retry_status=$?
set -e
retry_query="$out/retry-broadcast-query.json"
if [[ "$retry_status" == "0" ]]; then
	retry_tx_hash="$(tx_hash_from_file "$out/retry-broadcast.json")"
	wait_tx "$retry_tx_hash" "$retry_query"
	if python3 - "$retry_query" <<'PY'
import json,sys
raise SystemExit(0 if int(json.load(open(sys.argv[1])).get("code", 0)) == 0 else 1)
PY
	then
		echo "spent-nullifier retry unexpectedly succeeded on chain" >&2
		exit 1
	fi
fi
if ! grep -Eqi 'nullifier|spent' "$out/retry-broadcast.json" "$out/retry-broadcast.stderr" "$retry_query" 2>/dev/null; then
	echo "retry failed without spent-nullifier evidence" >&2
	exit 1
fi

run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/bob-notes-after.json"
python3 - "$out" <<'PY'
import json,sys
from pathlib import Path
out = Path(sys.argv[1])
labels = ["one-input-one-payment", "three-input-four-output-mixed-disclosure", "thirty-one-payments-plus-change", "exact-thirty-two-payments", "explicit-zero-padding"]
for label in labels:
    assert (out / f"{label}-prepared.json").stat().st_mode & 0o777 == 0o600
    assert (out / f"{label}-proof.json").stat().st_mode & 0o777 == 0o600
    tx = json.loads((out / f"{label}-broadcast-query.json").read_text())
    assert int(tx.get("code", 0)) == 0
assert json.loads((out / "bob-notes-after.json").read_text())["notes"]
summary = {
    "schema_version": "clairveil.batch-transfer.localnet-result.v1",
    "status": "passed",
    "cases": labels,
    "prover_route": "/v1/proofs/batch-transfer",
    "restart_tx_hash_reconciled": True,
    "spent_nullifier_retry_rejected": True,
    "automatic_multi_prover_failover": False,
}
(out / "session3b-localnet-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
PY

echo "Session 3B batch localnet passed: $out/session3b-localnet-summary.json"
