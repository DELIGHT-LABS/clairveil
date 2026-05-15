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
4. `tree_state`, `events`, `merkle_path`, `audit_config`, `disclosure_config`, and `circuit_config` queries are exposed.
5. snapshot/restore rehearsal is completed before release.

Reference local start example:

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict

clairveild start --minimum-gas-prices 0uclair
```

## 3. ZK Artifact Operations

`clairveil-setup` generates R1CS/PK/VK and checksum manifests.

```bash
clairveil-setup --out artifacts/privacy
```

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
| transfer | +2 |
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

A remote prover is a privacy-sensitive trusted component.

Operations baseline:

- place behind private network or edge proxy
- TLS or mTLS
- mandatory auth
- request body limit
- timeout and concurrency limit
- redacted logging
- artifact directory mounted read-only
- `/healthz` and `/readyz` internal-only

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

- tx count by type: deposit/transfer/withdraw
- transfer disclosure mode distribution
- proof generation latency
- prover error rate
- nullifier rejection count
- Merkle `leaf_count` and usage ratio
- failed `merkle_path` query
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

Maintainer baseline before release:

```bash
make release-check
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

## 10. Incident Response Criteria

| Situation | Response |
| --- | --- |
| audit key compromise | stop disclosure access, execute key rotation/migration plan, estimate affected disclosure scope |
| prover token leak | rotate token, review access logs, check proof endpoint abuse |
| artifact checksum mismatch | stop node/prover start, revalidate artifact source, treat as release blocker |
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
7. chain-specific threat model is written.
