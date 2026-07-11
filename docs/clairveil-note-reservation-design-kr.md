# Clairveil Note Reservation 설계 노트

English version: [clairveil-note-reservation-design.md](clairveil-note-reservation-design.md)

## 목적

이 문서는 Clairveil에서 payroll, batch transfer, 대량 전송을 구현할 때 필요한 note reservation 설계를 정리함.

대상 독자는 클라이언트, SDK, wallet, payroll control plane 개발자임. 목표는 대량 전송을 준비하는 동안 선택한 input note가 split, merge, 일반 transfer, 다른 payroll job에 의해 먼저 소비되는 문제를 막고, 실패 시 안전하게 재계획할 수 있게 하는 것임.

## 배경

Clairveil의 shielded transfer는 input note를 소비하고, nullifier를 공개하며, 새 commitment를 만듦. 한 note는 한 번만 소비될 수 있음. 따라서 대량 전송에서는 proof를 만들기 전에 어떤 note를 쓸지 계획하고, 그 note가 실제로 transaction에 포함될 때까지 다른 작업에서 사용되지 않도록 관리해야 함.

예를 들어 payroll job이 1,000건의 지급을 준비하면서 treasury note 100개를 input 후보로 배정했다고 하자. 이 상태에서 다른 wallet transfer가 같은 note를 먼저 써버리거나, background merge 작업이 그 note를 소비하면 payroll proof는 더 이상 유효하지 않음. broadcast 시점에는 nullifier가 이미 사용되었거나 Merkle root가 맞지 않아 실패할 수 있음.

따라서 production `MsgBatchTransfer`를 사용하는 Session 3B UX에는 note reservation이 필요함.

## 핵심 결론

note reservation은 1차적으로 off-chain control plane에서 구현하는 것이 좋음.

chain은 note가 실제로 spend될 때 nullifier 중복 여부를 검증할 수 있지만, "이 note는 payroll job에 예약되어 있으므로 다른 곳에서 쓰면 안 된다"는 의도를 기본적으로 알지 못함. 그래서 처음부터 protocol-level reservation을 만들기보다는, wallet 또는 payroll control plane이 note inventory를 관리하고 single writer lock을 제공해야 함.

추천 순서는 다음과 같음.

```text
1. client/control plane note reservation 구현
2. payroll 전용 treasury shard 운영
3. split/merge와 payroll 실행 window 분리
4. MsgBatchTransfer client/product flow와 reservation 연동
5. 필요할 때만 protocol-level reservation 검토
```

## 용어

| 용어 | 의미 |
| --- | --- |
| note | shielded asset을 나타내는 spendable record |
| nullifier | note를 소비할 때 공개되는 중복 소비 방지 값 |
| note inventory | wallet 또는 payroll system이 알고 있는 spendable note 목록 |
| reservation | 특정 note를 특정 job이 사용하도록 임시로 예약하는 것 |
| lock | 예약된 note를 다른 작업이 선택하지 못하도록 막는 상태 |
| shard note | 병렬 지급을 위해 treasury를 미리 여러 note로 쪼갠 것 |
| plan | payroll 또는 batch 실행 전에 recipient, amount, input note 후보를 계산한 결과 |
| replan | 기존 plan이 깨졌을 때 새 input note로 다시 계획하는 과정 |

## 해결해야 하는 문제

대량 전송에서는 다음 상황을 반드시 처리해야 함.

- payroll plan에서 선택한 note가 일반 transfer에 먼저 사용됨
- payroll plan에서 선택한 note가 split 또는 merge transaction에 사용됨
- 같은 payroll 안의 두 item이 같은 input note를 선택함
- 서로 다른 payroll job이 같은 treasury note를 선택함
- proof 생성 후 broadcast 전에 nullifier가 이미 사용됨
- transaction이 mempool에서 timeout되거나 sequence 문제로 실패함
- batch 일부가 실패해서 기존 chunk를 다시 구성해야 함
- chain reorg 또는 scanner 지연 때문에 note 상태가 늦게 반영됨

이 문제를 처리하지 않으면 batch 실행기는 정상적으로 proof를 만들었더라도 broadcast 단계에서 대량 실패를 경험할 수 있음.

## 기본 원칙

### 1. plan 확정 시점에 Reserved로 전환함

note는 payroll plan 단계에서 input note로 선택되는 즉시 `Reserved`로 전환함. proof 생성 시점이 아니라 plan 확정 시점에 잠가야 일반 transfer, split/merge, 다른 payroll job이 같은 note를 선택하지 못함.

권장 흐름:

```text
Available note 조회
-> payroll item/chunk에 input note 배정
-> DB transaction 안에서 reservation 생성
-> note_inventory.reservation_id 갱신
-> Reserved 상태로 plan 확정
```

plan이 아직 draft 상태라면 note를 잠그지 않을 수 있음. 하지만 사용자가 plan을 확정하거나, scheduler가 payroll 실행 대상으로 등록하는 순간에는 반드시 reservation을 생성해야 함.

### 2. 예약되지 않은 note만 선택함

payroll planner와 일반 transfer planner는 `Available` 상태의 note만 input 후보로 선택해야 함. 이미 `Reserved`, `Proving`, `Submitted` 상태인 note는 다른 작업에서 사용할 수 없음.

### 3. payroll treasury는 일반 wallet 사용과 분리함

기업 payroll에 사용할 treasury key 또는 account는 일반 사용자가 임의로 transfer하지 못하게 운영하는 것이 좋음. 같은 key를 여러 UI, script, background worker가 동시에 사용하면 off-chain reservation이 깨질 수 있음.

### 4. split/merge는 payroll 실행 window 밖에서 수행함

payroll을 실행하는 동안 background split/merge가 같은 treasury note를 소비하면 plan이 깨짐. split은 payroll plan 전에 수행하고, merge는 payroll 완료 후 수행함.

### 5. proof 직전과 broadcast 직전에 다시 확인함

plan 시점에는 note가 사용 가능했더라도, proof 생성 또는 broadcast 시점에는 상태가 바뀌었을 수 있음. 따라서 최소한 두 번 확인해야 함.

```text
plan 단계: Available note 선택
proof 직전: local reservation 상태 확인
broadcast 직전: chain nullifier 상태 확인
```

### 6. 실패 시 전체 job이 아니라 item 또는 chunk 단위로 재계획함

대량 전송에서 한 note가 invalid해졌다고 전체 payroll을 다시 만들면 운영 비용이 너무 큼. 실패한 item 또는 chunk만 `ReplanRequired`로 바꾸고 새 note를 배정해야 함.

## Note 상태 머신

권장 상태는 다음과 같음.

```text
Discovered
-> Available
-> Reserved
-> Proving
-> ProofReady
-> Submitted
-> ConfirmedSpent

Reserved
-> Released
-> Available

ManualReview
-> Released
-> Available

ProofReady
-> ConfirmedSpent

Submitted
-> Failed
-> ReplanRequired
-> Reserved(new note)

Submitted
-> Unknown

Unknown
-> ManualReview
```

상태 설명:

| 상태 | 의미 | 선택 가능 여부 |
| --- | --- | --- |
| `Discovered` | scanner가 발견했지만 spend 가능 여부 검증이 끝나지 않은 note | 불가 |
| `Available` | 사용할 수 있는 spendable note | 가능 |
| `Reserved` | 특정 job/item/chunk에 예약된 note | 불가 |
| `Proving` | 해당 note로 proof 생성 중 | 불가 |
| `ProofReady` | proof가 생성되었지만 아직 broadcast 전 | 불가 |
| `Submitted` | transaction이 broadcast되었고 결과 대기 중 | 불가 |
| `ConfirmedSpent` | chain에서 nullifier가 사용된 것이 확인됨 | 불가 |
| `Failed` | tx 또는 proof가 실패함 | 불가 |
| `ReplanRequired` | 기존 note로는 진행할 수 없어 새 plan 필요 | 불가 |
| `Released` | 예약이 해제되어 다시 available로 돌아갈 수 있음 | 전이 상태 |
| `Unknown` | broadcast 후 결과가 불명확함 | 불가 |
| `ManualReview` | 자동 reconcile이 위험하거나 불충분해 운영자 확인이 필요한 상태 | 불가 |

중요한 점은 `ProofReady` 상태도 lock 상태라는 것임. proof를 이미 만들었다고 해서 note를 다른 곳에 풀면 안 됨. 해당 proof는 특정 input note와 root에 묶여 있기 때문임.

또한 `Released` 전이는 상태별로 의미가 다름. 자동 release는 `Reserved` 상태에만 제한적으로 허용함. `Proving` 또는 `ProofReady`에서 `Released`로 직접 전환하는 것은 repo contract에서 거부함. proof artifact 폐기, tx 미제출, lease 상태 확인이 필요하면 먼저 `ReplanRequired` 또는 `ManualReview`로 보내고, 운영자가 안전하다고 확인한 경우에만 `ManualReview -> Released -> Available` 경로를 사용함. `ProofReady -> ConfirmedSpent`는 이미 chain spent 증거가 확인된 recovery/reconcile 경로임.

`Reconcile`은 저장 상태가 아니라 worker 또는 process 이름으로 봄. 즉, `Unknown` 상태의 reservation을 reconcile worker가 조회하고, 확인 결과에 따라 `ConfirmedSpent`, `ReplanRequired`, `ManualReview` 등으로 전환함.

active reservation 상태는 다음으로 봄.

- `Reserved`
- `Proving`
- `ProofReady`
- `Submitted`
- `Unknown`
- `ManualReview`

active 상태의 reservation은 같은 `owner_key_id + nullifier_lookup_key` 조합으로 중복 생성되면 안 됨.

## Note 상태와 operation 성공 판정 분리

`ConfirmedSpent`는 note-level 상태임. 이것은 chain에서 해당 nullifier가 사용되었음을 뜻함. 하지만 이것만으로 payroll item 또는 payment operation이 성공했다고 판단하면 안 됨.

예를 들어 다른 충돌 transaction이 같은 note를 먼저 소비했다면 nullifier는 spent가 됨. 이 경우 note 관점에서는 `ConfirmedSpent`가 맞지만, payroll 지급은 성공이 아님.

따라서 operation/payment 성공은 다음 증거가 현재 operation과 일치할 때만 인정함.

- tx hash 또는 tx result가 현재 `operation_id`와 연결되어 있음
- event의 output commitment가 expected output commitment와 일치함
- audit disclosure digest 또는 audit disclosure payload가 expected recipient/amount와 일치함
- recipient shielded key, amount, denom, batch item index가 plan과 일치함
- `sign_doc_hash`, `tx_bytes_hash`, `tx_hash`가 저장된 operation record와 일치함

증거가 일치하면 payment operation을 success로 처리할 수 있음. 증거가 부족하거나 불일치하면 note는 `ConfirmedSpent`로 갱신하되, operation은 `ManualReview` 또는 operation-level `ConflictSpent`로 보냄.

`ConflictSpent`는 note reservation 상태라기보다 payment operation 상태로 보는 것이 좋음. 이 상태는 "input note는 소비되었지만, 내가 의도한 지급으로 확인되지 않음"을 의미함.

## Reservation 데이터 모델

클라이언트 또는 payroll control plane은 최소한 다음 정보를 관리해야 함.

```text
NoteInventory {
  note_id
  commitment
  encrypted_nullifier
  nullifier_lookup_key
  nullifier_lookup_key_id
  asset_id
  encrypted_amount
  owner_key_id
  merkle_position
  discovered_height
  spend_status
  reservation_id
  updated_at
}

NoteReservation {
  reservation_id
  company_id
  payroll_id
  batch_id
  chunk_id
  item_id
  note_id
  owner_key_id
  encrypted_nullifier
  nullifier_lookup_key
  nullifier_lookup_key_id
  status
  expires_at
  lease_owner
  lease_token
  lease_until
  last_heartbeat_at
  operation_id
  sign_doc_hash
  tx_bytes_hash
  tx_hash
  account_sequence
  broadcast_attempt_count
  last_broadcast_at
  last_broadcast_error
  created_at
  updated_at
}

PayrollOperation {
  operation_id
  company_id
  payroll_id
  batch_id
  chunk_id
  item_id
  expected_output_commitment
  expected_disclosure_digest
  expected_recipient_hash
  encrypted_expected_recipient
  encrypted_expected_amount
  expected_amount_hash
  expected_denom
  batch_item_index
  batch_item_index_known
  sign_doc_hash
  tx_bytes_hash
  tx_hash
  status
  created_at
  updated_at
}
```

구현상 `note_id`는 local DB에서 쓰는 식별자임. `commitment`와 `nullifier`는 중복 방지와 chain reconciliation에 필요함. 다만 raw nullifier를 그대로 index에 쓰지 않는 편이 좋음.

권장 방식:

```text
nullifier_lookup_key = HMAC(index_key, nullifier)
nullifier_lookup_key_id = key identifier for index_key
encrypted_nullifier = Encrypt(db_field_key, nullifier)
```

`nullifier_lookup_key`는 unique/index 용도의 deterministic keyed value임. key rotation을 고려해 `nullifier_lookup_key_id` 또는 `lookup_key_version`을 함께 저장하는 것이 좋음. raw nullifier는 암호화 저장하고, 로그나 telemetry에 남기지 않음.

`PayrollOperation` 또는 `PayrollItem` record에는 operation 성공 판정에 필요한 expected value를 저장함. 예를 들어 `expected_output_commitment`, `expected_disclosure_digest`, `expected_recipient_hash`, `expected_amount`, `expected_denom`, `batch_item_index`를 저장해야 함. 현재 Go payroll worker의 `expected_disclosure_digest`는 `PreparedTransferPayload.AuditDisclosureDigestHex`와 `MsgTransfer.AuditDisclosureDigest`에 대응하는 audit disclosure digest임. user disclosure digest 또는 sender self-view disclosure digest를 operation success evidence로 대신 쓰면 안 됨. `batch_item_index`는 zero value와 실제 0번 item을 구분할 수 있도록 `batch_item_index_known` 같은 boolean과 함께 저장함. payroll/batch operation은 이 값을 true로 두고, batch 위치가 성공 판정에 포함되지 않는 직접 연동만 false를 사용할 수 있음. recipient와 amount가 민감하면 원문 대신 encrypted value, hash, HMAC 형태로 저장함.

## Active reservation 중복 방지

같은 `owner_key_id + nullifier_lookup_key` 조합에 대해 active reservation은 하나만 존재해야 함. 이 제약이 없으면 두 planner나 worker가 같은 note를 동시에 payroll, split, merge, 일반 transfer에 배정할 수 있음.

PostgreSQL을 사용하는 server/control-plane DB라면 partial unique index를 권장함.

```sql
CREATE UNIQUE INDEX uniq_active_note_reservation
ON note_reservations(owner_key_id, nullifier_lookup_key)
WHERE status IN (
  'Reserved',
  'Proving',
  'ProofReady',
  'Submitted',
  'Unknown',
  'ManualReview'
);
```

repo의 `x/privacy/client/sdk/reservation.SQLStore`는 이 제약을 포함한 PostgreSQL/SQLite reference schema를 제공함. 실제 production DB는 같은 active uniqueness 의미를 유지하되, tenant partitioning, field-level encryption, migration, connection pool 정책을 제품 환경에 맞게 추가해야 함.

local SQLite, IndexedDB, mobile DB처럼 partial unique index 지원이 제한적이면 다음 중 하나를 사용함.

- `owner_key_id` 단위 single writer queue
- process-local mutex와 DB transaction 조합
- reservation 생성 전 active status 재조회
- conflict 발생 시 기존 reservation을 우선하고 새 plan을 replan 처리

중요한 원칙은 active reservation 중복을 application logic만으로 기대하지 않는 것임. 가능하면 DB 제약으로 한 번 더 막음.

## Payroll plan 단계

`payroll plan`은 다음 순서로 동작함.

```text
1. payroll input 검증
2. recipient 중복 검사
3. treasury note inventory scan
4. Available note만 후보로 선택
5. amount별 note allocation 계산
6. 필요한 경우 shard note 부족 경고
7. reservation 생성
8. plan file 또는 DB record 저장
```

이 단계에서는 transaction을 보내지 않음. 대신 어떤 note를 어떤 payroll item 또는 chunk에 사용할지 결정하고 lock을 검.

이때 한 번에 너무 큰 note를 계속 change로 돌려 쓰는 plan은 피해야 함. change note chain이 생기면 병렬 proof 생성이 어렵기 때문임. payroll 전에는 treasury를 여러 shard note로 나눠두고, 각 chunk가 서로 다른 shard를 사용하게 하는 편이 좋음.

### DB transaction 기반 note selection

planner는 `Available` note를 선택하고 `Reserved`로 전환하는 작업을 하나의 DB transaction 안에서 수행해야 함. note 선택과 reservation 생성이 분리되면 두 planner가 같은 note를 동시에 선택할 수 있음.

PostgreSQL 기준 예:

```sql
BEGIN;

SELECT note_id, nullifier_lookup_key
FROM note_inventory
WHERE owner_key_id = $1
  AND spend_status = 'Available'
  AND reservation_id IS NULL
ORDER BY discovered_height, note_id
FOR UPDATE SKIP LOCKED
LIMIT $2;

-- 선택한 note에 대해 note_reservations row 생성
-- note_inventory.reservation_id 갱신

COMMIT;
```

`FOR UPDATE SKIP LOCKED`를 쓰면 동시에 여러 planner가 돌아도 이미 다른 transaction이 잡은 note를 건너뛸 수 있음.

DB가 `SKIP LOCKED`를 지원하지 않는 경우 대체 방식이 필요함.

- `owner_key_id` 단위 advisory lock
- `owner_key_id` 단위 single writer queue
- local SQLite transaction과 process-local mutex 조합
- browser 환경에서는 IndexedDB transaction과 background worker 단일화

대량 payroll을 안정적으로 처리하려면 "Available note 조회"와 "Reserved 전환"은 반드시 원자적으로 묶어야 함.

## Payroll run 단계

`payroll run`은 reservation이 있는 item만 실행함.

```text
1. Reserved note 조회
2. proof 직전 local lock 확인
3. chain에서 nullifier 미사용 여부 확인
4. state를 Proving으로 변경
5. proof 생성
6. state를 ProofReady로 변경
7. broadcast 직전 nullifier 재확인
8. transaction 제출
9. state를 Submitted로 변경
10. confirmation scanner/reconcile worker가 note 상태와 operation 상태를 각각 갱신
```

proof 생성과 broadcast 사이에는 시간이 생길 수 있음. 이 사이에 다른 프로세스가 같은 note를 사용하지 못하도록 local lock이 유지되어야 함.

### Compare-and-set 상태 전이

reservation 상태 변경은 항상 현재 상태를 조건으로 거는 compare-and-set 방식으로 수행함. 그래야 이미 다른 worker가 처리한 reservation을 뒤늦은 worker가 덮어쓰지 않음.

예:

```sql
UPDATE note_reservations
SET status = 'Proving',
    updated_at = NOW()
WHERE reservation_id = $1
  AND status = 'Reserved';
```

영향 받은 row 수가 0이면 상태 전이에 실패한 것임. 이 경우 worker는 현재 reservation 상태를 다시 읽고, 자기 작업을 중단하거나 새 상태에 맞게 처리해야 함.

권장 상태 전이 예:

| 전이 | 조건 |
| --- | --- |
| `Reserved -> Proving` | `status = 'Reserved'`이고 유효한 lease 획득 |
| `Proving -> ProofReady` | `status = 'Proving'`이고 같은 `lease_token` 보유 |
| `ProofReady -> Submitted` | `status = 'ProofReady'`이고 같은 `lease_token` 보유, broadcast 직전 nullifier unspent 확인 |
| `ProofReady -> Unknown` | 같은 `lease_token` 보유, signed tx 또는 broadcast attempt metadata는 있으나 RPC timeout 등으로 제출 결과가 불명확 |
| `ProofReady -> ConfirmedSpent` | local submitted 기록 전이라도 chain nullifier/tx evidence가 현재 operation과 일치해 이미 spend가 확인된 reconcile recovery |
| `Reserved -> Released` | proof/broadcast가 시작되지 않은 예약을 compare-and-set으로 해제 |
| `ManualReview -> Released` | 운영자가 proof artifact 폐기, tx 미제출, lease 상태를 확인해 재사용 가능하다고 승인 |
| `Submitted -> ConfirmedSpent` | 현재 operation과 일치하는 tx success 또는 spent 증거 확인 |
| `Submitted -> Unknown` | tx 결과 불명확 |
| `Unknown -> ManualReview` | 자동 판단 불가 |

`Unknown -> Submitted` 전이는 권장하지 않음. `Unknown`은 이미 signed tx 또는 broadcast attempt의 결과가 불명확한 상태이므로, reconcile worker는 같은 operation의 evidence를 확인해 `ConfirmedSpent`, `Failed`, `ReplanRequired`, `ManualReview` 중 하나로 좁히는 방식으로 처리함.

### Worker lease와 heartbeat

proof worker와 broadcaster worker는 reservation을 처리하기 전에 lease를 획득해야 함. lease는 worker가 살아 있고, 해당 reservation을 처리할 권한이 있음을 나타냄.

권장 필드:

- `lease_owner`
- `lease_token`
- `lease_until`
- `last_heartbeat_at`

worker는 작업 중 주기적으로 heartbeat를 보내 `lease_until`을 연장함. 상태 변경은 `lease_token`이 일치하는 경우에만 허용함.

proof worker는 `Reserved` 상태의 reservation에 대해서만 lease를 획득해야 함. 이미 `ProofReady` 또는 `Submitted` 상태인 reservation의 lease가 만료되어 있더라도 stale proof worker가 새 lease를 잡아 기존 broadcast 권한을 덮어쓰면 안 됨. 긴 proof 생성 작업에서는 `Proving` 상태와 같은 `lease_token`을 조건으로 heartbeat를 계속 갱신해야 함.

lease 획득, heartbeat 갱신, lease clear, worker-owned `ProofReady -> Submitted/Unknown` 전이는 저장소에서 원자적으로 처리해야 함. `ProofReady -> ConfirmedSpent`도 recovery/reconcile 증거를 조건으로 한 compare-and-set 또는 동일한 transaction/row lock 경로를 사용해야 하지만, broadcaster lease token으로 제출 권한을 행사하는 전이는 아님. 즉, application layer에서 reservation을 먼저 읽고 나중에 일반 update로 덮어쓰면 안 됨. DB 구현은 하나의 `UPDATE ... WHERE reservation_id = ? AND status = ? AND lease_token = ? AND lease_until > NOW()` 또는 동일한 transaction/row lock 안에서 조건 확인과 필드 갱신을 함께 수행해야 함.

예:

```sql
UPDATE note_reservations
SET status = 'ProofReady',
    lease_until = NULL,
    updated_at = NOW()
WHERE reservation_id = $1
  AND status = 'Proving'
  AND lease_token = $2;
```

이 규칙은 zombie worker 문제를 줄임. 예를 들어 죽은 줄 알았던 worker가 뒤늦게 살아나 오래된 proof나 tx를 제출하려고 할 때, lease token이 더 이상 유효하지 않으면 상태 변경과 broadcast를 막을 수 있음.

권장 정책:

- worker는 작업 시작 전 대상 상태를 조건으로 lease를 획득함. 예를 들어 proof worker는 `Reserved` 상태에서만 lease를 획득함.
- 긴 proof 생성 작업은 heartbeat로 lease를 연장함.
- lease가 만료되면 다른 worker가 takeover할 수 있음.
- 오래된 worker는 상태 변경 전 lease token을 다시 확인함.
- broadcaster는 `ProofReady` 상태와 유효 lease를 모두 확인한 뒤 tx를 제출함.

## Split / merge 정책

대량 전송에서는 split과 merge를 무작정 background job으로 돌리면 안 됨. split/merge도 note를 소비하는 transaction이기 때문에 payroll plan을 깨뜨릴 수 있음.

권장 정책:

- payroll 실행 전 `prepare treasury` 단계에서 큰 note를 shard note로 split함.
- split이 완료되고 scanner가 shard note를 확인한 뒤 payroll plan을 만듦.
- payroll 실행 중에는 같은 treasury에 대한 merge를 금지함.
- payroll 실행 중 추가 split이 필요하면 기존 reservation과 충돌하지 않는 available note만 사용함.
- payroll 완료 후 남은 change note를 merge하거나 다음 payroll shard로 재정리함.

예상 흐름:

```text
큰 treasury note
-> prepare split
-> shard note 1
-> shard note 2
-> shard note 3
-> payroll plan
-> reservation
-> payroll run
-> leftover merge 또는 next payroll reserve
```

이 구조를 쓰면 일반 transfer, payroll transfer, split/merge가 같은 note를 두고 경쟁하지 않음.

## Multi-message / MsgBatchTransfer와의 관계

multi-message transaction이나 `MsgBatchTransfer`는 여러 transfer item을 하나의 제출 단위로 묶음. 하지만 input note 충돌 문제를 자동으로 해결하지 않음.

따라서 batch를 만들기 전에 다음 검사를 해야 함.

- 같은 batch 안에 동일 `note_id`가 두 번 들어가지 않았는가
- 같은 batch 안에 동일 nullifier가 두 번 들어가지 않았는가
- batch에 들어간 모든 note가 `Reserved` 또는 `ProofReady` 상태인가
- reservation의 `payroll_id`, `batch_id`, `chunk_id`가 현재 batch와 일치하는가
- broadcast 직전 chain에서 nullifier가 아직 사용되지 않았는가

`MsgBatchTransfer`는 module에서 canonical validation과 batch 내부 nullifier 중복 검사를 수행함. 하지만 "이 nullifier가 off-chain에서 어떤 payroll에 예약되어 있었는지"는 keeper가 모름. 따라서 reservation 자체는 여전히 client/control plane의 책임임.

## 실패 대응

중요한 원칙은 다음과 같음.

```text
Submitted / Unknown / ManualReview 상태의 note는 TTL만으로 Available 처리하지 않음.
```

RPC timeout, mempool eviction, scanner 지연처럼 실패로 보이는 상황에서도 실제 transaction이 chain에 포함되었을 수 있음. 따라서 release 전에 반드시 tx hash와 nullifier 상태를 reconcile해야 함.

### nullifier already spent

원인:

- 같은 note가 다른 transaction에서 먼저 소비됨
- scanner가 늦게 반영한 spent note를 planner가 선택함
- split/merge와 payroll이 충돌함

대응:

```text
1. 해당 item 또는 chunk를 Failed로 표시
2. 사용된 note를 ConfirmedSpent로 갱신
3. 현재 operation/payment 증거와 일치하는지 확인
4. 일치하면 payment success 처리
5. 불일치하거나 증거가 부족하면 operation을 ManualReview 또는 ConflictSpent로 표시
6. 같은 note를 참조하는 다른 pending item을 ReplanRequired로 표시
7. Available note에서 새 input note 선택
8. proof 재생성
9. 재시도
```

### transaction timeout 또는 결과 불명확

원인:

- RPC timeout
- mempool eviction
- sequence mismatch
- scanner 지연

대응:

```text
1. 제출 attempt metadata가 있으면 ProofReady 또는 Submitted 상태를 Unknown으로 전환
2. tx_hash가 있으면 tx 조회
3. tx 성공이면 expected output/audit disclosure digest/recipient/amount와 일치하는지 확인
4. tx 실패이면 실패 원인 분류
5. tx가 없으면 nullifier 조회
6. nullifier가 spent이면 note 상태는 ConfirmedSpent로 갱신
7. spent 증거가 현재 operation과 일치하면 payment success 처리
8. spent 증거가 불일치하거나 부족하면 ManualReview 또는 ConflictSpent로 보냄
9. nullifier가 unspent이고 tx도 없으면 retry 또는 tx 재구성 검토
10. 일정 시간 후에도 불명확하면 ManualReview로 보냄
```

### proof generated but not broadcast

원인:

- broadcaster 장애
- fee 부족
- tx size/gas 초과

대응:

```text
1. note는 계속 ProofReady lock 유지
2. fee/gas/chunk size 조정
3. broadcast 재시도
4. 오래 지연되면 proof 폐기 여부 확인
5. tx가 제출되지 않았고 proof artifact를 폐기했음이 확인된 뒤 `ManualReview -> Released` 또는 `ReplanRequired` 경로로 처리
```

### chunk 일부 item 문제

multi-message transaction이나 `MsgBatchTransfer`는 보통 all-or-nothing으로 처리됨. 하나의 item이 invalid하면 전체 chunk가 실패할 수 있음.

대응:

```text
1. chunk 전체를 Failed로 표시
2. item별 사전 검사를 다시 수행
3. 문제 item을 분리
4. 나머지 item으로 작은 chunk 재구성
5. 문제 item은 ReplanRequired 처리
```

## TTL / release 정책

`expires_at`은 모든 상태에 같은 방식으로 적용하면 안 됨. 상태별로 release 가능 여부가 다름.

| 상태 | TTL 만료 시 정책 |
| --- | --- |
| `Reserved` | proof/broadcast가 시작되지 않았으면 release 가능. 단 release도 compare-and-set으로 수행함. |
| `Proving` | worker lease가 만료되면 proof artifact와 attempt record를 확인함. proof가 저장되지 않았으면 `Reserved`로 되돌리거나 `ReplanRequired`로 보냄. proof 완성 여부가 불명확하면 자동 release하지 않고 `ManualReview`로 보냄. |
| `ProofReady` | TTL만으로 `Available`로 되돌리면 안 됨. proof 폐기와 tx 미제출이 확인되어도 직접 release하지 않고 `ManualReview` 또는 `ReplanRequired`로 보냄. 운영자가 재사용 가능하다고 승인한 경우에만 `ManualReview -> Released -> Available`을 사용함. chain spent 증거가 현재 operation과 일치하면 `ProofReady -> ConfirmedSpent`로 reconcile할 수 있음. |
| `Submitted` | TTL만으로 `Available`로 되돌리면 안 됨. tx가 RPC timeout처럼 보여도 실제로 chain에 들어갔을 수 있음. |
| `Unknown` | 반드시 tx hash/nullifier 조회로 reconcile한 뒤 상태를 바꿈. |
| `ManualReview` | 운영자가 확인하기 전까지 active lock 상태로 유지함. |

자동 release를 허용할 수 있는 상태는 제한적임.

```text
Reserved -> Released -> Available
```

그 외 상태는 proof artifact, tx hash, nullifier, worker lease를 확인한 뒤에만 상태를 바꿈.

broadcast worker가 RPC/network error를 받았지만 tx hash, tx bytes hash, sign doc hash 같은 broadcast attempt metadata를 얻지 못한 경우에는 `ProofReady`를 `Unknown`으로 바꾸지 않음. 이때는 기존 `ProofReady` lock과 lease를 유지하고, takeover lease를 획득한 worker라면 그 lease가 만료된 뒤 재시도 worker가 다시 획득하게 둠. metadata가 있는 error 또는 non-zero tx code처럼 제출 attempt를 식별할 수 있을 때만 `ProofReady -> Unknown`으로 기록함.

## Submitted / Unknown / ManualReview reconcile

`Submitted` 또는 `Unknown` 상태는 다음 순서로 확인함.

```text
1. tx_hash가 있으면 tx 조회
2. tx 성공이면 expected output/audit disclosure digest/recipient/amount와 일치하는지 확인
3. tx 실패이면 실패 원인 확인
4. tx가 없으면 nullifier 조회
5. nullifier spent이면 note 상태는 ConfirmedSpent로 갱신
6. spent 증거가 현재 operation과 일치하면 payment success 처리
7. spent 증거가 불일치하거나 부족하면 ManualReview 또는 ConflictSpent
8. nullifier unspent이고 tx도 없으면 retry 또는 tx 재구성 검토
9. 계속 불명확하면 ManualReview
```

실패 원인별 정책:

| 실패 원인 | 처리 |
| --- | --- |
| RPC timeout | tx_hash/nullifier 조회 후 retry 가능 |
| mempool eviction | tx_hash/nullifier 조회 후, 저장된 signed tx bytes가 있으면 동일 tx 재전송을 검토함. 없으면 nullifier unspent 확인 뒤 재구성 검토 |
| gas 부족 | nullifier unspent 확인 후 gas 조정 및 재서명 가능 |
| sequence mismatch | account sequence 확인 후 재서명 가능. 단 재서명 전 nullifier unspent 확인 필요 |
| proof invalid | `ReplanRequired` |
| nullifier spent | note 상태는 `ConfirmedSpent`로 갱신. operation 성공은 expected output/audit disclosure digest/recipient/amount가 현재 operation과 일치할 때만 인정. 불일치하거나 증거가 부족하면 `ManualReview` 또는 operation-level `ConflictSpent` |
| root invalid | 새 root 기준 proof 재생성이 필요하므로 `ReplanRequired` |
| payload mismatch | proof/payload 불일치 원인 조사 후 `ReplanRequired` 또는 `ManualReview` |

`ManualReview`는 active lock 상태임. 운영자가 확인하기 전까지 같은 note를 다른 job에 배정하면 안 됨.

## Tx retry idempotency

broadcast retry는 `operation_id` 기준으로 idempotent해야 함. retry 때마다 새로운 논리 작업을 만들면 sequence, fee, nullifier 상태가 꼬일 수 있음.

같은 payment item을 replan하는 경우에는 기존 operation과 새 operation을 구분할 수 있는 attempt/run 차원이 필요함. 최초 plan은 기존 `payroll_id:item_id` 형태를 유지할 수 있지만, replan 결과는 `payroll_id:item_id:attempt:N`처럼 새 `operation_id`와 새 `reservation_id`를 사용해야 기존 terminal 또는 review 대상 operation과 충돌하지 않음.

권장 필드:

- `operation_id`
- `sign_doc_hash`
- `tx_bytes_hash`
- `tx_hash`
- `account_sequence`
- `broadcast_attempt_count`
- `last_broadcast_at`
- `last_broadcast_error`

권장 정책:

- 동일 `operation_id`의 tx는 하나의 논리 작업으로 취급함.
- sign된 tx bytes와 tx hash를 저장함.
- RPC timeout 후에는 새 tx를 바로 만들지 말고 tx_hash 조회를 먼저 함.
- 동일 tx bytes 재전송은 허용할 수 있음.
- 새 sequence로 재서명하기 전에는 nullifier가 unspent인지 확인함.
- gas/sequence 문제로 tx를 재구성해야 하는 경우에도 기존 `operation_id`와 reservation을 유지함.
- nullifier가 spent이면 note 상태는 `ConfirmedSpent`로 갱신할 수 있음. 다만 tx 조회가 실패한 상태에서 payment success로 처리하려면 expected output/audit disclosure digest/recipient/amount가 현재 operation과 일치한다는 별도 증거가 필요함.

현재 Go reference SDK는 `tx_hash`, `tx_bytes_hash`, `sign_doc_hash` 같은 식별자와 hash를 저장하는 contract를 제공함. 실제로 동일 signed tx bytes를 재전송하려면 scheduler 또는 broadcaster queue가 원본 signed tx bytes를 별도 durable storage에 보관해야 함. 이 저장소가 없다면 timeout/mempool 계열 실패는 `ReconcileUnknown` 흐름으로 보고, `tx_hash`와 nullifier 상태 확인을 먼저 수행한 뒤 재서명 또는 replan 여부를 결정함.

예상 흐름:

```text
ProofReady
-> sign tx
-> operation_id 생성 또는 재사용
-> sign_doc_hash / tx_bytes_hash / tx_hash 저장
-> broadcast
-> timeout 발생
-> tx_hash 조회
-> nullifier 조회
-> unspent이고 signed tx bytes가 있으면 동일 tx 재전송 검토
-> signed tx bytes가 없거나 gas/sequence 조정이 필요하면 재서명 검토
-> spent이면 note ConfirmedSpent
-> operation 증거 일치 시 payment success
-> 증거 부족 또는 불일치 시 ManualReview 또는 ConflictSpent
```

## 동시성 제어

note reservation은 single writer 원칙이 중요함. 같은 treasury key에 대해 여러 프로세스가 독립적으로 note를 선택하면 lock이 깨질 수 있음.

권장 구조:

```text
wallet / payroll control plane
-> single note inventory DB
-> transaction lock 또는 advisory lock
-> planner
-> prover queue
-> broadcaster queue
```

동시성 규칙:

- 같은 `owner_key_id`에 대해 note selection은 하나의 writer만 수행함.
- proof worker는 이미 예약된 job만 처리함.
- broadcaster는 `ProofReady` 상태만 제출함.
- split/merge worker도 같은 reservation DB를 사용함.
- 수동 wallet transfer는 payroll treasury key를 공유하지 않음.

server/control-plane 환경에서는 PostgreSQL transaction, `FOR UPDATE SKIP LOCKED`, partial unique index 조합을 권장함. local/mobile/web 환경에서는 같은 기능을 그대로 쓰기 어려울 수 있으므로 `owner_key_id` 단위 single writer queue, SQLite transaction, IndexedDB transaction, process-local mutex를 조합함.

## Reservation DB 민감정보 보호

reservation DB는 privacy-sensitive DB로 취급해야 함. nullifier, commitment, amount, payroll item, recipient mapping이 함께 있으면 DB 유출만으로 payroll activity가 많이 드러날 수 있음.

권장 정책:

- DB at-rest encryption을 사용함.
- 가능하면 field-level encryption을 적용함.
- unique/index 용도는 raw nullifier 대신 `nullifier_lookup_key = HMAC(index_key, nullifier)` 같은 deterministic keyed value를 사용함.
- raw nullifier는 암호화 저장함.
- nullifier, commitment, recipient, amount 원문을 로그에 남기지 않음.
- telemetry/analytics로 reservation payload를 전송하지 않음.
- 운영자 권한을 분리함.
- proof payload와 reservation detail retention 정책을 둠.
- UI에서는 nullifier와 commitment를 축약 표시함.

권장 저장 방식:

```text
index/search:
  nullifier_lookup_key = HMAC(index_key, nullifier)

encrypted fields:
  encrypted_nullifier
  encrypted_amount
  encrypted_recipient
  encrypted_payroll_item

logs/telemetry:
  reservation_id
  company_id
  payroll_id
  status
  error_code
  shortened tx_hash
```

이 방식은 운영자가 상태를 추적할 수 있게 하면서, DB나 로그 유출 시 payroll 세부 정보가 그대로 노출되는 위험을 줄임.

## Protocol-level reservation 검토

on-chain reservation을 만들 수도 있음.

예:

```text
MsgReserveNotes
  creator
  reservation_id
  nullifiers[]
  expires_at
```

이 방식은 chain이 예약된 nullifier를 알 수 있으므로 더 강한 보장을 줄 수 있음. 하지만 기본 추천은 아님.

이유:

- nullifier를 spend 전에 공개하면 privacy가 약해질 수 있음.
- 예약만 하고 실제 spend하지 않는 경우 lock 해제와 만료 처리가 필요함.
- 악의적 사용자가 reservation state를 부풀리는 DoS가 가능함.
- 예약 권한 검증이 복잡함.
- 기존 transfer keeper 로직과 nullifier lifecycle이 복잡해짐.

Protocol-level reservation은 다음 조건이 생겼을 때 검토하는 것이 좋음.

- 여러 독립 주체가 같은 treasury를 동시에 운영해야 함.
- off-chain single writer를 신뢰할 수 없음.
- reservation 자체를 감사 가능한 on-chain 상태로 남겨야 함.
- batch 실행 실패 비용이 매우 커서 on-chain 예약 보장이 필요함.

그 전까지는 client/control plane reservation과 treasury shard 운영으로 충분히 시작할 수 있음.

## 클라이언트 구현 체크리스트

- note inventory DB가 있는가
- note 상태가 `Discovered`, `Available`, `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `ManualReview`, `ConfirmedSpent`, `Failed`, `Released`, `ReplanRequired`로 관리되는가
- `payroll plan` 확정 시점에 선택된 note를 즉시 `Reserved`로 전환하는가
- `payroll plan`이 available note만 선택하는가
- active reservation에 대해 `owner_key_id + nullifier_lookup_key` unique constraint 또는 동등한 보장이 있는가
- `nullifier_lookup_key`에 대해 `index_key_id` 또는 `lookup_key_version`을 함께 저장하는가
- operation 성공 판정에 필요한 expected output commitment, audit disclosure digest, recipient hash, amount, denom, batch item index가 `PayrollOperation` 또는 `PayrollItem`에 저장되는가
- planner가 DB transaction, row lock, `FOR UPDATE SKIP LOCKED` 또는 대체 single writer lock으로 note를 선택하는가
- 상태 전이가 compare-and-set 방식으로 구현되어 있는가
- proof worker와 broadcaster가 lease, heartbeat, lease token을 사용하는가
- 같은 note/nullifier가 같은 job 또는 chunk에 중복 배정되지 않는가
- proof 직전과 broadcast 직전에 nullifier 상태를 다시 확인하는가
- split/merge worker가 reservation DB를 참고하는가
- payroll 실행 중 같은 treasury의 background merge가 금지되는가
- 실패 item 또는 chunk만 replan할 수 있는가
- transaction 결과가 불명확한 경우 `Unknown/ManualReview` 상태와 reconcile worker/process가 있는가
- `Submitted`, `Unknown`, `ManualReview` 상태가 TTL만으로 `Available` 처리되지 않는가
- tx retry가 `operation_id` 기준으로 idempotent하게 처리되는가
- tx hash와 nullifier 조회 순서로 reconcile하는가
- `ConfirmedSpent`를 payment success로 처리하기 전에 expected output commitment, audit disclosure digest, recipient, amount가 현재 operation과 일치하는지 확인하는가
- reservation DB가 raw nullifier/amount/recipient를 평문 로그에 남기지 않는가
- payroll treasury key가 일반 wallet transfer와 분리되어 있는가

## 권장 MVP 범위

초기 구현은 다음 수준이면 충분함.

```text
1. local note inventory DB
2. note reservation table
3. active reservation unique constraint 또는 single writer lock
4. payroll plan에서 즉시 reservation 생성
5. DB transaction 기반 note selection
6. compare-and-set 상태 전이
7. worker lease/heartbeat
8. proof/broadcast 전 nullifier 재확인
9. split/merge worker와 reservation 연동
10. Submitted/Unknown reconcile
11. operation_id 기반 tx retry
12. 실패 item/chunk replan
13. reservation DB 민감정보 보호
14. payroll report에 reservation/retry 이력 포함
```

이 MVP는 protocol 변경 없이 구현할 수 있음. 이후 `MsgBatchTransfer`, N-output batch circuit, payroll Merkle distribution을 도입하더라도 같은 reservation model을 상위 운영 레이어에서 계속 재사용할 수 있음.
