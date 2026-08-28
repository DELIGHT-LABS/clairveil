# Clairveil WebApp Integration

Korean version: [clairveil-web-app-integration-kr.md](clairveil-web-app-integration-kr.md)

This is the production integration guide for the browser WebApp scope,
including its feature-gated atomic batch flow, in
[WebApp scope](clairveil-web-app-scope.md). It uses the
`clairveiljs/browser-dapp` public API. Do not import the example application's
`public/app.js` as a library contract.

## 1. Create A Client From One Validated Profile

Load one validated profile from the versioned Web client configuration schema.
The active profile is the only source of the browser client's transport,
endpoints, and identity prefixes. For EVM it is also the source of `evmGasLimit` and
`evmSendGasLimit`; recreate a cached browser client if either limit changes
or its transport changes before a later prepare or wallet request.

Any cached recipient-address suggestion is profile-scoped display data. Clear
it synchronously on a profile change and discard an in-flight address lookup
whose profile no longer matches; never offer an old profile's transparent or
shielded address as a recipient in the new profile.

```ts
import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";

// rawConfig is the complete JSON fetched from the verified config endpoint.
const config = validateClairveilWebClientConfig(rawConfig);
const profile = config.activeProfile;
const client = createClairveilBrowserDappClient({
  profile,
  // Product exposure is explicit; SDK/prover availability is not discovery.
  enableExperimentalBatchTransfer:
    config.serverFeatures?.batchTransfer === true,
  queryTimeoutMs: 30_000,
  nullifierFailover: false
});
```

Do not construct a partial profile from selected fields: the validator requires
the complete schema profile and rejects duplicate IDs, a missing active profile,
or an incompatible flattened compatibility field. For EVM, the selected profile
includes the schema-required `evmRpc`, `evmChainId`,
`evmPrivacyPrecompileAddress`, `evmNativeDenom`, and gas limits, and its
`evmDepositMode` must be `payable-exact-value`. Its precompile must implement the
Clairveil 0.3.1 canonical deposit/transfer/withdraw/batch ABI: a proof-bearing
deposit with exact `msg.value`, transfer self-view plus expiry, exact-match
withdraw with no legacy output fields, and `singleProofBatchTransfer`.
Compare the connected wallet network with `evmChainId` before preparing an EVM
transaction. A raw chain-profile field is deployment input, not trusted chain
state.

An optional JSON-safe `evmAuthorizationProfile` supplies the target chain's
EIP-712 domain and authorization-kind allowlist; never synthesize it in the
WebApp or reuse another chain's values. A reviewed non-canonical
`EvmContractAdapter` may be injected through
`globalThis.CLAIRVEIL_EVM_CONTRACT_ADAPTERS[profile.id]`, a complete
`PrivacyStateAdapter` through
`globalThis.CLAIRVEIL_PRIVACY_STATE_ADAPTERS[profile.id]`, and a built-in or
reviewed custom finality policy through
`globalThis.CLAIRVEIL_EVM_FINALITY_POLICIES[profile.id]`. Bind every adapter to
the selected profile/precompile and treat its result as untrusted until the SDK
canonical validator accepts it. Without a privacy-state adapter, the typed host
query surfaces remain mandatory.

For Cosmos-EVM, keep the typed privacy-scan transaction hash separate from the
Ethereum wallet/receipt hash. Verify the scan record's indexed
`ethereumTxHash` relationship before using it as EVM success evidence; do not
compare the hashes for equality.

Before displaying spendable notes and immediately before every privacy
prepare, the integration must fail closed on this preflight:

```ts
await client.health();
await client.assertTransferProtocolConfig(profile.denom);
await client.queryReserve(profile.denom);
```

`assertTransferProtocolConfig` verifies the consensus circuit identity and
authoritative asset mapping, audit configuration, and disclosure configuration;
`health()` validates current node/tree state. For EVM, also compare a read-only
`eth_chainId` response from the configured EVM RPC with `evmChainId`. Any
failure or profile mismatch disables every privacy action and hides spendable
inventory until a fresh successful preflight.

When a privacy prepare runs this preflight, bind every request and its
success/failure UI update to that prepare's privacy-session generation. If the
account, wallet, profile, or selected REST endpoint changes while it is in
flight, discard the result rather than setting the new session's chain-safety
state from the old endpoint or profile. Disable endpoint selection while a note
mutation or account transaction is queued or running.

The example accepts a static config only when `/api/health` is absent or
unreachable. In that case it fetches the same-origin `/dapp-config.json`
artifact that the production gate verifies. A reachable server configuration
with an invalid schema, profile mismatch, or non-success response is a sync
failure, not a reason to fall back silently.

The browser bounds the `/api/health` bootstrap and static-config fetches to
30 seconds and 1 MiB. A health timeout is treated as unreachable and may use
the validated static artifact; an oversized or malformed response remains a
sync failure.

A static host that returns its HTML shell for a missing `/api/health` route is
treated as having no health endpoint only after the direct static artifact has
validated as `serverBacked: false`; an invalid server JSON configuration never
falls back silently.

Treat bootstrap and health data as a profile-scoped view. If a later health
refresh or profile selection wins while an earlier health request is pending,
discard the earlier response and error rather than reapplying its config,
feature visibility, or helper data to the current profile.

`waitForEvmTransaction(...)` is the normal confirmation helper. The typed,
read-only `evmJsonRpc<TResult>(method, params)` method is available only for
an EVM read that has no higher-level wrapper, such as receipt recovery. It
uses the configured RPC endpoint, not MetaMask/EIP-1193, and must never be
used for account access, signing, or transaction submission.

## 2. Establish The Privacy Session

For every account change, start a new in-memory privacy session.

1. Connect the configured wallet and verify its chain/network.
2. Obtain the transparent address and public key.
3. Build the domain-separated root signing message with
   `client.buildRootSigningMessage(address, pubKeyHex)`.
4. Request the signature through Keplr `signArbitrary` or MetaMask
   `personal_sign`; encode the EVM signature as base64.
5. Call `client.derivePrivacyAccount({ address, pubKeyHex, signatureBase64 })`.
6. Keep the signature/root material out of logs, analytics, URLs, server
   requests, and crash reports.

The root signature is privacy-sensitive authority material. Never substitute a
normal transaction signature or silently reuse a signature from another
transparent account, chain, or profile.

## 3. Scan Before Displaying A Spendable Balance

Build the encrypted note store and reservation manager described in
[WebApp storage and recovery](clairveil-web-app-storage-recovery.md), then
scan using the privacy session:

```ts
const scan = await client.scanWalletNotes({
  address,
  pubKeyHex,
  signatureBase64,
  noteStore,
  ...resumeCursor
});

await noteStore.save({
  notes: scan.notes,
  scanCursor: scan.scanCursor
});
```

Persist the complete `privacy-scan-v2` cursor. A note is spendable only after
a successful explicit nullifier response establishes `used: false`; unknown,
malformed, or failed nullifier results are excluded. A typed-scan failure is
terminal and must not silently fall back to another source with different
cursor semantics. The detailed retry and failover rules are in the
[client API checklist](clairveil-client-api-checklist.md).

## 4. Execute The Supported Flows

| Flow | Prepare | Before external boundary | After external boundary |
| --- | --- | --- | --- |
| Deposit | `prepareDeposit` with a product-provided `DepositCircuit` proof provider | If using a remote provider, pin its exact HTTPS `depositProofUrl` in the active profile and send the required proof material only to that origin. User signs the returned Cosmos sign doc or sends the EVM transaction. | Wait/lookup, then scan the output note. |
| Transfer | `prepareTransfer` with the privacy session and encrypted stores | If status is `self_merge_required`, obtain explicit approval for that ordinary self-transfer, complete it, rescan, then replan. | Sign/broadcast one prepared transfer and reconcile reservations/nullifiers. |
| Atomic batch transfer | Feature-gated `prepareTransferBatch` with per-payment disclosure and encrypted payload/proof checkpoint callbacks | After the authoritative prepare scan, bind every row to the exact prepared recipient, amount, disclosure, total, change, 1–16 inputs, and 1–32 outputs before opening the wallet. Never split silently; require explicit approval for multiple independent atomic batches. | Submit one Cosmos `MsgBatchTransfer` or canonical EVM `singleProofBatchTransfer` call. An ambiguous result remains pending until every input nullifier and expected payment output matches typed operation evidence. |
| Direct withdraw | `prepareWithdraw` | Require one exact-match note and a current chain time where required. | Sign/broadcast and reconcile the input nullifier. |
| Relay withdraw | `prepareRelayWithdraw` | Verify fresh chain time; record durable relay handoff before copying or uploading the payload. The browser handoff must work without a local relayer helper. | The relayer may submit until expiry; reconcile rather than treating local cancel as revocation. |

For Cosmos wallet submissions, acquire a cross-tab transaction lock scoped to
the canonical on-chain chain ID and transparent account before any prepare call
that reads the account sequence, and keep it through signing and the durable
broadcast boundary. The lock must not include a UI profile ID or local storage
epoch: equivalent profiles and tabs that observed different local epochs still
control the same on-chain account. Public and private actions use the same lock;
an unresolved public marker or private reservation broadcast blocks a new
sequence until reconciliation. In local-test mode, re-read and compare the
current genesis/storage epoch inside that lock before preparing the sequence;
reject a stale tab rather than letting it submit on the restarted chain.
Because transparent send is available before the root-signature session is
restored, mirror each private Cosmos tx hash into a non-sensitive canonical
account marker immediately before RPC broadcast and check it without opening
the encrypted reservation store. Remove it only after matching chain and
reservation reconciliation reaches a safe terminal state.
Relay-only preparation is exempt only when a different relayer account will
submit it and no local self transaction is needed.

When an EVM profile publishes an EIP-712 domain, the feature-gated batch panel
may exercise `singleProofBatchTransferWithAuthorization`. The built-in path
binds the connected account as both effective sender and submitting executor,
uses a fresh random nonce, and sets the authorization deadline to the prepared
operation expiry. A different external executor requires a separate durable
product handoff flow.

An authorized batch-audit view must use typed `privacy-scan-v2`, group outputs
by transaction hash, and verify/decode the mandatory audit disclosure for every
output. A raw `batch_transfer` event contains operation roots and audit-key
identity, not per-output audit ciphertexts or digests, and is not an input for
reconstructing batch disclosure.

`depositProofUrl` is an optional profile field for a reviewed, product-owned
deposit proof service. It is an exact `POST` endpoint, not a prover base URL:
the browser sends the `note_json` and `note_commitment_hex` material expected by
the provider and requires a versioned JSON proof response with
`Content-Type: application/json`. A static WebApp
must leave Deposit unavailable when neither this URL nor a browser/WASM provider
is configured. The example's same-origin `/api/deposit/proof` route is a
loopback local-test fallback only. The example aborts a deposit-proof request
that has not completed its response within 120 seconds; this client bound does
not replace provider-side body-size and timeout limits. It disallows redirects,
requires the final response URL to equal the pinned endpoint, and bounds a
deposit-proof response to 1 MiB before parsing it.

Capture the pinned proof endpoint when preparation starts. An SDK proof-provider
callback must not re-read the active profile later: assert the preparation
session before it sends proof material and after the bounded response returns.
If the account, wallet, or profile changed, discard the result rather than
sending the former operation to the replacement profile's endpoint.

The example always lets a browser prepare and copy a relay payload once the
normal privacy preflight and reservation checks pass. Its `Relay` button is
only the optional local `POST /api/relayer/withdraw` submission adapter; a
static deployment uses Copy (or its product-owned handoff adapter) and must
not treat a missing local helper as a reason to block payload preparation.

Never create a dummy withdraw output. Never retry a transaction merely because
the browser missed a broadcast response. First look up the known transaction
identity and refresh the nullifier result.

## 5. Reservation And Broadcast Boundary

Use the SDK's `signDirectAndBroadcast`, `broadcastSignedTx`, or supported EVM
submission integration whenever possible, passing both the reservation manager
and the prepared reservation metadata. A custom submission integration must
perform this exact order for the *whole operation*:

1. Keep the `Proving`/`ProofReady` lease alive.
2. Persist `markBroadcastAttempting(...)` with any tx/sign-doc identity.
3. Cross the external wallet/RPC/relayer boundary exactly once.
4. Persist `markSubmitted(...)` only after actual submission, or
   `markUnknown(...)` if the transaction may have reached the network.
5. Reconcile by tx identity and explicit nullifier evidence before replanning,
   releasing, or attempting another submission.

If step 4 fails, the step-2 marker remains a safety lock. Do not clear it or
resubmit; enter recovery. For relay handoff, first renew the complete reservation
batch lease through the immutable payload expiry, then call
`recordRelayHandoff(...)`, all before exposing the payload. Retain the lock until
authoritative expiry or reconciliation.

Do not record that external-handoff state for a same-origin local-relayer
submit button: no raw payload has left the browser yet. After fresh
chain/nullifier checks, stop the prepared-payload lease heartbeat, write
`markBroadcastAttempting(...)`, then make exactly one local-relayer request.
Treat any failure after that marker as possibly submitted and reconcile it;
reserve the ordered expiry-length lease renewal followed by
`recordRelayHandoff(...)` for Copy, download, QR, upload, or another actual
payload egress.

Make each value-moving action single-flight from the initial click through the
wallet request. Disabling a button only after asynchronous privacy setup leaves
a reentrancy window that can create two independently prepared submissions.
Release that UI lock only when the operation reaches its known pending or
terminal path; a later deliberate action may then create a new operation.
Apply this before local-relayer chain-time or recovery preflight as well:
server-side idempotency is a backstop, not a reason to let two browser flows
advance the same relay payload concurrently.

Treat wallet signing and raw-transaction submission as separate asynchronous
boundaries. Keep the prepared operation's privacy session through the wallet
approval, recheck it when the signature returns, and recheck it again
immediately before starting the raw broadcast. If the session changed while
the wallet was open, discard the signed checkpoint and take only the original
session's no-broadcast recovery path; never submit it under the replacement
account.
For MetaMask, `eth_sendTransaction` is the approval/submission boundary:
recheck the captured session after chain and gas setup and immediately before
calling it. After that final check, persist an account-scoped hashless
`attempting` marker immediately before the wallet request. Promote it to the
returned tx hash synchronously before any later session check or receipt poll.
Only an explicit provider rejection (`4001`) proves that the wallet did not
submit and may clear the hashless marker automatically. A crash, timeout, or
other provider error keeps it fail-closed; the user must inspect wallet history
before using a guarded manual clear. That clear removes only the separately
stored public send/deposit record under the canonical account lock; it never
removes the private Cosmos signed-tx fence. If the private fence is corrupt,
keep it fail-closed and offer only the reviewed fresh-state reset path. A
session change before the marker is a
no-broadcast failure, while a session change after it retains recovery state.

## 6. Stable User Actions For Failures

Map errors to actions without parsing prose:

| Condition | User action | Automatic action |
| --- | --- | --- |
| Typed scan/config validation failure | Show sync unavailable; offer same-source retry or reset/rescan. | Never downgrade scan semantics. |
| Nullifier result unknown | Show balance/input as unavailable. | Retry only the same endpoint by default. |
| Planner self-merge requirement | Ask for explicit self-transfer approval. | Do not choose batch or multi-send as a fallback. |
| Batch capacity exceeded | Offer an explicit multiple-atomic-batch choice and explain cross-batch partial completion. Show each prepared recipient and disclosure mode before approval. | Never split, reorder, or retry individual payments silently. Lock verified rows out of later retries. |
| Batch checkpoint or evidence unresolved | Show reservation recovery and keep affected inputs locked. | Do not overwrite the encrypted checkpoint or submit another batch until reconciliation is terminal. |
| Prover timeout/transport failure | Show cancel/retry for the same prover. | Do not fail over to a second prover without explicit privacy opt-in. |
| Broadcast timeout/terminal-write failure | Show pending reconciliation. | Query tx identity and nullifiers; block resubmission. |
| Expired payload | Rebuild and re-sign. | Never extend an existing payload. |
| Manual review | Present chain/payload evidence to an authorized operator. If no queryable submitted-transaction or signed-transaction identity remains, explain the unresolved wallet-request cases and require an explicit local-cancellation acknowledgement. | Never make the note spendable automatically. |

Bind a user-initiated ManualReview resolution to the privacy session and the
reviewing account that approved it. Re-read every reservation, nullifier, and
transaction-outcome evidence under that session immediately before requesting
the `ReplanRequired` transition. If the account, wallet, or profile changes
while any check or transition is in flight, abort without applying the former
operator's approval, rendering a result, or showing a stale error in the new
session.

### Explicit Cancellation Of An Untracked Wallet Request

`sign_doc_hash` identifies a request shown to a wallet; it does **not** prove
that the wallet request was approved, rejected, or submitted. A browser can
therefore retain a `ManualReview` reservation with a sign-doc identity but no
queryable `submitted_tx_hash` or `tx_bytes_hash`.

For a non-relay operation in that state, a product MAY offer an explicit
**Cancel reservation and reprepare** action. It is a local recovery action, not
a chain cancellation, and the UI MUST explain that the request may have stopped
before the wallet prompt, been rejected by the user, or been approved while the
browser lost the submission result. It MUST also warn that a previously
approved transaction could still be submitted later and require the reviewing
user to confirm that wallet activity and the explorer show no submitted
transaction.

The action is allowed only when every reservation in the operation still lacks a
queryable submitted/signed-transaction identity. Immediately after the explicit
acknowledgement, and under the same privacy session and reviewing account, the
product MUST re-read all reservation evidence and require every input nullifier
to be explicitly unspent. It may then transition only that operation to
`ReplanRequired`, recording the operator approval and an explicit-untracked-
request cancellation reason. It MUST NOT mark a note spendable directly,
silently release a relay/handoff reservation, or bypass the normal scan and
planner path for the replacement transaction.

Use `ClairveilErrorCode` where the SDK exposes it. Prover HTTP errors use the
versioned `invalid_request`, `method_not_allowed`, `not_found`, `unauthorized`,
`unavailable`, and `proof_failed` codes; see the
[JS SDK handoff](clairveil-js-sdk-handoff.md).

## 7. Required Integration Tests

The product must test its actual wallet adapters and endpoints, not only mocks:

- initial sync, interruption, encrypted-store reload, and forced rescan;
- deposit, self-merge when required, one transfer, one-proof batch transfer,
  exact-match withdraw, and relay withdraw over both Cosmos and EVM transports;
- wallet rejection before broadcast, RPC timeout after broadcast, and a failed
  local status write after broadcast;
- account, wallet-network, or selected-profile change while a wallet prompt,
  public send, local helper response, or SDK preparation is pending, proving
  stale UI/error updates are discarded and no new send crosses a wallet
  boundary after invalidation;
- tab contention and reload during `Proving`, `ProofReady`, `Submitted`, and
  relay handoff;
- an untracked wallet request with only a sign-doc identity, including the
  acknowledgement UI, fresh nullifier checks, and auditable
  `ManualReview -> ReplanRequired` transition;
- current chain/circuit/audit configuration mismatch;
- EVM wallet-network mismatch and receipt recovery;
- an earlier development persistence record present at startup, proving it is
  not decoded or migrated into the current v0.3.1 namespace before the
  required full rescan;
- batch gate off/on behavior, canonical Cosmos and EVM batch submission,
  EIP-712 authorization binding when configured, encrypted checkpoint reload,
  ambiguous receipt recovery, and typed per-item/audit reconciliation;
- the deployed-origin `verify:production-deployment` gate plus a manual
  Keplr/MetaMask connect, sign, scan, and recovery flow at the same origin.

Run the DApp package checks in [the testing guide](clairveil-testing-guide.md)
and the SDK's release verification with the canonical conformance fixtures.
