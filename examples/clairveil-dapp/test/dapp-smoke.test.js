import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const appSource = await readFile(new URL("../public/app.js", import.meta.url), "utf8");
const configSource = await readFile(new URL("../public/dapp-config.js", import.meta.url), "utf8");
const staticConfig = JSON.parse(await readFile(
  new URL("../public/dapp-config.json", import.meta.url),
  "utf8",
));
const readmeSource = await readFile(new URL("../README.md", import.meta.url), "utf8");
const packageSource = await readFile(new URL("../package.json", import.meta.url), "utf8");
const packageJson = JSON.parse(packageSource);
const makefileSource = await readFile(new URL("../../../Makefile", import.meta.url), "utf8");
const dappLocalScriptSource = await readFile(
  new URL("../../../scripts/dapp-local.sh", import.meta.url),
  "utf8",
);
const serverSource = await readFile(new URL("../server.js", import.meta.url), "utf8");
const htmlSource = await readFile(new URL("../public/index.html", import.meta.url), "utf8");
const cssSource = await readFile(new URL("../public/styles.css", import.meta.url), "utf8");
const deploymentGateSource = await readFile(
  new URL("../tools/verify-production-deployment.mjs", import.meta.url),
  "utf8",
);
const relayReservationStateSource = await readFile(
  new URL("../public/relay-reservation-state.js", import.meta.url),
  "utf8",
);
const webClientConfigSchema = JSON.parse(await readFile(
  new URL("../../../docs/schemas/clairveil-web-client-config.schema.json", import.meta.url),
  "utf8",
));

function sourceBetween(source, start, end) {
  const startIndex = source.indexOf(start);
  assert.notEqual(startIndex, -1, `missing source marker: ${start}`);
  const endIndex = source.indexOf(end, startIndex + start.length);
  assert.notEqual(endIndex, -1, `missing source marker: ${end}`);
  return source.slice(startIndex, endIndex);
}

test("examples checks bundle freshness before a mutating DApp build", () => {
  const examplesTarget = sourceBetween(makefileSource, "examples:\n", "\n.PHONY: vulncheck");
  const freshnessIndex = examplesTarget.indexOf("run check:bundle:fresh");
  const mutatingCheckIndex = examplesTarget.indexOf("run check:dapp");
  assert.notEqual(freshnessIndex, -1);
  assert.notEqual(mutatingCheckIndex, -1);
  assert.ok(freshnessIndex < mutatingCheckIndex);
});

test("dapp-local starts the loopback same-origin prover proxy", () => {
  assert.match(
    packageJson.dependencies.clairveiljs,
    /^file:vendor\/clairveiljs-0\.2\.0-f6a77843fc14\.tgz$/,
  );
  assert.match(makefileSource, /dapp-local:\n\t\.\/scripts\/dapp-local\.sh/);
  assert.match(dappLocalScriptSource, /npm --prefix "\$repo_root\/examples\/clairveil-dapp" ci --ignore-scripts/);
  assert.doesNotMatch(dappLocalScriptSource, /local_clairveiljs_root|npm link/);
  assert.match(
    dappLocalScriptSource,
    /CLAIRVEIL_HOME="\$CLAIRVEIL_HOME" CHAIN_ID="\$CHAIN_ID" CLAIRVEIL_DAPP_LOCAL_TEST_MODE=1 CLAIRVEIL_DAPP_ENABLE_PROVER_PROXY=1 CLAIRVEIL_DAPP_ENABLE_BATCH_TRANSFER=1 npm start -- --host "\$dapp_host" --port "\$dapp_port"/,
  );
});

test("DApp reviews batch self-view disclosures through complete typed output evidence", () => {
  assert.match(htmlSource, /id="eventDisclosureReports"[\s\S]*class="audit-output-reports"/);
  assert.match(appSource, /function isBatchTransferEvent\(event\)/);
  assert.match(
    appSource,
    /event\.event_type === "shielded_transfer" \|\| isBatchTransferEvent\(event\)/,
  );
  assert.match(appSource, /function typedBatchScanStartCursor\(event\)/);
  assert.match(appSource, /async function batchTypedOutputsForEvent\(event\)/);
  assert.match(appSource, /const selectedIdentity = typedBatchEventIdentity\(event\)/);
  assert.match(appSource, /sameTypedBatchEventIdentity\([\s\S]*selectedIdentity,[\s\S]*typedBatchEventIdentity\(item\)/);
  assert.match(appSource, /sameTypedBatchEventIdentity\([\s\S]*selectedIdentity,[\s\S]*typedBatchEventIdentity\(output\)/);
  assert.match(
    appSource,
    /const selectedIdentity = typedBatchEventIdentity\(event\);[\s\S]*let after = typedBatchScanStartCursor\(selectedIdentity\);[\s\S]*fetchAuditableBatchTransfers\(/,
  );
  assert.match(
    appSource,
    /fetchAuditableBatchTransfers\(\{[\s\S]*eventLimit: batchEventScanEventLimit,[\s\S]*outputLimit: batchEventScanOutputLimit/,
  );
  assert.match(
    appSource,
    /typed batch scan stopped before the selected batch was complete/,
  );
  assert.match(
    appSource,
    /decodeBatchSelfViewDisclosure\([\s\S]*keplrPrivacyRequest\(\{ txHash: event\.tx_hash_hex, output \}\)/,
  );
  assert.match(
    appSource,
    /Verified sender self-view disclosure for \$\{reports\.length\} batch output/,
  );
  assert.match(serverSource, /const selectedIdentity = typedBatchEventIdentity\(\{ height, sequence \}\)/);
  assert.match(serverSource, /sameTypedBatchEventIdentity\(selectedIdentity, typedBatchEventIdentity\(item\)\)/);
  assert.match(serverSource, /sameTypedBatchEventIdentity\(selectedIdentity, typedBatchEventIdentity\(output\)\)/);
});

test("DApp selects privacy events by their typed identity, not transaction hash alone", () => {
  assert.match(appSource, /privacyEventSelectionKey/);
  assert.match(appSource, /function privacyEventKey\(event\)/);
  assert.match(appSource, /selectedEventKey: ""/);
  assert.match(
    appSource,
    /privacyEventKey\(event\) === state\.privacyEvents\.selectedEventKey/,
  );
  assert.match(
    appSource,
    /privacyEventKey\(event\) === state\.auditor\.selectedEventKey/,
  );
  assert.match(
    appSource,
    /state\.privacyEvents\.selectedEventKey !== selectedEventKey/,
  );
  assert.match(
    appSource,
    /state\.auditor\.selectedEventKey !== selectedEventKey/,
  );
});

test("DApp exposes feature-gated atomic one-proof batch transfer", () => {
  assert.match(htmlSource, /id="openBatchTransfer"[\s\S]*Add recipient/);
  assert.match(htmlSource, /id="batchTransferSection"[\s\S]*All-or-nothing/);
  assert.match(htmlSource, /Every payment is included,[\s\S]*or the entire batch fails/);
  assert.match(htmlSource, /id="batchTransferInputs">0 \/ 16/);
  assert.match(htmlSource, /id="batchTransferOutputs">0 \/ 32/);
  assert.match(htmlSource, /1 proof \/ 1 transaction/);
  assert.match(htmlSource, /id="batchTransferSplit"[\s\S]*Completed batches remain[\s\S]*committed/);
  assert.match(cssSource, /#batchTransferSplitControl\[hidden\]\s*\{\s*display:\s*none;/);
  assert.match(htmlSource, /id="batchTransferConfirmationModal"[\s\S]*id="batchTransferConfirmationPayments"[\s\S]*확인 후 Keplr 열기/);
  assert.match(appSource, /function batchTransferFeatureEnabled\(\)/);
  assert.match(appSource, /import \{ decodeShieldedAddress \} from "clairveiljs\/core"/);
  assert.match(
    appSource,
    /function isConfiguredShieldedAddress[\s\S]*decodeShieldedAddress\([\s\S]*shieldedPrefix: shieldedPrefix\(\)/,
  );
  assert.match(
    appSource,
    /Every batch recipient must be a complete, valid \$\{shieldedPrefix\(\)\} shielded address\.[\s\S]*error\.code = "INVALID_SHIELDED_RECIPIENT"/,
  );
  assert.match(
    appSource,
    /case "INVALID_SHIELDED_RECIPIENT":[\s\S]*A shortened address or one with a checksum typo cannot be proved\./,
  );
  assert.match(appSource, /serverFeature\("batchTransfer"\)/);
  assert.match(appSource, /enableExperimentalBatchTransfer: batchTransferFeatureEnabled\(\)/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareTransferBatch/);
  assert.match(appSource, /outputMode: "compact"/);
  assert.match(appSource, /onPreparedPayload,[\s\S]*onPreparedProof/);
  assert.match(appSource, /batchTransferMaxInputs = 16/);
  assert.match(appSource, /batchTransferMaxOutputs = 32/);
  assert.match(appSource, /preview\.requiresSplit && els\.batchTransferSplit\.checked/);
  assert.match(appSource, /renderVerifiedBatchTransferEvidence/);
  assert.match(appSource, /expected_outputs/);
  assert.match(appSource, /function reconcileBatchTransferArtifact/);
  assert.match(
    appSource,
    /manualReview \? "Pending review" : "Pending evidence"/,
  );
  assert.match(appSource, /recordState === "conflict"/);
  assert.match(appSource, /restoreUi: true,[\s\S]*notify: !options\.quiet/);
  assert.match(appSource, /preview\.unsplittablePayments\.length > 0/);
  assert.match(appSource, /No available spendable notes; Scan Notes or resolve pending reservations/);
  assert.match(appSource, /function batchTransferBroadcastRequiresReconciliation/);
  const batchExecution = sourceBetween(
    appSource,
    "async function executeAtomicBatchTransfer",
    "async function transferBatchFromVeiled",
  );
  assert.ok(
    batchExecution.indexOf("confirmPreparedBatchTransferBeforeBroadcast") <
      batchExecution.indexOf("broadcastPreparedPrivacy"),
  );
  assert.match(batchExecution, /batchTransferReconciliationRequired = true/);
  assert.match(
    appSource,
    /const submittedOperationReconciliationRetry[\s\S]*async function refreshSubmittedOperationReconciliation[\s\S]*submittedOperationReconciliationRetry\.attempts/,
  );
  assert.match(
    batchExecution,
    /batchTransferIncludedPendingEvidence = true[\s\S]*batchTransferIncludedTxHash = txHash/,
  );
  assert.ok(
    batchExecution.indexOf("renderVerifiedBatchTransferEvidence") <
      batchExecution.lastIndexOf(
        "batchTransferReconciliationRequired = true",
      ),
  );
  const batchFlow = sourceBetween(
    appSource,
    "async function transferBatchFromVeiled",
    "async function transferFromVeiled",
  );
  assert.match(
    batchFlow,
    /markBatchTransferItemsCompleted\([\s\S]*pending\.splice/,
  );
  assert.match(
    batchFlow,
    /currentBatchItemIDs = group\.map[\s\S]*currentBatchItemIDs\.length[\s\S]*setBatchTransferItemEvidence\(pendingRows, "Pending review"\)/,
  );
  assert.match(
    appSource,
    /function submittedOperationIsReconciled[\s\S]*batchTransferReservationsSucceeded\(records,[\s\S]*expectedOperationEvidenceHash: operationEvidenceHash/,
  );
  assert.match(
    batchFlow,
    /batchTransferPreparationFailedBeforeWallet[\s\S]*openReservationReviewDialog\([\s\S]*knownNoWalletRequest: true/,
  );
  assert.match(
    batchFlow,
    /batchTransferArtifactReview[\s\S]*openReservationReviewDialog\([\s\S]*preparedBatchReservation: artifactReview\.reservation/,
  );
  assert.match(
    appSource,
    /async function discardPreparedBatchTransferArtifact[\s\S]*record\.status !== reservationStatuses\.ProofReady[\s\S]*checkNullifierSpent[\s\S]*markReservationBatchReplanRequired[\s\S]*clearBatchTransferArtifact/,
  );
  assert.match(
    appSource,
    /batchTransferArtifactRecordState[\s\S]*"spent-evidence-conflict"[\s\S]*async function reconcileBatchTransferArtifact[\s\S]*recordState === "spent-evidence-conflict"[\s\S]*storage\.clear\(\)/,
  );
  assert.match(appSource, /async function recoverBatchPreparationFailure/);
  assert.match(
    appSource,
    /async function preparePrivacyBatchTransferSignDoc[\s\S]*preflightStage = "chain-safety"[\s\S]*preflightStage = "note-sync"[\s\S]*preflightStage = "typed-note-scan"[\s\S]*preflightStage = "artifact-recovery"[\s\S]*preflightStage = "chain-time"[\s\S]*preflightStage = "reservation-storage"[\s\S]*batchTransferPreparationFailedBeforeWallet[\s\S]*batchTransferPreflightStage = preflightStage/,
  );
  assert.match(
    appSource,
    /function batchTransferPreflightErrorMessage[\s\S]*case "chain-safety"[\s\S]*case "note-sync"[\s\S]*case "typed-note-scan"[\s\S]*case "artifact-recovery"[\s\S]*case "chain-time"[\s\S]*case "reservation-storage"/,
  );
  assert.match(
    appSource,
    /async function broadcastPreparedPrivacy[\s\S]*const preBroadcastError = noBroadcastAttemptError\(error\)[\s\S]*markPreparedReservationReplanRequired\(data, preBroadcastError/,
  );
  assert.match(
    batchFlow,
    /const noBroadcastAttempt = Boolean\(error\?\.noBroadcastAttempt\);[\s\S]*Batch was not submitted; ready after the reported fix[\s\S]*The batch was not submitted to the chain/,
  );
  assert.match(
    batchFlow,
    /const includedPendingEvidence = Boolean\([\s\S]*Included · evidence pending[\s\S]*Batch transfer included; verification pending[\s\S]*The chain accepted this batch/,
  );
  assert.match(
    appSource,
    /Recipient: \$\{payment\.recipient\}[\s\S]*Disclosure: \$\{disclosure\}/,
  );
  assert.match(serverSource, /pathname === "\/v1\/proofs\/batch-transfer"/);
  assert.match(serverSource, /batchTransfer: config\.enableBatchTransfer/);
  assert.match(webClientConfigSchema.properties.serverFeatures.properties.batchTransfer.type, /boolean/);
});

test("DApp keeps minimal-denom amount inputs as integer strings", () => {
  assert.match(appSource, /function amountInputValue/);
  assert.doesNotMatch(appSource, /Number\(input\.value/);
  assert.match(appSource, /BigInt\(raw\)/);
});

test("DApp disables value-moving actions for zero or invalid minimal-denom amounts", () => {
  const updateActions = sourceBetween(appSource, "function updateAmountActionButtons", "function renderMyKeplrNotes");
  const withdrawAction = sourceBetween(appSource, "async function withdrawFromVeiled", "async function relayWithdrawFromVeiled");
  const relayWithdrawAction = sourceBetween(appSource, "async function relayWithdrawFromVeiled", "async function relayPreparedWithdraw");
  assert.match(appSource, /function hasPositiveUclairInput/);
  assert.match(appSource, /amount <= 0n/);
  assert.match(updateActions, /sendFromKeplr\.disabled =[\s\S]*!signerReady[\s\S]*!hasPositiveUclairInput\(els\.keplrSendAmount\)[\s\S]*!isSendRecipientForWallet\([\s\S]*els\.keplrSendRecipient\.value/);
  assert.match(updateActions, /depositFromKeplr\.disabled =[\s\S]*!signerReady[\s\S]*!chainSafetyReady[\s\S]*!hasPositiveUclairInput\(els\.keplrDepositAmount\)[\s\S]*!hasDepositProofProvider\(\)/);
  assert.doesNotMatch(
    updateActions,
    /depositFromKeplr\.disabled =[\s\S]*state\.keplr\.privacySetupFailed/,
  );
  assert.match(updateActions, /const chainSafetyReady = isChainSafetyReady\(\);[\s\S]*const spendChainReady = isSpendChainReady\(\)/);
  assert.match(updateActions, /transferFromVeiled\.disabled =[\s\S]*!veiledReady \|\| !spendChainReady \|\| !hasPositiveUclairInput\(els\.veiledTransferAmount\)/);
  assert.match(updateActions, /withdrawFromVeiled\.disabled =[\s\S]*!veiledReady[\s\S]*!spendChainReady[\s\S]*!hasPositiveUclairInput\(els\.veiledWithdrawAmount\)[\s\S]*isSendRecipientForWallet\([\s\S]*els\.veiledWithdrawRecipient\.value/);
  assert.match(updateActions, /relayWithdrawFromVeiled\.disabled =[\s\S]*!veiledReady[\s\S]*!spendChainReady[\s\S]*!hasPositiveUclairInput\(els\.relayWithdrawAmount\)[\s\S]*isSendRecipientForWallet\([\s\S]*els\.relayWithdrawRecipient\.value/);
  assert.doesNotMatch(relayWithdrawAction, /serverFeature\("relayer"\)/);
  assert.match(appSource, /keplrSendAmount,[\s\S]*keplrSendRecipient,[\s\S]*veiledWithdrawAmount,[\s\S]*veiledWithdrawRecipient,[\s\S]*relayWithdrawAmount,[\s\S]*relayWithdrawRecipient,[\s\S]*addEventListener\("input", updateAmountActionButtons\)/);
  assert.match(withdrawAction, /amount = amountInputValue\(els\.veiledWithdrawAmount\);/);
  assert.match(withdrawAction, /recipient = requireValidPrivacyWithdrawRecipient\([\s\S]*els\.veiledWithdrawRecipient\.value/);
  assert.match(withdrawAction, /setBusy\(els\.withdrawFromVeiled/);
  assert.match(relayWithdrawAction, /amount = amountInputValue\(els\.relayWithdrawAmount\);/);
  assert.match(relayWithdrawAction, /recipient = requireValidPrivacyWithdrawRecipient\([\s\S]*els\.relayWithdrawRecipient\.value,[\s\S]*"Relay withdraw recipient"/);
  assert.match(relayWithdrawAction, /setBusy\(els\.relayWithdrawFromVeiled/);
  assert.match(appSource, /function requireValidPrivacyWithdrawRecipient/);
  assert.match(appSource, /value, label = "Withdraw recipient"/);
  assert.match(appSource, /\$\{label\} must be a 0x address/);
});

test("DApp renders scanned note fields as text instead of HTML", () => {
  const noteRow = sourceBetween(
    appSource,
    "function appendPrivacyNoteRow",
    "function renderMyKeplrNotes",
  );
  assert.match(noteRow, /amount\.textContent = `\$\{note\.amount\}\$\{noteAssetDenom\(note\) \|\| "unknown-asset"\}`/);
  assert.match(noteRow, /\{ statusLabel = noteStatusLabel\(note\) \} = \{\}/);
  assert.match(noteRow, /status\.textContent = statusLabel/);
  assert.match(noteRow, /nullifier\.textContent = shorten\(note\.nullifier, 12, 10\)/);
  assert.doesNotMatch(noteRow, /innerHTML/);
  assert.match(appSource, /appendPrivacyNoteRow\(els\.myKeplrNotesList, note\)/);
  assert.match(appSource, /function localSignerNoteStatusLabel/);
  assert.match(appSource, /function isRenderableLocalSignerNote/);
  assert.match(appSource, /const notes = \(data\.notes \|\| \[\]\)\.filter\(isRenderableLocalSignerNote\)/);
  assert.match(appSource, /appendPrivacyNoteRow\(els\.notesList, note, \{[\s\S]*statusLabel: localSignerNoteStatusLabel\(note\)/);
});

test("DApp serializes value-moving actions from the initial click", () => {
  const updateActions = sourceBetween(
    appSource,
    "function updateAmountActionButtons",
    "function renderMyKeplrNotes",
  );
  const deposit = sourceBetween(
    appSource,
    "async function depositFromKeplr",
    "async function scanKeplrNotes",
  );
  const publicSend = sourceBetween(
    appSource,
    "async function sendFromKeplr",
    "async function depositFromKeplr",
  );
  const faucet = sourceBetween(
    appSource,
    "async function fundKeplr",
    "async function setupKeplrPrivacy",
  );
  assert.match(appSource, /let depositInFlight = false/);
  assert.match(appSource, /let depositInFlightLock = null/);
  assert.match(appSource, /let privacySetupInFlight = null/);
  assert.match(appSource, /let privacyValueActionLock = null/);
  assert.match(updateActions, /const privacyActionBusy = isPrivacyValueActionInFlight\(\)/);
  assert.match(updateActions, /sendFromKeplr\.disabled =[\s\S]*privacyActionBusy/);
  assert.match(appSource, /fundKeplr\.disabled =\s*isPrivacyValueActionInFlight\(\)/);
  assert.match(publicSend, /const actionLock = beginPrivacyValueAction\("public_send", session\);\s*if \(!actionLock\) return;\s*setBusy\(els\.sendFromKeplr, true\);\s*renderKeplr\(\);/);
  assert.match(publicSend, /finally \{\s*endPrivacyValueAction\(actionLock\);/);
  assert.match(faucet, /const actionLock = beginPrivacyValueAction\("faucet", session\);\s*if \(!actionLock\) return;[\s\S]*setBusy\(els\.fundKeplr, true\);\s*renderKeplr\(\);/);
  assert.match(faucet, /finally \{\s*endPrivacyValueAction\(actionLock\);/);
  assert.match(updateActions, /depositFromKeplr\.disabled =[\s\S]*privacyActionBusy[\s\S]*depositInFlight/);
  assert.match(deposit, /if \(depositInFlight\) return;\s*const actionLock = beginPrivacyValueAction\("deposit", session\);\s*if \(!actionLock\) return;\s*const depositLock = Object\.freeze\(\{ generation: session\.generation \}\);\s*depositInFlight = true;\s*depositInFlightLock = depositLock;\s*setBusy\(els\.depositFromKeplr, true\);/);
  assert.match(deposit, /const privacySetupReady = await setupKeplrPrivacy\(\);/);
  assert.match(deposit, /finally \{[\s\S]*if \(depositInFlightLock === depositLock\) \{\s*depositInFlight = false;\s*depositInFlightLock = null;\s*\}[\s\S]*endPrivacyValueAction\(actionLock\);\s*if \(!isPrivacySessionCurrent\(session\)\) return;/);
  for (const [flow, action, button] of [
    [sourceBetween(appSource, "async function transferFromVeiled", "async function withdrawFromVeiled"), "transfer", "transferFromVeiled"],
    [sourceBetween(appSource, "async function withdrawFromVeiled", "function beginRelayWithdrawPreparation"), "withdraw", "withdrawFromVeiled"],
    [sourceBetween(appSource, "async function relayWithdrawFromVeiled", "async function relayPreparedWithdraw"), "relay_withdraw", "relayWithdrawFromVeiled"],
  ]) {
    assert.match(flow, new RegExp(`const actionLock = beginPrivacyValueAction\\("${action}", session\\);\\s*if \\(!actionLock\\) return;\\s*setBusy\\(els\\.${button}, true\\);\\s*try \\{[\\s\\S]*await setupKeplrPrivacy\\(\\);`));
    assert.match(flow, /finally \{\s*endPrivacyValueAction\(actionLock\);/);
  }
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function invalidateFailedPrivacySetup",
  );
  assert.match(privacyInvalidation, /relayWithdrawPayloadCopyInFlight = false;\s*relayWithdrawPayloadCopyLock = null;\s*relaySubmissionInFlight = false;\s*relaySubmissionLock = null;\s*relayHandoffBoundaryLock = null;\s*depositInFlight = false;\s*depositInFlightLock = null;\s*noteScanInFlight = false;\s*noteScanLock = null;\s*noteScanResetInFlight = false;\s*noteScanResetLock = null;\s*privacySetupInFlight = null;\s*privacyValueActionLock = null;/);
  const setupPrivacy = sourceBetween(
    appSource,
    "async function setupKeplrPrivacy",
    "async function copyKeplrDisclosurePubKey",
  );
  assert.match(setupPrivacy, /const activeSetup = privacySetupInFlight;\s*if \(activeSetup\?\.generation === session\.generation\) \{\s*return activeSetup\.promise;/);
  assert.match(setupPrivacy, /privacySetupInFlight = \{ lock, generation: session\.generation, promise \};\s*void runKeplrPrivacySetup\(session\)\.then\(/);
  assert.match(setupPrivacy, /if \(privacySetupInFlight\?\.lock === lock\) \{\s*privacySetupInFlight = null;/);
  assert.match(privacyInvalidation, /noteScanResetInFlight = false;\s*noteScanResetLock = null;\s*privacySetupInFlight = null;\s*privacyValueActionLock = null;/);
});

test("DApp serializes relay handoff and local submission before recovery preflight", () => {
  const relayAdapter = sourceBetween(
    appSource,
    "async function relayPreparedWithdrawPayload",
    "async function broadcastPrivacyDeposit",
  );
  const relaySubmission = sourceBetween(
    appSource,
    "async function relayPreparedWithdraw",
    'els.connectWallet.addEventListener',
  );
  const relayCopy = sourceBetween(
    appSource,
    "async function copyRelayWithdrawPayload",
    "function noBroadcastAttemptError",
  );
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function invalidateFailedPrivacySetup",
  );
  assert.match(appSource, /let relaySubmissionInFlight = false/);
  assert.match(appSource, /let relaySubmissionLock = null/);
  assert.match(appSource, /let relayHandoffBoundaryLock = null/);
  assert.match(appSource, /const pendingRelayRecoveryLocks = new Map\(\)/);
  assert.match(appSource, /function beginRelayHandoffBoundary/);
  assert.match(appSource, /function endRelayHandoffBoundary/);
  assert.match(appSource, /function isRelayHandoffBoundaryInFlight/);
  assert.match(appSource, /function beginPendingRelayRecovery\(id, session = beginPrivacySessionOperation\(\)\)[\s\S]*pendingRelayRecoveryLocks\.has\(key\)/);
  assert.match(appSource, /function endPendingRelayRecovery\(lock\)[\s\S]*pendingRelayRecoveryLocks\.get\(lock\.key\) === lock/);
  assert.match(relayCopy, /const handoffBoundaryLock = beginRelayHandoffBoundary\(\s*"copy",\s*session,\s*expectedVersion,\s*\);\s*if \(!handoffBoundaryLock\) return;/);
  assert.match(relayCopy, /finally \{\s*endRelayHandoffBoundary\(handoffBoundaryLock\);/);
  assert.match(relaySubmission, /if \(relaySubmissionInFlight\) return;/);
  assert.match(relayAdapter, /expectedResponseUrl: "\/api\/relayer\/withdraw"/);
  assert.match(relayAdapter, /responseLabel: "Local relay response"/);
  assert.match(relayAdapter, /timeoutMs: relaySubmissionRequestTimeoutMs/);
  assert.match(relayAdapter, /redirect: "error"/);
  assert.match(relaySubmission, /const handoffBoundaryLock = beginRelayHandoffBoundary\(\s*"local_submit",\s*session,\s*expectedPayloadVersion,\s*\);\s*if \(!handoffBoundaryLock\) return;/);
  assert.match(relaySubmission, /const submissionLock = Object\.freeze\(\{[\s\S]*generation: session\.generation,[\s\S]*payloadVersion: expectedPayloadVersion,[\s\S]*payload,/);
  assert.match(relaySubmission, /relaySubmissionInFlight = true;\s*relaySubmissionLock = submissionLock;\s*setBusy\(els\.relayPreparedWithdraw, true\);\s*renderKeplr\(\);/);
  assert.match(relaySubmission, /finally \{\s*endRelayHandoffBoundary\(handoffBoundaryLock\);\s*if \(relaySubmissionLock === submissionLock\) \{\s*relaySubmissionInFlight = false;\s*relaySubmissionLock = null;/);
  assert.match(privacyInvalidation, /privacySessionGeneration \+= 1;[\s\S]*relaySubmissionInFlight = false;\s*relaySubmissionLock = null;\s*relayHandoffBoundaryLock = null;/);
  assert.match(privacyInvalidation, /privacyValueActionLock = null;[\s\S]*pendingRelayRecoveryLocks\.clear\(\);/);
});

test("DApp stops and session-binds prepared relay lease heartbeats", () => {
  const heartbeat = sourceBetween(
    appSource,
    "function stopPreparedRelayReservationHeartbeat",
    "async function extendReservationBatchLeaseToPayloadExpiry",
  );
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function invalidateFailedPrivacySetup",
  );
  assert.match(appSource, /let preparedRelayReservationHeartbeatGeneration = 0/);
  assert.match(privacyInvalidation, /stopPreparedRelayReservationHeartbeat\(\);[\s\S]*privacySessionGeneration \+= 1;/);
  assert.match(heartbeat, /preparedRelayReservationHeartbeatGeneration \+= 1;/);
  assert.match(heartbeat, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(heartbeat, /assertPrivacySessionCurrent\(session\);/);
  assert.match(heartbeat, /const expectedPayloadVersion = state\.keplr\.relayWithdrawPayloadVersion;/);
  assert.match(heartbeat, /const heartbeatGeneration = preparedRelayReservationHeartbeatGeneration;/);
  assert.match(heartbeat, /heartbeatGeneration !== preparedRelayReservationHeartbeatGeneration\) \{\s*return;/);
  assert.match(heartbeat, /!isPrivacySessionCurrent\(session\)[\s\S]*state\.keplr\.relayWithdrawPayloadVersion !== expectedPayloadVersion/);
  assert.match(heartbeat, /renewReservationBatchLease\(current, \{ session \}\)/);
  assert.match(heartbeat, /if \(heartbeatGeneration === preparedRelayReservationHeartbeatGeneration\) \{\s*preparedRelayReservationHeartbeatInFlight = null;/);
});

test("DApp extends relay handoff leases without trusting the browser clock", () => {
  const relayLeaseExtension = sourceBetween(
    appSource,
    "async function extendReservationBatchLeaseToPayloadExpiry",
    "async function withReservationHeartbeat",
  );
  assert.match(relayLeaseExtension, /const expiresAtMs = Number\(expiresAtUnix\) \* 1000;/);
  assert.doesNotMatch(relayLeaseExtension, /Date\.now\(\)/);
  assert.match(relayLeaseExtension, /manager\.renewLease\(ids, \{[\s\S]*leaseUntil: new Date\(expiresAtMs\)\.toISOString\(\)/);
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
  assert.match(appSource, /const localSigner =[\s\S]*selectedLocalAccount\(\)\?\.name/);
  assert.doesNotMatch(htmlSource, /Fund amount/);
  assert.doesNotMatch(htmlSource, /Fund Wallet/);
  assert.match(htmlSource, /<button id="fundKeplr" type="button" disabled>Faucet<\/button>/);
  assert.match(appSource, /fundKeplr\.disabled =\s*isPrivacyValueActionInFlight\(\) \|\| !serverFeature\("faucet"\) \|\| !signerReady/);
  assert.doesNotMatch(appSource, /fundKeplr\.disabled = !signerReady \|\| state\.activeWallet === "metamask"/);
  assert.match(serverSource, /function normalizeFaucetAmount/);
  assert.match(serverSource, /function sendEvmFaucet/);
  assert.match(serverSource, /import \{ FetchRequest, JsonRpcProvider, Wallet \} from "ethers"/);
  assert.match(serverSource, /Wallet\.fromPhrase/);
  assert.match(serverSource, /function boundedEvmJsonRpcProvider/);
  assert.match(serverSource, /const provider = boundedEvmJsonRpcProvider\(\)/);
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

test("DApp exposes a Clair balance refresh button", () => {
  assert.match(htmlSource, /id="myClairBalance"/);
  assert.match(htmlSource, /id="refreshClairBalance"[\s\S]*Refresh/);
  assert.match(appSource, /refreshClairBalance: \$\("#refreshClairBalance"\)/);
  assert.match(appSource, /refreshClairBalance\.disabled = !connected/);
  assert.match(appSource, /els\.refreshClairBalance\.addEventListener\("click"[\s\S]*refreshWalletBalance\(\)/);
  assert.match(cssSource, /\.balance-refresh-button\s*\{/);
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
  assert.match(serverSource, /return \[isEvmTransport\(\) \? evmProfile : clairveilProfile\]/);
  assert.equal(staticConfig.chainProfiles.length, 1);
  assert.equal(staticConfig.chainProfiles[0].id, "clairveil-local");
  assert.equal(staticConfig.chainProfiles[0].wallet, "keplr");
  assert.doesNotMatch(configSource, /globalThis\.CLAIRVEIL_DAPP_CONFIG/);
  assert.match(readmeSource, /EVM static profile example/);
  assert.match(readmeSource, /"id": "my-evm"/);
  assert.match(readmeSource, /"chainProfiles": \[/);
  assert.match(serverSource, /const chainProfiles = dappChainProfiles\(proverUrl\)/);
  assert.match(serverSource, /chainProfiles,/);
  assert.match(appSource, /function activeChainProfile/);
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

test("DApp uses the versioned Web client profile contract and rejects conflicting config", async () => {
  const {
    loadStaticDappConfig,
    staticDappConfigPath,
    staticDappConfigRequestTimeoutMs,
    staticDappConfigResponseMaxBytes,
  } = await import("../public/dapp-config.js");
  let loadedPath = "";
  let loadedOptions = null;
  const config = await loadStaticDappConfig({
    fetchImpl: async (path, options) => {
      loadedPath = path;
      loadedOptions = options;
      return new Response(JSON.stringify(staticConfig), {
        status: 200,
        headers: { "content-type": "application/json; charset=utf-8" },
      });
    },
  });
  const activeProfile = config.chainProfiles.find(
    profile => profile.id === config.activeChainProfileId,
  );
  assert.equal(loadedPath, staticDappConfigPath);
  assert.equal(loadedOptions.redirect, "error");
  assert.ok(loadedOptions.signal instanceof AbortSignal);
  assert.equal(staticDappConfigPath, "/dapp-config.json");
  assert.equal(staticDappConfigRequestTimeoutMs, 30_000);
  assert.equal(staticDappConfigResponseMaxBytes, 1 << 20);
  assert.equal(webClientConfigSchema.$id, "https://github.com/DELIGHT-LABS/clairveil/docs/schemas/clairveil-web-client-config.schema.json");
  assert.equal(config.schemaVersion, "clairveil-web-client-config-v1");
  assert.ok(activeProfile);
  assert.equal(config.chainId, activeProfile.chainId);
  assert.equal(config.rpc, activeProfile.rpc);
  assert.equal(config.rest, activeProfile.rest);
  assert.equal(config.proverUrl, activeProfile.proverUrl);
  assert.ok(
    webClientConfigSchema.$defs.url.allOf.some(
      (rule) => rule.not?.pattern === "^https?://[^/?#]*@",
    ),
  );
  assert.match(appSource, /function assertValidatedDappConfig/);
  assert.match(appSource, /url\.username \|\| url\.password \|\| url\.search \|\| url\.hash/);
  assert.match(appSource, /endpoint\.username \|\|[\s\S]*endpoint\.password/);
  assert.match(appSource, /endpoint\.search \|\|[\s\S]*endpoint\.hash/);
  assert.match(appSource, /function assertWebClientProfileSchema/);
  assert.match(appSource, /assertNoUnexpectedConfigFields/);
  assert.match(appSource, /profile\.transport === "cosmos"/);
  assert.match(appSource, /profile\.transport === "evm"/);
  assert.match(appSource, /duplicate or empty profile IDs/);
  assert.match(appSource, /disagrees with active profile/);
  assert.match(appSource, /function sameConfigValue/);
  const configValidator = sourceBetween(
    appSource,
    "function assertValidatedDappConfig",
    "function activeChainProfile",
  );
  assert.match(
    configValidator,
    /config\[field\] !== undefined[\s\S]*active\[field\] === undefined \|\| String\(config\[field\]\) !== String\(active\[field\]\)/,
  );
  assert.match(
    appSource,
    /config\.keplrChainInfo !== undefined[\s\S]*!sameConfigValue\(config\.keplrChainInfo, active\.keplrChainInfo\)/,
  );
  assert.match(appSource, /dappConfigValidationFailure/);
  assert.match(appSource, /WebApp configuration is invalid; sync is unavailable/);
  const browserClientFactory = sourceBetween(
    appSource,
    "function clairveilBrowserClient",
    "const chainSafetyRefreshIntervalMs",
  );
  assert.match(
    browserClientFactory,
    /const key = JSON\.stringify\([\s\S]*transport: resolved\?\.transport \|\| config\?\.transport \|\| "",[\s\S]*evmGasLimit:[\s\S]*evmSendGasLimit:/,
  );
  assert.match(
    browserClientFactory,
    /evmGasLimit: resolved\?\.evmGasLimit \|\| state\.config\?\.evmGasLimit,[\s\S]*evmSendGasLimit:[\s\S]*resolved\?\.evmSendGasLimit \|\| state\.config\?\.evmSendGasLimit/,
  );
  const serverHealthBootstrap = sourceBetween(
    appSource,
    "async function loadDappHealth",
    "function addressSuggestionConfigs",
  );
  assert.match(
    serverHealthBootstrap,
    /const healthTask = \(task\) =>[\s\S]*const data = await healthTask\(\(\) =>[\s\S]*api\("\/api\/health",[\s\S]*assertValidatedDappConfig\(data\.config\);[\s\S]*return ensureLocalSignersIfNeeded\(data, \{ healthView \}\);/,
  );
  assert.match(appSource, /healthTask\(\(\) => loadStaticDappConfig\(\)\)/);
  assert.match(appSource, /function assertJsonApiResponse/);
  assert.match(appSource, /accept: "application\/json"/);
  assert.match(appSource, /if \(response\.ok\) \{\s*assertJsonApiResponse\(response, path, responseLabel\);/);
  assert.match(appSource, /error\.apiResponseContentType = apiResponseContentType\(response\)/);
  assert.match(
    appSource,
    /const error = new ApiError\([\s\S]*error\.apiPath = String\(path\);[\s\S]*error\.apiResponseContentType = apiResponseContentType\(response\);[\s\S]*throw error;/,
  );
  assert.match(appSource, /error\?\.apiInvalidJsonResponse === true[\s\S]*error\?\.apiPath === "\/api\/health"[\s\S]*text\\\/html/);
  assert.match(appSource, /const depositProofRequestTimeoutMs = 120_000/);
  assert.match(appSource, /const depositProofResponseMaxBytes = 1 << 20/);
  assert.match(appSource, /const depositProofResponseVersion = "v1"/);
  assert.match(appSource, /const dappApiRequestTimeoutMs = 30_000/);
  assert.match(appSource, /const dappApiResponseMaxBytes = 1 << 20/);
  assert.match(appSource, /const healthBootstrapRequestTimeoutMs = 30_000/);
  assert.match(appSource, /const healthBootstrapResponseMaxBytes = 1 << 20/);
  assert.match(appSource, /timeoutMs: healthBootstrapRequestTimeoutMs/);
  assert.match(appSource, /maxResponseBytes: healthBootstrapResponseMaxBytes/);
  assert.match(appSource, /expectedResponseUrl: "\/api\/health"/);
  assert.match(appSource, /const timedOutHealth = error\?\.code === "request_timeout"/);
  assert.match(appSource, /timeoutMs: depositProofRequestTimeoutMs/);
  assert.match(appSource, /maxResponseBytes: depositProofResponseMaxBytes/);
  assert.match(appSource, /expectedResponseUrl: proofEndpoint/);
  assert.match(appSource, /responseLabel: "Deposit proof response"/);
  assert.match(appSource, /redirect: "error"/);
  assert.match(appSource, /function assertDirectApiResponse/);
  assert.match(appSource, /function readBoundedApiResponseText/);
  assert.match(
    appSource,
    /async function api\(path, options = \{\}\) \{[\s\S]*timeoutMs = dappApiRequestTimeoutMs,[\s\S]*maxResponseBytes = dappApiResponseMaxBytes,/,
  );
  assert.match(configSource, /export const staticDappConfigPath = "\/dapp-config\.json"/);
  assert.match(configSource, /cache: "no-store"/);
  assert.match(configSource, /assertDirectStaticConfigResponse/);
  assert.match(configSource, /assertJsonConfigResponse/);
  assert.match(configSource, /readBoundedStaticConfigText/);
  assert.match(configSource, /staticDappConfigRequestTimeoutMs = 30_000/);
  await assert.rejects(
    () => loadStaticDappConfig({
      locationHref: "https://app.example.com/",
      fetchImpl: async () => ({
        ok: true,
        redirected: true,
        url: "https://untrusted.example/dapp-config.json",
        json: async () => staticConfig,
      }),
    }),
    /must not redirect/,
  );
  await assert.rejects(
    () => loadStaticDappConfig({
      locationHref: "https://app.example.com/",
      fetchImpl: async () => ({
        ok: true,
        redirected: false,
        url: "https://untrusted.example/dapp-config.json",
        json: async () => staticConfig,
      }),
    }),
    /same-origin \/dapp-config\.json artifact/,
  );
  await assert.rejects(
    () => loadStaticDappConfig({
      fetchImpl: async () => new Response(JSON.stringify(staticConfig), {
        status: 200,
        headers: { "content-type": "text/html" },
      }),
    }),
    /must return Content-Type: application\/json/,
  );
  await assert.rejects(
    () => loadStaticDappConfig({
      maxResponseBytes: 32,
      fetchImpl: async () => new Response("x".repeat(33), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    }),
    /exceeds 32 byte limit/,
  );
  await assert.rejects(
    () => loadStaticDappConfig({
      timeoutMs: 1,
      fetchImpl: (_path, options) => new Promise((_, reject) => {
        options.signal.addEventListener("abort", () => reject(new Error("aborted")), {
          once: true,
        });
      }),
    }),
    (error) => error?.code === "request_timeout",
  );
  assert.doesNotMatch(configSource, /CLAIRVEIL_DAPP_CONFIG/);
  assert.match(serverSource, /schemaVersion: "clairveil-web-client-config-v1"/);
  assert.match(serverSource, /url\.username \|\|[\s\S]*url\.password[\s\S]*url\.search \|\|[\s\S]*url\.hash/);
  assert.match(deploymentGateSource, /url\.username \|\|[\s\S]*url\.password[\s\S]*url\.search \|\|[\s\S]*url\.hash/);
  assert.match(serverSource, /const healthUpstreamRequestTimeoutMs = 30_000/);
  assert.match(serverSource, /const healthUpstreamResponseMaxBytes = 1 << 20/);
  assert.match(serverSource, /async function readBoundedResponseText/);
  assert.match(readmeSource, /WebApp scope/);
  assert.match(readmeSource, /one-proof atomic batch transfer/);
});

test("DApp fails closed on current chain configuration and encrypted browser storage", () => {
  assert.match(appSource, /function refreshChainSafety/);
  assert.match(appSource, /function scheduleChainSafetyExpiry/);
  assert.match(appSource, /Date\.now\(\) - state\.chainSafety\.checkedAt < chainSafetyRefreshIntervalMs/);
  assert.match(appSource, /assertTransferProtocolConfig\(profile\.denom\)/);
  assert.match(appSource, /queryReserve\(profile\.denom\)/);
  assert.match(appSource, /chain status, tree, and protocol queries must all succeed/);
  assert.match(appSource, /await assertChainSafetyBeforePrivacyFlow\(\{ session \}\)/);
  assert.match(appSource, /new EncryptedIndexedDbNoteStore/);
  assert.match(appSource, /createEncryptedBrowserMetadataStore/);
  assert.match(appSource, /function invalidateFailedPrivacySetup/);
  assert.match(appSource, /rootSignatureBase64: ""/);
  assert.match(appSource, /privacySetupFailed: true/);
  const localNotes = sourceBetween(
    appSource,
    "async function refreshNotes",
    "async function refreshEvents",
  );
  assert.match(
    localNotes,
    /if \(!isChainSafetyReady\(\)\) \{[\s\S]*spendableTotal\.textContent = "Sync unavailable";[\s\S]*Notes are unavailable until current chain configuration is verified\./,
  );
  const setupPrivacy = sourceBetween(
    appSource,
    "async function setupKeplrPrivacy",
    "async function copyKeplrDisclosurePubKey",
  );
  const deposit = sourceBetween(
    appSource,
    "async function depositFromKeplr",
    "async function scanKeplrNotes",
  );
  const resetScan = sourceBetween(
    appSource,
    "async function resetAndRescanKeplrNotes",
    "async function scanKeplrNotes",
  );
  const noteScan = sourceBetween(
    appSource,
    "async function scanKeplrNotes",
    "async function refreshPrivacySurfaces",
  );
  const typedPreparationScan = sourceBetween(
    appSource,
    "async function assertTypedPrivacyScanBeforePreparation",
    "function profileSessionFingerprint",
  );
  const transferPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyTransferSignDoc",
    "async function preparePrivacyWithdrawSignDoc",
  );
  const withdrawPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyWithdrawSignDoc",
    "async function preparePrivacyRelayWithdrawPayload",
  );
  const relayPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyRelayWithdrawPayload",
    "async function relayPreparedWithdrawPayload",
  );
  assert.match(setupPrivacy, /await loadPersistedRelayWithdrawPayloadState\(\{ session \}\);[\s\S]*invalidateFailedPrivacySetup\(\)/);
  assert.match(setupPrivacy, /await hydratePersistedWalletNotes\(\{ session \}\);[\s\S]*invalidateFailedPrivacySetup\(\)/);
  assert.match(deposit, /const privacySetupReady = await setupKeplrPrivacy\(\);[\s\S]*!privacySetupReady \|\| !state\.keplr\.rootSignatureBase64/);
  assert.match(appSource, /await scanKeplrNotes\(\{[\s\S]*quiet: true,[\s\S]*skipPrivacySetup: true,[\s\S]*throwOnError: true,[\s\S]*session,/);
  assert.match(appSource, /function invalidatePrivacyScanState/);
  assert.match(appSource, /function privacySyncErrorMessage\(error\)/);
  assert.match(appSource, /function privacyPostScanErrorMessage\(error\)/);
  assert.match(appSource, /function privacySetupErrorMessage\(error\)/);
  assert.match(appSource, /state\.keplr\.scanError = privacySyncErrorMessage\(error\);/);
  assert.match(
    noteScan,
    /The typed scan and its encrypted note record are already complete\.[\s\S]*state\.keplr\.scanError = privacyPostScanErrorMessage\(error\);[\s\S]*return scan;/,
  );
  assert.match(resetScan, /await scanKeplrNotes\(\{ reset: true, session \}\);[\s\S]*toast\(privacySyncErrorMessage\(error\)\);/);
  assert.doesNotMatch(resetScan, /toast\(error\.message\)/);
  assert.match(appSource, /function assertSpendableNotesSyncReady/);
  assert.match(appSource, /function isSpendChainReady\(\)/);
  assert.match(appSource, /privacy tree has not been initialized yet/);
  assert.match(appSource, /discardStaleNotesForUninitializedPrivacyTree/);
  assert.match(appSource, /Cached notes from an earlier local chain were discarded/);
  assert.match(appSource, /Current privacy tree is empty; Deposit and Scan Notes before spending/);
  assert.match(appSource, /relayMetadataStore\.clear\(\)/);
  assert.match(appSource, /if \(isSpendChainReady\(\)\) \{[\s\S]*loadPersistedRelayWithdrawPayloadState/);
  assert.match(appSource, /assertSpendableNotesSyncReady\(\);[\s\S]*prepareTransfer/);
  assert.match(appSource, /function hasCompletedPrivacyNoteScan/);
  assert.match(appSource, /function hasPersistedTypedScanCursor/);
  assert.match(appSource, /function persistedTypedScanCursor\(cached\)/);
  assert.match(appSource, /cached\?\.scanCursor \?\? cached\?\.scan_cursor/);
  assert.match(appSource, /if \(!hasPersistedTypedScanCursor\(cached\)\) \{[\s\S]*await withPrivacySessionGuard\(session, \(\) => store\.clear\(\)\)/);
  assert.match(appSource, /cursor\.source === "privacy_scan" \|\|[\s\S]*cursor\.scan_source === "privacy_scan"/);
  assert.match(appSource, /nextCursor\.global_sequence != null \|\| nextCursor\.globalSequence != null/);
  assert.match(appSource, /nextCursor\.output_index != null \|\| nextCursor\.outputIndex != null/);
  assert.match(appSource, /cursor\.completed[\s\S]*!cursor\.hasMore/);
  assert.match(appSource, /requireComplete: true/);
  assert.match(appSource, /Privacy note sync did not complete within the \$\{completeNoteScanMaxPages\}-page safety limit/);
  assert.match(htmlSource, /id="resetKeplrNotes"/);
  assert.match(appSource, /resetKeplrNotes: \$\("#resetKeplrNotes"\)/);
  assert.match(appSource, /els\.scanKeplrNotes\.textContent = noteScanBusy[\s\S]*"Scanning notes…"[\s\S]*state\.keplr\.scanError[\s\S]*"Retry scan"/);
  assert.match(appSource, /els\.resetKeplrNotes\.hidden = !state\.keplr\.scanError;/);
  assert.match(resetScan, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(resetScan, /window\.confirm\(/);
  assert.match(resetScan, /active reservations are not released/);
  assert.match(resetScan, /await scanKeplrNotes\(\{ reset: true, session \}\);/);
  assert.match(resetScan, /assertPrivacySessionCurrent\(session\);/);
  assert.match(resetScan, /noteScanResetLock === lock/);
  assert.match(noteScan, /if \(noteScanInFlight\) return;/);
  assert.match(noteScan, /if \(noteScanInFlight\) return;[\s\S]*await refreshChainSafety\(\{ force: true, session \}\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*if \(noteScanInFlight\) return;/);
  assert.match(noteScan, /noteScanLock === lock/);
  assert.match(typedPreparationScan, /privacyRequest\(\{[\s\S]*\.\.\.noteScanRequestOptions\(\{ requireComplete: true \}\),[\s\S]*includeFoundNotes: true/);
  assert.match(typedPreparationScan, /clairveilBrowserClient\(\)\.scanWalletNotes/);
  assert.match(typedPreparationScan, /String\(cursor\.source \|\| ""\) !== "privacy_scan"/);
  assert.match(typedPreparationScan, /No wallet request was sent/);
  assert.match(typedPreparationScan, /cursor\.has_more === true[\s\S]*cursor\.hasMore === true[\s\S]*cursor\.completed !== true/);
  assert.match(typedPreparationScan, /noteStore\.replaceScanResult\(scan, \{[\s\S]*owner: state\.keplr\.shieldedAddress/);
  assert.match(typedPreparationScan, /applyPersistedWalletNoteState\(cached\)/);
  assert.match(typedPreparationScan, /await reconcileReservedNotesFromScan\(\{ session \}\)/);
  assert.match(typedPreparationScan, /await refreshNoteReservationState\(\{ session \}\)/);
  for (const prepare of [transferPrepare, withdrawPrepare, relayPrepare]) {
    assert.match(prepare, /await assertTypedPrivacyScanBeforePreparation\(session\);[\s\S]*assertPrivacySessionCurrent\(session\);/);
    assert.match(prepare, /scan: \{ limit: 200, maxPages: 1000, scanSource: "privacy_scan" \}/);
  }
  assert.match(
    relayPrepare,
    /await assertTypedPrivacyScanBeforePreparation\(session\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*const chainSnapshot = await withPrivacySessionGuard\([\s\S]*latestRelayChainSnapshot\(\)[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*const reservationManager = currentNoteReservationManager\(\)/,
  );
  assert.doesNotMatch(htmlSource, /clearLegacyPrivacyData/);
  assert.doesNotMatch(
    appSource,
    /clearLegacyPrivacyData|legacyMigrationMarker|legacyReservationNamespace|legacyLocalStorageKeys/,
  );
  assert.doesNotMatch(appSource, /LocalStorageNoteStore/);
  assert.doesNotMatch(appSource, /unsafeAllowMemoryFallback/);
  assert.match(serverSource, /config\.localTestMode[\s\S]*config\.enableProverProxy[\s\S]*isDirectLoopbackRequest/);
  assert.match(serverSource, /function staticSecurityHeaders/);
  assert.match(serverSource, /content-security-policy/);
  assert.match(serverSource, /x-content-type-options/);
  assert.match(serverSource, /assertProductionDeploymentConfig/);
  assert.match(appSource, /function assertBrowserDeploymentEndpointPolicy/);
  assert.match(appSource, /assertBrowserDeploymentEndpointPolicy\(config\)/);
  const deploymentEndpointPolicy = sourceBetween(
    appSource,
    "function assertBrowserDeploymentEndpointPolicy",
    "function browserRestEndpoints",
  );
  assert.match(deploymentEndpointPolicy, /if \(directLoopbackPage\) \{\s*return;/);
  assert.doesNotMatch(deploymentEndpointPolicy, /explicitlyEnabledLocalServer/);
  assert.match(appSource, /restEndpoints\[\$\{index\}\]/);
  assert.match(appSource, /function browserRestEndpoints/);
  assert.match(appSource, /restEndpoints: browserRestEndpoints\(resolved\)/);
  assert.match(appSource, /restEndpoints: browserRestEndpoints\(profile\)/);
  assert.doesNotMatch(appSource, /url\.hostname = window\.location\.hostname/);
  assert.match(appSource, /must use a public HTTPS endpoint outside direct loopback local development/);
  assert.match(appSource, /function assertKeplrChainInfoMatchesProfile/);
  assert.match(appSource, /keplrChainInfo\.chainId must match profile chainId/);
  assert.match(appSource, /keplrChainInfo\.chainName must match profile chainName/);
  assert.match(appSource, /keplrChainInfo\.rpc must match profile rpc/);
  assert.match(appSource, /keplrChainInfo\.rest must match profile rest/);
  assert.match(appSource, /keplrChainInfo\.bip44\.coinType must match profile keplrCoinType/);
  assert.match(appSource, /keplrChainInfo\.bech32Config\.\$\{field\} must match profile accountPrefix/);
  assert.match(appSource, /must contain exactly one configured currency/);
  assert.match(appSource, /must match profile currency/);
  assert.match(appSource, /gasPriceStep must match profile gasPriceStep/);
  const cosmosProfileRule = webClientConfigSchema.$defs.profile.allOf.find(
    (rule) => rule.if?.properties?.transport?.const === "cosmos",
  );
  assert.ok(cosmosProfileRule?.then?.required?.includes("keplrChainInfo"));
  const keplrChainInfoSchema = webClientConfigSchema.$defs.keplrChainInfo;
  assert.deepEqual(keplrChainInfoSchema.required, [
    "chainId", "chainName", "rpc", "rest", "bip44", "bech32Config",
    "currencies", "feeCurrencies", "stakeCurrency", "features",
  ]);
  assert.equal(keplrChainInfoSchema.additionalProperties, false);
  assert.equal(keplrChainInfoSchema.properties.currencies.maxItems, 1);
  assert.equal(keplrChainInfoSchema.properties.feeCurrencies.maxItems, 1);
  assert.equal(
    webClientConfigSchema.$defs.profile.properties.keplrChainInfo.$ref,
    "#/$defs/keplrChainInfo",
  );
  const evmProfileRule = webClientConfigSchema.$defs.profile.allOf.find(
    (rule) => rule.if?.properties?.transport?.const === "evm",
  );
  assert.ok(
    evmProfileRule?.then?.not?.anyOf?.some((rule) =>
      rule.required?.includes("keplrChainInfo"),
    ),
  );
  assert.match(
    appSource,
    /active\.transport === "evm" && config\.keplrChainInfo !== undefined/,
  );
  assert.match(
    appSource,
    /EVM profile \$\{profile\.id\} must not include Keplr wallet configuration/,
  );
  assert.match(appSource, /body = await readBoundedApiResponseText\(response, boundedResponseBytes\)/);
  assert.match(
    appSource,
    /const healthEndpointAbsent =[\s\S]*error\?\.statusCode === 404[\s\S]*error\?\.code === "not_found"[\s\S]*error\?\.data\?\.version === "v1"/,
  );
  assert.match(
    appSource,
    /const staticHealthEndpointAbsent =[\s\S]*error\?\.statusCode === 404[\s\S]*error\?\.apiPath === "\/api\/health"[\s\S]*text\\\/html/,
  );
  assert.match(
    appSource,
    /error\?\.statusCode &&[\s\S]*!healthEndpointAbsent &&[\s\S]*!staticHealthEndpointAbsent/,
  );
  assert.match(
    appSource,
    /const staticHealthFallback =[\s\S]*staticHealthEndpointAbsent[\s\S]*error\?\.apiInvalidJsonResponse === true/,
  );
  assert.match(
    appSource,
    /let staticHealthEndpointConfirmed = false;[\s\S]*if \(healthEndpointAbsent \|\| staticHealthFallback\) \{[\s\S]*staticHealthEndpointConfirmed = true;/,
  );
  assert.match(
    appSource,
    /const config = await healthTask\(\(\) => loadStaticDappConfig\(\)\);[\s\S]*if \(staticHealthEndpointConfirmed\) \{[\s\S]*config\.serverBacked !== false[\s\S]*serverConfigAvailable = false;/,
  );
});

test("DApp renews an expired chain-safety lease in the current privacy session", () => {
  const chainSafetyExpiry = sourceBetween(
    appSource,
    "function scheduleChainSafetyExpiry",
    "function isChainSafetyReady",
  );
  assert.match(
    chainSafetyExpiry,
    /const refresh = \{[\s\S]*generation: chainSafetyRefreshGeneration,[\s\S]*key: state\.chainSafety\.key,[\s\S]*session: beginPrivacySessionOperation\(\),/,
  );
  assert.match(
    chainSafetyExpiry,
    /if \(!isChainSafetyRefreshCurrent\(refresh\) \|\| isChainSafetyReady\(\)\) return;[\s\S]*void refreshChainSafety\(\{ force: true, session: refresh\.session \}\)\.catch\(\(\) => \{\}\);/,
  );
  assert.doesNotMatch(
    chainSafetyExpiry,
    /if \(!isChainSafetyReady\(\)\) renderKeplr\(\);/,
  );
});

test("DApp console diagnostics do not retain raw privacy-operation errors", () => {
  assert.match(appSource, /console\.warn\("clairveil_relay_metadata_persistence_failed"\)/);
  assert.match(appSource, /console\.warn\("clairveil_reservation_bookkeeping_failed"\)/);
  assert.doesNotMatch(
    appSource,
    /console\.warn\("Encrypted Clairveil relay metadata persistence failed", error\)/,
  );
  assert.doesNotMatch(
    appSource,
    /console\.warn\("Clairveil reservation bookkeeping failed", error\)/,
  );
});

test("DApp does not serialize Clairveil SDK error details", () => {
  const payloadSerializer = sourceBetween(
    serverSource,
    "function errorPayload(",
    "function readBody(req)",
  );
  assert.match(
    payloadSerializer,
    /if \(error\?\.privacySensitive\) \{[\s\S]*return privacyOperationErrorPayload\(error\);[\s\S]*if \(error instanceof ClairveilError\)[\s\S]*error: safeClairveilErrorMessage\(error\.code\),[\s\S]*code: error\.code,[\s\S]*version: "v1"/,
  );
  assert.doesNotMatch(payloadSerializer, /error: error\.message/);
  assert.doesNotMatch(payloadSerializer, /error\.details/);
  assert.match(serverSource, /function privacyOperationErrorPayload\(error\)[\s\S]*error: code === "proof_failed"[\s\S]*"privacy operation failed"[\s\S]*version: "v1"/);
  assert.match(serverSource, /function relayWithdrawSafeFailureCode\(error\)[\s\S]*RELAY_PAYLOAD_EXPIRED[\s\S]*RELAY_INPUT_UNAVAILABLE[\s\S]*RELAY_SUBMISSION_FAILED/);
  assert.ok(
    payloadSerializer.indexOf("if (error?.privacySensitive)") <
      payloadSerializer.indexOf("if (error instanceof ClairveilError)"),
  );
  assert.match(serverSource, /let privacyOperationSensitive = false;/);
  assert.match(serverSource, /privacyOperationSensitive = true;[\s\S]*runDepositProof/);
  assert.match(serverSource, /privacyOperationSensitive = true;[\s\S]*let responseError = privacyOperationSensitive[\s\S]*privacySensitiveOperationError\(error\)[\s\S]*url\.pathname === "\/api\/relayer\/withdraw"[\s\S]*relayWithdrawSafeFailureCode\(responseError\)[\s\S]*errorPayload\(responseError\)/);
});

test("DApp renders prover failures with stable privacy-safe messages", () => {
  const depositProvider = sourceBetween(
    appSource,
    "async function preparePrivacyDepositSignDoc",
    "async function preparePrivacyTransferSignDoc",
  );
  assert.match(appSource, /function privacyOperationErrorMessage\(error, fallback = "Privacy operation failed"\)/);
  assert.match(appSource, /error\?\.code \|\| error\?\.proverCode/);
  assert.match(
    appSource,
    /case "unauthorized":\s*return "Privacy proof service authorization failed\. Verify the configured provider access and retry\."/,
  );
  assert.match(
    appSource,
    /case "not_found":\s*case "method_not_allowed":\s*return "Privacy proof service endpoint is unavailable\. Verify the configured provider URL and retry\."/,
  );
  assert.match(
    appSource,
    /function insufficientBalancePlannerMessage\(error\)[\s\S]*plannerFactsFromError\(error\)[\s\S]*Reserved, spent, unverified, and other-asset notes are excluded/,
  );
  assert.match(
    appSource,
    /case "INSUFFICIENT_BALANCE":\s*return insufficientBalancePlannerMessage\(error\);/,
  );
  assert.match(
    appSource,
    /case "RELAY_PAYLOAD_EXPIRED":\s*return "Relay payload expired before local submission\. Prepare a fresh relay payload\."/,
  );
  assert.match(
    appSource,
    /case "RELAY_INPUT_UNAVAILABLE":\s*return "Relay input notes are no longer unspent\. Scan Notes and prepare a fresh relay payload\."/,
  );
  assert.match(
    appSource,
    /case "RELAY_SUBMISSION_FAILED":\s*return "The local relayer could not submit the payload and no transaction was confirmed\./,
  );
  assert.match(
    appSource,
    /const relaySubmissionRequestTimeoutMs = 155_000;/,
  );
  assert.match(
    appSource,
    /case "request_timeout":\s*return "The local relayer did not return before the timeout\./,
  );
  assert.match(
    appSource,
    /case "PROVER_CANCELLED":\s*return "Privacy proof preparation was cancelled after the wallet session changed\./,
  );
  assert.match(
    appSource,
    /case "self_merge_required":\s*return "Note preparation needs a self transaction before the transfer\. Approve the requested self transaction, then retry\."/,
  );
  assert.match(
    appSource,
    /Prepared transfer \(payload\|effect\) is missing[\s\S]*Prepared transfer details were incomplete\. Refresh Notes and retry\./,
  );
  assert.match(
    appSource,
    /Cosmos sign doc does not match the reservation ProofReady artifact[\s\S]*Keplr changed the prepared transaction during signing\./,
  );
  assert.match(depositProvider, /catch \(error\) \{[\s\S]*throw safeDepositProofProviderError\(error\);/);
  assert.match(
    appSource,
    /const recognizedCode = new Set\(\[[\s\S]*"unauthorized",[\s\S]*"not_found",[\s\S]*"method_not_allowed",[\s\S]*\]\)/,
  );
  assert.match(appSource, /finishTransferFlow\(privacyOperationErrorMessage\(error\), false\)/);
  const depositFlow = sourceBetween(
    appSource,
    "async function depositFromKeplr",
    "async function resetAndRescanKeplrNotes",
  );
  assert.match(depositFlow, /error\?\.depositConfirmationRequired[\s\S]*"Deposit confirmation required"/);
  assert.match(depositFlow, /depositConfirmedOnChain[\s\S]*"Deposit recovery required"/);
  assert.match(depositFlow, /privacyOperationErrorMessage\(error\)/);
  assert.match(appSource, /privacyReservationErrorCode\([\s\S]*"privacy_session_invalidated_before_external_boundary"[\s\S]*reconcile_reason: "privacy_session_changed_before_external_boundary"/);
  const relaySubmission = sourceBetween(
    appSource,
    "async function relayPreparedWithdraw()",
    'els.connectWallet.addEventListener',
  );
  assert.match(relaySubmission, /privacyOperationErrorMessage\([\s\S]*Relay withdraw failed\. Refresh Notes to reconcile before retrying\./);
  assert.doesNotMatch(relaySubmission, /toast\(error\.message\)/);
});

test("DApp keeps typed-scan and setup failures privacy-safe", () => {
  const noteScan = sourceBetween(
    appSource,
    "async function scanKeplrNotes",
    "async function refreshPrivacySurfaces",
  );
  const setupPrivacy = sourceBetween(
    appSource,
    "async function setupKeplrPrivacy",
    "async function copyKeplrDisclosurePubKey",
  );
  const scanInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacyScanState",
    "function isRecoverableEncryptedNoteCacheError",
  );
  assert.match(noteScan, /const stagedError = privacyScanStageFailure\(error, scanStage\);[\s\S]*const scanFailureMessage = privacySyncErrorMessage\(stagedError\);[\s\S]*invalidatePrivacyScanState\(stagedError\);[\s\S]*toast\(scanFailureMessage\);/);
  assert.doesNotMatch(noteScan, /toast\(error\.message\)/);
  assert.doesNotMatch(scanInvalidation, /error\?\.message|String\(error/);
  assert.match(
    appSource,
    /function privacySyncErrorText\(error\)[\s\S]*for \(let depth = 0; current && depth < 3; depth \+= 1\)[\s\S]*current = current\?\.cause;/,
  );
  assert.match(
    appSource,
    /if \(\/web locks api\/i\.test\(message\)\)[\s\S]*current Chrome, Edge, or Firefox window/,
  );
  assert.match(
    appSource,
    /if \(\/web crypto\/i\.test\(message\)\)[\s\S]*current browser/,
  );
  assert.match(
    appSource,
    /function privacyScanStageFailure\(error, stage\)[\s\S]*wrapped\.privacyScanStage = stage/,
  );
  assert.match(
    appSource,
    /case "typed-query":[\s\S]*typed privacy-scan response could not be accepted[\s\S]*case "encrypted-cache":[\s\S]*Scanned notes could not be saved[\s\S]*case "cursor-validation":[\s\S]*cursor was not safe to resume[\s\S]*case "scan-completion":[\s\S]*typed scan was complete/,
  );
  assert.match(setupPrivacy, /toast\(privacySetupErrorMessage\(error\)\);/);
  assert.match(
    setupPrivacy,
    /if \(error\?\.chainSafetyFailure\) \{[\s\S]*state\.keplr\.privacySetupFailed = false;[\s\S]*toast\(privacySyncErrorMessage\(error\)\);[\s\S]*return false;/,
  );
  assert.match(
    noteScan,
    /try \{\s*await refreshChainSafety\(\{ force: true, session \}\);[\s\S]*catch \(error\) \{[\s\S]*const stagedError = privacyScanStageFailure\(error, "chain-safety"\);[\s\S]*invalidatePrivacyScanState\(stagedError\);[\s\S]*if \(options\.throwOnError\) throw stagedError;[\s\S]*return;/,
  );
  assert.match(
    appSource,
    /scanKeplrNotes\(\)\.catch\(\(error\) => \{[\s\S]*if \(!error\?\.privacySessionInvalidated\) \{[\s\S]*toast\(privacySyncErrorMessage\(error\)\);/,
  );
});

test("DApp keeps relay handoff failures privacy-safe", () => {
  const relayHandoffClick = sourceBetween(
    appSource,
    'els.copyRelayWithdrawPayload.addEventListener',
    'els.relayPreparedWithdraw.addEventListener',
  );
  assert.match(appSource, /function privacyRelayHandoffErrorMessage\(error\)/);
  assert.match(
    appSource,
    /if \(error\?\.relayHandoffRecorded\) \{[\s\S]*Relay handoff was recorded, but the payload could not be copied/,
  );
  assert.match(
    appSource,
    /privacyRecoveryErrorMessage\([\s\S]*Relay payload handoff could not complete/,
  );
  assert.match(relayHandoffClick, /toast\(privacyRelayHandoffErrorMessage\(error\)\)/);
  assert.doesNotMatch(relayHandoffClick, /toast\(error\.message\)/);

  const relayDraftDiscard = sourceBetween(
    appSource,
    '[els.relayWithdrawAmount, els.relayWithdrawRecipient].forEach',
    'els.veiledDisclosureAdvanced.addEventListener',
  );
  assert.match(
    relayDraftDiscard,
    /privacyRecoveryErrorMessage\([\s\S]*Prepared relay payload could not be discarded safely/,
  );
  assert.doesNotMatch(relayDraftDiscard, /toast\(error\.message\)/);
});

test("DApp keeps user disclosure decode failures privacy-safe", () => {
  const disclosureDecode = sourceBetween(
    appSource,
    "async function decodeSelectedEventDisclosure",
    "function clearAuditorReport",
  );
  assert.match(appSource, /function privacyDisclosureErrorMessage\(error\)/);
  assert.match(disclosureDecode, /clairveilBrowserClient\(\)\.decodeUserDisclosure\([\s\S]*privacyRequest\(\{ txHash: event\.tx_hash_hex \}\)/);
  assert.match(disclosureDecode, /clairveilBrowserClient\(\)\.decodeSelfViewDisclosure\([\s\S]*keplrPrivacyRequest\(\{ txHash: event\.tx_hash_hex \}\)/);
  assert.match(disclosureDecode, /state\.privacyEvents\.error = privacyDisclosureErrorMessage\(error\);/);
  assert.doesNotMatch(disclosureDecode, /state\.privacyEvents\.error = error\.message/);
});

test("DApp pins and redacts local auditor disclosure decode", () => {
  const auditorDecode = sourceBetween(
    appSource,
    "async function decodeAuditorTransfer",
    "function canConnectWallet",
  );
  const auditorRoute = sourceBetween(
    serverSource,
    'if (req.method === "POST" && url.pathname === "/api/auditor/decode")',
    'if (req.method === "POST" && url.pathname === "/api/faucet")',
  );
  assert.match(auditorDecode, /expectedResponseUrl: "\/api\/auditor\/decode"/);
  assert.match(auditorDecode, /responseLabel: "Local auditor decode response"/);
  assert.match(auditorDecode, /redirect: "error"/);
  assert.match(
    appSource,
    /verification\.verified \?\? summary\.verified \?\? report\?\.verified/,
  );
  assert.match(appSource, /verificationResult === true/);
  assert.match(htmlSource, /id="auditorDecodeState" class="quiet" aria-live="polite"/);
  assert.match(auditorDecode, /clearAuditorReport\(privacyDisclosureErrorMessage\(error\)\);/);
  assert.doesNotMatch(auditorDecode, /clearAuditorReport\(error\.message\);/);
  assert.match(
    auditorRoute,
    /privacyOperationSensitive = true;[\s\S]*const body = await readBody\(req\);/,
  );
  assert.match(
    auditorRoute,
    /const event = auditorEventReference\(body\);[\s\S]*event\.eventType === "batch_transfer"[\s\S]*batchAuditOutputsForEvent\(event\)[\s\S]*clairveil\.decodeBatchAuditDisclosure\([\s\S]*event_type: "batch_transfer"/,
  );
  assert.match(
    appSource,
    /function renderBatchAuditorReport[\s\S]*renderBatchAuditorOutputReports\(outputs\)/,
  );
  assert.match(htmlSource, /id="auditorOutputReports" class="audit-output-reports" hidden/);
});

test("DApp minimizes and pins local auditor scalar material", () => {
  const auditorScalar = sourceBetween(
    appSource,
    "async function refreshAuditorTestScalar",
    "function updateAuditorDecodeButton",
  );
  const auditorScalarRoute = sourceBetween(
    serverSource,
    'if (req.method === "GET" && url.pathname === "/api/auditor/test-scalar")',
    '// Test/admin-only route. Public DApps must not receive or relay audit disclosure private scalars.',
  );
  assert.match(auditorScalar, /expectedResponseUrl: "\/api\/auditor\/test-scalar"/);
  assert.match(auditorScalar, /responseLabel: "Local auditor scalar response"/);
  assert.match(auditorScalar, /redirect: "error"/);
  assert.match(
    auditorScalar,
    /state\.auditor\.testScalarError = "Unavailable: local auditor test scalar could not be loaded\.";/,
  );
  assert.doesNotMatch(auditorScalar, /error\.message/);
  assert.match(
    auditorScalarRoute,
    /privacyOperationSensitive = true;[\s\S]*disclosure_private_scalar_hex: material\.disclosure_private_scalar_hex,[\s\S]*matches_audit_config:/,
  );
  assert.doesNotMatch(auditorScalarRoute, /\.\.\.material/);
  assert.doesNotMatch(auditorScalarRoute, /root_seed_hex|root_signature_base64|root_signing_message/);
  const auditorRefresh = sourceBetween(
    appSource,
    "if (els.refreshAuditorTransfers)",
    "if (els.decodeAuditorTransfer)",
  );
  assert.match(
    auditorRefresh,
    /Promise\.all\(\[[\s\S]*refreshAuditorTransfers\(\),[\s\S]*refreshAuditorTestScalar\(\),[\s\S]*\]\)/,
  );
});

test("DApp loads local auditor transfers through its same-origin admin route", () => {
  const auditorTransfers = sourceBetween(
    appSource,
    "async function refreshAuditorTransfers",
    "function selectAuditorTransfer",
  );
  assert.match(auditorTransfers, /`\/api\/auditor\/transfers\?page=\$\{requestedPage\}&limit=\$\{auditorEventsPageLimit\}`/);
  assert.match(auditorTransfers, /const transferPath =/);
  assert.match(auditorTransfers, /expectedResponseUrl: transferPath/);
  assert.match(auditorTransfers, /responseLabel: "Local auditor transfers response"/);
  assert.match(auditorTransfers, /redirect: "error"/);
  assert.match(auditorTransfers, /state\.auditor\.hasMore = Boolean\(data\.has_more \?\? data\.hasMore\)/);
  assert.match(
    auditorTransfers,
    /state\.auditor\.loading = false;[\s\S]*renderAuditorTransfers\(\);/,
  );

  const auditorTransfersRoute = sourceBetween(
    serverSource,
    'if (req.method === "GET" && url.pathname === "/api/auditor/transfers")',
    "// Test/admin-only route. Public DApps must not receive or relay audit disclosure private scalars.",
  );
  assert.match(auditorTransfersRoute, /assertLocalTestBackendAllowed\("auditor transfers"\)/);
  assert.match(auditorTransfersRoute, /assertLocalAdminAccessAllowed\(req\)/);
  assert.match(auditorTransfersRoute, /fetchAuditorTransferEvents\(\{[\s\S]*page: paginationInteger\(url\.searchParams\.get\("page"\)/);
  assert.match(
    serverSource,
    /async function fetchAuditorTransferEvents[\s\S]*clairveil\.fetchPrivacyEvents\(\{[\s\S]*eventTypes: \["shielded_transfer", "batch_transfer"\][\s\S]*\}\)[\s\S]*events: \(result\.events \|\| \[\]\)\.filter\(isAuditablePrivacyEvent\)/,
  );
  assert.match(
    serverSource,
    /async function batchAuditOutputsForEvent\(\{ txHash, height, sequence \}\)[\s\S]*typedBatchScanStartCursor\(\{ height, sequence \}\)[\s\S]*clairveil\.fetchAuditableBatchTransfers\([\s\S]*outputLimit: auditorBatchScanOutputLimit/,
  );
  assert.doesNotMatch(serverSource, /fetchAllAuditableBatchTransfers/);
});

test("DApp paginates privacy and auditor event history without preloading typed batch outputs", () => {
  assert.match(htmlSource, /id="previousEventsPage"/);
  assert.match(htmlSource, /id="nextEventsPage"/);
  assert.match(htmlSource, /id="previousAuditorPage"/);
  assert.match(htmlSource, /id="nextAuditorPage"/);
  assert.match(
    appSource,
    /client\.fetchPrivacyEvents\(\{[\s\S]*page: requestedPage,[\s\S]*limit: privacyEventsPageLimit/,
  );
  assert.match(appSource, /function renderPrivacyEventPagination/);
  assert.match(appSource, /function renderAuditorPagination/);
  assert.match(
    appSource,
    /eventType: event\.event_type,[\s\S]*eventHeight: event\.height,[\s\S]*eventSequence: event\.sequence,[\s\S]*eventPage: state\.auditor\.page/,
  );
});

test("DApp clears event pagination controls with the privacy session", () => {
  const resetAuditor = sourceBetween(
    appSource,
    "function resetAuditorSession",
    "function clearPrivacyOperationDrafts",
  );
  const resetEvents = sourceBetween(
    appSource,
    "function resetPrivacyEventSession",
    "function resetBlockEventSession",
  );
  assert.match(resetAuditor, /state\.auditor = defaultAuditorState\(\);[\s\S]*renderAuditorPagination\(\);/);
  assert.match(resetEvents, /privacyEventsRefreshGeneration \+= 1;/);
  assert.match(resetEvents, /state\.privacyEvents = defaultPrivacyEventsState\(\);[\s\S]*renderPrivacyEventPagination\(\);/);
});

test("DApp persists only stable reservation error codes for private operations", () => {
  const invalidatedPreparationCleanup = sourceBetween(
    appSource,
    "async function replanInvalidatedPreparedReservation",
    "async function finishPrivacyPreparation",
  );
  const reservationTransitions = sourceBetween(
    appSource,
    "async function markPreparedReservationBroadcastRejected",
    "function selectedNotesFromPlan",
  );
  const reservationFactory = sourceBetween(
    appSource,
    "function currentNoteReservationManager",
    "function currentWalletNoteStore",
  );
  const recoveredReservations = sourceBetween(
    appSource,
    "async function reconcileRecoveredActiveReservations",
    "async function reconcileDefiniteFailedUnknownReservations",
  );
  assert.match(appSource, /function privacyReservationErrorCode\(error, fallback = "privacy_operation_failed"\)/);
  assert.match(
    invalidatedPreparationCleanup,
    /privacyReservationErrorCode\([\s\S]*"privacy_session_invalidated_before_external_boundary"/,
  );
  assert.doesNotMatch(
    invalidatedPreparationCleanup,
    /privacyOperationErrorMessage\(error\)/,
  );
  assert.match(appSource, /function createPrivacySafeReservationManager\(manager\)/);
  assert.match(appSource, /const proxy = new Proxy\(manager/);
  assert.match(appSource, /privacyReservationErrorCode\(source\.error, fallback\)/);
  assert.match(reservationFactory, /createPrivacySafeReservationManager\([\s\S]*createNoteReservationManager/);
  assert.match(reservationTransitions, /privacyReservationErrorCode\(error, "wallet_request_rejected"\)/);
  assert.match(reservationTransitions, /privacyReservationErrorCode\(error, "broadcast_outcome_unknown"\)/);
  assert.match(reservationTransitions, /privacyReservationErrorCode\([\s\S]*"pre_broadcast_operation_cancelled"/);
  assert.match(reservationTransitions, /privacyReservationErrorCode\(error, "manual_review_required"\)/);
  assert.doesNotMatch(reservationTransitions, /error:\s*error\?\.message/);
  for (const code of [
    "recovered_operation_status_mismatch",
    "recovered_post_broadcast_missing_input_nullifier_evidence",
    "recovered_broadcast_attempt_missing_input_nullifier_evidence",
    "recovered_local_prepare_lost_before_broadcast",
    "recovered_broadcast_attempt_requires_reconciliation",
    "recovered_proof_ready_without_pre_broadcast_evidence",
    "recovered_transaction_not_found_before_deadline",
    "recovered_transaction_result_unverified",
    "recovered_transaction_success_nullifier_conflict",
    "recovered_transaction_failed_nullifiers_unspent",
  ]) {
    assert.match(recoveredReservations, new RegExp(`error:\\s*[^,;]+${code}`));
  }
  assert.doesNotMatch(
    recoveredReservations,
    /error:\s*(?:"[^"]*\s[^"]*"|[^,;]+\?\s*"[^"]*\s[^"]*")/,
  );
  assert.doesNotMatch(appSource, /first\.last_broadcast_error/);
});

test("DApp clears privacy state and storage scope when a validated profile changes", () => {
  const staticHealth = sourceBetween(
    appSource,
    "async function browserHealthFromStaticConfig",
    "async function loadDappHealth",
  );
  const renderHealthSource = sourceBetween(
    appSource,
    "async function renderHealth",
    "async function refreshHealth",
  );
  const reservationScopeSource = sourceBetween(
    appSource,
    "function reservationScope",
    "function currentReservationWorkerID",
  );
  const profilePersistenceScopeSource = sourceBetween(
    appSource,
    "function profilePersistenceScope",
    "function clairveilBrowserClient",
  );
  const profileSessionFingerprintSource = sourceBetween(
    appSource,
    "function profileSessionFingerprint",
    "function shieldedAddressBookProfileFingerprint",
  );
  const profileSelect = sourceBetween(
    appSource,
    "async function selectDappChainProfile",
    "function recipientTestAccounts",
  );
  const profileChange = sourceBetween(
    appSource,
    "async function clearPrivacySessionForProfileChange",
    "async function renderHealth",
  );
  const addressBookScope = sourceBetween(
    appSource,
    "function shieldedAddressBookProfileFingerprint",
    "function profilePersistenceScope",
  );
  const shieldedSuggestion = sourceBetween(
    appSource,
    "function suggestedAddressFor",
    "function transparentDisplayAddressFor",
  );
  const addressBookLoad = sourceBetween(
    appSource,
    "async function ensureShieldedAddressBook",
    "function showAddressSuggestions",
  );
  const addressBookFailure = sourceBetween(
    appSource,
    "function isShieldedAddressBookScopeCurrent",
    "function localAccountViewIdentity",
  );
  const showAddressSuggestion = sourceBetween(
    appSource,
    "function showAddressSuggestions",
    "function setupAddressSuggestions",
  );
  const recipientSuggestionScope = sourceBetween(
    appSource,
    "function hideAllAddressSuggestions",
    "function selectAddressSuggestion",
  );
  const healthView = sourceBetween(
    appSource,
    "function beginHealthView",
    "function profilePersistenceScope",
  );
  const healthRefresh = sourceBetween(
    appSource,
    "async function refreshHealth",
    "async function refreshSelectedAccount",
  );
  const keplrConnect = sourceBetween(
    appSource,
    "async function connectKeplr",
    "async function signKeplrSession",
  );
  assert.match(appSource, /function profileSessionFingerprint/);
  assert.match(appSource, /function profilePersistenceScope/);
  assert.match(
    profileSessionFingerprintSource,
    /evmGasLimit: profile\.evmGasLimit \|\| "",[\s\S]*evmSendGasLimit: profile\.evmSendGasLimit \|\| ""/,
  );
  assert.match(
    profileSessionFingerprintSource,
    /keplrCoinType: profile\.keplrCoinType \?\? null,[\s\S]*gasPriceStep: canonicalConfigValue\(profile\.gasPriceStep \|\| null\),[\s\S]*keplrChainInfo: canonicalConfigValue\(profile\.keplrChainInfo \|\| null\)/,
  );
  assert.match(
    appSource,
    /function canonicalConfigValue\(value\) \{[\s\S]*Object\.keys\(value\)\.sort\(\)/,
  );
  assert.match(
    profileSessionFingerprintSource,
    /\{ config = state\.config \} = \{\},[\s\S]*browserProverUrl\(profile, \{[\s\S]*serverFeatures: config\?\.serverFeatures,[\s\S]*serverBacked: config\?\.serverBacked/,
  );
  assert.match(appSource, /function defaultShieldedAddressBookState\(\)[\s\S]*profileFingerprint/);
  assert.match(reservationScopeSource, /profile: profilePersistenceScope\(profile\)/);
  assert.match(reservationScopeSource, /scope\.chainId\}:\$\{scope\.profile\}/);
  assert.match(profilePersistenceScopeSource, /const resolved = profile \|\| configuredChainProfile\(\);[\s\S]*encodeURIComponent\(profileSessionFingerprint\(resolved\)\)/);
  assert.match(renderHealthSource, /selectedProfileFromConfig\(data\.config\)/);
  assert.match(renderHealthSource, /await clearPrivacySessionForProfileChange/);
  assert.match(renderHealthSource, /ensureShieldedAddressBookScope\(\)/);
  assert.match(renderHealthSource, /previous privacy session was cleared/);
  assert.match(staticHealth, /profileSessionFingerprint\(previousProfile\) !==[\s\S]*profileSessionFingerprint\(profile, \{ config \}\)[\s\S]*clearProfileScopedRecipientSuggestions\(\);[\s\S]*resetWalletSession\(\);/);
  assert.match(staticHealth, /\{ healthView = null \} = \{\}/);
  assert.match(staticHealth, /if \(healthView !== null\) assertHealthViewCurrent\(healthView\);[\s\S]*clearProfileScopedRecipientSuggestions\(\);/);
  assert.match(staticHealth, /withHealthViewGuard\([\s\S]*healthView,[\s\S]*clairveilBrowserClient\(profile, \{ config \}\)\.health\(\{[\s\S]*allowUninitializedTree: true/);
  assert.match(profileChange, /\{ nextConfig = state\.config \} = \{\},[\s\S]*profileSessionFingerprint\(previousProfile\) ===[\s\S]*profileSessionFingerprint\(nextProfile, \{ config: nextConfig \}\)[\s\S]*clearProfileScopedRecipientSuggestions\(\);[\s\S]*resetWalletSession\(\);/);
  assert.match(renderHealthSource, /clearPrivacySessionForProfileChange\([\s\S]*previousProfile,[\s\S]*nextProfile,[\s\S]*nextConfig: data\.config/);
  assert.doesNotMatch(staticHealth, /hasInMemoryPrivacySession/);
  assert.doesNotMatch(profileChange, /hasInMemoryPrivacySession/);
  assert.match(profileSelect, /const profileChanged = state\.selectedChainProfileId !== profileId/);
  assert.match(profileSelect, /state\.selectedChainProfileId = profileId;[\s\S]*if \(profileChanged\) \{[\s\S]*clearProfileScopedRecipientSuggestions\(\);[\s\S]*invalidateHealthView\(\)[\s\S]*ensureShieldedAddressBookScope\(\)/);
  assert.match(appSource, /let healthViewGeneration = 0/);
  assert.match(healthView, /function beginHealthView\(\)/);
  assert.match(healthView, /function withHealthViewGuard\(view, task\)/);
  assert.match(healthView, /function invalidateHealthView\(\)/);
  assert.match(healthRefresh, /const healthView = beginHealthView\(\)/);
  assert.match(healthRefresh, /withHealthViewGuard\([\s\S]*healthView,[\s\S]*loadDappHealth\(\{ healthView \}\)/);
  assert.match(healthRefresh, /renderHealth\(data, \{ healthView \}\)/);
  assert.match(healthRefresh, /withHealthViewGuard\([\s\S]*refreshChainSafety\(\{ force: true \}\)/);
  assert.match(healthRefresh, /withHealthViewGuard\(healthView, \(\) => Promise\.allSettled\(tasks\)\)/);
  assert.match(addressBookScope, /function resetShieldedAddressBook\(\)[\s\S]*shieldedAddressBookGeneration \+= 1;[\s\S]*shieldedAddressBookPromise = null;[\s\S]*profileFingerprint: shieldedAddressBookProfileFingerprint\(\)/);
  assert.match(recipientSuggestionScope, /function clearProfileScopedRecipientSuggestions\(\)[\s\S]*resetShieldedAddressBook\(\);[\s\S]*for \(const config of addressSuggestionConfigs\(\)\) \{[\s\S]*config\.input\.value = "";[\s\S]*config\.list\.replaceChildren\(\);[\s\S]*hideAddressSuggestions\(config\);/);
  assert.match(addressBookScope, /function ensureShieldedAddressBookScope\(\)/);
  assert.match(addressBookFailure, /function isShieldedAddressBookScopeCurrent\(scope\)/);
  assert.match(addressBookFailure, /function recordShieldedAddressBookFailure\(scope, error\)[\s\S]*if \(!isShieldedAddressBookScopeCurrent\(scope\)\) return;/);
  assert.match(shieldedSuggestion, /if \(kind === "shielded"\) \{[\s\S]*isShieldedAddressBookCurrent\(\)[\s\S]*: ""/);
  assert.match(addressBookLoad, /scope = ensureShieldedAddressBookScope\(\)/);
  assert.match(addressBookLoad, /const \{ addressBook, generation \} = scope/);
  assert.match(addressBookLoad, /isShieldedAddressBookScopeCurrent\(scope\)/);
  assert.match(showAddressSuggestion, /const scope = ensureShieldedAddressBookScope\(\);[\s\S]*ensureShieldedAddressBook\(scope\)\.catch\([\s\S]*recordShieldedAddressBookFailure\(scope, error\)/);
  assert.match(renderHealthSource, /const addressBookScope = ensureShieldedAddressBookScope\(\);[\s\S]*ensureShieldedAddressBook\(addressBookScope\)\.catch[\s\S]*recordShieldedAddressBookFailure\(addressBookScope, error\)/);
  assert.doesNotMatch(staticHealth, /state\.config\s*=/);
  assert.doesNotMatch(staticHealth, /state\.chainProfiles\s*=/);
  assert.doesNotMatch(staticHealth, /state\.selectedChainProfileId\s*=/);
  assert.match(appSource, /function invalidatePrivacySessionOperations/);
  assert.match(appSource, /activeReservationHeartbeatStops/);
  assert.match(appSource, /function finishPrivacyPreparation/);
  assert.match(appSource, /privacy_session_changed_before_external_boundary/);
  assert.match(appSource, /const session = preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\);/);
  assert.match(
    keplrConnect,
    /resetWalletSession\(\);[\s\S]*beginWalletConnection\("keplr"\);[\s\S]*state\.activeWallet = "keplr"/,
  );
  assert.doesNotMatch(keplrConnect, /discardAndClearPreparedRelayWithdrawPayload/);
  assert.doesNotMatch(
    keplrConnect,
    /state\.keplr\.relayWithdrawPendingPayloads\s*=/,
  );
});

test("DApp withholds disclosure plaintext unless digest verification succeeds", () => {
  const eventDisclosure = sourceBetween(
    appSource,
    "function renderEventDisclosureReport",
    "function renderEventDetail",
  );
  const auditorDisclosure = sourceBetween(
    appSource,
    "function renderAuditorReport",
    "function renderAuditorTransfers",
  );
  assert.match(eventDisclosure, /const disclosureVerified = verified === true/);
  assert.match(eventDisclosure, /if \(!disclosureVerified\) \{[\s\S]*eventDisclosureAmount\.textContent = "-";[\s\S]*eventDisclosureFrom\.textContent = "-";[\s\S]*Plaintext withheld/);
  assert.match(
    auditorDisclosure,
    /const verificationResult =[\s\S]*verification\.verified \?\? summary\.verified \?\? report\?\.verified;[\s\S]*const disclosureVerified = verificationResult === true/,
  );
  assert.match(auditorDisclosure, /if \(!disclosureVerified\) \{[\s\S]*auditorAmount\.textContent = "-";[\s\S]*auditorFrom\.textContent = "-";[\s\S]*Plaintext withheld/);
});

test("DApp scopes local auditor disclosure material to the profile and admin feature", () => {
  const featureVisibility = sourceBetween(
    appSource,
    "function renderServerFeatureVisibility",
    "function expectedEvmChainIdHex",
  );
  const auditorReset = sourceBetween(
    appSource,
    "function resetAuditorSession",
    "function resetKeplrSession",
  );
  const walletReset = sourceBetween(
    appSource,
    "function resetWalletSession",
    "function currentWalletAccountForCopy",
  );
  const profileChange = sourceBetween(
    appSource,
    "async function clearPrivacySessionForProfileChange",
    "async function renderHealth",
  );
  const scalarRefresh = sourceBetween(
    appSource,
    "async function refreshAuditorTestScalar",
    "function updateAuditorDecodeButton",
  );
  const auditorDecode = sourceBetween(
    appSource,
    "async function decodeAuditorTransfer",
    "function canConnectWallet",
  );

  assert.match(appSource, /function defaultAuditorState/);
  assert.match(featureVisibility, /if \(!auditorAdmin\) \{\s*resetAuditorSession\(\);\s*\}/);
  assert.match(auditorReset, /auditorSessionGeneration \+= 1/);
  assert.match(auditorReset, /state\.auditor = defaultAuditorState\(\)/);
  assert.match(auditorReset, /auditorTestScalar\.textContent = "-"/);
  assert.match(auditorReset, /for \(const element of auditorDetailValueElements\(\)\) \{\s*element\.textContent = "-"/);
  assert.doesNotMatch(walletReset, /resetAuditorSession\(\)/);
  assert.match(
    profileChange,
    /clearProfileScopedRecipientSuggestions\(\);[\s\S]*resetWalletSession\(\);[\s\S]*resetAuditorSession\(\);/,
  );
  assert.match(scalarRefresh, /const generation = auditorSessionGeneration/);
  assert.match(scalarRefresh, /generation !== auditorSessionGeneration\s*\|\|\s*!hasAuditorUi\(\)/);
  assert.match(auditorDecode, /generation !== auditorSessionGeneration/);
  assert.match(auditorDecode, /state\.auditor\.selectedEventKey !== selectedEventKey/);
});

test("DApp clears and invalidates user disclosure results when its privacy session changes", () => {
  const privacyEventReset = sourceBetween(
    appSource,
    "function resetPrivacyEventSession",
    "function resetBlockEventSession",
  );
  const blockEventReset = sourceBetween(
    appSource,
    "function resetBlockEventSession",
    "function resetKeplrSession",
  );
  const keplrReset = sourceBetween(
    appSource,
    "function resetKeplrSession",
    "function resetWalletSession",
  );
  const eventSelection = sourceBetween(
    appSource,
    "function selectPrivacyEvent",
    "function selectedPrivacyEvent",
  );
  const disclosureDecode = sourceBetween(
    appSource,
    "async function decodeSelectedEventDisclosure",
    "function clearAuditorReport",
  );

  assert.match(appSource, /function defaultPrivacyEventsState/);
  assert.match(privacyEventReset, /privacyEventDisclosureGeneration \+= 1/);
  assert.match(privacyEventReset, /state\.privacyEvents = defaultPrivacyEventsState\(\)/);
  assert.match(privacyEventReset, /clearEventDisclosureResult\(\)/);
  assert.match(privacyEventReset, /eventDisclosureState\.textContent = "Privacy session cleared\."/);
  assert.match(blockEventReset, /state\.blockEvents = \{[\s\S]*events: \[\],[\s\S]*error: "",/);
  assert.match(blockEventReset, /blockEventsList\.innerHTML = ""/);
  assert.match(blockEventReset, /blockEventsState\.textContent = "Profile changed; refresh events\."/);
  assert.match(keplrReset, /resetPrivacyEventSession\(\)/);
  assert.match(keplrReset, /resetBlockEventSession\(\)/);
  assert.match(eventSelection, /privacyEventDisclosureGeneration \+= 1/);
  assert.match(disclosureDecode, /privacySession !== privacySessionGeneration/);
  assert.match(disclosureDecode, /disclosureGeneration !== privacyEventDisclosureGeneration/);
  assert.match(disclosureDecode, /state\.privacyEvents\.selectedEventKey !== selectedEventKey/);
});

test("DApp clears private operation drafts when its privacy session ends", () => {
  const draftReset = sourceBetween(
    appSource,
    "function clearPrivacyOperationDrafts",
    "function resetPrivacyEventSession",
  );
  const keplrReset = sourceBetween(
    appSource,
    "function resetKeplrSession",
    "function resetWalletSession",
  );

  assert.match(draftReset, /els\.keplrSendAmount/);
  assert.match(draftReset, /els\.keplrSendRecipient/);
  assert.match(draftReset, /els\.keplrDepositAmount/);
  assert.match(draftReset, /els\.veiledTransferAmount/);
  assert.match(draftReset, /els\.veiledTransferRecipient/);
  assert.match(draftReset, /els\.veiledWithdrawAmount/);
  assert.match(draftReset, /els\.veiledWithdrawRecipient/);
  assert.match(draftReset, /els\.relayWithdrawAmount/);
  assert.match(draftReset, /els\.relayWithdrawRecipient/);
  assert.match(draftReset, /els\.veiledDisclosurePubKey/);
  assert.match(draftReset, /input\.value = ""/);
  assert.match(draftReset, /veiledDisclosureMode\.value = "none"/);
  assert.match(draftReset, /resetTransferPlannerFacts\(\)/);
  assert.match(draftReset, /hideAllAddressSuggestions\(\)/);
  assert.match(keplrReset, /clearPrivacyOperationDrafts\(\)/);
  assert.match(keplrReset, /keplrTxState\.textContent = "Ready"/);
});

test("DApp discards stale privacy setup, receipt, and action completions after a session change", () => {
  const setupPrivacy = sourceBetween(
    appSource,
    "async function setupKeplrPrivacy",
    "async function copyKeplrDisclosurePubKey",
  );
  const noteHydration = sourceBetween(
    appSource,
    "async function hydratePersistedWalletNotes",
    "async function loadPersistedRelayWithdrawPayloadState",
  );
  const relayRecovery = sourceBetween(
    appSource,
    "async function loadPersistedRelayWithdrawPayloadState",
    "function currentPreparedRelayWithdrawSnapshot",
  );
  const noteStatuses = sourceBetween(
    appSource,
    "async function refreshCachedNoteStatuses",
    "async function reconcileReservedNotesFromScan",
  );
  const noteScan = sourceBetween(
    appSource,
    "async function scanKeplrNotes",
    "async function refreshPrivacySurfaces",
  );
  const walletBalanceRefresh = sourceBetween(
    appSource,
    "async function refreshWalletBalance",
    "async function refreshNotes",
  );
  const notesRefresh = sourceBetween(
    appSource,
    "async function refreshNotes",
    "async function refreshEvents",
  );
  const eventsRefresh = sourceBetween(
    appSource,
    "async function refreshEvents",
    "async function refreshBlockEvents",
  );
  const blockEventsRefresh = sourceBetween(
    appSource,
    "async function refreshBlockEvents",
    "function disclosureTargetMatches",
  );
  const privacySurfacesRefresh = sourceBetween(
    appSource,
    "async function refreshPrivacySurfaces",
    "async function transferFromVeiled",
  );
  const depositNoteRecovery = sourceBetween(
    appSource,
    "async function refreshDepositNoteRecovery",
    "async function transferFromVeiled",
  );
  const submittedOperationReconciliation = sourceBetween(
    appSource,
    "async function refreshSubmittedOperationReconciliation",
    "function submittedOperationReconciliationCopy",
  );
  const submittedOperationReport = sourceBetween(
    appSource,
    "function reportSubmittedOperationReconciliation",
    "async function transferFromVeiled",
  );
  const transferFlowModal = sourceBetween(
    appSource,
    "function resetTransferFlowForPrivacySession",
    "function updateTransferFlow",
  );
  const transferFlowFinish = sourceBetween(
    appSource,
    "function finishTransferFlow",
    "function responseContentLength",
  );
  const evmReceiptWatcher = sourceBetween(
    appSource,
    "function watchEvmBroadcast",
    "function keplrPrivacyRequest",
  );
  const reservationBookkeeping = sourceBetween(
    appSource,
    "async function noteReservationBookkeeping",
    "function reservationReconciliationRequiredError",
  );
  const depositFlow = sourceBetween(
    appSource,
    "async function depositFromKeplr",
    "async function scanKeplrNotes",
  );
  const transferFlow = sourceBetween(
    appSource,
    "async function transferFromVeiled",
    "async function withdrawFromVeiled",
  );
  const withdrawFlow = sourceBetween(
    appSource,
    "async function withdrawFromVeiled",
    "function beginRelayWithdrawPreparation",
  );
  const relayPreparationFlow = sourceBetween(
    appSource,
    "async function relayWithdrawFromVeiled",
    "async function relayPreparedWithdraw",
  );
  const exactWithdrawNote = sourceBetween(
    appSource,
    "async function createExactWithdrawNote",
    "async function sendFromKeplr",
  );
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function invalidateFailedPrivacySetup",
  );
  const transferFlowReset = sourceBetween(
    appSource,
    "function resetTransferFlowForPrivacySession",
    "function setTransferFlowStep",
  );
  const reservationReconciliation = sourceBetween(
    appSource,
    "async function reconcileReservedNotesFromScan",
    "function reservationOperationID",
  );
  const reservationStateRefresh = sourceBetween(
    appSource,
    "async function refreshNoteReservationState",
    "async function resolveGeneralManualReviewOperation",
  );
  const submittedReservation = sourceBetween(
    appSource,
    "async function recordSubmittedReservation",
    "async function markPreparedReservationBroadcastAttempting",
  );
  const manualReviewResolution = sourceBetween(
    appSource,
    "async function resolveGeneralManualReviewOperation",
    "function selectedLocalAccount",
  );
  const manualReviewUi = sourceBetween(
    appSource,
    "function renderReservationManualReviews",
    "function renderAccounts",
  );
  const recoveredTxOutcome = sourceBetween(
    appSource,
    "async function recoveredReservationTxOutcome",
    "function successfulTxNullifierConflictIsMature",
  );
  const recoveredReservationRecovery = sourceBetween(
    appSource,
    "async function reconcileRecoveredActiveReservations",
    "async function reconcileDefiniteFailedUnknownReservations",
  );
  const definiteFailureRecovery = sourceBetween(
    appSource,
    "async function reconcileDefiniteFailedUnknownReservations",
    "async function refreshNoteReservationState",
  );
  const failedReservationRecovery = sourceBetween(
    appSource,
    "async function reconcileDefiniteFailedReservation",
    "async function reconcileFailedEvmReservation",
  );
  const relayNullifierStatuses = sourceBetween(
    appSource,
    "async function checkNullifierSpent",
    "async function verifyRelayPayloadNullifierUnspentBeforeBroadcast",
  );
  const relayExpiryRecovery = sourceBetween(
    appSource,
    "async function reconcileExpiredRelayWithdrawSnapshot",
    "async function reconcileDefiniteFailedReservation",
  );
  const relayManualReviewResolution = sourceBetween(
    appSource,
    "async function resolveExpiredRelayManualReview",
    "async function reconcileExpiredRelayWithdrawPayloads",
  );
  const relayMetadataPersistence = sourceBetween(
    appSource,
    "function persistRelayWithdrawPayloadState",
    "async function latestReservationRecords",
  );
  const relayPayloadClear = sourceBetween(
    appSource,
    "async function clearPreparedRelayWithdrawPayload",
    "function renderPendingRelayWithdrawPayloads",
  );
  const chainSafetyPreflight = sourceBetween(
    appSource,
    "async function refreshChainSafety",
    "async function assertChainSafetyBeforePrivacyFlow",
  );
  const relayDraftHandler = sourceBetween(
    appSource,
    "[els.relayWithdrawAmount, els.relayWithdrawRecipient].forEach",
    "els.veiledDisclosureAdvanced.addEventListener",
  );
  const eventsHandler = sourceBetween(
    appSource,
    "els.refreshEvents.addEventListener",
    "els.decodeEventDisclosure.addEventListener",
  );
  const scanHandler = sourceBetween(
    appSource,
    "els.scanKeplrNotes.addEventListener",
    "els.resetKeplrNotes.addEventListener",
  );

  assert.match(appSource, /function isPrivacySessionCurrent/);
  assert.match(appSource, /async function withPrivacySessionGuard\(session, task\)[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*const result = await task\(\);[\s\S]*assertPrivacySessionCurrent\(session\)/);
  assert.match(setupPrivacy, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(setupPrivacy, /let signatureBase64;/);
  assert.match(setupPrivacy, /assertPrivacySessionCurrent\(session\);[\s\S]*state\.keplr\.rootSignatureBase64 = signatureBase64/);
  assert.match(setupPrivacy, /await hydratePersistedWalletNotes\(\{ session \}\);[\s\S]*isRecoverableEncryptedNoteCacheError\(error\)[\s\S]*invalidatePrivacyScanState\(error\)[\s\S]*privacySetupFailed = false/);
  assert.match(setupPrivacy, /Encrypted note cache recovery required/);
  assert.match(setupPrivacy, /if \(error\?\.privacySessionInvalidated\) return false/);
  assert.match(setupPrivacy, /finally \{[\s\S]*if \(isPrivacySessionCurrent\(session\)\) \{[\s\S]*setBusy\(els\.setupKeplrPrivacy, false\);[\s\S]*renderKeplr\(\);/);
  assert.match(noteHydration, /async function hydratePersistedWalletNotes\(\{ session = null \} = \{\}\)/);
  assert.match(noteHydration, /const cached = await withPrivacySessionGuard\(session, \(\) => store\.load\(\)\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*applyPersistedWalletNoteState\(cached\)/);
  assert.match(noteHydration, /if \(!hasPersistedTypedScanCursor\(cached\)\) \{[\s\S]*await withPrivacySessionGuard\(session, \(\) => store\.clear\(\)\);/);
  assert.match(relayRecovery, /async function loadPersistedRelayWithdrawPayloadState\(\{ session = null \} = \{\}\)/);
  assert.match(relayRecovery, /withPrivacySessionGuard\(session, \(\) => storage\.load\(\)\)[\s\S]*assertPrivacySessionCurrent\(session\)/);
  assert.match(relayRecovery, /syncRelayWithdrawSnapshotReservation\(snapshot, \{ session \}\)/);
  assert.match(relayRecovery, /recoverActiveRelayWithdrawSnapshots\(\{ session \}\)/);
  assert.match(relayRecovery, /reconcileExpiredRelayWithdrawPayloads\(null, \{ session \}\)/);
  assert.match(relayRecovery, /persistRelayWithdrawPayloadState\(\{ session \}\)/);
  assert.match(noteStatuses, /async function refreshCachedNoteStatuses\(\{ session = null, noteStore = null \} = \{\}\)/);
  assert.match(noteStatuses, /assertPrivacySessionCurrent\(session\);[\s\S]*state\.keplr\.notes = state\.keplr\.notes\.map/);
  assert.match(noteStatuses, /withPrivacySessionGuard\(\s*session,\s*\(\) => client\.checkNullifiers\(chunk\),\s*\)/);
  assert.match(noteStatuses, /catch \(error\) \{\s*if \(error\?\.privacySessionInvalidated\) throw error;\s*\/\/ Only this chunk falls back/);
  assert.match(noteStatuses, /withPrivacySessionGuard\(\s*session,\s*\(\) => client\.checkNullifier\(nullifier\),\s*\)/);
  assert.match(noteStatuses, /withPrivacySessionGuard\(\s*session,\s*\(\) => persistedNoteStore\.setNullifierStatuses\(/);
  assert.match(noteScan, /const session = options\.session \|\| beginPrivacySessionOperation\(\)/);
  assert.match(noteScan, /const fetchTypedScan = async \(extra = \{\}\) => \{[\s\S]*await withPrivacySessionGuard\([\s\S]*session,[\s\S]*\(\) => clairveilBrowserClient\(\)\.scanWalletNotes\([\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(noteScan, /if \(reset\) \{[\s\S]*await withPrivacySessionGuard\(session, \(\) => noteStore\.clear\(\)\);/);
  assert.match(noteScan, /const cachedBeforeScan = await withPrivacySessionGuard\([\s\S]*\(\) => noteStore\.load\(\),[\s\S]*let resumeOptions = \{\};[\s\S]*resumeOptions = resumeTypedNoteScanOptions\([\s\S]*let scan = await fetchTypedScan\(resumeOptions\);/);
  assert.match(noteScan, /assertPrivacySessionCurrent\(session\);[\s\S]*await withPrivacySessionGuard\(session, \(\) =>[\s\S]*noteStore\.mergeScanResult\(scan, \{[\s\S]*owner: state\.keplr\.shieldedAddress/);
  assert.doesNotMatch(noteScan, /noteStore,\s*includeFoundNotes: true/);
  assert.match(appSource, /import \{ nextPrivacyScanOptions \} from "clairveiljs\/cosmos-client";/);
  assert.match(appSource, /function resumeTypedNoteScanOptions\(cached, defaults\)/);
  assert.match(appSource, /nextPrivacyScanOptions\(cursor, defaults\)/);
  assert.match(noteScan, /result\?\.scanCursor\?\.source \?\? result\?\.scan_cursor\?\.source \?\? ""\) !== "privacy_scan"[\s\S]*await withPrivacySessionGuard\(session, \(\) => noteStore\.clear\(\)\);/);
  assert.match(noteScan, /let cached = await withPrivacySessionGuard\(session, \(\) =>[\s\S]*noteStore\.mergeScanResult\(scan, \{/);
  assert.match(noteScan, /if \(!applyPersistedWalletNoteState\(cached\)\) \{[\s\S]*if \(!reset\) \{[\s\S]*await withPrivacySessionGuard\(session, \(\) => noteStore\.clear\(\)\);[\s\S]*scan = await fetchTypedScan\(\);[\s\S]*cached = await withPrivacySessionGuard\(session, \(\) =>[\s\S]*noteStore\.replaceScanResult\(scan, \{/);
  assert.match(noteScan, /applyPersistedWalletNoteState\(cached\)[\s\S]*state\.keplr\.scanError = "";[\s\S]*scanStage = "scan-completion";[\s\S]*if \(!hasCompletedPrivacyNoteScan\(\)\)/);
  assert.match(noteScan, /let batchRecoveryDeferred = false;[\s\S]*await reconcileBatchTransferArtifact\([\s\S]*catch \(error\) \{[\s\S]*batchRecoveryDeferred = true;[\s\S]*warnReservationBookkeeping\(error\);/);
  assert.match(noteScan, /let relayRecoveryDeferred = false;[\s\S]*await reconcileExpiredRelayWithdrawPayloads\(null, \{ session \}\);[\s\S]*catch \(error\) \{[\s\S]*relayRecoveryDeferred = true;[\s\S]*warnReservationBookkeeping\(error\);/);
  assert.match(noteScan, /batchRecoveryDeferred \|\| relayRecoveryDeferred[\s\S]*Notes ready; saved operation recovery requires review[\s\S]*saved batch or relay operation still needs separate review/);
  assert.match(noteScan, /if \(error\?\.privacySessionInvalidated\) return;/);
  assert.match(noteScan, /finally \{[\s\S]*if \(isPrivacySessionCurrent\(session\)\) \{[\s\S]*setBusy\(els\.scanKeplrNotes, false\);[\s\S]*renderKeplr\(\);/);
  assert.match(noteScan, /await reconcileReservedNotesFromScan\(\{ session \}\)/);
  assert.match(noteScan, /await refreshNoteReservationState\(\{ session \}\)/);
  assert.match(
    appSource,
    /if \(!notesSyncReady && !state\.keplr\.notesScanned\)[\s\S]*if \(!notesSyncReady && state\.keplr\.scanError\)[\s\S]*warning\.className = "note-sync-warning"/,
  );
  assert.match(cssSource, /\.note-sync-warning\s*\{/);
  assert.match(relayDraftHandler, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(relayDraftHandler, /discardAndClearPreparedRelayWithdrawPayload\(\{ session \}\)/);
  assert.match(
    relayDraftHandler,
    /if \(!error\?\.privacySessionInvalidated\) \{[\s\S]*privacyRecoveryErrorMessage\([\s\S]*Prepared relay payload could not be discarded safely/,
  );
  assert.doesNotMatch(relayDraftHandler, /toast\(error\.message\)/);
  assert.match(relayDraftHandler, /finally\(\(\) => \{\s*if \(isPrivacySessionCurrent\(session\)\) renderKeplr\(\);/);
  assert.match(eventsHandler, /if \(!error\?\.privacySessionInvalidated\) toast\(error\.message\)/);
  assert.match(
    scanHandler,
    /if \(!error\?\.privacySessionInvalidated\) \{[\s\S]*toast\(privacySyncErrorMessage\(error\)\);/,
  );
  assert.match(walletBalanceRefresh, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(walletBalanceRefresh, /await withPrivacySessionGuard\(session, \(\) =>[\s\S]*clairveilBrowserClient\(\)\.evmJsonRpc\("eth_getBalance", \[[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*state\.keplr\.balance/);
  assert.match(walletBalanceRefresh, /await withPrivacySessionGuard\(session, \(\) =>[\s\S]*clairveilBrowserClient\(\)\.getBalances\(account\)[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*state\.keplr\.balance/);
  assert.match(notesRefresh, /session = beginPrivacySessionOperation\(\),[\s\S]*accountView = beginLocalAccountView\(\),[\s\S]*\} = \{\}/);
  assert.match(notesRefresh, /await withLocalAccountViewGuard\(accountView, \(\) =>[\s\S]*withPrivacySessionGuard\(session, \(\) =>[\s\S]*api\([\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*assertLocalAccountViewCurrent\(accountView\);[\s\S]*els\.spendableTotal/);
  assert.match(eventsRefresh, /allowFailure = false,[\s\S]*session = beginPrivacySessionOperation\(\),[\s\S]*healthView = null,[\s\S]*\} = \{\}/);
  assert.match(eventsRefresh, /await Promise\.allSettled\([\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*state\.privacyEvents\.events/);
  assert.match(blockEventsRefresh, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(blockEventsRefresh, /await clairveilBrowserClient\(\)\.fetchBlockEvents\(30\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*state\.blockEvents\.events/);
  assert.match(blockEventsRefresh, /catch \(error\) \{[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*state\.blockEvents\.events/);
  assert.match(privacySurfacesRefresh, /\{ balance = false, session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(privacySurfacesRefresh, /refreshEvents\(\{ session \}\)/);
  assert.match(privacySurfacesRefresh, /scanKeplrNotes\(\{ quiet: true, session \}\)/);
  assert.match(privacySurfacesRefresh, /refreshNotes\(\{ session \}\)/);
  assert.match(privacySurfacesRefresh, /refreshWalletBalance\(\{ session \}\)/);
  assert.match(privacySurfacesRefresh, /await Promise\.allSettled\(tasks\);\s*assertPrivacySessionCurrent\(session\);/);
  assert.match(depositNoteRecovery, /await refreshPrivacySurfaces\(\{ balance: true, session \}\);/);
  assert.match(depositNoteRecovery, /includedDepositRecoveryComplete\(\)/);
  assert.match(depositNoteRecovery, /await recoverIncludedDepositNote\(\{ session \}\)/);
  const includedDepositRecovery = sourceBetween(
    appSource,
    "async function recoverIncludedDepositNote",
    "async function refreshDepositNoteRecovery",
  );
  assert.match(includedDepositRecovery, /attempt < includedDepositRecoveryRetry\.attempts/);
  assert.match(includedDepositRecovery, /scanKeplrNotes\(\{[\s\S]*quiet: true,[\s\S]*throwOnError: true,[\s\S]*session,/);
  assert.match(includedDepositRecovery, /await scanKeplrNotes\(\{[\s\S]*reset: true,[\s\S]*quiet: true,[\s\S]*throwOnError: true,[\s\S]*session,/);
  assert.match(includedDepositRecovery, /never touches chain state or reservations/);
  assert.match(
    appSource,
    /function includedDepositRecoveryComplete\(\)[\s\S]*isChainSafetyReady\(\)[\s\S]*hasCompletedPrivacyNoteScan\(\)[\s\S]*hasRecoveredDepositNote\(\)/,
  );
  assert.match(depositNoteRecovery, /state\.keplr\.scanError = recoveryMessage/);
  assert.match(depositNoteRecovery, /Deposit included; note recovery required/);
  assert.match(depositNoteRecovery, /Retry Scan before relying on your shielded balance/);
  assert.match(
    appSource,
    /function hasRecoveredDepositNote\(txHash = state\.keplr\.depositHash\)/,
  );
  assert.match(depositFlow, /Deposit included; recovering note/);
  assert.match(depositFlow, /shielded balance remains unavailable until sync completes/);
  assert.match(depositFlow, /const recovered = await withPrivacySessionGuard\([\s\S]*refreshDepositNoteRecovery/);
  assert.match(depositFlow, /reportDepositNoteRecovery\(recovered\);/);
  assert.match(appSource, /function reportDepositNoteRecovery\(recovered\)/);
  assert.match(appSource, /Deposit included; note sync completed/);
  assert.match(appSource, /flowID: 0/);
  assert.match(transferFlowModal, /transferFlowState\.flowID \+= 1/);
  assert.match(transferFlowModal, /function transferFlowIsCurrent\(flowID\)/);
  assert.match(transferFlowFinish, /\{ successCopy = "", flowID = null \} = \{\}/);
  assert.match(transferFlowFinish, /successCopy \|\| copy\.successCopy/);
  assert.match(submittedOperationReconciliation, /await refreshPrivacySurfaces\(\{ balance, session \}\);/);
  assert.match(submittedOperationReconciliation, /latestReservationRecords\(preparedReservation\(data\), \{[\s\S]*session,/);
  assert.match(submittedOperationReconciliation, /isChainSafetyReady\(\)[\s\S]*hasCompletedPrivacyNoteScan\(\)[\s\S]*submittedOperationIsReconciled/);
  assert.match(appSource, /record\?\.status === reservationStatuses\.ConfirmedSpent/);
  assert.match(submittedOperationReport, /if \(flowID !== null && !transferFlowIsCurrent\(flowID\)\)/);
  assert.match(submittedOperationReport, /Earlier \$\{operation\} needs reconciliation/);
  assert.match(submittedOperationReport, /included; reconciliation required/);
  assert.match(reservationReconciliation, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(reservationReconciliation, /withPrivacySessionGuard\([\s\S]*manager\.reconcileSpentNotes/);
  assert.doesNotMatch(reservationReconciliation, /if \(!manager \|\| !notes\.length\) return;/);
  assert.match(reservationReconciliation, /if \(notes\.length\) \{[\s\S]*manager\.reconcileSpentNotes\(notes\)[\s\S]*\}[\s\S]*reconcileRecoveredActiveReservations\(manager, \{ session \}\)/);
  assert.match(reservationReconciliation, /reconcileRecoveredActiveReservations\(manager, \{ session \}\)/);
  assert.match(reservationReconciliation, /reconcileDefiniteFailedUnknownReservations\(manager, \{ session \}\)/);
  assert.match(recoveredReservationRecovery, /const activeBroadcastAttempt = records\.some\([\s\S]*record\.broadcast_in_flight === true/);
  assert.match(recoveredReservationRecovery, /const attemptOutcome = activeBroadcastAttempt[\s\S]*withPrivacySessionGuard\([\s\S]*recoveredReservationTxOutcome\(first, \{ session \}\)/);
  assert.match(recoveredReservationRecovery, /recovered_broadcast_attempt_requires_manual_review[\s\S]*attemptOutcome\?\.checked[\s\S]*tx_hash_checked/);
  assert.match(recoveredReservationRecovery, /const operationStatuses = new Set\(records\.map\(\(record\) => record\.status\)\);[\s\S]*operationStatuses\.size !== 1[\s\S]*manager\.markManualReview[\s\S]*recovered_mixed_operation_status_requires_manual_review[\s\S]*continue;[\s\S]*const nullifiers = records/);
  assert.match(appSource, /function reservationBroadcastRecoveryEvidence\([\s\S]*broadcastInFlight:[\s\S]*broadcastAttemptCount:[\s\S]*submittedTxHash:[\s\S]*txBytesHash:[\s\S]*signDocHash:/);
  assert.match(appSource, /function operationHasConsistentBroadcastRecoveryEvidence\([\s\S]*broadcastAttemptCount !== null[\s\S]*JSON\.stringify\(entry\)/);
  assert.match(appSource, /async function quarantineInconsistentOperationBroadcastEvidence\([\s\S]*operation_broadcast_recovery_evidence_inconsistent[\s\S]*recovered_inconsistent_operation_broadcast_evidence_requires_manual_review/);
  assert.match(recoveredReservationRecovery, /const operationBroadcastEvidenceInconsistent =[\s\S]*records\.some\(reservationHasBroadcastRecoveryEvidence\)[\s\S]*operationHasConsistentBroadcastRecoveryEvidence\(records\)[\s\S]*quarantineInconsistentOperationBroadcastEvidence\([\s\S]*continue;[\s\S]*const localWorkerState/);
  assert.match(recoveredReservationRecovery, /const activeBroadcastAttempt = records\.some\([\s\S]*const nullifiers = records[\s\S]*nullifiers\.length !== records\.length[\s\S]*workerExpired[\s\S]*activeBroadcastAttempt[\s\S]*recoveredReservationTxOutcome\(first, \{ session \}\)[\s\S]*recovered_broadcast_attempt_missing_input_nullifier_requires_manual_review[\s\S]*missing_input_nullifier_count[\s\S]*tx_hash_checked/);
  assert.match(recoveredReservationRecovery, /const postBroadcastRecoveryPastDeadline =[\s\S]*reservationStatuses\.Submitted, reservationStatuses\.Unknown[\s\S]*records\.every\(reservationNeedsManualReviewForMissingTx\)[\s\S]*recovered_post_broadcast_missing_input_nullifier_requires_manual_review[\s\S]*post_broadcast_status/);
  assert.match(appSource, /async function replanRecoveredLocalRelayPayload[\s\S]*operationStatuses\.size !== 1[\s\S]*recovered_mixed_relay_operation_status_requires_manual_review[\s\S]*markReservationBatchManualReview/);
  assert.match(appSource, /async function replanRecoveredLocalRelayPayload[\s\S]*const operationPayloadHashes = records\.map[\s\S]*payloadHashEvidenceInconsistent[\s\S]*recovered_relay_payload_hash_inconsistent_requires_manual_review[\s\S]*markReservationBatchManualReview/);
  assert.match(reservationStateRefresh, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(reservationStateRefresh, /withPrivacySessionGuard\([\s\S]*manager\.listActiveReservations/);
  assert.match(reservationStateRefresh, /withPrivacySessionGuard\([\s\S]*manager\.reservationStatusByNote/);
  assert.match(submittedReservation, /catch \(error\) \{\s*if \(error\?\.privacySessionInvalidated\) throw error;/);
  assert.match(submittedReservation, /catch \(unknownError\) \{\s*if \(unknownError\?\.privacySessionInvalidated\) throw unknownError;/);
  assert.match(manualReviewResolution, /\{\s*session = beginPrivacySessionOperation\(\),[\s\S]*\} = \{\}/);
  assert.match(manualReviewResolution, /allowExplicitUntrackedCancellation = false/);
  assert.match(manualReviewResolution, /const operatorId = state\.keplr\.account/);
  assert.match(manualReviewResolution, /withPrivacySessionGuard\([\s\S]*manager\.listActiveReservations/);
  assert.match(manualReviewResolution, /withPrivacySessionGuard\([\s\S]*manager\.reservationForNote\(note\)/);
  assert.match(manualReviewResolution, /const reviewRecords = await latestReservationRecords\([\s\S]*records\.map\(\(record\) => record\.reservation_id\)/);
  assert.match(manualReviewResolution, /withPrivacySessionGuard\([\s\S]*Promise\.all\([\s\S]*checkNullifierSpent\(nullifier, \{ session \}\)/);
  assert.match(manualReviewResolution, /const finalRecords = await latestReservationRecords\([\s\S]*manualReviewResolutionEvidence\(record\)/);
  assert.match(manualReviewResolution, /const transitionRecords = await latestReservationRecords\([\s\S]*finalEvidenceByID\.get\(record\.reservation_id\)[\s\S]*manualReviewResolutionEvidence\(record\)/);
  assert.match(manualReviewResolution, /transitionRecords\.map\(\(record\) =>[\s\S]*recoveredReservationTxOutcome\(record, \{ session \}\)/);
  assert.ok(
    manualReviewResolution.indexOf("const transitionRecords") <
      manualReviewResolution.indexOf("const [spent, transactionOutcomes]"),
  );
  assert.ok(
    manualReviewResolution.indexOf("const [spent, transactionOutcomes]") <
      manualReviewResolution.lastIndexOf("manager.resolveManualReview"),
  );
  assert.match(manualReviewResolution, /const resolutionRecords = await latestReservationRecords\([\s\S]*transitionRecords\.map\(\(record\) => record\.reservation_id\)[\s\S]*const transitionEvidenceByID = new Map\([\s\S]*manualReviewResolutionEvidence\(record\)/);
  assert.ok(
    manualReviewResolution.indexOf("const [spent, transactionOutcomes]") <
      manualReviewResolution.indexOf("const resolutionRecords"),
  );
  assert.match(manualReviewResolution, /state\.keplr\.account !== operatorId[\s\S]*privacySessionInvalidatedError\(\)/);
  assert.match(manualReviewResolution, /resolutionRecords\.map\(\(record\) => record\.reservation_id\)/);
  assert.match(manualReviewResolution, /withPrivacySessionGuard\([\s\S]*manager\.resolveManualReview/);
  assert.match(manualReviewResolution, /await refreshNoteReservationState\(\{ session \}\)[\s\S]*assertPrivacySessionCurrent\(session\)/);
  assert.match(appSource, /function manualReviewRequiresExplicitReservationCancellation\([\s\S]*!reservationHasDurableNoBroadcastEvidence\(record\)[\s\S]*!reservationHasQueryableTransactionIdentity\(record\)/);
  assert.match(manualReviewResolution, /const explicitUntrackedCancellation =[\s\S]*manualReviewRequiresExplicitReservationCancellation\(transitionRecords\)/);
  assert.match(manualReviewResolution, /explicit_untracked_wallet_request_cancellation: true[\s\S]*input_nullifiers_unspent_confirmed: true/);
  assert.match(appSource, /function manualReviewDisplayEvidence\([\s\S]*\{ payloadHash = "" \} = \{\}[\s\S]*submitted_tx_hash,[\s\S]*tx_bytes_hash,[\s\S]*sign_doc_hash,[\s\S]*metadata\?\.tx_hash_checked,[\s\S]*payload_hash,[\s\S]*payloadHash,[\s\S]*noteReservationByNullifier/);
  assert.match(appSource, /records\.length > 0[\s\S]*records\.every\(reservationHasDurableNoBroadcastEvidence\)/);
  assert.match(appSource, /function manualReviewRecordsForDisplay\(records = \[\]\)[\s\S]*reservationRecordsByID\(\)[\s\S]*authoritative\.length === ids\.length/);
  assert.match(appSource, /function appendManualReviewEvidence\(details, records, options = \{\}\)[\s\S]*Broadcast identity:[\s\S]*Payload hash:[\s\S]*Input nullifiers:/);
  assert.match(appSource, /function relayManualReviewRecoveryMessage\(snapshot\)[\s\S]*queryable broadcast identity[\s\S]*cannot cancel it/);
  assert.match(manualReviewUi, /appendManualReviewEvidence\(details, operationRecords\)/);
  assert.match(manualReviewUi, /openReservationReviewDialog\(operationID, operationRecords\)/);
  assert.match(relayExpiryRecovery, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(relayExpiryRecovery, /syncRelayWithdrawSnapshotReservation\(snapshot, \{ session \}\)/);
  assert.match(relayExpiryRecovery, /refreshCachedNoteStatuses\(\{ session \}\)/);
  assert.match(relayExpiryRecovery, /reconcileReservedNotesFromScan\(\{ session \}\)/);
  assert.match(relayExpiryRecovery, /persistRelayWithdrawPayloadState\(\{ session \}\)/);
  assert.match(relayManualReviewResolution, /const finalSnapshot = await relaySnapshotWithFullReservationRecords\([\s\S]*recoverySnapshot,[\s\S]*\{ session \}/);
  assert.match(relayManualReviewResolution, /const operatorId = state\.keplr\.account;\s*if \(!operatorId\) return snapshot;/);
  assert.match(relayManualReviewResolution, /finalRecords\.some\([\s\S]*record\.status !== reservationStatuses\.ManualReview/);
  assert.match(relayManualReviewResolution, /reviewEvidenceByID\.get\(record\.reservation_id\)[\s\S]*manualReviewResolutionEvidence\(record\)/);
  assert.match(relayManualReviewResolution, /const transitionSnapshot = await relaySnapshotWithFullReservationRecords\([\s\S]*finalSnapshot,[\s\S]*\{ session \}/);
  assert.match(relayManualReviewResolution, /!manager\?\.resolveManualReview \|\| state\.keplr\.account !== operatorId/);
  assert.match(relayManualReviewResolution, /const \[finalChainSnapshot, finalNullifierStatuses, transactionOutcomes\] =[\s\S]*latestRelayChainSnapshot\(\)/);
  assert.match(relayManualReviewResolution, /relaySnapshotIsExpired\(transitionSnapshot, finalChainSnapshot\.chainNowMs\)/);
  assert.match(relayManualReviewResolution, /relaySnapshotNullifierStatuses\(transitionSnapshot, \{ session \}\)/);
  assert.match(relayManualReviewResolution, /transitionRecords\.map\(\(record\) =>[\s\S]*recoveredReservationTxOutcome\(record, \{ session \}\)/);
  assert.ok(
    relayManualReviewResolution.indexOf("const transitionSnapshot") <
      relayManualReviewResolution.indexOf("const [finalChainSnapshot"),
  );
  assert.ok(
    relayManualReviewResolution.indexOf("const [finalChainSnapshot") <
      relayManualReviewResolution.indexOf("manager.resolveManualReview"),
  );
  assert.match(relayManualReviewResolution, /const resolutionSnapshot = await relaySnapshotWithFullReservationRecords\([\s\S]*transitionSnapshot,[\s\S]*\{ session \}/);
  assert.match(relayManualReviewResolution, /const resolutionRecords = resolutionSnapshot\?\.reservation\?\.reservations \|\| \[\];[\s\S]*transitionEvidenceByID\.get\(record\.reservation_id\)[\s\S]*manualReviewResolutionEvidence\(record\)/);
  assert.ok(
    relayManualReviewResolution.indexOf("const [finalChainSnapshot") <
      relayManualReviewResolution.indexOf("const resolutionSnapshot"),
  );
  assert.match(relayManualReviewResolution, /reservationIDs\(resolutionSnapshot\.reservation\)/);
  assert.match(relayManualReviewResolution, /operatorId,\s*approvalReference: `relay-expiry:/);
  assert.match(appSource, /const relayMetadataWrites = new Map\(\);/);
  assert.match(relayMetadataPersistence, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(relayMetadataPersistence, /const key = relayMetadataStoreKey;/);
  assert.match(relayMetadataPersistence, /const previousWrite = relayMetadataWrites\.get\(key\) \|\| Promise\.resolve\(\);/);
  assert.match(relayMetadataPersistence, /const write = previousWrite/);
  assert.match(relayMetadataPersistence, /await withPrivacySessionGuard\(session, \(\) => storage\.clear\(\)\);/);
  assert.match(relayMetadataPersistence, /await withPrivacySessionGuard\([\s\S]*session,[\s\S]*\(\) => storage\.save\([\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(relayMetadataPersistence, /const queuedWrite = write\.catch\(\(error\) => \{/);
  assert.match(relayMetadataPersistence, /relayMetadataWrites\.set\(key, queuedWrite\);/);
  assert.match(relayMetadataPersistence, /if \(error\?\.privacySessionInvalidated\) return;/);
  assert.match(relayMetadataPersistence, /return write;/);
  assert.doesNotMatch(relayMetadataPersistence, /if \(!isPrivacySessionCurrent\(session\)\) return;/);
  assert.match(relayRecovery, /const pendingWrite = relayMetadataWrites\.get\(relayMetadataStoreKey\);/);
  assert.match(relayRecovery, /if \(pendingWrite\) \{\s*await withPrivacySessionGuard\(session, \(\) => pendingWrite\);/);
  assert.match(relayPayloadClear, /session = beginPrivacySessionOperation\(\)/);
  assert.match(relayPayloadClear, /await persistRelayWithdrawPayloadState\(\{ session \}\);\s*assertPrivacySessionCurrent\(session\);/);
  assert.match(recoveredTxOutcome, /\{ session = null \} = \{\}/);
  assert.match(recoveredTxOutcome, /withPrivacySessionGuard\([\s\S]*evmJsonRpc\([\s\S]*withPrivacySessionGuard\([\s\S]*waitForTx\(/);
  assert.match(recoveredTxOutcome, /if \(error\?\.privacySessionInvalidated\) throw error;/);
  assert.match(recoveredReservationRecovery, /nullifiers\.map\(\(nullifier\) => checkNullifierSpent\(nullifier, \{ session \}\)\)/);
  assert.match(definiteFailureRecovery, /nullifiers\.map\(\(nullifier\) => checkNullifierSpent\(nullifier, \{ session \}\)\)/);
  assert.match(failedReservationRecovery, /nullifiers\.map\(\(nullifier\) => checkNullifierSpent\(nullifier, \{ session \}\)\)/);
  assert.match(relayNullifierStatuses, /checkNullifierSpent\(nullifier, \{ session = null \} = \{\}\)/);
  assert.match(relayNullifierStatuses, /withPrivacySessionGuard\([\s\S]*checkNullifier\(nullifier\)/);
  assert.match(relayNullifierStatuses, /relaySnapshotNullifierStatuses\(snapshot, \{ session = null \} = \{\}\)[\s\S]*checkNullifierSpent\(nullifier, \{ session \}\)[\s\S]*assertPrivacySessionCurrent\(session\)/);
  assert.match(chainSafetyPreflight, /\{ force = false, session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(appSource, /let chainSafetyRefreshGeneration = 0/);
  assert.match(appSource, /function clearChainSafety\(\)[\s\S]*chainSafetyRefreshGeneration \+= 1;/);
  assert.match(chainSafetyPreflight, /generation: \+\+chainSafetyRefreshGeneration,/);
  assert.match(chainSafetyPreflight, /withPrivacySessionGuard\([\s\S]*client\.health\(\{ allowUninitializedTree: true \}\)/);
  assert.match(chainSafetyPreflight, /withPrivacySessionGuard\([\s\S]*client\.evmJsonRpc\("eth_chainId"\)/);
  assert.match(chainSafetyPreflight, /assertChainSafetyRefreshCurrent\(refresh\);[\s\S]*state\.chainSafety = \{/);
  assert.match(chainSafetyPreflight, /if \(error\?\.privacySessionInvalidated\) throw error;/);
  assert.match(evmReceiptWatcher, /session = beginPrivacySessionOperation\(\)/);
  assert.match(evmReceiptWatcher, /const invoke = async \(callback, value\) =>/);
  assert.match(evmReceiptWatcher, /await callback\(value\)/);
  assert.match(evmReceiptWatcher, /void broadcast\.waitPromise[\s\S]*return invoke\(onFailed, error\)/);
  assert.match(evmReceiptWatcher, /error\?\.privacySessionInvalidated \|\| !isPrivacySessionCurrent\(session\)/);
  assert.match(reservationBookkeeping, /if \(error\?\.privacySessionInvalidated\) throw error;/);
  for (const flow of [depositFlow, transferFlow, withdrawFlow, relayPreparationFlow]) {
    assert.match(flow, /const session = beginPrivacySessionOperation\(\)/);
    assert.match(flow, /if \(!isPrivacySessionCurrent\(session\)\) return;/);
    assert.match(flow, /error\?\.privacySessionInvalidated \|\| !isPrivacySessionCurrent\(session\)/);
    assert.match(flow, /finally \{[\s\S]*endPrivacyValueAction\(actionLock\)/);
  }
  assert.match(depositFlow, /withPrivacySessionGuard\([\s\S]*refreshDepositNoteRecovery/);
  for (const flow of [transferFlow, withdrawFlow]) {
    assert.match(flow, /watchEvmBroadcast\(broadcast, \{\s*session,/);
    assert.match(flow, /flowID = transferFlowState\.flowID;/);
    assert.match(flow, /withPrivacySessionGuard\([\s\S]*refreshSubmittedOperationReconciliation/);
    assert.match(flow, /reportSubmittedOperationReconciliation\(/);
    assert.match(flow, /if \(!transferFlowIsCurrent\(flowID\)\) \{[\s\S]*Earlier/);
  }
  assert.match(transferFlow, /reconcileFailedEvmReservation\([\s\S]*\{ session \}/);
  assert.match(withdrawFlow, /reconcileFailedEvmReservation\([\s\S]*\{ session \}/);
  assert.match(transferFlow, /preparePrivacyTransferSignDoc\([\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(transferFlow, /await refreshPrivacySurfaces\(\{ session \}\);/);
  assert.doesNotMatch(transferFlow, /refreshPrivacySurfaces\(\);/);
  assert.match(withdrawFlow, /preparePrivacyWithdrawSignDoc\(amount, recipient\);[\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(exactWithdrawNote, /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(exactWithdrawNote, /await refreshPrivacySurfaces\(\{ session \}\);/);
  assert.doesNotMatch(exactWithdrawNote, /refreshPrivacySurfaces\(\);/);
  assert.match(withdrawFlow, /await createExactWithdrawNote\(amount,[\s\S]*\}, \{ session \}\);[\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(relayPreparationFlow, /await createExactWithdrawNote\(amount,[\s\S]*\}, \{ session \}\);[\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(relayPreparationFlow, /data = await preparePrivacyRelayWithdrawPayload\(amount, recipient\);\s*assertPrivacySessionCurrent\(session\);/);
  assert.match(privacyInvalidation, /resetTransferFlowForPrivacySession\(\)/);
  assert.match(transferFlowReset, /transferFlowState\.copy = null/);
  assert.match(transferFlowReset, /resetTransferPlannerFacts\(\)/);
  assert.match(transferFlowReset, /transferFlowModal\.hidden = true/);
  assert.match(transferFlowReset, /transferFailureReason\.textContent = "-"/);
  assert.match(transferFlowReset, /if \(resolve\) resolve\(false\)/);
});

test("DApp session-binds SDK preparation and public wallet sends", () => {
  const depositPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyDepositSignDoc",
    "async function preparePrivacyTransferSignDoc",
  );
  const transferPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyTransferSignDoc",
    "async function preparePrivacyWithdrawSignDoc",
  );
  const withdrawPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyWithdrawSignDoc",
    "async function preparePrivacyRelayWithdrawPayload",
  );
  const relayPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyRelayWithdrawPayload",
    "async function relayPreparedWithdrawPayload",
  );
  const publicSend = sourceBetween(
    appSource,
    "async function sendFromKeplr",
    "async function depositFromKeplr",
  );
  const depositBroadcast = sourceBetween(
    appSource,
    "async function broadcastPrivacyDeposit",
    "function broadcastTxEvents",
  );
  const transferBroadcast = sourceBetween(
    appSource,
    "async function broadcastVeiledTransfer",
    "function isExactMatchWithdrawError",
  );
  const prepareCleanup = sourceBetween(
    appSource,
    "async function preparePrivacyWithSessionCleanup",
    "function profileSessionFingerprint",
  );

  for (const prepare of [
    depositPrepare,
    transferPrepare,
    withdrawPrepare,
    relayPrepare,
  ]) {
    assert.match(prepare, /const session = beginPrivacySessionOperation\(\)/);
    assert.match(prepare, /preparePrivacyWithSessionCleanup\(\s*session,/);
  }
  assert.match(prepareCleanup, /const controller = new AbortController\(\);/);
  assert.match(prepareCleanup, /activePrivacyPreparationControllers\.add\(controller\);/);
  assert.match(prepareCleanup, /data = await task\(controller\.signal\);/);
  assert.match(prepareCleanup, /activePrivacyPreparationControllers\.delete\(controller\);/);
  assert.match(prepareCleanup, /catch \(error\) \{\s*assertPrivacySessionCurrent\(session\);/);
  assert.match(prepareCleanup, /return finishPrivacyPreparation\(data, session\)/);
  assert.match(depositPrepare, /clairveilBrowserClient\(\)\.prepareDeposit\(request\)/);
  assert.match(depositBroadcast, /const session = preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\);/);
  assert.match(depositBroadcast, /const data = await preparePrivacyDepositSignDoc\(amount\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*shieldedAddress/);
  assert.match(depositBroadcast, /const broadcast = await broadcastPreparedPrivacy\(data, label, options\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*depositHash/);
  assert.match(transferBroadcast, /const session = preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\);/);
  assert.match(transferBroadcast, /const data = await preparePrivacyTransferSignDoc\([\s\S]*\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*Waiting for/);
  assert.match(transferBroadcast, /const broadcast = await broadcastPreparedPrivacy\(data, label\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*transferHash/);
  assert.match(transferPrepare, /clairveilBrowserClient\(\)\.prepareTransfer/);
  assert.match(transferPrepare, /allowPlanStep: Boolean\(options\.allowPlanStep\),\s*signal,/);
  assert.match(withdrawPrepare, /clairveilBrowserClient\(\)\.prepareWithdraw/);
  assert.match(withdrawPrepare, /reservationManager,\s*signal,/);
  assert.match(relayPrepare, /latestRelayChainSnapshot/);
  assert.match(relayPrepare, /clairveilBrowserClient\(\)\.prepareRelayWithdraw/);
  assert.match(relayPrepare, /reservationManager,\s*signal,/);
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function invalidateFailedPrivacySetup",
  );
  assert.match(privacyInvalidation, /for \(const controller of activePrivacyPreparationControllers\) \{\s*controller\.abort\(\);\s*\}/);
  assert.match(publicSend, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(publicSend, /sendEvmTransaction\(transaction, \{[\s\S]*session,/);
  assert.match(publicSend, /watchEvmBroadcast\(broadcast, \{\s*session,/);
  assert.match(publicSend, /withPrivacySessionGuard\([\s\S]*buildBankSendSignDoc/);
  assert.match(publicSend, /signDirectAndBroadcast\(signDoc, \{ session \}\)/);
  assert.match(publicSend, /error\?\.privacySessionInvalidated \|\| !isPrivacySessionCurrent\(session\)/);
  assert.match(publicSend, /finally \{[\s\S]*if \(isPrivacySessionCurrent\(session\)\)/);
});

test("DApp discards stale local helper and balance refresh results", () => {
  const faucet = sourceBetween(
    appSource,
    "async function fundKeplr",
    "async function setupKeplrPrivacy",
  );
  const walletBalance = sourceBetween(
    appSource,
    "async function refreshWalletBalance",
    "async function refreshNotes",
  );
  const walletBalanceHandler = sourceBetween(
    appSource,
    "els.refreshWalletBalance.addEventListener",
    "els.scanKeplrNotes.addEventListener",
  );
  const relayerView = sourceBetween(
    appSource,
    "function relayerViewIdentity",
    "function profilePersistenceScope",
  );
  const relayerRefresh = sourceBetween(
    appSource,
    "async function refreshRelayerAccount",
    "async function refreshWalletBalance",
  );
  const profileSelect = sourceBetween(
    appSource,
    "async function selectDappChainProfile",
    "function recipientTestAccounts",
  );
  const healthRender = sourceBetween(
    appSource,
    "async function renderHealth",
    "async function refreshHealth",
  );
  const healthRefresh = sourceBetween(
    appSource,
    "async function refreshHealth",
    "async function refreshSelectedAccount",
  );
  const localAccountRefresh = sourceBetween(
    appSource,
    "async function refreshSelectedAccount",
    "async function refreshRelayerAccount",
  );
  const localNotesRefresh = sourceBetween(
    appSource,
    "async function refreshNotes",
    "async function refreshEvents",
  );
  const eventsRefresh = sourceBetween(
    appSource,
    "async function refreshEvents",
    "async function refreshBlockEvents",
  );

  assert.match(faucet, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(faucet, /withPrivacySessionGuard\(session, \(\) =>[\s\S]*api\("\/api\/faucet"/);
  assert.match(faucet, /await refreshWalletBalance\(\{ session \}\)/);
  assert.match(faucet, /error\?\.privacySessionInvalidated \|\| !isPrivacySessionCurrent\(session\)/);
  assert.match(faucet, /finally \{[\s\S]*if \(isPrivacySessionCurrent\(session\)\)/);
  assert.match(walletBalance, /withPrivacySessionGuard\(session, \(\) =>[\s\S]*getBalances\(account\)/);
  assert.match(walletBalanceHandler, /if \(!error\?\.privacySessionInvalidated\) toast\(error\.message\)/);
  assert.match(appSource, /let relayerViewGeneration = 0/);
  assert.match(relayerView, /function beginRelayerView\(\)/);
  assert.match(relayerView, /function withRelayerViewGuard\(view, task\)/);
  assert.match(relayerView, /function invalidateRelayerView\(\)/);
  assert.match(relayerRefresh, /const relayerView = beginRelayerView\(\)/);
  assert.match(relayerRefresh, /withRelayerViewGuard\([\s\S]*getBalances\(relayer\.transparentAddress\)/);
  assert.match(relayerRefresh, /catch \(error\) \{\s*assertRelayerViewCurrent\(relayerView\)/);
  assert.match(profileSelect, /if \(profileChanged\) \{[\s\S]*invalidateRelayerView\(\)/);
  assert.match(healthRender, /invalidateRelayerView\(\)/);
  assert.match(healthRefresh, /refreshSelectedAccount\(\{ healthView \}\)/);
  assert.match(healthRefresh, /refreshEvents\(\{ allowFailure: true, healthView \}\)/);
  assert.match(healthRefresh, /refreshRelayerAccount\(\{ healthView \}\)/);
  assert.match(healthRefresh, /refreshAuditorTransfers\(\{ healthView \}\)/);
  assert.match(healthRefresh, /refreshAuditorTestScalar\(\{ healthView \}\)/);
  assert.match(localAccountRefresh, /assertOptionalHealthViewCurrent\(healthView\)/);
  assert.match(localNotesRefresh, /assertOptionalHealthViewCurrent\(healthView\)/);
  assert.match(eventsRefresh, /assertOptionalHealthViewCurrent\(healthView\)/);
});

test("DApp discards stale selected-local-account views", () => {
  const localAccountView = sourceBetween(
    appSource,
    "function localAccountViewIdentity",
    "function profilePersistenceScope",
  );
  const profileSelect = sourceBetween(
    appSource,
    "async function selectDappChainProfile",
    "function recipientTestAccounts",
  );
  const selectedAccountRefresh = sourceBetween(
    appSource,
    "async function refreshSelectedAccount",
    "async function refreshRelayerAccount",
  );
  const healthRefresh = sourceBetween(
    appSource,
    "async function refreshHealth",
    "async function refreshSelectedAccount",
  );
  const notesRefresh = sourceBetween(
    appSource,
    "async function refreshNotes",
    "async function refreshEvents",
  );
  const notesHandler = sourceBetween(
    appSource,
    "els.refreshNotes.addEventListener",
    "els.refreshEvents.addEventListener",
  );
  const accountHandler = sourceBetween(
    appSource,
    "els.accountSelect.addEventListener",
    "const injectedMetaMask",
  );

  assert.match(appSource, /let localAccountViewGeneration = 0/);
  assert.match(localAccountView, /function beginLocalAccountView\(\)/);
  assert.match(localAccountView, /function withLocalAccountViewGuard\(view, task\)/);
  assert.match(localAccountView, /function invalidateLocalAccountView\(\)/);
  assert.match(profileSelect, /if \(profileChanged\) \{[\s\S]*invalidateLocalAccountView\(\)/);
  assert.match(selectedAccountRefresh, /const accountView = beginLocalAccountView\(\)/);
  assert.match(selectedAccountRefresh, /withLocalAccountViewGuard\([\s\S]*Promise\.all\(/);
  assert.match(selectedAccountRefresh, /await refreshNotes\(\{ accountView, healthView \}\)/);
  assert.match(healthRefresh, /if \(!error\?\.privacySessionInvalidated\)/);
  assert.doesNotMatch(healthRefresh, /if \(error\?\.privacySessionInvalidated\) return/);
  assert.match(notesRefresh, /assertLocalAccountViewCurrent\(accountView\)/);
  assert.match(notesRefresh, /withLocalAccountViewGuard\(accountView, \(\) =>[\s\S]*withPrivacySessionGuard\(session/);
  assert.match(notesHandler, /if \(!error\?\.privacySessionInvalidated\) toast\(error\.message\)/);
  assert.match(accountHandler, /invalidateLocalAccountView\(\);[\s\S]*state\.selectedAccount = event\.target\.value/);
  assert.match(accountHandler, /if \(!error\?\.privacySessionInvalidated\) toast\(error\.message\)/);
});

test("DApp delegates reserved Cosmos broadcast bookkeeping to ClairveilJS", () => {
  const signing = sourceBetween(
    appSource,
    "function keplrDirectSignWallet",
    "async function submitEvmTransaction",
  );
  const evmSubmission = sourceBetween(
    appSource,
    "async function submitEvmTransaction",
    "async function waitForEvmTransaction",
  );
  const preparedBroadcast = sourceBetween(
    appSource,
    "async function broadcastPreparedPrivacy",
    "async function broadcastVeiledTransfer",
  );
  const broadcastNullifierPreflight = sourceBetween(
    appSource,
    "async function verifyPreparedNullifiersUnspentBeforeBroadcast",
    "function isDefiniteEvmReceiptFailure",
  );
  assert.match(signing, /clairveilBrowserClient\(\)\.signDirect\(submission\)/);
  assert.match(signing, /clairveilBrowserClient\(\)\.broadcastTxRawBytes\(/);
  assert.match(signing, /relayPayload = null/);
  assert.match(signing, /relayPayload,[\s\S]*getChainNowUnix: \(\) => latestChainNowUnix\(\{ session \}\)/);
  assert.match(signing, /reservationManager: broadcastReservationManager,[\s\S]*reservation,[\s\S]*\.\.\.relayValidation/);
  assert.match(signing, /function sessionBoundReservationBroadcastManager/);
  assert.match(signing, /const broadcastReservationManager = hasReservation[\s\S]*sessionBoundReservationBroadcastManager/);
  assert.match(signing, /property !== "markBroadcastAttempting"/);
  assert.match(signing, /const updated = await value\.apply\(target, args\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*onBroadcastStart\?\.\(\);/);
  assert.match(signing, /reservationManager: broadcastReservationManager/);
  assert.match(signing, /function keplrDirectSignWallet\([\s\S]*\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(signing, /const account = state\.keplr\.account/);
  assert.match(signing, /withPrivacySessionGuard\([\s\S]*window\.keplr\.signDirect/);
  assert.match(signing, /window\.keplr\.signDirect\([\s\S]*preferNoSetFee: true,[\s\S]*preferNoSetMemo: true/);
  assert.match(signing, /withPrivacySessionGuard\([\s\S]*clairveilBrowserClient\(\)\.signDirect\(submission\)/);
  assert.match(signing, /if \(!hasReservation\) \{[\s\S]*onBroadcastStart\?\.\(\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*broadcastTxRawBytes/);
  assert.match(evmSubmission, /session = beginPrivacySessionOperation\(\)/);
  assert.match(evmSubmission, /await ensureMetaMaskChain\(\{ session \}\)/);
  assert.match(evmSubmission, /withPrivacySessionGuard\([\s\S]*withEstimatedEvmGas/);
  assert.match(evmSubmission, /await beforeBroadcast\?\.\(\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*walletRequest = provider\.request\([\s\S]*method: "eth_sendTransaction"[\s\S]*onBroadcastStart\?\.\(\{ externalBoundaryStarted: true \}\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*withPrivacySessionGuard\([\s\S]*\(\) => walletRequest/);
  assert.match(
    broadcastNullifierPreflight,
    /\{ session = preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\) \} = \{\}/,
  );
  assert.match(broadcastNullifierPreflight, /withPrivacySessionGuard\([\s\S]*clairveilBrowserClient\(\)\.checkNullifiers/);
  assert.match(broadcastNullifierPreflight, /withPrivacySessionGuard\([\s\S]*manager\.reconcileSpentNotes/);
  assert.match(broadcastNullifierPreflight, /refreshNoteReservationState\(\{ session \}\)/);
  assert.match(preparedBroadcast, /const beforeBroadcast = async \(\) => \{[\s\S]*markPreparedReservationBroadcastAttempting\(data, label, \{ session \}\);[\s\S]*durableBroadcastAttemptRecorded = true/);
  assert.match(preparedBroadcast, /verifyPreparedNullifiersUnspentBeforeBroadcast\(data, \{ session \}\);[\s\S]*verifyPreparedWithdrawPayloadBeforeEvmBroadcast\(data, \{ session \}\);[\s\S]*markPreparedReservationBroadcastAttempting/);
  assert.match(preparedBroadcast, /const onBroadcastStart = \(\{ externalBoundaryStarted = false \} = \{\}\) => \{[\s\S]*if \(externalBoundaryStarted\) \{[\s\S]*externalBroadcastBoundaryCrossed = true[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*externalBroadcastBoundaryCrossed = true/);
  assert.match(preparedBroadcast, /sendEvmTransaction\(data\.transaction, \{[\s\S]*beforeBroadcast,[\s\S]*onBroadcastStart,[\s\S]*session,/);
  assert.match(preparedBroadcast, /await verifyPreparedNullifiersUnspentBeforeBroadcast\(data, \{ session \}\);[\s\S]*await signDirectAndBroadcast\(data\.signDoc, \{[\s\S]*reservation: preparedReservation\(data\),[\s\S]*data\?\.payload \? \{ relayPayload: data\.payload \} : \{\}\),[\s\S]*session,[\s\S]*onBroadcastStart,/);
  assert.match(preparedBroadcast, /markPreparedReservationBroadcastAttempting\(data, label, \{ session \}\)/);
  assert.match(preparedBroadcast, /recordSubmittedReservation\([\s\S]*label,[\s\S]*\{ session \}/);
  assert.match(preparedBroadcast, /sdkReservationLifecycleManaged/);
  assert.match(preparedBroadcast, /Calling the local marker here would create a second attempt/);
  assert.match(preparedBroadcast, /if \(!externalBroadcastBoundaryCrossed\) \{\s*await replanInvalidatedPreparedReservation\(data, session, error\);/);

  const withdrawPreparation = sourceBetween(
    appSource,
    "async function preparePrivacyWithdrawSignDoc",
    "async function preparePrivacyRelayWithdrawPayload",
  );
  assert.match(withdrawPreparation, /await assertTypedPrivacyScanBeforePreparation\(session\);[\s\S]*const chainNowUnix = await latestChainNowUnix\(\{ session \}\);[\s\S]*chainNowUnix,/);
  const evmWithdrawValidation = sourceBetween(
    appSource,
    "async function verifyPreparedWithdrawPayloadBeforeEvmBroadcast",
    "function relaySnapshotNullifiers",
  );
  assert.match(evmWithdrawValidation, /validateRelayWithdrawPayload\(payload, \{[\s\S]*chainNowUnix/);
  assert.match(evmWithdrawValidation, /createEvmPrivacyPrecompileAdapter\([\s\S]*buildWithdrawMsgFromPayload\(payload, "", chainNowUnix\)/);
});

test("DApp confirms a delayed Cosmos transaction index before requiring reconciliation", () => {
  const delayedConfirmation = sourceBetween(
    appSource,
    "const delayedCosmosBroadcastConfirmation",
    "function preparedReservation",
  );
  const preparedBroadcast = sourceBetween(
    appSource,
    "async function broadcastPreparedPrivacy",
    "async function broadcastVeiledTransfer",
  );

  assert.match(delayedConfirmation, /attempts:\s*24/);
  assert.match(delayedConfirmation, /intervalMs:\s*1250/);
  assert.match(delayedConfirmation, /async function confirmDelayedCosmosBroadcast/);
  assert.match(delayedConfirmation, /clairveilBrowserClient\(\)\.waitForTx\([\s\S]*delayedCosmosBroadcastConfirmation/);
  assert.match(delayedConfirmation, /state\.activeWallet === "metamask"/);
  assert.match(delayedConfirmation, /ok: code === 0/);
  assert.match(
    preparedBroadcast,
    /result = await signDirectAndBroadcast\([\s\S]*result = await confirmDelayedCosmosBroadcast\(result, \{ session \}\);[\s\S]*assertSuccessfulBroadcast\(result, label\)/,
  );
});

test("DApp checks MetaMask network before privacy preparation and resets on network changes", () => {
  const chainSafety = sourceBetween(
    appSource,
    "async function assertChainSafetyBeforePrivacyFlow",
    "function injectedEthereumProviders",
  );
  const chainChange = sourceBetween(
    appSource,
    'injectedMetaMask.on?.("chainChanged"',
    "window.addEventListener(\"keplr_keystorechange\"",
  );
  assert.match(chainSafety, /activeWalletKind\(\) === "metamask"/);
  assert.match(chainSafety, /await ensureMetaMaskChain\(\{ session \}\);/);
  assert.match(chainSafety, /assertPrivacySessionCurrent\(session\);/);
  assert.match(chainSafety, /refreshChainSafety\(\{ force: true, session \}\)/);
  assert.match(chainSafety, /Connect MetaMask before preparing a privacy transaction/);
  assert.match(
    appSource,
    /function evmPrivacyRequest\(extra = \{\}\) \{[\s\S]*evmWallet: \{[\s\S]*getChainId: \(\) => requestMetaMask\(\{ method: "eth_chainId" \}\)/,
  );
  assert.match(chainChange, /resetWalletSession\(\);/);
  assert.doesNotMatch(chainChange, /discardAndClearPreparedRelayWithdrawPayload/);
  assert.match(chainChange, /Reconnect wallet before preparing another privacy transaction/);
});

test("DApp preserves old relay reservations when the privacy identity changes", () => {
  const profileSelect = sourceBetween(
    appSource,
    "async function selectDappChainProfile",
    "function recipientTestAccounts",
  );
  const profileChange = sourceBetween(
    appSource,
    "async function clearPrivacySessionForProfileChange",
    "async function renderHealth",
  );
  const metaMaskConnect = sourceBetween(
    appSource,
    "async function connectWallet",
    "async function signMetaMaskSession",
  );
  const keplrConnect = sourceBetween(
    appSource,
    "async function connectKeplr",
    "async function signKeplrSession",
  );
  const disconnect = sourceBetween(
    appSource,
    "async function disconnectWallet",
    "async function fundKeplr",
  );
  const accountChange = sourceBetween(
    appSource,
    'injectedMetaMask.on?.("accountsChanged"',
    'injectedMetaMask.on?.("chainChanged"',
  );
  const chainChange = sourceBetween(
    appSource,
    'injectedMetaMask.on?.("chainChanged"',
    "window.addEventListener(\"keplr_keystorechange\"",
  );
  const keplrChange = sourceBetween(
    appSource,
    "window.addEventListener(\"keplr_keystorechange\"",
    "renderWallet();",
  );

  assert.match(appSource, /Identity changes clear only this tab's memory/);
  for (const flow of [
    profileSelect,
    profileChange,
    metaMaskConnect,
    keplrConnect,
    disconnect,
    accountChange,
    chainChange,
    keplrChange,
  ]) {
    assert.doesNotMatch(flow, /discardAndClearPreparedRelayWithdrawPayload/);
  }
  assert.match(profileSelect, /resetWalletSession\(\);/);
  assert.match(profileChange, /resetWalletSession\(\);/);
  assert.match(metaMaskConnect, /resetWalletSession\(\);[\s\S]*beginWalletConnection\("metamask"\);/);
  assert.match(keplrConnect, /resetWalletSession\(\);[\s\S]*beginWalletConnection\("keplr"\);/);
  assert.match(disconnect, /resetWalletSession\(\);/);
  assert.match(accountChange, /resetWalletSession\(\);[\s\S]*renderWallet\(\);[\s\S]*renderKeplr\(\);/);
  assert.match(chainChange, /resetWalletSession\(\);[\s\S]*state\.wallet\.chainId = chainId;/);
  assert.match(keplrChange, /walletConnectionSession\?\.wallet !== "keplr"[\s\S]*resetWalletSession\(\);/);
});

test("DApp binds wallet setup and session signing to the active privacy session", () => {
  const walletHelpers = sourceBetween(
    appSource,
    "function beginWalletConnection",
    "function isPrivacySessionCurrent",
  );
  const privacyInvalidation = sourceBetween(
    appSource,
    "function invalidatePrivacySessionOperations",
    "function resetMetaMaskSession",
  );
  const metaMaskConnect = sourceBetween(
    appSource,
    "async function connectWallet",
    "async function signMetaMaskSession",
  );
  const metaMaskSign = sourceBetween(
    appSource,
    "async function signMetaMaskSession",
    "async function signSession",
  );
  const metaMaskChain = sourceBetween(
    appSource,
    "async function ensureMetaMaskChain",
    "function coinDecimals",
  );
  const keplrConnect = sourceBetween(
    appSource,
    "async function connectKeplr",
    "async function signKeplrSession",
  );
  const keplrSign = sourceBetween(
    appSource,
    "async function signKeplrSession",
    "async function disconnectWallet",
  );
  const walletBindings = sourceBetween(
    appSource,
    "els.connectWallet.addEventListener",
    "els.copyWalletAccount.addEventListener",
  );
  const accountChange = sourceBetween(
    appSource,
    'injectedMetaMask.on?.("accountsChanged"',
    'injectedMetaMask.on?.("chainChanged"',
  );
  const chainChange = sourceBetween(
    appSource,
    'injectedMetaMask.on?.("chainChanged"',
    "window.addEventListener(\"keplr_keystorechange\"",
  );
  const keplrChange = sourceBetween(
    appSource,
    "window.addEventListener(\"keplr_keystorechange\"",
    "renderWallet();",
  );

  assert.match(walletHelpers, /walletConnectionSession = connection/);
  assert.match(walletHelpers, /isPrivacySessionCurrent\(connection\.session\)/);
  assert.match(walletHelpers, /profileSessionFingerprint\(profile\) === connection\.profileFingerprint/);
  assert.match(privacyInvalidation, /walletConnectionSession = null/);
  assert.match(metaMaskConnect, /beginWalletConnection\("metamask"\)/);
  assert.match(metaMaskConnect, /ensureMetaMaskChain\(\{ session \}\)/);
  assert.match(metaMaskConnect, /withPrivacySessionGuard\([\s\S]*eth_requestAccounts/);
  assert.match(metaMaskConnect, /assertWalletConnectionCurrent\(connection\);[\s\S]*state\.activeWallet = "metamask"/);
  assert.match(metaMaskConnect, /refreshWalletBalance\(\{ session \}\)/);
  assert.match(metaMaskConnect, /finally \{[\s\S]*endWalletConnection\(connection\)/);
  assert.match(keplrConnect, /beginWalletConnection\("keplr"\)/);
  assert.match(keplrConnect, /withPrivacySessionGuard\([\s\S]*experimentalSuggestChain/);
  assert.match(keplrConnect, /withPrivacySessionGuard\([\s\S]*resolveKeplrSigner/);
  assert.match(keplrConnect, /assertWalletConnectionCurrent\(connection\);[\s\S]*state\.activeWallet = "keplr"/);
  assert.match(keplrConnect, /finally \{[\s\S]*endWalletConnection\(connection\)/);
  assert.match(metaMaskSign, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(
    metaMaskSign,
    /Chain: \$\{activeChainProfile\(\)\?\.chainId \|\| state\.config\?\.chainId/,
  );
  assert.match(metaMaskSign, /withPrivacySessionGuard\([\s\S]*personal_sign/);
  assert.match(metaMaskSign, /assertPrivacySessionCurrent\(session\);[\s\S]*state\.wallet\.signatureHash = signatureHash/);
  assert.match(metaMaskChain, /const current = await withPrivacySessionGuard\([\s\S]*session,[\s\S]*requestMetaMask\(\{ method: "eth_chainId" \}\)/);
  assert.match(metaMaskChain, /await withPrivacySessionGuard\([\s\S]*session,[\s\S]*wallet_switchEthereumChain/);
  assert.match(metaMaskChain, /await withPrivacySessionGuard\([\s\S]*session,[\s\S]*wallet_addEthereumChain/);
  assert.match(metaMaskChain, /const updated = await withPrivacySessionGuard\([\s\S]*session,[\s\S]*requestMetaMask\(\{ method: "eth_chainId" \}\)/);
  assert.match(keplrSign, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(keplrSign, /withPrivacySessionGuard\([\s\S]*signArbitrary/);
  assert.match(keplrSign, /withPrivacySessionGuard\([\s\S]*verifyArbitrary/);
  assert.match(accountChange, /walletConnectionSession\?\.wallet !== "metamask"/);
  assert.match(chainChange, /walletConnectionSession\?\.wallet !== "metamask"/);
  assert.match(keplrChange, /walletConnectionSession\?\.wallet !== "keplr"/);
  assert.match(walletBindings, /error\?\.privacySessionInvalidated/);
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
  assert.match(serverSource, /new Set\(\["alice", "bob", "relayer", "auditor"\]\)/);
  assert.match(serverSource, /localSigners: localSignerAdmin/);
  assert.match(serverSource, /localSignerSetup: localSignerMutation/);
  assert.match(serverSource, /faucet: localSignerMutation/);
  assert.match(serverSource, /depositProof: localSignerMutation/);
  assert.match(serverSource, /function depositProofFailure\(\)/);
  assert.match(serverSource, /"deposit proof generation failed", "proof_failed"/);
  assert.match(serverSource, /const depositProofOutputMaxBytes = 1 << 20/);
  assert.match(serverSource, /child\.stderr\.resume\(\)/);
  assert.match(serverSource, /relayer: localSignerMutation/);
  assert.match(serverSource, /auditorAdmin: localSignerAdmin/);
  assert.match(serverSource, /proverProxy: proverProxyEnabled\(req\)/);
  assert.match(serverSource, /function localAccountsForPublicConfig\(serverFeatures\)/);
  assert.match(serverSource, /serverFeatures\.localSignerAdmin[\s\S]*return accounts/);
  assert.match(serverSource, /accounts\.filter\(account => account\.name === localRelayerName\(\)\)/);
  assert.match(serverSource, /function publicConfig\(req\)/);
  assert.match(serverSource, /modeLabel: config\.localTestMode \? "Local Note Test Web" : "Public Node DApp"/);
  assert.match(appSource, /function serverFeature/);
  assert.match(
    appSource,
    /function serverFeature\(name\) \{[\s\S]*state\.config\?\.serverBacked === true[\s\S]*state\.config\?\.serverFeatures\?\.\[name\]/,
  );
  assert.match(appSource, /function renderServerFeatureVisibility/);
  assert.match(appSource, /modeBadge\.textContent/);
  assert.match(appSource, /modeBadge\.classList\.toggle\("public-mode"/);
  assert.match(appSource, /localSignerPanel\.hidden = !localSigners/);
  assert.match(appSource, /faucetRow\.hidden = !faucet/);
  assert.match(appSource, /auditorSection\.hidden = !auditorAdmin/);
  assert.match(appSource, /data\.config\?\.serverBacked !== true[\s\S]*!data\.config\?\.serverFeatures\?\.localSignerSetup/);
  assert.match(appSource, /serverFeature\("faucet"\)/);
});

test("DApp keeps EVM public send 0x-only without self-wallet suggestions", () => {
  assert.match(appSource, /import \{[\s\S]*bech32AddressToEvm,[\s\S]*\} from "clairveiljs\/evm"/);
  assert.match(appSource, /function connectedWalletAddressSuggestions/);
  assert.match(appSource, /function activeServerAccounts\(\) \{[\s\S]*return serverFeature\("localSigners"\) && selectedProfileMatchesServer\(\)[\s\S]*\? state\.accounts[\s\S]*: \[\]/);
  assert.match(appSource, /const accounts = activeServerAccounts\(\);[\s\S]*const preferred = accounts\.filter/);
  assert.match(appSource, /els\.accountSelect\.disabled = !accounts\.length/);
  assert.match(appSource, /if \(!accounts\.length\) \{[\s\S]*els\.keplrSendRecipient\.value = ""/);
  assert.match(appSource, /function isEvmAddress/);
  assert.match(appSource, /function isSendRecipientForWallet/);
  assert.match(appSource, /function activeTransparentAddressFormat/);
  assert.match(appSource, /function isEvmTransparentMode/);
  assert.match(appSource, /keplrSendRecipient\.placeholder =[\s\S]*transparentFormat === "evm" \? "0x\.\.\."/);
  assert.match(appSource, /veiledWithdrawRecipient\.placeholder =[\s\S]*transparentFormat === "evm" \? "0x\.\.\."/);
  assert.match(appSource, /relayWithdrawRecipient\.placeholder =[\s\S]*transparentFormat === "evm" \? "0x\.\.\."/);
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
  assert.match(appSource, /evmJsonRpc\("eth_getBalance", \[/);
});

test("DApp uses the npm ClairveilJS browser client for public wallet and privacy flows", () => {
  const depositPrepare = sourceBetween(
    appSource,
    "async function preparePrivacyDepositSignDoc",
    "async function preparePrivacyTransferSignDoc",
  );
  const relayExpiryRecovery = sourceBetween(
    appSource,
    "async function reconcileExpiredRelayWithdrawSnapshot",
    "async function reconcileDefiniteFailedReservation",
  );
  const definiteUnknownFailureRecovery = sourceBetween(
    appSource,
    "async function reconcileDefiniteFailedUnknownReservations",
    "async function refreshNoteReservationState",
  );
  const preparedNullifierPreflight = sourceBetween(
    appSource,
    "async function verifyPreparedNullifiersUnspentBeforeBroadcast",
    "function isDefiniteEvmReceiptFailure",
  );
  const relayPayloadCopy = sourceBetween(
    appSource,
    "async function copyRelayWithdrawPayload()",
    "function noBroadcastAttemptError",
  );
  assert.match(appSource, /import \{ createClairveilBrowserDappClient \} from "clairveiljs\/browser-dapp"/);
  assert.match(appSource, /from "clairveiljs\/reservation"/);
  assert.match(appSource, /function clairveilBrowserClient/);
  const browserClientFactory = sourceBetween(
    appSource,
    "function clairveilBrowserClient",
    "const chainSafetyRefreshIntervalMs",
  );
  assert.match(browserClientFactory, /queryTimeoutMs: 30_000/);
  assert.match(browserClientFactory, /nullifierFailover: false/);
  assert.match(appSource, /function currentNoteReservationManager/);
  assert.match(appSource, /createBrowserReservationStore/);
  assert.match(appSource, /createNoteReservationManager/);
  assert.match(appSource, /function refreshNoteReservationState/);
  assert.match(appSource, /function markPreparedReservationSubmitted/);
  assert.match(appSource, /function markPreparedReservationUnknown/);
	assert.match(appSource, /function markPreparedReservationManualReview/);
  assert.match(appSource, /function markReservationBatchUnknown/);
	assert.match(appSource, /function verifyPreparedNullifiersUnspentBeforeBroadcast/);
  assert.match(preparedNullifierPreflight, /const reservation = preparedReservation\(data\);[\s\S]*const nullifiers = reservationNullifiersFromPrepared\(data\);[\s\S]*if \(!nullifiers\.length\) \{[\s\S]*if \(!reservationIDs\(reservation\)\.length\) return;[\s\S]*noBroadcastAttemptError\([\s\S]*Input nullifiers are unavailable before broadcast[\s\S]*nullifierVerificationFailed = true/);
  assert.match(appSource, /function markPreparedReservationReplanRequired/);
  assert.match(appSource, /function markReservationBatchReplanRequired/);
  assert.match(appSource, /function currentRelayWithdrawMetadataStore/);
  assert.match(appSource, /function persistRelayWithdrawPayloadState/);
  assert.match(appSource, /function loadPersistedRelayWithdrawPayloadState/);
  assert.match(appSource, /function recoverActiveRelayWithdrawSnapshots/);
  assert.match(appSource, /function relaySnapshotFromActiveReservation/);
  assert.match(appSource, /createEncryptedBrowserMetadataStore/);
  assert.match(appSource, /EncryptedIndexedDbNoteStore/);
  assert.match(appSource, /clairveil:note-reservations:v2:/);
  assert.match(appSource, /clairveil:wallet-notes:v2:/);
  assert.match(appSource, /clairveil:relay-withdraw-payloads:v2:/);
  assert.match(appSource, /function noBroadcastAttemptError/);
  assert.match(appSource, /function broadcastAttemptMetadata/);
	assert.match(appSource, /opaque_broadcast_error_without_transaction_identity/);
	assert.match(appSource, /opaque_relay_broadcast_error_without_transaction_identity/);
  assert.match(appSource, /function hasBroadcastAttemptMetadata/);
  assert.match(appSource, /function reconcileFailedEvmReservation/);
  assert.match(appSource, /function reconcileFailedCosmosReservation/);
  assert.match(appSource, /function isDefiniteCosmosTxFailure/);
  assert.match(appSource, /function reconcileDefiniteFailedUnknownReservations/);
  assert.match(appSource, /definite_execution_failure: definiteExecutionFailure/);
  assert.match(appSource, /function reconcileRecoveredActiveReservations/);
  const rejectedBroadcast = sourceBetween(
    appSource,
    "async function markPreparedReservationBroadcastRejected",
    "async function markReservationBatchUnknown",
  );
  assert.match(rejectedBroadcast, /preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\)/);
  assert.match(rejectedBroadcast, /withPrivacySessionGuard\([\s\S]*manager\.markBroadcastRejected/);
  assert.match(rejectedBroadcast, /refreshNoteReservationState\(\{ session \}\)/);
  const manualReview = sourceBetween(
    appSource,
    "async function markPreparedReservationManualReview",
    "async function markReservationBatchReplanRequired",
  );
  assert.match(manualReview, /preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\)/);
  assert.match(manualReview, /markReservationBatchManualReview\([\s\S]*\{ session \}/);
  const replanRequired = sourceBetween(
    appSource,
    "async function markPreparedReservationReplanRequired",
    "function selectedNotesFromPlan",
  );
  assert.match(replanRequired, /preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\)/);
  assert.match(replanRequired, /markReservationBatchReplanRequired\([\s\S]*\{ session \}/);
  assert.match(appSource, /canReplanExpiredLocalReservation/);
  assert.match(appSource, /relay_payload_expired_requires_manual_review/);
  assert.match(appSource, /function currentReservationWorkerID/);
  assert.match(appSource, /leaseOwner: currentReservationWorkerID\(\)/);
  assert.match(appSource, /function reservationCanRecoverAfterWorkerExpiry/);
  assert.match(relayReservationStateSource, /function hasDurableNoBroadcastEvidence/);
  assert.match(appSource, /cacheReservationRecords\(active, \{ replace: true \}\)/);
  assert.match(appSource, /reservationHasLiveLease/);
  assert.match(appSource, /recovered_local_prepare_without_broadcast/);
  assert.match(appSource, /recovered_proof_ready_without_durable_pre_broadcast_evidence/);
  assert.match(appSource, /recovered_relay_proof_ready_without_durable_pre_broadcast_evidence/);
  assert.match(appSource, /recovered_definite_tx_failure_nullifier_unspent/);
  assert.match(appSource, /recovered_tx_not_found_manual_review/);
  assert.match(appSource, /function recoveredReservationTxOutcome/);
  assert.match(appSource, /String\(reservation\.tx_bytes_hash \|\| ""\)/);
  assert.match(appSource, /const definiteFailureReasons = new Set\(/);
  assert.match(appSource, /"cosmos_tx_code_failed"/);
  assert.match(appSource, /"evm_receipt_failed"/);
  assert.match(definiteUnknownFailureRecovery, /const outcome = await withPrivacySessionGuard\([\s\S]*recoveredReservationTxOutcome\(first, \{ session \}\)/);
  assert.match(definiteUnknownFailureRecovery, /if \(!outcome\.checked \|\| !outcome\.found \|\| !outcome\.failed\) continue;/);
  assert.match(definiteUnknownFailureRecovery, /txHashChecked:\s*outcome\.txHash \|\| first\.submitted_tx_hash \|\| first\.tx_bytes_hash \|\| true/);
  assert.match(appSource, /\$\{failureReason\}_later_nullifier_reconcile/);
  assert.match(appSource, /function isDefiniteEvmReceiptFailure/);
  assert.match(appSource, /markPreparedReservationBroadcastAttempting/);
  assert.match(appSource, /submitted_write_failed_after_external_broadcast/);
  assert.match(appSource, /isEvmReceiptConfirmationPending/);
  assert.match(appSource, /function updateRelayWithdrawReservationRecords/);
  assert.match(appSource, /function markRelayReservationHandedOff/);
  assert.match(appSource, /function reservationHasRelayHandoffEvidence\(record = \{\}\)/);
  assert.match(appSource, /async function discardPreparedRelayWithdrawPayload\([\s\S]*records\.some\(reservationHasRelayHandoffEvidence\)[\s\S]*stashHandedOffPreparedRelayWithdrawPayload\(\)[\s\S]*return;/);
  const recoveredRelayPayload = sourceBetween(
    appSource,
    "async function replanRecoveredLocalRelayPayload",
    "function reservationHasRelayHandoffEvidence",
  );
  assert.match(recoveredRelayPayload, /const handedOff = Boolean\([\s\S]*recoverySnapshot\.handedOff \|\| records\.some\(reservationHasRelayHandoffEvidence\)/);
  assert.match(recoveredRelayPayload, /const recoveredSnapshot = \{[\s\S]*handedOff,[\s\S]*\};[\s\S]*expiredRelayReservationRecoveryTarget\(\{[\s\S]*handedOff,/);
  assert.match(recoveredRelayPayload, /if \(!target\) return recoveredSnapshot;/);
  assert.match(appSource, /const expectedVersion = state\.keplr\.relayWithdrawPayloadVersion/);
  assert.match(appSource, /const copySnapshot = currentPreparedRelayWithdrawSnapshot\(\)/);
  assert.match(appSource, /async function copyRelayWithdrawPayload\(\)[\s\S]*const session = beginPrivacySessionOperation\(\)/);
  assert.match(appSource, /copyIsCurrent = \(\) =>[\s\S]*isPrivacySessionCurrent\(session\)[\s\S]*relayWithdrawPayloadVersion === expectedVersion/);
  assert.match(appSource, /relayWithdrawPayloadCopyInFlight = true/);
  assert.match(appSource, /await markRelayReservationHandedOff\(\s*copySnapshot\.reservation,\s*copySnapshot\.payload\?\.payload_hash,[\s\S]*expectedPayloadVersion: expectedVersion/);
  assert.match(appSource, /verifyRelayPayloadNullifierUnspentBeforeBroadcast\([\s\S]*copySnapshot\.preparedData,[\s\S]*\{ session \}/);
  assert.match(appSource, /extendReservationBatchLeaseToPayloadExpiry\([\s\S]*copySnapshot\.payload,[\s\S]*expectedPayloadVersion: expectedVersion, session/);
  assert.match(appSource, /await persistRelayWithdrawPayloadState\(\{ session \}\)[\s\S]*assertCopyIsCurrent\(\)/);
  assert.match(appSource, /canRelayHandoffPayloadBeCopied/);
  assert.match(relayPayloadCopy, /const currentReservationRecords = await latestReservationRecords\([\s\S]*copySnapshot\.reservation/);
  assert.match(relayPayloadCopy, /const finalReservationRecords = await latestReservationRecords\([\s\S]*copySnapshot\.reservation/);
  assert.match(relayPayloadCopy, /canRelayHandoffPayloadBeCopied\([\s\S]*finalReservationRecords/);
  assert.match(relayPayloadCopy, /const finalChainSnapshot = await withPrivacySessionGuard\([\s\S]*latestRelayChainSnapshot\(\)/);
  assert.match(relayPayloadCopy, /relaySnapshotIsExpired\(copySnapshot, finalChainSnapshot\.chainNowMs\)/);
  assert.ok(
    relayPayloadCopy.indexOf("const finalReservationRecords") <
      relayPayloadCopy.indexOf("clipboardAttempted = true"),
  );
  assert.ok(
    relayPayloadCopy.indexOf("const finalChainSnapshot") <
      relayPayloadCopy.indexOf("clipboardAttempted = true"),
  );
  assert.match(relayPayloadCopy, /clipboardAttempted = true;\s*await withPrivacySessionGuard\([\s\S]*navigator\.clipboard\.writeText\(copySnapshot\.payloadText\)/);
  assert.match(appSource, /navigator\.clipboard\.writeText\(copySnapshot\.payloadText\)/);
  assert.match(appSource, /copyRelayWithdrawPayload\(\)\.catch\(\(error\) => \{[\s\S]*!error\?\.privacySessionInvalidated/);
  assert.match(appSource, /relay_handed_off/);
  assert.match(appSource, /relayReservationStatus/);
  assert.match(appSource, /relayBroadcastTxHash/);
  assert.match(appSource, /relaySnapshotExpiresAtUnix/);
  assert.match(appSource, /relaySnapshotIsExpired/);
  assert.match(appSource, /sanitizeRelayWithdrawSnapshot/);
  assert.match(appSource, /updateReservationBatchRecords/);
  assert.match(appSource, /\.filter\(relaySnapshotNeedsPendingRecovery\)/);
  assert.match(appSource, /canRelaySnapshotBeSubmitted/);
  assert.match(appSource, /isRelaySnapshotStructurallyReady/);
  assert.match(appSource, /const relayPayloadReady = isRelayPreparedWithdrawStructurallyReady\(\)/);
  assert.match(appSource, /function canRelayPreparedWithdrawPayload/);
  assert.match(appSource, /receipt status/);
  assert.match(appSource, /EVM tx failed/);
  assert.match(appSource, /function renewReservationBatchLease/);
  assert.match(appSource, /function extendReservationBatchLeaseToPayloadExpiry/);
  assert.match(appSource, /function withReservationHeartbeat/);
  assert.match(appSource, /function withPreparedReservationHeartbeat/);
  assert.match(appSource, /function noteReservationBookkeeping/);
  assert.match(appSource, /function warnReservationBookkeeping/);
  const exactWithdrawNotePlanner = sourceBetween(
    appSource,
    "async function createExactWithdrawNote",
    "async function sendFromKeplr",
  );
  const preparedSelfTransferConfirmations = [
    ...exactWithdrawNotePlanner.matchAll(
      /await confirmPreparedTransferBeforeBroadcast\(\s*data,\s*\{ session, selfTransfer: true \},\s*\)/g,
    ),
  ];
  assert.equal(
    preparedSelfTransferConfirmations.length,
    2,
    "each exact-note self-transfer must receive its own prepared-effect approval",
  );
  assert.ok(
    preparedSelfTransferConfirmations[0].index <
      exactWithdrawNotePlanner.indexOf("broadcastPreparedPrivacy(\n        data,\n        \"exact-note self transaction\""),
  );
  assert.ok(
    preparedSelfTransferConfirmations[1].index <
      exactWithdrawNotePlanner.indexOf("broadcastPreparedPrivacy(\n      data,\n      \"exact-note self transfer\""),
  );
  assert.match(exactWithdrawNotePlanner, /preparedSelfTransferCancelled = true/);
  assert.match(appSource, /(?:clairveilBrowserClient\(\)|client)\.fetchPrivacyEvents\(\{/);
  assert.match(appSource, /\/api\/auditor\/transfers\?page=\$\{requestedPage\}&limit=\$\{auditorEventsPageLimit\}/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareDeposit/);
  assert.match(appSource, /depositProofProvider/);
  assert.match(appSource, /function browserDepositProofUrl/);
  assert.match(
    appSource,
    /function hasLocalDepositProofProvider\(\) \{[\s\S]*state\.config\?\.serverBacked === true[\s\S]*serverFeature\("depositProof"\)/,
  );
  assert.match(appSource, /function hasDepositProofProvider/);
  assert.match(
    appSource,
    /function hasDepositProofProvider[\s\S]*hasLocalDepositProofProvider\(\)/,
  );
  assert.match(appSource, /profile\?\.depositProofUrl/);
  assert.match(appSource, /api\(proofEndpoint/);
  assert.match(appSource, /\/api\/deposit\/proof/);
  assert.match(appSource, /Deposit requires a DepositCircuit proof provider/);
  assert.match(depositPrepare, /request\.depositProofProvider = async/);
  assert.match(depositPrepare, /const depositProofEndpoint = browserDepositProofUrl\(\)/);
  assert.match(depositPrepare, /const localDepositProofAvailable = hasLocalDepositProofProvider\(\)/);
  assert.match(depositPrepare, /assertPrivacySessionCurrent\(session\);/);
  assert.match(depositPrepare, /withPrivacySessionGuard\(\s*session,\s*\(\) => api\(proofEndpoint/);
  assert.match(appSource, /function requireVersionedDepositProofResponse\(response\)/);
  assert.match(
    appSource,
    /response\.version !== depositProofResponseVersion[\s\S]*error\.code = "proof_failed"/,
  );
  assert.match(depositPrepare, /return requireVersionedDepositProofResponse\(response\)/);
  assert.doesNotMatch(depositPrepare, /const endpoint = browserDepositProofUrl\(\)/);
  assert.doesNotMatch(depositPrepare, /state\.activeWallet !== "metamask"/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareTransfer/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareWithdraw/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.prepareRelayWithdraw/);
  assert.match(appSource, /const reservationManager = currentNoteReservationManager\(\);/);
  assert.match(appSource, /session\.reservationManager = reservationManager;/);
  assert.match(appSource, /reservationManager,\s*\.\.\.disclosure/);
  assert.match(appSource, /withPrivacySessionGuard\([\s\S]*manager\.markSubmitted/);
  assert.match(appSource, /withPrivacySessionGuard\([\s\S]*manager\.markUnknown/);
  assert.match(appSource, /withPrivacySessionGuard\([\s\S]*manager\.markReplanRequired/);
  assert.match(appSource, /withPrivacySessionGuard\([\s\S]*manager\.renewLease/);
  assert.match(appSource, /leaseToken: reservationLeaseToken\(reservation\)/);
  assert.match(appSource, /if \(!hasBroadcastAttemptMetadata\(attempt\)\) return/);
  assert.match(appSource, /error\?\.noBroadcastAttempt/);
  assert.match(
    appSource,
    /markPreparedReservationReplanRequired\(data, error, undefined, \{[\s\S]*session,[\s\S]*\}\)/,
  );
  assert.match(appSource, /no_broadcast_attempt:[\s\S]*!hasBroadcastAttemptMetadata/);
  assert.match(appSource, /withPreparedReservationHeartbeat\(data/);
  assert.match(appSource, /function reservationReconciliationRequiredError/);
  assert.match(appSource, /function recordSubmittedReservation/);
  assert.match(appSource, /await recordSubmittedReservation\([\s\S]*preparedReservation\(data\)/);
  assert.match(appSource, /error\?\.reservationReconciliationRequired/);
  assert.match(appSource, /noteReservationBookkeeping\(\(\) =>[\s\S]*markPreparedReservationUnknown/);
  assert.match(appSource, /"evm_receipt_failed_nullifier_unspent"/);
  assert.match(appSource, /"cosmos_tx_code_failed_nullifier_unspent"/);
  assert.match(appSource, /cosmos_tx_code_failed_pending_nullifier_reconcile/);
  assert.match(appSource, /evm_receipt_failed_pending_nullifier_reconcile/);
  assert.match(appSource, /nullifierUnspentConfirmed: true/);
  assert.match(appSource, /txAbsentOrFailedConfirmed: true/);
  assert.match(appSource, /txHashChecked: attempt\.txHash \|\| attempt\.txBytesHash \|\| attempt\.signDocHash/);
  assert.match(appSource, /reconcileFailedEvmReservation\([\s\S]*finalData,[\s\S]*reservationStatuses\.Submitted/);
  assert.match(appSource, /reconcileFailedEvmReservation\([\s\S]*data,[\s\S]*reservationStatuses\.Submitted/);
  const definiteFailureRecovery = sourceBetween(
    appSource,
    "async function reconcileDefiniteFailedReservation",
    "async function broadcastPreparedPrivacy",
  );
  assert.match(
    definiteFailureRecovery,
    /\{ session = preparedPrivacySession\(data\) \|\| beginPrivacySessionOperation\(\) \} = \{\}/,
  );
  assert.match(
    definiteFailureRecovery,
    /withPrivacySessionGuard\(\s*session,\s*\(\) => latestRelayChainSnapshot\(\),\s*\)/,
  );
  assert.match(
    definiteFailureRecovery,
    /withPrivacySessionGuard\(\s*session,\s*\(\) => Promise\.all\(\s*nullifiers\.map\(\(nullifier\) => checkNullifierSpent\(nullifier, \{ session \}\)\),\s*\),\s*\)/,
  );
  assert.match(definiteFailureRecovery, /markReservationBatchUnknown\([\s\S]*\{ session \}/);
  assert.match(definiteFailureRecovery, /refreshCachedNoteStatuses\(\{ session \}\)/);
  assert.match(definiteFailureRecovery, /reconcileReservedNotesFromScan\(\{ session \}\)/);
  assert.match(definiteFailureRecovery, /withPrivacySessionGuard\([\s\S]*manager\.markReplanRequired/);
  assert.match(
    definiteFailureRecovery,
    /catch \(transitionError\) \{[\s\S]*manager\.getReservation\(id\)[\s\S]*reservationStatuses\.ReplanRequired/,
  );
  assert.doesNotMatch(
    definiteFailureRecovery,
    /expected .*?, got ReplanRequired/,
  );
  assert.match(appSource, /Transfer submitted; reservation reconciliation required/);
  assert.match(appSource, /Withdraw submitted; reservation reconciliation required/);
  assert.match(
    appSource,
    /withPrivacySessionGuard\(\s*session,\s*\(\) => manager\.reconcileSpentNotes\(notes\),\s*\)/,
  );
  assert.match(appSource, /noteReservationByNullifier/);
  assert.match(appSource, /function noteHasVerifiedUnspentNullifier/);
  assert.match(appSource, /\^\[0-9a-f\]\{64\}\$\/\.test\(nullifier\)/);
  assert.match(appSource, /nullifierStatus === "unspent"/);
  assert.match(appSource, /function isSpendableNote[\s\S]*noteHasVerifiedUnspentNullifier\(note\)/);
  assert.match(appSource, /function noteUsesCurrentAsset\(note\)[\s\S]*noteAssetDenom\(note\) === baseDenom\(\)/);
  assert.match(appSource, /function isCurrentAssetSpendableNote\(note\)[\s\S]*isSpendableNote\(note\) && noteUsesCurrentAsset\(note\)/);
  assert.match(appSource, /function noteHasConfirmedSpentReservation\(note\)[\s\S]*reservationStatuses\.ConfirmedSpent/);
  assert.match(appSource, /function noteHasBlockingReservation\(note\)[\s\S]*noteHasActiveReservation\(note\) \|\| noteHasConfirmedSpentReservation\(note\)/);
  assert.match(appSource, /function isAvailableSpendableNote\(note\)[\s\S]*!noteHasBlockingReservation\(note\)/);
  assert.match(
    appSource,
    /function refreshNotesSummary\(\) \{[\s\S]*summarizeSpendableValueNotes\(state\.keplr\.notes\)[\s\S]*renderBatchTransferPreview\(\)/,
  );
  assert.match(appSource, /if \(noteHasActiveReservation\(note\)\) return "Reserved"/);
  assert.match(appSource, /if \(noteHasConfirmedSpentReservation\(note\)\) return "Confirmed spent"/);
  assert.match(
    appSource,
    /const visibleNotes = state\.keplr\.notes\.filter\(\s*\(note\) => !noteHasConfirmedSpentReservation\(note\),\s*\);[\s\S]*const valueNotes = visibleNotes\.filter/,
  );
  assert.match(appSource, /if \(isUnverifiedNote\(note\)\) return "Unverified"/);
  assert.match(appSource, /if \(!noteUsesCurrentAsset\(note\)\) return "Other asset"/);
  assert.match(
    appSource,
    /return isSpendableNote\(note\) \? "Spendable" : "Spent"/,
  );
  assert.match(
    appSource,
    /const helperCount = \(notes \|\| \[\]\)\.filter\(\s*\(note\) => isAvailableSpendableNote\(note\) && isZeroAmountNote\(note\),\s*\)\.length/,
  );
  assert.match(
    appSource,
    /const reservedHelperCount = \(notes \|\| \[\]\)\.filter\(\s*\(note\) =>\s*isCurrentAssetSpendableNote\(note\) &&\s*isZeroAmountNote\(note\) &&\s*noteHasActiveReservation\(note\),\s*\)\.length/,
  );
  assert.match(appSource, /Reserved helper/);
  assert.match(appSource, /Confirmed spent/);
  assert.match(appSource, /Other asset/);
  assert.match(appSource, /function noteStatusClass/);
  assert.match(
    appSource,
    /async function setPreparedRelayWithdrawPayload[\s\S]*await refreshNoteReservationState\(\{ session \}\)/,
  );
  assert.doesNotMatch(cssSource, /\.note-row\.reserved-note/);
  assert.doesNotMatch(cssSource, /\.note-status-reserved/);
  assert.match(htmlSource, /class="panel account-panel relayer-panel"/);
  assert.match(htmlSource, /<h2>Relay Withdraw<\/h2>/);
  assert.match(htmlSource, /id="relayerPrepareTitle"[\s\S]*<h3 class="relayer-account-title">Local Relayer \(optional\)<\/h3>/);
  assert.match(htmlSource, /<h3 class="relayer-account-title">Local Relayer \(optional\)<\/h3>/);
  assert.doesNotMatch(htmlSource, /id="relayerAccountName"/);
  assert.match(htmlSource, /id="relayerBalance"/);
  assert.match(htmlSource, /Prepared Payload/);
  assert.match(htmlSource, /id="relayWithdrawAmount"/);
  assert.match(htmlSource, /class="amount-row relayer-prepare-amount-row"[\s\S]*id="relayWithdrawAmount"[\s\S]*id="relayWithdrawFromVeiled"[\s\S]*<div class="field address-field">[\s\S]*id="relayWithdrawRecipient"/);
  assert.match(htmlSource, /id="relayWithdrawRecipient"/);
  assert.match(htmlSource, /id="relayWithdrawRecipientSuggestions"/);
  assert.match(htmlSource, /id="relayWithdrawRecipient"[\s\S]*aria-haspopup="listbox"[\s\S]*aria-controls="relayWithdrawRecipientSuggestions"/);
  assert.doesNotMatch(htmlSource, /id="relayWithdrawRecipientSuggestions"[\s\S]*id="relayWithdrawFromVeiled"/);
  assert.match(htmlSource, /id="relayPreparedWithdraw"/);
  assert.match(htmlSource, /id="copyRelayWithdrawPayload"/);
  assert.match(htmlSource, /id="relayWithdrawPreparedChainId"/);
  assert.match(htmlSource, /id="relayWithdrawPreparedExpiresAt"/);
  assert.match(htmlSource, /id="relayWithdrawPendingList"/);
  assert.match(htmlSource, /Pending Handoffs/);
  assert.match(htmlSource, /prepared[\s\S]*payload\/proof JSON is privacy-sensitive/);
  assert.match(htmlSource, /Only relay metadata[\s\S]*is kept after refresh/);
  assert.match(appSource, /input: els\.relayWithdrawRecipient,[\s\S]*list: els\.relayWithdrawRecipientSuggestions,[\s\S]*kind: "transparent"/);
  assert.match(appSource, /function setPreparedRelayWithdrawPayload/);
  assert.match(appSource, /relayWithdrawPayloadHandedOff: false/);
  assert.match(appSource, /relayWithdrawPendingPayloads: \[\]/);
  assert.match(appSource, /relayWithdrawReservation/);
  assert.match(appSource, /relayWithdrawPreparedData/);
  assert.match(appSource, /function rememberPendingRelayWithdrawPayload/);
  assert.match(appSource, /function replacePendingRelayWithdrawPayload/);
  assert.match(appSource, /createEncryptedStateCodec/);
  assert.match(appSource, /requireLocks: true/);
  assert.match(appSource, /encodeState: encryption\.encodeState/);
  assert.match(appSource, /decodeState: encryption\.decodeState/);
  assert.doesNotMatch(appSource, /unsafeAllowPlaintext: true/);
  assert.doesNotMatch(appSource, /window\.localStorage/);
  assert.doesNotMatch(appSource, /window\.sessionStorage/);
  assert.doesNotMatch(appSource, /relayWithdrawPendingPayloads = \[[\s\S]*\.slice\(0, 5\)/);
  assert.match(appSource, /function stashHandedOffPreparedRelayWithdrawPayload/);
  assert.match(appSource, /function restorePendingRelayWithdrawPayload/);
  const pendingRelayRecovery = sourceBetween(
    appSource,
    "async function restorePendingRelayWithdrawPayload",
    "async function setPreparedRelayWithdrawPayload",
  );
  const relayPayloadDiscard = sourceBetween(
    appSource,
    "async function discardPreparedRelayWithdrawPayload",
    "function clearPreparedRelayWithdrawPayload",
  );
  assert.match(pendingRelayRecovery, /async function restorePendingRelayWithdrawPayload\([\s\S]*\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(pendingRelayRecovery, /async function refreshPendingRelayWithdrawPayloadStatus\([\s\S]*\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(pendingRelayRecovery, /const expectedPayloadVersion = state\.keplr\.relayWithdrawPayloadVersion/);
  assert.match(pendingRelayRecovery, /assertPreparedPayloadIsCurrent\(\);[\s\S]*const cleared = await discardAndClearPreparedRelayWithdrawPayload\(\{ session \}\)/);
  assert.match(pendingRelayRecovery, /if \(!cleared\) return;/);
  assert.match(pendingRelayRecovery, /const finalRecords = await latestReservationRecords\(synced\.reservation, \{[\s\S]*session,/);
  assert.match(pendingRelayRecovery, /latestReservationRecords\(synced\.reservation, \{[\s\S]*assertPreparedPayloadIsCurrent\(\);[\s\S]*const finalReservation = updateReservationBatchRecords\(/);
  assert.match(pendingRelayRecovery, /finalRecords\.length !== reservationIDs\(synced\.reservation\)\.length[\s\S]*relayReservationStatus\(finalReservation\) !== reservationStatuses\.ProofReady/);
  assert.match(pendingRelayRecovery, /replacePendingRelayWithdrawPayload\([\s\S]*\{ \.\.\.synced, reservation: finalReservation \},[\s\S]*expectedSnapshot: synced/);
  assert.match(pendingRelayRecovery, /applyPreparedRelayWithdrawSnapshot\(\{[\s\S]*reservation: finalReservation,/);
  assert.match(pendingRelayRecovery, /pendingRelayWithdrawPayloadIsCurrent\(id, snapshot\)/);
  assert.match(pendingRelayRecovery, /syncRelayWithdrawSnapshotReservation\(snapshot, \{ session \}\)/);
  assert.match(pendingRelayRecovery, /replacePendingRelayWithdrawPayload\(id, synced, \{[\s\S]*expectedSnapshot: snapshot/);
  assert.match(pendingRelayRecovery, /replacePendingRelayWithdrawPayload\(id, null, \{[\s\S]*expectedSnapshot: synced/);
  assert.match(pendingRelayRecovery, /replacePendingRelayWithdrawPayload\(id, reconciled, \{[\s\S]*expectedSnapshot: snapshot/);
  assert.match(appSource, /function replacePendingRelayWithdrawPayload\([\s\S]*expectedSnapshot = null[\s\S]*item !== expectedSnapshot/);
  assert.match(pendingRelayRecovery, /discardAndClearPreparedRelayWithdrawPayload\(\{ session \}\)/);
  assert.match(pendingRelayRecovery, /resolveExpiredRelayManualReview\(synced, null, \{ session \}\)/);
  assert.match(pendingRelayRecovery, /reconcileExpiredRelayWithdrawSnapshot\(synced, null, \{ session \}\)/);
  assert.match(pendingRelayRecovery, /await persistRelayWithdrawPayloadState\(\{ session \}\)[\s\S]*assertPrivacySessionCurrent\(session\)[\s\S]*return reconciled;/);
  assert.match(relayPayloadDiscard, /latestReservationRecords\(reservation, \{ session \}\)/);
  assert.match(relayPayloadDiscard, /markReservationBatchReplanRequired\([\s\S]*\{ session \}/);
  assert.match(relayPayloadDiscard, /markReservationBatchManualReview\([\s\S]*\{ session \}/);
  assert.match(appSource, /function renderPendingRelayWithdrawPayloads/);
  assert.match(appSource, /function discardPreparedRelayWithdrawPayload/);
  assert.match(appSource, /function discardAndClearPreparedRelayWithdrawPayload/);
  assert.match(appSource, /const expectedVersion = state\.keplr\.relayWithdrawPayloadVersion/);
  assert.match(appSource, /relayWithdrawPayloadVersion !== expectedVersion/);
  assert.match(appSource, /local_relay_payload_discarded_before_handoff/);
  assert.match(appSource, /local_relay_payload_overwritten_before_handoff/);
  assert.match(appSource, /const canonicalRecipient =[\s\S]*payload\?\.recipient[\s\S]*data\?\.prepared\?\.recipient[\s\S]*recipient/);
  assert.match(appSource, /relayWithdrawPayloadRecipient = canonicalRecipient \|\| ""/);
  assert.match(appSource, /relayWithdrawPayloadChainId =[\s\S]*payload\?\.chain_id[\s\S]*payload\?\.chainId/);
  assert.match(appSource, /relayWithdrawPayloadExpiresAt = formatRelayPayloadExpiry/);
  assert.match(appSource, /extendReservationBatchLeaseToPayloadExpiry\(/);
  assert.match(appSource, /state\.keplr\.relayWithdrawPayloadHandedOff = true/);
  assert.match(appSource, /let relayDispatchMarked = false/);
  assert.match(appSource, /relayDispatchMarked = true/);
  assert.match(appSource, /if \(!relayDispatchMarked && !hasBroadcastAttemptMetadata\(attempt\)\)/);
  assert.match(appSource, /await clearPreparedRelayWithdrawPayload\([\s\S]*session,/);
  assert.match(appSource, /loadPersistedRelayWithdrawPayloadState\(\{ session \}\)/);
  assert.match(appSource, /stashHandedOffPreparedRelayWithdrawPayload\(\)/);
  assert.match(appSource, /discardAndClearPreparedRelayWithdrawPayload\(\{ session \}\)[\s\S]*!error\?\.privacySessionInvalidated[\s\S]*\.finally\(\(\) => \{[\s\S]*isPrivacySessionCurrent\(session\)[\s\S]*renderKeplr\(\)/);
  assert.match(appSource, /relayWithdrawPendingList/);
  assert.match(cssSource, /\.pending-relay-item\s*\{/);
  assert.match(appSource, /function renderRelayerPanel/);
  assert.match(appSource, /function localRelayerAccount\(\) \{[\s\S]*serverFeature\("relayer"\)[\s\S]*account\.name === "relayer"[\s\S]*account\.name === "dev0"/);
  assert.match(appSource, /function refreshRelayerAccount/);
  assert.match(appSource, /function relayPreparedWithdraw/);
  assert.match(appSource, /function verifyRelayPayloadNullifierUnspentBeforeBroadcast/);
  assert.match(appSource, /function reconcileExpiredRelayWithdrawPayloads/);
  assert.match(appSource, /function privacyRecoveryErrorMessage/);
  const pendingRelayRecoveryUi = sourceBetween(
    appSource,
    "function renderPendingRelayWithdrawPayloads",
    "function renderRelayerPanel",
  );
  assert.doesNotMatch(pendingRelayRecoveryUi, /toast\(error\.message\)/);
  assert.match(pendingRelayRecoveryUi, /const session = beginPrivacySessionOperation\(\);\s*const recoveryLock = beginPendingRelayRecovery\(id, session\);\s*if \(!recoveryLock\) return;\s*action\.disabled = true;[\s\S]*restorePendingRelayWithdrawPayload\(id, \{ session \}\)/);
  assert.match(pendingRelayRecoveryUi, /refreshPendingRelayWithdrawPayloadStatus\(id, \{ session \}\)[\s\S]*\.finally\(\(\) => \{\s*endPendingRelayRecovery\(recoveryLock\);\s*if \(isPrivacySessionCurrent\(session\)\) action\.disabled = false;/);
  assert.match(pendingRelayRecoveryUi, /privacyRecoveryErrorMessage\([\s\S]*Relay payload could not be restored/);
  assert.match(pendingRelayRecoveryUi, /privacyRecoveryErrorMessage\([\s\S]*Relay recovery status could not be refreshed/);
  assert.match(htmlSource, /id="relayWithdrawManualReviewEvidence"/);
  assert.match(cssSource, /\.relay-review-evidence\s*\{/);
  assert.match(pendingRelayRecoveryUi, /const pendingStatus = relayReservationStatus\(item\.reservation\);[\s\S]*const displayRecords = manualReviewRecordsForDisplay\([\s\S]*pendingStatus === reservationStatuses\.ManualReview[\s\S]*appendManualReviewEvidence\([\s\S]*displayRecords,[\s\S]*payloadHash: item\.payloadHash/);
  assert.match(pendingRelayRecoveryUi, /\? "Check recovery"[\s\S]*: "Refresh status"[\s\S]*Checks authoritative expiry[\s\S]*refreshPendingRelayWithdrawPayloadStatus\(id, \{ session \}\)[\s\S]*relayManualReviewRecoveryMessage\(reconciled\)/);
  const relayerPanel = sourceBetween(
    appSource,
    "function renderRelayerPanel",
    "function activeServerAccounts",
  );
  assert.match(relayerPanel, /renderRelayManualReviewEvidence\([\s\S]*relayNeedsManualResolution,[\s\S]*state\.keplr\.relayWithdrawReservation,[\s\S]*state\.keplr\.relayWithdrawPayloadHash/);
  assert.match(appSource, /function renderRelayManualReviewEvidence\([\s\S]*Manual review evidence[\s\S]*appendManualReviewEvidence\([\s\S]*payloadHash/);
  const generalManualReviewUi = sourceBetween(
    appSource,
    "function renderReservationManualReviews",
    "function renderAccounts",
  );
  assert.doesNotMatch(generalManualReviewUi, /toast\(error\.message\)/);
  assert.match(generalManualReviewUi, /openReservationReviewDialog\(operationID, operationRecords\)/);
  assert.match(appSource, /function openReservationReviewDialog\([\s\S]*지갑 승인 창을 열기 전 브라우저 흐름이 끊겼을 수 있습니다[\s\S]*사용자가 지갑 요청을 취소했을 수 있습니다[\s\S]*제출 결과를 저장하기 전 새로고침 또는 오류가 발생했을 수 있습니다/);
  assert.match(appSource, /els\.confirmReservationReview\.addEventListener\("click", async \(\) => \{[\s\S]*resolveGeneralManualReviewOperation\(operationID, \{[\s\S]*allowExplicitUntrackedCancellation: explicitCancellation/);
  assert.match(appSource, /function manualReviewResolutionErrorMessage\([\s\S]*privacyRecoveryErrorMessage/);
  assert.match(htmlSource, /id="reservationReviewModal"[\s\S]*id="reservationReviewCauses"[\s\S]*id="reservationReviewAcknowledge"[\s\S]*id="confirmReservationReview"/);
  assert.match(cssSource, /\.reservation-review-warning\s*\{/);
  const relayPayloadClearForExpirySchedule = sourceBetween(
    appSource,
    "async function clearPreparedRelayWithdrawPayload",
    "function renderPendingRelayWithdrawPayloads",
  );
  assert.match(appSource, /function stopRelayPayloadExpiryReconciliation/);
  assert.match(appSource, /async function scheduleRelayPayloadExpiryReconciliation/);
  const relayExpirySchedule = sourceBetween(
    appSource,
    "function relaySnapshotsAwaitingExpiryReconciliation",
    "function rememberPendingRelayWithdrawPayload",
  );
  assert.doesNotMatch(relayExpirySchedule, /Boolean\(snapshot\?\.payload\)/);
  assert.match(relayExpirySchedule, /relayReservationStatus\(snapshot\.reservation\) === reservationStatuses\.ProofReady/);
  assert.match(appSource, /latestRelayChainSnapshot\(\)/);
  assert.match(appSource, /expiresAtUnix \* 1000 - chainNowMs \+ 1/);
  assert.match(appSource, /void reconcileExpiredRelayWithdrawPayloads\(null, \{ session \}\)/);
  assert.match(appSource, /stopRelayPayloadExpiryReconciliation\(\);[\s\S]*privacySessionGeneration \+= 1/);
  assert.match(relayPayloadClearForExpirySchedule, /stopRelayPayloadExpiryReconciliation\(\);[\s\S]*stashHandedOffPreparedRelayWithdrawPayload\(\)[\s\S]*void scheduleRelayPayloadExpiryReconciliation\(\{ session \}\)/);
  const relayExpiryRecoveryList = sourceBetween(
    appSource,
    "async function reconcileExpiredRelayWithdrawPayloads",
    "async function reconcileDefiniteFailedReservation",
  );
  assert.match(relayExpiryRecoveryList, /const currentPayloadVersion = state\.keplr\.relayWithdrawPayloadVersion/);
  assert.match(relayExpiryRecoveryList, /state\.keplr\.relayWithdrawPayloadVersion === currentPayloadVersion/);
  assert.match(relayExpiryRecoveryList, /const latestPending = Array\.isArray\(state\.keplr\.relayWithdrawPendingPayloads\)/);
  assert.match(relayExpiryRecoveryList, /initial !== snapshot/);
  assert.match(appSource, /async function relaySnapshotWithFullReservationRecords/);
  assert.match(appSource, /const recoverySnapshot = await relaySnapshotWithFullReservationRecords\(snapshot,\s*\{\s*session,/);
  assert.match(appSource, /const recoverySnapshot = await relaySnapshotWithFullReservationRecords\(synced,\s*\{\s*session,/);
  assert.match(appSource, /relay_payload_expired_requires_manual_review/);
  assert.match(appSource, /relay_payload_expired_after_handoff/);
  assert.match(appSource, /no_broadcast_attempt: false/);
  assert.doesNotMatch(appSource, /relay_payload_expired_without_confirmed_broadcast_outcome/);
  assert.match(
    relayExpiryRecovery,
    /!nullifierStatuses\.length[\s\S]*nullifierStatuses\.some\(\(entry\) => entry\.spent == null\)[\s\S]*relay_payload_expired_nullifier_evidence_unavailable/,
  );
  assert.match(
    relayExpiryRecovery,
    /new Error\("relay payload expired and input nullifier evidence is unavailable"\)[\s\S]*markReservationBatchManualReview/,
  );
  assert.match(appSource, /markReservationBatchManualReview\([\s\S]{0,700}relay payload expired and nullifiers remain unspent/);
  assert.match(appSource, /function markReservationBatchManualReview/);
  assert.match(appSource, /expiredProofReady[\s\S]*markReservationBatchManualReview\([\s\S]*expired_local_relay_payload_discard_requires_manual_review/);
  assert.match(appSource, /async function refreshPendingRelayWithdrawPayloadStatus/);
  assert.match(appSource, /\? "Check recovery"[\s\S]*: "Refresh status"/);
  assert.match(appSource, /async function resolveExpiredRelayManualReview/);
  assert.match(appSource, /resolvedStatus === reservationStatuses\.ReplanRequired[\s\S]*clearPreparedRelayWithdrawPayload/);
  assert.match(appSource, /pending verification/);
  const relaySubmission = sourceBetween(
    appSource,
    "async function relayPreparedWithdraw()",
    "els.connectWallet.addEventListener",
  );
  assert.match(relaySubmission, /const session = beginPrivacySessionOperation\(\)/);
  assert.match(relaySubmission, /const expectedPayloadVersion = state\.keplr\.relayWithdrawPayloadVersion/);
  assert.match(relaySubmission, /const assertRelayIsCurrent = \(\) =>/);
  assert.match(relayPayloadCopy, /catch \(error\) \{[\s\S]*if \(!copyIsCurrent\(\)\) \{[\s\S]*assertCopyIsCurrent\(\);/);
  assert.match(relaySubmission, /catch \(error\) \{[\s\S]*if \(!relayIsCurrent\(\)\) \{[\s\S]*assertRelayIsCurrent\(\);/);
  assert.match(relaySubmission, /if \(error\?\.privacySessionInvalidated \|\| error\?\.relayPayloadChanged\) \{\s*throw error;/);
  assert.match(relaySubmission, /withReservationHeartbeat\(\s*reservation,[\s\S]*\{ session \}/);
  assert.match(appSource, /async function latestRelayChainSnapshot/);
  assert.match(appSource, /evmJsonRpc\([\s\S]*"eth_getBlockByNumber"[\s\S]*\["latest", false\]/);
  assert.match(relaySubmission, /chainSnapshot = await withPrivacySessionGuard\([\s\S]*latestRelayChainSnapshot\(\)/);
  assert.match(relaySubmission, /await reconcileExpiredRelayWithdrawPayloads\(chainSnapshot, \{ session \}\);[\s\S]*assertRelayIsCurrent\(\);[\s\S]*canRelayPreparedWithdrawPayload\(chainSnapshot\.chainNowMs\)/);
  assert.match(relaySubmission, /const latestChainSnapshot = await withPrivacySessionGuard\([\s\S]*latestRelayChainSnapshot\(\)[\s\S]*assertRelayIsCurrent\(\);[\s\S]*canRelayPreparedWithdrawPayload\(latestChainSnapshot\.chainNowMs\)/);
  assert.match(relaySubmission, /resolvedStatus === reservationStatuses\.ReplanRequired[\s\S]*clearPreparedRelayWithdrawPayload\([\s\S]*assertPrivacySessionCurrent\(session\);/);
  assert.match(relaySubmission, /recordSubmittedReservation\([\s\S]*clearPreparedRelayWithdrawPayload\([\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*els\.keplrTxState\.textContent = "Relay withdraw included"/);
  assert.match(relaySubmission, /let relaySubmissionRecorded = false;/);
  assert.match(relaySubmission, /await recordSubmittedReservation\([\s\S]*relaySubmissionRecorded = true;/);
  assert.match(relaySubmission, /if \(error\?\.reservationReconciliationRequired\)[\s\S]*throw error;[\s\S]*if \(relaySubmissionRecorded\) \{[\s\S]*submittedOperationReconciliationCopy\("Relay withdraw", false\)/);
  assert.match(appSource, /relayReservationStatus\([\s\S]*state\.keplr\.relayWithdrawReservation/);
  assert.match(appSource, /Reservation \$\{relayStatus \|\| "not ready"\}/);
  assert.match(appSource, /relayPreparedWithdraw\.disabled =[\s\S]*!relayPayloadReady && !relayNeedsManualResolution[\s\S]*!relayerReady/);
  assert.match(relaySubmission, /verifyRelayPayloadNullifierUnspentBeforeBroadcast\([\s\S]*payload,[\s\S]*reservation,[\s\S]*\{ session \}/);
  assert.doesNotMatch(relaySubmission, /markRelayReservationHandedOff\(/);
  assert.doesNotMatch(relaySubmission, /extendReservationBatchLeaseToPayloadExpiry\(/);
  assert.match(relaySubmission, /verifyRelayPayloadNullifierUnspentBeforeBroadcast\([\s\S]*stopPreparedRelayReservationHeartbeat\(\);[\s\S]*markPreparedReservationBroadcastAttempting\([\s\S]*"relay_withdraw",[\s\S]*\{ session \}[\s\S]*relayDispatchMarked = true;[\s\S]*relayPreparedWithdrawPayload\(/);
  assert.match(relaySubmission, /if \(!relayDispatchMarked && !hasBroadcastAttemptMetadata\(attempt\)\)[\s\S]*Relay validation failed before local dispatch/);
  assert.match(relaySubmission, /relayPreparedWithdrawPayload\(payload, recipient\)/);
  assert.match(appSource, /await setPreparedRelayWithdrawPayload\(data, \{[\s\S]*amount,[\s\S]*recipient,[\s\S]*preparation,[\s\S]*\}\)/);
  assert.match(relaySubmission, /await recordSubmittedReservation\([\s\S]*reservation,[\s\S]*\{ session \}/);
  assert.match(relaySubmission, /reconcileFailedEvmReservation\(preparedData, error, attemptSource, reservationStatuses\.ProofReady, \{ session \}\)/);
  assert.match(appSource, /const definiteEvmFailure = isDefiniteEvmReceiptFailure\(error\)/);
  assert.match(appSource, /const definiteCosmosFailure = isDefiniteCosmosTxFailure/);
  assert.match(appSource, /candidate\?\.txCode/);
  assert.match(appSource, /if \(definiteEvmFailure \|\| definiteCosmosFailure\)/);
  assert.match(relaySubmission, /markReservationBatchUnknown\([\s\S]*reservation,[\s\S]*error,[\s\S]*\{ session \}/);
  assert.match(relaySubmission, /\.then\(\(records\) => updateRelayWithdrawReservationRecords\(records, \{ session \}\)\)/);
  assert.match(appSource, /copyRelayWithdrawPayload\(\)\.catch\(\(error\) => \{[\s\S]*!error\?\.privacySessionInvalidated && !error\?\.relayPayloadChanged/);
  assert.match(appSource, /relayPreparedWithdraw\(\)\.catch\(\(error\) => \{[\s\S]*!error\?\.privacySessionInvalidated && !error\?\.relayPayloadChanged/);
  assert.match(serverSource, /const txHash = result\.json\.txhash;[\s\S]*relay withdraw tx was broadcast but not found yet:[\s\S]*error\.txHash = txHash/);
  assert.match(serverSource, /const checkTxCode = confirmedCosmosTxCode\(result\.json\);[\s\S]*checkTxCode !== 0[\s\S]*error\.txCode = checkTxCode;[\s\S]*const tx = await waitForTx\(txHash\)/);
  assert.match(serverSource, /function confirmedCosmosTxCode/);
  assert.match(serverSource, /typeof error\?\.txCode === "number" && Number\.isSafeInteger\(error\.txCode\) && error\.txCode >= 0/);
  assert.match(serverSource, /relay withdraw tx did not include a valid result code/);
  assert.match(serverSource, /hasSuccessfulEvmReceiptStatus/);
  assert.match(serverSource, /async function assertRelayPayloadNotExpired/);
  const relayExpiryGuard = sourceBetween(
    serverSource,
    "async function assertRelayPayloadNotExpired",
    "function serverFeaturesForRequest",
  );
  assert.match(relayExpiryGuard, /chainNowUnix >= expiresAtUnix/);
  assert.match(serverSource, /await assertRelayPayloadNotExpired\(payload, provider\);[\s\S]*relaySubmissionCoordinator\.run\([\s\S]*relayPayloadNullifierLockKey\(payload\)[\s\S]*submitRelayAfterNullifierPreflight\(\{[\s\S]*wallet\.sendTransaction/);
  assert.match(serverSource, /await assertRelayPayloadNotExpired\(payload\);[\s\S]*relaySubmissionCoordinator\.run\([\s\S]*relayPayloadNullifierLockKey\(payload\)[\s\S]*submitRelayAfterNullifierPreflight\(\{[\s\S]*runClairveild/);
  assert.match(serverSource, /checkNullifiers: \(nullifiers\) => clairveil\.checkNullifiers\(nullifiers\)/);
  assert.match(serverSource, /relaySubmissionCoordinator\.run\([\s\S]*relayPayloadNullifierLockKey\(payload\),[\s\S]*relaySubmissionIdempotencyKey\(payload\)/);
  assert.match(appSource, /clearPreparedRelayWithdrawPayload\(\{[\s\S]*clearPayloadHash: true,[\s\S]*stashHandedOff: false,[\s\S]*\}\)/);
  assert.doesNotMatch(appSource, /state\.keplr\.relayWithdrawPayloadSubmitted = true/);
  assert.doesNotMatch(appSource, /const relay = await relayPreparedWithdrawPayload\(data\.payload, recipient\)/);
  assert.match(appSource, /function localRelayerAccount/);
  assert.match(appSource, /const relayer =[\s\S]*localRelayerAccount\(\)\?\.name[\s\S]*isEvmTransparentMode\(\) \? "dev0" : "relayer"/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.scanWalletNotes/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.checkNullifier/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeUserDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.broadcastTxRawBytes/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.waitForEvmTransaction/);
  assert.match(appSource, /function defaultNoteScanCursor/);
  assert.match(appSource, /function privacyScanCursorPosition/);
  assert.match(appSource, /function noteScanRequestOptions/);
  assert.match(appSource, /function applyNoteScanResult/);
  const noteScanRequestOptions = sourceBetween(
    appSource,
    "function noteScanRequestOptions",
    "function applyNoteScanResult",
  );
  assert.match(noteScanRequestOptions, /scanSource: "privacy_scan"/);
  assert.doesNotMatch(noteScanRequestOptions, /eventTypes:/);
  assert.doesNotMatch(noteScanRequestOptions, /afterHeight:/);
  assert.match(appSource, /global_sequence/);
  assert.match(appSource, /output_index/);
  assert.match(appSource, /nextCursor/);
  assert.match(appSource, /latestSequence/);
  assert.match(appSource, /data\?\.nextScanOptions/);
  assert.match(appSource, /function refreshCachedNoteStatuses/);
  assert.match(appSource, /const candidateNotes = \(state\.keplr\.notes \|\| \[\]\)\.filter\(noteNullifier\)/);
  assert.match(appSource, /function nullifierUsedFromResponse/);
  assert.match(appSource, /return typeof used === "boolean" \? used : null/);
  assert.match(appSource, /function isUnverifiedNote/);
  assert.match(appSource, /!isZeroAmountNote\(note\) &&[\s\S]*!isUnverifiedNote\(note\) &&[\s\S]*noteUsesCurrentAsset\(note\)/);
  assert.match(appSource, /const unverifiedNotes = visibleNotes\.filter\([\s\S]*isUnverifiedNote\(note\)/);
  assert.match(appSource, /:\s*\[\.\.\.valueNotes, \.\.\.unverifiedNotes, \.\.\.otherAssetNotes\]/);
  assert.match(appSource, /return "Unverified"/);
  assert.match(appSource, /status: "spent"/);
  assert.match(appSource, /await refreshCachedNoteStatuses\(\{ session \}\)/);
  assert.match(appSource, /scanWalletNotes\([\s\S]*privacyRequest\(\{[\s\S]*\.\.\.scanOptions,[\s\S]*includeFoundNotes: true/);
  assert.match(appSource, /function notesSummarySuffix/);
  assert.match(
    appSource,
    /scan: \{ limit: 200, maxPages: 1000, scanSource: "privacy_scan" \}/,
  );
  assert.match(appSource, /function browserProverUrl/);
  assert.match(appSource, /serverBacked === true && serverFeatures\?\.proverProxy === true/);
  assert.match(appSource, /return window\.location\.origin\.replace/);
  assert.match(serverSource, /function handleProverProxy/);
  assert.match(serverSource, /function proverProxyPath/);
  assert.match(serverSource, /proverProxyPath\(url\.pathname\)/);
  assert.match(serverSource, /new URL\(path, config\.proverUrl\.replace/);
  assert.match(serverSource, /const proverProxyResponseMaxBytes = 1 << 20/);
  assert.match(serverSource, /readBoundedResponseText\(\s*response,\s*proverProxyResponseMaxBytes/);
  assert.match(appSource, /refreshEvents\(\{ allowFailure: true, healthView \}\)/);
  assert.match(appSource, /Browser cannot reach the selected chain REST\/RPC endpoint/);
  assert.match(appSource, /state\.privacyEvents\.loadError/);
  assert.doesNotMatch(appSource, /\/api\/tx\//);
  assert.doesNotMatch(appSource, /\/api\/keplr\/privacy/);
  assert.doesNotMatch(appSource, /\/api\/evm\/privacy/);
  assert.match(appSource, /\/api\/relayer\/withdraw/);
  assert.match(appSource, /Relay payload preparation and copy are browser-owned handoff actions/);
  assert.doesNotMatch(appSource, /\/sdk\/clairveiljs/);
  assert.doesNotMatch(appSource, /buildPreparedTransferPayload/);
  assert.doesNotMatch(appSource, /buildPreparedWithdrawProverPayload/);
  assert.doesNotMatch(appSource, /planTransferNotes/);
  assert.doesNotMatch(appSource, /planWithdrawNotes/);
  assert.doesNotMatch(appSource, /createHttpProverAdapter/);
  assert.doesNotMatch(serverSource, /function serveClairveiljsStatic/);
  assert.doesNotMatch(serverSource, /url\.pathname === "\/api\/events"/);
  assert.match(
    serverSource,
    /url\.pathname === "\/api\/auditor\/transfers"[\s\S]*assertLocalAdminAccessAllowed\(req\)[\s\S]*fetchAuditorTransferEvents\(\{[\s\S]*page: paginationInteger/,
  );
  assert.doesNotMatch(serverSource, /createHttpProverAdapter/);
  assert.doesNotMatch(serverSource, /buildTransferMessage/);
  assert.doesNotMatch(serverSource, /buildWithdrawMessage/);
  assert.doesNotMatch(serverSource, /planTransferNotes/);
  assert.doesNotMatch(serverSource, /planWithdrawNotes/);
  assert.doesNotMatch(serverSource, /prepareEvmTransfer/);
  assert.doesNotMatch(serverSource, /prepareEvmWithdraw/);
});

test("successful privacy deposits bypass input-note reservation bookkeeping", () => {
  const depositBroadcast = sourceBetween(
    appSource,
    "async function broadcastPrivacyDeposit",
    "function broadcastTxEvents",
  );
  const submittedReservation = sourceBetween(
    appSource,
    "async function recordSubmittedReservation",
    "async function markReservationBatchUnknown",
  );
  assert.match(depositBroadcast, /await broadcastPreparedPrivacy\(data, label, options\)/);
  assert.match(depositBroadcast, /preparedDeposit: data/);
  const depositFlow = sourceBetween(
    appSource,
    "async function depositFromKeplr",
    "async function resetAndRescanKeplrNotes",
  );
  assert.match(appSource, /async function confirmCosmosDepositBeforeRecovery/);
  assert.match(
    depositFlow,
    /await confirmCosmosDepositBeforeRecovery\(broadcast, \{ session \}\);[\s\S]*refreshDepositNoteRecovery/,
  );
  assert.match(appSource, /expectedCommitment,[\s\S]*expectedEncryptedNote/);
  assert.match(submittedReservation, /if \(!reservation\?\.reservation_ids\?\.length\) return \[\];/);
});

test("DApp default check does not require a clean generated bundle diff", () => {
  assert.match(packageJson.scripts["check:dapp"], /npm run build:dapp/);
  assert.doesNotMatch(packageJson.scripts["check:dapp"], /check:bundle:fresh/);
  assert.equal(packageJson.scripts["check:bundle"], "node --check public/app.bundle.js");
  assert.equal(packageJson.scripts["check:bundle:fresh"], "node tools/check-bundle-fresh.js");
  assert.equal(
    packageJson.scripts["verify:production-deployment"],
    "node tools/verify-production-deployment.mjs",
  );
  assert.match(packageJson.scripts["check:dapp"], /verify-production-deployment\.mjs/);
  assert.match(deploymentGateSource, /CLAIRVEIL_WEBAPP_ORIGIN/);
  assert.match(deploymentGateSource, /CLAIRVEIL_WEBAPP_CONFIG_URL/);
  assert.match(deploymentGateSource, /assertRestrictiveConnectSrc/);
  assert.match(deploymentGateSource, /restEndpoints/);
  assert.match(deploymentGateSource, /clairveil-cors-probe\.invalid/);
  assert.match(deploymentGateSource, /content-security-policy/);
  assert.match(deploymentGateSource, /access-control-allow-origin/);
  assert.match(deploymentGateSource, /access-control-allow-headers/);
  assert.match(deploymentGateSource, /Keplr\/MetaMask wallet-extension flow/);
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

test("DApp verifies durable reservation records for idempotent status writes", () => {
  const submitted = sourceBetween(
    appSource,
    "async function markReservationBatchSubmitted",
    "async function markPreparedReservationSubmitted",
  );
  const unknown = sourceBetween(
    appSource,
    "async function markReservationBatchUnknown",
    "async function markPreparedReservationUnknown",
  );
  const idempotentRecords = sourceBetween(
    appSource,
    "async function idempotentReservationBatchRecords",
    "async function markReservationBatchSubmitted",
  );

  assert.match(idempotentRecords, /assertPrivacySessionCurrent\(session\);/);
  assert.match(idempotentRecords, /withPrivacySessionGuard\([\s\S]*manager\.getReservation/);
  assert.match(submitted, /const records = await idempotentReservationBatchRecords\([\s\S]*"Submitted",[\s\S]*\{ session \}/);
  assert.match(unknown, /const records = await idempotentReservationBatchRecords\([\s\S]*"Unknown",[\s\S]*\{ session \}/);
  assert.doesNotMatch(submitted, /expected ProofReady, got Submitted/);
  assert.doesNotMatch(unknown, /expected \(ProofReady\|Submitted\)/);
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
  assert.match(appSource, /onFinalExactTransfer: \(data\) =>/);
  assert.doesNotMatch(appSource, /currentMax: zeroCoinText\(\)/);
  assert.doesNotMatch(appSource, /currentMax: error\.prepared/);
  assert.doesNotMatch(appSource, /currentMax: amount/);
});

test("DApp exposes none, public, and recipient-encrypted disclosure modes", () => {
  assert.match(htmlSource, /id="veiledDisclosureSummary"/);
  assert.match(htmlSource, /Audit disclosure is always encrypted for the configured auditor\./);
  assert.match(htmlSource, /Sender self-view disclosure is included by default/);
  assert.match(htmlSource, /Advanced settings control only optional\s+user disclosure\./);
  assert.match(htmlSource, /id="veiledDisclosureMode"/);
  assert.match(htmlSource, /value="none"/);
  assert.match(htmlSource, /value="public"/);
  assert.match(htmlSource, /value="recipient-encrypted"/);
  assert.match(appSource, /disclosureMode === "none"/);
  assert.match(appSource, /disclosureMode === "public"/);
  assert.match(appSource, /disclosureMode: "recipient-encrypted"/);
  assert.match(appSource, /disclosurePubKeyHex/);
});

test("DApp confirms the prepared transfer effect before wallet signing", () => {
  const transferAction = sourceBetween(
    appSource,
    "async function transferFromVeiled",
    "async function withdrawFromVeiled",
  );
  const confirmation = sourceBetween(
    appSource,
    "async function confirmPreparedTransferBeforeBroadcast",
    "function finishTransferFlow",
  );
  for (const id of [
    "transferConfirmationChainId",
    "transferConfirmationExpiry",
    "transferConfirmationRecipient",
    "transferConfirmationChange",
    "transferConfirmationDisclosure",
  ]) {
    assert.match(htmlSource, new RegExp(`id="${id}"`));
  }
  assert.match(appSource, /function preparedTransferConfirmationFacts/);
  assert.match(appSource, /payload\.chain_id/);
  assert.match(appSource, /payload\.expires_at_unix/);
  assert.match(appSource, /owner-intent signature/);
  assert.match(appSource, /Prepared transfer must contain recipient and change outputs/);
  assert.match(appSource, /function preparedTransferDisclosurePlanes/);
  assert.match(appSource, /Prepared transfer is missing mandatory audit disclosure/);
  assert.match(confirmation, /requestPreparedTransferConfirmation\(facts\)/);
  assert.match(confirmation, /prepared_transfer_confirmation_cancelled/);
  assert.match(confirmation, /replanInvalidatedPreparedReservation\([\s\S]*privacySessionInvalidatedError\(\)/);
  assert.match(
    transferAction,
    /const finalConfirmed = await confirmPreparedTransferBeforeBroadcast\([\s\S]*finalData,[\s\S]*\{ session \}/,
  );
  assert.match(transferAction, /if \(!isPrivacySessionCurrent\(session\) \|\| !finalConfirmed\)/);
});

test("DApp obtains explicit approval for each planner-required self-transfer", () => {
  const transferAction = sourceBetween(
    appSource,
    "async function transferFromVeiled",
    "async function withdrawFromVeiled",
  );
  const confirmation = sourceBetween(
    appSource,
    "function requestPreparedSelfTransferConfirmation",
    "function finishTransferFlow",
  );
  assert.match(appSource, /function requestPreparedSelfTransferConfirmation/);
  assert.match(confirmation, /showPreparedTransferConfirmationFacts\(facts\)/);
  assert.match(confirmation, /Self transaction 승인/);
  assert.match(confirmation, /setTransferFlowStep\("zero", "Self transaction 확인 필요"\)/);
  assert.doesNotMatch(confirmation, /setTransferFlowStep\("transfer", "Self transaction 확인 필요"\)/);
  assert.match(
    transferAction,
    /const selfMergeConfirmed = await confirmPreparedTransferBeforeBroadcast\([\s\S]*data,[\s\S]*\{ session, selfTransfer: true \}/,
  );
  assert.match(
    transferAction,
    /if \(!isPrivacySessionCurrent\(session\) \|\| !selfMergeConfirmed\) \{\s*return;/,
  );
  assert.match(confirmation, /prepared_self_transfer_confirmation_cancelled/);
  assert.ok(
    transferAction.indexOf("selfMergeConfirmed") <
      transferAction.indexOf("const plannerBroadcast = await broadcastPreparedPrivacy"),
  );
  assert.match(
    transferAction,
    /catch \(error\) \{[\s\S]*error\?\.reservationReconciliationRequired[\s\S]*reconcileSubmittedSelfTransferForContinuation\([\s\S]*if \(!reconciled\) throw error;[\s\S]*continue;/,
  );
  assert.match(
    appSource,
    /async function reconcileSubmittedSelfTransferForContinuation[\s\S]*broadcastTxHash\(error\)[\s\S]*refreshSubmittedOperationReconciliation\(data, \{[\s\S]*session,[\s\S]*\}\)/,
  );
});

test("DApp does not renew a lease after the Cosmos SDK has submitted it", () => {
  const heartbeat = sourceBetween(
    appSource,
    "async function withReservationHeartbeat",
    "async function withPreparedReservationHeartbeat",
  );
  assert.match(
    heartbeat,
    /const result = await task\(\{ assertHeartbeatHealthy, heartbeatNow \}\);[\s\S]*if \(!result\?\.sdkReservationLifecycleManaged\) \{[\s\S]*await heartbeatNow\(\);/,
  );
  assert.match(
    heartbeat,
    /Submitted is[\s\S]*not lease-renewable/,
  );
});

test("DApp discloses the EVM self-view transport limitation", () => {
  assert.match(appSource, /function renderTransferDisclosureSummary/);
  assert.match(appSource, /supported EVM transfer ABI does not carry sender self-view disclosure/);
  assert.match(appSource, /later sent-transfer recovery depends on this wallet's local cache/);
  assert.match(appSource, /renderTransferDisclosureSummary\(\);/);
});

test("DApp renders public disclosure reports without recipient-only branching", () => {
  assert.match(appSource, /renderEventDisclosureReport/);
  assert.match(appSource, /summary\.delivery/);
  assert.match(appSource, /function isPublicDisclosureEvent/);
  assert.match(appSource, /function hasSelfViewDisclosureEvent/);
  assert.match(appSource, /function canDecodeEventDisclosure/);
  assert.match(appSource, /function canDecodeSelfViewDisclosure/);
  assert.match(appSource, /if \(isPublicDisclosureEvent\(event\)\) return true/);
  assert.match(appSource, /decodeSelectedEventDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeUserDisclosure/);
  assert.match(appSource, /clairveilBrowserClient\(\)\.decodeSelfViewDisclosure/);
  assert.match(htmlSource, /id="eventDisclosurePlane"/);
  assert.match(htmlSource, /id="eventDisclosurePolicy"/);
  assert.match(htmlSource, /id="eventDisclosureOutputIndex"/);
  assert.match(htmlSource, /id="eventDisclosureCommitment"/);
  assert.match(htmlSource, /id="eventDisclosureDigest"/);
  assert.match(htmlSource, /id="eventDisclosureVerified"/);
  assert.match(htmlSource, /id="eventDisclosureAssetDenom"/);
  assert.match(appSource, /els\.eventDisclosurePolicy\.textContent/);
  assert.match(appSource, /els\.eventDisclosureOutputIndex\.textContent/);
  assert.match(appSource, /els\.eventDisclosureCommitment\.textContent/);
  assert.match(appSource, /els\.eventDisclosureDigest\.textContent/);
  assert.match(appSource, /els\.eventDisclosureVerified\.textContent/);
  assert.doesNotMatch(appSource, /\/api\/keplr\/privacy\/disclosure\/decode/);
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
  assert.match(serverSource, /function browserRequestIsSameOrigin/);
  assert.match(serverSource, /fetchSite !== "same-origin" && fetchSite !== "none"/);
  assert.match(serverSource, /const inboundRequestTimeoutMs = 30_000/);
  assert.match(serverSource, /const apiRequestBodyMaxBytes = 64 \* 1024/);
  assert.match(serverSource, /const proverProxyRequestMaxBytes = 4 \* 1024 \* 1024/);
  assert.match(serverSource, /await readRawBody\(req, \{ maxBytes: apiRequestBodyMaxBytes \}\)/);
  assert.match(serverSource, /req\.resume\(\);/);
  assert.match(serverSource, /fail\(httpError\(413, "request body too large", "invalid_request"\)\)/);
  assert.match(serverSource, /server\.headersTimeout = inboundRequestTimeoutMs/);
  assert.match(serverSource, /server\.requestTimeout = inboundRequestTimeoutMs/);
  assert.doesNotMatch(serverSource, /"access-control-allow-origin": "\*"/);
  assert.match(serverSource, /\/api\/local-signers\/ensure"\) \{\s*assertLocalTestBackendAllowed\("local signer setup"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/faucet"\) \{\s*assertLocalTestBackendAllowed\("faucet"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /\/api\/deposit\/proof"\) \{\s*assertLocalTestBackendAllowed\("deposit proof"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /go", \["run", "\.\/examples\/clairveil-dapp\/tools\/deposit-proof"\]/);
  assert.match(serverSource, /\/api\/relayer\/withdraw"\) \{\s*assertLocalTestBackendAllowed\("relay withdraw"\);\s*assertSignerMutationAllowed\(req\);/);
  assert.match(serverSource, /body\.relayer \?\? body\.from \?\? localRelayerName\(\)/);
  assert.match(serverSource, /buildRelayWithdrawMessageFromPayload/);
  assert.match(serverSource, /createClairveilEvmClient/);
  assert.match(serverSource, /evmClient\.buildWithdrawTransaction/);
  assert.match(serverSource, /const chainNowUnix = await relayChainNowUnix\(provider\);/);
  assert.match(serverSource, /relayer: account\.transparentAddress,[\s\S]*chainNowUnix,[\s\S]*expectedChainId/);
  assert.match(serverSource, /await assertRelayPayloadNotExpired\(payload, provider\);/);
  assert.match(
    serverSource,
    /const chainNowUnix = await relayChainNowUnix\(\);[\s\S]*chainNowUnix >= relayPayloadExpiryUnix\(payload\)/,
  );
  assert.match(serverSource, /"tx", "privacy", "relay-withdraw"/);
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

test("DApp invalidates asynchronous relay preparation and pre-broadcast failures", () => {
  const relayPayloadInstall = sourceBetween(
    appSource,
    "async function setPreparedRelayWithdrawPayload",
    "async function discardPreparedRelayWithdrawPayload",
  );
  assert.match(appSource, /let relayWithdrawPayloadGeneration = 0/);
  assert.match(appSource, /function advanceRelayWithdrawPayloadGeneration/);
  assert.match(appSource, /function beginRelayWithdrawPreparation/);
  assert.match(appSource, /function relayWithdrawPreparationIsCurrent/);
  assert.match(appSource, /async function rejectStaleRelayWithdrawPreparation\([\s\S]*\{ session = beginPrivacySessionOperation\(\) \} = \{\}/);
  assert.match(relayPayloadInstall, /\{[\s\S]*session = beginPrivacySessionOperation\(\),?\s*\} = \{\}/);
  assert.match(relayPayloadInstall, /discardPreparedRelayWithdrawPayload\([\s\S]*\{ session \}/);
  assert.match(relayPayloadInstall, /renewReservationBatchLease\([\s\S]*\{ session \}/);
  assert.match(relayPayloadInstall, /startPreparedRelayReservationHeartbeat\(\{ session \}\)/);
  assert.match(relayPayloadInstall, /refreshNoteReservationState\(\{ session \}\)/);
  assert.match(relayPayloadInstall, /await persistRelayWithdrawPayloadState\(\{ session \}\);\s*assertPrivacySessionCurrent\(session\);/);
  assert.match(appSource, /await rejectStaleRelayWithdrawPreparation\(data, \{ session \}\)/);
  assert.match(appSource, /const preparation = beginRelayWithdrawPreparation\(\)/);
  assert.match(appSource, /setPreparedRelayWithdrawPayload\(data, \{[\s\S]*preparation,[\s\S]*session,/);
  assert.match(appSource, /let externalBroadcastBoundaryCrossed = false/);
  assert.match(appSource, /durableBroadcastAttemptRecorded = true;\s*assertHeartbeatHealthy\(\);/);
  assert.match(appSource, /const onBroadcastStart = \(\{ externalBoundaryStarted = false \} = \{\}\) => \{[\s\S]*if \(externalBoundaryStarted\) \{[\s\S]*externalBroadcastBoundaryCrossed = true[\s\S]*assertHeartbeatHealthy\(\);[\s\S]*assertPrivacySessionCurrent\(session\);[\s\S]*externalBroadcastBoundaryCrossed = true/);
  assert.match(
    appSource,
    /if \(!externalBroadcastBoundaryCrossed\) \{\s*await replanInvalidatedPreparedReservation\(data, session, error\);/,
  );
  assert.match(appSource, /markPreparedReservationBroadcastRejected\(data, error, \{ session \}\)/);
  assert.match(appSource, /refreshNoteReservationState\(\{ session \}\);\s*throw error;/);
});

test("DApp shows a send result confirmation before refresh side effects", () => {
  assert.match(appSource, /function showSendResult/);
  assert.match(appSource, /title: "Send 요청됨"/);
  assert.match(appSource, /title: "Send 실패"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "MetaMask"/);
  assert.match(appSource, /showSendResult\(\{[\s\S]*success: true,[\s\S]*wallet: "Keplr"/);
  assert.match(appSource, /els\.keplrTxState\.textContent = "Send submitted"/);
  assert.match(appSource, /watchEvmBroadcast\(broadcast/);
  assert.match(appSource, /Promise\.allSettled\(\[\s*refreshWalletBalance\(\{ session \}\),\s*refreshBlockEvents\(\{ session \}\),\s*\]\)/);
  assert.doesNotMatch(appSource, /toast\("MetaMask send included"\)/);
  assert.doesNotMatch(appSource, /toast\("Keplr send included"\)/);
  assert.match(cssSource, /#noticeMessage\s*\{[\s\S]*white-space: pre-wrap/);
});

test("DApp submits final MetaMask transactions before waiting for receipt", () => {
  const submitEvm = sourceBetween(
    appSource,
    "async function submitEvmTransaction",
    "async function waitForEvmTransaction",
  );
  assert.match(appSource, /async function submitEvmTransaction/);
  assert.match(appSource, /function isMetaMaskUserRejectedError/);
  assert.match(appSource, /code === "4001"/);
  assert.match(appSource, /MetaMask eth_sendTransaction did not return a tx hash/);
  assert.match(submitEvm, /const provider = metaMaskProvider\(\);/);
  assert.match(submitEvm, /walletRequest = provider\.request\(\{[\s\S]*method: "eth_sendTransaction",[\s\S]*\}\);[\s\S]*onBroadcastStart\?\.\(\{ externalBoundaryStarted: true \}\);[\s\S]*\(\) => walletRequest/);
  assert.match(submitEvm, /if \(isMetaMaskUserRejectedError\(error\)\) \{[\s\S]*throw noBroadcastAttemptError\(error\);[\s\S]*\}[\s\S]*throw error/);
  assert.match(appSource, /async function waitForEvmTransaction/);
  assert.match(appSource, /async function sendEvmTransaction\([\s\S]*transaction,[\s\S]*\{\s*waitForReceipt = false/);
  const evmReceiptWait = sourceBetween(
    appSource,
    "async function waitForEvmTransaction",
    "async function sendEvmTransaction",
  );
  assert.match(
    evmReceiptWait,
    /\{ session = beginPrivacySessionOperation\(\) \} = \{\}/,
  );
  assert.match(
    evmReceiptWait,
    /withPrivacySessionGuard\([\s\S]*clairveilBrowserClient\(\)\.waitForEvmTransaction/,
  );
  const evmSubmission = sourceBetween(
    appSource,
    "async function sendEvmTransaction",
    "function watchEvmBroadcast",
  );
  assert.match(
    evmSubmission,
    /waitForEvmTransaction\(txHash, label, \{ session \}\)/,
  );
  assert.match(appSource, /pending: true/);
  assert.match(appSource, /waitPromise/);
  assert.match(appSource, /broadcast\?\.pending && txHash/);
  assert.match(appSource, /Deposit submitted/);
  assert.match(appSource, /Transfer submitted/);
  assert.match(appSource, /트랜스퍼 요청이 제출되었습니다/);
  assert.match(appSource, /Withdraw submitted/);
  assert.match(appSource, /Withdraw 요청이 제출되었습니다/);
  assert.match(appSource, /zero helper note",[\s\S]*\{ waitForEvmReceipt: true \}/);
  assert.match(appSource, /self transaction",[\s\S]*\{ waitForEvmReceipt: true \}/);
});

test("DApp forces MetaMask onto the configured EVM chain", () => {
  assert.match(serverSource, /evmChainId: normalizeEvmChainId/);
  assert.match(serverSource, /evmChainName:/);
  assert.match(appSource, /evmChainId: resolved\?\.evmChainId \|\| state\.config\?\.evmChainId/);
  assert.match(appSource, /function ensureMetaMaskChain/);
  assert.match(appSource, /wallet_switchEthereumChain/);
  assert.match(appSource, /wallet_addEthereumChain/);
  assert.match(
    appSource,
    /chainName:\s*activeChainProfile\(\)\?\.evmChainName\s*\|\|\s*state\.config\?\.evmChainName/,
  );
  assert.match(appSource, /await ensureMetaMaskChain\(\{ session \}\);[\s\S]*eth_requestAccounts/);
  assert.match(appSource, /await ensureMetaMaskChain\(\{ session \}\);[\s\S]*method: "eth_sendTransaction"/);
});

test("DApp estimates EVM gas through the configured RPC before opening MetaMask confirmation", () => {
  assert.match(appSource, /function withEstimatedEvmGas/);
  assert.match(appSource, /evmJsonRpc\("eth_estimateGas", \[tx\]\)/);
  assert.match(appSource, /const padded = \(estimated \* 13n \+ 9n\) \/ 10n/);
  assert.match(appSource, /tx\.gas = bigIntToEvmQuantity\(existing > padded \? existing : padded\)/);
  assert.doesNotMatch(appSource, /existing > 0n && existing < padded/);
  assert.match(appSource, /delete tx\.gas/);
  assert.match(appSource, /let tx;[\s\S]*tx = await withPrivacySessionGuard\([\s\S]*withEstimatedEvmGas\(\{[\s\S]*\.\.\.transaction,[\s\S]*from: account[\s\S]*\}\)[\s\S]*noBroadcastAttemptError\(error, "MetaMask gas estimation failed before broadcast"\)/);
  assert.match(appSource, /evmJsonRpc\("eth_estimateGas", \[tx\]\)/);
});

test("DApp resets MetaMask privacy identity after account changes", () => {
  const block = sourceBetween(appSource, 'injectedMetaMask.on?.("accountsChanged"', 'injectedMetaMask.on?.("chainChanged"');
  assert.match(block, /resetWalletSession\(\);/);
  assert.match(block, /renderWallet\(\);/);
  assert.match(block, /renderKeplr\(\);/);
  assert.match(block, /Reconnect wallet to refresh privacy identity/);
  assert.doesNotMatch(block, /state\.wallet\.account = accounts\[0\]/);
});
