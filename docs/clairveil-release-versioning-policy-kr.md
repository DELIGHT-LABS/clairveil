# Clairveil 릴리즈 버전 정책

이 문서는 Clairveil release tag, changelog, release note, handoff pack 작성 기준을 정의합니다.

Clairveil은 downstream chain이 import/fork할 수 있는 standalone privacy core입니다. 따라서 release note는 단순 변경 목록이 아니라 downstream 팀이 “무엇을 다시 검증해야 하는지”를 판단할 수 있어야 합니다.

## 1. Versioning 원칙

첫 public stable release 전까지는 `v0.x.y`를 사용합니다.

```text
v0.MAJOR.MINOR
```

권장 의미는 아래입니다.

| Version  | 의미                                                                                 |
| -------- | ------------------------------------------------------------------------------------ |
| `v0.x.0` | 기능 또는 계약이 의미 있게 추가된 release                                            |
| `v0.x.y` | bug fix, 문서, CI, packaging, fixture 보강 release                                   |
| `v1.0.0` | downstream production integration contract를 안정화했다고 선언하는 첫 stable release |

`v0` 구간에서는 API/fixture/proto/schema가 바뀔 수 있습니다. 다만 변경이 생기면 release note에서 migration impact를 명시해야 합니다.

## 2. Breaking change 기준

아래 항목이 바뀌면 release note에서 breaking 또는 migration impact로 표시합니다.

- `proto/clairveil/privacy/v1` message, service, field 변경
- `x/privacy/client/sdk/conformance/testdata` fixture shape 또는 값 변경
- `docs/schemas/clairveil-js-wallet-contract.schema.json` schema 변경
- prover HTTP path, request/response version, error code 변경
- CLI command, flag, JSON output field 변경
- shielded address prefix, transparent prefix, denom, chain-id 기본값 변경
- ZK circuit input shape, artifact manifest, checksum policy 변경
- disclosure payload version, policy, mode, digest binding 변경

## 3. Release 전 필수 명령

Release candidate는 아래를 통과해야 합니다.

```bash
make release-check
make release-pack
make release-pack-verify
```

Remote prover image를 release 대상으로 넘기면 아래도 실행합니다.

```bash
make docker-proverd-build
```

## 4. Changelog 작성 기준

`CHANGELOG.md`의 `Unreleased` 항목을 release version으로 이동합니다.

권장 섹션은 아래입니다.

```markdown
## v0.x.y - YYYY-MM-DD

### Added

### Changed

### Fixed

### Security

### Known Risk

### Handoff Notes
```

각 섹션의 의미는 아래입니다.

| Section         | 의미                                                                  |
| --------------- | --------------------------------------------------------------------- |
| `Added`         | 새 기능, 새 fixture, 새 schema, 새 command                            |
| `Changed`       | 기존 계약, UX, packaging, 문서의 의미 있는 변경                       |
| `Fixed`         | bug fix, test regression fix                                          |
| `Security`      | vulnerability scan, dependency update, threat model, custody guidance |
| `Known Risk`    | accepted vulnerability, downstream-owned production risk              |
| `Handoff Notes` | downstream chain/SDK/wallet/prover 팀이 반드시 봐야 하는 작업         |

## 5. Release note template

GitHub release 또는 downstream handoff message는 `docs/clairveil-release-note-template-kr.md`를 사용합니다. 축약해서 직접 작성해야 한다면 아래 구조를 유지합니다.

```markdown
# Clairveil v0.x.y 릴리즈 노트

## 1. 요약

## 2. 검증

- [ ] `make release-check`
- [ ] `make release-pack`
- [ ] `make release-pack-verify`
- [ ] prover image를 함께 배포한다면 `make docker-proverd-build`
- [ ] 성능 수치를 공개한다면 `make privacy-proverd-load-bench`, `make privacy-localnet-tps-bench`, `make privacy-user-latency-bench`, `make privacy-public-capacity-report` 결과의 `claim_eligible=true`와 evidence hash를 확인

## 3. Handoff 산출물

- handoff tarball:
- handoff sha256:
- commit:

## 4. 호환성 영향

- Proto:
- Fixture/schema:
- CLI:
- Prover HTTP:
- ZK artifacts:

## 5. 알려진 위험 / 허용 예외

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.
- `GO-2026-4479`: Cosmos SDK/CometBFT server stack을 통해 reachable한 pion/dtls v2 no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.

## 6. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:
```

## 6. Handoff pack naming

`make release-pack`는 기본적으로 아래 이름을 생성합니다.

```text
dist/clairveil-handoff-<git-describe>.tar.gz
dist/clairveil-handoff-<git-describe>.tar.gz.sha256
```

Release tag가 이미 있으면 `<git-describe>`는 tag 기반입니다. release candidate나 수동 override가 필요하면 아래처럼 실행합니다.

```bash
RELEASE_VERSION=v0.1.0-rc1 make release-pack
```

## 7. Tag 생성 권장 순서

1. `CHANGELOG.md`를 release version으로 업데이트합니다.
2. `make release-check`를 통과시킵니다.
3. 필요한 경우 `make docker-proverd-build`를 통과시킵니다.
4. release commit을 만듭니다.
5. annotated tag를 생성합니다.
6. tag 기준으로 `make release-pack`을 다시 실행합니다.
7. `make release-pack-verify`로 tag 기준 handoff pack을 검증합니다.
8. release note에 tarball checksum과 known risk를 포함합니다.

예시:

```bash
git tag -a v0.1.0 -m "Clairveil v0.1.0"
make release-pack
make release-pack-verify
```
