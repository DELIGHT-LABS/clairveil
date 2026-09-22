# Clairveil Downstream Cosmos SDK 통합 가이드

이 문서는 `github.com/DELIGHT-LABS/clairveil/x/privacy`를 실제 Cosmos SDK 기반 체인에 가져다 붙일 때 확인해야 하는 구현 체크리스트입니다. Clairveil standalone repo는 privacy 기능을 독립적으로 개발, 테스트, 문서화하는 core이고, 실제 체인은 이 모듈을 import해서 자기 app wiring, EVM, policy, precompile, 운영 정책과 연결합니다.

## 1. 통합 모델

권장 모델은 아래처럼 역할을 나누는 것입니다.

- Clairveil repo는 `x/privacy`, proto, Go SDK helper, conformance fixture, prover contract, reference daemon을 제공합니다.
- Downstream 체인은 `x/privacy`를 import하고 자신의 `app.go`, genesis, CLI/API, 테스트넷 설정에 연결합니다.
- EVM, policy module, precompile, fee policy, 권한 정책은 downstream 체인에서 구현합니다.
- Clairveil reference daemon인 `clairveild`는 “모듈이 단독으로 완주되는지”를 검증하는 호스트이며, downstream app을 대체하지 않습니다.
- Batch chain core는 production `MsgBatchTransfer` 처리 경로와 네 번째 circuit을 제공합니다. Batch reference integration은 reference Go SDK, bounded remote prover, typed wallet/payroll, batch CLI/tutorial surface를 추가합니다. Concrete package와 fixture를 import하고 proto만 보고 downstream JS/product contract를 추론하면 안 됩니다.

## 2. Go module 의존성

초기 개발 중에는 로컬 `replace`를 쓰면 빠릅니다.

```go
require github.com/DELIGHT-LABS/clairveil v0.5.1

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

릴리스 태그를 쓰기 시작하면 `replace`를 제거하고 특정 버전 또는 commit pseudo-version으로 고정합니다.

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

Cosmos SDK 버전, CometBFT 버전, gogoproto/grpc-gateway 버전은 downstream app과 Clairveil의 `go.mod`가 충돌하지 않는지 먼저 확인해야 합니다. 충돌이 크면 module import보다 먼저 dependency alignment commit을 따로 만드는 편이 안전합니다.

## 3. Proto 계약

현재 transaction과 audit-query proto package는 아래입니다.

```text
clairveil.privacy.v2
```

Generated Go package는 `github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2`입니다. 보존된 V1 wallet scan/tree/reserve query와 legacy contract는 `clairveil.privacy.v1`, `github.com/DELIGHT-LABS/clairveil/x/privacy/types`를 사용합니다.

주요 proto 파일은 아래입니다.

```text
proto/clairveil/privacy/v2/tx.proto
proto/clairveil/privacy/v2/query.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

Current V2 Msg service는 아래 message를 제공합니다.

```text
/clairveil.privacy.v2.Msg/Deposit
/clairveil.privacy.v2.Msg/Transfer
/clairveil.privacy.v2.Msg/Withdraw
/clairveil.privacy.v2.Msg/BatchTransfer
/clairveil.privacy.v2.Msg/ScheduleAuditKeyEpoch
/clairveil.privacy.v2.Msg/CancelPendingAuditEpoch
/clairveil.privacy.v2.Msg/SetPrivacyHalt
```

Audit runtime은 V1 Msg service를 등록하지 않으며 legacy 호출은 `legacy privacy service is disabled`로 실패합니다. Governance는 audit-management message 세 개를 실행할 수 있지만 asset message 네 개의 governance 실행은 명시적으로 차단됩니다.

Live V2 audit query는 아래입니다.

```text
GET /clairveil/privacy/v2/audit/configuration
GET /clairveil/privacy/v2/audit/key_schedule
GET /clairveil/privacy/v2/audit/keys/{epoch}
```

V1 Query service는 wallet scan/tree/reserve/asset read를 위해 계속 등록되며 아래 HTTP gateway path를 제공합니다.

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

Downstream repo가 별도 proto generation pipeline을 갖고 있다면 `proto/clairveil/privacy/v1/*.proto`와 `proto/clairveil/privacy/v2/*.proto`를 모두 포함시키고, stale generated file이 남지 않도록 한 commit에서 generation 결과까지 같이 갱신해야 합니다.

`scan_events`는 `(height, sequence)` cursor를 사용합니다. `limit`은 scan cursor page budget을 제한하므로, filter 때문에 `events=[]`, `has_more=true`인 page가 올 수 있습니다. Wallet client는 이 경우를 sync 완료로 보지 말고 `next_height`, `next_sequence`로 cursor를 전진시켜 계속 스캔해야 합니다.

`privacy_scan`은 Deposit, native 2x2 JoinSplit, BatchJoinSplit16x32, zero-output withdraw summary를 위한 typed state projection입니다. Lexicographic cursor `(height, global_sequence, output_index)`, `privacy-sequence-v1`, `privacy-scan-v2`를 사용합니다. `commitment_paths_at_root`는 exact root/height snapshot 하나에서 최대 16개 path를 반환하며 remote 사용은 query provider에게 input linkage를 노출할 수 있습니다.

Downstream web/mobile client는 batch `nullifiers` check에 POST JSON body binding을 사용하되, 요청당 1000개 단위로 chunk해야 합니다. GET은 작은 compatibility call을 위해 유지하지만, 큰 nullifier batch는 일반적인 URL 길이 제한을 넘기 쉽습니다.

아래 NoteV1 field 설명은 보존 V1 fixture와 inner circuit relation을 설명하며 current transaction wire가 아닙니다. Current V2 asset message는 `proto/clairveil/privacy/v2/tx.proto`에 정의된 `AuditAuthorization`, proof/expiry, structured output effect를 사용합니다.

Legacy `MsgWithdraw`에는 output note 필드가 없습니다. 보존 V1 fixture를 다루는 client는 더 오래된 `new_note_commitment`, `encrypted_note` 값을 dummy output note bytes로 보내지 말고 제거해야 합니다.

Legacy `MsgTransfer` fixture는 encrypted output note 2개와 2-byte `view_tags` 2개를 포함합니다. 이 tag는 untrusted local-scan hint이며 server-filterable ownership tag가 아닙니다. 안전한 기본 wallet sync는 product가 recovery/rescan을 갖춘 fast mode를 명시적으로 켜지 않는 한 tag mismatch에서도 full decrypt를 수행해야 합니다.

보존 V1 `MsgBatchTransfer` fixture는 proof, historical root, ordered nullifier, structured output을 포함합니다. Current V2는 같은 asset transition을 V2 output effect와 mandatory `AuditAuthorization`으로 표현하므로 wire field는 compiled V2 proto를 따릅니다. Legacy public-input schema SHA-256은 V2 message schema가 아니라 역사적 conformance evidence입니다.

## 4. App wiring 체크리스트

Current audit runtime은 `privacy.AppModuleBasic{AuditRuntime: true}`와 `ConfigureAuditRuntime`를 포함한 audited reference path로 wiring해야 합니다. Plain legacy `NewKeeper`/`AppModuleBasic{}` setup은 V2 service를 등록하지 않습니다. `app.NewAuditFieldApp`은 reference host 구현입니다. Downstream app은 자신의 `InitChainer`를 유지하고 V4 초기화에는 public `privacy.RunAuditGenesis`, `privacy.ComputeAuditGenesisAnchor` helper를 사용해야 합니다. 아래 snippet은 공통 Cosmos module-account/store scaffolding만 보여줍니다.

아래 import를 downstream app에 추가합니다.

```go
import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacykeeper "github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)
```

module account permission에 privacy module account를 추가합니다.

```go
var maccPerms = map[string][]string{
	privacytypes.ModuleName: nil,
}
```

store key에 privacy store를 추가합니다.

```go
keys := storetypes.NewKVStoreKeys(
	privacytypes.StoreKey,
)
```

app struct에 keeper를 추가합니다.

```go
type App struct {
	PrivacyKeeper privacykeeper.Keeper
}
```

keeper를 생성합니다.

```go
app.PrivacyKeeper = *privacykeeper.NewKeeper(
	appCodec,
	runtime.NewKVStoreService(keys[privacytypes.StoreKey]),
	app.GetSubspace(privacytypes.ModuleName),
	app.BankKeeper,
)
```

module manager에 AppModule을 추가합니다.

```go
app.ModuleManager = module.NewManager(
	privacy.NewAppModule(appCodec, app.PrivacyKeeper),
)
```

genesis order와 export order에 privacy module을 포함합니다.

```go
genesisModuleOrder := []string{
	privacytypes.ModuleName,
}

app.ModuleManager.SetOrderInitGenesis(genesisModuleOrder...)
app.ModuleManager.SetOrderExportGenesis(genesisModuleOrder...)
```

Basic module manager가 interface와 gRPC gateway route를 등록할 수 있어야 합니다.

```go
app.BasicModuleManager.RegisterInterfaces(interfaceRegistry)
app.BasicModuleManager.RegisterGRPCGatewayRoutes(clientCtx, apiSvr.GRPCGatewayRouter)
```

서비스 등록은 module manager를 통해 이뤄져야 합니다.

```go
if err := app.ModuleManager.RegisterServices(app.configurator); err != nil {
	panic(err)
}
```

### V4 genesis 초기화

Audit runtime을 켠 경우 전체 privacy genesis sequence를 `CacheContext`와 `privacy.RunAuditGenesis` 안에서 실행합니다. V4 metadata를 초기화하거나 load하고 marked context로 `ModuleManager.InitGenesis`를 호출한 뒤 결과를 검증하며, 모든 단계가 성공한 뒤에만 cache를 publish합니다. `internal/auditinit`을 import하거나 context marker를 다시 만들면 안 됩니다.

아래 예제는 privacy 초기화·검증 분기를 보여줍니다. `genesis`는 `privacytypes.ParseFreshGenesisV4`로 파싱한 V4 상태이고, `genesisState`는 전체 module genesis map입니다. Host `InitChainer`는 `app/audit_init.go`처럼 chain·최초/재시작 높이 검사, module version 초기화, 같은 cache에서의 genesis transaction 실행과 publish 전 bank 검증도 유지해야 합니다. Export 복원 시 anchor에는 원래의 `genesis.InitialHeight`를 사용합니다.

```go
cache, publish := ctx.CacheContext()
anchor, err := privacy.ComputeAuditGenesisAnchor(chainID, genesis.NetworkNonce, genesis.InitialHeight)
if err != nil {
	return nil, err
}
var response *abci.ResponseInitChain
if err := privacy.RunAuditGenesis(cache, func(init sdk.Context) error {
	if genesis.State == nil {
		if err := app.PrivacyKeeper.InitializeFreshAudit(init, genesis, anchor); err != nil {
			return err
		}
	} else {
		if err := app.PrivacyKeeper.SetCircuitSetIdentity(init, genesis.CircuitSetIdentity); err != nil {
			return err
		}
		if err := app.PrivacyKeeper.InitializeAuditMetadata(init, genesis, anchor); err != nil {
			return err
		}
		privacy.InitGenesis(init, app.PrivacyKeeper, *genesis.State)
	}
	response, err = app.ModuleManager.InitGenesis(init, appCodec, genesisState)
	if err != nil {
		return err
	}
	if genesis.State == nil {
		return app.PrivacyKeeper.ValidateFreshAudit(init, genesis, anchor)
	}
	return app.PrivacyKeeper.ValidateLoadedAuditIdentity(init)
}); err != nil {
	return nil, err
}
publish()
return response, nil
```

## 5. BankKeeper 요구사항

privacy module은 transparent 자산을 shielded pool module account로 이동시키고, withdraw 때 다시 recipient에게 보냅니다. 따라서 downstream `BankKeeper`는 최소 아래 메서드가 동작해야 합니다.

```go
GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
SendCoinsFromAccountToModule(ctx context.Context, sender sdk.AccAddress, recipientModule string, amt sdk.Coins) error
SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipient sdk.AccAddress, amt sdk.Coins) error
```

주의할 점은 아래입니다.

- `privacy` module account가 genesis에 생성되어야 합니다.
- deposit recipient module account가 blocked address 정책 때문에 송금을 거부당하면 안 됩니다.
- withdraw recipient는 일반 account address prefix를 따라야 합니다.
- downstream denom policy가 있다면 `uclair` 대신 실제 denom을 쓰되, CLI/tutorial/fixture도 함께 바꿔야 합니다.

### 5.1 Trusted deposit funding

In-process V2 EVM precompile 또는 policy adapter는 additive Keeper API를 호출해 proof-bound principal과 다른 고정 transparent escrow를 debit할 수 있습니다.

```go
resp, err := app.PrivacyKeeper.DepositWithFunderV2(ctx, msg, escrow)
```

이 API는 trusted Go integration surface이며 protobuf Msg service가 아닙니다. Public V2 `MsgDeposit` protobuf, gRPC, CLI, proof public input, 일반 `MsgServer.Deposit` path는 바뀌지 않습니다. `msg.Creator`는 authenticated original principal, proof public target, provenance identity로 유지되고 `funder`는 debit 및 `Lock` 대상 account에만 사용됩니다. Funder는 복제되며 canonical account여야 하고 `privacy` 및 governance module account와 달라야 합니다.

Proof는 escrow funder를 직접 bind하지 않습니다. 따라서 trusted downstream adapter는 아래 invariant를 모두 강제해야 합니다.

- `msg.Creator`를 user-supplied calldata에서 받지 않고 original EVM caller/operator로부터 derive하고 인증하며 original provider identity로 유지합니다.
- `funder`에는 고정된 precompile escrow만 전달하고 caller-selected funder는 노출하지 않습니다.
- Caller-to-escrow value 이동이 완료됐고 `MsgDeposit.Amount`가 runtime native denom의 `msg.value`와 정확히 같은지 인증합니다.
- Bank send restriction이 escrow-to-`privacy` module transfer를 redirect 또는 suppress하지 않게 합니다. Clairveil은 nested cache 안에서 escrow와 module의 정확한 balance delta를 검증합니다.
- Keeper API 호출 전에 downstream-specific EVM-to-Cosmos address mapping과 expected address length를 검증합니다.

V2 core는 verified transition이 nested apply cache에 도달하기 전에 일반 halt/epoch/gas/public-input/proof/reentrancy 검사를 그대로 재사용합니다. 검증 후에는 deposit bank endpoint만 escrow로 바뀌고 reserve, commitment, typed scan, event write는 atomic하게 유지됩니다. Delegated success event에만 bounded canonical V2 deposit protobuf와 escrow funder가 추가되며 native V2 event는 minimal 상태를 유지합니다. Typed scan state에는 계속 proof 또는 audit ciphertext를 넣지 않고 별도 audit ledger도 만들지 않습니다.

Clairveil success는 caller의 parent context에만 publish합니다. Downstream adapter는 caller-to-escrow value 이동, `DepositWithFunderV2`, event emission, 후속 policy check를 하나의 outer SDK/EVM rollback boundary 안에 둬야 합니다. Parent cache 폐기 시 balance, Clairveil state, event가 모두 사라져야 합니다. 이 repository는 특정 EVM precompile 구현이 event rollback을 제공한다고 주장하지 않으며 downstream wiring에서 별도로 검증해야 합니다.

External auditor가 delegated event를 읽을 때 downstream은 EVM wrapper를 이해하는 `sdk.TxDecoder`와 wrapper/receipt 성공 및 Creator/funder 연결을 인증하는 `VerifyDelegatedExecution` callback을 주입해야 합니다. Cosmos `Code == 0`만으로는 internal EVM call이 revert하지 않았다고 단정할 수 없습니다. Callback이 없거나 evidence를 거부하면 `CosmosTxSource`는 `AUDIT_INCOMPLETE`로 fail closed합니다. Wrapper message index 하나에서 성공한 internal deposit 여러 개는 `global_sequence`와 `execution_id`로 구분하며 native top-level V2 message는 계속 정확한 one-to-one event match가 필요합니다. Standalone downstream EVM decoder와 receipt wiring은 Clairveil 범위 밖입니다.

## 6. V4 audit configuration과 key epoch

Current initialization은 `--audit-config`로 작은 public V4 configuration을 입력받습니다. 여기에는 `chain_id`, `network_nonce32`, `initial_height`, public `initial_audit_key`(`epoch`, `key_id`, `suite`, `public_key`, `pop`), `circuit_set_identity`가 들어갑니다. Audit private key, replay archive, runtime bundle은 들어가지 않습니다. `clairveild init`은 이 public metadata를 검증해 쓰고, `start`와 `export`는 같은 configuration과 `--audit-artifacts`를 요구합니다.

Genesis 뒤에는 governance가 V2 management message 세 개로 future key epoch를 schedule하고 pending epoch를 cancel하거나 privacy halt를 변경합니다. Client는 V2 audit configuration, key schedule, epoch history를 query합니다. `show-disclosure-pubkey`는 user/self-view disclosure helper이며 initial audit key 생성에 사용하면 안 됩니다.

### 6.1 Audit private key custody

Clairveil은 public initial key/PoP를 입력받고 original successful transaction과 execution event를 검증하는 external auditor를 제공합니다. Audit private key의 생성, 보관, 접근 통제, epoch 회전, 사고 대응은 downstream production project의 책임입니다.

이 키는 일반 relayer key나 개발용 test key처럼 취급하면 안 됩니다. 유출되면 해당 chain의 mandatory audit disclosure로 암호화된 transfer metadata를 읽을 수 있습니다.

Production-like downstream chain은 최소 아래 정책을 정해야 합니다.

- audit private key 생성 ceremony와 승인자를 정합니다.
- key를 plaintext file, git, docker image, CI variable dump에 두지 않습니다.
- HSM, KMS, Vault, secure enclave, offline custody 중 하나를 선택합니다.
- 누가 어떤 조건에서 disclosure decrypt를 할 수 있는지 역할을 나눕니다.
- decrypt operation audit log와 접근 승인 기록을 남깁니다.
- key rotation과 compromised-key incident response 절차를 문서화합니다.
- local tutorial의 `--keyring-backend test` auditor key는 production custody 예시가 아니라는 점을 운영 문서에 명시합니다.

### 6.2 Wallet storage and prepared payload custody

Clairveil reference CLI는 local wallet note cache와 prepared payload/proof JSON을 `0600` file permission으로 저장합니다. 이것은 sample chain과 개발 환경에는 실용적인 기본값이지만, web wallet 또는 production wallet의 encrypted storage 정책을 대체하지 않습니다.

Downstream wallet은 아래 데이터를 privacy-sensitive local data로 분류해야 합니다.

- root seed 또는 root signer material
- spend/view/disclosure secret
- local note cache
- note amount, randomness, nullifier, merkle path
- prepared transfer payload
- prepared withdraw prover payload
- disclosure plaintext와 decrypted report

Web wallet 또는 external wallet SDK는 최소 아래를 정해야 합니다.

- browser storage에 그대로 plaintext note DB를 두지 않을지 결정합니다.
- password-derived key, platform keystore, hardware wallet, secure enclave, server-side KMS 등 storage encryption 방식을 선택합니다.
- prepared payload를 remote prover에 보낼 때 사용자가 어떤 metadata를 prover에 맡기는지 threat model에 포함합니다.
- telemetry, crash report, debug log에 payload body, bearer token, seed, disclosure plaintext가 들어가지 않게 redaction policy를 둡니다.

## 7. ZK artifact 런타임 설정

Node와 prover는 같은 identity-pinned artifact directory를 사용해야 합니다.

```bash
clairveild start \
  --audit-config /path/to/audit-config.json \
  --audit-artifacts /path/to/zk_artifacts

CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/path/to/zk_artifacts clairveil-proverd
```

artifact checksum env 파일을 만들려면 아래 명령을 사용합니다.

```bash
go run ./cmd/clairveil-setup \
  --out /path/to/zk_artifacts --development

```

현재 development 순서는 `privacy-note-v1-audit-field-v1`: `deposit-audit-field-v1`, `spend-audit-field-v1`, `joinsplit-2x2-audit-field-v1`, `batch-joinsplit-16x32-audit-field-v1`입니다. Validator는 exact consensus identity 비교 뒤 matching VK 네 개를 load하고 prover는 선택한 R1CS/PK pair를 lazy load합니다. `clairveil-setup`은 `--out`, `--development`만 지원합니다. 이전 `--circuit`/`--overwrite` 절차와 batch artifact measurement는 현재 runtime evidence가 아닌 legacy record입니다.

Node startup은 manifest와 consensus circuit identity를 자동으로 검증합니다. Generated checksum env file은 external release tooling에서 사용할 수 있지만 필수 `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE` runtime switch는 없습니다.

## 8. CLI/API 연결

downstream daemon은 root command에 module tx/query command를 노출해야 합니다. privacy module의 `AppModuleBasic`은 아래 command를 제공합니다.

```go
privacy.AppModuleBasic{AuditRuntime: true}.GetTxCmd()
privacy.AppModuleBasic{AuditRuntime: true}.GetQueryCmd()
```

현재 사용자-facing tx CLI에서 확인해야 하는 주요 command는 아래입니다.

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

Current one-proof V2 batch submission은 `clairveil.privacy.v2.MsgBatchTransfer`와 공통 companion prover route `POST /v2/prover/audit-field`를 사용합니다. 기존 `/v1/proofs/batch-transfer` route와 V1 staged contract는 보존 문서/fixture이며 live fallback endpoint가 아닙니다. Legacy `transfer-batch`도 여러 독립 V1 message를 보내므로 current batch protocol이 아닙니다.

현재 query CLI로 직접 노출된 command는 아래입니다.

```text
query privacy check-nullifier
query privacy reserve uclair
```

나머지 V1 wallet scan/tree/reserve/asset query는 gRPC/HTTP gateway로 제공됩니다. V2 audit runtime은 `audit/configuration`, `audit/key_schedule`, `audit/keys/{epoch}`를 별도로 제공합니다. Downstream chain에서 운영자 CLI가 필요하면 V1 wallet과 V2 audit contract를 한 version으로 합치지 말고 wrapper를 추가합니다.

## 9. Downstream 테스트 순서

### 9.1 Deposit proof 획득 경계

`/v1/prover/deposit`은 공식 current remote route가 아닌 보존 legacy 문서입니다. 유일한 live route는 [현재 HTTP API](clairveil-proverd-http-api-kr.md#현재-route)의 `POST /v2/prover/audit-field`입니다. Complete witness/PI23 경계는 V2 message 전 local artifact-identity verification을 요구하며 deployment는 ad-hoc handler 대신 auth/admission/no-store 공통 경계를 보존합니다.

처음부터 target chain의 모든 기능과 섞지 말고 아래 순서로 올리는 것을 권장합니다.

1. 수동 native V2 flow 또는 작동하는 다른 audited harness의 기록을 남깁니다. 현재 checkout에는 end-to-end native V2 증적을 제공하는 Make target이 없습니다.
2. Downstream app에 module import와 app wiring만 추가합니다.
3. Downstream node에서 `init`, genesis account, gentx, collect-gentxs, `start`가 되는지 확인합니다.
4. Public V4 audit configuration으로 initialize한 뒤 첫 block 이후 V2 `audit/configuration`, `audit/key_schedule`, `audit/keys/{epoch}`를 확인합니다.
5. Downstream CLI로 `show-address`, `deposit`, `list-notes`를 먼저 검증합니다.
6. gRPC/HTTP gateway로 `tree_state`, `events`, `scan_events`, `merkle_path`, `disclosure_config`, `circuit_config`, `reserve/{denom=**}`, `assets/by_denom/{canonical_denom=**}`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, `nullifiers`가 정상 응답하는지 확인합니다.
7. `transfer`와 `decode-transfer-disclosure`로 user disclosure와 audit disclosure를 검증합니다.
8. `withdraw`, `prepare-withdraw`, `relay-withdraw`로 direct/relayed withdraw를 검증합니다.
9. 마지막에 EVM/policy/precompile 연동 e2e를 추가하고 actor provenance, fixed escrow, exact `msg.value`/native-denom binding, trusted deposit 성공 뒤 outer rollback을 검증합니다.
10. Web wallet 또는 JS SDK가 local note storage encryption, remote prover timeout/auth, disclosure verification을 자체 테스트로 검증합니다.
11. Downstream `MsgBatchTransfer` adapter를 작성하기 전에 `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`를 실행합니다.

## 10. 자주 깨지는 지점

- Proto package, generated Go package, service descriptor가 서로 어긋나면 Msg service registration 또는 signing에서 실패합니다.
- root command의 client context에 `TxConfig`가 설정되지 않으면 gentx/signing 계열 command가 깨질 수 있습니다.
- node 시작 직후 첫 블록 전에는 privacy tx가 `invalid height`로 실패할 수 있으므로 e2e harness는 첫 블록을 기다려야 합니다.
- Public initial audit key/PoP 또는 active epoch history가 없으면 V2 asset transaction이 valid audit authorization을 얻을 수 없습니다.
- Audit epoch private key를 개발용 keyring/test mnemonic 기준으로 운영하면 custody boundary가 무너집니다.
- web wallet이 note cache나 prepared payload를 plaintext browser storage와 telemetry에 남기면 shielded UX의 실질 privacy가 크게 약해집니다.
- module account 권한 또는 blocked address 정책이 잘못되면 deposit/withdraw bank transfer가 실패합니다.
- direct bank send 또는 manual top-up이 기록된 deposit/withdraw accounting과 맞지 않으면 `reserve/{denom=**}`이 `invariant_holds=false`를 반환합니다.
- downstream denom을 바꾸면 tutorial, smoke script, JS SDK fixture, conformance vector의 denom도 같이 바꿔야 합니다.
- Genesis/state가 아직 three-circuit descriptor만 pin하거나 local artifact에 batch VK가 없으면 startup/readiness가 실패해야 합니다. `MsgBatchTransfer`를 켜기 위해 identity check를 우회하면 안 됩니다.

## 11. 완료 기준

Downstream 통합은 아래가 모두 통과하면 1차 완료로 봅니다.

- downstream daemon이 privacy store, keeper, module, query gateway, tx command를 포함해서 build됩니다.
- genesis에 작은 V4 privacy metadata와 public initial audit key/PoP가 들어갑니다.
- local single-node에서 deposit, transfer, disclosure decode, withdraw가 모두 통과합니다.
- V1 wallet scan/tree/reserve/asset query와 별도 V2 audit configuration/key-schedule/key-history query가 정상 응답합니다.
- Four-circuit identity, batch development artifact readiness, direct core integration, deterministic gas, atomic rollback, typed scan/minimal-event test가 통과합니다.
- Integration record는 구현된 batch integration용 Go SDK/prover/wallet/payroll/CLI reference surface와 downstream product가 맡을 작업을 구분하고, formal production artifact는 제공되지 않음을 명시합니다.
- Audit epoch private-key custody와 governance rotation policy가 production 운영 문서에 반영되어 있습니다.
- wallet storage encryption과 remote prover privacy policy가 JS/TS SDK 또는 web wallet 설계 문서에 반영되어 있습니다.
- downstream 전용 EVM/policy/precompile 연동은 별도 테스트로 분리되어 있습니다.
- V2 EVM/policy adapter는 `DepositWithFunderV2`만 사용하고 Creator/value/fixed escrow를 인증하며 delegated collector decode/receipt verification과 outer state/event rollback을 입증합니다. Legacy V1 `DepositWithFunder` path는 audit runtime에서 계속 비활성입니다.
