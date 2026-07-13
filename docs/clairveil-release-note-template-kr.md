# Clairveil v0.x.y 릴리즈 노트 템플릿

이 템플릿은 Clairveil release tag를 만들 때 사용하는 공개-facing 릴리즈 노트 초안입니다.

English version: [clairveil-release-note-template.md](clairveil-release-note-template.md)

## 1. 요약

- 이번 릴리즈의 핵심 변경:
- downstream project가 바로 알아야 할 변경:
- compatibility impact:

## 2. 검증

Release commit/tag 생성 전:

- [ ] `make docs-check`
- [ ] `make release-check`
- [ ] prover image를 함께 배포한다면 `make docker-proverd-build`
- [ ] 성능 수치를 공개한다면 관련 report마다 `claim_eligible=true`, 동일한 source/artifact identity, evidence hash 기록을 확인

최종 commit에 annotated exact-SemVer tag를 만든 뒤:

- [ ] `make release-pack`
- [ ] `make release-pack-verify`

## 3. Handoff 산출물

- exact tag:
- manifest/source commit:
- handoff tarball:
- external handoff SHA-256:
- GitHub release URL:
- signing/attestation/SBOM: 제공 / 미제공(설명)

## 4. 호환성 영향

| 영역           | 영향        |
| -------------- | ----------- |
| Proto          | 없음 / 있음 |
| Fixture/schema | 없음 / 있음 |
| CLI            | 없음 / 있음 |
| Prover HTTP    | 없음 / 있음 |
| Scan/query contract | 없음 / 있음 |
| Transfer/view tags | 없음 / 있음 |
| ZK artifact    | 없음 / 있음 |
| Circuit set/public witness | 없음 / 있음 |
| Privacy state/genesis migration | 없음 / 있음 |
| Batch gas/scan state | 없음 / 있음 |
| Session 3B SDK/prover/scanner/payroll/CLI surface | 없음 / 있음 |

영향이 있으면 downstream action을 아래 7번에 반드시 적습니다.

## 5. 알려진 위험 / 허용 예외

- `GO-2024-2584`: Cosmos SDK no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.
- `GO-2026-4479`: Cosmos SDK/CometBFT server stack을 통해 reachable한 pion/dtls v2 no-fixed-version advisory. downstream risk register에서 다시 평가해야 합니다.
- `GO-2026-5932`: Cosmos SDK의 local ASCII key armor에서만 좁게 reachable한 no-fixed-version `x/crypto/openpgp` advisory이며 Clairveil은 OpenPGP signing/encryption을 사용하지 않습니다. downstream에서 재평가하고 fixed dependency path가 생기면 예외를 제거해야 합니다.

## 6. 운영 메모

- audit key custody 영향:
- artifact signing/provenance 영향:
- prover deployment 영향:
- wallet storage/telemetry 영향:
- Merkle restore/capacity 영향:
- 이 release가 unsafe로 판명될 때 rollback/yank 대응:

## 7. Downstream Action Required

- Core chain:
- JS/TS SDK:
- Web wallet:
- Prover operations:
- Security/operations:

## 8. 책임 경계

Clairveil 릴리즈는 reusable privacy core와 reference host를 제공합니다. Downstream production project는 custom features integration, audit private key custody, wallet storage encryption, remote prover deployment, artifact signing/provenance를 별도로 소유해야 합니다.

Publication disposition을 명시합니다. `PUBLICATION_READY_EXPERIMENTAL`은 `PRODUCTION_RELEASE_READY`가 아닙니다. 공개 후 수정은 새 patch tag로 내며 기존 release tag를 이동하면 안 됩니다.
