const encryptedOperationStoreVersion = "clairveil-encrypted-operation-store-v2";
const encryptionInfo = new TextEncoder().encode("clairveil/encrypted-operation-store/v2");
export const relayWithdrawRecoveryVersion = "clairveil-relay-withdraw-recovery-v2";

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

function normalizedRelayRecoveryPersistenceId(value) {
  const normalized = String(value || "").trim().toLowerCase();
  if (!/^[0-9a-f]{64}$/.test(normalized)) {
    throw new Error("relay recovery persistence ID must be a canonical payload hash");
  }
  return normalized;
}

function normalizedRelayReservationIDs(value) {
  if (!Array.isArray(value)) throw new Error("relay recovery reservationIds must be an array");
  const normalized = value.map(id => String(id || "").trim()).filter(Boolean);
  if (!normalized.length || new Set(normalized).size !== normalized.length) {
    throw new Error("relay recovery reservationIds must be non-empty and unique");
  }
  return normalized;
}

function relayPayloadFromState(state) {
  return state?.handoff?.request?.payload || null;
}

export function relayWithdrawRecoveryMetadata(state = {}) {
  const payload = relayPayloadFromState(state);
  const reservationIds = normalizedRelayReservationIDs(state.reservationIds || []);
  const payloadHash = String(state.payloadHash || payload?.payload_hash || "").trim().toLowerCase();
  const expiresAtUnix = Number(state.expiresAtUnix ?? payload?.expires_at_unix ?? 0);
  const rawTxHash = String(state.txHash || "").trim();
  const txHash = rawTxHash.replace(/^0x/i, "").toLowerCase();
  const externalHandoff = Boolean(state.externalHandoff);
  const resultStatus = String(state.resultStatus || "manual-review").trim();
  if (!/^[0-9a-f]{64}$/.test(payloadHash)) {
    throw new Error("relay recovery payloadHash must be canonical 32-byte hex");
  }
  if (!Number.isSafeInteger(expiresAtUnix) || expiresAtUnix <= 0) {
    throw new Error("relay recovery expiresAtUnix must be a positive safe integer");
  }
  if (txHash && !/^[0-9a-f]{64}$/.test(txHash)) {
    throw new Error("relay recovery txHash must be empty or canonical 32-byte hex");
  }
  if (!/^[a-z0-9-]{1,64}$/.test(resultStatus)) {
    throw new Error("relay recovery resultStatus must be a stable status code");
  }
  return {
    reservationIds,
    payloadHash,
    expiresAtUnix,
    txHash,
    externalHandoff,
    durableNoBroadcast: Boolean(state.durableNoBroadcast) && !externalHandoff && !txHash,
    resultStatus
  };
}

export function relayWithdrawRecoveryPersistenceId(state = {}) {
  const metadata = state?.relayWithdraw || state;
  return normalizedRelayRecoveryPersistenceId(metadata?.payloadHash);
}

export function restoreRelayWithdrawRecoveryMetadata(metadata = {}) {
  if (!metadata || typeof metadata !== "object" || Array.isArray(metadata)) {
    throw new Error("relay recovery metadata must be an object");
  }
  const allowedFields = new Set([
    "reservationIds",
    "payloadHash",
    "expiresAtUnix",
    "txHash",
    "externalHandoff",
    "durableNoBroadcast",
    "resultStatus"
  ]);
  const unsupportedField = Object.keys(metadata).find(key => !allowedFields.has(key));
  if (unsupportedField) {
    throw new Error(`relay recovery metadata field ${unsupportedField} is not supported`);
  }
  if (!Array.isArray(metadata.reservationIds)
    || metadata.reservationIds.some(id => typeof id !== "string" || !id || id !== id.trim())) {
    throw new Error("relay recovery reservationIds must contain canonical non-empty strings");
  }
  if (typeof metadata.payloadHash !== "string" || !/^[0-9a-f]{64}$/.test(metadata.payloadHash)) {
    throw new Error("relay recovery payloadHash must be canonical 32-byte hex");
  }
  if (typeof metadata.expiresAtUnix !== "number"
    || !Number.isSafeInteger(metadata.expiresAtUnix)
    || metadata.expiresAtUnix <= 0) {
    throw new Error("relay recovery expiresAtUnix must be a positive safe integer");
  }
  if (typeof metadata.txHash !== "string"
    || (metadata.txHash && !/^[0-9a-f]{64}$/.test(metadata.txHash))) {
    throw new Error("relay recovery txHash must be empty or canonical 32-byte hex");
  }
  if (typeof metadata.externalHandoff !== "boolean"
    || typeof metadata.durableNoBroadcast !== "boolean") {
    throw new Error("relay recovery handoff flags must be booleans");
  }
  if (typeof metadata.resultStatus !== "string"
    || !/^[a-z0-9-]{1,64}$/.test(metadata.resultStatus)) {
    throw new Error("relay recovery resultStatus must be a stable status code");
  }
  const restored = relayWithdrawRecoveryMetadata(metadata);
  const resultMessage = restored.externalHandoff || restored.txHash
    ? "Restored relay metadata only · raw payload was not persisted; reconcile chain evidence and do not resubmit"
    : restored.durableNoBroadcast
      ? "Prepared relay payload was not persisted · resolve the locked reservation, then prepare a new payload"
      : "Restored relay metadata requires manual review before preparing another payload";
  return {
    handoff: null,
    json: "",
    ...restored,
    submittedBy: "",
    payloadUnavailable: true,
    resultStatus: "manual-review",
    resultMessage
  };
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
    locks = globalThis.navigator?.locks,
    requireLocks = true,
    key,
    keyMaterial,
    namespace
  } = {}) {
    if (!storage) throw new Error("localStorage-compatible storage is required");
    if (!cryptoImpl?.subtle || typeof cryptoImpl.getRandomValues !== "function") {
      throw new Error("Web Crypto is required for encrypted operation recovery");
    }
    if (!String(key || "").trim()) throw new Error("encrypted operation recovery key is required");
    if (requireLocks && typeof locks?.request !== "function") {
      throw new Error("Web Locks API is required for encrypted operation recovery");
    }
    const encryptionKey = await deriveEncryptionKey({
      cryptoImpl,
      keyMaterial,
      namespace: String(namespace || key)
    });
    return new EncryptedLocalStorageOperationStore({
      storage,
      cryptoImpl,
      locks,
      requireLocks,
      key,
      encryptionKey
    });
  }

  constructor({ storage, cryptoImpl, locks, requireLocks, key, encryptionKey }) {
    this.storage = storage;
    this.cryptoImpl = cryptoImpl;
    this.locks = locks;
    this.requireLocks = requireLocks;
    this.key = key;
    this.encryptionKey = encryptionKey;
  }

  recordKey(persistenceId) {
    return `${this.key}:${normalizedRelayRecoveryPersistenceId(persistenceId)}`;
  }

  async withLock(persistenceId, callback) {
    const lockName = `clairveil:v0.3.1:operation-recovery:${this.recordKey(persistenceId)}`;
    if (typeof this.locks?.request === "function") {
      return this.locks.request(lockName, { mode: "exclusive" }, callback);
    }
    if (this.requireLocks) {
      throw new Error("Web Locks API is required for encrypted operation recovery");
    }
    return callback();
  }

  async loadUnlocked(persistenceId) {
    const raw = this.storage.getItem(this.recordKey(persistenceId));
    if (!raw) return null;
    try {
      const envelope = JSON.parse(raw);
      if (envelope?.version !== encryptedOperationStoreVersion) {
        throw new Error("unsupported encrypted operation recovery version");
      }
      const plaintext = await this.cryptoImpl.subtle.decrypt({
        name: "AES-GCM",
        iv: base64ToBytes(envelope.iv, "operation recovery iv"),
        additionalData: new TextEncoder().encode(persistenceId)
      }, this.encryptionKey, base64ToBytes(envelope.ciphertext, "operation recovery ciphertext"));
      const state = JSON.parse(new TextDecoder().decode(plaintext));
      if (relayWithdrawRecoveryPersistenceId(state) !== persistenceId) {
        throw new Error("relay recovery persistence identity does not match its payload");
      }
      return state;
    } catch (error) {
      throw operationStateError(error);
    }
  }

  async load(persistenceId) {
    const normalizedId = normalizedRelayRecoveryPersistenceId(persistenceId);
    return this.withLock(normalizedId, () => this.loadUnlocked(normalizedId));
  }

  async loadAll() {
    const prefix = `${this.key}:`;
    const persistenceIds = [];
    for (let index = 0; index < Number(this.storage.length || 0); index += 1) {
      const candidate = this.storage.key(index);
      if (!candidate?.startsWith(prefix)) continue;
      persistenceIds.push(normalizedRelayRecoveryPersistenceId(candidate.slice(prefix.length)));
    }
    const states = await Promise.all(
      [...new Set(persistenceIds)].sort().map(persistenceId => this.load(persistenceId))
    );
    return states.filter(Boolean);
  }

  async save(state, { beforeCommit } = {}) {
    if (beforeCommit !== undefined && typeof beforeCommit !== "function") {
      throw new Error("encrypted operation recovery beforeCommit must be a function");
    }
    const persistenceId = relayWithdrawRecoveryPersistenceId(state);
    await this.withLock(persistenceId, async () => {
      const iv = this.cryptoImpl.getRandomValues(new Uint8Array(12));
      const plaintext = new TextEncoder().encode(JSON.stringify(state, (_key, value) => (
        typeof value === "bigint" ? value.toString() : value
      )));
      const ciphertext = await this.cryptoImpl.subtle.encrypt(
        {
          name: "AES-GCM",
          iv,
          additionalData: new TextEncoder().encode(persistenceId)
        },
        this.encryptionKey,
        plaintext
      );
      beforeCommit?.();
      this.storage.setItem(this.recordKey(persistenceId), JSON.stringify({
        version: encryptedOperationStoreVersion,
        iv: bytesToBase64(iv),
        ciphertext: bytesToBase64(new Uint8Array(ciphertext))
      }));
    });
    return persistenceId;
  }

  async clear(persistenceId, { beforeCommit } = {}) {
    if (beforeCommit !== undefined && typeof beforeCommit !== "function") {
      throw new Error("encrypted operation recovery beforeCommit must be a function");
    }
    const normalizedId = normalizedRelayRecoveryPersistenceId(persistenceId);
    await this.withLock(normalizedId, () => {
      beforeCommit?.();
      this.storage.removeItem(this.recordKey(normalizedId));
    });
  }
}

export { encryptedOperationStoreVersion };
