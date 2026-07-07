# Clairveil Reference Payroll Wallet Handoff

## 목적

이 문서는 웹/모바일 지갑팀이 Reference Payroll Product와 연동하기 위해 구현해야 할 작업을 정리함.

지갑팀의 핵심 역할은 사용자의 note와 disclosure key를 안전하게 관리하고, payroll/reservation 상태가 일반 transfer UX와 충돌하지 않게 만드는 것임.

## 지갑팀 담당 범위

지갑팀은 다음을 구현해야 함.

```text
reserved note handling
wallet DB migration
batch nullifier sync
disclosure pubkey generation/display/export
payroll incoming note display
note preparation visibility
rescan/recovery flow
privacy-safe logging
```

## Reserved Note 처리

가장 중요한 요구사항은 reserved note를 일반 송금 후보에서 제외하는 것임.

필수 정책:

- `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `ManualReview` note는 일반 transfer/split/merge 후보에서 제외함.
- `ConfirmedSpent` note는 spent로 표시함.
- `Released` note는 backend/reconcile 확인 후 available로 되돌릴 수 있음.
- `Unknown`, `ManualReview` note는 사용자가 임의로 unlock하지 못하게 함.

지갑이 reserved note를 무시하면 payroll backend와 wallet이 같은 note를 동시에 사용해 nullifier conflict가 발생할 수 있음.

## Wallet DB Migration

wallet DB에는 최소 다음 필드 또는 동등한 projection이 필요함.

```text
commitment_hex
nullifier_hex
nullifier_lookup_key
nullifier_lookup_key_id
amount
denom
spent
reservation_id
reservation_status
operation_id
payroll_id
batch_id
last_scan_height
last_scan_sequence
tx_hash
```

민감정보는 가능한 암호화 저장하고, nullifier/commitment/recipient/amount 원문을 로그로 남기지 않아야 함.

## Disclosure Public Key UX

Payroll product에서 recipient-encrypted user disclosure를 쓰려면 disclosure public key UX가 필요함.

필수 기능:

- disclosure public key 표시
- disclosure public key 복사/export
- key rotation 또는 재생성 정책 표시
- backup/recovery 안내
- 잘못된 network/account key를 제출하지 않도록 검증

On-chain event에는 sender self-view target pubkey를 노출하지 않는 것이 원칙임. 지갑도 static disclosure pubkey를 analytics나 일반 event log로 보내면 안 됨.

## Payroll Incoming Note 표시

지갑은 payroll로 받은 note를 일반 incoming shielded note처럼 scan해야 함.

권장 UX:

- payroll payment로 식별 가능한 metadata가 backend에서 제공되면 별도 label 표시
- chain event만으로 식별이 어려우면 일반 received note로 표시
- amount disclosure가 없으면 amount는 local decrypted note 기준으로만 표시
- disclosure payload 검증 실패 시 경고 표시

## Batch Nullifier Sync

지갑은 spent 상태 갱신에 batch nullifier query를 사용해야 함.

```text
POST /clairveil/privacy/v1/nullifiers
```

요구사항:

- 요청당 1000개 이하로 chunk
- 응답 누락 nullifier는 safe failure로 처리
- scan cursor rollback/reorg 대응 유지
- forced rescan UX 유지

## Note Preparation 표시

Reference payroll product가 note preparation report를 제공하면 지갑은 사용자 또는 운영자에게 준비 상태를 표시할 수 있음.

표시 후보:

- spendable note 부족
- dummy note 부족
- reserved note 때문에 payroll 실행 불가
- split/merge 준비 필요
- payroll run 가능 여부

최종 제품에서 이 화면을 wallet에 둘지 admin console에 둘지는 제품팀 결정 사항임.

## Privacy-Safe Logging

지갑은 다음 값을 일반 log, analytics, crash report로 전송하면 안 됨.

- raw nullifier
- raw commitment
- recipient address와 amount mapping
- payroll item과 employee mapping
- disclosure payload 원문
- disclosure private key
- viewing key
- root seed

UI에서는 nullifier/commitment를 표시해야 할 때 축약값만 사용함.

## 완료 기준

- reserved note가 일반 transfer/split/merge 후보에서 제외됨.
- disclosure public key를 안전하게 표시/export할 수 있음.
- payroll incoming note가 scan/display됨.
- batch nullifier sync가 동작함.
- forced rescan 후 reservation/spent 상태가 일관됨.
- 민감정보 logging 금지 정책이 적용됨.
