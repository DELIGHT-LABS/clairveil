# Clairveil 대량 전송 실행 계획

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | 1차 repo 구현 및 후속 검증 harness 완료, 제품/운영 구현 분리 필요 |
| 작성일 | 2026-07-03 |
| 대상 브랜치 | `private/bulk-transfer` |
| 대상 영역 | `x/privacy` client SDK, provider, benchmark, 이후 privacy protocol |
| 1차 범위 | Note Reservation, Payroll Control Plane, Proof/Broadcast/Reconcile Queue, Multi-Message Tx, Prover Scaling, Capacity Simulation Benchmark |
| 2차 범위 | N-output batch circuit |
| 2차 권장 N | `N=32` |
| 제외 범위 | Payroll Merkle Distribution, protocol-level reservation, production DB implementation, production scheduler service |

## 관련 문서

- [Clairveil 대량 전송 방안 검토 리포트](../docs/clairveil-bulk-transfer-strategy-kr.md)
- [Clairveil 대량 전송 소요시간 시뮬레이션 노트](../docs/clairveil-bulk-transfer-time-simulation-kr.md)
- [Clairveil 대량 전송에서 1/2번 방안과 Prover 수평확장만 적용하는 대안 검토](../docs/clairveil-bulk-transfer-prover-scale-option-kr.md)
- [Clairveil Note Reservation 설계 노트](../docs/clairveil-note-reservation-design-kr.md)

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

| 단계 | 상태 | 구현 위치 |
| --- | --- | --- |
| Phase 1. Note Reservation | 구현 완료 | `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json` |
| Phase 2. Payroll Control Plane | 구현 완료 | `x/privacy/client/sdk/payroll` |
| Phase 3. Proof/Broadcast/Reconcile Queue | 구현 완료 | `x/privacy/client/sdk/payroll/proof_queue.go`, `broadcast_queue.go`, `reconcile_worker.go`, `retry_policy.go` |
| Phase 4. Multi-Message Transaction | 구현 및 localnet 검증 harness 완료 | `x/privacy/client/sdk/payroll/chunker.go`, `batch_broadcaster.go`, `sdk_broadcaster.go`, `x/privacy/client/sdk/provider/tx.go`, `x/privacy/client/cli/tx_transfer_batch.go`, `scripts/privacy-transfer-batch-localnet-bench.sh` |
| Phase 5. Prover Scaling | 구현 및 pool load harness 완료 | `x/privacy/client/sdk/payroll/prover_pool.go`, `cmd/clairveil-proverload`, `scripts/privacy-proverd-scale-bench.sh` |
| Phase 6. Capacity Simulation Benchmark | 시뮬레이션 및 readiness harness 완료 | `cmd/clairveil-bulktransferbench`, `scripts/privacy-bulk-transfer-bench.sh`, `scripts/privacy-bulk-readiness-check.sh`, `make privacy-bulk-readiness-check` |
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
- proof 생성이 끝나면 reservation을 `ProofReady`로 전환함.
- broadcaster는 `ProofReady` 상태만 제출함.
- broadcast 후 `tx_hash`, `tx_bytes_hash`, `sign_doc_hash`, `account_sequence`를 저장함.
- tx result가 불명확하면 `Unknown`으로 보내고 reconcile worker가 처리함.
- nullifier spent만으로 payroll success를 인정하지 않음.
- expected output commitment, disclosure digest, recipient hash, amount, denom이 operation과 일치해야 success로 처리함.
- RPC timeout, mempool eviction, gas/sequence 문제, proof invalid, nullifier spent를 서로 다르게 분류함.

#### 완료 기준

- proof worker가 같은 reservation을 중복 처리하지 않음.
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

repo는 protocol과 SDK 기준 reference implementation, local harness, synthetic benchmark를 제공함. 실제 제품화에는 다음 작업이 추가로 필요함.

- production DB schema와 adapter 구현
- PostgreSQL transaction lock, partial unique index, HMAC lookup key, field-level encryption 적용
- payroll scheduler service와 tenant별 run orchestration 구현
- operator UI, alert, manual review flow 구현
- 실제 1천건, 1만건, 10만건 rehearsal runbook 작성 및 실행
- JS SDK/wallet storage와 note reservation 상태 계약의 conformance 확인

이 항목들은 repo 내부 code만으로 완료되는 작업이 아니며, 제품팀 전달 문서에서 별도 owner와 산출물을 정의해야 함.

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
