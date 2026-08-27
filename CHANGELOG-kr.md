# 변경 기록

Clairveil의 주요 변경 사항은 이 파일에 기록합니다.

이 프로젝트는 [release versioning policy](docs/clairveil-release-versioning-policy-kr.md)와 [handoff-pack policy](docs/clairveil-release-handoff-pack-kr.md)를 따릅니다.

## Unreleased

### Added

- 명시적인 supported-flow 범위, versioned browser chain-profile schema, browser API/lifecycle guide, encrypted storage/recovery contract, browser/prover deployment control로 이루어진 English/Korean WebApp integration pack을 추가했습니다.

### Changed

- 예제 DApp이 `clairveil-web-client-config-v1`을 emit/검증하도록 변경했습니다. Browser flow를 시작하기 전에 server/static flattened compatibility field가 active chain profile과 일치해야 합니다. Legacy top-level EVM account prefix는 host metadata로 남고 ClairveilJS에 전달하는 profile privacy prefix를 override할 수 없습니다.
- ClairveilJS browser-client declaration에 기존 typed, read-only `evmJsonRpc<TResult>` recovery/query method를 노출했습니다.
- 예제 DApp이 circuit, asset, audit/disclosure, tree, reserve, EVM chain 검증용 ClairveilJS browser preflight surface를 사용하도록 변경했습니다. Spendable inventory를 보여주기 전과 지원하는 모든 privacy prepare 직전에 fail closed합니다.
- 예제 DApp이 local development의 sibling checkout과 CI의 exact pinned checkout을
  통해 ClairveilJS 0.3.1을 대상으로 하도록 변경했습니다. Required conformance
  suite는 current browser circuit/asset/tree preflight와 V5/V2 preparation/proof
  contract를 검증합니다.

### Fixed

- CI에서 exact ClairveilJS commit을 sibling dependency path에 checkout하고 local
  development은 sibling checkout을 계속 사용하도록 하여 예제 DApp install의
  재현성을 복원했습니다. Bundle generation에서 developer-specific absolute
  path를 제거했습니다.
- V0.3.1 example의 one-proof batch product flow를 의도적으로 비활성화한 상태로
  유지했습니다. Server는 `serverFeatures.batchTransfer=false`를 반환하고 UI는
  batch submission과 authorized batch-audit surface를 노출하지 않습니다.
- 예제 DApp의 legacy 0.1 browser-data cleanup UI와 구현을 제거했습니다. 아직
  release되지 않은 v0.3.1 client가 current namespace
  fresh initialization과 full typed rescan만 지원하며 legacy lifecycle migration이나
  in-place downgrade contract를 정의하지 않도록 정리했습니다.
- `dapp-local`이 loopback same-origin prover proxy를 활성화하도록 수정했습니다.
  이제 local browser DApp이 browser CORS policy를 의도적으로 제공하지 않는
  reference prover에 cross-origin request를 직접 보내지 않습니다.
- 예제 DApp의 짧은 current chain-configuration preflight lease가 만료되면 이를
  다시 검증하도록 수정했습니다. 이전 successful preflight가 오래되었다는 이유만으로
  cached note가 계속 `Sync unavailable`으로 숨겨지지 않으며, 재검증에 실패하면
  기존처럼 fail closed합니다.
- Browser proof 요청이 설정된 prover URL의 path prefix를 보존하도록 pinned
  ClairveilJS 0.3.1 integration을 갱신해 WebApp profile과 deployment-gate contract에
  맞춴습니다. DApp compatibility fixture는 유효한 current Merkle path와 canonical
  asset ID를 구성합니다.

### Security

- Browser DApp이 모든 Cosmos public/private submission을 canonical chain/account
  기준으로 tab과 동등한 profile 사이에서 직렬화하고, 그 fence 안에서 local genesis
  epoch를 재검증하며, RPC broadcast 경계에 들어가기 전에 정확한 signed Cosmos
  transaction hash를 durable하게 기록합니다. Private submission은 그 hash를
  non-sensitive account marker에도 복제하므로 재접속 후 encrypted reservation store를
  복원하기 전에도 transparent send가 차단됩니다. REST endpoint 변경은 privacy
  session을 무효화하며 scan 또는 account transaction 진행 중에는 차단됩니다.
- Public EVM send/deposit은 `eth_sendTransaction` 전에 hashless wallet-boundary
  attempt를 기록하고 MetaMask가 hash를 반환하면 동기적으로 승격하며, 결과가
  불명확한 attempt는 명시적인 wallet-history 복구 전까지 차단합니다.
- 예제 DApp의 plaintext note cache와 relay recovery metadata를 namespace별 AES-GCM
  localStorage envelope로 교체하고 reservation state는 Web Locks로 보호한 encrypted
  IndexedDB record로 교체했습니다. Privacy setup에는 해당 browser storage, Web
  Crypto, Web Locks가 필요하며 plaintext storage나 memory fallback을 사용하지
  않습니다.
- 예제 static server를 강화했습니다. Loopback이 기본 bind이고 prover proxy는 명시적인 direct-loopback local-test opt-in이 필요하며, public mode는 proxy/token/cleartext configuration을 거부하고 static response에는 CSP, `nosniff`, no-referrer, cross-origin opener header를 보냅니다.

### Migration Notes

- `privacy_note_reservation_contract.json`과 schema를 v1에서 v3으로 올렸습니다. Downstream reservation 구현은 malformed 또는 unavailable nullifier/relay chain-time evidence를 fail-closed로 처리하고, `ProofReady` 전이 동안 lease heartbeat를 유지하며, payload를 외부에 노출하기 전에 lease가 있는 `ProofReady` relay handoff를 durable하게 기록해야 합니다.
- 무제한 `UpdateReservation`/`UpdateOperation` 호출을 Service 소유의 atomic batch, reconciliation, lease-expiry recovery, proof-discard, relay-handoff command로 교체해야 합니다. Persistent Store 구현은 현재 lease owner/token을 함께 검증하고 연결된 reservation/operation 변경을 한 transaction으로 commit해야 합니다.
- Durable reservation lifecycle storage는 JSON snapshot과 SQL metadata 모두 schema v2입니다. 이 workspace는 아직 release 전이므로 lifecycle store migration 또는 rollback 계약이 없으며, store는 현재 schema로 직접 초기화합니다.
- `FoundNote.VerifiedUnspent`는 Go client SDK hardening이며 새로운 consensus boundary나 freshness proof가 아닙니다. Public Go/JSON shape에 `verified_unspent`가 추가되며, 해당 필드가 없는 기존 cache는 false로 decode되므로 planning 전에 성공적인 nullifier 재검증을 거쳐야 합니다. 정상 sync가 cache note를 재검증하며, 폐기되거나 손상된 cache에는 Reset & Rescan을 사용할 수 있습니다. 이 flag는 broadcast 직전 nullifier check를 대체하지 않으며, ClairveilJS/DApp은 이 Go field 자체가 아니라 동등한 fresh-query 동작을 구현해야 합니다.

## v0.3.1 - 2026-07-21

### Fixed

- 이미 공개된 `v0.3.0` tag에 누락된 한영 날짜 changelog heading을 추가하고 release documentation, supported-version reference, immutable release packaging metadata를 완성했습니다. `v0.3.0` 대비 Go, protobuf, runtime, state, circuit, wire-contract 변경은 없습니다.

### Handoff Notes

- 이미 `v0.3.0`을 pin한 downstream codebase는 변경 없이 계속 사용할 수 있습니다. `v0.3.1`은 문서화와 release preparation을 포함한 공개 배포이며 검증된 handoff pack과 GitHub release identity로 사용합니다. `v0.3.0` tag는 이동하거나 재사용하지 않습니다.

## v0.3.0 - 2026-07-21

### Added

- `msg.Creator` actor attribution을 유지하면서 explicit validated funder를 canonical deposit transition에서 debit하는 trusted in-process `Keeper.DepositWithFunder` integration surface를 추가했습니다.

### Changed

- Public `MsgDeposit` protobuf, gRPC, CLI, client wire format, actor-as-funder 동작과 기존 gas path를 그대로 유지하고, downstream caller가 core-local rollback과 outer SDK/EVM rollback boundary를 결합할 수 있도록 deposit mutation을 nested cache에서 실행합니다. Explicit-funder bank transfer 검증을 위한 추가 module-balance read는 trusted entry에만 적용합니다.

### Security

- Downstream adapter가 authenticated caller에서 actor를 derive하고 `privacy` module account와 다른 fixed escrow만 funder로 사용하며 deposit amount를 EVM `msg.value` 및 runtime native denom과 정확히 bind하고 이후 policy failure를 atomic하게 rollback해야 함을 문서화했습니다. Trusted Keeper API는 `privacy` module account funder를 거부하고 bank send 후 module balance 증가량을 정확히 검증해 self-transfer 또는 redirected send로 unbacked shielded deposit이 생성되지 않게 합니다.

## v0.2.0 - 2026-07-13

### Added

- Prepared payload/disclosure plaintext `v5`, proof/prover contract `v2`, chain ID, absolute expiry, final owner intent, canonical decoding, disclosure blinding을 포함한 transfer authorization contract를 추가했습니다.
- Domain-separated NoteV1 primitive, `privacy-fixed-v1`, `AssetRegistryV1`, typed scan/path snapshot, bounded prover admission, consensus-pinned circuit identity를 포함한 `privacy-note-v1`/state-version-2 기반을 추가했습니다.
- Production `BatchJoinSplit16x32` chain core와 `MsgBatchTransfer`를 추가하고, 이어서 Go SDK, `POST /v1/proofs/batch-transfer`, scanner, one-proof payroll, CLI, localnet tutorial을 batch integration용 reference surface로 추가했습니다.
- v0.1.0 reference payroll 기반을 durable file/SQLite/PostgreSQL store, live daemon/reconciliation flow, rehearsal evidence, capacity tooling, public-claim eligibility gate로 확장했습니다.
- English/Korean 시작 가이드, 아키텍처, 문서 index, 계획 상태 문서를 추가하고 누락된 영문 payroll/bulk handoff 문서 세 개를 복원했습니다.
- Release-pack 문서/link/언어/tag/file 검증을 위한 `make docs-check`와 단일 required-file manifest를 추가했습니다.

### Changed

- 현재 호환성 기준을 `privacy-note-v1`, privacy state version 2, `privacy-fixed-v1`로 갱신했습니다. 기존 three-circuit development chain은 fresh genesis/reset이 필요하고 old artifact, proof job, prepared payload, note/reservation/scan cache를 폐기한 뒤 rescan해야 합니다.
- Release-pack membership을 `scripts/release-pack-paths.txt`와 `scripts/release-pack-required-files.txt`로 정의하고, superseded bulk phase-1 plan과 중복 working note를 handoff pack에서 제외했습니다.
- Release packaging에서 공개할 수 없는 full-commit CI snapshot과 공개 가능한 annotated exact-SemVer tag를 구분하고, release tag를 manifest commit 및 paired dated changelog heading에 결속하며, 이미 생성한 default archive를 교체하지 않고 검증하고, tracked release metadata와 post-tag external release note를 분리하도록 바꿨습니다.
- Legacy multi-message `transfer-batch`와 one-proof `transfer-batch-16x32`를 문서에서 구분하고 현재 query/prover surface 전체를 기록했습니다.

### Fixed

- Duplicate input/nullifier inflation, global nullifier/commitment collision, disclosure-blinding separation, structured batch-signing secret reuse, non-canonical BN254 alias, atomic rollback regression을 수정했습니다.
- Prover cancellation/failover와 payroll localnet cleanup 경계를 수정하고 CLI/environment 예제를 실제 flag/export checksum 동작과 맞췄습니다.
- Release identity 불일치, 불완전한 archive/checksum pair, fence/comment 안의 가짜 changelog heading, CommonMark link edge case, 존재하지 않는 fragment, 누락된 path manifest를 거부하도록 문서·handoff 검증을 강화하고 smoke port override와 rehearsal 결정을 문서 계약에 맞췄습니다.

### Security

- Security, protocol, chain-core, client-integration gates와 독립 공개 검증을 완료했고 unresolved Critical, High, security-relevant Medium은 없습니다. 처분은 production 승인이 아니라 `PUBLICATION_READY_EXPERIMENTAL`입니다.
- Consensus verifier identity, role-aware artifact loading, bounded proof verification/admission, secret-free validation error, privacy boundary를 넓히는 multi-prover failover의 explicit opt-in을 강제하고 문서화했습니다.

### Known Risk

- Formal trusted setup, 외부 ZK/security audit, signed production artifact distribution, chain-specific migration, production wallet storage, audit-key custody, downstream product validation은 현재 release state 범위 밖입니다.
- 문서화된 `govulncheck` exception은 downstream production project가 다시 평가해야 합니다. `SECURITY-kr.md`를 참고하세요.

### Handoff Notes

- 통합한 exact tag/commit의 문서를 읽고 `RELEASE-MANIFEST.txt`를 검증해야 합니다. 이전 code/artifact와 `HEAD` 문서를 섞으면 안 됩니다.
- External JS/TS/web product는 frozen Go reference contract를 port하고 storage, prover, scan, disclosure, batch end-to-end gate를 자체 통과해야 합니다.

## v0.1.0 - 2026-07-06

### Added

- Dependency-free Node audit-disclosure-key 예제를 추가했습니다.
- Client product, UX, risk-decision, API integration handoff 문서를 추가했습니다.
- Bounded shielded amount, deposit binding proof, reserve accounting query, 갱신된 ZK artifact contract를 추가했습니다.
- Cursor 기반 `scan_events`, batch nullifier query, transfer view tag와 schema/fixture contract를 추가했습니다.
- Scan optimization, sender self-view disclosure, downstream wallet/relayer용 relayed-withdraw handoff contract를 추가했습니다.
- Benchmark reporting, prover/localnet/user-latency/bulk load runner, public-capacity planning evidence를 추가했습니다.
- Note-reservation/reference payroll planner SDK, proof/multi-message broadcast worker, bounded prover pool, localnet transfer-batch harness, bulk benchmark, bulk-readiness gate를 추가했습니다.

### Changed

- `MsgWithdraw`에서 legacy output-note field를 제거했습니다. Client는 dummy output-note 값 없이 proto binding을 다시 생성해야 합니다.
- Scan cursor 저장, empty-page 전진, safe view-tag mismatch fallback, proto/schema/fixture 재생성 요구사항을 문서화했습니다.

### Fixed

- Canonical prover amount/artifact checksum validation, transfer output bound, reservation ownership/rollback, persisted reconciliation error, bulk-readiness failure handling을 보강했습니다.

## v0.0.0 - 2026-05-19

### Added

- 최초 standalone Clairveil privacy core, reference daemon, prover service, fixture, schema, CI, release handoff 문서를 공개했습니다.
- Apache-2.0 license/notice hygiene, release versioning, release note, handoff-pack verification, restore/security guidance, Korean public documentation을 추가했습니다.
- 기본 local reference chain을 위한 `make install`, `make init`을 추가하고 manual walkthrough와 initialization shortcut을 구분했습니다.
