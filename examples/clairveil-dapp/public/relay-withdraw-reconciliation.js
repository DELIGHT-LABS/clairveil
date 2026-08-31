import { MsgWithdraw, msgWithdrawTypeUrl } from "clairveiljs/cosmos-client";
import { computePreparedWithdrawPayloadHash } from "clairveiljs/payload";

export const relayWithdrawHandoffVersion = "v2";

export function createRelayWithdrawHandoff({ profileId, transport, payload, transaction } = {}) {
  if (!payload || typeof payload !== "object") {
    throw new Error("relay withdraw payload is required");
  }
  if (payload.version !== relayWithdrawHandoffVersion) {
    throw new Error("relay withdraw payload must use v2");
  }
  if (!["cosmos", "evm"].includes(transport)) {
    throw new Error(`unsupported relay transaction transport ${JSON.stringify(transport)}`);
  }
  return {
    schema_version: relayWithdrawHandoffVersion,
    handoff_version: relayWithdrawHandoffVersion,
    profile_id: String(profileId || ""),
    transport,
    request: {
      version: relayWithdrawHandoffVersion,
      payload
    },
    ...(transaction ? { transaction } : {})
  };
}

export function relayWithdrawHandoffPayload(handoff) {
  if (!handoff) return null;
  if (handoff.schema_version !== relayWithdrawHandoffVersion ||
      handoff.handoff_version !== relayWithdrawHandoffVersion ||
      handoff.request?.version !== relayWithdrawHandoffVersion ||
      handoff.request?.payload?.version !== relayWithdrawHandoffVersion) {
    throw new Error("relay withdraw handoff must use the v2 schema, handoff, request, and payload versions");
  }
  return handoff.request.payload;
}

function requiredText(value, label) {
  const text = String(value ?? "").trim();
  if (!text) throw new Error(`${label} is required`);
  return text;
}

function normalizedHex(value, label) {
  const text = requiredText(value, label).replace(/^0x/i, "").toLowerCase();
  if (!/^[0-9a-f]+$/.test(text) || text.length % 2 !== 0) {
    throw new Error(`${label} must be even-length hex`);
  }
  return text;
}

function bytesHex(value) {
  return [...(value || [])].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function normalizedEvmQuantity(value, label) {
  const text = requiredText(value, label);
  try {
    const quantity = BigInt(text);
    if (quantity < 0n) throw new Error("negative");
    return quantity;
  } catch {
    throw new Error(`${label} must be a non-negative EVM quantity`);
  }
}

function assertEqual(actual, expected, label, { caseInsensitive = false } = {}) {
  const left = String(actual ?? "").trim();
  const right = String(expected ?? "").trim();
  const matches = caseInsensitive
    ? left.toLowerCase() === right.toLowerCase()
    : left === right;
  if (!matches) throw new Error(`relayer transaction ${label} does not match the prepared withdraw payload`);
}

function assertEvmRelayTransactionMatches({ transaction, handoffTransaction, expectedEvmChainId }) {
  if (!transaction || typeof transaction !== "object") {
    throw new Error("included EVM relayer transaction could not be loaded");
  }
  if (!handoffTransaction || typeof handoffTransaction !== "object") {
    throw new Error("EVM relay handoff omitted its prepared transaction binding");
  }

  assertEqual(transaction.to, handoffTransaction.to, "target", { caseInsensitive: true });
  assertEqual(
    normalizedHex(transaction.input ?? transaction.data, "included EVM calldata"),
    normalizedHex(handoffTransaction.data ?? handoffTransaction.input, "prepared EVM calldata"),
    "calldata"
  );
  if (normalizedEvmQuantity(transaction.value ?? "0x0", "included EVM value") !==
      normalizedEvmQuantity(handoffTransaction.value ?? "0x0", "prepared EVM value")) {
    throw new Error("relayer transaction value does not match the prepared withdraw payload");
  }

  const preparedChainId = handoffTransaction.chainId ?? expectedEvmChainId;
  if (normalizedEvmQuantity(transaction.chainId, "included EVM chainId") !==
      normalizedEvmQuantity(preparedChainId, "prepared EVM chainId")) {
    throw new Error("relayer transaction chainId does not match the prepared withdraw payload");
  }
  if (expectedEvmChainId != null &&
      normalizedEvmQuantity(transaction.chainId, "included EVM chainId") !==
        normalizedEvmQuantity(expectedEvmChainId, "expected EVM chainId")) {
    throw new Error("relayer transaction chainId does not match the active EVM profile");
  }
}

function readProtobufVarint(bytes, offset, label) {
  let value = 0n;
  let shift = 0n;
  for (let index = 0; index < 10; index += 1) {
    if (offset >= bytes.length) throw new Error(`${label} ended inside a varint`);
    const byte = bytes[offset];
    offset += 1;
    value |= BigInt(byte & 0x7f) << shift;
    if ((byte & 0x80) === 0) return { value, offset };
    shift += 7n;
  }
  throw new Error(`${label} contains an oversized varint`);
}

function protobufFields(input, label) {
  const bytes = input instanceof Uint8Array ? input : new Uint8Array(input || []);
  const fields = [];
  let offset = 0;
  while (offset < bytes.length) {
    const tag = readProtobufVarint(bytes, offset, label);
    offset = tag.offset;
    const fieldNumber = Number(tag.value >> 3n);
    const wireType = Number(tag.value & 7n);
    if (!Number.isSafeInteger(fieldNumber) || fieldNumber <= 0) {
      throw new Error(`${label} contains an invalid field number`);
    }
    if (wireType === 0) {
      offset = readProtobufVarint(bytes, offset, label).offset;
    } else if (wireType === 1 || wireType === 5) {
      offset += wireType === 1 ? 8 : 4;
      if (offset > bytes.length) throw new Error(`${label} contains a truncated fixed-width field`);
    } else if (wireType === 2) {
      const length = readProtobufVarint(bytes, offset, label);
      offset = length.offset;
      if (length.value > BigInt(Number.MAX_SAFE_INTEGER)) {
        throw new Error(`${label} contains an oversized field`);
      }
      const end = offset + Number(length.value);
      if (end > bytes.length) throw new Error(`${label} contains a truncated length-delimited field`);
      fields.push({ fieldNumber, wireType, value: bytes.slice(offset, end) });
      offset = end;
    } else {
      throw new Error(`${label} uses unsupported protobuf wire type ${wireType}`);
    }
  }
  return fields;
}

function requiredLengthDelimitedField(fields, fieldNumber, label) {
  const matches = fields.filter(field => field.fieldNumber === fieldNumber && field.wireType === 2);
  if (matches.length !== 1) throw new Error(`${label} must appear exactly once`);
  return matches[0].value;
}

export function cosmosWithdrawMessage(transaction) {
  const raw = transaction?.tx;
  if (!(raw instanceof Uint8Array) || raw.length === 0) {
    throw new Error("included Cosmos transaction omitted raw transaction bytes");
  }
  const bodyBytes = requiredLengthDelimitedField(protobufFields(raw, "Cosmos TxRaw"), 1, "TxRaw body_bytes");
  const messages = protobufFields(bodyBytes, "Cosmos TxBody")
    .filter(field => field.fieldNumber === 1 && field.wireType === 2)
    .map(field => {
      const anyFields = protobufFields(field.value, "Cosmos message Any");
      return {
        typeUrl: new TextDecoder().decode(requiredLengthDelimitedField(anyFields, 1, "Any type_url")),
        value: requiredLengthDelimitedField(anyFields, 2, "Any value")
      };
    });
  const withdrawals = messages.filter(message => message.typeUrl === msgWithdrawTypeUrl);
  if (messages.length !== 1 || withdrawals.length !== 1) {
    throw new Error("included Cosmos transaction must contain exactly one MsgWithdraw");
  }
  return MsgWithdraw.decode(withdrawals[0].value);
}

function assertCosmosRelayTransactionMatches({ transaction, payload }) {
  const message = cosmosWithdrawMessage(transaction);
  assertEqual(bytesHex(message.proof), normalizedHex(payload?.proof_hex, "withdraw proof"), "proof");
  assertEqual(bytesHex(message.root), normalizedHex(payload?.root_hex, "withdraw root"), "root");
  assertEqual(bytesHex(message.nullifier), normalizedHex(payload?.nullifier_hex, "withdraw nullifier"), "nullifier");
  assertEqual(message.amount, payload?.amount, "amount");
  assertEqual(message.recipient, payload?.recipient, "recipient");
  assertEqual(message.chainId, payload?.chain_id, "chain ID");
  if (BigInt(message.expiresAtUnix) !== BigInt(payload?.expires_at_unix)) {
    throw new Error("relayer transaction expiry does not match the prepared withdraw payload");
  }
}

export function assertCosmosRelayWithdrawTransactionPayloadHash({
  transaction,
  payloadHash
} = {}) {
  const expectedPayloadHash = normalizedHex(payloadHash, "reserved relay payload hash");
  if (expectedPayloadHash.length !== 64) {
    throw new Error("reserved relay payload hash must be 32 bytes");
  }
  const message = cosmosWithdrawMessage(transaction);
  const includedPayloadHash = computePreparedWithdrawPayloadHash({
    version: relayWithdrawHandoffVersion,
    proof_hex: bytesHex(message.proof),
    root_hex: bytesHex(message.root),
    nullifier_hex: bytesHex(message.nullifier),
    amount: message.amount,
    recipient: message.recipient,
    chain_id: message.chainId,
    expires_at_unix: message.expiresAtUnix
  });
  if (includedPayloadHash !== expectedPayloadHash) {
    throw new Error("included Cosmos relayer transaction does not match the reserved payload hash");
  }
  return true;
}

export function assertRelayReservationPayloadMatches(records, payload) {
  const payloadHash = requiredText(payload?.payload_hash, "relay payload hash");
  if (!Array.isArray(records) || records.length === 0) {
    throw new Error("relay reservation records are required");
  }
  if (records.some(record => String(record?.payload_hash || "").trim() !== payloadHash)) {
    throw new Error("relay recovery state does not match the reserved payload hash");
  }
}

export function assertRelayWithdrawTransactionMatches({
  transport,
  payload,
  handoffTransaction,
  transaction,
  expectedEvmChainId
} = {}) {
  if (transport === "evm") {
    assertEvmRelayTransactionMatches({ transaction, handoffTransaction, expectedEvmChainId });
  } else if (transport === "cosmos") {
    assertCosmosRelayTransactionMatches({ transaction, payload });
  } else {
    throw new Error(`unsupported relay transaction transport ${JSON.stringify(transport)}`);
  }
  return true;
}

export function relayWithdrawPayloadExpired(payload, chainNowUnix) {
  const expiry = Number(payload?.expires_at_unix);
  const now = Number(chainNowUnix);
  if (!Number.isSafeInteger(expiry) || expiry < 0) {
    throw new Error("relay payload expires_at_unix is invalid");
  }
  if (!Number.isSafeInteger(now) || now < 0) {
    throw new Error("authoritative chain time is invalid");
  }
  return now >= expiry;
}

export function relayWithdrawExpiryLeaseUntil(payload) {
  const expiry = Number(payload?.expires_at_unix);
  if (!Number.isSafeInteger(expiry) || expiry <= 0) {
    throw new Error("relay payload expires_at_unix is invalid");
  }
  const expiryMilliseconds = expiry * 1000;
  if (!Number.isSafeInteger(expiryMilliseconds)) {
    throw new Error("relay payload expiry is outside the supported timestamp range");
  }
  try {
    return new Date(expiryMilliseconds).toISOString();
  } catch {
    throw new Error("relay payload expiry is outside the supported timestamp range");
  }
}
