# Clairveil Client UX Flows

이 문서는 Clairveil client가 제공해야 하는 사용자 흐름과 실패/복구 UX를 정리합니다.

English version: [clairveil-client-ux-flows.md](clairveil-client-ux-flows.md)

이 문서는 화면 wireframe이 아닙니다. 각 downstream wallet/app 팀은 이 흐름을 자기 제품의 화면, 상태, 문구로 변환해야 합니다.

## 1. First Setup

1. Client가 downstream chain config를 로드합니다.
2. 사용자가 transparent account를 연결합니다.
3. Client가 root signing message 서명을 요청합니다.
4. Shielded identity와 disclosure key를 파생합니다.
5. 최신 tree state와 wallet scan event를 sync합니다.
6. Consensus `privacy-note-v1` circuit identity를 확인하고 shielded address와 spendable balance를 표시합니다.

필수 상태:

- account 연결 전
- root signing 대기
- identity 파생 완료
- 최초 note sync 진행 중
- sync 완료
- sync 실패

## 2. Note Sync

1. Cursor 기반 `ScanEvents` query로 deposit/transfer output을 가져옵니다.
2. viewing key로 decrypt 가능한 note를 찾습니다.
3. commitment와 Merkle path를 확인합니다.
4. 가능하면 batch nullifier query로 spent 상태를 확인합니다.
5. `has_more=true`인 빈 `ScanEvents` page도 계속 따라간 뒤, 마지막 scan height/sequence cursor를 저장하고 local note cache를 업데이트합니다.

필수 실패 상태:

- endpoint unavailable
- scan cursor pagination 중단
- encrypted note decode 실패
- Merkle path 없음
- historical root 없음
- local cache corruption

복구 UX:

- retry
- endpoint 변경
- scan cursor rollback
- local cache backup 후 reset/rescan
- spent 상태 재확인

## 3. Deposit

1. 사용자가 deposit amount와 denom을 입력합니다.
2. Client가 transparent balance와 gas fee를 확인합니다.
3. 사용자가 tx를 approve/sign합니다.
4. Client가 deposit tx를 broadcast합니다.
5. Client가 deposit event를 scan해서 encrypted note를 복구합니다.
6. Shielded balance를 갱신합니다.

필수 UX:

- tx는 성공했지만 note scan이 아직 끝나지 않은 상태를 별도로 표시합니다.
- deposit tx 실패와 note recovery 실패를 구분합니다.
- local note cache가 최신이 아니면 balance를 확정값처럼 보여주지 않습니다.

## 4. Shielded Transfer

1. Recipient shielded address를 입력합니다.
2. Amount와 denom을 입력합니다.
3. User disclosure policy를 선택합니다.
4. Disclosure mode를 선택합니다.
5. Spendable note와 dummy input 필요 여부를 계산합니다.
6. Sender self-view disclosure를 기본 포함하고, 사용자가 명시적으로 끈 경우에만 제외합니다.
7. Prepared transfer payload를 만듭니다.
8. Chain ID, absolute expiry, recipient/change effect, disclosure plane을 표시하고 확정한 뒤 owner-intent signature 하나를 만듭니다.
9. Prover에서 proof를 생성합니다.
10. `MsgTransfer`를 broadcast합니다.
11. Event scan으로 recipient/change note 상태를 갱신합니다.
12. Disclosure report를 검증합니다.

필수 UX:

- recipient `clairs1...` validation
- privacy policy별 공개 범위 설명
- audit disclosure가 항상 포함된다는 안내
- sender self-view가 기본 포함되며, 끄면 보낸 transfer 상세 복구가 제한된다는 안내
- prover 진행률 또는 대기 상태
- prover timeout/cancel/retry
- 다른 prover로 automatic switch하지 않으며 witness 공유 boundary 확장은 명시적 경고와 opt-in 필요
- `block_time >= expires_at_unix`이면 만료되므로 기존 payload expiry를 연장하지 말고 rebuild/re-sign
- tx broadcast 전 최종 확인

Disclosure plaintext를 표시할 때는 반드시 digest verification 결과를 함께 보여줘야 합니다.

## 5. Withdraw

1. Transparent recipient와 amount를 입력합니다.
2. Client가 exact-match note를 찾습니다.
3. Exact-match note가 없으면 self-transfer로 note 크기를 맞추도록 안내하거나 별도 planner flow로 준비합니다.
4. Proof를 생성합니다.
5. Direct 또는 relayed withdraw를 실행합니다.
6. Nullifier spent 상태와 transparent receive 상태를 표시합니다.

중요 제약:

- withdraw는 output note 또는 change note를 만들지 않습니다.
- `MsgWithdraw`에는 output note field가 없습니다.
- withdraw proof/message 자체는 larger note split, fragmented note merge, change note 생성을 하지 않습니다.
- client는 withdraw 전에 별도 self-transfer/planner flow를 제공할 수 있습니다.

필수 실패 상태:

- exact-match note 없음
- recipient address invalid
- payload expired
- chain/circuit identity mismatch
- historical root 없음
- nullifier already spent
- proof verification 실패

## 6. Relayed Withdraw

1. 사용자가 withdraw payload를 준비합니다.
2. Client가 `chain_id`, `recipient`, `expires_at_unix`, `payload_hash`를 표시합니다.
3. User 또는 relayer가 payload를 전달받습니다.
4. Relayer가 tx fee를 내고 broadcast합니다.
5. Client가 tx result와 spent 상태를 확인합니다.

필수 UX:

- relayer는 user shielded secret을 알 필요가 없다는 안내
- payload expiry 표시
- recipient 변경 불가 안내
- `creator`는 fee payer relayer라 바뀔 수 있지만 recipient, chain, expiry는 바뀌지 않는다는 안내
- handoff 이후 relayer는 expiry 전까지 제출할 수 있으며 local cancel 또는 reservation release가 payload를 무효화하지 않는다는 안내
- `block_time >= expires_at_unix`는 expired이며 wallet/relayer 모두 연장할 수 없다는 안내
- prepared payload/proof JSON이 privacy-sensitive data라는 경고
- output을 redirect할 수 없더라도 prover payload가 private note witness를 노출한다는 경고

## 7. Disclosure Review

1. Tx hash 또는 payload를 입력합니다.
2. Client가 disclosure event를 찾습니다.
3. Disclosure plane을 선택하거나 자동 감지합니다.
4. 필요하면 disclosure private key로 decrypt합니다.
5. Versioned plaintext의 blinding을 복원하고 독립 blinded user/full digest를 recompute해서 on-chain digest와 비교합니다.
6. verified 결과와 disclosed fields를 표시합니다.

지원해야 하는 plane:

- `user`: public 또는 recipient-encrypted user disclosure
- `self-view`: sender가 자신의 disclosure private key로 보는 보낸 transfer 상세
- `audit`: auditor가 audit disclosure private key로 보는 mandatory audit disclosure

표시 정책:

- `verified=true`: disclosed fields를 사실로 표시할 수 있습니다.
- `verified=false`: plaintext를 사실처럼 표시하면 안 됩니다.
- decrypt 실패: key mismatch, wrong plane, malformed payload를 구분합니다.

## 8. Failure And Recovery Matrix

| Failure | 사용자에게 보여줄 의미 | 복구 방향 |
| --- | --- | --- |
| note scan 실패 | shielded balance가 최신이 아닐 수 있음 | retry, endpoint 변경, rescan |
| local cache 손상 | local note DB를 신뢰할 수 없음 | backup 후 reset/rescan |
| prover timeout | proof 생성이 끝나지 않음 | 같은 endpoint retry 또는 rebuild, 다른 endpoint는 explicit privacy opt-in 후에만 사용 |
| circuit identity mismatch | local verifier/prover artifact가 consensus와 다름 | 중단 후 exact `privacy-note-v1` artifact 설치, restart/rescan |
| payload expired | relayed payload가 더 이상 유효하지 않음 | 새 payload 생성 |
| disclosure verification 실패 | payload를 사실로 신뢰할 수 없음 | verified=false 표시, 원문 숨김 또는 경고 |
| exact-match withdraw note 없음 | withdraw 가능한 동일 금액 note가 없음 | shielded self-transfer 또는 planner flow로 note 크기 맞춤 |
| historical root 없음 | proof input이 현재 chain state와 맞지 않음 | wallet sync/rescan 후 retry |
| nullifier already spent | note가 이미 사용됨 | cache 업데이트, 중복 broadcast 방지 |
| audit config 없음/불일치 | transfer가 audit disclosure 요구사항을 만족하지 못함 | chain config 확인 후 retry |

## 9. Related Documents

- [Client product brief](clairveil-client-product-brief-kr.md)
- [Client risk decisions](clairveil-client-risk-decisions-kr.md)
- [Client API checklist](clairveil-client-api-checklist-kr.md)
- [CLI reference](clairveil-cli-reference-kr.md)

## 10. Session 2 Migration And Future-Batch UX

Production UX는 여전히 deposit, native 2x2 transfer, withdraw입니다. `BatchJoinSplit16x32`는 unavailable 또는 experimental documentation으로만 표시합니다. 현재 `transfer-batch` command는 여러 기존 2x2 operation을 coordination할 뿐 하나의 16x32 proof처럼 표현하면 안 됩니다.

`privacy-note-v1` / `privacy-fixed-v1` 전환 시 fresh genesis와 destructive local reset을 요구합니다. Old note, cursor state, pending/proof job, circuit artifact를 제거한 뒤 artifact를 다시 생성하고 rescan합니다. Legacy JSON note, disclosure, raw ciphertext, cached proof를 fixed binary contract로 조용히 해석하면 안 됩니다. 표시 denom은 authoritative `AssetRegistryV1`으로 resolve하며 unknown `asset_id`는 printable fallback denom이 아니라 error입니다.

Unified scan cursor는 `(height, global_sequence, output_index)` 전체로 저장하고 해당 cursor까지 모든 output이 durable해진 뒤에만 commit합니다. Spend screen은 prover에 표시한 root와 정확히 같은 Merkle path snapshot을 사용해야 합니다. Current-root incremental path에는 1,048,576-leaf cap이 없습니다. Non-current historical path는 persisted root/count/height metadata를 사용하지만 node를 bounded rebuild하므로 1,048,576 leaves로 제한됩니다. Cap을 넘으면 current root 또는 trusted local historical index를 사용합니다. Remote historical lookup을 수행할 때는 어떤 state에 언제 관심을 보였는지 노출될 수 있다는 warning을 유지합니다.

Future batch review screen은 동결된 count와 root를 기준으로 설계할 수 있지만 network support를 암시하면 안 됩니다. 12개 public input 순서는 `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`입니다. Prover-busy response는 같은 endpoint에 retry할 수 있지만 automatic cross-endpoint failover는 계속 꺼 둡니다. Screen을 cancel해도 in-process proving이 중단됐다는 뜻이 아니므로 transaction/proof-job state를 reconcile할 때까지 note reservation을 유지합니다.
