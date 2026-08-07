# Clairveil Getting Started

> Korean version: [clairveil-getting-started-kr.md](clairveil-getting-started-kr.md)

This guide is the prerequisite, initialization, configuration, and first-response reference for the current checkout. Clairveil is `PUBLICATION_READY_EXPERIMENTAL`; these steps create a development chain and development Groth16 artifacts, not a production deployment or trusted setup ceremony.

## 1. Prerequisites

Required for the default repository workflow:

| Tool | Baseline | Used by |
| --- | --- | --- |
| Go | `1.25.12` | Build, tests, binaries, circuit setup |
| Python | `3.9+` | Init/release scripts and JSON validation |
| Bash | `/bin/bash` | Make targets and scripts |
| Git | No repository-pinned minimum | Clone, exact-ref docs, and release manifests |
| Make | No repository-pinned minimum | Repository build, test, init, and release targets |
| Node.js/npm | Node.js `22+` | `make examples` and `make ci` |

Optional tools are task-specific: Docker is used by the PostgreSQL reservation integration when no external DSN is supplied; `grpcurl` is required by the live batch localnet gate; and `protoc`, `protoc-gen-go`, `protoc-gen-go-grpc`, `buf`, and `clang-format` are needed only when regenerating proto output.

The default repository workflow requires no third-party Python packages. `make docs-check` uses the Go toolchain for full Draft 2020-12 validation of the canonical prover HTTP schema and fixtures.

Check the main versions before initialization:

```bash
git --version
make --version
go version
python3 --version
bash --version
node --version
npm --version
```

The scripts assume a Unix-like command environment. Platform support is established by the repository CI and the downstream project; this document does not claim support for an untested OS.

## 2. Resource Planning

`make init` generates every active development circuit artifact. Recorded full-shape batch runs used roughly `3,339,862,016` to `3,354,689,536` bytes peak RSS; the batch R1CS and PK are `122,813,535` and `209,218,621` bytes. Treat those as reference measurements, not hard limits. Plan for more than 4 GiB of available memory and at least 1 GiB of free disk, with additional headroom for the Go build cache and local chain data.

Validators need the required VK files after exact consensus-identity comparison. Provers additionally need the selected R1CS/PK pair and therefore have the larger storage and memory boundary.

## 3. Initialize And Start

```bash
git clone https://github.com/DELIGHT-LABS/clairveil.git
cd clairveil
export CLAIRVEIL_HOME=${CLAIRVEIL_HOME:-"$HOME/.clairveil"}
export CHAIN_ID=${CHAIN_ID:-clairveil-local-1}
make init
source "$CLAIRVEIL_HOME/clairveil.env"
clairveild --home "$CLAIRVEIL_HOME" start
```

`make init` first builds all binaries and installs six project binaries (`clairveild`, `clairveil-setup`, `clairveil-proverd`, `clairveil-payroll`, `clairveil-payrolld`, and `clairveil-verify`) into `GOBIN` or `$(go env GOPATH)/bin`. The installed `clairveil-verify` binary is a legacy-only debugging helper; initialization and current note validation do not use it. It then:

1. moves an existing `~/.clairveil` to a timestamped backup;
2. generates the `privacy-note-v1` development artifact set;
3. creates `alice`, `bob`, `relayer`, and `auditor` development keys;
4. initializes genesis, funds the accounts, and creates the validator gentx;
5. places the auditor disclosure public key in genesis;
6. writes an exported runtime environment to `~/.clairveil/clairveil.env`.

The generated `init-out/*-key.json` files contain development key material. Keep the home private, never reuse these keys, and do not copy it into a production environment.

The active default listeners are RPC `26657`, P2P `26656`, and gRPC `9090`. The generated `app.toml` configures the REST address as `tcp://localhost:1317` but keeps `[api] enable = false`, so nothing binds `1317` by default. REST binds that address only after you explicitly set `enable = true` or start with `--api.enable`. Stop another local node using the active ports before starting the reference node. Smoke-test scripts accept port overrides; normal `clairveild start` configuration is changed in the generated files or with daemon flags.

## 4. Common Configuration

| Variable | Default | Scope |
| --- | --- | --- |
| `GOBIN` | `go env GOBIN`, then `$(go env GOPATH)/bin` | Binary installation |
| `CLAIRVEIL_HOME` | `~/.clairveil` | `make init` home and backup location |
| `CHAIN_ID` | `clairveil-local-1` | Init and smoke-test chain ID |
| `NODE_NAME` | `local` | Init and smoke-test node moniker |
| `KEYRING_BACKEND` | `test` | Development init keyring |
| `CLAIRVEIL_INIT_ACCOUNTS` | `alice bob relayer auditor` | Space-separated init keys |
| `VALIDATOR_KEY` / `AUDITOR_KEY` | `alice` / `auditor` | Required roles; both must be in the account list |
| `FUND_AMOUNT` / `STAKE_AMOUNT` | Script defaults | Development genesis balances |
| `CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR` | `<home>/artifacts/privacy` under `make init`; runtime fallback `.` when unset | Artifact output/runtime directory; source `clairveil.env` or set it explicitly |
| `CLAIRVEILD_BIN` / `CLAIRVEIL_SETUP_BIN` | Installed binary | Explicit binary override |
| `RPC_PORT`, `P2P_PORT`, `ABCI_PORT`, `GRPC_PORT`, `API_PORT`, `PPROF_PORT` | Script defaults | Smoke/localnet script configuration; `API_PORT` does not bind while REST is disabled |

Example isolated initialization:

```bash
CLAIRVEIL_HOME=/tmp/clairveil-home \
CHAIN_ID=my-local-chain \
CLAIRVEIL_INIT_ACCOUNTS="alice bob relayer auditor" \
make init
```

`clairveil-setup` writes `privacy_zk_checksums.env` as shell assignments, not exported variables. When sourcing a raw file outside `make init`, identify its directory explicitly and export all assignments:

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/absolute/path/to/artifacts/privacy
set -a
source "$CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR/privacy_zk_checksums.env"
set +a
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

The `~/.clairveil/clairveil.env` file generated by `make init` already contains `export` statements and can be sourced directly.

## 5. First Checks

Without a running node:

```bash
make docs-check
go test ./...
make build
```

With no other node on the active default RPC, P2P, or gRPC ports:

```bash
make localnet-smoke
make privacy-e2e-smoke
```

Use [clairveil-testing-guide.md](clairveil-testing-guide.md) to choose the heavier batch, payroll, benchmark, and release gates.

## 6. Troubleshooting And Cleanup

- `clairveild: command not found`: add `$(go env GOPATH)/bin` or the value of `go env GOBIN` to `PATH`, then rerun `make install`.
- setup is killed or reports out-of-memory/no-space: stop competing workloads, free disk, or generate artifacts on a larger host. Reuse only a complete artifact directory that passes strict preflight.
- artifact checksum or circuit identity mismatch: remove stale development artifacts/proof jobs, regenerate the exact active set, and start from fresh genesis. Environment checksums cannot override consensus identity.
- `address already in use`: stop the existing node or assign all relevant port overrides to the smoke script; do not change only one of the RPC/P2P/gRPC/REST endpoints.
- `privacy_scan` returns `ResourceExhausted`: if one typed record exceeds `max_encoded_bytes`, increase that byte budget up to the server maximum; reducing output/event limits cannot split a single record. If the record still cannot fit, treat it as a server/contract incident. Persist the last accepted cursor and never skip it.
- `commitment_paths_at_root` returns `ResourceExhausted`: for an oversized historical rebuild, use the current root or a trusted local historical index; for temporary rebuild-admission saturation, use bounded retry. Reducing the number of requested commitments does not reduce the historical tree's leaf count.
- `make release-pack-verify` rejects a dirty tree: run `make docs-check` while editing. Use a clean untagged commit only for a commit-bound CI snapshot; generate and verify a publishable release pack from the final annotated exact-SemVer tagged commit, or verify an explicit archive with its out-of-band commit.

To remove a disposable home, stop the node first and delete only the path you explicitly selected. `make init` preserves an existing home as `<home>.backup-YYYYMMDD-HHMMSS`; review and remove old backups manually because they can contain private development keys and wallet data.

## 7. First Privacy Flow

Leave the node from section 3 running and open a second Bash terminal at the repository root. Use the same home and chain ID in both terminals; the commands below assume the default account names and a fresh home. Use a disposable `CLAIRVEIL_HOME`; the test keyring and output files contain development private material.

Create a dedicated output directory and save the four transparent addresses. The shell functions below keep the commands readable while preserving the individual transaction evidence.

```bash
umask 077
REPO_DIR="$PWD"
export CLAIRVEIL_HOME=${CLAIRVEIL_HOME:-"$HOME/.clairveil"}
export CHAIN_ID=${CHAIN_ID:-clairveil-local-1}
export RPC_ADDR=${RPC_ADDR:-tcp://127.0.0.1:26657}
source "$CLAIRVEIL_HOME/clairveil.env"
WORK_DIR=${WORK_DIR:-"$PWD/tmp/clairveil-first-flow"}
mkdir -p -m 700 "$WORK_DIR"
cd "$WORK_DIR"

run_cli() { command clairveild --home "$CLAIRVEIL_HOME" "$@"; }
wait_for_tx() {
  local txhash="$1"
  local response code attempt
  for attempt in {1..60}; do
    if response="$(run_cli query tx "$txhash" --node "$RPC_ADDR" --output json 2>/dev/null)"; then
      code="$(python3 -c 'import json,sys; doc=json.load(sys.stdin); print(doc.get("code", doc.get("tx_response", {}).get("code", 0)))' <<<"$response")"
      [ "$code" = 0 ] && return 0
      printf 'transaction %s was included with code %s\n' "$txhash" "$code" >&2
      return 1
    fi
    sleep 2
  done
  printf 'timed out waiting for transaction %s\n' "$txhash" >&2
  return 1
}
txhash_from() {
  python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["txhash"])' "$1"
}

for name in alice bob relayer auditor; do
  run_cli keys show -a "$name" --keyring-backend test > "${name}-address.txt"
done
```

Derive Alice and Bob shielded addresses, and save Bob’s disclosure key and Alice’s view key. The auditor key was installed in genesis during initialization. `clairs1...` is the shielded address derived from the `clair1...` keyring account.

```bash
run_cli tx privacy show-address --from alice --keyring-backend test --output json > alice-shielded.json
run_cli tx privacy show-address --from bob --keyring-backend test --output json > bob-shielded.json
run_cli tx privacy show-view-key --from alice --keyring-backend test --output json > alice-view-key.json
run_cli tx privacy show-disclosure-pubkey --from bob --keyring-backend test --output json > bob-disclosure.json
python3 - <<'PY'
import json
from pathlib import Path
for source, target, field in [
    ('alice-shielded.json', 'alice-shielded-address.txt', 'address'),
    ('bob-shielded.json', 'bob-shielded-address.txt', 'address'),
    ('bob-disclosure.json', 'bob-disclosure.hex', 'public_key_hex'),
]:
    Path(target).write_text(json.loads(Path(source).read_text())[field] + '\n')
PY
```

Deposit an `11uclair`, `10uclair`, `7uclair`, and zero dummy note for Alice. The dummy note makes the single-transfer examples pairable. Wait for each returned hash before moving to the next command.

```bash
for amount in 11 10 7 0; do
  run_cli tx privacy deposit "${amount}uclair" \
    --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
    --gas 2500000 --gas-prices 8500000000uclair --yes --output json > "deposit-${amount}.json"
  wait_for_tx "$(txhash_from "deposit-${amount}.json")"
done
run_cli tx privacy list-notes --from alice --keyring-backend test \
  --node "$RPC_ADDR" --json > alice-notes.json
```

Confirm the spendable amounts include `11`, `10`, `7`, and `0`. A missing note normally means the wallet needs a scan or the deposit is not included yet; do not select another note merely to make the tutorial continue.

### Transfers and disclosure

First send `11uclair` privately. Every transfer still includes mandatory audit disclosure; “private” means no user disclosure.

```bash
run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 11uclair \
  --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --gas 9000000 --gas-prices 8500000000uclair --yes --output json > transfer-private.json
wait_for_tx "$(txhash_from transfer-private.json)"
```

Send `7uclair` with public amount-only user disclosure, then inspect the report. Its summary must identify `user`, `public`, `amount`, `7`, and `uclair`.

```bash
run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 7uclair \
  --privacy-policy amount --disclosure-mode public \
  --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --gas 9000000 --gas-prices 8500000000uclair --yes --output json > transfer-public.json
wait_for_tx "$(txhash_from transfer-public.json)"
run_cli tx privacy decode-transfer-disclosure \
  --tx-hash "$(txhash_from transfer-public.json)" --disclosure-plane public --node "$RPC_ADDR" --report > transfer-public-report.json
```

Send `10uclair` with recipient-encrypted `amount-from-to` disclosure. Bob can read the user plane, the auditor can read the audit plane, and Alice can read self-view. Each report must have `verification.verified: true`; audit delivery being on-chain does not itself guarantee decryptability.

```bash
run_cli tx privacy transfer "$(cat bob-shielded-address.txt)" 10uclair \
  --privacy-policy amount-from-to --disclosure-mode recipient-encrypted \
  --disclosure-pubkey "$(cat bob-disclosure.hex)" \
  --from alice --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --gas 10000000 --gas-prices 8500000000uclair --yes --output json > transfer-recipient.json
wait_for_tx "$(txhash_from transfer-recipient.json)"
for plane in recipient audit self-view; do
  account=bob; [ "$plane" = audit ] && account=auditor; [ "$plane" = self-view ] && account=alice
  run_cli tx privacy decode-transfer-disclosure \
    --tx-hash "$(txhash_from transfer-recipient.json)" --disclosure-plane "$plane" \
    --from "$account" --keyring-backend test --node "$RPC_ADDR" --report > "transfer-${plane}-report.json"
done
run_cli tx privacy list-notes --from bob --keyring-backend test \
  --node "$RPC_ADDR" --rescan-wallet --json > bob-notes.json
```

Bob should now hold `11`, `7`, and `10`. Scanners must use typed global `(height, global_sequence, output_index)` ordering, recompute recovered `NoteV1` commitments, and deduplicate retries. A view tag is only a hint: safe mode still attempts decryption on a mismatch. Recipient, auditor, and self-view consumers recompute their digest with the plaintext blinding; report an undecryptable audit ciphertext as `AuditDeliveryFailed` or `ManualReview`, not as chain failure.

### Direct and relayed withdrawal

Direct-withdraw Bob’s `11uclair` to Alice, then prepare Bob’s `7uclair` withdrawal and let the relayer submit the exact prepared file.

```bash
run_cli tx privacy withdraw 11uclair --recipient "$(cat alice-address.txt)" \
  --from bob --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --gas 3500000 --gas-prices 8500000000uclair --yes --output json > withdraw-direct.json
wait_for_tx "$(txhash_from withdraw-direct.json)"

run_cli tx privacy prepare-withdraw 7uclair --recipient "$(cat alice-address.txt)" \
  --from bob --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --out withdraw-payload.json --output json > withdraw-payload.stdout.json
python3 - <<'PY'
import json
from pathlib import Path
if json.loads(Path('withdraw-payload.stdout.json').read_text()) != json.loads(Path('withdraw-payload.json').read_text()):
    raise SystemExit('prepared payload differs from stdout')
PY
run_cli tx privacy relay-withdraw withdraw-payload.json \
  --from relayer --keyring-backend test --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --gas 3500000 --gas-prices 8500000000uclair --yes --output json > withdraw-relayed.json
wait_for_tx "$(txhash_from withdraw-relayed.json)"
run_cli tx privacy list-notes --from bob --keyring-backend test \
  --node "$RPC_ADDR" --rescan-wallet --json > bob-notes-final.json
```

The final spendable amount for Bob is `10`. Return to the repository root with `cd "$REPO_DIR"` before running the next Make targets. Before removing the disposable home, stop the node with its recorded PID or foreground terminal; do not delete a path selected by another run.

## 8. BatchJoinSplit16x32 Localnet

`transfer-batch-16x32` is one `MsgBatchTransfer` and one `BatchJoinSplit16x32` proof. It is distinct from a single 2x2 transfer and `transfer-batch`, which puts multiple independent 2x2 `MsgTransfer` messages/proofs into one Cosmos transaction. The implementation is experimental: formal trusted setup and external audit have not been performed. A remote prover receives the complete batch witness; public input/output counts reveal batch shape, while padding hides only active output count at extra chain-state and gas cost.

The default target is a static/conformance gate only: it validates the machine-readable fixture without starting a node or generating large artifacts. Run the actual node and remote-prover scenario explicitly.

```bash
make privacy-batch-joinsplit-localnet
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

The live runner writes `tmp/privacy-batch-joinsplit-localnet/out` and requires mode `0600` on mnemonic-bearing key JSON, prepared witnesses, and proofs. It covers the fixture’s one-input payment, mixed-disclosure change, 31-payments-plus-change, exact 32-payment, and explicit zero-padding cases. Exact amounts, roles, and modes are pinned in [`privacy_batch_transfer_v1_contract.json`](../x/privacy/client/sdk/conformance/testdata/privacy_batch_transfer_v1_contract.json).

The following is an API-shape example, not a continuation of the completed live runner: that runner creates and stops its own home and localnet. Run it only on a node that you started and kept running, after creating fresh batch-compatible Alice notes. Substitute the actual shielded recipient, disclosure key, RPC endpoint, and chain ID. Omit `--prover-url` for local proof; a supplied URL selects exactly one remote prover.

```bash
clairveild --home "$CLAIRVEIL_HOME" tx privacy prepare-batch-transfer \
  --payment '<recipient-clairs-address>,4uclair' \
  --payment '<recipient-clairs-address>,5uclair,amount,public' \
  --payment '<recipient-clairs-address>,6uclair,amount-from-to,recipient-encrypted,<recipient-disclosure-pubkey-hex>' \
  --chain-id "$CHAIN_ID" --node "$RPC_ADDR" \
  --output-mode compact --prepared-out prepared.json --rescan-wallet \
  --from alice --keyring-backend test
clairveild --home "$CLAIRVEIL_HOME" tx privacy prove-batch-transfer prepared.json --proof-out proof.json \
  --prover-url http://127.0.0.1:18080 --output json
clairveild --home "$CLAIRVEIL_HOME" tx privacy broadcast-batch-transfer prepared.json proof.json \
  --from alice --keyring-backend test --node "$RPC_ADDR" \
  --chain-id "$CHAIN_ID" --gas 80000000 \
  --gas-prices 8500000000uclair --yes --output json
```

Use `--output-mode exact32` only for intentional padding and `--no-self-view` only when the sender opts out for the whole batch. A bearer token belongs in `CLAIRVEIL_PRIVACY_PROVER_BEARER_TOKEN`, never a command argument. Loopback may use HTTP; every non-loopback prover URL must use HTTPS and the client does not follow redirects. There is no automatic multi-prover failover, so handle a remote failure explicitly and privacy-consciously.

The live runner restarts node and prover, queries the exact32 transaction by stored hash, then verifies that a newly signed envelope for an already-spent payload fails closed. That is a spent-nullifier smoke, not durable worker retry validation. Production order is: retain operation ID, reservations, prepared payload, proof, and exact signed bytes; query tx hash first; query all input nullifiers before re-signing; retry the same signed bytes when policy allows; never retry only part of an atomic batch; and mark an item successful only after expected output evidence matches.

For a fast disk or already verified development artifacts:

```bash
CLAIRVEIL_BATCH_LOCALNET_WORK_DIR=/fast-disk/clairveil-batch \
CLAIRVEIL_BATCH_ARTIFACT_DIR=/verified/dev-artifacts \
RPC_PORT=27657 PROVERD_PORT=19080 \
RUN_LOCALNET=1 make privacy-batch-joinsplit-localnet
```

The work directory must be new or empty. After one run the script leaves a marker that permits resetting only that directory; it rejects symlinks, protected paths, and unmarked non-empty directories. `BATCH_EXPIRES_IN` defaults to 7200 seconds and `BATCH_GAS` to 80000000. The [reference payroll README](../examples/reference-payroll/README.md) specifies the one-proof durable reservation, exact-byte retry, reconciliation, and item-evidence contract.

Next references: [architecture](clairveil-architecture.md), [operations guide](clairveil-operations-guide.md), [reference payroll](../examples/reference-payroll/README.md), and the [complete documentation index](README.md).
