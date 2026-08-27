const exactTxHashPattern = /^(?:0x)?[0-9a-fA-F]{64}$/;
const sameOriginLocalRelayerReason = "same_origin_local_relayer_submit";

function codedError(message, code) {
  const error = new Error(message);
  error.code = code;
  return error;
}

function canonicalUnixSeconds(value, label, { positive = false } = {}) {
  let parsed;
  if (typeof value === "number") {
    parsed = value;
  } else if (typeof value === "bigint") {
    parsed = Number(value);
  } else if (typeof value === "string" && /^(0|[1-9][0-9]*)$/.test(value)) {
    parsed = Number(value);
  } else {
    throw codedError(`${label} must be a canonical safe integer`, "INVALID_TRANSFER_EXPIRY");
  }
  if (!Number.isSafeInteger(parsed) || parsed < (positive ? 1 : 0)) {
    throw codedError(`${label} must be a ${positive ? "positive" : "non-negative"} safe integer`, "INVALID_TRANSFER_EXPIRY");
  }
  return parsed;
}

function canonicalTxHash(value) {
  const raw = String(value || "").trim();
  return exactTxHashPattern.test(raw) ? raw.replace(/^0x/i, "").toUpperCase() : "";
}

export function authoritativeChainBlockFromStatus(data, { chainId } = {}) {
  const expectedNetwork = String(chainId || "").trim();
  const observedNetwork = String(data?.result?.node_info?.network || "").trim();
  if (!expectedNetwork || observedNetwork !== expectedNetwork) {
    throw codedError(
      `Latest status network ${observedNetwork || "<missing>"} does not match active profile chain ID ${expectedNetwork || "<missing>"}`,
      "CHAIN_STATUS_NETWORK_MISMATCH"
    );
  }
  const value = data?.result?.sync_info?.latest_block_time;
  const milliseconds = Date.parse(String(value || ""));
  if (!Number.isFinite(milliseconds)) {
    throw new Error("Latest status response omitted a valid block timestamp");
  }
  const rawHeight = data?.result?.sync_info?.latest_block_height;
  const height = Number(rawHeight);
  if (!Number.isSafeInteger(height) || height <= 0) {
    throw new Error("Latest status response omitted a valid block height");
  }
  return { timeUnix: Math.floor(milliseconds / 1000), height };
}

export function typedPrivacyScanAfter(cursor = {}) {
  const candidate = cursor?.next_cursor
    ?? cursor?.nextCursor
    ?? cursor?.after
    ?? cursor?.afterCursor
    ?? null;
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) {
    return { height: 0, globalSequence: 0, outputIndex: 0 };
  }
  return { ...candidate };
}

export function preparedTransferExpiryUnix(data) {
  const payload = data?.prepared?.payload;
  if (!payload || typeof payload !== "object" || Array.isArray(payload)) {
    throw codedError("Prepared transfer payload is missing", "INVALID_TRANSFER_EXPIRY");
  }
  if (!Object.prototype.hasOwnProperty.call(payload, "expires_at_unix")) {
    throw codedError("Prepared transfer payload expires_at_unix is missing", "INVALID_TRANSFER_EXPIRY");
  }
  const expiry = canonicalUnixSeconds(
    payload.expires_at_unix,
    "Prepared transfer payload expires_at_unix",
    { positive: true }
  );
  return expiry;
}

export function assertPreparedTransferFreshAtChainTime(data, {
  chainNowUnix
} = {}) {
  const expiry = preparedTransferExpiryUnix(data);
  const chainNow = canonicalUnixSeconds(chainNowUnix, "Authoritative chain time");
  if (chainNow >= expiry) {
    throw codedError(
      `Prepared transfer expired at ${expiry}; authoritative chain time is ${chainNow}`,
      "TRANSFER_PAYLOAD_EXPIRED_BEFORE_BROADCAST"
    );
  }
  return expiry;
}

export function recoveredDepositNoteForTxHash(notes = [], txHash) {
  const expected = canonicalTxHash(txHash);
  if (!expected) {
    throw codedError("Deposit recovery requires an exact 32-byte transaction hash", "INVALID_DEPOSIT_TX_HASH");
  }
  return (Array.isArray(notes) ? notes : []).find(note => (
    canonicalTxHash(note?.tx_hash || note?.txHash) === expected
  )) || null;
}

function normalizedAccount(value) {
  return String(value || "").trim().toLowerCase();
}

export function isSeparateSameOriginLocalRelayerReservation(record, {
  browserAccount,
  localRelayer
} = {}) {
  const metadata = record?.metadata || {};
  const configuredName = String(localRelayer?.name || "").trim();
  const configuredAddress = normalizedAccount(localRelayer?.transparentAddress);
  const persistedName = String(metadata.local_relayer || "").trim();
  const persistedAddress = normalizedAccount(metadata.local_relayer_address);
  const walletAddress = normalizedAccount(browserAccount);
  return record?.kind === "relay_withdraw"
    && metadata.relay_handed_off !== true
    && String(metadata.broadcast_attempt_reason || "") === sameOriginLocalRelayerReason
    && Boolean(configuredName && configuredAddress && persistedName && persistedAddress && walletAddress)
    && persistedName === configuredName
    && persistedAddress === configuredAddress
    && persistedAddress !== walletAddress;
}

export function reservationConsumesBrowserCosmosSequence(record, context = {}) {
  return !isSeparateSameOriginLocalRelayerReservation(record, context);
}
