# Clairveil Downstream Cosmos SDK Integration Guide

This document is the implementation checklist for importing `github.com/DELIGHT-LABS/clairveil/x/privacy` into a real Cosmos SDK-based chain. The Clairveil standalone repository is the core for independently developing, testing, and documenting the privacy feature set. The real chain imports this module and connects it to its own app wiring, EVM, policy, precompile, and operations policy.

Korean version: [clairveil-downstream-cosmos-integration-guide-kr.md](clairveil-downstream-cosmos-integration-guide-kr.md)

## 1. Integration Model

The recommended model separates responsibilities as follows.

- The Clairveil repo provides `x/privacy`, proto, Go SDK helpers, conformance fixtures, prover contract, and reference daemon.
- The downstream chain imports `x/privacy` and wires it into its own `app.go`, genesis, CLI/API, and testnet configuration.
- EVM, policy modules, precompiles, fee policy, and permission policy are implemented by the downstream chain.
- The Clairveil reference daemon `clairveild` is a host for verifying that the module can run end-to-end by itself. It does not replace the downstream app.

## 2. Go Module Dependency

During early development, a local `replace` is fastest.

```go
require github.com/DELIGHT-LABS/clairveil v0.0.0

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

When release tags are available, remove `replace` and pin a specific version or commit pseudo-version.

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

Before integration, check that the downstream app and Clairveil `go.mod` do not conflict on Cosmos SDK, CometBFT, gogoproto, and grpc-gateway versions. If the conflict is large, make a separate dependency-alignment commit before importing the module.

## 3. Proto Contract

The privacy proto package is:

```text
clairveil.privacy.v1
```

The generated Go package is:

```text
github.com/DELIGHT-LABS/clairveil/x/privacy/types
```

The main proto files are:

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

The Msg service provides:

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
```

The Query service provides these HTTP gateway paths.

```text
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
```

If the downstream repo has its own proto generation pipeline, include `proto/clairveil/privacy/v1/*.proto` and update generated output in the same commit so stale generated files do not remain.

`MsgWithdraw` does not contain output note fields. Downstream clients upgrading from older generated bindings must drop legacy `new_note_commitment` and `encrypted_note` withdraw values instead of sending dummy output-note bytes.

## 4. App Wiring Checklist

Add these imports to the downstream app.

```go
import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacykeeper "github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)
```

Add the privacy module account to module account permissions.

```go
var maccPerms = map[string][]string{
	privacytypes.ModuleName: nil,
}
```

Add the privacy store key.

```go
keys := storetypes.NewKVStoreKeys(
	privacytypes.StoreKey,
)
```

Add the keeper to the app struct.

```go
type App struct {
	PrivacyKeeper privacykeeper.Keeper
}
```

Create the keeper.

```go
app.PrivacyKeeper = *privacykeeper.NewKeeper(
	appCodec,
	runtime.NewKVStoreService(keys[privacytypes.StoreKey]),
	app.GetSubspace(privacytypes.ModuleName),
	app.BankKeeper,
)
```

Add the AppModule to the module manager.

```go
app.ModuleManager = module.NewManager(
	privacy.NewAppModule(appCodec, app.PrivacyKeeper),
)
```

Include the privacy module in genesis init and export order.

```go
genesisModuleOrder := []string{
	privacytypes.ModuleName,
}

app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)
```

The basic module manager must register interfaces and gRPC gateway routes.

```go
app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)
app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
```

Service registration should happen through the module manager.

```go
if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
	panic(err)
}
```

## 5. BankKeeper Requirements

The privacy module moves transparent assets into the shielded pool module account, then sends them back to a recipient during withdraw. Therefore the downstream `BankKeeper` must support at least these methods.

```go
SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
```

Watch these points.

- The `privacy` module account must be created in genesis.
- The deposit recipient module account must not be blocked by blocked-address policy.
- The withdraw recipient must follow the normal account address prefix.
- If downstream denom policy exists, use the real denom instead of `uclair`, and update CLI/tutorial/fixtures together.

## 6. Genesis Audit Key

The latest transfer model includes mandatory master-auditor disclosure in every shielded transfer. A production-like chain must therefore set the audit master public key in privacy genesis state.

The genesis field is:

```json
{
  "app_state": {
    "privacy": {
      "audit_master_pubkey": "<base64-bytes>"
    }
  }
}
```

For local development, the disclosure key can be shown with the CLI.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

The CLI output `public_key_hex` is hex. Genesis stores bytes in JSON, so convert it to base64 before inserting it.

```bash
printf '%s' '<public_key_hex>' | xxd -r -p | base64
```

A development chain with an empty audit key may still pass query or genesis validation, but it is not the target state for the latest transfer UX. Downstream e2e should run with an audit key configured.

### 6.1 Audit Private Key Custody

The Clairveil repo provides the flow for putting an audit master public key in genesis/config and decoding audit disclosure. Creation, storage, access control, rotation, and incident response for the audit master private key are the responsibility of the downstream production project.

This key must not be treated like a normal relayer key or a development test key. If it leaks, transfer metadata encrypted to mandatory audit disclosure on that chain can be read.

A production-like downstream chain must define at least these policies.

- Define the audit private key creation ceremony and approvers.
- Do not place the key in plaintext files, git, Docker images, or CI variable dumps.
- Choose HSM, KMS, Vault, secure enclave, or offline custody.
- Separate roles for who can decrypt disclosure under which conditions.
- Keep decrypt-operation audit logs and access approval records.
- Document key rotation and compromised-key incident response.
- State clearly in operations docs that the local tutorial `--keyring-backend test` auditor key is not a production custody example.

### 6.2 Wallet Storage And Prepared Payload Custody

The Clairveil reference CLI stores the local wallet note cache and prepared payload/proof JSON with `0600` file permission. This is a practical default for the sample chain and development environments, but it does not replace an encrypted storage policy for web wallets or production wallets.

Downstream wallets must classify these as privacy-sensitive local data.

- root seed or root signer material
- spend/view/disclosure secret
- local note cache
- note amount, randomness, nullifier, Merkle path
- prepared transfer payload
- prepared withdraw prover payload
- disclosure plaintext and decrypted reports

A web wallet or external wallet SDK must decide at least:

- whether plaintext note DB will be avoided in browser storage;
- which storage encryption method to use, such as password-derived keys, platform keystore, hardware wallet, secure enclave, or server-side KMS;
- which metadata the user delegates to a remote prover when sending prepared payloads;
- a redaction policy preventing payload bodies, bearer tokens, seeds, and disclosure plaintext from entering telemetry, crash reports, or debug logs.

## 7. ZK Artifact Runtime Configuration

The node must know proving/verifying artifact locations and checksum policy.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/path/to/zk_artifacts
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

Create artifact checksum env files with:

```bash
go run ./cmd/clairveil-setup \
  --out /path/to/zk_artifacts

source /path/to/zk_artifacts/privacy_zk_checksums.env
```

Recommended modes:

- `strict`: Use in CI, release candidates, and production-like nodes. Missing artifacts or checksum mismatch are blocked before start.
- `warn`: Use only when you want artifact problems to appear as logs during development.
- `off`: Not recommended except for special debugging.

## 8. CLI/API Wiring

The downstream daemon should expose module tx/query commands from the root command. The privacy module `AppModuleBasic` provides:

```go
privacy.AppModuleBasic{}.GetTxCmd()
privacy.AppModuleBasic{}.GetQueryCmd()
```

Current user-facing tx CLI commands to check are:

```text
tx privacy show-address
tx privacy show-view-key
tx privacy show-disclosure-pubkey
tx privacy deposit
tx privacy transfer
tx privacy decode-transfer-disclosure
tx privacy list-notes
tx privacy withdraw
tx privacy prepare-withdraw
tx privacy relay-withdraw
```

The query CLI currently exposed directly is:

```text
query privacy check-nullifier
```

`tree_state`, `commitment_info`, `events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, and `reserve/{denom}` are available through gRPC/HTTP gateway queries. If the downstream chain needs an operator CLI, add separate CLI wrappers for those queries.

## 9. Downstream Test Order

Do not mix everything with target-chain-specific features from the start. Bring it up in this order.

1. Confirm `make privacy-e2e-smoke` passes in the Clairveil repo.
2. Add only module import and app wiring to the downstream app.
3. Confirm the downstream node can `init`, add genesis accounts, gentx, collect-gentxs, and `start`.
4. Add the audit master pubkey to genesis, then check that gRPC/HTTP gateway `audit_config` returns it after the first block.
5. Verify `show-address`, `deposit`, and `list-notes` first through the downstream CLI.
6. Verify `tree_state`, `events`, `merkle_path`, `disclosure_config`, `circuit_config`, and `reserve/{denom}` through gRPC/HTTP gateway.
7. Verify user disclosure and audit disclosure through `transfer` and `decode-transfer-disclosure`.
8. Verify direct and relayed withdraw with `withdraw`, `prepare-withdraw`, and `relay-withdraw`.
9. Add EVM/policy/precompile integration e2e last.
10. Make the web wallet or JS SDK verify local note storage encryption, remote prover timeout/auth, and disclosure verification in its own tests.

## 10. Common Breakage Points

- If proto package, generated Go package, and service descriptor drift, Msg service registration or signing can fail.
- If `TxConfig` is not configured in the root command's client context, gentx/signing commands can break.
- Immediately after node start, before the first block, privacy txs can fail with `invalid height`, so e2e harnesses must wait for the first block.
- Without the audit master pubkey, the latest transfer UX with mandatory audit disclosure cannot be properly verified.
- If the audit master private key is operated as a development keyring/test mnemonic, the disclosure custody boundary collapses.
- If a web wallet leaves note cache or prepared payload in plaintext browser storage and telemetry, the practical privacy of the shielded UX becomes much weaker.
- If module account permissions or blocked-address policy are wrong, deposit/withdraw bank transfers fail.
- If direct bank sends or manual top-ups bypass approved reserve accounting, `reserve/{denom}` returns `invariant_holds=false`.
- If the downstream denom changes, tutorial, smoke script, JS SDK fixtures, and conformance vectors must change together.

## 11. Completion Criteria

Downstream integration is first-pass complete when all of the following pass.

- The downstream daemon builds with privacy store, keeper, module, query gateway, and tx command included.
- Privacy state and audit master pubkey are present in genesis.
- A local single-node chain passes deposit, transfer, disclosure decode, and withdraw.
- `tree_state`, `events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, and `reserve/{denom}` queries respond correctly.
- Audit master private key custody policy is reflected in production operations docs.
- Wallet storage encryption and remote prover privacy policy are reflected in JS/TS SDK or web wallet design docs.
- Downstream-specific EVM/policy/precompile integration is separated into separate tests.
