# Clairveil Bulk Transfer Product Team Handoff

Korean version: [clairveil-bulk-transfer-product-handoff-kr.md](clairveil-bulk-transfer-product-handoff-kr.md)

## Purpose

This document hands off the bulk-transfer/reference-payroll implementation in the current checkout so that it can be carried forward into product and operations implementation. It uses the current batch contract and reference-product surfaces rather than superseded phase-1 plans.

The repository includes reference implementations of note reservation, the payroll control plane, proof/broadcast/reconcile queues, the legacy multi-message transaction and one-proof `MsgBatchTransfer`, a bounded prover, benchmark/readiness harnesses, the reference payroll CLI, simulated/live reference payroll daemons, a repository-local demo product, a live localnet payroll tutorial, a rehearsal harness, a file-backed reference artifact store, a durable reservation state store, and a SQL reference store. The product and operations teams must continue by deciding how to deploy the managed production database, defining tenant operations policies, implementing a production-grade live proof/broadcast/scanner daemon and operator UI, and running an actual 100,000-item rehearsal.

As of 2026-07-13, Session 3B implements the reference Go one-proof `BatchJoinSplit16x32` SDK/prover/scanner/payroll/CLI path and Session 4 validates it as `PUBLICATION_READY_EXPERIMENTAL`. `PRODUCTION_RELEASE_READY` is not approved; formal trusted setup, external audit, production artifacts, and production operations remain outside this handoff's completed boundary.

## What the Repository Provides

- Current batch roadmap: `plans/clairveil-batch-joinsplit-16x32-roadmap-kr.md`
- Normative batch contract/Session 3B surface: `docs/clairveil-batch-joinsplit-16x32.md`, `docs/clairveil-session3b-batch-transfer-handoff.md`
- Note reservation design: `docs/clairveil-note-reservation-design.md`
- Accounting design: `docs/clairveil-privacy-accounting-design-note.md`
- Go reference packages: `x/privacy/client/sdk/batchtransfer`, `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`
- Current one-proof CLI: `transfer-batch-16x32`, `prepare-batch-transfer`, `prove-batch-transfer`, `broadcast-batch-transfer`
- Legacy durable payroll CLI: `clairveil-payroll validate`, `build-input-from-notes`, `prepare-notes`, `plan`, `run`, `status`, `scan-evidence`, `reconcile`, `settle-transfer-batch`, `seed-localnet-notes`, `export-report`
- Reference payroll daemon: `clairveil-payrolld -mode simulated`, `clairveil-payrolld -mode live`
- Repository-local demo product: `make reference-payroll-demo`
- Legacy multi-message localnet tutorial: `make reference-payroll-live-localnet`
- Legacy phase 1 capacity rehearsal: `make reference-payroll-rehearsal`
- Product policy defaults: `docs/clairveil-reference-payroll-product-policy.md`
- File-backed reference artifact store: `x/privacy/client/sdk/payroll.FileArtifactStore`
- Durable reservation state store: `x/privacy/client/sdk/reservation.DurableFileStore`
- SQL reference reservation state store: `x/privacy/client/sdk/reservation.SQLStore`
- Verification entry point: `make privacy-bulk-readiness-check`
- One-proof localnet batch verification: `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`
- Legacy multi-message envelope measurement: `make privacy-transfer-batch-localnet-bench`
- Controlled legacy transfer/withdraw prover-pool measurement: `PROVERD_URLS=url1,url2 make privacy-proverd-scale-bench`

## Areas the Product Team Must Decide and Integrate

### 1. Production Database Deployment

The repository provides `DurableFileStore` and `SQLStore` reference adapters that satisfy the `reservation.Store` contract. `SQLStore` provides PostgreSQL/SQLite schema helpers and a `database/sql`-based contract implementation, but deployment of a managed production database is itself a product and operations concern. To use PostgreSQL, MySQL, or a cloud database in an actual customer environment, preserve the same contract and schema semantics while finalizing tenant partitioning, field-level encryption, migrations, connection pooling, and operational locking policies.

The required implementation includes the following:

- Define the `note_inventory`, `note_reservations`, `payroll_operations`, `payroll_runs`, and `payroll_items` schema families.
- Apply an active-reservation partial unique constraint on `owner_key_id + nullifier_lookup_key`.
- Apply `FOR UPDATE SKIP LOCKED` or an owner-scoped advisory lock during planner note selection.
- Apply compare-and-set to state changes.
- Add worker lease fields: `lease_owner`, `lease_token`, `lease_until`, `last_heartbeat_at`.
- Process lease acquisition, heartbeats, lease clearing, and worker-owned `ProofReady -> Submitted/Unknown` transitions atomically with a single database update/transaction, rather than a `Get -> Update` sequence. Process `ProofReady -> ConfirmedSpent` recovery through a chain-evidence-based compare-and-set/transaction path as well.
- The proof worker must acquire leases only in the `Reserved` state and send heartbeats while generating proofs.
- The broadcast worker must connect a chain nullifier query provider to `NullifierChecker` to block spent nullifiers immediately before transaction submission.
- If a spent nullifier is detected before transaction submission, the SDK broadcast worker returns `SpentNullifierError`, and the scheduler/reconcile layer transitions the item to `ConflictSpent`, `ManualReview`, or `ReplanRequired`. It must not retry the same `ProofReady` work indefinitely without change.
- The `expected_disclosure_digest` field in payroll/payment success evidence is an audit disclosure digest compatibility field. New implementations store `expected_audit_disclosure_digest`, `expected_user_disclosure_digest`, and `expected_self_view_disclosure_digest` separately.
- Determine operation success using the audit disclosure digest as the primary evidence, and verify the user/self-view disclosure digest separately when its expected field is present. Do not use the user disclosure or sender self-view disclosure digest in place of the audit digest.
- Use a deterministic keyed lookup of the form `nullifier_lookup_key = HMAC(index_key, nullifier)`.
- Store sensitive information such as raw nullifiers, commitments, recipients, and amounts in encrypted form.
- Filter payloads, logs, and telemetry so that raw sensitive information is not retained.

The completion criterion is that the selected database/adapter prevents two payroll planners from reserving the same note concurrently, prevents a stale worker from overwriting state, and does not make `Submitted`, `Unknown`, or `ManualReview` notes available based only on TTL expiration.

### 2. Payroll Scheduler / Worker Wiring

The current Session 3B path provides a many-input/one-operation/many-item durable graph and `BatchProofWorker`, `IdempotentBatchBroadcastWorker`, and `BatchReconcileWorker`. `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` connects that graph to one actual `MsgBatchTransfer`, process/node restart, exact stored-byte retry, tx-hash-first reconciliation, typed item evidence, disclosure verification, and separate batch/item status.

The older `clairveil-payroll run` / `scan-evidence` / `reconcile`, `clairveil-payrolld`, and `settle-transfer-batch` surfaces remain the legacy multi-message durable-control-plane tutorial and regression path. They must not be presented as the Session 3B one-proof path. In the product environment, connect the current batch graph to long-running proof workers, broadcast workers, and typed chain scanners while preserving its atomic operation and per-item evidence contracts.

The required implementation includes the following:

- A monthly payroll upload/import flow.
- Creation of a `PayrollRun` per tenant and run locking.
- One-proof planning that atomically reserves 1..16 inputs and binds 1..32 payment/change/padding outputs to one batch operation.
- Durable storage of the private prepared payload, proof, signed transaction bytes, transaction hash, input reservations, and per-output item evidence.
- A legacy simulated operations walkthrough, when needed for control-plane regression. In the legacy reference daemon, `clairveil-payrolld -state ... -once` performs this step.
- Operation of proof-worker and broadcast-worker queues.
- A proof-worker result store that persists the proof/message/payload before marking the batch ready for broadcast.
- Operation-level idempotency: `operation_id`, `sign_doc_hash`, `tx_bytes_hash`, `tx_hash`, `account_sequence`.
- Separate `operation_id`/`reservation_id` values for each replan attempt.
- Failure categorization: insufficient note, reservation conflict, invalid proof, invalid root, gas/sequence, RPC timeout, payload mismatch.
- Do not handle an RPC timeout or mempool eviction by immediately creating a new transaction. First check `tx_hash` and nullifier state through the `Unknown`/`ReconcileUnknown` flow.
- Never rebuild or retry only a subset of an atomic batch output list. Preserve the original operation and reservations through reconciliation; create a new operation only after the prior batch outcome is resolved.
- Update batch chain status and per-item evidence status separately. An item succeeds only when its expected output index, commitment, recipient, amount/asset, and disclosure evidence match.
- Start with the defaults in `docs/clairveil-reference-payroll-product-policy.md` for product policy, then extend them with per-tenant overrides.

The completion criterion is that the current one-proof path resumes after interruption/restart without duplicate payment, reuses exact stored signed bytes when retry is permitted, reconciles transaction hash and nullifiers before re-signing, and never partially retries an atomic output list. A separate 1,000-item staging rehearsal is still required for production-scale evidence.

### 3. JS SDK and Wallet Integration

The JS SDK or wallet storage must follow the note-reservation state contract.

The required checks are as follows:

- The JS SDK state enums/transitions match the Go conformance fixture.
- The JS/TS implementation passes `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json` for the 1/1, 3-input/4-output, 31-payments-plus-change, exact-32-payment, and explicit-padding shapes.
- The definitions of active reservation states match.
- `nullifier_lookup_key_id` or `lookup_key_version` is handled.
- Wallet note selection excludes reserved notes.
- Ordinary transfers, split/merge operations, and payroll jobs cannot select the same note concurrently.
- The UI displays only abbreviated values, rather than raw nullifiers/commitments.

If the JS SDK is already being developed against `docs/clairveil-note-reservation-design.md`, use the Go reference implementation as a contract verification baseline rather than as a mandate for a particular production database implementation. New batch work must also follow `docs/clairveil-session3b-batch-transfer-handoff.md`; the proto alone is not the downstream client contract.

### 4. Prover Operations and Horizontal Scaling

The product operations environment may operate multiple `clairveil-proverd` endpoints for capacity, but prepared proof payloads contain private note witness. Distributing distinct, previously unassigned jobs across endpoints is not failover: assign each job once, persist the endpoint identity, and pin that witness to the selected endpoint. The Session 3B `BatchPayrollProver` API represents exactly one local prover or one explicitly selected remote endpoint and includes no pool/failover behavior.

Sending the same witness-bearing payload to a second endpoint expands the privacy boundary and is forbidden by default. The legacy `ProverPool` selects only one endpoint per request unless `MultiProverFailoverOptIn` is supplied; opt-in validation requires the complete allowed `EndpointIDs` set and `PrivacyWarningAcknowledged=true`. A new call with the same witness must not be treated as an independent job or allowed to round-robin silently. HTTP `retryable=true`, endpoint timeout, or queue saturation does not authorize cross-endpoint failover.

The required implementation includes the following:

- Configure a concurrency limit and timeout for each endpoint.
- Health-check endpoints and exclude failed endpoints.
- Persist job-to-endpoint assignment before first witness disclosure and define separate same-endpoint retry and unassigned-job scheduling policies.
- Keep cross-endpoint same-witness failover disabled unless product/user policy records the full allowed endpoint set and explicit privacy-warning acknowledgment.
- Run the `PROVERD_URLS`-based scale benchmark with its controlled transfer/withdraw fixtures and record the unhealthy endpoint count. It measures legacy route distribution, not Session 3B 16x32 proof capacity, and benchmark round-robin is not production failover authorization.
- Collect per-endpoint latency, error-rate, timeout-rate, RSS, and CPU telemetry.
- Perform warm-up and capacity checks before the peak payroll window.

The completion criterion is that independent new/unassigned jobs can continue on healthy endpoints when one fails; an already disclosed witness is not sent elsewhere without validated explicit opt-in; default and opt-in endpoint contact counts are auditable; and the controlled benchmark records `unhealthy_endpoint_count`. A Session 3B capacity claim additionally requires actual 16x32 proof/sec and resource measurements at each claimed endpoint count; the existing transfer/withdraw scale benchmark is insufficient for that claim.

### 5. Rehearsal Runbook

Run rehearsals in stages before actual operations.

The recommended current order is as follows:

1. Session 3B conformance/static gate with `make privacy-batch-joinsplit-localnet`
2. Actual one-proof restart/retry/disclosure localnet gate with `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`
3. 1,000-item single-tenant one-proof staging dry run and restart/retry run
4. 100-tenant x 1,000-item synthetic scheduling run
5. 10,000-item single-tenant run
6. 100,000-item single-tenant capacity rehearsal

Each run must leave the following results:

- Total elapsed time
- proof/sec, tx/sec, and payroll item/sec
- Batch input/output shape distribution and payment/change/padding counts
- Reservation conflict count
- Retry count
- Replan count
- Manual review count
- Failed item count
- Final reserve invariant
- List of items requiring operator review
- Prover endpoint assignment, any privacy opt-in, and per-endpoint contact counts

## Decisions Required Outside the Repository

- Month-end peak SLA: how many hours may be used to finish 100,000 items
- Per-tenant concurrent-run limits and priorities
- Which party pays fees and the relayer operations policy
- Personnel responsible for manual review and the approval policy
- Sensitive-data retention period
- Whether to store raw payroll-item recipients/amounts or store them only in hash, HMAC, or encrypted form
- Evidence and change-control criteria for any future circuit shape beyond the frozen `BatchJoinSplit16x32` contract

## Handoff Checklist

- The product team has reviewed the result of `make privacy-bulk-readiness-check`.
- The operations team runs `make privacy-batch-joinsplit-localnet`, then `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet` when local resources and development artifacts are available, to verify the current one-proof path.
- The operations team may run `make reference-payroll-demo` and `make reference-payroll-live-localnet` as legacy control-plane and multi-message regression paths; they are not the current one-proof gate.
- Preserve the 2026-07-08 command `PAYROLL_SEED_NOTES=1 PAYROLL_ITEM_COUNT=1000 PAYROLL_CHUNK_SIZE=20 GAS_PRICES=0uclair make reference-payroll-live-localnet` and its successful result only as dated legacy restart/retry evidence; see `docs/clairveil-reference-payroll-localnet-rehearsal-result.md`.
- Before a release, verify the independent legacy multi-message path with `RUN_LOCALNET=1 TRANSFER_BATCH_COUNT=2 make privacy-bulk-readiness-check` in addition to, not instead of, the current one-proof gate.
- To make a legacy transfer/withdraw prover-pool scaling claim, retain the result of `RUN_PROVER_SCALE=1 PROVERD_URLS=url1,url2 make privacy-bulk-readiness-check` as a separate controlled-fixture artifact. `unhealthy_endpoint_count=0` is required for that public claim. Do not relabel it as 16x32 capacity evidence or production cross-endpoint failover permission.
- The backend team reviews both the current Session 3B batch graph/workers and the legacy durable-control-plane workflow; if a managed database is needed, it writes a migration plan preserving the applicable batch operation and `reservation.Store` contracts.
- The JS SDK team verifies both note-reservation conformance and `privacy_batch_transfer_session3b_contract.json`.
- The operations team defines endpoint assignment persistence, same-endpoint retry, explicit same-witness failover opt-in, and telemetry collection.
- The product team runs a current one-proof 1,000-item staging rehearsal before making a capacity claim.
- Staging/production note preparation does not use `PAYROLL_SEED_NOTES=1`; validate it with the actual deposit, split/merge, and approval-based preparation flow.
- If the current 16/32 evidence does not meet the SLA, open a separate protocol-shape decision with a roadmap, threat review, circuit/keeper/SDK contract, migration plan, and fresh validation; do not treat the implemented Session 3B work as a future phase.

## Non-Blocking Follow-up Backlog

- The `examples/clairveil-dapp/**` consumer drift check was excluded from this bulk-transfer review scope; inspect it separately when the dapp scope is opened.
- Full `make check` and full `make release-check` include dapp/localnet/external smoke coverage and must be run separately for a release candidate.
- To use live one-proof/prover-scale measurements in a public claim, retain `RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet`, actual 16x32 scale measurements, and the separately scoped controlled `RUN_PROVER_SCALE=1` transfer/withdraw readiness result as release artifacts.
- The allocator's current handoff risk is closed by targeted regressions and fixed-seed property-style tests. Treat an exhaustive fuzz suite based on random generation as a long-term test-depth enhancement, rather than as a release blocker.
