import assert from "node:assert/strict";
import test from "node:test";

import { cosmosGasFeeAmount, deterministicCosmosFeeAmount } from "../public/cosmos-fee.js";

test("Cosmos fee multiplication uses exact decimal rational arithmetic", () => {
  assert.equal(cosmosGasFeeAmount("0.025", "8000000"), 200000n);
  assert.equal(cosmosGasFeeAmount("0.0000001", "5000001"), 1n);
  assert.equal(cosmosGasFeeAmount("1e-7", "10000000"), 1n);
});

test("Cosmos fee multiplication remains exact beyond Number safe integer range", () => {
  assert.equal(
    cosmosGasFeeAmount("123456789.123456789", "9007199254740993"),
    1111999898985515920324994n
  );
});

test("deterministic Cosmos fee amount preserves denom and canonical integer", () => {
  assert.deepEqual(
    deterministicCosmosFeeAmount({ gasPrice: "0.025", gasLimit: 5000000, denom: "aokrw" }),
    [{ amount: "125000", denom: "aokrw" }]
  );
});
