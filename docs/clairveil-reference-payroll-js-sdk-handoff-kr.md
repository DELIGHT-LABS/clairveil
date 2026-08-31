# Clairveil Reference Payroll JS SDK Handoff

English version: [clairveil-reference-payroll-js-sdk-handoff.md](clairveil-reference-payroll-js-sdk-handoff.md)

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

## One-proof batch payroll reference 경계

Repository에는 many input reservation을 one `MsgBatchTransfer` operation 및 many item-evidence record와 연결하는 Go one-proof payroll graph, batch builder, bounded prover route, broadcast/retry reconciliation, typed scanner가 포함됩니다. ClairveilJS 0.3.1은 같은 prepared effect, reservation v3 contract, typed output evidence, Cosmos/EVM execution boundary를 구현합니다. Product UX는 deployment config와 transport capability로 feature gate합니다.

JS 구현은 `privacy-note-v1`을 pin하고 canonical `privacy-fixed-v1` note/disclosure/typed-envelope byte를 사용합니다. 모든 32-byte asset ID는 authoritative `AssetRegistryV1`으로 resolve하고 payroll denom configuration과 registry result가 일치해야 합니다. Fresh genesis와 exact artifact, empty reservation/note/cursor/prepared/proof namespace에서 초기화한 뒤 typed rescan을 완료합니다.

Unified scan cursor `(height, global_sequence, output_index)` 전체와 same-root Merkle path snapshot을 사용합니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical path query는 treasury activity를 드러내므로 privacy warning을 유지하고 필요하면 privacy-preserving infrastructure를 사용합니다. Downstream payroll port는 production 순서 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`를 보존해야 하며 자체 end-to-end 완료 전 support를 advertise하면 안 됩니다.

Prover integration에는 bounded service wrapper와 role-aware lazy artifact loader를 사용합니다. Default는 circuit별 in-flight 1개, queued 4개, positive 8 MiB request limit이며 0은 invalid입니다. Automatic failover를 끕니다. Client cancellation 뒤에도 in-process proving이 계속되어 reservation/admission slot을 점유할 수 있으므로 재사용 전에 job과 note state를 reconcile합니다. Hard termination이 필요하면 isolated worker process를 사용합니다.
