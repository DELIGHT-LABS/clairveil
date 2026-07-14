# Clairveil Operations Guide

This document lists operational items downstream projects must not miss when adopting Clairveil.

The Clairveil repository itself is not a production chain. It provides a reusable privacy core and reference host. This document separates the baseline provided by Clairveil from decisions owned by downstream operations teams.

Korean version: [clairveil-operations-guide-kr.md](clairveil-operations-guide-kr.md)

## 1. Responsibility Boundary

| Area | Clairveil repo | Downstream project |
| --- | --- | --- |
| Privacy module | `x/privacy` implementation and reference app | app wiring, store keys, module accounts, governance/policy integration |
| Reference node | local validation with `clairveild` | validator operations, sentry, snapshots, upgrades, monitoring |
| ZK artifacts | generation/validation tooling | artifact signing, provenance, reproducible build, release custody |
| Prover | `clairveil-proverd` reference service | topology, auth, quota, deployment, logging, retention |
| Audit disclosure | genesis pubkey and decode flow | master auditor private key custody, rotation, access control |
| Wallet | CLI/SDK helpers and fixtures | browser/mobile storage encryption, UX, telemetry redaction |

## 2. Node Operations Baseline

A production-like node should satisfy at least:

1. genesis has an audit master pubkey.
2. ZK artifact preflight runs in `strict` mode.
3. privacy module account is registered correctly as a bank module account.
4. `tree_state`, `commitment_info`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, and batch `nullifiers` queries are exposed.
5. snapshot/restore rehearsal is completed before release.
6. `Msg/BatchTransfer` is enabled only with the four-circuit `privacy-note-v1` consensus identity and matching local batch VK.

Reference local start example:

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

`privacy-note-v1` requires descriptors in the exact order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`. Validators check the consensus identity and load the four VKs only; prover-role readiness loads the selected R1CS/PK pair lazily. For the recorded BatchJoinSplit16x32 development artifacts, R1CS is `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK is `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, and VK is `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`. Generation peak RSS was `3,308,797,952 B`; role readiness peaked at `1,295,482,880 B`.

These are development identities only. Formal trusted setup, artifact signing, reproducible production generation, custody, and distribution remain downstream release responsibilities.

Production needs:

- artifact generation commit record
- artifact generation command record
- checksum manifest archival
- artifact signer or release signer
- runtime preflight `strict`
- stale artifact and verifier mismatch treated as release blockers

Related documents:

- `docs/clairveil-circuits.md`
- `docs/clairveil-proverd-remote-production-profile.md`
- `docs/clairveil-security-best-practices-review.md`

## 4. Merkle Tree Operations

The privacy pool is a depth-32 single Merkle tree.

| tx | leaf change |
| --- | --- |
| deposit | +1 |
| native 2x2 transfer | +2 |
| batch transfer | +1..32 |
| withdraw | +0 |

Operators should track:

- `leaf_count`
- `max_leaves`
- `remaining_leaves`
- current root
- historical root retention

Recommended alert thresholds:

| Usage | Meaning |
| --- | --- |
| 50% | start tracking long-term capacity trend |
| 70% | start discussing new pool/circuit upgrade |
| 85% | upgrade plan should be finalized |
| 95% | prepare migration window or limit large inflow |

After snapshot/restore/migration, recompute sample Merkle paths according to [clairveil-merkle-restore-sop.md](clairveil-merkle-restore-sop.md).

## 5. Prover Operations

`clairveil-proverd` does not directly receive private seeds, but it receives prepared proof payloads. Those payloads can include amount, note randomness, Merkle path, nullifier, shielded public keys, and disclosure metadata.

The reference prover exposes BatchJoinSplit16x32 only through `POST /v1/proofs/batch-transfer`. Do not route `MsgBatchTransfer` witnesses through a generic or existing JoinSplit endpoint. The batch route enforces strict framing/body bounds and circuit-specific queue/in-flight admission; its permit remains held until the actual prove call returns. Automatic multi-prover failover is disabled.

A remote prover is a privacy-sensitive trusted component.

Operations baseline:

- place behind private network or edge proxy
- TLS or mTLS
- mandatory auth
- request body limit
- timeout and concurrency limit
- redacted logging
- artifact directory mounted read-only
- `/healthz`, `/readyz`, and `/debug/vars` internal-only

Use [clairveil-proverd-remote-production-profile.md](clairveil-proverd-remote-production-profile.md) as the detailed baseline.

## 6. Audit Key Operations

Every transfer includes mandatory audit disclosure. Therefore the audit master private key can read from/to/amount/asset information for all shielded transfers.

Production needs:

- key generation ceremony
- HSM/KMS or equivalent custody
- decrypt permission separation
- access logs and approval workflow
- rotation/migration plan
- compromised-key incident response
- auditor UX that enforces disclosure verification

Clairveil does not implement private key custody.

## 7. Wallet Operations

The reference CLI stores local JSON files with restrictive permissions. This is a development baseline, not production wallet storage.

Production wallets must decide:

- root seed and derived secret encryption
- viewing key storage policy
- note cache encryption
- prepared payload/proof JSON retention
- telemetry redaction
- remote prover trust boundary UX
- disclosure decode verification display

## 8. Monitoring

Recommended metrics:

- tx count by type: deposit/native-transfer/batch-transfer/withdraw
- batch input/output counts, deterministic precharge, out-of-gas rejection, and atomic rollback errors
- transfer disclosure mode distribution
- proof generation latency
- prover error rate
- nullifier rejection count
- Merkle `leaf_count` and usage ratio
- failed `merkle_path` query
- reserve `invariant_holds=false`
- artifact preflight failure
- remote prover auth failure
- remote prover body limit rejection

Recommended log redaction:

- private seed, mnemonic, scalar
- viewing key, disclosure private key
- prepared payload body
- proof bytes
- bearer token
- decrypted disclosure payload

## 9. Release Operations

Before the release commit and tag:

```bash
make release-check
```

After creating the annotated exact-SemVer tag at that commit:

```bash
make release-pack
make release-pack-verify
```

If shipping prover image:

```bash
make docker-proverd-build
```

Release notes should include at least:

- proto/fixture/schema/CLI/prover contract impact
- ZK artifact impact
- accepted vulnerabilities
- downstream action required
- artifact checksum/provenance policy
- circuit-set/public-witness/gas/scan-schema version impact (`privacy-note-v1`, `BatchGasModelV1`, `privacy-sequence-v1`, `privacy-scan-v2`)

## 10. Incident Response Criteria

| Situation | Response |
| --- | --- |
| audit key compromise | stop disclosure access, execute key rotation/migration plan, estimate affected disclosure scope |
| prover token leak | rotate token, review access logs, check proof endpoint abuse |
| artifact checksum mismatch | stop node/prover start, revalidate artifact source, treat as release blocker |
| reserve invariant mismatch | pause release or rollout, compare module account balance with deposit/withdraw totals, investigate direct sends, top-ups, or migration writes |
| Merkle restore mismatch | do not resume node, rebuild offline or retry restore |
| wallet cache corruption | back up cache, rescan, verify user seed/key preservation |

## 11. Minimum Mainnet Gate

Before attaching Clairveil core to downstream mainnet:

1. downstream app e2e passes deposit/transfer/disclosure/withdraw.
2. JS/web wallet passes conformance fixtures and live chain tests.
3. remote/local/browser prover topology is decided.
4. audit key custody is documented and rehearsed.
5. artifact signing/provenance policy exists.
6. snapshot/restore rehearsal and Merkle path sample validation are complete.
7. `reserve/{denom}` returns `invariant_holds=true` after deposit/withdraw e2e for each supported denom.
8. chain-specific threat model is written.
9. `TestBatchTransferDirectCoreIntegration`, atomic scan-failure tests, and cross-message 2x2+batch/batch+batch rollback tests pass against the release commit.
10. The SDK, remote batch prover route, typed scanner/decrypt path, one-proof payroll integration, CLI/tutorial, conformance fixture, and actual localnet workflow supplied by the batch integration pass together; formal setup, production artifact release, external audit, and downstream wallet products remain separate gates.
