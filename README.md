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
| Proto package | `clairveil.privacy.v1` |
| Default local chain-id | `clairveil-local-1` |

## Current Status And Compatibility

| Item | Current baseline |
| --- | --- |
| Publication status | `PUBLICATION_READY_EXPERIMENTAL`; source/reference publication, not production deployment approval |
| Consensus circuit set | `privacy-note-v1` with state version 2 |
| Fixed client contract | `privacy-fixed-v1`; transfer payload `v5`, proof/prover contract `v2` |
| Batch surface | `BatchJoinSplit16x32`, `MsgBatchTransfer`; Go SDK/prover/scanner/payroll/CLI reference implementation for batch integration |
| Upgrade boundary | Earlier artifacts, proof jobs, note/scan caches, and three-circuit genesis are incompatible; use fresh genesis/reset and rescan |
| Outstanding production gates | Formal trusted setup, external security/circuit audit, signed production artifacts, and downstream chain/product validation |

Documentation describes the code at the same checkout. When integrating a tag or commit, read the docs from that exact ref and verify the release manifest; do not combine `HEAD` documentation with an older binary or tag.

## Quick Start

Prerequisites are Git, Make, Go `1.25.13`, Python `3.9+`, Bash, and—when running repository CI/example checks—Node.js `22+` with npm. The repository does not pin minimum Git or Make versions. Verify the tools you will use before initialization:

```bash
git --version
make --version
go version
python3 --version
bash --version
node --version
npm --version
```

Read the [Getting started guide](docs/clairveil-getting-started.md) before generating the full circuit set; it also covers resource requirements, ports, environment variables, and cleanup.

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
make init
```

Start the node with:

```bash
source ~/.clairveil/clairveil.env
clairveild start
```

## Validation

Code and example validation can run without a running node.

```bash
make ci
```

`make ci` runs documentation checks, Go tests, binary builds, and JS example checks. It does not connect to a `clairveild start` node.

To follow the full privacy flow manually on a local node, use the [Local walkthrough](docs/clairveil-local-privacy-walkthrough.md).

To validate the same flow automatically, run:

```bash
make privacy-e2e-smoke
```

This target creates a separate temporary home and starts its own local node. If a `clairveild start` node is already using the active default RPC, P2P, or gRPC ports (`26657`, `26656`, or `9090`), stop that node first or use e2e port overrides. REST is disabled in the generated `app.toml`; its configured `1317` address binds only when the API is explicitly enabled.

## Build

```bash
make build
```

Main binaries:

| Binary | Role |
| --- | --- |
| `clairveild` | reference chain daemon |
| `clairveil-setup` | ZK artifact generator |
| `clairveil-verify` | legacy-only note decryption/debug helper; incompatible with current typed notes |
| `clairveil-proverd` | companion prover HTTP service |
| `clairveil-payroll` | reference payroll planning, reservation, reconcile, and report CLI |
| `clairveil-payrolld` | reference payroll scheduler/daemon surface |
| `clairveil-benchreport` | benchmark report renderer |
| `clairveil-proverload` | external prover load benchmark tool |
| `clairveil-localnetload` | localnet load metric converter |
| `clairveil-userlatency` | wallet/user latency trace summarizer |
| `clairveil-bulktransferbench` | synthetic bulk-transfer capacity simulator |

You can also build each binary directly:

```bash
go build ./cmd/clairveild
go build ./cmd/clairveil-setup
go build ./cmd/clairveil-verify
go build ./cmd/clairveil-proverd
go build ./cmd/clairveil-payroll
go build ./cmd/clairveil-payrolld
go build ./cmd/clairveil-benchreport
go build ./cmd/clairveil-proverload
go build ./cmd/clairveil-localnetload
go build ./cmd/clairveil-userlatency
go build ./cmd/clairveil-bulktransferbench
```

Install built binaries into the Go install path:

```bash
make install
```

`make install` uses `go env GOBIN` when set. Otherwise it uses `$(go env GOPATH)/bin`.

It installs the six listed project binaries: `clairveild`, `clairveil-setup`, `clairveil-verify`, `clairveil-proverd`, `clairveil-payroll`, and `clairveil-payrolld`. Five belong to the current runtime/reference flow; `clairveil-verify` is installed only as a legacy debugging helper and cannot decrypt or validate current `privacy-fixed-v1` typed notes. Benchmark/load tools are built by `make build` but are not installed by `make install`; install one explicitly with `go install ./cmd/<tool-name>` when needed.

## Local Chain Initialization

Initialize the default local home `~/.clairveil`:

```bash
make init
```

What it does:

- Runs `make install` first.
- Backs up an existing `~/.clairveil` to `~/.clairveil.backup-YYYYMMDD-HHMMSS`.
- Runs `clairveild init`, `keys add`, `add-genesis-account`, `gentx`, `collect-gentxs`, and `validate`.
- Creates `alice`, `bob`, `relayer`, and `auditor` test keys, then sets the auditor disclosure public key as the genesis audit master key.
- Generates ZK artifacts under `~/.clairveil/artifacts/privacy` and writes `~/.clairveil/clairveil.env`.

Start:

```bash
source ~/.clairveil/clairveil.env
clairveild start
```

Common overrides:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home make init
CHAIN_ID=my-local-chain make init
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" make init
```

## Testing

For the usual full development check, run:

```bash
make ci
```

`make ci` does not require a running local node.

You can also run individual checks:

```bash
make test
make localnet-smoke
make privacy-e2e-smoke
make reference-payroll-demo
make reference-payroll-live-localnet
make reference-payroll-rehearsal
```

`make localnet-smoke` and `make privacy-e2e-smoke` start their own validation nodes. If a node is already using the active default RPC, P2P, or gRPC ports, the smoke tests can collide with it. Include `1317` in the collision check only when REST was explicitly enabled.

Before creating a release commit and tag:

```bash
make release-check
```

After creating the annotated exact-SemVer tag at that commit, generate and verify the final artifact:

```bash
make release-pack
make release-pack-verify
```

See the [Testing guide](docs/clairveil-testing-guide.md) for the test layers and target meanings.

## Using Clairveil From Another Project

During early integration, using a local `replace` is usually fastest:

```go
require github.com/DELIGHT-LABS/clairveil v0.2.0

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

Once release tags are available, pin a tag or commit:

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

A downstream Cosmos SDK app must wire `x/privacy`, proto, keeper dependencies, module accounts, genesis audit key, and CLI/API routes into its own app. Use the [Downstream integration guide](docs/clairveil-downstream-cosmos-integration-guide.md) as the baseline.

## CLI Overview

Representative privacy CLI commands:

```bash
clairveild tx privacy show-address --from alice --keyring-backend test --output json
clairveild tx privacy deposit 10uclair --from alice --keyring-backend test
clairveild tx privacy transfer <clairs1...> 7uclair --from alice --keyring-backend test
clairveild tx privacy list-notes --from alice --keyring-backend test --json
clairveild tx privacy withdraw 7uclair --from alice --keyring-backend test
```

Command purposes, major flags, and output shapes are documented in the [CLI reference](docs/clairveil-cli-reference.md).

## Document Map

| Document | Purpose |
| --- | --- |
| [Complete documentation index](docs/README.md) | Canonical document map, lifecycle, language-pair, and release rules |
| [Plan status index](plans/README.md) | Active and completed implementation plans, with legacy archive boundary |
| [Getting started](docs/clairveil-getting-started.md) | Prerequisites, resources, initialization, configuration, and troubleshooting |
| [Architecture](docs/clairveil-architecture.md) | Components, trust boundaries, state, and transaction data flow |
| [Reference app](plans/clairveild-reference-app-plan.md) | Design intent and current status of the `clairveild` reference host |
| [Local walkthrough](docs/clairveil-local-privacy-walkthrough.md) | Manually run deposit, transfer, disclosure, and withdraw on a local node |
| [Circuit guide](docs/clairveil-circuits.md) | What the Spend/JoinSplit circuits prove and do not prove |
| [CLI reference](docs/clairveil-cli-reference.md) | Usage of `clairveild tx/query privacy` commands |
| [Testing guide](docs/clairveil-testing-guide.md) | Unit, e2e, conformance, and release validation |
| [Operations guide](docs/clairveil-operations-guide.md) | Node, prover, artifact, Merkle, and audit operations baseline |
| [Maintainer instructions](docs/clairveil-maintainer-instructions.md) | Maintenance rules for docs, circuits, proto, fixtures, and releases |
| [Downstream integration](docs/clairveil-downstream-cosmos-integration-guide.md) | How to attach `x/privacy` to a Cosmos SDK app |
| [Client product brief](docs/clairveil-client-product-brief.md) | Product capability scope for wallet/app clients |
| [Client UX flows](docs/clairveil-client-ux-flows.md) | Setup, scan, transfer, withdraw, disclosure, and recovery flows |
| [Client risk decisions](docs/clairveil-client-risk-decisions.md) | Storage, prover, audit, disclosure, and telemetry decisions |
| [Client API checklist](docs/clairveil-client-api-checklist.md) | Chain/prover APIs, fixtures, release gates, and compatibility checks |
| [JS SDK handoff](docs/clairveil-js-sdk-handoff.md) | Contract for JS/TS SDK and web wallet implementation |
| [Scan optimization plan](plans/clairveil-scan-optimization-implementation-plan.md) | Implemented note scan optimization scope and excluded future work |
| [Reference payroll product](docs/clairveil-reference-payroll-product.md) | Payroll control-plane, localnet tutorial, and rehearsal reference product |
| [Prover profile](docs/clairveil-proverd-remote-production-profile.md) | Remote operation profile for `clairveil-proverd` |
| [Merkle restore SOP](docs/clairveil-merkle-restore-sop.md) | Tree verification after snapshot, restore, or migration |
| [Threat model](docs/clairveil-threat-model.md) | Trust boundaries, assets, and residual risks |
| [Security review](docs/clairveil-security-best-practices-review.md) | Pre-production security checkpoints |
| [Release handoff](docs/clairveil-release-handoff-pack.md) | Artifacts and validation steps for downstream teams |

## Security

If you suspect a vulnerability, do not post details in a public issue. Follow [SECURITY.md](SECURITY.md) and submit a private vulnerability report.

Clairveil is privacy-sensitive software. Before production deployment, the downstream project must separately complete audit key custody, wallet storage encryption, remote prover policy, ZK artifact provenance, and a chain-specific threat model.

## License

Clairveil is distributed under the Apache License 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
