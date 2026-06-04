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
4. `docs/clairveil-downstream-cosmos-integration-guide-kr.md`와 `docs/clairveil-js-sdk-handoff-kr.md`를 갱신합니다.
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
4. release artifact 구성이 바뀌면 `scripts/release-pack.sh`와 `scripts/release-pack-verify.sh`를 함께 갱신합니다.

검증:

```bash
make ci
make release-pack
make release-pack-verify
```

## 3. 문서 규칙

- root `README.md`와 directory-level `README.md`는 영문 문서가 기본 파일명을 사용하고, 한글 문서는 `README-kr.md`를 사용합니다.
- 새 공개 문서는 가능하면 한국어를 먼저 작성하고 `docs/<name>-kr.md` 형식을 사용합니다.
- 영어 문서는 같은 경로에서 `docs/<name>.md` 형식을 사용합니다.
- 문서가 downstream handoff에 필요하면 release pack에 포함합니다.
- 명령 예시는 가능한 한 실제 실행 가능한 형태로 씁니다.
- 어쩔 수 없는 placeholder는 `<...>`로 표시하고, 어디서 값을 가져오는지 바로 설명합니다.
- 튜토리얼 문서는 placeholder를 최소화하고 `keyring-backend test` 기준으로 재현 가능해야 합니다.

## 4. Release pack 포함 기준

아래 문서는 handoff pack에 포함해야 합니다.

- downstream integration에 필요한 문서
- JS/web wallet 구현 계약
- prover 운영 계약
- circuit/proof/artifact 설명
- security/threat/operation 문서
- release/versioning 문서
- schema/fixture/example

새 handoff 문서를 추가하면 아래 두 파일을 같이 수정합니다.

```text
scripts/release-pack.sh
scripts/release-pack-verify.sh
```

## 5. 권장 검증 순서

작은 문서 변경:

```bash
git diff --check
make release-pack-verify
```

일반 코드 변경:

```bash
make ci
make vulncheck
```

privacy flow 변경:

```bash
make privacy-e2e-smoke
```

release 후보:

```bash
make release-check
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
4. release pack에 들어가야 할 새 파일이 verifier 필수 목록에도 들어갔는지 확인합니다.
5. security-sensitive 변경이면 private key, payload, token이 log에 노출되지 않는지 확인합니다.
