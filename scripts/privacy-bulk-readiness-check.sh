#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-bulk-readiness}"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
steps_file="$bench_out_dir/readiness-steps-$stamp.jsonl"
summary_file="$bench_out_dir/readiness-summary-$stamp.json"
latest_summary_file="$bench_out_dir/latest-readiness-summary.json"
source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
if [[ -n "$(git status --short --untracked-files=all -- . 2>/dev/null | awk 'NF { status=substr($0,1,2); path=substr($0,4); if (status=="??" && path ~ /^benchmarks\//) next; print }')" ]]; then
  source_dirty="true"
fi

mkdir -p "$bench_out_dir"
: >"$steps_file"

append_step() {
  python3 - "$steps_file" "$@" <<'PY'
import json
import sys
from pathlib import Path

path = Path(sys.argv[1])
record = {
    "name": sys.argv[2],
    "status": sys.argv[3],
    "required": sys.argv[4] == "1",
    "exit_code": int(sys.argv[5]),
    "started_at": sys.argv[6],
    "ended_at": sys.argv[7],
    "log_file": sys.argv[8],
}
with path.open("a") as fp:
    fp.write(json.dumps(record, sort_keys=True) + "\n")
PY
}

run_step() {
  local name="$1"
  local required="$2"
  shift 2
  local log_file="$bench_out_dir/${name}-${stamp}.log"
  local started_at
  local ended_at
  local exit_code
  started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "running readiness step: $name"
  set +e
  "$@" >"$log_file" 2>&1
  exit_code=$?
  set -e
  ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  if [[ "$exit_code" == "0" ]]; then
    append_step "$name" "passed" "$required" "$exit_code" "$started_at" "$ended_at" "$log_file"
  else
    append_step "$name" "failed" "$required" "$exit_code" "$started_at" "$ended_at" "$log_file"
  fi
  return "$exit_code"
}

skip_step() {
  local name="$1"
  local reason="$2"
  local marker_file="$bench_out_dir/${name}-${stamp}.log"
  printf '%s\n' "$reason" >"$marker_file"
  append_step "$name" "skipped" "0" "0" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$marker_file"
}

failed=0

run_step "bulk-critical-unit-tests" "1" \
  go test \
    ./x/privacy/client/sdk/reservation \
    ./x/privacy/client/sdk/payroll \
    ./cmd/clairveil-bulktransferbench \
    ./cmd/clairveil-proverload \
    ./cmd/clairveil-localnetload || failed=1

run_step "reservation-failure-invariants" "1" \
  go test ./x/privacy/client/sdk/reservation \
    -run 'TestService(ReserveRejectsActiveDuplicate|TransitionUsesCompareAndSet|LeaseRejectsConcurrentWorkerAndAllowsHeartbeat|LeaseRejectsStaleToken|TransitionWithLeaseRejectsStaleToken|ReconcileRequiresOperationEvidenceForSuccess)' || failed=1

run_step "bulk-synthetic-bench" "1" \
  env BENCH_OUT_DIR="$bench_out_dir/bulk-transfer" ./scripts/privacy-bulk-transfer-bench.sh || failed=1

if [[ "${RUN_LOCALNET:-0}" == "1" ]]; then
  run_step "transfer-batch-localnet" "1" \
    env BENCH_OUT_DIR="$bench_out_dir/transfer-batch-localnet" TRANSFER_BATCH_COUNT="${TRANSFER_BATCH_COUNT:-1}" ./scripts/privacy-transfer-batch-localnet-bench.sh || failed=1
else
  skip_step "transfer-batch-localnet" "skipped; set RUN_LOCALNET=1 to include localnet multi-message transfer validation"
fi

if [[ "${RUN_PROVER_SCALE:-0}" == "1" || -n "${PROVERD_URLS:-}" ]]; then
  if [[ -z "${PROVERD_URLS:-}" ]]; then
    run_step "prover-pool-scale" "1" bash -c 'echo "PROVERD_URLS is required when RUN_PROVER_SCALE=1" >&2; exit 1' || failed=1
  else
    run_step "prover-pool-scale" "1" \
      env BENCH_OUT_DIR="$bench_out_dir/proverd-scale" ./scripts/privacy-proverd-scale-bench.sh || failed=1
  fi
else
  skip_step "prover-pool-scale" "skipped; set RUN_PROVER_SCALE=1 and PROVERD_URLS=url1,url2 to include external prover pool validation"
fi

python3 - "$steps_file" "$summary_file" "$source_commit" "$source_dirty" <<'PY'
import json
import sys
from pathlib import Path
from datetime import datetime, timezone

steps_path = Path(sys.argv[1])
summary_path = Path(sys.argv[2])
steps = [json.loads(line) for line in steps_path.read_text().splitlines() if line.strip()]
failed_required = [step for step in steps if step["required"] and step["status"] != "passed"]
summary = {
    "schema_version": "clairveil.bulk_readiness.v1",
    "generated_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
    "source_commit": sys.argv[3],
    "source_dirty": sys.argv[4] == "true",
    "status": "failed" if failed_required else "passed",
    "steps": steps,
}
summary_path.write_text(json.dumps(summary, indent=2, sort_keys=True) + "\n")
PY
cp "$summary_file" "$latest_summary_file"

echo "bulk readiness summary written to $summary_file"
exit "$failed"
