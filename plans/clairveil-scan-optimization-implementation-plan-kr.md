# Clairveil Note Scan 최적화 구현 계획

English version: [clairveil-scan-optimization-implementation-plan.md](clairveil-scan-optimization-implementation-plan.md)

이 문서는 Clairveil core repository에서 note scan 비용을 줄이기 위해 이번 작업에서 구현할 범위와 의도적으로 제외할 범위를 정리한다. 목표는 web wallet/mobile wallet이 대량 이벤트 구간을 동기화할 때 느끼는 fetch 비용과 local decrypt 비용을 줄이는 것이다.

## 작업 전 기준

- Merkle tree depth는 32이며 최대 leaf 수는 약 42.9억 개다.
- 현재 scan은 `PrivacyEvents(after_height, page, limit, event_types)`로 deposit/transfer event를 가져온다.
- 기존 event query는 offset page 방식이고 limit 상한이 200이다.
- SDK wallet scan은 transfer output의 `cipher_text_1`, `cipher_text_2`를 모두 trial decrypt한다.
- transfer decrypt는 view key로 실패하면 spend key fallback을 한 번 더 시도한다.
- 작업 전 proto/event에는 scan tag나 view tag가 없다.

## 구현 상태

이 문서의 1-5번 범위는 현재 구현에 포함되어 있다. 6번 server-filterable hint/FMD와 7번 proof-bound tag는 의도적으로 제외했다.

- Core query는 `ScanEvents` cursor projection과 `CheckNullifiers` batch spent query를 제공한다.
- SDK scan service는 `ScanEvents`와 `CheckNullifiers`가 있으면 우선 사용하고, 기존 provider path를 fallback으로 유지한다.
- Transfer payload는 `v3`이며 `view_tag_hexes`가 prepared payload hash에 포함된다.
- `MsgTransfer.view_tags`와 transfer event의 `view_tag_1`, `view_tag_2`는 output index 기준으로 encrypted output과 정렬된다.
- View tag는 아직 circuit-bound가 아니므로 wallet은 local decrypt 최적화에만 사용해야 한다.

## 설계 철학

1. Core는 private key, root seed, plaintext note를 알면 안 된다.
2. Prover는 proof payload만 처리하고 note scan을 대신하지 않는다.
3. Chain node/core는 public event feed를 더 효율적으로 제공한다.
4. Wallet은 자기 key로 note 소유 여부를 판단하고 wallet-owned local cache를 유지한다. Production client의 encrypted storage 정책은 core 밖에서 별도로 구현해야 한다.
5. Server-filterable hint는 privacy model을 바꾸므로 기본 core scope에 넣지 않는다.
6. Per-note view tag는 이번에는 untrusted performance hint로 구현하되, 나중에 proof-bound tag로 승격할 수 있게 포맷을 고정한다.

## 이번 구현 범위

### 1. Cursor/projection scan event query

새 query를 추가한다.

```text
ScanEvents(after_height, after_sequence, limit, event_types)
```

요구사항:

- offset page 대신 `(height, sequence)` keyset cursor를 사용한다.
- 응답은 wallet scan에 필요한 projection만 포함한다.
- deposit output은 `commitment`, `encrypted_note`를 포함한다.
- transfer output은 `commitment`, `cipher_text`, `view_tag`를 output index와 함께 포함한다.
- 응답에는 `next_height`, `next_sequence`, 실제 적용된 `limit`, `has_more`, `scan_format_version`, `view_tag_version`을 포함한다.
- `limit`은 반환 event 수뿐 아니라 cursor가 검사하는 event page budget으로도 적용한다. 필터링된 event만 포함된 page는 `events=[]`, `has_more=true`일 수 있으므로 client는 `next_height`, `next_sequence`를 따라 계속 진행해야 한다.
- 기존 `PrivacyEvents`는 compatibility/reference query로 유지한다.

기대 효과:

- page offset skip 비용을 제거한다.
- RPC 요청 수와 payload decode 비용을 줄인다.
- mobile/web wallet이 resume 가능한 cursor를 저장할 수 있다.

### 2. SDK cursor scan

SDK wallet scan을 새 `ScanEvents` query를 우선 사용하는 구조로 전환한다.

요구사항:

- wallet cache에 `last_sequence`를 추가한다.
- `ScanEvents` provider가 있으면 cursor scan을 사용한다.
- provider가 없으면 기존 `SearchPrivacyTxs` path로 fallback한다.
- rollback/reset 시 `last_height`, `last_sequence`, notes를 함께 초기화한다.

기대 효과:

- full rescan과 incremental sync 모두 안정적인 resume semantics를 갖는다.
- downstream mobile/web wallet이 같은 cursor model을 구현할 수 있다.

### 3. Batch nullifier query

여러 nullifier의 spent 상태를 한 번에 조회하는 query를 추가한다.

요구사항:

- `CheckNullifiers(repeated nullifier)` query를 추가한다.
- SDK scan은 batch query provider가 있으면 이를 우선 사용한다.
- 개별 `CheckNullifier` query는 유지한다.

기대 효과:

- 내 note 수가 많을 때 spent status 갱신 RPC 왕복 수를 줄인다.

### 4. Privacy-safe per-note view tag

Transfer output마다 2-byte `view_tag`를 추가한다. 이번 구현에서는 proof/circuit에 묶지 않는다.

요구사항:

- `MsgTransfer.view_tags`는 `new_commitments`, `cipher_texts`와 1:1 배열이다.
- 현재 transfer output 수는 2개이므로 길이는 2여야 한다.
- 각 tag는 정확히 2 bytes다.
- event에는 `view_tag_1`, `view_tag_2`를 기록한다.
- scan projection에는 output별 `view_tag`를 포함한다.
- 안전한 기본 wallet scan은 tag mismatch일 때도 full trial decrypt로 fallback한다. Tag는 아직 proof/circuit에 묶이지 않으므로, 잘못된 hint만으로 owned note를 누락하면 안 된다.
- mismatch output을 건너뛰는 fast mode는 recovery/rescan 정책을 갖춘 client가 명시적으로 선택할 때만 사용한다.
- tag가 없거나 형식이 맞지 않으면 recovery/fallback path로 기존 trial decrypt를 수행한다.
- forced rescan 또는 rollback recovery처럼 신뢰 복구가 목적일 때는 tag mismatch도 무시하고 full trial decrypt를 수행한다.

Tag derivation v1:

```text
shared_point = ECDH(ephemeral_secret, receiver_view_pubkey)
view_tag_full = MiMC(
  "clairveil.view_tag.v1",
  shared_point.x,
  shared_point.y,
  output_commitment,
  output_index
)
view_tag = first_2_bytes(canonical_32_bytes(view_tag_full))
```

설계 이유:

- tag는 ephemeral shared point에서 파생되므로 stable recipient fingerprint가 아니다.
- commitment와 output index를 넣어 output swap/reuse 여지를 줄인다.
- MiMC 기반이라 나중에 circuit이 같은 값을 계산하는 proof-bound tag로 승격하기 쉽다.
- 이번 버전에서는 untrusted hint이므로 wallet은 tag를 보안 근거로 사용하지 않는다. 특히 기본 sync는 tag mismatch만으로 cursor를 확정하며 owned note를 버리지 않는다.

기대 효과:

- event fetch 수는 줄지 않는다.
- 명시적 fast mode에서는 non-owned transfer output의 local decrypt 실패 경로 비용을 줄일 수 있다.
- 안전 기본 모드에서는 성능 개선보다 future proof-bound tag로 승격 가능한 wire format을 먼저 확보한다.

### 5. Minimal versioning

아직 정식 배포된 scan API가 없다는 전제로 migration layer는 만들지 않는다. 대신 format/version 표식만 둔다.

요구사항:

- scan projection response에 `scan_format_version = 1`을 포함한다.
- view tag가 있는 response에는 `view_tag_version = 1`을 포함한다.
- conformance fixture, schema, docs를 새 format에 맞춘다.

## 이번에 하지 않는 범위

### 6. Server-filterable hint/FMD

노드나 indexer가 tag로 "내 note 후보"만 필터링하는 기능은 이번 범위에서 제외한다.

제외 이유:

- event fetch를 크게 줄일 수 있지만 query metadata privacy가 바뀐다.
- static tag나 FMD 정책은 제품/위협모델 결정이 먼저 필요하다.
- 기본 privacy profile에 조용히 넣을 성격이 아니다.

### 7. Proof-bound tag

View tag를 circuit public input에 묶는 작업은 이번 범위에서 제외한다.

제외 이유:

- circuit, proving key, verifying key, payload, fixtures가 모두 바뀐다.
- local decrypt 최적화만을 위해 당장 필요한 변경은 아니다.

다만 4번의 tag derivation과 field layout은 proof-bound tag로 승격 가능하게 설계한다.

## 작업 단위와 커밋 계획

1. 계획 문서 추가.
2. Proto/query/keeper에 cursor scan event와 batch nullifier query 추가.
3. SDK provider와 scan service를 cursor/batch query에 맞게 확장.
4. View tag crypto/encryption/scan path를 추가하고 transfer message/event를 갱신.
5. Conformance fixture/schema/docs를 새 scan format에 맞게 갱신.
6. 전체 테스트를 실행하고 실패를 수정한다.
7. 기준 브랜치 대비 전체 diff에 대해 review-fix-loop를 실행하고 active finding이 없어질 때까지 수정한다.

## 검증 계획

- `make proto`
- `go test ./x/privacy/...`
- `go test ./...`
- 필요한 경우 `make build`
- 최종 review-fix-loop
