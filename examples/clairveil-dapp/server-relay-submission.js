import {
  relayPayloadNullifiers,
  submitRelayAfterNullifierPreflight,
} from "./public/relay-reservation-state.js";
import {
  createRelaySubmissionCoordinator,
  relaySubmissionIdempotencyKey,
} from "./public/relay-submission-coordinator.js";
import {
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus,
} from "./public/transaction-status.js";

function readOnlySubmissionEvidence(evidence) {
  if (!evidence || typeof evidence !== "object" || Array.isArray(evidence)) return null;
  const snapshot = {};
  for (const key of ["transport", "chainId", "evmChainId", "txHash"]) {
    if (evidence[key] === undefined || evidence[key] === null) continue;
    const value = String(evidence[key]).trim();
    if (value) snapshot[key] = value;
  }
  return Object.keys(snapshot).length ? Object.freeze(snapshot) : null;
}

export function createRelayWithdrawSubmissionGate(options = {}) {
  const coordinator = createRelaySubmissionCoordinator(options);
  return Object.freeze({
    async run(payload, { checkNullifiers, submit, reconcile } = {}) {
      const nullifiers = relayPayloadNullifiers(payload);
      if (typeof reconcile === "function") {
        await coordinator.reconcileMany(nullifiers, reconcile);
      }
      return coordinator.runMany(
        nullifiers,
        relaySubmissionIdempotencyKey(payload),
        (
          markSubmissionStarted,
          markSubmissionRejected,
          markIncludedExecutionFailed,
          recordSubmissionEvidence,
        ) => submitRelayAfterNullifierPreflight({
          payload,
          checkNullifiers,
          submit: () => submit(
            markSubmissionStarted,
            markSubmissionRejected,
            markIncludedExecutionFailed,
            recordSubmissionEvidence,
          ),
        }),
      );
    },
    async reconcileByPayloadHash(payloadHash, { reconcile } = {}) {
      const idempotencyKey = relaySubmissionIdempotencyKey({ payload_hash: payloadHash });
      const status = await coordinator.reconcileByIdempotencyKey(
        idempotencyKey,
        reconcile,
      );
      return Object.freeze({
        found: status.found === true,
        settled: status.settled === true,
        resolved: status.resolved === true,
        released: status.released === true,
        evidence: readOnlySubmissionEvidence(status.evidence),
        result: status.result ?? null,
      });
    },
  });
}

export async function waitForTrackedSubmissionOutcome({
  waitForOutcome,
  markSubmissionOutcomeUnknown,
  evidence,
} = {}) {
  if (typeof waitForOutcome !== "function") {
    throw new Error("submission outcome query callback is required");
  }
  if (typeof markSubmissionOutcomeUnknown !== "function") {
    throw new Error("submission unknown-outcome marker is required");
  }
  if (evidence === undefined || evidence === null) {
    throw new Error("submission transaction evidence is required");
  }
  let outcome;
  try {
    outcome = await waitForOutcome();
  } catch (error) {
    markSubmissionOutcomeUnknown(evidence);
    throw error;
  }
  if (!outcome) markSubmissionOutcomeUnknown(evidence);
  return outcome;
}

export function createRelayAccountSubmissionSerializer() {
  const accounts = new Map();

  function normalizedAccountKey(accountKey) {
    const key = String(accountKey || "").trim();
    if (!key) {
      throw new Error("relay submission account key is required");
    }
    return key;
  }

  function accountOutcomeUnknownError(entry) {
    const error = new Error(
      "local signer account has an unresolved submission outcome; reconcile it before another submission",
    );
    error.code = "SIGNER_ACCOUNT_SUBMISSION_UNKNOWN";
    if (entry.unknownCause instanceof Error) error.cause = entry.unknownCause;
    return error;
  }

  async function reconcileUnknownEntry(entry, reconcileUnknown) {
    if (!entry.outcomeUnknown || typeof reconcileUnknown !== "function") return false;
    try {
      const result = await reconcileUnknown(entry.unknownEvidence);
      const resolved = result === true || result?.resolved === true;
      if (!resolved) return false;
      entry.outcomeUnknown = false;
      entry.unknownCause = null;
      entry.unknownEvidence = null;
      return true;
    } catch (error) {
      entry.unknownCause = error;
      return false;
    }
  }

  return Object.freeze({
    run(accountKey, submitOrOptions) {
      const normalizedKey = normalizedAccountKey(accountKey);
      const options = typeof submitOrOptions === "function"
        ? { submit: submitOrOptions }
        : (submitOrOptions || {});
      const { submit, reconcileUnknown } = options;
      if (typeof submit !== "function") {
        throw new Error("local signer account submission callback is required");
      }

      // Different payloads are never coalesced. They take their own FIFO turn
      // so a shared Cosmos account sequence or EVM wallet nonce cannot race.
      const entry = accounts.get(normalizedKey) ?? {
        tail: Promise.resolve(),
        pending: 0,
        outcomeUnknown: false,
        unknownCause: null,
        unknownEvidence: null,
      };
      accounts.set(normalizedKey, entry);
      entry.pending += 1;

      const result = entry.tail.then(async () => {
        if (entry.outcomeUnknown) {
          await reconcileUnknownEntry(entry, reconcileUnknown);
          if (entry.outcomeUnknown) throw accountOutcomeUnknownError(entry);
        }

        let submissionStarted = false;
        let submissionRejected = false;
        let submissionOutcomeUnknown = false;
        let submissionEvidence = null;
        const markSubmissionStarted = () => {
          submissionStarted = true;
        };
        const markSubmissionRejected = () => {
          if (!submissionStarted) {
            throw new Error("local signer account submission cannot be rejected before it starts");
          }
          submissionRejected = true;
        };
        const recordSubmissionEvidence = (evidence) => {
          if (!submissionStarted || submissionRejected) {
            throw new Error("local signer account submission evidence cannot be recorded in this state");
          }
          if (evidence === undefined || evidence === null) {
            throw new Error("local signer account submission evidence is required");
          }
          submissionEvidence = evidence;
        };
        let unknownEvidence = null;
        const markSubmissionOutcomeUnknown = (evidence = null) => {
          if (!submissionStarted || submissionRejected) {
            throw new Error("local signer account submission outcome cannot be unknown in this state");
          }
          submissionOutcomeUnknown = true;
          if (evidence !== null) recordSubmissionEvidence(evidence);
          unknownEvidence = evidence ?? submissionEvidence;
        };

        try {
          const value = await submit(
            markSubmissionStarted,
            markSubmissionRejected,
            markSubmissionOutcomeUnknown,
            recordSubmissionEvidence,
          );
          if (submissionOutcomeUnknown) {
            entry.outcomeUnknown = true;
            entry.unknownEvidence = unknownEvidence;
          }
          return value;
        } catch (error) {
          if (submissionStarted && !submissionRejected) {
            // An unknown post-boundary outcome fences the whole signer account,
            // not just this payload: signing another account sequence/nonce is
            // unsafe until the prior submission is authoritatively reconciled.
            entry.outcomeUnknown = true;
            entry.unknownCause = error;
            if (unknownEvidence !== null) entry.unknownEvidence = unknownEvidence;
            else if (submissionEvidence !== null) entry.unknownEvidence = submissionEvidence;
          }
          throw error;
        }
      });
      const tail = result.then(() => undefined, () => undefined);
      entry.tail = tail;

      return result.finally(() => {
        entry.pending -= 1;
        if (
          accounts.get(normalizedKey) === entry
          && entry.tail === tail
          && entry.pending === 0
          && !entry.outcomeUnknown
        ) {
          accounts.delete(normalizedKey);
        }
      });
    },
    hasUnknownOutcome(accountKey) {
      return Boolean(accounts.get(normalizedAccountKey(accountKey))?.outcomeUnknown);
    },
    async reconcileUnknownOutcome(accountKey, reconcileUnknown) {
      const normalizedKey = normalizedAccountKey(accountKey);
      const entry = accounts.get(normalizedKey);
      if (!entry?.outcomeUnknown) return false;
      if (entry.pending !== 0) {
        throw new Error("local signer account reconciliation must wait for queued submissions");
      }
      const resolved = await reconcileUnknownEntry(entry, reconcileUnknown);
      if (resolved && accounts.get(normalizedKey) === entry) accounts.delete(normalizedKey);
      return resolved;
    },
  });
}

export function confirmedCosmosTxCode(tx) {
  const value = tx?.code;
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? value : null;
  }
  if (typeof value === "string" && /^(0|[1-9][0-9]*)$/.test(value)) {
    const code = Number(value);
    return Number.isSafeInteger(code) ? code : null;
  }
  return null;
}

export function trackedCosmosSubmissionOutcome(
  tx,
  evidence,
  markSubmissionOutcomeUnknown,
) {
  if (typeof markSubmissionOutcomeUnknown !== "function") {
    throw new Error("submission unknown-outcome marker is required");
  }
  if (!tx) {
    markSubmissionOutcomeUnknown(evidence);
    return { included: false, pending: true, unknown: true, failed: null };
  }
  const code = confirmedCosmosTxCode(tx);
  if (code == null) {
    markSubmissionOutcomeUnknown(evidence);
    return { included: true, pending: false, unknown: true, failed: null };
  }
  return {
    included: true,
    pending: false,
    unknown: false,
    failed: code > 0,
  };
}

export function trackedEvmSubmissionOutcome(
  receipt,
  evidence,
  markSubmissionOutcomeUnknown,
) {
  if (typeof markSubmissionOutcomeUnknown !== "function") {
    throw new Error("submission unknown-outcome marker is required");
  }
  if (!receipt) {
    markSubmissionOutcomeUnknown(evidence);
    return { included: false, pending: true, unknown: true, failed: null };
  }
  const failed = hasFailedEvmReceiptStatus(receipt);
  if (!failed && !hasSuccessfulEvmReceiptStatus(receipt)) {
    markSubmissionOutcomeUnknown(evidence);
    return { included: true, pending: false, unknown: true, failed: null };
  }
  return {
    included: true,
    pending: false,
    unknown: false,
    failed,
  };
}

export function assertCosmosCheckTxAccepted(tx, { markSubmissionRejected } = {}) {
  const code = confirmedCosmosTxCode(tx);
  if (tx && Object.prototype.hasOwnProperty.call(tx, "code") && code == null) {
    throw new Error("Cosmos CheckTx returned an invalid code");
  }
  if (code != null && code !== 0) {
    markSubmissionRejected?.();
    const error = new Error(
      String(tx?.raw_log || tx?.rawLog || "").trim()
        || `Cosmos CheckTx failed with code ${code}`,
    );
    error.txHash = String(tx?.txhash || tx?.txHash || "").trim();
    error.txCode = code;
    error.checkTxRejected = true;
    error.rpcInvoked = true;
    throw error;
  }
  return code;
}
