#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
previous_source_commit="0fc818c90fe98a876c8a2531e7c70ba5efac4b90"
output_root="${1:-}"

fail() {
	echo "session 3a artifact preparation failed: $*" >&2
	exit 1
}

[[ -n "$output_root" ]] || fail "usage: $0 OUTPUT_ROOT"
[[ -z "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]] || \
	fail "the current source tree must be clean so artifact provenance is commit-bound"

current_source_commit="$(git -C "$repo_root" rev-parse HEAD)"
resolved_previous_commit="$(git -C "$repo_root" rev-parse "${previous_source_commit}^{commit}")"
[[ "$resolved_previous_commit" == "$previous_source_commit" ]] || \
	fail "previous source commit did not resolve exactly: $previous_source_commit"

output_parent="$(cd "$(dirname "$output_root")" && pwd -P)"
output_root="$output_parent/$(basename "$output_root")"
case "$output_root" in
	"$repo_root"|"$repo_root"/*) fail "OUTPUT_ROOT must be outside the repository" ;;
esac
[[ ! -e "$output_root" ]] || fail "OUTPUT_ROOT already exists: $output_root"

mkdir "$output_root"
cleanup_output=1
cleanup() {
	if [[ "$cleanup_output" == "1" ]]; then
		rm -rf "$output_root"
	fi
}
trap cleanup EXIT

previous_source_dir="$output_root/previous-source"
previous_dir="$output_root/previous"
current_dir="$output_root/current"
mkdir -p "$previous_source_dir"

git -C "$repo_root" archive "$previous_source_commit" | tar -xf - -C "$previous_source_dir"
(
	cd "$previous_source_dir"
	go run ./cmd/clairveil-setup -out "$previous_dir"
)
cp -R "$previous_dir" "$current_dir"
(
	cd "$repo_root"
	go run ./cmd/clairveil-setup -circuit joinsplit -out "$current_dir" -overwrite
)

rm -rf "$previous_source_dir"
cat >"$output_root/session-3a-source-provenance.env" <<EOF
CLAIRVEIL_SESSION3A_PREVIOUS_SOURCE_COMMIT=$previous_source_commit
CLAIRVEIL_SESSION3A_CURRENT_SOURCE_COMMIT=$current_source_commit
EOF
cleanup_output=0

echo "session 3a artifact evidence prepared"
echo "previous_source_commit=$previous_source_commit"
echo "current_source_commit=$current_source_commit"
echo "previous_artifact_dir=$output_root/previous"
echo "current_artifact_dir=$output_root/current"
