# Clairveil Documentation Index

> Korean version: [README-kr.md](README-kr.md)

This is the complete index and lifecycle policy for durable repository knowledge. Read documentation from the same tag or commit as the code. The root [README](../README.md) is the entry point; this file defines where documents belong and which detailed references are current.

## Directory And Lifecycle Rules

| Location | Keep here | Release treatment |
| --- | --- | --- |
| `docs/` | Current architecture, API, protocol, operations, security, tutorials, validation evidence, and release knowledge | Tracked; included in the handoff pack when selected by the release manifests |
| `plans/` | Implementation intent, sequencing, acceptance criteria, and completed plan ledgers | Tracked while useful; status is recorded in the [plan index](../plans/README.md) |
| `tmpdocs/` | Superseded plans, duplicate handoffs, local drafts, commit notes, and non-authoritative archives | Git-ignored; never authoritative or release input |
| `tmp/` | Generated runtime/test scratch data only | Git-ignored; Markdown documents are not allowed |

Every durable top-level knowledge document under `docs/` must have English and Korean files in the same change. Update the two versions together and preserve commands, paths, API names, versions, and hashes. Historical validation reports may record an older decision at the time stated; current behavior must be described by current references and code, not inferred from an old report.

When content is duplicated, keep one current knowledge document in `docs/`, keep implementation planning in `plans/`, and move obsolete working material to `tmpdocs/`. Do not link from tracked documentation to `tmpdocs/` because ignored archives are not part of a clone or release.

## Contract Authority

For executable behavior, use the compiled proto/message/query definitions and keeper validation. For frozen cross-language encodings, use the normative circuit contract, schemas, and conformance fixtures. CLI help and the CLI reference define command surfaces. Guides explain integration and operations; plans and validation reports provide rationale and evidence.

Release membership has two machine-readable sources:

- `scripts/release-pack-paths.txt` selects tracked trees/files.
- `scripts/release-pack-required-files.txt` lists files that every handoff pack must contain.

Run `make docs-check` while editing. A release tag must use exact SemVer, be annotated, point to the final clean immutable commit, and have matching English/Korean `CHANGELOG` sections. Generate and verify the final archive from that tagged commit with `make release-pack` and `make release-pack-verify`; untagged snapshot packs are CI/internal checks, not releases.

## Start And Core Architecture

| Subject | English | 한국어 |
| --- | --- | --- |
| Prerequisites and first run | [Getting started](clairveil-getting-started.md) | [시작 가이드](clairveil-getting-started-kr.md) |
| Components and data flow | [Architecture](clairveil-architecture.md) | [아키텍처](clairveil-architecture-kr.md) |
| Manual end-to-end flow | [Local privacy walkthrough](clairveil-local-privacy-walkthrough.md) | [Local privacy walkthrough](clairveil-local-privacy-walkthrough-kr.md) |
| Circuit behavior | [Circuits](clairveil-circuits.md) | [Circuits](clairveil-circuits-kr.md) |
| Command surface | [CLI reference](clairveil-cli-reference.md) | [CLI reference](clairveil-cli-reference-kr.md) |
| Cosmos app integration | [Downstream integration](clairveil-downstream-cosmos-integration-guide.md) | [Downstream integration](clairveil-downstream-cosmos-integration-guide-kr.md) |
| Test layers and gates | [Testing guide](clairveil-testing-guide.md) | [Testing guide](clairveil-testing-guide-kr.md) |

## Batch Protocol And Evidence

| Subject | English | 한국어 |
| --- | --- | --- |
| Normative 16x32 contract | [BatchJoinSplit16x32](clairveil-batch-joinsplit-16x32.md) | [BatchJoinSplit16x32](clairveil-batch-joinsplit-16x32-kr.md) |
| Live batch flow | [Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial.md) | [Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial-kr.md) |
| Go SDK/prover/scanner/payroll/CLI handoff | [Batch client integration handoff](clairveil-batch-transfer-integration-handoff.md) | [Batch client integration handoff](clairveil-batch-transfer-integration-handoff-kr.md) |
| Publication validation evidence | [Independent publication validation report](clairveil-batch-joinsplit-16x32-publication-validation-report.md) | [Independent publication validation report](clairveil-batch-joinsplit-16x32-publication-validation-report-kr.md) |

## Client And Wallet

| Subject | English | 한국어 |
| --- | --- | --- |
| Product scope | [Client product brief](clairveil-client-product-brief.md) | [Client product brief](clairveil-client-product-brief-kr.md) |
| User journeys | [Client UX flows](clairveil-client-ux-flows.md) | [Client UX flows](clairveil-client-ux-flows-kr.md) |
| Product/security decisions | [Client risk decisions](clairveil-client-risk-decisions.md) | [Client risk decisions](clairveil-client-risk-decisions-kr.md) |
| Chain/prover API checklist | [Client API checklist](clairveil-client-api-checklist.md) | [Client API checklist](clairveil-client-api-checklist-kr.md) |
| Cross-language SDK contract | [JS SDK handoff](clairveil-js-sdk-handoff.md) | [JS SDK handoff](clairveil-js-sdk-handoff-kr.md) |
| Browser product boundary | [WebApp scope](clairveil-web-app-scope.md) | [WebApp scope](clairveil-web-app-scope-kr.md) |
| Browser API and lifecycle | [WebApp integration](clairveil-web-app-integration.md) | [WebApp integration](clairveil-web-app-integration-kr.md) |
| Browser storage and restart | [WebApp storage and recovery](clairveil-web-app-storage-recovery.md) | [WebApp storage and recovery](clairveil-web-app-storage-recovery-kr.md) |
| Browser/prover deployment | [WebApp deployment](clairveil-web-app-deployment.md) | [WebApp deployment](clairveil-web-app-deployment-kr.md) |

## Bulk Transfer And Reference Payroll

| Subject | English | 한국어 |
| --- | --- | --- |
| Bulk product/control-plane handoff | [Bulk transfer product handoff](clairveil-bulk-transfer-product-handoff.md) | [Bulk transfer product handoff](clairveil-bulk-transfer-product-handoff-kr.md) |
| Reservation model | [Note reservation design](clairveil-note-reservation-design.md) | [Note reservation design](clairveil-note-reservation-design-kr.md) |
| Accounting model | [Privacy accounting design note](clairveil-privacy-accounting-design-note.md) | [Privacy accounting design note](clairveil-privacy-accounting-design-note-kr.md) |
| Product overview | [Reference payroll product](clairveil-reference-payroll-product.md) | [Reference payroll product](clairveil-reference-payroll-product-kr.md) |
| Product policy | [Reference payroll policy](clairveil-reference-payroll-product-policy.md) | [Reference payroll policy](clairveil-reference-payroll-product-policy-kr.md) |
| JS SDK boundary | [Payroll JS SDK handoff](clairveil-reference-payroll-js-sdk-handoff.md) | [Payroll JS SDK handoff](clairveil-reference-payroll-js-sdk-handoff-kr.md) |
| Wallet boundary | [Payroll wallet handoff](clairveil-reference-payroll-wallet-handoff.md) | [Payroll wallet handoff](clairveil-reference-payroll-wallet-handoff-kr.md) |
| Live workflow | [Payroll localnet tutorial](clairveil-reference-payroll-live-localnet-tutorial.md) | [Payroll localnet tutorial](clairveil-reference-payroll-live-localnet-tutorial-kr.md) |
| Rehearsal procedure | [Payroll rehearsal](clairveil-reference-payroll-rehearsal.md) | [Payroll rehearsal](clairveil-reference-payroll-rehearsal-kr.md) |
| Recorded rehearsal result | [Payroll localnet result](clairveil-reference-payroll-localnet-rehearsal-result.md) | [Payroll localnet result](clairveil-reference-payroll-localnet-rehearsal-result-kr.md) |

## Operations And Security

| Subject | English | 한국어 |
| --- | --- | --- |
| Validator/prover/artifact operations | [Operations guide](clairveil-operations-guide.md) | [Operations guide](clairveil-operations-guide-kr.md) |
| Remote prover baseline | [Proverd production profile](clairveil-proverd-remote-production-profile.md) | [Proverd production profile](clairveil-proverd-remote-production-profile-kr.md) |
| Restore verification | [Merkle restore SOP](clairveil-merkle-restore-sop.md) | [Merkle restore SOP](clairveil-merkle-restore-sop-kr.md) |
| Trust boundaries and abuse paths | [Threat model](clairveil-threat-model.md) | [Threat model](clairveil-threat-model-kr.md) |
| Pre-production security review | [Security best-practices review](clairveil-security-best-practices-review.md) | [Security best-practices review](clairveil-security-best-practices-review-kr.md) |

## Maintenance And Releases

| Subject | English | 한국어 |
| --- | --- | --- |
| Cross-file maintenance rules | [Maintainer instructions](clairveil-maintainer-instructions.md) | [Maintainer instructions](clairveil-maintainer-instructions-kr.md) |
| Release/version/changelog policy | [Release versioning policy](clairveil-release-versioning-policy.md) | [Release versioning policy](clairveil-release-versioning-policy-kr.md) |
| Handoff bundle and verification | [Release handoff pack](clairveil-release-handoff-pack.md) | [Release handoff pack](clairveil-release-handoff-pack-kr.md) |
| Authoritative release-note form | [Release note template](clairveil-release-note-template.md) | [Release note template](clairveil-release-note-template-kr.md) |

Schema details have their own [schemas index](schemas/README.md). Canonical fixture files live in [the Go SDK conformance testdata directory](../x/privacy/client/sdk/conformance/testdata).
