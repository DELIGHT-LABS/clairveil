# Clairveil Security Best Practices Review

This document is the current review of the Clairveil repo from a security-best-practices perspective. Clairveil provides a reusable privacy module and reference host, so this document separates the safe defaults that core/reusable code should provide from the operational security that downstream production projects must supply.

Korean version: [clairveil-security-best-practices-review-kr.md](clairveil-security-best-practices-review-kr.md)

## 1. Areas In Good Shape

| Area | Status |
| --- | --- |
| On-chain tx validation | Validates major fields such as roots, nullifiers, commitments, and disclosure digests as canonical field bytes. |
| Double-spend protection | Both transfer and withdraw reject nullifier reuse before state mutation. |
| Mandatory audit disclosure | Transfer passes only when the chain audit master pubkey matches the message audit disclosure target pubkey. |
| Merkle safety | Fixed-capacity guard, rebuild bound, missing leaf/node explicit failure, and query/path error propagation are implemented. |
| Prepared payload integrity | `TransferIntentV2`/`SpendIntentV2` bind chain, expiry, recipient/output/disclosure effects; transfer uses one final owner signature and creator replacement remains relayer-safe. |
| Scan hint safety | Transfer view tags are validated for shape and treated as untrusted hints; safe default scan can full-decrypt on mismatch. |
| File permission | Local wallet cache and prepared/proof JSON files are written with `0600`. |
| Prover service basics | Request body limit, read header/read timeout, idle timeout, optional bearer auth, and readiness preflight exist. |
| ZK artifact verification | Consensus pins exact ordered VK and public-input schema hashes; local verifier mismatch blocks startup/readiness and env checksums cannot override it. |
| Proof verification cost | Canonical proof framing is checked cheaply, then fixed gas is precharged before decode, VK load, or cryptographic verification. |
| Batch chain core | `MsgBatchTransfer` re-derives the frozen 12-field witness, precharges every bounded resource category, verifies before mutation, and atomically commits globally unique nullifiers/commitments plus typed scan state. |
| Conformance fixture | Query, payload hash, and prover HTTP contract fixtures exist for JS SDK/external wallets. |

## 2. Production Decisions Required Before Launch

### 2.1 Remote Prover Exposure Policy

`clairveil-proverd` is a reference service that supports both local daemon and remote sidecar modes. The default compose sample binds the host port to `127.0.0.1`, but the Dockerfile command listens on `0.0.0.0:8080` inside the container. If downstream exposes the image unchanged to an external network, the remote proof API can become reachable.

A production remote prover needs:

- TLS termination or mTLS
- mandatory authentication
- sufficiently strong service credential or session policy instead of a weak bearer token
- IP allowlist or private network
- per-user/per-wallet quota and rate limit
- proof latency and error rate monitoring
- request/response body logging disabled or strongly redacted
- read-only mount for proving artifact directory

### 2.2 Prover Timeout And DoS Boundary

The current service sets read-header/read-body/idle timeouts and request body limits. However, proof generation can take a long time, so the default `WriteTimeout` is `0`. This is practical for local daemons, but for public remote services, long-running requests become a DoS surface by occupying workers for a long time.

Remote deployment must choose one of these paths.

- If keeping synchronous HTTP, define write timeout, concurrency limit, and queue limit.
- If using async proof jobs, return only a job id from the request.

### 2.3 Prover Payload Privacy

A remote prover does not directly receive private seeds, but prepared payloads include note amount, randomness, Merkle path, nullifier, shielded pubkeys, disclosure payloads, and related metadata. Therefore a remote prover is a privacy-sensitive service, not just a CPU service.

Recommended defaults:

- development/high-trust environment: local daemon
- user-privacy-first wallet: local daemon or browser/WASM proving
- operations-convenience-first wallet: remote prover is possible, but include it as a trusted component in the threat model

`ProverPool` sends a witness-bearing request to one endpoint only by default. Automatic failover is disabled because sending the same payload to another operator expands the privacy boundary. Multi-endpoint failover must be an explicit user or product-policy opt-in, with a clear warning and endpoint set.

Even after outputs, disclosure envelopes, ciphertexts, chain, and expiry are immutable under the owner intent, the prepared prover payload still contains private note witness data. Treat the payload as authority-equivalent privacy-sensitive material; do not log it, include it in crash reports, or retain it beyond the proof workflow.

### 2.4 Wallet Storage Encryption

The current reference CLI stores local wallet JSON with `0600`. This is practical enough for development and the sample chain, but it is plaintext at rest by production wallet standards.

A web wallet/JS SDK must choose one of:

- browser secure storage plus user password-derived encryption key
- OS keychain/secure enclave integration
- hardware wallet or external signer integration
- KMS/HSM-based envelope encryption for server-side wallets

### 2.5 Master Auditor Key Custody

The Clairveil repo provides the flow for putting the audit master public key in genesis/config and decoding disclosure. Audit master private key custody is the responsibility of the downstream production project. If this key leaks, all mandatory audit disclosures become readable.

Production needs:

- private key generation ceremony
- HSM/KMS or equivalent custody
- separated operator permissions
- break-glass procedure
- key rotation/migration plan
- decrypt-operation audit log
- incident response plan

### 2.6 ZK Artifact Provenance

Checksum verification helps catch file corruption and simple tampering. In production, the project must also prove which circuit source and process produced the artifacts.

Required work:

- pin artifact generation command
- record circuit source commit hash
- sign manifest
- document trusted setup/proving key provenance
- make strict preflight the default
- verify release artifact checksums in CI

The active identity is `privacy-note-v1`. `privacy_zk_manifest.json` schema `v2` must match the genesis/state `CircuitSetIdentity` schema `v1`, including the required order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`, exact VK SHA-256 values, and public-input schema SHA-256 values. Validators need VK files only; a prover lazily loads R1CS/PK for proving. Repository-generated artifacts are development-only and do not constitute a formal trusted setup, signed production release, or external audit.

## 3. Recommended Repo-Level Improvements

| Priority | Item | Reason |
| --- | --- | --- |
| P1 | Link downstream security gates with the release checklist | External users must clearly understand that this repo is sample/reference. |
| P1 | Maintain the remote prover production profile | The local sample is safe, but downstream teams can easily miss auth/rate-limit/TLS/queue policy for remote operations. |
| P2 | Keep explicit timeout in the HTTP prover client example | Prevent SDK consumers from writing remote prover clients with no timeout. |
| P2 | Document that `provertransport.HTTPHandler` must not be directly exposed | Body limits are applied in the `proverservice.Handler` wrapper. Directly attaching the raw transport handler to a public server can lose the body limit. |
| P2 | Emphasize wallet storage encryption requirements in JS SDK handoff | Current file permission is only for the reference CLI; web wallet security is separate. |
| P3 | Docker image digest pinning/SBOM/vuln scan policy | The reference image is for behavior validation, and downstream must define production supply-chain policy. |
| P3 | Health/readiness route exposure policy | Convenient for local samples, but a metadata/probing surface remotely. |

The repository currently configures `.github/workflows/security.yml` to run `make vulncheck`. This baseline checks Go dependency and standard-library reachable paths with `govulncheck` and pins the patched Go `1.25.12` toolchain baseline. `GO-2024-2584`, the `pion/dtls` v2 path of `GO-2026-4479`, and `GO-2026-5932` are narrowly tracked no-fixed-version exceptions. The last is reachable only because Cosmos SDK uses `x/crypto/openpgp/armor` for local ASCII key armor; Clairveil does not use OpenPGP signing or encryption. A fixed version immediately invalidates each exception. Downstream projects must re-evaluate these risks and add image scan, SBOM, secret scan, and artifact-signing checks.

## 4. Current Code-Level Notes

The Session 1 remediation closed the known current duplicate-input/output, intent-substitution, replay, disclosure-oracle, decoder, failover-default, genesis/artifact-identity, and proof-gas issues. No unresolved Critical/High finding remains in that scope. These points can still become issues if downstream SDK/service implementers misunderstand them.

- The body limit in `x/privacy/client/sdk/proverservice/service.go` applies only to proof routes. This is intentional, but downstream must separately decide whether health/readiness should be externally exposed.
- The raw `HTTPHandler` in `x/privacy/client/sdk/provertransport/http.go` reads body with `io.ReadAll`. Public services must use `proverservice.Handler` or a separate `MaxBytesReader` wrapper.
- `cmd/clairveil-proverd/main.go` runs with `auth_enabled=false` when the bearer token env is empty. This is convenient locally, but must be forbidden for remote services.
- `build/clairveil-proverd/compose.yaml` limits host bind to `127.0.0.1`. However, the Dockerfile itself listens on `0.0.0.0:8080`, so downstream compose/k8s manifests must re-check network policy.
- Prepared payload JSON and wallet JSON are stored with `0600`, but they are not encrypted. Production wallets need an encryption layer.
- Transfer/prover contract versions are intentionally breaking: transfer payload `v5`, transfer proof/request/response `v2`, withdraw prover/final payload and proof/request/response `v2`, and disclosure plaintext/query version `privacy-fixed-v1`. Legacy payloads must be regenerated, not replayed or decoded through a compatibility path.

## 5. Minimum Guidance To Downstream Developers

JS/TS SDK, web wallet, and downstream Cosmos SDK chain developers should receive at least this guidance.

1. The Clairveil repo is not a production chain. It is a reusable privacy core and reference host.
2. `clairveild` is a sample chain, and downstream chains must integrate it with their own app, policy, EVM/precompile, genesis, denom, and prefix.
3. Proving can be local, remote, or browser-based, but a remote prover is a privacy-sensitive trusted service.
4. Every transfer must include mandatory audit disclosure, and the audit master pubkey is configured by downstream genesis/config.
5. Audit master private key custody is downstream responsibility.
6. Wallet local storage must be encrypted in production.
7. Disclosure plaintext must not be trusted just because it decrypted; digest verification must pass.
8. Production artifacts need provenance and signing policy in addition to checksum.
9. After snapshot/restore/migration, recompute sample Merkle paths according to `docs/clairveil-merkle-restore-sop.md`.
10. Reject legacy prepared payloads, preserve the exact `SpendIntentV2`/`TransferIntentV2` public-input order, and reset cached proof jobs/artifacts when adopting `privacy-note-v1`.

## 6. NoteV1 And Session 3A Core Security Addendum

The current production circuits and state now share the `privacy-note-v1` NoteV1 commitment/nullifier/tree contract and canonical key validation. Canonical note, disclosure, and encrypted-envelope bytes are versioned `privacy-fixed-v1`; raw ciphertext, JSON plaintext, wrong envelope kind, non-canonical field/key data, non-zero reserved bytes, and trailing bytes must fail closed. `AssetRegistryV1` is the consensus-authoritative one-to-one denom/32-byte asset-ID mapping. Global commitment uniqueness is consensus state, not an SDK-only precheck.

This contract is intentionally incompatible with earlier state and artifacts. Use fresh genesis, remove wallet note/scan caches and prepared/proof jobs, regenerate the exact `privacy-note-v1` artifact set, and rescan. Do not add a permissive compatibility decoder or in-place migration. Unified scan order is `(height, global_sequence, output_index)` and a spend witness must use a path snapshot for the exact public root. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical path/root queries can disclose wallet interest, so retain that privacy warning and use privacy-preserving infrastructure when required.

`BatchJoinSplit16x32` is the fourth required production circuit, and `MsgBatchTransfer`/`BatchTransferOutput` plus the keeper handler are implemented. The circuit preserves capacities 16/32, active-prefix/zero-disabled rules, independent membership, owner/key constraints, active-only distinctness, value conservation, vector formulas, per-output independent user/full disclosure blindings, one owner signature, and the public-input order `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. Its schema SHA-256 is `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`.

The keeper permits only cheap bounded framing before `BatchGasModelV1` precharge. Canonical effect validation, audit-identity checks, global nullifier/commitment lookups, historical-root/capacity checks, public-witness derivation, VK load, and proof verification occur after precharge. Only a successful proof reaches the cache-context transition, and nullifiers, commitments, root snapshot, `privacy-scan-v2` records, and the minimal event commit atomically. `TestBatchTransferCoreRejectionsAndAtomicScanFailure` and `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache` cover internal and cross-message rollback.

The measured development batch artifact identity is R1CS `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, and VK `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`. These checksums do not replace artifact signing, provenance, reproducible generation, formal setup, or external review.

Session 3B user-facing surfaces remain absent: no public batch Go SDK, remote batch HTTP prover route, wallet scanner/decrypt UX, one-proof payroll workflow, or batch CLI/tutorial is shipped. Do not confuse the existing multi-message `transfer-batch`/payroll workflow with `MsgBatchTransfer`, and do not expose an unreviewed witness-bearing route.

Artifact access and proving must remain bounded:

- Validators load requested VKs only after exact consensus identity comparison; provers lazily load selected R1CS/PK pairs. Any mismatch fails readiness.
- Reference admission defaults are one in-flight and four queued jobs per circuit, and request bodies are limited to a positive 8 MiB. Zero is invalid.
- Public deployments use the bounded `proverservice.Handler`, never the raw transport handler. Automatic prover failover remains disabled.
- Context cancellation does not preempt an already running in-process solver; it may retain memory and its admission permit until return. Use supervised, memory-limited worker processes when hard cancellation or OOM containment is a security requirement.
