# Clairveil Client API Checklist

이 문서는 Clairveil client가 downstream chain, prover, fixture와 연동하기 전에 확인해야 하는 API/config/checklist를 정리합니다.

English version: [clairveil-client-api-checklist.md](clairveil-client-api-checklist.md)

## 1. Downstream Inputs The Client Needs

Downstream chain/client team은 client release 전에 아래 값을 확정해야 합니다.

- chain-id
- denom
- account prefix
- shielded address prefix
- gRPC/REST/RPC endpoint set
- prover topology와 endpoint
- prover auth policy
- audit master pubkey
- consensus circuit identity(`privacy-note-v1`), manifest `v2`, VK/schema checksum policy
- gas policy
- relayer 지원 여부
- disclosure UX policy
- storage/custody policy

## 2. Chain Queries

Client가 사용할 최소 query:

```text
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/scan_events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom=**}
GET /clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}
GET /clairveil/privacy/v1/assets/by_id/{asset_id_hex}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/privacy_scan
POST /clairveil/privacy/v1/commitment_paths_at_root
```

Wallet note sync에는 typed `privacy_scan`(`privacy-scan-v2`)을 우선 사용하고 full `(height, global_sequence, output_index)` cursor를 저장해야 합니다. Reference SDK는 source가 typed query를 구현하지 않은 경우에만 `scan_events`를 사용하고, 더 오래된 source에서는 raw transaction search로 fallback합니다. Typed source가 존재하는데 query가 실패하면 terminal error입니다. 조용히 downgrade하면 batch ciphertext가 누락될 수 있습니다. Client는 pagination/cursor 저장, response validation, bounded retry를 구현해야 합니다. `privacy_scan`, `scan_events` 같은 public global projection은 bounded endpoint failover가 가능하지만 failover 중 schema/cursor 의미를 바꾸면 안 됩니다. Legacy fallback의 `scan_events`는 page 안에 필터링된 event type만 있을 때 빈 `events` 배열과 `has_more=true`를 반환할 수 있으므로 이 경우에도 `next_height`, `next_sequence`로 cursor를 전진시켜야 합니다. Raw `events` query는 preferred wallet sync contract가 아니라 compatibility/debugging/auditor surface입니다.

일반적인 batch spent refresh에는 JSON body를 쓰는 `POST /clairveil/privacy/v1/nullifiers`를 사용해야 합니다. 요청당 nullifier는 최대 1000개로 나누고, 더 큰 wallet은 chunk 처리해야 합니다. GET binding은 작은 compatibility check 용도로 남아 있지만, 큰 query string은 browser, mobile gateway, proxy의 URL 길이 제한을 넘을 수 있습니다.

Nullifier query는 privacy-sensitive합니다. Wallet이 특정 nullifier의 spent 여부를 묻는다는 것은 그 note를 추적하고 있을 가능성을 endpoint에 알리는 신호가 될 수 있습니다. 기본 정책은 같은 endpoint 안에서만 nullifier query를 retry하는 것입니다. 같은 nullifier 묶음을 다른 public endpoint로 failover하는 동작은 제품/사용자가 명시적으로 켠 경우에만 허용해야 합니다.

## 3. Tx Messages

Client가 생성하거나 broadcast해야 하는 message:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/BatchTransfer
/clairveil.privacy.v1.Msg/Withdraw
```

중요:

- `MsgTransfer`는 absolute `expires_at_unix`, user disclosure, mandatory audit disclosure, optional sender self-view disclosure field, encrypted output note, 정확히 2개의 2-byte `view_tags`를 포함합니다.
- `MsgDeposit`은 transparent amount/asset과 note commitment를 binding하는 deposit proof를 요구합니다.
- `MsgWithdraw`는 output note field를 갖지 않습니다.
- `MsgBatchTransfer`는 1..16 input과 1..32 output을 처리하는 one-proof `BatchJoinSplit16x32` message입니다. 여러 `MsgTransfer`를 envelope하는 legacy CLI `transfer-batch`와 혼동하면 안 됩니다.
- Client는 legacy `new_note_commitment`, `encrypted_note` withdraw 값을 만들면 안 됩니다.
- Transfer `view_tags`는 signed canonical payload digest에 포함되지만 server-filterable ownership 증거는 아닌 untrusted performance hint입니다. 안전한 기본 sync는 mismatch에서도 full decrypt해야 하고 skip은 recovery/rescan을 갖춘 explicit fast-mode opt-in이어야 합니다.
- `creator`는 transfer/withdraw에서 의도적으로 replaceable합니다. Transfer output/disclosure/chain/expiry와 withdraw recipient/chain/expiry는 owner intent/proof-bound입니다.

## 4. Prover API

Companion prover HTTP paths:

```text
POST /v1/prover/transfer
POST /v1/prover/withdraw
POST /v1/proofs/batch-transfer
```

Batch route는 batch integration이 제공하는 one-proof reference surface입니다. Legacy transfer/withdraw prover route와 path namespace가 다르고 batch prepared-payload/proof contract를 사용하므로, client가 문자열 치환으로 route를 추론하면 안 됩니다.

Batch-transfer wire version 네 개를 서로 독립적으로 pin합니다.

| Wire object | Code constant | 필수 값 |
| --- | --- | --- |
| Prepared payload | `batchtransfer.PreparedBatchTransferPayloadVersion` | `batch-transfer-payload-v1` |
| Prepared proof | `batchtransfer.PreparedBatchTransferProofVersion` | `batch-transfer-proof-v1` |
| Prover request envelope | `provertransport.BatchTransferProofRequestVersion` | `v1` |
| Prover response envelope | `provertransport.BatchTransferProofResponseVersion` | `v1` |

Request/response envelope version은 nested payload/proof version을 대신하지 않습니다. 어느 layer든 mismatch가 있으면 compatibility를 추측하지 말고 prepared operation을 다시 생성합니다.

Client가 검증해야 할 것:

- request version
- response version
- payload hash
- proof payload hash
- proof hex shape
- timeout
- auth failure
- malformed response

현재 breaking version은 transfer payload `v5`, transfer proof/request/response `v2`, withdraw prover/final payload와 proof/request/response `v2`, batch payload/proof/request/response `batch-transfer-payload-v1`/`batch-transfer-proof-v1`/`v1`/`v1`, relay handoff/schema `v2`, disclosure plaintext/query `privacy-fixed-v1`입니다. Legacy payload는 거부하고 다시 생성합니다.

Remote prover를 쓰는 경우 request/response body는 privacy-sensitive data로 취급해야 합니다.

Prepared payload는 output이 immutable이어도 private note witness를 포함하므로 prover failover를 일반 read query처럼 처리하면 안 됩니다. Prover A 실패 후 B/C로 보내면 privacy boundary가 넓어집니다. 안전한 기본값은 single endpoint/no failover이고 같은 endpoint retry만 가능합니다. Multi-prover failover는 추가 endpoint를 명시한 경고 뒤 사용자/product-policy가 explicit opt-in해야 합니다.

## 5. Retry And Failover Policy

Retry와 endpoint failover를 같은 기능으로 취급하지 않습니다.

| 요청 유형 | 기본 정책 |
| --- | --- |
| `tree_state`, `audit_config`, `circuit_config`, `privacy_scan`, `scan_events` 같은 public read query | schema/cursor 의미가 같을 때 bounded retry와 endpoint failover 허용 가능 |
| nullifier query | 기본은 같은 endpoint retry. 다른 endpoint failover는 opt-in |
| tx broadcast | 자동 retry/failover 기본 off. 재구성/재전송 전 tx hash와 nullifier 상태 확인 |
| prover request | 기본은 timeout/validation과 same-endpoint retry만 허용. multi-prover failover는 explicit opt-in |

Tx broadcast timeout은 실패를 증명하지 않습니다. Tx가 이미 mempool 또는 chain에 들어갔지만 client가 응답만 받지 못했을 수 있습니다. 새 tx를 만들거나 endpoint를 바꾸거나 새 sequence로 재서명하기 전에는 가능한 경우 tx hash를 먼저 확인하고, 그 다음 nullifier 상태를 확인해야 합니다. 이렇게 해야 sequence 혼선, 중복 제출, nullifier conflict를 줄일 수 있습니다.

## 6. Fixture And Schema Checks

Client CI는 최소 아래를 검증해야 합니다.

- Go SDK와 같은 prepared payload hash를 계산합니다.
- transfer payload/proof/request/response가 `v5`/`v2`/`v2`/`v2`, withdraw prover/final payload와 proof/request/response가 `v2`인지 검증합니다.
- batch payload/proof/request/response가 `batch-transfer-payload-v1`/`batch-transfer-proof-v1`/`v1`/`v1`이며 네 wire layer를 서로 독립적으로 검증합니다.
- Exact 13-field transfer/9-field spend public-input 순서와 non-reduced SHA-256 128-bit limb를 fixture로 재현합니다.
- `CircuitConfig`가 consensus `CircuitSetIdentity`를 반환하고 local manifest/env checksum을 consensus authority로 쓰지 않는지 검증합니다.
- `docs/schemas/clairveil-js-wallet-contract.schema.json` fixture shape를 검증합니다.
- `x/privacy/client/sdk/conformance/testdata` fixture를 로드합니다.
- `privacy_batch_transfer_v1_contract.json`이 `TestBatchTransferContract`를 통과해야 합니다. 이 test는 fixture의 schema, 다섯 boundary shape, restart/retry policy, typed scan policy, payroll evidence graph를 검사하고, 네 wire version, circuit/route, 16x32 limit를 현재 Go constant와 binding합니다.
- `examples/js-sdk-fixture-validator`와 같은 semantic check를 수행합니다.
- relay withdraw handoff fixture로 relayer `creator`와 payload `recipient` 분리를 검증합니다.
- `examples/js-sdk-prover-http-client`와 같은 prover timeout/auth/response validation을 구현합니다.

Repo 기준 빠른 검증 명령:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
go test ./x/privacy/client/sdk/conformance -run '^TestBatchTransferContract$' -count=1
```

## 7. Release Gate Checklist

Client release 전 최소 검증:

- deposit e2e
- preferred `privacy_scan` V2 기반 note scan/rescan, full cursor, typed response fail-closed 검증. Legacy `scan_events` fallback은 `(height, sequence)`와 empty page/`has_more` 처리 유지
- 지원하지 않는 `scan_format_version` 또는 `view_tag_version`이 wallet cursor를 조용히 전진시키지 않는지 검증
- forced rescan/recovery에서 view tag mismatch를 비권위 힌트로 취급하고 full trial decrypt 수행 가능
- `nullifiers` 기반 batch spent refresh는 1000개 이하 chunk로 처리하고, 필요 시 개별 nullifier fallback
- shielded transfer e2e
- public disclosure decode/verify
- recipient-encrypted disclosure decode/verify
- sender self-view disclosure decode/verify
- audit disclosure decode/verify, auditor UX가 있는 경우
- deposit/withdraw flow 이후 target denom의 reserve query가 `invariant_holds=true`를 반환
- direct withdraw
- relayed withdraw와 relayer 제출 `MsgWithdraw` field mapping
- `block_time >= expires_at_unix`에서 transfer/withdraw가 거부되고 relay handoff가 expiry 연장/recipient 치환을 못 하는지 검증
- cross-chain replay, output/disclosure substitution, duplicate nullifier/commitment 거부와 creator-replacement 성공 case
- exact-match withdraw 실패와 self-transfer/planner 안내 UX
- retry/failover 정책이 public read query, nullifier query, tx broadcast, prover request를 분리함
- prover timeout/retry/cancel
- disclosure verification failure UI
- remote prover auth/rate limit/logging/retention, remote prover를 쓰는 경우

Downstream release gate는 repo의 `make examples`만으로 충분하지 않습니다. 실제 chain prefix, denom, endpoint, audit pubkey, prover topology를 적용한 testnet e2e가 필요합니다.

## 8. Compatibility Checklist

Breaking 또는 migration impact가 있는 변경:

- `proto/clairveil/privacy/v1` field/message/service 변경
- payload hash 계산 방식 변경
- prover request/response version 변경
- scan projection version 또는 cursor semantics 변경
- view tag 파생 방식, 길이, event field 변경
- disclosure payload version 변경
- circuit public input shape 변경
- deposit proof requirement 변경
- reserve/accounting query shape 변경
- fixture schema 변경
- withdraw exact-match 정책 변경
- relay withdraw handoff payload/message mapping 변경
- audit disclosure 필수 여부 변경

이런 변경이 있으면 client product brief, UX flows, risk decisions, API checklist, JS SDK handoff, release note impact를 함께 갱신해야 합니다.

이 계약을 적용할 때 cached prepared payload, proof response/job, old local development artifact를 지우고 `privacy-note-v1` artifact를 다시 생성하며 old circuit/disclosure version metadata를 저장한 client cache를 resync해야 합니다. Legacy prepared-payload decode path는 없습니다.

## 9. Related Documents

- [Client product brief](clairveil-client-product-brief-kr.md)
- [Client UX flows](clairveil-client-ux-flows-kr.md)
- [Client risk decisions](clairveil-client-risk-decisions-kr.md)
- [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)
- [Downstream integration guide](clairveil-downstream-cosmos-integration-guide-kr.md)
- [Testing guide](clairveil-testing-guide-kr.md)

## 10. Batch transfer reference / downstream client gate

Repository에는 batch chain core와 reference Go batch SDK, bounded remote prover route, typed wallet scanner, durable payroll path, staged batch CLI가 포함됩니다. 이는 experimental reference surface이지 배포된 downstream JS/TS SDK나 production product가 아닙니다. Downstream client는 support를 advertise하기 전에 같은 fixture와 안전 기본값을 재현해야 합니다.

- [ ] Consensus active set `privacy-note-v1`의 required Deposit/Spend/JoinSplit/BatchJoinSplit16x32 순서를 pin하고 artifact identity, VK hash, public-input schema가 하나라도 다르면 거부합니다.
- [ ] Note/disclosure/envelope payload를 canonical `privacy-fixed-v1`으로 encode합니다. Raw ciphertext, JSON plaintext, 잘못된 envelope kind, non-zero reserved byte, trailing byte를 거부합니다.
- [ ] `AssetRegistryV1`을 denom-to-`asset_id`와 reverse lookup의 authoritative source로 사용하며 missing, collision, inconsistent entry에서는 fail closed합니다.
- [ ] Unified `privacy-scan-v2` state를 전체 `(height, global_sequence, output_index)` cursor로 소비하고 선택한 root와 정확히 같은 path snapshot을 요청합니다. Current-root path는 incremental node를 사용하므로 online historical-rebuild budget을 소비하지 않습니다. Non-current historical path는 persisted root/count/height metadata를 요구하며 public query는 최대 1,024 leaves와 keeper당 동시 rebuild 2개만 허용하고 그 이상은 `ResourceExhausted`를 반환합니다. Online bound를 넘으면 current root 또는 trusted local historical index를 사용합니다. 별도 offline recovery/export bound는 `MaxMerkleRebuildLeaves`(1,048,576)입니다. Remote historical root/path query가 wallet 관심을 누설할 수 있다는 warning을 유지합니다.
- [ ] Note/scan/proof cache와 old artifact를 지우고 fresh genesis에서 시작하여 `privacy-note-v1` artifact를 다시 생성하고 rescan합니다. Legacy decode나 in-place migration을 제공하지 않습니다.
- [ ] Production batch public statement를 정확히 다음 12-field 순서로 취급합니다. `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. Repository reference path는 core conformance fixture를 재현하지만 downstream 구현도 이를 독립 재현하기 전에는 client support를 advertise하지 않습니다.
- [ ] Role-aware artifact readiness를 사용합니다. Validator는 VK만, prover는 선택한 R1CS/PK만 lazy load합니다. Circuit별 admission default는 `max_in_flight=1`, `max_queued=4`, positive `max_request_bytes=8388608`이며 0은 invalid입니다.
- [ ] Bounded `proverservice.Handler`만 노출하고 raw transport handler는 절대 직접 노출하지 않습니다. Automatic prover failover를 끕니다. Cancellation은 client wait cancellation이지 solver termination 보장이 아니므로 hard resource boundary에는 process isolation을 사용합니다.
