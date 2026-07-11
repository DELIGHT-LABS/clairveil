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

Displays the public key used for recipient-encrypted disclosure, sender self-view disclosure, and audit disclosure.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

This value is used for the genesis audit master pubkey, as a user disclosure recipient key, or to confirm the sender self-view disclosure key.

## 3. Deposit

Moves transparent coins into a shielded note.

```bash
clairveild tx privacy deposit 10uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
  --expires-in 1800 \
  --gas 2500000 \
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

- `0uclair` deposit can be used to prepare a dummy note.
- A dummy note may be needed when the 2-input transfer planner has to split one large note.

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
| `--auto-dummy` | `true` | prepare a zero-value dummy note when the planner needs it |
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
| `--auto-dummy` | `true` | prepare a dummy note automatically |

The summary prints the resolved absolute expiry and chain ID, and JSON uses the same `expires_at_unix`. Submission at or after that second fails; the relayer cannot extend it. Prepared payload/proof JSON is privacy-sensitive, and the prover payload still contains private note witness even though output/recipient/chain/expiry cannot be changed. Production wallets need encrypted storage and expiry/deletion policy.

Current CLI handoff versions are transfer payload `v5`, transfer proof/prover contract `v2`, withdraw prover/final payload and proof/prover/relay contract `v2`, and disclosure plaintext/query `privacy-fixed-v1`. Regenerate legacy files.

## 9. Query

Current direct CLI query wrapper:

```bash
clairveild query privacy check-nullifier <hex_nullifier> \
  --node tcp://localhost:26657
```

Other queries are available through gRPC/HTTP gateway and generated clients.

| Query | HTTP path |
| --- | --- |
| tree state | `/clairveil/privacy/v1/tree_state` |
| nullifier | `/clairveil/privacy/v1/nullifier/{nullifier}` |
| batch nullifiers (GET) | `/clairveil/privacy/v1/nullifiers` |
| batch nullifiers (POST) | `/clairveil/privacy/v1/nullifiers` |
| commitment info | `/clairveil/privacy/v1/commitment/{commitment_hex}` |
| events | `/clairveil/privacy/v1/events` |
| scan events | `/clairveil/privacy/v1/scan_events` |
| Merkle path | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| audit config | `/clairveil/privacy/v1/audit_config` |
| disclosure config | `/clairveil/privacy/v1/disclosure_config` |
| circuit config | `/clairveil/privacy/v1/circuit_config` |
| reserve | `/clairveil/privacy/v1/reserve/{denom}` |

## 10. Companion Binaries

### clairveil-setup

Generates development ZK artifacts for active set `privacy-note-v1` and manifest schema `v2`. Generated R1CS/PK/VK binaries are not source artifacts and this command is not a formal trusted setup ceremony.

```bash
clairveil-setup --out artifacts/privacy
clairveil-setup --out artifacts/privacy --overwrite
```

### clairveil-proverd

Runs the companion prover HTTP service.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=artifacts/privacy
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
export CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN="$(openssl rand -hex 32)"

clairveil-proverd \
  -listen 127.0.0.1:8080 \
  -read-header-timeout 5s \
  -read-timeout 30s \
  -write-timeout 0s \
  -idle-timeout 2m \
  -max-request-bytes 8388608
```

Follow the remote production profile in [clairveil-proverd-remote-production-profile.md](clairveil-proverd-remote-production-profile.md).

Validator startup compares the local VK hashes/public-input schema hashes to consensus `CircuitSetIdentity` schema `v1`; checksum env values cannot override it. Validators need VK only, while `clairveil-proverd` lazily loads R1CS/PK for proof generation. Prover endpoint failover is off by default and requires explicit privacy opt-in.

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

`prepare-notes` and `plan` also accept `-store-dir .clairveil-payroll` to write results into the file-backed reference artifact store. `run`, `scan-evidence`, `reconcile`, and `settle-transfer-batch` use the durable reservation state file. For the detailed workflow, see [clairveil-reference-payroll-product.md](clairveil-reference-payroll-product.md).

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

Run the live localnet payroll tutorial with:

```bash
make reference-payroll-live-localnet
```

Run the large-scale payroll rehearsal simulation with:

```bash
make reference-payroll-rehearsal
```

The live localnet walkthrough is [clairveil-reference-payroll-live-localnet-tutorial.md](clairveil-reference-payroll-live-localnet-tutorial.md). The rehearsal walkthrough is currently documented in Korean at [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md).

## 11. Session 2 Foundation Compatibility

The active circuit set generated and checked by the CLI is `privacy-note-v1`. Notes, disclosures, and encrypted envelopes use canonical `privacy-fixed-v1`; commands emit/consume the typed envelope rather than raw ciphertext or legacy JSON plaintext. `AssetRegistryV1` is authoritative for resolving canonical denoms and 32-byte asset IDs. On upgrade, use fresh genesis, delete local wallet/scan/proof caches and old development artifacts, regenerate artifacts, and rescan. There is no legacy decode or in-place state migration.

Wallet scan state is ordered by the complete cursor `(height, global_sequence, output_index)`. Any spend path must be obtained from a snapshot for exactly the selected root. Current-root paths use incremental nodes and do not consume the online historical-rebuild budget. A non-current historical path requires persisted root/count/height metadata; the public query admits at most 1,024 leaves and two concurrent rebuilds per keeper, otherwise it returns `ResourceExhausted`. Use the current root or a trusted local historical index above that online bound. The separate offline recovery/export bound remains `MaxMerkleRebuildLeaves` (1,048,576). Remote historical root/path queries can reveal wallet interest, so retain the privacy warning and prefer local or privacy-preserving infrastructure when that matters.

`BatchJoinSplit16x32` remains a Session 2 feasibility prototype. No CLI command in this reference submits a production 16x32 message. In particular, `transfer-batch` still coordinates current native 2x2 transfers. The future public schema is reserved, in order, as `MerkleRoot`, `ChainDomainHi`, `ChainDomainLo`, `ExpiresAtUnix`, `InputCount`, `OutputCount`, `NullifierRoot`, `CommitmentRoot`, `UserDisclosureRoot`, `FullDisclosureRoot`, `PayloadDigestHi`, `PayloadDigestLo`; it must not be treated as a live circuit or transaction.

`clairveil-proverd` uses the role-aware lazy artifact registry and per-circuit admission defaults of one in-flight and four queued requests. `-max-request-bytes` defaults to `8388608` and must be greater than zero; `0` is invalid and does not disable the limit. Expose only the bounded `proverservice.Handler`, never the raw transport handler. Automatic endpoint failover remains disabled. Cancellation may stop the caller while an in-process proof continues and retains its slot; operators needing hard cancellation or memory containment must isolate and terminate worker processes.
