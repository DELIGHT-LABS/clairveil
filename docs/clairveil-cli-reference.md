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

Displays the public key used to receive recipient-encrypted disclosure or audit disclosure.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

This value is used for the genesis audit master pubkey or as a user disclosure recipient key.

## 3. Deposit

Moves transparent coins into a shielded note.

```bash
clairveild tx privacy deposit 10uclair \
  --from alice \
  --keyring-backend test \
  --chain-id clairveil-local-1 \
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
- User disclosure defaults to `all-private` / `none`.
- Recipient must be a full `clairs1...` shielded address.
- `--auto-dummy=true` is the default.

### Selective Disclosure Flags

| Flag | Values |
| --- | --- |
| `--privacy-policy` | `all-private`, `amount`, `to`, `amount-to`, `from`, `amount-from`, `from-to`, `amount-from-to` |
| `--disclosure-mode` | `none`, `public`, `recipient-encrypted` |
| `--disclosure-pubkey` | Disclosure public key hex for recipient-encrypted mode |

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

Main flags:

| Flag | Meaning |
| --- | --- |
| `--tx-hash` | Find disclosure payload from tx events |
| `--disclosure-plane` | `auto`, `public`, `recipient`, `audit` |
| `--from` | Account used to derive a disclosure private key from keyring |
| `--disclosure-privkey` | Explicit disclosure private key scalar hex |
| `--report` | Print source, verification, summary, and payload as one JSON document |

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

Withdraw currently uses exact-match notes. If no spendable note exactly matches the requested amount, the planner tries to create one with a shielded self-transfer by default.

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

Prepared payload JSON is privacy-sensitive data. Production wallets need encrypted storage and expiry/deletion policy.

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
| commitment info | `/clairveil/privacy/v1/commitment/{commitment_hex}` |
| events | `/clairveil/privacy/v1/events` |
| Merkle path | `/clairveil/privacy/v1/merkle_path/{commitment_hex}` |
| audit config | `/clairveil/privacy/v1/audit_config` |
| disclosure config | `/clairveil/privacy/v1/disclosure_config` |
| circuit config | `/clairveil/privacy/v1/circuit_config` |

## 10. Companion Binaries

### clairveil-setup

Generates ZK artifacts.

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
