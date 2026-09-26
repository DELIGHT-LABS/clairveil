# Clairveil CLI Reference

This document describes the user-facing features of `clairveild` and privacy-related companion binaries.

All examples use the reference chain:

```text
chain-id: clairveil-local-1
denom: uclair
transparent prefix: clair
shielded prefix: clairs
```

Korean version: [clairveil-cli-reference-kr.md](clairveil-cli-reference-kr.md)

## 1. Basic Rules

Most tx commands use standard Cosmos SDK tx flags.

```bash
--from alice
--keyring-backend test
--chain-id clairveil-local-1
--gas 9000000
--gas-prices 8500000000uclair
--yes
--output json
```

`--output json` is the default choice when you need a tx broadcast response or command-specific JSON in a machine-readable form.

## 2. Shielded Identity

### show-address

Derives a full shielded address from a transparent keyring account.

```bash
clairveild tx privacy show-address \
  --from alice \
  --keyring-backend test \
  --output json
```

Main output:

| Field | Meaning |
| --- | --- |
| `from_address` | Transparent address used as the seed derivation base |
| `address` | Shareable full `clairs1...` shielded address |
| `derived_from` | `transparent-keyring-root` |

A sender needs the full `address` to send a private transfer.

### show-view-key

Displays the viewing key needed to scan incoming notes.

```bash
clairveild tx privacy show-view-key \
  --from alice \
  --keyring-backend test \
  --output json
```

Production wallets must not write viewing keys into plaintext logs or analytics.

### show-disclosure-pubkey

Displays a user disclosure public key for recipient-encrypted disclosure and sender self-view disclosure.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

This is not a V4 audit epoch key. Fresh V4 initialization consumes the public initial audit key and proof of possession from `--audit-config`; it does not derive them from an offline secret. Use this command only for user/self-view disclosure. Audit private keys remain external to node initialization.

### Audit-field V2 runtime configuration

Every V2 `tx privacy` command that prepares or proves a transaction obtains the network nonce, original initial height, active audit epoch/key, and circuit identity through the small audit configuration/key-history queries. The chain ID comes from the normal Cosmos client configuration or `--chain-id`. The removed `--audit-network-nonce-hex` and `--audit-initial-height` flags must not be supplied. Expiry and prover selection remain explicit operational controls:

```bash
--audit-expiry 30m --audit-prover-timeout 30m
```

Omit `--audit-prover-url` for local proving with the verified bundle, or explicitly select one trusted URL for remote proving. `transfer-batch-16x32` retains `--prover-url` as a V2 alias for `/v2/prover/audit-field`; it must not conflict with `--audit-prover-url`.

### Collect and verify audit provenance

`clairveil-auditor` reads `block` and `block_results` for a closed range, retains only original successful privacy transactions with their execution results in one atomic local JSON cache, verifies the original message proof against the configured circuit identity, decrypts with retained epoch keys, and builds deposit-rooted note lineage. It neither replays the chain nor uses PrivacyScan as an audit ledger.

```bash
clairveil-auditor \
  --chain-id reviewed-chain-1 \
  --node tcp://127.0.0.1:26657 \
  --from-height 100 --to-height 500 \
  --cache /secure/audit/range-100-500.json \
  --audit-keyring-file /secure/audit/keyring.json \
  --audit-artifacts /absolute/path/to/reviewed-artifacts
```

The keyring must be a `0600` version-1 JSON file containing `{key_id, secret_key}` lowercase 32-byte hex pairs. Configuration, nonce, initial height, public key history, and circuit identity are queried automatically. The report separates `collection_complete` from `provenance_complete`, and reports both `last_processed_block` and the last privacy execution position. A missing block/result, execution event, epoch key, decryptable envelope, or deposit root is `AUDIT_INCOMPLETE`; it is never emitted as an empty lineage or zero balance. Reusing the same cache resumes after the last atomically stored block, including empty blocks.

The report retains note linkage through `RootDeposits`, including zero-value inputs. `FundingRootDeposits` separately identifies funding origins propagated through positive inputs. For example, a positive output consuming A worth 10 and B worth 0 has linkage roots A/B but funding root A only. Zero-value outputs have no funding roots. `DepositFunder` retains deposit endpoint metadata, so an address on a zero deposit is not a principal provider. Missing zero-value deposit/input records still produce `AUDIT_INCOMPLETE`; an empty funding-root set does not excuse missing history.

The collector trusts the selected CometBFT RPC endpoint as its block/result source; it is not a light client. Deployments that need independently authenticated block history must provide that trust boundary outside this small collector. No replay input, runtime archive, or persistent audit server is created.

The stock `clairveil-auditor` does not wire an EVM-wrapper `sdk.TxDecoder` or `VerifyDelegatedExecution`, so it cannot authenticate delegated V2 deposits by itself. A downstream integration must supply both wrapper decoding and receipt-success verification; see [Downstream Cosmos Integration Guide §5.1](clairveil-downstream-cosmos-integration-guide.md#51-trusted-deposit-funding).

## 3. Deposit

Moves transparent coins into a shielded note.

```bash
clairveild tx privacy deposit 10uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Behavior:

1. Derives shielded spend/view keys from Alice's transparent keyring.
2. Binds amount and denom into the note commitment.
3. Sends transparent coins to the privacy module account.
4. Emits an encrypted note event.

Notes:

- Native V2 deposits and `DepositWithFunderV2` allow direct `0uclair` deposits. Zero skips only the actual bank transfer; funding endpoint validation, gas/fees, proof verification, note creation, and scanning remain required. Withdraw amounts must remain positive.
- A zero-value dummy note may be needed when the 2-input transfer planner splits one large note. With `--auto-dummy=true`, the CLI creates it through a one-input/two-output self batch transfer using an existing spendable positive note of the same denom. This preserves the shielded amount and adds a zero-value output; transaction fees still apply.
- The recorded development deposit used `2,868,008` gas. `2500000` ran out of gas (`code 11`), so this example uses `3500000`; downstream chains must set their own gas policy from measured execution.

## 4. Note Scan

Recovers your shielded wallet notes from chain events.

```bash
clairveild tx privacy list-notes \
  --from alice \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --json
```

Main flags:

| Flag | Meaning |
| --- | --- |
| `--json` | Print a machine-readable note list |
| `--rescan-wallet` | Clear local note cache and rescan from genesis |

The local wallet cache is written with restrictive permissions, but it does not replace production wallet encryption.

## 5. Transfer

The single transfer command handles user selective disclosure and mandatory audit disclosure together.

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 7uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Default behavior:

- The transfer itself remains private on-chain.
- Audit disclosure is always encrypted to the chain-configured audit key.
- Sender self-view disclosure is included by default and can be disabled with `--no-self-view`.
- User disclosure defaults to `all-private` / `none`.
- Recipient must be a full `clairs1...` shielded address.
- `--auto-dummy=true` is the default.
- The pre-proof summary prints the exact `chain id` and absolute `owner intent expires at unix`. The chain rejects at `block_time >= expires_at_unix`.

### Selective Disclosure Flags

| Flag | Values |
| --- | --- |
| `--privacy-policy` | `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to` |
| `--disclosure-mode` | `none`, `public`, `recipient-encrypted` |
| `--disclosure-pubkey` | Disclosure public key hex for recipient-encrypted mode |
| `--no-self-view` | Omit sender self-view disclosure |
| `--expires-in` | Owner-intent validity window in seconds; converted once to an absolute Unix expiry before signing/proving |

Public amount disclosure example:

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 7uclair \
  --privacy-policy amount \
  --disclosure-mode public \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 9000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Recipient-encrypted disclosure example:

```bash
clairveild tx privacy transfer "$(cat out/bob-shielded-address.txt)" 10uclair \
  --privacy-policy amount-from-to \
  --disclosure-mode recipient-encrypted \
  --disclosure-pubkey "$(cat out/bob-disclosure.hex)" \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 10000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

### transfer-batch

Broadcasts several independent `MsgTransfer` messages in one Cosmos tx envelope.

```bash
clairveild tx privacy transfer-batch "$(cat out/bob-shielded-address.txt)" \
  7uclair 8uclair 9uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 25000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Current limitations:

- Intended for bulk-transfer readiness and localnet capacity testing.
- `--privacy-policy`, `--disclosure-mode`, `--disclosure-pubkey`, and `--no-self-view` apply to the whole batch. Mixing different disclosure policies per item is not supported.
- Does not run the recursive split/merge planner.
- Each amount must already be satisfiable from spendable exact or pairable notes without reusing an input note inside the same batch.
- Zero-value dummy notes must already exist when a selected transfer input needs a dummy note.
- JSON output includes `txhash`, `height`, `code`, `message_count`, requested `amounts`, and per-message `items` evidence with nullifiers, output commitment, and disclosure digests.

### transfer-batch-16x32 and staged companions

`transfer-batch-16x32` runs one `MsgBatchTransfer` with one `BatchJoinSplit16x32` proof. Repeat `--payment 'shielded-address,coin[,policy,mode,target-key]'` for 1..32 independent payment policies, optionally pin 1..16 wallet notes with `--input-index`, and choose `--output-mode compact|exact32`. The command persists the private prepared payload and proof with mode `0600` before broadcast.

The restartable batch commands and `/v1/proofs/batch-transfer` endpoint are legacy-only. `--prover-url` is retained as a V2 alias for `/v2/prover/audit-field`; it never fails over or follows redirects. See the [legacy reference boundary](clairveil-getting-started.md#8-legacy-batchjoinsplit16x32-reference).

## 6. Disclosure Decode

Decrypts a transfer disclosure payload and produces a digest verification report.

Public disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-public.txhash)" \
  --disclosure-plane public \
  --node tcp://localhost:26657 \
  --report
```

Recipient disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane recipient \
  --from bob \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

Audit disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane audit \
  --from auditor \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

Sender self-view disclosure:

```bash
clairveild tx privacy decode-transfer-disclosure \
  --tx-hash "$(cat out/transfer-recipient.txhash)" \
  --disclosure-plane self-view \
  --from alice \
  --keyring-backend test \
  --node tcp://localhost:26657 \
  --report
```

Main flags:

| Flag | Meaning |
| --- | --- |
| `--tx-hash` | Find disclosure payload from tx events |
| `--disclosure-plane` | `auto`, `public`, `recipient`, `self-view`, `audit` |
| `--from` | Account used to derive a disclosure private key from keyring |
| `--disclosure-privkey` | Explicit disclosure private key scalar hex |
| `--report` | Print source, verification, summary, and payload as one JSON document |

`auto` tries candidate disclosure payloads from the tx event and selects the plane that decrypts and verifies with the current disclosure key.

If `verification.verified=true` is not present, the payload must not be shown to users as factual.

## 7. Withdraw

Sends a shielded note to a transparent recipient.

```bash
clairveild tx privacy withdraw 11uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

Withdraw currently uses exact-match notes. It does not create an output note or change note. If no spendable note exactly matches the requested amount, the planner tries to create one with a shielded self-transfer by default.

Before proving, the CLI prints the current `chain id` and absolute `spend intent expires at unix`. These, the recipient, amount, asset, root, and nullifier are owner-signed and proof-bound. `creator` remains the fee payer and may be replaced by a relayer.

Main flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--recipient` | sender address | transparent recipient |
| `--auto-plan` | `true` | create an exact-match note when missing |
| `--auto-dummy` | `true` | create a zero-value dummy via self batch transfer; requires a spendable positive note of the same denom |
| `--rescan-wallet` | `false` | reset local cache and rescan before note selection |

## 8. Relayed Withdraw

The user prepares a withdraw payload and a relayer submits it.

User:

```bash
clairveild tx privacy prepare-withdraw 7uclair \
  --recipient "$(cat out/alice-address.txt)" \
  --from bob \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --out out/withdraw-payload.json \
  --output json
```

Relayer:

```bash
clairveild tx privacy relay-withdraw out/withdraw-payload.json \
  --from relayer \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --gas 3500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

`prepare-withdraw` main flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--recipient` | sender address | transparent recipient |
| `--out` | empty | prepared payload file path |
| `--expires-in` | default expiry | payload validity window in seconds |
| `--auto-plan` | `true` | create an exact-match note automatically |
| `--auto-dummy` | `true` | create a zero-value dummy via self batch transfer; requires a spendable positive note of the same denom |

The summary prints the resolved absolute expiry and chain ID, and JSON uses the same `expires_at_unix`. Submission at or after that second fails; the relayer cannot extend it. Prepared payload/proof JSON is privacy-sensitive, and the prover payload still contains private note witness even though output/recipient/chain/expiry cannot be changed. Production wallets need encrypted storage and expiry/deletion policy.

Current CLI handoff versions are transfer payload `v5`, transfer proof/prover contract `v2`, withdraw prover/final payload and proof/prover/relay contract `v2`, and disclosure plaintext/query `privacy-fixed-v1`. Regenerate legacy files.

## 9. Query

Current direct CLI query wrappers:

```bash
clairveild query privacy check-nullifier <hex_nullifier> \
  --node tcp://localhost:26657

clairveild query privacy reserve uclair \
  --node tcp://localhost:26657
```

Other queries are available through gRPC/HTTP gateway and generated clients. V1 remains the wallet scan/tree/reserve/asset query surface; the live audit runtime configuration and epoch history are a separate V2 surface.

| Query | Method | HTTP path |
| --- | --- | --- |
| tree state | GET | `/clairveil/privacy/v1/tree_state` |
| nullifier | GET | `/clairveil/privacy/v1/nullifier/{nullifier}` |
| batch nullifiers | GET, POST | `/clairveil/privacy/v1/nullifiers` |
| commitment info | GET | `/clairveil/privacy/v1/commitment/{commitment_hex}` |
| events | GET | `/clairveil/privacy/v1/events` |
| scan events | GET | `/clairveil/privacy/v1/scan_events` |
| Merkle path | GET | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| disclosure config | GET | `/clairveil/privacy/v1/disclosure_config` |
| circuit config | GET | `/clairveil/privacy/v1/circuit_config` |
| reserve | GET | `/clairveil/privacy/v1/reserve/{denom=**}` |
| asset by denom | GET | `/clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}` |
| asset by ID | GET | `/clairveil/privacy/v1/assets/by_id/{asset_id_hex}` |
| typed privacy scan | POST | `/clairveil/privacy/v1/privacy_scan` |
| commitment paths at root | POST | `/clairveil/privacy/v1/commitment_paths_at_root` |

| V2 audit query | Method | HTTP path |
| --- | --- | --- |
| audit configuration | GET | `/clairveil/privacy/v2/audit/configuration` |
| audit key schedule | GET | `/clairveil/privacy/v2/audit/key_schedule` |
| audit key history entry | GET | `/clairveil/privacy/v2/audit/keys/{epoch}` |

## 10. Companion Binaries

### clairveil-setup

Generates the audit-field V2 artifact set and its identity-pinned manifest. The required acknowledgement keeps the bundle explicitly development-grade; generated R1CS/PK/VK binaries are not source artifacts and this command is not a formal trusted setup ceremony.

```bash
clairveil-setup --out artifacts/audit-field --development
```

The output directory must not already exist. Do not regenerate a reviewed release bundle: reuse its exact artifacts and matching runtime config.

### clairveild privacy configuration

Normal server start and export require the same small V4 configuration and reviewed verifier artifact directory; missing either input is an error.

```bash
clairveild start \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/audit-field-artifacts

clairveild export \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/audit-field-artifacts
```

### clairveil-verify (legacy only)

This binary exists only to inspect legacy ciphertext produced with the old SHA-256-of-address seed, raw Base64 ciphertext, and JSON `types.Note` format:

```bash
clairveil-verify -enc '<BASE64_LEGACY_CIPHERTEXT>' -secret '<LEGACY_ADDRESS_OR_SEED>'
```

It is incompatible with the current keyring-signature root seed and `privacy-fixed-v1` typed envelope. It also prints a derived scalar prefix and the decrypted plaintext, so never use it with production secrets or data. Validate current notes through `clairveild tx privacy list-notes`, the typed `privacy_scan` flow, and the conformance fixtures instead.

### clairveil-proverd

Runs the companion prover HTTP service.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=artifacts/audit-field
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd \
  -listen 127.0.0.1:8080 \
  -read-header-timeout 5s \
  -read-timeout 30s \
  -write-timeout 0s \
  -idle-timeout 2m \
  -max-request-bytes 8388608
```

Follow the remote production profile in [clairveil-operations-guide.md](clairveil-operations-guide.md#6-prover-operations).

`clairveil-proverd` serves only audit-field V2 and requires its artifact directory. It compares local VK/public-input schema hashes to the runtime `CircuitSetIdentity`; checksum env values cannot override it. Validators need VK only, while `clairveil-proverd` lazily loads R1CS/PK for proof generation. The bearer token check is enforced only when `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN` is configured, as in this production-oriented example. The bundle remains development-grade, and prover endpoint failover is off by default and requires explicit privacy opt-in.

### clairveil-payroll

Runs the reference payroll product workflow around local files and JSON reports.

```bash
clairveil-payroll validate -input payroll.json -out validation.json
clairveil-payroll build-input-from-notes -template payroll-template.json -notes alice-notes.json -out payroll.json
clairveil-payroll prepare-notes -input payroll.json -out note-preparation.json
clairveil-payroll plan -input payroll.json -out plan.json
clairveil-payroll run -plan plan.json -state .clairveil-payroll/reservation-state.json -out confirmed-plan.json
clairveil-payroll status -plan plan.json -out status.json
clairveil-payroll status -state .clairveil-payroll/reservation-state.json -out state-status.json
clairveil-payroll scan-evidence -plan plan.json -state .clairveil-payroll/reservation-state.json -tx-query tx-query.json -out scanned-evidence.json
clairveil-payroll scan-evidence -plan plan.json -state .clairveil-payroll/reservation-state.json -tx-query tx-query.json -apply -out scanned-and-reconciled.json
clairveil-payroll reconcile -state .clairveil-payroll/reservation-state.json -evidence evidence.json -out reconcile.json
clairveil-payroll settle-transfer-batch -plan plan.json -state .clairveil-payroll/reservation-state.json -tx transfer-batch.json -recipient-before bob-before.json -recipient-after bob-after.json -out settle.json
clairveil-payroll seed-localnet-notes -genesis home/config/genesis.json -wallet-home home -owner-address clair1... -shielded-address clairs1... -count 1000 -amount 1 -denom uclair -notes-out alice-notes.json -out seed-localnet-notes.json
clairveil-payroll export-report -plan plan.json -state .clairveil-payroll/reservation-state.json -out payroll-report.json
```

`build-input-from-notes` reads spendable notes from `list-notes --json` output and fills the payroll input `treasury_notes`. `scan-evidence` reads `clairveild query tx --output json` output or the equivalent TxObservation JSON, converts `shielded_transfer` events, output commitments, disclosure digests, and nullifier evidence into payroll reconcile evidence, and applies it to durable state when `-apply` is set. `settle-transfer-batch` verifies the actual `transfer-batch` tx result, per-message nullifier/output/disclosure evidence, and recipient note scan delta before settling the durable reservation state.

`seed-localnet-notes` is a localnet rehearsal helper. It writes payroll amount notes and zero dummy notes into localnet genesis commitments and the local wallet cache so large restart/retry rehearsals do not spend time preparing deposit txs. It is not a production note-preparation feature.

`prepare-notes` and `plan` also accept `-store-dir .clairveil-payroll` to write results into the file-backed reference artifact store. `run`, `scan-evidence`, `reconcile`, and `settle-transfer-batch` use the durable reservation state file. A compact runnable example is in the [reference payroll example](../examples/reference-payroll/README.md).

### clairveil-payrolld

Runs the scheduler/daemon surface for the reference payroll product.

```bash
clairveil-payrolld \
  -state .clairveil-payroll/reservation-state.json \
  -once \
  -out .clairveil-payroll/payrolld-report.json

clairveil-payrolld \
  -mode live \
  -state .clairveil-payroll/reservation-state.json \
  -plan .clairveil-payroll/payroll-plan.json \
  -tx-query .clairveil-payroll/tx-query.json \
  -interval 5s
```

`simulated` mode does not generate live proofs or broadcast chain transactions. Instead, it simulates proof-ready, submitted, and reconciled transitions against the durable reservation state so operators can exercise the full payroll workflow from this repo alone.

`live` mode is the long-running scheduler surface. The CLI reference implementation rereads the `-tx-query` file on every tick and reconciles `Submitted` or `Unknown` operations with tx event/nullifier evidence. Proof generation and broadcast are connected by injecting a production worker into the SDK `LiveOperationExecutor`, or by letting an external worker advance durable state to `Submitted`.

Run the complete demo with:

```bash
make reference-payroll-demo
```

Run the large-scale payroll rehearsal simulation with:

```bash
make reference-payroll-rehearsal
```

The rehearsal is a legacy simulation and rejects nonzero `RUN_LOCALNET`. There is no checked-in live payroll target for the current V2 runtime.

The Make targets above are the maintained runnable interfaces for the repository-local demo and legacy rehearsal simulation.

## 11. Batch Protocol Compatibility

The V2 circuit set generated and checked by the CLI is `privacy-note-v1-audit-field-v1`. Notes, disclosures, and encrypted envelopes use canonical `privacy-fixed-v1`; commands emit/consume the typed envelope rather than raw ciphertext or legacy JSON plaintext. `AssetRegistryV1` is authoritative for resolving canonical denoms and 32-byte asset IDs. On upgrade, use fresh genesis, delete local wallet/scan/proof caches and old development artifacts, reuse the reviewed matching artifacts, and rescan. There is no legacy decode or in-place state migration.

Wallet scan state is ordered by the complete cursor `(height, global_sequence, output_index)`. Any spend path must be obtained from a snapshot for exactly the selected root. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical root/path queries can reveal wallet interest, so retain the privacy warning and prefer local or privacy-preserving infrastructure when that matters.

The chain core and CLI now implement `BatchJoinSplit16x32`/`MsgBatchTransfer` through `transfer-batch-16x32` and its staged companions. The older `transfer-batch` still coordinates independent native 2x2 transfers and must not be presented as one 16x32 proof. The live core public schema remains, in order, `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`.

`clairveil-proverd` uses the role-aware lazy artifact registry and per-circuit admission defaults of one in-flight and four queued requests. `-max-request-bytes` defaults to `8388608` and must be greater than zero; `0` is invalid and does not disable the limit. Expose the production `proverservice.Handler`; the low-level raw transport handler has its own hard cap but not the service's auth, gzip dual-limit, health/readiness, and timeout policy. Automatic endpoint failover remains disabled. Cancellation may stop the caller while an in-process proof continues and retains its slot; operators needing hard cancellation or memory containment must isolate and terminate worker processes.
