#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

transfer_batch_count="${TRANSFER_BATCH_COUNT:-2}"
transfer_batch_amount="${TRANSFER_BATCH_AMOUNT:-1}"

if ! [[ "$transfer_batch_count" =~ ^[1-9][0-9]*$ ]]; then
  echo "TRANSFER_BATCH_COUNT must be a positive integer" >&2
  exit 1
fi
if ! [[ "$transfer_batch_amount" =~ ^[1-9][0-9]*$ ]]; then
  echo "TRANSFER_BATCH_AMOUNT must be a positive integer" >&2
  exit 1
fi

export BENCH_OUT_DIR="${BENCH_OUT_DIR:-benchmarks/privacy-transfer-batch-localnet}"
export RESULT_FAMILY="${RESULT_FAMILY:-privacy-transfer-batch-localnet}"
export RUN_PROFILE="${RUN_PROFILE:-smoke}"
export LOCALNET_LOAD_PROFILE="${LOCALNET_LOAD_PROFILE:-transfer_batch_k${transfer_batch_count}}"
export PRIVACY_E2E_BATCH_TRANSFER_COUNT="$transfer_batch_count"
export PRIVACY_E2E_BATCH_TRANSFER_AMOUNT="$transfer_batch_amount"
export PRIVACY_E2E_BATCH_TRANSFER_GAS="${PRIVACY_E2E_BATCH_TRANSFER_GAS:-$((transfer_batch_count * 9000000 + 3000000))}"

./scripts/privacy-bench-localnet.sh
