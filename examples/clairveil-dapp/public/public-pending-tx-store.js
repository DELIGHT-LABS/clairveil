export const publicPendingTxStateVersion = "clairveil-public-pending-tx-state-v4";
export const privacyPendingTxStateVersion = "clairveil-privacy-pending-tx-state-v1";

const unresolvedStatuses = new Set([
  "attempting",
  "submitted",
  "unknown",
  "checking",
  "recovery-pending"
]);

function normalizedPendingEntry(entry, { allowRecoveryPending = false } = {}) {
  if (entry == null) return null;
  const txHash = String(entry?.txHash || "").trim();
  const attemptId = String(entry?.attemptId || "").trim().toLowerCase();
  const status = String(entry?.status || "").trim();
  const attempting = status === "attempting";
  if (!unresolvedStatuses.has(status)
    || (status === "recovery-pending" && !allowRecoveryPending)
    || (attempting && (!/^[0-9a-f]{64}$/.test(attemptId) || txHash))
    || (!attempting && !/^(0x)?[0-9a-fA-F]{64}$/.test(txHash))
    || (attemptId && !/^[0-9a-f]{64}$/.test(attemptId))) {
    throw new Error("pending transaction entry is invalid");
  }
  return {
    txHash,
    status: status === "checking" ? "unknown" : status,
    ...(attemptId ? { attemptId } : {}),
    ...(entry?.height ? { height: String(entry.height) } : {})
  };
}

function normalizedPrivacyPendingEntry(entry) {
  const normalized = normalizedPendingEntry(entry);
  if (normalized?.status === "attempting" || !normalized?.txHash) {
    throw new Error("privacy pending transaction entry requires an exact transaction hash");
  }
  return normalized;
}

export function publicPendingTxKey({ profileId, owner, storageEpoch = "" }) {
  const normalizedProfile = String(profileId || "").trim();
  const normalizedOwner = String(owner || "").trim().toLowerCase();
  const normalizedEpoch = String(storageEpoch || "").trim().toLowerCase();
  return normalizedProfile && normalizedOwner
    ? `clairveil:v0.3.1:public-pending:${normalizedProfile}:${normalizedEpoch ? `${normalizedEpoch}:` : ""}${normalizedOwner}`
    : "";
}

export function privacyPendingTxKey({ profileId, owner, storageEpoch = "" }) {
  const normalizedProfile = String(profileId || "").trim();
  const normalizedOwner = String(owner || "").trim().toLowerCase();
  const normalizedEpoch = String(storageEpoch || "").trim().toLowerCase();
  return normalizedProfile && normalizedOwner
    ? `clairveil:v0.3.1:privacy-pending:${normalizedProfile}:${normalizedEpoch ? `${normalizedEpoch}:` : ""}${normalizedOwner}`
    : "";
}

function assertPendingIdentity(value, version, { profileId, owner, storageEpoch = "" } = {}) {
  if (value?.version !== version
    || value.profileId !== String(profileId || "")
    || value.owner !== String(owner || "").toLowerCase()
    || String(value.storageEpoch || "") !== String(storageEpoch || "").toLowerCase()) {
    throw new Error("pending transaction identity does not match");
  }
}

export function loadPublicPendingTxState(storage, key, { profileId, owner, storageEpoch = "" } = {}) {
  const raw = storage?.getItem(key);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    assertPendingIdentity(value, publicPendingTxStateVersion, { profileId, owner, storageEpoch });
    const send = normalizedPendingEntry(value.send);
    const deposit = normalizedPendingEntry(value.deposit, { allowRecoveryPending: true });
    if (!send && !deposit) throw new Error("pending transaction state is empty");
    return { send, deposit };
  } catch (cause) {
    const error = new Error("Pending public transaction state is corrupt. Clear it only after checking wallet history and tx hashes.", { cause });
    error.code = "PUBLIC_PENDING_STATE_CORRUPT";
    throw error;
  }
}

export function savePublicPendingTxState(storage, key, state = {}) {
  if (Object.prototype.hasOwnProperty.call(state, "privacy")) {
    throw new Error("private Cosmos transaction fences require the separate privacy pending store");
  }
  const {
    profileId,
    owner,
    storageEpoch = "",
    send,
    deposit
  } = state;
  const normalizedSend = unresolvedStatuses.has(String(send?.status || ""))
    ? normalizedPendingEntry(send)
    : null;
  const normalizedDeposit = unresolvedStatuses.has(String(deposit?.status || ""))
    ? normalizedPendingEntry(deposit, { allowRecoveryPending: true })
    : null;
  if (!normalizedSend && !normalizedDeposit) {
    storage?.removeItem(key);
    return;
  }
  storage?.setItem(key, JSON.stringify({
    version: publicPendingTxStateVersion,
    profileId: String(profileId || ""),
    owner: String(owner || "").toLowerCase(),
    ...(storageEpoch ? { storageEpoch: String(storageEpoch).toLowerCase() } : {}),
    send: normalizedSend,
    deposit: normalizedDeposit
  }));
}

export function loadPrivacyPendingTxState(storage, key, { profileId, owner, storageEpoch = "" } = {}) {
  const raw = storage?.getItem(key);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    assertPendingIdentity(value, privacyPendingTxStateVersion, { profileId, owner, storageEpoch });
    const privacy = normalizedPrivacyPendingEntry(value.privacy);
    if (!privacy) throw new Error("privacy pending transaction state is empty");
    return privacy;
  } catch (cause) {
    const error = new Error(
      "The private Cosmos transaction fence is corrupt and cannot be cleared selectively. Keep it fail-closed and use a reviewed fresh-state reset only after checking wallet and chain history.",
      { cause }
    );
    error.code = "PRIVACY_PENDING_STATE_CORRUPT";
    throw error;
  }
}

export function savePrivacyPendingTxState(storage, key, {
  profileId,
  owner,
  storageEpoch = "",
  privacy
} = {}) {
  const normalizedPrivacy = unresolvedStatuses.has(String(privacy?.status || ""))
    ? normalizedPrivacyPendingEntry(privacy)
    : null;
  if (!normalizedPrivacy) {
    storage?.removeItem(key);
    return;
  }
  storage?.setItem(key, JSON.stringify({
    version: privacyPendingTxStateVersion,
    profileId: String(profileId || ""),
    owner: String(owner || "").toLowerCase(),
    ...(storageEpoch ? { storageEpoch: String(storageEpoch).toLowerCase() } : {}),
    privacy: normalizedPrivacy
  }));
}
