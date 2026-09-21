# Clairveil JS/TS SDK 핸드오프

이 문서는 JS/TS SDK 또는 웹월렛 개발자가 Clairveil privacy 기능을 구현할 때 필요한 계약을 한 곳에 모은 문서입니다. 목표는 “Go core가 무엇을 제공하고, JS SDK가 무엇을 구현해야 하는지”를 분명하게 나누는 것입니다.

Current runtime integration은 V2 asset message, V2 audit configuration/key query, 단일 `/v2/prover/audit-field` route를 사용합니다. 뒤에서 보존하는 상세 NoteV1 payload, disclosure mode, staged batch command, `/v1` prover path는 legacy fixture guidance일 뿐 current V2 transaction으로 보내면 안 됩니다.

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
chain-id: 필수 audit configuration에서 제공
current message/audit-query package: clairveil.privacy.v2
wallet scan/tree query package: clairveil.privacy.v1
```

Downstream 체인이 denom, chain-id, gas policy를 바꾸면 JS SDK는 chain registry 또는 runtime config로 그 값을 받아야 합니다. `clairs` shielded address prefix와 proto package는 Clairveil privacy module 계약으로 유지하는 편이 가장 단순합니다.

## 3. Proto와 메시지

JS SDK는 아래 proto를 생성하거나 직접 type binding으로 표현해야 합니다.

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
```

Current Msg service는 아래 message를 사용합니다.

```text
/clairveil.privacy.v2.Msg/Deposit
/clairveil.privacy.v2.Msg/Transfer
/clairveil.privacy.v2.Msg/Withdraw
/clairveil.privacy.v2.Msg/BatchTransfer
/clairveil.privacy.v2.Msg/ScheduleAuditKeyEpoch
/clairveil.privacy.v2.Msg/CancelPendingAuditEpoch
/clairveil.privacy.v2.Msg/SetPrivacyHalt
```

핵심 tx message는 아래입니다.

```text
MsgDeposit
MsgTransfer
MsgWithdraw
MsgBatchTransfer
```

V2 asset message 네 개는 모두 mandatory `AuditAuthorization {key_id, epoch, envelope}`를 가집니다. Deposit은 `creator`, amount, `OutputEffect`, proof, expiry를 포함합니다. Withdraw는 creator, amount, recipient, root, nullifier, proof, expiry, audit authorization을 포함합니다. Transfer와 batch는 root, ordered nullifier/output effect, proof, expiry, audit authorization을 포함합니다. 아래 보존 V1 field 설명을 복사하지 말고 compiled V2 proto에서 binding을 생성하세요.

Audit runtime에서는 V1 Msg service가 등록되지 않고 legacy 호출은 `legacy privacy service is disabled`로 실패합니다. Governance는 V2 management message 세 개만 사용할 수 있으며 asset message 네 개의 governance 실행은 차단됩니다.

## 4. Query/API 계약

JS SDK provider가 우선 구현해야 하는 gRPC/HTTP query는 아래입니다.

```text
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
POST /clairveil/privacy/v1/commitment_paths_at_root
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom=**}
GET /clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}
GET /clairveil/privacy/v1/assets/by_id/{asset_id_hex}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
POST /clairveil/privacy/v1/privacy_scan
GET /clairveil/privacy/v2/audit/configuration
GET /clairveil/privacy/v2/audit/key_schedule
GET /clairveil/privacy/v2/audit/keys/{epoch}
```

Go SDK 기준 provider contract는 아래 파일에 있습니다.

```text
x/privacy/client/sdk/provider/info.go
x/privacy/client/sdk/provider/query.go
x/privacy/client/sdk/provider/scan.go
x/privacy/client/sdk/provider/typed_scan.go
x/privacy/client/sdk/provider/tx.go
```

웹월렛에서 최소로 필요한 provider 역할은 아래입니다.

- `TreeState`: 최신 root, leaf count, depth, max leaves, remaining leaves를 읽습니다.
- `CommitmentInfo`: commitment가 tree에 들어갔는지와 leaf index를 확인합니다.
- `MerklePath`: proving input에 필요한 path와 path helper를 가져옵니다.
- `CommitmentPathsAtRoot`: batch proving을 위해 하나의 root/height snapshot에 대한 path를 최대 16개 가져옵니다. Grouped lookup은 note-linkage privacy boundary로 취급합니다.
- `AssetRegistry`: canonical denom과 32-byte asset ID를 양방향으로 resolve합니다.
- `PrivacyScanV2`: primary wallet-sync 경로입니다. `PrivacyScan`을 호출하여 global `(height, global_sequence, output_index)` cursor로 typed deposit, JoinSplit2x2 transfer, batch-transfer output을 읽습니다.
- `ScanEvents`: deposit과 JoinSplit2x2 transfer만을 위한 legacy compatibility projection입니다. batch를 지원하지 않으며 primary wallet-sync API가 아닙니다.
- `PrivacyEvents`: compatibility와 diagnostics를 위한 raw legacy event inspection API이며 wallet-sync projection이 아닙니다.
- `AuditConfiguration`, `AuditKeySchedule`, `AuditKey`: V2에서 public runtime configuration, active/pending epoch, cancellation, historical public key를 가져옵니다. V1 `audit_config` contract는 legacy compatibility 자료이며 current configuration source가 아닙니다.
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

권장 scan 흐름은 `ScanEvents`나 ABCI event search가 아니라 `PrivacyScanV2`(typed `PrivacyScan` query)입니다.

1. 완전한 lexicographic cursor `(height, global_sequence, output_index)`를 저장하고 전송합니다. `global_sequence`는 privacy operation 전체에 대해 chain-global이며 transaction-local 또는 height별 sequence가 아닙니다.
2. 모든 event type을 요청하고 typed deposit, JoinSplit2x2 transfer, batch-transfer output을 소비합니다. deposit에서는 `encrypted_note`를, transfer/batch에서는 `ciphertext`, `commitment`, `output_index`, `view_tag`를 읽습니다.
3. response, 모든 summary, 모든 output의 `scan_schema_version`이 지원되는 값이 아니면 fail closed합니다. Strict cursor order, 한 event 안의 contiguous output index, output-to-summary identity/framing, response limit을 검증합니다.
4. zero-output withdrawal을 포함하여 summary를 event boundary로 취급합니다. Multi-output event는 `output_count`개의 output을 모두 모으고 그 event가 `has_more=false`에 도달하기 전에는 complete 처리하지 않습니다. `next_cursor` advance마다 summary를 검증하여 output-bearing event를 건너뛸 수 없게 합니다.
5. `PrivacyScanV2` query, validation, decoding failure는 terminal입니다. Batch ciphertext가 조용히 누락될 수 있으므로 `ScanEvents`, `PrivacyEvents`, ABCI transaction/event search로 fallback하지 않습니다. Legacy fallback은 typed capability 자체를 구현하지 않은 환경에서만 허용됩니다.
6. Transfer와 batch output에서는 local 2-byte view tag를 파생합니다. Ordered tag는 signed이지만 ownership 증거가 아닙니다. `view_tag`는 untrusted optimization이며, 안전한 기본값은 tag mismatch, missing tag, malformed tag에서도 full trial decrypt를 수행하는 것입니다. Mismatch를 건너뛰려면 recovery 또는 forced-rescan을 갖춘 명시적 fast-mode policy가 필요합니다.
7. wallet root seed와 viewing key로 복호화를 시도하고 Go-compatible spend-key compatibility/recovery attempt를 유지합니다. 복호화에 성공한 note만 commitment와 nullifier를 함께 저장합니다.
8. `CheckNullifiers`로 spent 상태를 갱신하고 요청당 1000개로 chunk합니다. Batch path를 쓸 수 없을 때만 `CheckNullifier`를 사용합니다.
9. note update와 함께 결과 full cursor, output height, global sequence, output index, tx hash를 atomically 저장하여 rollback/reorg에 대응합니다.

`ScanEvents(after_height, after_sequence, limit, event_types)`는 deposit과 JoinSplit2x2 compatibility만을 위한 legacy cursor projection입니다. `scan_format_version=1`, `view_tag_version=1`을 검증해야 합니다. 실제 적용된 `limit`은 page budget이므로 filter된 page는 반환 event가 더 적거나 0이어도 `has_more=true`일 수 있으며, 이때 `next_height`/`next_sequence`로 전진합니다. 이는 primary 경로도 typed-query failure 뒤의 fallback도 될 수 없습니다. `PrivacyEvents(after_height, page, limit, event_types)`는 compatibility와 diagnostics를 위한 raw legacy event-inspection API입니다. Offset pagination을 primary rescan UX로 만들면 안 됩니다.

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
global_sequence
output_index
tx_hash
spent
last_scan_height
last_scan_sequence
last_scan_output_index
```

`global_sequence`과 `last_scan_sequence`은 모두 chain-global privacy-operation sequence를 뜻합니다. `last_scan_height`, `last_scan_sequence`, `last_scan_output_index`는 하나의 atomic `PrivacyScanV2` cursor이므로 일부 prefix만 저장하면 안 됩니다.

## 7. Deposit 구현

이전 deposit SDK filename과 NoteV1 예시는 보존 fixture handoff이며 current wire/prover specification이 아닙니다. Current client는 V2 output effect를 만들고 nonce/initial height/active audit epoch/circuit identity를 query한 뒤 공통 [audit-field route](clairveil-proverd-http-api-kr.md#현재-route)를 사용합니다.

Current client는 V2 `OutputEffect`와 `AuditAuthorization`을 만들고 local 또는 `/v2/prover/audit-field`에서 audit-field proof를 얻습니다. 이어 repeated response binding, exact artifact identity, final PI23을 local verify한 뒤 `clairveil.privacy.v2.MsgDeposit`을 broadcast합니다. [Current HTTP contract](clairveil-proverd-http-api-kr.md#현재-route)와 compiled V2 proto를 authority로 사용하며 기존 per-deposit route와 client-field 분리는 보존 fixture일 뿐입니다.

## 8. Legacy NoteV1 transfer reference

이 section은 보존 fixture가 사용하는 V1 prepared-payload와 inner-relation detail을 남깁니다. Current transaction으로 encode하면 안 됩니다. Current V2 client는 `clairveil.privacy.v2.MsgTransfer`, mandatory `AuditAuthorization`, `/v2/prover/audit-field`를 사용합니다.

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
- active audit epoch public key(legacy fixture 비교 전용)
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

Go 2x2 boundary는 이제 input/output NoteV1 두 개씩, canonical policy, sender public-key projection, recipient output randomness, user/full blinding과 final effect를 전달하는 `JoinSplitOwnerIntentSigningRequestV1`을 사용합니다. `ValidateJoinSplitOwnerIntentSigningRequestV1`은 ordered nullifier, commitment 두 개, value conservation, change ownership, user/audit disclosure digest를 재계산해 final effect와 대조하고 domain, payload digest, final intent, `DBS-01..03`도 검증합니다. `SignValidatedJoinSplitOwnerIntentV1`은 invalid request, redirected change, decoupled projection에서 callback을 호출하지 않습니다. Downstream structured wallet signer도 이 fail-before-sign contract를 유지해야 합니다. Transfer payload `v5`, proof/request/response `v2`, NoteV1, fixed payload encoding, disclosure digest 공식, 13-input schema 변경 없이 `DISCLOSURE-BLINDING-SEPARATION` 구현을 완료했으며 새 JoinSplit VK identity는 `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`입니다.

Bulk payroll 또는 다른 대량 전송 client에서 쓰는 note reservation은 on-chain protocol이 아니라 client/control-plane layer 계약입니다. Go reference implementation과 fixture는 아래에 있습니다.

```text
x/privacy/client/sdk/reservation/
x/privacy/client/sdk/payroll/
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
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

Transport-neutral prover adapter를 유지합니다. HTTP 동작·route·versioning·error·공통 header는 [general prover HTTP API](clairveil-proverd-http-api-kr.md), deposit witness/response 처리는 [deposit API](clairveil-proverd-http-api-kr.md#deposit)만을 따릅니다. Finite timeout과 strict response validation을 적용하고 witness body를 log/persist하지 않으며, 같은 witness를 다른 endpoint에 보내기 전에는 명시적 user/product opt-in을 요구합니다.

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
- deposit, transfer, withdraw, batch proof request/response를 prover HTTP contract에 맞춰 검증합니다. Route별 independent envelope·nested payload/proof version과 route-specific response binding을 포함합니다.
- bulk payroll client가 `privacy_note_reservation_contract.json`의 reservation 전이와 operation 성공 규칙을 재현합니다.
- user disclosure, audit disclosure, sender self-view disclosure를 decode하고 `verified=true`를 확인합니다.
- exact-match withdraw와 relayed withdraw payload 검증이 동작합니다.
- JS SDK integration test가 별도로 문서화한 native V2 flow에서 deposit, transfer, batch, withdraw, rescan, auditor verification을 완료합니다. 현재 checkout에는 이 live 증적을 제공하는 Make target이 없습니다.

## 14. Go core 쪽에서 JS SDK가 믿어도 되는 것

현재 JS SDK가 안정 계약으로 삼아도 되는 항목은 아래입니다.
- 현재 prover integration은 `POST` `/v2/prover/audit-field`, request/response envelope `v1`, `privacy-note-v1-audit-field-v1`, base64 `[]byte` field, final PI23입니다.
- Client는 반복 response binding 뒤 exact artifact identity로 local verification을 수행해야 합니다. 아래의 이전 `/v1` example contract는 live V2 SDK surface가 아닌 legacy-only fixture reference입니다.

- current `clairveil.privacy.v2` asset/admin message와 V2 audit query
- 보존 `clairveil.privacy.v1` wallet scan/tree/reserve query
- gRPC/HTTP query path
- typed `privacy_scan`, single-snapshot `commitment_paths_at_root`, bidirectional asset-registry query
- transparent prefix `clair`, shielded prefix `clairs`
- reference denom `uclair`
- full shielded address 기반 transfer UX
- active key epoch의 mandatory V2 audit authorization
- user disclosure policy/mode label
- current V2 asset message는 공통 envelope `v1`/PI23 contract의 audit-field proof와 authorization을 요구
- 보존 legacy fixture: deposit payload/proof/request/response `v1`, transfer payload `v5`와 proof/request/response `v2`, withdraw payload/proof/request/response `v2`, batch payload `batch-transfer-payload-v1`·proof `batch-transfer-proof-v1`·request/response `v1`, disclosure plaintext/query `privacy-fixed-v1`. Current V2 wire가 아님
- active circuit set `privacy-note-v1-audit-field-v1`, consensus `CircuitSetIdentity` schema `v1`, manifest schema `v2`
- sole live prover HTTP path `/v2/prover/audit-field`; 나열된 `/v1` route는 보존 fixture 전용
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`
- `DISCLOSURE-BLINDING-SEPARATION` V1 semantics/error code와 완료된 production 2x2 circuit/native/prepared/structured pre-sign enforcement. Downstream signer도 SDK-wide secret reuse와 non-canonical field alias 거부를 포함한 fail-before-release contract를 유지해야 함. security, protocol, chain-core, and client-integration gates와 독립 공개 검증은 PASS했고 source는 `PUBLICATION_READY_EXPERIMENTAL`
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
docs/clairveil-getting-started-kr.md#7-첫-privacy-흐름
docs/clairveil-downstream-cosmos-integration-guide-kr.md
docs/clairveil-operations-guide-kr.md#6-prover-운영
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
```

그리고 Go core 쪽 sanity check는 아래 명령으로 확인합니다.

```bash
make test
```

Live 증적에는 별도로 문서화한 native V2 harness를 사용합니다. Repository에는 end-to-end live smoke Make target이 없습니다.

## 16. Reference Consumer 예제

| 예제 | 확인할 내용 |
| --- | --- |
| [Audit disclosure key](../examples/audit-disclosure-keys/README-kr.md) | Deterministic, random, privacy-root-signer key 파생과 canonical genesis public-key encoding. |
| [Fixture validator](../examples/js-sdk-fixture-validator/README-kr.md) | Wallet 주소, prepared payload hash, relay mapping. Node를 시작하지 않습니다. |
| [Prover HTTP client](../examples/js-sdk-prover-http-client/README-kr.md) | Fixture 기반 transfer/withdraw request의 finite timeout, bearer auth, version 검사, payload-hash binding. |

실행 명령은 각 예제와 [테스트 가이드](clairveil-testing-guide-kr.md)에서 관리합니다. 예제는 production SDK가 아닌 reference consumer입니다. HTTP demo는 live `clairveil-proverd` 대신 mock을 사용하고 JS client 예제는 transfer/withdraw만 실행합니다. Deposit/batch 계약은 repository schema와 Go conformance fixture에서 검증합니다. Downstream CI에 route-specific binding과 fixture 검증을 이식합니다.

## 17. Batch transfer reference addendum

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

## 18. 신뢰성 및 reference payroll handoff

이 절은 JS/TS control-plane 경계만 추가합니다. 앞선 일반 SDK 검증 기준이나 batch addendum의 encoding, artifact, scan 요구사항을 대체하지 않습니다. 상세 기준은 [reference payroll control plane](../examples/reference-payroll/README-kr.md)을 따릅니다.

### 재시도와 endpoint 안전성

Read, nullifier, broadcast, prover traffic은 별도 adapter로 분리하고 side effect 전에 attempt identity를 durable하게 저장합니다. 아래 matrix가 최소 안전 동작입니다.

| 작업 | 동일 endpoint 재시도 | Endpoint failover | Timeout 처리 |
| --- | --- | --- | --- |
| Public read query | Bounded backoff로 idempotent query를 재시도합니다. | Chain ID와 response shape를 검증한 뒤 허용합니다. Root/path는 서로 다른 snapshot을 섞지 말고 함께 다시 가져옵니다. | Unavailable read로 반환하며 spend/proof side effect를 만들지 않습니다. |
| Nullifier query | 동일한 canonical request(일반적으로 POST batch)를 재시도합니다. | 기본 off입니다. 다른 endpoint로 nullifier를 질의하려면 queried note set을 다른 operator에 드러내므로 명시적인 user/product privacy opt-in이 필요합니다. | Reservation을 active/unknown으로 유지하고 note를 재사용 가능하다고 추론하지 않습니다. |
| Tx broadcast | Tx hash/sign-doc/tx-bytes hash로 식별되는 byte-identical signed transaction만 다시 제출합니다. | 동일한 signed bytes이고 명시적으로 구성된 broadcast endpoint set일 때만 허용합니다. Endpoint를 바꾸려고 rebuild하지 않습니다. | 먼저 tx hash를 조회하고, 이어 모든 input nullifier를 조회합니다. Accepted가 배제되고 nullifier가 unspent이거나 deterministic expiry/rejection으로 새 intent가 필요할 때만 re-sign/rebuild합니다. 그 외에는 `Unknown`을 유지하거나 `ManualReview`로 보냅니다. |
| Prover request | Ambiguous timeout 뒤에는 witness request를 자동 replay하지 않습니다. Original이 accepted되지 않았다고 안전하게 판단할 수 있을 때만 product 승인 재시도를 할 수 있습니다. | 자동 수행 금지입니다. 같은 witness를 포함한 두 번째 endpoint는 별도의 명시적 user/product opt-in과 새 disclosure review가 필요합니다. | Prepared payload와 reservation을 보존합니다. HTTP cancellation은 remote proving이 멈췄다는 증거가 아닙니다. |

Public-read failover 결과만으로 spend를 승인하지 말고 선택한 root/path가 하나의 snapshot임을 검증합니다. Tx hash 조회가 ambiguity의 첫 해결 수단이며, nullifier 조회는 필수 두 번째 확인이지 operation evidence 일치의 대체 수단이 아닙니다.

### 최소 payroll 및 wallet surface

아래 portable type을 모델링합니다. JS 이름은 관용적으로 정해도 되지만 field와 의미는 Go reference와 호환되어야 합니다.

| Type/API | 최소 handoff field 또는 동작 |
| --- | --- |
| `PayrollInput` / `PayrollItem` / `PayrollPlan` | Stable company, payroll, batch, item, employee, operation, attempt, denom, amount, recipient, disclosure-policy, expected output/disclosure 값과 timestamp입니다. `PayrollItem`은 Go의 `PayrollItemInput`에 해당하며, plan item은 선택한 input note와 retry/status를 plan과 별도로 유지합니다. |
| `TreasuryNote` | `note_id`, owner와 nullifier-lookup key/ID, denom, amount, spent flag, `reservation_id`입니다. Spent note나 비어 있지 않은 `reservation_id`가 있는 note는 allocation과 preparation에서 제외합니다. |
| `NotePreparationReport` / `NotePreparationHint` | Ready/blocked item, spendable/reserved/spent count와 amount, zero-dummy availability/shortage, selected note ID, estimated message chunk, 실행 가능한 `add-funds`, `make-dummy`, `split-merge`, `resolve-reservation-lock` hint를 보고합니다. `NotePreparationHint`는 Go의 `NotePreparationOperationHint`에 해당합니다. 이는 note preparation signal이지 note가 사용 가능하다는 증명이 아닙니다. |
| `DisclosureKeyEntry` / registry | `key_id`, scope (`employee`, `company`, `auditor`, `external`), subject ID, canonical public key hex, version, active flag를 저장합니다. Planning 전에 `(scope, subject_id)`로 active key를 resolve하며 stale/inactive key로 조용히 fallback하지 않습니다. |
| `NoteReservation` / `PayrollOperation` | Reservation/operation ID, status, lease field, input-note linkage, tx/sign-doc/tx-bytes hash, broadcast-attempt metadata, expected output commitment, disclosure digest들, recipient/amount hash, denom, batch item index와 known flag를 저장합니다. `privacy_note_reservation_contract.json`의 atomic batch reserve와 compare-and-set, token-owned lease transition을 사용합니다. |

최소 `validatePayroll`, `prepareNotes`, `planPayroll`, atomic `reserve`, lease acquire/heartbeat, `markProofReady`, `markSubmitted`/`markBroadcastUnknown`, `reconcile`, `rescanProjection` operation을 노출합니다. Product별 method name은 달라도 되지만 durable-state와 compare-and-set 의미를 약화하면 안 됩니다.

Operation-success predicate에는 `tx_hash_or_tx_result`, `output_index`, output `commitment`, `recipient_hash`, `amount`(또는 해당 expected hash), `denom_or_asset_id`, audit/full disclosure digest, 기대될 때의 user digest, `audit_key_id`, `audit_key_epoch`, 그리고 plan이 position을 요구할 때의 `batch_item_index`와 known flag가 필요합니다. Spent nullifier만 있고 이 evidence가 일치하지 않으면 성공이 아니라 `ConflictSpent`입니다.

Wallet storage는 encrypted note inventory/projection과 scan cursor를 durable reservation, operation, broadcast attempt와 분리해 보관합니다. 일반 rescan은 projection을 재구축할 수 있지만 먼저 active/unknown reservation을 reconcile하거나 유지해야 하며, 그 linkage를 지워 note가 다시 선택되게 하면 안 됩니다. Unsupported projection version, ambiguous broadcast, missing evidence, cross-endpoint inconsistency는 cursor를 멈추고 `ManualReview`로 보내며 user-visible rescan/reconcile path를 제공합니다.

JS/TS 구현이 reservation fixture의 atomic reserve, active-note exclusion, lease/CAS transition, success/conflict evidence case를 재현하고, registry로 disclosure key를 resolve하며, preparation report/hint를 생성·소비하고, timeout 뒤 tx-hash 다음 nullifier reconciliation으로 duplicate signing이나 note reuse 없이 복구할 때 payroll handoff가 완료됩니다.
