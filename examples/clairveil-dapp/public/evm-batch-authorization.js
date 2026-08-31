function normalizedUint(value, label, bits) {
  let parsed;
  try {
    parsed = BigInt(value);
  } catch {
    throw new Error(`${label} must be a uint${bits}`);
  }
  if (parsed < 0n || parsed >= (1n << BigInt(bits))) {
    throw new Error(`${label} must be a uint${bits}`);
  }
  return parsed;
}

function normalizedEvmAddress(value, label = "EVM account") {
  const address = String(value || "").trim().toLowerCase();
  if (!/^0x[0-9a-f]{40}$/.test(address)) {
    throw new Error(`${label} must be a 20-byte hex address`);
  }
  return address;
}

export function evmBatchAuthorizationKinds(profile = {}) {
  if (profile?.transport !== "evm" || !profile.evmAuthorizationProfile?.typedDataDomain?.name) {
    return [];
  }
  const configured = profile.evmAuthorizationProfile.supportedAuthorizationKinds;
  const values = configured == null
    ? Array.from({ length: 256 }, (_, kind) => kind)
    : configured;
  return [...new Set(values.map(value => Number(normalizedUint(value, "authorization kind", 8))))];
}

export function evmBatchAuthorizationAvailable(profile = {}) {
  return evmBatchAuthorizationKinds(profile).length > 0;
}

export function randomEvmAuthorizationNonce(cryptoImpl = globalThis.crypto) {
  if (!cryptoImpl?.getRandomValues) {
    throw new Error("Secure browser randomness is required for EVM authorization nonce generation");
  }
  const words = cryptoImpl.getRandomValues(new Uint32Array(4));
  return [...words]
    .map(word => word.toString(16).padStart(8, "0"))
    .join("");
}

export function selfSubmittedEvmBatchAuthorization({
  profile,
  account,
  authorizationKind,
  nonce,
  deadline
} = {}) {
  const kinds = evmBatchAuthorizationKinds(profile);
  if (!kinds.length) {
    throw new Error("The active EVM profile does not configure an EIP-712 authorization domain");
  }
  const kind = Number(normalizedUint(authorizationKind, "authorization kind", 8));
  if (!kinds.includes(kind)) {
    throw new Error(`authorization kind ${kind} is not allowed by the active EVM profile`);
  }
  const sender = normalizedEvmAddress(account, "connected EVM account");
  const nonceText = String(nonce || "").trim();
  const nonceValue = /^0x/i.test(nonceText) ? nonceText : `0x${nonceText}`;
  return {
    effectiveSender: sender,
    executor: sender,
    nonce: normalizedUint(nonceValue, "authorization nonce", 256).toString(),
    deadline: normalizedUint(deadline, "authorization deadline", 64).toString(),
    authorizationKind: kind
  };
}
