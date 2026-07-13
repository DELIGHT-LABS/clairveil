#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

container_name=""
output_file="$(mktemp "${TMPDIR:-/tmp}/clairveil-sql-integration.XXXXXX")"
cleanup() {
  rm -f "$output_file"
  if [[ -n "$container_name" ]]; then
    docker rm -f "$container_name" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

postgres_dsn="${CLAIRVEIL_TEST_POSTGRES_DSN:-}"
if [[ -z "$postgres_dsn" ]]; then
  command -v docker >/dev/null 2>&1 || {
    echo "docker is required when CLAIRVEIL_TEST_POSTGRES_DSN is not set" >&2
    exit 1
  }
  docker info >/dev/null
  container_name="clairveil-g3b03-postgres-$$"
  docker run --rm -d \
    --name "$container_name" \
    -e POSTGRES_HOST_AUTH_METHOD=trust \
    -e POSTGRES_DB=clairveil_g3b03 \
    -p 127.0.0.1::5432 \
    postgres:17-alpine >/dev/null
  ready=false
  for _ in $(seq 1 60); do
    if docker exec "$container_name" pg_isready -U postgres -d clairveil_g3b03 >/dev/null 2>&1; then
      ready=true
      break
    fi
    sleep 1
  done
  if [[ "$ready" != true ]]; then
    echo "PostgreSQL integration container did not become ready" >&2
    docker logs "$container_name" >&2 || true
    exit 1
  fi
  host_port="$(docker port "$container_name" 5432/tcp | awk -F: 'END {print $NF}')"
  if [[ ! "$host_port" =~ ^[0-9]+$ ]]; then
    echo "failed to resolve the PostgreSQL integration port" >&2
    exit 1
  fi
  postgres_dsn="postgres://postgres@127.0.0.1:${host_port}/clairveil_g3b03?sslmode=disable"
fi

set +e
CLAIRVEIL_TEST_POSTGRES_DSN="$postgres_dsn" \
  go test ./x/privacy/client/sdk/reservation \
  -run '^TestSQLStore(SQLite|PostgreSQL)IntegrationGraphAtomicityAndRecovery$' \
  -count=1 -v 2>&1 | tee "$output_file"
test_status=${PIPESTATUS[0]}
set -e
if [[ "$test_status" -ne 0 ]]; then
  exit "$test_status"
fi
if grep -F -- "--- SKIP:" "$output_file" >/dev/null; then
  echo "SQL integration test skipped; refusing to record PASS" >&2
  exit 1
fi
for test_name in \
  TestSQLStoreSQLiteIntegrationGraphAtomicityAndRecovery \
  TestSQLStorePostgreSQLIntegrationGraphAtomicityAndRecovery
do
  grep -F -- "--- PASS: ${test_name}" "$output_file" >/dev/null || {
    echo "missing exact PASS marker for ${test_name}" >&2
    exit 1
  }
done

echo "SQLite and PostgreSQL reservation SQL integration passed without skips"
