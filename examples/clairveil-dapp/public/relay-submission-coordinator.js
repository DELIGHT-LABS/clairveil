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
    async run(key, submit) {
      const normalizedKey = String(key || "").trim().toLowerCase();
      if (!normalizedKey) {
        throw new Error("relay submission idempotency key is required");
      }
      if (typeof submit !== "function") {
        throw new Error("relay submission callback is required");
      }
      const existing = attempts.get(normalizedKey);
      if (existing) return existing.promise;

      trimCompleted();
      if (attempts.size >= limit) {
        throw new Error("relay submission coordinator capacity is exhausted by in-flight requests");
      }
      const entry = { settled: false, promise: null };
      entry.promise = Promise.resolve()
        .then(submit)
        .finally(() => {
          entry.settled = true;
        });
      attempts.set(normalizedKey, entry);
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
