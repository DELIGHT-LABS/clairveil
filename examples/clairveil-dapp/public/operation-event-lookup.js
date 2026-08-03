function normalizedTxHash(value) {
  return String(value || "").trim().replace(/^0x/i, "").toUpperCase();
}

function eventHeight(event) {
  const value = Number(event?.height || 0);
  return Number.isSafeInteger(value) && value > 0 ? value : 0;
}

export async function findPrivacyEventByTxHash({
  fetchPage,
  txHash,
  height = 0,
  eventTypes = ["shielded_transfer"],
  predicate = () => true,
  limit = 200,
  maxPages = 100
} = {}) {
  if (typeof fetchPage !== "function") throw new Error("privacy event page fetcher is required");
  const targetTxHash = normalizedTxHash(txHash);
  if (!targetTxHash) throw new Error("operation tx hash is required");
  const targetHeight = Number(height || 0);
  if (!Number.isSafeInteger(targetHeight) || targetHeight < 0) throw new Error("operation height is invalid");

  let page = 1;
  for (let pagesScanned = 0; pagesScanned < maxPages; pagesScanned += 1) {
    const data = await fetchPage({
      afterHeight: targetHeight > 0 ? targetHeight - 1 : 0,
      page,
      limit,
      eventTypes
    });
    const events = data?.events || [];
    const match = events.find(event => (
      normalizedTxHash(event?.tx_hash_hex) === targetTxHash && predicate(event)
    ));
    if (match) return match;
    if (!data?.has_more) return null;
    if (targetHeight > 0 && events.some(event => eventHeight(event) > targetHeight)) return null;
    page = Number(data.page || page) + 1;
  }
  throw new Error(`privacy event lookup exceeded ${maxPages} pages`);
}

export { normalizedTxHash };
