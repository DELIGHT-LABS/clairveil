# Reference Payroll Demo

This example provides a minimal payroll input that lets operators exercise the reference payroll product flow from this repo alone.

Korean version: [README-kr.md](README-kr.md)

## Run

From the repo root:

```bash
make reference-payroll-demo
```

Or choose an output directory:

```bash
OUT_DIR=tmp/my-payroll-demo ./scripts/reference-payroll-demo.sh
```

## Flow

The script runs:

```text
validate
prepare-notes
plan
run
status
clairveil-payrolld -once
status
export-report -state
```

This demo uses `clairveil-payrolld` in its default `simulated` mode. In this mode it does not generate live proofs or broadcast chain transactions. It simulates proof-ready, submitted, and reconciled transitions against the durable reservation state.

`clairveil-payrolld` also has a `live` mode for the long-running scheduler surface. The CLI reference live mode reconciles submitted/unknown operations from tx evidence files; production proof generation and broadcast are connected through the SDK live executor or an external worker.

## Outputs

Default outputs are written under `tmp/reference-payroll-demo/`.

| File | Meaning |
| --- | --- |
| `validation.json` | payroll input validation result |
| `note-preparation.json` | note preparation status and operation hints |
| `plan.json` | draft payroll plan |
| `confirmed-plan.json` | plan with confirmed reservations |
| `reservation-state.json` | durable reservation/operation state |
| `payrolld-report.json` | simulated daemon tick report |
| `status-after-daemon.json` | state summary after daemon execution |
| `final-report.json` | final item-level payroll report |

Successful run:

```text
status-after-daemon.json:
  reservations_by_status.ConfirmedSpent = all reservations
  operations_by_status.Succeeded = all operations

final-report.json:
  status = Confirmed
```
