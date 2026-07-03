# Clairveil 대량 전송에서 1/2번 방안과 Prover 수평확장만 적용하는 대안 검토

## 목적

이 문서는 10만건 대량 전송 또는 1천건 전송 기업 100개를 지원할 때, 방안 3인 N-output batch circuit과 방안 4인 Payroll Merkle Distribution을 당장 도입하지 않고 방안 1, 2와 prover 수평확장만으로 실용적인 대응이 가능한지 검토하는 메모임.

관련 수치의 상세 시뮬레이션은 [Clairveil 대량 전송 소요시간 시뮬레이션 노트](./clairveil-bulk-transfer-time-simulation-kr.md)를 참고함.

## 전제

검토 대상은 다음 조합임.

- 방안 1. Shielded Payroll Batch UX
- 방안 2. Batch Transfer Message / Multi-Message Transaction
- prover fleet 수평확장

이번 검토에서는 다음 방안은 제외함.

- 방안 3. N-Output Batch Circuit
- 방안 4. Payroll Merkle Distribution

따라서 protocol 관점의 기본 구조는 현재 transfer를 유지함. recipient 1명당 JoinSplit proof 1개와 `MsgTransfer` 1개가 필요함.

## 핵심 판단

방안 1, 2와 prover 수평확장만으로도 단기 실용 대안은 될 수 있음.

다만 이 접근은 proof 수를 줄이는 방식이 아니라 proof를 더 많이 병렬 생성하는 방식임. 따라서 처리량은 늘릴 수 있지만, 10만건 자체가 만드는 총 proof 수와 총 transfer item 수는 그대로 남음.

실용적인 범위는 다음과 같음.

| 목표 | 판단 |
| --- | --- |
| 100개 기업이 각 1천건을 몇 시간 안에 처리 | 실용 가능성이 큼 |
| 단일 기업 10만건을 반나절 안에 처리 | 실용 가능성이 있음 |
| 단일 기업 10만건을 1~2시간 안에 처리 | tx 처리량과 chunk size가 받쳐주면 가능성이 있음 |
| 단일 기업 10만건을 수십 분 안에 안정 처리 | 3번 또는 4번 없이 어려울 가능성이 큼 |
| 더 큰 고객 또는 더 많은 tenant까지 장기 확장 | 3번 또는 4번 필요성이 커짐 |

## 왜 가능한가

방안 1은 대량 지급을 운영 가능한 workflow로 만드는 역할임. payroll plan, note reservation, proof job queue, broadcast job queue, retry/reconcile, 결과 리포트를 제공함.

방안 2는 tx envelope 수를 줄이는 역할임. 기존 transfer를 1건씩 chain에 넣으면 10만건 지급에는 tx envelope 10만개가 필요함. 그러나 transaction 하나에 `MsgTransfer`를 20개씩 묶으면 tx envelope 수는 5천개로 줄어듦.

prover 수평확장은 proof 병목을 줄이는 역할임. 현재 구조에서는 10만건 지급에 proof 10만개가 필요하므로, prover를 여러 unit으로 늘리면 proof 생성 시간을 거의 선형에 가깝게 줄일 수 있음.

즉 이 조합은 다음 두 병목을 동시에 완화함.

| 병목 | 완화 방법 |
| --- | --- |
| proof 10만개 생성 | prover fleet 수평확장 |
| tx envelope 10만개 제출 | multi-message transaction으로 envelope 수 감소 |

## 10만건 단일 기업 기준 예상

아래 계산은 다음 가정을 사용함.

- 현재 transfer proof 처리량은 prover unit 1개당 약 `6.92638 proofs/sec`로 둠.
- 방안 2의 chunk size는 `K=20`으로 둠.
- 10만건의 tx envelope 수는 `100,000 / 20 = 5,000개`로 둠.
- proof 생성과 tx 제출은 pipeline으로 겹칠 수 있다고 보고 더 오래 걸리는 쪽을 총 시간으로 봄.
- 실제 운영에서는 retry, scanner, block inclusion, RPC timeout overhead가 추가될 수 있음.

| prover unit | proof 처리량 | proof 10만개 생성 시간 | `1 tx/sec` 총 시간 | `5 tx/sec` 총 시간 | `10 tx/sec` 총 시간 |
| ---: | ---: | ---: | ---: | ---: | ---: |
| `1` | 약 `6.9 proofs/sec` | 약 `4시간 1분` | 약 `4시간 1분` | 약 `4시간 1분` | 약 `4시간 1분` |
| `2` | 약 `13.9 proofs/sec` | 약 `2시간 0분` | 약 `2시간 0분` | 약 `2시간 0분` | 약 `2시간 0분` |
| `4` | 약 `27.7 proofs/sec` | 약 `1시간 0분` | 약 `1시간 23분` | 약 `1시간 0분` | 약 `1시간 0분` |
| `8` | 약 `55.4 proofs/sec` | 약 `30분` | 약 `1시간 23분` | 약 `30분` | 약 `30분` |
| `16` | 약 `110.8 proofs/sec` | 약 `15분` | 약 `1시간 23분` | 약 `16분 40초` | 약 `15분` |
| `32` | 약 `221.6 proofs/sec` | 약 `7분 30초` | 약 `1시간 23분` | 약 `16분 40초` | 약 `8분 20초` |

이 표에서 중요한 점은 prover를 늘릴수록 어느 순간 tx 제출이 병목으로 바뀐다는 것임.

`K=20`일 때 tx envelope는 5천개임. 따라서 tx 처리량별 하한은 다음과 같음.

| tx envelope 처리량 | 5천개 제출 시간 |
| ---: | ---: |
| `1 tx/sec` | 약 `1시간 23분` |
| `5 tx/sec` | 약 `16분 40초` |
| `10 tx/sec` | 약 `8분 20초` |

따라서 `1 tx/sec` 환경에서는 prover를 8개 이상으로 늘려도 전체 시간은 약 `1시간 23분` 아래로 내려가기 어려움. 반면 chain이 `5 tx/sec` 이상을 안정적으로 받아주면 prover 8~16개 구성에서 30분 이하도 계산상 가능함.

## 100개 기업 x 1천건 기준 예상

회사 1개의 월간 지급이 1천건이면 방안 2의 `K=20` 기준 tx envelope는 회사당 50개임.

| prover unit | 회사 1개 proof 생성 시간 | 회사 1개 tx 제출 시간 at `1 tx/sec` | 회사 1개 예상 총 시간 |
| ---: | ---: | ---: | ---: |
| `1` | 약 `2분 24초` | 약 `50초` | 약 `2분 24초` |
| `2` | 약 `1분 12초` | 약 `50초` | 약 `1분 12초` |
| `4` | 약 `36초` | 약 `50초` | 약 `50초` |
| `8` | 약 `18초` | 약 `50초` | 약 `50초` |

100개 기업이 완전히 같은 순간에 모두 실행되면 global 총량은 다시 10만건과 같음. 그러나 100개 기업 모델은 단일 10만건 기업보다 운영적으로 다루기 쉬움.

이유는 다음과 같음.

- tenant별 queue를 나눌 수 있음.
- 회사별 실행 시간을 분산할 수 있음.
- 실패한 회사 또는 batch만 재시도할 수 있음.
- blast radius가 회사 단위로 제한됨.
- 회사별 rate limit과 priority scheduling을 적용할 수 있음.

따라서 100개 기업 x 1천건 모델은 방안 1, 2와 prover 수평확장만으로도 꽤 현실적인 운영 모델이 될 수 있음.

## 실용적이라고 볼 수 있는 조건

이 접근이 실용 대안이 되려면 다음 조건을 만족해야 함.

1. multi-message transaction의 chunk size가 실제 chain gas, tx size, block limit 안에서 검증되어야 함.
2. `K=20`이 안 되면 `K=10`, `K=5` 기준으로 다시 산정해야 함.
3. prover worker를 늘렸을 때 proof throughput이 실제로 선형에 가깝게 증가해야 함.
4. proof job queue와 worker lease, heartbeat, retry 정책이 필요함.
5. note reservation이 plan 단계에서 적용되어야 함.
6. broadcast retry가 operation id 기준으로 idempotent해야 함.
7. sequence conflict와 RPC timeout을 처리하는 broadcaster가 필요함.
8. scanner/reconcile이 10만건 결과를 따라갈 수 있어야 함.
9. tenant별 rate limit과 execution window를 운영자가 조정할 수 있어야 함.
10. 실패한 batch를 안전하게 replan할 수 있어야 함.

특히 note reservation은 선택 기능이 아니라 필수 조건임. proof를 병렬로 많이 만들수록 같은 note를 중복 선택하거나, 준비한 note가 다른 transfer에 먼저 소비되는 문제가 더 자주 발생할 수 있음.

## 한계

이 접근은 3번 방안처럼 proof 수 자체를 줄이지 않음. 따라서 prover 비용은 지급 건수에 비례해서 계속 증가함.

또한 4번 방안처럼 회사의 월말 피크를 claim window로 분산하지 않음. 회사가 직접 push하는 모델을 유지하므로, 특정 시각에 대량 지급이 몰리면 tx 제출과 scanner/reconcile 부하가 여전히 집중됨.

prover를 많이 늘려도 tx envelope 수가 5천개 아래로 줄어들지 않으면 tx 제출 시간이 하한이 됨. `K=20`, `1 tx/sec` 기준으로는 약 `1시간 23분`이 하한임.

또한 multi-message transaction은 개별 transfer를 묶는 방식이므로, batch 안의 하나가 실패했을 때 전체 tx 실패가 될 수 있음. 이 경우 실패 단위가 커지고 retry/replan 정책이 중요해짐.

## 추천 접근

단기 로드맵으로는 방안 1, 2와 prover 수평확장을 먼저 추진하는 것이 합리적임.

추천 순서는 다음과 같음.

1. 방안 1로 payroll control plane과 note reservation을 구현함.
2. 방안 2로 multi-message transaction을 구현하고 `K=5`, `K=10`, `K=20`을 benchmark함.
3. prover worker pool을 만들고 1, 2, 4, 8, 16 unit에서 throughput을 측정함.
4. 10만건 synthetic payroll과 100개 기업 x 1천건 synthetic payroll을 각각 실행해 병목을 확인함.
5. 병목이 proof이면 prover fleet과 job scheduler를 개선함.
6. 병목이 tx/gas/block size이면 방안 3을 재검토함.
7. 병목이 월말 피크와 사용자 수령 UX이면 방안 4를 재검토함.

이 접근의 장점은 현재 protocol을 크게 바꾸지 않고 실제 운영 데이터를 확보할 수 있다는 점임. 방안 3과 4는 효과가 크지만 구현 비용도 크므로, 먼저 1/2번과 prover 수평확장으로 실제 한계를 측정한 뒤 장기 protocol 방향을 선택하는 편이 안전함.

## 결론

방안 1, 2와 prover 수평확장만으로도 단기적인 실용 대안은 될 수 있음.

특히 100개 기업 x 1천건 모델에는 적합성이 높음. tenant별로 실행을 나누고, 회사별 batch를 독립적으로 retry할 수 있기 때문임.

단일 기업 10만건도 몇 시간 내 완료 목표라면 현실적인 가능성이 있음. `K=20`, prover 4~8개, chain 처리량 `1~5 tx/sec` 수준이 확보되면 대략 30분~1시간 30분대 planning이 가능함.

다만 수십 분 이내의 안정적인 대량 push payroll을 장기 목표로 삼는다면 3번 N-output batch circuit이 필요해질 가능성이 큼. 회사 피크를 구조적으로 없애고 싶다면 4번 Payroll Merkle Distribution이 더 적합함.

따라서 최종 판단은 다음과 같음.

방안 1, 2 + prover 수평확장은 "지금 당장 현실적으로 갈 수 있는 1차 확장 전략"임.
방안 3, 4는 "실제 benchmark 후 병목이 확인되면 선택할 장기 protocol 전략"임.
