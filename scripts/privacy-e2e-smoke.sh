#!/usr/bin/env bash
set -euo pipefail

# The V1 deposit/transfer/withdraw sequence was not valid V2 evidence. This
# stable entrypoint now delegates to the identity-pinned audit-field runner.
exec "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/privacy-audit-v2-smoke.sh" "$@"
