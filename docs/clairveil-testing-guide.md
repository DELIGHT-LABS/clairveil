# Clairveil Testing Guide

This document describes Clairveil's test layers and what each command guarantees.

Korean version: [clairveil-testing-guide-kr.md](clairveil-testing-guide-kr.md)

## 1. Quick Validation

The default validation for ordinary PRs is:

```bash
make ci
make vulncheck
```

`make ci` and `make vulncheck` do not require a running `clairveild` node. `make ci` runs Go tests, Go binary builds, and JS example validation.

For release candidates or larger changes, run:

```bash
make release-check
make release-pack
make release-pack-verify
```

## 2. Make Targets

| Command | Meaning |
| --- | --- |
| `make test` | run `go test ./...` |
| `make build` | build `clairveild`, `clairveil-setup`, `clairveil-verify`, and `clairveil-proverd` |
| `make install` | run `make build`, then copy Clairveil binaries to `GOBIN` or `GOPATH/bin` |
| `make init` | run `make install`, then initialize the default local chain home for `clairveild start` |
| `make proto` | regenerate privacy protobuf/gateway Go files |
| `make examples` | run JS audit key, fixture validator, and prover HTTP client examples |
| `make ci` | `test`, `build`, and `examples` |
| `make vulncheck` | run govulncheck policy gate |
| `make localnet-smoke` | briefly verify that the reference daemon can start from genesis |
| `make privacy-e2e-smoke` | validate full deposit, transfer, disclosure, and withdraw flow |
| `make release-check` | `ci`, `vulncheck`, `localnet-smoke`, and `privacy-e2e-smoke` |
| `make release-pack` | create downstream handoff archive and sha256 |
| `make release-pack-verify` | verify handoff archive checksum, internal checksum, required files, and manifest commit |
| `make docker-proverd-build` | validate prover Dockerfile/compose build |

## 3. Go Unit/Integration Tests

```bash
make test
```

Main coverage:

| Package | Coverage |
| --- | --- |
| `x/privacy/circuit` | Deposit/Spend/JoinSplit circuit constraints |
| `x/privacy/keeper` | deposit/transfer/withdraw state transitions, Merkle capacity, query error handling |
| `x/privacy/types` | message validation, address, gateway paths |
| `x/privacy/client/cli` | CLI parsing, output, disclosure decode helpers |
| `x/privacy/client/sdk/*` | identity, deposit, scan, transfer, withdraw, disclosure, prover transport |
| `x/privacy/client/sdk/conformance` | JS/web wallet fixture contract |
| `x/privacy/zk` | artifact manifest/checksum loading |

Focused package examples:

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
```

## 4. JS/Web Wallet Fixture Validation

```bash
make examples
```

Internally runs:

```bash
npm --prefix examples/audit-disclosure-keys test
npm --prefix examples/js-sdk-fixture-validator run validate
npm --prefix examples/js-sdk-prover-http-client run demo
```

Validation scope:

- audit disclosure key derivation vectors and genesis public key encoding
- fixture address prefixes
- prepared transfer payload hash
- prepared withdraw payload hash
- relayed withdraw final payload hash
- prover HTTP request/response version
- timeout/auth client shape

## 5. Localnet Smoke

```bash
make localnet-smoke
```

This target creates a temporary home and directly runs a validation `clairveild start`. If another local node is already using default Tendermint/RPC ports, it may collide.

Validation scope:

1. build `clairveild`
2. create temporary home
3. run `init`
4. create key
5. add genesis account
6. run gentx / collect-gentxs / validate
7. start node
8. check block commit log

Useful environment variables:

| Env | Meaning |
| --- | --- |
| `CLAIRVEIL_HOME` | fixed home for smoke test |
| `KEEP_HOME=1` | keep home after exit |
| `START_SECONDS` | node runtime duration |
| `CHAIN_ID` | local chain id override |
| `CLAIRVEILD_BIN` | use an already-built daemon |

## 5.1 Local Init Helper

```bash
make init
```

`make init` is a convenience target for preparing a manual local chain. Unlike automatic smoke tests, the default target is the real `~/.clairveil` home.

Behavior:

1. copy binaries to Go install path with `make install`
2. move any existing home to a timestamped backup
3. create `alice`, `bob`, `relayer`, and `auditor` test keys
4. configure genesis accounts, validator gentx, and audit master pubkey
5. generate ZK artifacts and `clairveil.env`

To test without touching the real home:

```bash
tmp="$(mktemp -d)"
GOBIN="$tmp/bin" CLAIRVEIL_HOME="$tmp/home" make init
source "$tmp/home/clairveil.env"
"$tmp/bin/clairveild" start --home "$tmp/home"
```

For strict ZK preflight and proof commands using the same artifacts:

```bash
source ~/.clairveil/clairveil.env
```

## 6. Privacy E2E Smoke

```bash
make privacy-e2e-smoke
```

This target does not attach to an already running `~/.clairveil` node. It creates a temporary work dir, temporary genesis, temporary ZK artifacts, starts a local node, and then runs the CLI flow.

Validation scope:

1. generate ZK artifacts
2. create alice/bob/relayer/auditor keys
3. set genesis audit master pubkey
4. start local node
5. derive shielded address/view key/disclosure key
6. deposit `11`, `10`, `7`, and `0` notes
7. private transfer
8. public user disclosure transfer
9. recipient-encrypted user disclosure transfer
10. mandatory audit disclosure decode
11. direct withdraw
12. prepare/relay withdraw
13. final note state check

Useful environment variables:

| Env | Meaning |
| --- | --- |
| `CLAIRVEIL_E2E_WORK_DIR` | fixed e2e work dir |
| `KEEP_WORK_DIR=1` | keep work dir after exit |
| `CLAIRVEILD_BIN` | use an already-built daemon |
| `CLAIRVEIL_SETUP_BIN` | use an already-built setup binary |
| `CHAIN_ID` | local chain id override |
| `RPC_PORT`, `P2P_PORT`, `GRPC_PORT`, `API_PORT` | avoid port collisions |

If `clairveild start` is already using default ports, run with overrides:

```bash
RPC_PORT=27657 P2P_PORT=27656 GRPC_PORT=9190 API_PORT=1417 make privacy-e2e-smoke
```

Debug example:

```bash
KEEP_WORK_DIR=1 make privacy-e2e-smoke
```

## 7. Tutorial Validation Status

`docs/clairveil-local-privacy-walkthrough.md` is a manual line-by-line tutorial. The same core flow is automatically validated by `scripts/privacy-e2e-smoke.sh`.

Current tutorial criteria:

- uses public clone path `~/clairveil`
- uses tutorial workspace `~/clairveil-privacy-walkthrough`
- uses `keyring-backend test`
- minimizes placeholders; only values such as tx hashes copied from earlier output are placeholders
- includes public disclosure, recipient disclosure, audit disclosure, direct withdraw, and relayed withdraw

If the tutorial changes, run at least:

```bash
make privacy-e2e-smoke
```

If command strings changed heavily, manually follow the walkthrough once in a shell.

## 8. Release Pack Verification

```bash
make release-pack
make release-pack-verify
```

`release-pack-verify` checks:

- external `.sha256` matches archive bytes
- internal `SHA256SUMS.txt` verifies
- required handoff files exist
- default archive manifest commit matches current `HEAD`

## 9. Docker Prover Validation

```bash
make docker-proverd-build
```

Requires Docker daemon. This check is release-critical but not included in the default CI path.

Validation scope:

- compose config
- Dockerfile build
- image inspect

## 10. Documentation-Only Changes

Even for documentation-only changes, run:

```bash
git diff --check
make release-pack-verify
```

If README, release handoff, testing commands, or tutorial commands changed, also run `make ci` or the relevant smoke test.
