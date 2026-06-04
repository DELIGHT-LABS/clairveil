import assert from "node:assert/strict";
import { Buffer } from "node:buffer";
import { describe, it } from "node:test";

import {
  buildPrivacyRootSigningMessage,
  keypairFromPrivateKeyHex,
  keypairFromPrivacyRootMaterial,
  keypairFromPrivacyRootSigner,
  keypairFromSeedHex,
  randomKeypair,
} from "./audit-disclosure-keys.mjs";

describe("audit disclosure key generation", () => {
  it("matches Clairveil/gnark compressed pubkey vectors", () => {
    assert.deepEqual(keypairFromPrivateKeyHex("01".padStart(64, "0")), {
      privateKeyHex:
        "0000000000000000000000000000000000000000000000000000000000000001",
      publicKeyHex:
        "8b7d2d877a253c4b7733e1b91f05e0fcedf96bd11c2e572549b2a0f703727925",
      publicKeyGenesisBase64: "i30th3olPEt3M+G5HwXg/O35a9EcLlclSbKg9wNyeSU=",
    });

    assert.equal(
      keypairFromPrivateKeyHex("02".padStart(64, "0")).publicKeyHex,
      "53686d2b4005178e1843106f2992a867a01d8a84afbe9e8bda300abfaf6c6601",
    );

    assert.equal(
      keypairFromPrivateKeyHex("2a".padStart(64, "0")).publicKeyHex,
      "9c5450e237531487d332ca97ff2670ba9300d87bf9e3466e6392db1801714aa4",
    );
  });

  it("derives the same keypair from the same seed", () => {
    const seed = "deadbeef".padStart(64, "0");

    assert.deepEqual(keypairFromSeedHex(seed), keypairFromSeedHex(seed));
    assert.deepEqual(keypairFromSeedHex(seed), {
      privateKeyHex:
        "044c59b7fd11a558ebaa2b556aa93491bb8233804f019bba2d2d83bcbd8ff4a9",
      publicKeyHex:
        "bfb4e7f03a55be8198e05bc1a05dd5b23ee03b428a9d78217c1ee7710434630b",
      publicKeyGenesisBase64: "v7Tn8DpVvoGY4FvBoF3Vsj7gO0KKnXghfB7ncQQ0Yws=",
    });
  });

  it("matches Clairveil privacy root material derivation", async () => {
    const material = {
      address: "clair1auditor",
      transparentPubKeyHex: "010203",
      signatureHex: "040506",
    };

    assert.equal(
      buildPrivacyRootSigningMessage(material).toString("hex"),
      "636c6169727665696c2d726f6f742d76310a616464726573733a636c6169723161756469746f720a7075626b65793a303130323033",
    );

    const expected = {
      privateKeyHex:
        "0440c15a3d8cf2d4876b8a56a31b04265832e88cb43d158a61328c227049e037",
      publicKeyHex:
        "97d966b089a78c3422a12cb175874df5ec4c0374fcf90cdaed796e7b70f10e8f",
      publicKeyGenesisBase64: "l9lmsImnjDQioSyxdYdN9exMA3T8+Qza7Xlue3DxDo8=",
    };

    assert.deepEqual(keypairFromPrivacyRootMaterial(material), expected);

    const signer = {
      address: material.address,
      pubKeyBytes: Buffer.from(material.transparentPubKeyHex, "hex"),
      signPrivacyRoot: async (messageBytes) => {
        assert.equal(
          messageBytes.toString("hex"),
          buildPrivacyRootSigningMessage(material).toString("hex"),
        );
        return Buffer.from(material.signatureHex, "hex");
      },
    };

    assert.deepEqual(await keypairFromPrivacyRootSigner(signer), expected);
  });

  it("generates random keypairs that can be recomputed from their private key", () => {
    const generated = randomKeypair();
    const recomputed = keypairFromPrivateKeyHex(generated.privateKeyHex);

    assert.deepEqual(generated, recomputed);
    assert.match(generated.privateKeyHex, /^[0-9a-f]{64}$/);
    assert.match(generated.publicKeyHex, /^[0-9a-f]{64}$/);
    assert.equal(
      Buffer.from(generated.publicKeyGenesisBase64, "base64").toString("hex"),
      generated.publicKeyHex,
    );
  });

  it("rejects invalid private scalars", () => {
    assert.throws(
      () => keypairFromPrivateKeyHex("00".padStart(64, "0")),
      /private scalar must satisfy/,
    );

    assert.throws(() => keypairFromPrivateKeyHex("not-hex"), /value must be hex/);
  });
});
