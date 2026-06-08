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
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom}
GET /clairveil/privacy/v1/nullifier/{nullifier}
```

Client는 pagination, timeout, retry, endpoint failover를 구현해야 합니다.

## 3. Tx Messages

Client가 생성하거나 broadcast해야 하는 message:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

중요:

- `MsgTransfer`는 user disclosure와 mandatory audit disclosure field를 포함합니다.
- `MsgDeposit`은 transparent amount/asset과 note commitment를 binding하는 deposit proof를 요구합니다.
- `MsgWithdraw`는 output note field를 갖지 않습니다.
- Client는 legacy `new_note_commitment`, `encrypted_note` withdraw 값을 만들면 안 됩니다.

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

## 5. Fixture And Schema Checks

Client CI는 최소 아래를 검증해야 합니다.

- Go SDK와 같은 prepared payload hash를 계산합니다.
- `docs/schemas/clairveil-js-wallet-contract.schema.json` fixture shape를 검증합니다.
- `x/privacy/client/sdk/conformance/testdata` fixture를 로드합니다.
- `examples/js-sdk-fixture-validator`와 같은 semantic check를 수행합니다.
- `examples/js-sdk-prover-http-client`와 같은 prover timeout/auth/response validation을 구현합니다.

Repo 기준 빠른 검증 명령:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
```

## 6. Release Gate Checklist

Client release 전 최소 검증:

- deposit e2e
- note scan/rescan
- shielded transfer e2e
- public disclosure decode/verify
- recipient-encrypted disclosure decode/verify
- audit disclosure decode/verify, auditor UX가 있는 경우
- deposit/withdraw flow 이후 target denom의 reserve query가 `invariant_holds=true`를 반환
- direct withdraw
- relayed withdraw
- exact-match withdraw 실패와 self-transfer/planner 안내 UX
- prover timeout/retry/cancel
- disclosure verification failure UI
- remote prover auth/rate limit/logging/retention, remote prover를 쓰는 경우

Downstream release gate는 repo의 `make examples`만으로 충분하지 않습니다. 실제 chain prefix, denom, endpoint, audit pubkey, prover topology를 적용한 testnet e2e가 필요합니다.

## 7. Compatibility Checklist

Breaking 또는 migration impact가 있는 변경:

- `proto/clairveil/privacy/v1` field/message/service 변경
- payload hash 계산 방식 변경
- prover request/response version 변경
- disclosure payload version 변경
- circuit public input shape 변경
- deposit proof requirement 변경
- reserve/accounting query shape 변경
- fixture schema 변경
- withdraw exact-match 정책 변경
- audit disclosure 필수 여부 변경

이런 변경이 있으면 client product brief, UX flows, risk decisions, API checklist, JS SDK handoff, release note impact를 함께 갱신해야 합니다.

## 8. Related Documents

- [Client product brief](clairveil-client-product-brief-kr.md)
- [Client UX flows](clairveil-client-ux-flows-kr.md)
- [Client risk decisions](clairveil-client-risk-decisions-kr.md)
- [JS SDK handoff](clairveil-js-sdk-handoff-kr.md)
- [Downstream integration guide](clairveil-downstream-cosmos-integration-guide-kr.md)
- [Testing guide](clairveil-testing-guide-kr.md)
