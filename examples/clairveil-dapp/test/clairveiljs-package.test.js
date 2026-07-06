import test from "node:test";
import assert from "node:assert/strict";

test("package export map exposes public SDK entrypoints", async () => {
  const sdk = await import("clairveiljs");
  for (const name of [
    "createClairveilClient",
    "createWalletAdapter",
    "createOfflineSignerWalletAdapter",
    "createHttpProverAdapter",
    "createAsyncJobProverAdapter",
    "ClairveilError",
    "ClairveilErrorCode",
    "MemoryNoteStore",
    "buildRelayWithdrawMsgFromPayload",
    "buildRelayWithdrawPayload",
    "validateRelayWithdrawPayload"
  ]) {
    assert.equal(typeof sdk[name], name === "ClairveilErrorCode" ? "object" : "function", `${name} export`);
  }
});

test("package subpath exports are available", async () => {
  const core = await import("clairveiljs/core");
  const cosmos = await import("clairveiljs/cosmos");
  const cosmosClient = await import("clairveiljs/cosmos-client");
  const evm = await import("clairveiljs/evm");
  const crypto = await import("clairveiljs/browser-crypto");
  const browserDapp = await import("clairveiljs/browser-dapp");
  const planner = await import("clairveiljs/planner");
  const payload = await import("clairveiljs/payload");
  const prover = await import("clairveiljs/prover");
  const tx = await import("clairveiljs/generated/clairveil/privacy/v1/tx");

  assert.equal(typeof core.derivePrivacyMaterial, "function");
  assert.equal(typeof cosmos.createClairveilClient, "function");
  assert.equal(typeof cosmosClient.createClairveilClient, "function");
  assert.equal(typeof evm.createClairveilEvmClient, "function");
  assert.equal(typeof crypto.sha256Hex, "function");
  assert.equal(typeof browserDapp.createClairveilBrowserDappClient, "function");
  assert.equal(typeof browserDapp.ClairveilBrowserDappClient.prototype.prepareRelayWithdraw, "function");
  assert.equal(typeof browserDapp.ClairveilBrowserDappClient.prototype.createRelayWithdrawSignDoc, "function");
  assert.equal(typeof browserDapp.ClairveilBrowserDappClient.prototype.buildRelayWithdrawMessageFromPayload, "function");
  assert.equal(typeof planner.planTransferNotes, "function");
  assert.equal(typeof payload.buildRelayWithdrawMsgFromPayload, "function");
  assert.equal(typeof payload.buildRelayWithdrawPayload, "function");
  assert.equal(typeof payload.validateRelayWithdrawPayload, "function");
  assert.equal(typeof prover.createAsyncJobProverAdapter, "function");
  assert.equal(typeof tx.MsgDeposit.encode, "function");
  assert.equal(typeof tx.MsgTransfer.decode, "function");
  assert.equal(tx.MsgWithdraw.typeUrl, "/clairveil.privacy.v1.MsgWithdraw");
});

test("generated pagination helper is browser friendly without Buffer", async () => {
  const { setPaginationParams } = await import("clairveiljs/generated/helpers");
  const originalBuffer = globalThis.Buffer;
  try {
    globalThis.Buffer = undefined;
    const options = { params: {} };
    setPaginationParams(options, { key: Uint8Array.from([1, 2, 3]) });
    assert.equal(options.params["pagination.key"], "AQID");
  } finally {
    globalThis.Buffer = originalBuffer;
  }
});
