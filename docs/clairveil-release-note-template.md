# Clairveil v0.x.y Release Note Template

This tracked file is the generic form for a public-facing Clairveil release note. Copy it to an external draft for each release; do not write release-specific artifact identity or checksum values back into the tagged source.

Korean version: [clairveil-release-note-template-kr.md](clairveil-release-note-template-kr.md)

## 1. Summary

- Key change in this release:
- Change downstream projects must know immediately:
- Compatibility impact:

## 2. Verification

Before creating the release commit/tag:

- [ ] `make docs-check`
- [ ] `make release-check`
- [ ] `make docker-proverd-build` if prover image is included
- [ ] If publishing performance numbers, every relevant report has `claim_eligible=true`, matching source/artifact identity, and recorded evidence hashes

After creating the annotated exact-SemVer tag at the final commit:

- [ ] `make release-pack`
- [ ] `make release-pack-verify`

## 3. Handoff Artifacts

- exact tag:
- manifest/source commit:
- handoff tarball:
- external handoff SHA-256:
- GitHub release URL:
- signing/attestation/SBOM: supplied / not supplied (explain)

## 4. Compatibility Impact

| Area | Impact |
| --- | --- |
| Proto | none / yes |
| Fixture/schema | none / yes |
| CLI | none / yes |
| Prover HTTP | none / yes |
| Scan/query contract | none / yes |
| Transfer/view tags | none / yes |
| ZK artifact | none / yes |
| Circuit set/public witness | none / yes |
| Privacy state/genesis migration | none / yes |
| Batch gas/scan state | none / yes |
| Session 3B SDK/prover/scanner/payroll/CLI surface | none / yes |

If there is impact, record downstream actions in section 7.

## 5. Known Risk / Accepted Exceptions

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. Reassess in downstream risk register.
- `GO-2026-4479`: pion/dtls v2 no-fixed-version advisory reachable through the Cosmos SDK/CometBFT server stack. Reassess in downstream risk register.
- `GO-2026-5932`: no-fixed-version `x/crypto/openpgp` advisory, narrowly reachable through Cosmos SDK local ASCII key armor only; Clairveil does not use OpenPGP signing or encryption. Reassess and remove the exception when a fixed dependency path exists.

## 6. Operations Notes

- audit key custody impact:
- artifact signing/provenance impact:
- prover deployment impact:
- wallet storage/telemetry impact:
- Merkle restore/capacity impact:
- rollback/yank response if this release is found unsafe:

## 7. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:

## 8. Responsibility Boundary

Clairveil releases provide a reusable privacy core and reference host. Downstream production projects separately own custom feature integration, audit private key custody, wallet storage encryption, remote prover deployment, and artifact signing/provenance.

State the publication disposition explicitly: `PUBLICATION_READY_EXPERIMENTAL` is not `PRODUCTION_RELEASE_READY`. A correction after publication uses a new patch tag; never move the released tag.
