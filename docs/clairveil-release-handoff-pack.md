# Clairveil Release Handoff Pack

This document collects the artifacts and validation steps needed when handing a Clairveil release to downstream chain, JS/TS SDK, web wallet, and prover operations teams.

Clairveil provides a reusable privacy core and reference host. The downstream project owns production-chain EVM integration, policy modules, precompiles, validator operations, audit private key custody, wallet storage encryption, and remote prover exposure policy.

Korean version: [clairveil-release-handoff-pack-kr.md](clairveil-release-handoff-pack-kr.md)

## 1. Artifacts Release Recipients Should Receive

| Item | File/Path | Recipient | Purpose |
| --- | --- | --- | --- |
| Go module | `go.mod`, `x/privacy`, `app`, `cmd/clairveild` | Core chain team | baseline for downstream Cosmos SDK app import or fork |
| Proto | `proto/clairveil/privacy/v1` | Core chain team, JS SDK team | tx/query type generation |
| SDK fixtures | `x/privacy/client/sdk/conformance/testdata` | JS SDK team, web wallet team | wallet/prover/query contract conformance |
| JSON Schema | `docs/schemas/clairveil-js-wallet-contract.schema.json` | JS SDK team, web wallet team | machine-readable fixture shape validation |
| Prover service | `cmd/clairveil-proverd`, `x/privacy/client/sdk/proverservice`, `x/privacy/client/sdk/provertransport` | Prover operations, JS SDK team | local/remote companion prover contract |
| ZK artifact tooling | `cmd/clairveil-setup`, `x/privacy/zk` | Core chain team, prover operations | artifact generation, checksum, preflight |
| Legacy note debug helper | `cmd/clairveil-verify` | Maintainers only | legacy SHA-256/address-seed, Base64, and raw-JSON compatibility debugging; not a current protocol contract or release acceptance surface |
| Walkthrough | `docs/clairveil-local-privacy-walkthrough.md` | Integrators | local end-to-end manual verification |
| Circuit guide | `docs/clairveil-circuits.md` | Core chain team, prover operations, security reviewers | Deposit/Spend/JoinSplit/BatchJoinSplit16x32 circuits and artifact impact explanation |
| NoteV1 and batch normative contract | `docs/clairveil-batch-joinsplit-16x32.md`, `docs/clairveil-batch-joinsplit-16x32-kr.md` | Core chain, SDK, prover, security teams | Frozen NoteV1/fixed encoding/vector/public-witness/state contracts implemented by the batch chain core |
| Batch client integration pack | `docs/clairveil-batch-transfer-integration-handoff.md`, `docs/clairveil-batch-joinsplit-localnet-tutorial.md`, `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json` | Go/JS SDK, wallet, payroll, prover, operations teams | One-proof prepare/prove/broadcast/typed-scan/reconcile contract, boundary cases, and runnable localnet handoff |
| Batch protocol independent fixtures | `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`, `x/privacy/client/sdk/conformance/testdata/privacy_batch_joinsplit_v1_contract.json` | Core chain, SDK, security teams | Independent domains/empty-root/encoding, canonical audit-key ID, vector/public-input, and corrected wire-state goldens |
| Batch tx/query proto | `proto/clairveil/privacy/v1/tx.proto`, `proto/clairveil/privacy/v1/query.proto`, `proto/clairveil/privacy/v1/genesis.proto` | Core chain, SDK, security teams | production `MsgBatchTransfer`/structured outputs, AssetRegistryV1, same-root paths, typed scan/genesis contracts |
| Batch feasibility proto | `proto/clairveil/privacy/v1/batch_feasibility.proto` | Core chain, SDK, security teams | max-shape measurement fixture only; the production contract is in the normal tx/query/genesis proto |
| CLI reference | `docs/clairveil-cli-reference.md` | Integrators, wallet/SDK teams | user-facing commands and flags |
| Testing guide | `docs/clairveil-testing-guide.md` | Maintainers, integrators | test matrix and release validation commands |
| Operations guide | `docs/clairveil-operations-guide.md` | Operators, security reviewers | node/prover/artifact/Merkle/audit operations baseline |
| Privacy accounting design note | `docs/clairveil-privacy-accounting-design-note.md` | Core chain team, security reviewers | deposit binding, amount bounds, reserve invariant, and artifact contract rationale |
| Maintainer instructions | `docs/clairveil-maintainer-instructions.md` | Maintainers | documentation and validation rules by change type |
| Integration guide | `docs/clairveil-downstream-cosmos-integration-guide.md` | Core chain team | app wiring and responsibility checklist |
| Client product brief | `docs/clairveil-client-product-brief.md` | Wallet/app product and client teams | product capability scope and client profiles |
| Client UX flows | `docs/clairveil-client-ux-flows.md` | Wallet/app product and client teams | setup, scan, transfer, withdraw, disclosure, and recovery flows |
| Client risk decisions | `docs/clairveil-client-risk-decisions.md` | Product, security, operations | storage, prover, audit, disclosure, and telemetry decisions |
| Client API checklist | `docs/clairveil-client-api-checklist.md` | Client SDK and app teams | chain/prover API, fixture, release gate, and compatibility checks |
| JS SDK handoff | `docs/clairveil-js-sdk-handoff.md` | JS SDK team, web wallet team | SDK implementation checklist |
| WebApp integration pack | `docs/clairveil-web-app-scope.md`, `docs/clairveil-web-app-integration.md`, `docs/clairveil-web-app-storage-recovery.md`, `docs/clairveil-web-app-deployment.md`, `docs/schemas/clairveil-web-client-config.schema.json` | Browser WebApp, SDK, product, and operations teams | supported single-flow boundary, typed config, encrypted persistence/recovery, API lifecycle, and deployment controls |
| Scan optimization plan | `plans/clairveil-scan-optimization-implementation-plan.md` | Core chain team, JS SDK team, web wallet team | `ScanEvents`, batch nullifier, view tag design, and excluded server-filterable/proof-bound scopes |
| Reference payroll product | `docs/clairveil-reference-payroll-product.md`, `docs/clairveil-reference-payroll-*.md`, `examples/reference-payroll` | Operators, JS SDK team, wallet teams | payroll control-plane reference product, localnet tutorial, rehearsal records, and team handoff notes |
| Release policy | `docs/clairveil-release-versioning-policy.md`, `docs/clairveil-release-note-template.md` | Maintainers, release recipients | tag, changelog, release note, compatibility impact rules |
| Prover profile | `docs/clairveil-proverd-remote-production-profile.md` | Prover operations | remote prover production controls |
| Merkle restore SOP | `docs/clairveil-merkle-restore-sop.md` | Core chain team, operators | tree state validation after snapshot/restore/migration |
| Security docs | `docs/clairveil-threat-model.md`, `docs/clairveil-security-best-practices-review.md` | Security reviewers, operators | trust boundary and residual risk review |

## 2. Pre-Release Repository Maintainer Validation

Before creating the release commit and tag, the maintainer runs:

```bash
make release-check
```

After committing the paired dated changelogs and other tracked release metadata, create an annotated exact-SemVer tag at that commit. Prepare paired external release-note drafts before tagging, but fill their exact commit, archive, checksum, and GitHub URL fields only after tag-bound artifact verification. Run `make release-pack` and `make release-pack-verify` only from that tagged commit to create and verify the final artifact.

`make release-check` runs the following steps:

```text
make ci
make vulncheck
make localnet-smoke
make privacy-e2e-smoke
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check
```

Each step means:

| Step | Meaning |
| --- | --- |
| `make ci` | Verifies documentation, Go tests, Go binary builds, and JS/TS examples. |
| `make vulncheck` | Runs the govulncheck policy gate. New actionable vulnerabilities fail the check. |
| `make localnet-smoke` | Confirms the reference daemon can init and start from genesis. |
| `make privacy-e2e-smoke` | Verifies deposit, transfer, public disclosure, recipient disclosure, sender self-view disclosure, audit disclosure, direct withdraw, and relayed withdraw on a local node. |
| `make privacy-batch-joinsplit-localnet` | Runs the default `RUN_LOCALNET=0` batch-integration fixture/conformance gate. The resource-heavy live node/prover mode remains an explicit separate rehearsal. |
| `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check` | Verifies bulk-transfer critical units, reservation invariants, synthetic capacity estimate, and the multi-message transfer localnet path. |

`make release-check` is intentionally heavy for every pull request. The default PR checks are `.github/workflows/test.yml` with `make ci` and `.github/workflows/security.yml` with `make vulncheck`; release-candidate validation is run manually with `make release-check`.

For prover Docker packaging, run:

```bash
make docker-proverd-build
```

This command validates compose config, Dockerfile build, and image inspect. It requires a Docker daemon, so it is not included in the default `release-check`.

For a final release, `make release-pack` requires a clean commit with an annotated exact-SemVer tag that points to that commit, then creates `dist/clairveil-handoff-<tag>.tar.gz` and its `.sha256` file. On an untagged clean commit, it instead creates a commit-bound `snapshot-<40-character-commit-sha>` pack solely for packaging CI and internal completeness checks; that snapshot must not be published as a release. This pack is a downstream handoff contract bundle, not a full source distribution. Its selected paths and exact required files are defined by `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt`; prose lists are not membership authority. The pack includes license/notice, current documentation and plan indexes, major handoff/security/operations/circuit/CLI/testing/maintainer knowledge, proto, JSON Schema, conformance fixtures, non-DApp client examples, reference payroll surfaces, prover Docker samples, release scripts, `RELEASE-MANIFEST.txt`, and `SHA256SUMS.txt`. Superseded plans and ignored `tmpdocs/` archives are excluded. The bilingual batch contract and independent fixtures remain normative; the batch chain core additionally makes normal tx/query/genesis proto, the fourth circuit descriptor, gas/scan/genesis contracts, and direct core tests part of the handoff. `batch_feasibility.proto` remains measurement-only. Readiness commands are run from the source checkout before handoff; the pack records contract artifacts and validation expectations, not large R1CS/PK/VK binaries.

`make release-pack-verify` verifies that a SemVer manifest version names an annotated tag whose direct target is the manifest commit and has exactly one matching valid-date heading in both changelogs, or that an untagged CI snapshot version embeds that exact full commit. It also verifies the handoff pack's external `.sha256`, canonical complete internal `SHA256SUMS.txt`, every file in the required-file manifest, exact selected Git file set, canonical manifest, safe canonical tar members, every non-generated Git blob, raw and extracted Git-derived exact `0644`/`0755` file modes, and exact `0755` directory modes. Generation canonicalizes these modes independently of the caller's umask. When `RELEASE_PACK_ARCHIVE` is not set, the verifier requires a clean worktree and uses the existing default archive/checksum pair unchanged; it generates the pair only when either file is absent. For an externally supplied release archive, set `RELEASE_PACK_ARCHIVE`, `RELEASE_PACK_CHECKSUM`, and the out-of-band lowercase 40-character SHA in `RELEASE_PACK_EXPECTED_COMMIT`; the exact commit and release tag must exist in the local clone. Explicit verification never regenerates or replaces either supplied input, and fails immediately if the archive or checksum is missing. This step checks that the tarball is not just created, but suitable to hand off as a complete release contract bundle.

```bash
RELEASE_PACK_ARCHIVE=/path/to/clairveil-handoff.tar.gz \
RELEASE_PACK_CHECKSUM=/path/to/clairveil-handoff.tar.gz.sha256 \
RELEASE_PACK_EXPECTED_COMMIT=<40-character-commit-sha> \
./scripts/release-pack-verify.sh
```

## 3. Release Maintainer Checklist

1. Update both dated changelogs and tracked release metadata for the intended exact-SemVer version, and prepare paired external release-note drafts from the authoritative templates with post-tag fields left blank.
2. Pass `make release-check`.
3. If a remote prover image will be delivered or operated, pass `make docker-proverd-build`.
4. Confirm the artifact list in `docs/clairveil-release-handoff-pack.md` matches the current repository structure.
5. Commit the release metadata and confirm `git status --short` is empty.
6. Create an annotated exact-SemVer tag at that release commit.
7. Run `make release-pack` from the tagged commit to create the final handoff tarball and checksum.
8. Run `make release-pack-verify` and confirm the version tag, manifest commit, archive checksum, internal checksums, and required files all agree.
9. Confirm `docs/schemas/clairveil-js-wallet-contract.schema.json` is validated against the latest fixtures by `make examples`.
10. Confirm `x/privacy/client/sdk/conformance/testdata` fixtures come from the same tagged release commit delivered to the downstream JS SDK team.
11. Include ZK artifact checksums and preflight mode policy in the release note.
12. If Merkle snapshot/restore/migration behavior changed, include the sample path recomputation procedure from `docs/clairveil-merkle-restore-sop.md` in the release note.
13. Ensure accepted vulnerability policy exceptions `GO-2024-2584`, `GO-2026-4479`, and `GO-2026-5932` remain listed in the release note known risks.
14. State in the release note that the downstream project owns audit master private key custody, wallet storage encryption, and remote prover topology in separate operations documents.
15. Use the authoritative `docs/clairveil-release-note-template.md` to document compatibility impact and downstream action.
16. Run `go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v` and confirm the corrected max-shape goldens: canonical owner-effect payload `65,384` bytes, Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, and query response `74,551` bytes.
17. Retain the historical feasibility result from `CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v`: `1,111,837` constraints, peak RSS `3,339,862,016` bytes, `55.892 ms/output`, and `2.789x` per-output improvement over native 2x2. This remains a feasibility gate, not trusted setup.
18. Confirm that release-pack verification requires the bilingual batch contract, normal production tx/query/genesis proto, `batch_feasibility.proto`, and both independent batch protocol contract fixtures.
19. Run the production circuit and keeper matrices: `TestBatchJoinSplit16x32ProductionPositiveMatrix`, `TestBatchJoinSplit16x32ProductionNegativeMatrix`, `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, and `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`.
20. Generate development artifacts outside git and run `TestBatchDevelopmentArtifactRoleReadinessGate`. Record R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`, generation peak RSS `3,308,797,952 B`, and readiness peak RSS `1,295,482,880 B`. Do not package these development binaries as production artifacts.
21. Run `make privacy-batch-joinsplit-localnet` for fixture conformance and `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` on a capable host for the actual 1/1, 3/4, 31+change, exact32, padding, restart tx-hash reconcile, and freshly signed spent-nullifier fail-closed smoke. The durable exact-signed-byte retry remains a payroll worker/store test contract.
22. Confirm release-pack verification requires the batch reference integration bilingual handoff/tutorial, conformance fixture, and localnet runner while excluding private prepared payloads, proofs, and development R1CS/PK/VK binaries.

## 4. Downstream Core Chain Team Acceptance Criteria

The core chain team confirms:

1. Pin the `github.com/DELIGHT-LABS/clairveil` module version or fork commit.
2. Wire the `x/privacy` module, keeper, store key, module account permissions, and tx/query command wiring into the downstream app.
3. Confirm `proto/clairveil/privacy/v1` service paths and generated types do not conflict with the downstream API gateway.
4. Decide the downstream denom, chain-id, and fee/gas policy, then document any values that differ from tutorials, fixtures, or e2e config.
5. Set the audit master public key in production-like genesis.
6. Run ZK artifact preflight in `strict` mode for release candidates and production-like nodes.
7. Write downstream EVM, policy module, and precompile integration tests separately from Clairveil repository smoke tests.
8. Start from fresh genesis with active circuit set `privacy-note-v1`; old note/tree/scan state and artifacts are incompatible and must not be migrated through a compatibility decoder.
9. Treat `AssetRegistryV1` as the authoritative one-to-one denom/32-byte asset-ID mapping, consume `privacy-scan-v2` with cursor `(height, global_sequence, output_index)`, and obtain spend paths from a snapshot matching the selected root exactly. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. Every non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Above the online bound, use the current root or a trusted local historical index. Offline recovery/export keeps the separate `MaxMerkleRebuildLeaves` (1,048,576) bound. Genesis export above that offline bound remains supported when the complete per-prefix snapshot metadata index exists.
10. Preserve the live public-input order `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`, schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`, canonical `audit_key_id`, and exact `CanonicalBatchTransferPayloadBytesV1` grammar/domain. Register `batch-joinsplit-16x32-v1` as the fourth required descriptor and never silently downgrade to a three-circuit identity.

## 5. JS/TS SDK And Web Wallet Team Acceptance Criteria

The JS/TS SDK and web wallet teams confirm:

1. Use `docs/clairveil-js-sdk-handoff.md` as the baseline document.
2. Validate fixture shape with `docs/schemas/clairveil-js-wallet-contract.schema.json`.
3. Include `x/privacy/client/sdk/conformance/testdata` fixtures in SDK CI.
4. Port payload hash recomputation, relay withdraw handoff mapping, route/version checks, scan fixture checks, note reservation status/transition/evidence checks, and prefix checks from `examples/js-sdk-fixture-validator` into SDK tests.
5. Implement `ScanEvents` cursor sync, empty-page/`has_more` handling, `CheckNullifiers` batch spent refresh, transfer payload `v5`/owner intent/`view_tag_hexes`, final `MsgTransfer.view_tags`, and safe view-tag mismatch fallback before release.
6. Reflect timeout, bearer auth, and payload hash equality checks from `examples/js-sdk-prover-http-client` in the prover adapter implementation.
7. Treat wallet note cache, root seed derived secrets, viewing keys, disclosure keys, and prepared payload/proof JSON as privacy-sensitive data; do not leave them in plaintext browser storage.
8. If using a remote prover, reflect prover-visible metadata and trust boundaries in the user privacy UX and threat model.
9. Implement `privacy-fixed-v1` exactly: 350-byte note plaintext, 392-byte disclosure plaintext, and a 20-byte typed envelope header. Reject raw ciphertext, legacy JSON plaintext, wrong kinds, and trailing bytes without a compatibility fallback.
10. Keep prepared transfer payload `v5` as the outer prepared-payload version; it is not the note/disclosure encoding version and must not be renamed to `privacy-fixed-v1`.
11. Resolve asset IDs through `AssetRegistryV1` and persist the full `privacy-scan-v2` cursor. Reject typed scan records with a wrong event type, fixed-envelope kind, digest, key, sentinel, or orphan/non-adjacent output. Use same-root path snapshots and account for the privacy leak and rebuild cap of remote historical-root/path queries.
12. Pin ClairveilJS 0.3.1 and verify its `privacy-fixed-v1` fixtures and V5/V2 transfer/withdraw contracts. The unreleased example supports fresh initialization in its current v0.3.1 namespaces only: it defines no earlier browser-cache or lifecycle-store migration and no in-place downgrade. Start empty and complete a typed rescan instead of adding a compatibility decoder.
13. The batch integration provides the public Go `MsgBatchTransfer` SDK, wallet scanner/decrypt path, one-proof payroll workflow, batch CLI/tutorial, and ClairveilJS 0.3.1 Cosmos/EVM batch APIs. The example WebApp exposes its batch editor only behind the explicit server feature gate and requires encrypted prepared-artifact recovery, wallet confirmation, and typed input/item/audit reconciliation; SDK or prover-route availability alone is not product capability discovery.
14. For EVM, validate `evmNativeDenom`, `payable-exact-value` deposit binding, the canonical precompile address, receipt/finality evidence, and the indexed relationship between the outer scan tx hash and Ethereum tx hash. Treat optional EIP-712 authorization, contract/privacy-state adapters, and custom finality policies as profile-scoped reviewed dependencies and test them end to end.

## 6. Prover Operations Team Acceptance Criteria

The prover operations team confirms:

1. Use `docs/clairveil-proverd-remote-production-profile.md` as the baseline document.
2. Decide whether the remote prover topology is a public service, private sidecar, local daemon, or browser/WASM prover.
3. For remote deployment, define TLS/mTLS, auth, quota, rate limit, body limit, timeout, redacted logging, and health/readiness exposure policy.
4. Run the prover artifact directory read-only and treat checksum mismatch as a release blocker.
5. Preserve `payload_hash` equality checks on both the SDK and server sides for proof requests/responses.
6. Use the role-aware artifact registry: validators load required VKs after exact consensus identity verification, while provers lazily load selected R1CS/PK pairs.
7. Enforce per-circuit admission defaults of one in-flight and four queued jobs and a positive 8 MiB body limit. A zero body limit is invalid. Keep automatic prover failover disabled, and use process isolation if hard cancellation or memory containment is required.
8. The reference batch prover exposes bounded `POST /v1/proofs/batch-transfer` and advertises `batch-joinsplit-16x32-v1`. Preserve per-circuit admission, positive body limits, payload binding, TLS/auth, privacy, and artifact-role boundaries; do not mount an ad-hoc handler.

## 7. Known Risk And Accepted Exceptions

Release recipients must know the following risks.

| Item | Status | Recipient Action |
| --- | --- | --- |
| `GO-2024-2584` | Accepted in the repository `govulncheck` policy as a Cosmos SDK no-fixed-version actionable finding | Reassess in the downstream production risk register and realign dependencies if an upstream fixed path becomes available. |
| `GO-2026-4479` | Accepted in the repository `govulncheck` policy for the pion/dtls v2 no-fixed-version actionable finding reachable through the Cosmos SDK/CometBFT server stack | Reassess in the downstream production risk register and realign dependencies if an upstream fixed path becomes available. |
| `GO-2026-5932` | Accepted narrowly because Cosmos SDK reaches `x/crypto/openpgp/armor` only for local ASCII key armor; Clairveil does not use OpenPGP signing or encryption, and no fixed upstream version exists | Reassess in the downstream production risk register and remove the exception as soon as a fixed dependency path becomes available. |
| Audit master private key custody | Clairveil provides public key config and disclosure decode flow only | Downstream owns HSM/KMS, access control, rotation, and incident response. |
| Wallet local storage | The reference CLI uses `0600` plaintext JSON | Web wallets and production wallets must implement encrypted storage and telemetry redaction. |
| Remote prover metadata exposure | A remote prover can see proof input metadata | Include the remote prover as a trusted component in user privacy UX and the deployment threat model. |
| ZK artifact provenance | The repository provides checksum/preflight tooling, but ceremony and release-signing policy are downstream responsibilities | Production releases should define artifact signing, provenance, and reproducibility policy separately. |
| Batch chain-core/client-integration boundary | Circuit, `MsgBatchTransfer`, keeper, deterministic gas, typed scan/minimal event, genesis, reference Go SDK/prover/scanner/payroll/CLI, ClairveilJS 0.3.1 Cosmos/EVM APIs, and the feature-gated example flow are implemented; formal setup, production artifact delivery, and target-chain product acceptance remain separate | Keep the batch gate off until the selected chain, prover, encrypted recovery, wallet confirmation, typed reconciliation, and Cosmos/EVM product E2E pass together. |
| ClairveilJS 0.3.1 compatibility | The package implements `privacy-fixed-v1` and the V5/V2 prepared-payload/proof contracts; the unreleased example has no legacy browser-lifecycle migration or in-place downgrade contract | Start a fresh current cache/reservation/relay namespace, run a full typed rescan, and never add a compatibility decoder. Prepared transfer payload `v5` remains a separate outer version. |
| Prover cancellation boundary | Canceling a request does not preempt an already running in-process solver; its permit and memory may remain in use until return | Bound admission to `1`/`4` with positive `8 MiB` requests and use supervised worker-process isolation for hard cancellation or OOM containment. |
| Historical path rebuild boundary | Current-root paths use incremental nodes. Public non-current queries require complete root/count/height metadata, admit at most 1,024 leaves and two concurrent rebuilds per keeper, and otherwise return `ResourceExhausted`; offline recovery/export retains `MaxMerkleRebuildLeaves` (1,048,576). | Above the online bound, spend against the current root or use a trusted local historical-path index. Preserve the complete snapshot metadata index so large-tree genesis export stays available. |

## 8. Handoff Completion Criteria

Release handoff is complete when:

1. The maintainer passed `make release-check`.
2. The maintainer delivered an archive/checksum that passed `make release-pack` and `make release-pack-verify`.
3. The core chain team confirmed the downstream app import/fork commit and module wiring plan.
4. The JS/TS SDK team imported fixtures and JSON Schema into its own CI.
5. The web wallet team reflected wallet storage encryption and prover topology in its design document.
6. The prover operations team selected the remote/local prover production profile.
7. The security/operations team recorded accepted vulnerabilities, audit key custody, and ZK artifact provenance in the risk register.
8. All teams accepted the fresh-genesis `privacy-note-v1` / `privacy-fixed-v1` compatibility break, verified both independent batch protocol contract fixtures, and recorded `batch-joinsplit-16x32-v1` as the fourth required production chain-core circuit.
9. All teams recorded that the Go reference, ClairveilJS 0.3.1 Cosmos/EVM batch surfaces, and feature-gated example flow are implemented, while target-chain product acceptance, formal trusted setup, and production artifact distribution remain incomplete.

This document is not a replacement for a release package archive. It is a handoff index that lets teams start integration from the same release commit, fixtures, schema, and verification commands.
