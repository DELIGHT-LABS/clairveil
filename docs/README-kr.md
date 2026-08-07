# Clairveil 문서

> English version: [README.md](README.md)

코드와 같은 tag 또는 commit의 문서를 읽습니다. [저장소 개요](../README-kr.md)에서 시작하고 필요한 작업의 가이드만 선택합니다.

## 가이드와 계약

| 독자 작업 | English | 한국어 |
| --- | --- | --- |
| 설치와 privacy/batch 실행 | [Getting started](clairveil-getting-started.md) | [시작 가이드](clairveil-getting-started-kr.md) |
| 구성 요소와 데이터 흐름 | [Architecture](clairveil-architecture.md) | [아키텍처](clairveil-architecture-kr.md) |
| 회로별 증명 범위 | [Circuits](clairveil-circuits.md) | [회로](clairveil-circuits-kr.md) |
| NoteV1과 16x32의 정확한 규격 구현 | [Protocol contract](clairveil-batch-joinsplit-16x32.md) | [프로토콜 계약](clairveil-batch-joinsplit-16x32-kr.md) |
| 명령·flag·출력 조회 | [CLI reference](clairveil-cli-reference.md) | [CLI 참조](clairveil-cli-reference-kr.md) |
| Downstream Cosmos app 연결 | [Cosmos integration](clairveil-downstream-cosmos-integration-guide.md) | [Cosmos 통합](clairveil-downstream-cosmos-integration-guide-kr.md) |
| SDK 또는 wallet 구현 | [JS/TS SDK guide](clairveil-js-sdk-handoff.md) | [JS/TS SDK 가이드](clairveil-js-sdk-handoff-kr.md) |
| Deposit을 포함한 proof route 연동 | [Proverd HTTP API](clairveil-proverd-http-api.md) | [Proverd HTTP API](clairveil-proverd-http-api-kr.md) |
| Node/prover 운영과 상태 복구 | [Operations](clairveil-operations-guide.md) | [운영](clairveil-operations-guide-kr.md) |
| 테스트와 릴리스 검증 선택 | [Testing](clairveil-testing-guide.md) | [테스트](clairveil-testing-guide-kr.md) |
| 신뢰 경계와 잔여 위험 검토 | [Threat model](clairveil-threat-model.md) | [위협 모델](clairveil-threat-model-kr.md) |

## 예제와 기여 문서

- [Reference payroll](../examples/reference-payroll/README-kr.md): one-proof/legacy 경계, durable reservation/retry/reconciliation 계약과 demo.
- [기여 가이드](../CONTRIBUTING-kr.md): 변경 체크리스트, 문서 정책, release versioning/packaging.
- [Schema](schemas/README-kr.md): machine-readable wallet/prover 계약과 검증 명령. Canonical fixture는 [Go SDK conformance testdata](../x/privacy/client/sdk/conformance/testdata)에 있습니다.

## 기준과 유지보수

실행 동작은 compiled proto/message/query 정의와 keeper validation이 기준입니다. 동결된 cross-language encoding은 protocol contract, schema, conformance fixture가 정의합니다. CLI help와 CLI reference가 명령 형식을 정의하며 가이드는 사용법과 운영을 설명합니다.

주제별 원본을 하나로 유지합니다. 로컬 실행은 시작 가이드, HTTP 전송과 deposit 의미는 HTTP API, 복구와 remote prover 배포는 운영 가이드, 예제 계약은 해당 구현 옆, 기여·릴리스 규칙은 `CONTRIBUTING.md`에서 관리합니다. English/Korean pair를 함께 갱신합니다. 기존 가이드에서 명확하게 다루기 어려운 별도의 독자 작업이 있을 때만 독립 문서를 추가합니다.

`docs/`에는 현재 공통 지식을 둡니다. 계획, 날짜가 있는 조사, 완료 기록, 초안은 ignored `tmpdocs/`에, 생성된 runtime 데이터는 `tmp/`에 둡니다. `tmp/`에는 Markdown을 넣지 않고 tracked 문서에서 ignored archive로 링크하지 않습니다.

Release 포함 범위는 `scripts/release-pack-paths.txt`, `scripts/release-pack-required-files.txt`가 정의합니다. 문서 변경 뒤에는 `make docs-check`를 실행합니다.
