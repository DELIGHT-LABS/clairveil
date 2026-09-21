# Clairveil CLI 기능 문서

이 문서는 `clairveild`와 privacy 관련 companion binary의 사용자-facing 기능을 정리합니다.

예시는 모두 reference chain 기준입니다.

```text
chain-id: clairveil-local-1
denom: uclair
transparent prefix: clair
shielded prefix: clairs
```

## 1. 기본 규칙

대부분의 tx 명령은 Cosmos SDK 공통 tx flag를 함께 사용합니다.

```bash
--from alice
--keyring-backend test
--chain-id clairveil-local-1
--gas 9000000
--gas-prices 8500000000uclair
--yes
--output json
```

`--output json`은 tx broadcast response 또는 command-specific JSON을 machine-readable하게 받기 위한 기본 옵션입니다.

## 2. Shielded identity

### show-address

transparent keyring account에서 full shielded address를 파생합니다.

```bash
clairveild tx privacy show-address \
  --from alice \
  --keyring-backend test \
  --output json
```

주요 출력:

| field          | 의미                                           |
| -------------- | ---------------------------------------------- |
| `from_address` | seed 파생의 기준이 된 transparent address      |
| `address`      | 공유 가능한 `clairs1...` full shielded address |
| `derived_from` | `transparent-keyring-root`                     |

상대가 private transfer를 보내려면 `address` 전체가 필요합니다.

### show-view-key

incoming note scan에 필요한 viewing key를 표시합니다.

```bash
clairveild tx privacy show-view-key \
  --from alice \
  --keyring-backend test \
  --output json
```

production wallet에서는 viewing key를 plaintext log나 analytics에 남기면 안 됩니다.

### show-disclosure-pubkey

recipient-encrypted disclosure와 sender self-view disclosure에 쓸 user disclosure public key를 표시합니다.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

이는 V4 audit epoch key가 아닙니다. Fresh V4 initialization은 `--audit-config`에서 public initial audit key와 proof of possession을 입력받으며 offline secret에서 이를 파생하지 않습니다. 이 command는 user/self-view disclosure에만 사용하세요. Audit private key는 node initialization 외부에 둡니다.

### Audit-field V2 runtime 구성

Transaction을 prepare/prove하는 모든 V2 `tx privacy` command는 작은 audit configuration/key-history query에서 network nonce, 원래 initial height, active audit epoch/key, circuit identity를 자동으로 가져옵니다. Chain ID는 일반 Cosmos client 설정 또는 `--chain-id`를 사용합니다. 제거된 `--audit-network-nonce-hex`, `--audit-initial-height` flag를 전달하면 안 됩니다. Expiry와 prover 선택은 계속 명시적인 운영 제어입니다.

```bash
--audit-expiry 30m --audit-prover-timeout 30m
```

Verified bundle local proving에는 `--audit-prover-url`을 생략하고 remote proving에는 명시적으로 신뢰한 URL만 전달합니다. `transfer-batch-16x32`의 `--prover-url`은 `/v2/prover/audit-field` V2 alias이며 `--audit-prover-url`과 충돌하면 안 됩니다.

### Audit provenance 수집과 검증

`clairveil-auditor`는 닫힌 block 범위의 `block`과 `block_results`를 읽고, 실제 성공 privacy transaction 원본과 실행 결과만 하나의 atomic local JSON cache에 보존합니다. 이어 original message proof를 구성된 circuit identity로 검증하고, 보존한 epoch key로 복호화해 deposit-rooted note lineage를 만듭니다. 전체 chain replay를 수행하지 않으며 PrivacyScan을 audit ledger로 사용하지 않습니다.

```bash
clairveil-auditor \
  --chain-id reviewed-chain-1 \
  --node tcp://127.0.0.1:26657 \
  --from-height 100 --to-height 500 \
  --cache /secure/audit/range-100-500.json \
  --audit-keyring-file /secure/audit/keyring.json \
  --audit-artifacts /absolute/path/to/reviewed-artifacts
```

Keyring은 lowercase 32-byte hex `{key_id, secret_key}` 쌍을 담은 `0600` version-1 JSON file이어야 합니다. Configuration, nonce, initial height, public key history, circuit identity는 자동 query합니다. Report는 `collection_complete`와 `provenance_complete`를 분리하고 `last_processed_block`과 마지막 privacy execution 위치를 각각 표시합니다. Block/result, 실행 event, epoch key, 복호화 가능한 envelope, deposit root 중 하나라도 없으면 `AUDIT_INCOMPLETE`이며 empty lineage나 balance 0으로 표시하지 않습니다. 같은 cache를 다시 사용하면 empty block을 포함해 마지막 atomic 저장 block 다음부터 재개합니다.

Collector는 선택한 CometBFT RPC endpoint를 block/result source로 신뢰하며 light client가 아닙니다. 독립적으로 인증한 block history가 필요한 deployment는 이 작은 collector 바깥에서 trust boundary를 제공해야 합니다. Replay input, runtime archive, persistent audit server는 만들지 않습니다.

기본 `clairveil-auditor`에는 EVM wrapper용 `sdk.TxDecoder`와 `VerifyDelegatedExecution` 연결이 없으므로 delegated V2 deposit을 자체적으로 인증할 수 없습니다. Downstream integration이 wrapper decoding과 receipt-success verification을 모두 제공해야 합니다. [Downstream Cosmos Integration Guide §5.1](clairveil-downstream-cosmos-integration-guide-kr.md#51-trusted-deposit-funding)을 참고하세요.

## 3. Deposit

transparent coin을 shielded note로 넣습니다.

```bash
clairveild tx privacy deposit 10uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

동작:

1. `alice`의 transparent keyring에서 shielded spend/view key를 파생합니다.
2. amount와 denom을 note commitment에 묶습니다.
3. transparent coin을 privacy module account로 보냅니다.
4. encrypted note event를 남깁니다.

주의:

- `0uclair` deposit은 dummy note를 준비할 때 사용할 수 있습니다.
- dummy note는 2-input transfer planner가 single large note를 split해야 할 때 필요할 수 있습니다.
- 기록된 development deposit은 `2,868,008` gas를 사용했습니다. `2500000`은 out-of-gas(`code 11`)였으므로 예제는 `3500000`을 사용합니다. Downstream chain은 측정한 실행 결과로 자체 gas policy를 정해야 합니다.

## 4. Note scan

내 shielded wallet note를 chain event에서 복구합니다.

```bash
clairveild tx privacy list-notes \
  --from alice \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --json
```

주요 flag:

| flag              | 의미                                            |
| ----------------- | ----------------------------------------------- |
| `--json`          | machine-readable note list 출력                 |
| `--rescan-wallet` | local note cache를 지우고 genesis부터 다시 scan |

local wallet cache는 restrictive permission으로 저장되지만 production wallet encryption을 대신하지 않습니다.

## 5. Transfer

단일 transfer 명령은 user selective disclosure와 mandatory audit disclosure를 함께 처리합니다.

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 7uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

기본 동작:

- transfer 자체는 on-chain에서 private 상태를 유지합니다.
- audit disclosure는 항상 chain-configured audit key로 암호화되어 포함됩니다.
- sender self-view disclosure는 기본 포함되며 `--no-self-view`로 끌 수 있습니다.
- user disclosure는 기본값 `all-private` / `none`입니다.
- recipient는 full `clairs1...` shielded address여야 합니다.
- `--auto-dummy=true`가 기본값입니다.
- Proof 전 summary는 정확한 `chain id`와 absolute `owner intent expires at unix`를 출력합니다. Chain은 `block_time >= expires_at_unix`에서 거부합니다.

### selective disclosure flag

| flag                  | 값                                                                                             |
| --------------------- | ---------------------------------------------------------------------------------------------- |
| `--privacy-policy`    | `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to` |
| `--disclosure-mode`   | `none`, `public`, `recipient-encrypted`                                                        |
| `--disclosure-pubkey` | recipient-encrypted mode에서 받을 사람의 disclosure pubkey hex                                 |
| `--no-self-view`      | sender self-view disclosure를 생략                                                             |
| `--expires-in`        | owner-intent validity window(seconds). Sign/prove 전에 absolute Unix expiry로 한 번 변환         |

Public amount disclosure 예:

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 7uclair \
  --privacy-policy amount \
  --disclosure-mode public \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Recipient-encrypted disclosure 예:

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 10uclair \
  --privacy-policy amount-from-to \
  --disclosure-mode recipient-encrypted \
  --disclosure-pubkey "$(cat out/bob-disclosure.hex)" \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 10000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

### transfer-batch

여러 독립적인 `MsgTransfer`를 하나의 Cosmos tx envelope에 담아 broadcast합니다.

```bash
clairveild tx privacy transfer-batch "$(cat out/bob-shielded-address.txt)" \
  7uclair 8uclair 9uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 25000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

현재 제한:

- bulk-transfer readiness와 localnet capacity test 용도입니다.
- `--privacy-policy`, `--disclosure-mode`, `--disclosure-pubkey`, `--no-self-view`는 batch 전체에 동일하게 적용됩니다. item별로 서로 다른 disclosure policy를 섞는 기능은 제공하지 않습니다.
- recursive split/merge planner를 실행하지 않습니다.
- 각 amount는 같은 batch 안에서 input note를 재사용하지 않고 spendable exact note 또는 pairable note로 이미 충족 가능해야 합니다.
- 선택된 transfer input에 dummy note가 필요하면 zero-value dummy note가 미리 존재해야 합니다.
- JSON output에는 `txhash`, `height`, `code`, `message_count`, 요청한 `amounts`, 그리고 message별 nullifier, output commitment, disclosure digest를 담은 `items` evidence가 포함됩니다.

### transfer-batch-16x32와 단계형 companion command

`transfer-batch-16x32`는 `MsgBatchTransfer` 하나와 `BatchJoinSplit16x32` proof 하나를 실행합니다. `--payment 'shielded-address,coin[,policy,mode,target-key]'`를 1..32회 반복해 output별 독립 disclosure를 지정하고, 필요하면 `--input-index`로 wallet note 1..16개를 고정하며, `--output-mode compact|exact32`를 선택합니다. Broadcast 전에 private prepared payload와 proof를 mode `0600`으로 저장합니다.

재시작 가능한 batch command와 `/v1/proofs/batch-transfer` endpoint는 legacy-only입니다. `--prover-url`은 `/v2/prover/audit-field` V2 alias로 유지되며 failover나 redirect follow를 하지 않습니다. [legacy reference 경계](clairveil-getting-started-kr.md#8-legacy-batchjoinsplit16x32-reference)를 참고하세요.

## 6. Disclosure decode

transfer disclosure payload를 복호화하고 digest 검증 report를 만듭니다.

Public disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-public.txhash)" \
  --disclosure-plane public \
  --node tcp://localhost:26657 \
  --report
```

Recipient disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane recipient \
  --from bob \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

Audit disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

Sender self-view disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane self-view \
  --from alice \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

주요 flag:

| flag                   | 의미                                                |
| ---------------------- | --------------------------------------------------- |
| `--tx-hash`            | event에서 disclosure payload를 찾아옴               |
| `--disclosure-plane`   | `auto`, `public`, `recipient`, `self-view`, `audit` |
| `--from`               | disclosure private key를 keyring에서 파생할 account |
| `--disclosure-privkey` | explicit disclosure private key scalar hex          |
| `--report`             | verification, summary, payload를 한 JSON으로 출력   |

`auto`는 tx event에 있는 후보 disclosure payload를 순서대로 시도하고, 현재 disclosure key로 복호화와 검증에 성공한 plane을 선택합니다.

`verification.verified=true`가 아니면 payload를 사용자에게 사실처럼 보여주면 안 됩니다.

## 7. Withdraw

shielded note를 transparent recipient에게 보냅니다.

```bash
clairveild tx privacy withdraw 11uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

withdraw는 exact-match note를 사용합니다. output note나 change note를 만들지 않습니다. 요청 amount와 같은 spendable note가 없으면 기본적으로 planner가 self-transfer로 exact-match note를 만들려고 시도합니다.

Proof 전에 CLI가 current `chain id`와 absolute `spend intent expires at unix`를 출력합니다. 이 값과 recipient, amount, asset, root, nullifier는 owner-signed/proof-bound입니다. `creator`는 fee payer라 relayer가 바꿀 수 있습니다.

주요 flag:

| flag              | 기본값         | 의미                                                  |
| ----------------- | -------------- | ----------------------------------------------------- |
| `--recipient`     | sender address | transparent recipient                                 |
| `--auto-plan`     | `true`         | exact-match note가 없을 때 planner 실행               |
| `--auto-dummy`    | `true`         | planner가 필요로 하는 zero-value dummy note 자동 준비 |
| `--rescan-wallet` | `false`        | note 선택 전 local cache reset 후 rescan              |

## 8. Relayed withdraw

사용자가 withdraw payload를 만들고 relayer가 대신 제출하는 흐름입니다.

사용자:

```bash
clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --out out/withdraw-payload.json \
  --output json
```

Relayer:

```bash
clairveild tx privacy relay-withdraw out/withdraw-payload.json \
  --from relayer \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

`prepare-withdraw` 주요 flag:

| flag           | 기본값         | 의미                               |
| -------------- | -------------- | ---------------------------------- |
| `--recipient`  | sender address | transparent recipient              |
| `--out`        | empty          | prepared payload file path         |
| `--expires-in` | default expiry | payload validity window in seconds |
| `--auto-plan`  | `true`         | exact-match note 자동 준비         |
| `--auto-dummy` | `true`         | dummy note 자동 준비               |

Summary가 resolved absolute expiry와 chain ID를 출력하고 JSON도 같은 `expires_at_unix`를 사용합니다. 해당 second 이상에서는 제출이 실패하고 relayer는 연장할 수 없습니다. Prepared payload/proof JSON은 privacy-sensitive하며 output/recipient/chain/expiry를 바꿀 수 없어도 prover payload에는 private note witness가 남습니다. Production wallet은 암호화 저장과 만료/삭제 정책을 가져야 합니다.

현재 CLI handoff version은 transfer payload `v5`, transfer proof/prover contract `v2`, withdraw prover/final payload와 proof/prover/relay contract `v2`, disclosure plaintext/query `privacy-fixed-v1`입니다. Legacy file은 다시 생성합니다.

## 9. Query

직접 CLI wrapper가 있는 query:

```bash
clairveild query privacy check-nullifier <hex_nullifier> \
  --node tcp://localhost:26657

clairveild query privacy reserve uclair \
  --node tcp://localhost:26657
```

다른 query는 gRPC/HTTP gateway와 generated client로 사용할 수 있습니다. V1은 wallet scan/tree/reserve/asset query surface로 유지되고, live audit runtime configuration과 epoch history는 별도 V2 surface입니다.

| Query | Method | HTTP path |
| --- | --- | --- |
| tree state | GET | `/clairveil/privacy/v1/tree_state` |
| nullifier | GET | `/clairveil/privacy/v1/nullifier/{nullifier}` |
| batch nullifiers | GET, POST | `/clairveil/privacy/v1/nullifiers` |
| commitment info | GET | `/clairveil/privacy/v1/commitment/{commitment_hex}` |
| events | GET | `/clairveil/privacy/v1/events` |
| scan events | GET | `/clairveil/privacy/v1/scan_events` |
| Merkle path | GET | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| disclosure config | GET | `/clairveil/privacy/v1/disclosure_config` |
| circuit config | GET | `/clairveil/privacy/v1/circuit_config` |
| reserve | GET | `/clairveil/privacy/v1/reserve/{denom=**}` |
| asset by denom | GET | `/clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}` |
| asset by ID | GET | `/clairveil/privacy/v1/assets/by_id/{asset_id_hex}` |
| typed privacy scan | POST | `/clairveil/privacy/v1/privacy_scan` |
| commitment paths at root | POST | `/clairveil/privacy/v1/commitment_paths_at_root` |

| V2 audit query | Method | HTTP path |
| --- | --- | --- |
| audit configuration | GET | `/clairveil/privacy/v2/audit/configuration` |
| audit key schedule | GET | `/clairveil/privacy/v2/audit/key_schedule` |
| audit key history entry | GET | `/clairveil/privacy/v2/audit/keys/{epoch}` |

## 10. Companion binary

### clairveil-setup

audit-field V2 artifact set과 identity-pinned manifest를 생성합니다. 필수 acknowledgement는 bundle이 development-grade임을 명시적으로 유지합니다. Generated R1CS/PK/VK binary는 source artifact가 아니며 이 command는 formal trusted setup ceremony가 아닙니다.

```bash
clairveil-setup --out artifacts/audit-field --development
```

출력 디렉터리는 존재하면 안 됩니다. 검토된 release bundle을 재생성하지 말고, 그와 일치하는 runtime config 및 artifact를 그대로 재사용하세요.

### clairveild privacy 구성

일반 server start와 export에는 같은 작은 V4 구성과 검토된 verifier artifact directory가 필요하며, 둘 중 하나가 없으면 오류입니다.

```bash
clairveild start \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/audit-field-artifacts

clairveild export \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/audit-field-artifacts
```

### clairveil-verify (legacy 전용)

이 binary는 과거 SHA-256-of-address seed, raw Base64 ciphertext, JSON `types.Note` 형식으로 생성한 legacy ciphertext를 확인할 때만 사용합니다.

```bash
clairveil-verify -enc '<BASE64_LEGACY_CIPHERTEXT>' -secret '<LEGACY_ADDRESS_OR_SEED>'
```

현행 keyring-signature root seed 및 `privacy-fixed-v1` typed envelope와 호환되지 않습니다. Derived scalar prefix와 복호화한 plaintext도 출력하므로 production secret이나 data에 사용하지 마세요. 현행 note는 `clairveild tx privacy list-notes`, typed `privacy_scan` flow, conformance fixture로 검증합니다.

### clairveil-proverd

Companion prover HTTP service를 실행합니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=artifacts/audit-field
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd \
  -listen 127.0.0.1:8080 \
  -read-header-timeout 5s \
  -read-timeout 30s \
  -write-timeout 0s \
  -idle-timeout 2m \
  -max-request-bytes 8388608
```

Remote production profile은 [clairveil-operations-guide-kr.md](clairveil-operations-guide-kr.md#6-prover-운영)를 따릅니다.

`clairveil-proverd`는 audit-field V2만 제공하며 artifact directory가 필요합니다. local VK/public-input schema hash를 runtime `CircuitSetIdentity`와 비교하고 checksum env로 override할 수 없습니다. Validator는 VK만 필요하고 `clairveil-proverd`는 proof 생성 시 R1CS/PK를 lazy load합니다. Bearer token 검사는 이 production-oriented 예시처럼 `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`을 설정한 경우에만 강제됩니다. Bundle은 development-grade로 남으며 prover endpoint failover는 기본 off이고 explicit privacy opt-in이 필요합니다.

### clairveil-payroll

Reference payroll product workflow를 로컬 파일과 JSON report 중심으로 실행합니다.

```bash
clairveil-payroll validate -input payroll.json -out validation.json
clairveil-payroll build-input-from-notes -template payroll-template.json -notes alice-notes.json -out payroll.json
clairveil-payroll prepare-notes -input payroll.json -out note-preparation.json
clairveil-payroll plan -input payroll.json -out plan.json
clairveil-payroll run -plan plan.json -state .clairveil-payroll/reservation-state.json -out confirmed-plan.json
clairveil-payroll status -plan plan.json -out status.json
clairveil-payroll status -state .clairveil-payroll/reservation-state.json -out state-status.json
clairveil-payroll scan-evidence -plan plan.json -state .clairveil-payroll/reservation-state.json -tx-query tx-query.json -out scanned-evidence.json
clairveil-payroll scan-evidence -plan plan.json -state .clairveil-payroll/reservation-state.json -tx-query tx-query.json -apply -out scanned-and-reconciled.json
clairveil-payroll reconcile -state .clairveil-payroll/reservation-state.json -evidence evidence.json -out reconcile.json
clairveil-payroll settle-transfer-batch -plan plan.json -state .clairveil-payroll/reservation-state.json -tx transfer-batch.json -recipient-before bob-before.json -recipient-after bob-after.json -out settle.json
clairveil-payroll seed-localnet-notes -genesis home/config/genesis.json -wallet-home home -owner-address clair1... -shielded-address clairs1... -count 1000 -amount 1 -denom uclair -notes-out alice-notes.json -out seed-localnet-notes.json
clairveil-payroll export-report -plan plan.json -state .clairveil-payroll/reservation-state.json -out payroll-report.json
```

`build-input-from-notes`는 `list-notes --json` 결과에서 spendable note를 읽어 payroll input의 `treasury_notes`를 채웁니다. `scan-evidence`는 `clairveild query tx --output json` 결과 또는 같은 형태의 tx observation JSON을 읽어 `shielded_transfer` event, output commitment, disclosure digest, nullifier spent evidence를 payroll operation별 reconcile evidence로 변환합니다. `-apply`를 주면 스캔한 evidence를 즉시 durable reservation state에 반영합니다. `settle-transfer-batch`는 실제 `transfer-batch` tx 결과, message별 nullifier/output/disclosure evidence, recipient note scan delta를 검증한 뒤 durable reservation state를 settle합니다.

`seed-localnet-notes`는 localnet rehearsal helper입니다. localnet genesis commitment와 local wallet cache에 payroll용 amount note와 zero dummy note를 기록해 큰 restart/retry rehearsal에서 deposit 준비 시간을 줄입니다. Production note preparation 기능이 아니며 staging/testnet에서는 실제 deposit, split/merge, approval 기반 preparation flow를 사용해야 합니다.

`prepare-notes`와 `plan`은 `-store-dir .clairveil-payroll`을 받아 file-backed reference artifact store에도 결과를 저장할 수 있습니다. `run`, `scan-evidence`, `reconcile`, `settle-transfer-batch`는 durable reservation state 파일을 사용합니다. 간결한 실행 예제는 [reference payroll example](../examples/reference-payroll/README-kr.md)에 있습니다.

### clairveil-payrolld

Reference payroll product의 scheduler/daemon 표면입니다.

```bash
clairveil-payrolld \
  -state .clairveil-payroll/reservation-state.json \
  -once \
  -out .clairveil-payroll/payrolld-report.json

clairveil-payrolld \
  -mode live \
  -state .clairveil-payroll/reservation-state.json \
  -plan .clairveil-payroll/payroll-plan.json \
  -tx-query .clairveil-payroll/tx-query.json \
  -interval 5s
```

`simulated` mode는 실제 proof 생성과 chain broadcast를 수행하지 않고, durable reservation state 위에서 proof ready, submitted, reconciled 상태 전이를 시뮬레이션합니다. 운영팀이 repo만으로 payroll workflow를 끝까지 확인할 때 사용합니다.

`live` mode는 long-running scheduler 표면입니다. 현재 CLI reference 구현은 `-tx-query` 파일을 tick마다 다시 읽어 `Submitted` 또는 `Unknown` 상태의 operation을 tx event/nullifier evidence로 reconcile합니다. proof 생성과 broadcast는 SDK의 `LiveOperationExecutor` 인터페이스에 production worker를 연결하거나, 외부 worker가 durable state를 `Submitted`까지 진행시키는 방식으로 붙입니다.

전체 demo는 아래처럼 실행합니다.

```bash
make reference-payroll-demo
```

대규모 payroll rehearsal simulation은 아래처럼 실행합니다.

```bash
make reference-payroll-rehearsal
```

Rehearsal은 0이 아닌 `RUN_LOCALNET`을 거절하는 legacy simulation입니다. Current V2 runtime용 checked-in live payroll target은 없습니다.

위 Make target은 repository-local demo와 legacy rehearsal simulation의 maintained runnable interface입니다.

## 11. Batch protocol compatibility

CLI가 생성하고 검사하는 V2 circuit set은 `privacy-note-v1-audit-field-v1`입니다. Note, disclosure, encrypted envelope는 canonical `privacy-fixed-v1`을 사용합니다. Command는 raw ciphertext나 legacy JSON plaintext가 아니라 typed envelope를 emit/consume합니다. `AssetRegistryV1`이 canonical denom과 32-byte asset ID resolve의 authoritative source입니다. Upgrade 시 fresh genesis를 사용하고 local wallet/scan/proof cache와 old development artifact를 삭제한 뒤 검토된 일치 artifact를 재사용하고 rescan합니다. Legacy decode나 in-place state migration은 없습니다.

Wallet scan state는 전체 cursor `(height, global_sequence, output_index)`로 정렬됩니다. 모든 spend path는 선택한 root와 정확히 같은 snapshot에서 얻어야 합니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical root/path query는 wallet interest를 노출하므로 privacy warning을 유지하고 중요하면 local 또는 privacy-preserving infrastructure를 우선합니다.

Chain core와 CLI는 이제 `transfer-batch-16x32` 및 단계형 companion command로 `BatchJoinSplit16x32`/`MsgBatchTransfer`를 구현합니다. 기존 `transfer-batch`는 계속 독립적인 native 2x2 transfer를 coordination하며 하나의 16x32 proof처럼 표현하면 안 됩니다. Live core public schema 순서는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`입니다.

`clairveil-proverd`는 role-aware lazy artifact registry와 circuit별 in-flight 1개, queued 4개의 admission default를 사용합니다. `-max-request-bytes` default는 `8388608`이고 0보다 커야 합니다. `0`은 invalid이며 limit을 비활성화하지 않습니다. Production `proverservice.Handler`를 노출합니다. Low-level raw transport handler도 hard cap은 있지만 service의 auth, gzip dual-limit, health/readiness, timeout policy는 없습니다. Automatic endpoint failover는 계속 비활성화합니다. Cancellation으로 caller가 중단되어도 in-process proof가 계속되며 slot을 유지할 수 있습니다. Hard cancellation이나 memory containment가 필요한 operator는 worker process를 isolate하고 terminate해야 합니다.
