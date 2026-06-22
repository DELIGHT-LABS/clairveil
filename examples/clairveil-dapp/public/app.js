import { createClairveilBrowserDappClient } from "clairveiljs/browser-dapp";
import { bech32AddressToEvm } from "clairveiljs/evm";
import { getStaticDappConfig } from "./dapp-config.js";

function defaultMetaMaskState() {
  return {
    account: "",
    chainId: "",
    signatureHash: ""
  };
}

function defaultNoteScanCursor() {
  return {
    afterHeight: 0,
    page: 1,
    nextPage: 1,
    limit: 200,
    maxPages: 5,
    hasMore: false,
    latestHeight: 0,
    pagesScanned: 0,
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
    notesSummary: "",
    notes: [],
    notesScanned: false,
    noteScanCursor: defaultNoteScanCursor(),
    showSpendableOnly: true
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
  }
};

const $ = selector => document.querySelector(selector);
let shieldedAddressBookPromise = null;
let browserClient = null;
let browserClientKey = "";
let serverConfigAvailable = true;

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
    keplrChainInfo: state.config.keplrChainInfo
  };
}

function activeChainProfile() {
  return state.chainProfiles.find(profile => profile.id === state.selectedChainProfileId)
    || state.chainProfiles.find(profile => profile.id === state.config?.activeChainProfileId)
    || configuredChainProfile();
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
  return activeChainProfile()?.keplrChainInfo || state.config?.keplrChainInfo;
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
  const configured = profile?.rest || state.config?.rest || "";
  return browserEndpointUrl(configured, { trim: true });
}

function browserProverUrl(profile = activeChainProfile()) {
  const configured = profile?.proverUrl || state.config?.proverUrl || "";
  if (state.config?.serverBacked && configured) {
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

function clairveilBrowserClient(profile = activeChainProfile()) {
  const resolved = profile || configuredChainProfile();
  const key = JSON.stringify({
    id: resolved?.id || "",
    rpc: browserRpcUrl(resolved),
    rest: browserRestUrl(resolved),
    chainId: resolved?.chainId || state.config?.chainId || "",
    accountPrefix: resolved?.accountPrefix || state.config?.accountPrefix || "",
    shieldedPrefix: resolved?.shieldedPrefix || state.config?.shieldedPrefix || "",
    denom: resolved?.denom || state.config?.denom || "",
    proverUrl: browserProverUrl(resolved),
    evmRpc: evmRpcUrlForWallet(resolved),
    evmChainId: resolved?.evmChainId || state.config?.evmChainId || "",
    evmPrivacyPrecompileAddress: resolved?.evmPrivacyPrecompileAddress || state.config?.evmPrivacyPrecompileAddress || ""
  });
  if (!browserClient || browserClientKey !== key) {
    browserClient = createClairveilBrowserDappClient({
      profile: {
        ...resolved,
        rpc: browserRpcUrl(resolved),
        rest: browserRestUrl(resolved),
        chainId: resolved?.chainId || state.config?.chainId,
        accountPrefix: resolved?.accountPrefix || state.config?.accountPrefix,
        shieldedPrefix: resolved?.shieldedPrefix || state.config?.shieldedPrefix,
        denom: resolved?.denom || state.config?.denom,
        proverUrl: browserProverUrl(resolved),
        evmRpc: evmRpcUrlForWallet(resolved),
        evmChainId: resolved?.evmChainId || state.config?.evmChainId,
        evmPrivacyPrecompileAddress: resolved?.evmPrivacyPrecompileAddress || state.config?.evmPrivacyPrecompileAddress,
        evmGasLimit: resolved?.evmGasLimit || state.config?.evmGasLimit,
        evmSendGasLimit: resolved?.evmSendGasLimit || state.config?.evmSendGasLimit
      }
    });
    browserClientKey = key;
  }
  return browserClient;
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
  keplrDisclosurePubKey: $("#keplrDisclosurePubKey"),
  copyKeplrDisclosurePubKey: $("#copyKeplrDisclosurePubKey"),
  faucetHelpText: $("#faucetHelpText"),
  faucetRow: $(".faucet-row"),
  keplrFaucetAmount: $("#keplrFaucetAmount"),
  fundKeplr: $("#fundKeplr"),
  setupKeplrPrivacy: $("#setupKeplrPrivacy"),
  refreshWalletBalance: $("#refreshKeplrBalance"),
  scanKeplrNotes: $("#scanKeplrNotes"),
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
  keplrTransferHash: $("#keplrTransferHash"),
  keplrWithdrawHash: $("#keplrWithdrawHash"),
  keplrWithdrawHeight: $("#keplrWithdrawHeight"),
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
  eventDisclosureFields: $("#eventDisclosureFields"),
  eventDisclosureAmount: $("#eventDisclosureAmount"),
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
}

function transferDisclosurePolicy() {
  if (!els.veiledDisclosureAdvanced.checked) {
    return {
      privacyPolicy: "all-private"
    };
  }

  const disclosureMode = els.veiledDisclosureMode?.value || "recipient-encrypted";
  const pubKeyHex = els.veiledDisclosurePubKey.value.trim();

  if (disclosureMode === "none") {
    return {
      privacyPolicy: "all-private",
      disclosureMode
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
      disclosureMode
    };
  }

  if (!/^[0-9a-fA-F]{64}$/.test(pubKeyHex)) {
    throw new Error("Disclosure target은 show-disclosure-pubkey로 만든 32-byte hex 값을 넣어줘.");
  }

  return {
    privacyPolicy,
    disclosureMode: "recipient-encrypted",
    disclosurePubKeyHex: pubKeyHex
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
  copy: null
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
  els.transferFlowModal.hidden = true;
  els.transferFlowModal.classList.remove("visible");
  if (resolve) {
    resolve(result);
  }
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
  return new Promise(resolve => {
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
  els.transferModalLead.textContent = transferFlowState.copy?.runningLead || privacyFlowCopies.transfer.runningLead;
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
    els.transferFailureReason.textContent = message || "알 수 없는 오류가 발생했습니다.";
    els.transferFailurePanel.hidden = false;
  }
  els.cancelTransferFlow.textContent = "닫기";
  els.cancelTransferFlow.hidden = false;
  els.cancelTransferFlow.disabled = false;
  els.cancelTransferFlow.focus();
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
  return String(note?.nullifier || note?.nullifier_hex || "").trim().toLowerCase();
}

function isZeroAmountNote(note) {
  return noteAmountValue(note) === 0n;
}

function isHelperNote(note) {
  return isSpendableNote(note) && isZeroAmountNote(note);
}

function noteStatusLabel(note) {
  return isHelperNote(note) ? "helper" : String(note?.status || "-");
}

function summarizeSpendableValueNotes(notes) {
  const spendableValueNotes = (notes || []).filter(note => isSpendableNote(note) && !isZeroAmountNote(note));
  const helperCount = (notes || []).filter(isHelperNote).length;
  const total = spendableValueNotes.reduce((sum, note) => sum + noteAmountValue(note), 0n);
  const helperText = helperCount ? ` · ${helperCount} helper` : "";
  return `${total}${baseDenom()} / ${spendableValueNotes.length} spendable${helperText}`;
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

function noteScanRequestOptions({ reset = false } = {}) {
  const cursor = reset ? defaultNoteScanCursor() : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const hasMore = !reset && Boolean(cursor.hasMore);
  return {
    afterHeight: hasMore ? Number(cursor.afterHeight || 0) : Number(cursor.latestHeight || 0),
    page: hasMore ? Number(cursor.nextPage || 1) : 1,
    limit: Number(cursor.limit || 200),
    maxPages: Number(cursor.maxPages || 5),
    eventTypes: ["deposit", "shielded_transfer"]
  };
}

function applyNoteScanResult(data, { reset = false } = {}) {
  const previous = reset ? defaultNoteScanCursor() : state.keplr.noteScanCursor || defaultNoteScanCursor();
  const cursor = data?.scanCursor || data?.scan_cursor || {};
  const hasMore = Boolean(cursor.has_more ?? cursor.hasMore);
  const cursorAfterHeight = Number(cursor.after_height ?? cursor.afterHeight ?? previous.afterHeight ?? 0);
  const latestHeight = Number(cursor.latest_height ?? cursor.latestHeight ?? 0);
  const completedLatestHeight = Math.max(Number(previous.latestHeight || 0), latestHeight, hasMore ? 0 : cursorAfterHeight);
  state.keplr.notes = mergeCachedNotes(reset ? [] : state.keplr.notes, data?.notes || []);
  state.keplr.noteScanCursor = {
    afterHeight: hasMore ? cursorAfterHeight : completedLatestHeight,
    page: Number(cursor.page || previous.page || 1),
    nextPage: hasMore
      ? Number(cursor.next_page ?? cursor.nextPage ?? (Number(cursor.page || previous.page || 1) + 1))
      : 1,
    limit: Number(cursor.limit || previous.limit || 200),
    maxPages: Number(previous.maxPages || 5),
    hasMore,
    latestHeight: completedLatestHeight,
    pagesScanned: Number(cursor.pages_scanned ?? cursor.pagesScanned ?? 1),
    completed: Boolean(cursor.completed ?? !hasMore)
  };
  const moreText = state.keplr.noteScanCursor.hasMore ? " · more events queued" : "";
  state.keplr.notesSummary = `${summarizeSpendableValueNotes(state.keplr.notes)}${moreText}`;
  state.keplr.notesScanned = true;
}

async function refreshCachedNoteStatuses() {
  const spendableNotes = (state.keplr.notes || []).filter(note => isSpendableNote(note) && noteNullifier(note));
  if (!spendableNotes.length) return;

  const spentNullifiers = new Set();
  const concurrency = 8;
  for (let index = 0; index < spendableNotes.length; index += concurrency) {
    const chunk = spendableNotes.slice(index, index + concurrency);
    await Promise.all(chunk.map(async note => {
      try {
        const result = await clairveilBrowserClient().checkNullifier(noteNullifier(note));
        if (Boolean(result?.used ?? result?.Used ?? result)) {
          spentNullifiers.add(noteNullifier(note));
        }
      } catch {
        // Keep the cached status if the chain nullifier query is temporarily unavailable.
      }
    }));
  }
  if (!spentNullifiers.size) return;

  state.keplr.notes = state.keplr.notes.map(note => {
    if (!spentNullifiers.has(noteNullifier(note))) return note;
    return {
      ...note,
      status: "spent",
      spent: true,
      isSpent: true
    };
  });
  const moreText = state.keplr.noteScanCursor?.hasMore ? " · more events queued" : "";
  state.keplr.notesSummary = `${summarizeSpendableValueNotes(state.keplr.notes)}${moreText}`;
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
  const profiles = state.chainProfiles.length ? state.chainProfiles : [configuredChainProfile()].filter(Boolean);
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

function firstConfigProfile(config) {
  return (config.chainProfiles || []).find(profile => profile.id === config.activeChainProfileId)
    || config.chainProfiles?.[0]
    || null;
}

async function browserHealthFromStaticConfig(config) {
  const profile = firstConfigProfile(config);
  state.config = config;
  state.chainProfiles = config.chainProfiles || [];
  state.selectedChainProfileId = config.activeChainProfileId || profile?.id || "";
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
  state.keplr = defaultKeplrState();
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
  els.keplrTransferHash.textContent = state.keplr.transferHash ? shorten(state.keplr.transferHash, 14, 12) : "-";
  els.keplrWithdrawHash.textContent = state.keplr.withdrawHash ? shorten(state.keplr.withdrawHash, 14, 12) : "-";
  els.keplrWithdrawHeight.textContent = state.keplr.withdrawHeight || "-";
  if (connected && !els.veiledWithdrawRecipient.value) {
    els.veiledWithdrawRecipient.value = state.keplr.account;
  }
  renderMyKeplrNotes();
  els.fundKeplr.disabled = !serverFeature("faucet") || !signerReady;
  els.setupKeplrPrivacy.disabled = !signerReady;
  els.copyKeplrDisclosurePubKey.disabled = !state.keplr.disclosurePubKeyHex;
  els.refreshWalletBalance.disabled = !connected;
  els.scanKeplrNotes.disabled = !signerReady || !state.keplr.rootSignatureBase64;
  updateAmountActionButtons({ signerReady, veiledReady });
  renderEventDetail();
}

function updateAmountActionButtons(status = {}) {
  const connected = Boolean(state.keplr.account);
  const signerReady = status.signerReady ?? (connected && state.keplr.addressMatches);
  const veiledReady = status.veiledReady ?? (signerReady && Boolean(state.keplr.rootSignatureBase64));
  els.sendFromKeplr.disabled = !signerReady
    || !hasPositiveUclairInput(els.keplrSendAmount)
    || !isSendRecipientForWallet(els.keplrSendRecipient.value, state.activeWallet || activeWalletKind());
  els.depositFromKeplr.disabled = !signerReady || !hasPositiveUclairInput(els.keplrDepositAmount);
  els.transferFromVeiled.disabled = !veiledReady || !hasPositiveUclairInput(els.veiledTransferAmount);
  els.withdrawFromVeiled.disabled = !veiledReady || !hasPositiveUclairInput(els.veiledWithdrawAmount);
}

function renderMyKeplrNotes() {
  els.myKeplrSpendable.textContent = state.keplr.notesSummary || "-";
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
    const row = document.createElement("article");
    row.className = "note-row";
    row.classList.toggle("helper-note", isHelperNote(note));
    row.innerHTML = `
      <strong>${note.amount}${baseDenom()}</strong>
      <span>${noteStatusLabel(note)}</span>
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
  state.config = data.config;
  state.chainProfiles = data.config?.chainProfiles || [];
  if (!state.selectedChainProfileId || !state.chainProfiles.some(profile => profile.id === state.selectedChainProfileId)) {
    state.selectedChainProfileId = data.config?.activeChainProfileId || state.chainProfiles[0]?.id || "";
  }
  state.accounts = data.accounts || [];
  if (!state.accounts.some(account => account.name === state.selectedAccount)) {
    state.selectedAccount = state.accounts[0]?.name || "alice";
  }

  renderServerFeatureVisibility();
  els.modeBadge.textContent = data.config?.modeLabel || (localTestBackendEnabled() ? "Local Note Test Web" : "Public Node DApp");
  els.modeBadge.classList.toggle("public-mode", !localTestBackendEnabled());
  els.localHome.textContent = data.config?.localSignerHome || data.config?.home || "-";
  els.chainId.textContent = data.status?.node_info?.network || data.config?.chainId || "-";
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
  const tasks = [refreshEvents({ allowFailure: true })];
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
    const balanceHex = await requestMetaMask({
      method: "eth_getBalance",
      params: [state.wallet.account, "latest"]
    });
    state.keplr.balance = formatBalances([{
      denom: baseDenom(),
      amount: BigInt(balanceHex || "0x0").toString()
    }]);
  } else {
    const data = await clairveilBrowserClient().getBalances(state.keplr.account);
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

function eventDisclosureStatus(event) {
  if (!event) return "Select an event.";
  if (event.event_type !== "shielded_transfer") return "Disclosure 조회는 shielded transfer에서만 가능합니다.";
  const mode = eventAttribute(event, "user_disclosure_mode");
  const target = eventAttribute(event, "user_disclosure_target_pubkey");
  const payload = eventAttribute(event, "user_disclosure_payload");
  if (!payload) {
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
  els.eventDisclosureFields.textContent = "-";
  els.eventDisclosureAmount.textContent = "-";
  els.eventDisclosureFrom.textContent = "-";
  els.eventDisclosureTo.textContent = "-";
}

function renderEventDisclosureReport(report) {
  const summary = report?.summary || {};
  const amount = summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";
  els.eventDisclosureFields.textContent = (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") || "-";
  els.eventDisclosureAmount.textContent = amount;
  els.eventDisclosureFrom.textContent = summary.from_shielded_address || "-";
  els.eventDisclosureTo.textContent = summary.to_shielded_address || "-";
  els.eventDisclosureState.textContent = report?.verification?.verified
    ? `${summary.delivery || "recipient-encrypted"} / ${summary.policy || "unknown policy"}`
    : "Disclosure verification failed.";
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
    els.eventDisclosureState.textContent = state.privacyEvents.error;
  } else {
    els.eventDisclosureState.textContent = eventDisclosureStatus(event);
  }
  els.decodeEventDisclosure.disabled = state.privacyEvents.loading || !canDecodeEventDisclosure(event);
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
  } finally {
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
    "encoded"
  );
  els.auditorDecodeState.textContent = "Audit disclosure is present. Select Decode to use the local admin test scalar.";
  updateAuditorDecodeButton();
}

function renderAuditorReport(report) {
  if (!hasAuditorUi()) return;
  const summary = report?.summary || {};
  const payload = report?.payload || {};
  const verification = report?.verification || {};
  const verified = verification.verified ? "Verified" : "Failed";
  const amount = summary.amount
    ? `${summary.amount}${summary.asset_denom ? ` ${summary.asset_denom}` : ""}`
    : "-";

  els.auditorTxHash.textContent = report?.tx_hash || state.auditor.selectedTxHash || "-";
  els.auditorVerification.textContent = verified;
  els.auditorAmount.textContent = amount;
  els.auditorFrom.textContent = summary.from_shielded_address || "-";
  els.auditorTo.textContent = summary.to_shielded_address || "-";
  els.auditorFields.textContent = (summary.disclosed_fields || []).map(prettyDisclosureField).join(", ") || "-";
  els.auditorDigest.textContent = payload.disclosure_digest_hex || eventAttribute(
    state.auditor.events.find(event => event.tx_hash_hex === state.auditor.selectedTxHash),
    "audit_disclosure_digest"
  ) || "-";
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
  state.keplr.faucetHash = "";
  state.keplr.faucetSent = "";
  state.keplr.faucetRecipient = "";
  state.keplr.shieldedAddress = "";
  state.keplr.disclosurePubKeyHex = "";
  state.keplr.rootSignatureBase64 = "";
  state.keplr.rootSignatureHash = "";
  state.keplr.sendHash = "";
  state.keplr.depositHash = "";
  state.keplr.depositHeight = "";
  state.keplr.transferHash = "";
  state.keplr.withdrawHash = "";
  state.keplr.withdrawHeight = "";
  state.keplr.notesSummary = "";
  state.keplr.notes = [];
  state.keplr.notesScanned = false;
  state.keplr.noteScanCursor = defaultNoteScanCursor();
  state.privacyEvents.decoded = null;
  state.privacyEvents.error = "";
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

async function setupKeplrPrivacy() {
  if (!state.keplr.account) return;
  if (state.keplr.rootSignatureBase64 && state.keplr.shieldedAddress && state.keplr.disclosurePubKeyHex) {
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
    els.keplrTxState.textContent = "Ready";
    renderKeplr();
    toast("Clairveil account ready");
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

async function copyWalletAccount() {
  const account = currentWalletAccountForCopy();
  if (!account) {
    toast("Connect a wallet first");
    return;
  }
  await navigator.clipboard.writeText(account);
  toast("Account copied");
}

async function signDirectAndBroadcast(signDoc) {
  if (!window.keplr?.signDirect) {
    throw new Error("Keplr signDirect not available");
  }
  const directSignDoc = {
    bodyBytes: base64ToBytes(signDoc.bodyBytes),
    authInfoBytes: base64ToBytes(signDoc.authInfoBytes),
    chainId: signDoc.chainId,
    accountNumber: BigInt(signDoc.accountNumber)
  };
  const signed = await window.keplr.signDirect(signDoc.chainId, state.keplr.account, directSignDoc);
  return clairveilBrowserClient().broadcastSignedTx({
    bodyBytes: bytesToBase64(signed.signed.bodyBytes),
    authInfoBytes: bytesToBase64(signed.signed.authInfoBytes),
    signature: signed.signature.signature
  });
}

async function submitEvmTransaction(transaction) {
  if (!metaMaskProvider() || !state.wallet.account) {
    throw new Error("MetaMask is not connected");
  }
  await ensureMetaMaskChain();
  const tx = await withEstimatedEvmGas({ ...transaction, from: state.wallet.account });
  const txHash = await requestMetaMask({
    method: "eth_sendTransaction",
    params: [tx]
  });
  return normalizeEvmTxHash(txHash);
}

async function waitForEvmTransaction(txHash, label = "EVM transaction") {
  const broadcast = await clairveilBrowserClient().waitForEvmTransaction(txHash);
  assertSuccessfulBroadcast(broadcast, label);
  return broadcast;
}

async function sendEvmTransaction(transaction, { waitForReceipt = false, label = "EVM transaction" } = {}) {
  const txHash = await submitEvmTransaction(transaction);
  if (waitForReceipt) {
    const broadcast = await waitForEvmTransaction(txHash, label);
    return { ...broadcast, txHash: broadcast.txHash || txHash };
  }
  const waitPromise = waitForEvmTransaction(txHash, label);
  waitPromise.catch(() => {});
  return {
    txHash,
    pending: true,
    waitPromise
  };
}

function watchEvmBroadcast(broadcast, { onIncluded, onFailed } = {}) {
  if (!broadcast?.waitPromise) return;
  broadcast.waitPromise.then(result => {
    onIncluded?.(result);
  }).catch(error => {
    onFailed?.(error);
  });
}

function keplrPrivacyRequest(extra = {}) {
  return {
    address: state.keplr.account,
    pubKeyHex: state.keplr.pubkeyHex,
    signatureBase64: state.keplr.rootSignatureBase64,
    ...extra
  };
}

function evmPrivacyRequest(extra = {}) {
  return {
    walletType: "evm",
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

async function preparePrivacyDepositSignDoc(amount) {
  return clairveilBrowserClient().prepareDeposit(privacyRequest({ amount }));
}

async function preparePrivacyTransferSignDoc(amount, recipient, disclosure = {}, options = {}) {
  return clairveilBrowserClient().prepareTransfer(privacyRequest({
    amount,
    recipient,
    scan: { limit: 200, maxPages: 1000 },
    ...disclosure,
    allowPlanStep: Boolean(options.allowPlanStep)
  }));
}

async function preparePrivacyWithdrawSignDoc(amount, recipient) {
  return clairveilBrowserClient().prepareWithdraw(privacyRequest({
    amount,
    recipient,
    scan: { limit: 200, maxPages: 1000 }
  }));
}

async function broadcastPrivacyDeposit(amount, label = "deposit", options = {}) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyDepositSignDoc(amount);
  state.keplr.shieldedAddress = data.prepared?.shieldedAddress || state.keplr.shieldedAddress;
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label, options);
  state.keplr.depositHash = broadcast.broadcast?.txhash || "";
  state.keplr.depositHash = state.keplr.depositHash || broadcast.txHash || "";
  state.keplr.depositHeight = broadcast.tx?.height || broadcast.receipt?.blockNumber || "pending";
  return broadcast;
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
  const broadcast = state.activeWallet === "metamask"
    ? await sendEvmTransaction(data.transaction, {
      label,
      waitForReceipt: Boolean(options.waitForEvmReceipt)
    })
    : await signDirectAndBroadcast(data.signDoc);
  assertSuccessfulBroadcast(broadcast, label);
  return broadcast;
}

async function broadcastVeiledTransfer(amount, recipient, label = "veiled transfer", disclosure = {}, options = {}) {
  els.keplrTxState.textContent = `Preparing ${label}`;
  const data = await preparePrivacyTransferSignDoc(amount, recipient, disclosure, options);
  els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
  const broadcast = await broadcastPreparedPrivacy(data, label);
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
      data = await preparePrivacyTransferSignDoc(amount, state.keplr.shieldedAddress, {}, { allowPlanStep: true });
    } catch (error) {
      if (!isZeroHelperNeededError(error)) {
        throw error;
      }
      hooks.onZeroHelperNeeded?.(error, step, maxPlannerSteps);
      await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", { waitForEvmReceipt: true });
      await refreshPrivacySurfaces();
      continue;
    }

    if (data.prepared?.isFinal === false || data.prepared?.planAction === "self_merge") {
      hooks.onSelfMergeNeeded?.(data, step, maxPlannerSteps);
      els.keplrTxState.textContent = `${state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr"} (${step}/${maxPlannerSteps})`;
      const plannerBroadcast = await broadcastPreparedPrivacy(data, "exact-note self transaction", { waitForEvmReceipt: true });
      state.keplr.transferHash = plannerBroadcast.broadcast?.txhash || plannerBroadcast.txHash || "";
      await refreshPrivacySurfaces();
      continue;
    }

    hooks.onFinalExactTransfer?.(data, step, maxPlannerSteps);
    els.keplrTxState.textContent = state.activeWallet === "metamask" ? "Waiting for MetaMask" : "Waiting for Keplr";
    const broadcast = await broadcastPreparedPrivacy(data, "exact-note self transfer", { waitForEvmReceipt: true });
    state.keplr.transferHash = broadcast.broadcast?.txhash || broadcast.txHash || "";
    await refreshPrivacySurfaces();
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
          els.keplrTxState.textContent = "Send included";
          await Promise.allSettled([refreshWalletBalance(), refreshBlockEvents()]);
          renderKeplr();
        },
        onFailed: error => {
          els.keplrTxState.textContent = "Send failed";
          showSendResult({ success: false, error: error.message });
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
    const broadcast = await broadcastPrivacyDeposit(amountInputValue(els.keplrDepositAmount));
    const isPendingEvm = Boolean(broadcast.pending);
    els.keplrTxState.textContent = isPendingEvm ? "Deposit submitted" : "Deposit included";
    renderKeplr();
    showNotice({
      title: isPendingEvm ? "Deposit 요청됨" : "Deposit 성공",
      message: `${state.activeWallet === "metamask" ? "MetaMask" : "Keplr"} deposit이 ${isPendingEvm ? "제출되었습니다" : "처리되었습니다"}.\nTx: ${shorten(state.keplr.depositHash, 14, 12)}`
    });
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.depositHash = included.txHash || state.keplr.depositHash;
          state.keplr.depositHeight = included.receipt?.blockNumber || state.keplr.depositHeight;
          els.keplrTxState.textContent = "Deposit included";
          await refreshPrivacySurfaces({ balance: true });
          renderKeplr();
        },
        onFailed: error => {
          els.keplrTxState.textContent = "Deposit failed";
          showNotice({ title: "Deposit 실패", message: error.message, failed: true });
        }
      });
      return;
    }
    await refreshPrivacySurfaces({ balance: true });
  } catch (error) {
    els.keplrTxState.textContent = "Deposit failed";
    showNotice({ title: "Deposit 실패", message: error.message, failed: true });
  } finally {
    setBusy(els.depositFromKeplr, false);
    renderKeplr();
  }
}

async function scanKeplrNotes(options = {}) {
  if (!state.keplr.account) return;
  await setupKeplrPrivacy();
  if (!state.keplr.rootSignatureBase64) return;

  setBusy(els.scanKeplrNotes, true);
  if (!options.quiet) {
    els.keplrTxState.textContent = "Scanning notes";
  }
  try {
    const reset = Boolean(options.reset);
    const scanOptions = noteScanRequestOptions({ reset });
    const data = await clairveilBrowserClient().scanWalletNotes(privacyRequest({
      ...scanOptions,
      includeFoundNotes: true
    }));
    applyNoteScanResult(data, { reset });
    await refreshCachedNoteStatuses();
    if (!options.quiet) {
      const cursor = state.keplr.noteScanCursor || defaultNoteScanCursor();
      els.keplrTxState.textContent = "Ready";
      toast(cursor.hasMore
        ? `Keplr notes scanned (${cursor.pagesScanned} pages, more queued)`
        : "Keplr notes scanned");
    }
    renderKeplr();
  } catch (error) {
    if (!options.quiet) {
      els.keplrTxState.textContent = "Scan failed";
      toast(error.message);
    }
  } finally {
    setBusy(els.scanKeplrNotes, false);
    renderKeplr();
  }
}

async function refreshPrivacySurfaces({ balance = false } = {}) {
  const tasks = [
    refreshEvents(),
    refreshAuditorTransfers(),
    scanKeplrNotes({ quiet: true }),
    refreshNotes()
  ];
  if (balance) {
    tasks.unshift(refreshWalletBalance());
  }
  await Promise.allSettled(tasks);
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
  const confirmed = await openTransferFlowModal();
  if (!confirmed) return;

  setBusy(els.transferFromVeiled, true);
  els.keplrTxState.textContent = "Preparing veiled transfer";
  try {
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
        data = await preparePrivacyTransferSignDoc(amount, recipient, disclosure, { allowPlanStep: true });
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
        await broadcastPrivacyDeposit(zeroCoinText(), "zero helper note", { waitForEvmReceipt: true });
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
        continue;
      }

      finalData = data;
      break;
    }

    if (!finalData) {
      throw new Error("입력하신 금액의 노트 준비가 너무 오래 걸립니다. notes를 다시 스캔한 뒤 재시도해줘.");
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
    finishTransferFlow(isPendingEvm ? "트랜스퍼 요청이 제출되었습니다" : "트랜스퍼 요청이 성공하였습니다");
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.transferHash = included.txHash || state.keplr.transferHash;
          els.keplrTxState.textContent = "Transfer included";
          await refreshPrivacySurfaces();
          renderKeplr();
        },
        onFailed: error => {
          els.keplrTxState.textContent = "Transfer failed";
          finishTransferFlow(error.message, false);
        }
      });
      return;
    }
    await refreshPrivacySurfaces();
  } catch (error) {
    els.keplrTxState.textContent = "Transfer failed";
    finishTransferFlow(error.message, false);
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
  if (!recipient) {
    toast(`Withdraw recipient에 받을 ${accountPrefix()} 주소를 넣어줘.`);
    return;
  }

  await setupKeplrPrivacy();
  if (!state.keplr.rootSignatureBase64) return;

  const confirmed = await openTransferFlowModal("withdraw");
  if (!confirmed) return;

  setBusy(els.withdrawFromVeiled, true);
  els.keplrTxState.textContent = "Preparing withdraw";
  try {
    resetTransferPlannerFacts();
    updateTransferFlow(
      "zero",
      "노트 확인 중",
      "Withdraw에 사용할 정확한 금액의 note가 있는지 확인합니다."
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
      });
      resetTransferPlannerFacts();
      updateTransferFlow(
        "zero",
        "노트 재확인 중",
        "exact note 준비가 끝났습니다. withdraw sign-doc을 다시 준비합니다."
      );
      data = await preparePrivacyWithdrawSignDoc(amount, recipient);
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
    renderKeplr();
    finishTransferFlow(isPendingEvm ? "Withdraw 요청이 제출되었습니다" : "Withdraw 요청이 성공하였습니다");
    if (isPendingEvm) {
      watchEvmBroadcast(broadcast, {
        onIncluded: async included => {
          state.keplr.withdrawHash = included.txHash || state.keplr.withdrawHash;
          state.keplr.withdrawHeight = included.receipt?.blockNumber || state.keplr.withdrawHeight;
          els.keplrTxState.textContent = "Withdraw included";
          await refreshPrivacySurfaces({ balance: true });
          renderKeplr();
        },
        onFailed: error => {
          els.keplrTxState.textContent = "Withdraw failed";
          finishTransferFlow(error.message, false);
        }
      });
      return;
    }
    await refreshPrivacySurfaces({ balance: true });
  } catch (error) {
    els.keplrTxState.textContent = "Withdraw failed";
    finishTransferFlow(error.message, false);
  } finally {
    setBusy(els.withdrawFromVeiled, false);
    renderKeplr();
  }
}

els.connectWallet.addEventListener("click", () => connectWallet().catch(error => toast(error.message)));
els.connectKeplr.addEventListener("click", () => connectKeplr().catch(error => toast(error.message)));
els.disconnectWallet.addEventListener("click", disconnectWallet);
els.dappChainSelect.addEventListener("change", event => selectDappChainProfile(event.target.value));
els.signSession.addEventListener("click", () => signSession().catch(error => toast(error.message)));
els.copyWalletAccount.addEventListener("click", () => copyWalletAccount().catch(error => toast(error.message)));
els.fundKeplr.addEventListener("click", fundKeplr);
els.setupKeplrPrivacy.addEventListener("click", () => setupKeplrPrivacy().catch(error => toast(error.message)));
els.copyKeplrDisclosurePubKey.addEventListener("click", () => copyKeplrDisclosurePubKey().catch(error => toast(error.message)));
els.refreshWalletBalance.addEventListener("click", () => refreshWalletBalance().catch(error => toast(error.message)));
els.scanKeplrNotes.addEventListener("click", () => scanKeplrNotes().catch(error => toast(error.message)));
els.myKeplrSpendableOnly.addEventListener("change", event => {
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
  els.veiledWithdrawAmount
].forEach(input => {
  input.addEventListener("input", updateAmountActionButtons);
});
els.veiledDisclosureAdvanced.addEventListener("change", renderTransferDisclosureAdvanced);
els.veiledDisclosureMode.addEventListener("change", renderTransferDisclosureAdvanced);
els.transferFromVeiled.addEventListener("click", transferFromVeiled);
els.withdrawFromVeiled.addEventListener("click", withdrawFromVeiled);
els.refreshAll.addEventListener("click", () => refreshHealth().catch(error => toast(error.message)));
els.refreshNotes.addEventListener("click", () => refreshNotes().catch(error => toast(error.message)));
els.refreshEvents.addEventListener("click", () => refreshEvents().catch(error => toast(error.message)));
els.decodeEventDisclosure.addEventListener("click", () => decodeSelectedEventDisclosure().catch(error => toast(error.message)));
if (els.refreshAuditorTransfers) {
  els.refreshAuditorTransfers.addEventListener("click", () => refreshAuditorTransfers().catch(error => toast(error.message)));
}
if (els.decodeAuditorTransfer) {
  els.decodeAuditorTransfer.addEventListener("click", () => decodeAuditorTransfer().catch(error => toast(error.message)));
}
els.closeNoticeModal.addEventListener("click", closeNoticeModal);
els.cancelTransferFlow.addEventListener("click", cancelTransferFlow);
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
