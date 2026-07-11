# Session 3B Batch Transfer Handoff

영문: [clairveil-session3b-batch-transfer-handoff.md](clairveil-session3b-batch-transfer-handoff.md)

이 문서는 Session 3B `BatchJoinSplit16x32` client contract를 사용하는 Go, JS/TS wallet, prover, payroll, operations integrator용 handoff다. 프로젝트 Completion Record가 아니며 Master Roadmap을 변경하지 않는다.

## 고정 integration identity

| contract | 값 |
| --- | --- |
| prepared payload | `batch-transfer-payload-v1` |
| prepared proof | `batch-transfer-proof-v1` |
| active circuit set | `privacy-note-v1` |
| circuit artifact ID | `batch-joinsplit-16x32-v1` |
| prover request/response | `v1` / `v1` |
| prover route | `POST /v1/proofs/batch-transfer` |
| maximum shape | input 16 / output 32 |
| default prover admission | batch 전용 in-flight 1 / queued 4 |

payload hash는 prepared payload를 bind한다. response는 request payload hash를 포함하며 client는 broadcast 전에 version/hash mismatch를 거부해야 한다. unknown/duplicate JSON field와 trailing JSON value는 invalid다.

## 필수 client pipeline

```text
input 1..16개 plan/atomic reserve
-> payment/change/padding output 1..32개 prepare
-> canonical effect, roots, digest, intent 계산
-> structured owner signature 1개 획득
-> private prepared payload(0600) 저장
-> local 또는 명시적으로 선택한 remote prover 1곳에서 prove
-> response version/request payload hash 검증
-> private proof(0600) 저장
-> 모든 nullifier 재확인 후 MsgBatchTransfer broadcast
-> typed scan과 commitment/disclosure 검증
-> batch chain status와 item evidence를 별도 reconcile
```

automatic multi-prover failover를 구현하지 않는다. ciphertext, roots, canonical payload, expiry가 확정되기 전에 signature를 요청하지 않는다. creator는 proof 이후에도 교체 가능하다.

## Shape conformance

downstream 구현은 `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json`을 실행해야 한다. fixture는 1/1, mixed disclosure 3-input/4-output, 31 payments+change, exact 32 payments, explicit exact32 padding을 고정한다.

```bash
go test ./x/privacy/client/sdk/conformance -run TestSession3BBatchTransferContract -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

## Prover와 operations

prover readiness는 batch circuit의 valid R1CS/PK checksum을 뜻하며 validator VK readiness와 다르다. bounded body/JSON framing 뒤 batch permit을 획득하고 semantic validation부터 실제 gnark prove return까지 유지한다. request cancellation만으로 실행 중인 in-process proof capacity를 반환하지 않는다. queue saturation은 HTTP 429, `busy`, `retryable=true`이며 이 flag는 endpoint failover 권한이 아니다.

16x32 circuit은 memory 사용량이 크다. production hard cancellation과 containment에는 process isolation이 필요하다. request body, payload hash, note/path/amount/recipient/signature, witness-derived solver error를 log하지 않는다.

## Scan과 disclosure

typed global cursor를 사용하고 Deposit/2x2 Transfer/Batch Transfer 전체에서 `(height, global_sequence, output_index)` 순서를 보존한다. typed query 실패 뒤 ciphertext 없는 ABCI event로 fallback하지 않는다. wallet 저장 전에 NoteV1 commitment를 검증하고 retry duplicate를 제거한다.

view tag는 untrusted hint다. safe mode는 mismatch에도 full decrypt를 시도하며 tag-only mode는 누락 위험을 명시한 opt-in이어야 한다. public/recipient user disclosure와 audit/self-view full disclosure를 output index, commitment, policy, digest, plaintext blinding에 대해 검증한다. audit decrypt failure는 manual-review evidence이며 chain success를 되돌리지 않는다.

## Payroll과 retry

batch operation 하나가 여러 input reservation과 여러 item output을 소유한다. reservation 생성과 lease/CAS 전이는 atomic하다. batch success는 모든 active input을 소비하지만, payroll item은 expected output index/commitment/recipient evidence/amount/asset/disclosure evidence가 일치해야 성공한다.

timeout/restart에서는 재서명 전에 저장된 tx hash와 input nullifier를 조회한다. retry 정책이 허용하면 같은 signed bytes를 사용한다. atomic output list 일부를 재구성하거나 부분 retry하지 않는다. reconcile 동안 원래 operation ID와 reservation을 유지한다.

## Downstream risk 고지

- input/output count와 timing이 batch shape를 노출한다.
- padding은 state/query size/gas를 늘린다.
- remote prover는 batch 전체 witness를 본다.
- audit ciphertext 포함은 auditor decrypt 가능성을 보장하지 않는다.
- code는 experimental이다.
- formal trusted setup: **NOT PERFORMED**.
- external audit: **NOT PERFORMED**.

## Regression gate

다음 독립 경로를 계속 보존한다.

```bash
go test ./x/privacy/... -count=1
make privacy-e2e-smoke
make reference-payroll-live-localnet
make privacy-batch-joinsplit-localnet
```

`transfer-batch`는 multi-message 의미를 유지한다. one-proof `transfer-batch-16x32`의 alias로 바꾸지 않는다.
