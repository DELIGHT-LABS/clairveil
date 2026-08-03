import {
  createBrowserReservationStore,
  createNoteReservationManager
} from "clairveiljs/reservation";

const reservationStateVersion = "clairveil-encrypted-reservation-state-v1";
const reservationStateInfo = new TextEncoder().encode("clairveil/reservation-state/v1");

async function deriveReservationStateKey({ cryptoImpl, keyMaterial, namespace }) {
  const bytes = keyMaterial instanceof Uint8Array ? keyMaterial : new Uint8Array(keyMaterial || []);
  if (!bytes.length) throw new Error("reservation encryption key material is required");
  const material = await cryptoImpl.subtle.importKey("raw", bytes, "HKDF", false, ["deriveKey"]);
  return cryptoImpl.subtle.deriveKey({
    name: "HKDF",
    hash: "SHA-256",
    salt: new TextEncoder().encode(namespace),
    info: reservationStateInfo
  }, material, { name: "AES-GCM", length: 256 }, false, ["encrypt", "decrypt"]);
}

function reservationStateError(cause) {
  const error = new Error("Encrypted note reservation state cannot be decrypted. Manual recovery is required.", { cause });
  error.code = "RESERVATION_STATE_CORRUPT";
  return error;
}

export async function createEncryptedBrowserReservationManager({
  namespace,
  ownerKeyId,
  indexKey,
  keyMaterial = indexKey,
  leaseOwner,
  cryptoImpl = globalThis.crypto,
  indexedDB = globalThis.indexedDB,
  locks = globalThis.navigator?.locks
} = {}) {
  if (!cryptoImpl?.subtle || typeof cryptoImpl.getRandomValues !== "function") {
    throw new Error("Web Crypto is required for encrypted note reservations");
  }
  const resolvedNamespace = String(namespace || "").trim();
  if (!resolvedNamespace) throw new Error("reservation namespace is required");
  const encryptionKey = await deriveReservationStateKey({
    cryptoImpl,
    keyMaterial,
    namespace: resolvedNamespace
  });
  const encodeState = async state => {
    const iv = cryptoImpl.getRandomValues(new Uint8Array(12));
    const plaintext = new TextEncoder().encode(JSON.stringify(state));
    const ciphertext = await cryptoImpl.subtle.encrypt({ name: "AES-GCM", iv }, encryptionKey, plaintext);
    return {
      version: reservationStateVersion,
      iv: [...iv],
      ciphertext: [...new Uint8Array(ciphertext)]
    };
  };
  const decodeState = async value => {
    try {
      if (value?.version !== reservationStateVersion || !Array.isArray(value.iv) || !Array.isArray(value.ciphertext)) {
        throw new Error("unsupported encrypted reservation state");
      }
      const plaintext = await cryptoImpl.subtle.decrypt({
        name: "AES-GCM",
        iv: new Uint8Array(value.iv)
      }, encryptionKey, new Uint8Array(value.ciphertext));
      return JSON.parse(new TextDecoder().decode(plaintext));
    } catch (error) {
      throw reservationStateError(error);
    }
  };
  const store = createBrowserReservationStore({
    dbName: "clairveil-dapp-reservations-v1",
    namespace: resolvedNamespace,
    indexedDB,
    locks,
    requireLocks: true,
    encodeState,
    decodeState
  });
  return createNoteReservationManager({
    store,
    ownerKeyId,
    indexKey,
    leaseOwner
  });
}

export { reservationStateVersion };
