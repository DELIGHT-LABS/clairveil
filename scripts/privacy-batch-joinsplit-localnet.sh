#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$repo_root/x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json"
run_localnet="${RUN_LOCALNET:-0}"
default_work_dir="$repo_root/tmp/privacy-batch-joinsplit-localnet"
work_dir="${CLAIRVEIL_BATCH_LOCALNET_WORK_DIR:-$default_work_dir}"
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

prepare_work_dir() {
	local canonical_work_dir canonical_default_work_dir marker_name marker_value
	marker_name=".clairveil-batch-localnet-work-dir"
	marker_value="clairveil-batch-localnet-work-dir-v1"
	if [[ -L "$work_dir" ]]; then
		echo "refusing symlink CLAIRVEIL_BATCH_LOCALNET_WORK_DIR: $work_dir" >&2
		return 1
	fi
	canonical_work_dir="$(python3 - "$work_dir" "$repo_root" <<'PY'
import os
import pwd
import sys
import tempfile
from pathlib import Path

candidate = Path(sys.argv[1]).expanduser().resolve(strict=False)
repo = Path(sys.argv[2]).resolve(strict=False)
homes = {Path.home().resolve(strict=False), Path(pwd.getpwuid(os.getuid()).pw_dir).resolve(strict=False)}
temp_root = Path(tempfile.gettempdir()).resolve(strict=False)
protected = homes | {Path("/"), repo, temp_root}
if candidate in protected or candidate in repo.parents or any(candidate in home.parents for home in homes):
    raise SystemExit(f"refusing unsafe CLAIRVEIL_BATCH_LOCALNET_WORK_DIR: {candidate}")
print(candidate)
PY
)"
	canonical_default_work_dir="$(python3 - "$default_work_dir" "$repo_root" <<'PY'
import sys
from pathlib import Path
candidate = Path(sys.argv[1]).resolve(strict=False)
repo = Path(sys.argv[2]).resolve(strict=False)
if repo not in candidate.parents:
    raise SystemExit(f"refusing default work directory outside repository: {candidate}")
print(candidate)
PY
)"
	if [[ -e "$canonical_work_dir" ]]; then
		if [[ ! -d "$canonical_work_dir" ]]; then
			echo "CLAIRVEIL_BATCH_LOCALNET_WORK_DIR is not a directory: $canonical_work_dir" >&2
			return 1
		fi
		if [[ "$canonical_work_dir" == "$canonical_default_work_dir" ]] || { [[ -f "$canonical_work_dir/$marker_name" ]] && grep -Fxq "$marker_value" "$canonical_work_dir/$marker_name"; }; then
			rm -rf -- "$canonical_work_dir"
		elif find "$canonical_work_dir" -mindepth 1 -maxdepth 1 -print -quit | grep -q .; then
			echo "refusing to delete unmarked non-empty work directory: $canonical_work_dir" >&2
			return 1
		fi
	fi
	mkdir -p -- "$canonical_work_dir"
	chmod 700 "$canonical_work_dir"
	printf '%s\n' "$marker_value" >"$canonical_work_dir/$marker_name"
	chmod 600 "$canonical_work_dir/$marker_name"
	work_dir="$canonical_work_dir"
}

python3 - "$fixture" <<'PY'
import json
import sys
from pathlib import Path

doc = json.loads(Path(sys.argv[1]).read_text())
assert doc["schema_version"] == "clairveil.batch-transfer.contract.v1"
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
print("Batch transfer contract fixture validation passed.")
PY

(cd "$repo_root" && go test ./x/privacy/client/sdk/conformance -run TestBatchTransferContract -count=1)

if [[ "$run_localnet" == "0" ]]; then
	echo "Static batch transfer validation passed. Set RUN_LOCALNET=1 for the actual node/prover workflow."
	exit 0
fi
if [[ "$run_localnet" != "1" ]]; then
	echo "RUN_LOCALNET must be 0 or 1" >&2
	exit 1
fi

prepare_work_dir
mkdir -p "$work_dir/out" "$work_dir/home"
home="$work_dir/home"
out="$work_dir/out"
artifacts="${artifact_override:-$work_dir/artifacts}"
node_log="$work_dir/clairveild.log"
proverd_log="$work_dir/clairveil-proverd.log"
node_pid=""
proverd_pid=""
payroll_live_test_bin="$work_dir/batch-payroll-live.test"
payroll_store="$out/batch-payroll-reservations.json"
failover_evidence="$out/prover-failover-evidence.json"
failover_test_log="$out/prover-failover-test.log"

(
	cd "$repo_root"
	CLAIRVEIL_PROVER_FAILOVER_EVIDENCE_OUT="$failover_evidence" \
		go test ./x/privacy/client/sdk/payroll -run '^TestProverPoolLiveHTTPFailoverPrivacyBoundary$' -count=1 -v
) | tee "$failover_test_log"
if ! grep -Fq -- '--- PASS: TestProverPoolLiveHTTPFailoverPrivacyBoundary' "$failover_test_log"; then
	echo "live HTTP prover failover regression did not execute and pass" >&2
	exit 1
fi
if grep -Eq -- '(--- SKIP:|\[no tests to run\])' "$failover_test_log"; then
	echo "live HTTP prover failover regression was skipped or did not run" >&2
	exit 1
fi

clairveild="${CLAIRVEILD_BIN:-$work_dir/clairveild-batch-localnet}"
clairveil_setup="${CLAIRVEIL_SETUP_BIN:-$work_dir/clairveil-setup-batch-localnet}"
clairveil_proverd="${CLAIRVEIL_PROVERD_BIN:-$work_dir/clairveil-proverd-batch-localnet}"
grpcurl_bin="${GRPCURL_BIN:-$(command -v grpcurl || true)}"

if [[ -z "$grpcurl_bin" ]]; then
	echo "grpcurl is required for typed scan/genesis round-trip validation" >&2
	exit 1
fi

cleanup() {
	if [[ -n "$proverd_pid" ]]; then kill "$proverd_pid" >/dev/null 2>&1 || true; wait "$proverd_pid" >/dev/null 2>&1 || true; fi
	if [[ -n "$node_pid" ]]; then kill "$node_pid" >/dev/null 2>&1 || true; wait "$node_pid" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT

if [[ -z "${CLAIRVEILD_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveild" ./cmd/clairveild); fi
if [[ -z "$artifact_override" && -z "${CLAIRVEIL_SETUP_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveil_setup" ./cmd/clairveil-setup); fi
if [[ -z "${CLAIRVEIL_PROVERD_BIN:-}" ]]; then (cd "$repo_root" && go build -o "$clairveil_proverd" ./cmd/clairveil-proverd); fi
(cd "$repo_root" && go test -tags=batch_payroll_localnet -c -o "$payroll_live_test_bin" ./x/privacy/client/cli)
chmod 700 "$payroll_live_test_bin"

run() { "$clairveild" "$@"; }

patch_ports() {
	local target_home="${1:-$home}"
	python3 - "$target_home" "$rpc_port" "$p2p_port" "$abci_port" "$grpc_port" "$api_port" "$pprof_port" <<'PY'
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
		if [[ -z "$node_pid" ]] || ! kill -0 "$node_pid" >/dev/null 2>&1; then
			tail -200 "$node_log" >&2
			return 1
		fi
		if run status --node "$node" 2>/dev/null | python3 -c 'import json,sys; d=json.load(sys.stdin); raise SystemExit(0 if int(d["sync_info"]["latest_block_height"]) >= 1 else 1)' >/dev/null 2>&1; then return 0; fi
		sleep 1
	done
	tail -200 "$node_log" >&2
	return 1
}

restart_node_process() {
	local label="$1" previous_height="0" current_height
	if [[ -n "$node_pid" ]] && kill -0 "$node_pid" >/dev/null 2>&1; then
		previous_height="$(run status --node "$node" 2>/dev/null | python3 -c 'import json,sys; print(int(json.load(sys.stdin)["sync_info"]["latest_block_height"]))')"
	fi
	if [[ -n "$node_pid" ]]; then
		kill "$node_pid"
		wait "$node_pid" || true
		node_pid=""
	fi
	node_log="$work_dir/clairveild.${label}.log"
	"$clairveild" start --home "$home" --minimum-gas-prices 0uclair >"$node_log" 2>&1 & node_pid=$!
	wait_for_node
	for _ in $(seq 1 60); do
		current_height="$(run status --node "$node" 2>/dev/null | python3 -c 'import json,sys; print(int(json.load(sys.stdin)["sync_info"]["latest_block_height"]))' || true)"
		if [[ "$current_height" =~ ^[0-9]+$ ]] && (( current_height > previous_height )); then
			return 0
		fi
		sleep 1
	done
	echo "restarted node did not finalize a block above height $previous_height" >&2
	tail -200 "$node_log" >&2
	return 1
}

wait_for_proverd() {
	for _ in $(seq 1 120); do
		if [[ -z "$proverd_pid" ]] || ! kill -0 "$proverd_pid" >/dev/null 2>&1; then
			tail -200 "$proverd_log" >&2
			return 1
		fi
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

grpc_query_file() {
	local method="$1" request_file="$2" response_file="$3"
	"$grpcurl_bin" -plaintext -d @ "127.0.0.1:${grpc_port}" "clairveil.privacy.v1.Query/${method}" <"$request_file" >"$response_file"
}

snapshot_public_state() {
	local prefix="$1"
	printf '{}\n' >"${prefix}-empty-request.json"
	printf '{"denom":"uclair"}\n' >"${prefix}-reserve-request.json"
	printf '{"canonicalDenom":"uclair"}\n' >"${prefix}-asset-request.json"
	printf '{"outputLimit":512,"eventLimit":256,"maxEncodedBytes":"4194304"}\n' >"${prefix}-scan-request.json"
	grpc_query_file TreeState "${prefix}-empty-request.json" "${prefix}-tree.json"
	grpc_query_file Reserve "${prefix}-reserve-request.json" "${prefix}-reserve.json"
	grpc_query_file AssetByDenom "${prefix}-asset-request.json" "${prefix}-asset.json"
	grpc_query_file CircuitConfig "${prefix}-empty-request.json" "${prefix}-circuit.json"
	grpc_query_file PrivacyScan "${prefix}-scan-request.json" "${prefix}-scan.json"
	python3 - "${prefix}-tree.json" "${prefix}-scan.json" "${prefix}-paths-request.json" <<'PY'
import base64
import json
import sys
from pathlib import Path

tree = json.loads(Path(sys.argv[1]).read_text())
scan = json.loads(Path(sys.argv[2]).read_text())
outputs = scan.get("outputs", [])
if not outputs:
    raise SystemExit("privacy scan snapshot contains no outputs")
selected = [outputs[0]]
if len(outputs) > 1:
    selected.append(outputs[-1])
request = {
    "commitmentHexes": [base64.b64decode(output["commitment"]).hex() for output in selected],
    "rootHex": tree["root"],
}
Path(sys.argv[3]).write_text(json.dumps(request) + "\n")
PY
	grpc_query_file CommitmentPathsAtRoot "${prefix}-paths-request.json" "${prefix}-paths.json"
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
chmod 600 "$out/alice-key.json" "$out/bob-key.json" "$out/auditor-key.json"
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

"$clairveild" start --home "$home" --minimum-gas-prices 0uclair >"$node_log" 2>&1 & node_pid=$!
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

prepare_payroll_payload() {
	local label="$1"; shift
	local split="$1"; shift
	local -a input_amounts=("${@:1:split}")
	shift "$split"
	local -a payments=("$@") input_args=() prepare_args=()
	while IFS= read -r input_arg; do
		input_args+=("$input_arg")
	done < <(select_input_args "$out/${label}-alice-notes.json" "${input_amounts[@]}")
	for payment in "${payments[@]}"; do prepare_args+=(--payment "$payment"); done
	prepare_args+=("${input_args[@]}" --output-mode compact --prepared-out "$out/${label}-prepared.json" --expires-in "$expires_in" --rescan-wallet)
	run tx privacy prepare-batch-transfer "${prepare_args[@]}" --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --output json >"$out/${label}-prepare-command.json"
}

run_payroll_stage() {
	local stage="$1"
	CLAIRVEIL_BATCH_PAYROLL_STAGE="$stage" \
	CLAIRVEIL_BATCH_PAYROLL_HOME="$home" \
	CLAIRVEIL_BATCH_PAYROLL_OUT_DIR="$out" \
	CLAIRVEIL_BATCH_PAYROLL_STORE_PATH="$payroll_store" \
	CLAIRVEIL_BATCH_PAYROLL_NODE="$node" \
	CLAIRVEIL_BATCH_PAYROLL_GRPC_ADDR="127.0.0.1:${grpc_port}" \
	CLAIRVEIL_BATCH_PAYROLL_CHAIN_ID="$chain_id" \
	CLAIRVEIL_BATCH_PAYROLL_PROVER_URL="http://127.0.0.1:${proverd_port}" \
	CLAIRVEIL_BATCH_PAYROLL_GAS="$batch_gas" \
	CLAIRVEIL_BATCH_PAYROLL_GAS_PRICES="$gas_prices" \
	CLAIRVEIL_BATCH_PAYROLL_ALICE_NOTES_PATH="$out/three-input-four-output-mixed-disclosure-alice-notes.json" \
	CLAIRVEIL_BATCH_PAYROLL_BOB_ADDRESS="$bob_address" \
	CLAIRVEIL_BATCH_PAYROLL_BOB_DISCLOSURE_PUBKEY="$bob_disclosure" \
	CLAIRVEIL_BATCH_PAYROLL_PREPARED_PATH="$out/three-input-four-output-mixed-disclosure-prepared.json" \
	CLAIRVEIL_BATCH_PAYROLL_PROOF_PATH="$out/three-input-four-output-mixed-disclosure-proof.json" \
	"$payroll_live_test_bin" -test.run '^TestOneProofBatchPayrollLocalnet$' -test.v | tee "$out/payroll-${stage}.log"
}

deposit_notes one-input-one-payment 7
prepare_prove_broadcast one-input-one-payment compact enabled 1 7 "${bob_address},7uclair"

deposit_notes three-input-four-output-mixed-disclosure 5 7 9
prepare_payroll_payload three-input-four-output-mixed-disclosure 3 5 7 9 \
	"${bob_address},4uclair" \
	"${bob_address},5uclair,amount,public" \
	"${bob_address},9uclair,amount-from-to,recipient-encrypted,${bob_disclosure}"
run_payroll_stage graph
run_payroll_stage prove
run_payroll_stage timeout
payroll_node_pid_before="$node_pid"
restart_node_process payroll-retry
payroll_node_pid_after="$node_pid"
if [[ "$payroll_node_pid_before" == "$payroll_node_pid_after" ]]; then
	echo "payroll node restart did not replace the process" >&2
	exit 1
fi
printf '{"before_pid":%s,"after_pid":%s}\n' "$payroll_node_pid_before" "$payroll_node_pid_after" >"$out/payroll-node-restart.json"
run_payroll_stage retry
payroll_tx_hash="$(cat "$out/payroll.txhash")"
wait_tx "$payroll_tx_hash" "$out/three-input-four-output-mixed-disclosure-broadcast-query.json"
printf '%s\n' "$payroll_tx_hash" >"$out/three-input-four-output-mixed-disclosure.txhash"
run_payroll_stage reconcile
run_payroll_stage conflict

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
proverd_log="$work_dir/clairveil-proverd.restart.log"
"$clairveil_proverd" --listen "127.0.0.1:${proverd_port}" >"$proverd_log" 2>&1 & proverd_pid=$!
wait_for_proverd
restart_node_process restart
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

# Capture every consensus-facing identity required by the typed scanner, stop
# both processes cleanly, export a non-zero-height genesis, and restore it into
# a fresh node home. A non-zero-height export is intentional: the frozen scan
# cursor is ordered by (height, global_sequence, output_index), so subsequent
# blocks must remain above the imported historical records.
snapshot_public_state "$out/pre-export"
run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --json >"$out/pre-export-bob-wallet.json"
kill "$proverd_pid"; wait "$proverd_pid" || true; proverd_pid=""
kill "$node_pid"; wait "$node_pid" || true; node_pid=""
run export --home "$home" --output-document "$out/exported-genesis.json" >"$out/export.stdout" 2>"$out/export.stderr"

restored_home="$work_dir/restored-home"
run init batch-restored --chain-id "$chain_id" --home "$restored_home" >"$out/restored-init.stdout" 2>"$out/restored-init.stderr"
cp "$home/config/priv_validator_key.json" "$restored_home/config/priv_validator_key.json"
cp "$out/exported-genesis.json" "$restored_home/config/genesis.json"
chmod 600 "$restored_home/config/priv_validator_key.json" "$restored_home/config/genesis.json"
patch_ports "$restored_home"
run validate --home "$restored_home" >"$out/restored-validate.stdout" 2>"$out/restored-validate.stderr"

node_log="$work_dir/clairveild.restored.log"
"$clairveild" start --home "$restored_home" --minimum-gas-prices 0uclair >"$node_log" 2>&1 & node_pid=$!
wait_for_node
proverd_log="$work_dir/clairveil-proverd.restored.log"
"$clairveil_proverd" --listen "127.0.0.1:${proverd_port}" >"$proverd_log" 2>&1 & proverd_pid=$!
wait_for_proverd

snapshot_public_state "$out/post-restore"
run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --json >"$out/post-restore-bob-wallet-cached.json"
run tx privacy list-notes --from bob --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/post-restore-bob-wallet-rescan.json"

python3 - "$out" <<'PY'
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
for suffix in ["tree", "reserve", "asset", "circuit", "scan", "paths"]:
    before = json.loads((out / f"pre-export-{suffix}.json").read_text())
    restored = json.loads((out / f"post-restore-{suffix}.json").read_text())
    if before != restored:
        raise SystemExit(f"genesis round trip changed {suffix}")

genesis = json.loads((out / "exported-genesis.json").read_text())
initial_height = int(genesis["initial_height"])
scan = json.loads((out / "pre-export-scan.json").read_text())
summaries = scan["summaries"]
outputs = scan["outputs"]
if initial_height <= max(int(summary["height"]) for summary in summaries):
    raise SystemExit("exported initial height does not preserve scan cursor ordering")
if scan.get("hasMore", False):
    raise SystemExit("pre-export scan snapshot was unexpectedly paginated")
cursors = [(int(item["height"]), int(item["globalSequence"]), int(item.get("outputIndex", 0))) for item in outputs]
if cursors != sorted(cursors) or len(cursors) != len(set(cursors)):
    raise SystemExit("pre-export scan cursor order is unstable or duplicated")

def note_identity(path):
    notes = json.loads(path.read_text())["notes"]
    return sorted((n["index"], n["status"], str(n["amount"]), n.get("nullifier", ""), n.get("tx_hash", ""), int(n.get("height", 0))) for n in notes)

expected_notes = note_identity(out / "bob-notes-after.json")
if note_identity(out / "pre-export-bob-wallet.json") != expected_notes:
    raise SystemExit("cached wallet changed before export")
if note_identity(out / "post-restore-bob-wallet-cached.json") != expected_notes:
    raise SystemExit("cached wallet changed after genesis import")
if note_identity(out / "post-restore-bob-wallet-rescan.json") != expected_notes:
    raise SystemExit("wallet cursor resume duplicated or lost notes after genesis import")

(out / "genesis-roundtrip-check.json").write_text(json.dumps({
    "initial_height": initial_height,
    "summary_count": len(summaries),
    "output_count": len(outputs),
    "last_global_sequence": max(int(summary["globalSequence"]) for summary in summaries),
    "wallet_note_count": len(expected_notes),
}, indent=2) + "\n")
PY

# Append one fresh event after the imported state. This proves that sequence,
# reserve, asset, tree, path and wallet cursor state do not merely deserialize;
# they continue without collision or rollback after restart.
run tx privacy deposit 1uclair --from alice --keyring-backend test --home "$home" --node "$node" --chain-id "$chain_id" --gas 3000000 --gas-prices "$gas_prices" --yes --output json >"$out/post-restore-deposit.json"
post_restore_tx_hash="$(tx_hash_from_file "$out/post-restore-deposit.json")"
wait_tx "$post_restore_tx_hash" "$out/post-restore-deposit-query.json"
snapshot_public_state "$out/post-continuation"
run tx privacy list-notes --from alice --keyring-backend test --home "$home" --node "$node" --rescan-wallet --json >"$out/post-continuation-alice-wallet.json"

python3 - "$out" "$post_restore_tx_hash" "$failover_evidence" <<'PY'
import json,sys
import base64
from pathlib import Path
out = Path(sys.argv[1])
continued_tx_hash = sys.argv[2].lower()
failover = json.loads(Path(sys.argv[3]).read_text())
assert failover["schema_version"] == "clairveil.prover-failover-live-evidence.v1"
default_failover = failover["default_no_failover"]
assert default_failover["timeout_endpoint_contacts"] == 1
assert default_failover["healthy_endpoint_contacts"] == 0
assert default_failover["timeout_body_observed"]
assert not default_failover["healthy_body_observed"]
opt_in_failover = failover["explicit_opt_in_failover"]
assert opt_in_failover["timeout_endpoint_contacts"] == 1
assert opt_in_failover["healthy_endpoint_contacts"] == 1
assert opt_in_failover["timeout_body_observed"] and opt_in_failover["healthy_body_observed"]
assert opt_in_failover["bodies_equal"] and opt_in_failover["completed_from_healthy"]
failure_classes = failover["failure_classes"]
assert failure_classes["timeout_distinct"]
assert failure_classes["malformed_response_distinct"]
assert failure_classes["validation_failure_distinct"]
assert failure_classes["each_endpoint_contact_count_one"]
labels = ["one-input-one-payment", "three-input-four-output-mixed-disclosure", "thirty-one-payments-plus-change", "exact-thirty-two-payments", "explicit-zero-padding"]
for key_file in ["alice-key.json", "bob-key.json", "auditor-key.json"]:
    assert (out / key_file).stat().st_mode & 0o777 == 0o600
for label in labels:
    assert (out / f"{label}-prepared.json").stat().st_mode & 0o777 == 0o600
    assert (out / f"{label}-proof.json").stat().st_mode & 0o777 == 0o600
    tx = json.loads((out / f"{label}-broadcast-query.json").read_text())
    assert int(tx.get("code", 0)) == 0
assert json.loads((out / "bob-notes-after.json").read_text())["notes"]
payroll = json.loads((out / "batch-payroll-live-summary.json").read_text())
assert payroll["schema_version"] == "clairveil.batch-payroll.live-result.v1"
assert payroll["status"] == "passed"
assert payroll["process_restarted"]
assert payroll["timeout_before_send"]
assert payroll["exact_stored_bytes_retry"]
assert payroll["tx_hash_first_reconcile"]
assert payroll["spent_nullifier_conflict"]
assert payroll["input_count"] == 3 and payroll["output_count"] == 4
assert payroll["proof_count"] == 1 and payroll["tx_envelope_count"] == 1
assert payroll["broadcast_attempts"] == 2
assert payroll["succeeded_items"] == 3
assert payroll["conflict_manual_review_items"] == 3
assert payroll["disclosure_live_verified"]
assert payroll["view_tag_mismatch_safe"]
assert payroll["recipient_notes_verified"] == 4
assert payroll["user_disclosures_verified"] == 2
assert payroll["audit_disclosures_verified"] == 4
assert payroll["self_views_verified"] == 4
assert payroll["chain_status"] == "Succeeded"
assert payroll["conflict_chain_status"] == "ManualReview"
assert len(payroll["stage_pids"]) == 6 and len(set(payroll["stage_pids"].values())) == 6
node_restart = json.loads((out / "payroll-node-restart.json").read_text())
assert node_restart["before_pid"] != node_restart["after_pid"]

roundtrip = json.loads((out / "genesis-roundtrip-check.json").read_text())
before_tree = json.loads((out / "pre-export-tree.json").read_text())
after_tree = json.loads((out / "post-continuation-tree.json").read_text())
assert int(after_tree["leafCount"]) == int(before_tree["leafCount"]) + 1

before_scan = json.loads((out / "pre-export-scan.json").read_text())
after_scan = json.loads((out / "post-continuation-scan.json").read_text())
assert not after_scan.get("hasMore", False)
assert len(after_scan["summaries"]) == len(before_scan["summaries"]) + 1
assert len(after_scan["outputs"]) == len(before_scan["outputs"]) + 1
last_summary = after_scan["summaries"][-1]
last_output = after_scan["outputs"][-1]
assert int(last_summary["globalSequence"]) == roundtrip["last_global_sequence"] + 1
assert int(last_summary["height"]) >= roundtrip["initial_height"]
assert int(last_output["globalSequence"]) == int(last_summary["globalSequence"])
assert int(last_output.get("outputIndex", 0)) == 0
assert last_output.get("txHash", "").lower() == continued_tx_hash or base64.b64decode(last_output["txHash"]).hex() == continued_tx_hash

before_reserve = json.loads((out / "pre-export-reserve.json").read_text())
after_reserve = json.loads((out / "post-continuation-reserve.json").read_text())
assert int(after_reserve["totalDeposited"]) == int(before_reserve["totalDeposited"]) + 1
assert after_reserve["invariantHolds"]
assert json.loads((out / "post-continuation-asset.json").read_text()) == json.loads((out / "pre-export-asset.json").read_text())
assert json.loads((out / "post-continuation-circuit.json").read_text()) == json.loads((out / "pre-export-circuit.json").read_text())

paths = json.loads((out / "post-continuation-paths.json").read_text())
assert paths["rootHex"] == after_tree["root"]
assert int(paths["leafCount"]) == int(after_tree["leafCount"])
assert int(paths["paths"][-1]["leafIndex"]) == int(before_tree["leafCount"])

alice_wallet = json.loads((out / "post-continuation-alice-wallet.json").read_text())
nullifiers = [n.get("nullifier", "") for n in alice_wallet["notes"] if n.get("nullifier")]
assert len(nullifiers) == len(set(nullifiers))
assert any(n.get("tx_hash", "").lower() == continued_tx_hash for n in alice_wallet["notes"])
summary = {
    "schema_version": "clairveil.batch-transfer.localnet-result.v2",
    "status": "passed",
    "cases": labels,
    "prover_route": "/v1/proofs/batch-transfer",
    "restart_tx_hash_reconciled": True,
    "spent_nullifier_retry_rejected": True,
    "restart_retry_scope": "tx-hash-reconcile-spent-nullifier-rejection-and-nonzero-height-genesis-resume",
    "genesis_export_import_roundtrip": True,
    "typed_scan_cursor_resumed": True,
    "post_import_sequence_continued": True,
    "post_import_path_verified": True,
    "post_import_reserve_and_asset_verified": True,
    "wallet_duplicate_or_missing_notes": False,
    "default_no_failover_observed": default_failover,
    "explicit_opt_in_failover_observed": opt_in_failover,
    "prover_failure_classes": failure_classes,
    "automatic_multi_prover_failover": default_failover["healthy_endpoint_contacts"] > 0,
    "one_proof_payroll": payroll,
    "payroll_node_restart_observed": True,
}
(out / "batch-localnet-summary.json").write_text(json.dumps(summary, indent=2) + "\n")
PY

echo "Batch localnet validation passed: $out/batch-localnet-summary.json"
