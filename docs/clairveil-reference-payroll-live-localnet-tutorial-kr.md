# Clairveil Reference Payroll Live Localnet 튜토리얼

English version: [clairveil-reference-payroll-live-localnet-tutorial.md](clairveil-reference-payroll-live-localnet-tutorial.md)

이 문서는 payroll reference product를 실제 localnet 위에서 끝까지 실행하는 방법을 설명함.

이 튜토리얼은 simulated `clairveil-payrolld` 흐름이 아니라 실제 chain tx를 사용함.

```text
localnet 기동
-> treasury shielded note deposit
-> list-notes scan
-> payroll input 생성
-> validate / prepare-notes / plan / run
-> run 재실행으로 idempotency 확인
-> 실제 transfer-batch broadcast
-> recipient note scan
-> transfer-batch 결과로 payroll state settle
-> final payroll report export
```

## 빠른 실행

repo root에서 아래 명령을 실행함.

```bash
make reference-payroll-live-localnet
```

성공하면 아래와 같은 메시지가 출력됨.

```text
Reference payroll live localnet tutorial passed.

Work dir:              tmp/reference-payroll-live-localnet
Payroll input:         tmp/reference-payroll-live-localnet/out/payroll-input.json
Payroll plan:          tmp/reference-payroll-live-localnet/out/payroll-plan.json
Reservation state:     tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json
Confirmed retry plan:  tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan-retry.json
Transfer batch chunks: 1
Rehearsal summary:    tmp/reference-payroll-live-localnet/out/rehearsal-summary.json
Final status:          tmp/reference-payroll-live-localnet/out/payroll-status-after-settle.json
Final payroll report:  tmp/reference-payroll-live-localnet/out/payroll-final-report.json
```

기본값은 직원 2명에게 각각 `1uclair`를 지급함.

## 옵션

```bash
PAYROLL_ITEM_COUNT=3 PAYROLL_ITEM_AMOUNT=2 PAYROLL_CHUNK_SIZE=2 make reference-payroll-live-localnet
```

주요 환경변수는 다음과 같음.

| env | 의미 |
| --- | --- |
| `CLAIRVEIL_PAYROLL_LIVE_WORK_DIR` | 튜토리얼 출력 디렉토리. 기본값은 `tmp/reference-payroll-live-localnet` |
| `PAYROLL_ITEM_COUNT` | payroll item 수. 기본값은 `2` |
| `PAYROLL_ITEM_AMOUNT` | item별 지급 금액. 기본값은 `1` |
| `PAYROLL_CHUNK_SIZE` | transfer-batch tx 하나에 담을 payroll item 수. 기본값은 전체 item 수 |
| `PAYROLL_SEED_NOTES` | `1`이면 localnet genesis와 Alice wallet cache에 payroll용 note를 seed함. 기본값은 `0` |
| `PAYROLL_TRANSFER_BATCH_GAS` | transfer-batch gas limit. 기본값은 chunk size 기반 자동 계산 |
| `GAS_PRICES` | localnet tx gas price. 기본값은 `8500000000uclair` |
| `RPC_PORT`, `P2P_PORT`, `GRPC_PORT`, `API_PORT` | localnet port 충돌 회피 |
| `CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, `PAYROLL_BIN` | 이미 빌드한 binary 사용 |

## 생성되는 파일

기본 출력은 `tmp/reference-payroll-live-localnet/out/` 아래에 생성됨.

| 파일 | 의미 |
| --- | --- |
| `bob-shielded-address.txt` | payroll recipient shielded address |
| `bob-notes-before.json` | 전체 실행 전 recipient note scan |
| `alice-notes.json` | treasury 역할의 Alice note scan |
| `seed-localnet-notes.json` | `PAYROLL_SEED_NOTES=1`일 때 생성되는 localnet-only seeded note report |
| `payroll-template.json` | 직원 목록과 지급액만 가진 payroll template |
| `payroll-input.json` | `alice-notes.json`에서 treasury note를 채운 payroll input |
| `payroll-validation.json` | payroll input validation 결과 |
| `payroll-note-preparation.json` | note preparation 분석 결과 |
| `payroll-plan.json` | draft payroll plan |
| `payroll-confirmed-plan.json` | durable state에 reservation 확정된 plan |
| `payroll-confirmed-plan-retry.json` | 같은 plan 재실행 idempotency 확인 결과 |
| `payroll-reservation-state.json` | reservation/operation durable state |
| `payroll-transfer-batch-001.json` | 첫 번째 실제 `transfer-batch` tx 결과 |
| `payroll-transfer-batch-001-query.json` | 첫 번째 chain tx query 결과 |
| `bob-notes-before-chunk-001.json` | 첫 번째 chunk 전 recipient note scan |
| `bob-notes-after-chunk-001.json` | 첫 번째 chunk 후 recipient note scan |
| `payroll-settle-report-001.json` | 첫 번째 chunk의 actual tx와 recipient note delta 기반 settle 결과 |
| `bob-notes-after.json` | 모든 chunk 이후 recipient note scan |
| `payroll-status-after-settle.json` | settle 후 reservation/operation status |
| `payroll-final-report.json` | payroll 최종 report |
| `rehearsal-summary.json` | item 수, chunk 수, 최종 성공 count 요약 |

## 단계별 명령 흐름

`make reference-payroll-live-localnet`는 내부적으로 아래 흐름을 실행함.

### 1. Localnet과 key 준비

스크립트는 임시 home을 만들고 `clairveild`, `clairveil-setup`, `clairveil-payroll`을 빌드함.

```bash
go build -o tmp/reference-payroll-live-localnet/clairveild-payroll-live ./cmd/clairveild
go build -o tmp/reference-payroll-live-localnet/clairveil-setup-payroll-live ./cmd/clairveil-setup
go build -o tmp/reference-payroll-live-localnet/clairveil-payroll-live ./cmd/clairveil-payroll
```

그 다음 `alice`, `bob`, `auditor` key를 만들고 genesis audit key를 설정한 뒤 localnet을 시작함.

### 2. Recipient shielded address 확인

```bash
clairveild tx privacy show-address \
  --from bob \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --output json
```

이 주소가 payroll item의 `recipient_address`로 사용됨.

### 3. Treasury note 준비

기본값인 `PAYROLL_SEED_NOTES=0`에서는 직원 1명당 현재 2-input transfer circuit에 맞춰 지급 amount note 1개와 zero dummy note 1개를 실제 deposit tx로 준비함.

```bash
clairveild tx privacy deposit 1uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 2500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

```bash
clairveild tx privacy deposit 0uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 2500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

1천건 이상 restart/retry rehearsal처럼 deposit 준비 시간이 너무 큰 경우에는 localnet-only seed mode를 사용할 수 있음.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k-seeded \
PAYROLL_SEED_NOTES=1 \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

seed mode는 `clairveil-payroll seed-localnet-notes`를 사용해 localnet genesis commitment와 Alice wallet cache에 amount note와 zero dummy note를 직접 기록함. 이렇게 하면 2천개의 deposit tx 준비를 생략할 수 있음.

```bash
clairveil-payroll seed-localnet-notes \
  -genesis tmp/reference-payroll-live-localnet/home/config/genesis.json \
  -wallet-home tmp/reference-payroll-live-localnet/home \
  -owner-address "$(cat tmp/reference-payroll-live-localnet/out/alice-address.txt)" \
  -shielded-address "$(cat tmp/reference-payroll-live-localnet/out/alice-shielded-address.txt)" \
  -count 1000 \
  -amount 1 \
  -denom uclair \
  -notes-out tmp/reference-payroll-live-localnet/out/alice-notes.json \
  -out tmp/reference-payroll-live-localnet/out/seed-localnet-notes.json
```

이 helper는 localnet rehearsal 준비용임. Production note preparation, staging/testnet deposit, 운영 approval 기반 treasury note 준비를 대체하지 않음. Seed mode에서도 이후 payroll input 생성, reservation 확정, 실제 Groth16 proof 생성, 실제 `transfer-batch` broadcast, recipient scan, settle, final report export는 그대로 실행됨.

### 4. Treasury note scan

```bash
clairveild tx privacy list-notes \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet \
  --json > tmp/reference-payroll-live-localnet/out/alice-notes.json
```

### 5. Payroll input 생성

직원 목록 template에 scan된 treasury note를 채움.

```bash
clairveil-payroll build-input-from-notes \
  -template tmp/reference-payroll-live-localnet/out/payroll-template.json \
  -notes tmp/reference-payroll-live-localnet/out/alice-notes.json \
  -owner-key-id alice \
  -lookup-key-id localnet-scan \
  -out tmp/reference-payroll-live-localnet/out/payroll-input.json
```

### 6. Payroll plan과 reservation 확정

```bash
clairveil-payroll validate \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-validation.json
```

```bash
clairveil-payroll prepare-notes \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-note-preparation.json
```

```bash
clairveil-payroll plan \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-plan.json
```

```bash
clairveil-payroll run \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan.json
```

같은 plan을 한 번 더 실행해도 중복 reservation을 만들지 않고 기존 state를 재사용해야 함.

```bash
clairveil-payroll run \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan-retry.json
```

### 7. 실제 transfer-batch broadcast

chunk size가 2이고 item이 2개이면 chunk 1개가 만들어짐. item 수가 더 많으면 chunk label이 `001`, `002`, `003`처럼 증가함.

```bash
clairveild tx privacy transfer-batch "$(cat tmp/reference-payroll-live-localnet/out/bob-shielded-address.txt)" \
  1uclair 1uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 21000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --rescan-wallet \
  --output json > tmp/reference-payroll-live-localnet/out/payroll-transfer-batch-001.json
```

이 단계는 실제 Groth16 proof를 만들고 실제 localnet에 tx를 broadcast함.

### 8. Recipient note scan

```bash
clairveild tx privacy list-notes \
  --from bob \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet \
  --json > tmp/reference-payroll-live-localnet/out/bob-notes-after-chunk-001.json
```

### 9. Payroll state settle

```bash
clairveil-payroll settle-transfer-batch \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -tx tmp/reference-payroll-live-localnet/out/payroll-transfer-batch-001.json \
  -recipient-before tmp/reference-payroll-live-localnet/out/bob-notes-before-chunk-001.json \
  -recipient-after tmp/reference-payroll-live-localnet/out/bob-notes-after-chunk-001.json \
  -item-start 0 \
  -item-limit 2 \
  -out tmp/reference-payroll-live-localnet/out/payroll-settle-report-001.json
```

`settle-transfer-batch`는 다음을 확인함.

- tx `code`가 `0`임.
- tx `message_count`가 선택된 payroll item 수와 같음.
- tx amount 목록이 선택된 payroll item amount 목록과 같음.
- recipient scan 결과에서 지급 amount note가 선택된 payroll item 수만큼 증가함.
- `-item-start`와 `-item-limit`으로 plan의 어느 구간을 해당 tx와 매칭할지 지정함.

확인이 끝나면 durable reservation state를 `ConfirmedSpent`, operation state를 `Succeeded`로 갱신함.

### 10. Final report 확인

```bash
clairveil-payroll status \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-status-after-settle.json
```

```bash
clairveil-payroll export-report \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-final-report.json
```

성공 기준은 다음과 같음.

```json
{
  "reservations_by_status": {
    "ConfirmedSpent": 4
  },
  "operations_by_status": {
    "Succeeded": 2
  }
}
```

최종 report는 아래처럼 나와야 함.

```json
{
  "status": "Confirmed",
  "summary": {
    "TotalItems": 2,
    "ConfirmedItems": 2
  }
}
```

## 현재 한계

이 튜토리얼은 실제 chain tx와 recipient note scan을 사용함. 다만 아직 full production scanner는 아님.

현재 `settle-transfer-batch`의 성공 판정은 다음 증거를 사용함.

- 실제 `transfer-batch` tx가 성공함.
- tx message 수와 amount 목록이 payroll plan과 일치함.
- recipient의 spendable note가 지급 amount별로 필요한 수만큼 증가함.

아직 남은 production-grade 보강은 다음과 같음.

- tx event/nullifier scanner는 `clairveil-payroll scan-evidence`와 SDK `EvidenceScanner`로 제공되지만, 이 튜토리얼 스크립트는 beginner-friendly settle bridge를 기본으로 사용함.
- production daemon은 `scan-evidence` 또는 동등한 scanner를 long-running worker로 연결해야 함.
- 같은 amount가 많은 payroll에서 recipient note delta와 operation item을 더 강하게 매칭함.
- `PAYROLL_SEED_NOTES=1`은 localnet-only rehearsal helper이므로 production note preparation 성능 주장의 근거로 쓰면 안 됨.
- staging/testnet에서는 같은 runbook으로 restart/retry와 scanner evidence artifact를 release 산출물로 남겨야 함.
