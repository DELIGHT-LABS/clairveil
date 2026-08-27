# Clairveil Release Handoff Pack

이 문서는 Clairveil release를 downstream chain 팀, JS/TS SDK 팀, web wallet 팀, prover 운영 팀에게 넘길 때 확인해야 하는 산출물과 검증 절차를 한 곳에 묶은 handoff pack입니다.

Clairveil repo는 reusable privacy core와 reference host를 제공합니다. 실제 production chain의 EVM, policy module, precompile, validator 운영, audit private key custody, wallet storage encryption, remote prover 노출 정책은 downstream project가 결정하고 책임집니다.

## 1. 릴리즈 수령자가 받아야 하는 산출물

| 구분 | 파일/경로 | 수령자 | 용도 |
| --- | --- | --- | --- |
| Go module | `go.mod`, `x/privacy`, `app`, `cmd/clairveild` | Core chain team | downstream Cosmos SDK app import/fork 기준 |
| Proto | `proto/clairveil/privacy/v1` | Core chain team, JS SDK team | tx/query type generation |
| SDK fixtures | `x/privacy/client/sdk/conformance/testdata` | JS SDK team, web wallet team | wallet/prover/query contract conformance |
| JSON Schema | `docs/schemas/clairveil-js-wallet-contract.schema.json` | JS SDK team, web wallet team | machine-readable fixture shape validation |
| Prover service | `cmd/clairveil-proverd`, `x/privacy/client/sdk/proverservice`, `x/privacy/client/sdk/provertransport` | Prover operations, JS SDK team | local/remote companion prover contract |
| ZK artifact tooling | `cmd/clairveil-setup`, `x/privacy/zk` | Core chain team, prover operations | artifact generation, checksum, preflight |
| Legacy note debug helper | `cmd/clairveil-verify` | Maintainer 전용 | legacy SHA-256/address seed, Base64, raw JSON 호환성 debug 전용이며 현행 protocol contract나 release acceptance 표면이 아님 |
| Walkthrough | `docs/clairveil-local-privacy-walkthrough-kr.md` | Integrators | local end-to-end manual verification |
| Circuit guide | `docs/clairveil-circuits-kr.md` | Core chain team, prover operations, security reviewers | Deposit/Spend/JoinSplit/BatchJoinSplit16x32 회로와 artifact 영향 설명 |
| NoteV1/batch normative contract | `docs/clairveil-batch-joinsplit-16x32.md`, `docs/clairveil-batch-joinsplit-16x32-kr.md` | Core chain, SDK, prover, security teams | batch chain core가 구현한 frozen NoteV1/fixed encoding/vector/public-witness/state contract |
| Batch client integration pack | `docs/clairveil-batch-transfer-integration-handoff-kr.md`, `docs/clairveil-batch-joinsplit-localnet-tutorial-kr.md`, `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json` | Go/JS SDK, wallet, payroll, prover, operations teams | One-proof prepare/prove/broadcast/typed scan/reconcile contract, boundary case, 실행 가능한 localnet handoff |
| Batch protocol independent fixture | `x/privacy/client/sdk/conformance/testdata/privacy_note_v1_contract.json`, `x/privacy/client/sdk/conformance/testdata/privacy_batch_joinsplit_v1_contract.json` | Core chain, SDK, security teams | Independent domain/empty-root/encoding, canonical audit-key ID, vector/public-input, corrected wire-state golden |
| Batch tx/query proto | `proto/clairveil/privacy/v1/tx.proto`, `proto/clairveil/privacy/v1/query.proto`, `proto/clairveil/privacy/v1/genesis.proto` | Core chain, SDK, security teams | production `MsgBatchTransfer`/structured output, AssetRegistryV1, same-root path, typed scan/genesis contract |
| Batch feasibility proto | `proto/clairveil/privacy/v1/batch_feasibility.proto` | Core chain, SDK, security teams | max-shape measurement fixture 전용이며 production contract는 normal tx/query/genesis proto에 존재 |
| CLI reference | `docs/clairveil-cli-reference-kr.md` | Integrators, wallet/SDK teams | 사용자-facing command와 flag 설명 |
| Testing guide | `docs/clairveil-testing-guide-kr.md` | Maintainers, integrators | test matrix와 release 검증 명령 |
| Operations guide | `docs/clairveil-operations-guide-kr.md` | Operators, security reviewers | node/prover/artifact/Merkle/audit 운영 기준 |
| Privacy accounting design note | `docs/clairveil-privacy-accounting-design-note-kr.md` | Core chain team, security reviewers | deposit binding, amount bound, reserve invariant, artifact contract 설계 근거 |
| Maintainer instructions | `docs/clairveil-maintainer-instructions-kr.md` | Maintainers | 변경 유형별 문서/검증 규칙 |
| Integration guide | `docs/clairveil-downstream-cosmos-integration-guide-kr.md` | Core chain team | app wiring and responsibility checklist |
| Client product brief | `docs/clairveil-client-product-brief-kr.md` | Wallet/app product, client 팀 | product capability 범위와 client profile |
| Client UX flows | `docs/clairveil-client-ux-flows-kr.md` | Wallet/app product, client 팀 | setup, scan, transfer, withdraw, disclosure, recovery flow |
| Client risk decisions | `docs/clairveil-client-risk-decisions-kr.md` | Product, security, operations | storage, prover, audit, disclosure, telemetry 결정 |
| Client API checklist | `docs/clairveil-client-api-checklist-kr.md` | Client SDK, app 팀 | chain/prover API, fixture, release gate, compatibility check |
| JS SDK handoff | `docs/clairveil-js-sdk-handoff-kr.md` | JS SDK team, web wallet team | SDK implementation checklist |
| WebApp integration pack | `docs/clairveil-web-app-scope-kr.md`, `docs/clairveil-web-app-integration-kr.md`, `docs/clairveil-web-app-storage-recovery-kr.md`, `docs/clairveil-web-app-deployment-kr.md`, `docs/schemas/clairveil-web-client-config.schema.json` | Browser WebApp, SDK, product, operations 팀 | Supported single-flow 경계, typed config, encrypted persistence/recovery, API lifecycle, deployment control |
| Scan optimization plan | `plans/clairveil-scan-optimization-implementation-plan-kr.md` | Core chain team, JS SDK team, web wallet team | `ScanEvents`, batch nullifier, view tag 설계와 제외된 server-filterable/proof-bound 범위 |
| Reference payroll product | `docs/clairveil-reference-payroll-product-kr.md`, `docs/clairveil-reference-payroll-*.md`, `examples/reference-payroll` | Operators, JS SDK team, wallet teams | payroll control-plane reference product, localnet tutorial, rehearsal 기록, 팀별 handoff note |
| Release policy | `docs/clairveil-release-versioning-policy-kr.md`, `docs/clairveil-release-note-template-kr.md` | Maintainers, release recipients | tag, changelog, release note, compatibility impact 기준 |
| Prover profile | `docs/clairveil-proverd-remote-production-profile-kr.md` | Prover operations | remote prover production controls |
| Merkle restore SOP | `docs/clairveil-merkle-restore-sop-kr.md` | Core chain team, operators | snapshot/restore/migration 후 tree state 검증 |
| Security docs | `docs/clairveil-threat-model-kr.md`, `docs/clairveil-security-best-practices-review-kr.md` | Security reviewers, operators | trust boundary and residual risk review |

## 2. 릴리즈 전 repo maintainer 검증

Release commit과 tag를 만들기 전 maintainer는 아래 명령을 실행합니다.

```bash
make release-check
```

날짜가 있는 paired changelog와 그 밖의 tracked release metadata를 commit한 뒤 그 commit에 annotated exact-SemVer tag를 만듭니다. Paired external release-note draft는 tag 전에 준비하되 exact commit, archive, checksum, GitHub URL field는 tag-bound artifact 검증 뒤에만 채웁니다. 최종 artifact를 생성·검증하는 `make release-pack`, `make release-pack-verify`는 그 tagged commit에서만 실행합니다.

`make release-check`는 아래 순서로 실행됩니다.

```text
make ci
make vulncheck
make localnet-smoke
make privacy-e2e-smoke
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check
```

각 단계의 의미는 아래와 같습니다.

| 단계 | 의미 |
| --- | --- |
| `make ci` | 문서, Go test, Go binary build, JS/TS example을 검증합니다. |
| `make vulncheck` | govulncheck policy gate를 실행합니다. 새 actionable vulnerability가 있으면 실패합니다. |
| `make localnet-smoke` | reference daemon이 genesis부터 init/start 가능한지 확인합니다. |
| `make privacy-e2e-smoke` | deposit, transfer, public disclosure, recipient disclosure, sender self-view disclosure, audit disclosure, direct withdraw, relayed withdraw를 로컬 노드에서 검증합니다. |
| `make privacy-batch-joinsplit-localnet` | 기본 `RUN_LOCALNET=0` batch-integration fixture/conformance gate를 실행합니다. Resource-heavy live node/prover mode는 별도 explicit rehearsal로 남습니다. |
| `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check` | bulk transfer 핵심 unit, reservation invariant, synthetic capacity estimate, multi-message transfer localnet 경로를 검증합니다. |

`make release-check`는 pull request마다 자동으로 돌리기에는 무겁습니다. PR 기본 검증은 `.github/workflows/test.yml`의 `make ci`와 `.github/workflows/security.yml`의 `make vulncheck`가 담당하고, release 후보 검증은 사람이 수동으로 `make release-check`를 실행합니다.

Prover Docker packaging을 검증하려면 아래 명령을 별도로 실행합니다.

```bash
make docker-proverd-build
```

이 명령은 compose config, Dockerfile build, image inspect를 확인합니다. Docker daemon이 필요한 검증이므로 기본 `release-check`에는 포함하지 않습니다.

Final release의 `make release-pack`은 해당 commit을 가리키는 annotated exact-SemVer tag와 clean worktree를 요구하며 `dist/clairveil-handoff-<tag>.tar.gz`와 `.sha256` 파일을 생성합니다. Untagged clean commit에서는 packaging CI와 내부 완비성 검증 전용으로 commit-bound `snapshot-<40-character-commit-sha>` pack을 만들며, 이 snapshot을 release로 공개하면 안 됩니다. 이 pack은 전체 소스 배포본이 아니라 downstream handoff 계약 묶음입니다. Selected path와 exact required file은 `scripts/release-pack-paths.txt`, `scripts/release-pack-required-files.txt`가 정의하며 prose 목록은 membership authority가 아닙니다. Pack에는 license/notice, 현재 문서/계획 index, 주요 handoff/security/operations/circuit/CLI/testing/maintainer 지식, proto, JSON Schema, conformance fixture, non-DApp client example, reference payroll surface, prover Docker sample, release script, `RELEASE-MANIFEST.txt`, `SHA256SUMS.txt`가 들어갑니다. Superseded plan과 ignored `tmpdocs/` archive는 제외합니다. Bilingual batch contract와 independent fixture는 계속 normative입니다. batch chain core에서는 normal tx/query/genesis proto, 네 번째 circuit descriptor, gas/scan/genesis contract, direct core test도 handoff 범위입니다. `batch_feasibility.proto`는 measurement-only로 남습니다. Readiness 명령은 handoff 전에 source checkout에서 실행하고 pack은 large R1CS/PK/VK binary가 아니라 contract artifact와 검증 기대값을 기록합니다.

`make release-pack-verify`는 SemVer manifest version이면 annotated tag의 direct target이 manifest commit이고 두 changelog에 동일한 유효 날짜의 heading이 정확히 하나씩 있는지, untagged CI snapshot이면 version에 exact full commit이 들어 있는지 검증합니다. 이어서 handoff pack의 외부 `.sha256`, canonical complete 내부 `SHA256SUMS.txt`, required-file manifest의 모든 file, exact selected Git file set, canonical manifest, safe canonical tar member, generated file 외 모든 Git blob, raw/extracted Git-derived exact `0644`/`0755` file mode, exact `0755` directory mode를 검증합니다. Generator는 caller umask와 무관하게 이 mode를 canonicalize합니다. `RELEASE_PACK_ARCHIVE`를 지정하지 않으면 clean worktree를 요구하고 기존 default archive/checksum pair를 바꾸지 않고 검증하며 둘 중 하나라도 없을 때만 pair를 생성합니다. 외부 release archive를 검증할 때는 `RELEASE_PACK_ARCHIVE`, `RELEASE_PACK_CHECKSUM`, out-of-band lowercase 40-character SHA인 `RELEASE_PACK_EXPECTED_COMMIT`을 함께 지정하며 exact commit과 release tag가 local clone에 있어야 합니다. Explicit verify는 지정한 두 input을 재생성하거나 교체하지 않으며 archive 또는 checksum이 없으면 즉시 실패합니다. 이 검증은 “tarball이 만들어졌다”가 아니라 “넘겨도 되는 완전한 계약 묶음인지”를 확인하는 단계입니다.

```bash
RELEASE_PACK_ARCHIVE=/path/to/clairveil-handoff.tar.gz \
RELEASE_PACK_CHECKSUM=/path/to/clairveil-handoff.tar.gz.sha256 \
RELEASE_PACK_EXPECTED_COMMIT=<40-character-commit-sha> \
./scripts/release-pack-verify.sh
```

## 3. 릴리즈 maintainer 체크리스트

1. 예정 exact-SemVer version에 맞춰 두 changelog와 tracked release metadata를 갱신하고, authoritative template에서 paired external release-note draft를 준비하되 post-tag field는 비워 둡니다.
2. `make release-check`를 통과시킵니다.
3. remote prover image를 넘기거나 운영할 예정이면 `make docker-proverd-build`를 통과시킵니다.
4. `docs/clairveil-release-handoff-pack-kr.md`의 산출물 목록이 현재 repo 구조와 맞는지 확인합니다.
5. Release metadata를 commit하고 `git status --short`가 비어 있는지 확인합니다.
6. 그 release commit에 annotated exact-SemVer tag를 만듭니다.
7. Tagged commit에서 `make release-pack`을 실행해 최종 handoff tarball과 checksum을 생성합니다.
8. `make release-pack-verify`로 version tag, manifest commit, archive checksum, 내부 checksum, 필수 파일이 모두 일치하는지 확인합니다.
9. `docs/schemas/clairveil-js-wallet-contract.schema.json`이 최신 fixture와 함께 `make examples`에서 검증되는지 확인합니다.
10. `x/privacy/client/sdk/conformance/testdata` fixture가 downstream JS SDK 팀에게 전달될 tagged release commit과 같은 commit인지 확인합니다.
11. ZK artifact checksum과 preflight mode 정책이 release note에 포함되어 있는지 확인합니다.
12. Merkle snapshot/restore/migration 관련 변경이 있으면 `docs/clairveil-merkle-restore-sop-kr.md`의 샘플 path 재계산 절차가 release note에 반영되어 있는지 확인합니다.
13. accepted vulnerability policy exception인 `GO-2024-2584`, `GO-2026-4479`, `GO-2026-5932`가 release note의 known risk에 남아 있는지 확인합니다.
14. downstream project가 audit master private key custody, wallet storage encryption, remote prover topology를 별도 운영 문서로 소유한다는 점을 release note에 명시합니다.
15. Authoritative `docs/clairveil-release-note-template-kr.md`를 사용해 compatibility impact와 downstream action을 작성합니다.
16. `go test ./x/privacy/types -run TestBatchJoinSplit16x32MaxWireStateFeasibilityGate -count=1 -v`를 실행하고 정정된 max-shape golden인 canonical owner-effect payload `65,384` bytes, Tx `65,294` bytes, typed scan KV `75,105` bytes, total KV write `173,409` bytes, query response `74,551` bytes를 확인합니다.
17. Historical feasibility result인 `CLAIRVEIL_RUN_BATCH_FEASIBILITY=1 go test ./x/privacy/circuit -run TestBatchJoinSplit16x32FullShapeResourceGate -count=1 -v`의 constraint `1,111,837`, peak RSS `3,339,862,016` bytes, `55.892 ms/output`, native 2x2 대비 per-output `2.789x` 개선을 보존합니다. 이는 feasibility gate이지 trusted setup이 아닙니다.
18. Release-pack verification이 bilingual batch contract, normal production tx/query/genesis proto, `batch_feasibility.proto`, independent batch protocol contract fixture 2개를 required artifact로 검사하는지 확인합니다.
19. Production circuit/keeper matrix인 `TestBatchJoinSplit16x32ProductionPositiveMatrix`, `TestBatchJoinSplit16x32ProductionNegativeMatrix`, `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`를 실행합니다.
20. Git 밖에서 development artifact를 생성하고 `TestBatchDevelopmentArtifactRoleReadinessGate`를 실행합니다. R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`, generation peak RSS `3,308,797,952 B`, readiness peak RSS `1,295,482,880 B`를 기록합니다. 이 development binary를 production artifact로 package하면 안 됩니다.
21. fixture conformance는 `make privacy-batch-joinsplit-localnet`으로, 실제 1/1, 3/4, 31+change, exact32, padding, restart tx-hash reconcile, 새로 서명된 spent-nullifier fail-closed smoke는 충분한 자원의 host에서 `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`으로 실행합니다. Durable exact-signed-byte retry는 payroll worker/store 테스트 계약으로 별도 검증합니다.
22. release pack이 batch reference integration 한영 handoff/tutorial, conformance fixture, localnet runner를 필수로 검사하고 private prepared payload/proof와 development R1CS/PK/VK binary는 제외하는지 확인합니다.

## 4. Downstream core chain 팀 수령 기준

Core chain 팀은 아래를 확인합니다.

1. `github.com/DELIGHT-LABS/clairveil` module version 또는 fork commit을 고정합니다.
2. `x/privacy` module, keeper, store key, module account permission, tx/query command wiring을 downstream app에 연결합니다.
3. `proto/clairveil/privacy/v1` service path와 generated type이 downstream API gateway와 충돌하지 않는지 확인합니다.
4. downstream denom, chain-id, fee/gas policy를 정하고 tutorial, fixtures, e2e config와 충돌하는 값을 문서화합니다.
5. production-like genesis에는 audit master public key를 설정합니다.
6. ZK artifact preflight는 release candidate와 production-like node에서 `strict`로 운영합니다.
7. downstream EVM, policy module, precompile integration test는 Clairveil repo의 smoke test와 별도로 작성합니다.
8. Active circuit set `privacy-note-v1`의 fresh genesis에서 시작합니다. Old note/tree/scan state와 artifact는 호환되지 않으며 compatibility decoder로 migrate하면 안 됩니다.
9. `AssetRegistryV1`을 authoritative one-to-one denom/32-byte asset-ID mapping으로 취급하고 `(height, global_sequence, output_index)` cursor로 `privacy-scan-v2`를 소비하며 spend path는 선택한 root와 정확히 일치하는 snapshot에서 가져옵니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. 모든 non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. Offline recovery/export는 별도 `MaxMerkleRebuildLeaves`(1,048,576) bound를 유지합니다. Complete per-prefix snapshot metadata index가 있으면 offline bound를 넘는 genesis export도 계속 지원됩니다.
10. Live public-input 순서 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`, schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`, canonical `audit_key_id`, exact `CanonicalBatchTransferPayloadBytesV1` grammar/domain을 보존합니다. `batch-joinsplit-16x32-v1`을 네 번째 required descriptor로 등록하고 three-circuit identity로 조용히 downgrade하지 않습니다.

## 5. JS/TS SDK 및 web wallet 팀 수령 기준

JS/TS SDK와 web wallet 팀은 아래를 확인합니다.

1. `docs/clairveil-js-sdk-handoff-kr.md`를 기준 문서로 사용합니다.
2. `docs/schemas/clairveil-js-wallet-contract.schema.json`으로 fixture shape를 검증합니다.
3. `x/privacy/client/sdk/conformance/testdata` fixture를 SDK CI에 포함합니다.
4. `examples/js-sdk-fixture-validator`의 payload hash 재계산, relay withdraw handoff mapping, route/version 확인, scan fixture check, note reservation status/transition/evidence check, prefix check를 SDK 테스트로 옮깁니다.
5. Release 전 `ScanEvents` cursor sync, empty-page/`has_more` 처리, `CheckNullifiers` batch spent refresh, transfer payload `v5`/owner intent/`view_tag_hexes`, 최종 `MsgTransfer.view_tags`, 안전한 view-tag mismatch fallback을 구현합니다.
6. `examples/js-sdk-prover-http-client`의 timeout, bearer auth, payload hash equality check를 prover adapter 구현에 반영합니다.
7. wallet note cache, root seed derived secret, viewing key, disclosure key, prepared payload/proof JSON은 privacy-sensitive data로 분류하고 plaintext browser storage에 남기지 않습니다.
8. remote prover를 쓰는 경우 prover가 알 수 있는 metadata와 trust boundary를 사용자 UX와 threat model에 반영합니다.
9. `privacy-fixed-v1`을 exact하게 구현합니다. Note plaintext 350 bytes, disclosure plaintext 392 bytes, typed envelope header 20 bytes입니다. Raw ciphertext, legacy JSON plaintext, wrong kind, trailing byte는 compatibility fallback 없이 거부합니다.
10. Prepared transfer payload `v5`는 outer prepared-payload version으로 유지합니다. Note/disclosure encoding version이 아니므로 `privacy-fixed-v1`로 rename하면 안 됩니다.
11. Asset ID는 `AssetRegistryV1`으로 resolve하고 전체 `privacy-scan-v2` cursor를 저장합니다. Wrong event type, fixed-envelope kind, digest, key, sentinel, orphan/non-adjacent output이 있는 typed scan record는 거부합니다. Same-root path snapshot을 사용하고 remote historical-root/path query의 privacy leak과 rebuild cap을 고려합니다.
12. ClairveilJS 0.3.1을 pin하고 `privacy-fixed-v1` fixture와 V5/V2 transfer/withdraw contract를 검증합니다. 아직 release되지 않은 example은 current v0.3.1 namespace의 fresh initialization만 지원합니다. 이전 browser cache/lifecycle store migration과 in-place downgrade contract는 없습니다. Compatibility decoder를 추가하지 말고 empty state에서 full typed rescan을 수행합니다.
13. Batch integration은 public `MsgBatchTransfer` Go SDK, wallet scanner/decrypt path, one-proof payroll workflow, batch CLI/tutorial, ClairveilJS 0.3.1 batch API를 제공합니다. Checked-in WebApp은 의도적으로 `serverFeatures.batchTransfer=false`를 유지하고 one-proof batch submission과 authorized batch-audit UI를 노출하지 않습니다. Downstream product는 이 SDK surface를 활성화하기 전에 별도의 checkpoint, wallet confirmation, reconciliation, E2E release gate를 검토해야 합니다. SDK availability를 product deployment 완료와 혼동하면 안 됩니다.

## 6. Prover 운영 팀 수령 기준

Prover 운영 팀은 아래를 확인합니다.

1. `docs/clairveil-proverd-remote-production-profile-kr.md`를 기준 문서로 사용합니다.
2. remote prover를 public service, private sidecar, local daemon, browser/WASM 중 어떤 topology로 둘지 결정합니다.
3. remote deployment에는 TLS/mTLS, auth, quota, rate limit, body limit, timeout, redacted logging, health/readiness 노출 정책을 둡니다.
4. prover artifact directory는 read-only로 운영하고 checksum mismatch를 release blocker로 취급합니다.
5. proof request/response의 `payload_hash` equality check를 SDK와 server 양쪽에서 유지합니다.
6. Role-aware artifact registry를 사용합니다. Validator는 exact consensus identity 검증 뒤 필요한 VK를 load하고 prover는 선택한 R1CS/PK pair를 lazy load합니다.
7. Circuit별 admission default인 in-flight 1개, queued 4개와 positive 8 MiB body limit을 강제합니다. Zero body limit은 invalid입니다. Automatic prover failover를 비활성화하고 hard cancellation 또는 memory containment가 필요하면 process isolation을 사용합니다.
8. Reference batch prover는 bounded `POST /v1/proofs/batch-transfer`를 노출하고 `batch-joinsplit-16x32-v1`을 advertise합니다. Circuit별 admission, positive body limit, payload binding, TLS/auth, privacy, artifact-role boundary를 보존하고 ad-hoc handler를 mount하지 않습니다.

## 7. Known risk와 accepted exception

현재 release 수령자가 반드시 알아야 하는 known risk는 아래입니다.

| 항목 | 상태 | 수령자 조치 |
| --- | --- | --- |
| `GO-2024-2584` | Cosmos SDK no-fixed-version actionable finding으로 `govulncheck` policy에서 명시 accept | downstream production risk register에서 재평가하고 upstream fixed path가 나오면 dependency alignment를 다시 수행합니다. |
| `GO-2026-4479` | Cosmos SDK/CometBFT server stack을 통해 reachable한 pion/dtls v2 no-fixed-version actionable finding으로 `govulncheck` policy에서 명시 accept | downstream production risk register에서 재평가하고 upstream fixed path가 나오면 dependency alignment를 다시 수행합니다. |
| `GO-2026-5932` | Cosmos SDK가 local ASCII key armor에만 `x/crypto/openpgp/armor`를 사용해 reachable하고 Clairveil은 OpenPGP signing/encryption을 사용하지 않으며 upstream fixed version이 없어 좁게 accept | downstream production risk register에서 재평가하고 fixed dependency path가 생기는 즉시 예외를 제거합니다. |
| Audit master private key custody | Clairveil repo는 public key config와 decode flow만 제공 | downstream project가 HSM/KMS, access control, rotation, incident response를 소유합니다. |
| Wallet local storage | reference CLI는 `0600` plaintext JSON을 사용 | web wallet/production wallet은 encrypted storage와 telemetry redaction을 구현합니다. |
| Remote prover metadata exposure | remote prover는 proof input metadata를 볼 수 있음 | user privacy UX와 deployment threat model에 remote prover를 trusted component로 포함합니다. |
| ZK artifact provenance | repo는 checksum/preflight tooling을 제공하지만 ceremony/release signing 정책은 downstream responsibility | production release에서는 artifact signing, provenance, reproducibility policy를 별도로 둡니다. |
| Batch chain-core/client-integration boundary | Circuit, `MsgBatchTransfer`, keeper, deterministic gas, typed scan/minimal event, genesis, reference Go SDK/prover/scanner/payroll/CLI, ClairveilJS 0.3.1 batch API는 구현됐지만 checked-in WebApp batch product flow, formal setup, production artifact delivery는 없음 | Example batch gate를 false로 유지합니다. Downstream batch WebApp, checkpoint/recovery design, product E2E, production artifact release는 별도 gate로 취급합니다. |
| ClairveilJS 0.3.1 compatibility | Package는 `privacy-fixed-v1`과 V5/V2 prepared-payload/proof contract를 구현하며 아직 release되지 않은 example에는 legacy browser-lifecycle migration 또는 in-place downgrade contract가 없음 | Fresh current cache/reservation/relay namespace에서 full typed rescan하고 compatibility decoder를 추가하지 않습니다. Prepared transfer payload `v5`는 별도 outer version으로 계속 유효합니다. |
| Prover cancellation boundary | Request cancellation은 이미 실행 중인 in-process solver를 preempt하지 못하며 반환할 때까지 permit과 memory를 사용할 수 있음 | Admission을 `1`/`4`, request를 positive `8 MiB`로 제한하고 hard cancellation/OOM containment에는 supervised worker-process isolation을 사용합니다. |
| Historical path rebuild boundary | Current-root path는 incremental node를 사용합니다. Public non-current query는 complete root/count/height metadata를 요구하고 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하며 그 이상은 `ResourceExhausted`를 반환합니다. Offline recovery/export는 `MaxMerkleRebuildLeaves`(1,048,576)를 유지합니다. | Online bound를 넘으면 current root로 spend하거나 trusted local historical-path index를 사용합니다. Large-tree genesis export를 유지하도록 complete snapshot metadata index를 보존합니다. |

## 8. Handoff 완료 기준

Release handoff는 아래를 만족하면 완료로 봅니다.

1. Maintainer가 `make release-check`를 통과시켰습니다.
2. Maintainer가 `make release-pack`과 `make release-pack-verify`를 통과시킨 archive/checksum을 전달했습니다.
3. Core chain 팀이 downstream app import/fork 기준 commit과 module wiring plan을 확정했습니다.
4. JS/TS SDK 팀이 fixture와 JSON Schema를 자기 CI에 가져갔습니다.
5. Web wallet 팀이 wallet storage encryption과 prover topology를 설계 문서에 반영했습니다.
6. Prover 운영 팀이 remote/local prover production profile을 선택했습니다.
7. Security/operations 팀이 accepted vulnerability, audit key custody, ZK artifact provenance를 risk register에 올렸습니다.
8. 모든 팀이 fresh-genesis `privacy-note-v1` / `privacy-fixed-v1` compatibility break를 수용하고 independent batch protocol contract fixture 2개를 검증했으며 `batch-joinsplit-16x32-v1`을 네 번째 required production chain-core circuit으로 기록했습니다.
9. 모든 팀이 Go reference와 ClairveilJS 0.3.1 batch surface는 구현됐지만 checked-in WebApp batch product flow, formal trusted setup, production artifact distribution은 미완료임을 기록했습니다.

이 문서는 release package를 대신하는 압축 파일이 아닙니다. 대신 release commit을 넘겨받는 팀들이 같은 commit, 같은 fixture, 같은 schema, 같은 verification command를 기준으로 통합을 시작하게 만드는 handoff index입니다.
