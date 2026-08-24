import test from "node:test";
import assert from "node:assert/strict";

import {
  loadStaticDappConfig,
  staticDappConfigPath,
} from "../public/dapp-config.js";

test("static DApp loads the same-origin deployment artifact used by the release gate", async () => {
  const expected = {
    schemaVersion: "clairveil-web-client-config-v1",
    serverBacked: false,
    activeChainProfileId: "cosmos",
    chainProfiles: [{ id: "cosmos" }],
  };
  const calls = [];
  const config = await loadStaticDappConfig({
    fetchImpl: async (url, options) => {
      calls.push({ url, options });
      return new Response(JSON.stringify(expected), {
        headers: { "content-type": "application/json; charset=utf-8" },
      });
    },
  });

  assert.deepEqual(config, expected);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, staticDappConfigPath);
  assert.equal(calls[0].options.redirect, "error");
  assert.equal(calls[0].options.credentials, "same-origin");
});

test("static DApp rejects a non-JSON deployment artifact", async () => {
  await assert.rejects(
    () => loadStaticDappConfig({
      fetchImpl: async () => new Response("<html></html>", {
        headers: { "content-type": "text/html" },
      }),
    }),
    /must return Content-Type: application\/json/,
  );
});
