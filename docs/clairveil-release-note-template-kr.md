# Clairveil v0.x.y 릴리즈 노트 템플릿

이 템플릿은 Clairveil release tag를 만들 때 사용하는 공개-facing 릴리즈 노트 초안입니다.

## 1. 요약

- 이번 릴리즈의 핵심 변경:
- downstream project가 바로 알아야 할 변경:
- compatibility impact:

## 2. 검증

- [ ] `make release-check`
- [ ] `make release-pack`
- [ ] `make release-pack-verify`
- [ ] prover image를 함께 배포한다면 `make docker-proverd-build`

## 3. Handoff 산출물

- handoff tarball:
- handoff sha256:
- commit:

## 4. 호환성 영향

| 영역           | 영향        |
| -------------- | ----------- |
| Proto          | 없음 / 있음 |
| Fixture/schema | 없음 / 있음 |
| CLI            | 없음 / 있음 |
| Prover HTTP    | 없음 / 있음 |
| ZK artifact    | 없음 / 있음 |

영향이 있으면 downstream action을 아래 7번에 반드시 적습니다.

## 5. 알려진 위험 / 허용 예외

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.
- `GO-2026-4479`: Cosmos SDK/CometBFT server stack을 통해 reachable한 pion/dtls v2 no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.

## 6. 운영 메모

- audit key custody 영향:
- artifact signing/provenance 영향:
- prover deployment 영향:
- wallet storage/telemetry 영향:
- Merkle restore/capacity 영향:

## 7. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:

## 8. 책임 경계

Clairveil 릴리즈는 reusable privacy core와 reference host를 제공합니다. Downstream production project는 custom features integration, audit private key custody, wallet storage encryption, remote prover deployment, artifact signing/provenance를 별도로 소유해야 합니다.
