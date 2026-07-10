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
| `make build` | build `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`, and benchmark/load tools (`clairveil-benchreport`, `clairveil-proverload`, `clairveil-localnetload`, `clairveil-userlatency`, `clairveil-bulktransferbench`) |
| `make install` | run `make build`, then copy Clairveil binaries to `GOBIN` or `GOPATH/bin` |
| `make init` | run `make install`, then initialize the default local chain home for `clairveild start` |
| `make proto` | regenerate privacy protobuf/gateway Go files |
| `make examples` | run JS audit key, fixture validator, prover HTTP client, and browser DApp examples |
| `make ci` | `test`, `build`, and `examples` |
| `make vulncheck` | run govulncheck policy gate |
| `make localnet-smoke` | briefly verify that the reference daemon can start from genesis |
| `make privacy-e2e-smoke` | validate full deposit, transfer, disclosure, and withdraw flow |
| `make reference-payroll-demo` | validate the reference payroll product flow: validate, prepare, plan, reserve, simulated daemon, final report |
| `make reference-payroll-live-localnet` | validate the live localnet payroll flow: payroll input, reservation, transfer-batch, recipient scan, settle, final report |
| `make reference-payroll-rehearsal` | generate reference payroll capacity simulations and optional live localnet smoke |
| `make dapp-local` | start a local Clairveil node, prover, and browser DApp stack for manual testing |
| `make release-check` | `ci`, `vulncheck`, `localnet-smoke`, `privacy-e2e-smoke`, and bulk readiness with localnet transfer-batch smoke |
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
npm --prefix examples/clairveil-dapp ci
npm --prefix examples/clairveil-dapp run check:dapp
npm --prefix examples/clairveil-dapp run test:dapp
npm --prefix examples/clairveil-dapp run check:clairveiljs
npm --prefix examples/clairveil-dapp run test:clairveiljs
```

Validation scope:

- audit disclosure key derivation vectors and genesis public key encoding
- fixture address prefixes
- prepared transfer payload `v5` hash, including chain/expiry, disclosure blindings, owner signature, and `view_tag_hexes`
- sender self-view disclosure digest/payload fields
- prepared withdraw payload hash
- relayed withdraw final payload hash
- relay withdraw handoff relayer `creator` / payload `recipient` mapping
- `scan_events` request/response fixture shape, cursor fields, scan/view tag versions, and projection outputs
- batch `check_nullifiers` request/response fixture shape
- prover HTTP request/response version
- timeout/auth client shape
- browser DApp boundary checks, static bundle freshness, local helper route policy, and ClairveilJS package surface smoke tests

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
- includes public disclosure, recipient disclosure, sender self-view disclosure, audit disclosure, direct withdraw, and relayed withdraw

If the tutorial changes, run at least:

```bash
make privacy-e2e-smoke
```

If command strings changed heavily, manually follow the walkthrough once in a shell.

## 7.1 Reference Payroll Demo

```bash
make reference-payroll-demo
```

This target validates the reference payroll product flow using repo-local files only. It does not start a local chain.

Validation scope:

1. sample payroll input validation
2. note preparation analysis
3. payroll plan creation
4. plan confirmation into durable reservation state
5. one `clairveil-payrolld -once` simulated scheduler tick
6. reservation/operation status reload
7. final payroll report export

The default outputs are written under `tmp/reference-payroll-demo/`. A successful run has all reservations in `ConfirmedSpent`, all operations in `Succeeded`, and the final payroll report status in `Confirmed`.

## 7.2 Reference Payroll Live Localnet

```bash
make reference-payroll-live-localnet
```

This target starts a real localnet and connects the reference payroll product to an actual `transfer-batch` tx.

Validation scope:

1. localnet init/start
2. treasury shielded note deposits
3. payroll input generation from `list-notes --json`
4. payroll validate, prepare, plan, reserve
5. real `clairveild tx privacy transfer-batch` broadcast
6. recipient note scan delta check
7. `clairveil-payroll settle-transfer-batch`
8. final payroll report export

The manual walkthrough is [clairveil-reference-payroll-live-localnet-tutorial.md](clairveil-reference-payroll-live-localnet-tutorial.md).

## 7.3 Reference Payroll Rehearsal

```bash
make reference-payroll-rehearsal
```

This target generates payroll capacity simulation reports for 1k, 10k, 100k, and 100 companies x 1k profiles. To include a small live localnet smoke, run `RUN_LOCALNET=1 LOCALNET_PAYROLL_ITEM_COUNT=2 make reference-payroll-rehearsal`.

The repo-local 1k restart/retry rehearsal uses the actual localnet transfer path while seeding localnet-only notes to avoid deposit preparation time:

```bash
PAYROLL_SEED_NOTES=1 PAYROLL_ITEM_COUNT=1000 PAYROLL_CHUNK_SIZE=20 GAS_PRICES=0uclair make reference-payroll-live-localnet
```

The Korean rehearsal guide is [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md), and the recorded localnet result is [clairveil-reference-payroll-localnet-rehearsal-result-kr.md](clairveil-reference-payroll-localnet-rehearsal-result-kr.md).

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
