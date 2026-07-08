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
- circuit artifact/version/checksum policy
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
GET /clairveil/privacy/v1/reserve/{denom}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
```

Wallet note sync에는 cursor 기반 projection인 `scan_events`를 우선 사용해야 합니다. Raw `events` query는 compatibility, debugging, auditor 확인 용도로 유지됩니다. Client는 pagination/cursor 저장과 bounded retry를 구현해야 합니다. Endpoint failover는 `tree_state`, `audit_config`, `circuit_config`, `scan_events` 같은 public read query에 한해 기본적으로 안전합니다. `scan_events`는 page 안에 필터링된 event type만 있을 때 빈 `events` 배열과 `has_more=true`를 반환할 수 있으므로, client는 이 경우에도 `next_height`, `next_sequence`로 cursor를 전진시키고 계속 스캔해야 합니다.

일반적인 batch spent refresh에는 JSON body를 쓰는 `POST /clairveil/privacy/v1/nullifiers`를 사용해야 합니다. 요청당 nullifier는 최대 1000개로 나누고, 더 큰 wallet은 chunk 처리해야 합니다. GET binding은 작은 compatibility check 용도로 남아 있지만, 큰 query string은 browser, mobile gateway, proxy의 URL 길이 제한을 넘을 수 있습니다.

Nullifier query는 privacy-sensitive합니다. Wallet이 특정 nullifier의 spent 여부를 묻는다는 것은 그 note를 추적하고 있을 가능성을 endpoint에 알리는 신호가 될 수 있습니다. 기본 정책은 같은 endpoint 안에서만 nullifier query를 retry하는 것입니다. 같은 nullifier 묶음을 다른 public endpoint로 failover하는 동작은 제품/사용자가 명시적으로 켠 경우에만 허용해야 합니다.

## 3. Tx Messages

Client가 생성하거나 broadcast해야 하는 message:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

중요:

- `MsgTransfer`는 user disclosure, mandatory audit disclosure, optional sender self-view disclosure field, encrypted output note, 정확히 2개의 2-byte `view_tags`를 포함합니다.
- `MsgDeposit`은 transparent amount/asset과 note commitment를 binding하는 deposit proof를 요구합니다.
- `MsgWithdraw`는 output note field를 갖지 않습니다.
- Client는 legacy `new_note_commitment`, `encrypted_note` withdraw 값을 만들면 안 됩니다.
- Transfer `view_tags`는 local scan 속도를 줄이기 위한 untrusted performance hint입니다. 이것은 server-filterable ownership tag가 아니며, 현재 circuit에 binding되어 있지 않습니다. 안전한 기본 sync는 tag mismatch에서도 full decrypt로 복구할 수 있어야 하며, mismatch output을 건너뛰는 동작은 recovery/rescan을 갖춘 명시적 fast mode 정책이어야 합니다.

## 4. Prover API

Companion prover HTTP path:

```text
POST /v1/prover/transfer
POST /v1/prover/withdraw
```

Client가 검증해야 할 것:

- request version
- response version
- payload hash
- proof payload hash
- proof hex shape
- timeout
- auth failure
- malformed response

Remote prover를 쓰는 경우 request/response body는 privacy-sensitive data로 취급해야 합니다.

Prover request failover는 일반 read query failover처럼 동작하면 안 됩니다. Prover request에는 note amount, randomness, Merkle path, nullifier, disclosure payload metadata가 들어갈 수 있습니다. Prover A가 실패했다고 같은 payload를 Prover B, C로 자동 전송하면 privacy boundary가 넓어집니다. 안전한 기본값은 configured prover boundary 안에서 timeout, response validation, retry를 수행하는 것이고, multi-prover failover는 명시적인 제품/보안 정책 결정이어야 합니다.

## 5. Retry And Failover Policy

Retry와 endpoint failover를 같은 기능으로 취급하지 않습니다.

| 요청 유형 | 기본 정책 |
| --- | --- |
| `tree_state`, `audit_config`, `circuit_config`, `scan_events` 같은 public read query | bounded retry와 endpoint failover 허용 가능 |
| nullifier query | 기본은 같은 endpoint retry. 다른 endpoint failover는 opt-in |
| tx broadcast | 자동 retry/failover 기본 off. 재구성/재전송 전 tx hash와 nullifier 상태 확인 |
| prover request | timeout과 response validation은 필수. multi-prover failover는 opt-in |

Tx broadcast timeout은 실패를 증명하지 않습니다. Tx가 이미 mempool 또는 chain에 들어갔지만 client가 응답만 받지 못했을 수 있습니다. 새 tx를 만들거나 endpoint를 바꾸거나 새 sequence로 재서명하기 전에는 가능한 경우 tx hash를 먼저 확인하고, 그 다음 nullifier 상태를 확인해야 합니다. 이렇게 해야 sequence 혼선, 중복 제출, nullifier conflict를 줄일 수 있습니다.

## 6. Fixture And Schema Checks

Client CI는 최소 아래를 검증해야 합니다.

- Go SDK와 같은 prepared payload hash를 계산합니다.
- prepared transfer payload는 version `v3`를 사용하고 `view_tag_hexes`를 포함합니다.
- `docs/schemas/clairveil-js-wallet-contract.schema.json` fixture shape를 검증합니다.
- `x/privacy/client/sdk/conformance/testdata` fixture를 로드합니다.
- `examples/js-sdk-fixture-validator`와 같은 semantic check를 수행합니다.
- relay withdraw handoff fixture로 relayer `creator`와 payload `recipient` 분리를 검증합니다.
- `examples/js-sdk-prover-http-client`와 같은 prover timeout/auth/response validation을 구현합니다.

Repo 기준 빠른 검증 명령:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
```

## 7. Release Gate Checklist

Client release 전 최소 검증:

- deposit e2e
- `scan_events` 기반 note scan/rescan, `(height, sequence)` cursor 저장, empty page/`has_more` 처리
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

## 9. Related Documents

- [Client product brief](clairveil-client-product-brief-kr.md)
- [Client UX flows](clairveil-client-ux-flows-kr.md)
- [Client risk decisions](clairveil-client-risk-decisions-kr.md)
- [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)
- [Downstream integration guide](clairveil-downstream-cosmos-integration-guide-kr.md)
- [Testing guide](clairveil-testing-guide-kr.md)
