import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer as createHttpServer, request as createHttpRequest } from "node:http";
import { createServer as createTcpServer } from "node:net";
import { networkInterfaces } from "node:os";
import { validateClairveilWebClientConfig } from "clairveiljs/browser-dapp";

function validatedConfig(responseJson) {
  return validateClairveilWebClientConfig(responseJson);
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

function lanIpv4Address() {
  return Object.values(networkInterfaces())
    .flat()
    .find(entry => entry && !entry.internal && (entry.family === "IPv4" || entry.family === 4))
    ?.address || "";
}

async function startDummyProver(responseBody = null) {
  const calls = [];
  const responseJson = responseBody ?? {
    version: "v2",
    proof: {
      version: "v2",
      proof_hex: "00",
      payload_hash: "11".repeat(32),
    },
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

async function startHttpFixture(handler) {
  const server = createHttpServer(handler);
  const port = await freePort();
  server.listen(port, "127.0.0.1");
  await once(server, "listening");
  return {
    server,
    url: `http://127.0.0.1:${port}`,
    close: async () => {
      const closed = once(server, "close");
      server.close();
      server.closeAllConnections?.();
      await closed;
    },
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

async function waitForCondition(predicate, timeoutMs = 2000) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (predicate()) return;
    await new Promise(resolve => setTimeout(resolve, 10));
  }
  throw new Error("timed out waiting for condition");
}

test("DApp exposes config, health, and bundled frontend assets", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_COSMOS_REST_ENDPOINTS: "http://127.0.0.1:1317, http://127.0.0.1:2317"
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
    assert.deepEqual(config.json.chainProfiles[0].restEndpoints, [
      "http://127.0.0.1:1317",
      "http://127.0.0.1:2317"
    ]);
    assert.equal(config.json.keplrChainInfo.bech32Config.bech32PrefixAccAddr, "clair");
    assert.equal(config.json.schemaVersion, "clairveil-web-client-config-v1");
    assert.equal(config.json.serverFeatures.depositProof, false);
    assert.equal(config.json.serverFeatures.relayer, true);
    assert.equal(validatedConfig(config.json).activeProfile.id, "clairveil-local");

    const health = await waitForJson(`${baseUrl}/api/health`);
    assert.equal(health.response.status, 200);
    assert.equal(health.json.config.keplrChainInfo.chainId, config.json.chainId);
    assert.equal("evmChainId" in health.json.config, false);
    assert.ok(Array.isArray(health.json.errors));

    const appBundle = await fetch(`${baseUrl}/app.bundle.js`);
    assert.equal(appBundle.status, 200);
    assert.equal(appBundle.headers.get("x-content-type-options"), "nosniff");
    assert.equal(appBundle.headers.get("referrer-policy"), "no-referrer");
    assert.equal(appBundle.headers.get("cross-origin-opener-policy"), "same-origin");
    const csp = appBundle.headers.get("content-security-policy");
    assert.match(csp, /default-src 'self'/);
    assert.match(csp, /frame-ancestors 'none'/);
    assert.match(csp, /script-src 'self'/);
    assert.match(csp, /connect-src 'self'/);
    assert.match(csp, /http:\/\/127\.0\.0\.1:26657/);
    assert.match(csp, /http:\/\/127\.0\.0\.1:1317/);
    assert.match(csp, /http:\/\/127\.0\.0\.1:2317/);
    assert.match(csp, /http:\/\/127\.0\.0\.1:8080/);
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

test("health gateway bounds admission and aborts upstream work on timeout or disconnect", async () => {
  let upstreamRequests = 0;
  let upstreamClosed = 0;
  const upstream = await startHttpFixture((_req, res) => {
    upstreamRequests += 1;
    res.once("close", () => {
      upstreamClosed += 1;
    });
    // Intentionally never respond. The DApp must abort this request itself.
  });
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_RPC: upstream.url,
      CLAIRVEIL_REST: upstream.url,
      CLAIRVEIL_PUBLIC_RPC: "",
      CLAIRVEIL_PUBLIC_REST: "",
      CLAIRVEIL_COSMOS_RPC: "",
      CLAIRVEIL_COSMOS_REST: "",
      CLAIRVEIL_DAPP_UPSTREAM_TIMEOUT_MS: "75",
      CLAIRVEIL_DAPP_HEALTH_MAX_IN_FLIGHT: "1",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const first = fetch(`${baseUrl}/api/health`);
    await waitForCondition(() => upstreamRequests >= 3);
    const rejected = await fetch(`${baseUrl}/api/health`);
    assert.equal(rejected.status, 503);
    assert.deepEqual(await rejected.json(), {
      error: "health request capacity is exhausted",
      code: "capacity_exceeded",
      retry_after_ms: 1000,
    });

    const timedOut = await first;
    assert.equal(timedOut.status, 200);
    const timedOutBody = await timedOut.json();
    assert.equal(timedOutBody.errors.length, 3);
    assert.equal(timedOutBody.errors.every(message => /timed out after 75ms/.test(message)), true);
    await waitForCondition(() => upstreamClosed >= 3);

    const abortController = new AbortController();
    const requestsBeforeAbort = upstreamRequests;
    const closesBeforeAbort = upstreamClosed;
    const aborted = fetch(`${baseUrl}/api/health`, { signal: abortController.signal });
    await waitForCondition(() => upstreamRequests >= requestsBeforeAbort + 3);
    abortController.abort();
    await assert.rejects(aborted, error => error?.name === "AbortError");
    await waitForCondition(() => upstreamClosed >= closesBeforeAbort + 3);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await upstream.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("health gateway rejects oversized upstream JSON before parsing", async () => {
  const largeBody = JSON.stringify({ padding: "x".repeat(1024) });
  const upstream = await startHttpFixture((_req, res) => {
    res.writeHead(200, {
      "content-type": "application/json",
      "content-length": String(Buffer.byteLength(largeBody)),
    });
    res.end(largeBody);
  });
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_RPC: upstream.url,
      CLAIRVEIL_REST: upstream.url,
      CLAIRVEIL_PUBLIC_RPC: "",
      CLAIRVEIL_PUBLIC_REST: "",
      CLAIRVEIL_COSMOS_RPC: "",
      CLAIRVEIL_COSMOS_REST: "",
      CLAIRVEIL_DAPP_UPSTREAM_MAX_RESPONSE_BYTES: "128",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const health = await fetch(`${baseUrl}/api/health`);
    assert.equal(health.status, 200);
    const body = await health.json();
    assert.equal(body.status, null);
    assert.equal(body.tree, null);
    assert.equal(body.audit, null);
    assert.equal(body.errors.length, 3);
    assert.equal(body.errors.every(message => /exceeds 128 byte limit/.test(message)), true);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await upstream.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("local signer mutation routes require exact same-origin JSON requests", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_HOST: "127.0.0.1",
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_TRANSPORT: "cosmos",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const path = "/api/faucet";

    const crossOrigin = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: "https://attacker.example",
      },
      body: "{}",
    });
    assert.equal(crossOrigin.status, 403);
    assert.match((await crossOrigin.json()).error, /exact same-origin/);

    const simpleCsrf = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      headers: {
        "content-type": "text/plain",
        origin: "https://attacker.example",
      },
      body: "{}",
    });
    assert.equal(simpleCsrf.status, 415);
    assert.match((await simpleCsrf.json()).error, /application\/json/);

    const missingOrigin = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(missingOrigin.status, 403);

    const sameOrigin = await fetch(`${baseUrl}${path}`, {
      method: "POST",
      headers: {
        "content-type": "application/json; charset=utf-8",
        origin: baseUrl,
      },
      body: JSON.stringify({ amount: "not-a-coin" }),
    });
    assert.equal(sameOrigin.status, 400);
    assert.match((await sameOrigin.json()).error, /amount must look like/);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});

test("an explicit LAN bind keeps rewritten local browser endpoints in the CSP", async t => {
  const lanAddress = lanIpv4Address();
  if (!lanAddress) {
    t.skip("no LAN IPv4 address is available");
    return;
  }
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_DAPP_HOST: "0.0.0.0",
      CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
      CLAIRVEIL_TRANSPORT: "cosmos",
      CLAIRVEIL_RPC: "tcp://127.0.0.1:26657",
      CLAIRVEIL_REST: "http://127.0.0.1:1317",
      CLAIRVEIL_PUBLIC_RPC: "",
      CLAIRVEIL_PUBLIC_REST: "",
      CLAIRVEIL_PUBLIC_REST_ENDPOINTS: "",
      CLAIRVEIL_COSMOS_RPC: "",
      CLAIRVEIL_COSMOS_REST: "",
      CLAIRVEIL_COSMOS_REST_ENDPOINTS: "",
      CLAIRVEIL_PUBLIC_PROVER_URL: "http://127.0.0.1:8080",
      CLAIRVEIL_COSMOS_PROVER_URL: "",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "http://127.0.0.1:8081/v1/prove",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  try {
    await waitForJson(`http://127.0.0.1:${port}/api/config`);
    const response = await fetch(`http://${lanAddress}:${port}/app.bundle.js`);
    assert.equal(response.status, 200);
    const csp = response.headers.get("content-security-policy") || "";
    assert.ok(csp.includes(`http://${lanAddress}:26657`), csp);
    assert.ok(csp.includes(`http://${lanAddress}:1317`), csp);
    assert.ok(csp.includes(`http://${lanAddress}:8080`), csp);
    assert.ok(csp.includes(`http://${lanAddress}:${port}`), csp);
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
      CLAIRVEIL_DISPLAY_DENOM: "TOKEN",
      CLAIRVEIL_EVM_PRIVACY_PRECOMPILE: "0x0000000000000000000000000000000000000808",
      CLAIRVEIL_EVM_DEPOSIT_MODE: "payable-exact-value",
      CLAIRVEIL_EVM_NATIVE_DENOM: "utoken",
      CLAIRVEIL_EVM_HOST_REST_ENDPOINTS: "http://127.0.0.1:1317, http://127.0.0.1:3317",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: ""
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
    assert.equal(config.json.accountPrefix, "clair");
    assert.equal(config.json.evmChainId, "0x32f");
    assert.equal(config.json.activeChainProfileId, "evm-local");
    assert.equal(config.json.chainProfiles.length, 1);
    const evmProfile = config.json.chainProfiles[0];
    assert.equal(evmProfile.id, "evm-local");
    assert.equal(evmProfile.accountPrefix, "clair");
    assert.equal("hostAccountPrefix" in evmProfile, false);
    assert.equal(evmProfile.denom, "utoken");
    assert.equal(evmProfile.evmDepositMode, "payable-exact-value");
    assert.equal(evmProfile.evmNativeDenom, "utoken");
    assert.deepEqual(evmProfile.restEndpoints, [
      "http://127.0.0.1:1317",
      "http://127.0.0.1:3317"
    ]);
    assert.equal(validatedConfig(config.json).activeProfile.id, "evm-local");
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    assert.equal(stderr.join("").trim(), "");
  }
});

test("DApp proxies same-origin prover requests for browser SDK flows", async () => {
  const port = await freePort();
  const prover = await startDummyProver();
  const depositProof = await startDummyProver({
    version: "v1",
    proof_hex: "aa",
    note_commitment_hex: "33".repeat(32),
  });
  const publicProver = await startDummyProver({
    version: "v2",
    proof: {
      version: "v2",
      proof_hex: "ff",
      payload_hash: "22".repeat(32),
    },
  });
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_PROVER_URL: `${prover.url}/tenant/cosmos`,
      CLAIRVEIL_PUBLIC_PROVER_URL: publicProver.url,
      CLAIRVEIL_DEPOSIT_PROOF_URL: `${depositProof.url}/exact-deposit-proof`,
      CLAIRVEIL_PROVER_BEARER_TOKEN: "test-token",
      CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
      CLAIRVEIL_PROVER_PROXY_RATE_LIMIT_MAX: "2"
    },
    stdio: ["ignore", "pipe", "pipe"]
  });

  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`);
    assert.equal(config.json.proverUrl, publicProver.url);
    assert.equal(config.json.serverFeatures.depositProof, true);
    assert.equal(config.json.chainProfiles[0].depositProofUrl, `${baseUrl}/v1/prover/deposit`);
    assert.equal(validatedConfig(config.json).activeProfile.depositProofUrl, `${baseUrl}/v1/prover/deposit`);

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
      version: "v2",
      proof: {
        version: "v2",
        proof_hex: "00",
        payload_hash: "11".repeat(32),
      },
    });
    assert.equal(prover.calls.length, 1);
    assert.equal(prover.calls[0].method, "POST");
    assert.equal(prover.calls[0].path, "/tenant/cosmos/v1/prover/transfer");
    assert.equal(prover.calls[0].authorization, "Bearer test-token");
    assert.match(prover.calls[0].body, /browser-sdk-prover-proxy/);
    assert.equal(publicProver.calls.length, 0);

    const depositResponse = await fetch(`${baseUrl}/v1/prover/deposit`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "deposit-proof-v1", amount: "1uclair" })
    });
    assert.equal(depositResponse.status, 200);
    assert.equal((await depositResponse.json()).version, "v1");
    assert.equal(depositProof.calls.length, 1);
    assert.equal(depositProof.calls[0].path, "/exact-deposit-proof");
    assert.equal(depositProof.calls[0].authorization, "Bearer test-token");

    const rateLimited = await fetch(`${baseUrl}/v1/prover/withdraw`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "v2", payload: {} })
    });
    assert.equal(rateLimited.status, 429);
    assert.equal((await rateLimited.json()).code, "rate_limited");
    assert.equal(prover.calls.length, 1);

    const preflight = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "OPTIONS",
      headers: { origin: "https://untrusted.example" }
    });
    assert.equal(preflight.status, 204);
    assert.equal(preflight.headers.get("access-control-allow-origin"), null);

    const getResponse = await fetch(`${baseUrl}/v1/prover/transfer`);
    assert.equal(getResponse.status, 405);
    const getJson = await getResponse.json();
    assert.equal(getJson.code, "method_not_allowed");
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    await depositProof.close();
    await publicProver.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("local prover proxy times out or cancels incomplete request bodies and releases capacity", async () => {
  const prover = await startDummyProver();
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_PROVER_URL: prover.url,
      CLAIRVEIL_PUBLIC_PROVER_URL: prover.url,
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
      CLAIRVEIL_PROVER_TIMEOUT_MS: "75",
      CLAIRVEIL_PROVER_PROXY_MAX_IN_FLIGHT: "1",
      CLAIRVEIL_PROVER_PROXY_RATE_LIMIT_MAX: "100",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  function startIncompletePost(url) {
    let request;
    const response = new Promise((resolveResponse, rejectResponse) => {
      request = createHttpRequest(url, {
        method: "POST",
        headers: {
          "content-type": "application/json",
          "content-length": "1024",
        },
      }, responseMessage => {
        const chunks = [];
        responseMessage.on("data", chunk => chunks.push(chunk));
        responseMessage.on("end", () => resolveResponse({
          status: responseMessage.statusCode,
          body: Buffer.concat(chunks).toString("utf8"),
        }));
      });
      request.on("error", rejectResponse);
      request.write("{");
    });
    return { request, response };
  }

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);

    const timedOutBody = startIncompletePost(`${baseUrl}/v1/prover/transfer`);
    await new Promise(resolve => setTimeout(resolve, 25));
    const capacityRejected = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(capacityRejected.status, 429);
    assert.equal((await capacityRejected.json()).code, "capacity_exceeded");

    const timeoutResponse = await timedOutBody.response;
    assert.equal(timeoutResponse.status, 504);
    assert.equal(JSON.parse(timeoutResponse.body).code, "unavailable");
    timedOutBody.request.destroy();

    const afterTimeout = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(afterTimeout.status, 200);

    const disconnectedBody = startIncompletePost(`${baseUrl}/v1/prover/transfer`);
    const disconnectedOutcome = disconnectedBody.response.catch(error => error);
    await new Promise(resolve => setTimeout(resolve, 25));
    disconnectedBody.request.destroy();
    assert.equal((await disconnectedOutcome) instanceof Error, true);
    await new Promise(resolve => setTimeout(resolve, 25));

    const afterDisconnect = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(afterDisconnect.status, 200);
    assert.equal(prover.calls.length, 2);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await prover.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("local prover proxy rejects upstream redirects without forwarding the witness", async () => {
  let redirectedCalls = 0;
  const redirectedTarget = await startHttpFixture((_req, res) => {
    redirectedCalls += 1;
    res.writeHead(200, { "content-type": "application/json" });
    res.end(JSON.stringify({
      version: "v2",
      proof: {
        version: "v2",
        payload_hash: "11".repeat(32),
        proof_hex: "00",
      },
    }));
  });
  const redirectingProver = await startHttpFixture((_req, res) => {
    res.writeHead(307, { location: `${redirectedTarget.url}/stolen-witness` });
    res.end();
  });
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_PROVER_URL: redirectingProver.url,
      CLAIRVEIL_PUBLIC_PROVER_URL: redirectingProver.url,
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    const response = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "v2", payload: { private: "witness" } }),
    });
    assert.equal(response.status, 502);
    assert.deepEqual(await response.json(), {
      version: "v1",
      code: "proof_failed",
      message: "prover returned an invalid response",
      retryable: false,
    });
    assert.equal(redirectedCalls, 0);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await redirectingProver.close();
    await redirectedTarget.close();
    assert.equal(stderr.join("").trim(), "");
  }
});

test("local prover proxy bounds and validates responses and redacts upstream errors", async () => {
  const upstream = await startHttpFixture((req, res) => {
    if (req.url === "/v1/prover/transfer") {
      res.writeHead(200, { "content-type": "text/html" });
      res.end(JSON.stringify({ version: "v2", proof: {} }));
      return;
    }
    if (req.url === "/v1/prover/withdraw") {
      const body = JSON.stringify({ padding: "x".repeat(1024) });
      res.writeHead(200, {
        "content-type": "application/json",
        "content-length": String(Buffer.byteLength(body)),
      });
      res.end(body);
      return;
    }
    if (req.url === "/exact-deposit-proof") {
      res.writeHead(200, { "content-type": "application/json" });
      res.end(JSON.stringify({
        version: "legacy",
        proof_hex: "00",
        note_commitment_hex: "11".repeat(32),
      }));
      return;
    }
    res.writeHead(500, { "content-type": "text/plain" });
    res.end("private witness and prover internals must not escape");
  });
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_PROVER_URL: upstream.url,
      CLAIRVEIL_PUBLIC_PROVER_URL: upstream.url,
      CLAIRVEIL_DEPOSIT_PROOF_URL: `${upstream.url}/exact-deposit-proof`,
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
      CLAIRVEIL_PROVER_PROXY_MAX_RESPONSE_BYTES: "256",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    await waitForJson(`${baseUrl}/api/config`);
    for (const path of [
      "/v1/prover/transfer",
      "/v1/prover/withdraw",
      "/v1/prover/deposit",
    ]) {
      const response = await fetch(`${baseUrl}${path}`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: "{}",
      });
      assert.equal(response.status, 502, path);
      const body = await response.json();
      assert.equal(body.code, "proof_failed", path);
      assert.equal(body.message, "prover returned an invalid response", path);
      assert.equal(JSON.stringify(body).includes("private witness"), false, path);
    }

    const upstreamFailure = await fetch(`${baseUrl}/v1/proofs/batch-transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    assert.equal(upstreamFailure.status, 500);
    const upstreamFailureBody = await upstreamFailure.json();
    assert.deepEqual(upstreamFailureBody, {
      version: "v1",
      code: "proof_failed",
      message: "prover failed to produce a proof",
      retryable: false,
    });
    assert.equal(JSON.stringify(upstreamFailureBody).includes("private witness"), false);
  } finally {
    child.kill("SIGTERM");
    await once(child, "exit");
    await upstream.close();
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
      CLAIRVEIL_DAPP_PUBLIC_ORIGIN: "https://app.public.example",
      CLAIRVEIL_RPC: "https://rpc.public.example",
      CLAIRVEIL_REST: "https://rest.public.example",
      CLAIRVEIL_PUBLIC_RPC: "https://rpc.public.example",
      CLAIRVEIL_PUBLIC_REST: "https://rest.public.example",
      CLAIRVEIL_PUBLIC_REST_ENDPOINTS: "",
      CLAIRVEIL_COSMOS_RPC: "",
      CLAIRVEIL_COSMOS_REST: "",
      CLAIRVEIL_COSMOS_REST_ENDPOINTS: "",
      CLAIRVEIL_PROVER_URL: "https://prover.public.example",
      CLAIRVEIL_PUBLIC_PROVER_URL: "https://prover.public.example",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PROVER_BEARER_TOKEN: ""
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
    assert.equal(config.json.serverFeatures.relayer, false);
    assert.equal(config.json.serverFeatures.auditorAdmin, false);
    assert.equal(config.json.serverFeatures.proverProxy, false);
    assert.equal(config.json.localSignerHome, "");
    assert.equal("accounts" in config.json, false);

    const health = await waitForJson(`${baseUrl}/api/health`);
    assert.deepEqual(health.json.config, config.json);

    const disabledProverProxy = await fetch(`${baseUrl}/v1/prover/transfer`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ version: "v2", payload: {} })
    });
    assert.equal(disabledProverProxy.status, 404);

    const localOnlyRoutes = [
      { path: "/api/local-signers/ensure", init: { method: "POST", body: "{}" } },
      { path: "/api/faucet", init: { method: "POST", body: "{}" } },
      { path: "/api/relayer/withdraw", init: { method: "POST", body: "{}" } },
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

test("public DApp mode rejects a missing origin and cleartext browser endpoint", async () => {
  const cases = [
    {
      overrides: { CLAIRVEIL_DAPP_PUBLIC_ORIGIN: "" },
      expected: /CLAIRVEIL_DAPP_PUBLIC_ORIGIN must be a valid HTTPS URL/,
    },
    {
      overrides: {
        CLAIRVEIL_DAPP_PUBLIC_ORIGIN: "https://app.public.example",
        CLAIRVEIL_PUBLIC_RPC: "http://rpc.public.example",
      },
      expected: /clairveil-local\.rpc must be a valid HTTPS URL/,
    },
    {
      overrides: {
        CLAIRVEIL_DAPP_PUBLIC_ORIGIN: "https://app.public.example",
        CLAIRVEIL_PROVER_PROXY_ENABLED: "1",
      },
      expected: /CLAIRVEIL_PROVER_PROXY_ENABLED is local-test-only/,
    },
  ];
  for (const { overrides, expected } of cases) {
    const port = await freePort();
    const child = spawn(process.execPath, ["server.js"], {
      cwd: new URL("..", import.meta.url),
      env: {
        ...process.env,
        PORT: String(port),
        CLAIRVEIL_DAPP_PORT: String(port),
        CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "0",
        CLAIRVEIL_RPC: "https://rpc.internal.example",
        CLAIRVEIL_REST: "https://rest.internal.example",
        CLAIRVEIL_PUBLIC_RPC: "https://rpc.public.example",
        CLAIRVEIL_PUBLIC_REST: "https://rest.public.example",
        CLAIRVEIL_PUBLIC_REST_ENDPOINTS: "",
        CLAIRVEIL_COSMOS_RPC: "",
        CLAIRVEIL_COSMOS_REST: "",
        CLAIRVEIL_COSMOS_REST_ENDPOINTS: "",
        CLAIRVEIL_PROVER_URL: "https://prover.internal.example",
        CLAIRVEIL_PUBLIC_PROVER_URL: "https://prover.public.example",
        CLAIRVEIL_DEPOSIT_PROOF_URL: "",
        CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
        CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL: "",
        CLAIRVEIL_PROVER_BEARER_TOKEN: "",
        ...overrides,
      },
      stdio: ["ignore", "ignore", "pipe"],
    });
    let stderr = "";
    child.stderr.on("data", chunk => {
      stderr += String(chunk);
    });
    const [code] = await once(child, "exit");
    assert.notEqual(code, 0);
    assert.match(stderr, expected);
  }
});
