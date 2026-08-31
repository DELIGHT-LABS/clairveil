# Clairveil Bulk Transfer Product Handoff

Korean version: [clairveil-bulk-transfer-product-handoff-kr.md](clairveil-bulk-transfer-product-handoff-kr.md)

This handoff defines the current product and operations work required to deploy
one-proof `BatchJoinSplit16x32` bulk transfer.

## Implemented Reference Surface

The repository provides:

- atomic reservation contract v3 and operation graph;
- 1..16-input / 1..32-output batch planning;
- one-proof preparation, proving, and broadcast;
- Cosmos `MsgBatchTransfer` and ClairveilJS 0.3.1 EVM
  `singleProofBatchTransfer` support;
- typed `privacy-scan-v2` output recovery;
- per-item disclosure and operation-evidence verification;
- bounded prover service and single-endpoint privacy defaults;
- SQLite/PostgreSQL reference persistence tests;
- restart/retry, lease-expiry, relay, and ambiguous-broadcast recovery;
- fixture and live localnet release gates.

## Product Integration Requirements

The product team must:

1. validate recipient, canonical amount/denom, disclosure policy, and encryption
   target for every row;
2. show total, change, input/output capacity, and atomicity before approval;
3. require explicit approval for multiple independent batches;
4. encrypt prepared payload/proof checkpoints;
5. keep all linked inputs reserved through proof, broadcast, and reconciliation;
6. report item success only from complete matching operation evidence;
7. present `OPERATION_STATE_MIXED` and `OPERATION_EVIDENCE_CONFLICT`
   details to authorized operators;
8. test the real wallet, prover, chain, and storage deployment.

## Backend And Operations Requirements

Deploy a managed transactional store with unique-active owner/nullifier
constraints, durable queues, idempotent leases, backup/restore, tenant
isolation, retention, and audit logging that excludes witness and disclosure
secrets.

Use one selected prover endpoint by default. Sending the same witness to another
endpoint requires explicit privacy opt-in naming every endpoint. Configure TLS,
authentication, body and queue limits, process isolation, monitoring, and
incident response.

## Release Gates

Run:

```bash
make reservation-sql-integration
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
make release-check
```

Downstream production acceptance additionally requires target-chain gas
calibration, staging load/fault tests, production database evidence, formal
trusted setup and audit, signed artifact provenance, and Cosmos/EVM product E2E.

See [Reference payroll product](clairveil-reference-payroll-product.md),
[rehearsal guide](clairveil-reference-payroll-rehearsal.md), and
[Batch localnet tutorial](clairveil-batch-joinsplit-localnet-tutorial.md).
