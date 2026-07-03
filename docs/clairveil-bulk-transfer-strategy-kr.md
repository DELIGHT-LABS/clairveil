# Clairveil 대량 전송 방안 검토 리포트

## 목적

이 문서는 Clairveil의 현재 shielded transfer 구조에서 대량 전송, 특히 월 1회 직원 10만 명에게 급여를 지급하는 수준의 요구를 어떻게 처리할 수 있는지 검토함.

또한 같은 총량을 여러 기업이 나누어 사용하는 SaaS형 payroll 시나리오, 예를 들어 월 1회 1천 명에게 지급하는 기업 100개를 동시에 지원할 수 있는지도 함께 검토함.

현재 Clairveil의 shielded transfer는 2 input / 2 output 구조임. 일반적으로 output 0은 수신자 note, output 1은 송금자 change note임. 따라서 현재 구조 그대로는 수신자 1명당 proof 1개와 transfer transaction 1개가 필요함.

대량 지급을 위해서는 단순 반복 실행을 넘어 별도의 운영 레이어, batch 제출 방식, 새 batch circuit, 또는 claim 기반 payroll protocol 중 하나를 선택해야 함.

현재 구조의 10만건 예상 소요시간과 아래 4가지 방안의 개선 수치 시뮬레이션은 [Clairveil 대량 전송 소요시간 시뮬레이션 노트](./clairveil-bulk-transfer-time-simulation-kr.md)를 참고함.

## 현재 구조의 핵심 제약

- 한 번의 transfer는 기본적으로 수신자 1명에게만 보냄.
- 각 transfer마다 Groth16 proof 생성이 필요함.
- 각 transfer마다 nullifier, commitment, ciphertext, disclosure event가 발생함.
- 큰 treasury note 하나에서 계속 지급하면 change note chain이 생겨 병렬화가 어려움.
- 대량 지급에는 note inventory, 실패 재시도, transaction 상태 추적, 결과 리포트, 감사 로그가 필요함.
- 1번/2번 방식처럼 기존 transfer를 반복하거나 묶어서 쓰는 경우, 준비한 input note가 다른 transfer, split, merge에 먼저 사용되지 않도록 note reservation이 필요함. 자세한 내용은 [Clairveil Note Reservation 설계 노트](./clairveil-note-reservation-design-kr.md)를 참고함.
- 공개 claim으로 사용할 수 있는 production benchmark는 아직 없음. 다만 repo의 smoke/reference 결과를 바탕으로 한 소요시간 시뮬레이션은 [Clairveil 대량 전송 소요시간 시뮬레이션 노트](./clairveil-bulk-transfer-time-simulation-kr.md)를 참고함.

## 방안 1. Shielded Payroll Batch UX

### 개념

현재 protocol과 circuit은 그대로 두고, 그 위에 급여 전용 orchestration layer를 만드는 방식임.

내부적으로는 직원 1명당 기존 `transfer`를 1번 실행함. 다만 사용자는 CSV 또는 HR system에서 급여 목록을 넣고, 시스템이 자동으로 proof 생성, transaction 전송, 실패 재시도, 상태 추적, 결과 리포트를 관리함.

```text
급여 CSV / HR DB
-> 주소 검증
-> 지급 계획 생성
-> treasury note inventory 확인
-> proof job queue
-> transaction broadcast queue
-> transaction result scanner
-> retry / reconcile / report
```

### 구현 방식

기존 `transfer`를 그대로 노출하는 대신, 그 위에 payroll 전용 실행 레이어를 만듦. 이 레이어는 사용자가 CSV나 HR system 데이터를 넣으면 지급 계획을 만들고, 각 지급 건을 기존 transfer로 변환하고, proof 생성과 transaction 전송을 queue로 처리함.

사용자에게는 다음 command 또는 API를 제공함.

```text
payroll plan
payroll run
payroll status
payroll retry
payroll export-report
```

각 기능은 다음처럼 동작함.

| 기능 | 구현 내용 | 사용자가 할 수 있는 일 |
| --- | --- | --- |
| `payroll plan` | CSV/HR 데이터를 읽고 recipient address, amount, denom, duplicate row, treasury balance를 검증함. 가능한 경우 각 지급 건에 사용할 treasury shard와 input note 후보를 미리 배정함. | 실제 지급 전에 오류가 있는 직원을 찾아내고, 필요한 총액과 예상 proof/tx 수를 확인함. |
| `payroll run` | plan 결과를 proof job queue에 넣고, worker들이 기존 transfer SDK로 직원별 `MsgTransfer`를 생성함. proof가 준비되면 broadcast queue가 transaction을 순차 또는 병렬로 제출함. | 10만 건을 직접 반복 실행하지 않고 하나의 payroll job으로 실행함. |
| `payroll status` | 각 item의 상태를 `planned`, `proving`, `proof_ready`, `broadcasted`, `included`, `failed`, `confirmed`처럼 추적함. chain scanner가 tx hash와 block height를 수집함. | 지급이 몇 건 완료됐고 몇 건 실패했는지 실시간으로 확인함. |
| `payroll retry` | 실패한 item만 골라 원인을 분류하고 재시도함. 주소 오류처럼 재시도해도 안 되는 실패와 mempool/network 실패처럼 재시도 가능한 실패를 구분함. | 전체 payroll을 다시 돌리지 않고 실패분만 복구함. |
| `payroll export-report` | 지급 완료 내역, 실패 내역, tx hash, block height, disclosure 정책, 감사용 summary를 파일로 내보냄. | 기업 고객에게 지급 결과 리포트와 감사 자료를 제공함. |

이 레이어가 관리해야 하는 데이터는 payroll job과 item 단위로 나눔.

payroll job에는 다음 정보를 둠.

- payroll batch id
- company id
- payroll period
- total recipient count
- total amount / denom
- privacy policy
- disclosure policy
- job status
- created at / started at / completed at

payroll item에는 다음 정보를 둠.

- employee id 또는 익명화된 employee hash
- recipient shielded address
- amount / denom
- assigned treasury shard
- job status
- proof status
- transaction hash
- block height
- failure reason
- retry count

내부 component는 다음 역할로 나눔.

| component | 구현 내용 | 효과 |
| --- | --- | --- |
| recipient address validator | payroll import 시 shielded address format과 network prefix를 검사함. | 잘못된 주소 때문에 proof 생성 단계에서 실패하는 일을 줄임. |
| duplicate recipient detector | 같은 employee id, 같은 address, 같은 payroll row가 중복됐는지 검사함. | 중복 지급 위험을 줄임. |
| note inventory planner | 현재 treasury가 가진 spendable note를 스캔하고, 지급 가능한 input note 후보를 만듦. | 지급 전에 잔액 부족과 note 부족을 알 수 있음. |
| treasury shard allocator | 큰 treasury를 여러 shard note로 나누고, item별로 서로 다른 shard를 배정함. | change note chain을 줄여 proof 생성 병렬성을 높임. |
| proof worker queue | 각 item을 기존 transfer SDK 호출로 변환하고 proof를 생성함. | worker 수를 조절해 prover resource를 제어함. |
| transaction broadcaster | proof가 준비된 item을 chain에 제출하고 tx hash를 저장함. | account sequence, gas, mempool 제출을 중앙에서 관리함. |
| transaction confirmation scanner | tx hash를 조회해 included/failed 상태와 block height를 기록함. | 완료 여부를 수동 확인하지 않아도 됨. |
| retry policy | 실패 원인별로 재시도 가능 여부와 횟수를 정함. | 일시적 실패는 자동 복구하고 영구 실패는 report로 분리함. |
| audit/report generator | payroll job과 item 상태를 기업 고객용 report로 변환함. | 지급 결과, 실패 사유, 감사 로그를 제공함. |

기존 transfer 경로는 재사용함.

- `x/privacy/client/sdk/transfer.BuildTransferMessage`
- `x/privacy/client/sdk/transfer.ExecuteTransfer`
- `x/privacy/client/cli transfer`

이 방안에서 note inventory planner와 treasury shard allocator는 단순 편의 기능이 아니라 안전한 대량 지급의 핵심임. payroll plan에서 선택한 note가 실행 중 다른 transfer, split, merge에 소비되면 proof와 broadcast가 실패할 수 있으므로, 클라이언트는 별도의 [note reservation 모델](./clairveil-note-reservation-design-kr.md)을 구현해야 함.

### 장점

- 현재 circuit과 keeper를 거의 바꾸지 않음.
- 가장 빠르게 MVP를 만들 수 있음.
- 기존 `transfer` 검증 모델을 그대로 사용함.
- 운영 자동화와 실패 복구를 먼저 확보할 수 있음.
- 실제 처리량 benchmark를 수집하기 좋음.
- 이후 방안 2, 3, 4의 상위 UX로도 재사용할 수 있음.

### 단점

- proof 수와 transaction 수는 줄지 않음.
- 직원 10만 명이면 transfer 10만 건이 필요함.
- 단일 treasury note에서 지급하면 순차 의존성이 강함.
- 진짜 대량 지급 성능 문제는 해결하지 못함.
- treasury shard와 사전 note split 없이는 병목이 큼.

### 기대효과

단기적으로 수동 반복 실행을 운영 가능한 batch job으로 바꿀 수 있음. 제품 UX, 리포트, 실패 복구, 주소 검증, 감사 추적을 갖출 수 있으므로 실험 및 초기 운영에는 가장 현실적인 출발점임.

## 방안 2. Batch Transfer Message / Multi-Message Transaction

### 개념

기존 transfer proof와 `MsgTransfer`는 유지하되, 여러 transfer를 하나의 실행 단위로 묶는 방식임.

가벼운 방식은 하나의 Cosmos transaction 안에 여러 `MsgTransfer`를 넣는 것임.

```text
Tx
  MsgTransfer employee 1
  MsgTransfer employee 2
  MsgTransfer employee 3
```

더 강한 방식은 새 `MsgBatchTransfer`를 만들어 module message 자체가 여러 transfer item을 담게 하는 것임.

```text
MsgBatchTransfer
  creator
  items[]
    proof
    root
    nullifiers
    commitments
    ciphertexts
    disclosure fields
```

### 구현 방식

1차 구현은 기존 Cosmos transaction에 여러 `MsgTransfer`를 담는 방식으로 시작함. 새 circuit이나 keeper message를 만들지 않고, SDK와 payroll runner가 여러 transfer message를 chunk로 묶어 제출하도록 만듦.

구현 흐름은 다음과 같음.

```text
payroll items
-> item별 기존 transfer proof 생성
-> item별 MsgTransfer 생성
-> gas/size 한도에 맞춰 chunk 구성
-> 한 Cosmos tx에 여러 MsgTransfer 포함
-> chunk 단위 broadcast
-> chunk 단위 status/retry/report
```

각 기능은 다음처럼 구현함.

| 기능 | 구현 내용 | 사용자가 할 수 있는 일 |
| --- | --- | --- |
| message builder | payroll item마다 기존 transfer SDK를 호출해 `MsgTransfer`를 만듦. | 기존 transfer와 같은 보안 모델로 여러 지급 건을 준비함. |
| chunk planner | `K`개 message를 한 transaction에 넣되, gas limit과 transaction size를 넘지 않도록 동적으로 chunk 크기를 조정함. | 네트워크 한도에 맞는 안전한 제출 단위를 만듦. |
| note conflict checker | 같은 chunk 안에서 동일 input note나 nullifier가 중복 사용되지 않는지 검사함. | chunk 전체가 nullifier 충돌로 실패하는 일을 줄임. |
| batch broadcaster | `[]sdk.Msg`를 하나의 Cosmos tx로 sign/broadcast하고 chunk id와 tx hash를 저장함. | 10만 개 tx 대신 더 적은 수의 chunk tx를 추적함. |
| chunk retry | transaction이 실패하면 chunk 전체를 재시도하거나, 실패 item을 분리해 더 작은 chunk로 재구성함. | 하나의 실패가 전체 payroll을 멈추지 않게 함. |
| limit profiler | 실제 chain에서 `K` 값을 바꿔가며 gas, tx size, event size, inclusion time을 측정함. | 운영 환경에 맞는 최대 chunk 크기를 찾음. |

이 방식에서 중요한 점은 `MsgTransfer`의 proof와 검증 방식은 그대로라는 것임. 따라서 privacy logic은 바뀌지 않고, transaction 제출 방식만 개선됨.

2차 구현으로는 `MsgBatchTransfer`를 고려할 수 있음. 이 경우 여러 transfer item을 하나의 module message 안에 넣음.

```text
MsgBatchTransfer
  creator
  items[]
    proof
    root
    nullifiers
    commitments
    ciphertexts
    disclosure fields
```

이 방식을 선택하면 `proto/clairveil/privacy/v1/tx.proto`에 `MsgBatchTransfer`를 추가하고, keeper에 `BatchTransfer` handler를 만듦. handler는 각 item을 기존 `MsgTransfer`와 같은 규칙으로 검증하되, batch 전체에 대한 크기 제한, event format, 실패 정책을 함께 적용함.

`MsgBatchTransfer`의 장점은 module 관점에서 batch를 하나의 명시적인 도메인 개념으로 다룰 수 있다는 점임. 반면 proto, generated code, keeper, SDK, CLI, tests가 모두 늘어나므로 1차 MVP는 multi-message transaction으로 시작하는 편이 안전함.

multi-message transaction과 `MsgBatchTransfer` 모두 input note 충돌을 자동으로 해결하지는 않음. batch에 들어가는 item들은 사전에 예약된 note만 사용해야 하고, 같은 chunk 안에서 동일 note/nullifier가 중복되지 않아야 함. 이 정책은 [Clairveil Note Reservation 설계 노트](./clairveil-note-reservation-design-kr.md)의 상태 머신과 실패 대응 흐름을 따름.

### 장점

- 기존 proof와 circuit을 그대로 쓸 수 있음.
- transaction 제출 UX가 개선됨.
- 여러 transfer를 한 번의 제출 단위로 관리할 수 있음.
- payroll batch UX와 잘 결합됨.
- 새 circuit 없이 구현 가능함.

### 단점

- proof 수는 줄지 않음.
- transfer 10만 명이면 proof도 여전히 10만 개임.
- transaction size, gas limit, event size 한계가 있음.
- batch 안의 한 item이 실패하면 전체 transaction이 실패할 수 있음.
- 처리량 개선은 제한적임.

### 기대효과

방안 1의 실행 엔진을 더 효율화함. 특히 transaction 제출과 상태 추적을 chunk 단위로 관리할 수 있어 운영 편의가 좋아짐. 그러나 근본적인 proof 생성 비용과 on-chain 처리량 문제는 그대로 남음.

## 방안 3. N-Output Batch Circuit

### 개념

현재 2 input / 2 output transfer circuit을 대량 지급용 circuit으로 확장함. 한 proof가 여러 recipient note를 생성하도록 만듦.

예:

```text
BatchJoinSplit32
  inputs:
    treasury note 1
    treasury note 2

  outputs:
    employee note 1
    employee note 2
    ...
    employee note 32
    treasury change note
```

즉, 직원 32명 지급을 proof 1개와 transaction 1개로 처리하는 구조임.

### 구현 방식

privacy 모듈에 대량 지급용 `BatchJoinSplitN` 회로를 추가함. 이 회로는 여러 input note를 받아 여러 recipient output note와 change output note를 한 번에 생성함. 즉, 기존 transfer가 "수신자 1명 + change 1개"를 만드는 구조라면, batch circuit은 "수신자 N명 + change 1개"를 만드는 구조임.

예를 들어 `BatchJoinSplit32`는 다음 흐름으로 동작함.

```text
input notes:
  treasury note A
  treasury note B

recipient data:
  employee 1 address, amount
  employee 2 address, amount
  ...
  employee 32 address, amount

circuit output:
  employee commitment 1
  employee commitment 2
  ...
  employee commitment 32
  treasury change commitment
  input nullifiers
```

회로 내부에서는 다음을 증명함.

- input note를 실제로 소비할 권한이 있음.
- input note들의 총액이 recipient 지급액 합계와 change 금액을 충족함.
- 각 recipient output commitment가 올바른 amount, asset, recipient key, randomness로 만들어졌음.
- change output이 남은 금액을 sender에게 돌려줌.
- 소비한 input note의 nullifier가 올바르게 계산되었음.
- 공개 입력으로 나가는 root, nullifier, output commitment가 witness와 일치함.

이렇게 하면 직원 32명에게 지급하기 위해 proof 32개를 만들지 않고, proof 1개로 32개의 recipient note를 생성할 수 있음. keeper는 proof 1개를 검증한 뒤 N개의 commitment를 state에 추가하고, 직원별 encrypted note/ciphertext를 event 또는 별도 payload로 기록함.

구현은 다음 단계로 진행함.

| 단계 | 구현 내용 | 결과 |
| --- | --- | --- |
| 1. batch size 결정 | `N=8`, `N=16`, `N=32`처럼 고정 크기를 정함. 처음에는 `N=8` 또는 `N=16`으로 PoC를 만들고, 이후 `N=32`를 benchmark함. | circuit 크기, proving time, tx size를 통제할 수 있음. |
| 2. circuit 구현 | `x/privacy/circuit`에 `BatchJoinSplitN`을 추가하고 input note 배열, recipient output 배열, change output을 constraint로 연결함. | 하나의 proof가 여러 recipient commitment를 생성함. |
| 3. artifact 생성 | N별 R1CS, proving key, verifying key를 만들고 artifact manifest와 checksum을 확장함. | node와 prover가 같은 circuit artifact를 검증 가능하게 공유함. |
| 4. proto/message 추가 | `MsgBatchTransfer` 또는 `MsgBatchJoinSplit`에 root, nullifiers, output commitments, ciphertexts, disclosure payload, proof를 담음. | batch proof 결과를 chain에 제출할 수 있음. |
| 5. keeper verifier 추가 | keeper가 batch verifying key로 proof를 검증하고, nullifier 중복 확인 후 N개 commitment를 append함. | on-chain state가 batch 지급 결과를 반영함. |
| 6. SDK/prover 확장 | SDK가 payroll item N개를 묶어 witness를 만들고, prover HTTP contract가 batch proof 요청을 받을 수 있게 함. | app layer가 batch 단위로 proof를 생성함. |
| 7. CLI/API 추가 | `batch transfer` 또는 payroll runner 내부 API로 batch proof 생성과 제출을 제공함. | 사용자는 32명 단위 지급을 하나의 실행 단위로 처리함. |
| 8. disclosure 설계 | employee별 ciphertext와 disclosure payload를 개별로 둘지, batch metadata와 함께 묶을지 정함. | 수신자 복호화와 감사 리포트가 batch 구조에서도 동작함. |

설계 시 특히 정해야 하는 것은 input note 개수 `M`, recipient output 개수 `N`, change output 개수임. `M`이 너무 작으면 큰 payroll을 만들기 어렵고, 너무 크면 circuit이 무거워짐. `N`이 커질수록 proof 수는 줄지만 proving time, memory, transaction size가 커짐.

batch 안에서 직원별 금액을 다르게 허용할지도 중요함. 급여는 직원마다 금액이 다르기 때문에, 실사용 payroll을 목표로 한다면 output별 amount를 witness로 받고 commitment에 반영해야 함. 단, 공개적으로 amount를 노출하지 않을지, disclosure 정책에 따라 일부만 공개할지 같이 설계해야 함.

### 장점

- proof 수와 transaction 수를 크게 줄일 수 있음.
- 회사가 직접 일괄 지급하는 모델에 잘 맞음.
- 방안 1, 2보다 대량 처리량 개선 효과가 큼.
- 급여일에 회사가 지급을 밀어 넣는 방식과 자연스러움.

### 단점

- 구현 난이도가 높음.
- circuit, proving key, verifying key, keeper, SDK, CLI, docs, tests가 모두 바뀜.
- N이 커질수록 proving time, memory, key size, transaction size가 커짐.
- 고정 크기 circuit이 필요하므로 N 선택이 중요함.
- batch 안의 한 항목 문제로 전체 proof/transaction이 실패할 수 있음.

### 기대효과

직접 대량 송금 처리량을 실질적으로 개선할 수 있음. 예를 들어 N=32 circuit이 안정적으로 동작한다면 10만 명 지급은 약 3,125개 batch로 줄어듦. 기존 10만 transfer보다 훨씬 현실적임.

## 방안 4. Payroll Merkle Distribution

### 개념

회사가 직원 10만 명에게 직접 transfer를 모두 보내는 대신, 급여 배분 목록의 Merkle root를 on-chain에 등록하고 직원 또는 relayer가 각자 claim하는 방식임.

```text
회사
-> payroll list 생성
-> Merkle root 계산
-> total amount escrow
-> MsgCreatePayroll

직원 또는 relayer
-> 자기 leaf와 Merkle proof 확보
-> claim proof 생성
-> MsgClaimPayroll
-> shielded note 수령
```

### 구조

Payroll leaf 예시:

```text
leaf = hash(
  payroll_id,
  employee_id_hash,
  amount,
  denom,
  recipient_shielded_spend_pubkey,
  recipient_shielded_view_pubkey,
  salt
)
```

On-chain에는 전체 목록이 아니라 root만 올라감.

```text
PayrollCampaign
  payroll_id
  root
  denom
  total_amount
  claimed_amount
  deadline
  metadata_hash
```

직원은 자기 leaf가 root에 포함되어 있음을 증명하고, 그 leaf 내용으로 shielded note commitment를 생성함.

### 구현 방식

payroll을 하나의 campaign으로 등록하고, 직원 또는 relayer가 campaign에서 자기 몫을 claim하는 protocol을 privacy 모듈에 추가함. 구현의 핵심은 `MsgCreatePayroll`로 지급 목록의 root와 escrow를 만들고, `MsgClaimPayroll`로 개별 직원의 수령을 처리하는 것임.

```text
MsgCreatePayroll
  creator
  payroll_id
  root
  denom
  total_amount
  deadline
  metadata_hash

MsgClaimPayroll
  creator 또는 relayer
  payroll_id
  claim_nullifier
  output_commitment
  encrypted_note
  proof
```

`MsgCreatePayroll`은 회사가 실행함. 회사는 off-chain에서 급여 목록을 만들고 Merkle root를 계산한 뒤, root와 총 지급액을 chain에 등록함. keeper는 회사의 transparent 또는 shielded treasury에서 지급 재원을 escrow하고, `PayrollCampaign` state를 만듦.

`MsgClaimPayroll`은 직원 또는 relayer가 실행함. claim 요청자는 자기 leaf가 payroll root에 포함되어 있다는 Merkle proof와, 그 leaf 내용으로 생성한 shielded output commitment를 제출함. keeper는 claim proof를 검증하고, 이미 claim된 leaf인지 확인한 뒤 commitment를 append함.

이를 위해 claim 전용 회로를 만듦.

```text
PayrollClaimCircuit
  public:
    payroll_root
    claim_nullifier
    output_commitment

  secret:
    amount
    asset_id
    recipient spend/view keys
    salt
    merkle path
```

이 회로는 다음을 증명함.

- claim 대상 leaf가 payroll root에 포함되어 있음.
- leaf 안의 amount, asset, recipient key, salt가 output commitment와 일치함.
- claim nullifier가 payroll id와 employee claim secret에서 올바르게 계산되었음.
- 같은 leaf가 두 번 claim되지 않도록 공개 claim nullifier를 생성했음.

구현은 다음 단계로 나눔.

| 단계 | 구현 내용 | 결과 |
| --- | --- | --- |
| 1. payroll file 생성 | 회사가 직원별 amount, denom, recipient key, salt를 포함한 payroll file을 만듦. | on-chain에 전체 급여 목록을 올리지 않고도 claim 근거를 만들 수 있음. |
| 2. Merkle root 계산 | payroll file의 각 row를 leaf로 만들고 Merkle tree root를 계산함. | company는 root 하나로 전체 지급 목록을 commit함. |
| 3. campaign 등록 | `MsgCreatePayroll`로 root, total_amount, deadline, metadata_hash를 제출하고 재원을 escrow함. | chain에 claim 가능한 payroll campaign이 생김. |
| 4. claim package 배포 | 직원별 leaf, Merkle path, salt, amount 정보를 안전하게 전달함. | 직원 또는 relayer가 자기 claim proof를 만들 수 있음. |
| 5. claim proof 생성 | SDK/prover가 `PayrollClaimCircuit` witness를 만들고 proof를 생성함. | claim자가 자기 몫을 공개 목록 없이 증명함. |
| 6. claim tx 제출 | `MsgClaimPayroll`에 proof, claim_nullifier, output_commitment, encrypted_note를 담아 제출함. | 직원의 shielded note가 생성됨. |
| 7. campaign 정산 | deadline 이후 미수령 금액을 환수하거나 재발행 정책을 실행함. | 기업 고객이 미수령분을 관리할 수 있음. |

keeper에는 다음 기능을 구현함.

- payroll campaign 저장
- escrow balance 관리
- claim nullifier 저장
- duplicate claim 방지
- deadline 처리
- 미수령분 환수 정책
- commitment append
- reserve accounting 연결

SDK/CLI에는 다음 기능을 제공함.

- payroll root 생성
- claim file 생성
- claim proof 생성
- claim transaction 전송
- claim 상태 조회
- 미수령 목록/통계 리포트

사용자 흐름은 다음과 같음.

```text
회사:
payroll create-file
-> payroll create-campaign
-> payroll monitor-claims
-> payroll close/refund

직원 또는 relayer:
payroll claim
-> claim proof 생성
-> MsgClaimPayroll 제출
-> shielded note 수령
```

이 구조가 가능해지면 회사는 급여일에 직원 수만큼 transfer를 실행하지 않아도 됨. 회사는 campaign을 열고 재원을 escrow하며, 실제 수령은 직원 또는 relayer가 claim window 안에서 분산 처리함.

### 장점

- 10만 명 지급에 가장 잘 맞는 구조임.
- 회사가 10만 transaction을 직접 보내지 않아도 됨.
- claim이 직원 또는 relayer에 의해 시간적으로 분산됨.
- 회사의 급여일 처리 부하가 크게 줄어듦.
- Merkle root만 공개하면 전체 급여 목록을 직접 on-chain에 올리지 않아도 됨.
- 미수령, 만료, 환수 같은 급여 운영 정책을 명확하게 설계할 수 있음.

### 단점

- 현재 transfer와는 별도 protocol에 가까움.
- 새 message, keeper state, circuit, SDK, CLI가 필요함.
- 직원별 claim UX가 필요함.
- claim을 누가 실행하고 fee를 누가 낼지 정해야 함.
- 미수령 급여, 환수, 재발행, 오류 수정 정책이 필요함.
- payroll root 생성 과정의 off-chain 보안이 중요함.

### 기대효과

대량 급여 지급에는 가장 확장성 있는 방식임. 회사는 급여 campaign을 만들고 escrow를 잠그며, 수령은 직원 또는 relayer가 분산 처리함. 10만 명 규모에서도 회사가 한 시점에 10만 proof/transaction을 생성하지 않아도 됨.

## 멀티테넌트 적용 시나리오: 100개 기업 x 월 1천건

### 질문

앞의 방안들은 월 1회 직원 10만 명에게 지급하는 단일 대기업을 기준으로 설명했음. 그런데 실제 제품에서는 한 기업이 10만 명에게 지급하는 경우뿐 아님, 월 1회 1천 명에게 지급하는 기업 100개를 동시에 지원해야 할 수 있음.

두 시나리오는 월간 총 전송량만 보면 모두 10만 건임.

```text
단일 대기업 모델:
1개 기업 x 100,000명 = 월 100,000건

SaaS 멀티테넌트 모델:
100개 기업 x 1,000명 = 월 100,000건
```

따라서 순수 처리량 관점에서는 같은 문제처럼 보이지만, 실제 운영 관점에서는 멀티테넌트 모델이 별도의 요구사항을 만듦.

### 결론

100개 기업 x 월 1천건 모델도 대응 가능함. 오히려 단일 기업 10만건보다 운영적으로는 더 다루기 쉬운 면이 있음.

이유는 다음과 같음.

- 기업별 payroll job을 독립적으로 나눌 수 있음.
- 기업별 지급 시각을 분산할 수 있음.
- proof worker, broadcaster, relayer queue를 tenant 단위로 shard할 수 있음.
- 한 기업의 실패가 전체 10만건 지급을 막지 않도록 격리할 수 있음.
- 방안 4의 claim 기반 구조를 적용하면 수령 시점도 직원 단위로 자연스럽게 분산됨.

다만 여러 기업을 동시에 지원하려면 단순 batch 실행기가 아니라 SaaS형 payroll control plane이 필요함.

### 단일 대기업 모델과의 차이

| 항목 | 1개 기업 x 10만건 | 100개 기업 x 1천건 |
| --- | --- | --- |
| 총 전송량 | 월 10만건 | 월 10만건 |
| peak load | 특정 기업의 지급 시간에 집중 | 기업별 schedule로 분산 가능 |
| 실패 범위 | 한 payroll 전체가 큰 blast radius를 가짐 | tenant 단위로 격리 가능 |
| treasury 관리 | 큰 treasury 하나 또는 소수 shard | 기업별 treasury/account/shard 필요 |
| reporting | 단일 대형 report | tenant별 report와 전체 운영 report 모두 필요 |
| rate limit | company 내부 chunking 중심 | tenant별 quota와 global quota가 모두 필요 |
| 감사/공시 정책 | 한 회사 정책 중심 | 회사별 disclosure policy 필요 |
| 제품 복잡도 | 처리량 최적화가 핵심 | 처리량 + tenant isolation이 핵심 |

### 필요한 추가 구조

100개 기업을 지원하려면 각 payroll 실행 단위에 tenant identity가 명확히 들어가야 함.

필요한 주요 식별자는 다음과 같음.

- `company_id`
- `payroll_id`
- `payroll_period`
- `batch_id`
- `recipient_count`
- `total_amount`
- `privacy_policy`
- `disclosure_policy`
- `treasury_account` 또는 `treasury_shard`
- `status`

운영 레이어에서는 다음 구조가 필요함.

```text
Payroll Control Plane
-> company별 payroll 생성
-> payroll별 입력 검증
-> note planning
-> proof job queue 등록
-> tx broadcast queue 등록
-> chain 결과 수집
-> tenant별 report 생성
```

이 구조는 방안 1에서 시작하는 것이 가장 자연스러움. 방안 1의 payroll batch UX를 단일 기업용 CLI나 admin tool로만 만들지 말고, 처음부터 company/payroll/batch/job 단위를 갖는 control plane으로 설계하면 됨.

### 방안별 적용 방식

#### 방안 1 적용

방안 1은 100개 기업 x 월 1천건 모델의 필수 기반임.

각 기업의 payroll을 별도 job으로 관리함.

```text
company A payroll job: 1,000 transfers
company B payroll job: 1,000 transfers
...
company Z payroll job: 1,000 transfers
```

필요한 구현은 다음과 같음.

- 회사별 payroll import
- 회사별 shielded address validation
- 회사별 treasury balance check
- payroll job queue
- proof worker pool
- broadcast worker pool
- tenant별 retry policy
- tenant별 report
- global rate limiter

이 방식만으로도 월간 총 10만건 처리는 운영상 가능해질 수 있음. 다만 여전히 transfer 10만건에 대한 proof 10만개가 필요하므로 실제 가능한 처리 시간은 benchmark로 확인해야 함.

#### 방안 2 적용

방안 2는 각 회사의 1천건 payroll을 chunk 단위로 묶어 제출하는 데 유용함.

예를 들어 회사별 1천건을 20개씩 묶으면 회사당 50개 transaction chunk가 됨.

```text
1 company payroll:
1,000 transfers
-> 20 transfers per tx
-> 50 tx chunks

100 companies:
50 tx chunks x 100
-> 5,000 tx chunks
```

효과는 다음과 같음.

- transaction 제출 수 감소
- broadcast queue 관리 단위 감소
- payroll 진행률 추적 단순화
- 실패한 chunk만 재시도 가능

하지만 proof 수는 줄지 않음. 각 `MsgTransfer`가 기존 2-output JoinSplit proof를 그대로 가지므로, 1천명에게 지급하면 회사당 proof 1천개가 필요함.

#### 방안 3 적용

방안 3은 회사별 1천건을 N-output batch로 줄이는 데 효과적임.

예를 들어 N=32 batch circuit을 사용하면 회사당 batch 수는 약 32개가 됨.

```text
1 company payroll:
1,000 recipients
-> BatchJoinSplit32 약 32개

100 companies:
32 batches x 100
-> 약 3,200 batch proofs
```

기존 방식의 100,000 proof와 비교하면 proof 수가 크게 줄어듦.

멀티테넌트 환경에서는 다음을 추가로 고려해야 함.

- 회사별 batch size 선택
- batch circuit size별 proving key 관리
- company/payroll/batch 단위 proof artifact 관리
- 회사별 disclosure report 생성
- batch 일부 실패 시 재배치 로직

N-output batch circuit은 회사가 직원에게 직접 지급하는 push 방식 payroll에 잘 맞음. 직원이 claim하지 않아도 지급이 완료되어야 하는 기업 고객에게 특히 적합함.

#### 방안 4 적용

방안 4는 100개 기업 x 월 1천건 모델에 가장 SaaS 친화적임.

각 기업이 자기 payroll root를 등록하고, 직원 또는 relayer가 claim함.

```text
company A -> payroll root A 등록
company B -> payroll root B 등록
...
company Z -> payroll root Z 등록

employee 또는 relayer
-> claim proof 생성
-> claim transaction 제출
```

이 방식의 장점은 회사가 한 번에 모든 지급 transaction을 실행하지 않아도 된다는 점임.

각 회사는 다음만 수행함.

- payroll list 생성
- Merkle root 계산
- 총액 escrow
- payroll campaign 등록
- report와 claim 현황 관리

이후 실제 claim은 직원 또는 relayer가 분산 실행함. 따라서 100개 기업의 지급일이 같더라도 모든 지급 실행이 한 시점에 몰릴 가능성이 낮음.

멀티테넌트 구현에는 다음 상태가 필요함.

```text
PayrollCampaign {
  company_id
  payroll_id
  merkle_root
  total_amount
  claimed_amount
  starts_at
  expires_at
  disclosure_policy
  status
}

ClaimNullifier {
  payroll_id
  employee_claim_nullifier
}
```

중요한 점은 claim nullifier가 payroll campaign 단위로 namespace되어야 한다는 것임. 서로 다른 회사의 payroll에서 같은 index나 같은 employee identity가 등장하더라도 충돌하지 않아야 함.

### 운영상 필요한 rate limit

100개 기업을 동시에 지원하려면 global 처리량뿐 아니라 tenant별 공정성이 중요함.

필요한 제한은 다음과 같음.

- 회사별 동시 proof job 수
- 회사별 동시 broadcast 수
- 전체 worker pool의 global concurrency
- block당 제출할 transaction 수
- relayer별 지출 가능한 transparent fee budget
- 실패율이 높은 tenant의 자동 throttle
- 특정 지급일 peak에 대한 예약 slot

예를 들어 모든 회사가 매월 25일 오전 9시에 지급을 시작하면, 시스템은 각 회사의 job을 한 번에 모두 실행하지 않고 slot 단위로 나누는 것이 좋음.

```text
09:00 - 09:10  company 001-010
09:10 - 09:20  company 011-020
09:20 - 09:30  company 021-030
...
```

방안 4를 사용하면 이 scheduling pressure가 훨씬 작아짐. 회사는 root와 escrow만 먼저 등록하고, claim은 직원 또는 relayer가 이후 분산 처리할 수 있기 때문임.

### 제품 관점 추천

100개 기업 x 월 1천건 모델을 목표로 한다면 다음 접근이 현실적임.

1. 먼저 방안 1을 SaaS control plane 형태로 만듦.
2. 방안 2로 기존 transfer의 제출 효율을 개선함.
3. 실제 proof 생성 시간, broadcast throughput, block inclusion 시간을 측정함.
4. push payroll이 중요하면 방안 3으로 감.
5. SaaS형 self-claim payroll이 가능하면 방안 4로 감.

정리하면, 이 시나리오는 단일 10만건보다 protocol 처리량 요구가 낮아지는 것은 아니지만, 운영 분산 가능성이 커짐. 따라서 제품 구조만 잘 잡으면 100개 기업 x 월 1천건은 충분히 목표로 삼을 수 있음.

## 비교 요약

| 방안 | 핵심 | 새 circuit 필요 | 처리량 개선 | 구현 난이도 | 대량 급여 적합도 |
| --- | --- | --- | --- | --- | --- |
| 1. Shielded Payroll Batch UX | 기존 transfer 운영 자동화 | 없음 | 낮음 | 낮음~중간 | 중간 |
| 2. Batch Transfer Message / Multi-Message Transaction | 여러 transfer를 묶어 제출 | 없음 | 중간 이하 | 중간 | 중간 |
| 3. N-Output Batch Circuit | 한 proof로 여러 recipient 지급 | 필요 | 높음 | 높음 | 높음 |
| 4. Payroll Merkle Distribution | root 등록 후 각자 claim | 필요 | 높음 | 높음 | 매우 높음 |

## 추천 접근 순서

### 1단계: 방안 1로 운영 기반 구축

먼저 payroll batch UX를 만듦. 이 단계에서 실제 병목을 측정함.

측정할 것:

- proof 생성 시간
- transaction broadcast 성공률
- block inclusion 시간
- 평균 gas
- transaction 실패 원인
- note planning 실패율
- worker별 throughput
- treasury shard 효과
- note reservation 충돌률과 replan 빈도

### 2단계: 방안 2로 제출 효율 개선

기존 `MsgTransfer` 여러 개를 한 transaction에 담거나, 작은 batch 단위로 제출함. 이 단계는 새 circuit 없이 할 수 있는 최선의 batch 개선임.

검증할 것:

- transaction당 적정 message 수
- gas 한계
- transaction size 한계
- event size 한계
- 실패 시 chunk rollback 비용

### 3단계: 장기 protocol 방향 선택

회사 주도로 모든 급여를 즉시 지급해야 한다면 방안 3이 적합함.

```text
회사 treasury
-> batch circuit
-> 직원 note N개 생성
```

직원이 직접 claim하거나 relayer가 분산 claim해도 되는 모델이면 방안 4가 더 적합함.

```text
회사 payroll root 등록
-> 직원/relayer claim
```

## 결론

단기적으로는 방안 1과 방안 2를 함께 추진하는 것이 가장 현실적임. 이 조합은 현재 구조를 거의 유지하면서 대량 지급 운영 경험과 benchmark를 확보할 수 있음.

장기적으로 10만 명급 급여 지급을 안정적으로 처리하려면 방안 3 또는 방안 4가 필요함. 직접 일괄 지급 모델이면 방안 3, claim 기반 분산 수령 모델이면 방안 4가 더 적합함.

100개 기업 x 월 1천건 모델은 총 처리량은 동일하지만, tenant별 schedule과 queue를 나눌 수 있으므로 운영적으로 더 분산하기 쉬움. 이 경우 방안 1을 처음부터 SaaS control plane으로 설계하고, 방안 2로 제출 효율을 개선한 뒤, 제품 방향에 따라 방안 3 또는 방안 4를 선택하는 접근이 좋음.

추천 roadmap:

```text
1. Shielded Payroll Batch UX
-> 2. Multi-message / batch submission
-> benchmark
-> 3. N-output batch circuit 또는 4. Payroll Merkle Distribution 선택
```

제품 관점에서는 방안 4가 가장 확장성이 크고, 기존 transfer의 자연스러운 확장 관점에서는 방안 3이 더 직접적임.
