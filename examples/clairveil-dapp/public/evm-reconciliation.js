function normalizedHex(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
}

function canonicalBytesHex(value, label, { exactLength = 32 } = {}) {
  let hex;
  if (typeof value === "string") {
    hex = normalizedHex(value);
  } else {
    const bytes = value instanceof ArrayBuffer
      ? new Uint8Array(value)
      : ArrayBuffer.isView(value)
        ? new Uint8Array(value.buffer, value.byteOffset, value.byteLength)
        : null;
    if (!bytes) throw new Error(`${label} must be bytes or hex`);
    hex = [...bytes].map(byte => byte.toString(16).padStart(2, "0")).join("");
  }
  if (!new RegExp(`^[0-9a-f]{${exactLength * 2}}$`).test(hex)) {
    throw new Error(`${label} must be exactly ${exactLength} bytes`);
  }
  return hex;
}

function canonicalPositiveHeight(value, label) {
  const text = typeof value === "number" && Number.isSafeInteger(value)
    ? String(value)
    : String(value ?? "").trim();
  if (!/^[1-9][0-9]*$/.test(text)) throw new Error(`${label} must be a positive integer`);
  return BigInt(text).toString();
}

function verifiedTypedScanEffect(typedEffect, evmTxHash) {
  const summary = typedEffect?.summary;
  const outputs = typedEffect?.outputs;
  const link = typedEffect?.scanTransactionLink;
  if (!summary || !Array.isArray(outputs) || !link) {
    throw new Error("EVM operation requires complete typed scan transaction evidence");
  }
  const scanTxHash = canonicalBytesHex(summary.tx_hash ?? summary.txHash, "typed scan transaction hash");
  if (canonicalBytesHex(link.scanTxHash, "linked scan transaction hash") !== scanTxHash
    || canonicalBytesHex(link.evmTxHash, "linked Ethereum transaction hash")
      !== canonicalBytesHex(evmTxHash, "EVM receipt transaction hash")
    || link.cosmosTxSucceeded !== true
    || link.ethereumTxHashEventMatched !== true
    || canonicalPositiveHeight(link.cometHeight, "linked Comet height")
      !== canonicalPositiveHeight(summary.height, "typed scan height")) {
    throw new Error("Typed scan transaction is not linked to the verified EVM receipt");
  }
  const outputCount = Number(summary.output_count ?? summary.outputCount);
  if (!Number.isSafeInteger(outputCount) || outputCount < 0 || outputs.length !== outputCount) {
    throw new Error("Typed scan output set is incomplete");
  }
  return { summary, outputs, link, scanTxHash };
}

export function evmReceiptExplicitlySucceeded(receipt) {
  const status = receipt?.status;
  if (status === 1 || status === 1n || status === true) return true;
  const text = String(status ?? "").trim().toLowerCase();
  if (text === "1") return true;
  if (!/^0x[0-9a-f]+$/.test(text)) return false;
  try {
    return BigInt(text) === 1n;
  } catch {
    return false;
  }
}

export function isExplicitEvmWalletRejection(error) {
  return String(error?.code ?? error?.data?.code ?? "") === "4001";
}

export function isAmbiguousEvmSubmissionError(error, txHash = "") {
  return !String(txHash || "").trim()
    && error?.evmSubmissionAttempted === true
    && !isExplicitEvmWalletRejection(error);
}

export function verifiedEvmTransactionResult(result, label = "EVM transaction") {
  if (!result?.txHash || !result?.txBytesHash || !evmReceiptExplicitlySucceeded(result.receipt)
    || result.evmTransactionVerified !== true
    || result.evmPrivacyReceiptVerified !== true
    || result.evmFinalityVerified !== true
    || result.transactionVerification?.verified !== true
    || result.privacyReceipt?.verified !== true
    || result.finality?.verified !== true) {
    throw new Error(`${label} did not include complete transaction, receipt, privacy-event, and finality evidence`);
  }
  return {
    txHash: result.txHash,
    txBytesHash: result.txBytesHash,
    receipt: result.receipt,
    transactionVerification: result.transactionVerification,
    privacyReceipt: result.privacyReceipt,
    finality: result.finality,
    evmTransactionVerified: true,
    evmPrivacyReceiptVerified: true,
    evmFinalityVerified: true,
  };
}

export function directEvmOperationSuccessEvidence(records = [], result, typedEffect) {
  if (!records.length) throw new Error("EVM operation reservations are required");
  const txResult = verifiedEvmTransactionResult(result, "EVM privacy operation");
  const scanEffect = verifiedTypedScanEffect(typedEffect, txResult.txHash);
  const submittedHashes = [...new Set(records
    .map(record => normalizedHex(record?.submitted_tx_hash))
    .filter(Boolean))];
  if (submittedHashes.length !== 1 || submittedHashes[0] !== normalizedHex(txResult.txHash)) {
    throw new Error("EVM receipt transaction hash does not match the reserved operation");
  }
  const artifactHashes = [...new Set(records
    .map(record => String(record?.tx_bytes_hash || "").trim())
    .filter(Boolean))];
  if (artifactHashes.length !== 1 || artifactHashes[0] !== String(txResult.txBytesHash).trim()) {
    throw new Error("EVM receipt transaction binding does not match the reserved operation");
  }
  const first = records[0];
  const withdraw = records.every(record => ["withdraw", "relay_withdraw"].includes(String(record?.kind || "")));
  const expectedEventType = withdraw ? "withdraw" : "shielded_transfer";
  if (String(scanEffect.summary.event_type ?? scanEffect.summary.eventType) !== expectedEventType) {
    throw new Error("Typed scan event type does not match the reserved EVM operation");
  }
  let outputCommitment = "";
  let auditDisclosureDigest = "";
  if (withdraw) {
    if (scanEffect.outputs.length !== 0) {
      throw new Error("Typed EVM withdraw unexpectedly created a shielded output");
    }
  } else {
    const expectedCommitments = [...new Set(records
      .map(record => normalizedHex(record?.expected_output_commitment))
      .filter(Boolean))];
    if (expectedCommitments.length !== 1 || !/^[0-9a-f]{64}$/.test(expectedCommitments[0])) {
      throw new Error("EVM transfer reservation has an invalid expected output commitment");
    }
    const matches = scanEffect.outputs.filter(output => (
      canonicalBytesHex(output?.commitment, "typed scan output commitment") === expectedCommitments[0]
    ));
    if (matches.length !== 1) {
      throw new Error("Typed scan output set does not contain exactly one reserved transfer output");
    }
    outputCommitment = canonicalBytesHex(matches[0].commitment, "typed scan output commitment");
    auditDisclosureDigest = canonicalBytesHex(
      matches[0].full_disclosure_digest ?? matches[0].fullDisclosureDigest,
      "typed scan audit disclosure digest"
    );
  }
  return {
    txResult,
    outputCommitment,
    auditDisclosureDigest,
    recipientHash: first.expected_recipient_hash,
    amount: first.expected_amount,
    amountHash: first.expected_amount_hash,
    denom: first.expected_denom,
    batchItemIndex: first.batch_item_index,
    batchItemIndexKnown: first.batch_item_index_known,
    scanTransactionLink: scanEffect.link,
    typedScanEvidence: {
      scanTxHash: scanEffect.scanTxHash,
      height: canonicalPositiveHeight(scanEffect.summary.height, "typed scan height"),
      eventType: expectedEventType,
      outputCount: scanEffect.outputs.length,
      nullifiers: (scanEffect.summary.nullifiers || []).map((value, index) => (
        canonicalBytesHex(value, `typed scan nullifier ${index}`)
      ))
    }
  };
}
