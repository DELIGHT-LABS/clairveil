# Clairveil Reference Payroll Wallet Handoff

Korean version: [clairveil-reference-payroll-wallet-handoff-kr.md](clairveil-reference-payroll-wallet-handoff-kr.md)

## Purpose

This document lists the work the web and mobile wallet teams need to implement in order to integrate with the Reference Payroll Product.

The wallet team's core responsibility is to manage user notes and disclosure keys safely, while making sure payroll/reservation state does not conflict with the normal transfer UX.

## Wallet Team Scope

The wallet team should implement:

```text
reserved note handling
wallet DB migration
batch nullifier sync
disclosure pubkey generation/display/export
payroll incoming note display
note preparation visibility
rescan/recovery flow
privacy-safe logging
```

## Reserved Note Handling

The most important requirement is excluding reserved notes from normal send candidates.

Required policy:

- `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, and `ManualReview` notes are excluded from normal transfer, split, and merge candidates.
- `ConfirmedSpent` notes are displayed as spent.
- `Released` notes may return to available status after backend/reconcile confirmation.
- Users must not be able to manually unlock `Unknown` or `ManualReview` notes.

If the wallet ignores reserved notes, the payroll backend and wallet can spend the same note concurrently and create a nullifier conflict.

## Wallet DB Migration

The wallet DB needs at least the following fields or an equivalent projection.

```text
commitment_hex
nullifier_hex
nullifier_lookup_key
nullifier_lookup_key_id
amount
denom
spent
reservation_id
reservation_status
operation_id
payroll_id
batch_id
last_scan_height
last_scan_sequence
tx_hash
```

Sensitive data should be encrypted whenever possible. Raw nullifiers, commitments, recipients, and amount mappings must not be written to logs.

## Disclosure Public Key UX

Recipient-encrypted user disclosure in the payroll product requires a disclosure public key UX.

Required features:

- display disclosure public key
- copy/export disclosure public key
- show key rotation or regeneration policy
- explain backup/recovery
- validate that users do not submit a key for the wrong network/account

On-chain events should not expose the sender self-view target pubkey. Wallets also must not send static disclosure pubkeys to analytics or ordinary event logs.

## Payroll Incoming Note Display

The wallet should scan payroll-received notes like normal incoming shielded notes.

Recommended UX:

- If backend metadata can identify a payroll payment, show a dedicated label.
- If chain events alone cannot identify it, show it as a normal received note.
- If there is no amount disclosure, display the amount only from the locally decrypted note.
- If disclosure payload verification fails, show a warning.

## Batch Nullifier Sync

Wallets should use the batch nullifier query for spent-state refresh.

```text
POST /clairveil/privacy/v1/nullifiers
```

Requirements:

- chunk to at most 1000 nullifiers per request
- treat missing nullifiers in the response as a safe failure
- preserve cursor rollback/reorg handling
- preserve forced rescan UX

## Note Preparation Visibility

If the Reference Payroll Product provides a note preparation report, the wallet can show preparation state to a user or operator.

Candidate states:

- insufficient spendable notes
- insufficient dummy notes
- payroll cannot run because notes are reserved
- split/merge preparation is needed
- payroll run readiness

When Go's `NotePreparationOperationHint` is available, the wallet or admin console can show it directly as a preparation action candidate. For example, `make-dummy` means zero dummy note preparation, `split-merge` means note restructuring, `add-funds` means treasury funding shortage, and `resolve-reservation-lock` means an existing reservation needs review.

Whether this screen belongs in the wallet or the admin console is a product decision.

## Privacy-Safe Logging

Wallets must not send the following values to ordinary logs, analytics, or crash reports.

- raw nullifier
- raw commitment
- recipient address and amount mapping
- payroll item and employee mapping
- raw disclosure payload
- disclosure private key
- viewing key
- root seed

If the UI needs to show a nullifier or commitment, use a shortened value.

## Completion Criteria

- Reserved notes are excluded from normal transfer/split/merge candidates.
- Disclosure public key can be displayed/exported safely.
- Payroll incoming notes are scanned and displayed.
- Batch nullifier sync works.
- Reservation/spent state remains consistent after forced rescan.
- Sensitive logging restrictions are applied.

## Session 3A Core / Session 3B Wallet Boundary

The wallet must migrate to active set `privacy-note-v1` and canonical `privacy-fixed-v1` note, disclosure, and typed-envelope bytes. This is not cache-compatible: use fresh genesis, delete old note/reservation/scan/proof state and development artifacts, regenerate, and fully rescan. Never accept raw ciphertext or a legacy JSON note as a compatibility fallback. Resolve denomination labels only through authoritative `AssetRegistryV1`; quarantine an unknown or inconsistent asset ID.

Persist the unified cursor `(height, global_sequence, output_index)` atomically and derive spend paths from a snapshot for exactly the selected root. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical lookup can disclose the treasury's timing and state interest, so retain that privacy warning in provider selection. A canceled remote proving request does not guarantee that the in-process solver stopped; keep the affected reservations until job/chain reconciliation. Automatic prover failover stays disabled.

Do not treat Session 3A chain-core support as wallet support. The existing batch-oriented payroll UI still submits current 2x2 operations. Session 3B UI/scanner work must use the production 12 public inputs (`MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`) and remain feature-gated until its builder, prover route, decryption, submission, and reconciliation flow passes end to end.
