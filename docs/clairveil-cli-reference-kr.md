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

recipient-encrypted disclosure, sender self-view disclosure, audit disclosure에 사용하는 public key를 표시합니다.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

이 값은 genesis audit master pubkey 설정, user disclosure recipient 설정, sender self-view disclosure 복호화 키 확인에 사용됩니다.

## 3. Deposit

transparent coin을 shielded note로 넣습니다.

```bash
clairveild tx privacy deposit 10uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --expires-in 1800 \
  --gas 2500000 \
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
```

다른 query는 gRPC/HTTP gateway와 generated client로 사용할 수 있습니다.

| Query             | HTTP path                                            |
| ----------------- | ---------------------------------------------------- |
| tree state        | `/clairveil/privacy/v1/tree_state`                   |
| nullifier         | `/clairveil/privacy/v1/nullifier/{nullifier}`        |
| batch nullifiers (GET) | `/clairveil/privacy/v1/nullifiers`              |
| batch nullifiers (POST) | `/clairveil/privacy/v1/nullifiers`             |
| commitment info   | `/clairveil/privacy/v1/commitment/{commitment_hex}`  |
| events            | `/clairveil/privacy/v1/events`                       |
| scan events       | `/clairveil/privacy/v1/scan_events`                  |
| Merkle path       | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| audit config      | `/clairveil/privacy/v1/audit_config`                 |
| disclosure config | `/clairveil/privacy/v1/disclosure_config`            |
| circuit config    | `/clairveil/privacy/v1/circuit_config`               |
| reserve           | `/clairveil/privacy/v1/reserve/{denom}`              |

## 10. Companion binary

### clairveil-setup

Active set `privacy-note-v1`, manifest schema `v2`의 development ZK artifact를 생성합니다. Generated R1CS/PK/VK binary는 source artifact가 아니며 이 command는 formal trusted setup ceremony가 아닙니다.

```bash
clairveil-setup --out artifacts/privacy
clairveil-setup --out artifacts/privacy --overwrite
```

### clairveil-proverd

Companion prover HTTP service를 실행합니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=artifacts/privacy
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd \
  -listen 127.0.0.1:8080 \
  -read-header-timeout 5s \
  -read-timeout 30s \
  -write-timeout 0s \
  -idle-timeout 2m \
  -max-request-bytes 8388608
```

Remote production profile은 [clairveil-proverd-remote-production-profile-kr.md](clairveil-proverd-remote-production-profile-kr.md)를 따릅니다.

Validator startup은 local VK/public-input schema hash를 consensus `CircuitSetIdentity` schema `v1`과 비교하며 checksum env로 override할 수 없습니다. Validator는 VK만 필요하고 `clairveil-proverd`는 proof 생성 시 R1CS/PK를 lazy load합니다. Prover endpoint failover는 기본 off이며 explicit privacy opt-in이 필요합니다.

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

`prepare-notes`와 `plan`은 `-store-dir .clairveil-payroll`을 받아 file-backed reference artifact store에도 결과를 저장할 수 있습니다. `run`, `scan-evidence`, `reconcile`, `settle-transfer-batch`는 durable reservation state 파일을 사용합니다. 상세 workflow는 [clairveil-reference-payroll-product-kr.md](clairveil-reference-payroll-product-kr.md)를 따릅니다.

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

실제 localnet에서 payroll transfer-batch까지 실행하는 live 튜토리얼은 아래처럼 실행합니다.

```bash
make reference-payroll-live-localnet
```

대규모 payroll rehearsal simulation은 아래처럼 실행합니다.

```bash
make reference-payroll-rehearsal
```

live localnet 자세한 단계는 [clairveil-reference-payroll-live-localnet-tutorial-kr.md](clairveil-reference-payroll-live-localnet-tutorial-kr.md)를 따릅니다. rehearsal 자세한 단계는 [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md)를 따릅니다.

## 11. Session 2 Foundation Compatibility

CLI가 생성하고 검사하는 active circuit set은 `privacy-note-v1`입니다. Note, disclosure, encrypted envelope는 canonical `privacy-fixed-v1`을 사용합니다. Command는 raw ciphertext나 legacy JSON plaintext가 아니라 typed envelope를 emit/consume합니다. `AssetRegistryV1`이 canonical denom과 32-byte asset ID resolve의 authoritative source입니다. Upgrade 시 fresh genesis를 사용하고 local wallet/scan/proof cache와 old development artifact를 삭제한 뒤 artifact를 다시 생성하고 rescan합니다. Legacy decode나 in-place state migration은 없습니다.

Wallet scan state는 전체 cursor `(height, global_sequence, output_index)`로 정렬됩니다. 모든 spend path는 선택한 root와 정확히 같은 snapshot에서 얻어야 합니다. Current-root incremental path에는 1,048,576-leaf cap이 없습니다. Non-current historical path는 persisted root/count/height metadata를 사용하지만 node를 bounded rebuild하므로 1,048,576 leaves로 제한됩니다. Cap을 넘으면 current root 또는 trusted local historical index를 사용합니다. Remote historical root/path query는 wallet interest를 노출하므로 privacy warning을 유지하고 중요하면 local 또는 privacy-preserving infrastructure를 우선합니다.

`BatchJoinSplit16x32`는 Session 2 feasibility prototype으로 남아 있습니다. 이 reference의 어떤 CLI command도 production 16x32 message를 submit하지 않습니다. 특히 `transfer-batch`는 여전히 현재 native 2x2 transfer를 coordination합니다. Future public schema는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo` 순서로 reserve되며 live circuit이나 transaction으로 취급하면 안 됩니다.

`clairveil-proverd`는 role-aware lazy artifact registry와 circuit별 in-flight 1개, queued 4개의 admission default를 사용합니다. `-max-request-bytes` default는 `8388608`이고 0보다 커야 합니다. `0`은 invalid이며 limit을 비활성화하지 않습니다. Bounded `proverservice.Handler`만 노출하고 raw transport handler는 절대 직접 노출하지 않습니다. Automatic endpoint failover는 계속 비활성화합니다. Cancellation으로 caller가 중단되어도 in-process proof가 계속되며 slot을 유지할 수 있습니다. Hard cancellation이나 memory containment가 필요한 operator는 worker process를 isolate하고 terminate해야 합니다.
