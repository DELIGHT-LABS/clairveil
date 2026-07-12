# Clairveil BatchJoinSplit16x32 Master Roadmap

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Complete (`PUBLICATION_READY_EXPERIMENTAL`, production-ready/audited 아님) |
| 작성일 | 2026-07-10 |
| 대상 브랜치 | `private/multi-circuit-b` |
| 최종 목표 | 현재 shielded protocol의 알려진 보안·프라이버시 결함을 먼저 제거하고 안전하고 견고한 최대 16-input / 32-output shielded batch transfer를 구현함 |
| 실행 방식 | Session 1 -> 2 -> 3A -> 3B -> 4 순서로 실행하며 병렬 실행하지 않음 |
| 회로 기반 | `gnark v0.14.0`, Groth16, BN254 |
| 배포 상태 | production 배포 및 공식 trusted setup을 수행하지 않은 개발 단계 |
| 호환성 정책 | migration/backward compatibility보다 최종 안전한 protocol shape를 우선함 |

## 1. 문서 구조

이 roadmap은 전체 고정 결정, 단계 의존성, gate, 상태 ledger만 관리함. 실제 작업은 새 Codex 세션 하나가 문서 하나를 받아 독립적으로 완료할 수 있도록 세션별 계획으로 분리함.

1. [Session 1: 현재 shielded authorization과 JoinSplit 보안 수정](clairveil-batch-joinsplit-16x32-session-1-security-remediation-kr.md)
2. [Session 2: NoteV1 및 BatchJoinSplit16x32 기반/설계 확정](clairveil-batch-joinsplit-16x32-session-2-foundation-kr.md)
3. [Session 3A: BatchJoinSplit16x32 circuit/chain core 구현](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md)
4. [Session 3B: SDK/prover/scanner/payroll integration 구현](clairveil-batch-joinsplit-16x32-session-3b-integration-kr.md)
5. [Session 4: 독립 검증과 GitHub 코드 공개 gate](clairveil-batch-joinsplit-16x32-session-4-publication-validation-kr.md)

각 세션은 자기 계획의 completion record와 이 문서의 ledger를 함께 갱신함. 확정된 public contract는 계획 문서에만 남기지 않고 `docs/`의 한글/영문 페어로 반영함.

## 2. 고정 전제

### 2.1 최종 circuit set

- `DepositCircuit`
- `SpendCircuit`
- 정확히 2-input / 2-output인 `JoinSplitCircuit`
- 최대 16-input / 32-output인 `BatchJoinSplit16x32`

`JoinSplitCircuit`은 고정 2x2 arity privacy와 단건 경로의 단순성을 위해 유지함. `BatchJoinSplit16x32`는 별도 proving/verifying artifact를 사용하는 compact batch 회로임.

### 2.2 공통 NoteV1

Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32는 하나의 최종 `NoteV1` 의미를 사용함.

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

정확한 domain field constant와 asset ID derivation은 Session 2 golden vector로 동결함. 모든 shielded public key는 host와 circuit에서 on-curve, non-identity, prime-subgroup 조건을 만족해야 함.

Note tree의 empty contract도 NoteV1 일부로 동결함.

```text
empty[0] = 0
empty[level + 1] = MiMC(
  NOTE_TREE_NODE_V1_DOMAIN,
  level,
  empty[level],
  empty[level],
)
```

Keeper append, path, rebuild, genesis, circuit fixture는 literal zero sibling을 임의로 섞지 않고 같은 depth별 empty table을 사용함. Active commitment/nullifier는 canonical non-zero field여야 하며 commitment는 Deposit, JoinSplit2x2, BatchJoinSplit16x32 전체 tree에서 globally unique해야 함.

`asset_id`에서 denom을 되찾기 위해 on-chain `AssetRegistryV1`을 protocol state로 둠. canonical denom과 asset ID는 1:1이며 collision/재등록을 거부함. Deposit, scanner, SDK, UI는 local config가 아니라 이 registry/query를 authoritative source로 사용함.

### 2.3 Owner authorization

같은 transfer의 active input은 모두 같은 spend/view owner key를 사용함. 따라서 input별 signature를 검증하지 않고 **owner signature 하나**로 전체 intent를 인증함.

2x2와 16x32의 owner intent는 최소 다음을 묶음.

- protocol/circuit domain과 version
- chain domain
- Merkle root
- input/output count
- asset ID
- ordered nullifier set/root
- ordered output commitment set/root
- user disclosure digest/root
- full disclosure digest/root
- exact canonical message payload digest
- expiry

relayer/fee payer인 `creator`는 intent에서 제외함. recipient, amount, output, ciphertext, disclosure, chain, expiry를 바꾸면 owner signature 또는 proof verification이 실패해야 함.

### 2.4 SpendIntent

현재 `SpendCircuit`의 withdraw authorization도 production 공개 전에 함께 고침.

```text
recipient_digest = SHA-256(
  "clairveil.withdraw-recipient.v1" ||
  u32be(len(recipient_address_bytes)) ||
  recipient_address_bytes
)

spend_intent = MiMC(
  SPEND_INTENT_V2_DOMAIN,
  chain_domain_hi,
  chain_domain_lo,
  circuit_kind,
  merkle_root,
  nullifier,
  amount,
  asset_id,
  recipient_digest_hi,
  recipient_digest_lo,
  expires_at_unix,
)
```

`chain_id`, exact recipient address bytes, expiry는 keeper check만으로 끝내지 않고 owner signature와 proof public statement에 묶음. 주소 bytes를 `big.Int.SetBytes` 한 값만 인증하지 않음. `creator`는 relayed withdraw를 위해 제외함.

### 2.5 Canonical 256-bit digest representation

canonical message payload와 chain domain은 SHA-256으로 계산하고 modulo reduction하지 않음. digest를 big-endian 128-bit 두 limb로 나누어 BN254 public field에 넣음.

```text
digest_hi = uint128(sha256[0:16])
digest_lo = uint128(sha256[16:32])
```

canonical byte encoder는 versioned domain, fixed field order, explicit length prefix를 사용함. JSON/protobuf serialization 결과나 map order를 hash source로 사용하지 않음.

### 2.6 BatchJoinSplit16x32 기본 의미

- batch는 permissionless임.
- input count는 `1..16`, output count는 `1..32`임.
- count는 public이며 canonical active prefix를 사용함.
- 같은 batch의 active input은 같은 owner와 asset을 사용함.
- note amount는 기존 64-bit 범위를 유지함.
- active input nullifier와 active output commitment는 각각 pairwise distinct임.
- disabled slot은 실제 note/state가 아니며 canonical sentinel을 사용함.
- 기본 SDK는 compact mode임.
- optional output padding은 같은 circuit에서 zero-value output을 실제 생성하는 방식이며 Merkle state/event/gas 비용을 부담함.
- input padding은 실제 membership note 없이는 지원하지 않음.

### 2.7 Batch public statement

Batch circuit public input은 다음 12개 field element로 고정함.

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

nullifier, commitment, disclosure values는 message에 개별로 존재하고 circuit public input에는 domain-separated fixed-depth aggregate root를 사용함. Keeper는 message에서 roots/digest를 직접 계산함.

### 2.8 Disclosure

- user disclosure는 output별 policy/mode를 지원함.
- all-private를 제외한 user disclosure는 output별 CSPRNG `user_disclosure_blinding`을 사용함.
- 모든 active output은 CSPRNG `full_disclosure_blinding`을 포함한 proof-bound full disclosure digest를 가짐.
- auditor payload와 sender self-view payload는 같은 `FullDisclosureRoot`의 output digest를 사용하되 서로 다른 key로 암호화함.
- audit와 self-view plaintext는 같은 `full_disclosure_blinding`을 포함해 복호화 후 digest를 재계산할 수 있게 함.
- self-view payload는 batch-level optional all-or-none이며 SDK 기본값은 enabled임.
- self-view ciphertext는 `PayloadDigestHi/Lo`로 exact bytes가 owner intent에 묶임.
- disclosure blinding은 note randomness와 반드시 분리함. Note randomness를 disclosure verifier에게 전달하지 않음.
- 공개 digest가 amount/address 후보를 검증하는 dictionary oracle이 되지 않는지 current 2x2와 16x32 모두에서 검증함.
- circuit은 ciphertext decryptability를 증명하지 않음.
- audit decrypt 실패는 chain proof success와 구분해 `AuditDeliveryFailed` 또는 `ManualReview`로 다룸.
- ordinary change/padding output의 user disclosure 기본값은 all-private임.

### 2.9 Fixed-size encoding

공통 `NotePlaintextV1`, `DisclosurePlaintextV1`, versioned encrypted envelope를 canonical fixed-size binary format으로 사용함. Disclosure plaintext는 plane/policy/output index와 disclosure blinding을 포함함. 이는 batch output뿐 아니라 deposit encrypted note, 2x2 transfer output, scanner, CLI, fixture에 동일하게 적용함.

### 2.10 Scan data plane

32개 ciphertext/disclosure를 ABCI event와 KV event index에 중복 저장하지 않음. Deposit, 2x2, 16x32는 서로 분리된 cursor를 만들지 않고 하나의 global privacy event sequence와 unified scan cursor를 사용함.

- ABCI event는 batch effect ID, counts, roots 같은 최소 summary만 포함함.
- module KV scan index는 typed protobuf/raw bytes record로 output payload를 한 번만 저장하고 global sequence/output index로 정렬함.
- hex string attribute로 대형 payload를 저장하지 않음.
- scan cursor는 `(height, global_sequence, output_index)` 단위로 이동함.
- query는 event 수, output 수, total payload bytes를 모두 제한함.
- typed batch query가 실패하면 ciphertext가 없는 minimal ABCI event로 성공한 것처럼 fallback하지 않고 fail closed/retry함.
- audit key epoch/ID와 public target, circuit/payload/scan version, leaf index를 self-contained summary/output record에 저장함.
- 최대 16개 commitment의 path를 동일 query height/root에서 반환하는 bounded snapshot query 또는 동등한 local-tree provider contract를 제공함.
- scan records, global sequence, reserve counters, asset registry는 genesis export/import 또는 검증 가능한 deterministic rebuild 계약을 가짐.
- KV write bytes와 state growth를 consensus gas에 반영함.

### 2.11 Prover와 artifact

- remote prover payload는 spend-authority-equivalent 민감정보로 취급함.
- automatic multi-prover failover는 기본 off임.
- 기존 `ProverPool`을 포함해 failover는 명시적 privacy opt-in 없이는 같은 payload를 두 endpoint로 전송하지 않음.
- prover는 cheap framing/body 검증 직후 permit을 획득하고 semantic validation부터 실제 prove 종료까지 bounded in-flight/queue와 backpressure를 적용함.
- client cancellation이 실제 gnark 작업을 중단하지 못하면 작업 종료 전에 permit을 반환하지 않음.
- validator는 필요한 VK만, prover는 선택한 circuit의 R1CS/PK만 lazy load함.
- consensus state는 active circuit set ID, exact VK digest, public-input schema digest를 pin함. Validator는 local artifact가 consensus identity와 일치하지 않으면 시작/readiness에 실패함.
- development R1CS/PK/VK는 임시 생성하고 commit하지 않음.
- 공식 trusted setup은 circuit 동결, 독립 감사, production 배포 결정 이후에 수행함.

## 3. 순차 실행 Gate

```text
Session 1
  current transfer + withdraw authorization/invariant remediation
        |
Gate 1: 알려진 inflation, output redirection, disclosure oracle, recipient-byte ambiguity,
        global commitment ambiguity, unsafe crypto decode, privacy-unsafe prover failover 없음
        |
Session 2
  NoteV1 + full-shape feasibility + normative batch contract
        |
Gate 2: dominant full-shape와 max wire/state shape가 feasible하고 design TBD 없음
        |
Session 3A
  circuit + proto + keeper + gas + artifact + typed scan state
        |
Gate 3A: direct core integration과 development artifact 검증 통과
        |
Session 3B
  SDK + prover API + scanner + payroll + CLI + localnet E2E
        |
Gate 3B: prepare부터 scan/reconcile까지 end-to-end 통과
        |
Session 4
  fresh-session adversarial validation + publication hygiene
        |
Gate 4: PUBLICATION_READY_EXPERIMENTAL
```

각 세션은 이전 gate가 충족되지 않으면 시작하지 않음. 다음 세션이 이전 세션의 미완성 보안 결정을 암묵적으로 떠안게 하지 않음.

## 4. 단계별 산출물

| 세션 | 핵심 산출물 | 완료 기준 |
| --- | --- | --- |
| 1 | duplicate nullifier/commitment 방어, disclosure blinding, byte-exact SpendIntentV2, safe ECIES/EdDSA parsing, prover failover opt-in, 공격 회귀 테스트 | current transfer/withdraw의 알려진 soundness·authorization·privacy 결함이 제거됨 |
| 2 | NoteV1, exact empty/vector/sentinel contract, fixed encoding/envelope, circuit+wire/state feasibility, 12 public input spec, consensus circuit identity, unified scan/path/asset contract | dominant constraint/resource와 max message/state가 측정되고 구현을 바꾸는 TBD가 없음 |
| 3A | production circuit, `MsgBatchTransfer`, keeper/gas/atomic state, minimal event, typed scan index, artifact descriptor | direct proof/message/keeper state transition이 통과함 |
| 3B | batch SDK, prover route, scanner, disclosure, payroll, CLI/tutorial/localnet | recipient scan과 payroll reconcile까지 end-to-end로 동작함 |
| 4 | independent adversarial/property/fuzz 검증, benchmark, 공개 hygiene, residual TODO | Critical/High/security Medium이 없고 experimental 공개 문서가 정확함 |

## 5. 상태 Ledger

| 세션 | 상태 | 시작 commit | 완료 commit | 검증 요약 | 잔여 사항 |
| --- | --- | --- | --- | --- | --- |
| 1. 현재 보안 수정 | Complete (Gate 1 충족) | `e427370` | `14d85f5` | unit/proto/전체 Go/examples/vulncheck/localnet/privacy E2E/payroll live/release-check/bulk readiness/fuzz/release-pack, review-fix 및 prepared-transfer canonical key 회귀 검증 통과 | current protocol Critical/High 0건. no-fixed-version `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`, npm low 1건, formal setup/external audit 미수행을 known risk로 추적 |
| 2. 기반/설계 확정 | Complete (Gate 2 충족) | `ad99ef7193fdc0683e483e4440e5cda1f0945432` | `f117b4f8487c78b6531efe2be1ecccccefe6c5c1` | Gate 1 재검증, NoteV1/fixed payload/exact batch effect/registry/scan/path/artifact/admission 구현, corrected 16x32 full-shape와 max wire/state gate, 전체 privacy/release/E2E 검증 통과. 후속 review-fix에서 slash denom REST binding, reserve/registry genesis linkage, historical path online admission/cancellation·cached-root fail-closed, Note memo validation/fallible encoding을 보강하고 fresh consecutive clean review 2회를 통과함 | Critical/High/P0/P1/P2 0건. Historical path public rebuild는 1,024 leaves/동시 2개로 제한되고 offline recovery/export는 1,048,576 bound를 유지함. Production batch circuit/message/handler/integration, formal setup/audit는 후속 세션이며 약 3.11 GiB peak RSS와 process isolation/downstream fixed-encoding 전환을 risk로 추적 |
| 3A. Circuit/chain core | Complete (Gate 3A 충족) | `b7a97acd03c5e97b9e7e0bf52197ba421feda3c8` | `fc391f5e1d69634e0b64a14735d0956302038032` + closure commit | production 16x32 circuit/12-input witness, proto/types, direct/governance/authz signed raw 128 KiB cap·singleton·depth guard, keeper precharge/proof-gated atomic state, exact audit identity, global uniqueness/sequence, typed scan/minimal event, genesis/root, four-circuit artifact identity/readiness를 구현함. Direct real-proof core integration, 2x2+Batch/Batch+Batch 양방향 rollback, 59-case negative matrix, targeted composition/identity suite와 full Go/build/examples/localnet/privacy E2E/bulk/release/artifact gate를 최신 hardening 뒤 재통과함 | Active Critical/High/Medium/actionable security finding 0건. Formal setup/audit/production artifact, process isolation, gas/governance/pruning calibration은 residual. Public SDK/prover/scanner/payroll/CLI는 3B 범위 |
| 3B. Client/product integration | Complete (Gate 3B 재검증 충족) | `cd9b4124ee0a7d3f7faeec1e76f765ec3330a88d` | `d7809e9` + Gate 3B closure commit | 1..16/1..32 batch SDK, structured signing, version/hash-bound local+single-remote prover, bounded admission, idempotent broadcast, lossless typed scanner/disclosure, one-operation durable payroll, CLI/tutorial/fixture/release handoff를 구현함. Session 4 진입 검토에서 preparation, remote transport, typed cursor, durable payroll/reconcile, localnet hygiene를 보강하고 현재 tree에서 full privacy/race/vet/examples, 1/1·3/4·31+change·exact32·padding batch localnet, Deposit/2x2/Withdraw E2E, payroll live를 재통과함 | Active Critical/High/Medium/actionable finding 0건. Formal setup/production artifact/external audit/managed production infra/downstream 제품은 미수행. In-process prove 격리와 기존 no-fixed-version Go 3건/npm low 1건을 known risk로 추적 |
| 4. 독립 검증/공개 gate | Complete (`PUBLICATION_READY_EXPERIMENTAL`) | `b2fa95661590f681d268885c7dfdf7e9af3581ba` | `494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2` | Gate 3B를 먼저 재검증하고 Pass A~I 전체를 수행함. Preparation/remote prover/typed cursor/durable payroll/re-sign/restart/plaintext log/private-file mode/payload-free prover error/standalone proof body bound/release provenance·input immutability·handoff status finding을 수정했으며 independent KAT, input `1..16`/output `1..32` property, fuzz 10개, 5-shape resource profile, cross-message rollback, fresh privacy/payroll/batch localnet, 실제 restart와 genesis continuation, full test/race/vet/CI/vuln/release/pack gate를 통과함. Pack은 pinned Git tree와 shared selected-path manifest/template을 사용하고 generator/verifier는 umask와 무관한 exact file set, immutable explicit inputs, canonical safe tar/manifest/checksum list, expected commit/Git blob/raw·extracted `0644`·`0755` permission mode를 대조하며 immutable implementation/report SHA와 publication manifest commit을 분리 고정함 | Critical/High/security Medium 0건. Formal setup/external audit/source freeze/production artifact 및 downstream production 운영은 미수행이며 owner와 production blocking 여부가 한영 Session 4 보고서에 기록됨 |

## 6. 공통 실행 규칙

1. roadmap과 자기 세션 문서를 전체 읽음.
2. `git status --short --branch`와 이전 completion record를 확인함.
3. 이전 gate를 최소 명령으로 재확인함.
4. security-sensitive 결정은 normative docs와 golden vector를 먼저 갱신함.
5. circuit/native/keeper/client가 공유하는 invariant traceability matrix를 유지함.
6. proto/fixture/schema/CLI/prover contract 변경은 같은 세션에서 정렬함.
7. trust boundary 변경은 threat model/security review 한영 문서에 반영함.
8. generated development artifacts와 secret은 commit하지 않음.
9. 작업 성격별로 commit하고 각 commit을 buildable/testable하게 유지함.
10. 종료 시 자기 completion record와 master ledger를 갱신하고 clean worktree를 남김.

## 7. Code Publication Gate

Session 4에서 실제로 완료할 범위:

- known consensus inflation/double-spend/authorization bypass 0건
- NoteV1과 all circuit/native helper differential test 통과
- malformed count, disabled slot, duplicate value, wrong domain, expired intent, payload substitution rejection
- disclosure digest dictionary test, leading-zero recipient mutation, global commitment replay, malformed ECIES/EdDSA rejection
- permissionless message의 byte/state/gas hard bound
- prover bounded admission
- unified global scan cursor, typed scan index, path snapshot, genesis round-trip의 lossless 동작
- localnet E2E와 restart/retry/reconcile
- constraint, proving RAM/time, verification, tx/state/event size benchmark
- remote prover trust, public counts/grouping, decryptability 비보장, no formal setup 공개 문서화
- development artifact/secret/local path 미추적
- release check/pack/verify 통과

## 8. Production Release TODO

### Core repository owner

- 외부 ZK 전문가 circuit/constraint audit
- final source/constraint digest freeze
- 공식 Groth16 MPC/trusted setup ceremony
- transcript/contributor verification/toxic-waste 폐기 증적
- R1CS/PK/VK signing/provenance/reproducibility
- production circuit set manifest와 signed release
- release tag SBOM/image/dependency/source provenance

### Downstream chain owner

- production gas calibration/governance
- core consensus circuit identity에 production VK/circuit hash를 넣는 genesis/upgrade proposal과 rollout
- validator rollout/rollback/runbook
- staging/testnet load/fault rehearsal
- block/mempool/tx size 조율
- Merkle/state growth monitoring
- emergency disable와 incident response

### Prover/product/auditor operator

- TLS, mandatory auth, ACL, quota, process isolation
- payload log/telemetry/retention 정책
- audit key HSM/KMS/threshold custody와 epoch rotation
- decrypt failure/manual review 운영
- JS SDK/wallet batch UX와 padding opt-in 정책
- payroll production DB/scheduler/multi-tenant 운영

이 TODO가 남아 있으면 production-ready/audited라고 표현하지 않음.

## 9. Downstream/Handoff 영향

### 9.1 기존 이슈와 신규 설계 과제 구분

| 구분 | 항목 | 처리 세션 |
| --- | --- | --- |
| 현재 코드에 이미 존재 | user/audit/self-view disclosure의 deterministic dictionary oracle | Session 1 |
| 현재 코드에 이미 존재 | JoinSplit2x2 duplicate input/nullifier inflation과 output commitment ambiguity | Session 1 |
| 현재 코드에 이미 존재 | transfer final effect 미인증, withdraw chain/expiry 및 recipient byte alias | Session 1 |
| 현재 코드에 이미 존재 | Deposit/2x2/tree의 global commitment uniqueness 부재 | Session 1 |
| 현재 코드에 이미 존재 | ECIES point/EdDSA signature canonical·subgroup decoder 부족 | Session 1 |
| 현재 코드에 이미 존재 | `ProverPool` automatic multi-endpoint failover privacy 확장 | Session 1 |
| 현재 코드에 이미 존재 | arbitrary historical root genesis 및 circuit artifact consensus identity 부족 | Session 1 |
| 현재 코드에 이미 존재 | asset ID를 denom으로 복원할 authoritative registry 부재와 proof 비용 계량 공백 | Session 1~2 |
| 16x32 신규 설계 | `raw_user_digest_i`, vector node, disabled sentinel exact formula | Session 2 |
| 16x32 신규 설계 | same-root batch path snapshot, unified scan cursor/schema | Session 2~3A |
| 16x32 신규 설계 | max-shape circuit feasibility와 별도 wire/state feasibility | Session 2 |
| 16x32 신규 설계 | many-input reservation -> one operation -> many item output persistence | Session 3B |
| 16x32 신규 설계 | batch gas/admission/composition/atomic rollback | Session 3A~4 |

기존 이슈는 batch를 구현하지 않더라도 수정할 release blocker임. 신규 설계 과제는 16x32를 추가할 때 생기는 공격 표면과 integration 요구사항임.

### 9.2 Interface별 영향

이 roadmap은 배포 전 protocol을 안전하게 재정의하므로 다음 interface는 의도적으로 breaking change가 됨.

| 변경 | 영향 받는 downstream/handoff | 필요한 조치 |
| --- | --- | --- |
| `MsgTransfer.expires_at_unix`, TransferIntentV2, disclosure blinding | Go/JS SDK, wallet, prover, relayer | 새 canonical payload, signing request, proof/public witness, disclosure payload version을 동시에 적용함 |
| canonical ECIES/EdDSA decoder | Go/JS SDK, wallet, prover | identity/non-subgroup/non-canonical key와 64-byte가 아닌 signature를 거부하고 같은 negative fixture를 통과함 |
| recipient byte digest를 사용하는 SpendIntentV2 | Go/JS SDK, wallet, withdraw relayer, prover | Bech32 decode 결과의 exact raw bytes와 length를 digest하고 새 public-input 순서를 사용함 |
| global commitment uniqueness | downstream keeper, indexer, wallet/payroll | duplicate commitment를 retry 성공으로 보지 않고 fresh randomness/replan error로 처리함 |
| NoteV1/empty-tree/fixed envelope | wallet scanner, indexer, fixtures, downstream chain | 기존 개발 state와 wallet cache를 폐기하고 fresh genesis/reindex함. legacy fallback을 사용하지 않음 |
| consensus circuit identity와 artifact manifest | chain integrator, validator operator, prover operator | genesis/upgrade circuit set과 local VK/R1CS/PK checksum을 맞추고 mismatch readiness를 처리함 |
| unified scan cursor와 typed records | JS SDK, wallet, indexer, payroll scanner | cursor/schema version을 올리고 batch feed 실패를 tx-event fallback으로 숨기지 않음 |
| asset registry/query | wallet/SDK, chain registry | asset ID를 UI denom으로 복원하고 hash collision/config mismatch를 fail closed함 |
| prover failover opt-in | prover operator, payroll product | 기본 single endpoint 선택을 사용하고 다중 prover 전송은 명시적 privacy 정책에서만 켬 |
| batch operation many-to-many persistence | payroll product, DB adapter, admin tooling | batch operation-input reservation-item output join과 atomic lease/CAS schema를 적용함 |
| fresh genesis/reset 정책 | downstream chain, wallet, payroll/reconcile | predeploy 상태를 reset하고 old Note/cache/reservation/prepared proof를 폐기함. mixed-version import를 거부함 |

Session 1~3B는 한영 SDK handoff, client API/UX, circuit/artifact, operations, payroll 문서를 같은 contract version으로 갱신함. JS SDK와 wallet이 기존 fixture를 그대로 재사용하면 안 되며 release pack에는 old/new version 혼용을 검출하는 conformance test를 포함함.

### 9.3 반드시 갱신할 공개 handoff 문서

각 구현 세션은 코드만 바꾸고 다음 세션으로 넘기지 않음. 영향이 발생한 같은 commit 묶음에서 최소 아래 한영 페어와 conformance fixture를 갱신함.

- `docs/clairveil-js-sdk-handoff.md`, `docs/clairveil-js-sdk-handoff-kr.md`: version, canonical encoder, structured signing, scan cursor, no-failover default
- `docs/clairveil-client-api-checklist.md`, `docs/clairveil-client-api-checklist-kr.md`: retry/failover privacy boundary, new query/message/schema
- `docs/clairveil-client-ux-flows.md`, `docs/clairveil-client-ux-flows-kr.md`: expiry, reservation release, disclosure/padding/manual-review UX
- `docs/clairveil-circuits.md`, `docs/clairveil-circuits-kr.md`: exact public witness, NoteV1, vector/sentinel, artifact identity
- `docs/clairveil-reference-payroll-js-sdk-handoff.md`, `docs/clairveil-reference-payroll-js-sdk-handoff-kr.md`: batch operation many-to-many model과 expected evidence
- `docs/clairveil-reference-payroll-wallet-handoff.md`, `docs/clairveil-reference-payroll-wallet-handoff-kr.md`: cache reset, disclosure key/policy, scanner/reconcile
- `docs/clairveil-proverd-remote-production-profile.md`, `docs/clairveil-proverd-remote-production-profile-kr.md`: admission permit lifetime, artifact readiness, explicit failover opt-in
- `docs/clairveil-threat-model.md`, `docs/clairveil-threat-model-kr.md`와 security review: 새 trust boundary, known residual, publication status

Session 1 완료 시 기존 API의 breaking change만 먼저 반영하고, Session 2~3B에서는 그 위에 NoteV1/batch contract를 누적함. 아직 구현되지 않은 기능을 현재 제공 기능처럼 문서화하지 않음.

## 10. 권장 Codex 실행 환경

2026-07-10 로컬 Codex model catalog 기준임.

| 세션 | 모델 | Effort | 이유 |
| --- | --- | --- | --- |
| 1 | `gpt-5.6-sol` | `max` | current consensus/authorization 결함을 circuit과 host에서 함께 수정함 |
| 2 | `gpt-5.6-sol` | `max` | NoteV1, full-shape feasibility, canonical contract를 동결함 |
| 3A | `gpt-5.6-sol` | `max` | circuit/proto/keeper/artifact의 consensus contract를 구현함 |
| 3B | `gpt-5.6-sol` | `max` | SDK/prover/scanner/payroll integration을 일관되게 완성함 |
| 4 | `gpt-5.6-sol` | `ultra` | fresh-session 다각도 독립 재검증에 delegation이 유리함 |

Session 4의 내부 delegation은 Session 1~3B 병렬 실행을 의미하지 않으며 외부 cryptographic audit를 대체하지 않음.

## 11. 최종 완료 의미

- current 2x2 transfer와 withdraw의 알려진 authorization 결함이 수정됨.
- Deposit/Spend/2x2/16x32가 동일한 domain-separated NoteV1을 사용함.
- 한 proof와 한 message로 최대 16개 note를 소비하고 최대 32개 output을 생성함.
- keeper, SDK, prover, scanner, payroll이 같은 batch contract를 사용함.
- 코드 공개에 필요한 보안 검증, benchmark, 문서가 준비됨.

이는 공식 trusted setup, 외부 감사, downstream production 운영까지 완료했다는 의미는 아님.
