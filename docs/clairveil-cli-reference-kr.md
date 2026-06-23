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

### selective disclosure flag

| flag                  | 값                                                                                             |
| --------------------- | ---------------------------------------------------------------------------------------------- |
| `--privacy-policy`    | `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to` |
| `--disclosure-mode`   | `none`, `public`, `recipient-encrypted`                                                        |
| `--disclosure-pubkey` | recipient-encrypted mode에서 받을 사람의 disclosure pubkey hex                                 |
| `--no-self-view`      | sender self-view disclosure를 생략                                                             |

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

Prepared payload JSON은 privacy-sensitive data입니다. production wallet은 암호화 저장과 만료/삭제 정책을 가져야 합니다.

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
| commitment info   | `/clairveil/privacy/v1/commitment/{commitment_hex}`  |
| events            | `/clairveil/privacy/v1/events`                       |
| Merkle path       | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| audit config      | `/clairveil/privacy/v1/audit_config`                 |
| disclosure config | `/clairveil/privacy/v1/disclosure_config`            |
| circuit config    | `/clairveil/privacy/v1/circuit_config`               |
| reserve           | `/clairveil/privacy/v1/reserve/{denom}`              |

## 10. Companion binary

### clairveil-setup

ZK artifact를 생성합니다.

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
