# Clairveil Reference Payroll Product Guide

Korean version: [clairveil-reference-payroll-product-kr.md](clairveil-reference-payroll-product-kr.md)

## Purpose

This document defines the Reference Payroll Product baseline for Clairveil bulk-transfer phase 1.5.

The Reference Payroll Product is not required by the core protocol. It plays the same companion/reference role as `clairveil-proverd`: downstream developers can use it directly, fork it, or use it as a production product foundation.

Goals:

- Reduce the gap between the core module and an actual bulk-transfer/payroll product.
- Provide a baseline workflow for payroll import, planning, note preparation, reservation, proving, broadcast, reconcile, and reporting.
- Keep the sample product high enough quality to be useful as a real product foundation.

## Current Scope

The repository currently provides the following foundation.

| Area | Location |
| --- | --- |
| note reservation | `x/privacy/client/sdk/reservation` |
| payroll plan/control-plane | `x/privacy/client/sdk/payroll` |
| disclosure policy helper | `x/privacy/client/sdk/payroll/disclosure.go` |
| disclosure key registry contract | `x/privacy/client/sdk/payroll/disclosure_registry.go` |
| note preparation analyzer | `x/privacy/client/sdk/payroll/note_preparation.go` |
| file-backed reference artifact store | `x/privacy/client/sdk/payroll/file_artifact_store.go` |
| SQL reservation store contract | `x/privacy/client/sdk/reservation/sql_store.go` |
| reference payroll CLI | `cmd/clairveil-payroll` |
| reference payroll daemon | `cmd/clairveil-payrolld`, `x/privacy/client/sdk/payroll/reference_daemon.go`, `x/privacy/client/sdk/payroll/live_daemon.go` |
| repo-local demo product | `examples/reference-payroll/payroll-demo.json`, `scripts/reference-payroll-demo.sh` |
| live localnet tutorial | `scripts/reference-payroll-live-localnet.sh`, `docs/clairveil-reference-payroll-live-localnet-tutorial.md` |
| large-scale rehearsal | `scripts/reference-payroll-rehearsal.sh`, `docs/clairveil-reference-payroll-rehearsal-kr.md` |
| proof/broadcast/reconcile worker | `x/privacy/client/sdk/payroll/proof_queue.go`, `broadcast_queue.go`, `batch_broadcaster.go`, `reconcile_worker.go` |
| multi-message chunking | `x/privacy/client/sdk/payroll/chunker.go` |
| prover pool | `x/privacy/client/sdk/payroll/prover_pool.go` |
| readiness/benchmark | `scripts/privacy-bulk-readiness-check.sh`, `cmd/clairveil-bulktransferbench` |

## Reference Workflow

Recommended workflow:

```text
1. payroll input import
2. recipient address and disclosure policy validation
3. note preparation analysis
4. operator approval or auto-prepare
5. payroll plan creation
6. plan confirmation and note reservation
7. proof worker execution
8. proof result chunking
9. multi-message broadcast
10. tx/nullifier/evidence reconcile
11. status/report export
```

## Product Assumptions

Phase 1.5 preserves the existing `MsgTransfer` and existing join-split circuit. Therefore each employee payment still needs one transfer proof.

The Reference Payroll Product does not reduce proof count. Its purpose is to operate bulk transfer safely under the current protocol. Reducing proof count belongs to the phase 2 N-output batch circuit.

## User Disclosure Policy

The default user disclosure policy is `all-private` / `none`. This does not disable mandatory audit disclosure; it only means user-facing disclosure is off by default.

The `transfer-batch` CLI supports the same shared disclosure flags as normal `transfer`. A payroll product should therefore be able to represent not only `all-private`, but also `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, and `amount-from-to`.

The Reference Payroll Product exposes `PayrollDisclosurePolicy`.

```text
user_privacy_policy
user_disclosure_mode
user_disclosure_target_pubkey_hex
user_disclosure_target_key_id
expected_user_disclosure_digest
expected_audit_disclosure_digest
expected_self_view_disclosure_digest
```

Product implementations should validate policy during planning and convert it into the existing transfer SDK disclosure config during proof/payload generation.

Default product policy and operation success rules are in [clairveil-reference-payroll-product-policy.md](clairveil-reference-payroll-product-policy.md).

## Disclosure Key Registry

Supporting `recipient-encrypted` user disclosure requires a disclosure public key registry.

Reference scopes:

```text
employee
company
auditor
external
```

Each entry has at least:

```text
key_id
scope
subject_id
public_key_hex
version
active
```

The production registry belongs in the product repository, but key format and lookup meaning should follow the `DisclosureKeyRegistry` contract.

## Note Preparation

The current 2-input transfer circuit needs two input notes per transfer.

If prepared notes are missing, the payroll run fails. Products should run note preparation analysis before execution.

`AnalyzeNotePreparation` reports:

- spendable note count
- reserved/spent note count
- zero dummy note count
- ready item count
- blocked item count
- required dummy note or split/merge recommendations
- operation hints that the product layer can turn into an execution plan
- expected message chunk count

This helper does not automatically execute split/merge transactions. The product layer should read the report and implement operator approval or auto-prepare flow.

`operation_hints` make recommendations easier to convert into product-level actions. For example, insufficient dummy notes produce `make-dummy`; incompatible note pairs produce `split-merge`; insufficient spendable total produces `add-funds`; and active reservations that excluded notes produce `resolve-reservation-lock`.

## Reference CLI

The reference CLI exposes payroll workflow steps as separate commands.

```text
validate
build-input-from-notes
prepare-notes
plan
run
status
scan-evidence
reconcile
settle-transfer-batch
seed-localnet-notes
export-report
```

`run`, `scan-evidence`, and `reconcile` provide the durable control-plane surface: plan confirmation, durable reservation/operation state, tx event/nullifier evidence scanning, and evidence-based reconciliation.

`build-input-from-notes` converts treasury notes scanned with `list-notes --json` into `treasury_notes` in the payroll input.

`settle-transfer-batch` validates the actual `transfer-batch` tx result and recipient note-scan delta, then settles durable reservation state. In the live localnet tutorial it bridges real chain tx output to the payroll final report.

`seed-localnet-notes` is a localnet rehearsal helper. It writes payroll amount notes and zero dummy notes into localnet genesis commitments and Alice's wallet cache to reduce preparation time for large restart/retry rehearsals. It is not production note preparation; staging/testnet should use real deposit, split/merge, and approval-based preparation flows.

`clairveil-payrolld` reads the same durable state and lets operators experience the flow end-to-end inside this repository. `simulated` mode creates deterministic simulated proof/tx/evidence instead of live chain proofs and broadcasts, validating the `Reserved -> Proving -> ProofReady -> Submitted -> ConfirmedSpent` flow.

`live` mode uses the SDK `LiveDaemon` state machine. Proof generation, tx broadcast, and scanner evidence collection are injected through `LiveOperationExecutor`. The CLI reference executor reads `-tx-query` on every tick and reconciles `Submitted` or `Unknown` operations. Production products should connect real prover, tx broadcaster, and tx/nullifier scanner implementations to the same state machine.

## Input JSON

`validate`, `prepare-notes`, and `plan` use the same input JSON. It contains payroll items and treasury note inventory.

```json
{
  "company_id": "company-a",
  "payroll_id": "payroll-2026-07",
  "batch_id": "run-001",
  "denom": "uclair",
  "max_messages_per_tx": 20,
  "default_disclosure_policy": {
    "user_privacy_policy": "all-private",
    "user_disclosure_mode": "none"
  },
  "items": [
    {
      "item_id": "item-001",
      "employee_id": "employee-001",
      "recipient_address": "clairs1...",
      "amount": "70"
    }
  ],
  "treasury_notes": [
    {
      "note_id": "note-large",
      "owner_key_id": "treasury-key",
      "nullifier_lookup_key": "lookup-note-large",
      "denom": "uclair",
      "amount": "100"
    },
    {
      "note_id": "note-zero",
      "owner_key_id": "treasury-key",
      "nullifier_lookup_key": "lookup-note-zero",
      "denom": "uclair",
      "amount": "0"
    }
  ]
}
```

## `clairveil-payroll validate`

Validates recipient address, amount, denom, duplicate rows, and disclosure policy, and outputs a note preparation summary.

```bash
clairveil-payroll validate \
  -input payroll-prepare.json \
  -out payroll-validation.json
```

The output contains `valid`, `errors`, `warnings`, and `note_preparation`. If the input itself is valid but notes are insufficiently prepared, preparation items appear in `warnings`.

## `clairveil-payroll prepare-notes`

Runs the note preparation analyzer.

```bash
clairveil-payroll prepare-notes \
  -input payroll-prepare.json \
  -out payroll-prepare-report.json
```

With `-store-dir`, the report is also saved to the file-backed reference artifact store.

```bash
clairveil-payroll prepare-notes \
  -input payroll-prepare.json \
  -store-dir .clairveil-payroll
```

The report provides ready/blocked item counts, dummy note shortage, reserved-note exclusions, split/merge recommendations, and operation hints as JSON.

## `clairveil-payroll plan`

Creates a draft payroll plan from prepared note inventory and payroll input.

```bash
clairveil-payroll plan \
  -input payroll-prepare.json \
  -out payroll-plan.json
```

The plan contains per-item `operation_id`, `chunk_id`, selected input notes, expected recipient/amount hash, and expected disclosure digest. This is still a draft: note reservations are not committed to durable state yet. In the repo-local flow, `clairveil-payroll run` confirms it into `DurableFileStore` with `Service.ConfirmPlan` semantics. In production, the same contract is performed by `SQLStore` or the product scheduler service.

With `-store-dir`, the plan is also saved to the file-backed reference artifact store.

```bash
clairveil-payroll plan \
  -input payroll-prepare.json \
  -store-dir .clairveil-payroll
```

## `clairveil-payroll run`

Confirms a plan JSON into durable reservation state. This marks each input note as `Reserved` and stores `PayrollOperation` records.

```bash
clairveil-payroll run \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-confirmed-plan.json
```

`run` is idempotent for the same plan: rerunning it reads existing reservations and outputs the confirmed plan again. This command does not generate proofs or broadcast chain transactions. That work is performed by the live localnet tutorial's `transfer-batch`/`settle-transfer-batch` path or by production proof/broadcast workers.

## `clairveil-payroll status`

Reads a plan JSON or durable reservation state and outputs status counts.

```bash
clairveil-payroll status \
  -plan payroll-plan.json \
  -out payroll-status.json
```

```bash
clairveil-payroll status \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-state-status.json
```

Plan-based output counts `Planned`, `Reserved`, `Submitted`, `Confirmed`, `Failed`, `ReplanRequired`, and `ManualReview` items. State-based output counts reservation status and operation status separately.

## `clairveil-payroll reconcile`

Applies evidence JSON to durable reservation/operation state.

```bash
clairveil-payroll reconcile \
  -state .clairveil-payroll/reservation-state.json \
  -evidence reconcile-evidence.json \
  -out reconcile-report.json
```

Evidence JSON shape:

```json
{
  "evidence": [
    {
      "reservation_id": "operation-a:note:note-large",
      "tx_hash": "ABC123",
      "output_commitment": "commitment-a",
      "disclosure_digest": "digest-a",
      "audit_disclosure_digest": "digest-a",
      "user_disclosure_digest": "user-digest-a",
      "self_view_disclosure_digest": "self-view-digest-a",
      "recipient_hash": "recipient-hash-a",
      "amount_hash": "amount-hash-a",
      "denom": "uclair",
      "batch_item_index": 0,
      "batch_item_index_known": true,
      "nullifier_spent": true,
      "tx_succeeded": true
    }
  ]
}
```

`nullifier_spent=true` alone does not make the operation successful. The stored operation's tx identity, output commitment, audit disclosure digest, recipient hash, amount hash, denom, and batch item index must match. User/self-view disclosure digests are checked separately when expected fields exist. Mismatch or insufficient evidence leaves the operation in review/conflict status.

## `clairveil-payroll export-report`

Outputs item-level report JSON for the company customer or operator.

```bash
clairveil-payroll export-report \
  -plan payroll-plan.json \
  -out payroll-report.json
```

If durable reservation state is provided, operation results from the state are reflected into the report.

```bash
clairveil-payroll export-report \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-report.json
```

The output includes a plan summary and per-item `item_id`, `employee_id`, `operation_id`, `chunk_id`, `status`, `amount`, `denom`, `failure_reason`, and `retry_count`.

## Reference Payroll Daemon

`clairveil-payrolld` is the scheduler/daemon surface of the reference payroll product.

```bash
clairveil-payrolld \
  -state .clairveil-payroll/reservation-state.json \
  -once \
  -out .clairveil-payroll/payrolld-report.json
```

Important flags:

| flag | Meaning |
| --- | --- |
| `-state` | durable reservation state JSON path created by `clairveil-payroll run` |
| `-mode` | `simulated` or `live` |
| `-plan` | payroll plan JSON path used by `live` mode to compare evidence with expected values |
| `-tx-query` | `clairveild query tx --output json` result or `TxObservation` JSON path for `live` mode |
| `-nullifiers` | optional nullifier status JSON path for `live` mode |
| `-once` | run one scheduler tick and exit |
| `-interval` | repeat interval when `-once=false` |
| `-lease-owner` | reservation lease owner value |
| `-lease-ttl` | worker lease TTL |
| `-max-operations` | maximum operations per tick; `0` means unlimited |
| `-out` | report JSON path for `-once`; stdout when empty |

`simulated` mode is for product rehearsal. It does not make live proofs or send chain txs. Instead it executes this state flow against repo-local state.

```text
select Reserved operation
-> acquire lease
-> transition to Proving
-> store simulated proof artifact expected value
-> transition to ProofReady
-> store simulated tx metadata
-> transition to Submitted
-> reconcile with matching simulated evidence
-> ConfirmedSpent / Succeeded
```

This daemon lets operators exercise the payroll state model and final reporting even without a production DB, scheduler, scanner, or admin UI.

`live` mode is a long-running state machine where proof, broadcast, and scan are injected through `LiveOperationExecutor`. The CLI reference executor provides minimal live wiring by reading `-tx-query` on every tick and reconciling submitted/unknown operations. Production products should connect real provers, tx broadcasters, and tx/nullifier scanners to the same state machine.

## Repo-local Demo Product

The minimum demo product is available as:

```bash
make reference-payroll-demo
```

It runs:

```text
clairveil-payroll validate
clairveil-payroll prepare-notes
clairveil-payroll plan
clairveil-payroll run
clairveil-payroll status
clairveil-payrolld -once
clairveil-payroll status
clairveil-payroll export-report -state
```

Default input is `examples/reference-payroll/payroll-demo.json`; outputs are under `tmp/reference-payroll-demo/`.

Key outputs:

| File | Meaning |
| --- | --- |
| `validation.json` | input validation and note preparation summary |
| `note-preparation.json` | prepared notes and missing dummy/split/merge hints |
| `plan.json` | draft payroll plan |
| `confirmed-plan.json` | plan confirmed into durable state |
| `reservation-state.json` | reservation/operation durable state |
| `payrolld-report.json` | daemon tick report |
| `status-after-daemon.json` | state summary after daemon run |
| `final-report.json` | item status report for company customer/operator |

Success means every reservation is `ConfirmedSpent`, every operation is `Succeeded`, and `final-report.json` has payroll status `Confirmed`.

## Live Localnet Tutorial

The repository also provides a target that runs the payroll flow on an actual localnet.

```bash
make reference-payroll-live-localnet
```

It performs:

```text
localnet init/start
-> Alice treasury note deposit
-> Alice list-notes scan
-> payroll input generation
-> validate / prepare-notes / plan / run
-> idempotency check by rerunning run
-> actual transfer-batch broadcast
-> Bob recipient note scan
-> settle-transfer-batch
-> final report export
```

Success means `payroll-status-after-settle.json` shows every reservation as `ConfirmedSpent`, every operation as `Succeeded`, and `payroll-final-report.json` has payroll status `Confirmed`. If `PAYROLL_CHUNK_SIZE` is set, the run uses multiple `transfer-batch` txs; each chunk is settled with `settle-transfer-batch -item-start -item-limit`.

Detailed steps: [clairveil-reference-payroll-live-localnet-tutorial.md](clairveil-reference-payroll-live-localnet-tutorial.md).

## File Artifact Store

The reference product separates `FileArtifactStore` for plan/report artifacts from `DurableFileStore` for reservation/operation state.

`FileArtifactStore` preserves plans and reports for local/test/sample products.

Storage groups:

```text
plans
plan-reports
note-preparation-reports
disclosure-keys
```

Files are created with `0600` and directories with `0700`. This store may contain sensitive data, so production should replace it with encrypted DB or secret storage.

## Durable Reservation State Store

`x/privacy/client/sdk/reservation.DurableFileStore` is a durable reference adapter satisfying `reservation.Store`. State transitions, active reservation uniqueness, compare-and-set, lease/heartbeat, and operation evidence updates use the same contract as the memory store; each mutation atomically writes a JSON snapshot.

Example:

```bash
clairveil-payroll run \
  -plan payroll-plan.json \
  -state .clairveil-payroll/reservation-state.json \
  -out payroll-confirmed-plan.json
```

This adapter is a repo-local production-style adapter for restart/rerun rehearsal and reference product use. PostgreSQL, MySQL, or cloud secret-backed DB implementations must preserve the same `reservation.Store` meaning and state transition rules.

`x/privacy/client/sdk/reservation.SQLStore` is a `database/sql` based reference adapter. The repository does not pin a DB driver; products inject `*sql.DB` using PostgreSQL or SQLite drivers. Provided schemas include:

- active reservation partial unique index for `owner_key_id + nullifier_lookup_key`
- reservation/operation status index
- operation link index
- transaction-backed single-writer lock row
- JSON payload preservation
- CAS, lease, heartbeat, and reconcile semantics matching `reservation.Store`

Schema strings are available through `reservation.PostgreSQLSchema()` and `reservation.SQLiteSchema()`. This adapter is a reference transaction-backed store. Multi-tenant production should add tenant partitioning, field-level encryption, raw-nullifier avoidance, connection pool policy, migrations, and row-lock strategy according to product DB policy.

## Completion Criteria

Repo-level completion criteria for Reference Payroll Product phase 1.5:

- Phase 1 readiness checks pass.
- Disclosure policy and key registry contracts are provided.
- Note preparation analyzer is provided.
- File-backed reference artifact store is provided.
- Durable reservation state store is provided.
- `clairveil-payrolld` simulated/live reference daemon is provided.
- `make reference-payroll-demo` runs a repo-local end-to-end payroll demo.
- `make reference-payroll-live-localnet` runs the actual localnet payroll transfer-batch tutorial.
- `clairveil-payroll validate`, `build-input-from-notes`, `prepare-notes`, `plan`, `run`, `status`, `scan-evidence`, `reconcile`, `settle-transfer-batch`, `seed-localnet-notes`, and `export-report` are provided.
- `transfer-batch` CLI supports the same shared disclosure options as normal transfer.
- JS SDK handoff document is provided.
- Wallet handoff document is provided.
- Downstream teams have a baseline for assembling payroll workflow.

## Remaining Productization Work

This repository does not complete:

- managed production DB deployment and tenant-specific schema hardening
- production-grade live scanner/reconcile daemon deployment and tenant hardening
- admin UI
- JS SDK implementation
- web/mobile wallet implementation
- customer-specific payroll policy decisions
- staging/production rehearsal execution

## Repo-local Rehearsal Record

As of 2026-07-08, the repo-local 1,000-item restart/retry rehearsal passed with `PAYROLL_SEED_NOTES=1` localnet seed mode. That mode preloads payroll notes into localnet genesis and Alice's wallet cache to reduce deposit preparation time. After seeding, payroll plan, reservation, actual Groth16 proofs, actual `transfer-batch` tx, recipient scan, settle, and final report export still run. Seed mode is not production note preparation; production/staging should separately validate real deposit, split/merge, and approval-based preparation flows.
