# Clairveil Reference Payroll Product 가이드

English version: [clairveil-reference-payroll-product.md](clairveil-reference-payroll-product.md)

## 목적

이 문서는 현재 Clairveil checkout의 Reference Payroll Product 기준을 정의함. 이 문서는 legacy 1.5차 multi-message payroll 경로에서 시작했지만, 현재 protocol/reference surface는 batch integration이 제공하는 one-proof `BatchJoinSplit16x32` / `MsgBatchTransfer` 구현임.

2026-07-13 기준 repo는 reference Go batch SDK, bounded prover route, typed scanner, many-input/one-operation/many-item payroll graph, worker, reconcile, CLI, localnet workflow를 구현하고 검증함. 이는 experimental reference implementation이며 formal trusted setup, external audit, production artifact 배포, production deployment는 별도 gate로 남아 있음.

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
| SQL reservation store contract | `x/privacy/client/sdk/reservation/sql_store.go` |
| reference payroll CLI | `cmd/clairveil-payroll` |
| reference payroll daemon | `cmd/clairveil-payrolld`, `x/privacy/client/sdk/payroll/reference_daemon.go`, `x/privacy/client/sdk/payroll/live_daemon.go` |
| repo-local demo product | `examples/reference-payroll/payroll-demo.json`, `scripts/reference-payroll-demo.sh` |
| normative one-proof contract와 handoff | `docs/clairveil-batch-joinsplit-16x32-kr.md`, `docs/clairveil-batch-transfer-integration-handoff-kr.md` |
| one-proof batch SDK | `x/privacy/client/sdk/batchtransfer` |
| one-proof payroll graph/worker | `x/privacy/client/sdk/payroll/batch_plan.go`, `x/privacy/client/sdk/payroll/batch_graph.go`, `x/privacy/client/sdk/payroll/batch_proof_worker.go`, `x/privacy/client/sdk/payroll/batch_broadcast.go`, `x/privacy/client/sdk/payroll/batch_reconcile.go` |
| current one-proof localnet 검증 | `scripts/privacy-batch-joinsplit-localnet.sh`, `docs/clairveil-batch-joinsplit-localnet-tutorial-kr.md` |
| legacy multi-message localnet tutorial | `scripts/reference-payroll-live-localnet.sh`, `docs/clairveil-reference-payroll-live-localnet-tutorial-kr.md` |
| legacy phase 1 capacity rehearsal | `scripts/reference-payroll-rehearsal.sh`, `docs/clairveil-reference-payroll-rehearsal-kr.md` |
| proof/broadcast/reconcile worker | `x/privacy/client/sdk/payroll/proof_queue.go`, `broadcast_queue.go`, `batch_broadcaster.go`, `reconcile_worker.go` |
| legacy multi-message chunking | `x/privacy/client/sdk/payroll/chunker.go` |
| prover pool privacy/failover contract | `x/privacy/client/sdk/payroll/prover_pool.go` |
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
7. input 1..16개와 payment/change/padding output 1..32개를 batch operation 하나로 구성
8. canonical prepared payload 생성과 structured owner signature 1개 획득
9. BatchJoinSplit16x32 proof 1개 생성
10. 모든 nullifier 재확인 후 Cosmos MsgBatchTransfer 하나 또는 EVM singleProofBatchTransfer call 하나 제출
11. typed scan과 commitment/disclosure 검증
12. batch chain status와 item별 evidence를 분리 reconcile한 뒤 report export
```

legacy `transfer-batch` 경로는 독립적인 2x2 `MsgTransfer` 여러 개를 Cosmos transaction envelope 하나에서 coordination함. 이 경로는 regression/tutorial surface로 남아 있으며 one-proof `transfer-batch-16x32` 경로로 표현하거나 alias하면 안 됨.

## 상품화 전제

현재 batch reference integration 경로는 input note 1..16개를 소비하고 output 1..32개를 생성하는 atomic batch operation 하나에 `BatchJoinSplit16x32` proof 하나를 사용함. Payment, change, explicit padding이 모두 output slot을 사용하므로 output 32개가 항상 payroll payment 32건을 뜻하지는 않음.

Submission에서 operation contract는 transport-neutral함. Cosmos는
`MsgBatchTransfer` 하나, EVM host는 canonical `singleProofBatchTransfer` call 하나를
사용함. 두 경로 모두 complete linked input set과 모든 expected output이 reconcile된
뒤에만 성공을 보고하며 EVM 경로는 transaction에 bind된 명시적 successful receipt도
요구함.

이전 item별 `MsgTransfer` 구현과 recipient당 proof 1개 capacity model은 legacy comparison/regression surface로 계속 제공함. 이는 현재 proof-count model이 아님. One-proof batch 구현은 experimental이며 그 자체로 production productization 또는 production artifact 승인을 완료하지 않음.

## User Disclosure 정책

기본 user disclosure 정책은 `all-private` / `none`으로 둠. 이 기본값은 mandatory audit disclosure를 끄는 의미가 아니라, user-facing disclosure를 기본 off로 둔다는 의미임.

현재 one-proof batch SDK/CLI는 output별 disclosure를 독립 적용함. legacy `transfer-batch` CLI는 일반 `transfer`와 같은 shared disclosure flag인 `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to`를 계속 지원함. 제품 integration은 output별 batch reference integration contract를 보존해야 하며 legacy shared-flag 경로에서 이를 추론하면 안 됨.

Reference Payroll Product는 이 정책을 표현하기 위해 `PayrollDisclosurePolicy`를 제공함.

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

제품 기본값과 성공 판정 원칙은 [clairveil-reference-payroll-product-policy-kr.md](clairveil-reference-payroll-product-policy-kr.md)를 따름.

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

현재 one-proof planner는 `BatchJoinSplit16x32` operation 하나에 input note 1..16개를 선택하고 payment/change/padding output 1..32개를 생성함. 아래 legacy note-preparation analyzer와 `clairveil-payroll` CLI 경로는 payroll item마다 2-input `MsgTransfer` operation 하나를 계속 model함.

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

repo는 의도적으로 구분된 CLI surface 두 개를 제공함. 다음 `clairveil-payroll` command는 durable legacy multi-message control-plane/rehearsal surface임.

```text
validate
build-input-from-notes
prepare-notes
plan
run
status
scan-evidence
reconcile
settle-transfer-batch
seed-localnet-notes
export-report
```

현재 one-proof BatchJoinSplit16x32 chain CLI surface는 다음과 같음.

```text
transfer-batch-16x32
prepare-batch-transfer
prove-batch-transfer
broadcast-batch-transfer
```

신규 one-proof integration은 [batch transfer integration handoff](clairveil-batch-transfer-integration-handoff-kr.md)와 conformance fixture를 기준으로 함.

`run`, `scan-evidence`, `reconcile`은 durable control-plane 표면을 제공함. 즉 plan 확정, durable reservation/operation state 저장, tx event/nullifier evidence scan, evidence 기반 reconcile을 처리함.

`build-input-from-notes`는 실제 chain에서 `list-notes --json`으로 scan한 treasury note를 payroll input의 `treasury_notes`로 변환함.

`settle-transfer-batch`는 legacy `transfer-batch` tx 결과와 recipient note scan delta를 검증한 뒤 durable reservation state를 settle함. legacy live localnet tutorial에서 실제 chain tx와 payroll final report를 연결하는 bridge이며 one-proof batch-item reconcile API가 아님.

`seed-localnet-notes`는 localnet rehearsal 전용 helper임. localnet genesis commitment와 local wallet cache에 payroll용 amount note와 zero dummy note를 기록해 큰 restart/retry rehearsal에서 deposit 준비 시간을 줄임. Production note preparation 기능이 아니며 staging/testnet에서는 실제 deposit, split/merge, approval 기반 preparation flow를 검증해야 함.

`clairveil-payrolld`는 같은 durable legacy state를 읽어 repo 안에서 해당 흐름을 끝까지 체험할 수 있게 하는 reference daemon임. `simulated` mode는 실제 chain proof와 broadcast 대신 deterministic simulated proof/tx/evidence를 생성해 `Reserved -> Proving -> ProofReady -> Submitted -> ConfirmedSpent` 흐름을 검증함. 현재 one-proof payroll 경로는 별도 one-proof batch graph와 worker를 사용함.

`live` mode는 SDK `LiveDaemon` 상태머신을 사용함. proof 생성, tx broadcast, scanner evidence 수집은 `LiveOperationExecutor`로 주입할 수 있고, CLI reference 구현은 `-tx-query` 파일을 tick마다 다시 읽어 `Submitted` 또는 `Unknown` 상태를 reconcile함. production 제품은 실제 prover, tx broadcaster, tx/nullifier scanner implementation을 같은 상태머신에 연결해야 함.

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
      "nullifier_lookup_key_id": "lookup-v1",
      "denom": "uclair",
      "amount": "100",
      "verified_unspent": true
    },
    {
      "note_id": "note-zero",
      "owner_key_id": "treasury-key",
      "nullifier_lookup_key": "lookup-note-zero",
      "nullifier_lookup_key_id": "lookup-v1",
      "denom": "uclair",
      "amount": "0",
      "verified_unspent": true
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

생성된 plan에는 item별 `operation_id`, `chunk_id`, selected input notes, expected recipient/amount hash, disclosure expected digest가 포함됨. 이 단계는 아직 note reservation을 durable state에 확정하지 않는 draft 단계임. repo-local flow에서는 다음 `clairveil-payroll run` 명령이 `DurableFileStore`에 `Service.ConfirmPlan` 의미로 확정하고, production flow에서는 같은 contract를 `SQLStore` 또는 제품 scheduler service가 수행함.

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

`run`은 같은 plan으로 재실행해도 이미 생성된 reservation을 읽어 confirmed plan을 다시 출력하도록 idempotent하게 동작함. 이 명령은 proof 생성과 chain broadcast를 직접 수행하지 않음. legacy live localnet tutorial은 `transfer-batch`/`settle-transfer-batch`로 그 작업을 수행하고, 현재 one-proof integration은 one-proof batch graph와 proof/broadcast/reconcile worker를 사용함.

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
      "audit_disclosure_digest": "digest-a",
      "user_disclosure_digest": "user-digest-a",
      "self_view_disclosure_digest": "self-view-digest-a",
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

`nullifier_spent=true`만으로 operation success로 처리하지 않음. 저장된 operation의 tx identity, 명시적인 successful Cosmos execution 또는 EVM receipt, output commitment, audit disclosure digest, recipient hash, amount hash, denom, batch item index와 일치해야 성공으로 reconcile됨. user/self-view disclosure digest는 expected field가 있을 때 별도로 확인함. 일치하지 않으면 review/conflict 상태로 남김.

### `clairveil-payroll export-report`

기업 고객 또는 운영자가 볼 수 있는 item 단위 report JSON을 출력함.

```bash
clairveil-payroll export-report \
  -plan payroll-plan.json \
  -out payroll-report.json
```

durable reservation state를 함께 넘기면 state에 저장된 operation 결과를 plan에 반영해서 report를 출력함.

```bash
clairveil-payroll export-report \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-report.json
```

출력은 plan summary와 item별 `item_id`, `employee_id`, `operation_id`, `chunk_id`, `status`, `amount`, `denom`, `failure_reason`, `retry_count`를 포함함.

## Reference Payroll Daemon

`clairveil-payrolld`는 reference payroll product의 scheduler/daemon 표면임.

```bash
clairveil-payrolld \
  -state .clairveil-payroll/reservation-state.json \
  -once \
  -out .clairveil-payroll/payrolld-report.json
```

주요 flag는 다음과 같음.

| flag | 의미 |
| --- | --- |
| `-state` | `clairveil-payroll run`이 만든 durable reservation state JSON 경로 |
| `-mode` | `simulated` 또는 `live` |
| `-plan` | `live` mode에서 evidence를 operation expected value와 대조할 payroll plan JSON 경로 |
| `-tx-query` | `live` mode에서 읽을 `clairveild query tx --output json` 결과 또는 `TxObservation` JSON 경로 |
| `-nullifiers` | `live` mode에서 선택적으로 읽을 nullifier status JSON 경로 |
| `-once` | scheduler tick을 한 번 실행하고 종료함 |
| `-interval` | `-once=false`일 때 반복 실행 주기 |
| `-lease-owner` | reservation lease owner 값 |
| `-lease-ttl` | worker lease TTL |
| `-max-operations` | tick당 처리할 operation 수. `0`이면 제한 없음 |
| `-out` | `-once` report JSON 경로. 비우면 stdout 출력 |

`simulated` mode는 product rehearsal용임. 이 mode는 실제 proof를 만들거나 chain에 tx를 보내지 않음. 대신 다음을 repo-local state에서 실행함.

```text
Reserved operation 선택
-> lease 획득
-> Proving 전환
-> simulated proof artifact expected value 저장
-> ProofReady 전환
-> simulated tx metadata 저장
-> Submitted 전환
-> expected value가 일치하는 simulated evidence로 reconcile
-> ConfirmedSpent / Succeeded 전환
```

이 daemon 덕분에 운영팀은 production DB, scheduler, scanner, admin UI가 아직 없어도 payroll product의 상태 모델과 최종 report를 끝까지 시험할 수 있음.

`live` mode는 `LiveOperationExecutor`를 통해 proof, broadcast, scan 단계를 외부 구현으로 주입받는 long-running 상태머신임. CLI reference executor는 `-tx-query` 파일을 tick마다 읽어 submitted/unknown operation을 reconcile하는 최소 live wiring을 제공함. production 제품은 같은 상태머신에 실제 prover, tx broadcaster, tx/nullifier scanner를 연결해야 함.

## Repo-local Demo Product

운영팀이 바로 실행할 수 있는 최소 demo product는 아래 target으로 제공함.

```bash
make reference-payroll-demo
```

내부적으로 아래 순서를 실행함.

```text
clairveil-payroll validate
clairveil-payroll prepare-notes
clairveil-payroll plan
clairveil-payroll run
clairveil-payroll status
clairveil-payrolld -once
clairveil-payroll status
clairveil-payroll export-report -state
```

기본 입력은 `examples/reference-payroll/payroll-demo.json`이고, 출력은 `tmp/reference-payroll-demo/` 아래에 생성됨.

주요 산출물은 다음과 같음.

| 파일 | 의미 |
| --- | --- |
| `validation.json` | input validation과 note preparation summary |
| `note-preparation.json` | 준비된 note, 부족한 dummy/split/merge hint |
| `plan.json` | draft payroll plan |
| `confirmed-plan.json` | durable state에 reservation을 확정한 plan |
| `reservation-state.json` | reservation/operation durable state |
| `payrolld-report.json` | daemon tick 처리 report |
| `status-after-daemon.json` | daemon 실행 후 state summary |
| `final-report.json` | 기업 고객/운영자용 item status report |

성공 기준은 `status-after-daemon.json`에 모든 reservation이 `ConfirmedSpent`, 모든 operation이 `Succeeded`로 집계되고, `final-report.json`의 payroll status가 `Confirmed`인 것임.

## 현재 one-proof batch 검증

static conformance gate와, 필요한 local resource/development artifact를 준비한 경우 실제 node/prover/payroll workflow를 실행함.

```bash
go test ./x/privacy/client/sdk/conformance -run TestBatchTransferContract -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

live mode는 one-proof `MsgBatchTransfer` 경로, durable many-input/one-operation/many-item payroll graph, proof/broadcast worker, process/node restart, 저장한 동일 signed bytes retry, tx-hash-first reconcile, spent-nullifier conflict 처리, typed output/disclosure 검증, batch/item status 분리를 실행함. 상세 단계는 [clairveil-batch-joinsplit-localnet-tutorial-kr.md](clairveil-batch-joinsplit-localnet-tutorial-kr.md)를 따름.

## Legacy Multi-Message Live Localnet Tutorial

실제 localnet에서 legacy multi-message payroll flow를 실행하는 target도 보존함. Regression과 historical comparison에는 유용하지만 현재 one-proof protocol을 실행하는 경로는 아님.

```bash
make reference-payroll-live-localnet
```

이 target은 다음을 실제 chain 위에서 수행함.

```text
localnet init/start
-> Alice treasury note deposit
-> Alice list-notes scan
-> payroll input 생성
-> validate / prepare-notes / plan / run
-> run 재실행으로 idempotency 확인
-> 실제 transfer-batch broadcast
-> Bob recipient note scan
-> settle-transfer-batch
-> final report export
```

성공 기준은 `payroll-status-after-settle.json`에서 모든 reservation이 `ConfirmedSpent`, 모든 operation이 `Succeeded`이고, `payroll-final-report.json`의 payroll status가 `Confirmed`인 것임. `PAYROLL_CHUNK_SIZE`를 지정하면 여러 `transfer-batch` tx로 나누어 실행하고, 각 chunk는 `settle-transfer-batch -item-start -item-limit`로 plan의 해당 구간만 settle함.

상세 단계는 [clairveil-reference-payroll-live-localnet-tutorial-kr.md](clairveil-reference-payroll-live-localnet-tutorial-kr.md)를 따름.

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

`x/privacy/client/sdk/reservation.SQLStore`는 `database/sql` 기반 reference adapter임. repo는 DB driver를 고정하지 않고 제품이 PostgreSQL 또는 SQLite driver로 `*sql.DB`를 주입하게 함. 제공 schema는 다음 요구사항을 명시함.

- `owner_key_id + nullifier_lookup_key` active reservation partial unique index
- reservation/operation status index
- operation link index
- transaction-backed single-writer lock row
- JSON payload 보존
- `reservation.Store`와 같은 CAS, lease, heartbeat, reconcile 의미

schema 문자열은 `reservation.PostgreSQLSchema()`와 `reservation.SQLiteSchema()`로 얻을 수 있음. 이 adapter는 reference 수준의 transaction-backed store이므로, multi-tenant production에서는 tenant partitioning, field-level encryption, raw nullifier 비저장 정책, connection pool, migration tool, row-level lock 전략을 제품 DB 정책에 맞게 보강해야 함.

Reservation lifecycle payload는 schema version 2를 사용함. Durable JSON은 `version`에, SQL store는 `reservation_lifecycle_store_meta`에 기록함. 아직 릴리스되지 않은 이 reference는 fresh-state 초기화만 지원함. `InitSQLStore`는 비어 있거나 현재 v2인 store만 생성·검증하고, 이미 존재하는 non-v2 lifecycle store는 거부함. v1 migration과 in-place downgrade는 지원하지 않음. 이 버전에서는 새 빈 lifecycle store를 초기화해야 하며, 이전 개발 build가 만든 lifecycle snapshot을 가져오면 안 됨.

## 현재 Repo 완료 경계

2026-07-13 기준 repo-level reference boundary는 다음을 포함함.

- BatchJoinSplit16x32 reference integration, `MsgBatchTransfer`, Go batch SDK, bounded prover route, typed scanner, one-proof payroll graph/worker/reconcile, staged CLI가 구현됨.
- Batch reference conformance fixture와 `make privacy-batch-joinsplit-localnet` static gate가 통과하며 resource-heavy `RUN_LOCALNET=1` mode가 현재 actual one-proof 검증 경로임.
- disclosure policy와 key registry contract가 제공됨.
- note preparation analyzer가 제공됨.
- file-backed reference artifact store가 제공됨.
- durable reservation state store가 제공됨.
- `clairveil-payrolld` simulated/live reference daemon이 제공됨.
- `make reference-payroll-demo`로 repo-local end-to-end payroll demo를 실행할 수 있음.
- `make reference-payroll-live-localnet`은 legacy multi-message 2x2 payroll regression/tutorial로 계속 실행할 수 있음.
- `clairveil-payroll validate`, `build-input-from-notes`, `prepare-notes`, `plan`, `run`, `status`, `scan-evidence`, `reconcile`, `settle-transfer-batch`, `seed-localnet-notes`, `export-report` 명령이 제공됨.
- `transfer-batch`는 legacy multi-message 의미를 유지하고, `transfer-batch-16x32`와 staged batch command가 현재 one-proof surface를 제공함.
- One-proof handoff는 Cosmos `MsgBatchTransfer`와 canonical EVM
  `singleProofBatchTransfer` submission을 모두 정의하며 target-chain wallet/product
  E2E는 downstream release gate로 남음.
- JS SDK handoff 문서가 제공됨.
- wallet handoff 문서가 제공됨.
- downstream이 payroll workflow를 조립할 수 있는 기준 문서가 제공됨.
- reference code는 experimental이며 `PRODUCTION_RELEASE_READY`는 승인되지 않음.

## 남은 제품화 작업

이 repo가 직접 완료하지 않는 작업은 다음과 같음.

- managed production DB deployment와 tenant별 운영 schema hardening
- production-grade live scanner/reconcile daemon 운영 배포와 tenant별 hardening
- admin UI
- JS SDK 구현
- 웹/모바일 지갑 구현
- 실제 고객사의 payroll policy 결정
- staging/production rehearsal 실행
- Cosmos/EVM downstream wallet/product E2E
- formal trusted setup, external audit, signed production artifact custody/distribution
- production remote-prover isolation, authentication, deployment, operations

## Historical Legacy Phase 1 Localnet Rehearsal 기록 — 2026-07-08

2026-07-08 repo-local 1천건 restart/retry rehearsal은 `PAYROLL_SEED_NOTES=1` localnet seed mode로 통과함. 이 날짜가 있는 run은 legacy item별 2x2 proof를 사용하는 `transfer-batch` 경로를 실행했으며, seed 이후 payroll plan, reservation, 실제 Groth16 proof, 실제 multi-message tx, recipient scan, settle, final report export가 모두 동작함. Restart/retry와 durable control-plane evidence로 보존하되 현재 batch reference integration proof reduction 또는 production note preparation의 근거로 사용하면 안 됨. 결과는 [clairveil-reference-payroll-localnet-rehearsal-result-kr.md](clairveil-reference-payroll-localnet-rehearsal-result-kr.md)를 확인함.
