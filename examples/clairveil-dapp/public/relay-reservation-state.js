export function relayReservationIDs(reservation) {
  return Array.isArray(reservation?.reservation_ids)
    ? reservation.reservation_ids.filter(Boolean).map(String)
    : [];
}

function firstNonEmptyString(...values) {
  for (const value of values) {
    if (value === undefined || value === null || value === "") continue;
    return String(value);
  }
  return "";
}

export function relayBroadcastTxHash(source = {}) {
  return firstNonEmptyString(
    source?.broadcast?.txhash,
    source?.broadcast?.txHash,
    source?.broadcast?.tx_hash,
    source?.txhash,
    source?.txHash,
    source?.tx_hash,
    source?.source?.txhash,
    source?.source?.txHash,
    source?.source?.tx_hash,
    source?.data?.txhash,
    source?.data?.txHash,
    source?.data?.tx_hash,
    source?.tx?.txhash,
    source?.tx?.txHash,
    source?.tx?.tx_hash,
  );
}

export function relaySnapshotExpiresAtUnix(snapshot = {}) {
  const raw = firstNonEmptyString(
    snapshot.expiresAtUnix,
    snapshot.expires_at_unix,
    snapshot.payload?.expires_at_unix,
    snapshot.payload?.expiresAtUnix,
  );
  const seconds = strictPositiveUnixSeconds(raw);
  return seconds == null ? "" : String(seconds);
}

// Only chain-derived block time may authorize a payload-expiry recovery.
export function relaySnapshotIsExpired(snapshot = {}, chainNowMs) {
  const seconds = strictPositiveUnixSeconds(relaySnapshotExpiresAtUnix(snapshot));
  if (seconds == null || !hasValidRelayChainTime(chainNowMs)) return false;
  // The chain rejects a relay withdraw at the exact expiry second too. Keep
  // browser handoff and local recovery fail-closed at that same boundary.
  return Math.floor(chainNowMs / 1000) >= seconds;
}

export function hasValidRelayChainTime(chainNowMs) {
  return (
    typeof chainNowMs === "number" &&
    Number.isSafeInteger(chainNowMs) &&
    chainNowMs >= 0
  );
}

export function cosmosTxExecutionOutcome(tx) {
  const code = tx?.code;
  if (typeof code !== "number" || !Number.isSafeInteger(code) || code < 0) {
    return "unknown";
  }
  return code === 0 ? "success" : "failed";
}

export function hasDurableNoBroadcastEvidence(reservation = {}) {
  const metadata = reservation.metadata || {};
  const metadataHasEvidence =
    Object.prototype.hasOwnProperty.call(metadata, "no_broadcast_attempt") ||
    Object.prototype.hasOwnProperty.call(metadata, "noBroadcastAttempt");
  const explicitlyNotBroadcast = metadataHasEvidence
    ? metadata.no_broadcast_attempt === true || metadata.noBroadcastAttempt === true
    : reservation.no_broadcast_attempt === true ||
      reservation.noBroadcastAttempt === true;
  if (!explicitlyNotBroadcast || metadata.opaque_broadcast_error === true) {
    return false;
  }
  return !(
    reservation.submitted_tx_hash ||
    reservation.tx_bytes_hash ||
    reservation.sign_doc_hash ||
    metadata.submitted_tx_hash ||
    metadata.tx_bytes_hash ||
    metadata.sign_doc_hash
  );
}

function strictPositiveUnixSeconds(value) {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value > 0 ? value : null;
  }
  if (typeof value !== "string") return null;
  const normalized = value.trim();
  if (!/^[1-9][0-9]*$/.test(normalized)) return null;
  const seconds = Number(normalized);
  return Number.isSafeInteger(seconds) && seconds > 0 ? seconds : null;
}

function strictNullifierHex(value) {
  if (typeof value !== "string") return "";
  const normalized = value.trim().toLowerCase().replace(/^0x/, "");
  return /^[0-9a-f]{64}$/.test(normalized) ? normalized : "";
}

export function relayPayloadNullifiers(payload = {}) {
  const candidates = [
    payload.nullifier_hex,
    payload.nullifierHex,
    ...(Array.isArray(payload.inputs)
      ? payload.inputs.flatMap((input) => [input?.nullifier_hex, input?.nullifierHex])
      : []),
  ].filter((value) => value !== undefined && value !== null && value !== "");
  if (!candidates.length) {
    throw new Error("relay withdraw payload is missing input nullifiers");
  }
  const normalized = candidates.map(strictNullifierHex);
  if (normalized.some((value) => !value)) {
    throw new Error("relay withdraw payload contains an invalid input nullifier");
  }
  return [...new Set(normalized)];
}

// Every payload that can spend the same notes must share one submission lock,
// regardless of its recipient, fee, or payload hash. Sorting also makes the
// key independent of the input ordering used by the payload producer.
export function relayPayloadNullifierLockKey(payload = {}) {
  return relayPayloadNullifiers(payload).sort().join(":");
}

export async function submitRelayAfterNullifierPreflight({
  payload,
  checkNullifiers,
  submit,
} = {}) {
  if (typeof checkNullifiers !== "function" || typeof submit !== "function") {
    throw new Error("relay withdraw nullifier preflight is not configured");
  }
  const nullifiers = relayPayloadNullifiers(payload);
  let statuses;
  try {
    statuses = await checkNullifiers(nullifiers);
  } catch {
    throw new Error("relay withdraw nullifier status is unavailable");
  }
  if (!(statuses instanceof Map)) {
    throw new Error("relay withdraw nullifier status is missing or malformed");
  }
  for (const nullifier of nullifiers) {
    if (!statuses.has(nullifier) || statuses.get(nullifier) !== false) {
      throw new Error("relay withdraw requires explicitly unspent input nullifiers");
    }
  }
  return submit();
}

// An expired worker lease can recover Reserved/Proving work, but a ProofReady
// artifact must be reviewed because its proof may still be usable elsewhere.
export function canReplanExpiredLocalReservation({
  localPreBroadcast = false,
  workerExpired = false,
  hasProofReady = false,
} = {}) {
  return Boolean(localPreBroadcast && workerExpired && !hasProofReady);
}

export function expiredRelayReservationRecoveryTarget({
  handedOff = false,
  localWorkerState = false,
  localPreBroadcast = false,
  workerExpired = false,
  hasProofReady = false,
} = {}) {
  if (handedOff || !localWorkerState || !workerExpired) return "";
  if (hasProofReady) return "ManualReview";
  return canReplanExpiredLocalReservation({
    localPreBroadcast,
    workerExpired,
    hasProofReady,
  })
    ? "ReplanRequired"
    : "";
}

function sanitizeReservationRecord(record = {}) {
  return {
    reservation_id: String(record.reservation_id || ""),
    operation_id: String(record.operation_id || record.operationId || ""),
    status: String(record.status || ""),
    payload_hash: String(record.payload_hash || record.payloadHash || ""),
    // Transaction identities are the only broadcast evidence that relay
    // recovery metadata may retain. They are needed to reconcile a restart,
    // but do not expose the raw payload, proof, recipient, or amount.
    submitted_tx_hash: String(
      record.submitted_tx_hash || record.submittedTxHash || "",
    ),
    tx_bytes_hash: String(record.tx_bytes_hash || record.txBytesHash || ""),
    sign_doc_hash: String(record.sign_doc_hash || record.signDocHash || ""),
    broadcast_in_flight:
      record.broadcast_in_flight === true || record.broadcastInFlight === true,
    broadcast_attempt_count: Number(
      record.broadcast_attempt_count ?? record.broadcastAttemptCount ?? 0,
    ),
  };
}

export function sanitizeReservationBatch(batch) {
  if (!batch) return null;
  if (batch.reservations != null && !Array.isArray(batch.reservations)) {
    return null;
  }
  const ids = relayReservationIDs(batch);
  return {
    operation_id: String(batch.operation_id || batch.operationId || ""),
    reservation_ids: ids,
    reservations: (Array.isArray(batch.reservations) ? batch.reservations : [])
      .map(sanitizeReservationRecord)
      .filter((record) => record.reservation_id),
  };
}

function hasCompleteRelayReservationEvidence(reservation) {
  const operationID = String(reservation?.operation_id || "").trim();
  const ids = relayReservationIDs(reservation).map((id) => id.trim());
  const records = reservation?.reservations;
  if (
    !operationID ||
    !ids.length ||
    new Set(ids).size !== ids.length ||
    ids.some((id) => !id) ||
    !Array.isArray(records) ||
    records.length !== ids.length
  ) {
    return false;
  }

  const expectedIDs = new Set(ids);
  const seenIDs = new Set();
  return records.every((record) => {
    const reservationID = String(record.reservation_id || "").trim();
    if (!reservationID || !expectedIDs.has(reservationID) || seenIDs.has(reservationID)) {
      return false;
    }
    seenIDs.add(reservationID);
    return (
      String(record.operation_id || "").trim() === operationID &&
      Boolean(String(record.status || "").trim())
    );
  });
}

export function sanitizeRelayWithdrawSnapshot(snapshot) {
  if (!snapshot) return null;
  const reservation = sanitizeReservationBatch(snapshot.reservation);
  // Recovery metadata is only useful when it can be tied back to every
  // reservation in its operation. A payload hash alone is not enough to
  // reconcile, replan, or present an operation safely after restart.
  if (!reservation || !hasCompleteRelayReservationEvidence(reservation)) {
    return null;
  }
  const payloadHash = String(
    snapshot.payloadHash ||
      snapshot.payload_hash ||
      snapshot.payload?.payload_hash ||
      snapshot.payload?.payloadHash ||
      reservation?.reservations?.find((record) => record.payload_hash)
        ?.payload_hash ||
      "",
  );
  const reservationIDs = relayReservationIDs(reservation);
  if (!payloadHash && !reservationIDs.length) return null;
  // Never carry a caller-controlled persistence identifier forward.  The
  // relay-metadata record is deliberately an allowlist, and its key must be
  // derived only from the opaque payload hash or the reservation IDs.
  const metadataID = payloadHash || reservationIDs.join(":");
  return {
    id: metadataID,
    reservation,
    // Refresh snapshots retain reconciliation metadata only, not
    // payload-adjacent display values or arbitrary caller fields.
    expiresAtUnix: relaySnapshotExpiresAtUnix(snapshot),
    payloadHash,
    submitted: Boolean(snapshot.submitted),
    handedOff: Boolean(snapshot.handedOff || snapshot.handed_off),
    relayHash: String(snapshot.relayHash || snapshot.relay_hash || ""),
    relayHeight: String(snapshot.relayHeight || snapshot.relay_height || ""),
  };
}

export function parsePersistedRelayWithdrawState(raw) {
  if (raw == null || raw === "") {
    return { valid: true, current: null, pending: [] };
  }
  let saved;
  try {
    saved = typeof raw === "string" ? JSON.parse(raw) : raw;
  } catch {
    return { valid: false, current: null, pending: [] };
  }
  if (!saved || typeof saved !== "object" || Array.isArray(saved)) {
    return { valid: false, current: null, pending: [] };
  }
  if (saved.pending != null && !Array.isArray(saved.pending)) {
    return { valid: false, current: null, pending: [] };
  }
  const pending = [];
  for (const snapshot of saved.pending || []) {
    const sanitized = sanitizeRelayWithdrawSnapshot(snapshot);
    if (!sanitized) return { valid: false, current: null, pending: [] };
    pending.push(sanitized);
  }
  const current = saved.current ? sanitizeRelayWithdrawSnapshot(saved.current) : null;
  if (saved.current && !current) {
    return { valid: false, current: null, pending: [] };
  }
  return { valid: true, current, pending };
}

export function updateReservationBatchRecords(batch, records = []) {
  if (!batch || !records.length) return batch;
  const sanitized = records.map(sanitizeReservationRecord);
  const byID = new Map(
    sanitized.map((record) => [record.reservation_id, record]),
  );
  const existing = Array.isArray(batch.reservations) ? batch.reservations : [];
  const reservations = relayReservationIDs(batch).map(
    (id) =>
      byID.get(id) ||
      existing.find((record) => record.reservation_id === id) || {
        reservation_id: id,
        status: "",
        payload_hash: "",
      },
  );
  return {
    ...batch,
    reservations,
  };
}

export function relayReservationStatus(reservation, currentRecords = null) {
  const ids = relayReservationIDs(reservation);
  if (!ids.length) return "";
  const statuses = new Set();
  for (const id of ids) {
    const hasAuthoritativeRecords = typeof currentRecords?.get === "function";
    const current = hasAuthoritativeRecords ? currentRecords.get(id) : null;
    const embedded = Array.isArray(reservation?.reservations)
      ? reservation.reservations.find((record) => record.reservation_id === id)
      : null;
    const status = String(
      current?.status || (hasAuthoritativeRecords ? "" : embedded?.status) || "",
    );
    if (!status) return "";
    statuses.add(status);
  }
  return statuses.size === 1 ? [...statuses][0] : "mixed";
}

function relaySnapshotPayloadHash(snapshot = {}) {
  const declared = firstNonEmptyString(snapshot.payloadHash, snapshot.payload_hash);
  const embedded = firstNonEmptyString(
    snapshot.payload?.payload_hash,
    snapshot.payload?.payloadHash,
  );
  if (!declared || !embedded || declared !== embedded) return "";
  return declared;
}

function relayReservationRecord(reservation, reservationID, currentRecords) {
  if (typeof currentRecords?.get === "function") {
    return currentRecords.get(reservationID) || null;
  }
  return Array.isArray(reservation?.reservations)
    ? reservation.reservations.find(
        (record) => record.reservation_id === reservationID,
      )
    : null;
}

// This is intentionally chain-time independent so the UI can offer a valid
// prepared payload. The submission path must call canRelaySnapshotBeSubmitted
// with fresh chain time immediately before handing the payload to a relayer.
export function isRelaySnapshotStructurallyReady(
  snapshot,
  currentRecords,
  reservationStatuses,
) {
  if (
    !snapshot?.payload ||
    snapshot.submitted ||
    snapshot.handedOff ||
    snapshot.handed_off ||
    firstNonEmptyString(snapshot.relayHash, snapshot.relay_hash)
  ) {
    return false;
  }
  if (strictPositiveUnixSeconds(relaySnapshotExpiresAtUnix(snapshot)) == null) {
    return false;
  }
  const payloadHash = relaySnapshotPayloadHash(snapshot);
  if (!payloadHash) return false;
  const reservation = snapshot.reservation;
  const operationID = String(reservation?.operation_id || reservation?.operationId || "");
  const ids = relayReservationIDs(reservation);
  if (!operationID || !ids.length || new Set(ids).size !== ids.length) {
    return false;
  }
  return ids.every((reservationID) => {
    const record = relayReservationRecord(
      reservation,
      reservationID,
      currentRecords,
    );
    return (
      record &&
      String(record.status || "") === reservationStatuses.ProofReady &&
      String(record.operation_id || record.operationId || "") === operationID &&
      String(record.payload_hash || record.payloadHash || "") === payloadHash &&
      record.broadcast_in_flight !== true &&
      record.broadcastInFlight !== true &&
      record.relay_handed_off !== true &&
      record.relayHandedOff !== true &&
      record.metadata?.relay_handed_off !== true &&
      record.metadata?.relayHandedOff !== true &&
      Number(record.broadcast_attempt_count ?? record.broadcastAttemptCount ?? 0) === 0
    );
  });
}

// A copied payload may be copied again after a clipboard failure, but only
// while the durable reservation still proves that no local broadcast attempt
// has started. Unlike submission readiness, this requires the recorded relay
// handoff evidence and must use records freshly read from the reservation
// store rather than the snapshot embedded in UI state.
export function canRelayHandoffPayloadBeCopied(
  snapshot,
  currentRecords,
  reservationStatuses,
) {
  if (
    !snapshot?.payload ||
    snapshot.submitted ||
    firstNonEmptyString(snapshot.relayHash, snapshot.relay_hash) ||
    strictPositiveUnixSeconds(relaySnapshotExpiresAtUnix(snapshot)) == null ||
    typeof currentRecords?.get !== "function"
  ) {
    return false;
  }
  const payloadHash = relaySnapshotPayloadHash(snapshot);
  if (!payloadHash) return false;
  const reservation = snapshot.reservation;
  const operationID = String(reservation?.operation_id || reservation?.operationId || "");
  const ids = relayReservationIDs(reservation);
  if (!operationID || !ids.length || new Set(ids).size !== ids.length) {
    return false;
  }
  return ids.every((reservationID) => {
    const record = relayReservationRecord(
      reservation,
      reservationID,
      currentRecords,
    );
    const metadata = record?.metadata || {};
    return (
      record &&
      String(record.status || "") === reservationStatuses.ProofReady &&
      String(record.operation_id || record.operationId || "") === operationID &&
      String(record.payload_hash || record.payloadHash || "") === payloadHash &&
      (record.relay_handed_off === true ||
        record.relayHandedOff === true ||
        metadata.relay_handed_off === true ||
        metadata.relayHandedOff === true) &&
      !record.submitted_tx_hash &&
      !record.submittedTxHash &&
      !record.tx_bytes_hash &&
      !record.txBytesHash &&
      !record.sign_doc_hash &&
      !record.signDocHash &&
      record.broadcast_in_flight !== true &&
      record.broadcastInFlight !== true &&
      Number(record.broadcast_attempt_count ?? record.broadcastAttemptCount ?? 0) === 0
    );
  });
}

export function canRelaySnapshotBeSubmitted(
  snapshot,
  currentRecords,
  reservationStatuses,
  chainNowMs,
) {
  if (!isRelaySnapshotStructurallyReady(snapshot, currentRecords, reservationStatuses)) {
    return false;
  }
  if (!hasValidRelayChainTime(chainNowMs)) return false;
  if (relaySnapshotIsExpired(snapshot, chainNowMs)) return false;
  return true;
}
