# Clairveil JS/TS SDK Handoff

This document gathers the contracts needed by JS/TS SDK or web wallet developers implementing Clairveil privacy features. Its goal is to clearly separate what the Go core provides from what the JS SDK must implement.

Korean version: [clairveil-js-sdk-handoff-kr.md](clairveil-js-sdk-handoff-kr.md)

## 1. User Features The JS SDK Must Provide

A web wallet should ultimately provide the following privacy features.

- Derive shielded identity from a transparent account.
- Display and copy the full `clairs1...` shielded address.
- Scan chain events with the incoming viewing key and recover the user's notes.
- Build and broadcast deposit transactions.
- Build and broadcast shielded transfer transactions.
- Generate user selective disclosure in public or recipient-encrypted mode.
- Automatically include mandatory audit disclosure in every transfer.
- Decode disclosure payloads and show digest verification results.
- Support direct withdraw and relayed withdraw payload flows.
- Abstract whether proving uses browser WASM, a local companion prover, or a remote companion prover.

## 2. Network Constants

The Clairveil standalone reference chain constants are:

```text
Go module: github.com/DELIGHT-LABS/clairveil
daemon: clairveild
transparent account prefix: clair
shielded address prefix: clairs
reference denom: uclair
default local chain-id: clairveil-local-1
proto package: clairveil.privacy.v1
```

If a downstream chain changes denom, chain-id, or gas policy, the JS SDK should receive those values through a chain registry or runtime config. Keeping the `clairs` shielded address prefix and proto package as the Clairveil privacy module contract is the simplest path.

## 3. Proto And Messages

The JS SDK must generate or directly model bindings for these proto files.

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

The Msg service uses:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

The core tx messages are:

```text
MsgDeposit
MsgTransfer
MsgWithdraw
```

`MsgDeposit` includes a `proof` field. Clients must build or obtain a `DepositCircuit` Groth16 proof binding `amount`, `asset_id`, and `note_commitment`; proof-less deposits are not part of the current contract.

`MsgTransfer` contains `expires_at_unix`, user disclosure, audit disclosure, and sender self-view disclosure fields. Audit disclosure is not optional. Sender self-view disclosure is included by default and omitted only by explicit opt-out. `creator` is the replaceable fee payer/relayer; it is deliberately excluded from the owner intent.

`MsgWithdraw` is an exact-match withdraw message and does not contain output note fields. JS/TS clients must not model the legacy `new_note_commitment` or `encrypted_note` withdraw fields, and they must not send dummy output-note values.

## 4. Query/API Contract

The JS SDK provider should implement these gRPC/HTTP queries first.

```text
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
```

The Go SDK provider contract is in:

```text
x/privacy/client/sdk/provider/info.go
x/privacy/client/sdk/provider/query.go
x/privacy/client/sdk/provider/scan.go
x/privacy/client/sdk/provider/tx.go
```

A web wallet needs at least these provider roles.

- `TreeState`: read the latest root, leaf count, depth, max leaves, and remaining leaves.
- `CommitmentInfo`: check whether a commitment is in the tree and obtain its leaf index.
- `MerklePath`: fetch path and path helper needed for proving input.
- `ScanEvents`: scan the cursor-based wallet projection for deposit/transfer outputs.
- `PrivacyEvents`: read the raw deposit/transfer event feed for compatibility and diagnostics.
- `AuditConfig`: fetch the master auditor pubkey configured on-chain.
- `DisclosureConfig`: display user disclosure policy/mode and payload version.
- `CircuitConfig`: read the consensus `CircuitSetIdentity`, active set, ordered VK hashes, and public-input schema hashes. Do not infer consensus identity from a node-local manifest path or checksum environment variable.
- `Reserve`: compare privacy module-account balance to recorded deposit/withdraw totals for a denom.
- `CheckNullifiers`: refresh spent state for many notes in one request. Use the POST JSON body binding for normal batches, chunk at 1000 nullifiers per request, and keep GET only for small compatibility checks.
- `CheckNullifier`: determine whether one note is spent when a batch path is unavailable.

## 5. Identity Derivation

Clairveil wallet identity is a single-root model layered on top of the transparent keyring.

```text
transparent signer
  -> root signing message
  -> root seed
  -> spend key
  -> view key
  -> disclosure key
  -> full shielded address
```

The Go SDK implementation is in:

```text
x/privacy/client/sdk/identity/identity.go
x/privacy/client/sdk/identity/signer.go
x/privacy/types/address.go
```

The JS SDK must receive the transparent account address, public key, and signature from the browser wallet, then derive the root seed. The root signing message is domain-separated from chain tx signing, so a normal transfer tx signature must not be reused.

The browser provider reference fixtures are:

```text
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_wallet_readonly_reference_bundle.json
```

The machine-readable fixture shape is defined by this JSON Schema:

```text
docs/schemas/clairveil-js-wallet-contract.schema.json
```

A JS/TS SDK should include at least the same validation as this command in CI.

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

This validation pins required fixture fields, versions, address prefixes, hash lengths, and prover request/response shapes. JSON Schema alone is not enough for payload hash recomputation or disclosure/prover semantic verification, so keep the semantic checks from the validator example as well.

## 6. Note Scanning

A web wallet must read the wallet scan projection and recover its own notes with the viewing key.

The Go SDK implementation is in:

```text
x/privacy/client/sdk/scan/scan.go
x/privacy/client/sdk/scan/service.go
x/privacy/client/sdk/scan/wallet.go
```

The preferred scan flow is:

1. Fetch deposit/transfer outputs with `ScanEvents(after_height, after_sequence, limit, event_types)`.
2. Read `encrypted_note` from deposit projections, or output `cipher_text`, `commitment`, `output_index`, and `view_tag` from transfer projections.
3. Validate `scan_format_version` and `view_tag_version` before consuming the projection; fall back to the raw event path or stop without advancing the cursor on unsupported versions.
4. For transfer outputs, derive the local 2-byte view tag. The ordered tag is included in the signed canonical transfer effect but is not ownership evidence, so the safe default still runs full trial decrypt on mismatch.
5. Treat `view_tag` as an untrusted optimization only. If it is missing or malformed, fall back to full trial decrypt. Skipping mismatch outputs should be an explicit fast-mode policy with recovery or forced-rescan support.
6. Try to decrypt using the wallet root seed and viewing key. If view-key decryption fails, keep a spend-key compatibility/recovery fallback consistent with the Go SDK.
7. Store only notes that decrypt successfully in the wallet DB.
8. Track note commitment and nullifier.
9. Refresh spent state with `CheckNullifiers` when available, chunking at 1000 nullifiers per request and falling back to `CheckNullifier`.
10. Store event height, sequence, and tx hash for rollback/reorg handling.

`ScanEvents` returns the effective `limit`, `scan_format_version=1`, and `view_tag_version=1`. Treat `limit` as the scan cursor page budget: a response can contain fewer returned events than `limit`, or even zero events, while still setting `has_more=true` if the page only contained event types filtered out by the request. In that case, advance to `next_height` and `next_sequence` and continue. The legacy `PrivacyEvents(after_height, page, limit, event_types)` query is still available for raw event inspection and compatibility, but new web/mobile wallets should not build primary rescan UX around offset pagination.

The JS SDK wallet DB needs at least these fields.

```text
commitment_hex
nullifier_hex
amount
asset_denom
asset_id_hex
randomness_hex
spend_pubkey_hex
view_pubkey_hex
height
sequence
tx_hash
spent
last_scan_height
last_scan_sequence
```

## 7. Deposit Implementation

Deposit moves transparent balance into the privacy module account and appends one leaf.

The corresponding CLI command is:

```bash
clairveild tx privacy deposit 10uclair --from alice --keyring-backend test
```

The JS SDK must:

- create a note from the recipient wallet's shielded identity;
- compute the note commitment;
- create the encrypted note;
- generate a `DepositCircuit` proof locally, through WASM, or through a trusted prover adapter;
- build `MsgDeposit` and sign/broadcast it as a normal Cosmos tx;
- confirm the commitment and encrypted note event in the tx result.

## 8. Transfer Implementation

Transfer uses only the latest single model. Legacy `transfer-v2` and `transfer-v3` commands are not part of the downstream/JS SDK contract.

The corresponding CLI command is:

```bash
clairveild tx privacy transfer <recipient_clairs_address> 7uclair \
  --from alice \
  --keyring-backend test
```

A JS SDK transfer builder gathers:

- sender shielded identity;
- recipient full shielded address;
- spendable notes;
- target amount and denom;
- current tree root;
- Merkle path for selected notes;
- chain audit master pubkey;
- optional user disclosure target pubkey;
- user disclosure policy and mode.

Transfer should be structured as prepared payload before proof generation, then proof response from the prover, then final `MsgTransfer` construction.

The Go SDK implementation is in:

```text
x/privacy/client/sdk/transfer/prepare.go
x/privacy/client/sdk/transfer/payload.go
x/privacy/client/sdk/transfer/prove.go
x/privacy/client/sdk/transfer/build.go
x/privacy/client/sdk/transfer/service.go
```

Important constraints:

- Transfer has 2 input notes and 2 output notes.
- Output 0 is the recipient note and output 1 is the change note.
- Every transfer must include audit disclosure.
- User disclosure supports `none`, `public`, and `recipient-encrypted` mode.
- Sender self-view disclosure is enabled by default and omitted only by explicit opt-out.
- Supported policies are `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, and `amount-from-to`.
- Newly generated transfer payloads must use `v5`; transfer proof and prover request/response use `v2`. All earlier transfer payload/proof/request versions are rejected and must be regenerated.
- Build both outputs, ordered ciphertexts/view tags, user/audit/self-view envelopes, independent disclosure blindings, chain ID, and absolute expiry first. Then encode the canonical transfer effect, derive `TransferIntentV2`, and create exactly one `owner_signature_hex`. There are no per-input note-hash signatures.
- The canonical binary effect uses fixed field order and `u32be(length) || bytes` for variable bytes. It includes format version, root, ordered nullifiers/commitments/ciphertexts/view tags, every disclosure field, and expiry. It excludes proof, `creator`, fee/gas/memo/sequence/tx signature, and its own digest. The keeper recomputes it from `MsgTransfer`.
- Final `MsgTransfer` must include exactly two `view_tags`, aligned with `new_commitments` and `cipher_texts`.
- Disclosure plaintext/query version is `privacy-fixed-v1`. Enabled user disclosure and full audit/self-view disclosure use independent fresh CSPRNG blindings. After decrypting, recover the blinding and recompute the digest; decryption alone is not verification.
- For recipient output `0`, enforce `DBS-01` (`policy != 0 => user_blinding != output_randomness`), `DBS-02` (`full_blinding != output_randomness`), and `DBS-03` (`full_blinding != user_blinding`). All-private canonicalizes user blinding to zero and gates off only `DBS-01`. Output `1` is an active change note without a disclosure witness, not a disabled slot.
- Run the semantic validator before sending a prepared payload to any prover and before releasing an owner signature. Use the stable secret-free codes in `privacy_disclosure_blinding_v1_contract.json`; do not include randomness/blinding values in errors or telemetry.
- `expires_at_unix` is absolute. The chain rejects at `block_time >= expires_at_unix`.

The exact `JoinSplitCircuit` public-input order is: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `Nullifier0`, `Nullifier1`, `Commitment0`, `Commitment1`, `UserPrivacyPolicy`, `UserDisclosureDigest`, `FullDisclosureDigest`, `PayloadDigestHi`, `PayloadDigestLo`. Do not sort or rename fields. SHA-256 chain/payload digests are split into two non-reduced big-endian 128-bit limbs. Chain domain input is `"clairveil.chain-domain.v1"`, length-prefixed `chain_id`, then length-prefixed `circuit_set_id` (`privacy-note-v1`).

The Go 2x2 boundary now uses `JoinSplitOwnerIntentSigningRequestV1`, which carries both input and output NoteV1 values, the canonical policy, sender public-key projection, recipient output randomness, user/full blindings, and final effect. `ValidateJoinSplitOwnerIntentSigningRequestV1` rebuilds ordered nullifiers, both commitments, value conservation, change ownership, and user/audit disclosure digests, compares them with the final effect, recomputes the domain, payload digest, and final intent, and applies `DBS-01..03`; `SignValidatedJoinSplitOwnerIntentV1` never invokes the callback for an invalid, redirected, or decoupled projection. A downstream structured wallet signer MUST preserve this fail-before-sign contract. `S4-B02` implementation is resolved without changing transfer payload `v5`, proof/request/response `v2`, NoteV1, fixed payload encoding, disclosure digest formulas, or the 13-input schema; the new JoinSplit VK identity is `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`.

For bulk payroll or other high-volume transfer clients, the note reservation contract is part of the client/control-plane layer rather than the on-chain protocol. The Go reference implementation and fixture are:

```text
x/privacy/client/sdk/reservation/
x/privacy/client/sdk/payroll/
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
docs/clairveil-note-reservation-design.md
docs/clairveil-note-reservation-design-kr.md
```

JS/TS clients that reserve notes before proof generation should treat `privacy_note_reservation_contract.json` as the language-neutral source of truth and use [Clairveil Note Reservation Design Note](clairveil-note-reservation-design.md) for the detailed design rationale. Match the reservation status names, active-reservation definition, atomic batch-reserve rule, compare-and-set transition rules, lease token rules, HMAC lookup-key test vector, and operation success evidence model in that fixture. A spent nullifier proves that the note was consumed, but it is not enough to mark a payroll/payment operation successful unless the tx evidence also matches the expected output commitment, audit disclosure digest, recipient hash, amount, denom, and item index. The fixture field `expected_disclosure_digest` refers to the audit disclosure digest, not the user disclosure or sender self-view digest.

## 9. Disclosure Implementation

User selective disclosure, audit disclosure, and sender self-view disclosure use the same payload verification model, but they live on different planes and have different delivery meaning.

```text
user disclosure: sender-selected policy and delivery mode
audit disclosure: always generated for the chain audit master key
self-view disclosure: generated by default for the sender's own disclosure key
```

Self-view disclosure is an encrypted payload that lets the sender later view the amount/from/to details of their own sent transfer. The on-chain event includes only `self_view_disclosure_digest` and `self_view_disclosure_payload`; it intentionally does not expose the sender's static disclosure public key. The JS SDK should trial-decrypt self-view payloads with the sender disclosure private key, then verify the payload digest against the on-chain digest.

Audit and self-view plaintext carry the same fresh full-disclosure blinding and verify against `FullDisclosureDigest`; optional user disclosure uses a different fresh blinding. Never derive a blinding from low-entropy plaintext or reuse it across transfers/planes.

The web wallet UI should provide at least these user disclosure choices.

```text
mode: none | public | recipient-encrypted
policy: all-private | amount | to | amount-to | from | amount-from | from-to | amount-from-to
```

The CLI command for fetching the event by tx hash and showing a verification report is:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash <transfer_tx_hash> \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --report
```

The CLI-equivalent command for sender self-view is:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash <transfer_tx_hash> \
  --disclosure-plane self-view \
  --from sender \
  --keyring-backend test \
  --report
```

The JS SDK should display at least these fields from decode results.

- plane
- policy
- output index
- commitment hex
- digest hex
- verified
- disclosed fields
- amount
- asset denom
- from shielded address
- to shielded address

The Go SDK implementation is in:

```text
x/privacy/client/sdk/disclosure/disclosure.go
x/privacy/client/sdk/transfer/disclosure.go
```

## 10. Withdraw Implementation

Withdraw currently requires an exact-match note. To withdraw `10uclair`, the wallet must have a spendable `10uclair` note.

Direct withdraw CLI equivalent:

```bash
clairveild tx privacy withdraw 10uclair \
  --recipient "$(clairveild keys show bob -a --keyring-backend test)" \
  --from alice \
  --keyring-backend test
```

Relayed withdraw splits prepare and broadcast.

```bash
clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(clairveild keys show bob -a --keyring-backend test)" \
  --from alice \
  --keyring-backend test \
  --out ./withdraw-payload.json

clairveild tx privacy relay-withdraw ./withdraw-payload.json \
  --from relayer \
  --keyring-backend test
```

From a client perspective, relayed withdraw splits responsibilities as follows.

- The user client receives the withdraw proof response and builds the final `PreparedWithdrawPayload` JSON.
- Transport between the user client and relayer is product-defined. HTTP, QR, deep link, and file handoff are all possible.
- After the payload is handed to the relayer, it may still be submitted until `expires_at_unix`. Local cancel, UI dismissal, or releasing a local reservation does not invalidate the already-created payload.
- The relayer client/server validates `payload_hash`, `chain_id`, `recipient`, and `expires_at_unix`, then sets its own address as `MsgWithdraw.creator` before signing and broadcasting.
- The transparent withdraw target is the payload `recipient`, not the relayer address.
- This repository does not provide a production relay HTTP endpoint. Instead, it fixes the final-payload-to-relayer-submitted-message contract in `x/privacy/client/sdk/conformance/testdata/privacy_relay_withdraw_contract.json`.

The Go SDK implementation is in:

```text
x/privacy/client/sdk/withdraw/prepare.go
x/privacy/client/sdk/withdraw/prover_payload.go
x/privacy/client/sdk/withdraw/prove.go
x/privacy/client/sdk/withdraw/payload.go
x/privacy/client/sdk/withdraw/build.go
```

The JS SDK must clearly show these constraints to users.

- Withdraw does not create a change note.
- `MsgWithdraw` does not contain output note fields. Do not create a dummy output commitment or encrypted note for withdraw.
- If there is no exact-match note, the user must first create the desired note size with a shielded self-transfer.
- Relayed withdraw payload must validate `chain_id`, `recipient`, `expires_at_unix`, and `payload_hash`.
- Withdraw prover payload, proof, final payload, prover request, prover response, relay schema, and relay handoff are all `v2`; legacy files must be regenerated.
- `spend_intent_signature_hex` authenticates `SpendIntentV2`. The recipient is hashed from the exact raw decoded address bytes as `SHA-256("clairveil.withdraw-recipient.v1" || u32be(len(bytes)) || bytes)` and split into non-reduced big-endian 128-bit limbs. Do not convert the bytes through a field element or strip leading zeros.
- The exact spend public-input order is `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `Nullifier`, `Amount`, `RecipientDigestHi`, `RecipientDigestLo`, `AssetID`.
- `creator` is intentionally replaceable by a relayer; `recipient`, chain, and expiry are proof-bound. Submission at `block_time >= expires_at_unix` fails.
- Once handed off, a relayed withdraw payload remains submit-capable until expiry; the wallet must not treat local cancellation as proof that the note is reusable.
- The relayer does not need to know the user's shielded secret.

## 11. Prover Connection Model

The JS SDK should define a prover adapter interface first, rather than directly embedding a proving implementation.

```text
Browser SDK
  -> build prepared payload
  -> ProverAdapter.proveTransfer / proveWithdraw
  -> proof response
  -> build MsgTransfer / MsgWithdraw
  -> sign and broadcast with the existing Cosmos/downstream wallet stack
```

The current Go-side prover HTTP contract is:

```text
POST /v1/prover/transfer
POST /v1/prover/withdraw
Content-Type: application/json
request_version: v2
response_version: v2
```

Error codes are:

```text
invalid_request
method_not_allowed
not_found
unauthorized
unavailable
proof_failed
```

Related fixtures are:

```text
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_example_bundle.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
```

Whether the prover is a local daemon or a remote sidecar, it should look like the same adapter from the JS SDK's perspective. A future browser/WASM proving backend should also sit behind the same interface.

The prepared prover payload is authority-equivalent privacy-sensitive witness data even though its final outputs can no longer be changed. Never log or persist it unnecessarily. A prover pool must use one endpoint with no automatic failover by default. Sending the same witness to another endpoint is allowed only after explicit user/product-policy opt-in that names the additional privacy boundary; retries may target the same endpoint.

When connecting a remote prover, enforce request timeout and response validation at the client boundary. The examples and operations profile are:

```text
examples/js-sdk-prover-http-client
docs/clairveil-proverd-remote-production-profile.md
```

## 12. JS SDK Implementation Units

Recommended implementation order:

1. Attach proto/type generation.
2. Define network constants and chain config.
3. Implement identity derivation and `clairs1...` address encode/decode.
4. Implement the query provider.
5. Implement event scanner and wallet note store.
6. Implement deposit proof and tx builder.
7. Implement disclosure encode/decode/verify helpers.
8. Implement transfer prepared payload builder.
9. Implement prover adapter and HTTP prover client.
10. Implement `MsgTransfer` builder and broadcast flow.
11. Implement withdraw prepared payload, direct withdraw, and relayed withdraw.
12. For bulk payroll clients, implement note reservation and operation-state tracking from `privacy_note_reservation_contract.json`.
13. Add conformance fixture-based tests and local node e2e.

## 13. Validation Criteria

The JS SDK handoff is complete when the following work.

- `privacy_wallet_golden_vectors.json` produces the same root seed, spend/view/disclosure keys, and shielded address as Go.
- A JS wallet provider reproduces the signing contract in `privacy_browser_signer_provider_contract.json`.
- The SDK directly computes the shielded address corresponding to `show-address` on a local node.
- After deposit, event scanning finds the user's note.
- Transfer prepared payload hashes are calculated in the same way as the Go fixtures.
- `privacy_disclosure_blinding_v1_contract.json` positive/sentinel/negative vectors produce the same `DBS_*` result codes, and structured signing refuses every invalid vector before signature release.
- Transfer/withdraw proof requests and responses are validated against the prover HTTP contract.
- Bulk payroll clients reproduce the reservation transitions and operation success rules in `privacy_note_reservation_contract.json`.
- User disclosure, audit disclosure, and sender self-view disclosure decode with `verified=true`.
- Exact-match withdraw and relayed withdraw payload validation work.
- A JS SDK integration test can follow the same flow as Clairveil repo's `make privacy-e2e-smoke`.

## 14. What The JS SDK Can Treat As Stable From The Go Core

The JS SDK can currently treat these as stable contracts.

- `clairveil.privacy.v1` proto package
- `MsgDeposit`, `MsgTransfer`, `MsgWithdraw`
- gRPC/HTTP query paths
- transparent prefix `clair`, shielded prefix `clairs`
- reference denom `uclair`
- full shielded address-based transfer UX
- mandatory audit disclosure
- user disclosure policy/mode labels
- deposit proof requirement for `MsgDeposit`
- transfer payload `v5` and transfer proof/request/response `v2`
- withdraw prover/final payload and proof/request/response `v2`
- disclosure plaintext/query version `privacy-fixed-v1`
- active circuit set `privacy-note-v1` with consensus `CircuitSetIdentity` schema `v1` and manifest schema `v2`
- prover HTTP paths `/v1/prover/transfer`, `/v1/prover/withdraw`
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`
- `DISCLOSURE-BLINDING-SEPARATION` V1 semantics/error codes and completed production 2x2 circuit/native/prepared/structured pre-sign enforcement; downstream signers must preserve the fail-before-release contract. Fresh Gates 1/2/3A pass and Session 3B re-entry is unblocked, while Gate 3B and publication remain blocked
- note reservation status and operation evidence contract in `privacy_note_reservation_contract.json`

The JS SDK still needs to decide these independently.

- wallet local DB schema
- encrypted local storage method
- browser wallet provider API shape
- remote prover authentication method
- remote prover rate limit and quota policy
- how disclosure choices appear in the web UI
- the downstream chain's actual chain-id, denom, gas, and fee policy

## 15. Files Developers Should Read First

JS SDK developers should start with these files.

```text
docs/clairveil-local-privacy-walkthrough.md
docs/clairveil-downstream-cosmos-integration-guide.md
docs/clairveil-proverd-remote-production-profile.md
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
```

Check Go core sanity with:

```bash
make test
make privacy-e2e-smoke
```

## 16. Reference Consumer Examples

For audit disclosure key generation in JS, see:

```text
examples/audit-disclosure-keys
```

Run it from the repository root:

```bash
npm --prefix examples/audit-disclosure-keys test
```

This example derives deterministic, random, and privacy-root-signer-based audit disclosure keypairs, then checks the compressed public key encoding used in genesis.

Clairveil includes a small example showing how JS/TS SDK developers can start consuming fixtures.

```text
examples/js-sdk-fixture-validator
```

Run it from the repository root:

```bash
npm --prefix examples/js-sdk-fixture-validator run validate
```

This example does not start a node. It only validates:

- wallet-facing fixture addresses use `clair1...` and `clairs1...`;
- wallet-facing fixture addresses use only the `clair1...` or `clairs1...` prefixes;
- transfer prepared payload hash is calculated the same way as the Go SDK;
- withdraw prover payload hash is calculated the same way as the Go SDK;
- relayed withdraw final payload hash is calculated;
- relay withdraw handoff keeps the relayer address as `MsgWithdraw.creator` and the payload recipient as `MsgWithdraw.recipient`;
- prover HTTP paths are `/v1/prover/transfer` and `/v1/prover/withdraw`.

This is a first reference consumer, not a production JS SDK. A real JS SDK should not copy its file layout directly. Instead, bring the same hash contract and fixture validation into CI.

For the remote prover HTTP client shape, see:

```text
examples/js-sdk-prover-http-client
```

Run it from the repository root:

```bash
npm --prefix examples/js-sdk-prover-http-client run demo
```

This example runs a fixture-backed mock prover instead of a live `clairveil-proverd`, and validates:

- `fetch` requests use a finite timeout;
- bearer tokens are sent as `Authorization: Bearer ...`;
- transfer/withdraw request, response, and proof versions are `v2`;
- proof `payload_hash` equals the prepared payload `payload_hash`.

## 17. Session 3B Reference Addendum

The repository now includes the production core plus a reference Go batch builder, bounded proof adapter/HTTP route, decrypting typed scanner, durable payroll integration, and staged batch CLI. This JS SDK handoff still requires a downstream JS/TS implementation of those contracts. The older `transfer-batch` helper orchestrates native 2x2 messages and remains distinct from one `MsgBatchTransfer` proof.

The following rules are breaking and normative for new SDK work:

- The active circuit set is `privacy-note-v1`. Note, disclosure, and encrypted-envelope binary data use `privacy-fixed-v1`; `NotePlaintextV1` is exactly 350 bytes, `DisclosurePlaintextV1` is exactly 392 bytes, and every encrypted payload includes the canonical 20-byte envelope header and exact kind. Raw ciphertext, JSON plaintext, trailing bytes, and cross-kind decoding must be rejected.
- This transition requires fresh genesis. Delete cached notes, scan cursors, prepared/proof jobs, circuit identity metadata, and old development artifacts, then regenerate artifacts and rescan. There is no compatibility decode or in-place state migration from the earlier contract.
- `AssetRegistryV1` is the authoritative one-to-one mapping between canonical denom and 32-byte `asset_id`. A client may derive an ID for validation, but must not invent a denom by interpreting or hashing an ID; resolve it through the registry query and fail closed on a mismatch.
- Wallet synchronization uses the unified `privacy-scan-v2` projection and lexicographic cursor `(height, global_sequence, output_index)`. Persist the whole cursor atomically. Obtain every Merkle path from a snapshot that matches the selected root exactly; mixing a current path with an older root is invalid. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical lookups can reveal wallet timing and interest, so retain the privacy warning and use privacy-preserving infrastructure where the product threat model requires it.
- The production `BatchJoinSplit16x32` public-input order is `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. The Go reference path is complete; downstream JS/TS support remains feature-gated until it independently reproduces the conformance fixtures and localnet behavior.
- A downstream JS/TS batch builder must reproduce the reference `CanonicalBatchTransferPayloadBytesV1` exactly: format `1`, `u32be` vector counts, `u32be(length) || bytes` for every byte field, output fields in proto declaration order, followed by audit ID/epoch/target and expiry. SHA-256 domain `clairveil.batch-transfer-payload.v1` is split into non-reduced 128-bit limbs. Only `creator` and `proof` are excluded. Do not invent protobuf-marshal, JSON, or sorted-field alternatives.
- Artifact loading is role-aware: validators load only the required VKs after exact consensus identity verification; provers lazily load only selected R1CS/PK pairs. The reference prover admission defaults are one in-flight request and four queued requests per circuit, with a positive 8 MiB request limit. A value of zero is invalid and does not disable the body limit.
- Never expose `provertransport.HTTPHandler` directly; use the bounded `proverservice.Handler` wrapper. Prover requests have no automatic endpoint failover. Cancellation stops waiting and discards the response, but in-process proving may continue until the solver returns and still holds admission capacity. Production operators that require hard cancellation or memory containment must add process isolation and termination outside this reference implementation.
