function canonicalCosmosTxCode(value) {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? value : null;
  }
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/.test(value)) return null;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function checkTxRejectionMarker(value = {}) {
  const checkTxRejected = value.checkTxRejected === true || value.check_tx_rejected === true;
  return checkTxRejected && (value.rpcInvoked === true || value.rpc_invoked === true);
}

function reservationBookkeepingFailed(value = {}) {
  return value.reservationReconciliationRequired === true
    || value.reservation_reconciliation_required === true
    || value.reservationBookkeepingError != null
    || value.reservation_bookkeeping_error != null;
}

function explicitRejectionMarker(value = {}) {
  const legacyMarker = [
    value.explicitBroadcastRejection,
    value.explicit_broadcast_rejection,
    value.broadcastRejected,
    value.broadcast_rejected
  ].some(marker => marker === true || (marker && typeof marker === "object"));
  return legacyMarker || checkTxRejectionMarker(value);
}

function topLevelBroadcastErrorCode(value = {}) {
  const hasBroadcastErrorShape = ["code", "codespace", "log"].every(key => (
    Object.prototype.hasOwnProperty.call(value, key)
  ));
  return hasBroadcastErrorShape ? canonicalCosmosTxCode(value.code) : null;
}

export function cosmosTxEvidenceConfirmsFailure(value = {}) {
  const sources = [value, value?.broadcast, value?.cause].filter(source => (
    source && typeof source === "object"
  ));
  if (sources.some(explicitRejectionMarker)) return true;

  const codes = [
    value?.tx?.code,
    value?.broadcast?.tx?.code,
    ...sources.map(topLevelBroadcastErrorCode)
  ];
  return codes.some(code => {
    const parsed = canonicalCosmosTxCode(code);
    return parsed != null && parsed > 0;
  });
}

function normalizedTxHash(value) {
  const hash = String(value || "").trim().replace(/^0x/i, "").toLowerCase();
  return /^[0-9a-f]{64}$/.test(hash) ? hash : "";
}

function reservationHasDurableBroadcastAttempt(record = {}) {
  return record.broadcast_in_flight === true
    || Number(record.broadcast_attempt_count || 0) > 0
    || ["Submitted", "Unknown", "ConfirmedSpent"].includes(String(record.status || ""));
}

export function cosmosReservationTransactionHash(record = {}) {
  const rawSubmitted = String(record.submitted_tx_hash || "").trim();
  const rawTxBytes = String(record.tx_bytes_hash || "").trim();
  const submitted = normalizedTxHash(rawSubmitted);
  const txBytes = normalizedTxHash(rawTxBytes);
  if ((rawSubmitted && !submitted)
    || (rawTxBytes && !txBytes)
    || (submitted && txBytes && submitted !== txBytes)) {
    return "";
  }
  if (submitted) return submitted;
  return txBytes && reservationHasDurableBroadcastAttempt(record) ? txBytes : "";
}

export function commonCosmosReservationTransactionHash(records = []) {
  if (!Array.isArray(records) || records.length === 0) return "";
  const hashes = records.map(cosmosReservationTransactionHash);
  if (hashes.some(hash => !hash)) return "";
  const unique = [...new Set(hashes)];
  return unique.length === 1 ? unique[0] : "";
}

export function cosmosPrivatePendingMarkerCanClear({ markerTxHash, txHash, error } = {}) {
  const marker = normalizedTxHash(markerTxHash);
  const submitted = normalizedTxHash(txHash);
  if (!marker || marker !== submitted) return false;
  const abortedBeforeRpc = error?.rpcInvoked === false || error?.rpc_invoked === false;
  const sources = [error, error?.broadcast, error?.cause]
    .filter(source => source && typeof source === "object");
  const checkTxRejected = sources.some(checkTxRejectionMarker);
  const terminalWriteFailed = sources.some(reservationBookkeepingFailed);
  return abortedBeforeRpc || (checkTxRejected && !terminalWriteFailed);
}
