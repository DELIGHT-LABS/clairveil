# Clairveil 릴리즈 버전 정책

이 문서는 Clairveil release tag, changelog, release note, handoff pack 작성 기준을 정의합니다.

Clairveil은 downstream chain이 import/fork할 수 있는 standalone privacy core입니다. 따라서 release note는 단순 변경 목록이 아니라 downstream 팀이 “무엇을 다시 검증해야 하는지”를 판단할 수 있어야 합니다.

## 1. Versioning 원칙

첫 public stable release 전까지는 `v0.x.y`를 사용합니다.

```text
v0.MINOR.PATCH
```

권장 의미는 아래입니다.

| Version  | 의미                                                                                 |
| -------- | ------------------------------------------------------------------------------------ |
| `v0.x.0` | 기능 또는 계약이 의미 있게 추가된 release                                            |
| `v0.x.y` | bug fix, 문서, CI, packaging, fixture 보강 release                                   |
| `v1.0.0` | downstream production integration contract를 안정화했다고 선언하는 첫 stable release |

`v0` 구간에서는 API/fixture/proto/schema가 바뀔 수 있습니다. 다만 변경이 생기면 release note에서 migration impact를 명시해야 합니다.

Release tag는 annotated tag여야 하고 `v` prefix를 붙인 exact SemVer를 사용합니다. 예: `v0.1.1`. Prerelease는 `v0.2.0-rc.1` 같은 SemVer suffix를 사용합니다. 공개한 tag를 이동하거나 재사용하면 안 됩니다. Tag, `CHANGELOG.md`/`CHANGELOG-kr.md` heading, handoff manifest commit, archive, checksum, GitHub release가 모두 같은 immutable source를 식별해야 합니다.

## 2. Breaking change 기준

아래 항목이 바뀌면 release note에서 breaking 또는 migration impact로 표시합니다.

- `proto/clairveil/privacy/v1` message, service, field 변경
- `x/privacy/client/sdk/conformance/testdata` fixture shape 또는 값 변경
- `docs/schemas/clairveil-js-wallet-contract.schema.json` schema 변경
- prover HTTP path, request/response version, error code 변경
- CLI command, flag, JSON output field 변경
- shielded address prefix, transparent prefix, denom, chain-id 기본값 변경
- ZK circuit input shape, artifact manifest, checksum policy 변경
- required circuit-set descriptor/order, VK identity, public-input schema digest 변경
- `MsgBatchTransfer` framing/output 의미, `BatchGasModelV1`, atomic transition, minimal event, typed scan/genesis schema 변경
- disclosure payload version, policy, mode, digest binding 변경
- scan projection version, cursor semantics, empty-page/`has_more` 처리 변경
- transfer view tag 파생 방식, 길이, event field, payload-hash binding 변경

## 3. Release 필수 명령

Release commit과 tag를 만들기 전에 release candidate는 아래를 통과해야 합니다.

```bash
make release-check
```

날짜가 있는 changelog와 release note를 commit한 뒤 그 exact commit에 annotated tag를 만듭니다. 그 다음 tagged commit에서 최종 artifact를 생성하고 검증합니다.

```bash
make release-pack
make release-pack-verify
```

Remote prover image를 release 대상으로 넘기면 아래도 실행합니다.

```bash
make docker-proverd-build
```

## 4. Changelog 작성 기준

`CHANGELOG.md`와 `CHANGELOG-kr.md`의 대응하는 `Unreleased` 항목을 같은 release version으로 이동합니다. Development snapshot을 포함한 repository의 모든 tag는 두 파일 모두에 날짜가 있는 heading을 가져야 합니다.

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

[clairveil-release-note-template-kr.md](clairveil-release-note-template-kr.md)와 [English pair](clairveil-release-note-template.md)만 authoritative release-note template입니다. 이 policy에 두 번째 template를 복사하지 말고 해당 파일을 갱신합니다. 축약된 public note는 비어 있는 detail을 생략할 수 있지만 verification, immutable artifact identity, compatibility impact, known risk, downstream action은 유지해야 합니다.

## 6. Handoff pack naming

공개 가능한 최종 release pack은 clean source commit에 annotated exact-SemVer tag가 있어야 하며 아래 이름을 생성합니다.

```text
dist/clairveil-handoff-<tag>.tar.gz
dist/clairveil-handoff-<tag>.tar.gz.sha256
```

`RELEASE_VERSION`은 packed source commit을 가리키는 기존 annotated exact-SemVer tag만 선택할 수 있습니다. Tag가 없거나 다른 commit을 가리키는 version을 임의로 지정할 수 없습니다.

```bash
RELEASE_VERSION=v0.2.0-rc.1 make release-pack
```

Untagged clean commit에서는 packaging CI와 내부 완비성 검증을 위해 canonical `snapshot-<40-character-commit-sha>` version을 사용합니다. Snapshot pack은 commit-bound이지만 release artifact가 아니며 release로 공개하면 안 됩니다.

## 7. Tag 생성 권장 순서

1. 두 changelog를 같은 dated release version으로 이동하고 paired release-note template를 완성합니다.
2. `make docs-check`, `make release-check`, image 포함 시 `make docker-proverd-build`를 통과합니다.
3. Release commit을 만들고 worktree가 clean인지 확인합니다.
4. 그 commit에 annotated exact SemVer tag 하나를 만듭니다.
5. Tag에서 `make release-pack`을 실행하고 그 same archive를 `make release-pack-verify`로 검증합니다. 기본 verifier는 default archive/checksum pair가 이미 있으면 그대로 사용하고 pair가 없을 때만 생성합니다.
6. Archive manifest commit이 tag target과 같은지 확인하고 external SHA-256을 release note에 기록합니다.
7. 검증 후에만 release commit/tag를 push하고 GitHub release를 만든 뒤 검증한 tarball/checksum을 첨부합니다.
8. 지원 release line이 바뀌면 supported-version/security 정보를 갱신합니다. 공개한 tag를 이동하지 말고 수정은 새 patch release로 냅니다.

예시:

```bash
git tag -a v0.1.0 -m "Clairveil v0.1.0"
make release-pack
make release-pack-verify
```

## 8. Session 3A release baseline

Session 3A chain core를 포함하는 release note는 아래를 한 묶음으로 기록해야 합니다.

- circuit set `privacy-note-v1`, identity schema `v1`, manifest schema `v2`, exact required order `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`;
- batch public-input 순서 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`와 schema SHA-256 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`;
- production proto/API `MsgBatchTransfer`와 `BatchTransferOutput`, gas model `BatchGasModelV1`, sequence `privacy-sequence-v1`, scan schema `privacy-scan-v2`, asset registry `privacy-asset-registry-v1`, privacy state version `2`;
- development artifact identity: constraint `1,111,837`; R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`; PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`; VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`; generation/readiness peak RSS `3,308,797,952 B` / `1,295,482,880 B`;
- `S4-B02` 2x2 relation을 포함하면 JoinSplit constraint `99,775`, schema SHA-256 `4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82`, development R1CS/PK/VK SHA-256 `135528343084d9395ac3b59f87eb32661471751d936424c6aa3bc369483292d4` / `b41790cd96c41b78d7f7ca30f81cb76f4bdb93371bbf0b9437642348306c16d7` / `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`, old proof/job 폐기와 fresh-genesis/reset evidence;
- direct core integration, atomic scan failure, cross-message 2x2+batch/batch+batch rollback 결과.

같은 release note에서 Session 3B experimental reference Go SDK/prover/scanner/payroll/CLI surface와 downstream JS/product 완료를 구분해야 합니다. Formal trusted setup, external audit, production artifact 배포, production 운영은 계속 제외합니다. Development artifact hash는 검증한 binary identity일 뿐 production 배포를 허가하지 않습니다.
