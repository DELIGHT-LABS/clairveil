# Clairveil Maintainer Instructions

이 문서는 Clairveil maintainer가 변경 작업을 할 때 지켜야 할 기준입니다.

## 1. 기본 원칙

- Clairveil은 reusable privacy core와 reference host입니다.
- downstream production app의 운영을 이 repo에 섞지 않습니다.
- downstream-facing contract를 바꾸면 fixture, schema, docs, release note impact를 함께 갱신합니다.
- security/trust boundary가 바뀌면 threat model과 security review 문서를 함께 갱신합니다.

## 2. 변경 유형별 체크리스트

### CLI 변경

수정 대상 예:

- `x/privacy/client/cli`
- `cmd/clairveild`

해야 할 일:

1. CLI test를 갱신합니다.
2. `docs/clairveil-cli-reference-kr.md`를 갱신합니다.
3. 튜토리얼 명령이 바뀌면 `docs/clairveil-local-privacy-walkthrough-kr.md`와 `scripts/privacy-e2e-smoke.sh`를 함께 봅니다.
4. JSON output field가 바뀌면 JS SDK handoff와 schema 영향 여부를 확인합니다.

검증:

```bash
go test ./x/privacy/client/cli
make privacy-e2e-smoke
```

### Proto 변경

수정 대상:

- `proto/clairveil/privacy/v1`
- generated `x/privacy/types/*.pb.go`

해야 할 일:

1. proto를 수정합니다.
2. `make proto`를 실행합니다.
3. keeper/client/schema/test를 갱신합니다.
4. 영향이 있으면 downstream integration, client handoff, JS SDK handoff 문서를 갱신합니다.
5. release note에 breaking 또는 migration impact를 기록합니다.

검증:

```bash
make proto
make ci
```

### 회로 변경

수정 대상:

- `x/privacy/circuit`
- proof builder/verifier
- ZK artifact config

해야 할 일:

1. `docs/clairveil-circuits-kr.md`를 먼저 업데이트합니다.
2. circuit test와 proof builder test를 갱신합니다.
3. artifact filename/checksum/env가 바뀌는지 확인합니다.
4. JS/web wallet fixture와 prover contract impact를 확인합니다.
5. release note의 `ZK artifacts` 항목을 채웁니다.

검증:

```bash
go test ./x/privacy/circuit ./x/privacy/zk
make privacy-e2e-smoke
```

### Fixture/schema 변경

수정 대상:

- `x/privacy/client/sdk/conformance/testdata`
- `docs/schemas/clairveil-js-wallet-contract.schema.json`
- `examples/*`

해야 할 일:

1. fixture 생성/검증 test를 갱신합니다.
2. JSON Schema를 갱신합니다.
3. fixture validator와 prover HTTP client를 포함해 관련 JS 예제를 확인합니다.
4. `docs/clairveil-js-sdk-handoff-kr.md`를 갱신합니다.

검증:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
```

### 운영/보안 변경

수정 대상 예:

- prover service
- artifact preflight
- Merkle restore/capacity
- audit disclosure policy
- release process

해야 할 일:

1. `docs/clairveil-operations-guide-kr.md`를 갱신합니다.
2. trust boundary가 바뀌면 `docs/clairveil-threat-model-kr.md`를 갱신합니다.
3. production gate가 바뀌면 `docs/clairveil-security-best-practices-review-kr.md`를 갱신합니다.
4. Release artifact 구성이 바뀌면 `scripts/release-pack-paths.txt`와 `scripts/release-pack-required-files.txt`를 함께 갱신합니다. Packaging semantic이 바뀔 때만 generator/verifier code를 수정합니다.

검증:

```bash
make ci
```

Commit 후 canonical snapshot 검증에는 `make release-pack-verify`를 사용합니다. 공개 가능한 release는 최종 commit에 annotated exact-SemVer tag를 만든 뒤에만 `make release-pack`과 `make release-pack-verify`를 다시 실행합니다.

## 3. 문서 규칙

- root `README.md`와 directory-level `README.md`는 영문 문서가 기본 파일명을 사용하고, 한글 문서는 `README-kr.md`를 사용합니다.
- Durable current knowledge는 `docs/`, 구현 의도와 gate ledger는 `plans/`에 둡니다. Duplicate, superseded, local working document는 ignored `tmpdocs/`로 이동하고 runtime `tmp/` 아래에는 Markdown을 두지 않습니다.
- 새 top-level knowledge document는 같은 change에서 `docs/<name>.md`, `docs/<name>-kr.md`를 함께 추가하고 `docs/README*.md` 양쪽에 모두 등록합니다.
- Historical plan은 `plans/README*.md`가 상태를 기록하면 Korean-only로 남을 수 있습니다. 모든 tracked top-level plan은 양쪽 plan index에 link해야 합니다.
- 코드와 같은 ref의 문서를 읽고 수정합니다. 이전 tag/binary의 compatibility 판단에 `HEAD` 문서를 사용하면 안 됩니다.
- 문서가 downstream handoff에 필요하면 release pack에 포함합니다.
- 명령 예시는 가능한 한 실제 실행 가능한 형태로 씁니다.
- 어쩔 수 없는 placeholder는 `<...>`로 표시하고, 어디서 값을 가져오는지 바로 설명합니다.
- 튜토리얼 문서는 placeholder를 최소화하고 `keyring-backend test` 기준으로 재현 가능해야 합니다.

## 4. Release pack 포함 기준

아래 문서는 handoff pack에 포함해야 합니다.

- downstream integration에 필요한 문서
- client product/UX/risk/API handoff 문서
- JS/web wallet 구현 계약
- prover 운영 계약
- circuit/proof/artifact 설명
- security/threat/operation 문서
- release/versioning 문서
- schema/fixture/example

새 handoff 문서를 추가하면 아래 두 파일을 같이 수정합니다.

```text
scripts/release-pack-paths.txt
scripts/release-pack-required-files.txt
```

## 5. 권장 검증 순서

작은 문서 변경:

```bash
make docs-check
git diff --check
```

`make release-pack-verify`는 clean committed tree에서 실행하거나 explicit external archive를 대상으로 실행합니다. 기본 경로는 dirty worktree를 거부하고 canonical commit-bound snapshot을 만들 수 있습니다. 최종 annotated exact-SemVer tagged commit에서 검증한 pack만 release입니다.

일반 코드 변경:

```bash
make ci
make vulncheck
```

privacy flow 변경:

```bash
make privacy-e2e-smoke
```

Release commit/tag 전 후보:

```bash
make release-check
```

최종 tagged release:

```bash
make release-pack
make release-pack-verify
```

prover image 변경:

```bash
make docker-proverd-build
```

## 6. 커밋 전 자기 점검

1. `git status --short`로 의도하지 않은 파일이 있는지 확인합니다.
2. 공개 문서에 maintainer-local path가 들어가지 않았는지 확인합니다.
3. CLI/output/schema/proto 변경이 문서에 반영됐는지 확인합니다.
4. `make docs-check`를 실행하고 새 handoff file이 두 release manifest에서 selected/required인지 확인합니다.
5. security-sensitive 변경이면 private key, payload, token이 log에 노출되지 않는지 확인합니다.
