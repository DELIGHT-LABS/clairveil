# Clairveil 기여 가이드

Clairveil은 reusable Cosmos SDK privacy core, reference daemon, prover service, wallet-facing conformance fixture를 함께 제공하는 저장소입니다.

English version: [CONTRIBUTING.md](CONTRIBUTING.md)

## 개발 baseline

[docs/clairveil-getting-started-kr.md](docs/clairveil-getting-started-kr.md)의 toolchain/resource baseline을 사용합니다. 문서는 변경 중인 같은 checkout을 설명해야 합니다.

Pull request를 열기 전 기본적으로 아래를 실행합니다.

```bash
make docs-check
make ci
make vulncheck
```

`make ci`에 이미 `docs-check`가 포함되지만, 문서-only 작업은 첫 명령으로 더 빨리 feedback을 받을 수 있습니다.

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
- Release process 또는 handoff membership 변경은 release policy와 release-pack manifest 두 개를 함께 갱신해야 합니다.
- security-sensitive 변경으로 trust boundary가 바뀌면 threat model 또는 security review 문서를 함께 갱신해야 합니다.

## 문서

현재 durable knowledge는 `docs/`, 구현 의도와 completion ledger는 `plans/`, duplicate/superseded/local working material은 ignored `tmpdocs/`에 둡니다. [전체 문서 index](docs/README-kr.md)와 [계획 상태 index](plans/README-kr.md)에서 시작하세요.

Downstream 팀이 의존하는 동작을 바꾸면 같은 pull request에서 English/Korean 문서 pair를 함께 업데이트합니다. 새 top-level knowledge document는 양쪽 문서 index에 등록합니다. Runtime `tmp/`에 Markdown을 추가하거나 tracked 문서에서 ignored `tmpdocs/`를 link하면 안 됩니다.

모든 release tag는 `CHANGELOG.md`, `CHANGELOG-kr.md`에 같은 dated heading을 가져야 합니다. Handoff membership이 바뀌면 `scripts/release-pack-paths.txt`, `scripts/release-pack-required-files.txt`를 함께 갱신합니다.

## 라이선스

기여를 제출하면 해당 기여가 Apache License, Version 2.0으로 배포되는 것에 동의한 것으로 간주합니다.
