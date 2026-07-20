import test from "node:test";
import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { readFile } from "node:fs/promises";
import {
  MemoryNoteStore,
  LocalStorageNoteStore,
  ClairveilError,
  ClairveilErrorCode,
  assertDisclosurePubKeyHex,
  canonicalFieldHex,
  createAsyncJobProverAdapter,
  createClairveilClient,
  createOfflineSignerWalletAdapter,
  createWalletAdapter,
  buildPreparedTransferPayload,
  buildWithdrawMsgFromPayload,
  computePreparedWithdrawPayloadHash,
  computeExpectedDisclosureDigestHex,
  decodeUserDisclosureFromEvent,
  decodeShieldedAddress,
  derivePrivacyMaterial,
  derivePrivacyMaterialFromWallet,
  encryptWithRootSeed,
  decryptWithRootSeed,
  hashStringToField,
  payloadHex,
  planTransferNotes,
  planWithdrawNotes,
  scanNotes,
  MsgWithdraw,
  userDisclosureModePublic,
  userDisclosureModeRecipientEncrypted
} from "clairveiljs";
import {
  createClairveilEvmClient,
  createEvmContractAdapter,
  createEip1193WalletAdapter,
  evmAddressToBech32,
  functionSelector
} from "clairveiljs/evm";
import { createNote, deriveSpendKeys, deriveViewKeys } from "clairveiljs/core";
import { utf8Bytes, utf8String } from "clairveiljs/browser-crypto";
import { hashRecipient } from "clairveiljs/reservation";

const assetID = hashStringToField("uclair");
const canonicalRecipient = "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3";

function foundNote(amount, suffix, overrides = {}) {
  return {
    height: Number(amount) + suffix,
    txHash: `AA${String(suffix).padStart(2, "0")}`,
    isSpent: false,
    nullifierStatus: "unspent",
    nullifier: `00${String(suffix).padStart(62, "0")}`,
    note: {
      receiverSpendPubKeyX: 1n,
      receiverSpendPubKeyY: 2n,
      receiverViewPubKeyX: 3n,
      receiverViewPubKeyY: 4n,
      amount: BigInt(amount),
      assetID,
      randomness: BigInt(1000 + suffix),
      memo: "test",
      ...overrides.note
    },
    ...overrides
  };
}

function protobufFieldNumbers(bytes) {
  const fields = [];
  let offset = 0;
  const readVarint = () => {
    let value = 0n;
    let shift = 0n;
    while (offset < bytes.length) {
      const byte = BigInt(bytes[offset]);
      offset += 1;
      value |= (byte & 0x7fn) << shift;
      if ((byte & 0x80n) === 0n) return value;
      shift += 7n;
    }
    throw new Error("truncated varint");
  };

  while (offset < bytes.length) {
    const tag = readVarint();
    const fieldNumber = Number(tag >> 3n);
    const wireType = Number(tag & 0x07n);
    fields.push(fieldNumber);

    if (wireType === 0) {
      readVarint();
    } else if (wireType === 2) {
      const length = Number(readVarint());
      offset += length;
    } else {
      throw new Error(`unsupported wire type ${wireType}`);
    }
  }

  return fields;
}

test("wallet adapter derives Clairveil privacy material", async () => {
  const wallet = createWalletAdapter({
    address: "clair1example0000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signPrivacyRoot: async messageBytes => {
      assert.match(Buffer.from(messageBytes).toString("utf8"), /^clairveil-root-v1\n/);
      return Buffer.from("test-signature-v1");
    }
  });

  const material = await derivePrivacyMaterialFromWallet(wallet);
  assert.equal(material.address, "clair1example0000000000000000000000000000000");
  assert.equal(material.pubKeyHex, "02".padEnd(66, "0"));
  assert.match(material.shieldedAddress, /^clairs1/);
  assert.match(material.disclosurePubKeyHex, /^[0-9a-f]{64}$/);
});

test("vendored reservation recipient hash follows Go canonical address rules", () => {
  const expected = "8a3344bcbfdd71e8346f1fcc5d9d09d493c3345b0e94d26371f89b2574545d3c";
  assert.equal(hashRecipient(canonicalRecipient), expected);
  assert.equal(hashRecipient(canonicalRecipient.toUpperCase()), expected);
  assert.throws(() => hashRecipient("clair1recipient"), /shielded address/i);
  assert.throws(
    () => hashRecipient("clairs1llllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllllct37x5k"),
    /valid shielded address/i,
  );
});

test("vendored reservation recipient hash supports a custom shielded prefix", () => {
  const material = derivePrivacyMaterial({
    address: "demo1example0000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("dapp-custom-prefix").toString("base64"),
    shieldedPrefix: "demos",
  });
  const expected = createHash("sha256")
    .update(material.shieldedAddress)
    .digest("hex");

  assert.match(material.shieldedAddress, /^demos1/);
  assert.equal(
    hashRecipient(material.shieldedAddress, { shieldedPrefix: "demos" }),
    expected,
  );
  assert.equal(
    hashRecipient(material.shieldedAddress.toUpperCase(), { shieldedPrefix: "demos" }),
    expected,
  );
});

test("root privacy material retains signer fields for high-level builders", () => {
  const material = derivePrivacyMaterial({
    address: "clair1example0000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0").toUpperCase(),
    signatureBase64: Buffer.from("test-signature-v1").toString("base64")
  });

  assert.equal(material.address, "clair1example0000000000000000000000000000000");
  assert.equal(material.pubKeyHex, "02".padEnd(66, "0"));
  assert.equal(material.signatureBase64, Buffer.from("test-signature-v1").toString("base64"));
  assert.match(material.signingMessage, /pubkey:020000/);
  assert.match(material.shieldedAddress, /^clairs1/);
});

test("custom account and shielded prefixes flow through client privacy material", () => {
  const client = createClairveilClient({
    rpc: "tcp://127.0.0.1:26657",
    rest: "http://127.0.0.1:1317",
    chainId: "downstream-1",
    accountPrefix: "demo",
    shieldedPrefix: "demos",
    defaultDenom: "udemo"
  });
  const material = derivePrivacyMaterial({
    address: "demo1example0000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("test-signature-v1").toString("base64"),
    shieldedPrefix: "demos"
  });
  const account = client.derivePrivacyAccount({
    address: "demo1example0000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("test-signature-v1").toString("base64")
  });

  assert.match(account.shielded_address, /^demos1/);
  assert.doesNotThrow(() => decodeShieldedAddress(account.shielded_address, { shieldedPrefix: "demos" }));
  assert.throws(() => decodeShieldedAddress(account.shielded_address), /expected clairs, got demos/);
  assert.equal(client.buildDepositMaterial({
    creator: material.address,
    rootSeed: material.rootSeed,
    amount: "7"
  }).amount, "7udemo");
});

test("wallet adapter accepts browser signer fixture pubkey bytes", async () => {
  const fixture = JSON.parse(await readFile(
    new URL("../../../x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json", import.meta.url),
    "utf8"
  ));
  const rootSigner = fixture.root_signer;
  const wallet = createWalletAdapter({
    address: rootSigner.get_account_response.transparent_address,
    pubKeyHex: rootSigner.get_account_response.transparent_pubkey_hex,
    signPrivacyRoot: async messageBytes => {
      assert.equal(Buffer.from(messageBytes).toString("hex"), rootSigner.sign_request.message_hex);
      return Buffer.from(rootSigner.sign_response.signature_hex, "hex");
    }
  });

  assert.equal(await wallet.getPubKeyHex(), "0123456789abcdef");
  const material = await derivePrivacyMaterialFromWallet(wallet);
  assert.equal(material.rootSeedHex, rootSigner.expected_derived.root_seed_hex);
  assert.equal(material.shieldedAddress, rootSigner.expected_derived.shielded_address);
  assert.equal(material.disclosurePubKeyHex, rootSigner.expected_derived.disclosure_pubkey_hex);
});

test("offline signer adapter wraps CosmJS accounts and direct signing", async () => {
  let signedAddress = "";
  const adapter = createOfflineSignerWalletAdapter({
    signer: {
      async getAccounts() {
        return [{
          address: "clair1offline000000000000000000000000000000",
          pubkey: new Uint8Array([2, ...new Uint8Array(32)])
        }];
      },
      async signDirect(address, signDoc) {
        signedAddress = address;
        return { signed: signDoc, signature: { signature: "AQID" } };
      }
    },
    signPrivacyRoot: async () => new Uint8Array([1, 2, 3])
  });

  assert.equal(await adapter.getAddress(), "clair1offline000000000000000000000000000000");
  assert.equal((await adapter.getPubKeyHex()).length, 66);
  await adapter.signDirect({ bodyBytes: new Uint8Array(), authInfoBytes: new Uint8Array(), chainId: "x", accountNumber: 1n });
  assert.equal(signedAddress, "clair1offline000000000000000000000000000000");
});

test("transfer planner reports final transfer and self-merge states", () => {
  const ready = planTransferNotes({
    amount: "10uclair",
    notes: [foundNote(4, 1), foundNote(7, 2), foundNote(20, 3)]
  });
  assert.equal(ready.status, "final_transfer_ready");
  assert.equal(ready.canBuildTx, true);
  assert.equal(ready.selection.total, 11n);

  const merge = planTransferNotes({
    amount: "10uclair",
    notes: [foundNote(1, 1), foundNote(1, 2), foundNote(8, 3)]
  });
  assert.equal(merge.status, "self_merge_required");
  assert.equal(merge.canBuildTx, true);
  assert.equal(merge.nextAmount, "9uclair");
});

test("transfer payload builder rejects mixed-asset input notes before proving", async () => {
  const mixedAssetNote = foundNote(5, 2);
  mixedAssetNote.note.assetID = hashStringToField("uatom");

  await assert.rejects(
    buildPreparedTransferPayload({
      creator: "clair1builder000000000000000000000000000000",
      amount: "10uclair",
      inputs: [
        foundNote(5, 1),
        mixedAssetNote
      ]
    }),
    /transfer input 1 asset does not match requested denom uclair/
  );
});

test("transfer payload builder uses configured shielded prefix in disclosure payloads", async () => {
  const sender = derivePrivacyMaterial({
    address: "demo1sender00000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("sender-signature").toString("base64"),
    shieldedPrefix: "demos"
  });
  const recipient = derivePrivacyMaterial({
    address: "demo1recipient0000000000000000000000000000",
    pubKeyHex: "03".padEnd(66, "0"),
    signatureBase64: Buffer.from("recipient-signature").toString("base64"),
    shieldedPrefix: "demos"
  });

  const payload = await buildPreparedTransferPayload({
    creator: sender.address,
    inputs: [foundNote(4, 1), foundNote(7, 2)],
    recipient: recipient.shieldedAddress,
    amount: "10uclair",
    rootSeed: sender.rootSeed,
    merklePathProvider: () => ({
      root: "11".repeat(32),
      path: [],
      path_helper: []
    }),
    userPrivacyPolicy: "from-to",
    userDisclosureMode: "public",
    auditDisclosureTargetPubKeyHex: sender.disclosurePubKeyHex,
    shieldedPrefix: "demos"
  });
  const disclosure = JSON.parse(Buffer.from(payload.user_disclosure_payload_hex, "hex").toString("utf8"));

  assert.match(disclosure.from_shielded_address, /^demos1/);
  assert.match(disclosure.to_shielded_address, /^demos1/);
});

test("EVM adapter builds deposit transaction calldata and sends through EIP-1193", async () => {
  const sent = [];
  const provider = {
    async request({ method, params }) {
      if (method === "eth_requestAccounts") {
        return ["0x1111111111111111111111111111111111111111"];
      }
      if (method === "eth_sendTransaction") {
        sent.push(params[0]);
        return "0x" + "ab".repeat(32);
      }
      throw new Error(`unexpected method ${method}`);
    }
  };
  const client = createClairveilEvmClient({
    provider,
    contractAddress: "0x2222222222222222222222222222222222222222",
    chainId: "0x539",
    shieldedPrefix: "demos",
    defaultDenom: "udemo"
  });
  const material = derivePrivacyMaterial({
    address: "0x1111111111111111111111111111111111111111",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("evm-signature").toString("base64"),
    shieldedPrefix: "demos"
  });
  const prepared = client.buildDepositTransaction({
    creator: material.address,
    rootSeed: material.rootSeed,
    amount: "9",
    proofHex: "ab"
  });
  const wallet = createEip1193WalletAdapter({ provider });
  const txHash = await client.sendTransaction(wallet, prepared.transaction);

  assert.equal(prepared.material.amount, "9udemo");
  assert.equal(prepared.transaction.to, "0x2222222222222222222222222222222222222222");
  assert.equal(prepared.transaction.data.slice(2, 10), functionSelector("deposit((string,bytes,bytes))"));
  assert.equal(txHash, "0x" + "ab".repeat(32));
  assert.equal(sent[0].from, "0x1111111111111111111111111111111111111111");
});

test("EVM contract adapter allows project-specific calldata encoders", () => {
  const adapter = createEvmContractAdapter({
    contractAddress: "0x3333333333333333333333333333333333333333",
    encodeDeposit: () => "0x1234"
  });
  const tx = adapter.buildDepositTransaction({
    amount: "1uclair",
    noteCommitment: new Uint8Array(32),
    encryptedNote: new Uint8Array([1, 2, 3])
  });

  assert.equal(tx.to, "0x3333333333333333333333333333333333333333");
  assert.equal(tx.data, "0x1234");
});

test("vendored EVM direct transfer and withdraw enforce nullifier preflight", async () => {
  const rootSeed = new Uint8Array(32).fill(9);
  const spendPubKey = deriveSpendKeys(rootSeed).pubKey;
  const viewPubKey = deriveViewKeys(rootSeed).pubKey;
  const recipientMaterial = derivePrivacyMaterial({
    address: "0x1111111111111111111111111111111111111111",
    pubKeyHex: "03".padEnd(66, "0"),
    signatureBase64: Buffer.from("evm-direct-recipient").toString("base64"),
    shieldedPrefix: "demos"
  });
  const makeFoundNote = randomness => ({
    note: createNote({
      spendPubKey,
      viewPubKey,
      amount: 1n,
      assetDenom: "udemo",
      randomness
    }),
    isSpent: false,
    nullifierStatus: "unspent"
  });
  const merklePathProvider = {
    async lookupMerklePath() {
      return { root: "01".padStart(64, "0"), path: [], path_helper: [] };
    }
  };
  const client = createClairveilEvmClient({
    accountPrefix: "demo",
    shieldedPrefix: "demos",
    chainId: "demo-1",
    defaultDenom: "udemo"
  });
  const checked = [];
  const checkNullifiers = async nullifiers => {
    checked.push([...nullifiers]);
    return new Map(nullifiers.map(nullifier => [nullifier, false]));
  };

  const transfer = await client.buildTransferTransaction({
    creator: evmAddressToBech32("0x1111111111111111111111111111111111111111", "demo"),
    inputs: [makeFoundNote(101n), makeFoundNote(102n)],
    recipient: recipientMaterial.shieldedAddress,
    amount: "1udemo",
    rootSeed,
    merklePathProvider,
    auditDisclosureTargetPubKeyHex: recipientMaterial.disclosurePubKeyHex,
    checkNullifiers,
    proverAdapter: {
      async proveTransfer({ payload }) {
        assert.equal(checked.length, 1);
        return { version: "v1", payload_hash: payload.payload_hash, proof_hex: "01" };
      }
    }
  });
  const withdraw = await client.buildWithdrawTransaction({
    notes: [makeFoundNote(103n)],
    amount: "1udemo",
    recipient: "0x2222222222222222222222222222222222222222",
    rootSeed,
    merklePathProvider,
    chainNowUnix: 1_000,
    expiresAtUnix: 2_000,
    checkNullifiers,
    proverAdapter: {
      async proveWithdraw({ payload }) {
        assert.equal(checked.length, 2);
        return { version: "v1", payload_hash: payload.payload_hash, proof_hex: "01" };
      }
    }
  });

  assert.equal(transfer.status, "ready");
  assert.equal(withdraw.status, "ready");
  assert.deepEqual(checked.map(batch => batch.length), [2, 1]);

  for (const failedCheck of [
    async nullifiers => new Map(nullifiers.map(nullifier => [nullifier, true])),
    async () => new Map()
  ]) {
    let proverCalled = false;
    await assert.rejects(
      client.buildTransferTransaction({
        creator: evmAddressToBech32("0x1111111111111111111111111111111111111111", "demo"),
        inputs: [makeFoundNote(201n), makeFoundNote(202n)],
        recipient: recipientMaterial.shieldedAddress,
        amount: "1udemo",
        rootSeed,
        merklePathProvider,
        auditDisclosureTargetPubKeyHex: recipientMaterial.disclosurePubKeyHex,
        checkNullifiers: failedCheck,
        proverAdapter: {
          async proveTransfer() {
            proverCalled = true;
            throw new Error("prover must not run");
          }
        }
      }),
      /nullifier/i
    );
    assert.equal(proverCalled, false);

    await assert.rejects(
      client.buildWithdrawTransaction({
        notes: [makeFoundNote(203n)],
        amount: "1udemo",
        recipient: "0x2222222222222222222222222222222222222222",
        rootSeed,
        merklePathProvider,
        chainNowUnix: 1_000,
        expiresAtUnix: 2_000,
        checkNullifiers: failedCheck,
        proverAdapter: {
          async proveWithdraw() {
            proverCalled = true;
            throw new Error("prover must not run");
          }
        }
      }),
      /nullifier/i
    );
    assert.equal(proverCalled, false);
  }
});

test("withdraw planner requires exact-match notes", () => {
  const exact = planWithdrawNotes({
    amount: "5uclair",
    notes: [foundNote(5, 1), foundNote(9, 2)]
  });
  assert.equal(exact.status, "withdraw_ready");
  assert.equal(exact.selectedNote.note.amount, 5n);

  const needsExact = planWithdrawNotes({
    amount: "5uclair",
    notes: [foundNote(2, 1), foundNote(9, 2)]
  });
  assert.equal(needsExact.status, "exact_note_required");
});

test("MsgWithdraw omits reserved legacy output note fields", () => {
  const payload = {
    version: "v1",
    proof_hex: "aa",
    root_hex: "11".repeat(32),
    nullifier_hex: "22".repeat(32),
    amount: "1uclair",
    recipient: "clair1withdrawrecipient000000000000000000000",
    chain_id: "clairveil-local-3",
    expires_at_unix: Math.floor(Date.now() / 1000) + 600
  };
  payload.payload_hash = computePreparedWithdrawPayloadHash(payload);

  const message = buildWithdrawMsgFromPayload(
    payload,
    "clair1creator000000000000000000000000000000"
  );
  assert.equal("newNoteCommitment" in message, false);
  assert.equal("encryptedNote" in message, false);

  const encoded = MsgWithdraw.encode(message).finish();
  const fields = protobufFieldNumbers(encoded);
  assert.deepEqual([...new Set(fields)].sort((a, b) => a - b), [1, 2, 3, 4, 7, 8, 9, 10]);
  assert.equal(fields.includes(5), false);
  assert.equal(fields.includes(6), false);
});

test("planner errors expose stable error codes", () => {
  const plan = planTransferNotes({ amount: "10uclair", notes: [] });
  assert.equal(plan.status, "insufficient_balance");
  const error = new ClairveilError(ClairveilErrorCode.INSUFFICIENT_BALANCE, plan.message, { plan });
  assert.equal(error.code, ClairveilErrorCode.INSUFFICIENT_BALANCE);
});

test("transfer planner explains missing zero helper notes clearly", () => {
  const plan = planTransferNotes({ amount: "1uclair", notes: [foundNote(1, 1)] });
  assert.equal(plan.status, "zero_dummy_required");
  assert.equal(plan.message, "A second zero-value helper note is required before this transfer can be built.");
});

test("async job prover adapter polls completed transfer jobs", async () => {
  const sender = derivePrivacyMaterial({
    address: "clair1sender000000000000000000000000000000",
    pubKeyHex: "02".padEnd(66, "0"),
    signatureBase64: Buffer.from("sender-signature").toString("base64")
  });
  const recipient = derivePrivacyMaterial({
    address: "clair1recipient0000000000000000000000000000",
    pubKeyHex: "03".padEnd(66, "0"),
    signatureBase64: Buffer.from("recipient-signature").toString("base64")
  });
  const payload = await buildPreparedTransferPayload({
    creator: sender.address,
    inputs: [foundNote(4, 1), foundNote(7, 2)],
    recipient: recipient.shieldedAddress,
    amount: "10uclair",
    rootSeed: sender.rootSeed,
    merklePathProvider: () => ({
      root: "11".repeat(32),
      path: [],
      path_helper: []
    }),
    auditDisclosureTargetPubKeyHex: sender.disclosurePubKeyHex
  });
  const adapter = createAsyncJobProverAdapter({
    submitTransferJob: async () => ({ jobId: "job-1" }),
    submitWithdrawJob: async () => ({ jobId: "job-2" }),
    getJob: async jobId => ({
      status: "completed",
      response: {
        version: "v1",
        proof: {
          version: "v1",
          payload_hash: jobId === "job-1" ? payload.payload_hash : "bb".repeat(32),
          proof_hex: "cc"
        }
      }
    }),
    sleepImpl: async () => {}
  });

  const result = await adapter.proveTransfer({ version: "v1", payload });
  assert.equal(result.proof.payload_hash, payload.payload_hash);
});

test("async job prover adapter rejects unsupported versions before submit", async () => {
  let submitted = false;
  const adapter = createAsyncJobProverAdapter({
    submitTransferJob: async () => {
      submitted = true;
      return { jobId: "job-1" };
    },
    submitWithdrawJob: async () => {
      submitted = true;
      return { jobId: "job-2" };
    },
    getJob: async () => ({ status: "completed", response: {} }),
    sleepImpl: async () => {}
  });

  await assert.rejects(
    adapter.proveTransfer({ version: "v0", payload: { payload_hash: "aa".repeat(32) } }),
    /unsupported transfer proof request version/
  );
  assert.equal(submitted, false);
});

test("browser crypto AES-GCM helpers round-trip root-seed encryption", () => {
  const rootSeed = new Uint8Array(32).fill(9);
  const message = utf8Bytes("clairveil browser crypto");
  const encrypted = encryptWithRootSeed(message, rootSeed);
  const decrypted = decryptWithRootSeed(encrypted, rootSeed);
  assert.equal(utf8String(decrypted), "clairveil browser crypto");
});

test("note store merges scans and marks spent notes", async () => {
  const store = new MemoryNoteStore({ owner: "alice" });
  await store.mergeScanResult({
    foundNotes: [foundNote(5, 1), foundNote(7, 2)]
  });
  let loaded = await store.load();
  assert.equal(loaded.notes.length, 2);
  assert.equal(loaded.lastScannedHeight, 9);
  assert.match(loaded.notes[0].commitment_hex, /^[0-9a-f]{64}$/);
  assert.match(loaded.notes[0].asset_id_hex, /^[0-9a-f]{64}$/);
  assert.equal(loaded.notes[0].asset_denom, "uclair");
  assert.match(loaded.notes[0].randomness_hex, /^[0-9a-f]{64}$/);
  assert.match(loaded.notes[0].spend_pubkey_hex, /^[0-9a-f]{64}$/);
  assert.match(loaded.notes[0].view_pubkey_hex, /^[0-9a-f]{64}$/);
  assert.equal(loaded.notes[0].tx_hash, loaded.notes[0].txHash);
  assert.equal(loaded.notes[0].spent, false);

  await store.markSpent(loaded.notes[0].nullifier);
  loaded = await store.load();
  assert.equal(loaded.notes.filter(note => note.isSpent).length, 1);
});

test("note store tracks scan cursor, rollback metadata, and localStorage plaintext opt-in", async () => {
  const store = new MemoryNoteStore({ owner: "alice" });
  await store.mergeScanResult({
    foundNotes: [foundNote(5, 1), foundNote(7, 2)],
    scanCursor: {
      after_height: 0,
      after_sequence: 78,
      page: 1,
      limit: 50,
      event_types: ["deposit", "shielded_transfer"],
      next_height: 200,
      next_sequence: 78,
      latest_height: 9,
      latest_tx_hash: "AA02"
    }
  });
  let loaded = await store.load();
  assert.equal(loaded.lastScannedHeight, 200);
  assert.equal(loaded.lastScannedSequence, 78);
  assert.equal(loaded.lastScannedTxHash, "AA02");
  assert.deepEqual(loaded.scanCursor.event_types, ["deposit", "shielded_transfer"]);

  loaded = await store.rollbackToHeight(6);
  assert.equal(loaded.notes.length, 0);
  assert.equal(loaded.rollbackHeight, 6);
  assert.equal(loaded.lastScannedSequence, 0);
  assert.equal(loaded.lastScannedTxHash, "");
  assert.equal(loaded.scanCursor.source, "scan_events");
  assert.equal(loaded.scanCursor.after_height, 6);
  assert.equal(loaded.scanCursor.after_sequence, 0);

  const storage = new Map();
  const storageLike = {
    getItem: key => storage.get(key) ?? null,
    setItem: (key, value) => storage.set(key, value),
    removeItem: key => storage.delete(key)
  };
  assert.throws(
    () => new LocalStorageNoteStore({ storage: storageLike, key: "notes" }),
    /plaintext/
  );
  assert.ok(new LocalStorageNoteStore({ storage: storageLike, key: "notes", allowPlaintext: true }));
});

test("legacy external ClairveilJS scanner fails closed on privacy-fixed-v1 deposit fixtures", async () => {
  const fixture = JSON.parse(await readFile(
    new URL("../../../x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json", import.meta.url),
    "utf8"
  ));
  const result = await scanNotes({
    rootSeed: Buffer.from(fixture.sender_root_seed.root_seed_hex, "hex"),
    events: [
      {
        event_type: "deposit",
        height: fixture.scan.height,
        tx_hash_hex: fixture.scan.tx_hash_hex,
        attributes: [
          { key: "encrypted_note", value: fixture.note.encrypted_note_hex },
          { key: "commitment", value: fixture.note.commitment_hex }
        ]
      }
    ],
    checkNullifier: async () => ({ used: false })
  });

  // The npm/GitHub ClairveilJS dependency is downstream of this repository.
  // The NoteV1 contract freezes its downstream handoff but does not implement that external
  // SDK upgrade; accepting the old JSON/raw-ciphertext contract would be an
  // unsafe compatibility fallback.
  assert.equal(result.notes.length, 0);
  assert.equal(result.summary.spendable_count, 0);
});

test("ClairveilJS scanNotes sends scan event cursor query parameters", async () => {
  const originalFetch = globalThis.fetch;
  const seen = [];
  globalThis.fetch = async url => {
    seen.push(String(url));
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      async json() {
        return {
          events: [],
          scan_format_version: 1,
          view_tag_version: 1,
          next_height: 5,
          next_sequence: 0,
          limit: 50,
          has_more: false
        };
      }
    };
  };

  try {
    const client = createClairveilClient({
      rpc: "http://127.0.0.1:26657",
      rest: "http://example.test",
      chainId: "clairveil-local-3"
    });
    const result = await client.scanNotes({
      rootSeed: new Uint8Array(32),
      afterHeight: 5,
      page: 2,
      limit: 50,
      eventTypes: ["deposit", "shielded_transfer"]
    });
    const url = new URL(seen[0]);
    assert.equal(url.pathname, "/clairveil/privacy/v1/scan_events");
    assert.equal(url.searchParams.get("after_height"), "5");
    assert.equal(url.searchParams.get("after_sequence"), "0");
    assert.equal(url.searchParams.get("limit"), "50");
    assert.deepEqual(url.searchParams.getAll("event_types"), ["deposit", "shielded_transfer"]);
    assert.equal(result.scanCursor.after_height, 5);
    assert.equal(result.scanCursor.after_sequence, 0);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("decodeUserDisclosureFromEvent supports public disclosure payloads", () => {
  const digestHex = canonicalFieldHex(0n);
  const publicPayload = {
    version: "v4",
    plane: "user",
    policy: 0,
    output_index: 0,
    commitment_hex: canonicalFieldHex(123n),
    disclosure_digest_hex: digestHex
  };
  const report = decodeUserDisclosureFromEvent({
    event_type: "shielded_transfer",
    tx_hash_hex: "aa",
    attributes: [
      { key: "user_disclosure_mode", value: userDisclosureModePublic },
      { key: "user_disclosure_payload", value: payloadHex(publicPayload) },
      { key: "user_disclosure_digest", value: digestHex }
    ]
  }, 1n, "ff".repeat(32));

  assert.equal(report.source, "public");
  assert.equal(report.summary.delivery, "public");
  assert.equal(report.verification.verified, true);
});

test("ClairveilJS.decodeUserDisclosure decodes public payloads without signer material", async () => {
  const publicPayload = {
    version: "v4",
    plane: "user",
    policy: 1,
    output_index: 0,
    commitment_hex: canonicalFieldHex(456n),
    disclosure_digest_hex: "",
    amount: "3",
    asset_id_hex: canonicalFieldHex(assetID),
    asset_denom: "uclair"
  };
  const digestHex = computeExpectedDisclosureDigestHex(publicPayload);
  publicPayload.disclosure_digest_hex = digestHex;
  const client = createClairveilClient({
    rest: "http://127.0.0.1:1",
    rpc: "http://127.0.0.1:2",
    chainId: "clairveil-test"
  });
  client.findPrivacyEventByTxHash = async txHash => ({
    event_type: "shielded_transfer",
    tx_hash_hex: txHash,
    attributes: [
      { key: "user_disclosure_mode", value: userDisclosureModePublic },
      { key: "user_disclosure_payload", value: payloadHex(publicPayload) },
      { key: "user_disclosure_digest", value: digestHex }
    ]
  });

  const report = await client.decodeUserDisclosure({ txHash: "aa" });

  assert.equal(report.source, "public");
  assert.equal(report.summary.delivery, "public");
  assert.equal(report.summary.amount, "3");
  assert.equal(report.verification.verified, true);
});

test("ClairveilJS.decodeUserDisclosure can skip signer pubkey checks for EVM identity material", async () => {
  const client = createClairveilClient({
    rest: "http://127.0.0.1:1",
    rpc: "http://127.0.0.1:2",
    chainId: "evm-test",
    accountPrefix: "demo"
  });
  client.findPrivacyEventByTxHash = async txHash => ({
    event_type: "shielded_transfer",
    tx_hash_hex: txHash,
    attributes: [
      { key: "user_disclosure_mode", value: userDisclosureModeRecipientEncrypted },
      { key: "user_disclosure_target_pubkey", value: "ab".repeat(32) }
    ]
  });
  const input = {
    txHash: "aa",
    address: "demo1rcrtmxgycp0vgukkvkm7v49kyed6grpn4w49lx",
    pubKeyHex: "11".repeat(20),
    signatureBase64: "AQID"
  };

  await assert.rejects(
    () => client.decodeUserDisclosure(input),
    /signer address\/pubKey mismatch/
  );
  await assert.rejects(
    () => client.decodeUserDisclosure({ ...input, skipSignerPubKeyCheck: true }),
    /selected transfer has no user disclosure/
  );
});

test("schemas validate disclosure public keys", () => {
  assert.equal(assertDisclosurePubKeyHex("ab".repeat(32)), "ab".repeat(32));
  assert.throws(() => assertDisclosurePubKeyHex("ab"), /32-byte/);
});
