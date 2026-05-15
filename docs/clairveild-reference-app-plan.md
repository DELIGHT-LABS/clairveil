# Clairveil Reference App Design

Korean version: [clairveild-reference-app-plan-kr.md](clairveild-reference-app-plan-kr.md)

## Goal

`clairveild` is the minimal Cosmos SDK reference daemon used inside the Clairveil standalone repo to validate `x/privacy` on a real chain.

Its goal is not to replace a downstream project's full app. It exists so the following can be validated independently.

- `clairveild init`
- `clairveild add-genesis-account`
- `clairveild gentx`
- `clairveild collect-gentxs`
- `clairveild start`
- `clairveild tx privacy ...`
- `clairveild query privacy ...`
- `make init`
- local shell walkthrough
- e2e fixture / tutorial validation

## Reference App Scope

The reference app includes only these modules.

| Module | Reason |
| --- | --- |
| `auth` | account, signature, sequence, account number handling |
| `bank` | transparent coin lock/release for deposit/withdraw |
| `staking` | local validator/gentx/start baseline flow |
| `slashing` | basic validator lifecycle dependency |
| `distribution` | staking app baseline dependency |
| `gov` | compatibility with normal Cosmos SDK app composition |
| `mint` | staking app baseline dependency |
| `params` | compatibility for modules that need legacy params subspace |
| `consensus` | consensus params genesis/query |
| `genutil` | init/gentx/collect-gentxs |
| `tx` / `auth tx` | sign/broadcast/encode/decode CLI |
| `x/privacy` | Clairveil privacy module |

## Implementation Status

As of 2026-05-04, the standalone repo implements the following.

| Item | Status |
| --- | --- |
| `app` package | Includes `auth`, `bank`, `staking`, `slashing`, `distribution`, `gov`, `mint`, `params`, `consensus`, `genutil`, `vesting`, and `x/privacy`. |
| `cmd/clairveild` root command | Runs Cosmos SDK daemon commands. |
| Local genesis flow | `init`, `keys add`, `add-genesis-account`, `gentx`, `collect-gentxs`, and `validate` pass with `uclair`. |
| Short start smoke | `clairveild start` in a temporary home reaches block 1 commit. |
| Smoke script | `make localnet-smoke` or `./scripts/localnet-smoke.sh` reproduces local genesis/start smoke. |
| Local init helper | `make init` prepares build/install, backup of existing home, local genesis, test keys, audit master pubkey, and ZK artifacts. |
| Privacy walkthrough | `docs/clairveil-local-privacy-walkthrough.md` covers the full privacy flow on standalone `clairveild`. |
| Full privacy e2e smoke | `make privacy-e2e-smoke` validates deposit, transfer, disclosure, direct withdraw, and relayed withdraw on a local node. |
| Release handoff | `make release-pack` and `make release-pack-verify` generate and verify the downstream handoff pack. |

Note: Cosmos SDK v0.54 commands use root-level `add-genesis-account`, `gentx`, `collect-gentxs`, and `validate`, not `genesis` subcommands.

## Components

### 1. app package

The minimal Cosmos SDK app under `app` is the reference chain host.

Main files:

- `app/app.go`
- `app/encoding.go`
- `app/genesis.go`
- `app/export.go`
- `app/modules.go`

Done criteria:

- `go test ./app/...` passes
- `x/privacy` keeper is connected to bank keeper
- module account `privacy` is registered as a bank module account
- `DefaultNodeHome` is `~/.clairveil`

### 2. clairveild root command

`cmd/clairveild` runs the Cosmos SDK root command.

Main files:

- `cmd/clairveild/main.go`
- `cmd/clairveild/cmd/root.go`

Done criteria:

- `clairveild --help` prints
- `clairveild init --chain-id clairveil-local-1 test`
- `clairveild keys add alice --keyring-backend test`
- `clairveild tx privacy --help`
- `clairveild query privacy --help`

### 3. local node smoke test

`scripts/localnet-smoke.sh` starts a local node using a temporary home.

Done criteria:

- `clairveild init`
- add genesis account
- create gentx
- collect-gentxs
- `clairveild start`
- check `/status` or `clairveild status`

### 4. privacy walkthrough and e2e

`docs/clairveil-local-privacy-walkthrough.md` and `scripts/privacy-e2e-smoke.sh` validate the privacy flow for the Clairveil standalone repo.

Done criteria:

- `uclair` funding
- `clair1...` transparent address
- `clairs1...` shielded address
- deposit
- scan
- transfer
- decode disclosure
- direct withdraw
- prepare / relay withdraw
- public, recipient-encrypted, audit disclosure
