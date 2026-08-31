import assert from "node:assert/strict";
import test from "node:test";

import {
  evmBatchAuthorizationAvailable,
  evmBatchAuthorizationKinds,
  randomEvmAuthorizationNonce,
  selfSubmittedEvmBatchAuthorization
} from "../public/evm-batch-authorization.js";

const profile = {
  transport: "evm",
  evmAuthorizationProfile: {
    typedDataDomain: { name: "Target Privacy", version: "1" },
    supportedAuthorizationKinds: [1, "0x2", 3]
  }
};

test("EVM batch authorization is enabled only by an explicit typed-data profile", () => {
  assert.equal(evmBatchAuthorizationAvailable(profile), true);
  assert.deepEqual(evmBatchAuthorizationKinds(profile), [1, 2, 3]);
  assert.equal(evmBatchAuthorizationAvailable({ transport: "evm" }), false);
  const unrestrictedKinds = evmBatchAuthorizationKinds({
    transport: "evm",
    evmAuthorizationProfile: { typedDataDomain: { name: "Target Privacy" } }
  });
  assert.equal(unrestrictedKinds.length, 256);
  assert.equal(unrestrictedKinds[0], 0);
  assert.equal(unrestrictedKinds[255], 255);
  assert.deepEqual(evmBatchAuthorizationKinds({
    transport: "evm",
    evmAuthorizationProfile: {
      typedDataDomain: { name: "Target Privacy" },
      supportedAuthorizationKinds: []
    }
  }), []);
  assert.equal(evmBatchAuthorizationAvailable({
    transport: "cosmos",
    evmAuthorizationProfile: profile.evmAuthorizationProfile
  }), false);
});

test("self-submitted authorization binds the connected account as sender and executor", () => {
  const authorization = selfSubmittedEvmBatchAuthorization({
    profile,
    account: "0x1111111111111111111111111111111111111111",
    authorizationKind: 1,
    nonce: "01".repeat(16),
    deadline: 4_102_448_500
  });
  assert.deepEqual(authorization, {
    effectiveSender: "0x1111111111111111111111111111111111111111",
    executor: "0x1111111111111111111111111111111111111111",
    nonce: BigInt(`0x${"01".repeat(16)}`).toString(),
    deadline: "4102448500",
    authorizationKind: 1
  });
  assert.equal(selfSubmittedEvmBatchAuthorization({
    profile,
    account: authorization.effectiveSender,
    authorizationKind: 1,
    nonce: "0x01",
    deadline: 1
  }).nonce, "1");
  assert.throws(() => selfSubmittedEvmBatchAuthorization({
    profile,
    account: authorization.effectiveSender,
    authorizationKind: 4,
    nonce: "01",
    deadline: 1
  }), /not allowed/);
});

test("authorization nonce uses secure browser randomness", () => {
  const nonce = randomEvmAuthorizationNonce({
    getRandomValues(words) {
      words.set([1, 2, 3, 4]);
      return words;
    }
  });
  assert.equal(nonce, "00000001000000020000000300000004");
  assert.throws(() => randomEvmAuthorizationNonce(null), /Secure browser randomness/);
});
