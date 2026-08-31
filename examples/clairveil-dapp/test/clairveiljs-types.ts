import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";
import type {
  BrowserCosmosWalletProfile,
  BrowserEvmWalletProfile,
  ClairveilWebClientConfig,
  DepositProofProvider
} from "clairveiljs/browser-dapp";
import { LocalStorageNoteStore } from "clairveiljs/note-store";

const gasPriceStep = { low: 1, average: 1, high: 1 };

const cosmosProfile = {
  id: "clairveil-local",
  label: "Clairveil Localnet",
  chainName: "Clairveil Localnet",
  transport: "cosmos",
  wallet: "keplr",
  chainId: "clairveil-local-2",
  rpc: "http://127.0.0.1:26657",
  rest: "http://127.0.0.1:1317",
  proverUrl: "http://127.0.0.1:8080",
  depositProofUrl: "http://127.0.0.1:5173/v1/prover/deposit",
  accountPrefix: "clair",
  shieldedPrefix: "clairs",
  denom: "uclair",
  displayDenom: "CLAIR",
  coinDecimals: 18,
  keplrCoinType: 118,
  gasPriceStep,
  keplrChainInfo: {
    chainId: "clairveil-local-2",
    chainName: "Clairveil Localnet",
    rpc: "http://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    bip44: { coinType: 118 },
    bech32Config: {
      bech32PrefixAccAddr: "clair",
      bech32PrefixAccPub: "clairpub",
      bech32PrefixValAddr: "clairvaloper",
      bech32PrefixValPub: "clairvaloperpub",
      bech32PrefixConsAddr: "clairvalcons",
      bech32PrefixConsPub: "clairvalconspub"
    },
    currencies: [{ coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 }],
    feeCurrencies: [{ coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18, gasPriceStep }],
    stakeCurrency: { coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 },
    features: []
  }
} as const satisfies BrowserCosmosWalletProfile;

const evmProfile = {
  id: "evm-local",
  label: "Clairveil EVM Localnet",
  chainName: "Clairveil EVM Localnet",
  transport: "evm",
  wallet: "metamask",
  chainId: "evm-privacy-local-1",
  rpc: "http://127.0.0.1:26657",
  rest: "http://127.0.0.1:1317",
  proverUrl: "http://127.0.0.1:8080",
  depositProofUrl: "http://127.0.0.1:5173/v1/prover/deposit",
  accountPrefix: "clair",
  shieldedPrefix: "clairs",
  denom: "utoken",
  displayDenom: "TOKEN",
  coinDecimals: 18,
  evmRpc: "http://127.0.0.1:8545",
  evmChainId: "0x32f",
  evmChainName: "Clairveil EVM Localnet",
  evmPrivacyPrecompileAddress: "0x0000000000000000000000000000000000000900",
  evmDepositMode: "payable-exact-value",
  evmNativeDenom: "utoken",
  evmAuthorizationProfile: {
    typedDataDomain: {
      name: "Target Privacy",
      version: "1"
    },
    supportedAuthorizationKinds: [1, 2, 3]
  },
  evmGasLimit: "0x989680",
  evmSendGasLimit: "0x5208"
} as const satisfies BrowserEvmWalletProfile;

const config = {
  schemaVersion: "clairveil-web-client-config-v1",
  activeChainProfileId: cosmosProfile.id,
  chainProfiles: [cosmosProfile, evmProfile],
  serverBacked: true,
  serverFeatures: {
    depositProof: true,
    proverProxy: true,
    batchTransfer: false
  }
} satisfies ClairveilWebClientConfig;

const identity = {
  address: "clair1xcjufgh2jarkp2qkx68azh08w9v5gah8sx9zu2",
  pubKeyHex: "02".padEnd(66, "0"),
  signatureBase64: "AQID"
};

const depositProofProvider: DepositProofProvider = async input => ({
  version: "v1",
  proof_hex: "aa",
  note_commitment_hex: input.noteCommitmentHex
});

async function typeSmoke(storage: Storage) {
  const validated = validateClairveilWebClientConfig(config);
  const activeId: string = validated.activeProfile.id;
  const noteStore = new LocalStorageNoteStore({
    storage,
    key: "clairveil:v0.3.1:types",
    owner: identity.address,
    allowPlaintext: true
  });

  const cosmos = createClairveilBrowserDappClient({
    profile: cosmosProfile,
    depositProofProvider,
    enableExperimentalBatchTransfer: false
  });
  const preparedDeposit = cosmos.prepareDeposit({
    ...identity,
    amount: "1uclair",
    depositProofProvider
  });
  const scan = cosmos.scanWalletNotes({
    ...identity,
    scanSource: "privacy_scan",
    after: { height: 0, globalSequence: 0, outputIndex: 0 },
    outputLimit: 200,
    maxPages: 5,
    includeFoundNotes: true,
    noteStore
  });
  const nullifiers: Promise<Map<string, boolean>> = cosmos.checkNullifiers(["00".repeat(32)]);
  const reserve = cosmos.queryReserve("uclair");
  const preflight = cosmos.assertProtocolPreflight("uclair");
  const selfView = cosmos.decodeSelfViewDisclosure({ ...identity, txHash: "AA" });

  const evm = createClairveilBrowserDappClient({ profile: evmProfile, depositProofProvider });
  const evmWallet = {
    getChainId: async () => "0x32f",
    sendTransaction: async () => `0x${"ab".repeat(32)}`
  };
  const preparedEvmDeposit = evm.prepareDeposit({
    ...identity,
    amount: "1utoken",
    evmWallet,
    depositProofProvider
  });
  const submittedEvm = evm.sendEvmTransaction({
    wallet: evmWallet,
    transaction: {
      to: evmProfile.evmPrivacyPrecompileAddress,
      data: "0x1234",
      value: "0x1",
      chainId: evmProfile.evmChainId
    }
  });

  const scanResult = await scan;
  const cursorHeight: number | string | undefined = scanResult.nextScanOptions.after?.height;
  const reserveInvariant: true = (await reserve).invariant_holds;
  return {
    activeId,
    preparedDeposit,
    preparedEvmDeposit,
    submittedEvm,
    nullifiers,
    preflight,
    selfView,
    cursorHeight,
    reserveInvariant
  };
}

void typeSmoke;
