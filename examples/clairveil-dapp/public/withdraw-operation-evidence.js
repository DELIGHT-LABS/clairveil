import { sha256Hex } from "clairveiljs/browser-crypto";
import { bech32AddressToEvm, evmAddressToBech32 } from "clairveiljs/evm";
import { hashAmount } from "clairveiljs/reservation";

import { cosmosWithdrawMessage } from "./relay-withdraw-reconciliation.js";

function normalizedHex(value, label, { bytes = 0 } = {}) {
  const hex = String(value || "").trim().replace(/^0x/i, "").toLowerCase();
  if (!/^[0-9a-f]+$/.test(hex) || hex.length % 2 !== 0 || (bytes && hex.length !== bytes * 2)) {
    throw new Error(`${label} must be canonical${bytes ? ` ${bytes}-byte` : ""} hex`);
  }
  return hex;
}

function bytesHex(value) {
  return [...(value || [])].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function eventAttribute(event, key) {
  return (event?.attributes || []).find(attribute => attribute?.key === key)?.value || "";
}

function parseCosmosCoin(value) {
  const match = /^(0|[1-9][0-9]*)([A-Za-z][A-Za-z0-9/:._-]{2,127})$/.exec(String(value || "").trim());
  if (!match) throw new Error("included MsgWithdraw amount must be a canonical Cosmos coin");
  return { amount: match[1], denom: match[2] };
}

function requiredText(value, label) {
  const text = String(value || "").trim();
  if (!text) throw new Error(`${label} is required`);
  return text;
}

export function hashTransparentCosmosRecipient(recipient, { accountPrefix } = {}) {
  const normalizedRecipient = requiredText(recipient, "transparent recipient");
  const normalizedPrefix = requiredText(accountPrefix, "account prefix").toLowerCase();
  if (!/^[a-z0-9]+$/.test(normalizedPrefix) || normalizedPrefix.includes("1")) {
    throw new Error("account prefix must be a lowercase bech32 prefix without separator");
  }
  let canonicalRecipient;
  try {
    const evmAddress = bech32AddressToEvm(normalizedRecipient, normalizedPrefix);
    canonicalRecipient = evmAddressToBech32(evmAddress, normalizedPrefix);
  } catch {
    throw new Error(`transparent recipient must be a ${normalizedPrefix} account address`);
  }
  return sha256Hex(canonicalRecipient);
}

export function cosmosWithdrawOperationEvidence({
  event,
  transaction,
  txHash,
  expectedNullifiers = [],
  accountPrefix
} = {}) {
  if (event?.event_type !== "withdraw") return null;

  const expectedTxHash = normalizedHex(txHash, "withdraw transaction hash");
  const eventTxHash = normalizedHex(event?.tx_hash_hex, "withdraw event transaction hash");
  if (eventTxHash !== expectedTxHash) return null;

  const normalizedExpectedNullifiers = expectedNullifiers.map((nullifier, index) => (
    normalizedHex(nullifier, `withdraw input nullifier ${index}`, { bytes: 32 })
  ));
  if (normalizedExpectedNullifiers.length !== 1 || new Set(normalizedExpectedNullifiers).size !== 1) {
    throw new Error("direct MsgWithdraw evidence requires exactly one reserved input nullifier");
  }

  const eventNullifier = normalizedHex(
    eventAttribute(event, "nullifier"),
    "withdraw event nullifier",
    { bytes: 32 }
  );
  if (eventNullifier !== normalizedExpectedNullifiers[0]) return null;

  const message = cosmosWithdrawMessage(transaction);
  const messageNullifier = normalizedHex(bytesHex(message.nullifier), "included MsgWithdraw nullifier", { bytes: 32 });
  if (messageNullifier !== eventNullifier) return null;

  const eventRecipient = requiredText(eventAttribute(event, "recipient"), "withdraw event recipient");
  const messageRecipient = requiredText(message.recipient, "included MsgWithdraw recipient");
  if (eventRecipient !== messageRecipient) return null;

  const coin = parseCosmosCoin(message.amount);
  const eventAmount = String(eventAttribute(event, "amount") || "").trim();
  if (eventAmount && eventAmount !== message.amount) return null;

  return {
    txHash: eventTxHash,
    outputCommitment: "",
    auditDisclosureDigest: "",
    recipientHash: hashTransparentCosmosRecipient(messageRecipient, { accountPrefix }),
    amount: coin.amount,
    amountHash: hashAmount(coin.denom, coin.amount),
    denom: coin.denom,
    batchItemIndex: 0,
    batchItemIndexKnown: false
  };
}
