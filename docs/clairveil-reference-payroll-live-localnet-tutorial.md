# Clairveil Reference Payroll Live Localnet Tutorial

Korean version: [clairveil-reference-payroll-live-localnet-tutorial-kr.md](clairveil-reference-payroll-live-localnet-tutorial-kr.md)

This document explains how to run the payroll reference product end-to-end on an actual localnet.

This tutorial is not the simulated `clairveil-payrolld` flow. It uses real chain transactions.

```text
start localnet
-> deposit treasury shielded notes
-> scan with list-notes
-> build payroll input
-> validate / prepare-notes / plan / run
-> rerun run to verify idempotency
-> broadcast actual transfer-batch
-> scan recipient notes
-> settle payroll state from transfer-batch result
-> export final payroll report
```

## Quick Run

Run this from the repository root.

```bash
make reference-payroll-live-localnet
```

On success it prints output like this.

```text
Reference payroll live localnet tutorial passed.

Work dir:              tmp/reference-payroll-live-localnet
Payroll input:         tmp/reference-payroll-live-localnet/out/payroll-input.json
Payroll plan:          tmp/reference-payroll-live-localnet/out/payroll-plan.json
Reservation state:     tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json
Confirmed retry plan:  tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan-retry.json
Transfer batch chunks: 1
Rehearsal summary:    tmp/reference-payroll-live-localnet/out/rehearsal-summary.json
Final status:          tmp/reference-payroll-live-localnet/out/payroll-status-after-settle.json
Final payroll report:  tmp/reference-payroll-live-localnet/out/payroll-final-report.json
```

The default run pays `1uclair` each to 2 employees.

## Options

```bash
PAYROLL_ITEM_COUNT=3 PAYROLL_ITEM_AMOUNT=2 PAYROLL_CHUNK_SIZE=2 make reference-payroll-live-localnet
```

Important environment variables:

| env | Meaning |
| --- | --- |
| `CLAIRVEIL_PAYROLL_LIVE_WORK_DIR` | Tutorial output directory. Default: `tmp/reference-payroll-live-localnet` |
| `PAYROLL_ITEM_COUNT` | Payroll item count. Default: `2` |
| `PAYROLL_ITEM_AMOUNT` | Amount per item. Default: `1` |
| `PAYROLL_CHUNK_SIZE` | Payroll items per transfer-batch tx. Default: all items |
| `PAYROLL_SEED_NOTES` | If `1`, seed payroll notes into localnet genesis and Alice wallet cache. Default: `0` |
| `PAYROLL_TRANSFER_BATCH_GAS` | transfer-batch gas limit. Default: computed from chunk size |
| `GAS_PRICES` | localnet tx gas price. Default: `8500000000uclair` |
| `RPC_PORT`, `P2P_PORT`, `GRPC_PORT`, `API_PORT` | Avoid localnet port conflicts |
| `CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, `PAYROLL_BIN` | Use already-built binaries |

## Generated Files

Default outputs are written under `tmp/reference-payroll-live-localnet/out/`.

| File | Meaning |
| --- | --- |
| `bob-shielded-address.txt` | payroll recipient shielded address |
| `bob-notes-before.json` | recipient note scan before the whole run |
| `alice-notes.json` | Alice note scan, used as treasury inventory |
| `seed-localnet-notes.json` | localnet-only seeded note report when `PAYROLL_SEED_NOTES=1` |
| `payroll-template.json` | payroll template containing only employees and amounts |
| `payroll-input.json` | payroll input filled with treasury notes from `alice-notes.json` |
| `payroll-validation.json` | payroll input validation result |
| `payroll-note-preparation.json` | note preparation analysis |
| `payroll-plan.json` | draft payroll plan |
| `payroll-confirmed-plan.json` | plan with reservations confirmed in durable state |
| `payroll-confirmed-plan-retry.json` | idempotency result from rerunning the same plan |
| `payroll-reservation-state.json` | reservation/operation durable state |
| `payroll-transfer-batch-001.json` | first actual `transfer-batch` tx result |
| `payroll-transfer-batch-001-query.json` | first chain tx query result |
| `bob-notes-before-chunk-001.json` | recipient note scan before first chunk |
| `bob-notes-after-chunk-001.json` | recipient note scan after first chunk |
| `payroll-settle-report-001.json` | settle report from actual tx and recipient note delta |
| `bob-notes-after.json` | recipient note scan after all chunks |
| `payroll-status-after-settle.json` | reservation/operation status after settle |
| `payroll-final-report.json` | final payroll report |
| `rehearsal-summary.json` | item count, chunk count, and final success count summary |

## Step-by-Step Command Flow

`make reference-payroll-live-localnet` internally runs the following flow.

### 1. Prepare Localnet And Keys

The script creates a temporary home and builds `clairveild`, `clairveil-setup`, and `clairveil-payroll`.

```bash
go build -o tmp/reference-payroll-live-localnet/clairveild-payroll-live ./cmd/clairveild
go build -o tmp/reference-payroll-live-localnet/clairveil-setup-payroll-live ./cmd/clairveil-setup
go build -o tmp/reference-payroll-live-localnet/clairveil-payroll-live ./cmd/clairveil-payroll
```

It then creates `alice`, `bob`, and `auditor` keys, sets the genesis audit key, and starts localnet.

### 2. Check Recipient Shielded Address

```bash
clairveild tx privacy show-address \
  --from bob \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --output json
```

This address is used as the payroll item's `recipient_address`.

### 3. Prepare Treasury Notes

With the default `PAYROLL_SEED_NOTES=0`, the script prepares one amount note and one zero dummy note per employee using real deposit txs, matching the current 2-input transfer circuit.

```bash
clairveild tx privacy deposit 1uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 2500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

```bash
clairveild tx privacy deposit 0uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 2500000 \
  --gas-prices 8500000000uclair \
  --yes \
  --output json
```

For 1,000-item restart/retry rehearsals or other cases where deposit preparation is too slow, use localnet-only seed mode.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k-seeded \
PAYROLL_SEED_NOTES=1 \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

Seed mode uses `clairveil-payroll seed-localnet-notes` to write amount notes and zero dummy notes directly into localnet genesis commitments and Alice's wallet cache.

```bash
clairveil-payroll seed-localnet-notes \
  -genesis tmp/reference-payroll-live-localnet/home/config/genesis.json \
  -wallet-home tmp/reference-payroll-live-localnet/home \
  -owner-address "$(cat tmp/reference-payroll-live-localnet/out/alice-address.txt)" \
  -shielded-address "$(cat tmp/reference-payroll-live-localnet/out/alice-shielded-address.txt)" \
  -count 1000 \
  -amount 1 \
  -denom uclair \
  -notes-out tmp/reference-payroll-live-localnet/out/alice-notes.json \
  -out tmp/reference-payroll-live-localnet/out/seed-localnet-notes.json
```

This helper is only for localnet rehearsal setup. It does not replace production note preparation, staging/testnet deposits, or operator-approved treasury note preparation. Even in seed mode, payroll input generation, reservation confirmation, actual Groth16 proofs, actual `transfer-batch` broadcast, recipient scan, settle, and final report export still run.

### 4. Scan Treasury Notes

```bash
clairveild tx privacy list-notes \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet \
  --json > tmp/reference-payroll-live-localnet/out/alice-notes.json
```

### 5. Build Payroll Input

Fill the employee template with scanned treasury notes.

```bash
clairveil-payroll build-input-from-notes \
  -template tmp/reference-payroll-live-localnet/out/payroll-template.json \
  -notes tmp/reference-payroll-live-localnet/out/alice-notes.json \
  -owner-key-id alice \
  -lookup-key-id localnet-scan \
  -out tmp/reference-payroll-live-localnet/out/payroll-input.json
```

### 6. Confirm Payroll Plan And Reservation

```bash
clairveil-payroll validate \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-validation.json
```

```bash
clairveil-payroll prepare-notes \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-note-preparation.json
```

```bash
clairveil-payroll plan \
  -input tmp/reference-payroll-live-localnet/out/payroll-input.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-plan.json
```

```bash
clairveil-payroll run \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan.json
```

Rerunning the same plan must not create duplicate reservations. It should reuse existing state.

```bash
clairveil-payroll run \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-confirmed-plan-retry.json
```

### 7. Broadcast Actual transfer-batch

If chunk size is 2 and there are 2 items, the script creates one chunk. With more items, chunk labels increase as `001`, `002`, `003`, and so on.

```bash
clairveild tx privacy transfer-batch "$(cat tmp/reference-payroll-live-localnet/out/bob-shielded-address.txt)" \
  1uclair 1uclair \
  --from alice \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-local-1 \
  --gas 21000000 \
  --gas-prices 8500000000uclair \
  --yes \
  --rescan-wallet \
  --output json > tmp/reference-payroll-live-localnet/out/payroll-transfer-batch-001.json
```

This step creates real Groth16 proofs and broadcasts a real tx to localnet.

### 8. Scan Recipient Notes

```bash
clairveild tx privacy list-notes \
  --from bob \
  --keyring-backend test \
  --home tmp/reference-payroll-live-localnet/home \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet \
  --json > tmp/reference-payroll-live-localnet/out/bob-notes-after-chunk-001.json
```

### 9. Settle Payroll State

```bash
clairveil-payroll settle-transfer-batch \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -tx tmp/reference-payroll-live-localnet/out/payroll-transfer-batch-001.json \
  -recipient-before tmp/reference-payroll-live-localnet/out/bob-notes-before-chunk-001.json \
  -recipient-after tmp/reference-payroll-live-localnet/out/bob-notes-after-chunk-001.json \
  -item-start 0 \
  -item-limit 2 \
  -out tmp/reference-payroll-live-localnet/out/payroll-settle-report-001.json
```

`settle-transfer-batch` checks:

- tx `code` is `0`
- tx `message_count` equals the selected payroll item count
- tx amount list equals the selected payroll item amount list
- tx item evidence includes the selected input nullifiers, output commitment, and audit disclosure digest
- recipient scan result gained enough spendable notes from the same `txhash` for each payment amount
- `-item-start` and `-item-limit` identify the plan slice matched to the tx

After verification, durable reservation state is updated to `ConfirmedSpent` and operation state to `Succeeded`.

### 10. Check Final Report

```bash
clairveil-payroll status \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-status-after-settle.json
```

```bash
clairveil-payroll export-report \
  -plan tmp/reference-payroll-live-localnet/out/payroll-plan.json \
  -state tmp/reference-payroll-live-localnet/out/payroll-reservation-state.json \
  -out tmp/reference-payroll-live-localnet/out/payroll-final-report.json
```

Expected successful status:

```json
{
  "reservations_by_status": {
    "ConfirmedSpent": 4
  },
  "operations_by_status": {
    "Succeeded": 2
  }
}
```

Expected final report:

```json
{
  "status": "Confirmed",
  "summary": {
    "TotalItems": 2,
    "ConfirmedItems": 2
  }
}
```

## Current Limits

This tutorial uses real chain txs and recipient note scans. It is still not a full production scanner.

Current `settle-transfer-batch` success judgment uses:

- actual `transfer-batch` tx success
- tx message count and amount list matching the payroll plan
- tx output evidence tying selected reservation nullifiers to each selected payroll item
- recipient spendable notes from the transfer tx increasing by the expected payment amount counts

Remaining production-grade work:

- tx event/nullifier scanner is provided by `clairveil-payroll scan-evidence` and SDK `EvidenceScanner`; production deployments should feed scanner output from chain tx queries instead of relying only on command-local tx metadata
- production daemon should connect `scan-evidence` or an equivalent scanner as a long-running worker
- when many payroll items have the same amount, recipient note delta should be matched to operation items more strongly
- `PAYROLL_SEED_NOTES=1` is a localnet-only rehearsal helper and must not be used as evidence for production note preparation performance
- staging/testnet should run the same runbook and preserve restart/retry plus scanner evidence artifacts as release outputs
