import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { indexedDB } from "fake-indexeddb";
import {
  createEncryptedBrowserMetadataStore,
  EncryptedIndexedDbNoteStore,
} from "../public/encrypted-browser-store.js";

const storageDatabaseName = "clairveil-webapp-v2";

function installBrowserStorageGlobals({ locks = true } = {}) {
  const originals = new Map(
    ["crypto", "indexedDB", "navigator"].map((name) => [
      name,
      Object.getOwnPropertyDescriptor(globalThis, name),
    ]),
  );
  const lockCalls = [];
  const tails = new Map();
  const lockManager = {
    request(name, _options, callback) {
      lockCalls.push(name);
      const previous = tails.get(name) || Promise.resolve();
      const current = previous.catch(() => {}).then(() => callback());
      tails.set(name, current.catch(() => {}));
      return current;
    },
  };
  Object.defineProperty(globalThis, "crypto", {
    configurable: true,
    value: webcrypto,
  });
  Object.defineProperty(globalThis, "indexedDB", {
    configurable: true,
    value: indexedDB,
  });
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: locks ? { locks: lockManager } : {},
  });
  return {
    lockCalls,
    restore() {
      for (const [name, descriptor] of originals) {
        if (descriptor) Object.defineProperty(globalThis, name, descriptor);
        else delete globalThis[name];
      }
    },
  };
}

function readEncryptedRecord(key) {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(storageDatabaseName, 1);
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result;
      const transaction = db.transaction("encrypted-records", "readonly");
      const request = transaction.objectStore("encrypted-records").get(key);
      request.onerror = () => {
        db.close();
        reject(request.error);
      };
      request.onsuccess = () => {
        db.close();
        resolve(request.result);
      };
    };
  });
}

function writeEncryptedRecord(key, value) {
  return new Promise((resolve, reject) => {
    const open = indexedDB.open(storageDatabaseName, 1);
    open.onerror = () => reject(open.error);
    open.onsuccess = () => {
      const db = open.result;
      const transaction = db.transaction("encrypted-records", "readwrite");
      transaction.objectStore("encrypted-records").put(value, key);
      transaction.oncomplete = () => {
        db.close();
        resolve();
      };
      transaction.onerror = () => {
        db.close();
        reject(transaction.error);
      };
    };
  });
}

test("encrypted browser stores authenticate persistence and require Web Locks", async () => {
  const globals = installBrowserStorageGlobals();
  const namespace = `test:${globalThis.crypto.randomUUID()}`;
  const secretBase64 = Buffer.from("root-signature-for-storage-test").toString("base64");
  const secretPayload = "must-not-appear-in-indexeddb";
  try {
    const metadata = createEncryptedBrowserMetadataStore({ namespace, secretBase64 });
    await metadata.save({ payloadHash: "ab".repeat(32), secretPayload });

    const encrypted = await readEncryptedRecord(`relay-metadata:${namespace}`);
    assert.equal(encrypted.version, "clairveil-encrypted-browser-state-v1");
    assert.equal(JSON.stringify(encrypted).includes(secretPayload), false);
    assert.deepEqual(await metadata.load(), { payloadHash: "ab".repeat(32), secretPayload });
    assert.ok(globals.lockCalls.length >= 2);

    const wrongKeyStore = createEncryptedBrowserMetadataStore({
      namespace,
      secretBase64: Buffer.from("different-root-signature").toString("base64"),
    });
    await assert.rejects(wrongKeyStore.load(), /cannot be authenticated/);

    const notes = new EncryptedIndexedDbNoteStore({
      namespace: `${namespace}:notes`,
      owner: "clair1storage-test",
      secretBase64,
    });
    await notes.save({
      scanCursor: { source: "scan_events", after_height: 12, after_sequence: 7 },
    });
    assert.equal((await notes.load()).scanCursor.after_height, 12);
    await notes.clear();
    assert.deepEqual((await notes.load()).notes, []);

    await notes.save({
      scanCursor: { source: "scan_events", after_height: 14, after_sequence: 8 },
    });
    const replaced = await notes.replaceScanResult({
      foundNotes: [],
      scanCursor: { source: "privacy_scan", next_cursor: { height: 21, global_sequence: 4, output_index: 0 } },
    }, { owner: "clair1storage-test" });
    assert.equal(replaced.scanCursor.source, "privacy_scan");
    assert.deepEqual(replaced.notes, []);

    await notes.save({
      scanCursor: { source: "scan_events", after_height: 15, after_sequence: 9 },
    });
    await writeEncryptedRecord(`notes:${namespace}:notes`, {
      version: "clairveil-encrypted-browser-state-v1",
      iv: Buffer.alloc(12).toString("base64"),
      ciphertext: Buffer.from("corrupted ciphertext").toString("base64"),
    });
    await assert.rejects(notes.load(), /cannot be authenticated/);
    await notes.clear();
    assert.deepEqual((await notes.load()).notes, []);
  } finally {
    globals.restore();
  }
});

test("encrypted browser stores fail closed when Web Locks are unavailable", async () => {
  const globals = installBrowserStorageGlobals({ locks: false });
  try {
    const metadata = createEncryptedBrowserMetadataStore({
      namespace: `test:no-locks:${globalThis.crypto.randomUUID()}`,
      secretBase64: Buffer.from("root-signature-for-storage-test").toString("base64"),
    });
    await assert.rejects(metadata.save({ value: "blocked" }), /Web Locks API is required/);
  } finally {
    globals.restore();
  }
});

test("encrypted browser stores reject missing Web Crypto before an empty record can load", () => {
  const globals = installBrowserStorageGlobals();
  const secretBase64 = Buffer.from("root-signature-for-storage-test").toString("base64");
  try {
    Object.defineProperty(globalThis, "crypto", {
      configurable: true,
      value: undefined,
    });
    assert.throws(
      () => new EncryptedIndexedDbNoteStore({
        namespace: "test:no-web-crypto:notes",
        owner: "clair1storage-test",
        secretBase64,
      }),
      /Web Crypto is required/,
    );
    assert.throws(
      () => createEncryptedBrowserMetadataStore({
        namespace: "test:no-web-crypto:metadata",
        secretBase64,
      }),
      /Web Crypto is required/,
    );
  } finally {
    globals.restore();
  }
});
