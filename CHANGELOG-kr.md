# 변경 기록

Clairveil의 주요 변경 사항은 이 파일에 기록합니다.

이 프로젝트는 [release versioning policy](docs/clairveil-release-versioning-policy-kr.md)와 [handoff-pack policy](docs/clairveil-release-handoff-pack-kr.md)를 따릅니다.

## Unreleased

## v0.4.0 - 2026-08-02

### Added

- Language-neutral, versioned `POST /v1/prover/deposit` API와 canonical general/deposit HTTP 문서, 전용 JSON Schema, Go conformance fixture를 추가했습니다.

### Changed

- 모든 proof route에서 제공된 `Content-Type`을 일관되게 검증하되 기존 `v1` client를 위해 누락 호환성을 유지하고, post-validation `proof_failed` response를 HTTP `400`에서 `500`으로 교정했습니다. 해당 failure를 request error로 분류하던 downstream client는 분류를 갱신해야 합니다.
- Deposit API와 common HTTP-policy 교정은 circuit artifact, chain transaction wire contract, success request/response version, `ErrorResponseVersion=v1`을 변경하지 않습니다.
- Architecture, remote prover, JS SDK handoff, client checklist, security review의 current-contract inventory를 네 proof route와 route-specific response binding에 맞게 정렬했습니다.

### Fixed

- 깨끗한 지원 환경에서 `make ci`와 `make release-check`를 차단하던 미선언 Python `jsonschema` 의존성을 제거했습니다. 이제 repository는 필수 Go toolchain으로 language-neutral Draft 2020-12 prover schema와 positive/negative conformance case를 검증합니다.
- Health/readiness route 및 circuit inventory를 configured `ProverSet`에서 산출하여 compatibility constructor가 사용할 수 없는 deposit 또는 batch capability를 광고하지 않게 했습니다.
- Proof request body를 읽기 전에 반복, comma-separated, empty 또는 그 밖의 ambiguous `Content-Encoding` field를 거부하고, 문서의 `make examples` command inventory를 Makefile과 정렬했습니다.

### Security

- Repository `govulncheck` gate의 reachable `GO-2026-6061`, `GO-2026-5158`를 해소하기 위해 `google.golang.org/grpc`를 `v1.82.1`, `go.opentelemetry.io/otel` core module을 `v1.44.0`으로 갱신했습니다.
- Policy-approved finding이 partial output에 있어도 scan/usage failure를 통과시키지 못하도록 JSON `govulncheck` policy wrapper가 모든 nonzero scanner status를 거부하게 강화했습니다.
- Example DApp의 direct `esbuild` development dependency를 `v0.28.1`로 갱신하고 committed browser bundle을 재생성하여 Windows development-server 경로의 `GHSA-g7r4-m6w7-qqqr`을 해소하고 `npm audit` 결과를 zero-finding으로 복원했습니다.

### Known Risk

- Clairveil은 계속 `PUBLICATION_READY_EXPERIMENTAL`이며 `PRODUCTION_RELEASE_READY`가 아닙니다. Formal trusted setup, 외부 ZK/security audit, signed production artifact distribution, chain-specific migration, production wallet storage, audit-key custody, downstream product validation은 이번 release 범위 밖입니다.
- Deposit proof request에는 민감한 witness data가 포함됩니다. Remote deployment는 production profile의 TLS/auth, bounded admission/body limit, timeout, redacted logging, `no-store`, process-isolation 지침을 유지해야 합니다.

### Handoff Notes

- `POST /v1/prover/deposit`을 도입하는 downstream client는 `MsgDeposit`을 조립하기 전에 response version, commitment, proof를 검증하고 encrypted note와 transaction metadata는 prover request 밖에 유지해야 합니다.
- 모든 proof-route client는 제공된 `Content-Type`이 `application/json`이 아니면 unsupported로 처리하고 post-validation `proof_failed`를 HTTP `500` server/proving failure로 분류해야 합니다. 기존 `v1` client의 `Content-Type` 누락은 계속 호환됩니다.
- `v0.3.1` 대비 fresh genesis, circuit artifact rotation, state migration, protobuf regeneration, success-envelope migration은 필요하지 않습니다.

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
