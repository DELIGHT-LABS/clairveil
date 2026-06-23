#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${DIST_DIR:-"$repo_root/dist"}"
commit="$(git -C "$repo_root" rev-parse --short=12 HEAD)"
version="${RELEASE_VERSION:-"$(git -C "$repo_root" describe --tags --always --dirty 2>/dev/null || printf '%s' "$commit")"}"
pack_name="clairveil-handoff-${version}"
archive_path="${RELEASE_PACK_ARCHIVE:-"$dist_dir/${pack_name}.tar.gz"}"
checksum_path="${RELEASE_PACK_CHECKSUM:-"$archive_path.sha256"}"
work_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

fail() {
	echo "release pack verify failed: $*" >&2
	exit 1
}

if [[ -z "${RELEASE_PACK_ARCHIVE:-}" ]]; then
	"$repo_root/scripts/release-pack.sh" >/dev/null
elif [[ ! -f "$archive_path" || ! -f "$checksum_path" ]]; then
	"$repo_root/scripts/release-pack.sh" >/dev/null
fi

[[ -f "$archive_path" ]] || fail "missing archive: $archive_path"
[[ -f "$checksum_path" ]] || fail "missing checksum: $checksum_path"

expected_checksum="$(awk '{print $1; exit}' "$checksum_path")"
actual_checksum="$(shasum -a 256 "$archive_path" | awk '{print $1; exit}')"
[[ -n "$expected_checksum" ]] || fail "empty checksum file: $checksum_path"
[[ "$expected_checksum" == "$actual_checksum" ]] || fail "archive checksum mismatch"

top_level_count="$(tar -tzf "$archive_path" | awk -F/ 'NF {print $1}' | LC_ALL=C sort -u | wc -l | tr -d ' ')"
[[ "$top_level_count" == "1" ]] || fail "archive must contain exactly one top-level directory"

pack_root_name="$(tar -tzf "$archive_path" | awk -F/ 'NF {print $1; exit}')"
tar -xzf "$archive_path" -C "$work_dir"
pack_root="$work_dir/$pack_root_name"
[[ -d "$pack_root" ]] || fail "missing extracted pack root: $pack_root_name"

required_files=(
	"RELEASE-MANIFEST.txt"
	"SHA256SUMS.txt"
	"README.md"
	"README-kr.md"
	"LICENSE"
	"NOTICE"
	"CHANGELOG.md"
	"CHANGELOG-kr.md"
	"CONTRIBUTING.md"
	"CONTRIBUTING-kr.md"
	"SECURITY.md"
	"SECURITY-kr.md"
	"CODE_OF_CONDUCT.md"
	"CODE_OF_CONDUCT-kr.md"
	"Makefile"
	"go.mod"
	"go.sum"
	"proto/clairveil/privacy/v1/genesis.proto"
	"proto/clairveil/privacy/v1/query.proto"
	"proto/clairveil/privacy/v1/tx.proto"
	"docs/clairveil-release-handoff-pack.md"
	"docs/clairveil-release-handoff-pack-kr.md"
	"docs/clairveil-circuits.md"
	"docs/clairveil-circuits-kr.md"
	"docs/clairveil-cli-reference.md"
	"docs/clairveil-cli-reference-kr.md"
	"docs/clairveil-testing-guide.md"
	"docs/clairveil-testing-guide-kr.md"
	"docs/clairveil-operations-guide.md"
	"docs/clairveil-operations-guide-kr.md"
	"docs/clairveil-maintainer-instructions.md"
	"docs/clairveil-maintainer-instructions-kr.md"
	"docs/clairveil-downstream-cosmos-integration-guide.md"
	"docs/clairveil-downstream-cosmos-integration-guide-kr.md"
	"docs/clairveil-client-product-brief.md"
	"docs/clairveil-client-product-brief-kr.md"
	"docs/clairveil-client-ux-flows.md"
	"docs/clairveil-client-ux-flows-kr.md"
	"docs/clairveil-client-risk-decisions.md"
	"docs/clairveil-client-risk-decisions-kr.md"
	"docs/clairveil-client-api-checklist.md"
	"docs/clairveil-client-api-checklist-kr.md"
	"docs/clairveil-js-sdk-handoff.md"
	"docs/clairveil-js-sdk-handoff-kr.md"
	"docs/clairveil-proverd-remote-production-profile.md"
	"docs/clairveil-proverd-remote-production-profile-kr.md"
	"docs/clairveil-merkle-restore-sop.md"
	"docs/clairveil-merkle-restore-sop-kr.md"
	"docs/clairveil-release-versioning-policy.md"
	"docs/clairveil-release-versioning-policy-kr.md"
	"docs/clairveil-release-note-template.md"
	"docs/clairveil-release-note-template-kr.md"
	"docs/clairveil-threat-model.md"
	"docs/clairveil-threat-model-kr.md"
	"docs/clairveil-security-best-practices-review.md"
	"docs/clairveil-security-best-practices-review-kr.md"
	"docs/clairveil-local-privacy-walkthrough.md"
	"docs/clairveil-local-privacy-walkthrough-kr.md"
	"docs/clairveild-reference-app-plan.md"
	"docs/clairveild-reference-app-plan-kr.md"
	"docs/schemas/clairveil-js-wallet-contract.schema.json"
	"docs/schemas/README.md"
	"docs/schemas/README-kr.md"
	"x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_relay_withdraw_contract.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json"
	"examples/README.md"
	"examples/README-kr.md"
	"examples/audit-disclosure-keys/README.md"
	"examples/audit-disclosure-keys/README-kr.md"
	"examples/audit-disclosure-keys/package.json"
	"examples/js-sdk-fixture-validator/README.md"
	"examples/js-sdk-fixture-validator/README-kr.md"
	"examples/js-sdk-fixture-validator/package.json"
	"examples/js-sdk-prover-http-client/README.md"
	"examples/js-sdk-prover-http-client/README-kr.md"
	"examples/js-sdk-prover-http-client/package.json"
	"build/clairveil-proverd/Dockerfile"
	"build/clairveil-proverd/compose.yaml"
	"scripts/release-pack.sh"
	"scripts/release-pack-verify.sh"
)

for file in "${required_files[@]}"; do
	[[ -f "$pack_root/$file" ]] || fail "missing required file in archive: $file"
	if [[ "$file" != "SHA256SUMS.txt" ]]; then
		grep -Fq "  ./$file" "$pack_root/SHA256SUMS.txt" || fail "missing file from SHA256SUMS.txt: $file"
	fi
done

(
	cd "$pack_root"
	shasum -a 256 -c SHA256SUMS.txt >/dev/null
)

if [[ -z "${RELEASE_PACK_ARCHIVE:-}" ]]; then
	full_commit="$(git -C "$repo_root" rev-parse HEAD)"
	grep -Fq "commit: $full_commit" "$pack_root/RELEASE-MANIFEST.txt" || fail "manifest commit does not match HEAD"
fi

echo "release pack verified: $archive_path"
echo "checksum verified: $checksum_path"
echo "required files: ${#required_files[@]}"
