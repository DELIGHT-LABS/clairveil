# Clairveil

Clairveil is an auditable shielded privacy core for Cosmos SDK chains.

It packages shielded identity derived from transparent accounts, shielded deposits, ZK-based transfers and withdrawals, user selective disclosure, and mandatory audit disclosure on every transfer into a reusable `x/privacy` module. This repository is not a full production chain. It is a standalone reference host for developing and validating the privacy core independently.

> Korean documentation: [README-kr.md](README-kr.md)

## What This Repository Provides

- `x/privacy`: Cosmos SDK privacy module
- `clairveild`: reference daemon that runs the privacy module on a real local chain
- `clairveil-setup`: Groth16 circuit artifact generator
- `clairveil-proverd`: local/remote companion prover reference service
- `clairveil-payroll` and `clairveil-payrolld`: reference payroll control-plane CLI and daemon
- CLI, Go SDK helpers, and JS/web wallet conformance fixtures
- Local walkthrough, e2e smoke tests, reference payroll rehearsals, and release handoff pack

> Clairveil does not replace a downstream production app. Related modules, validator operations, audit key custody, wallet storage encryption, artifact signing, and deployment policy must be decided by the project that imports or forks Clairveil.

## Reference Chain

| Item | Value |
| --- | --- |
| Go module | `github.com/DELIGHT-LABS/clairveil` |
| Daemon | `clairveild` |
| Transparent prefix | `clair` |
| Shielded prefix | `clairs` |
| Reference denom | `uclair` |
| Proto packages | current Msg/audit query `clairveil.privacy.v2`; scan/tree query `clairveil.privacy.v1` |
| Local chain-id | supplied by the required audit configuration |

## Current Status And Compatibility

| Item | Current baseline |
| --- | --- |
| Publication status | `PUBLICATION_READY_EXPERIMENTAL`; source/reference publication, not production deployment approval |
| Consensus circuit set | `privacy-note-v1-audit-field-v1` with audit-field V2 runtime state |
| Current client contract | V2 asset messages; audit-field request/response envelope `v1` with exact artifact binding and final PI23 |
| Retained legacy fixtures | `privacy-fixed-v1`; transfer payload `v5`, proof/prover contract `v2` |
| Batch surface | `BatchJoinSplit16x32`, `MsgBatchTransfer`; Go SDK/prover/scanner/payroll/CLI reference implementation for batch integration |
| Upgrade boundary | Earlier artifacts, proof jobs, note/scan caches, and non-audited genesis are incompatible; use fresh genesis/reset and rescan |
| Outstanding production gates | Formal trusted setup, external security/circuit audit, signed production artifacts, and downstream chain/product validation |

Documentation describes the code at the same checkout. When integrating a tag or commit, read the docs from that exact ref and verify the release manifest; do not combine `HEAD` documentation with an older binary or tag.

## Quick Start

Use Git, Make, Go `1.25.12`, and Bash; repository CI/example checks also need Node.js `22+` and npm. Read the [getting started guide](docs/clairveil-getting-started.md) before using the reviewed verifier artifacts.

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/absolute/path/to/audit-field-artifacts
export CLAIRVEIL_HOME=${CLAIRVEIL_HOME:-"$HOME/.clairveil"}
clairveild --home "$CLAIRVEIL_HOME" init node-1 \
  --chain-id reviewed-chain-1 \
  --audit-config /absolute/path/to/audit-config.json
clairveild --home "$CLAIRVEIL_HOME" start \
  --audit-config /absolute/path/to/audit-config.json \
  --audit-artifacts "$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR"
```

The reviewed V4 configuration carries public network/key metadata only. `init` writes small V4 metadata and standard privacy genesis; `start` verifies the local artifacts. There is no replay runtime bundle or offline secret input. The current audit-field bundle remains development-grade, not a production trusted setup.

`clairveil-auditor` can collect a bounded range of original successful privacy transactions and execution results into an atomic local cache, then reuse the existing proof verification, epoch-key decryption, and deposit-rooted lineage logic. Collection and provenance completeness are reported separately; it does not replay the chain or use wallet scan state as an audit ledger.

## Validation

```bash
make ci
```

This runs documentation checks, Go tests, binary builds, and JS example checks without a running node. See the [testing guide](docs/clairveil-testing-guide.md) for focused protocol and capacity evidence.

## Integration And Documentation

Start with the [documentation index](docs/README.md) for architecture, protocol, CLI, SDK/prover contracts, and operations/security references.

- Cosmos app integration: [downstream guide](docs/clairveil-downstream-cosmos-integration-guide.md).
- Reference payroll contracts and demo: [payroll example](examples/reference-payroll/README.md).
- Contribution and release rules: [CONTRIBUTING.md](CONTRIBUTING.md).

## Security

If you suspect a vulnerability, do not post details in a public issue. Follow [SECURITY.md](SECURITY.md) and submit a private vulnerability report.

Clairveil is privacy-sensitive software. Before production deployment, the downstream project must separately complete audit key custody, wallet storage encryption, remote prover policy, ZK artifact provenance, and a chain-specific threat model.

## License

Clairveil is distributed under the Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
