import {
  createClairveilBrowserDappClient,
  validateClairveilWebClientConfig
} from "clairveiljs/browser-dapp";
import { bech32AddressToEvm } from "clairveiljs/evm";
import { createPrivacyScanValidationStateV2 } from "clairveiljs/scan";
import { derivePrivacyMaterial } from "clairveiljs/crypto";
import {
  decodeAuditDisclosureFromEvent,
  decodeSelfViewDisclosureFromEvent,
  decodeUserDisclosureFromEvent,
  disclosureScalarFromHex
} from "clairveiljs/core";
import {
  activeReservationStatuses,
  hashAmount,
  hashRecipient,
  operationStatuses,
  reservationHeartbeatIntervalMs,
  reservationStatuses
} from "clairveiljs/reservation";
import {
  healthBootstrapEndpointAbsent,
  healthBootstrapFallbackAllowed,
  loadServerDappHealth,
  loadStaticDappConfig
} from "./dapp-config.js";
import { createBrowserTaskCoordinator } from "./browser-task-coordinator.js";
import { fetchBoundedJson } from "./bounded-json-fetch.js";
import { EncryptedLocalStorageNoteStore } from "./encrypted-note-store.js";
import {
  createEncryptedBrowserReservationManager,
  reservationBlocksReviewedReset,
  reservationHasUnresolvedOperationEvidence,
  resetEncryptedBrowserReservationState
} from "./encrypted-reservation-manager.js";
import {
  EncryptedLocalStorageOperationStore,
  relayWithdrawRecoveryMetadata,
  relayWithdrawRecoveryVersion,
  restoreRelayWithdrawRecoveryMetadata
} from "./encrypted-operation-store.js";
import { EncryptedRecoveryArtifactStore } from "./encrypted-recovery-artifact-store.js";
import { assertDepositFundingAvailable } from "./deposit-funding.js";
import { cosmosGasFeeAmount, deterministicCosmosFeeAmount } from "./cosmos-fee.js";
import { cosmosChargedFeeAmount, evmChargedFeeAmount } from "./network-fee.js";
import {
  commonCosmosReservationTransactionHash,
  cosmosPrivatePendingMarkerCanClear,
  cosmosReservationTransactionHash,
  cosmosTxEvidenceConfirmsFailure
} from "./cosmos-transaction-evidence.js";
import {
  assertPreparedTransferFreshAtChainTime,
  authoritativeChainBlockFromStatus,
  preparedTransferExpiryUnix,
  recoveredDepositNoteForTxHash,
  reservationConsumesBrowserCosmosSequence,
  typedPrivacyScanAfter
} from "./cosmos-flow-state.js";
import {
  browserLoopbackRewriteEnabled,
  normalizeBrowserEndpointUrl,
  normalizeBrowserProfileEndpoints,
  normalizeBrowserRestEndpoints
} from "./browser-profile.js";
import { localChainStorageEpoch, walletStorageScope } from "./browser-storage-scope.js";
import {
  privacyBrowserStorageCapability,
  requirePrivacyBrowserStorage
} from "./privacy-browser-storage.js";
import { keplrDirectSignOptions } from "./cosmos-sign-options.js";
import { disclosureViewModel } from "./disclosure-view-model.js";
import {
  directEvmOperationSuccessEvidence,
  evmReceiptExplicitlySucceeded,
  verifiedEvmTransactionResult
} from "./evm-reconciliation.js";
import { preparedBatchTransferFacts } from "./batch-transfer-state.js";
import {
  assertTypedBatchEffect,
  canonicalBatchEvidenceHex
} from "./batch-reconciliation.js";
import {
  sameTypedBatchEventIdentity,
  typedBatchEventIdentity
} from "./batch-event-identity.js";
import {
  evmBatchAuthorizationAvailable,
  evmBatchAuthorizationKinds,
  randomEvmAuthorizationNonce,
  selfSubmittedEvmBatchAuthorization
} from "./evm-batch-authorization.js";
import {
  findPrivacyEventByTxHash,
  normalizedTxHash,
  reservationPrivacyEventTypes
} from "./operation-event-lookup.js";
import {
  loadPrivacyPendingTxState,
  loadPublicPendingTxState,
  privacyPendingTxKey,
  publicPendingTxKey,
  savePrivacyPendingTxState,
  savePublicPendingTxState
} from "./public-pending-tx-store.js";
import {
  assertCosmosRelayWithdrawTransactionPayloadHash,
  assertRelayReservationPayloadMatches,
  assertRelayWithdrawTransactionMatches,
  createRelayWithdrawHandoff,
  relayWithdrawExpiryLeaseUntil,
  relayWithdrawHandoffPayload,
  relayWithdrawPayloadExpired
} from "./relay-withdraw-reconciliation.js";
import {
  assessReservationRecovery,
  canReconcileReservationState,
  canResetStaleLocalGenesisReservations,
  groupReservationOperations,
  isEmptyLocalGenesisPrivacyState,
  reconciliationReservationRecords,
  reservationOperationKey,
  succeededOperationLookupKeys
} from "./reservation-recovery.js";
import {
  replanExplicitlyFailedReservations,
  reservationHasExplicitBroadcastRejection
} from "./reservation-reconciliation.js";
import { createValueMovingActionGate } from "./value-moving-action.js";
import { cosmosWithdrawOperationEvidence } from "./withdraw-operation-evidence.js";
import {
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus
} from "./transaction-status.js";

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
    privacyPendingStateError: "",
    cosmosPrivacyPendingHash: "",
    depositHash: "",
    depositHeight: "",
    depositPrepared: null,
    depositRecoveryStatus: "idle",
    depositRecoveryMessage: "Not started",
    networkFeeEstimate: "Not estimated",
    networkFeeAmount: "0",
    transferHash: "",
    batchTransferHash: "",
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

function defaultAuditorState() {
  return {
    events: [],
    selectedTxHash: "",
    decoded: null,
    testScalar: "",
    testScalarError: "",
    testScalarMatchesAuditConfig: false,
    loading: false
  };
}

function defaultPrivacyEventsState() {
  return {
    events: [],
    selectedTxHash: "",
    decoded: null,
    error: "",
    loadError: "",
    loading: false
  };
}

function defaultBlockEventsState() {
  return {
    events: [],
    error: ""
  };
}

const state = {
  config: null,
  chainStorageEpoch: "",
  chainProfiles: [],
  selectedChainProfileId: "",
  selectedRestEndpointByProfile: {},
  accounts: [],
  selectedAccount: "alice",
  addressBook: {
    scopeIdentity: "",
    shieldedByName: {},
    shieldedError: "",
    loadingShielded: false
  },
  activeWallet: "",
  wallet: defaultMetaMaskState(),
  keplr: defaultKeplrState(),
  auditor: defaultAuditorState(),
  privacyEvents: defaultPrivacyEventsState(),
  blockEvents: defaultBlockEventsState(),
  protocol: {
    ready: false,
    reserve: null,
    error: ""
  },
  relayer: {
    balance: "",
    error: ""
  },
  relayWithdraw: {
    handoff: null,
    json: "",
    reservationIds: [],
    payloadHash: "",
    expiresAtUnix: 0,
    durableNoBroadcast: false,
    payloadUnavailable: false,
    txHash: "",
    submittedBy: "",
    evmSender: "",
    evmReceiptEvidence: null,
    externalHandoff: false,
    resultStatus: "idle",
    resultMessage: "Not checked"
  },
  relayWithdrawRecoveries: [],
  reservations: defaultReservationState()
};

const $ = selector => document.querySelector(selector);
let shieldedAddressBookPromise = null;
let shieldedAddressBookPromiseScope = "";
let browserClient = null;
let browserClientKey = "";
let browserClientDepositProofProvider = null;
let browserClientEvmContractAdapter = null;
let browserClientPrivacyStateAdapter = null;
let browserClientEvmFinalityPolicy = null;
let noteStore = null;
let noteStorePromise = null;
let noteStoreKey = "";
let reservationManager = null;
let reservationManagerPromise = null;
let reservationManagerKey = "";
let operationStore = null;
let operationStorePromise = null;
let operationStoreKey = "";
let batchTransferArtifactStore = null;
let batchTransferArtifactStorePromise = null;
let batchTransferArtifactStoreKey = "";
let evmDepositArtifactStore = null;
let evmDepositArtifactStorePromise = null;
let evmDepositArtifactStoreKey = "";
let batchTransferInFlight = false;
let batchTransferRowSequence = 0;
let relayReservationHeartbeatTimer = null;
let relayReservationHeartbeatGeneration = 0;
let relayHandoffInFlight = false;
let privacySessionGeneration = 0;
let healthRequestGeneration = 0;
let protocolRequestGeneration = 0;
let serverHealthEndpointState = "unknown";
const noteStoreCoordinator = createBrowserTaskCoordinator();
const publicTransactionCoordinator = createBrowserTaskCoordinator();
const valueMovingActionGate = createValueMovingActionGate();
const cosmosGasLimits = Object.freeze({
  send: 200000,
  deposit: 2500000,
  transfer: 8000000,
  withdraw: 5000000
});
function createDocumentReservationLeaseOwner(cryptoImpl = globalThis.crypto) {
  const uuid = cryptoImpl?.randomUUID?.();
  if (uuid) return `browser-document:${uuid}`;
  if (typeof cryptoImpl?.getRandomValues !== "function") {
    throw new Error("Web Crypto is required for a document-scoped reservation lease owner");
  }
  const bytes = cryptoImpl.getRandomValues(new Uint8Array(32));
  return `browser-document:${bytesToHex(bytes)}`;
}

// Never persist or copy this value through sessionStorage. A duplicated tab can
// inherit sessionStorage, but it must never inherit another document's lease.
const reservationLeaseOwner = createDocumentReservationLeaseOwner();
function activeChainProfile() {
  return state.chainProfiles.find(profile => profile.id === state.selectedChainProfileId)
    || state.chainProfiles.find(profile => profile.id === state.config?.activeChainProfileId)
    || state.config?.activeProfile
    || null;
}

function accountTransactionScopeId(profile = activeChainProfile()) {
  if (!profile) return "";
  if (profile.transport === "evm") {
    return `evm:${String(profile.evmChainId || profile.chainId || "").trim().toLowerCase()}`;
  }
  return `cosmos:${String(profile.chainId || "").trim()}`;
}

function privacyStorageProfileId(profile = activeChainProfile()) {
  if (!profile) return "";
  if (profile.transport === "evm") {
    return [
      accountTransactionScopeId(profile),
      String(profile.evmPrivacyPrecompileAddress || "").trim().toLowerCase()
    ].join(":");
  }
  return accountTransactionScopeId(profile);
}

function activeProfileSessionIdentity(profile = activeChainProfile()) {
  if (!profile) return "";
  return JSON.stringify({
    profile,
    selectedRestEndpoint: state.selectedRestEndpointByProfile[profile.id] || "",
    serverBinding: {
      serverBacked: Boolean(state.config?.serverBacked),
      activeChainProfileId: String(state.config?.activeChainProfileId || ""),
      activeProfile: state.config?.activeProfile || null,
      transport: String(state.config?.transport || ""),
      chainId: String(state.config?.chainId || ""),
      proverProxy: Boolean(state.config?.serverFeatures?.proverProxy),
      depositProof: Boolean(state.config?.serverFeatures?.depositProof),
      batchTransfer: Boolean(state.config?.serverFeatures?.batchTransfer),
      auditorAdmin: Boolean(state.config?.serverFeatures?.auditorAdmin)
    }
  });
}

function privacySessionSnapshot() {
  const pendingIdentity = publicPendingIdentity();
  return Object.freeze({
    generation: privacySessionGeneration,
    profileId: String(activeChainProfile()?.id || ""),
    chainId: String(activeChainProfile()?.chainId || ""),
    profileIdentity: activeProfileSessionIdentity(),
    wallet: String(state.activeWallet || ""),
    account: String(state.keplr.account || "").trim().toLowerCase(),
    transactionScope: accountTransactionScopeId(),
    storageEpoch: String(state.chainStorageEpoch || ""),
    publicPendingKey: String(pendingIdentity?.key || ""),
    privacyPendingKey: String(pendingIdentity?.privacyKey || ""),
    publicPendingIdentity: pendingIdentity
  });
}

function invalidatePrivacySession() {
  privacySessionGeneration += 1;
  noteStoreCoordinator.reset();
  publicTransactionCoordinator.reset();
  invalidateActivePrivacyFlow();
  resetDisclosureSessionState();
}

function stalePrivacySessionError(context) {
  const error = new Error("Wallet, account, network, or chain profile changed while the operation was pending; stale result discarded");
  error.name = "StalePrivacySessionError";
  error.code = "STALE_PRIVACY_SESSION";
  error.sessionContext = context;
  return error;
}

function assertPrivacySession(context) {
  const current = privacySessionSnapshot();
  if (!context
    || context.generation !== current.generation
    || context.profileId !== current.profileId
    || context.chainId !== current.chainId
    || context.profileIdentity !== current.profileIdentity
    || context.wallet !== current.wallet
    || context.account !== current.account
    || context.transactionScope !== current.transactionScope
    || context.storageEpoch !== current.storageEpoch
    || context.publicPendingKey !== current.publicPendingKey
    || context.privacyPendingKey !== current.privacyPendingKey) {
    throw stalePrivacySessionError(context);
  }
  return context;
}

function isStalePrivacySessionError(error) {
  return error?.code === "STALE_PRIVACY_SESSION";
}

function privacySessionIsCurrent(context) {
  try {
    assertPrivacySession(context);
    return true;
  } catch (error) {
    if (isStalePrivacySessionError(error)) return false;
    throw error;
  }
}

function reportAsyncError(error) {
  if (!isStalePrivacySessionError(error)) toast(error?.message || String(error));
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

function browserEndpointUrl(configured, {
  trim = false,
  localTestMode = browserLoopbackRewriteEnabled(state.config)
} = {}) {
  return normalizeBrowserEndpointUrl(configured, {
    browserHostname: window.location.hostname,
    trim,
    localTestMode
  });
}

function evmRpcUrlForWallet(profile = activeChainProfile()) {
  const configured = profile?.evmRpc || state.config?.evmRpc || "http://127.0.0.1:8545";
  return browserEndpointUrl(configured);
}

function browserRpcUrl(profile = activeChainProfile()) {
  return browserEndpointUrl(profile?.rpc || state.config?.rpc || "", { trim: true });
}

function browserRestUrl(profile = activeChainProfile()) {
  const endpoints = browserRestEndpointUrls(profile);
  return endpoints[0] || browserEndpointUrl(profile?.rest || state.config?.rest || "", { trim: true });
}

function browserRestEndpointUrls(profile = activeChainProfile()) {
  const endpoints = profileRestEndpoints(profile);
  const selected = state.selectedRestEndpointByProfile[profile?.id || ""];
  return normalizeBrowserRestEndpoints({
    ...profile,
    rest: profile?.rest || state.config?.rest || "",
    restEndpoints: endpoints
  }, {
    browserHostname: window.location.hostname,
    selectedEndpoint: endpoints.includes(selected) ? selected : "",
    localTestMode: browserLoopbackRewriteEnabled(state.config)
  });
}

function profileRestEndpoints(profile = activeChainProfile()) {
  const values = [
    profile?.rest || state.config?.rest || "",
    ...(Array.isArray(profile?.restEndpoints) ? profile.restEndpoints : [])
  ];
  return [...new Set(values.map(value => String(value || "").trim()).filter(Boolean))];
}

async function fetchLatestChainBlock({ signal } = {}) {
  const profile = activeChainProfile();
  const endpoint = browserRpcUrl(profile);
  if (!endpoint) throw new Error("A browser-accessible chain RPC endpoint is required for authoritative expiry");
  const data = await fetchBoundedJson(`${endpoint}/status`, {
    signal,
    label: "Latest block time query"
  });
  return authoritativeChainBlockFromStatus(data, profile);
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
  if (browserLoopbackRewriteEnabled(state.config)
    && serverFeature("proverProxy")
    && configured) {
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

function configuredEvmContractAdapter(profile = activeChainProfile()) {
  if (profile?.transport !== "evm") return null;
  const registry = globalThis.CLAIRVEIL_EVM_CONTRACT_ADAPTERS;
  if (registry == null) return null;
  if (!registry || typeof registry !== "object" || Array.isArray(registry)) {
    throw new Error("CLAIRVEIL_EVM_CONTRACT_ADAPTERS must be an object keyed by chain profile id");
  }
  const adapter = registry[profile.id];
  if (adapter == null) return null;
  if (!adapter || typeof adapter !== "object") {
    throw new Error(`EVM contract adapter for profile ${profile.id} must be an object`);
  }
  return adapter;
}

function configuredPrivacyStateAdapter(profile = activeChainProfile()) {
  if (!profile?.id) return null;
  const registry = globalThis.CLAIRVEIL_PRIVACY_STATE_ADAPTERS;
  if (registry == null) return null;
  if (typeof registry !== "object" || Array.isArray(registry)) {
    throw new Error("CLAIRVEIL_PRIVACY_STATE_ADAPTERS must be an object keyed by chain profile id");
  }
  const adapter = registry[profile.id];
  if (adapter == null) return null;
  if (typeof adapter !== "object" || Array.isArray(adapter)) {
    throw new Error(`Privacy-state adapter for profile ${profile.id} must be an object`);
  }
  return adapter;
}

function configuredEvmFinalityPolicy(profile = activeChainProfile()) {
  if (profile?.transport !== "evm" || !profile.id) return null;
  const registry = globalThis.CLAIRVEIL_EVM_FINALITY_POLICIES;
  if (registry == null) return null;
  if (typeof registry !== "object" || Array.isArray(registry)) {
    throw new Error("CLAIRVEIL_EVM_FINALITY_POLICIES must be an object keyed by EVM chain profile id");
  }
  const policy = registry[profile.id];
  if (policy == null) return null;
  if (typeof policy !== "string" && (typeof policy !== "object" || Array.isArray(policy))) {
    throw new Error(`EVM finality policy for profile ${profile.id} must be a mode string or policy object`);
  }
  return policy;
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
    restEndpoints: browserRestEndpointUrls(resolved),
    proverUrl: browserProverUrl(resolved),
    depositProofUrl: browserDepositProofUrl(resolved)
  });
  if (normalized.transport === "evm") {
    Object.assign(normalized, {
      evmRpc: evmRpcUrlForWallet(resolved),
      evmChainId: resolved?.evmChainId || state.config?.evmChainId,
      evmPrivacyPrecompileAddress: resolved?.evmPrivacyPrecompileAddress || state.config?.evmPrivacyPrecompileAddress,
      evmDepositMode: resolved?.evmDepositMode || state.config?.evmDepositMode || "payable-exact-value",
      evmNativeDenom: resolved?.evmNativeDenom || state.config?.evmNativeDenom || resolved?.denom,
      evmAuthorizationProfile: resolved?.evmAuthorizationProfile || state.config?.evmAuthorizationProfile || null,
      evmGasLimit: resolved?.evmGasLimit || state.config?.evmGasLimit,
      evmSendGasLimit: resolved?.evmSendGasLimit || state.config?.evmSendGasLimit
    });
  }
  return normalized;
}

function isolatedBrowserClient(config) {
  const profile = config?.activeProfile
    || config?.chainProfiles?.find(candidate => candidate.id === config.activeChainProfileId)
    || config?.chainProfiles?.[0];
  if (!profile) throw new Error("A validated Clairveil chain profile is required");
  const rest = profile?.rest
    || profile?.restEndpoints?.[0]
    || config?.rest
    || "";
  // Static bootstrap is not the reviewed local-helper deployment shape. Keep
  // its validated endpoint origins exact even if contradictory feature flags
  // are present in a hand-authored artifact.
  const localTestMode = false;
  const restEndpoints = normalizeBrowserRestEndpoints({
    ...profile,
    rest,
    restEndpoints: profile?.restEndpoints || []
  }, {
    browserHostname: window.location.hostname,
    localTestMode
  });
  const browserProfile = normalizeBrowserProfileEndpoints(profile, {
    rpc: browserEndpointUrl(profile?.rpc || config?.rpc || "", { trim: true, localTestMode }),
    rest: restEndpoints[0] || browserEndpointUrl(rest, { trim: true, localTestMode }),
    restEndpoints,
    proverUrl: browserEndpointUrl(profile?.proverUrl || config?.proverUrl || "", { trim: true, localTestMode }),
    depositProofUrl: browserEndpointUrl(
      profile?.depositProofUrl || config?.depositProofUrl || "",
      { trim: true, localTestMode }
    )
  });
  if (browserProfile.transport === "evm") {
    Object.assign(browserProfile, {
      evmRpc: browserEndpointUrl(profile?.evmRpc || config?.evmRpc || "", { localTestMode }),
      evmChainId: profile?.evmChainId || config?.evmChainId,
      evmPrivacyPrecompileAddress: profile?.evmPrivacyPrecompileAddress || config?.evmPrivacyPrecompileAddress,
      evmDepositMode: profile?.evmDepositMode || config?.evmDepositMode || "payable-exact-value",
      evmNativeDenom: profile?.evmNativeDenom || config?.evmNativeDenom || profile?.denom,
      evmAuthorizationProfile: profile?.evmAuthorizationProfile || config?.evmAuthorizationProfile || null,
      evmGasLimit: profile?.evmGasLimit || config?.evmGasLimit,
      evmSendGasLimit: profile?.evmSendGasLimit || config?.evmSendGasLimit
    });
  }
  return createClairveilBrowserDappClient({
    profile: browserProfile,
    depositProofProvider: configuredDepositProofProvider(),
    evmContractAdapter: configuredEvmContractAdapter(browserProfile),
    privacyStateAdapter: configuredPrivacyStateAdapter(browserProfile),
    evmFinalityPolicy: configuredEvmFinalityPolicy(browserProfile),
    enableExperimentalBatchTransfer: config?.serverFeatures?.batchTransfer === true
  });
}

function validateCurrentWebAppConfig(config) {
  return validateClairveilWebClientConfig(config);
}

function clairveilBrowserClient(profile = activeChainProfile()) {
  const resolved = profile || state.config?.activeProfile;
  if (!resolved) throw new Error("A validated Clairveil chain profile is required");
  const depositProofProvider = configuredDepositProofProvider();
  const browserProfile = browserWalletProfile(resolved);
  const evmContractAdapter = configuredEvmContractAdapter(browserProfile);
  const privacyStateAdapter = configuredPrivacyStateAdapter(browserProfile);
  const evmFinalityPolicy = configuredEvmFinalityPolicy(browserProfile);
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
    evmDepositMode: browserProfile?.evmDepositMode || "payable-exact-value",
    evmNativeDenom: browserProfile?.evmNativeDenom || "",
    evmAuthorizationProfile: browserProfile?.evmAuthorizationProfile || null,
    batchTransfer: serverFeature("batchTransfer")
  });
  if (!browserClient || browserClientKey !== key
    || browserClientDepositProofProvider !== depositProofProvider
    || browserClientEvmContractAdapter !== evmContractAdapter
    || browserClientPrivacyStateAdapter !== privacyStateAdapter
    || browserClientEvmFinalityPolicy !== evmFinalityPolicy) {
    browserClient = createClairveilBrowserDappClient({
      profile: browserProfile,
      depositProofProvider,
      evmContractAdapter,
      privacyStateAdapter,
      evmFinalityPolicy,
      enableExperimentalBatchTransfer: serverFeature("batchTransfer")
    });
    browserClientKey = key;
    browserClientDepositProofProvider = depositProofProvider;
    browserClientEvmContractAdapter = evmContractAdapter;
    browserClientPrivacyStateAdapter = privacyStateAdapter;
    browserClientEvmFinalityPolicy = evmFinalityPolicy;
  }
  return browserClient;
}

function noteStoreKeys() {
  const profile = activeChainProfile();
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  const scope = walletStorageScope({
    chainId: profile?.chainId || profile?.id,
    profileId: privacyStorageProfileId(profile),
    owner,
    localTestMode: Boolean(state.config?.localTestMode),
    storageEpoch: state.chainStorageEpoch
  });
  if (!scope) return null;
  return {
    owner,
    namespace: scope.namespace,
    encrypted: `clairveil:v0.3.1:notes-encrypted:${scope.keySuffix}`,
    legacy: `clairveil:v0.3.1:notes:${scope.keySuffix}`
  };
}

function publicPendingIdentity() {
  const profile = activeChainProfile();
  const owner = String(state.keplr.account || "").trim().toLowerCase();
  const scope = walletStorageScope({
    chainId: profile?.chainId || profile?.id,
    profileId: accountTransactionScopeId(profile),
    owner,
    localTestMode: Boolean(state.config?.localTestMode),
    storageEpoch: state.chainStorageEpoch
  });
  if (!scope) return null;
  const identity = {
    profileId: accountTransactionScopeId(profile),
    owner,
    storageEpoch: scope.storageEpoch
  };
  const key = publicPendingTxKey(identity);
  const privacyKey = privacyPendingTxKey(identity);
  return key && privacyKey ? { ...identity, key, privacyKey } : null;
}

function hydratePublicPendingTransactions() {
  const identity = publicPendingIdentity();
  state.keplr.publicPendingStateError = "";
  state.keplr.privacyPendingStateError = "";
  state.keplr.cosmosPrivacyPendingHash = "";
  if (!identity || !globalThis.localStorage) return;
  try {
    const saved = loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
    if (saved?.send) {
      state.keplr.sendHash = saved.send.txHash || "";
      state.keplr.sendStatus = saved.send.status;
    }
    if (saved?.deposit) {
      state.keplr.depositHash = saved.deposit.txHash || "";
      state.keplr.depositHeight = saved.deposit.height || "";
      state.keplr.depositRecoveryStatus = saved.deposit.status;
      state.keplr.depositRecoveryMessage = saved.deposit.status === "recovery-pending"
        ? "Restored included tx · encrypted note recovery still pending"
        : saved.deposit.txHash
          ? "Restored unresolved tx · reconcile before retrying"
          : "Wallet submission may have started · check wallet history before clearing";
    }
  } catch (error) {
    state.keplr.publicPendingStateError = error.message;
    state.keplr.sendStatus = "unknown";
    state.keplr.depositRecoveryStatus = "unknown";
    state.keplr.depositRecoveryMessage = error.message;
  }
  try {
    const privacy = loadPrivacyPendingTxState(
      globalThis.localStorage,
      identity.privacyKey,
      identity
    );
    if (privacy) state.keplr.cosmosPrivacyPendingHash = privacy.txHash;
  } catch (error) {
    state.keplr.privacyPendingStateError = error.message;
    state.keplr.depositRecoveryMessage = error.message;
  }
}

function publicTransactionLockName(context) {
  const transactionScope = String(context?.transactionScope || "").trim();
  const account = String(context?.account || "").trim().toLowerCase();
  if (!transactionScope || !account) {
    throw new Error("Account transaction recovery identity is unavailable");
  }
  return `clairveil:v0.3.1:account-transaction:${transactionScope}:${account}`;
}

function capturedPublicPendingState(context) {
  const identity = context?.publicPendingIdentity;
  if (!identity?.key || !globalThis.localStorage) return null;
  return loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
}

function capturedPrivacyPendingState(context) {
  const identity = context?.publicPendingIdentity;
  if (!identity?.privacyKey || !globalThis.localStorage) return null;
  return loadPrivacyPendingTxState(globalThis.localStorage, identity.privacyKey, identity);
}

function publicPendingEntriesWith(existing, kind, entry) {
  if (!["send", "deposit"].includes(kind)) {
    throw new Error(`Unsupported pending transaction kind: ${kind}`);
  }
  return {
    send: kind === "send" ? entry : existing?.send,
    deposit: kind === "deposit" ? entry : existing?.deposit
  };
}

function assertNoCapturedPublicPendingTransaction(context, kind) {
  const pendingState = capturedPublicPendingState(context);
  const pendingKind = pendingState?.send ? "send" : pendingState?.deposit ? "deposit" : "";
  const pending = pendingKind ? pendingState[pendingKind] : null;
  if (!pending) return;
  const evidence = pending.txHash
    ? `reconcile ${pending.txHash}`
    : "check wallet history and resolve the saved wallet-boundary attempt";
  const error = new Error(`An unresolved ${pendingKind} transaction already exists; ${evidence} before starting ${kind}`);
  error.code = "PUBLIC_TX_PENDING";
  error.pendingKind = pendingKind;
  error.pendingTxHash = pending.txHash;
  throw error;
}

function clearCapturedPublicPendingTransaction(context, kind, txHash) {
  const identity = context?.publicPendingIdentity;
  const privacy = kind === "privacy";
  const key = privacy ? identity?.privacyKey : identity?.key;
  if (!key || !globalThis.localStorage) return false;
  const existing = privacy
    ? null
    : loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
  const entry = privacy
    ? loadPrivacyPendingTxState(globalThis.localStorage, identity.privacyKey, identity)
    : existing?.[kind];
  if (!entry) return true;
  if (normalizedHex(entry.txHash) !== normalizedHex(txHash)) {
    throw new Error(`Refusing to clear a different unresolved ${kind} transaction`);
  }
  if (privacy) {
    savePrivacyPendingTxState(globalThis.localStorage, identity.privacyKey, {
      ...identity,
      privacy: null
    });
  } else {
    savePublicPendingTxState(globalThis.localStorage, identity.key, {
      ...identity,
      ...publicPendingEntriesWith(existing, kind, null)
    });
  }
  if (kind === "privacy" && context?.generation === privacySessionGeneration) {
    state.keplr.cosmosPrivacyPendingHash = "";
  }
  return true;
}

function withAccountTransactionLock(context, task) {
  if (typeof globalThis.navigator?.locks?.request !== "function") {
    throw new Error("Account transaction serialization requires browser Web Locks support");
  }
  const operation = publicTransactionCoordinator.run(publicTransactionLockName(context), async () => {
    assertPrivacySession(context);
    return task();
  });
  renderNoteScanEndpoint();
  return operation.finally(() => {
    renderNoteScanEndpoint();
  });
}

function withPublicTransactionLock(context, task) {
  return withAccountTransactionLock(context, task);
}

async function assertCurrentLocalChainStorageEpoch(context) {
  if (state.config?.localTestMode !== true) return;
  assertPrivacySession(context);
  const health = await clairveilBrowserClient().health();
  assertPrivacySession(context);
  const observed = localChainStorageEpoch({ localTestMode: true, status: health?.status });
  if (!observed || observed !== context.storageEpoch) {
    const error = new Error("The local chain genesis changed while this transaction was pending. Refresh chain health and reconnect before retrying.");
    error.code = "CHAIN_STORAGE_EPOCH_CHANGED";
    error.expectedStorageEpoch = context.storageEpoch;
    error.observedStorageEpoch = observed;
    throw error;
  }
}

async function assertNoUnresolvedCosmosAccountBroadcast(context) {
  assertPrivacySession(context);
  await assertCurrentLocalChainStorageEpoch(context);
  const accountPending = capturedPrivacyPendingState(context);
  if (accountPending) {
    const error = new Error(
      `An unresolved privacy transaction already exists (${accountPending.txHash}); setup Clairveil and reconcile it before preparing another Cosmos account sequence`
    );
    error.code = "COSMOS_ACCOUNT_TX_PENDING";
    error.txHash = accountPending.txHash;
    throw error;
  }
  const manager = await currentReservationManager();
  assertPrivacySession(context);
  if (!manager) return;
  const active = await manager.listActiveReservations();
  assertPrivacySession(context);
  const accountSequenceContext = {
    browserAccount: state.keplr.account,
    localRelayer: localRelayerAccount()
  };
  const unresolved = active.find(record => (
    record?.broadcast_in_flight === true
      || Boolean(reservationTransactionHash(record))
      || [reservationStatuses.Submitted, reservationStatuses.Unknown].includes(record?.status)
  ) && reservationConsumesBrowserCosmosSequence(record, accountSequenceContext));
  if (!unresolved) return;
  const txHash = reservationTransactionHash(unresolved);
  const error = new Error(
    `An unresolved privacy transaction already exists${txHash ? ` (${txHash})` : ""}; reconcile it before preparing another Cosmos account sequence`
  );
  error.code = "COSMOS_ACCOUNT_TX_PENDING";
  error.txHash = txHash;
  throw error;
}

function withCosmosAccountTransactionLock(context, task) {
  if (activeChainProfile()?.transport !== "cosmos") {
    return task(false);
  }
  return withAccountTransactionLock(context, async () => {
    assertNoCapturedPublicPendingTransaction(context, "privacy transaction");
    await assertNoUnresolvedCosmosAccountBroadcast(context);
    return task(true);
  });
}

function withBatchAccountTransactionLock(context, task) {
  if (activeChainProfile()?.transport === "cosmos") {
    return withCosmosAccountTransactionLock(context, task);
  }
  return withAccountTransactionLock(context, () => task(true));
}

function transactionHashFromEvidence(value = {}) {
  const candidates = [
    value.txHash,
    value.txhash,
    value.broadcast?.txHash,
    value.broadcast?.txhash,
    value.txBytesHash,
    value.tx_bytes_hash
  ];
  return candidates
    .map(candidate => String(candidate || "").trim())
    .find(candidate => /^(0x)?[0-9a-fA-F]{64}$/.test(candidate)) || "";
}

function reservationTransactionHash(record = {}) {
  if (activeChainProfile()?.transport === "cosmos") {
    return cosmosReservationTransactionHash(record);
  }
  const submitted = String(record.submitted_tx_hash || "").trim();
  return /^(0x)?[0-9a-fA-F]{64}$/.test(submitted)
    ? submitted.replace(/^0x/i, "").toLowerCase()
    : "";
}

function commonReservationTransactionHash(records = []) {
  if (activeChainProfile()?.transport === "cosmos") {
    return commonCosmosReservationTransactionHash(records);
  }
  if (!Array.isArray(records) || records.length === 0) return "";
  const hashes = records.map(reservationTransactionHash);
  if (hashes.some(hash => !hash)) return "";
  const unique = [...new Set(hashes)];
  return unique.length === 1 ? unique[0] : "";
}

function canonicalCosmosTxCode(value) {
  if (typeof value === "number") {
    return Number.isSafeInteger(value) && value >= 0 ? value : null;
  }
  if (typeof value !== "string" || !/^(0|[1-9][0-9]*)$/.test(value)) {
    return null;
  }
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) ? parsed : null;
}

function newPublicTransactionAttemptId() {
  if (typeof globalThis.crypto?.getRandomValues !== "function") {
    throw new Error("Durable wallet-boundary recovery requires Web Crypto");
  }
  return bytesToHex(globalThis.crypto.getRandomValues(new Uint8Array(32)));
}

function persistCapturedPublicTransactionAttempt(context, kind) {
  const identity = context?.publicPendingIdentity;
  if (!identity?.key || !globalThis.localStorage) {
    throw new Error("Durable public transaction recovery storage is unavailable");
  }
  assertNoCapturedPublicPendingTransaction(context, kind);
  const attemptId = newPublicTransactionAttemptId();
  const existing = loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
  const entry = { attemptId, status: "attempting" };
  savePublicPendingTxState(globalThis.localStorage, identity.key, {
    ...identity,
    ...publicPendingEntriesWith(existing, kind, entry)
  });
  assertPrivacySession(context);
  if (kind === "send") {
    state.keplr.sendHash = "";
    state.keplr.sendStatus = "attempting";
  } else {
    state.keplr.depositHash = "";
    state.keplr.depositRecoveryStatus = "attempting";
    state.keplr.depositRecoveryMessage = "Wallet request opened · submission result not known yet";
  }
  renderKeplr();
  return attemptId;
}

function clearCapturedPublicTransactionAttempt(context, kind, attemptId) {
  const identity = context?.publicPendingIdentity;
  if (!identity?.key || !globalThis.localStorage) return false;
  const existing = loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
  const entry = existing?.[kind];
  if (!entry) return true;
  if (entry.status !== "attempting" || entry.attemptId !== attemptId || entry.txHash) {
    throw new Error(`Refusing to clear a different unresolved ${kind} wallet attempt`);
  }
  savePublicPendingTxState(globalThis.localStorage, identity.key, {
    ...identity,
    ...publicPendingEntriesWith(existing, kind, null)
  });
  return true;
}

function publicEvmTransactionBoundaryCallbacks(context, kind) {
  return {
    onTransactionAttempt: () => persistCapturedPublicTransactionAttempt(context, kind),
    onTransactionHash: (txHash, attemptId) => persistCapturedPublicPendingTransaction(
      context,
      kind,
      txHash,
      attemptId
    ),
    onTransactionRejected: attemptId => clearCapturedPublicTransactionAttempt(
      context,
      kind,
      attemptId
    )
  };
}

function restoreHashlessPublicAttemptAfterError(context, kind, error) {
  let entry;
  try {
    entry = capturedPublicPendingState(context)?.[kind];
  } catch {
    hydratePublicPendingTransactions();
    showNotice({
      title: `${kind === "send" ? "Send" : "Deposit"} recovery state 확인 필요`,
      message: "The durable wallet-attempt state could not be decoded. Check wallet history before using the guarded manual clear action."
    });
    return true;
  }
  if (entry?.status !== "attempting" || entry.txHash) return false;
  hydratePublicPendingTransactions();
  showNotice({
    title: `${kind === "send" ? "Send" : "Deposit"} 제출 여부 확인 필요`,
    message: `${error.message}\nMetaMask history에서 제출 여부를 확인하기 전에는 다시 보내거나 wallet-attempt marker를 지우지 마세요.`
  });
  return true;
}

function persistCapturedPublicPendingTransaction(context, kind, txHash, attemptId = "") {
  try {
    const identity = context?.publicPendingIdentity;
    const privacy = kind === "privacy";
    if (!(privacy ? identity?.privacyKey : identity?.key) || !globalThis.localStorage) {
      throw new Error("Durable public transaction recovery storage is unavailable");
    }
    const existing = privacy
      ? null
      : loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
    const previous = privacy
      ? loadPrivacyPendingTxState(globalThis.localStorage, identity.privacyKey, identity)
      : existing?.[kind];
    if (previous?.txHash
      && normalizedHex(previous.txHash) !== normalizedHex(txHash)) {
      throw new Error(`Refusing to replace a different unresolved ${kind} transaction fence`);
    }
    if (attemptId && (previous?.status !== "attempting" || previous?.attemptId !== attemptId)) {
      throw new Error(`Refusing to replace a different unresolved ${kind} wallet attempt`);
    }
    const entry = {
      txHash,
      status: "unknown",
      ...(attemptId || previous?.attemptId ? { attemptId: attemptId || previous.attemptId } : {})
    };
    if (privacy) {
      savePrivacyPendingTxState(globalThis.localStorage, identity.privacyKey, {
        ...identity,
        privacy: entry
      });
    } else {
      savePublicPendingTxState(globalThis.localStorage, identity.key, {
        ...identity,
        ...publicPendingEntriesWith(existing, kind, entry)
      });
    }

    assertPrivacySession(context);
    if (kind === "send") {
      state.keplr.sendHash = txHash;
      state.keplr.sendStatus = "unknown";
    } else if (kind === "deposit") {
      state.keplr.depositHash = txHash;
      state.keplr.depositRecoveryStatus = "unknown";
      state.keplr.depositRecoveryMessage = "Signed · broadcast result pending · do not retry";
    } else if (kind === "privacy") {
      state.keplr.cosmosPrivacyPendingHash = txHash;
    }
    renderKeplr();
  } catch (error) {
    const failure = error instanceof Error ? error : new Error(String(error));
    failure.txHash ||= txHash;
    throw failure;
  }
}

function persistCapturedDepositRecoveryPending(context, txHash, height = "") {
  const identity = context?.publicPendingIdentity;
  if (!identity?.key || !globalThis.localStorage) {
    throw new Error("Durable deposit recovery storage is unavailable");
  }
  const existing = loadPublicPendingTxState(globalThis.localStorage, identity.key, identity);
  const previous = existing?.deposit;
  if (previous?.txHash && normalizedHex(previous.txHash) !== normalizedHex(txHash)) {
    throw new Error("Refusing to replace a different unresolved deposit transaction");
  }
  const entry = {
    txHash,
    status: "recovery-pending",
    ...(previous?.attemptId ? { attemptId: previous.attemptId } : {}),
    ...(height ? { height: String(height) } : {})
  };
  savePublicPendingTxState(globalThis.localStorage, identity.key, {
    ...identity,
    ...publicPendingEntriesWith(existing, "deposit", entry)
  });
  assertPrivacySession(context);
  state.keplr.depositHash = txHash;
  state.keplr.depositHeight = height || state.keplr.depositHeight;
  state.keplr.depositRecoveryStatus = "recovery-pending";
  state.keplr.depositRecoveryMessage = "Included · encrypted note recovery pending";
}

async function clearPublicPendingTransactions() {
  const identity = publicPendingIdentity();
  const clearingCorruptState = Boolean(state.keplr.publicPendingStateError);
  const clearingHashlessAttempt = (state.keplr.sendStatus === "attempting" && !state.keplr.sendHash)
    || (state.keplr.depositRecoveryStatus === "attempting" && !state.keplr.depositHash);
  if (!identity || (!clearingCorruptState && !clearingHashlessAttempt)) return;
  const storage = globalThis.localStorage;
  if (!storage) return;
  const confirmedRawState = storage.getItem(identity.key);
  if (confirmedRawState == null) {
    hydratePublicPendingTransactions();
    renderKeplr();
    return;
  }
  const warning = clearingCorruptState
    ? "Only the public send/deposit recovery record will be cleared. The separate private Cosmos transaction fence is never removed by this action. Continue only after checking wallet history and chain transactions?"
    : "The wallet request may have submitted a transaction without returning its hash. Clear this attempt only after checking wallet history for the account. Continue?";
  if (!window.confirm(warning)) return;
  const sessionContext = privacySessionSnapshot();
  try {
    await withPublicTransactionLock(sessionContext, () => {
      if (storage.getItem(identity.key) !== confirmedRawState) {
        const error = new Error("Pending public transaction state changed while waiting for the account lock. Review the current state before clearing it.");
        error.code = "PUBLIC_PENDING_STATE_CHANGED";
        throw error;
      }
      storage.removeItem(identity.key);
    });
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (!isStalePrivacySessionError(error)) {
      hydratePublicPendingTransactions();
      renderKeplr();
    }
    throw error;
  }
  state.keplr.publicPendingStateError = "";
  state.keplr.sendHash = "";
  state.keplr.sendStatus = "idle";
  state.keplr.depositHash = "";
  state.keplr.depositHeight = "";
  state.keplr.depositRecoveryStatus = "idle";
  state.keplr.depositRecoveryMessage = "Not started";
  renderKeplr();
}

async function resetCorruptPrivateRecoveryStateUnlocked() {
  if (activeChainProfile()?.transport !== "cosmos" || !state.keplr.privacyPendingStateError) return;
  if (!state.keplr.rootSignatureBase64) {
    throw new Error("Setup Clairveil before starting the reviewed privacy recovery reset");
  }
  if (!state.protocol.ready) {
    throw new Error("Protocol preflight must pass before the reviewed privacy recovery reset");
  }
  const storage = requirePrivacyBrowserStorage();
  const identity = publicPendingIdentity();
  if (!identity?.privacyKey) {
    throw new Error("Private Cosmos recovery identity is unavailable");
  }
  const confirmedRawState = storage.getItem(identity.privacyKey);
  if (confirmedRawState == null) {
    hydratePublicPendingTransactions();
    renderKeplr();
    return;
  }
  const sessionContext = privacySessionSnapshot();
  const chainId = String(activeChainProfile()?.chainId || "").trim();
  const account = String(state.keplr.account || "").trim();
  const confirmationPhrase = `RESET ${chainId} ${account}`;
  const entered = globalThis.prompt(
    "The private Cosmos transaction identity cannot be decoded. This reset does not cancel a transaction that was already approved or propagated.\n\n"
    + `First inspect Keplr activity and the explorer for account ${account} on ${chainId}. Confirm that no private transfer, withdraw, or self-merge is pending or submitted. Close other Clairveil tabs. Relay handoffs must be reconciled separately.\n\n`
    + `To replace this account's current private state with a full typed scan, enter exactly:\n${confirmationPhrase}`
  );
  assertPrivacySession(sessionContext);
  if (entered !== confirmationPhrase) return;

  try {
    await withPublicTransactionLock(sessionContext, async () => {
      assertPrivacySession(sessionContext);
      await assertCurrentLocalChainStorageEpoch(sessionContext);
      assertPrivacySession(sessionContext);
      if (storage.getItem(identity.privacyKey) !== confirmedRawState) {
        const error = new Error("Private Cosmos recovery state changed while waiting for the account lock. Review the current state before resetting it.");
        error.code = "PRIVACY_PENDING_STATE_CHANGED";
        throw error;
      }

      const recoveryStore = await currentOperationStore();
      assertPrivacySession(sessionContext);
      if (!recoveryStore) {
        throw new Error("Encrypted relay recovery storage is unavailable");
      }
      const relayRecoveriesBeforeScan = await recoveryStore.loadAll();
      assertPrivacySession(sessionContext);
      if (relayRecoveriesBeforeScan.length) {
        throw new Error("Reconcile every relay handoff before resetting private Cosmos recovery state");
      }

      await scanKeplrNotes({
        reset: true,
        skipSetup: true,
        throwOnError: true,
        maxPages: 1000,
        sessionContext,
        accountTransactionLockHeld: true
      });
      assertPrivacySession(sessionContext);
      const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
      if (state.keplr.noteSyncStatus !== "synced"
        || Boolean(cursor.has_more ?? cursor.hasMore)
        || String(cursor.source || "") !== "privacy_scan") {
        throw new Error("Reviewed privacy recovery requires a complete genesis typed scan before reset");
      }

      const manager = await currentReservationManager();
      assertPrivacySession(sessionContext);
      if (!manager) {
        throw new Error("Encrypted reservation storage is unavailable");
      }
      const reservations = await manager.store.listReservations({
        ownerKeyId: manager.ownerKeyId
      });
      assertPrivacySession(sessionContext);
      if (reservations.some(reservationBlocksReviewedReset)) {
        throw new Error("Active or unresolved note reservations remain. Reconcile them before resetting private Cosmos recovery state");
      }
      const relayRecoveriesAfterScan = await recoveryStore.loadAll();
      assertPrivacySession(sessionContext);
      if (relayRecoveriesAfterScan.length) {
        throw new Error("A relay recovery appeared during review. Reconcile it before resetting private Cosmos recovery state");
      }

      await resetEncryptedBrowserReservationState(manager, {
        confirmedReviewedFreshStateReset: true,
        afterReset: () => {
          assertPrivacySession(sessionContext);
          if (storage.getItem(identity.privacyKey) !== confirmedRawState) {
            const error = new Error("Private Cosmos recovery state changed before reset commit. It was not cleared.");
            error.code = "PRIVACY_PENDING_STATE_CHANGED";
            throw error;
          }
          storage.removeItem(identity.privacyKey);
          if (storage.getItem(identity.privacyKey) !== null) {
            throw new Error("Private Cosmos recovery state could not be cleared; the account remains blocked");
          }
        }
      });
      assertPrivacySession(sessionContext);

      reservationManager = null;
      reservationManagerPromise = null;
      reservationManagerKey = "";
      state.reservations = defaultReservationState();
    });
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (!isStalePrivacySessionError(error)) {
      hydratePublicPendingTransactions();
      renderKeplr();
    }
    throw error;
  }

  hydratePublicPendingTransactions();
  state.keplr.privacyPendingStateError = "";
  state.keplr.cosmosPrivacyPendingHash = "";
  state.keplr.noteSyncMessage = "Fresh private state restored · full typed scan complete";
  els.keplrTxState.textContent = "Private recovery state reset";
  renderKeplr();
  toast("Reviewed private recovery reset completed. Current notes were rebuilt from the chain.");
}

function resetCorruptPrivateRecoveryState() {
  return runValueMovingAction("privacy-fresh-state-reset", resetCorruptPrivateRecoveryStateUnlocked);
}

async function currentNoteStore() {
  const keys = noteStoreKeys();
  if (!keys || !state.keplr.rootSignatureBase64) return null;
  const storage = requirePrivacyBrowserStorage();
  if (noteStore && noteStoreKey === keys.encrypted) return noteStore;
  if (!noteStorePromise || noteStoreKey !== keys.encrypted) {
    const openingKey = keys.encrypted;
    noteStoreKey = openingKey;
    const opening = EncryptedLocalStorageNoteStore.open({
      storage,
      key: openingKey,
      owner: keys.owner,
      namespace: keys.namespace,
      keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
    }).then(store => {
      if (noteStoreKey === openingKey && noteStorePromise === opening) {
        noteStore = store;
        if (storage.getItem(keys.legacy)) {
          storage.removeItem(keys.legacy);
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
  const scope = walletStorageScope({
    chainId: profile?.chainId,
    profileId: privacyStorageProfileId(profile),
    owner,
    localTestMode: Boolean(state.config?.localTestMode),
    storageEpoch: state.chainStorageEpoch
  });
  if (!scope || !state.keplr.rootSignatureBase64) return null;
  return {
    owner,
    ownerKeyId: scope.ownerKeyId,
    namespace: scope.namespace,
    cacheKey: `${scope.keySuffix}:${state.keplr.rootSignatureHash || state.keplr.signatureHash}`
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
  const scope = walletStorageScope({
    chainId: profile?.chainId || profile?.id,
    profileId: privacyStorageProfileId(profile),
    owner,
    localTestMode: Boolean(state.config?.localTestMode),
    storageEpoch: state.chainStorageEpoch
  });
  if (!scope || !state.keplr.rootSignatureBase64) return null;
  return {
    profileId: privacyStorageProfileId(profile),
    owner,
    namespace: scope.namespace,
    key: `clairveil:v0.3.1:operations-encrypted:${scope.keySuffix}`
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
      locks: globalThis.navigator?.locks,
      requireLocks: true,
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

function batchTransferArtifactIdentity() {
  const identity = operationStoreIdentity();
  if (!identity) return null;
  return {
    ...identity,
    key: identity.key.replace(":operations-encrypted:", ":batch-transfer-artifact-encrypted:")
  };
}

async function currentBatchTransferArtifactStore({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const identity = batchTransferArtifactIdentity();
  if (!identity || !globalThis.localStorage) return null;
  if (batchTransferArtifactStore && batchTransferArtifactStoreKey === identity.key) {
    return batchTransferArtifactStore;
  }
  if (!batchTransferArtifactStorePromise || batchTransferArtifactStoreKey !== identity.key) {
    const openingKey = identity.key;
    batchTransferArtifactStoreKey = openingKey;
    const opening = EncryptedRecoveryArtifactStore.open({
      storage: globalThis.localStorage,
      locks: globalThis.navigator?.locks,
      key: identity.key,
      profileId: identity.profileId,
      owner: identity.owner,
      keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
    }).then(store => {
      if (batchTransferArtifactStoreKey === openingKey && batchTransferArtifactStorePromise === opening) {
        batchTransferArtifactStore = store;
      }
      return store;
    }).catch(error => {
      if (batchTransferArtifactStoreKey === openingKey && batchTransferArtifactStorePromise === opening) {
        batchTransferArtifactStorePromise = null;
      }
      throw error;
    });
    batchTransferArtifactStorePromise = opening;
  }
  const store = await batchTransferArtifactStorePromise;
  assertPrivacySession(sessionContext);
  return store;
}

function batchReservationIDs(reservation = {}) {
  return [...new Set(reservation?.reservation_ids || [])].sort();
}

function batchTransferOperationEvidenceHash(data = {}) {
  return String(
    data?.operationEvidenceHash
      || data?.operation_evidence_hash
      || data?.prepared?.operationEvidenceHash
      || data?.prepared?.operation_evidence_hash
      || ""
  ).trim();
}

function assertBatchTransferArtifactIdentity(artifact, identity) {
  if (!artifact
    || artifact.version !== "clairveil-batch-transfer-artifact-v1"
    || artifact.profileId !== identity.profileId
    || String(artifact.owner || "").toLowerCase() !== identity.owner) {
    throw new Error("Encrypted batch recovery artifact does not match the active wallet or chain profile");
  }
}

function batchTransferArtifactRecord(artifact, identity) {
  return {
    version: "clairveil-batch-transfer-artifact-v1",
    profileId: identity.profileId,
    owner: identity.owner,
    savedAt: new Date().toISOString(),
    ...artifact
  };
}

async function saveBatchTransferArtifact(artifact, { sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentBatchTransferArtifactStore({ sessionContext });
  const identity = batchTransferArtifactIdentity();
  if (!store || !identity) throw new Error("Encrypted batch recovery storage is unavailable");
  await store.save(batchTransferArtifactRecord(artifact, identity), {
    beforeCommit: () => assertPrivacySession(sessionContext)
  });
  assertPrivacySession(sessionContext);
}

async function updateBatchTransferArtifact(updater, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentBatchTransferArtifactStore({ sessionContext });
  const identity = batchTransferArtifactIdentity();
  if (!store || !identity) throw new Error("Encrypted batch recovery storage is unavailable");
  const result = await store.update(current => {
    assertBatchTransferArtifactIdentity(current, identity);
    const updated = updater(current);
    return updated === undefined
      ? undefined
      : batchTransferArtifactRecord(updated, identity);
  }, { beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
  return result;
}

async function loadBatchTransferArtifact({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentBatchTransferArtifactStore({ sessionContext });
  const identity = batchTransferArtifactIdentity();
  if (!store || !identity) return null;
  const artifact = await store.load();
  assertPrivacySession(sessionContext);
  if (!artifact) return null;
  assertBatchTransferArtifactIdentity(artifact, identity);
  return artifact;
}

async function clearBatchTransferArtifact({
  reservation = null,
  records = [],
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentBatchTransferArtifactStore({ sessionContext });
  const identity = batchTransferArtifactIdentity();
  if (!store || !identity) return false;
  const expectedIDs = records.length
    ? [...new Set(records.map(record => record?.reservation_id).filter(Boolean))].sort()
    : batchReservationIDs(reservation);
  if (!expectedIDs.length) {
    throw new Error("Batch recovery artifact clear requires its reservation identity");
  }
  const result = await store.clearIf(artifact => {
    assertBatchTransferArtifactIdentity(artifact, identity);
    const actualIDs = batchReservationIDs(artifact.reservation);
    return actualIDs.length === expectedIDs.length
      && actualIDs.every((id, index) => id === expectedIDs[index]);
  }, { beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
  return result.changed;
}

function evmDepositArtifactIdentity() {
  const identity = operationStoreIdentity();
  if (!identity) return null;
  return {
    ...identity,
    key: identity.key.replace(":operations-encrypted:", ":evm-deposit-artifact-encrypted:")
  };
}

async function currentEvmDepositArtifactStore({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const identity = evmDepositArtifactIdentity();
  if (!identity || !globalThis.localStorage) return null;
  if (evmDepositArtifactStore && evmDepositArtifactStoreKey === identity.key) {
    return evmDepositArtifactStore;
  }
  if (!evmDepositArtifactStorePromise || evmDepositArtifactStoreKey !== identity.key) {
    const openingKey = identity.key;
    evmDepositArtifactStoreKey = openingKey;
    const opening = EncryptedRecoveryArtifactStore.open({
      storage: globalThis.localStorage,
      locks: globalThis.navigator?.locks,
      key: identity.key,
      profileId: identity.profileId,
      owner: identity.owner,
      keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
    }).then(store => {
      if (evmDepositArtifactStoreKey === openingKey && evmDepositArtifactStorePromise === opening) {
        evmDepositArtifactStore = store;
      }
      return store;
    }).catch(error => {
      if (evmDepositArtifactStoreKey === openingKey && evmDepositArtifactStorePromise === opening) {
        evmDepositArtifactStorePromise = null;
      }
      throw error;
    });
    evmDepositArtifactStorePromise = opening;
  }
  const store = await evmDepositArtifactStorePromise;
  assertPrivacySession(sessionContext);
  return store;
}

async function saveEvmDepositArtifact(artifact, { sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentEvmDepositArtifactStore({ sessionContext });
  const identity = evmDepositArtifactIdentity();
  if (!store || !identity) throw new Error("Encrypted EVM deposit recovery storage is unavailable");
  await store.save({
    version: "clairveil-evm-deposit-artifact-v1",
    profileId: identity.profileId,
    owner: identity.owner,
    savedAt: new Date().toISOString(),
    ...artifact
  }, { beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
}

function assertEvmDepositArtifactIdentity(artifact, identity) {
  if (!artifact
    || artifact.version !== "clairveil-evm-deposit-artifact-v1"
    || artifact.profileId !== identity.profileId
    || String(artifact.owner || "").toLowerCase() !== identity.owner) {
    throw new Error("Encrypted EVM deposit recovery artifact does not match the active wallet or chain profile");
  }
}

async function loadEvmDepositArtifact({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentEvmDepositArtifactStore({ sessionContext });
  const identity = evmDepositArtifactIdentity();
  if (!store || !identity) return null;
  const artifact = await store.load();
  assertPrivacySession(sessionContext);
  if (!artifact) return null;
  assertEvmDepositArtifactIdentity(artifact, identity);
  return artifact;
}

async function clearEvmDepositArtifact(txHash = "", {
  force = false,
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentEvmDepositArtifactStore({ sessionContext });
  const identity = evmDepositArtifactIdentity();
  if (!store || !identity) return false;
  if (force) {
    await store.clear({ beforeCommit: () => assertPrivacySession(sessionContext) });
    assertPrivacySession(sessionContext);
    return { changed: true, previous: null, artifact: null };
  }
  const expectedTxHash = String(txHash || "").trim();
  if (!expectedTxHash) {
    throw new Error("EVM deposit artifact clear requires its submitted transaction hash");
  }
  const normalizedExpectedTxHash = normalizeEvmTxHash(expectedTxHash);
  const result = await store.clearIf(artifact => {
    assertEvmDepositArtifactIdentity(artifact, identity);
    return Boolean(artifact.txHash)
      && normalizeEvmTxHash(artifact.txHash) === normalizedExpectedTxHash;
  }, { beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
  return result;
}

async function clearConfirmedDepositRecoveryUnlocked(sessionContext, txHash) {
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport === "evm") {
    const result = await clearEvmDepositArtifact(txHash, { sessionContext });
    if (!result.changed && result.previous) {
      throw new Error(
        "A newer EVM deposit recovery artifact replaced this transaction; its pending state was not cleared"
      );
    }
  }
  clearCapturedPublicPendingTransaction(sessionContext, "deposit", txHash);
  assertPrivacySession(sessionContext);
}

async function hydrateEvmDepositRecovery({
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport !== "evm") return null;
  const artifact = await loadEvmDepositArtifact({ sessionContext });
  if (!artifact) return null;
  const sender = String(artifact.sender || "").trim().toLowerCase();
  if (!artifact.transaction || !artifact.prepared?.noteCommitmentHex
    || !/^0x[0-9a-f]{40}$/.test(sender)) {
    throw new Error("Encrypted EVM deposit recovery artifact is incomplete; manual recovery is required");
  }
  if (artifact.txHash) {
    if (state.keplr.depositHash
      && normalizeEvmTxHash(state.keplr.depositHash) !== normalizeEvmTxHash(artifact.txHash)) {
      throw new Error("Encrypted EVM deposit recovery artifact does not match the restored pending transaction hash");
    }
    state.keplr.depositHash = artifact.txHash;
    if (state.keplr.depositRecoveryStatus === "idle") {
      state.keplr.depositRecoveryStatus = "submitted";
      state.keplr.depositRecoveryMessage = "Restored encrypted EVM deposit recovery binding · reconcile before retrying";
    }
  } else if (artifact.phase === "broadcast-unknown-no-hash") {
    state.keplr.depositRecoveryStatus = "attempting";
    state.keplr.depositRecoveryMessage = "Wallet submission may have started · check wallet activity before using the guarded manual clear";
  }
  state.keplr.depositPrepared = {
    ...artifact.prepared,
    transaction: artifact.transaction,
    sender
  };
  assertPrivacySession(sessionContext);
  return artifact;
}

async function restoreEvmDepositRecovery({
  sessionContext = privacySessionSnapshot()
} = {}) {
  try {
    return await hydrateEvmDepositRecovery({ sessionContext });
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
    state.keplr.publicPendingStateError = `EVM deposit recovery is unavailable: ${error.message}`;
    state.keplr.depositRecoveryStatus = "unknown";
    state.keplr.depositRecoveryMessage = state.keplr.publicPendingStateError;
    return null;
  }
}

function preparedOperationKey(data = {}) {
  return String(
    data?.reservation?.operation_id
      || data?.reservation?.reservations?.[0]?.operation_id
      || preparedReservationIDs(data).sort().join(":")
  ).trim();
}

function reservationGroupOperationKey(records = []) {
  const keys = [...new Set((records || [])
    .map(record => reservationOperationKey(record))
    .filter(Boolean))];
  if (keys.length > 1) {
    throw new Error("Reservations disagree about their operation identity");
  }
  return keys[0] || "";
}

function evmOperationArtifactIdentity(operationKey) {
  const identity = operationStoreIdentity();
  const key = String(operationKey || "").trim();
  if (!identity || !key) return null;
  const encodedKey = encodeURIComponent(key);
  return {
    ...identity,
    operationKey: key,
    key: identity.key.replace(":operations-encrypted:", `:evm-operation-artifact-encrypted:${encodedKey}:`)
  };
}

async function currentEvmOperationArtifactStore(operationKey, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const identity = evmOperationArtifactIdentity(operationKey);
  if (!identity || !globalThis.localStorage) return null;
  const store = await EncryptedRecoveryArtifactStore.open({
    storage: globalThis.localStorage,
    locks: globalThis.navigator?.locks,
    key: identity.key,
    profileId: identity.profileId,
    owner: identity.owner,
    keyMaterial: base64ToBytes(state.keplr.rootSignatureBase64)
  });
  assertPrivacySession(sessionContext);
  return store;
}

async function saveEvmOperationArtifact(operationKey, artifact, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const identity = evmOperationArtifactIdentity(operationKey);
  const store = await currentEvmOperationArtifactStore(operationKey, { sessionContext });
  if (!identity || !store) throw new Error("Encrypted EVM operation recovery storage is unavailable");
  await store.save({
    version: "clairveil-evm-operation-artifact-v1",
    profileId: identity.profileId,
    owner: identity.owner,
    operationKey: identity.operationKey,
    savedAt: new Date().toISOString(),
    ...artifact
  }, { beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
}

async function loadEvmOperationArtifact(operationKey, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const identity = evmOperationArtifactIdentity(operationKey);
  const store = await currentEvmOperationArtifactStore(operationKey, { sessionContext });
  if (!identity || !store) return null;
  const artifact = await store.load();
  assertPrivacySession(sessionContext);
  if (!artifact) return null;
  if (artifact.version !== "clairveil-evm-operation-artifact-v1"
    || artifact.profileId !== identity.profileId
    || String(artifact.owner || "").toLowerCase() !== identity.owner
    || artifact.operationKey !== identity.operationKey) {
    throw new Error("Encrypted EVM operation artifact does not match the active wallet, profile, or operation");
  }
  return artifact;
}

async function clearEvmOperationArtifact(operationKey, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentEvmOperationArtifactStore(operationKey, { sessionContext });
  await store?.clear({ beforeCommit: () => assertPrivacySession(sessionContext) });
  assertPrivacySession(sessionContext);
}

function evmOperationArtifactMatchesReservations(artifact, records = []) {
  const expected = [...new Set(records.map(record => record?.reservation_id).filter(Boolean))].sort();
  const actual = [...new Set(artifact?.reservationIds || [])].sort();
  return expected.length > 0
    && expected.length === actual.length
    && expected.every((id, index) => id === actual[index]);
}

async function checkpointPreparedEvmOperation(data, kind, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport !== "evm" || !data?.transaction || !data?.txBytesHash) return;
  const operationKey = preparedOperationKey(data);
  const reservationIds = preparedReservationIDs(data).sort();
  if (!operationKey || !reservationIds.length || !state.wallet.account) {
    throw new Error("Prepared EVM operation is missing its durable reservation or sender identity");
  }
  await saveEvmOperationArtifact(operationKey, {
    phase: "proof-ready",
    kind,
    reservationIds,
    transaction: data.transaction,
    txBytesHash: data.txBytesHash,
    sender: state.wallet.account
  }, { sessionContext });
}

async function updatePreparedEvmOperationArtifact(data, patch, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const operationKey = preparedOperationKey(data);
  if (!operationKey) return;
  const artifact = await loadEvmOperationArtifact(operationKey, { sessionContext });
  if (!artifact) throw new Error("Encrypted EVM operation recovery artifact is unavailable");
  const reservationIds = preparedReservationIDs(data).sort();
  if (!evmOperationArtifactMatchesReservations(artifact, reservationIds.map(reservation_id => ({ reservation_id })))) {
    throw new Error("Encrypted EVM operation recovery artifact does not match the prepared reservation");
  }
  await saveEvmOperationArtifact(operationKey, { ...artifact, ...patch }, { sessionContext });
}

async function loadEvmOperationArtifactForReservations(records = [], {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const operationKey = reservationGroupOperationKey(records);
  if (!operationKey) return null;
  const artifact = await loadEvmOperationArtifact(operationKey, { sessionContext });
  if (!artifact) return null;
  if (!evmOperationArtifactMatchesReservations(artifact, records)) {
    throw new Error("Encrypted EVM operation recovery artifact does not match its reservations");
  }
  return artifact;
}

function batchArtifactMatchesReservations(artifact, records = []) {
  const artifactIDs = batchReservationIDs(artifact?.reservation);
  const recordIDs = [...new Set(records.map(record => record.reservation_id).filter(Boolean))].sort();
  return artifactIDs.length > 0
    && artifactIDs.length === recordIDs.length
    && artifactIDs.every((id, index) => id === recordIDs[index]);
}

function isBatchReservationOperation(records = []) {
  return records.some(record => Boolean(
    record?.metadata?.batch_transfer_operation_evidence_hash
      || record?.metadata?.batchTransferOperationEvidenceHash
  ));
}

function batchReservationExecutionTransport(records = [], artifact = null) {
  const transports = [...new Set(records.map(record => String(
    record?.metadata?.execution_transport
      || record?.metadata?.executionTransport
      || ""
  ).trim().toLowerCase()).filter(Boolean))];
  if (transports.length > 1) {
    throw new Error("Batch reservations disagree about their execution transport");
  }
  return transports[0] || String(artifact?.executionTransport || artifact?.transport || "").trim().toLowerCase();
}

function batchReceiptEvidence(data, result) {
  const operationEvidenceHash = batchTransferOperationEvidenceHash(data);
  if (!operationEvidenceHash) throw new Error("EVM batch operation-evidence binding is required for reconciliation");
  return {
    txResult: verifiedEvmTransactionResult(result, "EVM batch receipt"),
    operationEvidenceHash
  };
}

async function saveBatchReceiptEvidence(data, result, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const records = data?.reservation?.reservations || [];
  const expected = [...new Set(records.map(record => normalizedHex(record.expected_operation_evidence_hash)).filter(Boolean))];
  if (expected.length !== 1 || normalizedHex(batchTransferOperationEvidenceHash(data)) !== expected[0]) {
    throw new Error("Prepared batch operation evidence does not match its durable reservation binding");
  }
  const receiptEvidence = batchReceiptEvidence(data, result);
  await updateBatchTransferArtifact(artifact => {
    if (!batchArtifactMatchesReservations(artifact, records)) {
      throw new Error("Encrypted batch recovery artifact does not match the prepared note reservation");
    }
    return {
      ...artifact,
      phase: "receipt-verified",
      txHash: result.txHash,
      txBytesHash: result.txBytesHash,
      receiptEvidence
    };
  }, { sessionContext });
  return receiptEvidence;
}

async function saveBatchSubmission(data, txHash, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const records = data?.reservation?.reservations || [];
  await updateBatchTransferArtifact(artifact => {
    if (!batchArtifactMatchesReservations(artifact, records)) {
      throw new Error("Encrypted batch recovery artifact does not match the submitted note reservation");
    }
    return {
      ...artifact,
      phase: "submitted",
      txHash,
      txBytesHash: data.txBytesHash || artifact.txBytesHash
    };
  }, { sessionContext });
}

async function saveBatchInclusion(data, txHash, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const records = data?.reservation?.reservations || [];
  await updateBatchTransferArtifact(artifact => {
    if (!batchArtifactMatchesReservations(artifact, records)) {
      throw new Error("Encrypted batch recovery artifact does not match the included note reservation");
    }
    return {
      ...artifact,
      phase: "included",
      txHash: txHash || artifact.txHash
    };
  }, { sessionContext });
}

async function batchReceiptEvidenceForReservations(records = [], {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (!isBatchReservationOperation(records)) return null;
  const artifact = await loadBatchTransferArtifact({ sessionContext });
  if (!artifact || !batchArtifactMatchesReservations(artifact, records)) return null;
  if (batchReservationExecutionTransport(records, artifact) !== "evm") return null;
  const receiptEvidence = artifact.receiptEvidence;
  const expected = [...new Set(records.map(record => normalizedHex(record.expected_operation_evidence_hash)).filter(Boolean))];
  if (expected.length !== 1 || normalizedHex(receiptEvidence?.operationEvidenceHash) !== expected[0]) return null;
  return receiptEvidence;
}

function batchOperationEvidence(artifact = {}) {
  return artifact.operationEvidence
    || artifact.prepared?.operationEvidence
    || artifact.prepared?.operation_evidence
    || null;
}

async function typedCosmosBatchEvidence(txHash, height, artifact, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const validationState = createPrivacyScanValidationStateV2();
  let after = {
    height: Math.max(0, height - 1),
    globalSequence: 0,
    outputIndex: 0
  };
  const targetSummaries = new Map();
  const targetOutputs = new Map();
  for (let pageIndex = 0; pageIndex < 1000; pageIndex += 1) {
    const page = await clairveilBrowserClient().fetchAuditableBatchTransfers({
      after,
      outputLimit: 128,
      eventLimit: 64,
      maxEncodedBytes: 1048576,
      validationState
    });
    assertPrivacySession(sessionContext);
    for (const summary of page.summaries) {
      if (canonicalBatchEvidenceHex(summary.tx_hash) !== normalizedHex(txHash)) continue;
      const identity = typedBatchEventIdentity(summary);
      targetSummaries.set(`${identity.height}:${identity.globalSequence}`, summary);
    }
    for (const output of page.outputs) {
      if (canonicalBatchEvidenceHex(output.tx_hash) !== normalizedHex(txHash)) continue;
      const identity = typedBatchEventIdentity(output);
      const summary = targetSummaries.get(`${identity.height}:${identity.globalSequence}`);
      if (!summary || !sameTypedBatchEventIdentity(identity, typedBatchEventIdentity(summary))) {
        throw new Error("Typed batch output has no matching transaction summary");
      }
      const key = `${identity.height}:${identity.globalSequence}:${output.output_index}`;
      const existing = targetOutputs.get(key);
      if (existing && canonicalBatchEvidenceHex(existing.commitment) !== canonicalBatchEvidenceHex(output.commitment)) {
        throw new Error("Typed batch scan returned conflicting output evidence");
      }
      targetOutputs.set(key, output);
    }
    if (targetSummaries.size > 1) {
      throw new Error("Submitted transaction contains multiple batch operations; automatic reconciliation is ambiguous");
    }
    const summary = [...targetSummaries.values()][0];
    if (summary) {
      const identity = typedBatchEventIdentity(summary);
      const outputs = [...targetOutputs.values()].filter(output => (
        sameTypedBatchEventIdentity(typedBatchEventIdentity(output), identity)
      ));
      if (outputs.length === Number(summary.output_count)) {
        outputs.sort((left, right) => Number(left.output_index) - Number(right.output_index));
        assertTypedBatchEffect({
          summary,
          outputs,
          operationEvidence: batchOperationEvidence(artifact),
          outputCount: artifact?.prepared?.outputCount,
          txHash,
          maxOutputs: batchTransferMaxPayments
        });
        return true;
      }
    }
    if (!page.has_more) return false;
    after = {
      height: page.next_cursor.height,
      globalSequence: page.next_cursor.global_sequence,
      outputIndex: page.next_cursor.output_index
    };
    if (Number(after.height) > height && !targetSummaries.size) return false;
  }
  throw new Error("Typed batch reconciliation exceeded the privacy scan page limit");
}

async function cosmosBatchEvidenceForReservations(records = [], {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (!isBatchReservationOperation(records)) return null;
  const artifact = await loadBatchTransferArtifact({ sessionContext });
  if (!artifact || !batchArtifactMatchesReservations(artifact, records)) return null;
  if (batchReservationExecutionTransport(records, artifact) !== "cosmos") return null;
  const expected = [...new Set(records.map(record => normalizedHex(record.expected_operation_evidence_hash)).filter(Boolean))];
  if (expected.length !== 1 || normalizedHex(artifact.operationEvidenceHash) !== expected[0]) return null;
  const operationEvidence = batchOperationEvidence(artifact);
  if (!operationEvidence
    || normalizedHex(await digestText(JSON.stringify(operationEvidence))) !== expected[0]) {
    throw new Error("Encrypted batch operation evidence does not match its durable evidence hash");
  }
  const txHashes = [...new Set(records.map(record => String(record.submitted_tx_hash || "").trim()).filter(Boolean))];
  if (txHashes.length !== 1) return null;
  const txHash = txHashes[0];
  const check = await checkReservationTransaction(txHash);
  assertPrivacySession(sessionContext);
  if (!check.included || check.failed) return null;
  const height = authoritativeTransactionHeight(check);
  if (!height) throw new Error(`Included batch transaction ${txHash} has no authoritative height`);
  if (!await typedCosmosBatchEvidence(txHash, height, artifact, { sessionContext })) return null;
  return { txHash, operationEvidenceHash: expected[0] };
}

async function refreshPersistedEvmBatchReceiptEvidence(manager, {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (!manager || activeChainProfile()?.transport !== "evm") return false;
  const allReservations = await manager.store.listReservations({ ownerKeyId: manager.ownerKeyId });
  assertPrivacySession(sessionContext);
  const groups = groupReservationOperations(allReservations);
  let updated = false;
  for (const group of groups) {
    const records = group.records || [];
    if (!isBatchReservationOperation(records)) continue;
    const artifact = await loadBatchTransferArtifact({ sessionContext });
    if (!artifact || !batchArtifactMatchesReservations(artifact, records) || artifact.receiptEvidence) continue;
    const txHashes = [...new Set(records.map(record => String(record.submitted_tx_hash || "").trim()).filter(Boolean))];
    if (txHashes.length !== 1 || !artifact.transaction) continue;
    let result;
    try {
      result = await waitForEvmTransaction(
        txHashes[0],
        "EVM batch transfer",
        { sessionContext },
        artifact.transaction,
        artifact.sender || state.wallet.account
      );
      assertPrivacySession(sessionContext);
    } catch (error) {
      // A failed receipt is conclusive non-spend evidence, not receipt evidence
      // for a successful batch. Leave it for reconcileReservations(), which
      // verifies every nullifier is unspent before marking ReplanRequired.
      if (evmReceiptHasFailed(error?.broadcast?.receipt)) continue;
      throw error;
    }
    if (result.unknown) continue;
    await saveBatchReceiptEvidence({
      reservation: artifact.reservation,
      operationEvidenceHash: artifact.operationEvidenceHash
    }, result, { sessionContext });
    updated = true;
  }
  return updated;
}

async function hydrateBatchTransferRecovery({
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const artifact = await loadBatchTransferArtifact({ sessionContext });
  if (artifact?.txHash) state.keplr.batchTransferHash = artifact.txHash;
}

async function assertNoUnresolvedBatchTransferArtifact({
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const artifact = await loadBatchTransferArtifact({ sessionContext });
  if (!artifact) return;
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  const reservationIDs = batchReservationIDs(artifact.reservation);
  const records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
  assertPrivacySession(sessionContext);
  if (records.some(record => !record)) {
    throw new Error("Encrypted batch recovery artifact is missing its reservation identity; manual recovery is required");
  }
  const terminal = new Set([
    reservationStatuses.ConfirmedSpent,
    reservationStatuses.ReplanRequired,
    reservationStatuses.Released,
    reservationStatuses.Failed
  ]);
  if (records.every(record => terminal.has(record.status))) {
    await clearBatchTransferArtifact({ records, sessionContext });
    return;
  }
  throw new Error("A previous atomic batch is still unresolved. Reconcile or review its note reservation before preparing another batch.");
}


async function persistRelayWithdrawRecovery(next = state.relayWithdraw, {
  store = null,
  identity = null,
  sessionContext = null
} = {}) {
  if (sessionContext) assertPrivacySession(sessionContext);
  const resolvedStore = store || await currentOperationStore();
  if (sessionContext) assertPrivacySession(sessionContext);
  if (!resolvedStore) throw new Error("Encrypted operation recovery store is not available");
  const resolvedIdentity = identity || operationStoreIdentity();
  if (!resolvedIdentity) throw new Error("Encrypted operation recovery identity is not available");
  if (!next?.handoff && !next?.reservationIds?.length) {
    throw new Error("A payload-bound relay recovery identity is required before persistence");
  }
  await resolvedStore.save({
    version: relayWithdrawRecoveryVersion,
    profileId: resolvedIdentity.profileId,
    owner: resolvedIdentity.owner,
    relayWithdraw: relayWithdrawRecoveryMetadata(next)
  }, {
    beforeCommit: sessionContext
      ? () => assertPrivacySession(sessionContext)
      : undefined
  });
  if (sessionContext) assertPrivacySession(sessionContext);
}

async function hydrateRelayWithdrawRecovery({
  sessionContext = privacySessionSnapshot(),
  preferredPayloadHash = ""
} = {}) {
  assertPrivacySession(sessionContext);
  const store = await currentOperationStore();
  assertPrivacySession(sessionContext);
  const identity = operationStoreIdentity();
  if (!store || !identity) return;
  const savedRecords = await store.loadAll();
  assertPrivacySession(sessionContext);
  if (!savedRecords.length) {
    state.relayWithdrawRecoveries = [];
    state.relayWithdraw = clearedRelayWithdrawState("idle", "Not checked");
    return;
  }
  const recoveries = savedRecords.map(saved => {
    if (saved.version !== relayWithdrawRecoveryVersion
      || saved.profileId !== identity.profileId
      || saved.owner !== identity.owner
      || !Array.isArray(saved.relayWithdraw?.reservationIds)) {
      const error = new Error("Encrypted relay recovery state has an invalid identity or format");
      error.code = "OPERATION_STATE_CORRUPT";
      throw error;
    }
    try {
      return restoreRelayWithdrawRecoveryMetadata(saved.relayWithdraw);
    } catch (cause) {
      const error = new Error("Encrypted relay recovery state contains invalid metadata", { cause });
      error.code = "OPERATION_STATE_CORRUPT";
      throw error;
    }
  });
  state.relayWithdrawRecoveries = recoveries;
  const preferred = String(preferredPayloadHash || state.relayWithdraw.payloadHash || "")
    .trim()
    .toLowerCase();
  const restored = recoveries.find(recovery => recovery.payloadHash === preferred) || recoveries[0];
  const nextRelayWithdraw = {
    ...state.relayWithdraw,
    ...restored,
    leaseToken: "",
    leaseUntil: ""
  };
  if (recoveries.length > 1) {
    nextRelayWithdraw.resultMessage += ` · ${recoveries.length - 1} additional payload-bound recovery record(s) remain encrypted`;
  }
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  const records = manager
    ? await Promise.all(nextRelayWithdraw.reservationIds.map(id => manager.getReservation(id)))
    : [];
  assertPrivacySession(sessionContext);
  if (manager && records.some(record => !record)) {
    const error = new Error("Encrypted relay recovery references a missing reservation record");
    error.code = "OPERATION_STATE_CORRUPT";
    throw error;
  }
  const reservationTxHashes = records.map(record => normalizedHex(record?.submitted_tx_hash));
  const commonReservationTxHashes = [...new Set(reservationTxHashes.filter(Boolean))];
  if (commonReservationTxHashes.length > 1
    || (commonReservationTxHashes.length === 1 && reservationTxHashes.some(hash => !hash))
    || (commonReservationTxHashes.length === 1
      && nextRelayWithdraw.txHash
      && normalizedHex(nextRelayWithdraw.txHash) !== commonReservationTxHashes[0])) {
    const error = new Error("Linked relay reservations contain conflicting or partial submitted transaction identities");
    error.code = "OPERATION_STATE_CORRUPT";
    throw error;
  }
  if (commonReservationTxHashes.length === 1) {
    nextRelayWithdraw.txHash = commonReservationTxHashes[0];
    nextRelayWithdraw.submittedBy = "Recovered from durable reservation transaction evidence";
    nextRelayWithdraw.resultStatus = "unknown";
    nextRelayWithdraw.resultMessage = "Restored relay transaction identity from linked reservations · reconcile chain evidence before retrying";
  }
  if (records.some(record => record?.metadata?.relay_handed_off === true)) {
    nextRelayWithdraw.externalHandoff = true;
    nextRelayWithdraw.durableNoBroadcast = false;
  }
  assertPrivacySession(sessionContext);
  state.relayWithdraw = nextRelayWithdraw;
}

async function selectRelayWithdrawRecovery(payloadHash) {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  await hydrateRelayWithdrawRecovery({
    sessionContext,
    preferredPayloadHash: payloadHash
  });
  assertPrivacySession(sessionContext);
  renderKeplr();
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
  const sessionContext = data?.privacySessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = data?.reservationManager;
  const reservation = data?.reservation;
  const reservationIDs = preparedReservationIDs(data);
  const leaseToken = reservation?.lease_token || reservation?.reservations?.[0]?.lease_token || "";
  if (!manager || !reservationIDs.length || !leaseToken || typeof manager.heartbeatLease !== "function") {
    const result = await task();
    assertPrivacySession(sessionContext);
    return result;
  }

  let heartbeatError = null;
  let heartbeatPromise = null;
  const heartbeat = async () => {
    if (heartbeatError) return;
    try {
      assertPrivacySession(sessionContext);
      const renewed = await manager.heartbeatLease(reservationIDs, { leaseToken });
      assertPrivacySession(sessionContext);
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
    assertPrivacySession(sessionContext);
  } finally {
    globalThis.clearInterval(timer);
    if (heartbeatPromise) await heartbeatPromise;
  }
  assertPrivacySession(sessionContext);

  if (heartbeatError) {
    const records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
    assertPrivacySession(sessionContext);
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
  const sessionContext = data?.privacySessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = data?.reservationManager;
  const reservationIDs = preparedReservationIDs(data);
  if (!manager || !reservationIDs.length) return;
  const currentManager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (currentManager !== manager) throw stalePrivacySessionError(sessionContext);
  await manager.markReplanRequired(reservationIDs, {
    leaseToken: data.reservation?.lease_token || data.reservation?.reservations?.[0]?.lease_token || "",
    error: reason,
    metadata: {
      reconcile_reason: reason,
      no_broadcast_attempt: true,
      proof_discarded: true
    }
  });
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport === "evm"
    && data?.reservationKind !== "batch-transfer") {
    const operationKey = preparedOperationKey(data);
    if (operationKey) {
      await clearEvmOperationArtifact(operationKey, { sessionContext });
      assertPrivacySession(sessionContext);
    }
  }
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
}

function stopRelayReservationHeartbeat(expectedGeneration = null) {
  if (expectedGeneration !== null && expectedGeneration !== relayReservationHeartbeatGeneration) {
    return false;
  }
  relayReservationHeartbeatGeneration += 1;
  if (relayReservationHeartbeatTimer !== null) {
    globalThis.clearInterval(relayReservationHeartbeatTimer);
    relayReservationHeartbeatTimer = null;
  }
  return true;
}

function startRelayReservationHeartbeat({
  manager,
  reservationIDs,
  leaseToken,
  leaseUntil,
  sessionContext = privacySessionSnapshot()
}) {
  if (!manager || !reservationIDs.length || !leaseToken) return;
  const expectedReservationIDs = JSON.stringify(reservationIDs);
  try {
    assertPrivacySession(sessionContext);
  } catch {
    return;
  }
  if (manager !== reservationManager
    || state.relayWithdraw.leaseToken !== leaseToken
    || JSON.stringify(state.relayWithdraw.reservationIds || []) !== expectedReservationIDs) {
    return;
  }
  stopRelayReservationHeartbeat();
  const generation = relayReservationHeartbeatGeneration;
  const heartbeatCurrent = () => {
    if (generation !== relayReservationHeartbeatGeneration) return false;
    try {
      assertPrivacySession(sessionContext);
    } catch {
      return false;
    }
    return state.relayWithdraw.leaseToken === leaseToken
      && JSON.stringify(state.relayWithdraw.reservationIds || []) === expectedReservationIDs;
  };
  const heartbeat = async () => {
    if (!heartbeatCurrent()) return;
    try {
      await manager.heartbeatLease(reservationIDs, { leaseToken });
    } catch (error) {
      if (!heartbeatCurrent()) return;
      const records = await Promise.allSettled(reservationIDs.map(id => manager.getReservation(id)));
      if (!heartbeatCurrent()) return;
      const stillProofReady = records.some(result => result.status === "fulfilled"
        && result.value.status === reservationStatuses.ProofReady);
      if (!stopRelayReservationHeartbeat(generation)) return;
      const failureGeneration = relayReservationHeartbeatGeneration;
      const failureCurrent = () => {
        if (failureGeneration !== relayReservationHeartbeatGeneration) return false;
        try {
          assertPrivacySession(sessionContext);
        } catch {
          return false;
        }
        return state.relayWithdraw.leaseToken === leaseToken
          && JSON.stringify(state.relayWithdraw.reservationIds || []) === expectedReservationIDs;
      };
      if (!stillProofReady) return;
      if (!failureCurrent()) return;
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = `Relay reservation heartbeat failed · ${error.message}`;
      await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext }).catch(() => {});
      if (!failureCurrent()) return;
      try {
        await refreshReservationState(manager, { sessionContext });
      } catch {
        return;
      }
      if (!failureCurrent()) return;
      renderRelayWithdraw();
    }
  };
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
  const operations = groupReservationOperations(reconciliationReservationRecords(
    state.reservations.active,
    state.reservations.unresolved
  ));
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
  const privacyPending = Boolean(state.keplr.cosmosPrivacyPendingHash);
  const privacyPendingNeedsSetup = privacyPending && !state.keplr.rootSignatureBase64;
  const privacyPendingOnly = privacyPending
    && !state.reservations.active.length
    && !state.reservations.unresolved.length;
  els.reservationState.textContent = privacyPendingNeedsSetup
    ? `Unresolved privacy tx ${shorten(state.keplr.cosmosPrivacyPendingHash, 14, 12)} · Setup Clairveil and Reconcile`
    : privacyPendingOnly
      ? `Unresolved privacy tx ${shorten(state.keplr.cosmosPrivacyPendingHash, 14, 12)} · Reconcile chain evidence`
      : state.reservations.message;
  els.reservationState.dataset.status = reservationStatusSlug(
    privacyPending ? reservationStatuses.Unknown : state.reservations.status
  );
  const canReconcile = canReconcileReservationState({
    privacyReady: Boolean(state.keplr.rootSignatureBase64),
    active: state.reservations.active,
    unresolved: state.reservations.unresolved,
    privacyPending,
    reconciling: state.reservations.reconciling
  });
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
  return records.filter(reservationHasUnresolvedOperationEvidence);
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
  const txHash = primaryRecords.map(reservationTransactionHash).find(Boolean) || "";
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

async function refreshReservationState(manager = null, {
  sessionContext = null,
  notes = null
} = {}) {
  if (sessionContext) assertPrivacySession(sessionContext);
  const resolvedManager = manager || await currentReservationManager();
  if (sessionContext) assertPrivacySession(sessionContext);
  if (!resolvedManager) {
    if (sessionContext) assertPrivacySession(sessionContext);
    state.reservations = defaultReservationState();
    renderReservationState();
    return [];
  }
  try {
    const reservationNotes = notes || state.keplr.notes;
    const [active, allReservations, noteStatuses] = await Promise.all([
      resolvedManager.listActiveReservations(),
      resolvedManager.store.listReservations({ ownerKeyId: resolvedManager.ownerKeyId }),
      resolvedManager.reservationStatusByNote(reservationNotes)
    ]);
    if (sessionContext) assertPrivacySession(sessionContext);
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
    if (isStalePrivacySessionError(error)) throw error;
    if (sessionContext) assertPrivacySession(sessionContext);
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

function withdrawEventMatchesOperation(event, records, notesByLookupKey) {
  const nullifiers = records
    .map(record => noteNullifier(notesByLookupKey.get(record.nullifier_lookup_key)))
    .filter(Boolean);
  if (event?.event_type !== "withdraw" || nullifiers.length !== records.length || nullifiers.length !== 1) {
    return false;
  }
  return normalizedHex(eventAttribute(event, "nullifier")) === normalizedHex(nullifiers[0]);
}

function operationEventMatches(event, records, notesByLookupKey) {
  return transferEventMatchesOperation(event, records, notesByLookupKey)
    || withdrawEventMatchesOperation(event, records, notesByLookupKey);
}

function operationEventForOperation(records, notesByLookupKey, txHash = "") {
  const expectedTxHash = normalizedTxHash(txHash);
  return state.privacyEvents.events.find(event => (
    (!expectedTxHash || normalizedTxHash(event?.tx_hash_hex) === expectedTxHash)
      && operationEventMatches(event, records, notesByLookupKey)
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
  const txHash = commonReservationTransactionHash(records);
  if (!txHash) return { complete: true, event: null };
  const local = operationEventForOperation(records, notesByLookupKey, txHash);
  if (local?.event_type === "shielded_transfer") {
    return {
      complete: true,
      event: local,
      operationSuccessEvidence: operationEvidenceFromEvent(records, local)
    };
  }

  const check = await checkReservationTransaction(txHash);
  if (check.pending || check.absent || (!check.included && !check.failed)) {
    return { complete: false, event: null };
  }
  if (!check.included || !check.successful) return { complete: true, event: null };
  const height = authoritativeTransactionHeight(check);
  if (!height) throw new Error(`Included transaction ${txHash} has no authoritative height`);
  const event = local || await findPrivacyEventByTxHash({
    fetchPage: options => clairveilBrowserClient().fetchPrivacyEvents(options),
    txHash,
    height,
    eventTypes: reservationPrivacyEventTypes(records),
    predicate: candidate => operationEventMatches(candidate, records, notesByLookupKey)
  });
  let operationSuccessEvidence = null;
  if (event?.event_type === "shielded_transfer") {
    operationSuccessEvidence = operationEvidenceFromEvent(records, event);
  } else if (event?.event_type === "withdraw" && activeChainProfile()?.transport === "cosmos") {
    operationSuccessEvidence = operationEvidenceWithReservationTransactionIdentity(records, txHash, cosmosWithdrawOperationEvidence({
      event,
      transaction: check.transaction,
      txHash,
      expectedNullifiers: records.map(record => (
        noteNullifier(notesByLookupKey.get(record.nullifier_lookup_key))
      )),
      accountPrefix: accountPrefix()
    }));
  }
  return { complete: true, event, operationSuccessEvidence };
}

function operationEvidenceWithReservationTransactionIdentity(records, txHash, evidence = {}) {
  const normalized = normalizedHex(txHash);
  if (!normalized) return null;
  let transactionIdentity;
  if (activeChainProfile()?.transport !== "cosmos") {
    transactionIdentity = { txHash: normalized };
  } else {
    const submittedHashes = records.map(record => normalizedHex(record?.submitted_tx_hash));
    if (submittedHashes.length > 0 && submittedHashes.every(hash => hash === normalized)) {
      transactionIdentity = { txHash: normalized };
    } else if (submittedHashes.every(hash => !hash)
      && commonCosmosReservationTransactionHash(records) === normalized) {
      transactionIdentity = { txBytesHash: normalized };
    }
  }
  if (!transactionIdentity) return null;
  const {
    txHash: _txHash,
    tx_hash: _txHashSnake,
    txBytesHash: _txBytesHash,
    tx_bytes_hash: _txBytesHashSnake,
    ...operationEvidence
  } = evidence || {};
  return { ...operationEvidence, ...transactionIdentity };
}

function operationEvidenceFromEvent(records, event) {
  const first = records[0];
  return operationEvidenceWithReservationTransactionIdentity(records, event?.tx_hash_hex, {
    outputCommitment: normalizedHex(eventAttribute(event, "commitment_1")),
    auditDisclosureDigest: normalizedHex(eventAttribute(event, "audit_disclosure_digest")),
    recipientHash: first.expected_recipient_hash,
    amount: first.expected_amount,
    amountHash: first.expected_amount_hash,
    denom: first.expected_denom,
    batchItemIndex: first.batch_item_index,
    batchItemIndexKnown: first.batch_item_index_known
  });
}

function reservationExecutionTransport(records = []) {
  const transports = [...new Set((records || []).map(record => String(
    record?.metadata?.execution_transport
      || record?.metadata?.executionTransport
      || ""
  ).trim().toLowerCase()).filter(Boolean))];
  if (transports.length > 1) {
    throw new Error("Reservations disagree about their execution transport");
  }
  return transports[0] || "cosmos";
}

function isRelayWithdrawReservation(records = []) {
  return (records || []).some(record => (
    String(record?.kind || "").toLowerCase().includes("relay")
      || record?.metadata?.relay_handed_off === true
      || record?.metadata?.relayHandedOff === true
  ));
}

async function directEvmReceiptEvidenceForReservations(records = [], {
  sessionContext = privacySessionSnapshot(),
  assertCurrent = null
} = {}) {
  const assertFresh = () => {
    assertPrivacySession(sessionContext);
    assertCurrent?.();
  };
  assertFresh();
  const artifact = await loadEvmOperationArtifactForReservations(records, { sessionContext });
  assertFresh();
  if (!artifact?.transaction || !artifact?.sender) {
    return { complete: false, evidence: null };
  }
  const txHashes = [...new Set(records
    .map(record => String(record?.submitted_tx_hash || "").trim())
    .filter(Boolean))];
  if (txHashes.length !== 1) return { complete: false, evidence: null };
  let receiptResult = artifact.receiptResult || null;
  if (!receiptResult) {
    const result = await waitForEvmTransaction(
      txHashes[0],
      "EVM privacy operation recovery",
      { sessionContext },
      artifact.transaction,
      artifact.sender
    );
    assertFresh();
    if (result.unknown) return { complete: false, evidence: null };
    receiptResult = result;
    await saveEvmOperationArtifact(artifact.operationKey, {
      ...artifact,
      phase: "receipt-verified",
      txHash: result.txHash,
      receiptResult: result
    }, { sessionContext });
    assertFresh();
  }
  const evidence = records.some(reservationRequiresOperationEvidence)
    ? directEvmOperationSuccessEvidence(records, receiptResult)
    : null;
  verifiedEvmTransactionResult(receiptResult, "EVM privacy operation recovery");
  return { complete: true, evidence };
}

async function clearTerminalDirectEvmOperationArtifacts(records = [], {
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport !== "evm") return;
  const terminalStatuses = new Set([
    reservationStatuses.ConfirmedSpent,
    reservationStatuses.ReplanRequired,
    reservationStatuses.Released,
    reservationStatuses.Failed
  ]);
  for (const operation of groupReservationOperations(records)) {
    const operationRecords = operation.records || [];
    if (!operationRecords.length
      || isBatchReservationOperation(operationRecords)
      || isRelayWithdrawReservation(operationRecords)
      || reservationExecutionTransport(operationRecords) !== "evm"
      || operationRecords.some(record => !terminalStatuses.has(record.status))) {
      continue;
    }
    const operationKey = reservationGroupOperationKey(operationRecords);
    if (operationKey) {
      await clearEvmOperationArtifact(operationKey, { sessionContext });
      assertPrivacySession(sessionContext);
    }
  }
}

async function reconcileSpentReservations(manager, notes = state.keplr.notes, {
  sessionContext = null,
  assertCurrent = null
} = {}) {
  const assertFresh = () => {
    if (sessionContext) assertPrivacySession(sessionContext);
    assertCurrent?.();
  };
  assertFresh();
  if (!manager) return [];
  const spent = (notes || []).filter(noteHasSpentEvidence);
  if (!spent.length) return [];
  const [active, allReservations] = await Promise.all([
    manager.listActiveReservations(),
    manager.store.listReservations({ ownerKeyId: manager.ownerKeyId })
  ]);
  assertFresh();
  const activeIDs = new Set(active.map(record => record.reservation_id));
  const candidates = allReservations.filter(record => activeIDs.has(record.reservation_id)
    || (reservationRequiresOperationEvidence(record) && [
      operationStatuses.ManualReview,
      operationStatuses.ConflictSpent
    ].includes(operationReconciliationStatus(record))));
  const notesByLookupKey = new Map();
  for (const note of spent) {
    notesByLookupKey.set(await manager.lookupKeyForNote(note), note);
    assertFresh();
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
    const executionTransport = reservationExecutionTransport(records);
    if (isBatchReservationOperation(records)) {
      if (spentRecords.length !== records.length) continue;
      const operationSuccessEvidence = executionTransport === "evm"
        ? await batchReceiptEvidenceForReservations(records, {
            sessionContext: sessionContext || privacySessionSnapshot()
          })
        : executionTransport === "cosmos"
          ? await cosmosBatchEvidenceForReservations(records, {
              sessionContext: sessionContext || privacySessionSnapshot()
            })
          : null;
      assertFresh();
      if (!operationSuccessEvidence) continue;
      for (const record of spentRecords) {
        eligibleLookupKeys.add(record.nullifier_lookup_key);
        evidenceByLookupKey.set(record.nullifier_lookup_key, operationSuccessEvidence);
      }
      continue;
    }
    if (executionTransport === "evm" && !isBatchReservationOperation(records) && !isRelayWithdrawReservation(records)) {
      if (spentRecords.length !== records.length) continue;
      const recovered = await directEvmReceiptEvidenceForReservations(records, {
        sessionContext: sessionContext || privacySessionSnapshot(),
        assertCurrent
      });
      assertFresh();
      if (!recovered.complete) continue;
      for (const record of spentRecords) {
        eligibleLookupKeys.add(record.nullifier_lookup_key);
        if (recovered.evidence) evidenceByLookupKey.set(record.nullifier_lookup_key, recovered.evidence);
      }
      continue;
    }
    if (!records.some(reservationRequiresOperationEvidence)) {
      spentRecords.forEach(record => eligibleLookupKeys.add(record.nullifier_lookup_key));
      continue;
    }
    if (spentRecords.length !== records.length) continue;
    const lookup = await operationEventForReservations(records, notesByLookupKey);
    assertFresh();
    if (!lookup.complete) continue;
    const operationSuccessEvidence = lookup.operationSuccessEvidence || null;
    for (const record of records) {
      eligibleLookupKeys.add(record.nullifier_lookup_key);
      if (operationSuccessEvidence) {
        evidenceByLookupKey.set(record.nullifier_lookup_key, operationSuccessEvidence);
      }
    }
  }
  // A scan can finish after another reconciliation already persisted this
  // operation as succeeded. Do not replay its event evidence into the SDK:
  // success is terminal and the SDK correctly rejects altered retry evidence.
  const latestReservations = await manager.store.listReservations({ ownerKeyId: manager.ownerKeyId });
  assertFresh();
  const terminalSucceededLookupKeys = succeededOperationLookupKeys(latestReservations);
  const eligible = [...notesByLookupKey.entries()]
    .filter(([lookupKey]) => eligibleLookupKeys.has(lookupKey))
    .filter(([lookupKey]) => !terminalSucceededLookupKeys.has(lookupKey))
    .map(([lookupKey, note]) => ({
      ...note,
      spent: true,
      isSpent: true,
      nullifierStatus: "spent",
      ...(evidenceByLookupKey.has(lookupKey)
        ? { operationSuccessEvidence: evidenceByLookupKey.get(lookupKey) }
        : {})
    }));
  if (!eligible.length) return [];
  assertFresh();
  const reconciled = await manager.reconcileSpentNotes(eligible);
  assertFresh();
  const reconciledByID = new Map((reconciled || []).map(record => [record.reservation_id, record]));
  for (const records of groups.values()) {
    const latest = records.map(record => reconciledByID.get(record.reservation_id) || record);
    const succeeded = latest.length > 0 && latest.every(record => (
      record.status === reservationStatuses.ConfirmedSpent
        && (!reservationRequiresOperationEvidence(record)
          || operationReconciliationStatus(record) === operationStatuses.Succeeded)
    ));
    if (!succeeded) continue;
    if (isBatchReservationOperation(latest)) {
      await clearBatchTransferArtifact({
        records: latest,
        sessionContext: sessionContext || privacySessionSnapshot()
      });
    } else if (reservationExecutionTransport(latest) === "evm"
      && !isRelayWithdrawReservation(latest)) {
      const operationKey = reservationGroupOperationKey(latest);
      if (operationKey) {
        await clearEvmOperationArtifact(operationKey, {
          sessionContext: sessionContext || privacySessionSnapshot()
        });
      }
    }
    assertFresh();
  }
  return reconciled;
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
  resetPrivatePendingState: $("#resetPrivatePendingState"),
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
  batchTransferSection: $("#batchTransferSection"),
  batchTransferDescription: $("#batchTransferDescription"),
  batchTransferRows: $("#batchTransferRows"),
  batchAuthorizationPanel: $("#batchAuthorizationPanel"),
  batchUseAuthorization: $("#batchUseAuthorization"),
  batchAuthorizationOptions: $("#batchAuthorizationOptions"),
  batchAuthorizationKind: $("#batchAuthorizationKind"),
  batchAuthorizationAccount: $("#batchAuthorizationAccount"),
  addBatchTransferPayment: $("#addBatchTransferPayment"),
  transferBatchFromVeiled: $("#transferBatchFromVeiled"),
  batchTransferHash: $("#batchTransferHash"),
  veiledWithdrawAmount: $("#veiledWithdrawAmount"),
  veiledWithdrawRecipient: $("#veiledWithdrawRecipient"),
  veiledWithdrawRecipientSuggestions: $("#veiledWithdrawRecipientSuggestions"),
  withdrawFromVeiled: $("#withdrawFromVeiled"),
  relayWithdrawAmount: $("#relayWithdrawAmount"),
  relayWithdrawRecipient: $("#relayWithdrawRecipient"),
  relayWithdrawRecipientSuggestions: $("#relayWithdrawRecipientSuggestions"),
  relayWithdrawFromVeiled: $("#relayWithdrawFromVeiled"),
  relayWithdrawState: $("#relayWithdrawState"),
  relayWithdrawRecoveryChoiceField: $("#relayWithdrawRecoveryChoiceField"),
  relayWithdrawRecoveryChoice: $("#relayWithdrawRecoveryChoice"),
  relayWithdrawChain: $("#relayWithdrawChain"),
  relayWithdrawPreparedRecipient: $("#relayWithdrawPreparedRecipient"),
  relayWithdrawExpiry: $("#relayWithdrawExpiry"),
  relayWithdrawPayloadHash: $("#relayWithdrawPayloadHash"),
  relayWithdrawJson: $("#relayWithdrawJson"),
  relayWithdrawTxHash: $("#relayWithdrawTxHash"),
  relayWithdrawTxHashDisplay: $("#relayWithdrawTxHashDisplay"),
  relayWithdrawSubmittedBy: $("#relayWithdrawSubmittedBy"),
  relayWithdrawResult: $("#relayWithdrawResult"),
  reconcileRelayWithdraw: $("#reconcileRelayWithdraw"),
  copyRelayWithdraw: $("#copyRelayWithdraw"),
  downloadRelayWithdraw: $("#downloadRelayWithdraw"),
  relayPreparedWithdraw: $("#relayPreparedWithdraw"),
  relayerTransparentAddress: $("#relayerTransparentAddress"),
  relayerBalance: $("#relayerBalance"),
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

function invalidateActivePrivacyFlow() {
  transferFlowState.controller?.abort();
  closeTransferFlowModal(false);
  transferFlowState.confirmationStage = "initial";
  transferFlowState.copy = null;
  transferFlowState.retry = null;
  transferFlowState.review = null;
  relayHandoffInFlight = false;
  batchTransferInFlight = false;
  valueMovingActionGate.invalidate();
  for (const action of [
    els.fundKeplr,
    els.sendFromKeplr,
    els.depositFromKeplr,
    els.transferFromVeiled,
    els.withdrawFromVeiled,
    els.relayWithdrawFromVeiled,
    els.relayPreparedWithdraw,
    els.transferBatchFromVeiled
  ]) {
    if (action) setBusy(action, false);
  }
}

function runValueMovingAction(action, task) {
  const operation = valueMovingActionGate.run(action, () => {
    renderKeplr();
    return task();
  });
  renderKeplr();
  return operation.finally(() => renderKeplr());
}

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
  batchTransfer: {
    title: "Atomic Batch Transfer 확인",
    lead: "모든 수신자·금액·payment별 disclosure를 proof 하나와 transaction 하나로 준비합니다. 이 batch는 all-or-nothing입니다.",
    runningLead: "proof를 준비하고 있습니다. 완료되면 연결된 wallet에서 batch transaction을 확인해 주세요.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "Batch transaction을 완료하지 못했습니다.",
    stepOneTitle: "Batch proof 준비",
    stepOneCopy: "선택된 input notes와 모든 batch output을 하나의 proof에 결합합니다.",
    stepTwoTitle: "Batch 서명",
    stepTwoCopy: "준비된 단일 privacy transaction을 연결된 wallet에서 확인하고 서명합니다.",
    successTitle: "Batch transfer 요청이 성공하였습니다",
    successCopy: "최신 notes와 batch operation evidence를 다시 확인합니다.",
    failureTitle: "Batch transfer 요청이 실패했습니다"
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
    this.txHash = String(data?.txHash || "").trim();
    this.txCode = data?.txCode;
    this.checkTxRejected = data?.checkTxRejected === true;
    this.rpcInvoked = data?.rpcInvoked === true;
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

function preparedSelfMergeReview(data, {
  selfView = "Encrypted self-view included"
} = {}) {
  const prepared = data?.prepared || {};
  const recipient = prepared.recipient || prepared.shieldedAddress || state.keplr.shieldedAddress;
  const amount = coinText(prepared.amount);
  const selectedInputTotal = coinText(prepared.selectedInputTotal || prepared.amount);
  const inputCount = Array.isArray(data?.plan?.selection?.inputs)
    ? data.plan.selection.inputs.length
    : 0;
  const inputLabel = inputCount > 0 ? `${inputCount} input note${inputCount === 1 ? "" : "s"}` : "Selected input notes";
  return {
    chainId: data?.privacySessionContext?.chainId || activeChainProfile()?.chainId,
    recipient,
    amount,
    disclosure: "All-private self-merge · user disclosure disabled",
    selfView,
    changeEffect: `${inputLabel} totaling ${selectedInputTotal} → one ${amount} self note; the final recipient is not paid in this step.`,
    expiresAtUnix: preparedTransferExpiryUnix(data)
  };
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

function requestPreparedSelfMergeConfirmation(review) {
  transferFlowState.confirmationStage = "self_merge";
  transferFlowState.running = false;
  renderTransferReview(review);
  els.transferSteps.hidden = true;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferModalState.textContent = "Self transaction 확인 필요";
  els.transferModalLead.textContent = "Planner가 준비한 self-merge의 입력 합계, 새 self note 금액, 내 shielded recipient를 확인한 뒤 wallet 서명을 진행하세요.";
  els.cancelTransferFlow.textContent = "취소";
  els.cancelTransferFlow.disabled = false;
  els.cancelTransferFlow.hidden = false;
  els.confirmTransferFlow.textContent = "Self transaction 승인";
  els.confirmTransferFlow.disabled = false;
  els.confirmTransferFlow.hidden = false;
  els.confirmTransferFlow.focus();
  return new Promise(resolve => {
    transferFlowState.resolve = resolve;
  });
}

async function confirmPreparedSelfMerge(data, review) {
  const sessionContext = data?.privacySessionContext || privacySessionSnapshot();
  return withPreparedReservationHeartbeat(data, async () => {
    const approved = await requestPreparedSelfMergeConfirmation(review);
    assertPrivacySession(sessionContext);
    if (!approved) {
      await discardPreparedReservation(data, "user_cancelled_self_merge_before_broadcast");
      assertPrivacySession(sessionContext);
    }
    return approved;
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

function paddedEvmGas(estimated) {
  return (estimated * 13n + 9n) / 10n;
}

function evmRpcTransaction(transaction = {}, { from = "" } = {}) {
  const request = {};
  for (const key of [
    "from", "to", "data", "value", "gas", "gasPrice",
    "maxFeePerGas", "maxPriorityFeePerGas", "nonce", "chainId", "type", "accessList"
  ]) {
    if (transaction?.[key] != null) request[key] = transaction[key];
  }
  if (from) request.from = from;
  return request;
}

async function withEstimatedEvmGas(transaction) {
  const tx = evmRpcTransaction(transaction);
  try {
    const estimateRequest = { ...tx };
    delete estimateRequest.gas;
    const estimated = evmQuantityToBigInt(await requestMetaMask({
      method: "eth_estimateGas",
      params: [estimateRequest]
    }), "estimated gas");
    const padded = paddedEvmGas(estimated);
    tx.gas = bigIntToEvmQuantity(padded);
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
    const fee = cosmosGasFeeEstimate(cosmosGasLimits.deposit);
    state.keplr.networkFeeAmount = fee.toString();
    state.keplr.networkFeeEstimate = `≈ ${fee}${baseDenom()} · gas limit ${cosmosGasLimits.deposit.toLocaleString()} · deterministic fee`;
    renderKeplr();
    return fee;
  }
  try {
    const request = evmRpcTransaction(transaction, { from: state.wallet.account });
    delete request.gas;
    const [gasHex, gasPriceHex] = await Promise.all([
      requestMetaMask({ method: "eth_estimateGas", params: [request] }),
      requestMetaMask({ method: "eth_gasPrice" })
    ]);
    const estimatedGas = evmQuantityToBigInt(gasHex, "estimated gas");
    const gas = paddedEvmGas(estimatedGas);
    const gasPrice = evmQuantityToBigInt(gasPriceHex, "gas price");
    const fee = gas * gasPrice;
    state.keplr.networkFeeAmount = fee.toString();
    state.keplr.networkFeeEstimate = `≈ ${formatEvmNetworkFee(fee)} · gas ${gas} (estimate + 30%)`;
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
  return cosmosGasFeeAmount(activeChainProfile()?.gasPriceStep?.average, gasLimit);
}

function cosmosFeeRequestOptions(gasLimit) {
  if (activeChainProfile()?.transport !== "cosmos") return {};
  return {
    gasLimit,
    feeAmount: deterministicCosmosFeeAmount({
      gasPrice: activeChainProfile()?.gasPriceStep?.average,
      gasLimit,
      denom: baseDenom()
    })
  };
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
  // A privacy proof is required before an EVM node can estimate this call.
  // Do not reject a valid deposit against the profile's emergency gas ceiling;
  // the exact estimate and the final funding check run immediately afterward.
  state.keplr.networkFeeAmount = "0";
  state.keplr.networkFeeEstimate = "Fee will be estimated after the deposit proof is prepared";
  renderKeplr();
  return 0n;
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

function attachSubmittedEvmTransactionEvidence(error, txHash) {
  const failure = error instanceof Error ? error : new Error(String(error));
  failure.txHash ||= txHash;
  failure.evmTxHash ||= txHash;
  return failure;
}

function isExplicitWalletRejection(error) {
  return String(error?.code ?? error?.data?.code ?? "") === "4001";
}

function runSynchronousWalletBoundaryCallback(name, callback, ...args) {
  if (callback == null) return undefined;
  if (typeof callback !== "function") throw new TypeError(`${name} must be a function`);
  const result = callback(...args);
  if (result != null
    && (typeof result === "object" || typeof result === "function")
    && typeof result.then === "function") {
    void Promise.resolve(result).catch(() => {});
    throw new TypeError(`${name} must be synchronous`);
  }
  return result;
}

function assertPrivacySessionAfterEvmSubmission(sessionContext, txHash) {
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    throw attachSubmittedEvmTransactionEvidence(error, txHash);
  }
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
  return `${formatUclairAsClair(total)} · ${available.length} available${reservedText}${helperText}`;
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

function reconcilePendingDepositRecoveryFromTypedNotes() {
  if (![
    "pending",
    "recovering",
    "recovery-pending",
    "submitted",
    "unknown",
    "checking"
  ].includes(state.keplr.depositRecoveryStatus) || !state.keplr.depositHash) {
    return null;
  }
  let recovered;
  try {
    recovered = recoveredDepositNoteForTxHash(state.keplr.notes, state.keplr.depositHash);
    const expectedCommitment = normalizedHex(state.keplr.depositPrepared?.noteCommitmentHex);
    if (recovered && expectedCommitment
      && normalizedHex(noteCommitment(recovered)) !== expectedCommitment) {
      return null;
    }
  } catch {
    return null;
  }
  if (!recovered) return null;
  return {
    txHash: state.keplr.depositHash,
    height: recovered.height || ""
  };
}

async function finalizePendingDepositRecoveryFromTypedNotes(recovery, {
  sessionContext,
  accountTransactionLockHeld = false
} = {}) {
  if (!recovery?.txHash) return false;
  const finalize = () => {
    assertPrivacySession(sessionContext);
    if (normalizedHex(state.keplr.depositHash) !== normalizedHex(recovery.txHash)) {
      return false;
    }
    const recovered = recoveredDepositNoteForTxHash(state.keplr.notes, recovery.txHash);
    const expectedCommitment = normalizedHex(state.keplr.depositPrepared?.noteCommitmentHex);
    if (!recovered || (expectedCommitment
      && normalizedHex(noteCommitment(recovered)) !== expectedCommitment)) {
      return false;
    }
    clearCapturedPublicPendingTransaction(sessionContext, "deposit", recovery.txHash);
    state.keplr.depositRecoveryStatus = "recovered";
    state.keplr.depositRecoveryMessage = "Recovered · encrypted note matched the exact included tx hash";
    state.keplr.depositHeight = recovered.height || recovery.height || state.keplr.depositHeight;
    return true;
  };
  return accountTransactionLockHeld
    ? finalize()
    : withPublicTransactionLock(sessionContext, finalize);
}

function noteScanRequestOptions({ reset = false, maxPages = 5 } = {}) {
  const cursor = reset ? defaultNoteScanCursor() : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const hasMore = !reset && Boolean(cursor.has_more ?? cursor.hasMore);
  if (hasMore && state.keplr.noteScanResumeOptions) {
    return {
      ...state.keplr.noteScanResumeOptions,
      scanSource: "privacy_scan",
      strictPrivacyScan: true,
      maxPages,
      includeFoundNotes: true
    };
  }
  const freshTypedScan = reset || (
    String(cursor.source || "") === "privacy_scan"
      && !hasMore
      && Number(cursor.pages_scanned ?? cursor.pagesScanned ?? 0) === 0
      && cursor.completed !== true
  );
  return {
    ...(freshTypedScan ? { after: typedPrivacyScanAfter(cursor) } : {}),
    scanSource: "privacy_scan",
    strictPrivacyScan: true,
    limit: 200,
    outputLimit: 200,
    maxPages,
    includeFoundNotes: true
  };
}

async function applyNoteScanResult(data, {
  reset = false,
  sessionContext = privacySessionSnapshot(),
  reservationManager: scanReservationManager = null,
  noteStore: scanNoteStore = undefined
} = {}) {
  assertPrivacySession(sessionContext);
  const store = scanNoteStore === undefined ? await currentNoteStore() : scanNoteStore;
  assertPrivacySession(sessionContext);
  if (!store || typeof store.load !== "function") {
    throw new Error("Encrypted note storage is required before applying a privacy scan");
  }
  const stored = await store.load();
  assertPrivacySession(sessionContext);
  const cursor = data?.scanCursor || data?.scan_cursor || stored?.scanCursor || defaultNoteScanCursor();
  if (cursor?.source !== "privacy_scan") {
    const error = new Error("Typed privacy-scan-v2 is required; legacy scan results are not accepted by this WebApp");
    error.code = "TYPED_PRIVACY_SCAN_REQUIRED";
    throw error;
  }
  if (!Array.isArray(stored?.notes)) {
    throw new Error("Encrypted note storage returned an invalid note inventory");
  }
  const scannedNotes = stored.notes;
  assertPrivacySession(sessionContext);
  state.keplr.notes = scannedNotes;
  const pendingDepositRecovery = reconcilePendingDepositRecoveryFromTypedNotes();
  state.keplr.noteScanCursor = cursor;
  state.keplr.noteScanResumeOptions = data?.nextScanOptions || data?.next_scan_options || null;
  const moreText = Boolean(cursor.has_more ?? cursor.hasMore) ? " · more events queued" : "";
  state.keplr.notesSummary = `${summarizeSpendableValueNotes(state.keplr.notes)}${moreText}`;
  state.keplr.notesScanned = true;
  state.keplr.noteSyncStatus = Boolean(cursor.has_more ?? cursor.hasMore) ? "partial" : "synced";
  state.keplr.noteSyncMessage = Boolean(cursor.has_more ?? cursor.hasMore)
    ? "Encrypted cache updated · more events queued"
    : "Encrypted cache synced";
  const manager = scanReservationManager || await currentReservationManager();
  assertPrivacySession(sessionContext);
  await reconcileSpentReservations(manager, scannedNotes, { sessionContext });
  assertPrivacySession(sessionContext);
  await refreshReservationState(manager, { sessionContext, notes: scannedNotes });
  assertPrivacySession(sessionContext);
  return pendingDepositRecovery;
}

function selectedLocalAccount() {
  const accounts = activeServerAccounts();
  return accounts.find(account => account.name === state.selectedAccount) || accounts[0];
}

function localAccountRequestIdentity(account = selectedLocalAccount()) {
  if (!account) return "";
  return JSON.stringify({
    selectedName: String(state.selectedAccount || ""),
    name: String(account.name || ""),
    transparentAddress: String(account.transparentAddress || ""),
    evmAddress: String(account.evmAddress || ""),
    profileIdentity: activeProfileSessionIdentity()
  });
}

function assertLocalAccountRequestCurrent(sessionContext, expectedIdentity) {
  assertPrivacySession(sessionContext);
  if (!expectedIdentity || localAccountRequestIdentity() !== expectedIdentity) {
    throw stalePrivacySessionError(sessionContext);
  }
}

function activeServerAccounts() {
  return serverFeature("localSigners") && selectedProfileMatchesServer() ? state.accounts : [];
}

function localRelayerAccount() {
  if (!serverFeature("relayer") || !selectedProfileMatchesServer()) return null;
  const preferred = activeChainProfile()?.transport === "evm" ? "dev0" : "relayer";
  return state.accounts.find(account => account.name === preferred) || null;
}

async function refreshRelayerAccount({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const relayer = localRelayerAccount();
  const relayerIdentity = JSON.stringify({
    profileIdentity: activeProfileSessionIdentity(),
    name: String(relayer?.name || ""),
    transparentAddress: String(relayer?.transparentAddress || "")
  });
  const assertCurrent = () => {
    assertPrivacySession(sessionContext);
    const current = localRelayerAccount();
    if (JSON.stringify({
      profileIdentity: activeProfileSessionIdentity(),
      name: String(current?.name || ""),
      transparentAddress: String(current?.transparentAddress || "")
    }) !== relayerIdentity) {
      throw stalePrivacySessionError(sessionContext);
    }
  };
  if (!relayer?.transparentAddress) {
    assertCurrent();
    state.relayer.balance = "";
    state.relayer.error = "";
    renderRelayWithdraw();
    return;
  }
  try {
    const balance = await clairveilBrowserClient().getBalances(relayer.transparentAddress);
    assertCurrent();
    state.relayer.balance = (balance.balances || [])
      .map(coin => `${coin.amount}${coin.denom}`)
      .join(", ") || zeroCoinText();
    state.relayer.error = "";
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertCurrent();
    state.relayer.balance = "";
    state.relayer.error = error.message;
  }
  renderRelayWithdraw();
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
  els.noteScanEndpoint.disabled = endpoints.length < 2
    || noteStoreCoordinator.pending > 0
    || publicTransactionCoordinator.pending > 0
    || valueMovingActionGate.active;
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
  if (noteStoreCoordinator.pending > 0
    || publicTransactionCoordinator.pending > 0
    || valueMovingActionGate.active) {
    throw new Error("Wait for the active scan, privacy action, or account transaction before changing the REST endpoint");
  }
  if (state.selectedRestEndpointByProfile[profile.id] === endpoint) return;
  invalidatePrivacySession();
  protocolRequestGeneration += 1;
  state.selectedRestEndpointByProfile[profile.id] = endpoint;
  browserClient = null;
  browserClientKey = "";
  browserClientDepositProofProvider = null;
  browserClientEvmContractAdapter = null;
  browserClientPrivacyStateAdapter = null;
  browserClientEvmFinalityPolicy = null;
  state.protocol.ready = false;
  state.protocol.error = "";
  renderNoteScanEndpoint();
  renderProtocolStatus();
  renderMyKeplrNotes();
  updateAmountActionButtons();
  refreshProtocolStatus().catch(error => {
    state.protocol.error = browserDataLoadErrorMessage(error);
    renderProtocolStatus();
    renderMyKeplrNotes();
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
  renderBatchTransfer();
}

function selectDappChainProfile(profileId) {
  invalidatePrivacySession();
  healthRequestGeneration += 1;
  protocolRequestGeneration += 1;
  if (state.activeWallet) {
    resetWalletSession();
  }
  state.selectedChainProfileId = profileId;
  resetProfileScopedAddressBook(addressBookScopeIdentity());
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

function addressBookScopeIdentity() {
  return JSON.stringify({
    profile: activeProfileSessionIdentity(),
    storageEpoch: String(state.chainStorageEpoch || ""),
    accounts: recipientTestAccounts().map(account => ({
      name: String(account.name || ""),
      transparentAddress: String(account.transparentAddress || ""),
      evmAddress: String(account.evmAddress || "")
    }))
  });
}

function resetProfileScopedAddressBook(scopeIdentity = "") {
  const previousAddresses = new Set(
    Object.values(state.addressBook.shieldedByName || {}).filter(Boolean)
  );
  if (els?.veiledTransferRecipient
    && previousAddresses.has(els.veiledTransferRecipient.value.trim())) {
    els.veiledTransferRecipient.value = "";
  }
  state.addressBook.scopeIdentity = scopeIdentity;
  state.addressBook.shieldedByName = {};
  state.addressBook.shieldedError = "";
  state.addressBook.loadingShielded = false;
  shieldedAddressBookPromise = null;
  shieldedAddressBookPromiseScope = "";
}

function syncProfileScopedAddressBook() {
  const scopeIdentity = addressBookScopeIdentity();
  if (state.addressBook.scopeIdentity !== scopeIdentity) {
    resetProfileScopedAddressBook(scopeIdentity);
  }
  return scopeIdentity;
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
    const message = error?.statusCode === 403
      ? "Local signer setup is blocked for LAN browsers. Create accounts on the server machine first, or restart with CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1."
      : `Automatic local signer setup failed: ${browserDataLoadErrorMessage(error)}`;
    toast(message);
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
  const validated = validateCurrentWebAppConfig(config);
  if (validated.serverBacked !== false) {
    throw new Error("Static DApp bootstrap requires serverBacked: false");
  }
  const health = await isolatedBrowserClient(validated).health();
  return {
    config: validated,
    status: health.status,
    tree: health.tree,
    audit: health.audit,
    accounts: [],
    errors: health.errors || []
  };
}

async function loadDappHealth() {
  if (serverHealthEndpointState !== "absent") {
    try {
      const data = await loadServerDappHealth();
      const validated = validateCurrentWebAppConfig(data.config);
      if (validated.serverBacked !== true) {
        throw new Error("Server DApp health requires config.serverBacked: true");
      }
      serverHealthEndpointState = "available";
      return ensureLocalSignersIfNeeded(data);
    } catch (error) {
      if (!healthBootstrapFallbackAllowed(error)) throw error;
      if (healthBootstrapEndpointAbsent(error)) {
        serverHealthEndpointState = "absent";
      }
    }
  }
  return browserHealthFromStaticConfig(await loadStaticDappConfig());
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
    },
    {
      input: els.relayWithdrawRecipient,
      list: els.relayWithdrawRecipientSuggestions,
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
  syncProfileScopedAddressBook();
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
  const scopeIdentity = syncProfileScopedAddressBook();
  const missing = recipientTestAccounts().filter(account => !state.addressBook.shieldedByName[account.name]);
  if (!missing.length) return;
  if (shieldedAddressBookPromise && shieldedAddressBookPromiseScope === scopeIdentity) {
    await shieldedAddressBookPromise;
    return;
  }

  state.addressBook.loadingShielded = true;
  state.addressBook.shieldedError = "";
  renderVisibleAddressSuggestions();

  const lookup = Promise.allSettled(missing.map(async account => {
    const data = await api(`/api/wallet/${account.name}/show-address`);
    return { name: account.name, address: data.address || "" };
  }));
  shieldedAddressBookPromise = lookup;
  shieldedAddressBookPromiseScope = scopeIdentity;

  const results = await lookup;
  if (state.addressBook.scopeIdentity !== scopeIdentity
    || shieldedAddressBookPromise !== lookup) {
    return;
  }
  for (const result of results) {
    if (result.status === "fulfilled" && result.value.address) {
      state.addressBook.shieldedByName[result.value.name] = result.value.address;
    }
  }
  state.addressBook.loadingShielded = false;
  shieldedAddressBookPromise = null;
  shieldedAddressBookPromiseScope = "";
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
    const scopeIdentity = syncProfileScopedAddressBook();
    ensureShieldedAddressBook().catch(error => {
      if (state.addressBook.scopeIdentity !== scopeIdentity) return;
      state.addressBook.loadingShielded = false;
      state.addressBook.shieldedError = error.message;
      shieldedAddressBookPromise = null;
      shieldedAddressBookPromiseScope = "";
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
  invalidatePrivacySession();
  state.wallet = defaultMetaMaskState();
}

function resetDisclosureSessionState() {
  state.privacyEvents = defaultPrivacyEventsState();
  state.blockEvents = defaultBlockEventsState();
  state.auditor = defaultAuditorState();

  if (els.disclosureSourceTxHash) els.disclosureSourceTxHash.value = "";
  if (els.disclosureSourceEventJson) els.disclosureSourceEventJson.value = "";
  if (els.decodeDisclosureSource) els.decodeDisclosureSource.disabled = false;
  renderPrivacyEvents();
  renderBlockEvents();
  renderEventDetail();
  if (els.auditorEventsList) els.auditorEventsList.replaceChildren();
  clearAuditorReport();
  renderAuditorTestScalar();
}

function resetKeplrSession() {
  invalidatePrivacySession();
  stopRelayReservationHeartbeat();
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
  batchTransferArtifactStore = null;
  batchTransferArtifactStorePromise = null;
  batchTransferArtifactStoreKey = "";
  evmDepositArtifactStore = null;
  evmDepositArtifactStorePromise = null;
  evmDepositArtifactStoreKey = "";
  batchTransferInFlight = false;
  batchTransferRowSequence = 0;
  state.reservations = defaultReservationState();
  state.relayWithdraw = {
    handoff: null,
    json: "",
    reservationIds: [],
    payloadHash: "",
    expiresAtUnix: 0,
    durableNoBroadcast: false,
    payloadUnavailable: false,
    txHash: "",
    submittedBy: "",
    evmSender: "",
    evmReceiptEvidence: null,
    externalHandoff: false,
    resultStatus: "idle",
    resultMessage: "Not checked"
  };
  state.relayWithdrawRecoveries = [];
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
  const handoff = state.relayWithdraw.handoff;
  const payload = relayWithdrawHandoffPayload(handoff);
  const relayer = localRelayerAccount();
  const localRelayerReady = Boolean(relayer?.transparentAddress);
  const recoveryChoices = state.relayWithdrawRecoveries || [];
  const showRecoveryChoices = recoveryChoices.length > 1 && !handoff;
  els.relayWithdrawRecoveryChoiceField.hidden = !showRecoveryChoices;
  els.relayWithdrawRecoveryChoice.disabled = state.relayWithdraw.resultStatus === "checking";
  if (showRecoveryChoices) {
    els.relayWithdrawRecoveryChoice.innerHTML = "";
    for (const recovery of recoveryChoices) {
      const option = document.createElement("option");
      option.value = recovery.payloadHash;
      option.textContent = `${shorten(recovery.payloadHash, 10, 8)} · expires ${new Date(recovery.expiresAtUnix * 1000).toLocaleString()}`;
      els.relayWithdrawRecoveryChoice.append(option);
    }
    els.relayWithdrawRecoveryChoice.value = state.relayWithdraw.payloadHash;
  }
  els.relayWithdrawState.textContent = handoff || state.relayWithdraw.payloadUnavailable
    ? state.relayWithdraw.resultMessage || "Payload ready"
    : "Ready for payload preparation";
  els.relayerTransparentAddress.textContent = relayer?.transparentAddress || "-";
  els.relayerBalance.textContent = state.relayer.error
    || state.relayer.balance
    || (localRelayerReady ? "Loading..." : "-");
  els.relayWithdrawChain.textContent = payload?.chain_id || handoff?.transaction?.chainId || "-";
  els.relayWithdrawPreparedRecipient.textContent = payload?.recipient || "-";
  const expiry = Number(payload?.expires_at_unix || state.relayWithdraw.expiresAtUnix || 0);
  els.relayWithdrawExpiry.textContent = expiry
    ? `${new Date(expiry * 1000).toLocaleString()} (${expiry})`
    : "-";
  els.relayWithdrawPayloadHash.textContent = payload?.payload_hash || state.relayWithdraw.payloadHash || "-";
  // Keep the raw payload out of the DOM until the durable external-handoff
  // marker and the final authoritative chain-time check have completed.
  els.relayWithdrawJson.value = state.relayWithdraw.externalHandoff
    ? state.relayWithdraw.json
    : "";
  els.relayWithdrawJson.placeholder = state.relayWithdraw.json && !state.relayWithdraw.externalHandoff
    ? "Use Copy or Download to record the handoff before exposing this payload."
    : "";
  els.relayWithdrawTxHashDisplay.textContent = state.relayWithdraw.txHash
    ? shorten(state.relayWithdraw.txHash, 14, 12)
    : "-";
  els.relayWithdrawTxHash.value = state.relayWithdraw.txHash;
  const metadataOnlyRecovery = !handoff
    && state.relayWithdraw.payloadUnavailable === true
    && ["cosmos", "evm"].includes(activeChainProfile()?.transport);
  els.relayWithdrawTxHash.disabled = !state.relayWithdraw.externalHandoff
    || (!handoff && !metadataOnlyRecovery)
    || ["checking", "confirmed"].includes(state.relayWithdraw.resultStatus);
  els.relayWithdrawSubmittedBy.textContent = state.relayWithdraw.submittedBy || "-";
  els.relayWithdrawResult.textContent = state.relayWithdraw.resultMessage || "Not checked";
  els.relayWithdrawResult.dataset.status = state.relayWithdraw.resultStatus;
  els.reconcileRelayWithdraw.disabled = (!handoff
    && !state.relayWithdraw.payloadUnavailable)
    || (state.relayWithdraw.resultStatus === "ready"
      && !state.relayWithdraw.txHash
      && !state.relayWithdraw.externalHandoff)
    || state.relayWithdraw.resultStatus === "checking";
  const canStartHandoff = Boolean(state.relayWithdraw.json)
    && !state.relayWithdraw.externalHandoff
    && !relayHandoffInFlight
    && !valueMovingActionGate.active
    && state.relayWithdraw.resultStatus === "ready";
  els.copyRelayWithdraw.disabled = !canStartHandoff;
  els.downloadRelayWithdraw.disabled = !canStartHandoff;
  els.relayPreparedWithdraw.hidden = !serverFeature("relayer");
  els.relayPreparedWithdraw.disabled = !handoff
    || !localRelayerReady
    || valueMovingActionGate.active
    || relayHandoffInFlight
    || state.relayWithdraw.externalHandoff
    || state.relayWithdraw.resultStatus !== "ready";
}

async function setRelayWithdrawHandoff(prepared) {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const reservationIDs = preparedReservationIDs(prepared);
  if (!prepared.reservationManager || !prepared.reservation || !reservationIDs.length) {
    throw new Error("Relay withdraw handoff requires an active prepared reservation");
  }
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (manager !== prepared.reservationManager) {
    throw stalePrivacySessionError(sessionContext);
  }
  const store = await currentOperationStore();
  assertPrivacySession(sessionContext);
  const identity = operationStoreIdentity();
  if (!store || !identity) {
    throw new Error("Encrypted operation recovery store is not available");
  }
  const handoff = createRelayWithdrawHandoff({
    profileId: activeChainProfile()?.id || "",
    transport: activeChainProfile()?.transport || "cosmos",
    payload: prepared.payload,
    transaction: prepared.transaction
  });
  const nextRelayWithdraw = {
    handoff,
    reservationIds: reservationIDs,
    payloadHash: String(prepared.payload?.payload_hash || "").trim().toLowerCase(),
    expiresAtUnix: Number(prepared.payload?.expires_at_unix || 0),
    durableNoBroadcast: true,
    payloadUnavailable: false,
    txHash: "",
    submittedBy: "",
    evmSender: "",
    evmReceiptEvidence: null,
    externalHandoff: false,
    resultStatus: "ready",
    resultMessage: "Payload ready · choose local Relay or external handoff",
    leaseToken: prepared.reservation.lease_token,
    leaseUntil: prepared.reservation.lease_until,
    json: JSON.stringify(handoff, (_key, value) => (
      typeof value === "bigint" ? value.toString() : value
    ), 2)
  };
  await persistRelayWithdrawRecovery(nextRelayWithdraw, { store, identity, sessionContext });
  assertPrivacySession(sessionContext);
  state.relayWithdraw = nextRelayWithdraw;
  startRelayReservationHeartbeat({
    manager,
    reservationIDs,
    leaseToken: prepared.reservation.lease_token,
    leaseUntil: prepared.reservation.lease_until,
    sessionContext
  });
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
  renderRelayWithdraw();
}

async function recordExternalRelayWithdrawHandoff(surface) {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  if (!state.relayWithdraw.handoff) {
    throw new Error("Prepare a relay withdraw payload first");
  }
  if (state.relayWithdraw.externalHandoff || relayHandoffInFlight) {
    throw new Error("This relay payload handoff is already in progress or recorded");
  }
  const context = captureRelaySubmitContext();
  const reservationIDs = [...state.relayWithdraw.reservationIds];
  const leaseToken = state.relayWithdraw.leaseToken;
  const payload = relayWithdrawHandoffPayload(state.relayWithdraw.handoff);
  const payloadHash = payload?.payload_hash || "";
  const expiryLeaseUntil = relayWithdrawExpiryLeaseUntil(payload);
  relayHandoffInFlight = true;
  renderRelayWithdraw();
  try {
    const manager = await currentReservationManager();
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    if (!manager || !reservationIDs.length || !leaseToken) {
      throw new Error("Relay withdraw reservation manager is unavailable");
    }
    const chainBlock = await fetchLatestChainBlock();
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    if (relayWithdrawPayloadExpired(payload, chainBlock.timeUnix)) {
      throw new Error(`Relay payload expired at authoritative chain height ${chainBlock.height}`);
    }
    const renewed = await manager.renewLease(reservationIDs, {
      leaseToken,
      leaseUntil: expiryLeaseUntil
    });
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    const expiryLeaseMilliseconds = Date.parse(expiryLeaseUntil);
    if (renewed.length !== reservationIDs.length
      || renewed.some(record => {
        const leaseMilliseconds = Date.parse(record?.lease_until || "");
        return !Number.isFinite(leaseMilliseconds) || leaseMilliseconds < expiryLeaseMilliseconds;
      })) {
      throw new Error("Relay handoff lease was not durably extended through payload expiry");
    }
    const renewedLeaseUntil = renewed[0]?.lease_until || expiryLeaseUntil;
    await manager.recordRelayHandoff(reservationIDs, {
      leaseToken,
      payloadHash,
      metadata: { handoff_surface: surface }
    });
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    const handedOffState = {
      ...state.relayWithdraw,
      leaseUntil: renewedLeaseUntil,
      externalHandoff: true,
      durableNoBroadcast: false,
      resultStatus: "waiting",
      resultMessage: "External handoff recorded · waiting for relayer result"
    };
    state.relayWithdraw = {
      ...handedOffState,
      json: "",
      resultStatus: "egress-blocked",
      resultMessage: "External handoff fence recorded · payload remains hidden until a fresh chain-time check succeeds"
    };
    await persistRelayWithdrawRecovery(handedOffState, { sessionContext });
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    await refreshReservationState(manager, { sessionContext });
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    const egressChainBlock = await fetchLatestChainBlock();
    assertPrivacySession(sessionContext);
    assertRelaySubmitContext(context);
    if (relayWithdrawPayloadExpired(payload, egressChainBlock.timeUnix)) {
      state.relayWithdraw = {
        ...handedOffState,
        handoff: null,
        json: "",
        payloadUnavailable: true,
        resultStatus: "expired-review",
        resultMessage: `External handoff was recorded, but the payload expired before exposure at authoritative chain height ${egressChainBlock.height} · reconcile before retrying`
      };
      const error = new Error(`Relay payload expired before external exposure at authoritative chain height ${egressChainBlock.height}`);
      error.code = "RELAY_PAYLOAD_EXPIRED_BEFORE_EGRESS";
      throw error;
    }
    state.relayWithdraw = handedOffState;
    return { relayContext: context, sessionContext };
  } finally {
    try {
      assertPrivacySession(sessionContext);
      relayHandoffInFlight = false;
      renderRelayWithdraw();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

function renderKeplr() {
  const connected = Boolean(state.keplr.account);
  const signerReady = connected && state.keplr.addressMatches;
  const veiledReady = signerReady && Boolean(state.keplr.rootSignatureBase64);
  const privacyStorage = privacyBrowserStorageCapability();
  const privacyStorageReady = privacyStorage.available;
  renderWalletSession();
  els.myClairBalance.textContent = connected ? state.keplr.balance || "-" : "-";
  els.keplrDisclosurePubKey.textContent = state.keplr.disclosurePubKeyHex || "Setup Clairveil first";
  els.keplrSendHash.textContent = state.keplr.sendHash ? shorten(state.keplr.sendHash, 14, 12) : "-";
  els.keplrDepositHash.textContent = state.keplr.depositHash ? shorten(state.keplr.depositHash, 14, 12) : "-";
  els.keplrDepositHeight.textContent = state.keplr.depositHeight || "-";
  els.keplrDepositRecovery.textContent = state.keplr.depositRecoveryMessage || "Not started";
  els.keplrDepositNetworkFee.textContent = state.keplr.networkFeeEstimate || "Not estimated";
  const sendPending = ["attempting", "submitted", "unknown", "checking"].includes(state.keplr.sendStatus);
  const depositPending = ["attempting", "submitted", "unknown", "checking", "recovery-pending"].includes(state.keplr.depositRecoveryStatus);
  els.reconcileKeplrSend.disabled = !sendPending || !state.keplr.sendHash;
  els.reconcileKeplrDeposit.disabled = !depositPending || !state.keplr.depositHash;
  const hashlessAttempt = (state.keplr.sendStatus === "attempting" && !state.keplr.sendHash)
    || (state.keplr.depositRecoveryStatus === "attempting" && !state.keplr.depositHash);
  els.clearPublicPendingState.hidden = !state.keplr.publicPendingStateError && !hashlessAttempt;
  els.clearPublicPendingState.textContent = state.keplr.publicPendingStateError
    ? "Clear corrupt pending state"
    : "Clear unresolved wallet attempt";
  els.keplrTransferHash.textContent = state.keplr.transferHash ? shorten(state.keplr.transferHash, 14, 12) : "-";
  els.batchTransferHash.textContent = state.keplr.batchTransferHash ? shorten(state.keplr.batchTransferHash, 14, 12) : "-";
  els.keplrWithdrawHash.textContent = state.keplr.withdrawHash ? shorten(state.keplr.withdrawHash, 14, 12) : "-";
  els.keplrWithdrawHeight.textContent = state.keplr.withdrawHeight || "-";
  els.keplrWithdrawNullifier.textContent = state.keplr.withdrawNullifierStatus;
  els.keplrWithdrawReceive.textContent = state.keplr.withdrawReceiveStatus;
  if (connected && !els.veiledWithdrawRecipient.value) {
    els.veiledWithdrawRecipient.value = state.keplr.account;
  }
  renderMyKeplrNotes();
  els.fundKeplr.disabled = valueMovingActionGate.active || !serverFeature("faucet") || !signerReady;
  els.setupKeplrPrivacy.disabled = valueMovingActionGate.active || !signerReady || !privacyStorageReady;
  els.setupKeplrPrivacy.title = privacyStorageReady ? "" : privacyStorage.message;
  els.copyKeplrShieldedAddress.disabled = !state.keplr.shieldedAddress;
  els.copyKeplrDisclosurePubKey.disabled = !state.keplr.disclosurePubKeyHex;
  els.refreshWalletBalance.disabled = !connected;
  const noteMutationPending = noteStoreCoordinator.pending > 0;
  const accountTransactionPending = publicTransactionCoordinator.pending > 0;
  const privateRecoveryResetVisible = activeChainProfile()?.transport === "cosmos"
    && Boolean(state.keplr.privacyPendingStateError);
  els.resetPrivatePendingState.hidden = !privateRecoveryResetVisible;
  els.resetPrivatePendingState.disabled = !privateRecoveryResetVisible
    || !veiledReady
    || !privacyStorageReady
    || !state.protocol.ready
    || noteMutationPending
    || accountTransactionPending
    || valueMovingActionGate.active
    || relayHandoffInFlight;
  els.resetPrivatePendingState.title = !privateRecoveryResetVisible
    ? ""
    : !veiledReady
      ? "Setup Clairveil before starting the reviewed privacy recovery reset."
      : !privacyStorageReady
        ? privacyStorage.message
        : !state.protocol.ready
          ? "Protocol preflight must pass before the required full typed scan."
          : "Review wallet and explorer history before resetting the corrupt private recovery state.";
  els.scanKeplrNotes.disabled = noteMutationPending || !privacyStorageReady || !signerReady || !state.keplr.rootSignatureBase64 || !state.protocol.ready;
  els.resetRescanNotes.disabled = noteMutationPending || !privacyStorageReady || !signerReady || !state.keplr.rootSignatureBase64 || !state.protocol.ready;
  if (els.noteScanEndpoint) {
    els.noteScanEndpoint.disabled = noteMutationPending
      || accountTransactionPending
      || valueMovingActionGate.active
      || profileRestEndpoints(activeChainProfile()).length < 2;
  }
  updateNoteRollbackButton({ signerReady, privacyStorageReady });
  els.backupNoteCache.disabled = !privacyStorageReady
    || !noteStoreKeys()?.encrypted
    || !privacyStorage.storage.getItem(noteStoreKeys().encrypted);
  els.noteSyncState.textContent = privacyStorageReady
    ? state.keplr.noteSyncMessage || "Not scanned"
    : privacyStorage.message;
  els.noteSyncState.dataset.status = privacyStorageReady ? state.keplr.noteSyncStatus : "error";
  renderReservationState();
  renderRelayWithdraw();
  updateAmountActionButtons({
    signerReady,
    veiledReady,
    privacyStorageReady,
    privacyStorageMessage: privacyStorage.message
  });
  renderEventDetail();
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

function updateNoteRollbackButton({
  signerReady = Boolean(state.keplr.account && state.keplr.addressMatches),
  privacyStorageReady = privacyBrowserStorageCapability().available
} = {}) {
  if (!els.rollbackRescanNotes) return;
  const height = String(els.noteRollbackHeight?.value || "").trim();
  els.rollbackRescanNotes.disabled = !signerReady
    || !privacyStorageReady
    || noteStoreCoordinator.pending > 0
    || !state.keplr.rootSignatureBase64
    || !state.protocol.ready
    || !/^(0|[1-9][0-9]*)$/.test(height);
}

function updateAmountActionButtons(status = {}) {
  const connected = Boolean(state.keplr.account);
  const signerReady = status.signerReady ?? (connected && state.keplr.addressMatches);
  const veiledReady = status.veiledReady ?? (signerReady && Boolean(state.keplr.rootSignatureBase64));
  const privacyStorage = status.privacyStorageReady === undefined
    ? privacyBrowserStorageCapability()
    : {
        available: status.privacyStorageReady,
        message: status.privacyStorageMessage || ""
      };
  const privacyStorageReady = privacyStorage.available;
  const sendPending = ["attempting", "submitted", "unknown", "checking"].includes(state.keplr.sendStatus);
  const depositPending = ["attempting", "submitted", "unknown", "checking", "recovery-pending"].includes(state.keplr.depositRecoveryStatus);
  const cosmosPrivacyPending = Boolean(state.keplr.cosmosPrivacyPendingHash);
  const privacyBoundaryBlocked = cosmosPrivacyPending || Boolean(state.keplr.privacyPendingStateError);
  const protocolReady = state.protocol.ready;
  const noteInventoryTrusted = state.keplr.noteSyncStatus === "synced" && protocolReady;
  const valueMovingActionPending = valueMovingActionGate.active;
  els.sendFromKeplr.disabled = valueMovingActionPending
    || !signerReady
    || Boolean(state.keplr.publicPendingStateError)
    || privacyBoundaryBlocked
    || sendPending
    || !hasPositiveUclairInput(els.keplrSendAmount)
    || !isSendRecipientForWallet(els.keplrSendRecipient.value, state.activeWallet || activeWalletKind());
  els.depositFromKeplr.disabled = valueMovingActionPending
    || !signerReady
    || !privacyStorageReady
    || Boolean(state.keplr.publicPendingStateError)
    || privacyBoundaryBlocked
    || depositPending
    || !protocolReady
    || !depositProofReady()
    || !hasPositiveUclairInput(els.keplrDepositAmount);
  els.depositFromKeplr.title = !privacyStorageReady
    ? privacyStorage.message
    : !protocolReady
    ? "Protocol preflight must pass before depositing."
    : state.keplr.privacyPendingStateError
      ? state.keplr.privacyPendingStateError
      : cosmosPrivacyPending
      ? "Reconcile the existing privacy transaction before preparing another Cosmos sequence."
    : depositProofReady()
      ? ""
      : "Configure CLAIRVEIL_DEPOSIT_PROOF_URL or inject CLAIRVEIL_DEPOSIT_PROOF_PROVIDER.";
  const relayRecoveryBlocked = Boolean(state.relayWithdraw.handoff)
    && state.relayWithdraw.resultStatus !== "confirmed";
  els.transferFromVeiled.disabled = valueMovingActionPending
    || !veiledReady
    || !privacyStorageReady
    || privacyBoundaryBlocked
    || !protocolReady
    || !noteInventoryTrusted
    || !hasPositiveUclairInput(els.veiledTransferAmount);
  els.withdrawFromVeiled.disabled = valueMovingActionPending
    || !veiledReady
    || !privacyStorageReady
    || privacyBoundaryBlocked
    || !protocolReady
    || !noteInventoryTrusted
    || !hasPositiveUclairInput(els.veiledWithdrawAmount);
  els.relayWithdrawFromVeiled.disabled = valueMovingActionPending
    || !veiledReady
    || !privacyStorageReady
    || !protocolReady
    || !noteInventoryTrusted
    || relayRecoveryBlocked
    || !hasPositiveUclairInput(els.relayWithdrawAmount);
  const privacySpendTitle = !privacyStorageReady
    ? privacyStorage.message
    : state.keplr.privacyPendingStateError
    ? state.keplr.privacyPendingStateError
    : cosmosPrivacyPending
      ? "Reconcile the existing privacy transaction before preparing another Cosmos sequence."
    : !protocolReady
    ? "Protocol preflight must pass before using shielded notes."
    : !noteInventoryTrusted
      ? "Complete the note scan before using the displayed shielded balance."
      : "";
  els.transferFromVeiled.title = privacySpendTitle;
  els.withdrawFromVeiled.title = privacySpendTitle;
  els.relayWithdrawFromVeiled.title = relayRecoveryBlocked
    ? "Reconcile the existing relay handoff before preparing another relay withdraw."
    : privacySpendTitle;
  if (els.transferBatchFromVeiled) {
    const batchReady = batchTransferEnabled() && batchPaymentDraftIsValid();
    els.transferBatchFromVeiled.disabled = !batchReady
      || batchTransferInFlight
      || valueMovingActionPending
      || !veiledReady
      || !privacyStorageReady
      || !protocolReady
      || !noteInventoryTrusted
      || privacyBoundaryBlocked;
    els.transferBatchFromVeiled.title = !batchReady
      ? "Add valid shielded payments and complete every disclosure setting. The 32-output limit includes change."
      : privacySpendTitle;
  }
}

const batchTransferMinPayments = 1;
const batchTransferMaxPayments = 32;
const batchTransferMaxInputs = 16;

function batchTransferEnabled() {
  return ["cosmos", "evm"].includes(activeChainProfile()?.transport)
    && serverFeature("batchTransfer");
}

function batchTransferRows() {
  return [...(els.batchTransferRows?.querySelectorAll("[data-batch-transfer-row]") || [])];
}

function batchAuthorizationRequested() {
  return Boolean(
    activeChainProfile()?.transport === "evm"
    && els.batchUseAuthorization?.checked
    && evmBatchAuthorizationAvailable(activeChainProfile())
  );
}

function renderBatchAuthorization() {
  if (!els.batchAuthorizationPanel) return;
  const profile = activeChainProfile();
  const available = evmBatchAuthorizationAvailable(profile);
  els.batchAuthorizationPanel.hidden = !available;
  if (!available) {
    els.batchUseAuthorization.checked = false;
    els.batchAuthorizationOptions.hidden = true;
    return;
  }
  const kinds = evmBatchAuthorizationKinds(profile);
  const signature = kinds.join(",");
  if (els.batchAuthorizationKind.dataset.kinds !== signature) {
    const previous = Number(els.batchAuthorizationKind.value);
    els.batchAuthorizationKind.replaceChildren(...kinds.map(kind => {
      const option = document.createElement("option");
      option.value = String(kind);
      option.textContent = `Kind ${kind}`;
      return option;
    }));
    els.batchAuthorizationKind.value = kinds.includes(previous) ? String(previous) : String(kinds[0]);
    els.batchAuthorizationKind.dataset.kinds = signature;
  }
  els.batchUseAuthorization.disabled = batchTransferInFlight;
  els.batchAuthorizationKind.disabled = batchTransferInFlight || !els.batchUseAuthorization.checked;
  els.batchAuthorizationOptions.hidden = !els.batchUseAuthorization.checked;
  els.batchAuthorizationAccount.textContent = state.wallet.account || "Connect an EVM wallet";
}

function batchAuthorizationPreparationOptions(
  timing = {},
  sessionContext = privacySessionSnapshot()
) {
  assertPrivacySession(sessionContext);
  if (!batchAuthorizationRequested()) return {};
  const authorization = selfSubmittedEvmBatchAuthorization({
    profile: activeChainProfile(),
    account: state.wallet.account,
    authorizationKind: els.batchAuthorizationKind.value,
    nonce: randomEvmAuthorizationNonce(),
    deadline: timing.expiresAtUnix
  });
  return {
    authorization,
    authorizationSigner: {
      signTypedData: typedData => evmWalletAdapter(sessionContext).signTypedData(typedData)
    }
  };
}

function batchDisclosureLabel(payment = {}) {
  const mode = payment.userDisclosureMode || "none";
  if (mode === "none") return "Private";
  const policy = payment.userPrivacyPolicy || "all-private";
  if (mode === "public") return `Public · ${policy}`;
  return `Recipient encrypted · ${policy} · target ${shorten(payment.userDisclosureTargetPubKeyHex, 12, 10)}`;
}

function renderBatchTransferRowDisclosure(row) {
  const mode = row.querySelector("[data-batch-disclosure-mode]")?.value || "none";
  const policy = row.querySelector("[data-batch-disclosure-policy]");
  const targetField = row.querySelector("[data-batch-disclosure-target-field]");
  const target = row.querySelector("[data-batch-disclosure-target]");
  if (policy) {
    policy.disabled = mode === "none" || batchTransferInFlight;
    if (mode === "none") policy.value = "all-private";
    if (mode !== "none" && policy.value === "all-private") policy.value = "amount";
  }
  if (targetField) targetField.hidden = mode !== "recipient-encrypted";
  if (target) target.disabled = mode !== "recipient-encrypted" || batchTransferInFlight;
}

function batchPaymentDraftIsValid() {
  const rows = batchTransferRows();
  if (rows.length < batchTransferMinPayments || rows.length > batchTransferMaxPayments) return false;
  return rows.every(row => {
    const recipient = row.querySelector("[data-batch-recipient]")?.value.trim() || "";
    const amount = row.querySelector("[data-batch-amount]");
    const mode = row.querySelector("[data-batch-disclosure-mode]")?.value || "none";
    const policy = row.querySelector("[data-batch-disclosure-policy]")?.value || "all-private";
    const target = row.querySelector("[data-batch-disclosure-target]")?.value.trim() || "";
    const disclosureValid = mode === "none"
      ? policy === "all-private"
      : policy !== "all-private"
        && (mode !== "recipient-encrypted" || /^[0-9a-fA-F]{64}$/.test(target));
    return Boolean(recipient)
      && !isSelfTransferRecipient(recipient)
      && hasPositiveUclairInput(amount)
      && disclosureValid;
  });
}

function addBatchTransferPayment({
  recipient = "",
  amount = "1",
  userPrivacyPolicy = "all-private",
  userDisclosureMode = "none",
  userDisclosureTargetPubKeyHex = ""
} = {}) {
  if (!els.batchTransferRows || batchTransferRows().length >= batchTransferMaxPayments) return;
  const row = document.createElement("div");
  row.className = "batch-transfer-row";
  row.dataset.batchTransferRow = String(++batchTransferRowSequence);

  const recipientField = document.createElement("label");
  recipientField.className = "field address-field";
  const recipientLabel = document.createElement("span");
  recipientLabel.textContent = "Shielded recipient";
  const recipientInput = document.createElement("input");
  recipientInput.dataset.batchRecipient = "";
  recipientInput.placeholder = `${shieldedPrefix()}1...`;
  recipientInput.autocomplete = "off";
  recipientInput.value = recipient;
  recipientField.append(recipientLabel, recipientInput);

  const amountField = document.createElement("label");
  amountField.className = "field amount-field";
  const amountLabel = document.createElement("span");
  amountLabel.textContent = "Amount";
  const amountControl = document.createElement("div");
  amountControl.className = "amount-control";
  const amountInput = document.createElement("input");
  amountInput.dataset.batchAmount = "";
  amountInput.inputMode = "numeric";
  amountInput.value = amount;
  const denom = document.createElement("span");
  denom.className = "denom";
  denom.textContent = baseDenom();
  amountControl.append(amountInput, denom);
  amountField.append(amountLabel, amountControl);

  const disclosureField = document.createElement("fieldset");
  disclosureField.className = "field batch-disclosure-field";
  const disclosureLegend = document.createElement("legend");
  disclosureLegend.textContent = "User disclosure";
  const disclosureControls = document.createElement("div");
  disclosureControls.className = "batch-disclosure-controls";

  const modeLabel = document.createElement("label");
  modeLabel.className = "field";
  const modeTitle = document.createElement("span");
  modeTitle.textContent = "Mode";
  const modeSelect = document.createElement("select");
  modeSelect.dataset.batchDisclosureMode = "";
  for (const [value, label] of [
    ["none", "Private"],
    ["public", "Public"],
    ["recipient-encrypted", "Recipient encrypted"]
  ]) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    modeSelect.append(option);
  }
  modeSelect.value = userDisclosureMode;
  modeLabel.append(modeTitle, modeSelect);

  const policyLabel = document.createElement("label");
  policyLabel.className = "field";
  const policyTitle = document.createElement("span");
  policyTitle.textContent = "Fields";
  const policySelect = document.createElement("select");
  policySelect.dataset.batchDisclosurePolicy = "";
  for (const value of [
    "all-private", "amount", "to", "amount-to", "from", "amount-from", "from-to", "amount-from-to"
  ]) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value === "all-private" ? "None" : value;
    policySelect.append(option);
  }
  policySelect.value = userPrivacyPolicy;
  policyLabel.append(policyTitle, policySelect);

  const targetLabel = document.createElement("label");
  targetLabel.className = "field batch-disclosure-target-field";
  targetLabel.dataset.batchDisclosureTargetField = "";
  const targetTitle = document.createElement("span");
  targetTitle.textContent = "Target disclosure public key";
  const targetInput = document.createElement("input");
  targetInput.dataset.batchDisclosureTarget = "";
  targetInput.placeholder = "32-byte hex public key";
  targetInput.autocomplete = "off";
  targetInput.value = userDisclosureTargetPubKeyHex;
  targetLabel.append(targetTitle, targetInput);
  disclosureControls.append(modeLabel, policyLabel, targetLabel);
  disclosureField.append(disclosureLegend, disclosureControls);

  const remove = document.createElement("button");
  remove.type = "button";
  remove.className = "secondary-button batch-transfer-remove";
  remove.textContent = "Remove";
  remove.addEventListener("click", () => {
    row.remove();
    renderBatchTransfer();
    updateAmountActionButtons();
  });
  [recipientInput, amountInput, targetInput].forEach(input => input.addEventListener("input", updateAmountActionButtons));
  modeSelect.addEventListener("change", () => {
    renderBatchTransferRowDisclosure(row);
    updateAmountActionButtons();
  });
  policySelect.addEventListener("change", updateAmountActionButtons);
  row.append(recipientField, amountField, remove, disclosureField);
  els.batchTransferRows.append(row);
  renderBatchTransferRowDisclosure(row);
}

function renderBatchTransfer() {
  if (!els.batchTransferSection) return;
  const enabled = batchTransferEnabled();
  els.batchTransferSection.hidden = !enabled;
  if (!enabled) {
    renderBatchAuthorization();
    return;
  }
  const transport = activeChainProfile()?.transport === "evm" ? "EVM" : "Cosmos";
  els.batchTransferDescription.textContent = `Submit shielded payments atomically with one proof and one ${transport} transaction. The 32-output limit includes change.`;
  while (batchTransferRows().length < batchTransferMinPayments) {
    addBatchTransferPayment();
  }
  const rows = batchTransferRows();
  els.addBatchTransferPayment.disabled = batchTransferInFlight || rows.length >= batchTransferMaxPayments;
  for (const row of rows) {
    const remove = row.querySelector(".batch-transfer-remove");
    if (remove) remove.disabled = batchTransferInFlight || rows.length <= batchTransferMinPayments;
    for (const control of row.querySelectorAll("input, select")) control.disabled = batchTransferInFlight;
    renderBatchTransferRowDisclosure(row);
  }
  renderBatchAuthorization();
}

function collectBatchTransferPayments() {
  const rows = batchTransferRows();
  if (rows.length < batchTransferMinPayments || rows.length > batchTransferMaxPayments) {
    throw new Error(`Atomic batch transfer requires ${batchTransferMinPayments}–${batchTransferMaxPayments} payments`);
  }
  return rows.map((row, index) => {
    const recipient = row.querySelector("[data-batch-recipient]")?.value.trim() || "";
    if (!recipient) throw new Error(`Batch recipient ${index + 1} is required`);
    if (isSelfTransferRecipient(recipient)) {
      throw new Error(`Batch recipient ${index + 1} cannot be your own shielded address`);
    }
    const amount = amountInputValue(row.querySelector("[data-batch-amount]"));
    const userDisclosureMode = row.querySelector("[data-batch-disclosure-mode]")?.value || "none";
    const userPrivacyPolicy = row.querySelector("[data-batch-disclosure-policy]")?.value || "all-private";
    const userDisclosureTargetPubKeyHex = row.querySelector("[data-batch-disclosure-target]")?.value.trim() || "";
    if (userDisclosureMode === "none" && userPrivacyPolicy !== "all-private") {
      throw new Error(`Batch payment ${index + 1} private mode must not select disclosed fields`);
    }
    if (userDisclosureMode !== "none" && userPrivacyPolicy === "all-private") {
      throw new Error(`Batch payment ${index + 1} disclosure must select at least one field`);
    }
    if (userDisclosureMode === "recipient-encrypted" && !/^[0-9a-fA-F]{64}$/.test(userDisclosureTargetPubKeyHex)) {
      throw new Error(`Batch payment ${index + 1} recipient-encrypted disclosure requires a 32-byte hex public key`);
    }
    return {
      itemId: `batch-payment-${row.dataset.batchTransferRow}`,
      amount,
      amountValue: parsePlannerAmountValue(amount),
      recipient,
      userPrivacyPolicy,
      userDisclosureMode,
      ...(userDisclosureMode === "recipient-encrypted" ? { userDisclosureTargetPubKeyHex } : {})
    };
  });
}

function batchTransferReviewRecipients(payments = []) {
  return payments.map((payment, index) => (
    `${index + 1}. ${payment.recipient} · ${coinText(payment.amount)} · ${batchDisclosureLabel(payment)}`
  )).join("\n");
}

function batchTransferReviewDisclosure(payments = []) {
  return payments.map((payment, index) => `${index + 1}. ${batchDisclosureLabel(payment)}`).join("\n")
    + "\nMandatory audit: full · Sender self-view: encrypted";
}

function batchTransferReviewAmount(payments = []) {
  const total = payments.reduce((sum, payment) => {
    const amount = parsePlannerAmountValue(payment.amount);
    if (amount === null) throw new Error("Batch payment amount is invalid");
    return sum + amount;
  }, 0n);
  return coinTextFromAmount(total.toString());
}

function preparedBatchReview(data, requestedPayments) {
  const facts = preparedBatchTransferFacts({
    requestedPayments,
    prepared: data?.prepared || {},
    denom: baseDenom(),
    maxInputs: batchTransferMaxInputs,
    maxOutputs: batchTransferMaxPayments
  });
  return {
    payments: requestedPayments,
    facts,
    changeEffect: `${coinTextFromAmount(facts.change.toString())} returned to ${shorten(state.keplr.shieldedAddress, 16, 12)} · ${facts.inputCount}/${batchTransferMaxInputs} inputs · ${facts.outputCount}/${batchTransferMaxPayments} outputs · all-or-nothing`
  };
}

function renderMyKeplrNotes() {
  const protocolReady = state.protocol.ready;
  const noteInventoryTrusted = privacyBrowserStorageCapability().available
    && state.keplr.noteSyncStatus === "synced"
    && protocolReady;
  els.myKeplrSpendable.textContent = !protocolReady
    ? state.protocol.error
      ? "Unavailable · protocol preflight failed"
      : "Checking protocol compatibility"
    : noteInventoryTrusted
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

  if (!protocolReady) {
    const warning = document.createElement("p");
    warning.className = "empty stale-note-warning";
    warning.textContent = "Spendable note inventory is hidden until protocol preflight succeeds.";
    els.myKeplrNotesList.append(warning);
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
  const previousProfileIdentity = activeProfileSessionIdentity();
  const validatedConfig = validateCurrentWebAppConfig(data.config);
  const observedStorageEpoch = localChainStorageEpoch({
    localTestMode: Boolean(validatedConfig.localTestMode),
    status: data.status
  });
  const previousStorageEpoch = state.chainStorageEpoch;
  const storageEpochChanged = Boolean(
    previousStorageEpoch && observedStorageEpoch && previousStorageEpoch !== observedStorageEpoch
  );
  state.chainStorageEpoch = observedStorageEpoch || previousStorageEpoch;
  state.config = validatedConfig;
  state.chainProfiles = [...validatedConfig.chainProfiles];
  if (!state.selectedChainProfileId || !state.chainProfiles.some(profile => profile.id === state.selectedChainProfileId)) {
    state.selectedChainProfileId = validatedConfig.activeChainProfileId || state.chainProfiles[0]?.id || "";
  }
  const nextProfileIdentity = activeProfileSessionIdentity();
  const profileChanged = Boolean(
    previousProfileIdentity && previousProfileIdentity !== nextProfileIdentity
  );
  if (storageEpochChanged || profileChanged) {
    protocolRequestGeneration += 1;
    state.protocol.ready = false;
    state.protocol.reserve = null;
    state.protocol.error = "";
    browserClient = null;
    browserClientKey = "";
    browserClientDepositProofProvider = null;
    if (state.activeWallet) resetWalletSession();
  }
  state.accounts = data.accounts || [];
  if (!state.accounts.some(account => account.name === state.selectedAccount)) {
    state.selectedAccount = state.accounts[0]?.name || "alice";
  }
  if (storageEpochChanged || profileChanged) {
    resetProfileScopedAddressBook(addressBookScopeIdentity());
  } else {
    syncProfileScopedAddressBook();
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
  const addressBookScope = state.addressBook.scopeIdentity;
  ensureShieldedAddressBook().catch(error => {
    if (state.addressBook.scopeIdentity !== addressBookScope) return;
    state.addressBook.loadingShielded = false;
    state.addressBook.shieldedError = error.message;
    shieldedAddressBookPromise = null;
    shieldedAddressBookPromiseScope = "";
    renderVisibleAddressSuggestions();
  });
  renderProtocolStatus();
}

function renderProtocolStatus() {
  if (!els.protocolState) return;
  if (state.protocol.ready) {
    els.protocolState.textContent = "v0.3.1 ready";
    return;
  }
  els.protocolState.textContent = state.protocol.error ? "Unavailable" : "Checking";
}

function canonicalEvmChainId(value) {
  const text = String(value ?? "").trim();
  if (!/^(?:0x[0-9a-fA-F]+|[0-9]+)$/.test(text)) {
    throw new Error("Configured EVM RPC returned an invalid eth_chainId");
  }
  return `0x${BigInt(text).toString(16)}`;
}

async function fullPrivacyProtocolPreflight(sessionContext) {
  assertPrivacySession(sessionContext);
  const client = clairveilBrowserClient();
  const profile = activeChainProfile();
  const [health, protocolConfig, reserve, observedEvmChainId] = await Promise.all([
    client.health(),
    client.assertTransferProtocolConfig(baseDenom()),
    client.queryReserve(baseDenom()),
    profile?.transport === "evm"
      ? client.evmJsonRpc("eth_chainId", [])
      : Promise.resolve("")
  ]);
  assertPrivacySession(sessionContext);
  if (profile?.transport === "evm") {
    const expected = canonicalEvmChainId(profile.evmChainId);
    const observed = canonicalEvmChainId(observedEvmChainId);
    if (observed !== expected) {
      throw new Error(`Configured EVM RPC chain mismatch: expected ${expected}, got ${observed}`);
    }
  }
  return { health, protocolConfig, reserve };
}

async function requirePrivacyPreparePreflight(sessionContext) {
  assertPrivacySession(sessionContext);
  requirePrivacyBrowserStorage();
  const requestGeneration = ++protocolRequestGeneration;
  state.protocol.ready = false;
  state.protocol.reserve = null;
  state.protocol.error = "";
  renderProtocolStatus();
  renderMyKeplrNotes();
  updateAmountActionButtons();
  try {
    const result = await fullPrivacyProtocolPreflight(sessionContext);
    assertPrivacySession(sessionContext);
    if (requestGeneration !== protocolRequestGeneration) {
      const error = new Error("Privacy protocol preflight was superseded; retry the operation");
      error.code = "PROTOCOL_PREFLIGHT_SUPERSEDED";
      throw error;
    }
    state.protocol.ready = true;
    state.protocol.reserve = result.reserve;
    renderProtocolStatus();
    renderMyKeplrNotes();
    updateAmountActionButtons();
    return result;
  } catch (error) {
    assertPrivacySession(sessionContext);
    if (requestGeneration === protocolRequestGeneration) {
      state.protocol.ready = false;
      state.protocol.reserve = null;
      state.protocol.error = browserDataLoadErrorMessage(error);
      renderProtocolStatus();
      renderMyKeplrNotes();
      updateAmountActionButtons();
    }
    throw error;
  }
}

async function refreshProtocolStatus() {
  if (!state.config) return;
  const requestGeneration = ++protocolRequestGeneration;
  const sessionContext = privacySessionSnapshot();
  const profileId = String(activeChainProfile()?.id || "");
  const profileIdentity = activeProfileSessionIdentity();
  state.protocol.ready = false;
  state.protocol.reserve = null;
  state.protocol.error = "";
  renderProtocolStatus();
  renderMyKeplrNotes();
  try {
    const { reserve } = await fullPrivacyProtocolPreflight(sessionContext);
    assertPrivacySession(sessionContext);
    if (requestGeneration !== protocolRequestGeneration
      || profileId !== String(activeChainProfile()?.id || "")
      || profileIdentity !== activeProfileSessionIdentity()) {
      return;
    }
    state.protocol.ready = true;
    state.protocol.reserve = reserve;
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    if (requestGeneration !== protocolRequestGeneration
      || profileId !== String(activeChainProfile()?.id || "")
      || profileIdentity !== activeProfileSessionIdentity()) {
      return;
    }
    state.protocol.error = browserDataLoadErrorMessage(error);
  }
  renderProtocolStatus();
  renderMyKeplrNotes();
  updateAmountActionButtons();
}

async function refreshHealth() {
  const requestGeneration = ++healthRequestGeneration;
  let data;
  try {
    data = await loadDappHealth();
  } catch (error) {
    if (requestGeneration !== healthRequestGeneration) return;
    throw error;
  }
  if (requestGeneration !== healthRequestGeneration) return;
  renderHealth(data);
  if (requestGeneration !== healthRequestGeneration) return;
  const sessionContext = privacySessionSnapshot();
  if (serverFeature("localSigners")) {
    try {
      await refreshSelectedAccount({ sessionContext });
      if (requestGeneration !== healthRequestGeneration) return;
    } catch (error) {
      if (requestGeneration !== healthRequestGeneration) return;
      if (error?.statusCode !== 403) {
        throw error;
      }
      renderLocalSignerUnavailable(error);
    }
  }
  const tasks = [refreshEvents({ allowFailure: true, sessionContext }), refreshProtocolStatus()];
  if (serverFeature("relayer")) {
    tasks.push(refreshRelayerAccount({ sessionContext }));
  }
  if (serverFeature("auditorAdmin")) {
    tasks.push(refreshAuditorTransfers({ sessionContext }), refreshAuditorTestScalar({ sessionContext }));
  }
  await Promise.allSettled(tasks);
  if (requestGeneration !== healthRequestGeneration) return;
}

async function refreshSelectedAccount({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const account = selectedLocalAccount();
  const accountIdentity = localAccountRequestIdentity(account);
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
  assertLocalAccountRequestCurrent(sessionContext, accountIdentity);

  els.shieldedAddress.textContent = shielded.address || "-";
  els.balanceValue.textContent = (balance.balances || [])
    .map(coin => `${coin.amount}${coin.denom}`)
    .join(", ") || zeroCoinText();

  await refreshNotes({ sessionContext });
  assertLocalAccountRequestCurrent(sessionContext, accountIdentity);
}

async function refreshWalletBalance({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  if (!state.keplr.account) return;
  const client = clairveilBrowserClient();
  const privacyAccount = state.keplr.account;
  const walletAccount = state.wallet.account;
  if (isEvmTransparentMode()) {
    if (!walletAccount) return;
    const [balanceHex, assetData] = await Promise.all([
      requestMetaMask({
        method: "eth_getBalance",
        params: [walletAccount, "latest"]
      }),
      client.getBalances(privacyAccount)
    ]);
    assertPrivacySession(sessionContext);
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
    const data = await client.getBalances(privacyAccount);
    assertPrivacySession(sessionContext);
    state.keplr.transparentBalances = balanceAmountsByDenom(data.balances);
    state.keplr.evmNativeBalance = "0";
    state.keplr.balance = formatBalances(data.balances);
  }
  renderKeplr();
}

async function refreshNotes({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const account = selectedLocalAccount();
  const accountIdentity = localAccountRequestIdentity(account);
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
  assertLocalAccountRequestCurrent(sessionContext, accountIdentity);
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

async function refreshEvents({
  allowFailure = false,
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const client = clairveilBrowserClient();
  const [privacyResult, blockResult] = await Promise.allSettled([
    client.fetchPrivacyEvents(),
    client.fetchBlockEvents(30)
  ]);
  assertPrivacySession(sessionContext);

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

async function refreshBlockEvents({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  const client = clairveilBrowserClient();
  try {
    const data = await client.fetchBlockEvents(30);
    assertPrivacySession(sessionContext);
    state.blockEvents.events = data.events || [];
    state.blockEvents.error = "";
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
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

async function refreshAuditorTestScalar({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  if (!hasAuditorUi() || !els.auditorTestScalar) return;
  els.auditorTestScalar.textContent = "Loading...";
  updateAuditorDecodeButton();
  try {
    const data = await api("/api/auditor/test-scalar");
    assertPrivacySession(sessionContext);
    state.auditor.testScalar = data.disclosure_private_scalar_hex || "";
    state.auditor.testScalarError = "";
    state.auditor.testScalarMatchesAuditConfig = Boolean(data.matches_audit_config);
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
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
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const event = selectedPrivacyEvent();
  if (!event || !canDecodeEventDisclosure(event)) return;
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  els.eventDisclosureState.textContent = "Disclosure 조회 중...";
  renderEventDetail();
  try {
    const report = await clairveilBrowserClient().decodeUserDisclosure(privacyRequest({ txHash: event.tx_hash_hex }));
    assertPrivacySession(sessionContext);
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    assertPrivacySession(sessionContext);
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    if (!privacySessionIsCurrent(sessionContext)) return;
    state.privacyEvents.loading = false;
    renderEventDetail();
  }
}

async function decodeSelectedSelfViewDisclosure() {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
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
    assertPrivacySession(sessionContext);
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    assertPrivacySession(sessionContext);
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    if (!privacySessionIsCurrent(sessionContext)) return;
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
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
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
    assertPrivacySession(sessionContext);
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    assertPrivacySession(sessionContext);
    state.privacyEvents.error = error.message;
    renderEventDisclosureError(error);
  } finally {
    if (!privacySessionIsCurrent(sessionContext)) return;
    state.privacyEvents.loading = false;
    els.decodeDisclosureSource.disabled = false;
  }
}

function clearAuditorReport(message = "Select a transfer.") {
  if (!els.auditorDecodeState) return;
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

async function refreshAuditorTransfers({ sessionContext = privacySessionSnapshot() } = {}) {
  assertPrivacySession(sessionContext);
  if (!hasAuditorUi()) return;
  setBusy(els.refreshAuditorTransfers, true);
  try {
    const data = await clairveilBrowserClient().fetchAuditableTransfers();
    assertPrivacySession(sessionContext);
    state.auditor.events = data.events || [];
    if (state.auditor.selectedTxHash && !state.auditor.events.some(event => event.tx_hash_hex === state.auditor.selectedTxHash)) {
      state.auditor.selectedTxHash = "";
      state.auditor.decoded = null;
      clearAuditorReport();
    }
    renderAuditorTransfers();
    renderAuditorEventDetail(state.auditor.events.find(event => event.tx_hash_hex === state.auditor.selectedTxHash));
  } finally {
    try {
      assertPrivacySession(sessionContext);
      setBusy(els.refreshAuditorTransfers, false);
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
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
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
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
    assertPrivacySession(sessionContext);
    state.auditor.decoded = report;
    renderAuditorReport(report);
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    assertPrivacySession(sessionContext);
    clearAuditorReport(error.message);
    if (isDisclosureVerificationFailure(error)) {
      els.auditorVerification.textContent = "Failed";
    }
  } finally {
    if (!privacySessionIsCurrent(sessionContext)) return;
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
  const sessionContext = privacySessionSnapshot();
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
  assertPrivacySession(sessionContext);
  if (!account) {
    resetWalletSession();
    renderWallet();
    renderKeplr();
    return;
  }
  await ensureMetaMaskChain();
  assertPrivacySession(sessionContext);
  const connectedChainId = await requestMetaMask({ method: "eth_chainId" });
  assertPrivacySession(sessionContext);
  resetKeplrSession();
  state.activeWallet = "metamask";
  state.wallet.account = account;
  state.wallet.chainId = connectedChainId;
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
  const connectedSessionContext = privacySessionSnapshot();
  try {
    await refreshWalletBalance({ sessionContext: connectedSessionContext });
    assertPrivacySession(connectedSessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    assertPrivacySession(connectedSessionContext);
    state.keplr.balance = error.message;
    renderKeplr();
  }
}

async function signMetaMaskSession() {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const account = state.wallet.account;
  if (!account) return;
  await ensureMetaMaskChain();
  assertPrivacySession(sessionContext);
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
  assertPrivacySession(sessionContext);
  state.wallet.signatureHash = await digestText(signature);
  assertPrivacySession(sessionContext);
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
  const sessionContext = privacySessionSnapshot();
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
  assertPrivacySession(sessionContext);

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

  const connectedSessionContext = privacySessionSnapshot();
  await refreshWalletBalance({ sessionContext: connectedSessionContext });
  toast("Keplr connected");
}

async function signKeplrSession() {
  if (!window.keplr || !state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
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
  assertPrivacySession(sessionContext);
  state.keplr.signatureHash = await digestText(signature.signature);
  assertPrivacySession(sessionContext);
  if (typeof window.keplr.verifyArbitrary === "function") {
    const verified = await window.keplr.verifyArbitrary(
      chainInfo.chainId,
      state.keplr.account,
      message,
      signature
    );
    assertPrivacySession(sessionContext);
    state.keplr.verified = verified;
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

function fundKeplr() {
  return runValueMovingAction("faucet", fundKeplrUnlocked);
}

async function fundKeplrUnlocked() {
  if (!state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  if (!serverFeature("faucet")) {
    toast("Faucet is available only when this DApp server is attached to a local test node.");
    return;
  }
  const amount = clairInputToUclair(els.keplrFaucetAmount);
  const recipient = connectedPublicRecipientAddress();
  const localSigner = selectedLocalAccount()?.name || state.accounts[0]?.name || "alice";
  const localSignerIdentity = localAccountRequestIdentity();
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
    assertLocalAccountRequestCurrent(sessionContext, localSignerIdentity);
    state.keplr.faucetHash = data.broadcast?.txhash || "";
    state.keplr.faucetRecipient = isEvmTransparentMode() ? data.recipientEvm || recipient : data.recipient || recipient;
    if (data.unknown === true) {
      state.keplr.faucetSent = "Result unknown";
      els.keplrTxState.textContent = "Faucet result unknown";
      renderKeplr();
      showNotice({
        title: "Faucet 결과 확인 필요",
        message: state.keplr.faucetHash
          ? `Submission returned an ambiguous chain result. Check tx ${state.keplr.faucetHash} before retrying.`
          : "Submission returned an ambiguous chain result. Do not retry until the local signer state is reconciled."
      });
      return;
    }
    state.keplr.faucetSent = formatUclairAsClair(data.amount?.funded?.replace(baseDenom(), "") || "0");
    state.keplr.balance = formatBalances(data.balance?.balances);
    await refreshWalletBalance({ sessionContext });
    assertLocalAccountRequestCurrent(sessionContext, localSignerIdentity);
    renderKeplr();
    toast(`Faucet sent: ${state.keplr.faucetSent}`);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    assertLocalAccountRequestCurrent(sessionContext, localSignerIdentity);
    toast(error.message);
  } finally {
    try {
      assertLocalAccountRequestCurrent(sessionContext, localSignerIdentity);
      setBusy(els.fundKeplr, false);
      renderKeplr();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function completeInitialPrivacySetup({
  skipInitialSync = false,
  sessionContext = privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  await refreshProtocolStatus();
  assertPrivacySession(sessionContext);
  if (!state.protocol.ready) {
    throw new Error(state.protocol.error || "Consensus circuit and asset preflight failed");
  }
  if (!skipInitialSync && state.keplr.noteSyncStatus !== "synced") {
    els.keplrTxState.textContent = "Initial note sync";
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      skipSetup: true,
      maxPages: 1000,
      sessionContext
    });
    assertPrivacySession(sessionContext);
    if (state.keplr.noteSyncStatus !== "synced") {
      throw new Error("Initial note sync did not reach the latest durable cursor");
    }
  }
}

async function setupKeplrPrivacy(options = {}) {
  if (!state.keplr.account) return false;
  const sessionContext = privacySessionSnapshot();
  let setupBusy = false;
  try {
    requirePrivacyBrowserStorage();
    if (state.keplr.rootSignatureBase64 && state.keplr.shieldedAddress && state.keplr.disclosurePubKeyHex) {
      await hydrateBatchTransferRecovery({ sessionContext });
      if (activeChainProfile()?.transport === "evm") {
        await restoreEvmDepositRecovery({ sessionContext });
      }
      assertPrivacySession(sessionContext);
      await refreshReservationState(null, { sessionContext });
      assertPrivacySession(sessionContext);
      await completeInitialPrivacySetup({ ...options, sessionContext });
      assertPrivacySession(sessionContext);
      els.keplrTxState.textContent = options.skipInitialSync ? "Identity ready" : "Ready · notes synced";
      return true;
    }

    setupBusy = true;
    setBusy(els.setupKeplrPrivacy, true);
    els.keplrTxState.textContent = "Setting up";
    let account;
    let rootSignatureBase64;
    if (state.activeWallet === "metamask") {
      await ensureMetaMaskChain();
      assertPrivacySession(sessionContext);
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(state.keplr.account, state.keplr.pubkeyHex);
      const signatureHex = await requestMetaMask({
        method: "personal_sign",
        params: [rootMessage, state.wallet.account]
      });
      assertPrivacySession(sessionContext);
      rootSignatureBase64 = bytesToBase64(hexToBytes(signatureHex));
      account = clairveilBrowserClient().derivePrivacyAccount({
        walletType: "evm",
        address: state.keplr.account,
        pubKeyHex: state.keplr.pubkeyHex,
        signatureBase64: rootSignatureBase64
      });
    } else {
      if (!window.keplr) return false;
      const chainInfo = activeKeplrChainInfo();
      if (!chainInfo) {
        throw new Error("Selected chain does not include Keplr chain info");
      }
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(state.keplr.account, state.keplr.pubkeyHex);
      const signature = await window.keplr.signArbitrary(chainInfo.chainId, state.keplr.account, rootMessage);
      assertPrivacySession(sessionContext);
      rootSignatureBase64 = signature.signature;
      account = clairveilBrowserClient().derivePrivacyAccount({
        address: state.keplr.account,
        pubKeyHex: state.keplr.pubkeyHex,
        signatureBase64: signature.signature
      });
    }
    assertPrivacySession(sessionContext);
    state.keplr.rootSignatureBase64 = rootSignatureBase64;
    state.keplr.shieldedAddress = account.shielded_address || "";
    state.keplr.disclosurePubKeyHex = account.disclosure_pubkey_hex || "";
    state.keplr.rootSignatureHash = account.root_signature_hash || "";
    try {
      await hydrateRelayWithdrawRecovery({ sessionContext });
    } catch (error) {
      if (isStalePrivacySessionError(error)) throw error;
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = error.message;
      state.reservations.status = "error";
      state.reservations.message = error.message;
      state.reservations.retryBlocked = true;
    }
    await hydrateBatchTransferRecovery({ sessionContext });
    if (activeChainProfile()?.transport === "evm") {
      await restoreEvmDepositRecovery({ sessionContext });
    }
    assertPrivacySession(sessionContext);
    await refreshReservationState(null, { sessionContext });
    assertPrivacySession(sessionContext);
    await completeInitialPrivacySetup({ ...options, sessionContext });
    assertPrivacySession(sessionContext);
    els.keplrTxState.textContent = options.skipInitialSync ? "Identity ready" : "Ready · notes synced";
    toast(options.skipInitialSync ? "Clairveil identity ready" : "Clairveil account ready · notes synced");
    return true;
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return false;
    els.keplrTxState.textContent = "Setup failed";
    toast(error.message);
    return false;
  } finally {
    if (privacySessionIsCurrent(sessionContext)) {
      if (setupBusy) setBusy(els.setupKeplrPrivacy, false);
      renderKeplr();
    }
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

function copyRelayWithdraw() {
  return runValueMovingAction("relay-external-handoff", copyRelayWithdrawUnlocked);
}

async function copyRelayWithdrawUnlocked() {
  if (!state.relayWithdraw.json) throw new Error("Prepare a relay withdraw payload first");
  const { relayContext, sessionContext } = await recordExternalRelayWithdrawHandoff("clipboard");
  assertPrivacySession(sessionContext);
  assertRelaySubmitContext(relayContext);
  await navigator.clipboard.writeText(state.relayWithdraw.json);
  toast("Relay withdraw handoff JSON copied");
}

function downloadRelayWithdraw() {
  return runValueMovingAction("relay-external-handoff", downloadRelayWithdrawUnlocked);
}

async function downloadRelayWithdrawUnlocked() {
  if (!state.relayWithdraw.json) throw new Error("Prepare a relay withdraw payload first");
  const { relayContext, sessionContext } = await recordExternalRelayWithdrawHandoff("download");
  assertPrivacySession(sessionContext);
  assertRelaySubmitContext(relayContext);
  downloadTextFile(`clairveil-relay-withdraw-${Date.now()}.json`, state.relayWithdraw.json);
  toast("Relay withdraw handoff JSON downloaded");
}

async function signDirectAndBroadcast(signDoc, options = {}) {
  if (!window.keplr?.signDirect) {
    throw new Error("Keplr signDirect not available");
  }
  const {
    sessionContext = privacySessionSnapshot(),
    publicPendingKind = "",
    publicTransactionLockHeld = false,
    afterSigningBeforeBroadcast = null,
    ...broadcastOptions
  } = options;
  if (afterSigningBeforeBroadcast !== null && typeof afterSigningBeforeBroadcast !== "function") {
    throw new TypeError("afterSigningBeforeBroadcast must be a function");
  }
  const privateReservationBroadcast = !publicPendingKind
    && Boolean(broadcastOptions.reservationManager && broadcastOptions.reservation);
  const execute = async () => {
    assertPrivacySession(sessionContext);
    if (publicPendingKind) assertNoCapturedPublicPendingTransaction(sessionContext, publicPendingKind);
    const client = clairveilBrowserClient();
    const signingAccount = state.keplr.account;
    const wallet = {
      address: signingAccount,
      pubKeyHex: state.keplr.pubkeyHex,
      signDirect: async directSignDoc => {
        assertPrivacySession(sessionContext);
        const signed = await window.keplr.signDirect(
          directSignDoc.chainId,
          signingAccount,
          directSignDoc,
          keplrDirectSignOptions(broadcastOptions)
        );
        assertPrivacySession(sessionContext);
        return signed;
      }
    };
    const checkpoint = await client.signDirect({
      wallet,
      signDoc,
      ...broadcastOptions
    });
    assertPrivacySession(sessionContext);
    const signedTxHash = transactionHashFromEvidence(checkpoint);
    if ((publicPendingKind || privateReservationBroadcast) && !signedTxHash) {
      throw new Error("Signed Cosmos transaction did not provide a durable transaction identity");
    }
    if (afterSigningBeforeBroadcast) {
      await afterSigningBeforeBroadcast({ checkpoint, signedTxHash });
      assertPrivacySession(sessionContext);
    }
    if (publicPendingKind) {
      persistCapturedPublicPendingTransaction(sessionContext, publicPendingKind, signedTxHash);
    }
    assertPrivacySession(sessionContext);
    let result;
    try {
      result = await client.broadcastTxRawBytes(checkpoint.txRawBytes, {
        ...broadcastOptions,
        beforeBroadcast: identity => {
          assertPrivacySession(sessionContext);
          if (publicPendingKind) {
            persistCapturedPublicPendingTransaction(
              sessionContext,
              publicPendingKind,
              transactionHashFromEvidence(identity) || signedTxHash
            );
          } else if (privateReservationBroadcast) {
            persistCapturedPublicPendingTransaction(
              sessionContext,
              "privacy",
              transactionHashFromEvidence(identity) || signedTxHash
            );
          }
        }
      });
    } catch (error) {
      const txHash = transactionHashFromEvidence(error) || signedTxHash;
      const broadcastDefinitelyNotSubmitted = error?.rpcInvoked === false
        || error?.rpc_invoked === false
        || cosmosTxEvidenceConfirmsFailure(error);
      if (publicPendingKind && broadcastDefinitelyNotSubmitted) {
        try {
          clearCapturedPublicPendingTransaction(
            sessionContext,
            publicPendingKind,
            txHash
          );
        } catch (persistenceError) {
          persistenceError.txHash = txHash;
          persistenceError.broadcast = error.broadcast;
          throw persistenceError;
        }
      }
      if (privateReservationBroadcast && cosmosPrivatePendingMarkerCanClear({
        markerTxHash: capturedPrivacyPendingState(sessionContext)?.txHash,
        txHash,
        error
      })) {
        clearCapturedPublicPendingTransaction(sessionContext, "privacy", txHash);
      }
      const failure = error instanceof Error ? error : new Error(String(error));
      failure.txHash ||= txHash;
      failure.tx ||= error?.tx;
      failure.broadcast ||= error?.broadcast;
      // Ambiguous private outcomes retain their account fence until the entire
      // linked operation reaches a reconciled safe terminal state. Only exact
      // pre-RPC abort or CheckTx rejection evidence can clear it here.
      if (!privacySessionIsCurrent(sessionContext)) {
        const stale = stalePrivacySessionError(sessionContext);
        stale.txHash = txHash;
        stale.tx = failure.tx;
        stale.broadcast = failure.broadcast;
        throw stale;
      }
      throw failure;
    }
    try {
      assertPrivacySession(sessionContext);
    } catch (error) {
      error.txHash = transactionHashFromEvidence(result) || signedTxHash;
      error.broadcast = result;
      throw error;
    }
    if (publicPendingKind && result?.ok) {
      try {
        const txHash = transactionHashFromEvidence(result) || signedTxHash;
        if (publicPendingKind === "deposit") {
          persistCapturedDepositRecoveryPending(
            sessionContext,
            txHash,
            result?.tx?.height || result?.height || ""
          );
        } else {
          clearCapturedPublicPendingTransaction(sessionContext, publicPendingKind, txHash);
        }
      } catch (error) {
        error.txHash = transactionHashFromEvidence(result) || signedTxHash;
        error.broadcast = result;
        throw error;
      }
    }
    return result;
  };
  return publicPendingKind && !publicTransactionLockHeld
    ? withPublicTransactionLock(sessionContext, execute)
    : execute();
}

async function submitEvmTransaction(transaction, options = {}) {
  if (!metaMaskProvider() || !state.wallet.account) {
    throw new Error("MetaMask is not connected");
  }
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  const {
    onTransactionAttempt,
    onTransactionHash,
    onTransactionRejected,
    ...submissionOptions
  } = options;
  assertPrivacySession(sessionContext);
  const txHash = await clairveilBrowserClient().sendEvmTransaction({
    wallet: evmWalletAdapter(sessionContext, {
      onTransactionAttempt,
      onTransactionHash,
      onTransactionRejected
    }),
    transaction,
    ...submissionOptions
  });
  const normalizedTxHash = normalizeEvmTxHash(txHash);
  assertPrivacySessionAfterEvmSubmission(sessionContext, normalizedTxHash);
  return normalizedTxHash;
}

async function waitForEvmTransaction(
  txHash,
  label = "EVM transaction",
  reservationBinding = {},
  privacyTransaction = null,
  sender = state.wallet.account
) {
  const sessionContext = reservationBinding.sessionContext
    || reservationBinding.privacySessionContext
    || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const transaction = privacyTransaction || reservationBinding.privacyTransaction;
  if (!transaction) {
    throw new Error(`${label} cannot be verified without the original SDK-prepared privacy transaction`);
  }
  const broadcast = await clairveilBrowserClient().waitForEvmTransaction(txHash, {
    privacyTransaction: transaction,
    sender
  });
  assertPrivacySession(sessionContext);
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
      assertPrivacySession(sessionContext);
      await refreshReservationState(manager, { sessionContext });
      assertPrivacySession(sessionContext);
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

async function waitForPublicEvmTransaction(txHash) {
  const receipt = await clairveilBrowserClient().evmJsonRpc("eth_getTransactionReceipt", [
    /^0x/i.test(txHash) ? txHash : `0x${txHash}`
  ]);
  if (!receipt) return { txHash, receipt: null, unknown: true };
  if (evmReceiptHasFailed(receipt)) {
    const error = new Error(`EVM transaction failed with receipt status ${receipt.status}`);
    error.broadcast = { txHash, receipt };
    throw error;
  }
  if (!evmReceiptSucceeded(receipt)) {
    return { txHash, receipt, unknown: true };
  }
  return { txHash, receipt };
}

async function sendEvmTransaction(transaction, {
  waitForReceipt = false,
  label = "EVM transaction",
  reservationBinding = {},
  privacyTransaction = null,
  sessionContext = privacySessionSnapshot(),
  onTransactionAttempt,
  onTransactionHash,
  onTransactionRejected,
  onSubmitted = null
} = {}) {
  const txHash = await submitEvmTransaction(transaction, {
    ...reservationBinding,
    sessionContext,
    onTransactionAttempt,
    onTransactionHash,
    onTransactionRejected
  });
  assertPrivacySessionAfterEvmSubmission(sessionContext, txHash);
  if (typeof onSubmitted === "function") {
    try {
      await onSubmitted(txHash);
      assertPrivacySessionAfterEvmSubmission(sessionContext, txHash);
    } catch (error) {
      error.txHash ||= txHash;
      throw error;
    }
  }
  const preparedPrivacyTransaction = privacyTransaction || reservationBinding.privacyTransaction || null;
  const waitForResult = preparedPrivacyTransaction
    ? () => waitForEvmTransaction(
        txHash,
        label,
        reservationBinding,
        preparedPrivacyTransaction,
        reservationBinding.sender || state.wallet.account
      )
    : () => waitForPublicEvmTransaction(txHash);
  if (waitForReceipt) {
    const broadcast = await waitForResult();
    assertPrivacySessionAfterEvmSubmission(sessionContext, txHash);
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  }
  const waitPromise = waitForResult();
  waitPromise.catch(() => {});
  return {
    txHash,
    pending: true,
    waitPromise
  };
}

function watchEvmBroadcast(broadcast, { sessionContext, onIncluded, onUnknown, onFailed } = {}) {
  if (!broadcast?.waitPromise) return;
  void broadcast.waitPromise.then(result => {
    if (sessionContext) assertPrivacySession(sessionContext);
    return result.unknown ? onUnknown?.(result) : onIncluded?.(result);
  }).catch(async error => {
    if (isStalePrivacySessionError(error)) return;
    try {
      await onFailed?.(error);
    } catch (callbackError) {
      reportAsyncError(callbackError);
    }
  });
}

async function reconcilePublicTransaction(kind) {
  const isDeposit = kind === "deposit";
  const txHash = isDeposit ? state.keplr.depositHash : state.keplr.sendHash;
  const button = isDeposit ? els.reconcileKeplrDeposit : els.reconcileKeplrSend;
  if (!txHash) return;
  const sessionContext = privacySessionSnapshot();
  const evm = activeChainProfile()?.transport === "evm";
  if (isDeposit) {
    state.keplr.depositRecoveryStatus = "checking";
    state.keplr.depositRecoveryMessage = "Checking the existing tx hash · retry remains blocked";
  } else {
    state.keplr.sendStatus = "checking";
  }
  setBusy(button, true);
  renderKeplr();
  try {
    let result;
    if (evm) {
      if (isDeposit && !state.keplr.depositPrepared?.transaction) {
        await restoreEvmDepositRecovery({ sessionContext });
        assertPrivacySession(sessionContext);
      }
      const preparedDepositTransaction = isDeposit
        ? state.keplr.depositPrepared?.transaction || null
        : null;
      if (isDeposit && !preparedDepositTransaction) {
        state.keplr.publicPendingStateError = state.keplr.publicPendingStateError
          || "EVM deposit receipt cannot be verified without its encrypted SDK transaction binding. Do not retry until wallet history and chain evidence are reviewed.";
        state.keplr.depositRecoveryStatus = "unknown";
        state.keplr.depositRecoveryMessage = "SDK transaction binding unavailable · manual recovery required";
        throw new Error(state.keplr.publicPendingStateError);
      }
      result = preparedDepositTransaction
        ? await waitForEvmTransaction(
            txHash,
            "EVM deposit",
            { sessionContext },
            preparedDepositTransaction,
            state.keplr.depositPrepared?.sender
          )
        : await waitForPublicEvmTransaction(txHash);
    } else {
      const check = await checkReservationTransaction(txHash);
      assertPrivacySession(sessionContext);
      if (check.failed) {
        const error = new Error(check.transaction?.raw_log || `${isDeposit ? "Deposit" : "Send"} failed on-chain`);
        error.code = "TX_FAILED_ON_CHAIN";
        error.txHash = txHash;
        error.tx = check.transaction;
        throw error;
      }
      if (!check.included || !check.successful) {
        result = { unknown: true, txHash };
      } else {
        result = {
          txHash,
          tx: check.transaction,
          broadcast: { txhash: txHash },
          height: check.height
        };
      }
    }
    assertPrivacySession(sessionContext);
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

    await withPublicTransactionLock(sessionContext, () => {
      if (isDeposit) {
        persistCapturedDepositRecoveryPending(
          sessionContext,
          txHash,
          result.receipt?.blockNumber || result.tx?.height || result.height || ""
        );
      } else {
        clearCapturedPublicPendingTransaction(sessionContext, kind, txHash);
      }
    });
    assertPrivacySession(sessionContext);

    if (isDeposit) {
      state.keplr.depositHeight = result.receipt?.blockNumber || result.tx?.height || result.height || state.keplr.depositHeight;
      updateIncludedDepositNetworkFee(result);
      if (state.keplr.depositPrepared) {
        await recoverDepositNote({ ...result, prepared: state.keplr.depositPrepared });
        assertPrivacySession(sessionContext);
      } else {
        try {
          await scanKeplrNotes({
            quiet: true,
            throwOnError: true,
            maxPages: 1000,
            sessionContext
          });
          assertPrivacySession(sessionContext);
          const recovered = recoveredDepositNoteForTxHash(state.keplr.notes, txHash);
          state.keplr.depositRecoveryStatus = recovered ? "recovered" : "pending";
          state.keplr.depositRecoveryMessage = recovered
            ? "Recovered · encrypted note matched the exact included tx hash"
            : "Included · exact transaction note not found · Reset & Rescan required";
        } catch (error) {
          if (isStalePrivacySessionError(error)) throw error;
          assertPrivacySession(sessionContext);
          state.keplr.depositRecoveryStatus = "pending";
          state.keplr.depositRecoveryMessage = `Included · note recovery pending (${error.message})`;
        }
      }
      await refreshPrivacySurfaces({ balance: true, sessionContext });
      assertPrivacySession(sessionContext);
    } else {
      state.keplr.sendStatus = "included";
      await Promise.allSettled([
        refreshWalletBalance({ sessionContext }),
        refreshBlockEvents({ sessionContext })
      ]);
      assertPrivacySession(sessionContext);
    }
    els.keplrTxState.textContent = `${isDeposit ? "Deposit" : "Send"} included`;
    const depositRecoveryPending = isDeposit
      && state.keplr.depositRecoveryStatus !== "recovered";
    showNotice({
      title: `${isDeposit ? "Deposit" : "Send"} 결과 확인됨`,
      message: depositRecoveryPending
        ? `기존 deposit tx 포함은 확인됐지만 encrypted note 복구가 남아 있습니다. exact tx recovery marker는 유지되며 Reset & Rescan 완료 전 새 요청은 차단됩니다.\nTx: ${shorten(txHash, 14, 12)}`
        : `기존 tx가 포함된 것을 확인했습니다. 새 요청을 만들 수 있습니다.\nTx: ${shorten(txHash, 14, 12)}`
    });
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    let failureConfirmed = error?.code === "TX_FAILED_ON_CHAIN"
      || evmReceiptHasFailed(error?.broadcast?.receipt);
    if (failureConfirmed) {
      try {
        await withPublicTransactionLock(sessionContext, async () => {
          if (isDeposit) {
            await clearConfirmedDepositRecoveryUnlocked(sessionContext, txHash);
          } else {
            clearCapturedPublicPendingTransaction(sessionContext, kind, txHash);
          }
        });
      } catch (persistenceError) {
        failureConfirmed = false;
        error = new Error(
          `${error.message}; durable pending state could not be cleared: ${persistenceError.message}`,
          { cause: error }
        );
      }
    }
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

function evmWalletAdapter(sessionContext = privacySessionSnapshot(), {
  onTransactionAttempt,
  onTransactionHash,
  onTransactionRejected
} = {}) {
  const walletAccount = state.wallet.account;
  return {
    getChainId: async () => {
      assertPrivacySession(sessionContext);
      const chainId = await requestMetaMask({ method: "eth_chainId" });
      assertPrivacySession(sessionContext);
      return chainId;
    },
    signTypedData: async typedData => {
      assertPrivacySession(sessionContext);
      await ensureMetaMaskChain();
      assertPrivacySession(sessionContext);
      const account = String(walletAccount || "").trim().toLowerCase();
      if (!/^0x[0-9a-f]{40}$/.test(account)) {
        throw new Error("A connected EVM account is required for EIP-712 authorization");
      }
      const signature = await requestMetaMask({
        method: "eth_signTypedData_v4",
        params: [account, JSON.stringify(typedData)]
      });
      assertPrivacySession(sessionContext);
      if (typeof signature !== "string" || !/^0x[0-9a-fA-F]+$/.test(signature)) {
        throw new Error("EVM wallet eth_signTypedData_v4 must return a 0x-prefixed hex signature");
      }
      return signature;
    },
    sendTransaction: async transaction => {
      assertPrivacySession(sessionContext);
      await ensureMetaMaskChain();
      assertPrivacySession(sessionContext);
      const tx = await withEstimatedEvmGas({ ...transaction, from: walletAccount });
      assertPrivacySession(sessionContext);
      const attemptId = String(runSynchronousWalletBoundaryCallback(
        "onTransactionAttempt",
        onTransactionAttempt,
        tx
      ) || "");
      if (attemptId && !/^[0-9a-f]{64}$/.test(attemptId)) {
        throw new Error("onTransactionAttempt must return a canonical 32-byte attempt ID");
      }
      let txHash;
      try {
        txHash = await requestMetaMask({
          method: "eth_sendTransaction",
          params: [tx]
        });
      } catch (error) {
        if (attemptId && isExplicitWalletRejection(error)) {
          runSynchronousWalletBoundaryCallback(
            "onTransactionRejected",
            onTransactionRejected,
            attemptId
          );
        } else if (attemptId) {
          const failure = new Error(error?.message || String(error), { cause: error });
          failure.code = "EVM_SUBMISSION_RESULT_UNKNOWN";
          failure.providerCode = error?.code ?? error?.data?.code;
          failure.publicTransactionAttemptId = attemptId;
          throw failure;
        }
        throw error;
      }
      const submittedTxHash = String(txHash || "").trim();
      try {
        runSynchronousWalletBoundaryCallback(
          "onTransactionHash",
          onTransactionHash,
          submittedTxHash,
          attemptId
        );
        assertPrivacySession(sessionContext);
      } catch (error) {
        throw attachSubmittedEvmTransactionEvidence(error, submittedTxHash);
      }
      return submittedTxHash;
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
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  await requirePrivacyPreparePreflight(sessionContext);
  assertPrivacySession(sessionContext);
  const data = await clairveilBrowserClient().prepareDeposit(privacyRequest({
    amount,
    ...cosmosFeeRequestOptions(cosmosGasLimits.deposit),
    signal: options.signal
  }));
  assertPrivacySession(sessionContext);
  return { ...data, privacySessionContext: sessionContext };
}

async function preparePrivacyTransferSignDoc(amount, recipient, disclosure = {}, options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  const amountValue = parsePlannerAmountValue(amount);
  if (amountValue === null) throw new Error("Transfer amount must be a canonical integer");
  await requirePrivacyPreparePreflight(sessionContext);
  assertPrivacySession(sessionContext);
  const data = await clairveilBrowserClient().prepareTransfer(privacyRequest({
    amount,
    recipient,
    scan: {
      after: typedPrivacyScanAfter(),
      scanSource: "privacy_scan",
      strictPrivacyScan: true,
      limit: 200,
      maxPages: 1000
    },
    ...disclosure,
    reservationManager: manager,
    ...cosmosFeeRequestOptions(cosmosGasLimits.transfer),
    allowPlanStep: Boolean(options.allowPlanStep),
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  assertPrivacySession(sessionContext);
  const preparedData = {
    ...data,
    reservationManager: manager,
    reservationKind: "transfer",
    reservationRecipient: recipient,
    privacySessionContext: sessionContext
  };
  let preparedExpiresAtUnix;
  try {
    preparedExpiresAtUnix = assertPreparedTransferFreshAtChainTime(preparedData, {
      chainNowUnix: options.chainNowUnix
    });
  } catch (error) {
    error.preparedPrivacyData = preparedData;
    error.rpcInvoked = false;
    error.broadcastAbortedBeforeRpc = true;
    try {
      await discardPreparedReservation(preparedData, "invalid_prepared_transfer_expiry");
    } catch (discardError) {
      error.reservationDiscardError = discardError;
    }
    throw error;
  }
  try {
    await checkpointPreparedEvmOperation(
      preparedData,
      data.prepared?.planAction === "self_merge" ? "self_merge" : "transfer",
      { sessionContext }
    );
  } catch (error) {
    await discardPreparedReservation(preparedData, "evm_recovery_checkpoint_failed").catch(() => {});
    throw error;
  }
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
  return {
    ...preparedData,
    preparedExpiresAtUnix,
  };
}

async function prepareBatchTransfer(payments, options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const transport = activeChainProfile()?.transport;
  const expectedWallet = transport === "evm" ? "metamask" : "keplr";
  if (!["cosmos", "evm"].includes(transport) || state.activeWallet !== expectedWallet) {
    throw new Error("One-proof batch transfer requires the wallet selected by the active Cosmos or EVM profile");
  }
  const client = clairveilBrowserClient();
  if (typeof client.prepareTransferBatch !== "function") {
    throw new Error("The installed ClairveilJS build does not support one-proof batch transfers");
  }
  await requirePrivacyPreparePreflight(sessionContext);
  assertPrivacySession(sessionContext);
  await assertNoUnresolvedBatchTransferArtifact({ sessionContext });
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");

  let artifact = {
    phase: "preparing",
    transport,
    executionTransport: transport,
    ...(transport === "evm" ? { sender: state.wallet.account } : {}),
    payments
  };
  const checkpoint = async (phase, fields = {}, context = {}) => {
    assertPrivacySession(sessionContext);
    artifact = {
      ...artifact,
      ...fields,
      phase,
      reservation: context?.reservation || artifact.reservation || null,
      operationId: context?.operationId || artifact.operationId || ""
    };
    await saveBatchTransferArtifact(artifact, { sessionContext });
  };

  const data = await client.prepareTransferBatch(privacyRequest({
    payments,
    outputMode: "compact",
    scan: {
      after: typedPrivacyScanAfter(),
      scanSource: "privacy_scan",
      strictPrivacyScan: true,
      limit: 200,
      maxPages: 1000
    },
    reservationManager: manager,
    ...cosmosFeeRequestOptions(cosmosGasLimits.transfer),
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal,
    ...(options.authorization ? {
      authorization: options.authorization,
      authorizationSigner: options.authorizationSigner
    } : {}),
    onPreparedPayload: async (payload, context) => checkpoint(
      "payload-checkpointed",
      { payload },
      context
    ),
    onPreparedProof: async (proof, context) => checkpoint("proof-checkpointed", {
      payload: context?.payload || artifact.payload,
      proof
    }, context)
  }));
  assertPrivacySession(sessionContext);
  const operationEvidenceHash = batchTransferOperationEvidenceHash(data);
  const executionComplete = transport === "evm"
    ? Boolean(data?.transaction && data?.txBytesHash)
    : Boolean(data?.signDoc);
  if (!executionComplete || !data?.reservation || !operationEvidenceHash) {
    const incomplete = {
      ...data,
      reservationManager: manager,
      privacySessionContext: sessionContext
    };
    await discardPreparedReservation(incomplete, "incomplete_batch_preparation").catch(() => {});
    throw new Error(`ClairveilJS did not return a complete ${transport} batch transaction and operation-evidence binding`);
  }
  const preparedData = {
    ...data,
    reservationManager: manager,
    reservationKind: "batch-transfer",
    reservationRecipient: `${payments.length} shielded recipients`,
    privacySessionContext: sessionContext
  };
  artifact = {
    ...artifact,
    phase: "proof-ready",
    reservation: data.reservation,
    ...(data.transaction ? { transaction: data.transaction } : {}),
    ...(data.signDoc ? { signDoc: data.signDoc } : {}),
    ...(data.txBytesHash ? { txBytesHash: data.txBytesHash } : {}),
    ...(data.authorization ? { authorization: data.authorization } : {}),
    ...(data.authorizationTypedData ? { authorizationTypedData: data.authorizationTypedData } : {}),
    operationEvidence: data?.prepared?.operationEvidence || data.operationEvidence || null,
    operationEvidenceHash,
    prepared: data.prepared
  };
  try {
    await saveBatchTransferArtifact(artifact, { sessionContext });
  } catch (error) {
    await discardPreparedReservation(preparedData, "batch_recovery_checkpoint_failed").catch(() => {});
    throw error;
  }
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
  return preparedData;
}

async function preparePrivacyWithdrawSignDoc(amount, recipient, options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  await requirePrivacyPreparePreflight(sessionContext);
  assertPrivacySession(sessionContext);
  const data = await clairveilBrowserClient().prepareWithdraw(privacyRequest({
    amount,
    recipient,
    scan: {
      after: typedPrivacyScanAfter(),
      scanSource: "privacy_scan",
      strictPrivacyScan: true,
      limit: 200,
      maxPages: 1000
    },
    reservationManager: manager,
    ...cosmosFeeRequestOptions(cosmosGasLimits.withdraw),
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  assertPrivacySession(sessionContext);
  const preparedData = {
    ...data,
    reservationManager: manager,
    reservationKind: "withdraw",
    reservationRecipient: recipient,
    privacySessionContext: sessionContext
  };
  try {
    await checkpointPreparedEvmOperation(preparedData, "withdraw", { sessionContext });
  } catch (error) {
    await discardPreparedReservation(preparedData, "evm_recovery_checkpoint_failed").catch(() => {});
    throw error;
  }
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
  return preparedData;
}

async function preparePrivacyRelayWithdraw(amount, recipient, options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  await requirePrivacyPreparePreflight(sessionContext);
  assertPrivacySession(sessionContext);
  const data = await clairveilBrowserClient().prepareRelayWithdraw(privacyRequest({
    amount,
    recipient,
    scan: {
      after: typedPrivacyScanAfter(),
      scanSource: "privacy_scan",
      strictPrivacyScan: true,
      limit: 200,
      maxPages: 1000
    },
    reservationManager: manager,
    expiresAtUnix: options.expiresAtUnix,
    chainNowUnix: options.chainNowUnix,
    signal: options.signal
  }));
  assertPrivacySession(sessionContext);
  const preparedData = {
    ...data,
    reservationManager: manager,
    reservationKind: "relay",
    reservationRecipient: recipient,
    privacySessionContext: sessionContext
  };
  try {
    await checkpointPreparedEvmOperation(preparedData, "relay-withdraw", { sessionContext });
  } catch (error) {
    await discardPreparedReservation(preparedData, "evm_recovery_checkpoint_failed").catch(() => {});
    throw error;
  }
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
  return preparedData;
}

async function broadcastPrivacyDeposit(amount, label = "deposit", options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  if (!options.publicTransactionLockHeld) {
    return withPublicTransactionLock(sessionContext, async () => {
      assertNoCapturedPublicPendingTransaction(sessionContext, "deposit");
      if (activeChainProfile()?.transport === "cosmos") {
        await assertNoUnresolvedCosmosAccountBroadcast(sessionContext);
      }
      return broadcastPrivacyDeposit(amount, label, {
        ...options,
        sessionContext,
        publicTransactionLockHeld: true
      });
    });
  }
  els.keplrTxState.textContent = `Preparing ${label}`;
  await refreshWalletBalance();
  assertPrivacySession(sessionContext);
  const feeBudget = await estimateDepositFeeBeforeProof();
  assertPrivacySession(sessionContext);
  assertDepositFunding(amount, feeBudget);
  const data = await preparePrivacyDepositSignDoc(amount, { ...options, sessionContext });
  assertPrivacySession(sessionContext);
  state.keplr.shieldedAddress = data.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  const exactFee = await updateDepositNetworkFee(data.transaction);
  assertPrivacySession(sessionContext);
  assertDepositFunding(amount, exactFee);
  const persistEvmDepositRecovery = activeChainProfile()?.transport === "evm"
    && options.persistEvmDepositRecovery !== false;
  const evmDepositSender = String(state.wallet.account || "").trim().toLowerCase();
  if (persistEvmDepositRecovery && !/^0x[0-9a-f]{40}$/.test(evmDepositSender)) {
    throw new Error("A connected EVM sender is required for durable deposit recovery");
  }
  const recoveryArtifact = persistEvmDepositRecovery
    ? {
        phase: "proof-ready",
        prepared: data.prepared,
        transaction: data.transaction,
        sender: evmDepositSender
      }
    : null;
  if (recoveryArtifact) {
    // This durable checkpoint must precede the MetaMask boundary: receipt
    // verification requires the SDK's original transaction binding.
    await saveEvmDepositArtifact(recoveryArtifact, { sessionContext });
  }
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const submit = () => broadcastPreparedPrivacy(data, label, {
    ...options,
    sessionContext,
    publicPendingKind: "deposit",
    onEvmSubmitted: recoveryArtifact
      ? async txHash => saveEvmDepositArtifact({
          ...recoveryArtifact,
          phase: "submitted",
          txHash
        }, { sessionContext })
      : null
  });
  const submitEvmDeposit = async () => {
        assertNoCapturedPublicPendingTransaction(sessionContext, "deposit");
        let submitted;
        try {
          submitted = await submit();
        } catch (error) {
          const txHash = transactionHashFromEvidence(error);
          if (txHash) {
            if (evmReceiptHasFailed(error?.broadcast?.receipt)) {
              await clearConfirmedDepositRecoveryUnlocked(sessionContext, txHash);
            } else {
              persistCapturedPublicPendingTransaction(sessionContext, "deposit", txHash);
            }
          } else if (recoveryArtifact && error?.code === "EVM_SUBMISSION_RESULT_UNKNOWN") {
            await saveEvmDepositArtifact({
              ...recoveryArtifact,
              phase: "broadcast-unknown-no-hash"
            }, { sessionContext });
          } else if (recoveryArtifact && isExplicitWalletRejection(error)) {
            await clearEvmDepositArtifact("", { force: true, sessionContext });
          }
          throw error;
        }
        assertPrivacySession(sessionContext);
        const txHash = transactionHashFromEvidence(submitted);
        if ((submitted.pending || submitted.unknown) && !txHash) {
          throw new Error("MetaMask did not return a recoverable transaction hash");
        }
        if (submitted.pending || submitted.unknown) {
          persistCapturedPublicPendingTransaction(sessionContext, "deposit", txHash);
        } else if (txHash) {
          clearCapturedPublicPendingTransaction(sessionContext, "deposit", txHash);
        }
        return submitted;
      };
  const broadcast = state.activeWallet === "metamask"
    ? options.publicTransactionLockHeld
      ? await submitEvmDeposit()
      : await withPublicTransactionLock(sessionContext, submitEvmDeposit)
    : await submit();
  assertPrivacySession(sessionContext);
  if (!broadcast.pending) updateIncludedDepositNetworkFee(broadcast);
  state.keplr.depositHash = broadcast.broadcast?.txhash || "";
  state.keplr.depositHash = state.keplr.depositHash || broadcast.txHash || "";
  state.keplr.depositHeight = broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
  if (!broadcast.pending) {
    state.keplr.depositRecoveryStatus = "recovering";
    state.keplr.depositRecoveryMessage = "Included · recovering encrypted note";
  }
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
  const sessionContext = broadcast?.preparedPrivacyData?.privacySessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
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
      assertPrivacySession(sessionContext);
    }
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      sessionContext
    });
    assertPrivacySession(sessionContext);
    const recovered = expectedCommitment
      && state.keplr.notes.some(note => normalizedHex(noteCommitment(note)) === expectedCommitment);
    if (!recovered) {
      throw new Error("Deposit was included, but its prepared note is not in the local wallet cache yet");
    }
    const txHash = broadcast.broadcast?.txhash || broadcast.txHash || state.keplr.depositHash;
    await withPublicTransactionLock(
      sessionContext,
      () => clearConfirmedDepositRecoveryUnlocked(sessionContext, txHash)
    );
    assertPrivacySession(sessionContext);
    state.keplr.depositRecoveryStatus = "recovered";
    state.keplr.depositRecoveryMessage = "Recovered · encrypted note available";
    return true;
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
    state.keplr.depositRecoveryStatus = "pending";
    state.keplr.depositRecoveryMessage = `Included · recovery pending (${error.message})`;
    return false;
  } finally {
    if (privacySessionIsCurrent(sessionContext)) renderKeplr();
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
  if (broadcast?.receipt && evmReceiptHasFailed(broadcast.receipt)) {
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
  if (broadcast?.ok === false) {
    throw new Error(broadcast.error || `${label} did not pass EVM transaction and privacy-receipt verification`);
  }
  if (broadcast?.receipt) {
    return;
  }
  if (!broadcast?.tx) {
    const error = new Error(`${label} was broadcast but not found yet: ${txHash || "unknown tx"}`);
    error.code = "TX_RESULT_UNKNOWN";
    error.txHash = txHash || broadcast?.txBytesHash || "";
    error.txBytesHash = broadcast?.txBytesHash || "";
    error.broadcast = broadcast;
    throw error;
  }
  const txCode = canonicalCosmosTxCode(broadcast.tx.code);
  if (txCode == null) {
    const error = new Error(`${label} returned a missing or malformed transaction code`);
    error.code = "TX_RESULT_UNKNOWN";
    error.txHash = txHash || broadcast?.txBytesHash || "";
    error.txBytesHash = broadcast?.txBytesHash || "";
    error.broadcast = broadcast;
    throw error;
  }
  if (txCode > 0) {
    const error = new Error(broadcast.tx.raw_log || `${label} failed with code ${broadcast.tx.code}`);
    error.code = "TX_FAILED_ON_CHAIN";
    error.txHash = txHash;
    error.txBytesHash = broadcast?.txBytesHash || "";
    error.broadcast = broadcast;
    throw error;
  }
}

async function broadcastPreparedPrivacy(data, label = "privacy transaction", options = {}) {
  const sessionContext = data.privacySessionContext || options.sessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
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
  const cosmosTransferValidation = data.reservationKind === "transfer"
    && activeChainProfile()?.transport === "cosmos"
    ? {
      getChainNowUnix: () => fetchLatestChainBlockTimeUnix(),
      afterSigningBeforeBroadcast: async () => {
          try {
            const chainBlock = await fetchLatestChainBlock();
            assertPrivacySession(sessionContext);
            assertPreparedTransferFreshAtChainTime(data, {
              chainNowUnix: chainBlock.timeUnix
            });
          } catch (error) {
            error.preparedPrivacyData = data;
            error.rpcInvoked = false;
            error.broadcastAbortedBeforeRpc = true;
            const reason = error?.code === "TRANSFER_PAYLOAD_EXPIRED_BEFORE_BROADCAST"
              ? "prepared_transfer_expired_before_broadcast"
              : "prepared_transfer_prebroadcast_validation_failed";
            try {
              await discardPreparedReservation(data, reason);
            } catch (discardError) {
              error.reservationDiscardError = discardError;
            }
            throw error;
          }
        }
      }
    : {};
  const broadcastOptions = {
    ...reservationBinding,
    ...relayValidation,
    ...cosmosTransferValidation,
    sessionContext,
    privacySessionContext: sessionContext,
    sender: state.wallet.account
  };
  try {
    let broadcast = await withPreparedReservationHeartbeat(data, () => {
      assertPrivacySession(sessionContext);
      return state.activeWallet === "metamask"
        ? sendEvmTransaction(data.transaction, {
            label,
            waitForReceipt: Boolean(options.waitForEvmReceipt),
            reservationBinding: broadcastOptions,
            privacyTransaction: data.transaction,
            sessionContext,
            onSubmitted: async txHash => {
              if (data.reservationKind === "batch-transfer") {
                await saveBatchSubmission(data, txHash, { sessionContext });
              } else if (data.reservation) {
                await updatePreparedEvmOperationArtifact(
                  data,
                  { phase: "submitted", txHash },
                  { sessionContext }
                );
              }
              await options.onEvmSubmitted?.(txHash);
              assertPrivacySessionAfterEvmSubmission(sessionContext, txHash);
            },
            ...(options.publicPendingKind
              ? publicEvmTransactionBoundaryCallbacks(sessionContext, options.publicPendingKind)
              : {})
          })
        : signDirectAndBroadcast(data.signDoc, {
            ...broadcastOptions,
            sessionContext,
            publicPendingKind: options.publicPendingKind || "",
            publicTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
          });
    });
    assertPrivacySession(sessionContext);
    if (state.activeWallet === "metamask"
      && data.reservationKind !== "batch-transfer"
      && data.reservation) {
      const persistVerifiedResult = async result => {
        assertPrivacySession(sessionContext);
        if (!result?.unknown) {
          verifiedEvmTransactionResult(result, label);
          await updatePreparedEvmOperationArtifact(data, {
            phase: "receipt-verified",
            txHash: result.txHash,
            receiptResult: result
          }, { sessionContext });
        }
        return result;
      };
      if (broadcast.pending && broadcast.waitPromise) {
        broadcast = {
          ...broadcast,
          waitPromise: broadcast.waitPromise.then(persistVerifiedResult)
        };
        broadcast.waitPromise.catch(() => {});
      } else if (!broadcast.unknown) {
        broadcast = await persistVerifiedResult(broadcast);
      }
    }
    await refreshReservationState(data.reservationManager, { sessionContext });
    assertPrivacySession(sessionContext);
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
    if (isStalePrivacySessionError(error)) throw error;
    await refreshReservationState(data.reservationManager, { sessionContext }).catch(() => {});
    throw error;
  }
}

function evmReceiptHasFailed(receipt) {
  return hasFailedEvmReceiptStatus(receipt);
}

function evmReceiptSucceeded(receipt) {
  const status = receipt?.status;
  if (status === 1 || status === 1n || status === true) return true;
  const text = String(status ?? "").trim().toLowerCase();
  if (text === "1") return true;
  if (/^0x[0-9a-f]+$/.test(text)) {
    try {
      return BigInt(text) === 1n;
    } catch {
      return false;
    }
  }
  return false;
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
      successful: hasSuccessfulEvmReceiptStatus(receipt),
      failed: hasFailedEvmReceiptStatus(receipt),
      absent: !receipt && !transaction,
      pending: !receipt && Boolean(transaction),
      height: receipt?.blockNumber || 0,
      receipt,
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
    successful: Boolean(tx) && code === 0,
    failed: Boolean(tx) && code !== null && code !== 0,
    absent: !tx,
    pending: false,
    height: tx?.height || 0,
    transaction: tx
  };
}

async function clearReconciledCosmosPrivacyPending({
  manager,
  records = [],
  transactionCheck = null,
  sessionContext = privacySessionSnapshot(),
  accountTransactionLockHeld = false
} = {}) {
  if (activeChainProfile()?.transport !== "cosmos" || !manager) return false;
  assertPrivacySession(sessionContext);
  const marker = capturedPrivacyPendingState(sessionContext);
  if (!marker?.txHash) return false;
  const markerHash = normalizedHex(marker.txHash);
  const matchingOperations = groupReservationOperations(records).filter(operation => (
    commonCosmosReservationTransactionHash(operation.records) === markerHash
  ));
  const linked = matchingOperations.flatMap(operation => operation.records);
  if (!linked.length) return false;

  const check = transactionCheck || await checkReservationTransaction(marker.txHash);
  assertPrivacySession(sessionContext);
  const successReconciled = check?.included === true
    && check?.successful === true
    && linked.every(record => (
      record.status === reservationStatuses.ConfirmedSpent
      && (!reservationRequiresOperationEvidence(record)
        || operationReconciliationStatus(record) === operationStatuses.Succeeded)
    ));
  const failureReconciled = check?.included === true
    && check?.failed === true
    && linked.every(record => [
      reservationStatuses.ReplanRequired,
      reservationStatuses.Failed,
      reservationStatuses.Released
    ].includes(record.status));
  const explicitRejectionReconciled = check?.included !== true
    && linked.every(record => (
      [
        reservationStatuses.ReplanRequired,
        reservationStatuses.Failed,
        reservationStatuses.Released
      ].includes(record.status)
        && reservationHasExplicitBroadcastRejection(record)
    ));
  if (!successReconciled && !failureReconciled && !explicitRejectionReconciled) return false;

  const clear = () => {
    assertPrivacySession(sessionContext);
    const latest = capturedPrivacyPendingState(sessionContext);
    if (!latest || normalizedHex(latest.txHash) !== markerHash) return false;
    clearCapturedPublicPendingTransaction(sessionContext, "privacy", latest.txHash);
    renderReservationState();
    updateAmountActionButtons();
    return true;
  };
  return accountTransactionLockHeld
    ? clear()
    : withPublicTransactionLock(sessionContext, clear);
}

function clearedRelayWithdrawState(resultStatus, resultMessage) {
  return {
    handoff: null,
    json: "",
    reservationIds: [],
    payloadHash: "",
    expiresAtUnix: 0,
    durableNoBroadcast: false,
    payloadUnavailable: false,
    txHash: "",
    submittedBy: "",
    evmSender: "",
    evmReceiptEvidence: null,
    externalHandoff: false,
    resultStatus,
    resultMessage
  };
}

async function recoverExpiredRelayWithdraw({
  manager,
  records,
  chainBlock,
  check,
  reconciliationContext,
  notes
}) {
  assertRelayReconciliationContext(reconciliationContext);
  const { payload, sessionContext } = reconciliationContext;
  if (!relayWithdrawPayloadExpired(payload, chainBlock.timeUnix)) return false;
  const unspentIDs = await explicitlyUnspentReservationIDs(
    manager,
    records,
    notes,
    () => assertRelayReconciliationContext(reconciliationContext)
  );
  assertRelayReconciliationContext(reconciliationContext);
  if (!records.length || unspentIDs.length !== records.length) {
    stopRelayReservationHeartbeat(reconciliationContext.heartbeatGeneration);
    assertRelayReconciliationContext(reconciliationContext);
    state.relayWithdraw.resultStatus = "manual-review";
    state.relayWithdraw.resultMessage = `Payload expired at chain time ${chainBlock.timeUnix}, but every reserved nullifier is not confirmed unspent`;
    setWithdrawEvidence(
      "Spent or unspent evidence incomplete · manual review",
      "Payload expired · transparent receive not established",
      { render: false }
    );
    return true;
  }

  const durableNoBroadcast = state.relayWithdraw.durableNoBroadcast === true
    && state.relayWithdraw.externalHandoff !== true
    && !state.relayWithdraw.txHash
    && records.every(record => record.status === reservationStatuses.ProofReady
      && record.broadcast_in_flight !== true
      && !record.submitted_tx_hash
      && record.metadata?.relay_handed_off !== true);
  const queryableNoSuccess = check?.checked === true && (check.failed === true || check.absent === true);
  if (!durableNoBroadcast && !queryableNoSuccess) {
    const proofReady = records.every(record => record.status === reservationStatuses.ProofReady);
    const leaseTokens = [...new Set(records.map(record => record.lease_token).filter(Boolean))];
    if (proofReady && leaseTokens.length === 1) {
      await manager.markManualReview(unspentIDs, {
        leaseToken: leaseTokens[0],
        error: "relay_expired_without_submission_evidence",
        metadata: {
          relay_payload_expired: true,
          nullifier_unspent_confirmed: true,
          checked_height: chainBlock.height,
          checked_chain_time_unix: chainBlock.timeUnix,
          recovery_blocked_reason: "missing_no_broadcast_or_queryable_transaction_evidence"
        }
      });
      assertRelayReconciliationContext(reconciliationContext);
    }
    stopRelayReservationHeartbeat(reconciliationContext.heartbeatGeneration);
    assertRelayReconciliationContext(reconciliationContext);
    state.relayWithdraw.resultStatus = "manual-review";
    state.relayWithdraw.resultMessage = "Payload expired, but no durable no-broadcast or queryable transaction evidence exists · reservation remains locked";
    setWithdrawEvidence(
      `Unspent · confirmed at height ${chainBlock.height}`,
      "Payload expired · submission outcome requires manual review",
      { render: false }
    );
    return true;
  }

  const approved = globalThis.confirm(
    `Relay payload가 chain height ${chainBlock.height}에서 만료되었고 모든 nullifier가 unspent로 확인되었습니다.\n\n` +
    "이 handoff를 종료하고 새 withdraw payload를 만들 수 있도록 reservation을 재계획할까요?"
  );
  assertRelayReconciliationContext(reconciliationContext);
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
    durable_no_broadcast_confirmed: durableNoBroadcast,
    queryable_failed_or_absent_transaction: queryableNoSuccess,
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
    assertRelayReconciliationContext(reconciliationContext);
  } else if (!(statuses.size === 1 && statuses.has(reservationStatuses.ManualReview))) {
    throw new Error(`Expired relay reservation has an unsupported recovery status: ${[...statuses].join(", ")}`);
  }

  stopRelayReservationHeartbeat(reconciliationContext.heartbeatGeneration);
  assertRelayReconciliationContext(reconciliationContext);
  await manager.resolveManualReview(unspentIDs, {
    target: reservationStatuses.ReplanRequired,
    operatorId: sessionContext.account,
    approvalReference: `relay-expiry:${payload.payload_hash}:${chainBlock.height}`,
    reason: "Wallet owner approved replan after authoritative relay expiry and unspent reconciliation",
    metadata: evidence
  });
  assertRelayReconciliationContext(reconciliationContext);
  await refreshReservationState(manager, { sessionContext, notes });
  assertRelayReconciliationContext(reconciliationContext);
  reconciliationContext.operationReplaced = true;
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

async function quarantineRelayWithdrawOperation({
  manager,
  records = [],
  check = {},
  error,
  reason,
  reconciliationContext
}) {
  assertRelayReconciliationContext(reconciliationContext);
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
            ...(check.txHash ? { tx_hash_checked: check.txHash } : {}),
            ...(check.height ? { checked_height: check.height } : {})
          }
        });
        assertRelayReconciliationContext(reconciliationContext);
      } else {
        reservationError = "relay reservations do not share one reviewable lease token";
      }
    } catch (transitionError) {
      if (isStalePrivacySessionError(transitionError)) throw transitionError;
      reservationError = transitionError.message;
    }
  }
  stopRelayReservationHeartbeat(reconciliationContext.heartbeatGeneration);
  assertRelayReconciliationContext(reconciliationContext);
  state.relayWithdraw.resultStatus = "manual-review";
  state.relayWithdraw.resultMessage = `Manual review required · ${message}${reservationError ? ` · ${reservationError}` : ""}`;
  setWithdrawEvidence(
    "Spent or binding evidence conflicts · manual review",
    "Transparent receive is not safely attributable to this handoff",
    { render: false }
  );
}

function captureRelaySubmitContext() {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const handoff = state.relayWithdraw.handoff;
  const relayer = localRelayerAccount();
  return {
    sessionContext,
    profileId: activeChainProfile()?.id || "",
    account: state.keplr.account,
    rootSignatureHash: state.keplr.rootSignatureHash,
    storageEpoch: state.chainStorageEpoch,
    relayEndpoint: new URL("/api/relayer/withdraw", document.baseURI).href,
    relayerName: String(relayer?.name || ""),
    relayerAddress: String(relayer?.transparentAddress || ""),
    handoffVersion: [
      handoff?.schema_version,
      handoff?.handoff_version,
      handoff?.request?.version
    ].join(":"),
    payloadHash: relayWithdrawHandoffPayload(handoff)?.payload_hash || ""
  };
}

function assertRelaySubmitContext(context) {
  assertPrivacySession(context?.sessionContext);
  const current = captureRelaySubmitContext();
  for (const key of [
    "profileId",
    "account",
    "rootSignatureHash",
    "storageEpoch",
    "relayEndpoint",
    "relayerName",
    "relayerAddress",
    "handoffVersion",
    "payloadHash"
  ]) {
    if (current[key] !== context[key]) {
      throw stalePrivacySessionError(context?.sessionContext);
    }
  }
  if (context.manager && reservationManager !== context.manager) {
    throw stalePrivacySessionError(context?.sessionContext);
  }
  return context;
}

function captureRelayReconciliationContext({ candidateTxHash = "" } = {}) {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const handoff = state.relayWithdraw.handoff;
  const handoffPayload = relayWithdrawHandoffPayload(handoff);
  const payloadHash = String(handoffPayload?.payload_hash || state.relayWithdraw.payloadHash || "").trim().toLowerCase();
  const expiresAtUnix = Number(handoffPayload?.expires_at_unix || state.relayWithdraw.expiresAtUnix || 0);
  const metadataOnly = !handoff && state.relayWithdraw.payloadUnavailable === true;
  const payload = handoffPayload || (metadataOnly ? {
    payload_hash: payloadHash,
    expires_at_unix: expiresAtUnix
  } : null);
  const persistedTxHash = normalizedHex(state.relayWithdraw.txHash);
  const normalizedCandidateTxHash = normalizedHex(candidateTxHash);
  const localSubmissionAttempted = state.relayWithdraw.externalHandoff !== true
    && state.relayWithdraw.durableNoBroadcast === false
    && Boolean(payloadHash);
  return {
    sessionContext,
    handoff,
    transport: handoff?.transport || activeChainProfile()?.transport || "",
    payload,
    metadataOnly,
    externalHandoff: state.relayWithdraw.externalHandoff === true,
    payloadHash,
    expiresAtUnix,
    txHash: normalizedCandidateTxHash || persistedTxHash,
    persistedTxHash,
    candidateExternalTxHash: Boolean(normalizedCandidateTxHash
      && normalizedCandidateTxHash !== persistedTxHash
      && state.relayWithdraw.externalHandoff === true),
    candidateTxHashUnbound: Boolean(normalizedCandidateTxHash
      && normalizedCandidateTxHash !== persistedTxHash),
    localSubmissionAttempted,
    reservationIDs: [...state.relayWithdraw.reservationIds],
    leaseToken: state.relayWithdraw.leaseToken || "",
    evmSender: String(state.relayWithdraw.evmSender || "").trim().toLowerCase(),
    heartbeatGeneration: relayReservationHeartbeatGeneration,
    expectedEvmChainId: activeChainProfile()?.evmChainId,
    operationReplaced: false
  };
}

async function reconcileRelayWithdrawFromInput() {
  const raw = String(els.relayWithdrawTxHash.value || "").trim();
  if (!raw) return reconcileRelayWithdrawResult();
  if (!/^(0x)?[0-9a-fA-F]{64}$/.test(raw)) {
    throw new Error("External relayer tx hash must be a 32-byte hex value");
  }
  const txHash = normalizedHex(raw);
  const persistedTxHash = normalizedHex(state.relayWithdraw.txHash);
  const metadataOnlyRecovery = !state.relayWithdraw.handoff
    && state.relayWithdraw.payloadUnavailable === true
    && ["cosmos", "evm"].includes(activeChainProfile()?.transport);
  if (persistedTxHash && persistedTxHash !== txHash) {
    throw new Error("The relayer tx hash is already bound to a different transaction");
  }
  if (!persistedTxHash
    && (state.relayWithdraw.externalHandoff !== true
      || (!state.relayWithdraw.handoff && !metadataOnlyRecovery))) {
    throw new Error("A current handoff or restorable relay payload hash is required before attaching its transaction hash");
  }
  return reconcileRelayWithdrawResult({ candidateTxHash: txHash });
}

function assertRelayReconciliationContext(context) {
  assertPrivacySession(context?.sessionContext);
  if (!context
    || state.relayWithdraw.handoff !== context.handoff
    || normalizedHex(state.relayWithdraw.txHash) !== context.persistedTxHash
    || (state.relayWithdraw.externalHandoff === true) !== context.externalHandoff
    || String(state.relayWithdraw.payloadHash || context.payloadHash || "").trim().toLowerCase() !== context.payloadHash
    || Number(state.relayWithdraw.expiresAtUnix || context.expiresAtUnix || 0) !== context.expiresAtUnix
    || String(state.relayWithdraw.leaseToken || "") !== context.leaseToken
    || String(state.relayWithdraw.evmSender || "").trim().toLowerCase() !== context.evmSender
    || JSON.stringify(state.relayWithdraw.reservationIds || []) !== JSON.stringify(context.reservationIDs)) {
    throw stalePrivacySessionError(context?.sessionContext);
  }
  return context;
}

async function verifyRelayEvmReceiptAndFinality({
  handoff,
  operationArtifact,
  txHash,
  check,
  reconciliationContext
}) {
  const transport = handoff?.transport || reconciliationContext?.transport;
  if (transport !== "evm") return true;
  assertRelayReconciliationContext(reconciliationContext);
  if (!check?.included) return false;
  if (check.successful !== true) {
    if (check.failed) return false;
    throw new Error("EVM relay receipt does not contain an explicit successful status");
  }
  const privacyTransaction = operationArtifact?.transaction || handoff?.transaction || null;
  if (!privacyTransaction) {
    throw new Error(
      "EVM relay finality cannot be verified without the encrypted original SDK-prepared transaction"
    );
  }
  const sender = String(
    reconciliationContext?.evmSender
      || check.transaction?.from
      || ""
  ).trim().toLowerCase();
  if (!/^0x[0-9a-f]{40}$/.test(sender)) {
    throw new Error("EVM relay sender is unavailable; transaction and finality evidence cannot be verified");
  }
  const result = await clairveilBrowserClient().waitForEvmTransaction(txHash, {
    privacyTransaction,
    sender
  });
  assertRelayReconciliationContext(reconciliationContext);
  verifiedEvmTransactionResult(result, "EVM relay withdraw");
  state.relayWithdraw.evmSender = sender;
  reconciliationContext.evmSender = sender;
  state.relayWithdraw.evmReceiptEvidence = result;
  return true;
}

function relayPreparedWithdraw() {
  return runValueMovingAction("relay-submit", relayPreparedWithdrawUnlocked);
}

function assertLocalRelaySubmissionAvailable() {
  if (relayHandoffInFlight || state.relayWithdraw.externalHandoff) {
    throw new Error("This payload is crossing or already crossed an external handoff boundary; reconcile it instead of submitting locally");
  }
}

async function relayPreparedWithdrawUnlocked() {
  const handoff = state.relayWithdraw.handoff;
  const payload = relayWithdrawHandoffPayload(handoff);
  const relayer = localRelayerAccount();
  if (!handoff || !payload) throw new Error("Prepare a relay withdraw payload first");
  if (!serverFeature("relayer") || !relayer?.transparentAddress) {
    throw new Error("Local relayer helper is unavailable");
  }
  if (String(relayer.transparentAddress).trim().toLowerCase()
    === String(state.keplr.account || "").trim().toLowerCase()) {
    throw new Error("The built-in local relayer must use a separate server-side account");
  }
  assertLocalRelaySubmissionAvailable();
  if (state.relayWithdraw.resultStatus !== "ready") {
    throw new Error("This relay payload is already being submitted or requires reconciliation");
  }
  const context = captureRelaySubmitContext();
  const { sessionContext } = context;
  const reservationIDs = [...state.relayWithdraw.reservationIds];
  const leaseToken = state.relayWithdraw.leaseToken;
  state.relayWithdraw.resultStatus = "preflighting";
  state.relayWithdraw.resultMessage = "Checking fresh chain time and reserved nullifier state…";
  setBusy(els.relayPreparedWithdraw, true);
  renderRelayWithdraw();
  let manager = null;
  let attemptMarkerStarted = false;
  try {
    manager = await currentReservationManager();
    assertRelaySubmitContext(context);
    if (!manager || !reservationIDs.length || !leaseToken) {
      throw new Error("Relay withdraw reservation state is unavailable");
    }
    context.manager = manager;
    assertRelaySubmitContext(context);
    const chainBlock = await fetchLatestChainBlock();
    assertRelaySubmitContext(context);
    if (relayWithdrawPayloadExpired(payload, chainBlock.timeUnix)) {
      throw new Error(`Relay payload expired at authoritative chain height ${chainBlock.height}`);
    }
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      skipSetup: true,
      maxPages: 1000,
      sessionContext,
      reservationManager: manager
    });
    assertRelaySubmitContext(context);
    const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
    const checkedHeight = checkedReservationHeight();
    if (Boolean(cursor.has_more ?? cursor.hasMore)
      || !checkedHeight
      || checkedHeight < chainBlock.height) {
      throw new Error("Reserved nullifier preflight did not reach the authoritative chain height");
    }
    const records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
    assertRelaySubmitContext(context);
    assertRelayReservationPayloadMatches(records, payload);
    const unspentIDs = await explicitlyUnspentReservationIDs(
      manager,
      records,
      state.keplr.notes,
      () => assertRelaySubmitContext(context)
    );
    assertRelaySubmitContext(context);
    if (!records.length || unspentIDs.length !== records.length) {
      throw new Error("Every reserved nullifier must be explicitly unspent before local relay submission");
    }

    assertLocalRelaySubmissionAvailable();
    assertRelaySubmitContext(context);
    stopRelayReservationHeartbeat();
    attemptMarkerStarted = true;
    await manager.markBroadcastAttempting(reservationIDs, {
      leaseToken,
      reason: "same_origin_local_relayer_submit",
      metadata: {
        local_relayer: relayer.name,
        local_relayer_address: relayer.transparentAddress,
        payload_hash: context.payloadHash,
        nullifier_unspent_confirmed: true,
        checked_height: checkedHeight,
        checked_chain_time_unix: chainBlock.timeUnix
      }
    });
    assertRelaySubmitContext(context);
    state.relayWithdraw.durableNoBroadcast = false;
    state.relayWithdraw.resultStatus = "submitting";
    state.relayWithdraw.resultMessage = "Local relayer is paying the fee and broadcasting…";
    await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext });
    assertRelaySubmitContext(context);
    await refreshReservationState(manager, { sessionContext });
    assertRelaySubmitContext(context);
    const relay = await api(context.relayEndpoint, {
      method: "POST",
      body: JSON.stringify({
        handoff,
        expectedRecipient: payload.recipient,
        relayer: relayer.name
      })
    });
    assertRelaySubmitContext(context);
    const txHash = String(relay.broadcast?.txhash || "").trim();
    if (!/^(0x)?[0-9a-fA-F]{64}$/.test(txHash)) {
      throw new Error("Local relayer response omitted a valid transaction hash");
    }
    if (relay.relayerAddress
      && String(relay.relayerAddress).trim().toLowerCase()
        !== String(relayer.transparentAddress).trim().toLowerCase()) {
      throw new Error("Local relayer response account does not match the configured server relayer account");
    }
    state.relayWithdraw.txHash = txHash;
    state.relayWithdraw.evmSender = String(relay.relayerEvmAddress || "").trim();
    state.relayWithdraw.submittedBy = relay.relayerAddress
      ? `${relay.relayer || relayer.name} · ${shorten(relay.relayerAddress, 14, 12)}`
      : relay.relayer || relayer.name;
    await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext });
    assertRelaySubmitContext(context);
    const relayMetadata = {
      local_relayer: relayer.name,
      local_relayer_address: relayer.transparentAddress,
      payload_hash: relay.payloadHash || context.payloadHash
    };
    if (relay.unknown === true) {
      await manager.markUnknown(reservationIDs, {
        fromStatus: reservationStatuses.ProofReady,
        leaseToken,
        txHash,
        error: "local_relayer_included_status_unknown",
        metadata: relayMetadata
      });
    } else {
      await manager.markSubmitted(reservationIDs, {
        leaseToken,
        txHash,
        metadata: relayMetadata
      });
    }
    assertRelaySubmitContext(context);
    state.relayWithdraw.resultStatus = relay.unknown === true
      ? "unknown"
      : relay.pending ? "submitted" : "checking";
    state.relayWithdraw.resultMessage = relay.unknown === true
      ? "Relayer result unknown · reconcile the saved tx hash and do not retry"
      : relay.pending
        ? "Relayer broadcast submitted · waiting for inclusion"
        : "Relayer tx included · reconciling payload and nullifier evidence";
    await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext });
    assertRelaySubmitContext(context);
    renderRelayWithdraw();
    await reconcileRelayWithdrawResult();
    assertPrivacySession(sessionContext);
    await refreshRelayerAccount({ sessionContext });
    assertPrivacySession(sessionContext);
    toast(relay.unknown === true
      ? "Relay result unknown · reconcile before retrying"
      : relay.pending ? "Relay withdraw submitted" : "Relay withdraw included");
  } catch (error) {
    if (!privacySessionIsCurrent(sessionContext)) {
      throw stalePrivacySessionError(sessionContext);
    }
    if (attemptMarkerStarted
      && manager
      && error?.checkTxRejected === true
      && error?.rpcInvoked === true
      && Number.isSafeInteger(error?.txCode)
      && error.txCode > 0) {
      const rejectedTxHash = /^(0x)?[0-9a-fA-F]{64}$/.test(String(error.txHash || "").trim())
        ? String(error.txHash).trim()
        : "";
      const quarantineErrors = [];
      let markedUnknown = false;
      if (rejectedTxHash) {
        try {
          assertRelaySubmitContext(context);
          await manager.markUnknown(reservationIDs, {
            fromStatus: reservationStatuses.ProofReady,
            leaseToken,
            txHash: rejectedTxHash,
            error: "local_relayer_check_tx_rejected",
            metadata: {
              local_relayer: relayer.name,
              local_relayer_address: relayer.transparentAddress,
              payload_hash: context.payloadHash,
              check_tx_rejected: true,
              rpc_invoked: true,
              provider_code: String(error.txCode)
            }
          });
          assertRelaySubmitContext(context);
          state.relayWithdraw.txHash = rejectedTxHash;
          markedUnknown = true;
        } catch (recoveryError) {
          if (isStalePrivacySessionError(recoveryError)) throw recoveryError;
          quarantineErrors.push(recoveryError);
        }
      }
      try {
        assertRelaySubmitContext(context);
        await manager.markManualReview(reservationIDs, {
          ...(!markedUnknown ? { leaseToken } : {}),
          error: "local_relayer_check_tx_rejected",
          metadata: {
            local_relayer: relayer.name,
            local_relayer_address: relayer.transparentAddress,
            payload_hash: context.payloadHash,
            reconcile_reason: "local_relayer_check_tx_rejected",
            check_tx_rejected: true,
            rpc_invoked: true,
            provider_code: String(error.txCode),
            automatic_replan_blocked: true
          }
        });
        assertRelaySubmitContext(context);
      } catch (recoveryError) {
        if (isStalePrivacySessionError(recoveryError)) throw recoveryError;
        quarantineErrors.push(recoveryError);
      }
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = quarantineErrors.length
        ? `Node rejected the relay transaction, and reservation quarantine requires review · ${quarantineErrors.map(item => item.message).join(" · ")}`
        : `Node rejected the relay transaction · reservations remain locked for authoritative failure reconciliation · ${error.message}`;
      await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext });
      assertRelaySubmitContext(context);
      await refreshReservationState(manager, { sessionContext });
      assertRelaySubmitContext(context);
      renderRelayWithdraw();
      throw error;
    }
    try {
      assertRelaySubmitContext(context);
      state.relayWithdraw.resultStatus = attemptMarkerStarted ? "unknown" : "ready";
      state.relayWithdraw.resultMessage = attemptMarkerStarted
        ? `Local relayer result is unknown · ${error.message}`
        : `Relay preflight failed before broadcast · ${error.message}`;
      await persistRelayWithdrawRecovery(state.relayWithdraw, { sessionContext });
      assertRelaySubmitContext(context);
      if (manager) await refreshReservationState(manager, { sessionContext });
      assertRelaySubmitContext(context);
      renderRelayWithdraw();
    } catch (recoveryError) {
      if (isStalePrivacySessionError(recoveryError)) throw recoveryError;
      // A changed wallet/session must not receive the previous submission result.
    }
    throw error;
  } finally {
    try {
      assertPrivacySession(sessionContext);
      setBusy(els.relayPreparedWithdraw, false);
      renderKeplr();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function reconcileRelayWithdrawResult({ candidateTxHash = "" } = {}) {
  const reconciliationContext = captureRelayReconciliationContext({ candidateTxHash });
  const {
    handoff,
    transport,
    payload,
    metadataOnly,
    reservationIDs,
    sessionContext
  } = reconciliationContext;
  let txHash = reconciliationContext.txHash;
  if (!handoff && !metadataOnly) throw new Error("Prepare and hand off a relay withdraw payload first");
  if (txHash && !/^(0x)?[0-9a-fA-F]{64}$/.test(txHash)) {
    throw new Error("Relayer tx hash must be a 32-byte hex value");
  }
  let manager;
  let store;
  let operationIdentity;
  let localRelayRecovery = null;
  let relayOperationArtifact = null;
  let relayOperationKey = "";
  try {
    manager = await currentReservationManager();
    assertRelayReconciliationContext(reconciliationContext);
    if (!manager || !reservationIDs.length) {
      throw new Error("Relay withdraw reservation state is unavailable");
    }
    store = await currentOperationStore();
    assertRelayReconciliationContext(reconciliationContext);
    operationIdentity = operationStoreIdentity();
    if (!store || !operationIdentity) {
      throw new Error("Encrypted operation recovery store is not available");
    }
    assertRelayReconciliationContext(reconciliationContext);
    state.relayWithdraw.resultStatus = "checking";
    state.relayWithdraw.resultMessage = "Checking tx result first, then nullifier spent state…";
    await persistRelayWithdrawRecovery(state.relayWithdraw, {
      store,
      identity: operationIdentity,
      sessionContext
    });
    assertRelayReconciliationContext(reconciliationContext);
    renderRelayWithdraw();
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  try {
    if (!txHash
      && reconciliationContext.localSubmissionAttempted
      && serverFeature("relayer")) {
      const recovered = await api("/api/relayer/withdraw/reconcile", {
        method: "POST",
        body: JSON.stringify({ payloadHash: reconciliationContext.payloadHash })
      });
      assertRelayReconciliationContext(reconciliationContext);
      localRelayRecovery = recovered;
      const recoveredTxHash = normalizedHex(
        transactionHashFromEvidence(recovered?.result || {})
          || transactionHashFromEvidence(recovered?.evidence || {})
      );
      if (recovered?.found === true && recoveredTxHash) {
        txHash = recoveredTxHash;
        reconciliationContext.txHash = recoveredTxHash;
        reconciliationContext.candidateTxHashUnbound = true;
      }
    }
    const check = txHash
      ? await checkReservationTransaction(txHash)
      : { checked: false, txHash: "", included: false, failed: false, absent: false, pending: false };
    assertRelayReconciliationContext(reconciliationContext);
    let records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
    assertRelayReconciliationContext(reconciliationContext);
    if (transport === "evm") {
      relayOperationKey = reservationGroupOperationKey(records);
      relayOperationArtifact = await loadEvmOperationArtifactForReservations(records, { sessionContext });
      assertRelayReconciliationContext(reconciliationContext);
      if (!relayOperationArtifact?.transaction
        || relayOperationArtifact.kind !== "relay-withdraw") {
        throw new Error(
          "EVM relay recovery requires its encrypted original SDK-prepared transaction artifact"
        );
      }
    }
    let evmFinalityVerified = transport !== "evm";
    try {
      assertRelayReservationPayloadMatches(records, payload);
      if (txHash && records.some(record => record.submitted_tx_hash
        && normalizedHex(record.submitted_tx_hash) !== normalizedHex(txHash))) {
        throw new Error("relay recovery transaction hash does not match the reserved submission identity");
      }
      if (check.included) {
        if (metadataOnly && transport === "cosmos") {
          assertCosmosRelayWithdrawTransactionPayloadHash({
            transaction: check.transaction,
            payloadHash: reconciliationContext.payloadHash
          });
        } else if (metadataOnly) {
          if (!reconciliationContext.candidateTxHashUnbound && records.some(record => (
            !record.submitted_tx_hash
            || normalizedHex(record.submitted_tx_hash) !== normalizedHex(txHash)
          ))) {
            throw new Error("metadata-only EVM relay recovery requires every reservation to bind the same submitted transaction hash");
          }
          assertRelayWithdrawTransactionMatches({
            transport: "evm",
            payload,
            handoffTransaction: relayOperationArtifact.transaction,
            transaction: check.transaction,
            expectedEvmChainId: reconciliationContext.expectedEvmChainId
          });
        } else {
          assertRelayWithdrawTransactionMatches({
            transport: handoff.transport,
            payload,
            handoffTransaction: handoff.transaction,
            transaction: check.transaction,
            expectedEvmChainId: reconciliationContext.expectedEvmChainId
          });
        }
      }
    } catch (error) {
      if (reconciliationContext.candidateTxHashUnbound) {
        state.relayWithdraw.resultStatus = "waiting";
        state.relayWithdraw.resultMessage = `Relayer tx hash was not attached · ${error.message}`;
        setWithdrawEvidence(
          "Reserved · submitted transaction not safely bound",
          "Candidate transaction did not match this relay payload",
          { render: false }
        );
        return;
      }
      await quarantineRelayWithdrawOperation({
        manager,
        records,
        check,
        error,
        reason: "relay_transaction_binding_conflict",
        reconciliationContext
      });
      assertRelayReconciliationContext(reconciliationContext);
      return;
    }
    if (check.included) {
      evmFinalityVerified = await verifyRelayEvmReceiptAndFinality({
        handoff,
        operationArtifact: relayOperationArtifact,
        txHash,
        check,
        reconciliationContext
      });
      assertRelayReconciliationContext(reconciliationContext);
    }

    if (!txHash && reconciliationContext.localSubmissionAttempted) {
      if (localRelayRecovery?.found === true && localRelayRecovery?.settled !== true) {
        state.relayWithdraw.resultStatus = "submitted";
        state.relayWithdraw.resultMessage = "Local relayer submission is still running · do not retry";
        setWithdrawEvidence(
          "Reserved · local relayer submission still running",
          "Waiting for the original relayer result",
          { render: false }
        );
        return;
      }
      await manager.markManualReview(reservationIDs, {
        ...(reconciliationContext.leaseToken ? { leaseToken: reconciliationContext.leaseToken } : {}),
        error: "local_relayer_transaction_identity_unavailable",
        metadata: {
          reconcile_reason: "local_relayer_transaction_identity_unavailable",
          opaque_broadcast_attempt: true
        }
      });
      assertRelayReconciliationContext(reconciliationContext);
      state.relayWithdraw.resultStatus = "manual-review";
      state.relayWithdraw.resultMessage = "Local relayer transaction identity is unavailable · manual review required; do not retry";
      setWithdrawEvidence(
        "Manual review · opaque local relayer attempt",
        "Transaction identity unavailable",
        { render: false }
      );
      return;
    }

    if (reconciliationContext.candidateTxHashUnbound && !check.included) {
      state.relayWithdraw.resultStatus = "waiting";
      state.relayWithdraw.resultMessage = check.pending
        ? "Candidate relayer tx is pending · it was not attached; reconcile again after inclusion"
        : "Candidate relayer tx is not included · it was not attached to the reservation";
      setWithdrawEvidence(
        "Reserved · candidate transaction not yet included",
        check.pending ? "Pending transaction inclusion" : "Candidate transaction not found",
        { render: false }
      );
      return;
    }

    const txAlreadyBound = records.length > 0 && records.every(record => (
      normalizedHex(record.submitted_tx_hash) === normalizedHex(txHash)
    ));
    if (reconciliationContext.candidateTxHashUnbound) {
      const previousTxHash = state.relayWithdraw.txHash;
      const previousSubmittedBy = state.relayWithdraw.submittedBy;
      state.relayWithdraw.txHash = txHash;
      state.relayWithdraw.submittedBy = reconciliationContext.candidateExternalTxHash
        ? "external relayer · chain-verified tx hash"
        : "local relayer · recovered chain-verified tx hash";
      reconciliationContext.persistedTxHash = txHash;
      try {
        await persistRelayWithdrawRecovery(state.relayWithdraw, {
          store,
          identity: operationIdentity,
          sessionContext
        });
        assertRelayReconciliationContext(reconciliationContext);
        reconciliationContext.candidateExternalTxHash = false;
        reconciliationContext.candidateTxHashUnbound = false;
      } catch (error) {
        if (privacySessionIsCurrent(sessionContext)) {
          state.relayWithdraw.txHash = previousTxHash;
          state.relayWithdraw.submittedBy = previousSubmittedBy;
          reconciliationContext.persistedTxHash = normalizedHex(previousTxHash);
        }
        throw error;
      }
    }

    if (check.included
      && (reconciliationContext.externalHandoff || reconciliationContext.localSubmissionAttempted)
      && !txAlreadyBound) {
      if (typeof manager.recordRelayTransactionEvidence !== "function") {
        throw new Error(
          "ClairveilJS does not provide authoritative relay transaction evidence binding; " +
          "reservations were left unchanged for manual review and this relay payload must not be resubmitted"
        );
      }
      const operationKeys = [...new Set(records.map(reservationOperationKey).filter(Boolean))];
      const checkedHeight = checkedReservationHeight(check);
      if (operationKeys.length !== 1 || !checkedHeight) {
        throw new Error("Included relay transaction evidence is missing one exact operation identity or checked height");
      }
      const evidenceTxHash = transport === "evm"
        ? `0x${normalizedHex(txHash)}`
        : normalizedHex(txHash).toUpperCase();
      const boundRecords = await manager.recordRelayTransactionEvidence({
        operationId: operationKeys[0],
        payloadHash: reconciliationContext.payloadHash,
        txHash: evidenceTxHash,
        checkedHeight,
        transactionIncludedConfirmed: true,
        payloadHashMatched: true
      });
      assertRelayReconciliationContext(reconciliationContext);
      const expectedReservationIDs = [...reservationIDs].sort();
      const boundReservationIDs = (boundRecords || [])
        .map(record => String(record?.reservation_id || ""))
        .filter(Boolean)
        .sort();
      if (JSON.stringify(boundReservationIDs) !== JSON.stringify(expectedReservationIDs)) {
        throw new Error("Included relay transaction evidence did not bind the exact encrypted recovery reservation set");
      }
      records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
      assertRelayReconciliationContext(reconciliationContext);
      if (records.some(record => normalizedHex(record.submitted_tx_hash) !== normalizedHex(txHash))) {
        throw new Error("Included relay transaction evidence was not bound to every linked reservation");
      }
    }

    await refreshEvents({ allowFailure: true, sessionContext });
    assertRelayReconciliationContext(reconciliationContext);
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      sessionContext,
      reservationManager: manager
    });
    assertRelayReconciliationContext(reconciliationContext);
    const reconciledNotes = [...state.keplr.notes];
    records = await Promise.all(reservationIDs.map(id => manager.getReservation(id)));
    assertRelayReconciliationContext(reconciliationContext);
    const spentConfirmed = records.length > 0
      && records.every(record => record.status === reservationStatuses.ConfirmedSpent);
    const receiveConfirmed = check.included
      && check.successful === true
      && evmFinalityVerified
      && records.length > 0
      && records.every(record => !reservationRequiresOperationEvidence(record)
        || operationReconciliationStatus(record) === operationStatuses.Succeeded);

    if (!check.included || check.failed) {
      const chainBlock = await fetchLatestChainBlock();
      assertRelayReconciliationContext(reconciliationContext);
      if (await recoverExpiredRelayWithdraw({
        manager,
        records,
        chainBlock,
        check,
        reconciliationContext,
        notes: reconciledNotes
      })) return;
      assertRelayReconciliationContext(reconciliationContext);
    }

    if (spentConfirmed && check.successful !== true) {
      await quarantineRelayWithdrawOperation({
        manager,
        records,
        check,
        error: new Error(check.failed
          ? "A failed relayer transaction cannot explain the spent nullifier"
          : check.included
            ? "An included transaction with an unknown execution status cannot explain the spent nullifier"
            : "The spent nullifier is not attributable to an included relayer transaction"),
        reason: "relay_spent_without_successful_bound_transaction",
        reconciliationContext
      });
      assertRelayReconciliationContext(reconciliationContext);
      return;
    }

    if (check.failed) {
      assertRelayReconciliationContext(reconciliationContext);
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
      assertRelayReconciliationContext(reconciliationContext);
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
    if (check.successful !== true) {
      assertRelayReconciliationContext(reconciliationContext);
      state.relayWithdraw.resultStatus = "unknown";
      state.relayWithdraw.resultMessage = "Tx was included with an unknown execution status · reconcile and do not retry";
      setWithdrawEvidence(
        "Unknown · successful spend not proven",
        "Unknown · successful transparent receive not proven",
        { render: false }
      );
      return;
    }

    assertRelayReconciliationContext(reconciliationContext);
    state.keplr.withdrawHash = txHash;
    state.keplr.withdrawHeight = check.height || "included";
    const fullyConfirmed = spentConfirmed && receiveConfirmed;
    state.relayWithdraw.resultStatus = fullyConfirmed ? "confirmed" : "recovering";
    state.relayWithdraw.resultMessage = fullyConfirmed
      ? "Tx finalized · bound transparent recipient confirmed · input nullifier spent"
      : transport === "evm" && !evmFinalityVerified
        ? "Tx included · waiting for configured EVM finality and verified privacy receipt"
        : "Tx included · waiting for nullifier and bound transparent output reconciliation";
    if (fullyConfirmed) {
      confirmWithdrawEvidence({ render: false });
      stopRelayReservationHeartbeat(reconciliationContext.heartbeatGeneration);
    } else {
      setWithdrawEvidence(
        spentConfirmed ? "Spent · confirmed" : "Checking spent state",
        receiveConfirmed
          ? "Received · bound output confirmed"
          : transport === "evm" && !evmFinalityVerified
            ? "Included · waiting for configured finality"
            : "Checking bound transparent output",
        { render: false }
      );
    }
    await Promise.allSettled([
      refreshWalletBalance({ sessionContext }),
      refreshProtocolStatus()
    ]);
    assertRelayReconciliationContext(reconciliationContext);
  } catch (error) {
    if (isStalePrivacySessionError(error) || reconciliationContext.operationReplaced) return;
    assertRelayReconciliationContext(reconciliationContext);
    state.relayWithdraw.resultStatus = "unknown";
    state.relayWithdraw.resultMessage = `Unable to confirm result · ${error.message}`;
    setWithdrawEvidence(
      "Unknown · reconciliation failed",
      "Unknown · reconciliation failed",
      { render: false }
    );
  } finally {
    try {
      assertPrivacySession(sessionContext);
      if (!reconciliationContext.operationReplaced) {
        assertRelayReconciliationContext(reconciliationContext);
      }
    } catch (error) {
      if (isStalePrivacySessionError(error)) return;
      throw error;
    }
    let clearedTerminalRecovery = false;
    if (reconciliationContext.operationReplaced || state.relayWithdraw.resultStatus === "confirmed") {
      if (relayOperationKey) {
        await clearEvmOperationArtifact(relayOperationKey, { sessionContext });
        assertPrivacySession(sessionContext);
      }
      await store.clear(reconciliationContext.payloadHash, {
        beforeCommit: () => assertPrivacySession(sessionContext)
      });
      reconciliationContext.operationReplaced = true;
      clearedTerminalRecovery = true;
    } else {
      try {
        await persistRelayWithdrawRecovery(state.relayWithdraw, {
          store,
          identity: operationIdentity,
          sessionContext
        });
        assertRelayReconciliationContext(reconciliationContext);
      } catch (error) {
        if (isStalePrivacySessionError(error)) return;
        assertRelayReconciliationContext(reconciliationContext);
        state.relayWithdraw.resultStatus = "manual-review";
        state.relayWithdraw.resultMessage = error.message;
      }
    }
    try {
      assertPrivacySession(sessionContext);
      if (!reconciliationContext.operationReplaced) {
        assertRelayReconciliationContext(reconciliationContext);
      }
      await refreshReservationState(manager, { sessionContext });
      assertPrivacySession(sessionContext);
      if (!reconciliationContext.operationReplaced) {
        assertRelayReconciliationContext(reconciliationContext);
      }
    } catch (error) {
      if (isStalePrivacySessionError(error)) return;
    }
    if (clearedTerminalRecovery) {
      await hydrateRelayWithdrawRecovery({ sessionContext });
      assertPrivacySession(sessionContext);
    }
    renderKeplr();
  }
}

async function explicitlyUnspentReservationIDs(
  manager,
  records,
  notes = state.keplr.notes,
  assertCurrent = null
) {
  const byLookupKey = new Map();
  for (const note of notes || []) {
    if (!noteHasUnspentEvidence(note)) continue;
    const lookupKey = await manager.lookupKeyForNote(note);
    assertCurrent?.();
    byLookupKey.set(lookupKey, note);
  }
  return records
    .filter(record => byLookupKey.has(record.nullifier_lookup_key))
    .map(record => record.reservation_id);
}

async function maybeResetStaleLocalGenesisReservations(manager, {
  refreshProtocol = true,
  sessionContext = null
} = {}) {
  const assertCurrent = () => {
    if (sessionContext) assertPrivacySession(sessionContext);
  };
  assertCurrent();
  if (!localTestBackendEnabled()) return { eligible: false, reset: false };
  const reservationSnapshot = await manager.store.load();
  assertCurrent();
  const reservations = Array.isArray(reservationSnapshot?.reservations)
    ? reservationSnapshot.reservations
    : [];
  const activeStatusSet = new Set(activeReservationStatuses);
  const active = reservations.filter(record => (
    record?.owner_key_id === manager.ownerKeyId
      && activeStatusSet.has(record?.status)
  ));
  if (!active.length) return { eligible: false, reset: false };

  if (refreshProtocol) {
    await refreshProtocolStatus();
    assertCurrent();
  }
  const assessments = groupReservationOperations(active).map(operation => (
    assessReservationRecovery(operation.records, { leaseOwner: reservationLeaseOwner })
  ));
  const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
  const eligible = canResetStaleLocalGenesisReservations({
    localTestMode: true,
    reserve: state.protocol.reserve,
    notes: state.keplr.notes,
    noteSyncStatus: state.keplr.noteSyncStatus,
    scanHasMore: Boolean(cursor.has_more ?? cursor.hasMore),
    assessments
  });
  if (!eligible) return { eligible: false, reset: false };

  const approved = globalThis.confirm(
    "현재 local test chain의 reserve와 deposit/withdraw history가 모두 0이고 full scan에도 note가 없습니다.\n\n"
    + "동일한 chain ID로 localnet이 재초기화되어 이전 genesis의 ProofReady reservation만 남은 상태입니다. 이전 localnet의 encrypted reservation 기록을 모두 삭제할까요? 이 동작은 public network에서는 제공되지 않습니다."
  );
  assertCurrent();
  if (!approved) return { eligible: true, reset: false };

  assertCurrent();
  await resetEncryptedBrowserReservationState(manager, {
    confirmedFreshLocalGenesis: true,
    expectedReservationState: reservationSnapshot
  });
  assertCurrent();
  await refreshReservationState(manager, { sessionContext });
  assertCurrent();
  els.keplrTxState.textContent = "Stale localnet reservations cleared";
  toast("Previous-genesis reservations cleared. New plans can use notes from the current localnet.");
  return { eligible: true, reset: true };
}

function activeReservationOperation(records, operationKey) {
  return groupReservationOperations(records)
    .find(operation => operation.key === operationKey)?.records || [];
}

async function resolvePreparationRecovery(manager, assessment, evidence, approvalReference, {
  sessionContext = null,
  operatorId = ""
} = {}) {
  const assertCurrent = () => {
    if (sessionContext) assertPrivacySession(sessionContext);
  };
  assertCurrent();
  const reservationIDs = assessment.reservationIDs;
  const status = assessment.status;
  if (assessment.signDocOnly) {
    if (evidence?.sign_doc_only_request !== true
      || evidence?.untracked_wallet_request_acknowledged !== true
      || evidence?.post_approval_chain_recheck !== true
      || evidence?.nullifier_unspent_confirmed !== true) {
      throw new Error("Sign-doc-only recovery requires explicit acknowledgement and a post-approval chain recheck");
    }
    if (status !== reservationStatuses.ManualReview) {
      if (![reservationStatuses.Proving, reservationStatuses.ProofReady].includes(status)) {
        throw new Error(`Sign-doc-only reservation status ${status} cannot enter recovery quarantine`);
      }
      assertCurrent();
      await manager.markManualReview(reservationIDs, {
        ...(assessment.leaseToken ? { leaseToken: assessment.leaseToken } : {}),
        error: "untracked_sign_doc_only_request",
        metadata: {
          reconcile_reason: "untracked_sign_doc_only_request",
          sign_doc_only_request: true,
          queryable_transaction_identity_absent: true
        }
      });
      assertCurrent();
    }
    await manager.resolveManualReview(reservationIDs, {
      target: reservationStatuses.ReplanRequired,
      operatorId,
      approvalReference,
      reason: "explicit_untracked_sign_doc_request_cancelled",
      metadata: evidence
    });
    assertCurrent();
    return "Approved quarantined sign-doc-only request and enabled replanning";
  }
  if (status === reservationStatuses.Reserved) {
    assertCurrent();
    await manager.releaseReservedOrProving(reservationIDs);
    assertCurrent();
    return "Released unused reservation";
  }
  if (status === reservationStatuses.Proving && assessment.leaseLive) {
    assertCurrent();
    await manager.releaseReservedOrProving(reservationIDs, { leaseToken: assessment.leaseToken });
    assertCurrent();
    return "Released local proving reservation";
  }
  if (status === reservationStatuses.ProofReady && assessment.leaseLive) {
    assertCurrent();
    await manager.markReplanRequired(reservationIDs, {
      fromStatus: reservationStatuses.ProofReady,
      leaseToken: assessment.leaseToken,
      proofDiscarded: true,
      error: "wallet_owner_discarded_unsubmitted_proof",
      metadata: evidence
    });
    assertCurrent();
    return "Discarded unsubmitted proof and enabled replanning";
  }

  if ([reservationStatuses.Proving, reservationStatuses.ProofReady].includes(status)) {
    assertCurrent();
    await manager.markManualReview(reservationIDs, {
      error: "expired_preparation_recovery",
      metadata: evidence
    });
    assertCurrent();
  } else if (status !== reservationStatuses.ManualReview) {
    throw new Error(`Reservation status ${status} cannot enter preparation recovery`);
  }

  assertCurrent();
  await manager.resolveManualReview(reservationIDs, {
    target: reservationStatuses.ReplanRequired,
    operatorId,
    approvalReference,
    reason: "wallet_owner_approved_no_broadcast_replan",
    metadata: evidence
  });
  assertCurrent();
  return "Approved manual review and enabled replanning";
}

async function recoverReservationPreparation(operationKey) {
  const sessionContext = privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!manager) throw new Error("Encrypted note reservation manager is not available");
  if (state.reservations.recoveringOperationKey) return;
  state.reservations.recoveringOperationKey = operationKey;
  renderReservationState();
  try {
    let records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
    assertPrivacySession(sessionContext);
    let assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
    if (assessment.action !== "review-replan") throw new Error(assessment.reason);

    els.keplrTxState.textContent = "Checking reservation recovery evidence";
    await refreshEvents({ allowFailure: true, sessionContext });
    assertPrivacySession(sessionContext);
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      skipSetup: true,
      maxPages: 1000,
      sessionContext,
      reservationManager: manager
    });
    assertPrivacySession(sessionContext);
    records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
    assertPrivacySession(sessionContext);
    if (!records.length) {
      await refreshReservationState(manager, { sessionContext });
      assertPrivacySession(sessionContext);
      toast("Reservation was already reconciled by the latest note scan.");
      return;
    }
    assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
    if (assessment.action !== "review-replan") throw new Error(assessment.reason);
    const signDocOnlyRecovery = assessment.signDocOnly === true;
    if (signDocOnlyRecovery && assessment.status !== reservationStatuses.ManualReview) {
      if (![reservationStatuses.Proving, reservationStatuses.ProofReady].includes(assessment.status)) {
        throw new Error(`Sign-doc-only reservation status ${assessment.status} cannot enter recovery quarantine`);
      }
      await manager.markManualReview(assessment.reservationIDs, {
        ...(assessment.leaseToken ? { leaseToken: assessment.leaseToken } : {}),
        error: "untracked_sign_doc_only_request",
        metadata: {
          reconcile_reason: "untracked_sign_doc_only_request",
          sign_doc_only_request: true,
          queryable_transaction_identity_absent: true
        }
      });
      assertPrivacySession(sessionContext);
      records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
      assertPrivacySession(sessionContext);
      assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
      if (assessment.action !== "review-replan"
        || assessment.status !== reservationStatuses.ManualReview
        || assessment.signDocOnly !== true) {
        throw new Error("Sign-doc-only request could not be durably quarantined for recovery");
      }
    }

    if (localTestBackendEnabled()) {
      await refreshProtocolStatus();
      assertPrivacySession(sessionContext);
      if (isEmptyLocalGenesisPrivacyState({
        localTestMode: true,
        reserve: state.protocol.reserve
      })) {
        const staleReset = await maybeResetStaleLocalGenesisReservations(manager, {
          refreshProtocol: false,
          sessionContext
        });
        assertPrivacySession(sessionContext);
        if (staleReset.eligible) {
          if (!staleReset.reset) {
            els.keplrTxState.textContent = "Fresh-genesis reservation reset cancelled";
          }
          return;
        }
        throw new Error(
          "Fresh local genesis detected. Run Reset & Rescan to remove previous-genesis notes before resetting stale reservations."
        );
      }
    }

    let unspentIDs = await explicitlyUnspentReservationIDs(
      manager,
      records,
      state.keplr.notes,
      () => assertPrivacySession(sessionContext)
    );
    assertPrivacySession(sessionContext);
    let checkedHeight = checkedReservationHeight();
    if (unspentIDs.length !== assessment.reservationIDs.length || !checkedHeight) {
      throw new Error("Every reserved nullifier must be explicitly unspent at an authoritative scanned height before replanning");
    }
    const operationLabel = `${reservationKindLabel(assessment.kind)} ${shorten(operationKey, 12, 10)}`;
    const approved = globalThis.confirm(signDocOnlyRecovery
      ? `${operationLabel}에는 transaction hash나 signed transaction hash 없이 sign-doc hash만 남아 있습니다. sign-doc hash는 wallet 서명 요청을 식별할 뿐, transaction 미제출을 증명하지 않습니다.\n\n`
        + "지갑 활동과 explorer에서 이 요청으로 제출된 transaction이 없음을 직접 확인했습니까? 승인 직후 chain을 다시 scan하고 모든 nullifier가 여전히 unspent인 경우에만 격리된 reservation을 취소하고 새 계획을 허용합니다."
      : `${operationLabel}의 broadcast 시도 기록이 없고 ${assessment.reservationIDs.length}개 nullifier가 height ${checkedHeight}에서 unspent로 확인되었습니다.\n\n`
        + "저장되지 않은 local proof를 폐기하고 이 note를 새 transaction 계획에 다시 사용할까요? 이 작업은 기존 proof를 다시 보낼 수 없게 만드는 명시적 recovery 승인입니다."
    );
    assertPrivacySession(sessionContext);
    if (!approved) {
      els.keplrTxState.textContent = "Reservation recovery cancelled";
      return;
    }

    if (signDocOnlyRecovery) {
      await scanKeplrNotes({
        quiet: true,
        throwOnError: true,
        skipSetup: true,
        maxPages: 1000,
        sessionContext,
        reservationManager: manager
      });
      assertPrivacySession(sessionContext);
      records = activeReservationOperation(await manager.listActiveReservations(), operationKey);
      assertPrivacySession(sessionContext);
      if (!records.length) {
        await refreshReservationState(manager, { sessionContext });
        assertPrivacySession(sessionContext);
        toast("The sign-doc-only request was reconciled by the post-approval chain scan; it was not released for replanning.");
        return;
      }
      const expectedIDs = [...assessment.reservationIDs].sort();
      assessment = assessReservationRecovery(records, { leaseOwner: reservationLeaseOwner });
      const currentIDs = [...assessment.reservationIDs].sort();
      if (assessment.action !== "review-replan"
        || assessment.status !== reservationStatuses.ManualReview
        || assessment.signDocOnly !== true
        || assessment.hasQueryableTransactionIdentity === true
        || JSON.stringify(currentIDs) !== JSON.stringify(expectedIDs)) {
        throw new Error("Reservation evidence changed after acknowledgement; keep the operation in Manual Review and reconcile it");
      }
      unspentIDs = await explicitlyUnspentReservationIDs(
        manager,
        records,
        state.keplr.notes,
        () => assertPrivacySession(sessionContext)
      );
      assertPrivacySession(sessionContext);
      checkedHeight = checkedReservationHeight();
      if (unspentIDs.length !== assessment.reservationIDs.length || !checkedHeight) {
        throw new Error("Post-approval scan did not prove every quarantined nullifier unspent; keep the operation in Manual Review");
      }
    }

    const approvalReference = `direct-recovery:${operationKey}:${checkedHeight}:${Date.now()}`;
    const evidence = {
      reconcile_reason: signDocOnlyRecovery
        ? "explicit_untracked_sign_doc_request_cancelled"
        : "wallet_owner_approved_unsubmitted_preparation_replan",
      proof_discarded: true,
      nullifier_unspent_confirmed: true,
      checked_height: checkedHeight,
      wallet_owner_approved_replan: true,
      ...(signDocOnlyRecovery ? {
        sign_doc_only_request: true,
        queryable_transaction_identity_absent: true,
        broadcast_outcome_untracked: true,
        untracked_wallet_request_acknowledged: true,
        post_approval_chain_recheck: true
      } : {
        no_broadcast_attempt: true
      }),
      recovery_approval_reference: approvalReference
    };
    const result = await resolvePreparationRecovery(manager, assessment, evidence, approvalReference, {
      sessionContext,
      operatorId: sessionContext.account
    });
    assertPrivacySession(sessionContext);
    els.keplrTxState.textContent = "Reservation recovery complete";
    const recoveredReservations = await manager.store.listReservations({
      ownerKeyId: manager.ownerKeyId
    });
    assertPrivacySession(sessionContext);
    await clearTerminalDirectEvmOperationArtifacts(recoveredReservations, { sessionContext });
    assertPrivacySession(sessionContext);
    await refreshReservationState(manager, { sessionContext });
    assertPrivacySession(sessionContext);
    toast(`${result}. A new plan may now use the released notes.`);
  } finally {
    try {
      assertPrivacySession(sessionContext);
      state.reservations.recoveringOperationKey = "";
      await refreshReservationState(manager, { sessionContext }).catch(error => {
        if (isStalePrivacySessionError(error)) throw error;
      });
      assertPrivacySession(sessionContext);
      renderReservationState();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function reconcileReservations({
  quiet = false,
  manager = null,
  sessionContext = privacySessionSnapshot(),
  accountTransactionLockHeld = false
} = {}) {
  assertPrivacySession(sessionContext);
  if (activeChainProfile()?.transport === "cosmos" && !accountTransactionLockHeld) {
    return withPublicTransactionLock(sessionContext, () => reconcileReservations({
      quiet,
      manager,
      sessionContext,
      accountTransactionLockHeld: true
    }));
  }
  const resolvedManager = manager || await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (!resolvedManager) throw new Error("Encrypted note reservation manager is not available");
  state.reservations.reconciling = true;
  els.keplrTxState.textContent = "Reconciling note reservations";
  renderReservationState();
  try {
    const [initialActive, allReservations] = await Promise.all([
      resolvedManager.listActiveReservations(),
      resolvedManager.store.listReservations({ ownerKeyId: resolvedManager.ownerKeyId })
    ]);
    assertPrivacySession(sessionContext);
    const initial = reconciliationReservationRecords(
      initialActive,
      unresolvedOperationReservations(allReservations)
    );
    const initialOperations = groupReservationOperations(initial);
    const privacyMarker = capturedPrivacyPendingState(sessionContext);
    const privacyMarkerHasReservation = Boolean(privacyMarker?.txHash)
      && groupReservationOperations(allReservations).some(operation => (
        commonCosmosReservationTransactionHash(operation.records)
          === normalizedHex(privacyMarker.txHash)
      ));
    const txHashes = [...new Set([
      ...initialOperations.map(operation => commonReservationTransactionHash(operation.records)),
      ...(privacyMarkerHasReservation ? [privacyMarker.txHash] : [])
    ].filter(Boolean))];
    const txChecks = new Map();
    for (const txHash of txHashes) {
      txChecks.set(txHash, await checkReservationTransaction(txHash));
      assertPrivacySession(sessionContext);
    }

    await refreshPersistedEvmBatchReceiptEvidence(resolvedManager, { sessionContext });
    assertPrivacySession(sessionContext);

    await refreshEvents({ allowFailure: true, sessionContext });
    assertPrivacySession(sessionContext);
    await scanKeplrNotes({
      quiet: true,
      throwOnError: true,
      sessionContext,
      reservationManager: resolvedManager,
      accountTransactionLockHeld
    });
    assertPrivacySession(sessionContext);
    const active = await resolvedManager.listActiveReservations();
    assertPrivacySession(sessionContext);
    for (const status of [
      reservationStatuses.ProofReady,
      reservationStatuses.Submitted,
      reservationStatuses.Unknown
    ]) {
      const recordsByTx = new Map();
      for (const operation of groupReservationOperations(active)) {
        const records = operation.records;
        if (!records.length || records.some(record => record.status !== status)) continue;
        if (status === reservationStatuses.ProofReady && records.some(record => (
          record.broadcast_in_flight !== true || Number(record.broadcast_attempt_count || 0) < 1
        ))) continue;
        const txHash = commonReservationTransactionHash(records);
        if (!txHash) continue;
        const grouped = recordsByTx.get(txHash) || [];
        grouped.push(...records);
        recordsByTx.set(txHash, grouped);
      }
      for (const [txHash, records] of recordsByTx) {
        const check = txChecks.get(txHash);
        // A missing Cosmos tx-index/REST result is not proof of non-submission:
        // broadcast_sync transactions may still be in the mempool or awaiting
        // indexing. Only an explicit on-chain failure can authorize replan.
        if (!check?.failed) continue;
        const unspentIDs = await explicitlyUnspentReservationIDs(
          resolvedManager,
          records,
          state.keplr.notes,
          () => assertPrivacySession(sessionContext)
        );
        assertPrivacySession(sessionContext);
        const checkedHeight = checkedReservationHeight(check);
        if (unspentIDs.length !== records.length || !checkedHeight) continue;
        assertPrivacySession(sessionContext);
        await replanExplicitlyFailedReservations({
          manager: resolvedManager,
          records,
          txHash,
          checkedHeight,
          metadata: { reconcile_source: "clairveil_example_dapp" },
          cosmosTransactionIdentity: activeChainProfile()?.transport === "cosmos"
        });
        assertPrivacySession(sessionContext);
      }
    }
    if (privacyMarkerHasReservation) {
      const finalReservations = await resolvedManager.store.listReservations({
        ownerKeyId: resolvedManager.ownerKeyId
      });
      assertPrivacySession(sessionContext);
      await clearReconciledCosmosPrivacyPending({
        manager: resolvedManager,
        records: finalReservations,
        transactionCheck: txChecks.get(privacyMarker.txHash),
        sessionContext,
        accountTransactionLockHeld
      });
      assertPrivacySession(sessionContext);
    }
    const finalReservations = await resolvedManager.store.listReservations({
      ownerKeyId: resolvedManager.ownerKeyId
    });
    assertPrivacySession(sessionContext);
    await clearTerminalDirectEvmOperationArtifacts(finalReservations, { sessionContext });
    assertPrivacySession(sessionContext);
    const remaining = await refreshReservationState(resolvedManager, { sessionContext });
    assertPrivacySession(sessionContext);
    const unresolvedCount = state.reservations.unresolved.length;
    const reconciliationIncomplete = remaining.length > 0 || unresolvedCount > 0;
    els.keplrTxState.textContent = reconciliationIncomplete
      ? "Reservation reconciliation requires review"
      : "Reservation reconciliation complete";
    if (!quiet) {
      toast(reconciliationIncomplete
        ? "Reservation reconciliation is incomplete. Do not retry until the listed operation is resolved."
        : "Note reservations reconciled. A new plan may now be prepared.");
    }
    return remaining;
  } catch (error) {
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
    els.keplrTxState.textContent = "Reservation reconciliation failed";
    throw error;
  } finally {
    try {
      assertPrivacySession(sessionContext);
      state.reservations.reconciling = false;
      renderReservationState();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function resolvePreparedPrivacyFailure(error, data = error?.preparedPrivacyData) {
  const sessionContext = data?.privacySessionContext || privacySessionSnapshot();
  try {
    assertPrivacySession(sessionContext);
    const currentManager = await currentReservationManager();
    assertPrivacySession(sessionContext);
    const manager = data?.reservationManager || currentManager;
    if (data?.reservationManager && data.reservationManager !== currentManager) {
      throw stalePrivacySessionError(sessionContext);
    }
    if (!manager) return { blocked: false, active: [] };
    if (error?.txHash || error?.broadcast || error?.txhash) {
      try {
        await reconcileReservations({ quiet: true, manager, sessionContext });
      } catch (reconcileError) {
        if (isStalePrivacySessionError(reconcileError)) throw reconcileError;
      }
      assertPrivacySession(sessionContext);
    }
    const active = await refreshReservationState(manager, { sessionContext });
    assertPrivacySession(sessionContext);
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
    if (isStalePrivacySessionError(reservationError)) throw reservationError;
    assertPrivacySession(sessionContext);
    return {
      blocked: true,
      active: state.reservations.active,
      reservationError
    };
  }
}

async function activePreparedReservations(data, {
  sessionContext = data?.privacySessionContext || privacySessionSnapshot()
} = {}) {
  assertPrivacySession(sessionContext);
  const ids = new Set(preparedReservationIDs(data));
  if (!ids.size || !data?.reservationManager) return [];
  const active = await refreshReservationState(data.reservationManager, { sessionContext });
  assertPrivacySession(sessionContext);
  return active.filter(record => ids.has(record.reservation_id));
}

async function requirePreparedReservationReconciled(data, label, {
  accountTransactionLockHeld = false,
  transactionCheck = null
} = {}) {
  const sessionContext = data?.privacySessionContext || privacySessionSnapshot();
  assertPrivacySession(sessionContext);
  const active = await activePreparedReservations(data, { sessionContext });
  assertPrivacySession(sessionContext);
  if (active.length) {
    const error = new Error(`${label} was included, but its note nullifier and operation evidence have not been reconciled yet. Do not retry or continue the plan.`);
    error.code = "NULLIFIER_RECONCILIATION_PENDING";
    error.preparedPrivacyData = data;
    throw error;
  }
  const records = await Promise.all(
    preparedReservationIDs(data).map(id => data.reservationManager.getReservation(id))
  );
  assertPrivacySession(sessionContext);
  const unresolved = records.filter(record => reservationRequiresOperationEvidence(record)
    && operationReconciliationStatus(record) !== operationStatuses.Succeeded);
  if (unresolved.length) {
    await refreshReservationState(data.reservationManager, { sessionContext });
    assertPrivacySession(sessionContext);
    const error = new Error(`${label} consumed its input note, but the tx output evidence does not prove the intended recipient and amount. Manual review is required.`);
    error.code = "OPERATION_RECONCILIATION_REQUIRED";
    error.preparedPrivacyData = data;
    throw error;
  }
  await clearReconciledCosmosPrivacyPending({
    manager: data.reservationManager,
    records,
    transactionCheck,
    sessionContext,
    accountTransactionLockHeld
  });
}

async function broadcastVeiledTransfer(amount, recipient, label = "veiled transfer", disclosure = {}, options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  if (activeChainProfile()?.transport === "cosmos" && !options.publicTransactionLockHeld) {
    return withCosmosAccountTransactionLock(sessionContext, publicTransactionLockHeld => broadcastVeiledTransfer(
      amount,
      recipient,
      label,
      disclosure,
      { ...options, sessionContext, publicTransactionLockHeld }
    ));
  }
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyTransferSignDoc(amount, recipient, disclosure, {
    ...options,
    sessionContext
  });
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, {
    ...options,
    sessionContext
  });
  state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
  return { ...broadcast, prepared: data.prepared, preparedTransaction: data.transaction };
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
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  if (activeChainProfile()?.transport === "cosmos" && !options.publicTransactionLockHeld) {
    return withCosmosAccountTransactionLock(sessionContext, publicTransactionLockHeld => createExactWithdrawNote(
      amount,
      hooks,
      { ...options, sessionContext, publicTransactionLockHeld }
    ));
  }
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
        signal: options.signal,
        sessionContext: options.sessionContext,
        publicTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
      });
      await refreshPrivacySurfaces({
        sessionContext: options.sessionContext,
        accountTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
      });
      continue;
    }

    if (data.prepared?.isFinal === false || data.prepared?.planAction === "self_merge") {
      hooks.onSelfMergeNeeded?.(data, step, maxPlannerSteps);
      const selfMergeConfirmed = await confirmPreparedSelfMerge(
        data,
        preparedSelfMergeReview(data)
      );
      if (!selfMergeConfirmed) {
        els.keplrTxState.textContent = "Exact-note self transaction cancelled";
        return null;
      }
      els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
      const plannerBroadcast = await broadcastPreparedPrivacy(data, "exact-note self transaction", {
        waitForEvmReceipt: true,
        sessionContext: options.sessionContext,
        publicTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
      });
      state.keplr.transferHash = plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
      await refreshPrivacySurfaces({
        sessionContext: options.sessionContext,
        accountTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
      });
      await requirePreparedReservationReconciled(data, "Exact-note self transaction", {
        accountTransactionLockHeld: Boolean(options.publicTransactionLockHeld),
        transactionCheck: { included: true, successful: true, failed: false }
      });
      continue;
    }

    hooks.onFinalExactTransfer?.(data, step, maxPlannerSteps);
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "exact-note self transfer", {
      waitForEvmReceipt: true,
      sessionContext: options.sessionContext,
      publicTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
    });
    state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    await refreshPrivacySurfaces({
      sessionContext: options.sessionContext,
      accountTransactionLockHeld: Boolean(options.publicTransactionLockHeld)
    });
    await requirePreparedReservationReconciled(data, "Exact-note self transfer", {
      accountTransactionLockHeld: Boolean(options.publicTransactionLockHeld),
      transactionCheck: { included: true, successful: true, failed: false }
    });
    return data;
  }

  throw new Error("Withdraw에 필요한 exact note 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.");
}

function sendFromKeplr() {
  return runValueMovingAction("transparent-send", sendFromKeplrUnlocked);
}

async function sendFromKeplrUnlocked() {
  if (!state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
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
      const broadcast = await withPublicTransactionLock(sessionContext, async () => {
        assertNoCapturedPublicPendingTransaction(sessionContext, "send");
        let submitted;
        try {
          submitted = await sendEvmTransaction(transaction, {
            label: "EVM send",
            sessionContext,
            ...publicEvmTransactionBoundaryCallbacks(sessionContext, "send")
          });
        } catch (error) {
          const txHash = transactionHashFromEvidence(error);
          if (txHash && !evmReceiptHasFailed(error?.broadcast?.receipt)) {
            persistCapturedPublicPendingTransaction(sessionContext, "send", txHash);
          }
          throw error;
        }
        assertPrivacySession(sessionContext);
        const txHash = transactionHashFromEvidence(submitted);
        if (!txHash) throw new Error("MetaMask did not return a recoverable transaction hash");
        persistCapturedPublicPendingTransaction(sessionContext, "send", txHash);
        return submitted;
      });
      assertPrivacySession(sessionContext);
      assertSuccessfulBroadcast(broadcast, "EVM send");
      state.keplr.sendHash = broadcast.txHash || state.keplr.sendHash || "";
      state.keplr.sendStatus = "submitted";
      els.keplrTxState.textContent = "Send submitted";
      renderKeplr();
      showSendResult({
        success: true,
        wallet: "MetaMask",
        txHash: state.keplr.sendHash
      });
      watchEvmBroadcast(broadcast, {
        sessionContext,
        onIncluded: async included => {
          state.keplr.sendHash = included.txHash || state.keplr.sendHash;
          await withPublicTransactionLock(sessionContext, () => {
            clearCapturedPublicPendingTransaction(sessionContext, "send", state.keplr.sendHash);
          });
          assertPrivacySession(sessionContext);
          state.keplr.sendStatus = "included";
          els.keplrTxState.textContent = "Send included";
          await Promise.allSettled([
            refreshWalletBalance({ sessionContext }),
            refreshBlockEvents({ sessionContext })
          ]);
          assertPrivacySession(sessionContext);
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
        onFailed: async error => {
          const failureConfirmed = evmReceiptHasFailed(error?.broadcast?.receipt);
          if (failureConfirmed) {
            await withPublicTransactionLock(sessionContext, () => {
              clearCapturedPublicPendingTransaction(sessionContext, "send", state.keplr.sendHash);
            });
            assertPrivacySession(sessionContext);
          }
          state.keplr.sendStatus = failureConfirmed ? "failed" : "unknown";
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

    const broadcast = await withPublicTransactionLock(sessionContext, async () => {
      assertNoCapturedPublicPendingTransaction(sessionContext, "send");
      await assertNoUnresolvedCosmosAccountBroadcast(sessionContext);
      const signDoc = await clairveilBrowserClient().buildBankSendSignDoc({
        from: state.keplr.account,
        pubKeyHex: state.keplr.pubkeyHex,
        to: recipient,
        amount: amountInputValue(els.keplrSendAmount),
        ...cosmosFeeRequestOptions(cosmosGasLimits.send)
      });
      assertPrivacySession(sessionContext);
      els.keplrTxState.textContent = "Waiting for Keplr";
      return signDirectAndBroadcast(signDoc, {
        sessionContext,
        publicPendingKind: "send",
        publicTransactionLockHeld: true
      });
    });
    assertPrivacySession(sessionContext);
    assertSuccessfulBroadcast(broadcast, "Cosmos send");
    state.keplr.sendHash = broadcast.broadcast?.txhash || "";
    state.keplr.sendStatus = "included";
    els.keplrTxState.textContent = "Send included";
    renderKeplr();
    showSendResult({
      success: true,
      wallet: "Keplr",
      txHash: state.keplr.sendHash
    });
    await Promise.allSettled([
      refreshWalletBalance({ sessionContext }),
      refreshBlockEvents({ sessionContext })
    ]);
    assertPrivacySession(sessionContext);
    renderKeplr();
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    if (error?.code === "PUBLIC_TX_PENDING") {
      hydratePublicPendingTransactions();
      showNotice({
        title: "기존 트랜잭션 확인 필요",
        message: error.message
      });
      return;
    }
    if (error?.code === "COSMOS_ACCOUNT_TX_PENDING") {
      showNotice({
        title: "기존 privacy 트랜잭션 확인 필요",
        message: `${error.message}\nNote reservations에서 Reconcile을 실행한 뒤 다시 시도하세요.`
      });
      return;
    }
    if (activeChainProfile()?.transport === "evm"
      && restoreHashlessPublicAttemptAfterError(sessionContext, "send", error)) {
      return;
    }
    const txHash = transactionHashFromEvidence(error);
    const failureConfirmed = activeChainProfile()?.transport === "evm"
      ? evmReceiptHasFailed(error?.broadcast?.receipt)
      : cosmosTxEvidenceConfirmsFailure(error);
    if (txHash) state.keplr.sendHash = txHash;
    state.keplr.sendStatus = failureConfirmed || !txHash ? "failed" : "unknown";
    els.keplrTxState.textContent = state.keplr.sendStatus === "unknown" ? "Send status unknown" : "Send failed";
    showSendResult({
      success: false,
      error: state.keplr.sendStatus === "unknown"
        ? `${error.message}\nThe signed transaction identity is saved. Reconcile it before retrying.`
        : error.message
    });
  } finally {
    if (privacySessionIsCurrent(sessionContext)) {
      setBusy(els.sendFromKeplr, false);
      renderKeplr();
    }
  }
}

function depositFromKeplr() {
  return runValueMovingAction("privacy-deposit", depositFromKeplrUnlocked);
}

async function depositFromKeplrUnlocked() {
  if (!state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
  const privacySetupReady = await setupKeplrPrivacy();
  if (!privacySetupReady) return;
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    throw error;
  }
  if (!state.keplr.rootSignatureBase64) return;
  requirePrivacyBrowserStorage();

  setBusy(els.depositFromKeplr, true);
  els.keplrTxState.textContent = "Preparing deposit";
  try {
    const amount = amountInputValue(els.keplrDepositAmount);
    const broadcast = await broadcastPrivacyDeposit(amount, "deposit", { sessionContext });
    assertPrivacySession(sessionContext);
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
        sessionContext,
        onIncluded: async included => {
          state.keplr.depositHash = included.txHash || state.keplr.depositHash;
          await withPublicTransactionLock(sessionContext, () => {
            persistCapturedDepositRecoveryPending(
              sessionContext,
              state.keplr.depositHash,
              included.receipt?.blockNumber || state.keplr.depositHeight
            );
          });
          assertPrivacySession(sessionContext);
          state.keplr.depositHeight = included.receipt?.blockNumber || state.keplr.depositHeight;
          updateIncludedDepositNetworkFee(included);
          els.keplrTxState.textContent = "Deposit included";
          const recovered = await recoverDepositNote({ ...broadcast, ...included, prepared: broadcast.prepared });
          assertPrivacySession(sessionContext);
          await refreshPrivacySurfaces({ balance: true, sessionContext });
          assertPrivacySession(sessionContext);
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
        onFailed: async error => {
          const failureConfirmed = evmReceiptHasFailed(error?.broadcast?.receipt);
          if (failureConfirmed) {
            await withPublicTransactionLock(
              sessionContext,
              () => clearConfirmedDepositRecoveryUnlocked(
                sessionContext,
                error?.txHash || error?.broadcast?.txHash || state.keplr.depositHash
              )
            );
            assertPrivacySession(sessionContext);
          }
          state.keplr.depositRecoveryStatus = failureConfirmed ? "failed" : "unknown";
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
    assertPrivacySession(sessionContext);
    await refreshPrivacySurfaces({ balance: true, sessionContext });
    assertPrivacySession(sessionContext);
    showNotice({
      title: recovered ? "Deposit 및 note 복구 완료" : "Deposit 포함 · note 복구 대기",
      message: recovered
        ? `${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit과 encrypted note 복구가 완료되었습니다.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
        : "트랜잭션은 성공했지만 note 복구가 아직 완료되지 않았습니다. Reset & Rescan으로 다시 복구할 수 있습니다."
    });
  } catch (error) {
    if (isStalePrivacySessionError(error) || !privacySessionIsCurrent(sessionContext)) return;
    if (error?.code === "PUBLIC_TX_PENDING") {
      hydratePublicPendingTransactions();
      showNotice({
        title: "기존 트랜잭션 확인 필요",
        message: error.message
      });
      return;
    }
    if (error?.code === "COSMOS_ACCOUNT_TX_PENDING") {
      showNotice({
        title: "기존 privacy 트랜잭션 확인 필요",
        message: `${error.message}\nNote reservations에서 Reconcile을 실행한 뒤 다시 시도하세요.`
      });
      return;
    }
    if (activeChainProfile()?.transport === "evm"
      && restoreHashlessPublicAttemptAfterError(sessionContext, "deposit", error)) {
      return;
    }
    state.keplr.depositPrepared = error?.preparedPrivacyData?.prepared || state.keplr.depositPrepared;
    const txHash = transactionHashFromEvidence(error);
    const failureConfirmed = activeChainProfile()?.transport === "evm"
      ? evmReceiptHasFailed(error?.broadcast?.receipt)
      : cosmosTxEvidenceConfirmsFailure(error);
    if (txHash) state.keplr.depositHash = txHash;
    state.keplr.depositRecoveryStatus = failureConfirmed || !txHash ? "failed" : "unknown";
    state.keplr.depositRecoveryMessage = state.keplr.depositRecoveryStatus === "unknown"
      ? `Broadcast result unknown · reconcile ${txHash} before retrying`
      : error.message;
    els.keplrTxState.textContent = state.keplr.depositRecoveryStatus === "unknown"
      ? "Deposit status unknown"
      : "Deposit failed";
    showNotice({
      title: state.keplr.depositRecoveryStatus === "unknown" ? "Deposit 결과 확인 필요" : "Deposit 실패",
      message: state.keplr.depositRecoveryStatus === "unknown"
        ? `${error.message}\nThe signed transaction identity is saved. Do not retry before reconciliation.`
        : error.message,
      failed: state.keplr.depositRecoveryStatus === "failed"
    });
  } finally {
    if (privacySessionIsCurrent(sessionContext)) {
      setBusy(els.depositFromKeplr, false);
      renderKeplr();
    }
  }
}

function noteStoreMutationLockName(context) {
  const namespace = context?.publicPendingIdentity?.key
    || noteStoreKeys()?.namespace
    || `${context?.profileId || "unknown"}:${context?.account || "unknown"}`;
  return `clairveil:v0.3.1:note-store-mutation:${namespace}`;
}

async function withNoteStoreMutation(sessionContext, task) {
  assertPrivacySession(sessionContext);
  if (typeof globalThis.navigator?.locks?.request !== "function") {
    throw new Error("Encrypted note recovery requires browser Web Locks support");
  }
  const operation = noteStoreCoordinator.run(noteStoreMutationLockName(sessionContext), async () => {
    assertPrivacySession(sessionContext);
    // Each tab caches its own in-memory note-store snapshot. Reopen it only
    // after acquiring the cross-tab lock so a prior reset/rescan in another
    // tab cannot be overwritten by this tab's stale cursor and note set.
    noteStore = null;
    noteStorePromise = null;
    noteStoreKey = "";
    return task();
  });
  assertPrivacySession(sessionContext);
  renderKeplr();
  try {
    return await operation;
  } finally {
    try {
      assertPrivacySession(sessionContext);
      renderKeplr();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function scanKeplrNotesUnlocked(options, sessionContext) {
  assertPrivacySession(sessionContext);
  if (!state.keplr.account) return;
  if (!options.skipSetup) {
    const privacySetupReady = await setupKeplrPrivacy({ skipInitialSync: true });
    if (!privacySetupReady) return;
    assertPrivacySession(sessionContext);
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
      assertPrivacySession(sessionContext);
    }
    state.keplr.noteSyncStatus = "scanning";
    state.keplr.noteSyncMessage = reset ? "Full rescan in progress" : "Incremental scan in progress";
    els.noteSyncState.textContent = state.keplr.noteSyncMessage;
    els.noteSyncState.dataset.status = state.keplr.noteSyncStatus;
    const store = await currentNoteStore();
    if (!store) {
      throw new Error("Encrypted note storage is required before scanning privacy notes");
    }
    if (!reset) {
      const latest = await store.load();
      assertPrivacySession(sessionContext);
      state.keplr.notes = latest.notes || [];
      state.keplr.noteScanCursor = latest.scanCursor || defaultNoteScanCursor();
      // Page-resume options are process-local. The durable cursor loaded under
      // the cross-tab lock is authoritative after another tab has scanned.
      state.keplr.noteScanResumeOptions = null;
    }
    const scanOptions = noteScanRequestOptions({ reset, maxPages: options.maxPages ?? 5 });
    const data = await clairveilBrowserClient().scanWalletNotes(privacyRequest({
      ...scanOptions,
      noteStore: store,
      includeFoundNotes: true
    }));
    assertPrivacySession(sessionContext);
    const pendingDepositRecovery = await applyNoteScanResult(data, {
      reset,
      sessionContext,
      reservationManager: options.reservationManager || null,
      noteStore: store
    });
    options.capturePendingDepositRecovery?.(pendingDepositRecovery);
    assertPrivacySession(sessionContext);
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
    if (isStalePrivacySessionError(error)) throw error;
    assertPrivacySession(sessionContext);
    state.keplr.noteSyncStatus = error?.code === "NOTE_CACHE_CORRUPT" ? "corrupt" : "failed";
    state.keplr.noteSyncMessage = error.message;
    if (!options.quiet) {
      els.keplrTxState.textContent = "Scan failed";
      toast(error.message);
    }
    if (options.throwOnError) throw error;
  } finally {
    try {
      assertPrivacySession(sessionContext);
      setBusy(els.scanKeplrNotes, false);
      renderKeplr();
    } catch (error) {
      if (!isStalePrivacySessionError(error)) throw error;
    }
  }
}

async function scanKeplrNotes(options = {}) {
  const sessionContext = options.sessionContext || privacySessionSnapshot();
  let pendingDepositRecovery = null;
  const result = await withNoteStoreMutation(sessionContext, () => scanKeplrNotesUnlocked({
    ...options,
    capturePendingDepositRecovery: recovery => {
      pendingDepositRecovery = recovery;
    }
  }, sessionContext));
  assertPrivacySession(sessionContext);
  await finalizePendingDepositRecoveryFromTypedNotes(pendingDepositRecovery, {
    sessionContext,
    accountTransactionLockHeld: Boolean(options.accountTransactionLockHeld)
  });
  assertPrivacySession(sessionContext);
  return result;
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
  const sessionContext = privacySessionSnapshot();
  await scanKeplrNotes({
    reset: true,
    throwOnError: true,
    maxPages: 1000,
    sessionContext
  });
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  if (manager) await maybeResetStaleLocalGenesisReservations(manager, { sessionContext });
  assertPrivacySession(sessionContext);
}

async function rollbackAndRescanNotes() {
  const sessionContext = privacySessionSnapshot();
  let pendingDepositRecovery = null;
  const result = await withNoteStoreMutation(sessionContext, async () => {
  const height = String(els.noteRollbackHeight.value || "").trim();
  if (!/^(0|[1-9][0-9]*)$/.test(height)) {
    throw new Error("Rollback height must be a canonical non-negative integer");
  }
  const store = await currentNoteStore();
  assertPrivacySession(sessionContext);
  if (!store || typeof store.rollbackToHeight !== "function") {
    throw new Error("Encrypted note store does not support cursor rollback");
  }
  const current = await store.load();
  assertPrivacySession(sessionContext);
  if (BigInt(height) > BigInt(current.lastScannedHeight || 0)) {
    throw new Error(`Rollback height cannot exceed the last scanned height ${current.lastScannedHeight || 0}`);
  }
  if (!globalThis.confirm(`Height ${height}부터 note cache를 되감고 다시 스캔할까요? 필요하면 먼저 Backup cache를 실행하세요.`)) return;

  const rolledBack = await store.rollbackToHeight(height);
  assertPrivacySession(sessionContext);
  state.keplr.notes = rolledBack.notes || [];
  state.keplr.noteScanCursor = rolledBack.scanCursor || defaultNoteScanCursor();
  state.keplr.noteScanResumeOptions = null;
  state.keplr.noteSyncStatus = "rollback-ready";
  state.keplr.noteSyncMessage = `Cursor rolled back to height ${height} · rescan required`;
  renderKeplr();
  await scanKeplrNotesUnlocked({
    quiet: false,
    throwOnError: true,
    skipSetup: true,
    maxPages: 1000,
    capturePendingDepositRecovery: recovery => {
      pendingDepositRecovery = recovery;
    }
  }, sessionContext);
  });
  assertPrivacySession(sessionContext);
  await finalizePendingDepositRecoveryFromTypedNotes(pendingDepositRecovery, { sessionContext });
  assertPrivacySession(sessionContext);
  return result;
}

async function refreshPrivacySurfaces({
  balance = false,
  sessionContext = privacySessionSnapshot(),
  accountTransactionLockHeld = false
} = {}) {
  assertPrivacySession(sessionContext);
  const tasks = [
    refreshEvents({ sessionContext }),
    refreshAuditorTransfers({ sessionContext }),
    scanKeplrNotes({ quiet: true, sessionContext, accountTransactionLockHeld }),
    refreshNotes({ sessionContext }),
    refreshProtocolStatus()
  ];
  if (balance) {
    tasks.unshift(refreshWalletBalance({ sessionContext }));
  }
  await Promise.allSettled(tasks);
  assertPrivacySession(sessionContext);
  const manager = await currentReservationManager();
  assertPrivacySession(sessionContext);
  await reconcileSpentReservations(manager, state.keplr.notes, { sessionContext });
  assertPrivacySession(sessionContext);
  await refreshReservationState(manager, { sessionContext });
  assertPrivacySession(sessionContext);
}

function transferFromVeiled() {
  return runValueMovingAction("privacy-transfer", transferFromVeiledUnlocked);
}

async function transferFromVeiledUnlocked() {
  if (!state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
  const privacySetupReady = await setupKeplrPrivacy();
  if (!privacySetupReady) return;
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
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
    assertPrivacySession(sessionContext);
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
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  if (!confirmed) return;

  setBusy(els.transferFromVeiled, true);
  els.keplrTxState.textContent = "Preparing veiled transfer";
  const executeTransfer = async publicTransactionLockHeld => {
    const operationTiming = await privacyOperationTiming();
    assertPrivacySession(sessionContext);
    const operationOptions = {
      ...operationTiming,
      signal: activeProofSignal(),
      sessionContext,
      publicTransactionLockHeld
    };
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
          signal: operationOptions.signal,
          sessionContext,
          publicTransactionLockHeld
        });
        await refreshPrivacySurfaces({
          sessionContext,
          accountTransactionLockHeld: Boolean(publicTransactionLockHeld)
        });
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
        const selfMergeConfirmed = await confirmPreparedSelfMerge(
          data,
          preparedSelfMergeReview(data, {
            selfView: transferFlowState.review?.selfView || "Encrypted self-view included"
          })
        );
        if (!selfMergeConfirmed) {
          els.keplrTxState.textContent = "Self transaction cancelled";
          return;
        }
        els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
        const plannerBroadcast = await broadcastPreparedPrivacy(data, "self transaction", {
          waitForEvmReceipt: true,
          sessionContext,
          publicTransactionLockHeld
        });
        state.keplr.transferHash = plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
        await refreshPrivacySurfaces({
          sessionContext,
          accountTransactionLockHeld: Boolean(publicTransactionLockHeld)
        });
        await requirePreparedReservationReconciled(data, "Self transaction", {
          accountTransactionLockHeld: Boolean(publicTransactionLockHeld),
          transactionCheck: { included: true, successful: true, failed: false }
        });
        continue;
      }

      finalData = data;
      break;
    }

    if (!finalData) {
      throw new Error("입력하신 금액의 노트 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.");
    }

    const finalPreparedExpiresAtUnix = preparedTransferExpiryUnix(finalData);
    const finalConfirmed = await withPreparedReservationHeartbeat(finalData, () => (
      requestPreparedTransferConfirmation({
        ...transferFlowState.review,
        recipient: finalData.prepared?.finalRecipient || recipient,
        amount: coinText(finalData.prepared?.finalAmount || amount),
        changeEffect: preparedTransferChangeEffect(finalData),
        expiresAtUnix: finalPreparedExpiresAtUnix
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
    const broadcast = await broadcastPreparedPrivacy(finalData, "privacy transfer", {
      sessionContext,
      publicTransactionLockHeld
    });
    state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm ? "Transfer submitted" : "Transfer included";
    renderKeplr();
    if (isPendingEvm) {
      finishTransferFlow("트랜스퍼 요청이 제출되었습니다");
      watchEvmBroadcast(broadcast, {
        sessionContext,
        onIncluded: async included => {
          state.keplr.transferHash = included.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer included";
          await refreshPrivacySurfaces({ sessionContext });
          assertPrivacySession(sessionContext);
          await requirePreparedReservationReconciled(finalData, "Privacy transfer", {
            transactionCheck: { included: true, successful: true, failed: false }
          });
          finishTransferFlow("트랜스퍼 요청이 성공하였습니다");
          renderKeplr();
        },
        onUnknown: async unknown => {
          state.keplr.transferHash = unknown.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer status unknown";
          await refreshReservationState(finalData.reservationManager, { sessionContext }).catch(() => {});
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
    await refreshPrivacySurfaces({
      sessionContext,
      accountTransactionLockHeld: Boolean(publicTransactionLockHeld)
    });
    await requirePreparedReservationReconciled(finalData, "Privacy transfer", {
      accountTransactionLockHeld: Boolean(publicTransactionLockHeld),
      transactionCheck: { included: true, successful: true, failed: false }
    });
    finishTransferFlow("트랜스퍼 요청이 성공하였습니다");
  };
  try {
    await withCosmosAccountTransactionLock(sessionContext, executeTransfer);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
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

function transferBatchFromVeiled() {
  return runValueMovingAction("privacy-batch-transfer", transferBatchFromVeiledUnlocked);
}

async function transferBatchFromVeiledUnlocked() {
  if (batchTransferInFlight || !state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
  const transport = activeChainProfile()?.transport;
  const expectedWallet = transport === "evm" ? "metamask" : "keplr";
  if (!batchTransferEnabled() || state.activeWallet !== expectedWallet) {
    toast("Atomic batch transfer is not enabled for the active chain profile and wallet.");
    return;
  }
  const privacySetupReady = await setupKeplrPrivacy();
  if (!privacySetupReady) return;
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  if (!state.keplr.rootSignatureBase64) return;

  let payments;
  let timing;
  try {
    payments = collectBatchTransferPayments();
    timing = await privacyOperationTiming();
    assertPrivacySession(sessionContext);
  } catch (error) {
    toast(error.message);
    return;
  }
  const confirmed = await openTransferFlowModal("batchTransfer", {
    chainId: activeChainProfile()?.chainId,
    recipient: batchTransferReviewRecipients(payments),
    amount: batchTransferReviewAmount(payments),
    disclosure: batchTransferReviewDisclosure(payments),
    selfView: batchAuthorizationRequested()
      ? "Encrypted self-view included · EIP-712 authorized self-submit"
      : "Encrypted self-view included",
    changeEffect: "Pending payload preparation",
    expiresAtUnix: timing.expiresAtUnix
  });
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  if (!confirmed) return;

  batchTransferInFlight = true;
  setBusy(els.transferBatchFromVeiled, true);
  els.keplrTxState.textContent = `Preparing ${transport} batch transfer`;
  let data = null;
  const executeBatchTransfer = async publicTransactionLockHeld => {
    const operationTiming = await privacyOperationTiming();
    assertPrivacySession(sessionContext);
    const authorizationOptions = batchAuthorizationPreparationOptions(operationTiming, sessionContext);
    updateTransferFlow(
      "zero",
      "Batch proof 준비 중",
      `모든 recipient·amount·disclosure를 하나의 ${transport} privacy proof로 준비합니다.${authorizationOptions.authorization ? " Proof가 준비되면 연결된 EVM wallet이 EIP-712 authorization을 서명합니다." : ""}`
    );
    data = await prepareBatchTransfer(payments, {
      ...operationTiming,
      ...authorizationOptions,
      signal: activeProofSignal(),
      sessionContext,
      publicTransactionLockHeld
    });
    assertPrivacySession(sessionContext);
    const preparedReview = preparedBatchReview(data, payments);
    const finalConfirmed = await withPreparedReservationHeartbeat(data, () => (
      requestPreparedTransferConfirmation({
        ...transferFlowState.review,
        recipient: batchTransferReviewRecipients(preparedReview.payments),
        amount: batchTransferReviewAmount(preparedReview.payments),
        disclosure: batchTransferReviewDisclosure(preparedReview.payments),
        changeEffect: preparedReview.changeEffect,
        expiresAtUnix: operationTiming.expiresAtUnix
      })
    ));
    assertPrivacySession(sessionContext);
    if (!finalConfirmed) {
      await discardPreparedReservation(data);
      await clearBatchTransferArtifact({ reservation: data.reservation, sessionContext });
      return;
    }

    updateTransferFlow(
      "transfer",
      "Batch 서명 대기",
      `준비된 단일 ${transport} privacy transaction을 ${expectedWallet === "metamask" ? "MetaMask" : "Keplr"}에서 확인하고 서명해 주세요.`
    );
    els.keplrTxState.textContent = expectedWallet === "metamask"
      ? "Waiting for MetaMask"
      : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, `${transport} batch transfer`, {
      sessionContext,
      publicTransactionLockHeld
    });
    assertPrivacySession(sessionContext);
    state.keplr.batchTransferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    if (transport === "cosmos") {
      await saveBatchSubmission(data, state.keplr.batchTransferHash, { sessionContext });
    }
    const pending = Boolean(broadcast.pending);
    els.keplrTxState.textContent = pending ? "Batch transfer submitted" : "Batch transfer included";
    renderKeplr();

    const confirmIncluded = async included => {
      assertPrivacySession(sessionContext);
      state.keplr.batchTransferHash = included.txHash || state.keplr.batchTransferHash;
      if (transport === "evm") {
        await saveBatchReceiptEvidence(data, included, { sessionContext });
      } else {
        await saveBatchInclusion(data, state.keplr.batchTransferHash, { sessionContext });
      }
      els.keplrTxState.textContent = "Batch transfer included";
      await refreshPrivacySurfaces({
        balance: true,
        sessionContext,
        accountTransactionLockHeld: Boolean(publicTransactionLockHeld)
      });
      assertPrivacySession(sessionContext);
      await requirePreparedReservationReconciled(data, `${transport} batch transfer`, {
        accountTransactionLockHeld: Boolean(publicTransactionLockHeld),
        transactionCheck: { included: true, successful: true, failed: false }
      });
      assertPrivacySession(sessionContext);
      await clearBatchTransferArtifact({ reservation: data.reservation, sessionContext });
      finishTransferFlow("Batch transfer 요청이 성공하였습니다");
      renderKeplr();
    };
    if (pending && transport === "evm") {
      finishTransferFlow("Batch transfer 요청이 제출되었습니다");
      watchEvmBroadcast(broadcast, {
        sessionContext,
        onIncluded: confirmIncluded,
        onUnknown: async unknown => {
          assertPrivacySession(sessionContext);
          state.keplr.batchTransferHash = unknown.txHash || state.keplr.batchTransferHash;
          els.keplrTxState.textContent = "Batch transfer status unknown";
          await refreshReservationState(data.reservationManager, { sessionContext }).catch(() => {});
          finishTransferFlowUnknown(`Receipt polling이 끝났지만 실패가 확인되지 않았습니다. batch tx와 모든 input nullifier를 reconcile하기 전에는 다시 전송하지 마세요.\nTx: ${state.keplr.batchTransferHash}`);
          renderKeplr();
        },
        onFailed: async error => {
          assertPrivacySession(sessionContext);
          const resolution = await resolvePreparedPrivacyFailure(error, data);
          els.keplrTxState.textContent = resolution.blocked
            ? "Batch reconciliation required"
            : "Batch transfer failed";
          if (resolution.blocked) {
            finishTransferFlowUnknown(error.message);
          } else {
            await clearBatchTransferArtifact({ reservation: data.reservation, sessionContext });
            finishTransferFlow(error.message, false, { retry: () => transferBatchFromVeiled() });
          }
          renderKeplr();
        }
      });
      return;
    }
    await confirmIncluded(broadcast);
  };
  try {
    await withBatchAccountTransactionLock(sessionContext, executeBatchTransfer);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    const cancelled = error?.name === "AbortError" || activeProofSignal()?.aborted;
    const resolution = await resolvePreparedPrivacyFailure(error, data || undefined);
    if (resolution.blocked) {
      els.keplrTxState.textContent = "Batch reconciliation required";
      finishTransferFlowUnknown(error.message);
    } else {
      if (data) {
        await clearBatchTransferArtifact({ reservation: data.reservation, sessionContext });
      }
      els.keplrTxState.textContent = cancelled ? "Batch preparation cancelled" : "Batch transfer failed";
      finishTransferFlow(cancelled ? "Proof 요청을 취소했습니다." : error.message, false, {
        retry: () => transferBatchFromVeiled()
      });
    }
  } finally {
    if (privacySessionIsCurrent(sessionContext)) {
      batchTransferInFlight = false;
      setBusy(els.transferBatchFromVeiled, false);
      renderKeplr();
    }
  }
}

function withdrawFromVeiled({ relayMode = false } = {}) {
  return runValueMovingAction(
    relayMode ? "relay-prepare" : "privacy-withdraw",
    () => withdrawFromVeiledUnlocked({ relayMode })
  );
}

async function withdrawFromVeiledUnlocked({ relayMode = false } = {}) {
  if (!state.keplr.account) return;
  const sessionContext = privacySessionSnapshot();
  const amountInput = relayMode ? els.relayWithdrawAmount : els.veiledWithdrawAmount;
  const recipientInput = relayMode ? els.relayWithdrawRecipient : els.veiledWithdrawRecipient;
  const actionButton = relayMode ? els.relayWithdrawFromVeiled : els.withdrawFromVeiled;
  let amount;
  try {
    amount = amountInputValue(amountInput);
  } catch (error) {
    toast(error.message);
    return;
  }
  const recipient = recipientInput.value.trim();
  if (!recipient) {
    toast(`Withdraw recipient에 받을 ${accountPrefix()} 주소를 넣어줘.`);
    return;
  }

  const privacySetupReady = await setupKeplrPrivacy();
  if (!privacySetupReady) return;
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  if (!state.keplr.rootSignatureBase64) return;

  let timing;
  try {
    timing = await privacyOperationTiming();
    assertPrivacySession(sessionContext);
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
  try {
    assertPrivacySession(sessionContext);
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    throw error;
  }
  if (!confirmed) return;

  if (!relayMode) {
    setWithdrawEvidence("Preparing · no broadcast yet", "Preparing · no broadcast yet");
  }
  setBusy(actionButton, true);
  (relayMode ? els.withdrawFromVeiled : els.relayWithdrawFromVeiled).disabled = true;
  els.keplrTxState.textContent = "Preparing withdraw";
  const executeWithdraw = async publicTransactionLockHeld => {
    const operationTiming = await privacyOperationTiming();
    assertPrivacySession(sessionContext);
    const operationOptions = {
      ...operationTiming,
      signal: activeProofSignal(),
      sessionContext,
      publicTransactionLockHeld
    };
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
      const exactNotePrepared = await createExactWithdrawNote(amount, {
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
      if (!exactNotePrepared) {
        els.keplrTxState.textContent = "Withdraw cancelled before self transaction broadcast";
        return;
      }
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
      finishTransferFlow("Relay withdraw payload가 준비되었습니다");
      return;
    }
    updateTransferFlow(
      "transfer",
      "위드드로우 서명 대기",
      `note 준비가 완료되었습니다. 이제 Clair balance로 이동할 withdraw를 요청합니다. ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 최종 내용을 확인하고 서명해 주세요.`
    );
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "privacy withdraw", {
      sessionContext,
      publicTransactionLockHeld
    });
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
        sessionContext,
        onIncluded: async included => {
          state.keplr.withdrawHash = included.txHash || state.keplr.withdrawHash;
          state.keplr.withdrawHeight = included.receipt?.blockNumber || state.keplr.withdrawHeight;
          els.keplrTxState.textContent = "Withdraw included";
          await refreshPrivacySurfaces({ balance: true, sessionContext });
          assertPrivacySession(sessionContext);
          await requirePreparedReservationReconciled(data, "Privacy withdraw", {
            transactionCheck: { included: true, successful: true, failed: false }
          });
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
          await refreshReservationState(data.reservationManager, { sessionContext }).catch(() => {});
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
            finishTransferFlow(error.message, false, { retry: () => withdrawFromVeiled({ relayMode }) });
          }
          renderKeplr();
        }
      });
      return;
    }
    await refreshPrivacySurfaces({
      balance: true,
      sessionContext,
      accountTransactionLockHeld: Boolean(publicTransactionLockHeld)
    });
    await requirePreparedReservationReconciled(data, "Privacy withdraw", {
      accountTransactionLockHeld: Boolean(publicTransactionLockHeld),
      transactionCheck: { included: true, successful: true, failed: false }
    });
    confirmWithdrawEvidence({ render: false });
    finishTransferFlow("Withdraw 요청이 성공하였습니다");
  };
  try {
    if (relayMode) {
      await executeWithdraw(false);
    } else {
      await withCosmosAccountTransactionLock(sessionContext, executeWithdraw);
    }
  } catch (error) {
    if (isStalePrivacySessionError(error)) return;
    const cancelled = error?.name === "AbortError" || activeProofSignal()?.aborted;
    const resolution = await resolvePreparedPrivacyFailure(error);
    if (resolution.blocked) {
      els.keplrTxState.textContent = relayMode
        ? "Relay preparation reconciliation required"
        : "Withdraw reconciliation required";
      if (!relayMode) {
        setWithdrawEvidence(
          "Unknown · reservation remains locked",
          "Unknown · reconcile before retry",
          { render: false }
        );
      }
      finishTransferFlowUnknown(error.message);
    } else {
      els.keplrTxState.textContent = relayMode
        ? cancelled ? "Relay preparation cancelled" : "Relay preparation failed"
        : cancelled ? "Withdraw cancelled" : "Withdraw failed";
      if (!relayMode) {
        setWithdrawEvidence(
          cancelled ? "Not spent · cancelled before submission" : "Unspent · failure confirmed",
          cancelled ? "Not received · no submission" : "Not received · transaction failed",
          { render: false }
        );
      }
      finishTransferFlow(cancelled ? "Proof 요청을 취소했습니다." : error.message, false, {
        retry: () => withdrawFromVeiled({ relayMode })
      });
    }
  } finally {
    setBusy(actionButton, false);
    renderKeplr();
  }
}

els.connectWallet.addEventListener("click", () => connectWallet().catch(reportAsyncError));
els.connectKeplr.addEventListener("click", () => connectKeplr().catch(reportAsyncError));
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
els.signSession.addEventListener("click", () => signSession().catch(reportAsyncError));
els.copyWalletAccount.addEventListener("click", () => copyWalletAccount().catch(error => toast(error.message)));
els.fundKeplr.addEventListener("click", fundKeplr);
els.setupKeplrPrivacy.addEventListener("click", () => setupKeplrPrivacy().catch(reportAsyncError));
els.copyKeplrShieldedAddress.addEventListener("click", () => copyKeplrShieldedAddress().catch(error => toast(error.message)));
els.copyKeplrDisclosurePubKey.addEventListener("click", () => copyKeplrDisclosurePubKey().catch(error => toast(error.message)));
els.refreshWalletBalance.addEventListener("click", () => refreshWalletBalance().catch(reportAsyncError));
els.scanKeplrNotes.addEventListener("click", () => scanKeplrNotes().catch(reportAsyncError));
els.backupNoteCache.addEventListener("click", () => backupNoteCache().catch(error => toast(error.message)));
els.resetRescanNotes.addEventListener("click", () => resetAndRescanNotes().catch(reportAsyncError));
els.noteRollbackHeight.addEventListener("input", () => updateNoteRollbackButton());
els.rollbackRescanNotes.addEventListener("click", () => rollbackAndRescanNotes().catch(reportAsyncError));
els.reconcileReservations.addEventListener("click", () => reconcileReservations().catch(reportAsyncError));
els.reservationRecoveryList.addEventListener("click", event => {
  const button = event.target.closest("[data-recover-reservation-operation]");
  if (!button || button.disabled) return;
  recoverReservationPreparation(button.dataset.recoverReservationOperation).catch(error => {
    if (isStalePrivacySessionError(error)) return;
    els.keplrTxState.textContent = "Reservation recovery blocked";
    toast(error.message);
  });
});
els.myKeplrSpendableOnly.addEventListener("change", event => {
  state.keplr.showSpendableOnly = event.target.checked;
  renderMyKeplrNotes();
});
els.sendFromKeplr.addEventListener("click", sendFromKeplr);
els.reconcileKeplrSend.addEventListener("click", () => reconcilePublicTransaction("send"));
els.clearPublicPendingState.addEventListener("click", () => clearPublicPendingTransactions().catch(reportAsyncError));
els.resetPrivatePendingState.addEventListener("click", () => resetCorruptPrivateRecoveryState().catch(reportAsyncError));
els.depositFromKeplr.addEventListener("click", depositFromKeplr);
els.reconcileKeplrDeposit.addEventListener("click", () => reconcilePublicTransaction("deposit"));
[
  els.keplrSendAmount,
  els.keplrSendRecipient,
  els.keplrDepositAmount,
  els.veiledTransferAmount,
  els.veiledWithdrawAmount,
  els.relayWithdrawAmount
].forEach(input => {
  input.addEventListener("input", updateAmountActionButtons);
});
els.veiledDisclosureAdvanced.addEventListener("change", renderTransferDisclosureAdvanced);
els.veiledDisclosureMode.addEventListener("change", renderTransferDisclosureAdvanced);
els.includeSelfViewDisclosure.addEventListener("change", renderTransferDisclosureAdvanced);
els.transferFromVeiled.addEventListener("click", transferFromVeiled);
els.addBatchTransferPayment.addEventListener("click", () => {
  addBatchTransferPayment();
  renderBatchTransfer();
  updateAmountActionButtons();
});
els.batchUseAuthorization.addEventListener("change", () => {
  renderBatchAuthorization();
  updateAmountActionButtons();
});
els.batchAuthorizationKind.addEventListener("change", updateAmountActionButtons);
els.transferBatchFromVeiled.addEventListener("click", transferBatchFromVeiled);
els.withdrawFromVeiled.addEventListener("click", withdrawFromVeiled);
els.relayWithdrawFromVeiled.addEventListener("click", () => withdrawFromVeiled({ relayMode: true }));
els.relayPreparedWithdraw.addEventListener("click", () => relayPreparedWithdraw().catch(reportAsyncError));
els.reconcileRelayWithdraw.addEventListener("click", () => reconcileRelayWithdrawFromInput().catch(reportAsyncError));
els.relayWithdrawRecoveryChoice.addEventListener("change", event => {
  selectRelayWithdrawRecovery(event.target.value).catch(reportAsyncError);
});
els.copyRelayWithdraw.addEventListener("click", () => copyRelayWithdraw().catch(reportAsyncError));
els.downloadRelayWithdraw.addEventListener("click", () => downloadRelayWithdraw().catch(reportAsyncError));
els.refreshAll.addEventListener("click", () => refreshHealth().catch(reportAsyncError));
els.refreshNotes.addEventListener("click", () => refreshNotes().catch(reportAsyncError));
els.refreshEvents.addEventListener("click", () => refreshEvents().catch(reportAsyncError));
els.decodeEventDisclosure.addEventListener("click", () => decodeSelectedEventDisclosure().catch(error => toast(error.message)));
els.decodeSelfViewDisclosure.addEventListener("click", () => decodeSelectedSelfViewDisclosure().catch(error => toast(error.message)));
els.decodeDisclosureSource.addEventListener("click", () => decodeDisclosureSource().catch(error => toast(error.message)));
if (els.refreshAuditorTransfers) {
  els.refreshAuditorTransfers.addEventListener("click", () => refreshAuditorTransfers().catch(reportAsyncError));
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
  refreshSelectedAccount().catch(reportAsyncError);
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
    resetWalletSession();
    renderWallet();
    renderKeplr();
    toast(`MetaMask network changed to ${chainId}. Reconnect wallet before continuing.`);
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
refreshHealth().catch(reportAsyncError);
