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
explicit_archive=false
if [[ -n "${RELEASE_PACK_ARCHIVE:-}" ]]; then
	explicit_archive=true
fi

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

fail() {
	echo "release pack verify failed: $*" >&2
	exit 1
}

validate_release_path() {
	local path="$1"
	if [[ -z "$path" || "$path" == /* || "$path" == */ || "$path" == *$'\n'* ]]; then
		fail "invalid selected release path: $path"
	fi
	case "/$path/" in
	*"/../"* | *"/./"*)
		fail "invalid selected release path: $path"
		;;
	esac
}

release_file_mode() {
	python3 - "$1" <<'PY'
import os
import stat
import sys

print(f"{stat.S_IMODE(os.stat(sys.argv[1]).st_mode):04o}")
PY
}

if [[ -z "${RELEASE_PACK_ARCHIVE:-}" ]]; then
	"$repo_root/scripts/release-pack.sh" >/dev/null
elif [[ ! -f "$archive_path" || ! -f "$checksum_path" ]]; then
	"$repo_root/scripts/release-pack.sh" >/dev/null
fi

[[ -f "$archive_path" ]] || fail "missing archive: $archive_path"
[[ -f "$checksum_path" ]] || fail "missing checksum: $checksum_path"

checksum_line_count="$(wc -l <"$checksum_path" | tr -d ' ')"
checksum_format_count="$(grep -Ec '^[0-9a-f]{64}  .+$' "$checksum_path" 2>/dev/null || true)"
[[ "$checksum_line_count" == "1" && "$checksum_format_count" == "1" ]] || fail "archive checksum file is not canonical"
expected_checksum="$(awk '{print $1; exit}' "$checksum_path")"
actual_checksum="$(shasum -a 256 "$archive_path" | awk '{print $1; exit}')"
[[ -n "$expected_checksum" ]] || fail "empty checksum file: $checksum_path"
[[ "$expected_checksum" == "$actual_checksum" ]] || fail "archive checksum mismatch"

if ! pack_root_name="$(python3 - "$archive_path" "$work_dir" <<'PY'
import os
from pathlib import Path, PurePosixPath
import shutil
import sys
import tarfile

archive = Path(sys.argv[1])
destination = Path(sys.argv[2])
max_archive_bytes = 512 * 1024 * 1024
max_file_bytes = 128 * 1024 * 1024
max_total_bytes = 512 * 1024 * 1024
max_members = 10_000

try:
    if archive.stat().st_size > max_archive_bytes:
        raise ValueError("compressed archive exceeds the verification limit")
    seen: set[str] = set()
    directory_members: set[str] = set()
    required_directories: set[str] = set()
    top_level: str | None = None
    total_bytes = 0
    member_count = 0
    with tarfile.open(archive, "r:gz") as tf:
        for member in tf:
            member_count += 1
            if member_count > max_members:
                raise ValueError("archive contains too many members")
            name = member.name
            path = PurePosixPath(name)
            if (
                not name
                or path.is_absolute()
                or str(path) != name
                or any(part in ("", ".", "..") for part in path.parts)
                or any(ord(char) < 32 or ord(char) == 127 for char in name)
            ):
                raise ValueError(f"non-canonical archive path: {name!r}")
            if name in seen:
                raise ValueError(f"duplicate archive member: {name}")
            seen.add(name)
            if not (member.isdir() or member.isreg()):
                raise ValueError(f"non-regular archive member: {name}")
            if member.isdir():
                if member.mode != 0o755:
                    raise ValueError(
                        f"non-canonical directory mode for {name}: {member.mode:04o}"
                    )
                directory_members.add(name)
            elif member.mode not in (0o644, 0o755):
                raise ValueError(
                    f"non-canonical regular-file mode for {name}: {member.mode:04o}"
                )
            if len(path.parts) == 1 and not member.isdir():
                raise ValueError("archive top level must be a directory")
            if top_level is None:
                top_level = path.parts[0]
            elif path.parts[0] != top_level:
                raise ValueError("archive must contain exactly one top-level directory")
            for parent in path.parents:
                if str(parent) == ".":
                    break
                required_directories.add(str(parent))

            target = destination.joinpath(*path.parts)
            if member.isdir():
                target.mkdir(parents=True, exist_ok=True)
                os.chmod(target, member.mode)
                continue
            if member.size > max_file_bytes:
                raise ValueError(f"archive member exceeds the file limit: {name}")
            total_bytes += member.size
            if total_bytes > max_total_bytes:
                raise ValueError("archive contents exceed the verification limit")
            target.parent.mkdir(parents=True, exist_ok=True)
            source = tf.extractfile(member)
            if source is None:
                raise ValueError(f"failed to read archive member: {name}")
            with source, target.open("xb") as output:
                shutil.copyfileobj(source, output)
            os.chmod(target, member.mode)
    if top_level is None:
        raise ValueError("archive is empty")
    missing_directories = required_directories - directory_members
    if missing_directories:
        raise ValueError(
            "archive omits explicit directory members: "
            + ", ".join(sorted(missing_directories)[:5])
        )
    print(top_level)
except (OSError, tarfile.TarError, ValueError) as error:
    print(f"unsafe release archive: {error}", file=sys.stderr)
    raise SystemExit(1)
PY
)"; then
	fail "archive structure validation failed"
fi
pack_root="$work_dir/$pack_root_name"
[[ -d "$pack_root" ]] || fail "missing extracted pack root: $pack_root_name"
while IFS= read -r -d '' packed_directory; do
	[[ "$(release_file_mode "$packed_directory")" == "0755" ]] || fail "archive directory must have mode 0755: ${packed_directory#"$pack_root/"}"
done < <(find "$pack_root" -type d -print0)

manifest_commit_line_count="$(grep -Ec '^commit:' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
manifest_commit_count="$(grep -Ec '^commit: [0-9a-f]{40}$' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
[[ "$manifest_commit_line_count" == "1" && "$manifest_commit_count" == "1" ]] || fail "manifest must contain exactly one canonical full commit"
manifest_commit="$(sed -nE 's/^commit: ([0-9a-f]{40})$/\1/p' "$pack_root/RELEASE-MANIFEST.txt")"

if [[ "$explicit_archive" == true ]]; then
	[[ -n "${RELEASE_PACK_EXPECTED_COMMIT:-}" ]] || fail "RELEASE_PACK_EXPECTED_COMMIT is required for an explicit archive"
	[[ "$RELEASE_PACK_EXPECTED_COMMIT" =~ ^[0-9a-f]{40}$ ]] || fail "RELEASE_PACK_EXPECTED_COMMIT must be a canonical 40-character commit SHA"
	expected_commit="$RELEASE_PACK_EXPECTED_COMMIT"
else
	expected_commit="$(git -C "$repo_root" rev-parse HEAD)"
fi
[[ "$manifest_commit" == "$expected_commit" ]] || fail "manifest commit does not match expected commit"
git -C "$repo_root" cat-file -e "${manifest_commit}^{commit}" 2>/dev/null || fail "manifest commit is not available in the local Git repository"

manifest_version_line_count="$(grep -Ec '^version:' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
manifest_version_count="$(grep -Ec '^version: [0-9A-Za-z._+-]+$' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
manifest_generated_line_count="$(grep -Ec '^generated_at_utc:' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
manifest_generated_count="$(grep -Ec '^generated_at_utc: [0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$' "$pack_root/RELEASE-MANIFEST.txt" 2>/dev/null || true)"
[[ "$manifest_version_line_count" == "1" && "$manifest_version_count" == "1" ]] || fail "manifest must contain exactly one canonical version"
[[ "$manifest_generated_line_count" == "1" && "$manifest_generated_count" == "1" ]] || fail "manifest must contain exactly one canonical generation timestamp"
manifest_version="$(sed -nE 's/^version: ([0-9A-Za-z._+-]+)$/\1/p' "$pack_root/RELEASE-MANIFEST.txt")"
manifest_generated_at_utc="$(sed -nE 's/^generated_at_utc: ([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z)$/\1/p' "$pack_root/RELEASE-MANIFEST.txt")"
[[ "$pack_root_name" == "clairveil-handoff-${manifest_version}" ]] || fail "archive root does not match manifest version"

manifest_template="$work_dir/release-manifest-template.txt"
expected_manifest="$work_dir/expected-release-manifest.txt"
git -C "$repo_root" show "${manifest_commit}:scripts/release-manifest-template.txt" >"$manifest_template" || fail "manifest template is missing from manifest commit"
awk \
	-v version="$manifest_version" \
	-v commit="$manifest_commit" \
	-v generated_at_utc="$manifest_generated_at_utc" \
	'{
		gsub(/@VERSION@/, version)
		gsub(/@COMMIT@/, commit)
		gsub(/@GENERATED_AT_UTC@/, generated_at_utc)
		print
	}' "$manifest_template" >"$expected_manifest"
cmp -s "$expected_manifest" "$pack_root/RELEASE-MANIFEST.txt" || fail "release manifest differs from the canonical Git template"

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
	"proto/clairveil/privacy/v1/batch_feasibility.proto"
	"docs/clairveil-release-handoff-pack.md"
	"docs/clairveil-release-handoff-pack-kr.md"
	"docs/clairveil-circuits.md"
	"docs/clairveil-circuits-kr.md"
	"docs/clairveil-batch-joinsplit-16x32.md"
	"docs/clairveil-batch-joinsplit-16x32-kr.md"
	"docs/clairveil-batch-joinsplit-16x32-session-4-validation-report.md"
	"docs/clairveil-batch-joinsplit-16x32-session-4-validation-report-kr.md"
	"docs/clairveil-batch-joinsplit-localnet-tutorial.md"
	"docs/clairveil-batch-joinsplit-localnet-tutorial-kr.md"
	"docs/clairveil-session3b-batch-transfer-handoff.md"
	"docs/clairveil-session3b-batch-transfer-handoff-kr.md"
	"docs/clairveil-cli-reference.md"
	"docs/clairveil-cli-reference-kr.md"
	"docs/clairveil-testing-guide.md"
	"docs/clairveil-testing-guide-kr.md"
	"docs/clairveil-operations-guide.md"
	"docs/clairveil-operations-guide-kr.md"
	"docs/clairveil-privacy-accounting-design-note.md"
	"docs/clairveil-privacy-accounting-design-note-kr.md"
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
	"docs/clairveil-bulk-transfer-product-handoff-kr.md"
	"plans/clairveil-bulk-transfer-strategy-kr.md"
	"plans/clairveil-bulk-transfer-time-simulation-kr.md"
	"docs/clairveil-note-reservation-design-kr.md"
	"docs/clairveil-note-reservation-design.md"
	"docs/clairveil-reference-payroll-product.md"
	"docs/clairveil-reference-payroll-product-kr.md"
	"docs/clairveil-reference-payroll-product-policy.md"
	"docs/clairveil-reference-payroll-product-policy-kr.md"
	"docs/clairveil-reference-payroll-js-sdk-handoff.md"
	"docs/clairveil-reference-payroll-js-sdk-handoff-kr.md"
	"docs/clairveil-reference-payroll-wallet-handoff.md"
	"docs/clairveil-reference-payroll-wallet-handoff-kr.md"
	"docs/clairveil-reference-payroll-live-localnet-tutorial.md"
	"docs/clairveil-reference-payroll-live-localnet-tutorial-kr.md"
	"docs/clairveil-reference-payroll-rehearsal-kr.md"
	"docs/clairveil-reference-payroll-localnet-rehearsal-result-kr.md"
	"plans/clairveil-scan-optimization-implementation-plan.md"
	"plans/clairveil-scan-optimization-implementation-plan-kr.md"
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
	"plans/clairveild-reference-app-plan.md"
	"plans/clairveild-reference-app-plan-kr.md"
	"docs/schemas/clairveil-js-wallet-contract.schema.json"
	"docs/schemas/README.md"
	"docs/schemas/README-kr.md"
	"plans/clairveil-bulk-transfer-implementation-plan-kr.md"
	"plans/clairveil-public-benchmark-plan-kr.md"
	"plans/clairveil-public-capacity-benchmark-followup-plan-kr.md"
	"plans/clairveil-public-capacity-claim-execution-plan-kr.md"
	"x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_relay_withdraw_contract.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_batch_joinsplit_v1_contract.json"
	"x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json"
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
	"examples/reference-payroll/README.md"
	"examples/reference-payroll/README-kr.md"
	"examples/reference-payroll/payroll-demo.json"
	"build/clairveil-proverd/Dockerfile"
	"build/clairveil-proverd/compose.yaml"
	"scripts/release-pack.sh"
	"scripts/release-pack-verify.sh"
	"scripts/release-pack-paths.txt"
	"scripts/release-manifest-template.txt"
	"scripts/privacy-batch-joinsplit-localnet.sh"
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
expected_sums="$work_dir/expected-SHA256SUMS.txt"
(
	cd "$pack_root"
	find . -type f ! -name SHA256SUMS.txt | LC_ALL=C sort | while IFS= read -r file; do
		shasum -a 256 "$file"
	done
) >"$expected_sums"
cmp -s "$expected_sums" "$pack_root/SHA256SUMS.txt" || fail "SHA256SUMS.txt is not canonical and complete"
[[ "$(release_file_mode "$pack_root/RELEASE-MANIFEST.txt")" == "0644" ]] || fail "RELEASE-MANIFEST.txt must have mode 0644"
[[ "$(release_file_mode "$pack_root/SHA256SUMS.txt")" == "0644" ]] || fail "SHA256SUMS.txt must have mode 0644"

selected_paths_file="$work_dir/release-pack-paths.txt"
git -C "$repo_root" show "${manifest_commit}:scripts/release-pack-paths.txt" >"$selected_paths_file" || fail "release path manifest is missing from manifest commit"
duplicate_path="$(LC_ALL=C sort "$selected_paths_file" | uniq -d | head -1)"
[[ -z "$duplicate_path" ]] || fail "release path manifest contains a duplicate: $duplicate_path"

expected_files_unsorted="$work_dir/expected-files.unsorted"
expected_files="$work_dir/expected-files.txt"
actual_files="$work_dir/actual-files.txt"
: >"$expected_files_unsorted"
while IFS= read -r selected_path || [[ -n "$selected_path" ]]; do
	validate_release_path "$selected_path"
	matched_files="$(git -C "$repo_root" ls-tree -r --name-only "$manifest_commit" -- "$selected_path")"
	[[ -n "$matched_files" ]] || fail "selected release path is missing from manifest commit: $selected_path"
	printf '%s\n' "$matched_files" >>"$expected_files_unsorted"
done <"$selected_paths_file"
LC_ALL=C sort -u "$expected_files_unsorted" >"$expected_files"

while IFS= read -r -d '' packed_file; do
	relative_path="${packed_file#"$pack_root/"}"
	case "$relative_path" in
	"RELEASE-MANIFEST.txt" | "SHA256SUMS.txt")
		continue
		;;
	esac
	printf '%s\n' "$relative_path"
done < <(find "$pack_root" -type f -print0) | LC_ALL=C sort -u >"$actual_files"
cmp -s "$expected_files" "$actual_files" || fail "archive file set differs from the selected manifest Git tree"

while IFS= read -r -d '' packed_file; do
	relative_path="${packed_file#"$pack_root/"}"
	case "$relative_path" in
	"RELEASE-MANIFEST.txt" | "SHA256SUMS.txt")
		continue
		;;
	esac
	git -C "$repo_root" cat-file -e "${manifest_commit}:${relative_path}" 2>/dev/null || fail "archive file is not tracked by manifest commit: $relative_path"
	if ! git -C "$repo_root" show "${manifest_commit}:${relative_path}" | cmp -s - "$packed_file"; then
		fail "archive file differs from manifest commit: $relative_path"
	fi
	git_entry="$(git -C "$repo_root" ls-tree "$manifest_commit" -- "$relative_path")"
	git_mode="${git_entry%% *}"
	case "$git_mode" in
	100644)
		expected_mode="0644"
		;;
	100755)
		expected_mode="0755"
		;;
	*)
		fail "unsupported Git file mode in release pack: $relative_path ($git_mode)"
		;;
	esac
	actual_mode="$(release_file_mode "$packed_file")"
	[[ "$actual_mode" == "$expected_mode" ]] || fail "archive file mode differs from manifest commit: $relative_path (expected $expected_mode, got $actual_mode)"
done < <(find "$pack_root" -type f -print0)

echo "release pack verified: $archive_path"
echo "checksum verified: $checksum_path"
echo "required files: ${#required_files[@]}"
echo "manifest commit verified: $manifest_commit"
