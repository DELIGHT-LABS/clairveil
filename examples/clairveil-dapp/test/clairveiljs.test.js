import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { readFile, realpath } from "node:fs/promises";
import {
  createClairveilClient,
  derivePrivacyMaterial
} from "clairveiljs";
import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";
import { createClairveilPublicClient } from "clairveiljs/browser-public";
import { validatePrivacyScanPageV2 } from "clairveiljs/scan";
import { EncryptedLocalStorageNoteStore } from "../public/encrypted-note-store.js";

function cosmosProfile(overrides = {}) {
  const profile = {
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
    gasPriceStep: { low: 1, average: 1, high: 1 }
  };
  profile.keplrChainInfo = {
    chainId: profile.chainId,
    chainName: profile.chainName,
    rpc: profile.rpc,
    rest: profile.rest,
    bip44: { coinType: profile.keplrCoinType },
    bech32Config: {
      bech32PrefixAccAddr: "clair",
      bech32PrefixAccPub: "clairpub",
      bech32PrefixValAddr: "clairvaloper",
      bech32PrefixValPub: "clairvaloperpub",
      bech32PrefixConsAddr: "clairvalcons",
      bech32PrefixConsPub: "clairvalconspub"
    },
    currencies: [{ coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 }],
    feeCurrencies: [{
      coinDenom: "CLAIR",
      coinMinimalDenom: "uclair",
      coinDecimals: 18,
      gasPriceStep: profile.gasPriceStep
    }],
    stakeCurrency: { coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 },
    features: []
  };
  return { ...profile, ...overrides };
}

function evmProfile(overrides = {}) {
  return {
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
    evmGasLimit: "0x989680",
    evmSendGasLimit: "0x5208",
    ...overrides
  };
}

function identity() {
  return {
    address: "clair1xcjufgh2jarkp2qkx68azh08w9v5gah8sx9zu2",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("clairveil-dapp-v0.3.1").toString("base64")
  };
}

function memoryStorage() {
  const values = new Map();
  return {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, String(value)),
    removeItem: key => values.delete(key)
  };
}

test("example DApp resolves the sibling local ClairveilJS v0.3.1 package", async () => {
  const dappPackage = JSON.parse(await readFile(new URL("../package.json", import.meta.url), "utf8"));
  const sdkPackage = JSON.parse(await readFile(new URL("../node_modules/clairveiljs/package.json", import.meta.url), "utf8"));
  const resolvedSdk = await realpath(new URL("../node_modules/clairveiljs", import.meta.url));

  assert.equal(dappPackage.dependencies.clairveiljs, "file:../../../clairveiljs");
  assert.equal(sdkPackage.version, "0.3.1");
  assert.match(resolvedSdk, /\/clairveiljs$/);
});

test("v0.3.1 web config validates Cosmos and payable-exact-value EVM profiles", () => {
  const cosmos = cosmosProfile();
  const cosmosConfig = validateClairveilWebClientConfig({
    schemaVersion: "clairveil-web-client-config-v1",
    activeChainProfileId: cosmos.id,
    chainProfiles: [cosmos],
    serverBacked: true,
    serverFeatures: { depositProof: true, proverProxy: true, batchTransfer: false }
  });
  assert.equal(cosmosConfig.activeProfile.depositProofUrl, cosmos.depositProofUrl);

  const evm = evmProfile();
  const evmConfig = validateClairveilWebClientConfig({
    schemaVersion: "clairveil-web-client-config-v1",
    activeChainProfileId: evm.id,
    chainProfiles: [evm]
  });
  assert.equal(evmConfig.activeProfile.evmDepositMode, "payable-exact-value");
  assert.throws(
    () => validateClairveilWebClientConfig({
      schemaVersion: "clairveil-web-client-config-v1",
      activeChainProfileId: evm.id,
      chainProfiles: [evmProfile({ evmNativeDenom: "uother" })]
    }),
    /must match profile\.denom/
  );
});

test("browser deposit preparation requires a DepositCircuit proof provider", async () => {
  const profile = cosmosProfile();
  delete profile.depositProofUrl;
  const client = createClairveilBrowserDappClient({ profile });
  let preparedInput = null;
  client.cosmos.prepareDeposit = async input => {
    preparedInput = input;
    return {
      signDoc: { chainId: "clairveil-local-2" },
      privacyAccount: { shielded_address: input.material.shieldedAddress },
      material: input.depositMaterial
    };
  };

  await assert.rejects(
    () => client.prepareDeposit({ ...identity(), amount: "1uclair" }),
    /deposit proof is required/
  );

  const prepared = await client.prepareDeposit({
    ...identity(),
    amount: "1uclair",
    depositProofProvider: async input => ({
      version: "v1",
      proof_hex: "aa",
      note_commitment_hex: input.noteCommitmentHex
    })
  });
  assert.equal(prepared.prepared.amount, "1uclair");
  assert.equal(preparedInput.proofHex, "aa");
  assert.equal(preparedInput.depositMaterial.note_commitment_hex.length, 64);
});

test("typed privacy scan preserves its three-part cursor and forbids event filters", async () => {
  const client = createClairveilClient({
    rpc: "http://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    chainId: "clairveil-local-2"
  });
  const requests = [];
  client.queryPrivacyScan = async request => {
    requests.push(request);
    return validatePrivacyScanPageV2({
      scanSchemaVersion: "privacy-scan-v2",
      summaries: [],
      outputs: [],
      nextCursor: { height: 0, globalSequence: 0, outputIndex: 0 },
      hasMore: false,
      scannedEventCount: 0
    }, request);
  };

  const result = await client.scanNotes({
    rootSeed: new Uint8Array(32),
    scanSource: "privacy_scan",
    after: { height: 0, globalSequence: 0, outputIndex: 0 },
    outputLimit: 200,
    includeFoundNotes: true
  });
  assert.deepEqual(requests[0].eventTypes, []);
  assert.equal(result.scanCursor.source, "privacy_scan");
  assert.deepEqual(result.nextScanOptions.after, { height: 0, globalSequence: 0, outputIndex: 0 });
  assert.equal(result.nextScanOptions.scanSource, "privacy_scan");
});

test("encrypted local note store persists the full typed cursor without plaintext", async () => {
  const storage = memoryStorage();
  const cursor = {
    source: "privacy_scan",
    after: { height: 8, global_sequence: 3, output_index: 1 },
    next_cursor: { height: 9, global_sequence: 4, output_index: 2 },
    output_limit: 200,
    has_more: true
  };
  const options = {
    storage,
    cryptoImpl: webcrypto,
    key: "clairveil:v0.3.1:test",
    owner: "clair1owner",
    keyMaterial: new Uint8Array([1, 2, 3, 4]),
    namespace: "clairveil-local-2:test:clair1owner"
  };
  const store = await EncryptedLocalStorageNoteStore.open(options);
  await store.mergeScanResult({ foundNotes: [], scanCursor: cursor }, { owner: "clair1owner" });

  const raw = storage.getItem("clairveil:v0.3.1:test");
  assert.match(raw, /clairveil-encrypted-note-store-v1/);
  assert.doesNotMatch(raw, /clair1owner|privacy_scan|next_cursor/);

  const reloaded = await EncryptedLocalStorageNoteStore.open(options);
  assert.deepEqual((await reloaded.load()).scanCursor, cursor);
  await assert.rejects(
    () => EncryptedLocalStorageNoteStore.open({ ...options, keyMaterial: new Uint8Array([9, 9, 9]) }),
    error => error.code === "NOTE_CACHE_CORRUPT"
  );
});

test("nullifier checks use the v0.3.1 batch POST endpoint", async () => {
  const requests = [];
  const client = createClairveilPublicClient({
    rest: "http://rest.example"
  });
  client.fetchNullifierJson = async (path, init) => {
    const body = JSON.parse(init.body);
    requests.push({ path, method: init.method, body });
    return {
      statuses: body.nullifiers.map((nullifier, index) => ({ nullifier, used: index === 1 }))
    };
  };
  const nullifiers = ["00".repeat(32), "11".repeat(32)];
  const statuses = await client.checkNullifiers(nullifiers);

  assert.equal(client.restUrl(requests[0].path), "http://rest.example/clairveil/privacy/v1/nullifiers");
  assert.equal(requests[0].method, "POST");
  assert.deepEqual(requests[0].body.nullifiers, nullifiers);
  assert.equal(statuses.get(nullifiers[0]), false);
  assert.equal(statuses.get(nullifiers[1]), true);
});

test("payable EVM deposits bind native denom before proof work and use SDK submission", async () => {
  const client = createClairveilBrowserDappClient({ profile: evmProfile() });
  client.evmJsonRpc = async () => "0x32f";
  let proofCalled = false;
  await assert.rejects(
    () => client.prepareDeposit({
      ...identity(),
      amount: "1uother",
      evmWallet: { getChainId: async () => "0x32f" },
      depositProofProvider: async () => {
        proofCalled = true;
        return { proof_hex: "aa" };
      }
    }),
    /does not match native denom/
  );
  assert.equal(proofCalled, false);

  let submitted = null;
  client.evm.sendTransaction = async (wallet, transaction) => {
    submitted = { wallet, transaction };
    return `0x${"ab".repeat(32)}`;
  };
  const wallet = {
    getChainId: async () => "0x32f",
    sendTransaction: async () => `0x${"cd".repeat(32)}`
  };
  const transaction = {
    to: evmProfile().evmPrivacyPrecompileAddress,
    data: "0x1234",
    value: "0x1",
    chainId: "0x32f"
  };
  const hash = await client.sendEvmTransaction({ wallet, transaction });
  assert.equal(hash, `0x${"ab".repeat(32)}`);
  assert.deepEqual(submitted.transaction, transaction);
});

test("browser facade exposes reserve, protocol preflight, and self-view decoding", async () => {
  const client = createClairveilBrowserDappClient({ profile: cosmosProfile() });
  client.cosmos.queryReserve = async denom => ({ denom, invariant_holds: true });
  client.cosmos.assertProtocolPreflight = async denom => ({ denom });
  client.cosmos.decodeSelfViewDisclosure = async input => ({ input, verified: true });

  assert.equal((await client.queryReserve("uclair")).invariant_holds, true);
  assert.equal((await client.assertProtocolPreflight("uclair")).denom, "uclair");
  const disclosure = await client.decodeSelfViewDisclosure({ ...identity(), txHash: "AA" });
  assert.equal(disclosure.verified, true);
  assert.equal(disclosure.input.txHash, "AA");
});

test("privacy identity remains deterministic under the v0.3.1 SDK", () => {
  const first = derivePrivacyMaterial(identity());
  const second = derivePrivacyMaterial(identity());
  assert.equal(first.rootSeedHex, second.rootSeedHex);
  assert.equal(first.shieldedAddress, second.shieldedAddress);
  assert.equal(first.disclosurePubKeyHex, second.disclosurePubKeyHex);
});
