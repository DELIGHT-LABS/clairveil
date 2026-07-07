# Clairveil Reference Payroll JS SDK Handoff

## 목적

이 문서는 JS SDK 팀이 Reference Payroll Product를 지원하기 위해 구현해야 할 작업을 정리함.

Core repo는 Go reference type, helper, fixture, 문서를 제공함. JS SDK 팀은 이를 JS/TS 환경에서 사용할 수 있는 SDK 계약으로 옮겨야 함.

## 구현 범위

JS SDK 팀이 담당할 범위는 다음과 같음.

```text
reservation type
payroll input/plan/item type
disclosure policy type
disclosure key registry client/helper
note inventory and preparation helper binding
batch nullifier query
prepared transfer/prover client
expected evidence helper
fixture/conformance CI
```

## 필수 Type Mapping

Go reference 위치:

```text
x/privacy/client/sdk/payroll/types.go
x/privacy/client/sdk/payroll/disclosure.go
x/privacy/client/sdk/payroll/disclosure_registry.go
x/privacy/client/sdk/payroll/note_preparation.go
x/privacy/client/sdk/reservation/types.go
```

JS SDK는 최소 다음 타입을 제공해야 함.

```text
PayrollInput
PayrollItemInput
PayrollDisclosurePolicy
PayrollPlan
PayrollPlanItem
TreasuryNote
NotePreparationReport
NotePreparationOperationHint
DisclosureKeyEntry
NoteReservation
PayrollOperation
```

## Reservation 요구사항

JS SDK는 reservation 상태를 Go reference와 같은 의미로 다뤄야 함.

필수 요구사항:

- active 상태의 reserved note를 일반 transfer, split, merge 후보에서 제외함.
- `owner_key_id + nullifier_lookup_key` 조합에 active reservation은 하나만 허용함.
- 상태 전이는 compare-and-set 의미를 유지함.
- worker-owned 상태 전이는 lease token을 요구함.
- `Submitted`, `Unknown`, `ManualReview`는 TTL만으로 available 처리하지 않음.

## Disclosure Policy 요구사항

JS SDK는 `PayrollDisclosurePolicy`를 표현하고 검증해야 함.

검증 규칙:

- `all-private` 정책은 `none` mode만 허용함.
- `all-private` 정책은 user disclosure target pubkey를 포함하지 않음.
- non-private 정책은 `public` 또는 `recipient-encrypted` mode를 사용함.
- `recipient-encrypted` mode는 32-byte compressed disclosure pubkey hex를 요구함.
- expected disclosure digest는 canonical 32-byte hex여야 함.

## Disclosure Key Registry 요구사항

JS SDK는 product backend 또는 wallet에서 받은 disclosure key entry를 검증할 수 있어야 함.

필수 필드:

```text
key_id
scope
subject_id
public_key_hex
version
active
```

지원 scope:

```text
employee
company
auditor
external
```

JS SDK는 key 원문을 analytics나 일반 log로 보내면 안 됨.

## Note Preparation 요구사항

JS SDK는 payroll 실행 전에 note preparation 상태를 계산하거나 backend 계산 결과를 검증할 수 있어야 함.

최소 제공 API:

```text
analyzeNotePreparation(input, treasuryNotes, policy)
```

결과에는 다음 정보가 있어야 함.

- ready item count
- blocked item count
- spendable note count
- reserved/spent note count
- zero dummy available/required
- selected note ids
- recommendations
- operation hints

JS SDK는 note preparation이 부족한 상태에서 payroll run을 무조건 진행하지 않도록 제품 UI에 signal을 제공해야 함.

`operation_hints`는 Go reference의 `NotePreparationOperationHint`와 같은 의미를 가져야 함. 제품 UI나 backend scheduler는 이 값을 보고 `make-dummy`, `split-merge`, `add-funds`, `resolve-reservation-lock` 같은 준비 작업 후보를 표시하거나 승인 flow로 넘길 수 있어야 함.

## File Artifact Store 참고사항

Go reference는 `FileArtifactStore`를 제공하지만, JS SDK가 반드시 같은 file layout을 구현해야 하는 것은 아님. 다만 local sample product나 CLI 연동을 지원한다면 다음 artifact 의미는 맞춰두는 것이 좋음.

```text
plans
plan-reports
note-preparation-reports
disclosure-keys
```

이 파일들은 payroll item, recipient, amount, selected note, disclosure key를 포함할 수 있으므로 local 저장소에서도 민감정보로 취급해야 함.

## Provider / Query 요구사항

JS SDK provider는 다음 query를 지원해야 함.

```text
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/circuit_config
```

대량 nullifier check는 POST body를 기본으로 하고, 요청당 1000개 이하로 chunk해야 함.

## Prover 연동

JS SDK는 다음 중 하나를 지원해야 함.

- browser/local prover
- remote `clairveil-proverd`
- product backend prover adapter

Remote prover를 사용할 경우 최소 route는 다음과 같음.

```text
POST /v1/prover/transfer
```

요청/응답 shape는 Go provertransport fixture와 맞춰야 함.

## Expected Evidence

Payroll item 성공 판정에는 nullifier spent만으로 충분하지 않음.

JS SDK 또는 backend는 가능한 범위에서 다음 expected value를 operation과 연결해야 함.

```text
expected_output_commitment
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
expected_recipient_hash
expected_amount_hash
expected_denom
batch_item_index
```

## 완료 기준

- Go fixture와 JS SDK fixture validator가 CI에서 통과함.
- reserved note가 일반 transfer 후보에서 제외됨.
- disclosure policy validation이 Go reference와 일치함.
- note preparation 부족 상태를 product UI가 표시할 수 있음.
- batch nullifier query를 chunking해서 사용할 수 있음.
- prepared transfer payload/prover response round-trip이 검증됨.
