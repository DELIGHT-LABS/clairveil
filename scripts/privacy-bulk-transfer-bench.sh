#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export BENCH_OUT_DIR="${BENCH_OUT_DIR:-benchmarks/privacy-bulk-transfer}"
export RESULT_FAMILY="${RESULT_FAMILY:-privacy-bulk-transfer}"
export RUN_PROFILE="${RUN_PROFILE:-smoke}"
export BULK_SCENARIO="${BULK_SCENARIO:-single-company-100k}"
export BULK_RECIPIENTS="${BULK_RECIPIENTS:-100000}"
export BULK_TENANTS="${BULK_TENANTS:-1}"
export BULK_RECIPIENTS_PER_TENANT="${BULK_RECIPIENTS_PER_TENANT:-0}"
export BULK_CHUNK_SIZE="${BULK_CHUNK_SIZE:-20}"
export BULK_PROVER_UNITS="${BULK_PROVER_UNITS:-1}"
export BULK_PROOFS_PER_SEC="${BULK_PROOFS_PER_SEC:-6.92638}"
export BULK_TX_PER_SEC="${BULK_TX_PER_SEC:-1}"

mkdir -p "$BENCH_OUT_DIR"
summary_path="$BENCH_OUT_DIR/bulk-summary.json"
run_started_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

echo "running privacy bulk transfer benchmark simulation"
echo "  BENCH_OUT_DIR=$BENCH_OUT_DIR"
echo "  RESULT_FAMILY=$RESULT_FAMILY"
echo "  RUN_PROFILE=$RUN_PROFILE"
echo "  BULK_SCENARIO=$BULK_SCENARIO"
echo "  BULK_RECIPIENTS=$BULK_RECIPIENTS"
echo "  BULK_TENANTS=$BULK_TENANTS"
echo "  BULK_RECIPIENTS_PER_TENANT=$BULK_RECIPIENTS_PER_TENANT"
echo "  BULK_CHUNK_SIZE=$BULK_CHUNK_SIZE"
echo "  BULK_PROVER_UNITS=$BULK_PROVER_UNITS"
echo "  BULK_PROOFS_PER_SEC=$BULK_PROOFS_PER_SEC"
echo "  BULK_TX_PER_SEC=$BULK_TX_PER_SEC"

go run ./cmd/clairveil-bulktransferbench \
  -out "$summary_path" \
  -scenario "$BULK_SCENARIO" \
  -recipients "$BULK_RECIPIENTS" \
  -tenants "$BULK_TENANTS" \
  -recipients-per-tenant "$BULK_RECIPIENTS_PER_TENANT" \
  -chunk-size "$BULK_CHUNK_SIZE" \
  -prover-units "$BULK_PROVER_UNITS" \
  -proofs-per-sec "$BULK_PROOFS_PER_SEC" \
  -tx-per-sec "$BULK_TX_PER_SEC"

run_ended_at="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"

go run ./cmd/clairveil-benchreport \
  -benchmark-summaries "$summary_path" \
  -out "$BENCH_OUT_DIR" \
  -result-family "$RESULT_FAMILY" \
  -run-profile "$RUN_PROFILE" \
  -run-started-at "$run_started_at" \
  -run-ended-at "$run_ended_at"
