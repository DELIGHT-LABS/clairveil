# Batch JoinSplit 16x32 Session 4 독립 검증 보고서

## 상태

| 항목 | 결과 |
| --- | --- |
| 검토 범위 | `e427370..d45f0753c16571743f630599776c9cd498d1e8c9` |
| 검증한 시작 HEAD | `d45f0753c16571743f630599776c9cd498d1e8c9` |
| 검토 역할 | Session 1~3B 구현에 참여하지 않은 fresh reviewer |
| 진입 시 Gate 3B | **FAIL — Session 3B integration/test 재진입 필요** |
| `S4-B02` Session 3A 보완 | **IMPLEMENTATION RESOLVED — Gate 1/2/3A fresh 독립 재검토 필요** |
| Session 4 공개 상태 | **`BLOCKED`** (`PUBLICATION_READY_EXPERIMENTAL` 철회) |
| production release 상태 | 승인하지 않음 |
| formal trusted setup | 수행하지 않음 |
| external audit | 수행하지 않음 |

이 2026-07-12 재검증은 같은 날 앞서 완료된 publication claim을 supersede한다. Gate 3B가 충족되지 않았고 unresolved High/security-relevant Medium이 남아 있으므로 experimental source 공개 승인도 현재 유효하지 않다. Session 3A는 이후 `S4-B02` implementation을 해결했지만 Gate 1/2/3A fresh 독립 재검토나 Session 4 Pass A~I를 수행하지 않았으므로 이 `BLOCKED` 판정은 바뀌지 않는다.

### `S4-B02` Session 3A Implementation Supplement

- 기준 HEAD `0fc818c`; implementation commit `0b7d97d`, `630736f`, `25c17ef`. Latest Master Ledger와 Session 2 Foundation Re-entry record가 authoritative source다.
- Production output 0에 `DBS-01..03`과 all-private canonical sentinel/gating을 구현하고 shared native/prepared validator와 structured 2x2 pre-sign boundary를 exact 정렬했다.
- Constraint count는 control `99,765`에서 production `99,775`로 `+10`이며 frozen target과 일치해 decision change가 없다. Public input 13개/schema hash, NoteV1, payload `v5`, proof/HTTP `v2`, disclosure digest/domain, circuit-set ID는 unchanged다.
- 새 development JoinSplit SHA-256: R1CS `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4`, PK `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7`, VK/consensus identity `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`. JoinSplit-only development rotation이며 Batch와 나머지 artifact는 unchanged다.
- Old/new proof 및 consensus/file mismatch, fresh genesis/reset, strict artifact preflight, 전체 2x2 regression과 full Batch `1,111,837` constraint resource comparison을 통과했다. Formal trusted setup은 수행하지 않았고 generated binary/secret은 tracked하지 않았다.
- `S4-B02` implementation은 **RESOLVED**다. Current unresolved count는 Critical 0, High 2, security-relevant Medium 3이다. Gate 1/2/3A fresh 독립 재검토가 필요하고 Session 3B/Session 4는 시작하거나 재개하지 않았다.

### Historical `S4-B02` Foundation Re-entry Supplement

- clean 기준 HEAD `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`에서 latest Master Ledger와 이 `BLOCKED` record를 authoritative source로 사용했다.
- `c7fc1be`, `a8697cd`, `a4ee959`, `4e75f1f`이 `DISCLOSURE-BLINDING-SEPARATION` V1의 `DBS-01..03`, all-private/disabled gating, shared `DBS_*` error/layer contract, conformance fixture, 2x2 feasibility target을 동결했다.
- Current `99,765` 대비 test-only hardened `99,775` constraints(`+10`), R1CS `+253 B`, PK `+912 B`, VK/proof size 변화 없음, peak RSS `690,438,144 B`, OOM 없음이다. Batch source/artifact는 unchanged `1,111,837` constraints다.
- Public input/NoteV1/payload encoding/disclosure digest/circuit-set version은 변경하지 않는다. Session 3A는 JoinSplit R1CS/PK/VK와 manifest/consensus JoinSplit identity만 교체한다.
- Session 3A re-entry는 **UNBLOCKED / NOT STARTED**이며 `S4-B02`는 production constraint/artifact/pre-sign enforcement와 regression/readiness/resource gate 완료 전까지 **IMPLEMENTATION PENDING / NOT RESOLVED**다.
- `S4-B03`은 `02f61f3746b67d5244c160b7c0e0e42f7c0b78b8`, `42d40bd19523e263aaf1c2043bcd274a4fc1a51d`에서 **RESOLVED**다.

## 현재 확정 Finding

| ID | Severity | 근거 | 영향 범위 | 필요한 조치 |
| --- | --- | --- | --- | --- |
| G3B-01 | High | Batch localnet은 one-proof transfer shape만 실행하고 payroll operation graph/worker/reconcile/report를 사용하지 않는다. Reference payroll localnet은 legacy multi-message 2x2 `transfer-batch` 경로다. | one-proof payroll reserve, prove, signed-byte retry, typed item evidence, reconcile/report가 실제 chain에서 연결된 적이 없다. | Session 3B에서 production payroll worker 전체 경로의 fresh localnet E2E를 추가하고 restart/retry까지 검증한다. |
| G3B-02 | High | Localnet은 mixed disclosure/self-view 옵션만 생성하고 recipient/auditor/self-view decrypt, blinding 기반 digest 재계산, expected output 전체 count/commitment, view-tag mismatch safe scan을 assert하지 않는다. | Typed scanner 또는 disclosure consumer가 output을 누락해도 runner가 성공할 수 있다. | Live disclosure consumer와 view-tag mismatch injection을 추가하고 output별 evidence를 검증한다. |
| G3B-03 | Medium, security-relevant | `SQLStore` 구현은 있으나 test는 schema 문자열, placeholder, isolation option만 검사한다. 실제 SQLite/PostgreSQL CRUD/rollback/reopen/lease-CAS 실행은 없다. | SQL backend의 reservation-operation-item-evidence 원자성이 입증되지 않아 orphan/duplicate/wrong item status 위험이 남는다. | 최소 실제 SQLite transaction/restart/rollback/concurrency test를 추가한다. |
| G3B-04 | Medium, security-relevant | `ValidateBatchTransferSigningRequest`는 final prepared validator와 달리 input/output 및 output 간 global secret reuse를 서명 전에 거부하지 않는다. | 비신뢰 preparer가 privacy-leaking intent에 owner signature를 먼저 얻을 수 있다. | Structured signer validator에 동일한 `seenSecrets` 검사를 넣고 adversarial signing test를 추가한다. |
| S4-B01 | Medium, security-relevant | Default no-failover와 explicit opt-in unit test는 있지만 localnet은 timeout/healthy endpoint의 실제 접촉 횟수를 측정하지 않는다. 결과 JSON 값은 실행 관찰이 아닌 literal이다. | 실제 transport에서 witness가 두 번째 prover로 전송되지 않는 privacy default를 publication evidence로 입증하지 못한다. | 두 endpoint live harness에서 default와 opt-in을 각각 검증한다. |

Resolved supplements: `S4-B03`은 `02f61f3`/`42d40bd`, `S4-B02` implementation은 `0b7d97d`/`630736f`/`25c17ef`로 닫혔으며 현재 finding count에 포함하지 않는다.

## 현재 검증 처분

- Gate 3B FAIL 때문에 Session 4 Pass A~I, fresh max-shape benchmark, fresh localnet, full regression/race/fuzz/release gate는 **수행하지 않았다**. 아래 historical 결과는 현재 gate evidence로 재승인하지 않는다.
- 보조 검증으로 production helper를 재사용하지 않는 `TestPrivacyNoteV1ContractIndependentGolden`과 `TestPrivacyBatchJoinSplitV1ContractIndependentGolden`을 실행해 PASS했다. 이 test source는 frozen domain/encoding/MiMC/vector 식을 독립 계산하며 production NoteV1/root helper를 계산 경로에 사용하지 않는다.
- Payroll default no-failover/explicit opt-in, durable reconcile, prove permit lifetime, memory/file store test는 PASS했다. SQL test는 schema-only여서 G3B-03을 닫지 않는다.
- `/tmp/clairveil-session3a-artifacts-381c984`의 batch R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`와 SHA-256은 historical record와 일치한다.
- Tracked R1CS/PK/VK, `dist/`, `benchmarks/`, `tmp/`, 개인 absolute path 또는 명백한 secret은 발견되지 않았다. `benchmarks/`, `dist/`, `tmp/`는 ignored 상태다.
- 현재 unresolved count는 Critical 0, High 2, security-relevant Medium 3다. Security finding을 accepted residual로 전환하지 않았다.
- Formal setup, external audit, production artifact/provenance, downstream production 운영은 여전히 미수행 Production TODO이며 active finding을 대체하지 않는다.

### 실행한 보조 검증 명령

| 명령 | 결과와 한계 |
| --- | --- |
| `go test ./x/privacy/client/sdk/conformance -run '^(TestPrivacyNoteV1ContractIndependentGolden\|TestPrivacyBatchJoinSplitV1ContractIndependentGolden)$' -count=1 -v` | PASS. Independent golden 계산 경로 확인. Gate 3B 대체 아님 |
| `go test ./x/privacy/client/sdk/payroll -run '^(TestProverPoolDoesNotFailOverAfterEndpointTimeoutByDefault\|TestProverPoolFallsBackAfterEndpointTimeoutWithExplicitOptIn\|TestBatchReconcileDurableRestartRetryTxHashFirstAndItemEvidenceSeparate\|TestBatchProofWorkerKeepsSharedLeaseUntilUninterruptibleProveReturns)$' -count=1 -v` | PASS. Unit 경계만 검증하며 live endpoint evidence 대체 아님 |
| `go test ./x/privacy/client/sdk/reservation -run '^(TestBatchOperationGraphIsAtomicAndConflictsWithOrdinaryReservation\|TestBatchOperationDurableFileRestartRoundTrip\|TestBatchOperationSQLSchemaIsVersionedAndRelational)$' -count=1 -v` | PASS. 실제 SQL transaction test가 아니므로 G3B-03 유지 |
| `go test ./x/privacy/types ./x/privacy/client/sdk/transfer ./x/privacy/client/sdk/conformance -run 'DisclosureBlinding\|AllPrivateUserBlinding' -count=1 -v` | PASS. Shared native/prepared/fixture contract 확인. Production circuit 대체 아님 |
| `CLAIRVEIL_RUN_JOINSPLIT_BLINDING_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestJoinSplitDisclosureBlindingSeparationResourceGate$' -count=1 -v` | PASS. Legacy control `99,765`, production `99,775`; production R1CS `10,824,169 B`, PK `16,766,489 B`, VK `748 B`, proof `164 B`; peak RSS `687,423,488 B` |
| `CLAIRVEIL_RUN_JOINSPLIT_ARTIFACT_ROTATION=1 go test ./x/privacy/zk ./x/privacy/circuit -run 'JoinSplit.*Artifact\|JoinSplit.*Proof' -count=1 -v` | PASS. JoinSplit-only rotation/readiness와 old/new proof identity 상호 거부 확인 |
| `CLAIRVEIL_RUN_JOINSPLIT_FRESH_GENESIS=1 go test ./x/privacy -run '^TestJoinSplitDevelopmentArtifactFreshGenesisGate$' -count=1 -v` | PASS. 새 identity fresh genesis 성공, old identity는 state write 전에 거부 |
| `CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run '^TestBatchJoinSplit16x32FullShapeResourceGate$' -count=1 -v` | PASS. Batch unchanged `1,111,837` constraints; R1CS `122,813,535 B`, PK `209,218,621 B`, VK `716 B`, proof `164 B`; peak RSS `3,324,461,056 B`, OOM 없음 |
| `git merge-base --is-ancestor e427370 HEAD`, `git diff --check e427370..HEAD` | PASS at starting HEAD `d45f0753c16571743f630599776c9cd498d1e8c9` |
| artifact `shasum -a 256`과 file-size 대조 | PASS. Batch R1CS/PK/VK가 historical development hash/size와 일치 |
| tracked artifact/personal-path/secret filename scan | PASS. `benchmarks/`, `dist/`, `tmp/`, `tmpdocs/`, local binary와 dependency output은 ignored/untracked이며 publication evidence가 아님 |
| `go test ./... -count=1`; `go vet ./x/privacy/...`; `make build`; `make examples`; `make vulncheck`; `git diff --check` | PASS. Session 3A implementation 범위의 final repository/release 정적 검증이며 Session 4 Pass A~I 또는 live E2E 재개가 아님 |
| `make release-pack`; `make release-pack-verify` | PASS at clean closure `354509db54f193295d1e1a18f9e4b45de3741d4f`. Required file 125개와 exact manifest commit 검증; final bookkeeping commit에서도 재실행 |
| Pass A~I, race/fuzz, benchmark, fresh localnet/live E2E와 전체 `make release-check` | **NOT RUN — Gate 3B FAIL 및 Session 3A 범위 밖** |

### Accepted Residual과 Production TODO

Active High/Medium은 residual로 수용하지 않았다. 아래 운영 항목만 기존 Production TODO로 유지한다.

| Residual/TODO | Owner | 현재 수용 이유 | Production blocking |
| --- | --- | --- | --- |
| External ZK audit, source/constraint freeze, official MPC/trusted setup과 transcript | Protocol/release owner | Session 4 비목표이며 development artifact만 존재함 | Yes |
| Artifact signing/provenance/custody, production manifest와 SBOM/image provenance | Release/validator operator | Target release infrastructure가 필요함 | Yes |
| Production gas/governance/upgrade/rollback, staging load/fault, monitoring/incident response | Downstream chain owner | Target chain 운영 범위임 | Yes |
| Prover TLS/auth/ACL/quota/process isolation/retention과 audit-key custody/rotation/manual review | Prover 및 auditor/payroll operator | Managed production infrastructure와 운영 절차가 필요함 | Yes |
| Downstream JS/TS wallet/product, metadata leakage와 padding policy | Product/privacy owner | Go reference와 declared leakage만 존재하며 제품 acceptance가 필요함 | Downstream production은 Yes |
| No-fixed-version Go advisory 3건과 example npm Low 1건 | Dependency/security owner | 기존 exact policy에서 추적 중이며 숨기지 않음 | Production 전 재평가 |

이 표는 active Gate 3B/Session 2·3A blocker를 수용하거나 publication을 승인하지 않는다.

## Prior 2026-07-12 Historical Validation Record (Superseded)

아래 내용은 이전 reviewer가 기록한 historical claim을 provenance 목적으로 보존한다. 현재 publication 상태, Pass 결과, benchmark/localnet evidence로 사용하지 않으며 위 2026-07-12 판정이 우선한다.

## 독립 검토 방법

Master Roadmap과 Session 1~4의 모든 plan 및 Completion Record를 처음부터 끝까지 읽고, code에서 protocol을 재구성한 뒤 normative contract와 비교했다. 초기 finding을 재현하고 판정하기 전에는 파일을 수정하지 않았다. Pass A~I보다 Gate 3B를 먼저 확인했으며 clean worktree와 integration evidence가 최초에는 충족되지 않았으므로 gate가 열린 동안 Session 4를 통과 처리하지 않았다.

다음을 독립 재구성했다.

- NoteV1 domain, field order, empty-tree root, vector root, active prefix, disabled sentinel
- 12개 public input과 native, circuit, keeper, SDK, prover witness encoding
- owner authorization, membership, distinctness, conservation, disclosure, payload binding
- keeper gas/resource/atomicity, cross-message composition, scan/genesis state, prover privacy, payroll evidence boundary

## Finding과 수정

| ID | Severity | 근거 | 영향 범위 | 해결 |
| --- | --- | --- | --- | --- |
| S4-01 | High | Batch preparation과 payload builder가 mutable plan의 total, ordering, ownership, disclosure mode, field를 완전히 독립 재계산하지 않았다. | 변조된 prepared object가 planner intent와 다른 의미로 signing/prover 경계를 넘거나 sensitive work 뒤 늦게 실패할 수 있었다. | `d9b1780`: count, ownership, asset, nullifier uniqueness, output role, conservation, disclosure policy, canonical field를 재계산하고 private file을 durable atomic replacement로 기록함. |
| S4-02 | High | Remote prover transport가 non-loopback cleartext HTTP와 일반 redirect를 허용했고 client contract에 인증 수단이 없었다. | private witness가 평문 전송되거나 의도하지 않은 endpoint로 redirect될 수 있었다. | `8dfe80b`: loopback 외 HTTPS 강제, redirect 거부, opt-in redirect-safe custom doer, bearer token, bounded response, atomic private-file write를 적용함. |
| S4-03 | High | Typed scan page가 zero-output summary를 건너뛴 cursor advance를 증명하지 않았고 scanner가 filtered event set을 요청했다. | pagination이 정상 boundary를 거부하거나 output-bearing event 누락을 숨겨 wallet/payroll recovery가 lossless하지 않을 수 있었다. | `868f108`: unfiltered typed scan, ordered summary cursor proof, output event skip 금지, nil cursor 안정화, lossy ABCI fallback 금지를 적용함. |
| S4-04 | High | Durable payroll의 reservation, operation, item, lease, broadcast, reconcile transition이 memory/file/SQL store 전체에서 일관되게 atomic/evidence-bound하지 않았다. | crash/retry race가 reservation을 고립시키거나 중복 작업을 만들고 필요한 batch-output evidence 없이 item 결과를 판정할 수 있었다. | `0b6b3ee`: atomic operation graph persistence, lease/CAS 검사, evidence-bound reconciliation, store별 regression을 추가함. |
| S4-05 | Medium | 초기 batch localnet rehearsal이 모든 negative 결과, artifact identity, process cleanup, repository hygiene를 강제하지 않았다. | Gate 3B evidence가 실제 integration boundary를 증명하지 않고 성공할 수 있었다. | `d7809e9`로 보강하고 targeted/full privacy/race/vet/examples/E2E/payroll/artifact/release 검증 뒤 `423f73a`에서만 Gate 3B를 재기록함. |
| S4-06 | High | confirmed failed payroll tx를 operation graph를 보존하면서 new-sequence 재서명 상태로 옮기는 명시적 store transition이 없었다. | 회복 가능한 chain failure가 durable dead end가 되어 수동 DB 수정 없이는 안전하게 retry할 수 없었다. | `16900cb`: `PrepareBatchOperationResign`을 lease-bound/confirmed-failure 전용으로 추가하고 nullifier를 재확인하며 explicit `ResignWithNewSequence`에서만 계속함. |
| S4-07 | Medium | 여러 decoder의 bounded fuzz와 모든 지원 input/output count의 cross-layer property coverage가 공개 gate 기준보다 부족했다. | canonical decoder, count, root, witness drift를 독립 검출하는 강도가 부족했다. | `2f4d065`: fuzz target 7개, circuit 16개 input shape property, SDK `1..16/1..32`, keeper 12-input property, frozen independent KAT를 추가함. |
| S4-08 | Medium | capacity evidence가 요구된 5개 shape의 distribution과 scanner/wire/state/gas profile을 모두 기록하지 않았다. | experimental 공개에서 resource envelope와 shape 비교를 근거로 제시할 수 없었다. | `7407007`: 5-shape p50/p95/max resource gate, solve/gas/wire/state/event/scanner benchmark를 추가함. |
| S4-09 | High | localnet restart helper가 shell function을 background로 실행해 기록한 PID가 node process가 아니었다. 기존 node가 살아 있으면 새 start가 DB lock으로 실패해도 health check가 통과했다. Genesis continuation도 실행하지 않았다. | restart/recovery 공개 근거가 false positive였다. | `15e644a`: process 직접 실행과 `kill -0` lifecycle 검사, 실제 node/prover restart, non-zero-height genesis export/import, cursor/cache/path/reserve/asset 비교, import 뒤 continuation을 추가함. |
| S4-10 | High | Deposit CLI가 완전한 NotePlaintextV1 hex를 stderr에 출력했다. | receiver key, amount, randomness, memo가 terminal capture 또는 application log에 노출될 수 있었다. | `c40c865`: helper가 message만 반환하고 note plaintext를 log하지 않도록 수정함. |
| S4-11 | Medium | 한영 handoff/security 문서가 구현된 batch Go reference를 여전히 pending으로 표현하거나 downstream product completion과 혼동했다. | reviewer와 integrator가 code와 다른 contract 상태를 신뢰할 수 있었다. | `cee89a7`: 18개 한영 공개 문서에서 구현된 Go reference와 미완료 downstream/formal/production 작업을 분리함. |
| S4-12 | Medium | Public low-level prover `HTTPHandler`는 batch route만 제한했고 transfer/withdraw는 admission 전에 unbounded `io.ReadAll`을 사용했다. | Handler를 직접 mount하면 공격자가 제어한 body를 permit 획득 전에 메모리에 올려 memory DoS를 만들 수 있었다. | `fc45edd`: 세 route가 같은 hard reader를 사용하고 wire/gzip overflow는 413을 반환하며 payload를 error에 포함하지 않는다. Transfer/withdraw overflow가 admission 전 거부되는 test도 추가함. |
| S4-13 | Medium | 초기 Completion Record가 mutable `HEAD` range와 `Session 4 closure commit` placeholder를 사용했다. | 공개한 validation claim을 하나의 immutable source snapshot에서 재현할 수 없었다. | 이 record에서 report, Completion Record, master ledger가 검증한 implementation `494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2`를 고정한다. 생성한 release manifest는 publication-record commit과 archive checksum을 별도로 정확히 기록한다. |
| S4-14 | Medium | Release pack이 working-tree 파일을 복사하면서 `HEAD`만 기록했고 verify도 같은 dirty content를 재생성했다. | Commit에 없는 uncommitted docs/schema/fixture/source가 검증을 통과할 수 있었다. | `8b48483`: copy 전과 manifest 기록 전에 tracked/untracked non-ignored 상태를 검사한다. Dirty generation/default verify는 fail-closed하고 ignored `dist/`·`tmp/` output은 허용한다. |
| S4-15 | Medium | Clean-worktree 검사는 ignored file을 숨기지만 recursive directory copy는 ignored `.env`, `node_modules` 등 local file을 포함했다. | Git status와 claimed commit을 바꾸지 않고 local secret 또는 development artifact가 pack에 유출될 수 있었다. | `816f627`: pinned source commit의 `git archive`에서만 선택 경로를 복사하므로 tracked blob만 들어간다. Copied example directory 안의 ignored `.env` probe가 archive에 없음을 실제 확인함. |
| S4-16 | Medium | Working-tree copy 사이에 다른 process가 변경을 commit하면 두 status check 모두 clean이어서 새 `HEAD`를 기록할 수 있었다. | Archive가 old content를 섞으면서 더 새로운 clean `HEAD`를 provenance로 주장할 수 있었다. | `816f627`: 추출 전에 source SHA를 고정하고 모든 file을 그 Git tree에서 가져오며, manifest 전 HEAD 동일성을 검사하고 pinned SHA만 기록함. |
| S4-17 | Low | `fc45edd` 뒤에도 한영 security/operations 문서 4쌍이 low-level prover handler에 body limit이 없다고 설명했다. | 실제보다 보수적이지만 현재 trust boundary와 달라 downstream review를 혼동시킬 수 있었다. | Publication record에서 raw handler도 hard cap을 유지하고 production wrapper가 auth, gzip wire/decompressed limit, health/readiness policy, server timeout을 추가로 담당한다고 정렬함. |
| S4-18 | High | 최초 immutable record가 short commit `816f627`을 존재하지 않는 40-character object ID로 잘못 확장했다. | Exact validation range가 resolve되지 않아 publication claim을 재현할 수 없고 Gate 4 provenance가 무효였다. | 이 record에서 실제 object `494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2`를 모든 exact range에 고정하고 공개 전에 range를 실행함. |
| S4-19 | Medium | Explicit external archive verify가 out-of-band expected commit과 Git-blob 비교 없이 archive 자체 manifest/checksum을 신뢰했다. | Self-consistent forged/stale archive가 임의 commit을 주장해도 handoff verify를 통과할 수 있었다. | `47bcca5`: explicit verify는 `RELEASE_PACK_EXPECTED_COMMIT`을 요구하고 local object로 resolve하며 canonical manifest commit을 확인한다. Non-regular entry를 거부하고 generated file 외 모든 packed file을 claimed Git tree와 byte-for-byte 비교함. |
| S4-20 | Medium | Git-blob verify가 archive에 남은 file만 순회해 non-required tracked source file을 삭제하고 checksum을 다시 만들면 통과했다. | Expected commit을 주장하는 불완전 handoff pack이 승인될 수 있었다. | `3453b55`: generator/verifier가 tracked selected-path manifest를 공유하고 recursive expected Git file set과 archive file set의 exact equality를 요구한다. JS prover source 삭제 variant가 실패함. |
| S4-21 | Medium | Generated `RELEASE-MANIFEST.txt`는 commit line만 검사했다. | Source identity, contents description, validation instruction을 수정하고 self-checksum할 수 있었다. | `3453b55`: tracked manifest template을 canonical version/commit/time으로 render하고 generated manifest 전체를 byte-for-byte 비교함. |
| S4-22 | Medium | Tar type/duplicate 검사를 extraction 뒤 filesystem에서 수행해 뒤의 regular duplicate가 앞의 symlink entry를 숨길 수 있었다. | Tar reader마다 다른 effective content를 소비하는 archive가 승인될 수 있었다. | `3453b55`: bounded Python `tarfile` 검사로 non-canonical path, duplicate header, link/special entry, multiple root, size/member overflow를 file acceptance 전에 거부함. |
| S4-23 | Medium | `RELEASE_PACK_EXPECTED_COMMIT`이 moving ref와 short SHA를 허용했다. | Branch/tag/`HEAD`가 이동해 immutable out-of-band trust anchor가 아니게 될 수 있었다. | `3453b55`: explicit verify는 canonical lowercase 40-character commit SHA만 허용하고 그 exact local commit을 요구함. |
| S4-24 | Medium | Release verify가 packed Git file의 executable bit만 비교했다. `100644` file을 mode `0400`으로 바꾸고 checksum을 재생성해도 통과했다. | 사용할 수 없거나 예상보다 넓은 permission의 handoff archive가 승인되면서 보고서가 Git mode 일치를 주장할 수 있었다. | `db79ff0`: 모든 tracked file을 Git-derived exact `0644`/`0755` permission mode와 비교하고 generated manifest/checksum은 `0644`를 요구한다. `README.md`를 mode `0400`으로 바꾼 adversarial archive가 거부됨. |
| S4-25 | Medium | Exact mode 비교가 extraction에서 raw tar mode를 `0o777`로 mask한 뒤에만 수행됐다. 따라서 raw `04644`/`04755` header가 `0644`/`0755`로 정규화되어 통과했다. | 다른 extractor가 raw header를 적용하면 setuid/setgid/sticky bit를 verifier에서 숨긴 채 exact-mode handoff 주장을 무효화할 수 있었다. | `7e27721`: raw regular file은 exact `0644`/`0755`, directory는 exact `0755`만 허용하고 모든 parent directory의 canonical explicit member를 요구한다. Special-bit/directory-mode variant를 extraction 전에 거부함. |
| S4-26 | Medium | Release-pack mode가 caller umask를 상속했다. `umask 077`에서는 공식 generator가 `0600`/`0700` member와 `0600` metadata를 만들고 자체 verifier가 거부했다. | Secure CI/operator 환경에서 재현 가능한 공식 handoff pack을 만들 수 없었다. | `7e27721`: tracked file은 Git mode로 재설정하고 directory는 `0755`, generated metadata와 archive/checksum output은 `0644`로 고정한다. `umask 077` 전체 생성과 external verify round trip이 통과함. |
| S4-27 | Medium | Transfer/withdraw/prover-transport/wallet writer가 `os.WriteFile(path, ..., 0600)`을 사용해 기존 `0644` file의 mode를 좁히지 못했다. | Prepared witness, note randomness/path/signature, key, amount, wallet cache가 문서화된 private-file 경계와 달리 다른 local user에게 읽힐 수 있었다. | `d5cef57`: 모든 SDK private JSON 경로가 atomic/durable fresh-inode writer를 공유하고 permissive file/symlink를 exact `0600`으로 교체한다. Direct/race regression을 추가함. |
| S4-28 | Medium | Explicit release verify가 지정한 archive/checksum 중 하나가 없을 때 current-HEAD pack을 재생성했다. | 손상되거나 불완전한 external artifact가 검증 대신 교체되어 out-of-band provenance gate가 무효화될 수 있었다. | `120a2d3`: default non-explicit verify만 pack을 생성한다. Explicit archive/checksum은 둘 다 존재해야 하고 누락 시 fail closed하며 수정되지 않는다. Checksum이 없는 corrupt archive가 byte-identical 상태로 거부됨. |
| S4-29 | Medium | SDK 수정 뒤에도 reference payroll CLI/daemon과 localnet seed output이 direct `os.WriteFile(..., 0600)`을 사용해 기존 `0644` mode를 유지했다. | Employee ID, recipient, amount, note lookup, report, seeded private state가 다른 local user에게 읽힐 수 있었다. | `5ae6140`: fresh-inode atomic writer를 module-level `internal/privatefile`로 옮겨 모든 production `0600` JSON writer를 포함한다. Command/seed direct mode regression과 targeted race가 통과함. |
| S4-30 | Medium | Required 한영 release handoff 일부가 구현된 Session 3B Go SDK/prover/scanner/payroll/CLI surface를 여전히 미완료라고 설명했다. | Downstream 수령자가 code, Completion Record와 같은 pack 내부 설명에 모순되는 contract state로 계획할 수 있었다. | `0d640ac`: 두 문서가 완료된 Go reference와 미완료 external JS/web product delivery, formal setup, production artifact distribution을 분리함. |
| S4-31 | Medium | Transfer/withdraw prover route가 raw decode/validation/prove/response-validation error를 반환했다. Canonical hash를 재계산한 request에서 private `asset_denom` canary가 HTTP body에 노출됐다. | Pass H의 payload-free error 요구와 달리 witness field가 client/proxy/telemetry log에 들어갈 수 있었다. | `fea5968`: 세 route 모두 fixed payload-free error를 사용하고 transfer/withdraw는 strict JSON decode를 적용한다. Nil response도 안전하게 실패하며 decode/validation/prove/response canary test와 race가 통과함. |

Protocol contract, public-input order, NoteV1 변경이 필요한 finding은 없었으므로 Session 2/3A 재진입은 필요하지 않았다. Unresolved Critical/High finding은 0건이고 unresolved security-relevant Medium finding도 0건이다.

## Pass A~I 결과 Matrix

| Pass | 독립 검증 항목 | 결과 |
| --- | --- | --- |
| A — current remediation | 2x2 duplicate input/nullifier, transfer final-effect mutation, withdraw chain/expiry/recipient binding과 leading-zero bytes, disclosure secret blinding, global commitment uniqueness, canonical/subgroup ECIES·EdDSA decode, automatic prover failover 금지, historical-root/artifact-identity mismatch 거부 | Targeted adversarial suite와 full privacy regression 통과 |
| B — NoteV1 | Native, Deposit, Spend, JoinSplit2x2, BatchJoinSplit16x32, scanner, genesis, denom registry, exact empty tree, domain separation, fixed encoding, version/reserved/trailing-byte 거부 | Production helper를 사용하지 않는 frozen KAT 포함 통과 |
| C — batch statement | 순서가 고정된 public input 12개, secret witness, count bound, active prefix, disabled sentinel, vector node type/level, path 16개, root, distinctness, ownership/asset, range/conservation, disclosure, digest limb, owner signature 1개 | Code/normative docs/matrix/SDK/prover/keeper/circuit 일치 |
| D — adversarial witness | Malformed count/slot/root/domain/payload/disclosure, sparse active, nonzero disabled helper, duplicate nullifier/commitment, membership/owner/signature/key mutation, wrap/conservation, vector reorder, digest limb swap, missing/reused/zero blinding | Negative matrix와 mutation/property coverage에서 거부 |
| E — differential/property | Count `1..16`, `1..32`, amount distribution, change/padding, disclosure mode, self-view, root, digest limb, intent, public witness serialization | 통과. Seed와 frozen fixture는 secret이 아니며 재현 가능 |
| F — host/consensus | Proto framing/hard cap, deterministic gas precharge, invalid proof bound, state byte charge, proof-gated atomic write, 2x2+Batch 및 Batch+Batch 양방향 | Cross-message nullifier reuse 전체 rollback 포함 통과 |
| G — event/scan | Minimal event, typed payload 1회 저장, global sequence/cursor, effect ID, limit, pagination/retry/restart, corrupt-state failure, lossy fallback 금지, one-snapshot path, genesis round trip, NoteV1 recomputation, item evidence | Unit/property/size profile/fresh localnet recovery 통과 |
| H — prover/privacy | Lazy VK와 selected R1CS/PK, body/admission limit, 실제 prove 종료까지 permit lifetime, cancel/panic 회수, secret-free log/error, automatic failover 금지, ciphertext policy, 안전한 view-tag 기본값, development artifact label | Transport hardening, strict payload-free error, plaintext log, private-file replacement finding 수정 뒤 통과 |
| I — payroll/reconcile | Atomic many-to-many persistence, 31+change/exact32, role/index/evidence, batch와 item 결과 분리, retry 전 tx/nullifier 조회, explicit re-sign, audit/manual-review metadata | Memory/durable-file/SQL/CLI·daemon private output/live localnet에서 통과 |

## Independent Golden Known-Answer Test

최소 다음 세 KAT 경로는 검증 대상 production helper를 import하지 않고 frozen bytes를 독립 계산한다.

- `TestPrivacyNoteV1ContractIndependentGolden`
- `TestPrivacyBatchJoinSplitV1ContractIndependentGolden`
- `TestCanonicalBatchTransferPayloadV1IndependentGolden`

독립 canonical batch payload는 3,702 bytes이고 SHA-256은 `f2588c7543fb83a7822aa0043e4747af0ac4c9dc14a038c230850f1cab5e24b0`이다. 별도 independent vector-root/effect-ID test가 typed vector domain과 exact effect identity를 검증한다.

## Property와 Fuzz Coverage

신규 bounded fuzz target은 NotePlaintextV1, DisclosurePlaintextV1, transfer/batch canonical payload, active-prefix/disabled sentinel을 포함한 batch vector root, typed scan page/cursor round trip, strict batch prover JSON request를 다룬다. 기존 bounded target은 canonical/subgroup point, EdDSA signature, ECIES envelope decoder를 다룬다. 모든 target은 panic 없음, bounded allocation, accepted canonical round trip, malformed/trailing input fail-closed, error의 secret 비포함을 요구한다.

Seeded circuit property는 randomized output count, amount, disclosure mode, key로 모든 input count `1..16`을 검증한다. SDK와 keeper property는 input `1..16`, output `1..32`를 독립 검증한다. 각 신규 target은 3초 bounded run을 완료했으며 최종 aggregate rerun은 아래 명령 표에 기록한다.

## Development Capacity Profile

환경은 Apple M5 Pro, `darwin/arm64`, Go `1.25.12`, gnark `0.14`이며 측정 시각은 `2026-07-11T19:55:51Z`이다. Development artifact 측정값이며 production SLA가 아니다. Proof shape마다 sample 5개를 사용했다.

공통 artifact profile:

| Metric | 값 |
| --- | ---: |
| Constraints | 1,111,837 |
| Compile | 961.485 ms |
| Development setup | 15,999.531 ms |
| R1CS | 122,813,535 bytes |
| Proving key | 209,218,621 bytes |
| Verifying key | 716 bytes |
| Proof | 164 bytes |
| Peak RSS | 3,429,646,336 bytes |

Artifact SHA-256:

- R1CS: `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`
- PK: `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`
- VK: `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`

| Input/output | Witness p50/p95/max ms | Prove p50/p95/max ms | Verify p50/p95/max ms |
| --- | ---: | ---: | ---: |
| 1/1 | 0.384 / 0.400 / 0.400 | 1,639.739 / 1,674.164 / 1,674.164 | 0.682 / 0.685 / 0.685 |
| 3/4 | 0.380 / 0.417 / 0.417 | 1,654.115 / 1,670.166 / 1,670.166 | 0.671 / 0.672 / 0.672 |
| 8/16 | 0.367 / 0.387 / 0.387 | 1,667.218 / 1,729.603 / 1,729.603 | 0.671 / 0.674 / 0.674 |
| 16/31 | 0.368 / 0.431 / 0.431 | 1,661.212 / 1,695.265 / 1,695.265 | 0.672 / 0.877 / 0.877 |
| 16/32 | 0.370 / 0.411 / 0.411 | 1,694.573 / 1,719.530 / 1,719.530 | 0.670 / 0.679 / 0.679 |

Warm mean prove time은 2x2가 152.349 ms, 16x32가 1,693.102 ms이며 output당 76.174 ms 대 52.909 ms다. Development 측정에서 output당 throughput이 약 1.44배라는 뜻이며 production capacity claim이 아니다.

| Input/output | Keeper gas | Protobuf tx bytes | Typed scan KV bytes | Tree bytes | Total state bytes | Event bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| 1/1 | 1,306,824 | 2,626 | 2,613 | 3,072 | 5,685 | 582 |
| 3/4 | 2,133,464 | 8,709 | 9,647 | 12,288 | 21,935 | 582 |
| 8/16 | 5,333,776 | 32,942 | 37,681 | 49,152 | 86,833 | 583 |
| 16/31 | 9,396,144 | 63,289 | 72,783 | 95,232 | 168,015 | 584 |
| 16/32 | 9,648,080 | 65,294 | 75,105 | 98,304 | 173,409 | 584 |

32-output scanner benchmark는 1,317,912 ns/op, 24,281 outputs/s, 214,851 B/op, 1,393 allocs/op이다.

## Fresh Localnet과 Recovery 결과

Batch rehearsal은 실제 proof/message 5개 case인 1/1, 3/4, 31 payments+change, exact 32 payments, zero-padding을 완료했다. Recipient/auditor/self-view scan, view-tag-safe full decryption, automatic prover failover 금지, tx-hash reconcile, spent-nullifier retry 거부, 실제 prover/node restart, cursor resume, non-zero-height genesis export/import도 수행했다.

Export 전 chain은 height 47, summary 42개, output 138개, global sequence 42, Bob note 68개였다. Import 뒤 scanner와 wallet cache는 missing/duplicate 없이 일치했다. 새 Deposit 뒤 height 49, sequence 43, leaf 139, reserve 2,658로 계속되었고 path, asset registry, reserve state가 보존됐다. Live tx gas used는 1/1 1,609,514, 3/4 3,141,099, 31+change 16,017,355, exact32 16,876,619, padding 15,529,326이었다.

별도 privacy smoke regression은 Deposit, JoinSplit2x2, Withdraw와 expiry/chain-domain authorization negative를 검증했다. Reference payroll live localnet은 reserve, prove, broadcast, reconcile, item evidence, report generation을 검증했다.

## 최종 검증 명령

| 명령 | 결과 |
| --- | --- |
| `go test ./... -count=1` | Cache 없이 통과 |
| Private writer/prover canary targeted test와 race | SDK/payroll/seed output이 permissive file을 `0600`으로 교체하고 proof route가 decode/validation/prove/response canary를 반향하지 않음을 확인 |
| `go test -race ./x/privacy/... -count=1 -timeout=30m` | 통과. 최초 기본-timeout run은 race report 없이 `circuit`/`keeper` 통과 뒤 `zk`에서 timeout됨 |
| bounded fuzz target 10개, 각각 `-fuzztime=3s` | 통과 |
| `go vet ./...` | 통과 |
| `make ci` | 통과 |
| `make vulncheck` | 문서화된 exact policy로 통과, no-fixed-version residual 3건 유지 |
| `make examples` | 통과, example-only npm Low 1건 유지 |
| `make privacy-e2e-smoke` | Release gate 실행을 포함해 fresh state에서 두 번 통과 |
| `make privacy-batch-joinsplit-localnet` | Fresh state에서 restart/genesis continuation 포함 통과 |
| `make reference-payroll-live-localnet` | Fresh state에서 통과 |
| `make release-check` | CI, vulnerability policy, 일반 localnet, privacy E2E, 2-batch bulk readiness 포함 통과 |
| `make release-pack` | Session 4 한영 보고서를 포함해 통과 |
| `make release-pack-verify` | 필수 파일 125개, exact selected Git file set, canonical manifest/tar/checksum list, Git blob/raw·extracted exact permission mode, archive checksum 검증 통과 |
| External archive adversarial variant | immutable archive/checksum input 누락, missing file/directory, modified manifest, duplicate member, moving/short expected commit, mode `0400`/`04644`/`04755` Git file, mode-`0777` directory를 모두 거부함 |
| `umask 077` release generation과 external verify | tracked file, directory, metadata, archive, checksum의 canonical mode를 유지하며 통과 |
| `git diff --check e427370..494c72df2cad38dc1cc97d5e6e0f15b38e0c82d2`과 publication hygiene | 통과. Tracked artifact/secret/personal-path 결과는 없고 생성된 `dist/`/`tmp/`는 ignored 유지 |

## Accepted Residual과 Production TODO

| Residual | Owner | Experimental 공개에서 수용하는 이유 | Production blocking |
| --- | --- | --- | --- |
| External ZK/constraint audit, final source freeze, official MPC/trusted setup, transcript/toxic-waste evidence | Protocol/release owner | Session 4 명시적 비범위이며 development artifact만 사용함 | Yes |
| Artifact reproducibility/signing/provenance/custody, production circuit manifest, SBOM/image provenance | Release/validator operator | Development hash는 기록했지만 production provenance가 아님 | Yes |
| Production gas governance, VK/circuit genesis·upgrade pinning, rollout/rollback, staging load/fault rehearsal, monitoring/incident response | Downstream chain owner | Target chain과 governance 절차가 필요함 | Yes |
| Remote prover TLS/auth/ACL/quota/process isolation, retention policy, capacity/fault rehearsal | Prover operator | Client default는 fail-closed지만 managed infra는 downstream 범위임 | Yes |
| Audit-key HSM/KMS/threshold custody, rotation, decrypt-failure/manual-review 운영 | Auditor/payroll operator | Code는 evidence/manual-review 상태를 기록하며 운영 custody는 외부임 | Yes |
| Downstream JS/TS wallet과 product 구현 | Downstream product owner | Go reference/schema/fixture/handoff가 제공됨 | Downstream production은 Yes, source 공개는 No |
| Public input/output count, batch grouping, timing, policy-dependent metadata leakage | Product/privacy owner | Batch의 inherent/declared metadata이며 padding은 명시적 비용/privacy 결정임 | Production privacy acceptance 필요 |
| In-process development proving peak RSS와 process isolation 부재 | Prover operator | 통제된 experimental 재현에만 적합함 | Yes |
| Repository 정책에서 no-fixed-version인 `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932` | Dependency/security owner | Exact policy에서 현재 fixed version이 없으며 숨기지 않고 추적함 | Production 전 재평가 |
| Examples의 npm low advisory 1건 | Examples/dependency owner | Release policy상 example-only Low임 | Production 전 재평가 |

Accepted residual은 이 구현을 production-ready 또는 audited라고 부를 근거가 아니다.

## Publication Hygiene

최종 gate는 Git tracked path에서 R1CS/PK/VK binary, private key, seed, token, audit secret, 개인 absolute path, scratch benchmark, temporary file을 검사한다. 생성한 development artifact와 `dist/` release pack은 ignored/untracked 상태를 유지한다. 한영 contract, fixture, schema, example, release handoff pack을 함께 검사한다.
