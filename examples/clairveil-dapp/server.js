import { createServer } from "node:http";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createReadStream, existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir, networkInterfaces, tmpdir } from "node:os";
import { FetchRequest, JsonRpcProvider, Wallet } from "ethers";
import {
  createClairveilClient,
  createClairveilEvmClient,
  ClairveilError,
  ClairveilErrorCode,
  bech32AddressToEvm,
  computePreparedTransferPayloadHash,
  computePreparedWithdrawProverPayloadHash,
  derivePrivacyMaterial,
  evmAddressToBech32,
  isEvmAddress,
  evmPrivacyPrecompileAddress,
  plannerStatusToErrorCode
} from "clairveiljs";
import {
  relayPayloadNullifierLockKey,
  submitRelayAfterNullifierPreflight,
} from "./public/relay-reservation-state.js";
import {
  createRelaySubmissionCoordinator,
  relaySubmissionIdempotencyKey,
} from "./public/relay-submission-coordinator.js";
import {
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus,
} from "./public/transaction-status.js";
import {
  sameTypedBatchEventIdentity,
  typedBatchEventIdentity,
} from "./public/batch-event-identity.js";

const __dirname = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(__dirname, "../..");
const publicDir = join(__dirname, "public");
const defaultHome = existsSync("/tmp/clairveil-codex-home-2")
  ? "/tmp/clairveil-codex-home-2"
  : existsSync("/tmp/clairveil-codex-home")
  ? "/tmp/clairveil-codex-home"
  : join(homedir(), ".clairveil");
const relaySubmissionCoordinator = createRelaySubmissionCoordinator();

function readCliOptions(argv = process.argv.slice(2)) {
  const options = {};
  for (let index = 0; index < argv.length; index += 1) {
    const arg = argv[index];
    if (arg === "--host") {
      const next = argv[index + 1];
      options.host = next && !next.startsWith("-") ? next : "0.0.0.0";
      if (next && !next.startsWith("-")) index += 1;
      continue;
    }
    if (arg.startsWith("--host=")) {
      options.host = arg.slice("--host=".length) || "0.0.0.0";
      continue;
    }
    if (arg === "--port") {
      const next = argv[index + 1];
      if (next && !next.startsWith("-")) {
        options.port = next;
        index += 1;
      }
      continue;
    }
    if (arg.startsWith("--port=")) {
      options.port = arg.slice("--port=".length);
    }
  }
  return options;
}

const cliOptions = readCliOptions();
const configuredDenom = process.env.CLAIRVEIL_DENOM ?? "uclair";
const healthUpstreamRequestTimeoutMs = 30_000;
const healthUpstreamResponseMaxBytes = 1 << 20;
const proverProxyResponseMaxBytes = 1 << 20;
const depositProofOutputMaxBytes = 1 << 20;
const inboundRequestTimeoutMs = 30_000;
const apiRequestBodyMaxBytes = 64 * 1024;
const proverProxyRequestMaxBytes = 4 * 1024 * 1024;
const auditorBatchScanEventLimit = 64;
const auditorBatchScanOutputLimit = 128;
const auditorBatchScanMaxEncodedBytes = 1 << 20;

function normalizeEvmChainId(value) {
  const text = String(value ?? "").trim();
  if (/^0x[0-9a-fA-F]+$/.test(text)) {
    return `0x${BigInt(text).toString(16)}`;
  }
  if (/^[0-9]+$/.test(text)) {
    return `0x${BigInt(text).toString(16)}`;
  }
  throw new Error("EVM chain id must be a decimal or hex string");
}

function envFlag(name, defaultValue = false) {
  const raw = process.env[name];
  if (raw === undefined || raw === "") return defaultValue;
  const value = String(raw).trim().toLowerCase();
  if (["1", "true", "yes", "local"].includes(value)) return true;
  if (["0", "false", "no", "public"].includes(value)) return false;
  throw new Error(`${name} must be one of 1/0, true/false, yes/no, or local/public`);
}

function resolveLocalTestMode() {
  return envFlag("CLAIRVEIL_DAPP_LOCAL_TEST_MODE", true);
}

const config = {
  host: cliOptions.host ?? process.env.CLAIRVEIL_DAPP_HOST ?? "127.0.0.1",
  port: Number(cliOptions.port ?? process.env.PORT ?? process.env.CLAIRVEIL_DAPP_PORT ?? 5173),
  home: process.env.CLAIRVEIL_HOME ?? process.env.CLAIRVEIL_DAPP_HOME ?? defaultHome,
  chainId: process.env.CHAIN_ID ?? "clairveil-local-2",
  bin: process.env.CLAIRVEILD_BIN ?? "clairveild",
  rpc: process.env.CLAIRVEIL_RPC ?? "tcp://127.0.0.1:26657",
  rest: process.env.CLAIRVEIL_REST ?? "http://127.0.0.1:1317",
  publicRpc: process.env.CLAIRVEIL_PUBLIC_RPC ?? "",
  publicRest: process.env.CLAIRVEIL_PUBLIC_REST ?? "",
  publicRestEndpoints: process.env.CLAIRVEIL_PUBLIC_REST_ENDPOINTS ?? "",
  cosmosRestEndpoints: process.env.CLAIRVEIL_COSMOS_REST_ENDPOINTS ?? "",
  evmHostRestEndpoints: process.env.CLAIRVEIL_EVM_HOST_REST_ENDPOINTS ?? "",
  proverUrl: process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  publicProverUrl: process.env.CLAIRVEIL_PUBLIC_PROVER_URL ?? process.env.CLAIRVEIL_PROVER_PUBLIC_URL ?? process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  publicDepositProofUrl: process.env.CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL ?? process.env.CLAIRVEIL_DEPOSIT_PROOF_URL ?? "",
  cosmosDepositProofUrl: process.env.CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL ?? "",
  evmDepositProofUrl: process.env.CLAIRVEIL_EVM_DEPOSIT_PROOF_URL ?? "",
  referenceProverAccountPrefix: process.env.CLAIRVEIL_REFERENCE_PROVER_ACCOUNT_PREFIX ?? process.env.CLAIRVEIL_PROVER_ACCOUNT_PREFIX ?? "clair",
  proverBearerToken: process.env.CLAIRVEIL_PROVER_BEARER_TOKEN ?? process.env.CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN ?? "",
  proverTimeoutMs: Number(process.env.CLAIRVEIL_PROVER_TIMEOUT_MS ?? 120000),
  enableProverProxy: envFlag("CLAIRVEIL_DAPP_ENABLE_PROVER_PROXY", false),
  enableBatchTransfer: envFlag("CLAIRVEIL_DAPP_ENABLE_BATCH_TRANSFER", false),
  publicOrigin: process.env.CLAIRVEIL_DAPP_PUBLIC_ORIGIN ?? "",
  transport: process.env.CLAIRVEIL_TRANSPORT ?? "cosmos",
  denom: configuredDenom,
  displayDenom: process.env.CLAIRVEIL_DISPLAY_DENOM ?? "CLAIR",
  coinDecimals: Number(process.env.CLAIRVEIL_COIN_DECIMALS ?? 18),
  keplrCoinType: Number(process.env.CLAIRVEIL_KEPLR_COIN_TYPE ?? 118),
  accountPrefix: process.env.CLAIRVEIL_ACCOUNT_PREFIX ?? "clair",
  shieldedPrefix: process.env.CLAIRVEIL_SHIELDED_PREFIX ?? "clairs",
  gasPrices: process.env.CLAIRVEIL_GAS_PRICES ?? `1${configuredDenom}`,
  evmRpc: process.env.CLAIRVEIL_EVM_RPC ?? "http://127.0.0.1:8545",
  evmChainId: normalizeEvmChainId(process.env.CLAIRVEIL_EVM_CHAIN_ID ?? "815"),
  evmChainName: process.env.CLAIRVEIL_EVM_CHAIN_NAME ?? "EVM Localnet",
  evmPrivacyPrecompileAddress: process.env.CLAIRVEIL_EVM_PRIVACY_PRECOMPILE ?? evmPrivacyPrecompileAddress,
  evmGasLimit: process.env.CLAIRVEIL_EVM_GAS_LIMIT ?? "0x989680",
  evmSendGasLimit: process.env.CLAIRVEIL_EVM_SEND_GAS_LIMIT ?? "0x5208",
  localTestMode: resolveLocalTestMode(),
  allowLanSigning: process.env.CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING === "1",
  allowLanAdmin: process.env.CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN === "1",
  keplrGasPriceStep: {
    low: Number(process.env.CLAIRVEIL_KEPLR_GAS_LOW ?? 1),
    average: Number(process.env.CLAIRVEIL_KEPLR_GAS_AVERAGE ?? 1),
    high: Number(process.env.CLAIRVEIL_KEPLR_GAS_HIGH ?? 1)
  }
};

const clairveil = createClairveilClient({
  rpc: config.rpc,
  rest: config.rest,
  chainId: config.chainId,
  accountPrefix: config.accountPrefix,
  shieldedPrefix: config.shieldedPrefix,
  defaultDenom: config.denom
});

const cosmosAccountNames = new Set(["alice", "bob", "relayer", "auditor"]);
const evmDefaultSignerAccounts = [
  {
    name: "dev0",
    mnemonic: "copper push brief egg scan entry inform record adjust fossil boss egg comic alien upon aspect dry avoid interest fury window hint race symptom"
  },
  {
    name: "dev1",
    mnemonic: "maximum display century economy unlock van census kite error heart snow filter midnight usage egg venture cash kick motor survey drastic edge muffin visual"
  },
  {
    name: "dev2",
    mnemonic: "will wear settle write dance topic tape sea glory hotel oppose rebel client problem era video gossip glide during yard balance cancel file rose"
  },
  {
    name: "dev3",
    mnemonic: "doll midnight silk carpet brush boring pluck office gown inquiry duck chief aim exit gain never tennis crime fragile ship cloud surface exotic patch"
  }
];
const localTestAuditMaterial = {
  address: "clair1z8v9c0x2l4m6n8p0q2r4s6t8u0w2y4z6a8s0d2",
  pubKeyHex: "deadbeef10203040",
  signatureBase64: Buffer.from("recipient-root-signature-v1").toString("base64"),
  auditMasterPubKeyHex: "8cb0ef883bce364e0d946867ebd7a7f84ec153eeb28e5973ffe9381ec8d7940a"
};
const contentTypes = new Map([
  [".html", "text/html; charset=utf-8"],
  [".css", "text/css; charset=utf-8"],
  [".js", "text/javascript; charset=utf-8"],
  [".json", "application/json; charset=utf-8"],
  [".svg", "image/svg+xml; charset=utf-8"]
]);

function rpcHttpUrl(path) {
  return config.rpc.replace(/^tcp:\/\//, "http://").replace(/\/$/, "") + path;
}

function restUrl(path) {
  return config.rest.replace(/\/$/, "") + path;
}

function httpRpcEndpoint(value = config.rpc) {
  return value.replace(/^tcp:\/\//, "http://").replace(/\/$/, "");
}

function publicRpcEndpoint() {
  return httpRpcEndpoint(config.publicRpc || config.rpc);
}

function publicRestEndpoint() {
  return (config.publicRest || config.rest).replace(/\/$/, "");
}

function configuredRestEndpoints(primary, configured = "") {
  const endpoints = [primary, ...String(configured).split(",")]
    .map(value => value.trim().replace(/\/$/, ""))
    .filter(Boolean);
  return [...new Set(endpoints)];
}

function isEvmTransport() {
  return config.transport === "evm";
}

function localSignerBin() {
  return process.env.CLAIRVEIL_LOCAL_SIGNER_BIN ?? process.env.CLAIRVEIL_EVM_LOCAL_SIGNER_BIN ?? config.bin;
}

function localSignerHome() {
  return process.env.CLAIRVEIL_LOCAL_SIGNER_HOME ?? process.env.CLAIRVEIL_EVM_LOCAL_SIGNER_HOME ?? config.home;
}

function localSignerKeyring() {
  return process.env.CLAIRVEIL_LOCAL_SIGNER_KEYRING ?? "test";
}

function localSignerNames() {
  return isEvmTransport()
    ? new Set(evmDefaultSignerAccounts.map(account => account.name))
    : cosmosAccountNames;
}

function localRelayerName() {
  const fallback = isEvmTransport() ? "dev0" : "relayer";
  const configured = process.env.CLAIRVEIL_LOCAL_RELAYER
    ?? process.env.CLAIRVEIL_RELAYER_ACCOUNT
    ?? process.env.CLAIRVEIL_EVM_RELAYER_ACCOUNT
    ?? fallback;
  return localSignerNames().has(configured) ? configured : fallback;
}

function evmPrivacyAccountPrefix() {
  return process.env.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX
    ?? config.referenceProverAccountPrefix
    ?? "clair";
}

function buildKeplrChainInfo({
  chainId,
  chainName,
  rpc,
  rest,
  coinType,
  accountPrefix,
  displayDenom,
  denom,
  coinDecimals,
  gasPriceStep
}) {
  return {
    chainId,
    chainName,
    rpc,
    rest,
    bip44: {
      coinType
    },
    bech32Config: {
      bech32PrefixAccAddr: accountPrefix,
      bech32PrefixAccPub: `${accountPrefix}pub`,
      bech32PrefixValAddr: `${accountPrefix}valoper`,
      bech32PrefixValPub: `${accountPrefix}valoperpub`,
      bech32PrefixConsAddr: `${accountPrefix}valcons`,
      bech32PrefixConsPub: `${accountPrefix}valconspub`
    },
    currencies: [
      {
        coinDenom: displayDenom,
        coinMinimalDenom: denom,
        coinDecimals
      }
    ],
    feeCurrencies: [
      {
        coinDenom: displayDenom,
        coinMinimalDenom: denom,
        coinDecimals,
        gasPriceStep
      }
    ],
    stakeCurrency: {
      coinDenom: displayDenom,
      coinMinimalDenom: denom,
      coinDecimals
    },
    features: []
  };
}

function dappChainProfiles(proverUrl = config.publicProverUrl) {
  const cosmosRpc = httpRpcEndpoint(
    process.env.CLAIRVEIL_COSMOS_RPC ??
      (isEvmTransport() ? "tcp://127.0.0.1:26657" : config.publicRpc || config.rpc),
  );
  const cosmosRest = (
    process.env.CLAIRVEIL_COSMOS_REST ??
    (isEvmTransport() ? "http://127.0.0.1:1317" : config.publicRest || config.rest)
  ).replace(/\/$/, "");
  const clairveilProfile = {
    id: "clairveil-local",
    label: "Clairveil Localnet",
    chainName: "Clairveil Localnet",
    transport: "cosmos",
    wallet: "keplr",
    chainId: process.env.CLAIRVEIL_COSMOS_CHAIN_ID ?? (isEvmTransport() ? "clairveil-local-2" : config.chainId),
    rpc: cosmosRpc,
    rest: cosmosRest,
    restEndpoints: configuredRestEndpoints(
      cosmosRest,
      config.cosmosRestEndpoints || config.publicRestEndpoints,
    ),
    proverUrl: process.env.CLAIRVEIL_COSMOS_PROVER_URL ?? proverUrl,
    ...((config.cosmosDepositProofUrl || config.publicDepositProofUrl)
      ? { depositProofUrl: config.cosmosDepositProofUrl || config.publicDepositProofUrl }
      : {}),
    accountPrefix: process.env.CLAIRVEIL_COSMOS_ACCOUNT_PREFIX ?? "clair",
    shieldedPrefix: process.env.CLAIRVEIL_COSMOS_SHIELDED_PREFIX ?? "clairs",
    denom: process.env.CLAIRVEIL_COSMOS_DENOM ?? "uclair",
    displayDenom: process.env.CLAIRVEIL_COSMOS_DISPLAY_DENOM ?? "CLAIR",
    coinDecimals: Number(process.env.CLAIRVEIL_COSMOS_COIN_DECIMALS ?? 18),
    keplrCoinType: Number(process.env.CLAIRVEIL_COSMOS_COIN_TYPE ?? 118),
    gasPriceStep: config.keplrGasPriceStep
  };
  clairveilProfile.keplrChainInfo = buildKeplrChainInfo({
    chainId: clairveilProfile.chainId,
    chainName: clairveilProfile.chainName,
    rpc: clairveilProfile.rpc,
    rest: clairveilProfile.rest,
    coinType: clairveilProfile.keplrCoinType,
    accountPrefix: clairveilProfile.accountPrefix,
    displayDenom: clairveilProfile.displayDenom,
    denom: clairveilProfile.denom,
    coinDecimals: clairveilProfile.coinDecimals,
    gasPriceStep: clairveilProfile.gasPriceStep
  });

  const evmHostRpc = httpRpcEndpoint(
    process.env.CLAIRVEIL_EVM_HOST_RPC ??
      (isEvmTransport() ? config.publicRpc || config.rpc : "tcp://127.0.0.1:26657"),
  );
  const evmHostRest = (
    process.env.CLAIRVEIL_EVM_HOST_REST ??
    (isEvmTransport() ? config.publicRest || config.rest : "http://127.0.0.1:1317")
  ).replace(/\/$/, "");
  const evmProfile = {
    id: "evm-local",
    label: config.evmChainName,
    chainName: config.evmChainName,
    transport: "evm",
    wallet: "metamask",
    chainId: process.env.CLAIRVEIL_EVM_HOST_CHAIN_ID ?? (isEvmTransport() ? config.chainId : "evm-local-1"),
    rpc: evmHostRpc,
    rest: evmHostRest,
    restEndpoints: configuredRestEndpoints(
      evmHostRest,
      config.evmHostRestEndpoints || config.publicRestEndpoints,
    ),
    proverUrl: process.env.CLAIRVEIL_EVM_PROVER_URL ?? proverUrl,
    ...((config.evmDepositProofUrl || config.publicDepositProofUrl)
      ? { depositProofUrl: config.evmDepositProofUrl || config.publicDepositProofUrl }
      : {}),
    accountPrefix: process.env.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX ?? "clair",
    shieldedPrefix: process.env.CLAIRVEIL_EVM_SHIELDED_PREFIX ?? (isEvmTransport() ? config.shieldedPrefix : "clairs"),
    denom: process.env.CLAIRVEIL_EVM_DENOM ?? (isEvmTransport() ? config.denom : "utoken"),
    displayDenom: process.env.CLAIRVEIL_EVM_DISPLAY_DENOM ?? (isEvmTransport() ? config.displayDenom : "TOKEN"),
    coinDecimals: Number(process.env.CLAIRVEIL_EVM_COIN_DECIMALS ?? (isEvmTransport() ? config.coinDecimals : 18)),
    evmRpc: config.evmRpc,
    evmChainId: config.evmChainId,
    evmChainName: config.evmChainName,
    evmPrivacyPrecompileAddress: config.evmPrivacyPrecompileAddress,
    evmGasLimit: config.evmGasLimit,
    evmSendGasLimit: config.evmSendGasLimit
  };

  return [isEvmTransport() ? evmProfile : clairveilProfile];
}

function activeChainProfileId() {
  return dappChainProfiles().find(profile =>
    profile.transport === config.transport && profile.chainId === config.chainId
  )?.id || (isEvmTransport() ? "evm-local" : "clairveil-local");
}

function assertProductionHttpsUrl(value, label) {
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`${label} must be a valid HTTPS URL in public mode`);
  }
  if (
    url.protocol !== "https:" ||
    !url.hostname ||
    url.username ||
    url.password ||
    url.search ||
    url.hash
  ) {
    throw new Error(`${label} must be a valid HTTPS URL in public mode`);
  }
}

function assertProductionDeploymentConfig() {
  if (config.localTestMode) return;
  if (config.enableProverProxy) {
    throw new Error("CLAIRVEIL_DAPP_ENABLE_PROVER_PROXY is local-test-only; deploy a separately hardened production gateway instead");
  }
  if (config.proverBearerToken) {
    throw new Error("A public DApp server must not hold CLAIRVEIL_PROVER_BEARER_TOKEN; use a reviewed session-token service or an unauthenticated prover");
  }
  assertProductionHttpsUrl(config.publicOrigin, "CLAIRVEIL_DAPP_PUBLIC_ORIGIN");
  for (const profile of dappChainProfiles(config.publicProverUrl)) {
    assertProductionHttpsUrl(profile.rpc, `${profile.id}.rpc`);
    assertProductionHttpsUrl(profile.rest, `${profile.id}.rest`);
    for (const [index, endpoint] of (profile.restEndpoints || []).entries()) {
      assertProductionHttpsUrl(endpoint, `${profile.id}.restEndpoints[${index}]`);
    }
    assertProductionHttpsUrl(profile.proverUrl, `${profile.id}.proverUrl`);
    if (profile.depositProofUrl) {
      assertProductionHttpsUrl(profile.depositProofUrl, `${profile.id}.depositProofUrl`);
    }
    if (profile.transport === "evm") {
      assertProductionHttpsUrl(profile.evmRpc, `${profile.id}.evmRpc`);
    }
  }
}

assertProductionDeploymentConfig();

function localNetworkAddresses() {
  return Object.values(networkInterfaces())
    .flat()
    .filter(entry => entry && !entry.internal && (entry.family === "IPv4" || entry.family === 4))
    .map(entry => entry.address);
}

function isWildcardHost(host) {
  return host === "0.0.0.0" || host === "::" || host === "";
}

function isLoopbackRemoteAddress(address) {
  const value = String(address || "").trim().toLowerCase();
  const normalized = value.startsWith("::ffff:") ? value.slice("::ffff:".length) : value;
  return normalized === "::1"
    || normalized === "0:0:0:0:0:0:0:1"
    || normalized === "localhost"
    || normalized.startsWith("127.");
}

function hasForwardedClientHeaders(req) {
  return [
    "forwarded",
    "x-forwarded-for",
    "x-forwarded-host",
    "x-forwarded-proto",
    "x-real-ip",
  ].some((name) => String(req.headers[name] || "").trim() !== "");
}

function requestHostIsLoopback(req) {
  const host = String(req.headers.host || "").trim();
  if (!host) return false;
  try {
    const hostname = new URL(`http://${host}`).hostname.replace(/^\[|\]$/g, "");
    return isLoopbackRemoteAddress(hostname);
  } catch {
    return false;
  }
}

function browserRequestIsSameOrigin(req) {
  const fetchSite = String(req.headers["sec-fetch-site"] || "").trim().toLowerCase();
  if (fetchSite && fetchSite !== "same-origin" && fetchSite !== "none") {
    return false;
  }
  const originValue = String(req.headers.origin || "").trim();
  if (!originValue) return true;
  if (originValue.toLowerCase() === "null") return false;
  try {
    const origin = new URL(originValue);
    const host = String(req.headers.host || "").trim().toLowerCase();
    return (origin.protocol === "http:" || origin.protocol === "https:")
      && origin.host.toLowerCase() === host;
  } catch {
    return false;
  }
}

function isDirectLoopbackRequest(req) {
  return isLoopbackRemoteAddress(req.socket?.remoteAddress)
    && requestHostIsLoopback(req)
    && !hasForwardedClientHeaders(req)
    && browserRequestIsSameOrigin(req);
}

function signerMutationAllowed(req) {
  return browserRequestIsSameOrigin(req)
    && (config.allowLanSigning || isDirectLoopbackRequest(req));
}

function localAdminAccessAllowed(req) {
  return browserRequestIsSameOrigin(req)
    && (config.allowLanAdmin || isDirectLoopbackRequest(req));
}

function assertSignerMutationAllowed(req) {
  if (signerMutationAllowed(req)) return;
  throw httpError(
    403,
    "LAN access to signer-mutating APIs is disabled. Set CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1 to allow LAN signing."
  );
}

function assertLocalAdminAccessAllowed(req) {
  if (localAdminAccessAllowed(req)) return;
  throw httpError(
    403,
    "LAN access to local admin/private-read APIs is disabled. Set CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN=1 to allow LAN admin helpers."
  );
}

function assertLocalTestBackendAllowed(feature = "local test backend") {
  if (config.localTestMode) return;
  throw httpError(
    403,
    `${feature} is disabled because CLAIRVEIL_DAPP_LOCAL_TEST_MODE is off. Public-node DApps must not use local signer, faucet, or auditor test-secret routes.`
  );
}

function dappUrls() {
  if (!isWildcardHost(config.host)) {
    return [`http://${config.host}:${config.port}`];
  }
  return [
    `http://127.0.0.1:${config.port}`,
    ...localNetworkAddresses().map(address => `http://${address}:${config.port}`)
  ];
}

function jsonReplacer(_key, value) {
  if (typeof value === "bigint") return value.toString();
  return value;
}

function isJsonContentType(value) {
  const mediaType = String(value || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
  return mediaType === "application/json" || mediaType.endsWith("+json");
}

function sendJson(res, status, data) {
  const body = JSON.stringify(data, jsonReplacer, 2);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store"
  });
  res.end(body);
}

function requestOrigin(req) {
  const forwardedProto = String(req.headers["x-forwarded-proto"] || "").split(",")[0].trim();
  const proto = forwardedProto || "http";
  const host = req.headers.host || `${isWildcardHost(config.host) ? "127.0.0.1" : config.host}:${config.port}`;
  return `${proto}://${host}`;
}

function browserProverUrl(req) {
  // The reference prover is loopback-only and intentionally has no browser
  // CORS policy. Every local profile must therefore use the same-origin proxy,
  // not just the EVM profile that may need its request-compatibility rewrite.
  if (config.localTestMode && proverProxyEnabled(req)) {
    return requestOrigin(req);
  }
  return config.publicProverUrl;
}

function sendPlannerResult(res, result) {
  const code = plannerStatusToErrorCode(result?.status);
  sendJson(res, 409, {
    error: result?.plan?.message || `privacy transaction is not ready: ${result?.status || "unknown"}`,
    code,
    version: "v1",
    status: result?.status || "",
    plan: result?.plan || null,
    prepared: result?.prepared || null
  });
}

function httpError(statusCode, message, code = ClairveilErrorCode.INVALID_ARGUMENT) {
  const error = new Error(message);
  error.statusCode = statusCode;
  error.clairveilCode = code;
  error.responseVersion = "v1";
  return error;
}

function depositProofFailure() {
  const error = httpError(502, "deposit proof generation failed", "proof_failed");
  error.responseVersion = "v1";
  return error;
}

function safeClairveilErrorMessage(code) {
  switch (code) {
    case "PROVER_TIMEOUT":
      return "privacy proof service timed out";
    case "PROVER_UNAVAILABLE":
      return "privacy proof service is unavailable";
    case "PROVER_REJECTED":
      return "privacy proof service rejected the request";
    default:
      return "privacy operation failed";
  }
}

function privacyOperationErrorPayload(error) {
  const code = error?.clairveilCode || ClairveilErrorCode.INVALID_ARGUMENT;
  return {
    error: code === "proof_failed"
      ? "deposit proof generation failed"
      : "privacy operation failed",
    code,
    version: "v1",
    ...(error?.txHash ? { txHash: error.txHash } : {}),
    ...(error?.tx_hash ? { tx_hash: error.tx_hash } : {}),
    ...(typeof error?.receiptStatus === "string" ? { receiptStatus: error.receiptStatus } : {}),
    ...(error?.executionFailed === true ? { executionFailed: true } : {}),
    ...(typeof error?.txCode === "number" && Number.isSafeInteger(error.txCode) && error.txCode >= 0
      ? { txCode: error.txCode }
      : {})
  };
}

// Relay payloads and their proofs must never be returned to the browser in an
// error response. The browser can still take the correct recovery action when
// it receives a small, non-sensitive failure category instead of one generic
// INVALID_ARGUMENT code.
function relayWithdrawSafeFailureCode(error) {
  const message = String(error?.message || "");
  if (/relay withdraw payload.*(?:expired|expires_at_unix)|payload is expired/i.test(message)) {
    return "RELAY_PAYLOAD_EXPIRED";
  }
  if (/nullifier.*(?:already used|spent|explicitly unspent)|requires explicitly unspent input nullifiers/i.test(message)) {
    return "RELAY_INPUT_UNAVAILABLE";
  }
  if (
    error?.txHash ||
    error?.tx_hash ||
    error?.executionFailed === true ||
    (typeof error?.txCode === "number" && Number.isSafeInteger(error.txCode))
  ) {
    return "TX_BROADCAST_FAILED";
  }
  return "RELAY_SUBMISSION_FAILED";
}

function privacySensitiveOperationError(error) {
  if (error && typeof error === "object") {
    error.privacySensitive = true;
    return error;
  }
  const wrapped = new Error(String(error || "privacy operation failed"));
  wrapped.privacySensitive = true;
  return wrapped;
}

function errorPayload(error) {
  if (error?.privacySensitive) {
    // Relay and deposit-proof boundaries may wrap an SDK ClairveilError. The
    // route has already converted a relay failure to a small, safe recovery
    // code in `clairveilCode`; preserve that category instead of leaking the
    // original INVALID_ARGUMENT/SDK code back to the browser.
    return privacyOperationErrorPayload(error);
  }
  if (error instanceof ClairveilError) {
    // SDK details can include planner/prepared operation context or a nested
    // prover cause. Its message is not a safe display string: it can include
    // nested endpoint text. Only the stable code and a local generic message
    // may cross this HTTP boundary.
    return {
      error: safeClairveilErrorMessage(error.code),
      code: error.code,
      version: "v1",
    };
  }
  return {
    error: error?.message || String(error),
    code: error?.clairveilCode || ClairveilErrorCode.INVALID_ARGUMENT,
    version: typeof error?.responseVersion === "string" && error.responseVersion
      ? error.responseVersion
      : "v1",
    ...(error?.txHash ? { txHash: error.txHash } : {}),
    ...(error?.tx_hash ? { tx_hash: error.tx_hash } : {}),
    ...(typeof error?.receiptStatus === "string" ? { receiptStatus: error.receiptStatus } : {}),
    ...(error?.executionFailed === true ? { executionFailed: true } : {}),
    ...(typeof error?.txCode === "number" && Number.isSafeInteger(error.txCode) && error.txCode >= 0
      ? { txCode: error.txCode }
      : {})
  };
}

async function readBody(req) {
  if (!isJsonContentType(req.headers["content-type"])) {
    throw httpError(415, "request content-type must be application/json", "invalid_request");
  }
  const raw = await readRawBody(req, { maxBytes: apiRequestBodyMaxBytes });
  if (!raw.length) return {};
  try {
    return JSON.parse(raw.toString("utf8"));
  } catch {
    throw httpError(400, "invalid JSON body", "invalid_request");
  }
}

function readRawBody(req, { maxBytes = proverProxyRequestMaxBytes } = {}) {
  return new Promise((resolveBody, reject) => {
    const chunks = [];
    let size = 0;
    let settled = false;
    const fail = (error) => {
      if (settled) return;
      settled = true;
      // Keep draining the stream so a bounded rejection can return a JSON
      // response without retaining any more request bytes in memory.
      req.resume();
      reject(error);
    };
    req.on("data", chunk => {
      if (settled) return;
      size += chunk.length;
      if (size > maxBytes) {
        fail(httpError(413, "request body too large", "invalid_request"));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => {
      if (settled) return;
      settled = true;
      resolveBody(Buffer.concat(chunks));
    });
    req.on("error", () => fail(httpError(400, "request body read failed", "invalid_request")));
  });
}

function proverProxyPath(pathname) {
  if (
    pathname === "/v1/prover/transfer" ||
    pathname === "/v1/prover/withdraw" ||
    pathname === "/v1/proofs/batch-transfer"
  ) {
    return pathname;
  }
  return "";
}

function proverProxyEnabled(req) {
  return config.localTestMode
    && config.enableProverProxy
    && isDirectLoopbackRequest(req);
}

function shouldRewriteTransferProofForReferenceProver(path) {
  return path === "/v1/prover/transfer"
    && config.localTestMode
    && isEvmTransport()
    && config.accountPrefix !== config.referenceProverAccountPrefix;
}

function shouldRewriteWithdrawProofForReferenceProver(path) {
  return path === "/v1/prover/withdraw"
    && config.localTestMode
    && isEvmTransport()
    && config.accountPrefix !== config.referenceProverAccountPrefix;
}

function bech32PrefixOf(value) {
  const text = String(value || "").trim();
  const separator = text.indexOf("1");
  return separator > 0 ? text.slice(0, separator) : "";
}

function maybeRewriteBech32ForReferenceProver(value, label) {
  const prefix = bech32PrefixOf(value);
  if (!prefix || prefix === config.referenceProverAccountPrefix) {
    return { value, rewritten: false, evmAddress: "" };
  }
  if (prefix !== config.accountPrefix) {
    throw new Error(`${label} prefix mismatch: expected ${config.accountPrefix} or ${config.referenceProverAccountPrefix}, got ${prefix}`);
  }
  const evmAddress = bech32AddressToEvm(value, config.accountPrefix);
  return {
    value: evmAddressToBech32(evmAddress, config.referenceProverAccountPrefix),
    rewritten: true,
    evmAddress
  };
}

function rewriteProofBodyForReferenceProver(path, body) {
  if (!shouldRewriteTransferProofForReferenceProver(path) && !shouldRewriteWithdrawProofForReferenceProver(path)) {
    return { body };
  }

  // Local EVM tests use the EVM chain account prefix, while the reference
  // prover still validates payload metadata with the Clairveil account prefix.
  // The EVM precompile works with EVM addresses, so this rewrite is only a
  // proof-service compatibility shim for the example server.
  const request = JSON.parse(body.toString("utf8"));
  const payload = request?.payload;
  if (!payload || typeof payload !== "object") {
    return { body };
  }

  let originalPayloadHash = "";
  let rewrittenPayload = payload;
  if (shouldRewriteTransferProofForReferenceProver(path)) {
    const rewrite = maybeRewriteBech32ForReferenceProver(payload.creator, "transfer creator");
    if (rewrite.rewritten) {
      originalPayloadHash = payload.payload_hash || computePreparedTransferPayloadHash(payload);
      rewrittenPayload = {
        ...payload,
        creator: rewrite.value
      };
      rewrittenPayload.payload_hash = computePreparedTransferPayloadHash(rewrittenPayload);
    }
  }
  if (shouldRewriteWithdrawProofForReferenceProver(path)) {
    const rewrite = maybeRewriteBech32ForReferenceProver(payload.recipient, "withdraw recipient");
    if (rewrite.rewritten) {
      originalPayloadHash = payload.payload_hash || computePreparedWithdrawProverPayloadHash(payload);
      rewrittenPayload = {
        ...payload,
        recipient: rewrite.value,
        recipient_bytes_hex: rewrite.evmAddress.replace(/^0x/i, "").toLowerCase()
      };
      rewrittenPayload.payload_hash = computePreparedWithdrawProverPayloadHash(rewrittenPayload);
    }
  }

  if (!originalPayloadHash) {
    return { body };
  }

  return {
    body: Buffer.from(JSON.stringify({ ...request, payload: rewrittenPayload }, jsonReplacer), "utf8"),
    originalPayloadHash
  };
}

function restoreProofHashForBrowser(path, text, originalPayloadHash) {
  if (!originalPayloadHash || (path !== "/v1/prover/transfer" && path !== "/v1/prover/withdraw")) {
    return text;
  }

  const response = JSON.parse(text);
  if (response?.proof?.payload_hash) {
    response.proof.payload_hash = originalPayloadHash;
  }
  return JSON.stringify(response, jsonReplacer, 2);
}

function proverProxyErrorResponse(statusCode, { timedOut = false } = {}) {
  if (timedOut) {
    return {
      version: "v1",
      code: "unavailable",
      message: "proof service request timed out",
    };
  }
  if (statusCode === 400 || statusCode === 413 || statusCode === 422) {
    return {
      version: "v1",
      code: "invalid_request",
      message: "proof service rejected the request",
    };
  }
  if (statusCode === 401 || statusCode === 403) {
    return {
      version: "v1",
      code: "unauthorized",
      message: "proof service authorization failed",
    };
  }
  if (statusCode === 404) {
    return {
      version: "v1",
      code: "not_found",
      message: "proof service route was not found",
    };
  }
  if (statusCode === 405) {
    return {
      version: "v1",
      code: "method_not_allowed",
      message: "proof service method is not allowed",
    };
  }
  if (statusCode >= 500) {
    return {
      version: "v1",
      code: "unavailable",
      message: "proof service is unavailable",
    };
  }
  return {
    version: "v1",
    code: "proof_failed",
    message: "proof service failed to generate a proof",
  };
}

async function handleProverProxy(req, res, url) {
  const path = proverProxyPath(url.pathname);
  if (!path) {
    sendJson(res, 404, { error: "not found", code: "not_found", version: "v1" });
    return;
  }
  if (!proverProxyEnabled(req)) {
    sendJson(res, 404, { error: "not found", code: "not_found", version: "v1" });
    return;
  }
  if (req.method === "OPTIONS") {
    res.writeHead(204, { allow: "POST, OPTIONS" });
    res.end();
    return;
  }
  if (req.method !== "POST") {
    sendJson(res, 405, {
      version: "v1",
      code: "method_not_allowed",
      message: "prover proxy requires POST"
    });
    return;
  }
  if (!isJsonContentType(req.headers["content-type"])) {
    sendJson(res, 415, {
      version: "v1",
      code: "invalid_request",
      message: "prover proxy content-type must be application/json"
    });
    return;
  }

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), config.proverTimeoutMs);
  try {
    const rawBody = await readRawBody(req, {
      maxBytes: proverProxyRequestMaxBytes,
    });
    const rewritten = rewriteProofBodyForReferenceProver(path, rawBody);
    const target = new URL(path, config.proverUrl.replace(/\/$/, ""));
    const headers = {
      "content-type": req.headers["content-type"] || "application/json",
      accept: "application/json"
    };
    if (config.proverBearerToken) {
      headers.authorization = `Bearer ${config.proverBearerToken}`;
    }
    const response = await fetch(target, {
      method: "POST",
      headers,
      body: rewritten.body,
      signal: controller.signal
    });
    const responseText = await readBoundedResponseText(
      response,
      proverProxyResponseMaxBytes,
    );
    if (!response.ok) {
      // An upstream prover can put witness-derived data or credentials in its
      // error text. Keep that body inside the proxy and retain only the
      // protocol status category for the loopback browser client.
      sendJson(res, response.status, proverProxyErrorResponse(response.status));
      return;
    }
    if (!isJsonContentType(response.headers.get("content-type"))) {
      // A successful proof still has a versioned JSON envelope. Do not pass a
      // MIME-mismatched upstream body through the browser's proof boundary.
      sendJson(res, 502, proverProxyErrorResponse(502));
      return;
    }
    const text = restoreProofHashForBrowser(
      path,
      responseText,
      rewritten.originalPayloadHash,
    );
    res.writeHead(response.status, {
      "content-type": response.headers.get("content-type") || "application/json; charset=utf-8",
      "cache-control": "no-store"
    });
    res.end(text);
  } catch (error) {
    const timedOut = error?.name === "AbortError";
    const invalidRequest = error?.statusCode === 413 || error?.statusCode === 400;
    const statusCode = invalidRequest ? error.statusCode : timedOut ? 504 : 502;
    sendJson(
      res,
      statusCode,
      proverProxyErrorResponse(statusCode, { timedOut }),
    );
  } finally {
    clearTimeout(timeout);
  }
}

async function readTextIfExists(path) {
  try {
    return (await readFile(path, "utf8")).trim();
  } catch {
    return "";
  }
}

async function readEnvFile() {
  const path = join(config.home, "clairveil.env");
  const env = {};
  const text = await readTextIfExists(path);
  for (const line of text.split("\n")) {
    const match = line.match(/^export\s+([A-Z0-9_]+)=(.*)$/);
    if (!match) continue;
    env[match[1]] = match[2].replace(/^"|"$/g, "");
  }
  return env;
}

async function localAccounts() {
  if (!config.localTestMode) {
    return [];
  }

  if (isEvmTransport()) {
    try {
      const result = await runLocalSigner([
        "keys", "list",
        "--keyring-backend", localSignerKeyring(),
        "--home", localSignerHome(),
        "--output", "json"
      ]);
      const allowed = localSignerNames();
      return (Array.isArray(result.json) ? result.json : [])
        .filter(account => allowed.has(account.name) && account.address)
        .map(account => ({
          name: account.name,
          transparentAddress: account.address
        }));
    } catch {
      return [];
    }
  }

  const out = join(config.home, "init-out");
  const entries = await Promise.all([...cosmosAccountNames].map(async name => ({
    name,
    transparentAddress: await readTextIfExists(join(out, `${name}-address.txt`))
  })));
  return entries.filter(entry => entry.transparentAddress);
}

function validateAccount(value) {
  if (!localSignerNames().has(value)) {
    throw new Error("unsupported local signer");
  }
  return value;
}

async function ensureLocalSigners() {
  if (!isEvmTransport()) {
    return localAccounts();
  }

  const existing = await localAccounts();
  const existingNames = new Set(existing.map(account => account.name));
  for (const account of evmDefaultSignerAccounts) {
    if (existingNames.has(account.name)) continue;
    try {
      await runLocalSigner([
        "keys", "add", account.name,
        "--recover",
        "--keyring-backend", localSignerKeyring(),
        "--algo", "eth_secp256k1",
        "--home", localSignerHome()
      ], {
        input: `${account.mnemonic}\n`,
        json: false
      });
    } catch (error) {
      if (!String(error.message || "").includes("already exists")) {
        throw error;
      }
    }
  }
  return localAccounts();
}

function escapeRegExp(value) {
  return String(value).replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

function validateCoin(value) {
  const denom = escapeRegExp(config.denom);
  const pattern = new RegExp(`^(0|[1-9][0-9]*)${denom}$`);
  if (typeof value !== "string" || !pattern.test(value)) {
    throw new Error(`amount must look like 1${config.denom}`);
  }
  return value;
}

function coinAmount(value) {
  return BigInt(validateCoin(value).slice(0, -config.denom.length));
}

function denomCoin(amount) {
  return `${amount}${config.denom}`;
}

function normalizeFaucetAmount(value) {
  const requested = coinAmount(value);
  if (requested <= 0n) {
    throw new Error(`faucet amount must be greater than 0${config.denom}`);
  }
  return {
    requested: denomCoin(requested),
    funded: denomCoin(requested)
  };
}

function validateClairAddress(value) {
  const pattern = new RegExp(`^${config.accountPrefix}1[0-9a-z]{20,}$`);
  if (typeof value !== "string" || !pattern.test(value)) {
    throw new Error(`invalid ${config.accountPrefix} address`);
  }
  return value;
}

function validateEvmAddress(value) {
  if (!isEvmAddress(value)) {
    throw new Error("EVM address must be 20-byte hex");
  }
  return `0x${String(value).trim().replace(/^0x/i, "").toLowerCase()}`;
}

function evmDefaultSigner(name) {
  return evmDefaultSignerAccounts.find(account => account.name === name);
}

function evmWalletForLocalSigner(name) {
  const signer = evmDefaultSigner(name);
  if (!signer) {
    throw new Error("unsupported EVM faucet signer");
  }
  return Wallet.fromPhrase(signer.mnemonic);
}

function validateTxHashHex(value) {
  const txHash = typeof value === "string" ? value.trim().replace(/^0x/i, "") : "";
  if (!/^[0-9a-fA-F]{64}$/.test(txHash)) {
    throw new Error("txHash must be a 32-byte hex string");
  }
  return txHash.toUpperCase();
}

function typedScanBytes(value, label) {
  if (Buffer.isBuffer(value) || value instanceof Uint8Array) {
    return Buffer.from(value);
  }
  if (value instanceof ArrayBuffer) return Buffer.from(new Uint8Array(value));
  if (ArrayBuffer.isView(value)) {
    return Buffer.from(value.buffer, value.byteOffset, value.byteLength);
  }
  if (Array.isArray(value)) return Buffer.from(value);

  const encoded = String(value || "").trim();
  if (!encoded) throw new Error(`typed privacy scan output is missing ${label}`);
  if (/^[0-9a-fA-F]+$/.test(encoded) && encoded.length % 2 === 0) {
    return Buffer.from(encoded, "hex");
  }
  const bytes = Buffer.from(encoded, "base64");
  if (!bytes.length || bytes.toString("base64") !== encoded) {
    throw new Error(`typed privacy scan output has an invalid ${label}`);
  }
  return bytes;
}

function typedScanTxHashHex(value) {
  const bytes = typedScanBytes(value, "tx hash");
  if (bytes.length !== 32) {
    throw new Error("typed privacy scan output has an invalid tx hash");
  }
  return bytes.toString("hex").toUpperCase();
}

function typedScanCursor(page) {
  const cursor = page?.next_cursor ?? page?.nextCursor;
  if (!cursor || typeof cursor !== "object") {
    throw new Error("typed batch audit scan is missing its next cursor");
  }
  return {
    height: cursor.height ?? 0,
    globalSequence: cursor.global_sequence ?? cursor.globalSequence ?? 0,
    outputIndex: cursor.output_index ?? cursor.outputIndex ?? 0
  };
}

function sameTypedScanCursor(left, right) {
  return String(left?.height ?? 0) === String(right?.height ?? 0) &&
    String(left?.globalSequence ?? left?.global_sequence ?? 0) === String(right?.globalSequence ?? right?.global_sequence ?? 0) &&
    String(left?.outputIndex ?? left?.output_index ?? 0) === String(right?.outputIndex ?? right?.output_index ?? 0);
}

function eventAttribute(event, key) {
  const attribute = (event?.attributes || []).find(item =>
    String(item?.key ?? item?.Key ?? "") === key
  );
  return String(attribute?.value ?? attribute?.Value ?? "");
}

function isAuditablePrivacyEvent(event) {
  return event?.event_type === "batch_transfer" || (
    event?.event_type === "shielded_transfer" &&
    Boolean(eventAttribute(event, "audit_disclosure_payload"))
  );
}

function paginationInteger(value, label, { fallback, maximum = 200 } = {}) {
  const text = String(value ?? "").trim();
  if (!text) return fallback;
  if (!/^[1-9][0-9]*$/.test(text)) {
    throw new Error(`${label} must be a positive integer`);
  }
  const parsed = BigInt(text);
  if (parsed > BigInt(maximum)) {
    throw new Error(`${label} must not exceed ${maximum}`);
  }
  return Number(parsed);
}

function typedBatchScanStartCursor({ height, sequence }) {
  const identity = typedBatchEventIdentity({ height, sequence });
  return {
    height: identity.height,
    globalSequence: (BigInt(identity.globalSequence) - 1n).toString(),
    outputIndex: 0
  };
}

function auditorEventReference(body) {
  const txHash = validateTxHashHex(body.txHash ?? body.tx_hash);
  const eventType = String(body.eventType ?? body.event_type ?? "").trim();
  if (eventType !== "shielded_transfer" && eventType !== "batch_transfer") {
    throw new Error("auditor event type is invalid");
  }
  return {
    txHash,
    eventType,
    page: paginationInteger(body.eventPage ?? body.event_page, "auditor event page", {
      fallback: 1,
      maximum: 1_000_000,
    }),
    height: body.eventHeight ?? body.event_height,
    sequence: body.eventSequence ?? body.event_sequence,
  };
}

async function batchAuditOutputsForEvent({ txHash, height, sequence }) {
  const outputsByIndex = new Map();
  let expectedOutputCount = null;
  const selectedIdentity = typedBatchEventIdentity({ height, sequence });
  let after = typedBatchScanStartCursor({ height, sequence });

  while (true) {
    const page = await clairveil.fetchAuditableBatchTransfers({
      after,
      eventLimit: auditorBatchScanEventLimit,
      outputLimit: auditorBatchScanOutputLimit,
      maxEncodedBytes: auditorBatchScanMaxEncodedBytes
    });
    const summary = (page.summaries || []).find(item =>
      typedScanTxHashHex(item.tx_hash ?? item.txHash) === txHash &&
      sameTypedBatchEventIdentity(selectedIdentity, typedBatchEventIdentity(item))
    );
    if (summary) {
      const count = Number(summary.output_count ?? summary.outputCount ?? 0);
      if (!Number.isInteger(count) || count < 1 || count > 32) {
        throw new Error("typed batch audit scan has an invalid output count");
      }
      expectedOutputCount = count;
    }
    for (const output of page.outputs || []) {
      if (typedScanTxHashHex(output.tx_hash ?? output.txHash) !== txHash) continue;
      if (!sameTypedBatchEventIdentity(selectedIdentity, typedBatchEventIdentity(output))) {
        continue;
      }
      const index = Number(output.output_index ?? output.outputIndex);
      if (!Number.isInteger(index) || index < 0 || index >= 32 || outputsByIndex.has(index)) {
        throw new Error("typed batch audit scan has invalid output evidence");
      }
      outputsByIndex.set(index, output);
    }
    if (expectedOutputCount !== null && outputsByIndex.size === expectedOutputCount) {
      return [...outputsByIndex.entries()]
        .sort(([left], [right]) => left - right)
        .map(([, output]) => output);
    }
    if (expectedOutputCount === null) {
      throw new Error("typed batch audit scan no longer contains the selected event");
    }
    if (!page.has_more && !page.hasMore) {
      throw new Error("typed batch audit scan stopped before the selected event was complete");
    }
    const next = typedScanCursor(page);
    if (sameTypedScanCursor(after, next)) {
      throw new Error("typed batch audit scan cursor did not advance");
    }
    after = next;
  }
}

async function fetchAuditorTransferEvents({ page = 1, limit = 20 } = {}) {
  const result = await clairveil.fetchPrivacyEvents({
    page,
    limit,
    eventTypes: ["shielded_transfer", "batch_transfer"]
  });
  return {
    events: (result.events || []).filter(isAuditablePrivacyEvent),
    page: Number(result.page ?? page),
    limit: Number(result.limit ?? limit),
    has_more: Boolean(result.has_more ?? result.hasMore)
  };
}

function parseCoin(value) {
  const coin = validateCoin(value);
  return {
    amount: coin.slice(0, -config.denom.length),
    denom: config.denom,
    raw: coin
  };
}

function buildRootSigningMessage(address, pubKeyHex) {
  return [
    "clairveil-root-v1",
    `address:${address}`,
    `pubkey:${pubKeyHex}`
  ].join("\n");
}

function extractLastJson(stdout) {
  const text = stdout.trim();
  try {
    return JSON.parse(text);
  } catch {
    // Some tx commands print an extra diagnostic JSON before the broadcast response.
  }

  const lines = text.split("\n").reverse();
  for (const line of lines) {
    const trimmed = line.trim();
    if (!trimmed.startsWith("{") && !trimmed.startsWith("[")) continue;
    try {
      return JSON.parse(trimmed);
    } catch {
      // Keep searching because deposit also prints a note JSON before the response.
    }
  }
  throw new Error("command did not return JSON");
}

async function runClairveild(args, options = {}) {
  const env = {
    ...process.env,
    ...(await readEnvFile()),
    CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE: process.env.CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE ?? "strict"
  };
  const timeoutMs = options.timeoutMs ?? 120000;

  return new Promise((resolveRun, reject) => {
    const child = spawn(config.bin, args, {
      env,
      cwd: repoRoot,
      timeout: timeoutMs
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => {
      stdout += chunk;
    });
    child.stderr.on("data", chunk => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", code => {
      if (code !== 0) {
        const message = stderr.trim() || stdout.trim() || `clairveild exited with code ${code}`;
        reject(new Error(message));
        return;
      }
      resolveRun({ stdout, stderr, json: options.json === false ? null : extractLastJson(stdout) });
    });
  });
}

async function runLocalSigner(args, options = {}) {
  const env = {
    ...process.env,
    ...(await readEnvFile()),
    CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE: process.env.CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE ?? "strict"
  };
  const timeoutMs = options.timeoutMs ?? 120000;

  return new Promise((resolveRun, reject) => {
    const child = spawn(localSignerBin(), args, {
      env,
      cwd: repoRoot,
      timeout: timeoutMs
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => {
      stdout += chunk;
    });
    child.stderr.on("data", chunk => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", code => {
      if (code !== 0) {
        const message = stderr.trim() || stdout.trim() || `${localSignerBin()} exited with code ${code}`;
        reject(new Error(message));
        return;
      }
      resolveRun({ stdout, stderr, json: options.json === false ? null : extractLastJson(stdout) });
    });
    child.stdin.end(options.input || "");
  });
}

async function runAuditorMaterial(input) {
  const timeoutMs = 120000;
  const env = {
    ...process.env,
    ...(await readEnvFile())
  };
  return new Promise((resolveRun, reject) => {
    const child = spawn("go", ["run", "./examples/clairveil-dapp/tools/auditor-material"], {
      cwd: repoRoot,
      env,
      timeout: timeoutMs
    });
    let stdout = "";
    let stderr = "";
    child.stdout.on("data", chunk => {
      stdout += chunk;
    });
    child.stderr.on("data", chunk => {
      stderr += chunk;
    });
    child.on("error", reject);
    child.on("close", code => {
      if (code !== 0) {
        reject(new Error(stderr.trim() || stdout.trim() || `auditor-material exited with code ${code}`));
        return;
      }
      try {
        resolveRun(extractLastJson(stdout));
      } catch {
        const output = stdout.trim() || stderr.trim();
        reject(new Error(output ? `auditor-material did not return JSON: ${output.slice(0, 500)}` : "auditor-material did not return JSON"));
      }
    });
    child.stdin.end(JSON.stringify(input));
  });
}

async function runDepositProof(input) {
  const timeoutMs = 120000;
  const env = {
    ...process.env,
    ...(await readEnvFile()),
    CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE: process.env.CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE ?? "strict"
  };
  return new Promise((resolveRun, reject) => {
    const child = spawn("go", ["run", "./examples/clairveil-dapp/tools/deposit-proof"], {
      cwd: repoRoot,
      env,
      timeout: timeoutMs
    });
    let stdout = "";
    let stdoutBytes = 0;
    let outputTooLarge = false;
    child.stdout.on("data", chunk => {
      const bytes = Buffer.byteLength(chunk);
      if (stdoutBytes + bytes > depositProofOutputMaxBytes) {
        outputTooLarge = true;
        child.kill();
        return;
      }
      stdoutBytes += bytes;
      stdout += chunk;
    });
    // A helper may include its sensitive stdin in diagnostics. Drain stderr
    // without retaining it so it cannot reach an API error or remain buffered.
    child.stderr.resume();
    child.on("error", () => reject(depositProofFailure()));
    child.on("close", code => {
      if (code !== 0 || outputTooLarge) {
        reject(depositProofFailure());
        return;
      }
      try {
        resolveRun(extractLastJson(stdout));
      } catch {
        reject(depositProofFailure());
      }
    });
    child.stdin.end(JSON.stringify(input));
  });
}

function testAuditMaterialFromConfig(auditMasterPubKeyHex) {
  if (String(auditMasterPubKeyHex || "").toLowerCase() !== localTestAuditMaterial.auditMasterPubKeyHex) {
    return null;
  }
  const material = derivePrivacyMaterial({
    address: localTestAuditMaterial.address,
    pubKeyHex: localTestAuditMaterial.pubKeyHex,
    signatureBase64: localTestAuditMaterial.signatureBase64,
    shieldedPrefix: config.shieldedPrefix
  });
  return {
    key_name: "local-test-fixture-auditor",
    from_address: localTestAuditMaterial.address,
    transparent_pubkey_hex: localTestAuditMaterial.pubKeyHex,
    root_signing_message: buildRootSigningMessage(localTestAuditMaterial.address, localTestAuditMaterial.pubKeyHex),
    root_signature_base64: localTestAuditMaterial.signatureBase64,
    root_seed_hex: material.rootSeedHex,
    disclosure_private_scalar_hex: material.disclosureScalarHex,
    disclosure_pubkey_hex: material.disclosurePubKeyHex,
    derived_from: "local-test-fixture-root"
  };
}

function responseContentLength(response) {
  const raw = response?.headers?.get?.("content-length");
  if (!raw || !/^(0|[1-9][0-9]*)$/.test(raw.trim())) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) ? value : null;
}

async function readBoundedResponseText(response, maxResponseBytes) {
  const declaredLength = responseContentLength(response);
  if (declaredLength !== null && declaredLength > maxResponseBytes) {
    throw new Error(`upstream response exceeds ${maxResponseBytes} byte limit`);
  }
  if (!response?.body || typeof response.body.getReader !== "function") {
    const text = await response.text();
    if (Buffer.byteLength(text, "utf8") > maxResponseBytes) {
      throw new Error(`upstream response exceeds ${maxResponseBytes} byte limit`);
    }
    return text;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let totalBytes = 0;
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    if (!value) continue;
    totalBytes += value.byteLength;
    if (totalBytes > maxResponseBytes) {
      await reader.cancel().catch(() => {});
      throw new Error(`upstream response exceeds ${maxResponseBytes} byte limit`);
    }
    chunks.push(value);
  }
  return Buffer.concat(chunks.map(chunk => Buffer.from(chunk)), totalBytes).toString("utf8");
}

async function fetchEvmRpcResponse(url, {
  method,
  headers,
  body,
  cancelSignal = null,
} = {}) {
  const controller = new AbortController();
  let timedOut = false;
  const timeoutID = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, healthUpstreamRequestTimeoutMs);
  cancelSignal?.addListener?.(() => controller.abort());
  try {
    const response = await fetch(url, {
      method,
      headers,
      body,
      redirect: "error",
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`EVM RPC request failed with status ${response.status}`);
    }
    if (!isJsonContentType(response.headers.get("content-type"))) {
      throw new Error("EVM RPC response must be application/json");
    }
    const responseText = await readBoundedResponseText(
      response,
      healthUpstreamResponseMaxBytes,
    );
    return {
      statusCode: response.status,
      statusMessage: response.statusText,
      headers: Object.fromEntries(response.headers.entries()),
      body: Buffer.from(responseText, "utf8"),
    };
  } catch (cause) {
    if (timedOut) {
      throw new Error(
        `EVM RPC request timed out after ${healthUpstreamRequestTimeoutMs}ms`,
        { cause },
      );
    }
    throw cause;
  } finally {
    clearTimeout(timeoutID);
  }
}

function boundedEvmJsonRpcProvider() {
  const request = new FetchRequest(config.evmRpc);
  request.timeout = healthUpstreamRequestTimeoutMs;
  request.getUrlFunc = async (outboundRequest, cancelSignal) => fetchEvmRpcResponse(
    outboundRequest.url,
    {
      method: outboundRequest.method,
      headers: outboundRequest.headers,
      body: outboundRequest.body,
      cancelSignal,
    },
  );
  return new JsonRpcProvider(request, undefined, { batchMaxCount: 1 });
}

async function fetchJson(url, {
  timeoutMs = healthUpstreamRequestTimeoutMs,
  maxResponseBytes = healthUpstreamResponseMaxBytes,
} = {}) {
  const controller = new AbortController();
  let timedOut = false;
  const timeoutID = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  try {
    const response = await fetch(url, {
      headers: { accept: "application/json" },
      redirect: "error",
      signal: controller.signal,
    });
    if (!response.ok) {
      throw new Error(`${response.status} ${response.statusText}`);
    }
    if (!isJsonContentType(response.headers.get("content-type"))) {
      throw new Error("upstream JSON response must be application/json");
    }
    const body = await readBoundedResponseText(response, maxResponseBytes);
    return JSON.parse(body);
  } catch (cause) {
    if (timedOut) {
      throw new Error(`upstream JSON request timed out after ${timeoutMs}ms`, { cause });
    }
    throw cause;
  } finally {
    clearTimeout(timeoutID);
  }
}

async function waitForTx(txhash) {
  return clairveil.waitForTx(txhash);
}

async function queryBalances(address) {
  return clairveil.getBalances(address);
}

async function queryEvmNativeBalance(address) {
  const recipientEvm = validateEvmAddress(address);
  const balanceHex = await evmJsonRpc("eth_getBalance", [recipientEvm, "latest"]);
  return {
    balances: [{
      denom: config.denom,
      amount: BigInt(balanceHex || "0x0").toString()
    }],
    evmAddress: recipientEvm,
    hex: balanceHex
  };
}

function relayPayloadExpiryUnix(payload = {}) {
  const raw = payload.expires_at_unix ?? payload.expiresAtUnix;
  const expiresAtUnix = Number(raw);
  if (!Number.isSafeInteger(expiresAtUnix) || expiresAtUnix <= 0) {
    throw new Error("relay withdraw payload has an invalid expires_at_unix");
  }
  return expiresAtUnix;
}

function confirmedCosmosTxCode(tx) {
  const value = tx?.code;
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? value : null;
  }
  if (typeof value === "string" && /^(0|[1-9][0-9]*)$/.test(value)) {
    return Number(value);
  }
  return null;
}

async function relayChainNowUnix(evmProvider = null) {
  if (isEvmTransport()) {
    const provider = evmProvider || boundedEvmJsonRpcProvider();
    const block = await provider.getBlock("latest");
    const timestamp = Number(block?.timestamp);
    if (!Number.isSafeInteger(timestamp) || timestamp < 0) {
      throw new Error("latest EVM block time is unavailable");
    }
    return timestamp;
  }
  const status = await fetchJson(rpcHttpUrl("/status"));
  const chainNowMs = Date.parse(status?.result?.sync_info?.latest_block_time || "");
  if (!Number.isFinite(chainNowMs)) {
    throw new Error("latest chain block time is unavailable");
  }
  return Math.floor(chainNowMs / 1000);
}

async function assertRelayPayloadNotExpired(payload, evmProvider = null) {
  const expiresAtUnix = relayPayloadExpiryUnix(payload);
  const chainNowUnix = await relayChainNowUnix(evmProvider);
  // Match the proof/keeper expiry contract: the payload is no longer valid
  // when the authoritative chain time reaches its absolute expiry second.
  if (chainNowUnix >= expiresAtUnix) {
    throw new Error("relay withdraw payload is expired at the latest chain block time");
  }
}

function serverFeaturesForRequest(req) {
  const localTestMode = config.localTestMode;
  const localSignerAdmin = localTestMode && localAdminAccessAllowed(req);
  const localSignerMutation = localTestMode && signerMutationAllowed(req);
  return {
    localTestMode,
    localSigners: localSignerAdmin,
    localSignerAdmin,
    localSignerSetup: localSignerMutation,
    faucet: localSignerMutation,
    depositProof: localSignerMutation,
    relayer: localSignerMutation,
    auditorAdmin: localSignerAdmin,
    proverProxy: proverProxyEnabled(req),
    batchTransfer: config.enableBatchTransfer
  };
}

async function localAccountsForPublicConfig(serverFeatures) {
  if (!serverFeatures.localSignerAdmin && !serverFeatures.relayer) {
    return [];
  }
  const accounts = await localAccounts();
  if (serverFeatures.localSignerAdmin) {
    return accounts;
  }
  return accounts.filter(account => account.name === localRelayerName());
}

function publicConfig(req) {
  const serverFeatures = serverFeaturesForRequest(req);
  const exposeLocalAdmin = serverFeatures.localSignerAdmin;
  const proverUrl = browserProverUrl(req);
  const chainProfiles = dappChainProfiles(proverUrl);
  const activeChainProfileIdValue = activeChainProfileId();
  const activeProfile = chainProfiles.find(profile => profile.id === activeChainProfileIdValue);
  if (!activeProfile) {
    throw new Error("active DApp chain profile is unavailable");
  }
  return {
    schemaVersion: "clairveil-web-client-config-v1",
    serverBacked: true,
    modeLabel: config.localTestMode ? "Local Note Test Web" : "Public Node DApp",
    home: exposeLocalAdmin ? config.home : "",
    localSignerHome: exposeLocalAdmin ? localSignerHome() : "",
    localSignerBin: exposeLocalAdmin ? localSignerBin() : "",
    chainId: activeProfile.chainId,
    rpc: activeProfile.rpc,
    rest: activeProfile.rest,
    proverUrl: activeProfile.proverUrl,
    transport: activeProfile.transport,
    denom: activeProfile.denom,
    displayDenom: activeProfile.displayDenom,
    coinDecimals: activeProfile.coinDecimals,
    // The flattened EVM value remains host-chain metadata for compatibility.
    // ClairveilJS receives the selected profile's privacy prefix instead.
    accountPrefix: activeProfile.transport === "evm" ? config.accountPrefix : activeProfile.accountPrefix,
    shieldedPrefix: activeProfile.shieldedPrefix,
    localTestMode: config.localTestMode,
    serverFeatures,
    activeChainProfileId: activeChainProfileIdValue,
    chainProfiles,
    ...(activeProfile.keplrChainInfo
      ? { keplrChainInfo: activeProfile.keplrChainInfo }
      : {}),
    ...(activeProfile.transport === "evm" ? {
      evmRpc: activeProfile.evmRpc,
      evmChainId: activeProfile.evmChainId,
      evmChainName: activeProfile.evmChainName,
      evmPrivacyPrecompileAddress: activeProfile.evmPrivacyPrecompileAddress,
      evmGasLimit: activeProfile.evmGasLimit,
      evmSendGasLimit: activeProfile.evmSendGasLimit
    } : {})
  };
}

async function evmJsonRpc(method, params = []) {
  const response = await fetchEvmRpcResponse(config.evmRpc, {
    method: "POST",
    headers: {
      accept: "application/json",
      "content-type": "application/json",
    },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: Date.now(),
      method,
      params
    })
  });
  let data;
  try {
    data = JSON.parse(response.body.toString("utf8"));
  } catch (cause) {
    throw new Error(`EVM RPC ${method} response was not valid JSON`, { cause });
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    throw new Error(`EVM RPC ${method} response was not a JSON object`);
  }
  if (data.error || !Object.hasOwn(data, "result")) {
    throw new Error(`EVM RPC ${method} returned an invalid JSON-RPC response`);
  }
  return data.result;
}

async function sendEvmFaucet({ from, recipient, amount }) {
  const recipientEvm = validateEvmAddress(recipient);
  const provider = boundedEvmJsonRpcProvider();
  const wallet = evmWalletForLocalSigner(from).connect(provider);
  const tx = await wallet.sendTransaction({
    to: recipientEvm,
    value: amount.amount.toString(),
    gasLimit: 21000
  });
  const txHash = validateTxHashHex(tx.hash);
  const receipt = await waitForEvmReceipt(txHash);
  if (!receipt) {
    throw new Error(`faucet tx was broadcast but not found yet: ${txHash}`);
  }
  if (!hasSuccessfulEvmReceiptStatus(receipt)) {
    throw new Error(`faucet tx did not include an explicit successful EVM receipt status: ${String(receipt.status ?? "missing")}`);
  }
  return {
    txHash,
    receipt,
    recipientEvm
  };
}

async function waitForEvmReceipt(txHash, { attempts = 30, intervalMs = 1000 } = {}) {
  const hash = `0x${validateTxHashHex(txHash).toLowerCase()}`;
  for (let i = 0; i < attempts; i += 1) {
    const receipt = await evmJsonRpc("eth_getTransactionReceipt", [hash]);
    if (receipt) return receipt;
    await new Promise(resolve => setTimeout(resolve, intervalMs));
  }
  return null;
}

async function handleApi(req, res, url) {
  let privacyOperationSensitive = false;
  try {
    if (req.method === "GET" && url.pathname === "/api/config") {
      const cfg = publicConfig(req);
      sendJson(res, 200, {
        ...cfg,
        accounts: await localAccountsForPublicConfig(cfg.serverFeatures)
      });
      return;
    }

    if (req.method === "GET" && url.pathname === "/api/health") {
      const cfg = publicConfig(req);
      const [status, tree, audit, accounts] = await Promise.allSettled([
        fetchJson(rpcHttpUrl("/status")),
        fetchJson(restUrl("/clairveil/privacy/v1/tree_state")),
        fetchJson(restUrl("/clairveil/privacy/v1/audit_config")),
        localAccountsForPublicConfig(cfg.serverFeatures)
      ]);
      sendJson(res, 200, {
        config: cfg,
        status: status.status === "fulfilled" ? status.value.result : null,
        tree: tree.status === "fulfilled" ? tree.value : null,
        audit: audit.status === "fulfilled" ? audit.value : null,
        accounts: accounts.status === "fulfilled" ? accounts.value : [],
        errors: [status, tree, audit, accounts]
          .filter(result => result.status === "rejected")
          .map(result => result.reason.message)
      });
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/local-signers/ensure") {
      assertLocalTestBackendAllowed("local signer setup");
      assertSignerMutationAllowed(req);
      sendJson(res, 200, {
        accounts: await ensureLocalSigners()
      });
      return;
    }

    if (req.method === "GET" && url.pathname === "/api/auditor/test-scalar") {
      assertLocalTestBackendAllowed("auditor test scalar");
      assertLocalAdminAccessAllowed(req);
      // The helper derives this scalar from root authority material. Keep
      // helper diagnostics private and expose only the two UI-required fields.
      privacyOperationSensitive = true;
      const audit = await fetchJson(restUrl("/clairveil/privacy/v1/audit_config"));
      const auditMasterPubKeyHex = audit.audit_master_pubkey_hex || "";
      const material = testAuditMaterialFromConfig(auditMasterPubKeyHex) ?? await runAuditorMaterial({
        home: localSignerHome(),
        key_name: "auditor",
        keyring_backend: localSignerKeyring(),
        account_prefix: config.accountPrefix
      });
      sendJson(res, 200, {
        disclosure_private_scalar_hex: material.disclosure_private_scalar_hex,
        matches_audit_config: Boolean(
          auditMasterPubKeyHex &&
          material.disclosure_pubkey_hex &&
          auditMasterPubKeyHex.toLowerCase() === material.disclosure_pubkey_hex.toLowerCase()
        )
      });
      return;
    }

    if (req.method === "GET" && url.pathname === "/api/auditor/transfers") {
      assertLocalTestBackendAllowed("auditor transfers");
      assertLocalAdminAccessAllowed(req);
      // Keep the local-admin panel on the DApp's same origin. This stays
      // page-bounded: individual batch output evidence is loaded only after
      // the operator selects that event for decode.
      sendJson(res, 200, await fetchAuditorTransferEvents({
        page: paginationInteger(url.searchParams.get("page"), "auditor page", {
          fallback: 1,
          maximum: 1_000_000,
        }),
        limit: paginationInteger(url.searchParams.get("limit"), "auditor page limit", {
          fallback: 20,
          maximum: 200,
        }),
      }));
      return;
    }

    // Test/admin-only route. Public DApps must not receive or relay audit disclosure private scalars.
    if (req.method === "POST" && url.pathname === "/api/auditor/decode") {
      assertLocalTestBackendAllowed("auditor disclosure decode");
      assertLocalAdminAccessAllowed(req);
      // The request contains a disclosure private scalar. Treat every
      // subsequent failure as sensitive so no route or SDK diagnostic can
      // echo key material back to the browser.
      privacyOperationSensitive = true;
      const body = await readBody(req);
      const event = auditorEventReference(body);
      const disclosurePrivKeyHex = body.disclosurePrivKeyHex ??
        body.disclosure_privkey_hex;
      if (!disclosurePrivKeyHex) {
        throw new Error("disclosurePrivKeyHex is required for auditor JS decode");
      }
      if (event.eventType === "batch_transfer") {
        const batchOutputs = await batchAuditOutputsForEvent(event);
        // One batch has a mandatory audit envelope per typed output. Never
        // reconstruct those envelopes from a lossy public event: each decode
        // verifies the output index, commitment, and proof-bound full digest.
        const outputs = await Promise.all(batchOutputs.map(async output => ({
          output_index: Number(output.output_index ?? output.outputIndex ?? 0),
          report: await clairveil.decodeBatchAuditDisclosure({
            output,
            txHash: event.txHash,
            disclosurePrivKeyHex
          })
        })));
        sendJson(res, 200, {
          tx_hash: event.txHash,
          event_type: "batch_transfer",
          outputs
        });
        return;
      }
      sendJson(res, 200, await clairveil.decodeAuditDisclosure({
        txHash: event.txHash,
        disclosurePrivKeyHex,
        page: event.page,
        limit: 20,
        maxPages: 1,
        eventTypes: ["shielded_transfer", "batch_transfer"],
      }));
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/faucet") {
      assertLocalTestBackendAllowed("faucet");
      assertSignerMutationAllowed(req);
      const body = await readBody(req);
      const rawRecipient = String(body.recipient || "").trim();
      const amount = normalizeFaucetAmount(body.amount);
      const from = validateAccount(body.from ?? "alice");

      if (isEvmTransport()) {
        const recipient = validateEvmAddress(rawRecipient);
        const beforeBalance = await queryEvmNativeBalance(recipient);
        const faucet = await sendEvmFaucet({
          from,
          recipient,
          amount: parseCoin(amount.funded)
        });
        const balance = await queryEvmNativeBalance(recipient);
        sendJson(res, 200, {
          broadcast: { txhash: faucet.txHash },
          receipt: faucet.receipt,
          balance,
          beforeBalance,
          amount,
          from,
          recipient,
          recipientEvm: faucet.recipientEvm
        });
        return;
      }

      const recipient = validateClairAddress(rawRecipient);
      const beforeBalance = await queryBalances(recipient);
      const result = await runClairveild([
        "tx", "bank", "send", from, recipient, amount.funded,
        "--from", from,
        "--keyring-backend", "test",
        "--home", config.home,
        "--node", config.rpc,
        "--chain-id", config.chainId,
        "--gas", "200000",
        "--gas-prices", config.gasPrices,
        "--yes",
        "--output", "json"
      ]);
      const tx = await waitForTx(result.json.txhash);
      if (!tx) {
        throw new Error(`faucet tx was broadcast but not found yet: ${result.json.txhash}`);
      }
      if (Number(tx.code || 0) !== 0) {
        throw new Error(tx.raw_log || `faucet tx failed with code ${tx.code}`);
      }
      const balance = await queryBalances(recipient);
      sendJson(res, 200, { broadcast: result.json, tx, balance, beforeBalance, amount, from, recipient });
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/relayer/withdraw") {
      assertLocalTestBackendAllowed("relay withdraw");
      assertSignerMutationAllowed(req);

      const body = await readBody(req);
      const payload = body.payload;
      const relayer = validateAccount(body.relayer ?? body.from ?? localRelayerName());
      const account = (await localAccounts()).find(entry => entry.name === relayer);
      if (!account?.transparentAddress) {
        throw new Error(`local relayer account ${relayer} is not initialized`);
      }
      privacyOperationSensitive = true;

      if (isEvmTransport()) {
        const provider = boundedEvmJsonRpcProvider();
        const chainNowUnix = await relayChainNowUnix(provider);
        const evmClient = createClairveilEvmClient({
          contractAddress: config.evmPrivacyPrecompileAddress,
          chainId: config.chainId,
          accountPrefix: evmPrivacyAccountPrefix(),
          shieldedPrefix: config.shieldedPrefix,
          defaultDenom: config.denom
        });
        const built = await evmClient.buildWithdrawTransaction({
          payload,
          relayer: account.transparentAddress,
          chainNowUnix,
          expectedChainId: config.chainId,
          expectedRecipient: body.expectedRecipient ?? body.expected_recipient ?? payload?.recipient
        });
        const wallet = evmWalletForLocalSigner(relayer).connect(provider);
        await assertRelayPayloadNotExpired(payload, provider);
        const { txHash, receipt } = await relaySubmissionCoordinator.run(
          relayPayloadNullifierLockKey(payload),
          relaySubmissionIdempotencyKey(payload),
          async (markSubmissionStarted) => {
            const tx = await submitRelayAfterNullifierPreflight({
              payload,
              checkNullifiers: (nullifiers) => clairveil.checkNullifiers(nullifiers),
              submit: () => {
                markSubmissionStarted();
                return wallet.sendTransaction({
                  to: built.transaction.to,
                  data: built.transaction.data,
                  value: built.transaction.value ?? "0x0",
                  gasLimit: BigInt(config.evmGasLimit)
                });
              },
            });
            const txHash = validateTxHashHex(tx.hash);
            const receipt = await waitForEvmReceipt(txHash);
            if (!receipt) {
              const error = new Error(`relay withdraw tx was broadcast but not found yet: ${txHash}`);
              error.txHash = txHash;
              throw error;
            }
            if (!hasSuccessfulEvmReceiptStatus(receipt)) {
              const failed = hasFailedEvmReceiptStatus(receipt);
              const error = new Error(
                failed
                  ? `relay withdraw EVM execution failed with receipt status: ${String(receipt.status)}`
                  : `relay withdraw tx did not include an explicit EVM receipt status: ${String(receipt.status ?? "missing")}`,
              );
              error.txHash = txHash;
              error.receipt = receipt;
              if (failed) {
                error.receiptStatus = String(receipt.status);
                error.executionFailed = true;
              }
              throw error;
            }
            return { txHash, receipt };
          }
        );
        sendJson(res, 200, {
          broadcast: { txhash: txHash },
          receipt,
          relayer,
          relayerAddress: account.transparentAddress,
          relayerEvmAddress: wallet.address,
          payloadHash: payload?.payload_hash || "",
          message: {
            creator: built.message.creator,
            amount: built.message.amount,
            recipient: built.message.recipient,
            evmRecipient: built.message.evmRecipient || "",
            chainId: built.message.chainId,
            expiresAtUnix: built.message.expiresAtUnix?.toString?.() ?? String(built.message.expiresAtUnix ?? "")
          }
        });
        return;
      }

      const chainNowUnix = await relayChainNowUnix();
      // A relay payload expires at the start of its declared expiry second.
      // Keep this preflight aligned with the final just-before-submit check:
      // do not construct a local relay transaction for an already-expired
      // payload and rely on the later check to reject it.
      if (chainNowUnix >= relayPayloadExpiryUnix(payload)) {
        throw new Error("relay withdraw payload is expired at the latest chain block time");
      }
      const message = clairveil.buildRelayWithdrawMessageFromPayload({
        payload,
        relayer: account.transparentAddress,
        chainNowUnix,
        expectedChainId: config.chainId,
        expectedRecipient: body.expectedRecipient ?? body.expected_recipient ?? payload?.recipient,
        accountPrefix: config.accountPrefix
      });

      const workDir = await mkdtemp(join(tmpdir(), "clairveil-relay-withdraw-"));
      const payloadPath = join(workDir, "payload.json");
      try {
        await assertRelayPayloadNotExpired(payload);
        await writeFile(payloadPath, JSON.stringify(payload, null, 2), "utf8");
        const { result, txHash, tx } = await relaySubmissionCoordinator.run(
          relayPayloadNullifierLockKey(payload),
          relaySubmissionIdempotencyKey(payload),
          async (markSubmissionStarted) => {
            const result = await submitRelayAfterNullifierPreflight({
              payload,
              checkNullifiers: (nullifiers) => clairveil.checkNullifiers(nullifiers),
              submit: () => {
                markSubmissionStarted();
                return runClairveild([
                  "tx", "privacy", "relay-withdraw", payloadPath,
                  "--from", relayer,
                  "--keyring-backend", "test",
                  "--home", config.home,
                  "--node", config.rpc,
                  "--chain-id", config.chainId,
                  "--gas", "5000000",
                  "--gas-prices", config.gasPrices,
                  "--yes",
                  "--output", "json"
                ]);
              },
            });
            const txHash = result.json.txhash;
            const checkTxCode = confirmedCosmosTxCode(result.json);
            if (checkTxCode != null && checkTxCode !== 0) {
              const error = new Error(
                result.json.raw_log || `relay withdraw CheckTx failed with code ${checkTxCode}`,
              );
              error.txHash = txHash;
              error.txCode = checkTxCode;
              throw error;
            }
            const tx = await waitForTx(txHash);
            if (!tx) {
              const error = new Error(`relay withdraw tx was broadcast but not found yet: ${txHash}`);
              error.txHash = txHash;
              throw error;
            }
            const txCode = confirmedCosmosTxCode(tx);
            if (txCode !== 0) {
              const error = new Error(
                txCode == null
                  ? "relay withdraw tx did not include a valid result code"
                  : tx.raw_log || `relay withdraw tx failed with code ${txCode}`,
              );
              error.txHash = txHash;
              error.txCode = txCode;
              throw error;
            }
            return { result, txHash, tx };
          }
        );
        sendJson(res, 200, {
          broadcast: result.json,
          tx,
          relayer,
          relayerAddress: account.transparentAddress,
          payloadHash: payload?.payload_hash || "",
          message: {
            creator: message.creator,
            amount: message.amount,
            recipient: message.recipient,
            chainId: message.chainId,
            expiresAtUnix: message.expiresAtUnix?.toString?.() ?? String(message.expiresAtUnix ?? "")
          }
        });
      } finally {
        await rm(workDir, { recursive: true, force: true });
      }
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/deposit/proof") {
      assertLocalTestBackendAllowed("deposit proof");
      assertSignerMutationAllowed(req);
      const body = await readBody(req);
      privacyOperationSensitive = true;
      sendJson(res, 200, await runDepositProof({
        note_json: body.note_json ?? body.noteJson,
        note_commitment_hex: body.note_commitment_hex ?? body.noteCommitmentHex
      }));
      return;
    }

    const showAddress = url.pathname.match(/^\/api\/wallet\/([^/]+)\/show-address$/);
    if (req.method === "GET" && showAddress) {
      assertLocalTestBackendAllowed("local wallet show-address");
      assertLocalAdminAccessAllowed(req);
      const from = validateAccount(showAddress[1]);
      const result = await runLocalSigner([
        "tx", "privacy", "show-address",
        "--from", from,
        "--keyring-backend", localSignerKeyring(),
        "--home", localSignerHome(),
        "--output", "json"
      ]);
      sendJson(res, 200, result.json);
      return;
    }

    const listNotes = url.pathname.match(/^\/api\/wallet\/([^/]+)\/notes$/);
    if (req.method === "GET" && listNotes) {
      assertLocalTestBackendAllowed("local wallet note scan");
      assertLocalAdminAccessAllowed(req);
      const from = validateAccount(listNotes[1]);
      const result = await runLocalSigner([
        "tx", "privacy", "list-notes",
        "--from", from,
        "--keyring-backend", localSignerKeyring(),
        "--home", localSignerHome(),
        "--node", config.rpc,
        "--json"
      ]);
      sendJson(res, 200, result.json);
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/deposit") {
      assertLocalTestBackendAllowed("local CLI deposit");
      assertSignerMutationAllowed(req);
      const body = await readBody(req);
      const from = validateAccount(body.from);
      const amount = validateCoin(body.amount);
      const result = await runClairveild([
        "tx", "privacy", "deposit", amount,
        "--from", from,
        "--keyring-backend", "test",
        "--home", config.home,
        "--node", config.rpc,
        "--chain-id", config.chainId,
        "--gas", "2500000",
        "--gas-prices", config.gasPrices,
        "--yes",
        "--output", "json"
      ]);
      const tx = await waitForTx(result.json.txhash);
      sendJson(res, 200, { broadcast: result.json, tx });
      return;
    }

    sendJson(res, 404, { error: "not found", code: "not_found", version: "v1" });
  } catch (error) {
    let responseError = privacyOperationSensitive
      ? privacySensitiveOperationError(error)
      : error;
    if (
      privacyOperationSensitive &&
      req.method === "POST" &&
      url.pathname === "/api/relayer/withdraw" &&
      (!responseError?.clairveilCode ||
        responseError.clairveilCode === ClairveilErrorCode.INVALID_ARGUMENT)
    ) {
      responseError.clairveilCode = relayWithdrawSafeFailureCode(responseError);
    }
    sendJson(
      res,
      error?.statusCode || 400,
      errorPayload(responseError),
    );
  }
}

function configuredConnectOrigins(req) {
  const profile = dappChainProfiles(browserProverUrl(req)).find(
    candidate => candidate.id === activeChainProfileId(),
  );
  const origins = new Set();
  for (const endpoint of [
    profile?.rpc,
    profile?.rest,
    ...(profile?.restEndpoints || []),
    profile?.proverUrl,
    profile?.depositProofUrl,
    profile?.evmRpc,
  ]) {
    if (!endpoint) continue;
    try {
      origins.add(new URL(endpoint).origin);
    } catch {
      // Endpoint validation rejects malformed public-mode values. Local mode
      // still receives a restrictive self-only CSP when a value is malformed.
    }
  }
  return [...origins];
}

function staticSecurityHeaders(req) {
  const connectSources = ["'self'", ...configuredConnectOrigins(req)].join(" ");
  return {
    "content-security-policy": [
      "default-src 'self'",
      "base-uri 'none'",
      "object-src 'none'",
      "frame-ancestors 'none'",
      "form-action 'self'",
      `connect-src ${connectSources}`,
      "script-src 'self'",
      "style-src 'self'",
      "img-src 'self' data:",
    ].join("; "),
  };
}

function serveStatic(req, res, url) {
  const requested = url.pathname === "/" ? "/index.html" : url.pathname;
  const path = resolve(join(publicDir, requested));
  if (path !== publicDir && !path.startsWith(publicDir + "/")) {
    sendJson(res, 403, { error: "forbidden", code: "forbidden", version: "v1" });
    return;
  }
  const fallbackToIndex = !extname(path);
  const filePath = existsSync(path) ? path : fallbackToIndex ? join(publicDir, "index.html") : "";
  if (!filePath) {
    sendJson(res, 404, { error: "not found", code: "not_found", version: "v1" });
    return;
  }
  const contentType = contentTypes.get(extname(filePath)) ?? "application/octet-stream";
  const stream = createReadStream(filePath);
  stream.on("error", () => {
    sendJson(res, 404, { error: "not found", code: "not_found", version: "v1" });
  });
  stream.pipe(res.writeHead(200, {
    "content-type": contentType,
    "cache-control": "no-store",
    ...staticSecurityHeaders(req),
  }));
}

const server = createServer((req, res) => {
  res.setHeader("x-content-type-options", "nosniff");
  res.setHeader("referrer-policy", "no-referrer");
  res.setHeader("cross-origin-opener-policy", "same-origin");
  const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "localhost"}`);
  if (proverProxyPath(url.pathname)) {
    handleProverProxy(req, res, url);
    return;
  }
  if (url.pathname.startsWith("/api/")) {
    handleApi(req, res, url);
    return;
  }
  serveStatic(req, res, url);
});

// Limit both headers and complete request bodies. Individual upstream and
// helper calls retain their own longer operation timeouts after parsing.
server.headersTimeout = inboundRequestTimeoutMs;
server.requestTimeout = inboundRequestTimeoutMs;

server.listen(config.port, config.host, () => {
  console.log(`Clairveil DApp: ${dappUrls().join(", ")}`);
  console.log(`Clairveil home: ${config.home}`);
  console.log(`RPC: ${config.rpc}`);
  console.log(`REST: ${config.rest}`);
  console.log(`Keplr RPC: ${publicRpcEndpoint()}`);
  console.log(`Keplr REST: ${publicRestEndpoint()}`);
});
