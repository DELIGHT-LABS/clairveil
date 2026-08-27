import test from "node:test";
import assert from "node:assert/strict";
import {
  assertCosmosCheckTxAccepted,
  createRelayAccountSubmissionSerializer,
  createRelayWithdrawSubmissionGate,
  trackedCosmosSubmissionOutcome,
  trackedEvmSubmissionOutcome,
  waitForTrackedSubmissionOutcome,
} from "../server-relay-submission.js";

test("server relay gate coalesces concurrent and replayed payload submissions", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "aa".repeat(32),
    nullifier_hex: "bb".repeat(32),
  };
  let checks = 0;
  let submissions = 0;
  let release;
  let signalStarted;
  const pending = new Promise(resolve => {
    release = resolve;
  });
  const started = new Promise(resolve => {
    signalStarted = resolve;
  });
  const attempt = () => gate.run(payload, {
    checkNullifiers: async nullifiers => {
      checks += 1;
      return new Map(nullifiers.map(nullifier => [nullifier, false]));
    },
    submit: async markSubmissionStarted => {
      submissions += 1;
      markSubmissionStarted();
      signalStarted();
      await pending;
      return { broadcast: { txhash: "tx-a" } };
    },
  });

  const first = attempt();
  const concurrent = attempt();
  await started;
  assert.equal(checks, 1);
  assert.equal(submissions, 1);
  release();
  const expected = { broadcast: { txhash: "tx-a" } };
  assert.deepEqual(await first, expected);
  assert.deepEqual(await concurrent, expected);
  assert.deepEqual(await attempt(), expected);
  assert.equal(checks, 1);
  assert.equal(submissions, 1);
});

test("server relay gate rejects a second payload for the same input nullifier", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const nullifier = "cc".repeat(32);
  const firstPayload = {
    payload_hash: "aa".repeat(32),
    nullifier_hex: nullifier,
  };
  const conflictingPayload = {
    payload_hash: "dd".repeat(32),
    nullifier_hex: nullifier,
  };
  let release;
  const pending = new Promise(resolve => {
    release = resolve;
  });
  const first = gate.run(firstPayload, {
    checkNullifiers: async values => new Map(values.map(value => [value, false])),
    submit: async markSubmissionStarted => {
      markSubmissionStarted();
      await pending;
      return "submitted";
    },
  });
  await Promise.resolve();

  await assert.rejects(
    () => gate.run(conflictingPayload, {
      checkNullifiers: async values => new Map(values.map(value => [value, false])),
      submit: async () => "duplicate",
    }),
    /input nullifiers already have a submission attempt/,
  );
  release();
  assert.equal(await first, "submitted");
});

test("server relay gate releases preflight failures but retains ambiguous submissions", async () => {
  const payload = {
    payload_hash: "ee".repeat(32),
    nullifier_hex: "ff".repeat(32),
  };
  const gate = createRelayWithdrawSubmissionGate();
  let submissions = 0;

  await assert.rejects(
    () => gate.run(payload, {
      checkNullifiers: async () => new Map(),
      submit: async () => {
        submissions += 1;
      },
    }),
    /explicitly unspent/,
  );
  await assert.rejects(
    () => gate.run(payload, {
      checkNullifiers: async values => new Map(values.map(value => [value, false])),
      submit: async markSubmissionStarted => {
        submissions += 1;
        markSubmissionStarted();
        throw new Error("broadcast outcome unknown");
      },
    }),
    /broadcast outcome unknown/,
  );
  await assert.rejects(
    () => gate.run(payload, {
      checkNullifiers: async values => new Map(values.map(value => [value, false])),
      submit: async () => {
        submissions += 1;
      },
    }),
    /broadcast outcome unknown/,
  );
  assert.equal(submissions, 1);
});

test("Cosmos relay rejects an explicit non-zero CheckTx response", () => {
  assert.equal(assertCosmosCheckTxAccepted({ code: 0 }), 0);
  assert.equal(assertCosmosCheckTxAccepted({ code: "0" }), 0);
  assert.equal(assertCosmosCheckTxAccepted({ txhash: "legacy" }), null);

  assert.throws(
    () => assertCosmosCheckTxAccepted({
      code: "12",
      txhash: "ABCD",
      raw_log: "insufficient fee",
    }),
    error => error.message === "insufficient fee"
      && error.txHash === "ABCD"
      && error.txCode === 12
      && error.checkTxRejected === true
      && error.rpcInvoked === true,
  );
  assert.throws(
    () => assertCosmosCheckTxAccepted({ code: "not-a-code", txhash: "ABCD" }),
    /CheckTx returned an invalid code/,
  );
  assert.throws(
    () => assertCosmosCheckTxAccepted({ code: "9007199254740992", txhash: "ABCD" }),
    /CheckTx returned an invalid code/,
  );
});

test("malformed included Cosmos and EVM statuses remain exact-hash unknown outcomes", () => {
  const cosmosEvidence = { txHash: "ab".repeat(32) };
  const evmEvidence = { txHash: "cd".repeat(32) };
  const observed = [];
  const markUnknown = evidence => observed.push(evidence);

  assert.deepEqual(
    trackedCosmosSubmissionOutcome(
      { txhash: cosmosEvidence.txHash, code: "malformed" },
      cosmosEvidence,
      markUnknown,
    ),
    { included: true, pending: false, unknown: true, failed: null },
  );
  assert.deepEqual(
    trackedEvmSubmissionOutcome(
      { transactionHash: `0x${evmEvidence.txHash}`, status: "0x2" },
      evmEvidence,
      markUnknown,
    ),
    { included: true, pending: false, unknown: true, failed: null },
  );
  assert.deepEqual(observed, [cosmosEvidence, evmEvidence]);

  assert.deepEqual(
    trackedCosmosSubmissionOutcome({ code: 0 }, cosmosEvidence, markUnknown),
    { included: true, pending: false, unknown: false, failed: false },
  );
  assert.deepEqual(
    trackedCosmosSubmissionOutcome({ code: 12 }, cosmosEvidence, markUnknown),
    { included: true, pending: false, unknown: false, failed: true },
  );
  assert.deepEqual(
    trackedEvmSubmissionOutcome({ status: "0x1" }, evmEvidence, markUnknown),
    { included: true, pending: false, unknown: false, failed: false },
  );
  assert.deepEqual(
    trackedEvmSubmissionOutcome({ status: "0x0" }, evmEvidence, markUnknown),
    { included: true, pending: false, unknown: false, failed: true },
  );
});

test("a malformed included status cannot be cached as relay success", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "31".repeat(32),
    nullifier_hex: "32".repeat(32),
  };
  const evidence = { txHash: "33".repeat(32) };
  let submissions = 0;
  const attempt = () => gate.run(payload, {
    checkNullifiers: async values => new Map(values.map(value => [value, false])),
    submit: async (
      markSubmissionStarted,
      _markSubmissionRejected,
      _markIncludedExecutionFailed,
      recordSubmissionEvidence,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      recordSubmissionEvidence(evidence);
      return {
        broadcast: { txhash: evidence.txHash },
        ...trackedCosmosSubmissionOutcome(
          { txhash: evidence.txHash, code: "malformed" },
          evidence,
          () => {},
        ),
      };
    },
  });

  const expected = {
    broadcast: { txhash: evidence.txHash },
    included: true,
    pending: false,
    unknown: true,
    failed: null,
  };
  assert.deepEqual(await attempt(), expected);
  assert.deepEqual(await attempt(), expected);
  assert.equal(submissions, 1);
});

test("Cosmos explicit CheckTx rejection releases the server relay lock for retry", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "12".repeat(32),
    nullifier_hex: "34".repeat(32),
  };
  let submissions = 0;
  const run = tx => gate.run(payload, {
    checkNullifiers: async nullifiers => new Map(
      nullifiers.map(nullifier => [nullifier, false]),
    ),
    submit: async (markSubmissionStarted, markSubmissionRejected) => {
      submissions += 1;
      markSubmissionStarted();
      assertCosmosCheckTxAccepted(tx, { markSubmissionRejected });
      return "accepted";
    },
  });

  await assert.rejects(
    () => run({ code: 12, txhash: "ABCD", raw_log: "rejected" }),
    error => error.checkTxRejected === true && error.txCode === 12,
  );
  assert.equal(await run({ code: 0, txhash: "DCBA" }), "accepted");
  assert.equal(submissions, 2);
});

test("Cosmos malformed CheckTx response remains ambiguous and retains the relay lock", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "56".repeat(32),
    nullifier_hex: "78".repeat(32),
  };
  let submissions = 0;
  let reconciliations = 0;
  const evidence = {
    transport: "cosmos",
    chainId: "clairveil-local-2",
    txHash: "ab".repeat(32),
  };
  const run = () => gate.run(payload, {
    reconcile: async attempt => {
      reconciliations += 1;
      assert.deepEqual(attempt.evidence, evidence);
      return { included: false, failed: false };
    },
    checkNullifiers: async nullifiers => new Map(
      nullifiers.map(nullifier => [nullifier, false]),
    ),
    submit: async (
      markSubmissionStarted,
      markSubmissionRejected,
      _markIncludedExecutionFailed,
      recordSubmissionEvidence,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      recordSubmissionEvidence(evidence);
      assertCosmosCheckTxAccepted(
        { code: "malformed", txhash: evidence.txHash },
        { markSubmissionRejected },
      );
    },
  });

  await assert.rejects(run, /CheckTx returned an invalid code/);
  await assert.rejects(run, /CheckTx returned an invalid code/);
  assert.equal(submissions, 1);
  assert.equal(reconciliations, 1);
});

test("included execution failure releases the relay lock and reruns nullifier preflight", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "9a".repeat(32),
    nullifier_hex: "9b".repeat(32),
  };
  let checks = 0;
  let submissions = 0;
  const attempt = ({ terminalFailure = false, spent = false } = {}) => gate.run(payload, {
    checkNullifiers: async nullifiers => {
      checks += 1;
      return new Map(nullifiers.map(nullifier => [nullifier, spent]));
    },
    submit: async (
      markSubmissionStarted,
      _markSubmissionRejected,
      markIncludedExecutionFailed,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      if (terminalFailure) markIncludedExecutionFailed();
      return { failed: terminalFailure, attempt: submissions };
    },
  });

  assert.deepEqual(await attempt({ terminalFailure: true }), {
    failed: true,
    attempt: 1,
  });
  await assert.rejects(
    () => attempt({ spent: true }),
    /requires explicitly unspent/,
  );
  assert.equal(submissions, 1);
  assert.deepEqual(await attempt(), { failed: false, attempt: 2 });
  assert.equal(checks, 3);
  assert.equal(submissions, 2);
});

test("unclassified failed or pending result retains the relay lock", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "9c".repeat(32),
    nullifier_hex: "9d".repeat(32),
  };
  let checks = 0;
  let submissions = 0;
  const attempt = () => gate.run(payload, {
    checkNullifiers: async nullifiers => {
      checks += 1;
      return new Map(nullifiers.map(nullifier => [nullifier, false]));
    },
    submit: async markSubmissionStarted => {
      submissions += 1;
      markSubmissionStarted();
      return { failed: true, pending: true, attempt: submissions };
    },
  });

  const expected = { failed: true, pending: true, attempt: 1 };
  assert.deepEqual(await attempt(), expected);
  assert.deepEqual(await attempt(), expected);
  assert.equal(checks, 1);
  assert.equal(submissions, 1);
});

test("authoritative late execution failure releases a pending relay lock", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const nullifier = "9e".repeat(32);
  const pendingPayload = {
    payload_hash: "9f".repeat(32),
    nullifier_hex: nullifier,
  };
  const replacementPayload = {
    payload_hash: "a0".repeat(32),
    nullifier_hex: nullifier,
  };
  const evidence = {
    transport: "cosmos",
    chainId: "clairveil-local-2",
    txHash: "ab".repeat(32),
  };
  let checks = 0;
  let submissions = 0;

  const pending = await gate.run(pendingPayload, {
    checkNullifiers: async values => {
      checks += 1;
      return new Map(values.map(value => [value, false]));
    },
    submit: async (
      markSubmissionStarted,
      _markSubmissionRejected,
      _markIncludedExecutionFailed,
      recordSubmissionEvidence,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      recordSubmissionEvidence(evidence);
      return { included: false, pending: true, failed: false };
    },
  });
  assert.equal(pending.pending, true);

  const replacement = await gate.run(replacementPayload, {
    reconcile: async attempt => {
      assert.deepEqual(attempt.evidence, evidence);
      assert.equal(attempt.idempotencyKey, pendingPayload.payload_hash);
      return { included: true, failed: true };
    },
    checkNullifiers: async values => {
      checks += 1;
      return new Map(values.map(value => [value, false]));
    },
    submit: async markSubmissionStarted => {
      submissions += 1;
      markSubmissionStarted();
      return { included: true, pending: false, failed: false };
    },
  });
  assert.equal(replacement.included, true);
  assert.equal(checks, 2);
  assert.equal(submissions, 2);
});

test("authoritative late success refreshes the cached relay result without resubmission", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "a3".repeat(32),
    nullifier_hex: "a4".repeat(32),
  };
  const evidence = { txHash: "a5".repeat(32) };
  let submissions = 0;
  const options = {
    checkNullifiers: async values => new Map(values.map(value => [value, false])),
    submit: async (
      markSubmissionStarted,
      _markSubmissionRejected,
      _markIncludedExecutionFailed,
      recordSubmissionEvidence,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      recordSubmissionEvidence(evidence);
      return { included: false, pending: true, failed: false };
    },
  };
  await gate.run(payload, options);

  const authoritative = {
    included: true,
    pending: false,
    failed: false,
    tx: { code: 0 },
  };
  assert.deepEqual(await gate.run(payload, {
    ...options,
    reconcile: async attempt => {
      assert.deepEqual(attempt.evidence, evidence);
      return { included: true, failed: false, result: authoritative };
    },
  }), authoritative);
  assert.equal(submissions, 1);
});

test("read-only payload-hash reconciliation returns tx evidence without resubmitting", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  const payload = {
    payload_hash: "b3".repeat(32),
    nullifier_hex: "b4".repeat(32),
  };
  const evidence = {
    transport: "cosmos",
    chainId: "clairveil-local-2",
    txHash: "b5".repeat(32),
    privateWitness: "must-not-leave-the-coordinator",
  };
  let preflights = 0;
  let submissions = 0;
  const pendingResult = {
    broadcast: { txhash: evidence.txHash },
    included: false,
    pending: true,
    failed: false,
  };

  await gate.run(payload, {
    checkNullifiers: async values => {
      preflights += 1;
      return new Map(values.map(value => [value, false]));
    },
    submit: async (
      markSubmissionStarted,
      _markSubmissionRejected,
      _markIncludedExecutionFailed,
      recordSubmissionEvidence,
    ) => {
      submissions += 1;
      markSubmissionStarted();
      recordSubmissionEvidence(evidence);
      return pendingResult;
    },
  });

  const includedResult = {
    broadcast: { txhash: evidence.txHash },
    tx: { txhash: evidence.txHash, code: 0, height: "42" },
    included: true,
    pending: false,
    failed: false,
  };
  const status = await gate.reconcileByPayloadHash(payload.payload_hash, {
    reconcile: async attempt => {
      assert.equal(Object.isFrozen(attempt), true);
      assert.equal(attempt.idempotencyKey, payload.payload_hash);
      assert.equal(attempt.evidence.txHash, evidence.txHash);
      assert.deepEqual(attempt.result, pendingResult);
      return { included: true, failed: false, result: includedResult };
    },
  });

  assert.equal(status.found, true);
  assert.equal(status.settled, true);
  assert.equal(status.resolved, true);
  assert.equal(status.released, false);
  assert.deepEqual(status.evidence, {
    transport: evidence.transport,
    chainId: evidence.chainId,
    txHash: evidence.txHash,
  });
  assert.deepEqual(status.result, includedResult);
  assert.equal(Object.hasOwn(status.evidence, "privateWitness"), false);
  assert.equal(preflights, 1, "read-only reconciliation must not repeat nullifier preflight");
  assert.equal(submissions, 1, "read-only reconciliation must never invoke submit again");
});

test("read-only payload-hash reconciliation reports a missing attempt without callbacks", async () => {
  const gate = createRelayWithdrawSubmissionGate();
  let reconciliations = 0;
  const status = await gate.reconcileByPayloadHash("c3".repeat(32), {
    reconcile: async () => {
      reconciliations += 1;
      throw new Error("must not run");
    },
  });

  assert.deepEqual(status, {
    found: false,
    settled: false,
    resolved: false,
    released: false,
    evidence: null,
    result: null,
  });
  assert.equal(reconciliations, 0);
});

test("relay coordinator capacity never evicts a pending submission lock", async () => {
  const gate = createRelayWithdrawSubmissionGate({ maxEntries: 1 });
  const pendingPayload = {
    payload_hash: "a1".repeat(32),
    nullifier_hex: "b1".repeat(32),
  };
  const unrelatedPayload = {
    payload_hash: "a2".repeat(32),
    nullifier_hex: "b2".repeat(32),
  };

  const pending = await gate.run(pendingPayload, {
    checkNullifiers: async values => new Map(values.map(value => [value, false])),
    submit: async markSubmissionStarted => {
      markSubmissionStarted();
      return { included: false, pending: true, failed: false };
    },
  });
  assert.equal(pending.pending, true);

  await assert.rejects(
    () => gate.run(unrelatedPayload, {
      checkNullifiers: async values => new Map(values.map(value => [value, false])),
      submit: async () => ({ included: true, pending: false, failed: false }),
    }),
    /capacity is exhausted by in-flight requests/,
  );
});

test("relay coordinator capacity may evict only an included-success lock", async () => {
  const gate = createRelayWithdrawSubmissionGate({ maxEntries: 1 });
  const includedPayload = {
    payload_hash: "c1".repeat(32),
    nullifier_hex: "d1".repeat(32),
  };
  const nextPayload = {
    payload_hash: "c2".repeat(32),
    nullifier_hex: "d2".repeat(32),
  };
  const checkNullifiers = async values => new Map(values.map(value => [value, false]));

  assert.equal((await gate.run(includedPayload, {
    checkNullifiers,
    submit: async markSubmissionStarted => {
      markSubmissionStarted();
      return { included: true, pending: false, failed: false };
    },
  })).included, true);

  assert.equal((await gate.run(nextPayload, {
    checkNullifiers,
    submit: async markSubmissionStarted => {
      markSubmissionStarted();
      return { included: true, pending: false, failed: false };
    },
  })).included, true);
});

test("relay account submission serializer runs same-account payloads in FIFO order", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const order = [];
  let releaseFirst;
  let firstStarted;
  const firstCanFinish = new Promise(resolve => {
    releaseFirst = resolve;
  });
  const didStartFirst = new Promise(resolve => {
    firstStarted = resolve;
  });

  const first = serializer.run("cosmos:relayer", async () => {
    order.push("first:start");
    firstStarted();
    await firstCanFinish;
    order.push("first:end");
    return { payload: "first" };
  });
  const second = serializer.run("cosmos:relayer", async () => {
    order.push("second:start");
    order.push("second:end");
    return { payload: "second" };
  });

  await didStartFirst;
  await Promise.resolve();
  assert.deepEqual(order, ["first:start"]);
  releaseFirst();
  assert.deepEqual(await first, { payload: "first" });
  assert.deepEqual(await second, { payload: "second" });
  assert.deepEqual(order, [
    "first:start",
    "first:end",
    "second:start",
    "second:end",
  ]);
});

test("relay account submission serializer continues after a pre-boundary task fails", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const order = [];
  const first = serializer.run("evm:dev0", async () => {
    order.push("first");
    throw new Error("preflight failed");
  });
  const second = serializer.run("evm:dev0", async () => {
    order.push("second");
    return "submitted";
  });

  await assert.rejects(first, /preflight failed/);
  assert.equal(await second, "submitted");
  assert.deepEqual(order, ["first", "second"]);
});

test("relay account submission serializer continues after explicit rejection", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "cosmos:relayer:checktx";
  let submissions = 0;

  await assert.rejects(
    () => serializer.run(
      accountKey,
      async (
        markSubmissionStarted,
        markSubmissionRejected,
        _markSubmissionOutcomeUnknown,
        recordSubmissionEvidence,
      ) => {
        submissions += 1;
        markSubmissionStarted();
        recordSubmissionEvidence({ txHash: "ef".repeat(32) });
        markSubmissionRejected();
        throw new Error("CheckTx rejected sequence");
      },
    ),
    /CheckTx rejected sequence/,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), false);
  assert.equal(
    await serializer.run(accountKey, async () => {
      submissions += 1;
      return "retried";
    }),
    "retried",
  );
  assert.equal(submissions, 2);
});

test("malformed Cosmos CheckTx preserves a returned exact hash for account reconciliation", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "cosmos:relayer:malformed-checktx";
  const evidence = {
    transport: "cosmos",
    chainId: "clairveil-local-2",
    txHash: "ab".repeat(32),
  };

  await assert.rejects(
    () => serializer.run(
      accountKey,
      async (
        markSubmissionStarted,
        markSubmissionRejected,
        _markSubmissionOutcomeUnknown,
        recordSubmissionEvidence,
      ) => {
        markSubmissionStarted();
        recordSubmissionEvidence(evidence);
        assertCosmosCheckTxAccepted(
          { code: "malformed", txhash: evidence.txHash },
          { markSubmissionRejected },
        );
      },
    ),
    /CheckTx returned an invalid code/,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), true);

  assert.equal(await serializer.run(accountKey, {
    reconcileUnknown: async observed => {
      assert.deepEqual(observed, evidence);
      return { resolved: true, tx: { code: 0 } };
    },
    submit: async () => "continued after exact-hash reconciliation",
  }), "continued after exact-hash reconciliation");
  assert.equal(serializer.hasUnknownOutcome(accountKey), false);
});

test("ambiguous post-submit error fences the signer account until reconciliation", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "cosmos:relayer:ambiguous";
  let callbacks = 0;

  await assert.rejects(
    () => serializer.run(accountKey, async (
      markSubmissionStarted,
      _markSubmissionRejected,
      markSubmissionOutcomeUnknown,
    ) => {
      callbacks += 1;
      markSubmissionStarted();
      markSubmissionOutcomeUnknown({
        transport: "cosmos",
        txHash: "ab".repeat(32),
      });
      throw new Error("RPC response lost after submission");
    }),
    /RPC response lost/,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), true);
  await assert.rejects(
    () => serializer.run(accountKey, {
      reconcileUnknown: async evidence => {
        assert.equal(evidence.txHash, "ab".repeat(32));
        return false;
      },
      submit: async () => {
        callbacks += 1;
        return "unsafe second payload";
      },
    }),
    error => error.code === "SIGNER_ACCOUNT_SUBMISSION_UNKNOWN"
      && /reconcile it before another submission/.test(error.message),
  );
  assert.equal(callbacks, 1);

  assert.equal(
    await serializer.run(accountKey, {
      reconcileUnknown: async evidence => ({
        resolved: evidence.txHash === "ab".repeat(32),
        tx: { code: 0 },
      }),
      submit: async () => {
        callbacks += 1;
        return "safe after reconciliation";
      },
    }),
    "safe after reconciliation",
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), false);
  assert.equal(callbacks, 2);
});

test("polling exceptions fence the signer account with the exact transaction evidence", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "cosmos:relayer:polling-exception";
  const evidence = {
    transport: "cosmos",
    chainId: "clairveil-local-2",
    txHash: "bc".repeat(32),
  };

  await assert.rejects(
    () => serializer.run(accountKey, async (
      markSubmissionStarted,
      _markSubmissionRejected,
      markSubmissionOutcomeUnknown,
    ) => {
      markSubmissionStarted();
      await waitForTrackedSubmissionOutcome({
        waitForOutcome: async () => {
          throw new Error("RPC disconnected during transaction lookup");
        },
        markSubmissionOutcomeUnknown,
        evidence,
      });
    }),
    /RPC disconnected/,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), true);

  assert.equal(await serializer.run(accountKey, {
    reconcileUnknown: async observed => {
      assert.deepEqual(observed, evidence);
      return { resolved: true, tx: { code: 0 } };
    },
    submit: async () => "continued after exact-hash reconciliation",
  }), "continued after exact-hash reconciliation");
});

test("manual account reconciliation cannot clear a fence without authoritative evidence", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "cosmos:relayer:manual-reconcile";
  await serializer.run(
    accountKey,
    async (started, _rejected, unknown) => {
      started();
      unknown({ txHash: "cd".repeat(32) });
      return { pending: true };
    },
  );

  assert.equal(
    await serializer.reconcileUnknownOutcome(accountKey, async () => false),
    false,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), true);
  assert.equal(
    await serializer.reconcileUnknownOutcome(
      accountKey,
      async evidence => ({ resolved: evidence.txHash === "cd".repeat(32) }),
    ),
    true,
  );
  assert.equal(serializer.hasUnknownOutcome(accountKey), false);
});

test("pending account submission result fences later payloads", async () => {
  const serializer = createRelayAccountSubmissionSerializer();
  const accountKey = "evm:dev0:pending";
  const pending = await serializer.run(
    accountKey,
    async (markSubmissionStarted, _markSubmissionRejected, markSubmissionOutcomeUnknown) => {
      markSubmissionStarted();
      markSubmissionOutcomeUnknown();
      return { txHash: "ab".repeat(32), pending: true };
    },
  );
  assert.equal(pending.pending, true);
  assert.equal(serializer.hasUnknownOutcome(accountKey), true);
  await assert.rejects(
    () => serializer.run(accountKey, async () => "another nonce"),
    error => error.code === "SIGNER_ACCOUNT_SUBMISSION_UNKNOWN",
  );
});
