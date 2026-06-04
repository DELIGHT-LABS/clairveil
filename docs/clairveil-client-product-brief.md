# Clairveil Client Product Brief

This document summarizes the product scope for wallet, SDK, and app client teams building on a downstream chain that uses Clairveil.

Korean version: [clairveil-client-product-brief-kr.md](clairveil-client-product-brief-kr.md)

This document summarizes the Clairveil client capability scope that mobile wallets, web wallets, desktop wallets, and custodial/backend wallets should commonly understand.

## 1. Client Profile

| Profile | Main Use Case | Decisions To Make |
| --- | --- | --- |
| Mobile wallet | iOS/Android self-custody wallet | secure storage, background scan, whether to use a remote prover, memory/battery constraints |
| Web wallet | browser wallet or dapp embedded wallet | browser storage, WASM/local/remote proving, session security, CORS/auth |
| Desktop wallet | desktop self-custody wallet | local prover daemon, artifact install, local file encryption |
| Custodial/backend wallet | exchange, admin wallet, regulated backend | key custody, audit log, role separation, remote prover trust boundary |

Profiles differ, but the following functional contracts are shared.

- Derive shielded identity from a transparent account.
- Display and copy the `clairs1...` shielded address.
- Scan chain events and recover spendable notes.
- Support deposit, shielded transfer, withdraw, and relayed withdraw.
- Handle user selective disclosure separately from mandatory audit disclosure.
- Decide where proofs are generated: browser/WASM, local prover, remote prover, or backend sidecar.
- Treat note cache, key material, prepared payloads, and disclosure plaintext as privacy-sensitive data.

## 2. Product Capabilities

### 2.1 Identity And Address

The client must derive Clairveil root material from the transparent signer, then derive spend/view/disclosure keys and the shielded address.

Required capabilities:

- transparent account connection
- root signing message request
- full shielded address display/copy/share
- disclosure public key display or export

Important notes:

- The root signing message is domain-separated from chain transaction signing.
- Root seed, spend key, view key, and disclosure key must not enter logs, analytics, or crash reports.
- If the user changes accounts, the shielded identity and note cache must be separated.

### 2.2 Chain Configuration

The client must receive runtime config for each downstream chain.

Required config:

- chain-id
- transparent account prefix
- shielded address prefix
- asset denom
- gRPC/REST/RPC endpoint
- prover endpoint and auth policy
- audit master public key
- circuit artifact/checksum information
- supported disclosure policies and modes

If the downstream chain changes denom, prefixes, gas policy, or prover topology, the client should receive those values through a registry or runtime config instead of hard-coding them.

### 2.3 Note Scan And Local State

The client must scan the privacy event feed and recover the user's own notes with the viewing key.

Required capabilities:

- show initial sync progress
- store the last scan height or cursor
- support rescan/reset
- check spent status through the nullifier query
- provide a recovery path for local cache corruption or decode failures

The local note cache can reveal amount, asset, and ownership metadata, so it is privacy-sensitive local data.

### 2.4 Deposit

Deposit moves transparent assets into the shielded pool and creates an encrypted note.

Required capabilities:

- show transparent balance and gas fee
- confirm deposit amount and denom
- broadcast deposit transaction
- after transaction success, show note recovery status from event scanning

### 2.5 Shielded Transfer

Shielded transfer spends a note and creates a new note for a recipient shielded address.

Required capabilities:

- recipient `clairs1...` address validation
- amount/denom input
- privacy policy selection: `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to`
- disclosure mode selection: `none`, `public`, `recipient-encrypted`
- explanation that mandatory audit disclosure is always included
- proof generation progress and failure states

Important product constraints:

- Transfer must always include audit disclosure even when user disclosure is disabled.
- When showing disclosure plaintext, verify that digest verification is true.
- Proof payloads can reveal sensitive metadata to a remote prover.

### 2.6 Withdraw

Withdraw sends a shielded note to a transparent recipient.

The current Clairveil withdraw requires an exact-match note.

- To withdraw `10uclair`, the wallet must have a spendable `10uclair` note.
- Withdraw does not create an output note or change note.
- `MsgWithdraw` has no output note fields.
- If no exact-match note exists, the client should guide the user to first create the desired note size with a shielded self-transfer, or prepare it through a separate planner flow.

Required capabilities:

- transparent recipient address validation
- exact-match note existence check
- self-transfer/planner preparation or clear failure when no exact-match note exists
- direct withdraw and relayed withdraw selection
- payload expiry and recipient immutability explanation

### 2.7 Disclosure Review

The client must be able to decode and verify public, recipient-encrypted, and audit disclosure payloads.

Required capabilities:

- disclosure source selection: tx hash, event payload, pasted payload
- disclosure plane selection: public, recipient, audit
- decryption availability display
- digest verification result display
- policy that unverified payloads must not be shown as factual

Successful decryption is different from successful verification. A payload can decrypt successfully but still be untrusted if digest verification fails.

## 3. Product Scope Boundaries

The Clairveil client documents provide:

- client feature scope
- shared UX flow baseline
- security/operations decision points
- API and prover integration checklist
- release gate baseline

The Clairveil repository does not provide:

- downstream product-specific PRDs
- screen wireframes
- iOS/Android implementation design
- custody/compliance operations policy
- remote prover business/pricing/operations policy

## 4. Related Documents

- [Client UX flows](clairveil-client-ux-flows.md)
- [Client risk decisions](clairveil-client-risk-decisions.md)
- [Client API checklist](clairveil-client-api-checklist.md)
- [JS SDK handoff](clairveil-js-sdk-handoff.md)
- [CLI reference](clairveil-cli-reference.md)
