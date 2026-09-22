# Clairveil JS/TS SDK Handoff

This document gathers the contracts needed by JS/TS SDK or web wallet developers implementing Clairveil privacy features. Its goal is to clearly separate what the Go core provides from what the JS SDK must implement.

Current runtime integration uses V2 asset messages, V2 audit configuration/key queries, and the single `/v2/prover/audit-field` route. Detailed NoteV1 payloads, disclosure modes, staged batch commands, and `/v1` prover paths retained later in this document are legacy fixture guidance only; they must not be emitted as current V2 transactions.

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
chain-id: supplied by the required audit configuration
current message/audit-query package: clairveil.privacy.v2
wallet scan/tree query package: clairveil.privacy.v1
```

If a downstream chain changes denom, chain-id, or gas policy, the JS SDK should receive those values through a chain registry or runtime config. Keeping the `clairs` shielded address prefix and proto package as the Clairveil privacy module contract is the simplest path.

## 3. Proto And Messages

The JS SDK must generate or directly model bindings for these proto files.

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
```

The current Msg service uses:

```text
/clairveil.privacy.v2.Msg/Deposit
/clairveil.privacy.v2.Msg/Transfer
/clairveil.privacy.v2.Msg/Withdraw
/clairveil.privacy.v2.Msg/BatchTransfer
/clairveil.privacy.v2.Msg/ScheduleAuditKeyEpoch
/clairveil.privacy.v2.Msg/CancelPendingAuditEpoch
/clairveil.privacy.v2.Msg/SetPrivacyHalt
```

The core tx messages are:

```text
MsgDeposit
MsgTransfer
MsgWithdraw
MsgBatchTransfer
```

All four V2 asset messages carry mandatory `AuditAuthorization {key_id, epoch, envelope}`. Deposit includes `creator`, amount, an `OutputEffect`, proof, and expiry. Withdraw includes creator, amount, recipient, root, nullifier, proof, expiry, and audit authorization. Transfer and batch carry root, ordered nullifiers/output effects, proof, expiry, and audit authorization. Generate bindings from the compiled V2 proto instead of copying the retained V1 field descriptions below.

The V1 Msg service is not registered when the audit runtime is enabled and legacy calls fail with `legacy privacy service is disabled`. Governance may use only the three V2 management messages; governance execution of the four asset messages is blocked.

## 4. Query/API Contract

The JS SDK provider should implement these gRPC/HTTP queries first.

```text
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
POST /clairveil/privacy/v1/commitment_paths_at_root
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom=**}
GET /clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}
GET /clairveil/privacy/v1/assets/by_id/{asset_id_hex}
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/scan_events
POST /clairveil/privacy/v1/privacy_scan
GET /clairveil/privacy/v2/audit/configuration
GET /clairveil/privacy/v2/audit/key_schedule
GET /clairveil/privacy/v2/audit/keys/{epoch}
```

The Go SDK provider contract is in:

```text
x/privacy/client/sdk/provider/info.go
x/privacy/client/sdk/provider/query.go
x/privacy/client/sdk/provider/scan.go
x/privacy/client/sdk/provider/typed_scan.go
x/privacy/client/sdk/provider/tx.go
```

A web wallet needs at least these provider roles.

- `TreeState`: read the latest root, leaf count, depth, max leaves, and remaining leaves.
- `CommitmentInfo`: check whether a commitment is in the tree and obtain its leaf index.
- `MerklePath`: fetch path and path helper needed for proving input.
- `CommitmentPathsAtRoot`: fetch up to 16 paths against one root/height snapshot for batch proving; treat the grouped lookup as a note-linkage privacy boundary.
- `AssetRegistry`: resolve canonical denoms and 32-byte asset IDs in both directions.
- `PrivacyScanV2`: the primary wallet-sync path. Invoke `PrivacyScan` to read typed deposit, JoinSplit2x2 transfer, and batch-transfer outputs with the global `(height, global_sequence, output_index)` cursor.
- `ScanEvents`: legacy compatibility projection for deposit and JoinSplit2x2 transfer only; it is not a batch-capable or primary wallet-sync API.
- `PrivacyEvents`: raw legacy event inspection for compatibility and diagnostics, not a wallet-sync projection.
- `AuditConfiguration`, `AuditKeySchedule`, and `AuditKey`: fetch the public runtime configuration, active/pending epochs, cancellations, and historical public keys through V2. The V1 `audit_config` contract is legacy compatibility material, not the current configuration source.
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

The preferred scan flow is `PrivacyScanV2` (the typed `PrivacyScan` query), not `ScanEvents` or ABCI event search:

1. Persist and send the complete lexicographic cursor `(height, global_sequence, output_index)`. `global_sequence` is chain-global across privacy operations, not a transaction-local or per-height sequence.
2. Request all event types and consume the typed deposit, JoinSplit2x2 transfer, and batch-transfer outputs. Read deposit `encrypted_note`; read transfer/batch `ciphertext`, `commitment`, `output_index`, and `view_tag`.
3. Fail closed unless the response, every summary, and every output have the supported `scan_schema_version`. Validate strict cursor order, contiguous output indices within an event, output-to-summary identity/framing, and the response limits.
4. Treat summaries as event boundaries, including zero-output withdrawals. Do not mark a multi-output event complete until all `output_count` outputs have been collected and that event has reached `has_more=false`; validate every `next_cursor` advance against the summaries so it cannot skip an output-bearing event.
5. A `PrivacyScanV2` query, validation, or decoding failure is terminal: do not fall back to `ScanEvents`, `PrivacyEvents`, or ABCI transaction/event search, because those paths can silently omit batch ciphertexts. Legacy fallback is only for an environment that does not implement the typed capability at all.
6. For transfer and batch outputs, derive the local 2-byte view tag. The ordered tag is signed but is not ownership evidence: `view_tag` is an untrusted optimization, and the safe default performs full trial decrypt even on a mismatch, missing tag, or malformed tag. Skipping mismatches requires an explicit fast-mode policy with recovery or forced-rescan support.
7. Try decryption with the wallet root seed and viewing key; retain the Go-compatible spend-key compatibility/recovery attempt. Store only successfully decrypted notes, with their commitment and nullifier.
8. Refresh spent state with `CheckNullifiers`, chunked to 1000 nullifiers per request; use `CheckNullifier` only when the batch path is unavailable.
9. Atomically store the resulting full cursor with note updates, plus output height, global sequence, output index, and tx hash for rollback/reorg handling.

`ScanEvents(after_height, after_sequence, limit, event_types)` remains a legacy cursor projection for deposit and JoinSplit2x2 compatibility only. Its `scan_format_version=1` and `view_tag_version=1` must be validated; its effective `limit` is a page budget, so filtered pages may return fewer events (including zero) while `has_more=true`, in which case advance `next_height`/`next_sequence`. It must not be used as the primary path or as fallback after a typed-query failure. `PrivacyEvents(after_height, page, limit, event_types)` is the raw legacy event-inspection API for compatibility and diagnostics; do not build primary rescan UX around its offset pagination.

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
global_sequence
output_index
tx_hash
spent
last_scan_height
last_scan_sequence
last_scan_output_index
```

`global_sequence` and `last_scan_sequence` both mean the chain-global privacy-operation sequence. `last_scan_height`, `last_scan_sequence`, and `last_scan_output_index` are one atomic `PrivacyScanV2` cursor; do not persist only a prefix.

## 7. Deposit Implementation

The older deposit SDK filenames and NoteV1 examples remain discovery and fixture handoffs, not the current wire or prover specification. Current clients build the V2 output effect, query nonce/initial height/active audit epoch/circuit identity, and use the shared [audit-field route](clairveil-proverd-http-api.md#current-route).

A current client builds the V2 `OutputEffect` and `AuditAuthorization`, obtains an audit-field proof locally or from `/v2/prover/audit-field`, verifies the repeated response binding, exact artifact identity, and final PI23 locally, then broadcasts `clairveil.privacy.v2.MsgDeposit`. Use the [current HTTP contract](clairveil-proverd-http-api.md#current-route) and compiled V2 proto as authority; the old per-deposit route and its client-field split are retained fixtures only.

## 8. Legacy NoteV1 Transfer Reference

This section preserves the V1 prepared-payload and inner-relation details used by retained fixtures. Do not encode it as a current transaction. Current V2 clients use `clairveil.privacy.v2.MsgTransfer`, mandatory `AuditAuthorization`, and `/v2/prover/audit-field`.

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
- active audit epoch public key (for legacy fixture comparison only);
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
- Disclosure plaintext/query version is `privacy-fixed-v2`. Enabled user disclosure and full audit/self-view disclosure use independent fresh CSPRNG blindings. After decrypting, recover the blinding and recompute the digest; decryption alone is not verification.
- For recipient output `0`, enforce `DBS-01` (`policy != 0 => user_blinding != output_randomness`), `DBS-02` (`full_blinding != output_randomness`), and `DBS-03` (`full_blinding != user_blinding`). All-private canonicalizes user blinding to zero and gates off only `DBS-01`. Output `1` is an active change note without a disclosure witness, not a disabled slot.
- Run the semantic validator before sending a prepared payload to any prover and before releasing an owner signature. Use the stable secret-free codes in `privacy_disclosure_blinding_v1_contract.json`; do not include randomness/blinding values in errors or telemetry.
- `expires_at_unix` is absolute. The chain rejects at `block_time >= expires_at_unix`.

The exact `JoinSplitCircuit` public-input order is: `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `Nullifier0`, `Nullifier1`, `Commitment0`, `Commitment1`, `UserPrivacyPolicy`, `UserDisclosureDigest`, `FullDisclosureDigest`, `PayloadDigestHi`, `PayloadDigestLo`. Do not sort or rename fields. SHA-256 chain/payload digests are split into two non-reduced big-endian 128-bit limbs. Chain domain input is `"clairveil.chain-domain.v1"`, length-prefixed `chain_id`, then length-prefixed `circuit_set_id` (`privacy-note-v1`).

The Go 2x2 boundary now uses `JoinSplitOwnerIntentSigningRequestV1`, which carries both input and output NoteV1 values, the canonical policy, sender public-key projection, recipient output randomness, user/full blindings, and final effect. `ValidateJoinSplitOwnerIntentSigningRequestV1` rebuilds ordered nullifiers, both commitments, value conservation, change ownership, and user/audit disclosure digests, compares them with the final effect, recomputes the domain, payload digest, and final intent, and applies `DBS-01..03`; `SignValidatedJoinSplitOwnerIntentV1` never invokes the callback for an invalid, redirected, or decoupled projection. A downstream structured wallet signer MUST preserve this fail-before-sign contract. Implementation of `DISCLOSURE-BLINDING-SEPARATION` is complete without changing transfer payload `v5`, proof/request/response `v2`, NoteV1, fixed payload encoding, disclosure digest formulas, or the 13-input schema; the new JoinSplit VK identity is `3dd068d67137791666e81e599b8b3b6820f92d8aed8234eca16370b2d54ed112`.

For bulk payroll or other high-volume transfer clients, the note reservation contract is part of the client/control-plane layer rather than the on-chain protocol. The Go reference implementation and fixture are:

```text
x/privacy/client/sdk/reservation/
x/privacy/client/sdk/payroll/
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
```

JS/TS clients that reserve notes before proof generation should treat `privacy_note_reservation_contract.json` as the language-neutral source of truth. Match the reservation status names, active-reservation definition, atomic batch-reserve rule, compare-and-set transition rules, lease token rules, HMAC lookup-key test vector, and operation success evidence model in that fixture. A spent nullifier proves that the note was consumed, but it is not enough to mark a payroll/payment operation successful unless the tx evidence also matches the expected output commitment, audit disclosure digest, recipient hash, amount, denom, and item index. The fixture field `expected_disclosure_digest` refers to the audit disclosure digest, not the user disclosure or sender self-view digest.

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

Keep a transport-neutral prover adapter. Its HTTP behavior, routes, versioning, errors, and common headers are defined only by the [general prover HTTP API](clairveil-proverd-http-api.md); deposit-specific witness/response handling is defined by the [deposit API](clairveil-proverd-http-api.md#deposit). Apply a finite timeout and strict response validation, never log/persist witness bodies, and require explicit user/product opt-in before sending the same witness to another endpoint.

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
- Deposit, transfer, withdraw, and batch proof requests/responses are validated against the prover HTTP contract, including each route's independent envelope and nested payload/proof versions and route-specific response binding.
- Bulk payroll clients reproduce the reservation transitions and operation success rules in `privacy_note_reservation_contract.json`.
- User disclosure, audit disclosure, and sender self-view disclosure decode with `verified=true`.
- Exact-match withdraw and relayed withdraw payload validation work.
- A JS SDK integration test completes deposit, transfer, batch, withdraw, rescan, and auditor verification against a separately documented native V2 flow. No checked-in Make target currently supplies this live evidence.

## 14. What The JS SDK Can Treat As Stable From The Go Core

The JS SDK can currently treat these as stable contracts.
- Current prover integration is `POST` `/v2/prover/audit-field` with request/response envelope `v1`, `privacy-note-v1-u128-audit-field-v1`, base64 `[]byte` fields, and final PI23.
- A client must perform local verification with the exact artifact identity after checking the repeated response binding. The older `/v1` example contracts below are legacy-only fixture references, not a live V2 SDK surface.

- current `clairveil.privacy.v2` asset/admin messages and V2 audit queries
- retained `clairveil.privacy.v1` wallet scan/tree/reserve queries
- gRPC/HTTP query paths
- typed `privacy_scan`, single-snapshot `commitment_paths_at_root`, and bidirectional asset-registry queries
- transparent prefix `clair`, shielded prefix `clairs`
- reference denom `uclair`
- full shielded address-based transfer UX
- mandatory V2 audit authorization with active key epoch
- user disclosure policy/mode labels
- current V2 asset messages require audit-field proof and authorization under the shared envelope `v1`/PI23 contract
- retained legacy fixtures: deposit payload/proof/request/response `v1`; transfer payload `v5` and proof/request/response `v2`; withdraw payload/proof/request/response `v2`; batch payload `batch-transfer-payload-v1`, proof `batch-transfer-proof-v1`, and request/response `v1`; disclosure plaintext/query `privacy-fixed-v2`. These are not the current V2 wire.
- active circuit set `privacy-note-v1-u128-audit-field-v1` with consensus `CircuitSetIdentity` schema `v1` and manifest schema `v2`
- sole live prover HTTP path `/v2/prover/audit-field`; the listed `/v1` routes are retained fixtures only
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`
- `DISCLOSURE-BLINDING-SEPARATION` V1 semantics/error codes and completed production 2x2 circuit/native/prepared/structured pre-sign enforcement; downstream signers must preserve the fail-before-release contract, including rejection of SDK-wide secret reuse and non-canonical field aliases. The security, protocol, chain-core, client-integration, and independent-publication-validation gates have passed, and the source is `PUBLICATION_READY_EXPERIMENTAL`
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
docs/clairveil-getting-started.md#7-first-privacy-flow
docs/clairveil-downstream-cosmos-integration-guide.md
docs/clairveil-operations-guide.md#6-prover-operations
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
x/privacy/client/sdk/conformance/testdata/privacy_wallet_golden_vectors.json
x/privacy/client/sdk/conformance/testdata/privacy_browser_signer_provider_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_prover_http_api_contract.json
x/privacy/client/sdk/conformance/testdata/privacy_send_capable_reference_flow.json
x/privacy/client/sdk/conformance/testdata/privacy_note_reservation_contract.json
```

Check Go core sanity with:

```bash
make test
```

Use a separately documented native V2 harness for live evidence; the repository exposes no end-to-end live smoke Make target.

## 16. Reference Consumer Examples

| Example | What it demonstrates |
| --- | --- |
| [Audit disclosure keys](../examples/audit-disclosure-keys/README.md) | Deterministic, random and privacy-root-signer key derivation with canonical genesis public-key encoding. |
| [Fixture validator](../examples/js-sdk-fixture-validator/README.md) | Wallet addresses, prepared payload hashes and relay mapping; it does not start a node. |
| [Prover HTTP client](../examples/js-sdk-prover-http-client/README.md) | Fixture-backed transfer/withdraw requests with finite timeout, bearer auth, version checks and payload-hash binding. |

Run commands are maintained with each example and in the [testing guide](clairveil-testing-guide.md). These examples are reference consumers, not a production SDK. The HTTP demo uses a mock, not a live `clairveil-proverd`; the JS client examples exercise transfer/withdraw only. Deposit and batch contracts are covered by the repository schemas and Go conformance fixtures. Port route-specific binding and fixture checks into downstream CI.

## 17. Batch Transfer Reference Addendum

The repository now includes the production core plus a reference Go batch builder, bounded proof adapter/HTTP route, decrypting typed scanner, durable payroll integration, and staged batch CLI. This JS SDK handoff still requires a downstream JS/TS implementation of those contracts. The older `transfer-batch` helper orchestrates native 2x2 messages and remains distinct from one `MsgBatchTransfer` proof.

The following rules are breaking and normative for new SDK work:

- The active circuit set is `privacy-note-v1-u128-audit-field-v1`. Note, disclosure, and encrypted-envelope binary data use `privacy-fixed-v2`; `NotePlaintextV1` is exactly 358 bytes, `DisclosurePlaintextV1` is exactly 400 bytes, and every encrypted payload includes the canonical 20-byte envelope header and exact kind. Raw ciphertext, JSON plaintext, trailing bytes, and cross-kind decoding must be rejected.
- Amounts are canonical decimal strings bounded by `M = 2^128-1`; calculate with `bigint` and check both input/output totals of each proof against `M`. Do not cap per-asset wallet balances or payroll/audit totals across proofs. Wallet files use version `3`; codec lengths and reserve bounds follow the [HTTP API amount contract](clairveil-proverd-http-api.md#uint128-amount-contract).
- This transition requires fresh genesis. Delete cached notes, scan cursors, prepared/proof jobs, circuit identity metadata, and old development artifacts, then regenerate artifacts and rescan. There is no compatibility decode or in-place state migration from the earlier contract.
- `AssetRegistryV1` is the authoritative one-to-one mapping between canonical denom and 32-byte `asset_id`. A client may derive an ID for validation, but must not invent a denom by interpreting or hashing an ID; resolve it through the registry query and fail closed on a mismatch.
- Wallet synchronization uses the unified `privacy-scan-v2` projection and lexicographic cursor `(height, global_sequence, output_index)`. Persist the whole cursor atomically. Obtain every Merkle path from a snapshot that matches the selected root exactly; mixing a current path with an older root is invalid. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical lookups can reveal wallet timing and interest, so retain the privacy warning and use privacy-preserving infrastructure where the product threat model requires it.
- The production `BatchJoinSplit16x32` public-input order is `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`. The Go reference path is complete; downstream JS/TS support remains feature-gated until it independently reproduces the conformance fixtures and localnet behavior.
- A downstream JS/TS batch builder must reproduce the reference `CanonicalBatchTransferPayloadBytesV1` exactly: format `1`, `u32be` vector counts, `u32be(length) || bytes` for every byte field, output fields in proto declaration order, followed by audit ID/epoch/target and expiry. SHA-256 domain `clairveil.batch-transfer-payload.v1` is split into non-reduced 128-bit limbs. Only `creator` and `proof` are excluded. Do not invent protobuf-marshal, JSON, or sorted-field alternatives.
- Artifact loading is role-aware: validators load only the required VKs after exact consensus identity verification; provers lazily load only selected R1CS/PK pairs. The reference prover admission defaults are one in-flight request and four queued requests per circuit, with a positive 8 MiB request limit. A value of zero is invalid and does not disable the body limit.
- Never expose `provertransport.HTTPHandler` directly; use the bounded `proverservice.Handler` wrapper. Prover requests have no automatic endpoint failover. Cancellation stops waiting and discards the response, but in-process proving may continue until the solver returns and still holds admission capacity. Production operators that require hard cancellation or memory containment must add process isolation and termination outside this reference implementation.

## 18. Reliability And Reference Payroll Handoff

This section adds only the JS/TS control-plane boundary. It does not replace the generic SDK validation criteria above or the batch encoding, artifact, and scan requirements in the addendum. The detailed reference is the [reference payroll control plane](../examples/reference-payroll/README.md).

### Retry And Endpoint Safety

Keep read, nullifier, broadcast, and prover traffic in separate adapters and persist the attempt identity before a side effect. The following matrix is the minimum safe behavior.

| Operation | Same-endpoint retry | Endpoint failover | Timeout handling |
| --- | --- | --- | --- |
| Public read query | Retry an idempotent query with a bounded backoff. | Allowed after validating chain ID and response shape; re-fetch root/path together rather than mixing snapshots. | Return an unavailable read; do not create a spend/proof side effect. |
| Nullifier query | Retry the identical canonical request, normally the POST batch request. | Off by default. A cross-endpoint nullifier query requires explicit user/product privacy opt-in because it discloses the queried note set to another operator. | Keep the reservation active/unknown; do not infer that a note is reusable. |
| Tx broadcast | Re-submit only the byte-identical signed transaction, keyed by its tx hash/sign-doc/tx-bytes hashes. | Allowed only for those identical signed bytes and an explicitly configured broadcast endpoint set; never rebuild merely to change endpoint. | First query the tx hash, then query every input nullifier. Re-sign/rebuild only after acceptance is excluded and nullifiers are unspent, or after a deterministic expiry/rejection requires a new intent. Otherwise retain `Unknown` or move to `ManualReview`. |
| Prover request | Do not automatically replay a witness request after an ambiguous timeout. A caller may make a product-approved retry only when it can safely treat the original as not accepted. | Never automatic. A second endpoint, including the same witness, needs separate explicit user/product opt-in and a fresh disclosure review. | Preserve the prepared payload and reservation; canceling HTTP does not prove that remote proving stopped. |

Do not use a public-read failover result to authorize a spend until the selected root/path is validated as one snapshot. Tx hash lookup is the primary ambiguity resolver; nullifier lookup is the required second check, not a substitute for matching operation evidence.

### Minimum Payroll And Wallet Surface

Model the following portable types; JS names may be idiomatic, but their fields and semantics must remain compatible with the Go reference.

| Type/API | Minimum handoff fields or behavior |
| --- | --- |
| `PayrollInput` / `PayrollItem` / `PayrollPlan` | Stable company, payroll, batch, item, employee, operation, attempt, denom, amount, recipient, disclosure-policy, expected output/disclosure values, and timestamps. `PayrollItem` corresponds to Go `PayrollItemInput`; plan items retain selected input notes and retry/status separately from the plan. |
| `TreasuryNote` | `note_id`, owner and nullifier-lookup key/ID, denom, amount, spent flag, and `reservation_id`. Exclude any spent note or non-empty `reservation_id` from allocation and preparation. |
| `NotePreparationReport` / `NotePreparationHint` | Report ready/blocked items, spendable/reserved/spent counts and amounts, zero-dummy availability/shortage, selected note IDs, estimated message chunks, and actionable `add-funds`, `make-dummy`, `split-merge`, or `resolve-reservation-lock` hints. `NotePreparationHint` maps to Go `NotePreparationOperationHint`. It is a signal to prepare notes, not proof that notes are available. |
| `DisclosureKeyEntry` / registry | Store `key_id`, scope (`employee`, `company`, `auditor`, or `external`), subject ID, canonical public key hex, version, and active flag. Resolve an active key by `(scope, subject_id)` before planning; do not silently fall back to a stale or inactive key. |
| `NoteReservation` / `PayrollOperation` | Persist reservation/operation IDs, status, lease fields, input-note linkage, tx/sign-doc/tx-bytes hashes, broadcast-attempt metadata, expected output commitment, disclosure digest(s), recipient/amount hashes, denom, and batch item index plus its known flag. Use atomic batch reserve and compare-and-set, token-owned lease transitions from `privacy_note_reservation_contract.json`. |

Expose at least `validatePayroll`, `prepareNotes`, `planPayroll`, atomic `reserve`, lease acquire/heartbeat, `markProofReady`, `markSubmitted`/`markBroadcastUnknown`, `reconcile`, and `rescanProjection` operations. Product-specific method names are acceptable; weakening their durable-state and compare-and-set semantics is not.

The operation-success predicate requires `tx_hash_or_tx_result`, `output_index`, output `commitment`, `recipient_hash`, `amount` (or its expected hash), `denom_or_asset_id`, audit/full disclosure digest, user digest when expected, `audit_key_id`, `audit_key_epoch`, and `batch_item_index` with its known flag when the plan requires position. A spent nullifier without that matching evidence is `ConflictSpent`, not success.

Wallet storage must keep the encrypted note inventory/projection and scan cursor separately from durable reservations, operations, and broadcast attempts. A normal rescan may rebuild the projection, but must first reconcile or retain active/unknown reservations; it must not erase their linkage and make notes selectable. Unsupported projection versions, ambiguous broadcast, missing evidence, or cross-endpoint inconsistency require a stopped cursor plus `ManualReview`, with a user-visible rescan/reconcile path.

Payroll handoff is complete when the JS/TS implementation reproduces the reservation fixture's atomic reserve, active-note exclusion, lease/CAS transitions, and success/conflict evidence cases; resolves disclosure keys through the registry; produces and consumes preparation reports/hints; and survives a timeout by tx-hash then nullifier reconciliation without duplicate signing or note reuse.
