import assert from "node:assert/strict";
import test from "node:test";

import {
  verifyEvmScanTransactionLink
} from "../public/cosmos-evm-transaction-correlation.js";

const scanTxHash = `0x${"12".repeat(32)}`;
const evmTxHash = `0x${"ab".repeat(32)}`;

function successfulCometTx({
  hash = scanTxHash,
  height = "42",
  code = 0,
  attributes = [{ key: "ethereumTxHash", value: evmTxHash, index: true }]
} = {}) {
  return {
    hash,
    height,
    tx_result: {
      code,
      events: [{
        type: "profile_defined_event_type",
        attributes
      }]
    }
  };
}

test("correlates distinct outer Comet and Ethereum hashes without an event-type assumption", () => {
  const evidence = verifyEvmScanTransactionLink({
    scanTxHash: scanTxHash.toUpperCase(),
    evmTxHash: evmTxHash.toUpperCase(),
    cometTransaction: { result: successfulCometTx() }
  });

  assert.deepEqual(evidence, {
    scanTxHash,
    evmTxHash,
    cometHeight: "42",
    cosmosTxSucceeded: true,
    ethereumTxHashEventMatched: true
  });
  assert.equal(Object.isFrozen(evidence), true);
});

test("requires a loaded Comet transaction", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({ scanTxHash, evmTxHash }),
    /Comet transaction result is required/
  );
});

test("rejects malformed input and Comet transaction hashes", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash: "0x12",
      evmTxHash,
      cometTransaction: successfulCometTx()
    }),
    /typed scan transaction hash must be a 32-byte/
  );
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash: "not-a-hash",
      cometTransaction: successfulCometTx()
    }),
    /expected Ethereum transaction hash must be a 32-byte/
  );
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({ hash: "ff".repeat(31) })
    }),
    /Comet transaction hash must be a 32-byte/
  );
});

test("rejects a Comet result for a different outer transaction", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({ hash: `0x${"34".repeat(32)}` })
    }),
    /does not match the typed scan transaction/
  );
});

test("requires an explicit numeric zero Comet execution code", () => {
  const missingCode = successfulCometTx();
  delete missingCode.tx_result.code;
  for (const cometTransaction of [
    missingCode,
    successfulCometTx({ code: "0" }),
    successfulCometTx({ code: 1 })
  ]) {
    assert.throws(
      () => verifyEvmScanTransactionLink({
        scanTxHash,
        evmTxHash,
        cometTransaction
      }),
      /did not explicitly succeed/
    );
  }
});

test("requires an indexed ethereumTxHash instead of accepting hash equality", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash: scanTxHash,
      cometTransaction: successfulCometTx({ attributes: [] })
    }),
    /missing an indexed ethereumTxHash/
  );
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({
        attributes: [{ key: "ethereumTxHash", value: evmTxHash, index: false }]
      })
    }),
    /missing an indexed ethereumTxHash/
  );
});

test("rejects malformed, mismatched, and conflicting ethereumTxHash attributes", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({
        attributes: [{ key: "ethereumTxHash", value: "0x12", index: true }]
      })
    }),
    /indexed ethereumTxHash must be a 32-byte/
  );
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({
        attributes: [{
          key: "ethereumTxHash",
          value: `0x${"cd".repeat(32)}`,
          index: true
        }]
      })
    }),
    /does not match the expected Ethereum transaction/
  );
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({
        attributes: [
          { key: "ethereumTxHash", value: evmTxHash, index: true },
          { key: "ethereumTxHash", value: `0x${"cd".repeat(32)}`, index: true }
        ]
      })
    }),
    /conflicting ethereumTxHash/
  );
});

test("rejects malformed Comet height and event containers", () => {
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: successfulCometTx({ height: "0" })
    }),
    /height must be a positive integer/
  );
  const malformedEvents = successfulCometTx();
  malformedEvents.tx_result.events = null;
  assert.throws(
    () => verifyEvmScanTransactionLink({
      scanTxHash,
      evmTxHash,
      cometTransaction: malformedEvents
    }),
    /events are required/
  );
});
