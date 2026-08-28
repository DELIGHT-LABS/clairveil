import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const appSource = await readFile(new URL("../public/app.js", import.meta.url), "utf8");
const browserStorageScopeSource = await readFile(new URL("../public/browser-storage-scope.js", import.meta.url), "utf8");
const privacyBrowserStorageSource = await readFile(new URL("../public/privacy-browser-storage.js", import.meta.url), "utf8");
const encryptedStoreSource = await readFile(new URL("../public/encrypted-note-store.js", import.meta.url), "utf8");
const encryptedReservationSource = await readFile(new URL("../public/encrypted-reservation-manager.js", import.meta.url), "utf8");
const encryptedOperationSource = await readFile(new URL("../public/encrypted-operation-store.js", import.meta.url), "utf8");
const depositFundingSource = await readFile(new URL("../public/deposit-funding.js", import.meta.url), "utf8");
const relayReconciliationSource = await readFile(new URL("../public/relay-withdraw-reconciliation.js", import.meta.url), "utf8");
const cosmosFlowStateSource = await readFile(new URL("../public/cosmos-flow-state.js", import.meta.url), "utf8");
const evmReconciliationSource = await readFile(new URL("../public/evm-reconciliation.js", import.meta.url), "utf8");
const evmBroadcastWatchSource = await readFile(new URL("../public/evm-broadcast-watch.js", import.meta.url), "utf8");
const evmTypedScanEvidenceSource = await readFile(new URL("../public/evm-typed-scan-evidence.js", import.meta.url), "utf8");
const cosmosEvmCorrelationSource = await readFile(new URL("../public/cosmos-evm-transaction-correlation.js", import.meta.url), "utf8");
const reservationRecoverySource = await readFile(new URL("../public/reservation-recovery.js", import.meta.url), "utf8");
const reservationReconciliationSource = await readFile(new URL("../public/reservation-reconciliation.js", import.meta.url), "utf8");
const configSource = await readFile(new URL("../public/dapp-config.js", import.meta.url), "utf8");
const readmeSource = await readFile(new URL("../README.md", import.meta.url), "utf8");
const serverSource = await readFile(new URL("../server.js", import.meta.url), "utf8");
const htmlSource = await readFile(new URL("../public/index.html", import.meta.url), "utf8");
const cssSource = await readFile(new URL("../public/styles.css", import.meta.url), "utf8");

test("DApp keeps the top summary focused on core chain status", () => {
  assert.match(htmlSource, /id="protocolState"/);
  assert.doesNotMatch(htmlSource, /id="reserveState"/);
  assert.doesNotMatch(htmlSource, /id="depositProofState"/);
  assert.doesNotMatch(appSource, /els\.reserveState/);
  assert.doesNotMatch(appSource, /els\.depositProofState/);
});

test("DApp separates the transfer prover warning from the review facts", () => {
  assert.match(htmlSource, /id="proverPrivacyWarning"/);
  assert.match(cssSource, /#proverPrivacyWarning\s*\{[\s\S]*margin-bottom:\s*14px;/);
});

test("DApp keeps minimal-denom amount inputs as integer strings", () => {
  assert.match(appSource, /function amountInputValue/);
  assert.doesNotMatch(appSource, /Number\(input\.value/);
  assert.match(appSource, /BigInt\(raw\)/);
});

test("DApp disables value-moving actions for zero or invalid minimal-denom amounts", () => {
  assert.match(appSource, /function hasPositiveUclairInput/);
  assert.match(appSource, /amount <= 0n/);
  assert.match(appSource, /function updateAmountActionButtons/);
  assert.match(appSource, /sendFromKeplr\.disabled = valueMovingActionPending[\s\S]*!signerReady[\s\S]*!hasPositiveUclairInput\(els\.keplrSendAmount\)[\s\S]*!isSendRecipientForWallet\(els\.keplrSendRecipient\.value/);
  assert.match(appSource, /depositFromKeplr\.disabled = valueMovingActionPending[\s\S]*!signerReady[\s\S]*!depositProofReady\(\)[\s\S]*!hasPositiveUclairInput\(els\.keplrDepositAmount\)/);
  assert.match(appSource, /transferFromVeiled\.disabled = valueMovingActionPending[\s\S]*!veiledReady[\s\S]*!hasPositiveUclairInput\(els\.veiledTransferAmount\)/);
  assert.match(appSource, /withdrawFromVeiled\.disabled = valueMovingActionPending[\s\S]*!veiledReady[\s\S]*!hasPositiveUclairInput\(els\.veiledWithdrawAmount\)/);
  assert.match(appSource, /relayWithdrawFromVeiled\.disabled = valueMovingActionPending[\s\S]*!veiledReady[\s\S]*relayRecoveryBlocked[\s\S]*!hasPositiveUclairInput\(els\.relayWithdrawAmount\)/);
  assert.doesNotMatch(appSource, /const reservationBlocked = state\.reservations\.retryBlocked/);
  assert.match(appSource, /keplrSendAmount,[\s\S]*keplrSendRecipient,[\s\S]*veiledWithdrawAmount[\s\S]*addEventListener\("input", updateAmountActionButtons\)/);
});

test("DApp single-flights value-moving actions and invalidates pending privacy UI", () => {
  assert.match(appSource, /function depositFromKeplr\(\) \{\s*return runValueMovingAction\("privacy-deposit", depositFromKeplrUnlocked\);\s*\}/);
  assert.match(appSource, /function sendFromKeplr\(\) \{\s*return runValueMovingAction\("transparent-send", sendFromKeplrUnlocked\);\s*\}/);
  assert.match(appSource, /function transferFromVeiled\(\) \{\s*return runValueMovingAction\("privacy-transfer", transferFromVeiledUnlocked\);\s*\}/);
  assert.match(appSource, /function invalidateActivePrivacyFlow\(\) \{[\s\S]*controller\?\.abort\(\)[\s\S]*closeTransferFlowModal\(false\)[\s\S]*valueMovingActionGate\.invalidate\(\)[\s\S]*setBusy\(action, false\)/);
  assert.match(appSource, /function invalidatePrivacySession\(\) \{[\s\S]*invalidateActivePrivacyFlow\(\)/);
});

test("DApp fails closed when encrypted browser-storage capabilities are unavailable", () => {
  assert.match(privacyBrowserStorageSource, /localStorage is required for encrypted Clairveil browser storage/);
  assert.match(privacyBrowserStorageSource, /IndexedDB is required for encrypted Clairveil browser storage/);
  assert.match(privacyBrowserStorageSource, /Web Crypto is required for encrypted Clairveil browser storage/);
  assert.match(privacyBrowserStorageSource, /Web Locks API is required for encrypted Clairveil browser storage/);
  assert.match(appSource, /const storage = requirePrivacyBrowserStorage\(\)/);
  assert.match(appSource, /async function setupKeplrPrivacy\(options = \{\}\) \{[\s\S]*requirePrivacyBrowserStorage\(\)/);
  assert.match(appSource, /async function requirePrivacyPreparePreflight\(sessionContext\) \{[\s\S]*requirePrivacyBrowserStorage\(\)/);
  assert.match(appSource, /Encrypted note storage is required before applying a privacy scan/);
  assert.doesNotMatch(appSource, /const stored = store \? await store\.load\(\) : null/);
  assert.match(appSource, /setupKeplrPrivacy\.disabled = valueMovingActionGate\.active \|\| !signerReady \|\| !privacyStorageReady/);
  assert.match(appSource, /depositFromKeplr\.disabled = valueMovingActionPending[\s\S]*!privacyStorageReady/);
  assert.match(appSource, /transferFromVeiled\.disabled = valueMovingActionPending[\s\S]*!privacyStorageReady/);
  assert.match(appSource, /noteInventoryTrusted = privacyBrowserStorageCapability\(\)\.available/);
});

test("DApp stops deposit when asynchronous privacy setup does not reach ready state", () => {
  const setupStart = appSource.indexOf("async function setupKeplrPrivacy");
  const setupEnd = appSource.indexOf("async function copyKeplrDisclosurePubKey", setupStart);
  const setupBlock = appSource.slice(setupStart, setupEnd);
  const rootStored = setupBlock.indexOf("state.keplr.rootSignatureBase64 = rootSignatureBase64");
  const postRootPersistence = setupBlock.indexOf("await refreshReservationState(null, { sessionContext })", rootStored);
  const failureHandler = setupBlock.indexOf("} catch (error) {", postRootPersistence);

  assert.ok(setupStart >= 0 && setupEnd > setupStart);
  assert.ok(rootStored >= 0);
  assert.ok(postRootPersistence > rootStored);
  assert.ok(failureHandler > postRootPersistence);
  assert.match(setupBlock.slice(failureHandler), /Setup failed[\s\S]*return false/);
  const completedSetups = [...setupBlock.matchAll(/await completeInitialPrivacySetup\(/g)].map(match => match.index);
  const successfulReturns = [...setupBlock.matchAll(/return true;/g)].map(match => match.index);
  assert.equal(completedSetups.length, 2);
  assert.equal(successfulReturns.length, 2);
  assert.ok(successfulReturns[0] > completedSetups[0]);
  assert.ok(successfulReturns[1] > completedSetups[1]);

  const depositStart = appSource.indexOf("async function depositFromKeplrUnlocked");
  const depositEnd = appSource.indexOf("function noteStoreMutationLockName", depositStart);
  const depositBlock = appSource.slice(depositStart, depositEnd);
  const setupAttempt = depositBlock.indexOf("const privacySetupReady = await setupKeplrPrivacy()");
  const setupFailureExit = depositBlock.indexOf("if (!privacySetupReady) return", setupAttempt);
  const broadcastBoundary = depositBlock.indexOf("await broadcastPrivacyDeposit", setupFailureExit);

  assert.ok(depositStart >= 0 && depositEnd > depositStart);
  assert.ok(setupAttempt >= 0);
  assert.ok(setupFailureExit > setupAttempt);
  assert.ok(broadcastBoundary > setupFailureExit);
});

test("DApp injects exact deterministic Cosmos fees into private preparations", () => {
  assert.match(appSource, /function cosmosFeeRequestOptions\(gasLimit\)[\s\S]*deterministicCosmosFeeAmount/);
  assert.match(appSource, /buildBankSendSignDoc\(\{[\s\S]*cosmosFeeRequestOptions\(cosmosGasLimits\.send\)/);
  assert.match(appSource, /prepareTransfer\(privacyRequest\([\s\S]*cosmosFeeRequestOptions\(cosmosGasLimits\.transfer\)/);
  assert.match(appSource, /prepareWithdraw\(privacyRequest\([\s\S]*cosmosFeeRequestOptions\(cosmosGasLimits\.withdraw\)/);
});

test("DApp exposes evidence-gated preparation recovery without globally blocking unreserved notes", () => {
  assert.match(htmlSource, /id="reservationRecovery"/);
  assert.match(htmlSource, /id="reservationRecoveryList"/);
  assert.match(htmlSource, /Only preparations with no broadcast or relay handoff evidence/);
  assert.match(cssSource, /\.reservation-recovery-item\s*\{/);
  assert.match(appSource, /function recoverReservationPreparation/);
  assert.match(appSource, /await scanKeplrNotes\(\{[\s\S]*quiet: true,[\s\S]*throwOnError: true/);
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
  assert.match(appSource, /fundKeplr\.disabled = valueMovingActionGate\.active \|\| !serverFeature\("faucet"\) \|\| !signerReady/);
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
  assert.match(serverSource, /const activeProfile = isEvmTransport\(\) \? evmProfile : clairveilProfile/);
  assert.match(serverSource, /activeProfile\.denom !== config\.denom/);
  assert.match(serverSource, /return \[validateBrowserWalletProfile\(activeProfile\)\]/);
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
  assert.match(serverSource, /const cosmosRestEndpoints = configuredRestEndpoints\(/);
  assert.match(serverSource, /config\.cosmosRestEndpoints\.length[\s\S]*config\.publicRestEndpoints/);
  assert.match(serverSource, /restEndpoints: cosmosRestEndpoints/);
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
  assert.match(serverSource, /relayer: localSignerMutation/);
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
  assert.match(appSource, /const client = clairveilBrowserClient\(\);[\s\S]*client\.fetchPrivacyEvents\(\)/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.fetchAuditableTransfers\(\)/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareDeposit/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareTransfer/);
  assert.match(appSource, /client\.prepareTransferBatch\(privacyRequest\(/);
  assert.match(appSource, /onPreparedPayload:/);
  assert.match(appSource, /onPreparedProof:/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareWithdraw/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.scanWalletNotes/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeUserDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeSelfViewDisclosure/);
  assert.match(appSource, /const checkpoint = await client\.signDirect/);
  assert.match(appSource, /client\.broadcastTxRawBytes\(checkpoint\.txRawBytes/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.waitForEvmTransaction/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.sendEvmTransaction/);
  assert.match(appSource, /function defaultNoteScanCursor/);
  assert.match(appSource, /function noteScanRequestOptions/);
  assert.match(appSource, /const freshTypedScan = reset \|\|/);
  assert.match(appSource, /freshTypedScan \? \{ after: typedPrivacyScanAfter\(cursor\) \} : \{\}/);
  assert.match(appSource, /function applyNoteScanResult/);
  assert.match(appSource, /scanSource: "privacy_scan"/);
  assert.match(appSource, /strictPrivacyScan: true/);
  assert.match(appSource, /Typed privacy-scan-v2 is required; legacy scan results are not accepted/);
  assert.match(appSource, /state\.keplr\.noteScanResumeOptions/);
  assert.match(appSource, /const store = await currentNoteStore\(\)/);
  assert.match(appSource, /await clearCurrentNoteStore\(\)/);
  assert.doesNotMatch(appSource, /eventTypes: \["deposit", "shielded_transfer"\]/);
  assert.match(appSource, /scanWalletNotes\(privacyRequest\(\{\s*\.\.\.scanOptions,[\s\S]*noteStore: store,[\s\S]*includeFoundNotes: true/);
  assert.match(appSource, /more events queued/);
  assert.match(appSource, /scan:\s*\{\s*after: typedPrivacyScanAfter\(\),\s*scanSource: "privacy_scan",\s*strictPrivacyScan: true,\s*limit: 200,\s*maxPages: 1000/);
  assert.match(appSource, /function browserProverUrl/);
  assert.match(appSource, /browserLoopbackRewriteEnabled\(state\.config\)[\s\S]*serverFeature\("proverProxy"\)[\s\S]*configured/);
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
  assert.match(serverSource, /new URL\(pathname\.replace/);
  assert.match(appSource, /function browserDepositProofUrl/);
  assert.match(appSource, /function configuredDepositProofProvider/);
  assert.match(appSource, /const depositProofProvider = configuredDepositProofProvider\(\)/);
  assert.match(appSource, /depositProofProvider,/);
  assert.match(appSource, /async function fullPrivacyProtocolPreflight/);
  assert.match(appSource, /client\.health\(\)/);
  assert.match(appSource, /client\.assertTransferProtocolConfig\(baseDenom\(\)\)/);
  assert.match(appSource, /client\.queryReserve\(baseDenom\(\)\)/);
  assert.match(appSource, /client\.evmJsonRpc\("eth_chainId", \[\]\)/);
  assert.match(appSource, /await requirePrivacyPreparePreflight\(sessionContext\)/);
  assert.match(appSource, /enableExperimentalBatchTransfer: config\?\.serverFeatures\?\.batchTransfer === true/);
  assert.match(appSource, /enableExperimentalBatchTransfer: serverFeature\("batchTransfer"\)/);
  assert.match(appSource, /refreshEvents\(\{ allowFailure: true, sessionContext \}\)/);
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
  assert.match(appSource, /const reservationSnapshot = await manager\.store\.load\(\)/);
  assert.match(appSource, /expectedReservationState: reservationSnapshot/);
  assert.match(encryptedReservationSource, /FRESH_GENESIS_RESERVATION_STATE_CHANGED/);
  assert.doesNotMatch(encryptedReservationSource, /unsafeAllowPlaintext/);

  assert.match(appSource, /prepareTransfer\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /prepareWithdraw\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /prepareRelayWithdraw\(privacyRequest\([\s\S]*reservationManager: manager/);
  assert.match(appSource, /preparedReservationBinding\(data\)/);
  assert.match(appSource, /sendEvmTransaction\(data\.transaction,[\s\S]*reservationBinding: broadcastOptions/);
  assert.match(appSource, /signDirectAndBroadcast\(data\.signDoc, \{[\s\S]*sessionContext/);
  assert.match(appSource, /beforeBroadcast: identity => \{[\s\S]*persistCapturedPublicPendingTransaction/);
  const signStart = appSource.indexOf("async function signDirectAndBroadcast");
  const signEnd = appSource.indexOf("async function submitEvmTransaction", signStart);
  const signBlock = appSource.slice(signStart, signEnd);
  const validationIndex = signBlock.indexOf("await afterSigningBeforeBroadcast");
  const directMarkerIndex = signBlock.indexOf("if (publicPendingKind) {", validationIndex);
  const transportIndex = signBlock.indexOf("client.broadcastTxRawBytes", directMarkerIndex);
  assert.ok(validationIndex >= 0 && validationIndex < directMarkerIndex);
  assert.ok(directMarkerIndex < transportIndex);
  assert.match(signBlock.slice(directMarkerIndex, transportIndex), /persistCapturedPublicPendingTransaction\(sessionContext, publicPendingKind, signedTxHash\)/);
  assert.match(appSource, /withPublicTransactionLock\(sessionContext/);
  assert.match(appSource, /function withCosmosAccountTransactionLock/);
  assert.match(appSource, /assertNoUnresolvedCosmosAccountBroadcast/);
  assert.match(appSource, /record\?\.broadcast_in_flight === true/);
  assert.match(appSource, /await withCosmosAccountTransactionLock\(sessionContext, executeTransfer\)/);
  assert.match(appSource, /await withCosmosAccountTransactionLock\(sessionContext, executeWithdraw\)/);
  assert.match(appSource, /publicTransactionLockHeld[\s\S]*preparePrivacyTransferSignDoc/);
  assert.match(appSource, /clearCapturedPublicPendingTransaction/);
  assert.match(appSource, /const reservationIDs = \[\.\.\.state\.relayWithdraw\.reservationIds\][\s\S]*manager\.recordRelayHandoff\(reservationIDs/);

  assert.match(appSource, /if \(!broadcast\?\.receipt\) \{[\s\S]*markUnknown\(reservationIDs,[\s\S]*fromStatus: reservationStatuses\.Submitted[\s\S]*unknown: true/);
  assert.match(appSource, /createEvmBroadcastWatcher/);
  assert.match(evmBroadcastWatchSource, /result\?\.unknown \? onUnknown : onIncluded/);
  assert.match(evmBroadcastWatchSource, /await evidenceIsCurrent\(result\)/);
  assert.match(reservationReconciliationSource, /nullifierUnspentConfirmed: true/);
  assert.match(reservationReconciliationSource, /txAbsentOrFailedConfirmed: true/);
  assert.match(reservationReconciliationSource, /txHashChecked: txHash/);
  assert.match(appSource, /same request|다시 전송하지 마세요/);
  assert.match(htmlSource, /id="reservationState"/);
  assert.match(htmlSource, /id="reconcileReservations"/);
});

test("DApp binds prepare and broadcast reservation refreshes to the originating privacy session", () => {
  const prepareStart = appSource.indexOf("async function preparePrivacyTransferSignDoc");
  const prepareEnd = appSource.indexOf("async function broadcastPrivacyDeposit", prepareStart);
  const prepareSource = appSource.slice(prepareStart, prepareEnd);
  assert.equal(
    [...prepareSource.matchAll(/await refreshReservationState\(manager, \{ sessionContext \}\)/g)].length,
    4
  );

  const broadcastStart = appSource.indexOf("async function broadcastPreparedPrivacy");
  const broadcastEnd = appSource.indexOf("function evmReceiptHasFailed", broadcastStart);
  const broadcastSource = appSource.slice(broadcastStart, broadcastEnd);
  assert.equal(
    [...broadcastSource.matchAll(/refreshReservationState\(data\.reservationManager, \{ sessionContext \}\)/g)].length,
    2
  );
});

test("DApp shares the Cosmos account fence across equivalent profiles and fences endpoint or local-genesis changes", () => {
  assert.match(appSource, /function accountTransactionScopeId[\s\S]*`cosmos:\$\{String\(profile\.chainId/);
  assert.match(appSource, /profileId: accountTransactionScopeId\(profile\)/);
  assert.match(appSource, /profileId: privacyStorageProfileId\(profile\)/);
  assert.match(appSource, /transactionScope: accountTransactionScopeId\(\)/);
  assert.match(appSource, /storageEpoch: String\(state\.chainStorageEpoch \|\| ""\)/);

  const lockStart = appSource.indexOf("function publicTransactionLockName");
  const lockEnd = appSource.indexOf("function capturedPublicPendingState", lockStart);
  const lockBlock = appSource.slice(lockStart, lockEnd);
  assert.match(lockBlock, /context\?\.transactionScope/);
  assert.match(lockBlock, /context\?\.account/);
  assert.doesNotMatch(lockBlock, /publicPendingKey|storageEpoch|profileId/);

  const epochStart = appSource.indexOf("async function assertCurrentLocalChainStorageEpoch");
  const epochEnd = appSource.indexOf("async function assertNoUnresolvedCosmosAccountBroadcast", epochStart);
  const epochBlock = appSource.slice(epochStart, epochEnd);
  assert.match(epochBlock, /await clairveilBrowserClient\(\)\.health\(\)/);
  assert.match(epochBlock, /observed !== context\.storageEpoch/);
  assert.match(epochBlock, /CHAIN_STORAGE_EPOCH_CHANGED/);
  assert.match(appSource, /async function assertNoUnresolvedCosmosAccountBroadcast[\s\S]*await assertCurrentLocalChainStorageEpoch\(context\)/);

  const endpointStart = appSource.indexOf("function selectNoteScanEndpoint");
  const endpointEnd = appSource.indexOf("function renderChainDependentUi", endpointStart);
  const endpointBlock = appSource.slice(endpointStart, endpointEnd);
  assert.match(appSource, /selectedRestEndpoint: state\.selectedRestEndpointByProfile/);
  assert.match(endpointBlock, /publicTransactionCoordinator\.pending > 0/);
  assert.match(endpointBlock, /valueMovingActionGate\.active/);
  assert.match(endpointBlock, /invalidatePrivacySession\(\)[\s\S]*state\.selectedRestEndpointByProfile/);
  assert.match(appSource, /els\.noteScanEndpoint\.disabled = endpoints\.length < 2[\s\S]*publicTransactionCoordinator\.pending > 0[\s\S]*valueMovingActionGate\.active/);
  assert.match(appSource, /els\.noteScanEndpoint\.disabled = noteMutationPending[\s\S]*accountTransactionPending[\s\S]*valueMovingActionGate\.active/);
});

test("DApp keeps a private Cosmos fence when the SDK reports the broadcast boundary", () => {
  const signStart = appSource.indexOf("async function signDirectAndBroadcast");
  const signEnd = appSource.indexOf("async function submitEvmTransaction", signStart);
  const signBlock = appSource.slice(signStart, signEnd);
  assert.match(signBlock, /privateReservationBroadcast = !publicPendingKind/);
  assert.doesNotMatch(signBlock, /else if \(privateReservationBroadcast\) \{\s*persistCapturedPublicPendingTransaction\(sessionContext, "privacy", signedTxHash\)/);
  assert.match(signBlock, /beforeBroadcast: identity => \{[\s\S]*persistCapturedPublicPendingTransaction\([\s\S]*"privacy"/);
  assert.match(signBlock, /cosmosPrivatePendingMarkerCanClear\(\{[\s\S]*clearCapturedPublicPendingTransaction\(sessionContext, "privacy", txHash\)/);
  assert.doesNotMatch(signBlock, /clearCapturedPrivacyPending/);
  assert.match(reservationReconciliationSource, /function reservationHasExplicitBroadcastRejection\(record\)[\s\S]*checkTxRejected && rpcInvoked === true[\s\S]*abortedBeforeRpc && rpcInvoked === false/);
  assert.match(appSource, /explicitRejectionReconciled = check\?\.included !== true[\s\S]*linked\.every\(record => \([\s\S]*reservationStatuses\.ReplanRequired[\s\S]*reservationHasExplicitBroadcastRejection\(record\)/);

  const fenceStart = appSource.indexOf("async function assertNoUnresolvedCosmosAccountBroadcast");
  const fenceEnd = appSource.indexOf("function withCosmosAccountTransactionLock", fenceStart);
  const fenceBlock = appSource.slice(fenceStart, fenceEnd);
  const durableMarkerIndex = fenceBlock.indexOf("capturedPrivacyPendingState(context)");
  const managerIndex = fenceBlock.indexOf("currentReservationManager()");
  assert.ok(durableMarkerIndex >= 0 && durableMarkerIndex < managerIndex);
  assert.match(fenceBlock, /COSMOS_ACCOUNT_TX_PENDING/);
  assert.match(fenceBlock, /reservationConsumesBrowserCosmosSequence/);

  assert.match(appSource, /async function clearReconciledCosmosPrivacyPending[\s\S]*reservationStatuses\.ConfirmedSpent[\s\S]*operationStatuses\.Succeeded[\s\S]*reservationStatuses\.ReplanRequired/);
  assert.match(appSource, /matchingOperations = groupReservationOperations\(records\)[\s\S]*commonCosmosReservationTransactionHash\(operation\.records\) === markerHash/);
  assert.match(appSource, /clearCapturedPublicPendingTransaction\(sessionContext, "privacy", latest\.txHash\)/);
  assert.match(appSource, /cosmosPrivacyPendingHash[\s\S]*Setup Clairveil and Reconcile/);
  assert.match(appSource, /canReconcileReservationState\(\{[\s\S]*privacyPending,/);

  const reconcileStart = appSource.indexOf("async function reconcileReservations");
  const reconcileEnd = appSource.indexOf("async function resolvePreparedPrivacyFailure", reconcileStart);
  const reconcileBlock = appSource.slice(reconcileStart, reconcileEnd);
  assert.match(reconcileBlock, /activeChainProfile\(\)\?\.transport === "cosmos"[\s\S]*withPublicTransactionLock\(sessionContext[\s\S]*accountTransactionLockHeld: true/);
  const markerClearIndex = reconcileBlock.indexOf("if (privacyMarkerHasReservation)");
  const unrelatedStateIndex = reconcileBlock.indexOf("const reconciliationIncomplete");
  assert.ok(markerClearIndex >= 0 && markerClearIndex < unrelatedStateIndex);
  assert.doesNotMatch(reconcileBlock, /if \(!reconciliationIncomplete && privacyMarkerHasReservation\)/);
});

test("DApp reconciles spent transfers and Cosmos withdraws only with matching operation evidence", () => {
  assert.match(appSource, /function reservationRequiresOperationEvidence/);
  assert.match(appSource, /function operationEventForOperation/);
  assert.match(appSource, /event\?\.event_type !== "shielded_transfer"/);
  assert.match(appSource, /event\?\.event_type !== "withdraw"/);
  assert.match(appSource, /eventAttribute\(event, "commitment_1"\)/);
  assert.match(appSource, /eventAttribute\(event, "audit_disclosure_digest"\)/);
  assert.match(appSource, /cosmosWithdrawOperationEvidence\(\{[\s\S]*transaction: check\.transaction[\s\S]*expectedNullifiers/);
  assert.match(appSource, /manager\.store\.listReservations\(\{ ownerKeyId: manager\.ownerKeyId \}\)/);
  assert.match(appSource, /operationStatuses\.ConflictSpent/);
  assert.match(appSource, /spentRecords\.length !== records\.length[\s\S]*operationEventForReservations\(records, notesByLookupKey\)/);
  assert.match(appSource, /findPrivacyEventByTxHash/);
  assert.match(appSource, /eventTypes: reservationPrivacyEventTypes\(records\)/);
  assert.match(appSource, /if \(!lookup\.complete\) continue/);
  assert.match(appSource, /operationSuccessEvidence: evidenceByLookupKey\.get\(lookupKey\)/);
  assert.match(appSource, /function operationEvidenceWithReservationTransactionIdentity[\s\S]*transactionIdentity = \{ txHash: normalized \}[\s\S]*transactionIdentity = \{ txBytesHash: normalized \}/);
  assert.match(appSource, /commonCosmosReservationTransactionHash\(records\) === normalized/);
  assert.match(appSource, /operationReconciliationStatus\(record\) !== operationStatuses\.Succeeded/);
  assert.match(appSource, /OPERATION_RECONCILIATION_REQUIRED/);
  assert.match(appSource, /state\.reservations\.unresolved\?\.length > 0/);
  assert.match(appSource, /canReconcileReservationState\(\{[\s\S]*unresolved: state\.reservations\.unresolved/);
  assert.match(appSource, /reconciliationReservationRecords\([\s\S]*unresolvedOperationReservations\(allReservations\)/);
  assert.match(appSource, /reconciliationIncomplete = remaining\.length > 0 \|\| unresolvedCount > 0/);
});

test("DApp keeps prepared reservation leases alive across wallet and relay waits", () => {
  assert.match(appSource, /reservationHeartbeatIntervalMs/);
  assert.match(appSource, /async function withPreparedReservationHeartbeat/);
  assert.match(appSource, /manager\.heartbeatLease\(reservationIDs, \{ leaseToken \}\)/);
  assert.match(appSource, /withPreparedReservationHeartbeat\(finalData, \(\) =>/);
  assert.match(appSource, /let broadcast = await withPreparedReservationHeartbeat\(data/);
  assert.match(appSource, /function startRelayReservationHeartbeat/);
  assert.match(appSource, /relayReservationHeartbeatTimer = globalThis\.setInterval/);
  assert.match(appSource, /if \(fullyConfirmed\) \{[\s\S]*stopRelayReservationHeartbeat\(reconciliationContext\.heartbeatGeneration\)/);
  assert.match(appSource, /RESERVATION_HEARTBEAT_FAILED/);
});

test("DApp fences scan result application to the originating privacy session", () => {
  const scanApplySource = appSource.slice(
    appSource.indexOf("async function applyNoteScanResult"),
    appSource.indexOf("function selectedLocalAccount")
  );
  const scanRunSource = appSource.slice(
    appSource.indexOf("async function scanKeplrNotesUnlocked"),
    appSource.indexOf("async function scanKeplrNotes(options")
  );
  assert.match(scanApplySource, /sessionContext = privacySessionSnapshot\(\)/);
  assert.match(scanApplySource, /const store = scanNoteStore === undefined \? await currentNoteStore\(\) : scanNoteStore;\s*assertPrivacySession\(sessionContext\);/);
  assert.match(scanApplySource, /Encrypted note storage is required before applying a privacy scan[\s\S]*const stored = await store\.load\(\);\s*assertPrivacySession\(sessionContext\);/);
  assert.match(scanApplySource, /const manager = scanReservationManager \|\| await currentReservationManager\(\);\s*assertPrivacySession\(sessionContext\);/);
  assert.match(scanApplySource, /await reconcileSpentReservations\(manager, scannedNotes, \{ sessionContext \}\);\s*assertPrivacySession\(sessionContext\);/);
  assert.match(scanApplySource, /refreshReservationState\(manager, \{ sessionContext, notes: scannedNotes \}\)/);
  assert.match(scanRunSource, /applyNoteScanResult\(data, \{[\s\S]*reset,[\s\S]*sessionContext,[\s\S]*reservationManager: options\.reservationManager \|\| null,[\s\S]*noteStore: store/);

  const scanWrapperSource = appSource.slice(
    appSource.indexOf("async function scanKeplrNotes(options"),
    appSource.indexOf("function downloadTextFile")
  );
  assert.ok(scanWrapperSource.indexOf("await withNoteStoreMutation")
    < scanWrapperSource.indexOf("await finalizePendingDepositRecoveryFromTypedNotes"));
  assert.match(appSource, /async function finalizePendingDepositRecoveryFromTypedNotes[\s\S]*accountTransactionLockHeld[\s\S]*withPublicTransactionLock\(sessionContext, finalize\)/);
});

test("DApp fences reservation reconciliation and recovery transitions to the originating privacy session", () => {
  const spentSource = appSource.slice(
    appSource.indexOf("async function reconcileSpentReservations"),
    appSource.indexOf("function injectedEthereumProviders")
  );
  assert.match(spentSource, /sessionContext = null,[\s\S]*const assertFresh/);
  assert.match(spentSource, /manager\.store\.listReservations[\s\S]*assertFresh\(\)/);
  assert.match(spentSource, /await manager\.lookupKeyForNote\(note\)[\s\S]*assertFresh\(\)/);
  assert.match(spentSource, /await operationEventForReservations[\s\S]*assertFresh\(\)/);
  assert.match(spentSource, /await manager\.reconcileSpentNotes\(eligible\);\s*assertFresh\(\);/);

  const reconcileSource = appSource.slice(
    appSource.indexOf("async function reconcileReservations"),
    appSource.indexOf("async function resolvePreparedPrivacyFailure")
  );
  assert.match(reconcileSource, /refreshEvents\(\{ allowFailure: true, sessionContext \}\)/);
  assert.match(reconcileSource, /scanKeplrNotes\(\{[\s\S]*sessionContext,[\s\S]*reservationManager: resolvedManager/);
  assert.match(reconcileSource, /explicitlyUnspentReservationIDs\([\s\S]*\(\) => assertPrivacySession\(sessionContext\)/);
  assert.match(reconcileSource, /refreshReservationState\(resolvedManager, \{ sessionContext \}\)/);
  assert.match(reconcileSource, /if \(isStalePrivacySessionError\(error\)\) throw error/);

  const recoverySource = appSource.slice(
    appSource.indexOf("async function recoverReservationPreparation"),
    appSource.indexOf("async function reconcileReservations")
  );
  assert.match(recoverySource, /const sessionContext = privacySessionSnapshot\(\)/);
  assert.match(recoverySource, /refreshEvents\(\{ allowFailure: true, sessionContext \}\)/);
  assert.match(recoverySource, /reservationManager: manager/);
  assert.match(recoverySource, /resolvePreparationRecovery\([\s\S]*sessionContext,[\s\S]*operatorId: sessionContext\.account/);
});

test("DApp quarantines sign-doc-only recovery before acknowledgement and rechecks after approval", () => {
  assert.match(reservationRecoverySource, /tx_bytes_hash[\s\S]*broadcastAttempted/);
  assert.match(reservationRecoverySource, /const signDocOnly = hasSignDocIdentity && !hasQueryableTransactionIdentity/);
  const recoverySource = appSource.slice(
    appSource.indexOf("async function recoverReservationPreparation"),
    appSource.indexOf("async function reconcileReservations")
  );
  const quarantine = recoverySource.indexOf('error: "untracked_sign_doc_only_request"');
  const approval = recoverySource.indexOf("const approved = globalThis.confirm", quarantine);
  const postApprovalScan = recoverySource.indexOf("await scanKeplrNotes({", approval);
  const reservationRequery = recoverySource.indexOf("await manager.listActiveReservations()", postApprovalScan);
  const nullifierRequery = recoverySource.indexOf("explicitlyUnspentReservationIDs(", reservationRequery);
  const resolution = recoverySource.indexOf("resolvePreparationRecovery(", nullifierRequery);
  assert.ok(quarantine >= 0 && quarantine < approval);
  assert.ok(approval < postApprovalScan && postApprovalScan < reservationRequery);
  assert.ok(reservationRequery < nullifierRequery && nullifierRequery < resolution);
  assert.match(recoverySource, /sign_doc_only_request: true,[\s\S]*queryable_transaction_identity_absent: true,[\s\S]*broadcast_outcome_untracked: true,[\s\S]*untracked_wallet_request_acknowledged: true,[\s\S]*post_approval_chain_recheck: true/);
  assert.match(recoverySource, /signDocOnlyRecovery \? \{[\s\S]*broadcast_outcome_untracked: true,[\s\S]*\} : \{\s*no_broadcast_attempt: true\s*\}/);
  assert.match(appSource, /reason: "explicit_untracked_sign_doc_request_cancelled"/);
});

test("DApp scopes address suggestions and reservation leases to the active document session", () => {
  const leaseSource = appSource.slice(
    appSource.indexOf("function createDocumentReservationLeaseOwner"),
    appSource.indexOf("function activeChainProfile")
  );
  assert.match(leaseSource, /browser-document:/);
  assert.match(leaseSource, /cryptoImpl\?\.randomUUID|cryptoImpl\.getRandomValues/);
  assert.doesNotMatch(leaseSource, /sessionStorage\?\.\w|sessionStorage\.(?:getItem|setItem)|localStorage\?\.\w|localStorage\.(?:getItem|setItem)/);

  const addressSource = appSource.slice(
    appSource.indexOf("function addressBookScopeIdentity"),
    appSource.indexOf("async function ensureLocalSignersIfNeeded")
  ) + appSource.slice(
    appSource.indexOf("async function ensureShieldedAddressBook"),
    appSource.indexOf("function showAddressSuggestions")
  );
  assert.match(addressSource, /profile: activeProfileSessionIdentity\(\)/);
  assert.match(addressSource, /storageEpoch: String\(state\.chainStorageEpoch/);
  assert.match(addressSource, /shieldedAddressBookPromiseScope === scopeIdentity/);
  assert.match(addressSource, /state\.addressBook\.scopeIdentity !== scopeIdentity[\s\S]*shieldedAddressBookPromise !== lookup/);
  assert.match(appSource, /selectDappChainProfile[\s\S]*resetProfileScopedAddressBook\(addressBookScopeIdentity\(\)\)/);
});

test("DApp suppresses stale scan, public action, confirmation, and local-helper UI", () => {
  const heartbeatSource = appSource.slice(
    appSource.indexOf("async function withPreparedReservationHeartbeat"),
    appSource.indexOf("async function discardPreparedReservation")
  );
  assert.match(heartbeatSource, /const sessionContext = data\?\.privacySessionContext/);
  assert.match(heartbeatSource, /await task\(\);\s*assertPrivacySession\(sessionContext\)/);

  const discardSource = appSource.slice(
    appSource.indexOf("async function discardPreparedReservation"),
    appSource.indexOf("function stopRelayReservationHeartbeat")
  );
  assert.match(discardSource, /currentManager !== manager/);
  assert.match(discardSource, /refreshReservationState\(manager, \{ sessionContext \}\)/);

  const sendSource = appSource.slice(
    appSource.indexOf("async function sendFromKeplrUnlocked"),
    appSource.indexOf("function depositFromKeplr")
  );
  const depositSource = appSource.slice(
    appSource.indexOf("async function depositFromKeplrUnlocked"),
    appSource.indexOf("function noteStoreMutationLockName")
  );
  assert.match(sendSource, /isStalePrivacySessionError\(error\) \|\| !privacySessionIsCurrent\(sessionContext\)/);
  assert.match(sendSource, /finally \{[\s\S]*privacySessionIsCurrent\(sessionContext\)/);
  assert.match(depositSource, /isStalePrivacySessionError\(error\) \|\| !privacySessionIsCurrent\(sessionContext\)/);
  assert.match(depositSource, /refreshPrivacySurfaces\(\{ balance: true, sessionContext \}\)/);

  const helperSource = appSource.slice(
    appSource.indexOf("function localAccountRequestIdentity"),
    appSource.indexOf("function localSignerLabel")
  ) + appSource.slice(
    appSource.indexOf("async function fundKeplrUnlocked"),
    appSource.indexOf("async function completeInitialPrivacySetup")
  );
  assert.match(helperSource, /assertLocalAccountRequestCurrent/);
  assert.match(helperSource, /data\.unknown === true[\s\S]*Faucet result unknown/);
});

test("DApp fences external relay persistence before exposing the handoff", () => {
  const renderSource = appSource.slice(
    appSource.indexOf("function renderRelayWithdraw"),
    appSource.indexOf("async function setRelayWithdrawHandoff")
  );
  assert.match(renderSource, /relayWithdrawJson\.value = state\.relayWithdraw\.externalHandoff\s*\? state\.relayWithdraw\.json\s*:\s*""/);
  const recordSource = appSource.slice(
    appSource.indexOf("async function recordExternalRelayWithdrawHandoff"),
    appSource.indexOf("function renderKeplr")
  );
  assert.match(recordSource, /const sessionContext = privacySessionSnapshot\(\)/);
  const initialExpiryIndex = recordSource.indexOf("relayWithdrawPayloadExpired(payload, chainBlock.timeUnix)");
  const renewIndex = recordSource.indexOf("manager.renewLease(reservationIDs");
  const handoffIndex = recordSource.indexOf("manager.recordRelayHandoff(reservationIDs");
  const persistIndex = recordSource.indexOf("persistRelayWithdrawRecovery(handedOffState");
  const hiddenFenceIndex = recordSource.indexOf('resultStatus: "egress-blocked"');
  const refreshIndex = recordSource.indexOf("refreshReservationState(manager");
  const egressFetchIndex = recordSource.indexOf("const egressChainBlock = await fetchLatestChainBlock()");
  const egressExpiryIndex = recordSource.indexOf("relayWithdrawPayloadExpired(payload, egressChainBlock.timeUnix)");
  const exposeIndex = recordSource.indexOf("state.relayWithdraw = handedOffState;");
  assert.equal([...recordSource.matchAll(/await fetchLatestChainBlock\(\)/g)].length, 2);
  assert.ok(initialExpiryIndex >= 0 && renewIndex > initialExpiryIndex);
  assert.ok(handoffIndex > renewIndex && hiddenFenceIndex > handoffIndex);
  assert.ok(persistIndex > hiddenFenceIndex);
  assert.ok(refreshIndex > persistIndex && egressFetchIndex > refreshIndex);
  assert.ok(egressExpiryIndex > egressFetchIndex && exposeIndex > egressExpiryIndex);
  assert.match(recordSource, /leaseUntil: expiryLeaseUntil/);
  assert.match(recordSource, /relayWithdrawPayloadExpired\(payload, chainBlock\.timeUnix\)/);
  assert.match(recordSource, /recordRelayHandoff\(reservationIDs[\s\S]*assertPrivacySession\(sessionContext\)/);
  assert.match(recordSource, /persistRelayWithdrawRecovery\(handedOffState, \{ sessionContext \}\)/);
  assert.match(recordSource, /externalHandoff: true,[\s\S]*json: "",[\s\S]*resultStatus: "egress-blocked"/);
  assert.match(recordSource, /refreshReservationState\(manager, \{ sessionContext \}\)/);
  assert.match(recordSource, /RELAY_PAYLOAD_EXPIRED_BEFORE_EGRESS/);
  assert.match(recordSource, /handoff: null,[\s\S]*json: "",[\s\S]*payloadUnavailable: true/);

  const exposureSource = appSource.slice(
    appSource.indexOf("async function copyRelayWithdraw"),
    appSource.indexOf("async function signDirectAndBroadcast")
  );
  assert.match(exposureSource, /recordExternalRelayWithdrawHandoff\("clipboard"\);\s*assertPrivacySession\(sessionContext\);\s*assertRelaySubmitContext\(relayContext\);\s*await navigator\.clipboard\.writeText/);
  assert.match(exposureSource, /recordExternalRelayWithdrawHandoff\("download"\);\s*assertPrivacySession\(sessionContext\);\s*assertRelaySubmitContext\(relayContext\);\s*downloadTextFile/);
});

test("DApp serializes relay payload egress and same-origin submission", () => {
  const renderSource = appSource.slice(
    appSource.indexOf("function renderRelayWithdraw"),
    appSource.indexOf("async function setRelayWithdrawHandoff")
  );
  assert.match(renderSource, /const canStartHandoff = Boolean\(state\.relayWithdraw\.json\)[\s\S]*!relayHandoffInFlight[\s\S]*!valueMovingActionGate\.active/);
  assert.match(renderSource, /relayPreparedWithdraw\.disabled =[\s\S]*valueMovingActionGate\.active[\s\S]*relayHandoffInFlight[\s\S]*state\.relayWithdraw\.externalHandoff/);

  const externalHandoffSource = appSource.slice(
    appSource.indexOf("function copyRelayWithdraw"),
    appSource.indexOf("async function signDirectAndBroadcast")
  );
  assert.match(externalHandoffSource, /function copyRelayWithdraw\(\) \{\s*return runValueMovingAction\("relay-external-handoff", copyRelayWithdrawUnlocked\);\s*\}/);
  assert.match(externalHandoffSource, /function downloadRelayWithdraw\(\) \{\s*return runValueMovingAction\("relay-external-handoff", downloadRelayWithdrawUnlocked\);\s*\}/);

  const localRelaySource = appSource.slice(
    appSource.indexOf("function assertLocalRelaySubmissionAvailable"),
    appSource.indexOf("async function reconcileRelayWithdrawResult")
  );
  assert.match(localRelaySource, /relayHandoffInFlight \|\| state\.relayWithdraw\.externalHandoff/);
  assert.equal(
    [...localRelaySource.matchAll(/assertLocalRelaySubmissionAvailable\(\);/g)].length,
    2,
    "local relay must check the handoff boundary both before preflight and immediately before its attempt marker"
  );
  assert.ok(
    localRelaySource.lastIndexOf("assertLocalRelaySubmissionAvailable();") <
      localRelaySource.indexOf("manager.markBroadcastAttempting"),
    "the final handoff check must precede the durable local broadcast marker"
  );
});

test("DApp relay heartbeat callbacks cannot stop or mutate a newer generation", () => {
  const heartbeatSource = appSource.slice(
    appSource.indexOf("function stopRelayReservationHeartbeat"),
    appSource.indexOf("function reservationStatusSlug")
  );
  assert.match(heartbeatSource, /expectedGeneration !== relayReservationHeartbeatGeneration/);
  assert.match(heartbeatSource, /relayReservationHeartbeatGeneration \+= 1/);
  assert.ok(
    heartbeatSource.indexOf("manager !== reservationManager") <
      heartbeatSource.indexOf("stopRelayReservationHeartbeat();"),
    "a stale heartbeat starter must be rejected before it can stop the active timer"
  );
  assert.match(heartbeatSource, /generation !== relayReservationHeartbeatGeneration/);
  assert.match(heartbeatSource, /state\.relayWithdraw\.leaseToken === leaseToken/);
  assert.match(heartbeatSource, /stopRelayReservationHeartbeat\(generation\)/);
  assert.match(heartbeatSource, /failureGeneration !== relayReservationHeartbeatGeneration/);
});

test("DApp relay reconciliation pins its session, manager, store, and reservation set", () => {
  const reconcileSource = appSource.slice(
    appSource.indexOf("async function reconcileRelayWithdrawResult"),
    appSource.indexOf("async function explicitlyUnspentReservationIDs")
  );
  assert.match(reconcileSource, /const reconciliationContext = captureRelayReconciliationContext\(\{ candidateTxHash \}\)/);
  assert.match(reconcileSource, /manager = await currentReservationManager\(\);\s*assertRelayReconciliationContext\(reconciliationContext\);/);
  assert.match(reconcileSource, /store = await currentOperationStore\(\);\s*assertRelayReconciliationContext\(reconciliationContext\);/);
  assert.match(reconcileSource, /Promise\.all\(reservationIDs\.map\(id => manager\.getReservation\(id\)\)\)/);
  assert.doesNotMatch(reconcileSource, /state\.relayWithdraw\.reservationIds\.map/);
  assert.match(reconcileSource, /metadataOnly && transport === "cosmos"[\s\S]*assertCosmosRelayWithdrawTransactionPayloadHash/);
  assert.match(reconcileSource, /payloadHash: reconciliationContext\.payloadHash/);
  assert.match(reconcileSource, /\/api\/relayer\/withdraw\/reconcile/);
  assert.match(reconcileSource, /localSubmissionAttempted[\s\S]*manager\.recordRelayTransactionEvidence/);
  assert.match(reconcileSource, /metadata-only EVM relay recovery requires every reservation to bind the same submitted transaction hash/);
  assert.match(reconcileSource, /loadEvmOperationArtifactForReservations\(records, \{ sessionContext \}\)/);
  assert.match(reconcileSource, /!reconciliationContext\.candidateTxHashUnbound && records\.some/);
  assert.match(reconcileSource, /handoffTransaction: relayOperationArtifact\.transaction/);
  const transactionCheckIndex = reconcileSource.indexOf("await checkReservationTransaction(txHash)");
  const payloadBindingIndex = reconcileSource.indexOf("assertRelayWithdrawTransactionMatches");
  const recoveryPersistIndex = reconcileSource.indexOf("await persistRelayWithdrawRecovery(state.relayWithdraw", payloadBindingIndex);
  const evidenceCapabilityIndex = reconcileSource.indexOf(
    "typeof manager.recordRelayTransactionEvidence",
    payloadBindingIndex
  );
  const evidenceBindingIndex = reconcileSource.indexOf("await manager.recordRelayTransactionEvidence", payloadBindingIndex);
  const scanIndex = reconcileSource.indexOf("await scanKeplrNotes", payloadBindingIndex);
  assert.ok(transactionCheckIndex >= 0
    && payloadBindingIndex > transactionCheckIndex
    && recoveryPersistIndex > payloadBindingIndex
    && evidenceCapabilityIndex > recoveryPersistIndex
    && evidenceBindingIndex > evidenceCapabilityIndex
    && scanIndex > evidenceBindingIndex,
  "external relay evidence must be queried, payload-bound, durably saved, reservation-bound, then scanned");
  assert.match(reconcileSource, /left unchanged for manual review.*must not be resubmitted/);
  assert.match(reconcileSource, /refreshEvents\(\{ allowFailure: true, sessionContext \}\)/);
  assert.match(reconcileSource, /scanKeplrNotes\(\{[\s\S]*sessionContext,[\s\S]*reservationManager: manager[\s\S]*\}\)/);
  assert.match(reconcileSource, /persistRelayWithdrawRecovery\(state\.relayWithdraw, \{\s*store,\s*identity: operationIdentity,\s*sessionContext/);
  assert.match(reconcileSource, /refreshReservationState\(manager, \{ sessionContext \}\)/);
});

test("DApp installs a relay handoff only after its originating store write remains current", () => {
  const handoffSource = appSource.slice(
    appSource.indexOf("async function setRelayWithdrawHandoff"),
    appSource.indexOf("async function recordExternalRelayWithdrawHandoff")
  );
  const persistIndex = handoffSource.indexOf("await persistRelayWithdrawRecovery(nextRelayWithdraw");
  const fenceIndex = handoffSource.indexOf("assertPrivacySession(sessionContext);", persistIndex);
  const installIndex = handoffSource.indexOf("state.relayWithdraw = nextRelayWithdraw;");
  assert.ok(persistIndex >= 0 && fenceIndex > persistIndex && installIndex > fenceIndex);
  assert.match(handoffSource, /persistRelayWithdrawRecovery\(nextRelayWithdraw, \{ store, identity, sessionContext \}\)/);
  assert.match(handoffSource, /startRelayReservationHeartbeat\(\{[\s\S]*sessionContext/);
});

test("DApp fences encrypted relay recovery immediately before committing it", () => {
  const persistSource = appSource.slice(
    appSource.indexOf("async function persistRelayWithdrawRecovery"),
    appSource.indexOf("async function hydrateRelayWithdrawRecovery")
  );
  assert.match(persistSource, /beforeCommit:\s*sessionContext[\s\S]*assertPrivacySession\(sessionContext\)/);
  assert.match(encryptedOperationSource, /beforeCommit\?\.\(\);\s*this\.storage\.setItem/);
});

test("DApp installs encrypted relay recovery only into its originating privacy session", () => {
  const hydrateSource = appSource.slice(
    appSource.indexOf("async function hydrateRelayWithdrawRecovery"),
    appSource.indexOf("function preparedReservationIDs")
  );
  const setupSource = appSource.slice(
    appSource.indexOf("async function completeInitialPrivacySetup"),
    appSource.indexOf("async function copyKeplrDisclosurePubKey")
  );
  const loadIndex = hydrateSource.indexOf("const savedRecords = await store.loadAll();");
  const loadFenceIndex = hydrateSource.indexOf("assertPrivacySession(sessionContext);", loadIndex);
  const installIndex = hydrateSource.indexOf("state.relayWithdraw = nextRelayWithdraw;");
  assert.ok(loadIndex >= 0 && loadFenceIndex > loadIndex && installIndex > loadFenceIndex);
  assert.match(hydrateSource, /Promise\.all\(nextRelayWithdraw\.reservationIds\.map[\s\S]*assertPrivacySession\(sessionContext\);/);
  assert.match(hydrateSource, /savedRecords\.map\(saved => \{[\s\S]*restoreRelayWithdrawRecoveryMetadata\(saved\.relayWithdraw\)/);
  assert.match(hydrateSource, /!savedRecords\.length[\s\S]*state\.relayWithdraw = clearedRelayWithdrawState\("idle", "Not checked"\)/);
  assert.match(hydrateSource, /commonReservationTxHashes\.length === 1[\s\S]*nextRelayWithdraw\.txHash = commonReservationTxHashes\[0\]/);
  assert.match(hydrateSource, /conflicting or partial submitted transaction identities/);
  assert.match(hydrateSource, /manager && records\.some\(record => !record\)[\s\S]*missing reservation record/);
  assert.match(hydrateSource, /record\?\.metadata\?\.relay_handed_off === true/);
  assert.doesNotMatch(hydrateSource, /startRelayReservationHeartbeat/);
  assert.match(setupSource, /hydrateRelayWithdrawRecovery\(\{ sessionContext \}\)/);
  assert.match(setupSource, /if \(isStalePrivacySessionError\(error\)\) throw error;/);
  assert.match(setupSource, /refreshReservationState\(null, \{ sessionContext \}\)/);
  assert.match(setupSource, /completeInitialPrivacySetup\(\{ \.\.\.options, sessionContext \}\)/);
  assert.match(setupSource, /scanKeplrNotes\(\{[\s\S]*sessionContext[\s\S]*\}\)/);
});

test("DApp blocks unresolved public retries and exposes Cosmos and EVM tx reconciliation", () => {
  const depositStart = appSource.indexOf("async function broadcastPrivacyDeposit");
  const depositEnd = appSource.indexOf("function normalizedHex", depositStart);
  const depositBroadcast = appSource.slice(depositStart, depositEnd);
  const publicSendStart = appSource.indexOf("async function sendFromKeplrUnlocked");
  const publicSendEnd = appSource.indexOf("function depositFromKeplr", publicSendStart);
  const publicSend = appSource.slice(publicSendStart, publicSendEnd);
  assert.match(appSource, /sendPending = \["attempting", "submitted", "unknown", "checking"\]/);
  assert.match(appSource, /depositPending = \["attempting", "submitted", "unknown", "checking", "recovery-pending"\]/);
  assert.match(appSource, /async function reconcilePublicTransaction/);
  assert.match(appSource, /const check = await checkReservationTransaction\(txHash\)/);
  assert.match(appSource, /failureConfirmed = error\?\.code === "TX_FAILED_ON_CHAIN"[\s\S]*evmReceiptHasFailed/);
  assert.match(appSource, /same request remains blocked|같은 요청은 계속 차단됩니다/);
  assert.match(htmlSource, /id="reconcileKeplrSend"/);
  assert.match(htmlSource, /id="reconcileKeplrDeposit"/);
  assert.match(appSource, /hydratePublicPendingTransactions/);
  assert.doesNotMatch(appSource, /persistPublicPendingTransactions/);
  assert.match(appSource, /assertNoCapturedPublicPendingTransaction/);
  assert.match(htmlSource, /id="clearPublicPendingState"/);
  assert.match(appSource, /function waitForPublicEvmTransaction/);
  assert.match(appSource, /privacyTransaction: data\.transaction/);
  assert.match(appSource, /sender: state\.wallet\.account/);
  assert.match(appSource, /broadcast\?\.ok === false/);
  assert.match(appSource, /async function saveEvmDepositArtifact/);
  assert.match(appSource, /async function hydrateEvmDepositRecovery/);
  assert.match(appSource, /await restoreEvmDepositRecovery\(\{ sessionContext \}\)/);
  assert.match(depositBroadcast, /await saveEvmDepositArtifact\(recoveryArtifact, \{ sessionContext \}\)/);
  assert.match(depositBroadcast, /onEvmSubmitted:/);
  assert.match(depositBroadcast, /transaction: data\.transaction/);
  assert.match(depositBroadcast, /sender: evmDepositSender/);
  assert.match(appSource, /SDK transaction binding unavailable · manual recovery required/);
  assert.match(publicSend, /publicEvmTransactionBoundaryCallbacks\(sessionContext, "send"\)/);
  assert.match(publicSend, /persistCapturedPublicPendingTransaction\(sessionContext, "send", txHash\)/);
  assert.doesNotMatch(appSource, /submissionAmbiguous/);
  assert.match(appSource, /broadcast-unknown-no-hash/);
  assert.match(appSource, /EVM_SUBMISSION_RESULT_UNKNOWN/);
});

test("DApp exposes a common Cosmos/EVM atomic batch flow with per-payment disclosure", () => {
  const persistedBatchEvidence = appSource.match(/async function refreshPersistedEvmBatchReceiptEvidence\([\s\S]*?\n\}\n\nasync function hydrateBatchTransferRecovery/)?.[0] || "";
  const renderChainDependentSource = appSource.match(/function renderChainDependentUi\([\s\S]*?\n\}/)?.[0] || "";
  assert.match(htmlSource, /id="batchTransferSection"/);
  assert.match(htmlSource, /id="transferBatchFromVeiled"/);
  assert.match(htmlSource, /id="batchUseAuthorization"/);
  assert.match(appSource, /function batchTransferEnabled/);
  assert.match(appSource, /serverFeature\("batchTransfer"\)/);
  assert.match(serverSource, /enableBatchTransfer: envFlag\("CLAIRVEIL_DAPP_ENABLE_BATCH_TRANSFER", false\)/);
  assert.match(serverSource, /batchTransfer: config\.enableBatchTransfer/);
  assert.match(appSource, /userPrivacyPolicy/);
  assert.match(appSource, /userDisclosureMode/);
  assert.match(appSource, /userDisclosureTargetPubKeyHex/);
  assert.match(appSource, /eth_signTypedData_v4/);
  assert.match(appSource, /authorizationSigner/);
  assert.match(appSource, /CLAIRVEIL_EVM_CONTRACT_ADAPTERS/);
  assert.match(appSource, /CLAIRVEIL_PRIVACY_STATE_ADAPTERS/);
  assert.match(appSource, /CLAIRVEIL_EVM_FINALITY_POLICIES/);
  assert.match(appSource, /privacyStateAdapter,/);
  assert.match(appSource, /evmFinalityPolicy,/);
  assert.match(renderChainDependentSource, /renderBatchTransfer\(\)/);
  assert.match(appSource, /preparedBatchTransferFacts/);
  assert.match(appSource, /function saveBatchTransferArtifact/);
  assert.match(appSource, /function saveBatchReceiptEvidence/);
  assert.match(appSource, /verifiedEvmTransactionResult\(result, "EVM batch receipt"\)/);
  assert.match(evmReconciliationSource, /result\.evmFinalityVerified !== true/);
  assert.match(appSource, /function cosmosBatchEvidenceForReservations/);
  assert.match(appSource, /fetchAuditableBatchTransfers/);
  assert.match(appSource, /assertTypedBatchEffect/);
  assert.match(appSource, /spentRecords\.length !== records\.length/);
  assert.match(appSource, /transport === "evm"[\s\S]*saveBatchReceiptEvidence\(data, included, \{ sessionContext \}\)[\s\S]*saveBatchInclusion/);
  assert.match(persistedBatchEvidence, /result = await waitForEvmTransaction/);
  assert.match(persistedBatchEvidence, /evmReceiptHasFailed\(error\?\.broadcast\?\.receipt\)/);
  assert.match(persistedBatchEvidence, /if \(evmReceiptHasFailed\(error\?\.broadcast\?\.receipt\)\) continue;/);
});

test("DApp links EVM receipts to typed outer Comet transactions before reconciliation", () => {
  const batchEvidence = appSource.slice(
    appSource.indexOf("async function batchReceiptEvidenceForReservations"),
    appSource.indexOf("function batchOperationEvidence")
  );
  const directEvidence = appSource.slice(
    appSource.indexOf("async function directEvmReceiptEvidenceForReservations"),
    appSource.indexOf("async function clearTerminalDirectEvmOperationArtifacts")
  );
  const depositRecovery = appSource.slice(
    appSource.indexOf("function reconcilePendingDepositRecoveryFromTypedNotes"),
    appSource.indexOf("function noteScanRequestOptions")
  );
  assert.match(appSource, /verifyEvmScanTransactionLink/);
  assert.match(appSource, /findVerifiedEvmTypedScanEffect/);
  assert.match(cosmosEvmCorrelationSource, /ethereumTxHash/);
  assert.match(cosmosEvmCorrelationSource, /attribute\.index === true/);
  assert.match(evmTypedScanEvidenceSource, /typed scan matched multiple effects; reconciliation is ambiguous/);
  assert.match(batchEvidence, /verifiedEvmTypedScanEffect/);
  assert.match(batchEvidence, /assertTypedBatchEffect/);
  assert.match(batchEvidence, /scanTransactionLink/);
  assert.match(directEvidence, /verifiedEvmTypedScanEffect/);
  assert.match(directEvidence, /directEvmOperationSuccessEvidence\(records, receiptResult, effect\)/);
  assert.match(depositRecovery, /recoveredDepositNoteForCommitment/);
  assert.match(depositRecovery, /verifyEvmTypedScanTransaction/);
  assert.match(depositRecovery, /clearConfirmedDepositRecoveryUnlocked/);
});

test("DApp clears corrupt public pending state under the account lock without deleting replacement state", () => {
  const clearStart = appSource.indexOf("async function clearPublicPendingTransactions");
  const clearEnd = appSource.indexOf("async function resetCorruptPrivateRecoveryStateUnlocked", clearStart);
  const clearBlock = appSource.slice(clearStart, clearEnd);
  const snapshotIndex = clearBlock.indexOf("const confirmedRawState = storage.getItem(identity.key)");
  const lockIndex = clearBlock.indexOf("await withPublicTransactionLock(sessionContext");
  const compareIndex = clearBlock.indexOf("storage.getItem(identity.key) !== confirmedRawState", lockIndex);
  const removeIndex = clearBlock.indexOf("storage.removeItem(identity.key)", compareIndex);

  assert.ok(clearStart >= 0 && clearEnd > clearStart);
  assert.ok(snapshotIndex >= 0);
  assert.ok(lockIndex > snapshotIndex);
  assert.ok(compareIndex > lockIndex);
  assert.ok(removeIndex > compareIndex);
  assert.match(clearBlock, /separate private Cosmos transaction fence is never removed/);
  assert.doesNotMatch(clearBlock, /removeItem\(identity\.privacyKey\)/);
  assert.match(clearBlock, /error\.code = "PUBLIC_PENDING_STATE_CHANGED"/);
  assert.match(clearBlock, /clearingHashlessAttempt/);
  assert.match(clearBlock, /wallet request may have submitted a transaction without returning its hash/);
  assert.match(clearBlock, /catch \(error\) \{[\s\S]*hydratePublicPendingTransactions\(\)/);
  assert.match(appSource, /clearPublicPendingState\.addEventListener\("click", \(\) => clearPublicPendingTransactions\(\)\.catch\(reportAsyncError\)\)/);
});

test("DApp offers a reviewed full-state recovery path for a corrupt private Cosmos fence", () => {
  const resetStart = appSource.indexOf("async function resetCorruptPrivateRecoveryStateUnlocked");
  const resetEnd = appSource.indexOf("async function currentNoteStore", resetStart);
  const resetBlock = appSource.slice(resetStart, resetEnd);
  const accountLock = resetBlock.indexOf("await withPublicTransactionLock(sessionContext");
  const fullScan = resetBlock.indexOf("await scanKeplrNotes({", accountLock);
  const reservationReset = resetBlock.indexOf("await resetEncryptedBrowserReservationState(manager", fullScan);
  const privateFenceClear = resetBlock.indexOf("storage.removeItem(identity.privacyKey)", reservationReset);

  assert.ok(resetStart >= 0 && resetEnd > resetStart);
  assert.match(htmlSource, /id="resetPrivatePendingState"/);
  assert.match(appSource, /privateRecoveryResetVisible = activeChainProfile\(\)\?\.transport === "cosmos"[\s\S]*privacyPendingStateError/);
  assert.match(resetBlock, /RESET \$\{chainId\} \$\{account\}/);
  assert.match(resetBlock, /does not cancel a transaction that was already approved or propagated/);
  assert.match(resetBlock, /inspect Keplr activity and the explorer/);
  assert.match(resetBlock, /relayRecoveriesBeforeScan\.length/);
  assert.match(resetBlock, /relayRecoveriesAfterScan\.length/);
  assert.match(resetBlock, /state\.keplr\.noteSyncStatus !== "synced"/);
  assert.match(resetBlock, /manager\.store\.listReservations\(\{\s*ownerKeyId: manager\.ownerKeyId\s*\}\)/);
  assert.match(resetBlock, /reservations\.some\(reservationBlocksReviewedReset\)/);
  assert.ok(accountLock >= 0);
  assert.ok(fullScan > accountLock);
  assert.ok(reservationReset > fullScan);
  assert.ok(privateFenceClear > reservationReset);
  assert.doesNotMatch(resetBlock, /removeItem\(identity\.key\)/);
  assert.match(encryptedReservationSource, /confirmedReviewedFreshStateReset/);
  assert.match(encryptedReservationSource, /activeReservationStatuses/);
  assert.match(encryptedReservationSource, /await afterReset\?\.\(\)/);
  assert.match(appSource, /resetPrivatePendingState\.addEventListener\("click", \(\) => resetCorruptPrivateRecoveryState\(\)\.catch\(reportAsyncError\)\)/);
});

test("DApp stores private Cosmos fences separately and keeps relay recoveries payload-bound", () => {
  assert.match(appSource, /privacyPendingTxKey\(identity\)/);
  assert.match(appSource, /savePrivacyPendingTxState\(globalThis\.localStorage, identity\.privacyKey/);
  assert.match(appSource, /loadPrivacyPendingTxState\(globalThis\.localStorage, identity\.privacyKey/);
  assert.match(encryptedOperationSource, /relayWithdrawRecoveryPersistenceId/);
  assert.match(encryptedOperationSource, /recordKey\(persistenceId\)/);
  assert.match(encryptedOperationSource, /async loadAll\(\)/);
  assert.match(encryptedOperationSource, /Web Locks API is required for encrypted operation recovery/);
  assert.match(appSource, /store\.clear\(reconciliationContext\.payloadHash/);
});

test("DApp treats only canonical positive Cosmos tx codes as confirmed public failures", () => {
  assert.match(appSource, /function canonicalCosmosTxCode\(value\) \{[\s\S]*Number\.isSafeInteger\(value\) && value >= 0[\s\S]*\/\^\(0\|\[1-9\]\[0-9\]\*\)\$\/[\s\S]*Number\.isSafeInteger\(parsed\)/);
  assert.match(appSource, /import \{[\s\S]*cosmosTxEvidenceConfirmsFailure[\s\S]*\} from "\.\/cosmos-transaction-evidence\.js"/);

  const signStart = appSource.indexOf("async function signDirectAndBroadcast");
  const signEnd = appSource.indexOf("async function submitEvmTransaction", signStart);
  const signBlock = appSource.slice(signStart, signEnd);
  assert.match(signBlock, /cosmosTxEvidenceConfirmsFailure\(error\)/);
  assert.match(signBlock, /failure\.txHash \|\|= txHash/);
  assert.doesNotMatch(signBlock, /Number\.isSafeInteger\(Number\(txCode\)\)/);

  const assertionStart = appSource.indexOf("function assertSuccessfulBroadcast");
  const assertionEnd = appSource.indexOf("async function broadcastPreparedPrivacy", assertionStart);
  const assertionBlock = appSource.slice(assertionStart, assertionEnd);
  assert.match(assertionBlock, /const txCode = canonicalCosmosTxCode\(broadcast\.tx\.code\)/);
  assert.match(assertionBlock, /if \(txCode == null\)[\s\S]*error\.code = "TX_RESULT_UNKNOWN"/);
  assert.match(assertionBlock, /if \(txCode > 0\)[\s\S]*error\.code = "TX_FAILED_ON_CHAIN"/);
  assert.doesNotMatch(assertionBlock, /Number\(broadcast\.tx\.code \|\| 0\)/);

  const sendStart = appSource.indexOf("async function sendFromKeplrUnlocked");
  const sendEnd = appSource.indexOf("async function depositFromKeplrUnlocked", sendStart);
  const sendBlock = appSource.slice(sendStart, sendEnd);
  const depositEnd = appSource.indexOf("function selectedAuditMode", sendEnd);
  const depositBlock = appSource.slice(sendEnd, depositEnd);
  for (const block of [sendBlock, depositBlock]) {
    assert.match(block, /activeChainProfile\(\)\?\.transport === "evm"[\s\S]*evmReceiptHasFailed\(error\?\.broadcast\?\.receipt\)[\s\S]*cosmosTxEvidenceConfirmsFailure\(error\)/);
    assert.doesNotMatch(block, /Number\(error\.(?:tx|broadcast\.tx)\.code\) !== 0/);
  }
});

test("DApp does not reuse a private Cosmos pending hash as public send or deposit state", () => {
  const sendStart = appSource.indexOf("async function sendFromKeplrUnlocked");
  const sendEnd = appSource.indexOf("async function depositFromKeplrUnlocked", sendStart);
  const depositEnd = appSource.indexOf("function selectedAuditMode", sendEnd);
  for (const block of [appSource.slice(sendStart, sendEnd), appSource.slice(sendEnd, depositEnd)]) {
    assert.match(block, /if \(error\?\.code === "COSMOS_ACCOUNT_TX_PENDING"\) \{[\s\S]*Note reservations에서 Reconcile[\s\S]*return;[\s\S]*const txHash = transactionHashFromEvidence\(error\)/);
  }
});

test("DApp marks stale note inventory as unconfirmed and blocks private spends", () => {
  assert.match(appSource, /noteInventoryTrusted = state\.keplr\.noteSyncStatus === "synced"/);
  assert.match(appSource, /Cached · not confirmed/);
  assert.match(appSource, /Cached notes are shown for recovery only/);
  assert.match(appSource, /transferFromVeiled\.disabled = valueMovingActionPending[\s\S]*!veiledReady[\s\S]*!noteInventoryTrusted/);
  assert.match(appSource, /withdrawFromVeiled\.disabled = valueMovingActionPending[\s\S]*!veiledReady[\s\S]*!noteInventoryTrusted/);
});

test("DApp hides spendable inventory throughout protocol preflight failure and refresh", () => {
  const renderStart = appSource.indexOf("function renderMyKeplrNotes");
  const renderEnd = appSource.indexOf("function renderAccounts", renderStart);
  const renderBlock = appSource.slice(renderStart, renderEnd);
  const hiddenGuard = renderBlock.indexOf("if (!protocolReady)");
  const noteEnumeration = renderBlock.indexOf("const valueNotes = state.keplr.notes.filter");

  assert.ok(hiddenGuard >= 0);
  assert.ok(noteEnumeration > hiddenGuard);
  assert.match(renderBlock.slice(hiddenGuard, noteEnumeration), /Spendable note inventory is hidden until protocol preflight succeeds\.[\s\S]*return;/);
  assert.match(renderBlock, /Unavailable · protocol preflight failed/);
  assert.match(renderBlock, /Checking protocol compatibility/);

  for (const [name, nextName] of [
    ["async function requirePrivacyPreparePreflight", "async function refreshProtocolStatus"],
    ["async function refreshProtocolStatus", "async function refreshHealth"]
  ]) {
    const start = appSource.indexOf(name);
    const end = appSource.indexOf(nextName, start);
    const block = appSource.slice(start, end);
    assert.match(block, /state\.protocol\.ready = false;[\s\S]*renderMyKeplrNotes\(\)/);
    assert.match(block, /renderProtocolStatus\(\);\s*renderMyKeplrNotes\(\)/);
  }
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

test("DApp requires prepared self-merge approval before transfer and exact-note planner broadcasts", () => {
  assert.match(appSource, /function preparedSelfMergeReview/);
  assert.match(appSource, /prepared\.selectedInputTotal/);
  assert.match(appSource, /data\?\.plan\?\.selection\?\.inputs/);
  assert.match(appSource, /the final recipient is not paid in this step/);

  const confirmationStart = appSource.indexOf("async function confirmPreparedSelfMerge");
  const confirmationEnd = appSource.indexOf("function cancelTransferFlow", confirmationStart);
  const confirmationBlock = appSource.slice(confirmationStart, confirmationEnd);
  assert.match(confirmationBlock, /withPreparedReservationHeartbeat\(data, async \(\) =>/);
  assert.match(confirmationBlock, /await requestPreparedSelfMergeConfirmation\(review\)/);
  assert.match(confirmationBlock, /if \(!approved\) \{[\s\S]*discardPreparedReservation\(data, "user_cancelled_self_merge_before_broadcast"\)/);

  const exactNoteStart = appSource.indexOf("async function createExactWithdrawNote");
  const exactNoteEnd = appSource.indexOf("function sendFromKeplr", exactNoteStart);
  const exactNoteBlock = appSource.slice(exactNoteStart, exactNoteEnd);
  const exactApproval = exactNoteBlock.indexOf("await confirmPreparedSelfMerge");
  const exactBroadcast = exactNoteBlock.indexOf("broadcastPreparedPrivacy(data, \"exact-note self transaction\"");
  assert.ok(exactApproval >= 0 && exactApproval < exactBroadcast);
  assert.match(exactNoteBlock, /if \(!selfMergeConfirmed\) \{[\s\S]*return null;/);

  const transferStart = appSource.indexOf("async function transferFromVeiledUnlocked");
  const transferEnd = appSource.indexOf("function withdrawFromVeiled", transferStart);
  const transferBlock = appSource.slice(transferStart, transferEnd);
  const transferApproval = transferBlock.indexOf("await confirmPreparedSelfMerge");
  const transferBroadcast = transferBlock.indexOf("broadcastPreparedPrivacy(data, \"self transaction\"");
  assert.ok(transferApproval >= 0 && transferApproval < transferBroadcast);
  assert.match(transferBlock, /if \(!selfMergeConfirmed\) \{[\s\S]*Self transaction cancelled[\s\S]*return;/);
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
  assert.match(appSource, /function resetAndRescanNotes[\s\S]*scanKeplrNotes\(\{[\s\S]*reset: true,[\s\S]*throwOnError: true,[\s\S]*maxPages: 1000,[\s\S]*sessionContext/);
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

test("DApp isolates browser state when a local chain ID is reused for a new genesis", () => {
  assert.match(browserStorageScopeSource, /earliest_block_hash/);
  assert.match(browserStorageScopeSource, /storageEpoch/);
  assert.match(appSource, /localChainStorageEpoch\(\{/);
  assert.match(appSource, /storageEpoch: state\.chainStorageEpoch/);
  assert.match(appSource, /notes-encrypted:\$\{scope\.keySuffix\}/);
  assert.match(appSource, /operations-encrypted:\$\{scope\.keySuffix\}/);
});

test("DApp verifies transparent deposit funding and surfaces a non-zero fee budget", () => {
  assert.match(appSource, /function estimateDepositFeeBeforeProof/);
  assert.match(appSource, /function assertDepositFunding/);
  assert.match(appSource, /transparentBalanceAmount/);
  assert.match(appSource, /evmNativeBalance/);
  assert.match(appSource, /const privacyAccount = state\.keplr\.account[\s\S]*client\.getBalances\(privacyAccount\)/);
  assert.match(depositFundingSource, /Insufficient transparent balance/);
  assert.match(depositFundingSource, /Insufficient EVM gas balance/);
  assert.match(appSource, /keplrDirectSignOptions\(broadcastOptions\)/);
  assert.match(appSource, /cosmosGasFeeEstimate/);
  assert.match(appSource, /Fee will be estimated after the deposit proof is prepared/);
  assert.match(appSource, /const exactFee = await updateDepositNetworkFee\(data\.transaction\);/);
  assert.match(appSource, /assertDepositFunding\(amount, exactFee\)/);
  assert.doesNotMatch(appSource, /0 \$\{baseDenom\(\)\} encoded/);
});

test("DApp separates deposit inclusion from exact note recovery", () => {
  assert.match(appSource, /function recoverDepositNote/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.confirmDeposit/);
  assert.match(appSource, /expectedCommitment: prepared\.noteCommitmentHex/);
  assert.match(appSource, /expectedEncryptedNote: prepared\.encryptedNoteHex/);
  assert.match(appSource, /Included · recovery pending/);
  assert.match(appSource, /recoveredDepositNoteForTxHash\(state\.keplr\.notes, txHash\)/);
  assert.match(appSource, /function reconcilePendingDepositRecoveryFromTypedNotes[\s\S]*recoveredDepositNoteForCommitment/);
  assert.match(appSource, /function finalizePendingDepositRecoveryFromTypedNotes[\s\S]*verifyEvmTypedScanTransaction/);
  assert.match(appSource, /state\.keplr\.notes = scannedNotes;[\s\S]*reconcilePendingDepositRecoveryFromTypedNotes\(\)/);
  assert.match(appSource, /Recovered · encrypted note matched the exact included tx hash/);
  assert.match(appSource, /persistCapturedDepositRecoveryPending/);
  assert.match(appSource, /status: "recovery-pending"/);
  assert.match(appSource, /clearCapturedPublicPendingTransaction\([\s\S]*"deposit"[\s\S]*state\.keplr\.depositHash/);
  assert.match(appSource, /Deposit 및 note 복구 완료/);
  assert.match(htmlSource, /id="keplrDepositRecovery"/);
});

test("DApp confirms chain-bound intent details and supports self-view opt-out", () => {
  assert.match(appSource, /function fetchLatestChainBlockTimeUnix/);
  assert.match(appSource, /fetchBoundedJson\(`\$\{endpoint\}\/status`/);
  assert.match(appSource, /const profile = activeChainProfile\(\)[\s\S]*browserRpcUrl\(profile\)/);
  assert.match(appSource, /authoritativeChainBlockFromStatus\(data, profile\)/);
  assert.match(cosmosFlowStateSource, /result\?\.node_info\?\.network/);
  assert.match(cosmosFlowStateSource, /result\?\.sync_info\?\.latest_block_time/);
  assert.match(cosmosFlowStateSource, /result\?\.sync_info\?\.latest_block_height/);
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
  assert.match(appSource, /const finalPreparedExpiresAtUnix = preparedTransferExpiryUnix\(finalData/);
  assert.match(appSource, /expiresAtUnix: finalPreparedExpiresAtUnix/);
  assert.match(appSource, /afterSigningBeforeBroadcast: async \(\) => \{[\s\S]*fetchLatestChainBlock\(\)[\s\S]*assertPreparedTransferFreshAtChainTime/);
  assert.match(appSource, /const cosmosTransferValidation[\s\S]*getChainNowUnix: \(\) => fetchLatestChainBlockTimeUnix\(\)/);
  assert.match(appSource, /prepared_transfer_expired_before_broadcast/);
  assert.match(appSource, /discardPreparedReservation\(preparedData, "invalid_prepared_transfer_expiry"\)/);
  assert.match(appSource, /prepared_transfer_prebroadcast_validation_failed[\s\S]*discardPreparedReservation\(data, reason\)/);
  const signStart = appSource.indexOf("async function signDirectAndBroadcast");
  const signEnd = appSource.indexOf("async function submitEvmTransaction", signStart);
  const signBlock = appSource.slice(signStart, signEnd);
  assert.ok(signBlock.indexOf("const checkpoint = await client.signDirect")
    < signBlock.indexOf("await afterSigningBeforeBroadcast"));
  assert.ok(signBlock.indexOf("await afterSigningBeforeBroadcast")
    < signBlock.indexOf("persistCapturedPublicPendingTransaction(sessionContext, publicPendingKind, signedTxHash)"));
  assert.ok(signBlock.indexOf("persistCapturedPublicPendingTransaction(sessionContext, publicPendingKind, signedTxHash)")
    < signBlock.indexOf("client.broadcastTxRawBytes"));
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
  assert.match(appSource, /retry: \(\) => withdrawFromVeiled\(\{ relayMode \}\)/);
  assert.doesNotMatch(htmlSource, /id="withdrawMode"/);
  assert.match(htmlSource, /class="panel relayer-panel"/);
  assert.match(htmlSource, /id="relayWithdrawAmount"/);
  assert.match(htmlSource, /id="relayWithdrawRecipient"/);
  assert.match(htmlSource, /id="relayWithdrawFromVeiled"/);
  assert.match(htmlSource, /id="relayPreparedWithdraw"/);
  assert.match(appSource, /withdrawFromVeiled\(\{ relayMode: true \}\)/);
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
  assert.match(relayReconcileSource, /receiveConfirmed = check\.included[\s\S]*check\.successful === true[\s\S]*evmFinalityVerified/);
  assert.match(relayReconcileSource, /spentConfirmed && check\.successful !== true/);
  assert.match(relayReconcileSource, /check\.successful !== true[\s\S]*Tx was included with an unknown execution status/);
  assert.match(relayReconcileSource, /relay_spent_without_successful_bound_transaction/);
  assert.match(appSource, /async function verifyRelayEvmReceiptAndFinality/);
  assert.match(appSource, /const privacyTransaction = operationArtifact\?\.transaction \|\| handoff\?\.transaction/);
  assert.match(appSource, /waitForEvmTransaction\(txHash, \{[\s\S]*privacyTransaction,/);
  assert.match(appSource, /checkpointPreparedEvmOperation\(preparedData, "relay-withdraw"/);
  assert.match(relayReconcileSource, /const fullyConfirmed = spentConfirmed && receiveConfirmed/);
  assert.match(relayReconciliationSource, /assertRelayWithdrawTransactionMatches/);
  assert.match(relayReconciliationSource, /included Cosmos transaction must contain exactly one MsgWithdraw/);
  assert.match(relayReconciliationSource, /"calldata"/);
  assert.match(appSource, /Checking tx result first, then nullifier spent state/);
  assert.match(htmlSource, /id="relayWithdrawTxHash"/);
  assert.match(htmlSource, /<input[^>]+id="relayWithdrawTxHash"/);
  assert.match(htmlSource, /id="relayWithdrawRecoveryChoice"/);
  assert.match(appSource, /metadataOnlyRecovery/);
  assert.match(htmlSource, /Relay \(pay fee &amp; broadcast\)/);
  assert.match(htmlSource, /id="reconcileRelayWithdraw"/);
  assert.match(htmlSource, /user shielded secret/);
  assert.match(htmlSource, /privacy-sensitive data/);
  assert.match(htmlSource, /Recipient, chain, expiry는 변경할 수 없습니다/);
  assert.match(htmlSource, /local cancel이나 reservation release는 payload를 무효화하지 않으며/);
  assert.match(encryptedOperationSource, /AES-GCM/);
  assert.match(encryptedOperationSource, /OPERATION_STATE_CORRUPT/);
  assert.match(appSource, /persistRelayWithdrawRecovery/);
  assert.match(appSource, /hydrateRelayWithdrawRecovery/);
  assert.match(appSource, /async function relayPreparedWithdrawUnlocked/);
  assert.match(appSource, /resultStatus = "preflighting"/);
  assert.match(appSource, /const chainBlock = await fetchLatestChainBlock\(\)/);
  assert.match(appSource, /scanKeplrNotes\(\{[\s\S]*quiet: true,[\s\S]*throwOnError: true,[\s\S]*skipSetup: true,[\s\S]*maxPages: 1000,[\s\S]*sessionContext,[\s\S]*reservationManager: manager/);
  assert.match(appSource, /explicitlyUnspentReservationIDs\([\s\S]*manager,[\s\S]*records,[\s\S]*state\.keplr\.notes,[\s\S]*assertRelaySubmitContext\(context\)/);
  assert.match(appSource, /stopRelayReservationHeartbeat\(\);[\s\S]*manager\.markBroadcastAttempting/);
  assert.match(appSource, /manager\.markBroadcastAttempting/);
  assert.match(appSource, /reason: "same_origin_local_relayer_submit"[\s\S]*local_relayer_address: relayer\.transparentAddress/);
  assert.match(appSource, /built-in local relayer must use a separate server-side account/);
  assert.match(appSource, /relayEndpoint: new URL\("\/api\/relayer\/withdraw", document\.baseURI\)\.href/);
  assert.match(appSource, /refreshReservationState\(manager, \{ sessionContext \}\);\s*assertRelaySubmitContext\(context\);\s*const relay = await api\(context\.relayEndpoint/);
  assert.match(appSource, /if \(relay\.unknown === true\) \{[\s\S]*manager\.markUnknown\([\s\S]*local_relayer_included_status_unknown/);
  assert.match(appSource, /relay\.unknown === true[\s\S]*Relayer result unknown · reconcile the saved tx hash and do not retry/);
  assert.match(appSource, /manager\.markSubmitted/);
  assert.doesNotMatch(appSource, /manager\.markBroadcastRejected\(/);
  assert.match(appSource, /error\?\.checkTxRejected === true[\s\S]*manager\.markUnknown\(reservationIDs,[\s\S]*error: "local_relayer_check_tx_rejected"[\s\S]*check_tx_rejected: true[\s\S]*manager\.markManualReview\(reservationIDs,[\s\S]*automatic_replan_blocked: true/);
  const rejectedRelayStart = appSource.indexOf("if (attemptMarkerStarted");
  const rejectedRelayEnd = appSource.indexOf(
    'state.relayWithdraw.resultStatus = "manual-review"',
    rejectedRelayStart
  );
  assert.ok(rejectedRelayStart >= 0 && rejectedRelayEnd > rejectedRelayStart);
  const rejectedRelayBlock = appSource.slice(rejectedRelayStart, rejectedRelayEnd);
  assert.equal(
    [...rejectedRelayBlock.matchAll(/error:\s*"local_relayer_check_tx_rejected"/g)].length,
    2
  );
  assert.doesNotMatch(rejectedRelayBlock, /error:\s*error\.message/);
  assert.match(appSource, /async function recordExternalRelayWithdrawHandoff/);
  assert.match(appSource, /const context = captureRelaySubmitContext\(\);[\s\S]*manager\.recordRelayHandoff[\s\S]*assertRelaySubmitContext\(context\)/);
  const copyRelaySource = appSource.slice(
    appSource.indexOf("async function copyRelayWithdraw"),
    appSource.indexOf("async function signDirectAndBroadcast")
  );
  assert.ok(
    copyRelaySource.indexOf("recordExternalRelayWithdrawHandoff") <
      copyRelaySource.indexOf("navigator.clipboard.writeText"),
    "external handoff evidence must be recorded before clipboard egress"
  );
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

test("DApp binds disclosure decode completion and disclosure UI to the privacy session", () => {
  assert.match(appSource, /serverBinding:[\s\S]*auditorAdmin: Boolean\(state\.config\?\.serverFeatures\?\.auditorAdmin\)/);
  const decodeFunctions = [
    ["async function decodeSelectedEventDisclosure", "async function decodeSelectedSelfViewDisclosure", "state.privacyEvents.decoded = report"],
    ["async function decodeSelectedSelfViewDisclosure", "function disclosureMaterial", "state.privacyEvents.decoded = report"],
    ["async function decodeDisclosureSource", "function clearAuditorReport", "state.privacyEvents.decoded = report"],
    ["async function decodeAuditorTransfer", "function canConnectWallet", "state.auditor.decoded = report"]
  ];

  for (const [name, nextName, commit] of decodeFunctions) {
    const start = appSource.indexOf(name);
    const end = appSource.indexOf(nextName, start);
    const block = appSource.slice(start, end);
    const commitIndex = block.indexOf(commit);
    const fenceIndex = block.lastIndexOf("assertPrivacySession(sessionContext)", commitIndex);

    assert.ok(start >= 0 && end > start, `${name} must remain inspectable`);
    assert.match(block, /const sessionContext = privacySessionSnapshot\(\);/);
    assert.ok(fenceIndex >= 0 && fenceIndex < commitIndex, `${name} must fence its result before committing it`);
    assert.match(block, /isStalePrivacySessionError\(error\) \|\| !privacySessionIsCurrent\(sessionContext\)/);
    assert.match(block, /finally \{\s*if \(!privacySessionIsCurrent\(sessionContext\)\) return;/);
  }

  const invalidateStart = appSource.indexOf("function invalidatePrivacySession");
  const invalidateEnd = appSource.indexOf("function stalePrivacySessionError", invalidateStart);
  assert.match(appSource.slice(invalidateStart, invalidateEnd), /resetDisclosureSessionState\(\)/);

  const resetStart = appSource.indexOf("function resetDisclosureSessionState");
  const resetEnd = appSource.indexOf("function resetKeplrSession", resetStart);
  const resetBlock = appSource.slice(resetStart, resetEnd);
  assert.match(resetBlock, /state\.privacyEvents = defaultPrivacyEventsState\(\)/);
  assert.match(resetBlock, /state\.blockEvents = defaultBlockEventsState\(\)/);
  assert.match(resetBlock, /state\.auditor = defaultAuditorState\(\)/);
  assert.match(resetBlock, /disclosureSourceTxHash\.value = ""/);
  assert.match(resetBlock, /disclosureSourceEventJson\.value = ""/);
  assert.match(resetBlock, /renderEventDetail\(\)/);
  assert.match(resetBlock, /clearAuditorReport\(\)/);
});

test("DApp gates privacy actions on preflight and surfaces recovery UX", () => {
  assert.match(appSource, /noteInventoryTrusted = state\.keplr\.noteSyncStatus === "synced" && protocolReady/);
  assert.match(appSource, /depositFromKeplr\.disabled = valueMovingActionPending[\s\S]*!signerReady[\s\S]*!protocolReady/);
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

test("DApp never promotes malformed Cosmos or EVM transaction status to relay success", () => {
  assert.match(appSource, /hasFailedEvmReceiptStatus,[\s\S]*hasSuccessfulEvmReceiptStatus[\s\S]*from "\.\/transaction-status\.js"/);
  const checkSource = appSource.slice(
    appSource.indexOf("async function checkReservationTransaction"),
    appSource.indexOf("async function clearReconciledCosmosPrivacyPending")
  );
  assert.match(checkSource, /successful: hasSuccessfulEvmReceiptStatus\(receipt\)/);
  assert.match(checkSource, /failed: hasFailedEvmReceiptStatus\(receipt\)/);
  assert.match(checkSource, /const code = typeof rawCode === "number"[\s\S]*:\s*null/);
  assert.match(checkSource, /successful: Boolean\(tx\) && code === 0/);
  assert.match(checkSource, /failed: Boolean\(tx\) && code !== null && code !== 0/);
  assert.doesNotMatch(checkSource, /successful: Boolean\(receipt\) && !/);
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
  const ensureLocalSignerSource = appSource.match(/async function ensureLocalSignersIfNeeded\([\s\S]*?\n\}\n\nasync function browserHealthFromStaticConfig/)?.[0] || "";
  assert.match(serverSource, /evmDefaultSignerAccounts/);
  assert.match(serverSource, /function ensureLocalSigners/);
  assert.match(serverSource, /\/api\/local-signers\/ensure/);
  assert.match(serverSource, /allowLanSigning: process\.env\.CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING === "1"/);
  assert.match(serverSource, /allowLanAdmin: process\.env\.CLAIRVEIL_DAPP_ALLOW_LAN_ADMIN === "1"/);
  assert.match(serverSource, /accountPrefix: process\.env\.CLAIRVEIL_EVM_PRIVACY_ACCOUNT_PREFIX \?\? "clair"/);
  assert.match(serverSource, /url\.pathname === "\/api\/relayer\/withdraw"/);
  assert.match(serverSource, /url\.pathname === "\/api\/relayer\/withdraw\/reconcile"/);
  assert.match(serverSource, /relayWithdrawSubmissionGate\.reconcileByPayloadHash\([\s\S]*reconcile: reconcileRelaySubmissionAttempt/);
  const relayReconcileRoute = serverSource.slice(
    serverSource.indexOf('url.pathname === "/api/relayer/withdraw/reconcile"'),
    serverSource.indexOf('url.pathname === "/api/relayer/withdraw"', serverSource.indexOf('url.pathname === "/api/relayer/withdraw/reconcile"') + 1)
  );
  assert.doesNotMatch(relayReconcileRoute, /sendJson\(res, 200, recovered\)/);
  assert.match(relayReconcileRoute, /result: \/\^\(0x\)\?\[0-9a-fA-F\]\{64\}\$\//);
  assert.match(serverSource, /local relay withdraw must use the configured/);
  assert.match(serverSource, /createRelayWithdrawSubmissionGate\(\)/);
  assert.match(serverSource, /createRelayAccountSubmissionSerializer\(\)/);
  assert.match(serverSource, /function runLocalSignerSubmission\(signer, submit\)[\s\S]*relayAccountSubmissionSerializer\.run\(relaySubmissionAccountKey\(signer\)/);
  assert.match(serverSource, /const faucet = await runLocalSignerSubmission\([\s\S]*const \{ result, tx, txHash, outcome \} = await runLocalSignerSubmission/);
  assert.match(serverSource, /const response = await runLocalSignerSubmission\(\s*relayer,[\s\S]*=> relayWithdrawSubmissionGate\.run\(payload, \{[\s\S]*checkNullifiers: nullifiers => clairveil\.checkNullifiers\(nullifiers\)[\s\S]*submit: async/);
  assert.match(serverSource, /markExternalSubmissionStarted\(\);\s*const submitted = await wallet\.sendTransaction/);
  assert.match(serverSource, /markExternalSubmissionStarted\(\);\s*const result = await runClairveild/);
  assert.match(serverSource, /submit: async \(\s*markSubmissionStarted,\s*markSubmissionRejected,\s*markIncludedExecutionFailed/);
  assert.match(serverSource, /markAccountSubmissionOutcomeUnknown/);
  assert.match(serverSource, /assertCosmosCheckTxAccepted\(result\.json, \{\s*markSubmissionRejected: markExternalSubmissionRejected/);
  assert.match(serverSource, /const outcome = trackedEvmSubmissionOutcome\([\s\S]*if \(outcome\.failed === true\) markIncludedExecutionFailed\(\)/);
  assert.match(serverSource, /const outcome = trackedCosmosSubmissionOutcome\([\s\S]*if \(outcome\.failed === true\) markIncludedExecutionFailed\(\)/);
  assert.match(serverSource, /recordReturnedCosmosSubmissionEvidence\([\s\S]*recordAccountSubmissionEvidence,[\s\S]*recordSubmissionEvidence/);
  assert.match(serverSource, /function latestChainNowUnix/);
  assert.match(serverSource, /buildRelayWithdrawMessageFromPayload/);
  assert.match(serverSource, /assertEvmRelayCandidateMatches/);
  assert.doesNotMatch(serverSource, /hostAccountPrefix/);
  assert.doesNotMatch(serverSource, /CLAIRVEIL_EVM_ACCOUNT_PREFIX/);
  assert.match(serverSource, /function queryEvmNativeBalance/);
  assert.match(serverSource, /eth_getBalance/);
  assert.match(serverSource, /function assertSignerMutationAllowed/);
  assert.match(serverSource, /signer-mutating APIs require application\/json/);
  assert.match(serverSource, /signer-mutating APIs require an exact same-origin browser request/);
  assert.match(serverSource, /reconcileUnknown: reconcileLocalSignerSubmission/);
  assert.match(serverSource, /function assertLocalAdminAccessAllowed/);
  assert.match(serverSource, /\/api\/local-signers\/ensure"\) \{\s*assertLocalTestBackendAllowed\("local signer setup"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/faucet"\) \{\s*assertLocalTestBackendAllowed\("faucet"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/deposit"\) \{\s*assertLocalTestBackendAllowed\("local CLI deposit"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/auditor\/test-scalar"\) \{\s*assertLocalTestBackendAllowed\("auditor test scalar"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /\/api\/auditor\/decode"\) \{\s*assertLocalTestBackendAllowed\("auditor disclosure decode"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /local wallet show-address"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(serverSource, /local wallet note scan"\);\s*assertLocalAdminAccessAllowed\(req\);/);
  assert.match(ensureLocalSignerSource, /function ensureLocalSignersIfNeeded/);
  assert.match(ensureLocalSignerSource, /Automatic local signer setup failed:/);
  assert.doesNotMatch(ensureLocalSignerSource, /throw error/);
  assert.match(ensureLocalSignerSource, /Create accounts on the server machine first/);
  assert.match(ensureLocalSignerSource, /CLAIRVEIL_DAPP_ALLOW_LAN_SIGNING=1/);
  assert.match(ensureLocalSignerSource, /accounts: \[\]/);
  assert.doesNotMatch(serverSource, /\/api\/evm\/account/);
  assert.doesNotMatch(serverSource, /\/api\/tx\/evm\/bank-send\/transaction/);
  assert.match(appSource, /evmNativeSendTransaction/);
  assert.match(appSource, /eth_sendTransaction/);
  assert.match(appSource, /walletType: "evm"/);
});

test("DApp server restores the documented production security boundary", () => {
  assert.match(serverSource, /CLAIRVEIL_DAPP_HOST \?\? "127\.0\.0\.1"/);
  assert.match(serverSource, /assertProductionDeploymentConfig\(\)/);
  assert.match(serverSource, /assertProductionHttpsUrl\(config\.publicOrigin/);
  assert.match(serverSource, /"content-security-policy"/);
  assert.match(serverSource, /"script-src 'self'"/);
  assert.match(serverSource, /res\.setHeader\("x-content-type-options", "nosniff"\)/);
  assert.match(serverSource, /res\.setHeader\("referrer-policy", "no-referrer"\)/);
  assert.match(serverSource, /res\.setHeader\("cross-origin-opener-policy", "same-origin"\)/);
});

test("DApp shows a send result confirmation before refresh side effects", () => {
  assert.match(appSource, /function showSendResult/);
  assert.match(appSource, /title: "Send 요청됨"/);
  assert.match(appSource, /title: "Send 실패"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "MetaMask"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "Keplr"/);
  assert.match(appSource, /els\.keplrTxState\.textContent = "Send submitted"/);
  assert.match(appSource, /watchEvmBroadcast\(broadcast/);
  assert.match(appSource, /Promise\.allSettled\(\[[\s\S]*refreshWalletBalance\(\{ sessionContext \}\)[\s\S]*refreshBlockEvents\(\{ sessionContext \}\)[\s\S]*\]\)/);
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
  assert.match(appSource, /self transaction", \{[\s\S]*?waitForEvmReceipt: true/);
});

test("DApp durably fences public EVM submission before the wallet boundary and then captures its hash", () => {
  const adapterStart = appSource.indexOf("function evmWalletAdapter");
  const adapterEnd = appSource.indexOf("function evmPrivacyRequest", adapterStart);
  const adapterBlock = appSource.slice(adapterStart, adapterEnd);
  const durableAttempt = adapterBlock.indexOf('"onTransactionAttempt"');
  const providerReturn = adapterBlock.indexOf('method: "eth_sendTransaction"');
  const durableCapture = adapterBlock.indexOf('"onTransactionHash"', providerReturn);
  const sessionFence = adapterBlock.indexOf("assertPrivacySession(sessionContext)", durableCapture);

  assert.ok(adapterStart >= 0 && adapterEnd > adapterStart);
  assert.ok(durableAttempt >= 0 && durableAttempt < providerReturn);
  assert.ok(providerReturn >= 0);
  assert.ok(durableCapture > providerReturn);
  assert.ok(sessionFence > durableCapture);
  assert.match(adapterBlock, /isExplicitWalletRejection\(error\)[\s\S]*"onTransactionRejected"/);
  assert.match(adapterBlock, /EVM_SUBMISSION_RESULT_UNKNOWN/);
  assert.match(adapterBlock, /attachSubmittedEvmTransactionEvidence\(error, submittedTxHash\)/);
  assert.match(appSource, /assertPrivacySessionAfterEvmSubmission\(sessionContext, normalizedTxHash\)/);
  assert.match(appSource, /function publicEvmTransactionBoundaryCallbacks[\s\S]*persistCapturedPublicTransactionAttempt/);
  assert.match(appSource, /publicEvmTransactionBoundaryCallbacks\(sessionContext, options\.publicPendingKind\)/);

  const sendStart = appSource.indexOf("async function sendFromKeplrUnlocked");
  const sendEnd = appSource.indexOf("async function depositFromKeplrUnlocked", sendStart);
  const sendBlock = appSource.slice(sendStart, sendEnd);
  assert.match(sendBlock, /publicEvmTransactionBoundaryCallbacks\(sessionContext, "send"\)/);
});

test("DApp retains the early EVM deposit marker through receipt polling", () => {
  const depositStart = appSource.indexOf("async function broadcastPrivacyDeposit");
  const depositEnd = appSource.indexOf("function normalizedHex", depositStart);
  const depositBlock = appSource.slice(depositStart, depositEnd);

  assert.match(depositBlock, /publicPendingKind: "deposit"/);
  assert.match(depositBlock, /submitted\.pending \|\| submitted\.unknown[\s\S]*persistCapturedPublicPendingTransaction/);
  assert.match(depositBlock, /else if \(txHash\) \{\s*clearCapturedPublicPendingTransaction/);
  assert.match(depositBlock, /evmReceiptHasFailed\(error\?\.broadcast\?\.receipt\)[\s\S]*clearCapturedPublicPendingTransaction/);
});

test("Keplr connection refreshes balance with the replacement privacy session", () => {
  const connectStart = appSource.indexOf("async function connectKeplr");
  const connectEnd = appSource.indexOf("async function signKeplrSession", connectStart);
  const connectBlock = appSource.slice(connectStart, connectEnd);
  const resetIndex = connectBlock.indexOf("resetMetaMaskSession()");
  const replacementContextIndex = connectBlock.indexOf("const connectedSessionContext = privacySessionSnapshot()");
  const refreshIndex = connectBlock.indexOf("refreshWalletBalance({ sessionContext: connectedSessionContext })");

  assert.ok(resetIndex >= 0);
  assert.ok(replacementContextIndex > resetIndex);
  assert.ok(refreshIndex > replacementContextIndex);
  assert.doesNotMatch(connectBlock, /refreshWalletBalance\(\{ sessionContext \}\)/);
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
  assert.match(appSource, /function evmRpcTransaction/);
  assert.match(appSource, /function paddedEvmGas/);
  assert.match(appSource, /method: "eth_estimateGas"/);
  assert.match(appSource, /const padded = paddedEvmGas\(estimated\)/);
  assert.match(appSource, /const gas = paddedEvmGas\(estimatedGas\)/);
  assert.match(appSource, /delete estimateRequest\.gas/);
  assert.match(appSource, /tx\.gas = bigIntToEvmQuantity\(padded\)/);
  assert.doesNotMatch(appSource, /existing > padded/);
  assert.match(appSource, /delete tx\.gas/);
  assert.match(appSource, /const tx = await withEstimatedEvmGas\(\{ \.\.\.transaction, from: walletAccount \}\)/);
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
