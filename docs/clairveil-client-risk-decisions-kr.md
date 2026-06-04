# Clairveil Client Risk Decisions

이 문서는 Clairveil client를 만들 때 제품/보안/운영팀이 반드시 결정해야 하는 risk decision을 정리합니다.

English version: [clairveil-client-risk-decisions.md](clairveil-client-risk-decisions.md)

이 문서는 threat model 전체를 대체하지 않습니다. Downstream project는 실제 chain, wallet, prover topology, custody 정책에 맞는 별도 threat model을 작성해야 합니다.

## 1. Sensitive Data Classification

Client는 아래 데이터를 민감 정보로 분류해야 합니다.

| Data | 위험 | 권장 정책 |
| --- | --- | --- |
| root signer material/root seed | shielded identity 전체 손상 | secure storage, export 제한, logs 금지 |
| spend key | note spend 권한 | hardware-backed storage 가능 여부 검토 |
| view key | note/balance privacy 노출 | encrypted local storage |
| disclosure key | disclosure payload decrypt 권한 | role별 보관 정책 |
| note cache | 금액, asset, ownership metadata 노출 | encrypted DB, rescan/reset 지원 |
| prepared payload/proof | prover metadata 노출 | expiry, deletion, redaction |
| disclosure plaintext/report | private transfer metadata 노출 | verified 표시, 로그 금지 |
| prover bearer token | remote proof API 접근 권한 | secure storage, rotation, no logs |

## 2. Storage Policy By Client Profile

| Profile | 기본 방향 |
| --- | --- |
| Mobile wallet | iOS Keychain, Android Keystore, encrypted local DB, background task 제한을 고려합니다. |
| Web wallet | browser storage encryption, session lifetime, extension/dapp isolation을 정해야 합니다. |
| Desktop wallet | local file permission만으로 충분하지 않으므로 OS keychain 또는 encrypted DB를 검토합니다. |
| Custodial/backend wallet | HSM/KMS, role separation, approval flow, audit log가 필요합니다. |

제품팀이 결정해야 할 질문:

- view key와 note cache를 사용자가 export할 수 있는가?
- crash report에서 privacy payload가 redaction되는가?
- cache reset/rescan 시 사용자에게 어떤 위험과 시간을 안내하는가?
- device 분실 또는 account 복구 시 shielded note 복구 UX가 있는가?

## 3. Prover Topology

| Topology | 장점 | 위험/제약 | 적합한 client |
| --- | --- | --- | --- |
| Browser/WASM prover | metadata 외부 전송 최소화 | 성능, memory, artifact delivery | web, 일부 mobile web |
| Mobile local prover | user privacy 우수 | 앱 크기, CPU, battery, platform 제약 | high-privacy mobile |
| Desktop local daemon | 성능과 privacy 균형 | daemon lifecycle, local auth | desktop wallet |
| Private remote sidecar | 운영 통제 가능 | operator가 payload metadata를 봄 | company wallet/backend |
| Public remote prover | UX 단순화 | auth, DoS, metadata exposure | consumer wallet with trust disclosure |

Client는 아래 정책을 명시해야 합니다.

- prover request timeout
- retry와 cancellation
- auth token 보관
- request/response body logging 금지
- remote prover data retention
- 사용자가 remote prover를 신뢰해야 한다는 안내 문구

## 4. Audit Disclosure

모든 shielded transfer에는 chain-configured audit master pubkey를 대상으로 하는 audit disclosure가 포함됩니다.

Client가 해야 할 일:

- audit config query로 현재 audit master pubkey를 확인합니다.
- transfer payload가 audit target pubkey와 digest를 포함하는지 확인합니다.
- auditor UX가 필요한 제품은 audit disclosure decrypt와 verification report를 제공합니다.

Auditor private key custody는 downstream 운영 책임입니다. Production에서는 HSM/KMS, role separation, access approval, key rotation, incident response를 정해야 합니다.

## 5. Disclosure Verification Policy

복호화 성공과 검증 성공은 다릅니다.

Client 정책:

- `verification.verified=true`가 아닌 disclosure plaintext는 사실처럼 표시하지 않습니다.
- public disclosure와 recipient/audit disclosure를 구분합니다.
- digest mismatch는 warning 수준이 아니라 trust failure로 취급합니다.
- disclosure plaintext와 report는 logs/analytics/crash report에 남기지 않습니다.

## 6. Logging And Telemetry

Client와 prover는 아래 값을 기록하면 안 됩니다.

- root seed/root signer material
- spend/view/disclosure private key
- note amount/randomness/nullifier
- Merkle path
- prepared payload body
- proof request/response body
- disclosure plaintext
- bearer token

필요하면 tx hash, height, high-level error code처럼 민감도가 낮은 식별자만 남깁니다.

## 7. Product Risk Decisions

Release 전에 제품/보안/운영팀이 최소 아래 질문에 답해야 합니다.

- 사용자는 remote prover를 신뢰해야 하는가?
- remote prover 장애 시 local fallback이 있는가?
- exact-match withdraw 실패를 제품이 어떻게 안내하는가?
- note scan 실패 시 balance를 어떻게 표시하는가?
- disclosure verification 실패 시 화면은 무엇을 숨기거나 경고하는가?
- auditor key custody와 access approval은 누가 책임지는가?
- prepared payload/proof JSON은 어디에 저장되고 언제 삭제되는가?
- mobile/web/desktop profile별 secure storage 기준이 다른가?

## 8. Related Documents

- [Threat model](clairveil-threat-model-kr.md)
- [Security best practices review](clairveil-security-best-practices-review-kr.md)
- [Remote prover production profile](clairveil-proverd-remote-production-profile-kr.md)
- [Client product brief](clairveil-client-product-brief-kr.md)
- [Client UX flows](clairveil-client-ux-flows-kr.md)
