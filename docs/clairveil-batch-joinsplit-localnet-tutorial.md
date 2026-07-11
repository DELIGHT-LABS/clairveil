# BatchJoinSplit16x32 Localnet Tutorial

Korean version: [clairveil-batch-joinsplit-localnet-tutorial-kr.md](clairveil-batch-joinsplit-localnet-tutorial-kr.md)

This tutorial exercises the one-proof `BatchJoinSplit16x32` path. It is different from both a single 2x2 transfer and `transfer-batch`, which only places several independent `MsgTransfer` messages in one Cosmos transaction.

```text
single transfer       = one MsgTransfer / one 2x2 proof
transfer-batch        = multiple MsgTransfer messages / multiple 2x2 proofs
transfer-batch-16x32  = one MsgBatchTransfer / one 16x32 proof
```

The implementation is experimental. Formal trusted setup and external audit have not been performed. A remote prover receives the whole batch witness. Public input/output counts leak batch shape, while padding hides the active output count only at additional chain state and gas cost.

## Quick Validation

The default target validates the machine-readable Session 3B fixture and its Go conformance test without starting a node or generating large proving artifacts.

```bash
make privacy-batch-joinsplit-localnet
```

Run the actual chain and remote prover workflow explicitly:

```bash
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

The 16x32 setup and proof are resource intensive. Use a host with enough memory and disk. Outputs are written to `tmp/privacy-batch-joinsplit-localnet/out`; prepared witness and proof files are checked for mode `0600`.

## Cases

The runner executes these contracts in order:

| case | inputs | payments | resulting outputs | disclosure/self-view |
| --- | ---: | ---: | --- | --- |
| `one-input-one-payment` | 1 | 1 | 1 payment | private, self-view enabled |
| `three-input-four-output-mixed-disclosure` | 3 | 3 | 3 payments + change | private/public/recipient-encrypted, self-view enabled |
| `thirty-one-payments-plus-change` | 16 | 31 | 31 payments + change | private, self-view disabled |
| `exact-thirty-two-payments` | 16 | 32 | 32 payments, no change | private, self-view enabled |
| `explicit-zero-padding` | 1 | 1 | payment + 31 padding outputs | private, self-view disabled |

The exact values and expected roles are pinned in `x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_session3b_contract.json`.

## Companion Commands

Prepare a payload. Repeat `--payment` and optionally repeat `--input-index` to pin selected notes.

```bash
clairveild tx privacy prepare-batch-transfer \
  --payment '<clairs-address>,4uclair' \
  --payment '<clairs-address>,5uclair,amount,public' \
  --payment '<clairs-address>,6uclair,amount-from-to,recipient-encrypted,<pubkey-hex>' \
  --input-index 10 --input-index 11 --input-index 12 \
  --output-mode compact \
  --prepared-out prepared.json \
  --rescan-wallet \
  --from alice --keyring-backend test
```

Generate one proof locally by omitting `--prover-url`, or call exactly one selected remote prover:

```bash
clairveild tx privacy prove-batch-transfer prepared.json \
  --proof-out proof.json \
  --prover-url http://127.0.0.1:18080 \
  --output json
```

There is no automatic multi-prover failover. A remote failure is returned to the caller for an explicit privacy-aware decision.

Broadcast the payload-bound proof:

```bash
clairveild tx privacy broadcast-batch-transfer prepared.json proof.json \
  --from alice --keyring-backend test \
  --node tcp://127.0.0.1:26657 \
  --chain-id clairveil-batch-local-1 \
  --gas 80000000 --gas-prices 8500000000uclair --yes --output json
```

The combined command accepts the prepare flags plus `--prepared-out`, `--proof-out`, and optional `--prover-url`:

```bash
clairveild tx privacy transfer-batch-16x32 \
  --payment '<clairs-address>,7uclair' \
  --output-mode compact \
  --prepared-out prepared.json \
  --proof-out proof.json \
  --prover-url http://127.0.0.1:18080 \
  --rescan-wallet \
  --from alice --keyring-backend test
```

Use `--output-mode exact32` only for explicit padding. Use `--no-self-view` only when the sender deliberately opts out for the entire batch.

## Scan And Disclosure Verification

After inclusion, rescan the recipient wallet:

```bash
clairveild tx privacy list-notes \
  --from bob --keyring-backend test \
  --node tcp://127.0.0.1:26657 \
  --rescan-wallet --json
```

Downstream scanners must use typed global `(height, global_sequence, output_index)` ordering, verify each recovered NoteV1 commitment, and deduplicate retries. View tags are hints; safe mode still attempts decryption on mismatch. Recipient, auditor, and self-view disclosure consumers must recompute the user/full digest with the plaintext blinding. Audit ciphertext delivery does not guarantee decryptability; report decrypt failures as `AuditDeliveryFailed`/`ManualReview`, not chain failure.

## Restart And Retry

The runner restarts both the node and prover after successful batches, then queries the exact32 transaction by its stored tx hash. It also verifies that rebroadcasting a payload whose nullifiers are already spent fails closed.

Production retry order is:

1. Keep the operation ID, reservations, prepared payload, proof, and signed transaction bytes.
2. Query the transaction hash first.
3. Query all input nullifiers before any re-sign.
4. Retry the same signed bytes when policy permits.
5. Never retry only part of an atomic batch.
6. Mark an item successful only after its expected output evidence matches.

## Payroll Path

The batch payroll worker maps one operation to one proof job and many item outputs. Reserve all input notes atomically, persist prepared/proof artifacts before `ProofReady`, broadcast once, then reconcile batch chain status separately from item evidence status. The legacy `reference-payroll-live-localnet` target remains a regression path for the multi-message 2x2 envelope and is not renamed to imply 16x32 proving.

## Useful Overrides

```bash
CLAIRVEIL_BATCH_LOCALNET_WORK_DIR=/fast-disk/clairveil-batch \
CLAIRVEIL_BATCH_ARTIFACT_DIR=/verified/dev-artifacts \
RPC_PORT=27657 PROVERD_PORT=19080 \
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

`CLAIRVEIL_BATCH_ARTIFACT_DIR` reuses an already verified development artifact directory and skips the expensive setup. Prebuilt binaries can be supplied through `CLAIRVEILD_BIN`, `CLAIRVEIL_SETUP_BIN`, and `CLAIRVEIL_PROVERD_BIN`.
`BATCH_EXPIRES_IN` controls the owner-intent lifetime for resource-intensive localnet proofs and defaults to 7200 seconds; `BATCH_GAS` defaults to 80000000.
