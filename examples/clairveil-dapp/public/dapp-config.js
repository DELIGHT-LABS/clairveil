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

export async function loadStaticDappConfig({ fetchImpl = globalThis.fetch } = {}) {
  if (typeof fetchImpl !== "function") {
    throw new Error("static DApp config requires fetch support");
  }
  const response = await fetchImpl(staticDappConfigPath, {
    cache: "no-store",
    credentials: "same-origin",
    headers: { Accept: "application/json" },
    redirect: "error"
  });
  if (!response.ok) {
    throw new Error(`static DApp config returned HTTP ${response.status}`);
  }
  const contentType = String(response.headers.get("content-type") || "")
    .split(";", 1)[0]
    .trim()
    .toLowerCase();
  if (contentType !== "application/json") {
    throw new Error("static DApp config must return Content-Type: application/json");
  }
  const config = await response.json();
  if (!config || typeof config !== "object" || Array.isArray(config)) {
    throw new Error("static DApp config must return a JSON object");
  }
  return config;
}

export function getStaticDappConfig() {
  const override = globalThis.CLAIRVEIL_DAPP_CONFIG || {};
  return {
    ...defaultDappConfig,
    ...override,
    serverBacked: false,
    serverFeatures: {
      ...defaultDappConfig.serverFeatures,
      ...(override.serverFeatures || {})
    },
    chainProfiles: override.chainProfiles || defaultDappConfig.chainProfiles
  };
}
