# Session 2 Plan: NoteV1과 BatchJoinSplit16x32 기반/설계 확정

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | **Gate 2 PASS — S4-B02 frozen decision/implementation fresh closure complete** (2026-07-13) |
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

### 진입 Gate 검증 결과

2026-07-11에 사용자가 지정한 `ad99ef7193fdc0683e483e4440e5cda1f0945432`에서 시작했다. 시작 worktree는 clean이었고 Session 1 Completion Record와 Master Roadmap Gate 1을 실제 코드, 공격 회귀 테스트, artifact/state 계약과 대조했다. `go test ./x/privacy/... -count=1`을 재실행했고 authorization, duplicate nullifier/commitment, canonical crypto decoder, disclosure blinding, global commitment uniqueness, artifact identity, prover failover 경계를 독립 재검토했다. 미해결 Critical/High finding, 불완전한 필수 테스트, Completion Record와 코드의 실질적 불일치를 발견하지 않아 **Gate 1을 PASS**로 판정하고 Session 2를 시작했다.

### 1.1 2026-07-12 `S4-B02` foundation 재진입 결과

기준 HEAD `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`에서 clean worktree를 확인하고 latest Master Ledger와 Session 4 `BLOCKED` Completion Record를 우선했다. Historical `PUBLICATION_READY_EXPERIMENTAL` 판정은 현재 evidence로 사용하지 않았다. `S4-B03`은 `02f61f3746b67d5244c160b7c0e0e42f7c0b78b8`, `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`에서 해결된 상태를 유지한다.

`S4-B02`의 frozen invariant 이름은 `DISCLOSURE-BLINDING-SEPARATION` V1이며 disclosure output slot `i`별 exact contract는 다음과 같다.

```text
user_enabled[i] = enabled[i] && (privacy_policy[i] != 0)

DBS-01: user_enabled[i] => user_disclosure_blinding[i] != output_randomness[i]
DBS-02: enabled[i]      => full_disclosure_blinding[i] != output_randomness[i]
DBS-03: enabled[i]      => full_disclosure_blinding[i] != user_disclosure_blinding[i]
```

- enabled non-all-private: user/full blinding은 canonical non-zero이고 `DBS-01..03`을 모두 적용한다.
- enabled all-private: `privacy_policy=0`, `user_disclosure_blinding=0`을 canonicalize하고 `DBS-01`만 gate off한다. Full blinding은 canonical non-zero이고 `DBS-02`/`DBS-03`을 적용한다. Active output randomness는 canonical field element이면 zero도 허용한다.
- disabled Batch capacity slot: policy, output randomness, user blinding, full blinding을 모두 zero로 canonicalize하고 `DBS-01..03`을 모두 gate off한다.
- JoinSplit2x2에는 disabled output slot이 없다. Disclosure witness는 recipient output `0`에만 대응하며 output `1`은 disclosure witness가 없는 active change note다. BatchJoinSplit16x32는 같은 의미를 32개 slot에 독립 적용해 gated inequality site 96개를 이미 가진다. Cross-output/input/transaction global freshness는 이 세 relation보다 강한 별도 SDK 정책이다.

공유 enforcement/error contract는 circuit, `ValidateDisclosureBlindingSeparationV1`, prepared validator, signature release 전 structured signer에 적용한다. Stable secret-free code는 `DBS_INVALID_POLICY`, `DBS_NON_CANONICAL_FIELD`, `DBS_DISABLED_SENTINEL`, `DBS_ALL_PRIVATE_USER_SENTINEL`, `DBS_USER_BLINDING_REQUIRED`, `DBS_FULL_BLINDING_REQUIRED`, `DBS_USER_RANDOMNESS_REUSE`, `DBS_FULL_RANDOMNESS_REUSE`, `DBS_USER_FULL_BLINDING_REUSE`다. Session 3A는 opaque 2x2 signer를 `JoinSplitOwnerIntentSigningRequestV1` 기반 structured request로 교체하고 callback 전에 같은 semantic validator를 실행했다. 어느 host 검증도 production circuit constraint를 대체하지 않는다.

Public input 순서, 13-input schema SHA-256 `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, NoteV1, payload encoding/version, disclosure digest/domain, circuit-set ID `privacy-note-v1`, manifest/identity schema는 변경하지 않는다. Session 3A는 JoinSplit accepted witness set 변경으로 `privacy_joinsplit_{r1cs,pk,vk}.bin`, manifest checksum, consensus JoinSplit `verifying_key_sha256`만 교체하고 old JoinSplit proof/job을 폐기하며 exact readiness와 fresh-genesis/reset을 다시 수행한다. Batch source/artifact는 이 finding 때문에 바꾸지 않는다.

Foundation 시점 test-only hardened circuit의 exact target은 당시 production `99,765` 대비 `99,775` constraints(`+10`, 약 `0.0100%`)였다. Cold development sample에서 R1CS `10,823,916 -> 10,824,169 B`, PK `16,765,577 -> 16,766,489 B`, VK `748 -> 748 B`, proof `164 -> 164 B`, peak RSS `690,438,144 B`였고 OOM은 없었다. Session 3A는 production `99,775`를 exact 재현하고 full Batch gate를 unchanged `1,111,837` constraints로 다시 실행했으므로 decision change는 없다.

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
- Active non-all-private user blinding과 모든 active full blinding은 output별 CSPRNG 값이다. Note randomness/서로에 대한 exact inequality와 all-private/disabled sentinel 예외는 §1.1의 `DBS-01..03` gating을 따른다.
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

Session 2에서 `lp(x) = u32be(len(x)) || x`로 두고 canonical owner-effect bytes를 다음처럼 동결함. 각 `output_effect_i`는 위 `BatchTransferOutput` field를 정확히 선언 순서대로 encoding하며 policy/mode는 `u32be`, 나머지 byte field는 `lp`를 사용함.

```text
canonical_batch_payload_v1 =
  u32be(1) || lp(root) ||
  u32be(input_count) || lp(nullifier_0) || ... || lp(nullifier_{input_count-1}) ||
  u32be(output_count) || output_effect_0 || ... || output_effect_{output_count-1} ||
  lp(audit_key_id) || u64be(audit_key_epoch) ||
  lp(audit_disclosure_target_pubkey) || u64be(expires_at_unix)

payload_sha256 = SHA-256(
  "clairveil.batch-transfer-payload.v1" || canonical_batch_payload_v1
)
```

`PayloadDigestHi/Lo`는 위 SHA-256의 앞/뒤 16 bytes를 non-reduced unsigned big-endian `uint128`로 해석함. empty optional field는 `lp(empty)=u32be(0)`이며 self-view는 batch-level all-or-none임. independent 2x2 golden과 max 16x32 payload size는 Completion Record 및 한영 normative 문서에 기록함.

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

- [x] NoteV1 commitment/nullifier/tree-node domain이 동결됨.
- [x] Deposit/Spend/2x2/native/scanner cross-circuit vector가 일치함.
- [x] key canonical/on-curve/non-identity/subgroup validation이 host/circuit에 정의됨.
- [x] full-shape 16x32 prototype가 dominant constraints를 모두 포함함.
- [x] single owner signature 하나만 사용함.
- [x] feasibility report가 max-shape compile/setup/prove/RSS/artifact를 기록함.
- [x] 12 public input 순서에 TBD가 없음.
- [x] active-prefix/sentinel/distinctness/root contract가 고정됨.
- [x] vector internal-node domain/level과 disabled input/output sentinel의 모든 field가 exact하게 고정됨.
- [x] user/full disclosure와 optional self-view contract가 고정됨.
- [x] disclosure digest가 per-output secret blinding을 포함하고 dictionary vector가 존재함.
- [x] fixed-size encoding이 deposit/2x2/scanner/fixture에 적용됨.
- [x] AssetRegistryV1이 asset ID/denom을 1:1로 검증하고 query함.
- [x] structured batch message contract가 고정됨.
- [x] minimal ABCI event와 typed KV scan index가 고정됨.
- [x] 모든 privacy operation이 global sequence/cursor를 공유하고 typed query failure가 fail closed함.
- [x] same-root batch path snapshot query/local provider가 정의됨.
- [x] batch effect ID canonical formula와 golden vector가 고정됨.
- [x] gas/state byte formula가 고정됨.
- [x] max-shape wire/state feasibility gate가 tx/block/query/KV 한도를 통과함.
- [x] consensus circuit identity와 validator local artifact mismatch가 fail closed함.
- [x] fresh genesis/reset 및 cache/reservation invalidation handoff가 고정됨.
- [x] artifact lazy loader와 prover admission이 구현됨.
- [x] invariant traceability matrix가 존재함.
- [x] 미해결 Critical/High design finding이 없음.
- [x] master ledger가 갱신됨.
- [x] `S4-B02` re-entry가 `DBS-01..03`, all-private/disabled gating, shared error/layer contract, negative fixture를 TBD 없이 동결함.
- [x] 2x2 hardened feasibility target과 JoinSplit-only artifact identity 갱신 범위를 재현 가능하게 기록함.
- [x] production `JoinSplitCircuit`/R1CS/VK와 structured pre-sign boundary가 frozen invariant를 강제함. Session 3A `0b7d97d`, `630736f`, `25c17ef`에서 target/identity/readiness/resource gate를 완료함.

## 21. Session 3A Handoff

## Completion Record

### 2026-07-13 Gate 2 Fresh Closure

- `DISCLOSURE-BLINDING-SEPARATION` V1의 `DBS-01..03`, all-private sentinel, shared native/prepared/pre-sign error contract와 production `99,775` constraints(`+10`)를 independent full-scope review와 JoinSplit resource gate에서 재확인했다.
- Public input 13개/schema hash, NoteV1, prepared payload `v5`, proof/HTTP `v2`, disclosure digest/domain, circuit-set ID는 unchanged다. Batch는 unchanged `1,111,837` constraints로 full-shape resource gate를 통과했다.
- `G123A-AR02` actual artifact-byte 검증, exact current-source R1CS serialization 결합과 negative regression은 foundation의 JoinSplit-only rotation 결정을 fail-closed evidence로 보강하며 decision change를 만들지 않는다.
- 처분: **Gate 2 PASS.** Gate 1/3A도 PASS했고 Session 3B re-entry는 **UNBLOCKED**다. Gate 3B/S4-B01, Session 4/publication은 계속 **BLOCKED**다.

### 2026-07-13 `G123A-AR01`/`DOC01`/`RP01` 보완

- fresh review task `019f56bb-7962-7210-a4b5-f01c7a47a4b8`, clean 기준 HEAD `def4f8405a22011eb4d73b1e1bbfba68fec82b60`에서 시작했다. 작업 단위 commit은 `G123A-AR01` `57670bbfeff9d2fcb7bcfc7ba85cf4caedfb5b90`, `G123A-DOC01` `46dbb754549d07b162935aade59ba8827b968c91`, `G123A-RP01` `0d65faa2efe45d606864251d050c0679f0109716`이다.
- Historical G123A 보완에서는 ephemeral `/tmp/clairveil-g123a.T24SZF/{current,previous}` supplied set으로 `make session-3a-validation-evidence`를 실행했다. Current closure target은 이 경로에 의존하지 않으며 clean commit에서 pinned prior/current source로 complete set을 재생성한다. Exact test가 실제 실행되어야 하며 test 미발견, `SKIP`, `[no tests to run]`을 성공으로 인정하지 않는다.
- `S4-B03` 명령 `go test ./x/privacy/circuit ./x/privacy/types ./x/privacy/keeper -run '^(TestJoinSplitCircuitRejectsExactDuplicateInputInflation|TestBatchJoinSplit16x32RejectsExactDuplicateInputInflation|TestMsgTransferValidateBasicRejectsExactDuplicateInputInflation|TestMsgServerTransferRejectsExactDuplicateInputInflationBeforeProof)$' -count=1 -v`와 `S4-B02` 명령 `go test ./x/privacy/circuit ./x/privacy/types ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/conformance -run '^(TestJoinSplitCircuitEnforcesDisclosureBlindingSeparationV1|TestValidateDisclosureBlindingSeparationV1|TestJoinSplitStructuredSigningBoundaryRejectsDisclosureReuseBeforeRelease|TestGenerateTransferDisclosureBlindingsV1RetriesExactReuse|TestValidatePreparedTransferPayloadMetadataRejectsDisclosureBlindingReuse|TestValidatePreparedTransferPayloadMetadataCanonicalizesAllPrivateUserBlinding|TestPrivacyDisclosureBlindingV1Contract)$' -count=1 -v`가 PASS했다. 한영 공개 문서와 invariant traceability는 S4-B02/B03 구현·regression 완료와 fresh Gate 재검토 필요 상태로 정렬했고 stale signer test-file 참조를 실제 `x/privacy/client/sdk/transfer/payload_test.go` 회귀로 교정했다.
- `go test ./x/privacy/... -count=1`, `go test ./... -count=1`, `go vet ./x/privacy/...`, `make build`, `make examples`, `make release-check`, `make release-pack`, `git diff --check`, 한영 문서 pair/stale reference 검사가 모두 PASS했다.
- 동일 archive `clairveil-handoff-v0.1.0-142-g0d65faa.tar.gz`(SHA-256 `e5dbb48638ab621acfcf396ec89d0f18e5d66827f94c148c48e8a2d4a5f04960`)를 기본 Python `3.9.6`과 `3.12.8`의 `make release-pack-verify`로 각각 검증해 required file `125`개와 exact commit `0d65faa2efe45d606864251d050c0679f0109716`을 확인했다. CI도 Python `3.9`/`3.12` matrix로 같은 target을 실행한다.
- `git diff --name-only def4f84..0d65faa -- x proto`는 비어 있다. `JoinSplitCircuit` constraint, public input, NoteV1, payload/schema/version, circuit/artifact identity, Batch source/artifact와 tracked R1CS/PK/VK를 변경하거나 회전하지 않았다.
- 처분: G123A 보완 완료는 Gate PASS가 아니다. Gate 1/2/3A fresh 독립 재검토가 필요하며 `G3B-01..04`/`S4-B01`은 시작하지 않았다. Session 3B와 publication은 계속 **BLOCKED**다.
- worktree: 이 Completion Record commit과 exact release-pack 재검증 뒤 clean이며 generated artifact/secret은 tracked되지 않는다.

### 2026-07-12 `S4-B02` Session 3A Implementation Supplement

- Session 2의 `2026-07-12 S4-B02 Foundation Re-entry`를 authoritative contract로 사용했고 `DBS-01..03`, all-private/disabled gating, stable `DBS_*` code, public input/NoteV1/payload/disclosure/circuit-set 계약을 변경하지 않았다.
- 기준 HEAD `0fc818c`; production relation commit `0b7d97d`, structured 2x2 pre-sign commit `630736f`, JoinSplit-only artifact rotation commit `25c17ef`.
- Production count는 historical control `99,765` 대비 `99,775`(`+10`)로 frozen target과 exact 일치해 decision change가 없다. Current control acceptance와 각 hardened rejection은 완전한 digest/signature를 재계산한 production regression으로 원인 분리했다.
- 새 development JoinSplit R1CS/PK/VK SHA-256은 각각 `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`이며 VK hash가 consensus JoinSplit identity다. `gnark v0.14.0`, Groth16/BN254 development setup에서 `clairveil-setup -circuit joinsplit -overwrite`로 생성했다.
- Non-JoinSplit 9개 artifact와 Batch source/artifact는 byte-identical하다. Old/new proof와 consensus/file mismatch, fresh genesis/reset, strict preflight, 2x2 regression 및 full Batch `1,111,837` constraint resource gate가 통과했다. Formal trusted setup은 수행하지 않았고 binary/secret은 tracked하지 않았다.
- 처분: `S4-B02` implementation **RESOLVED**. Foundation decision은 그대로이며 Gate 1/2/3A는 fresh 독립 재검토가 필요하다. Gate 3B와 Session 4는 재개하지 않았고 Session 3B 작업도 시작하지 않았다.

### 2026-07-12 `S4-B02` Foundation Re-entry

- 시작 commit/worktree: `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`, branch `private/multi-circuit-b`, tracked/untracked worktree clean을 확인했다.
- 상태 기준: latest Master Ledger와 Session 4 `BLOCKED` Completion Record가 authoritative다. `S4-B03`은 `02f61f3`, `42d40bd`에서 **RESOLVED**이고 `S4-B02`만 이 재진입의 대상이다. Historical publication-ready record는 현재 판정으로 사용하지 않았다.
- 작업 commit:
  - `c7fc1be`: shared native invariant/error contract, 2x2 collision-retrying generator/prepared guard, independent conformance fixture와 negative vector.
  - `a8697cd`: current-vs-hardened 2x2 control/feasibility circuit과 opt-in resource gate.
  - `a4ee959`: prepared/native typed error mapping과 exact canonicalization/relations conformance assertion 보강.
  - `4e75f1f`: 한영 normative circuit/SDK/security/testing/schema/traceability 문서 동결.
  - `4e90223`: Session 2 re-entry Completion Record, Master Ledger, Session 3A handoff, Session 4 `BLOCKED` supplement 정렬.
- frozen contract: `DISCLOSURE-BLINDING-SEPARATION` V1의 `DBS-01..03`과 enabled non-all-private, enabled all-private, disabled Batch slot의 exact sentinel/gating은 §1.1이 authoritative하다. JoinSplit2x2는 output `0`만 적용하고 output `1`을 disabled slot으로 취급하지 않는다.
- 공유 contract: circuit/native/prepared/structured-signer는 동일한 relation과 stable secret-free `DBS_*` code를 사용해야 한다. Foundation code는 native/prepared/SDK/conformance를 구현했다. Production circuit과 독립 structured signer enforcement는 Session 3A implementation pending이다. Batch structured signer `G3B-04`는 별도 finding이며 변경하지 않았다.
- fixture/negative evidence: `privacy_disclosure_blinding_v1_contract.json`이 canonical/non-canonical, all-private/disabled sentinel, zero requirement, `DBS-01..03` 실패를 code와 함께 고정한다. Prepared validator와 current-vs-hardened circuit test가 각 relevant negative를 거부/대조한다.
- interface/version 영향: public input/순서, NoteV1, canonical payload/envelope, disclosure digest/domain, protobuf, transfer payload `v5`, proof/HTTP `v2`, manifest `v2`, identity `v1`, circuit set `privacy-note-v1`은 변경하지 않는다. JoinSplit R1CS/PK/VK와 manifest/consensus JoinSplit identity만 Session 3A에서 교체한다. Batch artifact delta는 없다.
- feasibility/resource: current `99,765` constraints, test-only target `99,775`(`+10`); R1CS `+253 B`, PK `+912 B`, VK/proof size 변화 없음, peak RSS `690,438,144 B`, OOM 없음. Single cold timing은 feasibility sample이며 성능 개선 claim이 아니다. Batch production source는 unchanged `1,111,837` constraints다.
- 검증: targeted native/prepared/conformance tests와 전체 circuit package가 통과했다. Closure에서 `go test ./x/privacy/... -count=1`, `go vet ./x/privacy/...`, `make examples`, `git diff --check`가 모두 통과했다. Opt-in resource gate를 두 번째 실행해 constraint/artifact byte delta가 정확히 재현됐고 두 cold run peak RSS는 `690,438,144 B`, `690,536,448 B`였으며 OOM은 없었다. Session 3B live E2E/release publication gate는 이 범위에서 실행하지 않았다.
- 영향 파일: `x/privacy/types/disclosure_blinding.go`, transfer prepared/generator 경로, disclosure conformance fixture/test, test-only 2x2 feasibility file, 한영 normative circuit/SDK/security/testing/schema 문서, 이 Session 2 record와 Master Ledger/Session 3A/Session 4 status record. Production `x/privacy/circuit/joinsplit.go`, tracked R1CS/PK/VK, MsgBatchTransfer/keeper, Session 3B signer/payroll/live E2E는 변경하지 않았다.
- Session 3A re-entry: **UNBLOCKED FOR `S4-B02` IMPLEMENTATION / NOT STARTED**. Exact target을 구현하고 JoinSplit artifact identity를 교체할 수 있지만 이 세션에서는 시작하지 않았다.
- disposition: `S4-B02`는 **IMPLEMENTATION PENDING**, **NOT RESOLVED**. Production circuit/artifact replacement와 해당 regression/identity/resource gate가 끝날 때까지 Gate 1, Gate 4, publication은 `BLOCKED`다.
- worktree: Completion Record/Ledger closure commit 뒤 `git status --short --branch` clean을 확인하며 generated production R1CS/PK/VK와 secret은 만들거나 track하지 않는다.

### Historical Session 2 Completion

- 시작 commit: `ad99ef7193fdc0683e483e4440e5cda1f0945432`. 시작 시 branch HEAD가 exact commit이고 tracked worktree가 clean임을 확인했다.
- 완료 commit: 핵심 Session 2 contract 동결은 `43d0e8d`, Completion Record/공개 문서 정리는 `3862b13`, 최종 review-fix 완료 기준은 `f117b4f8487c78b6531efe2be1ecccccefe6c5c1`이다. 이 commit reference를 확정하는 최종 bookkeeping은 후속 문서 commit으로만 수행한다.
- 후속 `review-fix-loop` hardening: `ad99ef7193fdc0683e483e4440e5cda1f0945432..HEAD` 전체 재검토에서 slash-containing denom의 REST deep-wildcard binding, reserve/AssetRegistry genesis 교차 검증, public historical path rebuild의 resource asymmetry와 cached-root 우회, Note memo validation/직렬화 오류 은폐를 수정했다. Historical query는 persisted snapshot metadata만 사용하고 최대 1,024 leaves, keeper당 동시 rebuild 2개, context cancellation을 강제하며 초과/admission full은 `ResourceExhausted`로 fail closed한다. Current-root query는 persisted incremental node만 읽고 cached root 누락 시 `FailedPrecondition`으로 offline repair를 요구하며 query-time rebuild/state write를 수행하지 않는다. `NewNote`/`ValidateV1`/`MarshalNotePlaintextV1`은 invalid UTF-8과 128-byte 초과 memo를 동일하게 거부하고 silent `Note.Bytes()`는 제거했다. Offline recovery/export의 `MaxMerkleRebuildLeaves`(1,048,576) 계약과 16/32 capacity/security constraint는 변경하지 않았다.
- 후속 review 결과: fresh rediscovery 6 rounds와 fix batch 4회를 수행해 P1 1건, P2 4건을 해결했다. 마지막 두 fresh rounds는 각각 독립 reviewer 2명 모두 active/unresolved finding 0건이었고 main adjudication도 clean으로 판정해 deep-mode consecutive clean `2/2`를 충족했다. `examples/clairveil-dapp`는 사용자 지시에 따라 수정 범위에서 제외했다.
- Gate 1 재검증: Session 1 Completion Record와 Master Roadmap을 코드에 대조하고 `go test ./x/privacy/... -count=1`을 통과했다. authorization, inflation/duplicate, disclosure oracle, canonical crypto decode, global commitment uniqueness, artifact identity, prover failover 범위를 독립 검토했으며 Critical/High 또는 필수 테스트/문서 불일치가 없어 Gate 1을 PASS로 판정했다.
- NoteV1/domain/asset version:
  - active circuit set `privacy-note-v1`, module consensus/state version `2`, Note tree depth `32`, asset registry `privacy-asset-registry-v1`.
  - `domain_field(label) = SHA-256("clairveil.field-domain.v1" || u32be(len(label)) || label) mod Fr`; commitment/nullifier/tree label과 field constant는 한영 normative 문서와 fixture에 동결했다.
  - asset ID는 `SHA-256("clairveil.asset-id.v1" || u32be(len(denom)) || denom) mod Fr`; `uclair` ID는 `238d5f23e4d918d40b0982ce3aef16a75c4d1760193d1c3b30b9f5df681903ca`다.
  - independent NoteV1 commitment `023aab554dcb995210888fa4e28c3d718568c1de0623578c690a2b6ca9d3610a`, nullifier `13b50fceae57ce77eee3f686abc1563aadc27ff6d1e32ce2fcc599463d28585b`, depth-32 empty root `057551a52590c07629bf07fa2b61832f852fb69ff8472bb21c30e5675ae8e8c1`.
- fixed payload/scan version과 길이:
  - `privacy-fixed-v1`, binary version `1`; Note plaintext `350 B`, disclosure plaintext `392 B`, envelope header `20 B`.
  - exact envelope는 symmetric deposit note `398 B`, ECIES transfer note `430 B`, user/audit/self-view disclosure `472 B`이며 legacy JSON/raw fallback은 없다.
  - scan schema `privacy-scan-v2`, global sequence `privacy-sequence-v1`, cursor `(height, global_sequence, output_index)`.
- public input 순서: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`의 정확한 12개 순서를 동결했다. reserved schema SHA-256은 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`이다.
- canonical batch payload: format `1`, SHA-256 domain `clairveil.batch-transfer-payload.v1`. `u32be` count/length framing으로 root, ordered nullifier/output effect 전체, audit ID/epoch/target, expiry를 묶고 `creator`/`proof`만 제외한다. Independent 2x2 vector는 `3,702 B`, digest `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`, hi `322132945931579789235567236199104333743`, lo `14314064343031468430392382204273370288`이다.
- golden fixture 경로: `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`, `privacy_batch_joinsplit_v1_contract.json`; 한영 normative contract와 traceability matrix는 `docs/clairveil-batch-joinsplit-16x32.md`, `docs/clairveil-batch-joinsplit-16x32-kr.md`다.
- full-shape circuit feasibility 결과:
  - 2026-07-11 Apple M5 Pro/RAM 64 GiB/macOS 26.5.1, `darwin/arm64`, Go `1.25.12`, gnark `0.14.0`, gnark-crypto `0.19.2`, BN254 Groth16 development setup에서 측정했다.
  - corrected 16x32 prototype `1,111,837` constraints, current JoinSplit2x2 `99,765`; subgroup point `67`, subgroup incremental `161,202` constraints.
  - compile `1,047.684 ms`, setup `17,160.691 ms`, R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`, peak RSS `3,339,862,016 B`(`~3.11 GiB`).
  - `16/32` warm prove `[1,791.545, 1,785.570] ms`, mean `1,788.5575 ms`, verify `[0.699, 0.677, 0.698] ms`; `55.892422 ms/output`으로 2x2 baseline 대비 `2.788813x` 개선했다. `1/1`, `3/4`, `8/16`, `16/32` 모두 OOM 없이 compile/setup/prove/verify를 통과했다.
- max-shape wire/state feasibility 결과:
  - canonical owner-effect payload `65,384 B`, actual prototype message `65,060 B`, signed Cosmos `TxRaw` `65,294 B`, summary `788 B`, 32 output records `73,628 B`, key 포함 scan KV `75,105 B`, tree allowance `98,304 B`, total KV write `173,409 B`, minimal event `584 B`, max query response `74,551 B`.
  - reference limit `1 MiB` tx, `21 MiB` block bytes, `4 MiB` gRPC/query body, `256 KiB` scan/total KV, `16 KiB` event를 모두 통과했다.
- independent path vs multiproof 결정: security constraint와 16개의 independent depth-32 path를 유지한 full-shape gate가 통과했으므로 constrained multiproof는 Gate 2 필수 대안이 아니다. Current root는 incremental provider를 사용한다. 모든 non-current historical root는 exact persisted root/count/height snapshot과 deterministic prefix rebuild를 사용하며 public query는 최대 `1,024` leaves와 keeper당 동시 rebuild 2개만 허용하고 초과 시 `ResourceExhausted`를 반환한다. Offline recovery/export는 별도 `MaxMerkleRebuildLeaves`(`1,048,576`) bound를 유지하며 더 큰 online request는 archival/local provider를 요구한다.
- state/scan/registry 계약:
  - Deposit, Withdraw, JoinSplit2x2와 future batch가 하나의 summary-driven global sequence/cursor를 공유하며 zero-output Withdraw, page 중간 resume, missing/extra/non-adjacent output, malformed envelope를 exact하게 처리한다.
  - commitment index는 operation 전체에서 global이며 malformed stored index/read error를 absent로 축약하지 않는다. AssetRegistryV1 export는 forward/reverse namespace를 모두 순회하여 orphan/malformed reverse entry도 fail closed한다.
  - 모든 append가 `(root,height,leaf_count)` snapshot을 영속하고 genesis export/import가 registry, scan, sequence, tree/index/snapshot, reserve와 circuit identity를 보존한다.
- artifact loader contract: injectable/thread-safe registry가 validator에서는 requested VK만, prover에서는 selected R1CS/PK만 lazy load한다. consensus의 `circuit_set_id`, VK SHA-256, public-input schema SHA-256과 local manifest/artifact가 다르면 readiness가 실패하며 development override는 production에서 거부된다. reserved batch identity는 production required circuit 목록에 등록하지 않았다.
- prover admission/gas contract: circuit별 default in-flight `1`, queue `4`, body `8 MiB`; cheap framing 후 semantic/cryptographic work 전에 permit을 얻고 실제 gnark prove return까지 보유한다. queue full은 `429`, `code="busy"`, `retryable=true`이며 automatic multi-prover failover는 off다. gas model은 verify, input/output, canonical payload byte, typed state byte, tree write, global lookup을 positive bounded coefficient와 overflow check로 계량한다.
- 실행한 검증과 결과:
  - targeted circuit/types/crypto/keeper/SDK/scanner/artifact/admission/gas/wire tests와 `go test ./x/privacy/... -count=1`: 통과.
  - opt-in full gate `/usr/bin/time -l env CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v`: 통과; 위 corrected resource 수치를 생성했다.
  - `go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v`: 통과; 위 corrected wire/state 수치를 생성했다.
  - `go vet ./x/privacy/...`: 통과. prover service/transport/zk race test 통과(macOS linker의 known `LC_DYSYMTAB` warning만 관찰).
  - `go test ./x/privacy/keeper -run '^$' -bench '^BenchmarkHistoricalPathRebuildWorkBudget$' -benchmem -count=3`: Apple M5 Pro에서 1,024-leaf online budget이 `39.47~39.90 ms/op`, `2,085,142~2,085,248 B/op`, `46,378~46,379 allocs/op`로 측정됐다. 초기 4,096-leaf 후보는 `159.81~160.92 ms/op`, `8,354,444~8,355,243 B/op`여서 공개 RPC budget으로 채택하지 않았다.
  - 후속 review-fix 표준 검증: `examples/clairveil-dapp`를 제외한 전체 `go test -count=1`/`go vet`, keeper/types/scanner/prover-admission 핵심 `-race`, `make proto`, `git diff --check`가 통과했다. macOS linker의 known `LC_DYSYMTAB` warning만 관찰됐고 test/race failure는 없었다.
  - 최종 status audit에서 max-shape wire/state gate, NoteV1/batch independent golden conformance, vector root, user-disclosure root, canonical payload golden을 다시 실행해 모두 통과했다. Normative 한영 문서와 이 계획에서 구현 결정을 남기는 `TBD`는 없으며 남은 `TODO`는 gnark hard cancellation을 위한 production process isolation뿐이다.
  - `make proto` 후 generated diff 없음, `make examples`, `make privacy-e2e-smoke`, `make release-check`: 통과. release-check가 full Go/CLI/examples, vuln policy, localnet, privacy E2E, 2-batch bulk readiness를 통과했다.
  - `make release-pack`, `make release-pack-verify`, `git diff --check`: 통과.
- 미해결 finding과 residual risk:
  - Session 2의 미해결 P0/P1/P2 및 Critical/High finding은 `0`건이다. 독립 final review도 같은 판정을 내렸다.
  - `BatchJoinSplit16x32`와 wire proto는 feasibility prototype/reserved schema다. production circuit/artifact, `MsgBatchTransfer`, keeper handler, batch SDK/prover route/scanner/payroll, formal trusted setup과 외부 audit는 구현하지 않았다.
  - final R1CS/PK와 약 3.11 GiB peak RSS는 lazy loader와 production capacity/process isolation을 요구한다. client cancellation은 in-process gnark solver를 중단하지 못한다.
  - external ClairveilJS는 `privacy-fixed-v1`을 아직 decode하지 못하며 conformance test가 fail-closed 동작만 확인한다. downstream은 compatibility fallback 없이 새 fixture/encoder로 갱신해야 한다.
  - historical internal node를 영속하지 않으므로 non-current root의 public query는 1,024-leaf/동시 2개 online admission bound를 가진다. Offline recovery/export의 1,048,576-leaf bound는 별도다. Remote path query는 input-note linkage를 노출할 수 있다.
  - current 2x2 SDK는 note/user/full blinding을 독립 CSPRNG로 생성하지만 batch prototype의 세 equality처럼 circuit에서 재사용을 별도로 금지하지 않는다. 정상 SDK flow의 collision은 negligible이나 future hardening 후보로 남긴다.
  - `QueryPrivacyScanResponse.encoded_bytes`는 record proto 합계이고 wrapper/tag overhead는 별도 actual-response 측정으로 보완했다. 둘 다 4 MiB gate에 충분한 margin이 있다.
  - public count/grouping/timing, ciphertext decryptability 비보장, remote prover witness exposure, audit key/epoch 운영, AssetRegistry governance message와 production gas coefficient/state pruning은 후속 risk다.
  - no-fixed-version `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`와 example npm low 1건은 기존 exact policy/known risk로 계속 추적한다.
- Session 3A 진입 Gate: **Gate 2 PASS, Session 3A Unblocked (Not Started)**. Session 3A 작업은 이 세션에서 시작하지 않았다.
- Session 3A가 변경하면 안 되는 결정: 16/32 capacity, security constraint/subgroup check/independent membership, NoteV1와 exact empty tree, 12 public input 순서, active-prefix/disabled sentinel, typed vector leaf/node/root, two-stage user disclosure와 per-output full/user blinding, fixed payload framing, AssetRegistryV1/global uniqueness, unified scan cursor/same-root snapshot, consensus artifact identity, bounded admission/gas category를 임의로 약화·재해석하면 안 된다.
- worktree 상태: Completion Record/Ledger·공개 문서 commit과 release-pack 검증 후 tracked worktree clean. `dist/` release pack과 `benchmarks/` 측정 summary는 gitignored 검증 산출물이고 generated R1CS/PK/VK 또는 secret은 tracked되지 않는다.

full-shape feasibility 또는 design gate가 미완료이면 Session 3A를 시작하지 않음.
