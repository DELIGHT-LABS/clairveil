#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-user-latency}"
created_work_dir="0"
if [[ -n "${CLAIRVEIL_BENCH_WORK_DIR:-}" ]]; then
  work_dir="$CLAIRVEIL_BENCH_WORK_DIR"
else
  work_dir="$(mktemp -d)"
  created_work_dir="1"
fi
fee_denom="${FEE_DENOM:-uclair}"
min_gas_price="${MIN_GAS_PRICE:-0.025}"
gas_adjustment="${GAS_ADJUSTMENT:-1.2}"
run_profile="${RUN_PROFILE:-smoke}"
claim_types="${CLAIM_TYPES:-user_latency}"
machine_profile="${MACHINE_PROFILE:-}"
cpu_governor="${CPU_GOVERNOR:-}"
memory_gib="${MEMORY_GIB:-}"
claim_steady_state_seconds="${CLAIM_STEADY_STATE_SECONDS:-0}"
claim_load_profile="${CLAIM_LOAD_PROFILE:-wallet_flow_smoke}"
claim_latency_mode="${CLAIM_LATENCY_MODE:-native}"
claim_cold_warm="${CLAIM_COLD_WARM:-warm}"
user_latency_flow_filter="${USER_LATENCY_FLOW_FILTER:-}"
user_latency_repeat="${USER_LATENCY_REPEAT:-1}"
claim_cold_warm_separated="${CLAIM_COLD_WARM_SEPARATED:-true}"
claim_latency_p99_slo_ms="${CLAIM_LATENCY_P99_SLO_MS:-}"
claim_inclusion_p95_slo_ms="${CLAIM_INCLUSION_P95_SLO_MS:-}"
claim_chain_config="${CLAIM_CHAIN_CONFIG:-}"
claim_chain_config_file="${CLAIM_CHAIN_CONFIG_FILE:-}"
claim_chain_config_sha256="${CLAIM_CHAIN_CONFIG_SHA256:-}"
claim_browser_matrix="${CLAIM_BROWSER_MATRIX:-}"
claim_browser_adapter_ready="${CLAIM_BROWSER_ADAPTER_READY:-}"
claim_browser_adapter_version="${CLAIM_BROWSER_ADAPTER_VERSION:-}"
claim_browser_adapter_file="${CLAIM_BROWSER_ADAPTER_FILE:-}"
claim_browser_adapter_sha256="${CLAIM_BROWSER_ADAPTER_SHA256:-}"
claim_remote_topology="${CLAIM_REMOTE_TOPOLOGY:-}"
claim_instance_profile="${CLAIM_INSTANCE_PROFILE:-}"
claim_prover_config_file="${CLAIM_PROVER_CONFIG_FILE:-}"
claim_prover_config_sha256="${CLAIM_PROVER_CONFIG_SHA256:-}"
claim_linked_prover_report_file="${CLAIM_LINKED_PROVER_REPORT_FILE:-}"
claim_linked_prover_report_sha256="${CLAIM_LINKED_PROVER_REPORT_SHA256:-}"

source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
source_status="$(git status --short --untracked-files=all -- . 2>/dev/null || true)"
if [[ -n "$(printf '%s\n' "$source_status" | awk 'NF { status=substr($0,1,2); path=substr($0,4); if (status=="??" && path ~ /^benchmarks\/(privacy-circuits|privacy-proverd|privacy-localnet|privacy-proverd-load|privacy-localnet-tps|privacy-user-latency|public-capacity)\//) next; if (status=="??" && path ~ /^(clairveild|clairveil-setup|clairveil-verify|clairveil-proverd|clairveil-benchreport|clairveil-proverload|clairveil-localnetload|clairveil-userlatency)$/) next; print }')" ]]; then
  source_dirty="true"
fi

cleanup() {
  if [[ "$created_work_dir" == "1" && "${KEEP_BENCH_WORK_DIR:-0}" != "1" ]]; then
    rm -rf "$work_dir"
  fi
}
trap cleanup EXIT

if ! [[ "$user_latency_repeat" =~ ^[0-9]+$ ]] || [[ "$user_latency_repeat" -le 0 ]]; then
  echo "USER_LATENCY_REPEAT must be a positive integer" >&2
  exit 1
fi
if [[ "$run_profile" == "public_claim" && "$user_latency_repeat" -lt 100 && "${ALLOW_PUBLIC_USER_LATENCY_LOW_REPEAT:-0}" != "1" ]]; then
  echo "public_claim user latency runs require USER_LATENCY_REPEAT>=100 (or ALLOW_PUBLIC_USER_LATENCY_LOW_REPEAT=1 for a blocked dry run)" >&2
  exit 1
fi

mkdir -p "$bench_out_dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
latency_trace_file="$bench_out_dir/user-latency-trace-$stamp.jsonl"
metrics_file="$bench_out_dir/tx-metrics-$stamp.json"
summary_file="$bench_out_dir/user-latency-summary-$stamp.json"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "running privacy user latency benchmark smoke"
echo "  work_dir=$work_dir"
echo "  BENCH_OUT_DIR=$bench_out_dir"
echo "  CLAIRVEIL_PRIVACY_LATENCY_MODE=$claim_latency_mode"
echo "  CLAIRVEIL_PRIVACY_LATENCY_COLD_WARM=$claim_cold_warm"
echo "  USER_LATENCY_REPEAT=$user_latency_repeat"
if [[ -n "$user_latency_flow_filter" ]]; then
  echo "  USER_LATENCY_FLOW_FILTER=$user_latency_flow_filter"
fi

unset CLAIRVEIL_PRIVACY_LATENCY_FLOW_ID
unset CLAIRVEIL_PRIVACY_LATENCY_FLOW_PROFILE

run_work_dirs=()
for ((run_index = 1; run_index <= user_latency_repeat; run_index++)); do
  if [[ "$user_latency_repeat" -eq 1 ]]; then
    run_work_dir="$work_dir"
  else
    run_work_dir="$work_dir/run-$run_index"
  fi
  run_work_dirs+=("$run_work_dir")
  echo "  run $run_index/$user_latency_repeat work_dir=$run_work_dir"
  CLAIRVEIL_PRIVACY_LATENCY_TRACE_FILE="$latency_trace_file" \
    CLAIRVEIL_PRIVACY_LATENCY_MODE="$claim_latency_mode" \
    CLAIRVEIL_PRIVACY_LATENCY_COLD_WARM="$claim_cold_warm" \
    KEEP_WORK_DIR=1 CLAIRVEIL_E2E_WORK_DIR="$run_work_dir" ./scripts/privacy-e2e-smoke.sh
done
run_ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

out_dirs=()
for run_work_dir in "${run_work_dirs[@]}"; do
  out_dirs+=("$run_work_dir/out")
done

python3 - "$metrics_file" "${out_dirs[@]}" <<'PY'
import json
import sys
from pathlib import Path

metrics_path = Path(sys.argv[1])
out_dirs = [Path(arg) for arg in sys.argv[2:]]

def classify(path: Path) -> str:
    name = path.name.removesuffix("-query.json")
    if name == "deposit-dummy":
        return "dummy_deposit"
    if name.startswith("deposit-"):
        return "deposit"
    if name.startswith("transfer-"):
        return "transfer"
    if name == "withdraw-direct":
        return "withdraw"
    if name == "withdraw-relayed":
        return "relay_withdraw"
    return name

transactions = []
for run_index, out in enumerate(out_dirs, start=1):
    for path in sorted(out.glob("*-query.json")):
        doc = json.loads(path.read_text())
        response = doc.get("tx_response", doc)
        tx_name = path.name.removesuffix("-query.json")
        submitted_path = out / f"{tx_name}.submitted-at"
        submitted_at = submitted_path.read_text().strip() if submitted_path.exists() else ""
        transactions.append({
            "tx_type": classify(path),
            "txhash": response.get("txhash", ""),
            "source_file": str(path),
            "run_index": run_index,
            "height": int(response.get("height") or 0),
            "gas_used": int(response.get("gas_used") or 0),
            "gas_wanted": int(response.get("gas_wanted") or 0),
            "success": int(response.get("code") or 0) == 0,
            "submitted_at": submitted_at,
            "included_at": response.get("timestamp", ""),
        })

if not transactions:
    raise SystemExit(f"no tx query JSON files found under {', '.join(str(p) for p in out_dirs)}")

metrics_path.write_text(json.dumps({
    "schema_version": "clairveil.tx_metrics.v1",
    "source": "privacy-e2e-smoke-repeat",
    "transactions": transactions,
}, indent=2) + "\n")
PY

summary_args=(
  run ./cmd/clairveil-userlatency
  -trace "$latency_trace_file"
  -tx-metrics "$metrics_file"
  -out "$summary_file"
)
if [[ -n "$user_latency_flow_filter" ]]; then
  summary_args+=(-flow-profile "$user_latency_flow_filter")
fi
if [[ -n "$claim_latency_mode" ]]; then
  summary_args+=(-latency-mode "$claim_latency_mode")
fi
if [[ -n "$claim_cold_warm" ]]; then
  summary_args+=(-cold-warm "$claim_cold_warm")
fi

go "${summary_args[@]}"

cp "$latency_trace_file" "$bench_out_dir/latest-user-latency-trace.jsonl"
cp "$metrics_file" "$bench_out_dir/latest-tx-metrics.json"
cp "$summary_file" "$bench_out_dir/latest-user-latency-summary.json"

report_args=(
  run ./cmd/clairveil-benchreport
  -benchmark-summaries "$summary_file"
  -tx-metrics "$metrics_file"
  -out "$bench_out_dir"
  -fee-denom "$fee_denom"
  -min-gas-price "$min_gas_price"
  -gas-adjustment "$gas_adjustment"
  -commit "$source_commit"
  -dirty "$source_dirty"
  -result-family "privacy-user-latency"
  -source-files "$latency_trace_file"
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
if [[ -n "$claim_latency_mode" ]]; then
  report_args+=(-claim-latency-mode "$claim_latency_mode")
fi
if [[ -n "$claim_cold_warm_separated" ]]; then
  report_args+=(-claim-cold-warm-separated "$claim_cold_warm_separated")
fi
if [[ -n "$claim_latency_p99_slo_ms" ]]; then
  report_args+=(-claim-latency-p99-slo-ms "$claim_latency_p99_slo_ms")
fi
if [[ -n "$claim_inclusion_p95_slo_ms" ]]; then
  report_args+=(-claim-inclusion-p95-slo-ms "$claim_inclusion_p95_slo_ms")
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
if [[ -n "$claim_instance_profile" ]]; then
  report_args+=(-claim-instance-profile "$claim_instance_profile")
fi
if [[ -n "$claim_prover_config_file" ]]; then
  report_args+=(-claim-prover-config-file "$claim_prover_config_file")
fi
if [[ -n "$claim_prover_config_sha256" ]]; then
  report_args+=(-claim-prover-config-sha256 "$claim_prover_config_sha256")
fi
if [[ -n "$claim_linked_prover_report_file" ]]; then
  report_args+=(-claim-linked-prover-report-file "$claim_linked_prover_report_file")
fi
if [[ -n "$claim_linked_prover_report_sha256" ]]; then
  report_args+=(-claim-linked-prover-report-sha256 "$claim_linked_prover_report_sha256")
fi

go "${report_args[@]}"

echo "user latency report written to $bench_out_dir"
