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
- consensus circuit identity (`privacy-intent-v2`), manifest `v2`, VK/schema checksum policy
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

Clients should prefer `scan_events` for wallet note sync because it is a cursor-based projection of wallet-relevant event data. The raw `events` query remains useful for compatibility, debugging, and auditors. The client should implement pagination/cursor persistence and bounded retry. Endpoint failover is safe by default only for public read queries such as `tree_state`, `audit_config`, `circuit_config`, and `scan_events`. `scan_events` can return an empty `events` array with `has_more=true` when the page only contains filtered-out event types; clients must still persist/advance to `next_height` and `next_sequence` and continue scanning.

Use `POST /clairveil/privacy/v1/nullifiers` with a JSON body for normal batch spent refresh. Send at most 1000 nullifiers per request and chunk larger wallets. The GET binding remains available for small compatibility checks, but large query strings can exceed browser, mobile gateway, or proxy URL limits.

Nullifier queries are privacy-sensitive because a wallet asking about a nullifier reveals that it may be tracking the corresponding note. The default policy should retry nullifier queries only against the same endpoint. Failing over the same nullifier set to a different public endpoint should be an explicit product/user opt-in.

## 3. Tx Messages

Messages the client must build or broadcast:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

Important:

- `MsgTransfer` includes absolute `expires_at_unix`, user disclosure, mandatory audit disclosure, optional sender self-view disclosure fields, encrypted output notes, and exactly two 2-byte `view_tags`.
- `MsgDeposit` requires a deposit proof binding the transparent amount/asset to the note commitment.
- `MsgWithdraw` has no output note fields.
- Clients must not create legacy `new_note_commitment` or `encrypted_note` withdraw values.
- Transfer `view_tags` are untrusted performance hints for local scan. They are included in the signed canonical payload digest but are not server-filterable ownership evidence. Safe default sync must still full-decrypt on a mismatch; skipping mismatch outputs requires explicit fast-mode opt-in and recovery/rescan support.
- `creator` is intentionally replaceable for transfer and withdraw. Transfer outputs/disclosures/chain/expiry and withdraw recipient/chain/expiry are owner-intent/proof-bound.

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

Current breaking versions are transfer payload `v5`, transfer proof/request/response `v2`, withdraw prover/final payload and proof/request/response `v2`, relay handoff/schema `v2`, and disclosure plaintext/query `v5`. Reject and regenerate legacy payloads.

When using a remote prover, request/response bodies are privacy-sensitive data.

Prover request failover must not behave like ordinary read-query failover. A prepared payload still contains private note witness even though its outputs are immutable. If prover A fails, sending it to B or C expands the privacy boundary. The safe default is a single endpoint with no failover; retry may target the same endpoint. Multi-prover failover requires explicit user/product-policy opt-in and a warning naming the additional endpoints.

## 5. Retry And Failover Policy

Do not treat retry and endpoint failover as the same feature.

| Request type | Default policy |
| --- | --- |
| Public read queries such as `tree_state`, `audit_config`, `circuit_config`, `scan_events` | bounded retry and endpoint failover are acceptable |
| Nullifier queries | retry against the same endpoint by default; cross-endpoint failover is opt-in |
| Tx broadcast | automatic retry/failover is off by default; check tx hash and nullifier status before rebuilding or rebroadcasting |
| Prover requests | timeout/validation and same-endpoint retry only by default; multi-prover failover requires explicit opt-in |

For tx broadcast, a timeout does not prove failure. The tx may already be in the mempool or chain while the client missed the response. Before creating a new tx, changing endpoint, or re-signing with a new sequence, clients should check the tx hash when available and then check nullifier status. This prevents sequence confusion, duplicate submission, and nullifier conflicts.

## 6. Fixture And Schema Checks

Client CI should validate at least:

- prepared payload hashes are calculated the same way as the Go SDK;
- transfer payload/proof/request/response use `v5`/`v2`/`v2`/`v2`; withdraw prover/final payload and proof/request/response use `v2`;
- fixtures reproduce the exact 13-field transfer and 9-field spend public-input order and non-reduced SHA-256 128-bit limbs;
- `CircuitConfig` returns consensus `CircuitSetIdentity` and never treats local manifest paths/env checksums as consensus authority;
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

## 7. Release Gate Checklist

Minimum validation before client release:

- deposit e2e
- note scan/rescan through `scan_events`, including persisted `(height, sequence)` cursor and empty-page/`has_more` handling
- unsupported `scan_format_version` or `view_tag_version` does not advance the wallet cursor silently
- forced rescan/recovery treats view tag mismatches as non-authoritative and runs full trial decrypt
- batch spent refresh through `nullifiers` in <=1000-item chunks, with fallback behavior for individual nullifier checks if needed
- shielded transfer e2e
- public disclosure decode/verify
- recipient-encrypted disclosure decode/verify
- sender self-view disclosure decode/verify
- audit disclosure decode/verify, if auditor UX exists
- reserve query returns `invariant_holds=true` for the target denom after deposit/withdraw flows
- direct withdraw
- relayed withdraw and relayer-submitted `MsgWithdraw` field mapping
- expiry boundary rejects transfer/withdraw at `block_time >= expires_at_unix`, and relayed handoff cannot extend expiry or replace recipient
- cross-chain replay, output/disclosure substitution, duplicate nullifier/commitment, and creator-replacement positive cases
- no-exact-match withdraw failure and self-transfer/planner guidance
- retry/failover policy separates public read queries, nullifier queries, tx broadcast, and prover requests
- prover timeout/retry/cancel
- disclosure verification failure UI
- remote prover auth/rate limit/logging/retention, if using a remote prover

Downstream release gates are not satisfied by repository-level `make examples` alone. The downstream client also needs testnet e2e with its real chain prefix, denom, endpoints, audit pubkey, and prover topology.

## 8. Compatibility Checklist

Changes with breaking or migration impact:

- `proto/clairveil/privacy/v1` field/message/service changes
- payload hash calculation changes
- prover request/response version changes
- scan projection version or cursor semantics changes
- view tag derivation, length, or event field changes
- disclosure payload version changes
- circuit public input shape changes
- deposit proof requirement changes
- reserve/accounting query shape changes
- fixture schema changes
- withdraw exact-match policy changes
- relay withdraw handoff payload/message mapping changes
- audit disclosure requiredness changes

When these change, update the client product brief, UX flows, risk decisions, API checklist, JS SDK handoff, and release note impact together.

Adopting this contract requires clearing cached prepared payloads, proof responses/jobs, and old local development artifacts, regenerating `privacy-intent-v2` artifacts, and resyncing any client cache that persisted old circuit or disclosure-version metadata. There is no legacy prepared-payload decode path.

## 9. Related Documents

- [Client product brief](clairveil-client-product-brief.md)
- [Client UX flows](clairveil-client-ux-flows.md)
- [Client risk decisions](clairveil-client-risk-decisions.md)
- [JS SDK handoff](clairveil-js-sdk-handoff.md)
- [Downstream integration guide](clairveil-downstream-cosmos-integration-guide.md)
- [Testing guide](clairveil-testing-guide.md)
