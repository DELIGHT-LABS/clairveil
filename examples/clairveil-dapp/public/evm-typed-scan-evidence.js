const HASH_BYTES = 32;
const MAX_INT64 = (1n << 63n) - 1n;
const MAX_UINT32 = (1n << 32n) - 1n;

function canonicalUint(value, label, { maximum } = {}) {
  const text = typeof value === "bigint"
    ? value.toString()
    : typeof value === "number" && Number.isSafeInteger(value)
      ? String(value)
      : typeof value === "string"
        ? value.trim()
        : "";
  if (!/^(?:0|[1-9][0-9]*)$/.test(text)) {
    throw new Error(`${label} must be a non-negative integer`);
  }
  const parsed = BigInt(text);
  if (maximum != null && parsed > maximum) {
    throw new Error(`${label} exceeds its supported range`);
  }
  return parsed;
}

function wireUint(value) {
  return value <= BigInt(Number.MAX_SAFE_INTEGER) ? Number(value) : value.toString();
}

function byteView(value, label) {
  if (value instanceof ArrayBuffer) return new Uint8Array(value);
  if (ArrayBuffer.isView(value)) {
    return new Uint8Array(value.buffer, value.byteOffset, value.byteLength);
  }
  if (Array.isArray(value) && value.every(byte => (
    Number.isInteger(byte) && byte >= 0 && byte <= 255
  ))) {
    return Uint8Array.from(value);
  }
  throw new Error(`${label} must be bytes or hexadecimal`);
}

function canonicalHex(value, label, { bytes = HASH_BYTES } = {}) {
  if (typeof value === "string") {
    const text = value.trim().replace(/^0x/i, "");
    if (!/^[0-9a-f]+$/i.test(text) || text.length !== bytes * 2) {
      throw new Error(`${label} must be exactly ${bytes} bytes`);
    }
    return text.toLowerCase();
  }
  const view = byteView(value, label);
  if (view.byteLength !== bytes) {
    throw new Error(`${label} must be exactly ${bytes} bytes`);
  }
  return [...view].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function normalizedAlias(record, camel, snake, label, normalize) {
  if (!record || typeof record !== "object" || Array.isArray(record)) {
    throw new Error(`${label} record is required`);
  }
  const hasCamel = Object.prototype.hasOwnProperty.call(record, camel);
  const hasSnake = Object.prototype.hasOwnProperty.call(record, snake);
  if (!hasCamel && !hasSnake) throw new Error(`${label} is required`);
  const camelValue = hasCamel ? normalize(record[camel]) : null;
  const snakeValue = hasSnake ? normalize(record[snake]) : null;
  if (hasCamel && hasSnake && camelValue !== snakeValue) {
    throw new Error(`${label} aliases do not match`);
  }
  return hasCamel ? camelValue : snakeValue;
}

function eventIdentity(record, label, { allowZeroSequence = false } = {}) {
  const height = canonicalUint(record?.height, `${label} height`, { maximum: MAX_INT64 });
  const globalSequence = normalizedAlias(
    record,
    "globalSequence",
    "global_sequence",
    `${label} global sequence`,
    value => canonicalUint(value, `${label} global sequence`).toString(),
  );
  if (!allowZeroSequence && BigInt(globalSequence) === 0n) {
    throw new Error(`${label} global sequence must be positive`);
  }
  return { height, globalSequence: BigInt(globalSequence) };
}

function eventKey(identity) {
  return `${identity.height}:${identity.globalSequence}`;
}

function compareEvent(left, right) {
  if (left.height !== right.height) return left.height < right.height ? -1 : 1;
  if (left.globalSequence !== right.globalSequence) {
    return left.globalSequence < right.globalSequence ? -1 : 1;
  }
  return 0;
}

function compareCursor(left, right) {
  const eventComparison = compareEvent(left, right);
  if (eventComparison !== 0) return eventComparison;
  if (left.outputIndex === right.outputIndex) return 0;
  return left.outputIndex < right.outputIndex ? -1 : 1;
}

function pageCursor(value, label) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${label} is required`);
  }
  const identity = eventIdentity(value, label, { allowZeroSequence: true });
  const outputIndex = normalizedAlias(
    value,
    "outputIndex",
    "output_index",
    `${label} output index`,
    candidate => canonicalUint(candidate, `${label} output index`, {
      maximum: MAX_UINT32,
    }).toString(),
  );
  return { ...identity, outputIndex: BigInt(outputIndex) };
}

function eventType(record, label) {
  return normalizedAlias(
    record,
    "eventType",
    "event_type",
    `${label} event type`,
    value => {
      if (typeof value !== "string" || !value.trim()) {
        throw new Error(`${label} event type must be a non-empty string`);
      }
      return value.trim();
    },
  );
}

function transactionHash(record, label) {
  return normalizedAlias(
    record,
    "txHash",
    "tx_hash",
    `${label} transaction hash`,
    value => canonicalHex(value, `${label} transaction hash`),
  );
}

function outputCount(record, label, maximum) {
  const count = normalizedAlias(
    record,
    "outputCount",
    "output_count",
    `${label} output count`,
    value => canonicalUint(value, `${label} output count`, {
      maximum: BigInt(maximum),
    }).toString(),
  );
  return Number(count);
}

function outputIndex(record, label) {
  const index = normalizedAlias(
    record,
    "outputIndex",
    "output_index",
    `${label} output index`,
    value => canonicalUint(value, `${label} output index`, {
      maximum: MAX_UINT32,
    }).toString(),
  );
  return Number(index);
}

function stableFingerprint(value, seen = new Set()) {
  if (value === undefined) return "undefined";
  if (value === null || typeof value === "boolean" || typeof value === "string") {
    return JSON.stringify(value);
  }
  if (typeof value === "bigint") return `bigint:${value}`;
  if (typeof value === "number") {
    if (!Number.isFinite(value)) throw new Error("typed scan evidence contains a non-finite number");
    return `number:${value}`;
  }
  if (value instanceof ArrayBuffer || ArrayBuffer.isView(value)) {
    const bytes = byteView(value, "typed scan bytes");
    const hex = [...bytes].map(byte => byte.toString(16).padStart(2, "0")).join("");
    return `bytes:${hex}`;
  }
  if (typeof value !== "object") {
    throw new Error("typed scan evidence contains an unsupported value");
  }
  if (seen.has(value)) throw new Error("typed scan evidence contains a cycle");
  seen.add(value);
  const result = Array.isArray(value)
    ? `[${value.map(item => stableFingerprint(item, seen)).join(",")}]`
    : `{${Object.keys(value).sort().map(key => (
      `${JSON.stringify(key)}:${stableFingerprint(value[key], seen)}`
    )).join(",")}}`;
  seen.delete(value);
  return result;
}

function normalizedSummary(record, maximumOutputs) {
  const identity = eventIdentity(record, "typed scan summary");
  const nullifiers = Array.isArray(record.nullifiers)
    ? record.nullifiers.map((value, index) => canonicalHex(
      value,
      `typed scan summary nullifier ${index}`,
    ))
    : (() => { throw new Error("typed scan summary nullifiers must be an array"); })();
  if (new Set(nullifiers).size !== nullifiers.length) {
    throw new Error("typed scan summary nullifiers must be distinct");
  }
  return {
    record,
    identity,
    key: eventKey(identity),
    eventType: eventType(record, "typed scan summary"),
    txHash: transactionHash(record, "typed scan summary"),
    outputCount: outputCount(record, "typed scan summary", maximumOutputs),
    nullifiers,
    fingerprint: stableFingerprint(record),
  };
}

function normalizedOutput(record) {
  const identity = eventIdentity(record, "typed scan output");
  const index = outputIndex(record, "typed scan output");
  return {
    record,
    identity,
    key: eventKey(identity),
    index,
    eventType: eventType(record, "typed scan output"),
    txHash: transactionHash(record, "typed scan output"),
    commitment: canonicalHex(record?.commitment, "typed scan output commitment"),
    fingerprint: stableFingerprint(record),
  };
}

function booleanAlias(record, camel, snake, label) {
  return normalizedAlias(record, camel, snake, label, value => {
    if (typeof value !== "boolean") throw new Error(`${label} must be boolean`);
    return value;
  });
}

function positiveLimit(value, label) {
  if (!Number.isSafeInteger(value) || value < 1) {
    throw new Error(`${label} must be a positive safe integer`);
  }
  return value;
}

function expectedHexSet(values, label) {
  if (!Array.isArray(values)) throw new Error(`${label} must be an array`);
  const normalized = values.map((value, index) => canonicalHex(value, `${label} ${index}`));
  if (new Set(normalized).size !== normalized.length) {
    throw new Error(`${label} must not contain duplicates`);
  }
  return new Set(normalized);
}

function expectedTypeSet(values) {
  if (!Array.isArray(values) || !values.length) {
    throw new Error("expected event types must be a non-empty array");
  }
  const normalized = values.map(value => {
    if (typeof value !== "string" || !value.trim()) {
      throw new Error("expected event types must contain non-empty strings");
    }
    return value.trim();
  });
  if (new Set(normalized).size !== normalized.length) {
    throw new Error("expected event types must not contain duplicates");
  }
  return new Set(normalized);
}

function requestCursor(cursor) {
  return {
    height: wireUint(cursor.height),
    globalSequence: wireUint(cursor.globalSequence),
    outputIndex: wireUint(cursor.outputIndex),
  };
}

function validatePageBounds(page, { outputLimit, eventLimit, maxEncodedBytes }) {
  if (page.outputs.length > outputLimit) {
    throw new Error("typed scan page exceeds the requested output limit");
  }
  if (page.summaries.length > eventLimit) {
    throw new Error("typed scan page exceeds the requested event limit");
  }
  const scanned = page.scannedEventCount ?? page.scanned_event_count;
  if (scanned != null && canonicalUint(scanned, "typed scan scanned event count") > BigInt(eventLimit)) {
    throw new Error("typed scan page exceeds the requested event limit");
  }
  const encoded = page.encodedBytes ?? page.encoded_bytes;
  if (encoded != null && canonicalUint(encoded, "typed scan encoded byte count") > BigInt(maxEncodedBytes)) {
    throw new Error("typed scan page exceeds the requested encoded byte limit");
  }
}

function summaryMatchesExpected(summary, outputs, expected) {
  if (!expected.eventTypes.has(summary.eventType)) return false;
  const actualNullifiers = new Set(summary.nullifiers);
  if ([...expected.nullifiers].some(value => !actualNullifiers.has(value))) return false;
  const actualCommitments = new Set(outputs.map(output => output.commitment));
  return [...expected.commitments].every(value => actualCommitments.has(value));
}

export async function findVerifiedEvmTypedScanEffect({
  fetchScanPage,
  verifyTransactionLink,
  evmTxHash,
  expectedEventTypes,
  expectedNullifiers = [],
  expectedCommitments = [],
  afterHeight = 0,
  throughHeight,
  maxPages = 1000,
  outputLimit = 128,
  eventLimit = 64,
  maxEncodedBytes = 1048576,
  maxOutputsPerEffect = 32,
  validationState,
} = {}) {
  if (typeof fetchScanPage !== "function") throw new Error("fetchScanPage is required");
  if (typeof verifyTransactionLink !== "function") {
    throw new Error("verifyTransactionLink is required");
  }
  const canonicalEvmTxHash = canonicalHex(evmTxHash, "expected Ethereum transaction hash");
  const expected = {
    eventTypes: expectedTypeSet(expectedEventTypes),
    nullifiers: expectedHexSet(expectedNullifiers, "expected nullifiers"),
    commitments: expectedHexSet(expectedCommitments, "expected commitments"),
  };
  if (!expected.nullifiers.size && !expected.commitments.size) {
    throw new Error("at least one expected nullifier or commitment is required");
  }
  const limits = {
    maxPages: positiveLimit(maxPages, "maximum scan page count"),
    outputLimit: positiveLimit(outputLimit, "scan output limit"),
    eventLimit: positiveLimit(eventLimit, "scan event limit"),
    maxEncodedBytes: positiveLimit(maxEncodedBytes, "scan encoded byte limit"),
    maxOutputsPerEffect: positiveLimit(maxOutputsPerEffect, "maximum outputs per effect"),
  };
  const startHeight = canonicalUint(afterHeight, "scan start height", { maximum: MAX_INT64 });
  const endHeight = throughHeight == null
    ? null
    : canonicalUint(throughHeight, "scan end height", { maximum: MAX_INT64 });
  if (endHeight != null && endHeight < startHeight) {
    throw new Error("scan end height must not precede the scan start height");
  }
  let cursor = {
    height: startHeight,
    globalSequence: 0n,
    outputIndex: 0n,
  };
  const summaries = new Map();
  const outputs = new Map();
  let exhausted = false;

  for (let pageIndex = 0; pageIndex < limits.maxPages; pageIndex += 1) {
    const request = {
      after: requestCursor(cursor),
      eventTypes: [],
      outputLimit: limits.outputLimit,
      eventLimit: limits.eventLimit,
      maxEncodedBytes: limits.maxEncodedBytes,
    };
    if (validationState !== undefined) request.validationState = validationState;
    const page = await fetchScanPage(request);
    if (!page || typeof page !== "object" || Array.isArray(page)) {
      throw new Error("typed scan page is required");
    }
    if (!Array.isArray(page.summaries) || !Array.isArray(page.outputs)) {
      throw new Error("typed scan page summaries and outputs must be arrays");
    }
    validatePageBounds(page, limits);
    const hasMore = booleanAlias(page, "hasMore", "has_more", "typed scan has_more");
    const rawNextCursor = normalizedAlias(
      page,
      "nextCursor",
      "next_cursor",
      "typed scan next cursor",
      value => stableFingerprint(value),
    );
    const nextCursorRecord = Object.prototype.hasOwnProperty.call(page, "nextCursor")
      ? page.nextCursor
      : page.next_cursor;
    if (!rawNextCursor) throw new Error("typed scan next cursor is required");
    const nextCursor = pageCursor(nextCursorRecord, "typed scan next cursor");
    const progress = compareCursor(nextCursor, cursor);
    if (progress < 0 || (hasMore && progress === 0)) {
      throw new Error(hasMore
        ? "typed scan has_more page did not advance its cursor"
        : "typed scan next cursor regressed");
    }

    let previousSummary = null;
    for (const record of page.summaries) {
      const summary = normalizedSummary(record, limits.maxOutputsPerEffect);
      if (previousSummary && compareEvent(previousSummary.identity, summary.identity) >= 0) {
        throw new Error("typed scan page summaries are not strictly ordered");
      }
      previousSummary = summary;
      if (compareEvent(summary.identity, cursor) < 0
        || compareEvent(summary.identity, nextCursor) > 0) {
        throw new Error("typed scan summary is outside the page cursor range");
      }
      if (endHeight == null || summary.identity.height <= endHeight) {
        const existing = summaries.get(summary.key);
        if (existing && existing.fingerprint !== summary.fingerprint) {
          throw new Error("typed scan returned conflicting summary evidence");
        }
        if (!existing) summaries.set(summary.key, summary);
      }
    }

    let previousOutput = null;
    for (const record of page.outputs) {
      const output = normalizedOutput(record);
      const outputCursor = { ...output.identity, outputIndex: BigInt(output.index) };
      const storageKey = `${output.key}:${output.index}`;
      const insideRequestedRange = endHeight == null || output.identity.height <= endHeight;
      if (insideRequestedRange) {
        const existing = outputs.get(storageKey);
        if (existing && existing.fingerprint !== output.fingerprint) {
          throw new Error("typed scan returned conflicting output evidence");
        }
        if (existing) throw new Error("typed scan page repeated output evidence");
      }
      if (previousOutput && compareCursor(previousOutput, outputCursor) >= 0) {
        throw new Error("typed scan page outputs are not strictly ordered");
      }
      previousOutput = outputCursor;
      if (compareCursor(outputCursor, cursor) <= 0
        || compareCursor(outputCursor, nextCursor) > 0) {
        throw new Error("typed scan output is outside the page cursor range");
      }
      if (insideRequestedRange) {
        outputs.set(storageKey, output);
      }
    }
    if (progress === 0 && (page.summaries.length || page.outputs.length)) {
      throw new Error("typed scan page returned evidence without advancing its cursor");
    }
    cursor = nextCursor;
    if (!hasMore || (endHeight != null && cursor.height > endHeight)) {
      exhausted = true;
      break;
    }
  }
  if (!exhausted) throw new Error("typed scan exceeded the maximum page count");

  const outputsByEvent = new Map();
  for (const output of outputs.values()) {
    const summary = summaries.get(output.key);
    if (!summary) throw new Error("typed scan output has no matching summary");
    if (output.eventType !== summary.eventType || output.txHash !== summary.txHash) {
      throw new Error("typed scan output does not match its summary identity");
    }
    if (output.index >= summary.outputCount) {
      throw new Error("typed scan output index exceeds its summary output count");
    }
    const eventOutputs = outputsByEvent.get(output.key) || [];
    eventOutputs.push(output);
    outputsByEvent.set(output.key, eventOutputs);
  }

  const matches = [];
  for (const summary of summaries.values()) {
    const eventOutputs = (outputsByEvent.get(summary.key) || [])
      .sort((left, right) => left.index - right.index);
    if (eventOutputs.length !== summary.outputCount
      || eventOutputs.some((output, index) => output.index !== index)) {
      throw new Error("typed scan did not return the complete output set for a summary");
    }
    const commitments = eventOutputs.map(output => output.commitment);
    if (new Set(commitments).size !== commitments.length) {
      throw new Error("typed scan output commitments must be distinct");
    }
    if (summaryMatchesExpected(summary, eventOutputs, expected)) {
      matches.push({ summary, outputs: eventOutputs });
    }
  }
  if (!matches.length) return null;
  if (matches.length !== 1) {
    throw new Error("typed scan matched multiple effects; reconciliation is ambiguous");
  }

  const matched = matches[0];
  const scanTransactionLink = await verifyTransactionLink(
    `0x${matched.summary.txHash}`,
    `0x${canonicalEvmTxHash}`,
  );
  if (!scanTransactionLink || typeof scanTransactionLink !== "object"
    || Array.isArray(scanTransactionLink)) {
    throw new Error("transaction link verification did not return evidence");
  }
  return Object.freeze({
    summary: matched.summary.record,
    outputs: Object.freeze(matched.outputs.map(output => output.record)),
    scanTransactionLink,
  });
}
