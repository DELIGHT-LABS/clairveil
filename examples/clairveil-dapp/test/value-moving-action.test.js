import assert from "node:assert/strict";
import test from "node:test";

import { createValueMovingActionGate } from "../public/value-moving-action.js";

test("double-clicking a deposit joins one value-moving action", async () => {
  const gate = createValueMovingActionGate();
  let calls = 0;
  let release;
  const blocked = new Promise(resolve => { release = resolve; });

  const first = gate.run("privacy-deposit", async () => {
    calls += 1;
    await blocked;
    return "submitted";
  });
  const second = gate.run("privacy-deposit", () => {
    calls += 1;
    return "duplicate";
  });

  assert.strictEqual(second, first);
  assert.equal(gate.active, true);
  assert.equal(gate.action, "privacy-deposit");
  assert.equal(calls, 1);
  release();
  assert.equal(await first, "submitted");
  assert.equal(gate.active, false);
});

test("a different value-moving action cannot enter while one is active", async () => {
  const gate = createValueMovingActionGate();
  let release;
  const blocked = new Promise(resolve => { release = resolve; });
  let withdrawCalls = 0;
  const deposit = gate.run("privacy-deposit", () => blocked);
  const withdraw = gate.run("privacy-withdraw", () => { withdrawCalls += 1; });

  assert.strictEqual(withdraw, deposit);
  assert.equal(withdrawCalls, 0);
  release("included");
  assert.equal(await deposit, "included");
});

test("session invalidation releases the UI gate without letting the old task clear a new action", async () => {
  const gate = createValueMovingActionGate();
  let releaseOld;
  const old = gate.run("privacy-transfer", () => new Promise(resolve => { releaseOld = resolve; }));
  gate.invalidate();
  let releaseNew;
  const current = gate.run("privacy-withdraw", () => new Promise(resolve => { releaseNew = resolve; }));
  releaseOld("stale");
  assert.equal(await old, "stale");
  assert.equal(gate.active, true);
  assert.equal(gate.action, "privacy-withdraw");
  releaseNew("current");
  assert.equal(await current, "current");
  assert.equal(gate.active, false);
});
