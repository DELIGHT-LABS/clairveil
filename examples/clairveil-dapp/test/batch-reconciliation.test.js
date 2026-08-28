import assert from "node:assert/strict";
import test from "node:test";

import { advanceEvmBatchReceiptArtifact } from "../public/batch-reconciliation.js";

const txHash = `0x${"12".repeat(32)}`;
const operationEvidenceHash = "34".repeat(32);

function receiptEvidence(extra = {}) {
  return {
    txResult: { txHash },
    operationEvidenceHash,
    ...extra,
  };
}

test("late batch receipts cannot remove already verified typed effect evidence", () => {
  const typedReceipt = receiptEvidence({
    scanTransactionLink: { scanTxHash: `0x${"56".repeat(32)}`, evmTxHash: txHash },
    typedScanEvidence: { eventType: "batch_transfer", outputCount: 2 },
  });
  const artifact = {
    phase: "typed-effect-verified",
    txHash,
    receiptEvidence: typedReceipt,
  };

  const updated = advanceEvmBatchReceiptArtifact({
    artifact,
    receiptEvidence: receiptEvidence(),
    txHash,
    txBytesHash: "78".repeat(32),
  });

  assert.equal(updated.phase, "typed-effect-verified");
  assert.equal(updated.receiptEvidence, typedReceipt);
});

test("late batch receipts cannot replace a different transaction identity", () => {
  assert.throws(() => advanceEvmBatchReceiptArtifact({
    artifact: {
      phase: "receipt-verified",
      receiptEvidence: receiptEvidence(),
    },
    receiptEvidence: receiptEvidence(),
    txHash: `0x${"90".repeat(32)}`,
  }), /does not match its transaction identity|different receipt evidence/);
});
