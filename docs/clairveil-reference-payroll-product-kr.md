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
- 예상 message chunk 수

이 helper는 자동 split/merge tx를 직접 실행하지 않음. Product layer는 report를 보고 operator approval 또는 auto-prepare flow를 구현해야 함.

### `clairveil-payroll prepare-notes`

Reference CLI는 note preparation analyzer를 실행하는 첫 제품 표면을 제공함.

```bash
clairveil-payroll prepare-notes \
  -input payroll-prepare.json \
  -out payroll-prepare-report.json
```

입력 JSON은 payroll item과 treasury note inventory를 포함함.

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

출력 report는 ready/blocked item 수, dummy note 부족 여부, reserved note 제외 여부, split/merge recommendation을 JSON으로 제공함.

## 완료 기준

Reference Payroll Product 1.5차의 repo 기준 완료 조건은 다음과 같음.

- 1차 readiness check가 통과함.
- disclosure policy와 key registry contract가 제공됨.
- note preparation analyzer가 제공됨.
- JS SDK handoff 문서가 제공됨.
- wallet handoff 문서가 제공됨.
- downstream이 payroll workflow를 조립할 수 있는 기준 문서가 제공됨.

## 남은 제품화 작업

이 repo가 직접 완료하지 않는 작업은 다음과 같음.

- 실제 production DB adapter
- scheduler/daemon service
- admin UI
- JS SDK 구현
- 웹/모바일 지갑 구현
- 실제 고객사의 payroll policy 결정
- staging/production rehearsal 실행
