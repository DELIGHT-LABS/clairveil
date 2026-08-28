import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createPrivacyScanValidationStateV2 } from "clairveiljs/scan";
import { typedBatchEventIdentity } from "../public/batch-event-identity.js";
import { canonicalBatchEvidenceHex } from "../public/batch-reconciliation.js";

const appSource = await readFile(new URL("../public/app.js", import.meta.url), "utf8");
const htmlSource = await readFile(new URL("../public/index.html", import.meta.url), "utf8");
const cssSource = await readFile(new URL("../public/styles.css", import.meta.url), "utf8");

function batchAuditHelpers() {
  const start = appSource.indexOf("const auditorBatchMaxPages");
  const end = appSource.indexOf("function clearAuditorBatchOutputs", start);
  assert.ok(start >= 0 && end > start, "batch audit helpers must remain inspectable");
  return new Function(
    "canonicalBatchEvidenceHex",
    "typedBatchEventIdentity",
    "createPrivacyScanValidationStateV2",
    "assertPrivacySession",
    "batchTransferMaxPayments",
    `"use strict"; ${appSource.slice(start, end)}; return {
      groupAuditableBatchTransactions,
      fetchAllAuditableBatchTransactions
    };`,
  )(
    canonicalBatchEvidenceHex,
    typedBatchEventIdentity,
    createPrivacyScanValidationStateV2,
    () => {},
    32,
  );
}

function bytes(fill, length = 32) {
  return new Uint8Array(length).fill(fill);
}

function batchSummary({ sequence, outputCount, txFill = 1, height = "10" }) {
  return {
    event_type: "batch_transfer",
    height,
    global_sequence: String(sequence),
    output_count: outputCount,
    tx_hash: bytes(txFill),
    effect_id: bytes(sequence + 20),
    audit_key_id: "audit-main",
    audit_key_epoch: "1",
    audit_target_pubkey: bytes(31),
  };
}

function batchOutput(summary, outputIndex) {
  return {
    event_type: "batch_transfer",
    height: summary.height,
    global_sequence: summary.global_sequence,
    output_index: outputIndex,
    tx_hash: summary.tx_hash,
    full_disclosure_digest: bytes(outputIndex + 40),
    audit_disclosure_payload: bytes(outputIndex + 50, 8),
    commitment: bytes(outputIndex + 60),
  };
}

test("auditor groups complete typed batch outputs into one row per transaction", () => {
  const { groupAuditableBatchTransactions } = batchAuditHelpers();
  const first = batchSummary({ sequence: 1, outputCount: 2 });
  const second = batchSummary({ sequence: 2, outputCount: 1 });
  const transactions = groupAuditableBatchTransactions(
    [first, second],
    [batchOutput(first, 0), batchOutput(first, 1), batchOutput(second, 0)],
  );

  assert.equal(transactions.length, 1);
  assert.equal(transactions[0].events.length, 2);
  assert.equal(transactions[0].outputCount, 3);
  assert.equal(transactions[0].txHash, canonicalBatchEvidenceHex(first.tx_hash).toUpperCase());

  assert.throws(
    () => groupAuditableBatchTransactions([first], [batchOutput(first, 0)]),
    /did not return every mandatory audit output/,
  );
  assert.throws(
    () => groupAuditableBatchTransactions(
      [{ ...first, output_count: 1 }],
      [{ ...batchOutput(first, 0), audit_disclosure_payload: new Uint8Array() }],
    ),
    /missing its mandatory audit disclosure/,
  );
});

test("auditor retains one validation state across typed cursor pages and rejects a stalled cursor", async () => {
  const { fetchAllAuditableBatchTransactions } = batchAuditHelpers();
  const summary = batchSummary({ sequence: 1, outputCount: 2 });
  const requests = [];
  const pages = [
    {
      summaries: [summary],
      outputs: [batchOutput(summary, 0)],
      next_cursor: { height: "10", global_sequence: "1", output_index: 0 },
      has_more: true,
    },
    {
      summaries: [summary],
      outputs: [batchOutput(summary, 1)],
      next_cursor: { height: "10", global_sequence: "1", output_index: 1 },
      has_more: false,
    },
  ];
  const client = {
    async fetchAuditableBatchTransfers(request) {
      requests.push(request);
      return pages.shift();
    },
  };
  const transactions = await fetchAllAuditableBatchTransactions(client, {});
  assert.equal(transactions[0].outputCount, 2);
  assert.equal(requests.length, 2);
  assert.equal(requests[0].validationState, requests[1].validationState);
  assert.deepEqual(requests[1].after, {
    height: "10",
    globalSequence: "1",
    outputIndex: 0,
  });

  await assert.rejects(
    () => fetchAllAuditableBatchTransactions({
      async fetchAuditableBatchTransfers() {
        return {
          summaries: [],
          outputs: [],
          next_cursor: { height: "0", global_sequence: "0", output_index: 0 },
          has_more: true,
        };
      },
    }, {}),
    /cursor did not advance/,
  );
});

test("auditor decodes every typed batch output before exposing any plaintext", () => {
  const decodeStart = appSource.indexOf("async function decodeAuditorTransfer");
  const decodeEnd = appSource.indexOf("function canConnectWallet", decodeStart);
  const decodeSource = appSource.slice(decodeStart, decodeEnd);
  const loopIndex = decodeSource.indexOf("for (const entry of auditorBatchOutputs(transaction))");
  const decodeIndex = decodeSource.indexOf("decodeBatchAuditDisclosure", loopIndex);
  const completeCheck = decodeSource.indexOf("entries.length !== transaction.outputCount", decodeIndex);
  const stateCommit = decodeSource.indexOf("state.auditor.decoded = { kind: \"batch\"", completeCheck);
  const renderIndex = decodeSource.indexOf("renderAuditorBatchReport", stateCommit);

  assert.ok(loopIndex >= 0 && decodeIndex > loopIndex);
  assert.ok(completeCheck > decodeIndex);
  assert.ok(stateCommit > completeCheck);
  assert.ok(renderIndex > stateCommit);
  assert.match(decodeSource, /if \(!view\.verified[\s\S]*failed disclosure verification/);
  assert.match(decodeSource, /catch \(error\) \{[\s\S]*state\.auditor\.decoded = null;[\s\S]*clearAuditorReport/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.fetchAuditableTransfers\(\)/);
  assert.match(appSource, /api\("\/api\/auditor\/decode"/);
  const typedBatchSource = appSource.slice(
    appSource.indexOf("function groupAuditableBatchTransactions"),
    appSource.indexOf("function clearAuditorBatchOutputs"),
  );
  assert.doesNotMatch(
    typedBatchSource,
    /eventAttribute\([^,]+,\s*"audit_disclosure_(?:digest|payload)"/,
  );
  assert.match(htmlSource, /id="auditorBatchOutputs"/);
  assert.match(cssSource, /\.auditor-batch-output\.verified/);
});
