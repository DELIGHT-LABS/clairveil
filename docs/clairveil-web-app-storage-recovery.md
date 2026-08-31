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
| Prepared proof/payload and raw recipient/amount | Memory only unless a product has a separately reviewed encrypted recovery design. The example batch-transfer flow uses a dedicated account/profile-scoped AES-GCM checkpoint because `prepareTransferBatch` requires durable prepared-payload and prepared-proof callbacks before broadcast. |
| Relay recovery metadata | Encrypted store; retain only opaque payload hash, reservation IDs/status, tx identity, handoff/submission status, and expiry. Derive its persistence ID from the payload hash or reservation IDs; do not retain a caller-supplied ID or display-only fields. |

Browser `localStorage`, plaintext IndexedDB, an unencrypted export, and an
in-memory fallback are demo/test-only choices. They are not production
fallbacks when encrypted persistence or Web Locks is unavailable.

## Namespace And Encryption Rules

Use separate namespace prefixes for the current persistence epoch:

```text
clairveil:wallet-notes:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:note-reservations:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:relay-withdraw-payloads:v2:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
clairveil:batch-transfer-artifacts:v1:<chain-id>:<profile-scope>:<wallet-kind>:<account-scope>
```

`<profile-scope>` must uniquely identify the validated profile's privacy
identity fields and be replaced when those fields change. `<account-scope>`
must be stable only for the selected account and must not be sent to telemetry.
A product may use an opaque keyed identifier instead of a literal address.
Never share a namespace across profiles, chain IDs, wallet kinds, or
transparent accounts.

The batch-transfer checkpoint stores only the exact canonical prepared payload
and proof required to bind recovery to the reservation operation. Encrypt and
authenticate the complete checkpoint, and never write it to logs, exports,
analytics, or crash reports. Once either durable callback succeeds, a later
failure is a recovery state: keep the reservation locked and block a new batch
until reconciliation reaches `ConfirmedSpent` with matching operation/output
evidence, or an authorized terminal replan/failure transition releases it.
Clear the checkpoint only after that terminal reconciliation.
If inclusion initially remains pending, a later typed note scan reloads this
checkpoint, rechecks every reservation and expected payment output, and
restores the per-item evidence states in the batch review UI. Mixed terminal
states are an atomicity conflict and remain locked for manual review.

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
durably record `recordRelayHandoff` with the immutable payload hash. Do not
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

## ClairveilJS 0.2 Upgrade Procedure

ClairveilJS 0.2 uses `privacy-fixed-v1` note/disclosure envelopes and strict
V5/V2 payload contracts. A pre-0.2 persistence record is untrusted and
incompatible.

1. Deploy code that uses the `v2` namespace prefixes before loading a legacy
   browser record.
2. Do not attempt compatibility decode, reservation migration, or relay proof
   replay.
3. Create an empty current namespace, perform a full typed scan, and refresh
   all nullifiers before exposing the balance or planner.
4. Leave legacy browser records untouched. A v0.2 browser integration must
   neither read, migrate, nor mutate them, including legacy `localStorage`,
   `sessionStorage`, and reservation-database records. Never upload legacy
   records for migration or support diagnostics.

The checked-in DApp implements this baseline. It stores note state in
`EncryptedIndexedDbNoteStore`, reservation state through encrypted
`createBrowserReservationStore` callbacks with `requireLocks: true`, and relay
recovery metadata in an encrypted IndexedDB record. Each record uses an
AES-GCM key that Web Crypto derives from the current in-memory root signature
and its namespace; the key is non-extractable and is never stored alongside
the ciphertext. If IndexedDB, Web Crypto, or Web Locks is unavailable, the
DApp disables privacy setup rather than falling back to plaintext or memory.

The DApp does not read or mutate legacy `localStorage`/`sessionStorage` or
legacy reservation records. Its `v2` namespaces are a fresh scan boundary, not
a compatibility migration. A product using this example still needs a reviewed
key-recovery and user-data retention policy before it treats browser
persistence as a production wallet.
