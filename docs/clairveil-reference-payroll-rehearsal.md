# Clairveil Reference Payroll Large-Scale Rehearsal Guide

Korean version: [clairveil-reference-payroll-rehearsal-kr.md](clairveil-reference-payroll-rehearsal-kr.md)

## Purpose

This document separates the current one-proof BatchJoinSplit16x32 rehearsal from the legacy phase 1 multi-message capacity model and its dated localnet evidence.

The current protocol/reference path is `BatchJoinSplit16x32` / `MsgBatchTransfer`: one atomic operation consumes 1..16 notes and creates 1..32 payment/change/padding outputs with one proof. The older `make reference-payroll-rehearsal` model still calculates one proof per recipient and legacy multi-message transaction envelopes. Preserve that output as comparison evidence; do not use it as the current one-proof capacity model.

## Current One-Proof Batch Payroll Gate

Run the conformance/static gate by default and opt into the actual node/prover/payroll workflow when the required local resources and development artifacts are available.

```bash
go test ./x/privacy/client/sdk/conformance -run TestBatchTransferContract -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

The live path verifies one proof and one transaction envelope for the exercised payroll batch, the many-input/one-operation/many-item payroll graph, separate batch/item evidence, actual process and node restart, exact stored-byte retry, tx-hash-first reconciliation, spent-nullifier conflict handling, typed disclosure/decryption, and the default no-cross-endpoint-failover privacy boundary. See [clairveil-batch-transfer-integration-handoff.md](clairveil-batch-transfer-integration-handoff.md) and [clairveil-batch-joinsplit-localnet-tutorial.md](clairveil-batch-joinsplit-localnet-tutorial.md).

This is an experimental reference gate, not production approval. Formal trusted setup, external audit, signed production artifact distribution, and a production-scale rehearsal remain separate work.

## Legacy Phase 1 Simulation Command

This target does not submit all 100,000 payments as actual chain transactions. It repeatedly calculates legacy proof, transaction-envelope, chunk, and completion-time estimates from fixed input profiles and can optionally run the small legacy localnet smoke below.

Default legacy comparison run:

```bash
make reference-payroll-rehearsal
```

By default, the results are generated in the following locations.

```text
benchmarks/reference-payroll-rehearsal/
  rehearsal-summary.json
  latest-rehearsal-summary.json
  scenarios/
    single-company-1k.json
    single-company-10k.json
    single-company-100k.json
    hundred-companies-1k.json
```

`benchmarks/` contains runtime artifacts and is not included in Git.

## Legacy Default Scenarios

The script runs the following four scenarios.

| scenario | meaning |
| --- | --- |
| `single-company-1k` | One company pays 1,000 people once per month. |
| `single-company-10k` | One company pays 10,000 people once per month. |
| `single-company-100k` | One company pays 100,000 people once per month. |
| `hundred-companies-1k` | 100 companies each pay 1,000 people, creating a total peak of 100,000 items. |

The legacy profile is as follows.

```text
BULK_CHUNK_SIZE=20
BULK_PROVER_UNITS=1
BULK_PROOFS_PER_SEC=6.92638
BULK_TX_PER_SEC=1
```

`BULK_CHUNK_SIZE=20` is the legacy phase 1 multi-message baseline of 20 independent `MsgTransfer` messages in one transaction envelope. Increasing this value reduces the number of legacy envelopes, but may increase the risk of exceeding transaction size or gas limits. It does not configure `BatchJoinSplit16x32` output capacity.

## Legacy Prover Horizontal-Scaling Profile

To run a rehearsal that models 8 prover units, use the following command.

```bash
BULK_PROVER_UNITS=8 make reference-payroll-rehearsal
```

To determine whether proof generation or transaction submission is the bottleneck, compare the following values for each scenario in `latest-rehearsal-summary.json`.

- `proof_count`
- `tx_envelope_count`
- `estimated_total_seconds`
- `payroll_items_per_sec`

In the legacy phase 1 model, each recipient requires one proof, so payroll for 100,000 recipients requires 100,000 proofs. Adding prover units reduces that legacy proof bottleneck, but the transaction-envelope and scanner/reconcile workloads remain. This statement does not describe the current one-proof batch graph.

## Optional Legacy Multi-Message Localnet Smoke Test

To run a small live localnet payroll flow together with the simulation, use the following command.

```bash
RUN_LOCALNET=1 LOCALNET_PAYROLL_ITEM_COUNT=2 make reference-payroll-rehearsal
```

This option runs `scripts/reference-payroll-live-localnet.sh` internally. On the localnet, it verifies the legacy path through treasury deposit, note scan, payroll planning/reservation, an actual multi-message `transfer-batch`, recipient note scan, settlement, and final report export. It is independent from the current one-proof batch reference integration gate above.

Increasing `LOCALNET_PAYROLL_ITEM_COUNT` lengthens the localnet run and increases proof/transaction costs. For the default rehearsal, use simulation to validate large numbers and a small smoke test to verify the chain path. For a restart/retry rehearsal of 1,000 or more items, use the seed mode described below.

## Historical Legacy 1,000-Item Localnet Restart/Retry Rehearsal — 2026-07-08

The following command and results are retained from the 2026-07-08 repository-local legacy chain rehearsal. The command remains useful for regression, but the dated result is not current one-proof capacity evidence.

```bash
CLAIRVEIL_PAYROLL_LIVE_WORK_DIR=tmp/reference-payroll-live-localnet-1k \
PAYROLL_SEED_NOTES=1 \
PAYROLL_ITEM_COUNT=1000 \
PAYROLL_CHUNK_SIZE=20 \
GAS_PRICES=0uclair \
make reference-payroll-live-localnet
```

The 2026-07-08 rehearsal verified the following:

- It prepares localnet-only seeded treasury notes and a wallet cache for 1,000 employees.
- Running `clairveil-payroll run` twice with the same plan resumes idempotently without duplicate reservations.
- It submits 50 actual localnet `transfer-batch` chunks of 20 items each.
- Each `transfer-batch` generates an actual Groth16 proof and is included in an actual chain transaction.
- Each chunk uses `settle-transfer-batch -item-start -item-limit` to settle only the corresponding segment of the plan.
- In the final `payroll-status-after-settle.json`, all 1,000 operations are `Succeeded` and all 2,000 input reservations are `ConfirmedSpent`.

`GAS_PRICES=0uclair` is a local-only setting that prevents the fees for the 1,000-item localnet rehearsal from exceeding the genesis account balance. Run the rehearsal again under the actual fee policy on staging/testnet.

`PAYROLL_SEED_NOTES=1` is a rehearsal helper that preloads localnet genesis commitments and the Alice wallet cache with payroll amount notes and zero-valued dummy notes. This option eliminates the preparation time for 2,000 deposit transactions, but the subsequent payroll input generation, reservation confirmation, transfer proof generation, transaction broadcast, recipient scan, and settlement paths still run for real. This option is not a production note-preparation method.

On success, the primary artifacts are as follows.

```text
tmp/reference-payroll-live-localnet-1k/out/rehearsal-summary.json
tmp/reference-payroll-live-localnet-1k/out/seed-localnet-notes.json
tmp/reference-payroll-live-localnet-1k/out/payroll-status-after-settle.json
tmp/reference-payroll-live-localnet-1k/out/payroll-final-report.json
tmp/reference-payroll-live-localnet-1k/out/payroll-confirmed-plan-retry.json
tmp/reference-payroll-live-localnet-1k/out/payroll-settle-report-001.json ... payroll-settle-report-050.json
```

As of 2026-07-08, the successful seeded 1,000-item localnet run recorded `confirmed_items=1000`, `succeeded_operations=1000`, `confirmed_spent_reservations=2000`, and `chunk_count=50`, with a wall-clock time of approximately 8 minutes 57 seconds.

The purpose of this legacy 1,000-item localnet run was to verify the restart/retry invariant and durable control plane during development. For 10,000 items, 100,000 items, and concurrent peaks across multiple tenants, retain current one-proof staging/testnet evidence and a capacity report that covers the batch reference integration rather than extrapolating this legacy run.

The successful actual 1,000-item localnet result and small multi-chunk smoke result from 2026-07-08 are recorded in [clairveil-reference-payroll-localnet-rehearsal-result.md](clairveil-reference-payroll-localnet-rehearsal-result.md).

## Interpreting the Legacy Results

The legacy `latest-rehearsal-summary.json` has the following structure.

```json
{
  "schema_version": "clairveil.reference_payroll_rehearsal.v1",
  "profile": {
    "chunk_size": 20,
    "prover_units": 1,
    "proofs_per_sec_per_unit": 6.92638,
    "tx_per_sec": 1
  },
  "scenarios": [
    {
      "name": "single-company-100k",
      "recipient_count": 100000,
      "proof_count": 100000,
      "tx_envelope_count": 5000,
      "estimated_total_seconds": 14437.5639,
      "estimated_total_hours": 4.0104
    }
  ]
}
```

`single-company-100k` and `hundred-companies-1k` have the same total number of payments, but different operational implications.

- With `single-company-100k`, one tenant's proofs, note preparation, and scanner results are concentrated in a single run.
- With `hundred-companies-1k`, the total volume is the same, but scheduling, rate limits, and retry windows can be distributed by tenant.

## Current One-Proof Interpretation and Remaining Scaling Decisions

`BatchJoinSplit16x32` and the payroll SDK provided by the batch integration are already implemented and must not be described as future protocol work under an obsolete phase label. Interpret rehearsal evidence as follows:

- The current proof count is one per atomic batch operation, not one per recipient. Each operation has 1..16 inputs and 1..32 total outputs; change and padding reduce the number of payment outputs available in that operation.
- Legacy `proof_count`, `tx_envelope_count`, and completion estimates in this document are comparison values and cannot support a current one-proof capacity claim.
- A current capacity claim must measure the actual distribution of shapes used by the batch integration, prover latency and memory, transaction gas/inclusion, typed scan, disclosure verification, reconciliation, retry, and tenant scheduling under staging/testnet load.
- If evidence shows that the frozen 16/32 shape is insufficient, any new circuit/protocol shape requires a separate roadmap, security review, circuit/keeper/SDK contract, and migration plan. It is not an unimplemented name already implied by this document.

## Related Commands

To run the current one-proof conformance and localnet gates, use:

```bash
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

To run a single legacy simulation directly, use the following command.

```bash
go run ./cmd/clairveil-bulktransferbench \
  -scenario single-company-100k \
  -recipients 100000 \
  -chunk-size 20 \
  -prover-units 8 \
  -proofs-per-sec 6.92638 \
  -tx-per-sec 1 \
  -out benchmarks/reference-payroll-rehearsal/scenarios/single-company-100k-prover8.json
```

To include the legacy bulk readiness checks, use the following command. This does not replace the current one-proof live gate.

```bash
make privacy-bulk-readiness-check
```
