import assert from "node:assert/strict";
import test from "node:test";

import {
  batchTransferErrorRequiresReconciliation,
  batchTransferNeedsReconciliation,
  batchTransferReservationsSucceeded,
  computeBatchTransferPreviewState,
  pendingBatchTransferPayments,
  preparedBatchTransferFacts,
  selectNextAtomicBatchPayments,
} from "../public/batch-transfer-state.js";
import {
  privacyEventSelectionKey,
  sameTypedBatchEventIdentity,
  typedBatchEventIdentity,
} from "../public/batch-event-identity.js";
import { assertTypedBatchEffect } from "../public/batch-reconciliation.js";

function hexBytes(value) {
  return Uint8Array.from(value.match(/../g), byte => Number.parseInt(byte, 16));
}

test("typed batch event identity separates messages sharing one transaction", () => {
  const selected = typedBatchEventIdentity({ height: "42", sequence: "7" });
  const first = typedBatchEventIdentity({
    height: "42",
    global_sequence: "7",
  });
  const second = typedBatchEventIdentity({
    height: "42",
    global_sequence: "8",
  });

  assert.equal(sameTypedBatchEventIdentity(selected, first), true);
  assert.equal(sameTypedBatchEventIdentity(selected, second), false);
});

test("typed batch event identity rejects an unsafe cursor", () => {
  assert.throws(
    () => typedBatchEventIdentity({ height: "42", sequence: "0" }),
    /global sequence/,
  );
});

test("Cosmos batch reconciliation binds tx, nullifiers, commitments, and per-payment disclosure", () => {
  const txHash = "aa".repeat(32);
  const nullifier = "bb".repeat(32);
  const commitment = "cc".repeat(32);
  const userDigest = "dd".repeat(32);
  const auditDigest = "ee".repeat(32);
  const summary = {
    tx_hash: hexBytes(txHash),
    nullifiers: [hexBytes(nullifier)],
    output_count: 1,
  };
  const output = {
    output_index: 0,
    commitment: hexBytes(commitment),
    user_privacy_policy: 1,
    user_disclosure_mode: "USER_DISCLOSURE_MODE_PUBLIC",
    user_disclosure_digest: hexBytes(userDigest),
    full_disclosure_digest: hexBytes(auditDigest),
  };
  const operationEvidence = {
    input_nullifier_hexes: [nullifier],
    expected_outputs: [{
      role: "payment",
      batch_item_index: 0,
      expected_output_commitment: commitment,
      expected_user_disclosure_digest: userDigest,
      expected_audit_disclosure_digest: auditDigest,
      user_privacy_policy: 1,
      user_disclosure_mode: 1,
    }],
  };

  assert.equal(assertTypedBatchEffect({
    summary,
    outputs: [output],
    operationEvidence,
    outputCount: 1,
    txHash,
  }), true);
  assert.throws(() => assertTypedBatchEffect({
    summary,
    outputs: [{ ...output, commitment: hexBytes("ff".repeat(32)) }],
    operationEvidence,
    outputCount: 1,
    txHash,
  }), /commitment does not match/);
});

test("privacy event selection keys distinguish messages in one transaction", () => {
  const txHash = "A".repeat(64);
  const first = privacyEventSelectionKey({
    tx_hash_hex: txHash,
    height: "42",
    sequence: "7",
  });
  const second = privacyEventSelectionKey({
    tx_hash_hex: txHash,
    height: "42",
    sequence: "8",
  });

  assert.notEqual(first, second);
  assert.match(first, /:42:7$/);
  assert.match(second, /:42:8$/);
});

test("batch preview computes exact one-batch capacity and change", () => {
  const preview = computeBatchTransferPreviewState({
    paymentAmounts: [10n, 20n],
    noteAmounts: [20n, 10n, 5n],
  });

  assert.equal(preview.total, 30n);
  assert.equal(preview.totalAvailable, 35n);
  assert.equal(preview.selectedCount, 2);
  assert.equal(preview.selectedTotal, 30n);
  assert.equal(preview.estimatedChange, 0n);
  assert.equal(preview.outputCount, 2);
  assert.equal(preview.requiresSplit, false);
  assert.deepEqual(preview.unsplittablePaymentIndexes, []);
});

test("batch preview rejects a payment larger than current 16-input capacity", () => {
  const preview = computeBatchTransferPreviewState({
    paymentAmounts: [17n, 1n],
    noteAmounts: Array.from({ length: 18 }, () => 1n),
  });

  assert.equal(preview.totalCovered, true);
  assert.equal(preview.oneBatchCapacity, 16n);
  assert.equal(preview.requiresSplit, true);
  assert.deepEqual(preview.unsplittablePaymentIndexes, [0]);
});

test("batch preview does not mislabel an empty available-note set as unsplittable payments", () => {
  const preview = computeBatchTransferPreviewState({
    paymentAmounts: [5n, 7n],
    noteAmounts: [],
  });

  assert.equal(preview.totalCovered, false);
  assert.equal(preview.oneBatchCapacity, 0n);
  assert.equal(preview.requiresSplit, false);
  assert.deepEqual(preview.unsplittablePaymentIndexes, []);
});

test("batch broadcast disposition preserves ambiguous submissions", () => {
  assert.equal(
    batchTransferNeedsReconciliation({
      noBroadcastAttempt: true,
      hasBroadcastEvidence: false,
    }),
    false,
  );
  assert.equal(
    batchTransferNeedsReconciliation({
      hasBroadcastEvidence: true,
    }),
    true,
  );
  assert.equal(
    batchTransferNeedsReconciliation({
      reservations: [{ status: "Unknown" }],
    }),
    true,
  );
  assert.equal(
    batchTransferNeedsReconciliation({
      reservations: [{ status: "ProofReady", broadcast_in_flight: true }],
    }),
    true,
  );
  assert.equal(
    batchTransferNeedsReconciliation({
      noBroadcastAttempt: true,
      reservations: [{ status: "Unknown" }],
    }),
    true,
  );
});

test("batch retry keeps only payments that have not already completed", () => {
  const payments = [
    { itemId: "A", amountValue: 10n },
    { itemId: "B", amountValue: 20n },
    { itemId: "C", amountValue: 30n },
  ];

  assert.deepEqual(
    pendingBatchTransferPayments(payments, new Set(["A", "B"])),
    [payments[2]],
  );
});

test("explicit atomic splitting retains a 32nd payment when it leaves no change", () => {
  const payments = Array.from({ length: 33 }, (_, index) => ({
    itemId: `payment-${index + 1}`,
    amountValue: 1n,
  }));

  assert.equal(
    selectNextAtomicBatchPayments(payments, {
      inputCapacity: 32n,
      maxOutputs: 32,
    }).length,
    32,
  );
  assert.equal(
    selectNextAtomicBatchPayments(payments, {
      inputCapacity: 33n,
      maxOutputs: 32,
    }).length,
    31,
  );
});

test("all reconciliation-required error shapes use the safe retry state", () => {
  assert.equal(
    batchTransferErrorRequiresReconciliation({
      reservationReconciliationRequired: true,
    }),
    true,
  );
  assert.equal(
    batchTransferErrorRequiresReconciliation({
      batchTransferReconciliationRequired: true,
    }),
    true,
  );
  assert.equal(batchTransferErrorRequiresReconciliation(new Error("failed")), false);
});

test("batch success requires matching aggregate operation evidence", () => {
  const evidenceHash = "ab".repeat(32);
  const succeeded = {
    status: "ConfirmedSpent",
    expected_operation_evidence_hash: evidenceHash,
    metadata: {
      operation_success_evidence_required: true,
      operation_status: "Succeeded",
      operation_success_evidence_matches: true,
    },
  };

  assert.equal(
    batchTransferReservationsSucceeded([succeeded], {
      expectedCount: 1,
      expectedOperationEvidenceHash: evidenceHash,
    }),
    true,
  );
  assert.equal(
    batchTransferReservationsSucceeded(
      [
        {
          ...succeeded,
          metadata: {
            ...succeeded.metadata,
            operation_status: "ConflictSpent",
            operation_success_evidence_matches: false,
          },
        },
      ],
      {
        expectedCount: 1,
        expectedOperationEvidenceHash: evidenceHash,
      },
    ),
    false,
  );
  assert.equal(
    batchTransferReservationsSucceeded([succeeded], {
      expectedCount: 1,
      expectedOperationEvidenceHash: "cd".repeat(32),
    }),
    false,
  );
});

test("prepared batch facts bind the final SDK effect to every requested row", () => {
  const requestedPayments = [
    {
      itemId: "A",
      amountValue: 10n,
      recipient: "clairs1alice",
      userPrivacyPolicy: "all-private",
      userDisclosureMode: "none",
    },
    {
      itemId: "B",
      amountValue: 20n,
      recipient: "clairs1bob",
      userPrivacyPolicy: "amount",
      userDisclosureMode: "public",
    },
  ];
  const prepared = {
    inputCount: 3,
    outputCount: 3,
    selectedInputTotal: "35",
    payload: {
      outputs: [
        {},
        {},
        {},
      ],
      message_outputs: [
        {},
        {},
        {},
      ],
    },
    operationEvidence: {
      expected_outputs: [
        { item_id: "A", role: "payment", batch_item_index: 0 },
        { item_id: "B", role: "payment", batch_item_index: 1 },
      ],
    },
    payments: [
      {
        itemId: "A",
        amount: "10uclair",
        recipient: "clairs1alice",
        privacyPolicy: "all-private",
        disclosureMode: "none",
      },
      {
        itemId: "B",
        amount: "20uclair",
        recipient: "clairs1bob",
        privacyPolicy: "amount",
        disclosureMode: "public",
      },
    ],
  };

  const facts = preparedBatchTransferFacts({
    requestedPayments,
    prepared,
    denom: "uclair",
  });
  assert.equal(facts.total, 30n);
  assert.equal(facts.change, 5n);
  assert.equal(facts.inputCount, 3);
  assert.equal(facts.outputCount, 3);
  assert.deepEqual(facts.disclosureCounts, {
    private: 1,
    public: 1,
    recipientEncrypted: 0,
  });

  assert.throws(
    () =>
      preparedBatchTransferFacts({
        requestedPayments,
        prepared: {
          ...prepared,
          payments: [
            prepared.payments[0],
            { ...prepared.payments[1], recipient: "clairs1mallory" },
          ],
        },
        denom: "uclair",
      }),
    /does not match its requested effect/,
  );
});

test("prepared batch facts bind a recipient-encrypted disclosure target", () => {
  const targetHex = "03".repeat(32);
  const otherTargetHex = "04".repeat(32);
  const targetBase64 = Buffer.from(targetHex, "hex").toString("base64");
  const requestedPayments = [
    {
      itemId: "A",
      amountValue: 10n,
      recipient: "clairs1alice",
      userPrivacyPolicy: "amount",
      userDisclosureMode: "recipient-encrypted",
      userDisclosureTargetPubKeyHex: targetHex,
    },
  ];
  const prepared = {
    inputCount: 1,
    outputCount: 1,
    selectedInputTotal: "10",
    payments: [
      {
        itemId: "A",
        amount: "10uclair",
        recipient: "clairs1alice",
        privacyPolicy: "amount",
        disclosureMode: "recipient-encrypted",
      },
    ],
    payload: {
      outputs: [{ disclosure_target_pubkey: targetBase64 }],
      message_outputs: [{ user_disclosure_target_pubkey: targetBase64 }],
    },
    operationEvidence: {
      expected_outputs: [
        { item_id: "A", role: "payment", batch_item_index: 0 },
      ],
    },
  };

  assert.equal(
    preparedBatchTransferFacts({
      requestedPayments,
      prepared,
      denom: "uclair",
    }).paymentCount,
    1,
  );
  assert.throws(
    () =>
      preparedBatchTransferFacts({
        requestedPayments: [
          {
            ...requestedPayments[0],
            userDisclosureTargetPubKeyHex: otherTargetHex,
          },
        ],
        prepared,
        denom: "uclair",
      }),
    /does not match its requested effect/,
  );
});
