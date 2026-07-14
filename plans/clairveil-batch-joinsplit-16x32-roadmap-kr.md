# Clairveil BatchJoinSplit16x32 Master Roadmap

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | **Session 4 PASS — `PUBLICATION_READY_EXPERIMENTAL`** (Gate 1/2/3A/3B 및 Pass A~I PASS, `S4-B01` RESOLVED, unresolved Critical/High/security-relevant Medium 0) |
| 작성일 | 2026-07-10 |
| 대상 브랜치 | `private/multi-circuit-b` |
| 최종 목표 | 현재 shielded protocol의 알려진 보안·프라이버시 결함을 먼저 제거하고 안전하고 견고한 최대 16-input / 32-output shielded batch transfer를 구현함 |
| 실행 방식 | Session 1 -> 2 -> 3A -> 3B -> 4 순서로 실행하며 병렬 실행하지 않음 |
| 회로 기반 | `gnark v0.14.0`, Groth16, BN254 |
| 배포 상태 | experimental source publication 승인; production 배포, external ZK audit와 공식 trusted setup은 미승인 |
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
| 1. 현재 보안 수정 | **Gate 1 PASS — fresh closure complete** | `e427370`; closure scope `6aa341e..HEAD` | historical `14d85f5`; S4-B03 `02f61f3`/`42d40bd`; S4-B02 `0b7d97d`, `630736f`, `25c17ef`; G123A closure commit은 이 record와 함께 생성 | Production 2x2 `DBS-01..03`, structured pre-sign private projection/final effect binding, duplicate-inflation regression, exact artifact tamper/rotation gate를 fresh multi-agent review와 full verification으로 재확인함 | Gate 1과 `S4-B01`은 PASS/RESOLVED; formal setup/audit/dependency production risk는 유지 |
| 2. 기반/설계 확정 | **Gate 2 PASS — frozen decision unchanged** | historical `ad99ef7193fdc0683e483e4440e5cda1f0945432`; re-entry `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`; closure `6aa341e..HEAD` | foundation `c7fc1be`, `a8697cd`, `a4ee959`, `4e75f1f`, `4e90223`; implementation `0b7d97d`, `630736f`, `25c17ef` | `99,775` constraints와 `+10` delta, public input/NoteV1/payload/schema/version unchanged, Batch `1,111,837` unchanged를 fresh resource/contract review로 재확인함 | Foundation decision change 없음; formal setup/audit와 production artifact 배포는 미수행 |
| 3A. Circuit/chain core | **Gate 3A PASS** | re-entry `0fc818c`; closure scope `6aa341e..HEAD` | `0b7d97d`, `630736f`, `25c17ef`; G123A-AR01/DOC01/RP01 및 AR02 closure | Current/previous 모든 required artifact actual SHA-256, non-JoinSplit byte identity, missing/duplicate/unknown/tamper fail-closed, JoinSplit-only positive rotation, old/new proof, fresh genesis, full repository/release 검증을 통과함 | Frozen core를 변경하지 않고 Session 3B closure 완료 |
| 3B. Client/product integration | **Gate 3B PASS — re-entry complete** | re-entry `16d2280` | `79ea24e`, `bbf168f`, `1e855cb`, `009cc36`; Completion Record는 Session 3B 문서 | Structured signer global secret independence, actual SQLite/PostgreSQL graph atomicity, one-proof payroll production worker localnet, live recipient/auditor/self-view disclosure와 view-tag mismatch safe scan을 targeted/full/release 검증으로 확인함 | `G3B-01..04` active finding 0. Public input/NoteV1/payload/schema/version/circuit contract unchanged |
| 4. 독립 검증/공개 gate | **PASS — `PUBLICATION_READY_EXPERIMENTAL`** | `0df27417910f46ff714e73ce0730f5e167ece33a` | implementation `cdc7780`, `1b1dc08`, `a84ca1c`; publication record는 이 ledger/report commit | Pass A~I, max-shape, actual SQL, bounded fuzz, race, independent localnet/restart/retry, disclosure/view-tag/payroll E2E, full release gate를 fresh 실행함 | Production audit/freeze/MPC/transcript/signed provenance/downstream rollout은 release-blocking Production TODO |

### 2026-07-13 Session 4 Closure Ledger Note — Current Authoritative Record

- 고정 범위는 Session 1 base `e42737022b2aa87b498f57ae4d089ccb84a45968`, Session 4 시작 snapshot `0df27417910f46ff714e73ce0730f5e167ece33a`, immutable implementation snapshot `a84ca1cc1cd835990243d9b3f5f064e7b538f7ae`다. 시작 snapshot은 수정하지 않았고 최종 검토 범위만 base부터 final `HEAD`까지 확장한다.
- Read-only reviewer 두 명의 단일 wave를 Pass A~E와 Pass F~I/publication hygiene로 나눴고 main reviewer가 후보를 직접 판정했다. 반복 reviewer wave나 clean-round loop는 수행하지 않았다.
- `cdc7780`은 동일 BN254 residue를 가진 `r`/`r+q`가 commitment는 같지만 raw decimal 비교를 우회하던 batch signing secret 검사를 canonical field byte 기준으로 바꿨다. `1b1dc08`은 실제 timeout/healthy HTTP prover의 server-side counter/body 관찰로 default `1/0`, opt-in `1/1`과 timeout/malformed/validation failure 분리를 증명해 `S4-B01`을 닫았다. `a84ca1c`은 reference payroll localnet 종료 시 node process가 남는 shell backgrounding 문제를 수정했다.
- Fresh max-shape 결과는 `1,111,837` constraints, R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`, peak RSS `3,354,689,536 B`다. 모든 10개 repository fuzz target, 실제 SQLite/PostgreSQL, artifact/proof/genesis identity와 three localnet/E2E 경로가 skip 없이 PASS했다.
- Current unresolved count는 Critical 0, High 0, security-relevant Medium 0이다. 운영/Low residual은 owner와 production-blocking 여부를 Session 4 한영 보고서에 기록했다.
- 처분은 **`PUBLICATION_READY_EXPERIMENTAL`**이다. `PRODUCTION_RELEASE_READY`는 승인하지 않으며 external ZK audit, final source/constraint freeze, official MPC/trusted setup, transcript/toxic-waste evidence, signed production artifact/provenance와 downstream production rollout은 Production TODO로 유지한다.

### Historical — 2026-07-13 Session 3B Re-entry Closure Ledger Note

- 시작 HEAD `16d2280`에서 frozen public input, NoteV1, payload/schema/version, circuit contract와 Gate 1/2/3A closure를 유지했다.
- `79ea24e`가 structured batch signer와 final prepared validator의 global secret-reuse 검사를 공유하고 input/output 및 output 간 reuse intent를 signature release 전에 거부한다.
- `bbf168f`가 실제 SQLite/PostgreSQL graph CRUD/rollback/reopen와 lease acquire/heartbeat/expiry/CAS/duplicate-active-reservation을 실행한다. `make reservation-sql-integration`은 PostgreSQL 미준비나 skip을 PASS로 처리하지 않는다.
- `1e855cb`가 one-proof payroll durable graph -> `BatchProofWorker` -> `IdempotentBatchBroadcastWorker` -> `BatchReconcileWorker` -> typed evidence/report를 실제 localnet에 연결하고 process/node restart, timeout, 동일 signed bytes retry, tx-hash-first reconcile, spent-nullifier item 결과를 검증한다.
- `009cc36`이 같은 live E2E에서 recipient note 4개, user disclosure 2개, audit/self-view 각 4개 plaintext를 실제 키로 복호화하고 commitment/recipient/amount/asset/digest 및 view-tag mismatch safe scan을 확인한다.
- Session 3B targeted/full privacy/race/vet, 실제 SQLite/PostgreSQL, one-proof localnet, build/examples/release-check/release-pack/verify/diff 검증을 통과해 **Gate 3B를 PASS**로 판정한다.
- **Session 3B closure 당시 Session 4는 UNBLOCKED / NOT STARTED**였다. `S4-B01`과 독립 Pass A~I/publication hygiene는 후속 Session 4가 확인하는 범위였고, 위 current Session 4 record가 이를 supersede한다.

### Historical — 2026-07-12 독립 재검증 Ledger Note

- fresh reviewer가 여섯 계획과 Completion Record를 처음부터 끝까지 읽고 `e427370..d45f0753c16571743f630599776c9cd498d1e8c9`를 code에서 재구성함. Finding 확정 전 파일은 수정하지 않았음.
- `scripts/privacy-batch-joinsplit-localnet.sh`는 one-proof transfer shape를 실행하지만 payroll operation graph/worker/reconcile/report를 실행하지 않음. `scripts/reference-payroll-live-localnet.sh`는 legacy multi-message 2x2 `transfer-batch` 경로임.
- live runner는 recipient/auditor/self-view disclosure decrypt와 digest 재계산, view-tag mismatch safe scan, default/opt-in prover endpoint 접촉 횟수를 assert하지 않음. 결과의 no-failover 값은 실행 관찰이 아닌 literal임.
- `SQLStore` many-to-many 구현은 존재하지만 실제 SQLite/PostgreSQL CRUD, transaction rollback, restart, concurrent lease/CAS test가 없음.
- `JoinSplitCircuit`은 SDK가 독립 CSPRNG를 쓰더라도 user/full blinding과 output randomness의 exact reuse를 circuit에서 금지하지 않음. 이 privacy invariant를 완전히 닫으려면 R1CS/VK가 바뀌므로 Session 2/3A 재진입이 필요함.
- `ValidateBatchTransferSigningRequest`는 최종 prepared validator와 달리 input/output 및 output 간 secret reuse를 서명 전에 거부하지 않음. Duplicate inflation 회귀도 exact exploit-shaped witness를 격리하지 못함.
- tracked R1CS/PK/VK, `dist/`, `benchmarks/`, `tmp/`, 개인 absolute path 또는 명백한 secret은 발견되지 않았고 기존 development artifact SHA/size는 기록과 일치함.

### Historical — 2026-07-12 S4-B03 보완 Ledger Note

- `02f61f3746b67d5244c160b7c0e0e42f7c0b78b8`에서 production code나 circuit artifact를 바꾸지 않고 exact duplicate inflation regression을 추가함.
- `TestJoinSplitCircuitRejectsExactDuplicateInputInflation`과 `TestBatchJoinSplit16x32RejectsExactDuplicateInputInflation`은 production circuit이 distinctness constraint에서 실패하고 해당 assertion 하나만 완화한 control은 나머지 constraint를 모두 만족함을 확인함.
- `TestMsgTransferValidateBasicRejectsExactDuplicateInputInflation`과 `TestMsgServerTransferRejectsExactDuplicateInputInflationBeforeProof`는 wire 단계 및 proof 검증 전 local duplicate 거부와 실패 시 state 불변성을 보강함.
- targeted regression과 `go test ./x/privacy/... -count=1`이 통과해 `S4-B03`을 **RESOLVED**로 처분함.
- `S4-B02`는 active이며 Gate 1, Gate 4, publication은 완료하지 않음. 다음 순차 단계였던 Session 2 re-entry 결과는 아래 note가 supersede한다.

### Historical — 2026-07-12 `S4-B02` Session 2 Re-entry Ledger Note

- 시작 기준은 clean `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`이며 latest Ledger/Session 4 `BLOCKED` record를 authoritative source로 사용했다. Historical publication-ready 판단은 사용하지 않았다.
- `c7fc1be`, `a4ee959`이 `DISCLOSURE-BLINDING-SEPARATION` V1 shared native/prepared/error contract와 collision-retrying 2x2 SDK path를 구현하고, language-neutral `privacy_disclosure_blinding_v1_contract.json`이 exact gating/error/negative vector를 동결한다.
- `a8697cd` test-only hardened circuit은 production definition에 exact all-private sentinel과 `DBS-01..03`만 더한다. Current `99,765` 대비 target `99,775` constraints(`+10`), R1CS `+253 B`, PK `+912 B`, VK/proof size 변화 없음, peak RSS `690,438,144 B`, OOM 없음이다. Batch는 unchanged `1,111,837` constraints다.
- `4e75f1f`이 한영 normative circuit/SDK/security/testing/schema와 invariant traceability matrix를 갱신했다. Public input/NoteV1/payload encoding/disclosure digest/circuit-set version은 변경하지 않으며 Session 3A가 JoinSplit R1CS/PK/VK와 manifest/consensus JoinSplit identity만 교체한다.
- Session 3A re-entry는 **UNBLOCKED / NOT STARTED**다. Production circuit/artifact, pre-sign structured enforcement, negative regression, exact readiness/resource gate가 완료되기 전까지 `S4-B02`는 **IMPLEMENTATION PENDING / NOT RESOLVED**다.
- `S4-B03` resolved 상태와 Gate 3B/S4-B01 finding은 변하지 않는다. 전체 Gate 1, Gate 4, publication은 계속 `BLOCKED`다.

### Historical — 2026-07-12 `S4-B02` Session 3A Implementation Ledger Note

- clean 기준 HEAD `0fc818c`에서 latest Ledger와 Session 2 `S4-B02 Foundation Re-entry` record를 authoritative source로 사용했다. Historical publication-ready record는 현재 evidence로 사용하지 않았다.
- `0b7d97d`은 production `JoinSplitCircuit` output 0에 `DBS-01..03`과 all-private canonical user-blinding sentinel/gating을 exact 적용했다. Legacy control `99,765` 대비 production `99,775` constraints(`+10`)를 재현했으므로 decision change는 없다.
- `630736f`은 shared native/prepared relation과 같은 typed `DBS_*` contract를 2x2 structured pre-sign boundary에 적용한다. Invalid disclosure blinding, sentinel 또는 final intent/effect mismatch에서는 signer callback이 호출되지 않는다.
- `25c17ef`은 JoinSplit-only development setup 경로와 exact rotation/readiness regression을 추가했다. 새 JoinSplit SHA-256은 R1CS `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, PK `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, VK/consensus identity `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`다. 생성 조건은 `gnark v0.14.0`, Groth16/BN254, development `groth16.Setup`, `clairveil-setup -circuit joinsplit -overwrite`이며 formal trusted setup은 수행하지 않았다.
- Old/new VK proof 상호 mismatch, old consensus/file mismatch, fresh genesis/reset와 strict artifact preflight를 확인했다. Cached old JoinSplit proof/job은 폐기 대상이며 repository 안에는 해당 proof/job cache가 없었다. 9개 non-JoinSplit artifact와 Batch source/artifact는 byte-identical하게 유지했다.
- 전체 2x2 regression과 JoinSplit resource gate가 통과했다. Full Batch resource gate도 `1,111,837` constraints, R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`, peak RSS `3,324,461,056 B`로 통과해 OOM과 Batch artifact delta가 없음을 확인했다.
- `S4-B02` implementation은 **RESOLVED**다. 이는 Gate 1/2/3A를 승인하거나 Gate 3B/Session 4를 재개한 것이 아니며 세 gate 모두 fresh 독립 재검토가 필요하다. Current unresolved count는 Critical 0, High 2, security-relevant Medium 3이고 publication은 계속 `BLOCKED`다. Session 3B 작업은 시작하지 않았다.

### Historical — 2026-07-13 `G123A-AR01`/`DOC01`/`RP01` 보완 Ledger Note

- fresh review task `019f56bb-7962-7210-a4b5-f01c7a47a4b8`, clean 기준 HEAD `def4f8405a22011eb4d73b1e1bbfba68fec82b60`에서 시작했다. 작업 단위 commit은 `G123A-AR01` `57670bbfeff9d2fcb7bcfc7ba85cf4caedfb5b90`, `G123A-DOC01` `46dbb754549d07b162935aade59ba8827b968c91`, `G123A-RP01` `0d65faa2efe45d606864251d050c0679f0109716`이다.
- Historical G123A 보완에서는 ephemeral `/tmp/clairveil-g123a.T24SZF/{current,previous}` supplied set으로 `make validate-joinsplit-artifact-rotation-evidence`를 실행했다. Current closure target은 이 경로에 의존하지 않으며 clean commit에서 pinned prior/current source로 complete set을 재생성한다. Exact test 존재와 exact `--- PASS`를 요구하고 `SKIP`/`[no tests to run]`을 실패 처리한다.
- `S4-B03` targeted 명령 `go test ./x/privacy/circuit ./x/privacy/types ./x/privacy/keeper -run '^(TestJoinSplitCircuitRejectsExactDuplicateInputInflation|TestBatchJoinSplit16x32RejectsExactDuplicateInputInflation|TestMsgTransferValidateBasicRejectsExactDuplicateInputInflation|TestMsgServerTransferRejectsExactDuplicateInputInflationBeforeProof)$' -count=1 -v`와 `S4-B02` DBS targeted 명령 `go test ./x/privacy/circuit ./x/privacy/types ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/conformance -run '^(TestJoinSplitCircuitEnforcesDisclosureBlindingSeparationV1|TestValidateDisclosureBlindingSeparationV1|TestJoinSplitStructuredSigningBoundaryRejectsDisclosureReuseBeforeRelease|TestGenerateTransferDisclosureBlindingsV1RetriesExactReuse|TestValidatePreparedTransferPayloadMetadataRejectsDisclosureBlindingReuse|TestValidatePreparedTransferPayloadMetadataCanonicalizesAllPrivateUserBlinding|TestPrivacyDisclosureBlindingV1Contract)$' -count=1 -v`가 PASS했다. Stale signer test-file 참조는 실제 `x/privacy/client/sdk/transfer/payload_test.go` 회귀로 교정했다.
- `go test ./x/privacy/... -count=1`, `go test ./... -count=1`, `go vet ./x/privacy/...`, `make build`, `make examples`, `make release-check`, `make release-pack`, `git diff --check`, 한영 문서 heading pair 및 stale reference 검사가 모두 PASS했다. Examples의 기존 npm Low 1건과 기존 vulnerability policy residual은 변하지 않는다.
- Python 3.9.6 실패 원인인 `str | None` annotation을 호환 문법으로 바꾸고 CI에 Python `3.9`/`3.12` `make release-pack-verify` matrix를 추가했다. 동일 archive `clairveil-handoff-v0.1.0-142-g0d65faa.tar.gz`(SHA-256 `e5dbb48638ab621acfcf396ec89d0f18e5d66827f94c148c48e8a2d4a5f04960`)를 기본 `/usr/bin/python3` `3.9.6`과 `python3` `3.12.8`에서 검증해 두 실행 모두 required file `125`개와 exact manifest commit `0d65faa2efe45d606864251d050c0679f0109716`을 확인했다.
- `git diff --name-only def4f84..0d65faa -- x proto`는 비어 있다. `JoinSplitCircuit` constraint, public input, NoteV1, payload/schema/version, circuit/manifest identity, Batch source/artifact와 tracked R1CS/PK/VK를 변경하거나 회전하지 않았다. 검증용 development artifact는 repository 밖 임시 경로에만 생성했다.
- 처분: 세 G123A finding의 보완은 완료했지만 이 세션은 Gate를 PASS 처리하지 않는다. Gate 1/2/3A fresh 독립 재검토가 여전히 필요하고 `G3B-01..04`/`S4-B01`은 시작하거나 수정하지 않았다. Session 3B와 publication은 계속 **BLOCKED**다.
- worktree: 이 Ledger/Completion Record commit과 exact release-pack 재검증 뒤 `git status --short --branch`가 clean이고 generated artifact/secret이 tracked되지 않은 상태로 종료한다.

### Historical — 2026-07-13 Gate 1/2/3A Fresh Closure Ledger Note

- 범위/방식: `6aa341e..HEAD`, `mode=deep`, `verify=max`, `threshold=P2`, `scope=release`, pinned read-only reviewer 2명을 매 round 새로 생성했다. 첫 wave가 full scope를 검토한 뒤에만 finding을 확정·수정했고, 이후 이전 finding을 park한 연속 fresh clean round 2회와 final PR-style gate를 통과했다.
- fixed: `G123A-AR02`는 current/previous 12개 required artifact의 actual SHA-256을 각 manifest와 비교하고 non-JoinSplit 9개 actual digest 동일성을 검증한다. Missing/duplicate/unknown descriptor, missing file, stale digest, manifest-backed non-JoinSplit 변경을 fail closed하며 R1CS/PK/VK tamper negative, JoinSplit-only positive rotation, exact current-source R1CS serialization 결합을 재현 가능한 `make validate-joinsplit-artifact-rotation-evidence`에 포함했다.
- structured pre-sign: `JoinSplitOwnerIntentSigningRequestV1`의 input/output NoteV1, sender 공개키 projection으로 ordered nullifier, commitment 두 개, value conservation, change ownership, user/audit disclosure digest를 final effect와 재계산 대조한다. Decoy private fields와 redirected change는 signature callback 전에 거부한다. Public input, NoteV1, payload/schema/version, production circuit contract는 변경하지 않았다.
- selective rotation closure: `clairveil-setup`은 검증된 staging install과 backup rollback rename이 모두 실패하면 staging과 backup을 삭제하지 않고, 반환 오류에 두 exact 복구 경로를 포함한다. 정상 rotation, install 실패/rollback 성공, install 실패/rollback 실패, 성공 후 backup cleanup 실패 fault-injection regression이 PASS했고, 지정 함수와 테스트만 읽기 전용으로 확인한 새 reviewer 1명은 추가 결함을 발견하지 않았다.
- verification: S4-B03/DBS targeted regressions, repository 밖 supplied set으로 재생성한 pinned previous/current artifact tamper/rotation/proof/fresh-genesis evidence, `go test ./x/privacy/... -count=1`, `go test ./... -count=1`, `go vet ./x/privacy/...`, `make build`, `make examples`, JoinSplit/Batch resource gate, `make release-check`, `git diff --check`가 PASS했다. Closure commit 뒤 `make release-pack`과 Python 3.9/3.12 `make release-pack-verify`를 exact clean commit에 대해 실행한다.
- disposition: **Gate 1 PASS, Gate 2 PASS, Gate 3A PASS. Session 3B re-entry UNBLOCKED.** `G3B-01..04`, `S4-B01`, payroll/scanner/SQL/live E2E, Session 3B 구현은 시작하지 않았다. Gate 3B FAIL과 `S4-B01`이 남으므로 Session 4와 publication은 계속 **BLOCKED**다. Formal trusted setup, production artifact 배포, external audit은 수행하지 않았다.

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
