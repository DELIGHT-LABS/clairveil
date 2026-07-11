# Clairveil Reference Payroll Wallet Handoff

English version: [clairveil-reference-payroll-wallet-handoff.md](clairveil-reference-payroll-wallet-handoff.md)

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

Go reference의 `NotePreparationOperationHint`가 제공되면 지갑 또는 admin console은 이를 그대로 준비 작업 후보로 표시할 수 있음. 예를 들어 `make-dummy`는 zero dummy note 준비, `split-merge`는 note 재구성 필요, `add-funds`는 treasury funding 부족, `resolve-reservation-lock`은 기존 reservation 확인 필요를 의미함.

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

## Session 3A Core / Session 3B Wallet Boundary

Wallet은 active set `privacy-note-v1`과 canonical `privacy-fixed-v1` note, disclosure, typed-envelope byte로 migrate해야 합니다. 이 변경은 cache-compatible하지 않습니다. Fresh genesis를 사용하고 old note/reservation/scan/proof state와 development artifact를 삭제한 뒤 재생성하고 full rescan합니다. Raw ciphertext나 legacy JSON note를 compatibility fallback으로 받지 않습니다. Denom label은 authoritative `AssetRegistryV1`으로만 resolve하고 unknown 또는 inconsistent asset ID는 quarantine합니다.

Unified cursor `(height, global_sequence, output_index)` 전체를 atomically 저장하고 선택한 root와 정확히 같은 snapshot에서 spend path를 구합니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical lookup은 treasury timing과 관심 state를 노출하므로 provider 선택 UI에서 이 privacy warning을 유지합니다. Canceled remote proving request가 in-process solver 중단을 보장하지 않으므로 job/chain reconciliation 전까지 관련 reservation을 유지합니다. Automatic prover failover는 계속 비활성화합니다.

Session 3A chain-core 지원을 wallet support로 취급하지 않습니다. 기존 batch-oriented payroll UI는 여전히 현재 2x2 operation을 submit합니다. Session 3B UI/scanner 작업은 production 12개 public input(`MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`)을 사용해야 하며 builder, prover route, decryption, submission, reconciliation flow가 end-to-end로 통과할 때까지 feature-gated 상태를 유지합니다.
