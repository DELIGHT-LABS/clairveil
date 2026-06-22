import {
  createClairveilClient,
  buildPreparedWithdrawProverPayload,
  decryptWithRootSeed,
  disclosureAmountAndAsset,
  encryptWithRootSeed,
  hashStringToField,
  scanNotes
} from "clairveiljs";
import { utf8Bytes, utf8String } from "clairveiljs/browser-crypto";
import { createClairveilPublicClient } from "clairveiljs/browser-public";
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
    const status: "spendable" | "spent" = firstNote.status;
    void { index, status };
    // @ts-expect-error scan note responses do not expose denom.
    firstNote.denom;
    // @ts-expect-error scan note responses expose status, not is_spent.
    firstNote.is_spent;
  }
  const plan = planTransferNotes({ amount: "1uclair", notes: [] });
  const prover = createStaticProverAdapter({ transferProofHex: "aa", withdrawProofHex: "bb" });
  const client = createClairveilClient({
    rpc: "http://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    chainId: "clairveil-local-3"
  });
  const restPath: string = client.restUrl("/clairveil/privacy/v1/events");
  const publicClient = createClairveilPublicClient({
    rest: "http://127.0.0.1:1317"
  });
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
    amount: "1uclair"
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
    proverAdapter: prover
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
    transactionOptions: {
      value: "0x0",
      withdrawOutputMode: "legacy-zero"
    }
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
