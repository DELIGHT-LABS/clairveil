#!/usr/bin/env bash
set -euo pipefail

govulncheck_version="${GOVULNCHECK_VERSION:-v1.3.0}"
go_toolchain="${GOVULNCHECK_GOTOOLCHAIN:-go1.25.11}"
tmp_dir="$(mktemp -d)"
report_path="$tmp_dir/govulncheck.jsonl"
trap 'rm -rf "$tmp_dir"' EXIT

GOTOOLCHAIN="$go_toolchain" GOBIN="$tmp_dir" go install "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}"

set +e
GOTOOLCHAIN="$go_toolchain" "$tmp_dir/govulncheck" -format=json ./... >"$report_path"
scanner_status=$?
set -e

GOTOOLCHAIN="$go_toolchain" go run ./cmd/clairveil-govulncheck-policy "$report_path" "$scanner_status"
