# Clairveil Getting Started

> Korean version: [clairveil-getting-started-kr.md](clairveil-getting-started-kr.md)

This guide is the prerequisite, initialization, configuration, and first-response reference for the current checkout. Clairveil is `PUBLICATION_READY_EXPERIMENTAL`; these steps create a development chain and development Groth16 artifacts, not a production deployment or trusted setup ceremony.

## 1. Prerequisites

Required for the default repository workflow:

| Tool | Baseline | Used by |
| --- | --- | --- |
| Go | `1.25.12` | Build, tests, binaries, circuit setup |
| Python | `3.9+` | Init/release scripts and JSON validation |
| Bash | `/bin/bash` | Make targets and scripts |
| Git | No repository-pinned minimum | Clone, exact-ref docs, and release manifests |
| Make | No repository-pinned minimum | Repository build, test, init, and release targets |
| Node.js/npm | Node.js `22+` | `make examples` and `make ci` |

Optional tools are task-specific: Docker is used by the PostgreSQL reservation integration when no external DSN is supplied; `grpcurl` is required by the live batch localnet gate; and `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`, `buf`, and `clang-format` are needed only when regenerating proto output.

The default repository workflow requires no third-party Python packages. `make docs-check` uses the Go toolchain for full Draft 2020-12 validation of the canonical prover HTTP schema and fixtures.

Check the main versions before initialization:

```bash
git --version
make --version
go version
python3 --version
bash --version
node --version
npm --version
```

The scripts assume a Unix-like command environment. Platform support is established by the repository CI and the downstream project; this document does not claim support for an untested OS.

## 2. Resource Planning

`make init` generates every active development circuit artifact. Recorded full-shape batch runs used roughly `3,339,862,016` to `3,354,689,536` bytes peak RSS; the batch R1CS and PK are `122,813,535` and `209,218,621` bytes. Treat those as reference measurements, not hard limits. Plan for more than 4 GiB of available memory and at least 1 GiB of free disk, with additional headroom for the Go build cache and local chain data.

Validators need the required VK files after exact consensus-identity comparison. Provers additionally need the selected R1CS/PK pair and therefore have the larger storage and memory boundary.

## 3. Initialize And Start

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
make init
source ~/.clairveil/clairveil.env
clairveild start
```

`make init` first builds all binaries and installs the six project binaries listed in the README into `GOBIN` or `$(go env GOPATH)/bin`. The installed `clairveil-verify` binary is a legacy-only debugging helper; initialization and current note validation do not use it. It then:

1. moves an existing `~/.clairveil` to a timestamped backup;
2. generates the `privacy-note-v1` development artifact set;
3. creates `alice`, `bob`, `relayer`, and `auditor` development keys;
4. initializes genesis, funds the accounts, and creates the validator gentx;
5. places the auditor disclosure public key in genesis;
6. writes an exported runtime environment to `~/.clairveil/clairveil.env`.

The generated `init-out/*-key.json` files contain development key material. Keep the home private, never reuse these keys, and do not copy it into a production environment.

The active default listeners are RPC `26657`, P2P `26656`, and gRPC `9090`. The generated `app.toml` configures the REST address as `tcp://localhost:1317` but keeps `[api] enable = false`, so nothing binds `1317` by default. REST binds that address only after you explicitly set `enable = true` or start with `--api.enable`. Stop another local node using the active ports before starting the reference node. Smoke-test scripts accept port overrides; normal `clairveild start` configuration is changed in the generated files or with daemon flags.

## 4. Common Configuration

| Variable | Default | Scope |
| --- | --- | --- |
| `GOBIN` | `go env GOBIN`, then `$(go env GOPATH)/bin` | Binary installation |
| `CLAIRVEIL_HOME` | `~/.clairveil` | `make init` home and backup location |
| `CHAIN_ID` | `clairveil-local-1` | Init and smoke-test chain ID |
| `NODE_NAME` | `local` | Init and smoke-test node moniker |
| `KEYRING_BACKEND` | `test` | Development init keyring |
| `CLAIRVEIL_INIT_ACCOUNTS` | `alice bob relayer auditor` | Space-separated init keys |
| `VALIDATOR_KEY` / `AUDITOR_KEY` | `alice` / `auditor` | Required roles; both must be in the account list |
| `FUND_AMOUNT` / `STAKE_AMOUNT` | Script defaults | Development genesis balances |
| `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR` | `<home>/artifacts/privacy` under `make init`; runtime fallback `.` when unset | Artifact output/runtime directory; source `clairveil.env` or set it explicitly |
| `CLAIRVEILD_BIN` / `CLAIRVEIL_SETUP_BIN` | Installed binary | Explicit binary override |
| `RPC_PORT`, `P2P_PORT`, `ABCI_PORT`, `GRPC_PORT`, `API_PORT`, `PPROF_PORT` | Script defaults | Smoke/localnet script configuration; `API_PORT` does not bind while REST is disabled |

Example isolated initialization:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home \
CHAIN_ID=my-local-chain \
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" \
make init
```

`clairveil-setup` writes `privacy_zk_checksums.env` as shell assignments, not exported variables. When sourcing a raw file outside `make init`, identify its directory explicitly and export all assignments:

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/absolute/path/to/artifacts/privacy
set -a
source "$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

The `~/.clairveil/clairveil.env` file generated by `make init` already contains `export` statements and can be sourced directly.

## 5. First Checks

Without a running node:

```bash
make docs-check
go test ./...
make build
```

With no other node on the active default RPC, P2P, or gRPC ports:

```bash
make localnet-smoke
make privacy-e2e-smoke
```

Use [clairveil-testing-guide.md](clairveil-testing-guide.md) to choose the heavier batch, payroll, benchmark, and release gates.

## 6. Troubleshooting And Cleanup

- `clairveild: command not found`: add `$(go env GOPATH)/bin` or the value of `go env GOBIN` to `PATH`, then rerun `make install`.
- setup is killed or reports out-of-memory/no-space: stop competing workloads, free disk, or generate artifacts on a larger host. Reuse only a complete artifact directory that passes strict preflight.
- artifact checksum or circuit identity mismatch: remove stale development artifacts/proof jobs, regenerate the exact active set, and start from fresh genesis. Environment checksums cannot override consensus identity.
- `address already in use`: stop the existing node or assign all relevant port overrides to the smoke script; do not change only one of the RPC/P2P/gRPC/REST endpoints.
- `privacy_scan` returns `ResourceExhausted`: if one typed record exceeds `max_encoded_bytes`, increase that byte budget up to the server maximum; reducing output/event limits cannot split a single record. If the record still cannot fit, treat it as a server/contract incident. Persist the last accepted cursor and never skip it.
- `commitment_paths_at_root` returns `ResourceExhausted`: for an oversized historical rebuild, use the current root or a trusted local historical index; for temporary rebuild-admission saturation, use bounded retry. Reducing the number of requested commitments does not reduce the historical tree's leaf count.
- `make release-pack-verify` rejects a dirty tree: run `make docs-check` while editing. Use a clean untagged commit only for a commit-bound CI snapshot; generate and verify a publishable release pack from the final annotated exact-SemVer tagged commit, or verify an explicit archive with its out-of-band commit.

To remove a disposable home, stop the node first and delete only the path you explicitly selected. `make init` preserves an existing home as `<home>.backup-YYYYMMDD-HHMMSS`; review and remove old backups manually because they can contain private development keys and wallet data.

Next references: [architecture](clairveil-architecture.md), [local walkthrough](clairveil-local-privacy-walkthrough.md), [operations guide](clairveil-operations-guide.md), and the [complete documentation index](README.md).
