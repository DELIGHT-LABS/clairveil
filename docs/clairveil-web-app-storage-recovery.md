# Clairveil WebApp Storage And Recovery

Korean version: [clairveil-web-app-storage-recovery-kr.md](clairveil-web-app-storage-recovery-kr.md)

This document is the minimum production storage and restart contract for the
browser WebApp scope. It replaces the demo-only plaintext storage policy in
`examples/clairveil-dapp`.

## Sensitive Data And Lifetime

| Data | Required location/lifetime |
| --- | --- |
| Root signature, root seed, spend/view/disclosure private material | In-memory or a wallet-controlled secure store only; never application server, URL, analytics, or crash report. |
| Decrypted notes and scan cursor | Encrypted persistent browser store scoped to one chain/profile/account. |
| Reservation state and operation evidence | Encrypted IndexedDB with cross-tab locking. Reservation error fields contain stable internal codes only, never wallet/prover/RPC error prose. |
| Account transaction boundary marker | Two durable canonical transaction-scope/account records that do not require the privacy root signature to open. The public send/deposit record may contain `attempting` plus a random attempt ID before an EVM wallet request and must be promoted to the returned tx hash immediately. The physically separate private Cosmos record contains the exact signed tx hash plus only the generic `privacy` kind. Never store recipient, amount, calldata, note/reservation IDs, or other private operation data in either record. |
| Prepared proof/payload and raw recipient/amount | Memory only unless a flow has a reviewed encrypted recovery design. The feature-gated batch flow stores the exact prepared batch payload/proof/evidence needed for restart recovery, and EVM deposit/transfer/withdraw store the original prepared transaction binding needed to verify a later receipt. These artifacts use dedicated encrypted namespaces and are never mixed with relay metadata. |
| Relay recovery metadata | Encrypted, cross-tab locked, multi-record store; retain only opaque payload hash, reservation IDs/status, tx identity, handoff/submission status, and expiry. Derive each persistence ID from the payload hash or reservation IDs; one payload must never overwrite another and caller-supplied IDs or display-only fields must not be retained. |

Browser `localStorage`, plaintext IndexedDB, or an unencrypted export for
sensitive wallet data, plus any in-memory persistence fallback, are
demo/test-only choices. The narrowly scoped account boundary marker above may be
plaintext because it contains no transaction intent or private material. These
choices are not fallbacks when required encrypted persistence or Web Locks is
unavailable.

## Namespace And Encryption Rules

Use separate v0.3.1 namespace prefixes for the current persistence epoch. The
checked-in example uses scoped keys and an encrypted reservation namespace
equivalent to:

```text
clairveil:v0.3.1:notes-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:operations-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>:<payload-hash>
clairveil:v0.3.1:batch-transfer-artifact-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:evm-deposit-artifact-encrypted:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:evm-operation-artifact-encrypted:<operation-id>:<privacy-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:public-pending:<transaction-scope>:<storage-epoch>:<account-scope>
clairveil:v0.3.1:privacy-pending:<transaction-scope>:<storage-epoch>:<account-scope>
reservation namespace: <chain-id>:<privacy-scope>:<storage-epoch>:<account-scope>
```

`<privacy-scope>` must canonically identify the on-chain privacy system (Cosmos
chain ID, or EVM chain ID plus privacy precompile), while
`<transaction-scope>` identifies the account sequence/nonce domain. Equivalent
UI profiles that point at the same identity must share these scopes; profile
labels and endpoint choices are not storage identities. Replace the privacy
scope when an identity field changes. `<account-scope>` must be stable only for
the selected account and must not be sent to telemetry. A product may use an
opaque keyed identifier instead of a literal address. Never share a namespace
across different privacy systems, chain IDs, wallet kinds, or transparent
accounts.

The dedicated batch checkpoint contains only the canonical prepared
payload/proof, reservation identity, expected aggregate/item evidence, execution
transport, and—when used—the validated EIP-712 authorization transaction
binding. It is authenticated against the active profile, account, and exact
storage key. Save it before the external boundary, keep every linked
reservation locked after an ambiguous result, and clear it only after terminal
typed reconciliation verifies the complete input and expected-output set.

EVM deposit and private-operation artifacts are separate from the batch and
relay namespaces. They bind the original prepared transaction, operation or
reservation identity, and later wallet/receipt hash so receipt finality can be
checked against what the wallet was asked to submit. Corrupt, missing, or
identity-mismatched artifacts fail closed; they must not be replaced by a new
prepare while the original operation remains unresolved. Every artifact
mutation requires Web Locks and a final active-session check immediately before
commit.

For reservations, use `createBrowserReservationStore` with all of these
properties:

- `requireLocks: true`; IndexedDB or Web Locks failure is fatal for spend
  preparation.
- paired `encodeState`/`decodeState` callbacks that encrypt and authenticate
  the entire record before persistence;
- a non-extractable, namespace-separated encryption key derived from wallet
  private material or a reviewed secure-key service;
- no `unsafeAllowPlaintext`, `unsafeAllowMemoryFallback`, or public lookup-key
  opt-in.

The note cache must meet the same at-rest encryption standard. A production
adapter can replace `LocalStorageNoteStore`; it must preserve the complete
scan cursor and fail closed on corrupt/decrypt-failed records.

## Account, Tab, And Lease Ownership

On account, wallet, or chain-profile change:

1. Stop every heartbeat and clear only in-memory session material.
2. Close the old namespace; never copy notes, leases, or prepared data into
   the new scope.
3. Derive a new privacy session and load only its encrypted namespace.
4. Run/continue a scan and nullifier refresh before presenting spendable
   balance.

The in-memory material in step 1 includes UI single-flight guards. Reset those
guards synchronously at invalidation so a hung prior proof, wallet, or relay
request cannot block a new account's action; its stale completion must still
be unable to render or mutate the replacement session.

If a product offers a local account selector or profile-scoped helper view
(such as a relayer balance), bind those reads to the selected account and
profile as applicable. A prior selection or profile's response must not
overwrite the current view or refresh its note list after the scope changes.

Every asynchronous setup, encrypted-store load, scan, nullifier refresh,
receipt watcher, and relay-recovery continuation must be bound to the
privacy-session generation it started with. Recheck that generation after each
awaited boundary before writing browser state or UI. A stale completion must be
discarded; it must not apply the old namespace to the new session or invalidate
the new session as a failed old setup. In particular, a prior account's EVM
receipt callback must not update the new account's transaction status; durable
reservation/transaction recovery handles that former session when it resumes.
This also applies to scan-triggered reservation reconciliation: pass the
session token through every recovery helper and recheck it before caching
reservation records or updating note-reservation UI.
The same rule covers a later EVM receipt or Cosmos execution-failure recovery:
the `Unknown` marker, chain-time and nullifier checks, `ReplanRequired`
transition, and refreshed reservation view must retain the session that
prepared the operation. A receipt callback must stop if that session changed;
it must not reconcile or render against the newly connected account.
It must also await and contain its result handlers rather than fire-and-forget
them: propagate a session-invalidated result and stop the handler before it
writes success/failure copy or clears a busy state in the replacement session.
The top-level UI handler must treat that sentinel as a no-op rather than show a
toast, modal, or other stale error in the replacement session.
Relay recovery follows the same rule through encrypted-metadata reads, expiry
checks, nullifier queries, reservation transitions, and metadata writes; a
stale continuation must not replace the pending relay list or render it in a
new session. This includes user-invoked pending-handoff actions such as
refreshing its status or restoring a payload for local use, not only startup
recovery.
A prepared relay heartbeat must capture that same session and immutable payload
version when installed, and account/profile invalidation must stop it before
resetting in-memory state. An older in-flight tick must not clear or replace a
new heartbeat's ownership token when it later settles.
An open approval/signing modal must be closed and its pending approval resolved
as rejected during the same transition, so prior recipient, amount, planner,
or failure text is not left in the new session's DOM.

Each tab/worker must use a fresh random `leaseOwner`, for example
`browser-tab:${crypto.randomUUID()}`. It must not share lease tokens with
another tab. Web Locks serialize store mutations, but they do not make stale
worker ownership valid: only the manager holding the live lease may advance a
`Proving` or `ProofReady` operation.

Cosmos transaction preparation also needs one exclusive, cross-tab lock scoped
to the canonical on-chain chain ID and transparent account. It must be shared
across equivalent UI profiles and must not include the local storage epoch.
Public send, deposit, private transfer, direct withdraw, and any
self-merge/helper transaction must share that lock from the
account-sequence/sign-doc read through signing and the durable broadcast
boundary. Preparing a sign doc before acquiring the lock is not sufficient:
another tab can consume the captured sequence while proof or wallet work is in
progress. In local-test mode, read and compare the current genesis/storage epoch
inside the lock before sequence preparation, so a stale tab fails closed after a
chain restart. A durable public pending marker or reservation with
`broadcast_in_flight`, `Submitted`, `Unknown`, or a submitted tx hash blocks a
new Cosmos sequence until reconciliation. A relay-only payload that will be
submitted by a different account does not consume the browser wallet's Cosmos
sequence; any local self transaction needed to construct it still does.

The encrypted reservation store alone is not a restart-safe account fence:
after reconnect, transparent send is available before the user recreates the
root-signature session. Therefore every private Cosmos submission also writes
the exact signed tx hash to the non-sensitive account boundary marker
immediately before RPC submission. Public and private sequence preparation must
check this marker without opening the reservation store. Clear its `privacy`
entry only after the chain result is explicitly included (success or failure)
and the matching encrypted reservations have reached their corresponding safe
terminal reconciliation state.

The public send/deposit marker and private Cosmos marker are separate physical
records under the same canonical account transaction lock. A guarded manual
clear may remove only the public record after wallet-history review. It must
never delete or rewrite the private record. If the private record itself cannot
be authenticated or decoded, keep the account boundary fail-closed and direct
the user to the reviewed fresh-state reset procedure; a generic corrupt-state
clear is not proof that the private broadcast never occurred.

## Durable Operation State Machine

The relevant high-level state progression is:

```text
Verified unspent note
  -> Reserved -> Proving -> ProofReady
  -> BroadcastAttempting -> Submitted | Unknown
  -> ConfirmedSpent | ReplanRequired | ManualReview
```

`Reserved` protects inventory. `Proving` and `ProofReady` hold the batch lease.
Every input in one operation must move together; never mark only one note as
broadcasting/submitted. Preserve the heartbeat through proof construction and
the final `Proving -> ProofReady` transition.

Immediately before calling an external wallet, RPC, or relayer, persist
`markBroadcastAttempting` for the operation. Include any available tx hash,
signed-tx hash, or sign-doc hash. Then:

- record `Submitted` only when submission actually occurred;
- record `Unknown` when it may have occurred;
- require tx identity and explicit nullifier evidence before a post-broadcast
  `ReplanRequired` transition;
- transition to `ConfirmedSpent` only through reconciliation with chain-spent
  evidence;
- retain unresolved evidence as `ManualReview`, not as a released note.

The preceding fresh-nullifier preflight is part of the same session-bound
operation. If the privacy session changes while querying or reconciling an
already-spent input, stop before recording a new broadcast attempt or touching
the new session's reservation view.
Use that captured session for every failure branch too: rejected-wallet
bookkeeping, `Unknown`, `ReplanRequired`, and `ManualReview` transitions must
never be applied to the account that replaced the operation's owner.

If a browser crashes after the attempt marker, startup must discover the active
operation, query its known transaction identity, and refresh every input
nullifier. It must not prepare or submit another transaction using the input
until reconciliation reaches a safe terminal state.

When an old/local operation has only `sign_doc_hash` and no queryable submitted
or signed-transaction identity, automatic recovery must keep it in
`ManualReview`: a sign-doc hash cannot prove whether the wallet prompt was
rejected, never opened, or approved just before the browser lost the result. A
product may provide an explicit local-cancellation recovery only after showing
those alternatives and warning that it does not cancel an already-approved
chain transaction. The user must acknowledge that the wallet and explorer show
no submission; the active reviewing account must then re-read the reservation
batch and confirm every input nullifier is unspent. Record that acknowledgement
and transition the operation to `ReplanRequired`, never directly to
`Released`/spendable. This exception does not apply to relay payloads or
handoffs, which retain their separate expiry and reconciliation boundary.

## Relay Withdraw Recovery

Before copying, downloading, QR-encoding, or uploading a relay payload,
first durably renew the complete reservation batch lease through the payload's
on-chain expiry, then durably record `recordRelayHandoff` with the immutable
payload hash. Do not
persist the raw payload/proof merely for convenience. A copied payload remains
valid for the relayer until its on-chain expiry; a local cancel button, tab
close, or lease expiry does not revoke it.

A pending-handoff recovery control is an evidence check, never a forced
release. It may transition an expired handoff to `ReplanRequired` only after
authoritative chain expiry, explicit-unspent input-nullifier results, and
durable no-broadcast or queryable failed/absent broadcast evidence agree. If
the record has neither a queryable broadcast identity nor durable
no-broadcast evidence, retain the lock and explain that the handoff may have
reached a relayer; do not present an enabled control that silently has no
effect.

Persist every handoff recovery as an independently encrypted record keyed by
its canonical payload hash (or an equivalently derived reservation identity).
Enumerate and validate all such records at restart. Cross-tab save/load/clear
uses a payload-scoped Web Lock, and terminal reconciliation clears only the
matching payload record; two tabs preparing disjoint payloads must not overwrite
or account-wide-clear each other's recovery evidence.

Bind that handoff flow to the privacy-session generation as well as the
immutable payload version. Recheck both after every awaited chain, nullifier,
lease, and persistence operation, and once more immediately before exposing
the payload to the clipboard, download, QR encoder, or upload. If the session
changed, stop without exposing the former session's payload or rendering its
handoff result in the new session.

For a same-origin local relayer-submit button, keep the captured session and
immutable payload version through fresh chain/nullifier checks,
`BroadcastAttempting`, submission, result recording, and failure recovery;
do not call `recordRelayHandoff` or extend the lease to payload expiry merely
for that local request. The durable attempt marker is written immediately
before the one local-relayer request, so any later ambiguity retains the lock
for reconciliation. `recordRelayHandoff` and the expiry-length lease remain
mandatory before Copy, download, QR, upload, or any other actual payload
egress. After either value changes, stop before crossing a new external
boundary and do not write its result or error into the new session.

At restart and at expiry, fetch authoritative chain time, check tx evidence,
and check each input nullifier. Release/replan only after the SDK's required
post-broadcast reconciliation evidence. Otherwise preserve the lock for manual
review.

## ClairveilJS 0.3.1 Fresh-State Initialization

ClairveilJS 0.3.1 uses `privacy-fixed-v1` note/disclosure envelopes and the
current V5/V2 payload contracts. Because this WebApp has not been released with
an earlier persistence contract, it supports fresh initialization only. It
does not define a browser-cache or lifecycle-store migration and does not
support an in-place downgrade.

1. Deploy code with the current `clairveil:v0.3.1:*` namespaces before loading
   wallet state.
2. Do not compatibility-decode or migrate an earlier development cache,
   reservation, operation, relay record, recovery artifact, prepared payload,
   or proof.
3. Initialize an empty current namespace, perform a full typed scan, and
   refresh all nullifiers before exposing the balance or planner.
4. Do not upload old development records for migration or support diagnostics.
   The example deletes only its explicitly named legacy plaintext note-cache
   key and requires Reset & Rescan; that deletion is not a migration. All other
   earlier records remain outside the supported input contract.

The checked-in DApp implements this baseline. It stores note state in
`EncryptedLocalStorageNoteStore`, reservation state through encrypted
`createBrowserReservationStore` IndexedDB callbacks with `requireLocks: true`,
and payload-hash-keyed relay recovery metadata in the multi-record,
Web-Locks-required `EncryptedLocalStorageOperationStore`. Batch and EVM
prepared-transaction recovery use the separate, one-record,
Web-Locks-required `EncryptedRecoveryArtifactStore`. Each
record uses an AES-GCM key that Web Crypto derives from current in-memory wallet
material and its namespace; the key is non-extractable and is never stored
alongside the ciphertext. If the required browser storage, IndexedDB, Web
Crypto, or Web Locks is unavailable, the DApp disables privacy setup rather
than falling back to plaintext or memory.

The DApp does not decode an earlier cache or reservation record as current
state. Its v0.3.1 namespaces are a fresh scan boundary, not a compatibility
migration. A product using this example still needs a reviewed key-recovery and
user-data retention policy before it treats browser persistence as a
production wallet.
