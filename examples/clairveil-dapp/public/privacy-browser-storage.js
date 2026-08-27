function unavailable(message) {
  return Object.freeze({
    available: false,
    message,
    storage: null
  });
}

const STORAGE_CAPABILITY_KEY = "clairveil:v0.3.1:storage-capability";
const STORAGE_CAPABILITY_VALUE = "clairveil-storage-capability-probe";

function safely(read) {
  try {
    return read();
  } catch {
    return undefined;
  }
}

function succeeds(action) {
  try {
    action();
    return true;
  } catch {
    return false;
  }
}

function probeLocalStorage(storage) {
  const getItem = safely(() => storage.getItem);
  const setItem = safely(() => storage.setItem);
  const removeItem = safely(() => storage.removeItem);
  if (![getItem, setItem, removeItem].every(method => typeof method === "function")) return false;

  const previousValue = safely(() => getItem.call(storage, STORAGE_CAPABILITY_KEY));
  if (previousValue === undefined) return false;

  let probeSucceeded = false;
  succeeds(() => {
    setItem.call(storage, STORAGE_CAPABILITY_KEY, STORAGE_CAPABILITY_VALUE);
    probeSucceeded = getItem.call(storage, STORAGE_CAPABILITY_KEY) === STORAGE_CAPABILITY_VALUE;
  });

  const restore = () => succeeds(() => previousValue === null
    ? removeItem.call(storage, STORAGE_CAPABILITY_KEY)
    : setItem.call(storage, STORAGE_CAPABILITY_KEY, previousValue));
  const restored = restore();
  if (!restored) {
    restore(); // Best-effort retry after a transient cleanup failure.
  }
  return probeSucceeded && restored;
}

export function privacyBrowserStorageCapability(environment = globalThis) {
  const storage = safely(() => environment?.localStorage);
  if (!storage || !probeLocalStorage(storage)) {
    return unavailable("localStorage is required for encrypted Clairveil browser storage");
  }

  const indexedDB = safely(() => environment?.indexedDB);
  if (!indexedDB) {
    return unavailable("IndexedDB is required for encrypted Clairveil browser storage");
  }

  const cryptoApi = safely(() => environment?.crypto);
  const subtleCrypto = safely(() => cryptoApi?.subtle);
  const getRandomValues = safely(() => cryptoApi?.getRandomValues);
  if (!subtleCrypto || typeof getRandomValues !== "function") {
    return unavailable("Web Crypto is required for encrypted Clairveil browser storage");
  }

  const navigatorApi = safely(() => environment?.navigator);
  const locksApi = safely(() => navigatorApi?.locks);
  const requestLock = safely(() => locksApi?.request);
  if (typeof requestLock !== "function") {
    return unavailable("Web Locks API is required for encrypted Clairveil browser storage");
  }
  return Object.freeze({
    available: true,
    message: "",
    storage
  });
}

export function requirePrivacyBrowserStorage(environment = globalThis) {
  const capability = privacyBrowserStorageCapability(environment);
  if (!capability.available) {
    const error = new Error(capability.message);
    error.code = "PRIVACY_BROWSER_STORAGE_UNAVAILABLE";
    throw error;
  }
  return capability.storage;
}
