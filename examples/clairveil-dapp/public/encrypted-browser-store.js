import { MemoryNoteStore } from "clairveiljs/note-store";

const databaseName = "clairveil-webapp-v2";
const databaseVersion = 1;
const recordStoreName = "encrypted-records";
const encoder = new TextEncoder();
const decoder = new TextDecoder();

function base64FromBytes(bytes) {
  let binary = "";
  for (const byte of bytes) binary += String.fromCharCode(byte);
  return btoa(binary);
}

function bytesFromBase64(value) {
  const binary = atob(String(value || ""));
  const bytes = new Uint8Array(binary.length);
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index);
  }
  return bytes;
}

function requestResult(request) {
  return new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error("IndexedDB request failed"));
  });
}

function transactionDone(transaction) {
  return new Promise((resolve, reject) => {
    transaction.oncomplete = () => resolve();
    transaction.onerror = () => reject(transaction.error || new Error("IndexedDB transaction failed"));
    transaction.onabort = () => reject(transaction.error || new Error("IndexedDB transaction aborted"));
  });
}

function requiredBrowserCrypto() {
  const cryptoApi = globalThis.crypto;
  if (!cryptoApi?.subtle || typeof cryptoApi.getRandomValues !== "function") {
    throw new Error("Web Crypto is required for encrypted Clairveil browser storage");
  }
  return cryptoApi;
}

function requiredBrowserLocks() {
  const locks = globalThis.navigator?.locks;
  if (typeof locks?.request !== "function") {
    throw new Error("Web Locks API is required for encrypted Clairveil browser storage");
  }
  return locks;
}

function requiredIndexedDB() {
  if (!globalThis.indexedDB) {
    throw new Error("IndexedDB is required for encrypted Clairveil browser storage");
  }
  return globalThis.indexedDB;
}

async function deriveNamespaceKey(namespace, secretBase64) {
  const cryptoApi = requiredBrowserCrypto();
  const secret = bytesFromBase64(secretBase64);
  if (!secret.length) {
    throw new Error("Privacy-session root signature is required for encrypted browser storage");
  }
  const domain = encoder.encode(`clairveil.webapp.storage-key.v1\u0000${namespace}\u0000`);
  const material = new Uint8Array(domain.length + secret.length);
  material.set(domain);
  material.set(secret, domain.length);
  const digest = await cryptoApi.subtle.digest("SHA-256", material);
  return cryptoApi.subtle.importKey("raw", digest, { name: "AES-GCM" }, false, [
    "encrypt",
    "decrypt",
  ]);
}

export function createEncryptedStateCodec({ namespace, secretBase64 }) {
  const resolvedNamespace = String(namespace || "");
  if (!resolvedNamespace) throw new Error("Encrypted browser-storage namespace is required");
  // `deriveNamespaceKey` is async, so checking Web Crypto only from inside it
  // would turn a missing API into an unobserved rejected promise when an empty
  // record is loaded. Fail during store construction instead: a privacy
  // session must not start unless every encrypted persistence dependency is
  // available.
  requiredBrowserCrypto();
  const keyPromise = deriveNamespaceKey(resolvedNamespace, secretBase64);
  return {
    async encodeState(state) {
      const cryptoApi = requiredBrowserCrypto();
      const iv = cryptoApi.getRandomValues(new Uint8Array(12));
      const plaintext = encoder.encode(JSON.stringify(state));
      const ciphertext = await cryptoApi.subtle.encrypt(
        { name: "AES-GCM", iv },
        await keyPromise,
        plaintext,
      );
      return {
        version: "clairveil-encrypted-browser-state-v1",
        iv: base64FromBytes(iv),
        ciphertext: base64FromBytes(new Uint8Array(ciphertext)),
      };
    },
    async decodeState(value) {
      if (
        !value ||
        typeof value !== "object" ||
        value.version !== "clairveil-encrypted-browser-state-v1" ||
        typeof value.iv !== "string" ||
        typeof value.ciphertext !== "string"
      ) {
        throw new Error("Encrypted Clairveil browser-storage record is invalid");
      }
      const cryptoApi = requiredBrowserCrypto();
      let plaintext;
      try {
        plaintext = await cryptoApi.subtle.decrypt(
          { name: "AES-GCM", iv: bytesFromBase64(value.iv) },
          await keyPromise,
          bytesFromBase64(value.ciphertext),
        );
      } catch {
        throw new Error("Encrypted Clairveil browser-storage record cannot be authenticated");
      }
      try {
        const decoded = JSON.parse(decoder.decode(plaintext));
        if (!decoded || typeof decoded !== "object" || Array.isArray(decoded)) {
          throw new Error("Encrypted Clairveil browser-storage state is invalid");
        }
        return decoded;
      } catch (error) {
        if (error?.message === "Encrypted Clairveil browser-storage state is invalid") {
          throw error;
        }
        throw new Error("Encrypted Clairveil browser-storage state cannot be decoded");
      }
    },
  };
}

class EncryptedIndexedDbRecord {
  constructor({ namespace, secretBase64, kind }) {
    this.namespace = String(namespace || "");
    this.kind = String(kind || "");
    if (!this.namespace || !this.kind) {
      throw new Error("Encrypted browser-storage namespace and kind are required");
    }
    this.recordKey = `${this.kind}:${this.namespace}`;
    this.lockName = `clairveil-browser-storage:${this.recordKey}`;
    this.codec = createEncryptedStateCodec({
      namespace: this.recordKey,
      secretBase64,
    });
    this.dbPromise = null;
  }

  async db() {
    if (this.dbPromise) return this.dbPromise;
    const indexedDB = requiredIndexedDB();
    this.dbPromise = new Promise((resolve, reject) => {
      const request = indexedDB.open(databaseName, databaseVersion);
      request.onupgradeneeded = () => {
        if (!request.result.objectStoreNames.contains(recordStoreName)) {
          request.result.createObjectStore(recordStoreName);
        }
      };
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error || new Error("IndexedDB open failed"));
    });
    return this.dbPromise;
  }

  async withLock(callback) {
    return requiredBrowserLocks().request(
      this.lockName,
      { mode: "exclusive" },
      callback,
    );
  }

  async loadUnlocked() {
    const db = await this.db();
    const transaction = db.transaction(recordStoreName, "readonly");
    const record = await requestResult(
      transaction.objectStore(recordStoreName).get(this.recordKey),
    );
    await transactionDone(transaction);
    return record === undefined ? null : this.codec.decodeState(record);
  }

  async saveUnlocked(value) {
    const encrypted = await this.codec.encodeState(value);
    const db = await this.db();
    const transaction = db.transaction(recordStoreName, "readwrite");
    transaction.objectStore(recordStoreName).put(encrypted, this.recordKey);
    await transactionDone(transaction);
  }

  async clearUnlocked() {
    const db = await this.db();
    const transaction = db.transaction(recordStoreName, "readwrite");
    transaction.objectStore(recordStoreName).delete(this.recordKey);
    await transactionDone(transaction);
  }
}

export class EncryptedIndexedDbNoteStore {
  constructor({ namespace, owner = "", secretBase64 }) {
    this.owner = String(owner || "");
    this.record = new EncryptedIndexedDbRecord({
      namespace,
      secretBase64,
      kind: "notes",
    });
  }

  async withStore({ persist = false } = {}, callback) {
    return this.record.withLock(async () => {
      const saved = await this.record.loadUnlocked();
      const store = new MemoryNoteStore({ owner: this.owner, state: saved || undefined });
      const result = await callback(store);
      if (persist) await this.record.saveUnlocked(store.state);
      return result;
    });
  }

  async load() {
    return this.withStore({}, (store) => store.load());
  }

  async save(state) {
    return this.withStore({ persist: true }, (store) => store.save(state));
  }

  async clear() {
    // Recovery must be able to remove an authenticated-but-corrupt record.
    // `withStore` loads before invoking its callback, which would make a
    // decrypt failure impossible to reset through the UI.
    return this.record.withLock(() => this.record.clearUnlocked());
  }

  async mergeScanResult(scanResult, options) {
    return this.withStore({ persist: true }, (store) =>
      store.mergeScanResult(scanResult, options),
    );
  }

  async replaceScanResult(scanResult, options) {
    // A completed genesis scan is authoritative for the wallet session. Replace
    // the cached inventory under one lock so an interrupted reconciliation can
    // never leave a cleared cache between two separate IndexedDB writes.
    return this.withStore({ persist: true }, async (store) => {
      await store.clear();
      return store.mergeScanResult(scanResult, options);
    });
  }

  async rollbackToHeight(height) {
    return this.withStore({ persist: true }, (store) => store.rollbackToHeight(height));
  }

  async markSpent(nullifiers) {
    return this.withStore({ persist: true }, (store) => store.markSpent(nullifiers));
  }

  async setNullifierStatuses(statuses) {
    return this.withStore({ persist: true }, (store) =>
      store.setNullifierStatuses(statuses),
    );
  }
}

export function createEncryptedBrowserMetadataStore({ namespace, secretBase64 }) {
  const record = new EncryptedIndexedDbRecord({
    namespace,
    secretBase64,
    kind: "relay-metadata",
  });
  return {
    load() {
      return record.withLock(() => record.loadUnlocked());
    },
    save(value) {
      return record.withLock(() => record.saveUnlocked(value));
    },
    clear() {
      return record.withLock(() => record.clearUnlocked());
    },
  };
}
