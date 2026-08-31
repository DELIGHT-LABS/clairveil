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
| Direct withdraw | `prepareWithdraw` | Spend one exact-match note, sign/broadcast, and reconcile the nullifier. |
| Relayed withdraw | `prepareRelayWithdraw` | Persist the handoff boundary, let a relayer submit the immutable payload, and reconcile until expiry or spent evidence. |
| Disclosure review | `decodeUserDisclosure`, `decodeSelfViewDisclosure`, `decodeAuditDisclosure` | Show plaintext only with verified digest status. |

`prepareTransfer` is the transfer entry point. The checked-in v0.3.1 example
does not expose one-proof batch preparation or submission: its server reports
`serverFeatures.batchTransfer=false`, and the UI does not call
`prepareTransferBatch`. ClairveilJS and the chain core may expose the underlying
batch contract for separately reviewed products, but a configuration value
alone must not turn that contract into a WebApp feature.

An EVM profile is eligible only when its privacy precompile implements the
Clairveil 0.3.1 canonical ABI: a proof-bearing deposit, a transfer that preserves
self-view disclosure and absolute expiry, and a withdraw with no legacy output
note fields. The WebApp does not support an ABI fallback or dummy withdraw
outputs.

## Explicitly Out Of Scope

Do not add a UI, route, automatic fallback, or background worker for any of
the following in this WebApp release:

- payroll, treasury allocation, recipient-file import, or bulk payment review
- one-proof batch-transfer preparation, durable checkpoint, or submission UI
- automatic, silent, or background splitting/submission of any batch

Payroll and unattended bulk orchestration remain valid core/reference
integration contracts but are outside this browser product boundary.

## Release Contract

The WebApp targets ClairveilJS `0.3.1` and the fixed privacy contracts:

- note/disclosure envelope: `privacy-fixed-v1`
- transfer prepared payload/proof/prover envelopes: `v5` / `v2` / `v2`
- withdraw and relay handoff: `v2`
- protocol identity: `privacy-note-v1`, including
  `batch-joinsplit-16x32-v1` as the required fourth chain-core circuit
- preferred wallet scan: `privacy-scan-v2`

This unreleased WebApp supports only fresh state initialized in its current
v0.3.1 namespaces and current lifecycle schema. It defines no migration from
an earlier browser cache or lifecycle store and no in-place downgrade. An old
note cache, reservation, lease, relay snapshot, prepared payload, or proof must
not be decoded as current state; initialize an empty current namespace and run
a full scan instead. The complete policy is specified in [WebApp storage and
recovery](clairveil-web-app-storage-recovery.md).

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
6. Keep `serverFeatures.batchTransfer` false and do not expose one-proof batch
   preparation or submission UI in this example. A loopback prover proxy route
   used for SDK/reference testing does not change that product gate. A future
   product integration must
   define its own encrypted checkpoint, wallet-confirmation, reconciliation,
   and end-to-end release gates before changing this product boundary.

## Relationship To General Client Documents

The [client API checklist](clairveil-client-api-checklist.md) and
[JS SDK handoff](clairveil-js-sdk-handoff.md) describe all supported Clairveil
contracts, including batch capabilities needed by other products. They remain
authoritative for protocol semantics. This document is authoritative for what
the current browser WebApp is allowed to expose.
