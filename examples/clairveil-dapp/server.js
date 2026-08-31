import { createServer } from "node:http";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { createReadStream, existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir, networkInterfaces, tmpdir } from "node:os";
import { JsonRpcProvider, Wallet } from "ethers";
import {
  createClairveilClient,
  createClairveilEvmClient,
  ClairveilError,
  ClairveilErrorCode,
  derivePrivacyMaterial,
  isEvmAddress,
  plannerStatusToErrorCode
} from "clairveiljs";
import { validateBrowserWalletProfile } from "clairveiljs/browser-dapp";
import {
  normalizeConfiguredTransport,
  resolveProfileDenom
} from "./server-profile-config.js";
import {
  assertCosmosCheckTxAccepted,
  confirmedCosmosTxCode,
  createRelayAccountSubmissionSerializer,
  createRelayWithdrawSubmissionGate,
  trackedCosmosSubmissionOutcome,
  trackedEvmSubmissionOutcome,
  waitForTrackedSubmissionOutcome,
} from "./server-relay-submission.js";
import {
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus,
} from "./public/transaction-status.js";

const __dirname = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(__dirname, "../..");
const publicDir = join(__dirname, "public");
const relayWithdrawSubmissionGate = createRelayWithdrawSubmissionGate();
const relayAccountSubmissionSerializer = createRelayAccountSubmissionSerializer();
const defaultHome = existsSync("/tmp/clairveil-codex-home-2")
  ? "/tmp/clairveil-codex-home-2"
  : existsSync("/tmp/clairveil-codex-home")
  ? "/tmp/clairveil-codex-home"
  : join(homedir(), ".clairveil");

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
const configuredTransport = normalizeConfiguredTransport(process.env.CLAIRVEIL_TRANSPORT);
const configuredCosmosDenom = resolveProfileDenom({
  transport: "cosmos",
  environment: process.env
});
const configuredEvmDenom = resolveProfileDenom({
  transport: "evm",
  environment: process.env
});
const configuredDenom = configuredTransport === "evm"
  ? configuredEvmDenom
  : configuredCosmosDenom;

function nonEmptyConfiguredText(value, fallback) {
  return String(value ?? "").trim() || fallback;
}

const sharedChainId = nonEmptyConfiguredText(process.env.CHAIN_ID, "clairveil-local-2");
const sharedAccountPrefix = nonEmptyConfiguredText(process.env.CLAIRVEIL_ACCOUNT_PREFIX, "clair");
const sharedShieldedPrefix = nonEmptyConfiguredText(process.env.CLAIRVEIL_SHIELDED_PREFIX, "clairs");
const configuredCosmosChainId = nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_CHAIN_ID,
  sharedChainId,
);
const configuredCosmosAccountPrefix = nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_ACCOUNT_PREFIX,
  sharedAccountPrefix,
);
const configuredCosmosShieldedPrefix = nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_SHIELDED_PREFIX,
  sharedShieldedPrefix,
);
const configuredCosmosChainName = nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_CHAIN_NAME,
  "Clairveil Localnet",
);
const configuredCosmosLabel = nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_LABEL,
  configuredCosmosChainName,
);
const configuredCosmosCoinType = Number(nonEmptyConfiguredText(
  process.env.CLAIRVEIL_COSMOS_COIN_TYPE,
  nonEmptyConfiguredText(process.env.CLAIRVEIL_KEPLR_COIN_TYPE, "118"),
));
if (!Number.isSafeInteger(configuredCosmosCoinType) || configuredCosmosCoinType < 0) {
  throw new Error("CLAIRVEIL_COSMOS_COIN_TYPE must be a non-negative safe integer");
}

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

function positiveIntegerEnv(name, defaultValue) {
  const value = Number(process.env[name] ?? defaultValue);
  if (!Number.isSafeInteger(value) || value <= 0) {
    throw new Error(`${name} must be a positive integer`);
  }
  return value;
}

function envEndpointList(value) {
  return [...new Set(String(value || "").split(",").map(entry => entry.trim()).filter(Boolean))];
}

function optionalJsonObjectEnv(name) {
  const raw = String(process.env[name] || "").trim();
  if (!raw) return null;
  let value;
  try {
    value = JSON.parse(raw);
  } catch (error) {
    throw new Error(`${name} must contain valid JSON: ${error.message}`);
  }
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(`${name} must contain a JSON object`);
  }
  return value;
}

const localTestMode = resolveLocalTestMode();
const config = {
  host: cliOptions.host ?? process.env.CLAIRVEIL_DAPP_HOST ?? "127.0.0.1",
  port: Number(cliOptions.port ?? process.env.PORT ?? process.env.CLAIRVEIL_DAPP_PORT ?? 5173),
  home: process.env.CLAIRVEIL_HOME ?? process.env.CLAIRVEIL_DAPP_HOME ?? defaultHome,
  chainId: configuredTransport === "cosmos" ? configuredCosmosChainId : sharedChainId,
  bin: process.env.CLAIRVEILD_BIN ?? "clairveild",
  rpc: process.env.CLAIRVEIL_RPC ?? "tcp://127.0.0.1:26657",
  rest: process.env.CLAIRVEIL_REST ?? "http://127.0.0.1:1317",
  publicRpc: process.env.CLAIRVEIL_PUBLIC_RPC ?? "",
  publicRest: process.env.CLAIRVEIL_PUBLIC_REST ?? "",
  publicRestEndpoints: envEndpointList(process.env.CLAIRVEIL_PUBLIC_REST_ENDPOINTS),
  cosmosRestEndpoints: envEndpointList(process.env.CLAIRVEIL_COSMOS_REST_ENDPOINTS),
  evmHostRestEndpoints: envEndpointList(process.env.CLAIRVEIL_EVM_HOST_REST_ENDPOINTS),
  proverUrl: process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  publicProverUrl: process.env.CLAIRVEIL_PUBLIC_PROVER_URL ?? process.env.CLAIRVEIL_PROVER_PUBLIC_URL ?? process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  proverBearerToken: process.env.CLAIRVEIL_PROVER_BEARER_TOKEN ?? process.env.CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN ?? "",
  proverTimeoutMs: positiveIntegerEnv("CLAIRVEIL_PROVER_TIMEOUT_MS", 120000),
  proverProxyEnabled: localTestMode && envFlag("CLAIRVEIL_PROVER_PROXY_ENABLED", false),
  proverProxyRateLimitWindowMs: positiveIntegerEnv("CLAIRVEIL_PROVER_PROXY_RATE_LIMIT_WINDOW_MS", 60000),
  proverProxyRateLimitMax: positiveIntegerEnv("CLAIRVEIL_PROVER_PROXY_RATE_LIMIT_MAX", 30),
  proverProxyMaxInFlight: positiveIntegerEnv("CLAIRVEIL_PROVER_PROXY_MAX_IN_FLIGHT", 2),
  proverProxyMaxResponseBytes: positiveIntegerEnv("CLAIRVEIL_PROVER_PROXY_MAX_RESPONSE_BYTES", 1024 * 1024),
  upstreamTimeoutMs: positiveIntegerEnv("CLAIRVEIL_DAPP_UPSTREAM_TIMEOUT_MS", 10000),
  upstreamMaxResponseBytes: positiveIntegerEnv("CLAIRVEIL_DAPP_UPSTREAM_MAX_RESPONSE_BYTES", 1024 * 1024),
  healthMaxInFlight: positiveIntegerEnv("CLAIRVEIL_DAPP_HEALTH_MAX_IN_FLIGHT", 8),
  enableBatchTransfer: envFlag("CLAIRVEIL_DAPP_ENABLE_BATCH_TRANSFER", false),
  depositProofUrl: process.env.CLAIRVEIL_DEPOSIT_PROOF_URL ?? "",
  publicDepositProofUrl: process.env.CLAIRVEIL_PUBLIC_DEPOSIT_PROOF_URL ?? "",
  cosmosDepositProofUrl: process.env.CLAIRVEIL_COSMOS_DEPOSIT_PROOF_URL ?? "",
  evmDepositProofUrl: process.env.CLAIRVEIL_EVM_DEPOSIT_PROOF_URL ?? "",
  publicOrigin: process.env.CLAIRVEIL_DAPP_PUBLIC_ORIGIN ?? "",
  transport: configuredTransport,
  denom: configuredDenom,
  cosmosDenom: configuredCosmosDenom,
  evmDenom: configuredEvmDenom,
  displayDenom: process.env.CLAIRVEIL_DISPLAY_DENOM ?? "CLAIR",
  coinDecimals: Number(process.env.CLAIRVEIL_COIN_DECIMALS ?? 18),
  keplrCoinType: configuredCosmosCoinType,
  accountPrefix: configuredTransport === "cosmos"
    ? configuredCosmosAccountPrefix
    : sharedAccountPrefix,
  shieldedPrefix: configuredTransport === "cosmos"
    ? configuredCosmosShieldedPrefix
    : sharedShieldedPrefix,
  gasPrices: process.env.CLAIRVEIL_GAS_PRICES ?? `1${configuredDenom}`,
  evmRpc: process.env.CLAIRVEIL_EVM_RPC ?? "http://127.0.0.1:8545",
  evmChainId: normalizeEvmChainId(process.env.CLAIRVEIL_EVM_CHAIN_ID ?? "815"),
  evmChainName: process.env.CLAIRVEIL_EVM_CHAIN_NAME ?? "EVM Localnet",
  // The privacy contract address belongs to the selected EVM deployment
  // profile. ClairveilJS intentionally does not provide a chain-specific
  // fallback, so Cosmos mode may leave this blank while EVM profile validation
  // fails closed unless the deployment supplies it.
  evmPrivacyPrecompileAddress: nonEmptyConfiguredText(
    process.env.CLAIRVEIL_EVM_PRIVACY_PRECOMPILE,
    "",
  ),
  evmDepositMode: process.env.CLAIRVEIL_EVM_DEPOSIT_MODE || "payable-exact-value",
  evmNativeDenom: process.env.CLAIRVEIL_EVM_NATIVE_DENOM || configuredEvmDenom,
  evmAuthorizationProfile: optionalJsonObjectEnv("CLAIRVEIL_EVM_AUTHORIZATION_PROFILE"),
  evmGasLimit: process.env.CLAIRVEIL_EVM_GAS_LIMIT ?? "0x989680",
  evmSendGasLimit: process.env.CLAIRVEIL_EVM_SEND_GAS_LIMIT ?? "0x5208",
  localTestMode,
  allowLanSigning: process.env.CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING === "1",
  allowLanAdmin: process.env.CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN === "1",
  allowLanProver: process.env.CLAIRVEIL_DAPP_ALLOW_LAN_PROVER === "1",
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
  return dappChainProfiles()[0]?.rpc || httpRpcEndpoint(config.publicRpc || config.rpc);
}

function publicRestEndpoint() {
  return dappChainProfiles()[0]?.rest || (config.publicRest || config.rest).replace(/\/$/, "");
}

function configuredRestEndpoints(primary, configured = []) {
  const entries = Array.isArray(configured)
    ? configured
    : String(configured || "").split(",");
  return [...new Set([primary, ...entries]
    .map(value => String(value || "").trim().replace(/\/$/, ""))
    .filter(Boolean))];
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

function relaySubmissionAccountKey(relayer) {
  return JSON.stringify([
    config.transport,
    config.chainId,
    isEvmTransport() ? config.evmChainId : "",
    relayer,
  ]);
}

function evmPrivacyAccountPrefix() {
  return process.env.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX ?? "clair";
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

function dappChainProfiles() {
  const proxyDepositProofUrl = config.proverProxyEnabled && config.depositProofUrl
    ? `http://127.0.0.1:${config.port}/v1/prover/deposit`
    : "";
  const sharedDepositProofUrl = config.publicDepositProofUrl || proxyDepositProofUrl;
  const cosmosRpc = httpRpcEndpoint(
    process.env.CLAIRVEIL_COSMOS_RPC
      || config.publicRpc
      || (isEvmTransport() ? "tcp://127.0.0.1:26657" : config.rpc),
  );
  const cosmosRest = (
    process.env.CLAIRVEIL_COSMOS_REST
      || config.publicRest
      || (isEvmTransport() ? "http://127.0.0.1:1317" : config.rest)
  ).replace(/\/$/, "");
  const cosmosRestEndpoints = configuredRestEndpoints(
    cosmosRest,
    config.cosmosRestEndpoints.length
      ? config.cosmosRestEndpoints
      : config.publicRestEndpoints,
  );
  const cosmosDepositProofUrl = config.cosmosDepositProofUrl || sharedDepositProofUrl;
  const clairveilProfile = {
    id: "clairveil-local",
    label: configuredCosmosLabel,
    chainName: configuredCosmosChainName,
    transport: "cosmos",
    wallet: "keplr",
    chainId: configuredCosmosChainId,
    rpc: cosmosRpc,
    rest: cosmosRest,
    ...(cosmosRestEndpoints.length > 1 ? { restEndpoints: cosmosRestEndpoints } : {}),
    proverUrl: process.env.CLAIRVEIL_COSMOS_PROVER_URL || config.publicProverUrl,
    ...(cosmosDepositProofUrl ? { depositProofUrl: cosmosDepositProofUrl } : {}),
    accountPrefix: configuredCosmosAccountPrefix,
    shieldedPrefix: configuredCosmosShieldedPrefix,
    denom: config.cosmosDenom,
    displayDenom: process.env.CLAIRVEIL_COSMOS_DISPLAY_DENOM || config.displayDenom,
    coinDecimals: Number(process.env.CLAIRVEIL_COSMOS_COIN_DECIMALS || config.coinDecimals),
    keplrCoinType: config.keplrCoinType,
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

  const evmDepositProofUrl = config.evmDepositProofUrl || sharedDepositProofUrl;
  const evmProfile = {
    id: "evm-local",
    label: config.evmChainName,
    chainName: config.evmChainName,
    transport: "evm",
    wallet: "metamask",
    chainId: process.env.CLAIRVEIL_EVM_HOST_CHAIN_ID ?? (isEvmTransport() ? config.chainId : "evm-local-1"),
    rpc: httpRpcEndpoint(process.env.CLAIRVEIL_EVM_HOST_RPC ?? (isEvmTransport() ? config.rpc : "tcp://127.0.0.1:26657")),
    rest: (process.env.CLAIRVEIL_EVM_HOST_REST ?? (isEvmTransport() ? config.rest : "http://127.0.0.1:1317")).replace(/\/$/, ""),
    ...(config.evmHostRestEndpoints.length ? { restEndpoints: config.evmHostRestEndpoints } : {}),
    proverUrl: process.env.CLAIRVEIL_EVM_PROVER_URL ?? config.publicProverUrl,
    ...(evmDepositProofUrl ? { depositProofUrl: evmDepositProofUrl } : {}),
    accountPrefix: process.env.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX ?? "clair",
    shieldedPrefix: process.env.CLAIRVEIL_EVM_SHIELDED_PREFIX ?? (isEvmTransport() ? config.shieldedPrefix : "clairs"),
    denom: config.evmDenom,
    displayDenom: process.env.CLAIRVEIL_EVM_DISPLAY_DENOM ?? (isEvmTransport() ? config.displayDenom : "TOKEN"),
    coinDecimals: Number(process.env.CLAIRVEIL_EVM_COIN_DECIMALS ?? (isEvmTransport() ? config.coinDecimals : 18)),
    evmRpc: config.evmRpc,
    evmChainId: config.evmChainId,
    evmChainName: config.evmChainName,
    evmPrivacyPrecompileAddress: config.evmPrivacyPrecompileAddress,
    evmDepositMode: config.evmDepositMode,
    evmNativeDenom: config.evmNativeDenom,
    ...(config.evmAuthorizationProfile ? { evmAuthorizationProfile: config.evmAuthorizationProfile } : {}),
    evmGasLimit: config.evmGasLimit,
    evmSendGasLimit: config.evmSendGasLimit
  };

  const activeProfile = isEvmTransport() ? evmProfile : clairveilProfile;
  if (activeProfile.denom !== config.denom) {
    throw new Error("active chain profile denom must match the server coin denom");
  }
  return [validateBrowserWalletProfile(activeProfile)];
}

function activeChainProfileId() {
  return dappChainProfiles().find(profile =>
    profile.transport === config.transport && profile.chainId === config.chainId
  )?.id || (isEvmTransport() ? "evm-local" : "clairveil-local");
}

function assertProductionHttpsUrl(value, label, { originOnly = false } = {}) {
  let url;
  try {
    url = new URL(String(value || "").trim());
  } catch {
    throw new Error(`${label} must be a valid HTTPS URL in public mode`);
  }
  if (
    url.protocol !== "https:"
    || !url.hostname
    || url.username
    || url.password
    || url.search
    || url.hash
    || (originOnly && url.pathname !== "/")
  ) {
    throw new Error(`${label} must be a valid HTTPS URL in public mode`);
  }
}

function assertProductionDeploymentConfig() {
  if (config.localTestMode) return;
  assertProductionHttpsUrl(config.publicOrigin, "CLAIRVEIL_DAPP_PUBLIC_ORIGIN", {
    originOnly: true,
  });
  if (envFlag("CLAIRVEIL_PROVER_PROXY_ENABLED", false)) {
    throw new Error(
      "CLAIRVEIL_PROVER_PROXY_ENABLED is local-test-only and must be disabled in public mode",
    );
  }
  if (config.proverBearerToken) {
    throw new Error(
      "A public DApp server must not hold CLAIRVEIL_PROVER_BEARER_TOKEN; use a reviewed gateway instead",
    );
  }
  for (const profile of dappChainProfiles()) {
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

function signerMutationAllowed(req) {
  return config.allowLanSigning || isLoopbackRemoteAddress(req.socket?.remoteAddress);
}

function localAdminAccessAllowed(req) {
  return config.allowLanAdmin || isLoopbackRemoteAddress(req.socket?.remoteAddress);
}

function signerMutationRequestOrigin(req) {
  const rawOrigin = String(req.headers?.origin || "").trim();
  const rawHost = String(req.headers?.host || "").trim();
  if (!rawOrigin || !rawHost || !configuredBrowserHostname(req)) return "";
  try {
    const origin = new URL(rawOrigin);
    const expected = new URL(`http://${rawHost}`);
    if (
      origin.origin !== rawOrigin
      || origin.username
      || origin.password
      || origin.origin !== expected.origin
    ) {
      return "";
    }
    return origin.origin;
  } catch {
    return "";
  }
}

function assertSignerMutationAllowed(req) {
  if (!signerMutationAllowed(req)) {
    throw httpError(
      403,
      "LAN access to signer-mutating APIs is disabled. Set CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1 to allow LAN signing."
    );
  }
  const contentType = String(req.headers?.["content-type"] || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
  if (contentType !== "application/json") {
    throw httpError(415, "signer-mutating APIs require application/json");
  }
  if (!signerMutationRequestOrigin(req)) {
    throw httpError(403, "signer-mutating APIs require an exact same-origin browser request");
  }
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

function sendJson(res, status, data) {
  const body = JSON.stringify(data, jsonReplacer, 2);
  res.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store"
  });
  res.end(body);
}

function sendPlannerResult(res, result) {
  const code = plannerStatusToErrorCode(result?.status);
  sendJson(res, 409, {
    error: result?.plan?.message || `privacy transaction is not ready: ${result?.status || "unknown"}`,
    code,
    status: result?.status || "",
    plan: result?.plan || null,
    prepared: result?.prepared || null
  });
}

function httpError(statusCode, message, code = ClairveilErrorCode.INVALID_ARGUMENT) {
  const error = new Error(message);
  error.statusCode = statusCode;
  error.clairveilCode = code;
  return error;
}

function errorPayload(error) {
  if (error instanceof ClairveilError) {
    return {
      error: error.message,
      code: error.code,
      ...(error.details || {})
    };
  }
  const checkTxRejected = error?.checkTxRejected === true;
  const txHash = String(error?.txHash || "").trim();
  const txCode = error?.txCode;
  return {
    error: error?.message || String(error),
    code: error?.clairveilCode || ClairveilErrorCode.INVALID_ARGUMENT,
    ...(checkTxRejected ? {
      checkTxRejected: true,
      rpcInvoked: error?.rpcInvoked === true,
      ...(Number.isSafeInteger(txCode) && txCode > 0 ? { txCode } : {}),
      ...(/^(0x)?[0-9a-fA-F]{64}$/.test(txHash) ? { txHash } : {})
    } : {})
  };
}

function readBody(req) {
  return new Promise((resolveBody, reject) => {
    let raw = "";
    req.on("data", chunk => {
      raw += chunk;
      if (raw.length > 1024 * 64) {
        req.destroy();
        reject(new Error("request body too large"));
      }
    });
    req.on("end", () => {
      if (!raw) {
        resolveBody({});
        return;
      }
      try {
        resolveBody(JSON.parse(raw));
      } catch {
        reject(new Error("invalid JSON body"));
      }
    });
    req.on("error", reject);
  });
}

function readRawBody(req, { maxBytes = 1024 * 1024 * 4, signal } = {}) {
  return new Promise((resolveBody, reject) => {
    const chunks = [];
    let size = 0;
    let settled = false;
    const cleanup = () => {
      req.removeListener("data", onData);
      req.removeListener("end", onEnd);
      req.removeListener("error", onError);
      signal?.removeEventListener("abort", onAbort);
    };
    const settle = (callback, value) => {
      if (settled) return;
      settled = true;
      cleanup();
      callback(value);
    };
    const onData = chunk => {
      size += chunk.length;
      if (size > maxBytes) {
        req.destroy();
        settle(reject, new Error("request body too large"));
        return;
      }
      chunks.push(chunk);
    };
    const onEnd = () => settle(resolveBody, Buffer.concat(chunks));
    const onError = error => settle(reject, error);
    const onAbort = () => {
      const error = new Error("request body read aborted");
      error.name = "AbortError";
      settle(reject, error);
      // Stop retaining body chunks after the handler has timed out. Draining the
      // remaining bytes lets Node send the typed timeout response before closing
      // or reusing the connection.
      req.resume();
    };
    if (signal?.aborted) {
      onAbort();
      return;
    }
    req.on("data", onData);
    req.on("end", onEnd);
    req.on("error", onError);
    signal?.addEventListener("abort", onAbort, { once: true });
  });
}

function responseContentLength(response) {
  const raw = response?.headers?.get?.("content-length");
  if (!raw || !/^(0|[1-9][0-9]*)$/.test(raw.trim())) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) ? value : null;
}

function upstreamResponseError(code, message) {
  const error = new Error(message);
  error.code = code;
  return error;
}

async function readBoundedResponseText(response, maxBytes) {
  const declaredLength = responseContentLength(response);
  if (declaredLength !== null && declaredLength > maxBytes) {
    throw upstreamResponseError(
      "UPSTREAM_RESPONSE_TOO_LARGE",
      `upstream response exceeds ${maxBytes} byte limit`,
    );
  }
  if (!response?.body || typeof response.body.getReader !== "function") {
    const text = await response.text();
    if (Buffer.byteLength(text, "utf8") > maxBytes) {
      throw upstreamResponseError(
        "UPSTREAM_RESPONSE_TOO_LARGE",
        `upstream response exceeds ${maxBytes} byte limit`,
      );
    }
    return text;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = value instanceof Uint8Array ? value : new Uint8Array(value);
      total += chunk.byteLength;
      if (total > maxBytes) {
        try {
          await reader.cancel();
        } catch {
          // The oversized response is already rejected; cancellation is best effort.
        }
        throw upstreamResponseError(
          "UPSTREAM_RESPONSE_TOO_LARGE",
          `upstream response exceeds ${maxBytes} byte limit`,
        );
      }
      chunks.push(chunk);
    }
  } finally {
    reader.releaseLock();
  }

  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(bytes);
}

function linkAbortSignal(signal, controller) {
  if (!signal) return () => {};
  if (signal.aborted) {
    controller.abort();
    return () => {};
  }
  const abort = () => controller.abort();
  signal.addEventListener("abort", abort, { once: true });
  return () => signal.removeEventListener("abort", abort);
}

function responseContentType(response) {
  return String(response?.headers?.get?.("content-type") || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
}

function assertDirectUpstreamResponse(response, target, label) {
  if (response?.redirected === true) {
    throw upstreamResponseError("UPSTREAM_REDIRECTED", `${label} must not redirect`);
  }
  const finalUrl = String(response?.url || "");
  if (!finalUrl || new URL(finalUrl).href !== new URL(target).href) {
    throw upstreamResponseError(
      "UPSTREAM_FINAL_URL_MISMATCH",
      `${label} must be served directly from its configured endpoint`,
    );
  }
}

function assertJsonUpstreamResponse(response, label) {
  if (responseContentType(response) !== "application/json") {
    throw upstreamResponseError(
      "UPSTREAM_CONTENT_TYPE",
      `${label} must return Content-Type: application/json`,
    );
  }
}

function assertOnlyJsonFields(value, label, allowedFields) {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw upstreamResponseError("UPSTREAM_SCHEMA", `${label} must be a JSON object`);
  }
  const allowed = new Set(allowedFields);
  const unknown = Object.keys(value).find(key => !allowed.has(key));
  if (unknown) {
    throw upstreamResponseError(
      "UPSTREAM_SCHEMA",
      `${label} contains unsupported field ${unknown}`,
    );
  }
  return value;
}

function assertHexString(value, label, { bytes } = {}) {
  const text = String(value ?? "");
  if (!text || text.length % 2 !== 0 || !/^[0-9a-fA-F]+$/.test(text)) {
    throw upstreamResponseError("UPSTREAM_SCHEMA", `${label} must be non-empty hexadecimal bytes`);
  }
  if (bytes !== undefined && text.length !== bytes * 2) {
    throw upstreamResponseError("UPSTREAM_SCHEMA", `${label} must be exactly ${bytes} bytes`);
  }
  return text;
}

function validateProverSuccessPayload(pathname, value) {
  if (pathname === "/v1/prover/deposit") {
    const response = assertOnlyJsonFields(
      value,
      "deposit proof response",
      ["version", "proof_hex", "note_commitment_hex"],
    );
    if (response.version !== "v1") {
      throw upstreamResponseError("UPSTREAM_SCHEMA", "deposit proof response.version must be v1");
    }
    assertHexString(response.proof_hex, "deposit proof response.proof_hex");
    assertHexString(
      response.note_commitment_hex,
      "deposit proof response.note_commitment_hex",
      { bytes: 32 },
    );
    return;
  }

  if (["/v1/prover/transfer", "/v1/prover/withdraw"].includes(pathname)) {
    const response = assertOnlyJsonFields(value, "prover response", ["version", "proof"]);
    if (response.version !== "v2") {
      throw upstreamResponseError("UPSTREAM_SCHEMA", "prover response.version must be v2");
    }
    const proof = assertOnlyJsonFields(
      response.proof,
      "prover response.proof",
      ["version", "payload_hash", "proof_hex"],
    );
    if (proof.version !== "v2") {
      throw upstreamResponseError("UPSTREAM_SCHEMA", "prover response.proof.version must be v2");
    }
    assertHexString(proof.payload_hash, "prover response.proof.payload_hash", { bytes: 32 });
    assertHexString(proof.proof_hex, "prover response.proof.proof_hex");
    return;
  }

  const response = assertOnlyJsonFields(value, "batch prover response", ["version", "proof"]);
  if (response.version !== "v1") {
    throw upstreamResponseError("UPSTREAM_SCHEMA", "batch prover response.version must be v1");
  }
  const proof = assertOnlyJsonFields(
    response.proof,
    "batch prover response.proof",
    ["version", "request_payload_hash", "proof", "circuit_set_id", "artifact_checksum"],
  );
  if (proof.version !== "batch-transfer-proof-v1") {
    throw upstreamResponseError(
      "UPSTREAM_SCHEMA",
      "batch prover response.proof.version must be batch-transfer-proof-v1",
    );
  }
  assertHexString(
    proof.request_payload_hash,
    "batch prover response.proof.request_payload_hash",
    { bytes: 32 },
  );
  if (typeof proof.proof !== "string" || !proof.proof.trim()) {
    throw upstreamResponseError("UPSTREAM_SCHEMA", "batch prover response.proof.proof is required");
  }
  for (const field of ["circuit_set_id", "artifact_checksum"]) {
    if (proof[field] !== undefined && (typeof proof[field] !== "string" || !proof[field].trim())) {
      throw upstreamResponseError("UPSTREAM_SCHEMA", `batch prover response.proof.${field} is invalid`);
    }
  }
}

const proverProxyRateWindows = new Map();
let proverProxyInFlight = 0;

function proverProxyClientKey(req) {
  return String(req.socket?.remoteAddress || "unknown").toLowerCase();
}

function proverProxyAccessAllowed(req) {
  return isLoopbackRemoteAddress(req.socket?.remoteAddress) || config.allowLanProver;
}

function acquireProverProxyCapacity(req) {
  const now = Date.now();
  if (proverProxyRateWindows.size > 1024) {
    for (const [key, value] of proverProxyRateWindows) {
      if (value.resetAt <= now) proverProxyRateWindows.delete(key);
    }
  }
  const key = proverProxyClientKey(req);
  let window = proverProxyRateWindows.get(key);
  if (!window || window.resetAt <= now) {
    window = { count: 0, resetAt: now + config.proverProxyRateLimitWindowMs };
    proverProxyRateWindows.set(key, window);
  }
  window.count += 1;
  if (window.count > config.proverProxyRateLimitMax) {
    return {
      acquired: false,
      code: "rate_limited",
      message: "local prover proxy request rate exceeded",
      retryAfterMs: Math.max(1, window.resetAt - now)
    };
  }
  if (proverProxyInFlight >= config.proverProxyMaxInFlight) {
    return {
      acquired: false,
      code: "capacity_exceeded",
      message: "local prover proxy concurrency exceeded",
      retryAfterMs: 1000
    };
  }
  proverProxyInFlight += 1;
  let released = false;
  return {
    acquired: true,
    release() {
      if (released) return;
      released = true;
      proverProxyInFlight = Math.max(0, proverProxyInFlight - 1);
    }
  };
}

function proverProxyTarget(pathname) {
  if (!config.proverProxyEnabled) return "";
  if (pathname === "/v1/prover/deposit" && config.depositProofUrl) {
    return config.depositProofUrl;
  }
  if (["/v1/prover/transfer", "/v1/prover/withdraw", "/v1/proofs/batch-transfer"].includes(pathname)) {
    const base = new URL(config.proverUrl);
    if (!base.pathname.endsWith("/")) base.pathname += "/";
    return new URL(pathname.replace(/^\/+/, ""), base).toString();
  }
  return "";
}

function proverProxyUpstreamFailure(status) {
  if (status === 400 || status === 422) {
    return { status, code: "invalid_request", message: "prover rejected the request", retryable: false };
  }
  if (status === 401 || status === 403) {
    return { status, code: "unauthorized", message: "prover authorization failed", retryable: false };
  }
  if (status === 404) {
    return { status, code: "not_found", message: "prover endpoint was not found", retryable: false };
  }
  if (status === 405) {
    return { status, code: "method_not_allowed", message: "prover rejected the request method", retryable: false };
  }
  if (status === 429) {
    return { status, code: "busy", message: "prover is busy", retryable: true };
  }
  if ([502, 503, 504].includes(status)) {
    return { status, code: "unavailable", message: "prover is unavailable", retryable: false };
  }
  return {
    status: status >= 400 && status <= 599 ? status : 502,
    code: "proof_failed",
    message: "prover failed to produce a proof",
    retryable: false,
  };
}

function sendProverProxyError(res, failure) {
  sendJson(res, failure.status, {
    version: "v1",
    code: failure.code,
    message: failure.message,
    retryable: failure.retryable,
  });
}

async function handleProverProxy(req, res, url) {
  const target = proverProxyTarget(url.pathname);
  if (!target) {
    sendJson(res, 404, { error: "not found" });
    return;
  }
  if (!proverProxyAccessAllowed(req)) {
    sendJson(res, 403, {
      version: "v1",
      code: "forbidden",
      message: "LAN access to the local prover proxy is disabled"
    });
    return;
  }
  if (req.method === "OPTIONS") {
    res.writeHead(204, {
      allow: "POST, OPTIONS",
      "cache-control": "no-store"
    });
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
  const contentType = String(req.headers["content-type"] || "").split(";", 1)[0].trim().toLowerCase();
  if (contentType !== "application/json") {
    sendJson(res, 415, {
      version: "v1",
      code: "unsupported_media_type",
      message: "prover proxy requires application/json"
    });
    return;
  }

  const capacity = acquireProverProxyCapacity(req);
  if (!capacity.acquired) {
    sendJson(res, 429, {
      version: "v1",
      code: capacity.code,
      message: capacity.message,
      retry_after_ms: capacity.retryAfterMs
    });
    return;
  }

  const controller = new AbortController();
  let timedOut = false;
  let clientDisconnected = false;
  const abortForClientDisconnect = () => {
    clientDisconnected = true;
    controller.abort();
  };
  req.once("aborted", abortForClientDisconnect);
  res.once("close", abortForClientDisconnect);
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, config.proverTimeoutMs);
  try {
    const body = await readRawBody(req, { signal: controller.signal });
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
      body,
      redirect: "error",
      signal: controller.signal
    });
    assertDirectUpstreamResponse(response, target, "prover response");
    if (!response.ok) {
      try {
        await response.body?.cancel();
      } catch {
        // The proxy returns its own stable error and never forwards upstream details.
      }
      sendProverProxyError(res, proverProxyUpstreamFailure(response.status));
      return;
    }
    assertJsonUpstreamResponse(response, "prover response");
    const text = await readBoundedResponseText(
      response,
      config.proverProxyMaxResponseBytes,
    );
    let parsed;
    try {
      parsed = JSON.parse(text);
    } catch {
      throw upstreamResponseError("UPSTREAM_SCHEMA", "prover response must be JSON");
    }
    validateProverSuccessPayload(url.pathname, parsed);
    res.writeHead(response.status, {
      "content-type": "application/json; charset=utf-8",
      "cache-control": "no-store"
    });
    res.end(text);
  } catch (error) {
    if (clientDisconnected || res.destroyed) return;
    sendProverProxyError(res, timedOut && error?.name === "AbortError"
      ? {
          status: 504,
          code: "unavailable",
          message: "prover request timed out",
          retryable: false,
        }
      : {
          status: 502,
          code: "proof_failed",
          message: "prover returned an invalid response",
          retryable: false,
        });
  } finally {
    clearTimeout(timeout);
    req.removeListener("aborted", abortForClientDisconnect);
    res.removeListener("close", abortForClientDisconnect);
    capacity.release();
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

async function localAccounts({ signal } = {}) {
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
      ], { signal });
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
      timeout: timeoutMs,
      ...(options.signal ? { signal: options.signal } : {}),
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

async function fetchJson(url, {
  signal,
  timeoutMs = config.upstreamTimeoutMs,
  maxResponseBytes = config.upstreamMaxResponseBytes,
} = {}) {
  const controller = new AbortController();
  let timedOut = false;
  const unlinkAbortSignal = linkAbortSignal(signal, controller);
  const timeout = setTimeout(() => {
    timedOut = true;
    controller.abort();
  }, timeoutMs);
  try {
    const response = await fetch(url, {
      headers: { accept: "application/json" },
      redirect: "error",
      signal: controller.signal,
    });
    assertDirectUpstreamResponse(response, url, "upstream JSON response");
    if (!response.ok) {
      try {
        await response.body?.cancel();
      } catch {
        // The stable status-only error below deliberately omits upstream content.
      }
      throw upstreamResponseError(
        "UPSTREAM_HTTP_ERROR",
        `upstream returned HTTP ${response.status}`,
      );
    }
    assertJsonUpstreamResponse(response, "upstream JSON response");
    const text = await readBoundedResponseText(response, maxResponseBytes);
    try {
      return JSON.parse(text);
    } catch {
      throw upstreamResponseError("UPSTREAM_SCHEMA", "upstream response must be JSON");
    }
  } catch (error) {
    if (error?.name === "AbortError") {
      throw upstreamResponseError(
        timedOut ? "UPSTREAM_TIMEOUT" : "UPSTREAM_CANCELLED",
        timedOut ? `upstream request timed out after ${timeoutMs}ms` : "upstream request was cancelled",
      );
    }
    if (error?.code) throw error;
    throw upstreamResponseError("UPSTREAM_UNAVAILABLE", "upstream request failed");
  } finally {
    clearTimeout(timeout);
    unlinkAbortSignal();
  }
}

let healthRequestsInFlight = 0;

function acquireHealthRequestCapacity() {
  if (healthRequestsInFlight >= config.healthMaxInFlight) return null;
  healthRequestsInFlight += 1;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    healthRequestsInFlight = Math.max(0, healthRequestsInFlight - 1);
  };
}

async function waitForTx(txhash) {
  return clairveil.waitForTx(txhash);
}

function localSignerSubmissionEvidence(txHash) {
  return Object.freeze({
    transport: config.transport,
    chainId: config.chainId,
    evmChainId: isEvmTransport() ? config.evmChainId : "",
    txHash: validateTxHashHex(txHash),
  });
}

function recordReturnedCosmosSubmissionEvidence(tx, ...recorders) {
  let txHash;
  try {
    txHash = validateTxHashHex(tx?.txhash ?? tx?.txHash);
  } catch (error) {
    // An explicit non-zero CheckTx code proves rejection even when the node did
    // not return a usable hash. Preserve that safe-release path; every other
    // post-boundary malformed response remains fenced without an identifier.
    const code = confirmedCosmosTxCode(tx);
    if (code != null && code !== 0) return null;
    throw error;
  }
  const evidence = localSignerSubmissionEvidence(txHash);
  for (const record of recorders) record(evidence);
  return { txHash, evidence };
}

async function reconcileLocalSignerSubmission(evidence) {
  if (
    !evidence
    || evidence.transport !== config.transport
    || evidence.chainId !== config.chainId
    || String(evidence.evmChainId || "") !== (isEvmTransport() ? config.evmChainId : "")
  ) {
    return false;
  }
  const txHash = validateTxHashHex(evidence.txHash);
  if (isEvmTransport()) {
    const receipt = await evmJsonRpc("eth_getTransactionReceipt", [
      `0x${txHash.toLowerCase()}`,
    ]);
    return receipt ? { resolved: true, receipt } : false;
  }
  const tx = await clairveil.getTx(txHash);
  return tx ? { resolved: true, tx } : false;
}

async function reconcileRelaySubmissionAttempt(attempt) {
  const evidence = attempt?.evidence;
  const authoritative = await reconcileLocalSignerSubmission(evidence);
  if (!authoritative?.resolved) {
    return { included: false, failed: false };
  }
  const previous = attempt?.result && typeof attempt.result === "object"
    ? attempt.result
    : {};
  const txHash = validateTxHashHex(evidence.txHash);

  if (isEvmTransport()) {
    const receipt = authoritative.receipt;
    const failed = hasFailedEvmReceiptStatus(receipt);
    if (!failed && !hasSuccessfulEvmReceiptStatus(receipt)) {
      return { included: false, failed: false };
    }
    return {
      included: true,
      failed,
      result: {
        ...previous,
        broadcast: { ...(previous.broadcast || {}), txhash: txHash },
        receipt,
        included: true,
        pending: false,
        failed,
      },
    };
  }

  const tx = authoritative.tx;
  const txCode = confirmedCosmosTxCode(tx);
  if (txCode == null) return { included: false, failed: false };
  const failed = txCode > 0;
  return {
    included: true,
    failed,
    result: {
      ...previous,
      broadcast: { ...(previous.broadcast || {}), txhash: txHash },
      tx,
      included: true,
      pending: false,
      failed,
    },
  };
}

function runLocalSignerSubmission(signer, submit) {
  return relayAccountSubmissionSerializer.run(relaySubmissionAccountKey(signer), {
    reconcileUnknown: reconcileLocalSignerSubmission,
    submit,
  });
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

async function latestChainNowUnix() {
  const block = await fetchJson(rpcHttpUrl("/block"));
  const timestamp = block?.result?.block?.header?.time;
  const milliseconds = Date.parse(String(timestamp || ""));
  if (!Number.isFinite(milliseconds)) {
    throw new Error("latest chain block omitted a valid timestamp");
  }
  return Math.floor(milliseconds / 1000);
}

function assertEvmRelayCandidateMatches(candidate, expected) {
  if (!candidate || typeof candidate !== "object") {
    throw new Error("EVM relay withdraw requires the prepared candidate transaction");
  }
  const actualTo = String(candidate.to || "").toLowerCase();
  const expectedTo = String(expected?.to || "").toLowerCase();
  const actualData = String(candidate.data || "").toLowerCase();
  const expectedData = String(expected?.data || "").toLowerCase();
  const actualValue = BigInt(candidate.value ?? 0);
  const expectedValue = BigInt(expected?.value ?? 0);
  const actualChainId = normalizeEvmChainId(candidate.chainId ?? candidate.chain_id ?? config.evmChainId);
  if (actualTo !== expectedTo
    || actualData !== expectedData
    || actualValue !== expectedValue
    || actualChainId !== config.evmChainId) {
    throw new Error("EVM relay candidate does not match the payload-derived transaction");
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
    depositProof: Boolean(dappChainProfiles()[0]?.depositProofUrl),
    relayer: localSignerMutation,
    auditorAdmin: localSignerAdmin,
    proverProxy: config.proverProxyEnabled,
    // Product exposure remains opt-in after capability/conformance checks.
    // The same prepared effect is submitted as one Cosmos MsgBatchTransfer or
    // one EVM singleProofBatchTransfer call, depending on the active profile.
    batchTransfer: config.enableBatchTransfer
  };
}

function publicConfig(req) {
  const serverFeatures = serverFeaturesForRequest(req);
  const exposeLocalAdmin = serverFeatures.localSignerAdmin;
  const chainProfiles = dappChainProfiles();
  const activeId = activeChainProfileId();
  const activeProfile = chainProfiles.find(profile => profile.id === activeId) || chainProfiles[0];
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
    accountPrefix: activeProfile.accountPrefix,
    shieldedPrefix: activeProfile.shieldedPrefix,
    localTestMode: config.localTestMode,
    serverFeatures,
    activeChainProfileId: activeId,
    chainProfiles,
    ...(activeProfile.transport === "cosmos" ? {
      keplrChainInfo: activeProfile.keplrChainInfo
    } : {
      evmRpc: activeProfile.evmRpc,
      evmChainId: activeProfile.evmChainId,
      evmChainName: activeProfile.evmChainName,
      evmPrivacyPrecompileAddress: activeProfile.evmPrivacyPrecompileAddress,
      evmDepositMode: activeProfile.evmDepositMode,
      evmNativeDenom: activeProfile.evmNativeDenom,
      ...(activeProfile.evmAuthorizationProfile ? { evmAuthorizationProfile: activeProfile.evmAuthorizationProfile } : {}),
      evmGasLimit: activeProfile.evmGasLimit,
      evmSendGasLimit: activeProfile.evmSendGasLimit
    })
  };
}

async function evmJsonRpc(method, params = []) {
  const response = await fetch(config.evmRpc, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      jsonrpc: "2.0",
      id: Date.now(),
      method,
      params
    })
  });
  const data = await response.json();
  if (data.error) {
    throw new Error(data.error.message || `EVM RPC ${method} failed`);
  }
  return data.result;
}

async function sendEvmFaucet({
  from,
  recipient,
  amount,
  markSubmissionStarted,
  markSubmissionOutcomeUnknown,
  recordSubmissionEvidence,
}) {
  const recipientEvm = validateEvmAddress(recipient);
  const provider = new JsonRpcProvider(config.evmRpc);
  const wallet = evmWalletForLocalSigner(from).connect(provider);
  markSubmissionStarted();
  const tx = await wallet.sendTransaction({
    to: recipientEvm,
    value: amount.amount.toString(),
    gasLimit: 21000
  });
  const txHash = validateTxHashHex(tx.hash);
  const evidence = localSignerSubmissionEvidence(txHash);
  recordSubmissionEvidence(evidence);
  const receipt = await waitForEvmReceipt(txHash);
  const outcome = trackedEvmSubmissionOutcome(
    receipt,
    evidence,
    markSubmissionOutcomeUnknown,
  );
  return {
    txHash,
    receipt,
    recipientEvm,
    outcome,
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
  try {
    if (req.method === "GET" && url.pathname === "/api/config") {
      const cfg = publicConfig(req);
      sendJson(res, 200, cfg);
      return;
    }

    if (req.method === "GET" && url.pathname === "/api/health") {
      const releaseCapacity = acquireHealthRequestCapacity();
      if (!releaseCapacity) {
        sendJson(res, 503, {
          error: "health request capacity is exhausted",
          code: "capacity_exceeded",
          retry_after_ms: 1000,
        });
        return;
      }
      const controller = new AbortController();
      let clientDisconnected = false;
      const abortForClientDisconnect = () => {
        clientDisconnected = true;
        controller.abort();
      };
      req.once("aborted", abortForClientDisconnect);
      res.once("close", abortForClientDisconnect);
      try {
        const cfg = publicConfig(req);
        const [status, tree, audit, accounts] = await Promise.allSettled([
          fetchJson(rpcHttpUrl("/status"), { signal: controller.signal }),
          fetchJson(restUrl("/clairveil/privacy/v1/tree_state"), { signal: controller.signal }),
          fetchJson(restUrl("/clairveil/privacy/v1/audit_config"), { signal: controller.signal }),
          cfg.serverFeatures.localSignerAdmin
            ? localAccounts({ signal: controller.signal })
            : [],
        ]);
        if (clientDisconnected || res.destroyed) return;
        sendJson(res, 200, {
          config: cfg,
          status: status.status === "fulfilled" ? status.value.result : null,
          tree: tree.status === "fulfilled" ? tree.value : null,
          audit: audit.status === "fulfilled" ? audit.value : null,
          accounts: accounts.status === "fulfilled" ? accounts.value : [],
          errors: [status, tree, audit, accounts]
            .filter(result => result.status === "rejected")
            .map(result => result.reason.message),
        });
      } finally {
        req.removeListener("aborted", abortForClientDisconnect);
        res.removeListener("close", abortForClientDisconnect);
        releaseCapacity();
      }
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
      const audit = await fetchJson(restUrl("/clairveil/privacy/v1/audit_config"));
      const auditMasterPubKeyHex = audit.audit_master_pubkey_hex || "";
      const material = testAuditMaterialFromConfig(auditMasterPubKeyHex) ?? await runAuditorMaterial({
        home: localSignerHome(),
        key_name: "auditor",
        keyring_backend: localSignerKeyring(),
        account_prefix: config.accountPrefix
      });
      sendJson(res, 200, {
        ...material,
        audit_master_pubkey_hex: auditMasterPubKeyHex,
        matches_audit_config: Boolean(
          auditMasterPubKeyHex &&
          material.disclosure_pubkey_hex &&
          auditMasterPubKeyHex.toLowerCase() === material.disclosure_pubkey_hex.toLowerCase()
        )
      });
      return;
    }

    // Test/admin-only route. Public DApps must not receive or relay audit disclosure private scalars.
    if (req.method === "POST" && url.pathname === "/api/auditor/decode") {
      assertLocalTestBackendAllowed("auditor disclosure decode");
      assertLocalAdminAccessAllowed(req);
      const body = await readBody(req);
      const txHash = validateTxHashHex(body.txHash ?? body.tx_hash);
      const disclosurePrivKeyHex = body.disclosurePrivKeyHex ??
        body.disclosure_privkey_hex;
      if (!disclosurePrivKeyHex) {
        throw new Error("disclosurePrivKeyHex is required for auditor JS decode");
      }
      sendJson(res, 200, await clairveil.decodeAuditDisclosure({
        txHash,
        disclosurePrivKeyHex
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
        const faucet = await runLocalSignerSubmission(
          from,
          async (
            markSubmissionStarted,
            _markSubmissionRejected,
            markSubmissionOutcomeUnknown,
            recordSubmissionEvidence,
          ) => sendEvmFaucet({
            from,
            recipient,
            amount: parseCoin(amount.funded),
            markSubmissionStarted,
            markSubmissionOutcomeUnknown,
            recordSubmissionEvidence,
          }),
        );
        if (faucet.outcome.failed === true) {
          throw new Error(`faucet tx failed with EVM receipt status ${faucet.receipt.status}`);
        }
        const balance = await queryEvmNativeBalance(recipient);
        sendJson(res, 200, {
          broadcast: { txhash: faucet.txHash },
          receipt: faucet.receipt,
          ...faucet.outcome,
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
      const { result, tx, txHash, outcome } = await runLocalSignerSubmission(
        from,
        async (
          markSubmissionStarted,
          markSubmissionRejected,
          markSubmissionOutcomeUnknown,
          recordSubmissionEvidence,
        ) => {
          markSubmissionStarted();
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
          const tracked = recordReturnedCosmosSubmissionEvidence(
            result.json,
            recordSubmissionEvidence,
          );
          assertCosmosCheckTxAccepted(result.json, { markSubmissionRejected });
          const { txHash, evidence } = tracked;
          const tx = await waitForTrackedSubmissionOutcome({
            waitForOutcome: () => waitForTx(txHash),
            markSubmissionOutcomeUnknown,
            evidence,
          });
          const outcome = trackedCosmosSubmissionOutcome(
            tx,
            evidence,
            markSubmissionOutcomeUnknown,
          );
          return { result, tx, txHash, outcome };
        },
      );
      if (outcome.failed === true) {
        throw new Error(tx.raw_log || `faucet tx failed with code ${confirmedCosmosTxCode(tx)}`);
      }
      const balance = await queryBalances(recipient);
      sendJson(res, 200, {
        broadcast: { ...result.json, txhash: txHash },
        tx,
        ...outcome,
        balance,
        beforeBalance,
        amount,
        from,
        recipient,
      });
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/relayer/withdraw/reconcile") {
      assertLocalTestBackendAllowed("relay withdraw reconciliation");
      assertSignerMutationAllowed(req);
      const body = await readBody(req);
      const recovered = await relayWithdrawSubmissionGate.reconcileByPayloadHash(
        body.payloadHash ?? body.payload_hash,
        { reconcile: reconcileRelaySubmissionAttempt }
      );
      if (recovered.found && recovered.settled) {
        try {
          await relayAccountSubmissionSerializer.reconcileUnknownOutcome(
            relaySubmissionAccountKey(localRelayerName()),
            reconcileLocalSignerSubmission
          );
        } catch (error) {
          // The original request may still be completing the serializer's
          // finally block. The read-only payload result remains safe to return.
          if (!/must wait for queued submissions/.test(error?.message || "")) throw error;
        }
      }
      const recoveredTxHash = String(
        recovered.evidence?.txHash
          || recovered.result?.broadcast?.txhash
          || recovered.result?.broadcast?.txHash
          || ""
      ).trim();
      sendJson(res, 200, {
        found: recovered.found,
        settled: recovered.settled,
        resolved: recovered.resolved,
        released: recovered.released,
        evidence: recovered.evidence,
        result: /^(0x)?[0-9a-fA-F]{64}$/.test(recoveredTxHash) ? {
          broadcast: { txhash: recoveredTxHash },
          included: recovered.result?.included === true,
          pending: recovered.result?.pending === true,
          unknown: recovered.result?.unknown === true,
          failed: recovered.result?.failed === true
        } : null
      });
      return;
    }

    if (req.method === "POST" && url.pathname === "/api/relayer/withdraw") {
      assertLocalTestBackendAllowed("relay withdraw");
      assertSignerMutationAllowed(req);
      const body = await readBody(req);
      const payload = body.handoff?.payload ?? body.payload;
      const candidateTransaction = body.handoff?.transaction ?? body.transaction;
      const relayer = localRelayerName();
      const requestedRelayer = body.relayer ?? body.from;
      if (requestedRelayer !== undefined && validateAccount(requestedRelayer) !== relayer) {
        throw new Error(`local relay withdraw must use the configured ${relayer} fee payer`);
      }
      if (!payload || typeof payload !== "object") {
        throw new Error("relay withdraw payload is required");
      }
      const expectedRecipient = body.expectedRecipient
        ?? body.expected_recipient
        ?? payload.recipient;
      const response = await runLocalSignerSubmission(
        relayer,
        async (
          markAccountSubmissionStarted,
          markAccountSubmissionRejected,
          markAccountSubmissionOutcomeUnknown,
          recordAccountSubmissionEvidence,
        ) => relayWithdrawSubmissionGate.run(payload, {
          checkNullifiers: nullifiers => clairveil.checkNullifiers(nullifiers),
          reconcile: reconcileRelaySubmissionAttempt,
          submit: async (
            markSubmissionStarted,
            markSubmissionRejected,
            markIncludedExecutionFailed,
            recordSubmissionEvidence,
          ) => {
            const markExternalSubmissionStarted = () => {
              markSubmissionStarted();
              markAccountSubmissionStarted();
            };
            const markExternalSubmissionRejected = () => {
              markSubmissionRejected();
              markAccountSubmissionRejected();
            };
            const account = (await localAccounts()).find(entry => entry.name === relayer);
            if (!account?.transparentAddress) {
              throw new Error(`local relayer account ${relayer} is not initialized`);
            }
            const chainNowUnix = await latestChainNowUnix();

            if (isEvmTransport()) {
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
                expectedRecipient
              });
              assertEvmRelayCandidateMatches(candidateTransaction, built.transaction);
              const provider = new JsonRpcProvider(config.evmRpc);
              const wallet = evmWalletForLocalSigner(relayer).connect(provider);
              markExternalSubmissionStarted();
              const submitted = await wallet.sendTransaction({
                to: built.transaction.to,
                data: built.transaction.data,
                value: built.transaction.value ?? "0x0",
                gasLimit: BigInt(config.evmGasLimit)
              });
              const txHash = validateTxHashHex(submitted.hash);
              const evidence = localSignerSubmissionEvidence(txHash);
              recordAccountSubmissionEvidence(evidence);
              recordSubmissionEvidence(evidence);
              const receipt = await waitForTrackedSubmissionOutcome({
                waitForOutcome: () => waitForEvmReceipt(txHash),
                markSubmissionOutcomeUnknown: markAccountSubmissionOutcomeUnknown,
                evidence,
              });
              const outcome = trackedEvmSubmissionOutcome(
                receipt,
                evidence,
                markAccountSubmissionOutcomeUnknown,
              );
              if (outcome.failed === true) markIncludedExecutionFailed();
              return {
                broadcast: { txhash: txHash },
                receipt,
                ...outcome,
                relayer,
                relayerAddress: account.transparentAddress,
                relayerEvmAddress: wallet.address,
                payloadHash: payload.payload_hash || ""
              };
            }

            clairveil.buildRelayWithdrawMessageFromPayload({
              payload,
              relayer: account.transparentAddress,
              chainNowUnix,
              expectedChainId: config.chainId,
              expectedRecipient,
              accountPrefix: config.accountPrefix
            });
            const workDir = await mkdtemp(join(tmpdir(), "clairveil-relay-withdraw-"));
            const payloadPath = join(workDir, "payload.json");
            try {
              await writeFile(payloadPath, JSON.stringify(payload, jsonReplacer, 2), "utf8");
              markExternalSubmissionStarted();
              const result = await runClairveild([
                "tx", "privacy", "relay-withdraw", payloadPath,
                "--from", relayer,
                "--keyring-backend", localSignerKeyring(),
                "--home", localSignerHome(),
                "--node", config.rpc,
                "--chain-id", config.chainId,
                "--gas", "5000000",
                "--gas-prices", config.gasPrices,
                "--yes",
                "--output", "json"
              ]);
              const tracked = recordReturnedCosmosSubmissionEvidence(
                result.json,
                recordAccountSubmissionEvidence,
                recordSubmissionEvidence,
              );
              assertCosmosCheckTxAccepted(result.json, {
                markSubmissionRejected: markExternalSubmissionRejected,
              });
              const { txHash, evidence } = tracked;
              const tx = await waitForTrackedSubmissionOutcome({
                waitForOutcome: () => waitForTx(txHash),
                markSubmissionOutcomeUnknown: markAccountSubmissionOutcomeUnknown,
                evidence,
              });
              const outcome = trackedCosmosSubmissionOutcome(
                tx,
                evidence,
                markAccountSubmissionOutcomeUnknown,
              );
              if (outcome.failed === true) markIncludedExecutionFailed();
              return {
                broadcast: { ...result.json, txhash: txHash },
                tx,
                ...outcome,
                relayer,
                relayerAddress: account.transparentAddress,
                payloadHash: payload.payload_hash || ""
              };
            } finally {
              await rm(workDir, { recursive: true, force: true });
            }
          },
        }),
      );
      sendJson(res, 200, response);
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
      const { result, tx, txHash, outcome } = await runLocalSignerSubmission(
        from,
        async (
          markSubmissionStarted,
          markSubmissionRejected,
          markSubmissionOutcomeUnknown,
          recordSubmissionEvidence,
        ) => {
          markSubmissionStarted();
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
          const tracked = recordReturnedCosmosSubmissionEvidence(
            result.json,
            recordSubmissionEvidence,
          );
          assertCosmosCheckTxAccepted(result.json, { markSubmissionRejected });
          const { txHash, evidence } = tracked;
          const tx = await waitForTrackedSubmissionOutcome({
            waitForOutcome: () => waitForTx(txHash),
            markSubmissionOutcomeUnknown,
            evidence,
          });
          const outcome = trackedCosmosSubmissionOutcome(
            tx,
            evidence,
            markSubmissionOutcomeUnknown,
          );
          return { result, tx, txHash, outcome };
        },
      );
      sendJson(res, 200, {
        broadcast: { ...result.json, txhash: txHash },
        tx,
        ...outcome,
      });
      return;
    }

    sendJson(res, 404, { error: "not found" });
  } catch (error) {
    sendJson(res, error?.statusCode || 400, errorPayload(error));
  }
}

function configuredBrowserHostname(req) {
  let hostname;
  try {
    hostname = new URL(`http://${String(req?.headers?.host || "")}`).hostname.toLowerCase();
  } catch {
    return "";
  }
  const configuredHost = String(config.host || "").trim().toLowerCase();
  if (["localhost", "127.0.0.1", "[::1]"].includes(hostname)) return hostname;
  if (!isWildcardHost(configuredHost) && configuredHost === hostname) return hostname;
  const localAddresses = localNetworkAddresses().map(address => address.toLowerCase());
  return isWildcardHost(configuredHost) && localAddresses.includes(hostname) ? hostname : "";
}

function configuredConnectOrigins(req) {
  let profiles;
  try {
    profiles = dappChainProfiles();
  } catch {
    return [];
  }
  const origins = new Set();
  const browserHostname = config.localTestMode ? configuredBrowserHostname(req) : "";
  for (const profile of profiles) {
    for (const endpoint of [
      profile.rpc,
      profile.rest,
      ...(profile.restEndpoints || []),
      profile.proverUrl,
      profile.depositProofUrl,
      profile.evmRpc,
    ]) {
      if (!endpoint) continue;
      try {
        const url = new URL(endpoint);
        origins.add(url.origin);
        if (browserHostname && ["localhost", "127.0.0.1"].includes(url.hostname)) {
          url.hostname = browserHostname;
          origins.add(url.origin);
        }
      } catch {
        // Local malformed configuration gets a restrictive self-only policy.
      }
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
      "img-src 'self' data:"
    ].join("; ")
  };
}

function serveStatic(req, res, url) {
  const requested = url.pathname === "/" ? "/index.html" : url.pathname;
  const path = resolve(join(publicDir, requested));
  if (path !== publicDir && !path.startsWith(publicDir + "/")) {
    sendJson(res, 403, { error: "forbidden" });
    return;
  }
  const fallbackToIndex = !extname(path);
  const filePath = existsSync(path) ? path : fallbackToIndex ? join(publicDir, "index.html") : "";
  if (!filePath) {
    sendJson(res, 404, { error: "not found" });
    return;
  }
  const contentType = contentTypes.get(extname(filePath)) ?? "application/octet-stream";
  const stream = createReadStream(filePath);
  stream.on("error", () => {
    sendJson(res, 404, { error: "not found" });
  });
  stream.pipe(res.writeHead(200, {
    "content-type": contentType,
    "cache-control": "no-store",
    ...staticSecurityHeaders(req)
  }));
}

const server = createServer((req, res) => {
  res.setHeader("x-content-type-options", "nosniff");
  res.setHeader("referrer-policy", "no-referrer");
  res.setHeader("cross-origin-opener-policy", "same-origin");
  const url = new URL(req.url ?? "/", `http://${req.headers.host ?? "localhost"}`);
  if (proverProxyTarget(url.pathname)) {
    handleProverProxy(req, res, url);
    return;
  }
  if (url.pathname.startsWith("/v1/prover/") || url.pathname === "/v1/proofs/batch-transfer") {
    sendJson(res, 404, { error: "not found" });
    return;
  }
  if (url.pathname.startsWith("/api/")) {
    handleApi(req, res, url);
    return;
  }
  serveStatic(req, res, url);
});

server.listen(config.port, config.host, () => {
  console.log(`Clairveil DApp: ${dappUrls().join(", ")}`);
  console.log(`Clairveil home: ${config.home}`);
  console.log(`RPC: ${config.rpc}`);
  console.log(`REST: ${config.rest}`);
  console.log(`Keplr RPC: ${publicRpcEndpoint()}`);
  console.log(`Keplr REST: ${publicRestEndpoint()}`);
});
