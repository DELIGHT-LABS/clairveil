# Clairveil 대량 전송 제품팀 전달 문서

## 목적

이 문서는 현재 checkout의 bulk-transfer/reference-payroll 구현을 제품/운영 구현으로 이어가기 위한 전달 문서임. Superseded phase-1 계획 문서가 아니라 current batch contract와 reference product surface를 기준으로 함.

repo에는 note reservation, payroll control plane, proof/broadcast/reconcile queue, legacy multi-message tx와 one-proof `MsgBatchTransfer`, bounded prover, benchmark/readiness harness, reference payroll CLI, simulated/live reference payroll daemon, repo-local demo product, live localnet payroll tutorial, rehearsal harness, file-backed reference artifact store, durable reservation state store, SQL reference store의 reference implementation이 들어 있음. 제품/운영 영역에서는 managed production DB 배포 방식, tenant 운영 정책, production-grade live proof/broadcast/scanner daemon, operator UI, 실제 10만건 rehearsal을 이어서 결정해야 함.

2026-07-13 기준 batch reference integration은 reference Go one-proof `BatchJoinSplit16x32` SDK/prover/scanner/payroll/CLI 경로를 구현했고 독립 공개 검증은 이를 `PUBLICATION_READY_EXPERIMENTAL`로 검증함. `PRODUCTION_RELEASE_READY`는 승인되지 않았으며 formal trusted setup, external audit, production artifact, production operations는 이 handoff의 완료 경계 밖임.

## Repo에서 제공하는 것

- Current batch roadmap: `plans/clairveil-batch-joinsplit-16x32-roadmap-kr.md`
- Normative batch contract/batch reference integration surface: `docs/clairveil-batch-joinsplit-16x32-kr.md`, `docs/clairveil-batch-transfer-integration-handoff-kr.md`
- Note reservation 설계: `docs/clairveil-note-reservation-design-kr.md`
- Accounting 설계: `docs/clairveil-privacy-accounting-design-note-kr.md`
- Go reference packages: `x/privacy/client/sdk/batchtransfer`, `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`
- Current one-proof CLI: `transfer-batch-16x32`, `prepare-batch-transfer`, `prove-batch-transfer`, `broadcast-batch-transfer`
- Legacy durable payroll CLI: `clairveil-payroll validate`, `build-input-from-notes`, `prepare-notes`, `plan`, `run`, `status`, `scan-evidence`, `reconcile`, `settle-transfer-batch`, `seed-localnet-notes`, `export-report`
- Reference payroll daemon: `clairveil-payrolld -mode simulated`, `clairveil-payrolld -mode live`
- Repo-local demo product: `make reference-payroll-demo`
- Legacy multi-message localnet tutorial: `make reference-payroll-live-localnet`
- Legacy phase 1 capacity rehearsal: `make reference-payroll-rehearsal`
- 제품 정책 기본값: `docs/clairveil-reference-payroll-product-policy-kr.md`
- File-backed reference artifact store: `x/privacy/client/sdk/payroll.FileArtifactStore`
- Durable reservation state store: `x/privacy/client/sdk/reservation.DurableFileStore`
- SQL reference reservation state store: `x/privacy/client/sdk/reservation.SQLStore`
- 검증 entrypoint: `make privacy-bulk-readiness-check`
- one-proof localnet batch 검증: `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`
- legacy multi-message envelope 측정: `make privacy-transfer-batch-localnet-bench`
- controlled legacy transfer/withdraw prover-pool 측정: `PROVERD_URLS=url1,url2 make privacy-proverd-scale-bench`

## 제품팀이 이어서 결정/연결해야 하는 영역

### 1. Production DB 배포 방식

repo는 `reservation.Store` contract를 만족하는 `DurableFileStore`와 `SQLStore` reference adapter를 제공함. `SQLStore`는 PostgreSQL/SQLite schema helper와 `database/sql` 기반 contract 구현을 제공하지만, managed production DB 배포 자체는 제품/운영 영역임. 실제 고객 환경에서 PostgreSQL/MySQL/cloud DB를 사용하려면 같은 contract와 schema 의미를 유지하면서 tenant partitioning, field-level encryption, migration, connection pool, 운영 lock 정책을 확정해야 함.

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
- payroll/payment success evidence의 `expected_disclosure_digest`는 audit disclosure digest 호환 필드임. 신규 구현은 `expected_audit_disclosure_digest`, `expected_user_disclosure_digest`, `expected_self_view_disclosure_digest`를 분리해 저장함.
- operation 성공 판정은 audit disclosure digest를 primary evidence로 사용하고, user/self-view disclosure digest는 expected field가 있을 때 별도로 확인함. user disclosure 또는 sender self-view disclosure digest를 audit digest 대신 쓰지 않음.
- `nullifier_lookup_key = HMAC(index_key, nullifier)` 형태의 deterministic keyed lookup 사용
- raw nullifier, commitment, recipient, amount 등 민감정보 암호화 저장
- payload/log/telemetry에 원문 민감정보가 남지 않도록 필터링

완료 기준은 사용하는 DB/adapter가 동시에 두 payroll planner가 같은 note를 reserve하지 못하게 하고, stale worker가 상태를 덮어쓰지 못하며, `Submitted`, `Unknown`, `ManualReview` note가 TTL만으로 available 처리되지 않게 하는 것임.

### 2. Payroll Scheduler / Worker Wiring

현재 batch reference integration 경로는 many-input/one-operation/many-item durable graph와 `BatchProofWorker`, `IdempotentBatchBroadcastWorker`, `BatchReconcileWorker`를 제공함. `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`은 이 graph를 실제 `MsgBatchTransfer` 하나, process/node restart, 저장한 동일 bytes retry, tx-hash-first reconcile, typed item evidence, disclosure 검증, batch/item status 분리에 연결함.

기존 `clairveil-payroll run` / `scan-evidence` / `reconcile`, `clairveil-payrolld`, `settle-transfer-batch` surface는 legacy multi-message durable-control-plane tutorial/regression 경로로 남음. 이를 one-proof BatchJoinSplit16x32 경로로 표현하면 안 됨. 제품 환경에서는 현재 batch graph를 long-running proof worker, broadcast worker, typed chain scanner와 연결하면서 atomic operation과 item별 evidence contract를 보존해야 함.

필수 구현 내용은 다음과 같음.

- 월별 payroll upload/import flow
- tenant별 `PayrollRun` 생성과 run locking
- input 1..16개를 atomic reserve하고 payment/change/padding output 1..32개를 batch operation 하나에 bind하는 one-proof plan
- private prepared payload, proof, signed tx bytes, tx hash, input reservation, output별 item evidence의 durable 저장
- control-plane regression이 필요할 때 legacy simulated 운영 체험. legacy reference daemon에서는 `clairveil-payrolld -state ... -once`가 담당함.
- proof worker queue와 broadcast worker queue 운영
- batch를 broadcast-ready로 표시하기 전에 proof/message/payload를 저장하는 proof worker 결과 store
- operation-level idempotency: `operation_id`, `sign_doc_hash`, `tx_bytes_hash`, `tx_hash`, `account_sequence`
- replan attempt별 `operation_id`/`reservation_id` 분리
- 실패 원인 분류: insufficient note, reservation conflict, proof invalid, root invalid, gas/sequence, RPC timeout, payload mismatch
- RPC timeout/mempool eviction은 즉시 새 tx 생성으로 처리하지 않고 `Unknown`/`ReconcileUnknown` 흐름에서 `tx_hash`와 nullifier 상태를 먼저 확인해야 함.
- atomic batch output list 일부만 재구성하거나 retry하지 않음. Reconcile 동안 원래 operation/reservation을 유지하고 기존 batch outcome을 해결한 뒤에만 새 operation을 생성함.
- batch chain status와 item별 evidence status를 분리 갱신함. Item은 expected output index, commitment, recipient, amount/asset, disclosure evidence가 일치할 때만 성공함.
- product 기본 정책은 `docs/clairveil-reference-payroll-product-policy-kr.md`의 기본값을 따른 뒤 tenant별 override로 확장함.

완료 기준은 현재 one-proof 경로가 중단/재시작 뒤 중복 지급 없이 재개되고, retry가 허용될 때 저장한 동일 signed bytes를 사용하며, 재서명 전에 tx hash/nullifier를 reconcile하고, atomic output list를 부분 retry하지 않는 것임. Production-scale evidence에는 별도 1천건 staging rehearsal이 여전히 필요함.

### 3. JS SDK 및 Wallet 연동

JS SDK 또는 wallet storage가 note reservation 상태 계약을 따라야 함.

필수 확인 내용은 다음과 같음.

- Go conformance fixture와 JS SDK 상태 enum/transition 일치
- JS/TS 구현이 `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json`의 1/1, 3-input/4-output, 31-payments-plus-change, exact-32-payment, explicit-padding shape를 통과함.
- Active reservation 상태 정의 일치
- `nullifier_lookup_key_id` 또는 `lookup_key_version` 처리
- wallet note selection이 reserved note를 제외함
- 일반 transfer, split/merge, payroll job이 같은 note를 동시에 선택하지 못함
- UI에는 nullifier/commitment 원문 대신 축약 값만 표시

JS SDK가 이미 `docs/clairveil-note-reservation-design-kr.md`를 기준으로 작업 중이라면, Go reference implementation은 production DB 구현을 강제하는 것이 아니라 contract 검증 기준으로 사용하면 됨. 신규 batch 작업은 `docs/clairveil-batch-transfer-integration-handoff-kr.md`도 따라야 하며 proto만으로 downstream client contract를 추론하면 안 됨.

### 4. Prover 운영 및 수평확장

제품 운영 환경은 capacity를 위해 여러 `clairveil-proverd` endpoint를 운영할 수 있지만 prepared proof payload에는 private note witness가 들어 있음. 서로 다른 미할당 job을 endpoint에 분산하는 것은 failover가 아님. Job을 한 번 할당하고 endpoint identity를 저장한 뒤 해당 witness를 선택한 endpoint에 pin해야 함. batch reference integration `BatchPayrollProver` API는 local prover 하나 또는 명시적으로 선택한 remote endpoint 하나만 나타내며 pool/failover 동작을 포함하지 않음.

같은 witness-bearing payload를 두 번째 endpoint로 보내면 privacy boundary가 확장되므로 기본적으로 금지함. legacy `ProverPool`은 `MultiProverFailoverOptIn`이 없으면 request마다 endpoint 하나만 선택하고, opt-in 검증은 허용할 전체 `EndpointIDs`와 `PrivacyWarningAcknowledged=true`를 요구함. 같은 witness의 새 호출을 독립 job으로 취급하거나 조용히 round-robin하면 안 됨. HTTP `retryable=true`, endpoint timeout, queue saturation은 cross-endpoint failover 권한이 아님.

필수 구현 내용은 다음과 같음.

- endpoint별 concurrency limit과 timeout 설정
- endpoint health check와 장애 endpoint 제외
- 최초 witness 공개 전에 job-to-endpoint assignment를 저장하고 same-endpoint retry와 unassigned-job scheduling 정책을 분리함.
- product/user policy가 전체 허용 endpoint set과 privacy warning 명시적 수락을 기록하지 않으면 cross-endpoint same-witness failover를 disabled로 유지함.
- controlled transfer/withdraw fixture로 `PROVERD_URLS` 기반 scale benchmark를 실행하고 unhealthy endpoint count를 기록함. 이는 legacy route 분산을 측정하며 batch reference integration 16x32 proof capacity가 아님. Benchmark round-robin은 production failover 권한도 아님.
- endpoint별 latency, error rate, timeout rate, RSS/CPU telemetry 수집
- peak payroll window 전 warm-up 및 capacity check

완료 기준은 장애 endpoint가 있어도 독립 new/unassigned job은 healthy endpoint에서 계속되고, 이미 공개한 witness는 검증된 explicit opt-in 없이 다른 곳으로 전송되지 않으며, default/opt-in endpoint contact count를 audit할 수 있고, controlled benchmark가 `unhealthy_endpoint_count`를 기록하는 것임. batch reference integration capacity claim에는 주장하는 endpoint count별 실제 16x32 proof/sec/resource 측정이 추가로 필요하며 기존 transfer/withdraw scale benchmark만으로는 부족함.

### 5. Rehearsal Runbook

실제 운영 전 rehearsal을 단계별로 실행해야 함.

현재 권장 순서는 다음과 같음.

1. `make privacy-batch-joinsplit-localnet` batch reference integration conformance/static gate
2. `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` actual one-proof restart/retry/disclosure localnet gate
3. 1천건 단일 tenant one-proof staging dry run과 restart/retry run
4. 100개 tenant x 1천건 synthetic scheduling run
5. 1만건 단일 tenant run
6. 10만건 단일 tenant capacity rehearsal

각 run은 다음 결과를 남겨야 함.

- 총 소요 시간
- proof/sec, tx/sec, payroll item/sec
- batch input/output shape 분포와 payment/change/padding count
- reservation conflict count
- retry count
- replan count
- manual review count
- failed item count
- final reserve invariant
- operator가 확인해야 한 item 목록
- prover endpoint assignment, privacy opt-in 사용 여부, endpoint별 contact count

## Repo 밖에서 결정해야 하는 것

- 월말 피크 SLA: 10만건을 몇 시간 안에 완료해야 하는지
- tenant별 동시 실행 제한과 우선순위
- 수수료 지불 주체와 relayer 운영 정책
- manual review 담당자와 승인 정책
- 민감정보 retention 기간
- payroll item 원문 recipient/amount를 저장할지, hash/HMAC/encrypted 형태로만 저장할지
- frozen `BatchJoinSplit16x32` contract를 넘는 future circuit shape의 근거와 change-control 기준

## 전달 체크리스트

- 제품팀은 `make privacy-bulk-readiness-check` 결과를 확인함.
- 운영팀은 `make privacy-batch-joinsplit-localnet`을 실행하고, local resource/development artifact가 있으면 `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`으로 현재 one-proof 경로를 검증함.
- 운영팀은 `make reference-payroll-demo`와 `make reference-payroll-live-localnet`을 legacy control-plane/multi-message regression 경로로 실행할 수 있지만 현재 one-proof gate로 취급하지 않음.
- 2026-07-08 명령 `PAYROLL_SEED_NOTES=1 PAYROLL_ITEM_COUNT=1000 PAYROLL_CHUNK_SIZE=20 GAS_PRICES=0uclair make reference-payroll-live-localnet`과 성공 결과는 날짜가 있는 legacy restart/retry evidence로만 보존함. 결과는 `docs/clairveil-reference-payroll-localnet-rehearsal-result-kr.md`를 확인함.
- release 전에는 `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check`로 독립 legacy multi-message 경로도 확인하되 현재 one-proof gate를 대체하지 않음.
- legacy transfer/withdraw prover-pool scale claim에는 `RUN_PROVER_SCALE=1 PROVERD_URLS=url1,url2 make privacy-bulk-readiness-check` 결과를 controlled-fixture artifact로 남김. 해당 public claim에는 `unhealthy_endpoint_count=0`이 필요함. 이를 16x32 capacity evidence 또는 production cross-endpoint failover 권한으로 바꾸어 표현하면 안 됨.
- Backend 팀은 현재 one-proof batch graph/worker와 legacy durable-control-plane workflow를 모두 확인하고, managed DB가 필요하면 해당 batch operation 및 `reservation.Store` contract를 보존하는 migration plan을 작성함.
- JS SDK 팀은 note-reservation conformance와 `privacy_batch_transfer_v1_contract.json`을 모두 검증함.
- 운영팀은 endpoint assignment persistence, same-endpoint retry, explicit same-witness failover opt-in, telemetry 수집 방식을 정함.
- 제품팀은 capacity claim 전에 현재 one-proof 1천건 staging rehearsal을 실행함.
- staging/production note preparation은 `PAYROLL_SEED_NOTES=1`을 사용하지 않고 실제 deposit, split/merge, approval 기반 preparation flow로 검증함.
- 현재 16/32 evidence가 SLA를 충족하지 못하면 별도 protocol-shape decision을 열어 roadmap, threat review, circuit/keeper/SDK contract, migration plan, fresh validation을 수행함. 구현된 batch reference integration 작업을 미래 단계로 취급하면 안 됨.

## 비차단 후속 backlog

- `examples/clairveil-dapp/**` consumer drift 점검은 이 bulk-transfer review scope에서 제외되었으므로, dapp scope가 열릴 때 별도 점검함.
- full `make check`와 full `make release-check`는 dapp/localnet/external smoke 범위를 포함하므로 release candidate에서 별도 실행함.
- live one-proof/prover-scale 수치를 public claim에 쓰려면 `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`, actual 16x32 scale 측정, 별도 scope의 controlled `RUN_PROVER_SCALE=1` transfer/withdraw readiness 결과를 release 산출물로 남김.
- allocator는 targeted regression과 fixed-seed property-style test로 현재 handoff risk를 닫음. 임의 생성 기반의 exhaustive fuzz suite는 release blocker가 아닌 장기 test-depth 강화 후보로 분리함.
