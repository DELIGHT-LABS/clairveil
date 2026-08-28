import assert from "node:assert/strict";
import test from "node:test";

import { createEvmBroadcastWatcher } from "../public/evm-broadcast-watch.js";

function deferred() {
  let resolve;
  let reject;
  const promise = new Promise((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, reject, resolve };
}

async function drainTasks() {
  await new Promise(resolve => setTimeout(resolve, 0));
}

test("ignores a receipt whose durable operation identity was already cleared", async () => {
  const receipt = deferred();
  let included = 0;
  const watcher = createEvmBroadcastWatcher();
  watcher.watch({ waitPromise: receipt.promise }, {
    key: "transfer",
    isCurrent: async () => false,
    onIncluded: () => { included += 1; },
  });

  receipt.resolve({ txHash: "0x1" });
  await drainTasks();
  assert.equal(included, 0);
});

test("a newer action invalidates an older callback while it is awaiting", async () => {
  const receipt = deferred();
  const callbackGate = deferred();
  const events = [];
  const watcher = createEvmBroadcastWatcher();
  watcher.watch({ waitPromise: receipt.promise }, {
    key: "deposit",
    isCurrent: () => true,
    onIncluded: async (_result, assertActive) => {
      events.push("started");
      await callbackGate.promise;
      assertActive();
      events.push("finished");
    },
    onFailed: () => events.push("failed"),
  });

  receipt.resolve({ txHash: "0x1" });
  await drainTasks();
  watcher.invalidateAll();
  callbackGate.resolve();
  await drainTasks();
  assert.deepEqual(events, ["started"]);
});

test("a callback failure does not downgrade an operation cleared during the callback", async () => {
  const receipt = deferred();
  let current = true;
  let failed = 0;
  const watcher = createEvmBroadcastWatcher();
  watcher.watch({ waitPromise: receipt.promise }, {
    key: "batch",
    isCurrent: () => current,
    onIncluded: async () => {
      current = false;
      throw new Error("late UI continuation");
    },
    onFailed: () => { failed += 1; },
  });

  receipt.resolve({ txHash: "0x1" });
  await drainTasks();
  assert.equal(failed, 0);
});

test("reports a current receipt failure exactly once", async () => {
  const receipt = deferred();
  const failures = [];
  const watcher = createEvmBroadcastWatcher();
  watcher.watch({ waitPromise: receipt.promise }, {
    key: "withdraw",
    isCurrent: () => true,
    onFailed: error => failures.push(error.message),
  });

  receipt.reject(new Error("receipt failed"));
  await drainTasks();
  assert.deepEqual(failures, ["receipt failed"]);
});
