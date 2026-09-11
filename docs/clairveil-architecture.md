# Clairveil Architecture

> Korean version: [clairveil-architecture-kr.md](clairveil-architecture-kr.md)

This document explains the current repository boundaries and data flow. Normative wire fields and hashes remain in `proto/clairveil/privacy/v1`, the circuit contract, schemas, and conformance fixtures linked from the [documentation index](README.md).

## 1. System Boundary

```text
wallet / Go SDK / payroll
        | queries, signed owner intent, prepared proof requests
        v
downstream Cosmos app or clairveild ---- optional HTTP ----> clairveil-proverd
        |                                                   |
        | Msg/Query                                         | R1CS + PK
        v                                                   v
     x/privacy -------------------------------------- ZK artifact set
        |
        +-- consensus verification and atomic state transition
        +-- Merkle/nullifier/reserve/asset/scan/audit state
        +-- bank keeper transfers to/from the privacy module account
```

`clairveild` is a standalone reference host. A downstream production chain imports and wires `x/privacy`; it still owns chain configuration, validator operations, artifact distribution, audit-key custody, wallet security, and any EVM/policy integration.

## 2. Repository Map

| Path | Responsibility |
| --- | --- |
| `app/` | Reference Cosmos SDK application and keeper/module wiring |
| `cmd/clairveild` | Daemon root command and reference chain process |
| `x/privacy/types` | Messages, queries, genesis/state contracts, keys, fixed encodings |
| `x/privacy/keeper` | Proof checks, bank transitions, Merkle/nullifier/reserve/asset/scan state |
| `x/privacy/circuit` | Deposit, Spend, JoinSplit2x2, and BatchJoinSplit16x32 circuits |
| `x/privacy/zk` | Artifact manifest, identity, setup, preflight, and proof registry |
| `x/privacy/client/cli` | Wallet/operator tx and direct query commands |
| `x/privacy/client/sdk` | Go wallet, prepared-payload, scanner, prover transport, and conformance helpers |
| `cmd/clairveil-proverd` | Bounded reference HTTP prover service |
| `cmd/clairveil-payroll*` | Reference payroll control plane and daemon |
| `proto/clairveil/privacy/v1` | Public Msg, Query, and genesis wire API |
| `scripts/` | Setup, localnet/e2e, evidence, benchmark, and release automation |
| `docs/`, `tmpdocs/` | Current public knowledge and ignored plans/archives/drafts |

## 3. Consensus State

`x/privacy` has Cosmos module consensus version 2. Its KV state includes:

- used nullifiers and commitment indexes;
- current Merkle nodes, historical roots, and root snapshots;
- audit key identity/epoch and disclosure configuration;
- privacy event sequence and typed scan summaries/outputs;
- per-denom deposit/withdraw reserve accounting;
- asset registry entries by denom and asset ID;
- the exact consensus circuit-set identity.

Deposit and withdraw move transparent coins through the privacy module account using the bank keeper. Reserve counters are checked against the module-account balance through the reserve query; direct top-ups or unrelated bank movement can make the reported invariant fail.

## 4. Transaction Data Flow

### Deposit

The wallet derives a shielded recipient, creates a note commitment and deposit proof, then broadcasts `MsgDeposit`. The keeper verifies the proof/asset binding, moves transparent funds into the module account, appends the commitment, updates reserve/scan state, and emits the encrypted note event.

### JoinSplit2x2 transfer

The wallet scans and selects notes, queries Merkle paths, fixes the chain/expiry/output/disclosure intent, signs the canonical payload, and proves locally or through one explicitly selected prover. `MsgTransfer` verification rejects used nullifiers, invalid roots/expiry/signature/proof, or disclosure contract violations before atomically consuming inputs and appending outputs.

### BatchJoinSplit16x32 transfer

The batch reference integration flow prepares 1..16 inputs and 1..32 payment/change/padding outputs, then produces one proof and one `MsgBatchTransfer`. `transfer-batch-16x32` is this flow. The legacy `transfer-batch` command instead puts multiple independent `MsgTransfer` messages in one Cosmos transaction and is not the batch circuit protocol.

### Withdraw

The owner binds a transparent recipient, chain, expiry, nullifier, and proof in the prepared payload. A relayer may replace the Cosmos `creator`, but cannot change owner-bound values. Successful execution consumes the note and moves the transparent amount from the module account to the recipient.

All keeper transitions must be atomic. A duplicate/conflicting nullifier, proof error, gas failure, or mid-transition error must leave no partial commitments, scan records, reserve deltas, or bank movement.

## 5. Proving And Artifact Boundary

The current runnable set is the development-only `privacy-note-v1-audit-field-v1` identity. It has four ordered audit-field circuits and exact matching local artifacts. Validators require matching VKs; `clairveil-proverd` additionally loads its selected R1CS/PK. Checksum environment variables are preflight input only and cannot override consensus. The earlier `privacy-note-v1` NoteV1/batch set is retained legacy material, not the current V2 runtime.

Prepared prover requests contain private note witness. Same-endpoint retry does not imply permission to fail over to a second prover. Multi-prover failover must be an explicit privacy decision. `clairveil-proverd` is a bounded reference implementation, not a production trust boundary by itself.

## 6. Client And Audit Boundary

### 6.1 Audit-field V2 Proving Boundary

The only current remote route is `POST /v2/prover/audit-field`. Its request/response envelope `v1` carries `privacy-note-v1-audit-field-v1`, an exact artifact hash, and final PI23 as base64 `[]byte` values plus a complete witness. The response repeats all binding fields; the client performs local verification against the exact artifact identity and final PI23 before assembling a V2 message. See the [HTTP API](clairveil-proverd-http-api.md); `/v1` deposit/transfer/withdraw/batch descriptions are retained legacy history only.

Wallet ownership is recovered by scanning typed chain data and attempting note decryption; `view_tags` are only untrusted performance hints. Clients must persist cursors, support rescan, keep prepared payloads and note caches encrypted, and treat nullifier queries as privacy-sensitive.

Every transfer carries mandatory audit disclosure. User-selected disclosure and sender self-view disclosure are distinct envelopes. On-chain validation verifies the frozen digest/envelope contract; custody and authorization for the audit private key remain external operational responsibilities.

## 7. Compatibility And Authority

The current fixed client contract is `/v2/prover/audit-field` with request/response envelope `v1`, `privacy-note-v1-audit-field-v1`, base64 byte slices, and final PI23 local verification. Earlier NoteV1 artifacts, queued proofs, cached prepared payloads, note/scan caches, and legacy genesis are not compatible with the V2 runtime. Preserve their fixtures as historical conformance evidence; do not submit them to V2.

When sources disagree, resolve them in this order for the affected contract:

1. compiled proto/message/query definitions and keeper validation for executable behavior;
2. normative circuit contract, schemas, and conformance fixtures for frozen cross-language encoding;
3. CLI help and the CLI reference for command surfaces;
4. current guides for integration rationale and operational context.

Release file membership is defined only by `scripts/release-pack-paths.txt` and `scripts/release-pack-required-files.txt`. See the [maintainer instructions](../CONTRIBUTING.md).
