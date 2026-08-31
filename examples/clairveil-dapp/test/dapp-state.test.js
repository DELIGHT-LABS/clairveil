import test from "node:test";
import assert from "node:assert/strict";
import { webcrypto } from "node:crypto";
import { indexedDB } from "fake-indexeddb";
import { MsgWithdraw, msgWithdrawTypeUrl } from "clairveiljs/cosmos-client";
import { validateBrowserWalletProfile } from "clairveiljs/browser-dapp";
import { hashAmount } from "clairveiljs/reservation";
import { computePreparedWithdrawPayloadHash } from "clairveiljs/payload";
import {
  browserLoopbackRewriteEnabled,
  normalizeBrowserEndpointUrl,
  normalizeBrowserProfileEndpoints,
  normalizeBrowserRestEndpoints
} from "../public/browser-profile.js";
import { localChainStorageEpoch, walletStorageScope } from "../public/browser-storage-scope.js";
import { keplrDirectSignOptions } from "../public/cosmos-sign-options.js";
import {
  assessReservationRecovery,
  canReconcileReservationState,
  canResetStaleLocalGenesisReservations,
  groupReservationOperations,
  isEmptyLocalGenesisPrivacyState,
  reconciliationReservationRecords,
  succeededOperationLookupKeys
} from "../public/reservation-recovery.js";
import {
  createEncryptedBrowserReservationManager,
  resetEncryptedBrowserReservationState
} from "../public/encrypted-reservation-manager.js";
import { getStaticDappConfig } from "../public/dapp-config.js";
import { assertDepositFundingAvailable } from "../public/deposit-funding.js";
import { cosmosChargedFeeAmount, evmChargedFeeAmount } from "../public/network-fee.js";
import {
  EncryptedLocalStorageOperationStore,
  relayWithdrawRecoveryMetadata,
  relayWithdrawRecoveryPersistenceId,
  relayWithdrawRecoveryVersion,
  restoreRelayWithdrawRecoveryMetadata
} from "../public/encrypted-operation-store.js";
import { disclosureViewModel } from "../public/disclosure-view-model.js";
import {
  findPrivacyEventByTxHash,
  reservationPrivacyEventTypes
} from "../public/operation-event-lookup.js";
import {
  loadPrivacyPendingTxState,
  loadPublicPendingTxState,
  privacyPendingTxKey,
  publicPendingTxKey,
  savePrivacyPendingTxState,
  savePublicPendingTxState
} from "../public/public-pending-tx-store.js";
import {
  assertCosmosRelayWithdrawTransactionPayloadHash,
  assertRelayReservationPayloadMatches,
  assertRelayWithdrawTransactionMatches,
  createRelayWithdrawHandoff,
  relayWithdrawHandoffPayload,
  relayWithdrawExpiryLeaseUntil,
  relayWithdrawPayloadExpired
} from "../public/relay-withdraw-reconciliation.js";
import {
  cosmosWithdrawOperationEvidence,
  hashTransparentCosmosRecipient
} from "../public/withdraw-operation-evidence.js";

class MemoryStorage {
  constructor() {
    this.values = new Map();
  }

  getItem(key) {
    return this.values.get(key) ?? null;
  }

  setItem(key, value) {
    this.values.set(key, String(value));
  }

  removeItem(key) {
    this.values.delete(key);
  }

  get length() {
    return this.values.size;
  }

  key(index) {
    return [...this.values.keys()][index] ?? null;
  }
}

function reservationNoteFixture({
  nullifier = "11".repeat(32),
  sequence = 1
} = {}) {
  return {
    note: {
      receiverSpendPubKeyX: 1n,
      receiverSpendPubKeyY: 2n,
      receiverViewPubKeyX: 3n,
      receiverViewPubKeyY: 4n,
      amount: 5n,
      assetID: 7n,
      randomness: 8n,
      memo: ""
    },
    nullifier,
    isSpent: false,
    nullifierStatus: "unspent",
    txHash: "AB".repeat(32),
    height: 10,
    sequence
  };
}

class MemoryLocks {
  async request(_name, _options, callback) {
    return callback();
  }
}

function hexBytes(value) {
  return Uint8Array.from(String(value).match(/../g).map(byte => Number.parseInt(byte, 16)));
}

function protobufVarint(value) {
  let remaining = BigInt(value);
  const bytes = [];
  while (remaining >= 0x80n) {
    bytes.push(Number((remaining & 0x7fn) | 0x80n));
    remaining >>= 7n;
  }
  bytes.push(Number(remaining));
  return Uint8Array.from(bytes);
}

function concatBytes(...values) {
  const result = new Uint8Array(values.reduce((sum, value) => sum + value.length, 0));
  let offset = 0;
  for (const value of values) {
    result.set(value, offset);
    offset += value.length;
  }
  return result;
}

function protobufBytesField(fieldNumber, value) {
  const bytes = value instanceof Uint8Array ? value : new TextEncoder().encode(String(value));
  return concatBytes(protobufVarint((BigInt(fieldNumber) << 3n) | 2n), protobufVarint(bytes.length), bytes);
}

function cosmosWithdrawTx(payload, overrides = {}) {
  const message = MsgWithdraw.fromPartial({
    creator: "clair1relayer",
    proof: hexBytes(payload.proof_hex),
    root: hexBytes(payload.root_hex),
    nullifier: hexBytes(payload.nullifier_hex),
    amount: payload.amount,
    recipient: payload.recipient,
    chainId: payload.chain_id,
    expiresAtUnix: BigInt(payload.expires_at_unix),
    ...overrides
  });
  const any = concatBytes(
    protobufBytesField(1, msgWithdrawTypeUrl),
    protobufBytesField(2, MsgWithdraw.encode(message).finish())
  );
  const bodyBytes = protobufBytesField(1, any);
  return protobufBytesField(1, bodyBytes);
}

test("browser endpoint rewriting keeps the Keplr chain profile on one RPC and REST", () => {
  const source = getStaticDappConfig().chainProfiles[0];
  const profile = normalizeBrowserProfileEndpoints(source, {
    rpc: "http://localhost:26657",
    rest: "http://localhost:1317",
    proverUrl: "http://localhost:8080"
  });

  assert.equal(profile.rpc, "http://localhost:26657");
  assert.equal(profile.keplrChainInfo.rpc, profile.rpc);
  assert.equal(profile.rest, "http://localhost:1317");
  assert.equal(profile.keplrChainInfo.rest, profile.rest);
  assert.equal(source.rpc, "http://127.0.0.1:26657");
  assert.doesNotThrow(() => validateBrowserWalletProfile(profile));
});

test("LAN browser profiles rewrite and deduplicate every REST failover endpoint", () => {
  const source = {
    ...getStaticDappConfig().chainProfiles[0],
    rest: "http://127.0.0.1:1317",
    restEndpoints: [
      "http://localhost:1317/",
      "http://127.0.0.1:2317",
      "http://192.168.1.20:2317/"
    ]
  };
  const restEndpoints = normalizeBrowserRestEndpoints(source, {
    browserHostname: "192.168.1.20",
    selectedEndpoint: "http://localhost:2317/",
    localTestMode: true
  });
  assert.deepEqual(restEndpoints, [
    "http://192.168.1.20:2317",
    "http://192.168.1.20:1317"
  ]);

  const profile = normalizeBrowserProfileEndpoints(source, {
    rpc: "http://192.168.1.20:26657",
    rest: restEndpoints[0],
    restEndpoints,
    proverUrl: "http://192.168.1.20:8080"
  });
  assert.equal(profile.rest, restEndpoints[0]);
  assert.deepEqual(profile.restEndpoints, restEndpoints);
  assert.equal(profile.keplrChainInfo.rest, profile.rest);
  assert.doesNotThrow(() => validateBrowserWalletProfile(profile));
});

test("browser endpoints preserve the validated origin outside local-test mode", () => {
  const configured = "https://localhost:8443/v1/proofs/transfer";
  assert.equal(normalizeBrowserEndpointUrl(configured, {
    browserHostname: "wallet.example.com"
  }), configured);
  assert.equal(normalizeBrowserEndpointUrl(configured, {
    browserHostname: "wallet.example.com",
    localTestMode: true
  }), "https://wallet.example.com:8443/v1/proofs/transfer");

  const profile = {
    rest: "https://127.0.0.1:1317",
    restEndpoints: ["https://localhost:2317"]
  };
  assert.deepEqual(normalizeBrowserRestEndpoints(profile, {
    browserHostname: "wallet.example.com"
  }), [
    "https://127.0.0.1:1317",
    "https://localhost:2317"
  ]);

  assert.equal(browserLoopbackRewriteEnabled({
    serverBacked: true,
    localTestMode: true,
    serverFeatures: { localTestMode: true }
  }), true);
  for (const config of [
    { serverBacked: false, localTestMode: true, serverFeatures: { localTestMode: true } },
    { serverBacked: true, localTestMode: false, serverFeatures: { localTestMode: true } },
    { serverBacked: true, localTestMode: true, serverFeatures: { localTestMode: false } }
  ]) {
    assert.equal(browserLoopbackRewriteEnabled(config), false);
  }
});

test("local browser state is isolated by the current genesis block hash", () => {
  const firstEpoch = localChainStorageEpoch({
    localTestMode: true,
    status: { sync_info: { earliest_block_hash: "AA".repeat(32) } }
  });
  const secondEpoch = localChainStorageEpoch({
    localTestMode: true,
    status: { sync_info: { earliest_block_hash: "BB".repeat(32) } }
  });
  const identity = {
    chainId: "clairveil-local-2",
    profileId: "clairveil-local",
    owner: "CLAIR1OWNER",
    localTestMode: true
  };

  assert.equal(firstEpoch, "aa".repeat(32));
  assert.notEqual(firstEpoch, secondEpoch);
  assert.notDeepEqual(
    walletStorageScope({ ...identity, storageEpoch: firstEpoch }),
    walletStorageScope({ ...identity, storageEpoch: secondEpoch })
  );
  assert.equal(walletStorageScope({ ...identity, storageEpoch: "" }), null);
  assert.equal(localChainStorageEpoch({
    localTestMode: false,
    status: { sync_info: { earliest_block_hash: "AA".repeat(32) } }
  }), "");
});

test("Keplr preserves ProofReady Cosmos sign docs but can price deposits", () => {
  assert.deepEqual(keplrDirectSignOptions({}), {
    preferNoSetFee: false,
    preferNoSetMemo: true
  });
  assert.deepEqual(keplrDirectSignOptions({
    reservation: { reservation_ids: ["reservation-1"] }
  }), {
    preferNoSetFee: true,
    preferNoSetMemo: true
  });
  assert.deepEqual(keplrDirectSignOptions({
    reservation_batch: { reservationIds: ["reservation-2"] }
  }), {
    preferNoSetFee: true,
    preferNoSetMemo: true
  });
});

test("reservation recovery groups linked inputs and only offers no-broadcast direct preparations", () => {
  const nowMs = Date.parse("2026-08-04T04:00:00.000Z");
  const records = [
    {
      reservation_id: "reservation-2",
      operation_id: "operation-1",
      status: "ProofReady",
      kind: "transfer",
      lease_owner: "browser-tab:one",
      lease_token: "lease-token",
      lease_until: "2026-08-04T04:05:00.000Z",
      broadcast_attempt_count: 0,
      metadata: { no_broadcast_attempt: true }
    },
    {
      reservation_id: "reservation-1",
      operation_id: "operation-1",
      status: "ProofReady",
      kind: "transfer",
      lease_owner: "browser-tab:one",
      lease_token: "lease-token",
      lease_until: "2026-08-04T04:05:00.000Z",
      broadcast_attempt_count: 0,
      metadata: { no_broadcast_attempt: true }
    }
  ];

  const operations = groupReservationOperations(records);
  assert.equal(operations.length, 1);
  assert.deepEqual(operations[0].records.map(record => record.reservation_id), [
    "reservation-1",
    "reservation-2"
  ]);

  const assessment = assessReservationRecovery(operations[0].records, {
    leaseOwner: "browser-tab:one",
    nowMs
  });
  assert.equal(assessment.action, "review-replan");
  assert.equal(assessment.leaseOwnedByCurrentWorker, true);
  assert.equal(assessment.leaseToken, "lease-token");
  const signDocOnly = assessReservationRecovery([{
    ...records[0],
    sign_doc_hash: "ab".repeat(32)
  }], {
    leaseOwner: "browser-tab:one",
    nowMs
  });
  assert.equal(signDocOnly.action, "review-replan");
  assert.equal(signDocOnly.signDocOnly, true);
  assert.equal(signDocOnly.hasQueryableTransactionIdentity, false);
});

test("reservation recovery fails closed for broadcast, relay, and foreign live lease evidence", () => {
  const base = {
    reservation_id: "reservation-1",
    operation_id: "operation-1",
    status: "ProofReady",
    kind: "transfer",
    lease_owner: "browser-tab:one",
    lease_token: "lease-token",
    lease_until: "2026-08-04T04:05:00.000Z",
    metadata: { no_broadcast_attempt: true }
  };
  const options = {
    leaseOwner: "browser-tab:two",
    nowMs: Date.parse("2026-08-04T04:00:00.000Z")
  };

  assert.equal(assessReservationRecovery([base], options).action, "wait-for-lease");
  assert.equal(assessReservationRecovery([{
    ...base,
    status: "Proving"
  }], {
    ...options,
    leaseOwner: "browser-tab:one"
  }).action, "wait-for-lease");
  assert.equal(assessReservationRecovery([{
    ...base,
    broadcast_attempt_count: 1,
    metadata: { no_broadcast_attempt: false }
  }], options).action, "reconcile");
  assert.equal(assessReservationRecovery([{
    ...base,
    tx_bytes_hash: "ab".repeat(32)
  }], {
    ...options,
    leaseOwner: "browser-tab:one"
  }).action, "reconcile");
  assert.equal(assessReservationRecovery([{
    ...base,
    kind: "relay_withdraw",
    metadata: { no_broadcast_attempt: true, relay_handed_off: true }
  }], options).action, "relay-reconcile");
  const preHandoffRelay = {
    ...base,
    kind: "relay_withdraw",
    metadata: { no_broadcast_attempt: true }
  };
  assert.equal(
    assessReservationRecovery([preHandoffRelay], options).action,
    "wait-for-lease"
  );
  assert.equal(
    assessReservationRecovery([preHandoffRelay], {
      ...options,
      nowMs: Date.parse("2026-08-04T04:06:00.000Z")
    }).action,
    "review-replan"
  );
});

test("fresh empty local genesis can discard only no-broadcast stale reservations", () => {
  const input = {
    localTestMode: true,
    reserve: {
      module_balance: "0",
      expected_module_balance: "0",
      total_deposited: "0",
      total_withdrawn: "0",
      invariant_holds: true
    },
    notes: [],
    noteSyncStatus: "synced",
    scanHasMore: false,
    assessments: [{ action: "review-replan" }]
  };
  assert.equal(isEmptyLocalGenesisPrivacyState(input), true);
  assert.equal(canResetStaleLocalGenesisReservations(input), true);
  assert.equal(canResetStaleLocalGenesisReservations({ ...input, localTestMode: false }), false);
  assert.equal(canResetStaleLocalGenesisReservations({
    ...input,
    reserve: { ...input.reserve, total_deposited: "100" }
  }), false);
  assert.equal(canResetStaleLocalGenesisReservations({ ...input, notes: [{ nullifier: "11" }] }), false);
  assert.equal(canResetStaleLocalGenesisReservations({
    ...input,
    assessments: [{ action: "review-replan" }, { action: "reconcile" }]
  }), false);
});

test("fresh-genesis reset deletes only the exact encrypted SDK namespace under its mutation lock", async () => {
  const namespace = `fresh-reset:${webcrypto.randomUUID()}`;
  const lockCalls = [];
  const manager = await createEncryptedBrowserReservationManager({
    namespace,
    ownerKeyId: "owner",
    indexKey: webcrypto.getRandomValues(new Uint8Array(32)),
    leaseOwner: "test-document",
    cryptoImpl: webcrypto,
    indexedDB,
    locks: {
      request(name, options, callback) {
        lockCalls.push({ name, options });
        return callback();
      }
    }
  });
  const db = await manager.store.db();
  await manager.reserveNotes({
    notes: [reservationNoteFixture()],
    operationId: "stale-operation"
  });
  const expectedReservationState = await manager.store.load();
  const adjacentNamespace = `${namespace}:keep`;
  await new Promise((resolve, reject) => {
    const transaction = db.transaction("states", "readwrite");
    transaction.objectStore("states").put({ sentinel: "keep" }, adjacentNamespace);
    transaction.oncomplete = resolve;
    transaction.onerror = () => reject(transaction.error);
    transaction.onabort = () => reject(transaction.error);
  });

  await assert.rejects(
    () => resetEncryptedBrowserReservationState(manager),
    /explicit local-genesis confirmation capability/
  );
  await resetEncryptedBrowserReservationState(manager, {
    confirmedFreshLocalGenesis: true,
    expectedReservationState
  });

  const [deleted, preserved] = await Promise.all([namespace, adjacentNamespace].map(key => (
    new Promise((resolve, reject) => {
      const transaction = db.transaction("states", "readonly");
      const request = transaction.objectStore("states").get(key);
      request.onsuccess = () => resolve(request.result);
      request.onerror = () => reject(request.error);
    })
  )));
  assert.equal(deleted, undefined);
  assert.deepEqual(preserved, { sentinel: "keep" });
  assert.equal(lockCalls.at(-1).name, manager.store.lockName);
  assert.deepEqual(lockCalls.at(-1).options, { mode: "exclusive" });
  await assert.rejects(
    () => resetEncryptedBrowserReservationState({ store: {} }, {
      confirmedFreshLocalGenesis: true
    }),
    /does not support fresh-genesis reset/
  );
});

test("fresh-genesis reset preserves reservations changed after wallet approval", async () => {
  const namespace = `fresh-reset-race:${webcrypto.randomUUID()}`;
  const manager = await createEncryptedBrowserReservationManager({
    namespace,
    ownerKeyId: "owner",
    indexKey: webcrypto.getRandomValues(new Uint8Array(32)),
    leaseOwner: "fresh-reset-race-test",
    cryptoImpl: webcrypto,
    indexedDB,
    locks: {
      request(_name, _options, callback) {
        return callback();
      }
    }
  });
  await manager.reserveNotes({
    notes: [reservationNoteFixture()],
    operationId: "approved-stale-operation"
  });
  const approvedReservationState = await manager.store.load();

  await manager.reserveNotes({
    notes: [reservationNoteFixture({ nullifier: "22".repeat(32), sequence: 2 })],
    operationId: "concurrent-new-operation"
  });
  let afterResetCalled = false;
  await assert.rejects(
    () => resetEncryptedBrowserReservationState(manager, {
      confirmedFreshLocalGenesis: true,
      expectedReservationState: approvedReservationState,
      afterReset: () => {
        afterResetCalled = true;
      }
    }),
    error => error?.code === "FRESH_GENESIS_RESERVATION_STATE_CHANGED"
  );

  assert.equal(afterResetCalled, false);
  assert.equal((await manager.store.load()).reservations.length, 2);
});

test("reviewed fresh-state reset refuses active and terminal unresolved reservations before its final fence callback", async () => {
  const namespace = `reviewed-reset:${webcrypto.randomUUID()}`;
  const manager = await createEncryptedBrowserReservationManager({
    namespace,
    ownerKeyId: "owner",
    indexKey: webcrypto.getRandomValues(new Uint8Array(32)),
    leaseOwner: "reviewed-reset-test",
    cryptoImpl: webcrypto,
    indexedDB,
    locks: {
      request(_name, _options, callback) {
        return callback();
      }
    }
  });
  const db = await manager.store.db();
  const putState = async state => {
    const encoded = await manager.store.encodeState(state);
    await new Promise((resolve, reject) => {
      const transaction = db.transaction("states", "readwrite");
      transaction.objectStore("states").put(encoded, namespace);
      transaction.oncomplete = resolve;
      transaction.onerror = () => reject(transaction.error);
      transaction.onabort = () => reject(transaction.error);
    });
  };
  const readStored = () => new Promise((resolve, reject) => {
    const transaction = db.transaction("states", "readonly");
    const request = transaction.objectStore("states").get(namespace);
    request.onsuccess = () => resolve(request.result);
    request.onerror = () => reject(request.error);
  });

  let finalFenceCommitted = false;
  await putState({ reservations: [{ status: "Unknown" }] });
  await assert.rejects(
    () => resetEncryptedBrowserReservationState(manager, {
      confirmedReviewedFreshStateReset: true,
      afterReset: () => {
        finalFenceCommitted = true;
      }
    }),
    /zero active or unresolved reservations/
  );
  assert.ok(await readStored());
  assert.equal(finalFenceCommitted, false);

  for (const operationStatus of ["ManualReview", "ConflictSpent"]) {
    await putState({
      reservations: [{
        status: "ConfirmedSpent",
        metadata: {
          operation_success_evidence_required: true,
          operation_status: operationStatus
        }
      }]
    });
    await assert.rejects(
      () => resetEncryptedBrowserReservationState(manager, {
        confirmedReviewedFreshStateReset: true,
        afterReset: () => {
          finalFenceCommitted = true;
        }
      }),
      /zero active or unresolved reservations/
    );
    assert.ok(await readStored());
    assert.equal(finalFenceCommitted, false);
  }

  await putState({
    reservations: [{
      status: "ConfirmedSpent",
      metadata: {
        operation_success_evidence_required: true,
        operation_status: "Succeeded",
        operation_success_evidence_matches: true
      }
    }]
  });
  await resetEncryptedBrowserReservationState(manager, {
    confirmedReviewedFreshStateReset: true,
    afterReset: () => {
      finalFenceCommitted = true;
    }
  });
  assert.equal(await readStored(), undefined);
  assert.equal(finalFenceCommitted, true);
});

test("completed operations are excluded from later spent-note reconciliation", () => {
  const succeeded = {
    operation_id: "transfer:done",
    reservation_id: "done:one",
    nullifier_lookup_key: "lookup:one",
    status: "ConfirmedSpent",
    metadata: {
      operation_status: "Succeeded",
      operation_success_evidence_matches: true
    }
  };
  const completed = succeededOperationLookupKeys([succeeded, {
    ...succeeded,
    reservation_id: "done:two",
    nullifier_lookup_key: "lookup:two"
  }]);
  assert.deepEqual([...completed], ["lookup:one", "lookup:two"]);
  assert.deepEqual([...succeededOperationLookupKeys([succeeded, {
    ...succeeded,
    reservation_id: "done:two",
    nullifier_lookup_key: "lookup:two",
    metadata: {
      operation_status: "ManualReview",
      operation_success_evidence_matches: false
    }
  }])], []);
});

test("reconciliation remains available for unresolved terminal reservations", () => {
  const unresolved = {
    reservation_id: "reservation-unresolved",
    operation_id: "transfer:unresolved",
    status: "ConfirmedSpent",
    metadata: {
      operation_status: "ManualReview",
      operation_success_evidence_required: true
    }
  };

  assert.deepEqual(
    reconciliationReservationRecords([unresolved], [unresolved]),
    [unresolved]
  );
  assert.equal(canReconcileReservationState({
    privacyReady: true,
    active: [],
    unresolved: [unresolved]
  }), true);
  assert.equal(canReconcileReservationState({
    privacyReady: true,
    active: [],
    unresolved: [unresolved],
    reconciling: true
  }), false);
  assert.equal(canReconcileReservationState({
    privacyReady: false,
    unresolved: [unresolved]
  }), false);
});

test("reconciliation remains available when only a durable Cosmos privacy marker survives", () => {
  assert.equal(canReconcileReservationState({
    privacyReady: true,
    active: [],
    unresolved: [],
    privacyPending: true
  }), true);
  assert.equal(canReconcileReservationState({
    privacyReady: true,
    active: [],
    unresolved: [],
    privacyPending: true,
    reconciling: true
  }), false);
  assert.equal(canReconcileReservationState({
    privacyReady: false,
    privacyPending: true
  }), false);
});

test("deposit funding separates EVM asset and native gas balances", () => {
  assert.deepEqual(assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "10",
    nativeBalance: "3",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), {
    amount: 10n,
    fee: 3n,
    assetBalance: 10n,
    nativeBalance: 3n,
    requiredAsset: 10n,
    requiredNative: 3n
  });
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "9",
    nativeBalance: "100",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), /Insufficient transparent uusdc balance/);
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "100",
    nativeBalance: "2",
    assetDenom: "uusdc",
    nativeDenom: "aphoton",
    transport: "evm"
  }), /Insufficient EVM gas balance/);
});

test("deposit funding combines amount and fee when the asset is EVM native", () => {
  assert.throws(() => assertDepositFundingAvailable({
    amount: "10",
    fee: "3",
    assetBalance: "100",
    nativeBalance: "12",
    assetDenom: "uclair",
    nativeDenom: "uclair",
    transport: "evm"
  }), /need 13uclair/);
});

test("Cosmos actual fee uses ante fee-charge events instead of message spends", () => {
  const attribute = (key, value) => ({ key, value });
  const depositSpend = {
    type: "coin_spent",
    attributes: [attribute("spender", "clair1sender"), attribute("amount", "100uclair"), attribute("msg_index", "0")]
  };

  assert.equal(cosmosChargedFeeAmount({ events: [depositSpend] }, "uclair"), 0n);
  assert.equal(cosmosChargedFeeAmount({
    events: [
      {
        type: "tx",
        attributes: [attribute("fee", "2500000uclair"), attribute("fee_payer", "clair1sender")]
      },
      depositSpend
    ]
  }, "uclair"), 2500000n);
  assert.equal(cosmosChargedFeeAmount({
    events: [{ type: "tx", attributes: [attribute("fee", "")] }]
  }, "uclair"), 0n);
  assert.equal(cosmosChargedFeeAmount({}, "uclair"), null);
});

test("EVM actual fee uses receipt gas used and effective gas price", () => {
  assert.equal(evmChargedFeeAmount({ gasUsed: "0x5208", effectiveGasPrice: "0x3b9aca00" }), 21000000000000n);
  assert.equal(evmChargedFeeAmount({ gasUsed: "0x5208" }), null);
});

test("relay reconciliation binds EVM calldata, target, value, and chain", () => {
  const prepared = {
    to: "0x100000000000000000000000000000000000000b",
    data: "0x1234abcd",
    value: "0x0",
    chainId: "0x32f"
  };
  const included = {
    to: prepared.to.toUpperCase(),
    input: prepared.data,
    value: "0x0",
    chainId: "0x32f"
  };
  assert.equal(assertRelayWithdrawTransactionMatches({
    transport: "evm",
    handoffTransaction: prepared,
    transaction: included,
    expectedEvmChainId: "0x32f"
  }), true);
  assert.throws(() => assertRelayWithdrawTransactionMatches({
    transport: "evm",
    handoffTransaction: prepared,
    transaction: { ...included, input: "0x1234abce" },
    expectedEvmChainId: "0x32f"
  }), /calldata does not match/);
});

test("relay handoff uses the complete v2 envelope and rejects legacy versions", () => {
  const payload = {
    version: "v2",
    payload_hash: "33".repeat(32)
  };
  const handoff = createRelayWithdrawHandoff({
    profileId: "evm-local",
    transport: "evm",
    payload,
    transaction: { to: "0x100000000000000000000000000000000000000b" }
  });
  assert.equal(handoff.schema_version, "v2");
  assert.equal(handoff.handoff_version, "v2");
  assert.equal(handoff.request.version, "v2");
  assert.equal(relayWithdrawHandoffPayload(handoff), payload);
  assert.throws(
    () => relayWithdrawHandoffPayload({ ...handoff, handoff_version: "v1" }),
    /must use the v2 schema/
  );
  assert.throws(
    () => createRelayWithdrawHandoff({ transport: "cosmos", payload: { ...payload, version: "v1" } }),
    /payload must use v2/
  );
});

test("relay reconciliation binds every Cosmos MsgWithdraw field except creator", () => {
  const payload = {
    proof_hex: "40".repeat(128),
    root_hex: "11".repeat(32),
    nullifier_hex: "22".repeat(32),
    amount: "10uclair",
    recipient: "clair1recipient",
    chain_id: "clairveil-local-2",
    expires_at_unix: 4_102_448_400,
    payload_hash: ""
  };
  payload.payload_hash = computePreparedWithdrawPayloadHash({ ...payload, version: "v2" });
  const transaction = { tx: cosmosWithdrawTx(payload) };
  assert.equal(assertRelayWithdrawTransactionMatches({
    transport: "cosmos",
    payload,
    transaction
  }), true);
  assert.throws(() => assertRelayWithdrawTransactionMatches({
    transport: "cosmos",
    payload,
    transaction: { tx: cosmosWithdrawTx(payload, { recipient: "clair1redirected" }) }
  }), /recipient does not match/);
  assert.equal(assertRelayReservationPayloadMatches([{ payload_hash: payload.payload_hash }], payload), undefined);
  assert.throws(
    () => assertRelayReservationPayloadMatches([{ payload_hash: "44".repeat(32) }], payload),
    /does not match the reserved payload hash/
  );
  assert.equal(assertCosmosRelayWithdrawTransactionPayloadHash({
    transaction,
    payloadHash: payload.payload_hash
  }), true);
  assert.throws(() => assertCosmosRelayWithdrawTransactionPayloadHash({
    transaction: { tx: cosmosWithdrawTx(payload, { recipient: "clair1redirected" }) },
    payloadHash: payload.payload_hash
  }), /does not match the reserved payload hash/);
});

test("direct Cosmos withdraw reconciliation derives evidence only from the matching event and MsgWithdraw", () => {
  const txHash = "aa".repeat(32);
  const recipient = "clair1xcjufgh2jarkp2qkx68azh08w9v5gah8sx9zu2";
  const payload = {
    proof_hex: "40".repeat(128),
    root_hex: "11".repeat(32),
    nullifier_hex: "22".repeat(32),
    amount: "10aokrw",
    recipient,
    chain_id: "clairveil-local-2",
    expires_at_unix: 4_102_448_400
  };
  const event = {
    event_type: "withdraw",
    tx_hash_hex: txHash,
    attributes: [
      { key: "nullifier", value: payload.nullifier_hex },
      { key: "recipient", value: recipient }
    ]
  };
  const evidence = cosmosWithdrawOperationEvidence({
    event,
    transaction: { tx: cosmosWithdrawTx(payload) },
    txHash,
    expectedNullifiers: [payload.nullifier_hex],
    accountPrefix: "clair"
  });
  assert.deepEqual(evidence, {
    txHash,
    outputCommitment: "",
    auditDisclosureDigest: "",
    recipientHash: hashTransparentCosmosRecipient(recipient, { accountPrefix: "clair" }),
    amount: "10",
    amountHash: hashAmount("aokrw", "10"),
    denom: "aokrw",
    batchItemIndex: 0,
    batchItemIndexKnown: false
  });
  assert.equal(cosmosWithdrawOperationEvidence({
    event,
    transaction: { tx: cosmosWithdrawTx(payload, { recipient: "clair1redirected" }) },
    txHash,
    expectedNullifiers: [payload.nullifier_hex],
    accountPrefix: "clair"
  }), null);
  assert.equal(cosmosWithdrawOperationEvidence({
    event,
    transaction: { tx: cosmosWithdrawTx(payload) },
    txHash,
    expectedNullifiers: ["33".repeat(32)],
    accountPrefix: "clair"
  }), null);
});

test("transparent Cosmos recipient hashing is canonical and prefix-bound", () => {
  const recipient = "clair1xcjufgh2jarkp2qkx68azh08w9v5gah8sx9zu2";
  assert.equal(
    hashTransparentCosmosRecipient(recipient, { accountPrefix: "clair" }),
    "e1a4af388353123e77468a4c45e5d719869c808cc686b4513deb23905a5d11ba"
  );
  assert.throws(
    () => hashTransparentCosmosRecipient(recipient, { accountPrefix: "other" }),
    /other account address/
  );
  assert.throws(
    () => hashTransparentCosmosRecipient("clair1qqqqqq", { accountPrefix: "clair" }),
    /clair account address/
  );
});

test("relay expiry uses the authoritative block-time boundary", () => {
  const payload = { expires_at_unix: 100 };
  assert.equal(relayWithdrawPayloadExpired(payload, 99), false);
  assert.equal(relayWithdrawPayloadExpired(payload, 100), true);
  assert.equal(relayWithdrawExpiryLeaseUntil(payload), "1970-01-01T00:01:40.000Z");
  assert.throws(() => relayWithdrawExpiryLeaseUntil({ expires_at_unix: 0 }), /invalid/);
});

test("public pending transactions survive reload and clear only after resolution", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "evm-local", owner: "0xabc" };
  const key = publicPendingTxKey(identity);
  savePublicPendingTxState(storage, key, {
    ...identity,
    send: { txHash: `0x${"01".repeat(32)}`, status: "checking" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "submitted", height: "0x10" }
  });

  assert.deepEqual(loadPublicPendingTxState(storage, key, identity), {
    send: { txHash: `0x${"01".repeat(32)}`, status: "unknown" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "submitted", height: "0x10" }
  });

  savePublicPendingTxState(storage, key, {
    ...identity,
    send: { txHash: `0x${"01".repeat(32)}`, status: "included" },
    deposit: { txHash: `0x${"02".repeat(32)}`, status: "failed" }
  });
  assert.equal(storage.getItem(key), null);
});

test("included deposit recovery remains durable until its exact note is recovered", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "cosmos:clairveil-local-2", owner: "clair1owner" };
  const key = publicPendingTxKey(identity);
  const txHash = "ab".repeat(32);
  savePublicPendingTxState(storage, key, {
    ...identity,
    deposit: { txHash, status: "recovery-pending", height: "42" }
  });

  assert.deepEqual(loadPublicPendingTxState(storage, key, identity), {
    send: null,
    deposit: { txHash, status: "recovery-pending", height: "42" }
  });
  assert.throws(
    () => savePublicPendingTxState(storage, key, {
      ...identity,
      send: { txHash, status: "recovery-pending" }
    }),
    /pending transaction entry is invalid/
  );
});

test("public pending transaction store preserves a hashless wallet-boundary attempt", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "evm-local", owner: "0xabc" };
  const key = publicPendingTxKey(identity);
  const attemptId = "ab".repeat(32);
  savePublicPendingTxState(storage, key, {
    ...identity,
    send: { attemptId, status: "attempting" }
  });

  assert.deepEqual(loadPublicPendingTxState(storage, key, identity), {
    send: { txHash: "", attemptId, status: "attempting" },
    deposit: null
  });
  assert.throws(
    () => savePublicPendingTxState(storage, key, {
      ...identity,
      send: { status: "attempting" }
    }),
    /pending transaction entry is invalid/
  );
});

test("account pending state preserves an exact Cosmos privacy transaction fence", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "cosmos:clairveil-local-2", owner: "clair1owner" };
  const publicKey = publicPendingTxKey(identity);
  const privacyKey = privacyPendingTxKey(identity);
  const txHash = "AB".repeat(32);
  savePrivacyPendingTxState(storage, privacyKey, {
    ...identity,
    privacy: { txHash, status: "unknown" }
  });

  assert.equal(loadPublicPendingTxState(storage, publicKey, identity), null);
  assert.deepEqual(loadPrivacyPendingTxState(storage, privacyKey, identity), {
    txHash,
    status: "unknown"
  });
  assert.equal(storage.getItem(publicKey), null);
  assert.throws(
    () => savePrivacyPendingTxState(storage, privacyKey, {
      ...identity,
      privacy: { attemptId: "ab".repeat(32), status: "attempting" }
    }),
    /requires an exact transaction hash/
  );
  assert.throws(
    () => savePublicPendingTxState(storage, publicKey, {
      ...identity,
      privacy: { txHash, status: "unknown" }
    }),
    /separate privacy pending store/
  );
});

test("public pending transaction state is isolated by local genesis epoch", () => {
  const storage = new MemoryStorage();
  const first = {
    profileId: "clairveil-local",
    owner: "clair1owner",
    storageEpoch: "aa".repeat(32)
  };
  const second = { ...first, storageEpoch: "bb".repeat(32) };
  const firstKey = publicPendingTxKey(first);
  const secondKey = publicPendingTxKey(second);
  assert.notEqual(firstKey, secondKey);

  savePublicPendingTxState(storage, firstKey, {
    ...first,
    send: { txHash: "01".repeat(32), status: "submitted" }
  });
  assert.throws(
    () => loadPublicPendingTxState(storage, firstKey, second),
    error => error.code === "PUBLIC_PENDING_STATE_CORRUPT"
  );
});

test("corrupt public pending state fails closed", () => {
  const storage = new MemoryStorage();
  const identity = { profileId: "evm-local", owner: "0xabc" };
  const key = publicPendingTxKey(identity);
  const privacyKey = privacyPendingTxKey(identity);
  const privacy = { txHash: "cd".repeat(32), status: "unknown" };
  savePrivacyPendingTxState(storage, privacyKey, { ...identity, privacy });
  storage.setItem(key, "not-json");
  assert.throws(
    () => loadPublicPendingTxState(storage, key, identity),
    error => error.code === "PUBLIC_PENDING_STATE_CORRUPT"
  );
  storage.removeItem(key);
  assert.deepEqual(loadPrivacyPendingTxState(storage, privacyKey, identity), privacy);
  storage.setItem(privacyKey, "not-json");
  assert.throws(
    () => loadPrivacyPendingTxState(storage, privacyKey, identity),
    error => error.code === "PRIVACY_PENDING_STATE_CORRUPT"
  );
});

test("relay handoff recovery persists metadata only and cannot restore the raw payload", async () => {
  const storage = new MemoryStorage();
  const keyMaterial = webcrypto.getRandomValues(new Uint8Array(32));
  const options = {
    storage,
    cryptoImpl: webcrypto,
    locks: new MemoryLocks(),
    key: "operation-state",
    namespace: "chain:profile:owner",
    keyMaterial
  };
  const store = await EncryptedLocalStorageOperationStore.open(options);
  const rawRelayState = {
    handoff: {
      request: {
        payload: {
          payload_hash: "AA".repeat(32),
          expires_at_unix: 1_800_000_000,
          proof: "private proof",
          recipient: "clair1private",
          amount: "99"
        }
      },
      transaction: { data: "private calldata" }
    },
    json: "private witness",
    reservationIds: ["r1"],
    txHash: "AB".repeat(32),
    submittedBy: "display-only relayer",
    resultMessage: "display-only upstream error",
    externalHandoff: true
  };
  const state = {
    version: relayWithdrawRecoveryVersion,
    relayWithdraw: relayWithdrawRecoveryMetadata(rawRelayState)
  };
  const persistenceId = relayWithdrawRecoveryPersistenceId(state);
  await store.save(state);
  assert.doesNotMatch(storage.getItem(`${options.key}:${persistenceId}`), /private witness/);
  assert.equal(storage.getItem(options.key), null);

  const reopened = await EncryptedLocalStorageOperationStore.open(options);
  const loaded = await reopened.load(persistenceId);
  assert.deepEqual(loaded, state);
  assert.deepEqual(await reopened.loadAll(), [state]);
  assert.doesNotMatch(JSON.stringify(loaded), /private witness|private proof|private calldata|clair1private|display-only/);
  const restored = restoreRelayWithdrawRecoveryMetadata(loaded.relayWithdraw);
  assert.equal(restored.handoff, null);
  assert.equal(restored.json, "");
  assert.equal(restored.payloadUnavailable, true);
  assert.match(restored.resultMessage, /raw payload was not persisted/);
  assert.throws(
    () => restoreRelayWithdrawRecoveryMetadata({
      ...loaded.relayWithdraw,
      payloadHash: "not-a-hash"
    }),
    /payloadHash must be canonical 32-byte hex/
  );
  assert.throws(
    () => restoreRelayWithdrawRecoveryMetadata({
      ...loaded.relayWithdraw,
      handoff: rawRelayState.handoff
    }),
    /metadata field handoff is not supported/
  );
  for (const invalid of [
    { reservationIds: [] },
    { reservationIds: ["r1", "r1"] },
    { expiresAtUnix: 0 },
    { expiresAtUnix: Number.MAX_SAFE_INTEGER + 1 },
    { txHash: `0x${"ab".repeat(32)}` },
    { txHash: "AB".repeat(32) },
    { externalHandoff: "true" },
    { payload: { proof: "private" } }
  ]) {
    assert.throws(() => restoreRelayWithdrawRecoveryMetadata({
      ...loaded.relayWithdraw,
      ...invalid
    }));
  }

  const wrongKey = await EncryptedLocalStorageOperationStore.open({
    ...options,
    keyMaterial: webcrypto.getRandomValues(new Uint8Array(32))
  });
  await assert.rejects(() => wrongKey.load(persistenceId), error => error.code === "OPERATION_STATE_CORRUPT");
});

test("encrypted relay recovery revalidates its session immediately before storage commit", async () => {
  const storage = new MemoryStorage();
  const store = await EncryptedLocalStorageOperationStore.open({
    storage,
    cryptoImpl: webcrypto,
    locks: new MemoryLocks(),
    key: "operation-state",
    namespace: "chain:profile:owner",
    keyMaterial: webcrypto.getRandomValues(new Uint8Array(32))
  });
  let currentSession = "session-b";
  const relayState = txHash => ({
    version: relayWithdrawRecoveryVersion,
    relayWithdraw: {
      reservationIds: ["r1"],
      payloadHash: "ef".repeat(32),
      expiresAtUnix: 1_800_000_000,
      txHash,
      externalHandoff: true,
      durableNoBroadcast: false,
      resultStatus: "unknown"
    }
  });

  await assert.rejects(
    () => store.save(relayState("11".repeat(32)), {
      beforeCommit: () => {
        if (currentSession !== "session-a") throw new Error("stale privacy session");
      }
    }),
    /stale privacy session/
  );
  assert.equal(storage.length, 0);

  currentSession = "session-a";
  const current = relayState("22".repeat(32));
  await store.save(current, {
    beforeCommit: () => {
      if (currentSession !== "session-a") throw new Error("stale privacy session");
    }
  });
  assert.equal((await store.load("ef".repeat(32))).relayWithdraw.txHash, "22".repeat(32));
});

test("encrypted relay recovery keeps concurrent payload records independently", async () => {
  const storage = new MemoryStorage();
  const locks = new MemoryLocks();
  const options = {
    storage,
    cryptoImpl: webcrypto,
    locks,
    key: "operation-state",
    namespace: "chain:profile:owner",
    keyMaterial: webcrypto.getRandomValues(new Uint8Array(32))
  };
  const firstStore = await EncryptedLocalStorageOperationStore.open(options);
  const secondStore = await EncryptedLocalStorageOperationStore.open(options);
  const recovery = (payloadHash, reservationId) => ({
    version: relayWithdrawRecoveryVersion,
    profileId: "cosmos:chain",
    owner: "clair1owner",
    relayWithdraw: {
      reservationIds: [reservationId],
      payloadHash,
      expiresAtUnix: 1_800_000_000,
      txHash: "",
      externalHandoff: false,
      durableNoBroadcast: true,
      resultStatus: "ready"
    }
  });
  const first = recovery("01".repeat(32), "r1");
  const second = recovery("02".repeat(32), "r2");

  await Promise.all([firstStore.save(first), secondStore.save(second)]);
  assert.deepEqual(await firstStore.loadAll(), [first, second]);
  await firstStore.clear(first.relayWithdraw.payloadHash);
  assert.deepEqual(await secondStore.loadAll(), [second]);
});

test("operation event lookup traverses pages at the included height", async () => {
  const requests = [];
  const event = { event_type: "shielded_transfer", height: "42", tx_hash_hex: "ABCD" };
  const found = await findPrivacyEventByTxHash({
    txHash: "0xabcd",
    height: 42,
    fetchPage: async request => {
      requests.push(request);
      return request.page === 1
        ? { page: 1, has_more: true, events: [{ height: "42", tx_hash_hex: "OTHER" }] }
        : { page: 2, has_more: false, events: [event] };
    }
  });

  assert.equal(found, event);
  assert.deepEqual(requests.map(request => [request.afterHeight, request.page, request.limit]), [
    [41, 1, 200],
    [41, 2, 200]
  ]);
});

test("operation event recovery queries the event type bound to its reservation kind", async () => {
  assert.deepEqual(reservationPrivacyEventTypes([{ kind: "transfer" }]), ["shielded_transfer"]);
  assert.deepEqual(reservationPrivacyEventTypes([{ kind: "withdraw" }]), ["withdraw"]);
  assert.deepEqual(reservationPrivacyEventTypes([{ kind: "relay_withdraw" }]), ["withdraw"]);
  assert.throws(
    () => reservationPrivacyEventTypes([{ kind: "transfer" }, { kind: "withdraw" }]),
    /one reservation kind/
  );
  assert.throws(
    () => reservationPrivacyEventTypes([{ kind: "unknown" }]),
    /unsupported reservation kind/
  );

  const requests = [];
  const withdraw = { event_type: "withdraw", height: "42", tx_hash_hex: "ABCD" };
  const found = await findPrivacyEventByTxHash({
    txHash: "ABCD",
    height: 42,
    eventTypes: reservationPrivacyEventTypes([{ kind: "withdraw" }]),
    fetchPage: async request => {
      requests.push(request);
      return request.page === 1
        ? { page: 1, has_more: true, events: [] }
        : { page: 2, has_more: false, events: [withdraw] };
    }
  });

  assert.equal(found, withdraw);
  assert.deepEqual(requests.map(request => request.eventTypes), [
    ["withdraw"],
    ["withdraw"]
  ]);
});

test("operation event lookup propagates query failures", async () => {
  await assert.rejects(() => findPrivacyEventByTxHash({
    txHash: "ABCD",
    height: 42,
    fetchPage: async () => {
      throw new Error("REST unavailable");
    }
  }), /REST unavailable/);
});

test("unverified disclosure reports never expose plaintext view data", () => {
  const report = {
    verification: { verified: false },
    summary: { amount: "100", from_shielded_address: "secret" },
    payload: { plaintext: "secret" }
  };
  assert.deepEqual(disclosureViewModel(report), {
    verified: false,
    plane: "",
    policy: "",
    outputIndex: null,
    commitmentHex: "",
    digestHex: "",
    summary: null,
    payload: null
  });
  const verified = disclosureViewModel({
    ...report,
    verification: { verified: true },
    plane: "user",
    policy: "public",
    output_index: 2,
    commitment_hex: "11".repeat(32),
    digest_hex: "22".repeat(32)
  });
  assert.equal(verified.summary.amount, "100");
  assert.deepEqual(
    [verified.plane, verified.policy, verified.outputIndex, verified.commitmentHex, verified.digestHex],
    ["user", "public", 2, "11".repeat(32), "22".repeat(32)]
  );
});
