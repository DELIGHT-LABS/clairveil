# 변경 기록

Clairveil의 주요 변경 사항은 이 파일에 기록합니다.

이 프로젝트는 아래 문서에 정리된 릴리스 정책을 따릅니다.

- `docs/clairveil-release-versioning-policy-kr.md`
- `docs/clairveil-release-handoff-pack-kr.md`

## Unreleased

- standalone Clairveil privacy core, reference daemon, prover service, fixture, schema, CI, release handoff 문서를 추가했습니다.
- Apache-2.0 오픈소스 hygiene 파일을 추가했습니다.
- release versioning 및 release note 정책을 추가했습니다.
- release handoff pack 검증 명령을 추가했습니다.
- release 검증, restore SOP, security reporting, reference app 상태, portable walkthrough path 기준에 맞춰 공개 문서를 업데이트했습니다.
- circuit, CLI, testing, operations, maintainer instructions, release notes, community templates, project README에 대한 한글 공개 문서를 확장했습니다.
- Clairveil binary 설치와 기본 로컬 `~/.clairveil` chain home 준비를 위한 `make install`, `make init` helper를 추가했습니다.
- 빠른 시작과 테스트 문서에서 중복 Make target 순서를 제거하고, manual walkthrough와 `make init` shortcut의 차이를 명확히 했습니다.
- `examples/audit-disclosure-keys` 아래 dependency-free Node audit disclosure key 예제를 추가했습니다.
- `MsgWithdraw`에서 legacy output-note field를 제거했습니다. withdraw는 exact-match로 유지되며 client는 dummy output-note 값 없이 proto binding을 다시 생성해야 합니다.
- wallet/app product planning, UX flow, 보안 결정, API integration을 위한 general client handoff 문서 묶음을 추가했습니다.
