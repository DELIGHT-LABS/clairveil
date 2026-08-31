import assert from "node:assert/strict";
import test from "node:test";

import { createBrowserTaskCoordinator } from "../public/browser-task-coordinator.js";

function deferred() {
  let resolve;
  const promise = new Promise(done => {
    resolve = done;
  });
  return { promise, resolve };
}

test("browser task coordinator serializes reset and background scan work in one tab", async () => {
  const coordinator = createBrowserTaskCoordinator({ locks: null });
  const firstGate = deferred();
  const order = [];
  const first = coordinator.run("note-store", async () => {
    order.push("first:start");
    await firstGate.promise;
    order.push("first:end");
  });
  const second = coordinator.run("note-store", async () => {
    order.push("second:start");
    order.push("second:end");
  });

  await Promise.resolve();
  assert.equal(coordinator.pending, 2);
  assert.deepEqual(order, ["first:start"]);
  firstGate.resolve();
  await Promise.all([first, second]);
  assert.deepEqual(order, ["first:start", "first:end", "second:start", "second:end"]);
  assert.equal(coordinator.pending, 0);
});

test("separate tab coordinators share the browser lock boundary", async () => {
  const lockTails = new Map();
  const locks = {
    request(name, _options, task) {
      const previous = lockTails.get(name) || Promise.resolve();
      const operation = previous.then(task, task);
      lockTails.set(name, operation.catch(() => {}));
      return operation;
    },
  };
  const firstTab = createBrowserTaskCoordinator({ locks });
  const secondTab = createBrowserTaskCoordinator({ locks });
  const firstGate = deferred();
  let active = 0;
  let maximumActive = 0;

  const run = (coordinator, gate) => coordinator.run("shared-note-store", async () => {
    active += 1;
    maximumActive = Math.max(maximumActive, active);
    if (gate) await gate.promise;
    active -= 1;
  });
  const first = run(firstTab, firstGate);
  const second = run(secondTab);
  await Promise.resolve();
  await Promise.resolve();
  assert.equal(maximumActive, 1);
  firstGate.resolve();
  await Promise.all([first, second]);
  assert.equal(maximumActive, 1);
});

test("reset detaches a new scope from a hung prior generation", async () => {
  const coordinator = createBrowserTaskCoordinator({ locks: null });
  const oldGate = deferred();
  const order = [];
  const oldOperation = coordinator.run("old-account", async () => {
    order.push("old:start");
    await oldGate.promise;
    order.push("old:end");
  });

  await Promise.resolve();
  assert.equal(coordinator.pending, 1);
  assert.equal(coordinator.generation, 0);

  coordinator.reset();
  assert.equal(coordinator.pending, 0);
  assert.equal(coordinator.generation, 1);

  await coordinator.run("new-account", async () => {
    order.push("new:start");
    order.push("new:end");
  });
  assert.deepEqual(order, ["old:start", "new:start", "new:end"]);
  assert.equal(coordinator.pending, 0);

  oldGate.resolve();
  await oldOperation;
  assert.deepEqual(order, ["old:start", "new:start", "new:end", "old:end"]);
  assert.equal(coordinator.pending, 0);
});
