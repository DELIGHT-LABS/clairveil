# Clairveil Operations Guide

This document lists the operational decisions downstream projects must make when adopting Clairveil. The repository is a reusable privacy core and reference host, not a production chain.

Korean version: [clairveil-operations-guide-kr.md](clairveil-operations-guide-kr.md)

## 1. Responsibility Boundary

| Area | Clairveil repo | Downstream project |
| --- | --- | --- |
| Privacy module | `x/privacy` implementation and reference app | app wiring, store keys, module accounts, governance/policy integration |
| Reference node | local validation with `clairveild` | validator operations, sentry, snapshots, upgrades, monitoring |
| ZK artifacts | generation/validation tooling | artifact signing, provenance, reproducible build, release custody |
| Prover | `clairveil-proverd` reference service | topology, auth, quota, deployment, logging, retention |
| Audit disclosure | genesis pubkey and decode flow | master auditor private-key custody, rotation, access control |
| Wallet | CLI/SDK helpers and fixtures | browser/mobile storage encryption, UX, telemetry redaction |

## 2. Node Operations Baseline

A production-like node must have an audit master pubkey in genesis, run ZK artifact preflight in `strict` mode, register the privacy module account as a bank module account, and expose `tree_state`, `commitment_info`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `reserve/{denom=**}`, `assets/by_denom/{canonical_denom=**}`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, and batch `nullifiers` queries. Complete a snapshot/restore rehearsal before release.

Enable `Msg/BatchTransfer` only with the four-circuit `privacy-note-v1` consensus identity and matching local batch VK.

```bash
set -a
source artifacts/privacy/privacy_zk_checksums.env
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict

clairveild start --minimum-gas-prices 0uclair
```

## 3. ZK Artifact Operations

`clairveil-setup` generates R1CS/PK/VK and checksum manifests.

```bash
clairveil-setup --out artifacts/privacy
```

Selective development rotation requires an already complete, checksum-valid set and explicit overwrite. For the `DISCLOSURE-BLINDING-SEPARATION` JoinSplit relation change:

```bash
clairveil-setup --out artifacts/privacy --circuit joinsplit --overwrite
```

The current JoinSplit development identity is R1CS `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, PK `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, and VK/consensus identity `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`. After rotation, discard old JoinSplit proofs/jobs, install the exact manifest through fresh genesis/reset, and require strict preflight. Do not mix old and new consensus/file identities or rotate Batch for this change.

`privacy-note-v1` requires descriptors in the exact order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`. Validators compare the consensus identity and load only the four VKs; prover readiness lazily loads only its selected R1CS/PK pair. `privacy_zk_manifest.json` schema `v2` must match `CircuitSetIdentity` schema `v1`, including ordered descriptors, VK SHA-256, and public-input schema SHA-256. Environment checksums add a consistency check but cannot override consensus identity; any mismatch must fail startup/readiness.

Repository artifacts are development artifacts, not a formal trusted setup or production distribution. Production releases must record the circuit source commit, generation command, checksum manifest, and signer; mount artifacts read-only; use `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict`; and block stale artifacts or chain-verifier mismatches. The recorded batch artifact hashes and resource history remain in [clairveil-batch-joinsplit-16x32.md](clairveil-batch-joinsplit-16x32.md).

## 4. Merkle Tree Operations

The privacy pool is a depth-32 single Merkle tree.

| Transaction | Leaf change |
| --- | --- |
| deposit | +1 |
| native 2x2 transfer | +2 |
| batch transfer | +1..32 |
| withdraw | +0 |

Track `leaf_count`, `max_leaves`, `remaining_leaves`, the current root, and historical-root retention.

| Usage | Meaning |
| --- | --- |
| 50% | Start tracking the long-term capacity trend. |
| 70% | Start discussing a new pool/circuit upgrade. |
| 85% | Finalize the upgrade plan. |
| 95% | Prepare a migration window or limit large inflows. |

## 5. Merkle Restore Validation

After a snapshot, restore, or migration, restore `Leaf/*`, `MerkleNode/*`, `CommitmentIndex/*`, `HistoricalRoot/*`, cached root, and `leaf_count` from one consistent height. A matching cached root alone is insufficient when a leaf or lower node is missing.

Query `tree_state` and confirm `leaf_count`, `max_leaves`, `remaining_leaves`, and `root`. Choose at least one old and one recently appended commitment. For each, require `merkle_path` to return `path` and `path_helper`, then recompute the root off-chain from the commitment bytes, `path`, and `path_helper`; it must equal `tree_state.root`. Comparing `merkle_path.root` alone is insufficient.

`TreeState` intentionally does not scan every lower `MerkleNode/*` on each query. Sampled `MerklePath` recomputation and required-node checks during append/write cover lower-node integrity. Restore completion therefore requires the successful recomputations, not only a successful `TreeState` query. If a tree above `MaxMerkleRebuildLeaves` lacks a cached root, the keeper intentionally does not auto-recover it; use an offline rebuild, revalidate the state restore, or execute a separate migration plan.

## 6. Prover Operations

`clairveil-proverd` receives prepared proof payloads rather than private seeds, but those payloads can contain amounts, note randomness, Merkle paths and roots, nullifiers, shielded public keys, disclosure payload/ciphertext, timing, and client-identity metadata. It is a privacy-sensitive trusted component, not a generic CPU worker. Never retain request/response bodies or private witness material.

### Remote Topology And Disclosure Boundary

| Topology | Appropriate use | Downstream decision |
| --- | --- | --- |
| Browser/WASM prover | User must not disclose privacy metadata outside the browser. | SDK and artifact delivery. |
| Local daemon | Development, desktop wallet, or high-trust workstation. | Installer, lifecycle, local auth. |
| Private remote sidecar | Company-controlled wallet backend. | mTLS, private network, retention. |
| Public remote prover | General web-wallet UX. | Strong auth, quota, monitoring. |

Use one configured prover endpoint and disable automatic failover. A same-endpoint retry after timeout/response checks is allowed. Sending a witness-bearing request to another endpoint requires explicit user or product-policy opt-in that names the additional operator and privacy boundary; availability alone does not authorize disclosure expansion.

Current contracts are transfer payload `v5` with request/response/proof `v2`; withdraw prover/final payload and request/response/proof `v2`; batch payload `batch-transfer-payload-v1`, proof `batch-transfer-proof-v1`, and request/response `v1`; deposit payload/proof/request/response `v1`; and disclosure plaintext/query `privacy-fixed-v1`. Reject legacy inputs. Exclude request bodies, bearer credentials, signatures, disclosure plaintext/blindings, and proofs from logs, traces, crash dumps, and analytics.

### Production HTTP Boundary

Put a remote prover behind a private network or edge proxy. Non-loopback witness traffic must use HTTPS; TLS may terminate at an edge proxy, load balancer, service mesh, or mTLS. Protect proof routes with bearer token, mTLS identity, session-bound API token, or equivalent, and apply quotas by user, wallet, IP, and API token. Bearer tokens need at least 128 bits of random entropy, must come from a secret manager or equivalent injection path, and must have a documented rotation procedure.

Set aligned edge and application body limits. `max_request_bytes=8388608` (8 MiB) is the reference default and must be positive: `0` is invalid and never means unlimited. Keep both gzip wire and decompressed-body limits, configure read-header/read/idle/write timeout policy, and limit workers and queue depth. For long synchronous proofs, benchmark a finite write timeout or return an async job id. Keep `/healthz`, `/readyz`, and `/debug/vars` on loopback, a private network, or an authenticated operations plane.

`x/privacy/client/sdk/provertransport.HTTPHandler` is a low-level transport handler. Although it applies the positive request limit before admission, it lacks production bearer authorization, gzip dual limits, readiness policy, and server timeouts. Expose the bounded `x/privacy/client/sdk/proverservice.Handler`, or a wrapper that preserves the raw limit and adds authorization, outer `http.MaxBytesReader`, timeouts, and operating policy. A proxy body limit alone is insufficient.

### Runtime, Readiness, And Admission

`proverservice.DefaultRuntimeInfo()` describes the complete four-route reference daemon. `/healthz` and `/readyz` derive advertised `routes` and `circuits` from non-nil configured provers, so a compatibility constructor with a partial `provertransport.ProverSet` must not advertise unconfigured routes. `NewReferenceHandler` configures the complete reference inventory. Readiness must fail closed unless the manifest, VK, public-input schema, and supplied consensus metadata exactly match the chain.

The reference admission defaults are per circuit: `max_in_flight=1`, `max_queued=4`. Queue saturation returns a retryable busy response. Export in-flight, queued, rejected, canceled, queue-wait, prove-time, CPU, RSS, route/status, latency, auth failures, body-limit rejections, and readiness/preflight failures without request or witness content.

Context cancellation ends the caller wait, but a running in-process gnark proof can continue until it returns and retains its permit; the reference service cannot preempt the solver. For hard cancellation or OOM containment, use isolated, memory-limited worker processes and terminate them.

The bounded reference service exposes BatchJoinSplit16x32 only at `POST /v1/proofs/batch-transfer`. Do not send `MsgBatchTransfer` witnesses through a generic or JoinSplit endpoint. Keep the same TLS/auth, positive body limit, per-circuit admission, payload binding, and artifact-role controls. The production 16x32 public-schema order is `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`; it does not authorize an ad-hoc remote endpoint.

### Canonical Deposit Prover Route

Expose `POST /v1/prover/deposit` only through the bounded service handler and include its artifact in readiness. It receives the versioned witness (receiver public keys, amount, asset ID, randomness, commitment), not encrypted note, creator, denom, memo, seed, or chain ID. Apply bearer auth, positive gzip/body limits, per-circuit admission, redacted logging, and `Cache-Control: no-store`. Send `Content-Type: application/json`; omission remains accepted for existing `v1` clients, while an unsupported supplied type returns `415`. Invalid witness/version returns `400`; only a failure after a validated request reaches proving returns `500`; `405` returns `Allow: POST`.

The JS SDK must use a request timeout and validate response version plus route-specific binding: payload/proof hashes for transfer, withdraw, and batch; recomputed witness commitment and nested proof version for deposit. The authoritative route contract is [the HTTP API deposit section](clairveil-proverd-http-api.md#deposit).

## 7. Audit Key Operations

Every transfer includes mandatory audit disclosure. The audit master private key can therefore read from/to/amount/asset information for every shielded transfer. Production requires a key-generation ceremony, HSM/KMS or equivalent custody, separated decrypt permissions, access logs and approval workflow, a rotation/migration plan, compromised-key incident response, and auditor UX that enforces disclosure verification. Clairveil does not implement private-key custody.

## 8. Wallet Operations

The reference CLI stores local JSON files with restrictive permissions; this is a development baseline, not production wallet storage. Production wallets must choose root-seed and derived-secret encryption, viewing-key policy, note-cache encryption, prepared payload/proof JSON retention, telemetry redaction, remote-prover trust-boundary UX, and disclosure-decode verification display.

## 9. Monitoring

Monitor transaction counts by type; batch input/output counts, deterministic precharge, out-of-gas rejection, and atomic rollback errors; disclosure-mode distribution; proof latency/error rate; nullifier rejections; Merkle `leaf_count`, usage, and failed `merkle_path` queries; `reserve` `invariant_holds=false`; artifact preflight; and remote-prover auth/body-limit failures. Retain the route/status, queue, cancellation, prove-time, CPU, RSS, and readiness signals described in section 6.

Redact private seeds, mnemonics, scalars, viewing keys, disclosure private keys, prepared payloads, proof bytes, bearer tokens, and decrypted disclosures from every log, trace, crash dump, and analytics sink.

## 10. Release Gates And Production Capacity Evidence

| Gate | Evidence provided | Boundary |
| --- | --- | --- |
| `make privacy-batch-joinsplit-localnet` | Static BatchJoinSplit16x32 fixture/conformance. | It starts neither node nor prover and produces no actual proof. |
| `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` | Actual one-proof node/prover functional workflow. | It is not a production-capacity measurement. |
| `reference-payroll-*` | Legacy multi-message/simulation regression and capacity planning. | It is not evidence of one-proof production capacity. |
| `make release-check` | `ci`, `vulncheck`, `localnet-smoke`, `privacy-e2e-smoke`, the static batch gate, and `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`. | It does not run the actual one-proof batch gate, any `reference-payroll-*` target, or a 16x32 production-capacity workload. |

Final downstream release and mainnet acceptance separately require live one-proof evidence from `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`. A production-capacity claim requires a tag/commit-bound artifact from an actual 16x32 workload recording proof/sec, tx/sec, item/sec, RSS, CPU, shape distribution, retry/replan/manual-review outcomes, circuit/artifact identity, checksums, and execution environment. Do not relabel synthetic or legacy `reference-payroll-*` results as one-proof production-capacity evidence.

## 11. Release Operations

Before the release commit and tag:

```bash
make release-check
```

After creating the annotated exact-SemVer tag at that commit:

```bash
make release-pack
make release-pack-verify
```

Build the reference prover image with `make docker-proverd-build`. Release notes must cover proto/fixture/schema/CLI/prover contract impact, ZK artifact impact, accepted vulnerabilities, downstream action, artifact checksum/provenance policy, and circuit-set/public-witness/gas/scan-schema versions (`privacy-note-v1`, `BatchGasModelV1`, `privacy-sequence-v1`, `privacy-scan-v2`).

## 12. Incident Response Criteria

| Situation | Response |
| --- | --- |
| audit key compromise | Stop disclosure access, execute the rotation/migration plan, and estimate affected disclosure scope. |
| prover token leak | Rotate the token, review access logs, and check proof-endpoint abuse. |
| artifact checksum mismatch | Stop node/prover start, revalidate artifact source, and treat it as a release blocker. |
| reserve invariant mismatch | Pause release/rollout; compare module-account balance with deposit/withdraw totals; investigate direct sends, top-ups, or migration writes. |
| Merkle restore mismatch | Do not resume the node; rebuild offline or retry the restore. |
| wallet cache corruption | Back up the cache, rescan, and verify user seed/key preservation. |

## 13. Minimum Mainnet Gate

Before attaching Clairveil core to downstream mainnet:

1. Downstream app e2e passes deposit/transfer/disclosure/withdraw.
2. JS/web wallet passes conformance fixtures and live-chain tests.
3. Remote/local/browser prover topology is decided.
4. Audit-key custody is documented and rehearsed.
5. Artifact signing/provenance policy exists.
6. Snapshot/restore rehearsal and Merkle-path sample validation are complete.
7. `reserve/{denom=**}` returns `invariant_holds=true` after deposit/withdraw e2e for each supported denom.
8. A chain-specific threat model is written.
9. `TestBatchTransferDirectCoreIntegration`, atomic scan-failure tests, and cross-message 2x2+batch/batch+batch rollback tests pass against the release commit.
10. The SDK, remote batch prover route, typed scanner/decrypt path, one-proof payroll integration, CLI/tutorial, conformance fixture, and actual localnet workflow pass together; formal setup, production artifact release, external audit, and downstream wallet products remain separate gates.
11. A tag/commit-bound live one-proof record from `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` is accepted, and every production-capacity claim meets section 10.
