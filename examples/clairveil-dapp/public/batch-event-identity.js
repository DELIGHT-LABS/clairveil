function canonicalNonNegativeInteger(value, label, minimum = 0n) {
  const text = String(value ?? "").trim();
  if (!/^(?:0|[1-9][0-9]*)$/.test(text)) {
    throw new Error(`typed batch event has an invalid ${label}`);
  }
  const parsed = BigInt(text);
  if (parsed < minimum) {
    throw new Error(`typed batch event has an invalid ${label}`);
  }
  return parsed.toString();
}

// Privacy events expose `sequence`, while typed PrivacyScanV2 records expose
// the same immutable identity as `global_sequence`. A transaction hash is not
// sufficient because one Cosmos transaction can contain multiple batch
// messages, each with output indexes starting at zero.
export function typedBatchEventIdentity(record = {}) {
  return {
    height: canonicalNonNegativeInteger(record.height, "height"),
    globalSequence: canonicalNonNegativeInteger(
      record.global_sequence ?? record.globalSequence ?? record.sequence,
      "global sequence",
      1n,
    ),
  };
}

export function sameTypedBatchEventIdentity(left, right) {
  return (
    left?.height === right?.height &&
    left?.globalSequence === right?.globalSequence
  );
}

// Cosmos allows one transaction to carry multiple privacy messages. Keep the
// transaction hash in the UI selection key as a consistency check, but use
// the typed event coordinates to distinguish those messages.
export function privacyEventSelectionKey(record = {}) {
  const txHash = String(record.tx_hash_hex ?? record.txHashHex ?? "")
    .trim()
    .toUpperCase();
  if (!/^[0-9A-F]{64}$/.test(txHash)) {
    throw new Error("privacy event has an invalid transaction hash");
  }
  const identity = typedBatchEventIdentity(record);
  return `${txHash}:${identity.height}:${identity.globalSequence}`;
}
