import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { createServer as createHttpServer } from "node:http";
import { createServer as createTcpServer } from "node:net";
import { networkInterfaces, tmpdir } from "node:os";
import { join } from "node:path";
import { evmAddressToBech32 } from "clairveiljs/evm";
import { computePreparedWithdrawProverPayloadHash } from "clairveiljs/payload";

function lanIpv4Address() {
  return Object.values(networkInterfaces())
    .flat()
    .find(entry => entry && !entry.internal && (entry.family === "IPv4" || entry.family === 4))
    ?.address || "";
}

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
    proof: {
      version: "v1",
      proof_hex: "00",
      payload_hash: "11".repeat(32)
    },
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

function withdrawProverPayload({ recipient, evmRecipient }) {
  const payload = {
    version: "v1",
    root_hex: "01".repeat(32),
    nullifier_hex: "02".repeat(32),
    amount: "7uclair",
    asset_denom: "uclair",
    asset_id_hex: "03".repeat(32),
    recipient,
    recipient_bytes_hex: evmRecipient.replace(/^0x/i, "").toLowerCase(),
    chain_id: "evm-privacy-local-1",
    expires_at_unix: 2000000000,
    note_randomness_hex: "04".repeat(32),
    spend_pubkey_hex: "05".repeat(32),
    view_pubkey_hex: "06".repeat(32),
    merkle_path: ["07".repeat(32), "08".repeat(32)],
    merkle_path_helper: [0, 1],
    spend_note_hash_signature_hex: "09".repeat(64)
  };
  payload.payload_hash = computePreparedWithdrawProverPayloadHash(payload);
  return payload;
}

async function writeLocalAccountFixtures(home) {
  const out = join(home, "init-out");
  await mkdir(out, { recursive: true });
  await Promise.all([
    writeFile(join(out, "alice-address.txt"), "clair1alice0000000000000000000000000000000"),
    writeFile(join(out, "bob-address.txt"), "clair1bob000000000000000000000000000000000"),
    writeFile(join(out, "relayer-address.txt"), "clair1relayer00000000000000000000000000000"),
    writeFile(join(out, "auditor-address.txt"), "clair1auditor00000000000000000000000000000")
  ]);
}

async function waitForJson(url, options = {}) {
  const { timeoutMs = 15000, debugOutput = () => "" } = typeof options === "number" ? { timeoutMs: options } : options;
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
  const debug = debugOutput();
  const message = `timed out waiting for ${url}${debug ? `\n${debug}` : ""}`;
  if (lastError) {
    throw new Error(message, { cause: lastError });
  }
  throw new Error(message);
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

  const stdout = [];
  const stderr = [];
  child.stdout.on("data", chunk => stdout.push(String(chunk)));
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  const debugOutput = () => [
    "server stdout:",
    stdout.join("").trim() || "-",
    "server stderr:",
    stderr.join("").trim() || "-"
  ].join("\n");

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`, { debugOutput });
    assert.equal(config.response.status, 200);
    assert.equal(config.json.chainId.startsWith("clairveil-local-"), true);
    assert.equal("evmChainId" in config.json, false);
    assert.equal(config.json.activeChainProfileId, "clairveil-local");
    assert.equal(config.json.chainProfiles.length, 1);
    assert.equal(config.json.chainProfiles[0].id, "clairveil-local");
    assert.equal(config.json.chainProfiles[0].wallet, "keplr");
    assert.equal(config.json.chainProfiles.find(profile => profile.id === "evm-local"), undefined);
    assert.equal(config.json.chainProfiles.find(profile => profile.id === "clairveil-local").proverUrl, "http://127.0.0.1:8080");
    assert.equal(config.json.serverFeatures.proverProxy, true);
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
    assert.equal(config.json.serverFeatures.proverProxy, true);
    assert.equal(config.json.proverUrl, baseUrl);
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
    proof: {
      version: "v1",
      proof_hex: "ff",
      payload_hash: "22".repeat(32)
    }
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
      proof: {
        version: "v1",
        proof_hex: "00",
        payload_hash: "11".repeat(32)
      }
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

test("DApp requires explicit prover proxy opt-in for forwarded loopback requests", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_PROVER_URL: prover.url,
      CLAIRVEIL_PROVER_BEARER_TOKEN: "forwarded-request-token"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const forwardedHeaders = {
      host: "dapp.public.example",
      "x-forwarded-for": "203.0.113.10",
      "x-forwarded-proto": "https"
    };
    const configResponse = await fetch(`${baseUrl}/api/config`, {
      headers: forwardedHeaders
    });
    assert.equal(configResponse.status, 200);
    const config = await configResponse.json();
    assert.equal(config.serverFeatures.proverProxy, false);

    const response = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: {
        ...forwardedHeaders,
        "content-type": "application/json"
      },
      body: JSON.stringify({ version: "v1", payload: {} })
    });
    assert.equal(response.status, 404);
    assert.equal(prover.calls.length, 0);

    const signerResponse = await fetch(`${baseUrl}/api/relayer/withdraw`, {
      method: "POST",
      headers: {
        ...forwardedHeaders,
        "content-type": "application/json"
      },
      body: "{}"
    });
    assert.equal(signerResponse.status, 403);
    const signerJson = await signerResponse.json();
    assert.match(signerJson.error, /LAN access to signer-mutating APIs is disabled/);

    const adminResponse = await fetch(`${baseUrl}/api/auditor/test-scalar`, {
      headers: forwardedHeaders
    });
    assert.equal(adminResponse.status, 403);
    const adminJson = await adminResponse.json();
    assert.match(adminJson.error, /LAN access to local admin\/private-read APIs is disabled/);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp rejects cross-origin privileged requests and non-JSON bodies", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_PROVER_URL: prover.url,
      CLAIRVEIL_PROVER_BEARER_TOKEN: "cross-origin-test-token"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const crossOriginHeaders = {
      origin: "https://attacker.example",
      "sec-fetch-site": "cross-site",
      "content-type": "application/json"
    };

    for (const path of ["/api/relayer/withdraw", "/api/deposit/proof"]) {
      const response = await fetch(`${baseUrl}${path}`, {
        method: "POST",
        headers: crossOriginHeaders,
        body: "{}"
      });
      assert.equal(response.status, 403, path);
    }

    const proverResponse = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: crossOriginHeaders,
      body: JSON.stringify({ version: "v1", payload: {} })
    });
    assert.equal(proverResponse.status, 404);
    assert.equal(prover.calls.length, 0);

    const nonJsonResponse = await fetch(`${baseUrl}/api/deposit/proof`, {
      method: "POST",
      headers: { "content-type": "text/plain" },
      body: "{}"
    });
    assert.equal(nonJsonResponse.status, 415);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("EVM prover proxy leaves reference-prefixed withdraw payloads unchanged", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_TRANSPORT: "evm",
      CLAIRVEIL_ACCOUNT_PREFIX: "maroo",
      CLAIRVEIL_PROVER_ACCOUNT_PREFIX: "clair",
      CLAIRVEIL_PROVER_URL: prover.url
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
    const response = await fetch(`${baseUrl}/v1/prover/withdraw`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        version: "v1",
        payload: {
          recipient: "clair1cml96vmptgw99syqrrz8az79xer2pcgphasy4k"
        }
      })
    });
    assert.equal(response.status, 200);
    assert.equal(prover.calls.length, 1);
    const forwarded = JSON.parse(prover.calls[0].body);
    assert.equal(
      forwarded.payload.recipient,
      "clair1cml96vmptgw99syqrrz8az79xer2pcgphasy4k"
    );
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("EVM prover proxy rewrites local-prefixed withdraw payloads for the reference prover", async () => {
  const port = await freePort();
  const evmRecipient = "0x1111111111111111111111111111111111111111";
  const localRecipient = evmAddressToBech32(evmRecipient, "maroo");
  const referenceRecipient = evmAddressToBech32(evmRecipient, "clair");
  const originalPayload = withdrawProverPayload({
    recipient: localRecipient,
    evmRecipient
  });
  const rewrittenPayload = {
    ...originalPayload,
    recipient: referenceRecipient,
    recipient_bytes_hex: evmRecipient.replace(/^0x/i, "").toLowerCase()
  };
  rewrittenPayload.payload_hash = computePreparedWithdrawProverPayloadHash(rewrittenPayload);
  const prover = await startDummyProver({
    proof: {
      version: "v1",
      proof_hex: "00",
      payload_hash: "ff".repeat(32)
    }
  });
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_TRANSPORT: "evm",
      CLAIRVEIL_ACCOUNT_PREFIX: "maroo",
      CLAIRVEIL_PROVER_ACCOUNT_PREFIX: "clair",
      CLAIRVEIL_PROVER_URL: prover.url
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
    const response = await fetch(`${baseUrl}/v1/prover/withdraw`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        version: "v1",
        payload: originalPayload
      })
    });
    assert.equal(response.status, 200);
    assert.equal(prover.calls.length, 1);
    const forwarded = JSON.parse(prover.calls[0].body);
    assert.equal(forwarded.version, "v1");
    assert.equal(forwarded.payload.recipient, referenceRecipient);
    assert.equal(forwarded.payload.recipient_bytes_hex, evmRecipient.slice(2).toLowerCase());
    assert.equal(forwarded.payload.payload_hash, rewrittenPayload.payload_hash);
    assert.notEqual(forwarded.payload.payload_hash, originalPayload.payload_hash);

    const proxied = await response.json();
    assert.deepEqual(proxied, {
      version: "v1",
      proof: {
        version: "v1",
        proof_hex: "00",
        payload_hash: originalPayload.payload_hash
      }
    });
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp disables local-only backend routes outside local test mode", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "0",
      CLAIRVEIL_RPC: "https://rpc.public.example",
      CLAIRVEIL_REST: "https://rest.public.example",
      CLAIRVEIL_PROVER_URL: prover.url,
      CLAIRVEIL_PROVER_BEARER_TOKEN: "public-mode-token"
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
    assert.equal(config.json.serverFeatures.proverProxy, false);
    assert.equal(config.json.localSignerHome, "");
    assert.deepEqual(config.json.accounts, []);

    const localOnlyRoutes = [
      { path: "/api/local-signers/ensure", init: { method: "POST", body: "{}" } },
      { path: "/api/faucet", init: { method: "POST", body: "{}" } },
      { path: "/api/auditor/test-scalar", init: { method: "GET" } },
      { path: "/api/auditor/decode", init: { method: "POST", body: "{}" } },
      { path: "/api/wallet/alice/show-address", init: { method: "GET" } },
      { path: "/api/wallet/alice/notes", init: { method: "GET" } },
      { path: "/api/deposit/proof", init: { method: "POST", body: "{}" } },
      { path: "/api/relayer/withdraw", init: { method: "POST", body: "{}" } },
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

    const proverProxy = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "v1", payload: {} })
    });
    assert.equal(proverProxy.status, 404);
    assert.equal(prover.calls.length, 0);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("EVM LAN clients keep the public prover URL when the proxy is disabled", async (t) => {
  const lanAddress = lanIpv4Address();
  if (!lanAddress) {
    t.skip("no LAN IPv4 address is available for non-loopback route coverage");
    return;
  }

  const port = await freePort();
  const child = spawn(process.execPath, ["server.js", "--host", "0.0.0.0"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_TRANSPORT: "evm",
      CLAIRVEIL_PUBLIC_PROVER_URL: "https://prover.public.example"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stdout = [];
  const stderr = [];
  child.stdout.on("data", chunk => stdout.push(String(chunk)));
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  const debugOutput = () => [stdout.join("").trim(), stderr.join("").trim()].join("\n");

  try {
    const baseUrl = `http://${lanAddress}:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`, { debugOutput });
    assert.equal(config.response.status, 200);
    assert.equal(config.json.transport, "evm");
    assert.equal(config.json.serverFeatures.proverProxy, false);
    assert.equal(config.json.proverUrl, "https://prover.public.example");
    assert.equal(config.json.chainProfiles[0].proverUrl, "https://prover.public.example");
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp exposes only the relayer account to LAN signing clients", async (t) => {
  const lanAddress = lanIpv4Address();
  if (!lanAddress) {
    t.skip("no LAN IPv4 address is available for non-loopback route coverage");
    return;
  }

  const port = await freePort();
  const home = await mkdtemp(join(tmpdir(), "clairveil-dapp-routes-"));
  await writeLocalAccountFixtures(home);

  const child = spawn(process.execPath, ["server.js", "--host", "0.0.0.0"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_HOME: home,
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING: "1",
      CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN: "0",
      CLAIRVEIL_TRANSPORT: "cosmos"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stdout = [];
  const stderr = [];
  child.stdout.on("data", chunk => stdout.push(String(chunk)));
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  const debugOutput = () => [
    "server stdout:",
    stdout.join("").trim() || "-",
    "server stderr:",
    stderr.join("").trim() || "-"
  ].join("\n");

  try {
    const baseUrl = `http://${lanAddress}:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`, { debugOutput });
    assert.equal(config.response.status, 200);
    assert.equal(config.json.serverFeatures.localSignerAdmin, false);
    assert.equal(config.json.serverFeatures.localSigners, false);
    assert.equal(config.json.serverFeatures.auditorAdmin, false);
    assert.equal(config.json.serverFeatures.relayer, true);
    assert.equal(config.json.serverFeatures.proverProxy, false);
    assert.deepEqual(config.json.accounts.map(account => account.name), ["relayer"]);
    assert.deepEqual(config.json.accounts.map(account => account.transparentAddress), [
      "clair1relayer00000000000000000000000000000"
    ]);

    const health = await waitForJson(`${baseUrl}/api/health`, { debugOutput });
    assert.equal(health.response.status, 200);
    assert.deepEqual(health.json.accounts.map(account => account.name), ["relayer"]);

    const auditorResponse = await fetch(`${baseUrl}/api/auditor/test-scalar`);
    assert.equal(auditorResponse.status, 403);
    const auditorJson = await auditorResponse.json();
    assert.match(auditorJson.error, /LAN access to local admin\/private-read APIs is disabled/);

    const proverProxy = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "v1", payload: {} })
    });
    assert.equal(proverProxy.status, 404);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await rm(home, { recursive: true, force: true });
    assert.equal(stderr.join("").trim(), "");
  }
});
