# Clairveil 회로 설명

이 문서는 Clairveil의 ZK 회로가 무엇을 증명하고, 무엇을 증명하지 않는지 설명합니다. 대상 독자는 core chain 개발자, prover 운영자, JS/TS SDK 개발자, 보안 리뷰어입니다.

회로는 `gnark` + Groth16 + BN254를 사용합니다. Hash는 circuit 내부에서 MiMC를 사용하고, note 소유권 서명 검증에는 twisted Edwards 기반 EdDSA 검증 로직을 사용합니다.

## 1. 회로 파일

| 파일                             | 회로               | 사용처                                                                |
| -------------------------------- | ------------------ | --------------------------------------------------------------------- |
| `x/privacy/circuit/spend.go`     | `SpendCircuit`     | shielded note를 transparent account로 withdraw할 때 사용              |
| `x/privacy/circuit/joinsplit.go` | `JoinSplitCircuit` | shielded transfer에서 input note 2개를 output note 2개로 바꿀 때 사용 |

공통 상수:

```text
MerkleDepth = 32
```

Clairveil은 depth 32 단일 Merkle tree를 fixed-capacity pool로 사용합니다.

## 2. Note commitment 모델

두 회로 모두 note commitment를 아래 의미로 계산합니다.

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

## 3. SpendCircuit

`SpendCircuit`은 withdraw에 사용됩니다. shielded note 하나가 존재하고, 그 note 소유자가 특정 transparent recipient로 withdraw를 승인했음을 증명합니다.

### Public input

| 입력         | 의미                                                |
| ------------ | --------------------------------------------------- |
| `MerkleRoot` | spend 대상 note가 포함된 historical Merkle root     |
| `Nullifier`  | 같은 note 재사용을 막기 위한 공개 nullifier         |
| `Amount`     | withdraw할 amount                                   |
| `Recipient`  | transparent recipient를 field element로 바인딩한 값 |
| `AssetID`    | denom을 hash한 asset id                             |

### Secret witness

| witness               | 의미                                             |
| --------------------- | ------------------------------------------------ |
| `ReceiverSpendPubKey` | note 소유권을 나타내는 shielded spend public key |
| `ReceiverViewPubKey`  | note 복구/scan에 쓰이는 view public key          |
| `Signature`           | note 소유자가 withdraw message에 서명했다는 증거 |
| `Randomness`          | commitment와 nullifier를 만드는 note randomness  |
| `Path`, `PathHelper`  | commitment leaf에서 root까지의 Merkle path       |

### 증명하는 것

1. secret note data로 계산한 commitment가 `MerkleRoot` 안에 포함됩니다.
2. `Signature`가 `ReceiverSpendPubKey`에 대해 유효합니다.
3. signature message는 `Amount`, `AssetID`, `Randomness`, `Recipient`에 묶입니다.
4. `Nullifier = MiMC(Randomness, spend_pubkey_x, spend_pubkey_y)`입니다.
5. 즉 같은 note를 다시 쓰면 같은 nullifier가 나오고 keeper가 재사용을 거부할 수 있습니다.

### 증명하지 않는 것

- transparent recipient 문자열 자체를 회로가 직접 이해하지 않습니다.
- recipient address decoding, denom string handling, tx signer 검사는 회로 밖 keeper/SDK/CLI 책임입니다.
- withdraw는 direct change note를 만들지 않습니다. exact-match note 또는 planner가 만든 exact-match note를 사용합니다.
- withdraw에는 output commitment public input이 없습니다. keeper는 input nullifier를 spent로 표시하고 transparent fund를 release하지만, 새 note leaf를 append하지 않습니다.

## 4. JoinSplitCircuit

`JoinSplitCircuit`은 shielded transfer에 사용됩니다. input note 2개를 소비하고 output note 2개를 생성합니다.

구조:

```text
inputs  = 2
outputs = 2
```

일반적으로 output 0은 recipient note, output 1은 sender change note입니다. 필요한 경우 zero-value dummy note가 input slot을 채우는 데 쓰입니다.

### Public input

| 입력                    | 의미                                                           |
| ----------------------- | -------------------------------------------------------------- |
| `MerkleRoot`            | input note들이 포함된 historical Merkle root                   |
| `Nullifiers[2]`         | 두 input note의 nullifier                                      |
| `Commitments[2]`        | 두 output note commitment                                      |
| `UserPrivacyPolicy`     | user selective disclosure 정책 mask                            |
| `UserDisclosureDigest`  | user disclosure payload와 output note를 묶는 digest            |
| `AuditDisclosureDigest` | mandatory audit disclosure payload와 output note를 묶는 digest |

### Secret witness

| witness                                         | 의미                           |
| ----------------------------------------------- | ------------------------------ |
| `AssetID`                                       | transfer asset id              |
| `InputAmounts[2]`, `InputRandomness[2]`         | input note amount/randomness   |
| `InputPaths[2]`, `InputPathHelpers[2]`          | 각 input note의 Merkle path    |
| `InputSignatures[2]`                            | 각 input note 소유권 signature |
| `InputSpendPubKeys[2]`, `InputViewPubKeys[2]`   | input note owner key           |
| `OutputAmounts[2]`, `OutputRandomness[2]`       | output note amount/randomness  |
| `OutputSpendPubKeys[2]`, `OutputViewPubKeys[2]` | recipient/change note key      |

### 증명하는 것

1. 두 input note commitment가 같은 `MerkleRoot` 안에 포함됩니다.
2. 두 input signature가 각각 유효합니다.
3. 두 nullifier가 input note randomness와 spend public key에 맞게 계산됩니다.
4. 두 input note는 같은 shielded owner에 속합니다.
5. 두 output commitment가 secret output data와 일치합니다.
6. `sum(input amounts) = sum(output amounts)`입니다.
7. user disclosure가 켜진 경우, policy에 따라 선택된 amount/from/to/asset 정보가 `UserDisclosureDigest`에 묶입니다.
8. audit disclosure는 항상 full disclosure mask로 계산되어 `AuditDisclosureDigest`에 묶입니다.

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

### Audit disclosure

transfer는 mandatory audit disclosure를 항상 포함해야 합니다. 회로는 full audit disclosure digest를 계산하고, keeper는 message 안의 audit disclosure target pubkey가 chain-configured audit key와 일치하는지 확인합니다.

이 구조의 의미는 아래입니다.

- 일반 observer는 amount/from/to를 직접 볼 수 없습니다.
- disclosure recipient 또는 master auditor는 자신이 가진 disclosure key로 payload를 복호화할 수 있습니다.
- 복호화한 payload는 digest 검증을 통해 on-chain transfer output과 연결됩니다.

## 5. Artifact

`clairveil-setup`은 아래 artifact를 생성합니다.

| 파일                         | 의미                               |
| ---------------------------- | ---------------------------------- |
| `privacy_spend_r1cs.bin`     | SpendCircuit constraint system     |
| `privacy_spend_pk.bin`       | SpendCircuit proving key           |
| `privacy_spend_vk.bin`       | SpendCircuit verifying key         |
| `privacy_joinsplit_r1cs.bin` | JoinSplitCircuit constraint system |
| `privacy_joinsplit_pk.bin`   | JoinSplitCircuit proving key       |
| `privacy_joinsplit_vk.bin`   | JoinSplitCircuit verifying key     |
| `privacy_zk_checksums.env`   | runtime checksum env               |
| `privacy_zk_manifest.json`   | JSON artifact manifest             |

생성 예:

```bash
go build -o clairveil-setup ./cmd/clairveil-setup
./clairveil-setup --out artifacts/privacy
```

runtime에서는 아래 환경변수를 사용합니다.

```bash
source artifacts/privacy/privacy_zk_checksums.env
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

## 6. 회로 변경 시 해야 할 일

회로를 바꾸면 아래를 한 commit 또는 연속 commit으로 정리해야 합니다.

1. `x/privacy/circuit` 테스트를 갱신합니다.
2. prover payload builder와 verifier input shape가 바뀌는지 확인합니다.
3. `proto`, CLI JSON, fixture schema 영향이 있으면 함께 갱신합니다.
4. JS/web wallet conformance fixture를 다시 생성하고 검증합니다.
5. `docs/clairveil-circuits-kr.md`, `docs/clairveil-js-sdk-handoff-kr.md`, release note impact를 갱신합니다.
6. `make ci`, `make privacy-e2e-smoke`, `make release-pack-verify`를 통과시킵니다.

## 7. 주의할 한계

- 회로는 fixed 2-input/2-output transfer 모델입니다.
- ciphertext delivery 자체는 회로가 직접 증명하지 않고 digest binding과 off-chain verification으로 검증합니다.
- production 배포에서는 artifact signing, reproducible generation, release provenance가 추가로 필요합니다.
