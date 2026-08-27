import assert from "node:assert/strict";
import test from "node:test";

import {
  privacyBrowserStorageCapability,
  requirePrivacyBrowserStorage
} from "../public/privacy-browser-storage.js";

function supportedEnvironment() {
  const values = new Map();
  const storage = {
    getItem(key) {
      return values.has(String(key)) ? values.get(String(key)) : null;
    },
    setItem(key, value) {
      values.set(String(key), String(value));
    },
    removeItem(key) {
      values.delete(String(key));
    }
  };
  return {
    storage,
    values,
    environment: {
      localStorage: storage,
      indexedDB: {},
      crypto: {
        subtle: {},
        getRandomValues() {}
      },
      navigator: {
        locks: {
          request() {}
        }
      }
    }
  };
}

test("privacy browser storage capability accepts the complete encrypted-storage baseline", () => {
  const { environment, storage } = supportedEnvironment();
  assert.deepEqual(privacyBrowserStorageCapability(environment), {
    available: true,
    message: "",
    storage
  });
  assert.equal(requirePrivacyBrowserStorage(environment), storage);
});

test("privacy browser storage capability fails closed for every required browser API", () => {
  const cases = [
    ["localStorage", /localStorage is required/],
    ["indexedDB", /IndexedDB is required/],
    ["crypto", /Web Crypto is required/],
    ["locks", /Web Locks API is required/]
  ];

  for (const [missing, message] of cases) {
    const { environment } = supportedEnvironment();
    if (missing === "locks") {
      delete environment.navigator.locks;
    } else {
      delete environment[missing];
    }
    const capability = privacyBrowserStorageCapability(environment);
    assert.equal(capability.available, false, missing);
    assert.match(capability.message, message, missing);
    assert.throws(
      () => requirePrivacyBrowserStorage(environment),
      error => error?.code === "PRIVACY_BROWSER_STORAGE_UNAVAILABLE" && message.test(error.message),
      missing
    );
  }
});

test("privacy browser storage capability handles a blocked localStorage getter", () => {
  const { environment } = supportedEnvironment();
  Object.defineProperty(environment, "localStorage", {
    get() {
      throw new DOMException("blocked", "SecurityError");
    }
  });
  assert.equal(privacyBrowserStorageCapability(environment).available, false);
  assert.throws(
    () => requirePrivacyBrowserStorage(environment),
    error => error?.code === "PRIVACY_BROWSER_STORAGE_UNAVAILABLE"
  );
});

test("privacy browser storage capability handles blocked localStorage reads", () => {
  const { environment } = supportedEnvironment();
  environment.localStorage.getItem = () => {
    throw new DOMException("blocked", "SecurityError");
  };
  assert.equal(privacyBrowserStorageCapability(environment).available, false);
  assert.throws(
    () => requirePrivacyBrowserStorage(environment),
    error => error?.code === "PRIVACY_BROWSER_STORAGE_UNAVAILABLE"
  );
});

test("privacy browser storage capability fails closed when localStorage writes are blocked", () => {
  const { environment, storage, values } = supportedEnvironment();
  const setItem = storage.setItem;
  let setCalls = 0;
  storage.setItem = function setItemWithPostWriteFailure(key, value) {
    setCalls += 1;
    setItem.call(this, key, value);
    if (setCalls === 1) {
      throw new DOMException("blocked", "SecurityError");
    }
  };

  const capability = privacyBrowserStorageCapability(environment);
  assert.equal(capability.available, false);
  assert.equal(values.size, 0);
});

test("privacy browser storage capability restores an existing value after a partial write failure", () => {
  const { environment, storage, values } = supportedEnvironment();
  const probeKey = "clairveil:v0.3.1:storage-capability";
  values.set(probeKey, "existing-value");
  const setItem = storage.setItem;
  let setCalls = 0;
  storage.setItem = function setItemWithPostWriteFailure(key, value) {
    setCalls += 1;
    setItem.call(this, key, value);
    if (setCalls === 1) {
      throw new DOMException("blocked", "SecurityError");
    }
  };

  const capability = privacyBrowserStorageCapability(environment);
  assert.equal(capability.available, false);
  assert.equal(values.get(probeKey), "existing-value");
});

test("privacy browser storage capability retries cleanup after localStorage removal fails", () => {
  const { environment, storage, values } = supportedEnvironment();
  const removeItem = storage.removeItem;
  let removeCalls = 0;
  storage.removeItem = function removeItemWithTransientFailure(key) {
    removeCalls += 1;
    if (removeCalls === 1) {
      throw new DOMException("blocked", "SecurityError");
    }
    return removeItem.call(this, key);
  };

  const capability = privacyBrowserStorageCapability(environment);
  assert.equal(capability.available, false);
  assert.equal(removeCalls, 2);
  assert.equal(values.size, 0);
});

test("privacy browser storage capability preserves an existing probe-key value", () => {
  const { environment, values } = supportedEnvironment();
  const probeKey = "clairveil:v0.3.1:storage-capability";
  values.set(probeKey, "existing-value");

  assert.equal(privacyBrowserStorageCapability(environment).available, true);
  assert.equal(values.get(probeKey), "existing-value");
});

test("privacy browser storage capability handles throwing required API getters", () => {
  const cases = [
    {
      name: "indexedDB",
      message: /IndexedDB is required/,
      install(environment) {
        Object.defineProperty(environment, "indexedDB", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    },
    {
      name: "crypto",
      message: /Web Crypto is required/,
      install(environment) {
        Object.defineProperty(environment, "crypto", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    },
    {
      name: "crypto.subtle",
      message: /Web Crypto is required/,
      install(environment) {
        Object.defineProperty(environment.crypto, "subtle", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    },
    {
      name: "navigator",
      message: /Web Locks API is required/,
      install(environment) {
        Object.defineProperty(environment, "navigator", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    },
    {
      name: "navigator.locks",
      message: /Web Locks API is required/,
      install(environment) {
        Object.defineProperty(environment.navigator, "locks", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    },
    {
      name: "navigator.locks.request",
      message: /Web Locks API is required/,
      install(environment) {
        Object.defineProperty(environment.navigator.locks, "request", {
          get() {
            throw new DOMException("blocked", "SecurityError");
          }
        });
      }
    }
  ];

  for (const { name, message, install } of cases) {
    const { environment } = supportedEnvironment();
    install(environment);
    const capability = privacyBrowserStorageCapability(environment);
    assert.equal(capability.available, false, name);
    assert.match(capability.message, message, name);
  }
});
