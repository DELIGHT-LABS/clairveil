const encryptedRecoveryArtifactStoreVersion = "clairveil-encrypted-recovery-artifact-store-v1";
const textEncoder = new TextEncoder();
const textDecoder = new TextDecoder();
const encryptionInfo = textEncoder.encode("clairveil/encrypted-recovery-artifact-store/v1");

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
  let bytes;
  try {
    bytes = Uint8Array.from(atob(text), character => character.charCodeAt(0));
  } catch {
    throw new Error(`${label} must be canonical base64`);
  }
  if (bytesToBase64(bytes) !== text) {
    throw new Error(`${label} must be canonical base64`);
  }
  return bytes;
}

function plainObject(value) {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function assertExactKeys(value, expected, label) {
  if (!plainObject(value)) throw new Error(`${label} must be an object`);
  const actual = Object.keys(value).sort();
  const wanted = [...expected].sort();
  if (actual.length !== wanted.length
    || actual.some((key, index) => key !== wanted[index])) {
    throw new Error(`${label} contains unsupported fields`);
  }
}

function boundedIdentityText(value, label, maxLength) {
  const text = String(value || "").trim();
  if (!text || text.length > maxLength) {
    throw new Error(`${label} must be between 1 and ${maxLength} characters`);
  }
  return text;
}

function normalizedIdentity({ profileId, owner, key } = {}) {
  return Object.freeze({
    profileId: boundedIdentityText(profileId, "recovery artifact profileId", 256),
    owner: boundedIdentityText(owner, "recovery artifact owner", 256).toLowerCase(),
    key: boundedIdentityText(key, "recovery artifact storage key", 1024)
  });
}

function identityBytes(identity) {
  return textEncoder.encode(JSON.stringify([
    encryptedRecoveryArtifactStoreVersion,
    identity.profileId,
    identity.owner,
    identity.key
  ]));
}

function identityMatches(actual, expected) {
  return actual?.profileId === expected.profileId
    && actual?.owner === expected.owner
    && actual?.key === expected.key;
}

function corruptionError(cause) {
  const error = new Error(
    "Encrypted recovery artifact cannot be decrypted or does not match the active profile, owner, and storage key.",
    { cause }
  );
  error.code = "RECOVERY_ARTIFACT_STATE_CORRUPT";
  return error;
}

function normalizeArtifact(artifact) {
  if (!plainObject(artifact)) {
    throw new Error("recovery artifact must be an object");
  }
  const encoded = JSON.stringify(artifact, (_key, value) => (
    typeof value === "bigint" ? value.toString() : value
  ));
  if (!encoded) throw new Error("recovery artifact must be JSON serializable");
  const normalized = JSON.parse(encoded);
  if (!plainObject(normalized)) {
    throw new Error("recovery artifact must serialize to an object");
  }
  return normalized;
}

function assertBeforeCommit(beforeCommit) {
  if (beforeCommit !== undefined && typeof beforeCommit !== "function") {
    throw new Error("encrypted recovery artifact beforeCommit must be a function");
  }
}

function assertUpdater(updater) {
  if (typeof updater !== "function") {
    throw new Error("encrypted recovery artifact updater must be a function");
  }
}

function assertPredicate(predicate) {
  if (typeof predicate !== "function") {
    throw new Error("encrypted recovery artifact predicate must be a function");
  }
}

async function deriveEncryptionKey({ cryptoImpl, keyMaterial, identity }) {
  const bytes = keyMaterial instanceof Uint8Array
    ? keyMaterial
    : new Uint8Array(keyMaterial || []);
  if (!bytes.length) {
    throw new Error("recovery artifact encryption key material is required");
  }
  const material = await cryptoImpl.subtle.importKey(
    "raw",
    bytes,
    "HKDF",
    false,
    ["deriveKey"]
  );
  return cryptoImpl.subtle.deriveKey({
    name: "HKDF",
    hash: "SHA-256",
    salt: identityBytes(identity),
    info: encryptionInfo
  }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

async function decryptArtifact({ cryptoImpl, encryptionKey, identity, raw }) {
  try {
    const envelope = JSON.parse(raw);
    assertExactKeys(
      envelope,
      ["version", "identity", "iv", "ciphertext"],
      "encrypted recovery artifact envelope"
    );
    if (envelope.version !== encryptedRecoveryArtifactStoreVersion) {
      throw new Error("unsupported encrypted recovery artifact version");
    }
    assertExactKeys(
      envelope.identity,
      ["profileId", "owner", "key"],
      "encrypted recovery artifact identity"
    );
    if (!identityMatches(envelope.identity, identity)) {
      throw new Error("encrypted recovery artifact identity does not match");
    }
    const iv = base64ToBytes(envelope.iv, "recovery artifact iv");
    if (iv.length !== 12) throw new Error("recovery artifact iv must contain 12 bytes");
    const ciphertext = base64ToBytes(
      envelope.ciphertext,
      "recovery artifact ciphertext"
    );
    const plaintext = await cryptoImpl.subtle.decrypt({
      name: "AES-GCM",
      iv,
      additionalData: identityBytes(identity)
    }, encryptionKey, ciphertext);
    const payload = JSON.parse(textDecoder.decode(plaintext));
    assertExactKeys(
      payload,
      ["version", "identity", "artifact"],
      "encrypted recovery artifact payload"
    );
    if (payload.version !== encryptedRecoveryArtifactStoreVersion) {
      throw new Error("encrypted recovery artifact payload version does not match");
    }
    assertExactKeys(
      payload.identity,
      ["profileId", "owner", "key"],
      "encrypted recovery artifact payload identity"
    );
    if (!identityMatches(payload.identity, identity)) {
      throw new Error("encrypted recovery artifact payload identity does not match");
    }
    if (!plainObject(payload.artifact)) {
      throw new Error("encrypted recovery artifact payload must contain an object");
    }
    return payload.artifact;
  } catch (error) {
    throw corruptionError(error);
  }
}

export class EncryptedRecoveryArtifactStore {
  static async open({
    storage = globalThis.localStorage,
    cryptoImpl = globalThis.crypto,
    locks = globalThis.navigator?.locks,
    key,
    profileId,
    owner,
    keyMaterial
  } = {}) {
    if (!storage
      || typeof storage.getItem !== "function"
      || typeof storage.setItem !== "function"
      || typeof storage.removeItem !== "function") {
      throw new Error("localStorage-compatible storage is required");
    }
    if (!cryptoImpl?.subtle || typeof cryptoImpl.getRandomValues !== "function") {
      throw new Error("Web Crypto is required for encrypted recovery artifacts");
    }
    if (typeof locks?.request !== "function") {
      throw new Error("Web Locks API is required for encrypted recovery artifacts");
    }
    const identity = normalizedIdentity({ profileId, owner, key });
    const encryptionKey = await deriveEncryptionKey({
      cryptoImpl,
      keyMaterial,
      identity
    });
    return new EncryptedRecoveryArtifactStore({
      storage,
      cryptoImpl,
      locks,
      identity,
      encryptionKey
    });
  }

  constructor({ storage, cryptoImpl, locks, identity, encryptionKey }) {
    this.storage = storage;
    this.cryptoImpl = cryptoImpl;
    this.locks = locks;
    this.identity = identity;
    this.encryptionKey = encryptionKey;
    this.lockName = `clairveil:v0.3.1:encrypted-recovery-artifact:${JSON.stringify(identity)}`;
  }

  withLock(callback) {
    return this.locks.request(this.lockName, { mode: "exclusive" }, callback);
  }

  async #loadUnlocked() {
    const raw = this.storage.getItem(this.identity.key);
    if (!raw) return null;
    return decryptArtifact({
      cryptoImpl: this.cryptoImpl,
      encryptionKey: this.encryptionKey,
      identity: this.identity,
      raw
    });
  }

  async #saveUnlocked(artifact, beforeCommit) {
    const normalized = normalizeArtifact(artifact);
    const iv = this.cryptoImpl.getRandomValues(new Uint8Array(12));
    const payload = textEncoder.encode(JSON.stringify({
      version: encryptedRecoveryArtifactStoreVersion,
      identity: this.identity,
      artifact: normalized
    }));
    const ciphertext = await this.cryptoImpl.subtle.encrypt({
      name: "AES-GCM",
      iv,
      additionalData: identityBytes(this.identity)
    }, this.encryptionKey, payload);
    beforeCommit?.();
    this.storage.setItem(this.identity.key, JSON.stringify({
      version: encryptedRecoveryArtifactStoreVersion,
      identity: this.identity,
      iv: bytesToBase64(iv),
      ciphertext: bytesToBase64(new Uint8Array(ciphertext))
    }));
    return normalized;
  }

  async load() {
    return this.withLock(() => this.#loadUnlocked());
  }

  async save(artifact, { beforeCommit } = {}) {
    assertBeforeCommit(beforeCommit);
    const normalized = normalizeArtifact(artifact);
    return this.withLock(() => this.#saveUnlocked(normalized, beforeCommit));
  }

  async clear({ beforeCommit } = {}) {
    assertBeforeCommit(beforeCommit);
    await this.withLock(() => {
      beforeCommit?.();
      this.storage.removeItem(this.identity.key);
    });
  }

  async update(updater, { beforeCommit } = {}) {
    assertUpdater(updater);
    assertBeforeCommit(beforeCommit);
    return this.withLock(async () => {
      const current = await this.#loadUnlocked();
      const candidate = await updater(current == null ? null : normalizeArtifact(current));
      if (candidate === undefined) {
        return { changed: false, previous: current, artifact: current };
      }
      if (candidate === null) {
        if (current === null) {
          return { changed: false, previous: null, artifact: null };
        }
        beforeCommit?.();
        this.storage.removeItem(this.identity.key);
        return { changed: true, previous: current, artifact: null };
      }
      const artifact = await this.#saveUnlocked(candidate, beforeCommit);
      return { changed: true, previous: current, artifact };
    });
  }

  async clearIf(predicate, { beforeCommit } = {}) {
    assertPredicate(predicate);
    return this.update(async current => (
      current !== null && await predicate(current) ? null : undefined
    ), { beforeCommit });
  }
}

export { encryptedRecoveryArtifactStoreVersion };
