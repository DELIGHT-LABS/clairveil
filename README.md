# Clairveil

Clairveil is an auditable shielded privacy core for Cosmos SDK chains.

It packages shielded identity derived from transparent accounts, shielded deposits, ZK-based transfers and withdrawals, user selective disclosure, and mandatory audit disclosure on every transfer into a reusable `x/privacy` module. This repository is not a full production chain. It is a standalone reference host for developing and validating the privacy core independently.

> Korean documentation: [README-kr.md](README-kr.md)

## What This Repository Provides

- `x/privacy`: Cosmos SDK privacy module
- `clairveild`: reference daemon that runs the privacy module on a real local chain
- `clairveil-setup`: Groth16 circuit artifact generator
- `clairveil-proverd`: local/remote companion prover reference service
- CLI, Go SDK helpers, and JS/web wallet conformance fixtures
- Local walkthrough, e2e smoke tests, and release handoff pack

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

## Quick Start

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

`make ci` only runs Go tests, binary builds, and JS example checks. It does not connect to a `clairveild start` node.

To follow the full privacy flow manually on a local node, use the [Local walkthrough](docs/clairveil-local-privacy-walkthrough.md).

To validate the same flow automatically, run:

```bash
make privacy-e2e-smoke
```

This target creates a separate temporary home and starts its own local node. If a `clairveild start` node is already using the default ports `26657`, `26656`, `9090`, and `1317`, stop that node first or use e2e port overrides.

## Build

```bash
make build
```

Main binaries:

| Binary | Role |
| --- | --- |
| `clairveild` | reference chain daemon |
| `clairveil-setup` | ZK artifact generator |
| `clairveil-verify` | legacy/debug note verification helper |
| `clairveil-proverd` | companion prover HTTP service |

You can also build each binary directly:

```bash
go build ./cmd/clairveild
go build ./cmd/clairveil-setup
go build ./cmd/clairveil-verify
go build ./cmd/clairveil-proverd
```

Install built binaries into the Go install path:

```bash
make install
```

`make install` uses `go env GOBIN` when set. Otherwise it uses `$(go env GOPATH)/bin`.

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
```

`make localnet-smoke` and `make privacy-e2e-smoke` start their own validation nodes. If a node is already using the default ports, the smoke tests can collide with it.

Release-candidate validation:

```bash
make release-check
make release-pack
make release-pack-verify
```

See the [Testing guide](docs/clairveil-testing-guide.md) for the test layers and target meanings.

## Using Clairveil From Another Project

During early integration, using a local `replace` is usually fastest:

```go
require github.com/DELIGHT-LABS/clairveil v0.0.0

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
| [Reference app](docs/clairveild-reference-app-plan.md) | Design intent and current status of the `clairveild` reference host |
| [Local walkthrough](docs/clairveil-local-privacy-walkthrough.md) | Manually run deposit, transfer, disclosure, and withdraw on a local node |
| [Circuit guide](docs/clairveil-circuits.md) | What the Spend/JoinSplit circuits prove and do not prove |
| [CLI reference](docs/clairveil-cli-reference.md) | Usage of `clairveild tx/query privacy` commands |
| [Testing guide](docs/clairveil-testing-guide.md) | Unit, e2e, conformance, and release validation |
| [Operations guide](docs/clairveil-operations-guide.md) | Node, prover, artifact, Merkle, and audit operations baseline |
| [Maintainer instructions](docs/clairveil-maintainer-instructions.md) | Maintenance rules for docs, circuits, proto, fixtures, and releases |
| [Downstream integration](docs/clairveil-downstream-cosmos-integration-guide.md) | How to attach `x/privacy` to a Cosmos SDK app |
| [JS SDK handoff](docs/clairveil-js-sdk-handoff.md) | Contract for JS/TS SDK and web wallet implementation |
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
