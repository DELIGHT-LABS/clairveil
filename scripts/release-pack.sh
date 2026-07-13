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

validate_pack_version() {
	local version="$1"
	local source_commit="$2"
	if python3 - "$version" <<'PY'
import re
import sys

identifier = r"(?:0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
pattern = re.compile(
    r"v(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)\.(?:0|[1-9][0-9]*)"
    rf"(?:-{identifier}(?:\.{identifier})*)?"
    r"(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"
)
raise SystemExit(0 if pattern.fullmatch(sys.argv[1]) else 1)
PY
	then
		local tag_ref="refs/tags/$version"
		local tag_type
		tag_type="$(git -C "$repo_root" cat-file -t "$tag_ref" 2>/dev/null || true)"
		[[ "$tag_type" == "tag" ]] || {
			echo "release version must name an annotated Git tag: $version" >&2
			exit 1
		}
		local tag_target_type
		tag_target_type="$(git -C "$repo_root" cat-file -p "$tag_ref" 2>/dev/null | sed -n 's/^type //p' | head -1)"
		[[ "$tag_target_type" == "commit" ]] || {
			echo "release tag must point directly to a commit: $version" >&2
			exit 1
		}
		local tag_commit
		tag_commit="$(git -C "$repo_root" rev-parse --verify "${tag_ref}^{commit}" 2>/dev/null || true)"
		[[ "$tag_commit" == "$source_commit" ]] || {
			echo "release tag must point to the packed source commit: $version" >&2
			exit 1
		}
		return
	fi
	[[ "$version" == "snapshot-$source_commit" ]] || {
		echo "untagged pack version must be snapshot-<full-commit-sha>: $version" >&2
		exit 1
	}
}

validate_release_path() {
	local path="$1"
	if [[ -z "$path" || "$path" == /* || "$path" == */ || "$path" == *\\* || "$path" == *//* || "$path" =~ [[:cntrl:]] ]]; then
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
version="${RELEASE_VERSION:-"$(git -C "$repo_root" describe --tags --exact-match "$source_commit" 2>/dev/null || printf 'snapshot-%s' "$source_commit")"}"
validate_pack_version "$version" "$source_commit"
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

canonicalize_packed_git_modes() {
	find "$pack_root" -type d -exec chmod 0755 {} +
	while IFS= read -r -d '' packed_file; do
		relative_path="${packed_file#"$pack_root/"}"
		git_entry="$(git -C "$repo_root" ls-tree "$source_commit" -- "$relative_path")"
		git_mode="${git_entry%% *}"
		case "$git_mode" in
		100644)
			chmod 0644 "$packed_file"
			;;
		100755)
			chmod 0755 "$packed_file"
			;;
		*)
			echo "unsupported Git file mode in release pack: $relative_path ($git_mode)" >&2
			exit 1
			;;
		esac
	done < <(find "$pack_root" -type f -print0)
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
canonicalize_packed_git_modes

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
chmod 0644 "$pack_root/RELEASE-MANIFEST.txt"

(
	cd "$pack_root"
	find . -type f ! -name SHA256SUMS.txt | LC_ALL=C sort | while IFS= read -r file; do
		shasum -a 256 "$file"
	done >SHA256SUMS.txt
)
chmod 0644 "$pack_root/SHA256SUMS.txt"

COPYFILE_DISABLE=1 tar -C "$work_dir" -czf "$archive_path" "$pack_name"
shasum -a 256 "$archive_path" >"$checksum_path"
chmod 0644 "$archive_path" "$checksum_path"

echo "release pack: $archive_path"
echo "checksum: $checksum_path"
