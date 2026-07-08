# Clairveil 대량 전송 소요시간 시뮬레이션 노트

## 목적

이 문서는 [Clairveil 대량 전송 방안 검토 리포트](./clairveil-bulk-transfer-strategy-kr.md)를 보강하기 위한 소요시간 시뮬레이션 자료임.

목표는 현재 구조에서 10만건 대량 전송이 어느 정도 걸릴 수 있는지 추정하고, 문서의 4가지 방안을 도입했을 때 어떤 병목이 얼마나 줄어드는지 숫자로 비교하는 것임.

이 문서는 제품 성능 보증이나 공개 benchmark claim이 아님. 현재 repo에 남아 있는 smoke/reference 결과와 구조 기반 가정값을 조합한 planning simulation임.

## 결론 요약

현재 구조에서 10만명에게 직접 transfer를 보내면 `proof 10만개 + tx envelope 10만개`가 필요함. repo의 최신 smoke 결과 기준 transfer proof 처리량은 약 `6.9 proofs/sec` 수준이므로 proof 생성만 보면 약 `4.0시간`으로 추정됨. 그러나 tx envelope도 10만개이기 때문에 실제 대량 전송에서는 chain 제출/포함 처리량이 더 큰 병목이 될 가능성이 높음.

보수적으로 `1 tx/sec`를 대량 제출 기준으로 잡으면 현재 구조의 10만건 submit 완료 시간은 약 `27.8시간`임. `5 tx/sec`이면 약 `5.6시간`, `10 tx/sec`이면 proof 생성이 병목이 되어 약 `4.0시간`임.

방안 1은 운영 자동화 방안이므로 정상 경로의 proof 수와 tx 수를 줄이지 않음. 따라서 순수 처리시간 개선은 `1.0x`에 가까움. 대신 note 충돌, 재시도, 수동 운영 지연을 줄여 실제 운영 실패율을 낮추는 효과가 큼.

방안 2는 여러 `MsgTransfer`를 하나의 transaction에 묶으므로 tx envelope 수를 줄임. 예를 들어 chunk size를 `20`으로 잡으면 10만건의 tx envelope가 `5,000개`로 줄어듦. `1 tx/sec` 기준 총 소요시간은 약 `4.0시간`이 되어 현재 대비 약 `6.9x` 개선됨. 다만 proof는 여전히 10만개라서 이후 병목이 proof 생성으로 이동함.

방안 3은 N-output batch circuit으로 proof 수와 tx envelope 수를 함께 줄임. 예를 들어 `N=32`, batch proof 비용을 기존 transfer proof의 `12배`로 가정하면 10만건은 `3,125개 batch`가 되고 총 소요시간은 약 `1.5시간`임. `1 tx/sec` 기준 현재 대비 약 `18.5x`, `5 tx/sec` 기준 약 `3.7x` 개선됨.

방안 4는 회사가 10만건을 직접 push하지 않고 payroll root/escrow를 등록한 뒤 직원 또는 relayer가 claim하는 구조임. 회사의 피크 작업량은 `10만 tx`에서 `root 등록 tx 1개 또는 소수 tx`로 줄어듦. 다만 전체 직원이 모두 claim해야 하는 총 workload는 claim 방식에 따라 남아 있으므로, 완료 시간은 claim window와 relayer/직원 참여율에 의해 결정됨.

## 확인한 현재 상태

현재 transfer는 `JoinSplitCircuit` 기반의 2-input/2-output 구조임. 코드 기준 `NumInputs = 2`, `NumOutputs = 2`이고, transfer prepare 단계에서 output 0은 recipient note, output 1은 sender change note로 사용됨.

관련 코드 위치는 다음과 같음.

- `x/privacy/circuit/joinsplit.go`
- `x/privacy/client/sdk/transfer/prepare.go`
- `x/privacy/client/sdk/transfer/prove.go`
- `x/privacy/types/msg.go`
- `x/privacy/keeper/msg_server.go`

현재 `MsgTransfer`는 정확히 2개의 nullifier, 2개의 commitment, 2개의 ciphertext를 요구함. 따라서 현재 구조에서는 한 명에게 지급할 때마다 하나의 `MsgTransfer`와 하나의 JoinSplit proof가 필요함.

SDK의 `CosmosTxBroadcaster.GenerateOrBroadcast(msgs ...sdk.Msg)`는 여러 sdk message를 받을 수 있음. 따라서 방안 2의 1차 구현인 multi-message transaction은 protocol 변경 없이 기존 broadcast path를 확장하는 방식으로 접근 가능함.

## 사용한 관측값

repo 안에 공개 claim으로 승격 가능한 production benchmark는 아직 없음. 다만 `benchmarks/*/latest.md`에는 smoke/reference 성격의 최근 결과가 있음.

이 문서에서 참고한 관측값은 다음과 같음.

| 항목 | 값 | 출처 | 해석 |
| --- | ---: | --- | --- |
| `BenchmarkJoinSplitCircuitProve` mean | `147.866 ms` | `../benchmarks/privacy-circuits/latest.md` | native single proof smoke benchmark임 |
| `BenchmarkJoinSplitCircuitProve` ops/sec | `6.76 proofs/sec` | `../benchmarks/privacy-circuits/latest.md` | 단일 프로세스 native proving 단순 역수임 |
| `ProverLoadTransferOnlyC1` requests/sec | `6.92638 req/sec` | `../benchmarks/public-capacity/latest.md` | external prover load smoke 결과임 |
| `ProverLoadTransferOnlyC1` latency p95 | `152.855 ms` | `../benchmarks/public-capacity/latest.md` | transfer-only prover route smoke latency임 |
| `UserLatencyTransferWithDisclosureNativeWarm` proof p95 | `154.748 ms` | `../benchmarks/public-capacity/latest.md` | native warm user latency trace의 proof 구간임 |
| `LocalnetTPSMixedDepositTransferWithdraw` successful tx/sec | `0.134328 tx/sec` | `../benchmarks/public-capacity/latest.md` | mixed localnet smoke 결과이며 capacity sweep가 아님 |

중요한 해석은 다음과 같음.

- proof latency는 현재 transfer proof 하나가 대략 `150 ms`대임을 보여줌.
- prover load smoke는 transfer proof 처리량이 약 `6.9 proofs/sec` 수준임을 보여줌.
- localnet tx/sec `0.134328`은 deposit/transfer/withdraw mixed smoke flow 결과라서 chain capacity로 일반화하면 안 됨.
- 따라서 본문에서는 `6.92638 proofs/sec`를 proof 기준값으로 사용하고, tx envelope 처리량은 별도 변수로 둠.

## 시뮬레이션 모델

기본 단위는 recipient 1명에 대한 지급 1건임.

기호는 다음과 같음.

| 기호 | 의미 | 기본값 |
| --- | --- | ---: |
| `R` | recipient 수 | `100,000` |
| `Q` | 현재 transfer proof 처리량 | `6.92638 proofs/sec` |
| `E` | chain tx envelope 처리량 | `1`, `5`, `10 tx/sec` |
| `K` | 방안 2의 transaction당 transfer message 수 | `20` |
| `N` | 방안 3의 batch circuit recipient 수 | `32` |
| `A_N` | N-output batch proof 비용 배수 | `12x` |

계산식은 다음과 같음.

```text
현재 proof 시간 = R / Q
현재 tx 제출 시간 = R / E
현재 총 시간 = max(현재 proof 시간, 현재 tx 제출 시간)

방안 2 proof 시간 = R / Q
방안 2 tx 제출 시간 = ceil(R / K) / E
방안 2 총 시간 = max(방안 2 proof 시간, 방안 2 tx 제출 시간)

방안 3 batch 수 = ceil(R / N)
방안 3 proof 시간 = ceil(R / N) * A_N / Q
방안 3 tx 제출 시간 = ceil(R / N) / E
방안 3 총 시간 = max(방안 3 proof 시간, 방안 3 tx 제출 시간)
```

총 시간은 proof 생성과 tx 제출이 pipeline으로 겹칠 수 있다고 보고 `max()`로 계산함. 실제 운영에서는 queueing, block inclusion, retry, RPC timeout, sequence conflict, scanner/reconcile 시간이 추가됨.

## 현재 구조 소요시간

현재 구조는 10만명 지급에 대해 다음 작업량을 가짐.

| 항목 | 수량 |
| --- | ---: |
| recipient 수 | `100,000` |
| JoinSplit proof 수 | `100,000` |
| `MsgTransfer` 수 | `100,000` |
| tx envelope 수 | 일반적으로 `100,000` |
| output note 수 | `200,000` |
| nullifier 수 | `200,000` |

proof 생성 시간은 다음과 같음.

| 기준 | 계산 | 예상 시간 |
| --- | ---: | ---: |
| 현재 smoke prover `Q=6.92638 proofs/sec` | `100,000 / 6.92638` | 약 `4.0시간` |

tx envelope 처리량별 총 소요시간은 다음과 같음.

| tx envelope 처리량 `E` | proof 시간 | tx 제출 시간 | 예상 총 시간 | 주 병목 |
| ---: | ---: | ---: | ---: | --- |
| `0.134328 tx/sec` | 약 `4.0시간` | 약 `206.8시간` (`8.6일`) | 약 `8.6일` | localnet smoke tx rate |
| `1 tx/sec` | 약 `4.0시간` | 약 `27.8시간` | 약 `27.8시간` | tx 제출 |
| `5 tx/sec` | 약 `4.0시간` | 약 `5.6시간` | 약 `5.6시간` | tx 제출 |
| `10 tx/sec` | 약 `4.0시간` | 약 `2.8시간` | 약 `4.0시간` | proof 생성 |

`0.134328 tx/sec` 행은 localnet mixed smoke 결과를 단순 외삽한 값임. capacity 수치가 아니라 "현재 하네스의 smoke 속도를 그대로 늘리면 이 정도로 보일 수 있음"을 보여주는 참고값임.

실제 planning 기준으로는 `1 tx/sec`, `5 tx/sec`, `10 tx/sec` 행을 보는 편이 더 유용함.

## 방안별 개선 시뮬레이션

### 공통 기준

방안별 비교는 다음 기준으로 계산함.

- recipient 수는 `100,000`명임.
- 현재 transfer proof 처리량은 `6.92638 proofs/sec`임.
- 기본 tx envelope 처리량은 보수적으로 `1 tx/sec`임.
- 방안 2의 chunk size는 `K=20`임.
- 방안 3의 batch size는 `N=32`임.
- 방안 3의 batch proof 비용은 기존 transfer proof의 `12배`로 가정함.

### 요약 표

| 구조 | proof 수 | tx envelope 수 | proof 시간 | tx 제출 시간 (`1 tx/sec`) | 예상 총 시간 | 현재 대비 개선 |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 현재 구조 | `100,000` | `100,000` | 약 `4.0시간` | 약 `27.8시간` | 약 `27.8시간` | `1.0x` |
| 방안 1. Shielded Payroll Batch UX | `100,000` | `100,000` | 약 `4.0시간` | 약 `27.8시간` | 약 `27.8시간` | `1.0x` |
| 방안 2. Multi-Message Transaction | `100,000` | `5,000` | 약 `4.0시간` | 약 `1.4시간` | 약 `4.0시간` | 약 `6.9x` |
| 방안 3. N-Output Batch Circuit | `3,125` | `3,125` | 약 `1.5시간` | 약 `0.9시간` | 약 `1.5시간` | 약 `18.5x` |
| 방안 4. Payroll Merkle Distribution | 회사 측 `0~소수` | 회사 측 `1~소수` | 회사 측 거의 없음 | 초~분 단위 | claim window에 의존 | 회사 피크 기준 매우 큼 |

방안 4는 구조가 다르므로 위 표의 "현재 대비 개선"을 같은 방식으로 계산하기 어려움. 회사가 직접 10만건을 push하는 모델에서는 10만 tx가 필요하지만, Merkle distribution에서는 회사가 root/escrow 등록 tx만 제출함. 따라서 회사 측 peak 작업량은 대략 `100,000 tx -> 1 tx 또는 소수 tx`로 줄어듦. 반면 직원이 모두 수령을 완료하는 시간은 claim window와 claim 처리량에 따라 달라짐.

### 방안 1. Shielded Payroll Batch UX

방안 1은 새 protocol이나 새 circuit 없이 현재 transfer를 대량 지급 workflow로 자동화하는 방안임.

구조적 작업량은 현재와 동일함.

| 항목 | 현재 구조 | 방안 1 |
| --- | ---: | ---: |
| proof 수 | `100,000` | `100,000` |
| tx envelope 수 | `100,000` | `100,000` |
| 정상 경로 예상 총 시간 (`1 tx/sec`) | 약 `27.8시간` | 약 `27.8시간` |
| 순수 throughput 개선 | `1.0x` | `1.0x` |

방안 1이 의미가 없는 것은 아님. 이 방안의 효과는 throughput보다 운영 안정성에 있음.

개선되는 부분은 다음과 같음.

- CSV/HR 데이터 검증을 사전에 수행함.
- 지급 총액, denom, recipient 중복, treasury balance를 실행 전에 검증함.
- note inventory를 계획 단계에서 선택하고 reservation으로 잠금.
- proof job과 broadcast job을 queue로 분리함.
- 실패한 payment item을 추적하고 retry/replan함.
- operator가 10만건을 수동으로 실행하는 시간을 제거함.

따라서 방안 1의 수치상 개선은 `1.0x`에 가깝지만, 실제 운영에서는 note 충돌, 중복 지급, proof 생성 중단, tx retry 누락 같은 손실을 줄여 "완료 가능한 대량 지급"의 기반이 됨.

### 방안 2. Batch Transfer Message / Multi-Message Transaction

방안 2는 기존 transfer proof와 `MsgTransfer`를 유지하면서 여러 message를 하나의 transaction envelope에 묶는 방안임.

`K=20`으로 가정하면 작업량은 다음처럼 바뀜.

| 항목 | 현재 구조 | 방안 2 |
| --- | ---: | ---: |
| proof 수 | `100,000` | `100,000` |
| `MsgTransfer` 수 | `100,000` | `100,000` |
| tx envelope 수 | `100,000` | `5,000` |
| output note 수 | `200,000` | `200,000` |
| nullifier 수 | `200,000` | `200,000` |

tx envelope 처리량별 총 소요시간은 다음과 같음.

| tx envelope 처리량 `E` | 현재 총 시간 | 방안 2 총 시간 | 개선 배수 | 방안 2 병목 |
| ---: | ---: | ---: | ---: | --- |
| `0.134328 tx/sec` | 약 `8.6일` | 약 `10.3시간` | 약 `20.0x` | tx 제출 |
| `1 tx/sec` | 약 `27.8시간` | 약 `4.0시간` | 약 `6.9x` | proof 생성 |
| `5 tx/sec` | 약 `5.6시간` | 약 `4.0시간` | 약 `1.4x` | proof 생성 |
| `10 tx/sec` | 약 `4.0시간` | 약 `4.0시간` | 약 `1.0x` | proof 생성 |

방안 2가 좋아지는 이유는 tx envelope 수를 줄이기 때문임. 현재 구조는 chain에 10만개의 envelope를 제출해야 하지만, `K=20`이면 5천개만 제출하면 됨.

그러나 proof 수는 줄지 않음. 그래서 tx 병목이 줄어들수록 proof 생성이 새로운 병목이 됨. 위 표에서 `1 tx/sec`부터 방안 2의 총 시간은 proof 시간인 약 `4.0시간`으로 수렴함.

방안 2의 제약은 다음과 같음.

- 하나의 transaction에 너무 많은 `MsgTransfer`를 넣으면 tx size와 gas limit에 걸릴 수 있음.
- chunk size `K`는 실제 gas, block max bytes, mempool policy를 측정해서 정해야 함.
- batch 안의 어느 item이 실패할 때 전체 tx가 실패하는지, 부분 성공이 가능한지 정책이 필요함.
- 같은 chunk 안에서 동일 note/nullifier가 중복되면 안 됨.
- payroll plan과 note reservation 없이는 대량 chunk 생성 중 note 충돌 가능성이 있음.

### 방안 3. N-Output Batch Circuit

방안 3은 하나의 proof가 여러 recipient output을 생성하도록 새 batch circuit을 만드는 방안임.

`N=32`, `A_N=12`로 가정하면 작업량은 다음처럼 바뀜.

| 항목 | 현재 구조 | 방안 3 |
| --- | ---: | ---: |
| recipient 수 | `100,000` | `100,000` |
| proof 수 | `100,000` | `3,125` |
| tx envelope 수 | `100,000` | `3,125` |
| proof 1개 비용 | `1x` | `12x` 가정 |
| 총 proof 비용 | `100,000x` | `37,500x` |

tx envelope 처리량별 총 소요시간은 다음과 같음.

| tx envelope 처리량 `E` | 현재 총 시간 | 방안 3 총 시간 | 개선 배수 | 방안 3 병목 |
| ---: | ---: | ---: | ---: | --- |
| `0.134328 tx/sec` | 약 `8.6일` | 약 `6.5시간` | 약 `32.0x` | tx 제출 |
| `1 tx/sec` | 약 `27.8시간` | 약 `1.5시간` | 약 `18.5x` | proof 생성 |
| `5 tx/sec` | 약 `5.6시간` | 약 `1.5시간` | 약 `3.7x` | proof 생성 |
| `10 tx/sec` | 약 `4.0시간` | 약 `1.5시간` | 약 `2.7x` | proof 생성 |

좋아지는 이유는 proof 수와 tx envelope 수가 함께 줄어들기 때문임. `N=32`이면 10만명의 지급이 `ceil(100000 / 32) = 3,125`개 batch로 줄어듦.

batch proof가 기존 proof보다 12배 느리다고 가정해도, proof 개수가 32분의 1로 줄기 때문에 총 proof 비용은 약 `2.7배` 줄어듦. tx envelope는 32분의 1로 줄어듦.

batch proof 비용 배수에 대한 민감도는 다음과 같음.

| batch proof 비용 배수 `A_N` | batch proof 총 시간 | `1 tx/sec` 기준 총 시간 | 현재 대비 개선 |
| ---: | ---: | ---: | ---: |
| `8x` | 약 `1.0시간` | 약 `1.0시간` | 약 `27.7x` |
| `12x` | 약 `1.5시간` | 약 `1.5시간` | 약 `18.5x` |
| `20x` | 약 `2.5시간` | 약 `2.5시간` | 약 `11.1x` |

방안 3의 제약은 다음과 같음.

- 새 gnark circuit과 새 trusted setup artifact가 필요함.
- `N=8`, `N=16`, `N=32`, `N=64`처럼 size별 circuit을 둘지, 하나의 고정 size만 둘지 결정해야 함.
- batch 안의 recipient 수가 N보다 작을 때 padding과 dummy output 정책이 필요함.
- recipient별 disclosure payload와 ciphertext 배열을 message에 담아야 하므로 tx size가 커짐.
- batch proof 검증 gas와 tx max bytes를 반드시 측정해야 함.
- 실패 시 batch 전체를 replan해야 할 수 있으므로 reservation/retry 설계가 더 중요해짐.

### 방안 4. Payroll Merkle Distribution

방안 4는 회사가 모든 직원에게 직접 transfer를 push하지 않고, payroll root와 escrow를 등록한 뒤 직원 또는 relayer가 claim하게 만드는 구조임.

회사 측 작업량은 다음처럼 바뀜.

| 항목 | 현재 push transfer | 방안 4 |
| --- | ---: | ---: |
| 회사가 생성하는 recipient별 transfer proof | `100,000` | `0` 또는 소수 |
| 회사가 제출하는 tx envelope | `100,000` | `1` 또는 root shard 수 |
| 회사 피크 submit 시간 (`1 tx/sec`) | 약 `27.8시간` | 초~분 단위 |
| 직원 수령 완료 시간 | 회사 push 완료 시점에 가까움 | claim window에 의존 |

방안 4가 좋아지는 이유는 회사의 월말 피크를 없애기 때문임. 회사는 전체 payroll commitment를 root로 등록하고, 각 직원은 자신의 leaf를 증명해 claim함.

다만 전체 수령 완료 시간은 다음 중 어떤 제품 모델을 선택하느냐에 따라 달라짐.

- 직원이 직접 claim하면 회사 피크는 거의 사라지지만 직원별 claim 시점이 분산됨.
- relayer가 직원 claim을 대신 실행하면 UX는 좋아지지만 relayer fleet이 claim proof와 tx를 처리해야 함.
- claim도 batch 처리하지 않으면 전체 claim tx 수는 여전히 직원 수와 같음.

claim window별 필요한 평균 claim 처리량은 다음과 같음.

| 전체 직원 claim 완료 목표 | 필요한 평균 claim tx/sec |
| ---: | ---: |
| `24시간` | 약 `1.16 tx/sec` |
| `12시간` | 약 `2.31 tx/sec` |
| `8시간` | 약 `3.47 tx/sec` |
| `4시간` | 약 `6.94 tx/sec` |
| `1시간` | 약 `27.78 tx/sec` |

따라서 방안 4는 "회사가 10만명을 한 번에 밀어 넣는 문제"를 "직원 또는 relayer가 claim window 안에서 분산 처리하는 문제"로 바꾸는 방안임.

방안 4의 제약은 다음과 같음.

- claim circuit이 필요함.
- payroll leaf schema, Merkle depth, root lifecycle, expiration, clawback 정책이 필요함.
- 직원별 claim secret 또는 proof material 배포 UX가 필요함.
- claim을 하지 않는 직원에 대한 회수/재지급 정책이 필요함.
- 회사가 즉시 지급 완료를 보장해야 하는 push payroll 모델과는 제품 의미가 달라짐.
- claim proof와 tx를 relayer가 대신 처리한다면 relayer capacity planning이 별도로 필요함.

## prover 수평 확장 민감도

위 계산은 `Q=6.92638 proofs/sec`를 하나의 기준 prover 처리량으로 둔 단순 모델임. prover를 여러 개 운영하면 proof 병목은 줄어듦.

예를 들어 prover unit을 4개로 확장하고 각 unit이 같은 처리량을 낸다고 가정하면 proof 처리량은 약 `27.7 proofs/sec`가 됨.

`1 tx/sec` 기준 비교는 다음과 같음.

| 구조 | prover unit 1개 총 시간 | prover unit 4개 총 시간 | 해석 |
| --- | ---: | ---: | --- |
| 현재 구조 | 약 `27.8시간` | 약 `27.8시간` | tx 10만개가 병목이라 proof 확장 효과가 작음 |
| 방안 2 | 약 `4.0시간` | 약 `1.4시간` | tx envelope를 줄인 뒤 proof 확장이 효과적임 |
| 방안 3 | 약 `1.5시간` | 약 `0.9시간` | proof와 tx가 모두 줄어들어 1시간 내외까지 내려감 |

이 결과는 방안 2와 방안 3이 prover fleet 확장과 함께 적용될 때 효과가 더 커진다는 뜻임. 현재 구조는 tx envelope 수가 너무 많아서 prover만 늘려도 전체 시간이 크게 줄지 않음.

## 100개 기업 x 월 1천건 시나리오

총량은 `100개 기업 x 1,000명 = 100,000건`이므로 global capacity 관점에서는 10만명 단일 기업과 같은 규모임.

다만 tenant별 queue를 나누고 지급 시간을 분산할 수 있으므로 운영 난이도는 다르게 볼 수 있음.

회사 1개가 1천건을 처리할 때의 예상 시간은 다음과 같음.

| 구조 | proof 수 | tx envelope 수 | `1 tx/sec` 기준 회사 1개 소요시간 | `5 tx/sec` 기준 회사 1개 소요시간 |
| --- | ---: | ---: | ---: | ---: |
| 현재 구조 | `1,000` | `1,000` | 약 `16분 40초` | 약 `3분 20초` |
| 방안 1 | `1,000` | `1,000` | 약 `16분 40초` | 약 `3분 20초` |
| 방안 2 (`K=20`) | `1,000` | `50` | 약 `2분 24초` | 약 `2분 24초` |
| 방안 3 (`N=32`) | `32` | `32` | 약 `55초` | 약 `55초` |
| 방안 4 | 회사 측 `0~소수` | 회사 측 `1` | 초~분 단위 | 초~분 단위 |

100개 기업이 같은 시간대에 모두 실행되면 global 작업량은 다시 10만건과 같아짐.

| 구조 | global proof 수 | global tx envelope 수 | `1 tx/sec` 기준 global 소요시간 |
| --- | ---: | ---: | ---: |
| 현재 구조 | `100,000` | `100,000` | 약 `27.8시간` |
| 방안 2 (`K=20`) | `100,000` | `5,000` | 약 `4.0시간` |
| 방안 3 (`N=32`) | `3,200` 전후 | `3,200` 전후 | 약 `1.5시간` 전후 |
| 방안 4 | 회사 측 소수 | root 등록 `100개` 전후 | 회사 측 약 `1분 40초` at `1 tx/sec` |

100개 기업 모델에서 중요한 점은 tenant별 scheduling이 가능하다는 것임.

예를 들어 100개 기업을 10개 그룹으로 나눠 1시간 간격으로 실행하면, 현재 구조에서도 순간 피크는 줄일 수 있음. 그러나 총 tx envelope 수는 줄지 않으므로 전체 처리량 문제가 사라지는 것은 아님.

방안 2는 각 회사의 1천건을 50개 chunk로 줄이므로 SaaS 운영에 바로 도움이 됨. 방안 3은 회사 1개의 월별 payroll을 1분 내외로 줄일 수 있어 push payroll 제품에 강함. 방안 4는 회사별 root 등록만 먼저 처리하고 claim을 하루 이상 분산할 수 있어 multi-tenant SaaS 모델에 가장 자연스러움.

## 어떤 수치를 더 측정해야 하는가

이 문서의 숫자를 구현 계획이나 제품 SLA로 바꾸려면 다음 측정이 필요함.

1. `MsgTransfer` 단독 localnet open-loop throughput 측정이 필요함.
2. multi-message transaction의 chunk size별 gas, tx bytes, success rate 측정이 필요함.
3. `K=5`, `K=10`, `K=20`, `K=50` chunk별 block inclusion latency 측정이 필요함.
4. prover fleet을 1, 2, 4, 8개로 늘렸을 때 proof requests/sec와 latency p95/p99 측정이 필요함.
5. N-output batch circuit prototype의 `N=8`, `N=16`, `N=32`, `N=64`별 proving time, verification gas, proof artifact size 측정이 필요함.
6. claim circuit을 만들 경우 claim proof latency와 claim tx gas 측정이 필요함.
7. scanner/reconcile이 10만건 event와 nullifier를 처리하는 데 걸리는 시간이 필요함.
8. RPC timeout, mempool eviction, sequence conflict가 있는 상황에서 retry policy가 실제로 몇 퍼센트의 overhead를 만드는지 측정이 필요함.

특히 공개 또는 고객 제안용 수치로 쓰려면 smoke 결과가 아니라 `run_profile=public_claim` 또는 그에 준하는 장시간 saturation benchmark가 필요함.

## 해석

현재 구조에서 10만건 push payroll이 불가능하다고 단정할 수는 없음. proof 기준으로만 보면 repo의 smoke 값은 10만 proof를 약 4시간에 만들 수 있음을 시사함.

문제는 10만개의 tx envelope를 chain에 안정적으로 넣고, 각 tx의 성공/실패를 추적하며, 실패한 item을 재계획하는 운영 병목임. 따라서 현재 구조 그대로는 "가능 여부"보다 "월말 피크를 안정적으로 감당할 수 있는가"가 더 큰 질문임.

단기적으로는 방안 1과 방안 2가 가장 현실적임. 방안 1로 payroll control plane과 note reservation을 만들고, 방안 2로 tx envelope 수를 줄이면 `1 tx/sec` 기준 10만건이 약 `27.8시간`에서 약 `4.0시간`으로 내려갈 수 있음.

중장기적으로 push payroll을 유지하려면 방안 3이 가장 직접적인 성능 개선임. `N=32` batch circuit이 가정대로 동작하면 `1 tx/sec` 기준 약 `1.5시간`까지 줄어들 수 있음.

제품 모델을 claim 기반으로 바꿀 수 있다면 방안 4가 가장 큰 구조적 완화임. 회사의 월말 피크는 root 등록 수준으로 줄고, 직원 수령은 claim window 안에서 분산됨. 다만 "회사가 모든 직원에게 즉시 지급 완료"라는 의미와는 다르므로 제품 요구사항을 먼저 정해야 함.
