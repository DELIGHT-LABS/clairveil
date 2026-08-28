import test from "node:test";
import assert from "node:assert/strict";
import {
  createNoteReservationManager,
  MemoryReservationStore,
  operationStatuses,
  preparePlanReservation,
  reservationStatuses
} from "clairveiljs/reservation";
import {
  directEvmOperationSuccessEvidence,
  isAmbiguousEvmSubmissionError,
  verifiedEvmTransactionResult
} from "../public/evm-reconciliation.js";

function completeEvmResult({
  txHash = `0x${"12".repeat(32)}`,
  txBytesHash = "34".repeat(32)
} = {}) {
  return {
    txHash,
    txBytesHash,
    receipt: { transactionHash: txHash, status: "0x1", logs: [] },
    transactionVerification: { verified: true },
    privacyReceipt: { verified: true },
    finality: { verified: true, mode: "safe" },
    evmTransactionVerified: true,
    evmPrivacyReceiptVerified: true,
    evmFinalityVerified: true
  };
}

test("verified EVM evidence retains finality and rejects receipt-only success", () => {
  const complete = completeEvmResult();
  assert.deepEqual(verifiedEvmTransactionResult(complete).finality, complete.finality);
  assert.throws(
    () => verifiedEvmTransactionResult({ ...complete, finality: null, evmFinalityVerified: false }),
    /complete transaction, receipt, privacy-event, and finality evidence/
  );
});

test("only a crossed wallet submission boundary without a hash is ambiguous", () => {
  assert.equal(isAmbiguousEvmSubmissionError({ evmSubmissionAttempted: true }), true);
  assert.equal(isAmbiguousEvmSubmissionError({ evmSubmissionAttempted: true, code: 4001 }), false);
  assert.equal(isAmbiguousEvmSubmissionError(new Error("preflight failed")), false);
  assert.equal(isAmbiguousEvmSubmissionError(
    { evmSubmissionAttempted: true },
    `0x${"ab".repeat(32)}`
  ), false);
});

test("direct EVM DApp evidence converges a submitted reservation to succeeded", async () => {
  const store = new MemoryReservationStore();
  const manager = createNoteReservationManager({
    store,
    ownerKeyId: "evm:0x1111111111111111111111111111111111111111",
    indexKey: "index-key-v1"
  });
  const note = {
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
    nullifier: "56".repeat(32),
    isSpent: false,
    nullifierStatus: "unspent",
    txHash: "source",
    height: 10,
    sequence: 1
  };
  const reservation = await preparePlanReservation(manager, {
    plan: { selectedNote: note },
    kind: "transfer"
  });
  const result = completeEvmResult();
  await manager.markProofReady(reservation.reservation_ids, {
    leaseToken: reservation.lease_token,
    executionTransport: "evm",
    txBytesHash: result.txBytesHash,
    expectedOutputCommitment: "OUTPUT",
    expectedDisclosureDigest: "DISCLOSURE",
    expectedRecipientHash: "RECIPIENT",
    expectedAmount: "4",
    expectedAmountHash: "AMOUNT",
    expectedDenom: "uclair",
    batchItemIndex: 0,
    batchItemIndexKnown: true,
    operationSuccessEvidenceRequired: true
  });
  await manager.markBroadcastAttempting(reservation.reservation_ids, {
    leaseToken: reservation.lease_token,
    txBytesHash: result.txBytesHash,
    reason: "dapp_evm_test"
  });
  await manager.markSubmitted(reservation.reservation_ids, {
    leaseToken: reservation.lease_token,
    txHash: result.txHash,
    txBytesHash: result.txBytesHash
  });
  const records = await Promise.all(
    reservation.reservation_ids.map(id => store.getReservation(id))
  );
  const operationSuccessEvidence = directEvmOperationSuccessEvidence(records, result);
  await manager.reconcileSpentNotes([{
    ...note,
    isSpent: true,
    nullifierStatus: "spent",
    operationSuccessEvidence
  }]);

  const reconciled = await store.getReservation(reservation.reservation_ids[0]);
  assert.equal(reconciled.status, reservationStatuses.ConfirmedSpent);
  assert.equal(reconciled.metadata.operation_status, operationStatuses.Succeeded);
});

test("direct EVM evidence rejects a transaction artifact mismatch", () => {
  const result = completeEvmResult();
  assert.throws(() => directEvmOperationSuccessEvidence([{
    submitted_tx_hash: result.txHash,
    tx_bytes_hash: "ff".repeat(32)
  }], result), /transaction binding does not match/);
});
