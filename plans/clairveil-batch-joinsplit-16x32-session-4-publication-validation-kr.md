# Session 4 Plan: 독립 검증과 GitHub 코드 공개 Gate

## 메타데이터

| 항목 | 내용 |
| --- | --- |
| 상태 | Complete (`PUBLICATION_READY_EXPERIMENTAL`) |
| 선행 문서 | [Master Roadmap](clairveil-batch-joinsplit-16x32-roadmap-kr.md), Session 1~3B 계획과 completion record |
| 권장 모델 | `gpt-5.6-sol` |
| 권장 effort | `ultra` |
| 실행 환경 | 구현 세션과 분리된 fresh Codex session |
| 완료 목표 | protocol/core/client integration을 독립 재검증하고 experimental source 공개 gate를 통과함 |
| 비목표 | production-ready 선언, 외부 ZK audit, 공식 MPC setup, downstream 배포 |

## 1. 독립 검증의 의미

Session 1~3B의 설명을 정답으로 가정하지 않고 code에서 circuit statement와 trust boundary를 다시 도출함. Completion record는 scope와 재현 명령을 찾기 위한 입력임.

`ultra` 내부 delegation을 사용할 수 있으나 main agent가 모든 actionable finding을 직접 재현/판정함. 이는 external cryptographic audit가 아님.

## 2. Publication State

이 세션이 승인할 수 있는 상태:

```text
PUBLICATION_READY_EXPERIMENTAL
```

의미:

- source/test/docs를 GitHub에 공개할 수 있음.
- 알려진 Critical/High soundness, consensus, authorization 결함이 없음.
- development setup으로 기능/성능을 재현할 수 있음.
- no formal setup/no external audit/residual risk를 공개함.

승인하지 않는 상태:

```text
PRODUCTION_RELEASE_READY
```

## 3. 진입 Gate

- Session 3B review scope가 명시됨.
- core direct integration과 full client/payroll localnet E2E가 통과함.
- final NoteV1, 12 public input, proto/API/fixture versions가 기록됨.
- development artifact checksum과 생성 명령이 기록됨.
- invariant traceability matrix가 있음.
- worktree가 clean함.

```bash
git status --short --branch
git log --oneline --decorate -15
git diff --stat <SESSION_1_BASE>..HEAD
git diff --check <SESSION_1_BASE>..HEAD
```

scope base가 불명확하면 master ledger와 history를 먼저 정리함.

## 4. Finding Policy

반드시 수정:

- consensus inflation/reserve drain
- double spend/nullifier reuse
- owner intent/payload/chain/expiry bypass
- invalid proof/public witness acceptance
- active-prefix/disabled-slot bypass
- NoteV1 cross-circuit mismatch
- atomic state partial commit
- scanner가 invalid note를 valid로 저장함.
- permissionless unbounded CPU/RAM/state amplification
- secret/private witness log/fixture/tracked-file 노출

Gate:

- unresolved Critical/High = 0
- unresolved security-relevant Medium = 0
- accepted operational/Low residual은 owner, 이유, production blocking 여부를 문서화함.

## 5. Pass A: Current Security Remediation 재검증

### 5.1 JoinSplit duplicate exploit

same note/commitment/path/nullifier를 두 input에 복제하고 output 합계를 두 배로 만든 witness가 실패해야 함.

### 5.2 TransferIntent

다음 mutation이 실패해야 함.

- recipient/amount/output commitment
- ciphertext/view tag
- user/audit/self-view payload/target
- Merkle root/nullifier order
- chain ID/circuit set
- expiry extension

`creator`만 교체한 relayer sponsorship은 성공해야 함.

### 5.3 SpendIntent

- withdraw expiry extension 실패
- cross-chain replay 실패
- recipient/amount/asset mutation 실패
- creator replacement 성공
- leading-zero가 다른 raw recipient address의 digest/intent가 서로 다름.

Keeper의 chain/expiry check만 통과하고 owner signature에는 묶이지 않는 field가 없는지 확인함.

### 5.4 Current disclosure/state/decoder/privacy defaults

- current user/audit/self-view digest에 독립 secret blinding이 들어가며 offline dictionary vector가 실패함.
- Deposit/2x2/Batch/genesis를 가로지르는 global commitment collision이 state 변경 없이 거부됨.
- malformed/non-canonical/identity/non-subgroup ECIES key와 EdDSA signature가 panic 없이 거부됨.
- prover endpoint timeout이 explicit opt-in 없이 다른 endpoint로 payload를 전송하지 않음.
- forged historical root genesis와 consensus/local artifact mismatch가 startup/readiness를 통과하지 못함.

## 6. Pass B: NoteV1 재검증

code에서 다음 식과 domain을 독립 도출함.

- asset ID
- note commitment
- note nullifier
- note tree parent
- empty tree level별 root
- asset ID/denom registry round trip

다음을 비교함.

- native NoteV1 helper
- DepositCircuit
- SpendCircuit
- JoinSplitCircuit
- BatchJoinSplit16x32
- scanner recomputation
- JS/conformance golden vector

공격/negative:

- commitment/nullifier/tree-node domain 교환
- field order 변경
- randomness reuse with different note
- identity/low-order/off-curve/non-canonical keys
- legacy JSON/fixed payload ambiguity
- unknown version/reserved/trailing bytes
- active zero commitment/nullifier
- forged historical root/export-import state omission

## 7. Pass C: Batch Circuit Statement 재구성

code만 읽고 다음을 별도 표로 작성한 뒤 normative docs/matrix와 비교함.

- 12 public inputs and order
- secret witness
- count bounds/enabled prefix
- disabled sentinel
- vector internal node type/level domain
- 16 membership paths
- NoteV1 nullifier/commitment roots
- active-only pairwise distinctness
- owner/asset equality
- amount range/conservation
- user/full disclosure roots
- SHA-256 digest limbs
- single owner signature

docs-only constraint와 undocumented circuit constraint 모두 finding으로 처리함.

## 8. Pass D: Adversarial Batch Witness

### Count/slots

- count 0/max+1/out-of-range limb
- sparse active representation
- disabled non-zero amount/randomness/path/helper
- active zero vs disabled sentinel confusion
- output count 뒤에 hidden commitment/data

### Membership/ownership

- invalid gated Merkle path
- different roots
- one input owner/view/asset replacement
- invalid owner signature
- identity/low-order key

### Duplicate/value

- adjacent/non-adjacent duplicate nullifier
- duplicate commitment
- input sum double count
- field wrap attempt
- conservation mismatch

### Roots/digests

- vector reorder/remove/duplicate
- count only mutation
- vector type/domain swap
- user/full digest output index mutation
- payload SHA-256 hi/lo swap
- chain domain hi/lo mutation

### Disclosure

- all-private with non-empty user payload
- change/padding unexpected user disclosure
- audit full digest mismatch
- partial self-view enabled batch
- self-view payload compared against wrong full digest
- missing/reused/zeroed user/full disclosure blinding
- low-entropy amount/recipient dictionary guessing

## 9. Pass E: Differential/Property Test

generator가 다음 범위를 반복함.

- input count `1..16`
- output count `1..32`
- random valid 64-bit amount distributions
- compact/change/padding
- mixed user disclosure
- self-view on/off

native/circuit/keeper/SDK에서 다음 값이 같아야 함. 최소 golden KAT 하나는 production helper를 import하지 않는 작은 independent reference implementation이 frozen bytes를 생성해야 함.

- NoteV1 commitment/nullifier
- nullifier/commitment roots
- user/full roots
- payload digest hi/lo
- chain domain hi/lo
- batch intent
- public witness serialization

mutation은 mismatch 또는 validation failure를 만들어야 함. failing seed/vector를 secret 없이 재현 가능하게 남김.

## 10. Pass F: Host/Consensus

### Proto/types

- count를 field와 slice length로 이중 표현하지 않음.
- structured output exact limits
- invalid enum/version/key/fixed payload fail closed
- total message hard cap

### Keeper

- local duplicate before global state lookup
- historical root/Merkle capacity
- audit target current config
- roots/digest/context chain domain 직접 계산
- proof 전에 deterministic gas
- cheap framing 뒤, expensive key/hash/state/proof 전에 deterministic gas
- proof 후 atomic writes
- error rollback

### Gas/DoS

- invalid proof도 bounded gas
- arithmetic overflow 없음
- 32 output KV state bytes charge
- zero-padding state spam 비용
- multi-message tx에 여러 batch가 들어가도 per-message charge
- proof/hash/tree/global lookup/typed state 비용과 Cosmos KV gas의 중복/누락 없음
- same nullifier를 2x2+Batch 및 Batch+Batch로 한 tx에 넣은 양방향 순서가 전체 rollback됨.

## 11. Pass G: Event/Scan Data Plane

- ABCI event가 summary만 포함함.
- ciphertext/disclosure/nullifier list의 hex-expanded duplication이 없음.
- typed KV record가 payload를 한 번만 저장함.
- Deposit/2x2/Batch 공통 global sequence와 `(height, global_sequence, output_index)` cursor ordering이 stable함.
- batch effect ID가 proof/creator 변경에는 안정적이고 chain/effect mutation에는 달라짐.
- output count/byte budget을 적용함.
- page/retry/restart에서 duplicate/missing output이 없음.
- corrupt state가 panic하지 않음.
- typed query 실패가 ciphertext 없는 ABCI event로 downgrade되지 않음.
- max 16 input path가 하나의 root/height snapshot에서 조회됨.
- genesis export/import 후 scan sequence, leaf index, reserve counter, asset registry가 보존됨.
- decrypt 후 NoteV1 commitment를 재계산함.
- partial batch를 payroll item success로 표시하지 않음.

ABCI result, tx bytes, module KV state에 payload가 몇 번 저장되는지 실제 size report로 확인함.

## 12. Pass H: Prover/Privacy

- validator는 VK만 lazy load함.
- prover는 selected circuit R1CS/PK만 load함.
- batch route admission/body limit이 실제 적용됨.
- queue full/cancel/panic에서 permit을 정확히 회수하되 실제 gnark prove가 끝나기 전 조기 회수하지 않음.
- request/prove error/log/metrics에 witness 정보 없음.
- automatic multi-prover failover off
- fixed-size ciphertext length가 policy별 의도와 일치함.
- scanner 안전 기본값이 view-tag mismatch에서도 full decrypt를 시도함.
- development artifact와 formal artifact 상태를 혼동하지 않음.

## 13. Pass I: Payroll/Reconcile

- input reservation atomicity
- many input reservations -> one batch operation -> many payroll item outputs의 durable join/transaction atomicity
- 31 payments + change / exact 32 payments
- output role/index/expected evidence
- batch chain success와 item success 분리
- nullifier spent만으로 payroll success 처리하지 않음.
- timeout에서 tx hash/nullifier 조회 우선
- new sequence 재서명 전 nullifier unspent
- audit decrypt failure/manual review 분리
- expected evidence에 output index/role, audit key ID/epoch, asset ID가 포함됨.

## 14. Fuzz Targets

- NotePlaintextV1 decoder
- DisclosurePlaintextV1 decoder
- canonical transfer/batch payload encoder/decoder
- `MsgBatchTransfer.ValidateBasic` helpers
- vector root native helper
- typed batch scan decoder/cursor
- batch prover request decoder
- ECIES/EdDSA canonical decoder

Invariant:

- panic 없음
- unbounded allocation 없음
- accepted bytes canonical round trip
- malformed/trailing/non-canonical fail closed
- error string에 secret 없음

CI regression corpus와 수동 bounded fuzz run을 기록함.

## 15. Performance/Capacity

동일 development artifact/machine에서 측정함.

```text
1 input / 1 output
3 input / 4 output
8 input / 16 output
16 input / 31 output
16 input / 32 output
```

측정:

- constraint count/breakdown
- compile/setup time
- R1CS/PK/VK size
- witness/prove median/p95/max
- peak RSS
- proof/verify time
- keeper gas
- protobuf tx size
- ABCI event bytes
- typed KV scan state bytes
- scanner throughput
- per-payment time vs 2x2 baseline

hardware/OS/Go/gnark/sample/warm-cold를 기록하고 production SLA로 표현하지 않음.

## 16. Independent Localnet E2E

fresh state에서 다음을 재실행함.

1. current deposit/2x2/withdraw regression
2. withdraw expiry/cross-chain negative
3. 1/1 batch
4. 3/4 batch
5. mixed user disclosure
6. self-view enabled/disabled
7. 31 payments + change
8. exact 32 payments
9. zero padding
10. recipient/auditor/self-view scan verification
11. payroll reserve/prove/broadcast/reconcile/report
12. prover restart/retry
13. node restart/cursor resume
14. genesis export/import 후 scan/path/payroll resume
15. prover timeout에서 no-failover 기본값과 explicit opt-in 동작

## 17. Code Publication Hygiene

- R1CS/PK/VK development binaries 미추적
- private key/seed/token/audit secret 미추적
- absolute local path/personal identifier 없음
- scratch benchmark/temp file 미추적
- proto/generated/fixture/schema/docs version 일치
- invariant matrix code/test links 유효
- 2x2 multi-message와 one-proof batch 용어 구분
- README/docs에 experimental/unaudited/no-formal-setup 표시
- compact counts/grouping leakage 표시
- remote prover trust/decryptability boundary 표시

새 validation/benchmark report는 한영 페어로 작성하고 release pack 포함 여부를 갱신함.

## 18. 검증 명령

```bash
go test ./... -count=1
go test -race ./x/privacy/... -count=1
make ci
make vulncheck
make examples
make privacy-e2e-smoke
make privacy-batch-joinsplit-localnet
make reference-payroll-live-localnet
make release-check
make release-pack
make release-pack-verify
git diff --check
```

fuzz target은 실제 이름으로 각각 최소 bounded run을 실행하고 completion record에 기록함.

## 19. Commit Policy

1. `test: harden shielded intent and note v1 coverage`
2. `test: harden batch joinsplit adversarial coverage`
3. `fix: address batch publication validation findings`
4. `docs: record batch joinsplit publication validation`

finding이 없으면 억지 refactor를 만들지 않음. finding 수정 후 관련 pass를 처음부터 재실행함.

## 20. Acceptance Criteria

- [x] transfer/withdraw current authorization attacks가 실패함.
- [x] current disclosure dictionary, commitment collision, crypto decoder, prover failover, genesis/artifact identity 회귀가 통과함.
- [x] NoteV1가 all circuits/native/scanner에서 일치함.
- [x] 12 public input statement가 code/docs와 일치함.
- [x] active/duplicate/value/root/disclosure attacks가 실패함.
- [x] single owner signature 외 per-input conditional EdDSA가 없음.
- [x] differential/property/fuzz가 통과함.
- [x] independent reference KAT와 cross-message nullifier composition test가 통과함.
- [x] keeper gas/atomicity/resource bounds가 확인됨.
- [x] minimal event/typed scan index가 payload를 중복 저장하지 않음.
- [x] prover admission/privacy 경계가 확인됨.
- [x] payroll item evidence가 batch status와 분리됨.
- [x] max-shape benchmark가 재현 가능하게 기록됨.
- [x] independent localnet restart/retry가 통과함.
- [x] unresolved Critical/High/security Medium = 0
- [x] accepted residual이 문서화됨.
- [x] secret/artifact/local path가 tracked되지 않음.
- [x] release gate가 통과함.
- [x] master ledger가 `PUBLICATION_READY_EXPERIMENTAL`로 갱신됨.

## 21. Production TODO

실행하지 않고 release-blocking TODO로 유지함.

### Core

- external ZK audit
- final source/constraint freeze
- official MPC/trusted setup
- transcript/toxic-waste evidence
- artifact signing/provenance/reproducibility
- signed production circuit manifest
- release SBOM/provenance

### Downstream chain

- production gas governance/calibration
- VK/circuit hash genesis or upgrade pinning
- validator rollout/rollback
- staging/testnet load/fault rehearsal
- state/Merkle capacity monitoring
- emergency disable/incident response

### Prover/product/auditor

- TLS/auth/ACL/quota/process isolation
- payload log/retention policy
- audit key HSM/KMS/threshold/rotation
- decrypt failure/manual review operations
- downstream JS SDK/wallet implementation
- padding/privacy/cost policy
- payroll production deployment

## 22. Completion Record

- review scope: `e427370..47bcca58edb79e2e0a6a1bcdb8d655842f30898d`
- 시작 HEAD: `b2fa95661590f681d268885c7dfdf7e9af3581ba` (`docs: record Session 3B completion`)
- 최종 HEAD: `47bcca58edb79e2e0a6a1bcdb8d655842f30898d` (Pass A~I, 전체 regression, pinned-tree generation과 external Git-blob verification을 검증한 immutable implementation snapshot; 이후 commit은 publication record만 변경함)
- Gate 3B: 진입 시 dirty integration tree 때문에 최초 차단함. `d9b1780`, `8dfe80b`, `868f108`, `0b6b3ee`, `d7809e9` 수정과 targeted/full privacy/race/vet/examples, 5-case batch localnet, privacy smoke, payroll live, artifact/release 재검증 뒤 `423f73a`에서 closure 처리함.
- findings/fixes: batch preparation (`d9b1780`), remote prover transport (`8dfe80b`), lossless typed pagination (`868f108`), durable payroll (`0b6b3ee`), localnet gate (`d7809e9`), confirmed-failure re-sign (`16900cb`), property/fuzz (`2f4d065`), 5-shape capacity (`7407007`), Deposit plaintext log (`c40c865`), 실제 restart/genesis continuation (`15e644a`), 한영 문서 정렬 (`cee89a7`), 모든 standalone proof body hard bound (`fc45edd`), immutable validation scope, dirty release-pack fail-closed (`8b48483`), pinned Git-tree pack으로 ignored/concurrent input 배제 (`816f627`), raw/service prover boundary 한영 문서 정렬, invalid full SHA 교정과 external archive Git binding (`47bcca5`). Severity, 근거와 영향은 [한글 검증 보고서](../docs/clairveil-batch-joinsplit-16x32-session-4-validation-report-kr.md)에 기록함.
- unresolved findings: Critical 0, High 0, security-relevant Medium 0
- protocol re-entry: public input/NoteV1/protocol contract 변경 finding 없음. Session 2/3A 재진입 불필요
- accepted residual: formal setup/audit/source freeze, production artifact provenance, production chain/prover/auditor/downstream 운영, metadata leakage acceptance, in-process RSS, no-fixed-version Go 3건, example npm Low 1건. Owner·이유·production blocking 여부를 한영 보고서에 기록함.
- property/fuzz results: input `1..16`, output `1..32` seeded property와 independent KAT 통과. Note/disclosure/transfer/batch/vector/scan/prover/ECIES/EdDSA fuzz target 10개를 각각 3초 실행해 모두 통과함.
- benchmark report: Apple M5 Pro, darwin/arm64, Go 1.25.12, gnark 0.14, constraints 1,111,837, setup 15,999.531ms, peak RSS 3,429,646,336 bytes. 1/1·3/4·8/16·16/31·16/32의 witness/prove/verify p50/p95/max, gas/tx/state/event/scanner를 [보고서](../docs/clairveil-batch-joinsplit-16x32-session-4-validation-report-kr.md)에 기록함.
- localnet/restart/retry results: Deposit/2x2/Withdraw smoke, payroll live, 1/1·3/4·31+change·exact32·padding batch 통과. 실제 node/prover restart, tx hash reconcile, spent retry 거부, non-zero-height genesis export/import, cursor/cache/path/reserve/asset 보존, import 뒤 sequence/leaf continuation을 확인함.
- release gate results: `go test ./... -count=1`, full privacy race (`-timeout=30m`; 최초 기본 timeout은 data race가 아님), `go vet`, `make ci`, `make vulncheck`, `make examples`, privacy/payroll/batch localnet, `make release-check`, release pack/verify, diff/hygiene 통과
- publication status: `PUBLICATION_READY_EXPERIMENTAL`
- formal setup: `NOT PERFORMED`
- external audit: `NOT PERFORMED`
- production status: `NOT PRODUCTION-READY`, `NOT AUDITED`
- publication record identity: release pack의 `RELEASE-MANIFEST.txt`가 이 문서를 포함하는 exact Git commit을 기록하고 archive checksum이 그 manifest를 고정함.
- worktree 상태: publication record commit과 최종 release-pack verification 뒤 clean 확인
