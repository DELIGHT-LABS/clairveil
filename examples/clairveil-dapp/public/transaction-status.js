export function evmReceiptStatusKind(status) {
  if (typeof status === "number") {
    if (status === 1) return "success";
    if (status === 0) return "failure";
    return "unknown";
  }
  if (typeof status === "bigint") {
    if (status === 1n) return "success";
    if (status === 0n) return "failure";
    return "unknown";
  }
  if (typeof status !== "string") return "unknown";
  const normalized = status.trim().toLowerCase();
  if (/^(?:0x0*1|0*1)$/.test(normalized)) return "success";
  if (/^(?:0x0+|0+)$/.test(normalized)) return "failure";
  return "unknown";
}

export function hasSuccessfulEvmReceiptStatus(receipt) {
  return evmReceiptStatusKind(receipt?.status) === "success";
}

export function hasFailedEvmReceiptStatus(receipt) {
  return evmReceiptStatusKind(receipt?.status) === "failure";
}

export function isEvmReceiptConfirmationPending(error) {
  const broadcast = error?.broadcast;
  return Boolean(
    (error?.txHash || broadcast?.txHash) &&
      !broadcast?.receipt &&
      /receipt was not found yet|broadcast but not found yet/i.test(error?.message || ""),
  );
}

export function evmBlockChainSnapshot(block) {
  const timestampHex = String(block?.timestamp || "").trim();
  const heightHex = String(block?.number || "").trim();
  if (!/^0x[0-9a-f]+$/i.test(timestampHex) || !/^0x[0-9a-f]+$/i.test(heightHex)) {
    throw new Error("Latest EVM block time or height is unavailable");
  }
  const timestamp = BigInt(timestampHex);
  const height = BigInt(heightHex);
  if (timestamp > BigInt(Math.floor(Number.MAX_SAFE_INTEGER / 1000))) {
    throw new Error("Latest EVM block time exceeds the safe integer range");
  }
  return {
    chainNowMs: Number(timestamp) * 1000,
    chainHeight: height.toString(),
  };
}

export function shouldEscalateSuccessfulTxWithUnspentNullifiers({
  txHeight = 0,
  scanHeight = 0,
  observedAtMs = 0,
  nowMs = Date.now(),
  graceMs = 120_000,
} = {}) {
  const includedHeight = Number(txHeight || 0);
  const verifiedHeight = Number(scanHeight || 0);
  if (includedHeight > 0 && verifiedHeight < includedHeight) return false;
  const observed = Number(observedAtMs || 0);
  return observed > 0 && Number(nowMs) - observed >= Math.max(0, Number(graceMs) || 0);
}
