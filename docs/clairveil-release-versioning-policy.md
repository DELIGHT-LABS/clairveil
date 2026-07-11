# Clairveil Release Versioning Policy

This document defines the rules for Clairveil release tags, changelog entries, release notes, and handoff packs.

Clairveil is a standalone privacy core that downstream chains can import or fork. Therefore release notes must be more than a change list; they must help downstream teams decide what to revalidate.

Korean version: [clairveil-release-versioning-policy-kr.md](clairveil-release-versioning-policy-kr.md)

## 1. Versioning Principles

Until the first public stable release, use `v0.x.y`.

```text
v0.MAJOR.MINOR
```

Recommended meaning:

| Version | Meaning |
| --- | --- |
| `v0.x.0` | release with meaningful feature or contract additions |
| `v0.x.y` | bug fix, docs, CI, packaging, or fixture hardening release |
| `v1.0.0` | first stable release where downstream production integration contract is declared stable |

During `v0`, API/fixture/proto/schema can change. If they change, release notes must state migration impact.

## 2. Breaking Change Criteria

Mark release notes with breaking or migration impact when any of these change:

- `proto/clairveil/privacy/v1` message, service, or field
- `x/privacy/client/sdk/conformance/testdata` fixture shape or value
- `docs/schemas/clairveil-js-wallet-contract.schema.json`
- prover HTTP path, request/response version, or error code
- CLI command, flag, or JSON output field
- shielded address prefix, transparent prefix, denom, or chain-id defaults
- ZK circuit input shape, artifact manifest, or checksum policy
- required circuit-set descriptor/order, VK identity, or public-input schema digest
- `MsgBatchTransfer` framing/output semantics, `BatchGasModelV1`, atomic transition, minimal event, or typed scan/genesis schema
- disclosure payload version, policy, mode, or digest binding
- scan projection version, cursor semantics, or empty-page/`has_more` handling
- transfer view tag derivation, length, event field, or payload-hash binding

## 3. Required Pre-Release Commands

Release candidates must pass:

```bash
make release-check
make release-pack
make release-pack-verify
```

If remote prover image is included:

```bash
make docker-proverd-build
```

## 4. Changelog Rules

Move `CHANGELOG.md` `Unreleased` entries into the release version.

Recommended sections:

```markdown
## v0.x.y - YYYY-MM-DD

### Added

### Changed

### Fixed

### Security

### Known Risk

### Handoff Notes
```

Meaning:

| Section | Meaning |
| --- | --- |
| `Added` | new feature, fixture, schema, command |
| `Changed` | meaningful change to existing contract, UX, packaging, or docs |
| `Fixed` | bug fix or test regression fix |
| `Security` | vulnerability scan, dependency update, threat model, custody guidance |
| `Known Risk` | accepted vulnerability or downstream-owned production risk |
| `Handoff Notes` | work downstream chain/SDK/wallet/prover teams must review |

## 5. Release Note Template

Use `docs/clairveil-release-note-template.md` for GitHub release or downstream handoff messages. If a shorter note is needed, keep the same structure.

```markdown
# Clairveil v0.x.y Release Notes

## 1. Summary

## 2. Verification

- [ ] `make release-check`
- [ ] `make release-pack`
- [ ] `make release-pack-verify`
- [ ] `make docker-proverd-build` if prover image is included
- [ ] If publishing performance numbers, confirm `make privacy-proverd-load-bench`, `make privacy-localnet-tps-bench`, `make privacy-user-latency-bench`, and `make privacy-public-capacity-report` outputs have `claim_eligible=true` with evidence hashes

## 3. Handoff Artifacts

- handoff tarball:
- handoff sha256:
- commit:

## 4. Compatibility Impact

- Proto:
- Fixture/schema:
- CLI:
- Prover HTTP:
- Scan/query contract:
- Transfer/view tags:
- ZK artifacts:
- Circuit set/public witness:
- Batch gas/scan state:
- Session 3B surface status:

## 5. Known Risk / Accepted Exceptions

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. Reassess in downstream risk register.
- `GO-2026-4479`: pion/dtls v2 no-fixed-version advisory reachable through the Cosmos SDK/CometBFT server stack. Reassess in downstream risk register.
- `GO-2026-5932`: no-fixed-version `x/crypto/openpgp` advisory, narrowly reachable through Cosmos SDK local ASCII key armor only; Clairveil does not use OpenPGP signing or encryption. Reassess and remove the exception when a fixed dependency path exists.

## 6. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:
```

## 6. Handoff Pack Naming

`make release-pack` creates:

```text
dist/clairveil-handoff-<git-describe>.tar.gz
dist/clairveil-handoff-<git-describe>.tar.gz.sha256
```

If a release tag exists, `<git-describe>` is tag-based. For release candidates or manual override:

```bash
RELEASE_VERSION=v0.1.0-rc1 make release-pack
```

## 7. Recommended Tag Flow

1. Move `CHANGELOG.md` entries into the release version.
2. Pass `make release-check`.
3. Pass `make docker-proverd-build` if needed.
4. Create release commit.
5. Create annotated tag.
6. Run `make release-pack` again on the tag.
7. Run `make release-pack-verify` against the tag handoff pack.
8. Include tarball checksum and known risks in release notes.

Example:

```bash
git tag -a v0.1.0 -m "Clairveil v0.1.0"
make release-pack
make release-pack-verify
```

## 8. Session 3A Release Baseline

Release notes that include the Session 3A chain core must record all of the following together:

- circuit set `privacy-note-v1`, identity schema `v1`, manifest schema `v2`, and exact required order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`;
- batch public-input order `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo` and schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`;
- production proto/API `MsgBatchTransfer` and `BatchTransferOutput`, gas model `BatchGasModelV1`, sequence `privacy-sequence-v1`, scan schema `privacy-scan-v2`, asset registry `privacy-asset-registry-v1`, and privacy state version `2`;
- development artifact identity: constraints `1,111,837`; R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`; PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`; VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`; generation/readiness peak RSS `3,308,797,952 B` / `1,295,482,880 B`;
- direct core integration, atomic scan failure, and cross-message 2x2+batch/batch+batch rollback results.

The same release note must state that public batch Go SDK, remote batch prover route, wallet scanner/decrypt UX, one-proof payroll integration, batch CLI/tutorial, formal trusted setup, and production artifact distribution are not part of Session 3A. Development artifact hashes identify the tested binaries; they do not authorize production deployment.
