#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${DIST_DIR:-"$repo_root/dist"}"
commit="$(git -C "$repo_root" rev-parse --short=12 HEAD)"
version="${RELEASE_VERSION:-"$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || printf '%s' "$commit")"}"
pack_name="clairveil-handoff-${version}"
work_dir="$(mktemp -d)"
pack_root="$work_dir/$pack_name"
archive_path="$dist_dir/${pack_name}.tar.gz"
checksum_path="$archive_path.sha256"

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

require_clean_worktree() {
	if [[ -n "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]]; then
		echo "release pack generation requires a clean worktree" >&2
		exit 1
	fi
}

copy_path() {
	local path="$1"
	if [[ ! -e "$repo_root/$path" ]]; then
		echo "missing release pack path: $path" >&2
		exit 1
	fi
	mkdir -p "$pack_root/$(dirname "$path")"
	cp -R "$repo_root/$path" "$pack_root/$path"
}

require_clean_worktree
mkdir -p "$pack_root" "$dist_dir"

copy_path "README.md"
copy_path "README-kr.md"
copy_path "LICENSE"
copy_path "NOTICE"
copy_path "CHANGELOG.md"
copy_path "CHANGELOG-kr.md"
copy_path "CONTRIBUTING.md"
copy_path "CONTRIBUTING-kr.md"
copy_path "SECURITY.md"
copy_path "SECURITY-kr.md"
copy_path "CODE_OF_CONDUCT.md"
copy_path "CODE_OF_CONDUCT-kr.md"
copy_path "Makefile"
copy_path "go.mod"
copy_path "go.sum"
copy_path "proto"
copy_path "docs/clairveil-release-handoff-pack.md"
copy_path "docs/clairveil-release-handoff-pack-kr.md"
copy_path "docs/clairveil-circuits.md"
copy_path "docs/clairveil-circuits-kr.md"
copy_path "docs/clairveil-batch-joinsplit-16x32.md"
copy_path "docs/clairveil-batch-joinsplit-16x32-kr.md"
copy_path "docs/clairveil-batch-joinsplit-16x32-session-4-validation-report.md"
copy_path "docs/clairveil-batch-joinsplit-16x32-session-4-validation-report-kr.md"
copy_path "docs/clairveil-batch-joinsplit-localnet-tutorial.md"
copy_path "docs/clairveil-batch-joinsplit-localnet-tutorial-kr.md"
copy_path "docs/clairveil-session3b-batch-transfer-handoff.md"
copy_path "docs/clairveil-session3b-batch-transfer-handoff-kr.md"
copy_path "docs/clairveil-cli-reference.md"
copy_path "docs/clairveil-cli-reference-kr.md"
copy_path "docs/clairveil-testing-guide.md"
copy_path "docs/clairveil-testing-guide-kr.md"
copy_path "docs/clairveil-operations-guide.md"
copy_path "docs/clairveil-operations-guide-kr.md"
copy_path "docs/clairveil-privacy-accounting-design-note.md"
copy_path "docs/clairveil-privacy-accounting-design-note-kr.md"
copy_path "docs/clairveil-maintainer-instructions.md"
copy_path "docs/clairveil-maintainer-instructions-kr.md"
copy_path "docs/clairveil-downstream-cosmos-integration-guide.md"
copy_path "docs/clairveil-downstream-cosmos-integration-guide-kr.md"
copy_path "docs/clairveil-client-product-brief.md"
copy_path "docs/clairveil-client-product-brief-kr.md"
copy_path "docs/clairveil-client-ux-flows.md"
copy_path "docs/clairveil-client-ux-flows-kr.md"
copy_path "docs/clairveil-client-risk-decisions.md"
copy_path "docs/clairveil-client-risk-decisions-kr.md"
copy_path "docs/clairveil-client-api-checklist.md"
copy_path "docs/clairveil-client-api-checklist-kr.md"
copy_path "docs/clairveil-js-sdk-handoff.md"
copy_path "docs/clairveil-js-sdk-handoff-kr.md"
copy_path "docs/clairveil-bulk-transfer-product-handoff-kr.md"
copy_path "plans/clairveil-bulk-transfer-strategy-kr.md"
copy_path "plans/clairveil-bulk-transfer-time-simulation-kr.md"
copy_path "docs/clairveil-note-reservation-design-kr.md"
copy_path "docs/clairveil-note-reservation-design.md"
copy_path "docs/clairveil-reference-payroll-product.md"
copy_path "docs/clairveil-reference-payroll-product-kr.md"
copy_path "docs/clairveil-reference-payroll-product-policy.md"
copy_path "docs/clairveil-reference-payroll-product-policy-kr.md"
copy_path "docs/clairveil-reference-payroll-js-sdk-handoff.md"
copy_path "docs/clairveil-reference-payroll-js-sdk-handoff-kr.md"
copy_path "docs/clairveil-reference-payroll-wallet-handoff.md"
copy_path "docs/clairveil-reference-payroll-wallet-handoff-kr.md"
copy_path "docs/clairveil-reference-payroll-live-localnet-tutorial.md"
copy_path "docs/clairveil-reference-payroll-live-localnet-tutorial-kr.md"
copy_path "docs/clairveil-reference-payroll-rehearsal-kr.md"
copy_path "docs/clairveil-reference-payroll-localnet-rehearsal-result-kr.md"
copy_path "plans/clairveil-scan-optimization-implementation-plan.md"
copy_path "plans/clairveil-scan-optimization-implementation-plan-kr.md"
copy_path "docs/clairveil-proverd-remote-production-profile.md"
copy_path "docs/clairveil-proverd-remote-production-profile-kr.md"
copy_path "docs/clairveil-merkle-restore-sop.md"
copy_path "docs/clairveil-merkle-restore-sop-kr.md"
copy_path "docs/clairveil-release-versioning-policy.md"
copy_path "docs/clairveil-release-versioning-policy-kr.md"
copy_path "docs/clairveil-release-note-template.md"
copy_path "docs/clairveil-release-note-template-kr.md"
copy_path "docs/clairveil-threat-model.md"
copy_path "docs/clairveil-threat-model-kr.md"
copy_path "docs/clairveil-security-best-practices-review.md"
copy_path "docs/clairveil-security-best-practices-review-kr.md"
copy_path "docs/clairveil-local-privacy-walkthrough.md"
copy_path "docs/clairveil-local-privacy-walkthrough-kr.md"
copy_path "plans/clairveild-reference-app-plan.md"
copy_path "plans/clairveild-reference-app-plan-kr.md"
copy_path "docs/schemas"
copy_path "plans/clairveil-bulk-transfer-implementation-plan-kr.md"
copy_path "plans/clairveil-public-benchmark-plan-kr.md"
copy_path "plans/clairveil-public-capacity-benchmark-followup-plan-kr.md"
copy_path "plans/clairveil-public-capacity-claim-execution-plan-kr.md"
copy_path "x/privacy/client/sdk/conformance/testdata"
copy_path "examples/README.md"
copy_path "examples/README-kr.md"
copy_path "examples/audit-disclosure-keys"
copy_path "examples/js-sdk-fixture-validator"
copy_path "examples/js-sdk-prover-http-client"
copy_path "examples/reference-payroll"
copy_path "build/clairveil-proverd"
copy_path "scripts/release-pack.sh"
copy_path "scripts/release-pack-verify.sh"
copy_path "scripts/privacy-batch-joinsplit-localnet.sh"

# Recheck after copying so a concurrent working-tree mutation cannot be
# attributed to the immutable HEAD recorded in the manifest.
require_clean_worktree

cat >"$pack_root/RELEASE-MANIFEST.txt" <<EOF
Clairveil release handoff pack
==============================

version: $version
commit: $(git -C "$repo_root" rev-parse HEAD)
generated_at_utc: $(date -u +"%Y-%m-%dT%H:%M:%SZ")
source_repo: github.com/DELIGHT-LABS/clairveil

Contents:
- root license, notice, changelog, contribution, and security files
- English and Korean public documentation pairs where available
- proto/clairveil/privacy/v1
- client, JS/web wallet, and scan optimization handoff documents
- bulk transfer design, plan, product handoff, simulation, reference payroll, and readiness materials
- JS/web wallet JSON schemas
- wallet/prover conformance fixtures
- JS audit disclosure key, fixture validator, prover HTTP client, and reference payroll examples
- prover Dockerfile and compose sample
- release pack generation and verification scripts
- release, circuit, CLI, testing, operations, downstream integration, client, JS SDK, scan optimization, prover, Merkle restore, threat model, security, and reference app documents
- NoteV1 and BatchJoinSplit16x32 foundation contracts and independent golden fixtures
- Session 4 independent validation, benchmark, localnet, finding, and residual-risk reports
- Korean bulk transfer planning documents and source-repo readiness commands
- release versioning policy and release note templates

Validation before handoff:
- make release-check
- make privacy-bulk-readiness-check from the source checkout when doing extra bulk-readiness rehearsal
- RUN_PROVER_SCALE=1 PROVERD_URLS=url1,url2 make privacy-bulk-readiness-check before making prover-pool scale claims
- make release-pack
- make release-pack-verify

Notes:
- This pack is a handoff contract bundle, not a full source distribution.
- Bulk-transfer planning documents are currently Korean working records; the
  language-neutral implementation contract is carried by schemas, conformance
  fixtures, CLI/API references, and readiness commands.
- Downstream production apps must still own EVM/policy/precompile integration,
  audit private key custody, wallet storage encryption, artifact signing, and
  remote prover deployment policy.
EOF

(
	cd "$pack_root"
	find . -type f ! -name SHA256SUMS.txt | LC_ALL=C sort | while IFS= read -r file; do
		shasum -a 256 "$file"
	done >SHA256SUMS.txt
)

tar -C "$work_dir" -czf "$archive_path" "$pack_name"
shasum -a 256 "$archive_path" >"$checksum_path"

echo "release pack: $archive_path"
echo "checksum: $checksum_path"
