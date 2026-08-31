import test from "node:test";
import assert from "node:assert/strict";

import {
  assertRestrictiveConnectSrc,
  assertRestrictiveFrameAncestors,
  assertRestrictiveScriptSrc,
  deploymentEndpoints,
  verifyProductionDeployment,
} from "../tools/verify-production-deployment.mjs";

const deployedConfig = {
  chainProfiles: [
    {
      id: "cosmos-mainnet",
      transport: "cosmos",
      rest: "https://rest.example.com",
      restEndpoints: [
        "https://rest.example.com",
        "https://rest-backup.example.com",
      ],
      rpc: "https://rpc.example.com",
      proverUrl: "https://prover.example.com",
      depositProofUrl: "https://deposit-proof.example.com/v1/prove",
      keplrChainInfo: {
        rest: "https://rest.example.com",
        rpc: "https://rpc.example.com",
      },
    },
    {
      id: "evm-mainnet",
      transport: "evm",
      rest: "https://evm-rest.example.com",
      rpc: "https://evm-host-rpc.example.com",
      proverUrl: "https://evm-prover.example.com",
      evmRpc: "https://evm-rpc.example.com",
    },
  ],
};

const profileEnvironment = {
  CLAIRVEIL_WEBAPP_ORIGIN: "https://app.example.com",
  CLAIRVEIL_WEBAPP_CONFIG_URL: "https://app.example.com/api/config",
};

const restrictiveCsp = "default-src 'self'; frame-ancestors 'none'; script-src 'self'; connect-src 'self' https://rest.example.com https://rest-backup.example.com https://rpc.example.com https://prover.example.com https://deposit-proof.example.com https://evm-rest.example.com https://evm-host-rpc.example.com https://evm-prover.example.com https://evm-rpc.example.com";

function productionGateFetch({
  config = deployedConfig,
  configPath = "/api/config",
  corsHeaders,
  actualCorsHeaders,
  configResponseOverride,
  webAppCsp = restrictiveCsp,
} = {}) {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    const requestUrl = new URL(url);
    const method = options.method || "GET";
    const headers = options.headers || {};
    calls.push({ url: requestUrl.toString(), method, origin: headers.Origin || "" });
    if (method === "OPTIONS") {
      return new Response(null, {
        status: 204,
        headers: corsHeaders?.(headers.Origin || "") || {
          "access-control-allow-origin": "https://app.example.com",
          "access-control-allow-methods": "GET, POST, OPTIONS",
          "access-control-allow-headers": "Content-Type",
        },
      });
    }
    const responseCorsHeaders = headers.Origin
      ? actualCorsHeaders?.(headers.Origin || "") || {
          "access-control-allow-origin": "https://app.example.com",
        }
      : {};
    if (requestUrl.pathname === configPath) {
      if (configResponseOverride) return configResponseOverride;
      return new Response(JSON.stringify(config), {
        status: 200,
        headers: { "content-type": "application/json; charset=utf-8" },
      });
    }
    return new Response("ok", {
      status: 200,
      headers: {
        "content-security-policy": webAppCsp,
        "x-content-type-options": "nosniff",
        "referrer-policy": "no-referrer",
        "cross-origin-opener-policy": "same-origin",
        ...responseCorsHeaders,
      },
    });
  };
  return { calls, fetchImpl };
}

test("production gate rejects broad connect-src even when required origins are listed", () => {
  assert.throws(
    () => assertRestrictiveConnectSrc("default-src 'self'; connect-src * https://rest.example.com"),
    /connect-src must not allow \*/,
  );
  assert.throws(
    () => assertRestrictiveConnectSrc("default-src 'self'; connect-src 'self' https:"),
    /connect-src must enumerate exact HTTPS origins/,
  );
  assert.throws(
    () => assertRestrictiveConnectSrc("default-src 'self'; connect-src 'self' https://*.example.com"),
    /connect-src must enumerate exact HTTPS origins/,
  );
});

test("production gate requires a restrictive anti-framing CSP", () => {
  assert.throws(
    () => assertRestrictiveFrameAncestors("default-src 'self'; connect-src 'self'"),
    /missing a frame-ancestors CSP directive/,
  );
  assert.throws(
    () => assertRestrictiveFrameAncestors("default-src 'self'; frame-ancestors *; connect-src 'self'"),
    /frame-ancestors must not allow \*/,
  );
  assert.throws(
    () => assertRestrictiveFrameAncestors("default-src 'self'; frame-ancestors https:; connect-src 'self'"),
    /frame-ancestors must enumerate exact HTTPS origins/,
  );
});

test("production gate requires same-origin scripts only", () => {
  assert.throws(
    () => assertRestrictiveScriptSrc("default-src 'self'; connect-src 'self'"),
    /missing a script-src CSP directive/,
  );
  assert.throws(
    () => assertRestrictiveScriptSrc("default-src 'self'; script-src 'self' https://cdn.example.com; connect-src 'self'"),
    /script-src must allow only 'self'/,
  );
  assert.throws(
    () => assertRestrictiveScriptSrc("default-src 'self'; script-src 'self' 'unsafe-inline'; connect-src 'self'"),
    /script-src must allow only 'self'/,
  );
});

test("production gate enumerates every profile and REST failover", () => {
  const endpoints = deploymentEndpoints(deployedConfig);
  assert.deepEqual(
    endpoints.map((endpoint) => [endpoint.profileId, endpoint.label, endpoint.kind, endpoint.url.origin]),
    [
      ["cosmos-mainnet", "cosmos-mainnet.rest", "rest", "https://rest.example.com"],
      ["cosmos-mainnet", "cosmos-mainnet.restEndpoints[1]", "rest", "https://rest-backup.example.com"],
      ["cosmos-mainnet", "cosmos-mainnet.rpc", "rpc", "https://rpc.example.com"],
      ["cosmos-mainnet", "cosmos-mainnet.proverUrl", "prover", "https://prover.example.com"],
      ["cosmos-mainnet", "cosmos-mainnet.depositProofUrl", "deposit-proof", "https://deposit-proof.example.com"],
      ["evm-mainnet", "evm-mainnet.rest", "rest", "https://evm-rest.example.com"],
      ["evm-mainnet", "evm-mainnet.rpc", "rpc", "https://evm-host-rpc.example.com"],
      ["evm-mainnet", "evm-mainnet.proverUrl", "prover", "https://evm-prover.example.com"],
      ["evm-mainnet", "evm-mainnet.evmRpc", "evm-rpc", "https://evm-rpc.example.com"],
    ],
  );
});

test("production gate rejects secret-bearing endpoint URLs", () => {
  for (const proverUrl of [
    "https://token:secret@prover.example.com",
    "https://prover.example.com?token=secret",
    "https://prover.example.com#token",
  ]) {
    const invalidConfig = {
      ...deployedConfig,
      chainProfiles: deployedConfig.chainProfiles.map((profile) =>
        profile.id === "cosmos-mainnet" ? { ...profile, proverUrl } : profile,
      ),
    };
    assert.throws(
      () => deploymentEndpoints(invalidConfig),
      /cosmos-mainnet\.proverUrl must be a valid HTTPS URL/,
    );
  }
});

test("production gate verifies preflight and actual CORS for every configured endpoint", async () => {
  const { calls, fetchImpl } = productionGateFetch();

  const result = await verifyProductionDeployment({
    environment: profileEnvironment,
    fetchImpl,
  });

  assert.deepEqual(result, { profileCount: 2, endpointCount: 9 });
  assert.deepEqual(calls.slice(0, 2).map((call) => [call.method, call.url]), [
    ["GET", "https://app.example.com/"],
    ["GET", "https://app.example.com/api/config"],
  ]);
  const corsCalls = calls.slice(2);
  assert.equal(corsCalls.length, 36);
  assert.equal(
    corsCalls.filter((call) => call.origin === "https://app.example.com").length,
    18,
  );
  assert.equal(
    corsCalls.filter((call) => call.origin === "https://clairveil-cors-probe.invalid").length,
    18,
  );
});

test("production gate rejects reflected origins and unnecessary CORS permissions", async () => {
  const reflected = productionGateFetch({
    corsHeaders: (origin) => ({
      "access-control-allow-origin": origin,
      "access-control-allow-methods": "GET, POST, OPTIONS",
      "access-control-allow-headers": "Content-Type",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({ environment: profileEnvironment, fetchImpl: reflected.fetchImpl }),
    /must not allow an untrusted WebApp origin/,
  );

  const permissive = productionGateFetch({
    corsHeaders: () => ({
      "access-control-allow-origin": "https://app.example.com",
      "access-control-allow-methods": "GET, POST, OPTIONS, DELETE",
      "access-control-allow-headers": "Content-Type, X-Admin",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({ environment: profileEnvironment, fetchImpl: permissive.fetchImpl }),
    /allows an unnecessary method/,
  );

  const actualResponsePermissive = productionGateFetch({
    actualCorsHeaders: (origin) => ({
      "access-control-allow-origin":
        origin === "https://app.example.com" ? origin : "*",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: actualResponsePermissive.fetchImpl,
    }),
    /actual response must not allow an untrusted WebApp origin/,
  );
});

test("production gate requires a same-origin deployed config response", async () => {
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: {
        ...profileEnvironment,
        CLAIRVEIL_WEBAPP_CONFIG_URL: "https://untrusted.example/config.json",
      },
      fetchImpl: productionGateFetch().fetchImpl,
    }),
    /must be served from the final WebApp origin/,
  );
});

test("production gate rejects a final WebApp response without anti-framing protection", async () => {
  const unchecked = productionGateFetch({
    webAppCsp: restrictiveCsp.replace("frame-ancestors 'none'; ", ""),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unchecked.fetchImpl,
    }),
    /missing a frame-ancestors CSP directive/,
  );
});

test("production gate rejects a final WebApp response that allows external scripts", async () => {
  const unchecked = productionGateFetch({
    webAppCsp: restrictiveCsp.replace("script-src 'self'", "script-src 'self' https://cdn.example.com"),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unchecked.fetchImpl,
    }),
    /script-src must allow only 'self'/,
  );
});

test("production gate rejects redirected configuration artifacts", async () => {
  const redirected = productionGateFetch({
    configResponseOverride: {
      ok: true,
      status: 200,
      redirected: true,
      url: "https://untrusted.example/dapp-config.json",
      text: async () => JSON.stringify(deployedConfig),
    },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: redirected.fetchImpl,
    }),
    /WebApp config must not redirect/,
  );
});

test("production gate rejects configuration final URLs that differ from the requested artifact", async () => {
  const redirected = productionGateFetch({
    configResponseOverride: {
      ok: true,
      status: 200,
      redirected: false,
      url: "https://app.example.com/rewritten-config.json",
      text: async () => JSON.stringify(deployedConfig),
    },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: redirected.fetchImpl,
    }),
    /must be served directly from its configured same-origin URL/,
  );
});

test("production gate requires the deployed config MIME type to be JSON", async () => {
  const unchecked = productionGateFetch({
    configResponseOverride: {
      ok: true,
      status: 200,
      redirected: false,
      url: "https://app.example.com/api/config",
      headers: new Headers({ "content-type": "text/html" }),
      text: async () => JSON.stringify(deployedConfig),
    },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unchecked.fetchImpl,
    }),
    /must return Content-Type: application\/json/,
  );
});

test("production gate bounds the deployed config response before parsing it", async () => {
  const oversized = productionGateFetch({
    configResponseOverride: {
      ok: true,
      status: 200,
      redirected: false,
      url: "https://app.example.com/api/config",
      headers: new Headers({
        "content-type": "application/json",
        "content-length": String((1 << 20) + 1),
      }),
      text: async () => JSON.stringify(deployedConfig),
    },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: oversized.fetchImpl,
    }),
    /response exceeds 1048576 byte limit/,
  );
});

test("production gate requires the browser-loaded artifact for a static DApp", async () => {
  const staticConfig = { ...deployedConfig, serverBacked: false };
  const staticEnvironment = {
    ...profileEnvironment,
    CLAIRVEIL_WEBAPP_CONFIG_URL: "https://app.example.com/dapp-config.json",
  };
  const accepted = productionGateFetch({
    config: staticConfig,
    configPath: "/dapp-config.json",
  });
  await verifyProductionDeployment({
    environment: staticEnvironment,
    fetchImpl: accepted.fetchImpl,
  });

  const unbound = productionGateFetch({ config: staticConfig });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unbound.fetchImpl,
    }),
    /must use the browser-loaded \/dapp-config\.json artifact/,
  );
});
