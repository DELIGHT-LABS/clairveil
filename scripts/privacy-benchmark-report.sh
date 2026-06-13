#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

out_file="${HUMAN_BENCHMARK_REPORT_OUT:-benchmarks/clairveil-benchmark-results-report-kr.md}"
reports="${SUMMARY_REPORTS:-}"

if [[ -z "$reports" ]]; then
  candidates=(
    "benchmarks/privacy-circuits/latest.json"
    "benchmarks/privacy-proverd/latest.json"
    "benchmarks/privacy-proverd-load/latest.json"
    "benchmarks/privacy-localnet/latest.json"
    "benchmarks/privacy-localnet-tps/latest.json"
    "benchmarks/privacy-user-latency/latest.json"
    "benchmarks/public-capacity/latest.json"
  )
  existing=()
  for candidate in "${candidates[@]}"; do
    if [[ -f "$candidate" ]]; then
      existing+=("$candidate")
    fi
  done
  if [[ ${#existing[@]} -eq 0 ]]; then
    echo "no benchmark reports found; run benchmark targets first or set SUMMARY_REPORTS=path1.json,path2.json" >&2
    exit 1
  fi
  reports="$(IFS=,; echo "${existing[*]}")"
fi

echo "building human benchmark summary report"
echo "  SUMMARY_REPORTS=$reports"
echo "  HUMAN_BENCHMARK_REPORT_OUT=$out_file"
go run ./cmd/clairveil-benchreport \
  -human-summary-reports "$reports" \
  -human-summary-out "$out_file"
