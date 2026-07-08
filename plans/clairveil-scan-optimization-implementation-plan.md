# Clairveil Note Scan Optimization Implementation Plan

Korean version: [clairveil-scan-optimization-implementation-plan-kr.md](clairveil-scan-optimization-implementation-plan-kr.md)

This document records the scope implemented in the Clairveil core repository to reduce note scan cost. The goal is to reduce the event fetch cost and local decrypt cost that web wallets and mobile wallets feel when they synchronize a large event range.

## Baseline Before This Work

- The Merkle tree depth is 32, so the maximum leaf count is about 4.29 billion.
- The previous scan path fetched deposit and transfer events through `PrivacyEvents(after_height, page, limit, event_types)`.
- The existing event query used offset pagination and capped `limit` at 200.
- SDK wallet scan tried to decrypt both transfer outputs, `cipher_text_1` and `cipher_text_2`.
- Transfer decrypt first used the view key and then tried a spend-key fallback on failure.
- The previous proto/event shape had no scan tag or view tag.

## Implementation Status

Items 1-5 in this document are included in the current implementation. Item 6, server-filterable hints/FMD, and item 7, proof-bound tags, are intentionally excluded.

- Core queries provide a `ScanEvents` cursor projection and a `CheckNullifiers` batch spent query.
- The SDK scan service prefers `ScanEvents` and `CheckNullifiers` when available, while keeping the previous provider path as a fallback.
- Transfer payloads use `v3`, and `view_tag_hexes` is included in the prepared payload hash.
- `MsgTransfer.view_tags` and the transfer event attributes `view_tag_1` and `view_tag_2` are aligned with encrypted outputs by output index.
- View tags are not circuit-bound yet, so wallets must use them only as local decrypt optimization hints.

## Design Philosophy

1. Core must not learn private keys, root seeds, or plaintext notes.
2. The prover handles proof payloads only. It does not scan notes on behalf of the wallet.
3. The chain node/core provides a more efficient public event feed.
4. The wallet decides note ownership with its own keys and maintains a wallet-owned local cache; production clients must add their encrypted storage policy outside the core.
5. Server-filterable hints change the privacy model, so they are not part of the default core scope.
6. The per-note view tag is implemented as an untrusted performance hint for now, but its format is fixed so it can later be upgraded to a proof-bound tag.

## Implemented Scope

### 1. Cursor/Projection Scan Event Query

Add a new query:

```text
ScanEvents(after_height, after_sequence, limit, event_types)
```

Requirements:

- Use a `(height, sequence)` keyset cursor instead of offset pages.
- Return only the projection needed by wallet scan.
- Deposit outputs include `commitment` and `encrypted_note`.
- Transfer outputs include `commitment`, `cipher_text`, and `view_tag` with the output index.
- The response includes `next_height`, `next_sequence`, the effective `limit`, `has_more`, `scan_format_version`, and `view_tag_version`.
- `limit` bounds the cursor page budget, not only the number of returned events. A page that only contains filtered-out events can return `events=[]` with `has_more=true`, so clients must continue with `next_height` and `next_sequence`.
- Keep the previous `PrivacyEvents` query as a compatibility/reference query.

Expected effect:

- Removes offset-page skip cost.
- Reduces RPC request count and payload decode cost.
- Gives mobile/web wallets a resumable cursor.

### 2. SDK Cursor Scan

Switch SDK wallet scan to prefer the new `ScanEvents` query.

Requirements:

- Add `last_sequence` to the wallet cache.
- Use cursor scan when the provider supports `ScanEvents`.
- Fall back to the previous `SearchPrivacyTxs` path when the provider does not support it.
- Reset `last_height`, `last_sequence`, and notes together on rollback/reset.

Expected effect:

- Gives both full rescan and incremental sync stable resume semantics.
- Lets downstream mobile/web wallets implement the same cursor model.

### 3. Batch Nullifier Query

Add a query that checks the spent state of many nullifiers in one request.

Requirements:

- Add `CheckNullifiers(repeated nullifier)`.
- SDK scan prefers the batch query provider when available.
- Keep the individual `CheckNullifier` query.

Expected effect:

- Reduces RPC round trips when the wallet has many notes.

### 4. Privacy-Safe Per-Note View Tag

Add a 2-byte `view_tag` to each transfer output. In this implementation the tag is not bound to the proof/circuit.

Requirements:

- `MsgTransfer.view_tags` is a one-to-one array with `new_commitments` and `cipher_texts`.
- The current transfer output count is 2, so the array length must be 2.
- Each tag is exactly 2 bytes.
- Events record `view_tag_1` and `view_tag_2`.
- The scan projection includes the output-level `view_tag`.
- Safe default wallet scan must full trial decrypt even on tag mismatch. Since the tag is not proof/circuit-bound yet, a bad hint must not be able to hide an owned note.
- Skipping mismatch outputs is allowed only as an explicit fast mode chosen by a client that also has recovery/rescan policy.
- If the tag is missing or malformed, the wallet uses the existing trial decrypt recovery/fallback path.
- Forced rescan or rollback recovery ignores tag mismatches and runs full trial decrypt.

Tag derivation v1:

```text
shared_point = ECDH(ephemeral_secret, receiver_view_pubkey)
view_tag_full = MiMC(
  "clairveil.view_tag.v1",
  shared_point.x,
  shared_point.y,
  output_commitment,
  output_index
)
view_tag = first_2_bytes(canonical_32_bytes(view_tag_full))
```

Design reasons:

- The tag is derived from an ephemeral shared point, so it is not a stable recipient fingerprint.
- Including the commitment and output index reduces output swap/reuse ambiguity.
- MiMC derivation makes it easier to upgrade the same value to a proof-bound tag later.
- In this version the tag is only an untrusted hint. Default sync must not finalize the cursor while discarding an owned note only because the tag mismatched.

Expected effect:

- It does not reduce event fetch count.
- In explicit fast mode, it can reduce the local failed-decrypt path cost for non-owned transfer outputs.
- In safe default mode, the main value is to establish a wire format that can later be upgraded to a proof-bound tag.

### 5. Minimal Versioning

Because no stable public scan API has been released yet, this work does not add a migration layer. Instead it adds format/version markers.

Requirements:

- The scan projection response includes `scan_format_version = 1`.
- Responses with view tags include `view_tag_version = 1`.
- Conformance fixtures, schema, and docs follow the new format.

## Intentionally Excluded Scope

### 6. Server-Filterable Hint/FMD

The node or indexer does not filter down to "my candidate notes" by tag in this scope.

Reason for exclusion:

- It can greatly reduce event fetch, but it changes query metadata privacy.
- Static tag or FMD policy needs product/threat-model decisions first.
- It is not suitable as a silent default privacy-profile change.

### 7. Proof-Bound Tag

Binding the view tag into circuit public inputs is excluded from this scope.

Reason for exclusion:

- It changes circuits, proving keys, verifying keys, payloads, and fixtures.
- It is not immediately required for local decrypt optimization.

However, the derivation and field layout in item 4 are designed so they can be upgraded to a proof-bound tag later.

## Work Units

1. Add this plan document.
2. Add cursor scan event and batch nullifier queries to proto/query/keeper.
3. Extend the SDK provider and scan service for cursor/batch queries.
4. Add view tag crypto/encryption/scan paths and update transfer message/events.
5. Update conformance fixtures, schema, and docs to the new scan format.
6. Run the full test suite and fix failures.
7. Run the final review/fix loop over the full diff until no active findings remain.

## Verification Plan

- `make proto`
- `go test ./x/privacy/...`
- `go test ./...`
- `make build` if needed
- final review/fix loop
