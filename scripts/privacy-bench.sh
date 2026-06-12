#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_count="${BENCH_COUNT:-10}"
bench_pattern="${BENCH_PATTERN:-Benchmark(Deposit|Spend|JoinSplit)Circuit(Prove|PublicWitness|Verify|Compile|Setup|ArtifactWrite)$}"
bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-circuits}"
bench_time="${BENCH_TIME:-}"
fee_denom="${FEE_DENOM:-}"
min_gas_price="${MIN_GAS_PRICE:-}"
gas_adjustment="${GAS_ADJUSTMENT:-}"
tx_metrics="${TX_METRICS:-}"
source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
if [[ -n "$(git status --short 2>/dev/null || true)" ]]; then
  source_dirty="true"
fi

mkdir -p "$bench_out_dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
raw_file="$bench_out_dir/raw-$stamp.txt"
benchstat_file="$bench_out_dir/benchstat-$stamp.txt"

bench_args=(
  test ./x/privacy/circuit
  -run '^$'
  -bench "$bench_pattern"
  -benchmem
  -count "$bench_count"
)
if [[ -n "$bench_time" ]]; then
  bench_args+=(-benchtime "$bench_time")
fi

echo "running privacy circuit benchmarks"
echo "  BENCH_COUNT=$bench_count"
echo "  BENCH_PATTERN=$bench_pattern"
if [[ -n "$bench_time" ]]; then
  echo "  BENCH_TIME=$bench_time"
fi

go "${bench_args[@]}" | tee "$raw_file"

if command -v benchstat >/dev/null 2>&1; then
  benchstat "$raw_file" > "$benchstat_file"
  cp "$benchstat_file" "$bench_out_dir/latest-benchstat.txt"
  echo "benchstat written to $benchstat_file"
else
  echo "benchstat not found; install golang.org/x/perf/cmd/benchstat for statistical comparison" >&2
fi

report_args=(
  run ./cmd/clairveil-benchreport
  -input "$raw_file"
  -out "$bench_out_dir"
  -commit "$source_commit"
  -dirty "$source_dirty"
)
if [[ -n "$fee_denom" ]]; then
  report_args+=(-fee-denom "$fee_denom")
fi
if [[ -n "$min_gas_price" ]]; then
  report_args+=(-min-gas-price "$min_gas_price")
fi
if [[ -n "$gas_adjustment" ]]; then
  report_args+=(-gas-adjustment "$gas_adjustment")
fi
if [[ -n "$tx_metrics" ]]; then
  report_args+=(-tx-metrics "$tx_metrics")
fi

go "${report_args[@]}"
