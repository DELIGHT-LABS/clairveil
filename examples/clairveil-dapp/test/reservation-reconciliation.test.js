import assert from "node:assert/strict";
import test from "node:test";
import {
  createNoteReservationManager,
  MemoryReservationStore,
  preparePlanReservation
} from "clairveiljs/reservation";

import {
  replanExplicitlyFailedReservations,
  reservationHasExplicitBroadcastRejection
} from "../public/reservation-reconciliation.js";

function noteFixture() {
  return {
    note: {
      receiverSpendPubKeyX: 1n,
      receiverSpendPubKeyY: 2n,
      receiverViewPubKeyX: 3n,
      receiverViewPubKeyY: 4n,
      amount: 5n,
      assetID: 7n,
      randomness: 8n,
      memo: ""
    },
    nullifier: "11".repeat(32),
    isSpent: false,
    nullifierStatus: "unspent",
    txHash: "ab".repeat(32),
    height: 10,
    sequence: 1
  };
}

test("durably attempted ProofReady reservations use authoritative broadcast-failure recovery", async () => {
  const calls = [];
  const manager = {
    async markBroadcastFailed(ids, input) { calls.push(["failed", ids, input]); }
  };
  const txHash = "AB".repeat(32);
  const records = ["r1", "r2"].map(reservation_id => ({
    reservation_id,
    status: "ProofReady",
    lease_token: "lease",
    broadcast_in_flight: true,
    broadcast_attempt_count: 1,
    submitted_tx_hash: txHash
  }));

  await replanExplicitlyFailedReservations({
    manager,
    records,
    txHash,
    checkedHeight: 91
  });

  assert.deepEqual(calls.map(call => call[0]), ["failed"]);
  assert.deepEqual(calls[0][1], ["r1", "r2"]);
  assert.equal(calls[0][2].txHashChecked, txHash);
  assert.equal(calls[0][2].checkedHeight, 91);
  assert.equal(calls[0][2].leaseToken, "lease");
  assert.equal(calls[0][2].metadata.leaseToken, undefined);
  assert.equal(calls[0][2].metadata.nullifier_unspent_confirmed, true);
});

test("ProofReady failed recovery rejects a missing or mixed operation lease token", async () => {
  const manager = {
    async markBroadcastFailed() {
      assert.fail("invalid ProofReady recovery must not reach the reservation manager");
    }
  };
  const txHash = "bc".repeat(32);
  const record = (reservation_id, lease_token) => ({
    reservation_id,
    status: "ProofReady",
    lease_token,
    broadcast_in_flight: true,
    broadcast_attempt_count: 1,
    submitted_tx_hash: txHash
  });

  await assert.rejects(() => replanExplicitlyFailedReservations({
    manager,
    records: [record("r1", "")],
    txHash,
    checkedHeight: 91
  }), /one operation-wide lease token/);
  await assert.rejects(() => replanExplicitlyFailedReservations({
    manager,
    records: [record("r1", "lease-a"), record("r2", "lease-b")],
    txHash,
    checkedHeight: 91
  }), /one operation-wide lease token/);
});

test("Submitted failed reservations use the direct evidence-gated replan transition", async () => {
  let transition;
  const manager = {
    async markBroadcastFailed(ids, input) { transition = { ids, input }; }
  };
  const txHash = "cd".repeat(32);
  await replanExplicitlyFailedReservations({
    manager,
    records: [{ reservation_id: "r1", status: "Submitted", submitted_tx_hash: txHash }],
    txHash,
    checkedHeight: 17
  });
  assert.deepEqual(transition.ids, ["r1"]);
  assert.equal(transition.input.txHashChecked, txHash);
  assert.equal(transition.input.checkedHeight, 17);
  assert.equal(transition.input.nullifierUnspentConfirmed, true);
  assert.equal(transition.input.txAbsentOrFailedConfirmed, true);
});

test("Cosmos failed recovery accepts one durable tx-bytes identity for every linked reservation", async () => {
  let transition;
  const manager = {
    async markBroadcastFailed(ids, input) { transition = { ids, input }; }
  };
  const txHash = "de".repeat(32);
  const records = ["r1", "r2"].map(reservation_id => ({
    reservation_id,
    status: "ProofReady",
    lease_token: "lease",
    broadcast_in_flight: true,
    broadcast_attempt_count: 1,
    tx_bytes_hash: txHash
  }));
  await replanExplicitlyFailedReservations({
    manager,
    records,
    txHash,
    checkedHeight: 31,
    cosmosTransactionIdentity: true
  });
  assert.deepEqual(transition.ids, ["r1", "r2"]);
  assert.equal(transition.input.txHashChecked, txHash);

  await assert.rejects(() => replanExplicitlyFailedReservations({
    manager,
    records: [records[0], { ...records[1], tx_bytes_hash: "ff".repeat(32) }],
    txHash,
    checkedHeight: 31,
    cosmosTransactionIdentity: true
  }), /does not match every reservation/);
});

test("persisted CheckTx and pre-RPC rejection evidence can clear the matching account marker after restart", () => {
  const base = { status: "ReplanRequired", broadcast_in_flight: false };
  assert.equal(reservationHasExplicitBroadcastRejection({
    ...base,
    metadata: { check_tx_rejected: true, rpc_invoked: true }
  }), true);
  assert.equal(reservationHasExplicitBroadcastRejection({
    ...base,
    metadata: { broadcast_aborted_before_rpc: true, rpc_invoked: false }
  }), true);
  assert.equal(reservationHasExplicitBroadcastRejection({
    ...base,
    metadata: { check_tx_rejected: true, rpc_invoked: false }
  }), false);
  assert.equal(reservationHasExplicitBroadcastRejection({
    ...base,
    metadata: { rpc_invoked: true }
  }), false);
});

test("ProofReady failed transaction recovery either uses the SDK guard or fails closed without mutation", async () => {
  const store = new MemoryReservationStore();
  const manager = createNoteReservationManager({
    store,
    ownerKeyId: "chain:clair1owner",
    indexKey: "index-key-v1",
    leaseOwner: "browser-tab:test"
  });
  const batch = await preparePlanReservation(manager, {
    plan: { selectedNote: noteFixture() },
    kind: "withdraw"
  });
  await manager.markProofReady(batch.reservation_ids, { leaseToken: batch.lease_token });
  const txHash = "ef".repeat(32);
  await manager.markBroadcastAttempting(batch.reservation_ids, {
    leaseToken: batch.lease_token,
    txHash,
    reason: "test_broadcast"
  });
  const records = await Promise.all(batch.reservation_ids.map(id => manager.getReservation(id)));

  if (typeof manager.markBroadcastFailed !== "function") {
    await assert.rejects(() => replanExplicitlyFailedReservations({
      manager,
      records,
      txHash,
      checkedHeight: 42
    }), /left unchanged for manual review.*must not be retried/);
    const unchanged = await manager.getReservation(batch.reservation_ids[0]);
    assert.equal(unchanged.status, "ProofReady");
    assert.equal(unchanged.broadcast_in_flight, true);
    return;
  }

  await replanExplicitlyFailedReservations({
    manager,
    records,
    txHash,
    checkedHeight: 42
  });

  const recovered = await manager.getReservation(batch.reservation_ids[0]);
  assert.equal(recovered.status, "ReplanRequired");
  assert.equal(recovered.metadata.nullifier_unspent_confirmed, true);
  assert.equal(recovered.metadata.tx_absent_or_failed_confirmed, true);
  assert.equal(recovered.metadata.leaseToken, undefined);
});
