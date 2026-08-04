import test from "node:test";
import assert from "node:assert/strict";
import {
  createNoteReservationManager,
  MemoryReservationStore,
  preparePlanReservation,
  reservationStatuses,
} from "clairveiljs/reservation";

function noteFixture({
  nullifier = "aa".repeat(32),
  amount = 5,
  height = 10,
  sequence = 1,
  txHash = "ABCD",
  spent = false,
} = {}) {
  return {
    note: {
      receiverSpendPubKeyX: 1n,
      receiverSpendPubKeyY: 2n,
      receiverViewPubKeyX: 3n,
      receiverViewPubKeyY: 4n,
      amount: BigInt(amount),
      assetID: 7n,
      randomness: 8n,
      memo: "",
    },
    nullifier,
    isSpent: spent,
    txHash,
    height,
    sequence,
  };
}

function createTestManager(store = new MemoryReservationStore()) {
  return createNoteReservationManager({
    store,
    ownerKeyId: "chain:clair1owner",
    indexKey: "index-key-v1",
  });
}

async function prepareProofReadyRelayReservation(manager, note) {
  const batch = await preparePlanReservation(manager, {
    plan: { selectedNote: note },
    kind: "relay_withdraw",
  });
  await manager.markProofReady(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    payloadHash: "relay-payload-hash",
  });
  const proofReady = await manager.getReservation(batch.reservation_ids[0]);
  assert.equal(proofReady.metadata.no_broadcast_attempt, true);
  return batch;
}

test("local prepared relay payload discard replans the ProofReady reservation", async () => {
  const store = new MemoryReservationStore();
  const manager = createTestManager(store);
  const note = noteFixture();
  const batch = await prepareProofReadyRelayReservation(manager, note);

  await manager.markReplanRequired(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    error: "local_relay_payload_discarded_before_handoff",
    metadata: {
      reconcile_reason: "local_relay_payload_discarded_before_handoff",
      no_broadcast_attempt: true,
      proof_discarded: true,
    },
  });

  const record = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(record.status, reservationStatuses.ReplanRequired);
  assert.equal(record.payload_hash, "relay-payload-hash");
  assert.equal(
    record.metadata.reconcile_reason,
    "local_relay_payload_discarded_before_handoff",
  );
  assert.equal(await manager.reservationForNote(note), null);

  const nextBatch = await preparePlanReservation(manager, {
    plan: { selectedNote: note },
    kind: "relay_withdraw",
  });
  assert.equal(nextBatch.reservations[0].status, reservationStatuses.Proving);
});

test("wallet rejection before broadcast replans instead of creating Unknown", async () => {
  const store = new MemoryReservationStore();
  const manager = createTestManager(store);
  const note = noteFixture({ nullifier: "bb".repeat(32) });
  const batch = await prepareProofReadyRelayReservation(manager, note);

  await manager.markReplanRequired(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    error: "wallet request rejected before broadcast",
    metadata: {
      reconcile_reason: "wallet_rejected_before_broadcast",
      no_broadcast_attempt: true,
      proof_discarded: true,
    },
  });

  const record = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(record.status, reservationStatuses.ReplanRequired);
  assert.equal(record.submitted_tx_hash, "");
  assert.equal(record.sign_doc_hash, "");
  assert.equal(record.tx_bytes_hash, "");
  assert.equal(record.metadata.no_broadcast_attempt, true);
  assert.equal(await manager.reservationForNote(note), null);
});

test("durable broadcast attempt can fall back to Unknown with transaction evidence", async () => {
  const store = new MemoryReservationStore();
  const manager = createTestManager(store);
  const note = noteFixture({ nullifier: "bd".repeat(32) });
  const batch = await prepareProofReadyRelayReservation(manager, note);

  await manager.markBroadcastAttempting(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    reason: "dapp_external_broadcast_boundary",
  });
  const attempting = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(attempting.status, reservationStatuses.ProofReady);
  assert.equal(attempting.broadcast_in_flight, true);
  assert.equal(attempting.metadata.no_broadcast_attempt, false);

  await manager.markUnknown(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    txHash: "0xsubmitted",
    error: "Submitted bookkeeping failed",
    metadata: { reconcile_reason: "submitted_write_failed_after_external_broadcast" },
  });
  const unknown = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(unknown.status, reservationStatuses.Unknown);
  assert.equal(unknown.submitted_tx_hash, "0xsubmitted");
  assert.equal(unknown.broadcast_in_flight, false);
});

test("relay handoff metadata keeps the current ProofReady lease owner", async () => {
  const store = new MemoryReservationStore();
  const manager = createTestManager(store);
  const note = noteFixture({ nullifier: "bc".repeat(32) });
  const batch = await prepareProofReadyRelayReservation(manager, note);

  const firstHandoff = await manager.recordRelayHandoff(batch.reservation_ids, {
    lease_token: batch.lease_token,
    payload_hash: "relay-payload-hash",
  });
  const secondHandoff = await manager.recordRelayHandoff(batch.reservation_ids, {
    lease_token: batch.lease_token,
    payload_hash: "relay-payload-hash",
  });

  const record = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(record.status, reservationStatuses.ProofReady);
  assert.equal(record.lease_owner, manager.leaseOwner);
  assert.equal(record.metadata.relay_handed_off, true);
  assert.equal(record.metadata.no_broadcast_attempt, false);
  assert.equal(secondHandoff[0].metadata.relay_handed_off_at, firstHandoff[0].metadata.relay_handed_off_at);
});

test("successful relay submit keeps tx evidence until spent reconciliation clears the lock", async () => {
  const store = new MemoryReservationStore();
  const manager = createTestManager(store);
  const note = noteFixture({ nullifier: "cc".repeat(32) });
  const batch = await prepareProofReadyRelayReservation(manager, note);

  await manager.markBroadcastAttempting(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    reason: "test_external_broadcast_boundary",
  });
  await manager.markSubmitted(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    txHash: "0xrelay",
  });

  const submitted = await manager.reservationForNote(note);
  assert.equal(submitted.status, reservationStatuses.Submitted);
  assert.equal(submitted.submitted_tx_hash, "0xrelay");
  assert.equal(submitted.metadata.no_broadcast_attempt, false);

  await manager.reconcileSpentNotes([{ ...note, isSpent: true }]);

  assert.equal(
    (await manager.reservationForNote(note)).status,
    reservationStatuses.ConfirmedSpent
  );
  const record = await store.getReservation(batch.reservation_ids[0]);
  assert.equal(record.status, reservationStatuses.ConfirmedSpent);
  assert.equal(record.submitted_tx_hash, "0xrelay");
});
