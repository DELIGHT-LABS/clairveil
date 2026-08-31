function eventAttribute(event, key) {
  const attributes = Array.isArray(event?.attributes) ? event.attributes : [];
  return attributes.find(attribute => attribute?.key === key)?.value ?? null;
}

function parseCoinList(value) {
  if (String(value ?? "").trim() === "") return [];
  const coins = [];
  for (const entry of String(value || "").split(",")) {
    const match = /^(0|[1-9][0-9]*)([A-Za-z][A-Za-z0-9/:._-]*)$/.exec(entry.trim());
    if (!match) return null;
    coins.push({ amount: BigInt(match[1]), denom: match[2] });
  }
  return coins;
}

/**
 * Return the fee actually charged by a Cosmos transaction in one denom.
 *
 * AuthInfo contains the wallet-declared fee, which is not proof that the
 * chain deducted it. Standard Cosmos fee deduction emits an ante-handler
 * tx.fee event and bank coin_spent evidence without msg_index; message-level
 * spends have msg_index.
 */
export function cosmosChargedFeeAmount(tx, denom) {
  const events = tx?.events ?? tx?.tx_response?.events;
  if (!Array.isArray(events)) return null;

  const explicitFeeEvents = events.filter(event => (
    event?.type === "tx" && eventAttribute(event, "fee") !== null
  ));
  if (explicitFeeEvents.length) {
    let explicitTotal = 0n;
    for (const event of explicitFeeEvents) {
      const coins = parseCoinList(eventAttribute(event, "fee"));
      if (!coins) return null;
      for (const coin of coins) {
        if (coin.denom === denom) explicitTotal += coin.amount;
      }
    }
    return explicitTotal;
  }

  let total = 0n;
  for (const event of events) {
    if (event?.type !== "coin_spent" || eventAttribute(event, "msg_index") !== null) continue;
    const coins = parseCoinList(eventAttribute(event, "amount"));
    if (!coins) return null;
    for (const coin of coins) {
      if (coin.denom === denom) total += coin.amount;
    }
  }
  return total;
}

function evmQuantity(value) {
  if (typeof value === "bigint") return value >= 0n ? value : null;
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? BigInt(value) : null;
  }
  const text = String(value ?? "").trim();
  if (!/^(?:0x[0-9a-fA-F]+|0|[1-9][0-9]*)$/.test(text)) return null;
  return BigInt(text);
}

export function evmChargedFeeAmount(receipt) {
  const gasUsed = evmQuantity(receipt?.gasUsed ?? receipt?.gas_used);
  const gasPrice = evmQuantity(
    receipt?.effectiveGasPrice
      ?? receipt?.effective_gas_price
      ?? receipt?.gasPrice
      ?? receipt?.gas_price
  );
  return gasUsed === null || gasPrice === null ? null : gasUsed * gasPrice;
}
