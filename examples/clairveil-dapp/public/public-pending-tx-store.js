export const publicPendingTxStateVersion = "clairveil-public-pending-tx-state-v1";

const unresolvedStatuses = new Set(["submitted", "unknown", "checking"]);

function normalizedPendingEntry(entry) {
  if (entry == null) return null;
  const txHash = String(entry?.txHash || "").trim();
  const status = String(entry?.status || "").trim();
  if (!/^(0x)?[0-9a-fA-F]{64}$/.test(txHash) || !unresolvedStatuses.has(status)) {
    throw new Error("pending transaction entry is invalid");
  }
  return {
    txHash,
    status: status === "checking" ? "unknown" : status,
    ...(entry?.height ? { height: String(entry.height) } : {})
  };
}

export function publicPendingTxKey({ profileId, owner, storageEpoch = "" }) {
  const normalizedProfile = String(profileId || "").trim();
  const normalizedOwner = String(owner || "").trim().toLowerCase();
  const normalizedEpoch = String(storageEpoch || "").trim().toLowerCase();
  return normalizedProfile && normalizedOwner
    ? `clairveil:v0.3.1:public-pending:${normalizedProfile}:${normalizedEpoch ? `${normalizedEpoch}:` : ""}${normalizedOwner}`
    : "";
}

export function loadPublicPendingTxState(storage, key, { profileId, owner, storageEpoch = "" } = {}) {
  const raw = storage?.getItem(key);
  if (!raw) return null;
  try {
    const value = JSON.parse(raw);
    if (value?.version !== publicPendingTxStateVersion
      || value.profileId !== String(profileId || "")
      || value.owner !== String(owner || "").toLowerCase()
      || String(value.storageEpoch || "") !== String(storageEpoch || "").toLowerCase()) {
      throw new Error("pending transaction identity does not match");
    }
    const send = normalizedPendingEntry(value.send);
    const deposit = normalizedPendingEntry(value.deposit);
    if (!send && !deposit) throw new Error("pending transaction state is empty");
    return { send, deposit };
  } catch (cause) {
    const error = new Error("Pending public transaction state is corrupt. Clear it only after checking wallet history and tx hashes.", { cause });
    error.code = "PUBLIC_PENDING_STATE_CORRUPT";
    throw error;
  }
}

export function savePublicPendingTxState(storage, key, {
  profileId,
  owner,
  storageEpoch = "",
  send,
  deposit
} = {}) {
  const normalizedSend = unresolvedStatuses.has(String(send?.status || ""))
    ? normalizedPendingEntry(send)
    : null;
  const normalizedDeposit = unresolvedStatuses.has(String(deposit?.status || ""))
    ? normalizedPendingEntry(deposit)
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
