# Clairveil Client Product Brief

이 문서는 Clairveil을 downstream한 chain에서 wallet, SDK, app client를 기획하거나 구현하는 팀을 위한 제품 범위 요약입니다.

English version: [clairveil-client-product-brief.md](clairveil-client-product-brief.md)

이 문서는 모바일, 웹월렛, 데스크톱 월렛, custodial/backend wallet이 공통으로 알아야 하는 Clairveil client 기능 범위를 정리합니다.

## 1. Client Profile

| Profile                  | 주요 사용처                               | 특히 결정해야 할 것                                                          |
| ------------------------ | ----------------------------------------- | ---------------------------------------------------------------------------- |
| Mobile wallet            | iOS/Android self-custody wallet           | secure storage, background scan, remote prover 사용 여부, 메모리/배터리 제약 |
| Web wallet               | browser wallet 또는 dapp embedded wallet  | browser storage, WASM/local/remote proving, session 보안, CORS/auth          |
| Desktop wallet           | desktop self-custody wallet               | local prover daemon, artifact install, local file encryption                 |
| Custodial/backend wallet | exchange, admin wallet, regulated backend | key custody, audit log, role separation, remote prover trust boundary        |

Profile은 다르지만 아래 기능 계약은 공통입니다.

- transparent account에서 shielded identity를 파생합니다.
- `clairs1...` shielded address를 표시하고 복사할 수 있어야 합니다.
- chain event를 scan해서 spendable note를 복구합니다.
- deposit, shielded transfer, withdraw, relayed withdraw를 지원합니다.
- user selective disclosure와 mandatory audit disclosure를 구분해서 처리합니다.
- proof 생성 위치를 browser/WASM, local prover, remote prover, backend sidecar 중 하나로 결정합니다.
- note cache, key material, prepared payload, disclosure plaintext를 privacy-sensitive data로 취급합니다.

## 2. Product Capabilities

### 2.1 Identity And Address

Client는 transparent signer로부터 Clairveil root material을 만들고, spend/view/disclosure key와 shielded address를 파생해야 합니다.

필수 기능:

- transparent account 연결
- root signing message 서명 요청
- full shielded address 표시/copy/share
- disclosure public key 표시 또는 export

주의사항:

- root signing message는 chain tx signing과 다른 domain-separated message입니다.
- root seed, spend key, view key, disclosure key는 로그/analytics/crash report에 남기면 안 됩니다.
- 사용자가 account를 바꾸면 shielded identity와 note cache도 분리해야 합니다.

### 2.2 Chain Configuration

Client는 downstream chain별 runtime config를 받아야 합니다.

필수 config:

- chain-id
- transparent account prefix
- shielded address prefix
- asset denom
- gRPC/REST/RPC endpoint
- prover endpoint와 auth policy
- audit master public key
- circuit artifact/checksum 정보
- disclosure policy/mode 지원 범위

Downstream chain이 denom, prefix, gas policy, prover topology를 바꾸면 client는 hard-coded 값이 아니라 registry 또는 runtime config로 받아야 합니다.

### 2.3 Note Scan And Local State

Client는 privacy event feed를 scan하고 viewing key로 자신의 note를 복구해야 합니다.

필수 기능:

- 최초 sync 진행률 표시
- 마지막 scan height 또는 cursor 저장
- rescan/reset 지원
- nullifier query로 spent 상태 확인
- local cache corruption 또는 decode 실패 시 복구 경로 제공

Local note cache는 금액, asset, ownership metadata를 포함할 수 있으므로 privacy-sensitive local data입니다.

### 2.4 Deposit

Deposit은 transparent asset을 shielded pool로 넣고 encrypted note를 생성하는 흐름입니다.

필수 기능:

- transparent balance와 gas fee 표시
- deposit amount와 denom 확인
- deposit tx broadcast
- tx 성공 후 event scan으로 note 복구 상태 표시

### 2.5 Shielded Transfer

Shielded transfer는 spendable note를 사용해 recipient shielded address로 새 note를 만듭니다.

필수 기능:

- recipient `clairs1...` 주소 validation
- amount/denom 입력
- privacy policy 선택: `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to`
- disclosure mode 선택: `none`, `public`, `recipient-encrypted`
- mandatory audit disclosure가 항상 포함된다는 안내
- proof 생성 진행 상태와 실패 상태 표시

중요한 제품 제약:

- transfer는 user disclosure가 꺼져 있어도 audit disclosure를 항상 포함해야 합니다.
- disclosure plaintext를 보여줄 때는 digest verification 결과가 true인지 확인해야 합니다.
- proof payload는 remote prover에게 민감한 metadata를 노출할 수 있습니다.

### 2.6 Withdraw

Withdraw는 shielded note를 transparent recipient에게 보내는 흐름입니다.

현재 Clairveil withdraw는 exact-match note를 요구합니다.

- `10uclair`를 withdraw하려면 spendable `10uclair` note가 있어야 합니다.
- withdraw는 output note 또는 change note를 만들지 않습니다.
- `MsgWithdraw`에는 output note field가 없습니다.
- exact-match note가 없으면 client는 shielded self-transfer로 원하는 크기의 note를 먼저 만들도록 안내하거나 별도 planner flow로 준비해야 합니다.

필수 기능:

- transparent recipient address validation
- exact-match note 존재 여부 확인
- exact-match note가 없을 때 self-transfer/planner 준비 또는 실패 안내
- direct withdraw와 relayed withdraw 선택
- payload expiry와 recipient 변경 불가 안내

### 2.7 Disclosure Review

Client는 public, recipient-encrypted, sender self-view, audit disclosure payload를 decode하고 검증할 수 있어야 합니다.

필수 기능:

- disclosure source 선택: tx hash, event payload, pasted payload
- disclosure plane 선택: public, recipient, self-view, audit
- decrypt 가능 여부 표시
- digest verification 결과 표시
- verified가 아닌 payload는 사실처럼 표시하지 않는 정책

복호화 성공과 검증 성공은 다릅니다. 복호화에 성공해도 digest verification이 실패하면 신뢰할 수 없는 payload입니다.

## 3. Product Scope Boundaries

Clairveil client 문서는 아래를 제공합니다.

- client 기능 범위
- UX 흐름의 공통 기준
- 보안/운영 결정 항목
- API와 prover 연동 체크리스트
- release gate 기준

Clairveil repo가 제공하지 않는 것:

- downstream 제품별 PRD
- 화면 wireframe
- iOS/Android 구현 세부 설계
- custody/compliance 운영 정책
- remote prover 사업/요금/운영 정책

## 4. Related Documents

- [Client UX flows](clairveil-client-ux-flows-kr.md)
- [Client risk decisions](clairveil-client-risk-decisions-kr.md)
- [Client API checklist](clairveil-client-api-checklist-kr.md)
- [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)
- [CLI reference](clairveil-cli-reference-kr.md)
