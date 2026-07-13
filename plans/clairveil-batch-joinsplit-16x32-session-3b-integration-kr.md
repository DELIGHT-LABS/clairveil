# Session 3B Plan: BatchJoinSplit16x32 Client/Product Integration 구현

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | **Complete — Gate 3B PASS** (2026-07-13 re-entry closure; 후속 Session 4 PASS 및 `PUBLICATION_READY_EXPERIMENTAL`) |
| 선행 문서 | [Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md), [Session 3A](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md) |
| 후속 세션 | [Session 4 Publication Validation](clairveil-batch-joinsplit-16x32-session-4-publication-validation-kr.md) |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `max` |
| 완료 목표 | SDK prepare부터 prover, broadcast, scan, payroll reconcile까지 batch transfer end-to-end를 완성함 |

### 2026-07-13 Session 4 후속 상태

- Session 4는 고정 시작 snapshot `0df27417910f46ff714e73ce0730f5e167ece33a`에서 독립 검증을 시작해 Pass A~I, max-shape benchmark, actual SQLite/PostgreSQL, bounded fuzz, race, independent localnet restart/retry, disclosure/view-tag/payroll E2E와 release gate를 fresh 실행했다.
- `S4-B01`은 실제 timeout/healthy HTTP prover 두 endpoint의 server-side request counter와 수신 body 관찰로 닫았다. Default no-failover는 접촉 `1/0`, explicit opt-in은 `1/1`이며 timeout/malformed response/validation failure를 구분한다.
- Gate 3B의 아래 historical completion disposition은 그대로 보존한다. 현재 authoritative downstream 상태는 **Session 4 PASS, unresolved Critical/High/security-relevant Medium 0, `PUBLICATION_READY_EXPERIMENTAL`**이다. `PRODUCTION_RELEASE_READY`는 승인하지 않았다.

## 1. 진입 Gate

- production `BatchJoinSplit16x32`와 direct core integration이 통과함.
- `MsgBatchTransfer` proto/API version이 고정됨.
- keeper gas/atomic state/minimal event/typed scan index가 구현됨.
- global scan cursor/path snapshot과 AssetRegistryV1 query가 구현됨.
- payroll batch many-to-many durable schema version이 고정됨.
- artifact names/manifest/readiness가 고정됨.
- Session 3A completion record와 invariant matrix가 있음.
- 미해결 Critical/High core finding이 없음.

```bash
git status --short --branch
git log -10 --oneline
go test ./x/privacy/... -count=1
make release-pack-verify
```

Core contract 변경이 필요하면 Session 3A decision으로 되돌아가 영향과 vectors를 먼저 갱신함. SDK 편의를 위해 circuit/keeper invariant를 약화하지 않음.

## 2. 범위

```text
Payroll/SDK request
  -> select/reserve 1..16 inputs
  -> construct 1..32 outputs
  -> encrypt NotePlaintextV1/disclosures
  -> compute roots + SHA-256 digest limbs + owner intent
  -> single owner signature
  -> local/remote prover
  -> MsgBatchTransfer broadcast
  -> typed scan query/decrypt/commitment verification
  -> payroll item reconcile/report
```

## 3. Workstream A: Go Batch Transfer SDK

### 3.1 Package

권장 package:

```text
x/privacy/client/sdk/batchtransfer
```

2x2 전용 `transfer` package를 fixed `[16]/[32]` 구조로 억지 확장하지 않음. Session 2 common NoteV1, digest, encoding, vector, disclosure helper를 재사용함.

### 3.2 Public API

```text
PlanBatchTransfer
PrepareBatchTransfer
BuildPreparedBatchTransferPayload
ProvePreparedBatchTransfer
BuildMsgBatchTransfer
BroadcastBatchTransfer
```

단계별 artifact는 version/payload hash를 포함하고 mutation을 검출함.

### 3.3 Planning rules

- input은 `1..16`, 같은 owner/asset/root임.
- output은 payment/change/padding 합계 `1..32`임.
- change가 필요하면 payment recipient 최대 31명임.
- input total이 payment total과 정확히 같으면 32명 지급 가능함.
- compact mode는 zero change output을 만들지 않음.
- explicit padding mode만 zero-value output을 추가함.
- payment amount는 positive여야 함.
- change/padding user disclosure 기본값은 all-private임.
- output randomness는 모두 fresh함.
- duplicate input note/nullifier와 output commitment를 proof 전에 거부함.
- input root mismatch는 typed wallet-sync/replan error임.
- 16 input으로 금액을 충족하지 못하면 recursive merge를 암묵 수행하지 않고 preparation-required error를 반환함.

### 3.4 Build order

1. input notes/common root를 확정함.
2. payment/change/padding output notes를 생성함.
3. NoteV1 commitments와 fixed-size ciphertext를 생성함.
4. output별 독립 CSPRNG user/full disclosure blinding을 생성하고 user/full digest를 계산함.
5. audit payload와 optional all-or-none self-view payload를 암호화함.
6. aggregate roots를 계산함.
7. canonical message bytes와 payload digest hi/lo를 계산함.
8. batch intent를 계산함.
9. owner signature 하나를 요청함.
10. fixed-capacity witness와 disabled sentinel을 채움.
11. prepared payload hash를 계산함.

output/ciphertext를 확정하기 전에 signature를 요청하지 않음.

### 3.5 Signer interface

input별 note hash signer가 아니라 다음 의미의 signer를 사용함.

opaque field만 전달하는 blind-sign API를 만들지 않음. signer가 canonical effect를 독립 재계산하는 구조화 요청을 사용함.

```text
SignBatchTransfer(BatchTransferSigningRequest) -> EdDSA signature

BatchTransferSigningRequest
  version / circuit_set_id / chain_id / expiry
  ordered input nullifiers
  ordered payment/change/padding outputs
  recipient hashes / amounts / asset_id / disclosure policies
  aggregate roots / canonical payload bytes or hash
  expected intent field
```

hardware/external signer는 canonical encoder로 roots/payload digest/intent를 재계산하고 supplied value와 다르면 서명하지 않음. 표시용 summary도 canonical request에서 생성하며 별도 caller-provided 문자열을 신뢰하지 않음.

### 3.6 Sensitive payload

- 최대 16개 input witness와 32개 payroll output을 포함함.
- file output은 `0600`임.
- note amount/randomness/path/recipient를 log하지 않음.
- telemetry에 request body/hash correlation을 보내지 않음.
- automatic multi-prover failover는 off임.
- creator는 proof 이후 선택/교체할 수 있음.

## 4. Workstream B: Prepared Payload and Proof Contract

### 4.1 Payload

최소 field:

- version/circuit set
- chain ID/chain domain limbs
- expiry
- active input/output counts
- common root/asset
- active input NoteV1/path/nullifier data
- active output NoteV1 data
- structured output message metadata
- aggregate roots
- payload digest limbs
- single owner signature
- payload hash

### 4.2 Validation

- exact version
- canonical field/key/fixed payload
- disclosure blinding 존재, per-output 독립성, plaintext digest 재계산
- count/capacity
- NoteV1 commitment/nullifier recomputation
- root/path helper bits
- owner/asset/root equality
- duplicate inputs/outputs
- roots/payload digest/intent/signature consistency
- expiry
- payload hash

malformed payload는 witness construction/proving queue 전에 실패함.

### 4.3 Proof response

- response version
- request payload hash
- proof bytes
- optional circuit set/artifact checksum identity

client는 proof response를 broadcast message에 넣기 전에 payload hash/version을 검증함.

## 5. Workstream C: Prover API/Service

### 5.1 Route

```text
POST /v1/proofs/batch-transfer
```

2x2 route와 분리하고 request shape guessing으로 multiplex하지 않음.

### 5.2 Service

- batch proof request/response version
- strict JSON decode
- body/decompressed body hard limit
- metadata/payload hash validation
- circuit-specific R1CS/PK provider
- batch-specific max in-flight/queue
- overload `429`와 retryable error code
- queue wait/prove duration/RSS-safe metrics
- permit은 cheap framing 뒤 획득하고 semantic validation과 실제 gnark prove 종료까지 보유함.
- client cancellation만으로 아직 실행 중인 prove의 permit을 먼저 회수하지 않음.
- failure/panic과 실제 prove 종료에서 permit을 정확히 한 번 회수함.
- sensitive payload-free errors/logs

### 5.3 Readiness

- route list에 batch route가 표시됨.
- required batch R1CS/PK checksum이 valid해야 ready임.
- node/validator VK readiness와 prover R1CS/PK readiness를 구분함.

### 5.4 Tests

- valid local/HTTP prove
- missing/mismatch artifact
- malformed/oversized request
- queue full
- cancellation/permit release
- payload mutation
- wrong chain/expiry
- error/log redaction

## 6. Workstream D: Provider and Broadcast

- `MsgBatchTransfer` query/tx codec 지원
- single-message broadcast helper
- same transaction에 여러 batch message를 넣는 generic Cosmos multi-message 사용 가능
- gas estimation/fallback 정책
- account sequence와 signed tx bytes 저장 hook
- timeout 후 tx hash/nullifier query 우선
- 같은 signed bytes retry 허용
- new sequence 재서명 전 all input nullifier unspent 확인

batch message는 atomic이므로 일부 output만 retry하지 않음.

## 7. Workstream E: Scanner and Disclosure

### 7.1 Typed scan provider

- batch summary/output query client
- stable cursor
- max outputs/bytes
- retry/page resume
- Deposit/2x2/Batch 공통 `(height, global_sequence, output_index)` cursor
- output order와 batch effect ID 검증
- corrupt record fail closed
- typed record query 실패 시 ciphertext 없는 ABCI event로 fallback 금지
- same-root/height batch Merkle path snapshot query 또는 local tree provider

### 7.2 Note scan

- view tag는 untrusted hint임.
- 안전 기본값은 tag mismatch여도 full decrypt를 시도함. view tag는 note를 영구 누락시키면 안 되는 untrusted hint임.
- tag-only fast mode는 명시적 opt-in으로만 제공하고 누락 위험을 API/docs에 표시함.
- `NotePlaintextV1` exact parser를 사용함.
- NoteV1 commitment를 재계산해 record commitment와 비교함.
- mismatch/invalid key/amount를 note wallet에 저장하지 않음.
- duplicate page/retry에서 same commitment/index note를 중복 저장하지 않음.

### 7.3 Disclosure

- user public/recipient payload를 output index/commitment/policy/digest에 대해 검증함.
- user/full plaintext의 blinding을 사용해 digest를 재계산함.
- audit payload decrypt 후 `full_digest_i`와 비교함.
- self-view payload도 같은 full digest를 사용함.
- self-view ciphertext가 absent이면 batch all-or-none disabled contract를 따름.
- decrypt failure와 digest mismatch를 구분함.
- audit decrypt failure는 chain failure가 아니라 `AuditDeliveryFailed`/`ManualReview` evidence임.

## 8. Workstream F: Payroll Integration

### 8.1 Planner

- 16 input/32 output capacity를 고려해 items를 batch operation으로 묶음.
- change 필요 시 31 payments, exact total이면 32 payments임.
- input reservation을 batch 단위로 atomic 생성함.
- existing ordinary transfer와 다른 batch가 같은 note를 reserve하지 못함.
- output별 expected commitment, full/user disclosure digest, recipient hash, amount, denom, batch item index를 저장함.
- payment/change/padding role을 off-chain operation metadata로 구분함.

기존 one-operation/one-item 가정을 유지하지 않음. durable model은 다음 cardinality를 가짐.

```text
PayrollRun 1 --- N BatchOperation
BatchOperation 1 --- N OperationInputReservation
BatchOperation 1 --- N PayrollItemOutput
PayrollItemOutput 1 --- 1 ExpectedOutputEvidence
```

batch operation 생성, 최대 16 reservation 연결, 최대 32 item/output 연결, lease/CAS 전이는 하나의 DB transaction으로 atomic해야 함. file store와 SQL store 모두 명시적 schema version을 사용하며 old one-item fallback은 제공하지 않음.

expected evidence에는 commitment, user/full digest, recipient hash, amount, denom/asset ID, batch item index, output role, audit key ID/epoch을 저장함. 민감 recipient/amount는 encrypted 또는 keyed lookup form으로 저장함.

### 8.2 Proof worker

- one batch operation -> one proof job
- reservation lease/heartbeat
- batch prepared payload/proof artifact durable write
- write 성공 후 `ProofReady`
- private/local/enterprise prover default
- no automatic multi-prover failover
- explicit multi-prover opt-in 없이는 첫 endpoint 실패 후 다른 endpoint를 호출하지 않음.

### 8.3 Broadcaster/reconcile

- same operation ID/reservation 유지
- broadcast 직전 all nullifier unspent 확인
- timeout에서 tx hash/nullifier reconcile 우선
- batch tx success는 input note consumption을 확정함.
- item success는 expected output index/commitment/disclosure/evidence가 일치해야 함.
- evidence 부족 item은 `ManualReview`임.
- nullifier spent만으로 모든 payroll item을 성공 처리하지 않음.

### 8.4 Report

- batch effect ID/output index/item mapping
- proof count/tx envelope/output count
- batch chain status와 item evidence status
- padding/change state cost
- retry/manual review reason

## 9. Workstream G: CLI and Tutorial

### 9.1 Command naming

기존 `transfer-batch`는 multiple `MsgTransfer` Cosmos envelope 의미를 유지함.

새 one-proof command:

```text
clairveild tx privacy transfer-batch-16x32
```

필요한 단계형 companion command:

```text
prepare-batch-transfer
prove-batch-transfer
broadcast-batch-transfer
```

CLI docs에서 세 경로를 비교함.

- single 2x2 transfer
- multi-message envelope
- one-proof 16x32 batch

### 9.2 Localnet tutorial

실행 가능한 입력 fixture와 명령으로 다음을 재현함.

- 1 input / 1 payment, no change
- 3 input / 4 output
- mixed user disclosure
- self-view enabled/disabled
- 31 payments + change
- exact 32 payments
- explicit zero padding
- recipient scan
- auditor/self-view verification
- payroll reserve/prove/broadcast/reconcile/report

placeholder는 값을 생성/조회하는 명령을 바로 제공함.

## 10. Workstream H: Documentation/Fixtures/Release Pack

갱신 대상:

- normative batch spec
- circuit guide
- Go/JS SDK handoff
- client API/UX/risk docs
- CLI reference
- testing/localnet tutorial
- prover production profile
- operations/security/threat docs
- payroll product/policy/handoff
- JSON schema/conformance fixture
- release pack scripts/verifier

문서에서 다음을 분명히 함.

- public input counts/grouping leakage
- output padding state/gas cost
- remote prover whole-batch witness trust
- audit ciphertext decryptability 비보장
- code는 experimental이며 formal setup/audit 미수행

## 11. Commit 전략

1. `feat: prepare batch shielded transfers`
2. `feat: prove batch transfers through the prover service`
3. `feat: broadcast and scan batch transfer outputs`
4. `feat: execute payroll with batch joinsplit operations`
5. `feat: add batch transfer cli and localnet workflow`
6. `docs: publish batch transfer client integration contracts`

각 commit은 관련 tests와 build를 통과함.

## 12. 검증

```bash
go test ./x/privacy/client/sdk/batchtransfer -count=1
go test ./x/privacy/client/sdk/provertransport ./x/privacy/client/sdk/proverservice -count=1
go test ./x/privacy/client/sdk/provider ./x/privacy/client/sdk/scan -count=1
go test ./x/privacy/client/sdk/payroll ./x/privacy/client/sdk/reservation -count=1
go test ./x/privacy/client/cli -count=1
go test ./x/privacy/... -count=1
make examples
make privacy-batch-joinsplit-localnet
make privacy-e2e-smoke
make reference-payroll-live-localnet
make release-check
make release-pack
make release-pack-verify
git diff --check
```

## 13. 중단 조건

- SDK와 keeper canonical roots/digest가 다름.
- remote prover contract가 prepared payload mutation을 막지 못함.
- 32 output typed scan pagination이 lossless하지 않음.
- tx/message hard limit에서 max case가 구조적으로 불가능함.
- payroll item evidence를 batch chain success와 분리할 수 없음.
- 새 Critical/High protocol/privacy issue가 발견됨.

Core invariant를 낮추지 않고 blocker를 보고함.

## 14. 범위 밖

- external ZK audit
- official MPC/trusted setup
- production VK/genesis deployment
- production remote prover infrastructure
- downstream JS SDK/wallet 실제 제품 구현
- production DB/scheduler/multi-tenant deployment

Go reference와 conformance fixture는 repo에 있으나, 2026-07-12 재검증에서 one-proof payroll 및 live disclosure/view-tag tutorial/E2E 공백을 확인해 CLI/tutorial 완료 claim을 철회함.

## 15. Acceptance Criteria

- [x] batchtransfer SDK가 1..16 input/1..32 output을 준비함.
- [x] one owner signature가 final roots/payload/chain/expiry를 인증함.
- [x] fixed-size NoteV1/disclosure를 사용함.
- [x] local/remote prover route가 payload-bound proof를 생성함.
- [x] prover admission/body/error/log 경계가 안전함.
- [x] structured signer가 roots/payload/intent와 global secret independence를 서명 전에 독립 재계산함. (`G3B-04` closure: `79ea24e`)
- [x] broadcast retry가 idempotent하고 nullifier-first reconcile을 사용함.
- [x] typed scanner가 32 outputs를 누락/중복 없이 복구함.
- [x] scanner 기본값이 view-tag mismatch에서도 decrypt하며 typed-query failure를 lossy fallback하지 않음.
- [x] user/audit/self-view disclosure가 full/user roots와 일치함.
- [x] payroll이 31+change와 exact 32 payment를 올바르게 계획함.
- [x] payroll durable schema가 many-input reservations -> one batch operation -> many item outputs를 실제 SQLite/PostgreSQL transaction에서 atomic하게 표현함. (`G3B-03` closure: `bbf168f`)
- [x] batch status와 item evidence status가 분리됨.
- [x] CLI/localnet tutorial이 one-proof payroll과 live disclosure/view-tag 검증까지 처음부터 끝까지 실행됨. (`G3B-01` closure: `1e855cb`; `G3B-02` closure: `009cc36`)
- [x] `S4-B03` exact duplicate-inflation witness와 원인 분리 control, `S4-B02` production 2x2 disclosure-blinding relation/structured pre-sign/artifact regression 및 Gate 1/2/3A fresh closure가 완료됨. Session 3B re-entry에서 `G3B-01..04`도 모두 닫혀 Gate 3B는 PASS임.
- [x] schema/fixture/docs/release pack이 현재 `S4-B02`/`S4-B03` closure contract와 artifact version에 일치함. 향후 `G3B-01..04`/`S4-B01` 보완이 contract를 바꾸면 다시 검증해야 함.
- [x] artifact/secret이 tracked되지 않음.
- [x] master ledger가 갱신됨.

## 16. Session 4 Handoff

## 2026-07-13 Session 3B Re-entry Completion Record — Current Gate Record

### 범위와 frozen contract

- 시작 HEAD/worktree: `16d2280`, clean `private/multi-circuit-b`.
- Gate 1/2/3A의 기존 PASS를 다시 열지 않았고 public input, NoteV1, payload/schema/version, circuit contract를 변경하지 않았다.
- `examples/clairveil-dapp`을 수정하지 않았으며 formal trusted setup, production artifact publication, Session 4 Pass A~I를 수행하지 않았다.

### Finding closure와 commit

| Finding | Closure commit | 구현 및 실제 증거 |
| --- | --- | --- |
| `G3B-04` | `79ea24e` | Structured batch signing request와 final prepared validator가 같은 global secret-reuse 검사 경로를 사용한다. Input/output 및 output 간 randomness/user/full blinding 재사용 intent 4종이 signer callback 호출 전에 거부된다. |
| `G3B-03` | `bbf168f` | 실제 SQLite와 Docker `postgres:17-alpine`에서 many-input reservation -> one operation -> many item/evidence graph CRUD, forced mid-transaction rollback, reopen, lease acquire/heartbeat/expiry takeover, stale token/CAS conflict, sequential/concurrent duplicate active reservation을 검증한다. Runner는 PostgreSQL을 준비할 수 없거나 test가 skip되면 실패한다. |
| `G3B-01` | `1e855cb` | 실제 localnet one-proof `3-input/4-output` payroll이 durable reservation graph -> `BatchProofWorker` -> `IdempotentBatchBroadcastWorker` -> `BatchReconcileWorker` -> typed item evidence/report 경로를 사용한다. 분리 process, 실제 node restart, send 전 timeout, 저장된 동일 signed bytes retry, tx-hash-first reconcile, spent-nullifier `ManualReview`, payment item별 `Succeeded`/`ManualReview`를 확인한다. |
| `G3B-02` | `009cc36` | 같은 live output 4개를 Bob/Alice recipient key로 복호화하고 auditor/self-view payload 전부와 public/recipient-encrypted user payload를 실제 키로 복호화한다. Output count/index, NoteV1 commitment, recipient, amount, asset, disclosure digest를 독립 재계산하며, 변조한 view tag에서도 안전 기본 scan이 4개 output을 누락하지 않는다. |

### 검증 기록

| 검증 | 결과 |
| --- | --- |
| Session 3B targeted signer/SDK/payroll/scan/provider test | PASS |
| `make reservation-sql-integration` | PASS; SQLite와 PostgreSQL exact PASS marker, skip 0 |
| `RUN_LOCALNET=1 CLAIRVEIL_BATCH_ARTIFACT_DIR=/private/tmp/clairveil-session3a-s4b02-artifacts-25c17ef make privacy-batch-joinsplit-localnet` | PASS; one proof/one tx envelope, six stage PID, 실제 node restart, timeout/retry/reconcile/conflict, recipient 4/user 2/audit 4/self-view 4 plaintext, view-tag mismatch safe scan 포함 |
| `go test ./x/privacy/... -count=1` 및 relevant `go test -race` | PASS |
| `go vet ./...`, `make build`, `make examples` | PASS |
| `make release-check`, `make release-pack`, `make release-pack-verify` | PASS |
| `git diff --check` | PASS |

### Gate 처분

- **Gate 3B: PASS.** `G3B-01..04` active finding은 0건이다.
- **Session 3B closure 당시 Session 4: UNBLOCKED / NOT STARTED.** 이 Session 3B closure에서는 Session 4 독립 검증, fresh publication benchmark/fuzz/Pass A~I 또는 공개 승인을 수행하지 않았다.
- 당시 `S4-B01` live two-endpoint no-failover 검증은 Session 4가 독립적으로 확인할 active security-relevant Medium이어서 publication이 **BLOCKED**였다. 문서 앞의 current Session 4 후속 상태가 이 historical disposition을 supersede한다.
- Formal trusted setup, external audit, production artifact provenance/custody, production remote prover/DB/scheduler 운영은 Production TODO로 유지한다.

## 2026-07-12 Independent Revalidation — Historical Gate Record (Superseded)

### 2026-07-13 Gate 3A Re-entry Disposition

- Gate 1/2/3A fresh closure가 PASS했으므로 **Session 3B integration/test re-entry는 UNBLOCKED**다.
- 이 당시 판정은 아래 당시 Gate 3B FAIL을 닫지 않았다. `G3B-01..04`와 `S4-B01`은 해당 loop의 명시적 범위 밖이라 수정하지 않았고, Session 3B 구현도 시작하지 않았다.
- 다음 허용 단계는 frozen public input/NoteV1/payload/circuit contract를 유지한 채 `G3B-01..04`를 구현·검증하는 Session 3B re-entry다. Gate 3B가 PASS하기 전 Session 4/publication은 계속 **BLOCKED**다.

- 당시 상태: **BLOCKED — Gate 3B FAIL.** 아래의 더 오래된 PASS claim은 이 재검증 record에 의해 supersede됐고, 이 FAIL record는 위 2026-07-13 re-entry closure에 의해 다시 supersede됨.
- review scope: `e427370..d45f0753c16571743f630599776c9cd498d1e8c9`.
- BLOCKED validation record commit: `773e97d5dac68c485479cfe8de40c1d002cb5240`.
- fresh reviewer가 Master Roadmap과 Session 1~4 문서를 전부 읽고 code/runner/test에서 실제 경계를 재구성했으며, finding을 확정하기 전에는 파일을 수정하지 않았음.
- **High G3B-01:** `privacy-batch-joinsplit-localnet.sh`는 one-proof CLI shape만 실행하고 payroll operation graph, `BatchProofWorker`, `IdempotentBatchBroadcastWorker`, `BatchReconcileWorker`, item evidence/report를 연결하지 않음. `reference-payroll-live-localnet.sh`는 legacy multi-message 2x2 `transfer-batch` 경로이므로 one-proof batch payroll E2E 증거가 아님.
- **High G3B-02:** localnet은 mixed disclosure/self-view 옵션을 생성하지만 recipient/auditor/self-view plaintext를 복호화해 blinding 기반 digest를 재계산하지 않으며, expected output count/commitment와 view-tag mismatch safe scan을 assert하지 않음.
- **Security-relevant Medium G3B-03:** `SQLStore` 구현과 dialect schema는 존재하지만 실제 SQLite/PostgreSQL graph CRUD, transaction rollback, reopen, lease/CAS test가 없음. Schema 문자열 test는 DB transaction atomicity 증거가 아님.
- **Security-relevant Medium G3B-04:** structured batch signer validator가 final prepared validator의 global secret-reuse 검사를 수행하지 않아 비신뢰 preparer가 input/output 또는 output 간 재사용 intent에 서명을 먼저 얻을 수 있음.
- protocol/public input/NoteV1는 이 네 Gate 3B finding을 닫기 위해 변경할 필요가 없음. Session 3B 구현/runner/test에서 수정하고 fresh localnet·SQL integration·targeted/full regression을 다시 수행해야 함.
- 보조 검증은 independent NoteV1/batch KAT, payroll no-failover/explicit-opt-in unit test, durable reconcile/permit lifetime/file-store test까지 PASS했으나 live/SQL gap을 대체하지 않음.
- Pass A~I, fresh max-shape benchmark, fresh localnet, full race/fuzz/release gate는 Gate 3B가 닫힐 때까지 수행하지 않음.

## Historical Completion Record (Gate claim superseded)

- 당시 기록 상태: **Complete — Gate 3B 충족.** 현재 gate 판정으로 사용하지 않음.
- 작업 기준/시작 commit: `cd9b4124ee0a7d3f7faeec1e76f765ec3330a88d`
- 완료 구현 commit: `43b49460cacc91e27ab0ae8cdb9607b44d2edce8`
- 최초 completion/ledger closure commit: `b2fa95661590f681d268885c7dfdf7e9af3581ba`
- Session 4 진입 재검증 closure claim: `423f73a59dda472cf0ca959335e4ba8d6bcc64f2` (2026-07-12 재검증으로 superseded)
- review scope: `cd9b4124ee0a7d3f7faeec1e76f765ec3330a88d..HEAD`

### Gate 3A 선행 검증과 frozen core

- Session 3A Completion Record, production circuit, 12 public input, `MsgBatchTransfer`, keeper/gas/atomic state, typed scan state, genesis, artifact descriptor/readiness와 direct core integration을 현재 코드에 대조함.
- 작업 시작 전 `go test ./x/privacy/... -count=1`과 `make release-pack-verify`가 PASS했으며, repo 밖 development artifact의 batch R1CS/PK/VK SHA-256이 각각 `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`로 Completion Record와 일치함.
- **Gate 3A: PASS.** Session 3B에서 production circuit, public witness 순서/의미, keeper invariant와 gas model은 변경하지 않았음. `x/privacy/circuit/batch_joinsplit_16x32_bench_test.go`에는 test-only `3x4` 측정 shape만 추가함.

### SDK, payload, signer, prover contract

- SDK: `x/privacy/client/sdk/batchtransfer`; prepared payload `batch-transfer-payload-v1`, prepared proof `batch-transfer-proof-v1`, active circuit set `privacy-note-v1`, circuit `batch-joinsplit-16x32-v1`.
- prover request/response version은 `v1`/`v1`, route는 `POST /v1/proofs/batch-transfer`임. local prover와 사용자가 명시적으로 선택한 remote prover 한 곳만 지원하며 automatic multi-prover failover는 기본 비활성화가 아니라 **구현 자체가 없음**.
- planner는 1..16 input과 1..32 payment/change/padding output, compact/exact32, active zero padding을 지원함. 각 active output의 output randomness, user disclosure blinding, full disclosure blinding을 독립 생성/검증함.
- structured signing request가 ordered input/output 전체, owner keys, final root, asset, totals, audit identity, self-view mode, 네 vector root, canonical payload, payload digest, chain/expiry와 expected intent를 독립 재계산함. opaque intent bytes만 넘기는 blind-signing API는 없음.
- prepared/proof 파일은 version과 request payload hash를 검증하고 `0600`으로 저장함. mutation, version/hash mismatch, expiry, malformed proof를 build/broadcast 전에 거부함.
- prover 기본 body limit은 wire와 gzip decompressed body 각각 `8 MiB`, circuit별 admission은 in-flight `1`/queued `4`임. bounded framing 뒤 permit을 얻고 semantic validation부터 실제 gnark prove return까지 유지함. saturation은 payload-free HTTP `429 busy`, cancellation은 진행 중 prove가 끝나기 전에 permit을 반환하지 않음.

### Broadcast, typed scan, disclosure

- signed tx bytes를 최초 전송 전에 durable storage에 stage하고 tx hash를 먼저, 이어 모든 input nullifier를 batch query해 reconcile함. ambiguous retry는 같은 signed bytes만 재전송하며 자동 재서명/부분 output retry를 하지 않음.
- scan schema는 `privacy-scan-v2`, cursor는 `(height, global_sequence, output_index)`인 `PrivacyScanCursorV1`임. Deposit/2x2/Batch가 같은 typed cursor를 사용하며 typed query 실패 시 ciphertext가 없는 ABCI event로 fallback하지 않음.
- scanner safe default는 view-tag mismatch에서도 decrypt를 수행하고 tag-only skip은 명시적 opt-in임. 32 output pagination은 cursor가 페이지 중간에서 끊겨도 transactional retry와 de-duplication으로 lossless하게 복구함.
- owned `NoteV1` plaintext의 commitment를 재계산한 뒤 wallet에 넣고 `AssetRegistryV1` asset ID/denom mapping을 역검증함. output별 public/recipient user disclosure와 audit/self-view full disclosure를 output index, commitment, policy, digest, blinding에 대해 검증함.
- live regression에서 기존 2x2 real output만 self-view를 갖는 framing과 typed scanner의 lower-case tx hash를 보존하도록 호환성 결함 두 건을 수정함. batch-level self-view all-or-none와 tx-hash identity 검증은 그대로 유지함.

### Durable payroll contract

- schema version은 `privacy-payroll-batch-v1`이며 `many input reservations -> one BatchOperation -> many PayrollItemOutput/ExpectedOutputEvidence` 관계를 memory, private file, SQLite/PostgreSQL dialect schema로 저장함.
- batch reservation/lease token/CAS, encrypted prepared payload/proof/signed tx bytes, payload/proof/tx/sign-doc hash, account sequence, broadcast history, output별 evidence를 atomic하게 유지함. expired proving lease recovery와 caller cancellation 이후 실제 prove 종료까지 이어지는 heartbeat를 검증함.
- operation chain status와 item evidence status(`Pending`, `Succeeded`, `ManualReview`, `Failed`)는 별도임. 모든 input note가 spent여도 expected output commitment/recipient/amount/asset/disclosure evidence가 맞지 않으면 payroll item을 성공 처리하지 않음.
- 31 payments+change와 exact 32 payments를 한 proof로 계획하고, restart/ambiguous broadcast/repeated reconcile에서 operation ID와 reservation을 유지함. 동일 evidence 재처리는 idempotent함.

### 실제 localnet E2E 결과

`RUN_LOCALNET=1 CLAIRVEIL_BATCH_ARTIFACT_DIR=/tmp/clairveil-session3a-artifacts-381c984 make privacy-batch-joinsplit-localnet`을 최종 구현에서 실행함. 모든 proof는 `/v1/proofs/batch-transfer`를 거쳤고 automatic failover는 사용하지 않았음.

| case | shape | tx hash | DeliverTx | gas used | prepared/proof bytes |
| --- | --- | --- | ---: | ---: | ---: |
| 1 payment | 1/1 | `5CCE911550AAA40A8574F18898901D98BF7A7973585796F99CB4560390CBC17A` | `0` | `1,609,514` | `7,947 / 410` |
| mixed disclosure | 3/4 | `9D6F7B2A110B5E4E97F4A2D054A1046BF8CC5A00FF79306AD0950FCE13F2665B` | `0` | `3,141,099` | `25,878 / 410` |
| 31 payments + change | 16/32 | `15675341BC6EFEA1D400963B02648D7E78E6B13C5A44E8EEB8682D3CECB2ADD8` | `0` | `16,017,355` | `132,583 / 410` |
| exact 32 payments | 16/32 | `1AB163FF489B938335990C1A07D3BCA71AF8603E8CC08E0F464BF15D621F0A12` | `0` | `16,876,619` | `154,135 / 410` |
| explicit zero padding | 1/32 | `8B88CD2D0315C5647A9EC37E9539D392009F46AC54AED984240C0B15159CFC65` | `0` | `15,529,326` | `79,306 / 410` |

- node/prover restart 뒤 stored exact-32 tx hash를 canonical query로 reconcile함.
- prepared payload/proof를 CLI에 다시 넘겨 새 account sequence로 서명한 tx `D298FC32D3DCD9658F9655FB44172A29C3C6E0D2EDEF7CC1F64FFB588C54C86F`이 DeliverTx code `18`, `batch nullifier 0 was already used`로 거절됨을 확인함. 이 runner는 durable worker의 exact-signed-byte ambiguous retry를 검증하지 않으며, 해당 불변식은 payroll worker/store 테스트가 담당함.
- 결과 schema `clairveil.batch-transfer.localnet-result.v1`의 `status=passed`, `restart_tx_hash_reconciled=true`, `spent_nullifier_retry_rejected=true`, `restart_retry_scope=tx-hash-reconcile-and-freshly-signed-spent-nullifier-rejection`을 확인함. 결과는 ignored test workdir `tmp/privacy-batch-joinsplit-localnet/out/`에만 생성되고 artifact/secret은 tracked되지 않음.
- 기존 회귀는 `make privacy-e2e-smoke`의 Deposit/2x2 세 disclosure mode/Withdraw/prepared Withdraw, `TRANSFER_BATCH_COUNT=2` multi-message localnet, `make reference-payroll-live-localnet`의 durable reservation/settlement/idempotent retry를 각각 실제 체인에서 PASS함.

### 1/1, 3/4, 16/32 resource snapshot

Apple M5 Pro에서 production CCS를 한 번 compile해 재사용하고 `-benchtime=1x -count=1 -benchmem`으로 측정한 단일 snapshot임. SLA나 shape 간 유의미한 성능 비교로 해석하지 않음.

| shape | solve ns/op | B/op | allocs/op | actual localnet gas |
| --- | ---: | ---: | ---: | ---: |
| 1/1 | `385,685,250` | `238,733,528` | `11,439` | `1,609,514` |
| 3/4 | `377,207,541` | `238,752,168` | `11,534` | `3,141,099` |
| 16/32 exact-32 | `390,604,625` | `238,994,416` | `17,321` | `16,876,619` |

production circuit constraint는 Session 3A와 같은 `1,111,837`이며 formal setup resource 측정으로 해석하지 않음.

### 검증 기록

| 명령/검증 | 결과 |
| --- | --- |
| Session 3B 문서의 5개 targeted SDK test 묶음 | PASS |
| `go test ./x/privacy/... -count=1` | PASS |
| `go test -race ./x/privacy/client/sdk/... ./x/privacy/client/cli ./cmd/clairveil-payroll -count=1` | PASS; macOS linker의 비치명적 `LC_DYSYMTAB` warning만 관찰 |
| `go vet ./...` | PASS; unkeyed disclosure input 두 곳을 keyed literal로 수정 후 재통과 |
| `make examples` | PASS |
| batch 전용 actual localnet | PASS; 5 cases + restart tx-hash reconcile + freshly signed spent-nullifier fail-closed smoke |
| `make privacy-e2e-smoke` | PASS |
| `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check` | PASS |
| `make reference-payroll-live-localnet` | PASS |
| `make release-check` | PASS; 전체 Go test/build/examples/vulnerability policy/plain localnet/privacy E2E/multi-message 포함 |
| `make release-pack` / `make release-pack-verify` | PASS |
| `git diff --check` | PASS |

### Handoff, invariant matrix, residual risk

- conformance fixture: `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json`.
- invariant traceability matrix: [한글](../docs/clairveil-batch-joinsplit-16x32-kr.md#13-invariant-traceability-matrix), [영문](../docs/clairveil-batch-joinsplit-16x32.md#13-invariant-traceability-matrix).
- Session 3B handoff: [한글](../docs/clairveil-session3b-batch-transfer-handoff-kr.md), [영문](../docs/clairveil-session3b-batch-transfer-handoff.md). CLI/localnet tutorial과 release pack verifier가 fixture/schema/handoff를 포함함.
- review에서 expired `Proving` lease recovery, failed tx result propagation, repeated reconcile evidence 보존, plan/payload recipient·disclosure binding, nullifier lookup binding, ambiguous `Unknown` operation/reservation consistency를 보강함. 이후 실제 회귀에서 발견한 legacy 2x2 self-view framing과 tx-hash case normalization도 회귀 test와 localnet 재실행으로 닫음.
- 최종 active Critical/High/Medium/actionable finding은 `0`건임. Formal trusted setup, production artifact, external ZK audit, managed production DB/multi-tenant SaaS, production remote prover deployment, downstream JS SDK/wallet 제품은 범위 밖이며 수행하지 않았음.
- in-process gnark prove의 hard cancellation/resource containment에는 production process isolation이 필요함. fixed version이 없는 `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`와 examples npm low 1건은 기존 release policy의 known dependency risk로 계속 추적함.
- formal trusted setup: **NOT PERFORMED**.
- external audit: **NOT PERFORMED**.
- **Gate 3B: PASS.** prepare -> local/remote prove -> build -> broadcast -> typed scan/disclosure -> payroll item reconcile의 end-to-end가 frozen Session 2/3A contract를 낮추지 않고 통과함.
- **Session 4 (최초 closure 시점): Unblocked, Not Started.** 당시 Session 3B 구현에서는 독립 검증을 시작하지 않았음.
- 최초 closure worktree 상태: completion/ledger commit과 release-pack 재검증 뒤 `git status --short --branch`가 clean이었음.

### Session 4 진입 시 Gate 3B 재검증 closure

- 최초 Session 4 독립 검토에서 `b2fa956` 뒤의 42개 미커밋 integration 변경 때문에 기존 Completion Record의 clean-tree 및 E2E/release 증거를 현재 tree에 적용할 수 없다고 판정하고 Gate 3B를 일시 `FAIL`로 재개방했다. Session 4 Pass A~I는 이 closure가 끝날 때까지 시작하지 않았다.
- 현재 변경을 `d9b1780`(batch preparation), `8dfe80b`(remote prover transport), `868f108`(typed scan pagination), `0b6b3ee`(durable payroll reconcile), `d7809e9`(localnet/publication hygiene)의 독립 작업 commit으로 정리했다. Production circuit, NoteV1, 12 public input, proto/public witness, keeper consensus contract는 변경하지 않았다.
- 현재 tree에서 targeted SDK/CLI test, `go test ./x/privacy/... -count=1`, `go test -race ./x/privacy/client/sdk/... ./x/privacy/client/cli ./cmd/clairveil-payroll -count=1`, `go vet ./...`, `make examples`를 재실행해 PASS했다.
- `RUN_LOCALNET=1 CLAIRVEIL_BATCH_ARTIFACT_DIR=/tmp/clairveil-session3a-artifacts-381c984 make privacy-batch-joinsplit-localnet`은 5개 shape, node/prover restart, tx-hash reconcile, freshly signed spent-nullifier fail-closed smoke와 no automatic failover를 PASS했다. Artifact size/SHA-256은 이 record의 Session 3A 값과 일치했다.
- `make privacy-e2e-smoke`와 `make reference-payroll-live-localnet`을 fresh localnet에서 재실행해 Deposit/2x2/Withdraw와 durable payroll reserve/prove/broadcast/reconcile/idempotent retry가 PASS했다.
- `make release-check`, `make release-pack`, `make release-pack-verify`, `git diff --check`를 현재 closure tree에서 다시 실행해 PASS했다. Release check 내부의 전체 Go test/build/examples/vulnerability policy/plain localnet/privacy E2E/2-message bulk readiness도 모두 PASS했다. Closure commit 뒤 clean manifest와 status를 한 번 더 확인하고 **Gate 3B를 PASS로 재확정해 Session 4를 시작**한다.

최초 Session 3B 구현은 Session 4를 수행하지 않고 종료했다. 위 문단은 당시 closure claim을 보존한 historical record이며, 현재 gate는 이 문서 앞의 2026-07-13 Session 4 후속 상태에 따라 **Gate 3B PASS, Session 4 PASS, `PUBLICATION_READY_EXPERIMENTAL`**이다.
