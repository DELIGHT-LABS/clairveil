import test from "node:test";
import assert from "node:assert/strict";
import { reservationStatuses } from "clairveiljs/reservation";
import {
  canRelaySnapshotBeSubmitted,
  canReplanExpiredLocalReservation,
  cosmosTxExecutionOutcome,
  expiredRelayReservationRecoveryTarget,
  hasDurableNoBroadcastEvidence,
  hasValidRelayChainTime,
  isRelaySnapshotStructurallyReady,
  parsePersistedRelayWithdrawState,
  relayBroadcastTxHash,
  relayPayloadNullifierLockKey,
  relayReservationStatus,
  relayPayloadNullifiers,
  relaySnapshotExpiresAtUnix,
  relaySnapshotIsExpired,
  sanitizeRelayWithdrawSnapshot,
  submitRelayAfterNullifierPreflight,
  updateReservationBatchRecords,
} from "../public/relay-reservation-state.js";
import {
  createRelaySubmissionCoordinator,
  relaySubmissionIdempotencyKey,
} from "../public/relay-submission-coordinator.js";

test("relay submission coordinator coalesces concurrent and repeated payload attempts", async () => {
  const coordinator = createRelaySubmissionCoordinator();
  const payload = {
    payload_hash: "ab".repeat(32),
    nullifier_hex: "cd".repeat(32),
  };
  const lockKey = relayPayloadNullifierLockKey(payload);
  const idempotencyKey = relaySubmissionIdempotencyKey(payload);
  let calls = 0;
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const submit = async () => {
    calls += 1;
    await gate;
    return { txHash: "tx-a" };
  };

  const first = coordinator.run(lockKey, idempotencyKey, submit);
  const second = coordinator.run(lockKey, idempotencyKey, submit);
  await Promise.resolve();
  assert.equal(calls, 1);
  release();
  assert.deepEqual(await first, { txHash: "tx-a" });
  assert.deepEqual(await second, { txHash: "tx-a" });
  assert.deepEqual(await coordinator.run(lockKey, idempotencyKey, submit), { txHash: "tx-a" });
  assert.equal(calls, 1);
});

test("relay submission lock key canonicalizes the complete input set", () => {
  const first = "aa".repeat(32);
  const second = "bb".repeat(32);
  const lockKey = relayPayloadNullifierLockKey({
    inputs: [
      { nullifier_hex: `0x${second.toUpperCase()}` },
      { nullifier_hex: first },
    ],
  });

  assert.equal(lockKey, `${first}:${second}`);
});

test("relay submission locks same inputs before nullifier preflight and rejects a different payload", async () => {
  const coordinator = createRelaySubmissionCoordinator();
  const nullifier = "cd".repeat(32);
  const firstPayload = {
    payload_hash: "aa".repeat(32),
    nullifier_hex: nullifier,
  };
  const conflictingPayload = {
    payload_hash: "bb".repeat(32),
    nullifier_hex: nullifier,
  };
  let checks = 0;
  let submissions = 0;
  let release;
  let startedSubmit;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const submitStarted = new Promise((resolve) => {
    startedSubmit = resolve;
  });
  const attempt = (payload) => coordinator.run(
    relayPayloadNullifierLockKey(payload),
    relaySubmissionIdempotencyKey(payload),
    () => submitRelayAfterNullifierPreflight({
      payload,
      checkNullifiers: async (nullifiers) => {
        checks += 1;
        return new Map(nullifiers.map((value) => [value, false]));
      },
      submit: async () => {
        submissions += 1;
        startedSubmit();
        await gate;
        return { txHash: "tx-a" };
      },
    }),
  );

  const first = attempt(firstPayload);
  await submitStarted;
  await assert.rejects(
    () => attempt(conflictingPayload),
    /input nullifiers already have a submission attempt/,
  );
  assert.equal(checks, 1);
  assert.equal(submissions, 1);
  release();
  assert.deepEqual(await first, { txHash: "tx-a" });
});

test("relay submission coordinator never evicts an in-flight attempt", async () => {
  const coordinator = createRelaySubmissionCoordinator({ maxEntries: 1 });
  let release;
  const gate = new Promise((resolve) => {
    release = resolve;
  });
  const first = coordinator.run("lock-a", "aa".repeat(32), async () => {
    await gate;
    return "first";
  });
  await Promise.resolve();

  await assert.rejects(
    () => coordinator.run("lock-b", "bb".repeat(32), async () => "second"),
    /capacity is exhausted by in-flight requests/,
  );
  assert.equal(coordinator.has("lock-a"), true);
  release();
  assert.equal(await first, "first");
  assert.equal(await coordinator.run("lock-b", "bb".repeat(32), async () => "second"), "second");
});

function proofReadyReservation() {
  return {
    operation_id: "relay-op",
    reservation_ids: ["relay-op:note:1"],
    reservations: [
      {
        reservation_id: "relay-op:note:1",
        operation_id: "relay-op",
        status: reservationStatuses.ProofReady,
        lease_token: "secret-lease-token",
        nullifier_lookup_key: "private-lookup",
        payload_hash: "payload-hash",
      },
    ],
  };
}

test("persisted relay snapshots keep metadata but drop payload and proof JSON", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    id: "payload-hash",
    payload: {
      payload_hash: "payload-hash",
      proof: "sensitive-proof-json",
      nullifier_hex: "sensitive-nullifier",
      expires_at_unix: 4102448400,
    },
    preparedData: { plan: { selectedNote: { nullifier: "note-nullifier" } } },
    payloadText: "{\"proof\":\"sensitive-proof-json\"}",
    reservation: proofReadyReservation(),
    amount: "10",
    recipient: "clair1recipient",
    handedOff: false,
  });

  assert.equal(snapshot.payloadHash, "payload-hash");
  assert.equal(snapshot.amount, "");
  assert.equal(snapshot.recipient, "");
  assert.equal(snapshot.expiresAtUnix, "4102448400");
  assert.equal("payload" in snapshot, false);
  assert.equal("payloadText" in snapshot, false);
  assert.equal("preparedData" in snapshot, false);
  assert.deepEqual(snapshot.reservation.reservations, [
    {
      reservation_id: "relay-op:note:1",
      operation_id: "relay-op",
      status: reservationStatuses.ProofReady,
      payload_hash: "payload-hash",
      broadcast_in_flight: false,
      broadcast_attempt_count: 0,
    },
  ]);
});

test("corrupted relay persistence fails closed without throwing", () => {
  assert.deepEqual(parsePersistedRelayWithdrawState("{broken"), {
    valid: false,
    current: null,
    pending: [],
  });
  assert.deepEqual(
    parsePersistedRelayWithdrawState(JSON.stringify({ pending: {} })),
    { valid: false, current: null, pending: [] },
  );
  assert.deepEqual(
    parsePersistedRelayWithdrawState(JSON.stringify({
      pending: [{
        payloadHash: "payload-hash",
        reservation: {
          reservation_ids: ["r1"],
          reservations: { r1: { status: reservationStatuses.ProofReady } },
        },
      }],
    })),
    { valid: false, current: null, pending: [] },
  );
});

test("Cosmos recovery accepts only an explicit integer transaction code", () => {
  assert.equal(cosmosTxExecutionOutcome({ code: 0 }), "success");
  assert.equal(cosmosTxExecutionOutcome({ code: 12 }), "failed");
  assert.equal(cosmosTxExecutionOutcome({}), "unknown");
  assert.equal(cosmosTxExecutionOutcome({ code: "0" }), "unknown");
  assert.equal(cosmosTxExecutionOutcome({ code: "bogus" }), "unknown");
});

test("only explicit durable pre-broadcast evidence can release a reservation", () => {
  assert.equal(
    hasDurableNoBroadcastEvidence({ metadata: { no_broadcast_attempt: true } }),
    true,
  );
  assert.equal(
    hasDurableNoBroadcastEvidence({
      metadata: {
        no_broadcast_attempt: true,
        opaque_broadcast_error: true,
      },
    }),
    false,
  );
  assert.equal(
    hasDurableNoBroadcastEvidence({
      submitted_tx_hash: "TX",
      metadata: { no_broadcast_attempt: true },
    }),
    false,
  );
  assert.equal(
    hasDurableNoBroadcastEvidence({ metadata: { no_broadcast_attempt: "true" } }),
    false,
  );
  assert.equal(
    hasDurableNoBroadcastEvidence({
      no_broadcast_attempt: true,
      metadata: { no_broadcast_attempt: false },
    }),
    false,
  );
});

test("relay broadcast tx hash helper accepts Cosmos txhash shapes", () => {
  assert.equal(relayBroadcastTxHash({ txhash: "COSMOS_TX" }), "COSMOS_TX");
  assert.equal(
    relayBroadcastTxHash({ broadcast: { txhash: "NESTED_TX" } }),
    "NESTED_TX",
  );
  assert.equal(
    relayBroadcastTxHash({ data: { txhash: "ERROR_PAYLOAD_TX" } }),
    "ERROR_PAYLOAD_TX",
  );
});

test("expired relay snapshots are not submittable", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 100,
    },
    reservation: proofReadyReservation(),
  });

  assert.equal(relaySnapshotExpiresAtUnix(snapshot), "100");
  assert.equal(relaySnapshotIsExpired(snapshot), false);
  assert.equal(relaySnapshotIsExpired(snapshot, 100999), false);
  assert.equal(relaySnapshotIsExpired(snapshot, 101000), true);
  assert.equal(
    canRelaySnapshotBeSubmitted(
      {
        ...snapshot,
        payload: { payload_hash: "payload-hash" },
      },
      null,
      reservationStatuses,
      101000,
    ),
    false,
  );
});

test("relay submission fails closed without a valid chain time", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: proofReadyReservation(),
  });

  assert.equal(hasValidRelayChainTime(undefined), false);
  assert.equal(hasValidRelayChainTime(null), false);
  assert.equal(hasValidRelayChainTime(""), false);
  assert.equal(hasValidRelayChainTime(" "), false);
  assert.equal(hasValidRelayChainTime(false), false);
  assert.equal(hasValidRelayChainTime(true), false);
  assert.equal(hasValidRelayChainTime("not-a-time"), false);
  assert.equal(hasValidRelayChainTime(4_000_000_000_000), true);
  assert.equal(
    canRelaySnapshotBeSubmitted(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      null,
      reservationStatuses,
    ),
    false,
  );
  assert.equal(
    canRelaySnapshotBeSubmitted(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      null,
      reservationStatuses,
      4_000_000_000_000,
    ),
    true,
  );
});

test("valid relay payload is structurally ready for the submit button before fresh chain time is fetched", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: proofReadyReservation(),
  });

  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      null,
      reservationStatuses,
    ),
    true,
  );
  assert.equal(
    canRelaySnapshotBeSubmitted(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      new Map(),
      reservationStatuses,
    ),
    false,
  );
});

test("relay snapshot with an existing tx hash cannot be submitted again", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: proofReadyReservation(),
    relayHash: "ALREADY_SUBMITTED_TX",
  });

  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      new Map(),
      reservationStatuses,
    ),
    false,
  );
});

test("handed-off relay snapshots cannot be submitted by the local relayer", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: proofReadyReservation(),
    handedOff: true,
  });

  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload: { payload_hash: "payload-hash" } },
      null,
      reservationStatuses,
    ),
    false,
  );
});

test("relay snapshot with a durable broadcast attempt cannot be submitted again", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: proofReadyReservation(),
  });
  snapshot.payload = { payload_hash: "payload-hash" };
  snapshot.reservation.reservations[0].broadcast_in_flight = true;
  snapshot.reservation.reservations[0].broadcast_attempt_count = 1;

  assert.equal(
    isRelaySnapshotStructurallyReady(snapshot, null, reservationStatuses),
    false,
  );
});

test("relay structural readiness rejects missing expiry, mixed status, and payload hash mismatch", () => {
  const snapshot = sanitizeRelayWithdrawSnapshot({
    payload: {
      payload_hash: "payload-hash",
      expires_at_unix: 4102448400,
    },
    reservation: {
      operation_id: "relay-op",
      reservation_ids: ["r1", "r2"],
      reservations: [
        { reservation_id: "r1", operation_id: "relay-op", status: reservationStatuses.ProofReady, payload_hash: "payload-hash" },
        { reservation_id: "r2", operation_id: "relay-op", status: reservationStatuses.ProofReady, payload_hash: "payload-hash" },
      ],
    },
  });
  const payload = { payload_hash: "payload-hash" };

  assert.equal(
    isRelaySnapshotStructurallyReady({ ...snapshot, payload }, null, reservationStatuses),
    true,
  );
  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload },
      new Map([["r2", { reservation_id: "r2", operation_id: "relay-op", status: reservationStatuses.Unknown, payload_hash: "payload-hash" }]]),
      reservationStatuses,
    ),
    false,
  );
  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload },
      new Map([["r2", { reservation_id: "r2", operation_id: "relay-op", status: reservationStatuses.ProofReady, payload_hash: "other-payload" }]]),
      reservationStatuses,
    ),
    false,
  );
  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, expiresAtUnix: "", payload },
      null,
      reservationStatuses,
    ),
    false,
  );
  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, expiresAtUnix: null, payload, reservation: snapshot.reservation },
      null,
      reservationStatuses,
    ),
    false,
  );
  assert.equal(
    isRelaySnapshotStructurallyReady(
      { ...snapshot, payload: { payload_hash: "other-payload" } },
      new Map(),
      reservationStatuses,
    ),
    false,
  );
});

test("expired local ProofReady reservations require manual review instead of replan", () => {
  assert.equal(
    canReplanExpiredLocalReservation({
      localPreBroadcast: true,
      workerExpired: true,
      hasProofReady: true,
    }),
    false,
  );
  assert.equal(
    canReplanExpiredLocalReservation({
      localPreBroadcast: true,
      workerExpired: true,
      hasProofReady: false,
    }),
    true,
  );
  assert.equal(
    expiredRelayReservationRecoveryTarget({
      localWorkerState: true,
      localPreBroadcast: true,
      workerExpired: true,
      hasProofReady: true,
    }),
    reservationStatuses.ManualReview,
  );
  assert.equal(
    expiredRelayReservationRecoveryTarget({
      localWorkerState: true,
      localPreBroadcast: true,
      workerExpired: true,
      hasProofReady: false,
    }),
    reservationStatuses.ReplanRequired,
  );
  assert.equal(
    expiredRelayReservationRecoveryTarget({
      handedOff: true,
      localWorkerState: true,
      localPreBroadcast: true,
      workerExpired: true,
      hasProofReady: true,
    }),
    "",
  );
});

test("latest reservation status overrides stale ProofReady snapshots before relay", () => {
  const sanitized = sanitizeRelayWithdrawSnapshot({
    payload: { payload_hash: "payload-hash", expires_at_unix: 4102448400 },
    reservation: proofReadyReservation(),
  });
  const latest = new Map([
    [
      "relay-op:note:1",
      {
        reservation_id: "relay-op:note:1",
        status: reservationStatuses.Unknown,
      },
    ],
  ]);

  assert.equal(
    relayReservationStatus(sanitized.reservation, latest),
    reservationStatuses.Unknown,
  );
  assert.equal(
    canRelaySnapshotBeSubmitted(
      { ...sanitized, payload: { payload_hash: "payload-hash" } },
      latest,
      reservationStatuses,
    ),
    false,
  );
});

test("metadata-only recovered snapshots cannot be relayed until payload is prepared again", () => {
  const sanitized = sanitizeRelayWithdrawSnapshot({
    reservation: proofReadyReservation(),
    payloadHash: "payload-hash",
    expiresAtUnix: 4102448400,
  });
  const updated = updateReservationBatchRecords(sanitized.reservation, [
    {
      reservation_id: "relay-op:note:1",
      status: reservationStatuses.ProofReady,
      payload_hash: "payload-hash",
    },
  ]);

  assert.equal(
    canRelaySnapshotBeSubmitted(
      { ...sanitized, reservation: updated },
      new Map(),
      reservationStatuses,
    ),
    false,
  );
});

test("relay snapshots without reservation ids fail closed", () => {
  assert.equal(
    canRelaySnapshotBeSubmitted(
      { payload: { payload_hash: "payload-hash" }, reservation: null },
      new Map(),
      reservationStatuses,
    ),
    false,
  );
});

test("relay server preflight calls the signer only for explicit unspent statuses", async () => {
  const nullifier = "ab".repeat(32);
  const payload = { nullifier_hex: `0x${nullifier.toUpperCase()}` };
  assert.deepEqual(relayPayloadNullifiers(payload), [nullifier]);

  let submissions = 0;
  const result = await submitRelayAfterNullifierPreflight({
    payload,
    checkNullifiers: async (values) => new Map([[values[0], false]]),
    submit: async () => {
      submissions += 1;
      return "submitted";
    },
  });
  assert.equal(result, "submitted");
  assert.equal(submissions, 1);

  for (const checkNullifiers of [
    async () => new Map([[nullifier, true]]),
    async () => new Map(),
    async () => new Map([[nullifier, "false"]]),
    async () => ({}),
    async () => {
      throw new Error("upstream detail");
    },
  ]) {
    await assert.rejects(
      submitRelayAfterNullifierPreflight({
        payload,
        checkNullifiers,
        submit: async () => {
          submissions += 1;
        },
      }),
      /nullifier/,
    );
  }
  assert.equal(submissions, 1);
});

test("relay server preflight rejects missing and malformed nullifiers before lookup", async () => {
  let checks = 0;
  for (const payload of [{}, { nullifier_hex: "not-hex" }]) {
    await assert.rejects(
      submitRelayAfterNullifierPreflight({
        payload,
        checkNullifiers: async () => {
          checks += 1;
          return new Map();
        },
        submit: async () => undefined,
      }),
      /nullifier/,
    );
  }
  assert.equal(checks, 0);
});
