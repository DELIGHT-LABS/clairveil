# Changelog

All notable changes to Clairveil are documented in this file.

This project follows [the release versioning policy](docs/clairveil-release-versioning-policy.md) and [handoff-pack policy](docs/clairveil-release-handoff-pack.md).

## Unreleased

### Security

- Updated OpenTelemetry to v1.44.0 and gRPC to v1.82.1 to resolve `GO-2026-5158` and `GO-2026-6061`.

### Migration Notes

- Upgraded `privacy_note_reservation_contract.json` and its schema from v1 to v3. Downstream reservation implementations must fail closed for malformed or unavailable nullifier/relay chain-time evidence, keep lease heartbeats through the ProofReady transition, and durably record a leased `ProofReady` relay handoff before exposing payloads externally.
- Replace unrestricted `UpdateReservation`/`UpdateOperation` calls with the Service-owned atomic batch, reconciliation, lease-expiry recovery, proof-discard, and relay-handoff commands. Persistent Store implementations must validate the current lease owner and token together and commit linked reservation/operation changes in one transaction.
- Notes without an explicit successful `used: false` nullifier response are now unverified and excluded from spending. Clear or revalidate older cached `isSpent: false` entries before using them for planning.

## v0.3.1 - 2026-07-21

### Fixed

- Added the missing paired dated changelog headings for the already-published `v0.3.0` tag and completed the release documentation, supported-version references, and immutable release packaging metadata. There is no Go, protobuf, runtime, state, circuit, or wire-contract change from `v0.3.0`.

### Handoff Notes

- Downstream codebases already pinned to `v0.3.0` may continue using it unchanged. `v0.3.1` is the documentation and release-preparation publication and is the identity used for the verified handoff pack and GitHub release; `v0.3.0` remains unmoved and unreused.

## v0.3.0 - 2026-07-21

### Added

- Added the trusted in-process `Keeper.DepositWithFunder` integration surface, which preserves `msg.Creator` attribution while debiting an explicit validated funder through the canonical deposit transition.

### Changed

- Kept the public `MsgDeposit` protobuf, gRPC, CLI, client wire format, actor-as-funder behavior, and existing gas path unchanged; deposit mutations now use a nested cache so downstream callers can combine core-local rollback with an outer SDK/EVM rollback boundary. The trusted entry alone performs the additional module-balance reads required to verify its explicit-funder bank transfer.

### Security

- Documented that downstream adapters must derive the actor from the authenticated caller, use only a fixed escrow funder distinct from the `privacy` module account, bind the deposit amount exactly to EVM `msg.value` and the runtime native denom, and roll back later policy failures atomically. The trusted Keeper API rejects the `privacy` module account as a funder and verifies the exact module-balance increase after the bank send so self-transfers or redirected sends cannot mint an unbacked shielded deposit.

## v0.2.0 - 2026-07-13

### Added

- Added the transfer authorization contract with prepared payload/disclosure plaintext `v5`, proof/prover contract `v2`, chain ID, absolute expiry, final owner intent, canonical decoding, and disclosure blindings.
- Added the `privacy-note-v1`/state-version-2 foundation: domain-separated NoteV1 primitives, `privacy-fixed-v1`, `AssetRegistryV1`, typed scan/path snapshots, bounded prover admission, and consensus-pinned circuit identity.
- Added the production `BatchJoinSplit16x32` chain core and `MsgBatchTransfer`, followed by reference surfaces for batch integration: the Go SDK, `POST /v1/proofs/batch-transfer`, scanner, one-proof payroll, CLI, and localnet tutorial.
- Extended the v0.1.0 reference payroll foundation with durable file/SQLite/PostgreSQL stores, live daemon and reconciliation flows, rehearsal evidence, capacity tooling, and public-claim eligibility gates.
- Added paired getting-started, architecture, documentation-index, and plan-status documents; restored three missing English payroll/bulk handoff documents.
- Added `make docs-check` and a single required-file manifest for release-pack documentation/link/language/tag/file validation.

### Changed

- Active compatibility is now `privacy-note-v1`, privacy state version 2, and `privacy-fixed-v1`. Existing three-circuit development chains require fresh genesis/reset; old artifacts, proof jobs, prepared payloads, and note/reservation/scan caches must be discarded and rescanned.
- Release-pack membership is now defined by `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt`; superseded bulk phase-1 plans and duplicate working notes are excluded from the handoff pack.
- Release packaging now distinguishes non-publishable full-commit CI snapshots from publishable annotated exact-SemVer tags, binds release tags to the manifest commit and paired dated changelog headings, verifies the already-generated default archive without replacing it, and separates tracked release metadata from post-tag external release notes.
- Documentation now distinguishes the legacy multi-message `transfer-batch` command from the one-proof `transfer-batch-16x32` flow and lists the complete current query/prover surface.

### Fixed

- Fixed duplicate input/nullifier inflation, global nullifier/commitment collision handling, disclosure-blinding separation, structured batch-signing secret reuse, non-canonical BN254 aliases, and atomic rollback regressions.
- Fixed prover cancellation/failover and payroll localnet cleanup boundaries, and aligned CLI/environment examples with the implemented flags and exported checksum behavior.
- Hardened documentation and handoff validation against mismatched release identities, incomplete archive/checksum pairs, fenced or commented changelog impostors, CommonMark link edge cases, missing fragments, and an omitted path manifest; aligned smoke port overrides and rehearsal decisions with their documented contracts.

### Security

- Completed the security, protocol, chain-core, client-integration, and independent publication validations with no unresolved Critical, High, or security-relevant Medium finding; disposition is `PUBLICATION_READY_EXPERIMENTAL`, not production approval.
- Consensus verifier identity, role-aware artifact loading, bounded proof verification/admission, secret-free validation errors, and explicit opt-in for privacy-expanding multi-prover failover are enforced and documented.

### Known Risk

- Formal trusted setup, external ZK/security audit, signed production artifact distribution, chain-specific migration, production wallet storage, audit-key custody, and downstream product validation remain outside this release state.
- The documented `govulncheck` exceptions must be reassessed by each downstream production project; see `SECURITY.md`.

### Handoff Notes

- Read documentation from the exact integrated tag/commit and verify `RELEASE-MANIFEST.txt`; do not combine `HEAD` documentation with older code or artifacts.
- External JS/TS and web products must port the frozen Go reference contracts and pass their own storage, prover, scan, disclosure, and batch end-to-end gates.

## v0.1.0 - 2026-07-06

### Added

- Added a dependency-free Node audit-disclosure-key example.
- Added client product, UX, risk-decision, and API integration handoff documents.
- Added bounded shielded amounts, deposit binding proofs, reserve accounting queries, and updated ZK artifact contracts.
- Added cursor-based `scan_events`, batch nullifier queries, transfer view tags, and their schema/fixture contracts.
- Added scan optimization, sender self-view disclosure, and the relayed-withdraw handoff contract for downstream wallets and relayers.
- Added benchmark reporting plus prover/localnet/user-latency/bulk load runners and public-capacity planning evidence.
- Added note-reservation and reference payroll planner SDKs, proof and multi-message broadcast workers, a bounded prover pool, localnet transfer-batch harnesses, bulk benchmarks, and the bulk-readiness gate.

### Changed

- Removed legacy output-note fields from `MsgWithdraw`; clients must regenerate proto bindings without dummy output-note values.
- Documented scan cursor persistence, empty-page advancement, safe view-tag mismatch fallback, and regenerated proto/schema/fixture requirements.

### Fixed

- Hardened canonical prover amount and artifact checksum validation, transfer output bounds, reservation ownership/rollback, persisted reconciliation errors, and bulk-readiness failure handling.

## v0.0.0 - 2026-05-19

### Added

- Published the initial standalone Clairveil privacy core, reference daemon, prover service, fixtures, schemas, CI, and release handoff documentation.
- Added Apache-2.0 license/notice hygiene, release versioning, release notes, handoff-pack verification, restore/security guidance, and Korean public documentation.
- Added `make install` and `make init` for the default local reference chain and clarified the manual walkthrough versus initialization shortcut.
