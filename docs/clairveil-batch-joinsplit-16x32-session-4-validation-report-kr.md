# Batch JoinSplit 16x32 Session 4 독립 검증 보고서

## 상태

| 항목 | 결과 |
| --- | --- |
| 검토 범위 | `e427370..8b4848350f9199439eed48e8adddd6ac4d4749c8` |
| 검증한 implementation HEAD | `8b4848350f9199439eed48e8adddd6ac4d4749c8` |
| 검토 역할 | Session 1~3B 구현에 참여하지 않은 fresh reviewer |
| 진입 시 Gate 3B | 미커밋 integration tree 때문에 최초 차단됨. 아래 수정과 재검증을 완료한 뒤에만 closure 처리함 |
| Session 4 공개 상태 | `PUBLICATION_READY_EXPERIMENTAL` |
| production release 상태 | 승인하지 않음 |
| formal trusted setup | 수행하지 않음 |
| external audit | 수행하지 않음 |

최종 회귀 gate 뒤 기록할 수 있는 `PUBLICATION_READY_EXPERIMENTAL`은 source, test, docs를 experimental 용도로 공개할 수 있다는 뜻이다. production-ready, audited 또는 production proving artifact를 갖췄다는 뜻이 아니다.

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
| S4-13 | Medium | 초기 Completion Record가 mutable `HEAD` range와 `Session 4 closure commit` placeholder를 사용했다. | 공개한 validation claim을 하나의 immutable source snapshot에서 재현할 수 없었다. | 이 record에서 report, Completion Record, master ledger가 검증한 implementation `8b4848350f9199439eed48e8adddd6ac4d4749c8`를 고정한다. 생성한 release manifest는 publication-record commit과 archive checksum을 별도로 정확히 기록한다. |
| S4-14 | Medium | Release pack이 working-tree 파일을 복사하면서 `HEAD`만 기록했고 verify도 같은 dirty content를 재생성했다. | Commit에 없는 uncommitted docs/schema/fixture/source가 검증을 통과할 수 있었다. | `8b48483`: copy 전과 manifest 기록 전에 tracked/untracked non-ignored 상태를 검사한다. Dirty generation/default verify는 fail-closed하고 ignored `dist/`·`tmp/` output은 허용한다. |

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
| H — prover/privacy | Lazy VK와 selected R1CS/PK, body/admission limit, 실제 prove 종료까지 permit lifetime, cancel/panic 회수, secret-free log/error, automatic failover 금지, ciphertext policy, 안전한 view-tag 기본값, development artifact label | Transport hardening과 plaintext log finding 수정 뒤 통과 |
| I — payroll/reconcile | Atomic many-to-many persistence, 31+change/exact32, role/index/evidence, batch와 item 결과 분리, retry 전 tx/nullifier 조회, explicit re-sign, audit/manual-review metadata | Memory/durable-file/SQL/live localnet에서 통과 |

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
| `make release-pack-verify` | 필수 파일 123개와 내부/archive checksum 검증 통과 |
| `git diff --check e427370..8b4848350f9199439eed48e8adddd6ac4d4749c8`와 publication hygiene | 통과. Tracked artifact/secret/personal-path 결과는 없고 생성된 `dist/`/`tmp/`는 ignored 유지 |

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
