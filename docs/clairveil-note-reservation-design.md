# Clairveil Note Reservation Design Note

Korean version: [clairveil-note-reservation-design-kr.md](clairveil-note-reservation-design-kr.md)

## Purpose

This document defines the note reservation design needed for payroll, batch transfer, and bulk transfer in Clairveil.

The target audience is client, SDK, wallet, and payroll control-plane developers. The goal is to prevent selected input notes from being consumed by split, merge, normal transfer, or another payroll job while a bulk transfer is being prepared, and to make failed work safely replannable.

## Background

A Clairveil shielded transfer consumes input notes, reveals nullifiers, and creates new commitments. A note can be consumed only once. Bulk transfer therefore needs to decide which notes will be used before proof generation and prevent other work from using them until the transaction path is resolved.

For example, suppose a payroll job prepares 1,000 payments and assigns 100 treasury notes as input candidates. If another wallet transfer spends one of those notes first, or a background merge consumes it, the payroll proof is no longer valid. At broadcast time the nullifier may already be spent or the Merkle root may no longer match.

Note reservation is therefore required for a reliable UX around the production `MsgBatchTransfer` submission flow provided by the batch reference integration.

## Core Conclusion

Note reservation should first be implemented in the off-chain control plane.

The chain can reject duplicate nullifiers when notes are actually spent, but it does not know the off-chain intent that "this note is reserved for payroll and must not be selected elsewhere." Rather than introducing protocol-level reservation immediately, a wallet or payroll control plane should own note inventory and provide a single-writer lock.

Recommended order:

```text
1. implement client/control-plane note reservation
2. operate dedicated payroll treasury shards
3. separate split/merge windows from payroll execution windows
4. connect reservation to the MsgBatchTransfer client/product flow
5. consider protocol-level reservation only if needed
```

## Terms

| Term | Meaning |
| --- | --- |
| note | spendable record representing a shielded asset |
| nullifier | duplicate-spend prevention value revealed when a note is consumed |
| note inventory | spendable note set known by a wallet or payroll system |
| reservation | temporary assignment of a specific note to a specific job |
| lock | state that prevents other work from selecting a reserved note |
| shard note | treasury note pre-split for parallel payment execution |
| plan | recipient, amount, and input-note candidate calculation before payroll or batch execution |
| replan | recalculating with new input notes after the previous plan becomes invalid |

## Problems To Solve

Bulk transfer must handle:

- a note selected by a payroll plan is first used by a normal transfer
- a note selected by a payroll plan is used by a split or merge transaction
- two items in the same payroll select the same input note
- two payroll jobs select the same treasury note
- a nullifier becomes spent after proof generation but before broadcast
- a transaction times out in mempool or fails because of sequence issues
- part of a batch fails and the chunk must be rebuilt
- scanner delay or chain reorg delays note status updates

Without this design, a batch executor can generate proofs successfully and still experience mass failure at broadcast time.

## Basic Principles

### 1. Transition To Reserved When The Plan Is Confirmed

A note should become `Reserved` as soon as it is selected as an input note in a confirmed payroll plan. Locking at plan confirmation, not proof generation, prevents normal transfers, split/merge jobs, and other payroll jobs from selecting the same note.

Recommended flow:

```text
query Available notes
-> assign input notes to payroll item/chunk
-> create reservation inside DB transaction
-> update note_inventory.reservation_id
-> confirm plan with Reserved state
```

A draft plan may avoid locking notes. Once a user confirms the plan or the scheduler registers it for execution, reservations must be created.

### 2. Select Only Unreserved Notes

Payroll planners and normal transfer planners should select only `Available` notes. Notes in `Reserved`, `Proving`, or `Submitted` state cannot be used by other work.

### 3. Separate Payroll Treasury From Normal Wallet Use

The treasury key/account used for enterprise payroll should not be freely used by ordinary wallet transfers. Sharing the same key across multiple UIs, scripts, and background workers can break off-chain reservation.

### 4. Run Split/Merge Outside Payroll Execution Windows

Background split/merge during payroll execution can consume the same treasury notes and invalidate the plan. Run split before payroll planning and merge after payroll completion.

### 5. Recheck Before Proof And Before Broadcast

A note that was available at plan time can change state before proof or broadcast. Check at least twice.

```text
plan stage: select Available notes
before proof: check local reservation state
before broadcast: check chain nullifier state
```

### 6. Replan By Item Or Chunk, Not The Entire Job

If one note becomes invalid in a bulk transfer, rebuilding the entire payroll is too expensive. Mark only the failed item or chunk as `ReplanRequired` and assign a new note.

## Note State Machine

Recommended states:

```text
Discovered
-> Available
-> Reserved
-> Proving
-> ProofReady
-> Submitted
-> ConfirmedSpent

Reserved
-> Released
-> Available

ManualReview
-> Released
-> Available

ProofReady
-> ConfirmedSpent

Submitted
-> Failed
-> ReplanRequired
-> Reserved(new note)

Submitted
-> Unknown

Unknown
-> ManualReview
```

State meanings:

| State | Meaning | Selectable |
| --- | --- | --- |
| `Discovered` | scanner found the note but spendability is not verified | no |
| `Available` | spendable note available for use | yes |
| `Reserved` | note reserved for a specific job/item/chunk | no |
| `Proving` | proof generation is in progress for this note | no |
| `ProofReady` | proof exists but broadcast has not happened yet | no |
| `Submitted` | transaction was broadcast and result is pending | no |
| `ConfirmedSpent` | chain confirmed the nullifier as spent | no |
| `Failed` | tx or proof failed | no |
| `ReplanRequired` | current note can no longer proceed; new plan needed | no |
| `Released` | reservation was released and can return to available | transition state |
| `Unknown` | broadcast result is unclear | no |
| `ManualReview` | automation cannot safely decide; operator review required | no |

`ProofReady` is still a locked state. A proof is bound to specific input notes and a root; releasing the note just because the proof exists is unsafe.

`Released` has state-specific meaning. Automatic release is allowed only in limited `Reserved` cases. Direct `Proving -> Released` or `ProofReady -> Released` transitions are rejected by the repo contract. If proof artifact disposal, tx non-submission, and lease status must be checked, move to `ReplanRequired` or `ManualReview` first. Only after an operator confirms safety should `ManualReview -> Released -> Available` be used. `ProofReady -> ConfirmedSpent` is a recovery/reconcile path when chain evidence already proves the spend.

`Reconcile` is a worker/process name, not a stored state. A reconcile worker reads `Unknown` reservations and transitions them to `ConfirmedSpent`, `ReplanRequired`, `ManualReview`, or another terminal/review state based on evidence.

Active reservation states:

- `Reserved`
- `Proving`
- `ProofReady`
- `Submitted`
- `Unknown`
- `ManualReview`

No duplicate active reservation may exist for the same `owner_key_id + nullifier_lookup_key`.

## Separate Note State From Operation Success

`ConfirmedSpent` is a note-level state. It means the chain saw the nullifier spent. It does not by itself prove that the intended payroll/payment operation succeeded.

If a conflicting transaction consumed the same note first, the nullifier is spent but the payroll payment failed.

Payment operation success should be accepted only when evidence matches the current operation:

- tx hash or tx result is connected to the current `operation_id`
- event output commitment matches the expected output commitment
- audit disclosure digest or audit disclosure payload matches expected recipient/amount
- recipient shielded key, amount, denom, and batch item index match the plan
- `sign_doc_hash`, `tx_bytes_hash`, and `tx_hash` match the stored operation record

If evidence matches, the payment operation can be marked successful. If evidence is missing or mismatched, update the note to `ConfirmedSpent` but move the operation to `ManualReview` or operation-level `ConflictSpent`.

`ConflictSpent` is better modeled as a payment operation status than a note reservation status. It means "the input note was consumed, but not proven to be my intended payment."

## Reservation Data Model

The client or payroll control plane should manage at least:

```text
NoteInventory {
  note_id
  commitment
  encrypted_nullifier
  nullifier_lookup_key
  nullifier_lookup_key_id
  asset_id
  encrypted_amount
  owner_key_id
  merkle_position
  discovered_height
  spend_status
  reservation_id
  updated_at
}

NoteReservation {
  reservation_id
  company_id
  payroll_id
  batch_id
  chunk_id
  item_id
  note_id
  owner_key_id
  encrypted_nullifier
  nullifier_lookup_key
  nullifier_lookup_key_id
  status
  expires_at
  lease_owner
  lease_token
  lease_until
  last_heartbeat_at
  operation_id
  sign_doc_hash
  tx_bytes_hash
  tx_hash
  account_sequence
  broadcast_attempt_count
  last_broadcast_at
  last_broadcast_error
  created_at
  updated_at
}

PayrollOperation {
  operation_id
  company_id
  payroll_id
  batch_id
  chunk_id
  item_id
  expected_output_commitment
  expected_disclosure_digest
  expected_recipient_hash
  encrypted_expected_recipient
  encrypted_expected_amount
  expected_amount_hash
  expected_denom
  batch_item_index
  batch_item_index_known
  sign_doc_hash
  tx_bytes_hash
  tx_hash
  status
  created_at
  updated_at
}
```

`note_id` is a local DB identifier. `commitment` and `nullifier` are needed for duplicate prevention and chain reconciliation, but raw nullifiers should not be used directly as indexes.

Recommended form:

```text
nullifier_lookup_key = HMAC(index_key, nullifier)
nullifier_lookup_key_id = key identifier for index_key
encrypted_nullifier = Encrypt(db_field_key, nullifier)
```

`nullifier_lookup_key` is a deterministic keyed value for unique/index use. Store `nullifier_lookup_key_id` or `lookup_key_version` for key rotation. Store raw nullifiers encrypted and never put them in logs or telemetry.

`PayrollOperation` or `PayrollItem` must store expected values for operation success judgment, such as `expected_output_commitment`, `expected_disclosure_digest`, `expected_recipient_hash`, `expected_amount`, `expected_denom`, and `batch_item_index`. In the Go payroll worker, `expected_disclosure_digest` corresponds to the audit disclosure digest in `PreparedTransferPayload.AuditDisclosureDigestHex` and `MsgTransfer.AuditDisclosureDigest`. Do not substitute user disclosure or sender self-view disclosure digest for operation success evidence. Use a `batch_item_index_known` boolean to distinguish zero-value unknown from actual item index `0`. Store sensitive recipients and amounts as encrypted values, hashes, or HMACs when appropriate.

## Prevent Duplicate Active Reservations

Only one active reservation may exist for a given `owner_key_id + nullifier_lookup_key`. Without this constraint, two planners or workers can assign the same note to payroll, split, merge, or normal transfer concurrently.

For PostgreSQL control-plane DBs, use a partial unique index.

```sql
CREATE UNIQUE INDEX uniq_active_note_reservation
ON note_reservations(owner_key_id, nullifier_lookup_key)
WHERE status IN (
  'Reserved',
  'Proving',
  'ProofReady',
  'Submitted',
  'Unknown',
  'ManualReview'
);
```

`x/privacy/client/sdk/reservation.SQLStore` provides PostgreSQL/SQLite reference schemas with this constraint. Production DBs must preserve the same active uniqueness semantics while adding tenant partitioning, field-level encryption, migrations, and connection-pool policy.

If partial unique indexes are not available, use one of:

- single-writer queue per `owner_key_id`
- process-local mutex plus DB transaction
- active-status requery before reservation creation
- conflict handling that keeps the existing reservation and replans the new plan

Do not rely only on application logic when a DB constraint or equivalent lock is possible.

## Payroll Plan Stage

`payroll plan` runs:

```text
1. validate payroll input
2. check duplicate recipients
3. scan treasury note inventory
4. select only Available notes
5. calculate note allocation by amount
6. warn if shard notes are insufficient
7. create reservations
8. save plan file or DB record
```

This stage sends no transaction. It decides which notes will be used for which payroll items/chunks and locks them.

Avoid plans that repeatedly roll a single large note through change outputs. Change-note chains reduce parallel proof generation. Before payroll, split treasury into multiple shard notes so chunks can use independent shards.

### DB Transaction Based Note Selection

Selecting `Available` notes and transitioning them to `Reserved` must happen in one DB transaction. If selection and reservation creation are separate, two planners can select the same note.

PostgreSQL example:

```sql
BEGIN;

SELECT note_id, nullifier_lookup_key
FROM note_inventory
WHERE owner_key_id = $1
  AND spend_status = 'Available'
  AND reservation_id IS NULL
ORDER BY discovered_height, note_id
FOR UPDATE SKIP LOCKED
LIMIT $2;

-- create note_reservations rows for selected notes
-- update note_inventory.reservation_id

COMMIT;
```

`FOR UPDATE SKIP LOCKED` lets concurrent planners skip notes already locked by another transaction.

If the DB does not support `SKIP LOCKED`, use:

- advisory lock per `owner_key_id`
- single-writer queue per `owner_key_id`
- SQLite transaction plus process-local mutex
- IndexedDB transaction plus a single background worker in browser environments

For reliable bulk payroll, "query Available notes" and "transition to Reserved" must be atomic.

## Payroll Run Stage

`payroll run` executes only items with reservations.

```text
1. query Reserved notes
2. check local lock before proof
3. check chain nullifier is unspent
4. change state to Proving
5. generate proof
6. change state to ProofReady
7. recheck nullifier before broadcast
8. submit transaction
9. change state to Submitted
10. confirmation scanner/reconcile worker updates note state and operation state separately
```

Local lock must remain held between proof generation and broadcast so no other process can use the same note.

### Compare-And-Set Transitions

Reservation state changes must always include the current state as a condition. This prevents stale workers from overwriting work already handled by another worker.

Example:

```sql
UPDATE note_reservations
SET status = 'Proving',
    updated_at = NOW()
WHERE reservation_id = $1
  AND status = 'Reserved';
```

If zero rows are affected, the transition failed. The worker should reread the current state and stop or adjust.

Recommended transitions:

| Transition | Condition |
| --- | --- |
| `Reserved -> Proving` | `status = 'Reserved'` and valid lease acquired |
| `Proving -> ProofReady` | `status = 'Proving'` and same `lease_token` |
| `ProofReady -> Submitted` | same `lease_token`, nullifier unspent before broadcast |
| `ProofReady -> Unknown` | same `lease_token`, signed tx or broadcast attempt metadata exists but result is unclear |
| `ProofReady -> ConfirmedSpent` | chain nullifier/tx evidence matches the current operation even without local submitted record |
| `Reserved -> Released` | reservation never started proof/broadcast and release is compare-and-set |
| `ManualReview -> Released` | operator confirms proof artifact disposal, tx non-submission, and lease status |
| `Submitted -> ConfirmedSpent` | tx success or spent evidence matches the current operation |
| `Submitted -> Unknown` | tx result unclear |
| `Unknown -> ManualReview` | automation cannot decide |

`Unknown -> Submitted` is not recommended. `Unknown` already means signed tx or broadcast attempt result is unclear; reconcile should narrow it to `ConfirmedSpent`, `Failed`, `ReplanRequired`, or `ManualReview` based on evidence.

### Worker Lease And Heartbeat

Proof workers and broadcaster workers should acquire a lease before processing a reservation. A lease proves the worker is alive and authorized to handle that reservation.

Recommended fields:

- `lease_owner`
- `lease_token`
- `lease_until`
- `last_heartbeat_at`

Workers periodically heartbeat to extend `lease_until`. State changes are allowed only when `lease_token` matches.

Proof workers should acquire leases only for `Reserved` reservations. A stale proof worker must not reacquire a lease on `ProofReady` or `Submitted` and overwrite broadcast authority. Long proof jobs should keep heartbeating the same `lease_token` while in `Proving`.

Lease acquisition, heartbeat, lease clear, and worker-owned `ProofReady -> Submitted/Unknown` transitions must be atomic in the store. `ProofReady -> ConfirmedSpent` also needs compare-and-set or the same transaction/row-lock path, although it is evidence-driven recovery rather than broadcaster authority.

Example:

```sql
UPDATE note_reservations
SET status = 'ProofReady',
    lease_until = NULL,
    updated_at = NOW()
WHERE reservation_id = $1
  AND status = 'Proving'
  AND lease_token = $2;
```

This reduces zombie worker problems: if a worker thought to be dead returns later and tries to submit old proof/tx, an invalid lease token blocks the state change and broadcast path.

## Split / Merge Policy

Do not run split and merge as arbitrary background jobs during bulk transfer. Split/merge transactions also consume notes and can invalidate payroll plans.

Recommended policy:

- Split large notes into shard notes during the pre-payroll `prepare treasury` stage.
- Build payroll plan only after split is confirmed and scanner sees shard notes.
- Ban merge for the same treasury during payroll execution.
- If extra split is needed during payroll, use only available notes that do not conflict with existing reservations.
- Merge leftover change notes after payroll or prepare them for the next payroll shard.

Expected flow:

```text
large treasury note
-> prepare split
-> shard note 1
-> shard note 2
-> shard note 3
-> payroll plan
-> reservation
-> payroll run
-> leftover merge or next payroll reserve
```

This prevents normal transfer, payroll transfer, split, and merge from competing for the same notes.

## Relationship To Multi-message / MsgBatchTransfer

Multi-message transactions or `MsgBatchTransfer` group multiple transfer items into one submission unit. They do not automatically solve input-note conflicts.

Before building a batch, check:

- no duplicate `note_id` inside the same batch
- no duplicate nullifier inside the same batch
- every note in the batch is `Reserved` or `ProofReady`
- reservation `payroll_id`, `batch_id`, and `chunk_id` match the current batch
- nullifier is still unspent on-chain immediately before broadcast

`MsgBatchTransfer` performs module-level canonical and duplicate-nullifier validation. The keeper still does not know which off-chain payroll reserved a nullifier. Reservation remains a client/control-plane responsibility.

## Failure Handling

Key rule:

```text
Do not release Submitted / Unknown / ManualReview notes to Available by TTL alone.
```

RPC timeout, mempool eviction, and scanner delay can look like failure even when the tx entered the chain. Always reconcile tx hash and nullifier state before release.

### nullifier already spent

Causes:

- another transaction consumed the same note first
- planner selected a note whose spent state was delayed by scanner
- split/merge conflicted with payroll

Handling:

```text
1. mark the item or chunk Failed
2. update the note to ConfirmedSpent
3. check whether evidence matches the current operation/payment
4. if it matches, mark payment success
5. if it mismatches or evidence is insufficient, mark operation ManualReview or ConflictSpent
6. mark other pending items referencing the same note ReplanRequired
7. select new input notes from Available notes
8. regenerate proof
9. retry
```

### transaction timeout or unclear result

Causes:

- RPC timeout
- mempool eviction
- sequence mismatch
- scanner delay

Handling:

```text
1. if submit attempt metadata exists, move ProofReady or Submitted to Unknown
2. if tx_hash exists, query tx
3. if tx succeeded, check expected output/audit disclosure digest/recipient/amount
4. if tx failed, classify failure reason
5. if no tx, query nullifier
6. if nullifier is spent, update note state to ConfirmedSpent
7. if spent evidence matches current operation, mark payment success
8. if spent evidence mismatches or is insufficient, send to ManualReview or ConflictSpent
9. if nullifier is unspent and tx is absent, consider retry or tx reconstruction
10. if still unclear after a policy window, send to ManualReview
```

### proof generated but not broadcast

Causes:

- broadcaster failure
- insufficient fee
- tx size/gas exceeded

Handling:

```text
1. keep the note locked in ProofReady
2. adjust fee/gas/chunk size
3. retry broadcast
4. if delayed too long, verify whether proof should be discarded
5. only after tx non-submission and proof artifact disposal are confirmed, use ManualReview -> Released or ReplanRequired
```

### partial chunk item problem

Multi-message transactions and `MsgBatchTransfer` are usually all-or-nothing. One invalid item can fail the entire chunk.

Handling:

```text
1. mark the whole chunk Failed
2. rerun item-level prechecks
3. isolate the problematic item
4. rebuild a smaller chunk with remaining items
5. mark problematic item ReplanRequired
```

## TTL / Release Policy

`expires_at` cannot apply the same way to every status.

| State | Policy when TTL expires |
| --- | --- |
| `Reserved` | release may be allowed if proof/broadcast has not started; release must still be compare-and-set |
| `Proving` | if worker lease expires, inspect proof artifact and attempt record; if proof is not saved, return to `Reserved` or `ReplanRequired`; if proof completion is unclear, send to `ManualReview` |
| `ProofReady` | never return to `Available` by TTL alone; even after proof disposal and tx non-submission checks, move through `ManualReview` or `ReplanRequired`; use `ManualReview -> Released -> Available` only after operator approval |
| `Submitted` | never release by TTL alone; an apparent RPC timeout may already be on-chain |
| `Unknown` | reconcile by tx hash/nullifier lookup before state change |
| `ManualReview` | keep active lock until operator confirms |

The only generally automatic release path is:

```text
Reserved -> Released -> Available
```

Other states require proof artifact, tx hash, nullifier, and worker lease checks.

If a broadcaster gets RPC/network error but no tx hash, tx bytes hash, or sign doc hash, do not move `ProofReady` to `Unknown`. Keep the `ProofReady` lock and lease; retry workers can acquire a takeover lease after expiry. Record `ProofReady -> Unknown` only when attempt metadata exists or when non-zero tx code identifies a submission attempt.

## Submitted / Unknown / ManualReview Reconcile

Recommended reconcile order:

```text
1. if tx_hash exists, query tx
2. if tx succeeded, check expected output/audit disclosure digest/recipient/amount
3. if tx failed, inspect failure reason
4. if no tx, query nullifier
5. if nullifier spent, update note state to ConfirmedSpent
6. if spent evidence matches current operation, mark payment success
7. if spent evidence mismatches or is insufficient, ManualReview or ConflictSpent
8. if nullifier unspent and tx absent, consider retry or tx reconstruction
9. if still unclear, ManualReview
```

Failure reason policy:

| Failure reason | Handling |
| --- | --- |
| RPC timeout | query tx_hash/nullifier, then retry if safe |
| mempool eviction | query tx_hash/nullifier; if signed tx bytes are stored, consider retransmitting same tx; otherwise confirm nullifier unspent before reconstruction |
| insufficient gas | confirm nullifier unspent, then adjust gas and re-sign |
| sequence mismatch | check account sequence and re-sign; confirm nullifier unspent first |
| proof invalid | `ReplanRequired` |
| nullifier spent | note becomes `ConfirmedSpent`; operation success only when expected output/audit disclosure digest/recipient/amount match; otherwise `ManualReview` or operation-level `ConflictSpent` |
| root invalid | regenerate proof against a new root, so `ReplanRequired` |
| payload mismatch | investigate proof/payload mismatch, then `ReplanRequired` or `ManualReview` |

`ManualReview` is an active lock state. Do not assign the same note to another job until an operator confirms.

## Tx Retry Idempotency

Broadcast retry must be idempotent by `operation_id`. If every retry creates a new logical operation, sequence, fee, and nullifier state become hard to reason about.

When replanning the same payment item, create a new attempt/run dimension. The initial plan may use `payroll_id:item_id`, but replan results should use new `operation_id` and `reservation_id` such as `payroll_id:item_id:attempt:N` so terminal/review operations do not conflict.

Recommended fields:

- `operation_id`
- `sign_doc_hash`
- `tx_bytes_hash`
- `tx_hash`
- `account_sequence`
- `broadcast_attempt_count`
- `last_broadcast_at`
- `last_broadcast_error`

Recommended policy:

- Treat txs for the same `operation_id` as one logical operation.
- Store signed tx bytes and tx hash.
- After RPC timeout, query tx_hash before creating a new tx.
- Retransmitting the exact same signed tx bytes can be allowed.
- Before re-signing with a new sequence, confirm the nullifier is unspent.
- If gas/sequence problems require tx reconstruction, keep the same `operation_id` and reservation.
- If nullifier is spent, note state may become `ConfirmedSpent`, but payment success still requires evidence matching expected output/audit disclosure digest/recipient/amount.

The current Go reference SDK stores identifiers and hashes such as `tx_hash`, `tx_bytes_hash`, and `sign_doc_hash`. To retransmit the same signed tx bytes, a scheduler or broadcaster queue must store those bytes durably. If it does not, timeout/mempool failures should follow `ReconcileUnknown`: query `tx_hash` and nullifier state first, then decide whether to re-sign or replan.

## Concurrency Control

Note reservation depends on a single-writer principle. Multiple independent selectors for the same treasury key can break the lock.

Recommended architecture:

```text
wallet / payroll control plane
-> single note inventory DB
-> transaction lock or advisory lock
-> planner
-> prover queue
-> broadcaster queue
```

Concurrency rules:

- one writer selects notes for each `owner_key_id`
- proof workers process already-reserved jobs only
- broadcasters submit only `ProofReady` reservations
- split/merge workers use the same reservation DB
- manual wallet transfer does not share payroll treasury keys

For server/control-plane environments, use PostgreSQL transactions, `FOR UPDATE SKIP LOCKED`, and partial unique indexes. For local/mobile/web environments, combine single-writer queues, SQLite or IndexedDB transactions, and process-local mutexes.

## Reservation DB Sensitive Data Protection

Reservation DB is privacy-sensitive. If nullifier, commitment, amount, payroll item, and recipient mapping are stored together, a DB leak reveals significant payroll activity.

Recommended policy:

- use DB at-rest encryption
- apply field-level encryption when possible
- use deterministic keyed values such as `nullifier_lookup_key = HMAC(index_key, nullifier)` for unique/index use
- store raw nullifiers encrypted
- do not log raw nullifier, commitment, recipient, or amount
- do not send reservation payloads to telemetry/analytics
- separate operator privileges
- define proof payload and reservation detail retention policy
- show shortened nullifier/commitment in UI

Recommended storage:

```text
index/search:
  nullifier_lookup_key = HMAC(index_key, nullifier)

encrypted fields:
  encrypted_nullifier
  encrypted_amount
  encrypted_recipient
  encrypted_payroll_item

logs/telemetry:
  reservation_id
  company_id
  payroll_id
  status
  error_code
  shortened tx_hash
```

This lets operators track status while reducing payroll detail exposure if DB or logs leak.

## Protocol-Level Reservation Consideration

On-chain reservation is possible.

Example:

```text
MsgReserveNotes
  creator
  reservation_id
  nullifiers[]
  expires_at
```

This can provide stronger guarantees because the chain knows reserved nullifiers. It is not the default recommendation.

Reasons:

- revealing nullifiers before spend can weaken privacy
- reservations that never spend need lock release and expiry handling
- malicious users can bloat reservation state
- reservation authority checks are complex
- existing transfer keeper logic and nullifier lifecycle become more complex

Consider protocol-level reservation only when:

- multiple independent actors must operate the same treasury concurrently
- off-chain single-writer control is not trusted
- reservation itself must be auditable on-chain
- batch execution failure cost is high enough to require on-chain reservation guarantees

Until then, client/control-plane reservation and treasury sharding are enough to start.

## Client Implementation Checklist

- note inventory DB exists
- note status is managed as `Discovered`, `Available`, `Reserved`, `Proving`, `ProofReady`, `Submitted`, `Unknown`, `ManualReview`, `ConfirmedSpent`, `Failed`, `Released`, `ReplanRequired`
- selected notes become `Reserved` immediately when `payroll plan` is confirmed
- `payroll plan` selects only available notes
- active reservations have `owner_key_id + nullifier_lookup_key` unique constraint or equivalent guarantee
- `nullifier_lookup_key` stores `index_key_id` or `lookup_key_version`
- expected output commitment, audit disclosure digest, recipient hash, amount, denom, and batch item index are stored on `PayrollOperation` or `PayrollItem`
- planner selects notes with DB transaction, row lock, `FOR UPDATE SKIP LOCKED`, or equivalent single-writer lock
- state transitions are compare-and-set
- proof workers and broadcasters use lease, heartbeat, and lease token
- the same note/nullifier is not duplicated in the same job or chunk
- nullifier state is rechecked before proof and before broadcast
- split/merge worker consults reservation DB
- background merge for the same treasury is banned during payroll
- failed item or chunk can be replanned independently
- unclear transaction results move through `Unknown/ManualReview` and reconcile worker/process
- `Submitted`, `Unknown`, and `ManualReview` are not released by TTL alone
- tx retry is idempotent by `operation_id`
- reconcile queries tx hash before nullifier status
- before treating `ConfirmedSpent` as payment success, expected output commitment, audit disclosure digest, recipient, and amount match the current operation
- reservation DB never writes raw nullifier/amount/recipient to plaintext logs
- payroll treasury key is separate from normal wallet transfers

## Recommended MVP Scope

Initial implementation can be:

```text
1. local note inventory DB
2. note reservation table
3. active reservation unique constraint or single-writer lock
4. reservation creation at payroll plan confirmation
5. DB transaction based note selection
6. compare-and-set state transitions
7. worker lease/heartbeat
8. nullifier recheck before proof/broadcast
9. split/merge worker integration with reservation
10. Submitted/Unknown reconcile
11. operation_id based tx retry
12. failed item/chunk replan
13. reservation DB sensitive data protection
14. reservation/retry history in payroll report
```

This MVP can be implemented without protocol changes. Even after `MsgBatchTransfer`, N-output batch circuits, or payroll Merkle distribution are introduced, the same reservation model remains useful in the upper operational layer.
