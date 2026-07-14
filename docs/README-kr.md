# Clairveil 문서 Index

> English version: [README.md](README.md)

이 문서는 repository의 durable knowledge 전체 index이자 수명주기 정책입니다. 코드와 같은 tag 또는 commit의 문서를 읽어야 합니다. Root [README](../README-kr.md)가 시작점이며, 이 문서는 문서 위치와 현재 상세 reference를 정의합니다.

## Directory와 수명주기 규칙

| 위치 | 유지할 내용 | Release 처리 |
| --- | --- | --- |
| `docs/` | 현재 architecture, API, protocol, operations, security, tutorial, validation evidence, release 지식 | Tracked. Release manifest가 선택한 파일은 handoff pack에 포함 |
| `plans/` | 구현 의도, 순서, acceptance criteria, completed plan ledger | 유용한 동안 tracked. 상태는 [계획 index](../plans/README-kr.md)에 기록 |
| `tmpdocs/` | Superseded plan, 중복 handoff, local draft, commit note, non-authoritative archive | Git-ignored. Authority나 release input이 될 수 없음 |
| `tmp/` | Generated runtime/test scratch data만 | Git-ignored. Markdown 문서 금지 |

`docs/` 최상위의 durable knowledge 문서는 같은 change에서 English/Korean 파일을 함께 유지합니다. Command, path, API name, version, hash를 보존하고 두 언어를 동시에 review합니다. Historical validation report는 명시된 시점의 판단을 기록할 수 있지만, 현재 동작은 오래된 report가 아니라 current reference와 코드로 판단해야 합니다.

내용이 중복되면 현재 지식 하나만 `docs/`에, 구현 계획은 `plans/`에 두고 obsolete working material은 `tmpdocs/`로 이동합니다. Ignored archive는 clone/release에 없으므로 tracked 문서에서 `tmpdocs/`를 링크하면 안 됩니다.

## Contract authority

실행 동작은 compiled proto/message/query definition과 keeper validation을 기준으로 합니다. Frozen cross-language encoding은 normative circuit contract, schema, conformance fixture를 기준으로 합니다. CLI help와 CLI reference가 command surface를 정의합니다. Guide는 통합/운영을 설명하고 plan/validation report는 배경과 evidence를 제공합니다.

Release membership의 machine-readable source는 두 개입니다.

- `scripts/release-pack-paths.txt`는 tracked tree/file을 선택합니다.
- `scripts/release-pack-required-files.txt`는 모든 handoff pack에 필요한 파일을 나열합니다.

편집 중에는 `make docs-check`를 실행합니다. Release tag는 exact SemVer annotated tag여야 하고 최종 clean immutable commit을 가리키며 English/Korean `CHANGELOG` section이 있어야 합니다. 그 tagged commit에서 `make release-pack`과 `make release-pack-verify`를 실행합니다. Untagged snapshot pack은 CI/내부 검증용이지 release가 아닙니다.

## 시작과 core architecture

| 주제 | English | 한국어 |
| --- | --- | --- |
| 전제조건과 첫 실행 | [Getting started](clairveil-getting-started.md) | [시작 가이드](clairveil-getting-started-kr.md) |
| Component와 data flow | [Architecture](clairveil-architecture.md) | [아키텍처](clairveil-architecture-kr.md) |
| Manual end-to-end flow | [Local privacy walkthrough](clairveil-local-privacy-walkthrough.md) | [Local privacy walkthrough](clairveil-local-privacy-walkthrough-kr.md) |
| Circuit 동작 | [Circuits](clairveil-circuits.md) | [Circuits](clairveil-circuits-kr.md) |
| Command surface | [CLI reference](clairveil-cli-reference.md) | [CLI reference](clairveil-cli-reference-kr.md) |
| Cosmos app 통합 | [Downstream integration](clairveil-downstream-cosmos-integration-guide.md) | [Downstream integration](clairveil-downstream-cosmos-integration-guide-kr.md) |
| Test layer와 gate | [Testing guide](clairveil-testing-guide.md) | [Testing guide](clairveil-testing-guide-kr.md) |

## Batch protocol과 evidence

| 주제 | English | 한국어 |
| --- | --- | --- |
| Normative 16x32 contract | [BatchJoinSplit16x32](clairveil-batch-joinsplit-16x32.md) | [BatchJoinSplit16x32](clairveil-batch-joinsplit-16x32-kr.md) |
| Live batch flow | [Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial.md) | [Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial-kr.md) |
| Go SDK/prover/scanner/payroll/CLI handoff | [Batch client integration handoff](clairveil-batch-transfer-integration-handoff.md) | [Batch client 통합 인수인계](clairveil-batch-transfer-integration-handoff-kr.md) |
| Publication validation evidence | [Independent publication validation report](clairveil-batch-joinsplit-16x32-publication-validation-report.md) | [독립 공개 검증 보고서](clairveil-batch-joinsplit-16x32-publication-validation-report-kr.md) |

## Client와 wallet

| 주제 | English | 한국어 |
| --- | --- | --- |
| Product scope | [Client product brief](clairveil-client-product-brief.md) | [Client product brief](clairveil-client-product-brief-kr.md) |
| User journey | [Client UX flows](clairveil-client-ux-flows.md) | [Client UX flows](clairveil-client-ux-flows-kr.md) |
| Product/security decision | [Client risk decisions](clairveil-client-risk-decisions.md) | [Client risk decisions](clairveil-client-risk-decisions-kr.md) |
| Chain/prover API checklist | [Client API checklist](clairveil-client-api-checklist.md) | [Client API checklist](clairveil-client-api-checklist-kr.md) |
| Cross-language SDK contract | [JS SDK handoff](clairveil-js-sdk-handoff.md) | [JS SDK handoff](clairveil-js-sdk-handoff-kr.md) |

## Bulk transfer와 reference payroll

| 주제 | English | 한국어 |
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

## Operations와 security

| 주제 | English | 한국어 |
| --- | --- | --- |
| Validator/prover/artifact operations | [Operations guide](clairveil-operations-guide.md) | [Operations guide](clairveil-operations-guide-kr.md) |
| Remote prover baseline | [Proverd production profile](clairveil-proverd-remote-production-profile.md) | [Proverd production profile](clairveil-proverd-remote-production-profile-kr.md) |
| Restore verification | [Merkle restore SOP](clairveil-merkle-restore-sop.md) | [Merkle restore SOP](clairveil-merkle-restore-sop-kr.md) |
| Trust boundary와 abuse path | [Threat model](clairveil-threat-model.md) | [Threat model](clairveil-threat-model-kr.md) |
| Pre-production security review | [Security best-practices review](clairveil-security-best-practices-review.md) | [Security best-practices review](clairveil-security-best-practices-review-kr.md) |

## Maintenance와 release

| 주제 | English | 한국어 |
| --- | --- | --- |
| Cross-file maintenance rule | [Maintainer instructions](clairveil-maintainer-instructions.md) | [Maintainer instructions](clairveil-maintainer-instructions-kr.md) |
| Release/version/changelog policy | [Release versioning policy](clairveil-release-versioning-policy.md) | [Release versioning policy](clairveil-release-versioning-policy-kr.md) |
| Handoff bundle과 verification | [Release handoff pack](clairveil-release-handoff-pack.md) | [Release handoff pack](clairveil-release-handoff-pack-kr.md) |
| Authoritative release-note form | [Release note template](clairveil-release-note-template.md) | [Release note template](clairveil-release-note-template-kr.md) |

Schema detail은 [schemas index](schemas/README-kr.md)에 있습니다. Canonical fixture file은 [Go SDK conformance testdata directory](../x/privacy/client/sdk/conformance/testdata)에 있습니다.
