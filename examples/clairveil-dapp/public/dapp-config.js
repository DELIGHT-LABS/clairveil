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
    auditorAdmin: false
  },
  activeChainProfileId: clairveilProfile.id,
  chainProfiles: [clairveilProfile],
  keplrChainInfo: clairveilProfile.keplrChainInfo
};

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
