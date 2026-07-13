# Clairveil Release Versioning Policy

This document defines the rules for Clairveil release tags, changelog entries, release notes, and handoff packs.

Clairveil is a standalone privacy core that downstream chains can import or fork. Therefore release notes must be more than a change list; they must help downstream teams decide what to revalidate.

Korean version: [clairveil-release-versioning-policy-kr.md](clairveil-release-versioning-policy-kr.md)

## 1. Versioning Principles

Until the first public stable release, use `v0.x.y`.

```text
v0.MINOR.PATCH
```

Recommended meaning:

| Version | Meaning |
| --- | --- |
| `v0.x.0` | release with meaningful feature or contract additions |
| `v0.x.y` | bug fix, docs, CI, packaging, or fixture hardening release |
| `v1.0.0` | first stable release where downstream production integration contract is declared stable |

During `v0`, API/fixture/proto/schema can change. If they change, release notes must state migration impact.

Release tags must be annotated and use exact SemVer prefixed by `v`, for example `v0.1.1`; prereleases use a SemVer suffix such as `v0.2.0-rc.1`. Do not move or reuse a published tag. The tag, `CHANGELOG.md`/`CHANGELOG-kr.md` heading, handoff manifest commit, archive, checksum, and GitHub release must all identify the same immutable source.

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

## 3. Required Release Commands

Before the release commit and tag, release candidates must pass:

```bash
make release-check
```

After committing the dated changelogs and other tracked release metadata, create the annotated tag at that exact commit. Prepare the public release-note draft from the authoritative templates before tagging, but keep post-tag fields such as the exact source commit, archive name, external SHA-256, and GitHub release URL outside the tagged source. Fill those fields only after generating and verifying the final artifacts from the tagged commit:

```bash
make release-pack
make release-pack-verify
```

If remote prover image is included:

```bash
make docker-proverd-build
```

## 4. Changelog Rules

Move the matching `CHANGELOG.md` and `CHANGELOG-kr.md` `Unreleased` entries into the release version. Every repository tag, including development snapshot tags, must have a dated heading in both files.

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

[clairveil-release-note-template.md](clairveil-release-note-template.md) and its [Korean pair](clairveil-release-note-template-kr.md) are the only authoritative release-note templates. Update those generic tracked forms rather than copying a second template into this policy. For each release, copy them to an external draft; do not fill release-specific artifact identity or checksum values in the tagged source. A shortened public note may omit empty detail, but it must retain verification, immutable artifact identity, compatibility impact, known risk, and downstream action.

## 6. Handoff Pack Naming

A publishable final release pack requires the clean source commit to have an annotated exact-SemVer tag and creates:

```text
dist/clairveil-handoff-<tag>.tar.gz
dist/clairveil-handoff-<tag>.tar.gz.sha256
```

`RELEASE_VERSION` may select an existing annotated exact-SemVer tag only when that tag points to the packed source commit. It cannot assign an untagged or unrelated version:

```bash
RELEASE_VERSION=v0.2.0-rc.1 make release-pack
```

On an untagged clean commit, the tooling uses the canonical `snapshot-<40-character-commit-sha>` version for packaging CI and internal completeness checks. A snapshot pack is commit-bound but is not a release artifact and must not be published as one.

## 7. Recommended Tag Flow

1. Move both changelogs into the same dated release version and prepare paired external release-note drafts from the authoritative templates, leaving post-tag identity fields blank.
2. Pass `make docs-check`, `make release-check`, and `make docker-proverd-build` when an image is included.
3. Create the release commit and confirm the worktree is clean.
4. Create one annotated, exact SemVer tag at that commit.
5. Run `make release-pack` on the tag and verify that same archive with `make release-pack-verify`. The default verifier reuses the existing default archive/checksum pair and generates it only when the pair is absent.
6. Confirm the archive manifest commit equals the tag target and record the exact commit, archive, and external SHA-256 in the external release-note drafts.
7. Push the release commit and tag only after verification, then create the GitHub release and attach the verified tarball and checksum.
8. Update supported-version/security information when the supported release line changes. Never move the published tag; issue a new patch release for corrections.

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
- when including the `S4-B02` 2x2 relation, JoinSplit constraints `99,775`, schema SHA-256 `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, and development R1CS/PK/VK SHA-256 `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4` / `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7` / `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`, plus explicit old-proof/job invalidation and fresh-genesis/reset evidence;
- direct core integration, atomic scan failure, and cross-message 2x2+batch/batch+batch rollback results.

The same release note must distinguish the Session 3B experimental reference Go SDK/prover/scanner/payroll/CLI surfaces from downstream JS/product completion. Formal trusted setup, external audit, production artifact distribution, and production operations remain excluded. Development artifact hashes identify the tested binaries; they do not authorize production deployment.
