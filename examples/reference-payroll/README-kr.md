# Reference Payroll Demo

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

## 흐름

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

현재 `clairveil-payrolld`는 `simulated` mode만 지원함. 실제 proof 생성과 chain broadcast를 하지 않고, durable reservation state 위에서 proof ready, submitted, reconcile 결과를 시뮬레이션함.

## 출력

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
