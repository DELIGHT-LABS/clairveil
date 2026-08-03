import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { MsgWithdraw, msgWithdrawTypeUrl } from "clairveiljs/cosmos-client";
import { assertDepositFundingAvailable } from "../public/deposit-funding.js";
import { EncryptedLocalStorageOperationStore } from "../public/encrypted-operation-store.js";
import { disclosureViewModel } from "../public/disclosure-view-model.js";
import { findPrivacyEventByTxHash } from "../public/operation-event-lookup.js";
import {
  loadPublicPendingTxState,
  publicPendingTxKey,
  savePublicPendingTxState
} from "../public/public-pending-tx-store.js";
import {
  assertRelayReservationPayloadMatches,
  assertRelayWithdrawTransactionMatches,
  relayWithdrawPayloadExpired
} from "../public/relay-withdraw-reconciliation.js";

class MemoryStorage {
  constructor() {
    this.values = new Map();
  }

  getItem(key) {
    return this.values.get(key) ?? null;
  }

  setItem(key, value) {
    this.values.set(key, String(value));
  }

  removeItem(key) {
    this.values.delete(key);
  }
}

function hexBytes(value) {
  return Uint8Array.from(String(value).match(/../g).map(byte => Number.parseInt(byte, 16)));
}

function protobufVarint(value) {
  let remaining = BigInt(value);
  const bytes = [];
  while (remaining >= 0x80n) {
    bytes.push(Number((remaining & 0x7fn) | 0x80n));
    remaining >>= 7n;
  }
  bytes.push(Number(remaining));
  return Uint8Array.from(bytes);
}

function concatBytes(...values) {
  const result = new Uint8Array(values.reduce((sum, value) => sum + value.length, 0));
  let offset = 0;
  for (const value of values) {
    result.set(value, offset);
    offset += value.length;
  }
  return result;
}

function protobufBytesField(fieldNumber, value) {
  const bytes = value instanceof Uint8Array ? value : new TextEncoder().encode(String(value));
  return concatBytes(protobufVarint((BigInt(fieldNumber) << 3n) | 2n), protobufVarint(bytes.length), bytes);
}

function cosmosWithdrawTx(payload, overrides = {}) {
  const message = MsgWithdraw.fromPartial({
    creator: "clair1relayer",
    proof: hexBytes(payload.proof_hex),
    root: hexBytes(payload.root_hex),
    nullifier: hexBytes(payload.nullifier_hex),
    amount: payload.amount,
    recipient: payload.recipient,
    chainId: payload.chain_id,
    expiresAtUnix: BigInt(payload.expires_at_unix),
    ...overrides
  });
  const any = concatBytes(
    protobufBytesField(1, msgWithdrawTypeUrl),
    protobufBytesField(2, MsgWithdraw.encode(message).finish())
  );
  const bodyBytes = protobufBytesField(1, any);
  return protobufBytesField(1, bodyBytes);
}

test("deposit funding separates EVM asset and native gas balances", () => {
  assert.deepEqual(assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "10",
    nativeBalance: "3",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), {
    amount: 10n,
    fee: 3n,
    assetBalance: 10n,
    nativeBalance: 3n,
    requiredAsset: 10n,
    requiredNative: 3n
  });
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "9",
    nativeBalance: "100",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), /Insufficient transparent uusdc balance/);
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "100",
    nativeBalance: "2",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), /Insufficient EVM gas balance/);
});

test("deposit funding combines amount and fee when the asset is EVM native", () => {
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "100",
    nativeBalance: "12",
    assetDenom: "uclair",
    nativeDenom: "uclair",
    transport: "evm"
  }), /need 13uclair/);
});

test("relay reconciliation binds EVM calldata, target, value, and chain", () => {
  const prepared = {
    to: "0x100000000000000000000000000000000000000b",
    data: "0x1234abcd",
    value: "0x0",
    chainId: "0x32f"
  };
  const included = {
    to: prepared.to.toUpperCase(),
    input: prepared.data,
    value: "0x0",
    chainId: "0x32f"
  };
  assert.equal(assertRelayWithdrawTransactionMatches({
    transport: "evm",
    handoffTransaction: prepared,
    transaction: included,
    expectedEvmChainId: "0x32f"
  }), true);
  assert.throws(() => assertRelayWithdrawTransactionMatches({
    transport: "evm",
    handoffTransaction: prepared,
    transaction: { ...included, input: "0x1234abce" },
    expectedEvmChainId: "0x32f"
  }), /calldata does not match/);
});

test("relay reconciliation binds every Cosmos MsgWithdraw field except creator", () => {
  const payload = {
    proof_hex: "40".repeat(128),
    root_hex: "11".repeat(32),
    nullifier_hex: "22".repeat(32),
    amount: "10uclair",
    recipient: "clair1recipient",
    chain_id: "clairveil-local-2",
    expires_at_unix: 4_102_448_400,
    payload_hash: "33".repeat(32)
  };
  const transaction = { tx: cosmosWithdrawTx(payload) };
  assert.equal(assertRelayWithdrawTransactionMatches({
    transport: "cosmos",
    payload,
    transaction
  }), true);
  assert.throws(() => assertRelayWithdrawTransactionMatches({
    transport: "cosmos",
    payload,
    transaction: { tx: cosmosWithdrawTx(payload, { recipient: "clair1redirected" }) }
  }), /recipient does not match/);
  assert.equal(assertRelayReservationPayloadMatches([{ payload_hash: payload.payload_hash }], payload), undefined);
  assert.throws(
    () => assertRelayReservationPayloadMatches([{ payload_hash: "44".repeat(32) }], payload),
    /does not match the reserved payload hash/
  );
});

test("relay expiry uses the authoritative block-time boundary", () => {
  const payload = { expires_at_unix: 100 };
  assert.equal(relayWithdrawPayloadExpired(payload, 99), false);
  assert.equal(relayWithdrawPayloadExpired(payload, 100), true);
});

test("public pending transactions survive reload and clear only after resolution", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "evm-local", owner: "0xabc" };
  const key = publicPendingTxKey(identity);
  savePublicPendingTxState(storage, key, {
    ...identity,
    send: { txHash: `0x${"01".repeat(32)}`, status: "checking" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "submitted", height: "0x10" }
  });

  assert.deepEqual(loadPublicPendingTxState(storage, key, identity), {
    send: { txHash: `0x${"01".repeat(32)}`, status: "unknown" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "submitted", height: "0x10" }
  });

  savePublicPendingTxState(storage, key, {
    ...identity,
    send: { txHash: `0x${"01".repeat(32)}`, status: "included" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "failed" }
  });
  assert.equal(storage.getItem(key), null);
});

test("corrupt public pending state fails closed", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "evm-local", owner: "0xabc" };
  const key = publicPendingTxKey(identity);
  storage.setItem(key, "not-json");
  assert.throws(
    () => loadPublicPendingTxState(storage, key, identity),
    error => error.code === "PUBLIC_PENDING_STATE_CORRUPT"
  );
});

test("relay handoff recovery is encrypted and can be reopened", async () => {
  const storage = new MemoryStorage();
  const keyMaterial = webcrypto.getRandomValues(new Uint8Array(32));
  const options = {
    storage,
    cryptoImpl: webcrypto,
    key: "operation-state",
    namespace: "chain:profile:owner",
    keyMaterial
  };
  const store = await EncryptedLocalStorageOperationStore.open(options);
  const state = {
    version: "clairveil-relay-withdraw-recovery-v1",
    relayWithdraw: { json: "private witness", reservationIds: ["r1"], txHash: "ABC" }
  };
  await store.save(state);
  assert.doesNotMatch(storage.getItem(options.key), /private witness/);

  const reopened = await EncryptedLocalStorageOperationStore.open(options);
  assert.deepEqual(await reopened.load(), state);

  const wrongKey = await EncryptedLocalStorageOperationStore.open({
    ...options,
    keyMaterial: webcrypto.getRandomValues(new Uint8Array(32))
  });
  await assert.rejects(() => wrongKey.load(), error => error.code === "OPERATION_STATE_CORRUPT");
});

test("operation event lookup traverses pages at the included height", async () => {
  const requests = [];
  const event = { event_type: "shielded_transfer", height: "42", tx_hash_hex: "ABCD" };
  const found = await findPrivacyEventByTxHash({
    txHash: "0xabcd",
    height: 42,
    fetchPage: async request => {
      requests.push(request);
      return request.page === 1
        ? { page: 1, has_more: true, events: [{ height: "42", tx_hash_hex: "OTHER" }] }
        : { page: 2, has_more: false, events: [event] };
    }
  });

  assert.equal(found, event);
  assert.deepEqual(requests.map(request => [request.afterHeight, request.page, request.limit]), [
    [41, 1, 200],
    [41, 2, 200]
  ]);
});

test("operation event lookup propagates query failures", async () => {
  await assert.rejects(() => findPrivacyEventByTxHash({
    txHash: "ABCD",
    height: 42,
    fetchPage: async () => {
      throw new Error("REST unavailable");
    }
  }), /REST unavailable/);
});

test("unverified disclosure reports never expose plaintext view data", () => {
  const report = {
    verification: { verified: false },
    summary: { amount: "100", from_shielded_address: "secret" },
    payload: { plaintext: "secret" }
  };
  assert.deepEqual(disclosureViewModel(report), {
    verified: false,
    plane: "",
    policy: "",
    outputIndex: null,
    commitmentHex: "",
    digestHex: "",
    summary: null,
    payload: null
  });
  const verified = disclosureViewModel({
    ...report,
    verification: { verified: true },
    plane: "user",
    policy: "public",
    output_index: 2,
    commitment_hex: "11".repeat(32),
    digest_hex: "22".repeat(32)
  });
  assert.equal(verified.summary.amount, "100");
  assert.deepEqual(
    [verified.plane, verified.policy, verified.outputIndex, verified.commitmentHex, verified.digestHex],
    ["user", "public", 2, "11".repeat(32), "22".repeat(32)]
  );
});
