function canonicalAmount(value, label) {
  const text = String(value ?? "").trim();
  if (!/^(0|[1-9][0-9]*)$/.test(text)) {
    throw new Error(`${label} must be a canonical non-negative integer`);
  }
  return BigInt(text);
}

export function assertDepositFundingAvailable({
  amount,
  fee,
  assetBalance,
  nativeBalance,
  assetDenom,
  nativeDenom,
  transport
} = {}) {
  const amountValue = canonicalAmount(amount, "deposit amount");
  const feeValue = canonicalAmount(fee ?? "0", "deposit fee");
  const assetValue = canonicalAmount(assetBalance ?? "0", `${assetDenom || "asset"} balance`);
  const nativeValue = canonicalAmount(nativeBalance ?? assetBalance ?? "0", `${nativeDenom || assetDenom || "native"} balance`);
  const splitEvmFunding = transport === "evm" && String(assetDenom) !== String(nativeDenom);

  if (splitEvmFunding) {
    if (assetValue < amountValue) {
      throw new Error(`Insufficient transparent ${assetDenom} balance: need ${amountValue}${assetDenom}, available ${assetValue}${assetDenom}`);
    }
    if (nativeValue < feeValue) {
      throw new Error(`Insufficient EVM gas balance: need ${feeValue}${nativeDenom}, available ${nativeValue}${nativeDenom}`);
    }
    return {
      amount: amountValue,
      fee: feeValue,
      assetBalance: assetValue,
      nativeBalance: nativeValue,
      requiredAsset: amountValue,
      requiredNative: feeValue
    };
  }

  const required = amountValue + feeValue;
  const balance = transport === "evm" ? nativeValue : assetValue;
  if (balance < required) {
    throw new Error(`Insufficient transparent balance: need ${required}${assetDenom} including estimated fee, available ${balance}${assetDenom}`);
  }
  return { amount: amountValue, fee: feeValue, balance, required };
}
