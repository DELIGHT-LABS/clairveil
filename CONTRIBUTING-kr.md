# Clairveil 기여 가이드

Clairveil은 reusable Cosmos SDK privacy core, reference daemon, prover service, wallet-facing conformance fixture를 함께 제공하는 저장소입니다.

English version: [CONTRIBUTING.md](CONTRIBUTING.md)

## 개발과 검증

Toolchain, 리소스, 로컬 실행 절차는 [시작 가이드](docs/clairveil-getting-started-kr.md)를 사용합니다. 변경 범위는 reusable privacy core와 reference host로 유지하며 downstream production 배포는 해당 프로젝트가 책임집니다.

| 변경 | 필요한 검증 |
| --- | --- |
| 문서만 변경 | `make docs-check`, `git diff --check` |
| 일반 코드 | `make ci`, `make vulncheck` |
| Privacy flow 또는 CLI workflow | `make privacy-e2e-smoke` 추가 |
| Release candidate | `make release-check`와 release가 주장하는 live/capacity 증거 |
| Prover image | `make docker-proverd-build` 추가 |

`make ci`는 문서 검사를 포함합니다. `make release-check`는 local node를 시작하지만 모든 live batch/capacity gate를 실행하지는 않습니다. 추가 검증은 [테스트 가이드](docs/clairveil-testing-guide-kr.md)에서 선택합니다.

## 변경 체크리스트

커밋을 작고 검토 가능한 단위로 유지합니다. Downstream-facing contract, fixture, schema, 양쪽 언어 문서, release 영향을 함께 갱신합니다.

| 변경 영역 | 관련 파일과 후속 작업 | 집중 검증 |
| --- | --- | --- |
| CLI | `x/privacy/client/cli`, `cmd/clairveild`: CLI test/reference, 시작 가이드 명령, `scripts/privacy-e2e-smoke.sh`를 갱신하고 JSON 변경 시 SDK/schema 영향을 확인합니다. | `go test ./x/privacy/client/cli` |
| Proto | `proto/clairveil/privacy/v1`: `make proto`로 `x/privacy/types/*.pb.go`를 재생성하고 keeper/client/schema/test 및 관련 integration/SDK guide를 갱신하며 migration 영향을 기록합니다. | `make proto`, `make ci` |
| Circuit | `x/privacy/circuit`, proof builder/verifier, artifact config: circuit 문서/test, artifact filename/checksum/env, wallet/prover contract를 갱신하고 `ZK artifacts` 영향을 기록합니다. | `go test ./x/privacy/circuit ./x/privacy/zk` |
| Fixture/schema | `x/privacy/client/sdk/conformance/testdata`, `docs/schemas`, `examples`: 생성/검증 test, JSON Schema, SDK guide, 관련 JS consumer를 갱신합니다. | `make examples`, `go test ./x/privacy/client/sdk/conformance` |
| Payroll/control plane | Store, lease, CAS, retry, reconcile, wallet 변경 시 [payroll reference](examples/reference-payroll/README-kr.md)와 SDK guide, fixture/test를 갱신합니다. One-proof와 legacy 경계를 보존하고 gate 변경은 testing/operations 문서에 static, live one-proof, legacy regression, capacity evidence 중 무엇인지 명시합니다. | `go test ./x/privacy/client/sdk/payroll ./x/privacy/client/sdk/reservation ./x/privacy/client/sdk/conformance` |
| 운영/보안 | 운영 가이드를 갱신하고 trust boundary가 바뀌면 threat model을 갱신합니다. Packaging 의미가 바뀔 때만 generator/verifier 코드를 변경합니다. | `make ci` |

커밋 전 `git status --short`로 의도하지 않은 파일을 확인합니다. 공개 문서에 maintainer-local path를 넣지 않고 private key, witness payload, token이 log에 노출되지 않도록 확인합니다.

## 문서

[문서 index](docs/README-kr.md)에서 시작합니다. `docs/`에는 현재 공통 계약과 가이드를, 예제별 설명은 해당 example에, 기여·릴리스 규칙은 이 문서에 둡니다. 새 문서보다 기존 관련 절의 갱신을 우선합니다. 독립 문서는 별도의 독자 작업이 있고 기존 가이드에서 명확하게 관리하기 어려운 내용이 있을 때 추가합니다.

- 기본 파일명은 `README.md`, 한국어는 `README-kr.md`를 사용합니다. English/Korean pair를 함께 갱신하고 모든 top-level knowledge document의 두 버전을 `docs/README*.md` 양쪽에 등록합니다.
- 코드와 같은 ref의 문서를 읽고 수정하며 이전 tag/binary에 `HEAD` 문서를 사용하지 않습니다.
- 계획, completion ledger, 날짜가 있는 조사, superseded 자료, local draft는 ignored `tmpdocs/`에 둡니다. Runtime `tmp/`에 Markdown을 넣거나 tracked 문서에서 ignored archive로 링크하지 않습니다.
- 명령은 실행 가능한 형태로 쓰고 불가피한 placeholder만 `<...>`로 표시하며 값의 출처를 설명합니다. 로컬 튜토리얼은 `keyring-backend test`를 사용합니다.
- Handoff 구성이 바뀌면 `scripts/release-pack-paths.txt`, `scripts/release-pack-required-files.txt`를 함께 갱신합니다. 현재 protocol, integration, CLI, testing, SDK/prover, operations/security, 기여 문서 및 schema, fixture, example을 포함합니다. `make docs-check`로 포함 범위와 링크를 검증합니다.

## Release Versioning Rules

첫 stable release 전에는 `v0.MINOR.PATCH`를 사용합니다. 의미 있는 기능 또는 계약 추가는 `v0.x.0`, fix, 문서, CI, packaging, fixture hardening은 patch version을 올립니다. `v1.0.0`은 downstream production integration contract를 stable로 선언하는 첫 release에만 사용합니다. `v0`에서는 API, fixture, proto, schema가 바뀔 수 있지만 migration impact를 반드시 명시합니다.

Release tag는 `v0.4.1`, `v0.5.0-rc.1`처럼 `v` prefix를 붙인 annotated exact SemVer여야 합니다. 공개한 tag를 이동하거나 재사용하면 안 됩니다. Tag, 양쪽 changelog heading, manifest commit, archive, checksum, public release가 모두 같은 immutable source를 식별해야 합니다.

아래 surface가 바뀌면 breaking 또는 migration impact로 다룹니다.

- proto message/service/field, transaction framing, typed state/query schema
- conformance fixture value/shape 또는 JSON Schema
- prover path, request/response version, error, response binding
- CLI command/flag/JSON output, prefix, denom, chain default
- circuit input, circuit-set order/identity, artifact manifest, VK/schema digest, checksum policy
- disclosure, scan cursor/projection, view tag, gas, atomicity, genesis semantic

각 release는 아래 순서를 따릅니다.

1. 두 changelog를 같은 `## vX.Y.Z - YYYY-MM-DD` heading으로 옮기고 external release note에 compatibility, known risk, downstream action을 기록합니다.
2. `make release-check`를 실행하고 prover image를 배포하면 `make docker-proverd-build`도 실행합니다.
3. Release metadata를 commit하고 clean tree를 확인한 뒤 그 commit에 annotated exact-SemVer tag 하나를 만듭니다.
4. Tagged commit에서 `make release-pack`, `make release-pack-verify`를 실행합니다. 검증 뒤에만 exact commit, archive 이름, external SHA-256을 기록합니다.
5. 모든 identity가 일치한 뒤 commit/tag를 push하고 검증한 archive를 공개합니다. 수정은 tag 이동이 아니라 새 patch release로 냅니다.

Tag가 없는 clean commit은 packaging CI/internal completeness check 전용 `snapshot-<40-character-commit-sha>`를 만듭니다. Snapshot은 release로 공개하면 안 됩니다.

`make release-pack-verify`의 기본 경로는 clean committed tree가 필요하며, 또는 out-of-band identity와 함께 explicit external archive를 검증할 수 있습니다. 문서 수정 중에는 `make docs-check`를 사용합니다. 공개 가능한 release pack은 최종 annotated exact-SemVer tag에서만 생성합니다.

## 라이선스

기여를 제출하면 해당 기여가 Apache License, Version 2.0으로 배포되는 것에 동의한 것으로 간주합니다.
