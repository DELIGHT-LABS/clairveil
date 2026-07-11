# Clairveil NoteV1 및 BatchJoinSplit16x32 프로토콜 계약

## 1. 상태와 범위

이 문서는 향후 `BatchJoinSplit16x32` 구현을 위한 Session 2 프로토콜 기반을 동결한다. NoteV1, domain separation, 고정 인코딩, 16-input/32-output statement shape, aggregate vector root, disclosure digest, scan state, artifact identity, admission control, resource accounting에 대한 normative 문서다.

**Session 2 Gate 2: PASS.** final two-stage user-disclosure contract를 기준으로 두 feasibility gate를 재실행하거나 재확인했다.

- **Full-shape circuit gate: PASS.** corrected Groth16/BN254 prototype이 compile과 development setup을 완료했고, OOM 없이 `16/32`를 포함한 모든 shape를 prove했으며, current JoinSplit2x2 baseline보다 output당 warm proving cost를 개선했다.
- **Max wire/state gate: PASS.** 실제 protobuf message를 실제 Cosmos `TxRaw`에 넣고 typed scan KV record, tree-write allowance, minimal ABCI event, query response까지 측정한 결과 동결된 reference limit 안에 들었다.

Gate 2 통과로 Session 3A 진입 조건은 충족되었지만 **Session 3A 작업은 시작하지 않았다.** 다음 항목은 이번 Session에 존재하지 않으며 prototype에서 구현되었다고 추론하면 안 된다.

- production `BatchJoinSplit16x32` circuit, proving key 또는 verifying key
- 등록된 `MsgBatchTransfer` service 또는 keeper state transition
- batch SDK, prover HTTP route, scanner, payroll integration 또는 formal trusted setup

현재 active circuit set은 `privacy-note-v1`이며 production Deposit, Spend, JoinSplit2x2 circuit으로 구성된다. `batch-joinsplit-16x32-v1`은 reserved schema와 feasibility prototype일 뿐이다.

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
| Reserved batch circuit ID | `batch-joinsplit-16x32-v1` |
| Reserved batch public-input schema SHA-256 | `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333` |

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

circuit은 on-curve, non-identity, subgroup membership을 독립적으로 enforce한다. batch prototype은 owner spend/view point, signature `R`, 32개 output spend/view pair 전체를 검증한다. 모든 input key는 single owner key와 같고 disabled key slot은 같은 owner key sentinel을 사용한다. 성능 최적화를 이유로 이 circuit constraint를 host-only check로 낮추면 안 된다.

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

memo는 local metadata이며 `note_commitment`에 포함되지 않는다. decoder는 invalid UTF-8, non-zero padding, non-canonical field, invalid key, zero active commitment/nullifier, wrong length/version/domain/reserved bytes, trailing data를 거부한다.

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

### 7.1 Prototype message shape

`BatchTransferWirePrototypeV1`은 다음 field set을 동결하고 측정한다.

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

각 `BatchTransferOutputWirePrototypeV1`은 다음을 포함한다.

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

이 protobuf는 의도적으로 Msg service에 등록하지 않았으며 Session 2 prototype은 state transition을 허가하지 않는다. 그러나 owner가 인증할 canonical effect view는 Session 2에서 완전히 동결했다. `lp(x) = u32be(len(x)) || x`이며 empty optional field의 유일한 encoding은 `u32be(0)`이다. 각 output의 `output_effect_i`는 위 output field를 정확히 다음 순서로 인코딩한다.

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

향후 batch ABCI event는 effect ID, relayer, input/output count, 네 aggregate root, expiry만 포함해야 한다. ciphertext, disclosure payload, 전체 nullifier/commitment list를 hex attribute로 반복하면 안 된다.

typed state는 summary 한 개와 output당 raw-byte record 한 개를 저장한다. summary/output identity에는 global cursor, effect ID, circuit-set ID, payload version, scan schema version, audit key ID/epoch/target, tx hash, event type을 포함한다. output state에는 commitment, ciphertext, view tag, leaf index, policy/mode, disclosure digest/target/payload, optional self-view payload도 포함한다.

### 7.4 Exact typed scan event 계약

typed scan state는 protobuf shape만이 아니라 event type별로 검증한다.

| Event | Summary 계약 | Output 계약 |
| --- | --- | --- |
| Deposit | output 한 개, nullifier/effect ID 없음, audit ID/epoch/target은 zero sentinel | `encrypted_note`에 exact deposit-note envelope, empty `ciphertext`/view tag, disclosure field 전체 exact zero sentinel |
| Withdraw | nullifier 한 개, zero output, effect ID 없음, zero audit sentinel | output record 없음, scan은 summary를 계속 반환 |
| JoinSplit2x2 | nullifier/output 정확히 두 개, effect ID 없음, audit ID/epoch은 zero, audit target은 canonical point | exact transfer-note envelope와 2-byte view tag, change output은 exact zero disclosure sentinel, disclosed output은 아래 user/full 규칙 적용 |
| Batch V1 prototype/future event | nullifier `1..16`, output `1..32`, non-zero 32-byte effect ID, canonical audit ID, positive epoch, canonical target point | exact transfer-note envelope와 2-byte view tag, 모든 output에 아래 user/full 규칙 적용 |

all-private user disclosure는 mode `NONE`이고 digest/target/payload가 empty다. public disclosure는 target이 empty이고 `DisclosurePlaintextV1`이 output index/policy/commitment와 일치하며 digest를 재계산한다. recipient-encrypted disclosure는 target이 canonical point이고 payload가 exact user-disclosure envelope다. disclosure가 있는 output은 non-zero canonical full digest, exact audit envelope, empty 또는 exact self-view envelope를 가진다. 모든 output commitment와 leaf index는 commitment state와 일치해야 하며 summary/output identity 및 audit field는 byte-for-byte 같아야 한다.

한 `(height, global_sequence)`의 output key는 정확히 contiguous prefix `0..output_count-1`을 이뤄야 한다. store와 query는 event prefix 아래 어디에 있든 missing, non-adjacent, extra/orphan record, malformed envelope, invalid point/digest, non-canonical sentinel을 거부한다.

## 8. Consensus state와 scanning

### 8.1 Global commitment uniqueness

commitment index는 Deposit, JoinSplit2x2, genesis, 향후 batch output 전체에서 global이다. commitment는 canonical, non-zero이고 proof/state 실행 전에 존재하지 않아야 한다. `AppendCommitment`가 이 조건을 다시 확인하고 immutable leaf index 하나를 기록하며 duplicate를 거부한다. index lookup은 store error를 전파하고 malformed stored index를 absent로 취급하지 않는다. genesis도 globally distinct commitment를 요구한다. Session 3A는 proof verification 전에 local output duplicate와 global index를 검사하고 proof 성공 후에만 append해야 한다.

### 8.2 Unified sequence와 cursor

Deposit, JoinSplit2x2, 향후 batch operation은 하나의 monotonically increasing global privacy sequence를 공유한다. scan cursor는 다음 lexicographic order를 사용한다.

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

모든 commitment append는 결과 root와 exact leaf count, block height를 함께 영속한다. 이 snapshot은 authoritative metadata지만 historical internal tree node는 영속하지 않는다. current-root request는 rebuild cap 없이 incremental node provider를 사용한다. 따라서 모든 non-current historical path는 root/count/height metadata가 있어도 같은 NoteV1 node/empty-root helper로 deterministic prefix rebuild를 수행하며 `1,048,576` leaves cap이 적용된다. cap을 넘는 historical path query는 fail closed하고 archival/local tree provider가 필요하다. 원격 multi-note path query는 해당 provider에 input-note linkage를 노출하므로 privacy requirement가 필요하면 trusted local provider로 대체하는 것이 좋다.

### 8.4 Genesis/reset

genesis export/import는 commitment와 index, historical root와 모든 commitment prefix별 persisted root/count/height snapshot, nullifier, asset registry, global sequence, privacy scan summary/output, privacy event record, reserve counter, exact circuit identity를 보존한다. Asset registry export는 forward/reverse namespace를 모두 순회하고 교차검증하므로 orphan 또는 malformed reverse key를 export에서 조용히 누락하지 않고 fail closed한다. `1,048,576` leaves를 넘는 tree는 complete persisted snapshot index에서 export하며 bounded rebuild에 의존하지 않는다. 작은 export는 persisted metadata 교차검증에만 rebuild를 사용할 수 있다. historical root는 commitment prefix와 대조한다. corruption, large-tree prefix snapshot 누락, mixed state version, duplicate field, mismatched circuit identity는 fail closed한다.

## 9. Circuit identity, artifact loading, prover admission

### 9.1 Consensus artifact identity

consensus는 fixed Deposit/Spend/JoinSplit 순서로 다음을 저장한다.

```text
circuit_set_id
circuit_id
verifying_key_sha256
public_input_schema_sha256
```

validator readiness는 complete local manifest identity가 consensus와 같아야 하며 requested verifying key만 load한다. development에서도 environment checksum으로 consensus를 override할 수 없다. prover readiness는 requested R1CS/proving-key pair만 load하고 명시적 checksum override는 development runtime에서만 허용한다. production override는 거부한다. file은 SHA-256을 검사하고 trailing bytes 없이 완전히 decode하며 verifying key는 canonical round-trip을 통과해야 한다.

registry는 injectable, thread-safe, lazy이며 circuit/artifact type별로 분리해 cache한다. batch schema identity는 reserved 상태지만 Session 3A가 production circuit과 artifact를 만들기 전에는 `RequiredCircuitIDs` 또는 active artifact manifest에 들어가지 않는다.

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

공식은 verification, canonical hashing/framing, leaf/index/node/root write, nullifier와 commitment lookup, typed summary/output bytes를 포함한다. Session 3A는 보수적인 consensus coefficient를 선택하고 Cosmos KV gas와 explicit surcharge가 각각 어느 비용을 담당하는지 중복 또는 누락 없이 문서화해야 한다. production coefficient calibration은 Session 4 범위다.

## 10. Full-shape circuit feasibility 결과

### 10.1 Prototype 구성

capacity prototype은 16개 independent depth-32 membership, exact active prefix, 16개 nullifier, active-only pairwise distinctness, owner signature 하나, 32개 output commitment, 64-bit range와 value conservation, 모든 owner/output subgroup check, 32개 raw user-disclosure digest, 32개 domain-separated user-value leaf hash, 32개 full-disclosure digest, ordered vector tree 네 개, 12개 public input을 포함한다.

지배적 gadget count에는 active-prefix one-hot value 48개, amount range check 48개, independent Merkle node hash 512개, pairwise distinctness check 616개, subgroup point check 67개, note commitment 48개, active-input commitment non-zero check 16개, nullifier 16개, raw user-disclosure hash 32개, user-value leaf hash 32개, full-disclosure hash 32개, blinding inequality check 96개, generic vector leaf 112개, vector internal node 108개, vector root 네 개, EdDSA verifier 하나가 포함된다.

### 10.2 측정 환경과 결과

final run은 `2026-07-11T06:43:45Z`에 Apple M5 Pro, RAM 64 GiB, macOS 26.5.1 (`darwin/arm64`), Go 1.25.12, gnark 0.14.0, gnark-crypto 0.19.2, BN254 Groth16에서 생성했다. trusted ceremony가 아닌 development setup을 사용했다. 각 shape에서 첫 sample과 warm sample 두 개, 총 proof 세 개를 실행했으며 peak RSS는 전체 test process를 측정했다.

| Metric | 결과 |
| --- | ---: |
| constraint, corrected 16x32 prototype | `1,111,837` |
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

## 12. Session 3A용으로 동결된 keeper 순서

향후 production handler는 다음 순서로 실행해야 한다.

1. structural 및 canonical validation
2. count, exact framing/length, total-byte limit
3. deterministic explicit gas precharge
4. exact root, key, payload validation
5. audit-target validation
6. local duplicate nullifier/commitment 거부
7. global spent-nullifier와 global commitment-uniqueness check
8. historical-root와 Merkle-capacity check
9. aggregate-root와 canonical payload-digest 재계산
10. chain-domain과 expiry check
11. proof/public-witness verification
12. nullifier write
13. commitment append
14. typed scan summary/output write
15. minimal ABCI summary event 한 개

proof 성공 전에 batch state write가 발생하면 안 된다. message는 all-or-nothing이다.

## 13. Invariant traceability matrix

`prototype`은 full-shape feasibility circuit에 invariant가 있지만 production batch handler에는 아직 없음을 뜻한다. `Session 3A`는 이 문서가 requirement를 동결했지만 구현은 의도적으로 범위 밖임을 뜻한다.

현재 batch-specific test 범위는 정확히 기록한다. `TestBatchJoinSplit16x32FeasibilityPublicInputOrder`는 12개 서로 다른 public value를 넣고 witness index 전체를 검증한다. `TestBatchJoinSplit16x32FeasibilityActivePrefixAndSentinels`는 disabled input randomness, disabled output key, zero input count, duplicate active nullifier, duplicate active commitment, value-conservation tamper, 세 blinding reuse 관계를 모두 검사한다. tamper case는 aggregate root와 owner signature를 갱신해 의도한 constraint까지 도달한다. `TestBatchJoinSplit16x32ActiveInputCommitmentMustBeNonZero`는 active commitment 규칙을 분리해 검사한다. `TestBatchVectorRootV1RejectsNonCanonicalDisabledSlots`는 zero count, active zero outer value, non-zero disabled value를 검사한다. `TestBatchUserDisclosureVectorRootV1IndependentGolden`은 two-stage user leaf와 disabled metadata를, `TestBatchUserDisclosureV1RejectsNonZeroUnselectedFields`는 policy-selected field를, `TestBatchDisclosureV1BlindingPreventsDictionaryMatch`는 dictionary resistance를, `TestValidateAuditKeyIDV1BoundsAndCanonicalCharset`는 audit ID grammar를 검사한다. `TestCanonicalBatchTransferPayloadV1IndependentGolden`은 exact payload framing과 SHA limb를 독립 재현하고, `TestBatchTransferPayloadDigestV1BindsEveryEffectClass`는 ordered vector와 모든 effect class를 변경하면서 creator/proof 교체만 digest 불변임을 확인하며, `TestBatchTransferWirePrototypeV1PublicDisclosureRecomputesDigest`는 public plaintext와 digest를 묶는다. `TestPrivacyScanV2RejectsCorruptExactOutputContracts`는 malformed envelope, event-prefix orphan, invalid audit point/digest를 검사하고 `TestPrivacyScanV2AcceptsExactBatchPublicDisclosureContract`는 valid batch typed-state shape를 검사한다. `TestNoteV1OneVectorAcrossDepositSpendJoinSplitBatchAndScanner`는 하나의 NoteV1 vector를 이름에 적힌 여섯 경로에서 실행한다.

| ID | Invariant | Circuit constraint | Native helper | Types/Keeper | SDK guard | Negative test | Public doc |
| --- | --- | --- | --- | --- | --- | --- | --- |
| NOTE-COMMITMENT | 하나의 domain-separated NoteV1 공식, active input commitment non-zero | Deposit/Spend/JoinSplit/prototype | `ComputeNoteCommitmentV1` | `Note.ComputeCommitment`, keeper tree input | fixed note decode/recompute | six-path vector test, `TestBatchJoinSplit16x32ActiveInputCommitmentMustBeNonZero` | §3.2, §4.3 |
| NOTE-NULLIFIER | commitment-bound, domain-separated, non-zero | Spend/JoinSplit/prototype | `ComputeNoteNullifierV1` | `Note.ComputeNullifier` | scanner/witness recompute | `TestNoteV1OneVectorAcrossDepositSpendJoinSplitBatchAndScanner` 및 NoteV1 zero test | §3.2 |
| NOTE-KEY-SUBGROUP | canonical, curve, non-identity, prime subgroup | 관련 circuit의 `assertPrimeSubgroupPoint` | `DecodeCanonicalPoint`, `ValidatePrimeSubgroupPoint` | `Note.ValidateV1` | address/envelope decode | crypto decoder 및 circuit subgroup test | §3.4 |
| ACTIVE-PREFIX | slot은 정확히 `[0,count)` | `exactActivePrefix` (prototype) | vector count validation | count bound | future batch builder | `TestBatchJoinSplit16x32FeasibilityActivePrefixAndSentinels`: disabled input randomness, disabled output key, input count zero | §4.3 |
| INPUT-MEMBERSHIP | 16개 independent depth-32 path가 한 root 공유 | gated path loop (prototype) | NoteV1 tree helper | same-root path query | local/query path provider | keeper path-snapshot test만 존재; batch-circuit path-tamper negative test 아직 없음 | §4.3, §8.3 |
| NULLIFIER-DISTINCT | active input nullifier가 pairwise distinct | active-pair check (prototype) | vector validation | current/future duplicate guard | future builder | `TestBatchJoinSplit16x32FeasibilityActivePrefixAndSentinels/duplicate_active_nullifier` | §4.3 |
| COMMITMENT-DISTINCT | active output commitment가 pairwise distinct | active-pair check (prototype) | vector validation | `HasCommitment`/`AppendCommitment` | current transfer builder | batch `duplicate_active_commitment` 및 current Deposit/Transfer global-collision test | §4.3, §8.1 |
| VALUE-CONSERVATION | 64-bit active sum이 같음 | range/sum constraint (prototype) | shielded amount validation | future handler는 proof 사용 | future builder | `TestBatchJoinSplit16x32FeasibilityActivePrefixAndSentinels/value_conservation` | §4.3 |
| OWNER-INTENT | owner 한 명이 exact batch effect에 서명 | EdDSA verifier 하나 (prototype) | `ComputeBatchTransferIntentV1` | reserved public schema | future builder | batch-circuit signature-tamper negative test 아직 없음 | §4.2 |
| CHAIN-EXPIRY | chain/circuit domain과 expiry가 proof-bound | intent input, limb/range check (prototype) | chain-domain 및 batch-intent helper | future expiry check (Session 3A) | future builder | current-circuit replay test만 존재; batch-circuit replay negative test 아직 없음 | §4.2 |
| USER-DISCLOSURE | exact selected field, asset, policy 및 two-stage user value | raw digest 32개 + user-value constraint 32개 (prototype) | `ComputeBatchUserDisclosureDigestV1`, `ComputeBatchUserDisclosureVectorRootV1` | fixed plaintext validation | current disclosure builder/future batch | `TestBatchUserDisclosureVectorRootV1IndependentGolden`, `TestBatchUserDisclosureV1RejectsNonZeroUnselectedFields`, `TestBatchDisclosureV1BlindingPreventsDictionaryMatch` | §5.1–§5.2 |
| FULL-DISCLOSURE | complete per-output evidence | digest constraint 32개 (prototype) | `ComputeBatchFullDisclosureDigestV1` | fixed plaintext validation | audit/self-view builder | current native disclosure test만 존재; batch-circuit digest-tamper negative test 아직 없음 | §5.3 |
| PAYLOAD-BINDING | ciphertext/metadata substitution이 public limb 변경 | public payload limb와 signed intent (prototype) | `CanonicalBatchTransferPayloadBytesV1`, `ComputeBatchTransferPayloadDigestV1` | exact prototype validator; Session 3A handler가 그대로 재사용해야 함 | future builder | independent framing golden, every-effect mutation, public-plaintext 재계산 | §4.2, §7.1 |
| BATCH-EFFECT-ID | proof/relayer와 무관한 stable ID | in-circuit 불필요 | `ComputeBatchEffectIDV1` | future summary key/data | conformance helper | independent golden fixture | §7.2 |
| ATOMIC-STATE | proof 전에 write 없음, effect는 all-or-nothing | proof가 state를 authorize | — | future handler (Session 3A) | result semantics | Session 3A 필요 | §12 |
| SCAN-CURSOR | summary-driven lossless resume와 exact event-prefixed record | — | cursor comparison | `PrivacyScan`, typed state | scanner cursor | cursor/zero-output test, `TestPrivacyScanV2RejectsCorruptExactOutputContracts` | §7.4, §8.2 |
| RESOURCE-BOUND | CPU, byte, state, queue가 bounded | fixed capacity | `ComputeBatchGasV1` | formula/bound | admission/body limit | gas overflow/bound 및 admission test | §9.2–§9.3 |
| GLOBAL-COMMITMENT-UNIQUE | commitment 하나에 global leaf index 하나 | active distinctness (prototype) | canonical field validation | commitment index/append | current builder preflight | Deposit/Transfer collision test | §8.1 |
| ASSET-REGISTRY | denom/ID가 authoritative 1:1 state | asset field 하나 | `ComputeAssetIDV1` | `AssetRegistryV1` query/state | registry lookup | collision/re-registration/corruption test | §3.3 |
| DISCLOSURE-BLINDING | fresh non-zero이며 서로 재사용하지 않는 user/full/note secret | reuse inequality 96개와 per-output non-zero check (prototype) | disclosure digest helper | fixed plaintext에 blinding 포함 | CSPRNG builder | blinding-reuse subtest 세 개, `TestBatchDisclosureV1BlindingPreventsDictionaryMatch` | §5 |
| AUDIT-IDENTITY | bounded canonical ID, positive epoch, canonical target point | future digest/intent가 payload bind | `ValidateAuditKeyIDV1`, canonical point decoder | typed summary/output exact match | future batch builder | audit ID charset test, exact scan accept/reject test | §7.1, §7.4 |
| GLOBAL-SCAN-SEQUENCE | 모든 privacy effect가 sequence 하나 공유 | — | allocation helper | global sequence/index | cursor consumer | sequence reuse/genesis test | §8.2 |
| ARTIFACT-CONSENSUS-IDENTITY | local artifact identity가 consensus와 같음 | public schema 동결 | schema/manifest digest helper | genesis circuit identity | role-aware registry | mismatch/override/readiness test | §9.1 |

## 14. Residual risk와 명시적 non-goal

- batch circuit과 message는 prototype이다. production audit, formal trusted setup, Session 3A/3B 구현이 남아 있다.
- development setup artifact는 production trust anchor가 아니다. 측정에 사용한 proving key와 R1CS는 registered artifact가 아니다.
- final reference run의 peak RSS는 exact `3,339,862,016 B`, 약 3.11 GiB다. lazy loading은 불필요한 artifact 상주를 줄이지만 process-level hard isolation을 제공하지 않는다.
- client cancellation은 gnark proving을 중단할 수 없다. production process isolation, worker recycling, memory limit, overload operation이 필요하다.
- ciphertext decryptability는 proof하지 않는다. auditor-key compromise, key-epoch rotation, delivery failure manual review가 operational risk로 남는다.
- public input/output count, timing, root, batch grouping, minimal summary는 public metadata다.
- remote prover는 complete witness/payment batch를 보게 된다. 매우 민감한 서비스로 취급하고 automatic failover를 계속 비활성화해야 한다.
- 모든 정상 append는 authoritative root/count/height metadata를 영속하지만 historical internal node는 저장하지 않는다. 모든 non-current historical path rebuild는 1,048,576 leaves로 제한되고 current-root incremental path에는 cap이 없으며 large-tree export는 complete persisted metadata index를 요구한다.
- current Deposit과 JoinSplit2x2는 기존 event compatibility 동작을 유지한다. minimal-event 규칙은 향후 batch path의 의무사항이며 Session 2가 모든 legacy event를 재설계했다는 의미가 아니다.
- concrete production gas coefficient, per-chain governance limit, new-asset registration governance, long-run state-pruning policy는 미뤄졌지만 표현된 어떤 work category도 unmetered로 두면 안 된다.

미해결 Critical 또는 High Session 2 design finding은 없다. corrected full-shape resource measurement로 Gate 2가 통과했다. 위 항목은 residual implementation/operational risk이며 동결된 security constraint를 약화하거나 16/32 capacity를 조용히 낮출 권한이 아니다.

## 15. Authoritative code와 fixture

- Note/domain/tree helper: `x/privacy/types/note_v1.go`
- Batch statement/vector/disclosure/effect helper: `x/privacy/types/batch_contract.go`; exact effect encoding/digest: `x/privacy/types/batch_payload.go`
- Fixed payload: `x/privacy/types/fixed_payload.go`
- Feasibility circuit/report: `x/privacy/circuit/batch_joinsplit_16x32_feasibility.go` 및 `_test.go`
- Wire prototype/measurement: `proto/clairveil/privacy/v1/batch_feasibility.proto` 및 `x/privacy/types/batch_wire_feasibility_test.go`
- Asset registry, scan, path snapshot: `x/privacy/keeper/asset_registry.go`, `privacy_scan.go`, `path_snapshot.go`
- Artifact registry와 gas model: `x/privacy/zk/registry.go`, `identity.go`, `schema.go`, `resource_model.go`
- Prover admission: `x/privacy/client/sdk/proverservice/admission.go`
- Independent fixture: `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json` 및 `privacy_batch_joinsplit_v1_contract.json`

이 문서와 구현이 불일치하면 integration을 중단하고 Session 3A가 protocol code를 변경하기 전에 차이를 해소해야 한다.
