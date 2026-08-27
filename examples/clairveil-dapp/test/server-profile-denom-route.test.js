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

function environmentWithout(names, overrides = {}) {
  const environment = { ...process.env, ...overrides };
  for (const name of names) delete environment[name];
  return environment;
}

async function startConfigServer(environment) {
  const port = await freePort();
  const child = spawn(process.execPath, ["server.js"], {
    cwd: new URL("..", import.meta.url),
    env: {
      ...environment,
      PORT: String(port),
      CLAIRVEIL_DAPP_HOST: "127.0.0.1",
      CLAIRVEIL_DAPP_PORT: String(port),
    },
    stdio: ["ignore", "pipe", "pipe"],
  });
  const stderr = [];
  child.stderr.on("data", chunk => stderr.push(String(chunk)));
  return {
    child,
    stderr,
    config: await waitForJson(`http://127.0.0.1:${port}/api/config`, child, stderr),
  };
}

test("Cosmos profile gives transport-specific browser endpoints precedence", async () => {
  const environment = environmentWithout([], {
    CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
    CLAIRVEIL_TRANSPORT: "cosmos",
    CHAIN_ID: "cosmos-profile-1",
    CLAIRVEIL_COSMOS_CHAIN_ID: "cosmos-profile-2",
    CLAIRVEIL_RPC: "tcp://internal.example:26657",
    CLAIRVEIL_REST: "http://internal.example:1317",
    CLAIRVEIL_PUBLIC_RPC: "https://rpc.public.example",
    CLAIRVEIL_PUBLIC_REST: "https://rest.public.example",
    CLAIRVEIL_PUBLIC_REST_ENDPOINTS: "https://rest-public-backup.example",
    CLAIRVEIL_COSMOS_RPC: "https://rpc.cosmos.example",
    CLAIRVEIL_COSMOS_REST: "https://rest.cosmos.example",
    CLAIRVEIL_COSMOS_REST_ENDPOINTS: "https://rest-cosmos-backup.example",
    CLAIRVEIL_COSMOS_CHAIN_NAME: "Cosmos Profile 2",
    CLAIRVEIL_COSMOS_LABEL: "Cosmos Browser",
    CLAIRVEIL_PUBLIC_PROVER_URL: "https://prover.public.example",
    CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "",
    CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL: "https://proof.cosmos.example/v1/prove",
    CLAIRVEIL_DISPLAY_DENOM: "SHARED",
    CLAIRVEIL_COIN_DECIMALS: "7",
    CLAIRVEIL_ACCOUNT_PREFIX: "shared",
    CLAIRVEIL_SHIELDED_PREFIX: "shareds",
    CLAIRVEIL_COSMOS_DISPLAY_DENOM: "COSMOS",
    CLAIRVEIL_COSMOS_COIN_DECIMALS: "9",
    CLAIRVEIL_COSMOS_ACCOUNT_PREFIX: "cosmos",
    CLAIRVEIL_COSMOS_SHIELDED_PREFIX: "cosmoss",
    CLAIRVEIL_COSMOS_COIN_TYPE: "60",
    CLAIRVEIL_KEPLR_COIN_TYPE: "118",
  });
  const { child, stderr, config } = await startConfigServer(environment);
  try {
    const profile = config.json.chainProfiles[0];
    assert.equal(config.json.chainId, "cosmos-profile-2");
    assert.equal(config.json.accountPrefix, "cosmos");
    assert.equal(config.json.shieldedPrefix, "cosmoss");
    assert.equal(profile.chainId, config.json.chainId);
    assert.equal(profile.label, "Cosmos Browser");
    assert.equal(profile.chainName, "Cosmos Profile 2");
    assert.equal(profile.keplrChainInfo.chainName, profile.chainName);
    assert.equal(profile.rpc, "https://rpc.cosmos.example");
    assert.equal(profile.rest, "https://rest.cosmos.example");
    assert.deepEqual(profile.restEndpoints, [
      "https://rest.cosmos.example",
      "https://rest-cosmos-backup.example",
    ]);
    assert.equal(profile.depositProofUrl, "https://proof.cosmos.example/v1/prove");
    assert.equal(config.json.serverFeatures.depositProof, true);
    assert.equal(profile.displayDenom, "COSMOS");
    assert.equal(profile.coinDecimals, 9);
    assert.equal(profile.accountPrefix, "cosmos");
    assert.equal(profile.shieldedPrefix, "cosmoss");
    assert.equal(profile.keplrCoinType, 60);
    assert.equal(profile.keplrChainInfo.bip44.coinType, 60);
    assert.equal(profile.keplrChainInfo.rpc, profile.rpc);
    assert.equal(profile.keplrChainInfo.rest, profile.rest);

    const baseUrl = new URL(config.response.url).origin;
    const wrongPrefix = await fetch(`${baseUrl}/api/faucet`, {
      method: "POST",
      headers: {
        "content-type": "application/json",
        origin: baseUrl,
      },
      body: JSON.stringify({
        from: "alice",
        recipient: "shared1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq",
        amount: `1${profile.denom}`,
      }),
    });
    assert.equal(wrongPrefix.status, 400);
    assert.match((await wrongPrefix.json()).error, /invalid cosmos address/);
  } finally {
    await stopChild(child);
    assert.equal(stderr.join("").trim(), "");
  }
});

test("Cosmos profile falls back to shared public browser endpoints", async () => {
  const environment = environmentWithout([
    "CLAIRVEIL_COSMOS_RPC",
    "CLAIRVEIL_COSMOS_REST",
    "CLAIRVEIL_COSMOS_REST_ENDPOINTS",
    "CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL",
    "CLAIRVEIL_COSMOS_CHAIN_NAME",
    "CLAIRVEIL_COSMOS_LABEL",
  ], {
    CLAIRVEIL_DAPP_LOCAL_TEST_MODE: "1",
    CLAIRVEIL_TRANSPORT: "cosmos",
    CHAIN_ID: "cosmos-profile-1",
    CLAIRVEIL_COSMOS_CHAIN_ID: "  ",
    CLAIRVEIL_RPC: "tcp://internal.example:26657",
    CLAIRVEIL_REST: "http://internal.example:1317",
    CLAIRVEIL_PUBLIC_RPC: "https://rpc.public.example",
    CLAIRVEIL_PUBLIC_REST: "https://rest.public.example",
    CLAIRVEIL_PUBLIC_REST_ENDPOINTS: "https://rest-public-backup.example",
    CLAIRVEIL_PUBLIC_PROVER_URL: "https://prover.public.example",
    CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL: "https://proof.public.example/v1/prove",
    CLAIRVEIL_DISPLAY_DENOM: "SHARED",
    CLAIRVEIL_COIN_DECIMALS: "7",
    CLAIRVEIL_ACCOUNT_PREFIX: "shared",
    CLAIRVEIL_SHIELDED_PREFIX: "shareds",
    CLAIRVEIL_COSMOS_DISPLAY_DENOM: "",
    CLAIRVEIL_COSMOS_COIN_DECIMALS: "",
    CLAIRVEIL_COSMOS_ACCOUNT_PREFIX: "  ",
    CLAIRVEIL_COSMOS_SHIELDED_PREFIX: "  ",
    CLAIRVEIL_COSMOS_COIN_TYPE: "  ",
    CLAIRVEIL_KEPLR_COIN_TYPE: "330",
  });
  const { child, stderr, config } = await startConfigServer(environment);
  try {
    const profile = config.json.chainProfiles[0];
    assert.equal(profile.chainId, "cosmos-profile-1");
    assert.equal(profile.label, "Clairveil Localnet");
    assert.equal(profile.chainName, "Clairveil Localnet");
    assert.equal(profile.keplrChainInfo.chainName, profile.chainName);
    assert.equal(profile.rpc, "https://rpc.public.example");
    assert.equal(profile.rest, "https://rest.public.example");
    assert.deepEqual(profile.restEndpoints, [
      "https://rest.public.example",
      "https://rest-public-backup.example",
    ]);
    assert.equal(profile.depositProofUrl, "https://proof.public.example/v1/prove");
    assert.equal(config.json.serverFeatures.depositProof, true);
    assert.equal(profile.displayDenom, "SHARED");
    assert.equal(profile.coinDecimals, 7);
    assert.equal(profile.accountPrefix, "shared");
    assert.equal(profile.shieldedPrefix, "shareds");
    assert.equal(profile.keplrCoinType, 330);
    assert.equal(profile.keplrChainInfo.bip44.coinType, 330);
  } finally {
    await stopChild(child);
    assert.equal(stderr.join("").trim(), "");
  }
});

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
      CLAIRVEIL_EVM_PRIVACY_PRECOMPILE: "0x0000000000000000000000000000000000000808",
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
      headers: {
        "content-type": "application/json",
        origin: baseUrl,
      },
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
