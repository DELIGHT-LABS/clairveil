#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

resolve_gobin() {
	local gobin
	gobin="${GOBIN:-"$(go env GOBIN)"}"
	if [[ -z "$gobin" ]]; then
		gobin="$(go env GOPATH)/bin"
	fi
	printf '%s\n' "$gobin"
}

gobin="$(resolve_gobin)"
binaries=(
	clairveild
	clairveil-setup
	clairveil-verify
	clairveil-proverd
)

mkdir -p "$gobin"

for binary in "${binaries[@]}"; do
	src="$repo_root/$binary"
	if [[ ! -x "$src" ]]; then
		echo "missing built binary: $src" >&2
		echo "run 'make build' before running this script directly" >&2
		exit 1
	fi

	install -m 0755 "$src" "$gobin/$binary"
	echo "installed $binary -> $gobin/$binary"
done

echo "GOBIN: $gobin"
