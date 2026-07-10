# Session 3A Plan: BatchJoinSplit16x32 Circuit/Chain Core 구현

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Blocked by Session 2 |
| 선행 문서 | [Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md), [Session 2](clairveil-batch-joinsplit-16x32-session-2-foundation-kr.md) |
| 후속 세션 | [Session 3B Integration](clairveil-batch-joinsplit-16x32-session-3b-integration-kr.md) |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `max` |
| 완료 목표 | production batch circuit, message, keeper, gas, atomic state, scan index, artifact contract를 구현함 |

## 1. 진입 Gate

- final NoteV1과 cross-circuit vectors가 동결됨.
- full-shape feasibility prototype가 16/32 max witness를 성공적으로 prove함.
- max-shape protobuf tx, KV scan state, event/query가 target chain의 hard limit을 통과함.
- 12개 public input 순서가 확정됨.
- active prefix, aggregate roots, single owner signature, disclosure, payload digest contract에 TBD가 없음.
- structured message, minimal event, typed KV scan index, gas formula가 확정됨.
- invariant traceability matrix가 존재함.
- global scan sequence/path snapshot, AssetRegistryV1, consensus artifact identity 계약이 동결됨.
- 미해결 Critical/High design finding이 없음.

```bash
git status --short --branch
git log -10 --oneline
go test ./x/privacy/... -count=1
make release-pack-verify
```

Session 2 normative contract를 바꿔야만 구현 가능한 문제가 나오면 임의 변경하지 않음. decision change proposal과 vector/security/resource 영향을 먼저 기록함.

## 2. 범위

이 세션은 consensus/core 경계를 구현함.

```text
production circuit
  -> proto/types
  -> keeper validation/gas/proof verification
  -> atomic nullifier/commitment writes
  -> typed scan state
  -> minimal ABCI summary event
  -> artifact setup/manifest/readiness
```

Go product SDK, remote prover HTTP route, wallet scanner, payroll adapter, CLI tutorial은 Session 3B 범위임. 다만 core test에서 proof를 만들 수 있는 internal fixture/witness builder는 구현함.

## 3. Workstream A: Production Circuit

### 3.1 파일

```text
x/privacy/circuit/batch_joinsplit_16x32.go
x/privacy/circuit/batch_joinsplit_16x32_test.go
x/privacy/circuit/batch_joinsplit_16x32_bench_test.go
```

```go
const (
    MaxBatchJoinSplitInputs  = 16
    MaxBatchJoinSplitOutputs = 32
)
```

기존 2x2 `NumInputs/NumOutputs`를 재사용하지 않음.

### 3.2 Public witness 순서

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

struct declaration, public witness builder, docs, fixture에서 순서를 동일하게 유지하고 regression test로 고정함.

### 3.3 Secret witness

- common `AssetID`
- 16 input NoteV1 field
- 16 Merkle path/helper
- single `OwnerSignature`
- 32 output NoteV1 field
- output별 user policy와 disclosure 계산 field
- output별 `user_disclosure_blinding`과 `full_disclosure_blinding`
- disabled canonical sentinel

ciphertext bytes는 circuit witness로 넣지 않음. PayloadDigest limbs와 owner intent가 exact message bytes를 묶음.

### 3.4 Constraint 순서

1. input/output count를 range-constrain하고 enabled prefix를 유도함.
2. input NoteV1 commitment/path/membership을 계산함.
3. input nullifier와 vector root를 계산함.
4. active nullifier pairwise distinctness를 적용함.
5. active owner/asset equality와 key invariant를 적용함.
6. output NoteV1 commitment와 vector root를 계산함.
7. active output commitment distinctness를 적용함.
8. input/output amount range와 conservation을 적용함.
9. output별 user/full disclosure roots를 계산함.
10. batch intent를 계산하고 single owner signature를 검증함.
11. disabled witness canonicalization을 적용함.

vector 내부 node는 type/level-separated domain을 사용하고 disabled input/output의 모든 field는 Session 2 sentinel 표와 exact match해야 함. active commitment/nullifier `0`은 거부함.

security constraint를 host-only 검사로 이동하지 않음.

### 3.5 Positive matrix

- 1 input / 1 output
- 1 input / 2 outputs with change
- 3 input / 4 output
- 8 input / 16 output
- 16 input / 31 recipient + change
- 16 input / exact 32 outputs
- mixed user disclosure
- self-view enabled/disabled
- explicit zero-value output padding

### 3.6 Negative matrix

- count 0/max+1/out-of-range limb
- sparse/non-canonical active representation
- disabled non-zero amount/randomness/path/helper
- invalid Merkle path
- duplicate active nullifier at adjacent/non-adjacent positions
- duplicate active commitment
- wrong owner/view key/asset
- identity/low-order/off-curve key
- amount overflow or conservation mismatch
- altered vector/disclosure root
- altered payload digest limbs
- wrong chain/circuit domain
- invalid owner signature
- reordered outputs/disclosures
- disclosure blinding 제거/재사용/변조
- disabled slot의 non-canonical key/policy/blinding
- vector node type/level confusion

## 4. Workstream B: Proto/Types

### 4.1 Proto

`proto/clairveil/privacy/v1/tx.proto`에 추가함.

```text
rpc BatchTransfer(MsgBatchTransfer) returns (MsgBatchTransferResponse)
MsgBatchTransfer
BatchTransferOutput
MsgBatchTransferResponse
QueryAssetByID / QueryAssetByDenom
QueryCommitmentPathsAtRoot
QueryPrivacyScan
```

normative fields:

```text
MsgBatchTransfer
  creator
  proof
  root
  nullifiers[]
  outputs[]
  audit_disclosure_target_pubkey
  audit_key_id
  audit_key_epoch
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

count는 repeated field length에서 유도함.

### 4.2 ValidateBasic

- valid creator
- proof required
- canonical root
- `1..16` canonical/distinct nullifiers
- `1..32` structured outputs
- canonical/distinct commitments
- exact fixed ciphertext/view tag/disclosure payload length
- user policy/mode/target/payload canonical combination
- mandatory full digest/audit payload
- self-view batch-level all-or-none
- valid audit target encoding
- audit key ID/epoch/target이 active chain config와 exact match
- positive expiry
- total encoded message hard cap

Keeper에서 consensus-critical validation을 반복함.

### 4.3 Canonical payload

generated protobuf bytes를 hash하지 않음. Session 2 canonical encoder로 proof/creator를 제외한 effect field를 encoding하고 SHA-256 hi/lo를 계산함.

unknown semantic version, non-canonical field, trailing fixed payload는 fail closed함.

`AssetRegistryV1` query는 canonical denom과 32-byte asset ID를 양방향으로 반환하고 mapping version을 포함함. path snapshot query는 root 또는 height, 최대 16 commitment를 받고 모두 같은 snapshot에서 생성한 path/leaf index를 반환함. remote query가 note linkage를 노출할 수 있다는 API 문서 경고를 포함함.

## 5. Workstream C: Keeper

권장 파일:

```text
x/privacy/keeper/msg_server_batch_transfer.go
x/privacy/keeper/msg_server_batch_transfer_test.go
x/privacy/keeper/batch_scan_index.go
x/privacy/keeper/batch_scan_index_test.go
```

### 5.1 Validation order

1. structural/canonical/count/framing/byte limits
2. deterministic gas precharge
3. chain audit key ID/epoch/target
4. local duplicate nullifier/commitment
5. global spent nullifier와 global commitment uniqueness
6. historical root
7. Merkle capacity for output count
8. payload digest and aggregate roots
9. current context chain domain and expiry
10. proof decode/public witness/verification
11. atomic state/index/event writes

expensive key/hash/state lookup/proof 작업보다 먼저 gas를 charge함. cheap framing을 통과한 뒤의 invalid request도 cryptographic/state cost를 무상으로 사용하지 못함.

### 5.2 Public witness

Keeper derives all 12 values itself.

- counts from slice lengths
- chain domain from context chain ID/current circuit set
- roots from canonical message arrays
- payload limbs from canonical encoder
- expiry from message

message가 arbitrary public witness root/digest를 직접 제공하지 않음.

### 5.3 Explicit gas

```text
verify_base
+ per_input * input_count
+ per_output * output_count
+ per_payload_byte * canonical_payload_bytes
+ per_state_byte * typed_scan_record_bytes
+ per_tree_write * tree_node_writes
+ per_global_lookup * commitment_and_nullifier_lookups
```

- integer overflow-safe 계산
- proof verification 전에 charge
- invalid proof도 bounded resource cost
- insufficient gas에서 state write 없음
- constants와 benchmark 근거를 code/docs에 기록
- canonical hashing, proof verify, leaf/index/node/root write, global lookup, typed summary/output bytes의 비용 주체를 기록
- Cosmos KV gas와 explicit surcharge가 같은 일을 이중 청구하거나 빠뜨리지 않음을 test

### 5.4 Atomic state

- 모든 check/proof 후 nullifier를 씀.
- output count 전체 capacity를 먼저 확인함.
- commitment와 typed scan record를 order대로 씀.
- `AppendCommitment`가 과거 Deposit/2x2/Batch commitment와의 충돌을 최종 방어선에서 거부함.
- 중간 KV/event error가 tx 전체 rollback되는 test를 둠.
- batch message는 all-or-nothing임.

### 5.5 Asset registry와 genesis

- canonical denom 등록 시 `asset_id`를 재계산하고 양방향 collision/재등록을 거부함.
- Deposit은 registry에 없는 denom 또는 mapping mismatch를 거부함.
- genesis export/import는 commitment tree/indices/historical roots, global scan sequence/records, reserve counters, asset registry, consensus circuit descriptor를 보존함.
- import 시 historical roots를 commitment prefix에서 재계산하고 supplied root와 다르면 실패함.
- old/new Note/circuit/schema version 혼합 genesis를 거부함.

## 6. Workstream D: Scan State and Event

### 6.1 Minimal ABCI summary

event attribute:

- batch effect ID
- relayer
- input/output count
- nullifier/commitment/user/full roots
- expiry
- circuit set/payload/scan schema version
- audit key ID/epoch/target

다음은 넣지 않음.

- ciphertext
- disclosure payload
- full nullifier list
- 32개의 hex-expanded output metadata

batch effect ID는 Session 2 canonical helper로 계산하며 proof bytes와 creator를 바꿔도 같고 chain/effect field가 바뀌면 달라야 함.

### 6.2 Typed KV records

protobuf query/state type를 추가함.

```text
PrivacyScanSummaryV2
PrivacyScanOutputV2
```

raw `bytes` field를 사용해 commitment/ciphertext/disclosure를 한 번만 저장함. existing generic string attribute event index에 batch payload를 넣지 않음.

Deposit/2x2/Batch가 공유하는 global privacy sequence를 할당하고 key는 stable ordering을 보장함.

```text
privacy scan summary: height / global sequence
privacy scan output:  height / global sequence / output index
```

### 6.3 Query

- after cursor
- max output records
- max encoded payload bytes
- stable next cursor
- partial page support
- no duplicate/missing record
- corrupt record는 panic 대신 internal error
- batch typed record query가 실패하면 minimal ABCI event로 downgrade하지 않고 retryable error
- max 16 commitment path를 하나의 root/height snapshot으로 조회하는 batch query
- genesis export/import 후 cursor/sequence/leaf index가 연속임
- path snapshot의 모든 path가 요청한 동일 root를 재구성함.

legacy deposit/2x2 scan query는 regression을 유지함.

## 7. Workstream E: Artifact Lifecycle

새 artifact:

```text
privacy_batch_joinsplit_16x32_r1cs.bin
privacy_batch_joinsplit_16x32_pk.bin
privacy_batch_joinsplit_16x32_vk.bin
```

갱신:

- `x/privacy/zk` descriptor/manifest/checksum env
- genesis/consensus circuit descriptor set과 public-input schema digest
- `cmd/clairveil-setup`
- circuit config query
- node/prover role readiness
- release pack manifest/verifier
- circuit docs

임시 development setup으로 다음을 확인함.

- source commit/circuit set/constraint count/checksum 기록
- validator가 batch VK만 lazy load함.
- proving test가 batch R1CS/PK만 load함.
- missing/mismatch artifact가 strict mode에서 실패함.
- validator local VK checksum/schema/circuit set이 consensus descriptor와 다르면 startup/readiness가 실패함.
- artifact override는 development build/config에서만 허용함.
- R1CS/PK/VK binary는 commit하지 않음.
- formal setup은 수행하지 않음.

## 8. Workstream F: Direct Core Integration Test

Session 3B SDK를 기다리지 않고 internal deterministic fixture로 다음을 검증함.

1. valid NoteV1 input/path/output witness 생성
2. canonical batch message effect 생성
3. owner intent/signature 생성
4. development proof 생성
5. keeper `BatchTransfer` 호출
6. nullifier/commitment/tree root 확인
7. typed scan summary/output 확인
8. minimal ABCI event 확인

negative core integration:

- duplicate/local/global spent nullifier
- duplicate commitment
- commitment collision with prior Deposit/2x2/Batch
- wrong roots/payload limbs
- wrong chain/expiry
- invalid proof
- insufficient gas
- insufficient Merkle capacity
- scan index write failure rollback
- same nullifier를 2x2+Batch 및 Batch+Batch로 한 Cosmos tx에 넣은 양방향 순서와 full rollback
- forged genesis historical root와 export/import scan sequence continuity
- gas exhaustion이 expensive work 전에 발생하고 state가 불변임

## 9. Documentation/Traceability

갱신할 한영 문서:

- normative batch spec
- circuit guide
- threat model/security review
- operations/artifact guide
- testing guide
- downstream Cosmos integration
- release handoff/versioning

invariant matrix에 production file/test location을 채움.

특히 다음 세 경로를 구분함.

1. fixed 2x2 `MsgTransfer`
2. 여러 `MsgTransfer`를 한 Cosmos tx에 넣는 multi-message envelope
3. one-proof `MsgBatchTransfer`

## 10. Commit 전략

1. `feat: implement batch joinsplit 16x32 circuit`
2. `feat: add batch shielded transfer messages`
3. `feat: verify and execute batch transfers on chain`
4. `feat: index batch scan outputs without event duplication`
5. `feat: register batch joinsplit zk artifacts`
6. `test: exercise batch transfer core integration`
7. `docs: document batch transfer core contract`

각 commit은 buildable/testable하게 유지함.

## 11. 검증

```bash
make proto
go test ./x/privacy/circuit -count=1
go test ./x/privacy/types ./x/privacy/keeper -count=1
go test ./x/privacy/zk -count=1
go test ./x/privacy/client/sdk/conformance -count=1
go test ./x/privacy/... -count=1
make examples
make privacy-e2e-smoke
make release-check
make release-pack
make release-pack-verify
git diff --check
```

새 direct core integration target이 필요하면 `make privacy-batch-core-smoke`처럼 추가하고 completion record에 남김.

## 12. 중단 조건

- production circuit이 feasibility prototype보다 의미 있게 다른 constraint/resource를 보임.
- native/circuit roots 또는 public witness가 불일치함.
- explicit gas를 적용할 deterministic consensus 지점을 찾지 못함.
- message/typed scan state가 hard byte limit을 초과함.
- atomic rollback을 증명하는 test를 만들 수 없음.
- 새 Critical/High protocol finding이 발견됨.

보안 제약 또는 16/32 capacity를 조용히 낮추지 않고 blocker를 보고함.

## 13. 범위 밖

- public batchtransfer Go SDK
- remote prover HTTP batch route
- wallet scanner/decrypt UX
- payroll planner/worker/reconcile
- tx CLI/tutorial/localnet product flow
- formal setup/external audit/production deployment

## 14. Acceptance Criteria

- [ ] production circuit이 normative 12 public input과 일치함.
- [ ] single owner signature를 사용함.
- [ ] active prefix, NoteV1 membership, distinctness, conservation, disclosure roots가 circuit에서 강제됨.
- [ ] `MsgBatchTransfer`와 structured output proto가 생성됨.
- [ ] types/keeper hard limit과 local/global duplicate check가 있음.
- [ ] commitment가 Deposit/2x2/Batch/genesis 전체에서 전역 유일함.
- [ ] keeper가 public witness를 message/context에서 직접 계산함.
- [ ] explicit gas와 atomic state transition test가 있음.
- [ ] gas가 expensive validation/proof 전에 charge되고 Cosmos KV gas와의 책임이 명시됨.
- [ ] minimal ABCI event만 emit함.
- [ ] batch effect ID가 canonical vector와 일치함.
- [ ] typed binary KV scan index가 payload를 한 번만 저장함.
- [ ] Deposit/2x2/Batch가 하나의 global privacy sequence/cursor를 사용함.
- [ ] typed query failure가 lossy event fallback 없이 fail closed함.
- [ ] same-root batch path snapshot query/local provider가 동작함.
- [ ] cursor/output/byte-limited query가 동작함.
- [ ] artifact descriptor/setup/strict readiness가 동작함.
- [ ] consensus artifact identity와 local VK/schema mismatch가 startup/readiness를 막음.
- [ ] direct proof/message/keeper integration이 통과함.
- [ ] existing Deposit/Spend/2x2 regression이 없음.
- [ ] invariant matrix와 한영 docs가 code/test location을 가리킴.
- [ ] artifact binary/secret이 tracked되지 않음.
- [ ] master ledger가 갱신됨.

## 15. Session 3B Handoff

```text
## Completion Record

- 시작 commit:
- 완료 commit:
- circuit set ID/source/constraint count:
- batch proto/API version:
- public witness order:
- gas constants:
- typed scan schema/query cursor:
- artifact names/checksums:
- direct core integration 결과:
- invariant matrix 경로:
- 미해결 finding:
- Session 3B review scope:
- worktree 상태:
```

core gate가 미완료이면 Session 3B를 시작하지 않음.
