# Clairveil Downstream Cosmos SDK Integration Guide

This document is the implementation checklist for importing `github.com/DELIGHT-LABS/clairveil/x/privacy` into a real Cosmos SDK-based chain. The Clairveil standalone repository is the core for independently developing, testing, and documenting the privacy feature set. The real chain imports this module and connects it to its own app wiring, EVM, policy, precompile, and operations policy.

Korean version: [clairveil-downstream-cosmos-integration-guide-kr.md](clairveil-downstream-cosmos-integration-guide-kr.md)

## 1. Integration Model

The recommended model separates responsibilities as follows.

- The Clairveil repo provides `x/privacy`, proto, Go SDK helpers, conformance fixtures, prover contract, and reference daemon.
- The downstream chain imports `x/privacy` and wires it into its own `app.go`, genesis, CLI/API, and testnet configuration.
- EVM, policy modules, precompiles, fee policy, and permission policy are implemented by the downstream chain.
- The Clairveil reference daemon `clairveild` is a host for verifying that the module can run end-to-end by itself. It does not replace the downstream app.
- The batch chain core supplies the production `MsgBatchTransfer` contract and fourth circuit. The batch integration adds a reference Go SDK, a bounded remote prover, typed wallet/payroll surfaces, and a batch CLI/tutorial. Import their concrete packages and fixtures; do not infer a downstream JS/product contract from the proto alone.

## 2. Go Module Dependency

During early development, a local `replace` is fastest.

```go
require github.com/DELIGHT-LABS/clairveil v0.4.0

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
/clairveil.privacy.v1.Msg/BatchTransfer
```

The Query service provides these HTTP gateway paths.

```text
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/scan_events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/audit_config
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/assets/by_denom/{canonical_denom}
GET /clairveil/privacy/v1/assets/by_id/{asset_id_hex}
POST /clairveil/privacy/v1/privacy_scan
POST /clairveil/privacy/v1/commitment_paths_at_root
```

If the downstream repo has its own proto generation pipeline, include `proto/clairveil/privacy/v1/*.proto` and update generated output in the same commit so stale generated files do not remain.

`scan_events` uses a `(height, sequence)` cursor. Its `limit` bounds the scan cursor page budget, so filtered pages can return `events=[]` with `has_more=true`. Wallet clients must advance to `next_height` and `next_sequence` and continue instead of treating an empty page as scan completion.

`privacy_scan` is the typed state projection for Deposit, native 2x2 JoinSplit, BatchJoinSplit16x32, and zero-output withdraw summaries. It uses the lexicographic cursor `(height, global_sequence, output_index)`, `privacy-sequence-v1`, and `privacy-scan-v2`. `commitment_paths_at_root` returns at most 16 paths from one exact root/height snapshot; remote use can reveal input linkage to the query provider.

Downstream web and mobile clients should use the POST JSON body binding for batch `nullifiers` checks, chunking requests at 1000 nullifiers. GET is retained for small compatibility calls, but large nullifier batches are likely to exceed common URL length limits.

`MsgWithdraw` does not contain output note fields. Downstream clients upgrading from older generated bindings must drop legacy `new_note_commitment` and `encrypted_note` withdraw values instead of sending dummy output-note bytes.

`MsgTransfer` contains two encrypted output notes and two 2-byte `view_tags`. The tags are untrusted local-scan hints, not server-filterable ownership tags. Safe default wallet sync must full-decrypt on a tag mismatch unless the product explicitly enables a fast mode with recovery/rescan support. Downstream EVM precompiles, bindings, and generated clients must keep `new_commitments`, `cipher_texts`, and `view_tags` aligned by output index.

`MsgBatchTransfer` contains one proof, one historical root, 1..16 ordered nullifiers, 1..32 structured `BatchTransferOutput` values, exact audit key ID/epoch/target, and expiry. Counts come only from repeated-field lengths. The keeper re-derives the frozen 12 public values, precharges `BatchGasModelV1`, verifies the batch VK, and atomically writes nullifiers, globally unique commitments, root snapshot, typed scan state, and a minimal event. The public-input schema SHA-256 is `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`.

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
GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
```

Watch these points.

- The `privacy` module account must be created in genesis.
- The deposit recipient module account must not be blocked by blocked-address policy.
- The withdraw recipient must follow the normal account address prefix.
- If downstream denom policy exists, use the real denom instead of `uclair`, and update CLI/tutorial/fixtures together.

### 5.1 Trusted Deposit Funding

An in-process EVM precompile or policy adapter may debit a transparent funder that differs from the attributed actor by calling the additive Keeper API:

```go
resp, err := app.PrivacyKeeper.DepositWithFunder(ctx, msg, escrow)
```

This is a trusted Go integration surface, not a protobuf Msg service. The public `MsgDeposit` protobuf, gRPC, CLI, and client transaction wire remain unchanged, and public `MsgServer.Deposit` continues to use `msg.Creator` as both actor and funder.

`DepositWithFunder` validates address formats but does not authenticate `msg.Creator` or authorize `funder`; the deposit proof also does not bind the creator. The downstream adapter must therefore enforce all of these invariants:

- Derive `msg.Creator` as a canonical Cosmos address from the authenticated EVM caller/operator, never from user-supplied calldata.
- Pass only the fixed Privacy precompile escrow address as `funder`; do not expose a caller-selected funder, and ensure the escrow is distinct from the Clairveil `privacy` module account. `DepositWithFunder` rejects the `privacy` module account because a bank self-transfer would not add transparent backing.
- Keep bank send restrictions from redirecting transfers addressed to the `privacy` module account. The trusted `DepositWithFunder` entry verifies that the module balance increases by the exact deposit amount and rolls the nested cache back if a restriction redirects or suppresses the transfer. These two balance reads are limited to the trusted entry so the existing public `MsgServer.Deposit` gas path remains unchanged.
- Require the parsed `MsgDeposit.Amount` amount to equal EVM `msg.value` exactly and its denom to equal the runtime native denom.
- Verify downstream-specific EVM-to-Cosmos address mapping and expected address length before calling the Keeper API.

The canonical deposit core executes bank, reserve, tree, event, and index mutations in a nested SDK cache. A core failure discards that cache, while success writes only into the caller's parent context. The downstream adapter must still place the EVM value transfer, `DepositWithFunder`, and any after-call policy checks inside one outer SDK/EVM rollback boundary so a later policy failure restores escrow, module balances, and all Clairveil state and events.

## 6. Genesis Audit Key

The latest transfer model includes mandatory master-auditor disclosure in every shielded transfer. A production-like chain must therefore set the complete audit key identity in privacy genesis state.

The genesis field is:

```json
{
  "app_state": {
    "privacy": {
      "audit_master_pubkey": "<base64-bytes>",
      "audit_key_id": "master",
      "audit_key_epoch": "1"
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

set -a
source /path/to/zk_artifacts/privacy_zk_checksums.env
set +a
```

The required `privacy-note-v1` order is `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`. Validators load the four required VKs only after exact consensus identity comparison; provers lazily load selected R1CS/PK pairs. The recorded development batch artifacts are R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, and VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`. These are development identities, not production-distribution or formal-setup artifacts.

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
tx privacy transfer-batch
tx privacy transfer-batch-16x32
tx privacy prepare-batch-transfer
tx privacy prove-batch-transfer
tx privacy broadcast-batch-transfer
```

The current batch reference integration surface includes the one-proof `MsgBatchTransfer` commands `transfer-batch-16x32`, `prepare-batch-transfer`, `prove-batch-transfer`, and `broadcast-batch-transfer`, plus the companion prover route `POST /v1/proofs/batch-transfer`. The older `transfer-batch` command is intentionally different: it sends multiple independent `MsgTransfer` messages in one Cosmos transaction envelope. Do not document or wire that legacy command as the one-proof batch protocol.

The query CLI currently exposed directly is:

```text
query privacy check-nullifier
query privacy reserve uclair
```

The remaining `tree_state`, `commitment_info`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, and batch `nullifiers` queries are available through gRPC/HTTP gateway queries. If the downstream chain needs an operator CLI, add separate CLI wrappers for those queries.

## 9. Downstream Test Order

### 9.1 Deposit Proof Acquisition Boundary

The official remote acquisition route is `POST /v1/prover/deposit`, defined by the [deposit API](clairveil-proverd-deposit-api.md) and shared [HTTP API](clairveil-proverd-http-api.md). A downstream client may prove locally or call that route, but it must compute/retain the encrypted note and assemble/sign/broadcast `MsgDeposit` itself. The prover validates the versioned witness and returns a proof; it does not select denom, construct transaction metadata, or replace keeper verification. Deployments must retain the common auth/admission/no-store/error boundary rather than mount an ad-hoc handler.

Do not mix everything with target-chain-specific features from the start. Bring it up in this order.

1. Confirm `make privacy-e2e-smoke` passes in the Clairveil repo.
2. Add only module import and app wiring to the downstream app.
3. Confirm the downstream node can `init`, add genesis accounts, gentx, collect-gentxs, and `start`.
4. Add the audit master pubkey to genesis, then check that gRPC/HTTP gateway `audit_config` returns it after the first block.
5. Verify `show-address`, `deposit`, and `list-notes` first through the downstream CLI.
6. Verify `tree_state`, `events`, `scan_events`, `merkle_path`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, and `nullifiers` through gRPC/HTTP gateway.
7. Verify user disclosure and audit disclosure through `transfer` and `decode-transfer-disclosure`.
8. Verify direct and relayed withdraw with `withdraw`, `prepare-withdraw`, and `relay-withdraw`.
9. Add EVM/policy/precompile integration e2e last, including actor provenance, fixed escrow, exact `msg.value`/native-denom binding, and outer rollback after a successful trusted deposit.
10. Make the web wallet or JS SDK verify local note storage encryption, remote prover timeout/auth, and disclosure verification in its own tests.
11. Run `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, and `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache` before writing any downstream `MsgBatchTransfer` adapter.

## 10. Common Breakage Points

- If proto package, generated Go package, and service descriptor drift, Msg service registration or signing can fail.
- If `TxConfig` is not configured in the root command's client context, gentx/signing commands can break.
- Immediately after node start, before the first block, privacy txs can fail with `invalid height`, so e2e harnesses must wait for the first block.
- Without the audit master pubkey, the latest transfer UX with mandatory audit disclosure cannot be properly verified.
- If the audit master private key is operated as a development keyring/test mnemonic, the disclosure custody boundary collapses.
- If a web wallet leaves note cache or prepared payload in plaintext browser storage and telemetry, the practical privacy of the shielded UX becomes much weaker.
- If module account permissions or blocked-address policy are wrong, deposit/withdraw bank transfers fail.
- If direct bank sends or manual top-ups do not match recorded deposit/withdraw accounting, `reserve/{denom}` returns `invariant_holds=false`.
- If the downstream denom changes, tutorial, smoke script, JS SDK fixtures, and conformance vectors must change together.
- If genesis/state still pins only three circuit descriptors, or local artifacts omit the batch VK, startup/readiness must fail; do not bypass identity checks to make `MsgBatchTransfer` available.

## 11. Completion Criteria

Downstream integration is first-pass complete when all of the following pass.

- The downstream daemon builds with privacy store, keeper, module, query gateway, and tx command included.
- Privacy state and audit master pubkey are present in genesis.
- A local single-node chain passes deposit, transfer, disclosure decode, and withdraw.
- `tree_state`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, and `nullifiers` queries respond correctly.
- The four-circuit identity, batch development artifact readiness, direct core integration, deterministic gas, atomic rollback, and typed scan/minimal-event tests pass.
- The integration record distinguishes the implemented Go SDK/prover/wallet/payroll/CLI reference surfaces for batch integration from work still owned by the downstream product, and states that formal production artifacts are not supplied.
- Audit master private key custody policy is reflected in production operations docs.
- Wallet storage encryption and remote prover privacy policy are reflected in JS/TS SDK or web wallet design docs.
- Downstream-specific EVM/policy/precompile integration is separated into separate tests.
- Trusted deposit integration proves that only the fixed escrow is debited, creator attribution comes from the authenticated operator, exact value/denom binding holds, and both core-local and downstream outer rollback leave no partial state or events.
