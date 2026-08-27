const localRpc = "http://127.0.0.1:26657";
const localRest = "http://127.0.0.1:1317";
const localProver = "http://127.0.0.1:8080";

function keplrChainInfo({
  chainId,
  chainName,
  rpc,
  rest,
  coinType = 118,
  accountPrefix = "clair",
  displayDenom = "CLAIR",
  denom = "uclair",
  coinDecimals = 18,
  gasPriceStep = { low: 1, average: 1, high: 1 }
}) {
  return {
    chainId,
    chainName,
    rpc,
    rest,
    bip44: { coinType },
    bech32Config: {
      bech32PrefixAccAddr: accountPrefix,
      bech32PrefixAccPub: `${accountPrefix}pub`,
      bech32PrefixValAddr: `${accountPrefix}valoper`,
      bech32PrefixValPub: `${accountPrefix}valoperpub`,
      bech32PrefixConsAddr: `${accountPrefix}valcons`,
      bech32PrefixConsPub: `${accountPrefix}valconspub`
    },
    currencies: [{ coinDenom: displayDenom, coinMinimalDenom: denom, coinDecimals }],
    feeCurrencies: [{ coinDenom: displayDenom, coinMinimalDenom: denom, coinDecimals, gasPriceStep }],
    stakeCurrency: { coinDenom: displayDenom, coinMinimalDenom: denom, coinDecimals },
    features: []
  };
}

const clairveilProfile = {
  id: "clairveil-local",
  label: "Clairveil Localnet",
  chainName: "Clairveil Localnet",
  transport: "cosmos",
  wallet: "keplr",
  chainId: "clairveil-local-2",
  rpc: localRpc,
  rest: localRest,
  proverUrl: localProver,
  accountPrefix: "clair",
  shieldedPrefix: "clairs",
  denom: "uclair",
  displayDenom: "CLAIR",
  coinDecimals: 18,
  keplrCoinType: 118,
  gasPriceStep: { low: 1, average: 1, high: 1 }
};
clairveilProfile.keplrChainInfo = keplrChainInfo({
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

export const defaultDappConfig = {
  schemaVersion: "clairveil-web-client-config-v1",
  serverBacked: false,
  modeLabel: "Static Public DApp",
  home: "",
  localSignerHome: "",
  localSignerBin: "",
  chainId: clairveilProfile.chainId,
  rpc: clairveilProfile.rpc,
  rest: clairveilProfile.rest,
  proverUrl: clairveilProfile.proverUrl,
  transport: clairveilProfile.transport,
  denom: clairveilProfile.denom,
  displayDenom: clairveilProfile.displayDenom,
  coinDecimals: clairveilProfile.coinDecimals,
  accountPrefix: clairveilProfile.accountPrefix,
  shieldedPrefix: clairveilProfile.shieldedPrefix,
  localTestMode: false,
  serverFeatures: {
    localTestMode: false,
    localSigners: false,
    faucet: false,
    depositProof: false,
    auditorAdmin: false,
    proverProxy: false,
    batchTransfer: false
  },
  activeChainProfileId: clairveilProfile.id,
  chainProfiles: [clairveilProfile],
  keplrChainInfo: clairveilProfile.keplrChainInfo
};

export const staticDappConfigPath = "/dapp-config.json";
export const serverBackedDappConfigPath = "/api/health";
export const dappBootstrapTimeoutMs = 30_000;
export const dappBootstrapMaxBytes = 1024 * 1024;

export class DappBootstrapError extends Error {
  constructor(message, { code = "DAPP_BOOTSTRAP_FAILED", cause, status = 0, contentType = "" } = {}) {
    super(message, cause === undefined ? undefined : { cause });
    this.name = "DappBootstrapError";
    this.code = code;
    this.status = status;
    this.contentType = contentType;
  }
}

function assertDirectDappBootstrapResponse(response, path) {
  const status = Number(response?.status || 0);
  const redirectStatus = [300, 301, 302, 303, 305, 307, 308].includes(status);
  if (response?.type === "opaqueredirect" || response?.redirected === true || redirectStatus) {
    throw new DappBootstrapError("DApp bootstrap must not redirect", {
      code: "DAPP_BOOTSTRAP_REDIRECTED",
      status
    });
  }

  const finalUrl = String(response?.url || "").trim();
  const browserUrl = String(globalThis.location?.href || "").trim();
  let expectedUrl = "";
  try {
    expectedUrl = browserUrl
      ? new URL(path, browserUrl).href
      : new URL(path).href;
  } catch {
    // Relative paths can be compared only in a browser, where location exists.
  }
  if (finalUrl && expectedUrl && finalUrl !== expectedUrl) {
    throw new DappBootstrapError("DApp bootstrap final URL does not match the requested artifact", {
      code: "DAPP_BOOTSTRAP_REDIRECTED",
      status
    });
  }
}

async function readBoundedResponse(response, maxBytes) {
  const declaredLength = Number(response.headers.get("content-length") || 0);
  if (Number.isFinite(declaredLength) && declaredLength > maxBytes) {
    throw new DappBootstrapError(`DApp bootstrap response exceeds ${maxBytes} bytes`, {
      code: "DAPP_BOOTSTRAP_TOO_LARGE",
      status: response.status
    });
  }

  if (!response.body?.getReader) {
    const bytes = new Uint8Array(await response.arrayBuffer());
    if (bytes.byteLength > maxBytes) {
      throw new DappBootstrapError(`DApp bootstrap response exceeds ${maxBytes} bytes`, {
        code: "DAPP_BOOTSTRAP_TOO_LARGE",
        status: response.status
      });
    }
    return bytes;
  }

  const reader = response.body.getReader();
  const chunks = [];
  let length = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      length += value.byteLength;
      if (length > maxBytes) {
        await reader.cancel("DApp bootstrap response too large").catch(() => {});
        throw new DappBootstrapError(`DApp bootstrap response exceeds ${maxBytes} bytes`, {
          code: "DAPP_BOOTSTRAP_TOO_LARGE",
          status: response.status
        });
      }
      chunks.push(value);
    }
  } finally {
    reader.releaseLock();
  }

  const bytes = new Uint8Array(length);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return bytes;
}

export async function fetchBoundedDappJson(path, {
  fetchImpl = globalThis.fetch,
  timeoutMs = dappBootstrapTimeoutMs,
  maxBytes = dappBootstrapMaxBytes
} = {}) {
  if (typeof fetchImpl !== "function") {
    throw new DappBootstrapError("DApp bootstrap requires fetch support", {
      code: "DAPP_BOOTSTRAP_UNREACHABLE"
    });
  }

  const controller = new AbortController();
  const timeout = globalThis.setTimeout(() => controller.abort(), timeoutMs);
  try {
    const response = await fetchImpl(path, {
      cache: "no-store",
      credentials: "same-origin",
      headers: { Accept: "application/json" },
      // `redirect: "error"` rejects with an indistinguishable TypeError, which
      // would be misclassified below as an unreachable health endpoint and
      // incorrectly permit the static-config fallback. Inspect a manual
      // response instead, then fail closed on every redirect shape.
      redirect: "manual",
      signal: controller.signal
    });
    assertDirectDappBootstrapResponse(response, path);
    const contentType = String(response.headers.get("content-type") || "")
      .split(";", 1)[0]
      .trim()
      .toLowerCase();
    if (!response.ok) {
      throw new DappBootstrapError(`DApp bootstrap returned HTTP ${response.status}`, {
        code: "DAPP_BOOTSTRAP_HTTP_ERROR",
        status: response.status,
        contentType
      });
    }
    if (contentType !== "application/json") {
      throw new DappBootstrapError("DApp bootstrap must return Content-Type: application/json", {
        code: "DAPP_BOOTSTRAP_CONTENT_TYPE",
        status: response.status,
        contentType
      });
    }

    const bytes = await readBoundedResponse(response, maxBytes);
    try {
      return JSON.parse(new TextDecoder().decode(bytes));
    } catch (cause) {
      throw new DappBootstrapError("DApp bootstrap returned malformed JSON", {
        code: "DAPP_BOOTSTRAP_INVALID_JSON",
        status: response.status,
        contentType,
        cause
      });
    }
  } catch (cause) {
    if (cause instanceof DappBootstrapError) throw cause;
    const timedOut = controller.signal.aborted;
    throw new DappBootstrapError(
      timedOut ? `DApp bootstrap timed out after ${timeoutMs}ms` : "DApp bootstrap endpoint is unreachable",
      { code: timedOut ? "DAPP_BOOTSTRAP_TIMEOUT" : "DAPP_BOOTSTRAP_UNREACHABLE", cause }
    );
  } finally {
    globalThis.clearTimeout(timeout);
  }
}

export function healthBootstrapFallbackAllowed(error) {
  if (["DAPP_BOOTSTRAP_UNREACHABLE", "DAPP_BOOTSTRAP_TIMEOUT"].includes(error?.code)) {
    return true;
  }
  if (error?.code === "DAPP_BOOTSTRAP_HTTP_ERROR" && [404, 405].includes(error.status)) {
    return true;
  }
  return error?.code === "DAPP_BOOTSTRAP_CONTENT_TYPE" && error.contentType === "text/html";
}

export function healthBootstrapEndpointAbsent(error) {
  return (error?.code === "DAPP_BOOTSTRAP_HTTP_ERROR" && [404, 405].includes(error.status))
    || (error?.code === "DAPP_BOOTSTRAP_CONTENT_TYPE" && error.contentType === "text/html");
}

function assertBatchTransferDisabled(config) {
  if (config?.serverFeatures?.batchTransfer !== false) {
    throw new DappBootstrapError("Clairveil v0.3.1 WebApp requires serverFeatures.batchTransfer=false", {
      code: "DAPP_BOOTSTRAP_UNSUPPORTED_FEATURE"
    });
  }
}

export async function loadServerDappHealth(options = {}) {
  const data = await fetchBoundedDappJson(serverBackedDappConfigPath, options);
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    throw new DappBootstrapError("server DApp health must return a JSON object", {
      code: "DAPP_BOOTSTRAP_INVALID_SCHEMA"
    });
  }
  assertBatchTransferDisabled(data.config);
  return data;
}

export async function loadStaticDappConfig(options = {}) {
  const config = await fetchBoundedDappJson(staticDappConfigPath, options);
  if (!config || typeof config !== "object" || Array.isArray(config)) {
    throw new DappBootstrapError("static DApp config must return a JSON object", {
      code: "DAPP_BOOTSTRAP_INVALID_SCHEMA"
    });
  }
  if (config.serverBacked !== false) {
    throw new DappBootstrapError("static DApp config must declare serverBacked: false", {
      code: "DAPP_BOOTSTRAP_INVALID_SCHEMA"
    });
  }
  assertBatchTransferDisabled(config);
  return config;
}

export function getStaticDappConfig() {
  const override = globalThis.CLAIRVEIL_DAPP_CONFIG || {};
  const config = {
    ...defaultDappConfig,
    ...override,
    serverBacked: false,
    serverFeatures: {
      ...defaultDappConfig.serverFeatures,
      ...(override.serverFeatures || {})
    },
    chainProfiles: override.chainProfiles || defaultDappConfig.chainProfiles
  };
  assertBatchTransferDisabled(config);
  return config;
}
