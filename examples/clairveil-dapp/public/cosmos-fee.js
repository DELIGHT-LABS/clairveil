function decimalRational(value, label = "decimal value") {
  const text = String(value ?? "").trim();
  const match = /^(0|[1-9][0-9]*)(?:\.([0-9]+))?(?:[eE]([+-]?[0-9]+))?$/.exec(text);
  if (!match) throw new Error(`${label} must be a non-negative decimal`);

  const fraction = match[2] || "";
  const exponent = Number(match[3] || 0);
  if (!Number.isSafeInteger(exponent) || Math.abs(exponent) > 1000) {
    throw new Error(`${label} exponent is outside the supported range`);
  }
  const digits = `${match[1]}${fraction}`.replace(/^0+(?=\d)/, "");
  const scale = fraction.length - exponent;
  if (scale <= 0) {
    return { numerator: BigInt(digits) * (10n ** BigInt(-scale)), denominator: 1n };
  }
  return { numerator: BigInt(digits), denominator: 10n ** BigInt(scale) };
}

function canonicalGasLimit(value) {
  if (typeof value === "bigint") {
    if (value < 0n) throw new Error("Cosmos gas limit must be non-negative");
    return value;
  }
  const text = String(value ?? "").trim();
  if (!/^(0|[1-9][0-9]*)$/.test(text)) {
    throw new Error("Cosmos gas limit must be a canonical non-negative integer");
  }
  return BigInt(text);
}

export function cosmosGasFeeAmount(gasPrice, gasLimit) {
  const gas = canonicalGasLimit(gasLimit);
  const { numerator, denominator } = decimalRational(gasPrice, "Cosmos gas price");
  const product = gas * numerator;
  return product === 0n ? 0n : (product + denominator - 1n) / denominator;
}

export function deterministicCosmosFeeAmount({ gasPrice, gasLimit, denom } = {}) {
  const normalizedDenom = String(denom || "").trim();
  if (!normalizedDenom) throw new Error("Cosmos fee denom is required");
  return Object.freeze([Object.freeze({
    denom: normalizedDenom,
    amount: cosmosGasFeeAmount(gasPrice, gasLimit).toString()
  })]);
}
