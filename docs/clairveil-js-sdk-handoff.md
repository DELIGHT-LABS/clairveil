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

`MsgTransfer` contains both user disclosure and audit disclosure fields. In the latest model, audit disclosure is not optional. It must be included in every shielded transfer.

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
GET /clairveil/privacy/v1/nullifier/{nullifier}
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
- `PrivacyEvents`: scan the deposit/transfer event feed.
- `AuditConfig`: fetch the master auditor pubkey configured on-chain.
- `DisclosureConfig`: display user disclosure policy/mode and payload version.
- `CircuitConfig`: check the active circuit set and artifact checksum information.
- `CheckNullifier`: determine whether a note is spent.

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

A web wallet must read the privacy event feed and recover its own notes with the viewing key.

The Go SDK implementation is in:

```text
x/privacy/client/sdk/scan/scan.go
x/privacy/client/sdk/scan/service.go
x/privacy/client/sdk/scan/wallet.go
```

The scan flow is:

1. Fetch deposit/transfer events with the `PrivacyEvents` query.
2. Read `encrypted_note` from deposit events, or `cipher_text_1` and `cipher_text_2` from transfer events.
3. Try to decrypt using the wallet root seed and viewing key.
4. Store only notes that decrypt successfully in the wallet DB.
5. Track note commitment and nullifier.
6. Update spent state using `CheckNullifier` or event scan results.
7. Store event height and tx hash for rollback/reorg handling.

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
tx_hash
spent
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
- Supported policies are `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, and `amount-from-to`.
- Transfer payload/proof version is currently `v1`.
- Disclosure payload version is currently `v4` by query.

## 9. Disclosure Implementation

User selective disclosure and audit disclosure use the same payload verification model, but they live on different planes.

```text
user disclosure: sender-selected policy and delivery mode
audit disclosure: always generated for the chain audit master key
```

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
- If there is no exact-match note, the user must first create the desired note size with a shielded self-transfer.
- Relayed withdraw payload must validate `chain_id`, `recipient`, `expires_at_unix`, and `payload_hash`.
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
request_version: v1
response_version: v1
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
6. Implement deposit tx builder.
7. Implement disclosure encode/decode/verify helpers.
8. Implement transfer prepared payload builder.
9. Implement prover adapter and HTTP prover client.
10. Implement `MsgTransfer` builder and broadcast flow.
11. Implement withdraw prepared payload, direct withdraw, and relayed withdraw.
12. Add conformance fixture-based tests and local node e2e.

## 13. Validation Criteria

The JS SDK handoff is complete when the following work.

- `privacy_wallet_golden_vectors.json` produces the same root seed, spend/view/disclosure keys, and shielded address as Go.
- A JS wallet provider reproduces the signing contract in `privacy_browser_signer_provider_contract.json`.
- The SDK directly computes the shielded address corresponding to `show-address` on a local node.
- After deposit, event scanning finds the user's note.
- Transfer prepared payload hashes are calculated in the same way as the Go fixtures.
- Transfer/withdraw proof requests and responses are validated against the prover HTTP contract.
- User disclosure and audit disclosure decode with `verified=true`.
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
- transfer proof request/response version `v1`
- withdraw proof request/response version `v1`
- prover HTTP paths `/v1/prover/transfer`, `/v1/prover/withdraw`
- conformance fixture files under `x/privacy/client/sdk/conformance/testdata`

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
```

Check Go core sanity with:

```bash
make test
make privacy-e2e-smoke
```

## 16. Reference Consumer Examples

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
- transfer/withdraw request and response versions are `v1`;
- proof `payload_hash` equals the prepared payload `payload_hash`.
