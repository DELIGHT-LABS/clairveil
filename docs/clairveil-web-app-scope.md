# Clairveil WebApp Scope

Korean version: [clairveil-web-app-scope-kr.md](clairveil-web-app-scope-kr.md)

This document fixes the product boundary for the browser WebApp shipped from
`examples/clairveil-dapp`. It is a product integration scope, not a statement
about every capability in the Clairveil chain core or ClairveilJS.

## Supported User Flows

The current WebApp may expose only these privacy flows.

| Flow | Browser client surface | Required outcome |
| --- | --- | --- |
| Setup and sync | `buildRootSigningMessage`, `derivePrivacyAccount`, `scanWalletNotes` | Derive one account-scoped shielded identity and reach a verified scan cursor. |
| Deposit | `prepareDeposit` | Obtain a product-provided `DepositCircuit` proof, sign, broadcast, then scan the created note. |
| Single shielded transfer | `prepareTransfer` | Prepare one ordinary transfer operation, including a self-merge step when the planner requires one. |
| Feature-gated atomic batch transfer | `prepareTransferBatch` | On Cosmos only, prepare one 1–16 input / 1–32 output `MsgBatchTransfer`, sign/broadcast it as one transaction, and verify every payment output before reporting success. |
| Direct withdraw | `prepareWithdraw` | Spend one exact-match note, sign/broadcast, and reconcile the nullifier. |
| Relayed withdraw | `prepareRelayWithdraw` | Persist the handoff boundary, let a relayer submit the immutable payload, and reconcile until expiry or spent evidence. |
| Disclosure review | `decodeUserDisclosure`, `decodeSelfViewDisclosure`, `decodeBatchSelfViewDisclosure`, `decodeAuditDisclosure` | Show plaintext only with verified digest status. Batch self-view review must load complete validated `privacy-scan-v2` outputs and verify each output separately. |

`prepareTransfer` remains the default entry point. The batch editor is exposed
only when the server-backed config enables `serverFeatures.batchTransfer`; it
must not appear for EVM profiles or become an automatic transfer fallback.

An EVM profile is eligible only when its privacy precompile implements the
Clairveil 0.2 canonical ABI: a proof-bearing deposit, a transfer that preserves
self-view disclosure and absolute expiry, and a withdraw with no legacy output
note fields. The WebApp does not support an ABI fallback or dummy withdraw
outputs.

## Explicitly Out Of Scope

Do not add a UI, route, automatic fallback, or background worker for any of
the following in this WebApp release:

- payroll, treasury allocation, recipient-file import, or bulk payment review
- automatic or silent splitting of an oversized batch
- background batch submission without the explicit review and feature gate

Payroll and unattended bulk orchestration remain valid core/reference
integration contracts but are outside this browser product boundary.

## Release Contract

The WebApp targets ClairveilJS `0.2.x` and the fixed privacy contracts:

- note/disclosure envelope: `privacy-fixed-v1`
- transfer prepared payload/proof/prover envelopes: `v5` / `v2` / `v2`
- withdraw and relay handoff: `v2`
- atomic batch transfer: `BatchJoinSplit16x32` and `MsgBatchTransfer`
- preferred wallet scan: `privacy-scan-v2`

The WebApp treats ClairveilJS 0.2 as a fresh persistence epoch. It must never
decode or reuse a pre-0.2 note cache, reservation, lease, relay snapshot,
prepared payload, or proof. Upgrade behavior is specified in
[WebApp storage and recovery](clairveil-web-app-storage-recovery.md).

## Implementation Gates

Before exposing a flow, the WebApp must:

1. Validate its chain profile with
   [`clairveil-web-client-config.schema.json`](schemas/clairveil-web-client-config.schema.json)
   and reject duplicate profile IDs or a flattened compatibility field that
   disagrees with the active profile, including a top-level `keplrChainInfo`.
   Do not emit `keplrChainInfo` for an active EVM profile. The legacy top-level EVM
   `accountPrefix` is host metadata, not a privacy-client input; only the
   active profile's prefix may be passed to ClairveilJS.
2. Verify wallet/network identity and query current on-chain circuit, audit,
   disclosure, asset, and tree configuration. Static configuration is not
   consensus authority.
3. Use the storage and durable reservation recovery rules in
   [WebApp storage and recovery](clairveil-web-app-storage-recovery.md).
4. Use the lifecycle and API rules in
   [WebApp integration](clairveil-web-app-integration.md).
5. Meet the browser/prover deployment boundary in
   [WebApp deployment](clairveil-web-app-deployment.md).
6. For batch transfer, keep payload/proof checkpoints encrypted, show total,
   change, input/output capacity and the all-or-nothing boundary before wallet
   approval, require an explicit opt-in before creating multiple independent
   atomic batches, and report each item as successful only after typed output
   evidence and every input nullifier reconcile.

## Relationship To General Client Documents

The [client API checklist](clairveil-client-api-checklist.md) and
[JS SDK handoff](clairveil-js-sdk-handoff.md) describe all supported Clairveil
contracts, including batch capabilities needed by other products. They remain
authoritative for protocol semantics. This document is authoritative for what
the current browser WebApp is allowed to expose.
