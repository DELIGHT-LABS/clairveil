#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

export BENCH_OUT_DIR="${BENCH_OUT_DIR:-benchmarks/privacy-localnet-tps}"
export RESULT_FAMILY="${RESULT_FAMILY:-privacy-localnet-tps}"
export RUN_PROFILE="${RUN_PROFILE:-smoke}"
export CLAIM_TYPES="${CLAIM_TYPES:-chain_tps}"
export CLAIM_LOAD_PROFILE="${CLAIM_LOAD_PROFILE:-${LOCALNET_LOAD_PROFILE:-mixed_deposit_transfer_withdraw}}"
export LOCALNET_LOAD_PROFILE="${LOCALNET_LOAD_PROFILE:-mixed_deposit_transfer_withdraw}"
export LOCALNET_TARGET_TX_SEC="${LOCALNET_TARGET_TX_SEC:-1}"

echo "running privacy localnet TPS benchmark"
echo "  BENCH_OUT_DIR=$BENCH_OUT_DIR"
echo "  RESULT_FAMILY=$RESULT_FAMILY"
echo "  RUN_PROFILE=$RUN_PROFILE"
echo "  LOCALNET_LOAD_PROFILE=$LOCALNET_LOAD_PROFILE"
echo "  LOCALNET_TARGET_TX_SEC=$LOCALNET_TARGET_TX_SEC"

./scripts/privacy-bench-localnet.sh
