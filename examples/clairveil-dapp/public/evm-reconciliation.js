function normalizedHex(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
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

export function directEvmOperationSuccessEvidence(records = [], result) {
  if (!records.length) throw new Error("EVM operation reservations are required");
  const txResult = verifiedEvmTransactionResult(result, "EVM privacy operation");
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
  return {
    txResult,
    outputCommitment: first.expected_output_commitment,
    auditDisclosureDigest: first.expected_disclosure_digest,
    recipientHash: first.expected_recipient_hash,
    amount: first.expected_amount,
    amountHash: first.expected_amount_hash,
    denom: first.expected_denom,
    batchItemIndex: first.batch_item_index,
    batchItemIndexKnown: first.batch_item_index_known,
  };
}
