# Clairveil Reference Payroll 대규모 Rehearsal 가이드

## 목적

이 문서는 현재 Session 3B one-proof rehearsal과 legacy phase 1 multi-message capacity model 및 날짜가 있는 localnet evidence를 구분함.

현재 protocol/reference 경로는 `BatchJoinSplit16x32` / `MsgBatchTransfer`임. Atomic operation 하나가 input note 1..16개를 소비하고 payment/change/padding output 1..32개를 proof 하나로 생성함. 기존 `make reference-payroll-rehearsal` model은 recipient당 proof 하나와 legacy multi-message tx envelope를 계속 계산함. 그 출력은 comparison evidence로 보존하되 현재 one-proof capacity model로 사용하면 안 됨.

## 현재 Session 3B One-Proof Gate

기본 conformance/static gate를 실행하고, 필요한 local resource와 development artifact가 있으면 actual node/prover/payroll workflow를 opt-in으로 실행함.

```bash
go test ./x/privacy/client/sdk/conformance -run TestSession3BBatchTransferContract -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

live 경로는 실행한 payroll batch의 proof 1개와 tx envelope 1개, many-input/one-operation/many-item payroll graph, batch/item evidence 분리, 실제 process/node restart, 저장한 동일 signed bytes retry, tx-hash-first reconcile, spent-nullifier conflict 처리, typed disclosure/decryption, 기본 no-cross-endpoint-failover privacy boundary를 검증함. [clairveil-session3b-batch-transfer-handoff-kr.md](clairveil-session3b-batch-transfer-handoff-kr.md)와 [clairveil-batch-joinsplit-localnet-tutorial-kr.md](clairveil-batch-joinsplit-localnet-tutorial-kr.md)를 따름.

이는 experimental reference gate이며 production 승인이 아님. Formal trusted setup, external audit, signed production artifact 배포, production-scale rehearsal은 별도 작업임.

## Legacy Phase 1 Simulation 명령

이 target은 실제 지급 10만건을 모두 chain tx로 제출하지 않음. 고정 input profile에서 legacy proof, tx envelope, chunk, 완료 시간 추정값을 반복 계산하고 아래의 작은 legacy localnet smoke를 선택적으로 실행함.

기본 legacy comparison 실행:

```bash
make reference-payroll-rehearsal
```

결과는 기본적으로 아래에 생성됨.

```text
benchmarks/reference-payroll-rehearsal/
  rehearsal-summary.json
  latest-rehearsal-summary.json
  scenarios/
    single-company-1k.json
    single-company-10k.json
    single-company-100k.json
    hundred-companies-1k.json
```

`benchmarks/`는 git에 포함되지 않는 runtime artifact임.

## Legacy 기본 Scenario

script는 다음 네 가지 scenario를 실행함.

| scenario | 의미 |
| --- | --- |
| `single-company-1k` | 단일 회사가 월 1회 1천명에게 지급함 |
| `single-company-10k` | 단일 회사가 월 1회 1만명에게 지급함 |
| `single-company-100k` | 단일 회사가 월 1회 10만명에게 지급함 |
| `hundred-companies-1k` | 100개 회사가 각 1천명에게 지급해 총 10만건 peak를 만듦 |

legacy profile은 다음과 같음.

```text
BULK_CHUNK_SIZE=20
BULK_PROVER_UNITS=1
BULK_PROOFS_PER_SEC=6.92638
BULK_TX_PER_SEC=1
```

`BULK_CHUNK_SIZE=20`은 legacy phase 1 multi-message에서 독립 `MsgTransfer` 20개를 tx envelope 하나에 담는 기준임. 이 값이 커지면 legacy envelope 수는 줄지만 tx size/gas 한도 위험이 커질 수 있음. `BatchJoinSplit16x32` output capacity를 설정하는 값은 아님.

## Legacy Prover 수평확장 Profile

prover를 8개 unit으로 보는 rehearsal은 다음처럼 실행함.

```bash
BULK_PROVER_UNITS=8 make reference-payroll-rehearsal
```

tx submit이 병목인지 proof가 병목인지 확인하려면 `latest-rehearsal-summary.json`의 각 scenario에서 다음 값을 비교함.

- `proof_count`
- `tx_envelope_count`
- `estimated_total_seconds`
- `payroll_items_per_sec`

legacy phase 1 model에서는 recipient 1명당 proof 1개가 필요하므로 10만명 payroll에 proof 10만개가 필요함. prover unit을 늘리면 이 legacy proof 병목은 줄지만 tx envelope와 scanner/reconcile 운영량은 남음. 이 설명은 현재 one-proof batch graph를 나타내지 않음.

## 선택적 Legacy Multi-Message Localnet Smoke

simulation과 함께 작은 live localnet payroll flow도 실행하려면 다음처럼 실행함.

```bash
RUN_LOCALNET=1 LOCALNET_PAYROLL_ITEM_COUNT=2 make reference-payroll-rehearsal
```

이 옵션은 내부적으로 `scripts/reference-payroll-live-localnet.sh`를 실행함. localnet에서 treasury deposit, note scan, payroll plan/reservation, 실제 multi-message `transfer-batch`, recipient note scan, settle, final report export까지 legacy 경로를 확인함. 위의 현재 Session 3B one-proof gate와 독립된 경로임.

`LOCALNET_PAYROLL_ITEM_COUNT`를 높이면 localnet 실행 시간이 길어지고 proof/tx 비용이 커짐. 기본 rehearsal에서는 큰 수치 검증을 simulation으로 하고, chain path는 작은 smoke로 확인하는 것을 권장함. 1천건 이상 restart/retry rehearsal은 아래 seed mode를 사용함.

## Historical Legacy 1천건 Localnet Restart/Retry Rehearsal — 2026-07-08

다음 명령과 결과는 2026-07-08 repo-local legacy chain rehearsal에서 보존한 기록임. 명령은 regression에 계속 유용하지만 날짜가 있는 결과는 현재 one-proof capacity evidence가 아님.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k \
PAYROLL_SEED_NOTES=1 \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

2026-07-08 rehearsal은 다음을 확인함.

- 직원 1천명 기준 localnet-only seeded treasury note와 wallet cache를 준비함.
- `clairveil-payroll run`을 같은 plan으로 두 번 실행해도 중복 reservation 없이 idempotent하게 재개됨.
- 20건 단위 `transfer-batch` chunk 50개가 실제 localnet에 제출됨.
- 각 `transfer-batch`는 실제 Groth16 proof를 만들고 실제 chain tx로 포함됨.
- 각 chunk가 `settle-transfer-batch -item-start -item-limit`로 plan의 해당 구간만 settle함.
- 최종 `payroll-status-after-settle.json`에서 operation 1천건이 `Succeeded`이고 input reservation 2천개가 `ConfirmedSpent`임.

`GAS_PRICES=0uclair`는 1천건 localnet rehearsal이 genesis 계정 잔액보다 fee 소모가 커지는 것을 피하기 위한 local-only 설정임. staging/testnet에서는 실제 fee policy로 다시 실행해야 함.

`PAYROLL_SEED_NOTES=1`은 localnet genesis commitment와 Alice wallet cache에 payroll용 amount note와 zero dummy note를 미리 넣는 rehearsal helper임. 이 옵션은 2천개의 deposit tx 준비 시간을 제거하지만, 이후 payroll input 생성, reservation 확정, transfer proof 생성, tx broadcast, recipient scan, settle 경로는 실제로 실행함. 이 옵션은 production note preparation 방식이 아님.

성공 시 주요 산출물은 다음과 같음.

```text
tmp/reference-payroll-live-localnet-1k/out/rehearsal-summary.json
tmp/reference-payroll-live-localnet-1k/out/seed-localnet-notes.json
tmp/reference-payroll-live-localnet-1k/out/payroll-status-after-settle.json
tmp/reference-payroll-live-localnet-1k/out/payroll-final-report.json
tmp/reference-payroll-live-localnet-1k/out/payroll-confirmed-plan-retry.json
tmp/reference-payroll-live-localnet-1k/out/payroll-settle-report-001.json ... payroll-settle-report-050.json
```

2026-07-08 기준 성공한 1천건 seeded localnet run은 `confirmed_items=1000`, `succeeded_operations=1000`, `confirmed_spent_reservations=2000`, `chunk_count=50`을 기록했고 wall-clock은 약 8분 57초였음.

이 legacy 1천건 localnet run의 목적은 개발 중 restart/retry invariant와 durable control plane을 확인하는 것이었음. 1만건, 10만건, 여러 tenant 동시 peak는 이 legacy run을 외삽하지 말고 현재 one-proof staging/testnet evidence와 Session 3B-aware capacity report를 함께 남겨야 함.

2026-07-08 actual 1천건 localnet 성공 결과와 작은 multi-chunk smoke 성공 기록은 [clairveil-reference-payroll-localnet-rehearsal-result-kr.md](clairveil-reference-payroll-localnet-rehearsal-result-kr.md)에 남김.

## Legacy 결과 해석

legacy `latest-rehearsal-summary.json`은 다음 구조를 가짐.

```json
{
  "schema_version": "clairveil.reference_payroll_rehearsal.v1",
  "profile": {
    "chunk_size": 20,
    "prover_units": 1,
    "proofs_per_sec_per_unit": 6.92638,
    "tx_per_sec": 1
  },
  "scenarios": [
    {
      "name": "single-company-100k",
      "recipient_count": 100000,
      "proof_count": 100000,
      "tx_envelope_count": 5000,
      "estimated_total_seconds": 14437.5639,
      "estimated_total_hours": 4.0104
    }
  ]
}
```

`single-company-100k`와 `hundred-companies-1k`는 총 지급 건수는 같지만 운영 의미가 다름.

- `single-company-100k`는 한 tenant의 proof, note preparation, scanner 결과가 한 run에 몰림.
- `hundred-companies-1k`는 총량은 같아도 tenant별 scheduling, rate limit, retry window를 분산할 수 있음.

## 현재 One-Proof 해석과 남은 Scaling 판단

`BatchJoinSplit16x32`와 Session 3B payroll SDK는 이미 구현됐으며 obsolete phase label 아래의 미래 protocol 작업으로 설명하면 안 됨. Rehearsal evidence는 다음처럼 해석함.

- 현재 proof 수는 recipient당 하나가 아니라 atomic batch operation당 하나임. Operation 하나는 input 1..16개와 전체 output 1..32개를 가지며 change와 padding이 해당 operation의 payment output 수를 줄임.
- 이 문서의 legacy `proof_count`, `tx_envelope_count`, 완료 시간 추정값은 comparison 값이며 현재 one-proof capacity claim의 근거가 될 수 없음.
- 현재 capacity claim에는 실제 Session 3B shape 분포, prover latency/memory, tx gas/inclusion, typed scan, disclosure 검증, reconcile, retry, tenant scheduling을 staging/testnet load에서 측정한 결과가 필요함.
- 근거상 frozen 16/32 shape가 부족하면 새 circuit/protocol shape는 별도 roadmap, security review, circuit/keeper/SDK contract, migration plan이 필요함. 이 문서가 이미 암시한 미구현 이름으로 취급하면 안 됨.

## 관련 명령

현재 one-proof conformance/localnet gate는 다음 명령으로 실행함.

```bash
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

단일 legacy simulation만 직접 실행하려면 아래 명령을 사용함.

```bash
go run ./cmd/clairveil-bulktransferbench \
  -scenario single-company-100k \
  -recipients 100000 \
  -chunk-size 20 \
  -prover-units 8 \
  -proofs-per-sec 6.92638 \
  -tx-per-sec 1 \
  -out benchmarks/reference-payroll-rehearsal/scenarios/single-company-100k-prover8.json
```

legacy bulk readiness check를 함께 실행하려면 아래 명령을 사용함. 현재 one-proof live gate를 대체하지 않음.

```bash
make privacy-bulk-readiness-check
```
