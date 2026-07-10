# Session 3B Plan: BatchJoinSplit16x32 Client/Product Integration 구현

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Blocked by Session 3A |
| 선행 문서 | [Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md), [Session 3A](clairveil-batch-joinsplit-16x32-session-3-implementation-kr.md) |
| 후속 세션 | [Session 4 Publication Validation](clairveil-batch-joinsplit-16x32-session-4-publication-validation-kr.md) |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `max` |
| 완료 목표 | SDK prepare부터 prover, broadcast, scan, payroll reconcile까지 batch transfer end-to-end를 완성함 |

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

Go reference, conformance fixture, CLI/tutorial은 repo에서 완성함.

## 15. Acceptance Criteria

- [ ] batchtransfer SDK가 1..16 input/1..32 output을 준비함.
- [ ] one owner signature가 final roots/payload/chain/expiry를 인증함.
- [ ] fixed-size NoteV1/disclosure를 사용함.
- [ ] local/remote prover route가 payload-bound proof를 생성함.
- [ ] prover admission/body/error/log 경계가 안전함.
- [ ] structured signer가 roots/payload/intent를 독립 재계산하고 opaque blind-signing을 허용하지 않음.
- [ ] broadcast retry가 idempotent하고 nullifier-first reconcile을 사용함.
- [ ] typed scanner가 32 outputs를 누락/중복 없이 복구함.
- [ ] scanner 기본값이 view-tag mismatch에서도 decrypt하며 typed-query failure를 lossy fallback하지 않음.
- [ ] user/audit/self-view disclosure가 full/user roots와 일치함.
- [ ] payroll이 31+change와 exact 32 payment를 올바르게 계획함.
- [ ] payroll durable schema가 many-input reservations -> one batch operation -> many item outputs를 atomic하게 표현함.
- [ ] batch status와 item evidence status가 분리됨.
- [ ] CLI/localnet tutorial을 처음부터 끝까지 따라갈 수 있음.
- [ ] existing 2x2/multi-message/payroll regression이 없음.
- [ ] schema/fixture/docs/release pack이 contract version과 일치함.
- [ ] artifact/secret이 tracked되지 않음.
- [ ] master ledger가 갱신됨.

## 16. Session 4 Handoff

```text
## Completion Record

- 시작 commit:
- 완료 commit:
- review scope base..HEAD:
- SDK/payload/prover versions:
- batch route/admission defaults:
- scanner cursor/schema:
- payroll operation/item evidence contract:
- localnet cases/results:
- 1/1, 3/4, 16/32 resource snapshot:
- invariant matrix 경로:
- residual finding:
- formal setup: NOT PERFORMED
- external audit: NOT PERFORMED
- worktree 상태:
```

end-to-end gate가 미완료이면 Session 4를 시작하지 않음.
