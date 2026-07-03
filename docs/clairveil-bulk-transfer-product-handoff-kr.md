# Clairveil 대량 전송 제품팀 전달 문서

## 목적

이 문서는 `private/bulk-transfer` 브랜치의 1차 repo 구현을 제품/운영 구현으로 이어가기 위한 전달 문서임.

repo에는 note reservation, payroll control plane, proof/broadcast/reconcile queue, multi-message tx, prover pool, benchmark/readiness harness의 reference implementation이 들어 있음. 그러나 production DB, scheduler service, tenant 운영 정책, operator UI, 실제 10만건 rehearsal은 제품/운영 영역에서 이어서 구현해야 함.

## Repo에서 제공하는 것

- 계획 문서: `plans/clairveil-bulk-transfer-implementation-plan-kr.md`
- Note reservation 설계: `docs/clairveil-note-reservation-design-kr.md`
- 대량 전송 전략/시뮬레이션: `docs/clairveil-bulk-transfer-strategy-kr.md`, `docs/clairveil-bulk-transfer-time-simulation-kr.md`
- Go reference packages: `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`
- 검증 entrypoint: `make privacy-bulk-readiness-check`
- localnet batch 검증: `make privacy-transfer-batch-localnet-bench`
- prover pool 측정: `PROVERD_URLS=url1,url2 make privacy-proverd-scale-bench`

## 제품팀이 구현해야 하는 영역

### 1. Production DB Adapter

`reservation.Store` contract를 production DB로 구현해야 함.

필수 구현 내용은 다음과 같음.

- `note_inventory`, `note_reservations`, `payroll_operations`, `payroll_runs`, `payroll_items` 계열 schema 정의
- `owner_key_id + nullifier_lookup_key` active reservation partial unique constraint 적용
- planner note selection에서 `FOR UPDATE SKIP LOCKED` 또는 owner 단위 advisory lock 적용
- 상태 변경 compare-and-set 적용
- worker lease 필드 적용: `lease_owner`, `lease_token`, `lease_until`, `last_heartbeat_at`
- `nullifier_lookup_key = HMAC(index_key, nullifier)` 형태의 deterministic keyed lookup 사용
- raw nullifier, commitment, recipient, amount 등 민감정보 암호화 저장
- payload/log/telemetry에 원문 민감정보가 남지 않도록 필터링

완료 기준은 동시에 두 payroll planner가 같은 note를 reserve하지 못하고, stale worker가 상태를 덮어쓰지 못하며, `Submitted`, `Unknown`, `ManualReview` note가 TTL만으로 available 처리되지 않는 것임.

### 2. Payroll Scheduler Service

Go SDK의 `payroll` model을 실제 job/run/item/operation service로 연결해야 함.

필수 구현 내용은 다음과 같음.

- 월별 payroll upload/import flow
- tenant별 `PayrollRun` 생성과 run locking
- run 확정 시 note reservation 생성
- proof worker queue와 broadcast worker queue 운영
- operation-level idempotency: `operation_id`, `sign_doc_hash`, `tx_bytes_hash`, `tx_hash`, `account_sequence`
- 실패 원인 분류: insufficient note, reservation conflict, proof invalid, root invalid, gas/sequence, RPC timeout, payload mismatch
- 실패 item만 `ReplanRequired`로 분리하고 재계획
- confirmation scanner/reconcile worker가 note 상태와 operation 상태를 각각 갱신

완료 기준은 1천건 run을 중단/재시작해도 중복 지급 없이 재개되고, 실패 item만 재시도할 수 있는 것임.

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
- `PROVERD_URLS` 기반 scale benchmark 실행
- endpoint별 latency, error rate, timeout rate, RSS/CPU telemetry 수집
- peak payroll window 전 warm-up 및 capacity check

완료 기준은 prover 1개, 2개, 4개, 8개 환경에서 aggregate proof/sec가 측정되고, 장애 endpoint가 있어도 queue가 멈추지 않는 것임.

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
- backend 팀은 production DB adapter 설계를 작성함.
- JS SDK 팀은 note reservation conformance fixture를 검증함.
- 운영팀은 prover pool endpoint 구성과 telemetry 수집 방식을 정함.
- 제품팀은 1천건 rehearsal runbook을 먼저 실행함.
- 1차 결과로 SLA가 부족하면 2차 `BatchJoinSplit32` 개발을 시작함.
