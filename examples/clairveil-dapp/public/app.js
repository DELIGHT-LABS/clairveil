import { createClairveilBrowserDappClient } from "clairveiljs/browser-dapp";
import { decodeShieldedAddress } from "clairveiljs/core";
import { computeAssetIdV1 } from "clairveiljs/protocol-v1";
import {
  bech32AddressToEvm,
  createEvmPrivacyPrecompileAdapter,
} from "clairveiljs/evm";
import {
  buildWithdrawMsgFromPayload,
  validateRelayWithdrawPayload,
} from "clairveiljs/payload";
import {
  createBrowserReservationStore,
  createNoteReservationManager,
  hashAmount,
  hashRecipient,
  isActiveReservationStatus,
  reservationHeartbeatIntervalMs,
  reservationStatuses,
} from "clairveiljs/reservation";
import { nextPrivacyScanOptions } from "clairveiljs/cosmos-client";
import { loadStaticDappConfig } from "./dapp-config.js";
import {
  createEncryptedBrowserMetadataStore,
  createEncryptedStateCodec,
  EncryptedIndexedDbNoteStore,
} from "./encrypted-browser-store.js";
import {
  batchTransferErrorRequiresReconciliation,
  batchTransferNeedsReconciliation,
  batchTransferReservationsSucceeded,
  computeBatchTransferPreviewState,
  pendingBatchTransferPayments,
  preparedBatchTransferFacts,
  selectNextAtomicBatchPayments,
} from "./batch-transfer-state.js";
import {
  privacyEventSelectionKey,
  sameTypedBatchEventIdentity,
  typedBatchEventIdentity,
} from "./batch-event-identity.js";
import {
  canRelayHandoffPayloadBeCopied,
  canRelaySnapshotBeSubmitted,
  expiredRelayReservationRecoveryTarget,
  isRelaySnapshotStructurallyReady,
  relayBroadcastTxHash,
  canReplanExpiredLocalReservation,
  cosmosTxExecutionOutcome,
  hasDurableNoBroadcastEvidence,
  parsePersistedRelayWithdrawState,
  relayReservationIDs,
  relayReservationStatus,
  relaySnapshotExpiresAtUnix,
  relaySnapshotIsExpired,
  sanitizeRelayWithdrawSnapshot,
  updateReservationBatchRecords,
} from "./relay-reservation-state.js";
import {
  evmBlockChainSnapshot,
  hasFailedEvmReceiptStatus,
  hasSuccessfulEvmReceiptStatus,
  isEvmReceiptConfirmationPending,
  shouldEscalateSuccessfulTxWithUnspentNullifiers,
} from "./transaction-status.js";

const completeNoteScanMaxPages = 1_000;
const depositProofRequestTimeoutMs = 120_000;
const depositProofResponseMaxBytes = 1 << 20;
const depositProofResponseVersion = "v1";
const dappApiRequestTimeoutMs = 30_000;
// The loopback relayer shells out to clairveild (which may perform strict ZK
// preflight) and then waits for inclusion. Its server-side budget is up to
// two minutes plus the inclusion lookup, so the generic 30s browser API
// timeout must not abort a still-live relay submission first.
const relaySubmissionRequestTimeoutMs = 155_000;
const dappApiResponseMaxBytes = 1 << 20;
const healthBootstrapRequestTimeoutMs = 30_000;
const healthBootstrapResponseMaxBytes = 1 << 20;
const batchTransferMaxInputs = 16;
const batchTransferMaxOutputs = 32;
const privacyEventsPageLimit = 100;
const auditorEventsPageLimit = 20;
// Batch disclosures live only in the validated privacy-scan-v2 projection,
// not in the compact event row. Start from the selected public event's exact
// predecessor cursor: scanning from genesis makes valid recent events
// unreachable once the chain has more than an arbitrary number of batches.
// Individual typed pages remain tightly bounded below.
const batchEventScanEventLimit = 64;
const batchEventScanOutputLimit = 128;
const batchEventScanMaxEncodedBytes = 1 << 20;
const batchTransferArtifactStoragePrefix =
  "clairveil:batch-transfer-artifacts:v1:";

function defaultMetaMaskState() {
  return {
    account: "",
    chainId: "",
    signatureHash: "",
  };
}

function defaultPrivacyScanPosition() {
  return {
    height: 0,
    globalSequence: 0,
    outputIndex: 0,
  };
}

function privacyScanCursorPosition(value, fallback = defaultPrivacyScanPosition()) {
  return {
    height: value?.height ?? fallback.height ?? 0,
    globalSequence:
      value?.global_sequence ??
      value?.globalSequence ??
      fallback.globalSequence ??
      fallback.global_sequence ??
      0,
    outputIndex:
      value?.output_index ??
      value?.outputIndex ??
      fallback.outputIndex ??
      fallback.output_index ??
      0,
  };
}

function defaultNoteScanCursor() {
  return {
    // privacy-scan-v2 has an ordered three-part cursor. Never reduce this to
    // a height or event sequence: a page can stop between outputs of one
    // event, and resuming a partial cursor would skip a spendable note.
    source: "privacy_scan",
    after: defaultPrivacyScanPosition(),
    nextCursor: defaultPrivacyScanPosition(),
    limit: 200,
    outputLimit: 200,
    eventLimit: 0,
    maxEncodedBytes: 0,
    maxPages: completeNoteScanMaxPages,
    hasMore: false,
    latestHeight: 0,
    latestSequence: 0,
    latestOutputIndex: 0,
    pagesScanned: 0,
    completed: false,
  };
}

function nullifierUsedFromResponse(value) {
  if (typeof value === "boolean") return value;
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  const used = value.used ?? value.Used;
  return typeof used === "boolean" ? used : null;
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
    faucetHash: "",
    faucetSent: "",
    faucetRecipient: "",
    shieldedAddress: "",
    disclosurePubKeyHex: "",
    rootSignatureBase64: "",
    rootSignatureHash: "",
    sendHash: "",
    depositHash: "",
    depositHeight: "",
    transferHash: "",
    batchTransferHash: "",
    withdrawHash: "",
    withdrawHeight: "",
    relayWithdrawHash: "",
    relayWithdrawHeight: "",
    relayWithdrawRelayer: "",
    relayWithdrawPayloadHash: "",
    relayWithdrawPayload: null,
    relayWithdrawPreparedData: null,
    relayWithdrawPayloadText: "",
    relayWithdrawPayloadAmount: "",
    relayWithdrawPayloadRecipient: "",
    relayWithdrawPayloadChainId: "",
    relayWithdrawPayloadExpiresAt: "",
    relayWithdrawPayloadSubmitted: false,
    relayWithdrawPayloadHandedOff: false,
    relayWithdrawPayloadVersion: 0,
    relayWithdrawPendingPayloads: [],
    relayWithdrawReservation: null,
    notesSummary: "",
    notes: [],
    noteReservationByNullifier: {},
    reservationRecordByID: {},
    manualReviewReservations: [],
    notesScanned: false,
    scanError: "",
    privacySetupFailed: false,
    noteScanCursor: defaultNoteScanCursor(),
    showSpendableOnly: false,
  };
}

function defaultAuditorState() {
  return {
    events: [],
    page: 1,
    hasMore: false,
    selectedEventKey: "",
    selectedTxHash: "",
    decoded: null,
    testScalar: "",
    testScalarError: "",
    testScalarMatchesAuditConfig: false,
    loading: false,
  };
}

function defaultPrivacyEventsState() {
  return {
    events: [],
    page: 1,
    hasMore: false,
    pageLoading: false,
    selectedEventKey: "",
    selectedTxHash: "",
    decoded: null,
    batchDecoded: [],
    error: "",
    loadError: "",
    loading: false,
  };
}

function defaultShieldedAddressBookState() {
  return {
    profileFingerprint: "",
    shieldedByName: {},
    shieldedError: "",
    loadingShielded: false,
  };
}

const state = {
  config: null,
  chainProfiles: [],
  selectedChainProfileId: "",
  accounts: [],
  selectedAccount: "alice",
  addressBook: defaultShieldedAddressBookState(),
  activeWallet: "",
  wallet: defaultMetaMaskState(),
  keplr: defaultKeplrState(),
  relayer: {
    balance: "",
    error: "",
  },
  auditor: defaultAuditorState(),
  privacyEvents: defaultPrivacyEventsState(),
  blockEvents: {
    events: [],
    error: "",
  },
  chainSafety: {
    key: "",
    status: "unknown",
    error: "",
    checkedAt: 0,
  },
};

const $ = (selector) => document.querySelector(selector);
let shieldedAddressBookPromise = null;
let shieldedAddressBookGeneration = 0;
let localAccountViewGeneration = 0;
let relayerViewGeneration = 0;
let healthViewGeneration = 0;
let browserClient = null;
let browserClientKey = "";
let auditorSessionGeneration = 0;
let privacyEventDisclosureGeneration = 0;
let privacyEventsRefreshGeneration = 0;
let reservationStore = null;
let reservationStoreKey = "";
let relayPendingPayloadSequence = 0;
let relayWithdrawPayloadGeneration = 0;
let relayPayloadExpiryReconciliationTimer = null;
let relayPayloadExpiryReconciliationGeneration = 0;
let preparedRelayReservationHeartbeatTimer = null;
let preparedRelayReservationHeartbeatInFlight = null;
let preparedRelayReservationHeartbeatGeneration = 0;
let relayWithdrawPayloadCopyInFlight = false;
let relayWithdrawPayloadCopyLock = null;
let relaySubmissionInFlight = false;
let relaySubmissionLock = null;
let relayHandoffBoundaryLock = null;
let depositInFlight = false;
let depositInFlightLock = null;
let noteScanInFlight = false;
let noteScanLock = null;
let noteScanResetInFlight = false;
let noteScanResetLock = null;
let privacySetupInFlight = null;
let privacyValueActionLock = null;
const pendingRelayRecoveryLocks = new Map();
let walletConnectionSession = null;
let reservationManager = null;
let reservationManagerKey = "";
let reservationWorkerID = "";
let serverConfigAvailable = true;
// ClairveilJS 0.2 starts a fresh privacy-note-v1 cache/reservation epoch.
// Never revive 0.1 notes, leases, or relay payload metadata under the new
// fixed-envelope contract; the first scan repopulates this namespace.
const reservationStoreNamespacePrefix = "clairveil:note-reservations:v2:";
const walletNoteStoreNamespacePrefix = "clairveil:wallet-notes:v2:";
const relayWithdrawPayloadStoragePrefix =
  "clairveil:relay-withdraw-payloads:v2:";
const webClientConfigSchemaVersion = "clairveil-web-client-config-v1";
const reservationRecoveryGraceMs = 15 * 60 * 1000;
const unresolvedReservationManualReviewAgeMs = 24 * 60 * 60 * 1000;
const successfulTxNullifierConflictGraceMs = 2 * 60 * 1000;
const relayPayloadExpiryReconciliationRetryMs = 30_000;
const maximumBrowserTimerDelayMs = 2_147_000_000;
let walletNoteStore = null;
let walletNoteStoreKey = "";
let relayMetadataStore = null;
let relayMetadataStoreKey = "";
let batchTransferArtifactStore = null;
let batchTransferArtifactStoreKey = "";
let batchTransferExpanded = false;
let batchTransferRowSequence = 0;
let batchTransferInFlight = false;
let batchTransferConfirmationResolve = null;
const completedBatchTransferItemIDs = new Set();
// Keep writes ordered within a persistence namespace, including across an
// in-memory session reset. A different wallet/profile uses another key and
// must not wait for a stale namespace's storage operation.
const relayMetadataWrites = new Map();
let chainSafetyExpiryTimer = null;
let chainSafetyRefreshGeneration = 0;
let privacySessionGeneration = 0;
const activeReservationHeartbeatStops = new Set();
// SDK proof requests accept AbortSignal. Keep only preparation controllers in
// this set: a wallet approval or broadcast is an external boundary and must
// never be cancelled merely because a browser session is replaced.
const activePrivacyPreparationControllers = new Set();
const preparedPrivacySessionContexts = new WeakMap();

function configuredChainProfile() {
  if (!state.config) return null;
  return {
    id: state.config.activeChainProfileId || "configured",
    label: state.config.chainId || "Configured chain",
    chainName: state.config.chainId || "Configured chain",
    transport: state.config.transport || "cosmos",
    wallet: state.config.transport === "evm" ? "metamask" : "keplr",
    chainId: state.config.chainId,
    accountPrefix: state.config.accountPrefix,
    shieldedPrefix: state.config.shieldedPrefix,
    denom: state.config.denom,
    displayDenom: state.config.displayDenom,
    coinDecimals: state.config.coinDecimals,
    evmRpc: state.config.evmRpc,
    evmChainId: state.config.evmChainId,
    evmChainName: state.config.evmChainName,
    keplrChainInfo: state.config.keplrChainInfo,
  };
}

const webClientRootConfigFields = new Set([
  "schemaVersion",
  "activeChainProfileId",
  "chainProfiles",
  "serverBacked",
  "modeLabel",
  "home",
  "localSignerHome",
  "localSignerBin",
  "localTestMode",
  "chainId",
  "rpc",
  "rest",
  "proverUrl",
  "transport",
  "denom",
  "displayDenom",
  "coinDecimals",
  "accountPrefix",
  "shieldedPrefix",
  "keplrChainInfo",
  "evmRpc",
  "evmChainId",
  "evmChainName",
  "evmPrivacyPrecompileAddress",
  "evmGasLimit",
  "evmSendGasLimit",
  "serverFeatures",
]);

const webClientProfileFields = new Set([
  "id",
  "label",
  "chainName",
  "transport",
  "wallet",
  "chainId",
  "rpc",
  "rest",
  "restEndpoints",
  "proverUrl",
  "depositProofUrl",
  "accountPrefix",
  "shieldedPrefix",
  "denom",
  "displayDenom",
  "coinDecimals",
  "keplrCoinType",
  "gasPriceStep",
  "keplrChainInfo",
  "evmRpc",
  "evmChainId",
  "evmChainName",
  "evmPrivacyPrecompileAddress",
  "evmGasLimit",
  "evmSendGasLimit",
]);

const keplrChainInfoFields = new Set([
  "chainId",
  "chainName",
  "rpc",
  "rest",
  "bip44",
  "bech32Config",
  "currencies",
  "feeCurrencies",
  "stakeCurrency",
  "features",
]);

const keplrBip44Fields = new Set(["coinType"]);

const keplrBech32ConfigFields = new Set([
  "bech32PrefixAccAddr",
  "bech32PrefixAccPub",
  "bech32PrefixValAddr",
  "bech32PrefixValPub",
  "bech32PrefixConsAddr",
  "bech32PrefixConsPub",
]);

const keplrCurrencyFields = new Set([
  "coinDenom",
  "coinMinimalDenom",
  "coinDecimals",
]);

const keplrFeeCurrencyFields = new Set([
  ...keplrCurrencyFields,
  "gasPriceStep",
]);

const webClientServerFeatureFields = new Set([
  "localTestMode",
  "localSigners",
  "localSignerAdmin",
  "localSignerSetup",
  "faucet",
  "depositProof",
  "relayer",
  "auditorAdmin",
  "proverProxy",
  "batchTransfer",
]);

function isPlainConfigObject(value) {
  return Boolean(value && typeof value === "object" && !Array.isArray(value));
}

function assertNoUnexpectedConfigFields(value, allowed, label) {
  for (const field of Object.keys(value)) {
    if (!allowed.has(field)) {
      throw new Error(`Clairveil WebApp ${label} contains unsupported field ${field}`);
    }
  }
}

function assertConfigString(value, label, pattern, { minLength = 1, maxLength = 128 } = {}) {
  if (
    typeof value !== "string" ||
    value.length < minLength ||
    value.length > maxLength ||
    (pattern && !pattern.test(value))
  ) {
    throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
  }
}

function assertConfigUrl(value, label) {
  assertConfigString(value, label, undefined, { maxLength: 4096 });
  let url;
  try {
    url = new URL(value);
  } catch {
    throw new Error(`Clairveil WebApp configuration ${label} must be an HTTP(S) URL`);
  }
  if (!url.hostname || !["http:", "https:"].includes(url.protocol)) {
    throw new Error(`Clairveil WebApp configuration ${label} must be an HTTP(S) URL`);
  }
  if (url.username || url.password || url.search || url.hash) {
    throw new Error(
      `Clairveil WebApp configuration ${label} must not include URL credentials, queries, or fragments`,
    );
  }
}

function isLoopbackHostname(hostname) {
  const normalized = String(hostname || "")
    .trim()
    .toLowerCase()
    .replace(/^\[|\]$/g, "");
  return (
    normalized === "localhost" ||
    normalized === "::1" ||
    /^127(?:\.\d{1,3}){3}$/.test(normalized)
  );
}

function assertBrowserDeploymentEndpointPolicy(config) {
  const pageLocation = globalThis.location;
  if (!pageLocation?.protocol || !pageLocation?.hostname) return;
  const directLoopbackPage = isLoopbackHostname(pageLocation.hostname);
  if (directLoopbackPage) {
    return;
  }
  if (pageLocation.protocol !== "https:") {
    throw new Error(
      "Clairveil WebApp must be served over HTTPS outside direct loopback local development",
    );
  }
  for (const profile of config.chainProfiles || []) {
    const endpoints = [
      ["rpc", profile.rpc],
      ["rest", profile.rest],
      ...((profile.restEndpoints || []).map((endpoint, index) => [
        `restEndpoints[${index}]`,
        endpoint,
      ])),
      ["proverUrl", profile.proverUrl],
      ...(profile.depositProofUrl
        ? [["depositProofUrl", profile.depositProofUrl]]
        : []),
      ...(profile.transport === "evm" ? [["evmRpc", profile.evmRpc]] : []),
      ...(profile.keplrChainInfo
        ? [
            ["keplrChainInfo.rpc", profile.keplrChainInfo.rpc],
            ["keplrChainInfo.rest", profile.keplrChainInfo.rest],
          ]
        : []),
    ];
    for (const [field, value] of endpoints) {
      const endpoint = new URL(value);
      if (
        endpoint.protocol !== "https:" ||
        isLoopbackHostname(endpoint.hostname) ||
        endpoint.username ||
        endpoint.password ||
        endpoint.search ||
        endpoint.hash
      ) {
        throw new Error(
          `Clairveil WebApp profile ${profile.id}.${field} must use a public HTTPS endpoint outside direct loopback local development`,
        );
      }
    }
  }
}

function sameConfigUrl(left, right) {
  return new URL(left).toString() === new URL(right).toString();
}

function sameConfigValue(left, right) {
  if (Object.is(left, right)) return true;
  if (Array.isArray(left) || Array.isArray(right)) {
    return (
      Array.isArray(left) &&
      Array.isArray(right) &&
      left.length === right.length &&
      left.every((value, index) => sameConfigValue(value, right[index]))
    );
  }
  if (!isPlainConfigObject(left) || !isPlainConfigObject(right)) return false;
  const leftKeys = Object.keys(left).sort();
  const rightKeys = Object.keys(right).sort();
  return (
    leftKeys.length === rightKeys.length &&
    leftKeys.every(
      (key, index) =>
        key === rightKeys[index] && sameConfigValue(left[key], right[key]),
    )
  );
}

function canonicalConfigValue(value) {
  if (Array.isArray(value)) {
    return value.map((entry) => canonicalConfigValue(entry));
  }
  if (!isPlainConfigObject(value)) return value;
  const canonical = {};
  for (const key of Object.keys(value).sort()) {
    canonical[key] = canonicalConfigValue(value[key]);
  }
  return canonical;
}

function assertKeplrChainInfoMatchesProfile(profile) {
  const chainInfo = profile.keplrChainInfo;
  if (!isPlainConfigObject(chainInfo)) {
    throw new Error(`Clairveil WebApp profile ${profile.id}.keplrChainInfo is invalid`);
  }
  assertNoUnexpectedConfigFields(
    chainInfo,
    keplrChainInfoFields,
    `profile ${profile.id}.keplrChainInfo`,
  );
  assertConfigString(
    chainInfo.chainId,
    `profile ${profile.id}.keplrChainInfo.chainId`,
  );
  assertConfigString(
    chainInfo.chainName,
    `profile ${profile.id}.keplrChainInfo.chainName`,
  );
  assertConfigUrl(chainInfo.rpc, `profile ${profile.id}.keplrChainInfo.rpc`);
  assertConfigUrl(chainInfo.rest, `profile ${profile.id}.keplrChainInfo.rest`);
  if (chainInfo.chainId !== profile.chainId) {
    throw new Error(
      `Clairveil WebApp profile ${profile.id}.keplrChainInfo.chainId must match profile chainId`,
    );
  }
  if (!sameConfigUrl(chainInfo.rpc, profile.rpc)) {
    throw new Error(
      `Clairveil WebApp profile ${profile.id}.keplrChainInfo.rpc must match profile rpc`,
    );
  }
  if (!sameConfigUrl(chainInfo.rest, profile.rest)) {
    throw new Error(
      `Clairveil WebApp profile ${profile.id}.keplrChainInfo.rest must match profile rest`,
    );
  }
  if (chainInfo.chainName !== profile.chainName) {
    throw new Error(
      `Clairveil WebApp profile ${profile.id}.keplrChainInfo.chainName must match profile chainName`,
    );
  }

  if (!isPlainConfigObject(chainInfo.bip44)) {
    throw new Error(`Clairveil WebApp profile ${profile.id}.keplrChainInfo.bip44 is invalid`);
  }
  assertNoUnexpectedConfigFields(
    chainInfo.bip44,
    keplrBip44Fields,
    `profile ${profile.id}.keplrChainInfo.bip44`,
  );
  assertConfigInteger(
    chainInfo.bip44.coinType,
    `profile ${profile.id}.keplrChainInfo.bip44.coinType`,
    { min: 0, max: 4294967295 },
  );
  if (chainInfo.bip44.coinType !== profile.keplrCoinType) {
    throw new Error(
      `Clairveil WebApp profile ${profile.id}.keplrChainInfo.bip44.coinType must match profile keplrCoinType`,
    );
  }

  if (!isPlainConfigObject(chainInfo.bech32Config)) {
    throw new Error(`Clairveil WebApp profile ${profile.id}.keplrChainInfo.bech32Config is invalid`);
  }
  assertNoUnexpectedConfigFields(
    chainInfo.bech32Config,
    keplrBech32ConfigFields,
    `profile ${profile.id}.keplrChainInfo.bech32Config`,
  );
  const expectedBech32Prefixes = {
    bech32PrefixAccAddr: profile.accountPrefix,
    bech32PrefixAccPub: `${profile.accountPrefix}pub`,
    bech32PrefixValAddr: `${profile.accountPrefix}valoper`,
    bech32PrefixValPub: `${profile.accountPrefix}valoperpub`,
    bech32PrefixConsAddr: `${profile.accountPrefix}valcons`,
    bech32PrefixConsPub: `${profile.accountPrefix}valconspub`,
  };
  for (const [field, expected] of Object.entries(expectedBech32Prefixes)) {
    assertConfigString(
      chainInfo.bech32Config[field],
      `profile ${profile.id}.keplrChainInfo.bech32Config.${field}`,
    );
    if (chainInfo.bech32Config[field] !== expected) {
      throw new Error(
        `Clairveil WebApp profile ${profile.id}.keplrChainInfo.bech32Config.${field} must match profile accountPrefix`,
      );
    }
  }

  const assertCurrencyMatchesProfile = (currency, label, { fee = false } = {}) => {
    if (!isPlainConfigObject(currency)) {
      throw new Error(`Clairveil WebApp ${label} is invalid`);
    }
    assertNoUnexpectedConfigFields(
      currency,
      fee ? keplrFeeCurrencyFields : keplrCurrencyFields,
      label,
    );
    assertConfigString(currency.coinDenom, `${label}.coinDenom`);
    assertConfigString(currency.coinMinimalDenom, `${label}.coinMinimalDenom`);
    assertConfigInteger(currency.coinDecimals, `${label}.coinDecimals`, { min: 0, max: 255 });
    if (
      currency.coinDenom !== profile.displayDenom ||
      currency.coinMinimalDenom !== profile.denom ||
      currency.coinDecimals !== profile.coinDecimals
    ) {
      throw new Error(`Clairveil WebApp ${label} must match profile currency`);
    }
    if (fee) {
      assertConfigGasPriceStep(currency.gasPriceStep, `${label}.gasPriceStep`);
      if (!sameConfigValue(currency.gasPriceStep, profile.gasPriceStep)) {
        throw new Error(`Clairveil WebApp ${label}.gasPriceStep must match profile gasPriceStep`);
      }
    }
  };
  for (const [field, fee] of [["currencies", false], ["feeCurrencies", true]]) {
    const currencies = chainInfo[field];
    if (!Array.isArray(currencies) || currencies.length !== 1) {
      throw new Error(
        `Clairveil WebApp profile ${profile.id}.keplrChainInfo.${field} must contain exactly one configured currency`,
      );
    }
    assertCurrencyMatchesProfile(
      currencies[0],
      `profile ${profile.id}.keplrChainInfo.${field}[0]`,
      { fee },
    );
  }
  assertCurrencyMatchesProfile(
    chainInfo.stakeCurrency,
    `profile ${profile.id}.keplrChainInfo.stakeCurrency`,
  );
  if (!Array.isArray(chainInfo.features) || chainInfo.features.length !== 0) {
    throw new Error(`Clairveil WebApp profile ${profile.id}.keplrChainInfo.features must be empty`);
  }
}

function assertConfigInteger(value, label, { min = 0, max = Number.MAX_SAFE_INTEGER } = {}) {
  if (!Number.isInteger(value) || value < min || value > max) {
    throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
  }
}

function assertConfigGasPriceStep(value, label) {
  if (!isPlainConfigObject(value)) {
    throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
  }
  assertNoUnexpectedConfigFields(value, new Set(["low", "average", "high"]), label);
  for (const field of ["low", "average", "high"]) {
    if (typeof value[field] !== "number" || !Number.isFinite(value[field]) || value[field] <= 0) {
      throw new Error(`Clairveil WebApp configuration ${label}.${field} is invalid`);
    }
  }
}

function assertOptionalRootConfigField(config, field, check) {
  if (config[field] !== undefined) check(config[field], field);
}

function assertWebClientProfileSchema(profile) {
  if (!isPlainConfigObject(profile)) {
    throw new Error("Clairveil WebApp profile is invalid");
  }
  assertNoUnexpectedConfigFields(profile, webClientProfileFields, "profile");
  assertConfigString(profile.id, "profile.id", /^[A-Za-z0-9][A-Za-z0-9._-]*$/);
  assertConfigString(profile.label, `profile ${profile.id}.label`);
  assertConfigString(profile.chainName, `profile ${profile.id}.chainName`);
  if (!["cosmos", "evm"].includes(profile.transport)) {
    throw new Error(`Clairveil WebApp profile ${profile.id} has invalid transport`);
  }
  if ((profile.transport === "cosmos" && profile.wallet !== "keplr") ||
      (profile.transport === "evm" && profile.wallet !== "metamask")) {
    throw new Error(`Clairveil WebApp profile ${profile.id} has an invalid transport/wallet pair`);
  }
  assertConfigString(profile.chainId, `profile ${profile.id}.chainId`);
  assertConfigUrl(profile.rpc, `profile ${profile.id}.rpc`);
  assertConfigUrl(profile.rest, `profile ${profile.id}.rest`);
  assertConfigUrl(profile.proverUrl, `profile ${profile.id}.proverUrl`);
  if (profile.depositProofUrl !== undefined) {
    assertConfigUrl(
      profile.depositProofUrl,
      `profile ${profile.id}.depositProofUrl`,
    );
  }
  assertConfigString(profile.accountPrefix, `profile ${profile.id}.accountPrefix`, /^[a-z][a-z0-9]*$/, { maxLength: 32 });
  assertConfigString(profile.shieldedPrefix, `profile ${profile.id}.shieldedPrefix`, /^[a-z][a-z0-9]*$/, { maxLength: 32 });
  assertConfigString(profile.denom, `profile ${profile.id}.denom`, /^[A-Za-z][A-Za-z0-9/:._-]*$/, {
    minLength: 3,
  });
  assertConfigString(profile.displayDenom, `profile ${profile.id}.displayDenom`, undefined, { maxLength: 32 });
  assertConfigInteger(profile.coinDecimals, `profile ${profile.id}.coinDecimals`, { min: 0, max: 255 });
  if (profile.restEndpoints !== undefined) {
    if (!Array.isArray(profile.restEndpoints) || !profile.restEndpoints.length) {
      throw new Error(`Clairveil WebApp profile ${profile.id}.restEndpoints is invalid`);
    }
    const endpoints = new Set();
    for (const endpoint of profile.restEndpoints) {
      assertConfigUrl(endpoint, `profile ${profile.id}.restEndpoints`);
      if (endpoints.has(endpoint)) {
        throw new Error(`Clairveil WebApp profile ${profile.id}.restEndpoints has duplicates`);
      }
      endpoints.add(endpoint);
    }
  }
  if (profile.keplrCoinType !== undefined) {
    assertConfigInteger(profile.keplrCoinType, `profile ${profile.id}.keplrCoinType`, { min: 0, max: 4294967295 });
  }
  if (profile.gasPriceStep !== undefined) {
    assertConfigGasPriceStep(profile.gasPriceStep, `profile ${profile.id}.gasPriceStep`);
  }
  if (profile.transport === "cosmos") {
    if (
      profile.keplrCoinType === undefined ||
      profile.gasPriceStep === undefined ||
      profile.keplrChainInfo === undefined
    ) {
      throw new Error(`Clairveil WebApp profile ${profile.id} is missing Cosmos wallet configuration`);
    }
    assertKeplrChainInfoMatchesProfile(profile);
    return;
  }
  if (
    profile.keplrCoinType !== undefined ||
    profile.gasPriceStep !== undefined ||
    profile.keplrChainInfo !== undefined
  ) {
    throw new Error(
      `Clairveil WebApp EVM profile ${profile.id} must not include Keplr wallet configuration`,
    );
  }
  assertConfigUrl(profile.evmRpc, `profile ${profile.id}.evmRpc`);
  assertConfigString(profile.evmChainId, `profile ${profile.id}.evmChainId`, /^0x[0-9a-fA-F]+$/);
  assertConfigString(profile.evmChainName, `profile ${profile.id}.evmChainName`);
  assertConfigString(profile.evmPrivacyPrecompileAddress, `profile ${profile.id}.evmPrivacyPrecompileAddress`, /^0x[0-9a-fA-F]{40}$/);
  assertConfigString(profile.evmGasLimit, `profile ${profile.id}.evmGasLimit`, /^0x[0-9a-fA-F]+$/);
  assertConfigString(profile.evmSendGasLimit, `profile ${profile.id}.evmSendGasLimit`, /^0x[0-9a-fA-F]+$/);
}

function assertValidatedDappConfig(config) {
  if (!config || typeof config !== "object") {
    throw new Error("Clairveil WebApp configuration is missing");
  }
  if (config.schemaVersion !== webClientConfigSchemaVersion) {
    throw new Error(
      `Unsupported Clairveil WebApp configuration version: ${String(config.schemaVersion || "missing")}`,
    );
  }
  assertNoUnexpectedConfigFields(config, webClientRootConfigFields, "configuration");
  assertConfigString(config.activeChainProfileId, "activeChainProfileId", /^[A-Za-z0-9][A-Za-z0-9._-]*$/);
  if (!Array.isArray(config.chainProfiles) || !config.chainProfiles.length) {
    throw new Error("Clairveil WebApp configuration has no chain profiles");
  }
  assertOptionalRootConfigField(config, "serverBacked", (value, label) => {
    if (typeof value !== "boolean") throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
  });
  assertOptionalRootConfigField(config, "localTestMode", (value, label) => {
    if (typeof value !== "boolean") throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
  });
  for (const field of ["modeLabel", "chainId", "displayDenom", "evmChainName"]) {
    assertOptionalRootConfigField(config, field, (value, label) => assertConfigString(value, label));
  }
  if (config.transport !== undefined && !["cosmos", "evm"].includes(config.transport)) {
    throw new Error("Clairveil WebApp configuration transport is invalid");
  }
  for (const field of ["home", "localSignerHome", "localSignerBin"]) {
    assertOptionalRootConfigField(config, field, (value, label) => {
      if (typeof value !== "string") {
        throw new Error(`Clairveil WebApp configuration ${label} is invalid`);
      }
    });
  }
  for (const field of ["rpc", "rest", "proverUrl", "evmRpc"]) {
    assertOptionalRootConfigField(config, field, (value, label) => assertConfigUrl(value, label));
  }
  for (const field of ["accountPrefix", "shieldedPrefix"]) {
    assertOptionalRootConfigField(config, field, (value, label) =>
      assertConfigString(value, label, /^[a-z][a-z0-9]*$/, { maxLength: 32 }),
    );
  }
  assertOptionalRootConfigField(config, "denom", (value, label) =>
    assertConfigString(value, label, /^[A-Za-z][A-Za-z0-9/:._-]*$/, { minLength: 3 }),
  );
  assertOptionalRootConfigField(config, "coinDecimals", (value, label) =>
    assertConfigInteger(value, label, { min: 0, max: 255 }),
  );
  assertOptionalRootConfigField(config, "evmChainId", (value, label) =>
    assertConfigString(value, label, /^0x[0-9a-fA-F]+$/),
  );
  for (const field of ["evmPrivacyPrecompileAddress"]) {
    assertOptionalRootConfigField(config, field, (value, label) =>
      assertConfigString(value, label, /^0x[0-9a-fA-F]{40}$/),
    );
  }
  for (const field of ["evmGasLimit", "evmSendGasLimit"]) {
    assertOptionalRootConfigField(config, field, (value, label) =>
      assertConfigString(value, label, /^0x[0-9a-fA-F]+$/),
    );
  }
  if (config.keplrChainInfo !== undefined && !isPlainConfigObject(config.keplrChainInfo)) {
    throw new Error("Clairveil WebApp configuration keplrChainInfo is invalid");
  }
  if (config.serverFeatures !== undefined) {
    if (!isPlainConfigObject(config.serverFeatures)) {
      throw new Error("Clairveil WebApp configuration serverFeatures is invalid");
    }
    assertNoUnexpectedConfigFields(config.serverFeatures, webClientServerFeatureFields, "serverFeatures");
    for (const [field, value] of Object.entries(config.serverFeatures)) {
      if (typeof value !== "boolean") {
        throw new Error(`Clairveil WebApp configuration serverFeatures.${field} is invalid`);
      }
    }
  }
  const profilesByID = new Map();
  for (const profile of config.chainProfiles) {
    assertWebClientProfileSchema(profile);
    const id = profile.id;
    if (!id || profilesByID.has(id)) {
      throw new Error("Clairveil WebApp configuration has duplicate or empty profile IDs");
    }
    profilesByID.set(id, profile);
  }
  const active = profilesByID.get(String(config.activeChainProfileId || ""));
  if (!active) {
    throw new Error("Clairveil WebApp configuration selects an unknown active profile");
  }
  if (active.transport === "evm" && config.keplrChainInfo !== undefined) {
    throw new Error(
      "Clairveil WebApp configuration must not emit keplrChainInfo for an active EVM profile",
    );
  }
  if (
    config.keplrChainInfo !== undefined &&
    !sameConfigValue(config.keplrChainInfo, active.keplrChainInfo)
  ) {
    throw new Error(
      `Clairveil WebApp configuration keplrChainInfo disagrees with active profile ${active.id}`,
    );
  }
  for (const field of [
    "chainId",
    "rpc",
    "rest",
    "proverUrl",
    "transport",
    "denom",
    "displayDenom",
    "coinDecimals",
    "accountPrefix",
    "shieldedPrefix",
    "evmRpc",
    "evmChainId",
    "evmChainName",
    "evmPrivacyPrecompileAddress",
    "evmGasLimit",
    "evmSendGasLimit",
  ]) {
    // The legacy top-level EVM response may describe the host account prefix.
    // The active profile's accountPrefix is the privacy identity prefix and is
    // the only value passed to ClairveilJS for EVM flows.
    if (field === "accountPrefix" && active.transport === "evm") continue;
    if (
      config[field] !== undefined &&
      (active[field] === undefined || String(config[field]) !== String(active[field]))
    ) {
      throw new Error(
        `Clairveil WebApp configuration ${field} disagrees with active profile ${active.id}`,
      );
    }
  }
  return config;
}

function activeChainProfile() {
  return (
    state.chainProfiles.find(
      (profile) => profile.id === state.selectedChainProfileId,
    ) ||
    state.chainProfiles.find(
      (profile) => profile.id === state.config?.activeChainProfileId,
    ) ||
    configuredChainProfile()
  );
}

function activeWalletKind() {
  const profile = activeChainProfile();
  return (
    profile?.wallet || (profile?.transport === "evm" ? "metamask" : "keplr")
  );
}

function activeTransparentAddressFormat() {
  const profile = activeChainProfile();
  return profile?.transport === "evm" || activeWalletKind() === "metamask"
    ? "evm"
    : "bech32";
}

function isEvmTransparentMode(walletKind = activeWalletKind()) {
  return (
    activeTransparentAddressFormat() === "evm" ||
    walletKind === "metamask" ||
    walletKind === "evm"
  );
}

function activeKeplrChainInfo() {
  return activeChainProfile()?.keplrChainInfo || state.config?.keplrChainInfo;
}

function selectedProfileMatchesServer(profile = activeChainProfile()) {
  if (state.config?.serverBacked === false) return true;
  if (!profile || !state.config) return true;
  return (
    profile.transport === state.config.transport &&
    profile.chainId === state.config.chainId
  );
}

function accountPrefix() {
  const profile = activeChainProfile();
  return (
    profile?.accountPrefix ||
    state.config?.accountPrefix ||
    state.config?.keplrChainInfo?.bech32Config?.bech32PrefixAccAddr ||
    "clair"
  );
}

function shieldedPrefix() {
  return (
    activeChainProfile()?.shieldedPrefix ||
    state.config?.shieldedPrefix ||
    "clairs"
  );
}

function isConfiguredShieldedAddress(address) {
  try {
    // A prefix check accepts truncated or typoed bech32 strings.  Those used
    // to reach batch payload construction and surface as a misleading prover
    // failure even though no proof request was made.
    decodeShieldedAddress(String(address || "").trim(), {
      shieldedPrefix: shieldedPrefix(),
    });
    return true;
  } catch {
    return false;
  }
}

function baseDenom() {
  return activeChainProfile()?.denom || state.config?.denom || "uclair";
}

function displayDenom() {
  return (
    activeChainProfile()?.displayDenom || state.config?.displayDenom || "CLAIR"
  );
}

function serverFeature(name) {
  // Every server feature is a local/server-backed capability. Static
  // artifacts may use browser-owned flows and pinned public endpoints, but
  // must not expose a stale server feature as a local helper route.
  return (
    state.config?.serverBacked === true &&
    Boolean(state.config?.serverFeatures?.[name])
  );
}

function localTestBackendEnabled() {
  return serverFeature("localTestMode");
}

function batchTransferFeatureEnabled() {
  return (
    serverFeature("batchTransfer") &&
    String(activeChainProfile()?.transport || "").toLowerCase() === "cosmos"
  );
}

function renderServerFeatureVisibility() {
  const localSigners = serverFeature("localSigners");
  const faucet = serverFeature("faucet");
  const auditorAdmin = serverFeature("auditorAdmin");

  if (els.localSignerPanel) {
    els.localSignerPanel.hidden = !localSigners;
  }
  // Relay payload preparation and copy are browser-owned handoff actions.
  // Only the optional local submission adapter is server-feature gated.
  if (els.relayerPanel) els.relayerPanel.hidden = false;
  if (els.faucetRow) {
    els.faucetRow.hidden = !faucet;
  }
  for (const row of [
    els.localHomeRow,
    els.faucetHashRow,
    els.faucetSentRow,
    els.faucetRecipientRow,
  ]) {
    if (row) row.hidden = !localTestBackendEnabled();
  }
  if (els.auditorSection) {
    els.auditorSection.hidden = !auditorAdmin;
  }
  if (!auditorAdmin) {
    resetAuditorSession();
  }
  renderBatchTransferVisibility();
}

function expectedEvmChainIdHex() {
  const value = String(
    activeChainProfile()?.evmChainId || state.config?.evmChainId || "",
  ).trim();
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
    const text = url.toString();
    return trim ? text.replace(/\/$/, "") : text;
  } catch {
    return trim ? String(configured || "").replace(/\/$/, "") : configured;
  }
}

function evmRpcUrlForWallet(profile = activeChainProfile()) {
  const configured =
    profile?.evmRpc || state.config?.evmRpc || "http://127.0.0.1:8545";
  return browserEndpointUrl(configured);
}

function browserRpcUrl(profile = activeChainProfile()) {
  return browserEndpointUrl(profile?.rpc || state.config?.rpc || "", {
    trim: true,
  });
}

function browserRestUrl(profile = activeChainProfile()) {
  const configured = profile?.rest || state.config?.rest || "";
  return browserEndpointUrl(configured, { trim: true });
}

function browserRestEndpoints(profile = activeChainProfile()) {
  return (profile?.restEndpoints || []).map((configured) =>
    browserEndpointUrl(configured, { trim: true }),
  );
}

function browserProverUrl(
  profile = activeChainProfile(),
  {
    serverFeatures = state.config?.serverFeatures,
    serverBacked = state.config?.serverBacked,
  } = {},
) {
  const configured = profile?.proverUrl || state.config?.proverUrl || "";
  if (serverBacked === true && serverFeatures?.proverProxy === true && configured) {
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
  return browserEndpointUrl(profile?.depositProofUrl || "", { trim: true });
}

function hasLocalDepositProofProvider() {
  // The loopback helper is a server-backed local-test feature. A static
  // artifact must never make Deposit available merely because a stale or
  // misconfigured serverFeatures object says the helper exists.
  return state.config?.serverBacked === true && serverFeature("depositProof");
}

function hasDepositProofProvider(profile = activeChainProfile()) {
  return Boolean(browserDepositProofUrl(profile)) || hasLocalDepositProofProvider();
}

function privacySessionInvalidatedError() {
  const error = new Error(
    "Privacy session changed while this operation was in progress. Reconnect the wallet and refresh Notes before retrying.",
  );
  error.name = "PrivacySessionInvalidatedError";
  error.privacySessionInvalidated = true;
  return error;
}

function beginPrivacySessionOperation({ reservationManager = null } = {}) {
  return {
    generation: privacySessionGeneration,
    reservationManager,
  };
}

function beginWalletConnection(wallet) {
  const profile = activeChainProfile();
  const connection = Object.freeze({
    wallet,
    session: beginPrivacySessionOperation(),
    profileID: state.selectedChainProfileId,
    profileFingerprint: profileSessionFingerprint(profile),
  });
  walletConnectionSession = connection;
  return connection;
}

function isWalletConnectionCurrent(connection) {
  const profile = activeChainProfile();
  return Boolean(
    connection &&
      walletConnectionSession === connection &&
      isPrivacySessionCurrent(connection.session) &&
      state.selectedChainProfileId === connection.profileID &&
      profileSessionFingerprint(profile) === connection.profileFingerprint,
  );
}

function assertWalletConnectionCurrent(connection) {
  if (isWalletConnectionCurrent(connection)) return;
  throw privacySessionInvalidatedError();
}

function endWalletConnection(connection) {
  if (walletConnectionSession === connection) {
    walletConnectionSession = null;
  }
}

function isPrivacySessionCurrent(session) {
  return !session || session.generation === privacySessionGeneration;
}

function assertPrivacySessionCurrent(session) {
  if (isPrivacySessionCurrent(session)) return;
  throw privacySessionInvalidatedError();
}

async function withPrivacySessionGuard(session, task) {
  assertPrivacySessionCurrent(session);
  try {
    const result = await task();
    assertPrivacySessionCurrent(session);
    return result;
  } catch (error) {
    assertPrivacySessionCurrent(session);
    throw error;
  }
}

function invalidatePrivacySessionOperations() {
  for (const controller of activePrivacyPreparationControllers) {
    controller.abort();
  }
  activePrivacyPreparationControllers.clear();
  stopRelayPayloadExpiryReconciliation();
  stopPreparedRelayReservationHeartbeat();
  for (const stop of [...activeReservationHeartbeatStops]) {
    stop();
  }
  privacySessionGeneration += 1;
  walletConnectionSession = null;
  relayWithdrawPayloadCopyInFlight = false;
  relayWithdrawPayloadCopyLock = null;
  relaySubmissionInFlight = false;
  relaySubmissionLock = null;
  relayHandoffBoundaryLock = null;
  depositInFlight = false;
  depositInFlightLock = null;
  noteScanInFlight = false;
  noteScanLock = null;
  noteScanResetInFlight = false;
  noteScanResetLock = null;
  privacySetupInFlight = null;
  privacyValueActionLock = null;
  // A stale pending-handoff continuation must not retain a lock in the
  // replacement privacy session. Its finally handler only removes its own
  // object, so clearing here cannot release a newer action's lock.
  pendingRelayRecoveryLocks.clear();
  resetTransferFlowForPrivacySession();
}

// A value-moving flow may make more than one wallet or local-helper request
// while it creates a self-merge or exact-match note. Reserve the UI flow from
// the first click, rather than only after the confirmation modal or
// preparation begins.
function beginPrivacyValueAction(action, session = beginPrivacySessionOperation()) {
  assertPrivacySessionCurrent(session);
  if (privacyValueActionLock) return null;
  const lock = Object.freeze({ action, generation: session.generation });
  privacyValueActionLock = lock;
  return lock;
}

function endPrivacyValueAction(lock) {
  if (privacyValueActionLock === lock) {
    privacyValueActionLock = null;
  }
}

function isPrivacyValueActionInFlight() {
  return Boolean(privacyValueActionLock);
}

// Copying a payload and submitting it to the optional local relayer are the
// same durable handoff boundary. Keep them single-flight from the initial
// click, so one payload cannot be copied while another handler begins relay
// preflight or submission for it.
function beginRelayHandoffBoundary(
  kind,
  session,
  payloadVersion,
) {
  assertPrivacySessionCurrent(session);
  if (relayHandoffBoundaryLock) return null;
  const lock = Object.freeze({
    kind,
    generation: session.generation,
    payloadVersion,
  });
  relayHandoffBoundaryLock = lock;
  return lock;
}

function endRelayHandoffBoundary(lock) {
  if (relayHandoffBoundaryLock === lock) {
    relayHandoffBoundaryLock = null;
  }
}

function isRelayHandoffBoundaryInFlight() {
  return Boolean(relayHandoffBoundaryLock);
}

// Pending relay recovery can resolve ManualReview or replace the active
// payload. Lock one immutable pending entry from the initial click, before
// its reservation/chain preflight begins, so repeated Use/Refresh clicks
// cannot race each other into competing recovery transitions.
function beginPendingRelayRecovery(id, session = beginPrivacySessionOperation()) {
  assertPrivacySessionCurrent(session);
  const key = String(id || "");
  if (!key || pendingRelayRecoveryLocks.has(key)) return null;
  const lock = Object.freeze({ key, generation: session.generation });
  pendingRelayRecoveryLocks.set(key, lock);
  return lock;
}

function endPendingRelayRecovery(lock) {
  if (!lock) return;
  if (pendingRelayRecoveryLocks.get(lock.key) === lock) {
    pendingRelayRecoveryLocks.delete(lock.key);
  }
}

function invalidateFailedPrivacySetup() {
  // A root signature without working encrypted persistence must not be kept as
  // a usable privacy session. In particular, Deposit creates a new note that
  // this browser would otherwise be unable to retain or recover.
  invalidatePrivacySessionOperations();
  stopPreparedRelayReservationHeartbeat();
  advanceRelayWithdrawPayloadGeneration();
  Object.assign(state.keplr, {
    shieldedAddress: "",
    disclosurePubKeyHex: "",
    rootSignatureBase64: "",
    rootSignatureHash: "",
    relayWithdrawPayloadHash: "",
    relayWithdrawPayload: null,
    relayWithdrawPreparedData: null,
    relayWithdrawPayloadText: "",
    relayWithdrawPayloadAmount: "",
    relayWithdrawPayloadRecipient: "",
    relayWithdrawPayloadChainId: "",
    relayWithdrawPayloadExpiresAt: "",
    relayWithdrawPayloadSubmitted: false,
    relayWithdrawPayloadHandedOff: false,
    relayWithdrawPayloadVersion: relayWithdrawPayloadGeneration,
    relayWithdrawPendingPayloads: [],
    relayWithdrawReservation: null,
    notesSummary: "",
    notes: [],
    noteReservationByNullifier: {},
    reservationRecordByID: {},
    manualReviewReservations: [],
    notesScanned: false,
    scanError: "",
    noteScanCursor: defaultNoteScanCursor(),
    privacySetupFailed: true,
  });
  walletNoteStore = null;
  walletNoteStoreKey = "";
  relayMetadataStore = null;
  relayMetadataStoreKey = "";
  batchTransferArtifactStore = null;
  batchTransferArtifactStoreKey = "";
  reservationManager = null;
  reservationManagerKey = "";
  reservationStore = null;
  reservationStoreKey = "";
  reservationWorkerID = "";
}

function rememberPreparedPrivacySession(data, session) {
  if (data && typeof data === "object") {
    preparedPrivacySessionContexts.set(data, session);
  }
  return data;
}

function preparedPrivacySession(data) {
  return preparedPrivacySessionContexts.get(data) || null;
}

async function replanInvalidatedPreparedReservation(data, session, error) {
  const reservation = preparedReservation(data);
  const ids = reservationIDs(reservation);
  const manager = session?.reservationManager;
  if (!ids.length || !manager || typeof manager.markReplanRequired !== "function") {
    return;
  }
  try {
    await manager.markReplanRequired(ids, {
      leaseToken: reservationLeaseToken(reservation),
      // Reservation records are durable recovery data. Keep their error field
      // to a stable internal code rather than a user-facing message that may
      // later include wallet, prover, or transport details.
      error: privacyReservationErrorCode(
        error,
        "privacy_session_invalidated_before_external_boundary",
      ),
      metadata: {
        reconcile_reason: "privacy_session_changed_before_external_boundary",
        no_broadcast_attempt: true,
        proof_discarded: true,
      },
    });
  } catch (cleanupError) {
    error.reservationCleanupError = cleanupError;
  }
}

async function finishPrivacyPreparation(data, session) {
  try {
    assertPrivacySessionCurrent(session);
  } catch (error) {
    await replanInvalidatedPreparedReservation(data, session, error);
    throw error;
  }
  return rememberPreparedPrivacySession(data, session);
}

// Keep the completed prepared value long enough for finishPrivacyPreparation
// to replan its old-session reservation when the session changes at the final
// await. The normal guard alone would discard that value before cleanup.
async function preparePrivacyWithSessionCleanup(session, task) {
  assertPrivacySessionCurrent(session);
  const controller = new AbortController();
  activePrivacyPreparationControllers.add(controller);
  let data;
  try {
    data = await task(controller.signal);
  } catch (error) {
    assertPrivacySessionCurrent(session);
    throw error;
  } finally {
    activePrivacyPreparationControllers.delete(controller);
  }
  return finishPrivacyPreparation(data, session);
}

async function assertTypedPrivacyScanBeforePreparation(session) {
  assertPrivacySessionCurrent(session);
  // A prepare performs its own scan. Run the WebApp's strict typed-scan
  // preflight first so ClairveilJS's generic legacy ScanEvents fallback is
  // rejected before any proof or reservation preparation can start.
  const scan = await withPrivacySessionGuard(
    session,
    () => clairveilBrowserClient().scanWalletNotes(
      privacyRequest({
        ...noteScanRequestOptions({ requireComplete: true }),
        includeFoundNotes: true,
      }),
    ),
  );
  assertPrivacySessionCurrent(session);
  const cursor = scan?.scanCursor || scan?.scan_cursor || {};
  if (String(cursor.source || "") !== "privacy_scan") {
    throw new Error(
      "The configured node did not return privacy-scan-v2. No wallet request was sent; refresh Notes from the unified endpoint before retrying.",
    );
  }
  if (
    cursor.has_more === true ||
    cursor.hasMore === true ||
    cursor.completed !== true
  ) {
    throw new Error(
      "Privacy note sync did not complete during preparation. Refresh Notes before preparing a spend.",
    );
  }
  // `prepareTransfer` performs its own fresh scan. Replace the visual cache
  // with this equally complete preflight result first so the notes labelled
  // Spendable in the UI cannot be an older incremental-cache view than the
  // planner's input inventory.
  const noteStore = currentWalletNoteStore({ optional: false });
  const cached = await withPrivacySessionGuard(
    session,
    () => noteStore.replaceScanResult(scan, {
      owner: state.keplr.shieldedAddress,
    }),
  );
  assertPrivacySessionCurrent(session);
  if (!applyPersistedWalletNoteState(cached)) {
    throw new Error(
      "The authoritative privacy scan could not be saved locally. No wallet request was sent; refresh Notes before retrying.",
    );
  }
  await reconcileReservedNotesFromScan({ session });
  assertPrivacySessionCurrent(session);
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return scan;
}

function profileSessionFingerprint(
  profile = activeChainProfile(),
  { config = state.config } = {},
) {
  if (!profile) return "";
  return JSON.stringify({
    id: profile.id || "",
    transport: profile.transport || "",
    wallet: profile.wallet || "",
    chainId: profile.chainId || "",
    rpc: browserRpcUrl(profile),
    rest: browserRestUrl(profile),
    restEndpoints: browserRestEndpoints(profile),
    proverUrl: browserProverUrl(profile, {
      serverFeatures: config?.serverFeatures,
      serverBacked: config?.serverBacked,
    }),
    depositProofUrl: browserDepositProofUrl(profile),
    accountPrefix: profile.accountPrefix || "",
    shieldedPrefix: profile.shieldedPrefix || "",
    denom: profile.denom || "",
    evmRpc: evmRpcUrlForWallet(profile),
    evmChainId: profile.evmChainId || "",
    evmPrivacyPrecompileAddress: profile.evmPrivacyPrecompileAddress || "",
    // These limits affect the transaction presented to MetaMask. Treat a
    // change as a profile boundary so an operation prepared under an earlier
    // deployment value cannot advance into a later wallet request.
    evmGasLimit: profile.evmGasLimit || "",
    evmSendGasLimit: profile.evmSendGasLimit || "",
    // Keplr's derivation, address, currency, and fee definitions are part of
    // the active wallet profile. A refreshed configuration that changes any
    // of them must invalidate an in-flight connection and encrypted-session
    // scope just like an RPC or prefix change.
    keplrCoinType: profile.keplrCoinType ?? null,
    gasPriceStep: canonicalConfigValue(profile.gasPriceStep || null),
    keplrChainInfo: canonicalConfigValue(profile.keplrChainInfo || null),
    keplrChainId: profile.keplrChainInfo?.chainId || "",
    keplrRpc: profile.keplrChainInfo
      ? browserEndpointUrl(profile.keplrChainInfo.rpc, { trim: true })
      : "",
    keplrRest: profile.keplrChainInfo
      ? browserEndpointUrl(profile.keplrChainInfo.rest, { trim: true })
      : "",
  });
}

function shieldedAddressBookProfileFingerprint() {
  return profileSessionFingerprint();
}

function isShieldedAddressBookCurrent() {
  return (
    state.addressBook.profileFingerprint ===
    shieldedAddressBookProfileFingerprint()
  );
}

function resetShieldedAddressBook() {
  shieldedAddressBookGeneration += 1;
  shieldedAddressBookPromise = null;
  state.addressBook = {
    ...defaultShieldedAddressBookState(),
    profileFingerprint: shieldedAddressBookProfileFingerprint(),
  };
}

function ensureShieldedAddressBookScope() {
  if (!isShieldedAddressBookCurrent()) {
    resetShieldedAddressBook();
  }
  return {
    addressBook: state.addressBook,
    generation: shieldedAddressBookGeneration,
  };
}

function isShieldedAddressBookScopeCurrent(scope) {
  return Boolean(
    scope &&
      scope.generation === shieldedAddressBookGeneration &&
      state.addressBook === scope.addressBook &&
      isShieldedAddressBookCurrent(),
  );
}

function recordShieldedAddressBookFailure(scope, error) {
  // A failed request can settle after a profile switch just as a successful
  // one can. Do not turn that former profile's lookup failure into an error
  // displayed beside recipient suggestions for the replacement profile.
  if (!isShieldedAddressBookScopeCurrent(scope)) return;
  scope.addressBook.loadingShielded = false;
  scope.addressBook.shieldedError = error?.message || "Unable to load shielded addresses";
  shieldedAddressBookPromise = null;
  renderVisibleAddressSuggestions();
}

function localAccountViewIdentity(account = selectedLocalAccount()) {
  return JSON.stringify({
    name: account?.name || "",
    transparentAddress: account?.transparentAddress || "",
    evmAddress: account?.evmAddress || "",
  });
}

function beginLocalAccountView() {
  return {
    generation: localAccountViewGeneration,
    profileFingerprint: profileSessionFingerprint(),
    accountIdentity: localAccountViewIdentity(),
  };
}

function isLocalAccountViewCurrent(view) {
  return Boolean(
    view &&
      view.generation === localAccountViewGeneration &&
      view.profileFingerprint === profileSessionFingerprint() &&
      view.accountIdentity === localAccountViewIdentity(),
  );
}

function assertLocalAccountViewCurrent(view) {
  if (isLocalAccountViewCurrent(view)) return;
  throw privacySessionInvalidatedError();
}

async function withLocalAccountViewGuard(view, task) {
  assertLocalAccountViewCurrent(view);
  try {
    const result = await task();
    assertLocalAccountViewCurrent(view);
    return result;
  } catch (error) {
    assertLocalAccountViewCurrent(view);
    throw error;
  }
}

function invalidateLocalAccountView() {
  localAccountViewGeneration += 1;
}

function clearLocalAccountDetails() {
  els.shieldedAddress.textContent = "-";
  els.balanceValue.textContent = "-";
  els.spendableTotal.textContent = zeroCoinText();
  els.notesList.innerHTML = "";
}

function relayerViewIdentity(relayer = localRelayerAccount()) {
  return JSON.stringify({
    name: relayer?.name || "",
    transparentAddress: relayer?.transparentAddress || "",
  });
}

function beginRelayerView() {
  relayerViewGeneration += 1;
  return {
    generation: relayerViewGeneration,
    profileFingerprint: profileSessionFingerprint(),
    relayerIdentity: relayerViewIdentity(),
  };
}

function isRelayerViewCurrent(view) {
  return Boolean(
    view &&
      view.generation === relayerViewGeneration &&
      view.profileFingerprint === profileSessionFingerprint() &&
      view.relayerIdentity === relayerViewIdentity(),
  );
}

function assertRelayerViewCurrent(view) {
  if (isRelayerViewCurrent(view)) return;
  throw privacySessionInvalidatedError();
}

async function withRelayerViewGuard(view, task) {
  assertRelayerViewCurrent(view);
  try {
    const result = await task();
    assertRelayerViewCurrent(view);
    return result;
  } catch (error) {
    assertRelayerViewCurrent(view);
    throw error;
  }
}

function invalidateRelayerView() {
  relayerViewGeneration += 1;
  state.relayer = {
    balance: "",
    error: "",
  };
}

function beginHealthView() {
  healthViewGeneration += 1;
  return healthViewGeneration;
}

function isHealthViewCurrent(view) {
  return view === healthViewGeneration;
}

function assertHealthViewCurrent(view) {
  if (isHealthViewCurrent(view)) return;
  throw privacySessionInvalidatedError();
}

function assertOptionalHealthViewCurrent(view) {
  if (view !== null) assertHealthViewCurrent(view);
}

async function withHealthViewGuard(view, task) {
  assertHealthViewCurrent(view);
  try {
    const result = await task();
    assertHealthViewCurrent(view);
    return result;
  } catch (error) {
    assertHealthViewCurrent(view);
    throw error;
  }
}

function invalidateHealthView() {
  healthViewGeneration += 1;
}

function profilePersistenceScope(profile = activeChainProfile()) {
  const resolved = profile || configuredChainProfile();
  // Use exactly the same validated-profile boundary that invalidates a live
  // privacy session. In particular, a replacement REST/RPC/prover endpoint
  // must never reopen the former profile's encrypted note, reservation, or
  // relay-recovery namespace merely because its chain ID and prefixes match.
  return encodeURIComponent(profileSessionFingerprint(resolved));
}

function clairveilBrowserClient(
  profile = activeChainProfile(),
  { config = state.config } = {},
) {
  const resolved = profile || configuredChainProfile();
  const key = JSON.stringify({
    id: resolved?.id || "",
    transport: resolved?.transport || config?.transport || "",
    rpc: browserRpcUrl(resolved),
    rest: browserRestUrl(resolved),
    restEndpoints: browserRestEndpoints(resolved),
    chainId: resolved?.chainId || state.config?.chainId || "",
    accountPrefix: resolved?.accountPrefix || state.config?.accountPrefix || "",
    shieldedPrefix:
      resolved?.shieldedPrefix || state.config?.shieldedPrefix || "",
    denom: resolved?.denom || state.config?.denom || "",
    proverUrl: browserProverUrl(resolved, {
      serverFeatures: config?.serverFeatures,
      serverBacked: config?.serverBacked,
    }),
    evmRpc: evmRpcUrlForWallet(resolved),
    evmChainId: resolved?.evmChainId || state.config?.evmChainId || "",
    evmPrivacyPrecompileAddress:
      resolved?.evmPrivacyPrecompileAddress ||
      state.config?.evmPrivacyPrecompileAddress ||
      "",
    evmGasLimit: resolved?.evmGasLimit || state.config?.evmGasLimit || "",
    evmSendGasLimit:
      resolved?.evmSendGasLimit || state.config?.evmSendGasLimit || "",
    enableExperimentalBatchTransfer: batchTransferFeatureEnabled(),
  });
  if (!browserClient || browserClientKey !== key) {
    browserClient = createClairveilBrowserDappClient({
      profile: {
        ...resolved,
        rpc: browserRpcUrl(resolved),
        rest: browserRestUrl(resolved),
        restEndpoints: browserRestEndpoints(resolved),
        chainId: resolved?.chainId || state.config?.chainId,
        accountPrefix: resolved?.accountPrefix || state.config?.accountPrefix,
        shieldedPrefix:
          resolved?.shieldedPrefix || state.config?.shieldedPrefix,
        denom: resolved?.denom || state.config?.denom,
        proverUrl: browserProverUrl(resolved, {
          serverFeatures: config?.serverFeatures,
          serverBacked: config?.serverBacked,
        }),
        evmRpc: evmRpcUrlForWallet(resolved),
        evmChainId: resolved?.evmChainId || state.config?.evmChainId,
        evmPrivacyPrecompileAddress:
          resolved?.evmPrivacyPrecompileAddress ||
          state.config?.evmPrivacyPrecompileAddress,
        evmGasLimit: resolved?.evmGasLimit || state.config?.evmGasLimit,
        evmSendGasLimit:
          resolved?.evmSendGasLimit || state.config?.evmSendGasLimit,
      },
      // Keep privacy-sensitive nullifier queries on the selected endpoint even
      // if a future ClairveilJS default changes.
      queryTimeoutMs: 30_000,
      nullifierFailover: false,
      enableExperimentalBatchTransfer: batchTransferFeatureEnabled(),
    });
    browserClientKey = key;
  }
  return browserClient;
}

const chainSafetyRefreshIntervalMs = 30_000;

function activeChainSafetyKey(profile = activeChainProfile()) {
  if (!profile) return "";
  return JSON.stringify({
    id: profile.id || "",
    transport: profile.transport || "",
    chainId: profile.chainId || "",
    rpc: browserRpcUrl(profile),
    rest: browserRestUrl(profile),
    restEndpoints: browserRestEndpoints(profile),
    evmRpc: evmRpcUrlForWallet(profile),
    evmChainId: profile.evmChainId || "",
    denom: profile.denom || "",
    depositProofUrl: browserDepositProofUrl(profile),
  });
}

function clearChainSafety() {
  chainSafetyRefreshGeneration += 1;
  if (chainSafetyExpiryTimer !== null) {
    globalThis.clearTimeout(chainSafetyExpiryTimer);
    chainSafetyExpiryTimer = null;
  }
  state.chainSafety = {
    key: activeChainSafetyKey(),
    status: "unknown",
    error: "",
    checkedAt: 0,
  };
}

function isChainSafetyRefreshCurrent({ generation, key, session }) {
  return (
    isPrivacySessionCurrent(session) &&
    generation === chainSafetyRefreshGeneration &&
    key === activeChainSafetyKey()
  );
}

function assertChainSafetyRefreshCurrent(refresh) {
  if (isChainSafetyRefreshCurrent(refresh)) return;
  throw privacySessionInvalidatedError();
}

function scheduleChainSafetyExpiry() {
  if (chainSafetyExpiryTimer !== null) {
    globalThis.clearTimeout(chainSafetyExpiryTimer);
    chainSafetyExpiryTimer = null;
  }
  if (state.chainSafety.status !== "ready" || !Number.isFinite(state.chainSafety.checkedAt)) {
    return;
  }
  const delayMs = Math.max(
    0,
    state.chainSafety.checkedAt + chainSafetyRefreshIntervalMs - Date.now(),
  );
  // The expiry callback is asynchronous work just like a scan or recovery
  // continuation: capture its scope so an old timer cannot validate or render
  // a replacement wallet/profile session.
  const refresh = {
    generation: chainSafetyRefreshGeneration,
    key: state.chainSafety.key,
    session: beginPrivacySessionOperation(),
  };
  chainSafetyExpiryTimer = globalThis.setTimeout(() => {
    chainSafetyExpiryTimer = null;
    if (!isChainSafetyRefreshCurrent(refresh) || isChainSafetyReady()) return;
    // Do not require a manual note scan merely because the short-lived
    // preflight lease expired. A failed refresh still transitions to the
    // existing fail-closed state inside refreshChainSafety.
    void refreshChainSafety({ force: true, session: refresh.session }).catch(() => {});
  }, delayMs + 1);
}

function isChainSafetyReady() {
  return state.chainSafety.status === "ready" &&
    state.chainSafety.key === activeChainSafetyKey() &&
    Number.isFinite(state.chainSafety.checkedAt) &&
    Date.now() - state.chainSafety.checkedAt < chainSafetyRefreshIntervalMs;
}

// A freshly bootstrapped chain has a structurally valid but uninitialized
// privacy tree. Its configuration is safe to use for the first deposit, but
// no value-moving operation can consume notes until that deposit initializes
// the tree and its encrypted output has been scanned back into this wallet.
function isSpendChainReady() {
  return isChainSafetyReady() && state.chainSafety.tree?.initialized === true;
}

function hasCompletedPrivacyNoteScan() {
  const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
  return Boolean(
    state.keplr.notesScanned &&
      !state.keplr.scanError &&
      cursor.completed &&
      !cursor.hasMore,
  );
}

function assertSpendableNotesSyncReady() {
  if (!isSpendChainReady()) {
    throw new Error(
      "The privacy tree has not been initialized yet. Deposit funds, wait for the transaction to be included, then Scan Notes before preparing a spend.",
    );
  }
  if (hasCompletedPrivacyNoteScan()) return;
  throw new Error(
    state.keplr.scanError ||
      "Privacy note sync is incomplete. Finish scanning notes successfully before preparing a spend.",
  );
}

function privacySyncErrorMessage(error) {
  if (error?.chainSafetyFailure) {
    return "Privacy note sync is unavailable until current chain configuration is verified. Retry after the selected network is available.";
  }
  const message = privacySyncErrorText(error);
  if (isRecoverableEncryptedNoteCacheError(error)) {
    return "The encrypted local note cache cannot be read. Reset & rescan Notes to rebuild it from the chain.";
  }
  if (/web locks api/i.test(message)) {
    return "This browser cannot provide the lock required for encrypted note storage. Open the DApp in a current Chrome, Edge, or Firefox window, then reset and rescan Notes.";
  }
  if (/web crypto/i.test(message)) {
    return "This browser cannot provide Web Crypto for encrypted note storage. Open the DApp in a current browser, then reset and rescan Notes.";
  }
  if (/indexeddb|browser[- ]storage|quota|storage is unavailable|invalidstateerror|dataerror/i.test(message)) {
    return "Encrypted local note storage is unavailable. Enable browser storage for this DApp, then reset and rescan Notes.";
  }
  if (/failed to fetch|load failed|networkerror|network request failed/i.test(message)) {
    return "Browser cannot reach the selected chain REST endpoint. Check the local node and browser CORS, then retry Scan.";
  }
  if (/privacy-scan-v2|unified privacy scan|typed privacy scan cursor/i.test(message)) {
    return "The selected chain did not provide a valid unified privacy scan response. Reset & rescan after the node is available.";
  }
  if (/signer address\/pubkey mismatch|signaturebase64|pubkeyhex/i.test(message)) {
    return "Wallet privacy identity is no longer valid. Reconnect the wallet, run Setup Clairveil, then reset and rescan Notes.";
  }
  switch (error?.privacyScanStage) {
    case "typed-query":
      return "The typed privacy-scan response could not be accepted. No scanned notes were used; retry Scan after the node catches up.";
    case "encrypted-cache":
      return "Scanned notes could not be saved in this browser's encrypted cache. Reset & rescan Notes after checking browser storage permissions.";
    case "cursor-validation":
      return "The typed privacy-scan cursor was not safe to resume. Reset & rescan Notes from the beginning.";
    case "scan-completion":
      return "Privacy note sync stopped before the typed scan was complete. Retry Scan to continue from the saved cursor.";
    default:
      return "Privacy note sync failed. Retry Scan, or reset and rescan the encrypted local cache.";
  }
}

function privacySyncErrorText(error) {
  // ClairveilJS may wrap a transport or persistence error in `cause`. Its
  // first-level message is often deliberately terse, but the safe category
  // needed for recovery (for example IndexedDB or Web Locks) is retained in
  // the cause. Inspect only a short chain and never render it directly.
  const parts = [];
  let current = error;
  for (let depth = 0; current && depth < 3; depth += 1) {
    const message = String(current?.message || "").trim();
    if (message) parts.push(message);
    current = current?.cause;
  }
  return parts.join(" | ");
}

function privacyScanStageFailure(error, stage) {
  if (error?.privacySessionInvalidated) return error;
  const wrapped = new Error("Privacy note scan did not complete", { cause: error });
  wrapped.privacyScanStage = stage;
  return wrapped;
}

function privacyPostScanErrorMessage(error) {
  if (error?.chainSafetyFailure) return privacySyncErrorMessage(error);
  const message = String(error?.message || "");
  if (/indexeddb|browser-storage|quota|storage is unavailable/i.test(message)) {
    return "Notes were recovered, but their encrypted status cache could not be updated. Reset & rescan Notes before spending.";
  }
  return "Notes were recovered, but reservation or nullifier reconciliation did not complete. They are shown for recovery only; retry Scan before spending.";
}

function privacySetupErrorMessage(error) {
  if (error?.chainSafetyFailure) {
    return "Clairveil setup is unavailable until current chain configuration is verified. Retry after the selected network is available.";
  }
  return "Clairveil setup failed. Reconnect the wallet and retry.";
}

function privacyDisclosureErrorMessage(error) {
  if (error?.chainSafetyFailure) {
    return "Disclosure is unavailable until current chain configuration is verified. Retry after the selected network is available.";
  }
  return "Disclosure could not be decoded. Verify the selected event and retry.";
}

function privacyRecoveryErrorMessage(
  error,
  fallback = "Privacy recovery could not complete. Refresh Notes and retry.",
) {
  if (error?.chainSafetyFailure) {
    return "Privacy recovery is unavailable until current chain configuration is verified. Refresh Notes after the selected network is available.";
  }
  return fallback;
}

function privacyRelayHandoffErrorMessage(error) {
  if (error?.relayHandoffRecorded) {
    return "Relay handoff was recorded, but the payload could not be copied. Copy again to use the existing payload.";
  }
  return privacyRecoveryErrorMessage(
    error,
    "Relay payload handoff could not complete. Refresh Notes to verify its reservation state before retrying.",
  );
}

function invalidatePrivacyScanState(error) {
  state.keplr.notes = [];
  state.keplr.notesSummary = "";
  state.keplr.noteReservationByNullifier = {};
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  // scanWalletNotes receives the root-signature authority. Upstream failures
  // may reflect request material, so keep only a stable retry/reset action in
  // browser state and rendered UI.
  state.keplr.scanError = privacySyncErrorMessage(error);
}

// `make dapp-local` can start a fresh local ledger with the same chain ID.
// A typed scan cache is scoped to that ID and wallet, so it cannot otherwise
// distinguish the old ledger from the empty replacement. Never let those
// cached notes become a plausible balance on an uninitialized privacy tree.
async function discardStaleNotesForUninitializedPrivacyTree(
  { session = beginPrivacySessionOperation() } = {},
) {
  if (state.chainSafety.tree?.initialized !== false) return false;
  // An empty completed scan is valid on a newly bootstrapped chain. Only a
  // recovered note is evidence that this browser is carrying inventory from
  // the prior local ledger.
  const hasCachedNotes = state.keplr.notes.length > 0;
  if (!hasCachedNotes) return false;

  const resetMessage =
    "The current chain has an uninitialized privacy tree. Cached notes from an earlier local chain were discarded. Deposit, wait for inclusion, then Scan Notes before spending.";
  try {
    const noteStore = currentWalletNoteStore({ optional: true });
    if (noteStore) {
      await withPrivacySessionGuard(session, () => noteStore.clear());
      assertPrivacySessionCurrent(session);
    }
    // A checkpoint from the old ledger cannot be submitted to this one. Drop
    // it together with its input cache, rather than making a fresh chain look
    // as if it has an unresolved batch transaction.
    const batchArtifactStore = currentBatchTransferArtifactStore();
    if (batchArtifactStore) {
      await withPrivacySessionGuard(session, () => batchArtifactStore.clear());
      assertPrivacySessionCurrent(session);
    }
    const relayMetadataStore = currentRelayWithdrawMetadataStore();
    if (relayMetadataStore) {
      await withPrivacySessionGuard(session, () => relayMetadataStore.clear());
      assertPrivacySessionCurrent(session);
    }
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    // Do not keep the old notes in memory when durable-cache cleanup fails.
    // The user can use Reset & rescan to retry the persistent cleanup.
  }

  state.keplr.notes = [];
  state.keplr.notesSummary = "";
  state.keplr.noteReservationByNullifier = {};
  state.keplr.reservationRecordByID = {};
  state.keplr.manualReviewReservations = [];
  state.keplr.relayWithdrawPayload = null;
  state.keplr.relayWithdrawPreparedData = null;
  state.keplr.relayWithdrawPayloadText = "";
  state.keplr.relayWithdrawPayloadAmount = "";
  state.keplr.relayWithdrawPayloadRecipient = "";
  state.keplr.relayWithdrawPayloadChainId = "";
  state.keplr.relayWithdrawPayloadExpiresAt = "";
  state.keplr.relayWithdrawPayloadSubmitted = false;
  state.keplr.relayWithdrawPayloadHandedOff = false;
  state.keplr.relayWithdrawPendingPayloads = [];
  state.keplr.relayWithdrawReservation = null;
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.keplr.scanError = resetMessage;
  resetBatchTransferDraft();
  return true;
}

function isRecoverableEncryptedNoteCacheError(error) {
  return /Encrypted Clairveil browser-storage (record is invalid|record cannot be authenticated|state is invalid|state cannot be decoded)/.test(
    String(error?.message || ""),
  );
}

function chainSafetyFailure(error) {
  const message = error?.message || String(error || "unknown chain safety error");
  const wrapped = new Error(
    `Privacy flow is blocked until current chain configuration is verified: ${message}`,
  );
  wrapped.chainSafetyFailure = true;
  return wrapped;
}

async function refreshChainSafety(
  { force = false, session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const profile = activeChainProfile();
  const key = activeChainSafetyKey(profile);
  const refresh = {
    generation: ++chainSafetyRefreshGeneration,
    key,
    session,
  };
  if (!profile || !key) throw chainSafetyFailure(new Error("active chain profile is unavailable"));
  if (
    !force &&
    state.chainSafety.status === "ready" &&
    state.chainSafety.key === key &&
    Date.now() - state.chainSafety.checkedAt < chainSafetyRefreshIntervalMs
  ) {
    return state.chainSafety;
  }
  state.chainSafety = { key, status: "checking", error: "", checkedAt: 0 };
  renderKeplr();
  try {
    const client = clairveilBrowserClient(profile);
    const [health, protocolConfig, reserve] = await withPrivacySessionGuard(
      session,
      () => Promise.all([
        // An empty local chain is a valid bootstrap state. Keep all network,
        // protocol, reserve, and tree-shape checks, while allowing Deposit to
        // initialize its first privacy tree leaf. Spend flows separately
        // require isSpendChainReady().
        client.health({ allowUninitializedTree: true }),
        client.assertTransferProtocolConfig(profile.denom),
        client.queryReserve(profile.denom),
      ]),
    );
    assertChainSafetyRefreshCurrent(refresh);
    if (!health?.status || !health?.tree || (health.errors || []).length) {
      throw new Error("chain status, tree, and protocol queries must all succeed");
    }
    if (
      profile.transport === "cosmos" &&
      health.status?.node_info?.network !== profile.chainId
    ) {
      throw new Error("connected Cosmos network does not match the selected profile");
    }
    if (profile.transport === "evm") {
      const actualChainId = await withPrivacySessionGuard(
        session,
        () => client.evmJsonRpc("eth_chainId"),
      );
      assertChainSafetyRefreshCurrent(refresh);
      if (String(actualChainId || "").toLowerCase() !== expectedEvmChainIdHex().toLowerCase()) {
        throw new Error("connected EVM RPC network does not match the selected profile");
      }
    }
    state.chainSafety = {
      key,
      status: "ready",
      error: "",
      checkedAt: Date.now(),
      protocolConfig,
      reserve,
      tree: health.tree,
    };
    await discardStaleNotesForUninitializedPrivacyTree({ session });
    assertChainSafetyRefreshCurrent(refresh);
    scheduleChainSafetyExpiry();
    renderKeplr();
    return state.chainSafety;
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    assertChainSafetyRefreshCurrent(refresh);
    state.chainSafety = {
      key,
      status: "failed",
      error: error?.message || String(error),
      checkedAt: 0,
    };
    renderKeplr();
    throw chainSafetyFailure(error);
  }
}

async function assertChainSafetyBeforePrivacyFlow(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (activeWalletKind() === "metamask") {
    if (state.activeWallet !== "metamask" || !state.wallet.account) {
      throw new Error("Connect MetaMask before preparing a privacy transaction");
    }
    await ensureMetaMaskChain({ session });
    assertPrivacySessionCurrent(session);
  }
  return refreshChainSafety({ force: true, session });
}

function injectedEthereumProviders() {
  const provider = window.ethereum;
  if (!provider) return [];
  const providers = Array.isArray(provider.providers) ? provider.providers : [];
  return [...new Set([...providers, provider])].filter(
    (candidate) => candidate?.request,
  );
}

function metaMaskProvider() {
  const providers = injectedEthereumProviders();
  return (
    providers.find((provider) => provider.isMetaMask) ||
    providers.find(
      (provider) =>
        provider.isRabby || provider.isBraveWallet || provider.isCoinbaseWallet,
    ) ||
    providers[0] ||
    null
  );
}

function unsupportedEvmMethodError(error) {
  return (
    error?.code === -32601 ||
    /method .*not supported|not supported|unsupported method|does not support/i.test(
      error?.message || "",
    )
  );
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
      throw new Error(
        `${method} is not supported by the injected wallet provider. Open this DApp in a browser with MetaMask or another EVM wallet selected.`,
      );
    }
    throw error;
  }
}

async function ensureMetaMaskChain(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!metaMaskProvider()) {
    throw new Error("MetaMask not found");
  }
  const expected = expectedEvmChainIdHex();
  if (!expected) return;

  const current = await withPrivacySessionGuard(
    session,
    () => requestMetaMask({ method: "eth_chainId" }),
  );
  assertPrivacySessionCurrent(session);
  if (String(current).toLowerCase() === expected.toLowerCase()) {
    state.wallet.chainId = current;
    return;
  }

  try {
    await withPrivacySessionGuard(
      session,
      () => requestMetaMask({
        method: "wallet_switchEthereumChain",
        params: [{ chainId: expected }],
      }),
    );
    assertPrivacySessionCurrent(session);
  } catch (error) {
    assertPrivacySessionCurrent(session);
    const unknownChain =
      error?.code === 4902 ||
      /unknown|unrecognized|not added/i.test(error?.message || "");
    if (!unknownChain) {
      throw error;
    }
    await withPrivacySessionGuard(
      session,
      () => requestMetaMask({
        method: "wallet_addEthereumChain",
        params: [
          {
            chainId: expected,
            chainName:
              activeChainProfile()?.evmChainName ||
              state.config?.evmChainName ||
              "EVM Localnet",
            nativeCurrency: {
              name: displayDenom(),
              symbol: displayDenom(),
              decimals: coinDecimals(),
            },
            rpcUrls: [evmRpcUrlForWallet()],
          },
        ],
      }),
    );
    assertPrivacySessionCurrent(session);
  }

  const updated = await withPrivacySessionGuard(
    session,
    () => requestMetaMask({ method: "eth_chainId" }),
  );
  assertPrivacySessionCurrent(session);
  state.wallet.chainId = updated;
  if (String(updated).toLowerCase() !== expected.toLowerCase()) {
    throw new Error(`MetaMask chain must be ${expected}, current ${updated}`);
  }
  renderWallet();
}

function coinDecimals() {
  return Number(
    activeChainProfile()?.coinDecimals ?? state.config?.coinDecimals ?? 18,
  );
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
  keplrDisclosurePubKey: $("#keplrDisclosurePubKey"),
  copyKeplrDisclosurePubKey: $("#copyKeplrDisclosurePubKey"),
  faucetHelpText: $("#faucetHelpText"),
  faucetRow: $(".faucet-row"),
  keplrFaucetAmount: $("#keplrFaucetAmount"),
  fundKeplr: $("#fundKeplr"),
  setupKeplrPrivacy: $("#setupKeplrPrivacy"),
  refreshWalletBalance: $("#refreshKeplrBalance"),
  refreshClairBalance: $("#refreshClairBalance"),
  scanKeplrNotes: $("#scanKeplrNotes"),
  resetKeplrNotes: $("#resetKeplrNotes"),
  keplrTxState: $("#keplrTxState"),
  keplrSendAmount: $("#keplrSendAmount"),
  keplrSendRecipient: $("#keplrSendRecipient"),
  keplrSendRecipientSuggestions: $("#keplrSendRecipientSuggestions"),
  sendFromKeplr: $("#sendFromKeplr"),
  keplrDepositAmount: $("#keplrDepositAmount"),
  depositFromKeplr: $("#depositFromKeplr"),
  keplrSendHash: $("#keplrSendHash"),
  keplrDepositHash: $("#keplrDepositHash"),
  keplrDepositHeight: $("#keplrDepositHeight"),
  myClairBalance: $("#myClairBalance"),
  myKeplrSpendable: $("#myKeplrSpendable"),
  myKeplrSpendableOnly: $("#myKeplrSpendableOnly"),
  myKeplrNotesList: $("#myKeplrNotesList"),
  reservationReviewList: $("#reservationReviewList"),
  veiledTransferAmount: $("#veiledTransferAmount"),
  veiledTransferRecipient: $("#veiledTransferRecipient"),
  veiledTransferRecipientSuggestions: $("#veiledTransferRecipientSuggestions"),
  veiledDisclosureSummary: $("#veiledDisclosureSummary"),
  veiledDisclosureAdvanced: $("#veiledDisclosureAdvanced"),
  veiledDisclosureOptions: $("#veiledDisclosureOptions"),
  veiledDisclosureMode: $("#veiledDisclosureMode"),
  veiledDisclosurePubKey: $("#veiledDisclosurePubKey"),
  veiledDisclosureAmount: $("#veiledDisclosureAmount"),
  veiledDisclosureFrom: $("#veiledDisclosureFrom"),
  veiledDisclosureTo: $("#veiledDisclosureTo"),
  transferFromVeiled: $("#transferFromVeiled"),
  openBatchTransfer: $("#openBatchTransfer"),
  batchTransferSection: $("#batchTransferSection"),
  closeBatchTransfer: $("#closeBatchTransfer"),
  batchTransferRows: $("#batchTransferRows"),
  addBatchTransferRecipient: $("#addBatchTransferRecipient"),
  batchTransferTotal: $("#batchTransferTotal"),
  batchTransferChange: $("#batchTransferChange"),
  batchTransferInputs: $("#batchTransferInputs"),
  batchTransferOutputs: $("#batchTransferOutputs"),
  batchTransferCapacity: $("#batchTransferCapacity"),
  batchTransferCapacityWarning: $("#batchTransferCapacityWarning"),
  batchTransferSplitControl: $("#batchTransferSplitControl"),
  batchTransferSplit: $("#batchTransferSplit"),
  prepareBatchTransfer: $("#prepareBatchTransfer"),
  keplrBatchTransferHash: $("#keplrBatchTransferHash"),
  batchTransferState: $("#batchTransferState"),
  batchTransferConfirmationModal: $("#batchTransferConfirmationModal"),
  batchTransferConfirmationTotal: $("#batchTransferConfirmationTotal"),
  batchTransferConfirmationChange: $("#batchTransferConfirmationChange"),
  batchTransferConfirmationInputs: $("#batchTransferConfirmationInputs"),
  batchTransferConfirmationOutputs: $("#batchTransferConfirmationOutputs"),
  batchTransferConfirmationPayments: $("#batchTransferConfirmationPayments"),
  batchTransferConfirmationDisclosures: $(
    "#batchTransferConfirmationDisclosures",
  ),
  cancelBatchTransferConfirmation: $("#cancelBatchTransferConfirmation"),
  confirmBatchTransferConfirmation: $("#confirmBatchTransferConfirmation"),
  veiledWithdrawAmount: $("#veiledWithdrawAmount"),
  veiledWithdrawRecipient: $("#veiledWithdrawRecipient"),
  veiledWithdrawRecipientSuggestions: $("#veiledWithdrawRecipientSuggestions"),
  withdrawFromVeiled: $("#withdrawFromVeiled"),
  relayWithdrawAmount: $("#relayWithdrawAmount"),
  relayWithdrawRecipient: $("#relayWithdrawRecipient"),
  relayWithdrawRecipientSuggestions: $("#relayWithdrawRecipientSuggestions"),
  relayWithdrawFromVeiled: $("#relayWithdrawFromVeiled"),
  copyRelayWithdrawPayload: $("#copyRelayWithdrawPayload"),
  relayPreparedWithdraw: $("#relayPreparedWithdraw"),
  keplrTransferHash: $("#keplrTransferHash"),
  keplrWithdrawHash: $("#keplrWithdrawHash"),
  keplrWithdrawHeight: $("#keplrWithdrawHeight"),
  keplrRelayWithdrawHash: $("#keplrRelayWithdrawHash"),
  keplrRelayWithdrawRelayer: $("#keplrRelayWithdrawRelayer"),
  relayerPanel: $(".relayer-panel"),
  relayerState: $("#relayerState"),
  relayerTransparentAddress: $("#relayerTransparentAddress"),
  relayerBalance: $("#relayerBalance"),
  relayWithdrawPreparedAmount: $("#relayWithdrawPreparedAmount"),
  relayWithdrawPreparedRecipient: $("#relayWithdrawPreparedRecipient"),
  relayWithdrawPreparedChainId: $("#relayWithdrawPreparedChainId"),
  relayWithdrawPreparedExpiresAt: $("#relayWithdrawPreparedExpiresAt"),
  relayWithdrawPreparedPayloadHash: $("#relayWithdrawPreparedPayloadHash"),
  relayWithdrawManualReviewEvidence: $("#relayWithdrawManualReviewEvidence"),
  relayWithdrawPreparedPayloadJson: $("#relayWithdrawPreparedPayloadJson"),
  relayWithdrawPendingList: $("#relayWithdrawPendingList"),
  localHome: $("#localHome"),
  localHomeRow: $("#localHome")?.closest("div"),
  faucetHashRow: $("#keplrFaucetHash")?.closest("div"),
  faucetSentRow: $("#keplrFaucetSent")?.closest("div"),
  faucetRecipientRow: $("#keplrFaucetRecipient")?.closest("div"),
  blockHeight: $("#blockHeight"),
  leafCount: $("#leafCount"),
  chainId: $("#chainId"),
  restState: $("#restState"),
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
  previousEventsPage: $("#previousEventsPage"),
  nextEventsPage: $("#nextEventsPage"),
  eventsPageState: $("#eventsPageState"),
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
  eventDisclosureAssetDenom: $("#eventDisclosureAssetDenom"),
  eventDisclosureFrom: $("#eventDisclosureFrom"),
  eventDisclosureTo: $("#eventDisclosureTo"),
  eventDisclosureState: $("#eventDisclosureState"),
  eventDisclosureReports: $("#eventDisclosureReports"),
  decodeEventDisclosure: $("#decodeEventDisclosure"),
  refreshAuditorTransfers: $("#refreshAuditorTransfers"),
  previousAuditorPage: $("#previousAuditorPage"),
  nextAuditorPage: $("#nextAuditorPage"),
  auditorPageState: $("#auditorPageState"),
  auditorEventsList: $("#auditorEventsList"),
  auditorDecodeState: $("#auditorDecodeState"),
  auditorTxHash: $("#auditorTxHash"),
  auditorVerification: $("#auditorVerification"),
  auditorAmount: $("#auditorAmount"),
  auditorFrom: $("#auditorFrom"),
  auditorTo: $("#auditorTo"),
  auditorFields: $("#auditorFields"),
  auditorDigest: $("#auditorDigest"),
  auditorOutputReports: $("#auditorOutputReports"),
  auditorTestScalar: $("#auditorTestScalar"),
  decodeAuditorTransfer: $("#decodeAuditorTransfer"),
  auditorSection: $(".auditor-section"),
  noticeModal: $("#noticeModal"),
  noticeTitle: $("#noticeTitle"),
  noticeMessage: $("#noticeMessage"),
  closeNoticeModal: $("#closeNoticeModal"),
  reservationReviewModal: $("#reservationReviewModal"),
  reservationReviewState: $("#reservationReviewState"),
  reservationReviewLead: $("#reservationReviewLead"),
  reservationReviewCauses: $("#reservationReviewCauses"),
  reservationReviewWarning: $("#reservationReviewWarning"),
  reservationReviewAcknowledgement: $("#reservationReviewAcknowledgement"),
  reservationReviewAcknowledge: $("#reservationReviewAcknowledge"),
  cancelReservationReview: $("#cancelReservationReview"),
  confirmReservationReview: $("#confirmReservationReview"),
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
  transferPlannerFacts: $("#transferPlannerFacts"),
  transferPlannerRequested: $("#transferPlannerRequested"),
  transferPlannerCurrentMax: $("#transferPlannerCurrentMax"),
  transferPlannerAction: $("#transferPlannerAction"),
  transferConfirmationFacts: $("#transferConfirmationFacts"),
  transferConfirmationChainId: $("#transferConfirmationChainId"),
  transferConfirmationExpiry: $("#transferConfirmationExpiry"),
  transferConfirmationRecipient: $("#transferConfirmationRecipient"),
  transferConfirmationChange: $("#transferConfirmationChange"),
  transferConfirmationDisclosure: $("#transferConfirmationDisclosure"),
  cancelTransferFlow: $("#cancelTransferFlow"),
  confirmTransferFlow: $("#confirmTransferFlow"),
};

function shorten(value, head = 10, tail = 8) {
  if (!value || value.length <= head + tail + 3) return value || "-";
  return `${value.slice(0, head)}...${value.slice(-tail)}`;
}

function eventAttribute(event, key) {
  return (
    (event?.attributes || []).find((attribute) => attribute.key === key)
      ?.value || ""
  );
}

function prettyDisclosureField(value) {
  return String(value || "").replace(/_/g, " ");
}

function renderTransferDisclosureSummary() {
  els.veiledDisclosureSummary.textContent = isEvmTransparentMode()
    ? [
        "Audit disclosure is always encrypted for the configured auditor.",
        "The supported EVM transfer ABI does not carry sender self-view disclosure,",
        "so later sent-transfer recovery depends on this wallet's local cache.",
        "Advanced settings control only optional user disclosure.",
      ].join(" ")
    : [
        "Audit disclosure is always encrypted for the configured auditor.",
        "Sender self-view disclosure is included by default for later",
        "sent-transfer recovery. Advanced settings control only optional user disclosure.",
      ].join(" ");
}

function renderTransferDisclosureAdvanced() {
  renderTransferDisclosureSummary();
  els.veiledDisclosureOptions.hidden = !els.veiledDisclosureAdvanced.checked;
  const mode = els.veiledDisclosureMode?.value || "none";
  const noDisclosure = mode === "none";
  const isPublic = mode === "public";
  const disableTarget =
    !els.veiledDisclosureAdvanced.checked || noDisclosure || isPublic;
  els.veiledDisclosurePubKey.disabled = disableTarget;
  els.veiledDisclosurePubKey
    .closest(".field")
    .classList.toggle("muted", disableTarget);
  [
    els.veiledDisclosureAmount,
    els.veiledDisclosureFrom,
    els.veiledDisclosureTo,
  ].forEach((checkbox) => {
    checkbox.disabled = !els.veiledDisclosureAdvanced.checked || noDisclosure;
    checkbox
      .closest(".checkbox-control")
      .classList.toggle("muted", noDisclosure);
  });
}

function transferDisclosurePolicy() {
  if (!els.veiledDisclosureAdvanced.checked) {
    return {
      privacyPolicy: "all-private",
    };
  }

  const disclosureMode =
    els.veiledDisclosureMode?.value || "recipient-encrypted";
  const pubKeyHex = els.veiledDisclosurePubKey.value.trim();

  if (disclosureMode === "none") {
    return {
      privacyPolicy: "all-private",
      disclosureMode,
    };
  }

  const amount = els.veiledDisclosureAmount.checked;
  const from = els.veiledDisclosureFrom.checked;
  const to = els.veiledDisclosureTo.checked;
  if (!amount && !from && !to) {
    throw new Error(
      "Advanced disclosure에서 공개할 항목을 하나 이상 선택해줘.",
    );
  }

  const privacyPolicy = [
    amount ? "amount" : "",
    from ? "from" : "",
    to ? "to" : "",
  ]
    .filter(Boolean)
    .join("-");

  if (disclosureMode === "public") {
    return {
      privacyPolicy,
      disclosureMode,
    };
  }

  if (!/^[0-9a-fA-F]{64}$/.test(pubKeyHex)) {
    throw new Error(
      "Disclosure target은 show-disclosure-pubkey로 만든 32-byte hex 값을 넣어줘.",
    );
  }

  return {
    privacyPolicy,
    disclosureMode: "recipient-encrypted",
    disclosurePubKeyHex: pubKeyHex,
  };
}

function batchTransferItemLabel(index) {
  let value = index + 1;
  let label = "";
  while (value > 0) {
    value -= 1;
    label = String.fromCharCode(65 + (value % 26)) + label;
    value = Math.floor(value / 26);
  }
  return label;
}

function batchTransferRows() {
  return [...(els.batchTransferRows?.querySelectorAll("tr") || [])];
}

function updateBatchTransferRowLabels() {
  batchTransferRows().forEach((row, index) => {
    const label = row.querySelector(".batch-item-label");
    if (label) label.textContent = batchTransferItemLabel(index);
  });
}

function renderBatchTransferRowDisclosure(row) {
  const mode = row.querySelector("[data-batch-disclosure]")?.value || "private";
  const details = row.querySelector(".batch-disclosure-details");
  const target = row.querySelector("[data-batch-disclosure-target]");
  if (details) details.hidden = mode !== "recipient-encrypted";
  if (target) target.disabled = mode !== "recipient-encrypted";
}

function addBatchTransferRow({
  itemId = "",
  recipient = "",
  amount = "",
  disclosureMode = "private",
  disclosureTargetHex = "",
  evidence = "Not submitted",
} = {}) {
  if (!els.batchTransferRows) return null;
  batchTransferRowSequence += 1;
  const row = document.createElement("tr");
  row.dataset.batchItemId =
    itemId || `batch-payment-${Date.now()}-${batchTransferRowSequence}`;

  const itemCell = document.createElement("td");
  const itemLabel = document.createElement("span");
  itemLabel.className = "batch-item-label";
  itemCell.append(itemLabel);

  const recipientCell = document.createElement("td");
  recipientCell.className = "batch-recipient-cell";
  const recipientInput = document.createElement("input");
  recipientInput.dataset.batchRecipient = "true";
  recipientInput.placeholder = `${shieldedPrefix()}1...`;
  recipientInput.autocomplete = "off";
  recipientInput.value = recipient;
  recipientInput.setAttribute("aria-label", "Batch payment recipient");
  recipientCell.append(recipientInput);

  const amountCell = document.createElement("td");
  amountCell.className = "batch-amount-cell";
  const amountInput = document.createElement("input");
  amountInput.dataset.batchAmount = "true";
  amountInput.inputMode = "numeric";
  amountInput.placeholder = "0";
  amountInput.value = amount;
  amountInput.setAttribute("aria-label", `Batch payment amount in ${baseDenom()}`);
  amountCell.append(amountInput);

  const disclosureCell = document.createElement("td");
  disclosureCell.className = "batch-disclosure-cell";
  const disclosure = document.createElement("select");
  disclosure.dataset.batchDisclosure = "true";
  disclosure.setAttribute("aria-label", "Batch payment disclosure");
  for (const [value, label] of [
    ["private", "Private"],
    ["recipient-encrypted", "Recipient encrypted"],
    ["public", "Public amount"],
  ]) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = label;
    disclosure.append(option);
  }
  disclosure.value = disclosureMode;
  const disclosureDetails = document.createElement("div");
  disclosureDetails.className = "batch-disclosure-details";
  disclosureDetails.hidden = true;
  const disclosureTarget = document.createElement("input");
  disclosureTarget.dataset.batchDisclosureTarget = "true";
  disclosureTarget.placeholder = "recipient disclosure pubkey hex";
  disclosureTarget.value = disclosureTargetHex;
  disclosureTarget.disabled = disclosureMode !== "recipient-encrypted";
  disclosureTarget.setAttribute(
    "aria-label",
    "Recipient encrypted disclosure public key",
  );
  disclosureDetails.append(disclosureTarget);
  disclosureCell.append(disclosure, disclosureDetails);

  const evidenceCell = document.createElement("td");
  evidenceCell.className = "batch-evidence-cell";
  evidenceCell.dataset.batchEvidence = "true";
  evidenceCell.textContent = evidence;

  const actionCell = document.createElement("td");
  const remove = document.createElement("button");
  remove.type = "button";
  remove.className =
    "secondary-button icon-button batch-remove-button";
  remove.dataset.removeBatchPayment = "true";
  remove.textContent = "×";
  remove.setAttribute("aria-label", "Remove batch payment");
  actionCell.append(remove);

  row.append(
    itemCell,
    recipientCell,
    amountCell,
    disclosureCell,
    evidenceCell,
    actionCell,
  );
  els.batchTransferRows.append(row);
  renderBatchTransferRowDisclosure(row);
  for (const input of [recipientInput, amountInput, disclosureTarget]) {
    input.addEventListener("input", renderBatchTransferPreview);
  }
  disclosure.addEventListener("change", () => {
    renderBatchTransferRowDisclosure(row);
    renderBatchTransferPreview();
  });
  remove.addEventListener("click", () => {
    row.remove();
    if (!batchTransferRows().length) addBatchTransferRow();
    updateBatchTransferRowLabels();
    renderBatchTransferPreview();
  });
  updateBatchTransferRowLabels();
  renderBatchTransferPreview();
  return row;
}

function batchTransferPaymentFromRow(row, { strict = false } = {}) {
  const itemId = String(row.dataset.batchItemId || "");
  const recipient = String(
    row.querySelector("[data-batch-recipient]")?.value || "",
  ).trim();
  const rawAmount = String(
    row.querySelector("[data-batch-amount]")?.value || "",
  )
    .trim()
    .replace(/,/g, "");
  const mode =
    row.querySelector("[data-batch-disclosure]")?.value || "private";
  const disclosureTarget = String(
    row.querySelector("[data-batch-disclosure-target]")?.value || "",
  ).trim();
  const amountValid = /^(0|[1-9][0-9]*)$/.test(rawAmount) &&
    BigInt(rawAmount) > 0n;
  const recipientValid =
    Boolean(recipient) && isConfiguredShieldedAddress(recipient);
  const disclosureValid =
    mode !== "recipient-encrypted" ||
    /^[0-9a-fA-F]{64}$/.test(disclosureTarget);

  if (strict && !amountValid) {
    throw new Error(`Payment ${itemId || "item"} amount must be a positive ${baseDenom()} integer`);
  }
  if (strict && !recipientValid) {
    const error = new Error(
      `Every batch recipient must be a complete, valid ${shieldedPrefix()} shielded address.`,
    );
    error.code = "INVALID_SHIELDED_RECIPIENT";
    throw error;
  }
  if (strict && isSelfTransferRecipient(recipient)) {
    throw new Error("Batch payment recipients cannot be your own shielded address");
  }
  if (strict && !disclosureValid) {
    throw new Error(
      "Recipient encrypted disclosure requires a 32-byte disclosure public key",
    );
  }

  const payment = {
    itemId,
    recipient,
    amount: amountValid ? coinTextFromAmount(BigInt(rawAmount)) : "",
    amountValue: amountValid ? BigInt(rawAmount) : 0n,
    valid:
      amountValid &&
      recipientValid &&
      disclosureValid &&
      !isSelfTransferRecipient(recipient),
  };
  if (mode === "recipient-encrypted") {
    return {
      ...payment,
      userPrivacyPolicy: "amount",
      userDisclosureMode: "recipient-encrypted",
      userDisclosureTargetPubKeyHex: disclosureTarget,
    };
  }
  if (mode === "public") {
    return {
      ...payment,
      userPrivacyPolicy: "amount",
      userDisclosureMode: "public",
    };
  }
  return {
    ...payment,
    userPrivacyPolicy: "all-private",
    userDisclosureMode: "none",
  };
}

function collectBatchTransferPayments({ strict = false } = {}) {
  const payments = pendingBatchTransferPayments(
    batchTransferRows().map((row) =>
      batchTransferPaymentFromRow(row, { strict }),
    ),
    completedBatchTransferItemIDs,
  );
  if (strict && !payments.length) {
    throw new Error("Add at least one batch payment");
  }
  return payments;
}

function batchTransferAvailableNotes() {
  return state.keplr.notes
    .filter(isAvailableSpendableNote)
    .filter((note) => noteAmountValue(note) > 0n)
    .sort((left, right) => {
      const leftAmount = noteAmountValue(left);
      const rightAmount = noteAmountValue(right);
      return leftAmount === rightAmount ? 0 : leftAmount > rightAmount ? -1 : 1;
    });
}

function batchTransferPreview() {
  const payments = collectBatchTransferPayments();
  const notes = batchTransferAvailableNotes();
  const preview = computeBatchTransferPreviewState({
    paymentAmounts: payments.map((payment) => payment.amountValue),
    noteAmounts: notes.map(noteAmountValue),
    maxInputs: batchTransferMaxInputs,
    maxOutputs: batchTransferMaxOutputs,
  });
  const allRowsValid =
    payments.length > 0 && payments.every((payment) => payment.valid);
  const unsplittablePayments = preview.unsplittablePaymentIndexes.map(
    (index) => payments[index],
  );
  return {
    payments,
    ...preview,
    selected: notes.slice(0, preview.selectedCount),
    allRowsValid,
    unsplittablePayments,
  };
}

function renderBatchTransferPreview() {
  if (!els.batchTransferTotal) return;
  const preview = batchTransferPreview();
  els.batchTransferTotal.textContent = coinTextFromAmount(preview.total);
  els.batchTransferChange.textContent =
    preview.estimatedChange === null
      ? "Depends on batch split"
      : coinTextFromAmount(preview.estimatedChange);
  els.batchTransferInputs.textContent =
    `${preview.selected.length} / ${batchTransferMaxInputs}`;
  els.batchTransferOutputs.textContent =
    `${preview.outputCount} / ${batchTransferMaxOutputs}`;

  let capacity = "Add valid payment rows";
  if (!isChainSafetyReady()) {
    capacity = "Verifying current chain configuration";
  } else if (!isSpendChainReady()) {
    capacity = "Current privacy tree is empty; Deposit and Scan Notes before spending";
  } else if (preview.allRowsValid && !preview.totalCovered) {
    capacity =
      preview.totalAvailable > 0n
        ? `Insufficient available notes: ${coinTextFromAmount(preview.totalAvailable)} available`
        : "No available spendable notes; Scan Notes or resolve pending reservations";
  } else if (preview.requiresSplit) {
    capacity = "Exceeds one atomic batch";
  } else if (preview.allRowsValid) {
    capacity = "Fits one atomic batch";
  }
  if (preview.unsplittablePayments.length) {
    capacity = "A payment exceeds one batch input capacity";
  }
  els.batchTransferCapacity.textContent = capacity;
  const unsplittableLabels = preview.unsplittablePayments
    .map((payment) => payment.itemId)
    .join(", ");
  els.batchTransferCapacityWarning.textContent =
    preview.unsplittablePayments.length
      ? `Payment ${unsplittableLabels} exceeds the current 16-input capacity and cannot be split automatically. Reduce that payment or reorganize notes first.`
      : "This draft exceeds one atomic batch. It will not be split automatically.";
  els.batchTransferCapacityWarning.hidden =
    !preview.requiresSplit && !preview.unsplittablePayments.length;
  els.batchTransferSplitControl.hidden =
    !preview.requiresSplit || preview.unsplittablePayments.length > 0;
  if (
    !preview.requiresSplit ||
    preview.unsplittablePayments.length
  ) {
    els.batchTransferSplit.checked = false;
  }
  els.addBatchTransferRecipient.disabled =
    batchTransferInFlight || batchTransferRows().length >= 128;
  els.prepareBatchTransfer.disabled =
    batchTransferInFlight ||
    isPrivacyValueActionInFlight() ||
    !batchTransferFeatureEnabled() ||
    !isSpendChainReady() ||
    !hasCompletedPrivacyNoteScan() ||
    !preview.allRowsValid ||
    !preview.totalCovered ||
    preview.unsplittablePayments.length > 0 ||
    (preview.requiresSplit && !els.batchTransferSplit.checked);
}

function setBatchTransferItemEvidence(itemIds, text, tone = "") {
  const wanted = new Set(itemIds);
  for (const row of batchTransferRows()) {
    if (!wanted.has(row.dataset.batchItemId)) continue;
    const evidence = row.querySelector("[data-batch-evidence]");
    if (!evidence) continue;
    evidence.textContent = text;
    evidence.classList.toggle("verified", tone === "verified");
    evidence.classList.toggle("failed", tone === "failed");
  }
}

function markBatchTransferItemsCompleted(itemIds) {
  const completed = new Set(itemIds.map(String));
  for (const itemId of completed) {
    completedBatchTransferItemIDs.add(itemId);
  }
  for (const row of batchTransferRows()) {
    if (!completed.has(String(row.dataset.batchItemId || ""))) continue;
    row.dataset.batchCompleted = "true";
    row.classList.add("batch-payment-completed");
    for (const control of row.querySelectorAll("input, select, button")) {
      control.disabled = true;
    }
  }
}

function resetBatchTransferDraft() {
  closeBatchTransferConfirmation(false);
  batchTransferExpanded = false;
  batchTransferInFlight = false;
  completedBatchTransferItemIDs.clear();
  if (els?.batchTransferRows) els.batchTransferRows.textContent = "";
  if (els?.batchTransferSplit) els.batchTransferSplit.checked = false;
  if (els?.batchTransferState) els.batchTransferState.textContent = "Draft";
  renderBatchTransferVisibility();
}

function renderBatchTransferVisibility() {
  if (!els?.openBatchTransfer || !els?.batchTransferSection) return;
  const enabled = batchTransferFeatureEnabled();
  if (!enabled) batchTransferExpanded = false;
  els.openBatchTransfer.hidden = !enabled;
  els.batchTransferSection.hidden = !enabled || !batchTransferExpanded;
  if (enabled && batchTransferExpanded) renderBatchTransferPreview();
}

function openBatchTransferEditor() {
  if (!batchTransferFeatureEnabled()) return;
  batchTransferExpanded = true;
  if (!batchTransferRows().length) {
    addBatchTransferRow({
      recipient: els.veiledTransferRecipient.value.trim(),
      amount: String(els.veiledTransferAmount.value || "").trim(),
    });
    addBatchTransferRow();
  }
  renderBatchTransferVisibility();
  els.batchTransferSection.scrollIntoView?.({
    behavior: "smooth",
    block: "start",
  });
}

function closeBatchTransferEditor() {
  if (batchTransferInFlight) return;
  batchTransferExpanded = false;
  renderBatchTransferVisibility();
}

function closeBatchTransferConfirmation(result = false) {
  const resolve = batchTransferConfirmationResolve;
  batchTransferConfirmationResolve = null;
  if (els?.batchTransferConfirmationModal) {
    els.batchTransferConfirmationModal.hidden = true;
    els.batchTransferConfirmationModal.classList.remove("visible");
  }
  if (resolve) resolve(result);
}

function batchTransferConfirmationFacts(data, payments) {
  const facts = preparedBatchTransferFacts({
    requestedPayments: payments,
    prepared: data?.prepared || {},
    denom: baseDenom(),
    maxInputs: batchTransferMaxInputs,
    maxOutputs: batchTransferMaxOutputs,
  });
  const paymentLines = payments.map((payment, index) => {
    let disclosure = "Private";
    if (payment.userDisclosureMode === "public") {
      disclosure = "Public amount";
    } else if (payment.userDisclosureMode === "recipient-encrypted") {
      disclosure =
        `Recipient encrypted · target ${shorten(payment.userDisclosureTargetPubKeyHex, 12, 10)}`;
    }
    return (
      `${batchTransferItemLabel(index)} · ${coinTextFromAmount(payment.amountValue)}\n` +
      `Recipient: ${payment.recipient}\n` +
      `Disclosure: ${disclosure}`
    );
  });
  const disclosure = facts.disclosureCounts;
  return {
    ...facts,
    paymentLines,
    disclosureText:
      `Private ${disclosure.private} · Public ${disclosure.public} · ` +
      `Recipient encrypted ${disclosure.recipientEncrypted}`,
  };
}

function requestPreparedBatchTransferConfirmation(facts) {
  closeBatchTransferConfirmation(false);
  els.batchTransferConfirmationTotal.textContent =
    coinTextFromAmount(facts.total);
  els.batchTransferConfirmationChange.textContent =
    coinTextFromAmount(facts.change);
  els.batchTransferConfirmationInputs.textContent =
    `${facts.inputCount} / ${batchTransferMaxInputs}`;
  els.batchTransferConfirmationOutputs.textContent =
    `${facts.outputCount} / ${batchTransferMaxOutputs}`;
  els.batchTransferConfirmationPayments.textContent =
    facts.paymentLines.join("\n");
  els.batchTransferConfirmationDisclosures.textContent =
    facts.disclosureText;
  els.batchTransferConfirmationModal.hidden = false;
  requestAnimationFrame(() =>
    els.batchTransferConfirmationModal.classList.add("visible"),
  );
  els.confirmBatchTransferConfirmation.focus();
  return new Promise((resolve) => {
    batchTransferConfirmationResolve = resolve;
  });
}

async function confirmPreparedBatchTransferBeforeBroadcast(
  data,
  payments,
  {
    session = preparedPrivacySession(data) || beginPrivacySessionOperation(),
  } = {},
) {
  let facts;
  try {
    facts = batchTransferConfirmationFacts(data, payments);
  } catch (error) {
    if (error && typeof error === "object") {
      // The proof is prepared but no wallet request has started.  This is a
      // local evidence/confirmation failure, so it is safe to release the
      // reservation and tell the caller not to treat it as a submitted tx.
      error.batchTransferConfirmationFailedBeforeWallet = true;
    }
    await markPreparedReservationReplanRequired(
      data,
      error,
      "prepared_batch_confirmation_facts_invalid",
      { session },
    );
    await clearBatchTransferArtifact({ session });
    throw error;
  }
  const confirmed = await requestPreparedBatchTransferConfirmation(facts);
  if (!isPrivacySessionCurrent(session)) {
    await replanInvalidatedPreparedReservation(
      data,
      session,
      privacySessionInvalidatedError(),
    );
    return false;
  }
  if (confirmed) return true;
  const error = noBroadcastAttemptError(
    new Error("Prepared batch transfer was cancelled before wallet signing"),
  );
  await markPreparedReservationReplanRequired(
    data,
    error,
    "prepared_batch_confirmation_cancelled",
    { session },
  );
  await clearBatchTransferArtifact({ session });
  assertPrivacySessionCurrent(session);
  return false;
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

const reservationReviewDialogState = {
  operationID: "",
  explicitCancellation: false,
  preparedBatchReservation: null,
  stalledBatchReservation: null,
  running: false,
};

function closeReservationReviewDialog() {
  if (reservationReviewDialogState.running) return;
  reservationReviewDialogState.operationID = "";
  reservationReviewDialogState.explicitCancellation = false;
  reservationReviewDialogState.preparedBatchReservation = null;
  reservationReviewDialogState.stalledBatchReservation = null;
  els.reservationReviewAcknowledge.checked = false;
  els.reservationReviewModal.hidden = true;
  els.reservationReviewModal.classList.remove("visible");
}

function openReservationReviewDialog(
  operationID,
  records,
  {
    knownNoWalletRequest = false,
    preparedBatchReservation = null,
    stalledBatchReservation = null,
  } = {},
) {
  const explicitCancellation =
    knownNoWalletRequest ||
    Boolean(preparedBatchReservation) ||
    Boolean(stalledBatchReservation) ||
    manualReviewRequiresExplicitReservationCancellation(records);
  reservationReviewDialogState.operationID = operationID;
  reservationReviewDialogState.explicitCancellation = explicitCancellation;
  reservationReviewDialogState.preparedBatchReservation =
    preparedBatchReservation;
  reservationReviewDialogState.stalledBatchReservation =
    stalledBatchReservation;
  reservationReviewDialogState.running = false;
  const recoveredPreparedBatch = Boolean(preparedBatchReservation);
  const recoveredStalledBatch = Boolean(stalledBatchReservation);
  els.reservationReviewState.textContent = knownNoWalletRequest
    ? "지갑 서명 전 중단"
    : recoveredPreparedBatch
      ? "이전 batch checkpoint 확인 필요"
      : recoveredStalledBatch
        ? "이전 batch checkpoint 확인 필요"
      : explicitCancellation
      ? "제출 상태 불명"
      : "상태 확인 필요";
  els.reservationReviewLead.textContent = knownNoWalletRequest
    ? "Batch proof 준비가 지갑 서명 창을 열기 전에 중단되었습니다. 체인 거래는 제출되지 않았지만, 암호화된 checkpoint가 input note를 보호하기 위해 로컬 예약을 남겼습니다."
    : recoveredPreparedBatch
      ? "이전 batch proof checkpoint가 남아 있어 새 batch를 막고 있습니다. 제출 가능한 tx hash는 없지만, 지갑 승인 직후 브라우저가 중단됐을 가능성까지 고려해 확인 후에만 이 checkpoint를 폐기합니다."
      : recoveredStalledBatch
        ? "이전 batch checkpoint가 입력 note 예약을 남긴 채 끝났습니다. query 가능한 tx hash는 없지만, 지갑 승인 또는 broadcast 경계에서 중단됐을 가능성까지 고려해 확인 후에만 이 예약을 검토 상태로 옮겨 해제합니다."
      : explicitCancellation
      ? "이 예약에는 체인에서 조회할 수 있는 제출 거래 식별자가 남아 있지 않습니다. 이전 지갑 요청이 어느 단계에서 멈췄는지 확정할 수 없습니다."
      : "예약과 체인 상태를 다시 확인한 뒤, 안전한 경우에만 이 예약을 다시 계획할 수 있게 해제합니다.";
  els.reservationReviewCauses.replaceChildren();
  if (explicitCancellation || knownNoWalletRequest) {
    const causes = knownNoWalletRequest
      ? [
          "Batch prover 또는 checkpoint 준비가 실패했습니다.",
          "Keplr 서명 창은 열리지 않았고 batch transaction도 제출되지 않았습니다.",
          "계속하면 앱은 모든 input nullifier가 아직 unspent인지 확인한 뒤 이 로컬 예약만 해제합니다.",
        ]
      : recoveredPreparedBatch
        ? [
            "지갑 승인 창을 열기 전 브라우저 흐름이 끊겼을 수 있습니다.",
            "사용자가 지갑 요청을 취소했을 수 있습니다.",
            "사용자가 승인했지만 제출 결과를 저장하기 전 새로고침 또는 오류가 발생했을 수 있습니다.",
          ]
        : recoveredStalledBatch
          ? [
              "proof 준비 중 브라우저 흐름 또는 prover 요청이 중단됐을 수 있습니다.",
              "지갑 승인 또는 broadcast 경계에서 결과를 저장하기 전에 중단됐을 수 있습니다.",
              "계속하면 앱은 broadcast 증거가 없는지와 모든 input nullifier가 unspent인지 확인한 뒤에만 예약을 해제합니다.",
            ]
      : [
          "지갑 승인 창을 열기 전 브라우저 흐름이 끊겼을 수 있습니다.",
          "사용자가 지갑 요청을 취소했을 수 있습니다.",
          "사용자가 승인했지만 제출 결과를 저장하기 전 새로고침 또는 오류가 발생했을 수 있습니다.",
        ];
    for (const cause of causes) {
      const item = document.createElement("li");
      item.textContent = cause;
      els.reservationReviewCauses.append(item);
    }
  }
  els.reservationReviewCauses.hidden = !explicitCancellation && !knownNoWalletRequest;
  els.reservationReviewWarning.textContent = explicitCancellation
    ? knownNoWalletRequest
      ? "이 작업은 실패한 proof checkpoint와 로컬 예약만 폐기합니다. 새 거래를 전송하지 않으며, input nullifier가 모두 unspent인 경우에만 다시 계획 가능 상태로 전환합니다."
      : recoveredPreparedBatch
        ? "이 작업은 새 거래를 전송하지 않습니다. 현재 checkpoint에 broadcast 증거나 tx hash가 없는지와 모든 input nullifier가 unspent인지 다시 확인한 뒤에만 checkpoint와 로컬 예약을 폐기합니다."
        : recoveredStalledBatch
          ? "이 작업은 새 거래를 전송하지 않습니다. 이전 batch 예약을 먼저 검토 상태로 전환한 뒤, 모든 input nullifier가 unspent인 경우에만 다시 계획 가능 상태로 해제합니다."
      : "예약 취소는 체인 거래를 취소하지 않습니다. 이미 승인된 거래가 나중에 제출될 가능성이 없는지 지갑 활동과 탐색기에서 확인한 경우에만 계속하세요. 계속하면 앱은 모든 input nullifier가 아직 unspent인지 다시 확인한 뒤 이 로컬 예약만 다시 계획 가능 상태로 전환합니다."
    : "이 작업은 새 거래를 전송하지 않습니다. 현재 예약과 input nullifier를 다시 확인하며, 확인할 수 없는 상태면 예약은 계속 잠깁니다.";
  els.reservationReviewAcknowledgement.hidden = !explicitCancellation;
  els.reservationReviewAcknowledge.checked = false;
  els.confirmReservationReview.textContent = explicitCancellation
    ? "예약 취소 및 다시 준비"
    : "상태 확인 후 예약 해제";
  els.confirmReservationReview.disabled = explicitCancellation;
  els.reservationReviewModal.hidden = false;
  requestAnimationFrame(() => els.reservationReviewModal.classList.add("visible"));
  els.cancelReservationReview.focus();
}

function toast(message) {
  showNotice({ message });
}

function showSendResult({ success, wallet, txHash, error }) {
  if (success) {
    showNotice({
      title: "Send 요청됨",
      message: `${wallet} send가 제출되었습니다.\nTx: ${shorten(txHash, 14, 12)}`,
    });
    return;
  }

  showNotice({
    title: "Send 실패",
    message: error || "Send 요청이 완료되지 않았습니다.",
    failed: true,
  });
}

const transferFlowState = {
  resolve: null,
  running: false,
  copy: null,
  flowID: 0,
};

const transferFlowSteps = [
  { key: "zero", element: () => els.transferStepZero },
  { key: "transfer", element: () => els.transferStepTransfer },
];

const privacyFlowCopies = {
  transfer: {
    title: "Privacy Transfer 확인",
    lead: "입력하신 금액을 보낼 수 있도록 note 구성을 먼저 확인합니다. 필요한 경우 self transaction 서명이 먼저 요청됩니다.",
    runningLead: "Keplr 창이 뜨면 현재 단계의 내용을 확인하고 서명해 주세요.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "요청이 완료되지 않았습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy:
      "입력하신 금액의 노트를 만들기 위해 self transaction이 필요한지 확인합니다.",
    stepTwoTitle: "트랜스퍼 서명",
    stepTwoCopy:
      "준비된 note로 실제 privacy transfer를 요청합니다. Keplr에서 내용을 확인하고 서명합니다.",
    successTitle: "트랜스퍼 요청이 성공하였습니다",
    successCopy: "최신 notes를 다시 스캔한 상태입니다.",
    failureTitle: "트랜스퍼 요청이 실패했습니다",
  },
  withdraw: {
    title: "Privacy Withdraw 확인",
    lead: "Clair로 출금하려면 입력 금액과 정확히 같은 note가 필요합니다. 없으면 먼저 self transaction 서명이 요청됩니다.",
    runningLead: "Keplr 창이 뜨면 현재 단계의 내용을 확인하고 서명해 주세요.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "요청이 완료되지 않았습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy:
      "Withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다. 없으면 내 Veiled balance 안에서 note를 재구성합니다.",
    stepTwoTitle: "위드드로우 서명",
    stepTwoCopy:
      "준비된 note로 실제 withdraw를 요청합니다. Keplr에서 받을 Clair 주소와 금액을 확인하고 서명합니다.",
    successTitle: "Withdraw 요청이 성공하였습니다",
    successCopy: "Clair balance와 최신 notes를 다시 불러온 상태입니다.",
    failureTitle: "Withdraw 요청이 실패했습니다",
  },
  relayWithdraw: {
    title: "Relay Withdraw 확인",
    lead: "Clair로 출금할 payload는 브라우저 SDK가 만들고, 마지막 broadcast는 Relayer 패널에서 relayer 계정이 제출합니다.",
    runningLead:
      "note 준비 단계에서는 지갑 확인이 필요할 수 있고, 완료 후 payload가 화면에 고정됩니다.",
    doneLead: "요청이 처리되었습니다.",
    failedLead: "요청이 완료되지 않았습니다.",
    stepOneTitle: "노트 준비",
    stepOneCopy:
      "Relay withdraw도 입력 금액과 정확히 같은 note가 필요합니다. 없으면 내 Veiled balance 안에서 note를 재구성합니다.",
    stepTwoTitle: "Payload 준비",
    stepTwoCopy: "준비된 relay withdraw payload를 오른쪽 화면에 고정합니다.",
    successTitle: "Relay withdraw payload가 준비되었습니다",
    successCopy:
      "오른쪽 Relayer 패널에서 payload를 확인한 뒤 relayer 계정으로 제출할 수 있습니다.",
    failureTitle: "Relay withdraw 요청이 실패했습니다",
  },
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
    this.txHash = data?.txHash || data?.tx_hash || "";
    this.data = data || {};
  }
}

function plannerFactsFromError(error) {
  const plan = error?.plan ?? error?.details?.plan ?? error?.data?.plan;
  const facts = plan?.facts;
  return facts && typeof facts === "object" && !Array.isArray(facts)
    ? facts
    : null;
}

function plannerFactAmount(facts, field, denom) {
  const value = String(facts?.[field] ?? "").trim();
  const match = /^(0|[1-9][0-9]*)([a-zA-Z][a-zA-Z0-9/._-]*)$/.exec(value);
  return match && match[2] === denom ? value : "";
}

function plannerFactCount(facts, field) {
  const value = Number(facts?.[field]);
  return Number.isSafeInteger(value) && value >= 0 ? value : null;
}

function insufficientBalancePlannerMessage(error) {
  const fallback =
    "Scanned spendable notes do not cover this amount. Scan Notes after any pending transaction, then retry.";
  const facts = plannerFactsFromError(error);
  const denom = baseDenom();
  const requested = plannerFactAmount(facts, "requestedAmount", denom);
  const spendable = plannerFactAmount(facts, "spendableTotal", denom);
  const count = plannerFactCount(facts, "spendableCount");
  if (!requested || !spendable || count == null) return fallback;
  return (
    `The current planner scan found ${spendable} across ${count} available ${denom} note(s), ` +
    `but ${requested} was requested. Reserved, spent, unverified, and other-asset notes are excluded.`
  );
}

function privacyOperationErrorMessage(error, fallback = "Privacy operation failed") {
  // ClairveilJS intentionally keeps upstream prover response bodies private.
  // Its HTTP adapter exposes only the reviewed response code as `proverCode`,
  // while local DApp API errors use `code`. Treat both as the same safe UI
  // classification so batch failures do not collapse into a generic message.
  const code = String(error?.code || error?.proverCode || "");
  const plannerStatus = String(error?.status || error?.plan?.status || "");
  const message = String(error?.message || "");
  switch (code) {
    case "INVALID_AMOUNT":
      return "Transfer amount must be a positive whole number of the displayed minimal denom.";
    case "INVALID_SHIELDED_RECIPIENT":
      return `Enter a complete, valid ${shieldedPrefix()} shielded recipient address. A shortened address or one with a checksum typo cannot be proved.`;
    case "INSUFFICIENT_BALANCE":
      return insufficientBalancePlannerMessage(error);
    case "ZERO_DUMMY_REQUIRED":
      return "Note preparation needs a zero-value helper note. Approve the requested self transaction, then retry.";
    case "SELF_MERGE_REQUIRED":
      return "Note preparation needs a self transaction before the transfer. Approve the requested self transaction, then retry.";
    case "ROOT_SIGNATURE_REQUIRED":
    case "SIGNER_MISMATCH":
      return "Wallet privacy session is no longer valid. Reconnect the wallet, sign the session, and scan Notes before retrying.";
    case "WALLET_UNAVAILABLE":
      return "Wallet connection is unavailable. Reconnect the wallet before retrying.";
    case "4001":
      return "The wallet signature request was rejected before broadcast.";
    case "DISCLOSURE_UNAVAILABLE":
      return "The selected disclosure mode is unavailable on the current chain configuration. Review the disclosure settings and retry.";
    case "TX_BROADCAST_FAILED":
      return "The wallet or chain rejected the privacy transaction before completion. Scan Notes before retrying.";
    case "RELAY_PAYLOAD_EXPIRED":
      return "Relay payload expired before local submission. Prepare a fresh relay payload.";
    case "RELAY_INPUT_UNAVAILABLE":
      return "Relay input notes are no longer unspent. Scan Notes and prepare a fresh relay payload.";
    case "RELAY_SUBMISSION_FAILED":
      return "The local relayer could not submit the payload and no transaction was confirmed. Refresh relay status, then prepare a fresh payload if it remains pending.";
    case "request_timeout":
      return "The local relayer did not return before the timeout. The relay transaction may still be pending; refresh Notes to reconcile and do not submit this payload again.";
    case "PROVER_TIMEOUT":
      return "Privacy proof service timed out. Retry with the same configured provider.";
    case "PROVER_CANCELLED":
      return "Privacy proof preparation was cancelled after the wallet session changed. Reconnect or rescan Notes, then retry.";
    case "PROVER_UNAVAILABLE":
    case "unavailable":
      return "Privacy proof service is unavailable. Retry with the same configured provider.";
    case "PROVER_REJECTED":
    case "proof_failed":
      return "Privacy proof service rejected the request. Verify the provider configuration and retry.";
    case "invalid_request":
      return "Privacy proof request was rejected. Verify the provider configuration and retry.";
    case "unauthorized":
      return "Privacy proof service authorization failed. Verify the configured provider access and retry.";
    case "not_found":
    case "method_not_allowed":
      return "Privacy proof service endpoint is unavailable. Verify the configured provider URL and retry.";
    default:
      break;
  }
  if (/^Prepared transfer (payload|effect) is missing$/.test(message)) {
    return "Prepared transfer details were incomplete. Refresh Notes and retry.";
  }
  if (/privacy note sync is incomplete|privacy note sync did not complete/i.test(message)) {
    return "Privacy note sync is incomplete. Finish a successful Scan Notes before preparing a batch.";
  }
  if (/privacy tree has not been initialized/i.test(message)) {
    return "The privacy tree is not initialized yet. Deposit funds, wait for inclusion, then Scan Notes before preparing a batch.";
  }
  if (/checkpointed batch is still unresolved/i.test(message)) {
    return "A previous batch is still awaiting reservation recovery. Scan Notes and resolve that batch before preparing another one.";
  }
  if (/encrypted batch artifact storage is unavailable/i.test(message)) {
    return "Encrypted batch recovery storage is unavailable in this browser. Enable browser storage, then reconnect and Scan Notes before retrying.";
  }
  if (/latest chain block time is unavailable/i.test(message)) {
    return "The latest chain time is unavailable. Wait for the selected node to respond, then retry without creating a new batch draft.";
  }
  if (/keplr signdirect not available/i.test(message)) {
    return "Keplr does not expose Direct signing for this chain. Reconnect the wallet or update Keplr before retrying.";
  }
  if (/keplr is not connected/i.test(message)) {
    return "Keplr is no longer connected. Reconnect the wallet, then prepare the batch again.";
  }
  if (/Cosmos sign doc does not match the reservation ProofReady artifact/.test(message)) {
    return "Keplr changed the prepared transaction during signing. Refresh Notes and retry with the exact prepared fee and memo.";
  }
  switch (plannerStatus) {
    case "invalid_amount":
      return "Transfer amount must be a positive whole number of the displayed minimal denom.";
    case "insufficient_balance":
      return "Scanned spendable notes do not cover this amount. Scan Notes after any pending transaction, then retry.";
    case "zero_dummy_required":
      return "Note preparation needs a zero-value helper note. Approve the requested self transaction, then retry.";
    case "self_merge_required":
      return "Note preparation needs a self transaction before the transfer. Approve the requested self transaction, then retry.";
    case "exact_note_required":
      return "The requested amount needs note preparation before it can be spent. Approve the requested self transaction, then retry.";
    default:
      return fallback;
  }
}

function batchTransferPreflightErrorMessage(error) {
  // Preparation contains several fail-closed reads.  The raw SDK/transport
  // messages may include endpoint or request details, so attach and render a
  // small stage label rather than exposing them to the page.
  switch (String(error?.batchTransferPreflightStage || "")) {
    case "feature-gate":
      return "Atomic batch transfer is not enabled for the selected chain profile.";
    case "chain-safety":
      return "The selected chain's privacy configuration could not be verified. Wait for the local node to be available, then retry.";
    case "note-sync":
      return "Notes are not in a completed, spend-safe scan state. Scan Notes successfully before preparing this batch.";
    case "typed-note-scan":
      return "The fresh typed privacy scan required before batch preparation did not complete. Scan Notes again, then retry.";
    case "artifact-recovery":
      if (/without reservation identity/i.test(String(error?.message || ""))) {
        return "An encrypted batch checkpoint is missing its reservation identity. It cannot be cancelled safely; reset and rescan the encrypted local cache before preparing another batch.";
      }
      if (/no payment records/i.test(String(error?.message || ""))) {
        return "An encrypted batch checkpoint has no recoverable payment records. Reset and rescan the encrypted local cache before preparing another batch.";
      }
      if (/conflicting terminal reservation|conflicting operation evidence/i.test(String(error?.message || ""))) {
        return "A previous batch has conflicting chain or reservation evidence. It remains locked; finish Scan Notes and review its transaction record before preparing another batch.";
      }
      return "A previous batch reservation could not be recovered into a safe cancellation flow. Finish Scan Notes; if it persists, reset and rescan the encrypted local cache before preparing another batch.";
    case "chain-time":
      return "The latest chain time could not be read. Wait for the selected node to respond, then retry.";
    case "reservation-storage":
      return "Encrypted local reservation storage is unavailable. Enable browser storage, reconnect the wallet, then Scan Notes before retrying.";
    default:
      return privacyOperationErrorMessage(
        error,
        "Batch preflight failed before proof preparation. No wallet request or transaction was submitted. Fix the reported chain, scan, or local-storage issue, then retry.",
      );
  }
}

function privacyReservationErrorCode(error, fallback = "privacy_operation_failed") {
  switch (String(error?.code || error?.proverCode || "")) {
    case "PROVER_TIMEOUT":
      return "prover_timeout";
    case "PROVER_UNAVAILABLE":
    case "unavailable":
      return "prover_unavailable";
    case "PROVER_REJECTED":
    case "proof_failed":
    case "invalid_request":
      return "prover_rejected";
    case "unauthorized":
      return "prover_unauthorized";
    case "not_found":
    case "method_not_allowed":
      return "prover_endpoint_unavailable";
    default:
      return fallback;
  }
}

const privacySafeReservationManagers = new WeakMap();

// ClairveilJS may perform reservation transitions internally around wallet and
// RPC boundaries. Keep its durable error field to a stable code even if a
// wallet, prover, or transport error echoes private request material.
function createPrivacySafeReservationManager(manager) {
  if (!manager || typeof manager !== "object") return manager;
  const existing = privacySafeReservationManagers.get(manager);
  if (existing) return existing;
  const fallbackByMethod = new Map([
    ["markBroadcastRejected", "wallet_request_rejected"],
    ["markUnknown", "broadcast_outcome_unknown"],
    ["markReplanRequired", "pre_broadcast_operation_cancelled"],
    ["markManualReview", "manual_review_required"],
  ]);
  const proxy = new Proxy(manager, {
    get(target, property) {
      const value = Reflect.get(target, property, target);
      if (typeof value !== "function") return value;
      const fallback = fallbackByMethod.get(property);
      if (!fallback) return value.bind(target);
      return (reservationIDs, metadata = {}) => {
        const source =
          metadata && typeof metadata === "object" ? metadata : {};
        const safeMetadata = {
          ...source,
          error: privacyReservationErrorCode(source.error, fallback),
        };
        if (property === "markBroadcastRejected") {
          safeMetadata.providerCode = "4001";
        }
        return value.call(target, reservationIDs, safeMetadata);
      };
    },
  });
  privacySafeReservationManagers.set(manager, proxy);
  return proxy;
}

function safeDepositProofProviderError(error) {
  if (error?.privacySessionInvalidated) return error;
  const code = String(error?.code || "");
  const recognizedCode = new Set([
    "PROVER_TIMEOUT",
    "PROVER_UNAVAILABLE",
    "PROVER_REJECTED",
    "unavailable",
    "proof_failed",
    "invalid_request",
    "unauthorized",
    "not_found",
    "method_not_allowed",
  ]);
  const safe = new Error(
    recognizedCode.has(code)
      ? privacyOperationErrorMessage(error)
      : "Privacy proof service failed. Verify the provider configuration and retry.",
  );
  safe.code = recognizedCode.has(code) ? code : "proof_failed";
  return safe;
}

function requireVersionedDepositProofResponse(response) {
  if (
    !response ||
    typeof response !== "object" ||
    Array.isArray(response) ||
    response.version !== depositProofResponseVersion
  ) {
    const error = new Error("Deposit proof response has an unsupported version");
    error.code = "proof_failed";
    throw error;
  }
  return response;
}

function browserDataLoadErrorMessage(error) {
  const message = error?.message || String(error || "Request failed");
  if (
    /failed to fetch|load failed|networkerror|network request failed/i.test(
      message,
    )
  ) {
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

function resetTransferConfirmationFacts() {
  els.transferConfirmationFacts.hidden = true;
  els.transferConfirmationChainId.textContent = "-";
  els.transferConfirmationExpiry.textContent = "-";
  els.transferConfirmationRecipient.textContent = "-";
  els.transferConfirmationChange.textContent = "-";
  els.transferConfirmationDisclosure.textContent = "-";
}

function preparedTransferText(value, label) {
  const text = String(value ?? "").trim();
  if (!text) throw new Error(`Prepared transfer ${label} is missing`);
  return text;
}

function preparedTransferPayload(data) {
  const payload = data?.payload || data?.prepared?.payload;
  if (!payload || typeof payload !== "object") {
    throw new Error("Prepared transfer payload is missing");
  }
  return payload;
}

function preparedTransferExpiryText(value) {
  const expiresAtUnix = Number(value);
  if (!Number.isSafeInteger(expiresAtUnix) || expiresAtUnix <= 0) {
    throw new Error("Prepared transfer expiry is invalid");
  }
  return `${expiresAtUnix} (${new Date(expiresAtUnix * 1000).toISOString()})`;
}

function preparedTransferDisclosurePlanes(payload) {
  const policy = Number(payload.user_privacy_policy);
  const mode = Number(payload.user_disclosure_mode);
  const fields = [
    policy & 1 ? "amount + asset" : "",
    policy & 2 ? "recipient" : "",
    policy & 4 ? "sender" : "",
  ].filter(Boolean);
  const userMode =
    mode === 0
      ? "none"
      : mode === 1
        ? "public"
        : mode === 2
          ? "recipient encrypted"
          : "invalid";
  if (
    !Number.isInteger(policy) ||
    policy < 0 ||
    policy > 7 ||
    userMode === "invalid"
  ) {
    throw new Error("Prepared transfer user disclosure is invalid");
  }
  if ((policy === 0) !== (mode === 0)) {
    throw new Error("Prepared transfer user disclosure policy and mode disagree");
  }
  const auditIncluded = Boolean(
    String(payload.audit_disclosure_payload_hex || "").trim(),
  );
  if (!auditIncluded) {
    throw new Error("Prepared transfer is missing mandatory audit disclosure");
  }
  const selfViewIncluded = Boolean(
    String(payload.self_view_disclosure_payload_hex || "").trim(),
  );
  if (!selfViewIncluded && !isEvmTransparentMode()) {
    throw new Error(
      "Prepared Cosmos transfer is missing default self-view disclosure",
    );
  }
  const user =
    mode === 0 ? "User: none" : `User: ${userMode} (${fields.join(", ")})`;
  const selfView = selfViewIncluded
    ? "Self-view: encrypted"
    : "Self-view: unavailable on the EVM transfer ABI";
  return `${user} · ${selfView} · Audit: encrypted`;
}

function preparedTransferConfirmationFacts(data) {
  const payload = preparedTransferPayload(data);
  const outputs = payload.outputs;
  if (!Array.isArray(outputs) || outputs.length !== 2) {
    throw new Error("Prepared transfer must contain recipient and change outputs");
  }
  preparedTransferText(payload.owner_signature_hex, "owner-intent signature");
  const recipient = preparedTransferText(
    data?.prepared?.finalRecipient || data?.prepared?.recipient,
    "recipient",
  );
  const recipientAmount = preparedTransferText(
    outputs[0]?.amount,
    "recipient output amount",
  );
  const changeAmount = preparedTransferText(
    outputs[1]?.amount,
    "change output amount",
  );
  return {
    chainId: preparedTransferText(payload.chain_id, "chain ID"),
    expiry: preparedTransferExpiryText(payload.expires_at_unix),
    recipient: `${recipient} receives ${coinText(recipientAmount)}`,
    change: `${coinText(changeAmount)} returns to your shielded address`,
    disclosure: preparedTransferDisclosurePlanes(payload),
  };
}

function showPreparedTransferConfirmationFacts(facts) {
  els.transferConfirmationChainId.textContent = facts.chainId;
  els.transferConfirmationExpiry.textContent = facts.expiry;
  els.transferConfirmationRecipient.textContent = facts.recipient;
  els.transferConfirmationChange.textContent = facts.change;
  els.transferConfirmationDisclosure.textContent = facts.disclosure;
  els.transferConfirmationFacts.hidden = false;
}

function showTransferPlannerFacts({ requested, currentMax, action }) {
  const currentMaxRow = els.transferPlannerCurrentMax.closest("div");
  const hasCurrentMax =
    currentMax !== undefined &&
    currentMax !== null &&
    String(currentMax).trim() !== "";
  els.transferPlannerFacts.hidden = false;
  els.transferPlannerRequested.textContent = coinText(requested);
  currentMaxRow.hidden = !hasCurrentMax;
  els.transferPlannerCurrentMax.textContent = hasCurrentMax
    ? coinText(currentMax)
    : "-";
  els.transferPlannerAction.textContent = action || "-";
}

function parsePlannerAmountValue(value) {
  const text = String(value || "").trim();
  const raw = text.endsWith(baseDenom())
    ? text.slice(0, -baseDenom().length)
    : text;
  if (!/^\d+$/.test(raw)) return null;
  return BigInt(raw);
}

function plannerCurrentTransferMaxForNoteMerge(data, requested) {
  const facts = data?.plan?.facts || {};
  const requestedValue = parsePlannerAmountValue(requested);
  const currentTransferMax =
    facts.selectedInputTotalValue ||
    facts.selectedInputTotal ||
    data?.plan?.nextAmount ||
    data?.prepared?.amount;
  const currentTransferMaxValue = parsePlannerAmountValue(currentTransferMax);
  if (
    requestedValue === null ||
    currentTransferMaxValue === null ||
    currentTransferMaxValue >= requestedValue
  ) {
    return "";
  }
  return (
    facts.selectedInputTotal ||
    facts.selectedInputTotalValue ||
    data?.plan?.nextAmount ||
    data?.prepared?.amount ||
    ""
  );
}

function plannerCurrentExactNoteMaxForWithdraw(data, requested) {
  const facts = data?.plan?.facts || {};
  const requestedValue = parsePlannerAmountValue(requested);
  const currentExactNoteMax = facts.currentMaxNoteValue || facts.currentMaxNote;
  const currentExactNoteMaxValue = parsePlannerAmountValue(currentExactNoteMax);
  if (
    requestedValue === null ||
    currentExactNoteMaxValue === null ||
    currentExactNoteMaxValue >= requestedValue
  ) {
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
  els.transferFlowModal.hidden = true;
  els.transferFlowModal.classList.remove("visible");
  if (resolve) {
    resolve(result);
  }
}

function resetTransferFlowForPrivacySession() {
  const { resolve } = transferFlowState;
  transferFlowState.resolve = null;
  transferFlowState.running = false;
  transferFlowState.copy = null;
  transferFlowState.flowID += 1;
  resetTransferPlannerFacts();
  resetTransferConfirmationFacts();
  els.transferFlowModal.hidden = true;
  els.transferFlowModal.classList.remove("visible", "failed");
  els.transferSteps.hidden = false;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferFailureReason.textContent = "-";
  els.transferModalLead.textContent = "Privacy session cleared.";
  els.cancelTransferFlow.hidden = true;
  els.confirmTransferFlow.hidden = true;
  setTransferFlowStep("", "Privacy session cleared.");
  if (resolve) resolve(false);
}

function setTransferFlowStep(activeKey, stateText) {
  if (stateText) {
    els.transferModalState.textContent = stateText;
  }

  const activeIndex = transferFlowSteps.findIndex(
    (step) => step.key === activeKey,
  );
  for (const [index, step] of transferFlowSteps.entries()) {
    const element = step.element();
    const isActive = step.key === activeKey;
    const isDone =
      activeKey === "done" || (activeIndex > -1 && index < activeIndex);
    element.classList.toggle("active", isActive);
    element.classList.toggle("done", isDone);
  }
}

function transferFlowIsCurrent(flowID) {
  return Number.isSafeInteger(flowID) &&
    flowID === transferFlowState.flowID;
}

function openTransferFlowModal(kind = "transfer") {
  applyPrivacyFlowCopy(kind);
  transferFlowState.flowID += 1;
  transferFlowState.running = false;
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
  resetTransferPlannerFacts();
  resetTransferConfirmationFacts();
  setTransferFlowStep("", "확인 필요");
  els.transferFlowModal.hidden = false;
  requestAnimationFrame(() => els.transferFlowModal.classList.add("visible"));
  els.confirmTransferFlow.focus();
  return new Promise((resolve) => {
    transferFlowState.resolve = resolve;
  });
}

function confirmTransferFlowStart() {
  if (!transferFlowState.resolve) return;
  const resolve = transferFlowState.resolve;
  transferFlowState.resolve = null;
  transferFlowState.running = true;
  els.cancelTransferFlow.hidden = true;
  els.confirmTransferFlow.hidden = true;
  els.transferModalLead.textContent =
    transferFlowState.copy?.runningLead ||
    privacyFlowCopies.transfer.runningLead;
  resolve(true);
}

function cancelTransferFlow() {
  if (transferFlowState.running) return;
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

function requestPreparedTransferConfirmation(facts) {
  transferFlowState.running = false;
  els.transferSteps.hidden = false;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferFlowModal.classList.remove("failed");
  showPreparedTransferConfirmationFacts(facts);
  els.cancelTransferFlow.textContent = "취소";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.confirmTransferFlow.hidden = false;
  els.confirmTransferFlow.disabled = false;
  els.confirmTransferFlow.textContent = "최종 전송 확인";
  els.transferModalLead.textContent =
    "준비된 owner-intent effect를 확인한 뒤 전송을 승인해 주세요. 취소하면 이 prepared proof는 버리고 note plan을 다시 만들어야 합니다.";
  setTransferFlowStep("transfer", "최종 확인 필요");
  els.confirmTransferFlow.focus();
  return new Promise((resolve) => {
    transferFlowState.resolve = resolve;
  });
}

function requestPreparedSelfTransferConfirmation(facts) {
  transferFlowState.running = false;
  els.transferSteps.hidden = false;
  els.transferSuccessPanel.hidden = true;
  els.transferFailurePanel.hidden = true;
  els.transferFlowModal.classList.remove("failed");
  showPreparedTransferConfirmationFacts(facts);
  els.cancelTransferFlow.textContent = "취소";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.confirmTransferFlow.hidden = false;
  els.confirmTransferFlow.disabled = false;
  els.confirmTransferFlow.textContent = "Self transaction 승인";
  els.transferModalLead.textContent =
    "Planner가 note를 정리하기 위한 self transaction을 준비했습니다. effect를 확인한 뒤 이 self-transfer를 별도로 승인해 주세요.";
  // A self transaction is still note preparation. The final transfer has not
  // been prepared or approved yet, so keep the first step active.
  setTransferFlowStep("zero", "Self transaction 확인 필요");
  els.confirmTransferFlow.focus();
  return new Promise((resolve) => {
    transferFlowState.resolve = resolve;
  });
}

async function confirmPreparedTransferBeforeBroadcast(
  data,
  {
    session = preparedPrivacySession(data) || beginPrivacySessionOperation(),
    selfTransfer = false,
  } = {},
) {
  let facts;
  try {
    facts = preparedTransferConfirmationFacts(data);
  } catch (error) {
    await markPreparedReservationReplanRequired(
      data,
      error,
      "prepared_transfer_confirmation_facts_invalid",
      { session },
    );
    throw error;
  }
  const confirmed = selfTransfer
    ? await requestPreparedSelfTransferConfirmation(facts)
    : await requestPreparedTransferConfirmation(facts);
  if (!isPrivacySessionCurrent(session)) {
    await replanInvalidatedPreparedReservation(
      data,
      session,
      privacySessionInvalidatedError(),
    );
    return false;
  }
  if (confirmed) return true;
  const error = noBroadcastAttemptError(
    new Error("Prepared transfer was cancelled before wallet signing"),
  );
  await markPreparedReservationReplanRequired(
    data,
    error,
    selfTransfer
      ? "prepared_self_transfer_confirmation_cancelled"
      : "prepared_transfer_confirmation_cancelled",
    { session },
  );
  return false;
}

function finishTransferFlow(
  message,
  success = true,
  { successCopy = "", flowID = null } = {},
) {
  if (flowID !== null && !transferFlowIsCurrent(flowID)) return false;
  const copy = transferFlowState.copy || privacyFlowCopies.transfer;
  transferFlowState.running = false;
  els.transferModalLead.textContent = success ? copy.doneLead : copy.failedLead;
  els.confirmTransferFlow.hidden = true;
  resetTransferConfirmationFacts();
  setTransferFlowStep(success ? "done" : "", success ? "성공" : "실패");
  els.transferFlowModal.classList.toggle("failed", !success);
  if (success) {
    els.transferSuccessTitle.textContent = message || copy.successTitle;
    els.transferSuccessCopy.textContent = successCopy || copy.successCopy;
    els.transferSteps.hidden = true;
    els.transferSuccessPanel.hidden = false;
    els.transferFailurePanel.hidden = true;
  } else {
    els.transferSteps.hidden = false;
    els.transferSuccessPanel.hidden = true;
    els.transferFailureReason.textContent =
      message || "알 수 없는 오류가 발생했습니다.";
    els.transferFailurePanel.hidden = false;
  }
  els.cancelTransferFlow.textContent = "닫기";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.cancelTransferFlow.focus();
  return true;
}

function responseContentLength(response) {
  const raw = response?.headers?.get?.("content-length");
  if (!raw || !/^(0|[1-9][0-9]*)$/.test(raw.trim())) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) ? value : null;
}

function resolvedExpectedResponseUrl(value) {
  if (!value) return null;
  try {
    return new URL(
      String(value),
      globalThis.location?.href || undefined,
    );
  } catch {
    throw new Error("DApp API expected response URL is invalid");
  }
}

function assertDirectApiResponse(response, expectedResponseUrl, label) {
  const expected = resolvedExpectedResponseUrl(expectedResponseUrl);
  if (!expected) return;
  if (response?.redirected === true) {
    throw new Error(`${label} must not redirect`);
  }
  const finalUrl = String(response?.url || "");
  if (!finalUrl) return;
  let actual;
  try {
    actual = new URL(finalUrl);
  } catch {
    throw new Error(`${label} response URL is invalid`);
  }
  if (actual.href !== expected.href) {
    throw new Error(`${label} must be served directly from its configured endpoint`);
  }
}

function apiResponseContentType(response) {
  return String(response?.headers?.get?.("content-type") || "");
}

function assertJsonApiResponse(response, path, label) {
  const contentType = apiResponseContentType(response);
  if (contentType.split(";", 1)[0].trim().toLowerCase() === "application/json") {
    return;
  }
  const error = new Error(`${label} must return Content-Type: application/json`);
  // Preserve the static-host health fallback classification. Only that route
  // may treat an HTML app shell as evidence that no server API is deployed.
  error.apiInvalidJsonResponse = true;
  error.apiPath = String(path);
  error.apiResponseContentType = contentType;
  throw error;
}

async function readBoundedApiResponseText(response, maxResponseBytes) {
  if (!maxResponseBytes) return response.text();
  const declaredLength = responseContentLength(response);
  if (declaredLength !== null && declaredLength > maxResponseBytes) {
    throw new Error(`DApp API response exceeds ${maxResponseBytes} byte limit`);
  }
  if (!response?.body || typeof response.body.getReader !== "function") {
    const text = await response.text();
    if (new TextEncoder().encode(text).byteLength > maxResponseBytes) {
      throw new Error(`DApp API response exceeds ${maxResponseBytes} byte limit`);
    }
    return text;
  }
  const reader = response.body.getReader();
  const chunks = [];
  let total = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      const chunk = value instanceof Uint8Array ? value : new Uint8Array(value);
      total += chunk.byteLength;
      if (total > maxResponseBytes) {
        try {
          await reader.cancel();
        } catch {
          // The oversized response is already rejected; cancellation is best effort.
        }
        throw new Error(`DApp API response exceeds ${maxResponseBytes} byte limit`);
      }
      chunks.push(chunk);
    }
  } finally {
    reader.releaseLock();
  }
  const bytes = new Uint8Array(total);
  let offset = 0;
  for (const chunk of chunks) {
    bytes.set(chunk, offset);
    offset += chunk.byteLength;
  }
  return new TextDecoder().decode(bytes);
}

async function api(path, options = {}) {
  const {
    timeoutMs = dappApiRequestTimeoutMs,
    signal: suppliedSignal,
    expectedResponseUrl = "",
    responseLabel = "DApp API response",
    maxResponseBytes = dappApiResponseMaxBytes,
    ...fetchOptions
  } = options;
  const boundedTimeoutMs = Number(timeoutMs);
  const boundedResponseBytes = Number(maxResponseBytes);
  const timeoutEnabled =
    Number.isSafeInteger(boundedTimeoutMs) && boundedTimeoutMs > 0;
  if (
    boundedResponseBytes !== 0 &&
    (!Number.isSafeInteger(boundedResponseBytes) || boundedResponseBytes < 1)
  ) {
    throw new Error("DApp API maxResponseBytes must be a positive integer");
  }
  const controller =
    timeoutEnabled && typeof AbortController === "function"
      ? new AbortController()
      : null;
  if (timeoutEnabled && !controller) {
    throw new Error("AbortController is required for bounded DApp API requests");
  }
  let timeoutID = null;
  let timedOut = false;
  let removeSuppliedAbortListener = null;
  if (controller && suppliedSignal) {
    const abort = () => controller.abort(suppliedSignal.reason);
    if (suppliedSignal.aborted) {
      abort();
    } else {
      suppliedSignal.addEventListener("abort", abort, { once: true });
      removeSuppliedAbortListener = () =>
        suppliedSignal.removeEventListener("abort", abort);
    }
  }
  if (controller) {
    timeoutID = globalThis.setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, boundedTimeoutMs);
  }
  let response;
  let body;
  try {
    response = await fetch(path, {
      ...fetchOptions,
      ...(controller ? { signal: controller.signal } : { signal: suppliedSignal }),
      headers: {
        accept: "application/json",
        "content-type": "application/json",
        ...(options.headers || {}),
      },
    });
    assertDirectApiResponse(response, expectedResponseUrl, responseLabel);
    // A static host can legitimately return a non-JSON 404 for its absent
    // server API. Preserve that HTTP status for the bootstrap fallback; every
    // successful API response, including remote proof output, must be JSON.
    if (response.ok) {
      assertJsonApiResponse(response, path, responseLabel);
    }
    body = await readBoundedApiResponseText(response, boundedResponseBytes);
  } catch (cause) {
    if (timedOut) {
      const error = new Error(
        `DApp API ${path} timed out after ${boundedTimeoutMs}ms`,
      );
      error.code = "request_timeout";
      error.cause = cause;
      throw error;
    }
    throw cause;
  } finally {
    if (timeoutID !== null) globalThis.clearTimeout(timeoutID);
    removeSuppliedAbortListener?.();
  }
  let data = null;
  if (body) {
    try {
      data = JSON.parse(body);
    } catch {
      data = null;
    }
  }
  if (!response.ok) {
    const error = new ApiError(
      {
        error: data?.error || response.statusText,
        ...(data && typeof data === "object" ? data : {}),
      },
      response.status,
    );
    // A same-origin static host can return an HTML 404 for its absent
    // /api/health route. Keep the response classification on the typed error
    // so bootstrap can distinguish that static-host shape from a reachable
    // JSON health API that returned an invalid response.
    error.apiPath = String(path);
    error.apiResponseContentType = apiResponseContentType(response);
    throw error;
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    const error = new Error(`DApp API ${path} returned an invalid JSON response`);
    error.apiInvalidJsonResponse = true;
    error.apiPath = String(path);
    error.apiResponseContentType = apiResponseContentType(response);
    throw error;
  }
  if (data.error) {
    throw new ApiError(
      {
        error: data.error || response.statusText,
        ...data,
      },
      response.status,
    );
  }
  return data;
}

async function digestText(value) {
  const bytes = new TextEncoder().encode(value);
  const digest = await crypto.subtle.digest("SHA-256", bytes);
  return [...new Uint8Array(digest)]
    .map((byte) => byte.toString(16).padStart(2, "0"))
    .join("");
}

function bytesToHex(bytes) {
  const view =
    bytes instanceof ArrayBuffer
      ? new Uint8Array(bytes)
      : new Uint8Array(bytes || []);
  return [...view].map((byte) => byte.toString(16).padStart(2, "0")).join("");
}

function hexToBytes(value) {
  const hex = String(value || "")
    .trim()
    .replace(/^0x/i, "");
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
  const hex = String(value || "")
    .trim()
    .replace(/^0x/i, "");
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
  if (
    isSendRecipientForWallet(
      recipient,
      state.activeWallet || activeWalletKind(),
    )
  ) {
    return recipient;
  }
  if (isEvmTransparentMode(state.activeWallet || activeWalletKind())) {
    throw new Error("EVM send recipient must be a 0x address.");
  }
  throw new Error(
    `Cosmos send recipient must be a ${accountPrefix()}1... address.`,
  );
}

function requireValidPrivacyWithdrawRecipient(value, label = "Withdraw recipient") {
  const recipient = String(value || "").trim();
  if (isSendRecipientForWallet(recipient, state.activeWallet || activeWalletKind())) {
    return recipient;
  }
  if (isEvmTransparentMode(state.activeWallet || activeWalletKind())) {
    throw new Error(`${label} must be a 0x address.`);
  }
  throw new Error(`${label} must be a ${accountPrefix()}1... address.`);
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
    const estimated = evmQuantityToBigInt(
      await clairveilBrowserClient().evmJsonRpc("eth_estimateGas", [tx]),
      "estimated gas",
    );
    const padded = (estimated * 13n + 9n) / 10n;
    const existing = tx.gas
      ? evmQuantityToBigInt(tx.gas, "transaction gas")
      : 0n;
    tx.gas = bigIntToEvmQuantity(existing > padded ? existing : padded);
    return tx;
  } catch {
    delete tx.gas;
    return tx;
  }
}

function normalizeEvmTxHash(txHash) {
  return String(txHash || "")
    .trim()
    .replace(/^0x/i, "")
    .toUpperCase();
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

function reservationScope() {
  const profile = activeChainProfile();
  return {
    chainId: profile?.chainId || state.config?.chainId || "clairveil",
    profile: profilePersistenceScope(profile),
    wallet: state.activeWallet || activeWalletKind() || "wallet",
    account: state.keplr.account || "",
  };
}

function reservationOwnerKeyId() {
  const scope = reservationScope();
  return `${scope.chainId}:${scope.profile}:${scope.wallet}:${scope.account}`;
}

function reservationNamespace() {
  const scope = reservationScope();
  return `${scope.chainId}:${scope.profile}:${scope.wallet}:${scope.account || "disconnected"}`;
}

function currentReservationWorkerID() {
  if (!reservationWorkerID) {
    if (!globalThis.crypto?.getRandomValues) {
      throw new Error("Web Crypto is required for browser reservation lease ownership");
    }
    const suffix = globalThis.crypto.randomUUID?.() ||
      [...globalThis.crypto.getRandomValues(new Uint8Array(16))]
        .map((value) => value.toString(16).padStart(2, "0"))
        .join("");
    reservationWorkerID = `browser-tab:${suffix}`;
  }
  return reservationWorkerID;
}

function currentNoteReservationManager({ optional = false } = {}) {
  if (!state.keplr.account || !state.keplr.rootSignatureBase64) {
    if (optional) return null;
    throw new Error("Connect a wallet and initialize privacy keys first");
  }

  const storeKey = `${reservationStoreNamespacePrefix}${reservationNamespace()}`;
  if (!reservationStore || reservationStoreKey !== storeKey) {
    const encryption = createEncryptedStateCodec({
      namespace: `reservation:${storeKey}`,
      secretBase64: state.keplr.rootSignatureBase64,
    });
    reservationStore = createBrowserReservationStore({
      namespace: storeKey,
      requireLocks: true,
      encodeState: encryption.encodeState,
      decodeState: encryption.decodeState,
    });
    reservationStoreKey = storeKey;
  }

  const managerKey = JSON.stringify({
    namespace: storeKey,
    ownerKeyId: reservationOwnerKeyId(),
    rootSignatureHash:
      state.keplr.rootSignatureHash || state.keplr.rootSignatureBase64,
  });
  if (!reservationManager || reservationManagerKey !== managerKey) {
    reservationManager = createPrivacySafeReservationManager(
      createNoteReservationManager({
        store: reservationStore,
        ownerKeyId: reservationOwnerKeyId(),
        indexKey: base64ToBytes(state.keplr.rootSignatureBase64),
        nullifierLookupKeyId: "root-signature-v1",
        leaseOwner: currentReservationWorkerID(),
      }),
    );
    reservationManagerKey = managerKey;
  }

  return reservationManager;
}

function currentWalletNoteStore({ optional = false } = {}) {
  if (!state.keplr.account || !state.keplr.rootSignatureBase64) {
    if (optional) return null;
    throw new Error("Connect a wallet and initialize privacy keys first");
  }
  const key = `${walletNoteStoreNamespacePrefix}${reservationNamespace()}`;
  if (walletNoteStore && walletNoteStoreKey === key) return walletNoteStore;
  try {
    walletNoteStore = new EncryptedIndexedDbNoteStore({
      namespace: key,
      owner: reservationOwnerKeyId(),
      secretBase64: state.keplr.rootSignatureBase64,
    });
    walletNoteStoreKey = key;
    return walletNoteStore;
  } catch (error) {
    walletNoteStore = null;
    walletNoteStoreKey = "";
    if (!optional) throw error;
    throw new Error(`Encrypted Clairveil note storage is unavailable: ${error.message}`);
  }
}

function persistedTypedScanCursor(cached) {
  // The encrypted store uses `scanCursor`, but accept the wire spelling while
  // rebuilding a v0.2 browser cache. A three-part cursor is the security
  // boundary; the property spelling is not.
  const cursor = cached?.scanCursor ?? cached?.scan_cursor ?? null;
  return cursor && typeof cursor === "object" ? cursor : null;
}

function hasPersistedTypedScanCursor(cached) {
  const cursor = persistedTypedScanCursor(cached);
  const nextCursor = cursor?.next_cursor ?? cursor?.nextCursor;
  return Boolean(
    cursor &&
      typeof cursor === "object" &&
      // A cursor returned by the typed endpoint is still safe if an older
      // browser record omitted its redundant source property. Never accept a
      // cursor which explicitly identifies a different scan protocol.
      (!cursor.source ||
        cursor.source === "privacy_scan" ||
        cursor.scan_source === "privacy_scan") &&
      nextCursor &&
      typeof nextCursor === "object" &&
      nextCursor.height != null &&
      (nextCursor.global_sequence != null || nextCursor.globalSequence != null) &&
      (nextCursor.output_index != null || nextCursor.outputIndex != null),
  );
}

function applyPersistedWalletNoteState(cached) {
  if (!cached || !hasPersistedTypedScanCursor(cached)) return false;
  const scanCursor = persistedTypedScanCursor(cached);
  applyNoteScanResult({
    notes: cached.notes || [],
    // The encrypted note store commits the entire cursor atomically with the
    // recovered notes. Do not reconstruct a lossy height/sequence resume
    // token in the UI layer.
    scanCursor,
  }, { reset: true });
  return true;
}

async function hydratePersistedWalletNotes({ session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const store = currentWalletNoteStore({ optional: false });
  const cached = await withPrivacySessionGuard(session, () => store.load());
  assertPrivacySessionCurrent(session);
  if (!hasPersistedTypedScanCursor(cached)) {
    await withPrivacySessionGuard(session, () => store.clear());
    assertPrivacySessionCurrent(session);
    return;
  }
  applyPersistedWalletNoteState(cached);
}

function amountInputValue(input) {
  const raw = String(input.value || "")
    .trim()
    .replace(/,/g, "");
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
  const raw = String(input?.value || "")
    .trim()
    .replace(/,/g, "");
  if (!/^(0|[1-9][0-9]*)$/.test(raw)) return false;
  return BigInt(raw) > 0n;
}

function clairInputToUclair(input) {
  const raw = String(input.value || "")
    .trim()
    .replace(/,/g, "");
  const decimals = coinDecimals();
  const pattern = new RegExp(`^(0|[1-9][0-9]*)(\\.[0-9]{0,${decimals}})?$`);
  if (!pattern.test(raw)) {
    throw new Error(
      `${displayDenom()} amount must be a positive number with up to ${decimals} decimals`,
    );
  }

  const [whole, fraction = ""] = raw.split(".");
  const scale = 10n ** BigInt(decimals);
  const paddedFraction = `${fraction}${"0".repeat(decimals)}`.slice(
    0,
    decimals,
  );
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

  const fractionText = fraction
    .toString()
    .padStart(decimals, "0")
    .replace(/0+$/, "");
  return `${whole}.${fractionText} ${displayDenom()}`;
}

function formatBalances(balances) {
  return (
    (balances || [])
      .map((coin) => {
        if (coin.denom === baseDenom()) {
          return `${formatUclairAsClair(coin.amount)} (${coin.amount}${baseDenom()})`;
        }
        return `${coin.amount}${coin.denom}`;
      })
      .join(", ") || `0 ${displayDenom()} (${zeroCoinText()})`
  );
}

function noteAmountValue(note) {
  try {
    return BigInt(String(note?.amount || "0"));
  } catch {
    return 0n;
  }
}

function noteHasVerifiedUnspentNullifier(note) {
  const nullifier = noteNullifier(note);
  const nullifierStatus = String(
    note?.nullifier_status ?? note?.nullifierStatus ?? "",
  ).toLowerCase();
  return (
    /^[0-9a-f]{64}$/.test(nullifier) &&
    nullifierStatus === "unspent" &&
    note?.spent !== true &&
    note?.isSpent !== true
  );
}

function isSpendableNote(note) {
  // The scan's display status is not sufficient authority to spend. Only an
  // explicit, well-formed nullifier response proving the note unspent may
  // expose it as inventory or let the planner consider it.
  return (
    String(note?.status || "").toLowerCase() === "spendable" &&
    noteHasVerifiedUnspentNullifier(note)
  );
}

function noteAssetDenom(note) {
  const supplied = String(note?.asset_denom ?? note?.assetDenom ?? "").trim();
  if (supplied) return supplied;

  // Local signer output is a compact CLI JSON contract. Older local wallet
  // caches may predate its `asset_denom` field, but the NoteV1 asset ID is
  // still authoritative. Recover only the active base denom when its derived
  // ID matches exactly; every other asset remains explicitly unknown.
  return noteAssetIDHex(note) === baseAssetIDHex() ? baseDenom() : "";
}

function noteAssetIDHex(note) {
  const supplied = String(note?.asset_id_hex ?? note?.assetIdHex ?? "")
    .trim()
    .toLowerCase();
  if (/^[0-9a-f]{64}$/.test(supplied)) return supplied;

  const raw = note?.note?.assetID ?? note?.note?.assetId ?? note?.note?.as;
  try {
    const assetID = BigInt(raw);
    if (assetID < 0n || assetID >= (1n << 256n)) return "";
    return assetID.toString(16).padStart(64, "0");
  } catch {
    return "";
  }
}

function baseAssetIDHex() {
  return computeAssetIdV1(baseDenom()).toString(16).padStart(64, "0");
}

function noteUsesCurrentAsset(note) {
  // The browser example currently prepares only the profile's base asset.
  // A verified note for another (or unknown legacy) asset must never be
  // summed or displayed as if it were spendable uclair.
  return noteAssetDenom(note) === baseDenom();
}

function isCurrentAssetSpendableNote(note) {
  return isSpendableNote(note) && noteUsesCurrentAsset(note);
}

function noteNullifier(note) {
  return String(note?.nullifier || note?.nullifier_hex || "")
    .trim()
    .toLowerCase();
}

function normalizedPrivacyTxHash(value) {
  return String(value || "")
    .trim()
    .replace(/^0x/i, "")
    .toUpperCase();
}

function hasRecoveredDepositNote(txHash = state.keplr.depositHash) {
  const expected = normalizedPrivacyTxHash(txHash);
  return Boolean(expected) && state.keplr.notes.some(
    (note) =>
      normalizedPrivacyTxHash(note?.tx_hash ?? note?.txHash) === expected,
  );
}

function noteReservation(note) {
  const nullifier = noteNullifier(note);
  return nullifier
    ? state.keplr.noteReservationByNullifier?.[nullifier] || null
    : null;
}

function noteHasActiveReservation(note) {
  return isActiveReservationStatus(noteReservation(note)?.status);
}

function noteHasConfirmedSpentReservation(note) {
  return (
    noteReservation(note)?.status === reservationStatuses.ConfirmedSpent
  );
}

function noteHasBlockingReservation(note) {
  // ClairveilJS deliberately excludes both active leases and a durable
  // ConfirmedSpent record. Keep the list and its spendable total on that exact
  // rule; otherwise a local cache can label an input Spendable that the
  // transfer planner must refuse.
  return noteHasActiveReservation(note) || noteHasConfirmedSpentReservation(note);
}

function isAvailableSpendableNote(note) {
  return isCurrentAssetSpendableNote(note) && !noteHasBlockingReservation(note);
}

function isUnverifiedNote(note) {
  const status = String(note?.status || "").toLowerCase();
  const nullifierStatus = String(note?.nullifier_status ?? note?.nullifierStatus ?? "").toLowerCase();
  if (
    status === "spent" ||
    nullifierStatus === "spent" ||
    note?.spent === true ||
    note?.isSpent === true
  ) {
    return false;
  }
  return !isSpendableNote(note);
}

function isZeroAmountNote(note) {
  return noteAmountValue(note) === 0n;
}

function isHelperNote(note) {
  return isCurrentAssetSpendableNote(note) && isZeroAmountNote(note);
}

function noteStatusLabel(note) {
  if (noteHasActiveReservation(note)) return "Reserved";
  if (noteHasConfirmedSpentReservation(note)) return "Confirmed spent";
  if (isUnverifiedNote(note)) return "Unverified";
  if (!noteUsesCurrentAsset(note)) return "Other asset";
  return isSpendableNote(note) ? "Spendable" : "Spent";
}

function noteStatusClass() {
  return "note-status";
}

function summarizeSpendableValueNotes(notes) {
  const spendableValueNotes = (notes || []).filter(
    (note) => isAvailableSpendableNote(note) && !isZeroAmountNote(note),
  );
  const helperCount = (notes || []).filter(
    (note) => isAvailableSpendableNote(note) && isZeroAmountNote(note),
  ).length;
  const reservedHelperCount = (notes || []).filter(
    (note) =>
      isCurrentAssetSpendableNote(note) &&
      isZeroAmountNote(note) &&
      noteHasActiveReservation(note),
  ).length;
  const reservedCount = (notes || []).filter(
    (note) =>
      isCurrentAssetSpendableNote(note) &&
      !isZeroAmountNote(note) &&
      noteHasActiveReservation(note),
  ).length;
  const confirmedSpentCount = (notes || []).filter(
    (note) =>
      isCurrentAssetSpendableNote(note) &&
      !isZeroAmountNote(note) &&
      noteHasConfirmedSpentReservation(note),
  ).length;
  const otherAssetCount = (notes || []).filter(
    (note) =>
      isSpendableNote(note) &&
      !noteUsesCurrentAsset(note) &&
      !isZeroAmountNote(note),
  ).length;
  const total = spendableValueNotes.reduce(
    (sum, note) => sum + noteAmountValue(note),
    0n,
  );
  const helperText = helperCount ? ` · ${helperCount} helper` : "";
  const reservedHelperText = reservedHelperCount
    ? ` · ${reservedHelperCount} Reserved helper`
    : "";
  const reservedText = reservedCount ? ` · ${reservedCount} Reserved` : "";
  const confirmedSpentText = confirmedSpentCount
    ? ` · ${confirmedSpentCount} Confirmed spent`
    : "";
  const otherAssetText = otherAssetCount ? ` · ${otherAssetCount} Other asset` : "";
  return `${total}${baseDenom()} / ${spendableValueNotes.length} Spendable${helperText}${reservedHelperText}${reservedText}${confirmedSpentText}${otherAssetText}`;
}

function notesSummarySuffix() {
  const moreEvents = state.keplr.noteScanCursor?.hasMore
    ? " · more events queued"
    : "";
  const unverifiedCount = state.keplr.notes.filter(isUnverifiedNote).length;
  const verificationWarning = unverifiedCount
    ? ` · ${unverifiedCount} pending verification`
    : "";
  return `${moreEvents}${verificationWarning}`;
}

function refreshNotesSummary() {
  state.keplr.notesSummary = `${summarizeSpendableValueNotes(state.keplr.notes)}${notesSummarySuffix()}`;
  // Note scans and reservation reconciliation can update inventory outside a
  // full render cycle. Recompute the batch preview from that exact same
  // filtered note set so its input count cannot lag behind the visible
  // spendable summary.
  renderBatchTransferPreview();
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
    const heightCompare =
      Number(left?.height || 0) - Number(right?.height || 0);
    if (heightCompare !== 0) return heightCompare;
    return String(left?.tx_hash || left?.txHash || "").localeCompare(
      String(right?.tx_hash || right?.txHash || ""),
    );
  });
}

function noteScanRequestOptions({ requireComplete = true } = {}) {
  // `EncryptedIndexedDbNoteStore` owns the durable privacy-scan-v2 cursor,
  // including its validation-state snapshot. Let ClairveilJS resume directly
  // from that atomic record rather than translating it through UI state.
  return {
    scanSource: "privacy_scan",
    limit: 200,
    maxPages: requireComplete
      ? completeNoteScanMaxPages
      : completeNoteScanMaxPages,
  };
}

function resumeTypedNoteScanOptions(cached, defaults) {
  const cursor = persistedTypedScanCursor(cached);
  if (!cursor || typeof cursor !== "object") return {};
  const resume = nextPrivacyScanOptions(cursor, defaults);
  if (resume.scanSource !== "privacy_scan") {
    throw new Error(
      "The encrypted note cache does not contain a privacy-scan-v2 cursor. Reset and rescan notes from the unified endpoint.",
    );
  }
  return {
    after: resume.after,
    limit: resume.limit,
    outputLimit: resume.outputLimit,
    eventLimit: resume.eventLimit,
    maxEncodedBytes: resume.maxEncodedBytes,
    maxPages: resume.maxPages,
    scanSource: resume.scanSource,
    ...(resume.validationStateSnapshot
      ? { validationStateSnapshot: resume.validationStateSnapshot }
      : {}),
  };
}

function applyNoteScanResult(data, { reset = false } = {}) {
  const previous = reset
    ? defaultNoteScanCursor()
    : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const cursor = data?.scanCursor || data?.scan_cursor || {};
  const nextScanOptions = data?.nextScanOptions || data?.next_scan_options || {};
  const hasMore = Boolean(cursor.has_more ?? cursor.hasMore);
  const source = String(
    cursor.source ||
      nextScanOptions.scanSource ||
      nextScanOptions.scan_source ||
      previous.source ||
      "privacy_scan",
  );
  if (source !== "privacy_scan") {
    throw new Error(
      "The configured node did not return privacy-scan-v2; notes were not accepted. Retry after the unified privacy scan endpoint is available.",
    );
  }
  const after = privacyScanCursorPosition(
    cursor.after ?? cursor.afterCursor,
    previous.after || previous.nextCursor,
  );
  const nextCursor = privacyScanCursorPosition(
    cursor.next_cursor ?? cursor.nextCursor,
    after,
  );
  const latest = privacyScanCursorPosition(
    {
      height: cursor.latest_height ?? cursor.latestHeight,
      global_sequence: cursor.latest_sequence ?? cursor.latestSequence,
      output_index: cursor.latest_output_index ?? cursor.latestOutputIndex,
    },
    nextCursor,
  );
  state.keplr.notes = mergeCachedNotes(
    reset ? [] : state.keplr.notes,
    data?.notes || [],
  );
  state.keplr.noteScanCursor = {
    source,
    after,
    nextCursor,
    limit: Number(cursor.output_limit ?? cursor.outputLimit ?? previous.limit ?? 200),
    outputLimit: Number(cursor.output_limit ?? cursor.outputLimit ?? previous.outputLimit ?? 200),
    eventLimit: cursor.event_limit ?? cursor.eventLimit ?? previous.eventLimit ?? 0,
    maxEncodedBytes:
      cursor.max_encoded_bytes ??
      cursor.maxEncodedBytes ??
      previous.maxEncodedBytes ??
      0,
    maxPages: completeNoteScanMaxPages,
    hasMore,
    latestHeight: latest.height,
    latestSequence: latest.globalSequence,
    latestOutputIndex: latest.outputIndex,
    pagesScanned: Number(cursor.pages_scanned ?? cursor.pagesScanned ?? 1),
    completed: Boolean(cursor.completed ?? !hasMore),
  };
  refreshNotesSummary();
  state.keplr.notesScanned = true;
}

async function refreshCachedNoteStatuses({ session = null, noteStore = null } = {}) {
  assertPrivacySessionCurrent(session);
  const candidateNotes = (state.keplr.notes || []).filter(noteNullifier);
  if (!candidateNotes.length) return;

  const client = clairveilBrowserClient();
  const persistedNoteStore = noteStore || currentWalletNoteStore({ optional: false });
  const nullifiers = [...new Set(candidateNotes.map(noteNullifier))];
  const statuses = new Map();
  const batchSize = 1000;
  for (let index = 0; index < nullifiers.length; index += batchSize) {
    const chunk = nullifiers.slice(index, index + batchSize);
    try {
      const result = await withPrivacySessionGuard(
        session,
        () => client.checkNullifiers(chunk),
      );
      for (const nullifier of chunk) {
        if (result instanceof Map && result.has(nullifier)) {
          const used = nullifierUsedFromResponse(result.get(nullifier));
          if (used !== null) statuses.set(nullifier, used);
        }
      }
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      // Only this chunk falls back to individual checks below.
    }
  }

  const missing = nullifiers.filter((nullifier) => !statuses.has(nullifier));
  const concurrency = 8;
  for (let index = 0; index < missing.length; index += concurrency) {
    const chunk = missing.slice(index, index + concurrency);
    await Promise.all(chunk.map(async (nullifier) => {
      try {
        const result = await withPrivacySessionGuard(
          session,
          () => client.checkNullifier(nullifier),
        );
        statuses.set(nullifier, nullifierUsedFromResponse(result));
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        statuses.set(nullifier, null);
      }
    }));
  }

  assertPrivacySessionCurrent(session);
  let changed = false;
  state.keplr.notes = state.keplr.notes.map((note) => {
    const nullifier = noteNullifier(note);
    if (!statuses.has(nullifier)) return note;
    const used = statuses.get(nullifier);
    const next = used === true
      ? { status: "spent", spent: true, isSpent: true, nullifier_status: "spent" }
      : used === false
        ? { status: "spendable", spent: false, isSpent: false, nullifier_status: "unspent" }
        : { status: "unverified", spent: false, isSpent: false, nullifier_status: "unknown" };
    if (
      note.status === next.status &&
      Boolean(note.spent) === next.spent &&
      String(note.nullifier_status || "") === next.nullifier_status
    ) return note;
    changed = true;
    return { ...note, ...next };
  });
  if (changed) refreshNotesSummary();
  await withPrivacySessionGuard(
    session,
    () => persistedNoteStore.setNullifierStatuses(new Map(
      [...statuses.entries()].map(([nullifier, used]) => [
        nullifier,
        used === true ? "spent" : used === false ? "unspent" : "unknown",
      ]),
    )),
  );
  assertPrivacySessionCurrent(session);
}

async function reconcileReservedNotesFromScan(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const manager = currentNoteReservationManager({ optional: true });
  const notes = state.keplr.notes;
  if (!manager) return;
  // An empty or reset note cache is itself a recovery case. It cannot supply
  // spent evidence, but it must not hide active post-broadcast operations
  // from transaction/nullifier recovery and ManualReview escalation.
  if (notes.length) {
    await withPrivacySessionGuard(
      session,
      () => manager.reconcileSpentNotes(notes),
    );
  }
  await reconcileRecoveredActiveReservations(manager, { session });
  assertPrivacySessionCurrent(session);
  await reconcileDefiniteFailedUnknownReservations(manager, { session });
  assertPrivacySessionCurrent(session);
}

function reservationOperationID(reservation = {}) {
  return reservation.operation_id || reservation.reservation_id || "";
}

function reservationHasBroadcastEvidence(reservation = {}) {
  return Boolean(
    reservation.submitted_tx_hash ||
      reservation.tx_bytes_hash ||
      reservation.sign_doc_hash,
  );
}

function reservationBroadcastAttemptCount(reservation = {}) {
  const raw =
    reservation.broadcast_attempt_count ??
    reservation.broadcastAttemptCount ??
    0;
  const count = Number(raw);
  return Number.isSafeInteger(count) && count >= 0 ? count : null;
}

function normalizedReservationTransactionIdentity(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
}

function reservationBroadcastRecoveryEvidence(reservation = {}) {
  return {
    broadcastInFlight:
      reservation.broadcast_in_flight === true ||
      reservation.broadcastInFlight === true,
    broadcastAttemptCount: reservationBroadcastAttemptCount(reservation),
    submittedTxHash: normalizedReservationTransactionIdentity(
      reservation.submitted_tx_hash || reservation.submittedTxHash,
    ),
    txBytesHash: normalizedReservationTransactionIdentity(
      reservation.tx_bytes_hash || reservation.txBytesHash,
    ),
    signDocHash: normalizedReservationTransactionIdentity(
      reservation.sign_doc_hash || reservation.signDocHash,
    ),
  };
}

function reservationHasBroadcastRecoveryEvidence(reservation = {}) {
  const evidence = reservationBroadcastRecoveryEvidence(reservation);
  return (
    evidence.broadcastInFlight ||
    evidence.broadcastAttemptCount === null ||
    evidence.broadcastAttemptCount > 0 ||
    Boolean(
      evidence.submittedTxHash || evidence.txBytesHash || evidence.signDocHash,
    )
  );
}

function operationHasConsistentBroadcastRecoveryEvidence(records = []) {
  if (!records.length) return false;
  const evidence = records.map(reservationBroadcastRecoveryEvidence);
  return (
    evidence.every((entry) => entry.broadcastAttemptCount !== null) &&
    new Set(evidence.map((entry) => JSON.stringify(entry))).size === 1
  );
}

async function quarantineInconsistentOperationBroadcastEvidence(
  manager,
  records,
  { session = beginPrivacySessionOperation() } = {},
) {
  if (typeof manager?.markManualReview !== "function") return [];
  return withPrivacySessionGuard(
    session,
    () => manager.markManualReview(
      records.map((record) => record.reservation_id),
      {
        error: "operation_broadcast_recovery_evidence_inconsistent",
        metadata: {
          reconcile_reason:
            "recovered_inconsistent_operation_broadcast_evidence_requires_manual_review",
        },
      },
    ),
  );
}

function manualReviewResolutionEvidence(record = {}) {
  return JSON.stringify({
    reservationID: record.reservation_id || "",
    operationID: reservationOperationID(record),
    kind: record.kind || "",
    status: record.status || "",
    submittedTxHash: record.submitted_tx_hash || "",
    txBytesHash: record.tx_bytes_hash || "",
    signDocHash: record.sign_doc_hash || "",
    noBroadcast: reservationHasDurableNoBroadcastEvidence(record),
    reconcileReason: record.metadata?.reconcile_reason || "",
    txHashChecked: record.metadata?.tx_hash_checked || "",
    payloadHash:
      record.payload_hash ||
      record.payloadHash ||
      record.metadata?.payload_hash ||
      record.metadata?.payloadHash ||
      "",
    payloadExpiresAtUnix:
      record.metadata?.payload_expires_at_unix ||
      record.metadata?.payloadExpiresAtUnix ||
      record.metadata?.expires_at_unix ||
      record.metadata?.expiresAtUnix ||
      "",
  });
}

const reservationHasDurableNoBroadcastEvidence =
  hasDurableNoBroadcastEvidence;

function reservationUpdatedAtMs(reservation = {}) {
  const value = Date.parse(reservation.updated_at || reservation.created_at || "");
  return Number.isFinite(value) ? value : 0;
}

function reservationHasLiveLease(reservation = {}, nowMs = Date.now()) {
  const leaseUntil = Date.parse(reservation.lease_until || "");
  return Number.isFinite(leaseUntil) && leaseUntil > nowMs;
}

function reservationCanRecoverAfterWorkerExpiry(reservation = {}, manager, nowMs = Date.now()) {
  if (reservationHasLiveLease(reservation, nowMs)) return false;
  const status = reservation.status;
  if (
    status === reservationStatuses.Proving ||
    status === reservationStatuses.ProofReady
  ) {
    return Boolean(reservation.lease_owner) ||
      nowMs - reservationUpdatedAtMs(reservation) >= reservationRecoveryGraceMs;
  }
  if (status !== reservationStatuses.Reserved) return false;
  return nowMs - reservationUpdatedAtMs(reservation) >=
    Math.max(reservationRecoveryGraceMs, Number(manager?.leaseDurationMs || 0));
}

function reservationNeedsManualReviewForMissingTx(reservation = {}, nowMs = Date.now()) {
  const updatedAtMs = reservationUpdatedAtMs(reservation);
  return updatedAtMs > 0 && nowMs - updatedAtMs >= unresolvedReservationManualReviewAgeMs;
}

async function recoveredReservationTxOutcome(
  reservation = {},
  { session = null } = {},
) {
  assertPrivacySessionCurrent(session);
  const isEvm = state.activeWallet === "metamask";
  const candidates = [...new Set([
    String(reservation.submitted_tx_hash || ""),
    isEvm ? "" : String(reservation.tx_bytes_hash || ""),
  ].filter(Boolean))];
  if (!candidates.length) return { checked: false, found: false, failed: false };
  try {
    if (isEvm) {
      const txHash = candidates[0];
      const receipt = await withPrivacySessionGuard(
        session,
        () => clairveilBrowserClient().evmJsonRpc(
          "eth_getTransactionReceipt",
          [`0x${txHash.replace(/^0x/i, "")}`],
        ),
      );
      if (!receipt) return { checked: true, found: false, failed: false };
      const succeeded = hasSuccessfulEvmReceiptStatus(receipt);
      const failed = hasFailedEvmReceiptStatus(receipt);
      return {
        checked: true,
        found: true,
        failed,
        succeeded,
        ambiguous: !succeeded && !failed,
        txHash,
        height: Number.parseInt(String(receipt.blockNumber || "0"), 16) || 0,
      };
    }
    for (const txHash of candidates) {
      const tx = await withPrivacySessionGuard(
        session,
        () => clairveilBrowserClient().waitForTx(txHash, {
          attempts: 1,
          intervalMs: 0,
        }),
      );
      if (!tx) continue;
      const executionOutcome = cosmosTxExecutionOutcome(tx);
      return {
        checked: true,
        found: true,
        failed: executionOutcome === "failed",
        succeeded: executionOutcome === "success",
        ambiguous: executionOutcome === "unknown",
        txHash,
        height: Number(tx.height || tx.tx_response?.height || 0),
      };
    }
    return { checked: true, found: false, failed: false, txHash: candidates[0] };
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    return { checked: false, found: false, failed: false };
  }
}

function successfulTxNullifierConflictIsMature(outcome, records) {
  const cursor = state.keplr.noteScanCursor || {};
  const typedCursor = cursor.nextCursor || cursor.next_cursor || {};
  const scanHeight = Math.max(
    Number(cursor.latestHeight || 0),
    Number(cursor.afterHeight || typedCursor.height || 0),
  );
  const observedAtMs = Math.max(...records.map(reservationUpdatedAtMs), 0);
  return shouldEscalateSuccessfulTxWithUnspentNullifiers({
    txHeight: outcome.height,
    scanHeight,
    observedAtMs,
    graceMs: successfulTxNullifierConflictGraceMs,
  });
}

async function reconcileRecoveredActiveReservations(
  manager,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (
    typeof manager?.listActiveReservations !== "function" ||
    typeof manager?.reservationForNote !== "function" ||
    typeof manager?.markReplanRequired !== "function"
  ) {
    return [];
  }
  const active = await withPrivacySessionGuard(
    session,
    () => manager.listActiveReservations(),
  );
  const recoverable = active.filter((reservation) =>
    reservation.kind !== "relay_withdraw" ||
    [reservationStatuses.Submitted, reservationStatuses.Unknown].includes(
      reservation.status,
    ),
  );
  if (!recoverable.length) return [];

  const recoverableIDs = new Set(
    recoverable.map((reservation) => reservation.reservation_id),
  );
  const nullifierByReservationID = new Map();
  for (const note of state.keplr.notes || []) {
    const reservation = await withPrivacySessionGuard(
      session,
      () => manager.reservationForNote(note),
    );
    if (!reservation || !recoverableIDs.has(reservation.reservation_id)) continue;
    const nullifier = noteNullifier(note);
    if (nullifier) nullifierByReservationID.set(reservation.reservation_id, nullifier);
  }

  const byOperation = new Map();
  for (const reservation of recoverable) {
    const operationID = reservationOperationID(reservation);
    const records = byOperation.get(operationID) || [];
    records.push(reservation);
    byOperation.set(operationID, records);
  }

  const updated = [];
  for (const records of byOperation.values()) {
    const status = records[0]?.status;
    if (!status) continue;
    const ids = records.map((record) => record.reservation_id);
    const first = records[0];
    // Mixed statuses are already unresolved operation evidence. Quarantine
    // them before attempting a nullifier read: a cache gap or unavailable
    // nullifier response must not hide an inconsistent operation from review.
    const operationStatuses = new Set(records.map((record) => record.status));
    if (operationStatuses.size !== 1) {
      if (typeof manager.markManualReview !== "function") continue;
      try {
        updated.push(
          ...(await withPrivacySessionGuard(session, () => manager.markManualReview(ids, {
            error: "recovered_operation_status_mismatch",
            metadata: {
              reconcile_reason:
                "recovered_mixed_operation_status_requires_manual_review",
              operation_statuses: [...operationStatuses].sort().join(","),
            },
          }))),
        );
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        warnReservationBookkeeping(error);
      }
      continue;
    }
    const operationBroadcastEvidenceInconsistent =
      records.some(reservationHasBroadcastRecoveryEvidence) &&
      !operationHasConsistentBroadcastRecoveryEvidence(records);
    if (operationBroadcastEvidenceInconsistent) {
      try {
        updated.push(
          ...(await quarantineInconsistentOperationBroadcastEvidence(
            manager,
            records,
            { session },
          )),
        );
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        warnReservationBookkeeping(error);
      }
      continue;
    }
    const localWorkerState = records.every((record) =>
      [
        reservationStatuses.Reserved,
        reservationStatuses.Proving,
        reservationStatuses.ProofReady,
      ].includes(record.status),
    );
    const workerExpired = records.every((record) =>
      reservationCanRecoverAfterWorkerExpiry(record, manager),
    );
    // An attempt marker is durable evidence that an external boundary may have
    // been crossed. Do not leave an expired operation invisible merely because
    // the encrypted note cache can no longer map every reservation to a
    // nullifier. Check its known transaction identity, then retain the lock in
    // a user-resolvable state until the missing nullifier evidence is restored.
    const activeBroadcastAttempt = records.some(
      (record) => record.broadcast_in_flight === true,
    );
    const nullifiers = records
      .map((record) => nullifierByReservationID.get(record.reservation_id))
      .filter(Boolean);
    if (nullifiers.length !== records.length) {
      const postBroadcastRecoveryPastDeadline =
        [reservationStatuses.Submitted, reservationStatuses.Unknown].includes(
          status,
        ) && records.every(reservationNeedsManualReviewForMissingTx);
      if (
        (
          (localWorkerState && workerExpired && activeBroadcastAttempt) ||
          postBroadcastRecoveryPastDeadline
        ) &&
        typeof manager.markManualReview === "function"
      ) {
        const attemptOutcome = await withPrivacySessionGuard(
          session,
          () => recoveredReservationTxOutcome(first, { session }),
        );
        try {
          updated.push(
            ...(await withPrivacySessionGuard(session, () => manager.markManualReview(ids, {
              error: postBroadcastRecoveryPastDeadline
                ? "recovered_post_broadcast_missing_input_nullifier_evidence"
                : "recovered_broadcast_attempt_missing_input_nullifier_evidence",
              metadata: {
                reconcile_reason:
                  postBroadcastRecoveryPastDeadline
                    ? "recovered_post_broadcast_missing_input_nullifier_requires_manual_review"
                    : "recovered_broadcast_attempt_missing_input_nullifier_requires_manual_review",
                missing_input_nullifier_count: records.length - nullifiers.length,
                ...(postBroadcastRecoveryPastDeadline
                  ? { post_broadcast_status: status }
                  : {}),
                ...(attemptOutcome?.checked
                  ? {
                      tx_hash_checked:
                        attemptOutcome.txHash ||
                        first.submitted_tx_hash ||
                        first.tx_bytes_hash ||
                        "",
                    }
                  : {}),
              },
            }))),
          );
        } catch (error) {
          if (error?.privacySessionInvalidated) throw error;
          warnReservationBookkeeping(error);
        }
      }
      continue;
    }
    const spent = await withPrivacySessionGuard(
      session,
      () => Promise.all(
        nullifiers.map((nullifier) => checkNullifierSpent(nullifier, { session })),
      ),
    );
    if (spent.some((value) => value == null || value)) continue;

    const localPreBroadcast = localWorkerState && records.every(
      (record) =>
        !reservationHasBroadcastEvidence(record),
    );
    const hasProofReady = records.some(
      (record) => record.status === reservationStatuses.ProofReady,
    );
    // markBroadcastAttempting keeps the reservation in ProofReady while it
    // records broadcast_in_flight. After a restart it is not a local proof
    // artifact any more: check the durable transaction identity as well as
    // every input nullifier before making its recovery decision.
    const attemptOutcome = activeBroadcastAttempt
      ? await withPrivacySessionGuard(
          session,
          () => recoveredReservationTxOutcome(first, { session }),
        )
      : null;
    if (canReplanExpiredLocalReservation({
      localPreBroadcast,
      workerExpired,
      hasProofReady,
    })) {
      try {
        updated.push(
          ...(await withPrivacySessionGuard(session, () => manager.markReplanRequired(ids, {
            error: "recovered_local_prepare_lost_before_broadcast",
            metadata: {
              reconcile_reason: "recovered_local_prepare_without_broadcast",
              no_broadcast_attempt: true,
            },
          }))),
        );
        continue;
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        // Fall through to ManualReview when recovery evidence is insufficient.
        warnReservationBookkeeping(error);
      }
    }

    if (
      localWorkerState &&
      workerExpired &&
      hasProofReady &&
      typeof manager.markManualReview === "function"
    ) {
      try {
        updated.push(
          ...(await withPrivacySessionGuard(session, () => manager.markManualReview(ids, {
            error: activeBroadcastAttempt
              ? "recovered_broadcast_attempt_requires_reconciliation"
              : "recovered_proof_ready_without_pre_broadcast_evidence",
            metadata: {
              reconcile_reason: activeBroadcastAttempt
                ? "recovered_broadcast_attempt_requires_manual_review"
                : "recovered_proof_ready_without_durable_pre_broadcast_evidence",
              ...(attemptOutcome?.checked
                ? {
                    tx_hash_checked:
                      attemptOutcome.txHash ||
                      first.submitted_tx_hash ||
                      first.tx_bytes_hash ||
                      "",
                  }
                : {}),
            },
          }))),
        );
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        warnReservationBookkeeping(error);
      }
      continue;
    }

    if (
      records.some((record) => record.status !== status) ||
      ![reservationStatuses.Submitted, reservationStatuses.Unknown].includes(status)
    ) {
      continue;
    }
    const outcome = await withPrivacySessionGuard(
      session,
      () => recoveredReservationTxOutcome(first, { session }),
    );
    if (!outcome.checked) continue;
    if (!outcome.found) {
      if (
        typeof manager.markManualReview === "function" &&
        records.every(reservationNeedsManualReviewForMissingTx)
      ) {
        try {
          updated.push(
            ...(await withPrivacySessionGuard(session, () => manager.markManualReview(ids, {
              error: "recovered_transaction_not_found_before_deadline",
              metadata: {
                reconcile_reason: "recovered_tx_not_found_manual_review",
                tx_hash_checked: outcome.txHash || first.submitted_tx_hash || first.tx_bytes_hash || "",
              },
            }))),
          );
        } catch (error) {
          if (error?.privacySessionInvalidated) throw error;
          warnReservationBookkeeping(error);
        }
      }
      continue;
    }
    if (outcome.succeeded && !successfulTxNullifierConflictIsMature(outcome, records)) {
      continue;
    }
    if (outcome.ambiguous || outcome.succeeded) {
      if (typeof manager.markManualReview === "function") {
        try {
          updated.push(
            ...(await withPrivacySessionGuard(session, () => manager.markManualReview(ids, {
              error: outcome.ambiguous
                ? "recovered_transaction_result_unverified"
                : "recovered_transaction_success_nullifier_conflict",
              metadata: {
                reconcile_reason: outcome.ambiguous
                  ? "recovered_tx_result_code_unverified"
                  : "recovered_tx_success_nullifier_unspent_conflict",
                tx_hash_checked: outcome.txHash || first.submitted_tx_hash || first.tx_bytes_hash || "",
              },
            }))),
          );
        } catch (error) {
          if (error?.privacySessionInvalidated) throw error;
          warnReservationBookkeeping(error);
        }
      }
      continue;
    }
    if (!outcome.failed) continue;
    try {
      updated.push(
        ...(await withPrivacySessionGuard(session, () => manager.markReplanRequired(ids, {
          txHash: first.submitted_tx_hash,
          txBytesHash: first.tx_bytes_hash,
          signDocHash: first.sign_doc_hash,
          nullifierUnspentConfirmed: true,
          txAbsentOrFailedConfirmed: true,
          txHashChecked: first.submitted_tx_hash || first.tx_bytes_hash || true,
          error: "recovered_transaction_failed_nullifiers_unspent",
          metadata: {
            reconcile_reason: "recovered_definite_tx_failure_nullifier_unspent",
            no_broadcast_attempt: false,
          },
        }))),
      );
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      warnReservationBookkeeping(error);
    }
  }
  if (updated.length) await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function reconcileDefiniteFailedUnknownReservations(
  manager,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (
    typeof manager?.listActiveReservations !== "function" ||
    typeof manager?.reservationForNote !== "function" ||
    typeof manager?.markReplanRequired !== "function"
  ) {
    return [];
  }
  const active = await withPrivacySessionGuard(
    session,
    () => manager.listActiveReservations(),
  );
  const definiteFailureReasons = new Set([
    "cosmos_tx_code_failed",
    "evm_receipt_failed",
  ]);
  const pendingUnknown = active.filter(
    (reservation) =>
      reservation.status === reservationStatuses.Unknown &&
      definiteFailureReasons.has(
        reservation.metadata?.definite_execution_failure,
      ),
  );
  if (!pendingUnknown.length) return [];

  const activeByID = new Map(active.map((reservation) => [reservation.reservation_id, reservation]));
  const nullifierByReservationID = new Map();
  for (const note of state.keplr.notes || []) {
    const reservation = await withPrivacySessionGuard(
      session,
      () => manager.reservationForNote(note),
    );
    if (!reservation || !activeByID.has(reservation.reservation_id)) continue;
    const nullifier = noteNullifier(note);
    if (nullifier) nullifierByReservationID.set(reservation.reservation_id, nullifier);
  }

  const unknownOperationIDs = new Set(
    pendingUnknown.map((reservation) => reservation.operation_id || reservation.reservation_id),
  );
  const updated = [];
  for (const operationID of unknownOperationIDs) {
    const operationReservations = active.filter(
      (reservation) => (reservation.operation_id || reservation.reservation_id) === operationID,
    );
    const failureReason =
      operationReservations[0]?.metadata?.definite_execution_failure || "";
    if (
      !operationReservations.length ||
      !definiteFailureReasons.has(failureReason) ||
      operationReservations.some(
        (reservation) =>
          reservation.status !== reservationStatuses.Unknown ||
          reservation.metadata?.definite_execution_failure !== failureReason,
      )
    ) {
      continue;
    }
    const first = operationReservations[0];
    // The persisted failure marker records what a prior browser session
    // observed, but it is not sufficient restart evidence on its own. A
    // transaction may have been replaced, reorged, or the original receipt
    // may no longer be available. Re-read the known transaction identity
    // before releasing these input notes for a new plan.
    const outcome = await withPrivacySessionGuard(
      session,
      () => recoveredReservationTxOutcome(first, { session }),
    );
    if (!outcome.checked || !outcome.found || !outcome.failed) continue;
    const nullifiers = operationReservations
      .map((reservation) => nullifierByReservationID.get(reservation.reservation_id))
      .filter(Boolean);
    if (nullifiers.length !== operationReservations.length) continue;
    const spentResults = await withPrivacySessionGuard(
      session,
      () => Promise.all(
        nullifiers.map((nullifier) => checkNullifierSpent(nullifier, { session })),
      ),
    );
    if (spentResults.some((spent) => spent == null || spent)) continue;
    try {
      const replanned = await withPrivacySessionGuard(
        session,
        () => manager.markReplanRequired(
          operationReservations.map((reservation) => reservation.reservation_id),
          {
            txHash: first.submitted_tx_hash,
            txBytesHash: first.tx_bytes_hash,
            signDocHash: first.sign_doc_hash,
            nullifierUnspentConfirmed: true,
            txAbsentOrFailedConfirmed: true,
            txHashChecked:
              outcome.txHash || first.submitted_tx_hash || first.tx_bytes_hash || true,
            error: "transaction_execution_failed",
            metadata: {
              reconcile_reason: `${failureReason}_later_nullifier_reconcile`,
              no_broadcast_attempt: false,
            },
          },
        ),
      );
      updated.push(...replanned);
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      warnReservationBookkeeping(error);
    }
  }
  if (updated.length) await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function refreshNoteReservationState(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager) {
    state.keplr.noteReservationByNullifier = {};
    state.keplr.reservationRecordByID = {};
    state.keplr.manualReviewReservations = [];
    refreshNotesSummary();
    return;
  }

  const active = typeof manager.listActiveReservations === "function"
    ? await withPrivacySessionGuard(
        session,
        () => manager.listActiveReservations(),
      )
    : [];
  cacheReservationRecords(active, { replace: true });
  state.keplr.manualReviewReservations = active.filter(
    (reservation) =>
      reservation.kind !== "relay_withdraw" &&
      reservation.status === reservationStatuses.ManualReview,
  );
  const notes = state.keplr.notes;
  const statusByNote = notes.length
    ? await withPrivacySessionGuard(
        session,
        () => manager.reservationStatusByNote(notes),
      )
    : new Map();
  const byNullifier = {};
  for (const [nullifier, reservation] of statusByNote.entries()) {
    if (
      isActiveReservationStatus(reservation?.status) ||
      reservation?.status === reservationStatuses.ConfirmedSpent
    ) {
      byNullifier[nullifier] = reservation;
    }
  }
  state.keplr.noteReservationByNullifier = byNullifier;
  refreshNotesSummary();
}

function reservationHasQueryableTransactionIdentity(reservation = {}) {
  return Boolean(
    String(
      reservation.submitted_tx_hash || reservation.tx_bytes_hash || "",
    ).trim(),
  );
}

function manualReviewRequiresExplicitReservationCancellation(records = []) {
  // A sign-doc hash identifies the wallet request, not an on-chain
  // transaction. If no transaction hash or signed transaction bytes were
  // retained, the chain cannot prove whether the request was abandoned.
  return (
    records.length > 0 &&
    records.every(
      (record) =>
        !reservationHasDurableNoBroadcastEvidence(record) &&
        !reservationHasQueryableTransactionIdentity(record),
    )
  );
}

async function resolveGeneralManualReviewOperation(
  operationID,
  {
    session = beginPrivacySessionOperation(),
    allowExplicitUntrackedCancellation = false,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const operatorId = state.keplr.account;
  if (!operatorId) {
    throw new Error("Connect the reviewing wallet before resolving this reservation");
  }
  const manager = currentNoteReservationManager();
  if (
    typeof manager.listActiveReservations !== "function" ||
    typeof manager.resolveManualReview !== "function"
  ) {
    throw new Error("This SDK does not support ManualReview resolution");
  }
  const active = await withPrivacySessionGuard(
    session,
    () => manager.listActiveReservations(),
  );
  const records = active.filter(
    (reservation) =>
      reservation.kind !== "relay_withdraw" &&
      reservationOperationID(reservation) === operationID,
  );
  if (
    !records.length ||
    records.some(
      (reservation) => reservation.status !== reservationStatuses.ManualReview,
    )
  ) {
    throw new Error("The operation is no longer waiting for manual review");
  }

  const nullifiers = [];
  const notes = [...(state.keplr.notes || [])];
  for (const note of notes) {
    const reservation = await withPrivacySessionGuard(
      session,
      () => manager.reservationForNote(note),
    );
    if (
      reservation &&
      records.some((record) => record.reservation_id === reservation.reservation_id)
    ) {
      const nullifier = noteNullifier(note);
      if (nullifier) nullifiers.push(nullifier);
    }
  }
  if (nullifiers.length !== records.length) {
    throw new Error("Scan Notes before resolving this reservation");
  }

  // The initial list is only used to identify the operation inputs. Read its
  // authoritative records again before reviewing chain evidence so another
  // tab cannot turn an earlier snapshot into an approval for a newer state.
  const reviewRecords = await latestReservationRecords(
    { reservation_ids: records.map((record) => record.reservation_id) },
    { session },
  );
  if (
    reviewRecords.length !== records.length ||
    reviewRecords.some(
      (record) =>
        record.kind === "relay_withdraw" ||
        reservationOperationID(record) !== operationID ||
        record.status !== reservationStatuses.ManualReview,
    )
  ) {
    throw new Error("The operation changed while its ManualReview evidence was loading");
  }

  // Read the operation a second time before the final chain checks. The
  // checks below must apply to the durable records that will be transitioned,
  // not to the earlier discovery snapshot.
  const finalRecords = await latestReservationRecords(
    { reservation_ids: reviewRecords.map((record) => record.reservation_id) },
    { session },
  );
  const reviewedEvidenceByID = new Map(
    reviewRecords.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    finalRecords.length !== reviewRecords.length ||
    finalRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !== operationID ||
        reviewedEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    throw new Error("Reservation evidence changed during ManualReview. Refresh Notes and review it again.");
  }

  // Read the durable operation immediately before its final external
  // evidence. The manager's CAS is a backstop; do not attach an operator
  // approval to changed operation evidence.
  const transitionRecords = await latestReservationRecords(
    { reservation_ids: finalRecords.map((record) => record.reservation_id) },
    { session },
  );
  const finalEvidenceByID = new Map(
    finalRecords.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    transitionRecords.length !== finalRecords.length ||
    transitionRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !== operationID ||
        finalEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    throw new Error("Reservation evidence changed during ManualReview. Refresh Notes and review it again.");
  }
  // Recheck all external evidence against the final durable reservation
  // snapshot, then request ReplanRequired without another asynchronous gap.
  const noBroadcast = transitionRecords.every(
    reservationHasDurableNoBroadcastEvidence,
  );
  const explicitUntrackedCancellation =
    allowExplicitUntrackedCancellation &&
    manualReviewRequiresExplicitReservationCancellation(transitionRecords);
  if (
    allowExplicitUntrackedCancellation &&
    !explicitUntrackedCancellation
  ) {
    throw new Error(
      "The reservation now has broadcast evidence and cannot be explicitly cancelled",
    );
  }
  const [spent, transactionOutcomes] = await withPrivacySessionGuard(
    session,
    () =>
      Promise.all([
        Promise.all(
          nullifiers.map((nullifier) => checkNullifierSpent(nullifier, { session })),
        ),
        noBroadcast || explicitUntrackedCancellation
          ? Promise.resolve([])
          : Promise.all(
              transitionRecords.map((record) =>
                recoveredReservationTxOutcome(record, { session }),
              ),
            ),
      ]),
  );
  if (spent.some((value) => value !== false)) {
    throw new Error(
      "Every nullifier must be explicitly confirmed unspent before re-planning",
    );
  }

  let resolutionReason = "operator confirmed no broadcast and unspent nullifiers";
  if (explicitUntrackedCancellation) {
    resolutionReason =
      "operator explicitly cancelled untracked wallet request after confirming unspent nullifiers";
  } else if (!noBroadcast) {
    const previouslyAgedOut = transitionRecords.every(
      (record) =>
        record.metadata?.reconcile_reason ===
        "recovered_tx_not_found_manual_review",
    );
    if (
      transactionOutcomes.some(
        (outcome) =>
          !outcome.checked ||
          outcome.succeeded ||
          outcome.ambiguous ||
          (outcome.found && !outcome.failed) ||
          (!outcome.found && !previouslyAgedOut),
      )
    ) {
      throw new Error(
        "Transaction absence or failure is not confirmed; keep this operation in ManualReview",
      );
    }
    resolutionReason = transactionOutcomes.every((outcome) => outcome.failed)
      ? "operator confirmed failed transaction and unspent nullifiers"
      : "operator reconfirmed aged-out transaction absence and unspent nullifiers";
  }
  // Nullifier and transaction checks are asynchronous. Read the durable
  // operation once more after those checks so an approval cannot transition a
  // still-ManualReview operation whose evidence changed in another tab while
  // the external evidence was loading.
  const resolutionRecords = await latestReservationRecords(
    { reservation_ids: transitionRecords.map((record) => record.reservation_id) },
    { session },
  );
  const transitionEvidenceByID = new Map(
    transitionRecords.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    resolutionRecords.length !== transitionRecords.length ||
    resolutionRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !== operationID ||
        transitionEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    throw new Error("Reservation evidence changed during ManualReview. Refresh Notes and review it again.");
  }
  // The session guard handles normal wallet change events. Retain the
  // approving account identity as a final independent check so an approval
  // can never be attached to a different account if a provider update arrives
  // outside that event path.
  if (state.keplr.account !== operatorId) {
    throw privacySessionInvalidatedError();
  }

  await withPrivacySessionGuard(
    session,
    () => manager.resolveManualReview(
      resolutionRecords.map((record) => record.reservation_id),
      {
        target: reservationStatuses.ReplanRequired,
        operatorId,
        approvalReference: `dapp-review:${operationID}:${Date.now()}`,
        reason: resolutionReason,
        metadata: explicitUntrackedCancellation
          ? {
              explicit_untracked_wallet_request_cancellation: true,
              input_nullifiers_unspent_confirmed: true,
            }
          : {},
      },
    ),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
  toast(
    explicitUntrackedCancellation
      ? "The unresolved wallet request was cancelled locally. The notes can be planned again."
      : "Reservation review resolved. The notes can be planned again.",
  );
  return { explicitUntrackedCancellation };
}

function selectedLocalAccount() {
  const accounts = activeServerAccounts();
  return (
    accounts.find((account) => account.name === state.selectedAccount) ||
    accounts[0]
  );
}

function localRelayerAccount() {
  if (!serverFeature("relayer")) return null;
  return (
    state.accounts.find((account) => account.name === "relayer") ||
    state.accounts.find((account) => account.name === "dev0") ||
    state.accounts[0] ||
    null
  );
}

function relayPayloadText(payload) {
  return payload ? JSON.stringify(payload, null, 2) : "";
}

function formatRelayPayloadExpiry(value) {
  if (value === undefined || value === null || value === "") {
    return "";
  }
  const raw = typeof value === "bigint" ? value.toString() : String(value);
  const seconds = Number(raw);
  if (!Number.isFinite(seconds)) {
    return raw;
  }
  const date = new Date(seconds * 1000);
  if (Number.isNaN(date.getTime())) {
    return raw;
  }
  return `${raw} (${date.toISOString()})`;
}

function relayWithdrawPendingPayloadID(item = {}) {
  return (
    item.id ||
    item.payloadHash ||
    item.payload?.payload_hash ||
    item.payload?.payloadHash ||
    relayReservationIDs(item.reservation).join(":") ||
    `pending-${(relayPendingPayloadSequence += 1)}`
  );
}

function pendingRelayWithdrawPayloadIsCurrent(id, snapshot) {
  return (state.keplr.relayWithdrawPendingPayloads || []).some(
    (item) =>
      relayWithdrawPendingPayloadID(item) === id &&
      item === snapshot,
  );
}

// Pending handoff actions can finish in a different order from the order in
// which the user started them. Update only the exact entry that was read: an
// equal payload ID is not enough, because expiry recovery may have replaced
// that entry with newer reservation evidence while an action was awaiting.
function replacePendingRelayWithdrawPayload(
  id,
  replacement,
  { expectedSnapshot = null } = {},
) {
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  let replaced = false;
  const next = [];
  for (const item of pending) {
    if (
      relayWithdrawPendingPayloadID(item) !== id ||
      (expectedSnapshot !== null && item !== expectedSnapshot)
    ) {
      next.push(item);
      continue;
    }
    replaced = true;
    if (replacement && relaySnapshotNeedsPendingRecovery(replacement)) {
      next.push(replacement);
    }
  }
  if (replaced) {
    state.keplr.relayWithdrawPendingPayloads = next;
  }
  return replaced;
}

function currentRelayWithdrawMetadataStore() {
  if (!state.keplr.account || !state.keplr.rootSignatureBase64) return null;
  const key = `${relayWithdrawPayloadStoragePrefix}${reservationNamespace()}`;
  if (relayMetadataStore && relayMetadataStoreKey === key) return relayMetadataStore;
  relayMetadataStore = createEncryptedBrowserMetadataStore({
    namespace: key,
    secretBase64: state.keplr.rootSignatureBase64,
  });
  relayMetadataStoreKey = key;
  return relayMetadataStore;
}

function currentBatchTransferArtifactStore() {
  if (!state.keplr.account || !state.keplr.rootSignatureBase64) return null;
  const key =
    `${batchTransferArtifactStoragePrefix}${reservationNamespace()}`;
  if (
    batchTransferArtifactStore &&
    batchTransferArtifactStoreKey === key
  ) {
    return batchTransferArtifactStore;
  }
  batchTransferArtifactStore = createEncryptedBrowserMetadataStore({
    namespace: key,
    secretBase64: state.keplr.rootSignatureBase64,
  });
  batchTransferArtifactStoreKey = key;
  return batchTransferArtifactStore;
}

function batchTransferArtifactReservation(artifact = {}) {
  return artifact.reservation || artifact.context?.reservation || null;
}

function batchTransferArtifactPayments(artifact = {}) {
  const payments = Array.isArray(artifact.payments) ? artifact.payments : [];
  const denom = baseDenom();
  return payments.map((payment, index) => {
    const amount = String(payment?.amount || "");
    if (!amount.endsWith(denom)) {
      throw new Error("Recovered batch payment denom does not match the active profile");
    }
    const rawAmount = amount.slice(0, -denom.length);
    if (!/^[1-9][0-9]*$/.test(rawAmount)) {
      throw new Error("Recovered batch payment amount is invalid");
    }
    const itemId = String(
      payment?.itemId ?? payment?.item_id ?? `batch-payment-${index + 1}`,
    );
    const recipient = String(
      payment?.recipient ??
        payment?.recipientAddress ??
        payment?.recipient_address ??
        "",
    );
    if (!itemId || !isConfiguredShieldedAddress(recipient)) {
      throw new Error("Recovered batch payment identity is invalid");
    }
    return {
      ...payment,
      itemId,
      recipient,
      amount,
      rawAmount,
      amountValue: BigInt(rawAmount),
    };
  });
}

function batchTransferArtifactDisclosure(payment = {}) {
  const mode = String(
    payment.userDisclosureMode ??
      payment.user_disclosure_mode ??
      payment.disclosureMode ??
      payment.disclosure_mode ??
      "none",
  );
  return {
    disclosureMode:
      mode === "recipient-encrypted"
        ? "recipient-encrypted"
        : mode === "public"
          ? "public"
          : "private",
    disclosureTargetHex: String(
      payment.userDisclosureTargetPubKeyHex ??
        payment.user_disclosure_target_pubkey_hex ??
        payment.disclosurePubKeyHex ??
        payment.disclosure_pubkey_hex ??
        "",
    ),
  };
}

function restoreBatchTransferArtifactRows(payments, evidence) {
  if (!batchTransferFeatureEnabled() || !els.batchTransferRows) return;
  els.batchTransferRows.textContent = "";
  completedBatchTransferItemIDs.clear();
  for (const payment of payments) {
    addBatchTransferRow({
      itemId: payment.itemId,
      recipient: payment.recipient,
      amount: payment.rawAmount,
      evidence,
      ...batchTransferArtifactDisclosure(payment),
    });
  }
  batchTransferExpanded = true;
  renderBatchTransferVisibility();
}

function batchTransferArtifactRecordState(
  records,
  expectedCount,
  expectedOperationEvidenceHash,
) {
  if (records.length !== expectedCount || expectedCount < 1) return "active";
  if (
    records.every(
      (record) => record.status === reservationStatuses.ConfirmedSpent,
    )
  ) {
    return batchTransferReservationsSucceeded(records, {
      expectedCount,
      expectedOperationEvidenceHash,
    })
      ? "confirmed"
      // Input nullifiers are already terminally spent, so retaining only a
      // stale local batch artifact cannot protect funds and must not prevent
      // a later batch from using unrelated notes. Keep the payment outcome
      // unverified, but clear this artifact without claiming success.
      : "spent-evidence-conflict";
  }
  const abandoned = new Set([
    reservationStatuses.Released,
    reservationStatuses.ReplanRequired,
    reservationStatuses.Failed,
  ]);
  if (records.every((record) => abandoned.has(record.status))) {
    return "abandoned";
  }
  const terminal = new Set([
    ...abandoned,
    reservationStatuses.ConfirmedSpent,
  ]);
  return records.every((record) => terminal.has(record.status))
    ? "conflict"
    : "active";
}

async function reconcileBatchTransferArtifact(
  {
    session = beginPrivacySessionOperation(),
    restoreUi = false,
    notify = false,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentBatchTransferArtifactStore();
  if (!storage) return "none";
  const artifact = await withPrivacySessionGuard(session, () => storage.load());
  assertPrivacySessionCurrent(session);
  if (!artifact) return "none";
  const reservation = batchTransferArtifactReservation(artifact);
  const ids = reservationIDs(reservation);
  if (!ids.length) {
    throw new Error(
      "An encrypted batch checkpoint exists without reservation identity. Keep the operation locked and review local storage recovery before preparing another batch.",
    );
  }
  const payments = batchTransferArtifactPayments(artifact);
  if (!payments.length) {
    throw new Error("Recovered batch checkpoint has no payment records");
  }
  const records = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  const recordState = batchTransferArtifactRecordState(
    records,
    ids.length,
    artifact.operationEvidenceHash,
  );

  if (recordState === "active") {
    const manualReview = records.every(
      (record) => record.status === reservationStatuses.ManualReview,
    );
    if (restoreUi) {
      restoreBatchTransferArtifactRows(
        payments,
        manualReview ? "Pending review" : "Pending evidence",
      );
      els.batchTransferState.textContent =
        manualReview
          ? "Recovered batch proof needs reservation review"
          : "Recovered batch; reservation reconciliation required";
    }
    return manualReview ? "manual-review" : "active";
  }
  if (recordState === "spent-evidence-conflict") {
    if (restoreUi) {
      restoreBatchTransferArtifactRows(
        payments,
        "Input spent · outcome evidence unavailable",
      );
      els.batchTransferState.textContent =
        "Previous batch inputs are spent; payment evidence needs review";
    }
    const txHash = records
      .map((record) => String(record.submitted_tx_hash || ""))
      .find(Boolean);
    if (txHash) state.keplr.batchTransferHash = txHash;
    await withPrivacySessionGuard(session, () => storage.clear());
    assertPrivacySessionCurrent(session);
    if (notify) {
      showNotice({
        title: "Batch recovery needs evidence review",
        message:
          "Every input note of a previous batch is confirmed spent, but its local payment-output evidence does not match. The stale checkpoint was cleared so unrelated notes are no longer blocked; verify that batch's transaction record before treating its payments as completed.",
        failed: true,
      });
    }
    return "spent-evidence-conflict";
  }
  if (recordState === "conflict") {
    if (restoreUi) {
      restoreBatchTransferArtifactRows(payments, "Pending review");
      els.batchTransferState.textContent =
        "Recovered batch has conflicting operation evidence";
    }
    throw new Error(
      "Recovered atomic batch has conflicting terminal reservation or operation evidence and requires manual review",
    );
  }
  if (recordState === "confirmed") {
    if (restoreUi) {
      restoreBatchTransferArtifactRows(payments, "Verifying");
    }
    renderVerifiedBatchTransferEvidence(
      { operationEvidence: artifact.operationEvidence },
      payments,
    );
    markBatchTransferItemsCompleted(
      payments.map((payment) => payment.itemId),
    );
    const txHash = records
      .map((record) => String(record.submitted_tx_hash || ""))
      .find(Boolean);
    if (txHash) state.keplr.batchTransferHash = txHash;
    els.batchTransferState.textContent = "Recovered atomic batch verified";
    await withPrivacySessionGuard(session, () => storage.clear());
    assertPrivacySessionCurrent(session);
    if (notify) {
      showNotice({
        title: "Batch transfer reconciled",
        message:
          "The atomic batch is confirmed spent and every payment output evidence record matches its prepared item.",
      });
    }
    return "confirmed";
  }

  if (restoreUi) {
    restoreBatchTransferArtifactRows(payments, "Not submitted");
    els.batchTransferState.textContent =
      "Previous batch ended before submission; ready to reprepare";
  }
  await withPrivacySessionGuard(session, () => storage.clear());
  assertPrivacySessionCurrent(session);
  return "abandoned";
}

async function assertNoUnresolvedBatchTransferArtifact(
  { session = beginPrivacySessionOperation() } = {},
) {
  if (!currentBatchTransferArtifactStore()) {
    throw new Error("Encrypted batch artifact storage is unavailable");
  }
  const state = await reconcileBatchTransferArtifact({ session });
  if (
    ["none", "confirmed", "spent-evidence-conflict", "abandoned"].includes(
      state,
    )
  ) {
    return;
  }
  const error = new Error(
    "A checkpointed batch is still unresolved. Refresh Notes and resolve its reservation before preparing another batch.",
  );
  const storage = currentBatchTransferArtifactStore();
  const artifact = await withPrivacySessionGuard(session, () => storage.load());
  assertPrivacySessionCurrent(session);
  const reservation = batchTransferArtifactReservation(artifact || {});
  const reservationIDsForArtifact = reservationIDs(reservation);
  const records = reservationIDsForArtifact.length
    ? await latestReservationRecords(reservation, { session })
    : [];
  assertPrivacySessionCurrent(session);
  const operationID =
    records.length &&
    records.every(
      (record) => reservationOperationID(record) === reservationOperationID(records[0]),
    )
      ? reservationOperationID(records[0])
      : "";
  // The checkpoint has no queryable transaction identity, but it may have
  // survived a browser interruption after a wallet interaction.  Never
  // release it automatically: pass the exact operation to the explicit
  // cancellation dialog, which rechecks every input nullifier first.
  if (
    operationID &&
    records.length === reservationIDsForArtifact.length
  ) {
    if (state === "manual-review") {
      error.batchTransferManualReview = { operationID, records };
    } else if (
      state === "active" &&
      records.every(
        (record) => record.status === reservationStatuses.ProofReady,
      )
    ) {
      error.batchTransferArtifactReview = {
        operationID,
        records,
        reservation,
      };
    } else if (
      state === "active" &&
      records.every((record) =>
        [
          reservationStatuses.Proving,
          reservationStatuses.Submitted,
          reservationStatuses.Unknown,
        ].includes(record.status),
      ) &&
      records.every(
        (record) => !reservationHasQueryableTransactionIdentity(record),
      )
    ) {
      error.batchTransferStalledArtifactReview = {
        operationID,
        records,
        reservation,
      };
    }
  }
  throw error;
}

async function moveUnresolvedBatchTransferArtifactToManualReview(
  reservation,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const records = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  if (
    !records.length ||
    records.some(
      (record) =>
        ![
          reservationStatuses.Proving,
          reservationStatuses.Submitted,
          reservationStatuses.Unknown,
        ].includes(record.status),
    )
  ) {
    throw new Error(
      "The unresolved batch reservation changed. Refresh Notes and review its current state before cancelling it.",
    );
  }
  if (records.some(reservationHasQueryableTransactionIdentity)) {
    throw new Error(
      "The unresolved batch has a queryable transaction identity and cannot be cancelled as an untracked checkpoint",
    );
  }
  await markReservationBatchManualReview(
    reservation,
    new Error("operator requested cancellation of an unresolved untracked batch"),
    "operator_cancelled_untracked_batch_checkpoint",
    {},
    { session },
  );
  const reviewed = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  if (
    reviewed.length !== records.length ||
    reviewed.some(
      (record) => record.status !== reservationStatuses.ManualReview,
    )
  ) {
    throw new Error(
      "The unresolved batch could not enter ManualReview. Refresh Notes and keep its reservation locked.",
    );
  }
  return reviewed;
}

async function discardPreparedBatchTransferArtifact(
  reservation,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const reservationIDsForArtifact = reservationIDs(reservation);
  if (!reservationIDsForArtifact.length) {
    throw new Error("The recovered batch checkpoint has no reservation identity");
  }
  const manager = currentNoteReservationManager();
  const records = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  if (
    records.length !== reservationIDsForArtifact.length ||
    records.some(
      (record) => record.status !== reservationStatuses.ProofReady,
    )
  ) {
    throw new Error(
      "The recovered batch reservation changed. Refresh Notes and review its current state before cancelling it.",
    );
  }
  if (
    records.some(
      (record) => {
        const broadcast = reservationBroadcastRecoveryEvidence(record);
        // A sign-doc hash only identifies the wallet request. It is not a
        // queryable chain transaction and is precisely the case where an
        // explicit user cancellation is needed. Actual broadcast evidence
        // stays locked for normal chain reconciliation.
        return (
          reservationHasQueryableTransactionIdentity(record) ||
          broadcast.broadcastInFlight ||
          broadcast.broadcastAttemptCount === null ||
          broadcast.broadcastAttemptCount > 0
        );
      },
    )
  ) {
    throw new Error(
      "The recovered batch has broadcast evidence and cannot be discarded as an unsubmitted checkpoint",
    );
  }

  const nullifiers = [];
  for (const note of state.keplr.notes || []) {
    const noteReservationRecord = await withPrivacySessionGuard(
      session,
      () => manager.reservationForNote(note),
    );
    if (
      noteReservationRecord &&
      reservationIDsForArtifact.includes(noteReservationRecord.reservation_id)
    ) {
      const nullifier = noteNullifier(note);
      if (nullifier) nullifiers.push(nullifier);
    }
  }
  if (nullifiers.length !== records.length) {
    throw new Error("Scan Notes before cancelling this batch checkpoint");
  }
  const spent = await withPrivacySessionGuard(
    session,
    () =>
      Promise.all(
        nullifiers.map((nullifier) => checkNullifierSpent(nullifier, { session })),
      ),
  );
  if (spent.some((value) => value !== false)) {
    throw new Error(
      "Every nullifier must be explicitly confirmed unspent before cancelling this batch checkpoint",
    );
  }

  const finalRecords = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  const evidenceByID = new Map(
    records.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    finalRecords.length !== records.length ||
    finalRecords.some(
      (record) =>
        record.status !== reservationStatuses.ProofReady ||
        evidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    throw new Error(
      "Reservation evidence changed while cancelling the batch checkpoint. Refresh Notes and review it again.",
    );
  }
  await markReservationBatchReplanRequired(
    reservation,
    new Error("operator cancelled an unresolved unsubmitted batch checkpoint"),
    "operator_cancelled_unsubmitted_batch_checkpoint",
    {
      metadata: {
        explicit_untracked_wallet_request_cancellation: true,
        input_nullifiers_unspent_confirmed: true,
      },
      session,
    },
  );
  await clearBatchTransferArtifact({ session });
  assertPrivacySessionCurrent(session);
  return true;
}

async function saveBatchTransferArtifact(
  artifact,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentBatchTransferArtifactStore();
  if (!storage) {
    throw new Error("Encrypted batch artifact storage is unavailable");
  }
  await withPrivacySessionGuard(
    session,
    () => storage.save({
      version: "clairveil-dapp-batch-artifact-v1",
      ...artifact,
      savedAt: new Date().toISOString(),
    }),
  );
  assertPrivacySessionCurrent(session);
}

async function clearBatchTransferArtifact(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentBatchTransferArtifactStore();
  if (!storage) return;
  await withPrivacySessionGuard(session, () => storage.clear());
  assertPrivacySessionCurrent(session);
}

// `prepareTransferBatch` persists the private payload before it asks the
// prover to work.  If that phase fails, ClairveilJS intentionally keeps a
// ManualReview reservation rather than silently losing the recoverable
// artifact.  This helper turns that durable fact into the exact review dialog
// for this batch.  It is only called from the preparation catch below, before
// any wallet-signing or broadcast function has been reached.
async function batchPreparationManualReview(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentBatchTransferArtifactStore();
  if (!storage) return null;
  const artifact = await withPrivacySessionGuard(session, () => storage.load());
  assertPrivacySessionCurrent(session);
  if (!artifact) return null;
  const reservation = batchTransferArtifactReservation(artifact);
  const ids = reservationIDs(reservation);
  if (!ids.length) return null;
  const records = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  if (
    records.length !== ids.length ||
    !records.every(
      (record) => record.status === reservationStatuses.ManualReview,
    )
  ) {
    return null;
  }
  const operationID = reservationOperationID(records[0]);
  if (!operationID || records.some(
    (record) => reservationOperationID(record) !== operationID,
  )) {
    return null;
  }
  return { operationID, records };
}

async function recoverBatchPreparationFailure(
  error,
  { session = beginPrivacySessionOperation() } = {},
) {
  if (error && typeof error === "object") {
    // This function is reached before executeAtomicBatchTransfer can open
    // Keplr or call broadcastPreparedPrivacy.  Preserve that boundary on the
    // error so the caller never describes this as an ambiguous transaction.
    error.batchTransferPreparationFailedBeforeWallet = true;
  }
  try {
    await refreshNoteReservationState({ session });
    assertPrivacySessionCurrent(session);
    const review = await batchPreparationManualReview({ session });
    if (review && error && typeof error === "object") {
      error.batchTransferManualReview = review;
    }
    return review;
  } catch (recoveryError) {
    if (recoveryError?.privacySessionInvalidated) throw recoveryError;
    // Keep the preparation failure as the user-facing cause. A failure to
    // refresh the local recovery UI must not claim that the notes are safe to
    // use, and the next Scan Notes will retry this read.
    warnReservationBookkeeping(recoveryError);
    return null;
  }
}

function persistRelayWithdrawPayloadState(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentRelayWithdrawMetadataStore();
  if (!storage) return Promise.resolve();
  const key = relayMetadataStoreKey;
  const currentSnapshot = currentPreparedRelayWithdrawSnapshot();
  const current = currentSnapshot
    ? sanitizeRelayWithdrawSnapshot(currentSnapshot)
    : null;
  const pending = (state.keplr.relayWithdrawPendingPayloads || [])
    .map((snapshot) => sanitizeRelayWithdrawSnapshot(snapshot))
    .filter(Boolean);
  const previousWrite = relayMetadataWrites.get(key) || Promise.resolve();
  const write = previousWrite
    .catch(() => undefined)
    .then(async () => {
      assertPrivacySessionCurrent(session);
      if (!current && !pending.length) {
        await withPrivacySessionGuard(session, () => storage.clear());
      } else {
        await withPrivacySessionGuard(
          session,
          () => storage.save({
            current,
            pending,
            savedAt: new Date().toISOString(),
          }),
        );
      }
      assertPrivacySessionCurrent(session);
    });
  const queuedWrite = write.catch((error) => {
    if (error?.privacySessionInvalidated) return;
    // Do not send the original error object to the browser console. A
    // product may forward console events to telemetry, and operation errors
    // can carry privacy-sensitive payload or proof context.
    console.warn("clairveil_relay_metadata_persistence_failed");
  });
  relayMetadataWrites.set(key, queuedWrite);
  void queuedWrite.finally(() => {
    if (relayMetadataWrites.get(key) === queuedWrite) {
      relayMetadataWrites.delete(key);
    }
  });
  return write;
}

async function latestReservationRecords(
  reservation,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  const manager = currentNoteReservationManager({ optional: true });
  if (!ids.length) return [];
  if (!manager || typeof manager.getReservation !== "function") return [];
  const records = [];
  for (const id of ids) {
    try {
      records.push(
        await withPrivacySessionGuard(session, () => manager.getReservation(id)),
      );
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      warnReservationBookkeeping(error);
    }
  }
  assertPrivacySessionCurrent(session);
  return records;
}

async function syncRelayWithdrawSnapshotReservation(
  snapshot,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!snapshot?.reservation) return snapshot;
  const records = await latestReservationRecords(snapshot.reservation, { session });
  assertPrivacySessionCurrent(session);
  if (!records.length) return snapshot;
  // The encrypted relay-recovery snapshot intentionally stores only a small
  // allowlist of metadata. Keep the full, current reservation records in the
  // in-memory cache as well so the recovery card does not mistake a retained
  // no-broadcast marker for missing evidence after a reload.
  cacheReservationRecords(records);
  const reservation = updateReservationBatchRecords(snapshot.reservation, records);
  return {
    ...snapshot,
    reservation,
    submitted: records.every(
      (record) => record.status === reservationStatuses.Submitted,
    ),
  };
}

function relaySnapshotNeedsPendingRecovery(snapshot) {
  const records = snapshot?.reservation?.reservations || [];
  if (!records.length) return true;
  return records.some((record) => isActiveReservationStatus(record.status));
}

async function relaySnapshotWithFullReservationRecords(
  snapshot,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(snapshot?.reservation);
  if (!ids.length) return null;
  const records = await latestReservationRecords(snapshot.reservation, { session });
  assertPrivacySessionCurrent(session);
  if (records.length !== ids.length) return null;
  return {
    ...snapshot,
    reservation: {
      ...snapshot.reservation,
      reservations: records,
    },
  };
}

async function replanRecoveredLocalRelayPayload(
  snapshot,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const manager = currentNoteReservationManager({ optional: true });
  const recoverySnapshot = await relaySnapshotWithFullReservationRecords(snapshot, {
    session,
  });
  if (!recoverySnapshot) return snapshot;
  const records = recoverySnapshot.reservation.reservations || [];
  // A relay operation must never be recovered as independent notes. A mixed
  // status means an earlier lifecycle write is incomplete or inconsistent;
  // preserve the whole operation for review instead of inferring a local
  // expiry/replan path from only a subset of its inputs.
  const handedOff = Boolean(
    recoverySnapshot.handedOff || records.some(reservationHasRelayHandoffEvidence),
  );
  const recoveredSnapshot = {
    ...recoverySnapshot,
    handedOff,
  };
  const operationStatuses = new Set(records.map((record) => record.status));
  if (records.length > 0 && operationStatuses.size !== 1) {
    try {
      const updated = await markReservationBatchManualReview(
        recoverySnapshot.reservation,
        new Error("relay operation reservations have mixed recovery statuses"),
        "recovered_mixed_relay_operation_status_requires_manual_review",
        {
          operation_statuses: [...operationStatuses].sort().join(","),
          ...(handedOff ? { relay_handed_off: true } : {}),
        },
        { session },
      );
      assertPrivacySessionCurrent(session);
      return {
        ...recoveredSnapshot,
        reservation: updateReservationBatchRecords(
          recoverySnapshot.reservation,
          updated || [],
        ),
      };
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      warnReservationBookkeeping(error);
      return recoveredSnapshot;
    }
  }
  // A prepared relay payload is immutable for the entire operation.  Do not
  // infer a replacement/replan path when one input is missing its payload hash
  // or carries a different one: that would combine lifecycle evidence from
  // distinct handoffs.
  const operationPayloadHashes = records.map((record) =>
    String(record.payload_hash || record.payloadHash || "").trim(),
  );
  const hasProofReady = records.some(
    (record) => record.status === reservationStatuses.ProofReady,
  );
  const payloadHashEvidenceInconsistent = hasProofReady &&
    (new Set(operationPayloadHashes).size !== 1 || !operationPayloadHashes[0]);
  if (payloadHashEvidenceInconsistent) {
    try {
      const updated = await markReservationBatchManualReview(
        recoverySnapshot.reservation,
        new Error("relay operation reservations have inconsistent payload hash evidence"),
        "recovered_relay_payload_hash_inconsistent_requires_manual_review",
        {
          payload_hash_evidence: "inconsistent",
          ...(handedOff ? { relay_handed_off: true } : {}),
        },
        { session },
      );
      assertPrivacySessionCurrent(session);
      return {
        ...recoveredSnapshot,
        reservation: updateReservationBatchRecords(
          recoverySnapshot.reservation,
          updated || [],
        ),
      };
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      warnReservationBookkeeping(error);
      return recoveredSnapshot;
    }
  }
  const localWorkerState = records.every((record) =>
    [
      reservationStatuses.Reserved,
      reservationStatuses.Proving,
      reservationStatuses.ProofReady,
    ].includes(record.status),
  );
  const localPreBroadcast = localWorkerState && records.every((record) =>
    !reservationHasBroadcastEvidence(record),
  );
  const workerExpired = records.every((record) =>
    reservationCanRecoverAfterWorkerExpiry(record, manager),
  );
  const durableNoBroadcast = records.every(
    reservationHasDurableNoBroadcastEvidence,
  );
  const target = expiredRelayReservationRecoveryTarget({
    handedOff,
    localWorkerState: records.length > 0 && localWorkerState,
    localPreBroadcast,
    workerExpired,
    hasProofReady,
  });
  if (!target) return recoveredSnapshot;
  try {
    const updated = target === reservationStatuses.ManualReview
      ? await markReservationBatchManualReview(
          recoverySnapshot.reservation,
          new Error("expired ProofReady relay payload requires reconciliation"),
          durableNoBroadcast
            ? "recovered_relay_proof_ready_requires_review_after_expiry"
            : "recovered_relay_proof_ready_without_durable_pre_broadcast_evidence",
          {},
          { session },
        )
      : await markReservationBatchReplanRequired(
          recoverySnapshot.reservation,
          new Error("local_relay_payload_lost_on_refresh"),
          "local_relay_payload_lost_on_refresh",
          { session },
        );
    assertPrivacySessionCurrent(session);
    return {
      ...recoveredSnapshot,
      reservation: updateReservationBatchRecords(
        recoverySnapshot.reservation,
        updated || [],
      ),
    };
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    warnReservationBookkeeping(error);
    return snapshot;
  }
}

function reservationHasRelayHandoffEvidence(record = {}) {
  const metadata = record.metadata || {};
  return Boolean(
    metadata.relay_handed_off ||
      metadata.relayHandedOff ||
      record.status === reservationStatuses.Submitted ||
      record.status === reservationStatuses.Unknown ||
      reservationHasBroadcastEvidence(record),
  );
}

function relaySnapshotFromActiveReservation(record = {}) {
  if (record.kind !== "relay_withdraw" || !record.reservation_id) return null;
  const metadata = record.metadata || {};
  const expiresAtUnix = String(
    metadata.payload_expires_at_unix ||
      metadata.payloadExpiresAtUnix ||
      metadata.expires_at_unix ||
      metadata.expiresAtUnix ||
      "",
  );
  return sanitizeRelayWithdrawSnapshot({
    id: record.payload_hash || record.reservation_id,
    reservation: {
      operation_id: record.operation_id || "",
      reservation_ids: [record.reservation_id],
      reservations: [record],
    },
    expiresAtUnix,
    payloadHash: record.payload_hash || "",
    submitted: record.status === reservationStatuses.Submitted,
    handedOff: reservationHasRelayHandoffEvidence(record),
    createdAt: record.created_at || "",
  });
}

async function recoverActiveRelayWithdrawSnapshots(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.listActiveReservations !== "function") {
    return [];
  }
  const active = await withPrivacySessionGuard(
    session,
    () => manager.listActiveReservations(),
  );
  const byOperation = new Map();
  for (const record of active) {
    if (record.kind !== "relay_withdraw") continue;
    const operationID = reservationOperationID(record);
    const records = byOperation.get(operationID) || [];
    records.push(record);
    byOperation.set(operationID, records);
  }
  const recovered = [];
  for (const records of byOperation.values()) {
    const snapshot = relaySnapshotFromActiveReservation(records[0]);
    if (!snapshot) continue;
    snapshot.reservation = {
      operation_id: reservationOperationID(records[0]),
      reservation_ids: records.map((record) => record.reservation_id),
      reservations: records,
    };
    snapshot.handedOff = records.some(reservationHasRelayHandoffEvidence);
    recovered.push(await replanRecoveredLocalRelayPayload(snapshot, { session }));
    assertPrivacySessionCurrent(session);
  }
  return recovered;
}

async function loadPersistedRelayWithdrawPayloadState({ session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const storage = currentRelayWithdrawMetadataStore();
  let saved = { valid: true, current: null, pending: [] };
  if (storage) {
    const pendingWrite = relayMetadataWrites.get(relayMetadataStoreKey);
    if (pendingWrite) {
      await withPrivacySessionGuard(session, () => pendingWrite);
    }
    saved = parsePersistedRelayWithdrawState(
      await withPrivacySessionGuard(session, () => storage.load()),
    );
    assertPrivacySessionCurrent(session);
    if (!saved.valid) {
      await withPrivacySessionGuard(session, () => storage.clear());
      assertPrivacySessionCurrent(session);
    }
  }
  const pending = [];
  for (const snapshot of saved.pending) {
    pending.push(
      await syncRelayWithdrawSnapshotReservation(snapshot, { session }),
    );
    assertPrivacySessionCurrent(session);
  }
  const current = saved.current;
  if (current && !state.keplr.relayWithdrawPayload) {
    const synced = await syncRelayWithdrawSnapshotReservation(current, { session });
    assertPrivacySessionCurrent(session);
    const replanned = await replanRecoveredLocalRelayPayload(synced, { session });
    assertPrivacySessionCurrent(session);
    if (replanned?.handedOff) {
      pending.unshift(replanned);
    }
  }
  const recovered = await recoverActiveRelayWithdrawSnapshots({ session });
  assertPrivacySessionCurrent(session);
  const existing = state.keplr.relayWithdrawPendingPayloads || [];
  const recoveredByID = new Map();
  for (const snapshot of [...existing, ...pending, ...recovered]) {
    recoveredByID.set(relayWithdrawPendingPayloadID(snapshot), snapshot);
  }
  state.keplr.relayWithdrawPendingPayloads = [
    ...recoveredByID.values(),
  ].filter(relaySnapshotNeedsPendingRecovery);
  await reconcileExpiredRelayWithdrawPayloads(null, { session });
  assertPrivacySessionCurrent(session);
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
}

function currentPreparedRelayWithdrawSnapshot() {
  if (!state.keplr.relayWithdrawPayload) return null;
  const snapshot = {
    payload: state.keplr.relayWithdrawPayload,
    preparedData: state.keplr.relayWithdrawPreparedData,
    reservation: state.keplr.relayWithdrawReservation,
    payloadText: state.keplr.relayWithdrawPayloadText,
    amount: state.keplr.relayWithdrawPayloadAmount,
    recipient: state.keplr.relayWithdrawPayloadRecipient,
    chainId: state.keplr.relayWithdrawPayloadChainId,
    expiresAt: state.keplr.relayWithdrawPayloadExpiresAt,
    expiresAtUnix: relaySnapshotExpiresAtUnix({
      payload: state.keplr.relayWithdrawPayload,
    }),
    payloadHash: state.keplr.relayWithdrawPayloadHash,
    submitted: state.keplr.relayWithdrawPayloadSubmitted,
    handedOff: state.keplr.relayWithdrawPayloadHandedOff,
    relayHash: state.keplr.relayWithdrawHash,
    relayHeight: state.keplr.relayWithdrawHeight,
    relayer: state.keplr.relayWithdrawRelayer,
    createdAt: new Date().toISOString(),
  };
  snapshot.id = relayWithdrawPendingPayloadID(snapshot);
  return snapshot;
}

function stopRelayPayloadExpiryReconciliation() {
  relayPayloadExpiryReconciliationGeneration += 1;
  if (relayPayloadExpiryReconciliationTimer !== null) {
    globalThis.clearTimeout(relayPayloadExpiryReconciliationTimer);
  }
  relayPayloadExpiryReconciliationTimer = null;
}

function relaySnapshotsAwaitingExpiryReconciliation() {
  const snapshots = [
    currentPreparedRelayWithdrawSnapshot(),
    ...(state.keplr.relayWithdrawPendingPayloads || []),
  ];
  return snapshots.filter((snapshot) => {
    const expiresAtUnix = Number(relaySnapshotExpiresAtUnix(snapshot));
    return (
      !snapshot.submitted &&
      !snapshot.relayHash &&
      Number.isSafeInteger(expiresAtUnix) &&
      expiresAtUnix > 0 &&
      relayReservationStatus(snapshot.reservation) === reservationStatuses.ProofReady
    );
  });
}

function earliestRelayPayloadExpiryUnix() {
  const expiries = relaySnapshotsAwaitingExpiryReconciliation()
    .map((snapshot) => Number(relaySnapshotExpiresAtUnix(snapshot)))
    .filter((expiresAtUnix) => Number.isSafeInteger(expiresAtUnix));
  return expiries.length ? Math.min(...expiries) : null;
}

async function scheduleRelayPayloadExpiryReconciliation(
  { session = beginPrivacySessionOperation() } = {},
) {
  if (!isPrivacySessionCurrent(session)) return;
  stopRelayPayloadExpiryReconciliation();
  const scheduleGeneration = relayPayloadExpiryReconciliationGeneration;
  const expiresAtUnix = earliestRelayPayloadExpiryUnix();
  if (expiresAtUnix === null) return;

  let delayMs = relayPayloadExpiryReconciliationRetryMs;
  try {
    const chainSnapshot = await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
    if (
      !isPrivacySessionCurrent(session) ||
      scheduleGeneration !== relayPayloadExpiryReconciliationGeneration
    ) {
      return;
    }
    const chainNowMs = chainSnapshot?.chainNowMs;
    if (Number.isSafeInteger(chainNowMs) && chainNowMs >= 0) {
      // The callback fetches chain time again before changing a reservation.
      // This calculation only decides when to perform that authoritative
      // check; it must never use the browser clock to authorize recovery.
      delayMs = Math.max(0, expiresAtUnix * 1000 - chainNowMs + 1);
    }
  } catch (error) {
    if (error?.privacySessionInvalidated) return;
    if (!isPrivacySessionCurrent(session)) return;
  }
  if (
    !isPrivacySessionCurrent(session) ||
    scheduleGeneration !== relayPayloadExpiryReconciliationGeneration
  ) {
    return;
  }
  relayPayloadExpiryReconciliationTimer = globalThis.setTimeout(() => {
    if (
      !isPrivacySessionCurrent(session) ||
      scheduleGeneration !== relayPayloadExpiryReconciliationGeneration
    ) {
      return;
    }
    relayPayloadExpiryReconciliationTimer = null;
    void reconcileExpiredRelayWithdrawPayloads(null, { session })
      .catch((error) => {
        if (!error?.privacySessionInvalidated) {
          warnReservationBookkeeping(error);
        }
      })
      .finally(() => {
        if (
          isPrivacySessionCurrent(session) &&
          scheduleGeneration === relayPayloadExpiryReconciliationGeneration
        ) {
          void scheduleRelayPayloadExpiryReconciliation({ session });
        }
      });
  }, Math.min(delayMs, maximumBrowserTimerDelayMs));
}

function rememberPendingRelayWithdrawPayload(snapshot) {
  if (!snapshot?.payload) return;
  const id = relayWithdrawPendingPayloadID(snapshot);
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  state.keplr.relayWithdrawPendingPayloads = [
    { ...snapshot, id, handedOff: true },
    ...pending.filter((item) => relayWithdrawPendingPayloadID(item) !== id),
  ];
}

function stashHandedOffPreparedRelayWithdrawPayload() {
  const snapshot = currentPreparedRelayWithdrawSnapshot();
  if (!snapshot?.handedOff || snapshot.submitted) return;
  rememberPendingRelayWithdrawPayload(snapshot);
}

function advanceRelayWithdrawPayloadGeneration() {
  relayWithdrawPayloadGeneration += 1;
  state.keplr.relayWithdrawPayloadVersion = relayWithdrawPayloadGeneration;
  return relayWithdrawPayloadGeneration;
}

function applyPreparedRelayWithdrawSnapshot(snapshot) {
  advanceRelayWithdrawPayloadGeneration();
  state.keplr.relayWithdrawPayload = snapshot.payload || null;
  state.keplr.relayWithdrawPreparedData = snapshot.preparedData || null;
  state.keplr.relayWithdrawReservation = snapshot.reservation || null;
  state.keplr.relayWithdrawPayloadText =
    snapshot.payloadText || relayPayloadText(snapshot.payload);
  state.keplr.relayWithdrawPayloadAmount = snapshot.amount || "";
  state.keplr.relayWithdrawPayloadRecipient = snapshot.recipient || "";
  state.keplr.relayWithdrawPayloadChainId = snapshot.chainId || "";
  state.keplr.relayWithdrawPayloadExpiresAt = snapshot.expiresAt || "";
  state.keplr.relayWithdrawPayloadHash = snapshot.payloadHash || "";
  state.keplr.relayWithdrawPayloadSubmitted = Boolean(snapshot.submitted);
  state.keplr.relayWithdrawPayloadHandedOff = true;
  state.keplr.relayWithdrawHash = snapshot.relayHash || "";
  state.keplr.relayWithdrawHeight = snapshot.relayHeight || "";
  state.keplr.relayWithdrawRelayer = snapshot.relayer || "";
}

async function restorePendingRelayWithdrawPayload(
  id,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const expectedPayloadVersion = state.keplr.relayWithdrawPayloadVersion;
  const expectedPayload = state.keplr.relayWithdrawPayload;
  const assertPreparedPayloadIsCurrent = () => {
    if (!isPrivacySessionCurrent(session)) {
      throw privacySessionInvalidatedError();
    }
    if (
      state.keplr.relayWithdrawPayloadVersion !== expectedPayloadVersion ||
      state.keplr.relayWithdrawPayload !== expectedPayload
    ) {
      const error = new Error(
        "Prepared relay withdraw payload changed while a pending payload was being restored",
      );
      error.relayPayloadChanged = true;
      throw error;
    }
  };
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  const snapshot = pending.find(
    (item) => relayWithdrawPendingPayloadID(item) === id,
  );
  if (!snapshot) return;
  const synced = await syncRelayWithdrawSnapshotReservation(snapshot, { session });
  assertPrivacySessionCurrent(session);
  if (
    !replacePendingRelayWithdrawPayload(id, synced, {
      expectedSnapshot: snapshot,
    })
  ) {
    return;
  }
  if (!synced.payload) {
    await persistRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
    toast(
      "Relay payload JSON은 refresh 후 복원되지 않습니다. Notes를 refresh해서 상태를 확인해줘.",
    );
    return;
  }
  if (
    relayReservationStatus(synced.reservation) !==
    reservationStatuses.ProofReady
  ) {
    await persistRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
    toast("Relay reservation이 더 이상 ProofReady가 아닙니다.");
    return;
  }
  assertPreparedPayloadIsCurrent();
  const cleared = await discardAndClearPreparedRelayWithdrawPayload({ session });
  assertPrivacySessionCurrent(session);
  if (!cleared) return;
  // Clearing the previously active payload can await reservation writes and
  // reads. Do not restore a payload from the earlier snapshot if another
  // context advanced any member of this operation while that cleanup ran.
  const finalRecords = await latestReservationRecords(synced.reservation, {
    session,
  });
  assertPreparedPayloadIsCurrent();
  const finalReservation = updateReservationBatchRecords(
    synced.reservation,
    finalRecords,
  );
  if (
    finalRecords.length !== reservationIDs(synced.reservation).length ||
    relayReservationStatus(finalReservation) !== reservationStatuses.ProofReady
  ) {
    if (
      !replacePendingRelayWithdrawPayload(
        id,
        { ...synced, reservation: finalReservation },
        { expectedSnapshot: synced },
      )
    ) {
      return;
    }
    await persistRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
    toast("Relay reservation 상태가 변경되어 payload를 복원하지 않았습니다.");
    return;
  }
  if (
    !replacePendingRelayWithdrawPayload(id, null, {
      expectedSnapshot: synced,
    })
  ) {
    return;
  }
  applyPreparedRelayWithdrawSnapshot({
    ...synced,
    reservation: finalReservation,
  });
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
}

async function refreshPendingRelayWithdrawPayloadStatus(
  id,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  const snapshot = pending.find(
    (item) => relayWithdrawPendingPayloadID(item) === id,
  );
  if (!snapshot) return;
  const synced = await syncRelayWithdrawSnapshotReservation(snapshot, { session });
  assertPrivacySessionCurrent(session);
  if (!pendingRelayWithdrawPayloadIsCurrent(id, snapshot)) return;
  const status = relayReservationStatus(synced.reservation);
  const reconciled =
    status === reservationStatuses.ManualReview
      ? await resolveExpiredRelayManualReview(synced, null, { session })
      : await reconcileExpiredRelayWithdrawSnapshot(synced, null, { session });
  assertPrivacySessionCurrent(session);
  if (
    !replacePendingRelayWithdrawPayload(id, reconciled, {
      expectedSnapshot: snapshot,
    })
  ) {
    return;
  }
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
  return reconciled;
}

async function setPreparedRelayWithdrawPayload(
  data,
  {
    amount = "",
    recipient = "",
    preparation = null,
    session = beginPrivacySessionOperation(),
  } = {},
) {
  assertPrivacySessionCurrent(session);
  if (preparation && !relayWithdrawPreparationIsCurrent(preparation)) {
    await rejectStaleRelayWithdrawPreparation(data, { session });
  }
  stashHandedOffPreparedRelayWithdrawPayload();
  const installVersion = advanceRelayWithdrawPayloadGeneration();
  const installPreparation = preparation
    ? { ...preparation, version: installVersion }
    : null;
  await discardPreparedRelayWithdrawPayload(
    "local_relay_payload_overwritten_before_handoff",
    { session },
  );
  assertPrivacySessionCurrent(session);
  if (
    state.keplr.relayWithdrawPayloadVersion !== installVersion ||
    (installPreparation && !relayWithdrawPreparationIsCurrent(installPreparation))
  ) {
    await rejectStaleRelayWithdrawPreparation(data, { session });
  }
  const payload = data?.payload || data || null;
  const canonicalRecipient =
    payload?.recipient || data?.prepared?.recipient || recipient;
  state.keplr.relayWithdrawPayload = payload;
  state.keplr.relayWithdrawPreparedData = data || null;
  state.keplr.relayWithdrawReservation =
    data?.reservation || data?.prepared?.reservation || null;
  state.keplr.relayWithdrawPayloadText = relayPayloadText(payload);
  state.keplr.relayWithdrawPayloadAmount = amount || "";
  state.keplr.relayWithdrawPayloadRecipient = canonicalRecipient || "";
  state.keplr.relayWithdrawPayloadChainId =
    payload?.chain_id || payload?.chainId || "";
  state.keplr.relayWithdrawPayloadExpiresAt = formatRelayPayloadExpiry(
    payload?.expires_at_unix || payload?.expiresAtUnix || "",
  );
  state.keplr.relayWithdrawPayloadHash =
    payload?.payload_hash || data?.payloadHash || "";
  state.keplr.relayWithdrawPayloadSubmitted = false;
  state.keplr.relayWithdrawPayloadHandedOff = false;
  state.keplr.relayWithdrawHash = "";
  state.keplr.relayWithdrawHeight = "";
  state.keplr.relayWithdrawRelayer = "";
  await renewReservationBatchLease(
    state.keplr.relayWithdrawReservation,
    { session },
  );
  assertPrivacySessionCurrent(session);
  if (state.keplr.relayWithdrawPayloadVersion !== installVersion) return false;
  startPreparedRelayReservationHeartbeat({ session });
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  if (state.keplr.relayWithdrawPayloadVersion !== installVersion) return false;
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  void scheduleRelayPayloadExpiryReconciliation({ session });
  return true;
}

async function discardPreparedRelayWithdrawPayload(
  reason = "local_relay_payload_discarded_before_handoff",
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (
    !state.keplr.relayWithdrawPayload ||
    state.keplr.relayWithdrawPayloadSubmitted ||
    state.keplr.relayWithdrawPayloadHandedOff ||
    !state.keplr.relayWithdrawReservation?.reservation_ids?.length
  ) {
    return;
  }
  const reservation = state.keplr.relayWithdrawReservation;
  const manager = currentNoteReservationManager({ optional: true });
  const records = await latestReservationRecords(reservation, { session });
  assertPrivacySessionCurrent(session);
  if (records.some(reservationHasRelayHandoffEvidence)) {
    // The durable handoff record is authoritative. A copy/local-submit flow
    // can finish recording it after a draft-input change has already advanced
    // the in-memory payload version; local discard must never revoke that
    // relayer-valid payload by replanning its inputs.
    updateRelayWithdrawReservationRecords(records, { session });
    const currentIDs = reservationIDs(state.keplr.relayWithdrawReservation);
    const discardedIDs = reservationIDs(reservation);
    if (
      currentIDs.length === discardedIDs.length &&
      currentIDs.every((id) => discardedIDs.includes(id))
    ) {
      state.keplr.relayWithdrawPayloadHandedOff = true;
      state.keplr.relayWithdrawPayloadSubmitted = records.every(
        (record) => record.status === reservationStatuses.Submitted,
      );
      stashHandedOffPreparedRelayWithdrawPayload();
    }
    return;
  }
  const expiredProofReady = records.length > 0 && records.every(
    (record) =>
      record.status === reservationStatuses.ProofReady &&
      reservationCanRecoverAfterWorkerExpiry(record, manager),
  );
  if (!expiredProofReady) {
    await markReservationBatchReplanRequired(
      reservation,
      new Error(reason),
      reason,
      { session },
    );
    assertPrivacySessionCurrent(session);
    return;
  }
  const updated = await markReservationBatchManualReview(
    reservation,
    new Error("expired local relay ProofReady payload requires review"),
    "expired_local_relay_payload_discard_requires_manual_review",
    {
      no_broadcast_attempt: true,
      proof_discarded: true,
    },
    { session },
  );
  assertPrivacySessionCurrent(session);
  const snapshot = currentPreparedRelayWithdrawSnapshot();
  if (!snapshot) return;
  const pendingSnapshot = sanitizeRelayWithdrawSnapshot({
    ...snapshot,
    reservation: updateReservationBatchRecords(reservation, updated || []),
  });
  if (!pendingSnapshot) return;
  const pending = state.keplr.relayWithdrawPendingPayloads || [];
  state.keplr.relayWithdrawPendingPayloads = [
    ...pending.filter(
      (item) =>
        relayWithdrawPendingPayloadID(item) !==
        relayWithdrawPendingPayloadID(pendingSnapshot),
    ),
    pendingSnapshot,
  ];
}

async function discardAndClearPreparedRelayWithdrawPayload(options = {}) {
  const {
    discardReason = "local_relay_payload_discarded_before_handoff",
    session = beginPrivacySessionOperation(),
    ...clearOptions
  } = options;
  assertPrivacySessionCurrent(session);
  const expectedVersion = advanceRelayWithdrawPayloadGeneration();
  await discardPreparedRelayWithdrawPayload(discardReason, { session });
  assertPrivacySessionCurrent(session);
  if (state.keplr.relayWithdrawPayloadVersion !== expectedVersion) {
    return false;
  }
  await clearPreparedRelayWithdrawPayload({
    ...clearOptions,
    advanceGeneration: false,
    session,
  });
  assertPrivacySessionCurrent(session);
  return true;
}

async function clearPreparedRelayWithdrawPayload({
  clearPayloadHash = false,
  stashHandedOff = true,
  advanceGeneration = true,
  session = beginPrivacySessionOperation(),
} = {}) {
  assertPrivacySessionCurrent(session);
  stopRelayPayloadExpiryReconciliation();
  if (advanceGeneration) advanceRelayWithdrawPayloadGeneration();
  stopPreparedRelayReservationHeartbeat();
  if (stashHandedOff) {
    stashHandedOffPreparedRelayWithdrawPayload();
  }
  const shouldClearPayloadHash =
    clearPayloadHash ||
    (Boolean(state.keplr.relayWithdrawPayload) &&
      !state.keplr.relayWithdrawPayloadSubmitted);
  state.keplr.relayWithdrawPayload = null;
  state.keplr.relayWithdrawPreparedData = null;
  state.keplr.relayWithdrawReservation = null;
  state.keplr.relayWithdrawPayloadText = "";
  state.keplr.relayWithdrawPayloadAmount = "";
  state.keplr.relayWithdrawPayloadRecipient = "";
  state.keplr.relayWithdrawPayloadChainId = "";
  state.keplr.relayWithdrawPayloadExpiresAt = "";
  state.keplr.relayWithdrawPayloadSubmitted = false;
  state.keplr.relayWithdrawPayloadHandedOff = false;
  if (shouldClearPayloadHash) {
    state.keplr.relayWithdrawPayloadHash = "";
  }
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  void scheduleRelayPayloadExpiryReconciliation({ session });
}

function renderPendingRelayWithdrawPayloads() {
  if (!els.relayWithdrawPendingList) return;
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  els.relayWithdrawPendingList.hidden = !pending.length;
  els.relayWithdrawPendingList.replaceChildren();
  for (const item of pending) {
    const row = document.createElement("div");
    row.className = "pending-relay-item";

    const details = document.createElement("div");
    details.className = "pending-relay-details";
    const amount = document.createElement("strong");
    amount.textContent = item.amount || "-";
    const hash = document.createElement("span");
    hash.textContent = item.payloadHash
      ? shorten(item.payloadHash, 12, 10)
      : "payload pending";
    details.append(amount, hash);

    const id = relayWithdrawPendingPayloadID(item);
    const pendingStatus = relayReservationStatus(item.reservation);
    const displayRecords = manualReviewRecordsForDisplay(
      item.reservation?.reservations || [],
    );
    if (pendingStatus === reservationStatuses.ManualReview) {
      const review = document.createElement("span");
      review.textContent = "Manual review evidence";
      details.append(review);
      appendManualReviewEvidence(
        details,
        displayRecords,
        { payloadHash: item.payloadHash },
      );
    }
    const action = document.createElement("button");
    action.type = "button";
    if (item.payload) {
      action.textContent = "Use";
      action.addEventListener("click", () => {
        const session = beginPrivacySessionOperation();
        const recoveryLock = beginPendingRelayRecovery(id, session);
        if (!recoveryLock) return;
        action.disabled = true;
        restorePendingRelayWithdrawPayload(id, { session })
          .catch((error) => {
            if (!error?.privacySessionInvalidated) {
              toast(
                privacyRecoveryErrorMessage(
                  error,
                  "Relay payload could not be restored. Refresh Notes to verify its reservation state.",
                ),
              );
            }
          })
          .finally(() => {
            endPendingRelayRecovery(recoveryLock);
            if (isPrivacySessionCurrent(session)) action.disabled = false;
          });
      });
    } else {
      action.textContent =
        pendingStatus === reservationStatuses.ManualReview
          ? "Check recovery"
          : "Refresh status";
      if (pendingStatus === reservationStatuses.ManualReview) {
        action.title =
          "Checks authoritative expiry, input-nullifier, and broadcast evidence. It never cancels a relay handoff.";
      }
      action.addEventListener("click", () => {
        const session = beginPrivacySessionOperation();
        const recoveryLock = beginPendingRelayRecovery(id, session);
        if (!recoveryLock) return;
        action.disabled = true;
        refreshPendingRelayWithdrawPayloadStatus(id, { session })
          .then((reconciled) => {
            if (!reconciled || !isPrivacySessionCurrent(session)) return;
            const reconciledStatus = relayReservationStatus(
              reconciled.reservation,
            );
            if (reconciledStatus === reservationStatuses.ReplanRequired) {
              toast(
                "Expired relay handoff was resolved. Prepare a fresh withdraw payload.",
              );
              return;
            }
            if (reconciledStatus === reservationStatuses.ManualReview) {
              toast(relayManualReviewRecoveryMessage(reconciled));
              return;
            }
            toast("Relay recovery status was refreshed.");
          })
          .catch((error) => {
            if (!error?.privacySessionInvalidated) {
              toast(
                privacyRecoveryErrorMessage(
                  error,
                  "Relay recovery status could not be refreshed. Refresh Notes and retry.",
                ),
              );
            }
          })
          .finally(() => {
            endPendingRelayRecovery(recoveryLock);
            if (isPrivacySessionCurrent(session)) action.disabled = false;
          });
      });
    }

    row.append(details, action);
    els.relayWithdrawPendingList.append(row);
  }
}

function renderRelayManualReviewEvidence(
  needsReview,
  reservation,
  payloadHash,
) {
  const container = els.relayWithdrawManualReviewEvidence;
  if (!container) return;
  container.hidden = !needsReview;
  container.replaceChildren();
  if (!needsReview) return;
  const title = document.createElement("strong");
  title.textContent = "Manual review evidence";
  container.append(title);
  appendManualReviewEvidence(
    container,
    reservation?.reservations || [],
    { payloadHash },
  );
}

function renderRelayerPanel() {
  const relayer = localRelayerAccount();
  const hasRelayedPayload = Boolean(state.keplr.relayWithdrawPayload);
  const relayStatus = relayReservationStatus(
    state.keplr.relayWithdrawReservation,
    reservationRecordsByID(),
  );
  const relayPayloadReady = isRelayPreparedWithdrawStructurallyReady();
  const relayNeedsManualResolution =
    hasRelayedPayload && relayStatus === reservationStatuses.ManualReview;
  const pendingPayloadCount =
    state.keplr.relayWithdrawPendingPayloads?.length || 0;
  const relayerReady =
    Boolean(relayer?.transparentAddress) && serverFeature("relayer");
  els.relayerTransparentAddress.textContent =
    transparentDisplayAddressFor(relayer) || "-";
  els.relayerBalance.textContent =
    state.relayer.error ||
    state.relayer.balance ||
    (relayerReady ? "Loading..." : "-");
  els.relayerState.textContent = state.keplr.relayWithdrawHash
    ? "Relay included"
    : hasRelayedPayload
      ? relayPayloadReady
        ? "Payload ready"
        : `Reservation ${relayStatus || "not ready"}`
      : pendingPayloadCount
        ? `${pendingPayloadCount} pending payload${
            pendingPayloadCount === 1 ? "" : "s"
          }`
        : relayerReady
          ? "Waiting for payload"
          : "Local relayer unavailable; copy for external handoff";
  els.keplrRelayWithdrawHash.textContent = state.keplr.relayWithdrawHash
    ? shorten(state.keplr.relayWithdrawHash, 14, 12)
    : "-";
  els.keplrRelayWithdrawRelayer.textContent =
    state.keplr.relayWithdrawRelayer || "-";
  els.relayWithdrawPreparedAmount.textContent =
    state.keplr.relayWithdrawPayloadAmount || "-";
  els.relayWithdrawPreparedRecipient.textContent =
    state.keplr.relayWithdrawPayloadRecipient || "-";
  els.relayWithdrawPreparedChainId.textContent =
    state.keplr.relayWithdrawPayloadChainId || "-";
  els.relayWithdrawPreparedExpiresAt.textContent =
    state.keplr.relayWithdrawPayloadExpiresAt || "-";
  els.relayWithdrawPreparedPayloadHash.textContent =
    state.keplr.relayWithdrawPayloadHash || "-";
  renderRelayManualReviewEvidence(
    relayNeedsManualResolution,
    state.keplr.relayWithdrawReservation,
    state.keplr.relayWithdrawPayloadHash,
  );
  els.relayWithdrawPreparedPayloadJson.textContent =
    state.keplr.relayWithdrawPayloadText || "No prepared payload";
  els.copyRelayWithdrawPayload.disabled =
    !hasRelayedPayload || isRelayHandoffBoundaryInFlight();
  els.relayPreparedWithdraw.textContent = relayNeedsManualResolution
    ? "Check recovery"
    : "Relay locally";
  els.relayPreparedWithdraw.disabled =
    (!relayPayloadReady && !relayNeedsManualResolution) ||
    !relayerReady ||
    isRelayHandoffBoundaryInFlight();
  renderPendingRelayWithdrawPayloads();
}

function activeServerAccounts() {
  return serverFeature("localSigners") && selectedProfileMatchesServer()
    ? state.accounts
    : [];
}

function localSignerLabel(name) {
  const value = String(name || "local signer").trim();
  return value.charAt(0).toUpperCase() + value.slice(1);
}

function renderDappChainSelect() {
  if (!els.dappChainSelect) return;
  const profiles = state.chainProfiles.length
    ? state.chainProfiles
    : [configuredChainProfile()].filter(Boolean);
  els.dappChainSelect.innerHTML = "";
  for (const profile of profiles) {
    const option = document.createElement("option");
    option.value = profile.id;
    option.textContent = `${profile.label} (${profile.transport === "evm" ? "EVM" : "Cosmos"})`;
    els.dappChainSelect.append(option);
  }
  if (
    !state.selectedChainProfileId ||
    !profiles.some((profile) => profile.id === state.selectedChainProfileId)
  ) {
    state.selectedChainProfileId =
      state.config?.activeChainProfileId || profiles[0]?.id || "";
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

function renderChainDependentUi() {
  const walletKind = activeWalletKind();
  const transparentFormat = activeTransparentAddressFormat();
  els.keplrSendRecipient.placeholder =
    transparentFormat === "evm" ? "0x..." : `${accountPrefix()}1...`;
  if (
    els.keplrSendRecipient.value &&
    !isSendRecipientForWallet(els.keplrSendRecipient.value, walletKind)
  ) {
    els.keplrSendRecipient.value = "";
  }
  els.veiledTransferRecipient.placeholder = `${shieldedPrefix()}1...`;
  els.veiledWithdrawRecipient.placeholder =
    transparentFormat === "evm" ? "0x..." : `${accountPrefix()}1...`;
  els.relayWithdrawRecipient.placeholder =
    transparentFormat === "evm" ? "0x..." : `${accountPrefix()}1...`;
  document.querySelectorAll(".amount-control .denom").forEach((label) => {
    label.textContent = label.closest(".faucet-row")
      ? displayDenom()
      : baseDenom();
  });
  const faucetSource = activeServerAccounts()[0]?.name || "local signer";
  els.faucetHelpText.textContent = `(${displayDenom()} get from ${localSignerLabel(faucetSource)}'s wallet)`;
  renderDappChainHint();
}

async function selectDappChainProfile(profileId) {
  const profileChanged = state.selectedChainProfileId !== profileId;
  if (
    state.activeWallet ||
    walletConnectionSession ||
    profileChanged
  ) {
    resetWalletSession();
  }
  state.selectedChainProfileId = profileId;
  if (profileChanged) {
    // The test auditor authority is scoped to the selected chain profile, not
    // to the connected wallet. Clear it for a profile switch, where a stale
    // scalar could target another chain's audit key.
    resetAuditorSession();
    clearProfileScopedRecipientSuggestions();
    invalidateHealthView();
    invalidateLocalAccountView();
    clearLocalAccountDetails();
    invalidateRelayerView();
    ensureShieldedAddressBookScope();
  }
  clearChainSafety();
  renderDappChainSelect();
  renderChainDependentUi();
  renderAccounts();
  renderWalletSession();
  renderKeplr();
  renderVisibleAddressSuggestions();
  await refreshChainSafety({ force: true }).catch(() => {});
}

function recipientTestAccounts() {
  const accounts = activeServerAccounts();
  const preferred = accounts.filter((account) =>
    ["alice", "bob"].includes(account.name),
  );
  if (preferred.length) return preferred;
  return accounts.filter((account) => account.name !== "auditor");
}

async function ensureLocalSignersIfNeeded(data, { healthView = null } = {}) {
  if (
    data.config?.serverBacked !== true ||
    !data.config?.serverFeatures?.localSignerSetup ||
    data.config?.transport !== "evm" ||
    (data.accounts || []).length
  ) {
    return data;
  }
  let ensured;
  try {
    ensured = healthView === null
      ? await api("/api/local-signers/ensure", {
          method: "POST",
          body: JSON.stringify({}),
        })
      : await withHealthViewGuard(healthView, () =>
          api("/api/local-signers/ensure", {
            method: "POST",
            body: JSON.stringify({}),
          })
        );
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    if (healthView !== null) assertHealthViewCurrent(healthView);
    if (error?.statusCode !== 403) {
      throw error;
    }
    toast(
      "Local signer setup is blocked for LAN browsers. Create accounts on the server machine first, or restart with CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1.",
    );
    return {
      ...data,
      accounts: [],
    };
  }
  if (healthView !== null) assertHealthViewCurrent(healthView);
  return {
    ...data,
    accounts: ensured.accounts || [],
  };
}

function firstConfigProfile(config) {
  return (
    (config.chainProfiles || []).find(
      (profile) => profile.id === config.activeChainProfileId,
    ) ||
    config.chainProfiles?.[0] ||
    null
  );
}

async function browserHealthFromStaticConfig(
  config,
  { healthView = null } = {},
) {
  if (healthView !== null) assertHealthViewCurrent(healthView);
  assertValidatedDappConfig(config);
  assertBrowserDeploymentEndpointPolicy(config);
  const previousProfile = activeChainProfile();
  const profile = selectedProfileFromConfig(config) || firstConfigProfile(config);
  if (
    previousProfile &&
    profile &&
    profileSessionFingerprint(previousProfile) !==
      profileSessionFingerprint(profile, { config })
  ) {
    // The static configuration is also a profile boundary. Clear every
    // profile-scoped view before its health query can race an earlier
    // background refresh, even when no wallet is currently connected.
    if (healthView !== null) assertHealthViewCurrent(healthView);
    clearProfileScopedRecipientSuggestions();
    resetWalletSession();
    resetAuditorSession();
  }
  const health = healthView === null
    ? await clairveilBrowserClient(profile, { config }).health({
        allowUninitializedTree: true,
      })
    : await withHealthViewGuard(
        healthView,
        () =>
          clairveilBrowserClient(profile, { config }).health({
            allowUninitializedTree: true,
          }),
      );
  if (healthView !== null) assertHealthViewCurrent(healthView);
  return {
    config,
    status: health.status,
    tree: health.tree,
    audit: health.audit,
    accounts: [],
    errors: health.errors || [],
  };
}

async function loadDappHealth({ healthView = null } = {}) {
  const healthTask = (task) =>
    healthView === null ? task() : withHealthViewGuard(healthView, task);
  let staticHealthEndpointConfirmed = false;
  if (serverConfigAvailable) {
    try {
      const data = await healthTask(() =>
        api("/api/health", {
          timeoutMs: healthBootstrapRequestTimeoutMs,
          maxResponseBytes: healthBootstrapResponseMaxBytes,
          expectedResponseUrl: "/api/health",
          responseLabel: "WebApp health response",
          redirect: "error",
        }),
      );
      try {
        assertValidatedDappConfig(data.config);
        assertBrowserDeploymentEndpointPolicy(data.config);
      } catch (error) {
        error.dappConfigValidationFailure = true;
        throw error;
      }
      serverConfigAvailable = true;
      return ensureLocalSignersIfNeeded(data, { healthView });
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      if (error?.dappConfigValidationFailure) {
        throw new Error(`WebApp configuration is invalid; sync is unavailable: ${error.message}`);
      }
      // Only the server's versioned not-found envelope establishes that the
      // health endpoint is absent. A different 404 can be a reachable but
      // misconfigured server response and must not silently switch profiles
      // by falling back to the static artifact.
      const healthEndpointAbsent =
        error?.statusCode === 404 &&
        error?.code === "not_found" &&
        error?.data?.version === "v1";
      const staticHealthEndpointAbsent =
        error?.statusCode === 404 &&
        error?.apiPath === "/api/health" &&
        /^text\/html(?:;|$)/i.test(error?.apiResponseContentType || "");
      if (
        error?.statusCode &&
        !healthEndpointAbsent &&
        !staticHealthEndpointAbsent
      ) {
        throw new Error(`WebApp server health is unavailable: ${error.message}`);
      }
      const staticHealthFallback =
        staticHealthEndpointAbsent ||
        (error?.apiInvalidJsonResponse === true &&
          error?.apiPath === "/api/health" &&
          /^text\/html(?:;|$)/i.test(
            error?.apiResponseContentType || "",
          ));
      const timedOutHealth = error?.code === "request_timeout";
      if (
        !staticHealthFallback &&
        !timedOutHealth &&
        error?.statusCode === undefined &&
        error?.name &&
        error.name !== "TypeError"
      ) {
        throw error;
      }
      // Do not disable the server probe yet. The direct static artifact must
      // first validate as a static-only (`serverBacked: false`) deployment;
      // otherwise an HTML shell or stale artifact could mask a malformed
      // server configuration on every later refresh.
      if (healthEndpointAbsent || staticHealthFallback) {
        staticHealthEndpointConfirmed = true;
      }
    }
  }
  const config = await healthTask(() => loadStaticDappConfig());
  if (staticHealthEndpointConfirmed) {
    // loadStaticDappConfig validates this too, but retain the local check at
    // the fallback boundary so a future loader change cannot weaken the
    // profile/source decision made here.
    if (config.serverBacked !== false) {
      throw new Error(
        "Static WebApp configuration must set serverBacked to false when /api/health is absent",
      );
    }
    serverConfigAvailable = false;
  }
  return browserHealthFromStaticConfig(config, { healthView });
}

function addressSuggestionConfigs() {
  const transparentFormat = activeTransparentAddressFormat();
  return [
    {
      input: els.keplrSendRecipient,
      list: els.keplrSendRecipientSuggestions,
      kind: "transparent",
      label: transparentFormat === "evm" ? "EVM" : "transparent",
      format: transparentFormat,
    },
    {
      input: els.veiledTransferRecipient,
      list: els.veiledTransferRecipientSuggestions,
      kind: "shielded",
      label: "shielded",
    },
    {
      input: els.veiledWithdrawRecipient,
      list: els.veiledWithdrawRecipientSuggestions,
      kind: "transparent",
      label: transparentFormat === "evm" ? "EVM" : "transparent",
      format: transparentFormat,
      includeWallet: true,
    },
    {
      input: els.relayWithdrawRecipient,
      list: els.relayWithdrawRecipientSuggestions,
      kind: "transparent",
      label: transparentFormat === "evm" ? "EVM" : "transparent",
      format: transparentFormat,
      includeWallet: true,
    },
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
  if (kind === "shielded") {
    return isShieldedAddressBookCurrent()
      ? state.addressBook.shieldedByName[account.name] || ""
      : "";
  }
  return account.transparentAddress || "";
}

function transparentDisplayAddressFor(account) {
  return suggestedAddressFor(account || {}, {
    kind: "transparent",
    format: activeTransparentAddressFormat(),
  });
}

function connectedWalletAddressSuggestions(config) {
  if (!config?.includeWallet || config.kind !== "transparent") {
    return [];
  }

  if (config.format === "evm") {
    if (state.wallet.account) {
      return [
        {
          name: "My wallet",
          address: state.wallet.account,
        },
      ];
    }
    if (state.keplr.account) {
      try {
        return [
          {
            name: "My wallet",
            address: bech32AddressToEvm(state.keplr.account, accountPrefix()),
          },
        ];
      } catch {
        return [];
      }
    }
    return [];
  }

  if (!state.keplr.account) {
    return [];
  }

  const suggestions = [
    {
      name: "My wallet",
      address: state.keplr.account,
    },
  ];

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

// Recipient suggestions are display-only data scoped to the validated chain
// profile. Clear their selected values as well as their rendered list before
// a replacement profile can render, even when both profiles use the same
// address format or prefix. Resetting the address book also invalidates any
// in-flight shielded-address lookup from the former profile.
function clearProfileScopedRecipientSuggestions() {
  resetShieldedAddressBook();
  for (const config of addressSuggestionConfigs()) {
    if (config.input) config.input.value = "";
    if (config.list) config.list.replaceChildren();
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
    ...accounts.map((account) => ({
      name: account.name,
      address: suggestedAddressFor(account, config),
    })),
  ].filter((entry) => {
    if (!entry.address) return false;
    if (config.format === "evm" && !isEvmAddress(entry.address)) return false;
    const key = entry.address.toLowerCase();
    if (seenAddresses.has(key)) return false;
    seenAddresses.add(key);
    return true;
  });

  if (
    config.kind === "shielded" &&
    isShieldedAddressBookCurrent() &&
    state.addressBook.loadingShielded &&
    suggestions.length < accounts.length
  ) {
    appendAddressSuggestionEmpty(config, "Loading shielded addresses...");
  }

  if (
    config.kind === "shielded" &&
    isShieldedAddressBookCurrent() &&
    state.addressBook.shieldedError &&
    !suggestions.length
  ) {
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
    option.addEventListener("mousedown", (event) => {
      event.preventDefault();
      selectAddressSuggestion(config, suggestion.address);
    });
    option.addEventListener("keydown", (event) => {
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

async function ensureShieldedAddressBook(
  scope = ensureShieldedAddressBookScope(),
) {
  const { addressBook, generation } = scope;
  const missing = recipientTestAccounts().filter(
    (account) => !addressBook.shieldedByName[account.name],
  );
  if (!missing.length) return;
  if (shieldedAddressBookPromise) {
    await shieldedAddressBookPromise;
    return;
  }

  addressBook.loadingShielded = true;
  addressBook.shieldedError = "";
  renderVisibleAddressSuggestions();

  const load = Promise.allSettled(
    missing.map(async (account) => {
      const data = await api(`/api/wallet/${account.name}/show-address`);
      if (!isShieldedAddressBookScopeCurrent(scope)) {
        return;
      }
      const address = data.address || "";
      if (address) {
        addressBook.shieldedByName[account.name] = address;
      }
    }),
  );
  shieldedAddressBookPromise = load;

  const results = await load;
  if (!isShieldedAddressBookScopeCurrent(scope)) {
    return;
  }
  addressBook.loadingShielded = false;
  if (shieldedAddressBookPromise === load) {
    shieldedAddressBookPromise = null;
  }
  if (results.some((result) => result.status === "rejected")) {
    addressBook.shieldedError = "Unable to load shielded addresses";
  }
  renderVisibleAddressSuggestions();
}

function showAddressSuggestions(config) {
  if (!config?.input || !config?.list) return;
  renderAddressSuggestions(config);
  config.list.hidden = false;
  config.input.setAttribute("aria-expanded", "true");
  if (config.kind === "shielded") {
    const scope = ensureShieldedAddressBookScope();
    ensureShieldedAddressBook(scope).catch((error) =>
      recordShieldedAddressBookFailure(scope, error),
    );
  }
}

function setupAddressSuggestions() {
  for (const config of addressSuggestionConfigs()) {
    if (!config.input || !config.list) continue;
    const currentConfig = () =>
      addressSuggestionConfigs().find((next) => next.input === config.input) ||
      config;
    config.input.addEventListener("focus", () =>
      showAddressSuggestions(currentConfig()),
    );
    config.input.addEventListener("click", () =>
      showAddressSuggestions(currentConfig()),
    );
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

  document.addEventListener("pointerdown", (event) => {
    if (event.target.closest(".address-field")) return;
    hideAllAddressSuggestions();
  });
}

function resetMetaMaskSession() {
  state.wallet = defaultMetaMaskState();
}

function resetAuditorSession() {
  auditorSessionGeneration += 1;
  state.auditor = defaultAuditorState();

  if (els.auditorTestScalar) els.auditorTestScalar.textContent = "-";
  if (els.auditorEventsList) els.auditorEventsList.innerHTML = "";
  if (els.auditorOutputReports) {
    els.auditorOutputReports.replaceChildren();
    els.auditorOutputReports.hidden = true;
  }
  setAuditorValueTone(auditorDetailValueElements());
  for (const element of auditorDetailValueElements()) {
    element.textContent = "-";
  }
  if (els.auditorDecodeState) {
    els.auditorDecodeState.textContent = "Local admin disclosure material cleared.";
  }
  if (els.decodeAuditorTransfer) {
    els.decodeAuditorTransfer.disabled = true;
    els.decodeAuditorTransfer.textContent = "Decode";
  }
  if (els.refreshAuditorTransfers) {
    els.refreshAuditorTransfers.disabled = !serverFeature("auditorAdmin");
  }
  renderAuditorPagination();
}

function clearPrivacyOperationDrafts() {
  for (const input of [
    els.keplrSendAmount,
    els.keplrSendRecipient,
    els.keplrDepositAmount,
    els.veiledTransferAmount,
    els.veiledTransferRecipient,
    els.veiledDisclosurePubKey,
    els.veiledWithdrawAmount,
    els.veiledWithdrawRecipient,
    els.relayWithdrawAmount,
    els.relayWithdrawRecipient,
  ]) {
    if (input) input.value = "";
  }
  if (els.veiledDisclosureAdvanced) els.veiledDisclosureAdvanced.checked = false;
  if (els.veiledDisclosureMode) els.veiledDisclosureMode.value = "none";
  for (const checkbox of [
    els.veiledDisclosureAmount,
    els.veiledDisclosureFrom,
    els.veiledDisclosureTo,
  ]) {
    if (checkbox) checkbox.checked = false;
  }
  renderTransferDisclosureAdvanced();
  resetBatchTransferDraft();
  resetTransferPlannerFacts();
  hideAllAddressSuggestions();
}

function resetPrivacyEventSession() {
  privacyEventDisclosureGeneration += 1;
  // A profile or wallet reset invalidates every in-flight event-page request.
  // Without a new generation, its finally block can re-render controls for
  // the cleared session after the current UI has returned to page one.
  privacyEventsRefreshGeneration += 1;
  state.privacyEvents = defaultPrivacyEventsState();

  if (els.eventsList) els.eventsList.innerHTML = "";
  for (const element of [
    els.eventDetailType,
    els.eventDetailHeight,
    els.eventDetailTx,
    els.eventDetailTarget,
    els.eventDetailUserMode,
    els.eventDisclosurePlane,
  ]) {
    if (element) element.textContent = "-";
  }
  clearEventDisclosureResult();
  if (els.eventDisclosureState) {
    els.eventDisclosureState.textContent = "Privacy session cleared.";
  }
  clearEventBatchDisclosureReports();
  if (els.decodeEventDisclosure) els.decodeEventDisclosure.disabled = true;
  renderPrivacyEventPagination();
}

function resetBlockEventSession() {
  // Block events use the active profile's client transport just like the
  // privacy-event feed. Do not leave the prior profile's public transaction
  // history visible while the replacement profile is still loading.
  state.blockEvents = {
    events: [],
    error: "",
  };

  if (els.blockEventsList) els.blockEventsList.innerHTML = "";
  if (els.blockEventsState) {
    els.blockEventsState.textContent = "Profile changed; refresh events.";
  }
}

function resetKeplrSession() {
  invalidatePrivacySessionOperations();
  stopPreparedRelayReservationHeartbeat();
  advanceRelayWithdrawPayloadGeneration();
  const nextState = defaultKeplrState();
  nextState.relayWithdrawPayloadVersion = relayWithdrawPayloadGeneration;
  state.keplr = nextState;
  reservationManager = null;
  reservationManagerKey = "";
  reservationStore = null;
  reservationStoreKey = "";
  reservationWorkerID = "";
  walletNoteStore = null;
  walletNoteStoreKey = "";
  relayMetadataStore = null;
  relayMetadataStoreKey = "";
  batchTransferArtifactStore = null;
  batchTransferArtifactStoreKey = "";
  clearPrivacyOperationDrafts();
  // A status can contain the previous session's operation or failure result.
  // Reset it alongside the input drafts so a new account/profile never sees
  // stale transaction context before it has started an operation.
  els.keplrTxState.textContent = "Ready";
  resetPrivacyEventSession();
  resetBlockEventSession();
}

function resetWalletSession() {
  // Identity changes clear only this tab's memory. The old encrypted
  // namespace retains its reservation evidence for recovery; do not replan or
  // persist a relay payload after invalidating its privacy session.
  state.activeWallet = "";
  resetMetaMaskSession();
  resetKeplrSession();
  // Local auditor material is a separately gated local-admin capability. It
  // has no relationship to a wallet root signature, so clearing it here left
  // the Auditor panel blank after Keplr sign/keystore events despite its
  // same-origin admin endpoint being healthy. It is cleared on profile
  // changes and whenever the admin feature is unavailable instead.
  clearChainSafety();
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
  const metamaskConnected =
    activeWallet === "metamask" && Boolean(state.wallet.account);
  const keplrConnected =
    activeWallet === "keplr" && Boolean(state.keplr.account);
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

  els.sessionWallet.textContent = metamaskConnected
    ? "MetaMask"
    : keplrConnected
      ? "Keplr"
      : "Not connected";
  els.walletAccount.textContent = metamaskConnected
    ? shorten(state.wallet.account, 12, 10)
    : keplrConnected
      ? state.keplr.account
      : "Not connected";
  els.copyWalletAccount.disabled = !currentWalletAccountForCopy();
  els.walletChain.textContent = metamaskConnected
    ? state.wallet.chainId || "-"
    : keplrConnected
      ? activeKeplrChainInfo()?.chainId ||
        profile?.chainId ||
        state.config?.chainId ||
        "-"
      : "-";
  els.walletSignatureHash.textContent =
    metamaskConnected && state.wallet.signatureHash
      ? shorten(state.wallet.signatureHash, 14, 12)
      : keplrConnected && state.keplr.signatureHash
        ? `${shorten(state.keplr.signatureHash, 14, 12)}${state.keplr.verified ? " verified" : ""}`
        : "-";
  els.keplrName.textContent = privacyConnected
    ? state.keplr.name || (metamaskConnected ? "MetaMask" : "Keplr")
    : "-";
  els.keplrPubkey.textContent =
    privacyConnected && state.keplr.pubkeyHex
      ? shorten(state.keplr.pubkeyHex, 14, 12)
      : "-";
  els.keplrSignerCheck.textContent = privacyConnected
    ? state.keplr.signerCheck || "Checking..."
    : "-";
  els.keplrBalance.textContent = privacyConnected
    ? state.keplr.balance || "-"
    : "-";
  els.keplrFaucetHash.textContent =
    privacyConnected && state.keplr.faucetHash
      ? shorten(state.keplr.faucetHash, 14, 12)
      : "-";
  els.keplrFaucetSent.textContent = privacyConnected
    ? state.keplr.faucetSent || "-"
    : "-";
  els.keplrFaucetRecipient.textContent = privacyConnected
    ? state.keplr.faucetRecipient || "-"
    : "-";
  els.keplrShieldedAddress.textContent = privacyConnected
    ? state.keplr.shieldedAddress || "Not set up"
    : "Not set up";
  els.signSession.disabled = !connected;
  renderDappChainHint();
}

function renderWallet() {
  renderWalletSession();
}

function renderKeplr() {
  const connected = Boolean(state.keplr.account);
  const signerReady = connected && state.keplr.addressMatches;
  const veiledReady = signerReady && Boolean(state.keplr.rootSignatureBase64);
  renderWalletSession();
  els.myClairBalance.textContent = connected ? state.keplr.balance || "-" : "-";
  els.keplrDisclosurePubKey.textContent =
    state.keplr.disclosurePubKeyHex || "Setup Clairveil first";
  els.keplrSendHash.textContent = state.keplr.sendHash
    ? shorten(state.keplr.sendHash, 14, 12)
    : "-";
  els.keplrDepositHash.textContent = state.keplr.depositHash
    ? shorten(state.keplr.depositHash, 14, 12)
    : "-";
  els.keplrDepositHeight.textContent = state.keplr.depositHeight || "-";
  els.keplrTransferHash.textContent = state.keplr.transferHash
    ? shorten(state.keplr.transferHash, 14, 12)
    : "-";
  els.keplrBatchTransferHash.textContent = state.keplr.batchTransferHash
    ? shorten(state.keplr.batchTransferHash, 14, 12)
    : "-";
  els.keplrWithdrawHash.textContent = state.keplr.withdrawHash
    ? shorten(state.keplr.withdrawHash, 14, 12)
    : "-";
  els.keplrWithdrawHeight.textContent = state.keplr.withdrawHeight || "-";
  renderRelayerPanel();
  if (connected && !els.veiledWithdrawRecipient.value) {
    els.veiledWithdrawRecipient.value = state.keplr.account;
  }
  if (connected && !els.relayWithdrawRecipient.value) {
    els.relayWithdrawRecipient.value = state.keplr.account;
  }
  renderMyKeplrNotes();
  els.fundKeplr.disabled =
    isPrivacyValueActionInFlight() || !serverFeature("faucet") || !signerReady;
  els.setupKeplrPrivacy.disabled = !signerReady;
  els.copyKeplrDisclosurePubKey.disabled = !state.keplr.disclosurePubKeyHex;
  els.refreshWalletBalance.disabled = !connected;
  els.refreshClairBalance.disabled = !connected;
  const canScanNotes = signerReady && Boolean(state.keplr.rootSignatureBase64);
  const noteScanBusy = noteScanInFlight || noteScanResetInFlight;
  els.scanKeplrNotes.disabled = !canScanNotes || noteScanBusy;
  els.scanKeplrNotes.textContent = noteScanBusy
    ? "Scanning notes…"
    : state.keplr.scanError
      ? "Retry scan"
      : "Scan";
  els.resetKeplrNotes.hidden = !state.keplr.scanError;
  els.resetKeplrNotes.disabled =
    !canScanNotes ||
    !state.keplr.scanError ||
    noteScanBusy ||
    isPrivacyValueActionInFlight();
  updateAmountActionButtons({ signerReady, veiledReady });
  renderBatchTransferVisibility();
  renderEventDetail();
}

function updateAmountActionButtons(status = {}) {
  const connected = Boolean(state.keplr.account);
  const signerReady =
    status.signerReady ?? (connected && state.keplr.addressMatches);
  const veiledReady =
    status.veiledReady ??
    (
      signerReady &&
      Boolean(state.keplr.rootSignatureBase64) &&
      hasCompletedPrivacyNoteScan()
  );
  const chainSafetyReady = isChainSafetyReady();
  const spendChainReady = isSpendChainReady();
  const privacyActionBusy = isPrivacyValueActionInFlight();
  els.sendFromKeplr.disabled =
    privacyActionBusy ||
    !signerReady ||
    !hasPositiveUclairInput(els.keplrSendAmount) ||
    !isSendRecipientForWallet(
      els.keplrSendRecipient.value,
      state.activeWallet || activeWalletKind(),
    );
  els.depositFromKeplr.disabled =
    privacyActionBusy ||
    depositInFlight ||
    !signerReady ||
    !chainSafetyReady ||
    !hasPositiveUclairInput(els.keplrDepositAmount) ||
    !hasDepositProofProvider();
  els.transferFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady || !spendChainReady || !hasPositiveUclairInput(els.veiledTransferAmount);
  els.withdrawFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady ||
    !spendChainReady ||
    !hasPositiveUclairInput(els.veiledWithdrawAmount) ||
    !isSendRecipientForWallet(
      els.veiledWithdrawRecipient.value,
      state.activeWallet || activeWalletKind(),
    );
  els.relayWithdrawFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady ||
    !spendChainReady ||
    !hasPositiveUclairInput(els.relayWithdrawAmount) ||
    !isSendRecipientForWallet(
      els.relayWithdrawRecipient.value,
      state.activeWallet || activeWalletKind(),
    );
  renderBatchTransferPreview();
}

function appendPrivacyNoteRow(
  container,
  note,
  { statusLabel = noteStatusLabel(note) } = {},
) {
  const row = document.createElement("article");
  row.className = "note-row";
  row.classList.toggle("helper-note", isHelperNote(note));

  const amount = document.createElement("strong");
  amount.textContent = `${note.amount}${noteAssetDenom(note) || "unknown-asset"}`;
  const status = document.createElement("span");
  status.className = noteStatusClass(note);
  status.textContent = statusLabel;
  const nullifier = document.createElement("code");
  nullifier.textContent = shorten(note.nullifier, 12, 10);

  row.append(amount, status, nullifier);
  container.append(row);
}

function localSignerNoteStatusLabel(note) {
  const status = String(note?.status || "").toLowerCase();
  if (status === "spendable") return "Spendable";
  if (status === "spent") return "Spent";
  return "Unknown";
}

function isRenderableLocalSignerNote(note) {
  // Local Signer notes come from the trusted local CLI route, whose
  // `status` already includes its nullifier check. Browser-wallet notes need
  // the separate nullifier_status proof below, but applying that requirement
  // here would hide every valid local CLI note because that field is absent.
  return ["spendable", "spent"].includes(
    String(note?.status || "").toLowerCase(),
  );
}

function renderMyKeplrNotes() {
  const chainSafetyReady = isChainSafetyReady();
  const notesSyncReady = hasCompletedPrivacyNoteScan();
  els.myKeplrSpendable.textContent = chainSafetyReady && notesSyncReady
    ? state.keplr.notesSummary || "-"
    : "Sync unavailable";
  els.myKeplrSpendableOnly.checked = state.keplr.showSpendableOnly;
  els.myKeplrNotesList.innerHTML = "";
  renderReservationManualReviews();

  if (!state.keplr.account) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "Wallet not connected";
    els.myKeplrNotesList.append(empty);
    return;
  }

  if (!chainSafetyReady) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.chainSafety.status === "checking"
      ? "Verifying current chain configuration"
      : "Notes are unavailable until current chain configuration is verified";
    els.myKeplrNotesList.append(empty);
    return;
  }

  // A successful typed scan may be followed by a failed nullifier or
  // reservation reconciliation. Those notes are recovery evidence, even
  // though they must remain unavailable for spending until the retry clears
  // scanError. Only hide the list when no typed scan recovered anything.
  if (!notesSyncReady && !state.keplr.notesScanned) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.keplr.scanError || "Not scanned";
    els.myKeplrNotesList.append(empty);
    return;
  }

  if (!notesSyncReady && state.keplr.scanError) {
    const warning = document.createElement("p");
    warning.className = "note-sync-warning";
    warning.textContent = state.keplr.scanError;
    els.myKeplrNotesList.append(warning);
  }

  // ConfirmedSpent is durable local evidence that the input was consumed.
  // It is retained in encrypted reservation storage for reconciliation, but
  // it is not useful inventory and should not remain in the visible note list.
  const visibleNotes = state.keplr.notes.filter(
    (note) => !noteHasConfirmedSpentReservation(note),
  );
  const valueNotes = visibleNotes.filter(
    (note) =>
      !isZeroAmountNote(note) &&
      !isUnverifiedNote(note) &&
      noteUsesCurrentAsset(note),
  );
  const otherAssetNotes = visibleNotes.filter(
    (note) =>
      !isZeroAmountNote(note) &&
      !isUnverifiedNote(note) &&
      !noteUsesCurrentAsset(note),
  );
  const unverifiedNotes = visibleNotes.filter(
    (note) => !isZeroAmountNote(note) && isUnverifiedNote(note),
  );
  const notes = state.keplr.showSpendableOnly
    ? valueNotes.filter(
        (note) =>
          isCurrentAssetSpendableNote(note) || noteHasBlockingReservation(note),
      )
    : [...valueNotes, ...unverifiedNotes, ...otherAssetNotes];

  if (notes.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    const hiddenZeroCount = state.keplr.notes.filter(isZeroAmountNote).length;
    empty.textContent = state.keplr.showSpendableOnly
      ? hiddenZeroCount
        ? `No value spendable notes (${hiddenZeroCount} zero notes hidden)`
        : "No spendable notes"
      : hiddenZeroCount
        ? `No value notes (${hiddenZeroCount} zero notes hidden)`
        : "No notes";
    els.myKeplrNotesList.append(empty);
    return;
  }

  for (const note of notes) appendPrivacyNoteRow(els.myKeplrNotesList, note);
}

// ManualReview is a durable safety lock, not a generic retry prompt. Keep the
// evidence shown to the reviewing wallet limited to identifiers already held
// in the local reservation/scan state, but show enough of it for an operator
// to distinguish the operation and verify the matching explorer/relayer
// records before explicitly resolving it. The resolver below still re-reads
// all of this evidence and the chain state immediately before its transition.
function manualReviewRecordsForDisplay(records = []) {
  const stored = Array.isArray(records) ? records : [];
  const ids = stored
    .map((record) => String(record?.reservation_id || ""))
    .filter(Boolean);
  if (!ids.length) return stored;
  const current = reservationRecordsByID();
  const authoritative = ids.map((id) => current.get(id)).filter(Boolean);
  // Persisted relay snapshots intentionally strip arbitrary reservation
  // metadata. Prefer the in-memory records read from the active reservation
  // store when every operation member is available, otherwise keep the
  // snapshot evidence rather than mixing two different operations.
  return authoritative.length === ids.length ? authoritative : stored;
}

function manualReviewDisplayEvidence(
  records = [],
  { payloadHash = "" } = {},
) {
  const reservationIDSet = new Set(
    records
      .map((record) => String(record?.reservation_id || ""))
      .filter(Boolean),
  );
  const unique = (values) => [...new Set(values.filter(Boolean).map(String))];
  const operationID = reservationOperationID(records[0]);
  const reservationIDs = unique(
    records.map((record) => record.reservation_id),
  );
  const chainIdentities = unique(
    records.flatMap((record) => [
      record.submitted_tx_hash,
      record.tx_bytes_hash,
      record.sign_doc_hash,
      record.metadata?.tx_hash_checked,
    ]),
  );
  const payloadHashes = unique(
    records.flatMap((record) => [
      record.payload_hash,
      record.payloadHash,
      record.metadata?.payload_hash,
      record.metadata?.payloadHash,
      payloadHash,
    ]),
  );
  const inputNullifiers = unique(
    Object.entries(state.keplr.noteReservationByNullifier || {})
      .filter(([, reservation]) =>
        reservationIDSet.has(String(reservation?.reservation_id || "")),
      )
      .map(([nullifier]) => nullifier),
  );
  const noBroadcastEvidence =
    records.length > 0 &&
    records.every(reservationHasDurableNoBroadcastEvidence);

  return {
    operationID,
    reservationIDs,
    chainIdentities,
    payloadHashes,
    inputNullifiers,
    noBroadcastEvidence,
  };
}

function appendManualReviewEvidence(details, records, options = {}) {
  const evidence = manualReviewDisplayEvidence(
    manualReviewRecordsForDisplay(records),
    options,
  );
  const append = (text, className = "") => {
    const item = document.createElement("code");
    if (className) item.className = className;
    item.textContent = text;
    details.append(item);
  };
  append(
    `Operation: ${shorten(evidence.operationID, 14, 12)} · reservations: ${evidence.reservationIDs.map((id) => shorten(id, 10, 8)).join(", ") || "unavailable"}`,
    "reservation-review-evidence",
  );
  append(
    evidence.chainIdentities.length
      ? `Broadcast identity: ${evidence.chainIdentities.map((identity) => shorten(identity, 14, 12)).join(", ")}`
      : evidence.noBroadcastEvidence
        ? "Broadcast identity: no durable broadcast attempt recorded"
        : "Broadcast identity: unavailable; keep the operation locked",
    "reservation-review-evidence",
  );
  if (evidence.payloadHashes.length) {
    append(
      `Payload hash: ${evidence.payloadHashes.map((hash) => shorten(hash, 14, 12)).join(", ")}`,
      "reservation-review-evidence",
    );
  }
  append(
    evidence.inputNullifiers.length
      ? `Input nullifiers: ${evidence.inputNullifiers.map((nullifier) => shorten(nullifier, 12, 10)).join(", ")}`
      : "Input nullifiers: rescan Notes before resolving if unavailable",
    "reservation-review-evidence",
  );
}

function relayManualReviewRecoveryMessage(snapshot) {
  const records = manualReviewRecordsForDisplay(
    snapshot?.reservation?.reservations || [],
  );
  if (!records.length) {
    return "The local reservation evidence is unavailable. Scan Notes before checking this relay handoff again.";
  }
  if (
    records.some(
      (record) => record.status !== reservationStatuses.ManualReview,
    )
  ) {
    return "The relay reservation status changed while recovery was checked. Refresh Notes before taking another action.";
  }
  if (!relaySnapshotExpiresAtUnix(snapshot)) {
    return "This older relay handoff has no durable expiry record, so it cannot be safely released. Keep it locked and verify the original relayer or chain records.";
  }
  if (records.every(reservationHasDurableNoBroadcastEvidence)) {
    return "The handoff was not broadcast, but its expiry or every input nullifier could not yet be confirmed. Scan Notes, then check recovery again.";
  }
  if (records.every((record) => !reservationHasQueryableTransactionIdentity(record))) {
    return "This relay handoff has no queryable broadcast identity and no durable no-broadcast record. It may have reached a relayer, so it remains locked; this control cannot cancel it.";
  }
  return "The relay transaction outcome or its input-nullifier evidence is not yet conclusive. The handoff remains locked; refresh Notes and verify the relayer or chain record before trying again.";
}

function manualReviewResolutionErrorMessage(error) {
  const message = String(error?.message || "");
  if (/Scan Notes before cancelling this batch checkpoint/.test(message)) {
    return "이 checkpoint를 취소하려면 먼저 Scan Notes를 완료해 모든 input note를 다시 확인해야 합니다.";
  }
  if (/Every nullifier must be explicitly confirmed unspent before cancelling this batch checkpoint/.test(message)) {
    return "input note가 아직 unspent인지 확인할 수 없습니다. Scan Notes 후 다시 시도하세요.";
  }
  if (/recovered batch has broadcast evidence/.test(message)) {
    return "이전 batch에서 broadcast 증거가 발견됐습니다. checkpoint를 취소하지 않았으며, 거래 결과를 먼저 확인해야 합니다.";
  }
  if (/recovered batch reservation changed|Reservation evidence changed while cancelling/.test(message)) {
    return "checkpoint 예약 상태가 바뀌었습니다. Scan Notes 후 현재 상태를 다시 검토하세요.";
  }
  if (/unresolved batch reservation changed|unresolved batch could not enter ManualReview/.test(message)) {
    return "이전 batch 예약 상태가 바뀌었거나 검토 상태로 전환할 수 없습니다. Scan Notes 후 다시 확인하세요.";
  }
  if (/unresolved batch has a queryable transaction identity/.test(message)) {
    return "이전 batch의 query 가능한 거래 식별자가 발견됐습니다. 예약을 취소하지 않았으며, 거래 결과를 먼저 확인해야 합니다.";
  }
  if (/Scan Notes before resolving/.test(message)) {
    return "예약을 해제하려면 먼저 Scan Notes를 완료해 모든 input note를 다시 확인해야 합니다.";
  }
  if (/Every nullifier must be explicitly confirmed unspent/.test(message)) {
    return "input note가 아직 unspent인지 확인할 수 없습니다. Scan Notes 후 다시 확인하세요.";
  }
  if (/broadcast evidence and cannot be explicitly cancelled/.test(message)) {
    return "새 broadcast 기록이 확인되었습니다. 예약을 취소하지 않았으며, 현재 거래 상태를 확인해야 합니다.";
  }
  if (/Transaction absence or failure is not confirmed/.test(message)) {
    return "제출된 거래의 결과를 확인할 수 없습니다. 예약을 계속 잠근 상태로 유지했습니다.";
  }
  return privacyRecoveryErrorMessage(
    error,
    "예약 검토를 완료하지 않았습니다. Scan Notes와 거래 기록을 확인한 뒤 다시 시도하세요.",
  );
}

function renderReservationManualReviews() {
  if (!els.reservationReviewList) return;
  const records = Array.isArray(state.keplr.manualReviewReservations)
    ? state.keplr.manualReviewReservations
    : [];
  const byOperation = new Map();
  for (const record of records) {
    const operationID = reservationOperationID(record);
    const grouped = byOperation.get(operationID) || [];
    grouped.push(record);
    byOperation.set(operationID, grouped);
  }
  els.reservationReviewList.hidden = byOperation.size === 0;
  els.reservationReviewList.replaceChildren();
  for (const [operationID, operationRecords] of byOperation) {
    const first = operationRecords[0];
    const row = document.createElement("div");
    row.className = "reservation-review-item";
    const details = document.createElement("div");
    details.className = "reservation-review-details";
    const title = document.createElement("strong");
    title.textContent = `${first.kind || "privacy operation"} requires review`;
    const reason = document.createElement("span");
    reason.textContent =
      first.metadata?.reconcile_reason ||
      "broadcast outcome is unresolved";
    const tx = document.createElement("code");
    const txIdentity = first.submitted_tx_hash || first.tx_bytes_hash || "";
    tx.textContent = txIdentity ? shorten(txIdentity, 14, 12) : "No tx identity";
    details.append(title, reason, tx);
    appendManualReviewEvidence(details, operationRecords);
    const action = document.createElement("button");
    action.type = "button";
    action.textContent = "검토";
    action.addEventListener("click", () => {
      openReservationReviewDialog(operationID, operationRecords);
    });
    row.append(details, action);
    els.reservationReviewList.append(row);
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
  renderRelayerPanel();
  if (!accounts.length) {
    els.keplrSendRecipient.value = "";
  } else if (
    !isSendRecipientForWallet(els.keplrSendRecipient.value) &&
    selectedTransparentAddress
  ) {
    els.keplrSendRecipient.value = selectedTransparentAddress;
  }
  renderVisibleAddressSuggestions();
}

function selectedProfileFromConfig(config) {
  const profiles = config?.chainProfiles || [];
  return (
    profiles.find((profile) => profile.id === state.selectedChainProfileId) ||
    profiles.find((profile) => profile.id === config?.activeChainProfileId) ||
    profiles[0] ||
    null
  );
}

async function clearPrivacySessionForProfileChange(
  previousProfile,
  nextProfile,
  { nextConfig = state.config } = {},
) {
  if (
    !previousProfile ||
    !nextProfile ||
    profileSessionFingerprint(previousProfile) ===
      profileSessionFingerprint(nextProfile, { config: nextConfig })
  ) {
    return null;
  }

  // Public event/auditor views share the profile boundary too. Advancing the
  // session generation unconditionally prevents an old background request
  // from applying its result after a refreshed config changes this profile.
  clearProfileScopedRecipientSuggestions();
  resetWalletSession();
  resetAuditorSession();
  return null;
}

async function renderHealth(data, { healthView = null } = {}) {
  if (healthView !== null) assertHealthViewCurrent(healthView);
  const previousProfile = activeChainProfile();
  const nextProfile = selectedProfileFromConfig(data.config);
  const profileChangeCleanupError = await clearPrivacySessionForProfileChange(
    previousProfile,
    nextProfile,
    { nextConfig: data.config },
  );
  if (healthView !== null) assertHealthViewCurrent(healthView);
  state.config = data.config;
  state.chainProfiles = data.config?.chainProfiles || [];
  if (
    !state.selectedChainProfileId ||
    !state.chainProfiles.some(
      (profile) => profile.id === state.selectedChainProfileId,
    )
  ) {
    state.selectedChainProfileId =
      data.config?.activeChainProfileId || state.chainProfiles[0]?.id || "";
  }
  state.accounts = data.accounts || [];
  if (
    !state.accounts.some((account) => account.name === state.selectedAccount)
  ) {
    state.selectedAccount = state.accounts[0]?.name || "alice";
  }
  invalidateLocalAccountView();
  invalidateRelayerView();
  ensureShieldedAddressBookScope();

  renderServerFeatureVisibility();
  els.modeBadge.textContent =
    data.config?.modeLabel ||
    (localTestBackendEnabled() ? "Local Note Test Web" : "Public Node DApp");
  els.modeBadge.classList.toggle("public-mode", !localTestBackendEnabled());
  els.localHome.textContent =
    data.config?.localSignerHome || data.config?.home || "-";
  els.chainId.textContent =
    data.status?.node_info?.network || data.config?.chainId || "-";
  els.blockHeight.textContent =
    data.status?.sync_info?.latest_block_height || "-";
  els.leafCount.textContent = data.tree?.leaf_count || "-";
  els.restState.textContent = data.tree ? "Online" : "Offline";
  renderDappChainSelect();
  renderChainDependentUi();
  renderAccounts();
  renderWalletSession();
  const addressBookScope = ensureShieldedAddressBookScope();
  ensureShieldedAddressBook(addressBookScope).catch((error) => {
    if (healthView !== null && !isHealthViewCurrent(healthView)) return;
    recordShieldedAddressBookFailure(addressBookScope, error);
  });
  if (profileChangeCleanupError) {
    throw new Error(
      `The chain profile changed and the previous privacy session was cleared, but relay reservation cleanup needs reconciliation: ${profileChangeCleanupError.message}`,
    );
  }
}

async function refreshHealth() {
  const healthView = beginHealthView();
  const data = await withHealthViewGuard(
    healthView,
    () => loadDappHealth({ healthView }),
  );
  await renderHealth(data, { healthView });
  assertHealthViewCurrent(healthView);
  await withHealthViewGuard(
    healthView,
    () => refreshChainSafety({ force: true }),
  ).catch((error) => {
    if (error?.privacySessionInvalidated) throw error;
  });
  assertHealthViewCurrent(healthView);
  if (serverFeature("localSigners")) {
    try {
      await refreshSelectedAccount({ healthView });
    } catch (error) {
      if (!error?.privacySessionInvalidated) {
        if (error?.statusCode !== 403) {
          throw error;
        }
        renderLocalSignerUnavailable(error);
      }
    }
  }
  const tasks = [refreshEvents({ allowFailure: true, healthView })];
  if (serverFeature("relayer")) {
    tasks.push(refreshRelayerAccount({ healthView }));
  }
  if (serverFeature("auditorAdmin")) {
    tasks.push(
      refreshAuditorTransfers({ healthView }),
      refreshAuditorTestScalar({ healthView }),
    );
  }
  await withHealthViewGuard(healthView, () => Promise.allSettled(tasks));
}

async function refreshSelectedAccount({ healthView = null } = {}) {
  assertOptionalHealthViewCurrent(healthView);
  const accountView = beginLocalAccountView();
  assertLocalAccountViewCurrent(accountView);
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

  els.transparentAddress.textContent =
    transparentDisplayAddressFor(account) || "-";
  els.shieldedAddress.textContent = "Loading...";
  els.balanceValue.textContent = "Loading...";

  const [shielded, balance] = await withLocalAccountViewGuard(
    accountView,
    () =>
      Promise.all([
        api(`/api/wallet/${account.name}/show-address`),
        clairveilBrowserClient().getBalances(account.transparentAddress),
      ]),
  );
  assertLocalAccountViewCurrent(accountView);
  assertOptionalHealthViewCurrent(healthView);

  els.shieldedAddress.textContent = shielded.address || "-";
  els.balanceValue.textContent =
    (balance.balances || [])
      .map((coin) => `${coin.amount}${coin.denom}`)
      .join(", ") || zeroCoinText();

  await refreshNotes({ accountView, healthView });
  assertLocalAccountViewCurrent(accountView);
  assertOptionalHealthViewCurrent(healthView);
}

async function refreshRelayerAccount({ healthView = null } = {}) {
  assertOptionalHealthViewCurrent(healthView);
  const relayerView = beginRelayerView();
  assertRelayerViewCurrent(relayerView);
  const relayer = localRelayerAccount();
  state.relayer.error = "";
  if (!relayer?.transparentAddress || !serverFeature("relayer")) {
    state.relayer.balance = "";
    renderRelayerPanel();
    return;
  }

  state.relayer.balance = "Loading...";
  renderRelayerPanel();
  try {
    const balance = await withRelayerViewGuard(
      relayerView,
      () => clairveilBrowserClient().getBalances(relayer.transparentAddress),
    );
    assertRelayerViewCurrent(relayerView);
    assertOptionalHealthViewCurrent(healthView);
    state.relayer.balance = formatBalances(balance.balances);
  } catch (error) {
    assertRelayerViewCurrent(relayerView);
    assertOptionalHealthViewCurrent(healthView);
    state.relayer.balance = "";
    state.relayer.error = error.message;
  }
  assertRelayerViewCurrent(relayerView);
  assertOptionalHealthViewCurrent(healthView);
  renderRelayerPanel();
}

async function refreshWalletBalance(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const account = state.keplr.account;
  const evmMode = isEvmTransparentMode();
  const evmAccount = state.wallet.account;
  if (!account) return;
  if (evmMode) {
    if (!evmAccount) return;
    const balanceHex = await withPrivacySessionGuard(session, () =>
      clairveilBrowserClient().evmJsonRpc("eth_getBalance", [
        evmAccount,
        "latest",
      ]),
    );
    assertPrivacySessionCurrent(session);
    state.keplr.balance = formatBalances([
      {
        denom: baseDenom(),
        amount: BigInt(balanceHex || "0x0").toString(),
      },
    ]);
  } else {
    const data = await withPrivacySessionGuard(session, () =>
      clairveilBrowserClient().getBalances(account),
    );
    assertPrivacySessionCurrent(session);
    state.keplr.balance = formatBalances(data.balances);
  }
  assertPrivacySessionCurrent(session);
  renderKeplr();
}

async function refreshNotes(
  {
    session = beginPrivacySessionOperation(),
    accountView = beginLocalAccountView(),
    healthView = null,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  assertLocalAccountViewCurrent(accountView);
  assertOptionalHealthViewCurrent(healthView);
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
  if (!isChainSafetyReady()) {
    els.spendableTotal.textContent = "Sync unavailable";
    els.notesList.innerHTML = "";
    const unavailable = document.createElement("p");
    unavailable.className = "empty";
    unavailable.textContent =
      "Notes are unavailable until current chain configuration is verified.";
    els.notesList.append(unavailable);
    return;
  }
  els.notesList.textContent = "Scanning...";
  const data = await withLocalAccountViewGuard(accountView, () =>
    withPrivacySessionGuard(session, () =>
      api(`/api/wallet/${account.name}/notes`),
    ),
  );
  assertPrivacySessionCurrent(session);
  assertLocalAccountViewCurrent(accountView);
  assertOptionalHealthViewCurrent(healthView);
  els.notesList.innerHTML = "";
  const notes = (data.notes || []).filter(isRenderableLocalSignerNote);
  // `total_spendable` is intentionally denomination-neutral in the CLI JSON
  // contract. Do not append the active denom to it: a mixed-asset wallet
  // would then advertise another asset as spendable uclair.
  const spendableTotal = notes
    .filter(
      (note) =>
        String(note?.status || "").toLowerCase() === "spendable" &&
        noteUsesCurrentAsset(note),
    )
    .reduce((total, note) => total + noteAmountValue(note), 0n);
  els.spendableTotal.textContent = `${spendableTotal}${baseDenom()}`;
  if (notes.length === 0) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = "No notes";
    els.notesList.append(empty);
    return;
  }

  for (const note of notes.slice(0, 8)) {
    appendPrivacyNoteRow(els.notesList, note, {
      statusLabel: localSignerNoteStatusLabel(note),
    });
  }
}

async function refreshEvents(
  {
    allowFailure = false,
    session = beginPrivacySessionOperation(),
    healthView = null,
    page = state.privacyEvents.page,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  assertOptionalHealthViewCurrent(healthView);
  const requestedPage = Number(page);
  if (!Number.isSafeInteger(requestedPage) || requestedPage < 1) {
    throw new Error("privacy event page must be a positive integer");
  }
  const refreshGeneration = privacyEventsRefreshGeneration + 1;
  privacyEventsRefreshGeneration = refreshGeneration;
  state.privacyEvents.pageLoading = true;
  renderPrivacyEventPagination();
  const client = clairveilBrowserClient();
  try {
    const [privacyResult, blockResult] = await Promise.allSettled([
      client.fetchPrivacyEvents({
        page: requestedPage,
        limit: privacyEventsPageLimit,
      }),
      client.fetchBlockEvents(30),
    ]);
    assertPrivacySessionCurrent(session);
    assertOptionalHealthViewCurrent(healthView);

    if (refreshGeneration !== privacyEventsRefreshGeneration) return;

    if (privacyResult.status === "rejected") {
      state.privacyEvents.events = [];
      state.privacyEvents.loadError = browserDataLoadErrorMessage(
        privacyResult.reason,
      );
      state.blockEvents.events = [];
      state.blockEvents.error =
        blockResult.status === "rejected"
          ? browserDataLoadErrorMessage(blockResult.reason)
          : "";
      renderPrivacyEvents();
      renderEventDetail();
      renderBlockEvents();
      if (allowFailure) return;
      throw privacyResult.reason;
    }

    state.privacyEvents.events = privacyResult.value.events || [];
    state.privacyEvents.page = Number(privacyResult.value.page ?? requestedPage);
    state.privacyEvents.hasMore = Boolean(
      privacyResult.value.has_more ?? privacyResult.value.hasMore,
    );
    state.privacyEvents.loadError = "";
    if (blockResult.status === "fulfilled") {
      state.blockEvents.events = blockResult.value.events || [];
      state.blockEvents.error = "";
    } else {
      state.blockEvents.events = [];
      state.blockEvents.error = browserDataLoadErrorMessage(blockResult.reason);
    }

    if (
      state.privacyEvents.selectedEventKey &&
      !state.privacyEvents.events.some(
        (event) => privacyEventKey(event) === state.privacyEvents.selectedEventKey,
      )
    ) {
      privacyEventDisclosureGeneration += 1;
      state.privacyEvents.selectedEventKey = "";
      state.privacyEvents.selectedTxHash = "";
      state.privacyEvents.decoded = null;
      state.privacyEvents.batchDecoded = [];
      state.privacyEvents.error = "";
    }
    renderPrivacyEvents();
    renderEventDetail();
    renderBlockEvents();
  } finally {
    if (refreshGeneration === privacyEventsRefreshGeneration) {
      state.privacyEvents.pageLoading = false;
      renderPrivacyEventPagination();
    }
  }
}

async function refreshBlockEvents(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  try {
    const data = await clairveilBrowserClient().fetchBlockEvents(30);
    assertPrivacySessionCurrent(session);
    state.blockEvents.events = data.events || [];
    state.blockEvents.error = "";
  } catch (error) {
    assertPrivacySessionCurrent(session);
    state.blockEvents.events = [];
    state.blockEvents.error = error.message;
  }
  renderBlockEvents();
}

function disclosureTargetMatches(event) {
  const target = eventAttribute(event, "user_disclosure_target_pubkey");
  return Boolean(
    target &&
    state.keplr.disclosurePubKeyHex &&
    target.toLowerCase() === state.keplr.disclosurePubKeyHex.toLowerCase(),
  );
}

function isPublicDisclosureEvent(event) {
  return Boolean(
    event?.event_type === "shielded_transfer" &&
    eventAttribute(event, "user_disclosure_mode") ===
      "USER_DISCLOSURE_MODE_PUBLIC" &&
    eventAttribute(event, "user_disclosure_payload"),
  );
}

function isCosmosProfile() {
  return activeChainProfile()?.transport !== "evm";
}

function hasSelfViewDisclosureEvent(event) {
  return Boolean(
    event?.event_type === "shielded_transfer" &&
    eventAttribute(event, "self_view_disclosure_payload"),
  );
}

function isBatchTransferEvent(event) {
  return event?.event_type === "batch_transfer";
}

function batchSelfViewDecoderReady() {
  return Boolean(
    isCosmosProfile() &&
      state.keplr.account &&
      state.keplr.pubkeyHex &&
      state.keplr.rootSignatureBase64,
  );
}

function canDecodeUserEventDisclosure(event) {
  if (!event || event.event_type !== "shielded_transfer") return false;
  if (isPublicDisclosureEvent(event)) return true;
  return disclosureTargetMatches(event);
}

function canDecodeSelfViewDisclosure(event) {
  return Boolean(
    hasSelfViewDisclosureEvent(event) && batchSelfViewDecoderReady(),
  );
}

function selectedEventDisclosurePlane(event) {
  if (!event) return "-";
  if (isBatchTransferEvent(event)) {
    return batchSelfViewDecoderReady() ? "self-view" : "-";
  }
  if (event.event_type !== "shielded_transfer") return "-";
  if (canDecodeUserEventDisclosure(event)) return "user";
  if (canDecodeSelfViewDisclosure(event)) return "self-view";
  if (eventAttribute(event, "user_disclosure_payload")) return "user";
  if (hasSelfViewDisclosureEvent(event)) return "self-view";
  return "-";
}

function canDecodeEventDisclosure(event) {
  if (isBatchTransferEvent(event)) return batchSelfViewDecoderReady();
  return (
    canDecodeUserEventDisclosure(event) || canDecodeSelfViewDisclosure(event)
  );
}

function eventDisclosureStatus(event) {
  if (!event) return "Select an event.";
  if (isBatchTransferEvent(event)) {
    if (!isCosmosProfile()) {
      return "Batch self-view disclosure는 현재 Cosmos profile에서만 조회할 수 있습니다.";
    }
    if (!batchSelfViewDecoderReady()) {
      return "Setup Clairveil 후 sender self-view disclosure를 조회할 수 있습니다.";
    }
    return "검증된 privacy-scan-v2 output을 조회해 self-view disclosure가 있는 각 batch output만 표시합니다.";
  }
  if (event.event_type !== "shielded_transfer")
    return "Disclosure 조회는 shielded transfer에서만 가능합니다.";
  if (
    canDecodeSelfViewDisclosure(event) &&
    !canDecodeUserEventDisclosure(event)
  ) {
    return "Sender self-view disclosure입니다. 내 wallet material로 조회할 수 있습니다.";
  }
  if (hasSelfViewDisclosureEvent(event) && !isCosmosProfile()) {
    return "Self-view disclosure는 현재 EVM profile에서 조회하지 않습니다.";
  }
  const mode = eventAttribute(event, "user_disclosure_mode");
  const target = eventAttribute(event, "user_disclosure_target_pubkey");
  const payload = eventAttribute(event, "user_disclosure_payload");
  if (!payload) {
    if (hasSelfViewDisclosureEvent(event)) {
      return "Cosmos sender self-view disclosure가 있습니다. Setup Clairveil 후 조회할 수 있습니다.";
    }
    return "이 transfer에는 user disclosure payload가 없습니다.";
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
    if (hasSelfViewDisclosureEvent(event)) {
      return state.keplr.rootSignatureBase64
        ? "User disclosure 대상은 아니지만 sender self-view로 조회할 수 있습니다."
        : "User disclosure 대상은 아닙니다. Setup Clairveil 후 sender self-view 조회를 시도할 수 있습니다.";
    }
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
    const canSelect =
      event.event_type === "shielded_transfer" || isBatchTransferEvent(event);
    const eventKey = privacyEventKey(event);
    const row = document.createElement("button");
    row.type = "button";
    row.className = "event-row";
    row.classList.toggle(
      "selected",
      Boolean(eventKey) && eventKey === state.privacyEvents.selectedEventKey,
    );
    row.disabled = !canSelect || !eventKey;
    if (!row.disabled) {
      row.addEventListener("click", () => selectPrivacyEvent(event));
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

function renderPrivacyEventPagination() {
  if (els.eventsPageState) {
    els.eventsPageState.textContent = `Page ${state.privacyEvents.page}${state.privacyEvents.hasMore ? " · newer events available" : ""}`;
  }
  if (els.previousEventsPage) {
    els.previousEventsPage.disabled =
      state.privacyEvents.pageLoading || state.privacyEvents.page <= 1;
  }
  if (els.nextEventsPage) {
    els.nextEventsPage.disabled =
      state.privacyEvents.pageLoading || !state.privacyEvents.hasMore;
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

function privacyEventKey(event) {
  try {
    return privacyEventSelectionKey(event);
  } catch {
    // A malformed event must not become selectable or match an existing
    // selection. The server is still responsible for reporting its error.
    return "";
  }
}

function selectPrivacyEvent(event) {
  const selectedEventKey = privacyEventKey(event);
  if (!selectedEventKey) return;
  privacyEventDisclosureGeneration += 1;
  state.privacyEvents.selectedEventKey = selectedEventKey;
  state.privacyEvents.selectedTxHash = event.tx_hash_hex;
  state.privacyEvents.decoded = null;
  state.privacyEvents.batchDecoded = [];
  state.privacyEvents.error = "";
  renderPrivacyEvents();
  renderEventDetail();
}

function selectedPrivacyEvent() {
  return state.privacyEvents.events.find(
    (event) => privacyEventKey(event) === state.privacyEvents.selectedEventKey,
  );
}

function clearEventDisclosureResult() {
  els.eventDisclosurePolicy.textContent = "-";
  els.eventDisclosureOutputIndex.textContent = "-";
  els.eventDisclosureCommitment.textContent = "-";
  els.eventDisclosureDigest.textContent = "-";
  els.eventDisclosureVerified.textContent = "-";
  els.eventDisclosureFields.textContent = "-";
  els.eventDisclosureAmount.textContent = "-";
  els.eventDisclosureAssetDenom.textContent = "-";
  els.eventDisclosureFrom.textContent = "-";
  els.eventDisclosureTo.textContent = "-";
}

function clearEventBatchDisclosureReports() {
  if (!els.eventDisclosureReports) return;
  els.eventDisclosureReports.replaceChildren();
  els.eventDisclosureReports.hidden = true;
}

function renderEventBatchDisclosureReports(reports) {
  if (!els.eventDisclosureReports) return;
  els.eventDisclosureReports.replaceChildren();
  els.eventDisclosureReports.hidden = reports.length === 0;
  for (const report of reports) {
    const summary = report?.summary || {};
    const output = document.createElement("article");
    output.className = "audit-output-report";
    const heading = document.createElement("h4");
    const index = report?.output_index;
    heading.textContent = `Output ${index == null ? "-" : Number(index) + 1}`;
    const status = document.createElement("span");
    status.className = "audit-output-verified";
    status.textContent = "Verified self-view";
    const facts = document.createElement("div");
    facts.className = "audit-output-facts";
    appendAuditorOutputValue(facts, "Amount", auditorReportAmount(report));
    appendAuditorOutputValue(
      facts,
      "Recipient",
      summary.to_shielded_address || report?.to || "-",
    );
    appendAuditorOutputValue(
      facts,
      "Sender",
      summary.from_shielded_address || report?.from || "-",
    );
    appendAuditorOutputValue(
      facts,
      "Fields",
      (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") ||
        "-",
    );
    output.append(heading, status, facts);
    els.eventDisclosureReports.append(output);
  }
}

function renderEventDisclosureReport(report) {
  const summary = report?.summary || {};
  const payload = report?.payload || {};
  const assetDenom =
    summary.asset_denom || report?.asset_denom || payload.asset_denom || "";
  const amount =
    summary.amount || report?.amount || payload.amount
      ? `${summary.amount || report?.amount || payload.amount}${assetDenom ? ` ${assetDenom}` : ""}`
      : "-";
  const verified =
    report?.verification?.verified ?? summary.verified ?? report?.verified;
  const disclosureVerified = verified === true;
  els.eventDisclosurePlane.textContent =
    summary.plane || report?.payload?.plane || "-";
  els.eventDisclosurePolicy.textContent =
    summary.policy || report?.policy || payload.policy || "-";
  els.eventDisclosureOutputIndex.textContent =
    summary.output_index ?? report?.output_index ?? payload.output_index ?? "-";
  els.eventDisclosureCommitment.textContent =
    summary.commitment_hex ||
    report?.commitment_hex ||
    payload.commitment_hex ||
    "-";
  els.eventDisclosureDigest.textContent =
    summary.digest_hex ||
    report?.digest_hex ||
    payload.disclosure_digest_hex ||
    payload.digest_hex ||
    "-";
  els.eventDisclosureVerified.textContent =
    typeof verified === "boolean" ? (verified ? "true" : "false") : "-";
  if (!disclosureVerified) {
    els.eventDisclosureFields.textContent = "-";
    els.eventDisclosureAmount.textContent = "-";
    els.eventDisclosureAssetDenom.textContent = "-";
    els.eventDisclosureFrom.textContent = "-";
    els.eventDisclosureTo.textContent = "-";
    els.eventDisclosureState.textContent =
      "Disclosure verification failed. Plaintext withheld.";
    return;
  }
  els.eventDisclosureFields.textContent =
    (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") ||
    "-";
  els.eventDisclosureAmount.textContent = amount;
  els.eventDisclosureAssetDenom.textContent = assetDenom || "-";
  els.eventDisclosureFrom.textContent = summary.from_shielded_address || "-";
  els.eventDisclosureTo.textContent = summary.to_shielded_address || "-";
  els.eventDisclosureState.textContent =
    `${summary.delivery || "recipient-encrypted"} / ${summary.policy || "unknown policy"}`;
}

function renderEventDetail() {
  const event = selectedPrivacyEvent();
  const batch = isBatchTransferEvent(event);
  els.eventDetailType.textContent = event?.event_type || "-";
  els.eventDetailHeight.textContent = event?.height || "-";
  els.eventDetailTx.textContent = event?.tx_hash_hex || "-";
  els.eventDetailUserMode.textContent = batch
    ? "per-output typed scan"
    : event
    ? eventAttribute(event, "user_disclosure_mode") || "-"
    : "-";
  els.eventDetailTarget.textContent = batch
    ? "not published for self-view"
    : event
    ? eventAttribute(event, "user_disclosure_target_pubkey") || "-"
    : "-";
  els.eventDisclosurePlane.textContent = selectedEventDisclosurePlane(event);
  clearEventDisclosureResult();
  clearEventBatchDisclosureReports();
  if (batch && state.privacyEvents.batchDecoded.length > 0) {
    const reports = state.privacyEvents.batchDecoded;
    renderEventDisclosureReport(reports[0]);
    renderEventBatchDisclosureReports(reports);
    els.eventDisclosureState.textContent =
      `Verified sender self-view disclosure for ${reports.length} batch output${reports.length === 1 ? "" : "s"}.`;
  } else if (state.privacyEvents.decoded) {
    renderEventDisclosureReport(state.privacyEvents.decoded);
  } else if (state.privacyEvents.error) {
    els.eventDisclosureState.textContent = state.privacyEvents.error;
  } else {
    els.eventDisclosureState.textContent = eventDisclosureStatus(event);
  }
  els.decodeEventDisclosure.disabled =
    state.privacyEvents.loading || !canDecodeEventDisclosure(event);
}

function hasAuditorUi() {
  return (
    serverFeature("auditorAdmin") &&
    Boolean(els.refreshAuditorTransfers && els.auditorEventsList)
  );
}

function auditorDetailValueElements() {
  return [
    els.auditorTxHash,
    els.auditorVerification,
    els.auditorAmount,
    els.auditorDigest,
    els.auditorFrom,
    els.auditorFields,
    els.auditorTo,
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
    const suffix = state.auditor.testScalarMatchesAuditConfig
      ? " (matches audit config)"
      : " (not current audit config)";
    els.auditorTestScalar.textContent = `${state.auditor.testScalar}${suffix}`;
  } else {
    els.auditorTestScalar.textContent = state.auditor.testScalarError || "-";
  }
  updateAuditorDecodeButton();
}

async function refreshAuditorTestScalar({ healthView = null } = {}) {
  assertOptionalHealthViewCurrent(healthView);
  if (!hasAuditorUi() || !els.auditorTestScalar) return;
  const generation = auditorSessionGeneration;
  els.auditorTestScalar.textContent = "Loading...";
  updateAuditorDecodeButton();
  try {
    const data = await api("/api/auditor/test-scalar", {
      expectedResponseUrl: "/api/auditor/test-scalar",
      responseLabel: "Local auditor scalar response",
      redirect: "error",
    });
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      (healthView !== null && !isHealthViewCurrent(healthView))
    ) return;
    state.auditor.testScalar = data.disclosure_private_scalar_hex || "";
    state.auditor.testScalarError = "";
    state.auditor.testScalarMatchesAuditConfig = Boolean(
      data.matches_audit_config,
    );
  } catch (error) {
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      (healthView !== null && !isHealthViewCurrent(healthView))
    ) return;
    state.auditor.testScalar = "";
    state.auditor.testScalarError = "Unavailable: local auditor test scalar could not be loaded.";
    state.auditor.testScalarMatchesAuditConfig = false;
  }
  renderAuditorTestScalar();
  updateAuditorDecodeButton();
}

function updateAuditorDecodeButton() {
  if (!els.decodeAuditorTransfer) return;
  const scalar = state.auditor.testScalar || "";
  els.decodeAuditorTransfer.disabled =
    state.auditor.loading ||
    !state.auditor.selectedEventKey ||
    !/^[0-9a-fA-F]{1,64}$/.test(scalar);
}

function typedBatchScanOutputTxHash(output) {
  const hash = bytesToHex(output?.tx_hash ?? output?.txHash).toUpperCase();
  if (!/^[0-9A-F]{64}$/.test(hash)) {
    throw new Error("typed batch scan output has an invalid transaction hash");
  }
  return hash;
}

function typedBatchScanCursor(page) {
  const cursor = page?.next_cursor ?? page?.nextCursor;
  if (!cursor || typeof cursor !== "object") {
    throw new Error("typed batch scan is missing its next cursor");
  }
  return {
    height: cursor.height ?? 0,
    globalSequence: cursor.global_sequence ?? cursor.globalSequence ?? 0,
    outputIndex: cursor.output_index ?? cursor.outputIndex ?? 0,
  };
}

function sameTypedBatchScanCursor(left, right) {
  return (
    String(left?.height ?? 0) === String(right?.height ?? 0) &&
    String(left?.globalSequence ?? left?.global_sequence ?? 0) ===
      String(right?.globalSequence ?? right?.global_sequence ?? 0) &&
    String(left?.outputIndex ?? left?.output_index ?? 0) ===
      String(right?.outputIndex ?? right?.output_index ?? 0)
  );
}

function typedBatchScanStartCursor(event) {
  const identity = typedBatchEventIdentity(event);
  return {
    height: identity.height,
    globalSequence: (BigInt(identity.globalSequence) - 1n).toString(),
    outputIndex: 0,
  };
}

async function batchTypedOutputsForEvent(event) {
  const expectedTxHash = String(event?.tx_hash_hex || "").trim().toUpperCase();
  if (!/^[0-9A-F]{64}$/.test(expectedTxHash)) {
    throw new Error("batch event has an invalid transaction hash");
  }
  const selectedIdentity = typedBatchEventIdentity(event);

  const outputsByIndex = new Map();
  let expectedOutputCount = null;
  let after = typedBatchScanStartCursor(selectedIdentity);
  while (true) {
    const page = await clairveilBrowserClient().fetchAuditableBatchTransfers({
      after,
      eventLimit: batchEventScanEventLimit,
      outputLimit: batchEventScanOutputLimit,
      maxEncodedBytes: batchEventScanMaxEncodedBytes,
    });
    const summaries = page?.summaries || [];
    const summary = summaries.find(
      (item) =>
        String(item?.event_type ?? item?.eventType ?? "") ===
          "batch_transfer" &&
        typedBatchScanOutputTxHash(item) === expectedTxHash &&
        sameTypedBatchEventIdentity(
          selectedIdentity,
          typedBatchEventIdentity(item),
        ),
    );
    if (summary) {
      const count = Number(summary.output_count ?? summary.outputCount ?? 0);
      if (!Number.isInteger(count) || count < 1 || count > batchTransferMaxOutputs) {
        throw new Error("typed batch scan has an invalid output count");
      }
      if (expectedOutputCount !== null && expectedOutputCount !== count) {
        throw new Error("typed batch scan changed the selected output count");
      }
      expectedOutputCount = count;
    }

    for (const output of page?.outputs || []) {
      if (typedBatchScanOutputTxHash(output) !== expectedTxHash) continue;
      if (
        !sameTypedBatchEventIdentity(
          selectedIdentity,
          typedBatchEventIdentity(output),
        )
      ) {
        continue;
      }
      const index = Number(output.output_index ?? output.outputIndex);
      if (!Number.isInteger(index) || index < 0 || index >= batchTransferMaxOutputs) {
        throw new Error("typed batch scan has an invalid output index");
      }
      if (outputsByIndex.has(index)) {
        throw new Error("typed batch scan repeated a selected output");
      }
      outputsByIndex.set(index, output);
    }

    if (expectedOutputCount !== null && outputsByIndex.size === expectedOutputCount) {
      return [...outputsByIndex.entries()]
        .sort(([left], [right]) => left - right)
        .map(([, output]) => output);
    }

    if (expectedOutputCount === null) {
      throw new Error("typed batch scan no longer contains the selected event");
    }

    if (!page?.has_more && !page?.hasMore) break;
    const next = typedBatchScanCursor(page);
    if (sameTypedBatchScanCursor(after, next)) {
      throw new Error("typed batch scan cursor did not advance");
    }
    after = next;
  }

  throw new Error("typed batch scan stopped before the selected batch was complete");
}

function hasBatchSelfViewDisclosure(output) {
  const payload =
    output?.self_view_disclosure_payload ?? output?.selfViewDisclosurePayload;
  return new Uint8Array(payload || []).length > 0;
}

async function decodeBatchSelfViewDisclosures(event) {
  const outputs = await batchTypedOutputsForEvent(event);
  const selfViewOutputs = outputs.filter(hasBatchSelfViewDisclosure);
  if (selfViewOutputs.length === 0) {
    throw new Error("This batch does not contain sender self-view disclosures.");
  }
  return Promise.all(
    selfViewOutputs.map((output) =>
      clairveilBrowserClient().decodeBatchSelfViewDisclosure(
        keplrPrivacyRequest({ txHash: event.tx_hash_hex, output }),
      ),
    ),
  );
}

async function decodeSelectedEventDisclosure() {
  const event = selectedPrivacyEvent();
  if (!event || !canDecodeEventDisclosure(event)) return;
  const selectedEventKey = privacyEventKey(event);
  if (!selectedEventKey) return;
  const eventTxHash = event.tx_hash_hex;
  const privacySession = privacySessionGeneration;
  const disclosureGeneration = privacyEventDisclosureGeneration + 1;
  privacyEventDisclosureGeneration = disclosureGeneration;
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.batchDecoded = [];
  state.privacyEvents.error = "";
  els.eventDisclosureState.textContent = "Disclosure 조회 중...";
  renderEventDetail();
  try {
    const decoded = isBatchTransferEvent(event)
      ? await decodeBatchSelfViewDisclosures(event)
      : canDecodeUserEventDisclosure(event)
        ? await clairveilBrowserClient().decodeUserDisclosure(
            privacyRequest({ txHash: event.tx_hash_hex }),
          )
        : await clairveilBrowserClient().decodeSelfViewDisclosure(
            keplrPrivacyRequest({ txHash: event.tx_hash_hex }),
          );
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedEventKey !== selectedEventKey
    ) {
      return;
    }
    if (isBatchTransferEvent(event)) {
      state.privacyEvents.batchDecoded = decoded;
      renderEventDetail();
    } else {
      state.privacyEvents.decoded = decoded;
      renderEventDisclosureReport(decoded);
    }
  } catch (error) {
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedEventKey !== selectedEventKey
    ) {
      return;
    }
    // Disclosure decode requests carry the current privacy authority. Do not
    // render an upstream error that could reflect that request or disclosure
    // material back into the browser UI.
    state.privacyEvents.error = privacyDisclosureErrorMessage(error);
  } finally {
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedEventKey !== selectedEventKey
    ) {
      return;
    }
    state.privacyEvents.loading = false;
    renderEventDetail();
  }
}

function isBatchAuditorEvent(event) {
  return event?.event_type === "batch_transfer";
}

function clearAuditorOutputReports() {
  if (!els.auditorOutputReports) return;
  els.auditorOutputReports.replaceChildren();
  els.auditorOutputReports.hidden = true;
}

function clearAuditorReport(message = "Select a transfer.") {
  if (!hasAuditorUi()) return;
  clearAuditorOutputReports();
  setAuditorValueTone(auditorDetailValueElements());
  els.auditorTxHash.textContent = "-";
  els.auditorVerification.textContent = "-";
  els.auditorAmount.textContent = "-";
  els.auditorFrom.textContent = "-";
  els.auditorTo.textContent = "-";
  els.auditorFields.textContent = "-";
  els.auditorDigest.textContent = "-";
  els.auditorDecodeState.textContent = message;
  updateAuditorDecodeButton();
}

function renderAuditorEventDetail(event) {
  if (!hasAuditorUi()) return;
  if (!event) {
    clearAuditorReport();
    return;
  }

  clearAuditorOutputReports();
  const batch = isBatchAuditorEvent(event);
  const target = batch
    ? event.audit_target_pubkey || ""
    : eventAttribute(event, "audit_disclosure_target_pubkey");
  const digest = batch
    ? ""
    : eventAttribute(event, "audit_disclosure_digest");
  const payload = batch
    ? ""
    : eventAttribute(event, "audit_disclosure_payload");
  const outputCount = Number(event.output_count || event.audit_output_count || 0);

  els.auditorTxHash.textContent = event.tx_hash_hex || "-";
  els.auditorVerification.textContent = batch
    ? `batch / height ${event.height || "-"}`
    : event.height || "-";
  els.auditorAmount.textContent = batch
    ? `${outputCount} encrypted output${outputCount === 1 ? "" : "s"}`
    : target
      ? shorten(target, 14, 12)
      : "-";
  els.auditorDigest.textContent = batch
    ? target
      ? shorten(target, 14, 12)
      : "typed output evidence"
    : digest
      ? shorten(digest, 14, 12)
      : "-";
  els.auditorFrom.textContent = batch
    ? "typed privacy-scan-v2"
    : payload
      ? shorten(payload, 14, 12)
      : "-";
  els.auditorFields.textContent = batch
    ? "one encrypted audit disclosure per output"
    : "encrypted";
  els.auditorTo.textContent = batch
    ? "decode all batch outputs"
    : "decode UI deferred";
  setAuditorValueTone(
    [els.auditorTxHash, els.auditorAmount, els.auditorDigest, els.auditorFrom],
    "encoded",
  );
  els.auditorDecodeState.textContent = batch
    ? `Batch audit disclosure is present for ${outputCount} output${outputCount === 1 ? "" : "s"}. Select Decode to verify each typed output with the local admin test scalar.`
    : "Audit disclosure is present. Select Decode to use the local admin test scalar.";
  updateAuditorDecodeButton();
}

function auditorReportVerificationResult(report) {
  const summary = report?.summary || {};
  const verification = report?.verification || {};
  return verification.verified ?? summary.verified ?? report?.verified;
}

function auditorReportAmount(report) {
  const summary = report?.summary || {};
  return summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";
}

function appendAuditorOutputValue(container, label, value) {
  const fact = document.createElement("div");
  const name = document.createElement("span");
  name.textContent = label;
  const detail = document.createElement("strong");
  detail.textContent = value || "-";
  fact.append(name, detail);
  container.append(fact);
}

function renderBatchAuditorOutputReports(outputs) {
  if (!els.auditorOutputReports) return;
  els.auditorOutputReports.replaceChildren();
  els.auditorOutputReports.hidden = outputs.length === 0;
  for (const entry of outputs) {
    const report = entry?.report || {};
    const summary = report.summary || {};
    const output = document.createElement("article");
    output.className = "audit-output-report";
    const heading = document.createElement("h4");
    const index = entry?.output_index ?? report?.output_index;
    heading.textContent = `Output ${index == null ? "-" : Number(index) + 1}`;
    const verified = auditorReportVerificationResult(report) === true;
    const status = document.createElement("span");
    status.className = verified ? "audit-output-verified" : "audit-output-failed";
    status.textContent = verified ? "Verified" : "Verification failed";
    const facts = document.createElement("div");
    facts.className = "audit-output-facts";
    appendAuditorOutputValue(facts, "Amount", auditorReportAmount(report));
    appendAuditorOutputValue(
      facts,
      "Recipient",
      summary.to_shielded_address || report.to || "-",
    );
    appendAuditorOutputValue(
      facts,
      "Sender",
      summary.from_shielded_address || report.from || "-",
    );
    appendAuditorOutputValue(
      facts,
      "Visible data",
      (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") || "-",
    );
    output.append(heading, status, facts);
    els.auditorOutputReports.append(output);
  }
}

function renderBatchAuditorReport(report) {
  const outputs = Array.isArray(report?.outputs) ? report.outputs : [];
  const verifiedCount = outputs.filter(
    (entry) => auditorReportVerificationResult(entry?.report) === true,
  ).length;
  const verified = outputs.length > 0 && verifiedCount === outputs.length;
  els.auditorTxHash.textContent =
    report?.tx_hash || state.auditor.selectedTxHash || "-";
  els.auditorVerification.textContent = `${verifiedCount}/${outputs.length} outputs verified`;
  els.auditorAmount.textContent = `${outputs.length} batch output${outputs.length === 1 ? "" : "s"}`;
  els.auditorDigest.textContent = `${verifiedCount}/${outputs.length} full digests verified`;
  els.auditorFrom.textContent = "typed privacy-scan-v2";
  els.auditorFields.textContent = "per-output audit disclosure";
  els.auditorTo.textContent = verified
    ? "all outputs verified"
    : "one or more output checks failed";
  setAuditorValueTone(auditorDetailValueElements(), verified ? "decoded" : "encoded");
  els.auditorDecodeState.textContent = verified
    ? `Verified the audit disclosure for all ${outputs.length} batch outputs.`
    : "One or more batch output disclosures failed verification. Plaintext for failed outputs is withheld.";
  renderBatchAuditorOutputReports(outputs);
  updateAuditorDecodeButton();
}

function renderAuditorReport(report) {
  if (!hasAuditorUi()) return;
  if (report?.event_type === "batch_transfer") {
    renderBatchAuditorReport(report);
    return;
  }
  clearAuditorOutputReports();
  const summary = report?.summary || {};
  const payload = report?.payload || {};
  const verification = report?.verification || {};
  // Fixed-encoding v0.2 reports expose the authoritative result at the
  // top-level `verified` field, while legacy reports also included
  // `verification.verified`. Prefer an explicitly present nested result so a
  // contradictory false value still fails closed, then accept the current
  // top-level contract.
  const verificationResult =
    verification.verified ?? summary.verified ?? report?.verified;
  const disclosureVerified = verificationResult === true;
  const verified = disclosureVerified ? "Verified" : "Failed";
  const amount = auditorReportAmount(report);

  els.auditorTxHash.textContent =
    report?.tx_hash || state.auditor.selectedTxHash || "-";
  els.auditorVerification.textContent = verified;
  els.auditorDigest.textContent =
    payload.disclosure_digest_hex ||
    eventAttribute(
      selectedAuditorEvent(),
      "audit_disclosure_digest",
    ) ||
    "-";
  if (!disclosureVerified) {
    els.auditorAmount.textContent = "-";
    els.auditorFrom.textContent = "-";
    els.auditorTo.textContent = "-";
    els.auditorFields.textContent = "-";
    setAuditorValueTone(auditorDetailValueElements(), "encoded");
    els.auditorDecodeState.textContent =
      "Disclosure verification failed. Plaintext withheld.";
    updateAuditorDecodeButton();
    return;
  }
  els.auditorAmount.textContent = amount;
  els.auditorFrom.textContent = summary.from_shielded_address || "-";
  els.auditorTo.textContent = summary.to_shielded_address || "-";
  els.auditorFields.textContent =
    (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") ||
    "-";
  setAuditorValueTone(auditorDetailValueElements(), "decoded");
  els.auditorDecodeState.textContent = `${summary.delivery || report?.source || "audit"} / ${summary.policy || "unknown policy"}`;
  updateAuditorDecodeButton();
}

function renderAuditorTransfers() {
  if (!hasAuditorUi()) return;
  els.auditorEventsList.innerHTML = "";
  const events = [...state.auditor.events].reverse();

  for (const event of events) {
    const batch = isBatchAuditorEvent(event);
    const eventKey = privacyEventKey(event);
    const row = document.createElement("button");
    row.type = "button";
    row.className = "audit-row";
    row.classList.toggle(
      "selected",
      Boolean(eventKey) && eventKey === state.auditor.selectedEventKey,
    );
    row.disabled = state.auditor.loading || !eventKey;
    if (!row.disabled) {
      row.addEventListener("click", () => selectAuditorTransfer(event));
    }

    const copy = document.createElement("div");
    copy.className = "row-copy";
    const title = document.createElement("strong");
    title.textContent = batch
      ? `Batch ${shorten(event.tx_hash_hex, 14, 12)}`
      : shorten(event.tx_hash_hex, 14, 12);
    const meta = document.createElement("span");
    const outputCount = Number(
      batch
        ? eventAttribute(event, "output_count") || 0
        : event.output_count || event.audit_output_count || 0,
    );
    meta.textContent = batch
      ? `${outputCount} output${outputCount === 1 ? "" : "s"} · height ${event.height}`
      : `height ${event.height}`;
    const digest = document.createElement("code");
    digest.textContent = batch
      ? "Decode output evidence"
      : shorten(
        eventAttribute(event, "audit_disclosure_digest"),
        12,
        10,
      );

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
  renderAuditorPagination();
}

function renderAuditorPagination() {
  if (els.auditorPageState) {
    els.auditorPageState.textContent = `Page ${state.auditor.page}${state.auditor.hasMore ? " · newer events available" : ""}`;
  }
  if (els.previousAuditorPage) {
    els.previousAuditorPage.disabled =
      state.auditor.loading || state.auditor.page <= 1;
  }
  if (els.nextAuditorPage) {
    els.nextAuditorPage.disabled =
      state.auditor.loading || !state.auditor.hasMore;
  }
}

async function refreshAuditorTransfers({ healthView = null, page = state.auditor.page } = {}) {
  assertOptionalHealthViewCurrent(healthView);
  if (!hasAuditorUi()) return;
  const requestedPage = Number(page);
  if (!Number.isSafeInteger(requestedPage) || requestedPage < 1) {
    throw new Error("auditor event page must be a positive integer");
  }
  const generation = auditorSessionGeneration;
  setBusy(els.refreshAuditorTransfers, true);
  state.auditor.loading = true;
  renderAuditorPagination();
  try {
    // This local-admin panel must use the same-origin server route. A direct
    // browser request to the node's loopback REST address fails whenever the
    // DApp is opened through another local hostname or a permitted LAN host,
    // leaving the panel silently empty even though the node has transfers.
    const transferPath =
      `/api/auditor/transfers?page=${requestedPage}&limit=${auditorEventsPageLimit}`;
    const data = await api(
      transferPath,
      {
        // The paginated endpoint's query string is part of its direct-response
        // identity. Validating only the path rejects its own 200 response
        // because Response.url retains ?page and ?limit.
        expectedResponseUrl: transferPath,
        responseLabel: "Local auditor transfers response",
        redirect: "error",
      },
    );
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      (healthView !== null && !isHealthViewCurrent(healthView))
    ) return;
    state.auditor.events = data.events || [];
    state.auditor.page = Number(data.page ?? requestedPage);
    state.auditor.hasMore = Boolean(data.has_more ?? data.hasMore);
    if (
      state.auditor.selectedEventKey &&
      !state.auditor.events.some(
        (event) => privacyEventKey(event) === state.auditor.selectedEventKey,
      )
    ) {
      state.auditor.selectedEventKey = "";
      state.auditor.selectedTxHash = "";
      state.auditor.decoded = null;
      clearAuditorReport();
    }
    renderAuditorTransfers();
    renderAuditorEventDetail(selectedAuditorEvent());
  } finally {
    if (
      generation === auditorSessionGeneration &&
      hasAuditorUi() &&
      (healthView === null || isHealthViewCurrent(healthView))
    ) {
      setBusy(els.refreshAuditorTransfers, false);
      state.auditor.loading = false;
      // Rows are created disabled while the request is in flight. Re-render
      // them after clearing that state so listed transfers are selectable.
      renderAuditorTransfers();
      renderAuditorPagination();
    }
  }
}

function selectedAuditorEvent() {
  return state.auditor.events.find(
    (event) => privacyEventKey(event) === state.auditor.selectedEventKey,
  );
}

function selectAuditorTransfer(event) {
  if (!hasAuditorUi()) return;
  const selectedEventKey = privacyEventKey(event);
  if (!selectedEventKey) return;
  state.auditor.selectedEventKey = selectedEventKey;
  state.auditor.selectedTxHash = event.tx_hash_hex;
  state.auditor.decoded = null;
  renderAuditorTransfers();
  renderAuditorEventDetail(event);
  updateAuditorDecodeButton();
}

async function decodeAuditorTransfer() {
  const event = selectedAuditorEvent();
  if (!hasAuditorUi()) {
    if (event) selectAuditorTransfer(event);
    return;
  }
  if (!event) {
    clearAuditorReport("Select a transfer first.");
    return;
  }
  const selectedEventKey = privacyEventKey(event);
  if (!selectedEventKey) return;
  const txHash = event.tx_hash_hex;
  const disclosurePrivKeyHex = state.auditor.testScalar || "";
  if (!/^[0-9a-fA-F]{1,64}$/.test(disclosurePrivKeyHex)) {
    state.auditor.selectedEventKey = selectedEventKey;
    state.auditor.selectedTxHash = txHash;
    clearAuditorReport("Local admin test scalar is unavailable.");
    renderAuditorTransfers();
    return;
  }

  const generation = auditorSessionGeneration;
  state.auditor.selectedEventKey = selectedEventKey;
  state.auditor.selectedTxHash = txHash;
  state.auditor.loading = true;
  state.auditor.decoded = null;
  els.decodeAuditorTransfer.textContent = "Decoding...";
  clearAuditorReport("Decoding audit disclosure with injected scalar...");
  renderAuditorTransfers();

  try {
    const report = await api("/api/auditor/decode", {
      method: "POST",
      body: JSON.stringify({
        txHash,
        disclosurePrivKeyHex,
        eventType: event.event_type,
        eventHeight: event.height,
        eventSequence: event.sequence,
        eventPage: state.auditor.page,
      }),
      expectedResponseUrl: "/api/auditor/decode",
      responseLabel: "Local auditor decode response",
      redirect: "error",
    });
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      state.auditor.selectedEventKey !== selectedEventKey
    ) return;
    state.auditor.decoded = report;
    renderAuditorReport(report);
  } catch (error) {
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      state.auditor.selectedEventKey !== selectedEventKey
    ) return;
    clearAuditorReport(privacyDisclosureErrorMessage(error));
  } finally {
    if (
      generation !== auditorSessionGeneration ||
      !hasAuditorUi() ||
      state.auditor.selectedEventKey !== selectedEventKey
    ) return;
    state.auditor.loading = false;
    els.decodeAuditorTransfer.textContent = "Decode";
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
    toast(
      "Selected chain is not running in this DApp server. Restart the server for that chain profile.",
    );
    return;
  }
  if (!metaMaskProvider()) {
    toast("MetaMask not found");
    return;
  }
  resetWalletSession();
  const connection = beginWalletConnection("metamask");
  const { session } = connection;
  try {
    await ensureMetaMaskChain({ session });
    assertWalletConnectionCurrent(connection);
    const accounts = await withPrivacySessionGuard(
      session,
      () => requestMetaMask({ method: "eth_requestAccounts" }),
    );
    assertWalletConnectionCurrent(connection);
    const account = accounts[0] || "";
    if (!account) {
      renderWallet();
      renderKeplr();
      return;
    }
    await ensureMetaMaskChain({ session });
    assertWalletConnectionCurrent(connection);
    const chainId = await withPrivacySessionGuard(
      session,
      () => requestMetaMask({ method: "eth_chainId" }),
    );
    assertWalletConnectionCurrent(connection);
    const identity = clairveilBrowserClient().evmAccountIdentity(account);
    state.activeWallet = "metamask";
    state.wallet.account = account;
    state.wallet.chainId = chainId;
    state.keplr.account = identity.address || "";
    state.keplr.name = "MetaMask";
    state.keplr.pubkeyHex = identity.pubKeyHex || "";
    state.keplr.expectedAddress = identity.address || "";
    state.keplr.addressMatches = Boolean(identity.address);
    state.keplr.signerCheck = "OK (EVM address)";
    if (!els.veiledWithdrawRecipient.value && identity.evmAddress) {
      els.veiledWithdrawRecipient.value = identity.evmAddress;
    }
    if (!els.relayWithdrawRecipient.value && identity.evmAddress) {
      els.relayWithdrawRecipient.value = identity.evmAddress;
    }
    renderWallet();
    renderKeplr();
    try {
      await refreshWalletBalance({ session });
    } catch (error) {
      assertWalletConnectionCurrent(connection);
      state.keplr.balance = error.message;
      renderKeplr();
    }
  } finally {
    endWalletConnection(connection);
  }
}

async function signMetaMaskSession() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  const account = state.wallet.account;
  if (!account) return;
  await ensureMetaMaskChain({ session });
  assertPrivacySessionCurrent(session);
  const local = selectedLocalAccount()?.name || "alice";
  const message = [
    "Clairveil local test session",
    `MetaMask: ${account}`,
    `Local signer: ${local}`,
    `Chain: ${activeChainProfile()?.chainId || state.config?.chainId || "clairveil-local-2"}`,
    `Time: ${new Date().toISOString()}`,
  ].join("\n");
  const signature = await withPrivacySessionGuard(session, () =>
    requestMetaMask({
      method: "personal_sign",
      params: [message, account],
    }),
  );
  assertPrivacySessionCurrent(session);
  const signatureHash = await withPrivacySessionGuard(session, () =>
    digestText(signature),
  );
  assertPrivacySessionCurrent(session);
  state.wallet.signatureHash = signatureHash;
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
      pubKeyHex: bytesToHex(key.pubKey),
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
      pubKeyHex: bytesToHex(pubKey),
    });
  }

  const uniqueCandidates = candidates.filter(
    (candidate, index) =>
      candidates.findIndex(
        (other) =>
          other.address === candidate.address &&
          other.pubKeyHex === candidate.pubKeyHex,
      ) === index,
  );

  for (const candidate of uniqueCandidates) {
    try {
      const signerCheck = clairveilBrowserClient().verifySignerPubKey(
        candidate.address,
        candidate.pubKeyHex,
      );
      if (signerCheck.matches) {
        return { ...candidate, signerCheck, candidates: uniqueCandidates };
      }
      candidate.signerCheck = signerCheck;
    } catch (error) {
      candidate.error = error.message;
    }
  }

  return {
    ...(uniqueCandidates[0] || {
      source: "Keplr",
      address: key?.bech32Address || "",
      pubKeyHex: "",
    }),
    signerCheck: uniqueCandidates[0]?.signerCheck || {
      expectedAddress: "",
      matches: false,
    },
    candidates: uniqueCandidates,
  };
}

async function connectKeplr() {
  if (!canConnectWallet("keplr")) return;
  if (activeWalletKind() !== "keplr") {
    toast("Selected DApp chain uses MetaMask.");
    return;
  }
  if (!selectedProfileMatchesServer()) {
    toast(
      "Selected chain is not running in this DApp server. Restart the server for that chain profile.",
    );
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
  resetWalletSession();
  const connection = beginWalletConnection("keplr");
  const { session } = connection;
  try {
    await withPrivacySessionGuard(
      session,
      () => window.keplr.experimentalSuggestChain(chainInfo),
    );
    assertWalletConnectionCurrent(connection);
    await withPrivacySessionGuard(
      session,
      () => window.keplr.enable(chainInfo.chainId),
    );
    assertWalletConnectionCurrent(connection);
    const key = await withPrivacySessionGuard(
      session,
      () => window.keplr.getKey(chainInfo.chainId),
    );
    assertWalletConnectionCurrent(connection);
    const signer = await withPrivacySessionGuard(
      session,
      () => resolveKeplrSigner(chainInfo.chainId, key),
    );
    assertWalletConnectionCurrent(connection);

    state.activeWallet = "keplr";
    state.keplr.account = signer.address || key.bech32Address || "";
    state.keplr.name = key.name || "";
    state.keplr.pubkeyHex = signer.pubKeyHex || "";
    state.keplr.signerCheck = "Checking...";
    renderKeplr();

    state.keplr.expectedAddress = signer.signerCheck?.expectedAddress || "";
    state.keplr.addressMatches = Boolean(signer.signerCheck?.matches);
    state.keplr.signerCheck = state.keplr.addressMatches
      ? `OK (${signer.source})`
      : `Mismatch: ${shorten(state.keplr.expectedAddress, 12, 10)}`;
    renderKeplr();

    if (!state.keplr.addressMatches) {
      const sources = signer.candidates?.length
        ? signer.candidates
            .map(
              (candidate) =>
                `${candidate.source}: ${shorten(candidate.address, 12, 10)}`,
            )
            .join(", ")
        : "no Keplr signer candidates";
      toast(
        `Keplr address/pubKey mismatch on ${chainInfo.chainId}. Checked ${sources}. Remove Clairveil Localnet (${chainInfo.chainId}) from Keplr once, reconnect, and try again. You do not need to change chains on every restart.`,
      );
      return;
    }

    await refreshWalletBalance({ session });
    assertWalletConnectionCurrent(connection);
    toast("Keplr connected");
  } finally {
    endWalletConnection(connection);
  }
}

async function signKeplrSession() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
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
    `Time: ${new Date().toISOString()}`,
  ].join("\n");
  const signature = await withPrivacySessionGuard(session, () =>
    window.keplr.signArbitrary(
      chainInfo.chainId,
      state.keplr.account,
      message,
    ),
  );
  assertPrivacySessionCurrent(session);
  const signatureHash = await withPrivacySessionGuard(session, () =>
    digestText(signature.signature),
  );
  assertPrivacySessionCurrent(session);
  let verified = false;
  if (typeof window.keplr.verifyArbitrary === "function") {
    verified = await withPrivacySessionGuard(session, () =>
      window.keplr.verifyArbitrary(
        chainInfo.chainId,
        state.keplr.account,
        message,
        signature,
      ),
    );
    assertPrivacySessionCurrent(session);
  }
  state.keplr.signatureHash = signatureHash;
  state.keplr.verified = verified;
  renderKeplr();
  toast("Keplr session signed");
}

async function disconnectWallet() {
  resetWalletSession();
  renderWallet();
  renderKeplr();
  toast("Wallet disconnected");
}

async function fundKeplr() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  if (!state.keplr.account) return;
  if (!serverFeature("faucet")) {
    toast(
      "Faucet is available only when this DApp server is attached to a local test node.",
    );
    return;
  }
  const amount = clairInputToUclair(els.keplrFaucetAmount);
  const recipient = connectedPublicRecipientAddress();
  const localSigner =
    selectedLocalAccount()?.name || state.accounts[0]?.name || "alice";
  const actionLock = beginPrivacyValueAction("faucet", session);
  if (!actionLock) return;
  setBusy(els.fundKeplr, true);
  renderKeplr();
  try {
    const data = await withPrivacySessionGuard(session, () =>
      api("/api/faucet", {
        method: "POST",
        body: JSON.stringify({
          from: localSigner,
          recipient,
          amount,
        }),
      }),
    );
    assertPrivacySessionCurrent(session);
    state.keplr.faucetHash = data.broadcast?.txhash || "";
    state.keplr.faucetSent = formatUclairAsClair(
      data.amount?.funded?.replace(baseDenom(), "") || "0",
    );
    state.keplr.faucetRecipient = isEvmTransparentMode()
      ? data.recipientEvm || recipient
      : data.recipient || recipient;
    state.keplr.balance = formatBalances(data.balance?.balances);
    await refreshWalletBalance({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
    toast(`Faucet sent: ${state.keplr.faucetSent}`);
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    toast(error.message);
  } finally {
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.fundKeplr, false);
      renderKeplr();
    }
  }
}

async function setupKeplrPrivacy() {
  if (!state.keplr.account) return false;
  const session = beginPrivacySessionOperation();
  const activeSetup = privacySetupInFlight;
  if (activeSetup?.generation === session.generation) {
    return activeSetup.promise;
  }
  const lock = Object.freeze({ generation: session.generation });
  let resolveSetup;
  let rejectSetup;
  const promise = new Promise((resolve, reject) => {
    resolveSetup = resolve;
    rejectSetup = reject;
  });
  privacySetupInFlight = { lock, generation: session.generation, promise };
  void runKeplrPrivacySetup(session).then(
    (result) => {
      if (privacySetupInFlight?.lock === lock) {
        privacySetupInFlight = null;
      }
      resolveSetup(result);
    },
    (error) => {
      if (privacySetupInFlight?.lock === lock) {
        privacySetupInFlight = null;
      }
      rejectSetup(error);
    },
  );
  return promise;
}

async function runKeplrPrivacySetup(session) {
  if (
    state.keplr.rootSignatureBase64 &&
    state.keplr.shieldedAddress &&
    state.keplr.disclosurePubKeyHex
  ) {
    // This session already loaded its recovery metadata during initial setup.
    // Do not make a later Deposit depend on decoding a stale relay snapshot.
    // A fresh scan below validates the chain before any value-moving flow.
    renderKeplr();
    return true;
  }

  setBusy(els.setupKeplrPrivacy, true);
  els.keplrTxState.textContent = "Setting up";
  try {
    const address = state.keplr.account;
    const pubKeyHex = state.keplr.pubkeyHex;
    let account;
    let signatureBase64;
    if (state.activeWallet === "metamask") {
      await ensureMetaMaskChain({ session });
      assertPrivacySessionCurrent(session);
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(
        address,
        pubKeyHex,
      );
      const signatureHex = await withPrivacySessionGuard(session, () =>
        requestMetaMask({
          method: "personal_sign",
          params: [rootMessage, state.wallet.account],
        }),
      );
      assertPrivacySessionCurrent(session);
      signatureBase64 = bytesToBase64(hexToBytes(signatureHex));
      account = clairveilBrowserClient().derivePrivacyAccount({
        walletType: "evm",
        address,
        pubKeyHex,
        signatureBase64,
      });
    } else {
      if (!window.keplr) return;
      const chainInfo = activeKeplrChainInfo();
      if (!chainInfo) {
        throw new Error("Selected chain does not include Keplr chain info");
      }
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(
        address,
        pubKeyHex,
      );
      const signature = await withPrivacySessionGuard(session, () =>
        window.keplr.signArbitrary(
          chainInfo.chainId,
          address,
          rootMessage,
        ),
      );
      assertPrivacySessionCurrent(session);
      signatureBase64 = signature.signature;
      account = clairveilBrowserClient().derivePrivacyAccount({
        address,
        pubKeyHex,
        signatureBase64,
      });
    }
    assertPrivacySessionCurrent(session);
    state.keplr.rootSignatureBase64 = signatureBase64;
    state.keplr.shieldedAddress = account.shielded_address || "";
    state.keplr.disclosurePubKeyHex = account.disclosure_pubkey_hex || "";
    state.keplr.rootSignatureHash = account.root_signature_hash || "";
    try {
      await hydratePersistedWalletNotes({ session });
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      if (!isRecoverableEncryptedNoteCacheError(error)) throw error;
      // Keep only the in-memory root authority so the user can explicitly
      // delete the unreadable ciphertext and perform a full typed rescan.
      // The unreadable cache is never displayed or used for a spend.
      invalidatePrivacyScanState(error);
      state.keplr.privacySetupFailed = false;
      els.keplrTxState.textContent = "Encrypted note cache recovery required";
      renderKeplr();
      toast(
        "The encrypted local note cache cannot be read. Reset & rescan notes before relying on your shielded balance.",
      );
      return false;
    }
    await scanKeplrNotes({
      quiet: true,
      skipPrivacySetup: true,
      throwOnError: true,
      session,
    });
    assertPrivacySessionCurrent(session);
    // A fresh local ledger has no valid old relay handoff to recover; the
    // chain-safety refresh above clears it. On an initialized chain, load
    // recovery metadata only after the typed scan has established this
    // session's current chain view.
    if (isSpendChainReady()) {
      await loadPersistedRelayWithdrawPayloadState({ session });
      assertPrivacySessionCurrent(session);
    }
    state.keplr.privacySetupFailed = false;
    els.keplrTxState.textContent = "Ready";
    renderKeplr();
    toast("Clairveil account ready");
    return true;
  } catch (error) {
    if (error?.privacySessionInvalidated) return false;
    // Chain configuration is a live sync preflight, not a failure of the
    // wallet-derived authority or encrypted persistence. Keep this session's
    // in-memory root material so the user can retry the same typed scan after
    // the selected endpoint recovers; privacy actions remain disabled until
    // that retry completes successfully.
    if (error?.chainSafetyFailure) {
      state.keplr.privacySetupFailed = false;
      els.keplrTxState.textContent = "Privacy sync unavailable";
      renderKeplr();
      toast(privacySyncErrorMessage(error));
      return false;
    }
    invalidateFailedPrivacySetup();
    els.keplrTxState.textContent = "Setup failed";
    toast(privacySetupErrorMessage(error));
    return false;
  } finally {
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.setupKeplrPrivacy, false);
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

async function copyWalletAccount() {
  const account = currentWalletAccountForCopy();
  if (!account) {
    toast("Connect a wallet first");
    return;
  }
  await navigator.clipboard.writeText(account);
  toast("Account copied");
}

async function copyRelayWithdrawPayload() {
  if (!state.keplr.relayWithdrawPayloadText) {
    toast("Prepared relay withdraw payload가 없습니다.");
    return;
  }
  if (relayWithdrawPayloadCopyInFlight) {
    return;
  }
  const session = beginPrivacySessionOperation();
  const expectedVersion = state.keplr.relayWithdrawPayloadVersion;
  const handoffBoundaryLock = beginRelayHandoffBoundary(
    "copy",
    session,
    expectedVersion,
  );
  if (!handoffBoundaryLock) return;
  const copySnapshot = currentPreparedRelayWithdrawSnapshot();
  const copyIsCurrent = () =>
    isPrivacySessionCurrent(session) &&
    state.keplr.relayWithdrawPayloadVersion === expectedVersion;
  const assertCopyIsCurrent = () => {
    if (copyIsCurrent()) return;
    if (!isPrivacySessionCurrent(session)) {
      throw privacySessionInvalidatedError();
    }
    const error = new Error(
      "Prepared relay withdraw payload changed while handoff was in progress",
    );
    error.relayPayloadChanged = true;
    throw error;
  };
  let handoffRecorded = Boolean(copySnapshot?.handedOff);
  let handoffTransitioned = false;
  let clipboardAttempted = false;
  const copyLock = Object.freeze({
    generation: session.generation,
    payloadVersion: expectedVersion,
  });
  relayWithdrawPayloadCopyInFlight = true;
  relayWithdrawPayloadCopyLock = copyLock;
  renderKeplr();
  try {
    const chainSnapshot = await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
    assertCopyIsCurrent();
    if (relaySnapshotIsExpired(copySnapshot, chainSnapshot.chainNowMs)) {
      throw new Error("Relay payload is expired at the latest chain block time");
    }
    const currentReservationRecords = await latestReservationRecords(
      copySnapshot.reservation,
      { session },
    );
    assertCopyIsCurrent();
    const currentReservationRecordsByID = new Map(
      currentReservationRecords.map((record) => [record.reservation_id, record]),
    );
    if (
      !handoffRecorded &&
      !canRelaySnapshotBeSubmitted(
        copySnapshot,
        currentReservationRecordsByID,
        reservationStatuses,
        chainSnapshot.chainNowMs,
      )
    ) {
      throw new Error("Relay payload reservation is not ready for handoff");
    }
    if (
      handoffRecorded &&
      !canRelayHandoffPayloadBeCopied(
        copySnapshot,
        currentReservationRecordsByID,
        reservationStatuses,
      )
    ) {
      throw new Error(
        "Relay payload has newer submission evidence. Refresh Notes before copying it again.",
      );
    }
    await verifyRelayPayloadNullifierUnspentBeforeBroadcast(
      copySnapshot.payload,
      copySnapshot.reservation,
      copySnapshot.preparedData,
      { session },
    );
    assertCopyIsCurrent();
    if (!handoffRecorded) {
      await markRelayReservationHandedOff(
        copySnapshot.reservation,
        copySnapshot.payload?.payload_hash,
        { expectedPayloadVersion: expectedVersion, session },
      );
      assertCopyIsCurrent();
      handoffRecorded = true;
      handoffTransitioned = true;
      state.keplr.relayWithdrawPayloadHandedOff = true;
      await persistRelayWithdrawPayloadState({ session });
      assertCopyIsCurrent();
    }
    stopPreparedRelayReservationHeartbeat();
    await extendReservationBatchLeaseToPayloadExpiry(
      copySnapshot.reservation,
      copySnapshot.payload,
      { expectedPayloadVersion: expectedVersion, session },
    );
    assertCopyIsCurrent();
    const finalReservationRecords = await latestReservationRecords(
      copySnapshot.reservation,
      { session },
    );
    assertCopyIsCurrent();
    updateRelayWithdrawReservationRecords(finalReservationRecords, { session });
    if (
      !canRelayHandoffPayloadBeCopied(
        copySnapshot,
        new Map(
          finalReservationRecords.map((record) => [record.reservation_id, record]),
        ),
        reservationStatuses,
      )
    ) {
      throw new Error(
        "Relay payload has newer submission evidence. Refresh Notes before copying it again.",
      );
    }
    const finalChainSnapshot = await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
    assertCopyIsCurrent();
    if (relaySnapshotIsExpired(copySnapshot, finalChainSnapshot.chainNowMs)) {
      throw new Error("Relay payload is expired at the latest chain block time");
    }
    clipboardAttempted = true;
    await withPrivacySessionGuard(
      session,
      () => navigator.clipboard.writeText(copySnapshot.payloadText),
    );
    assertCopyIsCurrent();
  } catch (error) {
    if (!copyIsCurrent()) {
      // An unguarded bookkeeping failure can race with account/profile
      // invalidation. Normalize it to the currentness sentinel so the
      // top-level handler cannot render the former session's error.
      assertCopyIsCurrent();
    }
    if (!handoffRecorded || (!handoffTransitioned && !clipboardAttempted)) {
      throw error;
    }
    state.keplr.relayWithdrawPayloadHandedOff = true;
    await persistRelayWithdrawPayloadState({ session });
    assertCopyIsCurrent();
    renderKeplr();
    const retryError = new Error(
      clipboardAttempted
        ? "Relay handoff는 안전하게 기록됐지만 clipboard 복사에 실패했습니다. Copy를 다시 눌러 기존 payload를 복사해 주세요."
        : "Relay handoff는 기록됐지만 lease 갱신을 완료하지 못했습니다. 상태를 확인한 뒤 Copy를 다시 시도해 주세요.",
    );
    retryError.relayHandoffRecorded = true;
    retryError.cause = error;
    throw retryError;
  } finally {
    endRelayHandoffBoundary(handoffBoundaryLock);
    if (relayWithdrawPayloadCopyLock === copyLock) {
      relayWithdrawPayloadCopyInFlight = false;
      relayWithdrawPayloadCopyLock = null;
      if (isPrivacySessionCurrent(session)) renderKeplr();
    }
  }
  assertCopyIsCurrent();
  state.keplr.relayWithdrawPayloadHandedOff = true;
  await persistRelayWithdrawPayloadState({ session });
  assertCopyIsCurrent();
  renderKeplr();
  toast("Relay withdraw payload copied");
}

function noBroadcastAttemptError(
  error,
  fallback = "Wallet request was not submitted",
) {
  if (error && typeof error === "object") {
    error.noBroadcastAttempt = true;
    return error;
  }
  const wrapped = new Error(error ? String(error) : fallback);
  wrapped.noBroadcastAttempt = true;
  return wrapped;
}

function isMetaMaskUserRejectedError(error) {
  const code = String(error?.code ?? error?.data?.code ?? "");
  return code === "4001";
}

function keplrDirectSignWallet(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!window.keplr?.signDirect) {
    throw noBroadcastAttemptError(new Error("Keplr signDirect not available"));
  }
  const account = state.keplr.account;
  if (!account) {
    throw noBroadcastAttemptError(new Error("Keplr is not connected"));
  }
  return {
    address: account,
    async signDirect(directSignDoc) {
      return withPrivacySessionGuard(
        session,
        () => window.keplr.signDirect(
          directSignDoc.chainId,
          account,
          {
            bodyBytes: directSignDoc.bodyBytes,
            authInfoBytes: directSignDoc.authInfoBytes,
            chainId: directSignDoc.chainId,
            accountNumber: BigInt(directSignDoc.accountNumber),
          },
          {
            preferNoSetFee: true,
            preferNoSetMemo: true,
          },
        ),
      );
    },
  };
}

function sessionBoundReservationBroadcastManager(
  manager,
  {
    session = beginPrivacySessionOperation(),
    onBroadcastStart = null,
  } = {},
) {
  if (!manager || typeof manager !== "object") return manager;
  return new Proxy(manager, {
    get(target, property) {
      const value = Reflect.get(target, property, target);
      if (typeof value !== "function") return value;
      if (property !== "markBroadcastAttempting") return value.bind(target);
      return async (...args) => {
        assertPrivacySessionCurrent(session);
        const updated = await value.apply(target, args);
        // The SDK calls this immediately before its raw RPC boundary. Mark
        // the boundary only after the durable attempt marker is committed and
        // the operation still belongs to this session.
        assertPrivacySessionCurrent(session);
        onBroadcastStart?.();
        assertPrivacySessionCurrent(session);
        return updated;
      };
    },
  });
}

async function signDirectAndBroadcast(
  signDoc,
  {
    reservation = null,
    relayPayload = null,
    session = beginPrivacySessionOperation(),
    onBroadcastStart = null,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const hasReservation = reservationIDs(reservation).length > 0;
  const reservationManagerForBroadcast = hasReservation
    ? currentNoteReservationManager()
    : null;
  const broadcastReservationManager = hasReservation
    ? sessionBoundReservationBroadcastManager(
        reservationManagerForBroadcast,
        { session, onBroadcastStart },
      )
    : null;
  // A MsgWithdraw is only valid while its payload has not expired. Keep its
  // chain-time provider inside the session-bound SDK calls so it is fetched
  // both before signing and again before raw transaction broadcast.
  const relayValidation = relayPayload
    ? {
        relayPayload,
        getChainNowUnix: () => latestChainNowUnix({ session }),
      }
    : {};
  const submission = {
    wallet: keplrDirectSignWallet({ session }),
    signDoc,
    ...relayValidation,
    ...(hasReservation
      ? {
          reservationManager: reservationManagerForBroadcast,
          reservation,
        }
      : {}),
  };

  let checkpoint;
  try {
    checkpoint = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().signDirect(submission),
    );
  } catch (error) {
    if (hasReservation && isMetaMaskUserRejectedError(error)) {
      error.reservationLifecycleHandled = true;
    }
    throw noBroadcastAttemptError(error);
  }
  assertPrivacySessionCurrent(session);
  if (!hasReservation) {
    onBroadcastStart?.();
    assertPrivacySessionCurrent(session);
  }
  const broadcast = await withPrivacySessionGuard(
    session,
    () => clairveilBrowserClient().broadcastTxRawBytes(
      checkpoint.txRawBytes,
      hasReservation
        ? {
            reservationManager: broadcastReservationManager,
            reservation,
            ...relayValidation,
          }
        : relayPayload
          ? relayValidation
          : undefined,
    ),
  );
  return {
    ...broadcast,
    sdkReservationLifecycleManaged: hasReservation,
  };
}

async function submitEvmTransaction(
  transaction,
  {
    beforeBroadcast,
    onBroadcastStart,
    session = beginPrivacySessionOperation(),
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const account = state.wallet.account;
  const provider = metaMaskProvider();
  if (!provider || !account) {
    throw noBroadcastAttemptError(new Error("MetaMask is not connected"));
  }
  try {
    await ensureMetaMaskChain({ session });
  } catch (error) {
    throw noBroadcastAttemptError(error);
  }
  let tx;
  try {
    tx = await withPrivacySessionGuard(
      session,
      () => withEstimatedEvmGas({
        ...transaction,
        from: account,
      }),
    );
  } catch (error) {
    throw noBroadcastAttemptError(error, "MetaMask gas estimation failed before broadcast");
  }
  await beforeBroadcast?.();
  assertPrivacySessionCurrent(session);
  let walletRequest;
  try {
    // Starting the provider request is the external boundary. A synchronous
    // provider failure occurred before any wallet request, so keep the
    // durable marker on the no-broadcast recovery path.
    walletRequest = provider.request({
      method: "eth_sendTransaction",
      params: [tx],
    });
  } catch (error) {
    if (unsupportedEvmMethodError(error)) {
      throw noBroadcastAttemptError(
        new Error(
          "eth_sendTransaction is not supported by the injected wallet provider. Open this DApp in a browser with MetaMask or another EVM wallet selected.",
        ),
      );
    }
    throw noBroadcastAttemptError(error);
  }
  onBroadcastStart?.({ externalBoundaryStarted: true });
  assertPrivacySessionCurrent(session);
  try {
    const txHash = await withPrivacySessionGuard(
      session,
      () => walletRequest,
    );
    const normalized = normalizeEvmTxHash(txHash);
    if (!/^[0-9A-F]{64}$/.test(normalized)) {
      throw new Error("MetaMask eth_sendTransaction did not return a tx hash");
    }
    return normalized;
  } catch (error) {
    if (unsupportedEvmMethodError(error)) {
      throw new Error(
        "eth_sendTransaction is not supported by the injected wallet provider. Open this DApp in a browser with MetaMask or another EVM wallet selected.",
      );
    }
    if (isMetaMaskUserRejectedError(error)) {
      throw noBroadcastAttemptError(error);
    }
    throw error;
  }
}

async function waitForEvmTransaction(
  txHash,
  label = "EVM transaction",
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const broadcast = await withPrivacySessionGuard(
    session,
    () => clairveilBrowserClient().waitForEvmTransaction(txHash),
  );
  assertPrivacySessionCurrent(session);
  try {
    assertSuccessfulBroadcast(broadcast, label);
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  } catch (error) {
    error.txHash = broadcast?.txHash || txHash;
    error.broadcast = broadcast;
    throw error;
  }
}

async function sendEvmTransaction(
  transaction,
  {
    waitForReceipt = false,
    label = "EVM transaction",
    beforeBroadcast,
    onBroadcastStart,
    session = beginPrivacySessionOperation(),
  } = {},
) {
  const txHash = await submitEvmTransaction(transaction, {
    beforeBroadcast,
    onBroadcastStart,
    session,
  });
  if (waitForReceipt) {
    const broadcast = await waitForEvmTransaction(txHash, label, { session });
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  }
  const waitPromise = waitForEvmTransaction(txHash, label, { session });
  waitPromise.catch(() => {});
  return {
    txHash,
    pending: true,
    waitPromise,
  };
}

function watchEvmBroadcast(
  broadcast,
  {
    session = beginPrivacySessionOperation(),
    onIncluded,
    onFailed,
  } = {},
) {
  if (!broadcast?.waitPromise) return;
  const invoke = async (callback, value) => {
    if (typeof callback !== "function" || !isPrivacySessionCurrent(session)) {
      return;
    }
    try {
      await callback(value);
    } catch (error) {
      if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
        return;
      }
      // Do not expose receipt callback details to console-forwarding telemetry.
      console.warn("clairveil_evm_receipt_callback_failed");
    }
  };
  void broadcast.waitPromise
    .then(
      (result) => invoke(onIncluded, result),
      (error) => {
        if (isEvmReceiptConfirmationPending(error)) return;
        if (!isPrivacySessionCurrent(session)) return;
        return invoke(onFailed, error);
      },
    )
    .catch((error) => {
      if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
        return;
      }
      // Do not expose receipt callback details to console-forwarding telemetry.
      console.warn("clairveil_evm_receipt_callback_failed");
    });
}

function keplrPrivacyRequest(extra = {}) {
  return {
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    ...extra,
  };
}

function evmPrivacyRequest(extra = {}) {
  return {
    walletType: "evm",
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    // ClairveilJS verifies the signer wallet separately from the read-only
    // RPC before an EVM proof is prepared. Keep that check bound to the
    // currently selected injected wallet rather than treating RPC health as
    // proof that MetaMask is on the same network.
    evmWallet: {
      getChainId: () => requestMetaMask({ method: "eth_chainId" }),
    },
    ...extra,
  };
}

function privacyRequest(extra = {}) {
  return state.activeWallet === "metamask"
    ? evmPrivacyRequest(extra)
    : keplrPrivacyRequest(extra);
}

async function preparePrivacyDepositSignDoc(amount) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  // The SDK invokes this callback later. Capture the reviewed endpoint now so
  // a profile switch cannot send this operation's proof material elsewhere.
  const depositProofEndpoint = browserDepositProofUrl();
  const localDepositProofAvailable = hasLocalDepositProofProvider();
  const request = privacyRequest({ amount });
  request.depositProofProvider = async ({ material }) => {
    assertPrivacySessionCurrent(session);
    if (!depositProofEndpoint && !localDepositProofAvailable) {
      throw new Error(
        "Deposit proof provider is unavailable. Configure the active profile's depositProofUrl or use the loopback local helper.",
      );
    }
    const proofEndpoint = depositProofEndpoint || "/api/deposit/proof";
    try {
      const response = await withPrivacySessionGuard(
        session,
        () => api(proofEndpoint, {
          method: "POST",
          timeoutMs: depositProofRequestTimeoutMs,
          maxResponseBytes: depositProofResponseMaxBytes,
          expectedResponseUrl: proofEndpoint,
          responseLabel: "Deposit proof response",
          redirect: "error",
          body: JSON.stringify({
            note_json: material.note_json,
            note_commitment_hex: material.note_commitment_hex,
          }),
        }),
      );
      assertPrivacySessionCurrent(session);
      return requireVersionedDepositProofResponse(response);
    } catch (error) {
      throw safeDepositProofProviderError(error);
    }
  };
  return preparePrivacyWithSessionCleanup(
    session,
    () => clairveilBrowserClient().prepareDeposit(request),
  );
}

async function preparePrivacyTransferSignDoc(
  amount,
  recipient,
  disclosure = {},
  options = {},
) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  assertSpendableNotesSyncReady();
  await assertTypedPrivacyScanBeforePreparation(session);
  assertPrivacySessionCurrent(session);
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return preparePrivacyWithSessionCleanup(
    session,
    (signal) => clairveilBrowserClient().prepareTransfer(
      privacyRequest({
        amount,
        recipient,
        scan: { limit: 200, maxPages: 1000, scanSource: "privacy_scan" },
        reservationManager,
        ...disclosure,
        allowPlanStep: Boolean(options.allowPlanStep),
        signal,
      }),
    ),
  );
}

async function preparePrivacyBatchTransferSignDoc(payments) {
  const session = beginPrivacySessionOperation();
  let preparationStarted = false;
  let preflightStage = "feature-gate";
  try {
    if (!batchTransferFeatureEnabled()) {
      throw new Error("Batch transfer is disabled for the active chain profile");
    }
    preflightStage = "chain-safety";
    await assertChainSafetyBeforePrivacyFlow({ session });
    assertPrivacySessionCurrent(session);
    preflightStage = "note-sync";
    assertSpendableNotesSyncReady();
    preflightStage = "typed-note-scan";
    await assertTypedPrivacyScanBeforePreparation(session);
    assertPrivacySessionCurrent(session);
    preflightStage = "artifact-recovery";
    await assertNoUnresolvedBatchTransferArtifact({ session });
    assertPrivacySessionCurrent(session);
    preflightStage = "chain-time";
    const chainNowUnix = await latestChainNowUnix({ session });
    assertPrivacySessionCurrent(session);
    preflightStage = "reservation-storage";
    const reservationManager = currentNoteReservationManager();
    session.reservationManager = reservationManager;
    const preparedPayments = payments.map((payment) => ({
      itemId: payment.itemId,
      amount: payment.amount,
      recipient: payment.recipient,
      userPrivacyPolicy: payment.userPrivacyPolicy,
      userDisclosureMode: payment.userDisclosureMode,
      ...(payment.userDisclosureTargetPubKeyHex
        ? {
            userDisclosureTargetPubKeyHex:
              payment.userDisclosureTargetPubKeyHex,
          }
        : {}),
    }));
    let checkpoint = {
      phase: "preparing",
      payments: preparedPayments,
    };
    const onPreparedPayload = async (payload, context) => {
      checkpoint = {
        ...checkpoint,
        phase: "payload-checkpointed",
        payload,
        context,
        reservation: context?.reservation || null,
      };
      await saveBatchTransferArtifact(checkpoint, { session });
    };
    const onPreparedProof = async (proof, context) => {
      checkpoint = {
        ...checkpoint,
        phase: "proof-checkpointed",
        payload: context?.payload || checkpoint.payload,
        proof,
        context,
        reservation: context?.reservation || checkpoint.reservation || null,
      };
      await saveBatchTransferArtifact(checkpoint, { session });
    };
    preparationStarted = true;
    const data = await preparePrivacyWithSessionCleanup(
      session,
      (signal) => clairveilBrowserClient().prepareTransferBatch(
        privacyRequest({
          payments: preparedPayments,
          outputMode: "compact",
          chainNowUnix,
          expiresAtUnix: chainNowUnix + 1_800,
          scan: {
            limit: 200,
            maxPages: 1_000,
            scanSource: "privacy_scan",
          },
          reservationManager,
          signal,
          onPreparedPayload,
          onPreparedProof,
        }),
      ),
    );
    await saveBatchTransferArtifact(
      {
        ...checkpoint,
        phase: "proof-ready",
        reservation: preparedReservation(data),
        operationEvidence:
          data.operationEvidence || data.prepared?.operationEvidence || null,
        operationEvidenceHash:
          data.operationEvidenceHash ||
          data.prepared?.operationEvidenceHash ||
          "",
      },
      { session },
    );
    return data;
  } catch (error) {
    // Browser adapters and third-party SDK boundaries are allowed to reject
    // with a string or a plain object. Normalize those failures here so the
    // top-level flow always receives the pre-wallet boundary and the exact
    // preflight stage, rather than falling back to its generic retry copy.
    const failure =
      error instanceof Error
        ? error
        : Object.assign(
            new Error(
              String(error?.message || error || "Batch preparation failed"),
            ),
            error && typeof error === "object" ? error : {},
          );
    if (failure.privacySessionInvalidated) throw failure;
    {
      // Every failure in this function is before the confirmation dialog and
      // therefore before Keplr can sign or broadcast.  Preserve the precise
      // safe boundary for the top-level UI; do not collapse a typed-scan or
      // local storage preflight into an ambiguous transaction failure.
      failure.batchTransferPreparationFailedBeforeWallet = true;
      failure.batchTransferPreparationStage = preparationStarted
        ? "proof-preparation"
        : "preflight";
      if (!preparationStarted) {
        failure.batchTransferPreflightStage = preflightStage;
      }
    }
    if (preparationStarted) {
      // A preparation that entered ClairveilJS can have acquired a durable
      // reservation or payload checkpoint. Refresh it immediately so the
      // Review action is visible instead of leaving the preview at zero
      // available notes with a generic retry message.
      await recoverBatchPreparationFailure(failure, { session });
    }
    throw failure;
  }
}

async function preparePrivacyWithdrawSignDoc(amount, recipient) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  assertSpendableNotesSyncReady();
  await assertTypedPrivacyScanBeforePreparation(session);
  assertPrivacySessionCurrent(session);
  // MsgWithdraw embeds an expiry. Use the latest authoritative block time
  // rather than browser time when constructing that expiry.
  const chainNowUnix = await latestChainNowUnix({ session });
  assertPrivacySessionCurrent(session);
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return preparePrivacyWithSessionCleanup(
    session,
    (signal) => clairveilBrowserClient().prepareWithdraw(
      privacyRequest({
        amount,
        recipient,
        chainNowUnix,
        scan: { limit: 200, maxPages: 1000, scanSource: "privacy_scan" },
        reservationManager,
        signal,
      }),
    ),
  );
}

async function preparePrivacyRelayWithdrawPayload(amount, recipient) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  assertSpendableNotesSyncReady();
  await assertTypedPrivacyScanBeforePreparation(session);
  assertPrivacySessionCurrent(session);
  // The typed scan can take long enough to make an earlier block-time read
  // stale. Obtain the relay expiry baseline only after that scan completes,
  // immediately before the SDK prepares the payload.
  const chainSnapshot = await withPrivacySessionGuard(
    session,
    () => latestRelayChainSnapshot(),
  );
  assertPrivacySessionCurrent(session);
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return preparePrivacyWithSessionCleanup(
    session,
    (signal) => clairveilBrowserClient().prepareRelayWithdraw(
      privacyRequest({
        amount,
        recipient,
        chainNowUnix: Math.floor(chainSnapshot.chainNowMs / 1000),
        scan: { limit: 200, maxPages: 1000, scanSource: "privacy_scan" },
        reservationManager,
        signal,
      }),
    ),
  );
}

async function relayPreparedWithdrawPayload(payload, recipient) {
  const relayer =
    localRelayerAccount()?.name ||
    (isEvmTransparentMode() ? "dev0" : "relayer");
  return api("/api/relayer/withdraw", {
    method: "POST",
    timeoutMs: relaySubmissionRequestTimeoutMs,
    body: JSON.stringify({
      payload,
      expectedRecipient: recipient,
      relayer,
    }),
    expectedResponseUrl: "/api/relayer/withdraw",
    responseLabel: "Local relay response",
    redirect: "error",
  });
}

async function broadcastPrivacyDeposit(
  amount,
  label = "deposit",
  options = {},
) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyDepositSignDoc(amount);
  const session = preparedPrivacySession(data) || beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  state.keplr.shieldedAddress =
    data.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  els.keplrTxState.textContent =
    state.activeWallet === "metamask"
      ? "Waiting for MetaMask"
      : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, options);
  assertPrivacySessionCurrent(session);
  state.keplr.depositHash = broadcast.broadcast?.txhash || "";
  state.keplr.depositHash = state.keplr.depositHash || broadcast.txHash || "";
  state.keplr.depositHeight =
    broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
  return {
    ...broadcast,
    // confirmDeposit needs both prepared values, not merely the broadcast
    // hash. Retain this only in memory for the immediately following Cosmos
    // inclusion check; it is never persisted as wallet state.
    preparedDeposit: data,
  };
}

function depositConfirmationRequiredError(cause) {
  const error = new Error(
    "The wallet returned a transaction hash, but the DApp could not verify the exact prepared deposit output on chain. The shielded balance remains unavailable. Refresh Notes to reconcile before attempting another deposit.",
  );
  error.depositConfirmationRequired = true;
  error.cause = cause;
  return error;
}

async function confirmCosmosDepositBeforeRecovery(broadcast, { session } = {}) {
  assertPrivacySessionCurrent(session);
  const txHash = String(
    broadcast?.broadcast?.txhash || broadcast?.txHash || state.keplr.depositHash || "",
  ).trim();
  const prepared = broadcast?.preparedDeposit?.prepared;
  const expectedCommitment = String(prepared?.noteCommitmentHex || "").trim();
  const expectedEncryptedNote = String(prepared?.encryptedNoteHex || "").trim();
  if (!txHash || !expectedCommitment || !expectedEncryptedNote) {
    throw depositConfirmationRequiredError(
      new Error("prepared Cosmos deposit confirmation material is unavailable"),
    );
  }
  try {
    const confirmed = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().confirmDeposit({
        txHash,
        expectedCommitment,
        expectedEncryptedNote,
        // broadcastSignedTx has already observed inclusion. Keep this short
        // retry window only for the REST transaction index to catch up.
        attempts: 5,
        intervalMs: 500,
      }),
    );
    assertPrivacySessionCurrent(session);
    state.keplr.depositHash = confirmed.txHash || state.keplr.depositHash;
    state.keplr.depositHeight =
      confirmed.tx?.height || confirmed.tx?.tx_response?.height || state.keplr.depositHeight;
    return confirmed;
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    throw depositConfirmationRequiredError(error);
  }
}

function broadcastTxEvents(broadcast) {
  return broadcast?.tx?.tx_result?.events || broadcast?.tx?.events || [];
}

function broadcastEventAttribute(event, key) {
  return (
    (event?.attributes || []).find((attribute) => attribute.key === key)
      ?.value || ""
  );
}

function evmFailureMessageFromBroadcast(broadcast, label = "transaction") {
  if (broadcast?.error) {
    return broadcast.error;
  }
  const evmFailure = broadcastTxEvents(broadcast)
    .filter((event) => event.type === "ethereum_tx")
    .map((event) => broadcastEventAttribute(event, "ethereumTxFailed"))
    .find(Boolean);
  if (evmFailure) {
    return `${label} failed: EVM execution reverted (${evmFailure})`;
  }
  if (hasFailedEvmReceiptStatus(broadcast?.receipt)) {
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
    if (hasSuccessfulEvmReceiptStatus(broadcast.receipt)) return;
    const error = new Error(
      `${label} receipt did not include an explicit success or failure status`,
    );
    error.receipt = broadcast.receipt;
    error.broadcast = broadcast;
    throw error;
  }
  if (!broadcast?.tx) {
    throw new Error(
      `${label} was broadcast but not found yet: ${txHash || "unknown tx"}`,
    );
  }
  if (Number(broadcast.tx.code || 0) !== 0) {
    throw new Error(
      broadcast.tx.raw_log || `${label} failed with code ${broadcast.tx.code}`,
    );
  }
}

const delayedCosmosBroadcastConfirmation = Object.freeze({
  // ClairveilJS has already waited for its normal transaction-index window
  // before returning an `ok: false` result with a transaction hash. A local
  // CometBFT node can accept the TxRaw and include it just after that window,
  // so do one bounded, read-only follow-up instead of telling the user to
  // submit the same signed transaction again.
  attempts: 24,
  intervalMs: 1250,
});

function cosmosBroadcastTxHash(broadcast = {}) {
  return String(
    broadcast?.broadcast?.txhash || broadcast?.txHash || "",
  ).trim();
}

function indexedCosmosTxCode(tx = {}) {
  const rawCode = tx?.code ?? tx?.tx_response?.code;
  const code = Number(rawCode);
  return Number.isSafeInteger(code) && code >= 0 ? code : null;
}

async function confirmDelayedCosmosBroadcast(
  broadcast,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  // A result with a transaction record already has a definitive execution
  // code. This only handles the post-CheckTx, pre-indexing gap returned by
  // ClairveilJS for Cosmos chains.
  if (
    state.activeWallet === "metamask" ||
    broadcast?.ok ||
    broadcast?.tx ||
    broadcast?.pending
  ) {
    return broadcast;
  }
  const txHash = cosmosBroadcastTxHash(broadcast);
  if (!txHash) return broadcast;

  let tx;
  try {
    tx = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().waitForTx(
        txHash,
        delayedCosmosBroadcastConfirmation,
      ),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    // Preserve the SDK's durable Unknown state when the follow-up read itself
    // is unavailable. It remains safer than treating a broadcast as failed.
    return broadcast;
  }
  assertPrivacySessionCurrent(session);
  if (!tx) return broadcast;

  const code = indexedCosmosTxCode(tx);
  const rawLog = String(tx?.raw_log || tx?.tx_response?.raw_log || "");
  return {
    ...broadcast,
    ok: code === 0,
    tx,
    broadcast: {
      ...(broadcast?.broadcast || {}),
      txhash: txHash,
      code,
      raw_log: rawLog,
    },
    error: code === 0 ? "" : rawLog || broadcast?.error || "",
  };
}

function preparedReservation(data) {
  return data?.reservation || data?.prepared?.reservation || null;
}

function broadcastTxHash(broadcast) {
  return relayBroadcastTxHash(broadcast);
}

function reservationLeaseToken(reservation) {
  return (
    reservation?.lease_token ||
    reservation?.reservations?.[0]?.lease_token ||
    ""
  );
}

function reservationIDs(reservation) {
  return Array.isArray(reservation?.reservation_ids)
    ? reservation.reservation_ids.filter(Boolean).map(String)
    : [];
}

function reservationRecordsByID() {
  return new Map(
    Object.entries(state.keplr.reservationRecordByID || {}),
  );
}

function cacheReservationRecords(records = [], { replace = false } = {}) {
  const byID = replace ? {} : { ...(state.keplr.reservationRecordByID || {}) };
  for (const reservation of records) {
    const id = String(reservation?.reservation_id || "");
    if (id) byID[id] = reservation;
  }
  state.keplr.reservationRecordByID = byID;
}

function updateRelayWithdrawReservationRecords(
  records = [],
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!records.length) return;
  cacheReservationRecords(records);
  state.keplr.relayWithdrawReservation = updateReservationBatchRecords(
    state.keplr.relayWithdrawReservation,
    records,
  );
  state.keplr.relayWithdrawPendingPayloads = (
    state.keplr.relayWithdrawPendingPayloads || []
  ).map((item) => ({
    ...item,
    reservation: updateReservationBatchRecords(item.reservation, records),
  }));
}

async function markRelayReservationHandedOff(
  reservation,
  payloadHash,
  {
    expectedPayloadVersion = null,
    session = beginPrivacySessionOperation(),
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  const leaseToken = reservationLeaseToken(reservation);
  const normalizedPayloadHash = String(payloadHash || "").trim();
  const manager = currentNoteReservationManager({ optional: true });
  if (!ids.length || !leaseToken || !normalizedPayloadHash || !manager?.recordRelayHandoff) {
    throw new Error("Relay payload handoff cannot be recorded without an active reservation lease");
  }
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.recordRelayHandoff(ids, {
      lease_token: leaseToken,
      payload_hash: normalizedPayloadHash,
      handed_off_at: new Date().toISOString(),
    }),
  );
  if (updated.length !== ids.length) {
    throw new Error("Relay payload handoff was not recorded for every reserved note");
  }
  if (
    expectedPayloadVersion != null &&
    state.keplr.relayWithdrawPayloadVersion !== expectedPayloadVersion
  ) {
    return updated;
  }
  updateRelayWithdrawReservationRecords(updated || [], { session });
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

function canRelayPreparedWithdrawPayload(chainNowMs) {
  return canRelaySnapshotBeSubmitted(
    currentPreparedRelayWithdrawSnapshot(),
    reservationRecordsByID(),
    reservationStatuses,
    chainNowMs,
  );
}

function isRelayPreparedWithdrawStructurallyReady() {
  return isRelaySnapshotStructurallyReady(
    currentPreparedRelayWithdrawSnapshot(),
    reservationRecordsByID(),
    reservationStatuses,
  );
}

function warnReservationBookkeeping(_error) {
  // Keep browser diagnostics safe for console-forwarding telemetry. The
  // detailed error may include operation context and must remain in memory.
  console.warn("clairveil_reservation_bookkeeping_failed");
}

function broadcastAttemptMetadata(source = {}) {
  const nested = source?.broadcast || {};
  return {
    txHash:
      broadcastTxHash(source) ||
      broadcastTxHash(nested) ||
      source?.data?.txHash ||
      source?.data?.tx_hash ||
      "",
    txBytesHash: source?.txBytesHash || source?.tx_bytes_hash || "",
    signDocHash: source?.signDocHash || source?.sign_doc_hash || "",
  };
}

function hasBroadcastAttemptMetadata(metadata = {}) {
  return Boolean(
    metadata.txHash || metadata.txBytesHash || metadata.signDocHash,
  );
}

async function noteReservationBookkeeping(task) {
  try {
    return await task();
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    warnReservationBookkeeping(error);
    return undefined;
  }
}

function reservationReconciliationRequiredError(label, broadcast, cause) {
  const error = new Error(
    `${label} was submitted, but the local note reservation could not be recorded. Refresh Notes to reconcile before preparing another transaction.`,
  );
  error.name = "ReservationReconciliationRequiredError";
  error.reservationReconciliationRequired = true;
  error.broadcast = broadcast;
  error.cause = cause;
  return error;
}

async function renewReservationBatchLease(
  reservation,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  const leaseToken = reservationLeaseToken(reservation);
  if (!ids.length || !leaseToken) return;
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.renewLease !== "function") return;
  await withPrivacySessionGuard(
    session,
    () => manager.renewLease(ids, { leaseToken }),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
}

function stopPreparedRelayReservationHeartbeat() {
  preparedRelayReservationHeartbeatGeneration += 1;
  if (preparedRelayReservationHeartbeatTimer && typeof globalThis.clearInterval === "function") {
    globalThis.clearInterval(preparedRelayReservationHeartbeatTimer);
  }
  preparedRelayReservationHeartbeatTimer = null;
  preparedRelayReservationHeartbeatInFlight = null;
}

function startPreparedRelayReservationHeartbeat(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  stopPreparedRelayReservationHeartbeat();
  const expectedPayloadVersion = state.keplr.relayWithdrawPayloadVersion;
  const heartbeatGeneration = preparedRelayReservationHeartbeatGeneration;
  const reservation = state.keplr.relayWithdrawReservation;
  const ids = reservationIDs(reservation);
  const manager = currentNoteReservationManager({ optional: true });
  if (!ids.length || !manager || typeof globalThis.setInterval !== "function") return;
  const heartbeatIntervalMs = reservationHeartbeatIntervalMs({
    leaseDurationMs: manager.leaseDurationMs,
    leaseUntil: reservation?.lease_until || reservation?.reservations?.[0]?.lease_until,
  });
  const operationID = String(reservation?.operation_id || reservation?.operationId || "");
  preparedRelayReservationHeartbeatTimer = globalThis.setInterval(() => {
    const current = state.keplr.relayWithdrawReservation;
    if (heartbeatGeneration !== preparedRelayReservationHeartbeatGeneration) {
      return;
    }
    if (
      !isPrivacySessionCurrent(session) ||
      state.keplr.relayWithdrawPayloadVersion !== expectedPayloadVersion ||
      state.keplr.relayWithdrawPayloadHandedOff ||
      String(current?.operation_id || current?.operationId || "") !== operationID
    ) {
      stopPreparedRelayReservationHeartbeat();
      return;
    }
    if (preparedRelayReservationHeartbeatInFlight) return;
    preparedRelayReservationHeartbeatInFlight = renewReservationBatchLease(current, { session })
      .catch((error) => {
        if (heartbeatGeneration !== preparedRelayReservationHeartbeatGeneration) {
          return;
        }
        if (!error?.privacySessionInvalidated) {
          warnReservationBookkeeping(error);
        }
        stopPreparedRelayReservationHeartbeat();
      })
      .finally(() => {
        if (heartbeatGeneration === preparedRelayReservationHeartbeatGeneration) {
          preparedRelayReservationHeartbeatInFlight = null;
        }
      });
  }, heartbeatIntervalMs);
}

async function extendReservationBatchLeaseToPayloadExpiry(
  reservation,
  payload,
  {
    expectedPayloadVersion = null,
    session = beginPrivacySessionOperation(),
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  const leaseToken = reservationLeaseToken(reservation);
  const expiresAtUnix = relaySnapshotExpiresAtUnix({ payload });
  const expiresAtMs = Number(expiresAtUnix) * 1000;
  if (!ids.length || !leaseToken || !Number.isFinite(expiresAtMs)) return;
  // Relay expiry is authorized by the fresh chain-time checks surrounding
  // handoff/submission, never by the browser clock. A clock-skewed browser
  // must not expose a chain-valid payload while silently leaving its lease
  // shorter than the payload lifetime. If the chain has already reached
  // expiry, the reservation manager rejects the stale extension and the
  // caller follows the durable handoff/recovery path instead.
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.renewLease !== "function") return;
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.renewLease(ids, {
      leaseToken,
      leaseUntil: new Date(expiresAtMs).toISOString(),
    }),
  );
  if (
    expectedPayloadVersion != null &&
    state.keplr.relayWithdrawPayloadVersion !== expectedPayloadVersion
  ) {
    return updated;
  }
  updateRelayWithdrawReservationRecords(updated || [], { session });
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function withReservationHeartbeat(
  reservation,
  task,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  try {
    await renewReservationBatchLease(reservation, { session });
  } catch (error) {
    throw noBroadcastAttemptError(
      error,
      "Reservation lease renewal failed before broadcast",
    );
  }
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  const manager = currentNoteReservationManager({ optional: true });
  const heartbeatIntervalMs = reservationHeartbeatIntervalMs({
    leaseDurationMs: manager?.leaseDurationMs,
    leaseUntil: reservation?.lease_until || reservation?.reservations?.[0]?.lease_until,
  });
  let heartbeatError = null;
  let inFlightHeartbeat = null;
  let timer = null;
  let stopped = false;
  const stop = () => {
    stopped = true;
    if (timer && typeof globalThis.clearInterval === "function") {
      globalThis.clearInterval(timer);
    }
    timer = null;
  };
  activeReservationHeartbeatStops.add(stop);
  const heartbeat = async () => {
    if (stopped || heartbeatError) return;
    try {
      assertPrivacySessionCurrent(session);
      await renewReservationBatchLease(reservation, { session });
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (!error?.privacySessionInvalidated) {
        heartbeatError = error;
      }
    }
  };
  const heartbeatNow = async () => {
    assertPrivacySessionCurrent(session);
    if (stopped) return;
    if (!inFlightHeartbeat) {
      inFlightHeartbeat = heartbeat().finally(() => {
        inFlightHeartbeat = null;
      });
    }
    await inFlightHeartbeat;
    assertHeartbeatHealthy();
  };
  const assertHeartbeatHealthy = () => {
    assertPrivacySessionCurrent(session);
    if (!heartbeatError) return;
    const error = new Error(
      "Note reservation lease renewal failed. Transaction reconciliation is required before retrying.",
    );
    error.name = "ReservationHeartbeatError";
    error.reservationHeartbeatFailed = true;
    error.cause = heartbeatError;
    throw error;
  };
  timer =
    ids.length && typeof globalThis.setInterval === "function"
      ? globalThis.setInterval(() => { void heartbeatNow().catch(() => {}); }, heartbeatIntervalMs)
      : null;
  try {
    const result = await task({ assertHeartbeatHealthy, heartbeatNow });
    // The Cosmos SDK advances a reserved operation through
    // BroadcastAttempting -> Submitted before it returns. Submitted is
    // intentionally not lease-renewable, so a final heartbeat here would
    // misclassify that successful, durable transition as a bookkeeping
    // failure. EVM and relayer callers still need this final heartbeat: they
    // record Submitted immediately after this wrapper returns.
    if (!result?.sdkReservationLifecycleManaged) {
      try {
        await heartbeatNow();
      } catch (error) {
        error.broadcast = result;
        throw error;
      }
    }
    return result;
  } finally {
    stop();
    activeReservationHeartbeatStops.delete(stop);
    if (inFlightHeartbeat) await inFlightHeartbeat;
  }
}

async function withPreparedReservationHeartbeat(data, task, options = {}) {
  return withReservationHeartbeat(preparedReservation(data), task, options);
}

function normalizedBroadcastIdentity(value) {
  return String(value || "").trim().replace(/^0x/i, "").toLowerCase();
}

function reservationMatchesBroadcastAttempt(reservation, attempt = {}) {
  const evidence = [
    [attempt.txHash, reservation?.submitted_tx_hash],
    [attempt.txBytesHash, reservation?.tx_bytes_hash],
    [attempt.signDocHash, reservation?.sign_doc_hash],
  ].filter(([expected]) => Boolean(expected));
  return evidence.length > 0 && evidence.every(([expected, actual]) =>
    normalizedBroadcastIdentity(expected) === normalizedBroadcastIdentity(actual),
  );
}

async function idempotentReservationBatchRecords(
  manager,
  ids,
  status,
  attempt,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  let records;
  try {
    records = await withPrivacySessionGuard(
      session,
      () => Promise.all(ids.map((id) => manager.getReservation(id))),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    return null;
  }
  return records.length === ids.length && records.every(
    (record) =>
      record.status === status &&
      reservationMatchesBroadcastAttempt(record, attempt),
  )
    ? records
    : null;
}

async function markReservationBatchSubmitted(
  reservation,
  broadcast,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!reservation?.reservation_ids?.length) {
    throw new Error("submitted transaction is missing note reservation ids");
  }
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markSubmitted !== "function") {
    throw new Error("note reservation manager is unavailable for submitted transaction");
  }
  let updated = [];
  const attempt = broadcastAttemptMetadata(broadcast);
  try {
    updated = await withPrivacySessionGuard(
      session,
      () => manager.markSubmitted(reservation.reservation_ids, {
        leaseToken: reservationLeaseToken(reservation),
        txHash: attempt.txHash,
        txBytesHash: attempt.txBytesHash,
        signDocHash: attempt.signDocHash,
      }),
    );
  } catch (error) {
    const ids = reservationIDs(reservation);
    const records = await idempotentReservationBatchRecords(
      manager,
      ids,
      "Submitted",
      attempt,
      { session },
    );
    if (!records) {
      throw error;
    }
    updated = records;
  }
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markPreparedReservationSubmitted(
  data,
  broadcast,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  return markReservationBatchSubmitted(preparedReservation(data), broadcast, { session });
}

async function recordSubmittedReservation(
  reservation,
  broadcast,
  label,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  // Deposits create a new note and therefore have no input-note reservation to advance.
  if (!reservation?.reservation_ids?.length) return [];
  try {
    return await markReservationBatchSubmitted(reservation, broadcast, { session });
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    warnReservationBookkeeping(error);
    let fallbackError = null;
    try {
      await markReservationBatchUnknown(reservation, error, broadcast, {
        reconcile_reason: "submitted_write_failed_after_external_broadcast",
        no_broadcast_attempt: false,
      }, { session });
    } catch (unknownError) {
      if (unknownError?.privacySessionInvalidated) throw unknownError;
      fallbackError = unknownError;
      warnReservationBookkeeping(unknownError);
    }
    const reconciliationError = reservationReconciliationRequiredError(
      label,
      broadcast,
      error,
    );
    if (fallbackError) reconciliationError.unknownFallbackError = fallbackError;
    throw reconciliationError;
  }
}

async function markPreparedReservationBroadcastAttempting(
  data,
  label,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const reservation = preparedReservation(data);
  const ids = reservationIDs(reservation);
  if (!ids.length) return [];
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markBroadcastAttempting !== "function") {
    throw new Error("note reservation manager cannot durably record the broadcast attempt");
  }
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.markBroadcastAttempting(ids, {
      leaseToken: reservationLeaseToken(reservation),
      reason: `${label}_external_broadcast_boundary`,
    }),
  );
  cacheReservationRecords(updated || []);
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markPreparedReservationBroadcastRejected(
  data,
  error,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const reservation = preparedReservation(data);
  const ids = reservationIDs(reservation);
  if (!ids.length) return [];
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markBroadcastRejected !== "function") {
    throw new Error("note reservation manager cannot record the rejected wallet request");
  }
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.markBroadcastRejected(ids, {
      leaseToken: reservationLeaseToken(reservation),
      error: privacyReservationErrorCode(error, "wallet_request_rejected"),
      providerCode: "4001",
    }),
  );
  cacheReservationRecords(updated || []);
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markReservationBatchUnknown(
  reservation,
  error,
  attemptSource = {},
  metadata = {},
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!reservation?.reservation_ids?.length) return;
  const attempt = broadcastAttemptMetadata(attemptSource);
  if (!hasBroadcastAttemptMetadata(attempt)) return;
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager) return;
  let updated = [];
  try {
    updated = await withPrivacySessionGuard(
      session,
      () => manager.markUnknown(reservation.reservation_ids, {
        fromStatus: metadata.fromStatus || metadata.from_status,
        leaseToken: reservationLeaseToken(reservation),
        txHash: attempt.txHash,
        txBytesHash: attempt.txBytesHash,
        signDocHash: attempt.signDocHash,
        error: privacyReservationErrorCode(error, "broadcast_outcome_unknown"),
        metadata,
      }),
    );
  } catch (transitionError) {
    const ids = reservationIDs(reservation);
    const records = await idempotentReservationBatchRecords(
      manager,
      ids,
      "Unknown",
      attempt,
      { session },
    );
    if (!records) {
      throw transitionError;
    }
    updated = records;
  }
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markPreparedReservationUnknown(
  data,
  error,
  attemptSource = {},
  metadata = {},
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  return markReservationBatchUnknown(
    preparedReservation(data),
    error,
    attemptSource,
    metadata,
    { session },
  );
}

async function markPreparedReservationManualReview(
  data,
  error,
  reason,
  metadata = {},
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  return markReservationBatchManualReview(
    preparedReservation(data),
    error,
    reason,
    metadata,
    { session },
  );
}

async function markReservationBatchReplanRequired(
  reservation,
  error,
  reason = "wallet_rejected_before_broadcast",
  {
    evidence = {},
    metadata = {},
    session = beginPrivacySessionOperation(),
  } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!reservation?.reservation_ids?.length) return;
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markReplanRequired !== "function") return;
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.markReplanRequired(
      reservation.reservation_ids,
      {
        ...evidence,
        leaseToken: evidence.leaseToken || evidence.lease_token || reservationLeaseToken(reservation),
        error: privacyReservationErrorCode(
          error,
          "pre_broadcast_operation_cancelled",
        ),
        metadata: {
          ...metadata,
          reconcile_reason: reason,
          no_broadcast_attempt:
            metadata.no_broadcast_attempt ??
            metadata.noBroadcastAttempt ??
            !hasBroadcastAttemptMetadata(broadcastAttemptMetadata(evidence)),
          proof_discarded:
            metadata.proof_discarded ??
            metadata.proofDiscarded ??
            !hasBroadcastAttemptMetadata(broadcastAttemptMetadata(evidence)),
        },
      },
    ),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markReservationBatchManualReview(
  reservation,
  error,
  reason,
  metadata = {},
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const ids = reservationIDs(reservation);
  if (!ids.length) return [];
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markManualReview !== "function") return [];
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.markManualReview(ids, {
      leaseToken: reservationLeaseToken(reservation),
      error: privacyReservationErrorCode(error, "manual_review_required"),
      metadata: {
        reconcile_reason: reason,
        ...metadata,
      },
    },
  ),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function markPreparedReservationReplanRequired(
  data,
  error,
  reason = "wallet_rejected_before_broadcast",
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  return markReservationBatchReplanRequired(
    preparedReservation(data),
    error,
    reason,
    { session },
  );
}

function selectedNotesFromPlan(plan) {
  if (!plan) return [];
  if (Array.isArray(plan.selection?.inputs)) return plan.selection.inputs;
  if (Array.isArray(plan.selections)) {
    return plan.selections.flatMap((selection) => selection?.inputs || []);
  }
  if (plan.selectedNote) return [plan.selectedNote];
  return [];
}

function reservationNullifiersFromPrepared(
  data,
  reservation = preparedReservation(data),
) {
  const nullifiers = new Set();
  for (const note of selectedNotesFromPlan(
    data?.plan || data?.prepared?.plan,
  )) {
    const nullifier = noteNullifier(note);
    if (nullifier) nullifiers.add(nullifier);
  }
  if (!nullifiers.size && reservation?.reservation_ids?.length) {
    const ids = new Set(reservationIDs(reservation));
    for (const note of state.keplr.notes || []) {
      const active = noteReservation(note);
      if (active?.reservation_id && ids.has(active.reservation_id)) {
        const nullifier = noteNullifier(note);
        if (nullifier) nullifiers.add(nullifier);
      }
    }
  }
  return [...nullifiers];
}

async function verifyPreparedNullifiersUnspentBeforeBroadcast(
  data,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const reservation = preparedReservation(data);
  const nullifiers = reservationNullifiersFromPrepared(data);
  if (!nullifiers.length) {
    // Deposits do not consume an input note, but every reserved spend must
    // retain enough input identity for a fresh nullifier preflight.  Do not
    // turn a cache/plan gap into an unchecked wallet or RPC submission.
    if (!reservationIDs(reservation).length) return;
    const error = noBroadcastAttemptError(
      new Error("Input nullifiers are unavailable before broadcast"),
    );
    error.nullifierVerificationFailed = true;
    throw error;
  }
  let statuses;
  try {
    statuses = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().checkNullifiers(nullifiers),
    );
  } catch (cause) {
    if (cause?.privacySessionInvalidated) throw cause;
    const error = noBroadcastAttemptError(
      new Error(`Nullifier status could not be verified before broadcast: ${cause?.message || "query failed"}`),
    );
    error.nullifierVerificationFailed = true;
    throw error;
  }
  const spentNullifiers = [];
  for (const nullifier of nullifiers) {
    const used = statuses instanceof Map
      ? nullifierUsedFromResponse(statuses.get(nullifier))
      : null;
    if (used === null) {
      const error = noBroadcastAttemptError(
        new Error("Nullifier status was missing or malformed before broadcast"),
      );
      error.nullifierVerificationFailed = true;
      throw error;
    }
    if (used) spentNullifiers.push(nullifier);
  }
  if (!spentNullifiers.length) return;
  const manager = currentNoteReservationManager({ optional: true });
  if (manager?.reconcileSpentNotes) {
    await withPrivacySessionGuard(
      session,
      () => manager.reconcileSpentNotes(spentNullifiers.map(nullifier => ({
        nullifier,
        spent: true,
      }))),
    );
    await refreshNoteReservationState({ session });
    assertPrivacySessionCurrent(session);
  }
  const error = noBroadcastAttemptError(
    new Error("Broadcast blocked because an input nullifier is already spent"),
  );
  error.reservationSpentReconciled = true;
  throw error;
}

function isDefiniteEvmReceiptFailure(error) {
  const receiptStatus =
    error?.broadcast?.receipt?.status ??
    error?.receipt?.status ??
    error?.data?.receipt?.status ??
    error?.data?.receiptStatus ??
    error?.receiptStatus ??
    "";
  if (error?.data?.executionFailed === true || error?.executionFailed === true) {
    return hasFailedEvmReceiptStatus({ status: receiptStatus });
  }
  if (hasFailedEvmReceiptStatus({ status: receiptStatus })) return true;
  return /EVM execution reverted|(?:receipt status|EVM tx failed with receipt status)\s+0x0\b/i.test(
    error?.message || error?.data?.error || "",
  );
}

function isDefiniteCosmosTxFailure(error, source = {}) {
  const candidates = [source, error, source?.broadcast, error?.broadcast, source?.data, error?.data];
  for (const candidate of candidates) {
    const tx = candidate?.tx || candidate?.tx_response || candidate?.txResponse;
    const code = Number(
      tx?.code ??
        candidate?.txCode ??
        candidate?.tx_code ??
        candidate?.broadcast?.code,
    );
    if (Number.isFinite(code) && code !== 0) return true;
  }
  return false;
}

async function checkNullifierSpent(nullifier, { session = null } = {}) {
  assertPrivacySessionCurrent(session);
  try {
    const result = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().checkNullifier(nullifier),
    );
    return nullifierUsedFromResponse(result);
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    return null;
  }
}

async function latestRelayChainSnapshot() {
  const profile = activeChainProfile();
  if (String(profile?.transport || state.config?.transport || "").toLowerCase() === "evm") {
    const block = await clairveilBrowserClient(profile).evmJsonRpc(
      "eth_getBlockByNumber",
      ["latest", false],
    );
    return evmBlockChainSnapshot(block);
  }
  const health = await clairveilBrowserClient().health();
  const syncInfo = health?.status?.sync_info || {};
  const chainNowMs = Date.parse(syncInfo.latest_block_time || "");
  const chainHeight = String(syncInfo.latest_block_height || "").trim();
  if (!Number.isFinite(chainNowMs) || !chainHeight) {
    throw new Error("Latest chain block time or height is unavailable");
  }
  return { chainNowMs, chainHeight };
}

async function latestChainNowUnix(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const snapshot = await withPrivacySessionGuard(
    session,
    () => latestRelayChainSnapshot(),
  );
  assertPrivacySessionCurrent(session);
  const chainNowUnix = Math.floor(snapshot.chainNowMs / 1000);
  if (!Number.isSafeInteger(chainNowUnix) || chainNowUnix < 0) {
    throw new Error("Latest chain block time is unavailable");
  }
  return chainNowUnix;
}

function normalizedEvmTransactionQuantity(value, label) {
  const text = String(value ?? "0x0").trim();
  if (!/^0x[0-9a-fA-F]+$/.test(text)) {
    throw new Error(`${label} must be a hex quantity`);
  }
  return BigInt(text);
}

async function verifyPreparedWithdrawPayloadBeforeEvmBroadcast(
  data,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const payload = data?.payload;
  const transaction = data?.transaction;
  // Transfers and deposits have no withdraw payload. Their prepared EVM
  // transactions are still covered by their reservation/signing flow.
  if (!payload || !transaction) return;

  const chainNowUnix = await latestChainNowUnix({ session });
  const profile = activeChainProfile();
  const expectedChainId = String(profile?.chainId || state.config?.chainId || "");
  const accountPrefix = profile?.accountPrefix || state.config?.accountPrefix || "";
  if (!expectedChainId || !accountPrefix) {
    throw new Error("Active EVM profile cannot validate the withdraw payload");
  }
  validateRelayWithdrawPayload(payload, {
    chainNowUnix,
    expectedChainId,
    expectedRecipient: data?.prepared?.recipient || payload.recipient,
    accountPrefix,
  });
  const expected = createEvmPrivacyPrecompileAdapter({
    contractAddress:
      profile?.evmPrivacyPrecompileAddress ||
      state.config?.evmPrivacyPrecompileAddress,
    accountPrefix,
    chainId: expectedChainId,
  }).buildWithdrawTransaction(
    buildWithdrawMsgFromPayload(payload, "", chainNowUnix),
  );
  if (
    String(transaction.to || "").toLowerCase() !==
      String(expected.to || "").toLowerCase() ||
    String(transaction.data || "").toLowerCase() !==
      String(expected.data || "").toLowerCase() ||
    normalizedEvmTransactionQuantity(transaction.value, "withdraw transaction value") !==
      normalizedEvmTransactionQuantity(expected.value, "expected withdraw value")
  ) {
    throw new Error("Prepared withdraw transaction no longer matches its payload");
  }
  const expectedEvmChainId = expectedEvmChainIdHex();
  if (
    !expectedEvmChainId ||
    normalizedEvmTransactionQuantity(
      transaction.chainId,
      "withdraw transaction chainId",
    ) !== normalizedEvmTransactionQuantity(expectedEvmChainId, "expected EVM chainId")
  ) {
    throw new Error("Prepared withdraw transaction chainId no longer matches the active profile");
  }
  assertPrivacySessionCurrent(session);
}

function relaySnapshotNullifiers(snapshot = {}) {
  const nullifiers = new Set();
  const payloadNullifier =
    snapshot.payload?.nullifier_hex ||
    snapshot.payload?.nullifierHex ||
    snapshot.payload?.nullifier ||
    "";
  if (payloadNullifier) {
    nullifiers.add(String(payloadNullifier));
  }
  for (const nullifier of reservationNullifiersFromPrepared(
    snapshot.preparedData,
    snapshot.reservation,
  )) {
    nullifiers.add(nullifier);
  }
  return [...nullifiers];
}

async function relaySnapshotNullifierStatuses(snapshot, { session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const nullifiers = relaySnapshotNullifiers(snapshot);
  if (!nullifiers.length) return [];
  const statuses = await Promise.all(
    nullifiers.map(async (nullifier) => ({
      nullifier,
      spent: await checkNullifierSpent(nullifier, { session }),
    })),
  );
  assertPrivacySessionCurrent(session);
  return statuses;
}

async function verifyRelayPayloadNullifierUnspentBeforeBroadcast(
  payload,
  reservation,
  preparedData = state.keplr.relayWithdrawPreparedData,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const statuses = await withPrivacySessionGuard(
    session,
    () => relaySnapshotNullifierStatuses(
      {
        payload,
        preparedData,
        reservation,
      },
      { session },
    ),
  );
  if (!statuses.length) {
    throw new Error("Relay nullifier를 broadcast 직전에 확인할 수 없습니다.");
  }
  if (statuses.some((entry) => entry.spent == null)) {
    throw new Error(
      "Relay nullifier chain status를 확인하지 못했습니다. 잠시 후 다시 시도해줘.",
    );
  }
  if (statuses.some((entry) => entry.spent)) {
    await refreshCachedNoteStatuses({ session });
    await reconcileReservedNotesFromScan({ session });
    await refreshNoteReservationState({ session });
    assertPrivacySessionCurrent(session);
    throw new Error(
      "Relay payload의 note nullifier가 이미 사용됐습니다. Notes를 refresh 해줘.",
    );
  }
}

async function reconcileExpiredRelayWithdrawSnapshot(
  snapshot,
  chainSnapshot = null,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!snapshot || snapshot.submitted || snapshot.relayHash) return snapshot;
  let resolvedChainSnapshot = chainSnapshot;
  try {
    resolvedChainSnapshot ||= await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    return snapshot;
  }
  if (!relaySnapshotIsExpired(snapshot, resolvedChainSnapshot.chainNowMs)) return snapshot;
  const synced = await syncRelayWithdrawSnapshotReservation(snapshot, { session });
  const recoverySnapshot = await relaySnapshotWithFullReservationRecords(synced, {
    session,
  });
  if (!recoverySnapshot) return synced;
  const status = relayReservationStatus(
    recoverySnapshot.reservation,
    new Map(
      recoverySnapshot.reservation.reservations.map((record) => [
        record.reservation_id,
        record,
      ]),
    ),
  );
  if (status !== reservationStatuses.ProofReady) return synced;

  const nullifierStatuses = await withPrivacySessionGuard(
    session,
    () => relaySnapshotNullifierStatuses(recoverySnapshot, { session }),
  );
  const expiresAtUnix = relaySnapshotExpiresAtUnix(recoverySnapshot);
  const handedOff = Boolean(
    recoverySnapshot.handedOff ||
      recoverySnapshot.reservation.reservations.some(
        (record) => record.metadata?.relay_handed_off,
      ),
  );
  const expiryReviewMetadata = {
    relay_payload_expired: true,
    authoritative_expiry_confirmed: true,
    checked_height: resolvedChainSnapshot.chainHeight,
    expires_at_unix: expiresAtUnix,
    payload_hash:
      recoverySnapshot.payloadHash ||
      recoverySnapshot.payload?.payload_hash ||
      "",
    ...(handedOff
      ? {
          relay_handed_off: true,
          relay_payload_expired_after_handoff: true,
          no_broadcast_attempt: false,
        }
      : { no_broadcast_attempt: true }),
  };
  if (
    !nullifierStatuses.length ||
    nullifierStatuses.some((entry) => entry.spent == null)
  ) {
    // The handoff remains a durable external boundary even when the local
    // note cache cannot identify every input after a restart. Keep the lock,
    // but make the recovery actionable after a rescan rather than leaving a
    // permanently refresh-only ProofReady entry.
    const updated = await markReservationBatchManualReview(
      recoverySnapshot.reservation,
      new Error("relay payload expired and input nullifier evidence is unavailable"),
      "relay_payload_expired_nullifier_evidence_unavailable",
      expiryReviewMetadata,
      { session },
    );
    assertPrivacySessionCurrent(session);
    return {
      ...synced,
      reservation: updateReservationBatchRecords(
        synced.reservation,
        updated || [],
      ),
    };
  }
  if (nullifierStatuses.some((entry) => entry.spent)) {
    await refreshCachedNoteStatuses({ session });
    await reconcileReservedNotesFromScan({ session });
    await refreshNoteReservationState({ session });
    const records = await latestReservationRecords(synced.reservation, {
      session,
    });
    return {
      ...synced,
      reservation: updateReservationBatchRecords(synced.reservation, records),
    };
  }

  const updated = await markReservationBatchManualReview(
    recoverySnapshot.reservation,
    new Error("relay payload expired and nullifiers remain unspent"),
    "relay_payload_expired_requires_manual_review",
    expiryReviewMetadata,
    { session },
  );
  assertPrivacySessionCurrent(session);
  return {
    ...synced,
    reservation: updateReservationBatchRecords(
      synced.reservation,
      updated || [],
    ),
  };
}

async function resolveExpiredRelayManualReview(
  snapshot,
  chainSnapshot = null,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  // Capture the account that explicitly requested this recovery. The session
  // guard rejects normal wallet/profile changes; retaining this identity also
  // makes the approval binding fail closed if a caller changes account state
  // without first issuing the expected invalidation event.
  const operatorId = state.keplr.account;
  if (!operatorId) return snapshot;
  if (!snapshot) return snapshot;
  let resolvedChainSnapshot = chainSnapshot;
  try {
    resolvedChainSnapshot ||= await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    return snapshot;
  }
  if (!relaySnapshotIsExpired(snapshot, resolvedChainSnapshot.chainNowMs)) {
    return snapshot;
  }
  const recoverySnapshot = await relaySnapshotWithFullReservationRecords(snapshot, {
    session,
  });
  if (!recoverySnapshot) return snapshot;
  const records = recoverySnapshot.reservation.reservations || [];
  if (
    !records.length ||
    !records.every(
      (record) => record.status === reservationStatuses.ManualReview,
    )
  ) {
    return snapshot;
  }
  // Read the durable operation before gathering the final expiry, nullifier,
  // and transaction evidence. A relay handoff is an external boundary, so an
  // expired local payload alone cannot prove that it was never submitted.
  const finalSnapshot = await relaySnapshotWithFullReservationRecords(
    recoverySnapshot,
    { session },
  );
  const finalRecords = finalSnapshot?.reservation?.reservations || [];
  const reviewEvidenceByID = new Map(
    records.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    !finalSnapshot ||
    finalRecords.length !== records.length ||
    finalRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !==
          reservationOperationID(records[0]) ||
        reviewEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    return snapshot;
  }

  const payloadHash =
    finalSnapshot.payloadHash || finalSnapshot.payload?.payload_hash || "";
  if (
    !payloadHash ||
    finalRecords.some(
      (record) => String(record.payload_hash || "") !== String(payloadHash),
    )
  ) {
    return snapshot;
  }

  // Re-read durable operation evidence before the final chain/nullifier and
  // transaction checks. If another tab changes a payload hash, broadcast
  // evidence, or status during review, retain the ManualReview lock instead
  // of applying this operator approval.
  const transitionSnapshot = await relaySnapshotWithFullReservationRecords(
    finalSnapshot,
    { session },
  );
  const transitionRecords = transitionSnapshot?.reservation?.reservations || [];
  const finalEvidenceByID = new Map(
    finalRecords.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    !transitionSnapshot ||
    transitionRecords.length !== finalRecords.length ||
    transitionRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !==
          reservationOperationID(finalRecords[0]) ||
        finalEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    return snapshot;
  }
  // The transition is a value-moving recovery boundary. Recheck every
  // external evidence source against the final durable reservation snapshot,
  // then request ReplanRequired without another asynchronous gap.
  const [finalChainSnapshot, finalNullifierStatuses, transactionOutcomes] =
    await withPrivacySessionGuard(
      session,
      () =>
        Promise.all([
          latestRelayChainSnapshot(),
          relaySnapshotNullifierStatuses(transitionSnapshot, { session }),
          transitionRecords.every(reservationHasDurableNoBroadcastEvidence)
            ? Promise.resolve([])
            : Promise.all(
                transitionRecords.map((record) =>
                  recoveredReservationTxOutcome(record, { session }),
                ),
              ),
        ]),
    );
  if (
    !relaySnapshotIsExpired(transitionSnapshot, finalChainSnapshot.chainNowMs) ||
    !finalNullifierStatuses.length ||
    finalNullifierStatuses.some((entry) => entry.spent !== false)
  ) {
    return snapshot;
  }
  if (
    transactionOutcomes.some(
      (outcome) =>
        !outcome.checked ||
        !outcome.found ||
        !outcome.failed ||
        outcome.succeeded ||
        outcome.ambiguous,
    )
  ) {
    return snapshot;
  }
  // External evidence may have awaited while another tab updated the durable
  // operation. Require the same ManualReview records immediately before the
  // user-approved transition rather than relying only on the earlier read.
  const resolutionSnapshot = await relaySnapshotWithFullReservationRecords(
    transitionSnapshot,
    { session },
  );
  const resolutionRecords = resolutionSnapshot?.reservation?.reservations || [];
  const transitionEvidenceByID = new Map(
    transitionRecords.map((record) => [
      record.reservation_id,
      manualReviewResolutionEvidence(record),
    ]),
  );
  if (
    !resolutionSnapshot ||
    resolutionRecords.length !== transitionRecords.length ||
    resolutionRecords.some(
      (record) =>
        record.status !== reservationStatuses.ManualReview ||
        reservationOperationID(record) !==
          reservationOperationID(transitionRecords[0]) ||
        transitionEvidenceByID.get(record.reservation_id) !==
          manualReviewResolutionEvidence(record),
    )
  ) {
    return snapshot;
  }
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager?.resolveManualReview || state.keplr.account !== operatorId) {
    return snapshot;
  }
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.resolveManualReview(
      reservationIDs(resolutionSnapshot.reservation),
      {
        target: reservationStatuses.ReplanRequired,
        operatorId,
        approvalReference: `relay-expiry:${payloadHash || "unknown"}:${finalChainSnapshot.chainHeight || "unknown"}`,
        reason: "user approved replan after authoritative relay expiry and unspent nullifier check",
      },
    ),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return {
    ...snapshot,
    reservation: updateReservationBatchRecords(
      snapshot.reservation,
      updated || [],
    ),
  };
}

async function reconcileExpiredRelayWithdrawPayloads(
  chainSnapshot = null,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  let changed = false;
  const current = currentPreparedRelayWithdrawSnapshot();
  const currentPayload = state.keplr.relayWithdrawPayload;
  const currentPayloadVersion = state.keplr.relayWithdrawPayloadVersion;
  if (current) {
    const reconciled = await reconcileExpiredRelayWithdrawSnapshot(
      current,
      chainSnapshot,
      { session },
    );
    assertPrivacySessionCurrent(session);
    if (
      reconciled !== current &&
      state.keplr.relayWithdrawPayload === currentPayload &&
      state.keplr.relayWithdrawPayloadVersion === currentPayloadVersion
    ) {
      // Expiry recovery performs several chain and reservation reads. Do not
      // apply a result for the prior immutable payload version to a payload
      // that was prepared while those reads were in flight.
      state.keplr.relayWithdrawReservation =
        reconciled?.reservation || state.keplr.relayWithdrawReservation;
      changed = true;
    }
  }
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  if (pending.length) {
    const pendingByID = new Map(
      pending.map((snapshot) => [relayWithdrawPendingPayloadID(snapshot), snapshot]),
    );
    const reconciledPendingByID = new Map();
    for (const snapshot of pending) {
      const reconciled = await reconcileExpiredRelayWithdrawSnapshot(
        snapshot,
        chainSnapshot,
        { session },
      );
      assertPrivacySessionCurrent(session);
      reconciledPendingByID.set(relayWithdrawPendingPayloadID(snapshot), reconciled);
    }
    const latestPending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
      ? state.keplr.relayWithdrawPendingPayloads
      : [];
    const reconciledPending = [];
    for (const snapshot of latestPending) {
      const id = relayWithdrawPendingPayloadID(snapshot);
      const initial = pendingByID.get(id);
      const reconciled = reconciledPendingByID.get(id);
      // A user action can replace or remove this item while reconciliation is
      // waiting on chain data. Preserve that newer entry; it is responsible
      // for persisting its own recovery result.
      if (!initial || initial !== snapshot || !reconciled) {
        reconciledPending.push(snapshot);
        continue;
      }
      if (reconciled === initial && relaySnapshotNeedsPendingRecovery(reconciled)) {
        reconciledPending.push(snapshot);
        continue;
      }
      changed = true;
      if (relaySnapshotNeedsPendingRecovery(reconciled)) {
        reconciledPending.push(reconciled);
      }
    }
    if (changed) {
      state.keplr.relayWithdrawPendingPayloads = reconciledPending;
    }
  }
  if (changed) {
    await persistRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
  }
  void scheduleRelayPayloadExpiryReconciliation({ session });
}

async function reconcileDefiniteFailedReservation(
  data,
  error,
  attemptSource = {},
  reason = "transaction_failed_nullifier_unspent",
  fromStatus = reservationStatuses.ProofReady,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const reservation = preparedReservation(data) || attemptSource?.reservation;
  const ids = reservationIDs(reservation);
  if (!ids.length) return;
  const attempt = broadcastAttemptMetadata(attemptSource || error);
  if (!hasBroadcastAttemptMetadata(attempt)) return;
  const definiteExecutionFailure = reason.startsWith("evm_receipt_failed")
    ? "evm_receipt_failed"
    : reason.startsWith("cosmos_tx_code_failed")
      ? "cosmos_tx_code_failed"
      : "transaction_execution_failed";
  const unknownUpdated = await markReservationBatchUnknown(
    reservation,
    error,
    attempt,
    {
      fromStatus,
      definite_execution_failure: definiteExecutionFailure,
      reconcile_reason: `${reason}_recorded_before_nullifier_reconcile`,
    },
    { session },
  );
  assertPrivacySessionCurrent(session);
  let chainSnapshot;
  try {
    chainSnapshot = await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
  } catch (chainSnapshotError) {
    if (chainSnapshotError?.privacySessionInvalidated) throw chainSnapshotError;
    return unknownUpdated;
  }
  const nullifiers = reservationNullifiersFromPrepared(data, reservation);
  if (!nullifiers.length) return unknownUpdated;

  const spentResults = await withPrivacySessionGuard(
    session,
    () => Promise.all(
      nullifiers.map((nullifier) => checkNullifierSpent(nullifier, { session })),
    ),
  );
  if (spentResults.some((result) => result == null)) return unknownUpdated;
  if (spentResults.some(Boolean)) {
    await refreshCachedNoteStatuses({ session });
    await reconcileReservedNotesFromScan({ session });
    await refreshNoteReservationState({ session });
    assertPrivacySessionCurrent(session);
    return [];
  }

  const manager = currentNoteReservationManager({ optional: true });
  if (!manager || typeof manager.markReplanRequired !== "function") {
    return unknownUpdated;
  }
  let updated = [];
  try {
    updated = await withPrivacySessionGuard(
      session,
      () => manager.markReplanRequired(ids, {
        fromStatus: reservationStatuses.Unknown,
        txHash: attempt.txHash,
        txBytesHash: attempt.txBytesHash,
        signDocHash: attempt.signDocHash,
        nullifierUnspentConfirmed: true,
        txAbsentOrFailedConfirmed: true,
        checkedHeight: chainSnapshot.chainHeight,
        txHashChecked: attempt.txHash || attempt.txBytesHash || attempt.signDocHash,
        error: privacyReservationErrorCode(
          error,
          "transaction_execution_failed",
        ),
        metadata: {
          reconcile_reason: reason,
          no_broadcast_attempt: false,
        },
      }),
    );
  } catch (transitionError) {
    // Another tab may have completed the same safe reconciliation while this
    // transition was in flight. Establish that outcome from the durable
    // records, not from a reservation-manager error message.
    const current = await withPrivacySessionGuard(
      session,
      () => Promise.all(ids.map((id) => manager.getReservation(id))),
    );
    if (
      current.length !== ids.length ||
      current.some(
        (reservation) =>
          reservation?.status !== reservationStatuses.ReplanRequired,
      )
    ) {
      throw transitionError;
    }
    updated = current;
  }
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  return updated;
}

async function reconcileFailedEvmReservation(
  data,
  error,
  attemptSource = {},
  fromStatus = reservationStatuses.ProofReady,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!isDefiniteEvmReceiptFailure(error)) return;
  return reconcileDefiniteFailedReservation(
    data,
    error,
    attemptSource,
    "evm_receipt_failed_nullifier_unspent",
    fromStatus,
    { session },
  );
}

async function reconcileFailedCosmosReservation(
  data,
  error,
  attemptSource = {},
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!isDefiniteCosmosTxFailure(error, attemptSource)) return;
  return reconcileDefiniteFailedReservation(
    data,
    error,
    attemptSource,
    "cosmos_tx_code_failed_nullifier_unspent",
    reservationStatuses.ProofReady,
    { session },
  );
}

async function broadcastPreparedPrivacy(
  data,
  label = "privacy transaction",
  options = {},
) {
  let attemptedBroadcast = null;
  let durableBroadcastAttemptRecorded = false;
  let externalBroadcastBoundaryCrossed = false;
  let sdkReservationLifecycleManaged = false;
  const session = preparedPrivacySession(data) || beginPrivacySessionOperation();
  try {
    assertPrivacySessionCurrent(session);
    const broadcast = await withPreparedReservationHeartbeat(data, async ({ assertHeartbeatHealthy }) => {
      assertHeartbeatHealthy();
      const beforeBroadcast = async () => {
        await verifyPreparedNullifiersUnspentBeforeBroadcast(data, { session });
        await verifyPreparedWithdrawPayloadBeforeEvmBroadcast(data, { session });
        await markPreparedReservationBroadcastAttempting(data, label, { session });
        durableBroadcastAttemptRecorded = true;
        assertHeartbeatHealthy();
      };
      const onBroadcastStart = ({ externalBoundaryStarted = false } = {}) => {
        // MetaMask starts the wallet request before this callback. Preserve
        // that durable recovery fact even if the provider synchronously emits
        // an account/network invalidation while opening its approval UI.
        if (externalBoundaryStarted) {
          externalBroadcastBoundaryCrossed = true;
        }
        assertHeartbeatHealthy();
        assertPrivacySessionCurrent(session);
        externalBroadcastBoundaryCrossed = true;
      };
      let result;
      if (state.activeWallet === "metamask") {
        result = await sendEvmTransaction(data.transaction, {
          label,
          waitForReceipt: Boolean(options.waitForEvmReceipt),
          beforeBroadcast,
          onBroadcastStart,
          session,
        });
      } else {
        // ClairveilJS owns the complete reserved Cosmos submission lifecycle:
        // the signed checkpoint binds the exact TxRaw bytes, then the SDK
        // records BroadcastAttempting/Submitted/Unknown around its RPC call.
        // Calling the local marker here would create a second attempt and lose
        // the signed transaction identity required for recovery.
        await verifyPreparedNullifiersUnspentBeforeBroadcast(data, { session });
        result = await signDirectAndBroadcast(data.signDoc, {
          reservation: preparedReservation(data),
          ...(data?.payload ? { relayPayload: data.payload } : {}),
          session,
          onBroadcastStart,
        });
        result = await confirmDelayedCosmosBroadcast(result, { session });
        sdkReservationLifecycleManaged = Boolean(
          result.sdkReservationLifecycleManaged,
        );
      }
      attemptedBroadcast = result;
      assertHeartbeatHealthy();
      assertSuccessfulBroadcast(result, label);
      return result;
    }, { session });
    if (sdkReservationLifecycleManaged) {
      await refreshNoteReservationState({ session });
    } else {
      await recordSubmittedReservation(
        preparedReservation(data),
        broadcast,
        label,
        { session },
      );
    }
    return {
      ...broadcast,
      reservation: preparedReservation(data),
    };
  } catch (error) {
    if (error?.privacySessionInvalidated) {
      if (!externalBroadcastBoundaryCrossed) {
        await replanInvalidatedPreparedReservation(data, session, error);
        throw error;
      }
      throw reservationReconciliationRequiredError(
        label,
        attemptedBroadcast || error?.broadcast || error,
        error,
      );
    }
    if (error?.reservationReconciliationRequired) {
      throw error;
    }
    if (error?.reservationLifecycleHandled) {
      await refreshNoteReservationState({ session });
      throw error;
    }
    const attemptSource = attemptedBroadcast || error?.broadcast || error;
    if (error?.reservationSpentReconciled) {
      throw error;
    }
    if (
      !externalBroadcastBoundaryCrossed &&
      !hasBroadcastAttemptMetadata(broadcastAttemptMetadata(attemptSource))
    ) {
      // This path runs before the raw RPC boundary. Preserve the fact that
      // the user can safely prepare a new batch after the reservation is
      // replanned; it must not be rendered as an ambiguous chain result.
      const preBroadcastError = noBroadcastAttemptError(error);
      await noteReservationBookkeeping(() =>
        markPreparedReservationReplanRequired(data, preBroadcastError, undefined, {
          session,
        }),
      );
      throw preBroadcastError;
    }
    if (
      durableBroadcastAttemptRecorded &&
      error?.noBroadcastAttempt &&
      isMetaMaskUserRejectedError(error)
    ) {
      try {
        await markPreparedReservationBroadcastRejected(data, error, { session });
      } catch (bookkeepingError) {
        throw reservationReconciliationRequiredError(label, error, bookkeepingError);
      }
      throw error;
    }
    if (error?.reservationHeartbeatFailed && hasBroadcastAttemptMetadata(
      broadcastAttemptMetadata(attemptSource),
    )) {
      await noteReservationBookkeeping(() =>
        markPreparedReservationUnknown(data, error, attemptSource, {
          reconcile_reason: "reservation_lease_heartbeat_failed_after_broadcast",
        }, {
          session,
        }),
      );
      throw reservationReconciliationRequiredError(label, attemptSource, error);
    }
    if (
      state.activeWallet === "metamask" &&
      isDefiniteEvmReceiptFailure(error)
    ) {
      const reconciled = await noteReservationBookkeeping(() =>
        reconcileFailedEvmReservation(
          data,
          error,
          attemptSource,
          reservationStatuses.ProofReady,
          { session },
        ),
      );
      if (reconciled === undefined) {
        await noteReservationBookkeeping(() =>
          markPreparedReservationUnknown(data, error, attemptSource, {
            definite_execution_failure: "evm_receipt_failed",
            reconcile_reason: "evm_receipt_failed_pending_nullifier_reconcile",
          }, {
            session,
          }),
        );
      }
    } else if (isDefiniteCosmosTxFailure(error, attemptSource)) {
      const reconciled = await noteReservationBookkeeping(() =>
        reconcileFailedCosmosReservation(data, error, attemptSource, { session }),
      );
      if (reconciled === undefined) {
        await noteReservationBookkeeping(() =>
          markPreparedReservationUnknown(data, error, attemptSource, {
            definite_execution_failure: "cosmos_tx_code_failed",
            reconcile_reason: "cosmos_tx_code_failed_pending_nullifier_reconcile",
          }, {
            session,
          }),
        );
      }
    } else if (
      error?.noBroadcastAttempt &&
      !hasBroadcastAttemptMetadata(broadcastAttemptMetadata(attemptSource))
    ) {
      await noteReservationBookkeeping(() =>
        markPreparedReservationReplanRequired(data, error, undefined, { session }),
      );
    } else {
      const attempt = broadcastAttemptMetadata(attemptSource);
      await noteReservationBookkeeping(() =>
        hasBroadcastAttemptMetadata(attempt)
          ? markPreparedReservationUnknown(data, error, attemptSource, {}, { session })
          : markPreparedReservationManualReview(
              data,
              error,
              "opaque_broadcast_error_without_transaction_identity",
              {
                opaque_broadcast_error: true,
                no_broadcast_attempt: false,
                relay_handed_off: Boolean(preparedReservation(data)?.relay_handed_off),
              },
              { session },
            ),
      );
    }
    throw error;
  }
}

async function broadcastVeiledTransfer(
  amount,
  recipient,
  label = "veiled transfer",
  disclosure = {},
  options = {},
) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyTransferSignDoc(
    amount,
    recipient,
    disclosure,
    options,
  );
  const session = preparedPrivacySession(data) || beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  els.keplrTxState.textContent =
    state.activeWallet === "metamask"
      ? "Waiting for MetaMask"
      : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label);
  assertPrivacySessionCurrent(session);
  state.keplr.transferHash =
    broadcast.broadcast?.txhash || broadcast.txHash || "";
  return { ...broadcast, prepared: data.prepared };
}

function isExactMatchWithdrawError(error) {
  return (
    error?.code === "EXACT_NOTE_REQUIRED" ||
    error?.status === "exact_note_required"
  );
}

function isZeroHelperNeededError(error) {
  return (
    error?.code === "ZERO_DUMMY_REQUIRED" ||
    error?.status === "zero_dummy_required"
  );
}

function isSelfTransferRecipient(recipient) {
  return Boolean(
    state.keplr.shieldedAddress && recipient === state.keplr.shieldedAddress,
  );
}

async function createExactWithdrawNote(
  amount,
  hooks = {},
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  if (!state.keplr.shieldedAddress) {
    throw new Error("Clairveil shielded address is not ready");
  }

  const maxPlannerSteps = 20;
  for (let step = 1; step <= maxPlannerSteps; step += 1) {
    els.keplrTxState.textContent = "Preparing exact note";
    hooks.onPlanCheck?.(step, maxPlannerSteps);

    let data;
    try {
      data = await preparePrivacyTransferSignDoc(
        amount,
        state.keplr.shieldedAddress,
        {},
        { allowPlanStep: true },
      );
      assertPrivacySessionCurrent(session);
    } catch (error) {
      assertPrivacySessionCurrent(session);
      if (!isZeroHelperNeededError(error)) {
        throw error;
      }
      hooks.onZeroHelperNeeded?.(error, step, maxPlannerSteps);
      await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", {
        waitForEvmReceipt: true,
      });
      assertPrivacySessionCurrent(session);
      await refreshPrivacySurfaces({ session });
      assertPrivacySessionCurrent(session);
      continue;
    }

    if (
      data.prepared?.isFinal === false ||
      data.prepared?.planAction === "self_merge"
    ) {
      hooks.onSelfMergeNeeded?.(data, step, maxPlannerSteps);
      const selfMergeConfirmed = await confirmPreparedTransferBeforeBroadcast(
        data,
        { session, selfTransfer: true },
      );
      if (!isPrivacySessionCurrent(session)) {
        throw privacySessionInvalidatedError();
      }
      if (!selfMergeConfirmed) {
        const error = noBroadcastAttemptError(
          new Error("Prepared self transaction was cancelled before wallet signing"),
        );
        error.preparedSelfTransferCancelled = true;
        throw error;
      }
      els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
      const plannerBroadcast = await broadcastPreparedPrivacy(
        data,
        "exact-note self transaction",
        { waitForEvmReceipt: true },
      );
      assertPrivacySessionCurrent(session);
      state.keplr.transferHash =
        plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
      await refreshPrivacySurfaces({ session });
      assertPrivacySessionCurrent(session);
      continue;
    }

    hooks.onFinalExactTransfer?.(data, step, maxPlannerSteps);
    const finalSelfTransferConfirmed = await confirmPreparedTransferBeforeBroadcast(
      data,
      { session, selfTransfer: true },
    );
    if (!isPrivacySessionCurrent(session)) {
      throw privacySessionInvalidatedError();
    }
    if (!finalSelfTransferConfirmed) {
      const error = noBroadcastAttemptError(
        new Error("Prepared exact-note self transfer was cancelled before wallet signing"),
      );
      error.preparedSelfTransferCancelled = true;
      throw error;
    }
    els.keplrTxState.textContent =
      state.activeWallet === "metamask"
        ? "Waiting for MetaMask"
        : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(
      data,
      "exact-note self transfer",
      { waitForEvmReceipt: true },
    );
    assertPrivacySessionCurrent(session);
    state.keplr.transferHash =
      broadcast.broadcast?.txhash || broadcast.txHash || "";
    await refreshPrivacySurfaces({ session });
    assertPrivacySessionCurrent(session);
    return data;
  }

  throw new Error(
    "Withdraw에 필요한 exact note 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.",
  );
}

async function sendFromKeplr() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  const account = state.keplr.account;
  const wallet = state.activeWallet;
  if (!account) return;
  const actionLock = beginPrivacyValueAction("public_send", session);
  if (!actionLock) return;
  setBusy(els.sendFromKeplr, true);
  renderKeplr();
  els.keplrTxState.textContent = "Preparing send";
  try {
    const recipient = requireValidSendRecipient();
    if (wallet === "metamask") {
      const transaction = clairveilBrowserClient().evmNativeSendTransaction({
        to: recipient,
        amount: amountInputValue(els.keplrSendAmount),
      });
      els.keplrTxState.textContent = "Waiting for MetaMask";
      const broadcast = await sendEvmTransaction(transaction, {
        label: "EVM send",
        session,
      });
      assertPrivacySessionCurrent(session);
      assertSuccessfulBroadcast(broadcast, "EVM send");
      state.keplr.sendHash = broadcast.txHash || "";
      els.keplrTxState.textContent = "Send submitted";
      renderKeplr();
      showSendResult({
        success: true,
        wallet: "MetaMask",
        txHash: state.keplr.sendHash,
      });
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.sendHash = included.txHash || state.keplr.sendHash;
          els.keplrTxState.textContent = "Send included";
          await Promise.allSettled([
            refreshWalletBalance({ session }),
            refreshBlockEvents({ session }),
          ]);
          assertPrivacySessionCurrent(session);
          renderKeplr();
        },
        onFailed: (error) => {
          els.keplrTxState.textContent = "Send failed";
          showSendResult({ success: false, error: error.message });
        },
      });
      return;
    }

    const signDoc = await withPrivacySessionGuard(
      session,
      () => clairveilBrowserClient().buildBankSendSignDoc({
        from: account,
        pubKeyHex: state.keplr.pubkeyHex,
        to: recipient,
        amount: amountInputValue(els.keplrSendAmount),
      }),
    );
    assertPrivacySessionCurrent(session);
    els.keplrTxState.textContent = "Waiting for Keplr";
    const broadcast = await signDirectAndBroadcast(signDoc, { session });
    assertPrivacySessionCurrent(session);
    state.keplr.sendHash = broadcast.broadcast?.txhash || "";
    els.keplrTxState.textContent = "Send included";
    renderKeplr();
    showSendResult({
      success: true,
      wallet: "Keplr",
      txHash: state.keplr.sendHash,
    });
    await Promise.allSettled([
      refreshWalletBalance({ session }),
      refreshBlockEvents({ session }),
    ]);
    assertPrivacySessionCurrent(session);
    renderKeplr();
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    els.keplrTxState.textContent = "Send failed";
    showSendResult({
      success: false,
      error: error.message,
    });
  } finally {
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.sendFromKeplr, false);
      renderKeplr();
    }
  }
}

async function depositFromKeplr() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account) return;
  if (!hasDepositProofProvider()) {
    showNotice({
      title: "Deposit unavailable",
      message:
        "Deposit requires a DepositCircuit proof provider. Configure the active profile's depositProofUrl, use a browser/WASM provider, or enable the loopback local helper before using Deposit.",
      failed: true,
    });
    return;
  }
  if (depositInFlight) return;
  const actionLock = beginPrivacyValueAction("deposit", session);
  if (!actionLock) return;
  const depositLock = Object.freeze({ generation: session.generation });
  depositInFlight = true;
  depositInFlightLock = depositLock;
  setBusy(els.depositFromKeplr, true);
  let depositConfirmedOnChain = false;
  try {
    const privacySetupReady = await setupKeplrPrivacy();
    if (!isPrivacySessionCurrent(session)) return;
    if (!privacySetupReady || !state.keplr.rootSignatureBase64) return;

    els.keplrTxState.textContent = "Preparing deposit";
    const broadcast = await broadcastPrivacyDeposit(
      amountInputValue(els.keplrDepositAmount),
    );
    assertPrivacySessionCurrent(session);
    const isPendingEvm = Boolean(broadcast.pending);
    if (isPendingEvm) {
      els.keplrTxState.textContent = "Deposit submitted";
      renderKeplr();
      showNotice({
        title: "Deposit submitted",
        message: `${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit was submitted.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`,
      });
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.depositHash = included.txHash || state.keplr.depositHash;
          state.keplr.depositHeight =
            included.receipt?.blockNumber || state.keplr.depositHeight;
          els.keplrTxState.textContent = "Deposit included";
          const recovered = await withPrivacySessionGuard(
            session,
            () => refreshDepositNoteRecovery({ session }),
          );
          assertPrivacySessionCurrent(session);
          reportDepositNoteRecovery(recovered);
          renderKeplr();
        },
        onFailed: (error) => {
          assertPrivacySessionCurrent(session);
          els.keplrTxState.textContent = "Deposit failed";
          showNotice({
            title: "Deposit 실패",
            message: privacyOperationErrorMessage(error),
            failed: true,
          });
        },
      });
      return;
    }
    els.keplrTxState.textContent = "Verifying deposit";
    renderKeplr();
    await confirmCosmosDepositBeforeRecovery(broadcast, { session });
    assertPrivacySessionCurrent(session);
    depositConfirmedOnChain = true;
    els.keplrTxState.textContent = "Deposit included; recovering note";
    renderKeplr();
    showNotice({
      title: "Deposit included; recovering note",
      message: `Keplr deposit transaction was verified against the prepared encrypted note. The shielded balance remains unavailable until sync completes.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`,
    });
    const recovered = await withPrivacySessionGuard(
      session,
      () => refreshDepositNoteRecovery({ session }),
    );
    assertPrivacySessionCurrent(session);
    reportDepositNoteRecovery(recovered);
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    els.keplrTxState.textContent = error?.depositConfirmationRequired
      ? "Deposit confirmation required"
      : depositConfirmedOnChain
        ? "Deposit recovery required"
        : "Deposit failed";
    showNotice({
      title: error?.depositConfirmationRequired
        ? "Deposit confirmation required"
        : depositConfirmedOnChain
          ? "Deposit recovery required"
          : "Deposit 실패",
      message: error?.depositConfirmationRequired
        ? error.message
        : depositConfirmedOnChain
          ? "The deposit was confirmed, but local encrypted-note recovery did not complete. Scan Notes or reset and rescan the encrypted local cache; do not repeat the deposit while reconciliation is pending."
          : privacyOperationErrorMessage(error),
      failed: true,
    });
  } finally {
    // A prior session may settle after invalidation has already admitted a
    // new deposit. Only its own lock may clear the replacement session's
    // in-flight state.
    if (depositInFlightLock === depositLock) {
      depositInFlight = false;
      depositInFlightLock = null;
    }
    endPrivacyValueAction(actionLock);
    if (!isPrivacySessionCurrent(session)) return;
    setBusy(els.depositFromKeplr, false);
    renderKeplr();
  }
}

async function resetAndRescanKeplrNotes() {
  const session = beginPrivacySessionOperation();
  if (
    !state.keplr.account ||
    !state.keplr.rootSignatureBase64 ||
    !state.keplr.scanError ||
    noteScanInFlight ||
    noteScanResetInFlight ||
    isPrivacyValueActionInFlight()
  ) {
    return;
  }
  const confirmed = window.confirm(
    "Reset the encrypted local note cache and rescan from the beginning? This removes only this browser's cached notes and scan cursor. Notes are recovered from the chain; active reservations are not released.",
  );
  if (!confirmed) return;
  assertPrivacySessionCurrent(session);

  const lock = {};
  noteScanResetInFlight = true;
  noteScanResetLock = lock;
  renderKeplr();
  try {
    await scanKeplrNotes({ reset: true, session });
    assertPrivacySessionCurrent(session);
  } catch (error) {
    if (!error?.privacySessionInvalidated) toast(privacySyncErrorMessage(error));
  } finally {
    if (noteScanResetLock === lock) {
      noteScanResetInFlight = false;
      noteScanResetLock = null;
      if (isPrivacySessionCurrent(session)) renderKeplr();
    }
  }
}

async function scanKeplrNotes(options = {}) {
  const session = options.session || beginPrivacySessionOperation();
  if (!state.keplr.account) return;
  if (!options.skipPrivacySetup) {
    const setupReady = await setupKeplrPrivacy();
    if (!setupReady) return;
  }
  assertPrivacySessionCurrent(session);
  if (!state.keplr.rootSignatureBase64) return;
  if (noteScanInFlight) return;
  // Scanning can make fresh inventory available to the UI, so do not rely on
  // the setup-time check or a previously cached chain-safety result. Every
  // scan, including background recovery scans, validates the active profile's
  // live chain, protocol, and reserve configuration before loading notes.
  try {
    await refreshChainSafety({ force: true, session });
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    const stagedError = privacyScanStageFailure(error, "chain-safety");
    invalidatePrivacyScanState(stagedError);
    if (!options.quiet) {
      els.keplrTxState.textContent = "Scan failed";
      toast(privacySyncErrorMessage(stagedError));
      renderKeplr();
    }
    if (options.throwOnError) throw stagedError;
    return;
  }
  assertPrivacySessionCurrent(session);
  if (noteScanInFlight) return;

  const lock = {};
  noteScanInFlight = true;
  noteScanLock = lock;
  setBusy(els.scanKeplrNotes, true);
  renderKeplr();
  if (!options.quiet) {
    els.keplrTxState.textContent = "Scanning notes";
  }
  let scanStage = "encrypted-cache";
  try {
    const reset = Boolean(options.reset);
    const noteStore = currentWalletNoteStore({ optional: false });
    if (reset) {
      await withPrivacySessionGuard(session, () => noteStore.clear());
      assertPrivacySessionCurrent(session);
    }
    const scanOptions = noteScanRequestOptions({
      reset,
      requireComplete: true,
    });
    // ClairveilJS persists automatically when it receives a noteStore. Keep
    // the network scan separate instead: an old session may finish its scan
    // after the wallet/profile changes, but it must never merge that result
    // into the replacement session's durable cache. The cursor is loaded only
    // to derive the exact typed-scan resume request, then the result is merged
    // after a fresh session check.
    scanStage = "encrypted-cache";
    const cachedBeforeScan = await withPrivacySessionGuard(
      session,
      () => noteStore.load(),
    );
    let resumeOptions = {};
    try {
      scanStage = "cursor-validation";
      resumeOptions = resumeTypedNoteScanOptions(
        cachedBeforeScan,
        scanOptions,
      );
    } catch (error) {
      if (reset) throw error;
      // An older WebApp stored a cursor shape that cannot establish an exact
      // typed-scan resume point. It is safe to discard only that local cache
      // and start the next request from genesis; never continue from an
      // ambiguous cursor that could skip an encrypted output.
      scanStage = "encrypted-cache";
      await withPrivacySessionGuard(session, () => noteStore.clear());
      assertPrivacySessionCurrent(session);
      resumeOptions = {};
    }

    const fetchTypedScan = async (extra = {}) => {
      scanStage = "typed-query";
      const result = await withPrivacySessionGuard(
        session,
        () => clairveilBrowserClient().scanWalletNotes(
          privacyRequest({
            ...scanOptions,
            ...extra,
            includeFoundNotes: true,
          }),
        ),
      );
      assertPrivacySessionCurrent(session);
      if (String(result?.scanCursor?.source ?? result?.scan_cursor?.source ?? "") !== "privacy_scan") {
        // ClairveilJS can fall back for a generic client, but this WebApp must
        // preserve one typed-cursor semantic. The SDK may already have merged
        // the fallback result, so erase it before surfacing the sync failure.
        scanStage = "encrypted-cache";
        await withPrivacySessionGuard(session, () => noteStore.clear());
        assertPrivacySessionCurrent(session);
        throw new Error(
          "The configured node does not provide privacy-scan-v2. Notes were not accepted; retry after the unified privacy scan endpoint is available.",
        );
      }
      return result;
    };

    let scan = await fetchTypedScan(resumeOptions);
    scanStage = "encrypted-cache";
    // `mergeScanResult` returns the normalized state committed under the same
    // IndexedDB lock. Use that authoritative result instead of opening a
    // second store immediately afterwards: a stale browser record must not
    // turn an already-completed typed scan into a false cursor failure.
    let cached = await withPrivacySessionGuard(session, () =>
      noteStore.mergeScanResult(scan, {
        owner: state.keplr.shieldedAddress,
      }),
    );
    assertPrivacySessionCurrent(session);
    scanStage = "cursor-validation";
    if (!applyPersistedWalletNoteState(cached)) {
      if (!reset) {
        // The node response was validated, but this old cache still failed to
        // persist an exact cursor. Replace it with one authoritative full
        // typed scan in the same user action instead of asking the user to
        // discover and repeat a separate Reset & rescan flow.
        scanStage = "encrypted-cache";
        await withPrivacySessionGuard(session, () => noteStore.clear());
        assertPrivacySessionCurrent(session);
        scan = await fetchTypedScan();
        scanStage = "encrypted-cache";
        cached = await withPrivacySessionGuard(session, () =>
          noteStore.replaceScanResult(scan, {
            owner: state.keplr.shieldedAddress,
          }),
        );
        assertPrivacySessionCurrent(session);
        scanStage = "cursor-validation";
      }
    }
    if (!applyPersistedWalletNoteState(cached)) {
      await withPrivacySessionGuard(session, () => noteStore.clear());
      assertPrivacySessionCurrent(session);
      throw new Error(
        "The typed privacy scan cursor was invalid. The encrypted note cache was discarded; rescan from the unified endpoint.",
      );
    }
    // A previous failed scan deliberately blocks spending through scanError.
    // This scan has now authenticated and applied a complete typed cursor, so
    // clear only that stale failure before evaluating completion. Otherwise a
    // retry can be reported as incomplete even when has_more is false.
    state.keplr.scanError = "";
    scanStage = "scan-completion";
    if (!hasCompletedPrivacyNoteScan()) {
      throw new Error(
        `Privacy note sync did not complete within the ${completeNoteScanMaxPages}-page safety limit. Scan again to resume before preparing a spend.`,
      );
    }
    try {
      await refreshCachedNoteStatuses({ session, noteStore });
      assertPrivacySessionCurrent(session);
      await reconcileReservedNotesFromScan({ session });
      assertPrivacySessionCurrent(session);
      await refreshNoteReservationState({ session });
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      // The typed scan and its encrypted note record are already complete.
      // Do not erase recovered notes merely because the follow-up status or
      // reservation reconciliation failed. Keeping scanError blocks every
      // spending action until a retry succeeds, while still letting the user
      // confirm that an included deposit output was recovered.
      state.keplr.scanError = privacyPostScanErrorMessage(error);
      if (!options.quiet) {
        els.keplrTxState.textContent = "Notes recovered; sync requires review";
        toast(state.keplr.scanError);
      }
      renderKeplr();
      return scan;
    }
    let batchRecoveryDeferred = false;
    try {
      // A checkpointed batch has its own reservation evidence and a dedicated
      // recovery flow. Once the live reservation map above has been applied,
      // a corrupt or stale batch UI checkpoint must not discard otherwise
      // verified note inventory or mark the whole note sync as failed.
      await reconcileBatchTransferArtifact({
        session,
        restoreUi: true,
        notify: !options.quiet,
      });
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      batchRecoveryDeferred = true;
      warnReservationBookkeeping(error);
    }
    let relayRecoveryDeferred = false;
    try {
      // Relay-payload expiry review is independent of the typed note cursor
      // and of the active-reservation map above. A stale relay handoff can
      // need manual review without making freshly verified, unreserved notes
      // unsafe to display or spend. Keep the relay lock intact, surface its
      // own recovery card, and do not turn that unrelated review into a
      // failed note sync.
      await reconcileExpiredRelayWithdrawPayloads(null, { session });
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      relayRecoveryDeferred = true;
      warnReservationBookkeeping(error);
    }
    state.keplr.scanError = "";
    if (!options.quiet) {
      const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
      const unverifiedCount = state.keplr.notes.filter(isUnverifiedNote).length;
      const operationRecoveryDeferred =
        batchRecoveryDeferred || relayRecoveryDeferred;
      els.keplrTxState.textContent = operationRecoveryDeferred
        ? "Notes ready; saved operation recovery requires review"
        : unverifiedCount
          ? `Scan completed with ${unverifiedCount} unverified notes`
          : "Ready";
      toast(
        operationRecoveryDeferred
          ? "Notes scanned. A saved batch or relay operation still needs separate review; its reservation remains locked."
          : unverifiedCount
          ? `Keplr notes scanned; ${unverifiedCount} notes hidden until nullifier status is verified`
          : `Keplr notes scanned (${cursor.pagesScanned} pages)`,
      );
    }
    renderKeplr();
  } catch (error) {
    if (error?.privacySessionInvalidated) return;
    const stagedError = privacyScanStageFailure(error, scanStage);
    const scanFailureMessage = privacySyncErrorMessage(stagedError);
    invalidatePrivacyScanState(stagedError);
    if (!options.quiet) {
      els.keplrTxState.textContent = "Scan failed";
      toast(scanFailureMessage);
    }
    if (options.throwOnError) throw stagedError;
  } finally {
    if (noteScanLock === lock) {
      noteScanInFlight = false;
      noteScanLock = null;
      if (isPrivacySessionCurrent(session)) {
        setBusy(els.scanKeplrNotes, false);
        renderKeplr();
      }
    }
  }
}

async function refreshPrivacySurfaces(
  { balance = false, session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const tasks = [
    refreshEvents({ session }),
    refreshAuditorTransfers(),
    scanKeplrNotes({ quiet: true, session }),
    refreshNotes({ session }),
  ];
  if (balance) {
    tasks.unshift(refreshWalletBalance({ session }));
  }
  await Promise.allSettled(tasks);
  assertPrivacySessionCurrent(session);
}

const submittedOperationReconciliationRetry = Object.freeze({
  attempts: 5,
  intervalMs: 1_250,
});

function waitForSubmittedOperationReconciliationRetry(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  return new Promise((resolve) => {
    globalThis.setTimeout(
      resolve,
      submittedOperationReconciliationRetry.intervalMs,
    );
  }).then(() => {
    assertPrivacySessionCurrent(session);
  });
}

const includedDepositRecoveryRetry = Object.freeze({
  attempts: 5,
  intervalMs: 1250,
});

function includedDepositRecoveryComplete() {
  return (
    isChainSafetyReady() &&
    hasCompletedPrivacyNoteScan() &&
    hasRecoveredDepositNote()
  );
}

function waitForIncludedDepositRecoveryRetry(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  return new Promise((resolve) => {
    globalThis.setTimeout(resolve, includedDepositRecoveryRetry.intervalMs);
  }).then(() => {
    assertPrivacySessionCurrent(session);
  });
}

async function recoverIncludedDepositNote(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  // Transaction inclusion and the typed privacy-scan index are separate
  // local services. `confirmDeposit` proves this exact encrypted output was
  // included, so it is safe to retry the read-only scan before treating the
  // browser cache as unrecoverable.
  for (let attempt = 0; attempt < includedDepositRecoveryRetry.attempts; attempt += 1) {
    if (includedDepositRecoveryComplete()) return true;
    if (!noteScanInFlight) {
      try {
        await scanKeplrNotes({
          quiet: true,
          skipPrivacySetup: true,
          throwOnError: true,
          session,
        });
      } catch (error) {
        if (error?.privacySessionInvalidated) throw error;
        // A transient typed-scan/index response is retried below. Do not
        // replace the confirmed deposit result with a generic scan error.
      }
    }
    assertPrivacySessionCurrent(session);
    if (includedDepositRecoveryComplete()) return true;
    if (attempt + 1 < includedDepositRecoveryRetry.attempts) {
      await waitForIncludedDepositRecoveryRetry({ session });
    }
  }

  if (!noteScanInFlight) {
    try {
      // The confirmed output can still be absent when an older encrypted
      // cursor was persisted from an interrupted scan. Resetting this local
      // cache never touches chain state or reservations; the next typed scan
      // reconstructs every owned note from the beginning.
      await scanKeplrNotes({
        reset: true,
        quiet: true,
        skipPrivacySetup: true,
        throwOnError: true,
        session,
      });
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
    }
  }
  assertPrivacySessionCurrent(session);
  return includedDepositRecoveryComplete();
}

async function refreshDepositNoteRecovery(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  await refreshPrivacySurfaces({ balance: true, session });
  assertPrivacySessionCurrent(session);
  // refreshPrivacySurfaces intentionally lets the independent UI refreshes
  // settle together. A chain-safety preflight can therefore fail while a
  // previous typed-scan cursor remains complete in the encrypted cache.
  // Never report the new deposit note as recovered from that stale cache.
  if (includedDepositRecoveryComplete()) {
    return true;
  }

  els.keplrTxState.textContent = "Deposit included; retrying encrypted note recovery";
  renderKeplr();
  if (await recoverIncludedDepositNote({ session })) return true;
  assertPrivacySessionCurrent(session);

  const txHash = normalizedPrivacyTxHash(state.keplr.depositHash);
  const recoveryMessage = txHash
    ? "The deposit transaction is included, but this browser did not recover its encrypted output note. Reset & rescan Notes from the beginning before relying on your shielded balance."
    : "The deposit transaction is included, but the encrypted note has not been recovered. Retry Scan before relying on your shielded balance.";
  state.keplr.scanError = recoveryMessage;
  els.keplrTxState.textContent = "Deposit included; note recovery required";
  renderKeplr();
  showNotice({
    title: "Deposit included; note recovery required",
    message: recoveryMessage,
    failed: true,
  });
  return false;
}

function reportDepositNoteRecovery(recovered) {
  if (!recovered) return;
  showNotice({
    title: "Deposit included; note sync completed",
    message:
      `The encrypted note scan completed for this ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit. Review My Notes to see the recovered note.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`,
  });
}

function submittedOperationIsReconciled(data, records = []) {
  const ids = reservationIDs(preparedReservation(data));
  if (!ids.length || records.length !== ids.length) return false;
  const operationEvidenceHash = batchTransferOperationEvidenceHash(data);
  if (operationEvidenceHash) {
    return batchTransferReservationsSucceeded(records, {
      expectedCount: ids.length,
      expectedOperationEvidenceHash: operationEvidenceHash,
    });
  }
  return records.every(
    (record) => record?.status === reservationStatuses.ConfirmedSpent,
  );
}

async function refreshSubmittedOperationReconciliation(
  data,
  {
    balance = false,
    session = preparedPrivacySession(data) || beginPrivacySessionOperation(),
  } = {},
) {
  // A Cosmos inclusion result and the typed privacy-scan index can arrive a
  // few blocks apart. Do not turn a known-successful transaction into a
  // failed-looking batch merely because its first post-commit scan raced the
  // indexer or local reservation write.
  for (let attempt = 0; attempt < submittedOperationReconciliationRetry.attempts; attempt += 1) {
    try {
      assertPrivacySessionCurrent(session);
      await refreshPrivacySurfaces({ balance, session });
      assertPrivacySessionCurrent(session);
      const records = await latestReservationRecords(preparedReservation(data), {
        session,
      });
      assertPrivacySessionCurrent(session);
      if (
        isChainSafetyReady() &&
        hasCompletedPrivacyNoteScan() &&
        submittedOperationIsReconciled(data, records)
      ) {
        return true;
      }
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
    }
    if (attempt + 1 < submittedOperationReconciliationRetry.attempts) {
      await waitForSubmittedOperationReconciliationRetry({ session });
    }
  }
  return false;
}

async function reconcileSubmittedSelfTransferForContinuation(
  data,
  error,
  { session = preparedPrivacySession(data) || beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  // A planner self-transaction changes the available note set. If its tx is
  // known to be included but only the local reservation write failed, a full
  // scan can prove its inputs spent and safely unlock the next planner step.
  // Never continue on an opaque error without a transaction identity.
  const txHash = broadcastTxHash(error);
  if (!txHash) return false;
  state.keplr.transferHash = txHash;
  updateTransferFlow(
    "zero",
    "Self transaction 반영 확인 중",
    "Self transaction이 포함되었습니다. 새 note를 확인한 뒤 원래 트랜스퍼를 계속합니다.",
  );
  const reconciled = await refreshSubmittedOperationReconciliation(data, {
    session,
  });
  assertPrivacySessionCurrent(session);
  if (reconciled) {
    updateTransferFlow(
      "zero",
      "노트 준비 계속",
      "Self transaction의 note 반영을 확인했습니다. 원래 트랜스퍼를 계속 준비합니다.",
    );
  }
  return reconciled;
}

function submittedOperationReconciliationCopy(operation, reconciled) {
  if (reconciled) {
    return `${operation} transaction is included and its input-note reconciliation completed.`;
  }
  return `${operation} transaction is included, but note scan or input-nullifier reconciliation has not completed. Retry Scan before relying on the shielded balance or attempting another spend. Input notes remain locked and resubmission is blocked.`;
}

function reportSubmittedOperationReconciliation(
  operation,
  reconciled,
  { flowID = null } = {},
) {
  const message = submittedOperationReconciliationCopy(operation, reconciled);
  if (flowID !== null && !transferFlowIsCurrent(flowID)) {
    showNotice({
      title: reconciled
        ? `Earlier ${operation} reconciled`
        : `Earlier ${operation} needs reconciliation`,
      message,
      failed: !reconciled,
    });
    return false;
  }

  els.keplrTxState.textContent = reconciled
    ? `${operation} included; note state reconciled`
    : `${operation} included; reconciliation required`;
  renderKeplr();
  if (!reconciled) {
    showNotice({
      title: `${operation} included; reconciliation required`,
      message,
      failed: true,
    });
  }
  return true;
}

function batchTransferOperationEvidence(data) {
  return (
    data?.operationEvidence ||
    data?.prepared?.operationEvidence ||
    null
  );
}

function batchTransferOperationEvidenceHash(data) {
  return String(
    data?.operationEvidenceHash ||
      data?.operation_evidence_hash ||
      data?.prepared?.operationEvidenceHash ||
      data?.prepared?.operation_evidence_hash ||
      "",
  );
}

function batchTransferExpectedOutputs(data) {
  const evidence = batchTransferOperationEvidence(data);
  return Array.isArray(evidence?.expected_outputs)
    ? evidence.expected_outputs
    : [];
}

function renderVerifiedBatchTransferEvidence(data, payments) {
  const outputs = batchTransferExpectedOutputs(data);
  if (outputs.length !== payments.length) {
    throw new Error(
      "Batch reconciliation did not return one expected output evidence record per payment",
    );
  }
  const matchedItems = new Set();
  for (const payment of payments) {
    const output = outputs.find(
      (candidate) =>
        String(candidate?.item_id ?? candidate?.itemId ?? "") ===
        payment.itemId,
    );
    const outputItemID = String(output?.item_id ?? output?.itemId ?? "");
    const expectedAmount = String(
      output?.expected_amount ?? output?.expectedAmount ?? "",
    );
    const expectedDenom = String(
      output?.expected_denom ?? output?.expectedDenom ?? "",
    );
    const expectedCommitment = String(
      output?.expected_output_commitment ??
        output?.expectedOutputCommitment ??
        "",
    );
    const expectedRecipientHash = String(
      output?.expected_recipient_hash ?? output?.expectedRecipientHash ?? "",
    ).toLowerCase();
    const expectedAmountHash = String(
      output?.expected_amount_hash ?? output?.expectedAmountHash ?? "",
    ).toLowerCase();
    const expectedUserDisclosureDigest = String(
      output?.expected_user_disclosure_digest ??
        output?.expectedUserDisclosureDigest ??
        "",
    );
    const expectedAuditDisclosureDigest = String(
      output?.expected_audit_disclosure_digest ??
        output?.expectedAuditDisclosureDigest ??
        "",
    );
    const expectedPrivacyPolicy =
      payment.userPrivacyPolicy === "all-private"
        ? 0
        : payment.userPrivacyPolicy === "amount"
          ? 1
          : -1;
    const expectedDisclosureMode =
      payment.userDisclosureMode === "none"
        ? 0
        : payment.userDisclosureMode === "public"
          ? 1
          : payment.userDisclosureMode === "recipient-encrypted"
            ? 2
            : -1;
    const recipientHash = hashRecipient(payment.recipient, {
      shieldedPrefix: activeChainProfile()?.shieldedPrefix,
    }).toLowerCase();
    const amountHash = hashAmount(
      baseDenom(),
      payment.amountValue,
    ).toLowerCase();
    if (
      !output ||
      matchedItems.has(outputItemID) ||
      String(output.role || "") !== "payment" ||
      expectedAmount !== String(payment.amountValue) ||
      expectedDenom !== baseDenom() ||
      !expectedCommitment ||
      expectedRecipientHash !== recipientHash ||
      expectedAmountHash !== amountHash ||
      Number(output.user_privacy_policy ?? output.userPrivacyPolicy) !==
        expectedPrivacyPolicy ||
      Number(output.user_disclosure_mode ?? output.userDisclosureMode) !==
        expectedDisclosureMode ||
      !expectedAuditDisclosureDigest ||
      (expectedDisclosureMode === 0
        ? Boolean(expectedUserDisclosureDigest)
        : !expectedUserDisclosureDigest)
    ) {
      throw new Error(
        `Batch output evidence for payment ${payment.itemId} is missing or does not match the prepared payment`,
      );
    }
    matchedItems.add(outputItemID);
    const index =
      output?.output_index ??
      output?.outputIndex ??
      output?.batch_item_index ??
      output?.batchItemIndex;
    setBatchTransferItemEvidence(
      [payment.itemId],
      index === undefined || index === null
        ? "Verified"
        : `Verified · output ${index}`,
      "verified",
    );
  }
}

function currentBatchTransferInputCapacity() {
  return batchTransferAvailableNotes()
    .slice(0, batchTransferMaxInputs)
    .reduce((sum, note) => sum + noteAmountValue(note), 0n);
}

function nextExplicitAtomicBatch(payments) {
  const inputCapacity = currentBatchTransferInputCapacity();
  return selectNextAtomicBatchPayments(payments, {
    inputCapacity,
    maxOutputs: batchTransferMaxOutputs,
  });
}

async function batchTransferBroadcastRequiresReconciliation(
  data,
  error,
  {
    session = preparedPrivacySession(data) || beginPrivacySessionOperation(),
  } = {},
) {
  const sourceEvidence =
    hasBroadcastAttemptMetadata(broadcastAttemptMetadata(error)) ||
    hasBroadcastAttemptMetadata(
      broadcastAttemptMetadata(error?.broadcast || {}),
    );
  const directResult = batchTransferNeedsReconciliation({
    explicit: Boolean(error?.reservationReconciliationRequired),
    noBroadcastAttempt: Boolean(error?.noBroadcastAttempt),
    hasBroadcastEvidence: sourceEvidence,
  });
  if (directResult || !isPrivacySessionCurrent(session)) {
    return directResult;
  }
  let records = [];
  try {
    records = await latestReservationRecords(preparedReservation(data), {
      session,
    });
    assertPrivacySessionCurrent(session);
  } catch (lookupError) {
    if (lookupError?.privacySessionInvalidated) throw lookupError;
  }
  return batchTransferNeedsReconciliation({
    noBroadcastAttempt: Boolean(error?.noBroadcastAttempt),
    hasBroadcastEvidence: sourceEvidence,
    reservations: records,
  });
}

async function executeAtomicBatchTransfer(
  payments,
  {
    session = beginPrivacySessionOperation(),
    batchNumber = 1,
    split = false,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const itemIds = payments.map((payment) => payment.itemId);
  setBatchTransferItemEvidence(itemIds, "Preparing");
  els.batchTransferState.textContent =
    split ? `Preparing atomic batch ${batchNumber}` : "Preparing atomic batch";
  const data = await preparePrivacyBatchTransferSignDoc(payments);
  assertPrivacySessionCurrent(session);
  const actualInputCount = Number(data.prepared?.inputCount || 0);
  const actualOutputCount = Number(data.prepared?.outputCount || 0);
  if (
    actualInputCount < 1 ||
    actualInputCount > batchTransferMaxInputs ||
    actualOutputCount < 1 ||
    actualOutputCount > batchTransferMaxOutputs
  ) {
    throw new Error("Prepared batch exceeds the 16-input / 32-output circuit");
  }
  const confirmed = await confirmPreparedBatchTransferBeforeBroadcast(
    data,
    payments,
  );
  assertPrivacySessionCurrent(session);
  if (!confirmed) {
    setBatchTransferItemEvidence(itemIds, "Cancelled");
    els.batchTransferState.textContent =
      "Prepared batch cancelled before wallet signing";
    return { cancelled: true };
  }
  setBatchTransferItemEvidence(itemIds, "Awaiting Keplr");
  els.batchTransferState.textContent =
    `Ready: ${actualInputCount} inputs / ${actualOutputCount} outputs · all-or-nothing`;
  let broadcast;
  try {
    broadcast = await broadcastPreparedPrivacy(
      data,
      split ? `atomic batch ${batchNumber}` : "atomic batch transfer",
    );
  } catch (error) {
    if (
      await batchTransferBroadcastRequiresReconciliation(
        data,
        error,
        { session },
      )
    ) {
      if (error && typeof error === "object") {
        error.batchTransferReconciliationRequired = true;
      } else {
        const wrapped = new Error("Batch transfer result is inconclusive");
        wrapped.batchTransferReconciliationRequired = true;
        throw wrapped;
      }
    }
    throw error;
  }
  try {
    assertPrivacySessionCurrent(session);
    const txHash =
      broadcast.broadcast?.txhash || broadcast.txHash || "";
    state.keplr.batchTransferHash = txHash;
    setBatchTransferItemEvidence(itemIds, "Included · verifying");
    els.batchTransferState.textContent = "Included; verifying output evidence";
    renderKeplr();

    const reconciled = await refreshSubmittedOperationReconciliation(data, {
      session,
    });
    assertPrivacySessionCurrent(session);
    if (!reconciled) {
      setBatchTransferItemEvidence(itemIds, "Pending evidence");
      els.batchTransferState.textContent =
        "Included; output evidence reconciliation required";
      const error = reservationReconciliationRequiredError(
        "Batch transfer",
        broadcast,
        new Error(
          "Batch transfer was included, but input nullifier and output evidence reconciliation has not completed",
        ),
      );
      // The broadcast result itself was accepted. Only the local scan and
      // payment-output evidence lagged, so preserve that distinction for the
      // user-facing status instead of describing it as an inconclusive wallet
      // request.
      error.batchTransferIncludedPendingEvidence = true;
      error.batchTransferIncludedTxHash = txHash;
      throw error;
    }

    renderVerifiedBatchTransferEvidence(data, payments);
    els.batchTransferState.textContent =
      split ? `Atomic batch ${batchNumber} verified` : "Atomic batch verified";
    await clearBatchTransferArtifact({ session });
    assertPrivacySessionCurrent(session);
    return { data, broadcast, cancelled: false };
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    if (error && typeof error === "object") {
      error.batchTransferReconciliationRequired = true;
      throw error;
    }
    const wrapped = new Error(
      "Batch transfer was included, but local evidence verification failed",
    );
    wrapped.batchTransferReconciliationRequired = true;
    throw wrapped;
  }
}

async function transferBatchFromVeiled() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account || !batchTransferFeatureEnabled()) return;
  const actionLock = beginPrivacyValueAction("batch_transfer", session);
  if (!actionLock) return;
  batchTransferInFlight = true;
  let currentBatchItemIDs = [];
  setBusy(els.prepareBatchTransfer, true);
  try {
    await setupKeplrPrivacy();
    if (!isPrivacySessionCurrent(session)) return;
    if (!state.keplr.rootSignatureBase64) return;
    const payments = collectBatchTransferPayments({ strict: true });
    const preview = batchTransferPreview();
    if (!preview.totalCovered) {
      throw new Error("Scanned spendable notes do not cover the batch total");
    }
    const split = preview.requiresSplit && els.batchTransferSplit.checked;
    if (preview.requiresSplit && !split) {
      throw new Error(
        "This draft exceeds one atomic batch. Explicitly choose multiple atomic batches or reduce the draft.",
      );
    }
    const pending = [...payments];
    let completedBatches = 0;
    while (pending.length) {
      assertPrivacySessionCurrent(session);
      const group = split ? nextExplicitAtomicBatch(pending) : pending;
      const batchNumber = completedBatches + 1;
      currentBatchItemIDs = group.map((payment) => payment.itemId);
      const result = await executeAtomicBatchTransfer(group, {
        session,
        batchNumber,
        split,
      });
      assertPrivacySessionCurrent(session);
      if (result.cancelled) {
        showNotice({
          title: "Batch transfer cancelled",
          message:
            completedBatches > 0
              ? `${completedBatches} earlier atomic batch(es) remain committed. The next prepared batch was cancelled before wallet signing.`
              : "The prepared batch proof was discarded before wallet signing. No batch transaction was submitted.",
        });
        return;
      }
      completedBatches += 1;
      markBatchTransferItemsCompleted(
        group.map((payment) => payment.itemId),
      );
      currentBatchItemIDs = [];
      pending.splice(0, group.length);
      if (!split) break;
    }
    els.batchTransferState.textContent =
      completedBatches > 1
        ? `${completedBatches} atomic batches verified`
        : "Atomic batch verified";
    showNotice({
      title: "Batch transfer completed",
      message:
        completedBatches > 1
          ? `${completedBatches} separately atomic batches were included. Every payment now has verified output evidence.`
          : "The entire batch was included atomically and every payment now has verified output evidence.",
    });
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    const pendingRows = currentBatchItemIDs.length
      ? currentBatchItemIDs
      : batchTransferRows()
          .filter((row) => {
            const evidence = row.querySelector("[data-batch-evidence]");
            return [
              "Preparing",
              "Awaiting Keplr",
              "Included · verifying",
              "Pending evidence",
              "Verifying",
            ].includes(evidence?.textContent || "");
          })
          .map((row) => row.dataset.batchItemId);
    const preparationReview = error?.batchTransferManualReview || null;
    const artifactReview = error?.batchTransferArtifactReview || null;
    const stalledArtifactReview =
      error?.batchTransferStalledArtifactReview || null;
    const preparationFailedBeforeWallet = Boolean(
      error?.batchTransferPreparationFailedBeforeWallet,
    );
    const confirmationFailedBeforeWallet = Boolean(
      error?.batchTransferConfirmationFailedBeforeWallet,
    );
    const noBroadcastAttempt = Boolean(error?.noBroadcastAttempt);
    const includedPendingEvidence = Boolean(
      error?.batchTransferIncludedPendingEvidence,
    );
    const requiresReconciliation =
      !preparationReview && batchTransferErrorRequiresReconciliation(error);
    if (stalledArtifactReview) {
      setBatchTransferItemEvidence(pendingRows, "Pending review");
      els.batchTransferState.textContent =
        "Stalled batch preparation needs explicit cancellation";
      openReservationReviewDialog(
        stalledArtifactReview.operationID,
        stalledArtifactReview.records,
        { stalledBatchReservation: stalledArtifactReview.reservation },
      );
    } else if (artifactReview) {
      setBatchTransferItemEvidence(pendingRows, "Pending review");
      els.batchTransferState.textContent =
        "Previous batch checkpoint needs explicit cancellation";
      openReservationReviewDialog(
        artifactReview.operationID,
        artifactReview.records,
        { preparedBatchReservation: artifactReview.reservation },
      );
    } else if (preparationReview) {
      setBatchTransferItemEvidence(pendingRows, "Pending review");
      els.batchTransferState.textContent =
        "Batch proof preparation needs reservation review";
      // This is a pre-wallet preparation failure, not an uncertain broadcast.
      // Open the existing explicit-cancellation flow for the exact durable
      // reservation so the user can check its nullifier and release it rather
      // than hunting through a 0-input preview for the hidden lock.
      openReservationReviewDialog(
        preparationReview.operationID,
        preparationReview.records,
        { knownNoWalletRequest: true },
      );
    } else if (includedPendingEvidence) {
      setBatchTransferItemEvidence(pendingRows, "Included · evidence pending");
      els.batchTransferState.textContent =
        "Batch included; payment evidence is still syncing";
    } else if (requiresReconciliation) {
      setBatchTransferItemEvidence(pendingRows, "Pending review");
      els.batchTransferState.textContent =
        "Included or submitted; reconciliation required";
    } else if (
      preparationFailedBeforeWallet ||
      confirmationFailedBeforeWallet ||
      noBroadcastAttempt
    ) {
      // No transaction identity exists on these paths. The reservation was
      // either never made or has been replanned, so do not leave a frightening
      // generic failure that suggests the user must reconcile an on-chain
      // transaction which was never submitted.
      setBatchTransferItemEvidence(pendingRows, "Not submitted");
      els.batchTransferState.textContent =
        "Batch was not submitted; ready after the reported fix";
    } else {
      setBatchTransferItemEvidence(pendingRows, "Failed", "failed");
      els.batchTransferState.textContent = "Batch failed";
    }
    if (stalledArtifactReview || artifactReview || preparationReview) return;
    showNotice({
      title:
        includedPendingEvidence
          ? "Batch transfer included; verification pending"
          : requiresReconciliation
          ? "Batch transfer requires reconciliation"
          : "Batch transfer failed",
      message:
        includedPendingEvidence
          ? `The chain accepted this batch${error?.batchTransferIncludedTxHash ? ` (Tx: ${shorten(error.batchTransferIncludedTxHash, 14, 12)})` : ""}, but the local note scan has not yet verified every payment output. Do not submit it again; the DApp will retry reconciliation during Scan Notes.`
          : requiresReconciliation
          ? "The wallet request crossed the broadcast boundary, but its final chain result is not yet conclusive. Do not submit the batch again; refresh Notes to reconcile it."
          : preparationFailedBeforeWallet
            ? privacyOperationErrorMessage(
                error,
                error?.batchTransferPreparationStage === "preflight"
                  ? batchTransferPreflightErrorMessage(error)
                  : "Batch proof preparation failed before wallet signing. No batch transaction was submitted. Retry after checking the proof provider.",
              )
            : confirmationFailedBeforeWallet
              ? privacyOperationErrorMessage(
                  error,
                  "The prepared batch did not pass local confirmation checks. No wallet request or transaction was submitted. Refresh Notes and prepare it again.",
                )
              : noBroadcastAttempt
                ? privacyOperationErrorMessage(
                    error,
                    "The batch was not submitted to the chain. Keplr signing or the final pre-broadcast check failed; no transaction needs reconciliation. Reconnect Keplr or refresh Notes, then prepare a new batch.",
                  )
            : privacyOperationErrorMessage(
              error,
              "Atomic batch transfer failed. Refresh Notes before retrying.",
            ),
      failed: !includedPendingEvidence,
    });
  } finally {
    batchTransferInFlight = false;
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.prepareBatchTransfer, false);
      renderKeplr();
    }
  }
}

async function transferFromVeiled() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account) return;
  let flowID = null;
  const actionLock = beginPrivacyValueAction("transfer", session);
  if (!actionLock) return;
  setBusy(els.transferFromVeiled, true);
  try {
    await setupKeplrPrivacy();
    if (!isPrivacySessionCurrent(session)) return;
    if (!state.keplr.rootSignatureBase64) return;

    const amount = amountInputValue(els.veiledTransferAmount);
    const recipient = els.veiledTransferRecipient.value.trim();
    if (!recipient) {
      toast(
        `Enter the recipient's ${shieldedPrefix()} address in Transfer recipient.`,
      );
      return;
    }
    if (!isConfiguredShieldedAddress(recipient)) {
      toast(
        `Transfer recipient must be a ${shieldedPrefix()} shielded address, not a transparent account address.`,
      );
      return;
    }
    if (isSelfTransferRecipient(recipient)) {
      toast(
        "이 주소는 내 shielded address야. 여기로 보내면 외부 전송이 아니라 note split/change self-transfer가 돼.",
      );
      return;
    }
    let disclosure;
    try {
      disclosure = transferDisclosurePolicy();
    } catch (error) {
      toast(error.message);
      return;
    }
    const confirmed = await openTransferFlowModal();
    if (!isPrivacySessionCurrent(session)) return;
    if (!confirmed) return;
    flowID = transferFlowState.flowID;

    els.keplrTxState.textContent = "Preparing veiled transfer";
    const maxPlannerSteps = 20;
    let finalData = null;

    for (let step = 1; step <= maxPlannerSteps; step += 1) {
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        step === 1 ? "노트 확인 중" : "노트 재확인 중",
        "요청 금액을 보낼 수 있는 note 조합이 있는지 확인합니다.",
      );

      let data;
      try {
        data = await preparePrivacyTransferSignDoc(
          amount,
          recipient,
          disclosure,
          { allowPlanStep: true },
        );
        assertPrivacySessionCurrent(session);
      } catch (error) {
        assertPrivacySessionCurrent(session);
        if (!isZeroHelperNeededError(error)) {
          throw error;
        }
        showTransferPlannerFacts({
          requested: amount,
          action: `${zeroCoinText()} helper note를 만들어 다음 self transaction에 사용합니다.`,
        });
        updateTransferFlow(
          "zero",
          "Self transaction 서명 대기",
          "요청 금액을 만들기 위해 note 정리가 필요합니다. 이 단계는 내 Veiled balance 안에서 note를 재구성하며, 받는 사람에게는 아직 전송되지 않습니다.",
        );
        await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", {
          waitForEvmReceipt: true,
        });
        assertPrivacySessionCurrent(session);
        await refreshPrivacySurfaces({ session });
        assertPrivacySessionCurrent(session);
        continue;
      }

      if (
        data.prepared?.isFinal === false ||
        data.prepared?.planAction === "self_merge"
      ) {
        showTransferPlannerFacts({
          requested: amount,
          currentMax: plannerCurrentTransferMaxForNoteMerge(data, amount),
          action: `두 note를 합쳐 ${data.prepared?.amount || "새 note"} note를 만듭니다.`,
        });
        updateTransferFlow(
          "zero",
          "Self transaction 서명 대기",
          "요청 금액을 만들기 위해 note 정리가 필요합니다. 이 단계는 내 Veiled balance 안에서 note를 재구성하며, 받는 사람에게는 아직 전송되지 않습니다.",
        );
        const selfMergeConfirmed = await confirmPreparedTransferBeforeBroadcast(
          data,
          { session, selfTransfer: true },
        );
        if (!isPrivacySessionCurrent(session) || !selfMergeConfirmed) {
          return;
        }
        updateTransferFlow(
          "zero",
          "Self transaction 서명 대기",
          "승인된 self transaction을 지갑에서 확인하고 서명해 주세요.",
        );
        els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
        try {
          const plannerBroadcast = await broadcastPreparedPrivacy(
            data,
            "self transaction",
            { waitForEvmReceipt: true },
          );
          assertPrivacySessionCurrent(session);
          state.keplr.transferHash =
            plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
          await refreshPrivacySurfaces({ session });
          assertPrivacySessionCurrent(session);
        } catch (error) {
          if (!error?.reservationReconciliationRequired) throw error;
          const reconciled = await reconcileSubmittedSelfTransferForContinuation(
            data,
            error,
            { session },
          );
          assertPrivacySessionCurrent(session);
          if (!reconciled) throw error;
        }
        continue;
      }

      finalData = data;
      break;
    }

    if (!finalData) {
      throw new Error(
        "입력하신 금액의 노트 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.",
      );
    }

    resetTransferPlannerFacts();
    els.keplrTxState.textContent = "Reviewing prepared transfer";
    const finalConfirmed = await confirmPreparedTransferBeforeBroadcast(
      finalData,
      { session },
    );
    if (!isPrivacySessionCurrent(session) || !finalConfirmed) {
      if (isPrivacySessionCurrent(session) && !finalConfirmed) {
        els.keplrTxState.textContent = "Transfer cancelled before broadcast";
      }
      return;
    }
    updateTransferFlow(
      "transfer",
      "트랜스퍼 서명 대기",
      `준비된 effect를 확인했습니다. 이제 ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 동일한 최종 전송 내용을 확인하고 서명해 주세요.`,
    );
    els.keplrTxState.textContent =
      state.activeWallet === "metamask"
        ? "Waiting for MetaMask"
        : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(
      finalData,
      "privacy transfer",
    );
    assertPrivacySessionCurrent(session);
    state.keplr.transferHash =
      broadcast.broadcast?.txhash || broadcast.txHash || "";
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm
      ? "Transfer submitted"
      : "Transfer included";
    renderKeplr();
    finishTransferFlow(
      isPendingEvm
        ? "트랜스퍼 요청이 제출되었습니다"
        : "트랜스퍼가 포함되었습니다 · note 상태 확인 중",
      true,
      {
        flowID,
        successCopy: isPendingEvm
          ? "트랜잭션이 제출되었습니다. receipt 확인 뒤 input note와 reservation을 reconcile합니다."
          : "트랜잭션이 포함되었습니다. 암호화된 note와 input nullifier 상태를 확인하고 있습니다.",
      },
    );
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.transferHash =
            included.txHash || state.keplr.transferHash;
          const reconciled = await withPrivacySessionGuard(
            session,
            () => refreshSubmittedOperationReconciliation(finalData, { session }),
          );
          assertPrivacySessionCurrent(session);
          if (!reportSubmittedOperationReconciliation("Transfer", reconciled, {
            flowID,
          })) return;
          finishTransferFlow(
            reconciled
              ? "트랜스퍼 요청이 성공하였습니다"
              : "트랜스퍼는 포함되었지만 reconciliation이 필요합니다",
            true,
            {
              flowID,
              successCopy: submittedOperationReconciliationCopy(
                "Transfer",
                reconciled,
              ),
            },
          );
        },
        onFailed: async (error) => {
          await noteReservationBookkeeping(() =>
            reconcileFailedEvmReservation(
              finalData,
              error,
              broadcast,
              reservationStatuses.Submitted,
              { session },
            ),
          );
          assertPrivacySessionCurrent(session);
          if (!transferFlowIsCurrent(flowID)) {
            showNotice({
              title: "Earlier Transfer failed",
              message: privacyOperationErrorMessage(error),
              failed: true,
            });
            return;
          }
          els.keplrTxState.textContent = "Transfer failed";
          finishTransferFlow(privacyOperationErrorMessage(error), false, { flowID });
        },
      });
      return;
    }
    const reconciled = await withPrivacySessionGuard(
      session,
      () => refreshSubmittedOperationReconciliation(finalData, { session }),
    );
    assertPrivacySessionCurrent(session);
    reportSubmittedOperationReconciliation("Transfer", reconciled, { flowID });
    finishTransferFlow(
      reconciled
        ? "트랜스퍼 요청이 성공하였습니다"
        : "트랜스퍼는 포함되었지만 reconciliation이 필요합니다",
      true,
      {
        flowID,
        successCopy: submittedOperationReconciliationCopy("Transfer", reconciled),
      },
    );
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    if (error?.reservationReconciliationRequired) {
      state.keplr.transferHash =
        broadcastTxHash(error) || state.keplr.transferHash;
      els.keplrTxState.textContent =
        "Transfer submitted; reservation reconciliation required";
      finishTransferFlow(
        `${error.message}${state.keplr.transferHash ? `\nTx: ${state.keplr.transferHash}` : ""}`,
        true,
        { flowID },
      );
      return;
    }
    els.keplrTxState.textContent = "Transfer failed";
    finishTransferFlow(privacyOperationErrorMessage(error), false, { flowID });
  } finally {
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.transferFromVeiled, false);
      renderKeplr();
    }
  }
}

async function withdrawFromVeiled() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account) return;
  let flowID = null;
  const actionLock = beginPrivacyValueAction("withdraw", session);
  if (!actionLock) return;
  setBusy(els.withdrawFromVeiled, true);
  try {
    let amount;
    try {
      amount = amountInputValue(els.veiledWithdrawAmount);
    } catch (error) {
      toast(error.message);
      return;
    }
    let recipient;
    try {
      recipient = requireValidPrivacyWithdrawRecipient(
        els.veiledWithdrawRecipient.value,
      );
    } catch (error) {
      toast(error.message);
      return;
    }

    await setupKeplrPrivacy();
    if (!isPrivacySessionCurrent(session)) return;
    if (!state.keplr.rootSignatureBase64) return;

    const confirmed = await openTransferFlowModal("withdraw");
    if (!isPrivacySessionCurrent(session)) return;
    if (!confirmed) return;
    flowID = transferFlowState.flowID;

    els.keplrTxState.textContent = "Preparing withdraw";
    resetTransferPlannerFacts();
    updateTransferFlow(
      "zero",
      "노트 확인 중",
      "Withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다.",
    );
    let data;
    try {
      data = await preparePrivacyWithdrawSignDoc(amount, recipient);
      assertPrivacySessionCurrent(session);
    } catch (error) {
      assertPrivacySessionCurrent(session);
      if (!isExactMatchWithdrawError(error)) {
        throw error;
      }
      showTransferPlannerFacts({
        requested: amount,
        action: `${coinText(amount)} exact note를 만들기 위해 self transaction을 요청합니다.`,
      });
      updateTransferFlow(
        "zero",
        "Self transaction 서명 대기",
        "Withdraw는 입력 금액과 정확히 같은 note가 필요합니다. 지금은 내 Veiled balance 안에서 exact note를 먼저 만듭니다.",
      );
      await createExactWithdrawNote(amount, {
        onPlanCheck: (step) => {
          updateTransferFlow(
            "zero",
            step === 1 ? "노트 확인 중" : "노트 재확인 중",
            "Withdraw에 필요한 exact note를 만들 수 있는 note 조합을 확인합니다.",
          );
        },
        onSelfMergeNeeded: (data) => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `두 note를 합쳐 ${data.prepared?.amount || data.plan?.nextAmount || "더 큰"} self note를 만듭니다.`,
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "요청 금액의 exact note를 만들기 위해 두 note를 먼저 합칩니다. 이 단계는 내 Veiled balance 안에서만 준비됩니다.",
          );
        },
        onZeroHelperNeeded: () => {
          showTransferPlannerFacts({
            requested: amount,
            action: `${zeroCoinText()} zero note를 만들어 exact note self transaction에 사용합니다.`,
          });
          updateTransferFlow(
            "zero",
            "Zero note 서명 대기",
            "exact note를 만들기 위한 보조 zero note가 필요합니다. 이 단계도 내 Veiled balance 안에서만 준비됩니다.",
          );
        },
        onFinalExactTransfer: (data) => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `${coinText(amount)} exact note를 만드는 마지막 self transaction을 요청합니다.`,
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "입력 금액과 정확히 같은 note를 만들기 위해 self transaction을 요청합니다.",
          );
        },
      }, { session });
      assertPrivacySessionCurrent(session);
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. withdraw sign-doc을 다시 준비합니다.",
      );
      data = await preparePrivacyWithdrawSignDoc(amount, recipient);
      assertPrivacySessionCurrent(session);
    }
    updateTransferFlow(
      "transfer",
      "위드드로우 서명 대기",
      `note 준비가 완료되었습니다. 이제 Clair balance로 이동할 withdraw를 요청합니다. ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 최종 내용을 확인하고 서명해 주세요.`,
    );
    els.keplrTxState.textContent =
      state.activeWallet === "metamask"
        ? "Waiting for MetaMask"
        : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "privacy withdraw");
    assertPrivacySessionCurrent(session);
    state.keplr.withdrawHash =
      broadcast.broadcast?.txhash || broadcast.txHash || "";
    state.keplr.withdrawHeight =
      broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm
      ? "Withdraw submitted"
      : "Withdraw included";
    renderKeplr();
    finishTransferFlow(
      isPendingEvm
        ? "Withdraw 요청이 제출되었습니다"
        : "Withdraw가 포함되었습니다 · input note 확인 중",
      true,
      {
        flowID,
        successCopy: isPendingEvm
          ? "트랜잭션이 제출되었습니다. receipt 확인 뒤 input note와 reservation을 reconcile합니다."
          : "트랜잭션이 포함되었습니다. input nullifier와 reservation 상태를 확인하고 있습니다.",
      },
    );
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.withdrawHash =
            included.txHash || state.keplr.withdrawHash;
          state.keplr.withdrawHeight =
            included.receipt?.blockNumber || state.keplr.withdrawHeight;
          const reconciled = await withPrivacySessionGuard(
            session,
            () => refreshSubmittedOperationReconciliation(data, {
              balance: true,
              session,
            }),
          );
          assertPrivacySessionCurrent(session);
          if (!reportSubmittedOperationReconciliation("Withdraw", reconciled, {
            flowID,
          })) return;
          finishTransferFlow(
            reconciled
              ? "Withdraw 요청이 성공하였습니다"
              : "Withdraw는 포함되었지만 reconciliation이 필요합니다",
            true,
            {
              flowID,
              successCopy: submittedOperationReconciliationCopy(
                "Withdraw",
                reconciled,
              ),
            },
          );
        },
        onFailed: async (error) => {
          await noteReservationBookkeeping(() =>
            reconcileFailedEvmReservation(
              data,
              error,
              broadcast,
              reservationStatuses.Submitted,
              { session },
            ),
          );
          assertPrivacySessionCurrent(session);
          if (!transferFlowIsCurrent(flowID)) {
            showNotice({
              title: "Earlier Withdraw failed",
              message: privacyOperationErrorMessage(error),
              failed: true,
            });
            return;
          }
          els.keplrTxState.textContent = "Withdraw failed";
          finishTransferFlow(privacyOperationErrorMessage(error), false, { flowID });
        },
      });
      return;
    }
    const reconciled = await withPrivacySessionGuard(
      session,
      () => refreshSubmittedOperationReconciliation(data, {
        balance: true,
        session,
      }),
    );
    assertPrivacySessionCurrent(session);
    reportSubmittedOperationReconciliation("Withdraw", reconciled, { flowID });
    finishTransferFlow(
      reconciled
        ? "Withdraw 요청이 성공하였습니다"
        : "Withdraw는 포함되었지만 reconciliation이 필요합니다",
      true,
      {
        flowID,
        successCopy: submittedOperationReconciliationCopy("Withdraw", reconciled),
      },
    );
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    if (error?.reservationReconciliationRequired) {
      state.keplr.withdrawHash =
        broadcastTxHash(error) || state.keplr.withdrawHash;
      state.keplr.withdrawHeight = state.keplr.withdrawHeight || "pending";
      els.keplrTxState.textContent =
        "Withdraw submitted; reservation reconciliation required";
      finishTransferFlow(
        `${error.message}${state.keplr.withdrawHash ? `\nTx: ${state.keplr.withdrawHash}` : ""}`,
        true,
        { flowID },
      );
      return;
    }
    els.keplrTxState.textContent = "Withdraw failed";
    finishTransferFlow(privacyOperationErrorMessage(error), false, { flowID });
  } finally {
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.withdrawFromVeiled, false);
      renderKeplr();
    }
  }
}

function beginRelayWithdrawPreparation() {
  return {
    version: advanceRelayWithdrawPayloadGeneration(),
    activeWallet: state.activeWallet,
    keplrAccount: state.keplr.account,
    evmAccount: state.wallet.account,
    chainProfileID: state.selectedChainProfileId,
    amountInput: els.relayWithdrawAmount.value,
    recipientInput: els.relayWithdrawRecipient.value.trim(),
  };
}

function relayWithdrawPreparationIsCurrent(preparation) {
  return Boolean(
    preparation &&
      state.keplr.relayWithdrawPayloadVersion === preparation.version &&
      state.activeWallet === preparation.activeWallet &&
      state.keplr.account === preparation.keplrAccount &&
      state.wallet.account === preparation.evmAccount &&
      state.selectedChainProfileId === preparation.chainProfileID &&
      els.relayWithdrawAmount.value === preparation.amountInput &&
      els.relayWithdrawRecipient.value.trim() === preparation.recipientInput,
  );
}

async function rejectStaleRelayWithdrawPreparation(
  data,
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const error = noBroadcastAttemptError(
    new Error(
      "Relay withdraw preparation was invalidated by changed inputs or wallet state",
    ),
  );
  try {
    await markReservationBatchReplanRequired(
      preparedReservation(data),
      error,
      "relay_withdraw_preparation_invalidated_before_install",
      { session },
    );
  } catch (cleanupError) {
    if (cleanupError?.privacySessionInvalidated) throw cleanupError;
    error.cleanupError = cleanupError;
  }
  assertPrivacySessionCurrent(session);
  throw error;
}

async function relayWithdrawFromVeiled() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account) return;
  const actionLock = beginPrivacyValueAction("relay_withdraw", session);
  if (!actionLock) return;
  setBusy(els.relayWithdrawFromVeiled, true);
  try {
    let amount;
    try {
      amount = amountInputValue(els.relayWithdrawAmount);
    } catch (error) {
      toast(error.message);
      return;
    }
    let recipient;
    try {
      recipient = requireValidPrivacyWithdrawRecipient(
        els.relayWithdrawRecipient.value,
        "Relay withdraw recipient",
      );
    } catch (error) {
      toast(error.message);
      return;
    }

    await setupKeplrPrivacy();
    if (!isPrivacySessionCurrent(session)) return;
    if (!state.keplr.rootSignatureBase64) return;

    const confirmed = await openTransferFlowModal("relayWithdraw");
    if (!isPrivacySessionCurrent(session)) return;
    if (!confirmed) return;

    const preparation = beginRelayWithdrawPreparation();
    els.keplrTxState.textContent = "Preparing relay withdraw";
    resetTransferPlannerFacts();
    updateTransferFlow(
      "zero",
      "노트 확인 중",
      "Relay withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다.",
    );
    let data;
    try {
      data = await preparePrivacyRelayWithdrawPayload(amount, recipient);
      assertPrivacySessionCurrent(session);
    } catch (error) {
      assertPrivacySessionCurrent(session);
      if (!isExactMatchWithdrawError(error)) {
        throw error;
      }
      showTransferPlannerFacts({
        requested: amount,
        action: `${coinText(amount)} exact note를 만들기 위해 self transaction을 요청합니다.`,
      });
      updateTransferFlow(
        "zero",
        "Self transaction 서명 대기",
        "Relay withdraw도 입력 금액과 정확히 같은 note가 필요합니다. 먼저 내 Veiled balance 안에서 exact note를 만듭니다.",
      );
      await createExactWithdrawNote(amount, {
        onPlanCheck: (step) => {
          updateTransferFlow(
            "zero",
            step === 1 ? "노트 확인 중" : "노트 재확인 중",
            "Relay withdraw에 필요한 exact note를 만들 수 있는 note 조합을 확인합니다.",
          );
        },
        onSelfMergeNeeded: (data) => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `두 note를 합쳐 ${data.prepared?.amount || data.plan?.nextAmount || "더 큰"} self note를 만듭니다.`,
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "요청 금액의 exact note를 만들기 위해 두 note를 먼저 합칩니다. 이 단계는 내 Veiled balance 안에서만 준비됩니다.",
          );
        },
        onZeroHelperNeeded: () => {
          showTransferPlannerFacts({
            requested: amount,
            action: `${zeroCoinText()} zero note를 만들어 exact note self transaction에 사용합니다.`,
          });
          updateTransferFlow(
            "zero",
            "Zero note 서명 대기",
            "exact note를 만들기 위한 보조 zero note가 필요합니다. 이 단계도 내 Veiled balance 안에서만 준비됩니다.",
          );
        },
        onFinalExactTransfer: (data) => {
          showTransferPlannerFacts({
            requested: amount,
            currentMax: plannerCurrentExactNoteMaxForWithdraw(data, amount),
            action: `${coinText(amount)} exact note를 만드는 마지막 self transaction을 요청합니다.`,
          });
          updateTransferFlow(
            "zero",
            "Self transaction 서명 대기",
            "입력 금액과 정확히 같은 note를 만들기 위해 self transaction을 요청합니다.",
          );
        },
      }, { session });
      assertPrivacySessionCurrent(session);
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. relay withdraw payload를 다시 준비합니다.",
      );
      data = await preparePrivacyRelayWithdrawPayload(amount, recipient);
      assertPrivacySessionCurrent(session);
    }

    updateTransferFlow(
      "transfer",
      "Payload 준비 완료",
      "relay withdraw payload가 준비되었습니다. Relayer 패널에서 내용을 확인할 수 있습니다.",
    );
    const installed = await setPreparedRelayWithdrawPayload(data, {
      amount,
      recipient,
      preparation,
      session,
    });
    assertPrivacySessionCurrent(session);
    if (!installed) {
      throw noBroadcastAttemptError(
        new Error("Relay withdraw preparation was invalidated before install"),
      );
    }
    els.keplrTxState.textContent = "Relay payload ready";
    renderKeplr();
    finishTransferFlow("Relay withdraw payload가 준비되었습니다");
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    els.keplrTxState.textContent = "Relay withdraw failed";
    finishTransferFlow(privacyOperationErrorMessage(error), false);
  } finally {
    endPrivacyValueAction(actionLock);
    if (isPrivacySessionCurrent(session)) {
      setBusy(els.relayWithdrawFromVeiled, false);
      renderKeplr();
    }
  }
}

async function relayPreparedWithdraw() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  if (relaySubmissionInFlight) return;
  const payload = state.keplr.relayWithdrawPayload;
  if (!payload) {
    toast("먼저 relay withdraw payload를 준비해줘.");
    return;
  }
  const expectedPayloadVersion = state.keplr.relayWithdrawPayloadVersion;
  const handoffBoundaryLock = beginRelayHandoffBoundary(
    "local_submit",
    session,
    expectedPayloadVersion,
  );
  if (!handoffBoundaryLock) return;
  const submissionLock = Object.freeze({
    generation: session.generation,
    payloadVersion: expectedPayloadVersion,
    payload,
  });
  relaySubmissionInFlight = true;
  relaySubmissionLock = submissionLock;
  setBusy(els.relayPreparedWithdraw, true);
  renderKeplr();
  try {
  const reservation = state.keplr.relayWithdrawReservation;
  const recipient = state.keplr.relayWithdrawPayloadRecipient;
  const relayIsCurrent = () =>
    isPrivacySessionCurrent(session) &&
    state.keplr.relayWithdrawPayloadVersion === expectedPayloadVersion &&
    state.keplr.relayWithdrawPayload === payload;
  const assertRelayIsCurrent = () => {
    if (relayIsCurrent()) return;
    if (!isPrivacySessionCurrent(session)) {
      throw privacySessionInvalidatedError();
    }
    const error = new Error(
      "Prepared relay withdraw payload changed while submission was in progress",
    );
    error.relayPayloadChanged = true;
    throw error;
  };
  let chainSnapshot;
  try {
    chainSnapshot = await withPrivacySessionGuard(
      session,
      () => latestRelayChainSnapshot(),
    );
    assertRelayIsCurrent();
  } catch (error) {
    if (error?.privacySessionInvalidated || error?.relayPayloadChanged) {
      throw error;
    }
    toast("Latest chain time is unavailable. Relay submission is blocked until it can be verified.");
    return;
  }
  const manualResolutionRequested =
    relayReservationStatus(
      state.keplr.relayWithdrawReservation,
      reservationRecordsByID(),
    ) === reservationStatuses.ManualReview;
  await reconcileExpiredRelayWithdrawPayloads(chainSnapshot, { session });
  assertRelayIsCurrent();
  if (manualResolutionRequested) {
    const resolved = await resolveExpiredRelayManualReview(
      currentPreparedRelayWithdrawSnapshot(),
      chainSnapshot,
      { session },
    );
    assertRelayIsCurrent();
    state.keplr.relayWithdrawReservation =
      resolved?.reservation || state.keplr.relayWithdrawReservation;
    const resolvedStatus = relayReservationStatus(
      state.keplr.relayWithdrawReservation,
    );
    if (resolvedStatus === reservationStatuses.ReplanRequired) {
      await clearPreparedRelayWithdrawPayload({
        clearPayloadHash: true,
        stashHandedOff: false,
        session,
      });
      // Clearing the finalized payload deliberately advances its generation.
      // It is no longer valid to require the pre-clear payload version; only
      // the captured privacy session may update the resulting UI.
      assertPrivacySessionCurrent(session);
    } else {
      await persistRelayWithdrawPayloadState({ session });
      assertRelayIsCurrent();
    }
    renderKeplr();
    toast(
      resolvedStatus === reservationStatuses.ReplanRequired
        ? "Expired relay reservation resolved. Prepare a fresh payload."
        : relayManualReviewRecoveryMessage(resolved),
    );
    return;
  }
  if (!canRelayPreparedWithdrawPayload(chainSnapshot.chainNowMs)) {
    toast(
      "Relay payload expiry or reservation status could not be verified. Refresh Notes to reconcile before retrying.",
    );
    return;
  }
  if (!serverFeature("relayer")) {
    toast(
      "Local relayer helper is disabled. Use loopback access or set CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1 for LAN testing.",
    );
    return;
  }

  els.keplrTxState.textContent = "Relayer broadcasting";
  // A same-origin local-relayer POST is not a relay-payload handoff. Recording
  // one here would mark the reservation as externally exposed, and the
  // reservation manager correctly refuses to advance it to BroadcastAttempting.
  // Reserve recordRelayHandoff for Copy/download/QR or another real payload
  // egress; this path records the durable broadcast attempt immediately before
  // the HTTP request instead.
  let relayDispatchMarked = false;
  let relaySubmissionRecorded = false;
  try {
    const relay = await withReservationHeartbeat(
      reservation,
      async ({ assertHeartbeatHealthy }) => {
        assertRelayIsCurrent();
        assertHeartbeatHealthy();
        const latestChainSnapshot = await withPrivacySessionGuard(
          session,
          () => latestRelayChainSnapshot(),
        );
        assertRelayIsCurrent();
        if (!canRelayPreparedWithdrawPayload(latestChainSnapshot.chainNowMs)) {
          await reconcileExpiredRelayWithdrawPayloads(latestChainSnapshot, { session });
          assertRelayIsCurrent();
          const error = new Error("Relay payload expiry or reservation status changed before broadcast");
          error.relayPayloadExpired = true;
          throw error;
        }
        await verifyRelayPayloadNullifierUnspentBeforeBroadcast(
          payload,
          reservation,
          state.keplr.relayWithdrawPreparedData,
          { session },
        );
        assertRelayIsCurrent();
        stopPreparedRelayReservationHeartbeat();
        assertHeartbeatHealthy();
        const attemptRecords = await markPreparedReservationBroadcastAttempting(
          { reservation },
          "relay_withdraw",
          { session },
        );
        assertRelayIsCurrent();
        // From this point the payload may reach the local relay, so a
        // subsequent ambiguous failure must retain the safety lock for
        // reconciliation rather than becoming a pre-dispatch retry.
        relayDispatchMarked = true;
        updateRelayWithdrawReservationRecords(attemptRecords, { session });
        await persistRelayWithdrawPayloadState({ session });
        assertRelayIsCurrent();
        renderKeplr();
        assertHeartbeatHealthy();
        return withPrivacySessionGuard(
          session,
          () => relayPreparedWithdrawPayload(payload, recipient),
        );
      },
      { session },
    );
    assertRelayIsCurrent();
    state.keplr.relayWithdrawHash = broadcastTxHash(relay);
    state.keplr.relayWithdrawHeight =
      relay.tx?.height || relay.receipt?.blockNumber || "";
    const relayerDisplayAddress =
      relay.relayerEvmAddress || relay.relayerAddress;
    state.keplr.relayWithdrawRelayer = relayerDisplayAddress
      ? `${relay.relayer || "relayer"} ${shorten(relayerDisplayAddress, 12, 10)}`
      : relay.relayer || "";
    state.keplr.relayWithdrawPayloadHash =
      relay.payloadHash || payload?.payload_hash || "";
    await recordSubmittedReservation(
      reservation,
      relay,
      "Relay withdraw",
      { session },
    );
    relaySubmissionRecorded = true;
    assertRelayIsCurrent();
    try {
      await clearPreparedRelayWithdrawPayload({
        clearPayloadHash: true,
        stashHandedOff: false,
        session,
      });
      // The successful clear invalidates the payload version by design. Keep
      // the session boundary, but do not classify that expected transition as
      // a stale payload and suppress the success/reconciliation path.
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (error?.privacySessionInvalidated) throw error;
      throw reservationReconciliationRequiredError(
        "Relay withdraw",
        relay,
        error,
      );
    }
    els.keplrTxState.textContent = "Relay withdraw included";
    renderKeplr();
    toast("Relay withdraw submitted");
    await withPrivacySessionGuard(
      session,
      () => refreshPrivacySurfaces({ balance: true, session }),
    );
    await withPrivacySessionGuard(session, () => refreshRelayerAccount());
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    if (!relayIsCurrent()) {
      // Preserve the session/payload-version boundary even when the failed
      // awaited operation did not itself use the session guard.
      assertRelayIsCurrent();
    }
    if (error?.relayPayloadExpired) {
      els.keplrTxState.textContent = "Relay payload is no longer submittable";
      toast(
        privacyOperationErrorMessage(
          error,
          "Relay payload is no longer submittable. Prepare a fresh payload.",
        ),
      );
      return;
    }
    if (error?.reservationReconciliationRequired) {
      els.keplrTxState.textContent =
        "Relay withdraw submitted; reservation reconciliation required";
      throw error;
    }
    if (relaySubmissionRecorded) {
      // The external transaction and its durable Submitted state are already
      // known. A later local refresh failure must never re-enter broadcast
      // failure bookkeeping or classify the cleared payload as stale.
      assertPrivacySessionCurrent(session);
      els.keplrTxState.textContent =
        "Relay withdraw included; reconciliation required";
      renderKeplr();
      showNotice({
        title: "Relay withdraw included; reconciliation required",
        message: submittedOperationReconciliationCopy("Relay withdraw", false),
        failed: true,
      });
      return;
    }
    const preparedData = state.keplr.relayWithdrawPreparedData || {
      reservation: state.keplr.relayWithdrawReservation,
    };
    const attemptSource = {
      ...error,
      data: error?.data,
      broadcast: error?.broadcast,
      txHash: error?.txHash || error?.data?.txHash || "",
      reservation: state.keplr.relayWithdrawReservation,
    };
    const attempt = broadcastAttemptMetadata(attemptSource);
    if (!relayDispatchMarked && !hasBroadcastAttemptMetadata(attempt)) {
      els.keplrTxState.textContent = "Relay validation failed before local dispatch";
      toast(
        privacyOperationErrorMessage(
          error,
          "Relay validation failed before local dispatch. Refresh Notes before retrying.",
        ),
      );
      return;
    }
    if (error?.reservationHeartbeatFailed && hasBroadcastAttemptMetadata(
      attempt,
    )) {
      await noteReservationBookkeeping(() =>
        markReservationBatchUnknown(
          reservation,
          error,
          attemptSource,
          { reconcile_reason: "reservation_lease_heartbeat_failed_after_relay" },
          { session },
        ).then((records) => updateRelayWithdrawReservationRecords(records, { session })),
      );
      await persistRelayWithdrawPayloadState({ session });
      assertRelayIsCurrent();
      els.keplrTxState.textContent =
        "Relay withdraw may be submitted; reservation reconciliation required";
      throw reservationReconciliationRequiredError("Relay withdraw", attemptSource, error);
    }
    const definiteEvmFailure = isDefiniteEvmReceiptFailure(error);
    const definiteCosmosFailure = isDefiniteCosmosTxFailure(error, attemptSource);
    if (definiteEvmFailure || definiteCosmosFailure) {
      const reconciled = await noteReservationBookkeeping(() =>
        definiteEvmFailure
          ? reconcileFailedEvmReservation(preparedData, error, attemptSource, reservationStatuses.ProofReady, { session })
          : reconcileFailedCosmosReservation(preparedData, error, attemptSource, { session }),
      );
      if (reconciled === undefined) {
        await noteReservationBookkeeping(() =>
          markReservationBatchUnknown(
            reservation,
            error,
            attemptSource,
            definiteCosmosFailure
              ? {
                  definite_execution_failure: "cosmos_tx_code_failed",
                  reconcile_reason: "cosmos_tx_code_failed_pending_nullifier_reconcile",
                }
              : {
                  definite_execution_failure: "evm_receipt_failed",
                  reconcile_reason: "evm_receipt_failed_pending_nullifier_reconcile",
                },
            { session },
          ).then((records) => updateRelayWithdrawReservationRecords(records, { session })),
        );
      } else {
        updateRelayWithdrawReservationRecords(reconciled, { session });
      }
    } else {
      await noteReservationBookkeeping(() =>
        (hasBroadcastAttemptMetadata(attempt)
          ? markReservationBatchUnknown(
              reservation,
              error,
              attemptSource,
              {},
              { session },
            )
          : markReservationBatchManualReview(
              reservation,
              error,
              "opaque_relay_broadcast_error_without_transaction_identity",
              {
                opaque_broadcast_error: true,
                no_broadcast_attempt: false,
                relay_handed_off: true,
              },
              { session },
            )
        ).then((records) => updateRelayWithdrawReservationRecords(records, { session })),
      );
    }
    await persistRelayWithdrawPayloadState({ session });
    assertRelayIsCurrent();
    els.keplrTxState.textContent = "Relay withdraw failed";
    toast(
      privacyOperationErrorMessage(
        error,
        "Relay withdraw failed. Refresh Notes to reconcile before retrying.",
      ),
    );
  }
  } finally {
    endRelayHandoffBoundary(handoffBoundaryLock);
    if (relaySubmissionLock === submissionLock) {
      relaySubmissionInFlight = false;
      relaySubmissionLock = null;
      if (isPrivacySessionCurrent(session)) {
        setBusy(els.relayPreparedWithdraw, false);
        renderKeplr();
      }
    }
  }
}

els.connectWallet.addEventListener("click", () =>
  connectWallet().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.connectKeplr.addEventListener("click", () =>
  connectKeplr().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.disconnectWallet.addEventListener("click", () =>
  disconnectWallet().catch((error) => toast(error.message)),
);
els.dappChainSelect.addEventListener("change", (event) =>
  selectDappChainProfile(event.target.value).catch((error) =>
    toast(error.message),
  ),
);
els.signSession.addEventListener("click", () =>
  signSession().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.copyWalletAccount.addEventListener("click", () =>
  copyWalletAccount().catch((error) => toast(error.message)),
);
els.fundKeplr.addEventListener("click", fundKeplr);
els.setupKeplrPrivacy.addEventListener("click", () =>
  setupKeplrPrivacy().catch((error) => toast(privacySetupErrorMessage(error))),
);
els.copyKeplrDisclosurePubKey.addEventListener("click", () =>
  copyKeplrDisclosurePubKey().catch((error) => toast(error.message)),
);
els.refreshWalletBalance.addEventListener("click", () =>
  refreshWalletBalance().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.refreshClairBalance.addEventListener("click", () =>
  refreshWalletBalance().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.scanKeplrNotes.addEventListener("click", () =>
  scanKeplrNotes().catch((error) => {
    // `refreshChainSafety` runs before scanKeplrNotes installs its local
    // catch/finally block. If a wallet or profile change wins during that
    // preflight, its session sentinel reaches this UI boundary directly.
    // Treat it as a no-op so the replacement session never receives a stale
    // sync-failure toast.
    if (!error?.privacySessionInvalidated) {
      toast(privacySyncErrorMessage(error));
    }
  }),
);
els.resetKeplrNotes.addEventListener("click", () =>
  resetAndRescanKeplrNotes().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(privacySyncErrorMessage(error));
  }),
);
els.myKeplrSpendableOnly.addEventListener("change", (event) => {
  state.keplr.showSpendableOnly = event.target.checked;
  renderMyKeplrNotes();
});
els.sendFromKeplr.addEventListener("click", sendFromKeplr);
els.depositFromKeplr.addEventListener("click", depositFromKeplr);
[
  els.keplrSendAmount,
  els.keplrSendRecipient,
  els.keplrDepositAmount,
  els.veiledTransferAmount,
  els.veiledWithdrawAmount,
  els.veiledWithdrawRecipient,
  els.relayWithdrawAmount,
  els.relayWithdrawRecipient,
].forEach((input) => {
  input.addEventListener("input", updateAmountActionButtons);
});
[els.relayWithdrawAmount, els.relayWithdrawRecipient].forEach((input) => {
  input.addEventListener("input", () => {
    const session = beginPrivacySessionOperation();
    discardAndClearPreparedRelayWithdrawPayload({ session })
      .catch((error) => {
        if (!error?.privacySessionInvalidated) {
          toast(
            privacyRecoveryErrorMessage(
              error,
              "Prepared relay payload could not be discarded safely. Refresh Notes to verify its reservation state before preparing another payload.",
            ),
          );
        }
      })
      .finally(() => {
        if (isPrivacySessionCurrent(session)) renderKeplr();
      });
  });
});
els.veiledDisclosureAdvanced.addEventListener(
  "change",
  renderTransferDisclosureAdvanced,
);
els.veiledDisclosureMode.addEventListener(
  "change",
  renderTransferDisclosureAdvanced,
);
els.openBatchTransfer.addEventListener("click", openBatchTransferEditor);
els.closeBatchTransfer.addEventListener("click", closeBatchTransferEditor);
els.addBatchTransferRecipient.addEventListener("click", () =>
  addBatchTransferRow(),
);
els.batchTransferSplit.addEventListener(
  "change",
  renderBatchTransferPreview,
);
els.prepareBatchTransfer.addEventListener(
  "click",
  transferBatchFromVeiled,
);
els.cancelBatchTransferConfirmation.addEventListener("click", () =>
  closeBatchTransferConfirmation(false),
);
els.confirmBatchTransferConfirmation.addEventListener("click", () =>
  closeBatchTransferConfirmation(true),
);
els.transferFromVeiled.addEventListener("click", transferFromVeiled);
els.withdrawFromVeiled.addEventListener("click", withdrawFromVeiled);
els.relayWithdrawFromVeiled.addEventListener("click", relayWithdrawFromVeiled);
els.copyRelayWithdrawPayload.addEventListener("click", () =>
  copyRelayWithdrawPayload().catch((error) => {
    if (!error?.privacySessionInvalidated && !error?.relayPayloadChanged) {
      toast(privacyRelayHandoffErrorMessage(error));
    }
  }),
);
els.relayPreparedWithdraw.addEventListener("click", () =>
  relayPreparedWithdraw().catch((error) => {
    if (!error?.privacySessionInvalidated && !error?.relayPayloadChanged) {
      toast(
        privacyOperationErrorMessage(
          error,
          "Relay withdraw may have been submitted. Refresh Notes to reconcile before retrying.",
        ),
      );
    }
  }),
);
els.refreshAll.addEventListener("click", () =>
  refreshHealth().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.refreshNotes.addEventListener("click", () =>
  refreshNotes().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.refreshEvents.addEventListener("click", () =>
  refreshEvents().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
if (els.previousEventsPage) {
  els.previousEventsPage.addEventListener("click", () =>
    refreshEvents({ page: state.privacyEvents.page - 1 }).catch((error) => {
      if (!error?.privacySessionInvalidated) toast(error.message);
    }),
  );
}
if (els.nextEventsPage) {
  els.nextEventsPage.addEventListener("click", () =>
    refreshEvents({ page: state.privacyEvents.page + 1 }).catch((error) => {
      if (!error?.privacySessionInvalidated) toast(error.message);
    }),
  );
}
els.decodeEventDisclosure.addEventListener("click", () =>
  decodeSelectedEventDisclosure().catch((error) => toast(error.message)),
);
if (els.refreshAuditorTransfers) {
  els.refreshAuditorTransfers.addEventListener("click", () =>
    // The panel's only Refresh control must restore both its public transfer
    // list and the local-admin scalar. A wallet/profile reset clears the
    // scalar deliberately; refreshing only the list previously left Decode
    // permanently disabled even though the local endpoint was healthy.
    Promise.all([
      refreshAuditorTransfers(),
      refreshAuditorTestScalar(),
    ]).catch((error) => toast(error.message)),
  );
}
if (els.previousAuditorPage) {
  els.previousAuditorPage.addEventListener("click", () =>
    refreshAuditorTransfers({ page: state.auditor.page - 1 }).catch((error) =>
      toast(error.message),
    ),
  );
}
if (els.nextAuditorPage) {
  els.nextAuditorPage.addEventListener("click", () =>
    refreshAuditorTransfers({ page: state.auditor.page + 1 }).catch((error) =>
      toast(error.message),
    ),
  );
}
if (els.decodeAuditorTransfer) {
  els.decodeAuditorTransfer.addEventListener("click", () =>
    decodeAuditorTransfer().catch((error) => toast(error.message)),
  );
}
els.reservationReviewAcknowledge.addEventListener("change", () => {
  if (!reservationReviewDialogState.explicitCancellation) return;
  els.confirmReservationReview.disabled =
    reservationReviewDialogState.running ||
    !els.reservationReviewAcknowledge.checked;
});
els.cancelReservationReview.addEventListener("click", closeReservationReviewDialog);
els.confirmReservationReview.addEventListener("click", async () => {
  const operationID = reservationReviewDialogState.operationID;
  const explicitCancellation =
    reservationReviewDialogState.explicitCancellation;
  const preparedBatchReservation =
    reservationReviewDialogState.preparedBatchReservation;
  const stalledBatchReservation =
    reservationReviewDialogState.stalledBatchReservation;
  if (
    !operationID ||
    reservationReviewDialogState.running ||
    (explicitCancellation && !els.reservationReviewAcknowledge.checked)
  ) {
    return;
  }
  const session = beginPrivacySessionOperation();
  reservationReviewDialogState.running = true;
  setBusy(els.confirmReservationReview, true);
  setBusy(els.cancelReservationReview, true);
  let resolved = false;
  try {
    if (stalledBatchReservation) {
      await moveUnresolvedBatchTransferArtifactToManualReview(
        stalledBatchReservation,
        { session },
      );
      await resolveGeneralManualReviewOperation(operationID, {
        session,
        allowExplicitUntrackedCancellation: true,
      });
      toast(
        "The stalled batch checkpoint was cancelled locally. The notes can be planned again.",
      );
    } else if (preparedBatchReservation) {
      await discardPreparedBatchTransferArtifact(preparedBatchReservation, {
        session,
      });
      toast(
        "The unsubmitted batch checkpoint was cancelled locally. The notes can be planned again.",
      );
    } else {
      await resolveGeneralManualReviewOperation(operationID, {
        session,
        allowExplicitUntrackedCancellation: explicitCancellation,
      });
    }
    resolved = true;
  } catch (error) {
    if (!error?.privacySessionInvalidated) {
      toast(manualReviewResolutionErrorMessage(error));
    }
  } finally {
    if (reservationReviewDialogState.operationID !== operationID) return;
    reservationReviewDialogState.running = false;
    setBusy(els.confirmReservationReview, false);
    setBusy(els.cancelReservationReview, false);
    if (resolved) {
      closeReservationReviewDialog();
    } else if (explicitCancellation) {
      els.confirmReservationReview.disabled =
        !els.reservationReviewAcknowledge.checked;
    }
  }
});
els.closeNoticeModal.addEventListener("click", closeNoticeModal);
els.cancelTransferFlow.addEventListener("click", cancelTransferFlow);
els.confirmTransferFlow.addEventListener("click", confirmTransferFlowStart);
els.noticeModal.addEventListener("click", (event) => {
  if (event.target === els.noticeModal) {
    closeNoticeModal();
  }
});
els.reservationReviewModal.addEventListener("click", (event) => {
  if (event.target === els.reservationReviewModal) {
    closeReservationReviewDialog();
  }
});
els.batchTransferConfirmationModal.addEventListener("click", (event) => {
  if (event.target === els.batchTransferConfirmationModal) {
    closeBatchTransferConfirmation(false);
  }
});
els.transferFlowModal.addEventListener("click", (event) => {
  if (event.target === els.transferFlowModal) {
    cancelTransferFlow();
  }
});
window.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (!els.reservationReviewModal.hidden) {
    closeReservationReviewDialog();
  } else if (!els.batchTransferConfirmationModal.hidden) {
    closeBatchTransferConfirmation(false);
  } else if (!els.transferFlowModal.hidden) {
    cancelTransferFlow();
  } else if (!els.noticeModal.hidden) {
    closeNoticeModal();
  }
});
els.accountSelect.addEventListener("change", (event) => {
  invalidateLocalAccountView();
  state.selectedAccount = event.target.value;
  refreshSelectedAccount().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  });
});

const injectedMetaMask = metaMaskProvider();
if (injectedMetaMask) {
  injectedMetaMask.on?.("accountsChanged", (accounts) => {
    if (
      state.activeWallet !== "metamask" &&
      walletConnectionSession?.wallet !== "metamask"
    ) {
      return;
    }
    resetWalletSession();
    renderWallet();
    renderKeplr();
    if (!accounts[0]) {
      return;
    }
    toast(
      "MetaMask account changed. Reconnect wallet to refresh privacy identity.",
    );
  });
  injectedMetaMask.on?.("chainChanged", (chainId) => {
    if (
      state.activeWallet !== "metamask" &&
      walletConnectionSession?.wallet !== "metamask"
    ) {
      return;
    }
    resetWalletSession();
    state.wallet.chainId = chainId;
    renderWallet();
    renderKeplr();
    toast(
      "MetaMask network changed. Reconnect wallet before preparing another privacy transaction.",
    );
  });
}

window.addEventListener("keplr_keystorechange", () => {
  if (
    state.activeWallet !== "keplr" &&
    walletConnectionSession?.wallet !== "keplr"
  ) {
    return;
  }
  resetWalletSession();
  renderWallet();
  renderKeplr();
});

renderWallet();
renderKeplr();
renderTransferDisclosureAdvanced();
setupAddressSuggestions();
refreshHealth().catch((error) => {
  if (!error?.privacySessionInvalidated) toast(error.message);
});
