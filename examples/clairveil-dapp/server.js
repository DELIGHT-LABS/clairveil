import { createServer } from "node:http";
import { readFile } from "node:fs/promises";
import { createReadStream, existsSync } from "node:fs";
import { spawn } from "node:child_process";
import { extname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { homedir, networkInterfaces } from "node:os";
import { JsonRpcProvider, Wallet } from "ethers";
import {
  createClairveilClient,
  ClairveilError,
  ClairveilErrorCode,
  derivePrivacyMaterial,
  isEvmAddress,
  evmPrivacyPrecompileAddress,
  plannerStatusToErrorCode
} from "clairveiljs";

const __dirname = fileURLToPath(new URL(".", import.meta.url));
const repoRoot = resolve(__dirname, "../..");
const publicDir = join(__dirname, "public");
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
const configuredDenom = process.env.CLAIRVEIL_DENOM ?? "uclair";

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
  host: cliOptions.host ?? process.env.CLAIRVEIL_DAPP_HOST ?? "0.0.0.0",
  port: Number(cliOptions.port ?? process.env.PORT ?? process.env.CLAIRVEIL_DAPP_PORT ?? 5173),
  home: process.env.CLAIRVEIL_HOME ?? process.env.CLAIRVEIL_DAPP_HOME ?? defaultHome,
  chainId: process.env.CHAIN_ID ?? "clairveil-local-2",
  bin: process.env.CLAIRVEILD_BIN ?? "clairveild",
  rpc: process.env.CLAIRVEIL_RPC ?? "tcp://127.0.0.1:26657",
  rest: process.env.CLAIRVEIL_REST ?? "http://127.0.0.1:1317",
  publicRpc: process.env.CLAIRVEIL_PUBLIC_RPC ?? "",
  publicRest: process.env.CLAIRVEIL_PUBLIC_REST ?? "",
  proverUrl: process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  publicProverUrl: process.env.CLAIRVEIL_PUBLIC_PROVER_URL ?? process.env.CLAIRVEIL_PROVER_PUBLIC_URL ?? process.env.CLAIRVEIL_PROVER_URL ?? "http://127.0.0.1:8080",
  proverBearerToken: process.env.CLAIRVEIL_PROVER_BEARER_TOKEN ?? process.env.CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN ?? "",
  proverTimeoutMs: Number(process.env.CLAIRVEIL_PROVER_TIMEOUT_MS ?? 120000),
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

const cosmosAccountNames = new Set(["alice", "bob", "auditor"]);
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

function keplrChainInfo() {
  return buildKeplrChainInfo({
    chainId: config.chainId,
    chainName: "Clairveil Localnet",
    rpc: publicRpcEndpoint(),
    rest: publicRestEndpoint(),
    coinType: config.keplrCoinType,
    accountPrefix: config.accountPrefix,
    displayDenom: config.displayDenom,
    denom: config.denom,
    coinDecimals: config.coinDecimals,
    gasPriceStep: config.keplrGasPriceStep
  });
}

function dappChainProfiles() {
  const clairveilProfile = {
    id: "clairveil-local",
    label: "Clairveil Localnet",
    chainName: "Clairveil Localnet",
    transport: "cosmos",
    wallet: "keplr",
    chainId: process.env.CLAIRVEIL_COSMOS_CHAIN_ID ?? (isEvmTransport() ? "clairveil-local-2" : config.chainId),
    rpc: httpRpcEndpoint(process.env.CLAIRVEIL_COSMOS_RPC ?? (isEvmTransport() ? "tcp://127.0.0.1:26657" : config.rpc)),
    rest: (process.env.CLAIRVEIL_COSMOS_REST ?? (isEvmTransport() ? "http://127.0.0.1:1317" : config.rest)).replace(/\/$/, ""),
    proverUrl: process.env.CLAIRVEIL_COSMOS_PROVER_URL ?? config.publicProverUrl,
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

  const evmProfile = {
    id: "evm-local",
    label: config.evmChainName,
    chainName: config.evmChainName,
    transport: "evm",
    wallet: "metamask",
    chainId: process.env.CLAIRVEIL_EVM_HOST_CHAIN_ID ?? (isEvmTransport() ? config.chainId : "evm-local-1"),
    rpc: httpRpcEndpoint(process.env.CLAIRVEIL_EVM_HOST_RPC ?? (isEvmTransport() ? config.rpc : "tcp://127.0.0.1:26657")),
    rest: (process.env.CLAIRVEIL_EVM_HOST_REST ?? (isEvmTransport() ? config.rest : "http://127.0.0.1:1317")).replace(/\/$/, ""),
    proverUrl: process.env.CLAIRVEIL_EVM_PROVER_URL ?? config.publicProverUrl,
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
  return {
    error: error?.message || String(error),
    code: error?.clairveilCode || ClairveilErrorCode.INVALID_ARGUMENT
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

function readRawBody(req, { maxBytes = 1024 * 1024 * 4 } = {}) {
  return new Promise((resolveBody, reject) => {
    const chunks = [];
    let size = 0;
    req.on("data", chunk => {
      size += chunk.length;
      if (size > maxBytes) {
        req.destroy();
        reject(new Error("request body too large"));
        return;
      }
      chunks.push(chunk);
    });
    req.on("end", () => resolveBody(Buffer.concat(chunks)));
    req.on("error", reject);
  });
}

function proverProxyPath(pathname) {
  if (pathname === "/v1/prover/transfer" || pathname === "/v1/prover/withdraw") {
    return pathname;
  }
  return "";
}

async function handleProverProxy(req, res, url) {
  const path = proverProxyPath(url.pathname);
  if (!path) {
    sendJson(res, 404, { error: "not found" });
    return;
  }
  if (req.method === "OPTIONS") {
    res.writeHead(204, {
      "access-control-allow-origin": "*",
      "access-control-allow-methods": "POST, OPTIONS",
      "access-control-allow-headers": "content-type, authorization",
      "access-control-max-age": "600"
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

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort(), config.proverTimeoutMs);
  try {
    const body = await readRawBody(req);
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
      body,
      signal: controller.signal
    });
    const text = await response.text();
    res.writeHead(response.status, {
      "content-type": response.headers.get("content-type") || "application/json; charset=utf-8",
      "cache-control": "no-store"
    });
    res.end(text);
  } catch (error) {
    const timedOut = error?.name === "AbortError";
    sendJson(res, timedOut ? 504 : 502, {
      version: "v1",
      code: timedOut ? "unavailable" : "proof_failed",
      message: timedOut ? `prover request timed out after ${config.proverTimeoutMs}ms` : error.message
    });
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

async function fetchJson(url) {
  const response = await fetch(url, { headers: { accept: "application/json" } });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`);
  }
  return response.json();
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
    auditorAdmin: localSignerAdmin
  };
}

function publicConfig(req) {
  const serverFeatures = serverFeaturesForRequest(req);
  const exposeLocalAdmin = serverFeatures.localSignerAdmin;
  return {
    serverBacked: true,
    modeLabel: config.localTestMode ? "Local Note Test Web" : "Public Node DApp",
    home: exposeLocalAdmin ? config.home : "",
    localSignerHome: exposeLocalAdmin ? localSignerHome() : "",
    localSignerBin: exposeLocalAdmin ? localSignerBin() : "",
    chainId: config.chainId,
    rpc: config.rpc,
    rest: config.rest,
    proverUrl: config.publicProverUrl,
    transport: config.transport,
    denom: config.denom,
    displayDenom: config.displayDenom,
    coinDecimals: config.coinDecimals,
    accountPrefix: config.accountPrefix,
    shieldedPrefix: config.shieldedPrefix,
    localTestMode: config.localTestMode,
    serverFeatures,
    activeChainProfileId: activeChainProfileId(),
    chainProfiles: dappChainProfiles(),
    keplrChainInfo: keplrChainInfo(),
    ...(isEvmTransport() ? {
      evmRpc: config.evmRpc,
      evmChainId: config.evmChainId,
      evmChainName: config.evmChainName,
      evmPrivacyPrecompileAddress: config.evmPrivacyPrecompileAddress,
      evmGasLimit: config.evmGasLimit,
      evmSendGasLimit: config.evmSendGasLimit
    } : {})
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

async function sendEvmFaucet({ from, recipient, amount }) {
  const recipientEvm = validateEvmAddress(recipient);
  const provider = new JsonRpcProvider(config.evmRpc);
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
  if (receipt.status && receipt.status !== "0x1") {
    throw new Error(`faucet tx failed with EVM receipt status ${receipt.status}`);
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
  try {
    if (req.method === "GET" && url.pathname === "/api/config") {
      const cfg = publicConfig(req);
      sendJson(res, 200, {
        ...cfg,
        accounts: cfg.serverFeatures.localSignerAdmin ? await localAccounts() : []
      });
      return;
    }

    if (req.method === "GET" && url.pathname === "/api/health") {
      const cfg = publicConfig(req);
      const [status, tree, audit, accounts] = await Promise.allSettled([
        fetchJson(rpcHttpUrl("/status")),
        fetchJson(restUrl("/clairveil/privacy/v1/tree_state")),
        fetchJson(restUrl("/clairveil/privacy/v1/audit_config")),
        cfg.serverFeatures.localSignerAdmin ? localAccounts() : []
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

    sendJson(res, 404, { error: "not found" });
  } catch (error) {
    sendJson(res, error?.statusCode || 400, errorPayload(error));
  }
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
  stream.pipe(res.writeHead(200, { "content-type": contentType, "cache-control": "no-store" }));
}

const server = createServer((req, res) => {
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

server.listen(config.port, config.host, () => {
  console.log(`Clairveil DApp: ${dappUrls().join(", ")}`);
  console.log(`Clairveil home: ${config.home}`);
  console.log(`RPC: ${config.rpc}`);
  console.log(`REST: ${config.rest}`);
  console.log(`Keplr RPC: ${publicRpcEndpoint()}`);
  console.log(`Keplr REST: ${publicRestEndpoint()}`);
});
