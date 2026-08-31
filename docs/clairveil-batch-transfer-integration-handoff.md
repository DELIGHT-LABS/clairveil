# BatchJoinSplit16x32 Client Integration Handoff

Korean version: [clairveil-batch-transfer-integration-handoff-kr.md](clairveil-batch-transfer-integration-handoff-kr.md)

This handoff is for Go and ClairveilJS 0.3.1 wallet, prover, payroll, and operations integrators consuming the `BatchJoinSplit16x32` client contract over Cosmos or EVM. It is not the project Completion Record and does not change the Master Roadmap.

## Frozen Integration Identities

| contract | value |
| --- | --- |
| prepared payload | `batch-transfer-payload-v1` |
| prepared proof | `batch-transfer-proof-v1` |
| active circuit set | `privacy-note-v1` |
| circuit artifact ID | `batch-joinsplit-16x32-v1` |
| prover request/response | `v1` / `v1` |
| prover route | `POST /v1/proofs/batch-transfer` |
| Cosmos execution | `MsgBatchTransfer` |
| EVM execution | canonical `singleProofBatchTransfer` precompile call |
| maximum shape | 16 inputs / 32 outputs |
| default prover admission | in-flight 1 / queued 4, batch-specific |

The payload hash binds the prepared payload. The response carries the request payload hash; clients must reject version/hash mismatch before broadcast. Unknown or duplicate JSON fields and trailing JSON values are invalid.

## Required Client Pipeline

```text
plan and atomically reserve 1..16 inputs
-> prepare 1..32 payment/change/padding outputs
-> compute canonical effect, roots, digest, intent
-> obtain one structured owner signature
-> persist private prepared payload (0600)
-> prove locally or with one explicitly selected remote prover
-> verify response version and request payload hash
-> persist private proof (0600)
-> recheck all nullifiers and broadcast one Cosmos MsgBatchTransfer or EVM singleProofBatchTransfer call
-> typed scan and commitment/disclosure verification
-> reconcile batch chain status and per-item evidence separately
```

Do not implement automatic multi-prover failover. Do not request a signature before ciphertexts, roots, canonical payload, and expiry are final. Creator remains replaceable after proving.

## Shape Conformance

Every downstream implementation should run the conformance test against the machine-readable fixture at `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json`. It pins 1/1, 3-input/4-output mixed disclosure, 31 payments plus change, exact 32 payments, and the explicit `exact32` padding shape.

Run:

```bash
go test ./x/privacy/client/sdk/conformance -run TestBatchTransferContract -count=1
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

## Prover And Operations

Prover readiness means valid R1CS and proving key checksums for the batch circuit; it is distinct from validator verifying-key readiness. Acquire the batch permit after bounded body/JSON framing and hold it from semantic validation until the actual gnark proof call returns. Request cancellation does not release capacity while an in-process proof still runs. Queue saturation returns HTTP 429 with `busy` and `retryable=true`; this flag does not authorize endpoint failover.

The 16x32 circuit has a large memory footprint. Production hard cancellation and containment require process isolation. Never log request bodies, payload hashes, note fields, paths, amounts, recipients, signatures, or witness-derived solver errors.

## Scan And Disclosure

Consume the typed global scan cursor and preserve `(height, global_sequence, output_index)` ordering across Deposit, 2x2 Transfer, and Batch Transfer. Do not fall back to ciphertext-free ABCI events after a typed query failure. Verify NoteV1 commitments before wallet insertion and deduplicate retries.

View tags are untrusted hints. Safe mode attempts full decrypt on mismatch; tag-only mode must be an explicit opt-in with documented loss risk. Verify public/recipient user disclosure and audit/self-view full disclosure against output index, commitment, policy, digest, and plaintext blinding. Audit decryption failure produces manual-review evidence and does not roll back chain success.

## Payroll And Retry

One batch operation owns many input reservations and many item outputs. Reservation creation and lease/CAS transitions are atomic. Batch success consumes every active input, but each payroll item succeeds only when expected output index, commitment, recipient evidence, amount/asset, and disclosure evidence match.

On timeout or restart, query the stored transaction hash and input nullifiers before re-signing. Reuse the same signed bytes when retry policy permits. Never rebuild or retry a subset of the atomic output list. Preserve the original operation ID and reservations through reconciliation.

## Downstream Risk Disclosure

- Input/output counts and timing leak batch shape.
- Padding increases state, query size, and gas.
- A remote prover sees the whole batch witness.
- Audit ciphertext inclusion does not guarantee auditor decryptability.
- The code is experimental.
- Formal trusted setup: **NOT PERFORMED**.
- External audit: **NOT PERFORMED**.

## Regression Gate

Keep these independent paths working:

```bash
go test ./x/privacy/... -count=1
make privacy-e2e-smoke
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

The supported batch path uses one proof and one atomic transaction for the complete output list.
