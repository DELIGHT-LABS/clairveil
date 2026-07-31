import { createClairveilBrowserDappClient } from "clairveiljs/browser-dapp";
import { bech32AddressToEvm } from "clairveiljs/evm";
import {
  createBrowserReservationStore,
  createNoteReservationManager,
  isActiveReservationStatus,
  reservationHeartbeatIntervalMs,
  reservationStatuses,
} from "clairveiljs/reservation";
import { loadStaticDappConfig } from "./dapp-config.js";
import {
  clearLegacyBrowserReservationState,
  createEncryptedBrowserMetadataStore,
  createEncryptedBrowserMigrationMarkerStore,
  createEncryptedStateCodec,
  EncryptedIndexedDbNoteStore,
} from "./encrypted-browser-store.js";
import {
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
const healthBootstrapRequestTimeoutMs = 30_000;
const healthBootstrapResponseMaxBytes = 1 << 20;

function defaultMetaMaskState() {
  return {
    account: "",
    chainId: "",
    signatureHash: "",
  };
}

function defaultNoteScanCursor() {
  return {
    source: "scan_events",
    afterHeight: 0,
    afterSequence: 0,
    page: 1,
    nextPage: 1,
    nextSequence: 0,
    limit: 200,
    maxPages: completeNoteScanMaxPages,
    hasMore: false,
    latestHeight: 0,
    latestSequence: 0,
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
    legacyCleanupState: "",
    noteScanCursor: defaultNoteScanCursor(),
    showSpendableOnly: true,
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
    loading: false,
  };
}

function defaultPrivacyEventsState() {
  return {
    events: [],
    selectedTxHash: "",
    decoded: null,
    error: "",
    loadError: "",
    loading: false,
  };
}

const state = {
  config: null,
  chainProfiles: [],
  selectedChainProfileId: "",
  accounts: [],
  selectedAccount: "alice",
  addressBook: {
    shieldedByName: {},
    shieldedError: "",
    loadingShielded: false,
  },
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
let browserClient = null;
let browserClientKey = "";
let auditorSessionGeneration = 0;
let privacyEventDisclosureGeneration = 0;
let reservationStore = null;
let reservationStoreKey = "";
let relayPendingPayloadSequence = 0;
let relayWithdrawPayloadGeneration = 0;
let preparedRelayReservationHeartbeatTimer = null;
let preparedRelayReservationHeartbeatInFlight = null;
let preparedRelayReservationHeartbeatGeneration = 0;
let relayWithdrawPayloadCopyInFlight = false;
let relayWithdrawPayloadCopyLock = null;
let relaySubmissionInFlight = false;
let relaySubmissionLock = null;
let depositInFlight = false;
let privacyValueActionLock = null;
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
const legacyMigrationMarkerNamespacePrefix = "clairveil:privacy-upgrade:v2:";
const webClientConfigSchemaVersion = "clairveil-web-client-config-v1";
const reservationRecoveryGraceMs = 15 * 60 * 1000;
const unresolvedReservationManualReviewAgeMs = 24 * 60 * 60 * 1000;
const successfulTxNullifierConflictGraceMs = 2 * 60 * 1000;
let walletNoteStore = null;
let walletNoteStoreKey = "";
let noteScanQueue = Promise.resolve();
let pendingNoteScans = 0;
let relayMetadataStore = null;
let relayMetadataStoreKey = "";
let relayMetadataWrite = Promise.resolve();
let legacyMigrationMarkerStore = null;
let legacyMigrationMarkerStoreKey = "";
let chainSafetyExpiryTimer = null;
let privacySessionGeneration = 0;
const activeReservationHeartbeatStops = new Set();
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
  const explicitlyEnabledLocalServer =
    config?.serverBacked !== false && config?.localTestMode === true;
  const directLoopbackPage = isLoopbackHostname(pageLocation.hostname);
  if (
    directLoopbackPage ||
    (pageLocation.protocol === "http:" && explicitlyEnabledLocalServer)
  ) {
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

function assertKeplrChainInfoMatchesProfile(profile) {
  const chainInfo = profile.keplrChainInfo;
  if (!isPlainConfigObject(chainInfo)) {
    throw new Error(`Clairveil WebApp profile ${profile.id}.keplrChainInfo is invalid`);
  }
  assertConfigString(
    chainInfo.chainId,
    `profile ${profile.id}.keplrChainInfo.chainId`,
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
      active[field] !== undefined &&
      String(config[field]) !== String(active[field])
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
  return address
    .trim()
    .toLowerCase()
    .startsWith(`${shieldedPrefix().toLowerCase()}1`);
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
  { serverFeatures = state.config?.serverFeatures } = {},
) {
  const configured = profile?.proverUrl || state.config?.proverUrl || "";
  if (serverFeatures?.proverProxy === true && configured) {
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

function hasDepositProofProvider(profile = activeChainProfile()) {
  return Boolean(browserDepositProofUrl(profile)) || serverFeature("depositProof");
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
  stopPreparedRelayReservationHeartbeat();
  for (const stop of [...activeReservationHeartbeatStops]) {
    stop();
  }
  privacySessionGeneration += 1;
  relayWithdrawPayloadCopyInFlight = false;
  relayWithdrawPayloadCopyLock = null;
  relaySubmissionInFlight = false;
  relaySubmissionLock = null;
  depositInFlight = false;
  privacyValueActionLock = null;
  resetTransferFlowForPrivacySession();
}

// A privacy flow may make more than one wallet request while it creates a
// self-merge or exact-match note. Reserve the UI flow from the first click,
// rather than only after the confirmation modal or preparation begins.
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
    legacyCleanupState: "",
    noteScanCursor: defaultNoteScanCursor(),
    privacySetupFailed: true,
  });
  walletNoteStore = null;
  walletNoteStoreKey = "";
  relayMetadataStore = null;
  relayMetadataStoreKey = "";
  relayMetadataWrite = Promise.resolve();
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
      error: privacyOperationErrorMessage(error),
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

function profileSessionFingerprint(profile = activeChainProfile()) {
  if (!profile) return "";
  return JSON.stringify({
    id: profile.id || "",
    transport: profile.transport || "",
    wallet: profile.wallet || "",
    chainId: profile.chainId || "",
    rpc: browserRpcUrl(profile),
    rest: browserRestUrl(profile),
    restEndpoints: browserRestEndpoints(profile),
    proverUrl: browserProverUrl(profile),
    depositProofUrl: browserDepositProofUrl(profile),
    accountPrefix: profile.accountPrefix || "",
    shieldedPrefix: profile.shieldedPrefix || "",
    denom: profile.denom || "",
    evmRpc: evmRpcUrlForWallet(profile),
    evmChainId: profile.evmChainId || "",
    evmPrivacyPrecompileAddress: profile.evmPrivacyPrecompileAddress || "",
    keplrChainId: profile.keplrChainInfo?.chainId || "",
    keplrRpc: profile.keplrChainInfo
      ? browserEndpointUrl(profile.keplrChainInfo.rpc, { trim: true })
      : "",
    keplrRest: profile.keplrChainInfo
      ? browserEndpointUrl(profile.keplrChainInfo.rest, { trim: true })
      : "",
  });
}

function profilePersistenceScope(profile = activeChainProfile()) {
  const fields = [
    profile?.id || "configured",
    profile?.transport || "",
    profile?.chainId || "",
    profile?.accountPrefix || "",
    profile?.shieldedPrefix || "",
    profile?.denom || "",
    profile?.evmChainId || "",
    profile?.evmPrivacyPrecompileAddress || "",
  ];
  return fields.map((field) => encodeURIComponent(String(field))).join("~");
}

function clairveilBrowserClient(
  profile = activeChainProfile(),
  { config = state.config } = {},
) {
  const resolved = profile || configuredChainProfile();
  const key = JSON.stringify({
    id: resolved?.id || "",
    rpc: browserRpcUrl(resolved),
    rest: browserRestUrl(resolved),
    restEndpoints: browserRestEndpoints(resolved),
    chainId: resolved?.chainId || state.config?.chainId || "",
    accountPrefix: resolved?.accountPrefix || state.config?.accountPrefix || "",
    shieldedPrefix:
      resolved?.shieldedPrefix || state.config?.shieldedPrefix || "",
    denom: resolved?.denom || state.config?.denom || "",
    proverUrl: browserProverUrl(resolved, { serverFeatures: config?.serverFeatures }),
    evmRpc: evmRpcUrlForWallet(resolved),
    evmChainId: resolved?.evmChainId || state.config?.evmChainId || "",
    evmPrivacyPrecompileAddress:
      resolved?.evmPrivacyPrecompileAddress ||
      state.config?.evmPrivacyPrecompileAddress ||
      "",
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
        proverUrl: browserProverUrl(resolved, { serverFeatures: config?.serverFeatures }),
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
  chainSafetyExpiryTimer = globalThis.setTimeout(() => {
    chainSafetyExpiryTimer = null;
    if (!isChainSafetyReady()) renderKeplr();
  }, delayMs + 1);
}

function isChainSafetyReady() {
  return state.chainSafety.status === "ready" &&
    state.chainSafety.key === activeChainSafetyKey() &&
    Number.isFinite(state.chainSafety.checkedAt) &&
    Date.now() - state.chainSafety.checkedAt < chainSafetyRefreshIntervalMs;
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
  if (hasCompletedPrivacyNoteScan()) return;
  throw new Error(
    state.keplr.scanError ||
      "Privacy note sync is incomplete. Finish scanning notes successfully before preparing a spend.",
  );
}

function invalidatePrivacyScanState(error) {
  state.keplr.notes = [];
  state.keplr.notesSummary = "";
  state.keplr.noteReservationByNullifier = {};
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.keplr.scanError = `Privacy note sync failed: ${
    error?.message || String(error || "unknown scan error")
  }`;
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
        client.health(),
        client.assertTransferProtocolConfig(profile.denom),
        client.queryReserve(profile.denom),
      ]),
    );
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
    scheduleChainSafetyExpiry();
    renderKeplr();
    return state.chainSafety;
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
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
    await ensureMetaMaskChain();
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
      params: [{ chainId: expected }],
    });
  } catch (error) {
    const unknownChain =
      error?.code === 4902 ||
      /unknown|unrecognized|not added/i.test(error?.message || "");
    if (!unknownChain) {
      throw error;
    }
    await requestMetaMask({
      method: "wallet_addEthereumChain",
      params: [
        {
          chainId: expected,
          chainName: state.config?.evmChainName || "EVM Localnet",
          nativeCurrency: {
            name: displayDenom(),
            symbol: displayDenom(),
            decimals: coinDecimals(),
          },
          rpcUrls: [evmRpcUrlForWallet()],
        },
      ],
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
  resetRescanNotes: $("#resetRescanNotes"),
  clearLegacyPrivacyData: $("#clearLegacyPrivacyData"),
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
  veiledDisclosureAdvanced: $("#veiledDisclosureAdvanced"),
  veiledDisclosureOptions: $("#veiledDisclosureOptions"),
  veiledDisclosureMode: $("#veiledDisclosureMode"),
  veiledDisclosurePubKey: $("#veiledDisclosurePubKey"),
  veiledDisclosureAmount: $("#veiledDisclosureAmount"),
  veiledDisclosureFrom: $("#veiledDisclosureFrom"),
  veiledDisclosureTo: $("#veiledDisclosureTo"),
  transferFromVeiled: $("#transferFromVeiled"),
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
  decodeEventDisclosure: $("#decodeEventDisclosure"),
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
  transferPlannerFacts: $("#transferPlannerFacts"),
  transferPlannerRequested: $("#transferPlannerRequested"),
  transferPlannerCurrentMax: $("#transferPlannerCurrentMax"),
  transferPlannerAction: $("#transferPlannerAction"),
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

function renderTransferDisclosureAdvanced() {
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

function privacyOperationErrorMessage(error, fallback = "Privacy operation failed") {
  switch (String(error?.code || "")) {
    case "PROVER_TIMEOUT":
      return "Privacy proof service timed out. Retry with the same configured provider.";
    case "PROVER_UNAVAILABLE":
    case "unavailable":
      return "Privacy proof service is unavailable. Retry with the same configured provider.";
    case "PROVER_REJECTED":
    case "proof_failed":
      return "Privacy proof service rejected the request. Verify the provider configuration and retry.";
    case "invalid_request":
      return "Privacy proof request was rejected. Verify the provider configuration and retry.";
    default:
      return fallback;
  }
}

function privacyReservationErrorCode(error, fallback = "privacy_operation_failed") {
  switch (String(error?.code || "")) {
    case "PROVER_TIMEOUT":
      return "prover_timeout";
    case "PROVER_UNAVAILABLE":
    case "unavailable":
      return "prover_unavailable";
    case "PROVER_REJECTED":
    case "proof_failed":
    case "invalid_request":
      return "prover_rejected";
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
  ]);
  const safe = new Error(
    recognizedCode.has(code)
      ? privacyOperationErrorMessage(error)
      : "Privacy proof service failed. Verify the provider configuration and retry.",
  );
  safe.code = recognizedCode.has(code) ? code : "proof_failed";
  return safe;
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
  resetTransferPlannerFacts();
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

function openTransferFlowModal(kind = "transfer") {
  applyPrivacyFlowCopy(kind);
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

function finishTransferFlow(message, success = true) {
  const copy = transferFlowState.copy || privacyFlowCopies.transfer;
  transferFlowState.running = false;
  els.transferModalLead.textContent = success ? copy.doneLead : copy.failedLead;
  els.confirmTransferFlow.hidden = true;
  setTransferFlowStep(success ? "done" : "", success ? "성공" : "실패");
  els.transferFlowModal.classList.toggle("failed", !success);
  if (success) {
    els.transferSuccessTitle.textContent = message || copy.successTitle;
    els.transferSuccessCopy.textContent = copy.successCopy;
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
    timeoutMs = 0,
    signal: suppliedSignal,
    expectedResponseUrl = "",
    responseLabel = "DApp API response",
    maxResponseBytes = 0,
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
        "content-type": "application/json",
        ...(options.headers || {}),
      },
    });
    assertDirectApiResponse(response, expectedResponseUrl, responseLabel);
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
    throw new ApiError(
      {
        error: data?.error || response.statusText,
        ...(data && typeof data === "object" ? data : {}),
      },
      response.status,
    );
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) {
    const error = new Error(`DApp API ${path} returned an invalid JSON response`);
    error.apiInvalidJsonResponse = true;
    error.apiPath = String(path);
    error.apiResponseContentType = String(
      response.headers.get("content-type") || "",
    );
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
      await requestMetaMask({
        method: "eth_estimateGas",
        params: [tx],
      }),
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

function hasPersistedTypedScanCursor(cached) {
  const cursor = cached?.scanCursor;
  return Boolean(
    cursor &&
      typeof cursor === "object" &&
      (cursor.source === "scan_events" || cursor.scan_source === "scan_events"),
  );
}

function applyPersistedWalletNoteState(cached) {
  if (!cached || !hasPersistedTypedScanCursor(cached)) return false;
  const scanCursor = cached.scanCursor;
  applyNoteScanResult({
    notes: cached.notes || [],
    scanCursor,
    nextScanOptions: {
      scanSource: scanCursor.source || "scan_events",
      afterHeight: scanCursor.next_height ?? scanCursor.after_height ?? cached.lastScannedHeight ?? 0,
      afterSequence: scanCursor.next_sequence ?? scanCursor.after_sequence ?? cached.lastScannedSequence ?? 0,
      page: scanCursor.next_page ?? scanCursor.page ?? 1,
    },
  }, { reset: true });
  return true;
}

async function clearCurrentWalletNoteStore({ session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const store = currentWalletNoteStore({ optional: false });
  await store.clear();
  assertPrivacySessionCurrent(session);
  state.keplr.notes = [];
  state.keplr.notesSummary = "";
  state.keplr.noteReservationByNullifier = {};
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.keplr.scanError = "";
}

async function hydratePersistedWalletNotes({ session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const store = currentWalletNoteStore({ optional: false });
  const cached = await store.load();
  assertPrivacySessionCurrent(session);
  if (!hasPersistedTypedScanCursor(cached)) {
    await store.clear();
    assertPrivacySessionCurrent(session);
    return;
  }
  applyPersistedWalletNoteState(cached);
}

function currentLegacyMigrationMarkerStore() {
  if (!state.keplr.account || !state.keplr.rootSignatureBase64) return null;
  const key = `${legacyMigrationMarkerNamespacePrefix}${reservationNamespace()}`;
  if (
    legacyMigrationMarkerStore &&
    legacyMigrationMarkerStoreKey === key
  ) {
    return legacyMigrationMarkerStore;
  }
  legacyMigrationMarkerStore = createEncryptedBrowserMigrationMarkerStore({
    namespace: key,
    secretBase64: state.keplr.rootSignatureBase64,
  });
  legacyMigrationMarkerStoreKey = key;
  return legacyMigrationMarkerStore;
}

async function loadLegacyMigrationCleanupMarker({ session = null } = {}) {
  assertPrivacySessionCurrent(session);
  const store = currentLegacyMigrationMarkerStore();
  if (!store) return;
  const marker = await store.load();
  assertPrivacySessionCurrent(session);
  state.keplr.legacyCleanupState = marker?.legacyV1CleanedAt
    ? "Legacy 0.1 local data removed"
    : "";
}

function legacyLocalStorageKeys() {
  const scope = reservationNamespace();
  return [
    `clairveil:wallet-notes:v1:${scope}`,
    `clairveil:relay-withdraw-payloads:v1:${scope}`,
  ];
}

function browserStorageForLegacyCleanup(name) {
  try {
    const storage = globalThis[name];
    return storage && typeof storage.removeItem === "function" ? storage : null;
  } catch {
    return null;
  }
}

function removeLegacyBrowserStorageKeys(storage) {
  if (!storage) return false;
  for (const key of legacyLocalStorageKeys()) {
    try {
      storage.removeItem(key);
    } catch {
      // Do not mark cleanup complete when a known legacy store cannot be
      // cleared. The next attempt must remain available to the user.
      return false;
    }
  }
  return true;
}

async function clearLegacyPrivacyData() {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  if (!hasCompletedPrivacyNoteScan()) {
    throw new Error("Finish a complete note scan before removing legacy 0.1 local data");
  }
  const localStorage = browserStorageForLegacyCleanup("localStorage");
  const sessionStorage = browserStorageForLegacyCleanup("sessionStorage");
  if (!localStorage || !sessionStorage) {
    throw new Error("Browser storage is unavailable for legacy cleanup");
  }
  const markerStore = currentLegacyMigrationMarkerStore();
  if (!markerStore) {
    throw new Error("Privacy-session storage is unavailable for legacy cleanup");
  }
  const localStorageCleared = removeLegacyBrowserStorageKeys(localStorage);
  const sessionStorageCleared = removeLegacyBrowserStorageKeys(sessionStorage);
  if (!localStorageCleared || !sessionStorageCleared) {
    throw new Error("Legacy browser storage could not be removed; cleanup was not marked complete");
  }
  await withPrivacySessionGuard(session, () =>
    clearLegacyBrowserReservationState({
      namespace: reservationNamespace(),
    }),
  );
  await withPrivacySessionGuard(session, () =>
    markerStore.save({
      version: "clairveil-privacy-upgrade-marker-v1",
      legacyV1CleanedAt: new Date().toISOString(),
    }),
  );
  assertPrivacySessionCurrent(session);
  state.keplr.legacyCleanupState = "Legacy 0.1 local data removed";
  renderKeplr();
  toast("Legacy Clairveil 0.1 local data removed after the completed rescan");
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

function isSpendableNote(note) {
  return String(note?.status || "").toLowerCase() === "spendable";
}

function noteNullifier(note) {
  return String(note?.nullifier || note?.nullifier_hex || "")
    .trim()
    .toLowerCase();
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

function isAvailableSpendableNote(note) {
  return isSpendableNote(note) && !noteHasActiveReservation(note);
}

function isUnverifiedNote(note) {
  const status = String(note?.status || "").toLowerCase();
  const nullifierStatus = String(note?.nullifier_status ?? note?.nullifierStatus ?? "").toLowerCase();
  return status === "unverified" || nullifierStatus === "unknown" || nullifierStatus === "unverified";
}

function isZeroAmountNote(note) {
  return noteAmountValue(note) === 0n;
}

function isHelperNote(note) {
  return isSpendableNote(note) && isZeroAmountNote(note);
}

function noteStatusLabel(note) {
  if (noteHasActiveReservation(note)) return "Reserved";
  if (isUnverifiedNote(note)) return "Unverified";
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
      isSpendableNote(note) &&
      isZeroAmountNote(note) &&
      noteHasActiveReservation(note),
  ).length;
  const reservedCount = (notes || []).filter(
    (note) =>
      isSpendableNote(note) &&
      !isZeroAmountNote(note) &&
      noteHasActiveReservation(note),
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
  return `${total}${baseDenom()} / ${spendableValueNotes.length} Spendable${helperText}${reservedHelperText}${reservedText}`;
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

function noteScanRequestOptions({ reset = false, requireComplete = true } = {}) {
  const cursor = reset
    ? defaultNoteScanCursor()
    : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const hasMore = !reset && Boolean(cursor.hasMore);
  return {
    scanSource: String(cursor.source || "scan_events"),
    afterHeight: hasMore
      ? Number(cursor.afterHeight || 0)
      : Number(cursor.latestHeight || 0),
    afterSequence: hasMore
      ? Number(cursor.afterSequence || 0)
      : Number(cursor.latestSequence || 0),
    page: hasMore ? Number(cursor.nextPage || 1) : 1,
    limit: Number(cursor.limit || 200),
    maxPages: requireComplete
      ? completeNoteScanMaxPages
      : Number(cursor.maxPages || completeNoteScanMaxPages),
    eventTypes: ["deposit", "shielded_transfer"],
  };
}

function applyNoteScanResult(data, { reset = false } = {}) {
  const previous = reset
    ? defaultNoteScanCursor()
    : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const cursor = data?.scanCursor || data?.scan_cursor || {};
  const nextScanOptions = data?.nextScanOptions || data?.next_scan_options || {};
  const hasMore = Boolean(cursor.has_more ?? cursor.hasMore);
  const cursorAfterHeight = Number(
    cursor.after_height ?? cursor.afterHeight ?? previous.afterHeight ?? 0,
  );
  const cursorNextHeight = Number(
    cursor.next_height ??
      cursor.nextHeight ??
      cursor.after_height ??
      cursor.afterHeight ??
      previous.afterHeight ??
      0,
  );
  const authoritativeAfterHeight = Number(
    nextScanOptions.afterHeight ??
      nextScanOptions.after_height ??
      cursor.next_height ??
      cursor.nextHeight ??
      (hasMore ? cursorAfterHeight : cursorNextHeight) ??
      0,
  );
  const cursorAfterSequence = Number(
    cursor.after_sequence ??
      cursor.afterSequence ??
      previous.afterSequence ??
      0,
  );
  const nextSequence = Number(
    cursor.next_sequence ??
      cursor.nextSequence ??
      cursorAfterSequence ??
      previous.nextSequence ??
      0,
  );
  const authoritativeAfterSequence = Number(
    nextScanOptions.afterSequence ??
      nextScanOptions.after_sequence ??
      cursor.next_sequence ??
      cursor.nextSequence ??
      (hasMore ? cursorAfterSequence : nextSequence) ??
      0,
  );
  const sequenceCursor = Boolean(
    cursor.source === "scan_events" ||
      nextScanOptions.afterSequence != null ||
      nextScanOptions.after_sequence != null ||
      cursor.next_sequence != null ||
      cursor.nextSequence != null,
  );
  const completedLatestHeight = hasMore
    ? Number(previous.latestHeight || 0)
    : authoritativeAfterHeight;
  const completedLatestSequence = hasMore
    ? Number(previous.latestSequence || 0)
    : authoritativeAfterSequence;
  state.keplr.notes = mergeCachedNotes(
    reset ? [] : state.keplr.notes,
    data?.notes || [],
  );
  state.keplr.noteScanCursor = {
    source: String(
      cursor.source ||
        nextScanOptions.scanSource ||
        nextScanOptions.scan_source ||
        previous.source ||
        "scan_events",
    ),
    afterHeight: hasMore
      ? sequenceCursor
        ? authoritativeAfterHeight
        : cursorAfterHeight
      : completedLatestHeight,
    afterSequence: hasMore
      ? sequenceCursor
        ? authoritativeAfterSequence
        : cursorAfterSequence
      : completedLatestSequence,
    page: Number(cursor.page || previous.page || 1),
    nextPage: hasMore
      ? Number(
          nextScanOptions.page ??
            cursor.next_page ??
            cursor.nextPage ??
            Number(cursor.page || previous.page || 1) + 1,
        )
      : 1,
    nextSequence: hasMore ? authoritativeAfterSequence : 0,
    limit: Number(cursor.limit || previous.limit || 200),
    maxPages: completeNoteScanMaxPages,
    hasMore,
    latestHeight: completedLatestHeight,
    latestSequence: completedLatestSequence,
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
      const result = await client.checkNullifiers(chunk);
      for (const nullifier of chunk) {
        if (result instanceof Map && result.has(nullifier)) {
          const used = nullifierUsedFromResponse(result.get(nullifier));
          if (used !== null) statuses.set(nullifier, used);
        }
      }
    } catch {
      // Only this chunk falls back to individual checks below.
    }
  }

  const missing = nullifiers.filter((nullifier) => !statuses.has(nullifier));
  const concurrency = 8;
  for (let index = 0; index < missing.length; index += concurrency) {
    const chunk = missing.slice(index, index + concurrency);
    await Promise.all(chunk.map(async (nullifier) => {
      try {
        const result = await client.checkNullifier(nullifier);
        statuses.set(nullifier, nullifierUsedFromResponse(result));
      } catch {
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
  await persistedNoteStore.setNullifierStatuses(new Map(
    [...statuses.entries()].map(([nullifier, used]) => [
      nullifier,
      used === true ? "spent" : used === false ? "unspent" : "unknown",
    ]),
  ));
  assertPrivacySessionCurrent(session);
}

async function reconcileReservedNotesFromScan(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const manager = currentNoteReservationManager({ optional: true });
  const notes = state.keplr.notes;
  if (!manager || !notes.length) return;
  await withPrivacySessionGuard(
    session,
    () => manager.reconcileSpentNotes(notes),
  );
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

async function recoveredReservationTxOutcome(reservation = {}) {
  const isEvm = state.activeWallet === "metamask";
  const candidates = [...new Set([
    String(reservation.submitted_tx_hash || ""),
    isEvm ? "" : String(reservation.tx_bytes_hash || ""),
  ].filter(Boolean))];
  if (!candidates.length) return { checked: false, found: false, failed: false };
  try {
    if (isEvm) {
      const txHash = candidates[0];
      const receipt = await clairveilBrowserClient().evmJsonRpc(
        "eth_getTransactionReceipt",
        [`0x${txHash.replace(/^0x/i, "")}`],
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
      const tx = await clairveilBrowserClient().waitForTx(txHash, {
        attempts: 1,
        intervalMs: 0,
      });
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
  } catch {
    return { checked: false, found: false, failed: false };
  }
}

function successfulTxNullifierConflictIsMature(outcome, records) {
  const cursor = state.keplr.noteScanCursor || {};
  const scanHeight = Math.max(
    Number(cursor.latestHeight || 0),
    Number(cursor.afterHeight || 0),
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
    const nullifiers = records
      .map((record) => nullifierByReservationID.get(record.reservation_id))
      .filter(Boolean);
    if (nullifiers.length !== records.length) continue;
    const spent = await withPrivacySessionGuard(
      session,
      () => Promise.all(nullifiers.map(checkNullifierSpent)),
    );
    if (spent.some((value) => value == null || value)) continue;

    const ids = records.map((record) => record.reservation_id);
    const first = records[0];
    const localWorkerState = records.every((record) =>
      [
        reservationStatuses.Reserved,
        reservationStatuses.Proving,
        reservationStatuses.ProofReady,
      ].includes(record.status),
    );
    const localPreBroadcast = localWorkerState && records.every(
      (record) =>
        !reservationHasBroadcastEvidence(record),
    );
    const workerExpired = records.every((record) =>
      reservationCanRecoverAfterWorkerExpiry(record, manager),
    );
    const hasProofReady = records.some(
      (record) => record.status === reservationStatuses.ProofReady,
    );
    if (canReplanExpiredLocalReservation({
      localPreBroadcast,
      workerExpired,
      hasProofReady,
    })) {
      try {
        updated.push(
          ...(await withPrivacySessionGuard(session, () => manager.markReplanRequired(ids, {
            error: "local prepared transaction was lost before broadcast",
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
            error: "prepared transaction has no durable pre-broadcast outcome",
            metadata: {
              reconcile_reason:
                "recovered_proof_ready_without_durable_pre_broadcast_evidence",
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
      () => recoveredReservationTxOutcome(first),
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
              error: "transaction could not be found before recovery deadline",
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
                ? "indexed transaction did not include a valid Cosmos result code"
                : "transaction succeeded but input nullifiers remain unspent",
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
          error: "recovered transaction failed and nullifiers remain unspent",
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
    const nullifiers = operationReservations
      .map((reservation) => nullifierByReservationID.get(reservation.reservation_id))
      .filter(Boolean);
    if (nullifiers.length !== operationReservations.length) continue;
    const spentResults = await withPrivacySessionGuard(
      session,
      () => Promise.all(nullifiers.map(checkNullifierSpent)),
    );
    if (spentResults.some((spent) => spent == null || spent)) continue;
    const first = operationReservations[0];
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
            txHashChecked: first.submitted_tx_hash || first.tx_bytes_hash || true,
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
    if (isActiveReservationStatus(reservation?.status)) {
      byNullifier[nullifier] = reservation;
    }
  }
  state.keplr.noteReservationByNullifier = byNullifier;
  refreshNotesSummary();
}

async function resolveGeneralManualReviewOperation(operationID) {
  const session = beginPrivacySessionOperation();
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
  const spent = await withPrivacySessionGuard(
    session,
    () => Promise.all(nullifiers.map(checkNullifierSpent)),
  );
  if (spent.some((value) => value !== false)) {
    throw new Error(
      "Every nullifier must be explicitly confirmed unspent before re-planning",
    );
  }

  const first = records[0];
  const noBroadcast = records.every(reservationHasDurableNoBroadcastEvidence);
  let resolutionReason = "operator confirmed no broadcast and unspent nullifiers";
  if (!noBroadcast) {
    const outcome = await withPrivacySessionGuard(
      session,
      () => recoveredReservationTxOutcome(first),
    );
    const previouslyAgedOut = records.every(
      (record) =>
        record.metadata?.reconcile_reason ===
        "recovered_tx_not_found_manual_review",
    );
    if (
      !outcome.checked ||
      outcome.succeeded ||
      outcome.ambiguous ||
      (outcome.found && !outcome.failed) ||
      (!outcome.found && !previouslyAgedOut)
    ) {
      throw new Error(
        "Transaction absence or failure is not confirmed; keep this operation in ManualReview",
      );
    }
    resolutionReason = outcome.failed
      ? "operator confirmed failed transaction and unspent nullifiers"
      : "operator reconfirmed aged-out transaction absence and unspent nullifiers";
  }

  await withPrivacySessionGuard(
    session,
    () => manager.resolveManualReview(
      records.map((record) => record.reservation_id),
      {
        target: reservationStatuses.ReplanRequired,
        operatorId,
        approvalReference: `dapp-review:${operationID}:${Date.now()}`,
        reason: resolutionReason,
      },
    ),
  );
  await refreshNoteReservationState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
  toast("Reservation review resolved. The notes can be planned again.");
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

function persistRelayWithdrawPayloadState(
  { session = beginPrivacySessionOperation() } = {},
) {
  assertPrivacySessionCurrent(session);
  const storage = currentRelayWithdrawMetadataStore();
  if (!storage) return Promise.resolve();
  const currentSnapshot = currentPreparedRelayWithdrawSnapshot();
  const current = currentSnapshot
    ? sanitizeRelayWithdrawSnapshot(currentSnapshot)
    : null;
  const pending = (state.keplr.relayWithdrawPendingPayloads || [])
    .map((snapshot) => sanitizeRelayWithdrawSnapshot(snapshot))
    .filter(Boolean);
  relayMetadataWrite = relayMetadataWrite
    .catch(() => undefined)
    .then(async () => {
      if (!isPrivacySessionCurrent(session)) return;
      if (!current && !pending.length) {
        await storage.clear();
        return;
      }
      await storage.save({
        current,
        pending,
        savedAt: new Date().toISOString(),
      });
    })
    .catch(() => {
      // Do not send the original error object to the browser console. A
      // product may forward console events to telemetry, and operation errors
      // can carry privacy-sensitive payload or proof context.
      console.warn("clairveil_relay_metadata_persistence_failed");
    });
  return relayMetadataWrite;
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
  const hasProofReady = records.some(
    (record) => record.status === reservationStatuses.ProofReady,
  );
  const durableNoBroadcast = records.every(
    reservationHasDurableNoBroadcastEvidence,
  );
  const target = expiredRelayReservationRecoveryTarget({
    handedOff: recoverySnapshot.handedOff,
    localWorkerState: records.length > 0 && localWorkerState,
    localPreBroadcast,
    workerExpired,
    hasProofReady,
  });
  if (!target) return snapshot;
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
      ...snapshot,
      reservation: updateReservationBatchRecords(
        snapshot.reservation,
        updated || [],
      ),
    };
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    warnReservationBookkeeping(error);
    return snapshot;
  }
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
    handedOff: Boolean(
      metadata.relay_handed_off ||
        metadata.relayHandedOff ||
        record.status === reservationStatuses.Submitted ||
        record.status === reservationStatuses.Unknown ||
        reservationHasBroadcastEvidence(record),
    ),
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
    snapshot.handedOff = records.some((record) => {
      const metadata = record.metadata || {};
      return Boolean(
        metadata.relay_handed_off ||
          metadata.relayHandedOff ||
          record.status === reservationStatuses.Submitted ||
          record.status === reservationStatuses.Unknown ||
          reservationHasBroadcastEvidence(record),
      );
    });
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

async function restorePendingRelayWithdrawPayload(id) {
  const session = beginPrivacySessionOperation();
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
  state.keplr.relayWithdrawPendingPayloads = pending.map((item) =>
    relayWithdrawPendingPayloadID(item) === id ? synced : item,
  );
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
  state.keplr.relayWithdrawPendingPayloads = pending.filter(
    (item) => relayWithdrawPendingPayloadID(item) !== id,
  );
  await discardAndClearPreparedRelayWithdrawPayload({ session });
  assertPrivacySessionCurrent(session);
  applyPreparedRelayWithdrawSnapshot(synced);
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
}

async function refreshPendingRelayWithdrawPayloadStatus(id) {
  const session = beginPrivacySessionOperation();
  assertPrivacySessionCurrent(session);
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  const snapshot = pending.find(
    (item) => relayWithdrawPendingPayloadID(item) === id,
  );
  if (!snapshot) return;
  const synced = await syncRelayWithdrawSnapshotReservation(snapshot, { session });
  const status = relayReservationStatus(synced.reservation);
  const reconciled =
    status === reservationStatuses.ManualReview
      ? await resolveExpiredRelayManualReview(synced, null, { session })
      : await reconcileExpiredRelayWithdrawSnapshot(synced, null, { session });
  assertPrivacySessionCurrent(session);
  state.keplr.relayWithdrawPendingPayloads = pending
    .map((item) =>
      relayWithdrawPendingPayloadID(item) === id ? reconciled : item,
    )
    .filter(relaySnapshotNeedsPendingRecovery);
  await refreshNoteReservationState({ session });
  await persistRelayWithdrawPayloadState({ session });
  assertPrivacySessionCurrent(session);
  renderKeplr();
}

async function setPreparedRelayWithdrawPayload(
  data,
  { amount = "", recipient = "", preparation = null } = {},
) {
  if (preparation && !relayWithdrawPreparationIsCurrent(preparation)) {
    await rejectStaleRelayWithdrawPreparation(data);
  }
  stashHandedOffPreparedRelayWithdrawPayload();
  const installVersion = advanceRelayWithdrawPayloadGeneration();
  const installPreparation = preparation
    ? { ...preparation, version: installVersion }
    : null;
  await discardPreparedRelayWithdrawPayload(
    "local_relay_payload_overwritten_before_handoff",
  );
  if (
    state.keplr.relayWithdrawPayloadVersion !== installVersion ||
    (installPreparation && !relayWithdrawPreparationIsCurrent(installPreparation))
  ) {
    await rejectStaleRelayWithdrawPreparation(data);
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
  await renewReservationBatchLease(state.keplr.relayWithdrawReservation);
  if (state.keplr.relayWithdrawPayloadVersion !== installVersion) return false;
  startPreparedRelayReservationHeartbeat();
  await refreshNoteReservationState();
  if (state.keplr.relayWithdrawPayloadVersion !== installVersion) return false;
  persistRelayWithdrawPayloadState();
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
  clearPreparedRelayWithdrawPayload({
    ...clearOptions,
    advanceGeneration: false,
  });
  return true;
}

function clearPreparedRelayWithdrawPayload({
  clearPayloadHash = false,
  stashHandedOff = true,
  advanceGeneration = true,
} = {}) {
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
  persistRelayWithdrawPayloadState();
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
    const action = document.createElement("button");
    action.type = "button";
    if (item.payload) {
      action.textContent = "Use";
      action.addEventListener("click", () => {
        restorePendingRelayWithdrawPayload(id).catch((error) => {
          if (!error?.privacySessionInvalidated) toast(error.message);
        });
      });
    } else {
      action.textContent =
        relayReservationStatus(item.reservation) ===
        reservationStatuses.ManualReview
          ? "Resolve expired"
          : "Refresh status";
      action.addEventListener("click", () => {
        refreshPendingRelayWithdrawPayloadStatus(id).catch((error) => {
          if (!error?.privacySessionInvalidated) toast(error.message);
        });
      });
    }

    row.append(details, action);
    els.relayWithdrawPendingList.append(row);
  }
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
  els.relayWithdrawPreparedPayloadJson.textContent =
    state.keplr.relayWithdrawPayloadText || "No prepared payload";
  els.copyRelayWithdrawPayload.disabled =
    !hasRelayedPayload || relayWithdrawPayloadCopyInFlight;
  els.relayPreparedWithdraw.textContent = relayNeedsManualResolution
    ? "Resolve expired"
    : "Relay locally";
  els.relayPreparedWithdraw.disabled =
    (!relayPayloadReady && !relayNeedsManualResolution) || !relayerReady;
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
  if (state.activeWallet) {
    invalidatePrivacySessionOperations();
    await discardAndClearPreparedRelayWithdrawPayload();
    resetWalletSession();
  }
  state.selectedChainProfileId = profileId;
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

async function ensureLocalSignersIfNeeded(data) {
  if (
    !data.config?.serverFeatures?.localSignerSetup ||
    data.config?.transport !== "evm" ||
    (data.accounts || []).length
  ) {
    return data;
  }
  let ensured;
  try {
    ensured = await api("/api/local-signers/ensure", {
      method: "POST",
      body: JSON.stringify({}),
    });
  } catch (error) {
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

async function browserHealthFromStaticConfig(config) {
  assertValidatedDappConfig(config);
  assertBrowserDeploymentEndpointPolicy(config);
  const previousProfile = activeChainProfile();
  const profile = selectedProfileFromConfig(config) || firstConfigProfile(config);
  if (
    previousProfile &&
    profile &&
    profileSessionFingerprint(previousProfile) !== profileSessionFingerprint(profile) &&
    hasInMemoryPrivacySession()
  ) {
    invalidatePrivacySessionOperations();
  }
  const health = await clairveilBrowserClient(profile, { config }).health();
  return {
    config,
    status: health.status,
    tree: health.tree,
    audit: health.audit,
    accounts: [],
    errors: health.errors || [],
  };
}

async function loadDappHealth() {
  if (serverConfigAvailable) {
    try {
      const data = await ensureLocalSignersIfNeeded(await api("/api/health", {
        timeoutMs: healthBootstrapRequestTimeoutMs,
        maxResponseBytes: healthBootstrapResponseMaxBytes,
        expectedResponseUrl: "/api/health",
        responseLabel: "WebApp health response",
        redirect: "error",
      }));
      try {
        assertValidatedDappConfig(data.config);
        assertBrowserDeploymentEndpointPolicy(data.config);
      } catch (error) {
        error.dappConfigValidationFailure = true;
        throw error;
      }
      serverConfigAvailable = true;
      return data;
    } catch (error) {
      if (error?.dappConfigValidationFailure) {
        throw new Error(`WebApp configuration is invalid; sync is unavailable: ${error.message}`);
      }
      if (error?.statusCode && error.statusCode !== 404) {
        throw new Error(`WebApp server health is unavailable: ${error.message}`);
      }
      const staticHealthFallback =
        error?.apiInvalidJsonResponse === true &&
        error?.apiPath === "/api/health" &&
        /^text\/html(?:;|$)/i.test(error?.apiResponseContentType || "");
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
      serverConfigAvailable = false;
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
  return kind === "shielded"
    ? state.addressBook.shieldedByName[account.name] || ""
    : account.transparentAddress || "";
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
    state.addressBook.loadingShielded &&
    suggestions.length < accounts.length
  ) {
    appendAddressSuggestionEmpty(config, "Loading shielded addresses...");
  }

  if (
    config.kind === "shielded" &&
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

async function ensureShieldedAddressBook() {
  const missing = recipientTestAccounts().filter(
    (account) => !state.addressBook.shieldedByName[account.name],
  );
  if (!missing.length) return;
  if (shieldedAddressBookPromise) {
    await shieldedAddressBookPromise;
    return;
  }

  state.addressBook.loadingShielded = true;
  state.addressBook.shieldedError = "";
  renderVisibleAddressSuggestions();

  shieldedAddressBookPromise = Promise.allSettled(
    missing.map(async (account) => {
      const data = await api(`/api/wallet/${account.name}/show-address`);
      const address = data.address || "";
      if (address) {
        state.addressBook.shieldedByName[account.name] = address;
      }
    }),
  );

  const results = await shieldedAddressBookPromise;
  state.addressBook.loadingShielded = false;
  shieldedAddressBookPromise = null;
  if (results.some((result) => result.status === "rejected")) {
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
    ensureShieldedAddressBook().catch((error) => {
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
  setAuditorValueTone(auditorDetailValueElements());
  for (const element of auditorDetailValueElements()) {
    element.textContent = "-";
  }
  if (els.auditorDecodeState) {
    els.auditorDecodeState.textContent = "Local admin disclosure material cleared.";
  }
  if (els.decodeAuditorTransfer) els.decodeAuditorTransfer.disabled = true;
  if (els.refreshAuditorTransfers) {
    els.refreshAuditorTransfers.disabled = !serverFeature("auditorAdmin");
  }
}

function clearPrivacyOperationDrafts() {
  for (const input of [
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
  resetTransferPlannerFacts();
  hideAllAddressSuggestions();
}

function resetPrivacyEventSession() {
  privacyEventDisclosureGeneration += 1;
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
  if (els.decodeEventDisclosure) els.decodeEventDisclosure.disabled = true;
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
  relayMetadataWrite = Promise.resolve();
  legacyMigrationMarkerStore = null;
  legacyMigrationMarkerStoreKey = "";
  clearPrivacyOperationDrafts();
  resetPrivacyEventSession();
}

function resetWalletSession() {
  state.activeWallet = "";
  resetMetaMaskSession();
  resetKeplrSession();
  resetAuditorSession();
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
  els.fundKeplr.disabled = !serverFeature("faucet") || !signerReady;
  const noteScanBusy = pendingNoteScans > 0;
  els.setupKeplrPrivacy.disabled = noteScanBusy || !signerReady;
  els.copyKeplrDisclosurePubKey.disabled = !state.keplr.disclosurePubKeyHex;
  els.refreshWalletBalance.disabled = !connected;
  els.refreshClairBalance.disabled = !connected;
  els.scanKeplrNotes.disabled =
    noteScanBusy || !signerReady || !state.keplr.rootSignatureBase64;
  els.resetRescanNotes.disabled =
    noteScanBusy || !signerReady || !state.keplr.rootSignatureBase64;
  els.clearLegacyPrivacyData.disabled =
    !hasCompletedPrivacyNoteScan() ||
    state.keplr.legacyCleanupState === "Legacy 0.1 local data removed";
  els.clearLegacyPrivacyData.textContent = state.keplr.legacyCleanupState ||
    "Remove legacy 0.1 data";
  updateAmountActionButtons({ signerReady, veiledReady });
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
  const privacyActionBusy = isPrivacyValueActionInFlight();
  els.sendFromKeplr.disabled =
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
    state.keplr.privacySetupFailed ||
    !chainSafetyReady ||
    !hasPositiveUclairInput(els.keplrDepositAmount) ||
    !hasDepositProofProvider();
  els.transferFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady || !chainSafetyReady || !hasPositiveUclairInput(els.veiledTransferAmount);
  els.withdrawFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady ||
    !chainSafetyReady ||
    !hasPositiveUclairInput(els.veiledWithdrawAmount) ||
    !isSendRecipientForWallet(
      els.veiledWithdrawRecipient.value,
      state.activeWallet || activeWalletKind(),
    );
  els.relayWithdrawFromVeiled.disabled =
    privacyActionBusy ||
    !veiledReady ||
    !chainSafetyReady ||
    !hasPositiveUclairInput(els.relayWithdrawAmount) ||
    !isSendRecipientForWallet(
      els.relayWithdrawRecipient.value,
      state.activeWallet || activeWalletKind(),
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

  if (!notesSyncReady) {
    const empty = document.createElement("p");
    empty.className = "empty";
    empty.textContent = state.keplr.scanError || "Not scanned";
    els.myKeplrNotesList.append(empty);
    return;
  }

  const valueNotes = state.keplr.notes.filter(
    (note) => !isZeroAmountNote(note) && !isUnverifiedNote(note),
  );
  const notes = state.keplr.showSpendableOnly
    ? valueNotes.filter(
        (note) => isSpendableNote(note) || noteHasActiveReservation(note),
      )
    : valueNotes;

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

  for (const note of notes) {
    const row = document.createElement("article");
    row.className = "note-row";
    row.classList.toggle("helper-note", isHelperNote(note));
    row.innerHTML = `
      <strong>${note.amount}${baseDenom()}</strong>
      <span class="${noteStatusClass(note)}">${noteStatusLabel(note)}</span>
      <code>${shorten(note.nullifier, 12, 10)}</code>
    `;
    els.myKeplrNotesList.append(row);
  }
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
    const action = document.createElement("button");
    action.type = "button";
    action.textContent = "Resolve";
    action.addEventListener("click", () => {
      action.disabled = true;
      resolveGeneralManualReviewOperation(operationID)
        .catch((error) => {
          if (!error?.privacySessionInvalidated) toast(error.message);
        })
        .finally(() => {
          action.disabled = false;
        });
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

function hasInMemoryPrivacySession() {
  return Boolean(
    state.activeWallet ||
      state.keplr.rootSignatureBase64 ||
      state.keplr.relayWithdrawPayload ||
      state.keplr.relayWithdrawReservation,
  );
}

async function clearPrivacySessionForProfileChange(previousProfile, nextProfile) {
  if (
    !previousProfile ||
    !nextProfile ||
    profileSessionFingerprint(previousProfile) ===
      profileSessionFingerprint(nextProfile) ||
    !hasInMemoryPrivacySession()
  ) {
    return null;
  }

  invalidatePrivacySessionOperations();
  let cleanupError = null;
  try {
    await discardAndClearPreparedRelayWithdrawPayload({
      discardReason: "chain_profile_changed_before_reconnect",
    });
  } catch (error) {
    cleanupError = error;
  }
  resetWalletSession();
  return cleanupError;
}

async function renderHealth(data) {
  const previousProfile = activeChainProfile();
  const nextProfile = selectedProfileFromConfig(data.config);
  const profileChangeCleanupError = await clearPrivacySessionForProfileChange(
    previousProfile,
    nextProfile,
  );
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
  ensureShieldedAddressBook().catch((error) => {
    state.addressBook.loadingShielded = false;
    state.addressBook.shieldedError = error.message;
    shieldedAddressBookPromise = null;
    renderVisibleAddressSuggestions();
  });
  if (profileChangeCleanupError) {
    throw new Error(
      `The chain profile changed and the previous privacy session was cleared, but relay reservation cleanup needs reconciliation: ${profileChangeCleanupError.message}`,
    );
  }
}

async function refreshHealth() {
  const data = await loadDappHealth();
  await renderHealth(data);
  await refreshChainSafety({ force: true }).catch(() => {});
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
  const tasks = [refreshEvents({ allowFailure: true })];
  if (serverFeature("relayer")) {
    tasks.push(refreshRelayerAccount());
  }
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

  els.transparentAddress.textContent =
    transparentDisplayAddressFor(account) || "-";
  els.shieldedAddress.textContent = "Loading...";
  els.balanceValue.textContent = "Loading...";

  const [shielded, balance] = await Promise.all([
    api(`/api/wallet/${account.name}/show-address`),
    clairveilBrowserClient().getBalances(account.transparentAddress),
  ]);

  els.shieldedAddress.textContent = shielded.address || "-";
  els.balanceValue.textContent =
    (balance.balances || [])
      .map((coin) => `${coin.amount}${coin.denom}`)
      .join(", ") || zeroCoinText();

  await refreshNotes();
}

async function refreshRelayerAccount() {
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
    const balance = await clairveilBrowserClient().getBalances(
      relayer.transparentAddress,
    );
    state.relayer.balance = formatBalances(balance.balances);
  } catch (error) {
    state.relayer.balance = "";
    state.relayer.error = error.message;
  }
  renderRelayerPanel();
}

async function refreshWalletBalance() {
  if (!state.keplr.account) return;
  if (isEvmTransparentMode()) {
    if (!state.wallet.account) return;
    const balanceHex = await requestMetaMask({
      method: "eth_getBalance",
      params: [state.wallet.account, "latest"],
    });
    state.keplr.balance = formatBalances([
      {
        denom: baseDenom(),
        amount: BigInt(balanceHex || "0x0").toString(),
      },
    ]);
  } else {
    const data = await clairveilBrowserClient().getBalances(
      state.keplr.account,
    );
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
  const notes = (data.notes || []).filter((note) => !isUnverifiedNote(note));
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
      <span class="${noteStatusClass(note)}">${noteStatusLabel(note)}</span>
      <code>${shorten(note.nullifier, 12, 10)}</code>
    `;
    els.notesList.append(row);
  }
}

async function refreshEvents({ allowFailure = false } = {}) {
  const [privacyResult, blockResult] = await Promise.allSettled([
    clairveilBrowserClient().fetchPrivacyEvents(),
    clairveilBrowserClient().fetchBlockEvents(30),
  ]);

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
  state.privacyEvents.loadError = "";
  if (blockResult.status === "fulfilled") {
    state.blockEvents.events = blockResult.value.events || [];
    state.blockEvents.error = "";
  } else {
    state.blockEvents.events = [];
    state.blockEvents.error = browserDataLoadErrorMessage(blockResult.reason);
  }

  if (
    state.privacyEvents.selectedTxHash &&
    !state.privacyEvents.events.some(
      (event) => event.tx_hash_hex === state.privacyEvents.selectedTxHash,
    )
  ) {
    privacyEventDisclosureGeneration += 1;
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

function canDecodeUserEventDisclosure(event) {
  if (!event || event.event_type !== "shielded_transfer") return false;
  if (isPublicDisclosureEvent(event)) return true;
  return disclosureTargetMatches(event);
}

function canDecodeSelfViewDisclosure(event) {
  return Boolean(
    isCosmosProfile() &&
    hasSelfViewDisclosureEvent(event) &&
    state.keplr.account &&
    state.keplr.pubkeyHex &&
    state.keplr.rootSignatureBase64,
  );
}

function selectedEventDisclosurePlane(event) {
  if (!event || event.event_type !== "shielded_transfer") return "-";
  if (canDecodeUserEventDisclosure(event)) return "user";
  if (canDecodeSelfViewDisclosure(event)) return "self-view";
  if (eventAttribute(event, "user_disclosure_payload")) return "user";
  if (hasSelfViewDisclosureEvent(event)) return "self-view";
  return "-";
}

function canDecodeEventDisclosure(event) {
  return (
    canDecodeUserEventDisclosure(event) || canDecodeSelfViewDisclosure(event)
  );
}

function eventDisclosureStatus(event) {
  if (!event) return "Select an event.";
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
    const canSelect = event.event_type === "shielded_transfer";
    const row = document.createElement("button");
    row.type = "button";
    row.className = "event-row";
    row.classList.toggle(
      "selected",
      event.tx_hash_hex === state.privacyEvents.selectedTxHash,
    );
    row.disabled = !canSelect;
    if (canSelect) {
      row.addEventListener("click", () =>
        selectPrivacyEvent(event.tx_hash_hex),
      );
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
  privacyEventDisclosureGeneration += 1;
  state.privacyEvents.selectedTxHash = txHash;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  renderPrivacyEvents();
  renderEventDetail();
}

function selectedPrivacyEvent() {
  return state.privacyEvents.events.find(
    (event) => event.tx_hash_hex === state.privacyEvents.selectedTxHash,
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
  els.eventDetailType.textContent = event?.event_type || "-";
  els.eventDetailHeight.textContent = event?.height || "-";
  els.eventDetailTx.textContent = event?.tx_hash_hex || "-";
  els.eventDetailUserMode.textContent = event
    ? eventAttribute(event, "user_disclosure_mode") || "-"
    : "-";
  els.eventDetailTarget.textContent = event
    ? eventAttribute(event, "user_disclosure_target_pubkey") || "-"
    : "-";
  els.eventDisclosurePlane.textContent = selectedEventDisclosurePlane(event);
  clearEventDisclosureResult();
  if (state.privacyEvents.decoded) {
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

async function refreshAuditorTestScalar() {
  if (!hasAuditorUi() || !els.auditorTestScalar) return;
  const generation = auditorSessionGeneration;
  els.auditorTestScalar.textContent = "Loading...";
  updateAuditorDecodeButton();
  try {
    const data = await api("/api/auditor/test-scalar");
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
    state.auditor.testScalar = data.disclosure_private_scalar_hex || "";
    state.auditor.testScalarError = "";
    state.auditor.testScalarMatchesAuditConfig = Boolean(
      data.matches_audit_config,
    );
  } catch (error) {
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
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
  els.decodeAuditorTransfer.disabled =
    state.auditor.loading ||
    !state.auditor.selectedTxHash ||
    !/^[0-9a-fA-F]{1,64}$/.test(scalar);
}

async function decodeSelectedEventDisclosure() {
  const event = selectedPrivacyEvent();
  if (!event || !canDecodeEventDisclosure(event)) return;
  const eventTxHash = event.tx_hash_hex;
  const privacySession = privacySessionGeneration;
  const disclosureGeneration = privacyEventDisclosureGeneration + 1;
  privacyEventDisclosureGeneration = disclosureGeneration;
  state.privacyEvents.loading = true;
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
  els.eventDisclosureState.textContent = "Disclosure 조회 중...";
  renderEventDetail();
  try {
    const report = canDecodeUserEventDisclosure(event)
      ? await clairveilBrowserClient().decodeUserDisclosure(
          privacyRequest({ txHash: event.tx_hash_hex }),
        )
      : await clairveilBrowserClient().decodeSelfViewDisclosure(
          keplrPrivacyRequest({ txHash: event.tx_hash_hex }),
        );
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedTxHash !== eventTxHash
    ) {
      return;
    }
    state.privacyEvents.decoded = report;
    renderEventDisclosureReport(report);
  } catch (error) {
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedTxHash !== eventTxHash
    ) {
      return;
    }
    state.privacyEvents.error = error.message;
  } finally {
    if (
      privacySession !== privacySessionGeneration ||
      disclosureGeneration !== privacyEventDisclosureGeneration ||
      state.privacyEvents.selectedTxHash !== eventTxHash
    ) {
      return;
    }
    state.privacyEvents.loading = false;
    renderEventDetail();
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
  els.auditorFrom.textContent = payload ? shorten(payload, 14, 12) : "-";
  els.auditorFields.textContent = "encrypted";
  els.auditorTo.textContent = "decode UI deferred";
  setAuditorValueTone(
    [els.auditorTxHash, els.auditorAmount, els.auditorDigest, els.auditorFrom],
    "encoded",
  );
  els.auditorDecodeState.textContent =
    "Audit disclosure is present. Select Decode to use the local admin test scalar.";
  updateAuditorDecodeButton();
}

function renderAuditorReport(report) {
  if (!hasAuditorUi()) return;
  const summary = report?.summary || {};
  const payload = report?.payload || {};
  const verification = report?.verification || {};
  const disclosureVerified = verification.verified === true;
  const verified = disclosureVerified ? "Verified" : "Failed";
  const amount = summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";

  els.auditorTxHash.textContent =
    report?.tx_hash || state.auditor.selectedTxHash || "-";
  els.auditorVerification.textContent = verified;
  els.auditorDigest.textContent =
    payload.disclosure_digest_hex ||
    eventAttribute(
      state.auditor.events.find(
        (event) => event.tx_hash_hex === state.auditor.selectedTxHash,
      ),
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
  const events = [...state.auditor.events].reverse().slice(0, 20);

  for (const event of events) {
    const row = document.createElement("button");
    row.type = "button";
    row.className = "audit-row";
    row.classList.toggle(
      "selected",
      event.tx_hash_hex === state.auditor.selectedTxHash,
    );
    row.disabled = state.auditor.loading;
    row.addEventListener("click", () =>
      selectAuditorTransfer(event.tx_hash_hex),
    );

    const copy = document.createElement("div");
    copy.className = "row-copy";
    const title = document.createElement("strong");
    title.textContent = shorten(event.tx_hash_hex, 14, 12);
    const meta = document.createElement("span");
    meta.textContent = `height ${event.height}`;
    const digest = document.createElement("code");
    digest.textContent = shorten(
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
}

async function refreshAuditorTransfers() {
  if (!hasAuditorUi()) return;
  const generation = auditorSessionGeneration;
  setBusy(els.refreshAuditorTransfers, true);
  try {
    const data = await clairveilBrowserClient().fetchAuditableTransfers();
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
    state.auditor.events = data.events || [];
    if (
      state.auditor.selectedTxHash &&
      !state.auditor.events.some(
        (event) => event.tx_hash_hex === state.auditor.selectedTxHash,
      )
    ) {
      state.auditor.selectedTxHash = "";
      state.auditor.decoded = null;
      clearAuditorReport();
    }
    renderAuditorTransfers();
    renderAuditorEventDetail(
      state.auditor.events.find(
        (event) => event.tx_hash_hex === state.auditor.selectedTxHash,
      ),
    );
  } finally {
    if (generation === auditorSessionGeneration && hasAuditorUi()) {
      setBusy(els.refreshAuditorTransfers, false);
    }
  }
}

function selectAuditorTransfer(txHash) {
  if (!hasAuditorUi()) return;
  state.auditor.selectedTxHash = txHash;
  state.auditor.decoded = null;
  renderAuditorTransfers();
  renderAuditorEventDetail(
    state.auditor.events.find((event) => event.tx_hash_hex === txHash),
  );
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

  const generation = auditorSessionGeneration;
  state.auditor.selectedTxHash = txHash;
  state.auditor.loading = true;
  state.auditor.decoded = null;
  clearAuditorReport("Decoding audit disclosure with injected scalar...");
  renderAuditorTransfers();

  try {
    const report = await api("/api/auditor/decode", {
      method: "POST",
      body: JSON.stringify({ txHash, disclosurePrivKeyHex }),
    });
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
    state.auditor.decoded = report;
    renderAuditorReport(report);
  } catch (error) {
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
    clearAuditorReport(error.message);
  } finally {
    if (generation !== auditorSessionGeneration || !hasAuditorUi()) return;
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
    toast(
      "Selected chain is not running in this DApp server. Restart the server for that chain profile.",
    );
    return;
  }
  if (!metaMaskProvider()) {
    toast("MetaMask not found");
    return;
  }
  invalidatePrivacySessionOperations();
  await ensureMetaMaskChain();
  const accounts = await requestMetaMask({ method: "eth_requestAccounts" });
  const account = accounts[0] || "";
  if (!account) {
    await discardAndClearPreparedRelayWithdrawPayload();
    resetWalletSession();
    renderWallet();
    renderKeplr();
    return;
  }
  await ensureMetaMaskChain();
  await discardAndClearPreparedRelayWithdrawPayload();
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
  if (!els.veiledWithdrawRecipient.value && identity.evmAddress) {
    els.veiledWithdrawRecipient.value = identity.evmAddress;
  }
  if (!els.relayWithdrawRecipient.value && identity.evmAddress) {
    els.relayWithdrawRecipient.value = identity.evmAddress;
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
    `Time: ${new Date().toISOString()}`,
  ].join("\n");
  const signature = await requestMetaMask({
    method: "personal_sign",
    params: [message, account],
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
  await window.keplr.experimentalSuggestChain(chainInfo);
  await window.keplr.enable(chainInfo.chainId);
  const key = await window.keplr.getKey(chainInfo.chainId);
  const signer = await resolveKeplrSigner(chainInfo.chainId, key);

  invalidatePrivacySessionOperations();
  await discardAndClearPreparedRelayWithdrawPayload();
  // The wallet may now resolve a different Keplr account. Drop every
  // account-scoped cache and storage handle before loading its new namespace.
  resetKeplrSession();
  resetMetaMaskSession();
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
    `Time: ${new Date().toISOString()}`,
  ].join("\n");
  const signature = await window.keplr.signArbitrary(
    chainInfo.chainId,
    state.keplr.account,
    message,
  );
  state.keplr.signatureHash = await digestText(signature.signature);
  if (typeof window.keplr.verifyArbitrary === "function") {
    state.keplr.verified = await window.keplr.verifyArbitrary(
      chainInfo.chainId,
      state.keplr.account,
      message,
      signature,
    );
  }
  renderKeplr();
  toast("Keplr session signed");
}

async function disconnectWallet() {
  invalidatePrivacySessionOperations();
  await discardAndClearPreparedRelayWithdrawPayload();
  resetWalletSession();
  renderWallet();
  renderKeplr();
  toast("Wallet disconnected");
}

async function fundKeplr() {
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
  setBusy(els.fundKeplr, true);
  try {
    const data = await api("/api/faucet", {
      method: "POST",
      body: JSON.stringify({
        from: localSigner,
        recipient,
        amount,
      }),
    });
    state.keplr.faucetHash = data.broadcast?.txhash || "";
    state.keplr.faucetSent = formatUclairAsClair(
      data.amount?.funded?.replace(baseDenom(), "") || "0",
    );
    state.keplr.faucetRecipient = isEvmTransparentMode()
      ? data.recipientEvm || recipient
      : data.recipient || recipient;
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

async function setupKeplrPrivacy({ scanNotes = true } = {}) {
  if (!state.keplr.account) return false;
  const session = beginPrivacySessionOperation();
  if (
    state.keplr.rootSignatureBase64 &&
    state.keplr.shieldedAddress &&
    state.keplr.disclosurePubKeyHex
  ) {
    try {
      await loadPersistedRelayWithdrawPayloadState({ session });
      assertPrivacySessionCurrent(session);
    } catch (error) {
      if (error?.privacySessionInvalidated) return false;
      invalidateFailedPrivacySetup();
      els.keplrTxState.textContent = "Setup failed";
      toast(error.message);
      renderKeplr();
      return false;
    }
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
      await ensureMetaMaskChain();
      assertPrivacySessionCurrent(session);
      const rootMessage = clairveilBrowserClient().buildRootSigningMessage(
        address,
        pubKeyHex,
      );
      const signatureHex = await requestMetaMask({
        method: "personal_sign",
        params: [rootMessage, state.wallet.account],
      });
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
      const signature = await window.keplr.signArbitrary(
        chainInfo.chainId,
        address,
        rootMessage,
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
    await hydratePersistedWalletNotes({ session });
    await loadLegacyMigrationCleanupMarker({ session });
    await loadPersistedRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    await refreshChainSafety({ force: true });
    assertPrivacySessionCurrent(session);
    if (scanNotes) {
      await scanKeplrNotes({
        quiet: true,
        skipPrivacySetup: true,
        throwOnError: true,
        session,
      });
    }
    assertPrivacySessionCurrent(session);
    state.keplr.privacySetupFailed = false;
    els.keplrTxState.textContent = "Ready";
    renderKeplr();
    toast("Clairveil account ready");
    return true;
  } catch (error) {
    if (error?.privacySessionInvalidated) return false;
    invalidateFailedPrivacySetup();
    els.keplrTxState.textContent = "Setup failed";
    toast(error.message);
    return false;
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
    if (
      !handoffRecorded &&
      !canRelaySnapshotBeSubmitted(
        copySnapshot,
        reservationRecordsByID(),
        reservationStatuses,
        chainSnapshot.chainNowMs,
      )
    ) {
      throw new Error("Relay payload reservation is not ready for handoff");
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
    clipboardAttempted = true;
    await withPrivacySessionGuard(
      session,
      () => navigator.clipboard.writeText(copySnapshot.payloadText),
    );
    assertCopyIsCurrent();
  } catch (error) {
    if (!copyIsCurrent()) {
      throw error;
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
        ),
      );
    },
  };
}

async function signDirectAndBroadcast(
  signDoc,
  {
    reservation = null,
    session = beginPrivacySessionOperation(),
    onBroadcastStart = null,
  } = {},
) {
  assertPrivacySessionCurrent(session);
  const hasReservation = reservationIDs(reservation).length > 0;
  const reservationManagerForBroadcast = hasReservation
    ? currentNoteReservationManager()
    : null;
  const submission = {
    wallet: keplrDirectSignWallet({ session }),
    signDoc,
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
  onBroadcastStart?.();
  assertPrivacySessionCurrent(session);
  const broadcast = await withPrivacySessionGuard(
    session,
    () => clairveilBrowserClient().broadcastTxRawBytes(
      checkpoint.txRawBytes,
      hasReservation
        ? {
            reservationManager: reservationManagerForBroadcast,
            reservation,
          }
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
  if (!metaMaskProvider() || !account) {
    throw noBroadcastAttemptError(new Error("MetaMask is not connected"));
  }
  try {
    await withPrivacySessionGuard(session, () => ensureMetaMaskChain());
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
  onBroadcastStart?.();
  assertPrivacySessionCurrent(session);
  try {
    const txHash = await withPrivacySessionGuard(
      session,
      () => requestMetaMask({
        method: "eth_sendTransaction",
        params: [tx],
      }),
    );
    const normalized = normalizeEvmTxHash(txHash);
    if (!/^[0-9A-F]{64}$/.test(normalized)) {
      throw new Error("MetaMask eth_sendTransaction did not return a tx hash");
    }
    return normalized;
  } catch (error) {
    if (isMetaMaskUserRejectedError(error)) {
      throw noBroadcastAttemptError(error);
    }
    throw error;
  }
}

async function waitForEvmTransaction(txHash, label = "EVM transaction") {
  const broadcast =
    await clairveilBrowserClient().waitForEvmTransaction(txHash);
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
    const broadcast = await waitForEvmTransaction(txHash, label);
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  }
  const waitPromise = waitForEvmTransaction(txHash, label);
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
  const localDepositProofAvailable = serverFeature("depositProof");
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
      return await withPrivacySessionGuard(
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
    } catch (error) {
      throw safeDepositProofProviderError(error);
    }
  };
  return finishPrivacyPreparation(
    await clairveilBrowserClient().prepareDeposit(request),
    session,
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
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return finishPrivacyPreparation(await clairveilBrowserClient().prepareTransfer(
    privacyRequest({
      amount,
      recipient,
      scan: { limit: 200, maxPages: 1000 },
      reservationManager,
      ...disclosure,
      allowPlanStep: Boolean(options.allowPlanStep),
    }),
  ), session);
}

async function preparePrivacyWithdrawSignDoc(amount, recipient) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  assertSpendableNotesSyncReady();
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return finishPrivacyPreparation(await clairveilBrowserClient().prepareWithdraw(
    privacyRequest({
      amount,
      recipient,
      scan: { limit: 200, maxPages: 1000 },
      reservationManager,
    }),
  ), session);
}

async function preparePrivacyRelayWithdrawPayload(amount, recipient) {
  const session = beginPrivacySessionOperation();
  await assertChainSafetyBeforePrivacyFlow({ session });
  assertPrivacySessionCurrent(session);
  assertSpendableNotesSyncReady();
  const chainSnapshot = await latestRelayChainSnapshot();
  assertPrivacySessionCurrent(session);
  const reservationManager = currentNoteReservationManager();
  session.reservationManager = reservationManager;
  return finishPrivacyPreparation(await clairveilBrowserClient().prepareRelayWithdraw(
    privacyRequest({
      amount,
      recipient,
      chainNowUnix: Math.floor(chainSnapshot.chainNowMs / 1000),
      scan: { limit: 200, maxPages: 1000 },
      reservationManager,
    }),
  ), session);
}

async function relayPreparedWithdrawPayload(payload, recipient) {
  const relayer =
    localRelayerAccount()?.name ||
    (isEvmTransparentMode() ? "dev0" : "relayer");
  return api("/api/relayer/withdraw", {
    method: "POST",
    body: JSON.stringify({
      payload,
      expectedRecipient: recipient,
      relayer,
    }),
  });
}

async function broadcastPrivacyDeposit(
  amount,
  label = "deposit",
  options = {},
) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyDepositSignDoc(amount);
  state.keplr.shieldedAddress =
    data.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  els.keplrTxState.textContent =
    state.activeWallet === "metamask"
      ? "Waiting for MetaMask"
      : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, options);
  state.keplr.depositHash = broadcast.broadcast?.txhash || "";
  state.keplr.depositHash = state.keplr.depositHash || broadcast.txHash || "";
  state.keplr.depositHeight =
    broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
  return broadcast;
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
  persistRelayWithdrawPayloadState({ session });
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

function startPreparedRelayReservationHeartbeat() {
  stopPreparedRelayReservationHeartbeat();
  const session = beginPrivacySessionOperation();
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
  if (expiresAtMs <= Date.now()) return;
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
    try {
      await heartbeatNow();
    } catch (error) {
      error.broadcast = result;
      throw error;
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

async function reservationBatchIsIdempotent(manager, ids, status, attempt) {
  return Boolean(
    await idempotentReservationBatchRecords(manager, ids, status, attempt),
  );
}

async function idempotentReservationBatchRecords(manager, ids, status, attempt) {
  const records = await Promise.all(ids.map((id) => manager.getReservation(id)));
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
    if (
      !/expected ProofReady, got Submitted/.test(error?.message || "") ||
      !(await withPrivacySessionGuard(
        session,
        () => reservationBatchIsIdempotent(manager, ids, "Submitted", attempt),
      ))
    ) {
      throw error;
    }
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
    warnReservationBookkeeping(error);
    let fallbackError = null;
    try {
      await markReservationBatchUnknown(reservation, error, broadcast, {
        reconcile_reason: "submitted_write_failed_after_external_broadcast",
        no_broadcast_attempt: false,
      }, { session });
    } catch (unknownError) {
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
    const records = /expected (ProofReady|Submitted)/.test(
      transitionError?.message || "",
    )
      ? await withPrivacySessionGuard(
          session,
          () => idempotentReservationBatchRecords(
            manager,
            ids,
            "Unknown",
            attempt,
          ),
        )
      : null;
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
  const nullifiers = reservationNullifiersFromPrepared(data);
  if (!nullifiers.length) return;
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

async function checkNullifierSpent(nullifier) {
  try {
    const result = await clairveilBrowserClient().checkNullifier(nullifier);
    return nullifierUsedFromResponse(result);
  } catch {
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

async function relaySnapshotNullifierStatuses(snapshot) {
  const nullifiers = relaySnapshotNullifiers(snapshot);
  if (!nullifiers.length) return [];
  const statuses = await Promise.all(
    nullifiers.map(async (nullifier) => ({
      nullifier,
      spent: await checkNullifierSpent(nullifier),
    })),
  );
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
    () => relaySnapshotNullifierStatuses({
      payload,
      preparedData,
      reservation,
    }),
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
    () => relaySnapshotNullifierStatuses(recoverySnapshot),
  );
  if (!nullifierStatuses.length) return synced;
  if (nullifierStatuses.some((entry) => entry.spent == null)) return synced;
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

  const expiresAtUnix = relaySnapshotExpiresAtUnix(recoverySnapshot);
  const handedOff = Boolean(
    recoverySnapshot.handedOff ||
      recoverySnapshot.reservation.reservations.some(
        (record) => record.metadata?.relay_handed_off,
      ),
  );
  const updated = await markReservationBatchManualReview(
    recoverySnapshot.reservation,
    new Error("relay payload expired and nullifiers remain unspent"),
    "relay_payload_expired_requires_manual_review",
    {
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
    },
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
  const nullifierStatuses = await withPrivacySessionGuard(
    session,
    () => relaySnapshotNullifierStatuses(recoverySnapshot),
  );
  if (
    !nullifierStatuses.length ||
    nullifierStatuses.some((entry) => entry.spent !== false)
  ) {
    return snapshot;
  }
  const manager = currentNoteReservationManager({ optional: true });
  if (!manager?.resolveManualReview || !state.keplr.account) return snapshot;
  const payloadHash =
    recoverySnapshot.payloadHash || recoverySnapshot.payload?.payload_hash || "";
  const updated = await withPrivacySessionGuard(
    session,
    () => manager.resolveManualReview(
      reservationIDs(recoverySnapshot.reservation),
      {
        target: reservationStatuses.ReplanRequired,
        operatorId: state.keplr.account,
        approvalReference: `relay-expiry:${payloadHash || "unknown"}:${resolvedChainSnapshot.chainHeight || "unknown"}`,
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
  if (current) {
    const reconciled = await reconcileExpiredRelayWithdrawSnapshot(
      current,
      chainSnapshot,
      { session },
    );
    assertPrivacySessionCurrent(session);
    if (reconciled !== current) {
      state.keplr.relayWithdrawReservation =
        reconciled?.reservation || state.keplr.relayWithdrawReservation;
      changed = true;
    }
  }
  const pending = Array.isArray(state.keplr.relayWithdrawPendingPayloads)
    ? state.keplr.relayWithdrawPendingPayloads
    : [];
  if (pending.length) {
    const reconciledPending = [];
    for (const snapshot of pending) {
      const reconciled = await reconcileExpiredRelayWithdrawSnapshot(
        snapshot,
        chainSnapshot,
        { session },
      );
      assertPrivacySessionCurrent(session);
      if (relaySnapshotNeedsPendingRecovery(reconciled)) {
        reconciledPending.push(reconciled);
      }
    }
    state.keplr.relayWithdrawPendingPayloads = reconciledPending;
    changed = true;
  }
  if (changed) {
    await persistRelayWithdrawPayloadState({ session });
    assertPrivacySessionCurrent(session);
    renderKeplr();
  }
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
    () => Promise.all(nullifiers.map(checkNullifierSpent)),
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
    if (
      !/expected .*?, got ReplanRequired/.test(transitionError?.message || "")
    ) {
      throw transitionError;
    }
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
        await markPreparedReservationBroadcastAttempting(data, label, { session });
        durableBroadcastAttemptRecorded = true;
        assertHeartbeatHealthy();
      };
      const onBroadcastStart = () => {
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
          session,
          onBroadcastStart: () => {
            assertHeartbeatHealthy();
            assertPrivacySessionCurrent(session);
            externalBroadcastBoundaryCrossed = true;
          },
        });
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
      await noteReservationBookkeeping(() =>
        markPreparedReservationReplanRequired(data, error, undefined, {
          session,
        }),
      );
      throw error;
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
  els.keplrTxState.textContent =
    state.activeWallet === "metamask"
      ? "Waiting for MetaMask"
      : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label);
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

async function createExactWithdrawNote(amount, hooks = {}) {
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
    } catch (error) {
      if (!isZeroHelperNeededError(error)) {
        throw error;
      }
      hooks.onZeroHelperNeeded?.(error, step, maxPlannerSteps);
      await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", {
        waitForEvmReceipt: true,
      });
      await refreshPrivacySurfaces();
      continue;
    }

    if (
      data.prepared?.isFinal === false ||
      data.prepared?.planAction === "self_merge"
    ) {
      hooks.onSelfMergeNeeded?.(data, step, maxPlannerSteps);
      els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
      const plannerBroadcast = await broadcastPreparedPrivacy(
        data,
        "exact-note self transaction",
        { waitForEvmReceipt: true },
      );
      state.keplr.transferHash =
        plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
      await refreshPrivacySurfaces();
      continue;
    }

    hooks.onFinalExactTransfer?.(data, step, maxPlannerSteps);
    els.keplrTxState.textContent =
      state.activeWallet === "metamask"
        ? "Waiting for MetaMask"
        : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(
      data,
      "exact-note self transfer",
      { waitForEvmReceipt: true },
    );
    state.keplr.transferHash =
      broadcast.broadcast?.txhash || broadcast.txHash || "";
    await refreshPrivacySurfaces();
    return data;
  }

  throw new Error(
    "Withdraw에 필요한 exact note 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.",
  );
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
        amount: amountInputValue(els.keplrSendAmount),
      });
      els.keplrTxState.textContent = "Waiting for MetaMask";
      const broadcast = await sendEvmTransaction(transaction, {
        label: "EVM send",
      });
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
        onIncluded: async (included) => {
          state.keplr.sendHash = included.txHash || state.keplr.sendHash;
          els.keplrTxState.textContent = "Send included";
          await Promise.allSettled([
            refreshWalletBalance(),
            refreshBlockEvents(),
          ]);
          renderKeplr();
        },
        onFailed: (error) => {
          els.keplrTxState.textContent = "Send failed";
          showSendResult({ success: false, error: error.message });
        },
      });
      return;
    }

    const signDoc = await clairveilBrowserClient().buildBankSendSignDoc({
      from: state.keplr.account,
      pubKeyHex: state.keplr.pubkeyHex,
      to: recipient,
      amount: amountInputValue(els.keplrSendAmount),
    });
    els.keplrTxState.textContent = "Waiting for Keplr";
    const broadcast = await signDirectAndBroadcast(signDoc);
    state.keplr.sendHash = broadcast.broadcast?.txhash || "";
    els.keplrTxState.textContent = "Send included";
    renderKeplr();
    showSendResult({
      success: true,
      wallet: "Keplr",
      txHash: state.keplr.sendHash,
    });
    await Promise.allSettled([refreshWalletBalance(), refreshBlockEvents()]);
    renderKeplr();
  } catch (error) {
    els.keplrTxState.textContent = "Send failed";
    showSendResult({
      success: false,
      error: error.message,
    });
  } finally {
    setBusy(els.sendFromKeplr, false);
    renderKeplr();
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
  depositInFlight = true;
  setBusy(els.depositFromKeplr, true);
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
    els.keplrTxState.textContent = isPendingEvm
      ? "Deposit submitted"
      : "Deposit included";
    renderKeplr();
    showNotice({
      title: isPendingEvm ? "Deposit 요청됨" : "Deposit 성공",
      message: `${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit이 ${isPendingEvm ? "제출되었습니다" : "처리되었습니다"}.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`,
    });
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.depositHash = included.txHash || state.keplr.depositHash;
          state.keplr.depositHeight =
            included.receipt?.blockNumber || state.keplr.depositHeight;
          els.keplrTxState.textContent = "Deposit included";
          await withPrivacySessionGuard(
            session,
            () => refreshPrivacySurfaces({ balance: true }),
          );
          assertPrivacySessionCurrent(session);
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
    await withPrivacySessionGuard(
      session,
      () => refreshPrivacySurfaces({ balance: true }),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    els.keplrTxState.textContent = "Deposit failed";
    showNotice({
      title: "Deposit 실패",
      message: privacyOperationErrorMessage(error),
      failed: true,
    });
  } finally {
    depositInFlight = false;
    endPrivacyValueAction(actionLock);
    if (!isPrivacySessionCurrent(session)) return;
    setBusy(els.depositFromKeplr, false);
    renderKeplr();
  }
}

function scanKeplrNotes(options = {}) {
  const session = options.session || beginPrivacySessionOperation();
  pendingNoteScans += 1;
  renderKeplr();
  const run = noteScanQueue.then(
    () => runKeplrNoteScan(options, session),
    () => runKeplrNoteScan(options, session),
  );
  noteScanQueue = run.catch(() => undefined);
  return run.finally(() => {
    pendingNoteScans -= 1;
    renderKeplr();
  });
}

async function runKeplrNoteScan(options = {}, session = beginPrivacySessionOperation()) {
  assertPrivacySessionCurrent(session);
  if (!state.keplr.account) return;
  if (!options.skipPrivacySetup) {
    const setupReady = await setupKeplrPrivacy({ scanNotes: false });
    if (!setupReady) return;
  }
  assertPrivacySessionCurrent(session);
  if (!state.keplr.rootSignatureBase64) return;

  setBusy(els.scanKeplrNotes, true);
  if (!options.quiet) {
    els.keplrTxState.textContent = "Scanning notes";
  }
  try {
    const reset = Boolean(options.reset);
    const noteStore = currentWalletNoteStore({ optional: false });
    if (reset) {
      await clearCurrentWalletNoteStore({ session });
    }
    const scanOptions = noteScanRequestOptions({
      reset,
      requireComplete: true,
    });
    await clairveilBrowserClient().scanWalletNotes(
      privacyRequest({
        ...scanOptions,
        noteStore,
        includeFoundNotes: true,
      }),
    );
    assertPrivacySessionCurrent(session);
    const cached = await noteStore.load();
    assertPrivacySessionCurrent(session);
    applyPersistedWalletNoteState(cached);
    if (!hasCompletedPrivacyNoteScan()) {
      throw new Error(
        `Privacy note sync did not complete within the ${completeNoteScanMaxPages}-page safety limit. Scan again to resume before preparing a spend.`,
      );
    }
    await refreshCachedNoteStatuses({ session, noteStore });
    assertPrivacySessionCurrent(session);
    await reconcileReservedNotesFromScan({ session });
    assertPrivacySessionCurrent(session);
    await refreshNoteReservationState({ session });
    assertPrivacySessionCurrent(session);
    await reconcileExpiredRelayWithdrawPayloads(null, { session });
    assertPrivacySessionCurrent(session);
    state.keplr.scanError = "";
    if (!options.quiet) {
      const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
      const unverifiedCount = state.keplr.notes.filter(isUnverifiedNote).length;
      els.keplrTxState.textContent = unverifiedCount
        ? `Scan completed with ${unverifiedCount} unverified notes`
        : "Ready";
      toast(
        unverifiedCount
          ? `Keplr notes scanned; ${unverifiedCount} notes hidden until nullifier status is verified`
          : `Keplr notes scanned (${cursor.pagesScanned} pages)`,
      );
    }
    renderKeplr();
  } catch (error) {
    if (error?.privacySessionInvalidated) return;
    invalidatePrivacyScanState(error);
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

async function resetAndRescanNotes() {
  const confirmed = globalThis.confirm(
    "로컬 note cache를 지우고 체인 genesis부터 다시 스캔할까요? 완료까지 시간이 걸릴 수 있습니다.",
  );
  if (!confirmed) return;
  await scanKeplrNotes({ reset: true, throwOnError: true });
}

async function refreshPrivacySurfaces({ balance = false } = {}) {
  const tasks = [
    refreshEvents(),
    refreshAuditorTransfers(),
    scanKeplrNotes({ quiet: true }),
    refreshNotes(),
  ];
  if (balance) {
    tasks.unshift(refreshWalletBalance());
  }
  await Promise.allSettled(tasks);
}

async function transferFromVeiled() {
  const session = beginPrivacySessionOperation();
  if (!state.keplr.account) return;
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
      } catch (error) {
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
        await refreshPrivacySurfaces();
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
        els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
        const plannerBroadcast = await broadcastPreparedPrivacy(
          data,
          "self transaction",
          { waitForEvmReceipt: true },
        );
        state.keplr.transferHash =
          plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
        await refreshPrivacySurfaces();
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
    updateTransferFlow(
      "transfer",
      "트랜스퍼 서명 대기",
      `note 준비가 완료되었습니다. 이제 받는 사람에게 privacy transfer를 요청합니다. ${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"}에서 최종 전송 내용을 확인하고 서명해 주세요.`,
    );
    els.keplrTxState.textContent =
      state.activeWallet === "metamask"
        ? "Waiting for MetaMask"
        : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(
      finalData,
      "privacy transfer",
    );
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
        : "트랜스퍼 요청이 성공하였습니다",
    );
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.transferHash =
            included.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer included";
          await withPrivacySessionGuard(session, () => refreshPrivacySurfaces());
          assertPrivacySessionCurrent(session);
          renderKeplr();
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
          els.keplrTxState.textContent = "Transfer failed";
          finishTransferFlow(privacyOperationErrorMessage(error), false);
        },
      });
      return;
    }
    await withPrivacySessionGuard(session, () => refreshPrivacySurfaces());
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    if (error?.reservationReconciliationRequired) {
      state.keplr.transferHash =
        broadcastTxHash(error.broadcast) || state.keplr.transferHash;
      els.keplrTxState.textContent =
        "Transfer submitted; reservation reconciliation required";
      finishTransferFlow(
        `${error.message}${state.keplr.transferHash ? `\nTx: ${state.keplr.transferHash}` : ""}`,
      );
      return;
    }
    els.keplrTxState.textContent = "Transfer failed";
    finishTransferFlow(privacyOperationErrorMessage(error), false);
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
    } catch (error) {
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
      });
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. withdraw sign-doc을 다시 준비합니다.",
      );
      data = await preparePrivacyWithdrawSignDoc(amount, recipient);
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
        : "Withdraw 요청이 성공하였습니다",
    );
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        session,
        onIncluded: async (included) => {
          state.keplr.withdrawHash =
            included.txHash || state.keplr.withdrawHash;
          state.keplr.withdrawHeight =
            included.receipt?.blockNumber || state.keplr.withdrawHeight;
          els.keplrTxState.textContent = "Withdraw included";
          await withPrivacySessionGuard(
            session,
            () => refreshPrivacySurfaces({ balance: true }),
          );
          assertPrivacySessionCurrent(session);
          renderKeplr();
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
          els.keplrTxState.textContent = "Withdraw failed";
          finishTransferFlow(privacyOperationErrorMessage(error), false);
        },
      });
      return;
    }
    await withPrivacySessionGuard(
      session,
      () => refreshPrivacySurfaces({ balance: true }),
    );
  } catch (error) {
    if (error?.privacySessionInvalidated || !isPrivacySessionCurrent(session)) {
      return;
    }
    if (error?.reservationReconciliationRequired) {
      state.keplr.withdrawHash =
        broadcastTxHash(error.broadcast) || state.keplr.withdrawHash;
      state.keplr.withdrawHeight = state.keplr.withdrawHeight || "pending";
      els.keplrTxState.textContent =
        "Withdraw submitted; reservation reconciliation required";
      finishTransferFlow(
        `${error.message}${state.keplr.withdrawHash ? `\nTx: ${state.keplr.withdrawHash}` : ""}`,
      );
      return;
    }
    els.keplrTxState.textContent = "Withdraw failed";
    finishTransferFlow(privacyOperationErrorMessage(error), false);
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

async function rejectStaleRelayWithdrawPreparation(data) {
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
    );
  } catch (cleanupError) {
    error.cleanupError = cleanupError;
  }
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
    } catch (error) {
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
      });
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. relay withdraw payload를 다시 준비합니다.",
      );
      data = await preparePrivacyRelayWithdrawPayload(amount, recipient);
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
  const submissionLock = Object.freeze({
    generation: session.generation,
    payloadVersion: expectedPayloadVersion,
    payload,
  });
  relaySubmissionInFlight = true;
  relaySubmissionLock = submissionLock;
  setBusy(els.relayPreparedWithdraw, true);
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
    if (error?.privacySessionInvalidated) throw error;
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
      clearPreparedRelayWithdrawPayload({
        clearPayloadHash: true,
        stashHandedOff: false,
      });
    } else {
      await persistRelayWithdrawPayloadState({ session });
      assertRelayIsCurrent();
    }
    renderKeplr();
    toast(
      resolvedStatus === reservationStatuses.ReplanRequired
        ? "Expired relay reservation resolved. Prepare a fresh payload."
        : "Manual review could not be resolved. Refresh Notes and verify chain status.",
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
  let relayHandoffRecorded = Boolean(
    state.keplr.relayWithdrawPayloadHandedOff,
  );
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
        await markRelayReservationHandedOff(
          reservation,
          payload?.payload_hash,
          { expectedPayloadVersion, session },
        );
        assertRelayIsCurrent();
        relayHandoffRecorded = true;
        state.keplr.relayWithdrawPayloadHandedOff = true;
        stopPreparedRelayReservationHeartbeat();
        await extendReservationBatchLeaseToPayloadExpiry(
          reservation,
          payload,
          { expectedPayloadVersion, session },
        );
        assertRelayIsCurrent();
        await persistRelayWithdrawPayloadState({ session });
        assertRelayIsCurrent();
        renderKeplr();
        assertHeartbeatHealthy();
        const attemptRecords = await markPreparedReservationBroadcastAttempting(
          { reservation },
          "relay_withdraw",
          { session },
        );
        assertRelayIsCurrent();
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
    assertRelayIsCurrent();
    clearPreparedRelayWithdrawPayload({
      clearPayloadHash: true,
      stashHandedOff: false,
    });
    els.keplrTxState.textContent = "Relay withdraw included";
    renderKeplr();
    toast("Relay withdraw submitted");
    await withPrivacySessionGuard(
      session,
      () => refreshPrivacySurfaces({ balance: true }),
    );
    await withPrivacySessionGuard(session, () => refreshRelayerAccount());
  } catch (error) {
    if (error?.privacySessionInvalidated) throw error;
    if (!relayIsCurrent()) throw error;
    if (error?.relayPayloadExpired) {
      els.keplrTxState.textContent = "Relay payload is no longer submittable";
      toast(error.message);
      return;
    }
    if (error?.reservationReconciliationRequired) {
      els.keplrTxState.textContent =
        "Relay withdraw submitted; reservation reconciliation required";
      throw error;
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
    if (!relayHandoffRecorded && !hasBroadcastAttemptMetadata(attempt)) {
      state.keplr.relayWithdrawPayloadHandedOff = false;
      await persistRelayWithdrawPayloadState({ session });
      assertRelayIsCurrent();
      els.keplrTxState.textContent = "Relay validation failed before handoff";
      toast(error.message);
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
    els.keplrTxState.textContent = "Relay withdraw failed";
    toast(error.message);
  }
  } finally {
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
  connectWallet().catch((error) => toast(error.message)),
);
els.connectKeplr.addEventListener("click", () =>
  connectKeplr().catch((error) => toast(error.message)),
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
  signSession().catch((error) => toast(error.message)),
);
els.copyWalletAccount.addEventListener("click", () =>
  copyWalletAccount().catch((error) => toast(error.message)),
);
els.fundKeplr.addEventListener("click", fundKeplr);
els.setupKeplrPrivacy.addEventListener("click", () =>
  setupKeplrPrivacy().catch((error) => toast(error.message)),
);
els.copyKeplrDisclosurePubKey.addEventListener("click", () =>
  copyKeplrDisclosurePubKey().catch((error) => toast(error.message)),
);
els.refreshWalletBalance.addEventListener("click", () =>
  refreshWalletBalance().catch((error) => toast(error.message)),
);
els.refreshClairBalance.addEventListener("click", () =>
  refreshWalletBalance().catch((error) => toast(error.message)),
);
els.scanKeplrNotes.addEventListener("click", () =>
  scanKeplrNotes().catch((error) => toast(error.message)),
);
els.resetRescanNotes.addEventListener("click", () =>
  resetAndRescanNotes().catch((error) => toast(error.message)),
);
els.clearLegacyPrivacyData.addEventListener("click", () =>
  clearLegacyPrivacyData().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
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
    discardAndClearPreparedRelayWithdrawPayload()
      .catch((error) => toast(error.message))
      .finally(() => renderKeplr());
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
els.transferFromVeiled.addEventListener("click", transferFromVeiled);
els.withdrawFromVeiled.addEventListener("click", withdrawFromVeiled);
els.relayWithdrawFromVeiled.addEventListener("click", relayWithdrawFromVeiled);
els.copyRelayWithdrawPayload.addEventListener("click", () =>
  copyRelayWithdrawPayload().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.relayPreparedWithdraw.addEventListener("click", () =>
  relayPreparedWithdraw().catch((error) => {
    if (!error?.privacySessionInvalidated) toast(error.message);
  }),
);
els.refreshAll.addEventListener("click", () =>
  refreshHealth().catch((error) => toast(error.message)),
);
els.refreshNotes.addEventListener("click", () =>
  refreshNotes().catch((error) => toast(error.message)),
);
els.refreshEvents.addEventListener("click", () =>
  refreshEvents().catch((error) => toast(error.message)),
);
els.decodeEventDisclosure.addEventListener("click", () =>
  decodeSelectedEventDisclosure().catch((error) => toast(error.message)),
);
if (els.refreshAuditorTransfers) {
  els.refreshAuditorTransfers.addEventListener("click", () =>
    refreshAuditorTransfers().catch((error) => toast(error.message)),
  );
}
if (els.decodeAuditorTransfer) {
  els.decodeAuditorTransfer.addEventListener("click", () =>
    decodeAuditorTransfer().catch((error) => toast(error.message)),
  );
}
els.closeNoticeModal.addEventListener("click", closeNoticeModal);
els.cancelTransferFlow.addEventListener("click", cancelTransferFlow);
els.confirmTransferFlow.addEventListener("click", confirmTransferFlowStart);
els.noticeModal.addEventListener("click", (event) => {
  if (event.target === els.noticeModal) {
    closeNoticeModal();
  }
});
els.transferFlowModal.addEventListener("click", (event) => {
  if (event.target === els.transferFlowModal) {
    cancelTransferFlow();
  }
});
window.addEventListener("keydown", (event) => {
  if (event.key !== "Escape") return;
  if (!els.transferFlowModal.hidden) {
    cancelTransferFlow();
  } else if (!els.noticeModal.hidden) {
    closeNoticeModal();
  }
});
els.accountSelect.addEventListener("change", (event) => {
  state.selectedAccount = event.target.value;
  refreshSelectedAccount().catch((error) => toast(error.message));
});

const injectedMetaMask = metaMaskProvider();
if (injectedMetaMask) {
  injectedMetaMask.on?.("accountsChanged", (accounts) => {
    if (state.activeWallet !== "metamask") return;
    invalidatePrivacySessionOperations();
    discardAndClearPreparedRelayWithdrawPayload()
      .catch((error) => toast(error.message))
      .finally(() => {
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
  });
  injectedMetaMask.on?.("chainChanged", (chainId) => {
    if (state.activeWallet !== "metamask") return;
    invalidatePrivacySessionOperations();
    discardAndClearPreparedRelayWithdrawPayload()
      .catch((error) => toast(error.message))
      .finally(() => {
        resetWalletSession();
        state.wallet.chainId = chainId;
        renderWallet();
        renderKeplr();
        toast(
          "MetaMask network changed. Reconnect wallet before preparing another privacy transaction.",
        );
      });
  });
}

window.addEventListener("keplr_keystorechange", () => {
  invalidatePrivacySessionOperations();
  discardAndClearPreparedRelayWithdrawPayload()
    .catch((error) => toast(error.message))
    .finally(() => {
      if (state.activeWallet === "keplr") {
        state.activeWallet = "";
      }
      resetKeplrSession();
      renderWallet();
      renderKeplr();
    });
});

renderWallet();
renderKeplr();
renderTransferDisclosureAdvanced();
setupAddressSuggestions();
refreshHealth().catch((error) => toast(error.message));
