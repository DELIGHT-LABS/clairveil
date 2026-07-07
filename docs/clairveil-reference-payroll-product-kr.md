# Clairveil Reference Payroll Product 가이드

## 목적

이 문서는 Clairveil 대량전송 1.5차의 Reference Payroll Product 기준을 정의함.

Reference Payroll Product는 core protocol 필수 요소가 아님. 그러나 `clairveil-proverd`처럼 downstream 개발자가 실제 제품에 가져다 쓰거나 fork할 수 있는 companion/reference product 역할을 함.

목표는 다음과 같음.

- core module만 보고 대량전송 제품을 어떻게 만들어야 할지 모르는 문제를 줄임.
- payroll import, plan, note preparation, reserve, prove, broadcast, reconcile, report 흐름의 기준을 제공함.
- 샘플이지만 실제 product foundation으로 사용할 수 있는 품질을 지향함.

## 현재 제공 범위

현재 repo는 다음 foundation을 제공함.

| 영역 | 위치 |
| --- | --- |
| note reservation | `x/privacy/client/sdk/reservation` |
| payroll plan/control-plane | `x/privacy/client/sdk/payroll` |
| disclosure policy helper | `x/privacy/client/sdk/payroll/disclosure.go` |
| disclosure key registry contract | `x/privacy/client/sdk/payroll/disclosure_registry.go` |
| note preparation analyzer | `x/privacy/client/sdk/payroll/note_preparation.go` |
| file-backed reference artifact store | `x/privacy/client/sdk/payroll/file_artifact_store.go` |
| reference payroll CLI | `cmd/clairveil-payroll` |
| proof/broadcast/reconcile worker | `x/privacy/client/sdk/payroll/proof_queue.go`, `broadcast_queue.go`, `batch_broadcaster.go`, `reconcile_worker.go` |
| multi-message chunking | `x/privacy/client/sdk/payroll/chunker.go` |
| prover pool | `x/privacy/client/sdk/payroll/prover_pool.go` |
| readiness/benchmark | `scripts/privacy-bulk-readiness-check.sh`, `cmd/clairveil-bulktransferbench` |

## Reference Workflow

권장 workflow는 다음과 같음.

```text
1. payroll input import
2. recipient address와 disclosure policy 검증
3. note preparation analysis
4. operator approval 또는 auto-prepare
5. payroll plan 생성
6. plan 확정과 note reservation
7. proof worker 실행
8. proof result chunking
9. multi-message broadcast
10. tx/nullifier/evidence reconcile
11. status/report export
```

## 상품화 전제

현재 1.5차는 기존 `MsgTransfer`와 기존 join-split circuit을 유지함. 따라서 직원 1명당 transfer proof 1개가 필요함.

Reference Payroll Product는 proof 수를 줄이는 것이 아니라, 현재 구조에서 대량전송을 안전하게 운영하는 것을 목표로 함. Proof 수 자체를 줄이는 작업은 2차 N-output batch circuit에서 다룸.

## User Disclosure 정책

`transfer-batch` CLI는 readiness/capacity 검증을 위해 `all-private` / `none` 중심 제한을 유지함.

Reference Payroll Product는 더 넓은 정책을 표현할 수 있어야 하므로 `PayrollDisclosurePolicy`를 제공함.

```text
user_privacy_policy
user_disclosure_mode
user_disclosure_target_pubkey_hex
user_disclosure_target_key_id
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
```

상품 구현은 plan 단계에서 policy를 검증하고, proof/payload 생성 단계에서 기존 transfer SDK disclosure config로 변환해야 함.

## Disclosure Key Registry

`recipient-encrypted` user disclosure를 지원하려면 disclosure public key registry가 필요함.

Reference contract는 다음 scope를 제공함.

```text
employee
company
auditor
external
```

각 entry는 최소 다음 값을 가짐.

```text
key_id
scope
subject_id
public_key_hex
version
active
```

Production registry는 제품 repo에서 구현하되, key format과 lookup 의미는 `DisclosureKeyRegistry` contract를 따라야 함.

## Note Preparation

현재 2-input transfer circuit에서는 한 transfer마다 input note 2개가 필요함.

준비된 note가 없으면 payroll run 단계에서 실패하므로, 상품은 실행 전에 note preparation analysis를 수행해야 함.

`AnalyzeNotePreparation`은 다음을 알려줌.

- spendable note 수
- reserved/spent note 수
- zero dummy note 수
- ready item 수
- blocked item 수
- 필요한 dummy note 또는 split/merge recommendation
- 제품 레이어가 실행 계획으로 옮기기 쉬운 operation hint
- 예상 message chunk 수

이 helper는 자동 split/merge tx를 직접 실행하지 않음. Product layer는 report를 보고 operator approval 또는 auto-prepare flow를 구현해야 함.

`operation_hints`는 recommendation을 제품 레이어가 실행 계획으로 옮기기 쉽게 만든 값임. 예를 들어 dummy note가 부족하면 `make-dummy`, 특정 지급건의 note 조합이 맞지 않으면 `split-merge`, spendable 총액이 부족하면 `add-funds`, active reservation 때문에 제외된 note가 있으면 `resolve-reservation-lock` hint가 들어감.

## Reference CLI

Reference CLI는 payroll workflow를 아래처럼 끊어서 실행할 수 있는 제품 표면을 제공함.

```text
validate
prepare-notes
plan
run
status
reconcile
export-report
```

`run`과 `reconcile`은 rehearsal 직전 control-plane daemon 표면을 제공함. 즉 plan 확정, durable reservation/operation state 저장, evidence 기반 reconcile을 처리함. 실제 proof 생성과 chain broadcast는 기존 `ProofWorker`, `BatchBroadcastWorker`, provider/prover 설정을 연결하는 운영 단계이며, rehearsal에서 wiring을 검증해야 함.

### 입력 JSON

`validate`, `prepare-notes`, `plan`은 같은 입력 JSON을 사용함. 입력 JSON은 payroll item과 treasury note inventory를 포함함.

```json
{
  "company_id": "company-a",
  "payroll_id": "payroll-2026-07",
  "batch_id": "run-001",
  "denom": "uclair",
  "max_messages_per_tx": 20,
  "default_disclosure_policy": {
    "user_privacy_policy": "all-private",
    "user_disclosure_mode": "none"
  },
  "items": [
    {
      "item_id": "item-001",
      "employee_id": "employee-001",
      "recipient_address": "clairs1...",
      "amount": "70"
    }
  ],
  "treasury_notes": [
    {
      "note_id": "note-large",
      "owner_key_id": "treasury-key",
      "nullifier_lookup_key": "lookup-note-large",
      "denom": "uclair",
      "amount": "100"
    },
    {
      "note_id": "note-zero",
      "owner_key_id": "treasury-key",
      "nullifier_lookup_key": "lookup-note-zero",
      "denom": "uclair",
      "amount": "0"
    }
  ]
}
```

### `clairveil-payroll validate`

Payroll input의 recipient address, amount, denom, duplicate row, disclosure policy를 검증하고 note preparation summary를 함께 출력함.

```bash
clairveil-payroll validate \
  -input payroll-prepare.json \
  -out payroll-validation.json
```

출력은 `valid`, `errors`, `warnings`, `note_preparation`으로 구성됨. 입력 자체가 유효하지만 준비된 note가 부족하면 `warnings`에 준비 필요 항목이 표시됨.

### `clairveil-payroll prepare-notes`

Note preparation analyzer를 실행함.

```bash
clairveil-payroll prepare-notes \
  -input payroll-prepare.json \
  -out payroll-prepare-report.json
```

`-store-dir`을 추가하면 file-backed reference artifact store에도 같은 report를 저장함.

```bash
clairveil-payroll prepare-notes \
  -input payroll-prepare.json \
  -store-dir .clairveil-payroll
```

출력 report는 ready/blocked item 수, dummy note 부족 여부, reserved note 제외 여부, split/merge recommendation, operation hint를 JSON으로 제공함.

### `clairveil-payroll plan`

준비된 note inventory와 payroll input으로 draft payroll plan을 생성함.

```bash
clairveil-payroll plan \
  -input payroll-prepare.json \
  -out payroll-plan.json
```

생성된 plan에는 item별 `operation_id`, `chunk_id`, selected input notes, expected recipient/amount hash, disclosure expected digest가 포함됨. 아직 note reservation을 DB에 확정하는 단계는 아님. 확정은 production reservation store 또는 scheduler service에서 `Service.ConfirmPlan` 의미로 수행해야 함.

`-store-dir`을 추가하면 plan을 file-backed reference artifact store에도 저장함.

```bash
clairveil-payroll plan \
  -input payroll-prepare.json \
  -store-dir .clairveil-payroll
```

### `clairveil-payroll run`

Plan JSON을 durable reservation state에 확정함. 이 단계에서 item별 input note가 `Reserved`가 되고, `PayrollOperation` record가 저장됨.

```bash
clairveil-payroll run \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-confirmed-plan.json
```

`run`은 같은 plan으로 재실행해도 이미 생성된 reservation을 읽어 confirmed plan을 다시 출력하도록 idempotent하게 동작함. 이 명령은 proof 생성과 chain broadcast를 직접 수행하지 않음. 그 작업은 persisted reservation state를 입력으로 proof/broadcast worker를 연결하는 rehearsal 단계에서 검증함.

### `clairveil-payroll status`

Plan JSON 또는 durable reservation state를 읽어서 상태별 count를 출력함.

```bash
clairveil-payroll status \
  -plan payroll-plan.json \
  -out payroll-status.json
```

```bash
clairveil-payroll status \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-state-status.json
```

Plan 기준 출력은 `Planned`, `Reserved`, `Submitted`, `Confirmed`, `Failed`, `ReplanRequired`, `ManualReview` item 수를 집계함. State 기준 출력은 reservation status와 operation status를 각각 집계함.

### `clairveil-payroll reconcile`

Evidence JSON을 받아 durable reservation state의 reservation/operation 상태를 갱신함.

```bash
clairveil-payroll reconcile \
  -state .clairveil-payroll/reservation-state.json \
  -evidence reconcile-evidence.json \
  -out reconcile-report.json
```

Evidence JSON은 다음 형태를 사용함.

```json
{
  "evidence": [
    {
      "reservation_id": "operation-a:note:note-large",
      "tx_hash": "ABC123",
      "output_commitment": "commitment-a",
      "disclosure_digest": "digest-a",
      "recipient_hash": "recipient-hash-a",
      "amount_hash": "amount-hash-a",
      "denom": "uclair",
      "batch_item_index": 0,
      "batch_item_index_known": true,
      "nullifier_spent": true,
      "tx_succeeded": true
    }
  ]
}
```

`nullifier_spent=true`만으로 operation success로 처리하지 않음. 저장된 operation의 tx identity, output commitment, disclosure digest, recipient hash, amount hash, denom, batch item index와 일치해야 성공으로 reconcile됨. 일치하지 않으면 review/conflict 상태로 남김.

### `clairveil-payroll export-report`

기업 고객 또는 운영자가 볼 수 있는 item 단위 report JSON을 출력함.

```bash
clairveil-payroll export-report \
  -plan payroll-plan.json \
  -out payroll-report.json
```

출력은 plan summary와 item별 `item_id`, `employee_id`, `operation_id`, `chunk_id`, `status`, `amount`, `denom`, `failure_reason`, `retry_count`를 포함함.

## File Artifact Store

Reference product는 plan/report artifact 저장용 `FileArtifactStore`와 reservation/operation 상태 저장용 `DurableFileStore`를 구분함.

`FileArtifactStore`는 로컬/테스트/샘플 제품에서 plan과 report를 잃어버리지 않도록 제공함.

저장 범위는 다음과 같음.

```text
plans
plan-reports
note-preparation-reports
disclosure-keys
```

파일은 `0600`, 디렉토리는 `0700` 권한으로 생성함. 이 store는 민감정보를 포함할 수 있으므로 production에서는 암호화 DB 또는 secret storage로 대체하는 것이 원칙임.

## Durable Reservation State Store

`x/privacy/client/sdk/reservation.DurableFileStore`는 `reservation.Store` contract를 만족하는 durable reference adapter임. 상태 전이, active reservation uniqueness, compare-and-set, lease/heartbeat, operation evidence update는 기존 memory store와 같은 contract를 사용하고, 각 mutation 후 snapshot을 JSON 파일에 atomic write함.

기본 사용 예시는 다음과 같음.

```bash
clairveil-payroll run \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-confirmed-plan.json
```

이 adapter는 rehearsal과 reference product에서 재시작/재실행 동작을 검증하기 위한 repo-local production-style adapter임. 실제 고객 환경에서 PostgreSQL, MySQL, cloud secret-backed DB를 쓰는 경우에도 같은 `reservation.Store` 의미와 상태 전이 규칙을 지켜야 함.

## 완료 기준

Reference Payroll Product 1.5차의 repo 기준 완료 조건은 다음과 같음.

- 1차 readiness check가 통과함.
- disclosure policy와 key registry contract가 제공됨.
- note preparation analyzer가 제공됨.
- file-backed reference artifact store가 제공됨.
- durable reservation state store가 제공됨.
- `clairveil-payroll validate`, `prepare-notes`, `plan`, `run`, `status`, `reconcile`, `export-report` 명령이 제공됨.
- JS SDK handoff 문서가 제공됨.
- wallet handoff 문서가 제공됨.
- downstream이 payroll workflow를 조립할 수 있는 기준 문서가 제공됨.

## 남은 제품화 작업

이 repo가 직접 완료하지 않는 작업은 다음과 같음.

- managed production DB deployment와 tenant별 운영 schema hardening
- proof/broadcast worker wiring rehearsal
- admin UI
- JS SDK 구현
- 웹/모바일 지갑 구현
- 실제 고객사의 payroll policy 결정
- staging/production rehearsal 실행
