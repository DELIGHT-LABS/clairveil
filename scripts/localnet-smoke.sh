#!/usr/bin/env bash
set -euo pipefail

# The former fresh-genesis daemon check cannot validate the identity-pinned V2
# runtime. Keep this entrypoint, but execute the reviewed V2 localnet runner.
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/privacy-audit-v2-smoke.sh" "$@"
