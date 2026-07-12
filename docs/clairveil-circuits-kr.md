# Clairveil 회로 설명

이 문서는 Clairveil의 ZK 회로가 무엇을 증명하고, 무엇을 증명하지 않는지 설명합니다. 대상 독자는 core chain 개발자, prover 운영자, JS/TS SDK 개발자, 보안 리뷰어입니다.

회로는 `gnark` + Groth16 + BN254를 사용합니다. Hash는 circuit 내부에서 MiMC를 사용하고, note 소유권 서명 검증에는 gnark twisted Edwards EdDSA verifier를 사용합니다.

## 1. 회로 파일

| 파일                             | 회로               | 사용처                                                                |
| -------------------------------- | ------------------ | --------------------------------------------------------------------- |
| `x/privacy/circuit/deposit.go`   | `DepositCircuit`   | deposit 시 transparent coin amount/asset을 shielded note commitment에 binding |
| `x/privacy/circuit/spend.go`     | `SpendCircuit`     | shielded note를 transparent account로 withdraw할 때 사용              |
| `x/privacy/circuit/joinsplit.go` | `JoinSplitCircuit` | shielded transfer에서 input note 2개를 output note 2개로 바꿀 때 사용 |
| `x/privacy/circuit/batch_joinsplit_16x32.go` | `BatchJoinSplit16x32` | `MsgBatchTransfer`에서 note 1..16개를 소비하고 note 1..32개를 atomic하게 생성 |

공통 상수:

```text
MerkleDepth = 32
```

Clairveil은 depth 32 단일 Merkle tree를 fixed-capacity pool로 사용합니다.

## 2. Note commitment 모델

네 회로 모두 note commitment를 아래 의미로 계산합니다.

```text
commitment = MiMC(
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness
)
```

이 commitment는 on-chain leaf로 저장됩니다. amount, asset, randomness, spend/view public key는 직접 공개되지 않고 commitment에 묶입니다.

모든 shielded amount는 non-negative 64-bit integer로 constrain됩니다. Keeper, SDK, payload, circuit 검증은 같은 bound를 사용합니다.

## 3. DepositCircuit

`DepositCircuit`은 deposit에 사용됩니다. Keeper가 privacy module account에 lock하는 transparent amount/asset과 Merkle tree에 append되는 shielded commitment가 같은 note data에 묶였음을 증명합니다.

### Public input

| 입력         | 의미                                      |
| ------------ | ----------------------------------------- |
| `Commitment` | Merkle tree에 append할 shielded note commitment |
| `Amount`     | `MsgDeposit`으로 lock되는 transparent amount |
| `AssetID`    | denom을 hash한 asset id                   |

### Secret witness

| witness               | 의미                                             |
| --------------------- | ------------------------------------------------ |
| `ReceiverSpendPubKey` | 새 note의 shielded spend public key              |
| `ReceiverViewPubKey`  | note 복구/scan에 쓰이는 view public key          |
| `Randomness`          | commitment를 만드는 note randomness              |

### 증명하는 것

1. `Commitment = MiMC(spend_pubkey, view_pubkey, Amount, AssetID, Randomness)`입니다.
2. shielded public key point가 circuit point로 유효합니다.
3. `Amount`가 64-bit shielded amount bound 안에 있습니다.

### 증명하지 않는 것

- 회로가 bank transfer를 수행하지는 않습니다. Keeper가 transparent fund를 lock하고, proof를 검증하고, reserve accounting을 기록하고, commitment를 append하는 일을 한 transaction 안에서 처리합니다.
- 회로가 note를 암호화하지 않습니다. `encrypted_note` 전달은 SDK/CLI 책임입니다.

## 4. SpendCircuit

`SpendCircuit`은 withdraw에 사용됩니다. shielded note 하나가 존재하고, 그 note 소유자가 특정 transparent recipient로 withdraw를 승인했음을 증명합니다.

### Public input

`SpendIntentV2` public-input 순서는 consensus-critical contract입니다.

| 순서 | 입력 | 의미 |
| --- | --- | --- |
| 1 | `MerkleRoot` | spend 대상 note가 포함된 historical Merkle root |
| 2 | `ChainDomainHi` | chain-domain SHA-256 digest의 상위 128 bit |
| 3 | `ChainDomainLo` | chain-domain SHA-256 digest의 하위 128 bit |
| 4 | `ExpiresAtUnix` | proof의 absolute expiry |
| 5 | `Nullifier` | 같은 note 재사용을 막는 공개 nullifier |
| 6 | `Amount` | withdraw할 amount |
| 7 | `RecipientDigestHi` | raw recipient byte digest의 상위 128 bit |
| 8 | `RecipientDigestLo` | raw recipient byte digest의 하위 128 bit |
| 9 | `AssetID` | denom을 hash한 asset id |

### Secret witness

| witness               | 의미                                             |
| --------------------- | ------------------------------------------------ |
| `ReceiverSpendPubKey` | note 소유권을 나타내는 shielded spend public key |
| `ReceiverViewPubKey`  | note 복구/scan에 쓰이는 view public key          |
| `Signature`           | note 소유자가 `SpendIntentV2`에 서명했다는 증거 |
| `Randomness`          | commitment와 nullifier를 만드는 note randomness  |
| `Path`, `PathHelper`  | commitment leaf에서 root까지의 Merkle path       |

### 증명하는 것

1. secret note data로 계산한 commitment가 `MerkleRoot` 안에 포함됩니다.
2. `Signature`가 `ReceiverSpendPubKey`에 대해 유효하고 `SpendIntentV2`의 chain domain, root, nullifier, amount, asset, recipient digest, expiry를 인증합니다.
3. Recipient digest는 `SHA-256("clairveil.withdraw-recipient.v1" || u32be(len(raw_recipient_bytes)) || raw_recipient_bytes)`이며 field reduction 없이 big-endian 128-bit limb 두 개로 나눕니다. 따라서 leading-zero byte string이 다른 recipient와 alias되지 않습니다.
4. `Nullifier = MiMC(Randomness, spend_pubkey_x, spend_pubkey_y)`입니다.
5. `Amount`가 64-bit shielded amount bound 안에 있습니다.
6. 즉 같은 note를 다시 쓰면 같은 nullifier가 나오고 keeper가 재사용을 거부할 수 있습니다.

### 증명하지 않는 것

- transparent recipient 문자열 자체를 회로가 직접 이해하지 않습니다.
- recipient address decoding, raw byte 보존, denom string handling, tx signer 검사, expiry boundary는 회로 밖 keeper/SDK/CLI 책임입니다. Keeper는 `block_time >= expires_at_unix`에서 거부합니다.
- `creator`는 fee를 내는 tx signer/relayer이며 의도적으로 `SpendIntentV2`에서 제외됩니다. `recipient`는 proof-bound되어 바꿀 수 없습니다.
- withdraw는 direct change note를 만들지 않습니다. exact-match note 또는 planner가 만든 exact-match note를 사용합니다.
- withdraw에는 output commitment public input이 없습니다. keeper는 input nullifier를 spent로 표시하고 transparent fund를 release하지만, 새 note leaf를 append하지 않습니다.

## 5. JoinSplitCircuit

`JoinSplitCircuit`은 shielded transfer에 사용됩니다. input note 2개를 소비하고 output note 2개를 생성합니다.

구조:

```text
inputs  = 2
outputs = 2
```

일반적으로 output 0은 recipient note, output 1은 sender change note입니다. 필요한 경우 zero-value dummy note가 input slot을 채우는 데 쓰입니다.

### Public input

`TransferIntentV2` public-input 순서는 consensus-critical contract입니다.

| 순서 | 입력 | 의미 |
| --- | --- | --- |
| 1 | `MerkleRoot` | input note들이 포함된 historical Merkle root |
| 2 | `ChainDomainHi` | chain-domain SHA-256 digest의 상위 128 bit |
| 3 | `ChainDomainLo` | chain-domain SHA-256 digest의 하위 128 bit |
| 4 | `ExpiresAtUnix` | proof의 absolute expiry |
| 5 | `Nullifier0` | 첫 번째 ordered input nullifier |
| 6 | `Nullifier1` | 두 번째 ordered input nullifier |
| 7 | `Commitment0` | 첫 번째 ordered output commitment |
| 8 | `Commitment1` | 두 번째 ordered output commitment |
| 9 | `UserPrivacyPolicy` | user selective-disclosure policy mask |
| 10 | `UserDisclosureDigest` | 독립 blinding이 들어간 selective-disclosure digest |
| 11 | `FullDisclosureDigest` | audit/self-view 검증이 공유하는 독립 blinded full digest |
| 12 | `PayloadDigestHi` | canonical transfer-effect SHA-256 digest의 상위 128 bit |
| 13 | `PayloadDigestLo` | canonical transfer-effect SHA-256 digest의 하위 128 bit |

### Secret witness

| witness                                         | 의미                           |
| ----------------------------------------------- | ------------------------------ |
| `AssetID`                                       | transfer asset id              |
| `InputAmounts[2]`, `InputRandomness[2]`         | input note amount/randomness   |
| `InputPaths[2]`, `InputPathHelpers[2]`          | 각 input note의 Merkle path    |
| `OwnerSignature`                                | final `TransferIntentV2`에 대한 단일 signature |
| `InputSpendPubKeys[2]`, `InputViewPubKeys[2]`   | input note owner key           |
| `OutputAmounts[2]`, `OutputRandomness[2]`       | output note amount/randomness  |
| `OutputSpendPubKeys[2]`, `OutputViewPubKeys[2]` | recipient/change note key      |
| `UserDisclosureBlinding`                        | enabled user disclosure용 독립 non-zero blinding |
| `FullDisclosureBlinding`                        | audit/self-view full disclosure용 독립 non-zero blinding |

### 증명하는 것

1. 두 input note commitment가 같은 `MerkleRoot` 안에 포함됩니다.
2. 두 input의 spend/view owner key가 같고, 그 owner의 `OwnerSignature` 하나가 final `TransferIntentV2`에 대해 유효합니다.
3. 두 nullifier가 input note randomness와 spend public key에 맞게 계산됩니다.
4. 두 nullifier가 서로 다르고 두 output commitment도 서로 다릅니다.
5. 두 output commitment가 secret output data와 일치합니다.
6. `sum(input amounts) = sum(output amounts)`입니다.
7. 각 input/output amount가 64-bit shielded amount bound 안에 있습니다.
8. user disclosure가 켜진 경우 policy로 선택한 field와 non-zero blinding이 `UserDisclosureDigest`에 묶입니다.
9. audit/self-view full disclosure는 non-zero blinding을 사용하고 `FullDisclosureDigest`에 묶입니다.
10. Ordered nullifier, commitment, ciphertext, view tag, 모든 disclosure envelope, expiry는 서명 전에 확정되고 canonical payload digest를 통해 묶입니다. Relayer가 `creator`만 바꿀 수 있도록 `creator`, proof bytes, fee, gas, memo, sequence, tx signature는 제외됩니다.

`S4-B02`는 recipient output `0`에 대한 `DISCLOSURE-BLINDING-SEPARATION` invariant를 동결합니다. Enabled user blinding은 `OutputRandomness[0]`과 달라야 하고, full blinding은 `OutputRandomness[0]` 및 user blinding과 각각 달라야 합니다. Policy `all-private`는 user blinding을 zero로 canonicalize하고 첫 번째 관계만 gate off합니다. JoinSplit2x2에는 disabled output slot이 없고 output `1`은 disclosure witness가 없는 active change note입니다. Production `JoinSplitCircuit`, shared native/prepared validator와 structured pre-sign boundary가 이 exact contract를 `99,775` constraints로 강제합니다. `S4-B02` implementation은 해결했고 Gate 1/2/3A fresh 독립 재검토가 남아 있습니다.

Transfer view tag는 별도 `JoinSplitCircuit` public input은 아니지만 ordered canonical payload digest에는 포함됩니다. `MsgTransfer`와 event에 실리는 public scan hint이며 note ownership signal로 취급하면 안 됩니다.

Chain domain은 `SHA-256("clairveil.chain-domain.v1" || u32be(len(chain_id)) || chain_id || u32be(len(circuit_set_id)) || circuit_set_id)`입니다. SHA-256 digest는 field modulus로 reduce하지 않고 big-endian 128-bit limb 두 개로 나눕니다. SDK는 configured chain에서 계산하고 keeper는 current chain context로 다시 계산합니다. Keeper는 transfer/withdraw 모두 `block_time >= expires_at_unix`에서 거부합니다.

### User disclosure policy

`UserPrivacyPolicy`는 3개 bit로 해석됩니다.

| Policy           | 공개 범위                           |
| ---------------- | ----------------------------------- |
| `all-private`    | user disclosure 없음                |
| `amount`         | amount, asset                       |
| `to`             | recipient shielded address 구성 key |
| `amount-to`      | amount, asset, recipient            |
| `from`           | sender shielded address 구성 key    |
| `amount-from`    | amount, asset, sender               |
| `from-to`        | sender, recipient                   |
| `amount-from-to` | amount, asset, sender, recipient    |

회로는 disclosure plaintext를 직접 암호화하지 않습니다. 회로가 보장하는 것은 “선택된 disclosure field들이 digest에 맞게 묶였다”는 점입니다. 실제 encryption, public/recipient/audit delivery, decode UX는 SDK/CLI와 event payload가 담당합니다.

Sender self-view disclosure는 별도 encrypted metadata입니다. Payload는 signed canonical transfer effect에 포함되고 audit disclosure와 같은 blinded `FullDisclosureDigest`를 사용합니다. Wallet은 복호화한 versioned plaintext에서 blinding을 복원하고 full digest를 다시 계산해 on-chain digest와 비교해야 합니다.

### Audit disclosure

transfer는 mandatory audit disclosure를 항상 포함해야 합니다. 회로는 독립 blinding이 들어간 full disclosure digest를 계산하고, keeper는 message 안의 audit disclosure target pubkey가 chain-configured audit key와 일치하는지 확인합니다.

이 구조의 의미는 아래입니다.

- 일반 observer는 amount/from/to를 직접 볼 수 없습니다.
- disclosure recipient 또는 master auditor는 자신이 가진 disclosure key로 payload를 복호화할 수 있습니다.
- 복호화한 payload는 digest 검증을 통해 on-chain transfer output과 연결됩니다.

## 6. Artifact

`clairveil-setup`은 아래 development artifact를 생성합니다. Active circuit set은 `privacy-note-v1`입니다.

| 파일                         | 의미                               |
| ---------------------------- | ---------------------------------- |
| `privacy_deposit_r1cs.bin`   | DepositCircuit constraint system   |
| `privacy_deposit_pk.bin`     | DepositCircuit proving key         |
| `privacy_deposit_vk.bin`     | DepositCircuit verifying key       |
| `privacy_spend_r1cs.bin`     | SpendCircuit constraint system     |
| `privacy_spend_pk.bin`       | SpendCircuit proving key           |
| `privacy_spend_vk.bin`       | SpendCircuit verifying key         |
| `privacy_joinsplit_r1cs.bin` | JoinSplitCircuit constraint system |
| `privacy_joinsplit_pk.bin`   | JoinSplitCircuit proving key       |
| `privacy_joinsplit_vk.bin`   | JoinSplitCircuit verifying key     |
| `privacy_batch_joinsplit_16x32_r1cs.bin` | BatchJoinSplit16x32 constraint system |
| `privacy_batch_joinsplit_16x32_pk.bin` | BatchJoinSplit16x32 proving key |
| `privacy_batch_joinsplit_16x32_vk.bin` | BatchJoinSplit16x32 verifying key |
| `privacy_zk_checksums.env`   | runtime checksum env               |
| `privacy_zk_manifest.json`   | JSON artifact manifest             |

생성 예:

```bash
go build -o clairveil-setup ./cmd/clairveil-setup
./clairveil-setup --out artifacts/privacy
```

Complete development artifact set에서 JoinSplit만 회전할 때는 다음을 사용합니다.

```bash
./clairveil-setup --out artifacts/privacy --circuit joinsplit --overwrite
```

runtime에서는 아래 환경변수를 사용합니다.

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

`privacy_zk_manifest.json` schema `v2`는 정확한 ordered circuit descriptor, VK SHA-256, public-input schema SHA-256을 기록합니다. Genesis/consensus state는 대응하는 `CircuitSetIdentity` schema `v1`을 고정하며 local checksum environment variable은 이를 override할 수 없습니다. Node는 serving 전에 local verifier identity와 consensus를 비교합니다. Validator는 네 required VK만 lazy load하고 prover는 proving 시 R1CS/PK를 load합니다. Mismatch는 startup/readiness를 막습니다. Generated R1CS/PK/VK binary와 secret은 commit하지 않습니다.

Session 3A development artifact gate는 아래 batch artifact identity를 기록했습니다. 이는 측정한 development artifact의 identity이며 formal setup이나 production artifact release가 아닙니다.

| 항목 | 기록값 |
| --- | --- |
| Constraints | `1,111,837` |
| R1CS | `122,813,535 B`, SHA-256 `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210` |
| Proving key | `209,218,621 B`, SHA-256 `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470` |
| Verifying key | `716 B`, SHA-256 `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621` |
| Public-input schema | SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333` |
| Generation peak RSS | `3,308,797,952 B` |
| Validator/prover readiness peak RSS | `1,295,482,880 B` |

여기서 생성하는 setup은 development 전용입니다. 이 repo는 formal trusted setup, artifact signing ceremony, production artifact release, external audit를 수행하거나 주장하지 않습니다.

`S4-B02` Session 3A 구현은 active circuit set `privacy-note-v1`, JoinSplit 13-input 순서/schema hash `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, payload `v5`, proof/HTTP `v2`를 유지합니다. `privacy_joinsplit_{r1cs,pk,vk}.bin`만 회전했으며 development SHA-256은 각각 `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`입니다. Old JoinSplit proof job을 폐기하고 fresh genesis/reset을 사용해야 합니다. Batch artifact는 unchanged입니다.

## 7. Reserve accounting query

Circuit soundness는 keeper-level reserve accounting과 함께 검증해야 합니다. Keeper는 denom별 `total_deposited`, `total_withdrawn`을 기록하고, 기대 reserve(`total_deposited - total_withdrawn`)와 실제 privacy module-account balance를 비교합니다.

```text
GET /clairveil/privacy/v1/reserve/{denom}
```

`invariant_holds=false`는 direct bank send, manual top-up, migration 작업 이후 특히 incident signal로 취급해야 합니다.

## 8. 회로 변경 시 해야 할 일

회로를 바꾸면 아래를 한 commit 또는 연속 commit으로 정리해야 합니다.

1. `x/privacy/circuit` 테스트를 갱신합니다.
2. prover payload builder와 verifier input shape가 바뀌는지 확인합니다.
3. `proto`, CLI JSON, fixture schema 영향이 있으면 함께 갱신합니다.
4. JS/web wallet conformance fixture를 다시 생성하고 검증합니다.
5. Shared native/prepared/structured-signer invariant vector와 2x2 old-circuit-control 대비 hardened-circuit feasibility test를 실행합니다.
6. `docs/clairveil-circuits-kr.md`, `docs/clairveil-js-sdk-handoff-kr.md`, release note impact를 갱신합니다.
7. `make ci`, `make privacy-e2e-smoke`, `make release-pack-verify`를 통과시킵니다.

## 9. 주의할 한계

- Native `JoinSplitCircuit`은 fixed 2-input/2-output 모델을 유지하고, `BatchJoinSplit16x32`는 별도 1..16-input/1..32-output 회로와 artifact입니다.
- ciphertext delivery 자체는 회로가 직접 증명하지 않고 digest binding과 off-chain verification으로 검증합니다.
- production 배포에서는 artifact signing, reproducible generation, release provenance가 추가로 필요합니다.
- Keeper는 cheap canonical Groth16 framing이 통과한 뒤 decode, VK load, pairing 전에 proof verification gas를 precharge합니다. Deposit/spend/joinsplit은 현재 attempt당 각각 `1,000,000` gas를 charge합니다. Cryptographically invalid proof도 full precharge를 소비하고 malformed framing은 소비하지 않습니다.

## 10. NoteV1과 Session 3A batch core

Active circuit set은 `privacy-note-v1`이고 required descriptor 순서는 `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`입니다. 네 회로, keeper tree, typed scan state는 하나의 domain-separated NoteV1 commitment/nullifier/tree 계약, canonical field/key 검사, exact depth-specific empty root를 공유합니다. Denom string은 circuit에 들어가지 않습니다. `AssetRegistryV1`이 32-byte `asset_id`에 대한 authoritative one-to-one mapping입니다. 이 변경은 breaking state/artifact transition이므로 fresh genesis, artifact 재생성, proof/note/scan cache 삭제, full rescan이 필요합니다.

Canonical plaintext와 encrypted payload는 `privacy-fixed-v1`을 사용합니다. Note plaintext는 fixed 350 bytes, disclosure plaintext는 fixed 392 bytes이며 exact encryption payload 앞에는 20-byte typed envelope header가 붙습니다. Raw ciphertext, cross-kind decode, trailing-byte decode는 invalid입니다. `DISCLOSURE-BLINDING-SEPARATION`은 disclosure output별 user-vs-note, full-vs-note, full-vs-user inequality와 exact all-private/disabled gating을 요구합니다. Batch는 active output slot별로 이를 강제합니다. Production 2x2도 output 0 relation을 circuit, shared native/prepared validator와 structured pre-sign boundary에서 강제하고 JoinSplit development identity를 회전했습니다. `S4-B02` implementation은 해결됐으며 Gate 1/2/3A fresh 독립 재검토만 남았습니다. 이는 cross-output global freshness까지 과장하지 않으면서 low-entropy disclosed value의 실용적 dictionary oracle을 막습니다.

`BatchJoinSplit16x32`는 이제 네 번째 production circuit입니다. Session 2에서 고정한 capacity 16/32, exact active prefix, zero disabled sentinel, 16개 independent depth-32 path, subgroup/key constraint, active-only distinctness, value conservation, output별 NoteV1/user/full-disclosure 검사, single owner signature를 그대로 유지합니다. Consensus public-input 순서는 아래와 같습니다.

1. `MerkleRoot`
2. `ChainDomainHi`
3. `ChainDomainLo`
4. `ExpiresAtUnix`
5. `InputCount`
6. `OutputCount`
7. `NullifierRoot`
8. `CommitmentRoot`
9. `UserDisclosureRoot`
10. `FullDisclosureRoot`
11. `PayloadDigestHi`
12. `PayloadDigestLo`

각 vector kind의 fixed-capacity leaf, node, final root는 domain-separated됩니다. Leaf는 `(index, enabled, value)`, node는 `(level, left, right)`, final root는 `(capacity, count, tree_root)`를 bind합니다. 이 공식과 순서는 public-input schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`인 live consensus contract입니다.

`MsgBatchTransfer`와 structured `BatchTransferOutput`은 registered production proto/type입니다. Owner-effect format `1`은 root, ordered nullifier, ordered output effect field 전체, audit ID/epoch/target, expiry에 `u32be` count와 `u32be(length) || bytes` framing을 사용합니다. SHA-256 domain `clairveil.batch-transfer-payload.v1`의 앞/뒤 non-reduced big-endian 128-bit limb가 public input 11–12입니다. `creator`와 `proof`만 제외합니다. Keeper는 12개 public value 전체를 derive하고 canonical/global nullifier·commitment 검사를 수행하며, cheap framing 뒤 semantic/cryptographic work 전에 deterministic gas를 precharge합니다. Proof 성공 뒤 nullifier, commitment, root snapshot, typed scan record, minimal ABCI/event-index summary를 하나의 cache-context write로 commit합니다.

`BatchGasModelV1`은 verification base `1,000,000`, input당 `25,000`, output당 `50,000`, canonical payload byte당 `4`, typed-state byte당 `8`, tree-node write당 `5,000`, global lookup당 `10,000` gas를 부과합니다. Bound는 canonical payload `65,384` bytes, typed scan state `256 KiB`, tree write `output_count * 33`, global lookup `input_count + output_count`입니다. Out-of-gas는 semantic/state work 전에 중단합니다.

Deposit, native 2x2 JoinSplit, batch transfer는 `privacy-sequence-v1` 순서와 `privacy-scan-v2` typed state를 공유합니다. Ciphertext/disclosure byte는 `PrivacyScanOutputV2`에 한 번만 저장하고 batch event에는 effect ID, count, root, version, expiry, relayer, audit identity만 둡니다. `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`가 direct core success, atomic failure, 2x2+batch/batch+batch rollback을 검증합니다.

Session 3A에는 public batch-transfer Go SDK, `clairveil-proverd`의 batch route, wallet scanner/decrypt UX, 이 one-proof message용 payroll planner/worker/reconcile integration, batch CLI/tutorial, formal trusted setup, production artifact 배포가 포함되지 않습니다. 기존 `transfer-batch`/payroll tooling은 `MsgBatchTransfer` adapter가 아니며 이런 user-facing integration은 Session 3B 이후 범위입니다.

Artifact registry는 role-aware입니다. Validator는 exact consensus identity를 검증하고 필요한 VK만 load하며 prover는 선택한 R1CS/PK pair를 lazy load합니다. Reference prover는 현재 circuit별 in-flight 1개와 queued 4개로 제한하고 positive 8 MiB body limit을 사용합니다. Request cancellation은 이미 실행 중인 in-process gnark solver를 종료하지 못합니다. Hard cancellation과 memory isolation에는 worker-process boundary가 필요합니다. Automatic prover failover는 계속 비활성화됩니다.
