const blockHashPattern = /^[0-9a-f]{64}$/i;

export function localChainStorageEpoch({ localTestMode = false, status } = {}) {
  if (localTestMode !== true) return "";
  const value = String(
    status?.sync_info?.earliest_block_hash
      || status?.syncInfo?.earliestBlockHash
      || ""
  ).trim();
  return blockHashPattern.test(value) ? value.toLowerCase() : "";
}

export function walletStorageScope({
  chainId,
  profileId,
  owner,
  localTestMode = false,
  storageEpoch = ""
} = {}) {
  const normalizedChainID = String(chainId || "").trim();
  const normalizedProfileID = String(profileId || "").trim();
  const normalizedOwner = String(owner || "").trim().toLowerCase();
  const normalizedEpoch = String(storageEpoch || "").trim().toLowerCase();
  if (!normalizedChainID || !normalizedProfileID || !normalizedOwner) return null;
  if (localTestMode === true && !blockHashPattern.test(normalizedEpoch)) return null;

  const epochParts = localTestMode === true ? [normalizedEpoch] : [];
  return Object.freeze({
    storageEpoch: epochParts[0] || "",
    namespace: [normalizedChainID, normalizedProfileID, ...epochParts, normalizedOwner].join(":"),
    ownerKeyId: [normalizedChainID, ...epochParts, normalizedOwner].join(":"),
    keySuffix: [normalizedProfileID, ...epochParts, normalizedOwner].join(":")
  });
}
