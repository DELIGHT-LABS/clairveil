import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";
import { bech32AddressToEvm } from "clairveiljs/evm";
import { derivePrivacyMaterial } from "clairveiljs/crypto";
import {
  decodeAuditDisclosureFromEvent,
  decodeSelfViewDisclosureFromEvent,
  decodeUserDisclosureFromEvent,
  disclosureScalarFromHex
} from "clairveiljs/core";
import {
  hashAmount,
  hashRecipient,
  operationStatuses,
  reservationHeartbeatIntervalMs,
  reservationStatuses
} from "clairveiljs/reservation";
import { getStaticDappConfig } from "./dapp-config.js";
import { EncryptedLocalStorageNoteStore } from "./encrypted-note-store.js";
import { createEncryptedBrowserReservationManager } from "./encrypted-reservation-manager.js";
import { EncryptedLocalStorageOperationStore } from "./encrypted-operation-store.js";
import { assertDepositFundingAvailable } from "./deposit-funding.js";
import { cosmosChargedFeeAmount, evmChargedFeeAmount } from "./network-fee.js";
import { normalizeBrowserProfileEndpoints } from "./browser-profile.js";
import { keplrDirectSignOptions } from "./cosmos-sign-options.js";
import { disclosureViewModel } from "./disclosure-view-model.js";
import { findPrivacyEventByTxHash, normalizedTxHash } from "./operation-event-lookup.js";
import {
  loadPublicPendingTxState,
  publicPendingTxKey,
  savePublicPendingTxState
} from "./public-pending-tx-store.js";
import {
  assertRelayReservationPayloadMatches,
  assertRelayWithdrawTransactionMatches,
  createRelayWithdrawHandoff,
  relayWithdrawHandoffPayload,
  relayWithdrawPayloadExpired
} from "./relay-withdraw-reconciliation.js";
import {
  assessReservationRecovery,
  groupReservationOperations
} from "./reservation-recovery.js";

function defaultMetaMaskState() {
  return {
    account: "",
    chainId: "",
    signatureHash: ""
  };
}

function defaultNoteScanCursor() {
  return {
    source: "privacy_scan",
    after: { height: 0, global_sequence: 0, output_index: 0 },
    next_cursor: { height: 0, global_sequence: 0, output_index: 0 },
    output_limit: 200,
    has_more: false,
    latest_height: 0,
    latest_sequence: 0,
    latest_output_index: 0,
    pages_scanned: 0,
    completed: false
  };
}

function defaultKeplrState() {
  return {
    account: "",
    name: "",
    pubkeyHex: "",
    expectedAddress: "",
    addressMatches: false,
    signerCheck: "",
    signatureHash: "",
    verified: false,
    balance: "",
    transparentBalances: {},
    evmNativeBalance: "0",
    faucetHash: "",
    faucetSent: "",
    faucetRecipient: "",
    shieldedAddress: "",
    disclosurePubKeyHex: "",
    rootSignatureBase64: "",
    rootSignatureHash: "",
    sendHash: "",
    sendStatus: "idle",
    publicPendingStateError: "",
    depositHash: "",
    depositHeight: "",
    depositPrepared: null,
    depositRecoveryStatus: "idle",
    depositRecoveryMessage: "Not started",
    networkFeeEstimate: "Not estimated",
    networkFeeAmount: "0",
    transferHash: "",
    withdrawHash: "",
    withdrawHeight: "",
    withdrawNullifierStatus: "Not checked",
    withdrawReceiveStatus: "Not checked",
    notesSummary: "",
    notes: [],
    notesScanned: false,
    noteScanCursor: defaultNoteScanCursor(),
    noteScanResumeOptions: null,
    noteSyncStatus: "idle",
    noteSyncMessage: "Not scanned",
    showSpendableOnly: true
  };
}

function defaultReservationState() {
  return {
    status: "idle",
    message: "No active note reservations",
    active: [],
    unresolved: [],
    retryBlocked: false,
    reconciling: false,
    recoveringOperationKey: "",
    noteStatuses: new Map()
  };
}

const state = {
  config: null,
  chainProfiles: [],
  selectedChainProfileId: "",
  selectedRestEndpointByProfile: {},
  accounts: [],
  selectedAccount: "alice",
  addressBook: {
    shieldedByName: {},
    shieldedError: "",
    loadingShielded: false
  },
  activeWallet: "",
  wallet: defaultMetaMaskState(),
  keplr: defaultKeplrState(),
  auditor: {
    events: [],
    selectedTxHash: "",
    decoded: null,
    testScalar: "",
    testScalarError: "",
    testScalarMatchesAuditConfig: false,
    loading: false
  },
  privacyEvents: {
    events: [],
    selectedTxHash: "",
    decoded: null,
    error: "",
    loadError: "",
    loading: false
  },
  blockEvents: {
    events: [],
    error: ""
  },
  protocol: {
    ready: false,
    reserve: null,
    error: ""
  },
  relayWithdraw: {
    handoff: null,
    json: "",
    reservationIds: [],
    txHash: "",
    resultStatus: "idle",
    resultMessage: "Not checked"
  },
  reservations: defaultReservationState()
};

const $ = selector => document.querySelector(selector);
let shieldedAddressBookPromise = null;
let browserClient = null;
let browserClientKey = "";
let browserClientDepositProofProvider = null;
let noteStore = null;
let noteStorePromise = null;
let noteStoreKey = "";
let reservationManager = null;
let reservationManagerPromise = null;
let reservationManagerKey = "";
let operationStore = null;
let operationStorePromise = null;
let operationStoreKey = "";
let publicPendingStateKey = "";
let relayReservationHeartbeatTimer = null;
let relayRecoverySaveTimer = null;
const reservationLeaseOwner = (() => {
  const storageKey = "clairveil:v0.3.1:reservation-lease-owner";
  try {
    const existing = globalThis.sessionStorage?.getItem(storageKey);
    if (existing) return existing;
    const value = `browser-tab:${globalThis.crypto?.randomUUID?.() || Date.now()}`;
    globalThis.sessionStorage?.setItem(storageKey, value);
    return value;
  } catch {
    return `browser-tab:${globalThis.crypto?.randomUUID?.() || Date.now()}`;
  }
})();
let serverConfigAvailable = true;

function activeChainProfile() {
  return state.chainProfiles.find(profile => profile.id === state.selectedChainProfileId)
    || state.chainProfiles.find(profile => profile.id === state.config?.activeChainProfileId)
    || state.config?.activeProfile
    || null;
}

function activeWalletKind() {
  const profile = activeChainProfile();
  return profile?.wallet || (profile?.transport === "evm" ? "metamask" : "keplr");
}

function activeTransparentAddressFormat() {
  const profile = activeChainProfile();
  return profile?.transport === "evm" || activeWalletKind() === "metamask" ? "evm" : "bech32";
}

function isEvmTransparentMode(walletKind = activeWalletKind()) {
  return activeTransparentAddressFormat() === "evm" || walletKind === "metamask" || walletKind === "evm";
}

function activeKeplrChainInfo() {
  return browserWalletProfile(activeChainProfile())?.keplrChainInfo || state.config?.keplrChainInfo;
}

function selectedProfileMatchesServer(profile = activeChainProfile()) {
  if (state.config?.serverBacked === false) return true;
  if (!profile || !state.config) return true;
  return profile.transport === state.config.transport && profile.chainId === state.config.chainId;
}

function accountPrefix() {
  const profile = activeChainProfile();
  return profile?.accountPrefix || state.config?.accountPrefix || state.config?.keplrChainInfo?.bech32Config?.bech32PrefixAccAddr || "clair";
}

function shieldedPrefix() {
  return activeChainProfile()?.shieldedPrefix || state.config?.shieldedPrefix || "clairs";
}

function baseDenom() {
  return activeChainProfile()?.denom || state.config?.denom || "uclair";
}

function evmNativeDenom() {
  return activeChainProfile()?.evmNativeDenom || state.config?.evmNativeDenom || baseDenom();
}

function displayDenom() {
  return activeChainProfile()?.displayDenom || state.config?.displayDenom || "CLAIR";
}

function serverFeature(name) {
  return Boolean(state.config?.serverFeatures?.[name]);
}

function localTestBackendEnabled() {
  return serverFeature("localTestMode");
}

function renderServerFeatureVisibility() {
  const localSigners = serverFeature("localSigners");
  const faucet = serverFeature("faucet");
  const auditorAdmin = serverFeature("auditorAdmin");

  if (els.localSignerPanel) {
    els.localSignerPanel.hidden = !localSigners;
  }
  if (els.faucetRow) {
    els.faucetRow.hidden = !faucet;
  }
  for (const row of [els.localHomeRow, els.faucetHashRow, els.faucetSentRow, els.faucetRecipientRow]) {
    if (row) row.hidden = !localTestBackendEnabled();
  }
  if (els.auditorSection) {
    els.auditorSection.hidden = !auditorAdmin;
  }
}

function expectedEvmChainIdHex() {
  const value = String(activeChainProfile()?.evmChainId || state.config?.evmChainId || "").trim();
  if (/^0x[0-9a-fA-F]+$/.test(value)) {
    return `0x${BigInt(value).toString(16)}`;
  }
  if (/^[0-9]+$/.test(value)) {
    return `0x${BigInt(value).toString(16)}`;
  }
  return "";
}

function browserEndpointUrl(configured, { trim = false } = {}) {
  try {
    const url = new URL(configured);
    if ((url.hostname === "127.0.0.1" || url.hostname === "localhost") && window.location.hostname) {
      url.hostname = window.location.hostname;
    }
    const text = url.toString();
    return trim ? text.replace(/\/$/, "") : text;
  } catch {
    return trim ? String(configured || "").replace(/\/$/, "") : configured;
  }
}

function evmRpcUrlForWallet(profile = activeChainProfile()) {
  const configured = profile?.evmRpc || state.config?.evmRpc || "http://127.0.0.1:8545";
  return browserEndpointUrl(configured);
}

function browserRpcUrl(profile = activeChainProfile()) {
  return browserEndpointUrl(profile?.rpc || state.config?.rpc || "", { trim: true });
}

function browserRestUrl(profile = activeChainProfile()) {
  const endpoints = profileRestEndpoints(profile);
  const selected = state.selectedRestEndpointByProfile[profile?.id || ""];
  const configured = endpoints.includes(selected) ? selected : endpoints[0] || profile?.rest || state.config?.rest || "";
  return browserEndpointUrl(configured, { trim: true });
}

function profileRestEndpoints(profile = activeChainProfile()) {
  const values = [
    profile?.rest || state.config?.rest || "",
    ...(Array.isArray(profile?.restEndpoints) ? profile.restEndpoints : [])
  ];
  return [...new Set(values.map(value => String(value || "").trim()).filter(Boolean))];
}

async function fetchLatestChainBlock({ signal } = {}) {
  const endpoint = browserRestUrl();
  if (!endpoint) throw new Error("A browser-accessible chain REST endpoint is required for authoritative expiry");
  const response = await fetch(`${endpoint}/cosmos/base/tendermint/v1beta1/blocks/latest`, { signal });
  if (!response.ok) {
    throw new Error(`Latest block time query failed with HTTP ${response.status}`);
  }
  const data = await response.json();
  const value = data?.block?.header?.time ?? data?.sdk_block?.header?.time;
  const milliseconds = Date.parse(String(value || ""));
  if (!Number.isFinite(milliseconds)) {
    throw new Error("Latest block response omitted a valid timestamp");
  }
  const rawHeight = data?.block?.header?.height ?? data?.sdk_block?.header?.height;
  const height = Number(rawHeight);
  if (!Number.isSafeInteger(height) || height <= 0) {
    throw new Error("Latest block response omitted a valid height");
  }
  return { timeUnix: Math.floor(milliseconds / 1000), height };
}

async function fetchLatestChainBlockTimeUnix(options = {}) {
  return (await fetchLatestChainBlock(options)).timeUnix;
}

async function privacyOperationTiming(options = {}) {
  const chainNowUnix = await fetchLatestChainBlockTimeUnix(options);
  return { chainNowUnix, expiresAtUnix: chainNowUnix + 1800 };
}

function activeProofSignal() {
  return transferFlowState.controller?.signal;
}

function browserProverUrl(profile = activeChainProfile()) {
  const configured = profile?.proverUrl || state.config?.proverUrl || "";
  if (state.config?.serverBacked && serverFeature("proverProxy") && configured) {
    try {
      const url = new URL(configured);
      if (url.hostname === "127.0.0.1" || url.hostname === "localhost") {
        return window.location.origin.replace(/\/$/, "");
      }
    } catch {
      // Keep the configured value path below.
    }
  }
  return browserEndpointUrl(configured, { trim: true });
}

function browserDepositProofUrl(profile = activeChainProfile()) {
  return browserEndpointUrl(profile?.depositProofUrl || state.config?.depositProofUrl || "", { trim: true });
}

function configuredDepositProofProvider() {
  return typeof globalThis.CLAIRVEIL_DEPOSIT_PROOF_PROVIDER === "function"
    ? globalThis.CLAIRVEIL_DEPOSIT_PROOF_PROVIDER
    : null;
}

function depositProofReady(profile = activeChainProfile()) {
  return Boolean(configuredDepositProofProvider() || browserDepositProofUrl(profile));
}

function browserWalletProfile(profile = activeChainProfile()) {
  const resolved = profile || state.config?.activeProfile;
  if (!resolved) return null;
  const normalized = normalizeBrowserProfileEndpoints(resolved, {
    rpc: browserRpcUrl(resolved),
    rest: browserRestUrl(resolved),
    proverUrl: browserProverUrl(resolved),
    depositProofUrl: browserDepositProofUrl(resolved)
  });
  if (normalized.transport === "evm") {
    Object.assign(normalized, {
      evmRpc: evmRpcUrlForWallet(resolved),
      evmChainId: resolved?.evmChainId || state.config?.evmChainId,
      evmPrivacyPrecompileAddress: resolved?.evmPrivacyPrecompileAddress || state.config?.evmPrivacyPrecompileAddress,
      evmDepositMode: resolved?.evmDepositMode || state.config?.evmDepositMode || "nonpayable",
      evmNativeDenom: resolved?.evmNativeDenom || state.config?.evmNativeDenom || resolved?.denom,
      evmGasLimit: resolved?.evmGasLimit || state.config?.evmGasLimit,
      evmSendGasLimit: resolved?.evmSendGasLimit || state.config?.evmSendGasLimit
    });
  }
  return normalized;
}

function clairveilBrowserClient(profile = activeChainProfile()) {
  const resolved = profile || state.config?.activeProfile;
  if (!resolved) throw new Error("A validated Clairveil chain profile is required");
  const depositProofProvider = configuredDepositProofProvider();
  const browserProfile = browserWalletProfile(resolved);
  const key = JSON.stringify({
    id: browserProfile?.id || "",
    rpc: browserProfile?.rpc || "",
    rest: browserProfile?.rest || "",
    chainId: browserProfile?.chainId || "",
    accountPrefix: browserProfile?.accountPrefix || "",
    shieldedPrefix: browserProfile?.shieldedPrefix || "",
    denom: browserProfile?.denom || "",
    proverUrl: browserProfile?.proverUrl || "",
    depositProofUrl: browserProfile?.depositProofUrl || "",
    evmRpc: browserProfile?.evmRpc || "",
    evmChainId: browserProfile?.evmChainId || "",
    evmPrivacyPrecompileAddress: browserProfile?.evmPrivacyPrecompileAddress || "",
    evmDepositMode: browserProfile?.evmDepositMode || "nonpayable",
    evmNativeDenom: browserProfile?.evmNativeDenom || "",
    batchTransfer: serverFeature("batchTransfer")
  });
  if (!browserClient || browserClientKey !== key || browserClientDepositProofProvider !== depositProofProvider) {
    browserClient = createClairveilBrowserDappClient({
      profile: browserProfile,
      depositProofProvider,
      enableExperimentalBatchTransfer: serverFeature("batchTransfer")
    });
    browserClientKey = key;
    browserClientDepositProofProvider = depositProofProvider;
  }
  return browserClient;
}

function noteStoreKeys() {
  const profile = activeChainProfile();
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  if (!profile?.id || !owner) return null;
  return {
    owner,
    namespace: `${profile.chainId || profile.id}:${profile.id}:${owner}`,
    encrypted: `clairveil:v0.3.1:notes-encrypted:${profile.id}:${owner}`,
    legacy: `clairveil:v0.3.1:notes:${profile.id}:${owner}`
  };
}

function publicPendingIdentity() {
  const profileId = activeChainProfile()?.id || "";
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  const key = publicPendingTxKey({ profileId, owner });
  return key ? { profileId, owner, key } : null;
}

function hydratePublicPendingTransactions() {
  const identity = publicPendingIdentity();
  publicPendingStateKey = identity?.key || "";
  state.keplr.publicPendingStateError = "";
  if (!identity || !globalThis.localStorage) return;
  try {
    const saved = loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
    if (saved?.send) {
      state.keplr.sendHash = saved.send.txHash;
      state.keplr.sendStatus = saved.send.status;
    }
    if (saved?.deposit) {
      state.keplr.depositHash = saved.deposit.txHash;
      state.keplr.depositHeight = saved.deposit.height || "";
      state.keplr.depositRecoveryStatus = saved.deposit.status;
      state.keplr.depositRecoveryMessage = "Restored unresolved tx · reconcile before retrying";
    }
  } catch (error) {
    state.keplr.publicPendingStateError = error.message;
    state.keplr.sendStatus = "unknown";
    state.keplr.depositRecoveryStatus = "unknown";
    state.keplr.depositRecoveryMessage = error.message;
  }
}

function persistPublicPendingTransactions() {
  const identity = publicPendingIdentity();
  if (!identity || identity.key !== publicPendingStateKey || !globalThis.localStorage) return;
  if (state.keplr.publicPendingStateError) return;
  savePublicPendingTxState(globalThis.localStorage, identity.key, {
    ...identity,
    send: { txHash: state.keplr.sendHash, status: state.keplr.sendStatus },
    deposit: {
      txHash: state.keplr.depositHash,
      status: state.keplr.depositRecoveryStatus,
      height: state.keplr.depositHeight
    }
  });
}

function clearPublicPendingTransactions() {
  const identity = publicPendingIdentity();
  if (!identity || !state.keplr.publicPendingStateError) return;
  if (!window.confirm("Only clear this state after checking wallet history and the chain for pending transactions. Continue?")) return;
  globalThis.localStorage?.removeItem(identity.key);
  state.keplr.publicPendingStateError = "";
  state.keplr.sendHash = "";
  state.keplr.sendStatus = "idle";
  state.keplr.depositHash = "";
  state.keplr.depositHeight = "";
  state.keplr.depositRecoveryStatus = "idle";
  state.keplr.depositRecoveryMessage = "Not started";
  renderKeplr();
}

async function currentNoteStore() {
  const keys = noteStoreKeys();
  if (!keys || !globalThis.localStorage || !state.keplr.rootSignatureBase64) return null;
  if (noteStore && noteStoreKey === keys.encrypted) return noteStore;
  if (!noteStorePromise || noteStoreKey !== keys.encrypted) {
    const openingKey = keys.encrypted;
    noteStoreKey = openingKey;
    const opening = EncryptedLocalStorageNoteStore.open({
      storage: globalThis.localStorage,
      key: openingKey,
      owner: keys.owner,
      namespace: keys.namespace,
      keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
    }).then(store => {
      if (noteStoreKey === openingKey && noteStorePromise === opening) {
        noteStore = store;
        if (globalThis.localStorage.getItem(keys.legacy)) {
          globalThis.localStorage.removeItem(keys.legacy);
          state.keplr.noteSyncStatus = "rescan-required";
          state.keplr.noteSyncMessage = "Legacy plaintext cache removed · reset and rescan required";
        }
      }
      return store;
    }).catch(error => {
      if (noteStoreKey === openingKey && noteStorePromise === opening) {
        noteStorePromise = null;
        state.keplr.noteSyncStatus = "corrupt";
        state.keplr.noteSyncMessage = error.message;
      }
      throw error;
    });
    noteStorePromise = opening;
  }
  return noteStorePromise;
}

async function clearCurrentNoteStore() {
  const keys = noteStoreKeys();
  if (keys && globalThis.localStorage) {
    globalThis.localStorage.removeItem(keys.encrypted);
    globalThis.localStorage.removeItem(keys.legacy);
  }
  noteStore = null;
  noteStorePromise = null;
  noteStoreKey = "";
  state.keplr.notes = [];
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.keplr.noteScanResumeOptions = null;
  state.keplr.notesScanned = false;
  state.keplr.noteSyncStatus = "rescan-required";
  state.keplr.noteSyncMessage = "Local note cache reset · full rescan required";
}

function reservationIdentity() {
  const profile = activeChainProfile();
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  if (!profile?.id || !profile.chainId || !owner || !state.keplr.rootSignatureBase64) return null;
  return {
    owner,
    ownerKeyId: `${profile.chainId}:${owner}`,
    namespace: `${profile.chainId}:${profile.id}:${owner}`,
    cacheKey: `${profile.id}:${profile.chainId}:${owner}:${state.keplr.rootSignatureHash || state.keplr.signatureHash}`
  };
}

async function currentReservationManager() {
  const identity = reservationIdentity();
  if (!identity) return null;
  if (reservationManager && reservationManagerKey === identity.cacheKey) return reservationManager;
  if (!reservationManagerPromise || reservationManagerKey !== identity.cacheKey) {
    const openingKey = identity.cacheKey;
    reservationManagerKey = openingKey;
    const material = derivePrivacyMaterial({
      address: state.keplr.account,
      pubKeyHex: state.keplr.pubkeyHex,
      signatureBase64: state.keplr.rootSignatureBase64,
      shieldedPrefix: shieldedPrefix()
    });
    const opening = createEncryptedBrowserReservationManager({
      namespace: identity.namespace,
      ownerKeyId: identity.ownerKeyId,
      indexKey: material.rootSeed,
      keyMaterial: material.rootSeed,
      leaseOwner: reservationLeaseOwner
    }).then(manager => {
      if (reservationManagerKey === openingKey && reservationManagerPromise === opening) {
        reservationManager = manager;
      }
      return manager;
    }).catch(error => {
      if (reservationManagerKey === openingKey && reservationManagerPromise === opening) {
        reservationManagerPromise = null;
        state.reservations.status = "error";
        state.reservations.message = error.message;
        state.reservations.retryBlocked = true;
      }
      throw error;
    });
    reservationManagerPromise = opening;
  }
  return reservationManagerPromise;
}

function operationStoreIdentity() {
  const profile = activeChainProfile();
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  if (!profile?.id || !owner || !state.keplr.rootSignatureBase64) return null;
  return {
    profileId: profile.id,
    owner,
    namespace: `${profile.chainId || profile.id}:${profile.id}:${owner}`,
    key: `clairveil:v0.3.1:operations-encrypted:${profile.id}:${owner}`
  };
}

async function currentOperationStore() {
  const identity = operationStoreIdentity();
  if (!identity || !globalThis.localStorage) return null;
  if (operationStore && operationStoreKey === identity.key) return operationStore;
  if (!operationStorePromise || operationStoreKey !== identity.key) {
    const openingKey = identity.key;
    operationStoreKey = openingKey;
    const opening = EncryptedLocalStorageOperationStore.open({
      storage: globalThis.localStorage,
      key: identity.key,
      namespace: identity.namespace,
      keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
    }).then(store => {
      if (operationStoreKey === openingKey && operationStorePromise === opening) operationStore = store;
      return store;
    }).catch(error => {
      if (operationStoreKey === openingKey && operationStorePromise === opening) operationStorePromise = null;
      throw error;
    });
    operationStorePromise = opening;
  }
  return operationStorePromise;
}

async function persistRelayWithdrawRecovery(next = state.relayWithdraw) {
  const store = await currentOperationStore();
  if (!store) throw new Error("Encrypted operation recovery store is not available");
  if (!next?.handoff) {
    store.clear();
    return;
  }
  const identity = operationStoreIdentity();
  await store.save({
    version: "clairveil-relay-withdraw-recovery-v1",
    profileId: identity.profileId,
    owner: identity.owner,
    relayWithdraw: next
  });
}

function queueRelayWithdrawRecoverySave() {
  globalThis.clearTimeout(relayRecoverySaveTimer);
  relayRecoverySaveTimer = globalThis.setTimeout(() => {
    persistRelayWithdrawRecovery().catch(error => {
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = error.message;
      renderRelayWithdraw();
    });
  }, 200);
}

async function hydrateRelayWithdrawRecovery() {
  const store = await currentOperationStore();
  const identity = operationStoreIdentity();
  if (!store || !identity) return;
  const saved = await store.load();
  if (!saved) return;
  if (saved.version !== "clairveil-relay-withdraw-recovery-v1"
    || saved.profileId !== identity.profileId
    || saved.owner !== identity.owner
    || !Array.isArray(saved.relayWithdraw?.reservationIds)) {
    const error = new Error("Encrypted relay recovery state has an invalid identity or format");
    error.code = "OPERATION_STATE_CORRUPT";
    throw error;
  }
  try {
    relayWithdrawHandoffPayload(saved.relayWithdraw?.handoff);
  } catch (cause) {
    const error = new Error("Encrypted relay recovery state contains a legacy or invalid relay handoff", { cause });
    error.code = "OPERATION_STATE_CORRUPT";
    throw error;
  }
  state.relayWithdraw = {
    ...state.relayWithdraw,
    ...saved.relayWithdraw,
    resultStatus: saved.relayWithdraw.resultStatus === "checking" ? "unknown" : saved.relayWithdraw.resultStatus,
    resultMessage: `Restored encrypted relay handoff · ${saved.relayWithdraw.resultMessage || "result not checked"}`
  };
  const manager = await currentReservationManager();
  const records = manager
    ? await Promise.all(state.relayWithdraw.reservationIds.map(id => manager.getReservation(id)))
    : [];
  const activeRecords = records.filter(record => record && [
    reservationStatuses.Reserved,
    reservationStatuses.Proving,
    reservationStatuses.ProofReady
  ].includes(record.status));
  const leaseTokens = [...new Set(activeRecords.map(record => record.lease_token).filter(Boolean))];
  if (manager && activeRecords.length === state.relayWithdraw.reservationIds.length && leaseTokens.length === 1) {
    startRelayReservationHeartbeat({
      manager,
      reservationIDs: state.relayWithdraw.reservationIds,
      leaseToken: leaseTokens[0],
      leaseUntil: activeRecords[0].lease_until
    });
  }
}

function preparedReservationIDs(data) {
  return [...new Set(data?.reservation?.reservation_ids || [])];
}

function preparedReservationBinding(data) {
  return data?.reservation && data?.reservationManager
    ? { reservationManager: data.reservationManager, reservation: data.reservation }
    : {};
}

async function withPreparedReservationHeartbeat(data, task) {
  const manager = data?.reservationManager;
  const reservation = data?.reservation;
  const reservationIDs = preparedReservationIDs(data);
  const leaseToken = reservation?.lease_token || reservation?.reservations?.[0]?.lease_token || "";
  if (!manager || !reservationIDs.length || !leaseToken || typeof manager.heartbeatLease !== "function") {
    return task();
  }

  let heartbeatError = null;
  let heartbeatPromise = null;
  const heartbeat = async () => {
    if (heartbeatError) return;
    try {
      const renewed = await manager.heartbeatLease(reservationIDs, { leaseToken });
      reservation.reservations = renewed;
      reservation.lease_until = renewed[0]?.lease_until || reservation.lease_until;
    } catch (error) {
      heartbeatError = error;
    }
  };
  const heartbeatNow = async () => {
    if (!heartbeatPromise) {
      heartbeatPromise = heartbeat().finally(() => {
        heartbeatPromise = null;
      });
    }
    await heartbeatPromise;
  };

  await heartbeatNow();
  if (heartbeatError) throw heartbeatError;
  const intervalMs = reservationHeartbeatIntervalMs({
    leaseDurationMs: manager.leaseDurationMs,
    leaseUntil: reservation.lease_until || reservation.reservations?.[0]?.lease_until
  });
  const timer = globalThis.setInterval(() => { void heartbeatNow(); }, intervalMs);
  let result;
  try {
    result = await task();
  } finally {
    globalThis.clearInterval(timer);
    if (heartbeatPromise) await heartbeatPromise;
  }

  if (heartbeatError) {
    const records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
    if (records.some(record => [reservationStatuses.Proving, reservationStatuses.ProofReady].includes(record.status))) {
      const error = new Error("Note reservation lease heartbeat failed while waiting for wallet or relayer confirmation.", {
        cause: heartbeatError
      });
      error.code = "RESERVATION_HEARTBEAT_FAILED";
      error.preparedPrivacyData = data;
      throw error;
    }
  }
  return result;
}

async function discardPreparedReservation(data, reason = "user_cancelled_before_broadcast") {
  const manager = data?.reservationManager;
  const reservationIDs = preparedReservationIDs(data);
  if (!manager || !reservationIDs.length) return;
  await manager.markReplanRequired(reservationIDs, {
    leaseToken: data.reservation?.lease_token || data.reservation?.reservations?.[0]?.lease_token || "",
    error: reason,
    metadata: {
      reconcile_reason: reason,
      no_broadcast_attempt: true,
      proof_discarded: true
    }
  });
  await refreshReservationState(manager);
}

function stopRelayReservationHeartbeat() {
  if (relayReservationHeartbeatTimer !== null) {
    globalThis.clearInterval(relayReservationHeartbeatTimer);
    relayReservationHeartbeatTimer = null;
  }
}

function startRelayReservationHeartbeat({ manager, reservationIDs, leaseToken, leaseUntil }) {
  stopRelayReservationHeartbeat();
  if (!manager || !reservationIDs.length || !leaseToken) return;
  const heartbeat = () => manager.heartbeatLease(reservationIDs, { leaseToken }).catch(async error => {
    const records = await Promise.allSettled(reservationIDs.map(id => manager.getReservation(id)));
    const stillProofReady = records.some(result => result.status === "fulfilled"
      && result.value.status === reservationStatuses.ProofReady);
    stopRelayReservationHeartbeat();
    if (!stillProofReady) return;
    state.relayWithdraw.resultStatus = "manual-review";
    state.relayWithdraw.resultMessage = `Relay reservation heartbeat failed · ${error.message}`;
    await persistRelayWithdrawRecovery().catch(() => {});
    await refreshReservationState(manager).catch(() => {});
    renderRelayWithdraw();
  });
  const intervalMs = reservationHeartbeatIntervalMs({
    leaseDurationMs: manager.leaseDurationMs,
    leaseUntil
  });
  void heartbeat();
  relayReservationHeartbeatTimer = globalThis.setInterval(() => { void heartbeat(); }, intervalMs);
}

function reservationStatusSlug(status) {
  return String(status || "idle").replace(/([a-z])([A-Z])/g, "$1-$2").toLowerCase();
}

function reservationKindLabel(kind) {
  return String(kind || "privacy spend")
    .replace(/_/g, " ")
    .replace(/\b\w/g, value => value.toUpperCase());
}

function reservationLeaseLabel(assessment) {
  if (!assessment.leaseUntil) return "No active lease";
  const until = new Date(assessment.leaseUntil);
  const timestamp = Number.isNaN(until.getTime()) ? assessment.leaseUntil : until.toLocaleString();
  if (!assessment.leaseLive) return `Expired · ${timestamp}`;
  return assessment.leaseOwnedByCurrentWorker
    ? `This tab · until ${timestamp}`
    : `Another worker · until ${timestamp}`;
}

function appendReservationRecoveryFact(list, label, value) {
  const row = document.createElement("div");
  const term = document.createElement("dt");
  const detail = document.createElement("dd");
  term.textContent = label;
  detail.textContent = value;
  row.append(term, detail);
  list.append(row);
}

function renderReservationRecovery() {
  if (!els.reservationRecovery || !els.reservationRecoveryList) return;
  const operations = groupReservationOperations(state.reservations.active);
  els.reservationRecovery.hidden = operations.length === 0;
  els.reservationRecoveryList.innerHTML = "";
  for (const operation of operations) {
    const assessment = assessReservationRecovery(operation.records, {
      leaseOwner: reservationLeaseOwner
    });
    const item = document.createElement("article");
    item.className = "reservation-recovery-item";

    const header = document.createElement("header");
    const title = document.createElement("strong");
    const badge = document.createElement("span");
    title.textContent = `${reservationKindLabel(assessment.kind)} · ${shorten(assessment.operationKey, 12, 10)}`;
    badge.className = "reservation-recovery-badge";
    badge.textContent = assessment.status;
    header.append(title, badge);

    const facts = document.createElement("dl");
    facts.className = "reservation-recovery-facts";
    appendReservationRecoveryFact(facts, "Reserved notes", String(assessment.reservationIDs.length));
    appendReservationRecoveryFact(facts, "Broadcast", assessment.broadcastAttempted ? "Attempt recorded" : "Not attempted");
    appendReservationRecoveryFact(facts, "Lease", reservationLeaseLabel(assessment));
    appendReservationRecoveryFact(facts, "Recovery", assessment.action === "review-replan" ? "Evidence check available" : "Locked");

    const action = document.createElement("div");
    action.className = "reservation-recovery-action";
    const reason = document.createElement("p");
    const button = document.createElement("button");
    reason.textContent = assessment.reason;
    button.type = "button";
    button.className = "secondary-button";
    button.dataset.recoverReservationOperation = assessment.operationKey;
    button.textContent = state.reservations.recoveringOperationKey === assessment.operationKey
      ? "Checking…"
      : assessment.action === "review-replan"
        ? "Review & replan"
        : assessment.action === "reconcile"
          ? "Use Reconcile"
          : assessment.action === "relay-reconcile"
            ? "Use relay recovery"
            : assessment.action === "wait-for-lease"
              ? "Lease active"
              : "Unavailable";
    button.disabled = assessment.action !== "review-replan"
      || Boolean(state.reservations.recoveringOperationKey);
    button.title = assessment.reason;
    action.append(reason, button);
    item.append(header, facts, action);
    els.reservationRecoveryList.append(item);
  }
}

function renderReservationState() {
  if (!els.reservationState || !els.reconcileReservations) return;
  els.reservationState.textContent = state.reservations.message;
  els.reservationState.dataset.status = reservationStatusSlug(state.reservations.status);
  const canReconcile = Boolean(state.keplr.rootSignatureBase64)
    && state.reservations.active.length > 0
    && !state.reservations.reconciling;
  els.reconcileReservations.disabled = !canReconcile;
  els.reconcileReservations.textContent = state.reservations.reconciling ? "Reconciling…" : "Reconcile";
  renderReservationRecovery();
}

function operationReconciliationStatus(record) {
  return record?.metadata?.operation_status || record?.metadata?.operationStatus || "";
}

function reservationRequiresOperationEvidence(record) {
  return record?.metadata?.operation_success_evidence_required === true
    || record?.metadata?.operationSuccessEvidenceRequired === true
    || record?.operation_success_evidence_required === true
    || record?.operationSuccessEvidenceRequired === true;
}

function unresolvedOperationReservations(records = []) {
  return records.filter(record => reservationRequiresOperationEvidence(record) && [
    operationStatuses.ManualReview,
    operationStatuses.ConflictSpent
  ].includes(operationReconciliationStatus(record)));
}

function summarizeReservationState(records = [], unresolved = []) {
  if (!records.length && !unresolved.length) return defaultReservationState();
  if (!records.length) {
    const operationStatus = operationReconciliationStatus(unresolved[0]) || operationStatuses.ManualReview;
    return {
      status: reservationStatuses.ManualReview,
      message: `${unresolved.length} spent operation · ${operationStatus} · verify tx output evidence before retrying`,
      active: [],
      unresolved,
      retryBlocked: true,
      reconciling: false
    };
  }
  const priority = [
    reservationStatuses.ManualReview,
    reservationStatuses.Unknown,
    reservationStatuses.Submitted,
    reservationStatuses.ProofReady,
    reservationStatuses.Proving,
    reservationStatuses.Reserved
  ];
  const primary = priority.find(status => records.some(record => record.status === status)) || records[0].status;
  const primaryRecords = records.filter(record => record.status === primary);
  const txHash = primaryRecords.map(record => record.submitted_tx_hash).find(Boolean) || "";
  const handedOff = records.some(record => record.metadata?.relay_handed_off === true);
  const detail = txHash ? ` · tx ${shorten(txHash, 12, 10)}` : "";
  const handoffDetail = handedOff ? " · relay handoff recorded" : "";
  return {
    status: primary,
    message: `${records.length} active · ${primary}${detail}${handoffDetail}`,
    active: records,
    unresolved,
    retryBlocked: true,
    reconciling: false
  };
}

async function refreshReservationState(manager = null) {
  const resolvedManager = manager || await currentReservationManager();
  if (!resolvedManager) {
    state.reservations = defaultReservationState();
    renderReservationState();
    return [];
  }
  try {
    const [active, allReservations, noteStatuses] = await Promise.all([
      resolvedManager.listActiveReservations(),
      resolvedManager.store.listReservations({ ownerKeyId: resolvedManager.ownerKeyId }),
      resolvedManager.reservationStatusByNote(state.keplr.notes)
    ]);
    const unresolved = unresolvedOperationReservations(allReservations);
    const reconciling = state.reservations.reconciling;
    const recoveringOperationKey = state.reservations.recoveringOperationKey;
    state.reservations = summarizeReservationState(active, unresolved);
    state.reservations.reconciling = reconciling;
    state.reservations.recoveringOperationKey = recoveringOperationKey;
    state.reservations.noteStatuses = noteStatuses;
    renderReservationState();
    renderMyKeplrNotes();
    updateAmountActionButtons();
    return active;
  } catch (error) {
    state.reservations = {
      status: "error",
      message: error.message,
      active: state.reservations.active,
      unresolved: state.reservations.unresolved,
      retryBlocked: true,
      reconciling: false,
      recoveringOperationKey: state.reservations.recoveringOperationKey,
      noteStatuses: state.reservations.noteStatuses
    };
    renderReservationState();
    updateAmountActionButtons();
    throw error;
  }
}

function noteHasSpentEvidence(note) {
  return note?.spent === true
    || note?.isSpent === true
    || String(note?.nullifier_status || note?.nullifierStatus || "").toLowerCase() === "spent";
}

function noteHasUnspentEvidence(note) {
  return note?.spent !== true
    && note?.isSpent !== true
    && String(note?.nullifier_status || note?.nullifierStatus || "").toLowerCase() === "unspent";
}

function privacyTransferEventNullifiers(event) {
  return ["nullifier_1", "nullifier_2"]
    .map(key => normalizedHex(eventAttribute(event, key)))
    .filter(Boolean);
}

function transferEventMatchesOperation(event, records, notesByLookupKey) {
  const nullifiers = records
    .map(record => noteNullifier(notesByLookupKey.get(record.nullifier_lookup_key)))
    .filter(Boolean);
  if (nullifiers.length !== records.length || event?.event_type !== "shielded_transfer") return false;
  const eventNullifiers = new Set(privacyTransferEventNullifiers(event));
  return nullifiers.every(nullifier => eventNullifiers.has(normalizedHex(nullifier)));
}

function transferEventForOperation(records, notesByLookupKey, txHash = "") {
  const expectedTxHash = normalizedTxHash(txHash);
  return state.privacyEvents.events.find(event => (
    (!expectedTxHash || normalizedTxHash(event?.tx_hash_hex) === expectedTxHash)
      && transferEventMatchesOperation(event, records, notesByLookupKey)
  )) || null;
}

function authoritativeTransactionHeight(check = {}) {
  try {
    const value = typeof check.height === "string" && /^0x[0-9a-f]+$/i.test(check.height)
      ? Number(BigInt(check.height))
      : Number(check.height);
    return Number.isSafeInteger(value) && value > 0 ? value : 0;
  } catch {
    return 0;
  }
}

async function operationEventForReservations(records, notesByLookupKey) {
  const txHashes = new Map();
  for (const record of records) {
    const raw = String(record.submitted_tx_hash || "").trim();
    const normalized = normalizedTxHash(raw);
    if (normalized) txHashes.set(normalized, raw);
  }
  if (txHashes.size !== 1) return { complete: true, event: null };
  const txHash = [...txHashes.values()][0];
  const local = transferEventForOperation(records, notesByLookupKey, txHash);
  if (local) return { complete: true, event: local };

  const check = await checkReservationTransaction(txHash);
  if (check.pending || (!check.included && !check.failed && !check.absent)) {
    return { complete: false, event: null };
  }
  if (!check.included) return { complete: true, event: null };
  const height = authoritativeTransactionHeight(check);
  if (!height) throw new Error(`Included transaction ${txHash} has no authoritative height`);
  const event = await findPrivacyEventByTxHash({
    fetchPage: options => clairveilBrowserClient().fetchPrivacyEvents(options),
    txHash,
    height,
    predicate: candidate => transferEventMatchesOperation(candidate, records, notesByLookupKey)
  });
  return { complete: true, event };
}

function operationEvidenceFromEvent(records, event) {
  const first = records[0];
  return {
    txHash: normalizedHex(event?.tx_hash_hex),
    outputCommitment: normalizedHex(eventAttribute(event, "commitment_1")),
    auditDisclosureDigest: normalizedHex(eventAttribute(event, "audit_disclosure_digest")),
    recipientHash: first.expected_recipient_hash,
    amount: first.expected_amount,
    amountHash: first.expected_amount_hash,
    denom: first.expected_denom,
    batchItemIndex: first.batch_item_index,
    batchItemIndexKnown: first.batch_item_index_known
  };
}

async function reconcileSpentReservations(manager, notes = state.keplr.notes) {
  if (!manager) return [];
  const spent = (notes || []).filter(noteHasSpentEvidence);
  if (!spent.length) return [];
  const [active, allReservations] = await Promise.all([
    manager.listActiveReservations(),
    manager.store.listReservations({ ownerKeyId: manager.ownerKeyId })
  ]);
  const activeIDs = new Set(active.map(record => record.reservation_id));
  const candidates = allReservations.filter(record => activeIDs.has(record.reservation_id)
    || (reservationRequiresOperationEvidence(record) && [
      operationStatuses.ManualReview,
      operationStatuses.ConflictSpent
    ].includes(operationReconciliationStatus(record))));
  const notesByLookupKey = new Map();
  for (const note of spent) {
    notesByLookupKey.set(await manager.lookupKeyForNote(note), note);
  }
  const groups = new Map();
  for (const record of candidates) {
    const key = record.operation_id || record.reservation_id;
    const group = groups.get(key) || [];
    group.push(record);
    groups.set(key, group);
  }
  const evidenceByLookupKey = new Map();
  const eligibleLookupKeys = new Set();
  for (const records of groups.values()) {
    const spentRecords = records.filter(record => notesByLookupKey.has(record.nullifier_lookup_key));
    if (!spentRecords.length) continue;
    if (!records.some(reservationRequiresOperationEvidence)) {
      spentRecords.forEach(record => eligibleLookupKeys.add(record.nullifier_lookup_key));
      continue;
    }
    const lookup = await operationEventForReservations(spentRecords, notesByLookupKey);
    if (!lookup.complete) continue;
    const event = lookup.event;
    const operationSuccessEvidence = event ? operationEvidenceFromEvent(spentRecords, event) : null;
    for (const record of spentRecords) {
      eligibleLookupKeys.add(record.nullifier_lookup_key);
      if (operationSuccessEvidence) {
        evidenceByLookupKey.set(record.nullifier_lookup_key, operationSuccessEvidence);
      }
    }
  }
  const eligible = [...notesByLookupKey.entries()]
    .filter(([lookupKey]) => eligibleLookupKeys.has(lookupKey))
    .map(([lookupKey, note]) => ({
      ...note,
      spent: true,
      isSpent: true,
      nullifierStatus: "spent",
      ...(evidenceByLookupKey.has(lookupKey)
        ? { operationSuccessEvidence: evidenceByLookupKey.get(lookupKey) }
        : {})
    }));
  return eligible.length ? manager.reconcileSpentNotes(eligible) : [];
}

function injectedEthereumProviders() {
  const provider = window.ethereum;
  if (!provider) return [];
  const providers = Array.isArray(provider.providers) ? provider.providers : [];
  return [...new Set([...providers, provider])].filter(candidate => candidate?.request);
}

function metaMaskProvider() {
  const providers = injectedEthereumProviders();
  return providers.find(provider => provider.isMetaMask)
    || providers.find(provider => provider.isRabby || provider.isBraveWallet || provider.isCoinbaseWallet)
    || providers[0]
    || null;
}

function unsupportedEvmMethodError(error) {
  return error?.code === -32601
    || /method .*not supported|not supported|unsupported method|does not support/i.test(error?.message || "");
}

async function requestMetaMask(payload) {
  const provider = metaMaskProvider();
  if (!provider) {
    throw new Error("MetaMask not found");
  }
  try {
    return await provider.request(payload);
  } catch (error) {
    const method = payload?.method || "EVM request";
    if (unsupportedEvmMethodError(error)) {
      throw new Error(`${method} is not supported by the injected wallet provider. Open this DApp in a browser with MetaMask or another EVM wallet selected.`);
    }
    throw error;
  }
}

async function ensureMetaMaskChain() {
  if (!metaMaskProvider()) {
    throw new Error("MetaMask not found");
  }
  const expected = expectedEvmChainIdHex();
  if (!expected) return;

  const current = await requestMetaMask({ method: "eth_chainId" });
  if (String(current).toLowerCase() === expected.toLowerCase()) {
    state.wallet.chainId = current;
    return;
  }

  try {
    await requestMetaMask({
      method: "wallet_switchEthereumChain",
      params: [{ chainId: expected }]
    });
  } catch (error) {
    const unknownChain = error?.code === 4902 || /unknown|unrecognized|not added/i.test(error?.message || "");
    if (!unknownChain) {
      throw error;
    }
    await requestMetaMask({
      method: "wallet_addEthereumChain",
      params: [{
        chainId: expected,
        chainName: state.config?.evmChainName || "EVM Localnet",
        nativeCurrency: {
          name: displayDenom(),
          symbol: displayDenom(),
          decimals: coinDecimals()
        },
        rpcUrls: [evmRpcUrlForWallet()]
      }]
    });
  }

  const updated = await requestMetaMask({ method: "eth_chainId" });
  state.wallet.chainId = updated;
  if (String(updated).toLowerCase() !== expected.toLowerCase()) {
    throw new Error(`MetaMask chain must be ${expected}, current ${updated}`);
  }
  renderWallet();
}

function coinDecimals() {
  return Number(activeChainProfile()?.coinDecimals ?? state.config?.coinDecimals ?? 18);
}

function coinTextFromAmount(amount) {
  return `${amount}${baseDenom()}`;
}

function zeroCoinText() {
  return coinTextFromAmount("0");
}

const els = {
  modeBadge: $("#modeBadge"),
  walletStatus: $("#walletStatus"),
  dappChainSelect: $("#dappChainSelect"),
  dappChainHint: $("#dappChainHint"),
  connectWallet: $("#connectWallet"),
  connectKeplr: $("#connectKeplr"),
  disconnectWallet: $("#disconnectWallet"),
  signSession: $("#signSession"),
  sessionWallet: $("#sessionWallet"),
  walletAccount: $("#walletAccount"),
  copyWalletAccount: $("#copyWalletAccount"),
  walletChain: $("#walletChain"),
  walletSignatureHash: $("#walletSignatureHash"),
  keplrName: $("#keplrName"),
  keplrPubkey: $("#keplrPubkey"),
  keplrSignerCheck: $("#keplrSignerCheck"),
  keplrBalance: $("#keplrBalance"),
  keplrFaucetHash: $("#keplrFaucetHash"),
  keplrFaucetSent: $("#keplrFaucetSent"),
  keplrFaucetRecipient: $("#keplrFaucetRecipient"),
  keplrShieldedAddress: $("#keplrShieldedAddress"),
  copyKeplrShieldedAddress: $("#copyKeplrShieldedAddress"),
  keplrDisclosurePubKey: $("#keplrDisclosurePubKey"),
  copyKeplrDisclosurePubKey: $("#copyKeplrDisclosurePubKey"),
  faucetHelpText: $("#faucetHelpText"),
  faucetRow: $(".faucet-row"),
  keplrFaucetAmount: $("#keplrFaucetAmount"),
  fundKeplr: $("#fundKeplr"),
  setupKeplrPrivacy: $("#setupKeplrPrivacy"),
  refreshWalletBalance: $("#refreshKeplrBalance"),
  scanKeplrNotes: $("#scanKeplrNotes"),
  noteScanEndpoint: $("#noteScanEndpoint"),
  backupNoteCache: $("#backupNoteCache"),
  resetRescanNotes: $("#resetRescanNotes"),
  noteRollbackHeight: $("#noteRollbackHeight"),
  rollbackRescanNotes: $("#rollbackRescanNotes"),
  noteSyncState: $("#noteSyncState"),
  reservationState: $("#reservationState"),
  reconcileReservations: $("#reconcileReservations"),
  reservationRecovery: $("#reservationRecovery"),
  reservationRecoveryList: $("#reservationRecoveryList"),
  keplrTxState: $("#keplrTxState"),
  keplrSendAmount: $("#keplrSendAmount"),
  keplrSendRecipient: $("#keplrSendRecipient"),
  keplrSendRecipientSuggestions: $("#keplrSendRecipientSuggestions"),
  sendFromKeplr: $("#sendFromKeplr"),
  reconcileKeplrSend: $("#reconcileKeplrSend"),
  clearPublicPendingState: $("#clearPublicPendingState"),
  keplrDepositAmount: $("#keplrDepositAmount"),
  depositFromKeplr: $("#depositFromKeplr"),
  reconcileKeplrDeposit: $("#reconcileKeplrDeposit"),
  keplrSendHash: $("#keplrSendHash"),
  keplrDepositHash: $("#keplrDepositHash"),
  keplrDepositHeight: $("#keplrDepositHeight"),
  keplrDepositRecovery: $("#keplrDepositRecovery"),
  keplrDepositNetworkFee: $("#keplrDepositNetworkFee"),
  myClairBalance: $("#myClairBalance"),
  myKeplrSpendable: $("#myKeplrSpendable"),
  myKeplrSpendableOnly: $("#myKeplrSpendableOnly"),
  myKeplrNotesList: $("#myKeplrNotesList"),
  veiledTransferAmount: $("#veiledTransferAmount"),
  veiledTransferRecipient: $("#veiledTransferRecipient"),
  veiledTransferRecipientSuggestions: $("#veiledTransferRecipientSuggestions"),
  veiledDisclosureAdvanced: $("#veiledDisclosureAdvanced"),
  veiledDisclosureOptions: $("#veiledDisclosureOptions"),
  veiledDisclosureMode: $("#veiledDisclosureMode"),
  veiledDisclosurePubKey: $("#veiledDisclosurePubKey"),
  veiledDisclosureAmount: $("#veiledDisclosureAmount"),
  veiledDisclosureFrom: $("#veiledDisclosureFrom"),
  veiledDisclosureTo: $("#veiledDisclosureTo"),
  includeSelfViewDisclosure: $("#includeSelfViewDisclosure"),
  selfViewWarning: $("#selfViewWarning"),
  transferFromVeiled: $("#transferFromVeiled"),
  veiledWithdrawAmount: $("#veiledWithdrawAmount"),
  veiledWithdrawRecipient: $("#veiledWithdrawRecipient"),
  veiledWithdrawRecipientSuggestions: $("#veiledWithdrawRecipientSuggestions"),
  withdrawMode: $("#withdrawMode"),
  withdrawFromVeiled: $("#withdrawFromVeiled"),
  relayWithdrawPanel: $("#relayWithdrawPanel"),
  relayWithdrawChain: $("#relayWithdrawChain"),
  relayWithdrawRecipient: $("#relayWithdrawRecipient"),
  relayWithdrawExpiry: $("#relayWithdrawExpiry"),
  relayWithdrawPayloadHash: $("#relayWithdrawPayloadHash"),
  relayWithdrawJson: $("#relayWithdrawJson"),
  relayWithdrawTxHash: $("#relayWithdrawTxHash"),
  relayWithdrawResult: $("#relayWithdrawResult"),
  reconcileRelayWithdraw: $("#reconcileRelayWithdraw"),
  copyRelayWithdraw: $("#copyRelayWithdraw"),
  downloadRelayWithdraw: $("#downloadRelayWithdraw"),
  keplrTransferHash: $("#keplrTransferHash"),
  keplrWithdrawHash: $("#keplrWithdrawHash"),
  keplrWithdrawHeight: $("#keplrWithdrawHeight"),
  keplrWithdrawNullifier: $("#keplrWithdrawNullifier"),
  keplrWithdrawReceive: $("#keplrWithdrawReceive"),
  localHome: $("#localHome"),
  localHomeRow: $("#localHome")?.closest("div"),
  faucetHashRow: $("#keplrFaucetHash")?.closest("div"),
  faucetSentRow: $("#keplrFaucetSent")?.closest("div"),
  faucetRecipientRow: $("#keplrFaucetRecipient")?.closest("div"),
  blockHeight: $("#blockHeight"),
  leafCount: $("#leafCount"),
  chainId: $("#chainId"),
  restState: $("#restState"),
  protocolState: $("#protocolState"),
  reserveState: $("#reserveState"),
  depositProofState: $("#depositProofState"),
  accountSelect: $("#accountSelect"),
  transparentAddress: $("#transparentAddress"),
  shieldedAddress: $("#shieldedAddress"),
  balanceValue: $("#balanceValue"),
  refreshAll: $("#refreshAll"),
  refreshNotes: $("#refreshNotes"),
  localSignerNotesTitle: $("#localSignerNotesTitle"),
  spendableTotal: $("#spendableTotal"),
  notesList: $("#notesList"),
  localSignerPanel: $(".local-signer-panel"),
  refreshEvents: $("#refreshEvents"),
  eventsList: $("#eventsList"),
  blockEventsList: $("#blockEventsList"),
  blockEventsState: $("#blockEventsState"),
  eventDetailType: $("#eventDetailType"),
  eventDetailHeight: $("#eventDetailHeight"),
  eventDetailTx: $("#eventDetailTx"),
  eventDetailTarget: $("#eventDetailTarget"),
  eventDetailUserMode: $("#eventDetailUserMode"),
  eventDisclosurePlane: $("#eventDisclosurePlane"),
  eventDisclosurePolicy: $("#eventDisclosurePolicy"),
  eventDisclosureOutputIndex: $("#eventDisclosureOutputIndex"),
  eventDisclosureCommitment: $("#eventDisclosureCommitment"),
  eventDisclosureDigest: $("#eventDisclosureDigest"),
  eventDisclosureVerified: $("#eventDisclosureVerified"),
  eventDisclosureFields: $("#eventDisclosureFields"),
  eventDisclosureAmount: $("#eventDisclosureAmount"),
  eventDisclosureFrom: $("#eventDisclosureFrom"),
  eventDisclosureTo: $("#eventDisclosureTo"),
  eventDisclosureState: $("#eventDisclosureState"),
  decodeEventDisclosure: $("#decodeEventDisclosure"),
  decodeSelfViewDisclosure: $("#decodeSelfViewDisclosure"),
  disclosureSourcePlane: $("#disclosureSourcePlane"),
  disclosureSourceTxHash: $("#disclosureSourceTxHash"),
  disclosureSourceEventJson: $("#disclosureSourceEventJson"),
  decodeDisclosureSource: $("#decodeDisclosureSource"),
  refreshAuditorTransfers: $("#refreshAuditorTransfers"),
  auditorEventsList: $("#auditorEventsList"),
  auditorDecodeState: $("#auditorDecodeState"),
  auditorTxHash: $("#auditorTxHash"),
  auditorVerification: $("#auditorVerification"),
  auditorAmount: $("#auditorAmount"),
  auditorFrom: $("#auditorFrom"),
  auditorTo: $("#auditorTo"),
  auditorFields: $("#auditorFields"),
  auditorDigest: $("#auditorDigest"),
  auditorPlanePolicy: $("#auditorPlanePolicy"),
  auditorOutputIndex: $("#auditorOutputIndex"),
  auditorCommitment: $("#auditorCommitment"),
  auditorTestScalar: $("#auditorTestScalar"),
  decodeAuditorTransfer: $("#decodeAuditorTransfer"),
  auditorSection: $(".auditor-section"),
  noticeModal: $("#noticeModal"),
  noticeTitle: $("#noticeTitle"),
  noticeMessage: $("#noticeMessage"),
  closeNoticeModal: $("#closeNoticeModal"),
  transferFlowModal: $("#transferFlowModal"),
  transferFlowTitle: $("#transferFlowTitle"),
  transferModalState: $("#transferModalState"),
  transferModalLead: $("#transferModalLead"),
  transferSteps: $("#transferSteps"),
  transferStepZero: $("#transferStepZero"),
  transferStepZeroTitle: $("#transferStepZeroTitle"),
  transferStepZeroCopy: $("#transferStepZeroCopy"),
  transferStepTransfer: $("#transferStepTransfer"),
  transferStepTransferTitle: $("#transferStepTransferTitle"),
  transferStepTransferCopy: $("#transferStepTransferCopy"),
  transferSuccessPanel: $("#transferSuccessPanel"),
  transferSuccessTitle: $("#transferSuccessTitle"),
  transferSuccessCopy: $("#transferSuccessCopy"),
  transferFailurePanel: $("#transferFailurePanel"),
  transferFailureTitle: $("#transferFailureTitle"),
  transferFailureReason: $("#transferFailureReason"),
  reviewChain: $("#reviewChain"),
  reviewRecipient: $("#reviewRecipient"),
  reviewAmount: $("#reviewAmount"),
  reviewDisclosure: $("#reviewDisclosure"),
  reviewSelfView: $("#reviewSelfView"),
  reviewChangeEffect: $("#reviewChangeEffect"),
  reviewExpiry: $("#reviewExpiry"),
  transferPlannerFacts: $("#transferPlannerFacts"),
  transferPlannerRequested: $("#transferPlannerRequested"),
  transferPlannerCurrentMax: $("#transferPlannerCurrentMax"),
  transferPlannerAction: $("#transferPlannerAction"),
  cancelTransferFlow: $("#cancelTransferFlow"),
  retryTransferFlow: $("#retryTransferFlow"),
  confirmTransferFlow: $("#confirmTransferFlow")
};

function shorten(value, head = 10, tail = 8) {
  if (!value || value.length <= head + tail + 3) return value || "-";
  return `${value.slice(0, head)}...${value.slice(-tail)}`;
}

function eventAttribute(event, key) {
  return (event?.attributes || []).find(attribute => attribute.key === key)?.value || "";
}

function prettyDisclosureField(value) {
  return String(value || "").replace(/_/g, " ");
}

function renderTransferDisclosureAdvanced() {
  els.veiledDisclosureOptions.hidden = !els.veiledDisclosureAdvanced.checked;
  const mode = els.veiledDisclosureMode?.value || "none";
  const noDisclosure = mode === "none";
  const isPublic = mode === "public";
  const disableTarget = !els.veiledDisclosureAdvanced.checked || noDisclosure || isPublic;
  els.veiledDisclosurePubKey.disabled = disableTarget;
  els.veiledDisclosurePubKey.closest(".field").classList.toggle("muted", disableTarget);
  [
    els.veiledDisclosureAmount,
    els.veiledDisclosureFrom,
    els.veiledDisclosureTo
  ].forEach(checkbox => {
    checkbox.disabled = !els.veiledDisclosureAdvanced.checked || noDisclosure;
    checkbox.closest(".checkbox-control").classList.toggle("muted", noDisclosure);
  });
  els.selfViewWarning.hidden = els.includeSelfViewDisclosure.checked;
}

function transferDisclosurePolicy() {
  const disableSelfViewDisclosure = !els.includeSelfViewDisclosure.checked;
  if (!els.veiledDisclosureAdvanced.checked) {
    return {
      privacyPolicy: "all-private",
      disableSelfViewDisclosure
    };
  }

  const disclosureMode = els.veiledDisclosureMode?.value || "recipient-encrypted";
  const pubKeyHex = els.veiledDisclosurePubKey.value.trim();

  if (disclosureMode === "none") {
    return {
      privacyPolicy: "all-private",
      disclosureMode,
      disableSelfViewDisclosure
    };
  }

  const amount = els.veiledDisclosureAmount.checked;
  const from = els.veiledDisclosureFrom.checked;
  const to = els.veiledDisclosureTo.checked;
  if (!amount && !from && !to) {
    throw new Error("Advanced disclosure에서 공개할 항목을 하나 이상 선택해줘.");
  }

  const privacyPolicy = [
    amount ? "amount" : "",
    from ? "from" : "",
    to ? "to" : ""
  ].filter(Boolean).join("-");

  if (disclosureMode === "public") {
    return {
      privacyPolicy,
      disclosureMode,
      disableSelfViewDisclosure
    };
  }

  if (!/^[0-9a-fA-F]{64}$/.test(pubKeyHex)) {
    throw new Error("Disclosure target은 show-disclosure-pubkey로 만든 32-byte hex 값을 넣어줘.");
  }

  return {
    privacyPolicy,
    disclosureMode: "recipient-encrypted",
    disclosurePubKeyHex: pubKeyHex,
    disableSelfViewDisclosure
  };
}

function transferDisclosureSummary(disclosure = {}) {
  const mode = disclosure.disclosureMode || "none";
  const policy = disclosure.privacyPolicy || "all-private";
  const userDisclosure = mode === "none" ? "None (all private)" : `${mode} · ${policy}`;
  return `User: ${userDisclosure} · Mandatory audit: full`;
}

function setBusy(element, busy) {
  element.disabled = busy;
  element.setAttribute("aria-busy", busy ? "true" : "false");
}

function closeNoticeModal() {
  els.noticeModal.hidden = true;
  els.noticeModal.classList.remove("visible", "failed");
}

function showNotice({ title = "Clairveil", message, failed = false }) {
  els.noticeTitle.textContent = title;
  els.noticeMessage.textContent = message;
  els.noticeModal.classList.toggle("failed", failed);
  els.noticeModal.hidden = false;
  requestAnimationFrame(() => els.noticeModal.classList.add("visible"));
  els.closeNoticeModal.focus();
}

function toast(message) {
  showNotice({ message });
}

function showSendResult({ success, wallet, txHash, error }) {
  if (success) {
    showNotice({
      title: "Send 요청됨",
      message: `${wallet} send가 제출되었습니다.\nTx: ${shorten(txHash, 14, 12)}`
    });
    return;
  }

  showNotice({
    title: "Send 실패",
    message: error || "Send 요청이 완료되지 않았습니다.",
    failed: true
  });
}

const transferFlowState = {
  resolve: null,
  running: false,
  confirmationStage: "initial",
  copy: null,
  controller: null,
  retry: null,
  review: null
};

const transferFlowSteps = [
  { key: "zero", element: () => els.transferStepZero },
  { key: "transfer", element: () => els.transferStepTransfer }
];

const privacyFlowCopies = {
  transfer: {
    title: "Privacy Transfer 확인",
    lead: "입력하신 금액을 보낼 수 있도록 note 구성을 먼저 확인합니다. 필요한 경우 self transaction 서명이 먼저 요청됩니다.",
    runningLead: "Keplr 창이 뜨면 현재 단계의 내용을 확인하고 서명해 주세요.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "요청이 완료되지 않았습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy: "입력하신 금액의 노트를 만들기 위해 self transaction이 필요한지 확인합니다.",
    stepTwoTitle: "트랜스퍼 서명",
    stepTwoCopy: "준비된 note로 실제 privacy transfer를 요청합니다. Keplr에서 내용을 확인하고 서명합니다.",
    successTitle: "트랜스퍼 요청이 성공하였습니다",
    successCopy: "최신 notes를 다시 스캔한 상태입니다.",
    failureTitle: "트랜스퍼 요청이 실패했습니다"
  },
  withdraw: {
    title: "Privacy Withdraw 확인",
    lead: "Clair로 출금하려면 입력 금액과 정확히 같은 note가 필요합니다. 없으면 먼저 self transaction 서명이 요청됩니다.",
    runningLead: "Keplr 창이 뜨면 현재 단계의 내용을 확인하고 서명해 주세요.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "요청이 완료되지 않았습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy: "Withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다. 없으면 내 Veiled balance 안에서 note를 재구성합니다.",
    stepTwoTitle: "위드드로우 서명",
    stepTwoCopy: "준비된 note로 실제 withdraw를 요청합니다. Keplr에서 받을 Clair 주소와 금액을 확인하고 서명합니다.",
    successTitle: "Withdraw 요청이 성공하였습니다",
    successCopy: "Clair balance와 최신 notes를 다시 불러온 상태입니다.",
    failureTitle: "Withdraw 요청이 실패했습니다"
  },
  relay: {
    title: "Relay Withdraw Payload 확인",
    lead: "Relayer에 전달할 payload를 준비합니다. Chain ID, recipient, 금액, 만료 시각은 생성 후 변경할 수 없습니다.",
    runningLead: "같은 prover endpoint에서 relay withdraw proof를 생성하고 있습니다.",
    doneLead: "Relayer handoff payload가 준비되었습니다.",
    failedLead: "Relay payload를 준비하지 못했습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy: "Relay withdraw에 사용할 정확한 금액의 note를 확인합니다. 없으면 self transaction으로 재구성합니다.",
    stepTwoTitle: "Payload 생성",
    stepTwoCopy: "서명·브로드캐스트하지 않고 relayer 검증용 payload를 생성합니다.",
    successTitle: "Relay payload가 준비되었습니다",
    successCopy: "아래 JSON을 신뢰할 수 있는 relayer transport로 전달하세요.",
    failureTitle: "Relay payload 준비가 실패했습니다"
  }
};

class ApiError extends Error {
  constructor(data, statusCode) {
    super(data?.error || "Request failed");
    this.name = "ApiError";
    this.statusCode = statusCode;
    this.code = data?.code || "";
    this.status = data?.status || data?.plan?.status || "";
    this.plan = data?.plan || null;
    this.prepared = data?.prepared || null;
    this.data = data || {};
  }
}

function browserDataLoadErrorMessage(error) {
  const message = error?.message || String(error || "Request failed");
  if (/failed to fetch|load failed|networkerror|network request failed/i.test(message)) {
    return `${message}. Browser cannot reach the selected chain REST/RPC endpoint; enable CORS or expose browser-accessible RPC/REST URLs.`;
  }
  return message;
}

function renderLocalSignerUnavailable(error) {
  const message = error?.message || "Local signer helper is unavailable.";
  els.shieldedAddress.textContent = "-";
  els.balanceValue.textContent = "-";
  els.spendableTotal.textContent = zeroCoinText();
  els.notesList.innerHTML = "";
  const empty = document.createElement("p");
  empty.className = "empty";
  empty.textContent = message;
  els.notesList.append(empty);
}

function coinText(value, fallback = "-") {
  const text = String(value || "").trim();
  if (!text) return fallback;
  return text.endsWith(baseDenom()) ? text : `${text}${baseDenom()}`;
}

function resetTransferPlannerFacts() {
  els.transferPlannerFacts.hidden = true;
  els.transferPlannerRequested.textContent = "-";
  els.transferPlannerCurrentMax.textContent = "-";
  els.transferPlannerCurrentMax.closest("div").hidden = false;
  els.transferPlannerAction.textContent = "-";
}

function showTransferPlannerFacts({ requested, currentMax, action }) {
  const currentMaxRow = els.transferPlannerCurrentMax.closest("div");
  const hasCurrentMax = currentMax !== undefined && currentMax !== null && String(currentMax).trim() !== "";
  els.transferPlannerFacts.hidden = false;
  els.transferPlannerRequested.textContent = coinText(requested);
  currentMaxRow.hidden = !hasCurrentMax;
  els.transferPlannerCurrentMax.textContent = hasCurrentMax ? coinText(currentMax) : "-";
  els.transferPlannerAction.textContent = action || "-";
}

function parsePlannerAmountValue(value) {
  const text = String(value || "").trim();
  const raw = text.endsWith(baseDenom()) ? text.slice(0, -baseDenom().length) : text;
  if (!/^\d+$/.test(raw)) return null;
  return BigInt(raw);
}

function preparedTransferChangeEffect(data) {
  const selectedInputTotal = parsePlannerAmountValue(data?.prepared?.selectedInputTotal);
  const finalAmount = parsePlannerAmountValue(data?.prepared?.finalAmount ?? data?.prepared?.amount);
  if (selectedInputTotal === null || finalAmount === null || selectedInputTotal < finalAmount) {
    throw new Error("Prepared transfer omitted a valid recipient/change effect");
  }
  const change = selectedInputTotal - finalAmount;
  const changeRecipient = data?.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  return `${coinTextFromAmount(change.toString())} returned to ${shorten(changeRecipient, 16, 12)}`;
}

function plannerCurrentTransferMaxForNoteMerge(data, requested) {
  const facts = data?.plan?.facts || {};
  const requestedValue = parsePlannerAmountValue(requested);
  const currentTransferMax = facts.selectedInputTotalValue
    || facts.selectedInputTotal
    || data?.plan?.nextAmount
    || data?.prepared?.amount;
  const currentTransferMaxValue = parsePlannerAmountValue(currentTransferMax);
  if (requestedValue === null || currentTransferMaxValue === null || currentTransferMaxValue >= requestedValue) {
    return "";
  }
  return facts.selectedInputTotal || facts.selectedInputTotalValue || data?.plan?.nextAmount || data?.prepared?.amount || "";
}

function plannerCurrentExactNoteMaxForWithdraw(data, requested) {
  const facts = data?.plan?.facts || {};
  const requestedValue = parsePlannerAmountValue(requested);
  const currentExactNoteMax = facts.currentMaxNoteValue || facts.currentMaxNote;
  const currentExactNoteMaxValue = parsePlannerAmountValue(currentExactNoteMax);
  if (requestedValue === null || currentExactNoteMaxValue === null || currentExactNoteMaxValue >= requestedValue) {
    return "";
  }
  return facts.currentMaxNote || facts.currentMaxNoteValue || "";
}

function applyPrivacyFlowCopy(kind = "transfer") {
  const copy = privacyFlowCopies[kind] || privacyFlowCopies.transfer;
  transferFlowState.copy = copy;
  els.transferFlowTitle.textContent = copy.title;
  els.transferModalLead.textContent = copy.lead;
  els.transferStepZeroTitle.textContent = copy.stepOneTitle;
  els.transferStepZeroCopy.textContent = copy.stepOneCopy;
  els.transferStepTransferTitle.textContent = copy.stepTwoTitle;
  els.transferStepTransferCopy.textContent = copy.stepTwoCopy;
  els.transferSuccessTitle.textContent = copy.successTitle;
  els.transferSuccessCopy.textContent = copy.successCopy;
  els.transferFailureTitle.textContent = copy.failureTitle;
}

function closeTransferFlowModal(result = false) {
  const { resolve } = transferFlowState;
  transferFlowState.resolve = null;
  transferFlowState.running = false;
  transferFlowState.controller = null;
  els.transferFlowModal.hidden = true;
  els.transferFlowModal.classList.remove("visible");
  if (resolve) {
    resolve(result);
  }
}

function renderTransferReview(review = {}) {
  els.reviewChain.textContent = review.chainId || "-";
  els.reviewRecipient.textContent = review.recipient || "-";
  els.reviewAmount.textContent = review.amount || "-";
  els.reviewDisclosure.textContent = review.disclosure || "Not applicable";
  els.reviewSelfView.textContent = review.selfView || "Not applicable";
  els.reviewChangeEffect.textContent = review.changeEffect || "Pending payload preparation";
  els.reviewExpiry.textContent = review.expiresAtUnix
    ? `${new Date(review.expiresAtUnix * 1000).toLocaleString()} (${review.expiresAtUnix})`
    : "-";
}

function setTransferFlowStep(activeKey, stateText) {
  if (stateText) {
    els.transferModalState.textContent = stateText;
  }

  const activeIndex = transferFlowSteps.findIndex(step => step.key === activeKey);
  for (const [index, step] of transferFlowSteps.entries()) {
    const element = step.element();
    const isActive = step.key === activeKey;
    const isDone = activeKey === "done" || (activeIndex > -1 && index < activeIndex);
    element.classList.toggle("active", isActive);
    element.classList.toggle("done", isDone);
  }
}

function openTransferFlowModal(kind = "transfer", review = {}) {
  applyPrivacyFlowCopy(kind);
  transferFlowState.running = false;
  transferFlowState.confirmationStage = "initial";
  transferFlowState.controller = null;
  transferFlowState.retry = null;
  transferFlowState.review = review;
  renderTransferReview(review);
  els.transferSteps.hidden = false;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferFlowModal.classList.remove("failed");
  els.cancelTransferFlow.textContent = "취소";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.confirmTransferFlow.hidden = false;
  els.confirmTransferFlow.disabled = false;
  els.confirmTransferFlow.textContent = "시작";
  els.retryTransferFlow.hidden = true;
  els.retryTransferFlow.disabled = false;
  resetTransferPlannerFacts();
  setTransferFlowStep("", "확인 필요");
  els.transferFlowModal.hidden = false;
  requestAnimationFrame(() => els.transferFlowModal.classList.add("visible"));
  els.confirmTransferFlow.focus();
  return new Promise(resolve => {
    transferFlowState.resolve = resolve;
  });
}

function confirmTransferFlowStart() {
  if (!transferFlowState.resolve) return;
  const resolve = transferFlowState.resolve;
  transferFlowState.resolve = null;
  transferFlowState.running = true;
  if (transferFlowState.confirmationStage === "initial") {
    transferFlowState.controller = new AbortController();
  }
  els.cancelTransferFlow.textContent = "Proof 요청 취소";
  els.cancelTransferFlow.hidden = false;
  els.confirmTransferFlow.hidden = true;
  els.transferModalLead.textContent = transferFlowState.copy?.runningLead || privacyFlowCopies.transfer.runningLead;
  resolve(true);
}

function requestPreparedTransferConfirmation(review) {
  transferFlowState.confirmationStage = "final";
  transferFlowState.running = false;
  transferFlowState.review = review;
  renderTransferReview(review);
  els.transferSteps.hidden = true;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferModalState.textContent = "최종 확인 필요";
  els.transferModalLead.textContent = "Prepared payload의 recipient, change, disclosure, chain, 만료 시각을 확인한 뒤 wallet 서명을 진행하세요.";
  els.cancelTransferFlow.textContent = "취소";
  els.cancelTransferFlow.disabled = false;
  els.cancelTransferFlow.hidden = false;
  els.confirmTransferFlow.textContent = "확인 후 서명";
  els.confirmTransferFlow.disabled = false;
  els.confirmTransferFlow.hidden = false;
  els.confirmTransferFlow.focus();
  return new Promise(resolve => {
    transferFlowState.resolve = resolve;
  });
}

function cancelTransferFlow() {
  if (transferFlowState.running && transferFlowState.controller) {
    transferFlowState.controller.abort();
    els.cancelTransferFlow.disabled = true;
    els.cancelTransferFlow.textContent = "취소 중";
    els.transferModalState.textContent = "취소 요청됨";
    return;
  }
  closeTransferFlowModal(false);
}

function updateTransferFlow(activeKey, stateText, leadText) {
  transferFlowState.running = true;
  els.transferSteps.hidden = false;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferFlowModal.classList.remove("failed");
  if (leadText) {
    els.transferModalLead.textContent = leadText;
  }
  setTransferFlowStep(activeKey, stateText);
}

function finishTransferFlow(message, success = true, {
  retry = null,
  stateText = success ? "성공" : "실패",
  failureTitle = "",
  failureLead = ""
} = {}) {
  const copy = transferFlowState.copy || privacyFlowCopies.transfer;
  transferFlowState.running = false;
  transferFlowState.controller = null;
  transferFlowState.retry = typeof retry === "function" ? retry : null;
  els.transferModalLead.textContent = success ? copy.doneLead : failureLead || copy.failedLead;
  els.confirmTransferFlow.hidden = true;
  setTransferFlowStep(success ? "done" : "", stateText);
  els.transferFlowModal.classList.toggle("failed", !success);
  if (success) {
    els.transferSuccessTitle.textContent = message || copy.successTitle;
    els.transferSuccessCopy.textContent = copy.successCopy;
    els.transferSteps.hidden = true;
    els.transferSuccessPanel.hidden = false;
    els.transferFailurePanel.hidden = true;
  } else {
    els.transferFailureTitle.textContent = failureTitle || copy.failureTitle;
    els.transferSteps.hidden = false;
    els.transferSuccessPanel.hidden = true;
    els.transferFailureReason.textContent = message || "알 수 없는 오류가 발생했습니다.";
    els.transferFailurePanel.hidden = false;
  }
  els.cancelTransferFlow.textContent = "닫기";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.retryTransferFlow.hidden = success || !transferFlowState.retry;
  els.retryTransferFlow.disabled = false;
  els.cancelTransferFlow.focus();
}

function finishTransferFlowUnknown(message) {
  finishTransferFlow(message, false, {
    stateText: "결과 확인 필요",
    failureTitle: "전송 결과를 확인해야 합니다",
    failureLead: "트랜잭션 결과가 아직 확정되지 않았습니다. 같은 요청을 다시 보내지 마세요."
  });
}

function retryTransferFlow() {
  const retry = transferFlowState.retry;
  if (!retry) return;
  closeTransferFlowModal(false);
  setTimeout(() => retry(), 0);
}

async function api(path, options = {}) {
  const response = await fetch(path, {
    ...options,
    headers: {
      "content-type": "application/json",
      ...(options.headers || {})
    }
  });
  const data = await response.json();
  if (!response.ok || data.error) {
    throw new ApiError({
      error: data.error || response.statusText,
      ...data
    }, response.status);
  }
  return data;
}

async function digestText(value) {
  const bytes = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function bytesToHex(bytes) {
  const view = bytes instanceof ArrayBuffer ? new Uint8Array(bytes) : new Uint8Array(bytes || []);
  return [...view].map(byte => byte.toString(16).padStart(2, "0")).join("");
}

function hexToBytes(value) {
  const hex = String(value || "").trim().replace(/^0x/i, "");
  if (!/^[0-9a-fA-F]*$/.test(hex) || hex.length % 2 !== 0) {
    throw new Error("hex value is invalid");
  }
  const bytes = new Uint8Array(hex.length / 2);
  for (let i = 0; i < bytes.length; i += 1) {
    bytes[i] = Number.parseInt(hex.slice(i * 2, i * 2 + 2), 16);
  }
  return bytes;
}

function isEvmAddress(value) {
  const hex = String(value || "").trim().replace(/^0x/i, "");
  return /^[0-9a-fA-F]{40}$/.test(hex);
}

function isSendRecipientForWallet(value, walletKind = activeWalletKind()) {
  const recipient = String(value || "").trim();
  if (!recipient) return false;
  if (isEvmTransparentMode(walletKind)) {
    return isEvmAddress(recipient);
  }
  return recipient.startsWith(`${accountPrefix()}1`);
}

function requireValidSendRecipient() {
  const recipient = els.keplrSendRecipient.value.trim();
  if (isSendRecipientForWallet(recipient, state.activeWallet || activeWalletKind())) {
    return recipient;
  }
  if (isEvmTransparentMode(state.activeWallet || activeWalletKind())) {
    throw new Error("EVM send recipient must be a 0x address.");
  }
  throw new Error(`Cosmos send recipient must be a ${accountPrefix()}1... address.`);
}

function evmQuantityToBigInt(value, label = "EVM quantity") {
  const text = String(value || "").trim();
  if (!/^0x[0-9a-fA-F]+$/.test(text)) {
    throw new Error(`${label} must be a hex quantity`);
  }
  return BigInt(text);
}

function bigIntToEvmQuantity(value) {
  return `0x${value.toString(16)}`;
}

async function withEstimatedEvmGas(transaction) {
  const tx = { ...transaction };
  try {
    const estimated = evmQuantityToBigInt(await requestMetaMask({
      method: "eth_estimateGas",
      params: [tx]
    }), "estimated gas");
    const padded = (estimated * 13n + 9n) / 10n;
    const existing = tx.gas ? evmQuantityToBigInt(tx.gas, "transaction gas") : 0n;
    tx.gas = bigIntToEvmQuantity(existing > padded ? existing : padded);
    return tx;
  } catch {
    delete tx.gas;
    return tx;
  }
}

function formatBaseUnits(value, decimals = 18) {
  const amount = BigInt(value);
  const places = Math.max(0, Number(decimals || 0));
  if (!places) return amount.toString();
  const padded = amount.toString().padStart(places + 1, "0");
  const whole = padded.slice(0, -places);
  const fraction = padded.slice(-places).replace(/0+$/, "").slice(0, 8);
  return fraction ? `${whole}.${fraction}` : whole;
}

function formatEvmNetworkFee(value) {
  const fee = BigInt(value);
  return evmNativeDenom() === baseDenom()
    ? `${formatBaseUnits(fee, coinDecimals())} ${displayDenom()}`
    : `${fee}${evmNativeDenom()}`;
}

async function updateDepositNetworkFee(transaction) {
  if (state.activeWallet !== "metamask") {
    const fee = cosmosGasFeeEstimate(2500000);
    state.keplr.networkFeeAmount = fee.toString();
    state.keplr.networkFeeEstimate = `≈ ${fee}${baseDenom()} · gas limit 2,500,000 · Keplr confirms final fee`;
    renderKeplr();
    return fee;
  }
  try {
    const request = { ...transaction, from: state.wallet.account };
    const [gasHex, gasPriceHex] = await Promise.all([
      requestMetaMask({ method: "eth_estimateGas", params: [request] }),
      requestMetaMask({ method: "eth_gasPrice" })
    ]);
    const gas = evmQuantityToBigInt(gasHex, "estimated gas");
    const gasPrice = evmQuantityToBigInt(gasPriceHex, "gas price");
    const fee = gas * gasPrice;
    state.keplr.networkFeeAmount = fee.toString();
    state.keplr.networkFeeEstimate = `≈ ${formatEvmNetworkFee(fee)} · gas ${gas}`;
    renderKeplr();
    return fee;
  } catch {
    const fallback = BigInt(state.keplr.networkFeeAmount || "0");
    state.keplr.networkFeeEstimate = `${state.keplr.networkFeeEstimate} · wallet confirms final fee`;
    renderKeplr();
    return fallback;
  }
}

function cosmosGasFeeEstimate(gasLimit) {
  const gasPrice = Number(activeChainProfile()?.gasPriceStep?.average);
  const numericGasLimit = Number(gasLimit);
  const fee = Math.ceil(gasPrice * numericGasLimit);
  if (!Number.isSafeInteger(fee) || fee < 0) {
    throw new Error("Configured Cosmos gas policy cannot produce a safe fee estimate");
  }
  return BigInt(fee);
}

function updateIncludedDepositNetworkFee(result) {
  if (activeChainProfile()?.transport === "evm") {
    const fee = evmChargedFeeAmount(result?.receipt);
    state.keplr.networkFeeEstimate = fee === null
      ? "Actual fee unavailable · transaction included"
      : `Actual ${formatEvmNetworkFee(fee)} · transaction included`;
    if (fee !== null) state.keplr.networkFeeAmount = fee.toString();
  } else {
    const fee = cosmosChargedFeeAmount(result?.tx, baseDenom());
    state.keplr.networkFeeEstimate = fee === null
      ? "Actual fee unavailable · transaction included"
      : `Actual ${fee}${baseDenom()} · transaction included`;
    if (fee !== null) state.keplr.networkFeeAmount = fee.toString();
  }
  renderKeplr();
}

async function estimateDepositFeeBeforeProof() {
  if (state.activeWallet !== "metamask") {
    return updateDepositNetworkFee(null);
  }
  const configuredGas = activeChainProfile()?.evmGasLimit || state.config?.evmGasLimit;
  const gas = evmQuantityToBigInt(configuredGas, "configured deposit gas limit");
  const gasPrice = evmQuantityToBigInt(await requestMetaMask({ method: "eth_gasPrice" }), "gas price");
  const fee = gas * gasPrice;
  state.keplr.networkFeeAmount = fee.toString();
  state.keplr.networkFeeEstimate = `≤ ${formatEvmNetworkFee(fee)} budget · gas limit ${gas}`;
  renderKeplr();
  return fee;
}

function transparentBalanceAmount(denom = baseDenom()) {
  const value = state.keplr.transparentBalances?.[denom] || "0";
  if (!/^(0|[1-9][0-9]*)$/.test(String(value))) {
    throw new Error(`Transparent ${denom} balance is not a canonical integer`);
  }
  return BigInt(value);
}

function assertDepositFunding(amount, feeAmount) {
  const amountValue = parsePlannerAmountValue(amount);
  if (amountValue === null) throw new Error("Deposit amount must be a canonical integer");
  return assertDepositFundingAvailable({
    amount: amountValue.toString(),
    fee: BigInt(feeAmount || 0).toString(),
    assetBalance: transparentBalanceAmount().toString(),
    nativeBalance: state.keplr.evmNativeBalance,
    assetDenom: baseDenom(),
    nativeDenom: evmNativeDenom(),
    transport: activeChainProfile()?.transport || "cosmos"
  });
}

function normalizeEvmTxHash(txHash) {
  return String(txHash || "").trim().replace(/^0x/i, "").toUpperCase();
}

function bytesToBase64(bytes) {
  let binary = "";
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.slice(i, i + 0x8000));
  }
  return btoa(binary);
}

function base64ToBytes(value) {
  const binary = atob(value);
  const bytes = new Uint8Array(binary.length);
  for (let i = 0; i < binary.length; i += 1) {
    bytes[i] = binary.charCodeAt(i);
  }
  return bytes;
}

function amountInputValue(input) {
  const raw = String(input.value || "").trim().replace(/,/g, "");
  if (!/^(0|[1-9][0-9]*)$/.test(raw)) {
    throw new Error(`${baseDenom()} amount must be a positive integer`);
  }
  const amount = BigInt(raw);
  if (amount <= 0n) {
    throw new Error(`${baseDenom()} amount must be greater than 0`);
  }
  return coinTextFromAmount(amount);
}

function hasPositiveUclairInput(input) {
  const raw = String(input?.value || "").trim().replace(/,/g, "");
  if (!/^(0|[1-9][0-9]*)$/.test(raw)) return false;
  return BigInt(raw) > 0n;
}

function clairInputToUclair(input) {
  const raw = String(input.value || "").trim().replace(/,/g, "");
  const decimals = coinDecimals();
  const pattern = new RegExp(`^(0|[1-9][0-9]*)(\\.[0-9]{0,${decimals}})?$`);
  if (!pattern.test(raw)) {
    throw new Error(`${displayDenom()} amount must be a positive number with up to ${decimals} decimals`);
  }

  const [whole, fraction = ""] = raw.split(".");
  const scale = 10n ** BigInt(decimals);
  const paddedFraction = `${fraction}${"0".repeat(decimals)}`.slice(0, decimals);
  const amount = BigInt(whole) * scale + BigInt(paddedFraction || "0");
  return coinTextFromAmount(amount);
}

function formatUclairAsClair(amount) {
  const value = BigInt(String(amount || "0"));
  const decimals = coinDecimals();
  const scale = 10n ** BigInt(decimals);
  const whole = value / scale;
  const fraction = value % scale;
  if (fraction === 0n) {
    return `${whole} ${displayDenom()}`;
  }

  const fractionText = fraction.toString().padStart(decimals, "0").replace(/0+$/, "");
  return `${whole}.${fractionText} ${displayDenom()}`;
}

function formatBalances(balances) {
  return (balances || [])
    .map(coin => {
      if (coin.denom === baseDenom()) {
        return `${formatUclairAsClair(coin.amount)} (${coin.amount}${baseDenom()})`;
      }
      return `${coin.amount}${coin.denom}`;
    })
    .join(", ") || `0 ${displayDenom()} (${zeroCoinText()})`;
}

function balanceAmountsByDenom(balances) {
  const amounts = {};
  for (const coin of balances || []) {
    const denom = String(coin?.denom || "").trim();
    const amount = String(coin?.amount || "").trim();
    if (!denom || !/^(0|[1-9][0-9]*)$/.test(amount)) continue;
    amounts[denom] = (BigInt(amounts[denom] || "0") + BigInt(amount)).toString();
  }
  return amounts;
}

function noteAmountValue(note) {
  try {
    return BigInt(String(note?.amount || "0"));
  } catch {
    return 0n;
  }
}

function isSpendableNote(note) {
  const status = String(note?.status || "").toLowerCase();
  if (status) return status === "spendable";
  if (note?.spent === true || note?.isSpent === true) return false;
  return String(note?.nullifier_status || note?.nullifierStatus || "").toLowerCase() === "unspent";
}

function noteNullifier(note) {
  return String(note?.nullifier || note?.nullifier_hex || "").trim().toLowerCase();
}

function isZeroAmountNote(note) {
  return noteAmountValue(note) === 0n;
}

function isHelperNote(note) {
  return isSpendableNote(note) && isZeroAmountNote(note);
}

function noteStatusLabel(note) {
  if (isHelperNote(note)) return "helper";
  if (note?.status) return String(note.status);
  if (note?.spent === true || note?.isSpent === true) return "spent";
  return String(note?.nullifier_status || note?.nullifierStatus || "unverified");
}

function summarizeSpendableValueNotes(notes) {
  const spendableValueNotes = (notes || []).filter(note => isSpendableNote(note) && !isZeroAmountNote(note));
  const helperCount = (notes || []).filter(isHelperNote).length;
  const total = spendableValueNotes.reduce((sum, note) => sum + noteAmountValue(note), 0n);
  const helperText = helperCount ? ` · ${helperCount} helper` : "";
  return `${total}${baseDenom()} / ${spendableValueNotes.length} spendable${helperText}`;
}

function reservationForDisplayedNote(note) {
  const statuses = state.reservations.noteStatuses;
  if (!(statuses instanceof Map)) return null;
  const nullifier = noteNullifier(note);
  return statuses.get(nullifier) || statuses.get(nullifier.replace(/^0x/, "")) || null;
}

function summarizeReservationAvailableNotes(notes) {
  const spendableValueNotes = (notes || []).filter(note => isSpendableNote(note) && !isZeroAmountNote(note));
  const available = spendableValueNotes.filter(note => !reservationForDisplayedNote(note));
  const reservedCount = spendableValueNotes.length - available.length;
  const helperCount = (notes || []).filter(note => isHelperNote(note) && !reservationForDisplayedNote(note)).length;
  const total = available.reduce((sum, note) => sum + noteAmountValue(note), 0n);
  const reservedText = reservedCount ? ` · ${reservedCount} reserved` : "";
  const helperText = helperCount ? ` · ${helperCount} helper` : "";
  return `${total}${baseDenom()} / ${available.length} available${reservedText}${helperText}`;
}

function noteCacheKey(note) {
  const nullifier = noteNullifier(note);
  if (nullifier) return `nullifier:${nullifier}`;
  return `event:${Number(note?.height || 0)}:${String(note?.tx_hash || note?.txHash || "").toUpperCase()}:${String(note?.amount || "")}`;
}

function mergeCachedNotes(existingNotes = [], incomingNotes = []) {
  const byKey = new Map();
  for (const note of existingNotes) byKey.set(noteCacheKey(note), note);
  for (const note of incomingNotes) byKey.set(noteCacheKey(note), note);
  return [...byKey.values()].sort((left, right) => {
    const heightCompare = Number(left?.height || 0) - Number(right?.height || 0);
    if (heightCompare !== 0) return heightCompare;
    return String(left?.tx_hash || left?.txHash || "").localeCompare(String(right?.tx_hash || right?.txHash || ""));
  });
}

function noteScanRequestOptions({ reset = false, maxPages = 5 } = {}) {
  const cursor = reset ? defaultNoteScanCursor() : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const hasMore = !reset && Boolean(cursor.has_more ?? cursor.hasMore);
  if (hasMore && state.keplr.noteScanResumeOptions) {
    return {
      ...state.keplr.noteScanResumeOptions,
      scanSource: "privacy_scan",
      maxPages,
      includeFoundNotes: true
    };
  }
  return {
    scanSource: "privacy_scan",
    limit: 200,
    outputLimit: 200,
    maxPages,
    includeFoundNotes: true
  };
}

async function applyNoteScanResult(data, { reset = false } = {}) {
  const store = await currentNoteStore();
  const stored = store ? await store.load() : null;
  const cursor = data?.scanCursor || data?.scan_cursor || stored?.scanCursor || defaultNoteScanCursor();
  state.keplr.notes = stored?.notes || mergeCachedNotes(reset ? [] : state.keplr.notes, data?.foundNotes || data?.notes || []);
  state.keplr.noteScanCursor = cursor;
  state.keplr.noteScanResumeOptions = data?.nextScanOptions || data?.next_scan_options || null;
  const moreText = Boolean(cursor.has_more ?? cursor.hasMore) ? " · more events queued" : "";
  state.keplr.notesSummary = `${summarizeSpendableValueNotes(state.keplr.notes)}${moreText}`;
  state.keplr.notesScanned = true;
  state.keplr.noteSyncStatus = Boolean(cursor.has_more ?? cursor.hasMore) ? "partial" : "synced";
  state.keplr.noteSyncMessage = Boolean(cursor.has_more ?? cursor.hasMore)
    ? "Encrypted cache updated · more events queued"
    : "Encrypted cache synced";
  const manager = await currentReservationManager();
  await reconcileSpentReservations(manager, state.keplr.notes);
  await refreshReservationState(manager);
}

function selectedLocalAccount() {
  const accounts = activeServerAccounts();
  return accounts.find(account => account.name === state.selectedAccount) || accounts[0];
}

function activeServerAccounts() {
  return serverFeature("localSigners") && selectedProfileMatchesServer() ? state.accounts : [];
}

function localSignerLabel(name) {
  const value = String(name || "local signer").trim();
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function renderDappChainSelect() {
  if (!els.dappChainSelect) return;
  const profiles = state.chainProfiles.length ? state.chainProfiles : [state.config?.activeProfile].filter(Boolean);
  els.dappChainSelect.innerHTML = "";
  for (const profile of profiles) {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = `${profile.label} (${profile.transport === "evm" ? "EVM" : "Cosmos"})`;
    els.dappChainSelect.append(option);
  }
  if (!state.selectedChainProfileId || !profiles.some(profile => profile.id === state.selectedChainProfileId)) {
    state.selectedChainProfileId = state.config?.activeChainProfileId || profiles[0]?.id || "";
  }
  els.dappChainSelect.value = state.selectedChainProfileId;
  renderDappChainHint();
}

function renderDappChainHint() {
  const profile = activeChainProfile();
  if (!els.dappChainHint || !profile) return;
  const wallet = activeWalletKind() === "metamask" ? "MetaMask" : "Keplr";
  const serverText = `${state.config?.chainId || "-"} / ${state.config?.transport || "-"}`;
  const activeText = selectedProfileMatchesServer(profile)
    ? "active"
    : `server is ${serverText}`;
  els.dappChainHint.textContent = `${profile.chainId} · ${wallet} · ${activeText}`;
}

function renderNoteScanEndpoint() {
  if (!els.noteScanEndpoint) return;
  const profile = activeChainProfile();
  const endpoints = profileRestEndpoints(profile);
  const profileId = profile?.id || "";
  const selected = state.selectedRestEndpointByProfile[profileId];
  const active = endpoints.includes(selected) ? selected : endpoints[0] || "";
  if (profileId && active) {
    state.selectedRestEndpointByProfile[profileId] = active;
  }
  els.noteScanEndpoint.innerHTML = "";
  for (const endpoint of endpoints) {
    const option = document.createElement("option");
    option.value = endpoint;
    option.textContent = endpoint;
    els.noteScanEndpoint.append(option);
  }
  els.noteScanEndpoint.value = active;
  els.noteScanEndpoint.disabled = endpoints.length < 2;
  els.noteScanEndpoint.title = endpoints.length < 2
    ? "Configure profile.restEndpoints to enable endpoint recovery."
    : "Choose the primary REST endpoint used for note recovery.";
}

function selectNoteScanEndpoint(endpoint) {
  const profile = activeChainProfile();
  const endpoints = profileRestEndpoints(profile);
  if (!profile?.id || !endpoints.includes(endpoint)) {
    throw new Error("Selected note scan endpoint is not configured for this chain profile");
  }
  state.selectedRestEndpointByProfile[profile.id] = endpoint;
  browserClient = null;
  browserClientKey = "";
  browserClientDepositProofProvider = null;
  state.protocol.ready = false;
  state.protocol.error = "";
  renderNoteScanEndpoint();
  renderProtocolStatus();
  updateAmountActionButtons();
  refreshProtocolStatus().catch(error => {
    state.protocol.error = browserDataLoadErrorMessage(error);
    renderProtocolStatus();
  });
}

function renderChainDependentUi() {
  const walletKind = activeWalletKind();
  const transparentFormat = activeTransparentAddressFormat();
  els.keplrSendRecipient.placeholder = transparentFormat === "evm" ? "0x..." : `${accountPrefix()}1...`;
  if (els.keplrSendRecipient.value && !isSendRecipientForWallet(els.keplrSendRecipient.value, walletKind)) {
    els.keplrSendRecipient.value = "";
  }
  els.veiledTransferRecipient.placeholder = `${shieldedPrefix()}1...`;
  els.veiledWithdrawRecipient.placeholder = transparentFormat === "evm" ? "0x..." : `${accountPrefix()}1...`;
  document.querySelectorAll(".amount-control .denom").forEach(label => {
    label.textContent = label.closest(".faucet-row") ? displayDenom() : baseDenom();
  });
  const faucetSource = activeServerAccounts()[0]?.name || "local signer";
  els.faucetHelpText.textContent = `(${displayDenom()} get from ${localSignerLabel(faucetSource)}'s wallet)`;
  renderDappChainHint();
  renderNoteScanEndpoint();
}

function selectDappChainProfile(profileId) {
  if (state.activeWallet) {
    resetWalletSession();
  }
  state.selectedChainProfileId = profileId;
  renderDappChainSelect();
  renderChainDependentUi();
  renderAccounts();
  renderWalletSession();
  renderKeplr();
  renderVisibleAddressSuggestions();
  refreshProtocolStatus().catch(() => {});
}

function recipientTestAccounts() {
  const accounts = activeServerAccounts();
  const preferred = accounts.filter(account => ["alice", "bob"].includes(account.name));
  if (preferred.length) return preferred;
  return accounts.filter(account => account.name !== "auditor");
}

async function ensureLocalSignersIfNeeded(data) {
  if (!data.config?.serverFeatures?.localSignerSetup || data.config?.transport !== "evm" || (data.accounts || []).length) {
    return data;
  }
  let ensured;
  try {
    ensured = await api("/api/local-signers/ensure", {
      method: "POST",
      body: JSON.stringify({})
    });
  } catch (error) {
    if (error?.statusCode !== 403) {
      throw error;
    }
    toast("Local signer setup is blocked for LAN browsers. Create accounts on the server machine first, or restart with CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1.");
    return {
      ...data,
      accounts: []
    };
  }
  return {
    ...data,
    accounts: ensured.accounts || []
  };
}

async function browserHealthFromStaticConfig(config) {
  const validated = validateClairveilWebClientConfig(config);
  const profile = validated.activeProfile;
  state.config = validated;
  state.chainProfiles = [...validated.chainProfiles];
  state.selectedChainProfileId = validated.activeChainProfileId;
  const health = await clairveilBrowserClient(profile).health();
  return {
    config,
    status: health.status,
    tree: health.tree,
    audit: health.audit,
    accounts: [],
    errors: health.errors || []
  };
}

async function loadDappHealth() {
  if (serverConfigAvailable) {
    try {
      const data = await ensureLocalSignersIfNeeded(await api("/api/health"));
      serverConfigAvailable = true;
      return data;
    } catch (error) {
      serverConfigAvailable = false;
    }
  }
  return browserHealthFromStaticConfig(getStaticDappConfig());
}

function addressSuggestionConfigs() {
  const transparentFormat = activeTransparentAddressFormat();
  return [
    {
      input: els.keplrSendRecipient,
      list: els.keplrSendRecipientSuggestions,
      kind: "transparent",
      label: transparentFormat === "evm" ? "EVM" : "transparent",
      format: transparentFormat
    },
    {
      input: els.veiledTransferRecipient,
      list: els.veiledTransferRecipientSuggestions,
      kind: "shielded",
      label: "shielded"
    },
    {
      input: els.veiledWithdrawRecipient,
      list: els.veiledWithdrawRecipientSuggestions,
      kind: "transparent",
      label: transparentFormat === "evm" ? "EVM" : "transparent",
      format: transparentFormat,
      includeWallet: true
    }
  ];
}

function suggestedAddressFor(account, config) {
  if (config?.format === "evm") {
    if (account.evmAddress) return account.evmAddress;
    try {
      return bech32AddressToEvm(account.transparentAddress || "");
    } catch {
      return "";
    }
  }
  const kind = config?.kind || "";
  return kind === "shielded"
    ? state.addressBook.shieldedByName[account.name] || ""
    : account.transparentAddress || "";
}

function transparentDisplayAddressFor(account) {
  return suggestedAddressFor(account || {}, {
    kind: "transparent",
    format: activeTransparentAddressFormat()
  });
}

function connectedWalletAddressSuggestions(config) {
  if (!config?.includeWallet || config.kind !== "transparent") {
    return [];
  }

  if (config.format === "evm") {
    if (state.wallet.account) {
      return [{
        name: "My wallet",
        address: state.wallet.account
      }];
    }
    if (state.keplr.account) {
      try {
        return [{
          name: "My wallet",
          address: bech32AddressToEvm(state.keplr.account, accountPrefix())
        }];
      } catch {
        return [];
      }
    }
    return [];
  }

  if (!state.keplr.account) {
    return [];
  }

  const suggestions = [{
    name: "My wallet",
    address: state.keplr.account
  }];

  return suggestions;
}

function hideAddressSuggestions(config) {
  if (!config?.list || !config?.input) return;
  config.list.hidden = true;
  config.input.setAttribute("aria-expanded", "false");
}

function hideAllAddressSuggestions() {
  for (const config of addressSuggestionConfigs()) {
    hideAddressSuggestions(config);
  }
}

function selectAddressSuggestion(config, address) {
  if (!address) return;
  config.input.value = address;
  config.input.dispatchEvent(new Event("input", { bubbles: true }));
  hideAddressSuggestions(config);
  config.input.focus();
}

function appendAddressSuggestionEmpty(config, message) {
  const empty = document.createElement("p");
  empty.className = "address-suggestion-empty";
  empty.textContent = message;
  config.list.append(empty);
}

function renderAddressSuggestions(config) {
  if (!config?.list) return;
  config.list.innerHTML = "";

  const accounts = recipientTestAccounts();
  const seenAddresses = new Set();
  const suggestions = [
    ...connectedWalletAddressSuggestions(config),
    ...accounts.map(account => ({
      name: account.name,
      address: suggestedAddressFor(account, config)
    }))
  ].filter(entry => {
    if (!entry.address) return false;
    if (config.format === "evm" && !isEvmAddress(entry.address)) return false;
    const key = entry.address.toLowerCase();
    if (seenAddresses.has(key)) return false;
    seenAddresses.add(key);
    return true;
  });

  if (config.kind === "shielded" && state.addressBook.loadingShielded && suggestions.length < accounts.length) {
    appendAddressSuggestionEmpty(config, "Loading shielded addresses...");
  }

  if (config.kind === "shielded" && state.addressBook.shieldedError && !suggestions.length) {
    appendAddressSuggestionEmpty(config, state.addressBook.shieldedError);
    return;
  }

  if (!suggestions.length && !config.list.childElementCount) {
    appendAddressSuggestionEmpty(config, `No ${config.label} test addresses`);
    return;
  }

  for (const suggestion of suggestions) {
    const option = document.createElement("div");
    option.className = "address-suggestion";
    option.setAttribute("role", "option");
    option.setAttribute("tabindex", "0");
    option.title = suggestion.address;
    option.addEventListener("mousedown", event => {
      event.preventDefault();
      selectAddressSuggestion(config, suggestion.address);
    });
    option.addEventListener("keydown", event => {
      if (event.key !== "Enter" && event.key !== " ") return;
      event.preventDefault();
      selectAddressSuggestion(config, suggestion.address);
    });

    const name = document.createElement("strong");
    name.textContent = `${suggestion.name} -`;

    const address = document.createElement("span");
    address.className = "address-suggestion-value";
    address.textContent = suggestion.address;

    option.append(name, address);
    config.list.append(option);
  }
}

function renderVisibleAddressSuggestions() {
  for (const config of addressSuggestionConfigs()) {
    if (config.list && !config.list.hidden) {
      renderAddressSuggestions(config);
    }
  }
}

async function ensureShieldedAddressBook() {
  const missing = recipientTestAccounts().filter(account => !state.addressBook.shieldedByName[account.name]);
  if (!missing.length) return;
  if (shieldedAddressBookPromise) {
    await shieldedAddressBookPromise;
    return;
  }

  state.addressBook.loadingShielded = true;
  state.addressBook.shieldedError = "";
  renderVisibleAddressSuggestions();

  shieldedAddressBookPromise = Promise.allSettled(missing.map(async account => {
    const data = await api(`/api/wallet/${account.name}/show-address`);
    const address = data.address || "";
    if (address) {
      state.addressBook.shieldedByName[account.name] = address;
    }
  }));

  const results = await shieldedAddressBookPromise;
  state.addressBook.loadingShielded = false;
  shieldedAddressBookPromise = null;
  if (results.some(result => result.status === "rejected")) {
    state.addressBook.shieldedError = "Unable to load shielded addresses";
  }
  renderVisibleAddressSuggestions();
}

function showAddressSuggestions(config) {
  if (!config?.input || !config?.list) return;
  renderAddressSuggestions(config);
  config.list.hidden = false;
  config.input.setAttribute("aria-expanded", "true");
  if (config.kind === "shielded") {
    ensureShieldedAddressBook().catch(error => {
      state.addressBook.loadingShielded = false;
      state.addressBook.shieldedError = error.message;
      shieldedAddressBookPromise = null;
      renderVisibleAddressSuggestions();
    });
  }
}

function setupAddressSuggestions() {
  for (const config of addressSuggestionConfigs()) {
    if (!config.input || !config.list) continue;
    const currentConfig = () => addressSuggestionConfigs().find(next => next.input === config.input) || config;
    config.input.addEventListener("focus", () => showAddressSuggestions(currentConfig()));
    config.input.addEventListener("click", () => showAddressSuggestions(currentConfig()));
    config.input.addEventListener("input", () => {
      const latestConfig = currentConfig();
      if (!latestConfig.list.hidden) {
        renderAddressSuggestions(latestConfig);
      }
      if (latestConfig.input === els.keplrSendRecipient) {
        updateAmountActionButtons();
      }
    });
    config.input.addEventListener("blur", () => {
      window.setTimeout(() => hideAddressSuggestions(currentConfig()), 120);
    });
  }

  document.addEventListener("pointerdown", event => {
    if (event.target.closest(".address-field")) return;
    hideAllAddressSuggestions();
  });
}

function resetMetaMaskSession() {
  state.wallet = defaultMetaMaskState();
}

function resetKeplrSession() {
  stopRelayReservationHeartbeat();
  globalThis.clearTimeout(relayRecoverySaveTimer);
  state.keplr = defaultKeplrState();
  noteStore = null;
  noteStorePromise = null;
  noteStoreKey = "";
  reservationManager = null;
  reservationManagerPromise = null;
  reservationManagerKey = "";
  operationStore = null;
  operationStorePromise = null;
  operationStoreKey = "";
  publicPendingStateKey = "";
  state.reservations = defaultReservationState();
  state.relayWithdraw = {
    handoff: null,
    json: "",
    reservationIds: [],
    txHash: "",
    resultStatus: "idle",
    resultMessage: "Not checked"
  };
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
}

function resetWalletSession() {
  state.activeWallet = "";
  resetMetaMaskSession();
  resetKeplrSession();
}

function currentWalletAccountForCopy() {
  if (state.activeWallet === "metamask" && state.wallet.account) {
    return state.wallet.account;
  }
  if (state.activeWallet === "keplr" && state.keplr.account) {
    return state.keplr.account;
  }
  return "";
}

function connectedPublicRecipientAddress() {
  if (isEvmTransparentMode() && state.wallet.account) {
    return state.wallet.account;
  }
  return state.keplr.account || "";
}

function renderWalletSession() {
  const activeWallet = state.activeWallet;
  const profile = activeChainProfile();
  const walletKind = activeWalletKind();
  const profileReady = selectedProfileMatchesServer(profile);
  const metamaskConnected = activeWallet === "metamask" && Boolean(state.wallet.account);
  const keplrConnected = activeWallet === "keplr" && Boolean(state.keplr.account);
  const privacyConnected = Boolean(state.keplr.account);
  const keplrReady = keplrConnected && state.keplr.addressMatches;
  const connected = metamaskConnected || keplrConnected;

  els.walletStatus.textContent = !connected
    ? profileReady
      ? "Wallet Offline"
      : "Chain Not Running"
    : metamaskConnected
      ? "MetaMask Connected"
      : keplrReady
        ? "Keplr Connected"
        : "Keplr Needs Reset";
  els.walletStatus.classList.toggle("online", metamaskConnected || keplrReady);

  els.connectWallet.hidden = connected || walletKind !== "metamask";
  els.connectKeplr.hidden = connected || walletKind !== "keplr";
  els.connectWallet.disabled = !profileReady;
  els.connectKeplr.disabled = !profileReady;
  els.disconnectWallet.hidden = !connected;

  els.sessionWallet.textContent = metamaskConnected ? "MetaMask" : keplrConnected ? "Keplr" : "Not connected";
  els.walletAccount.textContent = metamaskConnected
    ? shorten(state.wallet.account, 12, 10)
    : keplrConnected
      ? state.keplr.account
      : "Not connected";
  els.copyWalletAccount.disabled = !currentWalletAccountForCopy();
  els.walletChain.textContent = metamaskConnected
    ? state.wallet.chainId || "-"
    : keplrConnected
      ? activeKeplrChainInfo()?.chainId || profile?.chainId || state.config?.chainId || "-"
      : "-";
  els.walletSignatureHash.textContent = metamaskConnected && state.wallet.signatureHash
    ? shorten(state.wallet.signatureHash, 14, 12)
    : keplrConnected && state.keplr.signatureHash
      ? `${shorten(state.keplr.signatureHash, 14, 12)}${state.keplr.verified ? " verified" : ""}`
      : "-";
  els.keplrName.textContent = privacyConnected ? state.keplr.name || (metamaskConnected ? "MetaMask" : "Keplr") : "-";
  els.keplrPubkey.textContent = privacyConnected && state.keplr.pubkeyHex ? shorten(state.keplr.pubkeyHex, 14, 12) : "-";
  els.keplrSignerCheck.textContent = privacyConnected ? state.keplr.signerCheck || "Checking..." : "-";
  els.keplrBalance.textContent = privacyConnected ? state.keplr.balance || "-" : "-";
  els.keplrFaucetHash.textContent = privacyConnected && state.keplr.faucetHash ? shorten(state.keplr.faucetHash, 14, 12) : "-";
  els.keplrFaucetSent.textContent = privacyConnected ? state.keplr.faucetSent || "-" : "-";
  els.keplrFaucetRecipient.textContent = privacyConnected ? state.keplr.faucetRecipient || "-" : "-";
  els.keplrShieldedAddress.textContent = privacyConnected ? state.keplr.shieldedAddress || "Not set up" : "Not set up";
  els.signSession.disabled = !connected;
  renderDappChainHint();
}

function renderWallet() {
  renderWalletSession();
}

function renderRelayWithdraw() {
  const relayMode = els.withdrawMode.value === "relay";
  const handoff = state.relayWithdraw.handoff;
  const payload = relayWithdrawHandoffPayload(handoff);
  els.relayWithdrawPanel.hidden = !relayMode;
  els.withdrawFromVeiled.textContent = relayMode ? "Prepare relay payload" : "Withdraw";
  els.relayWithdrawChain.textContent = payload?.chain_id || handoff?.transaction?.chainId || "-";
  els.relayWithdrawRecipient.textContent = payload?.recipient || "-";
  const expiry = Number(payload?.expires_at_unix || 0);
  els.relayWithdrawExpiry.textContent = expiry
    ? `${new Date(expiry * 1000).toLocaleString()} (${expiry})`
    : "-";
  els.relayWithdrawPayloadHash.textContent = payload?.payload_hash || "-";
  els.relayWithdrawJson.value = state.relayWithdraw.json;
  if (els.relayWithdrawTxHash.value !== state.relayWithdraw.txHash) {
    els.relayWithdrawTxHash.value = state.relayWithdraw.txHash;
  }
  els.relayWithdrawResult.textContent = state.relayWithdraw.resultMessage || "Not checked";
  els.relayWithdrawResult.dataset.status = state.relayWithdraw.resultStatus;
  els.reconcileRelayWithdraw.disabled = !handoff
    || state.relayWithdraw.resultStatus === "checking";
  els.copyRelayWithdraw.disabled = !state.relayWithdraw.json;
  els.downloadRelayWithdraw.disabled = !state.relayWithdraw.json;
}

async function setRelayWithdrawHandoff(prepared) {
  const reservationIDs = preparedReservationIDs(prepared);
  if (!prepared.reservationManager || !prepared.reservation || !reservationIDs.length) {
    throw new Error("Relay withdraw handoff requires an active prepared reservation");
  }
  await withPreparedReservationHeartbeat(prepared, () => (
    prepared.reservationManager.recordRelayHandoff(reservationIDs, {
      leaseToken: prepared.reservation.lease_token,
      payloadHash: prepared.payload?.payload_hash || "",
      metadata: { handoff_surface: "clairveil_example_dapp" }
    })
  ));
  const handoff = createRelayWithdrawHandoff({
    profileId: activeChainProfile()?.id || "",
    transport: activeChainProfile()?.transport || "cosmos",
    payload: prepared.payload,
    transaction: prepared.transaction
  });
  const nextRelayWithdraw = {
    handoff,
    reservationIds: reservationIDs,
    txHash: "",
    resultStatus: "waiting",
    resultMessage: "Handoff recorded · waiting for relayer tx hash",
    leaseToken: prepared.reservation.lease_token,
    leaseUntil: prepared.reservation.lease_until,
    json: JSON.stringify(handoff, (_key, value) => (
      typeof value === "bigint" ? value.toString() : value
    ), 2)
  };
  await persistRelayWithdrawRecovery(nextRelayWithdraw);
  state.relayWithdraw = nextRelayWithdraw;
  startRelayReservationHeartbeat({
    manager: prepared.reservationManager,
    reservationIDs,
    leaseToken: prepared.reservation.lease_token,
    leaseUntil: prepared.reservation.lease_until
  });
  await refreshReservationState(prepared.reservationManager);
  renderRelayWithdraw();
}

function renderKeplr() {
  const connected = Boolean(state.keplr.account);
  const signerReady = connected && state.keplr.addressMatches;
  const veiledReady = signerReady && Boolean(state.keplr.rootSignatureBase64);
  renderWalletSession();
  els.myClairBalance.textContent = connected ? state.keplr.balance || "-" : "-";
  els.keplrDisclosurePubKey.textContent = state.keplr.disclosurePubKeyHex || "Setup Clairveil first";
  els.keplrSendHash.textContent = state.keplr.sendHash ? shorten(state.keplr.sendHash, 14, 12) : "-";
  els.keplrDepositHash.textContent = state.keplr.depositHash ? shorten(state.keplr.depositHash, 14, 12) : "-";
  els.keplrDepositHeight.textContent = state.keplr.depositHeight || "-";
  els.keplrDepositRecovery.textContent = state.keplr.depositRecoveryMessage || "Not started";
  els.keplrDepositNetworkFee.textContent = state.keplr.networkFeeEstimate || "Not estimated";
  const sendPending = ["submitted", "unknown", "checking"].includes(state.keplr.sendStatus);
  const depositPending = ["submitted", "unknown", "checking"].includes(state.keplr.depositRecoveryStatus);
  els.reconcileKeplrSend.disabled = !sendPending || !state.keplr.sendHash;
  els.reconcileKeplrDeposit.disabled = !depositPending || !state.keplr.depositHash;
  els.clearPublicPendingState.hidden = !state.keplr.publicPendingStateError;
  els.keplrTransferHash.textContent = state.keplr.transferHash ? shorten(state.keplr.transferHash, 14, 12) : "-";
  els.keplrWithdrawHash.textContent = state.keplr.withdrawHash ? shorten(state.keplr.withdrawHash, 14, 12) : "-";
  els.keplrWithdrawHeight.textContent = state.keplr.withdrawHeight || "-";
  els.keplrWithdrawNullifier.textContent = state.keplr.withdrawNullifierStatus;
  els.keplrWithdrawReceive.textContent = state.keplr.withdrawReceiveStatus;
  if (connected && !els.veiledWithdrawRecipient.value) {
    els.veiledWithdrawRecipient.value = state.keplr.account;
  }
  renderMyKeplrNotes();
  els.fundKeplr.disabled = !serverFeature("faucet") || !signerReady;
  els.setupKeplrPrivacy.disabled = !signerReady;
  els.copyKeplrShieldedAddress.disabled = !state.keplr.shieldedAddress;
  els.copyKeplrDisclosurePubKey.disabled = !state.keplr.disclosurePubKeyHex;
  els.refreshWalletBalance.disabled = !connected;
  els.scanKeplrNotes.disabled = !signerReady || !state.keplr.rootSignatureBase64 || !state.protocol.ready;
  els.resetRescanNotes.disabled = !signerReady || !state.keplr.rootSignatureBase64 || !state.protocol.ready;
  updateNoteRollbackButton({ signerReady });
  els.backupNoteCache.disabled = !noteStoreKeys()?.encrypted || !globalThis.localStorage?.getItem(noteStoreKeys().encrypted);
  els.noteSyncState.textContent = state.keplr.noteSyncMessage || "Not scanned";
  els.noteSyncState.dataset.status = state.keplr.noteSyncStatus;
  renderReservationState();
  renderRelayWithdraw();
  updateAmountActionButtons({ signerReady, veiledReady });
  renderEventDetail();
  persistPublicPendingTransactions();
}

function setWithdrawEvidence(nullifierStatus, receiveStatus, { render = true } = {}) {
  state.keplr.withdrawNullifierStatus = nullifierStatus;
  state.keplr.withdrawReceiveStatus = receiveStatus;
  if (render) renderKeplr();
}

function confirmWithdrawEvidence({ render = true } = {}) {
  setWithdrawEvidence(
    "Spent · confirmed by note reconciliation",
    "Received · intended transparent output confirmed",
    { render }
  );
}

function updateNoteRollbackButton({ signerReady = Boolean(state.keplr.account && state.keplr.addressMatches) } = {}) {
  if (!els.rollbackRescanNotes) return;
  const height = String(els.noteRollbackHeight?.value || "").trim();
  els.rollbackRescanNotes.disabled = !signerReady
    || !state.keplr.rootSignatureBase64
    || !state.protocol.ready
    || !/^(0|[1-9][0-9]*)$/.test(height);
}

function updateAmountActionButtons(status = {}) {
  const connected = Boolean(state.keplr.account);
  const signerReady = status.signerReady ?? (connected && state.keplr.addressMatches);
  const veiledReady = status.veiledReady ?? (signerReady && Boolean(state.keplr.rootSignatureBase64));
  const sendPending = ["submitted", "unknown", "checking"].includes(state.keplr.sendStatus);
  const depositPending = ["submitted", "unknown", "checking"].includes(state.keplr.depositRecoveryStatus);
  const protocolReady = state.protocol.ready;
  const noteInventoryTrusted = state.keplr.noteSyncStatus === "synced" && protocolReady;
  els.sendFromKeplr.disabled = !signerReady
    || Boolean(state.keplr.publicPendingStateError)
    || sendPending
    || !hasPositiveUclairInput(els.keplrSendAmount)
    || !isSendRecipientForWallet(els.keplrSendRecipient.value, state.activeWallet || activeWalletKind());
  els.depositFromKeplr.disabled = !signerReady
    || Boolean(state.keplr.publicPendingStateError)
    || depositPending
    || !protocolReady
    || !depositProofReady()
    || !hasPositiveUclairInput(els.keplrDepositAmount);
  els.depositFromKeplr.title = !protocolReady
    ? "Protocol preflight must pass before depositing."
    : depositProofReady()
      ? ""
      : "Configure CLAIRVEIL_DEPOSIT_PROOF_URL or inject CLAIRVEIL_DEPOSIT_PROOF_PROVIDER.";
  const relayRecoveryBlocked = els.withdrawMode?.value === "relay"
    && Boolean(state.relayWithdraw.handoff)
    && state.relayWithdraw.resultStatus !== "confirmed";
  els.transferFromVeiled.disabled = !veiledReady
    || !protocolReady
    || !noteInventoryTrusted
    || !hasPositiveUclairInput(els.veiledTransferAmount);
  els.withdrawFromVeiled.disabled = !veiledReady
    || !protocolReady
    || !noteInventoryTrusted
    || relayRecoveryBlocked
    || !hasPositiveUclairInput(els.veiledWithdrawAmount);
  const privacySpendTitle = !protocolReady
    ? "Protocol preflight must pass before using shielded notes."
    : !noteInventoryTrusted
      ? "Complete the note scan before using the displayed shielded balance."
      : "";
  els.transferFromVeiled.title = privacySpendTitle;
  els.withdrawFromVeiled.title = relayRecoveryBlocked
    ? "Reconcile the existing relay handoff before preparing another relay withdraw."
    : privacySpendTitle;
}

function renderMyKeplrNotes() {
  const noteInventoryTrusted = state.keplr.noteSyncStatus === "synced" && state.protocol.ready;
  els.myKeplrSpendable.textContent = noteInventoryTrusted
    ? summarizeReservationAvailableNotes(state.keplr.notes)
    : state.keplr.notesSummary
      ? `Cached · not confirmed (${state.keplr.notesSummary})`
      : "Not confirmed";
  els.myKeplrSpendableOnly.checked = state.keplr.showSpendableOnly;
  els.myKeplrNotesList.innerHTML = "";

  if (!state.keplr.account) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Wallet not connected";
    els.myKeplrNotesList.append(empty);
    return;
  }

  if (!state.keplr.notesScanned) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Not scanned";
    els.myKeplrNotesList.append(empty);
    return;
  }

  if (!noteInventoryTrusted) {
    const warning = document.createElement("p");
    warning.className = "empty stale-note-warning";
    warning.textContent = "Cached notes are shown for recovery only. Scan must complete before this balance is trusted.";
    els.myKeplrNotesList.append(warning);
  }

  const valueNotes = state.keplr.notes.filter(note => !isZeroAmountNote(note));
  const notes = state.keplr.showSpendableOnly
    ? valueNotes.filter(isSpendableNote)
    : valueNotes;

  if (notes.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    const hiddenZeroCount = state.keplr.notes.filter(isZeroAmountNote).length;
    empty.textContent = state.keplr.showSpendableOnly
      ? hiddenZeroCount ? `No value spendable notes (${hiddenZeroCount} zero notes hidden)` : "No spendable notes"
      : hiddenZeroCount ? `No value notes (${hiddenZeroCount} zero notes hidden)` : "No notes";
    els.myKeplrNotesList.append(empty);
    return;
  }

  for (const note of notes) {
    const reservation = reservationForDisplayedNote(note);
    const row = document.createElement("article");
    row.className = "note-row";
    row.classList.toggle("helper-note", isHelperNote(note));
    row.classList.toggle("reserved-note", Boolean(reservation));
    row.innerHTML = `
      <strong>${note.amount}${baseDenom()}</strong>
      <span>${reservation ? `reserved · ${reservation.status}` : noteStatusLabel(note)}</span>
      <code>${shorten(note.nullifier, 12, 10)}</code>
    `;
    els.myKeplrNotesList.append(row);
  }
}

function renderAccounts() {
  els.accountSelect.innerHTML = "";
  const accounts = activeServerAccounts();
  els.accountSelect.disabled = !accounts.length;
  for (const account of accounts) {
    const option = document.createElement("option");
    option.value = account.name;
    option.textContent = account.name;
    els.accountSelect.append(option);
  }
  els.accountSelect.value = state.selectedAccount;

  const account = selectedLocalAccount();
  const selectedTransparentAddress = transparentDisplayAddressFor(account);
  els.localSignerNotesTitle.textContent = "Notes";
  els.transparentAddress.textContent = selectedTransparentAddress || "-";
  if (!accounts.length) {
    els.keplrSendRecipient.value = "";
  } else if (!isSendRecipientForWallet(els.keplrSendRecipient.value) && selectedTransparentAddress) {
    els.keplrSendRecipient.value = selectedTransparentAddress;
  }
  renderVisibleAddressSuggestions();
}

function renderHealth(data) {
  const validatedConfig = validateClairveilWebClientConfig(data.config);
  state.config = validatedConfig;
  state.chainProfiles = [...validatedConfig.chainProfiles];
  if (!state.selectedChainProfileId || !state.chainProfiles.some(profile => profile.id === state.selectedChainProfileId)) {
    state.selectedChainProfileId = validatedConfig.activeChainProfileId || state.chainProfiles[0]?.id || "";
  }
  state.accounts = data.accounts || [];
  if (!state.accounts.some(account => account.name === state.selectedAccount)) {
    state.selectedAccount = state.accounts[0]?.name || "alice";
  }

  renderServerFeatureVisibility();
  els.modeBadge.textContent = validatedConfig.modeLabel || (localTestBackendEnabled() ? "Local Note Test Web" : "Public Node DApp");
  els.modeBadge.classList.toggle("public-mode", !localTestBackendEnabled());
  els.localHome.textContent = validatedConfig.localSignerHome || validatedConfig.home || "-";
  els.chainId.textContent = data.status?.node_info?.network || validatedConfig.chainId || "-";
  els.blockHeight.textContent = data.status?.sync_info?.latest_block_height || "-";
  els.leafCount.textContent = data.tree?.leaf_count || "-";
  els.restState.textContent = data.tree ? "Online" : "Offline";
  renderDappChainSelect();
  renderChainDependentUi();
  renderAccounts();
  renderWalletSession();
  ensureShieldedAddressBook().catch(error => {
    state.addressBook.loadingShielded = false;
    state.addressBook.shieldedError = error.message;
    shieldedAddressBookPromise = null;
    renderVisibleAddressSuggestions();
  });
  renderProtocolStatus();
}

function renderProtocolStatus() {
  if (!els.protocolState) return;
  els.depositProofState.textContent = depositProofReady() ? "Ready" : "Required";
  if (state.protocol.ready) {
    els.protocolState.textContent = "v0.3.1 ready";
    const reserve = state.protocol.reserve;
    els.reserveState.textContent = reserve
      ? `${reserve.module_balance}/${reserve.expected_module_balance}`
      : "-";
    return;
  }
  els.protocolState.textContent = state.protocol.error ? "Unavailable" : "Checking";
  els.reserveState.textContent = state.protocol.error ? "Unavailable" : "Checking";
}

async function refreshProtocolStatus() {
  if (!state.config) return;
  state.protocol.ready = false;
  state.protocol.reserve = null;
  state.protocol.error = "";
  renderProtocolStatus();
  try {
    const client = clairveilBrowserClient();
    const [, reserve] = await Promise.all([
      client.assertProtocolPreflight(baseDenom()),
      client.queryReserve(baseDenom())
    ]);
    state.protocol.ready = true;
    state.protocol.reserve = reserve;
  } catch (error) {
    state.protocol.error = browserDataLoadErrorMessage(error);
  }
  renderProtocolStatus();
  updateAmountActionButtons();
}

async function refreshHealth() {
  const data = await loadDappHealth();
  renderHealth(data);
  if (serverFeature("localSigners")) {
    try {
      await refreshSelectedAccount();
    } catch (error) {
      if (error?.statusCode !== 403) {
        throw error;
      }
      renderLocalSignerUnavailable(error);
    }
  }
  const tasks = [refreshEvents({ allowFailure: true }), refreshProtocolStatus()];
  if (serverFeature("auditorAdmin")) {
    tasks.push(refreshAuditorTransfers(), refreshAuditorTestScalar());
  }
  await Promise.allSettled(tasks);
}

async function refreshSelectedAccount() {
  const account = selectedLocalAccount();
  if (!account) {
    els.transparentAddress.textContent = "-";
    els.shieldedAddress.textContent = "-";
    els.balanceValue.textContent = "-";
    els.spendableTotal.textContent = zeroCoinText();
    els.notesList.innerHTML = "";
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No local signer accounts";
    els.notesList.append(empty);
    return;
  }

  els.transparentAddress.textContent = transparentDisplayAddressFor(account) || "-";
  els.shieldedAddress.textContent = "Loading...";
  els.balanceValue.textContent = "Loading...";

  const [shielded, balance] = await Promise.all([
    api(`/api/wallet/${account.name}/show-address`),
    clairveilBrowserClient().getBalances(account.transparentAddress)
  ]);

  els.shieldedAddress.textContent = shielded.address || "-";
  els.balanceValue.textContent = (balance.balances || [])
    .map(coin => `${coin.amount}${coin.denom}`)
    .join(", ") || zeroCoinText();

  await refreshNotes();
}

async function refreshWalletBalance() {
  if (!state.keplr.account) return;
  if (isEvmTransparentMode()) {
    if (!state.wallet.account) return;
    const [balanceHex, assetData] = await Promise.all([
      requestMetaMask({
        method: "eth_getBalance",
        params: [state.wallet.account, "latest"]
      }),
      clairveilBrowserClient().getBalances(state.keplr.account)
    ]);
    const nativeAmount = BigInt(balanceHex || "0x0").toString();
    const balances = [...(assetData.balances || [])];
    const amounts = balanceAmountsByDenom(balances);
    state.keplr.evmNativeBalance = nativeAmount;
    if (evmNativeDenom() === baseDenom()) {
      amounts[baseDenom()] = nativeAmount;
      const existing = balances.find(coin => coin.denom === baseDenom());
      if (existing) existing.amount = nativeAmount;
      else balances.push({ denom: baseDenom(), amount: nativeAmount });
    }
    state.keplr.transparentBalances = amounts;
    const nativeGasBalance = evmNativeDenom() === baseDenom()
      ? ""
      : `${nativeAmount}${evmNativeDenom()} (EVM gas)`;
    state.keplr.balance = [formatBalances(balances), nativeGasBalance].filter(Boolean).join(" · ");
  } else {
    const data = await clairveilBrowserClient().getBalances(state.keplr.account);
    state.keplr.transparentBalances = balanceAmountsByDenom(data.balances);
    state.keplr.evmNativeBalance = "0";
    state.keplr.balance = formatBalances(data.balances);
  }
  renderKeplr();
}

async function refreshNotes() {
  const account = selectedLocalAccount();
  if (!account) {
    els.spendableTotal.textContent = zeroCoinText();
    els.notesList.innerHTML = "";
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No local signer accounts";
    els.notesList.append(empty);
    return;
  }
  els.notesList.textContent = "Scanning...";
  const data = await api(`/api/wallet/${account.name}/notes`);
  els.spendableTotal.textContent = `${data.summary?.total_spendable || "0"}${baseDenom()}`;

  els.notesList.innerHTML = "";
  const notes = data.notes || [];
  if (notes.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No notes";
    els.notesList.append(empty);
    return;
  }

  for (const note of notes.slice(0, 8)) {
    const row = document.createElement("article");
    row.className = "note-row";
    row.classList.toggle("helper-note", isHelperNote(note));
    row.innerHTML = `
      <strong>${note.amount}${baseDenom()}</strong>
      <span>${noteStatusLabel(note)}</span>
      <code>${shorten(note.nullifier, 12, 10)}</code>
    `;
    els.notesList.append(row);
  }
}

async function refreshEvents({ allowFailure = false } = {}) {
  const [privacyResult, blockResult] = await Promise.allSettled([
    clairveilBrowserClient().fetchPrivacyEvents(),
    clairveilBrowserClient().fetchBlockEvents(30)
  ]);

  if (privacyResult.status === "rejected") {
    state.privacyEvents.events = [];
    state.privacyEvents.loadError = browserDataLoadErrorMessage(privacyResult.reason);
    state.blockEvents.events = [];
    state.blockEvents.error = blockResult.status === "rejected"
      ? browserDataLoadErrorMessage(blockResult.reason)
      : "";
    renderPrivacyEvents();
    renderEventDetail();
    renderBlockEvents();
    if (allowFailure) return;
    throw privacyResult.reason;
  }

  state.privacyEvents.events = privacyResult.value.events || [];
  state.privacyEvents.loadError = "";
  if (blockResult.status === "fulfilled") {
    state.blockEvents.events = blockResult.value.events || [];
    state.blockEvents.error = "";
  } else {
    state.blockEvents.events = [];
    state.blockEvents.error = browserDataLoadErrorMessage(blockResult.reason);
  }

  if (state.privacyEvents.selectedTxHash && !state.privacyEvents.events.some(event => event.tx_hash_hex === state.privacyEvents.selectedTxHash)) {
    state.privacyEvents.selectedTxHash = "";
    state.privacyEvents.decoded = null;
    state.privacyEvents.error = "";
  }
  renderPrivacyEvents();
  renderEventDetail();
  renderBlockEvents();
}

async function refreshBlockEvents() {
  try {
    const data = await clairveilBrowserClient().fetchBlockEvents(30);
    state.blockEvents.events = data.events || [];
    state.blockEvents.error = "";
  } catch (error) {
    state.blockEvents.events = [];
    state.blockEvents.error = error.message;
  }
  renderBlockEvents();
}

function disclosureTargetMatches(event) {
  const target = eventAttribute(event, "user_disclosure_target_pubkey");
  return Boolean(target && state.keplr.disclosurePubKeyHex && target.toLowerCase() === state.keplr.disclosurePubKeyHex.toLowerCase());
}

function isPublicDisclosureEvent(event) {
  return Boolean(
    event?.event_type === "shielded_transfer" &&
    eventAttribute(event, "user_disclosure_mode") === "USER_DISCLOSURE_MODE_PUBLIC" &&
    eventAttribute(event, "user_disclosure_payload")
  );
}

function canDecodeEventDisclosure(event) {
  if (!event || event.event_type !== "shielded_transfer") return false;
  if (isPublicDisclosureEvent(event)) return true;
  return disclosureTargetMatches(event);
}

function canDecodeSelfViewDisclosure(event) {
  return Boolean(
    event?.event_type === "shielded_transfer" &&
    eventAttribute(event, "self_view_disclosure_payload") &&
    state.keplr.rootSignatureBase64
  );
}

function eventDisclosureStatus(event) {
  if (!event) return "Select an event.";
  if (event.event_type !== "shielded_transfer") return "Disclosure 조회는 shielded transfer에서만 가능합니다.";
  const mode = eventAttribute(event, "user_disclosure_mode");
  const target = eventAttribute(event, "user_disclosure_target_pubkey");
  const payload = eventAttribute(event, "user_disclosure_payload");
  if (!payload) {
    return eventAttribute(event, "self_view_disclosure_payload")
      ? "User disclosure는 없지만 송신자라면 self-view로 조회할 수 있습니다."
      : "이 transfer에는 disclosure payload가 없습니다.";
  }
  if (mode === "USER_DISCLOSURE_MODE_PUBLIC") {
    return "Public disclosure입니다. 누구나 조회할 수 있습니다.";
  }
  if (mode !== "USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED") {
    return "이 transfer에는 recipient disclosure가 없습니다.";
  }
  if (!state.keplr.disclosurePubKeyHex) {
    return "Setup Clairveil 후 내 disclosure pubkey와 비교할 수 있습니다.";
  }
  if (!target) {
    return "Disclosure target pubkey가 없습니다.";
  }
  if (!disclosureTargetMatches(event)) {
    return "내 disclosure pubkey 대상이 아닙니다.";
  }
  return "내 disclosure pubkey 대상입니다. 조회할 수 있습니다.";
}

function renderPrivacyEvents() {
  els.eventsList.innerHTML = "";
  if (state.privacyEvents.loadError) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.privacyEvents.loadError;
    els.eventsList.append(empty);
    return;
  }

  const events = [...state.privacyEvents.events].reverse();

  for (const event of events) {
    const canSelect = event.event_type === "shielded_transfer";
    const row = document.createElement("button");
    row.type = "button";
    row.className = "event-row";
    row.classList.toggle("selected", event.tx_hash_hex === state.privacyEvents.selectedTxHash);
    row.disabled = !canSelect;
    if (canSelect) {
      row.addEventListener("click", () => selectPrivacyEvent(event.tx_hash_hex));
    }
    const copy = document.createElement("div");
    copy.className = "row-copy";
    const title = document.createElement("strong");
    title.textContent = event.event_type;
    const meta = document.createElement("span");
    meta.textContent = `height ${event.height}`;
    const txHash = document.createElement("code");
    txHash.textContent = shorten(event.tx_hash_hex, 14, 12);

    copy.append(title, meta);
    row.append(copy, txHash);
    els.eventsList.append(row);
  }
  if (!els.eventsList.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No events";
    els.eventsList.append(empty);
  }
}

function renderBlockEvents() {
  els.blockEventsList.innerHTML = "";

  if (state.blockEvents.error) {
    els.blockEventsState.textContent = "Unable to load";
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.blockEvents.error;
    els.blockEventsList.append(empty);
    return;
  }

  const events = state.blockEvents.events;
  els.blockEventsState.textContent = `${events.length} recent txs`;

  for (const event of events) {
    const row = document.createElement("article");
    row.className = "block-event-row";
    row.classList.toggle("send-event", event.type === "send");

    const copy = document.createElement("div");
    copy.className = "row-copy";
    const title = document.createElement("strong");
    title.textContent = event.type;
    const meta = document.createElement("span");
    meta.textContent = `height ${event.height}${event.summary?.amount ? ` / ${event.summary.amount}` : ""}${event.summary?.evmFailure ? ` / ${event.summary.evmFailure}` : ""}`;
    copy.append(title, meta);

    const details = document.createElement("div");
    details.className = "block-event-detail";
    const from = document.createElement("span");
    from.textContent = `from ${shorten(event.summary?.from, 12, 10)}`;
    const to = document.createElement("span");
    to.textContent = `to ${shorten(event.summary?.to, 12, 10)}`;
    const txHash = document.createElement("code");
    txHash.textContent = shorten(event.tx_hash_hex, 14, 12);
    details.append(from, to, txHash);

    row.append(copy, details);
    els.blockEventsList.append(row);
  }

  if (!els.blockEventsList.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No recent txs";
    els.blockEventsList.append(empty);
  }
}

function selectPrivacyEvent(txHash) {
  state.privacyEvents.selectedTxHash = txHash;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  renderPrivacyEvents();
  renderEventDetail();
}

function selectedPrivacyEvent() {
  return state.privacyEvents.events.find(event => event.tx_hash_hex === state.privacyEvents.selectedTxHash);
}

function clearEventDisclosureResult() {
  els.eventDisclosurePlane.textContent = "-";
  els.eventDisclosurePolicy.textContent = "-";
  els.eventDisclosureOutputIndex.textContent = "-";
  els.eventDisclosureCommitment.textContent = "-";
  els.eventDisclosureDigest.textContent = "-";
  els.eventDisclosureVerified.textContent = "-";
  els.eventDisclosureFields.textContent = "-";
  els.eventDisclosureAmount.textContent = "-";
  els.eventDisclosureFrom.textContent = "-";
  els.eventDisclosureTo.textContent = "-";
}

function renderEventDisclosureReport(report) {
  clearEventDisclosureResult();
  const view = disclosureViewModel(report);
  if (!view.verified) {
    els.eventDisclosureVerified.textContent = "false";
    els.eventDisclosureState.textContent = "Disclosure verification failed. Plaintext was discarded.";
    return;
  }
  const summary = view.summary;
  const amount = summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";
  els.eventDisclosurePlane.textContent = view.plane || "-";
  els.eventDisclosurePolicy.textContent = view.policy || "-";
  els.eventDisclosureOutputIndex.textContent = view.outputIndex === null ? "-" : String(view.outputIndex);
  els.eventDisclosureCommitment.textContent = view.commitmentHex || "-";
  els.eventDisclosureDigest.textContent = view.digestHex || "-";
  els.eventDisclosureVerified.textContent = "true";
  els.eventDisclosureFields.textContent = (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") || "-";
  els.eventDisclosureAmount.textContent = amount;
  els.eventDisclosureFrom.textContent = summary.from_shielded_address || "-";
  els.eventDisclosureTo.textContent = summary.to_shielded_address || "-";
  els.eventDisclosureState.textContent = `${summary.delivery || "recipient-encrypted"} / ${summary.policy || "unknown policy"}`;
}

function isDisclosureVerificationFailure(error) {
  return /digest mismatch|verification failed|commitment mismatch|output .* mismatch/i.test(
    String(error?.message || error || "")
  );
}

function renderEventDisclosureError(error) {
  clearEventDisclosureResult();
  const message = String(error?.message || error || "Disclosure decode failed");
  if (isDisclosureVerificationFailure(error)) {
    els.eventDisclosureVerified.textContent = "false";
  }
  els.eventDisclosureState.textContent = message;
}

function renderEventDetail() {
  const event = selectedPrivacyEvent();
  els.eventDetailType.textContent = event?.event_type || "-";
  els.eventDetailHeight.textContent = event?.height || "-";
  els.eventDetailTx.textContent = event?.tx_hash_hex || "-";
  els.eventDetailUserMode.textContent = event ? eventAttribute(event, "user_disclosure_mode") || "-" : "-";
  els.eventDetailTarget.textContent = event ? eventAttribute(event, "user_disclosure_target_pubkey") || "-" : "-";
  clearEventDisclosureResult();
  if (state.privacyEvents.decoded) {
    renderEventDisclosureReport(state.privacyEvents.decoded);
  } else if (state.privacyEvents.error) {
    renderEventDisclosureError(state.privacyEvents.error);
  } else {
    els.eventDisclosureState.textContent = eventDisclosureStatus(event);
  }
  els.decodeEventDisclosure.disabled = state.privacyEvents.loading || !canDecodeEventDisclosure(event);
  els.decodeSelfViewDisclosure.disabled = state.privacyEvents.loading || !canDecodeSelfViewDisclosure(event);
}

function hasAuditorUi() {
  return serverFeature("auditorAdmin") && Boolean(els.refreshAuditorTransfers && els.auditorEventsList);
}

function auditorDetailValueElements() {
  return [
    els.auditorTxHash,
    els.auditorVerification,
    els.auditorAmount,
    els.auditorDigest,
    els.auditorPlanePolicy,
    els.auditorOutputIndex,
    els.auditorCommitment,
    els.auditorFrom,
    els.auditorFields,
    els.auditorTo
  ].filter(Boolean);
}

function setAuditorValueTone(elements, tone = "") {
  for (const element of elements) {
    element.classList.remove("audit-value-encoded", "audit-value-decoded");
    if (tone) {
      element.classList.add(`audit-value-${tone}`);
    }
  }
}

function renderAuditorTestScalar() {
  if (!els.auditorTestScalar) return;
  if (state.auditor.testScalar) {
    const suffix = state.auditor.testScalarMatchesAuditConfig ? " (matches audit config)" : " (not current audit config)";
    els.auditorTestScalar.textContent = `${state.auditor.testScalar}${suffix}`;
  } else {
    els.auditorTestScalar.textContent = state.auditor.testScalarError || "-";
  }
  updateAuditorDecodeButton();
}

async function refreshAuditorTestScalar() {
  if (!hasAuditorUi() || !els.auditorTestScalar) return;
  els.auditorTestScalar.textContent = "Loading...";
  updateAuditorDecodeButton();
  try {
    const data = await api("/api/auditor/test-scalar");
    state.auditor.testScalar = data.disclosure_private_scalar_hex || "";
    state.auditor.testScalarError = "";
    state.auditor.testScalarMatchesAuditConfig = Boolean(data.matches_audit_config);
  } catch (error) {
    state.auditor.testScalar = "";
    state.auditor.testScalarError = `Unavailable: ${error.message}`;
    state.auditor.testScalarMatchesAuditConfig = false;
  }
  renderAuditorTestScalar();
  updateAuditorDecodeButton();
}

function updateAuditorDecodeButton() {
  if (!els.decodeAuditorTransfer) return;
  const scalar = state.auditor.testScalar || "";
  els.decodeAuditorTransfer.disabled = state.auditor.loading ||
    !state.auditor.selectedTxHash ||
    !/^[0-9a-fA-F]{1,64}$/.test(scalar);
}

async function decodeSelectedEventDisclosure() {
  const event = selectedPrivacyEvent();
  if (!event || !canDecodeEventDisclosure(event)) return;
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  els.eventDisclosureState.textContent = "Disclosure 조회 중...";
  renderEventDetail();
  try {
    const report = await clairveilBrowserClient().decodeUserDisclosure(privacyRequest({ txHash: event.tx_hash_hex }));
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    state.privacyEvents.loading = false;
    renderEventDetail();
  }
}

async function decodeSelectedSelfViewDisclosure() {
  const event = selectedPrivacyEvent();
  if (!event || !canDecodeSelfViewDisclosure(event)) return;
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  els.eventDisclosureState.textContent = "Self-view 조회 중...";
  renderEventDetail();
  try {
    const report = await clairveilBrowserClient().decodeSelfViewDisclosure(
      privacyRequest({ txHash: event.tx_hash_hex })
    );
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    state.privacyEvents.loading = false;
    renderEventDetail();
  }
}

function disclosureMaterial() {
  if (!state.keplr.rootSignatureBase64) return null;
  return derivePrivacyMaterial({
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    shieldedPrefix: shieldedPrefix()
  });
}

async function decodeDisclosureSource() {
  const plane = els.disclosureSourcePlane.value;
  const pasted = els.disclosureSourceEventJson.value.trim();
  const inputTxHash = els.disclosureSourceTxHash.value.trim();
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  els.decodeDisclosureSource.disabled = true;
  clearEventDisclosureResult();
  els.eventDisclosureState.textContent = "Decoding selected disclosure source…";
  try {
    let report;
    if (pasted) {
      let event;
      try {
        event = JSON.parse(pasted);
      } catch {
        throw new Error("Pasted disclosure event must be valid JSON");
      }
      const txHash = inputTxHash || event?.tx_hash_hex || "";
      const material = disclosureMaterial();
      if (plane === "user") {
        report = decodeUserDisclosureFromEvent(
          event,
          material?.disclosureScalar || 1n,
          material?.disclosurePubKeyHex || "",
          txHash,
          { assetDenom: baseDenom() }
        );
      } else if (plane === "self-view") {
        if (!material) throw new Error("Setup Clairveil before decoding self-view disclosure");
        report = decodeSelfViewDisclosureFromEvent(event, material.disclosureScalar, txHash, { assetDenom: baseDenom() });
      } else {
        if (!/^[0-9a-fA-F]{1,64}$/.test(state.auditor.testScalar || "")) {
          throw new Error("Local admin audit scalar is unavailable");
        }
        report = decodeAuditDisclosureFromEvent(
          event,
          disclosureScalarFromHex(state.auditor.testScalar),
          txHash,
          { assetDenom: baseDenom() }
        );
      }
    } else {
      if (!/^(0x)?[0-9a-fA-F]{64}$/.test(inputTxHash)) {
        throw new Error("Enter a 32-byte transaction hash or paste disclosure event JSON");
      }
      const request = privacyRequest({ txHash: inputTxHash, limit: 200, maxPages: 1000 });
      if (plane === "user") {
        report = await clairveilBrowserClient().decodeUserDisclosure(request);
      } else if (plane === "self-view") {
        if (!state.keplr.rootSignatureBase64) throw new Error("Setup Clairveil before decoding self-view disclosure");
        report = await clairveilBrowserClient().decodeSelfViewDisclosure(request);
      } else {
        if (!/^[0-9a-fA-F]{1,64}$/.test(state.auditor.testScalar || "")) {
          throw new Error("Local admin audit scalar is unavailable");
        }
        report = await api("/api/auditor/decode", {
          method: "POST",
          body: JSON.stringify({ txHash: inputTxHash, disclosurePrivKeyHex: state.auditor.testScalar })
        });
      }
    }
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    state.privacyEvents.loading = false;
    els.decodeDisclosureSource.disabled = false;
  }
}

function clearAuditorReport(message = "Select a transfer.") {
  if (!hasAuditorUi()) return;
  setAuditorValueTone(auditorDetailValueElements());
  els.auditorTxHash.textContent = "-";
  els.auditorVerification.textContent = "-";
  els.auditorAmount.textContent = "-";
  els.auditorFrom.textContent = "-";
  els.auditorTo.textContent = "-";
  els.auditorFields.textContent = "-";
  els.auditorDigest.textContent = "-";
  els.auditorPlanePolicy.textContent = "-";
  els.auditorOutputIndex.textContent = "-";
  els.auditorCommitment.textContent = "-";
  els.auditorDecodeState.textContent = message;
  updateAuditorDecodeButton();
}

function renderAuditorEventDetail(event) {
  if (!hasAuditorUi()) return;
  if (!event) {
    clearAuditorReport();
    return;
  }

  const target = eventAttribute(event, "audit_disclosure_target_pubkey");
  const digest = eventAttribute(event, "audit_disclosure_digest");
  const payload = eventAttribute(event, "audit_disclosure_payload");

  els.auditorTxHash.textContent = event.tx_hash_hex || "-";
  els.auditorVerification.textContent = event.height || "-";
  els.auditorAmount.textContent = target ? shorten(target, 14, 12) : "-";
  els.auditorDigest.textContent = digest ? shorten(digest, 14, 12) : "-";
  els.auditorPlanePolicy.textContent = "audit / encrypted";
  els.auditorOutputIndex.textContent = "-";
  els.auditorCommitment.textContent = "-";
  els.auditorFrom.textContent = payload ? shorten(payload, 14, 12) : "-";
  els.auditorFields.textContent = "encrypted";
  els.auditorTo.textContent = "decode UI deferred";
  setAuditorValueTone(auditorDetailValueElements(), "encoded");
  els.auditorDecodeState.textContent = "Audit disclosure is present. Select Decode to use the local admin test scalar.";
  updateAuditorDecodeButton();
}

function renderAuditorReport(report) {
  if (!hasAuditorUi()) return;
  const view = disclosureViewModel(report);
  if (!view.verified) {
    clearAuditorReport("Disclosure verification failed. Plaintext was discarded.");
    els.auditorVerification.textContent = "Failed";
    return;
  }
  const summary = view.summary;
  const amount = summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";

  els.auditorTxHash.textContent = report?.tx_hash || state.auditor.selectedTxHash || "-";
  els.auditorVerification.textContent = "Verified";
  els.auditorAmount.textContent = amount;
  els.auditorFrom.textContent = summary.from_shielded_address || "-";
  els.auditorTo.textContent = summary.to_shielded_address || "-";
  els.auditorFields.textContent = (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") || "-";
  els.auditorDigest.textContent = view.digestHex || eventAttribute(
    state.auditor.events.find(event => event.tx_hash_hex === state.auditor.selectedTxHash),
    "audit_disclosure_digest"
  ) || "-";
  els.auditorPlanePolicy.textContent = `${view.plane || "audit"} / ${view.policy || "unknown policy"}`;
  els.auditorOutputIndex.textContent = view.outputIndex === null ? "-" : String(view.outputIndex);
  els.auditorCommitment.textContent = view.commitmentHex || "-";
  setAuditorValueTone(auditorDetailValueElements(), "decoded");
  els.auditorDecodeState.textContent = `${summary.delivery || report?.source || "audit"} / ${summary.policy || "unknown policy"}`;
  updateAuditorDecodeButton();
}

function renderAuditorTransfers() {
  if (!hasAuditorUi()) return;
  els.auditorEventsList.innerHTML = "";
  const events = [...state.auditor.events].reverse().slice(0, 20);

  for (const event of events) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "audit-row";
    row.classList.toggle("selected", event.tx_hash_hex === state.auditor.selectedTxHash);
    row.disabled = state.auditor.loading;
    row.addEventListener("click", () => selectAuditorTransfer(event.tx_hash_hex));

    const copy = document.createElement("div");
    copy.className = "row-copy";
    const title = document.createElement("strong");
    title.textContent = shorten(event.tx_hash_hex, 14, 12);
    const meta = document.createElement("span");
    meta.textContent = `height ${event.height}`;
    const digest = document.createElement("code");
    digest.textContent = shorten(eventAttribute(event, "audit_disclosure_digest"), 12, 10);

    copy.append(title, meta);
    row.append(copy, digest);
    els.auditorEventsList.append(row);
  }

  if (!els.auditorEventsList.childElementCount) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No auditable transfers";
    els.auditorEventsList.append(empty);
  }
}

async function refreshAuditorTransfers() {
  if (!hasAuditorUi()) return;
  setBusy(els.refreshAuditorTransfers, true);
  try {
    const data = await clairveilBrowserClient().fetchAuditableTransfers();
    state.auditor.events = data.events || [];
    if (state.auditor.selectedTxHash && !state.auditor.events.some(event => event.tx_hash_hex === state.auditor.selectedTxHash)) {
      state.auditor.selectedTxHash = "";
      state.auditor.decoded = null;
      clearAuditorReport();
    }
    renderAuditorTransfers();
    renderAuditorEventDetail(state.auditor.events.find(event => event.tx_hash_hex === state.auditor.selectedTxHash));
  } finally {
    setBusy(els.refreshAuditorTransfers, false);
  }
}

function selectAuditorTransfer(txHash) {
  if (!hasAuditorUi()) return;
  state.auditor.selectedTxHash = txHash;
  state.auditor.decoded = null;
  renderAuditorTransfers();
  renderAuditorEventDetail(state.auditor.events.find(event => event.tx_hash_hex === txHash));
  updateAuditorDecodeButton();
}

async function decodeAuditorTransfer(txHash = state.auditor.selectedTxHash) {
  if (!hasAuditorUi()) {
    if (txHash) selectAuditorTransfer(txHash);
    return;
  }
  if (!txHash) {
    clearAuditorReport("Select a transfer first.");
    return;
  }
  const disclosurePrivKeyHex = state.auditor.testScalar || "";
  if (!/^[0-9a-fA-F]{1,64}$/.test(disclosurePrivKeyHex)) {
    state.auditor.selectedTxHash = txHash;
    clearAuditorReport("Local admin test scalar is unavailable.");
    renderAuditorTransfers();
    return;
  }

  state.auditor.selectedTxHash = txHash;
  state.auditor.loading = true;
  state.auditor.decoded = null;
  clearAuditorReport("Decoding audit disclosure with injected scalar...");
  renderAuditorTransfers();

  try {
    const report = await api("/api/auditor/decode", {
      method: "POST",
      body: JSON.stringify({ txHash, disclosurePrivKeyHex })
    });
    state.auditor.decoded = report;
    renderAuditorReport(report);
  } catch (error) {
    clearAuditorReport(error.message);
    if (isDisclosureVerificationFailure(error)) {
      els.auditorVerification.textContent = "Failed";
    }
  } finally {
    state.auditor.loading = false;
    renderAuditorTransfers();
    updateAuditorDecodeButton();
  }
}

function canConnectWallet(walletType) {
  if (state.activeWallet && state.activeWallet !== walletType) {
    toast("Disconnect the current wallet before connecting another one.");
    return false;
  }
  return true;
}

async function connectWallet() {
  if (!canConnectWallet("metamask")) return;
  if (activeWalletKind() !== "metamask") {
    toast("Selected DApp chain uses Keplr.");
    return;
  }
  if (!selectedProfileMatchesServer()) {
    toast("Selected chain is not running in this DApp server. Restart the server for that chain profile.");
    return;
  }
  if (!metaMaskProvider()) {
    toast("MetaMask not found");
    return;
  }
  await ensureMetaMaskChain();
  const accounts = await requestMetaMask({ method: "eth_requestAccounts" });
  const account = accounts[0] || "";
  if (!account) {
    resetWalletSession();
    renderWallet();
    renderKeplr();
    return;
  }
  await ensureMetaMaskChain();
  resetKeplrSession();
  state.activeWallet = "metamask";
  state.wallet.account = account;
  state.wallet.chainId = await requestMetaMask({ method: "eth_chainId" });
  const identity = clairveilBrowserClient().evmAccountIdentity(account);
  state.keplr.account = identity.address || "";
  state.keplr.name = "MetaMask";
  state.keplr.pubkeyHex = identity.pubKeyHex || "";
  state.keplr.expectedAddress = identity.address || "";
  state.keplr.addressMatches = Boolean(identity.address);
  state.keplr.signerCheck = "OK (EVM address)";
  hydratePublicPendingTransactions();
  if (!els.veiledWithdrawRecipient.value && identity.evmAddress) {
    els.veiledWithdrawRecipient.value = identity.evmAddress;
  }
  renderWallet();
  renderKeplr();
  try {
    await refreshWalletBalance();
  } catch (error) {
    state.keplr.balance = error.message;
    renderKeplr();
  }
}

async function signMetaMaskSession() {
  const account = state.wallet.account;
  if (!account) return;
  await ensureMetaMaskChain();
  const local = selectedLocalAccount()?.name || "alice";
  const message = [
    "Clairveil local test session",
    `MetaMask: ${account}`,
    `Local signer: ${local}`,
    `Chain: ${state.config?.chainId || "clairveil-local-2"}`,
    `Time: ${new Date().toISOString()}`
  ].join("\n");
  const signature = await requestMetaMask({
    method: "personal_sign",
    params: [message, account]
  });
  state.wallet.signatureHash = await digestText(signature);
  renderWallet();
  toast("Session signed");
}

async function signSession() {
  if (state.activeWallet === "metamask") {
    await signMetaMaskSession();
    return;
  }
  if (state.activeWallet === "keplr") {
    await signKeplrSession();
  }
}

async function getKeplrOfflineAccounts(chainId) {
  try {
    let signer = null;
    if (typeof window.getOfflineSignerAuto === "function") {
      signer = await window.getOfflineSignerAuto(chainId);
    } else if (typeof window.keplr?.getOfflineSignerAuto === "function") {
      signer = await window.keplr.getOfflineSignerAuto(chainId);
    } else if (typeof window.getOfflineSigner === "function") {
      signer = window.getOfflineSigner(chainId);
    } else if (typeof window.keplr?.getOfflineSigner === "function") {
      signer = window.keplr.getOfflineSigner(chainId);
    }
    if (typeof signer?.getAccounts !== "function") {
      return [];
    }
    return await signer.getAccounts();
  } catch {
    return [];
  }
}

async function resolveKeplrSigner(chainId, key) {
  const candidates = [];
  if (key?.bech32Address && key?.pubKey) {
    candidates.push({
      source: "Keplr getKey",
      address: key.bech32Address,
      pubKeyHex: bytesToHex(key.pubKey)
    });
  }

  const offlineAccounts = await getKeplrOfflineAccounts(chainId);
  for (const account of offlineAccounts) {
    const address = account.address || account.bech32Address || "";
    const pubKey = account.pubkey || account.pubKey;
    if (!address || !pubKey) continue;
    candidates.push({
      source: "Keplr offline signer",
      address,
      pubKeyHex: bytesToHex(pubKey)
    });
  }

  const uniqueCandidates = candidates.filter((candidate, index) =>
    candidates.findIndex(other =>
      other.address === candidate.address && other.pubKeyHex === candidate.pubKeyHex
    ) === index
  );

  for (const candidate of uniqueCandidates) {
    try {
      const signerCheck = clairveilBrowserClient().verifySignerPubKey(candidate.address, candidate.pubKeyHex);
      if (signerCheck.matches) {
        return { ...candidate, signerCheck, candidates: uniqueCandidates };
      }
      candidate.signerCheck = signerCheck;
    } catch (error) {
      candidate.error = error.message;
    }
  }

  return {
    ...(uniqueCandidates[0] || { source: "Keplr", address: key?.bech32Address || "", pubKeyHex: "" }),
    signerCheck: uniqueCandidates[0]?.signerCheck || {
      expectedAddress: "",
      matches: false
    },
    candidates: uniqueCandidates
  };
}

async function connectKeplr() {
  if (!canConnectWallet("keplr")) return;
  if (activeWalletKind() !== "keplr") {
    toast("Selected DApp chain uses MetaMask.");
    return;
  }
  if (!selectedProfileMatchesServer()) {
    toast("Selected chain is not running in this DApp server. Restart the server for that chain profile.");
    return;
  }
  if (!window.keplr) {
    toast("Keplr not found");
    return;
  }
  const chainInfo = activeKeplrChainInfo();
  if (!chainInfo) {
    throw new Error("Selected chain does not include Keplr chain info");
  }
  await window.keplr.experimentalSuggestChain(chainInfo);
  await window.keplr.enable(chainInfo.chainId);
  const key = await window.keplr.getKey(chainInfo.chainId);
  const signer = await resolveKeplrSigner(chainInfo.chainId, key);

  resetMetaMaskSession();
  state.activeWallet = "keplr";
  state.keplr.account = signer.address || key.bech32Address || "";
  state.keplr.name = key.name || "";
  state.keplr.pubkeyHex = signer.pubKeyHex || "";
  state.keplr.expectedAddress = "";
  state.keplr.addressMatches = false;
  state.keplr.signerCheck = "Checking...";
  state.keplr.signatureHash = "";
  state.keplr.verified = false;
  state.keplr.balance = "";
  state.keplr.transparentBalances = {};
  state.keplr.evmNativeBalance = "0";
  state.keplr.faucetHash = "";
  state.keplr.faucetSent = "";
  state.keplr.faucetRecipient = "";
  state.keplr.shieldedAddress = "";
  state.keplr.disclosurePubKeyHex = "";
  state.keplr.rootSignatureBase64 = "";
  state.keplr.rootSignatureHash = "";
  state.keplr.sendHash = "";
  state.keplr.sendStatus = "idle";
  state.keplr.depositHash = "";
  state.keplr.depositHeight = "";
  state.keplr.depositPrepared = null;
  state.keplr.depositRecoveryStatus = "idle";
  state.keplr.depositRecoveryMessage = "Not started";
  state.keplr.networkFeeEstimate = "Not estimated";
  state.keplr.networkFeeAmount = "0";
  state.keplr.transferHash = "";
  state.keplr.withdrawHash = "";
  state.keplr.withdrawHeight = "";
  state.keplr.withdrawNullifierStatus = "Not checked";
  state.keplr.withdrawReceiveStatus = "Not checked";
  state.keplr.notesSummary = "";
  state.keplr.notes = [];
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  hydratePublicPendingTransactions();
  renderKeplr();

  state.keplr.expectedAddress = signer.signerCheck?.expectedAddress || "";
  state.keplr.addressMatches = Boolean(signer.signerCheck?.matches);
  state.keplr.signerCheck = state.keplr.addressMatches
    ? `OK (${signer.source})`
    : `Mismatch: ${shorten(state.keplr.expectedAddress, 12, 10)}`;
  renderKeplr();

  if (!state.keplr.addressMatches) {
    const sources = signer.candidates?.length
      ? signer.candidates.map(candidate => `${candidate.source}: ${shorten(candidate.address, 12, 10)}`).join(", ")
      : "no Keplr signer candidates";
    toast(
      `Keplr address/pubKey mismatch on ${chainInfo.chainId}. Checked ${sources}. Remove Clairveil Localnet (${chainInfo.chainId}) from Keplr once, reconnect, and try again. You do not need to change chains on every restart.`
    );
    return;
  }

  await refreshWalletBalance();
  toast("Keplr connected");
}

async function signKeplrSession() {
  if (!window.keplr || !state.keplr.account) return;
  const chainInfo = activeKeplrChainInfo();
  if (!chainInfo) {
    throw new Error("Selected chain does not include Keplr chain info");
  }
  const local = selectedLocalAccount()?.name || "alice";
  const message = [
    "Clairveil local test session",
    `Keplr: ${state.keplr.account}`,
    `Local signer: ${local}`,
    `Chain: ${chainInfo.chainId}`,
    `Time: ${new Date().toISOString()}`
  ].join("\n");
  const signature = await window.keplr.signArbitrary(chainInfo.chainId, state.keplr.account, message);
  state.keplr.signatureHash = await digestText(signature.signature);
  if (typeof window.keplr.verifyArbitrary === "function") {
    state.keplr.verified = await window.keplr.verifyArbitrary(
      chainInfo.chainId,
      state.keplr.account,
      message,
      signature
    );
  }
  renderKeplr();
  toast("Keplr session signed");
}

function disconnectWallet() {
  resetWalletSession();
  renderWallet();
  renderKeplr();
  toast("Wallet disconnected");
}

async function fundKeplr() {
  if (!state.keplr.account) return;
  if (!serverFeature("faucet")) {
    toast("Faucet is available only when this DApp server is attached to a local test node.");
    return;
  }
  const amount = clairInputToUclair(els.keplrFaucetAmount);
  const recipient = connectedPublicRecipientAddress();
  const localSigner = selectedLocalAccount()?.name || state.accounts[0]?.name || "alice";
  setBusy(els.fundKeplr, true);
  try {
    const data = await api("/api/faucet", {
      method: "POST",
      body: JSON.stringify({
        from: localSigner,
        recipient,
        amount
      })
    });
    state.keplr.faucetHash = data.broadcast?.txhash || "";
    state.keplr.faucetSent = formatUclairAsClair(data.amount?.funded?.replace(baseDenom(), "") || "0");
    state.keplr.faucetRecipient = isEvmTransparentMode() ? data.recipientEvm || recipient : data.recipient || recipient;
    state.keplr.balance = formatBalances(data.balance?.balances);
    await refreshWalletBalance();
    renderKeplr();
    toast(`Faucet sent: ${state.keplr.faucetSent}`);
  } catch (error) {
    toast(error.message);
  } finally {
    setBusy(els.fundKeplr, false);
  }
}

async function completeInitialPrivacySetup({ skipInitialSync = false } = {}) {
  await refreshProtocolStatus();
  if (!state.protocol.ready) {
    throw new Error(state.protocol.error || "Consensus circuit and asset preflight failed");
  }
  if (!skipInitialSync && state.keplr.noteSyncStatus !== "synced") {
    els.keplrTxState.textContent = "Initial note sync";
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      skipSetup: true,
      maxPages: 1000
    });
    if (state.keplr.noteSyncStatus !== "synced") {
      throw new Error("Initial note sync did not reach the latest durable cursor");
    }
  }
}

async function setupKeplrPrivacy(options = {}) {
  if (!state.keplr.account) return;
  if (state.keplr.rootSignatureBase64 && state.keplr.shieldedAddress && state.keplr.disclosurePubKeyHex) {
    await refreshReservationState();
    await completeInitialPrivacySetup(options);
    els.keplrTxState.textContent = options.skipInitialSync ? "Identity ready" : "Ready · notes synced";
    renderKeplr();
    return;
  }

  setBusy(els.setupKeplrPrivacy, true);
  els.keplrTxState.textContent = "Setting up";
  try {
    let account;
    if (state.activeWallet === "metamask") {
      await ensureMetaMaskChain();
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(state.keplr.account, state.keplr.pubkeyHex);
      const signatureHex = await requestMetaMask({
        method: "personal_sign",
        params: [rootMessage, state.wallet.account]
      });
      state.keplr.rootSignatureBase64 = bytesToBase64(hexToBytes(signatureHex));
      account = clairveilBrowserClient().derivePrivacyAccount({
        walletType: "evm",
        address: state.keplr.account,
        pubKeyHex: state.keplr.pubkeyHex,
        signatureBase64: state.keplr.rootSignatureBase64
      });
    } else {
      if (!window.keplr) return;
      const chainInfo = activeKeplrChainInfo();
      if (!chainInfo) {
        throw new Error("Selected chain does not include Keplr chain info");
      }
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(state.keplr.account, state.keplr.pubkeyHex);
      const signature = await window.keplr.signArbitrary(chainInfo.chainId, state.keplr.account, rootMessage);
      state.keplr.rootSignatureBase64 = signature.signature;
      account = clairveilBrowserClient().derivePrivacyAccount({
        address: state.keplr.account,
        pubKeyHex: state.keplr.pubkeyHex,
        signatureBase64: signature.signature
      });
    }
    state.keplr.shieldedAddress = account.shielded_address || "";
    state.keplr.disclosurePubKeyHex = account.disclosure_pubkey_hex || "";
    state.keplr.rootSignatureHash = account.root_signature_hash || "";
    try {
      await hydrateRelayWithdrawRecovery();
    } catch (error) {
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = error.message;
      state.reservations.status = "error";
      state.reservations.message = error.message;
      state.reservations.retryBlocked = true;
    }
    await refreshReservationState();
    await completeInitialPrivacySetup(options);
    els.keplrTxState.textContent = options.skipInitialSync ? "Identity ready" : "Ready · notes synced";
    renderKeplr();
    toast(options.skipInitialSync ? "Clairveil identity ready" : "Clairveil account ready · notes synced");
  } catch (error) {
    els.keplrTxState.textContent = "Setup failed";
    toast(error.message);
  } finally {
    setBusy(els.setupKeplrPrivacy, false);
    renderKeplr();
  }
}

async function copyKeplrDisclosurePubKey() {
  if (!state.keplr.disclosurePubKeyHex) {
    toast("Setup Clairveil first");
    return;
  }
  await navigator.clipboard.writeText(state.keplr.disclosurePubKeyHex);
  toast("Disclosure pubkey copied");
}

async function copyKeplrShieldedAddress() {
  if (!state.keplr.shieldedAddress) {
    toast("Setup Clairveil first");
    return;
  }
  await navigator.clipboard.writeText(state.keplr.shieldedAddress);
  toast("Shielded address copied");
}

async function copyWalletAccount() {
  const account = currentWalletAccountForCopy();
  if (!account) {
    toast("Connect a wallet first");
    return;
  }
  await navigator.clipboard.writeText(account);
  toast("Account copied");
}

async function copyRelayWithdraw() {
  if (!state.relayWithdraw.json) throw new Error("Prepare a relay withdraw payload first");
  await navigator.clipboard.writeText(state.relayWithdraw.json);
  toast("Relay withdraw handoff JSON copied");
}

function downloadRelayWithdraw() {
  if (!state.relayWithdraw.json) throw new Error("Prepare a relay withdraw payload first");
  downloadTextFile(`clairveil-relay-withdraw-${Date.now()}.json`, state.relayWithdraw.json);
  toast("Relay withdraw handoff JSON downloaded");
}

async function signDirectAndBroadcast(signDoc, options = {}) {
  if (!window.keplr?.signDirect) {
    throw new Error("Keplr signDirect not available");
  }
  const wallet = {
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signDirect: directSignDoc => window.keplr.signDirect(
      directSignDoc.chainId,
      state.keplr.account,
      directSignDoc,
      keplrDirectSignOptions(options)
    )
  };
  return clairveilBrowserClient().signDirectAndBroadcast({
    wallet,
    signDoc,
    ...options
  });
}

async function submitEvmTransaction(transaction, options = {}) {
  if (!metaMaskProvider() || !state.wallet.account) {
    throw new Error("MetaMask is not connected");
  }
  const txHash = await clairveilBrowserClient().sendEvmTransaction({
    wallet: evmWalletAdapter(),
    transaction,
    ...options
  });
  return normalizeEvmTxHash(txHash);
}

async function waitForEvmTransaction(txHash, label = "EVM transaction", reservationBinding = {}) {
  const broadcast = await clairveilBrowserClient().waitForEvmTransaction(txHash);
  if (!broadcast?.receipt) {
    const manager = reservationBinding.reservationManager;
    const reservationIDs = preparedReservationIDs({ reservation: reservationBinding.reservation });
    if (manager && reservationIDs.length) {
      await manager.markUnknown(reservationIDs, {
        fromStatus: reservationStatuses.Submitted,
        txHash,
        error: "evm_receipt_polling_timeout",
        metadata: { reconcile_reason: "evm_receipt_polling_timeout" }
      });
      await refreshReservationState(manager);
    }
    return { ...broadcast, txHash: broadcast?.txHash || txHash, unknown: true };
  }
  try {
    assertSuccessfulBroadcast(broadcast, label);
  } catch (error) {
    error.broadcast = broadcast;
    error.txHash = broadcast.txHash || txHash;
    throw error;
  }
  return { ...broadcast, txHash: broadcast.txHash || txHash };
}

async function sendEvmTransaction(transaction, {
  waitForReceipt = false,
  label = "EVM transaction",
  reservationBinding = {}
} = {}) {
  const txHash = await submitEvmTransaction(transaction, reservationBinding);
  if (waitForReceipt) {
    const broadcast = await waitForEvmTransaction(txHash, label, reservationBinding);
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  }
  const waitPromise = waitForEvmTransaction(txHash, label, reservationBinding);
  waitPromise.catch(() => {});
  return {
    txHash,
    pending: true,
    waitPromise
  };
}

function watchEvmBroadcast(broadcast, { onIncluded, onUnknown, onFailed } = {}) {
  if (!broadcast?.waitPromise) return;
  broadcast.waitPromise.then(result => {
    return result.unknown ? onUnknown?.(result) : onIncluded?.(result);
  }).catch(error => {
    return onFailed?.(error);
  });
}

async function reconcilePublicEvmTransaction(kind) {
  const isDeposit = kind === "deposit";
  const txHash = isDeposit ? state.keplr.depositHash : state.keplr.sendHash;
  const button = isDeposit ? els.reconcileKeplrDeposit : els.reconcileKeplrSend;
  if (!txHash || activeChainProfile()?.transport !== "evm") return;
  if (isDeposit) {
    state.keplr.depositRecoveryStatus = "checking";
    state.keplr.depositRecoveryMessage = "Checking the existing tx hash · retry remains blocked";
  } else {
    state.keplr.sendStatus = "checking";
  }
  setBusy(button, true);
  renderKeplr();
  try {
    const result = await waitForEvmTransaction(txHash, isDeposit ? "EVM deposit" : "EVM send");
    if (result.unknown) {
      if (isDeposit) {
        state.keplr.depositRecoveryStatus = "unknown";
        state.keplr.depositRecoveryMessage = "Receipt still unknown · same deposit remains blocked";
      } else {
        state.keplr.sendStatus = "unknown";
      }
      showNotice({
        title: `${isDeposit ? "Deposit" : "Send"} 결과 미확정`,
        message: `기존 tx hash가 아직 포함 또는 실패로 확인되지 않았습니다. 같은 요청은 계속 차단됩니다.\nTx: ${shorten(txHash, 14, 12)}`
      });
      return;
    }

    if (isDeposit) {
      state.keplr.depositHeight = result.receipt?.blockNumber || state.keplr.depositHeight;
      updateIncludedDepositNetworkFee(result);
      if (state.keplr.depositPrepared) {
        await recoverDepositNote({ ...result, prepared: state.keplr.depositPrepared });
      } else {
        await scanKeplrNotes({ quiet: true, throwOnError: true });
        state.keplr.depositRecoveryStatus = "pending";
        state.keplr.depositRecoveryMessage = "Included · full rescan required to verify the prepared note";
      }
      await refreshPrivacySurfaces({ balance: true });
    } else {
      state.keplr.sendStatus = "included";
      await Promise.allSettled([refreshWalletBalance(), refreshBlockEvents()]);
    }
    els.keplrTxState.textContent = `${isDeposit ? "Deposit" : "Send"} included`;
    showNotice({
      title: `${isDeposit ? "Deposit" : "Send"} 결과 확인됨`,
      message: `기존 tx가 포함된 것을 확인했습니다. 새 요청을 만들 수 있습니다.\nTx: ${shorten(txHash, 14, 12)}`
    });
  } catch (error) {
    const failureConfirmed = evmReceiptHasFailed(error?.broadcast?.receipt);
    if (isDeposit) {
      state.keplr.depositRecoveryStatus = failureConfirmed ? "failed" : "unknown";
      state.keplr.depositRecoveryMessage = failureConfirmed
        ? `Failed on-chain · ${error.message}`
        : `Result still unknown · ${error.message}`;
    } else {
      state.keplr.sendStatus = failureConfirmed ? "failed" : "unknown";
    }
    els.keplrTxState.textContent = failureConfirmed
      ? `${isDeposit ? "Deposit" : "Send"} failed`
      : `${isDeposit ? "Deposit" : "Send"} status unknown`;
    showNotice({
      title: failureConfirmed
        ? `${isDeposit ? "Deposit" : "Send"} 실패 확인됨`
        : `${isDeposit ? "Deposit" : "Send"} 결과 미확정`,
      message: failureConfirmed
        ? error.message
        : `${error.message}\n실패 증거가 없으므로 같은 요청은 계속 차단됩니다.`,
      failed: failureConfirmed
    });
  } finally {
    setBusy(button, false);
    renderKeplr();
  }
}

function keplrPrivacyRequest(extra = {}) {
  return {
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    ...extra
  };
}

function evmWalletAdapter() {
  return {
    getChainId: () => requestMetaMask({ method: "eth_chainId" }),
    sendTransaction: async transaction => {
      await ensureMetaMaskChain();
      const tx = await withEstimatedEvmGas({ ...transaction, from: state.wallet.account });
      return requestMetaMask({
        method: "eth_sendTransaction",
        params: [tx]
      });
    }
  };
}

function evmPrivacyRequest(extra = {}) {
  return {
    walletType: "evm",
    evmWallet: evmWalletAdapter(),
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    ...extra
  };
}

function privacyRequest(extra = {}) {
  return state.activeWallet === "metamask"
    ? evmPrivacyRequest(extra)
    : keplrPrivacyRequest(extra);
}

async function preparePrivacyDepositSignDoc(amount, options = {}) {
  return clairveilBrowserClient().prepareDeposit(privacyRequest({
    amount,
    signal: options.signal
  }));
}

async function preparePrivacyTransferSignDoc(amount, recipient, disclosure = {}, options = {}) {
  const manager = await currentReservationManager();
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  const amountValue = parsePlannerAmountValue(amount);
  if (amountValue === null) throw new Error("Transfer amount must be a canonical integer");
  const data = await clairveilBrowserClient().prepareTransfer(privacyRequest({
    amount,
    recipient,
    scan: { scanSource: "privacy_scan", limit: 200, maxPages: 1000 },
    ...disclosure,
    expectedRecipientHash: hashRecipient(recipient, { shieldedPrefix: shieldedPrefix() }),
    expectedAmountHash: hashAmount(baseDenom(), amountValue),
    reservationManager: manager,
    allowPlanStep: Boolean(options.allowPlanStep),
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  await refreshReservationState(manager);
  return { ...data, reservationManager: manager, reservationKind: "transfer", reservationRecipient: recipient };
}

async function preparePrivacyWithdrawSignDoc(amount, recipient, options = {}) {
  const manager = await currentReservationManager();
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  const data = await clairveilBrowserClient().prepareWithdraw(privacyRequest({
    amount,
    recipient,
    scan: { scanSource: "privacy_scan", limit: 200, maxPages: 1000 },
    reservationManager: manager,
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  await refreshReservationState(manager);
  return { ...data, reservationManager: manager, reservationKind: "withdraw", reservationRecipient: recipient };
}

async function preparePrivacyRelayWithdraw(amount, recipient, options = {}) {
  const manager = await currentReservationManager();
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  const data = await clairveilBrowserClient().prepareRelayWithdraw(privacyRequest({
    amount,
    recipient,
    scan: { scanSource: "privacy_scan", limit: 200, maxPages: 1000 },
    reservationManager: manager,
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  await refreshReservationState(manager);
  return { ...data, reservationManager: manager, reservationKind: "relay", reservationRecipient: recipient };
}

async function broadcastPrivacyDeposit(amount, label = "deposit", options = {}) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  await refreshWalletBalance();
  const feeBudget = await estimateDepositFeeBeforeProof();
  assertDepositFunding(amount, feeBudget);
  const data = await preparePrivacyDepositSignDoc(amount, options);
  state.keplr.shieldedAddress = data.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  const exactFee = await updateDepositNetworkFee(data.transaction);
  assertDepositFunding(amount, exactFee);
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, options);
  if (!broadcast.pending) updateIncludedDepositNetworkFee(broadcast);
  state.keplr.depositHash = broadcast.broadcast?.txhash || "";
  state.keplr.depositHash = state.keplr.depositHash || broadcast.txHash || "";
  state.keplr.depositHeight = broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
  return { ...broadcast, prepared: data.prepared };
}

function normalizedHex(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
}

function noteCommitment(note) {
  return note?.commitment
    || note?.commitmentHex
    || note?.commitment_hex
    || note?.noteCommitmentHex
    || note?.note_commitment_hex
    || "";
}

async function recoverDepositNote(broadcast) {
  const prepared = broadcast?.prepared || {};
  const expectedCommitment = normalizedHex(prepared.noteCommitmentHex);
  state.keplr.depositRecoveryStatus = "recovering";
  state.keplr.depositRecoveryMessage = "Included · recovering encrypted note";
  renderKeplr();
  try {
    if (activeChainProfile()?.transport !== "evm") {
      await clairveilBrowserClient().confirmDeposit({
        txHash: broadcast.broadcast?.txhash || broadcast.txHash || state.keplr.depositHash,
        expectedCommitment: prepared.noteCommitmentHex,
        expectedEncryptedNote: prepared.encryptedNoteHex
      });
    }
    await scanKeplrNotes({ quiet: true, throwOnError: true });
    const recovered = expectedCommitment
      && state.keplr.notes.some(note => normalizedHex(noteCommitment(note)) === expectedCommitment);
    if (!recovered) {
      throw new Error("Deposit was included, but its prepared note is not in the local wallet cache yet");
    }
    state.keplr.depositRecoveryStatus = "recovered";
    state.keplr.depositRecoveryMessage = "Recovered · encrypted note available";
    return true;
  } catch (error) {
    state.keplr.depositRecoveryStatus = "pending";
    state.keplr.depositRecoveryMessage = `Included · recovery pending (${error.message})`;
    return false;
  } finally {
    renderKeplr();
  }
}

function broadcastTxEvents(broadcast) {
  return broadcast?.tx?.tx_result?.events || broadcast?.tx?.events || [];
}

function broadcastEventAttribute(event, key) {
  return (event?.attributes || []).find(attribute => attribute.key === key)?.value || "";
}

function evmFailureMessageFromBroadcast(broadcast, label = "transaction") {
  if (broadcast?.error) {
    return broadcast.error;
  }
  const evmFailure = broadcastTxEvents(broadcast)
    .filter(event => event.type === "ethereum_tx")
    .map(event => broadcastEventAttribute(event, "ethereumTxFailed"))
    .find(Boolean);
  if (evmFailure) {
    return `${label} failed: EVM execution reverted (${evmFailure})`;
  }
  if (broadcast?.receipt?.status && broadcast.receipt.status !== "0x1") {
    return `${label} failed with EVM receipt status ${broadcast.receipt.status}`;
  }
  return "";
}

function assertSuccessfulBroadcast(broadcast, label = "transaction") {
  const txHash = broadcast?.broadcast?.txhash || broadcast?.txHash || "";
  if (broadcast?.pending && txHash) {
    return;
  }
  const evmFailure = evmFailureMessageFromBroadcast(broadcast, label);
  if (evmFailure) {
    throw new Error(evmFailure);
  }
  if (broadcast?.receipt) {
    return;
  }
  if (!broadcast?.tx) {
    throw new Error(`${label} was broadcast but not found yet: ${txHash || "unknown tx"}`);
  }
  if (Number(broadcast.tx.code || 0) !== 0) {
    throw new Error(broadcast.tx.raw_log || `${label} failed with code ${broadcast.tx.code}`);
  }
}

async function broadcastPreparedPrivacy(data, label = "privacy transaction", options = {}) {
  const reservationBinding = preparedReservationBinding(data);
  const relayValidation = data.reservationKind === "withdraw"
    ? {
        relayPayload: data.payload,
        getChainNowUnix: () => fetchLatestChainBlockTimeUnix(),
        expectedChainId: activeChainProfile()?.chainId,
        expectedRecipient: data.reservationRecipient,
        accountPrefix: accountPrefix(),
        ...(state.activeWallet === "metamask" ? { expectedEvmChainId: expectedEvmChainIdHex() } : {})
      }
    : {};
  const broadcastOptions = { ...reservationBinding, ...relayValidation };
  try {
    const broadcast = await withPreparedReservationHeartbeat(data, () => (
      state.activeWallet === "metamask"
        ? sendEvmTransaction(data.transaction, {
            label,
            waitForReceipt: Boolean(options.waitForEvmReceipt),
            reservationBinding: broadcastOptions
          })
        : signDirectAndBroadcast(data.signDoc, broadcastOptions)
    ));
    await refreshReservationState(data.reservationManager);
    if (broadcast.unknown) {
      const error = new Error(`${label} was submitted, but its final result is unknown. Reconcile the tx hash and note nullifier before retrying.`);
      error.code = "TX_RESULT_UNKNOWN";
      error.txHash = broadcast.txHash || "";
      error.broadcast = broadcast;
      error.preparedPrivacyData = data;
      throw error;
    }
    assertSuccessfulBroadcast(broadcast, label);
    return { ...broadcast, preparedPrivacyData: data };
  } catch (error) {
    error.preparedPrivacyData ||= data;
    await refreshReservationState(data.reservationManager).catch(() => {});
    throw error;
  }
}

function evmReceiptHasFailed(receipt) {
  if (!receipt || receipt.status === undefined || receipt.status === null) return false;
  const status = typeof receipt.status === "string" ? receipt.status.toLowerCase() : receipt.status;
  return status === 0 || status === 0n || status === "0" || status === "0x0" || status === false;
}

function checkedReservationHeight(check = {}) {
  const candidates = [
    check.height,
    state.keplr.noteScanCursor?.latest_height,
    state.keplr.noteScanCursor?.latestHeight,
    els.blockHeight?.textContent
  ];
  for (const candidate of candidates) {
    try {
      const value = typeof candidate === "string" && /^0x[0-9a-f]+$/i.test(candidate)
        ? Number(BigInt(candidate))
        : Number(candidate);
      if (Number.isSafeInteger(value) && value > 0) return value;
    } catch {
      // Try the next authoritative height source.
    }
  }
  return 0;
}

async function checkReservationTransaction(txHash) {
  if (!txHash) return { checked: false, txHash: "" };
  if (activeChainProfile()?.transport === "evm") {
    const rpcTxHash = /^0x/i.test(txHash) ? txHash : `0x${txHash}`;
    const [receipt, transaction] = await Promise.all([
      clairveilBrowserClient().evmJsonRpc("eth_getTransactionReceipt", [rpcTxHash]),
      clairveilBrowserClient().evmJsonRpc("eth_getTransactionByHash", [rpcTxHash])
    ]);
    return {
      checked: true,
      txHash,
      included: Boolean(receipt),
      failed: evmReceiptHasFailed(receipt),
      absent: !receipt && !transaction,
      pending: !receipt && Boolean(transaction),
      height: receipt?.blockNumber || 0,
      transaction
    };
  }
  const tx = await clairveilBrowserClient().waitForTx(txHash, { attempts: 1, intervalMs: 1 });
  const rawCode = tx?.code;
  const code = typeof rawCode === "number" && Number.isSafeInteger(rawCode) && rawCode >= 0
    ? rawCode
    : typeof rawCode === "string" && /^(0|[1-9]\d*)$/.test(rawCode)
      ? Number(rawCode)
      : null;
  return {
    checked: true,
    txHash,
    included: Boolean(tx),
    failed: Boolean(tx) && code !== null && code !== 0,
    absent: !tx,
    pending: false,
    height: tx?.height || 0,
    transaction: tx
  };
}

function clearedRelayWithdrawState(resultStatus, resultMessage) {
  return {
    handoff: null,
    json: "",
    reservationIds: [],
    txHash: "",
    resultStatus,
    resultMessage
  };
}

async function recoverExpiredRelayWithdraw({ manager, records, chainBlock, check }) {
  const handoff = state.relayWithdraw.handoff;
  const payload = relayWithdrawHandoffPayload(handoff);
  if (!relayWithdrawPayloadExpired(payload, chainBlock.timeUnix)) return false;
  const unspentIDs = await explicitlyUnspentReservationIDs(manager, records);
  if (!records.length || unspentIDs.length !== records.length) {
    stopRelayReservationHeartbeat();
    state.relayWithdraw.resultStatus = "manual-review";
    state.relayWithdraw.resultMessage = `Payload expired at chain time ${chainBlock.timeUnix}, but every reserved nullifier is not confirmed unspent`;
    setWithdrawEvidence(
      "Spent or unspent evidence incomplete · manual review",
      "Payload expired · transparent receive not established",
      { render: false }
    );
    return true;
  }

  const approved = globalThis.confirm(
    `Relay payload가 chain height ${chainBlock.height}에서 만료되었고 모든 nullifier가 unspent로 확인되었습니다.\n\n` +
    "이 handoff를 종료하고 새 withdraw payload를 만들 수 있도록 reservation을 재계획할까요?"
  );
  if (!approved) {
    state.relayWithdraw.resultStatus = "expired-review";
    state.relayWithdraw.resultMessage = "Payload expired and nullifier unspent · owner approval required to replan";
    setWithdrawEvidence(
      `Unspent · confirmed at height ${chainBlock.height}`,
      "Payload expired · awaiting owner-approved replan",
      { render: false }
    );
    return true;
  }

  const statuses = new Set(records.map(record => record.status));
  const evidence = {
    relay_payload_expired: true,
    authoritative_expiry_confirmed: true,
    nullifier_unspent_confirmed: true,
    checked_height: chainBlock.height,
    checked_chain_time_unix: chainBlock.timeUnix,
    ...(check?.txHash ? { tx_hash_checked: check.txHash } : {})
  };
  if (statuses.size === 1 && statuses.has(reservationStatuses.ProofReady)) {
    const leaseTokens = [...new Set(records.map(record => record.lease_token).filter(Boolean))];
    if (leaseTokens.length !== 1) {
      throw new Error("Expired relay reservations do not share one recoverable lease token");
    }
    await manager.markManualReview(unspentIDs, {
      leaseToken: leaseTokens[0],
      error: "relay_payload_expired_with_unspent_nullifier",
      metadata: evidence
    });
  } else if (!(statuses.size === 1 && statuses.has(reservationStatuses.ManualReview))) {
    throw new Error(`Expired relay reservation has an unsupported recovery status: ${[...statuses].join(", ")}`);
  }

  stopRelayReservationHeartbeat();
  await manager.resolveManualReview(unspentIDs, {
    target: reservationStatuses.ReplanRequired,
    operatorId: state.keplr.account,
    approvalReference: `relay-expiry:${payload.payload_hash}:${chainBlock.height}`,
    reason: "Wallet owner approved replan after authoritative relay expiry and unspent reconciliation",
    metadata: evidence
  });
  await refreshReservationState(manager);
  state.relayWithdraw = clearedRelayWithdrawState(
    "expired-replanned",
    `Expired at chain height ${chainBlock.height} · nullifier unspent · new payload may be prepared`
  );
  setWithdrawEvidence(
    `Unspent · confirmed at height ${chainBlock.height}`,
    "Not received · expired payload closed",
    { render: false }
  );
  return true;
}

async function quarantineRelayWithdrawOperation({ manager, records = [], check = {}, error, reason }) {
  const message = error?.message || String(error || reason || "relay withdraw evidence conflict");
  const transitionable = records.filter(record => [
    reservationStatuses.ProofReady,
    reservationStatuses.Submitted,
    reservationStatuses.Unknown,
    reservationStatuses.ReplanRequired
  ].includes(record?.status));
  let reservationError = "";
  if (manager && transitionable.length) {
    const proofReady = transitionable.filter(record => record.status === reservationStatuses.ProofReady);
    const leaseTokens = [...new Set(proofReady.map(record => record.lease_token).filter(Boolean))];
    try {
      if (!proofReady.length || leaseTokens.length === 1) {
        await manager.markManualReview(transitionable.map(record => record.reservation_id), {
          ...(leaseTokens[0] ? { leaseToken: leaseTokens[0] } : {}),
          error: reason,
          metadata: {
            relay_operation_status: operationStatuses.ManualReview,
            relay_reconcile_reason: reason,
            relay_reconcile_error: message,
            ...(check.txHash ? { tx_hash_checked: check.txHash } : {}),
            ...(check.height ? { checked_height: check.height } : {})
          }
        });
      } else {
        reservationError = "relay reservations do not share one reviewable lease token";
      }
    } catch (transitionError) {
      reservationError = transitionError.message;
    }
  }
  stopRelayReservationHeartbeat();
  state.relayWithdraw.resultStatus = "manual-review";
  state.relayWithdraw.resultMessage = `Manual review required · ${message}${reservationError ? ` · ${reservationError}` : ""}`;
  setWithdrawEvidence(
    "Spent or binding evidence conflicts · manual review",
    "Transparent receive is not safely attributable to this handoff",
    { render: false }
  );
}

async function reconcileRelayWithdrawResult() {
  const handoff = state.relayWithdraw.handoff;
  const payload = relayWithdrawHandoffPayload(handoff);
  const txHash = state.relayWithdraw.txHash.trim();
  if (!handoff) throw new Error("Prepare and hand off a relay withdraw payload first");
  if (txHash && !/^(0x)?[0-9a-fA-F]{64}$/.test(txHash)) {
    throw new Error("Relayer tx hash must be a 32-byte hex value");
  }
  state.relayWithdraw.resultStatus = "checking";
  state.relayWithdraw.resultMessage = "Checking tx result first, then nullifier spent state…";
  await persistRelayWithdrawRecovery();
  renderRelayWithdraw();
  try {
    const check = txHash
      ? await checkReservationTransaction(txHash)
      : { checked: false, txHash: "", included: false, failed: false, absent: false, pending: false };
    const manager = await currentReservationManager();
    let records = manager
      ? await Promise.all(state.relayWithdraw.reservationIds.map(id => manager.getReservation(id)))
      : [];
    try {
      assertRelayReservationPayloadMatches(records, payload);
      if (check.included) {
        assertRelayWithdrawTransactionMatches({
          transport: handoff.transport,
          payload,
          handoffTransaction: handoff.transaction,
          transaction: check.transaction,
          expectedEvmChainId: activeChainProfile()?.evmChainId
        });
      }
    } catch (error) {
      await quarantineRelayWithdrawOperation({
        manager,
        records,
        check,
        error,
        reason: "relay_transaction_binding_conflict"
      });
      return;
    }

    await refreshEvents({ allowFailure: true });
    await scanKeplrNotes({ quiet: true, throwOnError: true });
    records = manager
      ? await Promise.all(state.relayWithdraw.reservationIds.map(id => manager.getReservation(id)))
      : [];
    const spentConfirmed = records.length > 0
      && records.every(record => record.status === reservationStatuses.ConfirmedSpent);
    const txBound = check.included;
    const receiveConfirmed = check.included && !check.failed && txBound && records.length > 0
      && records.every(record => !reservationRequiresOperationEvidence(record)
        || operationReconciliationStatus(record) === operationStatuses.Succeeded);

    if (!check.included || check.failed) {
      const chainBlock = await fetchLatestChainBlock();
      if (await recoverExpiredRelayWithdraw({ manager, records, chainBlock, check })) return;
    }

    if (spentConfirmed && (!check.included || check.failed)) {
      await quarantineRelayWithdrawOperation({
        manager,
        records,
        check,
        error: new Error(check.failed
          ? "A failed relayer transaction cannot explain the spent nullifier"
          : "The spent nullifier is not attributable to an included relayer transaction"),
        reason: "relay_spent_without_successful_bound_transaction"
      });
      return;
    }

    if (check.failed) {
      state.relayWithdraw.resultStatus = "failed";
      state.relayWithdraw.resultMessage = "Tx failed · nullifier not confirmed spent · reservation remains locked for review";
      setWithdrawEvidence(
        "Unspent not confirmed · retry blocked",
        "Not received · tx failed",
        { render: false }
      );
      return;
    }
    if (!check.included) {
      state.relayWithdraw.resultStatus = check.pending ? "submitted" : txHash ? "unknown" : "waiting";
      state.relayWithdraw.resultMessage = check.pending
        ? "Tx is pending · do not rebuild or hand off another payload"
        : txHash
          ? "Tx hash is not confirmed yet · absence is not treated as failure"
          : "No relayer tx hash yet · payload remains active until authoritative expiry";
      setWithdrawEvidence(
        check.pending ? "Submitted · not reconciled" : txHash ? "Unknown · reconcile before retry" : "Reserved · awaiting relayer result",
        check.pending ? "Pending transaction inclusion" : txHash ? "Unknown · reconcile before retry" : "Awaiting relayer submission",
        { render: false }
      );
      return;
    }

    state.keplr.withdrawHash = txHash;
    state.keplr.withdrawHeight = check.height || "included";
    const fullyConfirmed = spentConfirmed && receiveConfirmed;
    state.relayWithdraw.resultStatus = fullyConfirmed ? "confirmed" : "recovering";
    state.relayWithdraw.resultMessage = fullyConfirmed
      ? "Tx included · bound transparent recipient confirmed · input nullifier spent"
      : "Tx included · waiting for nullifier and bound transparent output reconciliation";
    if (fullyConfirmed) {
      confirmWithdrawEvidence({ render: false });
      stopRelayReservationHeartbeat();
    } else {
      setWithdrawEvidence(
        spentConfirmed ? "Spent · confirmed" : "Checking spent state",
        receiveConfirmed ? "Received · bound output confirmed" : "Checking bound transparent output",
        { render: false }
      );
    }
    await Promise.allSettled([refreshWalletBalance(), refreshProtocolStatus()]);
  } catch (error) {
    state.relayWithdraw.resultStatus = "unknown";
    state.relayWithdraw.resultMessage = `Unable to confirm result · ${error.message}`;
    setWithdrawEvidence(
      "Unknown · reconciliation failed",
      "Unknown · reconciliation failed",
      { render: false }
    );
  } finally {
    const store = await currentOperationStore().catch(() => null);
    if (!state.relayWithdraw.handoff || state.relayWithdraw.resultStatus === "confirmed") {
      store?.clear();
    } else {
      await persistRelayWithdrawRecovery().catch(error => {
        state.relayWithdraw.resultStatus = "manual-review";
        state.relayWithdraw.resultMessage = error.message;
      });
    }
    await refreshReservationState().catch(() => {});
    renderKeplr();
  }
}

async function explicitlyUnspentReservationIDs(manager, records, notes = state.keplr.notes) {
  const byLookupKey = new Map();
  for (const note of notes || []) {
    if (!noteHasUnspentEvidence(note)) continue;
    const lookupKey = await manager.lookupKeyForNote(note);
    byLookupKey.set(lookupKey, note);
  }
  return records
    .filter(record => byLookupKey.has(record.nullifier_lookup_key))
    .map(record => record.reservation_id);
}

function activeReservationOperation(records, operationKey) {
  return groupReservationOperations(records)
    .find(operation => operation.key === operationKey)?.records || [];
}

async function resolvePreparationRecovery(manager, assessment, evidence, approvalReference) {
  const reservationIDs = assessment.reservationIDs;
  const status = assessment.status;
  if (status === reservationStatuses.Reserved) {
    await manager.releaseReservedOrProving(reservationIDs);
    return "Released unused reservation";
  }
  if (status === reservationStatuses.Proving && assessment.leaseLive) {
    await manager.releaseReservedOrProving(reservationIDs, { leaseToken: assessment.leaseToken });
    return "Released local proving reservation";
  }
  if (status === reservationStatuses.ProofReady && assessment.leaseLive) {
    await manager.markReplanRequired(reservationIDs, {
      fromStatus: reservationStatuses.ProofReady,
      leaseToken: assessment.leaseToken,
      proofDiscarded: true,
      error: "wallet_owner_discarded_unsubmitted_proof",
      metadata: evidence
    });
    return "Discarded unsubmitted proof and enabled replanning";
  }

  if ([reservationStatuses.Proving, reservationStatuses.ProofReady].includes(status)) {
    await manager.markManualReview(reservationIDs, {
      error: "expired_preparation_recovery",
      metadata: evidence
    });
  } else if (status !== reservationStatuses.ManualReview) {
    throw new Error(`Reservation status ${status} cannot enter preparation recovery`);
  }

  await manager.resolveManualReview(reservationIDs, {
    target: reservationStatuses.ReplanRequired,
    operatorId: state.keplr.account,
    approvalReference,
    reason: "Wallet owner approved replan after no-broadcast and unspent evidence review",
    metadata: evidence
  });
  return "Approved manual review and enabled replanning";
}

async function recoverReservationPreparation(operationKey) {
  const manager = await currentReservationManager();
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  if (state.reservations.recoveringOperationKey) return;
  state.reservations.recoveringOperationKey = operationKey;
  renderReservationState();
  try {
    let records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
    let assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
    if (assessment.action !== "review-replan") throw new Error(assessment.reason);

    els.keplrTxState.textContent = "Checking reservation recovery evidence";
    await refreshEvents({ allowFailure: true });
    await scanKeplrNotes({ quiet: true, throwOnError: true, skipSetup: true, maxPages: 1000 });
    records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
    if (!records.length) {
      await refreshReservationState(manager);
      toast("Reservation was already reconciled by the latest note scan.");
      return;
    }
    assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
    if (assessment.action !== "review-replan") throw new Error(assessment.reason);

    const unspentIDs = await explicitlyUnspentReservationIDs(manager, records, state.keplr.notes);
    const checkedHeight = checkedReservationHeight();
    if (unspentIDs.length !== assessment.reservationIDs.length || !checkedHeight) {
      throw new Error("Every reserved nullifier must be explicitly unspent at an authoritative scanned height before replanning");
    }
    const operationLabel = `${reservationKindLabel(assessment.kind)} ${shorten(operationKey, 12, 10)}`;
    const approved = globalThis.confirm(
      `${operationLabel}의 broadcast 시도 기록이 없고 ${assessment.reservationIDs.length}개 nullifier가 height ${checkedHeight}에서 unspent로 확인되었습니다.\n\n`
      + "저장되지 않은 local proof를 폐기하고 이 note를 새 transaction 계획에 다시 사용할까요? 이 작업은 기존 proof를 다시 보낼 수 없게 만드는 명시적 recovery 승인입니다."
    );
    if (!approved) {
      els.keplrTxState.textContent = "Reservation recovery cancelled";
      return;
    }

    const approvalReference = `direct-recovery:${operationKey}:${checkedHeight}:${Date.now()}`;
    const evidence = {
      reconcile_reason: "wallet_owner_approved_unsubmitted_preparation_replan",
      no_broadcast_attempt: true,
      proof_discarded: true,
      nullifier_unspent_confirmed: true,
      checked_height: checkedHeight,
      wallet_owner_approved_replan: true,
      recovery_approval_reference: approvalReference
    };
    const result = await resolvePreparationRecovery(manager, assessment, evidence, approvalReference);
    els.keplrTxState.textContent = "Reservation recovery complete";
    await refreshReservationState(manager);
    toast(`${result}. A new plan may now use the released notes.`);
  } finally {
    state.reservations.recoveringOperationKey = "";
    await refreshReservationState(manager).catch(() => {});
    renderReservationState();
  }
}

async function reconcileReservations({ quiet = false, manager = null } = {}) {
  const resolvedManager = manager || await currentReservationManager();
  if (!resolvedManager) throw new Error("Encrypted note reservation manager is not available");
  state.reservations.reconciling = true;
  renderReservationState();
  try {
    const initial = await resolvedManager.listActiveReservations();
    const txHashes = [...new Set(initial.map(record => record.submitted_tx_hash).filter(Boolean))];
    const txChecks = new Map();
    for (const txHash of txHashes) {
      txChecks.set(txHash, await checkReservationTransaction(txHash));
    }

    await refreshEvents({ allowFailure: true });
    await scanKeplrNotes({ quiet: true, throwOnError: true });
    const active = await resolvedManager.listActiveReservations();
    for (const status of [reservationStatuses.Submitted, reservationStatuses.Unknown]) {
      const recordsByTx = new Map();
      for (const record of active.filter(item => item.status === status && item.submitted_tx_hash)) {
        const grouped = recordsByTx.get(record.submitted_tx_hash) || [];
        grouped.push(record);
        recordsByTx.set(record.submitted_tx_hash, grouped);
      }
      for (const [txHash, records] of recordsByTx) {
        const check = txChecks.get(txHash);
        if (!check || (!check.failed && !check.absent)) continue;
        const unspentIDs = await explicitlyUnspentReservationIDs(resolvedManager, records);
        const checkedHeight = checkedReservationHeight(check);
        if (unspentIDs.length !== records.length || !checkedHeight) continue;
        await resolvedManager.markReplanRequired(unspentIDs, {
          fromStatus: status,
          txHash,
          nullifierUnspentConfirmed: true,
          txAbsentOrFailedConfirmed: true,
          checkedHeight,
          txHashChecked: txHash,
          error: check.failed ? "checked_transaction_failed" : "checked_transaction_absent",
          metadata: { reconcile_source: "clairveil_example_dapp" }
        });
      }
    }
    const remaining = await refreshReservationState(resolvedManager);
    if (!quiet) {
      toast(remaining.length
        ? "Reservation reconciliation is incomplete. Do not retry while it remains active."
        : "Note reservations reconciled. A new plan may now be prepared.");
    }
    return remaining;
  } finally {
    state.reservations.reconciling = false;
    renderReservationState();
  }
}

async function resolvePreparedPrivacyFailure(error, data = error?.preparedPrivacyData) {
  try {
    const manager = data?.reservationManager || await currentReservationManager();
    if (!manager) return { blocked: false, active: [] };
    if (error?.txHash || error?.broadcast || error?.txhash) {
      await reconcileReservations({ quiet: true, manager }).catch(() => {});
    }
    const active = await refreshReservationState(manager);
    const reservationIDs = new Set(preparedReservationIDs(data));
    const operationActive = reservationIDs.size
      ? active.filter(record => reservationIDs.has(record.reservation_id))
      : active;
    return {
      blocked: operationActive.length > 0
        || error?.code === "OPERATION_RECONCILIATION_REQUIRED"
        || state.reservations.unresolved?.length > 0,
      active: operationActive
    };
  } catch (reservationError) {
    return {
      blocked: true,
      active: state.reservations.active,
      reservationError
    };
  }
}

async function activePreparedReservations(data) {
  const ids = new Set(preparedReservationIDs(data));
  if (!ids.size || !data?.reservationManager) return [];
  const active = await refreshReservationState(data.reservationManager);
  return active.filter(record => ids.has(record.reservation_id));
}

async function requirePreparedReservationReconciled(data, label) {
  const active = await activePreparedReservations(data);
  if (active.length) {
    const error = new Error(`${label} was included, but its note nullifier and operation evidence have not been reconciled yet. Do not retry or continue the plan.`);
    error.code = "NULLIFIER_RECONCILIATION_PENDING";
    error.preparedPrivacyData = data;
    throw error;
  }
  const records = await Promise.all(
    preparedReservationIDs(data).map(id => data.reservationManager.getReservation(id))
  );
  const unresolved = records.filter(record => reservationRequiresOperationEvidence(record)
    && operationReconciliationStatus(record) !== operationStatuses.Succeeded);
  if (!unresolved.length) return;
  await refreshReservationState(data.reservationManager);
  const error = new Error(`${label} consumed its input note, but the tx output evidence does not prove the intended recipient and amount. Manual review is required.`);
  error.code = "OPERATION_RECONCILIATION_REQUIRED";
  error.preparedPrivacyData = data;
  throw error;
}

async function broadcastVeiledTransfer(amount, recipient, label = "veiled transfer", disclosure = {}, options = {}) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyTransferSignDoc(amount, recipient, disclosure, options);
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, options);
  state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
  return { ...broadcast, prepared: data.prepared };
}

function isExactMatchWithdrawError(error) {
  return error?.code === "EXACT_NOTE_REQUIRED" || error?.status === "exact_note_required";
}

function isZeroHelperNeededError(error) {
  return error?.code === "ZERO_DUMMY_REQUIRED" || error?.status === "zero_dummy_required";
}

function isSelfTransferRecipient(recipient) {
  return Boolean(state.keplr.shieldedAddress && recipient === state.keplr.shieldedAddress);
}

async function createExactWithdrawNote(amount, hooks = {}, options = {}) {
  if (!state.keplr.shieldedAddress) {
    throw new Error("Clairveil shielded address is not ready");
  }

  const maxPlannerSteps = 20;
  for (let step = 1; step <= maxPlannerSteps; step += 1) {
    els.keplrTxState.textContent = "Preparing exact note";
    hooks.onPlanCheck?.(step, maxPlannerSteps);

    let data;
    try {
      data = await preparePrivacyTransferSignDoc(amount, state.keplr.shieldedAddress, {}, {
        ...options,
        allowPlanStep: true
      });
    } catch (error) {
      if (!isZeroHelperNeededError(error)) {
        throw error;
      }
      hooks.onZeroHelperNeeded?.(error, step, maxPlannerSteps);
      await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", {
        waitForEvmReceipt: true,
        signal: options.signal
      });
      await refreshPrivacySurfaces();
      continue;
    }

    if (data.prepared?.isFinal === false || data.prepared?.planAction === "self_merge") {
      hooks.onSelfMergeNeeded?.(data, step, maxPlannerSteps);
      els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
      const plannerBroadcast = await broadcastPreparedPrivacy(data, "exact-note self transaction", { waitForEvmReceipt: true });
      state.keplr.transferHash = plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
      await refreshPrivacySurfaces();
      await requirePreparedReservationReconciled(data, "Exact-note self transaction");
      continue;
    }

    hooks.onFinalExactTransfer?.(data, step, maxPlannerSteps);
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "exact-note self transfer", { waitForEvmReceipt: true });
    state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    await refreshPrivacySurfaces();
    await requirePreparedReservationReconciled(data, "Exact-note self transfer");
    return data;
  }

  throw new Error("Withdraw에 필요한 exact note 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.");
}

async function sendFromKeplr() {
  if (!state.keplr.account) return;
  setBusy(els.sendFromKeplr, true);
  els.keplrTxState.textContent = "Preparing send";
  try {
    const recipient = requireValidSendRecipient();
    if (state.activeWallet === "metamask") {
      const transaction = clairveilBrowserClient().evmNativeSendTransaction({
        to: recipient,
        amount: amountInputValue(els.keplrSendAmount)
      });
      els.keplrTxState.textContent = "Waiting for MetaMask";
      const broadcast = await sendEvmTransaction(transaction, { label: "EVM send" });
      assertSuccessfulBroadcast(broadcast, "EVM send");
      state.keplr.sendHash = broadcast.txHash || "";
      state.keplr.sendStatus = "submitted";
      els.keplrTxState.textContent = "Send submitted";
      renderKeplr();
      showSendResult({
        success: true,
        wallet: "MetaMask",
        txHash: state.keplr.sendHash
      });
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.sendHash = included.txHash || state.keplr.sendHash;
          state.keplr.sendStatus = "included";
          els.keplrTxState.textContent = "Send included";
          await Promise.allSettled([refreshWalletBalance(), refreshBlockEvents()]);
          renderKeplr();
        },
        onUnknown: unknown => {
          state.keplr.sendHash = unknown.txHash || state.keplr.sendHash;
          state.keplr.sendStatus = "unknown";
          els.keplrTxState.textContent = "Send status unknown";
          showNotice({
            title: "Send 결과 확인 필요",
            message: `Receipt polling이 끝났지만 실패가 확인된 것은 아닙니다. 같은 전송을 다시 보내기 전에 tx hash를 확인하세요.\nTx: ${shorten(state.keplr.sendHash, 14, 12)}`
          });
          renderKeplr();
        },
        onFailed: error => {
          state.keplr.sendStatus = evmReceiptHasFailed(error?.broadcast?.receipt) ? "failed" : "unknown";
          const failed = state.keplr.sendStatus === "failed";
          els.keplrTxState.textContent = failed ? "Send failed" : "Send status unknown";
          showNotice({
            title: failed ? "Send 실패" : "Send 결과 확인 필요",
            message: failed ? error.message : `${error.message}\n실패가 확인되지 않아 같은 요청은 계속 차단됩니다.`,
            failed
          });
          renderKeplr();
        }
      });
      return;
    }

    const signDoc = await clairveilBrowserClient().buildBankSendSignDoc({
      from: state.keplr.account,
      pubKeyHex: state.keplr.pubkeyHex,
      to: recipient,
      amount: amountInputValue(els.keplrSendAmount)
    });
    els.keplrTxState.textContent = "Waiting for Keplr";
    const broadcast = await signDirectAndBroadcast(signDoc);
    state.keplr.sendHash = broadcast.broadcast?.txhash || "";
    state.keplr.sendStatus = "included";
    els.keplrTxState.textContent = "Send included";
    renderKeplr();
    showSendResult({
      success: true,
      wallet: "Keplr",
      txHash: state.keplr.sendHash
    });
    await Promise.allSettled([refreshWalletBalance(), refreshBlockEvents()]);
    renderKeplr();
  } catch (error) {
    if (!state.keplr.sendHash) state.keplr.sendStatus = "failed";
    els.keplrTxState.textContent = "Send failed";
    showSendResult({
      success: false,
      error: error.message
    });
  } finally {
    setBusy(els.sendFromKeplr, false);
    renderKeplr();
  }
}

async function depositFromKeplr() {
  if (!state.keplr.account) return;
  await setupKeplrPrivacy();
  if (!state.keplr.rootSignatureBase64) return;

  setBusy(els.depositFromKeplr, true);
  els.keplrTxState.textContent = "Preparing deposit";
  try {
    const amount = amountInputValue(els.keplrDepositAmount);
    const broadcast = await broadcastPrivacyDeposit(amount);
    state.keplr.depositPrepared = broadcast.prepared || null;
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm ? "Deposit submitted" : "Deposit included";
    state.keplr.depositRecoveryStatus = isPendingEvm ? "submitted" : "recovering";
    state.keplr.depositRecoveryMessage = isPendingEvm
      ? "Submitted · waiting for inclusion"
      : "Included · recovering encrypted note";
    renderKeplr();
    if (isPendingEvm) {
      showNotice({
        title: "Deposit 제출됨",
        message: `트랜잭션은 제출되었고 아직 note 복구가 완료되지 않았습니다.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
      });
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.depositHash = included.txHash || state.keplr.depositHash;
          state.keplr.depositHeight = included.receipt?.blockNumber || state.keplr.depositHeight;
          updateIncludedDepositNetworkFee(included);
          els.keplrTxState.textContent = "Deposit included";
          const recovered = await recoverDepositNote({ ...broadcast, ...included, prepared: broadcast.prepared });
          await refreshPrivacySurfaces({ balance: true });
          renderKeplr();
          showNotice({
            title: recovered ? "Deposit 및 note 복구 완료" : "Deposit 포함 · note 복구 대기",
            message: recovered
              ? `Encrypted note가 로컬 cache에 복구되었습니다.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
              : "트랜잭션은 성공했지만 note 복구가 아직 완료되지 않았습니다. Reset & Rescan으로 다시 복구할 수 있습니다."
          });
        },
        onUnknown: unknown => {
          state.keplr.depositHash = unknown.txHash || state.keplr.depositHash;
          state.keplr.depositRecoveryStatus = "unknown";
          state.keplr.depositRecoveryMessage = "Submitted · receipt unknown · do not retry yet";
          els.keplrTxState.textContent = "Deposit status unknown";
          renderKeplr();
          showNotice({
            title: "Deposit 결과 확인 필요",
            message: `Receipt polling timeout은 실패 증거가 아닙니다. tx hash를 확인하기 전 같은 deposit을 다시 보내지 마세요.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
          });
        },
        onFailed: error => {
          state.keplr.depositRecoveryStatus = evmReceiptHasFailed(error?.broadcast?.receipt) ? "failed" : "unknown";
          state.keplr.depositRecoveryMessage = state.keplr.depositRecoveryStatus === "failed"
            ? `Failed on-chain · ${error.message}`
            : `Result still unknown · ${error.message}`;
          const failed = state.keplr.depositRecoveryStatus === "failed";
          els.keplrTxState.textContent = failed ? "Deposit failed" : "Deposit status unknown";
          showNotice({
            title: failed ? "Deposit 실패" : "Deposit 결과 확인 필요",
            message: failed ? error.message : `${error.message}\n실패가 확인되지 않아 같은 요청은 계속 차단됩니다.`,
            failed
          });
          renderKeplr();
        }
      });
      return;
    }
    const recovered = await recoverDepositNote(broadcast);
    await refreshPrivacySurfaces({ balance: true });
    showNotice({
      title: recovered ? "Deposit 및 note 복구 완료" : "Deposit 포함 · note 복구 대기",
      message: recovered
        ? `${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit과 encrypted note 복구가 완료되었습니다.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
        : "트랜잭션은 성공했지만 note 복구가 아직 완료되지 않았습니다. Reset & Rescan으로 다시 복구할 수 있습니다."
    });
  } catch (error) {
    if (!state.keplr.depositHash) {
      state.keplr.depositRecoveryStatus = "failed";
      state.keplr.depositRecoveryMessage = error.message;
    }
    els.keplrTxState.textContent = "Deposit failed";
    showNotice({ title: "Deposit 실패", message: error.message, failed: true });
  } finally {
    setBusy(els.depositFromKeplr, false);
    renderKeplr();
  }
}

async function scanKeplrNotes(options = {}) {
  if (!state.keplr.account) return;
  if (!options.skipSetup) {
    await setupKeplrPrivacy({ skipInitialSync: true });
  }
  if (!state.keplr.rootSignatureBase64) return;

  setBusy(els.scanKeplrNotes, true);
  if (!options.quiet) {
    els.keplrTxState.textContent = "Scanning notes";
  }
  try {
    const reset = Boolean(options.reset);
    if (reset) {
      await clearCurrentNoteStore();
    }
    state.keplr.noteSyncStatus = "scanning";
    state.keplr.noteSyncMessage = reset ? "Full rescan in progress" : "Incremental scan in progress";
    els.noteSyncState.textContent = state.keplr.noteSyncMessage;
    els.noteSyncState.dataset.status = state.keplr.noteSyncStatus;
    const scanOptions = noteScanRequestOptions({ reset, maxPages: options.maxPages ?? 5 });
    const store = await currentNoteStore();
    const data = await clairveilBrowserClient().scanWalletNotes(privacyRequest({
      ...scanOptions,
      noteStore: store,
      includeFoundNotes: true
    }));
    await applyNoteScanResult(data, { reset });
    if (!options.quiet) {
      const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
      els.keplrTxState.textContent = "Ready";
      const hasMore = Boolean(cursor.has_more ?? cursor.hasMore);
      const pagesScanned = Number(cursor.pages_scanned ?? cursor.pagesScanned ?? 1);
      toast(hasMore
        ? `Keplr notes scanned (${pagesScanned} pages, more queued)`
        : "Keplr notes scanned");
    }
    renderKeplr();
    return data;
  } catch (error) {
    state.keplr.noteSyncStatus = error?.code === "NOTE_CACHE_CORRUPT" ? "corrupt" : "failed";
    state.keplr.noteSyncMessage = error.message;
    if (!options.quiet) {
      els.keplrTxState.textContent = "Scan failed";
      toast(error.message);
    }
    if (options.throwOnError) throw error;
  } finally {
    setBusy(els.scanKeplrNotes, false);
    renderKeplr();
  }
}

function downloadTextFile(filename, content, type = "application/json") {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.append(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

async function backupNoteCache() {
  const keys = noteStoreKeys();
  const raw = keys ? globalThis.localStorage?.getItem(keys.encrypted) : "";
  if (!raw) throw new Error("Encrypted note cache is empty");
  downloadTextFile(`clairveil-note-cache-${Date.now()}.json`, raw);
  toast("Encrypted note cache backup downloaded");
}

async function resetAndRescanNotes() {
  if (!globalThis.confirm("로컬 note cache를 지우고 체인 genesis부터 다시 스캔할까요? cache는 복구 가능하지만 완료까지 시간이 걸릴 수 있습니다.")) return;
  await scanKeplrNotes({ reset: true, throwOnError: true });
}

async function rollbackAndRescanNotes() {
  const height = String(els.noteRollbackHeight.value || "").trim();
  if (!/^(0|[1-9][0-9]*)$/.test(height)) {
    throw new Error("Rollback height must be a canonical non-negative integer");
  }
  const store = await currentNoteStore();
  if (!store || typeof store.rollbackToHeight !== "function") {
    throw new Error("Encrypted note store does not support cursor rollback");
  }
  const current = await store.load();
  if (BigInt(height) > BigInt(current.lastScannedHeight || 0)) {
    throw new Error(`Rollback height cannot exceed the last scanned height ${current.lastScannedHeight || 0}`);
  }
  if (!globalThis.confirm(`Height ${height}부터 note cache를 되감고 다시 스캔할까요? 필요하면 먼저 Backup cache를 실행하세요.`)) return;

  const rolledBack = await store.rollbackToHeight(height);
  state.keplr.notes = rolledBack.notes || [];
  state.keplr.noteScanCursor = rolledBack.scanCursor || defaultNoteScanCursor();
  state.keplr.noteScanResumeOptions = null;
  state.keplr.noteSyncStatus = "rollback-ready";
  state.keplr.noteSyncMessage = `Cursor rolled back to height ${height} · rescan required`;
  renderKeplr();
  await scanKeplrNotes({
    quiet: false,
    throwOnError: true,
    skipSetup: true,
    maxPages: 1000
  });
}

async function refreshPrivacySurfaces({ balance = false } = {}) {
  const tasks = [
    refreshEvents(),
    refreshAuditorTransfers(),
    scanKeplrNotes({ quiet: true }),
    refreshNotes(),
    refreshProtocolStatus()
  ];
  if (balance) {
    tasks.unshift(refreshWalletBalance());
  }
  await Promise.allSettled(tasks);
  const manager = await currentReservationManager();
  await reconcileSpentReservations(manager, state.keplr.notes);
  await refreshReservationState(manager);
}

async function transferFromVeiled() {
  if (!state.keplr.account) return;
  await setupKeplrPrivacy();
  if (!state.keplr.rootSignatureBase64) return;

  const amount = amountInputValue(els.veiledTransferAmount);
  const recipient = els.veiledTransferRecipient.value.trim();
  if (!recipient) {
    toast(`Enter the recipient's ${shieldedPrefix()} address in Transfer recipient.`);
    return;
  }
  if (isSelfTransferRecipient(recipient)) {
    toast("이 주소는 내 shielded address야. 여기로 보내면 외부 전송이 아니라 note split/change self-transfer가 돼.");
    return;
  }
  let disclosure;
  try {
    disclosure = transferDisclosurePolicy();
  } catch (error) {
    toast(error.message);
    return;
  }
  let timing;
  try {
    timing = await privacyOperationTiming();
  } catch (error) {
    toast(`최종 확인을 위한 체인 시간을 불러오지 못했습니다: ${error.message}`);
    return;
  }
  const confirmed = await openTransferFlowModal("transfer", {
    chainId: activeChainProfile()?.chainId,
    recipient,
    amount: coinText(amount),
    disclosure: transferDisclosureSummary(disclosure),
    selfView: disclosure.disableSelfViewDisclosure ? "Disabled (recovery limited)" : "Encrypted self-view included",
    changeEffect: "Pending payload preparation",
    expiresAtUnix: timing.expiresAtUnix
  });
  if (!confirmed) return;

  setBusy(els.transferFromVeiled, true);
  els.keplrTxState.textContent = "Preparing veiled transfer";
  try {
    const operationOptions = { ...timing, signal: activeProofSignal() };
    const maxPlannerSteps = 20;
    let finalData = null;

    for (let step = 1; step <= maxPlannerSteps; step += 1) {
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        step === 1 ? "노트 확인 중" : "노트 재확인 중",
        "요청 금액을 보낼 수 있는 note 조합이 있는지 확인합니다."
      );

      let data;
      try {
        data = await preparePrivacyTransferSignDoc(amount, recipient, disclosure, {
          ...operationOptions,
          allowPlanStep: true
        });
      } catch (error) {
        if (!isZeroHelperNeededError(error)) {
          throw error;
        }
        showTransferPlannerFacts({
          requested: amount,
          action: `${zeroCoinText()} helper note를 만들어 다음 self transaction에 사용합니다.`
        });
        updateTransferFlow(
          "zero",
          "Self transaction 서명 대기",
          "요청 금액을 만들기 위해 note 정리가 필요합니다. 이 단계는 내 Veiled balance 안에서 note를 재구성하며, 받는 사람에게는 아직 전송되지 않습니다."
        );
        await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", {
          waitForEvmReceipt: true,
          signal: operationOptions.signal
        });
        await refreshPrivacySurfaces();
        continue;
      }

      if (data.prepared?.isFinal === false || data.prepared?.planAction === "self_merge") {
        showTransferPlannerFacts({
          requested: amount,
          currentMax: plannerCurrentTransferMaxForNoteMerge(data, amount),
          action: `두 note를 합쳐 ${data.prepared?.amount || "새 note"} note를 만듭니다.`
        });
        updateTransferFlow(
          "zero",
          "Self transaction 서명 대기",
          "요청 금액을 만들기 위해 note 정리가 필요합니다. 이 단계는 내 Veiled balance 안에서 note를 재구성하며, 받는 사람에게는 아직 전송되지 않습니다."
        );
        els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
        const plannerBroadcast = await broadcastPreparedPrivacy(data, "self transaction", { waitForEvmReceipt: true });
        state.keplr.transferHash = plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
        await refreshPrivacySurfaces();
        await requirePreparedReservationReconciled(data, "Self transaction");
        continue;
      }

      finalData = data;
      break;
    }

    if (!finalData) {
      throw new Error("입력하신 금액의 노트 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.");
    }

    const finalConfirmed = await withPreparedReservationHeartbeat(finalData, () => (
      requestPreparedTransferConfirmation({
        ...transferFlowState.review,
        recipient: finalData.prepared?.finalRecipient || recipient,
        amount: coinText(finalData.prepared?.finalAmount || amount),
        changeEffect: preparedTransferChangeEffect(finalData),
        expiresAtUnix: timing.expiresAtUnix
      })
    ));
    if (!finalConfirmed) {
      await discardPreparedReservation(finalData);
      return;
    }

    resetTransferPlannerFacts();
    updateTransferFlow(
      "transfer",
      "트랜스퍼 서명 대기",
      `note 준비가 완료되었습니다. 이제 받는 사람에게 privacy transfer를 요청합니다. ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 최종 전송 내용을 확인하고 서명해 주세요.`
    );
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(finalData, "privacy transfer");
    state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm ? "Transfer submitted" : "Transfer included";
    renderKeplr();
    if (isPendingEvm) {
      finishTransferFlow("트랜스퍼 요청이 제출되었습니다");
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.transferHash = included.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer included";
          await refreshPrivacySurfaces();
          await requirePreparedReservationReconciled(finalData, "Privacy transfer");
          finishTransferFlow("트랜스퍼 요청이 성공하였습니다");
          renderKeplr();
        },
        onUnknown: async unknown => {
          state.keplr.transferHash = unknown.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer status unknown";
          await refreshReservationState(finalData.reservationManager).catch(() => {});
          finishTransferFlowUnknown(`Receipt polling이 끝났지만 실패가 확인되지 않았습니다. tx hash와 nullifier를 reconcile하기 전에는 다시 전송하지 마세요.\nTx: ${state.keplr.transferHash}`);
          renderKeplr();
        },
        onFailed: async error => {
          const resolution = await resolvePreparedPrivacyFailure(error, finalData);
          els.keplrTxState.textContent = resolution.blocked ? "Transfer reconciliation required" : "Transfer failed";
          if (resolution.blocked) {
            finishTransferFlowUnknown(error.message);
          } else {
            finishTransferFlow(error.message, false, { retry: () => transferFromVeiled() });
          }
          renderKeplr();
        }
      });
      return;
    }
    await refreshPrivacySurfaces();
    await requirePreparedReservationReconciled(finalData, "Privacy transfer");
    finishTransferFlow("트랜스퍼 요청이 성공하였습니다");
  } catch (error) {
    const cancelled = error?.name === "AbortError" || activeProofSignal()?.aborted;
    const resolution = await resolvePreparedPrivacyFailure(error);
    if (resolution.blocked) {
      els.keplrTxState.textContent = "Transfer reconciliation required";
      finishTransferFlowUnknown(error.message);
    } else {
      els.keplrTxState.textContent = cancelled ? "Transfer cancelled" : "Transfer failed";
      finishTransferFlow(cancelled ? "Proof 요청을 취소했습니다." : error.message, false, {
        retry: () => transferFromVeiled()
      });
    }
  } finally {
    setBusy(els.transferFromVeiled, false);
    renderKeplr();
  }
}

async function withdrawFromVeiled() {
  if (!state.keplr.account) return;
  let amount;
  try {
    amount = amountInputValue(els.veiledWithdrawAmount);
  } catch (error) {
    toast(error.message);
    return;
  }
  const recipient = els.veiledWithdrawRecipient.value.trim();
  const relayMode = els.withdrawMode.value === "relay";
  if (!recipient) {
    toast(`Withdraw recipient에 받을 ${accountPrefix()} 주소를 넣어줘.`);
    return;
  }

  await setupKeplrPrivacy();
  if (!state.keplr.rootSignatureBase64) return;

  let timing;
  try {
    timing = await privacyOperationTiming();
  } catch (error) {
    toast(`최종 확인을 위한 체인 시간을 불러오지 못했습니다: ${error.message}`);
    return;
  }
  const confirmed = await openTransferFlowModal(relayMode ? "relay" : "withdraw", {
    chainId: activeChainProfile()?.chainId,
    recipient,
    amount: coinText(amount),
    disclosure: relayMode ? "Relay payload handoff" : "Withdraw recipient + amount",
    selfView: "Not applicable",
    expiresAtUnix: timing.expiresAtUnix
  });
  if (!confirmed) return;

  setWithdrawEvidence("Preparing · no broadcast yet", "Preparing · no broadcast yet");
  setBusy(els.withdrawFromVeiled, true);
  els.keplrTxState.textContent = "Preparing withdraw";
  try {
    const operationOptions = { ...timing, signal: activeProofSignal() };
    resetTransferPlannerFacts();
    updateTransferFlow(
      "zero",
      "노트 확인 중",
      "Withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다."
    );
    let data;
    try {
      data = relayMode
        ? await preparePrivacyRelayWithdraw(amount, recipient, operationOptions)
        : await preparePrivacyWithdrawSignDoc(amount, recipient, operationOptions);
    } catch (error) {
      if (!isExactMatchWithdrawError(error)) {
        throw error;
      }
      showTransferPlannerFacts({
        requested: amount,
        action: `${coinText(amount)} exact note를 만들기 위해 self transaction을 요청합니다.`
      });
      updateTransferFlow(
        "zero",
        "Self transaction 서명 대기",
        "Withdraw는 입력 금액과 정확히 같은 note가 필요합니다. 지금은 내 Veiled balance 안에서 exact note를 먼저 만듭니다."
      );
      await createExactWithdrawNote(amount, {
        onPlanCheck: step => {
          updateTransferFlow(
            "zero",
            step === 1 ? "노트 확인 중" : "노트 재확인 중",
            "Withdraw에 필요한 exact note를 만들 수 있는 note 조합을 확인합니다."
          );
        },
        onSelfMergeNeeded: data => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `두 note를 합쳐 ${data.prepared?.amount || data.plan?.nextAmount || "더 큰"} self note를 만듭니다.`
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "요청 금액의 exact note를 만들기 위해 두 note를 먼저 합칩니다. 이 단계는 내 Veiled balance 안에서만 준비됩니다."
          );
        },
        onZeroHelperNeeded: () => {
          showTransferPlannerFacts({
            requested: amount,
            action: `${zeroCoinText()} zero note를 만들어 exact note self transaction에 사용합니다.`
          });
          updateTransferFlow(
            "zero",
            "Zero note 서명 대기",
            "exact note를 만들기 위한 보조 zero note가 필요합니다. 이 단계도 내 Veiled balance 안에서만 준비됩니다."
          );
        },
        onFinalExactTransfer: data => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `${coinText(amount)} exact note를 만드는 마지막 self transaction을 요청합니다.`
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "입력 금액과 정확히 같은 note를 만들기 위해 self transaction을 요청합니다."
          );
        }
      }, operationOptions);
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. withdraw sign-doc을 다시 준비합니다."
      );
      data = relayMode
        ? await preparePrivacyRelayWithdraw(amount, recipient, operationOptions)
        : await preparePrivacyWithdrawSignDoc(amount, recipient, operationOptions);
    }
    if (relayMode) {
      updateTransferFlow(
        "transfer",
        "Payload 준비 완료",
        "Relayer에 전달할 payload가 생성되었습니다. 전송 전에 relayer가 payload와 candidate transaction을 독립적으로 검증해야 합니다."
      );
      await setRelayWithdrawHandoff(data);
      els.keplrTxState.textContent = "Relay withdraw payload ready";
      setWithdrawEvidence(
        "Reserved · awaiting relayer result",
        "Awaiting relayer submission",
        { render: false }
      );
      finishTransferFlow("Relay withdraw payload가 준비되었습니다");
      return;
    }
    updateTransferFlow(
      "transfer",
      "위드드로우 서명 대기",
      `note 준비가 완료되었습니다. 이제 Clair balance로 이동할 withdraw를 요청합니다. ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 최종 내용을 확인하고 서명해 주세요.`
    );
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "privacy withdraw");
    state.keplr.withdrawHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    state.keplr.withdrawHeight = broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm ? "Withdraw submitted" : "Withdraw included";
    setWithdrawEvidence(
      isPendingEvm ? "Submitted · not reconciled" : "Checking spent state",
      isPendingEvm ? "Submitted · receipt pending" : "Checking bound transparent output",
      { render: false }
    );
    renderKeplr();
    if (isPendingEvm) {
      finishTransferFlow("Withdraw 요청이 제출되었습니다");
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.withdrawHash = included.txHash || state.keplr.withdrawHash;
          state.keplr.withdrawHeight = included.receipt?.blockNumber || state.keplr.withdrawHeight;
          els.keplrTxState.textContent = "Withdraw included";
          await refreshPrivacySurfaces({ balance: true });
          await requirePreparedReservationReconciled(data, "Privacy withdraw");
          confirmWithdrawEvidence({ render: false });
          finishTransferFlow("Withdraw 요청이 성공하였습니다");
          renderKeplr();
        },
        onUnknown: async unknown => {
          state.keplr.withdrawHash = unknown.txHash || state.keplr.withdrawHash;
          els.keplrTxState.textContent = "Withdraw status unknown";
          setWithdrawEvidence(
            "Unknown · reconcile before retry",
            "Unknown · reconcile before retry",
            { render: false }
          );
          await refreshReservationState(data.reservationManager).catch(() => {});
          finishTransferFlowUnknown(`Receipt polling이 끝났지만 실패가 확인되지 않았습니다. tx hash와 nullifier를 reconcile하기 전에는 다시 전송하지 마세요.\nTx: ${state.keplr.withdrawHash}`);
          renderKeplr();
        },
        onFailed: async error => {
          const resolution = await resolvePreparedPrivacyFailure(error, data);
          els.keplrTxState.textContent = resolution.blocked ? "Withdraw reconciliation required" : "Withdraw failed";
          if (resolution.blocked) {
            setWithdrawEvidence(
              "Unknown · reservation remains locked",
              "Unknown · reconcile before retry",
              { render: false }
            );
            finishTransferFlowUnknown(error.message);
          } else {
            setWithdrawEvidence(
              "Unspent · failure confirmed",
              "Not received · transaction failed",
              { render: false }
            );
            finishTransferFlow(error.message, false, { retry: () => withdrawFromVeiled() });
          }
          renderKeplr();
        }
      });
      return;
    }
    await refreshPrivacySurfaces({ balance: true });
    await requirePreparedReservationReconciled(data, "Privacy withdraw");
    confirmWithdrawEvidence({ render: false });
    finishTransferFlow("Withdraw 요청이 성공하였습니다");
  } catch (error) {
    const cancelled = error?.name === "AbortError" || activeProofSignal()?.aborted;
    const resolution = await resolvePreparedPrivacyFailure(error);
    if (resolution.blocked) {
      els.keplrTxState.textContent = "Withdraw reconciliation required";
      setWithdrawEvidence(
        "Unknown · reservation remains locked",
        "Unknown · reconcile before retry",
        { render: false }
      );
      finishTransferFlowUnknown(error.message);
    } else {
      els.keplrTxState.textContent = cancelled ? "Withdraw cancelled" : "Withdraw failed";
      setWithdrawEvidence(
        cancelled ? "Not spent · cancelled before submission" : "Unspent · failure confirmed",
        cancelled ? "Not received · no submission" : "Not received · transaction failed",
        { render: false }
      );
      finishTransferFlow(cancelled ? "Proof 요청을 취소했습니다." : error.message, false, {
        retry: () => withdrawFromVeiled()
      });
    }
  } finally {
    setBusy(els.withdrawFromVeiled, false);
    renderKeplr();
  }
}

els.connectWallet.addEventListener("click", () => connectWallet().catch(error => toast(error.message)));
els.connectKeplr.addEventListener("click", () => connectKeplr().catch(error => toast(error.message)));
els.disconnectWallet.addEventListener("click", disconnectWallet);
els.dappChainSelect.addEventListener("change", event => selectDappChainProfile(event.target.value));
els.noteScanEndpoint.addEventListener("change", event => {
  try {
    selectNoteScanEndpoint(event.target.value);
    toast("Note scan endpoint changed. Retry Scan to continue recovery.");
  } catch (error) {
    toast(error.message);
  }
});
els.signSession.addEventListener("click", () => signSession().catch(error => toast(error.message)));
els.copyWalletAccount.addEventListener("click", () => copyWalletAccount().catch(error => toast(error.message)));
els.fundKeplr.addEventListener("click", fundKeplr);
els.setupKeplrPrivacy.addEventListener("click", () => setupKeplrPrivacy().catch(error => toast(error.message)));
els.copyKeplrShieldedAddress.addEventListener("click", () => copyKeplrShieldedAddress().catch(error => toast(error.message)));
els.copyKeplrDisclosurePubKey.addEventListener("click", () => copyKeplrDisclosurePubKey().catch(error => toast(error.message)));
els.refreshWalletBalance.addEventListener("click", () => refreshWalletBalance().catch(error => toast(error.message)));
els.scanKeplrNotes.addEventListener("click", () => scanKeplrNotes().catch(error => toast(error.message)));
els.backupNoteCache.addEventListener("click", () => backupNoteCache().catch(error => toast(error.message)));
els.resetRescanNotes.addEventListener("click", () => resetAndRescanNotes().catch(error => toast(error.message)));
els.noteRollbackHeight.addEventListener("input", () => updateNoteRollbackButton());
els.rollbackRescanNotes.addEventListener("click", () => rollbackAndRescanNotes().catch(error => toast(error.message)));
els.reconcileReservations.addEventListener("click", () => reconcileReservations().catch(error => toast(error.message)));
els.reservationRecoveryList.addEventListener("click", event => {
  const button = event.target.closest("[data-recover-reservation-operation]");
  if (!button || button.disabled) return;
  recoverReservationPreparation(button.dataset.recoverReservationOperation).catch(error => {
    els.keplrTxState.textContent = "Reservation recovery blocked";
    toast(error.message);
  });
});
els.myKeplrSpendableOnly.addEventListener("change", event => {
  state.keplr.showSpendableOnly = event.target.checked;
  renderMyKeplrNotes();
});
els.sendFromKeplr.addEventListener("click", sendFromKeplr);
els.reconcileKeplrSend.addEventListener("click", () => reconcilePublicEvmTransaction("send"));
els.clearPublicPendingState.addEventListener("click", clearPublicPendingTransactions);
els.depositFromKeplr.addEventListener("click", depositFromKeplr);
els.reconcileKeplrDeposit.addEventListener("click", () => reconcilePublicEvmTransaction("deposit"));
[
  els.keplrSendAmount,
  els.keplrSendRecipient,
  els.keplrDepositAmount,
  els.veiledTransferAmount,
  els.veiledWithdrawAmount
].forEach(input => {
  input.addEventListener("input", updateAmountActionButtons);
});
els.veiledDisclosureAdvanced.addEventListener("change", renderTransferDisclosureAdvanced);
els.veiledDisclosureMode.addEventListener("change", renderTransferDisclosureAdvanced);
els.includeSelfViewDisclosure.addEventListener("change", renderTransferDisclosureAdvanced);
els.transferFromVeiled.addEventListener("click", transferFromVeiled);
els.withdrawFromVeiled.addEventListener("click", withdrawFromVeiled);
els.withdrawMode.addEventListener("change", () => {
  renderRelayWithdraw();
  updateAmountActionButtons();
});
els.relayWithdrawTxHash.addEventListener("input", event => {
  state.relayWithdraw.txHash = event.target.value.trim();
  state.relayWithdraw.resultStatus = "waiting";
  state.relayWithdraw.resultMessage = state.relayWithdraw.txHash
    ? "Tx hash entered · result not checked"
    : "Waiting for relayer tx hash";
  renderRelayWithdraw();
  queueRelayWithdrawRecoverySave();
});
els.reconcileRelayWithdraw.addEventListener("click", () => reconcileRelayWithdrawResult());
els.copyRelayWithdraw.addEventListener("click", () => copyRelayWithdraw().catch(error => toast(error.message)));
els.downloadRelayWithdraw.addEventListener("click", () => {
  try {
    downloadRelayWithdraw();
  } catch (error) {
    toast(error.message);
  }
});
els.refreshAll.addEventListener("click", () => refreshHealth().catch(error => toast(error.message)));
els.refreshNotes.addEventListener("click", () => refreshNotes().catch(error => toast(error.message)));
els.refreshEvents.addEventListener("click", () => refreshEvents().catch(error => toast(error.message)));
els.decodeEventDisclosure.addEventListener("click", () => decodeSelectedEventDisclosure().catch(error => toast(error.message)));
els.decodeSelfViewDisclosure.addEventListener("click", () => decodeSelectedSelfViewDisclosure().catch(error => toast(error.message)));
els.decodeDisclosureSource.addEventListener("click", () => decodeDisclosureSource().catch(error => toast(error.message)));
if (els.refreshAuditorTransfers) {
  els.refreshAuditorTransfers.addEventListener("click", () => refreshAuditorTransfers().catch(error => toast(error.message)));
}
if (els.decodeAuditorTransfer) {
  els.decodeAuditorTransfer.addEventListener("click", () => decodeAuditorTransfer().catch(error => toast(error.message)));
}
els.closeNoticeModal.addEventListener("click", closeNoticeModal);
els.cancelTransferFlow.addEventListener("click", cancelTransferFlow);
els.retryTransferFlow.addEventListener("click", retryTransferFlow);
els.confirmTransferFlow.addEventListener("click", confirmTransferFlowStart);
els.noticeModal.addEventListener("click", event => {
  if (event.target === els.noticeModal) {
    closeNoticeModal();
  }
});
els.transferFlowModal.addEventListener("click", event => {
  if (event.target === els.transferFlowModal) {
    cancelTransferFlow();
  }
});
window.addEventListener("keydown", event => {
  if (event.key !== "Escape") return;
  if (!els.transferFlowModal.hidden) {
    cancelTransferFlow();
  } else if (!els.noticeModal.hidden) {
    closeNoticeModal();
  }
});
els.accountSelect.addEventListener("change", event => {
  state.selectedAccount = event.target.value;
  refreshSelectedAccount().catch(error => toast(error.message));
});

const injectedMetaMask = metaMaskProvider();
if (injectedMetaMask) {
  injectedMetaMask.on?.("accountsChanged", accounts => {
    if (state.activeWallet !== "metamask") return;
    resetWalletSession();
    renderWallet();
    renderKeplr();
    if (!accounts[0]) {
      return;
    }
    toast("MetaMask account changed. Reconnect wallet to refresh privacy identity.");
  });
  injectedMetaMask.on?.("chainChanged", chainId => {
    if (state.activeWallet !== "metamask") return;
    state.wallet.chainId = chainId;
    renderWallet();
  });
}

window.addEventListener("keplr_keystorechange", () => {
  if (state.activeWallet === "keplr") {
    state.activeWallet = "";
  }
  resetKeplrSession();
  renderWallet();
  renderKeplr();
});

renderWallet();
renderKeplr();
renderTransferDisclosureAdvanced();
setupAddressSuggestions();
refreshHealth().catch(error => toast(error.message));
