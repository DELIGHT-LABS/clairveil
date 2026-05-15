# Clairveil v0.x.y Release Note Template

This template is the public-facing release note draft used when creating a Clairveil release tag.

Korean version: [clairveil-release-note-template-kr.md](clairveil-release-note-template-kr.md)

## 1. Summary

- Key change in this release:
- Change downstream projects must know immediately:
- Compatibility impact:

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

| Area | Impact |
| --- | --- |
| Proto | none / yes |
| Fixture/schema | none / yes |
| CLI | none / yes |
| Prover HTTP | none / yes |
| ZK artifact | none / yes |

If there is impact, record downstream actions in section 7.

## 5. Known Risk / Accepted Exceptions

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. Reassess in downstream risk register.
- `GO-2026-4479`: pion/dtls v2 no-fixed-version advisory reachable through the Cosmos SDK/CometBFT server stack. Reassess in downstream risk register.

## 6. Operations Notes

- audit key custody impact:
- artifact signing/provenance impact:
- prover deployment impact:
- wallet storage/telemetry impact:
- Merkle restore/capacity impact:

## 7. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:

## 8. Responsibility Boundary

Clairveil releases provide a reusable privacy core and reference host. Downstream production projects separately own custom feature integration, audit private key custody, wallet storage encryption, remote prover deployment, and artifact signing/provenance.
