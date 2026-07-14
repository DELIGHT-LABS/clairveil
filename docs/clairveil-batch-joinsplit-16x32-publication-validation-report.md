# Batch JoinSplit 16x32 Independent Publication Validation Report

## 2026-07-13 Publication Validation Closure — Current Authoritative Record

| Item | Result |
| --- | --- |
| Security-hardening baseline commit | `e42737022b2aa87b498f57ae4d089ccb84a45968` |
| Publication-validation starting snapshot | `0df27417910f46ff714e73ce0730f5e167ece33a` |
| Validated immutable implementation snapshot | `a84ca1cc1cd835990243d9b3f5f064e7b538f7ae` |
| Final review range | `e42737022b2aa87b498f57ae4d089ccb84a45968..HEAD`; the starting snapshot was not changed |
| Validation state | Security, protocol, chain-core, and client-integration gates **PASS**; publication-validation Passes A–I **PASS** |
| Finding state | Unresolved Critical `0`, High `0`, security-relevant Medium `0` |
| Publication | **`PUBLICATION_READY_EXPERIMENTAL`** |
| Production | **`PRODUCTION_RELEASE_READY` not approved** |

This is the current authoritative record. All 2026-07-12 and earlier publication-ready, client-integration gate FAIL/PASS, and independent publication validation `BLOCKED` text below is retained only as historical provenance and is not current evidence. The initial range stayed read-only through one two-reviewer wave split between Passes A–E and Passes F–I/publication hygiene. The main reviewer adjudicated every candidate directly against code and tests. No repeated reviewer wave or clean-round loop was used.

### Finding closure

| ID | Severity | Independent decision and remediation | State |
| --- | --- | --- | --- |
| `PROVER-FAILOVER-LIVE-EVIDENCE` | Medium, security-relevant | Real timeout and healthy HTTP prover endpoints expose server-side request counters and received-body observations. Default no-failover is `1/0`; explicit opt-in is `1/1` and completes from the healthy response. Timeout, malformed response, and validation failure are separate regressions. No body, witness, nullifier, path, or amount is printed. Commit `1b1dc08` | **RESOLVED** |
| `NONCANONICAL-FIELD-ALIAS` | Medium, security-relevant | `r` and `r+q` have the same BN254 residue and commitment but bypassed raw decimal secret comparison. The structured batch-signing boundary now compares canonical field bytes and rejects non-canonical aliases before signature release. Commit `cdc7780` | **RESOLVED** |
| `LOCALNET-PROCESS-CLEANUP` | Low, operational | The reference payroll localnet backgrounded a shell function and cleaned up its subshell PID, leaving the node orphaned. It now backgrounds the node directly; the rerun left zero node/prover processes. Commit `a84ca1c` | **RESOLVED** |
| `PUBLICATION-DOC-CONSISTENCY` | Medium, publication hygiene | Current-state client-integration gate FAIL, active `PROVER-FAILOVER-LIVE-EVIDENCE`, and independent publication validation `BLOCKED` language remained in bilingual normative/handoff documents. Historical records were retained and current authoritative text was aligned in this publication record. | **RESOLVED** |

### Fresh Pass A–I results

| Pass | Independent validation | Result |
| --- | --- | --- |
| A | Current authorization/intent, disclosure dictionary, commitment collision, crypto decoder, privacy defaults, genesis/artifact identity | **PASS** |
| B | NoteV1 consistency across native, Deposit, Spend, 2x2, Batch, scanner, genesis, and independent KATs | **PASS** |
| C | Independent reconstruction of all 12 Batch public inputs and circuit/native/keeper/SDK/prover witness ordering | **PASS** |
| D | Active prefix, disabled sentinel, duplicate, value/root/path/owner/signature/disclosure adversarial witnesses | **PASS** |
| E | Inputs `1..16`, outputs `1..32`, differential/property tests, independent vector/payload KATs, bounded fuzz | **PASS** |
| F | Keeper framing/gas/precharge, consensus identity, atomic rollback, cross-message composition, resource bounds | **PASS** |
| G | Minimal event, single-copy typed scan plane, sequence/cursor/restart/genesis/item evidence | **PASS** |
| H | Role-aware artifact admission, body/queue/permit bounds, payload-free errors, live no-failover, safe view-tag default | **PASS** |
| I | One-proof payroll graph/worker/broadcast/reconcile and separate batch/item evidence | **PASS** |

Every targeted command produced the expected named `--- PASS` markers. Neither `SKIP` nor `[no tests to run]` was accepted. A first PostgreSQL skip was rejected; Docker PostgreSQL 17 was provisioned and the actual integration then passed.

### `PROVER-FAILOVER-LIVE-EVIDENCE` live evidence

| Scenario | Timeout contacts | Healthy contacts | Observation |
| --- | ---: | ---: | --- |
| Default no-failover | 1 | 0 | Timeout body observed server-side; no healthy body |
| Explicit opt-in failover | 1 | 1 | Both bodies equal; completion from the healthy response |

Timeout, malformed response, and response-validation failure are distinct error classes, and each failure endpoint is contacted once. `automatic_multi_prover_failover=false` is derived from these actual counters rather than emitted as a literal. Evidence contains only counts and booleans; it never records the private request body.

### Benchmark and resource evidence

The environment was Apple M5 Pro/64 GiB, Darwin arm64, Go `1.25.12`, gnark `v0.14.0`, and Groth16/BN254. `TestBatchJoinSplit16x32FullShapeResourceGate` passed in 61.62 seconds with process peak RSS `3,354,689,536 B`.

| Item | Value |
| --- | ---: |
| Constraints | `1,111,837` |
| Compile / setup | `1002.828 ms` / `16106.399 ms` |
| R1CS / PK / VK / proof | `122,813,535 B` / `209,218,621 B` / `716 B` / `164 B` |
| `16/32` witness p50/p95/max | `0.401/0.418/0.418 ms` |
| `16/32` prove p50/p95/max | `1685.541/1712.069/1712.069 ms` |
| `16/32` verify p50/p95/max | `0.673/0.686/0.686 ms` |
| JoinSplit2x2 warm-prove mean | `152.11075 ms` |

### Integration, fuzz, and release evidence

- `make reservation-sql-integration`: actual SQLite and Docker PostgreSQL 17 graph-atomicity/recovery tests passed with exact markers and zero skips.
- `make validate-joinsplit-artifact-rotation-evidence`: source-regenerated current/previous artifact rotation/tamper, old/new proof identity, and fresh-genesis identity passed.
- All ten actual fuzz targets ran separately with `-fuzztime=3s -parallel=4` and passed: note/disclosure plaintext, Batch/2x2 payloads, vector root, prover JSON, scan page, EC point, EdDSA signature, and ECIES envelope.
- `make privacy-e2e-smoke`: Deposit/disclosure/2x2/Withdraw passed.
- `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`: five shapes, one-proof/one-envelope payroll, node/prover restart, timeout/exact retry, tx-hash-first reconciliation, spent-nullifier rejection, disclosure/view-tag safe scan, genesis export/import, and post-import continuation passed. Gas was `1,609,514` for `1/1`, `3,141,099` for `3/4`, `16,017,355` for `31+change`, `16,876,619` for exact `32`, and `15,529,326` for padding.
- `make reference-payroll-live-localnet`: one chunk/two items, two confirmed items, four confirmed spent reservations, final `Confirmed`, and zero node/prover processes after exit.
- `go test ./... -count=1`, `go test -race ./x/privacy/... -count=1`, `go vet ./...`, `make ci`, `make vulncheck`, `make examples`, `make release-check`, and `git diff --check` passed.
- No tracked secret, generated R1CS/PK/VK/archive, `dist/`/`benchmarks`/`tmp`, or personal absolute path exists. Public inputs, NoteV1, payload/schema/version, and the circuit statement did not change.
- `make release-pack` and `make release-pack-verify` run on the final clean immutable commit, and the same archive is verified with Python 3.9 and 3.12. The pack has 125 required files. `RELEASE-MANIFEST.txt` records the exact source commit and the co-located `.sha256` records the archive digest; no self-referential archive checksum or commit placeholder is embedded.

### Accepted residuals and Production TODO

| Residual/TODO | Owner | Reason accepted now | Production blocking |
| --- | --- | --- | --- |
| External ZK audit, final source/constraint freeze, official MPC/trusted setup, transcript/toxic-waste evidence | Protocol/release owner | Explicit non-goals of the experimental source gate; development setup only | Yes |
| Signed production artifact/provenance/custody and production manifest/SBOM/image provenance | Release/validator operator | Requires target release infrastructure and signing policy | Yes |
| Production gas governance/calibration, upgrade/rollback, staging load/fault, monitoring/incident response | Downstream chain owner | Target-chain operations scope | Yes |
| Prover TLS/auth/ACL/quota/process isolation/retention and audit-key custody/rotation/manual review | Prover and auditor/payroll operators | Requires managed production infrastructure and procedures | Yes |
| Downstream JS/TS wallet/product plus metadata leakage and padding policy | Product/privacy owner | Only the Go reference is publication-approved; product acceptance is separate | Yes for downstream production |
| Three no-fixed-version Go advisories and one example npm Low | Dependency/security owner | Tracked under the existing exact policy rather than hidden | Reassess before production |

These items do not convert a security finding into a residual. `PRODUCTION_RELEASE_READY` remains unapproved until the external audit/freeze/MPC/transcript/signed-provenance/downstream-rollout work is complete.

## Historical status and prior validation records (superseded)

Every current/blocked/finding statement below describes its historical point in time. Only the 2026-07-13 independent publication validation Closure above determines current status.

| Item | Result |
| --- | --- |
| Review range | `e427370..d45f0753c16571743f630599776c9cd498d1e8c9` |
| Starting HEAD reviewed | `d45f0753c16571743f630599776c9cd498d1e8c9` |
| Review role | Fresh reviewer independent of the security-hardening, protocol, chain-core, and client-integration implementation work |
| Client-integration gate on entry | **FAIL — client implementation/test re-entry required** |
| `DISCLOSURE-BLINDING-SEPARATION` batch chain core remediation | **RESOLVED — fresh security, protocol, and chain-core gates closure PASS; batch reference integration re-entry UNBLOCKED** |
| independent publication validation publication state | **`BLOCKED`** (`PUBLICATION_READY_EXPERIMENTAL` withdrawn) |
| Production release state | Not approved |
| Formal trusted setup | Not performed |
| External audit | Not performed |

This 2026-07-12 revalidation supersedes the earlier publication claim completed on the same day and retained below. The 2026-07-13 closure passes fresh security, protocol, and chain-core gates and unblocks batch reference integration re-entry, but client-integration gate is not satisfied and unresolved High and security-relevant Medium findings remain. Independent publication validation Passes A–I were not resumed, so experimental source-publication approval remains `BLOCKED`.

### 2026-07-13 security, protocol, and chain-core gates closure supplement

- Scope `6aa341e..HEAD` plus the working tree was reviewed with two fresh pinned read-only reviewers per round under `mode=deep`, `verify=max`, and `scope=release`; active P0/P1/P2 and unresolved candidates reached zero.
- `ARTIFACT-BYTE-IDENTITY` verifies actual SHA-256 for every current/previous required artifact, exact non-JoinSplit byte identity, canonical descriptors, missing/duplicate/unknown/tamper failures, a positive JoinSplit-only rotation, and exact current-source R1CS serialization identity. The regression is part of `make validate-joinsplit-artifact-rotation-evidence`, which can regenerate both sets from pinned prior/current source on a clean commit.
- The structured 2x2 signer independently binds both input nullifiers, both output NoteV1 commitments, value conservation, change ownership, and user/audit disclosure preimages to the final effect before release. Public inputs, NoteV1, wire payload/schema/version, and the production circuit contract are unchanged.
- Targeted DISCLOSURE-BLINDING-SEPARATION and DUPLICATE-INFLATION-REGRESSION, artifact/proof/genesis evidence, all privacy/repository tests, vet/build/examples, JoinSplit/Batch resource gates, release-check, diff, bilingual-document, and secret/artifact hygiene checks passed. The exact release pack is generated and verified on Python 3.9/3.12 after the closure commit.
- **Security, protocol, and chain-core gates PASS; batch reference integration re-entry is UNBLOCKED.** The client-integration gate, independent publication validation, and publication remain **BLOCKED** by `ONE-PROOF-PAYROLL-E2E`, `LIVE-DISCLOSURE-VERIFICATION`, `SQL-GRAPH-ATOMICITY`, `BATCH-SIGNER-SECRET-FRESHNESS`, and `PROVER-FAILOVER-LIVE-EVIDENCE`.

### Disclosure-Blinding Separation Chain-Core Implementation Supplement

- The implementation starts from clean HEAD `0fc818c`; commits `0b7d97d`, `630736f`, and `25c17ef` use the latest Master Ledger and batch protocol contract re-entry record as authoritative.
- Production output 0 now enforces `DBS-01..03` and the canonical all-private sentinel/gating. The shared native/prepared validator and structured 2x2 pre-sign boundary use the same contract.
- The production count is `99,775`, exactly `+10` over the `99,765` control and equal to the frozen target, so there is no decision change. The 13 public inputs/schema hash, NoteV1, payload `v5`, proof/HTTP `v2`, disclosure digest/domain, and circuit-set ID remain unchanged.
- New development JoinSplit SHA-256 values are R1CS `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, PK `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, and VK/consensus identity `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`. This is a JoinSplit-only development rotation; Batch and the other artifacts are unchanged.
- Old/new proof and consensus/file mismatch, fresh genesis/reset, strict artifact preflight, the complete 2x2 regression, and the full unchanged Batch `1,111,837`-constraint resource comparison pass. No formal trusted setup was performed and no generated binary or secret is tracked.
- `DISCLOSURE-BLINDING-SEPARATION` implementation is **RESOLVED**. The later 2026-07-13 supplement passes fresh security, protocol, and chain-core gates and unblocks batch reference integration re-entry. Current unresolved counts remain Critical 0, High 2, and security-relevant Medium 3, and independent publication validation was not resumed.

### Historical `DISCLOSURE-BLINDING-SEPARATION` foundation re-entry supplement

- The re-entry started from clean HEAD `42d40bd19523e263aaf1c2043bcd274a4fc1a51d` and treated the latest Master Ledger plus this `BLOCKED` record as authoritative.
- Commits `c7fc1be`, `a8697cd`, `a4ee959`, and `4e75f1f` freeze `DISCLOSURE-BLINDING-SEPARATION` V1 (`DBS-01..03`), exact all-private/disabled gating, the shared `DBS_*` error/layer contract, conformance fixtures, and the 2x2 feasibility target.
- The test-only hardened circuit has `99,775` constraints versus current `99,765` (`+10`), R1CS `+253 B`, PK `+912 B`, unchanged VK/proof sizes, peak RSS `690,438,144 B`, and no OOM. Batch source/artifacts remain unchanged at `1,111,837` constraints.
- Public inputs, NoteV1, payload encoding, disclosure digests, and circuit-set version do not change. The batch chain core replaces only the JoinSplit R1CS/PK/VK plus manifest/consensus JoinSplit identity.
- Batch chain-core re-entry is **UNBLOCKED / NOT STARTED**. `DISCLOSURE-BLINDING-SEPARATION` remains **IMPLEMENTATION PENDING / NOT RESOLVED** until production constraints/artifacts, pre-sign enforcement, regression, readiness, and resource gates are complete.
- `DUPLICATE-INFLATION-REGRESSION` is **RESOLVED** by `02f61f3746b67d5244c160b7c0e0e42f7c0b78b8` and `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`.

## Historical — confirmed findings at the 2026-07-12 revalidation

| ID | Severity | Evidence | Impact | Required action |
| --- | --- | --- | --- | --- |
| ONE-PROOF-PAYROLL-E2E | High | The batch localnet runs one-proof transfer shapes but never the payroll operation graph/workers/reconcile/report. The reference payroll localnet is the legacy multi-message 2x2 `transfer-batch` path. | One-proof payroll reservation, proving, signed-byte retry, typed item evidence, reconciliation, and reporting have never been connected on a real chain. | Add a fresh batch reference integration localnet E2E that uses the complete production payroll-worker path and covers restart/retry. |
| LIVE-DISCLOSURE-VERIFICATION | High | The localnet only constructs mixed-disclosure/self-view options. It does not assert recipient/auditor/self-view decryption, blinding-based digest recomputation, the complete expected output count/commitment set, or safe scanning after a view-tag mismatch. | The runner can succeed while the typed scanner or disclosure consumer omits outputs. | Add live disclosure consumers, view-tag mismatch injection, and per-output evidence checks. |
| SQL-GRAPH-ATOMICITY | Medium, security-relevant | `SQLStore` exists, but tests cover schema strings, placeholders, and isolation options only. No real SQLite/PostgreSQL CRUD, rollback, reopen, or lease/CAS execution exists. | Reservation-operation-item-evidence atomicity is unproven on SQL backends, leaving orphan, duplicate-work, and incorrect-item-status risk. | Add at least real SQLite transaction, restart, rollback, and concurrency tests. |
| BATCH-SIGNER-SECRET-FRESHNESS | Medium, security-relevant | Unlike the final prepared validator, `ValidateBatchTransferSigningRequest` does not reject global input/output or cross-output secret reuse before signing. | An untrusted preparer can obtain an owner signature over a privacy-leaking intent. | Apply the same `seenSecrets` checks at the structured signer boundary and add adversarial signing tests. |
| PROVER-FAILOVER-LIVE-EVIDENCE | Medium, security-relevant | Unit tests cover default no-failover and explicit opt-in, but the localnet does not measure actual timeout/healthy endpoint contact counts. Its result value is a literal, not an observation. | The publication evidence does not prove that a live transport avoids sending the witness to a second prover by default. | Exercise two live endpoints and verify default and opt-in behavior separately. |

Resolved supplements: `DUPLICATE-INFLATION-REGRESSION` is closed by `02f61f3`/`42d40bd`, and `DISCLOSURE-BLINDING-SEPARATION` implementation is closed by `0b7d97d`/`630736f`/`25c17ef`; neither is included in the current finding count.

## Historical — verification disposition at the 2026-07-12 revalidation

- Because client-integration gate failed, independent publication validation Passes A–I, a fresh max-shape benchmark, fresh localnet, full regression/race/fuzz, and release gates were **not run**. Historical results below are not re-approved as current gate evidence.
- As supporting checks only, `TestPrivacyNoteV1ContractIndependentGolden` and `TestPrivacyBatchJoinSplitV1ContractIndependentGolden` passed. Their source independently calculates frozen domains, encodings, MiMC, and vector formulas without using the production NoteV1/root helper in the calculation path.
- Payroll default no-failover/explicit opt-in, durable reconciliation, prove-permit lifetime, and memory/file-store tests passed. The SQL check is schema-only and does not close SQL-GRAPH-ATOMICITY.
- Batch artifacts in `/tmp/clairveil-joinsplit-artifacts-381c984` match the historical sizes (R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`) and SHA-256 values.
- No tracked R1CS/PK/VK, `dist/`, `benchmarks/`, `tmp/`, personal absolute path, or evident secret was found. `benchmarks/`, `dist/`, and `tmp/` are ignored.
- Current unresolved counts are Critical 0, High 2, and security-relevant Medium 3. No security finding was converted into an accepted residual.
- Formal setup, external audit, production artifact/provenance, and downstream production operations remain unperformed Production TODOs and do not replace the active findings.

### Supporting verification commands run

The `ARTIFACT-GATE-EXECUTION` review found that the previous artifact and fresh-genesis commands used opt-in variables and test names that did not match the code, allowing `SKIP` or `[no tests to run]` to look successful. The table below retires those two rows and replaces them with fail-closed `make validate-joinsplit-artifact-rotation-evidence`. That evidence correction alone did not pass fresh security, protocol, and chain-core gates; the later 2026-07-13 closure supplement does.

| Command | Result and limitation |
| --- | --- |
| `go test ./x/privacy/client/sdk/conformance -run '^(TestPrivacyNoteV1ContractIndependentGolden\|TestPrivacyBatchJoinSplitV1ContractIndependentGolden)$' -count=1 -v` | PASS. Confirms the independent golden calculation path; does not replace client-integration gate |
| `go test ./x/privacy/client/sdk/payroll -run '^(TestProverPoolDoesNotFailOverAfterEndpointTimeoutByDefault\|TestProverPoolFallsBackAfterEndpointTimeoutWithExplicitOptIn\|TestBatchReconcileDurableRestartRetryTxHashFirstAndItemEvidenceSeparate\|TestBatchProofWorkerKeepsSharedLeaseUntilUninterruptibleProveReturns)$' -count=1 -v` | PASS. Unit boundary only; not live endpoint evidence |
| `go test ./x/privacy/client/sdk/reservation -run '^(TestBatchOperationGraphIsAtomicAndConflictsWithOrdinaryReservation\|TestBatchOperationDurableFileRestartRoundTrip\|TestBatchOperationSQLSchemaIsVersionedAndRelational)$' -count=1 -v` | PASS. Not a real SQL transaction test, so SQL-GRAPH-ATOMICITY remains open |
| `go test ./x/privacy/types ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/conformance -run 'DisclosureBlinding|AllPrivateUserBlinding' -count=1 -v` | PASS. Confirms the shared native/prepared/fixture contract; does not replace production circuit enforcement |
| `CLAIRVEIL_RUN_JOINSPLIT_BLINDING_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestJoinSplitDisclosureBlindingSeparationResourceGate$' -count=1 -v` | PASS. Legacy control `99,765`, production `99,775`; production R1CS `10,824,169 B`, PK `16,766,489 B`, VK `748 B`, proof `164 B`; peak RSS `687,423,488 B` |
| `make validate-joinsplit-artifact-rotation-evidence` (clean commit regeneration), or both artifact directory variables for supplied sets | PASS. Runs `TestJoinSplitArtifactRotationSnapshotValidation`, `TestJoinSplitDevelopmentArtifactRotationGate`, `TestJoinSplitOldAndNewProofIdentitiesAreMutuallyExclusive`, and `TestFreshGenesisUsesRotatedJoinSplitIdentity`, covering fail-closed descriptor/tamper regression, actual artifact readiness, exact current-source R1CS serialization identity, mutual old/new proof-VK rejection, and old-identity rejection before fresh-genesis state writes. The wrapper requires exact execution and rejects `SKIP` or `[no tests to run]` |
| `CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestBatchJoinSplit16x32FullShapeResourceGate$' -count=1 -v` | PASS. Batch unchanged at `1,111,837` constraints; R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`; peak RSS `3,324,461,056 B`, no OOM |
| `git merge-base --is-ancestor e427370 HEAD`, `git diff --check e427370..HEAD` | PASS at starting HEAD `d45f0753c16571743f630599776c9cd498d1e8c9` |
| Artifact `shasum -a 256` and file-size comparison | PASS. Batch R1CS/PK/VK match the historical development hashes and sizes |
| Tracked artifact/personal-path/secret-filename scan | PASS. `benchmarks/`, `dist/`, `tmp/`, `tmpdocs/`, local binaries, and dependency outputs are ignored/untracked and are not publication evidence |
| `go test ./... -count=1`; `go vet ./x/privacy/...`; `make build`; `make examples`; `make vulncheck`; `git diff --check` | PASS. Final repository/release-static verification for the batch chain core implementation scope; not a resumption of independent publication validation Passes A–I or live E2E |
| `make release-pack`; `make release-pack-verify` | PASS at clean closure `354509db54f193295d1e1a18f9e4b45de3741d4f`. Verified 125 required files and the exact manifest commit; rerun after the final bookkeeping commit |
| Passes A–I, race/fuzz, benchmark, fresh localnet/live E2E, and complete `make release-check` | **NOT RUN — client-integration gate FAIL and outside batch chain core scope** |

### Accepted residuals and Production TODO

No active High or Medium finding was accepted as a residual. Only the following operational items remain prior Production TODOs.

| Residual/TODO | Owner | Current reason accepted | Production blocking |
| --- | --- | --- | --- |
| External ZK audit, source/constraint freeze, official MPC/trusted setup, and transcript | Protocol/release owner | Explicitly outside independent publication validation; development artifacts only | Yes |
| Artifact signing/provenance/custody, production manifest, and SBOM/image provenance | Release/validator operator | Requires target release infrastructure | Yes |
| Production gas/governance/upgrade/rollback, staging load/fault, monitoring, and incident response | Downstream chain owner | Requires the target chain and operations process | Yes |
| Prover TLS/auth/ACL/quota/process isolation/retention and audit-key custody/rotation/manual review | Prover and auditor/payroll operators | Requires managed production infrastructure and procedures | Yes |
| Downstream JS/TS wallet/product plus metadata-leakage and padding policy | Product/privacy owner | Only the Go reference and declared leakage exist; product acceptance is required | Yes for downstream production |
| Three no-fixed-version Go advisories and one example npm Low | Dependency/security owner | Tracked under the existing exact policy rather than hidden | Reassess before production |

This table neither accepts the active client-integration and batch protocol/chain-core blockers nor authorizes publication.

## Historical — prior 2026-07-12 validation record (superseded)

The content below retains the prior reviewer's historical claim for provenance. It is not the current publication state, Pass result, benchmark, or localnet evidence; the 2026-07-12 disposition above controls.

## Historical — independent review method

The reviewer read the master roadmap and every security-hardening baseline–4 plan and Completion Record from beginning to end, reconstructed the protocol from code, and then compared the implementation to the normative contracts. No file was modified until the initial findings had been reproduced and adjudicated. client-integration gate was checked before Passes A–I. Because its clean-worktree and integration-evidence requirements were not initially met, independent publication validation was not treated as passed while that gate was open.

The review reconstructed:

- NoteV1 domains, field order, empty-tree roots, vector roots, active prefixes, and disabled sentinels;
- the 12 public inputs and native, circuit, keeper, SDK, and prover witness encodings;
- owner authorization, membership, distinctness, conservation, disclosure, and payload binding;
- keeper gas, resource, atomicity, cross-message composition, scan/genesis state, prover privacy, and payroll evidence boundaries.

## Historical — findings and fixes

| ID | Severity | Evidence | Impact | Resolution |
| --- | --- | --- | --- | --- |
| BATCH-PREPARATION-REVALIDATION | High | Batch preparation and payload construction trusted mutable plan totals, ordering, ownership, disclosure modes, and field values without a complete independent recomputation. | A tampered prepared object could cross the signing/prover boundary with semantics different from the planner's intent, or fail late after sensitive work. | Closed by `d9b1780`: recompute counts, ownership, asset, nullifier uniqueness, output roles, conservation, disclosure policy, canonical fields, and use durable private-file replacement. |
| PROVER-TRANSPORT-SECURITY | High | Remote prover transport allowed non-loopback cleartext HTTP and ordinary redirect following; authentication was not available in the client contract. | A private witness could be sent in cleartext or redirected to an unintended endpoint. | Closed by `8dfe80b`: HTTPS except loopback, redirect denial, opt-in redirect-safe custom doers, bearer-token support, bounded responses, and atomic private-file writes. |
| LOSSLESS-TYPED-PAGINATION | High | Typed scan page validation did not prove cursor advances across zero-output summaries and the scanner requested a filtered event set. | Pagination could reject a valid boundary or conceal a skipped output-bearing event, breaking lossless wallet/payroll recovery. | Closed by `868f108`: unfiltered typed scans, ordered summary proof for cursor advances, no skipped output event, stable nil cursor handling, and no lossy ABCI fallback. |
| DURABLE-PAYROLL-ATOMICITY | High | Durable payroll reservation, operation, item, lease, broadcast, and reconcile transitions were not consistently atomic or evidence-bound across memory, file, and SQL stores. | Crash/retry races could orphan reservations, duplicate work, or mark item outcomes without the required batch-output evidence. | Closed by `0b6b3ee`: atomic operation graph persistence, lease/CAS checks, evidence-bound reconciliation, and cross-store regression coverage. |
| BATCH-LOCALNET-GATE | Medium | The initial batch localnet rehearsal did not enforce all negative outcomes, artifact identity, process cleanup, or repository hygiene checks. | client-integration gate evidence could report success without proving the advertised integration boundary. | Closed by `d7809e9` and client-integration gate was re-recorded by `423f73a` only after targeted, full privacy, race, vet, examples, E2E, payroll, artifact, and release checks passed. |
| CONFIRMED-FAILURE-RESIGN | High | A confirmed failed payroll transaction had no explicit store transition that permitted a new-sequence re-sign while preserving the operation graph. | A recoverable chain failure became a durable dead end; operators could not safely retry without manual state surgery. | Closed by `16900cb`: `PrepareBatchOperationResign` is lease-bound, requires confirmed failure, rechecks nullifiers, and only proceeds with explicit `ResignWithNewSequence`. |
| PROPERTY-FUZZ-COVERAGE | Medium | Publication coverage lacked bounded fuzzers for several decoders and cross-layer properties over every supported input/output count. | Canonical decoder, count, root, and witness drift had insufficient independent detection. | Closed by `2f4d065`: seven new fuzz targets, 16 circuit property shapes, SDK `1..16/1..32` properties, keeper 12-input properties, and frozen independent KATs. |
| FIVE-SHAPE-CAPACITY-EVIDENCE | Medium | Capacity evidence did not report all five required shapes with distribution statistics and scanner/wire/state/gas profiles. | The experimental publication could not substantiate its resource envelope or compare shapes consistently. | Closed by `7407007`: five-shape p50/p95/max resource gate plus solve, gas, wire/state/event, and scanner benchmarks. |
| LOCALNET-RESTART-GENESIS-CONTINUATION | High | The localnet restart helper backgrounded a shell function, so the tracked PID was not the node process; the old node could answer health checks after a new start failed on the DB lock. Genesis continuation was also not exercised. | Restart and recovery claims were false-positive publication evidence. | Closed by `15e644a`: direct process launch with `kill -0` lifecycle checks, real node/prover restarts, non-zero-height genesis export/import, cursor/cache/path/reserve/asset comparisons, and post-import continuation. |
| DEPOSIT-PLAINTEXT-LOGGING | High | The deposit CLI printed the complete NotePlaintextV1 hex to stderr. | Receiver keys, amount, randomness, and memo could leak to terminal capture or application logs. | Closed by `c40c865`: the helper now returns only the message and does not log note plaintext. |
| BILINGUAL-CONTRACT-ALIGNMENT | Medium | English and Korean handoff/security documents still described the batch Go implementation as pending or conflated it with downstream product completion. | Reviewers and integrators could rely on a contract state different from the code. | Closed by `cee89a7`: 18 public EN/KR documents now distinguish the implemented Go reference from pending downstream/formal/production work. |
| PROVER-BODY-HARD-LIMIT | Medium | The public low-level prover `HTTPHandler` bounded only the batch route; transfer and withdraw used unbounded `io.ReadAll` before admission. | A directly mounted handler could materialize an attacker-controlled body in memory before acquiring a permit, creating a memory DoS path. | Closed by `fc45edd`: all three routes share the same hard reader, wire/gzip overflow returns 413, errors do not echo payloads, and transfer/withdraw overflow is tested before admission. |
| IMMUTABLE-VALIDATION-SNAPSHOT | Medium | The initial Completion Record used mutable `HEAD` ranges and a `independent publication validation closure commit` placeholder. | A published validation claim could not be reproduced against one immutable source snapshot. | Closed by this record: the reports, Completion Record, and master ledger pin validated implementation `494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2`; the generated release manifest separately records the exact publication-record commit and archive checksum. |
| DIRTY-RELEASE-PACK | Medium | Release-pack generation copied working-tree files but recorded only `HEAD`, and verification regenerated the same dirty content. | Uncommitted docs, schemas, fixtures, or source could pass verification while being absent from the claimed commit. | Closed by `8b48483`: generation checks tracked and untracked non-ignored state before copying and again before writing the manifest; dirty generation and default verification fail closed, while ignored `dist/` and `tmp/` outputs remain allowed. |
| IGNORED-FILE-EXCLUSION | Medium | The clean-worktree check ignored files, while recursive directory copies included ignored `.env`, `node_modules`, or other local files. | A local secret or development artifact could leak into the pack without changing Git status or the claimed commit. | Closed by `816f627`: every selected path is copied from `git archive` of the pinned source commit, so only tracked blobs can enter. An ignored `.env` probe inside a copied example directory was confirmed absent from the archive. |
| CONCURRENT-RELEASE-SNAPSHOT | Medium | Two status checks did not detect a concurrent change that was committed between individual working-tree copies and manifest creation. | The archive could mix old content while recording a newer clean `HEAD`. | Closed by `816f627`: source SHA is pinned before extraction, every file comes from that Git tree, HEAD must still equal the pinned SHA before manifest creation, and the manifest writes only the pinned SHA. |
| PROVER-HANDLER-DOC-ALIGNMENT | Low | Four EN/KR security and operations pairs still said the low-level prover handler had no body limit after `fc45edd`. | The guidance was conservative but no longer described the actual boundary, which could confuse downstream review. | Closed in the publication record: docs now state that the raw handler keeps the hard cap while the production wrapper additionally owns auth, gzip wire/decompressed limits, health/readiness policy, and server timeouts. |
| INVALID-COMMIT-PROVENANCE | High | The first immutable record expanded short commit `816f627` into a nonexistent 40-character object ID. | The exact validation range failed to resolve, so the publication claim could not be reproduced and publication gate provenance was invalid. | Closed by this record: every exact range now pins verified object `494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2`, and the range is executed before publication. |
| EXTERNAL-ARCHIVE-GIT-BINDING | Medium | Explicit external archive verification trusted the archive's self-supplied manifest and checksums without an out-of-band expected commit or Git-blob comparison. | A self-consistent forged or stale archive could claim an arbitrary commit and pass handoff verification. | Closed by `47bcca5`: explicit verification requires `RELEASE_PACK_EXPECTED_COMMIT`, resolves it locally, checks the canonical manifest commit, rejects non-regular entries, and compares every non-generated packed file byte-for-byte with the claimed Git tree. |
| RELEASE-PACK-COMPLETENESS | Medium | Git-blob verification iterated only files still present in the archive, so a non-required tracked source file could be deleted and checksums regenerated. | An incomplete handoff pack could claim the expected commit and pass. | Closed by `3453b55`: generator and verifier share a tracked selected-path manifest, derive the exact expected recursive Git file set, and require exact equality with archive files. A removed JS prover source now fails. |
| CANONICAL-RELEASE-MANIFEST | Medium | Generated `RELEASE-MANIFEST.txt` was checked only for its commit line. | Source identity, contents description, or validation instructions could be modified and self-checksummed. | Closed by `3453b55`: a tracked manifest template is rendered with canonical version/commit/time fields and the entire generated manifest must match it byte-for-byte. |
| SAFE-TAR-VALIDATION | Medium | Tar types and duplicates were checked only after extraction, where a later regular duplicate could hide an earlier symlink entry. | Different tar readers could consume different effective content from an archive that verification accepted. | Closed by `3453b55`: bounded Python `tarfile` validation rejects non-canonical paths, duplicate headers, links/special entries, multiple roots, and size/member overflows before accepting extracted files. |
| IMMUTABLE-EXPECTED-COMMIT | Medium | `RELEASE_PACK_EXPECTED_COMMIT` accepted moving refs and short SHAs. | A branch, tag, or `HEAD` could move and cease to be an immutable out-of-band trust anchor. | Closed by `3453b55`: explicit verification accepts only a canonical lowercase 40-character commit SHA and requires that exact commit locally. |
| EXTRACTED-FILE-PERMISSIONS | Medium | Release verification compared only the executable bit of packed Git files. A `100644` file changed to mode `0400` still passed after checksums were regenerated. | A handoff archive with unusable or unexpectedly permissive file permissions could be approved while the report claimed Git mode equality. | Closed by `db79ff0`: every tracked file is compared against exact `0644`/`0755` Git-derived permission mode, generated manifest/checksum files must be `0644`, and a `README.md` mode-`0400` adversarial archive is rejected. |
| RAW-TAR-MODE-VALIDATION | Medium | Exact mode comparison occurred only after extraction masked raw tar modes with `0o777`. Raw `04644`/`04755` headers therefore normalized to `0644`/`0755` and passed. | Setuid/setgid/sticky bits could be hidden from the verifier while another extractor honored the raw header, invalidating the exact-mode handoff claim. | Closed by `7e27721`: raw regular-file modes are restricted to exact `0644`/`0755`, directories to exact `0755`, every parent directory must have an explicit canonical member, and special-bit/directory-mode variants are rejected before extraction. |
| UMASK-INDEPENDENT-PACKAGING | Medium | Release-pack modes inherited the caller's umask. Under `umask 077`, the official generator emitted `0600`/`0700` members and `0600` metadata that its verifier rejected. | Secure CI/operator environments could not reproducibly create a valid official handoff pack. | Closed by `7e27721`: tracked files are reset from Git mode, directories are `0755`, generated metadata and archive/checksum outputs are `0644`; a complete `umask 077` generation and external verification round trip passed. |
| SDK-PRIVATE-FILE-REPLACEMENT | Medium | Transfer, withdraw, prover-transport, and wallet writers used `os.WriteFile(path, ..., 0600)`, which does not narrow an existing `0644` file. | Prepared witness, note randomness/path/signatures, keys, amounts, and wallet cache could remain readable by other local users despite the documented private-file boundary. | Closed by `d5cef57`: all SDK private JSON paths share an atomic durable fresh-inode writer, replace permissive files and symlinks with exact `0600`, and have direct/race regression coverage. |
| EXPLICIT-ARCHIVE-FAIL-CLOSED | Medium | Explicit release verification regenerated a current-HEAD pack when either the supplied archive or checksum was missing. | A corrupt or incomplete external artifact could be replaced rather than verified, defeating the out-of-band provenance gate. | Closed by `120a2d3`: only default non-explicit verification generates a pack; explicit archive/checksum inputs must both exist, fail closed when missing, and are never modified. A corrupt archive with a missing checksum remained byte-identical and was rejected. |
| REPO-WIDE-PRIVATE-OUTPUT | Medium | Reference payroll CLI/daemon and localnet seed output still used direct `os.WriteFile(..., 0600)`, retaining an existing `0644` mode after the SDK fix. | Employee IDs, recipients, amounts, note lookup data, reports, or seeded private state could remain readable by other local users. | Closed by `5ae6140`: the fresh-inode atomic writer moved to module-level `internal/privatefile` and now covers every production `0600` JSON writer; direct command and seed mode regressions plus targeted race passed. |
| BATCH-HANDOFF-STATUS-ALIGNMENT | Medium | Required EN/KR release handoff sections still described the implemented Go SDK/prover/scanner/payroll/CLI reference surfaces for batch integration as incomplete. | Downstream recipients could plan against a contract state that contradicted code, Completion Records, and the same handoff pack. | Closed by `0d640ac`: both documents distinguish completed Go reference surfaces from pending external JS/web product delivery, formal setup, and production artifact distribution. |
| PAYLOAD-FREE-PROVER-ERRORS | Medium | Transfer and withdraw prover routes returned raw decode, validation, prove, and response-validation errors. A canonical-hash request exposed a private `asset_denom` canary in the HTTP body. | Witness fields could enter client, proxy, and telemetry logs despite Pass H's payload-free error requirement. | Closed by `fea5968`: all three routes use fixed payload-free errors, transfer/withdraw use strict JSON decoding, nil responses fail safely, and decode/validation/prove/response canary tests plus race passed. |

No finding required a protocol contract, public-input order, or NoteV1 change. Batch protocol/chain-core re-entry was therefore not required. Unresolved Critical/High findings: 0. Unresolved security-relevant Medium findings: 0.

## Historical — Pass A–I result matrix

| Pass | Independent checks | Result |
| --- | --- | --- |
| A — current remediation | Duplicate 2x2 inputs/nullifiers; transfer final-effect mutations; withdraw chain/expiry/recipient binding including leading-zero bytes; disclosure secret blinding; global commitment uniqueness; canonical/subgroup ECIES and EdDSA decoding; no automatic prover failover; historical-root and artifact-identity rejection. | Passed in targeted adversarial suites and full privacy regression. |
| B — NoteV1 | Native, Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32, scanner, genesis, denom registry, exact empty tree, domain separation, fixed encodings, version/reserved/trailing-byte rejection. | Passed, including production-helper-free frozen KATs. |
| C — batch statement | Twelve public inputs in order; secret witnesses; count bounds; active prefix; disabled sentinel; vector node type/level; 16 paths; roots; distinctness; ownership/asset; range/conservation; disclosures; digest limbs; one owner signature. | Code, normative docs, matrix, SDK, prover, keeper, and circuit agree. |
| D — adversarial witness | Malformed count/slot/root/domain/payload/disclosure, sparse active slots, nonzero disabled helpers, duplicate nullifiers/commitments, membership/owner/signature/key mutation, wrap/conservation, reordered vectors, digest-limb swaps, missing/reused/zero blinding. | Rejected by the negative matrix and mutation/property coverage. |
| E — differential/property | Counts `1..16` and `1..32`, amount distributions, change/padding, disclosure modes, self-view, roots, digest limbs, intent, and public witness serialization. | Passed; deterministic seeds and frozen fixtures are non-secret. |
| F — host/consensus | Proto framing and hard cap; deterministic gas precharge; bounded invalid proof; state byte charge; proof-gated atomic writes; 2x2+Batch and Batch+Batch in both orders. | Passed, including complete rollback on cross-message nullifier reuse. |
| G — event/scan | Minimal event, one typed payload copy, global sequence/cursor, effect ID, limits, pagination/retry/restart, corrupt-state failure, no lossy fallback, one-snapshot paths, genesis round trip, NoteV1 recomputation, item evidence. | Passed in unit/property tests, size profile, and fresh localnet recovery rehearsal. |
| H — prover/privacy | Lazy VK vs selected R1CS/PK, body/admission limits, permit lifetime through actual prove, cancel/panic recovery, secret-free logs/errors, no automatic failover, ciphertext policy, safe view-tag default, development artifact labels. | Passed; transport hardening, strict payload-free errors, plaintext logging, and private-file replacement findings were fixed. |
| I — payroll/reconcile | Atomic many-to-many persistence, 31+change/exact32, role/index/evidence, batch vs item outcome, tx/nullifier lookup before retry, explicit re-sign, audit/manual review metadata. | Passed in memory, durable-file, SQL, CLI/daemon private-output, and live localnet coverage. |

## Historical — independent golden known-answer tests

At least three KAT paths intentionally calculate frozen bytes without importing the production helper under test:

- `TestPrivacyNoteV1ContractIndependentGolden`
- `TestPrivacyBatchJoinSplitV1ContractIndependentGolden`
- `TestCanonicalBatchTransferPayloadV1IndependentGolden`

The independent canonical batch payload is 3,702 bytes and its SHA-256 digest is `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`. Separate independent vector-root and effect-ID tests cover typed vector domains and exact effect identity.

## Historical — property and fuzz coverage

New bounded fuzz targets cover NotePlaintextV1, DisclosurePlaintextV1, transfer and batch canonical payloads, batch vector roots including active-prefix/disabled sentinels, typed scan page/cursor round trips, and strict batch prover JSON requests. Existing bounded fuzz targets cover canonical/subgroup point, EdDSA signature, and ECIES envelope decoding. Every target is required to remain panic-free, bounded, canonical on accepted round trips, fail-closed on malformed/trailing input, and secret-free in errors.

The seeded circuit property covers every input count `1..16` with randomized output counts, amounts, disclosure modes, and keys. SDK and keeper properties independently cover all input counts `1..16` and output counts `1..32`. Each new target completed a three-second bounded run; the final command table records the aggregate rerun.

## Historical — development capacity profile

Environment: Apple M5 Pro, `darwin/arm64`, Go `1.25.12`, gnark `0.14`, measured at `2026-07-11T19:55:51Z`. These are development-artifact measurements, not a production SLA. Each proof shape used five samples.

Common artifact profile:

| Metric | Value |
| --- | ---: |
| Constraints | 1,111,837 |
| Compile | 961.485 ms |
| Development setup | 15,999.531 ms |
| R1CS | 122,813,535 bytes |
| Proving key | 209,218,621 bytes |
| Verifying key | 716 bytes |
| Proof | 164 bytes |
| Peak RSS | 3,429,646,336 bytes |

Artifact SHA-256:

- R1CS: `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`
- PK: `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`
- VK: `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`

| Inputs/outputs | Witness p50/p95/max ms | Prove p50/p95/max ms | Verify p50/p95/max ms |
| --- | ---: | ---: | ---: |
| 1/1 | 0.384 / 0.400 / 0.400 | 1,639.739 / 1,674.164 / 1,674.164 | 0.682 / 0.685 / 0.685 |
| 3/4 | 0.380 / 0.417 / 0.417 | 1,654.115 / 1,670.166 / 1,670.166 | 0.671 / 0.672 / 0.672 |
| 8/16 | 0.367 / 0.387 / 0.387 | 1,667.218 / 1,729.603 / 1,729.603 | 0.671 / 0.674 / 0.674 |
| 16/31 | 0.368 / 0.431 / 0.431 | 1,661.212 / 1,695.265 / 1,695.265 | 0.672 / 0.877 / 0.877 |
| 16/32 | 0.370 / 0.411 / 0.411 | 1,694.573 / 1,719.530 / 1,719.530 | 0.670 / 0.679 / 0.679 |

Warm mean proof time was 152.349 ms for 2x2 and 1,693.102 ms for 16x32, or 76.174 ms/output versus 52.909 ms/output. This is about 1.44x development throughput per output and is not a production capacity claim.

| Inputs/outputs | Keeper gas | Protobuf tx bytes | Typed scan KV bytes | Tree bytes | Total state bytes | Event bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1/1 | 1,306,824 | 2,626 | 2,613 | 3,072 | 5,685 | 582 |
| 3/4 | 2,133,464 | 8,709 | 9,647 | 12,288 | 21,935 | 582 |
| 8/16 | 5,333,776 | 32,942 | 37,681 | 49,152 | 86,833 | 583 |
| 16/31 | 9,396,144 | 63,289 | 72,783 | 95,232 | 168,015 | 584 |
| 16/32 | 9,648,080 | 65,294 | 75,105 | 98,304 | 173,409 | 584 |

The 32-output scanner benchmark measured 1,317,912 ns/op, 24,281 outputs/s, 214,851 B/op, and 1,393 allocs/op.

## Historical — fresh localnet and recovery result

The batch rehearsal completed five live proof/message cases: 1/1, 3/4, 31 payments plus change, exact 32 payments, and zero-padding. It also exercised recipient, auditor, and self-view scanning; view-tag-safe full decryption; no automatic prover failover; tx-hash reconciliation; rejected spent-nullifier retry; real prover and node restarts; cursor resume; and a non-zero-height genesis export/import.

Before export the chain was at height 47 with 42 summaries, 138 outputs, global sequence 42, and 68 Bob notes. After import, the scanner and wallet cache matched without missing or duplicate outputs. A new deposit then continued at height 49, sequence 43, leaf 139, and reserve 2,658 while preserving path, asset registry, and reserve state. Live transaction gas used was 1,609,514 (1/1), 3,141,099 (3/4), 16,017,355 (31+change), 16,876,619 (exact32), and 15,529,326 (padding).

The separate privacy smoke regression covered Deposit, JoinSplit2x2, and Withdraw, including expiry and chain-domain authorization negatives. The reference payroll live localnet covered reserve, prove, broadcast, reconcile, item evidence, and report generation.

## Historical — final verification commands

| Command | Result |
| --- | --- |
| `go test ./... -count=1` | Passed without cache |
| Private writer and prover canary targeted tests/race | Passed; SDK/payroll/seed outputs replace permissive files with `0600`, and no proof route echoed decode/validation/prove/response canaries |
| `go test -race ./x/privacy/... -count=1 -timeout=30m` | Passed; the first default-timeout run had no race report but timed out in `zk` after `circuit` and `keeper` passed |
| 10 bounded fuzz targets, `-fuzztime=3s` each | Passed |
| `go vet ./...` | Passed |
| `make ci` | Passed |
| `make vulncheck` | Passed under the documented exact policy; three no-fixed-version residuals retained |
| `make examples` | Passed; one example-only npm Low retained |
| `make privacy-e2e-smoke` | Passed twice on fresh state, including the release gate run |
| `make privacy-batch-joinsplit-localnet` | Passed on fresh state with restart/genesis continuation |
| `make reference-payroll-live-localnet` | Passed on fresh state |
| `make release-check` | Passed, including CI, vulnerability policy, general localnet, privacy E2E, and two-batch bulk readiness |
| `make release-pack` | Passed; independent publication validation EN/KR reports included |
| `make release-pack-verify` | Passed; 125 required files, exact selected Git file set, canonical manifest/tar/checksum list, Git blobs/raw and extracted exact permission modes, and archive checksum verified |
| External archive adversarial variants | Passed; missing immutable archive/checksum inputs, missing files/directories, modified manifest, duplicate members, moving/short expected commits, mode `0400`/`04644`/`04755` Git files, and mode-`0777` directories were rejected |
| `umask 077` release generation and external verification | Passed; tracked files, directories, metadata, archive, and checksum retained their canonical modes |
| `git diff --check e427370..494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2` and publication hygiene checks | Passed; tracked artifact/secret/personal-path results were empty and generated `dist/`/`tmp/` remained ignored |

## Historical — accepted residual risks and Production TODO

| Residual | Owner | Reason accepted for experimental publication | Production blocking |
| --- | --- | --- | --- |
| External ZK/constraint audit, final source freeze, official MPC/trusted setup, transcript and toxic-waste evidence | Protocol/release owners | Explicitly outside independent publication validation; development artifacts only | Yes |
| Artifact reproducibility, signing, provenance, custody, production circuit manifest, SBOM/image provenance | Release and validator operators | Development hashes are recorded but are not production provenance | Yes |
| Production gas governance, VK/circuit genesis or upgrade pinning, rollout/rollback, staging load/fault rehearsal, monitoring and incident response | Downstream chain owner | Requires the target chain and governance process | Yes |
| Remote prover TLS/auth/ACL/quota/process isolation, retention policy, capacity and fault rehearsal | Prover operator | Client defaults are fail-closed, but managed infrastructure is downstream | Yes |
| Audit-key HSM/KMS/threshold custody, rotation, decrypt-failure/manual-review operations | Auditor/payroll operator | The code records evidence and manual-review state; operational custody is external | Yes |
| Downstream JS/TS wallet and product implementation | Downstream product owner | The Go reference, schema, fixture, and handoff are available | Yes for downstream production; no for source publication |
| Public input/output counts, batch grouping, timing, and policy-dependent metadata leakage | Product/privacy owner | Inherent/declared batch metadata; padding is an explicit cost/privacy decision | Requires production privacy acceptance |
| In-process development proving peak RSS and lack of process isolation | Prover operator | Suitable only for controlled experimental reproduction | Yes |
| No-fixed-version advisories `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932` under the repository policy | Dependency/security owner | No fixed version is currently available under the exact policy; tracked, not silently ignored | Reassess for production |
| One low-severity npm advisory in examples | Examples/dependency owner | Example-only low severity under the release policy | Reassess for production |

No accepted residual permits calling this implementation production-ready or audited.

## Historical — publication hygiene

The final gate checks Git-tracked paths for R1CS/PK/VK binaries, private keys, seeds, tokens, audit secrets, personal absolute paths, scratch benchmarks, and temporary files. Generated development artifacts and `dist/` release packs remain ignored and untracked. English/Korean contracts, fixtures, schemas, examples, and the release handoff pack are checked together.
