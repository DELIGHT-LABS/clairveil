import test from "node:test";
import assert from "node:assert/strict";

import {
  assertDirectEndpointResponse,
  assertRestrictiveConnectSrc,
  assertRestrictiveFrameAncestors,
  assertRestrictiveScriptSrc,
  deploymentEndpoints,
  validateDeployedWebAppConfig,
  verifyProductionDeployment,
} from "../tools/verify-production-deployment.mjs";

const cosmosProfile = {
  id: "cosmos-mainnet",
  label: "Cosmos Mainnet",
  chainName: "Cosmos Mainnet",
  transport: "cosmos",
  wallet: "keplr",
  chainId: "cosmos-mainnet-1",
  rest: "https://rest.example.com",
  restEndpoints: [
    "https://rest.example.com",
    "https://rest-backup.example.com",
  ],
  rpc: "https://rpc.example.com",
  proverUrl: "https://prover.example.com",
  depositProofUrl: "https://deposit-proof.example.com/v1/prove",
  accountPrefix: "clair",
  shieldedPrefix: "clairs",
  denom: "uclair",
  displayDenom: "CLAIR",
  coinDecimals: 18,
  keplrCoinType: 118,
  gasPriceStep: { low: 1, average: 1, high: 1 },
  keplrChainInfo: {
    chainId: "cosmos-mainnet-1",
    chainName: "Cosmos Mainnet",
    rest: "https://rest.example.com",
    rpc: "https://rpc.example.com",
    bip44: { coinType: 118 },
    bech32Config: {
      bech32PrefixAccAddr: "clair",
      bech32PrefixAccPub: "clairpub",
      bech32PrefixValAddr: "clairvaloper",
      bech32PrefixValPub: "clairvaloperpub",
      bech32PrefixConsAddr: "clairvalcons",
      bech32PrefixConsPub: "clairvalconspub",
    },
    currencies: [{ coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 }],
    feeCurrencies: [{
      coinDenom: "CLAIR",
      coinMinimalDenom: "uclair",
      coinDecimals: 18,
      gasPriceStep: { low: 1, average: 1, high: 1 },
    }],
    stakeCurrency: { coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 },
    features: [],
  },
};

const evmProfile = {
  id: "evm-mainnet",
  label: "EVM Mainnet",
  chainName: "EVM Mainnet",
  transport: "evm",
  wallet: "metamask",
  chainId: "evm-host-mainnet-1",
  rest: "https://evm-rest.example.com",
  rpc: "https://evm-host-rpc.example.com",
  proverUrl: "https://evm-prover.example.com",
  accountPrefix: "clair",
  shieldedPrefix: "clairs",
  denom: "aokrw",
  displayDenom: "OKRW",
  coinDecimals: 18,
  evmRpc: "https://evm-rpc.example.com",
  evmChainId: "0x539",
  evmChainName: "EVM Mainnet",
  evmPrivacyPrecompileAddress: "0x0000000000000000000000000000000000000808",
  evmDepositMode: "payable-exact-value",
  evmNativeDenom: "aokrw",
  evmGasLimit: "0x2dc6c0",
  evmSendGasLimit: "0x5208",
};

const deployedConfig = {
  schemaVersion: "clairveil-web-client-config-v1",
  serverBacked: true,
  serverFeatures: { batchTransfer: false },
  activeChainProfileId: cosmosProfile.id,
  chainProfiles: [cosmosProfile, evmProfile],
};

const profileEnvironment = {
  CLAIRVEIL_WEBAPP_ORIGIN: "https://app.example.com",
  CLAIRVEIL_WEBAPP_CONFIG_URL: "https://app.example.com/api/health",
};

const restrictiveCsp = "default-src 'self'; frame-ancestors 'none'; script-src 'self'; connect-src 'self' https://rest.example.com https://rest-backup.example.com https://rpc.example.com https://prover.example.com https://deposit-proof.example.com https://evm-rest.example.com https://evm-host-rpc.example.com https://evm-prover.example.com https://evm-rpc.example.com";

function reverseObjectKeyOrder(value) {
  if (Array.isArray(value)) return value.map((item) => reverseObjectKeyOrder(item));
  if (!value || typeof value !== "object") return value;
  return Object.fromEntries(
    Object.entries(value)
      .reverse()
      .map(([key, item]) => [key, reverseObjectKeyOrder(item)]),
  );
}

function responseAt(response, url) {
  Object.defineProperty(response, "url", {
    configurable: true,
    value: new URL(url).href,
  });
  return response;
}

function productionGateFetch({
  config = deployedConfig,
  configPath = "/api/health",
  informationalConfig = config,
  corsHeaders,
  actualCorsHeaders,
  endpointResponse,
  configResponseOverride,
  informationalConfigResponseOverride,
  webAppCsp = restrictiveCsp,
} = {}) {
  const calls = [];
  const fetchImpl = async (url, options = {}) => {
    const requestUrl = new URL(url);
    const method = options.method || "GET";
    const headers = options.headers || {};
    const requestedMethod = headers["Access-Control-Request-Method"]
      || headers.get?.("Access-Control-Request-Method")
      || "";
    calls.push({
      url: requestUrl.toString(),
      method,
      origin: headers.Origin || "",
      requestedMethod,
      redirect: options.redirect || "follow",
    });
    if (method === "OPTIONS") {
      return responseAt(new Response(null, {
        status: 204,
        headers: corsHeaders?.(headers.Origin || "", {
          requestUrl,
          requestedMethod,
        }) || {
          "access-control-allow-origin": "https://app.example.com",
          "access-control-allow-methods": "GET, POST, OPTIONS",
          "access-control-allow-headers": "Content-Type",
        },
      }), requestUrl);
    }
    const responseCorsHeaders = headers.Origin
      ? actualCorsHeaders?.(headers.Origin || "", { requestUrl, method }) || {
          "access-control-allow-origin": "https://app.example.com",
        }
      : {};
    if (requestUrl.pathname === configPath) {
      if (configResponseOverride) return configResponseOverride;
      const body = configPath === "/api/health" ? { config } : config;
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "content-type": "application/json; charset=utf-8" },
      });
    }
    if (requestUrl.pathname === "/api/config") {
      if (informationalConfigResponseOverride) {
        return informationalConfigResponseOverride;
      }
      return new Response(JSON.stringify(informationalConfig), {
        status: 200,
        headers: { "content-type": "application/json; charset=utf-8" },
      });
    }
    const overriddenEndpointResponse = endpointResponse?.({ requestUrl, method, headers });
    if (overriddenEndpointResponse) {
      return responseAt(overriddenEndpointResponse, requestUrl);
    }
    return responseAt(new Response(JSON.stringify({ ok: true }), {
      status: 200,
      headers: {
        "content-type": "application/json; charset=utf-8",
        "content-security-policy": webAppCsp,
        "x-content-type-options": "nosniff",
        "referrer-policy": "no-referrer",
        "cross-origin-opener-policy": "same-origin",
        ...responseCorsHeaders,
      },
    }), requestUrl);
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

test("production gate rejects an external script-src-elem override", () => {
  assert.throws(
    () => assertRestrictiveScriptSrc("default-src 'self'; script-src 'self'; script-src-elem https://cdn.example.com; connect-src 'self'"),
    /script-src-elem must allow only 'self'/,
  );
});

test("production gate rejects an inline script-src-attr override", () => {
  assert.throws(
    () => assertRestrictiveScriptSrc("default-src 'self'; script-src 'self'; script-src-attr 'unsafe-inline'; connect-src 'self'"),
    /script-src-attr must allow only 'none'/,
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

test("production gate validates the complete browser configuration contract", () => {
  const validated = validateDeployedWebAppConfig({ config: deployedConfig });
  assert.equal(validated.activeProfile.id, cosmosProfile.id);

  for (const invalidConfig of [
    { ...deployedConfig, schemaVersion: "legacy" },
    { ...deployedConfig, activeChainProfileId: "missing" },
    {
      ...deployedConfig,
      chainProfiles: [{ ...cosmosProfile, denom: undefined }, evmProfile],
    },
    { ...deployedConfig, rpc: "https://different-rpc.example.com" },
    { ...deployedConfig, unknownDeploymentField: true },
  ]) {
    assert.throws(
      () => deploymentEndpoints(invalidConfig),
      /deployed WebApp config is invalid/,
    );
  }
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
      /profile\.proverUrl must be an http\(s\) URL without query, fragment, or credentials/,
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
  assert.deepEqual(calls.slice(0, 3).map((call) => [call.method, call.url]), [
    ["GET", "https://app.example.com/"],
    ["GET", "https://app.example.com/api/health"],
    ["GET", "https://app.example.com/api/config"],
  ]);
  const corsCalls = calls.slice(3);
  assert.equal(corsCalls.length, 64);
  assert.equal(corsCalls.every((call) => call.redirect === "error"), true);
  assert.equal(
    corsCalls.filter((call) => call.origin === "https://app.example.com").length,
    32,
  );
  assert.equal(
    corsCalls.filter((call) => call.origin === "https://clairveil-cors-probe.invalid").length,
    32,
  );
  const trustedPreflights = corsCalls
    .filter((call) => call.method === "OPTIONS" && call.origin === "https://app.example.com")
    .map((call) => [new URL(call.url).host, new URL(call.url).pathname, call.requestedMethod]);
  assert.ok(trustedPreflights.some((call) => call[0] === "rest.example.com"
    && call[1] === "/clairveil/privacy/v1/tree_state" && call[2] === "GET"));
  assert.ok(trustedPreflights.some((call) => call[0] === "rest.example.com"
    && call[1] === "/clairveil/privacy/v1/privacy_scan" && call[2] === "POST"));
  assert.ok(trustedPreflights.some((call) => call[0] === "rpc.example.com"
    && call[1] === "/" && call[2] === "POST"));
  assert.ok(trustedPreflights.some((call) => call[0] === "rpc.example.com"
    && call[1] === "/status" && call[2] === "GET"));
  assert.ok(trustedPreflights.some((call) => call[0] === "prover.example.com"
    && call[1] === "/v1/prover/transfer" && call[2] === "POST"));
  assert.ok(trustedPreflights.some((call) => call[0] === "prover.example.com"
    && call[1] === "/v1/prover/withdraw" && call[2] === "POST"));
});

test("production gate probes same-origin gateways without requiring CORS", async () => {
  const sameOriginProfile = {
    ...cosmosProfile,
    id: "same-origin-cosmos",
    rest: "https://app.example.com/cosmos-rest",
    restEndpoints: ["https://app.example.com/cosmos-rest"],
    rpc: "https://app.example.com/cosmos-rpc",
    proverUrl: "https://app.example.com/prover",
    depositProofUrl: "https://app.example.com/deposit-proof",
    keplrChainInfo: {
      ...cosmosProfile.keplrChainInfo,
      rest: "https://app.example.com/cosmos-rest",
      rpc: "https://app.example.com/cosmos-rpc",
    },
  };
  const config = {
    ...deployedConfig,
    activeChainProfileId: sameOriginProfile.id,
    chainProfiles: [sameOriginProfile],
  };
  const sameOrigin = productionGateFetch({
    config,
    informationalConfig: config,
    actualCorsHeaders: () => ({}),
    webAppCsp: "default-src 'self'; frame-ancestors 'none'; script-src 'self'; connect-src 'self'",
  });

  const result = await verifyProductionDeployment({
    environment: profileEnvironment,
    fetchImpl: sameOrigin.fetchImpl,
  });

  assert.deepEqual(result, { profileCount: 1, endpointCount: 4 });
  const endpointCalls = sameOrigin.calls.slice(3);
  assert.equal(endpointCalls.length, 7);
  assert.equal(endpointCalls.some((call) => call.method === "OPTIONS"), false);
  assert.equal(endpointCalls.every((call) => new URL(call.url).origin === profileEnvironment.CLAIRVEIL_WEBAPP_ORIGIN), true);
});

test("production gate rejects missing or non-JSON endpoint routes", async () => {
  const missingRoute = productionGateFetch({
    endpointResponse: ({ requestUrl, method }) => (
      requestUrl.hostname === "rest.example.com"
        && requestUrl.pathname.endsWith("/tree_state")
        && method === "GET"
        ? new Response(JSON.stringify({ error: "missing" }), {
            status: 404,
            headers: {
              "content-type": "application/json",
              "access-control-allow-origin": profileEnvironment.CLAIRVEIL_WEBAPP_ORIGIN,
            },
          })
        : null
    ),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: missingRoute.fetchImpl,
    }),
    /tree_state GET probe failed with HTTP 404/,
  );

  const htmlFallback = productionGateFetch({
    endpointResponse: ({ requestUrl, method }) => (
      requestUrl.hostname === "rest.example.com"
        && requestUrl.pathname.endsWith("/privacy_scan")
        && method === "POST"
        ? new Response("<!doctype html><title>fallback</title>", {
            status: 200,
            headers: {
              "content-type": "text/html",
              "access-control-allow-origin": profileEnvironment.CLAIRVEIL_WEBAPP_ORIGIN,
            },
          })
        : null
    ),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: htmlFallback.fetchImpl,
    }),
    /privacy_scan must return a bounded JSON response/,
  );

  const missingWithdrawRoute = productionGateFetch({
    endpointResponse: ({ requestUrl, method }) => (
      requestUrl.hostname === "prover.example.com"
        && requestUrl.pathname === "/v1/prover/withdraw"
        && method === "POST"
        ? new Response(JSON.stringify({ error: "missing" }), {
            status: 404,
            headers: {
              "content-type": "application/json",
              "access-control-allow-origin": profileEnvironment.CLAIRVEIL_WEBAPP_ORIGIN,
            },
          })
        : null
    ),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: missingWithdrawRoute.fetchImpl,
    }),
    /cosmos-mainnet\.proverUrl withdraw POST route probe failed with HTTP 404/,
  );
});

test("production gate rejects CORS policies that omit required REST POST or RPC methods", async () => {
  const missingRestPost = productionGateFetch({
    corsHeaders: (origin, { requestUrl }) => ({
      "access-control-allow-origin": origin === "https://app.example.com" ? origin : "",
      "access-control-allow-methods": requestUrl.pathname.endsWith("/privacy_scan")
        ? "GET, OPTIONS"
        : "GET, POST, OPTIONS",
      "access-control-allow-headers": "Content-Type",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: missingRestPost.fetchImpl,
    }),
    /privacy_scan CORS preflight does not allow POST/,
  );

  const missingRpcGet = productionGateFetch({
    corsHeaders: (origin, { requestUrl }) => ({
      "access-control-allow-origin": origin === "https://app.example.com" ? origin : "",
      "access-control-allow-methods": requestUrl.pathname === "/status"
        ? "POST, OPTIONS"
        : "GET, POST, OPTIONS",
      "access-control-allow-headers": "Content-Type",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: missingRpcGet.fetchImpl,
    }),
    /status CORS preflight does not allow GET/,
  );

  const missingRpcPost = productionGateFetch({
    corsHeaders: (origin, { requestUrl, requestedMethod }) => ({
      "access-control-allow-origin": origin === "https://app.example.com" ? origin : "",
      "access-control-allow-methods": requestUrl.hostname === "rpc.example.com"
        && requestUrl.pathname === "/"
        && requestedMethod === "POST"
        ? "GET, OPTIONS"
        : "GET, POST, OPTIONS",
      "access-control-allow-headers": "Content-Type",
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: missingRpcPost.fetchImpl,
    }),
    /JSON-RPC CORS preflight does not allow POST/,
  );
});

test("production gate rejects redirected or rewritten profile endpoint probes", () => {
  const endpoint = new URL("https://prover.example.com/v1/prover/transfer");
  assert.throws(
    () => assertDirectEndpointResponse({ redirected: false, url: "" }, endpoint, "cosmos-mainnet.proverUrl"),
    /response URL is missing/,
  );
  assert.throws(
    () => assertDirectEndpointResponse({
      redirected: true,
      url: "https://other.example/v1/prover/transfer",
    }, endpoint, "cosmos-mainnet.proverUrl"),
    /must not redirect/,
  );
  assert.throws(
    () => assertDirectEndpointResponse({
      redirected: false,
      url: "https://prover.example.com/rewritten",
    }, endpoint, "cosmos-mainnet.proverUrl"),
    /must be served directly from its configured URL/,
  );
});

test("server-backed gate requires exact canonical equality across health and config routes", async () => {
  const reordered = productionGateFetch({
    informationalConfig: reverseObjectKeyOrder(deployedConfig),
  });
  await verifyProductionDeployment({
    environment: profileEnvironment,
    fetchImpl: reordered.fetchImpl,
  });

  const mismatches = [
    ["active profile", {
      ...deployedConfig,
      activeChainProfileId: evmProfile.id,
    }],
    ["profile metadata", {
      ...deployedConfig,
      chainProfiles: [
        { ...cosmosProfile, label: "Different Cosmos Profile" },
        evmProfile,
      ],
    }],
    ["endpoint", {
      ...deployedConfig,
      chainProfiles: [
        { ...cosmosProfile, proverUrl: "https://other-prover.example.com" },
        evmProfile,
      ],
    }],
    ["feature", {
      ...deployedConfig,
      serverFeatures: { batchTransfer: false, proverProxy: true },
    }],
  ];
  for (const [label, informationalConfig] of mismatches) {
    const mismatched = productionGateFetch({ informationalConfig });
    await assert.rejects(
      () => verifyProductionDeployment({
        environment: profileEnvironment,
        fetchImpl: mismatched.fetchImpl,
      }),
      /\/api\/health\.config must match \/api\/config/,
      label,
    );
  }
});

test("server-backed gate validates the informational config against the complete schema", async () => {
  const invalid = productionGateFetch({
    informationalConfig: {
      ...deployedConfig,
      schemaVersion: "legacy",
    },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: invalid.fetchImpl,
    }),
    /deployed WebApp config is invalid: config\.schemaVersion/,
  );
});

test("server-backed gate requires strict health and informational config shapes", async () => {
  const bareHealth = productionGateFetch({
    configResponseOverride: new Response(JSON.stringify(deployedConfig), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: bareHealth.fetchImpl,
    }),
    /\/api\/health must contain the browser config under config/,
  );

  const wrappedInformationalConfig = productionGateFetch({
    informationalConfig: { config: deployedConfig },
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: wrappedInformationalConfig.fetchImpl,
    }),
    /\/api\/config must return the bare Web client config/,
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

test("production gate does not accept a lookalike default-src directive", async () => {
  const unchecked = productionGateFetch({
    webAppCsp: restrictiveCsp.replace("default-src", "x-default-src"),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unchecked.fetchImpl,
    }),
    /default-src must allow only 'self'/,
  );
});

test("production gate rejects a widened default-src directive", async () => {
  const unchecked = productionGateFetch({
    webAppCsp: restrictiveCsp.replace(
      "default-src 'self'",
      "default-src 'self' https://cdn.example.com",
    ),
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unchecked.fetchImpl,
    }),
    /default-src must allow only 'self'/,
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
      url: "https://app.example.com/api/health",
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
      url: "https://app.example.com/api/health",
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
  assert.equal(
    accepted.calls.some((call) => new URL(call.url).pathname === "/api/config"),
    false,
  );

  const wrappedStatic = productionGateFetch({
    config: { config: staticConfig },
    configPath: "/dapp-config.json",
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: staticEnvironment,
      fetchImpl: wrappedStatic.fetchImpl,
    }),
    /\/dapp-config\.json must return the bare Web client config/,
  );

  const unbound = productionGateFetch({ config: staticConfig });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: profileEnvironment,
      fetchImpl: unbound.fetchImpl,
    }),
    /must use the browser-loaded \/dapp-config\.json response/,
  );
});

test("production gate permits server opt-in but keeps static batch disabled", async () => {
  const staticEnvironment = {
    ...profileEnvironment,
    CLAIRVEIL_WEBAPP_CONFIG_URL: "https://app.example.com/dapp-config.json",
  };
  const serverBackedConfig = {
    ...deployedConfig,
    serverFeatures: {
      ...deployedConfig.serverFeatures,
      batchTransfer: true,
    },
  };
  const serverBacked = productionGateFetch({ config: serverBackedConfig });
  await verifyProductionDeployment({
    environment: profileEnvironment,
    fetchImpl: serverBacked.fetchImpl,
  });

  const staticConfig = {
    ...serverBackedConfig,
    serverBacked: false,
  };
  const staticDeployment = productionGateFetch({
    config: staticConfig,
    configPath: "/dapp-config.json",
  });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: staticEnvironment,
      fetchImpl: staticDeployment.fetchImpl,
    }),
    /Static Clairveil v0\.3\.1 WebApp deployments require serverFeatures\.batchTransfer=false/,
  );
});

test("production gate requires the browser-loaded health response for a server-backed DApp", async () => {
  const unboundEnvironment = {
    ...profileEnvironment,
    CLAIRVEIL_WEBAPP_CONFIG_URL: "https://app.example.com/api/config",
  };
  const unbound = productionGateFetch({ configPath: "/api/config" });
  await assert.rejects(
    () => verifyProductionDeployment({
      environment: unboundEnvironment,
      fetchImpl: unbound.fetchImpl,
    }),
    /must use the browser-loaded \/api\/health response/,
  );
});
