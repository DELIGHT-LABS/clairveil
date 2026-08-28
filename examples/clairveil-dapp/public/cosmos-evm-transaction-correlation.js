const HASH_PATTERN = /^(?:0x)?[0-9a-f]{64}$/i;

function canonicalTransactionHash(value, label) {
  if (typeof value !== "string") {
    throw new Error(`${label} must be a 32-byte transaction hash`);
  }
  const hash = value.trim();
  if (!HASH_PATTERN.test(hash)) {
    throw new Error(`${label} must be a 32-byte transaction hash`);
  }
  return `0x${hash.replace(/^0x/i, "").toLowerCase()}`;
}

function canonicalCometHeight(value) {
  let text = "";
  if (typeof value === "string") text = value.trim();
  if (typeof value === "number" && Number.isSafeInteger(value)) text = String(value);
  if (!/^[1-9][0-9]*$/.test(text)) {
    throw new Error("Comet transaction height must be a positive integer");
  }
  return BigInt(text).toString();
}

function cometResult(value) {
  const wrapped = value && typeof value === "object"
    && Object.prototype.hasOwnProperty.call(value, "result");
  const candidate = wrapped ? value.result : value;
  if (!candidate || typeof candidate !== "object" || Array.isArray(candidate)) {
    throw new Error("Comet transaction result is required");
  }
  return candidate;
}

function indexedEthereumTransactionHash(result, expectedEvmTxHash) {
  const events = result.tx_result?.events;
  if (!Array.isArray(events)) {
    throw new Error("Comet transaction events are required");
  }

  const attributes = [];
  for (const event of events) {
    if (!event || typeof event !== "object" || Array.isArray(event)) continue;
    if (event.attributes != null && !Array.isArray(event.attributes)) {
      throw new Error("Comet transaction event attributes must be an array");
    }
    for (const attribute of event.attributes || []) {
      if (attribute?.key !== "ethereumTxHash") continue;
      attributes.push({
        hash: canonicalTransactionHash(attribute.value, "indexed ethereumTxHash"),
        indexed: attribute.index === true
      });
    }
  }

  if (attributes.length === 0 || !attributes.some(attribute => attribute.indexed)) {
    throw new Error("Comet transaction is missing an indexed ethereumTxHash attribute");
  }
  const hashes = [...new Set(attributes.map(attribute => attribute.hash))];
  if (hashes.length !== 1) {
    throw new Error("Comet transaction contains conflicting ethereumTxHash attributes");
  }
  if (hashes[0] !== expectedEvmTxHash) {
    throw new Error("indexed ethereumTxHash does not match the expected Ethereum transaction");
  }
  return hashes[0];
}

export function verifyEvmScanTransactionLink({
  scanTxHash,
  evmTxHash,
  cometTransaction
} = {}) {
  const canonicalScanTxHash = canonicalTransactionHash(
    scanTxHash,
    "typed scan transaction hash"
  );
  const canonicalEvmTxHash = canonicalTransactionHash(
    evmTxHash,
    "expected Ethereum transaction hash"
  );
  const result = cometResult(cometTransaction);
  const resultHash = canonicalTransactionHash(result.hash, "Comet transaction hash");
  if (resultHash !== canonicalScanTxHash) {
    throw new Error("Comet transaction hash does not match the typed scan transaction");
  }
  if (result.tx_result?.code !== 0) {
    throw new Error("Comet transaction did not explicitly succeed");
  }
  indexedEthereumTransactionHash(result, canonicalEvmTxHash);

  return Object.freeze({
    scanTxHash: canonicalScanTxHash,
    evmTxHash: canonicalEvmTxHash,
    cometHeight: canonicalCometHeight(result.height),
    cosmosTxSucceeded: true,
    ethereumTxHashEventMatched: true
  });
}
