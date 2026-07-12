# Session 3A Plan: BatchJoinSplit16x32 Circuit/Chain Core 구현

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Complete historical Gate 3A; **S4-B02 implementation RESOLVED / fresh Gate 3A 독립 재검토 필요** (2026-07-12) |
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

### 1.1 `S4-B02` Session 3A 재진입 Gate

Session 2 re-entry는 `DISCLOSURE-BLINDING-SEPARATION` V1을 TBD 없이 동결했고, 기준 HEAD `0fc818c`에서 해당 계약의 Session 3A production 구현을 완료했다. Frozen interface와 target을 바꾸는 decision change는 없었다.

- JoinSplit2x2 recipient output `0`에 `DBS-01`(`policy != 0 => user != output randomness`), `DBS-02`(`full != output randomness`), `DBS-03`(`full != user`)을 추가한다.
- All-private는 user blinding을 zero로 canonicalize하고 `DBS-01`만 gate off한다. Full blinding non-zero와 `DBS-02`/`DBS-03`은 유지한다. JoinSplit2x2 output `1`은 active change note이고 disabled slot이 아니다.
- Shared native/prepared validator와 `privacy_disclosure_blinding_v1_contract.json`의 stable `DBS_*` error/vector를 production circuit 및 signature-release 전 2x2 structured boundary와 함께 만족해야 한다. Batch signer `G3B-04`는 별도 Session 3B finding이다.
- Test-only target `99,775` constraints(current `99,765`, delta `+10`)을 재현하거나 decision change를 먼저 기록한다. Negative witness마다 complete digest/owner signature를 다시 계산하고 current control은 수락, hardened circuit은 실패하는 원인 분리 test를 production regression으로 승격한다.
- Public input 13개와 순서/schema hash `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, NoteV1, payload `v5`, proof/HTTP `v2`, disclosure digest/domain, manifest/identity schema, circuit-set ID `privacy-note-v1`은 변경하지 않는다.
- JoinSplit R1CS/PK/VK만 재생성하고 manifest checksum과 consensus JoinSplit `verifying_key_sha256`를 교체한다. Old JoinSplit proof/job을 폐기하고 exact readiness, fresh-genesis/reset, 전체 2x2 regression과 full batch resource comparison을 다시 실행한다. Batch source/artifact는 이 finding 때문에 회전하지 않는다.
- `S4-B02` implementation은 **RESOLVED**다. 이는 Gate 1/2/3A 승인 또는 Session 3B/4 재개가 아니며 변경된 relation과 identity에 대한 fresh 독립 재검토 전 publication은 blocked다.

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

- [x] production circuit이 normative 12 public input과 일치함.
- [x] single owner signature를 사용함.
- [x] active prefix, NoteV1 membership, distinctness, conservation, disclosure roots가 circuit에서 강제됨.
- [x] `MsgBatchTransfer`와 structured output proto가 생성됨.
- [x] types/keeper hard limit과 local/global duplicate check가 있음.
- [x] commitment가 Deposit/2x2/Batch/genesis 전체에서 전역 유일함.
- [x] keeper가 public witness를 message/context에서 직접 계산함.
- [x] explicit gas와 atomic state transition test가 있음.
- [x] gas가 expensive validation/proof 전에 charge되고 Cosmos KV gas와의 책임이 명시됨.
- [x] minimal ABCI event만 emit함.
- [x] batch effect ID가 canonical vector와 일치함.
- [x] typed binary KV scan index가 payload를 한 번만 저장함.
- [x] Deposit/2x2/Batch가 하나의 global privacy sequence/cursor를 사용함.
- [x] typed query failure가 lossy event fallback 없이 fail closed함.
- [x] same-root batch path snapshot query/local provider가 동작함.
- [x] cursor/output/byte-limited query가 동작함.
- [x] artifact descriptor/setup/strict readiness가 동작함.
- [x] consensus artifact identity와 local VK/schema mismatch가 startup/readiness를 막음.
- [x] production JoinSplit output 0이 `DBS-01..03`과 all-private canonical sentinel을 exact 강제함.
- [x] shared native/prepared validator와 2x2 structured pre-sign boundary가 동일한 `DBS_*` contract를 사용하고 invalid request에서 signature callback을 호출하지 않음.
- [x] production `99,775` constraints(`+10`)와 원인 분리 negative regression을 재현함.
- [x] JoinSplit-only development artifact rotation, old/new proof mismatch, fresh genesis/reset, exact readiness와 unchanged full Batch resource gate가 통과함.
- [x] direct proof/message/keeper integration이 통과함.
- [x] existing Deposit/Spend/2x2 regression이 없음.
- [x] invariant matrix와 한영 docs가 code/test location을 가리킴.
- [x] artifact binary/secret이 tracked되지 않음.
- [x] master ledger가 갱신됨.

## 15. Session 3B Handoff

## Completion Record

### 2026-07-12 `S4-B02` Session 3A Completion Record

- 시작 기준: clean `0fc818c`; latest Master Ledger와 Session 2 `2026-07-12 S4-B02 Foundation Re-entry` record를 authoritative source로 사용했다. Historical publication-ready record는 사용하지 않았다.
- 작업 단위 commit: `0b7d97d` production relation/regression, `630736f` structured 2x2 signing boundary/conformance, `25c17ef` JoinSplit-only development artifact rotation/readiness.
- 방어적 invariant: recipient output 0의 `DBS-01..03`, all-private canonical zero user-blinding sentinel과 exact gating을 circuit/native/prepared/pre-sign에 일치시켰다. Output 1은 active change로 유지했다.
- Constraint decision: legacy relation control `99,765`, production `99,775`, delta `+10`(`0.0100%`). Session 2 target과 exact 일치하므로 decision change가 없다. 각 negative는 commitment/digest/owner signature를 다시 계산하고 legacy control 성공과 production rejection을 함께 확인했다.
- Development artifact: `gnark v0.14.0`, Groth16/BN254, local `groth16.Setup`, `clairveil-setup -circuit joinsplit -overwrite`. R1CS `10,824,169 B` / SHA-256 `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`; PK `16,766,489 B` / `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`; VK `748 B` / consensus identity `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`.
- Identity/readiness: public-input 13개와 schema hash `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`는 unchanged다. Old/new proof 상호 mismatch, old consensus/file mismatch, fresh genesis/reset와 strict artifact preflight가 통과했다. Repository 내부에 폐기할 old JoinSplit proof/job cache는 없었으며 외부 cache는 새 identity와 함께 폐기해야 한다.
- Resource/regression: 전체 privacy 2x2 regression, JoinSplit cold gate, old/new proof gate와 fresh-genesis gate가 통과했다. Full Batch는 unchanged `1,111,837` constraints, R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`, peak RSS `3,324,461,056 B`로 통과했고 OOM은 없었다. 9개 non-JoinSplit artifact와 Batch source/artifact는 byte-identical하다.
- Final repository verification: `go test ./... -count=1`, `go vet ./x/privacy/...`, `make build`, `make examples`, `make vulncheck`, `git diff --check`가 통과했다. 명시적으로 범위 밖인 payroll/scanner/SQL/live E2E와 Session 3B/4 gate는 실행하지 않았다.
- Release/hygiene: clean documentation closure `354509db54f193295d1e1a18f9e4b45de3741d4f`에서 `make release-pack`, `make release-pack-verify`가 125개 required file과 exact manifest commit을 검증했다. 이 bookkeeping commit 뒤에도 같은 검증을 재실행하고 tracked worktree clean, generated R1CS/PK/VK·secret 미추적 상태를 확인한다.
- 범위/처분: formal trusted setup, G3B-01..04, `S4-B01`, batch structured signer, payroll/scanner/SQL/live E2E, Session 3B/4는 수행하지 않았다. Generated development R1CS/PK/VK와 secret은 repository에 commit하지 않았다. `S4-B02` implementation은 **RESOLVED**이고 Gate 1/2/3A는 fresh 독립 재검토가 필요하다. Publication은 계속 `BLOCKED`다.

### Historical 2026-07-12 `S4-B02` Re-entry Handoff

- 상태: **UNBLOCKED FOR IMPLEMENTATION / NOT STARTED**. Session 2 foundation commits `c7fc1be`, `a8697cd`, `a4ee959`, `4e75f1f`이 §1.1의 exact contract, negative fixture, feasibility target, artifact scope를 동결했다.
- 시작 기준: Session 3A re-entry는 Session 2 ledger/record가 clean 상태로 commit된 뒤 그 HEAD에서 시작한다. 이 문서 갱신은 production `JoinSplitCircuit`, R1CS/PK/VK, structured signer를 변경하지 않는다.
- 완료 의미: production JoinSplit constraint, pre-sign structured enforcement, regenerated JoinSplit identity, negative regression, exact readiness, 2x2/full-batch resource gate가 모두 통과해야 `S4-B02`를 RESOLVED로 전환할 수 있다.
- 현재 처분: `S4-B02` **IMPLEMENTATION PENDING**; Gate 1/Gate 4/publication `BLOCKED`. 아래 Completion Record는 historical Batch Gate 3A 완료 기록이며 이 re-entry 완료를 의미하지 않는다.

### Historical Batch Gate 3A Completion

- 상태: **Complete — Gate 3A 충족. Session 3B는 Unblocked이지만 시작하지 않음.**
- 시작 commit: `b7a97acd03c5e97b9e7e0bf52197ba421feda3c8`
- 완료 core hardening commit: `fc391f5e1d69634e0b64a14735d0956302038032`
- 공개 계약 문서 commit: `67115090d63578d3643617c866d03ef953b103f2`
- 최초 completion/ledger commit: `838da3ca502c330cd4493212d0528b570bc2bd5f`
- 최종 closure commit: `cd9b4124ee0a7d3f7faeec1e76f765ec3330a88d` (최신 hardening, 재검증, 이 record와 Master Roadmap 정정)

### Gate 2 재검증

- corrected full-shape gate는 production alias와 같은 16/32 circuit에서 constraint `1,111,837`, max witness prove/verify, peak RSS `3,339,862,016 B`를 기록해 PASS함.
- max-shape gate는 canonical owner-effect payload `65,384 B`, protobuf Tx `65,294 B`, typed scan KV `75,105 B`, total KV write `173,409 B`, minimal event `584 B`, query response `74,551 B`로 모든 hard limit을 통과함.
- public input 12개의 exact 순서, NoteV1/vector root/disabled sentinel/disclosure 공식, independent golden vector와 invariant traceability matrix가 존재함을 재확인함.
- Session 2의 fresh clean review 결과와 현재 구현 전 review를 대조해 unresolved Critical/High design finding이 0건임을 확인함. Frozen protocol contract 변경이나 capacity/security constraint 축소가 없어 decision change proposal은 필요하지 않았음.

### 동결된 protocol/consensus identity

- circuit set ID: `privacy-note-v1`
- required circuit order: `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`
- production batch source: `x/privacy/circuit/batch_joinsplit_16x32.go`; max input/output `16/32`; constraint `1,111,837`
- artifact manifest schema: `v2`; circuit identity schema: `v1`; privacy module/state version: `2`
- batch proto/API: package `clairveil.privacy.v1`, `MsgBatchTransfer`, `BatchTransferOutput`, canonical owner-effect format `1`, signed raw `Any.value` cap `128 KiB`. Direct message와 nested governance/authz wrapper에서 batch `Any`의 type URL/value singleton을 raw wire 기준으로 검사하고 malformed wire와 8-level 초과 nesting을 fail closed함.
- fixed payload/envelope: `privacy-fixed-v1`; AssetRegistry: `privacy-asset-registry-v1`
- public witness exact order: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`
- public-input schema SHA-256: `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`

### Gas, state, scan contract

- `BatchGasModelV1`: verify base `1,000,000`; per input `25,000`; per output `50,000`; per canonical payload byte `4`; per typed state byte `8`; per tree-node write `5,000`; per global lookup `10,000` gas.
- resource bounds: canonical payload `65,384 B`, typed state `256 KiB`, tree node writes `1,056`, global lookups `48`. BaseApp가 ante보다 먼저 호출하는 batch `ValidateBasic`은 bounded framing/creator만 검사하고, keeper가 cheap signed-wire/proof framing 뒤 deterministic precharge한 다음 canonical point/envelope/disclosure/audit state 검증과 proof 작업을 수행함. Cosmos KV gas는 실제 store I/O를 별도로 charge하며 real `1/1` handler와 max `16/32` post-proof transition에서 두 layer를 독립 계측함.
- scan schema/sequence: `privacy-scan-v2` / `privacy-sequence-v1`; lexicographic query cursor `(height, global_sequence, output_index)`. Deposit/2x2/Batch가 같은 global sequence를 사용하며 ciphertext/disclosure bytes는 typed output record에 한 번만 저장함.
- `EventBatchTransferV1`은 effect ID, count, root, version, expiry, relayer, audit identity만 emit함. Same-root path snapshot, output/record/byte limit, typed fail-closed query, genesis export/import와 모든 historical prefix root 재검증을 통과함.
- exact audit key ID/epoch/target과 `AssetRegistryV1` mapping을 consensus state/genesis/query에서 검증함. Nullifier와 commitment는 local duplicate 및 global Deposit/2x2/Batch/genesis uniqueness를 모두 검사함.

### Development artifact identity

Artifact는 repo 밖 `/tmp/clairveil-session3a-artifacts-381c984`에서 source `381c984189e823e5797104eb7cd2beb2386eaf80`, 생성 시각 `2026-07-11T09:32:32Z` 기준으로 만들었으며 binary/secret은 commit하지 않았음.

| 파일 | 크기 | SHA-256 |
| --- | ---: | --- |
| `privacy_batch_joinsplit_16x32_r1cs.bin` | `122,813,535 B` | `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210` |
| `privacy_batch_joinsplit_16x32_pk.bin` | `209,218,621 B` | `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470` |
| `privacy_batch_joinsplit_16x32_vk.bin` | `716 B` | `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621` |

Generation peak RSS는 `3,308,797,952 B`, role readiness peak RSS는 `1,295,482,880 B`였음. Validator role은 batch VK만, prover role은 batch R1CS/PK만 읽었고 exact consensus identity, VK hash, schema hash, constraint count mismatch가 readiness를 막는 것을 확인함.

### Direct core/invariant 결과

- `TestBatchTransferDirectCoreIntegration`은 internal deterministic fixture로 실제 Groth16 proof를 생성하고 `MsgBatchTransfer` handler를 직접 호출해 proof 성공 뒤에만 nullifier, commitment, root snapshot, scan state, minimal event가 atomic commit됨을 검증함.
- `TestBatchTransferCoreRejectionsAndAtomicScanFailure`은 canonical/public witness/proof/state failure가 partial state를 남기지 않음을 검증함.
- `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`은 2x2+Batch와 Batch+Batch를 두 ordering 모두 실행해 후속 nullifier conflict가 outer Cosmos tx cache 전체를 rollback함을 검증함.
- production positive matrix는 `1/1`, `1/2`, `3/4`, `8/16`, `16/31`, `16/32`, mixed disclosure, active zero-value padding을 포함하고 negative matrix 59건은 count/sentinel/path/distinctness/owner/asset/key/value/root/domain/expiry/signature/disclosure/vector separation을 포함함.
- invariant matrix와 공개 계약: [한글](../docs/clairveil-batch-joinsplit-16x32-kr.md), [영문](../docs/clairveil-batch-joinsplit-16x32.md). 관련 한영 circuit/testing/operations/security/threat/downstream 문서도 code/test location으로 갱신함.

### 전체 검증

| 명령 | 결과 |
| --- | --- |
| `make proto` | PASS; generated proto에 unexpected diff 없음 |
| `go test ./x/privacy/circuit -count=1` | PASS |
| `go test ./x/privacy/types ./x/privacy/keeper -count=1` | PASS |
| `go test ./x/privacy/zk -count=1` | PASS |
| `go test ./x/privacy/client/sdk/conformance -count=1` | PASS |
| `go test ./x/privacy/... -count=1` | PASS |
| `make examples` | PASS; 기존 npm low 1건은 known risk로 유지 |
| `make privacy-e2e-smoke` | PASS; deposit, disclosure 3 mode transfer, direct/relayed withdraw |
| development artifact generation + `TestBatchDevelopmentArtifactRoleReadinessGate` | PASS |
| `make release-check` | PASS; `go test ./...`, command build, examples, vulnerability policy, localnet smoke, privacy E2E, bulk readiness 포함 |
| `make release-pack` / `make release-pack-verify` | PASS |
| `git diff --check` | PASS |
| post-`fc391f5` closure audit | PASS; direct proof/keeper, gas/rollback, global commitment/nullifier, scan/genesis, artifact readiness targeted suite와 전체 `make release-check` 재실행 |

### Review finding과 residual risk

- Independent circuit/proto review와 keeper/state/gas review에서 최종 unresolved Critical/High/Medium finding은 0건임.
- Review 중 발견한 두 Medium은 signed raw protobuf duplicate-field wire-cap bypass 방어와 max-shape Cosmos KV gas/explicit precharge 경계 증명으로 수정했으며 follow-up review가 두 finding의 해소와 새 Critical/High/Medium 0건을 확인함.
- `fc391f5`는 BaseApp의 pre-ante `ValidateBasic` 때문에 발생할 수 있던 무과금 semantic validation을 keeper precharge 뒤로 이동하고, direct/governance/authz raw `Any` singleton/cap 재귀 검사를 추가했음. Exact delta 독립 재검토에서 active Critical/High/Medium/actionable security finding은 0건이었고 malformed wire 및 max-depth boundary regression도 후속 test로 고정함.
- 현재 활성 module 구성에서 unresolved security finding은 0건임. 향후 임의 `Any`를 실행하는 새 wrapper module을 추가하면 raw recursion registry와 cap test를 함께 갱신해야 한다는 변경관리 조건은 남지만 현재 finding은 아님.
- Formal trusted setup, external audit, production artifact signing/distribution은 미수행임. Development setup generation은 약 `3.08 GiB`, readiness는 약 `1.21 GiB` peak RSS를 사용하므로 production prover hard cancellation/resource containment에는 process isolation이 필요함.
- Public batch Go SDK, remote prover batch HTTP route, wallet scanner/decrypt UX, one-proof payroll planner/worker/reconcile, batch CLI/tutorial은 의도적으로 미구현이며 Session 3B 범위임. Remote historical path query의 wallet-interest leakage, state pruning/governance calibration, new-asset governance도 residual operational risk임.
- Accepted no-fixed-version Go findings `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`와 examples npm low 1건을 기존 release policy에 따라 계속 추적함.

### Session 3B 진입 판정

- **Gate 3A: PASS.** Direct core integration, development artifact identity/readiness, atomic cross-message rollback, full validation이 모두 통과했고 unresolved Critical/High core finding이 없음.
- **Session 3B: Unblocked, Not Started.** Review scope는 public batch Go SDK/builder, remote prover batch route, wallet scan/decrypt, payroll prepare/worker/reconcile, batch CLI/tutorial, recipient scan부터 reconcile까지의 end-to-end임. Session 2/3A의 16/32 capacity, 12-input order, NoteV1/sentinel/vector/disclosure, gas, scan schema를 변경하려면 Session 3A decision change로 되돌아가야 함.
- worktree 상태: completion/ledger commit과 최종 release-pack 재검증 후 `git status --short --branch`가 clean임을 확인함.

core gate가 미완료이면 Session 3B를 시작하지 않음.
