import assert from "node:assert/strict";
import test from "node:test";

import {
  findVerifiedEvmTypedScanEffect,
} from "../public/evm-typed-scan-evidence.js";

const evmTxHash = `0x${"ab".repeat(32)}`;
const scanTxHash = "12".repeat(32);
const otherScanTxHash = "34".repeat(32);
const nullifierA = "45".repeat(32);
const nullifierB = "56".repeat(32);
const commitmentA = "67".repeat(32);
const commitmentB = "78".repeat(32);

function hexBytes(value) {
  return Uint8Array.from(value.replace(/^0x/i, "").match(/../g), byte => (
    Number.parseInt(byte, 16)
  ));
}

function summary({
  height = 10,
  sequence = 1,
  type = "profile_defined_transfer",
  txHash = scanTxHash,
  outputCount = 1,
  nullifiers = [nullifierA],
} = {}) {
  return {
    height,
    global_sequence: sequence,
    event_type: type,
    tx_hash: hexBytes(txHash),
    output_count: outputCount,
    nullifiers: nullifiers.map(hexBytes),
  };
}

function output({
  height = 10,
  sequence = 1,
  index = 0,
  type = "profile_defined_transfer",
  txHash = scanTxHash,
  commitment = commitmentA,
} = {}) {
  return {
    height,
    global_sequence: sequence,
    output_index: index,
    event_type: type,
    tx_hash: hexBytes(txHash),
    commitment: hexBytes(commitment),
  };
}

function cursor(height, sequence, index) {
  return { height, global_sequence: sequence, output_index: index };
}

function page({ summaries = [], outputs = [], next, hasMore = false } = {}) {
  return {
    summaries,
    outputs,
    next_cursor: next,
    has_more: hasMore,
    scanned_event_count: summaries.length,
    encoded_bytes: 1024,
  };
}

function linkVerifier(calls = []) {
  return async (outerHash, ethereumHash) => {
    calls.push([outerHash, ethereumHash]);
    return Object.freeze({ outerHash, ethereumHash, verified: true });
  };
}

test("collects a complete deposit effect and verifies its outer transaction link", async () => {
  const requests = [];
  const calls = [];
  const depositSummary = summary({
    type: "profile_defined_deposit",
    outputCount: 1,
    nullifiers: [],
  });
  const depositOutput = output({ type: "profile_defined_deposit" });
  const evidence = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async request => {
      requests.push(request);
      return page({
        summaries: [depositSummary],
        outputs: [depositOutput],
        next: cursor(10, 1, 0),
      });
    },
    verifyTransactionLink: linkVerifier(calls),
    evmTxHash,
    expectedEventTypes: ["profile_defined_deposit"],
    expectedCommitments: [commitmentA],
    afterHeight: 9,
  });

  assert.equal(evidence.summary, depositSummary);
  assert.deepEqual(evidence.outputs, [depositOutput]);
  assert.equal(Object.isFrozen(evidence), true);
  assert.equal(Object.isFrozen(evidence.outputs), true);
  assert.deepEqual(calls, [[`0x${scanTxHash}`, evmTxHash]]);
  assert.deepEqual(requests[0].after, {
    height: 9,
    globalSequence: 0,
    outputIndex: 0,
  });
  assert.deepEqual(requests[0].eventTypes, []);
});

test("collects a split-page transfer by immutable event identity", async () => {
  const transferSummary = summary({
    outputCount: 2,
    nullifiers: [nullifierA, nullifierB],
  });
  const pages = [
    page({
      summaries: [transferSummary],
      outputs: [output()],
      next: cursor(10, 1, 0),
      hasMore: true,
    }),
    page({
      summaries: [transferSummary],
      outputs: [output({ index: 1, commitment: commitmentB })],
      next: cursor(10, 1, 1),
    }),
  ];
  const evidence = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => pages.shift(),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    expectedCommitments: [commitmentB],
    afterHeight: 9,
  });

  assert.deepEqual(evidence.outputs, [
    expectOutputIndex(0),
    expectOutputIndex(1, commitmentB),
  ]);
});

test("stops after completing the receipt height instead of scanning to chain head", async () => {
  let requests = 0;
  const matchingSummary = summary();
  const laterSummary = summary({
    height: 11,
    sequence: 2,
    txHash: otherScanTxHash,
  });
  const evidence = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => {
      requests += 1;
      if (requests > 1) throw new Error("collector scanned past the receipt height");
      return page({
        summaries: [matchingSummary, laterSummary],
        outputs: [output()],
        next: cursor(11, 2, 0),
        hasMore: true,
      });
    },
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
    throughHeight: 10,
  });

  assert.equal(evidence.summary, matchingSummary);
  assert.equal(requests, 1);
});

function expectOutputIndex(index, commitment = commitmentA) {
  return output({ index, commitment });
}

test("supports zero-output withdraw effects", async () => {
  const withdrawSummary = summary({
    type: "profile_defined_withdraw",
    outputCount: 0,
    nullifiers: [nullifierA],
  });
  const evidence = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({
      summaries: [withdrawSummary],
      next: cursor(10, 1, 0),
    }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_withdraw"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  });

  assert.equal(evidence.summary, withdrawSummary);
  assert.deepEqual(evidence.outputs, []);
});

test("supports a generic multi-output batch without chain-specific event names", async () => {
  const batchSummary = summary({
    type: "another_profile_batch_event",
    outputCount: 2,
    nullifiers: [nullifierA, nullifierB],
  });
  const evidence = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({
      summaries: [batchSummary],
      outputs: [
        output({ type: "another_profile_batch_event" }),
        output({
          type: "another_profile_batch_event",
          index: 1,
          commitment: commitmentB,
        }),
      ],
      next: cursor(10, 1, 1),
    }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["another_profile_batch_event"],
    expectedNullifiers: [nullifierA, nullifierB],
    expectedCommitments: [commitmentA, commitmentB],
    afterHeight: 9,
  });

  assert.equal(evidence.outputs.length, 2);
});

test("returns null when no complete effect matches the expected evidence", async () => {
  const result = await findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({ next: cursor(9, 0, 0) }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_deposit"],
    expectedCommitments: [commitmentA],
    afterHeight: 9,
  });

  assert.equal(result, null);
});

test("fails closed when multiple complete effects match", async () => {
  const first = summary({ type: "profile_defined_deposit", nullifiers: [] });
  const second = summary({
    height: 11,
    sequence: 2,
    type: "profile_defined_deposit",
    txHash: otherScanTxHash,
    nullifiers: [],
  });
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({
      summaries: [first, second],
      outputs: [
        output({ type: "profile_defined_deposit" }),
        output({
          height: 11,
          sequence: 2,
          type: "profile_defined_deposit",
          txHash: otherScanTxHash,
        }),
      ],
      next: cursor(11, 2, 0),
    }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_deposit"],
    expectedCommitments: [commitmentA],
    afterHeight: 9,
  }), /multiple effects.*ambiguous/);
});

test("rejects conflicting summary and output evidence across pages", async () => {
  const firstSummary = summary({ outputCount: 2 });
  const conflictingSummary = summary({ outputCount: 2, txHash: otherScanTxHash });
  const conflictingPages = [
    page({
      summaries: [firstSummary],
      outputs: [output()],
      next: cursor(10, 1, 0),
      hasMore: true,
    }),
    page({
      summaries: [conflictingSummary],
      outputs: [output({ index: 1, txHash: otherScanTxHash, commitment: commitmentB })],
      next: cursor(10, 1, 1),
    }),
  ];
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => conflictingPages.shift(),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  }), /conflicting summary evidence/);

  const summaryRecord = summary({ outputCount: 2 });
  const conflictingOutputPages = [
    page({
      summaries: [summaryRecord],
      outputs: [output()],
      next: cursor(10, 1, 0),
      hasMore: true,
    }),
    page({
      summaries: [summaryRecord],
      outputs: [
        output({ commitment: commitmentB }),
        output({ index: 1, commitment: commitmentB }),
      ],
      next: cursor(10, 1, 1),
    }),
  ];
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => conflictingOutputPages.shift(),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  }), /conflicting output evidence/);
});

test("rejects stalled, regressed, and malformed cursors", async () => {
  const common = {
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_withdraw"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  };
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...common,
    fetchScanPage: async () => page({ next: cursor(9, 0, 0), hasMore: true }),
  }), /did not advance/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...common,
    fetchScanPage: async () => page({ next: cursor(8, 1, 0) }),
  }), /regressed/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...common,
    fetchScanPage: async () => ({
      summaries: [],
      outputs: [],
      next_cursor: cursor(9, 0, 0),
      nextCursor: { height: 10, globalSequence: 1, outputIndex: 0 },
      has_more: false,
    }),
  }), /aliases do not match/);
});

test("rejects orphaned and incomplete output evidence", async () => {
  const common = {
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  };
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...common,
    fetchScanPage: async () => page({
      outputs: [output()],
      next: cursor(10, 1, 0),
    }),
  }), /no matching summary/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...common,
    fetchScanPage: async () => page({
      summaries: [summary({ outputCount: 2 })],
      outputs: [output()],
      next: cursor(10, 1, 0),
    }),
  }), /complete output set/);
});

test("propagates transaction-link failures and requires evidence", async () => {
  const options = {
    fetchScanPage: async () => page({
      summaries: [summary()],
      outputs: [output()],
      next: cursor(10, 1, 0),
    }),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
  };
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...options,
    verifyTransactionLink: async () => { throw new Error("outer link mismatch"); },
  }), /outer link mismatch/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    ...options,
    verifyTransactionLink: async () => false,
  }), /did not return evidence/);
});

test("enforces page limits and validates expected identities", async () => {
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async request => page({
      next: cursor(request.after.height + 1, 0, 0),
      hasMore: true,
    }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
    afterHeight: 9,
    maxPages: 2,
  }), /maximum page count/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({ next: cursor(0, 0, 0) }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash: "0x12",
    expectedEventTypes: ["profile_defined_transfer"],
    expectedNullifiers: [nullifierA],
  }), /Ethereum transaction hash must be exactly 32 bytes/);
  await assert.rejects(() => findVerifiedEvmTypedScanEffect({
    fetchScanPage: async () => page({ next: cursor(0, 0, 0) }),
    verifyTransactionLink: linkVerifier(),
    evmTxHash,
    expectedEventTypes: [],
    expectedNullifiers: [nullifierA],
  }), /event types must be a non-empty array/);
});
