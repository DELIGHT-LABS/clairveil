import test from "node:test";
import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { once } from "node:events";
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

async function waitForJson(url, child, stderr, timeoutMs = 5000) {
  const deadline = Date.now() + timeoutMs;
  let lastError;
  while (Date.now() < deadline) {
    if (child.exitCode !== null) {
      throw new Error(`DApp server exited early: ${stderr.join("").trim()}`);
    }
    try {
      const response = await fetch(url);
      return { response, json: await response.json() };
    } catch (error) {
      lastError = error;
      await new Promise(resolve => setTimeout(resolve, 50));
    }
  }
  throw lastError || new Error(`timed out waiting for ${url}`);
}

async function stopChild(child) {
  if (child.exitCode !== null || child.signalCode !== null) return;
  child.kill("SIGTERM");
  await once(child, "exit");
}

test("EVM profile and faucet parser share the transport-specific denom", async () => {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...process.env,
      PORT: String(port),
      CLAIRVEIL_DAPP_HOST: "127.0.0.1",
      CLAIRVEIL_DAPP_PORT: String(port),
      CLAIRVEIL_TRANSPORT: "evm",
      CHAIN_ID: "generic-evm-host-1",
      CLAIRVEIL_DENOM: "globalcoin",
      CLAIRVEIL_EVM_DENOM: "evmcoin",
      CLAIRVEIL_EVM_NATIVE_DENOM: "evmcoin",
      CLAIRVEIL_EVM_DEPOSIT_MODE: "payable-exact-value",
      CLAIRVEIL_EVM_DISPLAY_DENOM: "EVM",
      CLAIRVEIL_EVM_AUTHORIZATION_PROFILE: "",
      CLAIRVEIL_DEPOSIT_PROOF_URL: "",
      CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));

  try {
    const baseUrl = `http://127.0.0.1:${port}`;
    const config = await waitForJson(`${baseUrl}/api/config`, child, stderr);
    assert.equal(config.response.status, 200);
    assert.equal(config.json.denom, "evmcoin");
    assert.equal(config.json.evmNativeDenom, "evmcoin");
    assert.equal(config.json.chainProfiles[0].denom, "evmcoin");

    const rejected = await fetch(`${baseUrl}/api/faucet`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        from: "dev0",
        recipient: "0x0000000000000000000000000000000000000001",
        amount: "1globalcoin",
      }),
    });
    assert.equal(rejected.status, 400);
    assert.equal((await rejected.json()).error, "amount must look like 1evmcoin");
  } finally {
    await stopChild(child);
  }
});
