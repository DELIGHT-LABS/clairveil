# Clairveil Reference Payroll 대규모 Rehearsal 가이드

## 목적

이 문서는 payroll 대량전송을 production에 올리기 전에 repo 안에서 반복 실행할 수 있는 rehearsal 절차를 정의함.

이 rehearsal은 실제 10만건의 chain tx를 모두 localnet에 제출하는 테스트가 아님. 현재 1차 구조의 proof 수, tx envelope 수, chunk 수, 예상 완료 시간을 동일한 입력 profile로 반복 산출하고, 필요하면 작은 live localnet smoke를 함께 실행해 실제 chain 경로가 깨지지 않았는지 확인하는 절차임.

## 실행 명령

기본 실행:

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

## 기본 Scenario

script는 다음 네 가지 scenario를 실행함.

| scenario | 의미 |
| --- | --- |
| `single-company-1k` | 단일 회사가 월 1회 1천명에게 지급함 |
| `single-company-10k` | 단일 회사가 월 1회 1만명에게 지급함 |
| `single-company-100k` | 단일 회사가 월 1회 10만명에게 지급함 |
| `hundred-companies-1k` | 100개 회사가 각 1천명에게 지급해 총 10만건 peak를 만듦 |

기본 profile은 다음과 같음.

```text
BULK_CHUNK_SIZE=20
BULK_PROVER_UNITS=1
BULK_PROOFS_PER_SEC=6.92638
BULK_TX_PER_SEC=1
```

`BULK_CHUNK_SIZE=20`은 현재 phase 1 multi-message transaction에서 tx 하나에 `MsgTransfer` 20개를 담는 기준임. 이 값이 커지면 tx envelope 수는 줄지만 tx size/gas 한도 위험이 커질 수 있음.

## Prover 수평확장 Profile

prover를 8개 unit으로 보는 rehearsal은 다음처럼 실행함.

```bash
BULK_PROVER_UNITS=8 make reference-payroll-rehearsal
```

tx submit이 병목인지 proof가 병목인지 확인하려면 `latest-rehearsal-summary.json`의 각 scenario에서 다음 값을 비교함.

- `proof_count`
- `tx_envelope_count`
- `estimated_total_seconds`
- `payroll_items_per_sec`

phase 1에서는 recipient 1명당 proof 1개가 필요하므로 10만명 payroll은 proof 10만개를 유지함. prover unit을 늘리면 proof 병목은 줄지만, tx envelope와 scanner/reconcile 운영량은 남음.

## 선택적 Live Localnet Smoke

simulation과 함께 작은 live localnet payroll flow도 실행하려면 다음처럼 실행함.

```bash
RUN_LOCALNET=1 LOCALNET_PAYROLL_ITEM_COUNT=2 make reference-payroll-rehearsal
```

이 옵션은 내부적으로 `scripts/reference-payroll-live-localnet.sh`를 실행함. localnet에서 treasury deposit, note scan, payroll plan/reservation, 실제 `transfer-batch`, recipient note scan, settle, final report export까지 확인함.

`LOCALNET_PAYROLL_ITEM_COUNT`를 높이면 localnet 실행 시간이 길어지고 proof/tx 비용이 커짐. 기본 rehearsal에서는 큰 수치 검증을 simulation으로 하고, chain path는 작은 smoke로 확인하는 것을 권장함.

## 1천건 Localnet Restart/Retry Rehearsal

repo-local chain path에서 1천건 restart/retry 동작을 확인하려면 아래 명령을 사용함.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

이 rehearsal은 다음을 확인함.

- 직원 1천명 기준 treasury note deposit과 scan이 가능함.
- `clairveil-payroll run`을 같은 plan으로 두 번 실행해도 중복 reservation 없이 idempotent하게 재개됨.
- 20건 단위 `transfer-batch` chunk 50개가 실제 localnet에 제출됨.
- 각 chunk가 `settle-transfer-batch -item-start -item-limit`로 plan의 해당 구간만 settle함.
- 최종 `payroll-status-after-settle.json`에서 operation 1천건이 `Succeeded`이고 input reservation 2천개가 `ConfirmedSpent`임.

`GAS_PRICES=0uclair`는 1천건 localnet rehearsal이 genesis 계정 잔액보다 fee 소모가 커지는 것을 피하기 위한 local-only 설정임. staging/testnet에서는 실제 fee policy로 다시 실행해야 함.

성공 시 주요 산출물은 다음과 같음.

```text
tmp/reference-payroll-live-localnet-1k/out/rehearsal-summary.json
tmp/reference-payroll-live-localnet-1k/out/payroll-status-after-settle.json
tmp/reference-payroll-live-localnet-1k/out/payroll-final-report.json
tmp/reference-payroll-live-localnet-1k/out/payroll-confirmed-plan-retry.json
tmp/reference-payroll-live-localnet-1k/out/payroll-settle-report-001.json ... payroll-settle-report-050.json
```

1천건 localnet run은 개발 중 restart/retry invariant를 확인하는 목적임. 1만건, 10만건, 여러 tenant 동시 peak는 localnet full tx 제출보다 staging/testnet rehearsal과 simulation report를 함께 남기는 방식을 권장함.

## 결과 해석

`latest-rehearsal-summary.json`은 다음 구조를 가짐.

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

## 2차 진입 판단

다음 중 하나가 확인되면 2차 `BatchJoinSplit32`를 검토함.

- prover 수평확장을 적용해도 proof 10만개 운영 비용이 큼
- tx envelope 5천개 수준도 월말 peak에서 부담임
- scanner/reconcile이 제출량을 따라가지 못함
- 100개 회사 x 1천명 model에서 tenant scheduling만으로 peak를 충분히 완화하지 못함
- push payroll UX를 유지해야 해서 Merkle claim model로 바꾸기 어려움

## 관련 명령

단일 simulation만 직접 실행하려면 아래 명령을 사용함.

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

전체 readiness check와 함께 보고 싶으면 아래 명령을 사용함.

```bash
make privacy-bulk-readiness-check
```
