#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

bench_out_dir="${BENCH_OUT_DIR:-benchmarks/privacy-localnet}"
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
source_commit="$(git rev-parse HEAD 2>/dev/null || true)"
source_dirty="false"
if [[ -n "$(git status --short -- . ':(exclude)benchmarks' 2>/dev/null || true)" ]]; then
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

echo "running privacy localnet benchmark smoke"
echo "  work_dir=$work_dir"
echo "  FEE_DENOM=$fee_denom"
echo "  MIN_GAS_PRICE=$min_gas_price"
echo "  GAS_ADJUSTMENT=$gas_adjustment"
if [[ "$created_work_dir" == "1" && "${KEEP_BENCH_WORK_DIR:-0}" == "1" ]]; then
  echo "  KEEP_BENCH_WORK_DIR=1"
fi

KEEP_WORK_DIR=1 CLAIRVEIL_E2E_WORK_DIR="$work_dir" ./scripts/privacy-e2e-smoke.sh

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
    transactions.append({
        "tx_type": classify(path),
        "txhash": response.get("txhash", ""),
        "source_file": str(path),
        "gas_used": gas_used,
        "gas_wanted": gas_wanted,
        "success": code == 0,
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

go run ./cmd/clairveil-benchreport \
  -tx-metrics "$metrics_file" \
  -out "$bench_out_dir" \
  -fee-denom "$fee_denom" \
  -min-gas-price "$min_gas_price" \
  -gas-adjustment "$gas_adjustment" \
  -commit "$source_commit" \
  -dirty "$source_dirty"

echo "localnet fee report written to $bench_out_dir"
