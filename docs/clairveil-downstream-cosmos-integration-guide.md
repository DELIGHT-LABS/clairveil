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
require github.com/DELIGHT-LABS/clairveil v0.5.0

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

When release tags are available, remove `replace` and pin a specific version or commit pseudo-version.

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

Before integration, check that the downstream app and Clairveil `go.mod` do not conflict on Cosmos SDK, CometBFT, gogoproto, and grpc-gateway versions. If the conflict is large, make a separate dependency-alignment commit before importing the module.

## 3. Proto Contract

The current transaction and audit-query proto package is:

```text
clairveil.privacy.v2
```

Its generated Go package is `github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2`. The retained V1 wallet scan/tree/reserve queries and legacy contracts use `clairveil.privacy.v1` and `github.com/DELIGHT-LABS/clairveil/x/privacy/types`.

The main proto files are:

```text
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

The current V2 Msg service provides:

```text
/clairveil.privacy.v2.Msg/Deposit
/clairveil.privacy.v2.Msg/Transfer
/clairveil.privacy.v2.Msg/Withdraw
/clairveil.privacy.v2.Msg/BatchTransfer
/clairveil.privacy.v2.Msg/ScheduleAuditKeyEpoch
/clairveil.privacy.v2.Msg/CancelPendingAuditEpoch
/clairveil.privacy.v2.Msg/SetPrivacyHalt
```

The audit runtime does not register the V1 Msg service; legacy calls fail with `legacy privacy service is disabled`. Governance may execute the three audit-management messages, but the four asset messages are explicitly blocked from governance execution.

The live V2 audit queries are:

```text
GET /clairveil/privacy/v2/audit/configuration
GET /clairveil/privacy/v2/audit/key_schedule
GET /clairveil/privacy/v2/audit/keys/{epoch}
```

The V1 Query service remains registered for wallet scan, tree, reserve, and asset reads and provides these HTTP gateway paths.

```text
GET /clairveil/privacy/v1/nullifier/{nullifier}
GET /clairveil/privacy/v1/tree_state
GET /clairveil/privacy/v1/commitment/{commitment_hex}
GET /clairveil/privacy/v1/events
GET /clairveil/privacy/v1/scan_events
GET /clairveil/privacy/v1/merkle_path/{commitment_hex}
GET /clairveil/privacy/v1/disclosure_config
GET /clairveil/privacy/v1/circuit_config
GET /clairveil/privacy/v1/reserve/{denom=**}
GET /clairveil/privacy/v1/nullifiers
POST /clairveil/privacy/v1/nullifiers
GET /clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}
GET /clairveil/privacy/v1/assets/by_id/{asset_id_hex}
POST /clairveil/privacy/v1/privacy_scan
POST /clairveil/privacy/v1/commitment_paths_at_root
```

If the downstream repo has its own proto generation pipeline, include both `proto/clairveil/privacy/v1/*.proto` and `proto/clairveil/privacy/v2/*.proto`, and update generated output in the same commit so stale generated files do not remain.

`scan_events` uses a `(height, sequence)` cursor. Its `limit` bounds the scan cursor page budget, so filtered pages can return `events=[]` with `has_more=true`. Wallet clients must advance to `next_height` and `next_sequence` and continue instead of treating an empty page as scan completion.

`privacy_scan` is the typed state projection for Deposit, native 2x2 JoinSplit, BatchJoinSplit16x32, and zero-output withdraw summaries. It uses the lexicographic cursor `(height, global_sequence, output_index)`, `privacy-sequence-v1`, and `privacy-scan-v2`. `commitment_paths_at_root` returns at most 16 paths from one exact root/height snapshot; remote use can reveal input linkage to the query provider.

Downstream web and mobile clients should use the POST JSON body binding for batch `nullifiers` checks, chunking requests at 1000 nullifiers. GET is retained for small compatibility calls, but large nullifier batches are likely to exceed common URL length limits.

The detailed NoteV1 field descriptions below document retained V1 fixtures and inner circuit relations; they are not the current transaction wire. Current V2 asset messages carry `AuditAuthorization`, proof/expiry, and structured output effects as defined in `proto/clairveil/privacy/v2/tx.proto`.

The legacy `MsgWithdraw` does not contain output note fields. Downstream clients handling retained V1 fixtures must drop older `new_note_commitment` and `encrypted_note` withdraw values instead of sending dummy output-note bytes.

The legacy `MsgTransfer` fixture contains two encrypted output notes and two 2-byte `view_tags`. The tags are untrusted local-scan hints, not server-filterable ownership tags. Safe default wallet sync must full-decrypt on a tag mismatch unless the product explicitly enables a fast mode with recovery/rescan support.

The retained V1 `MsgBatchTransfer` fixture contains one proof, one historical root, ordered nullifiers, and structured outputs. Current V2 represents the same asset transition with V2 output effects and mandatory `AuditAuthorization`; use the compiled V2 proto for wire fields. The legacy public-input schema SHA-256 remains historical conformance evidence, not a V2 message schema.

## 4. App Wiring Checklist

The current audit runtime must be wired through the audited reference path, including `privacy.AppModuleBasic{AuditRuntime: true}` and `ConfigureAuditRuntime`; a plain legacy `NewKeeper`/`AppModuleBasic{}` setup does not register the V2 services. Use `app.NewAuditFieldApp` and the current reference app wiring as the executable integration example. The snippets below show only the common Cosmos module-account/store scaffolding.

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

An in-process V2 EVM precompile or policy adapter may debit a fixed transparent escrow that differs from the proof-bound principal by calling the additive Keeper API:

```go
resp, err := app.PrivacyKeeper.DepositWithFunderV2(ctx, msg, escrow)
```

This is a trusted Go integration surface, not a protobuf Msg service. The public V2 `MsgDeposit` protobuf, gRPC, CLI, proof public inputs, and normal `MsgServer.Deposit` path remain unchanged. `msg.Creator` remains the authenticated original principal, proof public target, and provenance identity; `funder` is only the account debited and passed to `Lock`. The funder is copied and must be a canonical account distinct from both the `privacy` and governance module accounts.

The proof does not directly bind the escrow funder. The trusted downstream adapter must therefore enforce all of these invariants:

- Derive and authenticate `msg.Creator` from the original EVM caller/operator, never from user-supplied calldata, and keep it as the original provider identity.
- Pass only the fixed precompile escrow as `funder`; never expose a caller-selected funder.
- Authenticate that caller-to-escrow value movement completed and that `MsgDeposit.Amount` equals `msg.value` exactly with the runtime native denom.
- Keep bank send restrictions from redirecting or suppressing the escrow-to-`privacy` module transfer. Clairveil verifies the exact escrow and module balance deltas inside its nested cache.
- Verify downstream-specific EVM-to-Cosmos address mapping and expected address length before calling the Keeper API.

The V2 core reuses the normal halt/epoch/gas/public-input/proof/reentrancy checks before the verified transition reaches the nested apply cache. After verification, only the deposit bank endpoint changes to the escrow; reserve, commitment, typed scan, and event writes remain atomic. A delegated success event additionally carries the bounded canonical V2 deposit protobuf and escrow funder, while native V2 events remain minimal. Typed scan state still carries no proof or audit ciphertext, and no separate audit ledger is created.

Clairveil success publishes only into the caller's parent context. The downstream adapter must place caller-to-escrow value movement, `DepositWithFunderV2`, event emission, and later policy checks inside one outer SDK/EVM rollback boundary. Discarding that parent cache must remove balances, Clairveil state, and events. This repository does not assert that a particular EVM precompile implementation provides that event rollback; downstream wiring must test it.

When the external auditor reads delegated events, the downstream must inject an `sdk.TxDecoder` that understands its EVM wrapper and a `VerifyDelegatedExecution` callback that authenticates wrapper/receipt success and the Creator/funder relationship. A Cosmos `Code == 0` result alone is insufficient because the internal EVM call may have reverted. `CosmosTxSource` fails closed with `AUDIT_INCOMPLETE` when the callback is absent or rejects the evidence. Multiple successful internal deposits at one wrapper message index are distinguished by `global_sequence` and `execution_id`; native top-level V2 messages still require their exact one-to-one event match. Standalone downstream EVM decoder and receipt wiring are outside Clairveil.

## 6. V4 Audit Configuration And Key Epochs

Current initialization consumes a small public V4 configuration through `--audit-config`. It contains `chain_id`, `network_nonce32`, `initial_height`, a public `initial_audit_key` (`epoch`, `key_id`, `suite`, `public_key`, `pop`), and `circuit_set_identity`. It contains no audit private key, replay archive, or runtime bundle. `clairveild init` validates and writes this public metadata; `start` and `export` require the same configuration plus `--audit-artifacts`.

After genesis, governance schedules a future key epoch, cancels a pending epoch, or changes the privacy halt through the three V2 management messages. Clients query V2 audit configuration, the key schedule, and epoch history. `show-disclosure-pubkey` is a user/self-view disclosure helper and must not be used to manufacture the initial audit key.

### 6.1 Audit Private Key Custody

Clairveil accepts the public initial key/PoP and provides an external auditor that verifies original successful transactions and execution events. Creation, storage, access control, epoch rotation, and incident response for audit private keys are the responsibility of the downstream production project.

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

The node and prover must use the same identity-pinned artifact directory.

```bash
clairveild start \
  --audit-config /path/to/audit-config.json \
  --audit-artifacts /path/to/zk_artifacts

CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/path/to/zk_artifacts clairveil-proverd
```

Create artifact checksum env files with:

```bash
go run ./cmd/clairveil-setup \
  --out /path/to/zk_artifacts --development

```

The current development order is `privacy-note-v1-audit-field-v1`: `deposit-audit-field-v1`, `spend-audit-field-v1`, `joinsplit-2x2-audit-field-v1`, `batch-joinsplit-16x32-audit-field-v1`. Validators load the four matching VKs after exact consensus identity comparison; the prover lazily loads a selected R1CS/PK pair. `clairveil-setup` supports only `--out` and `--development`; the old `--circuit`/`--overwrite` procedure and batch artifact measurements are legacy records, not current runtime evidence.

Node startup validates the manifest and consensus circuit identity automatically. The generated checksum env file may be used by external release tooling, but there is no required `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE` runtime switch.

## 8. CLI/API Wiring

The downstream daemon should expose module tx/query commands from the root command. The privacy module `AppModuleBasic` provides:

```go
privacy.AppModuleBasic{AuditRuntime: true}.GetTxCmd()
privacy.AppModuleBasic{AuditRuntime: true}.GetQueryCmd()
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

Current one-proof V2 batch submission uses `clairveil.privacy.v2.MsgBatchTransfer` and the shared companion prover route `POST /v2/prover/audit-field`. The older `/v1/proofs/batch-transfer` route and V1 staged contracts are retained documentation/fixtures and are not live fallback endpoints. Likewise, legacy `transfer-batch` sends multiple independent V1 messages and is not the current batch protocol.

The query CLI currently exposed directly is:

```text
query privacy check-nullifier
query privacy reserve uclair
```

The remaining V1 wallet scan/tree/reserve/asset queries are available through gRPC/HTTP gateway. The V2 audit runtime exposes `audit/configuration`, `audit/key_schedule`, and `audit/keys/{epoch}` separately. If the downstream chain needs an operator CLI, add wrappers without collapsing the V1 wallet and V2 audit contracts into one version.

## 9. Downstream Test Order

### 9.1 Deposit Proof Acquisition Boundary

`/v1/prover/deposit` is retained legacy documentation, not an official current remote route. The only live route is `POST /v2/prover/audit-field`, defined by the [current HTTP API](clairveil-proverd-http-api.md#current-route). Its complete witness/PI23 boundary requires local artifact-identity verification before V2 message construction; deployments retain the common auth/admission/no-store/error boundary rather than mounting an ad-hoc handler.

Do not mix everything with target-chain-specific features from the start. Bring it up in this order.

1. Record a manual native V2 flow or another working audited harness. No checked-in Make target currently provides end-to-end native V2 evidence.
2. Add only module import and app wiring to the downstream app.
3. Confirm the downstream node can `init`, add genesis accounts, gentx, collect-gentxs, and `start`.
4. Initialize with the public V4 audit configuration, then check V2 `audit/configuration`, `audit/key_schedule`, and `audit/keys/{epoch}` after the first block.
5. Verify `show-address`, `deposit`, and `list-notes` first through the downstream CLI.
6. Verify `tree_state`, `events`, `scan_events`, `merkle_path`, `disclosure_config`, `circuit_config`, `reserve/{denom=**}`, `assets/by_denom/{canonical_denom=**}`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, and `nullifiers` through gRPC/HTTP gateway.
7. Verify user disclosure and audit disclosure through `transfer` and `decode-transfer-disclosure`.
8. Verify direct and relayed withdraw with `withdraw`, `prepare-withdraw`, and `relay-withdraw`.
9. Add EVM/policy/precompile integration e2e last, including actor provenance, fixed escrow, exact `msg.value`/native-denom binding, and outer rollback after a successful trusted deposit.
10. Make the web wallet or JS SDK verify local note storage encryption, remote prover timeout/auth, and disclosure verification in its own tests.
11. Run `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, and `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache` before writing any downstream `MsgBatchTransfer` adapter.

## 10. Common Breakage Points

- If proto package, generated Go package, and service descriptor drift, Msg service registration or signing can fail.
- If `TxConfig` is not configured in the root command's client context, gentx/signing commands can break.
- Immediately after node start, before the first block, privacy txs can fail with `invalid height`, so e2e harnesses must wait for the first block.
- If the public initial audit key/PoP or active epoch history is missing, V2 asset transactions cannot obtain valid audit authorization.
- If an audit epoch private key is operated as a development keyring/test mnemonic, the custody boundary collapses.
- If a web wallet leaves note cache or prepared payload in plaintext browser storage and telemetry, the practical privacy of the shielded UX becomes much weaker.
- If module account permissions or blocked-address policy are wrong, deposit/withdraw bank transfers fail.
- If direct bank sends or manual top-ups do not match recorded deposit/withdraw accounting, `reserve/{denom=**}` returns `invariant_holds=false`.
- If the downstream denom changes, tutorial, smoke script, JS SDK fixtures, and conformance vectors must change together.
- If genesis/state still pins only three circuit descriptors, or local artifacts omit the batch VK, startup/readiness must fail; do not bypass identity checks to make `MsgBatchTransfer` available.

## 11. Completion Criteria

Downstream integration is first-pass complete when all of the following pass.

- The downstream daemon builds with privacy store, keeper, module, query gateway, and tx command included.
- The small V4 privacy metadata and public initial audit key/PoP are present in genesis.
- A local single-node chain passes deposit, transfer, disclosure decode, and withdraw.
- V1 wallet scan/tree/reserve/asset queries and the separate V2 audit configuration/key-schedule/key-history queries respond correctly.
- The four-circuit identity, batch development artifact readiness, direct core integration, deterministic gas, atomic rollback, and typed scan/minimal-event tests pass.
- The integration record distinguishes the implemented Go SDK/prover/wallet/payroll/CLI reference surfaces for batch integration from work still owned by the downstream product, and states that formal production artifacts are not supplied.
- Audit epoch private-key custody and governance rotation policy are reflected in production operations docs.
- Wallet storage encryption and remote prover privacy policy are reflected in JS/TS SDK or web wallet design docs.
- Downstream-specific EVM/policy/precompile integration is separated into separate tests.
- Any V2 EVM/policy adapter uses only `DepositWithFunderV2`, authenticates Creator/value/fixed escrow, supplies delegated collector decode/receipt verification, and proves outer state-and-event rollback. The legacy V1 `DepositWithFunder` path remains disabled under the audit runtime.
