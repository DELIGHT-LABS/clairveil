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
| `x/privacy/circuit` | Deposit/Spend/native JoinSplit/production BatchJoinSplit16x32 constraints, shared NoteV1 consistency, positive/negative batch matrix, and the opt-in full-shape resource gate |
| `x/privacy/keeper` | deposit/2x2/batch/withdraw transitions, global commitment uniqueness, deterministic batch gas, atomic rollback, `AssetRegistryV1`, `privacy-scan-v2`, same-root path snapshots, Merkle capacity, and query errors |
| `x/privacy/types` | `MsgBatchTransfer` and structured-output validation, canonical `privacy-fixed-v1` payloads, NoteV1/vector contracts, max-shape wire/state feasibility, addresses, and gateway paths |
| `x/privacy/client/cli` | CLI parsing, output, disclosure decode helpers |
| `x/privacy/client/sdk/*` | identity, deposit, scan, transfer, withdraw, disclosure, prover transport |
| `x/privacy/client/sdk/proverservice` | bounded request handling and per-circuit admission (`1` in flight, `4` queued, positive `8 MiB` body limit by default) |
| `x/privacy/client/sdk/conformance` | JS/web wallet fixture contracts plus independent NoteV1 and batch golden vectors |
| `x/privacy/zk` | consensus identity, public-input schema hashes, role-aware lazy artifact loading, and bounded batch gas/resource formulas |

Focused package examples:

```bash
go test ./x/privacy/circuit
go test ./x/privacy/keeper
go test ./x/privacy/client/sdk/transfer
```

### 3.1 NoteV1 And Session 3A Core Gates

The active circuit set is `privacy-note-v1`, with required order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`. Deposit, spend, native 2x2 JoinSplit, and production BatchJoinSplit16x32 share NoteV1, while canonical plaintext/envelopes use `privacy-fixed-v1`: note plaintext is exactly 350 bytes, disclosure plaintext is exactly 392 bytes, and the typed envelope header is exactly 20 bytes. `AssetRegistryV1` is authoritative for denom/asset-ID mapping. `privacy-scan-v2` uses the global lexicographic cursor `(height, global_sequence, output_index)`, and path tests enforce a single snapshot matching the selected root.

Focused tests for these contracts live at:

- `x/privacy/types/note_v1_test.go` and `x/privacy/circuit/note_v1_consistency_test.go`: domain-separated commitment/nullifier/tree, exact empty roots, canonical keys, and one shared implementation across circuits/scanner.
- `x/privacy/types/fixed_payload_test.go` and `x/privacy/types/batch_contract_test.go`: exact 350/392/20-byte encodings, envelope kinds, reserved bytes, padding, trailing-byte rejection, and canonical `audit_key_id` validation. An audit key ID is 1..64 bytes and matches `[a-z0-9][a-z0-9._-]*`.
- `x/privacy/keeper/asset_registry_test.go`: one-to-one registry, collision/corruption rejection, query bounds, and canonical genesis export.
- `x/privacy/keeper/privacy_scan_test.go` and `x/privacy/keeper/path_snapshot_test.go`: global scan ordering, within-event resume, record/byte bounds, sequence reuse rejection, same-root paths, and fail-closed validation of exact event types, fixed envelope kinds, digests, keys, zero/disabled sentinels, and orphan or non-adjacent outputs.
- `x/privacy/zk/registry_test.go` and `x/privacy/client/sdk/proverservice/admission_test.go`: role-aware lazy artifact access, exact identity behavior, queue bounds, cancellation lifetime, and unbounded-value rejection.
- `x/privacy/client/sdk/conformance/session2_contract_test.go`: independent verification of `privacy_note_v1_contract.json` and `privacy_batch_joinsplit_v1_contract.json`.
- `x/privacy/circuit/batch_joinsplit_16x32_test.go`: production positive shapes, exact sentinel/active-prefix behavior, key/range/root/signature tampering, output/disclosure ordering, and vector type/level separation.
- `x/privacy/keeper/batch_gas_test.go`, `batch_scan_index_test.go`, and `batch_transfer_core_integration_test.go`: deterministic precharge ordering, single-copy typed payload storage/minimal events, global Deposit/2x2/batch sequence, direct proof/state integration, atomic failure, cross-message rollback, and batch scan genesis round-trip.
- `app/ante_batch_transfer_test.go`: signed raw `Any.value` 128 KiB enforcement, duplicate-singular-field decode overwrite, nested governance/authz wrappers, malformed wire data, and the eight-level recursion boundary; keeper gas tests separately account explicit precharge and Cosmos KV descriptors on real `1/1` and max `16/32` state paths.
- `x/privacy/genesis_test.go`, `x/privacy/keeper/path_snapshot_test.go`: circuit identity is checked before writes, every imported historical root is recomputed, and exported per-prefix root snapshots remain queryable after restore.
- `x/privacy/zk/development_artifact_gate_test.go`: fourth-descriptor schema identity plus validator VK-only and prover R1CS/PK-only readiness against generated development artifacts.

The production `BatchJoinSplit16x32` public inputs are consensus-visible in this exact order: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. Its schema SHA-256 is `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`. `MsgBatchTransfer`, `BatchTransferOutput`, the keeper handler, deterministic gas precharge, atomic state transition, typed scan state, minimal event, genesis round-trip, and role-aware artifact identity are implemented.

Run the normal production circuit and direct core matrices with:

```bash
go test ./x/privacy/circuit -run 'TestBatchJoinSplit16x32(ProductionPositiveMatrix|SelfViewIsPayloadOnly|ProductionNegativeMatrix)' -count=1
go test ./x/privacy/keeper -run 'Test(BatchTransferDirectCoreIntegration|BatchTransferCoreRejectionsAndAtomicScanFailure|CrossMessageNullifierFailureRollsBackWholeCosmosTxCache|BatchGasPrechargeV1MetersEveryFrozenCategory|BatchScanIndexStoresPayloadOnceAndEmitsMinimalSummary|DepositJoinSplitAndBatchShareGlobalPrivacySequence|BatchScanGenesisRoundTripPreservesCursorLeafAndSequence|MerkleRootSnapshotGenesisExportCoversEveryPrefix)' -count=1
go test ./x/privacy -run 'Test(GenesisRoundTrip|InitGenesisPanicsWithForgedHistoricalRoot|InitGenesisRejectsCircuitIdentityMismatchBeforeStateWrites)' -count=1
```

Same-root path behavior has separate online and offline resource boundaries. Current-root requests read persisted incremental tree nodes and do not consume the online historical-rebuild budget. A missing cached current root fails with `FailedPrecondition` and never triggers an online rebuild or state write; repair it through the explicit offline path. Every non-current historical-root request requires persisted `(root, leaf_count, height)` metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Above the online bound, use the current root or a trusted local historical-path index. Offline recovery/export keeps the separate `MaxMerkleRebuildLeaves` (1,048,576) bound. Genesis export remains possible above the offline bound when the complete per-prefix snapshot metadata index was persisted; export does not need to rebuild all historical nodes in that case.

Run the always-on max-shape protobuf/Tx/KV/event/query wire-state gate with:

```bash
go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v
```

The corrected max-shape goldens are: canonical owner-effect payload `65,384` bytes, Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, and query response `74,551` bytes.

Run the expensive full-shape circuit setup/prove/resource gate explicitly with:

```bash
CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v
```

The full gate is opt-in because it compiles the 16x32 circuit, performs a development Groth16 setup, and proves multiple shapes while reporting constraints, artifact sizes, proving/verification timings, and resource measurements. The corrected reference run measured `1,111,837` constraints, peak RSS `3,339,862,016` bytes, max-shape warm proving cost `55.892 ms/output`, and `2.789x` per-output improvement over the current native 2x2 baseline. It is a feasibility measurement, not a production artifact generation or trusted setup command.

Generate and validate the actual development artifact set separately:

```bash
ARTIFACT_DIR=/tmp/clairveil-session3a-artifacts
/usr/bin/time -l go run ./cmd/clairveil-setup --out "$ARTIFACT_DIR"
CLAIRVEIL_RUN_BATCH_DEVELOPMENT_ARTIFACT_GATE=1 \
CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR="$ARTIFACT_DIR" \
go test ./x/privacy/zk -run TestBatchDevelopmentArtifactRoleReadinessGate -count=1 -v
```

The recorded batch files are R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, and VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`. Generation peak RSS was `3,308,797,952 B`; the role-readiness run peaked at `1,295,482,880 B`. These are development identities, not formal trusted-setup or production-distribution artifacts.

Session 3B remains excluded: there is no public `MsgBatchTransfer` Go SDK/builder, batch HTTP route in the reference prover, wallet scanner/decrypt UX, one-proof batch payroll integration, or batch CLI/tutorial. Existing `transfer-batch` and payroll tests exercise their existing multi-message workflow, not the new one-proof message.

Prepared transfer payload `v5` remains the current outer prepared-payload contract. Do not confuse that version with the inner note/disclosure encoding: inner canonical payloads and envelopes are `privacy-fixed-v1`. Compatibility fallback is forbidden. The external ClairveilJS package is still legacy at this handoff point; it must fail closed on the new fixed fixtures until it is upgraded, rather than interpreting them through its old decoder.

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
