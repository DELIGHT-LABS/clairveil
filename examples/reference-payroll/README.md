# Reference Payroll

This reference covers the payroll control-plane contract and a runnable demo. The demo uses simulated legacy multi-message processing; it generates no live proofs and broadcasts no transactions. The one-proof integration contract is described below.

Korean version: [README-kr.md](README-kr.md)

## Run

From the repo root:

```bash
make reference-payroll-demo
```

Or choose an output directory:

```bash
OUT_DIR=tmp/my-payroll-demo ./scripts/reference-payroll-demo.sh
```

## Demo flow

The script runs:

```text
validate
prepare-notes
plan
run
status
clairveil-payrolld -once
status
export-report -state
```

This demo uses `clairveil-payrolld` in its default `simulated` mode. In this mode it does not generate live proofs or broadcast chain transactions. It simulates proof-ready, submitted, and reconciled transitions against the durable reservation state.

`clairveil-payrolld` also has a `live` mode for the long-running scheduler surface. The CLI reference live mode reconciles submitted/unknown operations from tx evidence files; production proof generation and broadcast are connected through the SDK live executor or an external worker.

## Demo outputs

Default outputs are written under `tmp/reference-payroll-demo/`.

| File | Meaning |
| --- | --- |
| `validation.json` | payroll input validation result |
| `note-preparation.json` | note preparation status and operation hints |
| `plan.json` | draft payroll plan |
| `confirmed-plan.json` | plan with confirmed reservations |
| `reservation-state.json` | durable reservation/operation state |
| `payrolld-report.json` | simulated daemon tick report |
| `status-after-daemon.json` | state summary after daemon execution |
| `final-report.json` | final item-level payroll report |

Successful run:

```text
status-after-daemon.json:
  reservations_by_status.ConfirmedSpent = all reservations
  operations_by_status.Succeeded = all operations

final-report.json:
  status = Confirmed
```

## One-proof and legacy paths

This demo is the legacy multi-message control-plane path: `transfer-batch` and `clairveil-payroll ... settle-transfer-batch` place several independent native 2x2 `MsgTransfer` messages and proofs in one Cosmos transaction. It is a regression/tutorial path and must never be described, submitted, reconciled, or capacity-planned as a one-proof batch.

The current payroll integration uses `transfer-batch-16x32`, `prepare-batch-transfer`, `prove-batch-transfer`, and `broadcast-batch-transfer`: one `MsgBatchTransfer`, one `BatchJoinSplit16x32` proof, 1..16 inputs, and 1..32 outputs. The remote proof route is `POST /v1/proofs/batch-transfer`. Use the [getting started guide](../../docs/clairveil-getting-started.md#8-batchjoinsplit16x32-localnet) for that localnet workflow.

## Authority and ownership

Resolve disagreements in this order:

1. Consensus protocol and implementation: [`proto/clairveil/privacy/v1/tx.proto`](../../proto/clairveil/privacy/v1/tx.proto), `x/privacy/types`, the keeper, and the [batch contract](../../docs/clairveil-batch-joinsplit-16x32.md).
2. Cross-client boundary: [batch-transfer fixture](../../x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json), [reservation fixture](../../x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json), and `x/privacy/client/sdk/conformance` tests.
3. Reference behavior: `x/privacy/client/sdk/batchtransfer`, `x/privacy/client/sdk/reservation`, `x/privacy/client/sdk/payroll`, the [CLI reference](../../docs/clairveil-cli-reference.md), and the [JS SDK handoff](../../docs/clairveil-js-sdk-handoff.md).

Pin the checkout or tag for all three layers. Fixtures define interoperability expectations; protocol and code remain normative when a fixture needs correction.

The repository supplies the `MsgBatchTransfer` protocol and keeper, Go batch builder, bounded prover transport, typed scanner contracts, reference durable graph/workers, fixtures, and CLI/tutorials. Downstream owns tenant isolation, the production store, encryption and key management, workers and monitoring, signer/account-sequence coordination, RPC policy, product approval UX, wallet/JS implementation, audit operations, incident response, and production acceptance. `DurableFileStore`, `SQLStore`, and reference daemons are contract examples, not a required database or topology.

## Durable reservation and retry contract

On plan confirmation, select only `Available` notes and atomically create one operation with its 1..16 reservations. Enforce an active unique key on `(owner_key_id, nullifier_lookup_key)`. Selection/reservation must share one database transaction or an equivalent single-writer critical section; every later transition uses compare-and-set (CAS), never read-then-write.

Active reservation states are `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, and `ManualReview`; exclude them from ordinary send, split, merge, and payroll planning. Follow the transition rules in the reservation fixture. Do not release `Submitted`, `Unknown`, or `ManualReview` to `Available` only because a TTL expired, and do not take invalid `Proving -> Released`, `ProofReady -> Released`, or direct active-to-available shortcuts.

Worker mutations require a current non-expired lease token. Store `lease_owner`, `lease_token`, `lease_until`, and `last_heartbeat_at`; acquire, heartbeat, clear, and CAS-transition the lease in one atomic store operation. Continue heartbeats while proof generation or broadcast is still running after caller cancellation. Lease expiry permits safe recovery, never makes a note spendable.

Persist private material before durable status advances: prepared payload and hash, proof and hash, exact signed transaction bytes and byte hash, sign-doc hash, tx hash, account sequence, broadcast attempt/error, reservations, and expected/observed output evidence. Retry an `operation_id` with the exact stored signed bytes. Create a new operation/reservation attempt only after the earlier batch outcome resolves; never rebuild only part of an atomic output list.

## Reconciliation and item evidence

Reconcile **tx hash first**, then query every input nullifier only if the transaction is absent or ambiguous. An RPC timeout, mempool eviction, process crash, or cancelled request is `Unknown`; it does not authorize a new transaction. A missing nullifier response is a safe failure, not `unspent`.

`nullifier_spent` proves only that an input was consumed. Keep batch status separate from per-item evidence status. Mark an item successful only when the intended transaction identity and expected output evidence agree: output index, commitment, recipient hash, amount, denom or asset ID, audit key ID/epoch, and any fixture-required disclosure digest. If evidence is missing or differs, the reservation remains `ConfirmedSpent` while the item evidence is `ManualReview` or `ConflictSpent`.

Use the audit/full disclosure digest as the primary success predicate. Validate the user/self-view digest separately when expected; it cannot replace audit evidence. Reconciliation can compare digests without the audit private key, while audit payload decryption belongs to the audit workflow.

## Privacy, workers, and completion gate

Default user disclosure is `all-private` / `none`; use `recipient-encrypted` only with a registered, versioned recipient key, and `public` only under explicit policy or in tests. Keep the audit-key path and audit digest separate. Note preparation is approval-based: analyze, preview, approve, execute a split/merge/funding action, rescan/nullifier-check, then finalize the plan. Store `nullifier_lookup_key = HMAC(index_key, nullifier)` with its key/version identifier, encrypt raw nullifiers, commitments, recipients, amounts, payload/proof/signed bytes, and payroll mappings at rest, and keep those values and disclosure/viewing keys out of logs, telemetry, analytics, and crash reports.

Operate planner, proof, broadcast, scanner/reconcile, and operator review as restartable responsibilities. The one-proof reference workers are `BatchProofWorker`, `IdempotentBatchBroadcastWorker`, and `BatchReconcileWorker` under `x/privacy/client/sdk/payroll`. Do not enable automatic multi-prover failover. JS/TS and wallet implementations must port the fixtures; use `privacy-note-v1`, canonical `privacy-fixed-v1` typed bytes, `AssetRegistryV1`, the `(height, global_sequence, output_index)` scan cursor, and `POST /clairveil/privacy/v1/nullifiers` (maximum 1000 per request). Preserve forced-rescan/reorg recovery and disclosure-key display/import without telemetry leakage. Define retention and access controls for private artifacts and audit disclosure material.

Portable evidence includes:

```sh
go test ./x/privacy/client/sdk/conformance/... -count=1
go test ./x/privacy/client/sdk/... -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
make reference-payroll-live-localnet
make privacy-bulk-readiness-check
```

For staging, record the environment, pinned commit/artifact identity, configuration, and results. Completion requires fixture-compatible one-proof construction/proving/submission/scanning; exclusive active-note reservation; stale-worker protection; leases that survive long proof/broadcast work; encrypted replay-safe artifacts; tx-hash-first reconciliation of unknown outcomes; matching item evidence; enforced approval/disclosure/retention/operator-review policies; and demonstrated capacity for the proposed deployment. The legacy localnet target proves only legacy `transfer-batch` behavior.
