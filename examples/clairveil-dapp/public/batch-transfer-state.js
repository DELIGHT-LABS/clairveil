function positiveInteger(value, label) {
  const normalized = typeof value === "bigint" ? value : BigInt(String(value));
  if (normalized < 0n) {
    throw new Error(`${label} must not be negative`);
  }
  return normalized;
}

function boundedCount(value, label, maximum) {
  const normalized = Number(value);
  if (
    !Number.isSafeInteger(normalized) ||
    normalized < 1 ||
    normalized > maximum
  ) {
    throw new Error(`${label} must be between 1 and ${maximum}`);
  }
  return normalized;
}

function canonicalHex(value) {
  return String(value || "")
    .trim()
    .toLowerCase()
    .replace(/^0x/, "");
}

function base64Hex(value, label) {
  const encoded = String(value || "").trim();
  if (!encoded) return "";
  let decoded;
  try {
    decoded = atob(encoded);
  } catch {
    throw new Error(`${label} is not valid base64`);
  }
  return Array.from(decoded, (character) =>
    character.charCodeAt(0).toString(16).padStart(2, "0"),
  ).join("");
}

function preparedBatchPaymentOutput(prepared, itemId) {
  const operationEvidence =
    prepared?.operationEvidence ?? prepared?.operation_evidence;
  const expectedOutputs =
    operationEvidence?.expected_outputs ?? operationEvidence?.expectedOutputs;
  if (!Array.isArray(expectedOutputs)) {
    throw new Error("Prepared batch operation evidence is unavailable");
  }
  const matches = expectedOutputs.filter(
    (output) =>
      String(output?.item_id ?? output?.itemId ?? "") === String(itemId),
  );
  if (matches.length !== 1 || String(matches[0]?.role || "") !== "payment") {
    throw new Error(
      `Prepared batch payment ${itemId || "item"} output evidence is invalid`,
    );
  }
  const outputIndex = Number(
    matches[0]?.batch_item_index ?? matches[0]?.batchItemIndex,
  );
  if (!Number.isSafeInteger(outputIndex) || outputIndex < 0) {
    throw new Error(
      `Prepared batch payment ${itemId || "item"} output index is invalid`,
    );
  }
  const payload = prepared?.payload;
  const output = payload?.outputs?.[outputIndex];
  const messageOutput = payload?.message_outputs?.[outputIndex];
  if (!output || !messageOutput) {
    throw new Error(
      `Prepared batch payment ${itemId || "item"} output payload is unavailable`,
    );
  }
  const privateTarget = base64Hex(
    output.disclosure_target_pubkey ?? output.disclosureTargetPubKey,
    `Prepared batch payment ${itemId || "item"} disclosure target`,
  );
  const messageTarget = base64Hex(
    messageOutput.user_disclosure_target_pubkey ??
      messageOutput.userDisclosureTargetPubkey,
    `Prepared batch payment ${itemId || "item"} message disclosure target`,
  );
  if (privateTarget !== messageTarget) {
    throw new Error(
      `Prepared batch payment ${itemId || "item"} disclosure targets conflict`,
    );
  }
  return { evidence: matches[0], output, messageOutput, targetHex: privateTarget };
}

export function batchTransferReservationsSucceeded(
  reservations = [],
  {
    expectedCount = reservations.length,
    expectedOperationEvidenceHash = "",
  } = {},
) {
  const evidenceHash = canonicalHex(expectedOperationEvidenceHash);
  if (
    !evidenceHash ||
    reservations.length !== expectedCount ||
    expectedCount < 1
  ) {
    return false;
  }
  return reservations.every((reservation) => {
    const metadata = reservation?.metadata || {};
    const evidenceRequired =
      metadata.operation_success_evidence_required ??
      metadata.operationSuccessEvidenceRequired ??
      reservation?.operation_success_evidence_required ??
      reservation?.operationSuccessEvidenceRequired;
    const storedEvidenceHash = canonicalHex(
      reservation?.expected_operation_evidence_hash ??
        reservation?.expectedOperationEvidenceHash,
    );
    return (
      String(reservation?.status || "") === "ConfirmedSpent" &&
      evidenceRequired === true &&
      String(
        metadata.operation_status ?? metadata.operationStatus ?? "",
      ) === "Succeeded" &&
      (metadata.operation_success_evidence_matches ??
        metadata.operationSuccessEvidenceMatches) === true &&
      storedEvidenceHash === evidenceHash
    );
  });
}

export function computeBatchTransferPreviewState({
  paymentAmounts = [],
  noteAmounts = [],
  maxInputs = 16,
  maxOutputs = 32,
} = {}) {
  const payments = paymentAmounts.map((amount, index) =>
    positiveInteger(amount, `payment ${index} amount`),
  );
  const notes = noteAmounts.map((amount, index) =>
    positiveInteger(amount, `note ${index} amount`),
  );
  const total = payments.reduce((sum, amount) => sum + amount, 0n);
  const totalAvailable = notes.reduce((sum, amount) => sum + amount, 0n);
  const oneBatchCapacity = notes
    .slice(0, maxInputs)
    .reduce((sum, amount) => sum + amount, 0n);
  const selected = [];
  let selectedTotal = 0n;
  for (const amount of notes) {
    if (selected.length >= maxInputs || selectedTotal >= total) break;
    selected.push(amount);
    selectedTotal += amount;
  }
  const totalCovered = total > 0n && total <= totalAvailable;
  const inputsExceeded = total > oneBatchCapacity;
  const estimatedChange =
    !inputsExceeded && selectedTotal >= total ? selectedTotal - total : null;
  const outputCount =
    payments.length +
    (estimatedChange === null || estimatedChange > 0n ? 1 : 0);
  const outputsExceeded = outputCount > maxOutputs;
  const requiresSplit =
    totalCovered && (inputsExceeded || outputsExceeded);
  // A zero capacity means there are no currently selectable notes (or every
  // note is reserved). It is an inventory/reconciliation problem, not proof
  // that every payment independently exceeds the 16-input limit. Keep the
  // latter warning for a funded draft only, where one payment genuinely cannot
  // fit into any single atomic batch.
  const unsplittablePaymentIndexes =
    totalCovered && oneBatchCapacity > 0n
      ? payments
          .map((amount, index) => (amount > oneBatchCapacity ? index : -1))
          .filter((index) => index >= 0)
      : [];

  return {
    total,
    totalAvailable,
    oneBatchCapacity,
    selectedCount: selected.length,
    selectedTotal,
    estimatedChange,
    outputCount,
    totalCovered,
    inputsExceeded,
    outputsExceeded,
    requiresSplit,
    unsplittablePaymentIndexes,
  };
}

export function batchTransferNeedsReconciliation({
  explicit = false,
  noBroadcastAttempt = false,
  hasBroadcastEvidence = false,
  reservations = [],
} = {}) {
  if (explicit) return true;
  const durableAmbiguity = reservations.some((reservation) => {
    const status = String(reservation?.status || "");
    return (
      ["Submitted", "Unknown", "ManualReview"].includes(status) ||
      reservation?.broadcast_in_flight === true ||
      reservation?.broadcastInFlight === true
    );
  });
  if (hasBroadcastEvidence || durableAmbiguity) return true;
  return false;
}

export function batchTransferErrorRequiresReconciliation(error) {
  return Boolean(
    error?.reservationReconciliationRequired ||
      error?.batchTransferReconciliationRequired,
  );
}

export function pendingBatchTransferPayments(
  payments = [],
  completedItemIDs = [],
) {
  const completed = new Set([...completedItemIDs].map(String));
  return payments.filter((payment) => {
    const itemId = String(payment?.itemId ?? payment?.item_id ?? "");
    return !completed.has(itemId);
  });
}

// A compact batch has one output per payment and only needs one additional
// output when the selected inputs leave change.  In particular, an exact
// 32-payment match is valid without a change output; do not reserve an output
// slot for change before knowing whether the selected payment reaches input
// capacity exactly.
export function selectNextAtomicBatchPayments(
  payments = [],
  {
    inputCapacity,
    maxOutputs = 32,
  } = {},
) {
  const capacity = positiveInteger(inputCapacity, "atomic batch input capacity");
  const outputLimit = boundedCount(
    maxOutputs,
    "atomic batch output capacity",
    32,
  );
  if (capacity <= 0n) {
    throw new Error(
      "No currently spendable input capacity remains for the next atomic batch",
    );
  }

  const group = [];
  let total = 0n;
  for (const payment of payments) {
    const itemId = String(payment?.itemId ?? payment?.item_id ?? "item");
    const amount = positiveInteger(
      payment?.amountValue,
      `payment ${itemId} amount`,
    );
    if (amount <= 0n) {
      throw new Error(`Payment ${itemId} amount must be positive`);
    }
    if (amount > capacity && !group.length) {
      throw new Error(
        `Payment ${itemId} exceeds the current 16-input capacity and cannot be split without changing that payment's atomic semantics`,
      );
    }

    const nextTotal = total + amount;
    if (nextTotal > capacity) break;
    const nextOutputCount = group.length + 1 + (nextTotal < capacity ? 1 : 0);
    if (nextOutputCount > outputLimit) break;

    group.push(payment);
    total = nextTotal;
  }
  if (!group.length) {
    throw new Error("The next atomic batch cannot be planned from current notes");
  }
  return group;
}

export function preparedBatchTransferFacts({
  requestedPayments = [],
  prepared = {},
  denom,
  maxInputs = 16,
  maxOutputs = 32,
} = {}) {
  const normalizedDenom = String(denom || "");
  if (!normalizedDenom) throw new Error("Prepared batch denom is unavailable");
  if (
    !Array.isArray(requestedPayments) ||
    !requestedPayments.length ||
    !Array.isArray(prepared.payments) ||
    prepared.payments.length !== requestedPayments.length
  ) {
    throw new Error("Prepared batch payments do not match the requested rows");
  }

  const requestedByID = new Map();
  let total = 0n;
  for (const payment of requestedPayments) {
    const itemId = String(payment?.itemId || "");
    const amount = positiveInteger(
      payment?.amountValue,
      `payment ${itemId || "item"} amount`,
    );
    if (!itemId || amount <= 0n || requestedByID.has(itemId)) {
      throw new Error("Requested batch payment identity is invalid");
    }
    requestedByID.set(itemId, { ...payment, amount });
    total += amount;
  }

  for (const payment of prepared.payments) {
    const itemId = String(payment?.itemId ?? payment?.item_id ?? "");
    const requested = requestedByID.get(itemId);
    const preparedOutput = requested
      ? preparedBatchPaymentOutput(prepared, itemId)
      : null;
    const requestedDisclosureTarget = canonicalHex(
      requested?.userDisclosureTargetPubKeyHex ??
        requested?.user_disclosure_target_pubkey_hex,
    );
    const preparedDisclosureTarget = canonicalHex(
      payment?.userDisclosureTargetPubKeyHex ??
        payment?.user_disclosure_target_pubkey_hex,
    );
    if (
      !requested ||
      String(payment?.amount || "") !==
        `${requested.amount}${normalizedDenom}` ||
      String(payment?.recipient || "") !== String(requested.recipient || "") ||
      String(payment?.privacyPolicy || payment?.privacy_policy || "") !==
        String(requested.userPrivacyPolicy || "") ||
      String(payment?.disclosureMode || payment?.disclosure_mode || "") !==
        String(requested.userDisclosureMode || "") ||
      (preparedDisclosureTarget &&
        preparedDisclosureTarget !== requestedDisclosureTarget) ||
      preparedOutput.targetHex !== requestedDisclosureTarget
    ) {
      throw new Error(
        `Prepared batch payment ${itemId || "item"} does not match its requested effect`,
      );
    }
    requestedByID.delete(itemId);
  }
  if (requestedByID.size) {
    throw new Error("Prepared batch omitted a requested payment");
  }

  const inputCount = boundedCount(
    prepared.inputCount,
    "Prepared batch input count",
    maxInputs,
  );
  const outputCount = boundedCount(
    prepared.outputCount,
    "Prepared batch output count",
    maxOutputs,
  );
  const selectedInputTotal = positiveInteger(
    prepared.selectedInputTotal,
    "Prepared batch selected input total",
  );
  if (selectedInputTotal < total) {
    throw new Error("Prepared batch inputs do not cover the requested total");
  }
  const change = selectedInputTotal - total;
  const expectedOutputCount =
    requestedPayments.length + (change > 0n ? 1 : 0);
  if (outputCount !== expectedOutputCount) {
    throw new Error(
      "Prepared compact batch output count does not match payments and change",
    );
  }

  const disclosureCounts = {
    private: 0,
    public: 0,
    recipientEncrypted: 0,
  };
  for (const payment of requestedPayments) {
    if (payment.userDisclosureMode === "public") {
      disclosureCounts.public += 1;
    } else if (payment.userDisclosureMode === "recipient-encrypted") {
      disclosureCounts.recipientEncrypted += 1;
    } else {
      disclosureCounts.private += 1;
    }
  }

  return {
    total,
    change,
    inputCount,
    outputCount,
    paymentCount: requestedPayments.length,
    disclosureCounts,
  };
}
