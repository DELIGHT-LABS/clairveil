# Clairveil JS/TS SDK 핸드오프

이 문서는 JS/TS SDK 또는 웹월렛 개발자가 Clairveil privacy 기능을 구현할 때 필요한 계약을 한 곳에 모은 문서입니다. 목표는 “Go core가 무엇을 제공하고, JS SDK가 무엇을 구현해야 하는지”를 분명하게 나누는 것입니다.

## 1. JS SDK가 제공해야 하는 사용자 기능

웹월렛이 최종적으로 제공해야 하는 privacy 기능은 아래입니다.

- transparent account에서 shielded identity를 파생합니다.
- `clairs1...` full shielded address를 표시하고 복사할 수 있게 합니다.
- incoming viewing key로 chain event를 스캔해서 내 note를 복구합니다.
- deposit tx를 만들고 broadcast합니다.
- shielded transfer tx를 만들고 broadcast합니다.
- user selective disclosure를 public 또는 recipient-encrypted 방식으로 생성합니다.
- mandatory audit disclosure를 모든 transfer에 자동 포함합니다.
- disclosure payload를 decode하고 digest 검증 결과를 보여줍니다.
- direct withdraw와 relayed withdraw payload 흐름을 지원합니다.
- prover를 브라우저 내 wasm으로 붙일지, local/remote companion prover로 붙일지 선택할 수 있게 추상화합니다.

## 2. 네트워크 상수

Clairveil standalone reference chain 기준 상수는 아래입니다.

```text
Go module: github.com/DELIGHT-LABS/clairveil
daemon: clairveild
transparent account prefix: clair
shielded address prefix: clairs
reference denom: uclair
default local chain-id: clairveil-local-1
proto package: clairveil.privacy.v1
```

Downstream 체인이 denom, chain-id, gas policy를 바꾸면 JS SDK는 chain registry 또는 runtime config로 그 값을 받아야 합니다. `clairs` shielded address prefix와 proto package는 Clairveil privacy module 계약으로 유지하는 편이 가장 단순합니다.

## 3. Proto와 메시지

JS SDK는 아래 proto를 생성하거나 직접 type binding으로 표현해야 합니다.

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

Msg service는 아래 메시지를 사용합니다.

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

핵심 tx message는 아래입니다.

```text
MsgDeposit
MsgTransfer
MsgWithdraw
```

`MsgTransfer`에는 user disclosure와 audit disclosure 필드가 모두 있습니다. 최신 모델에서 audit disclosure는 선택 기능이 아니라 모든 shielded transfer에 포함되어야 하는 필수 기능입니다.

`MsgWithdraw`는 exact-match withdraw 메시지이며 output note 필드를 갖지 않습니다. JS/TS client는 legacy withdraw 필드인 `new_note_commitment`, `encrypted_note`를 모델링하지 말아야 하며, dummy output note 값을 보내지 않아야 합니다.

## 4. Query/API 계약

JS SDK provider가 우선 구현해야 하는 gRPC/HTTP query는 아래입니다.

```text
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/nullifier/{nullifier}
```

Go SDK 기준 provider contract는 아래 파일에 있습니다.

```text
x/privacy/client/sdk/provider/info.go
x/privacy/client/sdk/provider/query.go
x/privacy/client/sdk/provider/scan.go
x/privacy/client/sdk/provider/tx.go
```

웹월렛에서 최소로 필요한 provider 역할은 아래입니다.

- `TreeState`: 최신 root, leaf count, depth, max leaves, remaining leaves를 읽습니다.
- `CommitmentInfo`: commitment가 tree에 들어갔는지와 leaf index를 확인합니다.
- `MerklePath`: proving input에 필요한 path와 path helper를 가져옵니다.
- `PrivacyEvents`: deposit/transfer event feed를 스캔합니다.
- `AuditConfig`: chain에 설정된 master auditor pubkey를 가져옵니다.
- `DisclosureConfig`: user disclosure policy/mode와 payload version을 표시합니다.
- `CircuitConfig`: active circuit set과 artifact checksum 정보를 확인합니다.
- `CheckNullifier`: note spent 여부를 판단합니다.

## 5. Identity 파생

Clairveil wallet identity는 transparent keyring 위에 올라가는 single-root 모델입니다.

```text
transparent signer
  -> root signing message
  -> root seed
  -> spend key
  -> view key
  -> disclosure key
  -> full shielded address
```

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/identity/identity.go
x/privacy/client/sdk/identity/signer.go
x/privacy/types/address.go
```

JS SDK는 브라우저 지갑이 제공하는 transparent account address, public key, signature를 받아 root seed를 파생해야 합니다. root signing message는 chain tx signing과 별개인 domain-separated message이므로, 일반 transfer tx signature를 재사용하면 안 됩니다.

브라우저 provider가 맞춰야 하는 reference fixture는 아래입니다.

```text
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_wallet_readonly_reference_bundle.json
```

Fixture의 machine-readable 구조 계약은 아래 JSON Schema에 있습니다.

```text
docs/schemas/clairveil-js-wallet-contract.schema.json
```

JS/TS SDK는 최소한 아래 명령과 동일한 검증을 CI에 포함해야 합니다.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

이 검증은 fixture의 필수 필드, 버전, 주소 prefix, hash 길이, prover request/response shape를 고정합니다. Payload hash 재계산과 disclosure/prover 의미 검증은 schema만으로 충분하지 않으므로 validator 예제의 semantic check도 함께 유지해야 합니다.

## 6. Note scanning

웹월렛은 privacy event feed를 읽고 내 viewing key로 note를 복구해야 합니다.

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/scan/scan.go
x/privacy/client/sdk/scan/service.go
x/privacy/client/sdk/scan/wallet.go
```

scan 흐름은 아래처럼 구성합니다.

1. `PrivacyEvents` query로 deposit/transfer event를 가져옵니다.
2. deposit event의 `encrypted_note` 또는 transfer event의 `cipher_text_1`, `cipher_text_2`를 읽습니다.
3. wallet root seed와 viewing key로 복호화를 시도합니다.
4. 복호화에 성공한 note만 wallet DB에 저장합니다.
5. note commitment와 nullifier를 추적합니다.
6. `CheckNullifier` 또는 event scan 결과로 spent 여부를 갱신합니다.
7. rollback/reorg 대응을 위해 event height와 tx hash를 함께 저장합니다.

JS SDK의 wallet DB에는 최소 아래 필드가 필요합니다.

```text
commitment_hex
nullifier_hex
amount
asset_denom
asset_id_hex
randomness_hex
spend_pubkey_hex
view_pubkey_hex
height
tx_hash
spent
```

## 7. Deposit 구현

Deposit은 transparent balance를 privacy module account로 보내고 leaf 1개를 추가합니다.

CLI 대응 command는 아래입니다.

```bash
clairveild tx privacy deposit 10uclair --from alice --keyring-backend test
```

JS SDK는 아래를 수행해야 합니다.

- recipient wallet의 shielded identity에서 note를 만듭니다.
- note commitment를 계산합니다.
- encrypted note를 생성합니다.
- `MsgDeposit`을 만들어 일반 Cosmos tx로 sign/broadcast합니다.
- tx result에서 commitment와 encrypted note event를 확인합니다.

## 8. Transfer 구현

Transfer는 현재 최신 단일 모델만 사용합니다. legacy `transfer-v2`, `transfer-v3` command는 downstream/JS SDK 계약에 포함하지 않습니다.

CLI 대응 command는 아래입니다.

```bash
clairveild tx privacy transfer <recipient_clairs_address> 7uclair \
  --from alice \
  --keyring-backend test
```

JS SDK의 transfer builder는 아래 입력을 모읍니다.

- sender shielded identity
- recipient full shielded address
- spendable notes
- target amount and denom
- current tree root
- Merkle path for selected notes
- chain audit master pubkey
- optional user disclosure target pubkey
- user disclosure policy and mode

Transfer는 proof 생성 전 prepared payload를 만들고, prover가 proof를 돌려준 뒤 `MsgTransfer`를 완성하는 구조가 좋습니다.

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/transfer/prepare.go
x/privacy/client/sdk/transfer/payload.go
x/privacy/client/sdk/transfer/prove.go
x/privacy/client/sdk/transfer/build.go
x/privacy/client/sdk/transfer/service.go
```

중요한 제약은 아래입니다.

- transfer input note는 2개, output note는 2개입니다.
- output 0은 recipient note, output 1은 change note입니다.
- 모든 transfer는 audit disclosure를 포함해야 합니다.
- user disclosure는 `none`, `public`, `recipient-encrypted` mode를 지원합니다.
- supported policy는 `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to`입니다.
- transfer payload/proof version은 현재 `v1`입니다.
- disclosure payload version은 현재 query 기준 `v4`입니다.

## 9. Disclosure 구현

사용자 selective disclosure와 audit disclosure는 같은 payload 검증 모델을 사용하지만 plane이 다릅니다.

```text
user disclosure: sender가 선택한 정책과 전달 방식
audit disclosure: chain audit master key 대상으로 항상 생성
```

웹월렛 UI는 user disclosure에 대해 최소 아래 선택지를 제공하면 됩니다.

```text
mode: none | public | recipient-encrypted
policy: all-private | amount | to | amount-to | from | amount-from | from-to | amount-from-to
```

tx hash로 event를 조회해서 검증 report까지 보는 CLI 대응 command는 아래입니다.

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash <transfer_tx_hash> \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --report
```

JS SDK는 decode 결과에서 최소 아래를 표시해야 합니다.

- plane
- policy
- output index
- commitment hex
- digest hex
- verified
- disclosed fields
- amount
- asset denom
- from shielded address
- to shielded address

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/disclosure/disclosure.go
x/privacy/client/sdk/transfer/disclosure.go
```

## 10. Withdraw 구현

Withdraw는 현재 exact-match note를 요구합니다. 즉 `10uclair`를 withdraw하려면 spendable `10uclair` note가 있어야 합니다.

Direct withdraw CLI 대응 command는 아래입니다.

```bash
clairveild tx privacy withdraw 10uclair \
  --recipient "$(clairveild keys show bob -a --keyring-backend test)" \
  --from alice \
  --keyring-backend test
```

Relayed withdraw는 prepare/broadcast를 나눕니다.

```bash
clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(clairveild keys show bob -a --keyring-backend test)" \
  --from alice \
  --keyring-backend test \
  --out ./withdraw-payload.json

clairveild tx privacy relay-withdraw ./withdraw-payload.json \
  --from relayer \
  --keyring-backend test
```

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/withdraw/prepare.go
x/privacy/client/sdk/withdraw/prover_payload.go
x/privacy/client/sdk/withdraw/prove.go
x/privacy/client/sdk/withdraw/payload.go
x/privacy/client/sdk/withdraw/build.go
```

JS SDK가 사용자에게 분명히 보여줘야 하는 제약은 아래입니다.

- withdraw는 change note를 만들지 않습니다.
- `MsgWithdraw`에는 output note 필드가 없습니다. withdraw를 위해 dummy output commitment나 encrypted note를 만들지 마십시오.
- exact-match note가 없으면 먼저 shielded self-transfer로 원하는 크기의 note를 만들어야 합니다.
- relayed withdraw payload는 `chain_id`, `recipient`, `expires_at_unix`, `payload_hash`를 검증해야 합니다.
- relayer는 사용자의 shielded secret을 알 필요가 없습니다.

## 11. Prover 연결 모델

JS SDK는 proving을 직접 구현하기보다 prover adapter interface를 먼저 잡는 것이 좋습니다.

```text
Browser SDK
  -> build prepared payload
  -> ProverAdapter.proveTransfer / proveWithdraw
  -> proof response
  -> build MsgTransfer / MsgWithdraw
  -> sign and broadcast with the existing Cosmos/downstream wallet stack
```

현재 Go-side prover HTTP contract는 아래입니다.

```text
POST /v1/prover/transfer
POST /v1/prover/withdraw
Content-Type: application/json
request_version: v1
response_version: v1
```

error code는 아래입니다.

```text
invalid_request
method_not_allowed
not_found
unauthorized
unavailable
proof_failed
```

관련 fixture는 아래입니다.

```text
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
```

Prover가 local daemon이든 remote sidecar든 JS SDK 입장에서는 같은 adapter로 보이게 해야 합니다. 브라우저에서 직접 proving을 하는 wasm backend를 나중에 붙이더라도 같은 interface 뒤에 넣는 것이 좋습니다.

Remote prover를 붙일 때는 request timeout과 response validation을 client boundary에서 강제해야 합니다. 예제와 운영 profile은 아래에 있습니다.

```text
examples/js-sdk-prover-http-client
docs/clairveil-proverd-remote-production-profile-kr.md
```

## 12. JS SDK 구현 단위

권장 구현 순서는 아래입니다.

1. Proto/type generation을 붙입니다.
2. network constants와 chain config를 정의합니다.
3. identity derivation과 `clairs1...` address encode/decode를 구현합니다.
4. query provider를 구현합니다.
5. event scanner와 wallet note store를 구현합니다.
6. deposit tx builder를 구현합니다.
7. disclosure encode/decode/verify helper를 구현합니다.
8. transfer prepared payload builder를 구현합니다.
9. prover adapter와 HTTP prover client를 구현합니다.
10. `MsgTransfer` builder와 broadcast flow를 구현합니다.
11. withdraw prepared payload, direct withdraw, relayed withdraw를 구현합니다.
12. conformance fixture 기반 테스트와 local node e2e를 붙입니다.

## 13. 검증 기준

JS SDK handoff가 완료되었다고 보려면 아래가 가능해야 합니다.

- `privacy_wallet_golden_vectors.json`으로 root seed, spend/view/disclosure key, shielded address가 Go와 동일하게 나옵니다.
- `privacy_browser_signer_provider_contract.json`의 signing contract를 JS wallet provider가 재현합니다.
- local node에서 `show-address`에 해당하는 shielded address를 SDK가 직접 계산합니다.
- deposit 후 event scan으로 내 note를 찾습니다.
- transfer prepared payload의 hash가 Go fixture와 같은 방식으로 계산됩니다.
- prover HTTP contract에 맞춰 transfer/withdraw proof request와 response를 검증합니다.
- user disclosure와 audit disclosure를 decode하고 `verified=true`를 확인합니다.
- exact-match withdraw와 relayed withdraw payload 검증이 동작합니다.
- Clairveil repo의 `make privacy-e2e-smoke`와 같은 흐름을 JS SDK integration test가 따라갈 수 있습니다.

## 14. Go core 쪽에서 JS SDK가 믿어도 되는 것

현재 JS SDK가 안정 계약으로 삼아도 되는 항목은 아래입니다.

- `clairveil.privacy.v1` proto package
- `MsgDeposit`, `MsgTransfer`, `MsgWithdraw`
- gRPC/HTTP query path
- transparent prefix `clair`, shielded prefix `clairs`
- reference denom `uclair`
- full shielded address 기반 transfer UX
- mandatory audit disclosure
- user disclosure policy/mode label
- transfer proof request/response version `v1`
- withdraw proof request/response version `v1`
- prover HTTP path `/v1/prover/transfer`, `/v1/prover/withdraw`
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`

아직 JS SDK가 독자적으로 결정해야 하는 항목은 아래입니다.

- wallet local DB schema
- encrypted local storage 방식
- browser wallet provider API shape
- remote prover 인증 방식
- remote prover rate limit과 quota 정책
- web UI에서 disclosure 선택지를 어떻게 노출할지
- downstream chain의 실제 chain-id, denom, gas, fee policy

## 15. 개발자가 바로 시작할 때 보는 파일

JS SDK 개발자는 아래 파일부터 보면 됩니다.

```text
docs/clairveil-local-privacy-walkthrough-kr.md
docs/clairveil-downstream-cosmos-integration-guide-kr.md
docs/clairveil-proverd-remote-production-profile-kr.md
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
```

그리고 Go core 쪽 sanity check는 아래 명령으로 확인합니다.

```bash
make test
make privacy-e2e-smoke
```

## 16. Reference Consumer 예제

JS에서 audit disclosure key를 만들 때는 아래 예제를 봅니다.

```text
examples/audit-disclosure-keys
```

실행은 repo root에서 아래처럼 합니다.

```bash
npm --prefix examples/audit-disclosure-keys test
```

이 예제는 deterministic, random, privacy-root-signer 기반 audit disclosure keypair를 만들고 genesis에서 사용하는 compressed public key encoding을 검증합니다.

Clairveil repo에는 JS/TS SDK 개발자가 fixture consumer를 어떻게 시작하면 되는지 보여주는 작은 예제가 있습니다.

```text
examples/js-sdk-fixture-validator
```

실행은 repo root에서 아래처럼 합니다.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

이 예제는 node를 띄우지 않고 아래만 검증합니다.

- fixture 안의 wallet-facing 주소가 `clair1...`, `clairs1...` 기준인지 확인합니다.
- wallet-facing fixture 주소가 `clair1...` 또는 `clairs1...` prefix만 쓰는지 확인합니다.
- Go SDK와 같은 방식으로 transfer prepared payload hash를 계산합니다.
- Go SDK와 같은 방식으로 withdraw prover payload hash를 계산합니다.
- relayed withdraw final payload hash를 계산합니다.
- prover HTTP path가 `/v1/prover/transfer`, `/v1/prover/withdraw`인지 확인합니다.

이 예제는 production JS SDK가 아니라 첫 reference consumer입니다. 실제 JS SDK는 이 예제의 파일 구조를 그대로 따르기보다, 같은 hash contract와 fixture validation을 CI에 넣는 방식으로 가져가면 됩니다.

Remote prover HTTP client shape은 아래 예제를 봅니다.

```text
examples/js-sdk-prover-http-client
```

실행은 repo root에서 아래처럼 합니다.

```bash
npm --prefix examples/js-sdk-prover-http-client run demo
```

이 예제는 live `clairveil-proverd` 대신 fixture-backed mock prover를 띄워 아래를 검증합니다.

- `fetch` request에 finite timeout을 겁니다.
- bearer token을 `Authorization: Bearer ...`로 전달합니다.
- transfer/withdraw request와 response version이 `v1`인지 확인합니다.
- proof `payload_hash`가 prepared payload `payload_hash`와 같은지 확인합니다.
