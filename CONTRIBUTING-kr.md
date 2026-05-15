# Clairveil 기여 가이드

Clairveil은 reusable Cosmos SDK privacy core, reference daemon, prover service, wallet-facing conformance fixture를 함께 제공하는 저장소입니다.

## 개발 baseline

Pull request를 열기 전 기본적으로 아래를 실행합니다.

```bash
make ci
make vulncheck
```

Release candidate 또는 release-critical 변경은 더 무거운 local chain gate까지 실행합니다.

```bash
make release-check
```

`make release-check`는 local node를 띄우고 full privacy smoke test를 수행하므로 기본 CI 경로보다 의도적으로 느립니다.

## 커밋 범위

커밋은 작고 review 가능한 단위로 유지합니다.

- module/runtime 변경은 test를 포함해야 합니다.
- CLI 또는 workflow 변경은 문서를 함께 갱신해야 합니다.
- wallet-facing fixture 변경은 JSON Schema와 예제를 함께 갱신해야 합니다.
- release process 변경은 release handoff pack을 함께 갱신해야 합니다.
- security-sensitive 변경으로 trust boundary가 바뀌면 threat model 또는 security review 문서를 함께 갱신해야 합니다.

## 문서

중요한 integration 문서는 `docs/` 아래에 있습니다.

- `docs/clairveil-downstream-cosmos-integration-guide-kr.md`
- `docs/clairveil-js-sdk-handoff-kr.md`
- `docs/clairveil-circuits-kr.md`
- `docs/clairveil-cli-reference-kr.md`
- `docs/clairveil-testing-guide-kr.md`
- `docs/clairveil-operations-guide-kr.md`
- `docs/clairveil-maintainer-instructions-kr.md`
- `docs/clairveil-release-handoff-pack-kr.md`
- `docs/clairveil-proverd-remote-production-profile-kr.md`
- `docs/clairveil-threat-model-kr.md`
- `docs/clairveil-security-best-practices-review-kr.md`

Downstream 팀이 의존하는 동작을 바꾸면 같은 pull request에서 관련 문서를 함께 업데이트합니다.

## 라이선스

기여를 제출하면 해당 기여가 Apache License, Version 2.0으로 배포되는 것에 동의한 것으로 간주합니다.
