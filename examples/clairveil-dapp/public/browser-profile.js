export function browserLoopbackRewriteEnabled(config) {
  return config?.serverBacked === true
    && config?.localTestMode === true
    && config?.serverFeatures?.localTestMode === true;
}

export function normalizeBrowserEndpointUrl(configured, {
  browserHostname = "",
  trim = false,
  localTestMode = false
} = {}) {
  try {
    const url = new URL(configured);
    if (
      localTestMode === true
      && (url.hostname === "127.0.0.1" || url.hostname === "localhost")
      && browserHostname
    ) {
      url.hostname = browserHostname;
    }
    const text = url.toString();
    return trim ? text.replace(/\/$/, "") : text;
  } catch {
    return trim ? String(configured || "").replace(/\/$/, "") : configured;
  }
}

export function normalizeBrowserRestEndpoints(profile, {
  browserHostname = "",
  selectedEndpoint = "",
  localTestMode = false
} = {}) {
  const values = [
    selectedEndpoint,
    profile?.rest,
    ...(Array.isArray(profile?.restEndpoints) ? profile.restEndpoints : [])
  ];
  return [...new Set(values
    .map(value => normalizeBrowserEndpointUrl(value, {
      browserHostname,
      trim: true,
      localTestMode
    }))
    .map(value => String(value || "").trim())
    .filter(Boolean))];
}

export function normalizeBrowserProfileEndpoints(profile, {
  rpc,
  rest,
  restEndpoints,
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
    ...(Array.isArray(restEndpoints) && restEndpoints.length ? { restEndpoints } : {}),
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
