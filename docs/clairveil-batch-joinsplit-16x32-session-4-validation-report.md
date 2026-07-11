# Batch JoinSplit 16x32 Session 4 Independent Validation Report

## Status

| Item | Result |
| --- | --- |
| Review range | `e427370..HEAD` |
| Review role | Fresh reviewer independent of Sessions 1–3B implementation |
| Gate 3B on entry | Initially blocked by an uncommitted integration tree; closed only after the fixes and revalidation recorded below |
| Session 4 publication state | `PUBLICATION_READY_EXPERIMENTAL` |
| Production release state | Not approved |
| Formal trusted setup | Not performed |
| External audit | Not performed |

`PUBLICATION_READY_EXPERIMENTAL`, when recorded after the final regression gate, means that the source, tests, and documentation may be published for experimental use. It does not mean production-ready, audited, or backed by production proving artifacts.

## Independent review method

The reviewer read the master roadmap and every Session 1–4 plan and Completion Record from beginning to end, reconstructed the protocol from code, and then compared the implementation to the normative contracts. No file was modified until the initial findings had been reproduced and adjudicated. Gate 3B was checked before Passes A–I. Because its clean-worktree and integration-evidence requirements were not initially met, Session 4 was not treated as passed while that gate was open.

The review reconstructed:

- NoteV1 domains, field order, empty-tree roots, vector roots, active prefixes, and disabled sentinels;
- the 12 public inputs and native, circuit, keeper, SDK, and prover witness encodings;
- owner authorization, membership, distinctness, conservation, disclosure, and payload binding;
- keeper gas, resource, atomicity, cross-message composition, scan/genesis state, prover privacy, and payroll evidence boundaries.

## Findings and fixes

| ID | Severity | Evidence | Impact | Resolution |
| --- | --- | --- | --- | --- |
| S4-01 | High | Batch preparation and payload construction trusted mutable plan totals, ordering, ownership, disclosure modes, and field values without a complete independent recomputation. | A tampered prepared object could cross the signing/prover boundary with semantics different from the planner's intent, or fail late after sensitive work. | Closed by `d9b1780`: recompute counts, ownership, asset, nullifier uniqueness, output roles, conservation, disclosure policy, canonical fields, and use durable private-file replacement. |
| S4-02 | High | Remote prover transport allowed non-loopback cleartext HTTP and ordinary redirect following; authentication was not available in the client contract. | A private witness could be sent in cleartext or redirected to an unintended endpoint. | Closed by `8dfe80b`: HTTPS except loopback, redirect denial, opt-in redirect-safe custom doers, bearer-token support, bounded responses, and atomic private-file writes. |
| S4-03 | High | Typed scan page validation did not prove cursor advances across zero-output summaries and the scanner requested a filtered event set. | Pagination could reject a valid boundary or conceal a skipped output-bearing event, breaking lossless wallet/payroll recovery. | Closed by `868f108`: unfiltered typed scans, ordered summary proof for cursor advances, no skipped output event, stable nil cursor handling, and no lossy ABCI fallback. |
| S4-04 | High | Durable payroll reservation, operation, item, lease, broadcast, and reconcile transitions were not consistently atomic or evidence-bound across memory, file, and SQL stores. | Crash/retry races could orphan reservations, duplicate work, or mark item outcomes without the required batch-output evidence. | Closed by `0b6b3ee`: atomic operation graph persistence, lease/CAS checks, evidence-bound reconciliation, and cross-store regression coverage. |
| S4-05 | Medium | The initial batch localnet rehearsal did not enforce all negative outcomes, artifact identity, process cleanup, or repository hygiene checks. | Gate 3B evidence could report success without proving the advertised integration boundary. | Closed by `d7809e9` and Gate 3B was re-recorded by `423f73a` only after targeted, full privacy, race, vet, examples, E2E, payroll, artifact, and release checks passed. |
| S4-06 | High | A confirmed failed payroll transaction had no explicit store transition that permitted a new-sequence re-sign while preserving the operation graph. | A recoverable chain failure became a durable dead end; operators could not safely retry without manual state surgery. | Closed by `16900cb`: `PrepareBatchOperationResign` is lease-bound, requires confirmed failure, rechecks nullifiers, and only proceeds with explicit `ResignWithNewSequence`. |
| S4-07 | Medium | Publication coverage lacked bounded fuzzers for several decoders and cross-layer properties over every supported input/output count. | Canonical decoder, count, root, and witness drift had insufficient independent detection. | Closed by `2f4d065`: seven new fuzz targets, 16 circuit property shapes, SDK `1..16/1..32` properties, keeper 12-input properties, and frozen independent KATs. |
| S4-08 | Medium | Capacity evidence did not report all five required shapes with distribution statistics and scanner/wire/state/gas profiles. | The experimental publication could not substantiate its resource envelope or compare shapes consistently. | Closed by `7407007`: five-shape p50/p95/max resource gate plus solve, gas, wire/state/event, and scanner benchmarks. |
| S4-09 | High | The localnet restart helper backgrounded a shell function, so the tracked PID was not the node process; the old node could answer health checks after a new start failed on the DB lock. Genesis continuation was also not exercised. | Restart and recovery claims were false-positive publication evidence. | Closed by `15e644a`: direct process launch with `kill -0` lifecycle checks, real node/prover restarts, non-zero-height genesis export/import, cursor/cache/path/reserve/asset comparisons, and post-import continuation. |
| S4-10 | High | The deposit CLI printed the complete NotePlaintextV1 hex to stderr. | Receiver keys, amount, randomness, and memo could leak to terminal capture or application logs. | Closed by `c40c865`: the helper now returns only the message and does not log note plaintext. |
| S4-11 | Medium | English and Korean handoff/security documents still described the batch Go implementation as pending or conflated it with downstream product completion. | Reviewers and integrators could rely on a contract state different from the code. | Closed by `cee89a7`: 18 public EN/KR documents now distinguish the implemented Go reference from pending downstream/formal/production work. |

No finding required a protocol contract, public-input order, or NoteV1 change. Session 2/3A re-entry was therefore not required. Unresolved Critical/High findings: 0. Unresolved security-relevant Medium findings: 0.

## Pass A–I result matrix

| Pass | Independent checks | Result |
| --- | --- | --- |
| A — current remediation | Duplicate 2x2 inputs/nullifiers; transfer final-effect mutations; withdraw chain/expiry/recipient binding including leading-zero bytes; disclosure secret blinding; global commitment uniqueness; canonical/subgroup ECIES and EdDSA decoding; no automatic prover failover; historical-root and artifact-identity rejection. | Passed in targeted adversarial suites and full privacy regression. |
| B — NoteV1 | Native, Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32, scanner, genesis, denom registry, exact empty tree, domain separation, fixed encodings, version/reserved/trailing-byte rejection. | Passed, including production-helper-free frozen KATs. |
| C — batch statement | Twelve public inputs in order; secret witnesses; count bounds; active prefix; disabled sentinel; vector node type/level; 16 paths; roots; distinctness; ownership/asset; range/conservation; disclosures; digest limbs; one owner signature. | Code, normative docs, matrix, SDK, prover, keeper, and circuit agree. |
| D — adversarial witness | Malformed count/slot/root/domain/payload/disclosure, sparse active slots, nonzero disabled helpers, duplicate nullifiers/commitments, membership/owner/signature/key mutation, wrap/conservation, reordered vectors, digest-limb swaps, missing/reused/zero blinding. | Rejected by the negative matrix and mutation/property coverage. |
| E — differential/property | Counts `1..16` and `1..32`, amount distributions, change/padding, disclosure modes, self-view, roots, digest limbs, intent, and public witness serialization. | Passed; deterministic seeds and frozen fixtures are non-secret. |
| F — host/consensus | Proto framing and hard cap; deterministic gas precharge; bounded invalid proof; state byte charge; proof-gated atomic writes; 2x2+Batch and Batch+Batch in both orders. | Passed, including complete rollback on cross-message nullifier reuse. |
| G — event/scan | Minimal event, one typed payload copy, global sequence/cursor, effect ID, limits, pagination/retry/restart, corrupt-state failure, no lossy fallback, one-snapshot paths, genesis round trip, NoteV1 recomputation, item evidence. | Passed in unit/property tests, size profile, and fresh localnet recovery rehearsal. |
| H — prover/privacy | Lazy VK vs selected R1CS/PK, body/admission limits, permit lifetime through actual prove, cancel/panic recovery, secret-free logs/errors, no automatic failover, ciphertext policy, safe view-tag default, development artifact labels. | Passed; transport hardening and plaintext-log finding were fixed. |
| I — payroll/reconcile | Atomic many-to-many persistence, 31+change/exact32, role/index/evidence, batch vs item outcome, tx/nullifier lookup before retry, explicit re-sign, audit/manual review metadata. | Passed in memory, durable-file, SQL, and live localnet coverage. |

## Independent golden known-answer tests

At least three KAT paths intentionally calculate frozen bytes without importing the production helper under test:

- `TestPrivacyNoteV1ContractIndependentGolden`
- `TestPrivacyBatchJoinSplitV1ContractIndependentGolden`
- `TestCanonicalBatchTransferPayloadV1IndependentGolden`

The independent canonical batch payload is 3,702 bytes and its SHA-256 digest is `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`. Separate independent vector-root and effect-ID tests cover typed vector domains and exact effect identity.

## Property and fuzz coverage

New bounded fuzz targets cover NotePlaintextV1, DisclosurePlaintextV1, transfer and batch canonical payloads, batch vector roots including active-prefix/disabled sentinels, typed scan page/cursor round trips, and strict batch prover JSON requests. Existing bounded fuzz targets cover canonical/subgroup point, EdDSA signature, and ECIES envelope decoding. Every target is required to remain panic-free, bounded, canonical on accepted round trips, fail-closed on malformed/trailing input, and secret-free in errors.

The seeded circuit property covers every input count `1..16` with randomized output counts, amounts, disclosure modes, and keys. SDK and keeper properties independently cover all input counts `1..16` and output counts `1..32`. Each new target completed a three-second bounded run; the final command table records the aggregate rerun.

## Development capacity profile

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

## Fresh localnet and recovery result

The batch rehearsal completed five live proof/message cases: 1/1, 3/4, 31 payments plus change, exact 32 payments, and zero-padding. It also exercised recipient, auditor, and self-view scanning; view-tag-safe full decryption; no automatic prover failover; tx-hash reconciliation; rejected spent-nullifier retry; real prover and node restarts; cursor resume; and a non-zero-height genesis export/import.

Before export the chain was at height 47 with 42 summaries, 138 outputs, global sequence 42, and 68 Bob notes. After import, the scanner and wallet cache matched without missing or duplicate outputs. A new deposit then continued at height 49, sequence 43, leaf 139, and reserve 2,658 while preserving path, asset registry, and reserve state. Live transaction gas used was 1,609,514 (1/1), 3,141,099 (3/4), 16,017,355 (31+change), 16,876,619 (exact32), and 15,529,326 (padding).

The separate privacy smoke regression covered Deposit, JoinSplit2x2, and Withdraw, including expiry and chain-domain authorization negatives. The reference payroll live localnet covered reserve, prove, broadcast, reconcile, item evidence, and report generation.

## Final verification commands

| Command | Result |
| --- | --- |
| `go test ./... -count=1` | Passed without cache |
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
| `make release-pack` | Passed; Session 4 EN/KR reports included |
| `make release-pack-verify` | Passed; 123 required files plus internal and archive checksums verified |
| `git diff --check e427370..HEAD` and publication hygiene checks | Passed; tracked artifact/secret/personal-path results were empty and generated `dist/`/`tmp/` remained ignored |

## Accepted residual risks and Production TODO

| Residual | Owner | Reason accepted for experimental publication | Production blocking |
| --- | --- | --- | --- |
| External ZK/constraint audit, final source freeze, official MPC/trusted setup, transcript and toxic-waste evidence | Protocol/release owners | Explicitly outside Session 4; development artifacts only | Yes |
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

## Publication hygiene

The final gate checks Git-tracked paths for R1CS/PK/VK binaries, private keys, seeds, tokens, audit secrets, personal absolute paths, scratch benchmarks, and temporary files. Generated development artifacts and `dist/` release packs remain ignored and untracked. English/Korean contracts, fixtures, schemas, examples, and the release handoff pack are checked together.
