#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_count="${BENCH_COUNT:-10}"
bench_pattern="${BENCH_PATTERN:-BenchmarkHTTPProverClient(Transfer|Withdraw)(Parallel)?RoundTrip$}"
bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-proverd}"
bench_time="${BENCH_TIME:-}"
run_profile="${RUN_PROFILE:-smoke}"
claim_types="${CLAIM_TYPES:-}"
machine_profile="${MACHINE_PROFILE:-}"
cpu_governor="${CPU_GOVERNOR:-}"
memory_gib="${MEMORY_GIB:-}"
claim_steady_state_seconds="${CLAIM_STEADY_STATE_SECONDS:-0}"
claim_load_profile="${CLAIM_LOAD_PROFILE:-}"
claim_preflight_mode="${CLAIM_PREFLIGHT_MODE:-}"
claim_auth_enabled="${CLAIM_AUTH_ENABLED:-}"
claim_instance_profile="${CLAIM_INSTANCE_PROFILE:-}"
claim_prover_config_file="${CLAIM_PROVER_CONFIG_FILE:-}"
claim_prover_config_sha256="${CLAIM_PROVER_CONFIG_SHA256:-}"
claim_chain_config="${CLAIM_CHAIN_CONFIG:-}"
claim_chain_config_file="${CLAIM_CHAIN_CONFIG_FILE:-}"
claim_chain_config_sha256="${CLAIM_CHAIN_CONFIG_SHA256:-}"
claim_reserve_invariant="${CLAIM_RESERVE_INVARIANT:-}"
claim_latency_p99_slo_ms="${CLAIM_LATENCY_P99_SLO_MS:-}"
claim_inclusion_p95_slo_ms="${CLAIM_INCLUSION_P95_SLO_MS:-}"
claim_rss_stable="${CLAIM_RSS_STABLE:-}"
claim_saturation_profile="${CLAIM_SATURATION_PROFILE:-}"
claim_saturation_profile_file="${CLAIM_SATURATION_PROFILE_FILE:-}"
claim_saturation_profile_sha256="${CLAIM_SATURATION_PROFILE_SHA256:-}"
claim_throughput_window_seconds="${CLAIM_THROUGHPUT_WINDOW_SECONDS:-}"
claim_reserve_snapshot_before_file="${CLAIM_RESERVE_SNAPSHOT_BEFORE_FILE:-}"
claim_reserve_snapshot_before_sha256="${CLAIM_RESERVE_SNAPSHOT_BEFORE_SHA256:-}"
claim_reserve_snapshot_after_file="${CLAIM_RESERVE_SNAPSHOT_AFTER_FILE:-}"
claim_reserve_snapshot_after_sha256="${CLAIM_RESERVE_SNAPSHOT_AFTER_SHA256:-}"
claim_latency_mode="${CLAIM_LATENCY_MODE:-}"
claim_cold_warm_separated="${CLAIM_COLD_WARM_SEPARATED:-}"
claim_browser_matrix="${CLAIM_BROWSER_MATRIX:-}"
claim_browser_adapter_ready="${CLAIM_BROWSER_ADAPTER_READY:-}"
claim_browser_adapter_version="${CLAIM_BROWSER_ADAPTER_VERSION:-}"
claim_browser_adapter_file="${CLAIM_BROWSER_ADAPTER_FILE:-}"
claim_browser_adapter_sha256="${CLAIM_BROWSER_ADAPTER_SHA256:-}"
claim_remote_topology="${CLAIM_REMOTE_TOPOLOGY:-}"
claim_linked_prover_report_file="${CLAIM_LINKED_PROVER_REPORT_FILE:-}"
claim_linked_prover_report_sha256="${CLAIM_LINKED_PROVER_REPORT_SHA256:-}"
source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
source_status="$(git status --short -- . 2>/dev/null || true)"
if [[ -n "$(printf '%s\n' "$source_status" | awk 'NF { status=substr($0,1,2); path=substr($0,4); if (status=="??" && path ~ /^benchmarks\/(privacy-circuits|privacy-proverd|privacy-localnet|privacy-proverd-load|privacy-localnet-tps|privacy-user-latency|public-capacity)\//) next; print }')" ]]; then
  source_dirty="true"
fi

mkdir -p "$bench_out_dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
raw_file="$bench_out_dir/raw-$stamp.txt"
benchstat_file="$bench_out_dir/benchstat-$stamp.txt"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

bench_args=(
  test ./x/privacy/client/sdk/provertransport
  -run '^$'
  -bench "$bench_pattern"
  -benchmem
  -count "$bench_count"
)
if [[ -n "$bench_time" ]]; then
  bench_args+=(-benchtime "$bench_time")
fi

echo "running privacy prover HTTP transport benchmarks"
echo "  BENCH_COUNT=$bench_count"
echo "  BENCH_PATTERN=$bench_pattern"
if [[ -n "$bench_time" ]]; then
  echo "  BENCH_TIME=$bench_time"
fi

go "${bench_args[@]}" | tee "$raw_file"
run_ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

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
  -result-family "privacy-proverd"
  -source-files "$raw_file"
  -run-started-at "$run_started_at"
  -run-ended-at "$run_ended_at"
  -run-profile "$run_profile"
  -claim-steady-state-seconds "$claim_steady_state_seconds"
)
if [[ -n "$claim_types" ]]; then
  report_args+=(-claim-types "$claim_types")
fi
if [[ -n "$machine_profile" ]]; then
  report_args+=(-machine-profile "$machine_profile")
fi
if [[ -n "$cpu_governor" ]]; then
  report_args+=(-cpu-governor "$cpu_governor")
fi
if [[ -n "$memory_gib" ]]; then
  report_args+=(-memory-gib "$memory_gib")
fi
if [[ -n "$claim_load_profile" ]]; then
  report_args+=(-claim-load-profile "$claim_load_profile")
fi
if [[ -n "$claim_preflight_mode" ]]; then
  report_args+=(-claim-preflight-mode "$claim_preflight_mode")
fi
if [[ -n "$claim_auth_enabled" ]]; then
  report_args+=(-claim-auth-enabled "$claim_auth_enabled")
fi
if [[ -n "$claim_instance_profile" ]]; then
  report_args+=(-claim-instance-profile "$claim_instance_profile")
fi
if [[ -n "$claim_prover_config_file" ]]; then
  report_args+=(-claim-prover-config-file "$claim_prover_config_file")
fi
if [[ -n "$claim_prover_config_sha256" ]]; then
  report_args+=(-claim-prover-config-sha256 "$claim_prover_config_sha256")
fi
if [[ -n "$claim_chain_config" ]]; then
  report_args+=(-claim-chain-config "$claim_chain_config")
fi
if [[ -n "$claim_chain_config_file" ]]; then
  report_args+=(-claim-chain-config-file "$claim_chain_config_file")
fi
if [[ -n "$claim_chain_config_sha256" ]]; then
  report_args+=(-claim-chain-config-sha256 "$claim_chain_config_sha256")
fi
if [[ -n "$claim_reserve_invariant" ]]; then
  report_args+=(-claim-reserve-invariant "$claim_reserve_invariant")
fi
if [[ -n "$claim_latency_p99_slo_ms" ]]; then
  report_args+=(-claim-latency-p99-slo-ms "$claim_latency_p99_slo_ms")
fi
if [[ -n "$claim_inclusion_p95_slo_ms" ]]; then
  report_args+=(-claim-inclusion-p95-slo-ms "$claim_inclusion_p95_slo_ms")
fi
if [[ -n "$claim_rss_stable" ]]; then
  report_args+=(-claim-rss-stable "$claim_rss_stable")
fi
if [[ -n "$claim_saturation_profile" ]]; then
  report_args+=(-claim-saturation-profile "$claim_saturation_profile")
fi
if [[ -n "$claim_saturation_profile_file" ]]; then
  report_args+=(-claim-saturation-profile-file "$claim_saturation_profile_file")
fi
if [[ -n "$claim_saturation_profile_sha256" ]]; then
  report_args+=(-claim-saturation-profile-sha256 "$claim_saturation_profile_sha256")
fi
if [[ -n "$claim_throughput_window_seconds" ]]; then
  report_args+=(-claim-throughput-window-seconds "$claim_throughput_window_seconds")
fi
if [[ -n "$claim_reserve_snapshot_before_file" ]]; then
  report_args+=(-claim-reserve-snapshot-before-file "$claim_reserve_snapshot_before_file")
fi
if [[ -n "$claim_reserve_snapshot_before_sha256" ]]; then
  report_args+=(-claim-reserve-snapshot-before-sha256 "$claim_reserve_snapshot_before_sha256")
fi
if [[ -n "$claim_reserve_snapshot_after_file" ]]; then
  report_args+=(-claim-reserve-snapshot-after-file "$claim_reserve_snapshot_after_file")
fi
if [[ -n "$claim_reserve_snapshot_after_sha256" ]]; then
  report_args+=(-claim-reserve-snapshot-after-sha256 "$claim_reserve_snapshot_after_sha256")
fi
if [[ -n "$claim_latency_mode" ]]; then
  report_args+=(-claim-latency-mode "$claim_latency_mode")
fi
if [[ -n "$claim_cold_warm_separated" ]]; then
  report_args+=(-claim-cold-warm-separated "$claim_cold_warm_separated")
fi
if [[ -n "$claim_browser_matrix" ]]; then
  report_args+=(-claim-browser-matrix "$claim_browser_matrix")
fi
if [[ -n "$claim_browser_adapter_ready" ]]; then
  report_args+=(-claim-browser-adapter-ready "$claim_browser_adapter_ready")
fi
if [[ -n "$claim_browser_adapter_version" ]]; then
  report_args+=(-claim-browser-adapter-version "$claim_browser_adapter_version")
fi
if [[ -n "$claim_browser_adapter_file" ]]; then
  report_args+=(-claim-browser-adapter-file "$claim_browser_adapter_file")
fi
if [[ -n "$claim_browser_adapter_sha256" ]]; then
  report_args+=(-claim-browser-adapter-sha256 "$claim_browser_adapter_sha256")
fi
if [[ -n "$claim_remote_topology" ]]; then
  report_args+=(-claim-remote-topology "$claim_remote_topology")
fi
if [[ -n "$claim_linked_prover_report_file" ]]; then
  report_args+=(-claim-linked-prover-report-file "$claim_linked_prover_report_file")
fi
if [[ -n "$claim_linked_prover_report_sha256" ]]; then
  report_args+=(-claim-linked-prover-report-sha256 "$claim_linked_prover_report_sha256")
fi

go "${report_args[@]}"
