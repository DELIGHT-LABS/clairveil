export function createRelaySubmissionCoordinator({ maxEntries = 256 } = {}) {
  const attempts = new Map();
  const entries = new Set();
  const limit = Math.max(1, Number(maxEntries) || 256);

  function normalizeIdempotencyKey(value) {
    return String(value || "").trim().toLowerCase();
  }

  function snapshotValue(value, seen = new WeakMap()) {
    if (value === null || typeof value !== "object") return value;
    if (seen.has(value)) return seen.get(value);
    if (ArrayBuffer.isView(value)) return value.slice?.() ?? value;
    if (value instanceof ArrayBuffer) return value.slice(0);
    if (Array.isArray(value)) {
      const copy = [];
      seen.set(value, copy);
      copy.push(...value.map(item => snapshotValue(item, seen)));
      return Object.freeze(copy);
    }
    const copy = {};
    seen.set(value, copy);
    for (const [key, item] of Object.entries(value)) {
      copy[key] = snapshotValue(item, seen);
    }
    return Object.freeze(copy);
  }

  function reconciliationStatus(entry, {
    found = Boolean(entry),
    resolved = false,
    released = false,
  } = {}) {
    return Object.freeze({
      found,
      settled: Boolean(entry?.settled),
      resolved,
      released,
      evidence: entry ? snapshotValue(entry.submissionEvidence) : null,
      result: entry ? snapshotValue(entry.result) : null,
    });
  }

  function normalizedLockSet(lockKeys) {
    return [...new Set(
      (Array.isArray(lockKeys) ? lockKeys : [])
        .map(key => String(key || "").trim().toLowerCase())
        .filter(Boolean),
    )].sort();
  }

  function deleteEntry(entry) {
    for (const key of entry.lockKeys) {
      if (attempts.get(key) === entry) attempts.delete(key);
    }
    entries.delete(entry);
  }

  function trimCompleted() {
    for (const entry of entries) {
      if (entries.size < limit) break;
      // A resolved callback can still mean "broadcast accepted, inclusion
      // unknown". Never evict that lock merely to admit another payload.
      // A callback that never marked submission is also safe to forget. For a
      // crossed boundary, only an authoritatively included success is safe to
      // evict: replay then runs fresh nullifier preflight against chain state.
      if (entry.settled && (
        !entry.submissionStarted || entry.includedExecutionSucceeded
      )) deleteEntry(entry);
    }
  }

  async function reconcileEntry(entry, reconcile) {
    if (!entry) return reconciliationStatus(null);
    if (!entry.settled) return reconciliationStatus(entry);
    if (entry.reconciliation) return entry.reconciliation;
    if (typeof reconcile !== "function") {
      throw new Error("relay submission reconciliation callback is required");
    }

    const snapshot = Object.freeze({
      lockKeys: Object.freeze([...entry.lockKeys]),
      idempotencyKey: entry.idempotencyKey,
      evidence: snapshotValue(entry.submissionEvidence),
      result: snapshotValue(entry.result),
    });
    const reconciliation = Promise.resolve()
      .then(() => reconcile(snapshot))
      .then((outcome) => {
        if (
          outcome?.included !== true
          || (outcome.failed !== true && outcome.failed !== false)
        ) {
          return reconciliationStatus(entry);
        }
        if (Object.prototype.hasOwnProperty.call(outcome, "result")) {
          entry.result = outcome.result;
        }
        if (outcome.failed) {
          entry.includedExecutionFailed = true;
          const status = reconciliationStatus(entry, { resolved: true, released: true });
          deleteEntry(entry);
          return status;
        }

        entry.includedExecutionSucceeded = true;
        if (Object.prototype.hasOwnProperty.call(outcome, "result")) {
          entry.promise = Promise.resolve(entry.result);
        }
        return reconciliationStatus(entry, { resolved: true });
      })
      .finally(() => {
        if (entry.reconciliation === reconciliation) entry.reconciliation = null;
      });
    entry.reconciliation = reconciliation;
    return reconciliation;
  }

  let runManySubmission;
  const coordinator = {
    async run(lockKey, idempotencyKey, submit) {
      // Keep the legacy two-argument form usable for callers that only need
      // one lock. The relay server uses runMany so every nullifier is acquired
      // atomically while the immutable payload hash remains the idempotency key.
      if (typeof idempotencyKey === "function" && submit === undefined) {
        submit = idempotencyKey;
        idempotencyKey = lockKey;
      }
      return runManySubmission([lockKey], idempotencyKey, submit);
    },
    async runMany(lockKeys, idempotencyKey, submit) {
      const normalizedLockKeys = normalizedLockSet(lockKeys);
      const normalizedIdempotencyKey = normalizeIdempotencyKey(idempotencyKey);
      if (!normalizedLockKeys.length || !normalizedIdempotencyKey) {
        throw new Error("relay submission lock and idempotency keys are required");
      }
      if (typeof submit !== "function") {
        throw new Error("relay submission callback is required");
      }
      const occupied = [...new Set(
        normalizedLockKeys.map(key => attempts.get(key)).filter(Boolean),
      )];
      if (occupied.length) {
        const existing = occupied.length === 1 ? occupied[0] : null;
        const sameAttempt = existing
          && existing.idempotencyKey === normalizedIdempotencyKey
          && existing.lockKeys.length === normalizedLockKeys.length
          && existing.lockKeys.every((key, index) => key === normalizedLockKeys[index]);
        if (sameAttempt) return existing.promise;
        throw new Error("relay input nullifiers already have a submission attempt");
      }

      trimCompleted();
      if (entries.size >= limit) {
        throw new Error("relay submission coordinator capacity is exhausted by in-flight requests");
      }
      const entry = {
        lockKeys: normalizedLockKeys,
        idempotencyKey: normalizedIdempotencyKey,
        submissionStarted: false,
        submissionRejected: false,
        includedExecutionFailed: false,
        includedExecutionSucceeded: false,
        submissionEvidence: null,
        result: null,
        reconciliation: null,
        settled: false,
        promise: null,
      };
      const markSubmissionStarted = () => {
        entry.submissionStarted = true;
      };
      const markSubmissionRejected = () => {
        if (!entry.submissionStarted) {
          throw new Error("relay submission cannot be rejected before it starts");
        }
        // This marker is only for an explicit transport response proving that
        // the node did not accept the transaction. Timeouts, disconnects, and
        // malformed responses must remain ambiguous and retain the lock.
        entry.submissionRejected = true;
      };
      const markIncludedExecutionFailed = () => {
        if (!entry.submissionStarted) {
          throw new Error("relay execution cannot fail before submission starts");
        }
        // This marker is only for an authoritative included transaction whose
        // execution failed (DeliverTx or an EVM receipt). It is distinct from a
        // pre-inclusion rejection: the next request must run nullifier preflight
        // again before it is allowed to submit a replacement transaction.
        entry.includedExecutionFailed = true;
      };
      const recordSubmissionEvidence = evidence => {
        if (!entry.submissionStarted || entry.submissionRejected) {
          throw new Error("relay submission evidence cannot be recorded in this state");
        }
        if (evidence === undefined || evidence === null) {
          throw new Error("relay submission evidence is required");
        }
        entry.submissionEvidence = evidence;
      };
      entry.promise = Promise.resolve()
        .then(() => submit(
          markSubmissionStarted,
          markSubmissionRejected,
          markIncludedExecutionFailed,
          recordSubmissionEvidence,
        ))
        .then((result) => {
          entry.result = result;
          entry.includedExecutionSucceeded = result?.included === true
            && result?.failed === false;
          if (entry.includedExecutionFailed) deleteEntry(entry);
          return result;
        })
        .catch((error) => {
          // A callback that fails before the external boundary is safe to retry.
          // Keep the lock once submission has started, because the transaction may
          // have reached the network even if the caller did not receive a result.
          // The sole post-boundary exception is an explicit rejection response.
          if (
            (
              !entry.submissionStarted
              || entry.submissionRejected
              || entry.includedExecutionFailed
            )
          ) {
            deleteEntry(entry);
          }
          throw error;
        })
        .finally(() => {
          entry.settled = true;
        });
      for (const key of normalizedLockKeys) attempts.set(key, entry);
      entries.add(entry);
      return entry.promise;
    },
    async reconcileMany(lockKeys, reconcile) {
      const normalizedLockKeys = normalizedLockSet(lockKeys);
      if (!normalizedLockKeys.length) {
        throw new Error("relay submission lock keys are required for reconciliation");
      }
      const occupied = [...new Set(
        normalizedLockKeys.map(key => attempts.get(key)).filter(Boolean),
      )];
      const entry = occupied.length === 1 ? occupied[0] : null;
      const exactEntry = entry
        && entry.lockKeys.length === normalizedLockKeys.length
        && entry.lockKeys.every((key, index) => key === normalizedLockKeys[index]);
      return reconcileEntry(exactEntry ? entry : null, reconcile);
    },
    async reconcileByIdempotencyKey(idempotencyKey, reconcile) {
      const normalized = normalizeIdempotencyKey(idempotencyKey);
      if (!normalized) {
        throw new Error("relay submission idempotency key is required for reconciliation");
      }
      const matching = [...entries].filter(entry => entry.idempotencyKey === normalized);
      if (matching.length > 1) {
        throw new Error("relay submission idempotency key matches multiple attempts");
      }
      return reconcileEntry(matching[0] || null, reconcile);
    },
    has(key) {
      return attempts.has(String(key || "").trim().toLowerCase());
    },
  };
  runManySubmission = coordinator.runMany.bind(coordinator);
  return coordinator;
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
