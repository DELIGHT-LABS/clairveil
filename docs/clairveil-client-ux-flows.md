# Clairveil Client UX Flows

This document describes user flows and failure/recovery UX that Clairveil clients should support.

Korean version: [clairveil-client-ux-flows-kr.md](clairveil-client-ux-flows-kr.md)

This is not a screen wireframe. Each downstream wallet/app team should translate these flows into its own screens, states, and copy.

## 1. First Setup

1. The client loads downstream chain config.
2. The user connects a transparent account.
3. The client requests a root signing message signature.
4. The client derives shielded identity and disclosure keys.
5. The client syncs latest tree state and privacy events.
6. The client displays shielded address and spendable balance.

Required states:

- before account connection
- waiting for root signing
- identity derivation complete
- initial note sync in progress
- sync complete
- sync failed

## 2. Note Sync

1. Fetch deposit/transfer events with the `PrivacyEvents` query.
2. Find notes decryptable with the viewing key.
3. Check commitment and Merkle path information.
4. Check spent status with the nullifier query.
5. Update the local note cache.

Required failure states:

- endpoint unavailable
- event pagination interrupted
- encrypted note decode failure
- missing Merkle path
- missing historical root
- local cache corruption

Recovery UX:

- retry
- change endpoint
- roll back scan cursor
- backup local cache then reset/rescan
- recheck spent status

## 3. Deposit

1. The user enters deposit amount and denom.
2. The client checks transparent balance and gas fee.
3. The user approves/signs the transaction.
4. The client broadcasts the deposit transaction.
5. The client scans the deposit event and recovers the encrypted note.
6. The client updates shielded balance.

Required UX:

- Represent the state where the transaction succeeded but note scanning has not completed yet.
- Distinguish deposit transaction failure from note recovery failure.
- Do not show balance as final if the local note cache is stale.

## 4. Shielded Transfer

1. Enter the recipient shielded address.
2. Enter amount and denom.
3. Choose user disclosure policy.
4. Choose disclosure mode.
5. Select spendable notes and decide whether dummy input is needed.
6. Build the prepared transfer payload.
7. Generate the proof through the prover.
8. Broadcast `MsgTransfer`.
9. Update recipient/change note state through event scanning.
10. Verify disclosure reports.

Required UX:

- recipient `clairs1...` validation
- explanation of disclosed fields by privacy policy
- explanation that audit disclosure is always included
- prover progress or waiting state
- prover timeout/cancel/retry
- final confirmation before transaction broadcast

When showing disclosure plaintext, the client must also show digest verification result.

## 5. Withdraw

1. Enter transparent recipient and amount.
2. The client finds an exact-match note.
3. If no exact-match note exists, guide the user to create one with a self-transfer or prepare it through a separate planner flow.
4. Generate the proof.
5. Execute direct or relayed withdraw.
6. Show nullifier spent status and transparent receive status.

Important constraints:

- Withdraw does not create an output note or change note.
- `MsgWithdraw` has no output note fields.
- The withdraw proof/message itself does not split larger notes, merge fragmented notes, or create change notes.
- The client may provide a separate self-transfer/planner flow before withdraw.

Required failure states:

- no exact-match note
- invalid recipient address
- payload expired
- missing historical root
- nullifier already spent
- proof verification failure

## 6. Relayed Withdraw

1. The user prepares a withdraw payload.
2. The client displays `chain_id`, `recipient`, `expires_at_unix`, and `payload_hash`.
3. The user or relayer receives the payload.
4. The relayer pays the fee and broadcasts.
5. The client checks transaction result and spent status.

Required UX:

- Explain that the relayer does not need the user's shielded secret.
- Show payload expiry.
- Explain that the recipient cannot be changed after preparation.
- Warn that prepared payload/proof JSON is privacy-sensitive data.

## 7. Disclosure Review

1. Enter tx hash or payload.
2. The client finds the disclosure event.
3. Select or auto-detect the disclosure plane.
4. Decrypt with the disclosure private key when needed.
5. Recompute the digest and compare it with the on-chain digest.
6. Show verification result and disclosed fields.

Display policy:

- `verified=true`: disclosed fields can be shown as factual.
- `verified=false`: plaintext must not be shown as factual.
- decrypt failure: distinguish key mismatch, wrong plane, and malformed payload.

## 8. Failure And Recovery Matrix

| Failure | Meaning To User | Recovery Direction |
| --- | --- | --- |
| note scan failure | shielded balance may be stale | retry, change endpoint, rescan |
| local cache corruption | local note DB cannot be trusted | backup then reset/rescan |
| prover timeout | proof generation did not finish | retry, switch local/remote prover |
| payload expired | relayed payload is no longer valid | create a fresh payload |
| disclosure verification failure | payload cannot be trusted as factual | show verified=false, hide plaintext or warn |
| no exact-match withdraw note | no spendable same-amount note exists | create matching note with shielded self-transfer or planner flow |
| missing historical root | proof input does not match current chain state | sync/rescan wallet then retry |
| nullifier already spent | note was already used | update cache, prevent duplicate broadcast |
| missing/mismatched audit config | transfer does not satisfy audit disclosure requirements | check chain config then retry |

## 9. Related Documents

- [Client product brief](clairveil-client-product-brief.md)
- [Client risk decisions](clairveil-client-risk-decisions.md)
- [Client API checklist](clairveil-client-api-checklist.md)
- [CLI reference](clairveil-cli-reference.md)
