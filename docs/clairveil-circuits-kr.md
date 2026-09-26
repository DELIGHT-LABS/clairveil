# Clairveil 회로 설명

> 현재 runtime: `x/privacy/circuit/audit_field.go`와 네 audit-field descriptor가 development-only V2 identity를 사용합니다. 각 request는 exact artifact hash와 final PI23을 bind하며 [Proverd HTTP API](clairveil-proverd-http-api-kr.md#현재-route)를 참고합니다. 아래 NoteV1/BatchJoinSplit relation은 live-route 안내가 아닌 legacy specification입니다.

이 문서는 audit-field wrapper가 사용하는 보존 inner NoteV1 relation이 무엇을 증명하고 증명하지 않는지 설명합니다. Current external contract는 위 audit-field descriptor set과 PI23 framing입니다. 대상 독자는 core chain 개발자, prover 운영자, JS/TS SDK 개발자, 보안 리뷰어입니다.

회로는 `gnark` + Groth16 + BN254를 사용합니다. Hash는 circuit 내부에서 MiMC를 사용하고, note 소유권 서명 검증에는 gnark twisted Edwards EdDSA verifier를 사용합니다.

## 1. 회로 파일

| 파일                             | 회로               | 사용처                                                                |
| -------------------------------- | ------------------ | --------------------------------------------------------------------- |
| `x/privacy/circuit/audit_field.go` | four audit-field wrappers | deposit, spend, 2x2, 16x32의 current V2 descriptor/PI23 boundary |
| `x/privacy/circuit/deposit.go`   | `DepositCircuit`   | transparent coin amount/asset을 shielded note commitment에 binding하는 보존 inner relation |
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
  domain_field("clairveil.note-commitment.v1"),
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness
)
```

Domain label 파생과 정확한 argument 순서는 [BatchJoinSplit16x32 계약](clairveil-batch-joinsplit-16x32-kr.md#32-commitment-nullifier-tree)이 authoritative하며, 이 가이드는 해당 NoteV1 계약을 그대로 따릅니다.

이 commitment는 on-chain leaf로 저장됩니다. amount, asset, randomness, spend/view public key는 직접 공개되지 않고 commitment에 묶입니다.

모든 shielded amount는 non-negative 64-bit integer로 constrain됩니다. Keeper, SDK, payload, circuit 검증은 같은 bound를 사용합니다.

## 3. DepositCircuit

`DepositCircuit`은 deposit에 사용됩니다. Keeper가 privacy module account에 lock하는 transparent amount/asset과 Merkle tree에 append되는 shielded commitment가 같은 note data에 묶였음을 증명합니다.

현재 V2 `DepositAuditFieldV1`은 0 금액도 허용하며 withdraw의 양수 제약은 유지합니다. 0 deposit도 endpoint와 proof를 검증하고 commitment, event, scan 기록을 생성하며 실제 bank 송금만 생략합니다. Deposit audit 회로의 non-zero 제약 제거는 R1CS와 key 호환성을 바꾸므로 기존 deposit R1CS/PK/VK를 재사용할 수 없습니다. [운영 가이드](clairveil-operations-guide-kr.md)의 setup/manifest/identity 절차로 새 bundle을 생성해야 합니다.

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

1. `Commitment = MiMC(domain_field("clairveil.note-commitment.v1"), spend_pubkey_x, spend_pubkey_y, view_pubkey_x, view_pubkey_y, Amount, AssetID, Randomness)`이며, 위의 canonical NoteV1 수식과 정확히 같습니다.
2. shielded public key point가 circuit point로 유효합니다.
3. `Amount`가 64-bit shielded amount bound 안에 있습니다.

### 증명하지 않는 것

- 회로가 bank transfer를 수행하지는 않습니다. Keeper는 cache context를 만들거나 bank, reserve, tree, event, index state를 변경하기 전에 proof를 검증합니다. 검증 성공 뒤 cache context에서 bank transfer → optional module-balance delta check → reserve record → commitment와 indexed event append → `writeCache()` atomic commit 순서로 처리합니다.
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

`DISCLOSURE-BLINDING-SEPARATION` invariant는 recipient output `0`에 다음 조건을 요구합니다. Enabled user blinding은 `OutputRandomness[0]`과 달라야 하고, full blinding은 `OutputRandomness[0]` 및 user blinding과 각각 달라야 합니다. Policy `all-private`는 user blinding을 zero로 canonicalize하고 첫 번째 관계만 gate off합니다. JoinSplit2x2에는 disabled output slot이 없고 output `1`은 disclosure witness가 없는 active change note입니다. Production `JoinSplitCircuit`, shared native/prepared validator와 structured pre-sign boundary가 이 exact contract를 `99,775` constraints로 강제합니다. `DISCLOSURE-BLINDING-SEPARATION` 구현과 security, protocol, chain-core, client-integration, 독립 공개 검증 gate는 완료됐으며 source는 `PUBLICATION_READY_EXPERIMENTAL`이지만 production-release ready는 아닙니다.

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

## 6. Artifact와 유지보수

`clairveil-setup`은 `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1` 각각의 R1CS, proving key, verifying key와 `privacy_zk_manifest.json`, `privacy_zk_checksums.env`를 생성합니다. Manifest의 순서 있는 identity는 consensus와 같아야 하며 checksum 환경변수로 덮어쓸 수 없습니다. Validator는 필요한 VK를, prover는 선택한 R1CS/PK pair를 lazy load합니다. 생성한 binary와 secret은 commit하지 않습니다.

생성, strict preflight, 선택적 development rotation과 배포는 [운영 가이드](clairveil-operations-guide-kr.md)를 따릅니다. Artifact identity, compatibility, development hash는 [프로토콜 계약](clairveil-batch-joinsplit-16x32-kr.md)에 있습니다. Development artifact는 formal trusted setup이나 signed production release가 아닙니다.

회로 변경 시 proof builder/verifier, 영향받는 proto/CLI/schema, conformance fixture, release 영향도 함께 갱신합니다. [기여 체크리스트](../CONTRIBUTING-kr.md)와 [테스트 가이드](clairveil-testing-guide-kr.md)를 따르고 shared native/prepared/structured-signer invariant test도 실행합니다.

## 7. 주의할 한계

- Native `JoinSplitCircuit`은 fixed 2-input/2-output 모델을 유지하고, `BatchJoinSplit16x32`는 별도 1..16-input/1..32-output 회로와 artifact입니다.
- ciphertext delivery 자체는 회로가 직접 증명하지 않고 digest binding과 off-chain verification으로 검증합니다.
- production 배포에서는 artifact signing, reproducible generation, release provenance가 추가로 필요합니다.
- Keeper는 cheap canonical Groth16 framing이 통과한 뒤 decode, VK load, pairing 전에 proof verification gas를 precharge합니다. Deposit/spend/joinsplit은 현재 attempt당 각각 `1,000,000` gas를 charge합니다. Cryptographically invalid proof도 full precharge를 소비하고 malformed framing은 소비하지 않습니다.

## 8. BatchJoinSplit16x32

Batch 회로는 exact active prefix와 zero disabled sentinel, 독립적인 depth-32 membership path, canonical subgroup key, active input/output distinctness, 64-bit value conservation, output별 user/full disclosure digest, owner signature 하나로 input 1..16개와 output 1..32개를 증명합니다. Deposit, Spend, JoinSplit2x2와 NoteV1 relation을 공유합니다.

`DISCLOSURE-BLINDING-SEPARATION`은 output별 user-vs-note, full-vs-note, full-vs-user inequality와 exact all-private/disabled gating을 강제합니다. Production 2x2는 output-0 relation을 circuit과 shared native/prepared/structured signing validation에서 강제합니다. SDK 전체의 secret freshness는 별도의 더 강한 정책입니다.

12개 public input과 그 순서, vector domain, canonical owner-effect byte, fixed payload, gas coefficient, typed scan state, atomic keeper 순서는 [프로토콜 계약](clairveil-batch-joinsplit-16x32-kr.md)을 단일 기준으로 사용합니다. 변경에는 circuit identity, golden vector, compatibility 검토 갱신이 필요합니다. 잔여 disclosure·운영 위험은 [threat model](clairveil-threat-model-kr.md)에 있습니다.
