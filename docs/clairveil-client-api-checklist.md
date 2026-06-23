# Clairveil Client API Checklist

This document lists the API, config, and validation checklist that Clairveil clients should complete before integrating with a downstream chain, prover, and fixtures.

Korean version: [clairveil-client-api-checklist-kr.md](clairveil-client-api-checklist-kr.md)

## 1. Downstream Inputs The Client Needs

The downstream chain/client team must finalize these values before client release.

- chain-id
- denom
- account prefix
- shielded address prefix
- gRPC/REST/RPC endpoint set
- prover topology and endpoint
- prover auth policy
- audit master pubkey
- circuit artifact/version/checksum policy
- gas policy
- relayer support
- disclosure UX policy
- storage/custody policy

## 2. Chain Queries

Minimum queries used by clients:

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

The client should implement pagination, timeout, retry, and endpoint failover.

## 3. Tx Messages

Messages the client must build or broadcast:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

Important:

- `MsgTransfer` includes user disclosure and mandatory audit disclosure fields.
- `MsgDeposit` requires a deposit proof binding the transparent amount/asset to the note commitment.
- `MsgWithdraw` has no output note fields.
- Clients must not create legacy `new_note_commitment` or `encrypted_note` withdraw values.

## 4. Prover API

Companion prover HTTP paths:

```text
POST /v1/prover/transfer
POST /v1/prover/withdraw
```

The client must validate:

- request version
- response version
- payload hash
- proof payload hash
- proof hex shape
- timeout
- auth failure
- malformed response

When using a remote prover, request/response bodies are privacy-sensitive data.

## 5. Fixture And Schema Checks

Client CI should validate at least:

- prepared payload hashes are calculated the same way as the Go SDK;
- fixture shape matches `docs/schemas/clairveil-js-wallet-contract.schema.json`;
- fixtures load from `x/privacy/client/sdk/conformance/testdata`;
- semantic checks match `examples/js-sdk-fixture-validator`;
- relay withdraw handoff fixtures validate the relayer `creator` and payload `recipient` split;
- prover timeout/auth/response validation matches `examples/js-sdk-prover-http-client`.

Fast repo-level validation commands:

```bash
make examples
go test ./x/privacy/client/sdk/conformance
```

## 6. Release Gate Checklist

Minimum validation before client release:

- deposit e2e
- note scan/rescan
- shielded transfer e2e
- public disclosure decode/verify
- recipient-encrypted disclosure decode/verify
- audit disclosure decode/verify, if auditor UX exists
- reserve query returns `invariant_holds=true` for the target denom after deposit/withdraw flows
- direct withdraw
- relayed withdraw and relayer-submitted `MsgWithdraw` field mapping
- no-exact-match withdraw failure and self-transfer/planner guidance
- prover timeout/retry/cancel
- disclosure verification failure UI
- remote prover auth/rate limit/logging/retention, if using a remote prover

Downstream release gates are not satisfied by repository-level `make examples` alone. The downstream client also needs testnet e2e with its real chain prefix, denom, endpoints, audit pubkey, and prover topology.

## 7. Compatibility Checklist

Changes with breaking or migration impact:

- `proto/clairveil/privacy/v1` field/message/service changes
- payload hash calculation changes
- prover request/response version changes
- disclosure payload version changes
- circuit public input shape changes
- deposit proof requirement changes
- reserve/accounting query shape changes
- fixture schema changes
- withdraw exact-match policy changes
- relay withdraw handoff payload/message mapping changes
- audit disclosure requiredness changes

When these change, update the client product brief, UX flows, risk decisions, API checklist, JS SDK handoff, and release note impact together.

## 8. Related Documents

- [Client product brief](clairveil-client-product-brief.md)
- [Client UX flows](clairveil-client-ux-flows.md)
- [Client risk decisions](clairveil-client-risk-decisions.md)
- [JS SDK handoff](clairveil-js-sdk-handoff.md)
- [Downstream integration guide](clairveil-downstream-cosmos-integration-guide.md)
- [Testing guide](clairveil-testing-guide.md)
