#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

proverd_url="${PROVERD_URL:-}"
if [[ -z "$proverd_url" ]]; then
  echo "PROVERD_URL is required, for example PROVERD_URL=http://127.0.0.1:9090" >&2
  exit 1
fi

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-proverd-load}"
profile="${PROVERLOAD_PROFILE:-transfer_only}"
concurrency="${PROVERLOAD_CONCURRENCY:-1,2}"
duration="${PROVERLOAD_DURATION:-30s}"
warmup="${PROVERLOAD_WARMUP:-5s}"
timeout="${PROVERLOAD_TIMEOUT:-2m}"
fixture_bundle="${PROVERLOAD_FIXTURE_BUNDLE:-x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json}"
transfer_request="${PROVERLOAD_TRANSFER_REQUEST:-}"
withdraw_request="${PROVERLOAD_WITHDRAW_REQUEST:-}"
bearer_token="${PROVERD_BEARER_TOKEN:-${CLAIRVEIL_PROVERD_BEARER_TOKEN:-}}"
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
claim_latency_p99_slo_ms="${CLAIM_LATENCY_P99_SLO_MS:-}"
claim_rss_stable="${CLAIM_RSS_STABLE:-}"
claim_saturation_profile="${CLAIM_SATURATION_PROFILE:-}"
claim_saturation_profile_file="${CLAIM_SATURATION_PROFILE_FILE:-}"
claim_saturation_profile_sha256="${CLAIM_SATURATION_PROFILE_SHA256:-}"

source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
source_status="$(git status --short -- . 2>/dev/null || true)"
if [[ -n "$(printf '%s\n' "$source_status" | awk 'NF { status=substr($0,1,2); path=substr($0,4); if (status=="??" && path ~ /^benchmarks\/(privacy-circuits|privacy-proverd|privacy-localnet|privacy-proverd-load|privacy-localnet-tps|privacy-user-latency|public-capacity)\//) next; print }')" ]]; then
  source_dirty="true"
fi

mkdir -p "$bench_out_dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
summary_file="$bench_out_dir/load-summary-$stamp.json"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

load_args=(
  run ./cmd/clairveil-proverload
  -base-url "$proverd_url"
  -fixture-bundle "$fixture_bundle"
  -profile "$profile"
  -concurrency "$concurrency"
  -duration "$duration"
  -warmup "$warmup"
  -timeout "$timeout"
  -out "$summary_file"
)
if [[ -n "$bearer_token" ]]; then
  load_args+=(-bearer-token "$bearer_token")
fi
if [[ -n "$transfer_request" ]]; then
  load_args+=(-transfer-request "$transfer_request")
fi
if [[ -n "$withdraw_request" ]]; then
  load_args+=(-withdraw-request "$withdraw_request")
fi

echo "running external clairveil-proverd load benchmark"
echo "  PROVERD_URL=$proverd_url"
echo "  PROVERLOAD_PROFILE=$profile"
echo "  PROVERLOAD_CONCURRENCY=$concurrency"
echo "  PROVERLOAD_DURATION=$duration"
go "${load_args[@]}"
run_ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
cp "$summary_file" "$bench_out_dir/latest-load-summary.json"

report_args=(
  run ./cmd/clairveil-benchreport
  -benchmark-summaries "$summary_file"
  -out "$bench_out_dir"
  -commit "$source_commit"
  -dirty "$source_dirty"
  -result-family "privacy-proverd-load"
  -source-files "$summary_file"
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
if [[ -n "$claim_latency_p99_slo_ms" ]]; then
  report_args+=(-claim-latency-p99-slo-ms "$claim_latency_p99_slo_ms")
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

go "${report_args[@]}"
