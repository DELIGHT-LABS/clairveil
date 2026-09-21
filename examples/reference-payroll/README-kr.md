# Reference Payroll

이 문서는 payroll control-plane 계약과 실행 예제를 함께 다룹니다. Demo는 simulated legacy multi-message 처리이며 실제 proof 생성이나 transaction broadcast를 하지 않습니다. One-proof 통합 계약은 아래에 따로 설명합니다.

이 예제는 Clairveil repo만으로 payroll 대량전송 제품 흐름을 끝까지 체험하기 위한 최소 입력 파일임.

## 실행

repo root에서 아래를 실행함.

```bash
make reference-payroll-demo
```

또는 출력 디렉토리를 지정함.

```bash
OUT_DIR=tmp/my-payroll-demo ./scripts/reference-payroll-demo.sh
```

## Demo 흐름

스크립트는 아래 순서로 실행됨.

```text
validate
prepare-notes
plan
run
status
clairveil-payrolld -once
status
export-report -state
```

이 demo는 `clairveil-payrolld`의 기본값인 `simulated` mode를 사용함. 이 mode에서는 실제 proof 생성과 chain broadcast를 하지 않고, durable reservation state 위에서 proof ready, submitted, reconcile 결과를 시뮬레이션함.

`clairveil-payrolld`는 long-running scheduler 표면인 `live` mode도 지원함. CLI reference live mode는 tx evidence 파일에서 submitted/unknown operation을 reconcile하며, production proof 생성과 broadcast는 SDK live executor 또는 외부 worker로 연결함.

## Demo 출력

기본 출력은 `tmp/reference-payroll-demo/` 아래에 생성됨.

| 파일 | 의미 |
| --- | --- |
| `validation.json` | payroll input validation 결과 |
| `note-preparation.json` | note 준비 상태와 operation hint |
| `plan.json` | draft payroll plan |
| `confirmed-plan.json` | reservation을 확정한 plan |
| `reservation-state.json` | durable reservation/operation state |
| `payrolld-report.json` | simulated daemon tick report |
| `status-after-daemon.json` | daemon 실행 후 state summary |
| `final-report.json` | item별 최종 payroll report |

성공 기준:

```text
status-after-daemon.json:
  reservations_by_status.ConfirmedSpent = 전체 reservation 수
  operations_by_status.Succeeded = 전체 operation 수

final-report.json:
  status = Confirmed
```

## 입력 파일 수정

`payroll-demo.json`의 `items`와 `treasury_notes`를 바꾸면 다른 payroll run을 시험할 수 있음.

현재 transfer circuit은 지급건마다 input note 2개가 필요하므로, demo 입력도 각 item이 exact/pairable note와 zero dummy note를 확보한 상태로 구성되어 있음.

## One-proof와 legacy 경로

이 demo는 legacy multi-message control-plane 경로다. `transfer-batch`와 `clairveil-payroll ... settle-transfer-batch`는 하나의 Cosmos transaction에 독립 native 2x2 `MsgTransfer` message/proof 여러 개를 넣는다. 이 경로는 regression/tutorial 용도이며 one-proof batch라고 설명·제출·reconcile·capacity plan하면 안 된다.

Payroll batch integration과 `/v1/proofs/batch-transfer` route는 legacy-only이며 현행 V2 workflow가 아니다. Current V2는 `clairveil.privacy.v2.MsgBatchTransfer`와 공통 `POST /v2/prover/audit-field` route를 사용한다. [legacy reference 경계](../../docs/clairveil-getting-started-kr.md#8-legacy-batchjoinsplit16x32-reference)를 참고한다.

## 권한과 소유 범위

불일치가 있으면 다음 순서로 판단한다.

1. Consensus protocol과 구현: [`proto/clairveil/privacy/v1/tx.proto`](../../proto/clairveil/privacy/v1/tx.proto), `x/privacy/types`, keeper, [batch contract](../../docs/clairveil-batch-joinsplit-16x32-kr.md).
2. Cross-client boundary: [batch-transfer fixture](../../x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json), [reservation fixture](../../x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json), `x/privacy/client/sdk/conformance` test.
3. Reference behavior: `x/privacy/client/sdk/batchtransfer`, `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`, [CLI reference](../../docs/clairveil-cli-reference-kr.md), [JS SDK handoff](../../docs/clairveil-js-sdk-handoff-kr.md).

세 계층 모두 같은 checkout/tag로 pin한다. Fixture는 interoperability 기대값을 정하며, fixture를 고쳐야 할 때 protocol/code가 normative authority다.

Repository는 `MsgBatchTransfer` protocol/keeper, Go batch builder, bounded prover transport, typed scanner contract, reference durable graph/worker, fixture, CLI/tutorial을 제공한다. Downstream는 tenant isolation, production store, encryption/key management, worker/monitoring, signer/account-sequence coordination, RPC policy, product approval UX, wallet/JS 구현, audit 운영, incident response, production acceptance를 소유한다. `DurableFileStore`, `SQLStore`, reference daemon은 contract 예시이지 필수 database/topology가 아니다.

## Durable reservation과 retry contract

Plan confirmation 시 `Available` note만 선택하고 operation 하나와 1..16개 reservation을 atomic하게 생성한다. `(owner_key_id, nullifier_lookup_key)`에 active unique key를 강제한다. Selection/reservation은 database transaction 하나 또는 동등한 single-writer critical section에서 수행하고, 이후 transition은 read-then-write가 아닌 compare-and-set(CAS)을 사용한다.

Active reservation state는 `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `ManualReview`이며 normal send, split, merge, payroll planning에서 제외한다. Reservation fixture의 transition을 따른다. `Submitted`, `Unknown`, `ManualReview`를 TTL 만료만으로 `Available`로 release하지 않으며, `Proving -> Released`, `ProofReady -> Released`, direct active-to-available shortcut도 허용하지 않는다.

Worker 변경에는 current non-expired lease token이 필요하다. `lease_owner`, `lease_token`, `lease_until`, `last_heartbeat_at`를 저장하고 lease acquire/heartbeat/clear와 CAS transition을 atomic store operation 하나에서 수행한다. Caller cancellation 뒤에도 proof generation 또는 broadcast가 실행 중이면 heartbeat한다. Lease expiry는 안전한 recovery만 허용하고 note를 spendable로 만들지 않는다.

Durable status 전이 전 private material을 저장한다. prepared payload/hash, proof/hash, exact signed transaction bytes/byte hash, sign-doc hash, tx hash, account sequence, broadcast attempt/error, reservation, expected/observed output evidence가 대상이다. `operation_id` retry는 exact stored signed bytes를 사용한다. 이전 batch outcome이 resolve된 뒤에만 별도 operation/reservation attempt를 만들며, atomic output list 일부만 rebuild하지 않는다.

## Reconciliation과 item evidence

Reconcile은 **tx hash first**이고 transaction이 없거나 불명확할 때만 그 뒤에 모든 input nullifier를 query한다. RPC timeout, mempool eviction, process crash, cancelled request는 `Unknown`이며 새 transaction 생성 권한이 아니다. Nullifier response 누락은 `unspent`가 아닌 safe failure다.

`nullifier_spent`는 input 소비만 증명한다. Batch status와 item evidence status를 분리한다. Item은 의도한 transaction identity와 expected output evidence가 일치할 때만 성공이다. 즉 output index, commitment, recipient hash, amount, denom 또는 asset ID, audit key ID/epoch, fixture가 요구하는 disclosure digest가 일치해야 한다. Evidence가 없거나 다르면 reservation은 `ConfirmedSpent`로 유지하고 item evidence는 `ManualReview` 또는 `ConflictSpent`가 된다.

Audit/full disclosure digest를 primary success predicate로 쓴다. Expected value가 있는 user/self-view digest는 별도로 확인하며 audit evidence를 대체할 수 없다. Reconciliation은 audit private key 없이 digest를 비교할 수 있지만 audit payload decrypt는 audit workflow 책임이다.

## Privacy, worker, 완료 gate

User disclosure 기본값은 `all-private` / `none`이다. `recipient-encrypted`는 등록·versioning된 recipient key가 있을 때만, `public`은 explicit policy 또는 test에서만 쓴다. Audit-key path와 audit digest는 분리한다. Note preparation은 approval-based이며 순서는 analyze, preview, approve, split/merge/funding action 실행, rescan/nullifier-check, plan finalization이다. key/version identifier가 포함된 `nullifier_lookup_key = HMAC(index_key, nullifier)`를 쓰고 raw nullifier, commitment, recipient, amount, payload/proof/signed bytes, payroll mapping은 at rest encryption한다. 이 값과 disclosure/viewing key를 log, telemetry, analytics, crash report에 넣지 않는다.

Planner, proof, broadcast, scanner/reconcile, operator review를 재시작 가능한 책임으로 운영한다. One-proof reference worker는 `x/privacy/client/sdk/payroll`의 `BatchProofWorker`, `IdempotentBatchBroadcastWorker`, `BatchReconcileWorker`다. Automatic multi-prover failover는 사용하지 않는다. JS/TS와 wallet은 fixture를 port하고, `privacy-note-v1`, canonical `privacy-fixed-v1` typed bytes, `AssetRegistryV1`, `(height, global_sequence, output_index)` scan cursor, 요청당 최대 1000개인 `POST /clairveil/privacy/v1/nullifiers`를 사용해야 한다. Forced-rescan/reorg 복구와 telemetry 유출 없는 disclosure key 표시·가져오기를 유지한다. Private artifact와 audit disclosure material의 보존 기간과 접근 통제를 정의한다.

Portable evidence는 다음과 같다.

```sh
go test ./x/privacy/client/sdk/conformance/... -count=1
go test ./x/privacy/client/sdk/... -count=1
make privacy-batch-joinsplit-localnet
make privacy-bulk-readiness-check
```

이 명령은 static/unit/synthetic legacy 검사일 뿐입니다. 0이 아닌 `RUN_LOCALNET`을 거절하며 live V2 증적을 제공하지 않으므로 해당 경계에는 별도로 문서화한 native V2 harness를 사용합니다.

Staging에서는 environment, pinned commit/artifact identity, configuration, result를 기록한다. 완료에는 fixture-compatible one-proof construction/proving/submission/scanning, active-note 독점 reservation, stale-worker 보호, 긴 proof/broadcast를 견디는 lease, encrypted replay-safe artifact, unknown outcome의 tx-hash-first reconciliation, 일치하는 item evidence, 강제되는 approval/disclosure/retention/operator-review policy, 실제 deployment에 대한 capacity 입증이 모두 필요하다. Checked-in payroll target은 live V2 behavior를 입증하지 않는다.
