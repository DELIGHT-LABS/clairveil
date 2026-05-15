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
- disclosure payload version, policy, mode, or digest binding

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

## 3. Handoff Artifacts

- handoff tarball:
- handoff sha256:
- commit:

## 4. Compatibility Impact

- Proto:
- Fixture/schema:
- CLI:
- Prover HTTP:
- ZK artifacts:

## 5. Known Risk / Accepted Exceptions

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. Reassess in downstream risk register.
- `GO-2026-4479`: pion/dtls v2 no-fixed-version advisory reachable through the Cosmos SDK/CometBFT server stack. Reassess in downstream risk register.

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
