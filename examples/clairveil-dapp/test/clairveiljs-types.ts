import {
  createClairveilClient,
  buildPreparedWithdrawProverPayload,
  createNoteReservationManager as createRootNoteReservationManager,
  decryptWithRootSeed,
  disclosureAmountAndAsset,
  encryptWithRootSeed,
  hashStringToField,
  scanNotes
} from "clairveiljs";
import { utf8Bytes, utf8String } from "clairveiljs/browser-crypto";
import { createClairveilPublicClient } from "clairveiljs/browser-public";
import { createClairveilBrowserDappClient } from "clairveiljs/browser-dapp";
import { deriveShieldedAddress } from "clairveiljs/core";
import { createClairveilClient as createCosmosClient } from "clairveiljs/cosmos";
import {
  bech32AddressToEvm,
  createClairveilEvmClient,
  createEip1193WalletAdapter,
  evmAddressToBech32,
  functionSelector,
  evmPrivacyPrecompileAddress
} from "clairveiljs/evm";
import type {
  EvmTransactionRequest,
  EvmTransferTransactionResult,
  EvmWithdrawTransactionResult
} from "clairveiljs/evm";
import type {
  PreparedTransferPayload,
  PreparedWithdrawProverPayloadResult,
  TransferMessageBuildResult,
  WithdrawMessageBuildResult
} from "clairveiljs/payload";
import { planTransferNotes } from "clairveiljs/planner";
import { createStaticProverAdapter } from "clairveiljs/prover";
import {
  createNoteReservationManager,
  hashRecipient,
  MemoryReservationStore,
  nullifierLookupKey,
  type ReservationBatch
} from "clairveiljs/reservation";

async function typeSmoke() {
  const rootSeed = new Uint8Array(32);
  const encrypted = encryptWithRootSeed(utf8Bytes("clairveil"), rootSeed);
  const decrypted = decryptWithRootSeed(encrypted, rootSeed);
  const text: string = utf8String(decrypted);
  const assetId: bigint = hashStringToField("uclair");
  const scan = await scanNotes({ rootSeed, events: [] });
  const totalSpendable: string = scan.summary.total_spendable;
  const scannedEvents: number = scan.diagnostics.scanned_events;
  const firstNote = scan.notes[0];
  if (firstNote) {
    const index: number = firstNote.index;
    const status: "spendable" | "spent" | "unverified" = firstNote.status;
    void { index, status };
    // @ts-expect-error scan note responses do not expose denom.
    firstNote.denom;
    // @ts-expect-error scan note responses expose status, not is_spent.
    firstNote.is_spent;
  }
  const plan = planTransferNotes({ amount: "1uclair", notes: [] });
  const prover = createStaticProverAdapter({ transferProofHex: "aa", withdrawProofHex: "bb" });
  const checkNullifiers = async (nullifiers: readonly string[]) =>
    new Map(nullifiers.map(nullifier => [nullifier, false]));
  const reservationManager = createNoteReservationManager({
    store: new MemoryReservationStore(),
    ownerKeyId: "clairveil-local-3:clair1example",
    indexKey: rootSeed
  });
  const rootReservationManager = createRootNoteReservationManager({
    store: new MemoryReservationStore(),
    ownerKeyId: "clairveil-local-3:clair1example",
    indexKey: rootSeed
  });
  const lookupKey: string = nullifierLookupKey("index-key-v1", "nullifier-0001");
  const availableNotes: Promise<object[]> = reservationManager.filterAvailableNotes([]);
  const rootAvailableNotes: Promise<object[]> = rootReservationManager.filterAvailableNotes([]);
  const renewedReservations = reservationManager.heartbeatLease([], {
    leaseToken: "lease",
    leaseDurationMs: 60000
  });
  const replanReservations = reservationManager.markReplanRequired([], {
    txHash: "aa".repeat(32),
    nullifierUnspentConfirmed: true,
    txAbsentOrFailedConfirmed: true,
    txHashChecked: "aa".repeat(32),
    error: "receipt failed"
  });
  const client = createClairveilClient({
    rpc: "http://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    chainId: "clairveil-local-3"
  });
  const restPath: string = client.restUrl("/clairveil/privacy/v1/events");
  const publicClient = createClairveilPublicClient({
    rest: "http://127.0.0.1:1317"
  });
  const browserEvmClient = createClairveilBrowserDappClient({
    profile: {
      transport: "evm",
      wallet: "metamask",
      id: "clairveil-evm-local",
      label: "EVM Localnet",
      chainName: "EVM Localnet",
      chainId: "clairveil-evm-1",
      rest: "http://127.0.0.1:1317",
      rpc: "http://127.0.0.1:26657",
      proverUrl: "http://127.0.0.1:8080",
      displayDenom: "CLAIR",
      coinDecimals: 18,
      evmRpc: "http://127.0.0.1:8545",
      evmChainId: "0x539",
      evmChainName: "EVM Localnet",
      evmPrivacyPrecompileAddress: "0x1111111111111111111111111111111111111111",
      evmGasLimit: "0x989680",
      evmSendGasLimit: "0x5208",
      accountPrefix: "clair",
      shieldedPrefix: "clairs",
      denom: "uclair"
    }
  });
  const browserCosmosClient = createClairveilBrowserDappClient({
    profile: {
      transport: "cosmos",
      wallet: "keplr",
      id: "clairveil-local",
      label: "Clairveil Localnet",
      chainName: "Clairveil Localnet",
      chainId: "clairveil-local-3",
      rest: "http://127.0.0.1:1317",
      rpc: "http://127.0.0.1:26657",
      proverUrl: "http://127.0.0.1:8080/privacy-gateway",
      displayDenom: "CLAIR",
      coinDecimals: 18,
      keplrCoinType: 118,
      gasPriceStep: { low: 0.01, average: 0.025, high: 0.04 },
      keplrChainInfo: {
        chainId: "clairveil-local-3",
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
        feeCurrencies: [{
          coinDenom: "CLAIR",
          coinMinimalDenom: "uclair",
          coinDecimals: 18,
          gasPriceStep: { low: 0.01, average: 0.025, high: 0.04 }
        }],
        stakeCurrency: { coinDenom: "CLAIR", coinMinimalDenom: "uclair", coinDecimals: 18 },
        features: []
      },
      accountPrefix: "clair",
      shieldedPrefix: "clairs",
      denom: "uclair"
    },
    enableExperimentalBatchTransfer: true
  });
  const typedBrowserBatch = browserCosmosClient.prepareTransferBatch({
    address: "clair1example",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: "AQID",
    payments: [
      {
        itemId: "A",
        amount: "1uclair",
        recipient: "clairs1recipient",
        userPrivacyPolicy: "all-private",
        userDisclosureMode: "none"
      }
    ],
    reservationManager,
    onPreparedPayload: async (_payload, context) => {
      const operationId: string = context.operationId;
      void operationId;
    },
    onPreparedProof: async (_proof, context) => {
      const operationId: string = context.operationId;
      void operationId;
    }
  });
  const typedCosmosDeposit = browserCosmosClient.prepareDeposit({
    address: "clair1example",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: "AQID",
    amount: "1uclair",
    proofHex: "ab",
  });
  typedCosmosDeposit.then((prepared) => {
    const encryptedNote: string = prepared.prepared.encryptedNoteHex;
    void encryptedNote;
  });
  const typedEvmReceipt: Promise<{ blockNumber: string } | null> =
    browserEvmClient.evmJsonRpc<{ blockNumber: string } | null>(
      "eth_getTransactionReceipt",
      ["0x".padEnd(66, "0")]
    );
  const typedBrowserCircuitConfig = browserEvmClient.assertCircuitConfig();
  const typedBrowserTransferConfig = browserEvmClient.assertTransferProtocolConfig("uclair");
  const typedBrowserAsset = browserEvmClient.queryAssetByDenom("uclair");
  const typedBrowserTree = browserEvmClient.fetchTreeState();
  publicClient.fetchPrivacyEvents({ limit: 10 });
  publicClient.fetchAuditableTransfers({ eventTypes: ["shielded_transfer"] });
  client.getTx("AA");
  client.waitForTx("AA", { attempts: 1, intervalMs: 1 });
  client.fetchTreeState();
  client.fetchCommitmentInfo("aa");
  client.fetchDisclosureConfig();
  client.fetchCircuitConfig();
  client.fetchAuditableTransfers();
  client.findPrivacyEventByTxHash("AA");
  const account = client.derivePrivacyAccount({
    address: "clair1example",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: "AQID"
  });
  const shielded: string = account.shielded_address;
  const coreShielded: string = deriveShieldedAddress(rootSeed);
  const customShielded: string = deriveShieldedAddress(rootSeed, {
    shieldedPrefix: "demos"
  });
  const customRecipientHash: string = hashRecipient(customShielded, {
    shieldedPrefix: "demos"
  });
  const cosmosClient = createCosmosClient({
    rpc: "http://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    chainId: "clairveil-local-3"
  });
  const evmClient = createClairveilEvmClient({
    contractAddress: "0x1111111111111111111111111111111111111111",
    chainId: "0x539"
  });
  const evmWallet = createEip1193WalletAdapter({
    provider: {
      request: async () => ["0x1111111111111111111111111111111111111111"]
    }
  });
  const selector: string = functionSelector("deposit((string,bytes,bytes))");
  const evmPrecompileAddress: string = evmPrivacyPrecompileAddress;
  const bech32: string = evmAddressToBech32("0x1111111111111111111111111111111111111111", "demo");
  const evmAddress: string = bech32AddressToEvm(bech32, "demo");
  client.buildDepositMaterial({
    creator: "clair1example",
    rootSeed,
    amount: "1uclair"
  });
  client.createDepositSignDoc({
    material: {
      address: "clair1example",
      pubKeyHex: "02".padEnd(66, "0"),
      signatureBase64: "AQID",
      signingMessage: "x",
      rootSeed,
      rootSeedHex: "00".repeat(32),
      rootSignatureHash: "00".repeat(32),
      shieldedAddress: "clairs1example",
      disclosureScalar: 1n,
      disclosureScalarHex: "01".padStart(64, "0"),
      disclosurePubKey: { x: 0n, y: 1n },
      disclosurePubKeyHex: "00".repeat(32)
    },
    amount: "1uclair",
    proofHex: "ab"
  });
  client.createTransferSignDoc({
    material: {
      address: "clair1example",
      pubKeyHex: "02".padEnd(66, "0"),
      signatureBase64: "AQID",
      signingMessage: "x",
      rootSeed,
      rootSeedHex: "00".repeat(32),
      rootSignatureHash: "00".repeat(32),
      shieldedAddress: "clairs1example",
      disclosureScalar: 1n,
      disclosureScalarHex: "01".padStart(64, "0"),
      disclosurePubKey: { x: 0n, y: 1n },
      disclosurePubKeyHex: "00".repeat(32)
    },
    amount: "1uclair",
    recipient: "clairs1recipient",
    proverAdapter: prover,
    reservationManager
  });
  const transferBuild: Promise<TransferMessageBuildResult> = client.buildTransferMessage({
    creator: "clair1example",
    inputs: [],
    recipient: "clairs1recipient",
    amount: "1uclair",
    rootSeed,
    proverAdapter: prover
  });
  const transferBuildResult = await transferBuild;
  const transferPayload: PreparedTransferPayload = transferBuildResult.payload;
  const transferPayloadHash: string = transferPayload.payload_hash;
  const transferReservation: ReservationBatch | null = null;
  void {
    lookupKey,
    availableNotes,
    renewedReservations,
    replanReservations,
    transferReservation,
    typedEvmReceipt,
    typedBrowserBatch,
    typedBrowserCircuitConfig,
    typedBrowserTransferConfig,
    typedBrowserAsset,
    typedBrowserTree
  };
  const transferProofHex: string = transferBuildResult.proof.proof_hex;
  const transferNullifierBytes: Uint8Array | undefined = transferBuildResult.message.nullifiers[0];
  const withdrawProverPayload: Promise<PreparedWithdrawProverPayloadResult> = client.buildPreparedWithdrawProverPayload({
    notes: [],
    amount: "1uclair",
    recipient: "clair1recipient",
    rootSeed
  });
  const withdrawProverPayloadResult = await withdrawProverPayload;
  const withdrawProverPayloadHash: string = withdrawProverPayloadResult.payload.payload_hash;
  const withdrawBuild: Promise<WithdrawMessageBuildResult> = client.buildWithdrawMessage({
    creator: "clair1example",
    notes: [],
    amount: "1uclair",
    recipient: "clair1recipient",
    rootSeed,
    proverAdapter: prover
  });
  const withdrawBuildResult = await withdrawBuild;
  const withdrawPayloadHash: string = withdrawBuildResult.payload.payload_hash;
  const withdrawMessageRecipient: string = withdrawBuildResult.message.recipient;
  const evmTransferTx: Promise<EvmTransferTransactionResult> = evmClient.buildTransferTransaction({
    creator: "clair1example",
    inputs: [],
    recipient: "clairs1recipient",
    amount: "1uclair",
    rootSeed,
    proverAdapter: prover,
    checkNullifiers,
    transactionOptions: {
      value: "0x0",
      chainId: "0x539"
    }
  });
  const evmWithdrawTx: Promise<EvmWithdrawTransactionResult> = evmClient.buildWithdrawTransaction({
    notes: [],
    amount: "1uclair",
    recipient: "0x1111111111111111111111111111111111111111",
    rootSeed,
    proverAdapter: prover,
    checkNullifiers,
    transactionOptions: {
      value: "0x0"
    }
  });
  // @ts-expect-error direct EVM transfer preparation must provide nullifier preflight.
  evmClient.buildTransferTransaction({
    creator: "clair1example",
    inputs: [],
    recipient: "clairs1recipient",
    amount: "1uclair",
    rootSeed,
    proverAdapter: prover
  });
  // @ts-expect-error direct EVM withdraw preparation must provide nullifier preflight.
  evmClient.buildWithdrawTransaction({
    notes: [],
    amount: "1uclair",
    recipient: "0x1111111111111111111111111111111111111111",
    rootSeed,
    proverAdapter: prover
  });
  const builtEvmTransfer = await evmTransferTx;
  const evmTxRequest: EvmTransactionRequest = builtEvmTransfer.transaction;
  const builtEvmWithdraw = await evmWithdrawTx;
  const evmWithdrawRecipient: string | undefined = builtEvmWithdraw.message.evmRecipient;
  client.decodeUserDisclosure({
    txHash: "AA",
    address: "clair1example",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: "AQID",
    skipSignerPubKeyCheck: true
  });
  client.decodeUserDisclosure({
    txHash: "AA"
  });
  client.decodeSelfViewDisclosure({
    txHash: "AA",
    address: "clair1example",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: "AQID",
    skipSignerPubKeyCheck: true
  });
  client.decodeAuditDisclosure({
    txHash: "AA",
    disclosurePrivKeyHex: "01".padStart(64, "0")
  });
  const txBytes: Uint8Array = client.buildTxRawBytes({
    bodyBytes: "",
    authInfoBytes: "",
    signature: ""
  });
  const amountDisclosure = disclosureAmountAndAsset({});
  const maybeAmount: bigint | null = amountDisclosure.amount;
  const maybeAssetId: bigint | null = amountDisclosure.assetId;
  const assetDenomText: string = amountDisclosure.assetDenom;
  buildPreparedWithdrawProverPayload({
    notes: [],
    amount: "1uclair",
    assetDenom: "uclair",
    recipient: "clair1recipient",
    chainId: "clairveil-local-3",
    spendNoteHashSigner: {
      signSpendNoteHash: async () => new Uint8Array(64)
    }
  });
  // @ts-expect-error withdraw prover payload uses spendNoteHashSigner, not noteHashSigner.
  buildPreparedWithdrawProverPayload({ amount: "1uclair", noteHashSigner: {} });
  // @ts-expect-error withdraw prover payload uses amount/assetDenom, not targetCoin.
  buildPreparedWithdrawProverPayload({ targetCoin: "1uclair" });

  return {
    text,
    assetId,
    totalSpendable,
    scannedEvents,
    scan,
    plan,
    prover,
    client,
    restPath,
    shielded,
    coreShielded,
    customRecipientHash,
    cosmosClient,
    evmClient,
    evmWallet,
    selector,
    evmPrecompileAddress,
    bech32,
    evmAddress,
    txBytes,
    maybeAmount,
    maybeAssetId,
    assetDenomText,
    transferPayloadHash,
    transferProofHex,
    transferNullifierBytes,
    withdrawProverPayloadHash,
    withdrawPayloadHash,
    withdrawMessageRecipient,
    evmTxRequest,
    evmWithdrawRecipient
  };
}

void typeSmoke;
