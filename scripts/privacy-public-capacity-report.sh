#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/public-capacity}"
run_profile="${RUN_PROFILE:-public_claim}"
reports="${REPORTS:-}"
machine_profile="${MACHINE_PROFILE:-}"
cpu_governor="${CPU_GOVERNOR:-}"
memory_gib="${MEMORY_GIB:-}"

source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
source_status="$(git status --short -- . 2>/dev/null || true)"
if [[ -n "$(printf '%s\n' "$source_status" | awk 'NF { status=substr($0,1,2); path=substr($0,4); if (status=="??" && path ~ /^benchmarks\/(privacy-circuits|privacy-proverd|privacy-localnet|privacy-proverd-load|privacy-localnet-tps|privacy-user-latency|public-capacity)\//) next; print }')" ]]; then
  source_dirty="true"
fi

if [[ -z "$reports" ]]; then
  candidates=(
    "benchmarks/privacy-proverd-load/latest.json"
    "benchmarks/privacy-localnet-tps/latest.json"
    "benchmarks/privacy-user-latency/latest.json"
  )
  existing=()
  for candidate in "${candidates[@]}"; do
    if [[ -f "$candidate" ]]; then
      existing+=("$candidate")
    fi
  done
  if [[ ${#existing[@]} -eq 0 ]]; then
    echo "no default component reports found; set REPORTS=path1.json,path2.json" >&2
    exit 1
  fi
  reports="$(IFS=,; echo "${existing[*]}")"
fi

report_args=(
  run ./cmd/clairveil-benchreport
  -merge-reports "$reports"
  -out "$bench_out_dir"
  -commit "$source_commit"
  -dirty "$source_dirty"
  -run-profile "$run_profile"
)
if [[ -n "$machine_profile" ]]; then
  report_args+=(-machine-profile "$machine_profile")
fi
if [[ -n "$cpu_governor" ]]; then
  report_args+=(-cpu-governor "$cpu_governor")
fi
if [[ -n "$memory_gib" ]]; then
  report_args+=(-memory-gib "$memory_gib")
fi

echo "building public capacity aggregate report"
echo "  REPORTS=$reports"
go "${report_args[@]}"
