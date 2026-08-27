import test from "node:test";
import assert from "node:assert/strict";

import {
  assertPreparedTransferFreshAtChainTime,
  authoritativeChainBlockFromStatus,
  isSeparateSameOriginLocalRelayerReservation,
  preparedTransferExpiryUnix,
  recoveredDepositNoteForTxHash,
  reservationConsumesBrowserCosmosSequence,
  typedPrivacyScanAfter
} from "../public/cosmos-flow-state.js";

function preparedTransfer(expiry = 2_000) {
  return { prepared: { payload: { expires_at_unix: expiry } } };
}

function chainStatus(network = "clairveil-local-1") {
  return {
    result: {
      node_info: { network },
      sync_info: {
        latest_block_time: "2026-08-27T01:02:03.900Z",
        latest_block_height: "42"
      }
    }
  };
}

test("authoritative block time is bound to the active Cosmos host chain", () => {
  assert.deepEqual(
    authoritativeChainBlockFromStatus(chainStatus(), { chainId: "clairveil-local-1" }),
    { timeUnix: 1_787_792_523, height: 42 }
  );
  assert.throws(
    () => authoritativeChainBlockFromStatus(chainStatus("other-chain-1"), {
      chainId: "clairveil-local-1"
    }),
    error => error?.code === "CHAIN_STATUS_NETWORK_MISMATCH"
  );

  const evmProfile = {
    transport: "evm",
    chainId: "clairveil-evm-host-1",
    evmChainId: "0x539"
  };
  assert.equal(
    authoritativeChainBlockFromStatus(chainStatus("clairveil-evm-host-1"), evmProfile).height,
    42
  );
  assert.throws(
    () => authoritativeChainBlockFromStatus(chainStatus("0x539"), evmProfile),
    error => error?.code === "CHAIN_STATUS_NETWORK_MISMATCH"
  );
});

test("typed privacy scans always carry an explicit typed cursor", () => {
  assert.deepEqual(typedPrivacyScanAfter(), {
    height: 0,
    globalSequence: 0,
    outputIndex: 0
  });
  assert.deepEqual(typedPrivacyScanAfter({
    source: "privacy_scan",
    next_cursor: { height: "42", global_sequence: "7", output_index: 1 },
    has_more: false
  }), {
    height: "42",
    global_sequence: "7",
    output_index: 1
  });
  assert.deepEqual(typedPrivacyScanAfter({
    after: { height: 8, globalSequence: 3, outputIndex: 2 }
  }), {
    height: 8,
    globalSequence: 3,
    outputIndex: 2
  });
});

test("prepared transfer confirmation uses and canonically validates the actual payload expiry", () => {
  assert.equal(preparedTransferExpiryUnix(preparedTransfer("2000")), 2_000);
  assert.equal(preparedTransferExpiryUnix(preparedTransfer(2_001)), 2_001);
  assert.throws(
    () => preparedTransferExpiryUnix({ prepared: { payload: { expiresAtUnix: 2_000 } } }),
    error => error?.code === "INVALID_TRANSFER_EXPIRY"
  );
  assert.throws(
    () => preparedTransferExpiryUnix(preparedTransfer("02000")),
    error => error?.code === "INVALID_TRANSFER_EXPIRY"
  );
  assert.throws(
    () => preparedTransferExpiryUnix(preparedTransfer(" 2000")),
    error => error?.code === "INVALID_TRANSFER_EXPIRY"
  );
});

test("prepared Cosmos transfer freshness rejects the exact expiry boundary", () => {
  assert.equal(
    assertPreparedTransferFreshAtChainTime(preparedTransfer(2_000), {
      chainNowUnix: 1_999
    }),
    2_000
  );
  assert.throws(
    () => assertPreparedTransferFreshAtChainTime(preparedTransfer(2_000), {
      chainNowUnix: 2_000
    }),
    error => error?.code === "TRANSFER_PAYLOAD_EXPIRED_BEFORE_BROADCAST"
      && error.message.includes("authoritative chain time is 2000")
  );
});

test("deposit reload recovery matches only the exact typed-scan transaction hash", () => {
  const txHash = "AB".repeat(32);
  const exact = { tx_hash: txHash.toLowerCase(), commitment_hex: "01".repeat(32) };
  const other = { txHash: "CD".repeat(32), commitment_hex: "02".repeat(32) };
  assert.equal(recoveredDepositNoteForTxHash([other, exact], `0x${txHash}`), exact);
  assert.equal(recoveredDepositNoteForTxHash([other], txHash), null);
  assert.throws(
    () => recoveredDepositNoteForTxHash([exact], "not-a-hash"),
    error => error?.code === "INVALID_DEPOSIT_TX_HASH"
  );
});

test("only a persisted separate same-origin server relayer bypasses the browser sequence fence", () => {
  const browserAccount = "clair1wallet";
  const localRelayer = { name: "relayer", transparentAddress: "clair1serverrelayer" };
  const localRelayRecord = {
    kind: "relay_withdraw",
    status: "Unknown",
    submitted_tx_hash: "AB".repeat(32),
    metadata: {
      broadcast_attempt_reason: "same_origin_local_relayer_submit",
      local_relayer: "relayer",
      local_relayer_address: "clair1serverrelayer",
      relay_handed_off: false
    }
  };
  const context = { browserAccount, localRelayer };

  assert.equal(isSeparateSameOriginLocalRelayerReservation(localRelayRecord, context), true);
  assert.equal(reservationConsumesBrowserCosmosSequence(localRelayRecord, context), false);

  for (const record of [
    { ...localRelayRecord, kind: "withdraw" },
    { ...localRelayRecord, metadata: { ...localRelayRecord.metadata, relay_handed_off: true } },
    { ...localRelayRecord, metadata: { ...localRelayRecord.metadata, local_relayer_address: "" } },
    { ...localRelayRecord, metadata: { ...localRelayRecord.metadata, broadcast_attempt_reason: "external_broadcast_boundary_crossed" } },
    { ...localRelayRecord, metadata: { ...localRelayRecord.metadata, local_relayer_address: browserAccount } }
  ]) {
    assert.equal(isSeparateSameOriginLocalRelayerReservation(record, context), false);
    assert.equal(reservationConsumesBrowserCosmosSequence(record, context), true);
  }

  assert.equal(
    reservationConsumesBrowserCosmosSequence(localRelayRecord, {
      browserAccount,
      localRelayer: { ...localRelayer, transparentAddress: "clair1changed" }
    }),
    true
  );
});
