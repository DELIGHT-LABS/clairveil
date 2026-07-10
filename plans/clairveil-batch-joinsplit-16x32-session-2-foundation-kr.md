# Session 2 Plan: NoteV1과 BatchJoinSplit16x32 기반/설계 확정

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Blocked by Session 1 |
| 선행 문서 | [Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md), [Session 1](clairveil-batch-joinsplit-16x32-session-1-security-remediation-kr.md) |
| 후속 세션 | [Session 3A Core Implementation](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md) |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `max` |
| 완료 목표 | final NoteV1과 16x32 normative contract를 circuit 및 max wire/state feasibility 결과와 independent golden vector로 동결함 |

## 1. 진입 Gate

- duplicate input/nullifier inflation이 circuit과 keeper에서 차단됨.
- duplicate output commitment가 차단됨.
- 2x2가 single TransferIntent owner signature를 사용함.
- SpendIntent가 chain domain과 expiry를 proof-bound함.
- transfer/withdraw payload substitution과 cross-chain/expiry attack test가 통과함.
- current user/audit/self-view disclosure blinding과 dictionary regression이 통과함.
- global commitment uniqueness, canonical crypto decoder, no-prover-failover 기본값이 구현됨.
- forged genesis root와 consensus/local artifact mismatch가 fail closed함.
- 미해결 Critical/High current-circuit finding이 없음.
- Session 1 completion record와 clean worktree가 존재함.

```bash
git status --short --branch
git log -8 --oneline
go test ./x/privacy/... -count=1
```

## 2. 목적과 순서

Session 3A가 protocol 의미를 다시 발명하지 않도록 다음을 먼저 고정함.

1. Deposit/Spend/2x2/16x32 공통 NoteV1
2. dominant gadget을 모두 포함한 16x32 full-shape feasibility
3. public statement, active slot, aggregate roots, disclosure, message/event/scan contract
4. canonical fixed-size encoding과 golden fixture
5. artifact/prover resource foundation

중요한 순서는 **full-shape feasibility를 세부 infrastructure 구현보다 먼저 확인하는 것**임. 16-input independent Merkle paths와 32-output disclosure가 실용적이지 않다면 fixed encoding, scanner, prover API를 먼저 구현하지 않음.

## 3. Final NoteV1

### 3.1 Domain-separated primitives

다음 식을 Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32, SDK, scanner에서 동일하게 사용함.

```text
note_commitment = MiMC(
  NOTE_COMMITMENT_V1_DOMAIN,
  spend_pubkey_x,
  spend_pubkey_y,
  view_pubkey_x,
  view_pubkey_y,
  amount,
  asset_id,
  randomness,
)

note_nullifier = MiMC(
  NOTE_NULLIFIER_V1_DOMAIN,
  note_commitment,
  randomness,
  spend_pubkey_x,
  spend_pubkey_y,
)

merkle_parent = MiMC(
  NOTE_TREE_NODE_V1_DOMAIN,
  level,
  left,
  right,
)
```

empty tree도 구현 선택에 맡기지 않고 아래처럼 동결함.

```text
empty_leaf = 0
empty_root[0] = empty_leaf
empty_root[level + 1] = MiMC(NOTE_TREE_NODE_V1_DOMAIN, level, empty_root[level], empty_root[level])
```

active commitment와 nullifier는 `0`이 아니어야 함. `0`은 disabled vector leaf/sentinel과 empty tree를 위한 값으로만 사용함.

nullifier에 commitment를 포함해 같은 owner가 randomness를 실수로 재사용하더라도 서로 다른 note field가 같은 nullifier를 만드는 위험을 줄임.

### 3.2 Domain constant derivation

field domain constant는 다음 방식으로 고정함.

```text
domain_field(label) = SHA-256("clairveil.field-domain.v1" || u32be(len(label)) || label) mod Fr
```

label과 최종 canonical field bytes를 golden fixture에 기록함. 동일 constant를 다른 hash primitive나 semantic type에 재사용하지 않음.

### 3.3 Asset ID

asset ID는 single field를 유지함.

```text
asset_id = SHA-256(
  "clairveil.asset-id.v1" ||
  u32be(len(canonical_denom)) || canonical_denom
) mod Fr
```

canonical denom validation은 Cosmos denom 규칙과 기존 product contract를 유지함. modulo mapping을 사용하는 이유는 asset ID가 circuit 전반의 single field이기 때문이며, exact mapping과 collision security assumption을 문서화함.

denom을 ciphertext에서 제거하므로 on-chain `AssetRegistryV1`을 함께 설계함. `asset_id <-> canonical_denom`은 1:1이어야 하고 collision 또는 재등록을 거부하며 query를 제공함. Deposit은 registry에 없는 denom을 받지 않으며 scanner/SDK/UI는 이 registry를 denom 복원의 authoritative source로 사용함.

### 3.4 Key validation

모든 shielded spend/view/disclosure public key decoder는 다음을 fail closed함.

- canonical compressed encoding
- on-curve
- identity가 아님
- prime subgroup에 속함: `[SubgroupOrder]P == identity`

Circuit invariant:

- input owner key는 slot 0에서 on-curve/non-identity/subgroup를 검증함.
- 모든 active input key가 slot 0과 같음을 검증함.
- output key는 on-curve/non-identity/subgroup를 검증함.
- subgroup scalar multiplication 비용은 full-shape prototype에서 별도 측정함.
- 비용 최적화가 필요해도 circuit 검사를 host-only로 조용히 낮추지 않음. 위협 모델과 soundness 근거를 갱신한 명시적 decision이 필요함.

### 3.5 Cross-circuit consistency

하나의 NoteV1 vector에 대해 다음이 같아야 함.

- native `Note.ComputeCommitment/ComputeNullifier`
- DepositCircuit commitment
- SpendCircuit commitment/nullifier
- JoinSplitCircuit input/output commitment/nullifier
- BatchJoinSplit16x32 prototype
- scanner decrypt 후 commitment/nullifier recomputation

Note tree parent domain 변경은 다음 host state 전체에도 동일하게 적용함.

- keeper Merkle append/root/path 계산
- empty leaf와 depth별 empty root table
- historical root 저장/조회
- commitment index/path provider
- genesis/default tree state
- localnet/conformance Merkle fixture

기존 artifact와 fixture는 호환할 필요가 없음. circuit set, payload, fixture version을 올리고 silent legacy fallback을 제공하지 않음.

## 4. Full-shape Feasibility Gate

### 4.1 Prototype 범위

작은 toy conditional circuit이 아니라 다음 dominant constraint를 모두 포함한 capacity prototype을 먼저 작성함.

- 16개 input commitment와 depth-32 membership path
- canonical active-prefix membership gating
- 16개 nullifier calculation과 active-only distinctness
- active input owner equality
- single owner EdDSA signature 하나
- 32개 output commitment와 active-only distinctness
- amount range와 active sum conservation
- nullifier/commitment fixed-depth aggregate roots
- 32개 user/full disclosure digest와 roots
- 12개 final public input
- key on-curve/non-identity/subgroup invariant

fixed payload encryption, proto, keeper는 prototype에 필요하지 않음. native deterministic witness fixture로 circuit feasibility만 검증함.

### 4.2 Single owner signature

active input은 모두 같은 owner key이므로 batch circuit에는 `OwnerSignature` 하나만 둠.

```text
batch_intent = MiMC(
  BATCH_TRANSFER_INTENT_V1_DOMAIN,
  chain_domain_hi,
  chain_domain_lo,
  circuit_kind,
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

16개 conditional EdDSA verifier와 dummy signature는 만들지 않음.

### 4.3 Membership gating

```text
input_enabled[i] = (i < input_count)
```

- count는 public이며 `1..16` 범위를 constrain함.
- enabled slot은 computed root가 public Merkle root와 같아야 함.
- disabled slot은 amount/randomness/path/helper를 canonical zero로 제한함.
- disabled spend/view key는 slot 0 owner key와 같도록 canonicalize함.
- disabled slot의 owner-related key 외 모든 blinding/helper/signature field는 zero임.
- disabled membership final equality는 enabled bit로 gate함.
- disabled nullifier vector leaf는 enabled=0, value=0임.

### 4.4 Output gating

```text
output_enabled[j] = (j < output_count)
```

- count는 `1..32` 범위를 constrain함.
- disabled output amount/randomness/key fields는 canonical sentinel을 사용함.
- disabled output spend/view key는 slot 0 owner key, amount/asset/randomness/policy/digest/blinding/helper는 zero로 고정함.
- disabled commitment/disclosure leaves는 enabled=0 sentinel임.
- active zero-value padding output과 disabled output은 enabled bit/count로 구분함.

### 4.5 Distinctness

- nullifier는 두 input이 모두 enabled인 pair에만 distinctness를 적용함.
- commitment도 두 output이 모두 enabled인 pair에만 적용함.
- O(16^2 + 32^2) conditional comparison 비용을 측정함.
- 비용이 과도하면 sorting/permutation gadget을 검토할 수 있지만 keeper-only 방어로 낮추지 않음.

### 4.6 Feasibility 측정

최소 측정:

- constraint count와 breakdown
- compile time
- development setup time
- R1CS/PK/VK size
- witness build time
- proving time
- peak RSS
- verification time
- proof size

shape:

- 1 input / 1 output
- 3 input / 4 output
- 8 input / 16 output
- 16 input / 32 output

Gate:

- reference machine에서 compile/setup/max proof가 OOM 없이 완료됨.
- max-shape per-output proving cost가 current 2x2 per-payment baseline보다 개선됨.
- artifact가 lazy-loaded enterprise prover process에서 운영 가능한 크기임을 수치로 설명할 수 있음.
- security constraint를 삭제하지 않고 16x32 shape를 유지할 수 있음.

Gate 실패 시 Session 3A로 넘어가지 않음. 16개의 independent paths가 병목이면 constrained Merkle multiproof를 별도 prototype으로 비교함. M/N을 임의로 낮추거나 실제 zero-note를 요구하는 fixed-input 회로로 바꾸지 않음.

## 5. Batch Public Statement

full-shape gate 통과 후 다음 12개 public input과 정확한 순서를 normative spec로 동결함.

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

individual nullifier/commitment/disclosure digest는 message에 존재하고 Keeper가 aggregate root를 계산함.

## 6. Aggregate Vector Roots

ordered vector commitment를 fixed-depth binary MiMC tree로 구현함. `T`는 `nullifier`, `commitment`, `user_disclosure`, `full_disclosure` 중 하나이며 각 label은 `domain_field`로 field constant가 됨.

```text
leaf = MiMC(
  domain_field("clairveil.batch-vector." || T || ".leaf.v1"),
  index,
  enabled,
  value_or_struct_digest,
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

- nullifier capacity 16: depth 4
- commitment/user/full disclosure capacity 32: depth 5
- vector type마다 별도 leaf/root domain
- disabled leaf는 enabled=0, canonical zero
- active zero output은 enabled=1이라 disabled와 다름
- native/circuit golden vectors로 order/count/domain을 고정함

## 7. Disclosure Contract

### 7.1 User disclosure

output별 policy와 mode를 지원함.

```text
user_leaf_i = MiMC(
  USER_DISCLOSURE_LEAF_V1_DOMAIN,
  i,
  enabled_i,
  policy_i,
raw_user_digest_i,
)
```

`raw_user_digest_i`는 별도 secret blinding을 포함한 다음 exact formula로 고정함.

```text
raw_user_digest_i = MiMC(
  USER_DISCLOSURE_V2_DOMAIN,
  i,
  commitment_i,
  policy_i,
  disclosed_field_bitmap_i,
  selected_recipient_fields_i,
  selected_amount_i,
  asset_id,
  user_disclosure_blinding_i,
)
```

- all-private: policy 0, digest 0, mode NONE, target/payload 없음
- payment output은 requested policy를 사용함.
- change/padding output은 SDK 기본 all-private임.
- public/recipient-encrypted mode와 target/payload exact bytes는 canonical payload digest에 묶임.

### 7.2 Full disclosure

모든 active output은 다음 proof-bound digest를 가짐.

```text
full_digest_i = MiMC(
  FULL_DISCLOSURE_V2_DOMAIN,
  i,
  commitment_i,
  amount_i,
  asset_id,
  sender spend/view key,
  recipient spend/view key,
  full_disclosure_blinding_i,
)
```

audit payload와 self-view payload는 동일한 `full_digest_i`를 plaintext evidence로 사용함.

- audit payload는 chain-configured auditor key로 mandatory encryption함.
- self-view payload는 sender disclosure key로 optional encryption함.
- self-view는 batch-level all-or-none이며 default enabled임.
- 별도 `SelfViewDisclosureRoot`를 만들지 않음.
- self-view ciphertext exact bytes는 payload digest가 묶음.
- 두 blinding은 output별 CSPRNG 값이며 note randomness 또는 서로를 재사용하지 않음.
- user/full disclosure plaintext는 수신자가 digest를 재계산할 수 있도록 해당 blinding을 포함함.
- public disclosure는 공개 plaintext와 blinding을 함께 제공함. 이는 공개 정책의 의도된 동작임.

### 7.3 Decryptability boundary

Circuit/chain이 보장함:

- user/full digest가 note/sender secret과 일치함.
- message digest list가 proof roots와 일치함.
- exact ciphertext/metadata bytes가 owner intent와 일치함.

보장하지 않음:

- ciphertext가 target key로 실제 복호화 가능함.

audit decrypt failure는 `AuditDeliveryFailed` 또는 `ManualReview`로 사후 처리함.

## 8. Canonical Fixed-size Encoding

### 8.1 NotePlaintextV1

고정 width:

- version/domain
- recipient spend key X/Y
- recipient view key X/Y
- amount as unsigned 64-bit big-endian
- canonical 32-byte asset ID
- canonical 32-byte randomness
- user/full disclosure blinding은 note plaintext가 아니라 각 disclosure plaintext/envelope에만 둠.
- bounded memo length
- fixed-capacity zero-padded memo bytes

memo는 commitment에 포함되지 않는 local metadata임을 명시함.

### 8.2 DisclosurePlaintextV1

- version/domain
- output index
- policy/full marker
- commitment
- selected/full amount
- asset ID
- selected/full sender spend/view key
- selected/full recipient spend/view key
- 해당 user/full disclosure blinding

denom 문자열 대신 asset ID를 사용하고 UI는 on-chain `AssetRegistryV1` query에서 denom을 복원함. 임의 local chain config fallback은 허용하지 않음.

### 8.3 적용 범위

shared encoder/decoder를 다음 전체 경로에 적용함.

- deposit encrypted note
- 2x2 transfer recipient/change ciphertext
- future 16x32 output ciphertext
- user/audit/self-view disclosure payload
- scanner `ParseNoteBytes`
- CLI note output
- conformance/browser fixture
- JS SDK handoff/schema

legacy JSON plaintext decode를 제공하지 않음.

### 8.4 Parser

- exact length
- canonical field bytes
- known version
- zero reserved bytes
- no trailing bytes
- bounded allocation
- malformed/truncated/extended input negative tests
- encrypted envelope exact ciphertext length validation

## 9. Structured Batch Message Contract

normative proto 의미:

```text
MsgBatchTransfer
  creator
  proof
  root
  nullifiers[]
  outputs[]
  audit_disclosure_target_pubkey
  expires_at_unix

BatchTransferOutput
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

`input_count = len(nullifiers)`, `output_count = len(outputs)`이며 count를 wire에 중복 저장하지 않음.

- audit target은 batch-level 하나임.
- audit target은 `audit_key_id`, `audit_key_epoch`, canonical target key와 함께 batch summary/output record에 저장함.
- self-view target은 sender clustering 방지를 위해 공개하지 않음.
- self-view digest를 중복 전달하지 않고 full digest를 사용함.
- proof와 creator는 canonical payload digest에서 제외함.

## 10. Scan Data Plane

### 10.1 Batch effect ID

proof randomness와 relayer에 무관한 logical effect ID를 SHA-256으로 고정함.

```text
batch_effect_id = SHA-256(
  "clairveil.batch-effect.v1" ||
  chain_domain_hi || chain_domain_lo ||
  merkle_root ||
  input_count || output_count ||
  nullifier_root || commitment_root ||
  user_disclosure_root || full_disclosure_root ||
  payload_digest_hi || payload_digest_lo ||
  expires_at_unix
)
```

canonical fixed-width encoding을 사용하고 proof/creator/tx hash는 제외함. native golden vector를 conformance fixture에 포함함.

### 10.2 최소 ABCI event

ABCI event에는 다음 summary만 둠.

- batch effect ID
- relayer
- input/output count
- nullifier/commitment/user/full disclosure roots
- expiry

ciphertext, disclosure payload, 전체 nullifier 목록을 hex attribute로 반복하지 않음.

### 10.3 Typed KV scan index

module state에 typed protobuf/raw byte `BatchScanOutputV1` record를 output당 한 번 저장함.

- batch effect ID
- output index
- commitment bytes
- ciphertext bytes
- view tag bytes
- user metadata/payload bytes
- full digest bytes
- audit payload bytes
- optional self-view payload bytes

hex string expansion을 사용하지 않음. batch summary와 output record key ordering을 고정함.

### 10.4 Query/cursor

- Deposit/JoinSplit2x2/Batch가 공유하는 전역 privacy event sequence를 사용함.
- cursor는 `(height, global_sequence, output_index)`로 고정하며 batch 전용 cursor를 별도로 만들지 않음.
- event count, output count, encoded payload byte budget을 모두 제한함.
- page 중간 resume에서 duplicate/missing output이 없어야 함.
- partial batch를 item success로 표시하지 않음.
- state write byte 수를 consensus gas에 반영함.
- typed batch query 실패 시 ciphertext가 없는 minimal ABCI event로 fallback하지 않고 fail closed/retry함.
- typed summary/output에는 `circuit_set_id`, `payload_version`, `scan_schema_version`, `leaf_index`, audit key ID/epoch/target을 포함함.
- max 16 input의 path는 동일 root/height snapshot에서 한 번에 조회하는 query 또는 local tree provider로 제공함. 원격 path query는 note linkage를 노출할 수 있음을 문서화함.
- genesis export/import는 scan record, global sequence, commitment leaf/index, historical root, reserve counter, asset registry를 보존하거나 명시된 deterministic rebuild를 수행함.

## 11. Keeper Contract

Session 3A validation order:

1. structural/canonical validation
2. count, framing, exact length와 total byte hard limit
3. deterministic explicit gas precharge
4. root/key/payload exact validation
5. audit target match
6. local duplicate nullifier/commitment
7. global spent nullifier와 global commitment uniqueness
8. historical root/Merkle capacity
9. aggregate roots and payload digest
10. chain domain/expiry
11. proof/public witness verification
12. nullifier write
13. commitment append
14. typed scan index write
15. minimal ABCI summary event

모든 state write는 proof 성공 후이고 message 단위 all-or-nothing임.

## 12. Resource Foundation

### 12.1 Artifact loader

- validator: requested circuit VK만 lazy load
- prover: requested circuit R1CS/PK만 lazy load
- circuit/role별 thread-safe cache
- injectable registry로 test isolation
- manifest/env checksum strict validation 유지
- consensus/genesis의 exact circuit descriptor(`circuit_set_id`, VK checksum, public-input schema digest)와 local artifact를 비교함.
- mismatch는 validator startup/readiness failure이며 env override는 development-only임.
- readiness는 role별 required artifact만 검사

### 12.2 Prover admission

- circuit별 max in-flight
- bounded queue 또는 immediate reject
- queue full `429`/retryable error
- body hard limit
- queue/prove metrics
- permit은 cheap framing 뒤, semantic/cryptographic validation 전에 획득하고 실제 gnark prove가 종료될 때까지 보유함.
- request cancellation만으로 아직 실행 중인 prove의 permit을 먼저 회수하지 않음.
- sensitive payload log 금지
- automatic multi-prover failover off
- gnark prove가 cancellation되지 않으면 process isolation을 production TODO로 기록

### 12.3 Gas model

```text
batch_gas = verify_base
          + per_input * input_count
          + per_output * output_count
          + per_payload_byte * canonical_payload_bytes
          + per_state_byte * typed_scan_state_bytes
          + per_tree_write * tree_node_writes
          + per_global_lookup * commitment_and_nullifier_lookups
```

proof verification, canonical hashing, Merkle leaf/index/node/root write, global lookup, typed summary/output encoded bytes를 모두 포함함. Cosmos KV gas와 explicit surcharge 중 어느 항목이 비용을 담당하는지 중복/누락 없이 표로 기록함. Session 3A에서 보수적 상수를 구현하고 Session 4 benchmark로 재평가함. permissionless batch verification/state growth를 unmetered로 두지 않음.

### 12.4 Wire/State Feasibility Gate

회로 feasibility 통과 뒤 fixed encoding 초안을 만든 시점에 두 번째 gate를 수행함. 다음 max-shape를 실제 protobuf로 marshal해 측정함.

- 16 nullifier, 32 output, mandatory audit payload
- 최대 허용 user disclosure와 self-view payload
- proof, root, tags, keys, digests를 포함한 exact `MsgBatchTransfer`와 tx bytes
- typed scan summary/output KV 총량과 ABCI event bytes

downstream의 max tx bytes, max block gas/bytes, gRPC/REST body limit, state growth budget 안에 들지 못하면 Session 3A를 시작하지 않음. 이 gate는 회로가 prove 가능하다는 사실만으로 wire/consensus state가 실용적이라고 가정하는 오류를 막음.

### 12.5 State Reset/Genesis Contract

현재 배포 전이므로 migration/legacy compatibility는 구현하지 않음. 대신 consensus version과 circuit/payload/scan schema version을 올리고 fresh genesis/reset을 의무화함. old/new Note state 혼합 import를 거부하고 wallet note cache, reservation, prepared proof를 폐기하도록 handoff에 명시함. genesis historical roots는 commitment prefix로 재계산하고 불일치하면 실패함.

## 13. Invariant Traceability Matrix

normative 한영 설계 문서에 다음 열을 가진 matrix를 유지함.

| ID | Invariant | Circuit constraint | Native helper | Types/Keeper | SDK guard | Negative test | Public doc |
| --- | --- | --- | --- | --- | --- | --- | --- |

최소 ID:

- NOTE-COMMITMENT
- NOTE-NULLIFIER
- NOTE-KEY-SUBGROUP
- ACTIVE-PREFIX
- INPUT-MEMBERSHIP
- NULLIFIER-DISTINCT
- COMMITMENT-DISTINCT
- VALUE-CONSERVATION
- OWNER-INTENT
- CHAIN-EXPIRY
- USER-DISCLOSURE
- FULL-DISCLOSURE
- PAYLOAD-BINDING
- BATCH-EFFECT-ID
- ATOMIC-STATE
- SCAN-CURSOR
- RESOURCE-BOUND
- GLOBAL-COMMITMENT-UNIQUE
- ASSET-REGISTRY
- DISCLOSURE-BLINDING
- GLOBAL-SCAN-SEQUENCE
- ARTIFACT-CONSENSUS-IDENTITY

Session 3A/3B/4는 이 matrix를 code/test location으로 갱신함.

## 14. Threat Model 업데이트

- duplicate nullifier/commitment
- disabled-slot bypass
- NoteV1 cross-domain collision/confusion
- low-order/identity key
- aggregate root order/count/type confusion
- owner intent/payload substitution
- cross-chain/cross-circuit replay
- public count/grouping leakage
- zero-padding state spam
- remote prover whole-payroll disclosure
- invalid-proof CPU/RAM DoS
- ABCI/KV payload duplication과 scanner amplification
- batch atomic failure/item evidence mismatch
- ciphertext decryptability 비보장
- audit key compromise/epoch rotation
- development artifact/non-final setup

## 15. 실행 순서

1. Session 1 helper/completion record를 읽음.
2. NoteV1/domain/key invariant를 native helper와 cross-circuit fixture로 구현함.
3. Deposit/Spend/2x2/keeper Merkle/scanner를 NoteV1에 맞춰 갱신함.
4. minimal normative batch statement 초안을 작성함.
5. dominant full-shape prototype와 resource benchmark를 먼저 실행함.
6. feasibility gate가 실패하면 중단하고 multiproof 대안을 비교함.
7. gate 통과 후 12 public input/roots/disclosure/message contract를 동결함.
8. shared fixed-size encoding과 entire existing path를 갱신함.
9. exact max-shape protobuf/KV/event를 측정하는 wire/state feasibility gate를 실행함.
10. gate 실패 시 message/encoding/capacity 결정을 다시 설계하고 Session 3A를 중단함.
11. golden fixture/schema/traceability matrix를 완성함.
12. artifact loader와 prover admission foundation을 구현함.
13. keeper/event/scan/gas contract를 한영 문서에 동결함.
14. threat/security/JS SDK handoff를 갱신함.
15. 전체 regression/release documentation validation을 실행함.

## 16. 예상 산출물

```text
docs/clairveil-batch-joinsplit-16x32.md
docs/clairveil-batch-joinsplit-16x32-kr.md

x/privacy shared crypto/types
  note_v1.go
  digest256.go
  batch_vector_hash.go
  fixed_payload.go

x/privacy/circuit
  batch_joinsplit_16x32_feasibility_test.go
  note_v1_consistency_test.go

x/privacy/client/sdk/conformance/testdata
  privacy_note_v1_contract.json
  privacy_batch_joinsplit_v1_contract.json

x/privacy AssetRegistryV1 query/state
x/privacy unified privacy scan sequence and same-root path snapshot query

x/privacy/zk
  role-aware lazy artifact registry

x/privacy/client/sdk/proverservice
  bounded admission control
```

실제 패키지 위치는 dependency direction에 맞게 조정하되 책임을 축소하지 않음.

## 17. 검증

```bash
go test ./x/privacy/circuit -count=1
go test ./x/privacy/crypto ./x/privacy/types -count=1
go test ./x/privacy/client/sdk/deposit ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/withdraw -count=1
go test ./x/privacy/client/sdk/scan ./x/privacy/client/sdk/conformance -count=1
go test ./x/privacy/zk -count=1
go test ./x/privacy/client/sdk/proverservice ./x/privacy/client/sdk/provertransport -count=1
go test ./x/privacy/... -count=1
make examples
make privacy-e2e-smoke
make release-check
make release-pack
make release-pack-verify
git diff --check
```

feasibility report에는 hardware/OS/Go/gnark, warm/cold, sample 수를 기록함.

## 18. Commit 전략

1. `feat: define domain-separated note v1 primitives`
2. `test: validate batch joinsplit full-shape feasibility`
3. `docs: freeze batch joinsplit protocol contract`
4. `feat: use canonical fixed-size shielded payloads`
5. `refactor: load zk artifacts by circuit role`
6. `feat: bound prover admission and queueing`
7. `docs: record batch security and integration invariants`

## 19. 범위 밖

- production `BatchJoinSplit16x32` type
- `MsgBatchTransfer` RPC/keeper state transition
- batch SDK/prover route/scanner/payroll
- formal trusted setup
- external audit
- production gas final value

## 20. Acceptance Criteria

- [ ] NoteV1 commitment/nullifier/tree-node domain이 동결됨.
- [ ] Deposit/Spend/2x2/native/scanner cross-circuit vector가 일치함.
- [ ] key canonical/on-curve/non-identity/subgroup validation이 host/circuit에 정의됨.
- [ ] full-shape 16x32 prototype가 dominant constraints를 모두 포함함.
- [ ] single owner signature 하나만 사용함.
- [ ] feasibility report가 max-shape compile/setup/prove/RSS/artifact를 기록함.
- [ ] 12 public input 순서에 TBD가 없음.
- [ ] active-prefix/sentinel/distinctness/root contract가 고정됨.
- [ ] vector internal-node domain/level과 disabled input/output sentinel의 모든 field가 exact하게 고정됨.
- [ ] user/full disclosure와 optional self-view contract가 고정됨.
- [ ] disclosure digest가 per-output secret blinding을 포함하고 dictionary vector가 존재함.
- [ ] fixed-size encoding이 deposit/2x2/scanner/fixture에 적용됨.
- [ ] AssetRegistryV1이 asset ID/denom을 1:1로 검증하고 query함.
- [ ] structured batch message contract가 고정됨.
- [ ] minimal ABCI event와 typed KV scan index가 고정됨.
- [ ] 모든 privacy operation이 global sequence/cursor를 공유하고 typed query failure가 fail closed함.
- [ ] same-root batch path snapshot query/local provider가 정의됨.
- [ ] batch effect ID canonical formula와 golden vector가 고정됨.
- [ ] gas/state byte formula가 고정됨.
- [ ] max-shape wire/state feasibility gate가 tx/block/query/KV 한도를 통과함.
- [ ] consensus circuit identity와 validator local artifact mismatch가 fail closed함.
- [ ] fresh genesis/reset 및 cache/reservation invalidation handoff가 고정됨.
- [ ] artifact lazy loader와 prover admission이 구현됨.
- [ ] invariant traceability matrix가 존재함.
- [ ] 미해결 Critical/High design finding이 없음.
- [ ] master ledger가 갱신됨.

## 21. Session 3A Handoff

```text
## Completion Record

- 시작 commit:
- 완료 commit:
- NoteV1/domain/asset versions:
- fixed payload version/length:
- public input 순서:
- golden fixture 경로:
- full-shape constraint/resource 결과:
- max-shape wire/state 결과와 downstream limit:
- independent path vs multiproof 결정:
- artifact loader contract:
- prover admission defaults:
- invariant matrix 경로:
- 미해결 finding:
- Session 3A가 변경하면 안 되는 결정:
- worktree 상태:
```

full-shape feasibility 또는 design gate가 미완료이면 Session 3A를 시작하지 않음.
