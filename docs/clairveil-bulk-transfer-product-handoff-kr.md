# Clairveil 대량 전송 제품팀 전달 문서

## 목적

이 문서는 `private/bulk-transfer` 브랜치의 1차 repo 구현을 제품/운영 구현으로 이어가기 위한 전달 문서임.

repo에는 note reservation, payroll control plane, proof/broadcast/reconcile queue, multi-message tx, prover pool, benchmark/readiness harness, reference payroll CLI, simulated reference payroll daemon, repo-local demo product, file-backed reference artifact store, durable reservation state store의 reference implementation이 들어 있음. 제품/운영 영역에서는 managed production DB 배포 방식, tenant 운영 정책, live proof/broadcast/scanner mode, operator UI, 실제 10만건 rehearsal을 이어서 결정해야 함.

## Repo에서 제공하는 것

- 계획 문서: `plans/clairveil-bulk-transfer-implementation-plan-kr.md`
- Note reservation 설계: `docs/clairveil-note-reservation-design-kr.md`
- 대량 전송 전략/시뮬레이션: `docs/clairveil-bulk-transfer-strategy-kr.md`, `docs/clairveil-bulk-transfer-time-simulation-kr.md`
- Go reference packages: `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`
- Reference payroll CLI: `clairveil-payroll validate`, `prepare-notes`, `plan`, `run`, `status`, `reconcile`, `export-report`
- Reference payroll daemon: `clairveil-payrolld -mode simulated`
- Repo-local demo product: `make reference-payroll-demo`
- File-backed reference artifact store: `x/privacy/client/sdk/payroll.FileArtifactStore`
- Durable reservation state store: `x/privacy/client/sdk/reservation.DurableFileStore`
- 검증 entrypoint: `make privacy-bulk-readiness-check`
- localnet batch 검증: `make privacy-transfer-batch-localnet-bench`
- prover pool 측정: `PROVERD_URLS=url1,url2 make privacy-proverd-scale-bench`

## 제품팀이 이어서 결정/연결해야 하는 영역

### 1. Production DB 배포 방식

repo는 `reservation.Store` contract를 만족하는 `DurableFileStore` reference adapter를 제공함. 실제 고객 환경에서 PostgreSQL/MySQL/cloud DB를 사용하려면 같은 contract를 production DB로 옮기거나, reference adapter를 운영 정책에 맞게 감싸야 함.

필수 구현 내용은 다음과 같음.

- `note_inventory`, `note_reservations`, `payroll_operations`, `payroll_runs`, `payroll_items` 계열 schema 정의
- `owner_key_id + nullifier_lookup_key` active reservation partial unique constraint 적용
- planner note selection에서 `FOR UPDATE SKIP LOCKED` 또는 owner 단위 advisory lock 적용
- 상태 변경 compare-and-set 적용
- worker lease 필드 적용: `lease_owner`, `lease_token`, `lease_until`, `last_heartbeat_at`
- lease 획득, heartbeat, lease clear, worker-owned `ProofReady -> Submitted/Unknown`은 `Get -> Update` 조합이 아니라 단일 DB update/transaction으로 원자적 처리. `ProofReady -> ConfirmedSpent` recovery도 chain evidence 기반 compare-and-set/transaction 경로로 처리함.
- proof worker는 `Reserved` 상태 한정 lease 획득과 proof 생성 중 heartbeat를 사용
- broadcast worker는 `NullifierChecker`에 chain nullifier query provider를 연결해 tx 제출 직전 spent nullifier를 차단
- tx 제출 전 spent nullifier가 감지되면 SDK broadcast worker는 `SpentNullifierError`를 반환하고, scheduler/reconcile layer가 해당 item을 `ConflictSpent`, `ManualReview`, 또는 `ReplanRequired`로 전환함. 같은 `ProofReady` 작업을 그대로 무한 재시도하지 않아야 함.
- payroll/payment success evidence의 `expected_disclosure_digest`는 audit disclosure digest임. user disclosure 또는 sender self-view disclosure digest를 operation 성공 판정에 대신 쓰지 않음.
- `nullifier_lookup_key = HMAC(index_key, nullifier)` 형태의 deterministic keyed lookup 사용
- raw nullifier, commitment, recipient, amount 등 민감정보 암호화 저장
- payload/log/telemetry에 원문 민감정보가 남지 않도록 필터링

완료 기준은 사용하는 DB/adapter가 동시에 두 payroll planner가 같은 note를 reserve하지 못하게 하고, stale worker가 상태를 덮어쓰지 못하며, `Submitted`, `Unknown`, `ManualReview` note가 TTL만으로 available 처리되지 않게 하는 것임.

### 2. Payroll Scheduler / Worker Wiring

repo는 `clairveil-payroll run`으로 plan을 durable reservation/operation state에 확정하고, `clairveil-payroll reconcile`로 evidence 기반 상태 갱신을 수행할 수 있음. 또한 `clairveil-payrolld -mode simulated`로 같은 state 위에서 proof ready, submitted, reconciled 전이를 repo-local로 체험할 수 있음.

제품 환경에서는 이 state를 live proof worker, broadcast worker, chain scanner와 연결해야 함. 즉 simulated daemon은 운영 flow와 report를 검증하는 reference product이고, 실제 chain 제출 daemon은 같은 상태 계약을 live provider/prover/scanner로 대체하는 작업임.

필수 구현 내용은 다음과 같음.

- 월별 payroll upload/import flow
- tenant별 `PayrollRun` 생성과 run locking
- run 확정 시 note reservation 생성. reference CLI에서는 `clairveil-payroll run -plan ... -state ...`가 담당함.
- simulated 운영 체험. reference daemon에서는 `clairveil-payrolld -state ... -once`가 담당함.
- proof worker queue와 broadcast worker queue 운영
- proof worker 결과 저장소 구현: repo의 `ProofResultSink` 역할처럼 proof/message/payload를 durable queue 또는 DB에 먼저 저장하고, 저장 성공 후에만 `ProofReady`로 전환해야 함.
- operation-level idempotency: `operation_id`, `sign_doc_hash`, `tx_bytes_hash`, `tx_hash`, `account_sequence`
- replan attempt별 `operation_id`/`reservation_id` 분리
- 실패 원인 분류: insufficient note, reservation conflict, proof invalid, root invalid, gas/sequence, RPC timeout, payload mismatch
- RPC timeout/mempool eviction은 즉시 새 tx 생성으로 처리하지 않고 `Unknown`/`ReconcileUnknown` 흐름에서 `tx_hash`와 nullifier 상태를 먼저 확인해야 함.
- 실패 item만 `ReplanRequired`로 분리하고 재계획
- confirmation scanner/reconcile worker가 note 상태와 operation 상태를 각각 갱신. reference CLI에서는 `clairveil-payroll reconcile -state ... -evidence ...`가 evidence 반영을 담당함.

완료 기준은 1천건 rehearsal run을 중단/재시작해도 중복 지급 없이 재개되고, 실패 item만 재시도할 수 있는 것임.

### 3. JS SDK 및 Wallet 연동

JS SDK 또는 wallet storage가 note reservation 상태 계약을 따라야 함.

필수 확인 내용은 다음과 같음.

- Go conformance fixture와 JS SDK 상태 enum/transition 일치
- Active reservation 상태 정의 일치
- `nullifier_lookup_key_id` 또는 `lookup_key_version` 처리
- wallet note selection이 reserved note를 제외함
- 일반 transfer, split/merge, payroll job이 같은 note를 동시에 선택하지 못함
- UI에는 nullifier/commitment 원문 대신 축약 값만 표시

JS SDK가 이미 `docs/clairveil-note-reservation-design-kr.md`를 기준으로 작업 중이라면, Go reference implementation은 production DB 구현을 강제하는 것이 아니라 contract 검증 기준으로 사용하면 됨.

### 4. Prover 운영 및 수평확장

제품 운영 환경에서는 여러 `clairveil-proverd` endpoint를 pool로 운영해야 함.

필수 구현 내용은 다음과 같음.

- endpoint별 concurrency limit과 timeout 설정
- endpoint health check와 장애 endpoint 제외
- proof queue worker의 retry/failover 정책
- `PROVERD_URLS` 기반 scale benchmark 실행 및 unhealthy endpoint count 기록
- endpoint별 latency, error rate, timeout rate, RSS/CPU telemetry 수집
- peak payroll window 전 warm-up 및 capacity check

완료 기준은 prover 1개, 2개, 4개, 8개 환경에서 aggregate proof/sec가 측정되고, 장애 endpoint가 있어도 healthy endpoint만으로 queue가 멈추지 않으며, `unhealthy_endpoint_count`가 benchmark report에 남는 것임.

### 5. Rehearsal Runbook

실제 운영 전 rehearsal을 단계별로 실행해야 함.

권장 순서는 다음과 같음.

1. 1천건 단일 tenant dry run
2. 1천건 단일 tenant restart/retry run
3. 100개 tenant x 1천건 synthetic scheduling run
4. 1만건 단일 tenant run
5. 10만건 단일 tenant capacity rehearsal

각 run은 다음 결과를 남겨야 함.

- 총 소요 시간
- proof/sec, tx/sec, payroll item/sec
- reservation conflict count
- retry count
- replan count
- manual review count
- failed item count
- final reserve invariant
- operator가 확인해야 한 item 목록

## Repo 밖에서 결정해야 하는 것

- 월말 피크 SLA: 10만건을 몇 시간 안에 완료해야 하는지
- tenant별 동시 실행 제한과 우선순위
- 수수료 지불 주체와 relayer 운영 정책
- manual review 담당자와 승인 정책
- 민감정보 retention 기간
- payroll item 원문 recipient/amount를 저장할지, hash/HMAC/encrypted 형태로만 저장할지
- 2차 `BatchJoinSplit32` 진입 기준

## 전달 체크리스트

- 제품팀은 `make privacy-bulk-readiness-check` 결과를 확인함.
- 운영팀은 `make reference-payroll-demo`로 repo-local payroll product flow를 먼저 실행함.
- release 전에는 `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`로 multi-message transfer localnet 경로를 확인함.
- prover pool scale claim을 하려면 `RUN_PROVER_SCALE=1 PROVERD_URLS=url1,url2 make privacy-bulk-readiness-check` 결과를 별도 산출물로 남김. scale benchmark는 기본적으로 preflight 실패 endpoint를 제외하고 `unhealthy_endpoint_count`를 기록하지만, public claim 수치로 쓰려면 `unhealthy_endpoint_count=0`이어야 함.
- backend 팀은 `clairveil-payroll run -state ...`, `clairveil-payrolld -state ... -once`, `clairveil-payroll reconcile -state ...` durable control-plane workflow를 확인하고, managed DB가 필요하면 같은 `reservation.Store` contract로 이전 계획을 작성함.
- JS SDK 팀은 note reservation conformance fixture를 검증함.
- 운영팀은 prover pool endpoint 구성과 telemetry 수집 방식을 정함.
- 제품팀은 1천건 rehearsal runbook을 먼저 실행함.
- 1차 결과로 SLA가 부족하면 2차 `BatchJoinSplit32` 개발을 시작함.

## 비차단 후속 backlog

- `examples/clairveil-dapp/**` consumer drift 점검은 이 bulk-transfer review scope에서 제외되었으므로, dapp scope가 열릴 때 별도 점검함.
- full `make check`와 full `make release-check`는 dapp/localnet/external smoke 범위를 포함하므로 release candidate에서 별도 실행함.
- live localnet/prover-scale 수치를 public claim에 쓰려면 `RUN_LOCALNET=1` 및 `RUN_PROVER_SCALE=1` readiness run 결과를 release 산출물로 남김.
- allocator는 targeted regression과 fixed-seed property-style test로 현재 handoff risk를 닫음. 임의 생성 기반의 exhaustive fuzz suite는 release blocker가 아닌 장기 test-depth 강화 후보로 분리함.
