import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer as createHttpServer } from "node:http";
import { createServer as createTcpServer } from "node:net";

async function freePort() {
  const server = createTcpServer();
  server.listen(0, "127.0.0.1");
  await once(server, "listening");
  const port = server.address().port;
  server.close();
  await once(server, "close");
  return port;
}

async function startDummyProver(responseBody = {}) {
  const calls = [];
  const responseJson = {
    version: "v1",
    proof_hex: "00",
    payload_hash: "11".repeat(32),
    ...responseBody
  };
  const server = createHttpServer(async (req, res) => {
    const chunks = [];
    for await (const chunk of req) {
      chunks.push(chunk);
    }
    calls.push({
      method: req.method,
      path: req.url,
      authorization: req.headers.authorization || "",
      body: Buffer.concat(chunks).toString("utf8")
    });
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify(responseJson));
  });
  const port = await freePort();
  server.listen(port, "127.0.0.1");
  await once(server, "listening");
  return {
    calls,
    close: async () => {
      server.close();
      await once(server, "close");
    },
    url: `http://127.0.0.1:${port}`
  };
}

async function waitForJson(url, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    try {
      const response = await fetch(url);
      const json = await response.json();
      return { response, json };
    } catch (error) {
      lastError = error;
      await new Promise(resolve => setTimeout(resolve, 100));
    }
  }
  throw lastError || new Error(`timed out waiting for ${url}`);
}

test("DApp exposes config, health, and bundled frontend assets", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port)
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`);
    assert.equal(config.response.status, 200);
    assert.equal(config.json.chainId.startsWith("clairveil-local-"), true);
    assert.equal("evmChainId" in config.json, false);
    assert.equal(config.json.activeChainProfileId, "clairveil-local");
    assert.equal(config.json.chainProfiles.length, 1);
    assert.equal(config.json.chainProfiles[0].id, "clairveil-local");
    assert.equal(config.json.chainProfiles[0].wallet, "keplr");
    assert.equal(config.json.chainProfiles.find(profile => profile.id === "evm-local"), undefined);
    assert.equal(config.json.chainProfiles.find(profile => profile.id === "clairveil-local").proverUrl, "http://127.0.0.1:8080");
    assert.equal(config.json.keplrChainInfo.bech32Config.bech32PrefixAccAddr, "clair");

    const health = await waitForJson(`${baseUrl}/api/health`);
    assert.equal(health.response.status, 200);
    assert.equal(health.json.config.keplrChainInfo.chainId, config.json.chainId);
    assert.equal("evmChainId" in health.json.config, false);
    assert.ok(Array.isArray(health.json.errors));

    const appBundle = await fetch(`${baseUrl}/app.bundle.js`);
    assert.equal(appBundle.status, 200);
    assert.match(await appBundle.text(), /createClairveilBrowserDappClient/);

    const removedEventsProxy = await fetch(`${baseUrl}/api/events`);
    assert.equal(removedEventsProxy.status, 404);

    const removedAuditorProxy = await fetch(`${baseUrl}/api/auditor/transfers`);
    assert.equal(removedAuditorProxy.status, 404);

    const removedSdkStatic = await fetch(`${baseUrl}/sdk/clairveiljs/browser-public.js`);
    assert.equal(removedSdkStatic.status, 404);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp exposes EVM profile only when EVM transport is active", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_TRANSPORT: "evm",
      CHAIN_ID: "evm-privacy-local-1",
      CLAIRVEIL_ACCOUNT_PREFIX: "evm",
      CLAIRVEIL_DENOM: "utoken",
      CLAIRVEIL_DISPLAY_DENOM: "TOKEN"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`);
    assert.equal(config.response.status, 200);
    assert.equal(config.json.transport, "evm");
    assert.equal(config.json.accountPrefix, "evm");
    assert.equal(config.json.evmChainId, "0x32f");
    assert.equal(config.json.activeChainProfileId, "evm-local");
    assert.equal(config.json.chainProfiles.length, 1);
    const evmProfile = config.json.chainProfiles[0];
    assert.equal(evmProfile.id, "evm-local");
    assert.equal(evmProfile.accountPrefix, "clair");
    assert.equal("hostAccountPrefix" in evmProfile, false);
    assert.equal(evmProfile.denom, "utoken");
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp proxies same-origin prover requests for browser SDK flows", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const publicProver = await startDummyProver({
    proof_hex: "ff",
    payload_hash: "22".repeat(32)
  });
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_PROVER_URL: prover.url,
      CLAIRVEIL_PUBLIC_PROVER_URL: publicProver.url,
      CLAIRVEIL_PROVER_BEARER_TOKEN: "test-token"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`);
    assert.equal(config.json.proverUrl, publicProver.url);

    const response = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        version: "v1",
        payload: {
          memo: "browser-sdk-prover-proxy"
        }
      })
    });
    assert.equal(response.status, 200);
    assert.deepEqual(await response.json(), {
      version: "v1",
      proof_hex: "00",
      payload_hash: "11".repeat(32)
    });
    assert.equal(prover.calls.length, 1);
    assert.equal(prover.calls[0].method, "POST");
    assert.equal(prover.calls[0].path, "/v1/prover/transfer");
    assert.equal(prover.calls[0].authorization, "Bearer test-token");
    assert.match(prover.calls[0].body, /browser-sdk-prover-proxy/);
    assert.equal(publicProver.calls.length, 0);

    const getResponse = await fetch(`${baseUrl}/v1/prover/transfer`);
    assert.equal(getResponse.status, 405);
    const getJson = await getResponse.json();
    assert.equal(getJson.code, "method_not_allowed");
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    await publicProver.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp disables local-only backend routes outside local test mode", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "0",
      CLAIRVEIL_RPC: "https://rpc.public.example",
      CLAIRVEIL_REST: "https://rest.public.example",
      CLAIRVEIL_PROVER_URL: "https://prover.public.example"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`);
    assert.equal(config.response.status, 200);
    assert.equal(config.json.localTestMode, false);
    assert.equal(config.json.modeLabel, "Public Node DApp");
    assert.equal(config.json.serverFeatures.localSigners, false);
    assert.equal(config.json.serverFeatures.faucet, false);
    assert.equal(config.json.serverFeatures.auditorAdmin, false);
    assert.equal(config.json.localSignerHome, "");
    assert.deepEqual(config.json.accounts, []);

    const localOnlyRoutes = [
      { path: "/api/local-signers/ensure", init: { method: "POST", body: "{}" } },
      { path: "/api/faucet", init: { method: "POST", body: "{}" } },
      { path: "/api/auditor/test-scalar", init: { method: "GET" } },
      { path: "/api/auditor/decode", init: { method: "POST", body: "{}" } },
      { path: "/api/wallet/alice/show-address", init: { method: "GET" } },
      { path: "/api/wallet/alice/notes", init: { method: "GET" } },
      { path: "/api/deposit", init: { method: "POST", body: "{}" } }
    ];

    for (const route of localOnlyRoutes) {
      const response = await fetch(`${baseUrl}${route.path}`, {
        headers: { "content-type": "application/json" },
        ...route.init
      });
      assert.equal(response.status, 403, route.path);
      const json = await response.json();
      assert.match(json.error, /CLAIRVEIL_DAPP_LOCAL_TEST_MODE is off/);
    }

    const removedWalletFeatureRoutes = [
      { path: "/api/tx/keplr/bank-send/sign-doc", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/keplr/privacy-deposit/sign-doc", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/keplr/privacy-transfer/sign-doc", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/keplr/privacy-withdraw/sign-doc", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/evm/bank-send/transaction", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/evm/privacy-deposit/transaction", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/evm/privacy-transfer/transaction", init: { method: "POST", body: "{}" } },
      { path: "/api/tx/evm/privacy-withdraw/transaction", init: { method: "POST", body: "{}" } },
      { path: "/api/keplr/privacy/notes", init: { method: "POST", body: "{}" } },
      { path: "/api/keplr/privacy/disclosure/decode", init: { method: "POST", body: "{}" } }
    ];

    for (const route of removedWalletFeatureRoutes) {
      const response = await fetch(`${baseUrl}${route.path}`, {
        headers: { "content-type": "application/json" },
        ...route.init
      });
      assert.equal(response.status, 404, `${route.path} should be owned by browser ClairveilJS, not the demo server`);
    }
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});
