import {
  activeReservationStatuses,
  createBrowserReservationStore,
  createNoteReservationManager,
  operationStatuses
} from "clairveiljs/reservation";
import {
  assessReservationRecovery,
  groupReservationOperations
} from "./reservation-recovery.js";

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

const unresolvedOperationStatuses = new Set([
  operationStatuses.ManualReview,
  operationStatuses.ConflictSpent
]);
const activeReservationStatusSet = new Set(activeReservationStatuses);

export function reservationHasUnresolvedOperationEvidence(record) {
  const metadata = record?.metadata || {};
  const evidenceRequired = metadata.operation_success_evidence_required === true
    || metadata.operationSuccessEvidenceRequired === true
    || record?.operation_success_evidence_required === true
    || record?.operationSuccessEvidenceRequired === true;
  const operationStatus = metadata.operation_status
    || metadata.operationStatus
    || record?.operation_status
    || record?.operationStatus
    || "";
  return evidenceRequired && unresolvedOperationStatuses.has(operationStatus);
}

export function reservationBlocksReviewedReset(record) {
  return activeReservationStatusSet.has(record?.status)
    || reservationHasUnresolvedOperationEvidence(record);
}

function canonicalJson(value) {
  if (Array.isArray(value)) return value.map(canonicalJson);
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.keys(value).sort().map(key => [key, canonicalJson(value[key])])
  );
}

function canonicalReservationStateSnapshot(state) {
  const reservations = [...state.reservations]
    .sort((left, right) => String(left?.reservation_id || "")
      .localeCompare(String(right?.reservation_id || "")));
  return JSON.stringify(canonicalJson({ ...state, reservations }));
}

function assertFreshGenesisReservations(manager, reservations) {
  if (!Array.isArray(reservations) || !reservations.length) {
    throw new Error("Fresh-genesis reset requires an exact non-empty reservation snapshot");
  }
  const ownerKeyId = String(manager?.ownerKeyId || "");
  const reservationIDs = new Set();
  for (const record of reservations) {
    const reservationID = String(record?.reservation_id || "");
    if (!reservationID || reservationIDs.has(reservationID) || record?.owner_key_id !== ownerKeyId) {
      throw new Error("Fresh-genesis reset reservation snapshot is malformed or belongs to another owner");
    }
    reservationIDs.add(reservationID);
  }
  const active = reservations.filter(record => activeReservationStatusSet.has(record?.status));
  const operations = groupReservationOperations(active);
  const groupedCount = operations.reduce((count, operation) => count + operation.records.length, 0);
  const now = manager?.now?.();
  const nowMs = now instanceof Date ? now.getTime() : Number.NaN;
  if (!active.length
    || groupedCount !== active.length
    || !Number.isFinite(nowMs)
    || operations.some(operation => assessReservationRecovery(operation.records, {
      leaseOwner: manager.leaseOwner,
      nowMs
    }).action !== "review-replan")) {
    throw new Error("Fresh-genesis reset requires only stale no-broadcast, no-relay reservations");
  }
}

async function readEncryptedReservationState(db, store, label) {
  const transaction = db.transaction("states", "readonly");
  const completion = new Promise((resolve, reject) => {
    transaction.oncomplete = resolve;
    transaction.onerror = () => reject(transaction.error || new Error(`${label} read failed`));
    transaction.onabort = () => reject(transaction.error || new Error(`${label} read was aborted`));
  });
  const request = transaction.objectStore("states").get(store.namespace);
  const stored = await new Promise((resolve, reject) => {
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error || new Error(`${label} read failed`));
  });
  await completion;
  return stored === undefined
    ? { reservations: [] }
    : store.decodeState(stored);
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

/**
 * Destructive escape hatch for a confirmed fresh local genesis or a reviewed
 * account fresh-state reset. Callers must establish the corresponding chain
 * evidence and explicit wallet-owner approval; normal recovery must use
 * reservation CAS methods.
 */
export async function resetEncryptedBrowserReservationState(manager, {
  confirmedFreshLocalGenesis = false,
  confirmedReviewedFreshStateReset = false,
  expectedReservationState,
  afterReset
} = {}) {
  if (confirmedFreshLocalGenesis !== true && confirmedReviewedFreshStateReset !== true) {
    throw new Error("Fresh-genesis reservation reset requires an explicit local-genesis confirmation capability");
  }
  if (afterReset !== undefined && typeof afterReset !== "function") {
    throw new Error("Reservation reset afterReset must be a function");
  }
  const store = manager?.store;
  if (!store
    || typeof store.withMutationLock !== "function"
    || typeof store.db !== "function"
    || !String(store.namespace || "").trim()) {
    throw new Error("Encrypted reservation store does not support fresh-genesis reset");
  }
  let expectedSnapshot = "";
  if (confirmedFreshLocalGenesis === true) {
    assertFreshGenesisReservations(manager, expectedReservationState?.reservations);
    expectedSnapshot = canonicalReservationStateSnapshot(expectedReservationState);
  }
  await store.withMutationLock(async () => {
    const db = await store.db();
    if (!db || typeof db.transaction !== "function") {
      throw new Error("Encrypted reservation store database is unavailable");
    }
    if (confirmedFreshLocalGenesis === true || confirmedReviewedFreshStateReset === true) {
      const decoded = await readEncryptedReservationState(
        db,
        store,
        confirmedFreshLocalGenesis ? "Fresh-genesis reservation" : "Reviewed fresh-state reservation"
      );
      const reservations = Array.isArray(decoded?.reservations)
        ? decoded.reservations
        : null;
      if (!reservations) {
        throw new Error("Encrypted reservation state contains an invalid reservation list");
      }
      if (confirmedFreshLocalGenesis === true) {
        if (canonicalReservationStateSnapshot(decoded) !== expectedSnapshot) {
          const error = new Error("Reservation state changed after fresh-genesis reset approval");
          error.code = "FRESH_GENESIS_RESERVATION_STATE_CHANGED";
          throw error;
        }
        assertFreshGenesisReservations(manager, reservations);
      }
      if (confirmedReviewedFreshStateReset === true
        && reservations.some(reservationBlocksReviewedReset)) {
        throw new Error("Reviewed fresh-state reset requires zero active or unresolved reservations");
      }
    }
    const transaction = db.transaction("states", "readwrite");
    const completion = new Promise((resolve, reject) => {
      transaction.oncomplete = () => resolve();
      transaction.onerror = () => reject(transaction.error || new Error("Fresh-genesis reservation reset failed"));
      transaction.onabort = () => reject(transaction.error || new Error("Fresh-genesis reservation reset was aborted"));
    });
    transaction.objectStore("states").delete(store.namespace);
    await completion;
    await afterReset?.();
  });
}

export { reservationStateVersion };
