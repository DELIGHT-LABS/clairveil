const preparationStatuses = new Set(["Reserved", "Proving", "ProofReady", "ManualReview"]);
const transactionStatuses = new Set(["Submitted", "Unknown"]);

function reservationMetadata(record) {
  return record?.metadata && typeof record.metadata === "object" ? record.metadata : {};
}

export function reservationOperationKey(record = {}) {
  return String(
    record.operation_id
      || record.operationId
      || record.reservation_id
      || record.reservationId
      || ""
  );
}

export function groupReservationOperations(records = []) {
  const grouped = new Map();
  for (const record of records || []) {
    if (!record) continue;
    const key = reservationOperationKey(record);
    if (!key) continue;
    const operation = grouped.get(key) || { key, records: [] };
    operation.records.push(record);
    grouped.set(key, operation);
  }
  return [...grouped.values()]
    .map(operation => ({
      ...operation,
      records: operation.records.sort((left, right) => String(left.reservation_id || "")
        .localeCompare(String(right.reservation_id || "")))
    }))
    .sort((left, right) => String(left.records[0]?.created_at || "")
      .localeCompare(String(right.records[0]?.created_at || "")));
}

export function assessReservationRecovery(records = [], {
  leaseOwner = "",
  nowMs = Date.now()
} = {}) {
  const reservations = [...(records || [])].filter(Boolean);
  const statuses = [...new Set(reservations.map(record => String(record.status || "")))];
  const kinds = [...new Set(reservations.map(record => String(record.kind || "unknown")))];
  const metadata = reservations.map(reservationMetadata);
  const broadcastAttempted = reservations.some((record, index) => (
    record.broadcast_in_flight === true
      || Number(record.broadcast_attempt_count || 0) > 0
      || metadata[index].no_broadcast_attempt === false
      || Boolean(String(record.submitted_tx_hash || "").trim())
  ));
  const relayHandedOff = reservations.some((record, index) => (
    metadata[index].relay_handed_off === true
      || metadata[index].relayHandedOff === true
      || String(record.kind || "").toLowerCase().includes("relay")
  ));
  const liveLeaseRecords = reservations.filter(record => {
    const leaseUntil = Date.parse(String(record.lease_until || ""));
    return Number.isFinite(leaseUntil) && leaseUntil > nowMs;
  });
  const liveLeaseOwners = [...new Set(liveLeaseRecords.map(record => String(record.lease_owner || "")).filter(Boolean))];
  const liveLeaseTokens = [...new Set(liveLeaseRecords.map(record => String(record.lease_token || "")).filter(Boolean))];
  const foreignLiveLease = liveLeaseRecords.some(record => String(record.lease_owner || "") !== String(leaseOwner || ""));
  const malformedLiveLease = liveLeaseRecords.length > 0
    && (liveLeaseOwners.length !== 1 || liveLeaseTokens.length !== 1);
  const status = statuses.length === 1 ? statuses[0] : "Mixed";
  const kind = kinds.length === 1 ? kinds[0] : "mixed";
  const leaseUntil = reservations
    .map(record => String(record.lease_until || ""))
    .filter(Boolean)
    .sort()
    .at(-1) || "";

  let action = "review-replan";
  let reason = "Verify current nullifier evidence before discarding this local preparation.";
  if (!reservations.length) {
    action = "unavailable";
    reason = "No reservation records are available.";
  } else if (statuses.length !== 1) {
    action = "unavailable";
    reason = "Linked reservations have mixed states and require manual evidence review.";
  } else if (relayHandedOff) {
    action = "relay-reconcile";
    reason = "A relay handoff can only be recovered through its expiry and transaction reconciliation flow.";
  } else if (broadcastAttempted || transactionStatuses.has(status)) {
    action = "reconcile";
    reason = "A transaction may have reached the network. Reconcile its tx hash and nullifiers before retrying.";
  } else if (!preparationStatuses.has(status)) {
    action = "unavailable";
    reason = `Reservation status ${status || "unknown"} is not eligible for preparation recovery.`;
  } else if (status === "Proving" && liveLeaseRecords.length > 0) {
    action = "wait-for-lease";
    reason = "Proof generation is still active. Use the current transfer flow to cancel it, or wait for its lease to expire.";
  } else if (foreignLiveLease) {
    action = "wait-for-lease";
    reason = "Another browser tab or worker still owns the live reservation lease.";
  } else if (malformedLiveLease) {
    action = "unavailable";
    reason = "The linked reservations do not share one recoverable live lease.";
  }

  return Object.freeze({
    operationKey: reservationOperationKey(reservations[0]),
    reservationIDs: Object.freeze(reservations.map(record => String(record.reservation_id || "")).filter(Boolean)),
    status,
    kind,
    action,
    reason,
    broadcastAttempted,
    relayHandedOff,
    leaseLive: liveLeaseRecords.length > 0,
    leaseOwnedByCurrentWorker: liveLeaseRecords.length > 0 && !foreignLiveLease && !malformedLiveLease,
    leaseToken: liveLeaseTokens.length === 1 ? liveLeaseTokens[0] : "",
    leaseUntil
  });
}

function isZeroReserveAmount(value) {
  return /^(?:0+)$/.test(String(value ?? ""));
}

export function isEmptyLocalGenesisPrivacyState({
  localTestMode = false,
  reserve
} = {}) {
  return localTestMode === true
    && reserve?.invariant_holds === true
    && [
      reserve.module_balance,
      reserve.expected_module_balance,
      reserve.total_deposited,
      reserve.total_withdrawn
    ].every(isZeroReserveAmount);
}

export function canResetStaleLocalGenesisReservations({
  localTestMode = false,
  reserve,
  notes = [],
  noteSyncStatus = "",
  scanHasMore = true,
  assessments = []
} = {}) {
  return isEmptyLocalGenesisPrivacyState({ localTestMode, reserve })
    && Array.isArray(notes)
    && notes.length === 0
    && noteSyncStatus === "synced"
    && scanHasMore === false
    && Array.isArray(assessments)
    && assessments.length > 0
    && assessments.every(assessment => assessment?.action === "review-replan");
}
