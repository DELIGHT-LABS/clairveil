import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import test from "node:test";

import {
  EncryptedRecoveryArtifactStore,
  encryptedRecoveryArtifactStoreVersion
} from "../public/encrypted-recovery-artifact-store.js";

class MemoryStorage {
  constructor() {
    this.values = new Map();
  }

  get length() {
    return this.values.size;
  }

  key(index) {
    return [...this.values.keys()][index] ?? null;
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

class MemoryLocks {
  constructor() {
    this.tails = new Map();
    this.requests = [];
  }

  request(name, options, callback) {
    this.requests.push({ name, options });
    const previous = this.tails.get(name) || Promise.resolve();
    const current = previous.then(callback);
    const tail = current.then(() => undefined, () => undefined);
    this.tails.set(name, tail);
    void tail.then(() => {
      if (this.tails.get(name) === tail) this.tails.delete(name);
    });
    return current;
  }
}

function storeOptions(overrides = {}) {
  return {
    storage: new MemoryStorage(),
    cryptoImpl: webcrypto,
    locks: new MemoryLocks(),
    key: "clairveil:v0.3.1:evm-artifact:profile:owner",
    profileId: "evm:chain-1:0x0000000000000000000000000000000000000900",
    owner: "0x1111111111111111111111111111111111111111",
    keyMaterial: webcrypto.getRandomValues(new Uint8Array(32)),
    ...overrides
  };
}

function assertCorrupt(error) {
  return error?.code === "RECOVERY_ARTIFACT_STATE_CORRUPT";
}

test("encrypted recovery artifacts round-trip as one identity-bound record", async () => {
  const options = storeOptions();
  const store = await EncryptedRecoveryArtifactStore.open(options);
  const artifact = {
    version: "clairveil-evm-operation-artifact-v1",
    phase: "proof-ready",
    transaction: { to: "0x0000000000000000000000000000000000000900", data: "0xsecret" },
    reservationIds: ["reservation-1"]
  };

  assert.equal(await store.load(), null);
  assert.deepEqual(await store.save(artifact), artifact);
  const raw = options.storage.getItem(options.key);
  assert.ok(raw);
  assert.doesNotMatch(raw, /proof-ready|0xsecret|reservation-1/);
  assert.deepEqual(await store.load(), artifact);
  assert.equal(options.locks.requests.every(request => request.options.mode === "exclusive"), true);

  await store.clear();
  assert.equal(await store.load(), null);
});

test("artifact identity binds profile, normalized owner, and storage key", async () => {
  const options = storeOptions({ owner: "0xABCDEFabcdefABCDEFabcdefABCDEFabcdefABCD" });
  const store = await EncryptedRecoveryArtifactStore.open(options);
  await store.save({ phase: "submitted", txHash: `0x${"12".repeat(32)}` });
  const raw = options.storage.getItem(options.key);

  const copiedKey = `${options.key}:copied`;
  options.storage.setItem(copiedKey, raw);
  const copied = await EncryptedRecoveryArtifactStore.open({
    ...options,
    key: copiedKey
  });
  await assert.rejects(() => copied.load(), assertCorrupt);

  const differentOwner = await EncryptedRecoveryArtifactStore.open({
    ...options,
    owner: "0x2222222222222222222222222222222222222222"
  });
  await assert.rejects(() => differentOwner.load(), assertCorrupt);

  const differentProfile = await EncryptedRecoveryArtifactStore.open({
    ...options,
    profileId: "evm:chain-2:0x0000000000000000000000000000000000000900"
  });
  await assert.rejects(() => differentProfile.load(), assertCorrupt);
});

test("Web Locks are mandatory and beforeCommit fences the final mutation", async () => {
  const options = storeOptions();
  await assert.rejects(
    () => EncryptedRecoveryArtifactStore.open({ ...options, locks: null }),
    /Web Locks API is required/
  );
  const store = await EncryptedRecoveryArtifactStore.open(options);
  await assert.rejects(
    () => store.save({ phase: "proof-ready" }, {
      beforeCommit() {
        throw new Error("stale privacy session");
      }
    }),
    /stale privacy session/
  );
  assert.equal(options.storage.getItem(options.key), null);

  await store.save({ phase: "submitted" });
  const saved = options.storage.getItem(options.key);
  await assert.rejects(
    () => store.clear({
      beforeCommit() {
        throw new Error("profile changed");
      }
    }),
    /profile changed/
  );
  assert.equal(options.storage.getItem(options.key), saved);
});

test("strict envelope validation reports corruption without exposing internals", async () => {
  const options = storeOptions();
  const store = await EncryptedRecoveryArtifactStore.open(options);
  await store.save({ phase: "proof-ready" });
  const valid = JSON.parse(options.storage.getItem(options.key));
  assert.equal(valid.version, encryptedRecoveryArtifactStoreVersion);

  for (const envelope of [
    "not-json",
    JSON.stringify({ ...valid, version: "legacy" }),
    JSON.stringify({ ...valid, unsupported: true }),
    JSON.stringify({ ...valid, identity: { ...valid.identity, owner: "other" } }),
    JSON.stringify({ ...valid, iv: "AA==" }),
    JSON.stringify({ ...valid, ciphertext: "AA==" })
  ]) {
    options.storage.setItem(options.key, envelope);
    await assert.rejects(() => store.load(), error => {
      assert.equal(error.code, "RECOVERY_ARTIFACT_STATE_CORRUPT");
      assert.match(error.message, /active profile, owner, and storage key/);
      return true;
    });
  }
});

test("atomic update and clearIf reject a stale operation identity", async () => {
  const options = storeOptions();
  const firstTab = await EncryptedRecoveryArtifactStore.open(options);
  const secondTab = await EncryptedRecoveryArtifactStore.open(options);
  const oldArtifact = {
    operationId: "operation-old",
    phase: "submitted",
    txHash: `0x${"11".repeat(32)}`
  };
  const newArtifact = {
    operationId: "operation-new",
    phase: "proof-ready"
  };

  await firstTab.save(oldArtifact);
  const staleIdentity = (await firstTab.load()).operationId;
  await secondTab.save(newArtifact);

  const staleUpdate = await firstTab.update(current => (
    current?.operationId === staleIdentity
      ? { ...current, phase: "receipt-verified" }
      : undefined
  ));
  assert.equal(staleUpdate.changed, false);
  assert.deepEqual(staleUpdate.artifact, newArtifact);
  assert.deepEqual(await secondTab.load(), newArtifact);

  const staleClear = await firstTab.clearIf(
    current => current.operationId === staleIdentity
  );
  assert.equal(staleClear.changed, false);
  assert.deepEqual(staleClear.artifact, newArtifact);
  assert.deepEqual(await secondTab.load(), newArtifact);
});

test("atomic update commits one matching replacement or clear under the store lock", async () => {
  const options = storeOptions();
  const store = await EncryptedRecoveryArtifactStore.open(options);
  await store.save({ operationId: "operation-1", phase: "proof-ready" });

  let beforeCommitCalls = 0;
  const updated = await store.update(current => ({
    ...current,
    phase: "submitted",
    txHash: `0x${"22".repeat(32)}`
  }), {
    beforeCommit() {
      beforeCommitCalls += 1;
    }
  });
  assert.equal(updated.changed, true);
  assert.equal(updated.previous.phase, "proof-ready");
  assert.equal(updated.artifact.phase, "submitted");
  assert.equal(beforeCommitCalls, 1);
  assert.deepEqual(await store.load(), updated.artifact);

  const cleared = await store.clearIf(
    current => current.operationId === "operation-1",
    {
      beforeCommit() {
        beforeCommitCalls += 1;
      }
    }
  );
  assert.equal(cleared.changed, true);
  assert.equal(cleared.previous.phase, "submitted");
  assert.equal(cleared.artifact, null);
  assert.equal(beforeCommitCalls, 2);
  assert.equal(await store.load(), null);
});
