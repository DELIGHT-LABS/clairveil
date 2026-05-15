#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
image="${CLAIRVEIL_PROVERD_IMAGE:-delightlabs/clairveil-proverd:local}"
targetarch="${TARGETARCH:-"$(go env GOARCH)"}"
dockerfile="$repo_root/build/clairveil-proverd/Dockerfile"
compose_file="$repo_root/build/clairveil-proverd/compose.yaml"

if ! command -v docker >/dev/null 2>&1; then
	echo "docker is required for docker-proverd-build" >&2
	exit 127
fi
if ! docker info >/dev/null 2>&1; then
	echo "docker daemon is not reachable; start Docker and retry docker-proverd-build" >&2
	exit 1
fi

docker compose -f "$compose_file" config >/dev/null
docker build \
	--build-arg "TARGETARCH=$targetarch" \
	-f "$dockerfile" \
	-t "$image" \
	"$repo_root"

docker image inspect "$image" >/dev/null

echo "prover image built: $image"
echo "target arch: $targetarch"
