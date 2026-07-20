export function createRelaySubmissionCoordinator({ maxEntries = 256 } = {}) {
  const attempts = new Map();
  const limit = Math.max(1, Number(maxEntries) || 256);

  function trimCompleted() {
    for (const [key, entry] of attempts) {
      if (attempts.size < limit) break;
      if (entry.settled) attempts.delete(key);
    }
  }

  return {
    async run(lockKey, idempotencyKey, submit) {
      // Keep the legacy two-argument form usable for callers that only need
      // one idempotency key. Relay withdrawal uses the three-argument form:
      // the lock is the full input-nullifier set and the idempotency key is
      // the immutable payload hash.
      if (typeof idempotencyKey === "function" && submit === undefined) {
        submit = idempotencyKey;
        idempotencyKey = lockKey;
      }
      const normalizedLockKey = String(lockKey || "").trim().toLowerCase();
      const normalizedIdempotencyKey = String(idempotencyKey || "")
        .trim()
        .toLowerCase();
      if (!normalizedLockKey || !normalizedIdempotencyKey) {
        throw new Error("relay submission lock and idempotency keys are required");
      }
      if (typeof submit !== "function") {
        throw new Error("relay submission callback is required");
      }
      const existing = attempts.get(normalizedLockKey);
      if (existing) {
        if (existing.idempotencyKey !== normalizedIdempotencyKey) {
          throw new Error("relay input nullifiers already have a submission attempt");
        }
        return existing.promise;
      }

      trimCompleted();
      if (attempts.size >= limit) {
        throw new Error("relay submission coordinator capacity is exhausted by in-flight requests");
      }
      const entry = {
        idempotencyKey: normalizedIdempotencyKey,
        settled: false,
        promise: null,
      };
      entry.promise = Promise.resolve()
        .then(submit)
        .finally(() => {
          entry.settled = true;
        });
      attempts.set(normalizedLockKey, entry);
      return entry.promise;
    },
    has(key) {
      return attempts.has(String(key || "").trim().toLowerCase());
    },
  };
}

export function relaySubmissionIdempotencyKey(payload = {}) {
  const hash = String(payload.payload_hash || payload.payloadHash || "")
    .trim()
    .toLowerCase()
    .replace(/^0x/, "");
  if (!/^[0-9a-f]{64}$/.test(hash)) {
    throw new Error("relay withdraw payload has an invalid payload hash");
  }
  return hash;
}
