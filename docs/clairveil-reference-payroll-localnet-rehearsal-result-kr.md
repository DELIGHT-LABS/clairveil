# Clairveil Reference Payroll Localnet Rehearsal 결과

## 목적

이 문서는 Reference Payroll Product의 localnet rehearsal 실행 결과를 기록함.

기록 범위는 다음 세 가지임.

- seeded note harness를 사용한 1천건 actual localnet restart/retry rehearsal 성공 결과
- deposit mode 1천건 actual localnet restart/retry rehearsal 시도 결과
- 같은 구현에서 multi-chunk restart/retry 경로가 동작하는지 확인한 작은 actual localnet run 결과

## 실행 일시와 기준

- 실행일: 2026-07-08
- branch: `private/bulk-transfer-v2`
- 대상 script: `scripts/reference-payroll-live-localnet.sh`
- 관련 문서: [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md)

## 1천건 seeded actual localnet rehearsal 성공

직원 1천건을 repo-local 개발 세션 안에서 끝까지 검증하기 위해 localnet 전용 seeded note harness를 사용함.

실행 명령은 다음과 같음.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k-seeded \
PAYROLL_SEED_NOTES=1 \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

이 run은 성공함.

```text
Reference payroll live localnet tutorial passed.
Transfer batch chunks: 50
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k-seeded  2465.89s user 88.06s system 475% cpu 8:56.73 total
```

요약 결과는 다음과 같음.

```json
{
  "chunk_count": 50,
  "chunk_size": 20,
  "confirmed_items": 1000,
  "confirmed_spent_reservations": 2000,
  "final_payroll_status": "Confirmed",
  "payroll_item_amount": "1",
  "payroll_item_count": 1000,
  "seeded_notes": true,
  "succeeded_operations": 1000
}
```

최종 state summary는 다음과 같음.

```json
{
  "reservation_total": 2000,
  "operation_total": 1000,
  "reservations_by_status": {
    "ConfirmedSpent": 2000
  },
  "operations_by_status": {
    "Succeeded": 1000
  }
}
```

seed report는 다음과 같음.

```json
{
  "schema_version": "clairveil.payroll_localnet_seed.v1",
  "seeded_item_count": 1000,
  "seeded_note_count": 2000,
  "amount_note_count": 1000,
  "zero_dummy_count": 1000,
  "amount": "1",
  "denom": "uclair",
  "commitments_added": 2000,
  "existing_commitments": 0
}
```

이 run에서 확인한 것은 다음과 같음.

- localnet genesis에 Alice의 amount note 1천개와 zero dummy note 1천개를 넣고, 같은 note를 Alice wallet cache와 `alice-notes.json`에 기록함.
- 이 seed는 localnet rehearsal 준비용이며 production note preparation 로직이 아님.
- payroll input 생성, validation, note preparation 분석, plan 생성, durable reservation 확정이 1천건 기준으로 동작함.
- 같은 plan에 대해 `clairveil-payroll run`을 한 번 더 실행해도 중복 reservation 없이 idempotent하게 재개됨.
- `PAYROLL_CHUNK_SIZE=20` 기준 실제 `transfer-batch` tx 50개가 localnet에 broadcast됨.
- 각 transfer-batch는 실제 Groth16 proof를 만들고 실제 chain tx로 포함됨.
- Bob wallet scan에서 chunk마다 20개의 output note가 확인됨.
- 각 chunk가 `settle-transfer-batch -item-start -item-limit`로 plan의 해당 구간만 settle함.
- 최종 payroll report가 `Confirmed`가 됨.

## Deposit mode 1천건 actual localnet rehearsal 시도

시도한 명령은 다음과 같음.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

이 run은 완료하지 않음. 준비 단계에서 시간이 과도하게 길어지는 것이 확인되어 수동 중단함.

중단 시점까지 생성된 partial artifact는 다음과 같음.

```text
tmp/reference-payroll-live-localnet-1k/out/
```

중단 시점에 `deposit-payroll-*.txhash`는 10개 생성되어 있었음. 최종 `rehearsal-summary.json`, `payroll-status-after-settle.json`, `payroll-final-report.json`는 생성되지 않음.

## 중단 사유

현재 actual localnet tutorial은 직원 1명당 다음 note를 실제 chain tx로 준비함.

```text
amount note 1개
zero dummy note 1개
```

따라서 1천건 run은 transfer 전에만 최소 2천개의 deposit tx와 deposit proof가 필요함.

이번 시도에서 deposit proof/broadcast/wait는 wall-clock 기준 수 초 단위로 반복되었음. partial run 기준으로는 10개 deposit tx까지 진행하는 데 약 50초가 걸렸고, 이 속도를 단순 외삽하면 deposit 준비만 2시간 30분 이상 걸릴 수 있음. 이후 1천개의 transfer proof와 50개의 `transfer-batch` tx도 추가로 필요함.

결론적으로 deposit mode actual localnet 경로는 correctness smoke와 chunk/retry 확인에는 유용하지만, 1천건 actual localnet full run을 개발 세션 안에서 빠르게 완료하는 도구로는 적합하지 않음. 1천건 이상 restart/retry rehearsal은 `PAYROLL_SEED_NOTES=1` seed mode를 사용함.

## 통과한 actual localnet multi-chunk smoke

1천건 full run 대신 같은 script와 같은 restart/retry/chunk settle 경로로 작은 actual localnet run을 실행함.

실행 명령은 다음과 같음.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-chunk-smoke \
PAYROLL_ITEM_COUNT=4 \
PAYROLL_CHUNK_SIZE=2 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

이 run은 성공함.

요약 결과는 다음과 같음.

```json
{
  "chunk_count": 2,
  "chunk_size": 2,
  "confirmed_items": 4,
  "confirmed_spent_reservations": 8,
  "final_payroll_status": "Confirmed",
  "payroll_item_amount": "1",
  "payroll_item_count": 4,
  "succeeded_operations": 4
}
```

최종 state summary는 다음과 같음.

```json
{
  "reservation_total": 8,
  "operation_total": 4,
  "reservations_by_status": {
    "ConfirmedSpent": 8
  },
  "operations_by_status": {
    "Succeeded": 4
  }
}
```

확인한 것은 다음과 같음.

- `clairveil-payroll run` 재실행이 같은 plan을 idempotent하게 처리함.
- `PAYROLL_CHUNK_SIZE=2`로 2개 transfer-batch tx가 생성됨.
- 각 tx는 `settle-transfer-batch -item-start -item-limit`로 plan의 해당 구간만 settle함.
- 최종 payroll report가 `Confirmed`가 됨.

## Simulation rehearsal 결과

actual localnet 1천건 run과 별개로 1천건, 1만건, 10만건, 100개 회사 x 1천건 profile은 simulation rehearsal로도 산출함.

실행 명령은 다음과 같음.

```bash
make reference-payroll-rehearsal
```

1천건 scenario의 주요 결과는 다음과 같음.

```text
proof_count: 1000
tx_envelope_count: 50
estimated_total_seconds: 144.37556125999438
estimated_total_minutes: 2.4062593543332396
payroll_items_per_sec: 6.92638
```

이 simulation은 phase 1 구조에서 proof 수와 tx envelope 수를 추정하는 용도임. actual localnet note preparation 비용은 별도임. seeded actual localnet 1천건 run의 wall-clock은 약 8분 57초였고, simulation의 약 2분 24초는 순수 capacity model에 가까운 값이므로 local machine proof logging, chain wait, wallet scan, settle overhead를 포함한 실제 실행 시간과 다를 수 있음.

## 해석

현재 구현은 localnet에서 1천건 규모의 chunked payroll transfer와 restart/retry invariant를 검증할 수 있음.

다만 seed mode는 localnet rehearsal 준비 시간을 줄이기 위해 genesis commitment와 wallet cache를 직접 구성하는 도구임. production에서 treasury note를 만드는 방식이 아니며, staging/testnet 또는 production에서는 실제 deposit, split/merge, 운영 approval 기반 note preparation을 사용해야 함.

대규모 production rehearsal에는 아래 중 하나가 필요함.

- 대량 deposit을 더 빠르게 준비하는 batch deposit
- staging/testnet에서 장시간 soak test로 실행하는 runbook
- note preparation tx를 병렬화하거나 여러 treasury account로 sharding하는 운영 harness
- recursive split/merge 또는 preparation planner를 실제 worker로 연결한 rehearsal

## 다음 권장 작업

1. local developer 1천건 restart/retry rehearsal은 `PAYROLL_SEED_NOTES=1` seed mode를 표준으로 사용함.
2. local developer rehearsal은 `PAYROLL_ITEM_COUNT=4 PAYROLL_CHUNK_SIZE=2` 이상의 작은 multi-chunk actual run으로 유지함.
3. 1만건 이상은 `make reference-payroll-rehearsal` simulation과 staging/testnet soak test 결과를 함께 판단함.
4. production note preparation 성능을 검증하려면 seed mode가 아니라 batch deposit, split/merge, approval 기반 preparation planner를 별도 rehearsal로 실행함.
