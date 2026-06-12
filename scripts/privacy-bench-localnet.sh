#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-localnet}"
result_family="${RESULT_FAMILY:-privacy-localnet}"
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

cleanup() {
  if [[ "$created_work_dir" == "1" && "${KEEP_BENCH_WORK_DIR:-0}" != "1" ]]; then
    rm -rf "$work_dir"
  fi
}
trap cleanup EXIT

mkdir -p "$bench_out_dir"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
metrics_file="$bench_out_dir/tx-metrics-$stamp.json"
summary_file="$bench_out_dir/localnet-summary-$stamp.json"
run_started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

echo "running privacy localnet benchmark smoke"
echo "  work_dir=$work_dir"
echo "  FEE_DENOM=$fee_denom"
echo "  MIN_GAS_PRICE=$min_gas_price"
echo "  GAS_ADJUSTMENT=$gas_adjustment"
if [[ "$created_work_dir" == "1" && "${KEEP_BENCH_WORK_DIR:-0}" == "1" ]]; then
  echo "  KEEP_BENCH_WORK_DIR=1"
fi

KEEP_WORK_DIR=1 CLAIRVEIL_E2E_WORK_DIR="$work_dir" ./scripts/privacy-e2e-smoke.sh
run_ended_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"

python3 - "$work_dir/out" "$metrics_file" <<'PY'
import json
import sys
from pathlib import Path

out = Path(sys.argv[1])
metrics_path = Path(sys.argv[2])

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
for path in sorted(out.glob("*-query.json")):
    doc = json.loads(path.read_text())
    response = doc.get("tx_response", doc)
    gas_used = int(response.get("gas_used") or 0)
    gas_wanted = int(response.get("gas_wanted") or 0)
    code = int(response.get("code") or 0)
    tx_name = path.name.removesuffix("-query.json")
    submitted_path = out / f"{tx_name}.submitted-at"
    submitted_at = submitted_path.read_text().strip() if submitted_path.exists() else ""
    transactions.append({
        "tx_type": classify(path),
        "txhash": response.get("txhash", ""),
        "source_file": str(path),
        "height": int(response.get("height") or 0),
        "gas_used": gas_used,
        "gas_wanted": gas_wanted,
        "success": code == 0,
        "submitted_at": submitted_at,
        "included_at": response.get("timestamp", ""),
    })

if not transactions:
    raise SystemExit(f"no tx query JSON files found under {out}")

metrics_path.write_text(json.dumps({
    "schema_version": "clairveil.tx_metrics.v1",
    "source": "privacy-e2e-smoke",
    "transactions": transactions,
}, indent=2) + "\n")
PY

cp "$metrics_file" "$bench_out_dir/latest-tx-metrics.json"
cp "$work_dir/out/reserve-uclair.json" "$bench_out_dir/reserve-uclair-$stamp.json"
cp "$work_dir/out/reserve-uclair.json" "$bench_out_dir/latest-reserve-uclair.json"

go run ./cmd/clairveil-localnetload \
  -tx-metrics "$metrics_file" \
  -out "$summary_file" \
  -load-profile "${LOCALNET_LOAD_PROFILE:-mixed_deposit_transfer_withdraw}" \
  -target-tx-sec "${LOCALNET_TARGET_TX_SEC:-1}" \
  -started-at "$run_started_at" \
  -ended-at "$run_ended_at"
cp "$summary_file" "$bench_out_dir/latest-localnet-summary.json"

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
  -result-family "$result_family"
  -source-files "$metrics_file,$bench_out_dir/reserve-uclair-$stamp.json"
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

echo "localnet fee report written to $bench_out_dir"
