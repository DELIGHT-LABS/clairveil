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

`MsgDeposit`에는 `proof` 필드가 있습니다. Client는 `amount`, `asset_id`, `note_commitment`를 binding하는 `DepositCircuit` Groth16 proof를 만들거나 받아와야 하며, proof 없는 deposit은 현재 계약에 포함되지 않습니다.

`MsgTransfer`에는 `expires_at_unix`, user disclosure, audit disclosure, sender self-view disclosure 필드가 있습니다. Audit disclosure는 필수이고 sender self-view disclosure는 기본 포함되며 명시적 opt-out에서만 빠집니다. `creator`는 replaceable fee payer/relayer이며 owner intent에서 의도적으로 제외됩니다.

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
GET /clairveil/privacy/v1/reserve/{denom}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
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
- `ScanEvents`: cursor 기반 wallet projection으로 deposit/transfer output을 스캔합니다.
- `PrivacyEvents`: compatibility와 diagnostics용 raw deposit/transfer event feed를 읽습니다.
- `AuditConfig`: chain에 설정된 master auditor pubkey를 가져옵니다.
- `DisclosureConfig`: user disclosure policy/mode와 payload version을 표시합니다.
- `CircuitConfig`: consensus `CircuitSetIdentity`, active set, ordered VK hash, public-input schema hash를 읽습니다. Node-local manifest path나 checksum environment variable에서 consensus identity를 추론하지 않습니다.
- `Reserve`: denom별 privacy module-account balance와 기록된 deposit/withdraw 총량을 비교합니다.
- `CheckNullifiers`: 여러 note의 spent 상태를 한 번에 갱신합니다. 일반 batch에는 POST JSON body binding을 쓰고, 요청당 1000개로 chunk하며, GET은 작은 compatibility check에만 사용합니다.
- `CheckNullifier`: batch path를 쓸 수 없을 때 note 1개의 spent 여부를 판단합니다.

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

웹월렛은 wallet scan projection을 읽고 내 viewing key로 note를 복구해야 합니다.

Go SDK 기준 구현 위치는 아래입니다.

```text
x/privacy/client/sdk/scan/scan.go
x/privacy/client/sdk/scan/service.go
x/privacy/client/sdk/scan/wallet.go
```

권장 scan 흐름은 아래처럼 구성합니다.

1. `ScanEvents(after_height, after_sequence, limit, event_types)`로 deposit/transfer output projection을 가져옵니다.
2. deposit projection의 `encrypted_note`, 또는 transfer projection의 `cipher_text`, `commitment`, `output_index`, `view_tag`를 읽습니다.
3. Projection을 소비하기 전에 `scan_format_version`, `view_tag_version`을 검증합니다. 지원하지 않는 version이면 raw event path로 fallback하거나 cursor를 전진시키지 않고 중단합니다.
4. Transfer output은 local 2-byte view tag를 파생합니다. Ordered tag는 signed canonical transfer effect에 포함되지만 ownership 증거는 아니므로 안전한 기본값은 tag mismatch에서도 full trial decrypt를 수행하는 것입니다.
5. `view_tag`는 untrusted optimization으로만 취급합니다. 없거나 형식이 틀리면 full trial decrypt로 fallback합니다. Mismatch output을 건너뛰는 동작은 recovery 또는 forced rescan을 갖춘 명시적 fast mode 정책이어야 합니다.
6. wallet root seed와 viewing key로 복호화를 시도합니다. view-key 복호화가 실패하면 Go SDK와 호환되는 spend-key compatibility/recovery fallback을 유지합니다.
7. 복호화에 성공한 note만 wallet DB에 저장합니다.
8. note commitment와 nullifier를 추적합니다.
9. 가능하면 `CheckNullifiers`로 spent 상태를 batch 갱신하되 요청당 1000개로 chunk하고, 필요하면 `CheckNullifier`로 fallback합니다.
10. rollback/reorg 대응을 위해 event height, sequence, tx hash를 함께 저장합니다.

`ScanEvents`는 실제 적용된 `limit`, `scan_format_version=1`, `view_tag_version=1`을 반환합니다. `limit`은 scan cursor page budget으로 취급해야 합니다. 요청한 filter 밖의 event type만 page에 있으면 반환된 event 수가 `limit`보다 작거나 0이어도 `has_more=true`일 수 있습니다. 이 경우 client는 `next_height`, `next_sequence`로 cursor를 전진시키고 계속 스캔해야 합니다. Legacy `PrivacyEvents(after_height, page, limit, event_types)` query는 raw event inspection과 compatibility 용도로 유지되지만, 신규 web/mobile wallet은 offset pagination을 primary rescan UX로 삼지 않는 편이 좋습니다.

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
sequence
tx_hash
spent
last_scan_height
last_scan_sequence
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
- `DepositCircuit` proof를 local/WASM prover로 생성하거나 trusted prover adapter에서 받아옵니다.
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
- sender self-view disclosure는 기본 enabled이며, 명시적 opt-out일 때만 생략합니다.
- supported policy는 `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to`입니다.
- 새 transfer payload는 `v5`, transfer proof와 prover request/response는 `v2`를 사용합니다. 이전 transfer payload/proof/request version은 모두 거부하고 다시 생성해야 합니다.
- 두 output, ordered ciphertext/view tag, user/audit/self-view envelope, 독립 disclosure blinding, chain ID, absolute expiry를 먼저 확정합니다. 그 다음 canonical transfer effect와 `TransferIntentV2`를 계산하고 정확히 하나의 `owner_signature_hex`를 만듭니다. Per-input note-hash signature는 없습니다.
- Canonical binary effect는 고정 field 순서와 variable byte의 `u32be(length) || bytes` encoding을 사용합니다. Format version, root, ordered nullifier/commitment/ciphertext/view tag, 모든 disclosure field, expiry를 포함하고 proof, `creator`, fee/gas/memo/sequence/tx signature, digest 자신은 제외합니다. Keeper가 `MsgTransfer`에서 다시 계산합니다.
- 최종 `MsgTransfer`는 `new_commitments`, `cipher_texts`와 순서가 맞는 정확히 2개의 `view_tags`를 포함해야 합니다.
- Disclosure plaintext/query version은 `privacy-fixed-v1`입니다. Enabled user disclosure와 full audit/self-view disclosure는 서로 독립적인 fresh CSPRNG blinding을 사용합니다. 복호화 후 blinding을 복원해 digest를 재계산해야 하며 decrypt 성공만으로 verified 처리하면 안 됩니다.
- Recipient output `0`에 `DBS-01`(`policy != 0 => user_blinding != output_randomness`), `DBS-02`(`full_blinding != output_randomness`), `DBS-03`(`full_blinding != user_blinding`)를 강제합니다. All-private는 user blinding을 zero로 canonicalize하고 `DBS-01`만 gate off합니다. Output `1`은 disclosure witness가 없는 active change note이지 disabled slot이 아닙니다.
- Prepared payload를 prover에 보내기 전과 owner signature를 release하기 전에 semantic validator를 실행합니다. `privacy_disclosure_blinding_v1_contract.json`의 stable secret-free code를 사용하고 error/telemetry에 randomness/blinding 값을 포함하지 않습니다.
- `expires_at_unix`는 absolute 값이고 chain은 `block_time >= expires_at_unix`에서 거부합니다.

정확한 `JoinSplitCircuit` public-input 순서는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `Nullifier0`, `Nullifier1`, `Commitment0`, `Commitment1`, `UserPrivacyPolicy`, `UserDisclosureDigest`, `FullDisclosureDigest`, `PayloadDigestHi`, `PayloadDigestLo`입니다. Field를 sort하거나 rename하면 안 됩니다. SHA-256 chain/payload digest는 field reduction 없이 big-endian 128-bit limb 두 개로 나눕니다. Chain domain input은 `"clairveil.chain-domain.v1"`, length-prefixed `chain_id`, length-prefixed `circuit_set_id`(`privacy-note-v1`) 순서입니다.

Go 2x2 boundary는 이제 canonical policy, recipient output randomness, user/full blinding과 final effect를 전달하는 `JoinSplitOwnerIntentSigningRequestV1`을 사용합니다. `ValidateJoinSplitOwnerIntentSigningRequestV1`은 domain, payload digest와 final intent를 재계산한 뒤 `DBS-01..03`을 적용하고 `SignValidatedJoinSplitOwnerIntentV1`은 invalid request에서 callback을 호출하지 않습니다. Downstream structured wallet signer도 이 fail-before-sign contract를 유지해야 합니다. Transfer payload `v5`, proof/request/response `v2`, NoteV1, fixed payload encoding, disclosure digest 공식, 13-input schema 변경 없이 `S4-B02` implementation은 해결됐으며 새 JoinSplit VK identity는 `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`입니다.

Bulk payroll 또는 다른 대량 전송 client에서 쓰는 note reservation은 on-chain protocol이 아니라 client/control-plane layer 계약입니다. Go reference implementation과 fixture는 아래에 있습니다.

```text
x/privacy/client/sdk/reservation/
x/privacy/client/sdk/payroll/
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
docs/clairveil-note-reservation-design.md
docs/clairveil-note-reservation-design-kr.md
```

Proof 생성 전에 note를 예약하는 JS/TS client는 fixture에 고정된 reservation status 이름, active reservation 정의, atomic batch-reserve 규칙, compare-and-set 상태 전이, lease token 규칙, HMAC lookup-key test vector, operation 성공 증거 모델을 맞춰야 합니다. Nullifier spent는 note가 소비되었다는 증거이지만, payroll/payment operation을 성공 처리하려면 tx evidence가 expected output commitment, audit disclosure digest, recipient hash, amount, denom, item index와도 일치해야 합니다. fixture의 `expected_disclosure_digest`는 user disclosure나 sender self-view digest가 아니라 audit disclosure digest를 뜻합니다.

## 9. Disclosure 구현

사용자 selective disclosure, audit disclosure, sender self-view disclosure는 같은 payload 검증 모델을 사용하지만 plane과 delivery 의미가 다릅니다.

```text
user disclosure: sender가 선택한 정책과 전달 방식
audit disclosure: chain audit master key 대상으로 항상 생성
self-view disclosure: sender 자신의 disclosure key 대상으로 기본 생성
```

Self-view disclosure는 sender가 나중에 자신이 보낸 transfer의 amount/from/to를 볼 수 있게 하는 encrypted payload입니다. On-chain event에는 `self_view_disclosure_digest`와 `self_view_disclosure_payload`만 들어가며, sender의 static disclosure public key는 노출하지 않습니다. JS SDK는 sender disclosure private key로 self-view payload를 trial decrypt하고, payload 안의 digest와 on-chain digest를 검증해야 합니다.

Audit/self-view plaintext는 같은 fresh full-disclosure blinding을 운반하고 `FullDisclosureDigest`에 대해 검증합니다. Optional user disclosure는 별도의 fresh blinding을 사용합니다. Low-entropy plaintext에서 blinding을 derive하거나 transfer/plane 사이에 재사용하면 안 됩니다.

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

Sender self-view를 확인하는 CLI 대응 command는 아래입니다.

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash <transfer_tx_hash> \
  --disclosure-plane self-view \
  --from sender \
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

Client 관점의 relayed withdraw 책임 분리는 아래와 같습니다.

- user client는 withdraw proof response를 받아 최종 `PreparedWithdrawPayload` JSON을 만듭니다.
- user client와 relayer 사이의 전달 방식은 제품별 계약입니다. HTTP, QR, deep link, file handoff 모두 가능합니다.
- payload를 relayer에게 넘긴 뒤에는 `expires_at_unix` 전까지 여전히 제출될 수 있습니다. local cancel, UI dismiss, local reservation release는 이미 만들어진 payload를 무효화하지 않습니다.
- relayer client/server는 payload의 `payload_hash`, `chain_id`, `recipient`, `expires_at_unix`를 검증하고, 자기 주소를 `MsgWithdraw.creator`로 넣어 sign/broadcast합니다.
- withdraw 대상 투명 주소는 relayer 주소가 아니라 payload의 `recipient`입니다.
- 이 repo는 production relay HTTP endpoint를 제공하지 않습니다. 대신 final payload에서 relayer 제출 메시지로 변환되는 계약을 `x/privacy/client/sdk/conformance/testdata/privacy_relay_withdraw_contract.json` fixture로 고정합니다.

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
- Withdraw prover payload, proof, final payload, prover request/response, relay schema/handoff는 모두 `v2`이며 legacy file은 다시 생성해야 합니다.
- `spend_intent_signature_hex`는 `SpendIntentV2`를 인증합니다. Recipient는 정확한 raw decoded address byte를 `SHA-256("clairveil.withdraw-recipient.v1" || u32be(len(bytes)) || bytes)`로 hash하고 field reduction 없이 big-endian 128-bit limb 두 개로 나눕니다. Byte를 field element로 변환하거나 leading zero를 제거하면 안 됩니다.
- 정확한 spend public-input 순서는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `Nullifier`, `Amount`, `RecipientDigestHi`, `RecipientDigestLo`, `AssetID`입니다.
- `creator`는 relayer가 바꿀 수 있지만 `recipient`, chain, expiry는 proof-bound입니다. `block_time >= expires_at_unix`이면 제출이 실패합니다.
- relayed withdraw payload는 handoff 후 expiry 전까지 제출 가능하므로, 지갑은 local cancel을 note 재사용 가능 증거로 취급하면 안 됩니다.
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
request_version: v2
response_version: v2
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

Final output을 더는 바꿀 수 없더라도 prepared prover payload는 authority-equivalent privacy-sensitive witness data입니다. 불필요하게 log/persist하지 않습니다. Prover pool은 기본 single endpoint/no automatic failover여야 합니다. 같은 witness를 다른 endpoint로 보내는 것은 추가 privacy boundary를 명시한 user/product-policy opt-in 후에만 허용하고 retry는 같은 endpoint에 할 수 있습니다.

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
6. deposit proof와 tx builder를 구현합니다.
7. disclosure encode/decode/verify helper를 구현합니다.
8. transfer prepared payload builder를 구현합니다.
9. prover adapter와 HTTP prover client를 구현합니다.
10. `MsgTransfer` builder와 broadcast flow를 구현합니다.
11. withdraw prepared payload, direct withdraw, relayed withdraw를 구현합니다.
12. bulk payroll client라면 `privacy_note_reservation_contract.json` 기준 note reservation과 operation 상태 추적을 구현합니다.
13. conformance fixture 기반 테스트와 local node e2e를 붙입니다.

## 13. 검증 기준

JS SDK handoff가 완료되었다고 보려면 아래가 가능해야 합니다.

- `privacy_wallet_golden_vectors.json`으로 root seed, spend/view/disclosure key, shielded address가 Go와 동일하게 나옵니다.
- `privacy_browser_signer_provider_contract.json`의 signing contract를 JS wallet provider가 재현합니다.
- local node에서 `show-address`에 해당하는 shielded address를 SDK가 직접 계산합니다.
- deposit 후 event scan으로 내 note를 찾습니다.
- transfer prepared payload의 hash가 Go fixture와 같은 방식으로 계산됩니다.
- `privacy_disclosure_blinding_v1_contract.json`의 positive/sentinel/negative vector가 같은 `DBS_*` result code를 만들고 structured signing이 invalid vector를 signature release 전에 모두 거부합니다.
- prover HTTP contract에 맞춰 transfer/withdraw proof request와 response를 검증합니다.
- bulk payroll client가 `privacy_note_reservation_contract.json`의 reservation 전이와 operation 성공 규칙을 재현합니다.
- user disclosure, audit disclosure, sender self-view disclosure를 decode하고 `verified=true`를 확인합니다.
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
- `MsgDeposit` deposit proof requirement
- transfer payload `v5`, transfer proof/request/response `v2`
- withdraw prover/final payload와 proof/request/response `v2`
- disclosure plaintext/query version `privacy-fixed-v1`
- active circuit set `privacy-note-v1`, consensus `CircuitSetIdentity` schema `v1`, manifest schema `v2`
- prover HTTP path `/v1/prover/transfer`, `/v1/prover/withdraw`
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`
- `DISCLOSURE-BLINDING-SEPARATION` V1 semantics/error code, 단 production 2x2 circuit/artifact enforcement는 Session 3A pending
- `privacy_note_reservation_contract.json`의 note reservation status와 operation evidence contract

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
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
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
- relay withdraw handoff fixture에서 relayer 주소가 `MsgWithdraw.creator`로, payload recipient가 `MsgWithdraw.recipient`로 유지되는지 확인합니다.
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
- transfer/withdraw request, response, proof version이 `v2`인지 확인합니다.
- proof `payload_hash`가 prepared payload `payload_hash`와 같은지 확인합니다.

## 17. Session 3B Reference Addendum

Repository에는 production core와 reference Go batch builder, bounded proof adapter/HTTP route, decrypting typed scanner, durable payroll integration, staged batch CLI가 포함됩니다. 이 JS SDK handoff에는 해당 contract의 downstream JS/TS 구현이 여전히 필요합니다. 기존 `transfer-batch` helper는 native 2x2 message를 orchestration하며 one-proof `MsgBatchTransfer`와 구분됩니다.

새 SDK 작업에는 아래 breaking rule을 normative하게 적용합니다.

- Active circuit set은 `privacy-note-v1`입니다. Note, disclosure, encrypted-envelope binary data는 `privacy-fixed-v1`을 사용합니다. `NotePlaintextV1`은 정확히 350 bytes, `DisclosurePlaintextV1`은 정확히 392 bytes이며 모든 encrypted payload에는 canonical 20-byte envelope header와 정확한 kind가 있어야 합니다. Raw ciphertext, JSON plaintext, trailing bytes, cross-kind decoding은 거부합니다.
- 이 전환에는 fresh genesis가 필요합니다. Cached note, scan cursor, prepared/proof job, circuit identity metadata, old development artifact를 삭제한 뒤 artifact를 다시 생성하고 rescan합니다. 이전 계약을 위한 compatibility decode나 in-place state migration은 없습니다.
- `AssetRegistryV1`이 canonical denom과 32-byte `asset_id`의 authoritative one-to-one mapping입니다. Client는 검증을 위해 ID를 derive할 수 있지만 ID를 해석하거나 hash해서 denom을 임의로 만들면 안 됩니다. Registry query로 resolve하고 mismatch에서는 fail closed합니다.
- Wallet sync는 unified `privacy-scan-v2` projection과 lexicographic cursor `(height, global_sequence, output_index)`를 사용합니다. 전체 cursor를 atomically 저장합니다. 모든 Merkle path는 선택한 root와 정확히 일치하는 snapshot에서 가져와야 하며 current path와 older root를 섞으면 invalid입니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical lookup은 wallet timing과 관심 대상을 노출하므로 privacy warning을 유지하고 product threat model이 요구하면 privacy-preserving infrastructure를 사용합니다.
- Production `BatchJoinSplit16x32` public-input 순서는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`입니다. Go reference path는 완료됐지만 downstream JS/TS support는 conformance fixture와 localnet behavior를 독립 재현하기 전까지 feature-gated 상태입니다.
- Downstream JS/TS batch builder는 reference `CanonicalBatchTransferPayloadBytesV1`을 exact하게 재현해야 합니다. Format `1`, `u32be` vector count, 모든 byte field의 `u32be(length) || bytes`, proto 선언 순서의 output field, audit ID/epoch/target, expiry 순서입니다. SHA-256 domain `clairveil.batch-transfer-payload.v1`을 non-reduced 128-bit limb 둘로 나눕니다. `creator`와 `proof`만 제외하며 protobuf marshal, JSON, sorted-field 대안을 만들면 안 됩니다.
- Artifact loading은 role-aware입니다. Validator는 exact consensus identity를 검증한 뒤 필요한 VK만 load하고 prover는 선택한 R1CS/PK pair만 lazy load합니다. Reference prover admission default는 circuit별 in-flight 1개, queued 4개, positive 8 MiB request limit입니다. 0은 invalid이며 body limit을 비활성화하지 않습니다.
- `provertransport.HTTPHandler`를 직접 노출하지 말고 bounded `proverservice.Handler` wrapper를 사용합니다. Prover request에는 automatic endpoint failover가 없습니다. Cancellation은 대기를 중단하고 response를 버리지만 in-process proving은 solver가 반환할 때까지 계속되면서 admission capacity를 점유할 수 있습니다. Hard cancellation 또는 memory containment가 필요한 production operator는 이 reference 구현 밖에서 process isolation과 termination을 추가해야 합니다.
