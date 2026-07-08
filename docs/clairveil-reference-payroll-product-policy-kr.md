# Clairveil Reference Payroll 제품 정책 기본값

English version: [clairveil-reference-payroll-product-policy.md](clairveil-reference-payroll-product-policy.md)

## 목적

이 문서는 Reference Payroll Product를 제품화할 때 repo 기준으로 먼저 고정해둘 기본 정책을 정리함.

정책은 downstream 제품이 반드시 그대로 써야 하는 법적/사업적 결정이 아니라, 구현자가 혼동하지 않도록 하는 안전한 기본값임. 각 제품은 tenant, compliance, UX 요구에 맞게 override할 수 있으나, 아래 원칙을 깨면 note 중복 사용, 오지급 성공 오판, 민감정보 노출 위험이 커짐.

## Disclosure 기본 정책

기본 user disclosure 정책은 `all-private` / `none`으로 둠.

이 기본값은 사용자의 선택 공개를 하지 않는다는 의미임. mandatory audit disclosure를 끄는 의미가 아님. payroll/payment 성공 판정의 기본 digest는 audit disclosure digest를 사용함.

`transfer-batch` CLI는 현재 일반 `transfer`와 같은 shared disclosure flag를 지원함.

```text
--privacy-policy all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to
--disclosure-mode none|public|recipient-encrypted
--disclosure-pubkey <hex>
--no-self-view
```

Reference Payroll Product는 payroll input, plan item, operation record에서 아래 값을 보존해야 함.

```text
user_privacy_policy
user_disclosure_mode
user_disclosure_target_pubkey_hex
user_disclosure_target_key_id
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
```

`all-private`인 item에는 `expected_user_disclosure_digest`를 저장하지 않음. user disclosure가 없기 때문임. 반면 audit disclosure digest는 별도 expected/evidence 필드로 저장할 수 있음.

## User Disclosure 선택 정책

제품 기본값은 `all-private` / `none`으로 두되, payroll 제품은 다른 user disclosure option도 막지 않아야 함.

허용하는 정책은 현재 transfer 정책과 동일함.

| 정책 | 의미 |
| --- | --- |
| `all-private` | user-facing disclosure 없음 |
| `amount` | 금액 공개 |
| `to` | 수신자 공개 |
| `amount-to` | 금액과 수신자 공개 |
| `from` | 송신자 공개 |
| `amount-from` | 금액과 송신자 공개 |
| `from-to` | 송신자와 수신자 공개 |
| `amount-from-to` | 금액, 송신자, 수신자 공개 |

user disclosure mode는 `none`, `public`, `recipient-encrypted`를 지원함.

권장 기본값은 다음과 같음.

- 일반 payroll: `all-private` / `none`
- 감사/정산용 공개: user disclosure가 아니라 audit disclosure digest와 별도 audit key 경로 사용
- 직원 또는 외부 수신자에게 선택 공개가 필요한 경우: `recipient-encrypted`
- 테스트, 내부 검증, 명시적 공개 지급: `public`

`recipient-encrypted`를 사용하려면 disclosure public key registry가 필요함. key id/version을 operation expected value와 함께 저장하고, key rotation을 고려해야 함.

## 성공 판정 원칙

`nullifier_spent=true`만으로 payroll/payment 성공으로 처리하지 않음.

nullifier spent는 해당 note가 소비되었다는 뜻임. 같은 note가 다른 충돌 tx에서 먼저 소비되었을 수도 있으므로, operation 성공은 아래 evidence가 현재 operation의 expected value와 일치할 때만 인정함.

```text
tx_hash 또는 tx identity
output_commitment
audit_disclosure_digest
recipient_hash
amount_hash
denom
batch_item_index
optional user_disclosure_digest
optional self_view_disclosure_digest
```

audit disclosure digest는 operation 성공 판정의 primary disclosure evidence임. user/self-view disclosure digest는 해당 expected field가 있을 때 별도로 확인함.

성공 판정에서 audit disclosure digest를 비교하는 작업 자체에는 audit private key가 필요하지 않음. audit private key는 disclosure payload를 복호화해 내용을 감사할 때 필요하고, reconcile worker는 expected digest와 tx/event evidence digest의 일치 여부를 확인함.

일치 증거가 부족한데 nullifier만 spent이면 note 상태는 `ConfirmedSpent`로 볼 수 있어도 operation은 `Succeeded`로 처리하지 않음. 해당 operation은 `ManualReview` 또는 `ConflictSpent`로 보내야 함.

## Reconcile / Retry 정책

`Submitted`, `Unknown`, `ManualReview` 상태는 TTL만으로 available 처리하지 않음.

기본 reconcile 순서는 다음과 같음.

1. `tx_hash`가 있으면 tx를 조회함.
2. tx 성공이면 tx event, output commitment, audit disclosure digest, amount/recipient evidence를 대조함.
3. tx 실패이면 실패 원인을 분류함.
4. tx가 없거나 불명확하면 nullifier spent 여부를 조회함.
5. nullifier가 unspent이고 tx도 없으면 동일 operation retry 또는 tx 재구성을 검토함.
6. nullifier가 spent인데 operation evidence가 불충분하면 `ManualReview` 또는 `ConflictSpent`로 보냄.

RPC timeout 또는 mempool eviction은 즉시 새 tx 생성으로 처리하지 않음. 먼저 기존 `tx_hash`, signed tx bytes, nullifier 상태를 확인함.

retry는 `operation_id` 기준으로 idempotent해야 함. 같은 operation의 tx bytes, tx hash, sign doc hash, account sequence, broadcast attempt count를 저장해야 함.

## Note Preparation 정책

기본 note preparation 정책은 auto-prepare가 아니라 approval 기반으로 둠.

Reference product는 `AnalyzeNotePreparation`으로 필요한 dummy note, split/merge, add-funds, reservation lock 해소 hint를 제공함. 실제 split/merge tx 실행은 operator approval 또는 제품 정책이 있을 때만 수행함.

권장 흐름은 다음과 같음.

```text
payroll import
-> note preparation analysis
-> 준비 작업 preview
-> operator approval
-> preparation tx 실행
-> rescan/nullifier check
-> payroll plan 확정
```

이 기본값은 잘못된 자동 split/merge로 treasury note를 예상 밖으로 바꾸는 위험을 줄이기 위함임. downstream 제품이 충분한 운영 guardrail을 갖추면 tenant별 auto-prepare를 켤 수 있음.

## 민감정보 보호 정책

reservation/payroll DB는 privacy-sensitive DB로 취급함.

기본 원칙은 다음과 같음.

- raw nullifier는 index에 직접 쓰지 않음.
- `nullifier_lookup_key = HMAC(index_key, nullifier)` 같은 deterministic keyed lookup을 사용함.
- `index_key_id` 또는 `lookup_key_version`을 함께 저장해 key rotation을 지원함.
- raw nullifier, commitment, recipient, amount, payroll item mapping은 가능하면 field-level encryption으로 저장함.
- 원문 nullifier/commitment/recipient/amount를 로그와 telemetry에 남기지 않음.
- operator UI에는 축약값, hash, key id 중심으로 표시함.
- proof artifact, disclosure payload, reservation detail에는 retention 정책을 둠.

## 기본 제품 설정 요약

| 항목 | 기본값 |
| --- | --- |
| user disclosure | `all-private` / `none` |
| audit disclosure | operation success evidence로 별도 저장 |
| user/self-view digest | expected field가 있을 때만 success evidence로 요구 |
| note preparation | approval 기반 |
| retry | `operation_id` 기준 idempotent |
| Submitted/Unknown/ManualReview release | TTL 자동 release 금지 |
| sensitive DB | HMAC lookup key, key version, field-level encryption 권장 |
| public disclosure | 명시적 opt-in 또는 테스트 용도 |
