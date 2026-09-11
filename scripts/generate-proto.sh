#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
sdk_proto="${SDK_PROTO:-"$(go list -m -f '{{.Dir}}' github.com/cosmos/cosmos-sdk)/proto"}"
googleapis_proto="${GOOGLEAPIS_PROTO:-"$(go env GOPATH)/pkg/mod/github.com/grpc-ecosystem/grpc-gateway@v1.16.0/third_party/googleapis"}"
out_dir="$(mktemp -d)"

cleanup() {
	rm -rf "$out_dir"
}
trap cleanup EXIT

for tool in protoc protoc-gen-gocosmos protoc-gen-grpc-gateway; do
	if ! command -v "$tool" >/dev/null 2>&1; then
		echo "$tool is required but was not found in PATH" >&2
		exit 1
	fi
done

for package in privacy/v1 privacy/v2; do
 protoc \
  -I "$repo_root/proto" \
  -I "$sdk_proto" \
  -I "$googleapis_proto" \
  --gocosmos_out=plugins=grpc:"$out_dir" \
  --grpc-gateway_out=logtostderr=true,allow_colon_final_segments=true:"$out_dir" \
  "$repo_root/proto/clairveil/$package/"*.proto
done

generated_dir="$out_dir/github.com/DELIGHT-LABS/clairveil/x/privacy/types"
cp "$generated_dir"/*.pb.go "$repo_root/x/privacy/types/"
cp "$generated_dir"/*.pb.gw.go "$repo_root/x/privacy/types/"

gofmt -w "$repo_root"/x/privacy/types/*.pb.go "$repo_root"/x/privacy/types/*.pb.gw.go

mkdir -p "$repo_root/x/privacy/types/v2"
cp "$generated_dir/v2"/*.pb.go "$repo_root/x/privacy/types/v2/"
cp "$generated_dir/v2"/*.pb.gw.go "$repo_root/x/privacy/types/v2/"
gofmt -w "$repo_root"/x/privacy/types/v2/*.go
