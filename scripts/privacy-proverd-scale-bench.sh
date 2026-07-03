#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

proverd_urls="${PROVERD_URLS:-}"
if [[ -z "$proverd_urls" ]]; then
  echo "PROVERD_URLS is required, for example PROVERD_URLS=http://127.0.0.1:9090,http://127.0.0.1:9091" >&2
  exit 1
fi

endpoint_count="$(
  python3 - "$proverd_urls" <<'PY'
import sys
urls = [part.strip() for part in sys.argv[1].split(",") if part.strip()]
if not urls:
    raise SystemExit("PROVERD_URLS must contain at least one URL")
print(len(dict.fromkeys(urls)))
PY
)"

export BENCH_OUT_DIR="${BENCH_OUT_DIR:-benchmarks/privacy-proverd-scale}"
export RESULT_FAMILY="${RESULT_FAMILY:-privacy-proverd-scale}"
export CLAIM_LOAD_PROFILE="${CLAIM_LOAD_PROFILE:-prover_pool_${endpoint_count}x_${PROVERLOAD_PROFILE:-transfer_only}}"

./scripts/privacy-proverd-load-bench.sh
