# Clairveil getting started

> Korean version: [clairveil-getting-started-kr.md](clairveil-getting-started-kr.md)

Clairveil uses fixed, reviewed privacy verifier artifacts. This repository does
not create trusted-setup material, replay bundles, or audit private keys during
node initialization.

## Prerequisites

Install Go `1.25.12`, Git, Make, Bash, and the reviewed directory containing
the four required verifier artifacts. The artifact directory is supplied to a
running node; it is never copied into genesis.

Prepare a reviewed small V4 configuration file. Its JSON fields are
`chain_id`, base64 `network_nonce32`, `initial_height`, `initial_audit_key`,
and `circuit_set_identity`. `initial_audit_key` contains the public key and
proof of possession only (`epoch`, base64 `key_id`, `suite`, base64
`public_key`, base64 `pop`). The circuit identity must exactly match the local
artifact manifest.

## Initialize and start

```bash
clairveild init node-1 \
  --chain-id reviewed-chain-1 \
  --audit-config /absolute/path/to/audit-config.json

clairveild start \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts /absolute/path/to/reviewed-artifacts
```

`init` checks that the chain ID and initial height agree with the configuration
and writes a fresh V4 privacy genesis with the initial audit key, network
nonce, circuit identity, and canonical asset registry. `start` checks the same
configuration against the local verifier artifacts. It has no runtime archive,
source bundle, or offline secret input.

Use the same two flags with `clairveild export`; exports include ordinary
privacy state together with the small V4 metadata and can be imported again by
the normal Cosmos initialization path. Zero-height and partial-module exports
are intentionally unsupported.

## Audit provenance

Run `clairveil-auditor` with a closed block range, the normal Cosmos chain ID,
an owner-only retained audit-key JSON file, and the reviewed verifier artifact
directory. The tool queries the small public configuration and key history,
resumes its atomic original-tx/result cache, verifies and decrypts the collected
messages, and reports collection completeness separately from lineage
completeness. See the [CLI reference](clairveil-cli-reference.md#collect-and-verify-audit-provenance).

## Verification

```bash
go test ./app ./cmd/clairveild/cmd ./x/privacy/keeper ./x/privacy/client/cli
make build
```

Do not use this development-oriented workflow as a production trusted setup.
Protect operator keys and inspect any generated development home before
deleting it.

## 8. Legacy BatchJoinSplit16x32 reference

The retained restartable multi-message batch/payroll code, V1 contract text,
and fixtures are compatibility and regression references. The former
`/v1/proofs/batch-transfer` route is not served by the current prover and is not
a fallback endpoint. Current V2 uses `clairveil.privacy.v2.MsgBatchTransfer`
and `POST /v2/prover/audit-field`. Do not use legacy fixture output as V2 audit
provenance or capacity evidence. The historical circuit relation is documented
in the [BatchJoinSplit16x32 reference](clairveil-batch-joinsplit-16x32.md).
