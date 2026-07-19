# 변경 기록

Clairveil의 주요 변경 사항은 이 파일에 기록합니다.

이 프로젝트는 [release versioning policy](docs/clairveil-release-versioning-policy-kr.md)와 [handoff-pack policy](docs/clairveil-release-handoff-pack-kr.md)를 따릅니다.

## Unreleased

### Added

- `msg.Creator` actor attribution을 유지하면서 explicit validated funder를 canonical deposit transition에서 debit하는 trusted in-process `Keeper.DepositWithFunder` integration surface를 추가했습니다.

### Changed

- Public `MsgDeposit` protobuf, gRPC, CLI, client wire format과 actor-as-funder 동작을 그대로 유지하고, downstream caller가 core-local rollback과 outer SDK/EVM rollback boundary를 결합할 수 있도록 deposit mutation을 nested cache에서 실행합니다.

### Security

- Downstream adapter가 authenticated caller에서 actor를 derive하고 fixed escrow만 funder로 사용하며 deposit amount를 EVM `msg.value` 및 runtime native denom과 정확히 bind하고 이후 policy failure를 atomic하게 rollback해야 함을 문서화했습니다.

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
