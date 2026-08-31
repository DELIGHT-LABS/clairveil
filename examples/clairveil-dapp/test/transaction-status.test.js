import assert from "node:assert/strict";
import test from "node:test";

import {
  evmBlockChainSnapshot,
  evmReceiptStatusKind,
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus,
  isEvmReceiptConfirmationPending,
  shouldEscalateSuccessfulTxWithUnspentNullifiers,
} from "../public/transaction-status.js";

test("EVM relay snapshots use latest EVM block time and height", () => {
  assert.deepEqual(
    evmBlockChainSnapshot({ timestamp: "0x64", number: "0x2a" }),
    { chainNowMs: 100_000, chainHeight: "42" },
  );
  assert.throws(
    () => evmBlockChainSnapshot({ timestamp: "", number: "0x2a" }),
    /Latest EVM block time or height is unavailable/,
  );
  assert.throws(
    () => evmBlockChainSnapshot({ timestamp: "0x20000000000000", number: "0x2a" }),
    /safe integer range/,
  );
});

test("EVM receipt status distinguishes explicit success, failure, and unknown", () => {
  for (const status of ["0x1", "0x01", "1", "01", 1, 1n]) {
    assert.equal(evmReceiptStatusKind(status), "success");
    assert.equal(hasSuccessfulEvmReceiptStatus({ status }), true);
  }
  for (const status of ["0x0", "0x00", "0", "00", 0, 0n]) {
    assert.equal(evmReceiptStatusKind(status), "failure");
    assert.equal(hasFailedEvmReceiptStatus({ status }), true);
  }
  for (const status of [undefined, null, "", "missing", "0x2", false, true]) {
    assert.equal(evmReceiptStatusKind(status), "unknown");
    assert.equal(hasFailedEvmReceiptStatus({ status }), false);
    assert.equal(hasSuccessfulEvmReceiptStatus({ status }), false);
  }
});

test("successful tx and unspent nullifier conflict waits for scan catch-up and grace", () => {
  const input = {
    txHeight: 100,
    scanHeight: 99,
    observedAtMs: 1_000,
    nowMs: 1_000_000,
    graceMs: 120_000,
  };
  assert.equal(shouldEscalateSuccessfulTxWithUnspentNullifiers(input), false);
  assert.equal(shouldEscalateSuccessfulTxWithUnspentNullifiers({
    ...input,
    scanHeight: 100,
    nowMs: 100_000,
  }), false);
  assert.equal(shouldEscalateSuccessfulTxWithUnspentNullifiers({
    ...input,
    scanHeight: 100,
  }), true);
});

test("missing EVM receipt remains submitted and pending instead of failed", () => {
  assert.equal(
    isEvmReceiptConfirmationPending({
      message: "EVM tx was broadcast but receipt was not found yet: ABC",
      txHash: "ABC",
      broadcast: { txHash: "ABC", receipt: null },
    }),
    true,
  );
  assert.equal(
    isEvmReceiptConfirmationPending({
      message: "EVM tx failed with receipt status 0x0",
      txHash: "ABC",
      broadcast: { txHash: "ABC", receipt: { status: "0x0" } },
    }),
    false,
  );
});
