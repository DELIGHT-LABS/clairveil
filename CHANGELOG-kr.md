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
- bounded shielded amount, deposit binding proof, reserve accounting query, ZK artifact contract 갱신을 포함한 privacy accounting hardening update를 추가했습니다.
- note scan 최적화 계약을 추가했습니다. 여기에는 cursor 기반 `scan_events`, batch `nullifiers`, `view_tag_hexes`를 포함한 transfer payload `v3`, `MsgTransfer.view_tags`가 포함됩니다.
- scan cursor 저장, empty page 처리, 안전한 view-tag mismatch fallback, proto/schema/fixture 재생성 등 downstream migration 요구사항을 문서화했습니다.
- Transfer authorization contract를 prepared payload/disclosure plaintext `v5`, prover request/response/proof `v2`로 교체하고 chain ID, absolute expiry, final owner intent, canonical signature/point decoding, disclosure blinding을 추가했습니다. Legacy payload는 다시 생성해야 합니다.
- Consensus에 고정되는 circuit-set/verifier identity, manifest-authoritative artifact checksum, proof verification gas bound, global nullifier/commitment collision 검증, privacy boundary를 넓히는 multi-prover failover의 명시적 opt-in을 추가했습니다.
- Session 2 `privacy-note-v1` 기반을 추가했습니다. Domain-separated NoteV1/tree primitive, canonical `privacy-fixed-v1` payload, exact batch owner-effect framing/digest, `AssetRegistryV1`, unified typed scan/path snapshot, role-aware artifact, bounded prover admission, 측정된 16x32 circuit/wire feasibility prototype을 포함합니다. Production batch message/keeper/client integration은 아직 범위 밖입니다.
- Breaking deployment requirement: active set `privacy-note-v1`과 privacy state version `2`는 legacy chain-state migration을 포함하지 않습니다. 기존 개발 체인은 새 consensus `CircuitSetIdentity`를 포함한 fresh genesis를 사용하거나 reset/reinitialize해야 하며 cached proof job, prepared payload, wallet note/reservation/scan cache, 기존 R1CS/PK/VK/manifest artifact를 폐기해야 합니다. 별도 migration을 설계하지 않은 기존 production chain에는 in-place upgrade하지 마십시오.
