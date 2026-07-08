# Clairveil Reference Payroll Product Policy Defaults

Korean version: [clairveil-reference-payroll-product-policy-kr.md](clairveil-reference-payroll-product-policy-kr.md)

## Purpose

This document records the default product policies that the repository fixes for the Reference Payroll Product.

These policies are not legal or business requirements that every downstream product must copy verbatim. They are safe defaults intended to reduce implementation ambiguity. Downstream products may override them per tenant, compliance requirement, or UX requirement, but weakening these principles can increase the risk of note double-use, incorrect payment success decisions, and sensitive data exposure.

## Disclosure Defaults

The default user disclosure policy is `all-private` / `none`.

This means the product does not create user-facing selective disclosure by default. It does not disable mandatory audit disclosure. The default operation success evidence uses the audit disclosure digest.

The `transfer-batch` CLI currently supports the same shared disclosure flags as regular `transfer`.

```text
--privacy-policy all-private|amount|to|amount-to|from|amount-from|from-to|amount-from-to
--disclosure-mode none|public|recipient-encrypted
--disclosure-pubkey <hex>
--no-self-view
```

The Reference Payroll Product must preserve the following fields in payroll input, plan items, and operation records.

```text
user_privacy_policy
user_disclosure_mode
user_disclosure_target_pubkey_hex
user_disclosure_target_key_id
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
```

For an `all-private` item, do not store `expected_user_disclosure_digest` because there is no user disclosure. Store the audit disclosure digest in separate expected/evidence fields when it is available.

## User Disclosure Selection Policy

The product default is `all-private` / `none`, but payroll products must not block the other user disclosure options.

The allowed policies match the current transfer policy.

| Policy | Meaning |
| --- | --- |
| `all-private` | no user-facing disclosure |
| `amount` | disclose amount |
| `to` | disclose recipient |
| `amount-to` | disclose amount and recipient |
| `from` | disclose sender |
| `amount-from` | disclose amount and sender |
| `from-to` | disclose sender and recipient |
| `amount-from-to` | disclose amount, sender, and recipient |

The supported user disclosure modes are `none`, `public`, and `recipient-encrypted`.

Recommended defaults:

- Ordinary payroll: `all-private` / `none`
- Audit and accounting: use the audit disclosure digest and the separate audit-key path, not user disclosure
- Employee or external-recipient selective disclosure: `recipient-encrypted`
- Testing, internal validation, or explicitly public payments: `public`

`recipient-encrypted` requires a disclosure public-key registry. Store the key id/version with the operation expected values and account for key rotation.

## Success Decision Principle

Do not mark a payroll/payment operation successful from `nullifier_spent=true` alone.

A spent nullifier means that the note was consumed. The same note may have been consumed first by another conflicting transaction, so an operation is successful only when evidence matches the current operation's expected values.

```text
tx_hash or tx identity
output_commitment
audit_disclosure_digest
recipient_hash
amount_hash
denom
batch_item_index
optional user_disclosure_digest
optional self_view_disclosure_digest
```

The audit disclosure digest is the primary disclosure evidence for operation success decisions. User and self-view disclosure digests are checked separately only when the corresponding expected fields exist.

Comparing the audit disclosure digest for success reconciliation does not require the audit private key. The audit private key is needed to decrypt the disclosure payload for audit review; the reconcile worker only checks that the expected digest matches the digest observed from tx/event evidence.

If the nullifier is spent but matching operation evidence is missing, the note may be treated as `ConfirmedSpent`, but the operation must not be marked `Succeeded`. Move that operation to `ManualReview` or `ConflictSpent`.

## Reconcile And Retry Policy

Never release `Submitted`, `Unknown`, or `ManualReview` notes back to available state by TTL alone.

Default reconcile order:

1. If `tx_hash` exists, query the tx.
2. If the tx succeeded, compare tx events, output commitments, audit disclosure digest, amount evidence, and recipient evidence.
3. If the tx failed, classify the failure reason.
4. If the tx is missing or unclear, query nullifier spent status.
5. If the nullifier is unspent and no tx exists, consider retrying the same operation or reconstructing the tx.
6. If the nullifier is spent but operation evidence is insufficient, move the operation to `ManualReview` or `ConflictSpent`.

RPC timeout or mempool eviction must not immediately create a new tx. First check the existing `tx_hash`, signed tx bytes, and nullifier status.

Retry must be idempotent by `operation_id`. Store the tx bytes, tx hash, sign doc hash, account sequence, and broadcast attempt count for the same operation.

## Note Preparation Policy

The default note preparation policy is approval-based, not auto-prepare.

The reference product uses `AnalyzeNotePreparation` to provide hints for required dummy notes, split/merge work, add-funds actions, and blocked reservation locks. Actual split/merge tx execution should happen only after operator approval or an explicit product policy.

Recommended flow:

```text
payroll import
-> note preparation analysis
-> preparation preview
-> operator approval
-> preparation tx execution
-> rescan/nullifier check
-> payroll plan finalization
```

This default reduces the risk of an incorrect automatic split/merge unexpectedly changing treasury notes. Downstream products with sufficient operational guardrails may enable tenant-level auto-prepare.

## Sensitive Data Policy

Treat the reservation/payroll DB as a privacy-sensitive DB.

Default principles:

- Do not index raw nullifiers directly.
- Use deterministic keyed lookup values such as `nullifier_lookup_key = HMAC(index_key, nullifier)`.
- Store `index_key_id` or `lookup_key_version` to support key rotation.
- Store raw nullifiers, commitments, recipients, amounts, and payroll item mappings with field-level encryption where possible.
- Do not log or emit raw nullifiers, commitments, recipients, or amounts to telemetry.
- Operator UI should show abbreviated values, hashes, and key ids.
- Define retention policy for proof artifacts, disclosure payloads, and reservation details.

## Default Product Settings Summary

| Item | Default |
| --- | --- |
| user disclosure | `all-private` / `none` |
| audit disclosure | stored separately as operation success evidence |
| user/self-view digest | required as success evidence only when the expected field exists |
| note preparation | approval-based |
| retry | idempotent by `operation_id` |
| Submitted/Unknown/ManualReview release | no automatic TTL release |
| sensitive DB | HMAC lookup key, key version, and field-level encryption recommended |
| public disclosure | explicit opt-in or testing only |
