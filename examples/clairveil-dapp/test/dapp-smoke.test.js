import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const appSource = await readFile(new URL("../public/app.js", import.meta.url), "utf8");
const encryptedStoreSource = await readFile(new URL("../public/encrypted-note-store.js", import.meta.url), "utf8");
const encryptedReservationSource = await readFile(new URL("../public/encrypted-reservation-manager.js", import.meta.url), "utf8");
const encryptedOperationSource = await readFile(new URL("../public/encrypted-operation-store.js", import.meta.url), "utf8");
const depositFundingSource = await readFile(new URL("../public/deposit-funding.js", import.meta.url), "utf8");
const relayReconciliationSource = await readFile(new URL("../public/relay-withdraw-reconciliation.js", import.meta.url), "utf8");
const reservationRecoverySource = await readFile(new URL("../public/reservation-recovery.js", import.meta.url), "utf8");
const configSource = await readFile(new URL("../public/dapp-config.js", import.meta.url), "utf8");
const readmeSource = await readFile(new URL("../README.md", import.meta.url), "utf8");
const serverSource = await readFile(new URL("../server.js", import.meta.url), "utf8");
const htmlSource = await readFile(new URL("../public/index.html", import.meta.url), "utf8");
const cssSource = await readFile(new URL("../public/styles.css", import.meta.url), "utf8");

test("DApp keeps minimal-denom amount inputs as integer strings", () => {
  assert.match(appSource, /function amountInputValue/);
  assert.doesNotMatch(appSource, /Number\(input\.value/);
  assert.match(appSource, /BigInt\(raw\)/);
});

test("DApp disables value-moving actions for zero or invalid minimal-denom amounts", () => {
  assert.match(appSource, /function hasPositiveUclairInput/);
  assert.match(appSource, /amount <= 0n/);
  assert.match(appSource, /function updateAmountActionButtons/);
  assert.match(appSource, /sendFromKeplr\.disabled = !signerReady[\s\S]*!hasPositiveUclairInput\(els\.keplrSendAmount\)[\s\S]*!isSendRecipientForWallet\(els\.keplrSendRecipient\.value/);
  assert.match(appSource, /depositFromKeplr\.disabled = !signerReady[\s\S]*!depositProofReady\(\)[\s\S]*!hasPositiveUclairInput\(els\.keplrDepositAmount\)/);
  assert.match(appSource, /transferFromVeiled\.disabled = !veiledReady[\s\S]*!hasPositiveUclairInput\(els\.veiledTransferAmount\)/);
  assert.match(appSource, /withdrawFromVeiled\.disabled = !veiledReady[\s\S]*relayRecoveryBlocked[\s\S]*!hasPositiveUclairInput\(els\.veiledWithdrawAmount\)/);
  assert.doesNotMatch(appSource, /const reservationBlocked = state\.reservations\.retryBlocked/);
  assert.match(appSource, /keplrSendAmount,[\s\S]*keplrSendRecipient,[\s\S]*veiledWithdrawAmount[\s\S]*addEventListener\("input", updateAmountActionButtons\)/);
});

test("DApp exposes evidence-gated preparation recovery without globally blocking unreserved notes", () => {
  assert.match(htmlSource, /id="reservationRecovery"/);
  assert.match(htmlSource, /id="reservationRecoveryList"/);
  assert.match(htmlSource, /Only preparations with no broadcast or relay handoff evidence/);
  assert.match(cssSource, /\.reservation-recovery-item\s*\{/);
  assert.match(appSource, /function recoverReservationPreparation/);
  assert.match(appSource, /await scanKeplrNotes\(\{ quiet: true, throwOnError: true/);
  assert.match(appSource, /Every reserved nullifier must be explicitly unspent/);
  assert.match(appSource, /manager\.markManualReview/);
  assert.match(appSource, /manager\.resolveManualReview/);
  assert.match(appSource, /target: reservationStatuses\.ReplanRequired/);
  assert.match(appSource, /proofDiscarded: true/);
  assert.match(appSource, /wallet_owner_approved_replan: true/);
  assert.match(appSource, /summarizeReservationAvailableNotes/);
  assert.match(appSource, /reserved · \$\{reservation\.status\}/);
  assert.match(reservationRecoverySource, /broadcast_attempt_count/);
  assert.match(reservationRecoverySource, /relay_handed_off/);
  assert.match(reservationRecoverySource, /foreignLiveLease/);
});

test("DApp faucet sends the requested amount without minimum top-up", () => {
  assert.match(htmlSource, /Faucet amount/);
  assert.match(htmlSource, /CLAIR get from Alice's wallet/);
  assert.match(appSource, /get from \$\{localSignerLabel\(faucetSource\)\}'s wallet/);
  assert.match(appSource, /function connectedPublicRecipientAddress/);
  assert.match(appSource, /return state\.wallet\.account/);
  assert.match(appSource, /recipient = connectedPublicRecipientAddress\(\)/);
  assert.match(appSource, /recipient,\s*amount/);
  assert.match(appSource, /data\.recipientEvm \|\| recipient/);
  assert.match(appSource, /const localSigner = selectedLocalAccount\(\)\?\.name/);
  assert.doesNotMatch(htmlSource, /Fund amount/);
  assert.doesNotMatch(htmlSource, /Fund Wallet/);
  assert.match(htmlSource, /<button id="fundKeplr" type="button" disabled>Faucet<\/button>/);
  assert.match(appSource, /fundKeplr\.disabled = !serverFeature\("faucet"\) \|\| !signerReady/);
  assert.doesNotMatch(appSource, /fundKeplr\.disabled = !signerReady \|\| state\.activeWallet === "metamask"/);
  assert.match(serverSource, /function normalizeFaucetAmount/);
  assert.match(serverSource, /function sendEvmFaucet/);
  assert.match(serverSource, /import \{ JsonRpcProvider, Wallet \} from "ethers"/);
  assert.match(serverSource, /Wallet\.fromPhrase/);
  assert.match(serverSource, /new JsonRpcProvider\(config\.evmRpc\)/);
  assert.doesNotMatch(serverSource, /minimumFaucetAmount/);
  assert.doesNotMatch(serverSource, /requested < .*minimum/);
  assert.match(serverSource, /funded: denomCoin\(requested\)/);
  assert.match(serverSource, /faucet amount must be greater than 0\$\{config\.denom\}/);
});

test("DApp denomination labels render as input suffixes, not button-like chips", () => {
  assert.match(htmlSource, /class="amount-control"/);
  assert.doesNotMatch(htmlSource, /<\/label>\s*<span class="denom">/);
  assert.match(cssSource, /\.amount-control\s*\{/);
  assert.match(cssSource, /\.amount-control input\s*\{/);
  assert.match(cssSource, /\.denom\s*\{/);
  assert.match(cssSource, /background: transparent/);
  assert.match(cssSource, /border: 0/);
  assert.match(cssSource, /border-radius: 0/);
  assert.match(cssSource, /position: absolute/);
  assert.match(cssSource, /pointer-events: none/);
});

test("DApp renders one combined wallet session panel", () => {
  assert.match(htmlSource, /<h2>Wallet Session<\/h2>/);
  assert.doesNotMatch(htmlSource, /EVM Session \(MetaMask\)/);
  assert.doesNotMatch(htmlSource, /COSMOS Session \(Keplr\)/);
  assert.match(htmlSource, /class="panel wallet-session-panel"/);
  assert.match(htmlSource, /class="facts wallet-session-facts"/);
  assert.match(htmlSource, /<dt>Account<\/dt>/);
  assert.match(htmlSource, /id="copyWalletAccount"/);
  assert.match(htmlSource, /<span id="walletAccount">Not connected<\/span>/);
  assert.doesNotMatch(htmlSource, /MetaMask account/);
  assert.doesNotMatch(htmlSource, /Keplr account/);
  assert.match(htmlSource, /id="signSession"/);
  assert.doesNotMatch(htmlSource, /id="signKeplrSession"/);
  assert.match(htmlSource, /id="disconnectWallet"/);
  assert.doesNotMatch(htmlSource, /id="keplrStatus"/);
  assert.match(appSource, /activeWallet: ""/);
  assert.match(appSource, /function renderWalletSession/);
  assert.match(appSource, /function currentWalletAccountForCopy/);
  assert.match(appSource, /function copyWalletAccount/);
  assert.match(appSource, /copyWalletAccount\.disabled = !currentWalletAccountForCopy\(\)/);
  assert.match(appSource, /navigator\.clipboard\.writeText\(account\)/);
  assert.match(appSource, /function canConnectWallet/);
  assert.match(appSource, /els\.connectWallet\.hidden = connected \|\| walletKind !== "metamask"/);
  assert.match(appSource, /els\.connectKeplr\.hidden = connected \|\| walletKind !== "keplr"/);
  assert.match(appSource, /els\.disconnectWallet\.hidden = !connected/);
  assert.match(cssSource, /\.wallet-session-panel\s*\{/);
  assert.match(cssSource, /\.wallet-session-facts\s*\{/);
  assert.match(cssSource, /\.account-copy\s*\{/);
});

test("DApp exposes chain profiles and filters wallet connect buttons by chain", () => {
  assert.match(htmlSource, /DApp chain info/);
  assert.match(htmlSource, /id="dappChainSelect"/);
  assert.match(htmlSource, /id="dappChainHint"/);
  assert.match(serverSource, /function dappChainProfiles/);
  assert.match(serverSource, /id: "clairveil-local"/);
  assert.match(serverSource, /wallet: "keplr"/);
  assert.match(serverSource, /id: "evm-local"/);
  assert.match(serverSource, /wallet: "metamask"/);
  assert.match(serverSource, /return \[validateBrowserWalletProfile\(isEvmTransport\(\) \? evmProfile : clairveilProfile\)\]/);
  assert.match(configSource, /chainProfiles: \[clairveilProfile\]/);
  assert.doesNotMatch(configSource, /^const evmProfile/m);
  assert.doesNotMatch(configSource, /^\s*evmChainId:/m);
  assert.match(readmeSource, /EVM static profile example/);
  assert.match(readmeSource, /const myEvmProfile = \{/);
  assert.match(readmeSource, /chainProfiles: \[clairveilProfile, myEvmProfile\]/);
  assert.match(serverSource, /const chainProfiles = dappChainProfiles\(\)/);
  assert.match(appSource, /function activeChainProfile/);
  assert.match(serverSource, /CLAIRVEIL_COSMOS_REST_ENDPOINTS/);
  assert.match(serverSource, /CLAIRVEIL_EVM_HOST_REST_ENDPOINTS/);
  assert.match(serverSource, /restEndpoints: config\.cosmosRestEndpoints/);
  assert.match(appSource, /function profileRestEndpoints/);
  assert.match(appSource, /function browserWalletProfile/);
  assert.match(appSource, /normalizeBrowserProfileEndpoints\(resolved/);
  assert.match(appSource, /return browserWalletProfile\(activeChainProfile\(\)\)\?\.keplrChainInfo/);
  assert.match(appSource, /function selectNoteScanEndpoint/);
  assert.match(appSource, /function activeWalletKind/);
  assert.match(appSource, /function selectedProfileMatchesServer/);
  assert.match(appSource, /function activeServerAccounts/);
  assert.match(appSource, /function renderDappChainSelect/);
  assert.match(appSource, /function selectDappChainProfile/);
  assert.match(appSource, /selectDappChainProfile[\s\S]*renderAccounts\(\)/);
  assert.match(appSource, /els\.connectWallet\.disabled = !profileReady/);
  assert.match(appSource, /els\.connectKeplr\.disabled = !profileReady/);
  assert.match(cssSource, /\.chain-picker\s*\{/);
});

test("DApp hides local-only panels unless the server enables local test features", () => {
  assert.match(htmlSource, /id="modeBadge" class="mode-badge">Local Note Test Web/);
  assert.match(cssSource, /\.mode-badge\s*\{/);
  assert.match(cssSource, /\.mode-badge\.public-mode\s*\{/);
  assert.match(serverSource, /function envFlag/);
  assert.match(serverSource, /function resolveLocalTestMode/);
  assert.match(serverSource, /CLAIRVEIL_DAPP_LOCAL_TEST_MODE", true/);
  assert.doesNotMatch(serverSource, /function isLocalEndpoint/);
  assert.match(serverSource, /function assertLocalTestBackendAllowed/);
  assert.match(serverSource, /function serverFeaturesForRequest\(req\)/);
  assert.match(serverSource, /localSigners: localSignerAdmin/);
  assert.match(serverSource, /localSignerSetup: localSignerMutation/);
  assert.match(serverSource, /faucet: localSignerMutation/);
  assert.match(serverSource, /auditorAdmin: localSignerAdmin/);
  assert.match(serverSource, /function publicConfig\(req\)/);
  assert.match(serverSource, /modeLabel: config\.localTestMode \? "Local Note Test Web" : "Public Node DApp"/);
  assert.match(appSource, /function serverFeature/);
  assert.match(appSource, /function renderServerFeatureVisibility/);
  assert.match(appSource, /modeBadge\.textContent/);
  assert.match(appSource, /modeBadge\.classList\.toggle\("public-mode"/);
  assert.match(appSource, /localSignerPanel\.hidden = !localSigners/);
  assert.match(appSource, /faucetRow\.hidden = !faucet/);
  assert.match(appSource, /auditorSection\.hidden = !auditorAdmin/);
  assert.match(appSource, /!data\.config\?\.serverFeatures\?\.localSignerSetup/);
  assert.match(appSource, /serverFeature\("faucet"\)/);
});

test("DApp keeps EVM public send 0x-only without self-wallet suggestions", () => {
  assert.match(appSource, /import \{ bech32AddressToEvm \} from "clairveiljs\/evm"/);
  assert.match(appSource, /function connectedWalletAddressSuggestions/);
  assert.match(appSource, /function activeServerAccounts\(\) \{[\s\S]*selectedProfileMatchesServer\(\) \? state\.accounts : \[\]/);
  assert.match(appSource, /const accounts = activeServerAccounts\(\);[\s\S]*const preferred = accounts\.filter/);
  assert.match(appSource, /els\.accountSelect\.disabled = !accounts\.length/);
  assert.match(appSource, /if \(!accounts\.length\) \{[\s\S]*els\.keplrSendRecipient\.value = ""/);
  assert.match(appSource, /function isEvmAddress/);
  assert.match(appSource, /function isSendRecipientForWallet/);
  assert.match(appSource, /function activeTransparentAddressFormat/);
  assert.match(appSource, /function isEvmTransparentMode/);
  assert.match(appSource, /keplrSendRecipient\.placeholder = transparentFormat === "evm" \? "0x\.\.\."/);
  assert.match(appSource, /veiledWithdrawRecipient\.placeholder = transparentFormat === "evm" \? "0x\.\.\."/);
  assert.match(appSource, /format: transparentFormat/);
  assert.match(appSource, /includeWallet: true/);
  assert.match(appSource, /name: "My wallet"/);
  assert.doesNotMatch(appSource, /name: "My EVM wallet"/);
  assert.match(appSource, /\.\.\.connectedWalletAddressSuggestions\(config\)/);
  assert.match(appSource, /bech32AddressToEvm\(account\.transparentAddress \|\| ""\)/);
  assert.match(appSource, /config\.format === "evm" && !isEvmAddress\(entry\.address\)/);
  assert.match(appSource, /EVM send recipient must be a 0x address/);
  assert.match(appSource, /const seenAddresses = new Set\(\)/);
  assert.match(appSource, /function transparentDisplayAddressFor/);
  assert.match(appSource, /selectedTransparentAddress = transparentDisplayAddressFor/);
  assert.doesNotMatch(appSource, /function hostAccountPrefix/);
  assert.doesNotMatch(appSource, /hostAccountPrefix/);
  assert.doesNotMatch(appSource, /evmAddressToBech32/);
  assert.match(appSource, /method: "eth_getBalance"/);
});

test("DApp uses the npm ClairveilJS browser client for public wallet and privacy flows", () => {
  assert.match(appSource, /createClairveilBrowserDappClient,[\s\S]*validateClairveilWebClientConfig[\s\S]*from "clairveiljs\/browser-dapp"/);
  assert.match(appSource, /import \{ EncryptedLocalStorageNoteStore \} from "\.\/encrypted-note-store\.js"/);
  assert.doesNotMatch(appSource, /allowPlaintext: true/);
  assert.match(appSource, /function clairveilBrowserClient/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.fetchPrivacyEvents\(\)/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.fetchAuditableTransfers\(\)/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareDeposit/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareTransfer/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareWithdraw/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.scanWalletNotes/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeUserDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeSelfViewDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.signDirectAndBroadcast/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.waitForEvmTransaction/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.sendEvmTransaction/);
  assert.match(appSource, /function defaultNoteScanCursor/);
  assert.match(appSource, /function noteScanRequestOptions/);
  assert.match(appSource, /function applyNoteScanResult/);
  assert.match(appSource, /scanSource: "privacy_scan"/);
  assert.match(appSource, /state\.keplr\.noteScanResumeOptions/);
  assert.match(appSource, /const store = await currentNoteStore\(\)/);
  assert.match(appSource, /await clearCurrentNoteStore\(\)/);
  assert.doesNotMatch(appSource, /eventTypes: \["deposit", "shielded_transfer"\]/);
  assert.match(appSource, /scanWalletNotes\(privacyRequest\(\{\s*\.\.\.scanOptions,[\s\S]*noteStore: store,[\s\S]*includeFoundNotes: true/);
  assert.match(appSource, /more events queued/);
  assert.match(appSource, /scan: \{ scanSource: "privacy_scan", limit: 200, maxPages: 1000 \}/);
  assert.match(appSource, /function browserProverUrl/);
  assert.match(appSource, /state\.config\?\.serverBacked && serverFeature\("proverProxy"\) && configured/);
  assert.match(appSource, /return window\.location\.origin\.replace/);
  assert.match(serverSource, /function handleProverProxy/);
  assert.match(serverSource, /function proverProxyTarget/);
  assert.match(serverSource, /proverProxyEnabled: localTestMode/);
  assert.match(serverSource, /function acquireProverProxyCapacity/);
  assert.match(serverSource, /proverProxyMaxInFlight/);
  assert.match(serverSource, /proverProxyRateLimitMax/);
  assert.match(serverSource, /function proverProxyAccessAllowed/);
  assert.doesNotMatch(serverSource, /"access-control-allow-origin": "\*"/);
  assert.match(serverSource, /proverProxyTarget\(url\.pathname\)/);
  assert.match(serverSource, /new URL\(pathname, config\.proverUrl\.replace/);
  assert.match(appSource, /function browserDepositProofUrl/);
  assert.match(appSource, /function configuredDepositProofProvider/);
  assert.match(appSource, /const depositProofProvider = configuredDepositProofProvider\(\)/);
  assert.match(appSource, /depositProofProvider,/);
  assert.match(appSource, /client\.assertProtocolPreflight\(baseDenom\(\)\)/);
  assert.match(appSource, /client\.queryReserve\(baseDenom\(\)\)/);
  assert.match(appSource, /refreshEvents\(\{ allowFailure: true \}\)/);
  assert.match(appSource, /Browser cannot reach the selected chain REST\/RPC endpoint/);
  assert.match(appSource, /state\.privacyEvents\.loadError/);
  assert.doesNotMatch(appSource, /\/api\/tx\//);
  assert.doesNotMatch(appSource, /\/api\/keplr\/privacy/);
  assert.doesNotMatch(appSource, /\/api\/evm\/privacy/);
  assert.doesNotMatch(appSource, /\/sdk\/clairveiljs/);
  assert.doesNotMatch(appSource, /buildPreparedTransferPayload/);
  assert.doesNotMatch(appSource, /buildPreparedWithdrawProverPayload/);
  assert.doesNotMatch(appSource, /planTransferNotes/);
  assert.doesNotMatch(appSource, /planWithdrawNotes/);
  assert.doesNotMatch(appSource, /createHttpProverAdapter/);
  assert.doesNotMatch(serverSource, /function serveClairveiljsStatic/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/events"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/auditor\/transfers"/);
  assert.doesNotMatch(serverSource, /createHttpProverAdapter/);
  assert.doesNotMatch(serverSource, /buildTransferMessage/);
  assert.doesNotMatch(serverSource, /buildWithdrawMessage/);
  assert.doesNotMatch(serverSource, /planTransferNotes/);
  assert.doesNotMatch(serverSource, /planWithdrawNotes/);
  assert.doesNotMatch(serverSource, /prepareEvmTransfer/);
  assert.doesNotMatch(serverSource, /prepareEvmWithdraw/);
});

test("DApp persists encrypted reservations and keeps unknown broadcasts fail closed", () => {
  assert.match(appSource, /createEncryptedBrowserReservationManager/);
  assert.match(encryptedReservationSource, /createBrowserReservationStore/);
  assert.match(encryptedReservationSource, /createNoteReservationManager/);
  assert.match(encryptedReservationSource, /AES-GCM/);
  assert.match(encryptedReservationSource, /requireLocks: true/);
  assert.match(encryptedReservationSource, /RESERVATION_STATE_CORRUPT/);
  assert.doesNotMatch(encryptedReservationSource, /unsafeAllowPlaintext/);

  assert.match(appSource, /prepareTransfer\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /prepareWithdraw\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /prepareRelayWithdraw\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /preparedReservationBinding\(data\)/);
  assert.match(appSource, /sendEvmTransaction\(data\.transaction,[\s\S]*reservationBinding: broadcastOptions/);
  assert.match(appSource, /signDirectAndBroadcast\(data\.signDoc, broadcastOptions\)/);
  assert.match(appSource, /recordRelayHandoff\(reservationIDs/);

  assert.match(appSource, /if \(!broadcast\?\.receipt\) \{[\s\S]*markUnknown\(reservationIDs,[\s\S]*fromStatus: reservationStatuses\.Submitted[\s\S]*unknown: true/);
  assert.match(appSource, /result\.unknown \? onUnknown/);
  assert.match(appSource, /nullifierUnspentConfirmed: true/);
  assert.match(appSource, /txAbsentOrFailedConfirmed: true/);
  assert.match(appSource, /txHashChecked: txHash/);
  assert.match(appSource, /same request|다시 전송하지 마세요/);
  assert.match(htmlSource, /id="reservationState"/);
  assert.match(htmlSource, /id="reconcileReservations"/);
});

test("DApp reconciles spent transfers only with matching operation evidence", () => {
  assert.match(appSource, /function reservationRequiresOperationEvidence/);
  assert.match(appSource, /function transferEventForOperation/);
  assert.match(appSource, /event\?\.event_type !== "shielded_transfer"/);
  assert.match(appSource, /eventAttribute\(event, "commitment_1"\)/);
  assert.match(appSource, /eventAttribute\(event, "audit_disclosure_digest"\)/);
  assert.match(appSource, /manager\.store\.listReservations\(\{ ownerKeyId: manager\.ownerKeyId \}\)/);
  assert.match(appSource, /operationStatuses\.ConflictSpent/);
  assert.match(appSource, /operationEventForReservations\(spentRecords, notesByLookupKey\)/);
  assert.match(appSource, /findPrivacyEventByTxHash/);
  assert.match(appSource, /if \(!lookup\.complete\) continue/);
  assert.match(appSource, /operationSuccessEvidence: evidenceByLookupKey\.get\(lookupKey\)/);
  assert.match(appSource, /operationReconciliationStatus\(record\) !== operationStatuses\.Succeeded/);
  assert.match(appSource, /OPERATION_RECONCILIATION_REQUIRED/);
  assert.match(appSource, /state\.reservations\.unresolved\?\.length > 0/);
});

test("DApp keeps prepared reservation leases alive across wallet and relay waits", () => {
  assert.match(appSource, /reservationHeartbeatIntervalMs/);
  assert.match(appSource, /async function withPreparedReservationHeartbeat/);
  assert.match(appSource, /manager\.heartbeatLease\(reservationIDs, \{ leaseToken \}\)/);
  assert.match(appSource, /withPreparedReservationHeartbeat\(prepared, \(\) =>/);
  assert.match(appSource, /const broadcast = await withPreparedReservationHeartbeat\(data/);
  assert.match(appSource, /function startRelayReservationHeartbeat/);
  assert.match(appSource, /relayReservationHeartbeatTimer = globalThis\.setInterval/);
  assert.match(appSource, /if \(fullyConfirmed\) \{[\s\S]*stopRelayReservationHeartbeat\(\)/);
  assert.match(appSource, /RESERVATION_HEARTBEAT_FAILED/);
});

test("DApp blocks unresolved public EVM retries and exposes tx reconciliation", () => {
  assert.match(appSource, /sendPending = \["submitted", "unknown", "checking"\]/);
  assert.match(appSource, /depositPending = \["submitted", "unknown", "checking"\]/);
  assert.match(appSource, /async function reconcilePublicEvmTransaction/);
  assert.match(appSource, /failureConfirmed = evmReceiptHasFailed/);
  assert.match(appSource, /same request remains blocked|같은 요청은 계속 차단됩니다/);
  assert.match(htmlSource, /id="reconcileKeplrSend"/);
  assert.match(htmlSource, /id="reconcileKeplrDeposit"/);
  assert.match(appSource, /hydratePublicPendingTransactions/);
  assert.match(appSource, /persistPublicPendingTransactions/);
  assert.match(htmlSource, /id="clearPublicPendingState"/);
});

test("DApp marks stale note inventory as unconfirmed and blocks private spends", () => {
  assert.match(appSource, /noteInventoryTrusted = state\.keplr\.noteSyncStatus === "synced"/);
  assert.match(appSource, /Cached · not confirmed/);
  assert.match(appSource, /Cached notes are shown for recovery only/);
  assert.match(appSource, /transferFromVeiled\.disabled = !veiledReady[\s\S]*!noteInventoryTrusted/);
  assert.match(appSource, /withdrawFromVeiled\.disabled = !veiledReady[\s\S]*!noteInventoryTrusted/);
});

test("DApp planner UX uses structured API errors instead of message parsing", () => {
  assert.match(appSource, /class ApiError extends Error/);
  assert.match(appSource, /error\?\.code === "EXACT_NOTE_REQUIRED"/);
  assert.match(appSource, /error\?\.code === "ZERO_DUMMY_REQUIRED"/);
  assert.match(appSource, /allowPlanStep: true/);
  assert.match(appSource, /onSelfMergeNeeded/);
  assert.doesNotMatch(appSource, /includes\("withdraw requires one exact-match note"\)/);
  assert.doesNotMatch(appSource, /includes\("transfer needs a second spendable input note"\)/);
});

test("DApp shows current transferable max planner fact only for note merge steps", () => {
  assert.match(appSource, /const currentMaxRow = els\.transferPlannerCurrentMax\.closest\("div"\)/);
  assert.match(appSource, /currentMaxRow\.hidden = !hasCurrentMax/);
  assert.match(appSource, /function plannerCurrentTransferMaxForNoteMerge/);
  assert.match(appSource, /facts\.selectedInputTotalValue/);
  assert.match(appSource, /currentTransferMaxValue >= requestedValue/);
  assert.match(appSource, /currentMax: plannerCurrentTransferMaxForNoteMerge\(data, amount\)/);
  assert.match(appSource, /function plannerCurrentExactNoteMaxForWithdraw/);
  assert.match(appSource, /facts\.currentMaxNoteValue/);
  assert.match(appSource, /currentExactNoteMaxValue >= requestedValue/);
  assert.match(appSource, /currentMax: plannerCurrentExactNoteMaxForWithdraw\(data, amount\)/);
  assert.match(appSource, /onFinalExactTransfer: data =>/);
  assert.doesNotMatch(appSource, /currentMax: zeroCoinText\(\)/);
  assert.doesNotMatch(appSource, /currentMax: error\.prepared/);
  assert.doesNotMatch(appSource, /currentMax: amount/);
});

test("DApp exposes none, public, and recipient-encrypted disclosure modes", () => {
  assert.match(htmlSource, /id="veiledDisclosureMode"/);
  assert.match(htmlSource, /value="none"/);
  assert.match(htmlSource, /value="public"/);
  assert.match(htmlSource, /value="recipient-encrypted"/);
  assert.match(appSource, /disclosureMode === "none"/);
  assert.match(appSource, /disclosureMode === "public"/);
  assert.match(appSource, /disclosureMode: "recipient-encrypted"/);
  assert.match(appSource, /disclosurePubKeyHex/);
});

test("DApp stores note recovery state encrypted and exposes endpoint and rollback recovery", () => {
  assert.match(encryptedStoreSource, /AES-GCM/);
  assert.match(encryptedStoreSource, /HKDF/);
  assert.match(encryptedStoreSource, /NOTE_CACHE_CORRUPT/);
  assert.match(appSource, /notes-encrypted/);
  assert.match(appSource, /Legacy plaintext cache removed/);
  assert.match(appSource, /function resetAndRescanNotes/);
  assert.match(appSource, /scanKeplrNotes\(\{ reset: true, throwOnError: true \}\)/);
  assert.match(appSource, /function rollbackAndRescanNotes/);
  assert.match(appSource, /store\.rollbackToHeight\(height\)/);
  assert.match(appSource, /function completeInitialPrivacySetup/);
  assert.match(appSource, /maxPages: 1000/);
  assert.match(htmlSource, /id="backupNoteCache"/);
  assert.match(htmlSource, /id="resetRescanNotes"/);
  assert.match(htmlSource, /id="noteScanEndpoint"/);
  assert.match(htmlSource, /id="noteRollbackHeight"/);
  assert.match(htmlSource, /id="rollbackRescanNotes"/);
  assert.match(htmlSource, /id="noteSyncState"/);
});

test("DApp verifies transparent deposit funding and surfaces a non-zero fee budget", () => {
  assert.match(appSource, /function estimateDepositFeeBeforeProof/);
  assert.match(appSource, /function assertDepositFunding/);
  assert.match(appSource, /transparentBalanceAmount/);
  assert.match(appSource, /evmNativeBalance/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.getBalances\(state\.keplr\.account\)/);
  assert.match(depositFundingSource, /Insufficient transparent balance/);
  assert.match(depositFundingSource, /Insufficient EVM gas balance/);
  assert.match(appSource, /keplrDirectSignOptions\(options\)/);
  assert.match(appSource, /cosmosGasFeeEstimate/);
  assert.doesNotMatch(appSource, /0 \$\{baseDenom\(\)\} encoded/);
});

test("DApp separates deposit inclusion from exact note recovery", () => {
  assert.match(appSource, /function recoverDepositNote/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.confirmDeposit/);
  assert.match(appSource, /expectedCommitment: prepared\.noteCommitmentHex/);
  assert.match(appSource, /expectedEncryptedNote: prepared\.encryptedNoteHex/);
  assert.match(appSource, /Included · recovery pending/);
  assert.match(appSource, /Deposit 및 note 복구 완료/);
  assert.match(htmlSource, /id="keplrDepositRecovery"/);
});

test("DApp confirms chain-bound intent details and supports self-view opt-out", () => {
  assert.match(appSource, /function fetchLatestChainBlockTimeUnix/);
  assert.match(appSource, /cosmos\/base\/tendermint\/v1beta1\/blocks\/latest/);
  assert.match(appSource, /expiresAtUnix: chainNowUnix \+ 1800/);
  assert.match(appSource, /disableSelfViewDisclosure/);
  assert.match(htmlSource, /id="includeSelfViewDisclosure"/);
  for (const id of ["reviewChain", "reviewRecipient", "reviewAmount", "reviewChangeEffect", "reviewDisclosure", "reviewSelfView", "reviewExpiry"]) {
    assert.match(htmlSource, new RegExp(`id="${id}"`));
  }
  assert.match(appSource, /function requestPreparedTransferConfirmation/);
  assert.match(appSource, /function preparedTransferChangeEffect/);
  assert.match(appSource, /withPreparedReservationHeartbeat\(finalData, \(\) => \([\s\S]*requestPreparedTransferConfirmation/);
  assert.match(appSource, /discardPreparedReservation\(finalData/);
  assert.match(appSource, /expiresAtUnix: options\.expiresAtUnix/);
  assert.match(appSource, /chainNowUnix: options\.chainNowUnix/);
});

test("DApp offers relay handoff and cancellable same-prover retries", () => {
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareRelayWithdraw/);
  assert.match(relayReconciliationSource, /relayWithdrawHandoffVersion = "v2"/);
  assert.match(relayReconciliationSource, /schema_version: relayWithdrawHandoffVersion/);
  assert.match(relayReconciliationSource, /handoff_version: relayWithdrawHandoffVersion/);
  assert.match(relayReconciliationSource, /request: \{[\s\S]*version: relayWithdrawHandoffVersion/);
  assert.doesNotMatch(appSource, /clairveil-relay-withdraw-handoff-v1/);
  assert.match(appSource, /new AbortController\(\)/);
  assert.match(appSource, /signal: options\.signal/);
  assert.match(appSource, /transferFlowState\.controller\.abort\(\)/);
  assert.match(appSource, /retry: \(\) => transferFromVeiled\(\)/);
  assert.match(appSource, /retry: \(\) => withdrawFromVeiled\(\)/);
  assert.match(htmlSource, /id="withdrawMode"/);
  assert.match(htmlSource, /id="relayWithdrawJson"/);
  assert.match(htmlSource, /id="retryTransferFlow"/);
  assert.match(appSource, /async function reconcileRelayWithdrawResult/);
  assert.match(appSource, /recoverExpiredRelayWithdraw/);
  assert.match(appSource, /manager\.resolveManualReview/);
  assert.match(appSource, /async function quarantineRelayWithdrawOperation/);
  const relayReconcileSource = appSource.slice(
    appSource.indexOf("async function reconcileRelayWithdrawResult"),
    appSource.indexOf("async function explicitlyUnspentReservationIDs")
  );
  assert.ok(
    relayReconcileSource.indexOf("assertRelayWithdrawTransactionMatches") <
      relayReconcileSource.indexOf("scanKeplrNotes"),
    "relay transaction binding must be checked before spent-note reconciliation"
  );
  assert.match(relayReconcileSource, /spentConfirmed && \(!check\.included \|\| check\.failed\)/);
  assert.match(relayReconcileSource, /relay_spent_without_successful_bound_transaction/);
  assert.match(relayReconciliationSource, /assertRelayWithdrawTransactionMatches/);
  assert.match(relayReconciliationSource, /included Cosmos relayer transaction must contain exactly one MsgWithdraw/);
  assert.match(relayReconciliationSource, /"calldata"/);
  assert.match(appSource, /Checking tx result first, then nullifier spent state/);
  assert.match(htmlSource, /id="relayWithdrawTxHash"/);
  assert.match(htmlSource, /id="reconcileRelayWithdraw"/);
  assert.match(htmlSource, /user shielded secret/);
  assert.match(htmlSource, /privacy-sensitive data/);
  assert.match(htmlSource, /Recipient, chain, expiry는 변경할 수 없습니다/);
  assert.match(htmlSource, /local cancel이나 reservation release는 payload를 무효화하지 않으며/);
  assert.match(encryptedOperationSource, /AES-GCM/);
  assert.match(encryptedOperationSource, /OPERATION_STATE_CORRUPT/);
  assert.match(appSource, /persistRelayWithdrawRecovery/);
  assert.match(appSource, /hydrateRelayWithdrawRecovery/);
});

test("DApp discloses mandatory audit visibility before transfer confirmation", () => {
  assert.match(appSource, /Mandatory audit: full/);
  assert.match(htmlSource, /Audit disclosure는 모든 privacy transfer에 항상 포함/);
  assert.match(htmlSource, /amount·sender·recipient를 복호화/);
});

test("DApp renders public disclosure reports without recipient-only branching", () => {
  assert.match(appSource, /renderEventDisclosureReport/);
  assert.match(appSource, /summary\.delivery/);
  assert.match(appSource, /function isPublicDisclosureEvent/);
  assert.match(appSource, /function canDecodeEventDisclosure/);
  assert.match(appSource, /if \(isPublicDisclosureEvent\(event\)\) return true/);
  assert.match(appSource, /decodeSelectedEventDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeUserDisclosure/);
  assert.match(appSource, /function canDecodeSelfViewDisclosure/);
  assert.match(appSource, /self_view_disclosure_payload/);
  assert.match(appSource, /decodeSelectedSelfViewDisclosure/);
  assert.match(htmlSource, /id="decodeSelfViewDisclosure"/);
  assert.doesNotMatch(appSource, /\/api\/keplr\/privacy\/disclosure\/decode/);
  assert.match(appSource, /disclosureViewModel/);
  assert.match(appSource, /Plaintext was discarded/);
  assert.match(htmlSource, /id="disclosureSourceTxHash"/);
  assert.match(htmlSource, /id="disclosureSourceEventJson"/);
  assert.match(appSource, /decodeDisclosureSource/);
  for (const id of [
    "eventDisclosurePlane",
    "eventDisclosurePolicy",
    "eventDisclosureOutputIndex",
    "eventDisclosureCommitment",
    "eventDisclosureDigest",
    "eventDisclosureVerified",
    "auditorPlanePolicy",
    "auditorOutputIndex",
    "auditorCommitment"
  ]) {
    assert.match(htmlSource, new RegExp(`id="${id}"`));
  }
  assert.match(appSource, /view\.outputIndex/);
  assert.match(appSource, /view\.commitmentHex/);
  assert.match(appSource, /view\.digestHex/);
  assert.match(appSource, /function renderEventDisclosureError/);
  assert.match(appSource, /eventDisclosureVerified\.textContent = "false"/);
});

test("DApp gates privacy actions on preflight and surfaces recovery UX", () => {
  assert.match(appSource, /noteInventoryTrusted = state\.keplr\.noteSyncStatus === "synced" && protocolReady/);
  assert.match(appSource, /depositFromKeplr\.disabled = !signerReady[\s\S]*!protocolReady/);
  assert.match(appSource, /scanKeplrNotes\.disabled = [^;]*!state\.protocol\.ready/);
  assert.match(htmlSource, /id="copyKeplrShieldedAddress"/);
  assert.match(htmlSource, /id="keplrDepositNetworkFee"/);
  assert.match(htmlSource, /id="proverPrivacyWarning"/);
  assert.match(appSource, /updateDepositNetworkFee/);
});

test("DApp reports withdraw nullifier and transparent receive evidence separately", () => {
  assert.match(htmlSource, /id="keplrWithdrawNullifier"/);
  assert.match(htmlSource, /id="keplrWithdrawReceive"/);
  assert.match(appSource, /function setWithdrawEvidence/);
  assert.match(appSource, /function confirmWithdrawEvidence/);
  assert.match(appSource, /Spent · confirmed by note reconciliation/);
  assert.match(appSource, /Received · intended transparent output confirmed/);
  assert.match(appSource, /Unknown · reconcile before retry/);
  assert.match(appSource, /const fullyConfirmed = spentConfirmed && receiveConfirmed/);
});

test("DApp server does not own wallet privacy preparation routes", () => {
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/keplr\/privacy-deposit\/sign-doc"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/keplr\/privacy-transfer\/sign-doc"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/keplr\/privacy-withdraw\/sign-doc"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/evm\/privacy-deposit\/transaction"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/evm\/privacy-transfer\/transaction"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/evm\/privacy-withdraw\/transaction"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/keplr\/privacy\/notes"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/keplr\/privacy\/disclosure\/decode"/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/tx\/keplr\/broadcast"/);
});

test("DApp server keeps only local helper responsibilities", () => {
  assert.match(serverSource, /evmDefaultSignerAccounts/);
  assert.match(serverSource, /function ensureLocalSigners/);
  assert.match(serverSource, /\/api\/local-signers\/ensure/);
  assert.match(serverSource, /allowLanSigning: process\.env\.CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING === "1"/);
  assert.match(serverSource, /allowLanAdmin: process\.env\.CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN === "1"/);
  assert.match(serverSource, /accountPrefix: process\.env\.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX \?\? "clair"/);
  assert.doesNotMatch(serverSource, /hostAccountPrefix/);
  assert.doesNotMatch(serverSource, /CLAIRVEIL_EVM_ACCOUNT_PREFIX/);
  assert.match(serverSource, /function queryEvmNativeBalance/);
  assert.match(serverSource, /eth_getBalance/);
  assert.match(serverSource, /function assertSignerMutationAllowed/);
  assert.match(serverSource, /function assertLocalAdminAccessAllowed/);
  assert.match(serverSource, /\/api\/local-signers\/ensure"\) \{\s*assertLocalTestBackendAllowed\("local signer setup"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/faucet"\) \{\s*assertLocalTestBackendAllowed\("faucet"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/deposit"\) \{\s*assertLocalTestBackendAllowed\("local CLI deposit"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/auditor\/test-scalar"\) \{\s*assertLocalTestBackendAllowed\("auditor test scalar"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /\/api\/auditor\/decode"\) \{\s*assertLocalTestBackendAllowed\("auditor disclosure decode"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /local wallet show-address"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /local wallet note scan"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(appSource, /function ensureLocalSignersIfNeeded/);
  assert.match(appSource, /error\?\.statusCode !== 403/);
  assert.match(appSource, /Create accounts on the server machine first/);
  assert.match(appSource, /CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1/);
  assert.match(appSource, /accounts: \[\]/);
  assert.doesNotMatch(serverSource, /\/api\/evm\/account/);
  assert.doesNotMatch(serverSource, /\/api\/tx\/evm\/bank-send\/transaction/);
  assert.match(appSource, /evmNativeSendTransaction/);
  assert.match(appSource, /eth_sendTransaction/);
  assert.match(appSource, /walletType: "evm"/);
});

test("DApp shows a send result confirmation before refresh side effects", () => {
  assert.match(appSource, /function showSendResult/);
  assert.match(appSource, /title: "Send 요청됨"/);
  assert.match(appSource, /title: "Send 실패"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "MetaMask"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "Keplr"/);
  assert.match(appSource, /els\.keplrTxState\.textContent = "Send submitted"/);
  assert.match(appSource, /watchEvmBroadcast\(broadcast/);
  assert.match(appSource, /Promise\.allSettled\(\[refreshWalletBalance\(\), refreshBlockEvents\(\)\]\)/);
  assert.doesNotMatch(appSource, /toast\("MetaMask send included"\)/);
  assert.doesNotMatch(appSource, /toast\("Keplr send included"\)/);
  assert.match(cssSource, /#noticeMessage\s*\{[\s\S]*white-space: pre-wrap/);
});

test("DApp submits final MetaMask transactions before waiting for receipt", () => {
  assert.match(appSource, /async function submitEvmTransaction/);
  assert.match(appSource, /async function waitForEvmTransaction/);
  assert.match(appSource, /async function sendEvmTransaction\(transaction, \{[\s\S]*waitForReceipt = false,[\s\S]*reservationBinding = \{\}/);
  assert.match(appSource, /pending: true/);
  assert.match(appSource, /waitPromise/);
  assert.match(appSource, /broadcast\?\.pending && txHash/);
  assert.match(appSource, /Deposit 제출됨/);
  assert.match(appSource, /Transfer submitted/);
  assert.match(appSource, /트랜스퍼 요청이 제출되었습니다/);
  assert.match(appSource, /Withdraw submitted/);
  assert.match(appSource, /Withdraw 요청이 제출되었습니다/);
  assert.match(appSource, /zero helper note", \{[\s\S]*waitForEvmReceipt: true/);
  assert.match(appSource, /self transaction", \{ waitForEvmReceipt: true \}/);
});

test("DApp forces MetaMask onto the configured EVM chain", () => {
  assert.match(serverSource, /evmChainId: normalizeEvmChainId/);
  assert.match(serverSource, /evmChainName:/);
  assert.match(appSource, /evmChainId: resolved\?\.evmChainId \|\| state\.config\?\.evmChainId/);
  assert.match(appSource, /function ensureMetaMaskChain/);
  assert.match(appSource, /wallet_switchEthereumChain/);
  assert.match(appSource, /wallet_addEthereumChain/);
  assert.match(appSource, /await ensureMetaMaskChain\(\);\s*const accounts = await requestMetaMask\(\{ method: "eth_requestAccounts" \}\)/);
  assert.match(appSource, /await ensureMetaMaskChain\(\);[\s\S]*method: "eth_sendTransaction"/);
});

test("DApp estimates EVM gas before opening MetaMask confirmation", () => {
  assert.match(appSource, /function withEstimatedEvmGas/);
  assert.match(appSource, /method: "eth_estimateGas"/);
  assert.match(appSource, /const padded = \(estimated \* 13n \+ 9n\) \/ 10n/);
  assert.match(appSource, /tx\.gas = bigIntToEvmQuantity\(existing > padded \? existing : padded\)/);
  assert.doesNotMatch(appSource, /existing > 0n && existing < padded/);
  assert.match(appSource, /delete tx\.gas/);
  assert.match(appSource, /const tx = await withEstimatedEvmGas\(\{ \.\.\.transaction, from: state\.wallet\.account \}\)/);
  assert.match(appSource, /params: \[tx\]/);
});

test("DApp resets MetaMask privacy identity after account changes", () => {
  const block = appSource.match(/accountsChanged", accounts => \{[\s\S]*?\n  \}\);/)?.[0] || "";
  assert.match(block, /resetWalletSession\(\);/);
  assert.match(block, /renderWallet\(\);/);
  assert.match(block, /renderKeplr\(\);/);
  assert.match(block, /Reconnect wallet to refresh privacy identity/);
  assert.doesNotMatch(block, /state\.wallet\.account = accounts\[0\]/);
});
