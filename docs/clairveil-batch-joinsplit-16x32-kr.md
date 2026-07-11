# Clairveil NoteV1 및 BatchJoinSplit16x32 프로토콜 계약

## 1. 상태와 범위

이 문서는 Session 2 프로토콜 계약을 동결하고 Session 3A consensus/core 및 Session 3B reference-client 구현 상태를 기록한다. NoteV1, domain separation, 고정 인코딩, production 16-input/32-output statement, aggregate vector root, disclosure digest, scan state, artifact identity, resource accounting에 대한 normative 문서다.

**Session 2 Gate 2: PASS.** final two-stage user-disclosure contract를 기준으로 두 feasibility gate를 재실행하거나 재확인했다.

- **Full-shape circuit gate: PASS.** corrected Groth16/BN254 prototype이 compile과 development setup을 완료했고, OOM 없이 `16/32`를 포함한 모든 shape를 prove했으며, current JoinSplit2x2 baseline보다 output당 warm proving cost를 개선했다.
- **Max wire/state gate: PASS.** 실제 protobuf message를 실제 Cosmos `TxRaw`에 넣고 typed scan KV record, tree-write allowance, minimal ABCI event, query response까지 측정한 결과 동결된 reference limit 안에 들었다.

Session 3A는 production circuit과 consensus path를 구현한다. Session 3B는 repository의 reference Go batch planner/preparer, remote batch prover route, lossless typed scanner, durable payroll graph, staged CLI, localnet tutorial을 추가했고 Session 4가 이를 독립 재검증했다. Downstream JS/TS SDK 또는 product, formal trusted setup, external audit, production artifact 배포와 production 운영은 repository-level 완료 범위 밖이다.

Active circuit set은 계속 `privacy-note-v1`이고 이제 Deposit, Spend, JoinSplit2x2, `batch-joinsplit-16x32-v1`을 이 순서로 요구한다. Development R1CS/PK/VK identity는 Gate 3A 증거이지 production trust anchor가 아니다.

이 문서의 **MUST**, **MUST NOT**, **SHOULD**, **MAY**는 일반적인 프로토콜 규범 의미를 갖는다.

## 2. 동결된 version과 capacity

| 계약 | 동결 값 |
| --- | --- |
| Active circuit set | `privacy-note-v1` |
| Module consensus version | `2` |
| Privacy state version | `2` |
| Fixed payload version | `privacy-fixed-v1` / binary version `1` |
| Asset registry version | `privacy-asset-registry-v1` |
| Scan schema version | `privacy-scan-v2` |
| Global scan sequence version | `privacy-sequence-v1` |
| Audit key ID V1 | `1..64` lowercase ASCII bytes, `[a-z0-9][a-z0-9._-]*` |
| Note tree | BN254 Fr, MiMC, depth `32` |
| Batch capacity | input `1..16`, output `1..32` |
| Batch circuit ID | `batch-joinsplit-16x32-v1` |
| Batch public-input schema SHA-256 | `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333` |
| Batch proto/API | `clairveil.privacy.v1.Msg/BatchTransfer`, canonical payload format `1` |

이 version 변경은 fresh reset/genesis를 요구한다. old/new note state가 섞인 상태는 거부한다. 이전 circuit/payload/state version으로 만든 wallet note cache, reservation, prepared witness, prepared proof는 반드시 폐기해야 한다.

## 3. NoteV1

### 3.1 Field-domain 파생

이 문서의 모든 field-domain constant는 다음과 같이 파생한다.

```text
domain_field(label) =
  SHA-256(
    "clairveil.field-domain.v1" ||
    u32be(byte_length(label)) ||
    label
  ) mod Fr
```

label은 정확한 ASCII/UTF-8 bytes다. length와 integer는 unsigned big-endian이다. 하나의 label은 하나의 semantic type만 식별하며 다른 hash primitive나 의미에 재사용하면 안 된다.

NoteV1 label과 canonical 32-byte field encoding은 다음과 같다.

| 용도 | Label | `domain_field(label)` |
| --- | --- | --- |
| note commitment | `clairveil.note-commitment.v1` | `0927abf70e775c0f9fd7db79a93b7f8e94621f15921f6b7077407ec5210cfb1c` |
| note nullifier | `clairveil.note-nullifier.v1` | `1a49a4bf6a216ef5dba9311200be7b1374794ba1ca759a7761e11ac6d774e0b9` |
| note-tree node | `clairveil.note-tree-node.v1` | `0e7215b6529f83eaf86ae8e5ad92eb2ec9f61f1dbd7077c54ff0fdd0e7bfd620` |

### 3.2 Commitment, nullifier, tree

MiMC 인자는 표시된 순서 그대로의 field element다.

```text
note_commitment = MiMC(
  domain_field("clairveil.note-commitment.v1"),
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness,
)

note_nullifier = MiMC(
  domain_field("clairveil.note-nullifier.v1"),
  note_commitment,
  randomness,
  spend_pubkey_x,
  spend_pubkey_y,
)

merkle_parent(level, left, right) = MiMC(
  domain_field("clairveil.note-tree-node.v1"),
  level,
  left,
  right,
)
```

nullifier에 commitment를 포함하므로 같은 randomness를 다른 note에서 실수로 재사용했을 때 곧바로 같은 nullifier로 축약되는 위험을 줄인다.

정확한 empty-tree 계약은 다음과 같다.

```text
empty_root[0] = 0
empty_root[level + 1] = merkle_parent(
  level,
  empty_root[level],
  empty_root[level],
)
```

tree depth는 `32`이며 level `0`이 leaf를 결합한다. canonical empty root 예시는 다음과 같다.

| Depth | Root |
| --- | --- |
| `1` | `2a9932954f9328683b24310f96581603f12544f6da3910aeefebbfa84789b296` |
| `2` | `29bae378ecc69a3c6e1c861407bd57c9c8cd34d37ebc2d4fe8c205952f62793a` |
| `32` | `057551a52590c07629bf07fa2b61832f852fb69ff8472bb21c30e5675ae8e8c1` |

active note commitment와 active nullifier는 반드시 non-zero다. zero는 empty leaf와 disabled vector value에만 예약한다.

Deposit, Spend, JoinSplit2x2, native note helper, keeper tree, fixed-payload scanner decode, BatchJoinSplit feasibility circuit은 모두 같은 공식을 사용한다. legacy JSON 또는 legacy hash fallback은 허용하지 않는다.

### 3.3 Asset ID와 AssetRegistryV1

asset은 circuit과 note 안에서 하나의 field element로 표현한다.

```text
asset_id = SHA-256(
  "clairveil.asset-id.v1" ||
  u32be(byte_length(canonical_denom)) ||
  canonical_denom
) mod Fr
```

`canonical_denom`은 Cosmos denom 규칙을 통과해야 하며 앞뒤 whitespace가 없어야 한다. circuit 전체가 하나의 BN254 field를 사용하므로 reduction은 의도된 결정이다. 따라서 collision resistance는 SHA-256과 field-sized output에 의존하며 collision은 alias가 아니라 registry conflict로 처리해야 한다.

`AssetRegistryV1`은 consensus state이며 authoritative bidirectional `canonical_denom <-> canonical 32-byte asset_id` mapping이다. 두 방향이 모두 존재하고 일치해야 한다. 등록은 다음을 거부한다.

- invalid denom 또는 non-canonical field encoding
- 위 공식과 일치하지 않는 ID
- 동일해 보이는 재등록을 포함한 모든 denom re-registration
- 모든 ID collision 또는 불일치·손상된 reverse mapping

Deposit과 Withdraw는 registered denom만 허용한다. SDK와 UI는 registry query를 통해서만 display denom을 복원하며 ciphertext는 더 이상 신뢰 가능한 denom을 담지 않는다. local configuration이 registry를 조용히 override하면 안 된다. fresh default genesis는 `uclair`를 등록하며 asset ID는 다음과 같다.

```text
238d5f23e4d918d40b0982ce3aef16a75c4d1760193d1c3b30b9f5df681903ca
```

새 asset 등록을 위한 governance message는 Session 2 범위 밖이다.

### 3.4 Public-key 및 signature 검증

wire boundary에서 받는 모든 shielded spend, view, disclosure, EdDSA `R` point는 다음 조건을 모두 만족해야 한다.

1. exact 32-byte compressed encoding
2. decode 후 byte-for-byte canonical re-encoding
3. on-curve
4. identity가 아님
5. prime subgroup membership, `[SubgroupOrder]P = identity`

EdDSA signature는 exact 64-byte `R || S`이며 `R`은 같은 point 규칙을 따르고 `0 < S < SubgroupOrder`여야 한다.

circuit은 on-curve, non-identity, subgroup membership을 독립적으로 enforce한다. Production batch circuit은 owner spend/view point, signature `R`, 32개 output spend/view pair 전체를 검증한다. 모든 input key는 single owner key와 같고 disabled key slot은 같은 owner key sentinel을 사용한다. 성능 최적화를 이유로 이 circuit constraint를 host-only check로 낮추면 안 된다.

### 3.5 Independent golden NoteV1 vector

spend scalar `17`, view scalar `19`, amount `7`, denom `uclair`, randomness `13`일 때:

| 값 | Canonical hex |
| --- | --- |
| commitment | `023aab554dcb995210888fa4e28c3d718568c1de0623578c690a2b6ca9d3610a` |
| nullifier | `13b50fceae57ce77eee3f686abc1563aadc27ff6d1e32ce2fcc599463d28585b` |

independent fixture는 `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`이다.

## 4. 동결된 BatchJoinSplit16x32 statement

### 4.1 Public input

public witness는 정확히 12개 element이며 순서와 encoding은 다음과 같다.

| # | 이름 | Encoding |
| ---: | --- | --- |
| 1 | `MerkleRoot` | `bn254-fr` |
| 2 | `ChainDomainHi` | `uint128` |
| 3 | `ChainDomainLo` | `uint128` |
| 4 | `ExpiresAtUnix` | `uint64`, non-zero |
| 5 | `InputCount` | `uint5`, `1..16`으로 constrain |
| 6 | `OutputCount` | `uint6`, `1..32`로 constrain |
| 7 | `NullifierRoot` | `bn254-fr` |
| 8 | `CommitmentRoot` | `bn254-fr` |
| 9 | `UserDisclosureRoot` | `bn254-fr` |
| 10 | `FullDisclosureRoot` | `bn254-fr` |
| 11 | `PayloadDigestHi` | `uint128` |
| 12 | `PayloadDigestLo` | `uint128` |

개별 nullifier, commitment, disclosure digest는 message data다. keeper는 ordered aggregate root를 재계산하고 proof verification 전에 이 public witness와 비교해야 한다.

### 4.2 Owner intent

모든 active input note는 한 owner에게 속하므로 batch에는 정확히 하나의 owner EdDSA signature가 있다. 서명 field는 다음과 같다.

```text
batch_intent = MiMC(
  domain_field("clairveil.batch-transfer-intent.v1"),
  chain_domain_hi,
  chain_domain_lo,
  domain_field("clairveil.batch-joinsplit-16x32.v1"),
  merkle_root,
  input_count,
  output_count,
  asset_id,
  nullifier_root,
  commitment_root,
  user_disclosure_root,
  full_disclosure_root,
  payload_digest_hi,
  payload_digest_lo,
  expires_at_unix,
)
```

`chain_domain_hi/lo`는 기존 chain-domain contract에서 계산되며 chain ID와 active circuit-set ID를 bind한다. circuit은 chain limb 두 개와 payload limb 두 개를 128-bit로, expiry를 non-zero 64-bit로 constrain한다. Session 3A keeper validation은 expired message도 거부해야 한다.

### 4.3 Exact active prefix와 disabled sentinel

capacity `C`와 public `count`에 대해 circuit은 값 `1..C`의 one-hot vector를 만들고 합이 1임을 assert한 뒤 다음을 계산한다.

```text
enabled[i] = 1 iff i < count
```

따라서 active slot은 정확히 하나의 연속된 prefix이며 hole과 count `0`은 불가능하다.

모든 disabled input slot의 sentinel은 다음과 같다.

| Field | Disabled 값 |
| --- | --- |
| spend public key | owner spend public key |
| view public key | owner view public key |
| amount | `0` |
| randomness | `0` |
| 32개 Merkle sibling 각각 | `0` |
| 32개 path-helper bit 각각 | `0` |
| exported nullifier value | `enabled=0`인 `0` |

membership final-root equality는 `enabled`로 gate하지만 path-helper booleanity와 모든 disabled sentinel은 계속 constrain한다. 각 active input은 non-zero note commitment와 non-zero nullifier를 모두 계산한다. nullifier distinctness는 두 input이 모두 active인 모든 pair에 적용한다.

모든 disabled output slot의 sentinel은 다음과 같다.

| Field | Disabled 값 |
| --- | --- |
| spend public key | owner spend public key |
| view public key | owner view public key |
| amount | `0` |
| randomness | `0` |
| privacy policy/bitmap | `0` (`all-private`) |
| user disclosure blinding | `0` |
| full disclosure blinding | `0` |
| commitment/user/full vector value | `enabled=0`인 `0` |
| payload, target 및 기타 helper | canonical message view에서 absent/zero |

canonical key sentinel을 포함한 모든 output point를 circuit 안에서 검사한다. 모든 active output은 non-zero commitment를 계산한다. commitment distinctness는 두 output이 모두 active인 모든 pair에 적용한다. amount는 unsigned 64-bit이며 active input sum과 active output sum이 같아야 한다. active zero-amount output은 `enabled=1`로 disabled slot과 구별되며 non-zero commitment와 full-disclosure blinding이 여전히 필요하다. randomness는 canonical field value이지만 non-zero 규칙이 아니라 enabled bit로 disabled slot과 구별한다.

### 4.4 Ordered vector root

`T`를 `nullifier`, `commitment`, `user_disclosure`, `full_disclosure` 중 하나라고 한다. capacity는 nullifier가 `16`, 나머지가 `32`다. `count`는 `1..capacity`여야 한다. value는 정확히 full capacity 길이이며 disabled suffix value는 zero다.

```text
leaf[i] = MiMC(
  domain_field("clairveil.batch-vector." || T || ".leaf.v1"),
  i,
  enabled[i],
  value[i],
)

node[level] = MiMC(
  domain_field("clairveil.batch-vector." || T || ".node.v1"),
  level,
  left,
  right,
)

root = MiMC(
  domain_field("clairveil.batch-vector." || T || ".root.v1"),
  capacity,
  count,
  fixed_tree_root,
)
```

leaf는 left-to-right로 pair한다. level `0`이 leaf를 결합하고 nullifier tree depth는 `4`, 32-capacity tree 세 개의 depth는 `5`다. 따라서 type, capacity, count, position, enabled state, value가 모두 commit된다.

`user_disclosure`를 포함한 모든 active outer vector value는 non-zero여야 한다. 모든 type의 disabled value는 zero여야 한다. all-private 특례는 outer user value가 아니라 inner raw user digest에 적용하며 §5.2가 이 two-stage 계약을 정의한다.

count `3`, active value `11,13,17`인 independent nullifier-vector fixture의 root는 다음과 같다.

```text
065354bf1bf6dd8719b40b4c4dc561f437845a426cc2c086a8676a725a13e593
```

## 5. Per-output disclosure 계약

### 5.1 Policy bitmap

policy bit 세 개는 다음과 같다.

| Bit | 값 | 의미 |
| --- | ---: | --- |
| amount | `1` | amount 공개 |
| to | `2` | recipient spend/view key 공개 |
| from | `4` | sender spend/view key 공개 |

policy `1..7`은 해당 bitwise 조합을 사용한다. policy `0`은 all-private다. V1에서 `disclosed_field_bitmap`은 `policy`와 같아야 한다. non-all-private user disclosure에는 asset ID를 항상 포함하며 amount, sender key, recipient key만 policy로 선택한다.

### 5.2 User disclosure digest

user disclosure는 두 개의 명시적 hash stage를 사용한다. policy `1..7`인 active output의 첫 번째 stage는 다음과 같다.

```text
raw_user_digest[i] = MiMC(
  domain_field("clairveil.user-disclosure.v2"),
  i,
  commitment[i],
  policy[i],
  disclosed_field_bitmap[i],
  selected_amount[i],
  selected_sender_spend_x[i],
  selected_sender_spend_y[i],
  selected_sender_view_x[i],
  selected_sender_view_y[i],
  selected_recipient_spend_x[i],
  selected_recipient_spend_y[i],
  selected_recipient_view_x[i],
  selected_recipient_view_y[i],
  asset_id,
  user_disclosure_blinding[i],
)
```

선택되지 않은 amount, sender key, recipient key는 exact zero sentinel이다. `asset_id`는 모든 non-all-private plaintext/digest에서 실제 asset ID이며 policy로 선택하지 않는다. `user_disclosure_blinding[i]`는 fresh, non-zero, per-output CSPRNG field value여야 한다. policy `0`은 raw digest `0`, bitmap `0`, inner asset field를 포함한 selected field 전체 `0`, blinding `0`, mode `NONE`, target/payload 없음으로 표현한다.

두 번째 stage는 per-output user value다.

```text
user_value[i] = MiMC(
  domain_field("clairveil.user-disclosure-leaf.v1"),
  i,
  enabled[i],
  policy[i],
  raw_user_digest[i],
)
```

all-private를 포함한 모든 active output은 `enabled=1`이고 `user_value[i]`가 non-zero여야 한다. 따라서 active all-private output은 `(policy=0, raw_user_digest=0)`이지만 domain-separated outer value는 non-zero다. disabled output은 policy `0`, raw digest `0`이며 위 hash 결과 대신 literal outer value `0`을 사용한다. 이 `user_value[i]`를 §4.4의 generic `clairveil.batch-vector.user_disclosure.{leaf,node,root}.v1` tree가 다시 `value[i]`로 commit한다.

### 5.3 Full disclosure digest

모든 active output은 다음을 가진다.

```text
full_digest[i] = MiMC(
  domain_field("clairveil.full-disclosure.v2"),
  i,
  commitment[i],
  amount[i],
  asset_id,
  sender_spend_x,
  sender_spend_y,
  sender_view_x,
  sender_view_y,
  recipient_spend_x[i],
  recipient_spend_y[i],
  recipient_view_x[i],
  recipient_view_y[i],
  full_disclosure_blinding[i],
)
```

`full_disclosure_blinding[i]`는 fresh, non-zero, per-output CSPRNG value여야 한다. active disclosed user plane에서는 `user_disclosure_blinding[i] != output_randomness[i]`여야 한다. 모든 active output에서 `full_disclosure_blinding[i] != output_randomness[i]`이고 `full_disclosure_blinding[i] != user_disclosure_blinding[i]`여야 한다. capacity 전체에서 slot당 세 관계, 총 96개의 explicit inequality check다. 이는 exact secret reuse를 차단하지만 CSPRNG independence requirement를 대체하지 않는다. mandatory auditor envelope와 optional self-view envelope는 같은 digest의 evidence를 담으며 별도 self-view root는 없다. self-view는 batch-level all-or-none이며 기본값은 enabled다.

per-output secret blinding은 observer가 public digest에 small amount/address dictionary를 대입하는 것을 막는다. public disclosure는 의도적으로 selected plaintext와 blinding을 공개한다. encrypted recipient, auditor, self-view plaintext는 수신자가 proof-bound digest를 재계산할 수 있도록 해당 blinding을 포함한다.

proof는 digest와 canonical payload bytes를 bind하지만 claimed target key로 ciphertext를 복호화할 수 있는지는 증명하지 않는다. 따라서 auditor decrypt failure는 `AuditDeliveryFailed`/manual-review 경로로 처리해야 하며 valid delivery로 조용히 간주하면 안 된다.

## 6. Canonical fixed-size payload

모든 multibyte integer와 field value는 unsigned big-endian이다. 모든 field element는 exact 32 canonical BN254-Fr bytes다. 각 16-byte domain tag는 `SHA-256(label)`의 앞 16 bytes다.

### 6.1 NotePlaintextV1 — exact 350 bytes

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | `clairveil.note-plaintext.v1` domain tag |
| 16 | 2 | binary version `1` |
| 18 | 2 | flags/reserved, 둘 다 zero |
| 20 | 32 | recipient spend X |
| 52 | 32 | recipient spend Y |
| 84 | 32 | recipient view X |
| 116 | 32 | recipient view Y |
| 148 | 8 | amount (`uint64`) |
| 156 | 32 | asset ID |
| 188 | 32 | randomness |
| 220 | 2 | memo byte length (`0..128`) |
| 222 | 128 | UTF-8 memo 뒤에 zero padding |

memo는 local metadata이며 `note_commitment`에 포함되지 않는다. `NewNote`, `Note.ValidateV1`, `MarshalNotePlaintextV1`은 모두 invalid UTF-8과 128 bytes 초과 memo를 거부한다. Go caller는 fallible `MarshalNotePlaintextV1`을 사용해야 하며 silent `Note.Bytes()` helper는 contract에 포함되지 않는다. Decoder는 invalid UTF-8, non-zero padding, non-canonical field, invalid key, zero active commitment/nullifier, wrong length/version/domain/reserved bytes, trailing data를 거부한다.

### 6.2 DisclosurePlaintextV1 — exact 392 bytes

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | `clairveil.disclosure-plaintext.v1` domain tag |
| 16 | 2 | binary version `1` |
| 18 | 1 | plane: user `1`, full `2` |
| 19 | 1 | reserved zero |
| 20 | 4 | output index |
| 24 | 4 | policy; full plane은 `0xffffffff` |
| 28 | 4 | disclosed-field bitmap; full plane은 `7` |
| 32 | 32 | commitment |
| 64 | 8 | selected/full amount |
| 72 | 32 | asset ID |
| 104 | 32 | sender spend X |
| 136 | 32 | sender spend Y |
| 168 | 32 | sender view X |
| 200 | 32 | sender view Y |
| 232 | 32 | recipient spend X |
| 264 | 32 | recipient spend Y |
| 296 | 32 | recipient view X |
| 328 | 32 | recipient view Y |
| 360 | 32 | disclosure blinding |

user-plane policy는 `1..7`이다. asset ID는 실제 asset ID여야 하고 bitmap에서 선택하지 않은 amount, sender key, recipient key는 zero여야 한다. full plane은 모든 field와 non-zero blinding을 사용한다. denom string은 없으며 registry가 authoritative source다.

### 6.3 EncryptedEnvelopeV1

envelope header는 exact 20 bytes다.

| Offset | Size | Field |
| ---: | ---: | --- |
| 0 | 16 | `clairveil.encrypted-envelope.v1` domain tag |
| 16 | 2 | binary version `1` |
| 18 | 1 | kind |
| 19 | 1 | reserved zero |

| Kind | 값 | Encryption | Exact envelope bytes |
| --- | ---: | --- | ---: |
| deposit note | `1` | symmetric nonce `12` + tag `16` | `398` |
| transfer note | `2` | ECIES point `32` + nonce `12` + tag `16` | `430` |
| user disclosure | `3` | ECIES point `32` + nonce `12` + tag `16` | `472` |
| audit disclosure | `4` | 같은 ECIES framing | `472` |
| self-view disclosure | `5` | 같은 ECIES framing | `472` |

decoder는 allocation/use 전에 kind별 exact ciphertext length를 검증한다. Deposit, JoinSplit2x2 recipient/change, scanner parsing, CLI output, disclosure builder, conformance fixture가 이 encoding을 사용한다. legacy JSON plaintext와 raw unversioned ciphertext는 거부한다.

## 7. Structured batch wire 및 effect 계약

### 7.1 Production message shape

`MsgBatchTransfer`는 다음 field set을 사용한다. `BatchTransferWirePrototypeV1`은 independent wire/resource 측정 mirror로만 남는다.

```text
creator
proof
root
nullifiers[]
outputs[]
audit_key_id
audit_key_epoch
audit_disclosure_target_pubkey
expires_at_unix
```

각 `BatchTransferOutput`은 다음을 포함한다.

```text
commitment
ciphertext
view_tag
user_privacy_policy
user_disclosure_mode
user_disclosure_digest
user_disclosure_target_pubkey
user_disclosure_payload
full_disclosure_digest
audit_disclosure_payload
self_view_disclosure_payload
```

`input_count = len(nullifiers)`, `output_count = len(outputs)`이며 count를 wire에 중복 저장하지 않는다. audit identity/target은 batch-level이다. self-view target은 공개하지 않는다. full digest를 auditor와 self-view evidence에 재사용한다.

`audit_key_id`는 `[a-z0-9][a-z0-9._-]*`에 맞는 exact lowercase ASCII이고 길이는 `1..64` bytes다. 첫 byte에는 punctuation을 사용할 수 없다. batch audit epoch는 positive여야 하고 audit target은 canonical, non-identity, prime-subgroup compressed point여야 한다. max-wire 측정은 unbounded placeholder가 아니라 valid 64-byte ID를 사용한다.

이 protobuf는 `clairveil.privacy.v1.Msg/BatchTransfer`로 등록된다. Keeper는 generated protobuf bytes를 hash하지 않고 아래 동결된 canonical owner-authorized effect view를 재사용한다. `lp(x) = u32be(len(x)) || x`이며 empty optional field의 유일한 encoding은 `u32be(0)`이다. 각 output의 `output_effect_i`는 위 output field를 정확히 다음 순서로 인코딩한다.

```text
output_effect_i =
  lp(commitment_i) || lp(ciphertext_i) || lp(view_tag_i) ||
  u32be(user_privacy_policy_i) || u32be(user_disclosure_mode_i) ||
  lp(user_disclosure_digest_i) || lp(user_disclosure_target_pubkey_i) ||
  lp(user_disclosure_payload_i) || lp(full_disclosure_digest_i) ||
  lp(audit_disclosure_payload_i) || lp(self_view_disclosure_payload_i)

canonical_batch_payload_v1 =
  u32be(1) ||
  lp(root) ||
  u32be(input_count) || lp(nullifier_0) || ... || lp(nullifier_{input_count-1}) ||
  u32be(output_count) || output_effect_0 || ... || output_effect_{output_count-1} ||
  lp(audit_key_id ASCII bytes) ||
  u64be(audit_key_epoch) ||
  lp(audit_disclosure_target_pubkey) ||
  u64be(expires_at_unix)

payload_sha256 = SHA-256(
  "clairveil.batch-transfer-payload.v1" || canonical_batch_payload_v1
)
PayloadDigestHi = uint128be(payload_sha256[0:16])
PayloadDigestLo = uint128be(payload_sha256[16:32])
```

`creator`와 `proof`만 message effect에서 제외한다. ordered vector length가 canonical count이므로 별도 count source는 없다. encoder 전 validation은 non-canonical field, duplicate nullifier/commitment, unknown disclosure enum, 잘못된 policy/mode/target/payload 조합, malformed envelope, mixed self-view presence, invalid audit identity, non-positive epoch/expiry를 거부한다. independent 2-input/2-output golden은 canonical `3,702` bytes, SHA-256 `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`, high limb `322132945931579789235567236199104333743`, low limb `14314064343031468430392382204273370288`이다. max `16/32` shape는 canonical `65,384` bytes다.

Consensus per-message wire cap은 `128 KiB`다. `BatchTransferRawFramingDecorator`는 nested governance/authz wrapper를 포함한 signed raw `TxBody.messages[].Any` field를 scan하고 모든 batch message에 type URL과 value가 정확히 하나씩만 존재하도록 강제한다. Decoded `MsgBatchTransfer.Size()`는 secondary shape check일 뿐이다. BaseApp가 ante handling 전에 `ValidateBasic`을 호출하므로 batch `ValidateBasic`은 의도적으로 bounded framing과 creator만 검증하고, complete effect semantics는 keeper의 deterministic precharge 뒤에 실행한다. 따라서 무과금 point/envelope validation 없이 oversized 또는 duplicate raw value를 거부한다.

### 7.2 Batch effect ID

logical effect ID는 proof randomness, relayer, creator, tx hash, block placement와 무관하다.

```text
batch_effect_id = SHA-256(
  "clairveil.batch-effect.v1" ||
  field32(chain_domain_hi) ||
  field32(chain_domain_lo) ||
  field32(merkle_root) ||
  u32be(input_count) ||
  u32be(output_count) ||
  field32(nullifier_root) ||
  field32(commitment_root) ||
  field32(user_disclosure_root) ||
  field32(full_disclosure_root) ||
  field32(payload_digest_hi) ||
  field32(payload_digest_lo) ||
  u64be(expires_at_unix)
)
```

independent golden effect ID는 다음과 같다.

```text
7f76d7744607e06dc0a22e4be5464e1a420c933cff5d060cc657ccfd4ec45979
```

### 7.3 Minimal event와 typed state

Production batch ABCI event는 effect ID, relayer, input/output count, 네 aggregate root, expiry, circuit/payload/scan version, exact audit ID/epoch/target만 포함한다. ciphertext, disclosure payload, commitment, 전체 nullifier list를 hex attribute로 반복하면 안 된다.

typed state는 summary 한 개와 output당 raw-byte record 한 개를 저장한다. summary/output identity에는 global cursor, effect ID, circuit-set ID, payload version, scan schema version, audit key ID/epoch/target, tx hash, event type을 포함한다. output state에는 commitment, ciphertext, view tag, leaf index, policy/mode, disclosure digest/target/payload, optional self-view payload도 포함한다.

### 7.4 Exact typed scan event 계약

typed scan state는 protobuf shape만이 아니라 event type별로 검증한다.

| Event | Summary 계약 | Output 계약 |
| --- | --- | --- |
| Deposit | output 한 개, nullifier/effect ID 없음, audit ID/epoch/target은 zero sentinel | `encrypted_note`에 exact deposit-note envelope, empty `ciphertext`/view tag, disclosure field 전체 exact zero sentinel |
| Withdraw | nullifier 한 개, zero output, effect ID 없음, zero audit sentinel | output record 없음, scan은 summary를 계속 반환 |
| JoinSplit2x2 | nullifier/output 정확히 두 개, effect ID 없음, audit ID/epoch은 zero, audit target은 canonical point | exact transfer-note envelope와 2-byte view tag, change output은 exact zero disclosure sentinel, disclosed output은 아래 user/full 규칙 적용 |
| Batch V1 production event | nullifier `1..16`, output `1..32`, non-zero 32-byte effect ID, canonical audit ID, positive epoch, canonical target point | exact transfer-note envelope와 2-byte view tag, 모든 output에 아래 user/full 규칙 적용 |

all-private user disclosure는 mode `NONE`이고 digest/target/payload가 empty다. public disclosure는 target이 empty이고 `DisclosurePlaintextV1`이 output index/policy/commitment와 일치하며 digest를 재계산한다. recipient-encrypted disclosure는 target이 canonical point이고 payload가 exact user-disclosure envelope다. disclosure가 있는 output은 non-zero canonical full digest, exact audit envelope, empty 또는 exact self-view envelope를 가진다. 모든 output commitment와 leaf index는 commitment state와 일치해야 하며 summary/output identity 및 audit field는 byte-for-byte 같아야 한다.

한 `(height, global_sequence)`의 output key는 정확히 contiguous prefix `0..output_count-1`을 이뤄야 한다. store와 query는 event prefix 아래 어디에 있든 missing, non-adjacent, extra/orphan record, malformed envelope, invalid point/digest, non-canonical sentinel을 거부한다.

## 8. Consensus state와 scanning

### 8.1 Global commitment uniqueness

commitment index는 Deposit, JoinSplit2x2, BatchJoinSplit16x32, genesis 전체에서 global이다. commitment는 canonical, non-zero이고 proof/state 실행 전에 존재하지 않아야 한다. Deterministic precharge 뒤 `ValidateMsgBatchTransferEffectsV1`이 local duplicate를 거부하고 keeper가 proof verification 전에 global index를 검사하며, `AppendCommitment`가 다시 검사해 immutable leaf index 하나를 기록하고 duplicate를 거부한다. index lookup은 store error를 전파하고 malformed stored index를 absent로 취급하지 않는다. genesis도 globally distinct commitment를 요구한다. Batch output은 proof 성공 후에만 append한다.

### 8.2 Unified sequence와 cursor

Deposit, JoinSplit2x2, BatchJoinSplit16x32 operation은 하나의 monotonically increasing global privacy sequence를 공유한다. scan cursor는 다음 lexicographic order를 사용한다.

```text
(height, global_sequence, output_index)
```

별도 batch cursor는 없다. query는 summary-driven이다. 먼저 summary를 순회하고 `summary.output_count`개의 ordered output record가 정확히 존재하는지 검증하며 stray 또는 missing record를 거부한다. zero-output Withdraw도 summary를 반환하고 cursor를 전진시킨다. page는 multi-output operation 중간에서 duplicate나 omission 없이 resume할 수 있으며 `summary.output_count`와 `has_more=true`가 output prefix의 incomplete 상태를 명시하므로 partial batch를 item success로 보고하면 안 된다. typed-record corruption, identity mismatch 또는 query byte-budget failure는 fail closed하며 client는 typed query를 retry해야 한다. payload가 없는 event로 fallback해 complete result처럼 취급하면 안 된다.

동결된 query bound는 다음과 같다.

| Bound | Default | Maximum |
| --- | ---: | ---: |
| output | `128` | `512` |
| event | `64` | `256` |
| encoded response bytes | `1 MiB` | `4 MiB` |
| stored summary/output record 한 개 | — | `1 MiB` |

### 8.3 Same-root path snapshot

`CommitmentPathsAtRoot`는 `1..16`개의 distinct canonical commitment, exact historical root, optional snapshot height를 받는다. 하나의 immutable `(root, height, leaf_count)` prefix에서 모든 32-level path와 leaf index를 반환한다. root, height, leaf count는 서로 일치해야 하며 각 path는 requested root를 재구성해야 한다.

모든 commitment append는 결과 root와 exact leaf count, block height를 함께 영속한다. 이 snapshot은 authoritative metadata지만 historical internal tree node는 영속하지 않는다. Current-root request는 persisted incremental node만 읽는다. Cached current root 또는 필요한 incremental node가 없으면 query는 state를 rebuild하거나 쓰지 않고 fail closed하며(cached root 누락은 `FailedPrecondition`) explicit offline repair를 요구한다. 모든 non-current historical path는 같은 NoteV1 node/empty-root helper로 deterministic prefix rebuild를 수행한다. Public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환하므로 더 큰 online request는 current root 또는 archival/local tree provider가 필요하다. Offline recovery/export는 별도 `MaxMerkleRebuildLeaves`(1,048,576) bound를 유지한다. 원격 multi-note path query는 해당 provider에 input-note linkage를 노출하므로 privacy requirement가 필요하면 trusted local provider로 대체하는 것이 좋다.

### 8.4 Genesis/reset

genesis export/import는 commitment와 index, historical root와 모든 commitment prefix별 persisted root/count/height snapshot, nullifier, asset registry, global sequence, privacy scan summary/output, privacy event record, reserve counter, exact circuit identity를 보존한다. Asset registry export는 forward/reverse namespace를 모두 순회하고 교차검증하므로 orphan 또는 malformed reverse key를 export에서 조용히 누락하지 않고 fail closed한다. `1,048,576` leaves를 넘는 tree는 complete persisted snapshot index에서 export하며 bounded rebuild에 의존하지 않는다. 작은 export는 persisted metadata 교차검증에만 rebuild를 사용할 수 있다. historical root는 commitment prefix와 대조한다. corruption, large-tree prefix snapshot 누락, mixed state version, duplicate field, mismatched circuit identity는 fail closed한다.

## 9. Circuit identity, artifact loading, prover admission

### 9.1 Consensus artifact identity

consensus는 fixed Deposit/Spend/JoinSplit/BatchJoinSplit16x32 순서로 다음을 저장한다.

```text
circuit_set_id
circuit_id
verifying_key_sha256
public_input_schema_sha256
```

validator readiness는 complete local manifest identity가 consensus와 같아야 하며 requested verifying key만 load한다. development에서도 environment checksum으로 consensus를 override할 수 없다. prover readiness는 requested R1CS/proving-key pair만 load하고 명시적 checksum override는 development runtime에서만 허용한다. production override는 거부한다. file은 SHA-256을 검사하고 trailing bytes 없이 완전히 decode하며 verifying key는 canonical round-trip을 통과해야 한다.

registry는 injectable, thread-safe, lazy이며 circuit/artifact type별로 분리해 cache한다. `batch-joinsplit-16x32-v1`은 `RequiredCircuitIDs`의 네 번째 항목이며 canonical 12-descriptor manifest에 descriptor 세 개를 추가한다. Validator는 requested VK만, prover는 selected R1CS/PK pair만 load한다.

Session 3A development identity는 source commit `381c984189e823e5797104eb7cd2beb2386eaf80`에서 `2026-07-11T09:32:32Z`에 생성했다. 다음 값은 reproducibility evidence일 뿐이다.

| Batch artifact | Size | SHA-256 |
| --- | ---: | --- |
| `privacy_batch_joinsplit_16x32_r1cs.bin` | `122,813,535 B` | `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210` |
| `privacy_batch_joinsplit_16x32_pk.bin` | `209,218,621 B` | `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470` |
| `privacy_batch_joinsplit_16x32_vk.bin` | `716 B` | `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621` |

생성 peak RSS는 `3,308,797,952 B`였다. Opt-in role-readiness gate는 peak RSS `1,295,482,880 B`였고 validator role에서 batch VK만, prover role에서 batch R1CS/PK만 decode했으며 constraint `1,111,837`개와 public-schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`을 확인했다. Generated binary는 tracked하지 않는다.

### 9.2 Prover admission

default admission은 production circuit별로 독립적이다.

| 설정 | Default |
| --- | ---: |
| circuit별 max in-flight | `1` |
| circuit별 max queued | `4` |
| request body hard limit | `8 MiB` |

cheap HTTP/framing check 뒤, typed semantic/cryptographic decode 전에 admission을 획득한다. queue가 가득 차면 HTTP `429`, `code="busy"`, `retryable=true`를 반환하며 이 capacity error만 retryable이다. metric은 in-flight, queued, proving, admitted/rejected/canceled/released total, queue wait, prove duration을 제공한다.

permit은 실제 gnark prove가 return할 때까지 유지한다. non-cancelable prove가 실행 중이면 client cancellation만으로 capacity를 release하지 않는다. sensitive gnark solver output은 버리고 automatic multi-prover failover는 계속 off다. production hard cancellation/resource containment에는 여전히 process isolation이 필요하다.

### 9.3 동결된 gas/resource 공식

```text
batch_gas = verify_base
          + per_input * input_count
          + per_output * output_count
          + per_canonical_payload_byte * canonical_payload_bytes
          + per_typed_scan_state_byte * typed_scan_state_bytes
          + per_tree_write * tree_node_writes
          + per_global_lookup * global_lookups
```

모든 coefficient와 resource bound는 positive여야 한다. usage는 input `1..16`, output `1..32` 범위여야 하며 tree write는 output 수보다 작을 수 없고 global lookup은 input과 output의 합보다 작을 수 없다. multiplication과 sum에서 `uint64` overflow를 검사한다.

Session 3A는 다음 보수적인 V1 coefficient와 bound를 동결한다.

| Component | V1 value |
| --- | ---: |
| verify base | `1,000,000` gas |
| per input | `25,000` gas |
| per output | `50,000` gas |
| per canonical payload byte | `4` gas |
| per typed state byte | `8` gas |
| per tree node write | `5,000` gas |
| per global lookup | `10,000` gas |
| canonical payload bound | `65,384 B` |
| typed state bound | `256 KiB` |
| tree-write bound | `1,056` nodes |
| global-lookup bound | `48` |

Explicit surcharge는 privacy-specific proof verification, canonical hashing/encoding, state-growth amplification, Merkle computation/bookkeeping, global uniqueness check를 담당한다. Cosmos KV gas는 underlying store read/write를 계속 담당하며 explicit coefficient가 이를 대체하지 않는다. 따라서 한 logical operation이 computation과 physical I/O를 모두 일으켜도 두 meter는 서로 다른 layer를 담당한다. Exact category breakdown과 precharge-before-semantics/out-of-gas 동작은 regression test로 고정한다. 실제 `1/1` handler 성공과 max `16/32` post-proof transition이 explicit descriptor와 모든 Cosmos KV descriptor를 분리 기록하므로 어느 layer도 상대 layer의 책임을 조용히 흡수하거나 중복할 수 없다. Production coefficient calibration은 Session 4 범위다.

## 10. Full-shape circuit feasibility 결과

### 10.1 Production circuit 구성

Production circuit은 feasibility circuit을 exact하게 유지하며 compatibility type은 alias다. 16개 independent depth-32 membership, exact active prefix, 16개 nullifier, active-only pairwise distinctness, owner signature 하나, 32개 output commitment, 64-bit range와 value conservation, 모든 owner/output subgroup check, 32개 raw user-disclosure digest, 32개 domain-separated user-value leaf hash, 32개 full-disclosure digest, ordered vector tree 네 개, 12개 public input을 포함한다.

지배적 gadget count에는 active-prefix one-hot value 48개, amount range check 48개, independent Merkle node hash 512개, pairwise distinctness check 616개, subgroup point check 67개, note commitment 48개, active-input commitment non-zero check 16개, nullifier 16개, raw user-disclosure hash 32개, user-value leaf hash 32개, full-disclosure hash 32개, blinding inequality check 96개, generic vector leaf 112개, vector internal node 108개, vector root 네 개, EdDSA verifier 하나가 포함된다.

### 10.2 측정 환경과 결과

final run은 `2026-07-11T06:43:45Z`에 Apple M5 Pro, RAM 64 GiB, macOS 26.5.1 (`darwin/arm64`), Go 1.25.12, gnark 0.14.0, gnark-crypto 0.19.2, BN254 Groth16에서 생성했다. trusted ceremony가 아닌 development setup을 사용했다. 각 shape에서 첫 sample과 warm sample 두 개, 총 proof 세 개를 실행했으며 peak RSS는 전체 test process를 측정했다.

| Metric | 결과 |
| --- | ---: |
| constraint, production 16x32 circuit | `1,111,837` |
| constraint, current JoinSplit2x2 | `99,765` |
| measured subgroup point | `67` |
| on-curve/non-identity baseline | `335` constraints / `0.257 ms` compile |
| prime-subgroup check 포함 | `161,537` constraints / `108.752 ms` compile |
| incremental prime-subgroup cost | `161,202` constraints |
| full prototype compile | `1,047.684 ms` |
| development setup | `17,160.691 ms` |
| serialized R1CS | `122,813,535 B` |
| serialized proving key | `209,218,621 B` |
| serialized verifying key | `716 B` |
| proof | `164 B` |
| peak RSS | `3,339,862,016 B` (`~3.11 GiB`) |

subgroup 비교는 67개 point의 on-curve/non-identity shape와 같은 shape에 prime-subgroup scalar multiplication을 추가한 경우를 분리 측정한다. incremental constraint `161,202`개를 그대로 유지했으며 최적화를 이유로 subgroup validation을 host-only로 낮추지 않았다.

| Shape | Witness ms | First prove ms | Warm prove samples ms | Warm mean ms | Verify samples ms |
| --- | ---: | ---: | --- | ---: | --- |
| `1/1` | `0.428` | `1,802.880` | `[1,769.221, 1,770.889]` | `1,770.055` | `[0.699, 0.708, 0.706]` |
| `3/4` | `0.414` | `1,753.411` | `[1,781.738, 1,789.975]` | `1,785.8565` | `[0.722, 0.679, 0.799]` |
| `8/16` | `0.431` | `1,771.809` | `[1,816.801, 1,779.021]` | `1,797.911` | `[0.732, 0.680, 0.740]` |
| `16/32` | `0.429` | `1,874.354` | `[1,791.545, 1,785.570]` | `1,788.5575` | `[0.699, 0.677, 0.698]` |

current JoinSplit2x2 비교는 first prove `158.470 ms`, warm sample `[154.029, 157.718] ms`, warm mean `155.8735 ms`였다. corrected max-shape warm cost는 `55.892422 ms/output`이며 current per-payment baseline보다 `2.788813x` 개선되었다. compile, setup, 모든 proof와 verification이 OOM 없이 완료되었다. 약 209 MB proving key와 123 MB R1CS는 per-role lazy loading을 사용할 때 운영 가능성이 있지만 memory는 여전히 production capacity risk다.

**Circuit gate 결론: PASS.** security constraint, explicit two-stage user leaf, subgroup check, independent path, 16/32 capacity를 모두 유지했다. 이 gate 결과상 Session 3A에 constrained multiproof는 필수가 아니다.

## 11. Max wire/state feasibility 결과

max shape 측정에는 16개 nullifier, 32개 output, maximum valid 64-byte audit key ID와 canonical target point, mandatory auditor payload, maximum recipient disclosure와 self-view envelope, proof/root/tag/key/digest, 실제 Cosmos `TxRaw`, typed scan summary/output protobuf와 KV key, 96 KiB tree-write allowance, minimal ABCI event가 포함된다.

| 항목 | 측정값 | 동결된 reference limit | 결과 |
| --- | ---: | ---: | --- |
| canonical owner-effect payload | `65,384 B` | `4 MiB` gRPC body | PASS |
| prototype message | `65,060 B` | `4 MiB` gRPC body | PASS |
| signed `TxRaw` | `65,294 B` | `1 MiB` tx; `21 MiB` block bytes | PASS |
| typed scan summary | `788 B` | 아래 합계에 포함 | PASS |
| 32 typed scan output | `73,628 B` | 아래 합계에 포함 | PASS |
| key 포함 typed scan KV | `75,105 B` | `256 KiB` | PASS |
| tree-write upper allowance | `98,304 B` | 아래 합계에 포함 | PASS |
| total KV write | `173,409 B` | `256 KiB` | PASS |
| minimal ABCI event | `584 B` | `16 KiB` | PASS |
| max query response | `74,551 B` | `4 MiB` | PASS |

**Wire/state gate 결론: PASS. Combined Gate 2 결론: PASS.** Session 3A는 16/32 capacity와 security constraint를 유지할 수 있다. 이 수치는 feasibility limit이며 per-message hard limit, explicit gas, state-growth monitoring을 생략할 수 있다는 뜻이 아니다.

## 12. 구현된 keeper 순서

Production handler는 다음 순서로 실행한다.

1. cheap bounded count/length/message-size framing
2. fixed-size canonical proof framing
3. deterministic explicit gas precharge
4. full canonical field/point/envelope/disclosure validation 및 local duplicate 거부
5. chain audit ID/epoch/target exact match
6. global spent-nullifier와 commitment-uniqueness check
7. strict historical-root lookup과 whole-batch Merkle-capacity check
8. aggregate root, canonical payload limb, chain domain, expiry, 12개 public value 전부를 message/context에서 파생
9. proof/public-witness verification
10. nested cache에서 nullifier, commitment, root snapshot, typed scan summary/output, minimal event write
11. 모든 write가 성공할 때만 nested cache commit

proof 성공 전에 batch state write가 발생하면 안 된다. message는 all-or-nothing이다.

## 13. Invariant traceability matrix

Production coverage를 명시한다. `TestBatchJoinSplit16x32ProductionPositiveMatrix`는 `1/1`, `1/2`, `3/4`, `8/16`, `16/31`, `16/32`, mixed disclosure, active zero-value padding을 검사한다. `TestBatchJoinSplit16x32ProductionNegativeMatrix`의 59개 negative case는 count, disabled sentinel, path, distinctness, owner/asset/key, amount/conservation, aggregate root, domain/limb/expiry, signature, disclosure/blinding, vector domain separation을 포괄한다. `TestBatchPublicWitnessIsDerivedInFrozenOrder`, `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`는 host/circuit boundary, 실제 development proof, atomic state, 2x2+Batch/Batch+Batch rollback을 검사한다. Scan/gas/genesis/readiness test가 나머지 state/artifact 계약을 검사한다.

| ID | Invariant | Circuit constraint | Native helper | Types/Keeper | SDK guard | Negative test | Public doc |
| --- | --- | --- | --- | --- | --- | --- | --- |
| NOTE-COMMITMENT | 하나의 domain-separated NoteV1 공식, active input commitment non-zero | Deposit/Spend/JoinSplit/Batch | `ComputeNoteCommitmentV1` | `Note.ComputeCommitment`, keeper tree input | fixed note decode/recompute | six-path vector 및 production negative matrix | §3.2, §4.3 |
| NOTE-NULLIFIER | commitment-bound, domain-separated, non-zero | Spend/JoinSplit/Batch | `ComputeNoteNullifierV1` | `Note.ComputeNullifier` | scanner/witness recompute | six-path vector 및 production negative matrix | §3.2 |
| NOTE-KEY-SUBGROUP | canonical, curve, non-identity, prime subgroup | 관련 circuit의 `assertPrimeSubgroupPoint` | `DecodeCanonicalPoint`, `ValidatePrimeSubgroupPoint` | `Note.ValidateV1` | address/envelope decode | crypto decoder 및 circuit subgroup test | §3.4 |
| ACTIVE-PREFIX | slot은 정확히 `[0,count)` | `exactActivePrefix` | vector count validation | `MsgBatchTransfer` count bound | `PlanBatchTransfer`, prepared-payload validation | production positive/negative/property matrix | §4.3 |
| INPUT-MEMBERSHIP | 16개 independent depth-32 path가 한 root 공유 | gated path loop | NoteV1 tree helper | same-root path query | local/query path provider | `invalid_merkle_path`, non-boolean helper, same-root round-trip | §4.3, §8.3 |
| NULLIFIER-DISTINCT | active input nullifier가 pairwise distinct | active-pair check | vector validation | local/global duplicate guard | planner/preparer duplicate guard | adjacent/non-adjacent duplicate, cross-message rollback | §4.3 |
| COMMITMENT-DISTINCT | active output commitment가 pairwise distinct | active-pair check | vector validation | `HasCommitment`/`AppendCommitment` | 기존 preflight pattern | circuit duplicate 및 Deposit/2x2/Batch global collision | §4.3, §8.1 |
| VALUE-CONSERVATION | 64-bit active sum이 같음 | range/sum constraint | shielded amount validation | handler는 verified proof 사용 | planner/preparer total과 role | overflow와 conservation negative case | §4.3 |
| OWNER-INTENT | owner 한 명이 exact batch effect에 서명 | EdDSA verifier 하나 | `ComputeBatchTransferIntentV1` | 동결된 12-value witness | structured signing-request validation | invalid signature/intent mutation과 direct proof | §4.2 |
| CHAIN-EXPIRY | chain/circuit domain과 expiry가 proof-bound | intent input, limb/range check | chain-domain 및 batch-intent helper | context domain과 host expiry 거부 | prepared payload/proof expiry validation | domain/limb/expiry matrix와 wrong-chain/expired core case | §4.2 |
| USER-DISCLOSURE | exact selected field, asset, policy 및 two-stage user value | raw digest 32개 + user-value constraint 32개 | `ComputeBatchUserDisclosureDigestV1`, `ComputeBatchUserDisclosureVectorRootV1` | fixed plaintext validation | output별 plaintext/encryption builder | golden/helper와 production disclosure-root/blinding negative case | §5.1–§5.2 |
| FULL-DISCLOSURE | complete per-output evidence | digest constraint 32개 | `ComputeBatchFullDisclosureDigestV1` | fixed plaintext validation | audit/self-view builder | production full-root/digest/blinding negative case | §5.3 |
| PAYLOAD-BINDING | ciphertext/metadata substitution이 public limb 변경 | public payload limb와 signed intent | canonical production message helper | keeper가 exact encoder 재사용 | canonical effect/signing-request check | independent golden/effect mutation, wrong-payload core rejection | §4.2, §7.1 |
| BATCH-EFFECT-ID | proof/relayer와 무관한 stable ID | in-circuit 불필요 | `ComputeBatchEffectIDV1` | typed/minimal summary | conformance helper | independent golden과 creator/proof/order regression | §7.2 |
| ATOMIC-STATE | proof 전에 write 없음, effect는 all-or-nothing | proof가 state를 authorize | — | nested keeper cache | result semantics | proof/scan failure와 cross-message full rollback | §12 |
| SCAN-CURSOR | summary-driven lossless resume와 exact event-prefixed record | — | cursor comparison | `PrivacyScan`, typed state | scanner cursor | cursor/zero-output test, `TestPrivacyScanV2RejectsCorruptExactOutputContracts` | §7.4, §8.2 |
| RESOURCE-BOUND | CPU, byte, state, queue가 bounded | fixed capacity | `ComputeBatchGasV1` | formula/bound | admission/body limit | gas overflow/bound 및 admission test | §9.2–§9.3 |
| GLOBAL-COMMITMENT-UNIQUE | commitment 하나에 global leaf index 하나 | active distinctness | canonical field validation | commitment index/append | 기존 preflight pattern | Deposit/2x2/Batch/genesis collision test | §8.1 |
| ASSET-REGISTRY | denom/ID가 authoritative 1:1 state | asset field 하나 | `ComputeAssetIDV1` | `AssetRegistryV1` query/state | registry lookup | collision/re-registration/corruption test | §3.3 |
| DISCLOSURE-BLINDING | fresh non-zero이며 서로 재사용하지 않는 user/full/note secret | reuse inequality 96개와 per-output non-zero check | disclosure digest helper | fixed plaintext에 blinding 포함 | CSPRNG builder | production reuse/zero negative case와 dictionary-resistance helper | §5 |
| AUDIT-IDENTITY | bounded canonical ID, positive epoch, canonical target point | digest/intent가 payload bind | `ValidateAuditKeyIDV1`, canonical point decoder | exact chain config와 typed record | prepared payload와 payroll evidence identity | partial-state fail closed 및 ID/epoch/target mismatch | §7.1, §7.4 |
| GLOBAL-SCAN-SEQUENCE | 모든 privacy effect가 sequence 하나 공유 | — | allocation helper | global sequence/index | cursor consumer | Deposit/2x2/Batch 및 genesis continuity | §8.2 |
| ARTIFACT-CONSENSUS-IDENTITY | local artifact identity가 consensus와 같음 | public schema 동결 | schema/manifest digest helper | genesis circuit identity | role-aware registry | mismatch/override와 development artifact gate | §9.1 |

## 14. Residual risk와 명시적 non-goal

- Session 3A core와 Session 3B reference Go client/prover/scanner/payroll/CLI surface는 구현되고 독립 재검증되었다. Downstream JS/TS 또는 product integration, production audit, formal trusted setup, production artifact 배포가 남아 있다.
- Development setup artifact는 production trust anchor가 아니며 commit하지 않는다. 기록된 checksum은 이 Gate 3A run만 식별한다.
- Session 4 reference run의 peak RSS는 `3,429,646,336 B`, 약 3.19 GiB였다. lazy loading은 불필요한 artifact 상주를 줄이지만 process-level hard isolation을 제공하지 않는다.
- client cancellation은 gnark proving을 중단할 수 없다. production process isolation, worker recycling, memory limit, overload operation이 필요하다.
- ciphertext decryptability는 proof하지 않는다. auditor-key compromise, key-epoch rotation, delivery failure manual review가 operational risk로 남는다.
- public input/output count, timing, root, batch grouping, minimal summary는 public metadata다.
- remote prover는 complete witness/payment batch를 보게 된다. 매우 민감한 서비스로 취급하고 automatic failover를 계속 비활성화해야 한다.
- 모든 정상 append는 authoritative root/count/height metadata를 영속하지만 historical internal node는 저장하지 않는다. Current-root path는 incremental node를 사용한다. Public non-current historical query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환하므로 더 큰 online request는 current root 또는 trusted local historical index를 사용한다. Offline recovery/export는 별도 `MaxMerkleRebuildLeaves`(1,048,576) bound를 유지하며 large-tree export는 complete persisted metadata index를 요구한다.
- current Deposit과 JoinSplit2x2는 기존 event compatibility 동작을 유지한다. Production batch path만 minimal-event 규칙을 사용하며 모든 legacy event를 재설계했다는 의미는 아니다.
- Session 3A는 보수적인 gas coefficient를 제공한다. Per-chain calibration/governance limit, new-asset registration governance, long-run state-pruning policy는 남아 있지만 표현된 어떤 work category도 unmetered가 아니다.

미해결 Critical 또는 High Session 2 design finding은 없고 Session 3A는 동결된 protocol decision을 변경하지 않았다. 위 항목은 residual operational/release risk이며 security constraint를 약화하거나 16/32 capacity를 조용히 낮출 권한이 아니다.

## 15. Authoritative code와 fixture

- Note/domain/tree helper: `x/privacy/types/note_v1.go`
- Batch statement/vector/disclosure/effect helper: `x/privacy/types/batch_contract.go`; exact effect encoding/digest: `x/privacy/types/batch_payload.go`
- Fixed payload: `x/privacy/types/fixed_payload.go`
- Production circuit/matrix: `x/privacy/circuit/batch_joinsplit_16x32.go`, `batch_joinsplit_16x32_test.go`; feasibility resource gate는 `batch_joinsplit_16x32_feasibility_test.go`
- Production message/canonical effect: `proto/clairveil/privacy/v1/tx.proto`, `x/privacy/types/batch_payload.go`; wire measurement mirror는 `batch_feasibility.proto`
- Keeper/gas/scan/core integration: `x/privacy/keeper/msg_server_batch_transfer.go`, `batch_gas.go`, `batch_scan_index.go`, `batch_transfer_core_integration_test.go`
- Asset registry, common scan, path snapshot: `x/privacy/keeper/asset_registry.go`, `privacy_scan.go`, `path_snapshot.go`
- Artifact registry/readiness: `x/privacy/zk/registry.go`, `identity.go`, `schema.go`, `resource_model.go`, `development_artifact_gate_test.go`
- Prover admission: `x/privacy/client/sdk/proverservice/admission.go`
- Independent fixture: `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json` 및 `privacy_batch_joinsplit_v1_contract.json`

이 문서와 구현이 불일치하면 integration을 중단한다. 동결된 protocol 변경에는 soundness, public input/golden, downstream API, resource 영향을 다루는 explicit decision-change proposal이 필요하다.
