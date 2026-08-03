const encryptedOperationStoreVersion = "clairveil-encrypted-operation-store-v1";
const encryptionInfo = new TextEncoder().encode("clairveil/encrypted-operation-store/v1");

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
  try {
    return Uint8Array.from(atob(text), character => character.charCodeAt(0));
  } catch {
    throw new Error(`${label} must be base64`);
  }
}

function operationStateError(cause) {
  const error = new Error("Encrypted operation recovery state cannot be decrypted. Manual recovery is required.", { cause });
  error.code = "OPERATION_STATE_CORRUPT";
  return error;
}

async function deriveEncryptionKey({ cryptoImpl, keyMaterial, namespace }) {
  const bytes = keyMaterial instanceof Uint8Array ? keyMaterial : new Uint8Array(keyMaterial || []);
  if (!bytes.length) throw new Error("operation recovery encryption key material is required");
  const material = await cryptoImpl.subtle.importKey("raw", bytes, "HKDF", false, ["deriveKey"]);
  return cryptoImpl.subtle.deriveKey({
    name: "HKDF",
    hash: "SHA-256",
    salt: new TextEncoder().encode(namespace),
    info: encryptionInfo
  }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

export class EncryptedLocalStorageOperationStore {
  static async open({
    storage = globalThis.localStorage,
    cryptoImpl = globalThis.crypto,
    key,
    keyMaterial,
    namespace
  } = {}) {
    if (!storage) throw new Error("localStorage-compatible storage is required");
    if (!cryptoImpl?.subtle || typeof cryptoImpl.getRandomValues !== "function") {
      throw new Error("Web Crypto is required for encrypted operation recovery");
    }
    if (!String(key || "").trim()) throw new Error("encrypted operation recovery key is required");
    const encryptionKey = await deriveEncryptionKey({
      cryptoImpl,
      keyMaterial,
      namespace: String(namespace || key)
    });
    return new EncryptedLocalStorageOperationStore({ storage, cryptoImpl, key, encryptionKey });
  }

  constructor({ storage, cryptoImpl, key, encryptionKey }) {
    this.storage = storage;
    this.cryptoImpl = cryptoImpl;
    this.key = key;
    this.encryptionKey = encryptionKey;
  }

  async load() {
    const raw = this.storage.getItem(this.key);
    if (!raw) return null;
    try {
      const envelope = JSON.parse(raw);
      if (envelope?.version !== encryptedOperationStoreVersion) {
        throw new Error("unsupported encrypted operation recovery version");
      }
      const plaintext = await this.cryptoImpl.subtle.decrypt({
        name: "AES-GCM",
        iv: base64ToBytes(envelope.iv, "operation recovery iv")
      }, this.encryptionKey, base64ToBytes(envelope.ciphertext, "operation recovery ciphertext"));
      return JSON.parse(new TextDecoder().decode(plaintext));
    } catch (error) {
      throw operationStateError(error);
    }
  }

  async save(state) {
    const iv = this.cryptoImpl.getRandomValues(new Uint8Array(12));
    const plaintext = new TextEncoder().encode(JSON.stringify(state, (_key, value) => (
      typeof value === "bigint" ? value.toString() : value
    )));
    const ciphertext = await this.cryptoImpl.subtle.encrypt(
      { name: "AES-GCM", iv },
      this.encryptionKey,
      plaintext
    );
    this.storage.setItem(this.key, JSON.stringify({
      version: encryptedOperationStoreVersion,
      iv: bytesToBase64(iv),
      ciphertext: bytesToBase64(new Uint8Array(ciphertext))
    }));
  }

  clear() {
    this.storage.removeItem(this.key);
  }
}

export { encryptedOperationStoreVersion };
