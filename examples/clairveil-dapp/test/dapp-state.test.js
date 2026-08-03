import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { EncryptedLocalStorageOperationStore } from "../public/encrypted-operation-store.js";
import { disclosureViewModel } from "../public/disclosure-view-model.js";
import { findPrivacyEventByTxHash } from "../public/operation-event-lookup.js";
import {
  loadPublicPendingTxState,
  publicPendingTxKey,
  savePublicPendingTxState
} from "../public/public-pending-tx-store.js";

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
