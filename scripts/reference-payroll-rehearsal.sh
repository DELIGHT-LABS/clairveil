#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

out_dir="${REHEARSAL_OUT_DIR:-benchmarks/reference-payroll-rehearsal}"
chunk_size="${BULK_CHUNK_SIZE:-20}"
prover_units="${BULK_PROVER_UNITS:-1}"
proofs_per_sec="${BULK_PROOFS_PER_SEC:-6.92638}"
tx_per_sec="${BULK_TX_PER_SEC:-1}"
run_localnet="${RUN_LOCALNET:-0}"
localnet_count="${LOCALNET_PAYROLL_ITEM_COUNT:-2}"

mkdir -p "$out_dir/scenarios"

run_scenario() {
	local name="$1"
	local tenants="$2"
	local recipients="$3"
	local recipients_per_tenant="$4"
	local path="$out_dir/scenarios/${name}.json"
	echo "running rehearsal scenario: $name"
	go run ./cmd/clairveil-bulktransferbench \
		-out "$path" \
		-scenario "$name" \
		-tenants "$tenants" \
		-recipients "$recipients" \
		-recipients-per-tenant "$recipients_per_tenant" \
		-chunk-size "$chunk_size" \
		-prover-units "$prover_units" \
		-proofs-per-sec "$proofs_per_sec" \
		-tx-per-sec "$tx_per_sec" >/dev/null
}

run_scenario "single-company-1k" 1 1000 0
run_scenario "single-company-10k" 1 10000 0
run_scenario "single-company-100k" 1 100000 0
run_scenario "hundred-companies-1k" 100 0 1000

localnet_summary=""
if [[ "$run_localnet" == "1" ]]; then
	echo "running optional live localnet payroll smoke with $localnet_count items"
	localnet_dir="$out_dir/live-localnet"
	CLAIRVEIL_PAYROLL_LIVE_WORK_DIR="$localnet_dir" PAYROLL_ITEM_COUNT="$localnet_count" ./scripts/reference-payroll-live-localnet.sh
	localnet_summary="$localnet_dir/out/payroll-final-report.json"
fi

python3 - "$out_dir" "$chunk_size" "$prover_units" "$proofs_per_sec" "$tx_per_sec" "$run_localnet" "$localnet_summary" <<'PY'
import json
import sys
from datetime import datetime, timezone
from pathlib import Path

out_dir = Path(sys.argv[1])
chunk_size = int(sys.argv[2])
prover_units = int(sys.argv[3])
proofs_per_sec = float(sys.argv[4])
tx_per_sec = float(sys.argv[5])
run_localnet = sys.argv[6] == "1"
localnet_summary = sys.argv[7]

def metric(summary, name):
    return summary["metric_summaries"][name]["mean"]

scenarios = []
for path in sorted((out_dir / "scenarios").glob("*.json")):
    payload = json.loads(path.read_text())
    bench = payload["benchmarks"][0]
    scenarios.append({
        "name": bench["load_profile"],
        "source_file": str(path),
        "tenant_count": int(metric(bench, "tenant_count")),
        "recipient_count": int(metric(bench, "recipient_count")),
        "proof_count": int(metric(bench, "proof_count")),
        "tx_envelope_count": int(metric(bench, "tx_envelope_count")),
        "estimated_total_seconds": metric(bench, "estimated_total_seconds"),
        "estimated_total_minutes": metric(bench, "estimated_total_seconds") / 60,
        "estimated_total_hours": metric(bench, "estimated_total_seconds") / 3600,
        "payroll_items_per_sec": metric(bench, "payroll_item_per_sec"),
    })

report = {
    "schema_version": "clairveil.reference_payroll_rehearsal.v1",
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "profile": {
        "chunk_size": chunk_size,
        "prover_units": prover_units,
        "proofs_per_sec_per_unit": proofs_per_sec,
        "tx_per_sec": tx_per_sec,
    },
    "scenarios": scenarios,
    "localnet_smoke": {
        "enabled": run_localnet,
        "final_report": localnet_summary if run_localnet else "",
    },
    "interpretation": {
        "single_company_100k": "legacy multi-message comparison keeps proof_count at 100000; tx envelopes shrink by chunk_size",
        "hundred_companies_1k": "same total recipient count as 100k, but tenant scheduling can spread proof/broadcast peaks",
        "next_decision": "measure the current BatchJoinSplit16x32 one-proof path; if the frozen 16x32 shape misses SLA, open a separately named circuit/protocol roadmap and security review",
    },
}
(out_dir / "rehearsal-summary.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
(out_dir / "latest-rehearsal-summary.json").write_text(json.dumps(report, indent=2, sort_keys=True) + "\n")
print(f"reference payroll rehearsal summary written to {out_dir / 'rehearsal-summary.json'}")
PY

cat <<EOF
Reference payroll rehearsal completed.

Summary:   $out_dir/rehearsal-summary.json
Latest:    $out_dir/latest-rehearsal-summary.json
Scenarios: $out_dir/scenarios
EOF
