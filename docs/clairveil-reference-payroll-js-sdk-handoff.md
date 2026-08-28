# Clairveil Reference Payroll JS SDK Handoff

Korean version: [clairveil-reference-payroll-js-sdk-handoff-kr.md](clairveil-reference-payroll-js-sdk-handoff-kr.md)

## Purpose

This document lists the work the JS SDK team needs to implement in order to support the Reference Payroll Product.

The core repository provides Go reference types, helpers, fixtures, and documentation. The JS SDK team should translate those contracts into JS/TS APIs that downstream wallets and payroll products can use.

## Implementation Scope

The JS SDK team owns the following surface.

```text
reservation type
payroll input/plan/item type
disclosure policy type
disclosure key registry client/helper
note inventory and preparation helper binding
batch nullifier query
prepared transfer/prover client
expected evidence helper
fixture/conformance CI
```

## Required Type Mapping

Go reference locations:

```text
x/privacy/client/sdk/payroll/types.go
x/privacy/client/sdk/payroll/disclosure.go
x/privacy/client/sdk/payroll/disclosure_registry.go
x/privacy/client/sdk/payroll/note_preparation.go
x/privacy/client/sdk/reservation/types.go
```

The JS SDK should expose at least the following types.

```text
PayrollInput
PayrollItemInput
PayrollDisclosurePolicy
PayrollPlan
PayrollPlanItem
TreasuryNote
NotePreparationReport
NotePreparationOperationHint
DisclosureKeyEntry
NoteReservation
PayrollOperation
```

## Reservation Requirements

The JS SDK must interpret reservation states with the same meaning as the Go reference implementation.

Required behavior:

- Exclude active reserved notes from normal transfer, split, and merge candidate sets.
- Allow only one active reservation for each `owner_key_id + nullifier_lookup_key` pair.
- Preserve compare-and-set semantics for state transitions.
- Require lease tokens for worker-owned state transitions.
- Never release `Submitted`, `Unknown`, or `ManualReview` notes back to available status by TTL alone.

## Disclosure Policy Requirements

The JS SDK must represent and validate `PayrollDisclosurePolicy`.

Validation rules:

- `all-private` policy only allows `none` mode.
- `all-private` policy must not include a user disclosure target pubkey.
- Non-private policies use `public` or `recipient-encrypted` mode.
- `recipient-encrypted` mode requires a 32-byte compressed disclosure pubkey hex string.
- Expected disclosure digests must be canonical 32-byte hex strings.

## Disclosure Key Registry Requirements

The JS SDK must be able to validate disclosure key entries received from the product backend or wallet.

Required fields:

```text
key_id
scope
subject_id
public_key_hex
version
active
```

Supported scopes:

```text
employee
company
auditor
external
```

The JS SDK must not send raw keys to analytics or ordinary logs.

## Note Preparation Requirements

Before payroll execution, the JS SDK should either compute note preparation status or validate the backend-provided result.

Minimum API:

```text
analyzeNotePreparation(input, treasuryNotes, policy)
```

The result should include:

- ready item count
- blocked item count
- spendable note count
- reserved/spent note count
- zero dummy available/required
- selected note IDs
- recommendations
- operation hints

The JS SDK should provide a product/UI signal that prevents a payroll run from blindly continuing when note preparation is insufficient.

`operation_hints` should carry the same meaning as Go's `NotePreparationOperationHint`. A product UI or backend scheduler can use this value to show candidate preparation actions such as `make-dummy`, `split-merge`, `add-funds`, and `resolve-reservation-lock`, or route them into an approval flow.

## File Artifact Store Notes

The Go reference provides `FileArtifactStore`, but the JS SDK does not have to implement the same file layout. If it supports local sample products or CLI interop, it should preserve the meaning of these artifact groups.

```text
plans
plan-reports
note-preparation-reports
disclosure-keys
```

These files may contain payroll items, recipients, amounts, selected notes, and disclosure keys, so local storage must treat them as sensitive data.

## Provider / Query Requirements

The JS SDK provider should support:

```text
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/circuit_config
```

Large nullifier checks should use the POST body by default and be chunked to at most 1000 nullifiers per request.

## Prover Integration

The JS SDK should support one of the following:

- browser/local prover
- remote `clairveil-proverd`
- product backend prover adapter

When using a remote prover, the minimum route is:

```text
POST /v1/prover/transfer
```

Request and response shapes should match the Go provertransport fixtures.

## Expected Evidence

A spent nullifier alone is not enough to mark a payroll item successful.

The JS SDK or backend should connect the following expected values to each operation whenever possible.

```text
expected_output_commitment
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
expected_recipient_hash
expected_amount_hash
expected_denom
batch_item_index
```

## Completion Criteria

- Go fixtures and the JS SDK fixture validator pass in CI.
- Reserved notes are excluded from normal transfer candidates.
- Disclosure policy validation matches the Go reference.
- Product UI can show insufficient note preparation status.
- Batch nullifier queries are chunked.
- Prepared transfer payload and prover response round-trip validation works.

## One-Proof Batch Payroll Reference Boundary

The repository includes a Go one-proof payroll graph that joins many input reservations to one `MsgBatchTransfer` operation and many item-evidence records, plus the batch builder, bounded prover route, broadcast/retry reconciliation, and typed scanner. ClairveilJS 0.3.1 implements the same prepared effect, reservation v3 contract, typed output evidence, and Cosmos/EVM execution boundary. Product UX remains feature-gated by deployment configuration and transport capability.

JS implementations pin `privacy-note-v1` and use canonical `privacy-fixed-v1` note/disclosure/typed-envelope bytes. Resolve every 32-byte asset ID through authoritative `AssetRegistryV1`; payroll denomination configuration and registry results must agree. Initialize from fresh genesis with exact artifacts and empty reservation, note/cursor, and prepared/proof namespaces, then complete a typed rescan.

Use the complete unified scan cursor `(height, global_sequence, output_index)` and same-root Merkle path snapshots. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical path queries can reveal treasury activity, so retain the privacy warning and use privacy-preserving infrastructure where required. A downstream payroll port must preserve the production order `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`, and must not advertise support before its own end-to-end completion.

For prover integration, use the bounded service wrapper and the role-aware lazy artifact loader. Defaults are one in-flight and four queued jobs per circuit plus a positive 8 MiB request limit; zero is invalid. Keep automatic failover off. Client cancellation may leave in-process proving running and holding a reservation/admission slot, so reconcile job and note state before reuse; use isolated worker processes when hard termination is required.
