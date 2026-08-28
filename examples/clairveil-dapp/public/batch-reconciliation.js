function canonicalHex(value) {
  if (typeof value === "string") {
    return value.trim().replace(/^0x/i, "").toLowerCase();
  }
  const bytes = value instanceof ArrayBuffer
    ? new Uint8Array(value)
    : new Uint8Array(value || []);
  return [...bytes].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function expectedDisclosureMode(value) {
  const modes = [
    "USER_DISCLOSURE_MODE_NONE",
    "USER_DISCLOSURE_MODE_PUBLIC",
    "USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED",
  ];
  const index = Number(value);
  if (!Number.isSafeInteger(index) || !modes[index]) {
    throw new Error("Prepared batch operation evidence has an invalid disclosure mode");
  }
  return modes[index];
}

function assertOutputMatchesExpected(output, expected) {
  const index = Number(expected?.batch_item_index ?? expected?.batchItemIndex);
  if (!Number.isSafeInteger(index) || index < 0 || Number(output?.output_index) !== index) {
    throw new Error("Typed batch output index does not match prepared operation evidence");
  }
  const comparisons = [
    ["commitment", canonicalHex(output.commitment), canonicalHex(expected.expected_output_commitment)],
    ["user disclosure digest", canonicalHex(output.user_disclosure_digest), canonicalHex(expected.expected_user_disclosure_digest)],
    ["audit disclosure digest", canonicalHex(output.full_disclosure_digest), canonicalHex(expected.expected_audit_disclosure_digest)],
  ];
  const mismatch = comparisons.find(([, actual, wanted]) => actual !== wanted);
  if (mismatch) {
    throw new Error(`Typed batch output ${index} ${mismatch[0]} does not match prepared operation evidence`);
  }
  if (Number(output.user_privacy_policy) !== Number(expected.user_privacy_policy)) {
    throw new Error(`Typed batch output ${index} privacy policy does not match prepared operation evidence`);
  }
  if (String(output.user_disclosure_mode) !== expectedDisclosureMode(expected.user_disclosure_mode)) {
    throw new Error(`Typed batch output ${index} disclosure mode does not match prepared operation evidence`);
  }
}

export function assertTypedBatchEffect({
  summary,
  outputs = [],
  operationEvidence,
  outputCount,
  txHash,
  maxOutputs = 32,
} = {}) {
  const expectedOutputs = operationEvidence?.expected_outputs ?? operationEvidence?.expectedOutputs;
  const expectedNullifiers = operationEvidence?.input_nullifier_hexes ?? operationEvidence?.inputNullifierHexes;
  const count = Number(outputCount);
  if (!operationEvidence || !Array.isArray(expectedOutputs) || !expectedOutputs.length
    || !Array.isArray(expectedNullifiers) || !expectedNullifiers.length
    || !Number.isSafeInteger(count) || count < 1 || count > maxOutputs) {
    throw new Error("Encrypted batch recovery artifact has incomplete operation evidence");
  }
  if (canonicalHex(summary?.tx_hash) !== canonicalHex(txHash)
    || Number(summary?.output_count) !== count) {
    throw new Error("Typed batch summary does not match the submitted transaction and prepared output count");
  }
  const actualNullifiers = (summary?.nullifiers || []).map(canonicalHex).sort();
  const preparedNullifiers = expectedNullifiers.map(canonicalHex).sort();
  if (actualNullifiers.length !== preparedNullifiers.length
    || actualNullifiers.some((value, index) => value !== preparedNullifiers[index])) {
    throw new Error("Typed batch summary nullifiers do not match prepared operation evidence");
  }
  if (outputs.length !== count) {
    throw new Error("Typed batch scan did not return every prepared output");
  }
  const outputsByIndex = new Map(outputs.map(output => [Number(output.output_index), output]));
  if (outputsByIndex.size !== count
    || [...outputsByIndex.keys()].some(index => index < 0 || index >= count)) {
    throw new Error("Typed batch output indexes do not form the prepared output set");
  }
  for (const expected of expectedOutputs) {
    if (String(expected?.role || "") !== "payment") {
      throw new Error("Prepared batch operation evidence contains a non-payment expected output");
    }
    const index = Number(expected.batch_item_index ?? expected.batchItemIndex);
    const output = outputsByIndex.get(index);
    if (!output) throw new Error(`Typed batch scan is missing prepared payment output ${index}`);
    assertOutputMatchesExpected(output, expected);
  }
  return true;
}

export { canonicalHex as canonicalBatchEvidenceHex };
