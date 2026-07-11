#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dist_dir="${DIST_DIR:-"$repo_root/dist"}"

require_clean_worktree() {
	if [[ -n "$(git -C "$repo_root" status --porcelain=v1 --untracked-files=all)" ]]; then
		echo "release pack generation requires a clean worktree" >&2
		exit 1
	fi
}

validate_release_path() {
	local path="$1"
	if [[ -z "$path" || "$path" == /* || "$path" == */ || "$path" == *$'\n'* ]]; then
		echo "invalid release pack path: $path" >&2
		exit 1
	fi
	case "/$path/" in
	*"/../"* | *"/./"*)
		echo "invalid release pack path: $path" >&2
		exit 1
		;;
	esac
}

require_clean_worktree
source_commit="$(git -C "$repo_root" rev-parse HEAD)"
commit="$(git -C "$repo_root" rev-parse --short=12 "$source_commit")"
version="${RELEASE_VERSION:-"$(git -C "$repo_root" describe --tags --always "$source_commit" 2>/dev/null || printf '%s' "$commit")"}"
if [[ ! "$version" =~ ^[0-9A-Za-z._+-]+$ ]]; then
	echo "release version contains unsupported characters" >&2
	exit 1
fi
pack_name="clairveil-handoff-${version}"
work_dir="$(mktemp -d)"
source_root="$work_dir/source"
pack_root="$work_dir/$pack_name"
archive_path="$dist_dir/${pack_name}.tar.gz"
checksum_path="$archive_path.sha256"

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

copy_path() {
	local path="$1"
	validate_release_path "$path"
	if [[ ! -e "$source_root/$path" ]]; then
		echo "missing release pack path: $path" >&2
		exit 1
	fi
	mkdir -p "$pack_root/$(dirname "$path")"
	cp -R "$source_root/$path" "$pack_root/$path"
}

mkdir -p "$source_root" "$pack_root" "$dist_dir"
git -C "$repo_root" archive --format=tar "$source_commit" | tar -xf - -C "$source_root"

paths_file="$source_root/scripts/release-pack-paths.txt"
[[ -f "$paths_file" ]] || {
	echo "missing release pack path manifest" >&2
	exit 1
}
duplicate_path="$(LC_ALL=C sort "$paths_file" | uniq -d | head -1)"
[[ -z "$duplicate_path" ]] || {
	echo "duplicate release pack path: $duplicate_path" >&2
	exit 1
}
while IFS= read -r path || [[ -n "$path" ]]; do
	copy_path "$path"
done <"$paths_file"

# Recheck after copying so a concurrent working-tree mutation cannot be
# attributed to the immutable HEAD recorded in the manifest.
require_clean_worktree
if [[ "$(git -C "$repo_root" rev-parse HEAD)" != "$source_commit" ]]; then
	echo "release pack source commit changed during generation" >&2
	exit 1
fi

generated_at_utc="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
awk \
	-v version="$version" \
	-v commit="$source_commit" \
	-v generated_at_utc="$generated_at_utc" \
	'{
		gsub(/@VERSION@/, version)
		gsub(/@COMMIT@/, commit)
		gsub(/@GENERATED_AT_UTC@/, generated_at_utc)
		print
	}' "$source_root/scripts/release-manifest-template.txt" >"$pack_root/RELEASE-MANIFEST.txt"

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
