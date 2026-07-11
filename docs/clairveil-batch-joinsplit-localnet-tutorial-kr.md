# BatchJoinSplit16x32 Localnet 튜토리얼

영문: [clairveil-batch-joinsplit-localnet-tutorial.md](clairveil-batch-joinsplit-localnet-tutorial.md)

이 튜토리얼은 one-proof `BatchJoinSplit16x32` 경로를 검증한다. single 2x2 transfer 및 여러 독립 `MsgTransfer`를 한 Cosmos transaction에 넣는 기존 `transfer-batch`와 다른 경로다.

```text
single transfer       = MsgTransfer 1개 / 2x2 proof 1개
transfer-batch        = MsgTransfer 여러 개 / 2x2 proof 여러 개
transfer-batch-16x32  = MsgBatchTransfer 1개 / 16x32 proof 1개
```

현재 구현은 experimental이다. formal trusted setup과 external audit은 수행하지 않았다. remote prover는 batch 전체 witness를 보게 된다. public input/output count는 batch shape를 노출하며, padding은 추가 chain state와 gas 비용을 지불하고 active output count를 감춘다.

## 빠른 검증

기본 target은 node를 시작하거나 대형 proving artifact를 생성하지 않고 Session 3B fixture와 Go conformance test를 검증한다.

```bash
make privacy-batch-joinsplit-localnet
```

실제 chain과 remote prover 경로는 명시적으로 실행한다.

```bash
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

16x32 setup/proof는 resource 사용량이 크다. 충분한 memory와 disk가 있는 host에서 실행한다. 결과는 `tmp/privacy-batch-joinsplit-localnet/out`에 기록하며 mnemonic이 포함된 key JSON과 prepared witness/proof 파일 mode가 `0600`인지 검사한다.

## 실행 case

| case | input | payment | 결과 output | disclosure/self-view |
| --- | ---: | ---: | --- | --- |
| `one-input-one-payment` | 1 | 1 | payment 1개 | private, self-view enabled |
| `three-input-four-output-mixed-disclosure` | 3 | 3 | payment 3개 + change | private/public/recipient-encrypted, self-view enabled |
| `thirty-one-payments-plus-change` | 16 | 31 | payment 31개 + change | private, self-view disabled |
| `exact-thirty-two-payments` | 16 | 32 | payment 32개, change 없음 | private, self-view enabled |
| `explicit-zero-padding` | 1 | 1 | payment + padding 31개 | private, self-view disabled |

정확한 amount, role, mode는 `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json`에 고정되어 있다.

## 단계형 command

`--payment`와 선택적인 `--input-index`를 반복해 payload를 준비한다.

```bash
clairveild tx privacy prepare-batch-transfer \
  --payment '<clairs-address>,4uclair' \
  --payment '<clairs-address>,5uclair,amount,public' \
  --payment '<clairs-address>,6uclair,amount-from-to,recipient-encrypted,<pubkey-hex>' \
  --input-index 10 --input-index 11 --input-index 12 \
  --output-mode compact \
  --prepared-out prepared.json \
  --rescan-wallet \
  --from alice --keyring-backend test
```

`--prover-url`을 생략하면 local proof, 지정하면 선택한 remote prover 한 곳만 호출한다.

```bash
clairveild tx privacy prove-batch-transfer prepared.json \
  --proof-out proof.json \
  --prover-url http://127.0.0.1:18080 \
  --output json
```

bearer 인증 remote prover를 사용할 때는 CLI 환경에 `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`을 설정한다. CLI는 이를 `Authorization: Bearer ...` header로 보내며, process argument에 secret을 노출하는 token flag는 의도적으로 제공하지 않는다.

위 loopback 예시는 plain HTTP를 사용할 수 있다. Bearer credential과 전체 private proof witness를 plaintext로 보내지 않도록 모든 non-loopback prover URL은 HTTPS를 사용해야 한다. Client는 redirect를 따라가지 않는다.

automatic multi-prover failover는 없다. remote failure는 caller에게 반환하고 privacy-aware 정책으로 명시적으로 처리한다.

payload-bound proof를 broadcast한다.

```bash
clairveild tx privacy broadcast-batch-transfer prepared.json proof.json \
  --from alice --keyring-backend test \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-batch-local-1 \
  --gas 80000000 --gas-prices 8500000000uclair --yes --output json
```

통합 command는 prepare flag와 `--prepared-out`, `--proof-out`, optional `--prover-url`을 받는다.

```bash
clairveild tx privacy transfer-batch-16x32 \
  --payment '<clairs-address>,7uclair' \
  --output-mode compact \
  --prepared-out prepared.json \
  --proof-out proof.json \
  --prover-url http://127.0.0.1:18080 \
  --rescan-wallet \
  --from alice --keyring-backend test
```

명시적 padding에만 `--output-mode exact32`를 사용한다. batch 전체 sender self-view를 의도적으로 끌 때만 `--no-self-view`를 사용한다.

## Scan과 disclosure 검증

recipient wallet을 다시 scan한다.

```bash
clairveild tx privacy list-notes \
  --from bob --keyring-backend test \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet --json
```

downstream scanner는 typed global `(height, global_sequence, output_index)` 순서를 사용하고 복구한 NoteV1 commitment를 재계산하며 retry duplicate를 제거해야 한다. view tag는 hint이므로 safe mode는 mismatch에서도 decrypt를 시도한다. recipient/auditor/self-view consumer는 plaintext blinding으로 user/full digest를 재계산한다. audit ciphertext가 chain에 포함되어도 decrypt 가능성을 보장하지 않으므로 실패는 chain failure가 아니라 `AuditDeliveryFailed`/`ManualReview`로 보고한다.

## Restart와 retry

runner는 성공한 batch 이후 node와 prover를 재시작하고 저장한 tx hash로 exact32 transaction을 조회한다. 이어 CLI를 다시 호출해 이미 spent인 payload를 새 envelope로 서명했을 때 fail closed하는지도 확인한다. 이는 spent-nullifier smoke이며 durable payroll worker의 exact-signed-byte retry 검증은 아니다. 해당 불변식은 payroll worker/store 테스트가 담당한다.

production retry 순서는 다음과 같다.

1. operation ID, reservation, prepared payload, proof, signed tx bytes를 유지한다.
2. tx hash를 먼저 조회한다.
3. 재서명 전에 모든 input nullifier를 조회한다.
4. 정책이 허용하면 같은 signed bytes를 retry한다.
5. atomic batch 일부만 retry하지 않는다.
6. expected output evidence가 일치해야 item을 성공 처리한다.

## Payroll 경로

batch payroll worker는 operation 하나를 proof job 하나와 여러 item output에 연결한다. 모든 input note를 atomic reserve하고 prepared/proof artifact를 저장한 뒤 `ProofReady`로 전이한다. batch chain status와 item evidence status는 분리해 reconcile한다. 기존 `reference-payroll-live-localnet` target은 multi-message 2x2 envelope 회귀 경로이며 16x32 proof 경로로 오인해 이름을 바꾸지 않는다.

## 주요 override

```bash
CLAIRVEIL_BATCH_LOCALNET_WORK_DIR=/fast-disk/clairveil-batch \
CLAIRVEIL_BATCH_ARTIFACT_DIR=/verified/dev-artifacts \
RPC_PORT=27657 PROVERD_PORT=19080 \
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

`CLAIRVEIL_BATCH_ARTIFACT_DIR`를 지정하면 이미 검증한 development artifact를 재사용해 고비용 setup을 건너뛴다. `CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, `CLAIRVEIL_PROVERD_BIN`으로 prebuilt binary를 지정할 수 있다.
`CLAIRVEIL_BATCH_LOCALNET_WORK_DIR`는 전용 신규 또는 빈 directory여야 한다. 첫 실행 뒤 runner가 해당 directory만 초기화하도록 허용하는 marker를 남기며, symlink, 보호 경로, marker 없는 non-empty directory는 삭제하지 않고 거부한다.
resource-intensive localnet proof의 owner-intent lifetime은 `BATCH_EXPIRES_IN`으로 조정하며 기본값은 7200초다. `BATCH_GAS` 기본값은 80000000이다.
