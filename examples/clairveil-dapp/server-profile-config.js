const supportedTransports = new Set(["cosmos", "evm"]);

function environmentValue(environment, name) {
  const value = environment?.[name];
  return value == null ? "" : String(value).trim();
}

export function normalizeConfiguredTransport(value = "cosmos") {
  const transport = String(value || "cosmos").trim().toLowerCase();
  if (!supportedTransports.has(transport)) {
    throw new Error("CLAIRVEIL_TRANSPORT must be cosmos or evm");
  }
  return transport;
}

export function resolveProfileDenom({
  transport,
  environment = {},
  fallbackDenom = "uclair",
} = {}) {
  const normalizedTransport = normalizeConfiguredTransport(transport);
  const transportKeys = normalizedTransport === "evm"
    ? ["CLAIRVEIL_EVM_DENOM", "CLAIRVEIL_EVM_NATIVE_DENOM"]
    : ["CLAIRVEIL_COSMOS_DENOM"];
  for (const name of [...transportKeys, "CLAIRVEIL_DENOM"]) {
    const value = environmentValue(environment, name);
    if (value) return value;
  }
  const fallback = String(fallbackDenom || "").trim();
  if (!fallback) throw new Error("profile denom fallback must not be empty");
  return fallback;
}
