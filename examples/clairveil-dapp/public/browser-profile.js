export function normalizeBrowserProfileEndpoints(profile, {
  rpc,
  rest,
  proverUrl,
  depositProofUrl = ""
} = {}) {
  if (!profile || typeof profile !== "object" || Array.isArray(profile)) {
    throw new Error("browser profile is required");
  }

  const normalized = {
    ...profile,
    rpc,
    rest,
    proverUrl,
    ...(depositProofUrl ? { depositProofUrl } : {})
  };
  if (normalized.transport === "cosmos" && normalized.keplrChainInfo) {
    normalized.keplrChainInfo = {
      ...normalized.keplrChainInfo,
      rpc,
      rest
    };
  }
  return normalized;
}
