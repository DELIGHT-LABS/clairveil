import { MemoryNoteStore } from "clairveiljs/note-store";

const encryptedNoteStoreVersion = "clairveil-encrypted-note-store-v1";
const encryptionInfo = new TextEncoder().encode("clairveil/encrypted-note-store/v1");

function bytesToBase64(bytes) {
  let binary = "";
  for (let offset = 0; offset < bytes.length; offset += 0x8000) {
    binary += String.fromCharCode(...bytes.slice(offset, offset + 0x8000));
  }
  return btoa(binary);
}

function base64ToBytes(value, label) {
  const text = String(value || "").trim();
  if (!text) throw new Error(`${label} is required`);
  let binary;
  try {
    binary = atob(text);
  } catch {
    throw new Error(`${label} must be base64`);
  }
  return Uint8Array.from(binary, character => character.charCodeAt(0));
}

function corruptionError(cause) {
  const error = new Error("Encrypted note cache cannot be decrypted. Back it up, then reset and rescan.", { cause });
  error.code = "NOTE_CACHE_CORRUPT";
  return error;
}

async function deriveEncryptionKey({ cryptoImpl, keyMaterial, namespace }) {
  const bytes = keyMaterial instanceof Uint8Array ? keyMaterial : new Uint8Array(keyMaterial || []);
  if (!bytes.length) throw new Error("note cache encryption key material is required");
  const material = await cryptoImpl.subtle.importKey("raw", bytes, "HKDF", false, ["deriveKey"]);
  return cryptoImpl.subtle.deriveKey({
    name: "HKDF",
    hash: "SHA-256",
    salt: new TextEncoder().encode(namespace),
    info: encryptionInfo
  }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

async function decryptState({ cryptoImpl, encryptionKey, raw }) {
  try {
    const envelope = JSON.parse(raw);
    if (envelope?.version !== encryptedNoteStoreVersion) {
      throw new Error("unsupported encrypted note cache version");
    }
    const plaintext = await cryptoImpl.subtle.decrypt({
      name: "AES-GCM",
      iv: base64ToBytes(envelope.iv, "note cache iv")
    }, encryptionKey, base64ToBytes(envelope.ciphertext, "note cache ciphertext"));
    return JSON.parse(new TextDecoder().decode(plaintext));
  } catch (error) {
    throw corruptionError(error);
  }
}

export class EncryptedLocalStorageNoteStore extends MemoryNoteStore {
  static async open({
    storage = globalThis.localStorage,
    cryptoImpl = globalThis.crypto,
    key,
    owner,
    keyMaterial,
    namespace
  } = {}) {
    if (!storage) throw new Error("localStorage-compatible storage is required");
    if (!cryptoImpl?.subtle || typeof cryptoImpl.getRandomValues !== "function") {
      throw new Error("Web Crypto is required for encrypted note storage");
    }
    if (!String(key || "").trim()) throw new Error("encrypted note cache key is required");
    const encryptionKey = await deriveEncryptionKey({
      cryptoImpl,
      keyMaterial,
      namespace: String(namespace || `${key}:${owner || ""}`)
    });
    const raw = storage.getItem(key);
    const state = raw ? await decryptState({ cryptoImpl, encryptionKey, raw }) : undefined;
    return new EncryptedLocalStorageNoteStore({
      storage,
      cryptoImpl,
      key,
      owner,
      encryptionKey,
      state
    });
  }

  constructor({ storage, cryptoImpl, key, owner, encryptionKey, state }) {
    super({ owner, state });
    this.storage = storage;
    this.cryptoImpl = cryptoImpl;
    this.key = key;
    this.encryptionKey = encryptionKey;
  }

  async save(state) {
    const loaded = await super.save(state);
    const iv = this.cryptoImpl.getRandomValues(new Uint8Array(12));
    const plaintext = new TextEncoder().encode(JSON.stringify(this.state));
    const ciphertext = await this.cryptoImpl.subtle.encrypt(
      { name: "AES-GCM", iv },
      this.encryptionKey,
      plaintext
    );
    this.storage.setItem(this.key, JSON.stringify({
      version: encryptedNoteStoreVersion,
      iv: bytesToBase64(iv),
      ciphertext: bytesToBase64(new Uint8Array(ciphertext))
    }));
    return loaded;
  }

  async clear() {
    this.storage.removeItem(this.key);
    return super.clear();
  }

  exportEncryptedBackup() {
    return this.storage.getItem(this.key) || "";
  }
}

export { encryptedNoteStoreVersion };
