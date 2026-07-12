#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
current_dir="${CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR:-}"
previous_dir="${CLAIRVEIL_PRIVACY_PREVIOUS_ZK_ARTIFACT_DIR:-}"
work_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$work_dir"
}
trap cleanup EXIT

fail() {
	echo "session 3a validation evidence failed: $*" >&2
	exit 1
}

[[ -n "$current_dir" ]] || fail "CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR is required"
[[ -n "$previous_dir" ]] || fail "CLAIRVEIL_PRIVACY_PREVIOUS_ZK_ARTIFACT_DIR is required"
[[ -d "$current_dir" ]] || fail "current artifact directory does not exist: $current_dir"
[[ -d "$previous_dir" ]] || fail "previous artifact directory does not exist: $previous_dir"

current_dir="$(cd "$current_dir" && pwd -P)"
previous_dir="$(cd "$previous_dir" && pwd -P)"
[[ "$current_dir" != "$previous_dir" ]] || fail "current and previous artifact directories must differ"

for artifact_dir in "$current_dir" "$previous_dir"; do
	for filename in \
		privacy_zk_manifest.json \
		privacy_joinsplit_r1cs.bin \
		privacy_joinsplit_pk.bin \
		privacy_joinsplit_vk.bin; do
		[[ -f "$artifact_dir/$filename" ]] || fail "missing artifact input: $artifact_dir/$filename"
	done
done

export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR="$current_dir"
export CLAIRVEIL_PRIVACY_PREVIOUS_ZK_ARTIFACT_DIR="$previous_dir"

run_exact_test() {
	local package="$1"
	local test_name="$2"
	local gate_env="$3"
	local list_output="$work_dir/${test_name}.list"
	local test_output="$work_dir/${test_name}.test"

	(
		cd "$repo_root"
		go test "$package" -list "^${test_name}$" >"$list_output"
	) || fail "could not list $package/$test_name"
	[[ "$(grep -Fxc "$test_name" "$list_output" || true)" == "1" ]] || fail "exact test is missing or ambiguous: $package/$test_name"

	if ! (
		cd "$repo_root"
		env "$gate_env=1" go test "$package" -run "^${test_name}$" -count=1 -p=1 -v 2>&1 | tee "$test_output"
	); then
		fail "go test failed: $package/$test_name"
	fi

	grep -Fxq "=== RUN   $test_name" "$test_output" || fail "test did not run: $package/$test_name"
	grep -Eq "^--- PASS: ${test_name} \\(.*\\)$" "$test_output" || fail "exact test did not pass: $package/$test_name"
	if grep -Eq '^--- SKIP:|\[no tests to run\]|warning: no tests to run' "$test_output"; then
		fail "skip or empty test selection detected: $package/$test_name"
	fi
}

run_exact_test \
	./x/privacy/zk \
	TestJoinSplitDevelopmentArtifactRotationGate \
	CLAIRVEIL_RUN_JOINSPLIT_ARTIFACT_ROTATION_GATE
run_exact_test \
	./x/privacy/circuit \
	TestJoinSplitOldAndNewProofIdentitiesAreMutuallyExclusive \
	CLAIRVEIL_RUN_JOINSPLIT_ARTIFACT_PROOF_ROTATION_GATE
run_exact_test \
	./x/privacy \
	TestS4B02FreshGenesisUsesRotatedJoinSplitIdentity \
	CLAIRVEIL_RUN_JOINSPLIT_FRESH_GENESIS_GATE

echo "session 3a validation evidence verified: artifact readiness, old/new proof-VK mismatch, fresh genesis"
