import test from "node:test";
import assert from "node:assert/strict";
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
  functionSelector
} from "clairveiljs/evm";
import { utf8Bytes, utf8String } from "clairveiljs/browser-crypto";

const assetID = hashStringToField("uclair");

function foundNote(amount, suffix, overrides = {}) {
  return {
    height: Number(amount) + suffix,
    txHash: `AA${String(suffix).padStart(2, "0")}`,
    isSpent: false,
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
    amount: "9"
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
  const plan = planTransferNotes({ amount: "0uclair", notes: [] });
  assert.equal(plan.status, "insufficient_balance");
  assert.equal(plan.message, "No zero note is available; a 0uclair helper deposit is required.");
});

test("async job prover adapter polls completed transfer jobs", async () => {
  const payload = {
    payload_hash: "aa".repeat(32)
  };
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
      page: 1,
      limit: 50,
      event_types: ["deposit", "shielded_transfer"],
      latest_height: 9,
      latest_tx_hash: "AA02"
    }
  });
  let loaded = await store.load();
  assert.equal(loaded.lastScannedHeight, 9);
  assert.equal(loaded.lastScannedTxHash, "AA02");
  assert.deepEqual(loaded.scanCursor.event_types, ["deposit", "shielded_transfer"]);

  loaded = await store.rollbackToHeight(6);
  assert.equal(loaded.notes.length, 1);
  assert.equal(loaded.rollbackHeight, 6);

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

test("scanNotes reads Go-compatible deposit encrypted note fixture", async () => {
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
          { key: "note_commitment", value: fixture.note.commitment_hex }
        ]
      }
    ]
  });

  assert.equal(result.notes.length, 1);
  assert.equal(result.summary.spendable_count, 1);
  assert.equal(result.notes[0].amount, fixture.note.amount);
});

test("ClairveilJS scanNotes sends paginated event feed query parameters", async () => {
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
          page: 2,
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
    assert.equal(url.pathname, "/clairveil/privacy/v1/events");
    assert.equal(url.searchParams.get("after_height"), "5");
    assert.equal(url.searchParams.get("page"), "2");
    assert.equal(url.searchParams.get("limit"), "50");
    assert.deepEqual(url.searchParams.getAll("event_types"), ["deposit", "shielded_transfer"]);
    assert.equal(result.scanCursor.after_height, 5);
    assert.equal(result.scanCursor.page, 2);
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
