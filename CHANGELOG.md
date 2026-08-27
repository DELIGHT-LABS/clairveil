# Changelog

All notable changes to Clairveil are documented in this file.

This project follows [the release versioning policy](docs/clairveil-release-versioning-policy.md) and [handoff-pack policy](docs/clairveil-release-handoff-pack.md).

## Unreleased

### Added

- Added the English/Korean WebApp integration pack: explicit supported-flow
  scope, versioned browser chain-profile schema, browser API/lifecycle guide,
  encrypted storage/recovery contract, and browser/prover deployment controls.

### Changed

- The example DApp now emits and validates
  `clairveil-web-client-config-v1`; server and static flattened compatibility
  fields must agree with the active chain profile before a browser flow starts.
  The legacy top-level EVM account prefix remains host metadata and cannot
  override the profile privacy prefix passed to ClairveilJS.
- ClairveilJS browser-client declarations now expose the existing typed,
  read-only `evmJsonRpc<TResult>` recovery/query method.
- The example DApp now consumes the ClairveilJS browser preflight surface for
  circuit, asset, audit/disclosure, tree, reserve, and EVM chain validation.
  It fails closed before displaying spendable inventory and again immediately
  before every supported privacy preparation.
- The example DApp now targets ClairveilJS 0.3.1 through the sibling checkout
  used by local development and an exact pinned checkout in CI. Its required
  conformance suite covers the current browser circuit/asset/tree preflight and
  V5/V2 preparation/proof contracts.

### Fixed

- Restored reproducible example-DApp installs by checking out the exact
  ClairveilJS commit into the sibling dependency path in CI, while local
  development continues to use its sibling checkout. Bundle generation now
  avoids developer-specific absolute paths.
- Kept the v0.3.1 example's one-proof batch product flow intentionally disabled:
  the server reports `serverFeatures.batchTransfer=false`, and the UI exposes
  neither batch submission nor an authorized batch-audit surface.
- Removed the example DApp's legacy 0.1 browser-data cleanup UI and
  implementation. The unreleased v0.3.1 client supports only current-namespace
  fresh initialization and a full typed rescan; it defines no legacy lifecycle
  migration or in-place downgrade contract.
- Make `dapp-local` enable the loopback same-origin prover proxy. The local
  browser DApp no longer attempts a cross-origin request to the reference
  prover, which intentionally does not provide a browser CORS policy.
- Renew the example DApp's current chain-configuration preflight when its
  short-lived lease expires. Cached notes no longer stay hidden as
  `Sync unavailable` solely because the previous successful preflight aged
  out; a failed renewal still fails closed.
- Updated the pinned ClairveilJS 0.3.1 integration so browser proof requests
  preserve a configured prover URL path prefix, matching the WebApp profile and
  deployment-gate contract. DApp compatibility fixtures construct valid
  current Merkle paths and canonical asset IDs.

### Security

- The browser DApp now serializes every Cosmos public/private submission for a
  canonical chain/account across tabs and equivalent profiles, revalidates the
  local genesis epoch inside that fence, and durably records the exact signed
  Cosmos transaction hash before entering the RPC broadcast boundary. Private
  submissions also mirror that hash into a non-sensitive account marker, so a
  reconnect blocks transparent sends even before privacy setup restores the
  encrypted reservation store. REST endpoint changes invalidate the privacy
  session and are blocked while a scan or account transaction is active.
- Public EVM send/deposit records a hashless wallet-boundary attempt before
  `eth_sendTransaction`, promotes it synchronously when MetaMask returns a hash,
  and keeps an ambiguous attempt blocked for explicit wallet-history recovery.
- Replaced the example DApp's plaintext note cache and relay recovery metadata
  with namespace-separated AES-GCM localStorage envelopes, and reservation
  state with encrypted IndexedDB records guarded by Web Locks. Privacy setup
  requires the corresponding browser storage, Web Crypto, and Web Locks and
  does not fall back to plaintext storage or memory.
- Hardened the example static server: loopback is the default bind, the prover
  proxy needs explicit direct-loopback local-test opt-in, public mode rejects
  proxy/token/cleartext configuration, and static responses receive CSP,
  `nosniff`, no-referrer, and cross-origin opener headers.

### Migration Notes

- Upgraded `privacy_note_reservation_contract.json` and its schema from v1 to v3. Downstream reservation implementations must fail closed for malformed or unavailable nullifier/relay chain-time evidence, keep lease heartbeats through the ProofReady transition, and durably record a leased `ProofReady` relay handoff before exposing payloads externally.
- Replace unrestricted `UpdateReservation`/`UpdateOperation` calls with the Service-owned atomic batch, reconciliation, lease-expiry recovery, proof-discard, and relay-handoff commands. Persistent Store implementations must validate the current lease owner and token together and commit linked reservation/operation changes in one transaction.
- Durable reservation lifecycle storage is schema v2 in both JSON snapshots and SQL metadata. This unreleased workspace has no lifecycle-store migration or rollback contract; stores are initialized directly at the current schema.
- `FoundNote.VerifiedUnspent` is Go client SDK hardening, not a new consensus boundary or a freshness proof. It is a public Go/JSON shape change (`verified_unspent`); older cached entries without the field decode as false and must complete a successful nullifier revalidation before planning. A normal sync revalidates cached notes, while Reset & Rescan is available for discarded or corrupt caches. This flag does not replace the mandatory pre-broadcast nullifier check, and ClairveilJS/DApps need equivalent fresh-query behavior rather than this Go field itself.

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
