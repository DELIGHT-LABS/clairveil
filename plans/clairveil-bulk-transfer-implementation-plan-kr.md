# Clairveil 대량 전송 실행 계획

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | 1차 repo 구현 완료, 1차 안정화 및 1.5차 Reference Payroll Product repo 보강 완료, simulated daemon/demo 완료, live localnet tutorial 완료, production scanner/daemon integration 남음 |
| 작성일 | 2026-07-03 |
| 대상 브랜치 | `private/bulk-transfer` |
| 대상 영역 | `x/privacy` client SDK, provider, benchmark, 이후 privacy protocol |
| 1차 범위 | Note Reservation, Payroll Control Plane, Proof/Broadcast/Reconcile Queue, Multi-Message Tx, Prover Scaling, Capacity Simulation Benchmark |
| 1.5차 범위 | Reference Payroll Product, 상품화 보강, JS SDK/지갑 handoff |
| 2차 범위 | N-output batch circuit |
| 2차 권장 N | `N=32` |
| 제외 범위 | Payroll Merkle Distribution, protocol-level reservation, managed production DB deployment, customer-specific scheduler operations |

## 관련 문서

- [Clairveil 대량 전송 방안 검토 리포트](../docs/clairveil-bulk-transfer-strategy-kr.md)
- [Clairveil 대량 전송 소요시간 시뮬레이션 노트](../docs/clairveil-bulk-transfer-time-simulation-kr.md)
- [Clairveil 대량 전송에서 1/2번 방안과 Prover 수평확장만 적용하는 대안 검토](../docs/clairveil-bulk-transfer-prover-scale-option-kr.md)
- [Clairveil Note Reservation 설계 노트](../docs/clairveil-note-reservation-design-kr.md)
- [Clairveil Reference Payroll Product 가이드](../docs/clairveil-reference-payroll-product-kr.md)
- [Clairveil Reference Payroll JS SDK Handoff](../docs/clairveil-reference-payroll-js-sdk-handoff-kr.md)
- [Clairveil Reference Payroll Wallet Handoff](../docs/clairveil-reference-payroll-wallet-handoff-kr.md)

## 목적

이 계획은 Clairveil의 현재 shielded transfer 구조에서 대량 전송을 제품화 가능한 수준으로 만들기 위한 실행 순서를 정의함.

대상 시나리오는 다음 두 가지임.

- 단일 기업이 월 1회 직원 10만 명에게 급여를 push 방식으로 지급함.
- 100개 기업이 월 1회 각 1천 명에게 급여를 지급하는 SaaS형 payroll을 운영함.

1차 계획은 현재 protocol과 circuit을 유지한 상태에서 운영 레이어, SDK 레이어, worker 레이어, benchmark 레이어를 구축하는 것임. 2차 계획은 1차 benchmark 이후에도 push payroll 처리량이 부족하다고 판단될 때, N-output batch circuit을 도입해 proof 수와 tx envelope 수를 함께 줄이는 것임.

## 배경

현재 Clairveil shielded transfer는 2 input / 2 output 구조임. 일반적인 transfer는 수신자 output 1개와 송금자 change output 1개를 생성함. 따라서 현재 구조로 직원 10만 명에게 직접 지급하려면 대략 다음 작업이 필요함.

```text
proof 100,000개
tx envelope 100,000개
recipient note 100,000개
change note chain 관리
tx 결과 scan/reconcile 100,000건
```

기존 benchmark smoke 값을 단순 기준으로 삼으면 transfer proof 처리량은 prover unit 1개 기준 약 `6.9 proofs/sec` 수준임. proof 생성만 보면 10만건은 약 4시간으로 추정됨. 그러나 tx envelope도 10만개이므로 실제 운영에서는 tx 제출, 포함, scanner, retry, replan이 더 큰 병목이 될 수 있음.

문서의 시뮬레이션에서는 `1 tx/sec` 기준 현재 구조의 10만건 submit 완료 시간이 약 `27.8시간`으로 추정됨. multi-message transaction으로 transaction 하나에 `K=20`개의 `MsgTransfer`를 담으면 tx envelope 수는 `5,000개`로 줄고, 전체 시간은 proof 생성 병목인 약 `4.0시간` 수준으로 수렴함. N-output batch circuit `N=32`가 안정적으로 동작하면 10만건은 `3,125개` batch로 줄어 약 `1.5시간` 수준까지 내려갈 수 있음.

따라서 실행 순서는 다음 원칙을 따름.

1. protocol 변경 없이 운영 가능한 대량 지급 기반을 먼저 만듦.
2. note reservation과 payroll control plane을 먼저 고정해 후속 worker가 같은 상태 모델을 공유하게 함.
3. multi-message tx와 prover scaling으로 현재 구조의 한계를 측정함.
4. benchmark가 부족하다고 판단되면 N-output batch circuit을 2차로 진행함.

## 설계 원칙

### 1. 1차에서는 protocol 변경을 피함

1차 범위는 기존 `MsgTransfer`, 기존 join-split circuit, 기존 keeper validation을 최대한 재사용함. 필요한 변경은 주로 client SDK, control plane library, worker, benchmark에 둠.

`proto/clairveil/privacy/v1/tx.proto`, `x/privacy/keeper`, `x/privacy/circuit` 변경은 2차 N-output batch circuit에서 다룸.

### 2. Note Reservation은 공통 계약으로 둠

Note Reservation은 Go SDK만의 내부 구현이 아니라 JS SDK, wallet, payroll backend, split/merge worker가 함께 따라야 하는 상태 계약임.

따라서 repo에는 다음 산출물을 둠.

- Go reference implementation
- store interface
- in-memory test store
- 상태 전이 unit test
- JS SDK가 재사용할 수 있는 conformance fixture

production DB 구현은 이 repo의 1차 범위에서 제외함. PostgreSQL schema 예시는 문서와 fixture로 제공하되, 실제 DB adapter는 payroll backend 또는 JS SDK 쪽에서 구현할 수 있게 함.

### 3. Payroll Control Plane은 후속 실행기의 입력 모델임

Payroll Control Plane은 최종 사용자 UI 자체가 아니라, 대량 지급을 job/run/item/operation 단위로 표현하는 공통 모델임.

이 모델은 3차 proof queue, 4차 multi-message broadcaster, 5차 prover scaler, 6차 benchmark, 2차 N-output batch circuit adapter가 모두 입력으로 사용함.

### 4. 2차는 N-output batch circuit으로 고정함

장기 protocol 후보에는 N-output batch circuit과 Payroll Merkle Distribution이 있음. 이 계획에서는 2차를 N-output batch circuit으로 고정함.

이유는 다음과 같음.

- 현재 제품 방향이 "회사가 직원에게 직접 push 지급"에 가깝다고 가정함.
- Merkle Distribution은 claim 기반 제품 UX를 요구하므로 지급 완료의 의미가 달라짐.
- N-output batch circuit은 현재 transfer 모델의 직접 확장임.
- 1차에서 만든 reservation, payroll item, operation, report 모델을 그대로 batch adapter에 연결하기 쉬움.

## 전체 단계

```text
1차: 현재 protocol 기반 대량 전송 운영화

1. Note Reservation
2. Payroll Control Plane
3. Proof/Broadcast/Reconcile Queue
4. Multi-Message Transaction
5. Prover Scaling
6. Capacity Simulation Benchmark

2차: protocol-level throughput 개선

7. N-output Batch Circuit, 권장 N=32
```

## 현재 구현 상태

2026-07-03 기준 1차 범위는 repo 안에서 reusable SDK/reference implementation, localnet validation harness, prover pool load harness, readiness check 형태로 구현되어 있음. Production DB adapter, 운영 scheduler service, 실제 10만건 production rehearsal 결과는 아직 이 repo에 포함하지 않음.

2026-07-07 검토 기준으로, 현재 1차 구현은 상품형 payroll UX 자체가 아니라 상품화를 위한 하부 레일로 봄. 기존 `MsgTransfer`와 기존 transfer UX는 유지하면서, 대량 지급을 plan/reserve/prove/broadcast/reconcile/report 흐름으로 운영할 수 있는 기반을 만든 상태임. 실제 상품화에는 user disclosure 정책 관리, disclosure public key 관리, note preparation 운영 helper, production DB/worker/UI가 추가로 필요함.

2026-07-07 추가 결정으로 1.5차를 `Reference Payroll Product`로 정의함. 이는 protocol 필수 요소는 아니지만 `clairveil-proverd`처럼 downstream adoption에 필요한 companion/reference product로 제공하는 것을 목표로 함. 샘플이더라도 대충 만든 demo가 아니라, 실제 클라이언트가 그대로 가져가거나 fork해 production product의 출발점으로 삼을 수 있는 품질을 지향함.

2026-07-07 후속 구현으로 1.5차 repo 보강은 `validate`, `prepare-notes`, `plan`, `run`, `status`, `reconcile`, `export-report` CLI, file-backed reference artifact store, durable reservation state store, note preparation operation hint까지 확장됨. `run`은 plan을 durable reservation/operation state로 확정하고, `reconcile`은 evidence JSON으로 durable state를 갱신함.

2026-07-07 추가 구현으로 `clairveil-payrolld` simulated reference daemon과 `make reference-payroll-demo`를 추가함. 이제 운영팀은 repo만으로 sample payroll input을 검증하고, note preparation을 확인하고, plan/reservation state를 만들고, daemon tick으로 `Reserved -> ProofReady -> Submitted -> ConfirmedSpent` 흐름을 시뮬레이션한 뒤 final report까지 볼 수 있음.

2026-07-08 추가 구현으로 `make reference-payroll-live-localnet`과 `clairveil-payroll build-input-from-notes`, `settle-transfer-batch`를 추가함. 이 경로는 실제 localnet에서 treasury note deposit, note scan, payroll plan/reservation, 실제 `transfer-batch` broadcast, recipient note scan, payroll state settle, final report export까지 검증함. 같은 날 `clairveil-payroll scan-evidence`와 SDK `EvidenceScanner`를 추가해 tx event/nullifier evidence를 durable reservation state에 적용할 수 있게 함. 남은 production 보강은 long-running `clairveil-payrolld` live scheduler를 붙이는 것임.

| 단계 | 상태 | 구현 위치 |
| --- | --- | --- |
| Phase 1. Note Reservation | 구현 완료 | `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json` |
| Phase 2. Payroll Control Plane | 구현 완료 | `x/privacy/client/sdk/payroll` |
| Phase 3. Proof/Broadcast/Reconcile Queue | 구현 완료 | `x/privacy/client/sdk/payroll/proof_queue.go`, `broadcast_queue.go`, `reconcile_worker.go`, `retry_policy.go` |
| Phase 4. Multi-Message Transaction | 구현 및 localnet 검증 harness 완료 | `x/privacy/client/sdk/payroll/chunker.go`, `batch_broadcaster.go`, `sdk_broadcaster.go`, `x/privacy/client/sdk/provider/tx.go`, `x/privacy/client/cli/tx_transfer_batch.go`, `scripts/privacy-transfer-batch-localnet-bench.sh` |
| Phase 5. Prover Scaling | 구현 및 pool load harness 완료 | `x/privacy/client/sdk/payroll/prover_pool.go`, `cmd/clairveil-proverload`, `scripts/privacy-proverd-scale-bench.sh` |
| Phase 6. Capacity Simulation Benchmark | 시뮬레이션 및 readiness harness 완료 | `cmd/clairveil-bulktransferbench`, `scripts/privacy-bulk-transfer-bench.sh`, `scripts/privacy-bulk-readiness-check.sh`, `make privacy-bulk-readiness-check` |
| Phase 1.5. Reference Payroll Product | repo 보강 완료, simulated daemon/demo 완료, live localnet tutorial 완료, production integration 남음 | `cmd/clairveil-payroll`, `cmd/clairveil-payrolld`, `examples/reference-payroll`, `scripts/reference-payroll-live-localnet.sh`, `x/privacy/client/sdk/payroll`, handoff 문서 |
| Phase 7. N-output Batch Circuit | 미구현 | 2차에서 `BatchJoinSplit32`로 진행 예정임 |

## 1차 계획

### Phase 1. Note Reservation

#### 목표

대량 지급 계획에서 선택한 input note가 일반 transfer, split, merge, 다른 payroll job에 의해 먼저 소비되지 않도록 client/control plane 수준의 reservation 모델을 구현함.

#### repo 작업

새 패키지를 추가함.

```text
x/privacy/client/sdk/reservation/
  types.go
  status.go
  store.go
  service.go
  lease.go
  reconcile.go
  lookup_key.go
  errors.go
  memory_store.go
  service_test.go
  lease_test.go
  reconcile_test.go
  lookup_key_test.go
```

conformance fixture를 추가함.

```text
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
x/privacy/client/sdk/conformance/note_reservation_contract_test.go
```

#### 구현 내용

- `NoteInventory`, `NoteReservation`, `PayrollOperation` 타입을 정의함.
- `Available`, `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `ManualReview`, `ConfirmedSpent`, `Released`, `ReplanRequired` 상태를 정의함.
- active reservation 상태를 정의함.
- 허용되는 상태 전이를 코드로 고정함.
- 같은 `owner_key_id + nullifier_lookup_key` 조합에 active reservation이 하나만 존재하도록 store contract를 정의함.
- 여러 reservation과 operation을 batch로 생성할 때 하나라도 실패하면 아무 것도 기록하지 않는 원자적 batch reserve contract를 정의함.
- 상태 변경은 compare-and-set 방식으로 수행함.
- proof worker와 broadcaster가 사용할 lease/heartbeat 규칙을 구현하고, worker-owned 상태 전이는 현재 lease token을 요구함.
- lease 획득, heartbeat, lease clear, worker-owned `ProofReady -> Submitted/Unknown`은 store 단위 원자적 연산으로 제공함. `ProofReady -> ConfirmedSpent` recovery는 chain evidence 기반 compare-and-set/transaction 경로로 처리함.
- proof worker는 `Reserved` 상태 한정 lease 획득과 proof 생성 중 heartbeat를 사용함.
- `nullifier_lookup_key = HMAC(index_key, nullifier)` 형태의 lookup key helper를 제공함.
- `nullifier_lookup_key_id` 또는 `lookup_key_version`을 포함하고, conformance fixture에 HMAC test vector를 포함함.
- tx hash 조회, nullifier 조회, expected value 비교를 통한 reconcile helper를 구현함.
- note 상태와 operation 성공 판정을 분리함.

#### JS SDK 영향

Go 구현은 JS SDK를 대체하지 않음. 대신 JS SDK가 따라야 할 공통 상태 계약을 명확히 함.

JS SDK 개발자는 다음을 맞추면 됨.

- 상태 enum 이름
- active reservation 정의
- 상태 전이 규칙
- lease token 규칙
- operation 성공 판정 규칙
- conformance fixture 결과

따라서 JS SDK 선행 작업을 허사로 만들지 않으려면 Go implementation을 production DB의 정답으로 강제하지 않고, reference implementation과 contract test로 제공해야 함.

#### 완료 기준

- 상태 전이 unit test가 통과함.
- active reservation 중복 방지 test가 통과함.
- batch reserve가 실패할 때 partial reservation을 남기지 않음.
- lease 만료, heartbeat, zombie worker 방지 test가 통과함.
- `Submitted`, `Unknown`, `ManualReview`가 TTL만으로 `Available` 처리되지 않음.
- nullifier spent와 operation success가 분리되어 판정됨.
- JS SDK가 읽을 수 있는 conformance fixture가 추가됨.

### Phase 2. Payroll Control Plane

#### 목표

대량 지급을 job/run/item/operation 단위로 계획하고, 각 지급 건에 사용할 note reservation을 연결하는 control plane SDK를 구현함.

#### repo 작업

새 패키지를 추가함.

```text
x/privacy/client/sdk/payroll/
  types.go
  status.go
  validator.go
  note_allocator.go
  service.go
  report.go
  errors.go
  validator_test.go
  note_allocator_test.go
  service_test.go
```

#### 구현 내용

- `PayrollJob`, `PayrollRun`, `PayrollItem`, `PayrollPlan`, `PayrollOperation` 타입을 정의함.
- recipient shielded address, amount, denom, duplicate row를 검증함.
- payroll item별 expected value를 저장할 수 있게 함.
- treasury note inventory에서 `Available` note만 선택함.
- plan 확정 시점에 reservation을 원자적으로 생성함.
- item별 `reservation_id`, `operation_id`, `batch_id`, `chunk_id`를 연결함.
- 실패 item만 재계획할 수 있게 함.
- report DTO를 정의해 지급 결과, 실패 사유, retry 이력을 출력할 수 있게 함.

#### 후속 단계에서 쓰이는 방식

- Phase 3 proof queue는 `PayrollOperation`을 proof job으로 변환함.
- Phase 4 multi-message broadcaster는 `PayrollOperation`을 chunk로 묶음.
- Phase 5 prover scaling은 `PayrollOperation` proof job을 여러 worker에 분배함.
- Phase 6 benchmark는 synthetic payroll input을 `PayrollPlan`으로 변환해 전체 처리량을 측정함.
- Phase 7 N-output batch circuit은 `PayrollItem`들을 `BatchJoinSplit32` witness input으로 변환함.

#### 완료 기준

- 1천건 및 10만건 synthetic payroll plan 생성이 가능함.
- reservation service와 연동해 plan 확정 시 note가 `Reserved`로 전환됨.
- plan 확정 중 reservation conflict가 나면 partial reservation이 남지 않음.
- duplicate recipient, invalid address, insufficient note, reservation conflict가 분류됨.
- 실패 item만 `ReplanRequired`로 분리할 수 있음.
- report DTO가 operation status와 reservation status를 함께 표현함.

### Phase 3. Proof/Broadcast/Reconcile Queue

#### 목표

Payroll plan을 실제 transfer execution으로 연결함. proof 생성, transaction 생성, broadcast, confirmation scan, retry, reconcile을 worker 기반으로 실행함.

#### repo 작업

`payroll` 패키지에 runner와 worker를 추가하거나 별도 하위 패키지를 둠.

```text
x/privacy/client/sdk/payroll/
  proof_queue.go
  broadcast_queue.go
  reconcile_worker.go
  retry_policy.go
  sdk_broadcaster.go
  runner_test.go
```

필요하면 provider와 transfer helper를 보강함.

```text
x/privacy/client/sdk/provider/tx.go
x/privacy/client/sdk/transfer/broadcast.go
```

#### 구현 내용

- `Reserved` 상태의 operation을 가져와 proof job으로 실행함.
- proof worker는 lease를 획득한 뒤 lease-token guarded transition으로 `Reserved -> Proving -> ProofReady`를 진행하고 기존 transfer SDK로 `MsgTransfer`를 생성함.
- replan attempt는 기존 payment item과 다른 `operation_id`/`reservation_id`를 사용해 이전 operation과 충돌하지 않게 함.
- proof 생성이 끝나면 proof/message/payload 결과를 `ProofResultSink` 같은 durable queue 또는 artifact store에 저장하고, 저장 성공 후 reservation을 `ProofReady`로 전환함.
- broadcaster는 `ProofReady` 상태만 제출하며, 제출 직전 `NullifierChecker`로 input nullifier가 아직 unspent인지 확인함. checker가 없으면 제출하지 않음.
- broadcast 후 `tx_hash`, `tx_bytes_hash`, `sign_doc_hash`, `account_sequence`를 저장함.
- tx result가 불명확하면 즉시 새 tx를 만들지 않고 `Unknown`으로 보내며, retry policy는 `ReconcileUnknown`으로 분류해 reconcile worker가 `tx_hash`와 nullifier 상태를 먼저 확인함.
- nullifier spent만으로 payroll success를 인정하지 않음.
- expected output commitment, audit disclosure digest, recipient hash, amount, denom이 operation과 일치해야 success로 처리함.
- RPC timeout, mempool eviction, gas/sequence 문제, proof invalid, nullifier spent를 서로 다르게 분류함.

#### 완료 기준

- proof worker가 같은 reservation을 중복 처리하지 않음.
- `ProofReady`로 전환된 operation은 broadcast worker가 다시 읽을 수 있는 proof/message artifact를 가지고 있음.
- broadcaster가 stale lease로 tx를 제출하지 않음.
- broadcast retry가 `operation_id` 기준으로 idempotent하게 동작함.
- tx hash 조회 실패 후 nullifier 조회로 note 상태를 reconcile할 수 있음.
- 증거가 부족한 spent note는 operation-level `ConflictSpent` 또는 `ManualReview`로 분리됨.

### Phase 4. Multi-Message Transaction

#### 목표

기존 `MsgTransfer`를 유지하면서 여러 transfer message를 하나의 Cosmos transaction envelope에 담아 tx envelope 수를 줄임.

#### repo 작업

`payroll` 패키지에 chunk planner와 batch broadcaster를 추가함.

```text
x/privacy/client/sdk/payroll/
  chunker.go
  batch_broadcaster.go
  sdk_broadcaster.go
  chunker_test.go
```

필요하면 단건 중심 transfer broadcaster를 다건 helper로 확장함.

```text
x/privacy/client/sdk/provider/tx.go
x/privacy/client/cli/tx_transfer_batch.go
scripts/privacy-transfer-batch-localnet-bench.sh
```

#### 구현 내용

- `ProofReady` operation을 `K`개 단위 chunk로 묶음.
- 1차 benchmark 기준 `K=5`, `K=10`, `K=20`, `K=50`을 시험함.
- 같은 chunk 안에 duplicate nullifier가 없는지 검사함.
- gas limit, tx size, event size 기준으로 chunk 크기를 조정함.
- `CosmosTxBroadcaster.BroadcastSDKMessages(ctx, msgs ...sdk.Msg)`와 payroll `SDKMessageBroadcasterAdapter` 경로를 활용함.
- chunk 단위 tx hash와 item index mapping을 저장함.
- chunk 실패 시 전체 chunk retry 또는 item 분리 retry를 지원함.

#### 완료 기준

- 하나의 tx에 여러 `MsgTransfer`를 담아 localnet에서 성공적으로 처리함.
- chunk 안의 nullifier 중복을 사전에 거부함.
- `K=5`, `K=10`, `K=20`, `K=50`별 gas/size/inclusion 결과를 측정할 수 있음.
- 실패한 chunk에서 item 단위 replan 또는 smaller chunk retry가 가능함.
- `make privacy-transfer-batch-localnet-bench`로 localnet에서 multi-message envelope를 재현하고 `message_count`, `tx_json_size_bytes`, gas 사용량을 기록할 수 있음.

### Phase 5. Prover Scaling

#### 목표

proof 생성 병목을 줄이기 위해 여러 prover unit을 사용하는 worker pool을 구현하고 측정함.

#### repo 작업

payroll runner에 prover pool을 추가함.

```text
x/privacy/client/sdk/payroll/
  prover_pool.go
  prover_pool_test.go
```

1차 구현에서는 payroll runner가 사용할 prover pool abstraction을 추가하고, 기존 prover load 도구도 여러 endpoint를 하나의 pool처럼 round-robin 측정할 수 있게 확장함.

```text
x/privacy/client/sdk/payroll/prover_pool.go
x/privacy/client/sdk/payroll/prover_pool_test.go
cmd/clairveil-proverload/main.go
scripts/privacy-proverd-scale-bench.sh
```

필요하면 prover transport에 batch-friendly metadata를 추가함.

```text
x/privacy/client/sdk/provertransport/
x/privacy/client/sdk/proverservice/
```

#### 구현 내용

- prover endpoint 여러 개를 대상으로 proof job을 분산함.
- per-endpoint concurrency limit을 둠.
- endpoint별 동시 실행 개수를 제한함.
- request timeout과 context cancellation을 처리함.
- endpoint 장애 시 다음 endpoint로 failover함.
- lease 만료 시 stale worker 결과는 proof worker의 lease-token guarded transition에서 거부함.
- prover unit `1`, `2`, `4`, `8`, `16`개에서 처리량을 측정함.

#### 완료 기준

- prover worker pool이 proof job을 병렬 처리함.
- endpoint 장애 시 다른 endpoint로 retry할 수 있음.
- endpoint별 concurrency limit을 적용할 수 있음.
- stale lease 결과를 proof worker 상태 전이에서 거부함.
- prover unit 수별 예상 throughput이 benchmark report에 기록됨.
- `PROVERD_URLS=url1,url2 make privacy-proverd-scale-bench`로 endpoint 수, endpoint별 request 분산, aggregate requests/sec, latency, timeout/error rate를 측정할 수 있음.

### Phase 6. Capacity Simulation Benchmark

#### 목표

1차 구현으로 실제 목표 시나리오를 어느 정도 처리할 수 있을지 capacity simulation으로 추정함. 이 결과를 바탕으로 2차 N-output batch circuit의 필요성과 목표를 확정함. 실제 localnet 10만건 실행과 장애 주입 E2E benchmark는 이 harness를 기반으로 한 후속 운영 검증으로 둠.

#### repo 작업

bulk transfer 전용 시뮬레이션 benchmark와 report script를 추가함.

```text
cmd/clairveil-benchreport/main.go
cmd/clairveil-bulktransferbench/main.go
cmd/clairveil-bulktransferbench/main_test.go
scripts/privacy-bulk-transfer-bench.sh
scripts/privacy-bulk-readiness-check.sh
Makefile
```

#### benchmark 시나리오

- 단일 기업 `100,000`건 payroll
- `100`개 기업 x `1,000`건 payroll
- chunk size `K=5`, `K=10`, `K=20`, `K=50`
- prover unit `1`, `2`, `4`, `8`, `16`
- tx 처리량 가정 또는 실제 localnet 측정값별 비교
- reservation conflict, replan, manual review가 없는 정상 capacity path

#### 측정 지표

- payroll item/sec
- proof/sec
- tx envelope/sec
- proof count
- tx envelope count
- chunk count
- proof generation seconds
- tx submit seconds
- estimated total seconds
- reservation conflict count
- replan count
- `ManualReview` count

#### 완료 기준

- 10만건 단일 기업의 예상 완료 시간이 산출됨.
- 100개 기업 x 1천건의 global capacity 추정치가 산출됨.
- `K`와 prover unit 수에 따른 병목 전환 지점이 확인됨.
- 2차 N-output batch circuit 진입 여부를 판단할 수 있는 수치가 확보됨.

#### 실행 방법

기본 시나리오는 아래 명령으로 실행함.

```bash
make privacy-bulk-transfer-bench
```

임시 출력 경로를 지정하려면 아래처럼 실행함.

```bash
BENCH_OUT_DIR="$(mktemp -d)" ./scripts/privacy-bulk-transfer-bench.sh
```

benchmark command는 `single-company-100k`, `hundred-companies-1k` 시나리오, chunk size, prover unit 수, proof/sec, tx/sec 가정을 입력받아 `bulk-summary.json`과 benchreport markdown/json 산출물을 생성함.

## 1차 후속 검증 및 운영 준비

1차 repo 구현은 완료되었지만, production 적용 전에는 다음 검증을 별도 실행해야 함.

### Repo 안에서 실행할 검증

```bash
make privacy-bulk-readiness-check
```

기본 readiness check는 다음을 실행함.

- reservation/payroll/proverload/localnetload/bulktransferbench critical unit test
- active reservation duplicate, compare-and-set, lease token, reconcile evidence mismatch 같은 failure invariant test
- 10만건 synthetic bulk capacity benchmark

무거운 검증은 필요할 때 옵션으로 켬.

```bash
RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=5 make privacy-bulk-readiness-check
```

위 명령은 localnet smoke에 `transfer-batch`를 추가해 multi-message transaction envelope가 실제 chain에서 처리되는지 확인함.

```bash
RUN_PROVER_SCALE=1 PROVERD_URLS=http://127.0.0.1:9090,http://127.0.0.1:9091 make privacy-bulk-readiness-check
```

위 명령은 external `clairveil-proverd` pool을 대상으로 aggregate load를 측정함.

### 제품/운영 팀이 이어서 해야 하는 검증

repo는 protocol과 SDK 기준 reference implementation, durable control-plane adapter, local harness, synthetic benchmark를 제공함. 실제 제품화에는 다음 작업이 추가로 필요함.

- managed production DB 선택 시 PostgreSQL transaction lock, partial unique index, HMAC lookup key, field-level encryption 적용
- `DurableFileStore` 또는 production DB adapter를 tenant별 운영 정책에 맞게 배포
- production-grade live proof/broadcast worker와 chain scanner를 `clairveil-payroll run`으로 생성된 durable state에 연결
- operator UI, alert, manual review flow 구현
- 실제 1천건, 1만건, 10만건 rehearsal runbook 작성 및 실행
- JS SDK/wallet storage와 note reservation 상태 계약의 conformance 확인

이 항목들은 repo 내부 reference code를 운영 환경에 연결하고 검증하는 작업이며, 제품팀 전달 문서에서 별도 owner와 산출물을 정의해야 함.

## 1.5차 Reference Payroll Product 계획

1.5차의 목표는 repo 사용자가 core module만 보고 제품 레이어 부재 때문에 대량전송을 못 쓰는 일을 줄이는 것임. 이 레이어는 `clairveil-proverd`와 같은 companion product 성격임. Core protocol에 필수는 아니지만, web/mobile/backend/downstream이 실제 product workflow를 붙일 때 기준점이 됨.

### 원칙

- 샘플 제품이지만 production reference 품질로 작성함.
- 기존 `MsgTransfer`, 기존 circuit, 기존 keeper semantics를 유지함.
- 1.5차에서는 JS SDK와 지갑 구현을 직접 하지 않음.
- JS SDK 팀과 지갑팀이 해야 할 작업은 handoff 문서, fixture, schema, conformance 기준으로 제공함.
- reference product는 privacy-sensitive data를 다루므로 기본 logging, report, storage 정책을 보수적으로 둠.
- note preparation, user disclosure, durable worker, status/report는 실제 상품화에 필요한 최소 레이어로 취급함.

### Phase 1.0 안정화

목표는 이미 구현된 1차 foundation이 흔들리지 않도록 검증과 경계 조건을 보강하는 것임.

작업 항목은 다음과 같음.

- reservation/payroll/prover/bulk readiness test를 다시 실행함.
- 기존 단건 transfer UX가 bulk 구현으로 바뀌지 않았음을 문서화함.
- `transfer-batch`가 readiness/capacity command라는 제한을 명확히 유지함.
- failure invariant, duplicate nullifier, lease token, reconcile evidence mismatch test가 계속 통과하는지 확인함.
- 문제가 발견되면 작은 fix commit으로 분리함.

산출물은 다음과 같음.

- stabilization test 결과
- 필요한 경우 regression test 또는 문서 보강
- readiness check 통과 기록

### Phase 1.5.1 Reference product scope

Reference Payroll Product는 다음 기능을 목표로 함.

```text
payroll import
-> validate
-> plan preview
-> note preparation analysis
-> reservation
-> proof queue
-> batch broadcast
-> reconcile
-> status/report export
```

초기 산출물은 CLI/library/daemon 중심으로 둠. API server와 admin UI는 뒤 단계에서 붙일 수 있게 interface를 먼저 고정함.

### Phase 1.5.2 User disclosure policy model

상품형 payroll에서는 회사별 또는 지급건별 user disclosure 정책이 필요할 수 있음. Payroll input, plan item, operation record에 user disclosure 정책과 expected digest를 연결할 수 있어야 함.

필요한 필드는 다음과 같음.

- `user_privacy_policy`
- `user_disclosure_mode`
- `user_disclosure_target_pubkey`
- `expected_user_disclosure_digest`
- `expected_audit_disclosure_digest`
- `expected_self_view_disclosure_digest`

구현 방향은 다음과 같음.

- Go SDK에 reference type과 validator를 추가함.
- 기존 `transfer-batch` CLI는 capacity/readiness 목적상 `all-private` / `none` 제한을 유지함.
- reference payroll product는 per-item disclosure policy를 plan 단계에서 검증하고, proof/payload 생성 시 기존 transfer SDK disclosure config로 변환함.

### Phase 1.5.3 Disclosure public key registry contract

`recipient-encrypted` user disclosure를 지원하려면 disclosure public key 관리가 필요함.

Reference product는 다음 contract를 제공함.

- employee/company/auditor/external recipient 단위 key entry model
- key id 또는 key version
- pubkey format validation
- payroll import 시 missing/invalid key 검출
- key 원문 로그 금지
- report에는 key id와 digest/축약값만 표시

실제 registry DB와 admin UI는 제품 구현 영역이지만, repo는 reference type, validation helper, handoff 문서를 제공함.

### Phase 1.5.4 Note preparation helper

현재 `transfer-batch`는 준비된 spendable note가 이미 있다는 가정이 강함. 상품형 payroll에는 plan 실행 전에 필요한 exact/pairable note와 zero dummy note를 준비하는 helper가 필요함.

Reference helper는 다음을 제공함.

```text
treasury note inventory
payroll item amount list
target denom
preparation policy
-> 준비 필요 여부
-> ready item count
-> missing dummy count
-> split/merge recommendation
-> blocking reason
```

초기 구현은 자동 split/merge tx를 직접 실행하기보다 analyzer와 recommendation을 먼저 제공함. 이후 reference CLI/service에서 operator approval 또는 auto-prepare mode를 붙일 수 있음.

2026-07-07 구현 상태: `AnalyzeNotePreparation`은 recommendation과 함께 `operation_hints`를 제공함. Product layer는 이 hint를 `make-dummy`, `split-merge`, `add-funds`, `resolve-reservation-lock` 준비 작업 후보로 표시하거나 operator approval flow로 넘길 수 있음.

### Phase 1.5.5 Durable store / worker contract

현재 repo에는 in-memory reference store가 있음. Reference product는 production DB adapter가 따라야 할 contract를 명확히 해야 함.

필요한 산출물은 다음과 같음.

- reservation store contract 문서 보강
- PostgreSQL/SQLite adapter 후보 설계
- transaction lock 요구사항
- active reservation unique constraint
- worker lease/heartbeat persistence 요구사항
- operation evidence persistence 요구사항

초기에는 durable DB 구현보다 interface와 test contract를 우선함. 다만 reference CLI/service가 필요하면 SQLite adapter부터 시작할 수 있음.

2026-07-07 구현 상태: `x/privacy/client/sdk/payroll.FileArtifactStore`와 `x/privacy/client/sdk/reservation.DurableFileStore`를 추가함. `FileArtifactStore`는 `plans`, `plan-reports`, `note-preparation-reports`, `disclosure-keys`를 파일로 저장하고 다시 읽는 local/reference artifact store임. `DurableFileStore`는 `reservation.Store` contract를 만족하는 durable reservation/operation state adapter이며, active reservation uniqueness, compare-and-set, lease/heartbeat, operation evidence update를 snapshot JSON에 저장함. 파일은 `0600`, 디렉토리는 `0700`으로 생성함. 실제 고객 환경에서 PostgreSQL/MySQL/cloud DB를 쓰는 경우에도 이 store와 같은 상태 전이 의미를 지켜야 함.

### Phase 1.5.6 Reference CLI/service 후보

초기 reference product command 후보는 다음과 같음.

```text
clairveil-payroll validate
clairveil-payroll plan
clairveil-payroll prepare-notes
clairveil-payroll run
clairveil-payroll status
clairveil-payroll scan-evidence
clairveil-payroll reconcile
clairveil-payroll settle-transfer-batch
clairveil-payroll export-report
clairveil-payrolld
```

2026-07-07 구현 상태: `validate`, `prepare-notes`, `plan`, `run`, `status`, `reconcile`, `export-report`를 구현함. `prepare-notes`와 `plan`은 `-store-dir`로 file-backed artifact store에 결과를 저장할 수 있음. `run`은 `-state` durable reservation state에 plan을 확정하며 재실행 idempotency를 제공함. `status`는 plan 또는 state 기준 집계를 제공함. `reconcile`은 evidence JSON을 받아 reservation/operation 상태를 갱신함.

2026-07-07 추가 구현 상태: `clairveil-payrolld`를 추가함. 현재 mode는 `simulated`이며, durable reservation state 위에서 proof ready, submitted, reconcile 상태 전이를 시뮬레이션함. `scripts/reference-payroll-demo.sh`와 `make reference-payroll-demo`는 sample input으로 validate, prepare, plan, run, daemon tick, status, final report export를 한 번에 실행함.

2026-07-08 추가 구현 상태: `build-input-from-notes`, `settle-transfer-batch`, `scripts/reference-payroll-live-localnet.sh`, `make reference-payroll-live-localnet`을 추가함. 이 경로는 실제 localnet에서 `transfer-batch` tx를 실행하고 recipient note delta를 확인한 뒤 payroll final report를 `Confirmed`로 만든다.

2026-07-08 production scanner 보강 상태: `x/privacy/client/sdk/payroll.EvidenceScanner`와 `clairveil-payroll scan-evidence`를 추가함. scanner는 `clairveild query tx --output json` 결과에서 `shielded_transfer` event를 읽고, `commitment_1`, disclosure digest, nullifier spent 상태를 payroll item/reservation별 reconcile evidence로 변환함. `-apply`를 사용하면 기존 `reconcile`과 같은 durable state update 경로로 즉시 반영함.

1.5차 repo 완료 기준은 production 실운영 daemon을 대신하는 것이 아니라, 운영팀이 repo만으로 payroll product 상태 모델과 실제 localnet tx 경로를 끝까지 체험하고, production scanner/daemon 구현 전까지 필요한 durable control-plane workflow를 code와 문서로 조립 가능하게 만드는 것임.

### Phase 1.5.7 JS SDK handoff

JS SDK 팀이 직접 구현해야 할 항목은 이 repo에서 대신 구현하지 않음. 대신 다음을 제공함.

- JS SDK handoff 문서
- reservation/payroll/disclosure type mapping
- fixture/schema 요구사항
- expected evidence helper 요구사항
- batch nullifier query 사용 가이드
- prover HTTP client 연동 가이드

### Phase 1.5.8 Wallet handoff

지갑팀이 직접 구현해야 할 항목은 이 repo에서 대신 구현하지 않음. 대신 다음을 제공함.

- wallet handoff 문서
- reserved note exclusion requirement
- disclosure public key UX requirement
- wallet DB migration requirement
- batch nullifier sync requirement
- payroll incoming note 표시 정책
- privacy-safe logging policy

### Phase 1.5 완료 기준

- 1차 readiness check가 통과함.
- Reference payroll product scope와 workflow가 계획 문서에 고정됨.
- user disclosure policy와 disclosure key registry contract가 Go SDK 또는 문서로 제공됨.
- note preparation helper가 최소 analyzer/recommendation 수준으로 제공됨.
- note preparation helper가 operation hint를 제공함.
- reference CLI가 `validate`, `prepare-notes`, `plan`, `run`, `status`, `reconcile`, `export-report`를 제공함.
- reference CLI가 `build-input-from-notes`, `settle-transfer-batch`를 제공함.
- `clairveil-payrolld`가 simulated scheduler/daemon tick을 제공함.
- `make reference-payroll-demo`가 repo-local end-to-end payroll demo를 제공함.
- `make reference-payroll-live-localnet`이 실제 localnet payroll transfer-batch tutorial을 제공함.
- file-backed reference artifact store가 제공됨.
- durable reservation state store가 제공됨.
- JS SDK 팀과 지갑팀 handoff 문서가 추가됨.
- downstream이 "core는 있는데 제품 레이어가 없어 못 쓰는" 상태를 피할 수 있는 최소 reference product 경로가 생김.

## 상품화 보강 TODO

2026-07-07 검토에서 현재 1차 구현만으로는 바로 상품형 payroll 대량전송으로 보기 어렵다는 점을 기록함. 현재 repo 구현은 안전한 대량전송 실행 기반이며, 실제 고객-facing 제품에는 아래 보강 작업이 필요함.

### 1. User disclosure 정책 모델 보강

현재 `transfer-batch` CLI는 capacity/readiness 검증을 위해 `all-private` / `none` 중심으로 제한되어 있음. 이 제한은 mandatory audit disclosure를 끄는 의미가 아니라, 사용자 선택 공개를 제품 복잡도에서 분리해 multi-message 제출, note 충돌, gas/size, proof 병목 검증에 집중하기 위한 것임.

상품형 payroll에서는 회사별 또는 지급건별 user disclosure 정책이 필요할 수 있음. 따라서 payroll input, payroll plan item, operation record에 다음 정책 필드를 추가하거나 연결해야 함.

- `user_privacy_policy`
- `user_disclosure_mode`
- `user_disclosure_target_pubkey`
- `expected_user_disclosure_digest`
- `expected_audit_disclosure_digest`
- `expected_self_view_disclosure_digest`

이 보강이 있어야 payroll report와 reconcile worker가 "어떤 지급건이 어떤 공개 정책으로 생성되었는지"와 "tx/event의 disclosure digest가 의도한 operation과 일치하는지"를 item 단위로 판정할 수 있음.

### 2. Disclosure public key 관리

`recipient-encrypted` user disclosure를 payroll 제품에서 지원하려면 recipient 또는 disclosure recipient의 public key를 안정적으로 관리해야 함. 단순히 shielded address만 저장하는 것으로는 충분하지 않음.

필요한 기능은 다음과 같음.

- payroll import 시 disclosure public key를 함께 입력하거나 조회함.
- employee, company, auditor, external recipient 단위 disclosure key registry를 둠.
- key 누락, key rotation, 잘못된 key format, 만료된 key를 plan 단계에서 검출함.
- key id 또는 key version을 operation expected value와 함께 저장함.
- disclosure payload 원문은 민감정보로 취급하고, report/telemetry에는 digest 또는 축약값만 남김.

이 작업은 JS SDK, wallet, payroll backend가 함께 맞춰야 하는 제품 계약임. repo 안에서는 우선 Go reference type과 conformance fixture 확장으로 계약을 고정하고, production registry와 UI는 제품 repo에서 구현하는 방향이 적절함.

### 3. Note preparation helper

현재 multi-message `transfer-batch`는 준비된 spendable note가 이미 있다는 가정이 강함. recursive split/merge planner를 batch CLI 안에서 자동 실행하지 않는 이유는, split/merge가 중간 tx, block wait, rescan, reservation, replan, failure recovery를 동반하기 때문임. 이 로직은 단일 CLI 명령보다 payroll control plane 또는 scheduler가 담당하는 편이 안전함.

상품형 payroll에는 별도의 note preparation 단계가 필요함.

```text
treasury note scan
-> payroll 총액과 item별 amount 계산
-> 필요한 exact/pairable note 및 zero dummy note 수량 산출
-> 큰 treasury note를 shard note로 split
-> 지나치게 작은 note는 merge
-> 준비 tx 포함 대기
-> wallet rescan 및 nullifier 상태 갱신
-> 준비된 note를 payroll plan에 reservation
```

예상 산출물은 다음과 같음.

- `payroll prepare-notes` 또는 backend API
- treasury note inventory analyzer
- dummy note 부족 감지 및 생성 helper
- shard split plan generator
- merge plan generator
- preparation tx retry/reconcile flow
- preparation 완료 후 payroll run 가능 여부 report

이 helper가 없으면 사용자는 `transfer-batch` 또는 payroll run 단계에서 "batch item needs note preparation before batching" 류의 실패를 자주 만나게 됨. 상품 UX에서는 실행 직전에 실패시키기보다 plan/prepare 단계에서 필요한 note 상태를 만들어두는 것이 맞음.

### 4. Production worker와 DB adapter

현재 reservation/payroll 구현은 Go SDK/reference implementation과 in-memory store를 포함함. 상품화에는 durable DB와 worker orchestration이 필요함.

필요한 작업은 다음과 같음.

- PostgreSQL 또는 제품 DB 기반 reservation store adapter
- `owner_key_id + nullifier_lookup_key` active unique constraint
- `FOR UPDATE SKIP LOCKED` 또는 동등한 note selection lock
- compare-and-set 상태 전이
- worker lease/heartbeat persistence
- proof artifact store
- broadcast attempt store
- operation evidence store
- retry/reconcile scheduler
- manual review queue

이 작업은 core protocol 변경이 아니라 제품 운영 인프라에 가까움. 다만 repo의 `x/privacy/client/sdk/reservation.Store`와 `x/privacy/client/sdk/payroll` interface 의미를 그대로 지켜야 함.

### 5. Product UX와 report

고객-facing payroll 제품은 단순히 transfer를 많이 보내는 기능이 아니라, 운영자가 job 상태를 이해하고 실패를 복구할 수 있는 도구여야 함.

필요한 UX는 다음과 같음.

- payroll CSV/HR import
- recipient shielded address와 disclosure key 검증
- payroll plan preview
- note preparation 필요 여부 표시
- estimated proof time, tx envelope count, chunk count 표시
- run start/stop/retry
- item별 `Planned`, `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `Succeeded`, `Failed`, `ManualReview` 상태 표시
- 실패 원인별 필터와 retry
- 기업 고객용 완료 report export
- 운영자 manual review flow

이 UX가 있어야 1차 repo 구현이 실제 상품으로 연결됨.

### 6. JS SDK / wallet 연동

JS SDK와 wallet은 Go reference를 그대로 복사하지 않아도 되지만, 상태 의미와 fixture는 맞춰야 함.

필수 작업은 다음과 같음.

- note reservation status enum 반영
- reserved note를 일반 transfer, split, merge 후보에서 제외
- batch nullifier check 사용
- disclosure public key lookup/import flow
- payroll item별 expected evidence 저장 또는 backend와 동기화
- proof service 또는 browser prover 연동
- wallet rescan 후 prepared note inventory 갱신
- conformance fixture 검증을 CI에 포함

이 작업이 없으면 backend가 note를 reserved로 봐도 wallet이 같은 note를 일반 transfer에 써버리는 식의 제품 장애가 발생할 수 있음.

### 7. 상품화 전 의사결정 필요 항목

아래는 구현 전에 제품/운영/보안이 함께 결정해야 함.

- payroll product에서 기본 user disclosure 정책을 `all-private`로 둘지, 회사별 설정으로 둘지 결정함.
- recipient-encrypted disclosure를 employee별로 요구할지, 회사/auditor 단위 recipient로 요구할지 결정함.
- disclosure public key registry의 owner를 wallet, payroll backend, company admin 중 어디에 둘지 결정함.
- note preparation을 자동으로 실행할지, operator approval 후 실행할지 결정함.
- preparation tx 수수료와 relayer 사용 여부를 결정함.
- failed item retry를 자동으로 할지, 금액/recipient 변경 가능성이 있으면 manual approval을 요구할지 결정함.
- manual review SLA와 운영자 권한 모델을 결정함.

이 항목들이 정리되어야 1차 구현을 고객-facing payroll 제품으로 안전하게 끌어올릴 수 있음.

## 1차 산출물 요약

1차 완료 시 repo에는 다음 성격의 산출물이 생김.

```text
reservation SDK
payroll planner SDK
proof/broadcast/reconcile runner
multi-message tx chunker
prover pool
bulk transfer benchmark harness
conformance fixture
benchmark report
```

1차는 최종 성능 개선의 전부가 아니라, 대량 지급을 안정적으로 실행하고 측정할 수 있게 만드는 기반임. 이 기반이 있어야 2차 N-output batch circuit도 실제 payroll workflow에 연결할 수 있음.

## 2차 계획

### Phase 7. N-output Batch Circuit

#### 목표

기존 transfer가 직원 1명당 proof 1개와 tx envelope 1개를 요구하는 구조를 개선함. 하나의 proof가 여러 recipient output을 생성하도록 새 batch circuit과 module message를 추가함.

#### 권장 N

2차의 권장값은 `N=32`임.

`N=32`를 추천하는 이유는 다음과 같음.

- 기존 시뮬레이션에서 10만건을 `3,125`개 batch로 줄일 수 있음.
- `K=20` multi-message transaction보다 tx envelope 수가 더 적음.
- `N=16`보다 throughput 개선 효과가 분명함.
- `N=64`보다 circuit size, proving key size, tx/event payload risk가 낮음.
- 10만건 단일 기업과 100개 기업 x 1천건 모델 모두에 적용 가능함.
- `100,000 / 32 = 3,125`로 단일 10만건 시나리오가 정확히 나누어짐.

따라서 shipped target은 `BatchJoinSplit32`로 둠. 다만 개발 중에는 circuit complexity와 artifact size를 확인하기 위해 내부 PoC로 `N=8` 또는 `N=16`을 먼저 만들 수 있음. 이 PoC는 최종 protocol target이 아니라 risk reduction milestone으로만 취급함.

#### v1 circuit shape

초기 v1은 다음 형태를 권장함.

```text
BatchJoinSplit32

inputs:
  shielded input note 2개

outputs:
  recipient output note 32개
  sender change output note 1개

public:
  root
  nullifiers 2개
  output commitments 33개
  encrypted note payload references
  disclosure digest 또는 disclosure metadata
```

입력 note 수는 우선 현재 join-split의 2 input 구조를 유지함. 이렇게 하면 input side 복잡도를 제한하면서 output batching 효과를 먼저 검증할 수 있음. 큰 payroll은 사전에 treasury shard를 충분한 금액의 note로 준비하고, 각 batch는 2개 input note로 32명 지급액과 change를 만들도록 설계함.

부분 batch는 v1에서 다음 정책을 사용함.

- 가능한 한 32명 full batch를 사용함.
- 마지막 remainder는 기존 Phase 1 multi-message transfer로 처리함.
- 이후 필요하면 `N=8`, `N=16` 보조 circuit 또는 safe padding 방식을 검토함.

이 정책은 zero-value dummy output을 대량으로 state에 append하는 위험을 피하기 위한 것임.

#### repo 작업

회로와 artifact를 추가함.

```text
x/privacy/circuit/batch_joinsplit32.go
x/privacy/circuit/batch_joinsplit32_test.go
x/privacy/zk/
```

proto message를 추가함.

```text
proto/clairveil/privacy/v1/tx.proto
```

예상 message는 다음 성격을 가짐.

```text
MsgBatchTransfer
  creator
  root
  nullifiers[2]
  commitments[33]
  ciphertexts[33]
  view_tags[33]
  proof
  disclosure fields
  recipient_count
  batch_id
  chunk_id
```

keeper handler를 추가함.

```text
x/privacy/keeper/msg_server.go
x/privacy/types/msg.go
x/privacy/keeper/msg_server_test.go
```

SDK와 prover transport를 확장함.

```text
x/privacy/client/sdk/batchtransfer/
x/privacy/client/sdk/provertransport/
x/privacy/client/sdk/proverservice/
cmd/clairveil-proverd/main.go
```

CLI 또는 payroll runner integration을 추가함.

```text
x/privacy/client/cli/
x/privacy/client/sdk/payroll/
```

benchmark를 확장함.

```text
cmd/clairveil-localnetload/main.go
cmd/clairveil-benchreport/main.go
benchmarks/privacy-bulk-transfer/
```

#### 구현 내용

- `BatchJoinSplit32` witness 구조를 정의함.
- 2 input note의 ownership, nullifier, amount balance를 검증함.
- 32 recipient output과 1 change output의 commitment를 생성함.
- input total과 output total이 일치함을 검증함.
- recipient별 encrypted note payload를 생성함.
- disclosure mode와 disclosure digest를 기존 transfer 정책과 맞춤.
- verifying key와 proving key artifact manifest를 추가함.
- keeper는 nullifier 중복을 검사하고 commitment 33개를 append함.
- keeper는 batch 안의 commitment/nullifier length와 `recipient_count`를 검증함.
- SDK는 `PayrollItem` 32개를 하나의 batch witness로 변환함.
- payroll runner는 Phase 1의 reservation과 operation 상태를 batch 단위로 연결함.
- scanner/reconcile은 batch tx에서 item별 output commitment와 expected value를 매칭함.

#### Phase 1 산출물 재사용

2차는 1차 산출물을 다음처럼 재사용함.

- Note Reservation: batch에 들어가는 input note를 `Reserved`로 잠금.
- Payroll Control Plane: `PayrollItem` 32개를 batch input으로 묶음.
- Proof Queue: 단건 transfer proof job 대신 batch proof job을 실행함.
- Broadcast Queue: `MsgBatchTransfer`를 제출하고 batch tx hash를 기록함.
- Reconcile Worker: batch event에서 item별 output commitment를 찾아 operation status를 갱신함.
- Benchmark Harness: 기존 10만건/100개 기업 시나리오를 batch circuit으로 다시 측정함.

#### 완료 기준

- `BatchJoinSplit32` circuit unit test가 통과함.
- artifact manifest와 checksum 검증이 통과함.
- keeper가 `MsgBatchTransfer`를 검증하고 commitment 33개를 append함.
- duplicate nullifier와 malformed batch payload를 거부함.
- SDK가 32개 payroll item을 batch witness로 변환함.
- localnet에서 batch payroll path가 end-to-end로 성공함.
- 10만건 단일 기업 benchmark에서 proof 수와 tx envelope 수가 약 `3,125개` 수준으로 감소함.
- 100개 기업 x 1천건 benchmark에서 회사별 full batch와 remainder 처리가 모두 동작함.

## 2차 진입 조건

2차는 1차가 끝나자마자 무조건 진행하는 작업이 아니라, 다음 조건 중 하나 이상이 확인되면 진행함.

- 1차 benchmark에서 단일 기업 10만건이 목표 SLA를 만족하지 못함.
- multi-message transaction과 prover scaling을 적용해도 proof 수 10만개가 운영 비용상 부담임.
- tx envelope `5,000개` 수준도 월말 피크에서 부담임.
- push payroll UX를 유지해야 하며, Merkle claim 방식으로 제품 방향을 바꾸기 어려움.
- 100개 기업 x 1천건 모델에서 tenant scheduling만으로 peak를 충분히 완화하지 못함.

## 주요 리스크

### 1차 리스크

- reservation store contract와 JS SDK 구현이 어긋날 수 있음.
- production DB transaction lock과 in-memory reference implementation의 의미가 다를 수 있음.
- multi-message transaction의 실제 gas/tx size 한도가 예상보다 낮을 수 있음.
- prover scaling이 선형에 가깝게 늘지 않을 수 있음.
- scanner/reconcile이 tx throughput을 따라가지 못할 수 있음.

### 2차 리스크

- `N=32` circuit proving time이 예상보다 클 수 있음.
- proving key와 artifact size가 운영 부담이 될 수 있음.
- tx/event payload가 너무 커질 수 있음.
- 33개 commitment append와 event scan이 keeper/storage 병목을 만들 수 있음.
- partial batch 처리 정책이 제품 UX와 맞지 않을 수 있음.
- disclosure digest와 item별 success 판정이 복잡해질 수 있음.

## 의사결정 기록

### 1차를 6번까지로 자르는 이유

1차의 목적은 현재 protocol을 유지한 채 어디까지 가능한지 측정 가능한 시스템을 만드는 것임. 7번까지 1차에 넣으면 protocol 변경을 이미 확정한 것처럼 보이고, benchmark 없이 circuit 개발에 들어가게 됨.

따라서 1차는 Note Reservation부터 Capacity Simulation Benchmark까지로 제한함. 이 범위만으로도 대량 지급 workflow, failure handling, retry/reconcile, prover scaling, tx chunking의 구조적 한계와 예상 병목을 확인할 수 있음.

### 2차를 N-output batch circuit으로 정한 이유

2차는 N-output batch circuit으로 진행함. Payroll Merkle Distribution은 확장성은 가장 크지만 claim 기반 UX로 제품 의미가 달라짐. 이번 계획은 회사가 직접 직원에게 지급하는 push payroll 모델을 개선하는 데 초점을 둠.

### `N=32`를 선택한 이유

`N=32`는 처리량 개선과 구현 리스크 사이의 균형점으로 봄. `N=16`은 안전하지만 tx/proof 감소 효과가 절반이고, `N=64`는 회로와 payload 리스크가 커짐. 기존 시뮬레이션도 `N=32`를 기준으로 의미 있는 개선을 보였으므로, 2차 target은 `BatchJoinSplit32`로 둠.

## 최종 권장 실행 순서

```text
1. reservation contract와 Go reference implementation을 먼저 만듦
2. payroll planner가 reservation을 사용해 plan을 확정하게 만듦
3. proof/broadcast/reconcile worker를 붙임
4. multi-message tx chunking으로 tx envelope 수를 줄임
5. prover worker pool로 proof 병목을 줄임
6. 10만건 및 100개 기업 x 1천건 benchmark를 실행함
7. 1차 benchmark 결과를 바탕으로 BatchJoinSplit32를 구현함
```

이 순서를 따르면 1차 산출물이 단기 운영에도 쓰이고, 2차 protocol 확장에도 그대로 재사용됨. 반대로 reservation과 payroll control plane 없이 바로 batch circuit으로 가면 proof 수는 줄일 수 있지만, 실패 처리, 재시도, 결과 리포트, note 충돌 대응이 흩어지므로 실제 payroll 제품으로 운영하기 어려움.
