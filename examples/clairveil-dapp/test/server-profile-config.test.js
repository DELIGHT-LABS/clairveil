import test from "node:test";
import assert from "node:assert/strict";

import {
  normalizeConfiguredTransport,
  resolveProfileDenom,
} from "../server-profile-config.js";

test("profile denom prefers transport-specific metadata over the global fallback", () => {
  assert.equal(resolveProfileDenom({
    transport: "cosmos",
    environment: {
      CLAIRVEIL_DENOM: "globalcoin",
      CLAIRVEIL_COSMOS_DENOM: "cosmoscoin",
    },
  }), "cosmoscoin");

  assert.equal(resolveProfileDenom({
    transport: "evm",
    environment: {
      CLAIRVEIL_DENOM: "globalcoin",
      CLAIRVEIL_EVM_NATIVE_DENOM: "nativecoin",
      CLAIRVEIL_EVM_DENOM: "evmcoin",
    },
  }), "evmcoin");
});

test("EVM native denom and then the global denom provide generic fallbacks", () => {
  assert.equal(resolveProfileDenom({
    transport: "evm",
    environment: {
      CLAIRVEIL_DENOM: "globalcoin",
      CLAIRVEIL_EVM_NATIVE_DENOM: "nativecoin",
    },
  }), "nativecoin");
  assert.equal(resolveProfileDenom({
    transport: "evm",
    environment: { CLAIRVEIL_DENOM: "globalcoin" },
  }), "globalcoin");
  assert.equal(resolveProfileDenom({
    transport: "cosmos",
    environment: { CLAIRVEIL_COSMOS_DENOM: "  " },
  }), "uclair");
});

test("profile transport is normalized and unknown transports fail closed", () => {
  assert.equal(normalizeConfiguredTransport(" EVM "), "evm");
  assert.throws(
    () => normalizeConfiguredTransport("custom"),
    /must be cosmos or evm/,
  );
});
