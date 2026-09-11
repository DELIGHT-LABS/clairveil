#!/usr/bin/env bash
set -euo pipefail
umask 077

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture="$repo_root/x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json"
run_localnet="${RUN_LOCALNET:-0}"

# Keep the historical fixture as a static conformance gate. It does not start
# a node and is not V2 runtime evidence.
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
assert cases["three-input-four-output-mixed-disclosure"]["disclosure_modes"] == ["none", "public", "recipient-encrypted", "none"]
assert cases["thirty-one-payments-plus-change"]["expected_output_roles"].count("payment") == 31
assert cases["exact-thirty-two-payments"]["expected_output_roles"].count("payment") == 32
assert cases["explicit-zero-padding"]["expected_output_roles"].count("padding") == 31
print("Batch transfer contract fixture validation passed.")
PY
(cd "$repo_root" && go test ./x/privacy/client/sdk/conformance -run TestBatchTransferContract -count=1)

if [[ "$run_localnet" == "0" ]]; then
	echo "Static batch transfer validation passed. Set RUN_LOCALNET=1 for the reviewed V2 one-proof batch smoke."
	exit 0
fi
[[ "$run_localnet" == "1" ]] || { echo "RUN_LOCALNET must be 0 or 1" >&2; exit 1; }
exec "$repo_root/scripts/privacy-audit-v2-smoke.sh"
