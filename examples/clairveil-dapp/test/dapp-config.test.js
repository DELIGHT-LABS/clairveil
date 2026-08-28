import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { validateClairveilWebClientConfig } from "clairveiljs/browser-dapp";

import {
  fetchBoundedDappJson,
  healthBootstrapFallbackAllowed,
  loadStaticDappConfig,
  loadServerDappHealth,
  staticDappConfigPath,
} from "../public/dapp-config.js";

test("static DApp loads the same-origin deployment artifact used by the release gate", async () => {
  const expected = {
    schemaVersion: "clairveil-web-client-config-v1",
    serverBacked: false,
    serverFeatures: { batchTransfer: false },
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
  assert.equal(calls[0].options.redirect, "manual");
  assert.equal(calls[0].options.credentials, "same-origin");
});

test("redirected health bootstrap fails closed instead of using static fallback", async () => {
  const redirected = await loadServerDappHealth({
    fetchImpl: async (_url, options) => {
      assert.equal(options.redirect, "manual");
      return new Response(null, {
        status: 302,
        headers: { location: "/dapp-config.json" },
      });
    },
  }).catch(error => error);

  assert.equal(redirected?.code, "DAPP_BOOTSTRAP_REDIRECTED");
  assert.equal(healthBootstrapFallbackAllowed(redirected), false);
});

test("health bootstrap rejects a response whose final URL differs from the requested route", async () => {
  const mismatched = await fetchBoundedDappJson("https://app.example/api/health", {
    fetchImpl: async () => {
      const response = new Response(JSON.stringify({ config: {} }), {
        headers: { "content-type": "application/json" },
      });
      Object.defineProperty(response, "url", {
        value: "https://app.example/dapp-config.json",
      });
      return response;
    },
  }).catch(error => error);

  assert.equal(mismatched?.code, "DAPP_BOOTSTRAP_REDIRECTED");
  assert.equal(healthBootstrapFallbackAllowed(mismatched), false);
});

test("checked-in static artifact satisfies the current v0.3.1 feature boundary", async () => {
  const artifact = JSON.parse(await readFile(
    new URL("../public/dapp-config.json", import.meta.url),
    "utf8",
  ));
  const config = await loadStaticDappConfig({
    fetchImpl: async () => new Response(JSON.stringify(artifact), {
      headers: { "content-type": "application/json" },
    }),
  });
  const validated = validateClairveilWebClientConfig(config);

  assert.deepEqual(validated.serverFeatures, {
    localTestMode: false,
    localSigners: false,
    faucet: false,
    depositProof: false,
    auditorAdmin: false,
    proverProxy: false,
    batchTransfer: false,
  });
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

test("browser bootstrap enforces its response-size bound before JSON parsing", async () => {
  await assert.rejects(
    () => fetchBoundedDappJson("/oversized.json", {
      maxBytes: 8,
      fetchImpl: async () => new Response(JSON.stringify({ value: "too large" }), {
        headers: { "content-type": "application/json" },
      }),
    }),
    error => error?.code === "DAPP_BOOTSTRAP_TOO_LARGE",
  );
});

test("browser bootstrap timeout covers the response body and permits static fallback", async () => {
  let error;
  try {
    await fetchBoundedDappJson("/slow.json", {
      timeoutMs: 5,
      fetchImpl: async (_url, { signal }) => new Response(new ReadableStream({
        start(controller) {
          controller.enqueue(new TextEncoder().encode("{"));
          signal.addEventListener("abort", () => controller.error(new DOMException("aborted", "AbortError")), { once: true });
        },
      }), {
        headers: { "content-type": "application/json" },
      }),
    });
  } catch (cause) {
    error = cause;
  }
  assert.equal(error?.code, "DAPP_BOOTSTRAP_TIMEOUT");
  assert.equal(healthBootstrapFallbackAllowed(error), true);
});

test("reachable invalid health fails closed instead of falling back to static config", async () => {
  let malformed;
  try {
    await loadServerDappHealth({
      fetchImpl: async () => new Response("{", {
        headers: { "content-type": "application/json" },
      }),
    });
  } catch (error) {
    malformed = error;
  }
  assert.equal(malformed?.code, "DAPP_BOOTSTRAP_INVALID_JSON");
  assert.equal(healthBootstrapFallbackAllowed(malformed), false);

  const unavailable = await loadServerDappHealth({
    fetchImpl: async () => new Response("{}", {
      status: 404,
      headers: { "content-type": "application/json" },
    }),
  }).catch(error => error);
  assert.equal(healthBootstrapFallbackAllowed(unavailable), true);
});

test("static artifact must explicitly opt out of the server-backed mode", async () => {
  await assert.rejects(
    () => loadStaticDappConfig({
      fetchImpl: async () => new Response(JSON.stringify({ serverBacked: true }), {
        headers: { "content-type": "application/json" },
      }),
    }),
    /serverBacked: false/,
  );
});

test("batch transfer remains static-disabled and server opt-in", async () => {
  const featureConfig = {
    serverBacked: false,
    serverFeatures: { batchTransfer: true },
  };
  await assert.rejects(
    () => loadStaticDappConfig({
      fetchImpl: async () => new Response(JSON.stringify(featureConfig), {
        headers: { "content-type": "application/json" },
      }),
    }),
    error => error?.code === "DAPP_BOOTSTRAP_UNSUPPORTED_FEATURE",
  );
  const serverConfig = { ...featureConfig, serverBacked: true };
  const health = await loadServerDappHealth({
    fetchImpl: async () => new Response(JSON.stringify({ config: serverConfig }), {
      headers: { "content-type": "application/json" },
    }),
  });
  assert.equal(health.config.serverFeatures.batchTransfer, true);
});
