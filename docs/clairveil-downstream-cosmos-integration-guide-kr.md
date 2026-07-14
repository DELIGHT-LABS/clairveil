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
require github.com/DELIGHT-LABS/clairveil v0.2.0

replace github.com/DELIGHT-LABS/clairveil => ../clairveil
```

릴리스 태그를 쓰기 시작하면 `replace`를 제거하고 특정 버전 또는 commit pseudo-version으로 고정합니다.

```bash
go get github.com/DELIGHT-LABS/clairveil@<tag-or-commit>
go mod tidy
```

Cosmos SDK 버전, CometBFT 버전, gogoproto/grpc-gateway 버전은 downstream app과 Clairveil의 `go.mod`가 충돌하지 않는지 먼저 확인해야 합니다. 충돌이 크면 module import보다 먼저 dependency alignment commit을 따로 만드는 편이 안전합니다.

## 3. Proto 계약

privacy proto package는 아래입니다.

```text
clairveil.privacy.v1
```

Go package는 아래로 생성됩니다.

```text
github.com/DELIGHT-LABS/clairveil/x/privacy/types
```

주요 proto 파일은 아래입니다.

```text
proto/clairveil/privacy/v1/tx.proto
proto/clairveil/privacy/v1/query.proto
proto/clairveil/privacy/v1/genesis.proto
```

Msg service는 아래 메시지를 제공합니다.

```text
/clairveil.privacy.v1.Msg/Deposit
/clairveil.privacy.v1.Msg/Transfer
/clairveil.privacy.v1.Msg/Withdraw
/clairveil.privacy.v1.Msg/BatchTransfer
```

Query service는 아래 HTTP gateway path를 제공합니다.

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

Downstream repo가 별도 proto generation pipeline을 갖고 있다면 `proto/clairveil/privacy/v1/*.proto`를 포함시키고, stale generated file이 남지 않도록 한 commit에서 generation 결과까지 같이 갱신해야 합니다.

`scan_events`는 `(height, sequence)` cursor를 사용합니다. `limit`은 scan cursor page budget을 제한하므로, filter 때문에 `events=[]`, `has_more=true`인 page가 올 수 있습니다. Wallet client는 이 경우를 sync 완료로 보지 말고 `next_height`, `next_sequence`로 cursor를 전진시켜 계속 스캔해야 합니다.

`privacy_scan`은 Deposit, native 2x2 JoinSplit, BatchJoinSplit16x32, zero-output withdraw summary를 위한 typed state projection입니다. Lexicographic cursor `(height, global_sequence, output_index)`, `privacy-sequence-v1`, `privacy-scan-v2`를 사용합니다. `commitment_paths_at_root`는 exact root/height snapshot 하나에서 최대 16개 path를 반환하며 remote 사용은 query provider에게 input linkage를 노출할 수 있습니다.

Downstream web/mobile client는 batch `nullifiers` check에 POST JSON body binding을 사용하되, 요청당 1000개 단위로 chunk해야 합니다. GET은 작은 compatibility call을 위해 유지하지만, 큰 nullifier batch는 일반적인 URL 길이 제한을 넘기 쉽습니다.

`MsgWithdraw`에는 output note 필드가 없습니다. 이전 generated binding에서 업그레이드하는 downstream client는 legacy withdraw 값인 `new_note_commitment`, `encrypted_note`를 dummy output note bytes로 보내지 말고 제거해야 합니다.

`MsgTransfer`는 encrypted output note 2개와 2-byte `view_tags` 2개를 포함합니다. 이 tag는 untrusted local-scan hint이며, server-filterable ownership tag가 아닙니다. 안전한 기본 wallet sync는 product가 recovery/rescan을 갖춘 fast mode를 명시적으로 켜지 않는 한 tag mismatch에서도 full decrypt를 수행해야 합니다. Downstream EVM precompile, binding, generated client는 `new_commitments`, `cipher_texts`, `view_tags`를 output index 기준으로 항상 맞춰야 합니다.

`MsgBatchTransfer`는 proof 하나, historical root 하나, ordered nullifier 1..16개, structured `BatchTransferOutput` 1..32개, exact audit key ID/epoch/target, expiry를 포함합니다. Count는 repeated field length에서만 나옵니다. Keeper는 frozen 12 public value를 다시 derive하고 `BatchGasModelV1`을 precharge하며 batch VK를 검증한 뒤 nullifier, globally unique commitment, root snapshot, typed scan state, minimal event를 atomic하게 씁니다. Public-input schema SHA-256은 `5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333`입니다.

## 4. App wiring 체크리스트

아래 import를 downstream app에 추가합니다.

```go
import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacykeeper "github.com/DELIGHT-LABS/clairveil/x/privacy/keeper"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
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

## 6. Genesis audit key

최신 transfer 모델은 모든 shielded transfer에 mandatory master-auditor disclosure를 포함합니다. 따라서 production-like chain은 genesis의 privacy state에 완전한 audit key identity를 설정해야 합니다.

genesis 필드 이름은 아래입니다.

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

로컬에서는 disclosure key를 CLI로 확인할 수 있습니다.

```bash
clairveild tx privacy show-disclosure-pubkey \
  --from auditor \
  --keyring-backend test \
  --output json
```

CLI 출력의 `public_key_hex`는 hex입니다. genesis에는 bytes가 JSON으로 들어가므로 base64로 변환해서 넣습니다.

```bash
printf '%s' '<public_key_hex>' | xxd -r -p | base64
```

개발 중 audit key를 비워둔 chain은 query나 genesis validation은 통과할 수 있어도, 최신 transfer UX의 목표 상태와 다릅니다. downstream e2e는 audit key를 설정한 상태에서 돌려야 합니다.

### 6.1 Audit private key custody

Clairveil repo는 audit master public key를 genesis/config에 넣고, audit disclosure를 decode하는 CLI/SDK flow를 제공합니다. 그러나 audit master private key의 생성, 보관, 접근 통제, 회전, 사고 대응은 downstream production project의 책임입니다.

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

node는 proving/verifying artifact 위치와 checksum 정책을 알아야 합니다.

```bash
export CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=/path/to/zk_artifacts
export CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict
```

artifact checksum env 파일을 만들려면 아래 명령을 사용합니다.

```bash
go run ./cmd/clairveil-setup \
  --out /path/to/zk_artifacts

set -a
source /path/to/zk_artifacts/privacy_zk_checksums.env
set +a
```

Required `privacy-note-v1` 순서는 `deposit`, `spend`, `joinsplit`, `batch-joinsplit-16x32-v1`입니다. Validator는 exact consensus identity 비교 뒤 네 required VK만 load하고 prover는 선택한 R1CS/PK pair를 lazy load합니다. 기록된 development batch artifact는 R1CS `122,813,535 B` / `fc494191a1662e46c63dacaa0967e48ec64b21ed45dc0e8bb70b6a4aa088f210`, PK `209,218,621 B` / `9c53a14d5a7e4e20aaf1207426eaecac62ff240aff8a4f1f2dd8f3986f262470`, VK `716 B` / `7359bea73f43d2cb854bd5e5aaa682d467ebb472322d623a4c5fa52c4aed2621`입니다. 이는 development identity이며 production distribution/formal setup artifact가 아닙니다.

권장 모드는 아래입니다.

- `strict`: CI, release candidate, production-like node에서 사용합니다. artifact 누락이나 checksum mismatch를 시작 전에 막습니다.
- `warn`: 개발 중 artifact 문제를 로그로 보고 싶을 때만 사용합니다.
- `off`: 특수 디버깅 외에는 권장하지 않습니다.

## 8. CLI/API 연결

downstream daemon은 root command에 module tx/query command를 노출해야 합니다. privacy module의 `AppModuleBasic`은 아래 command를 제공합니다.

```go
privacy.AppModuleBasic{}.GetTxCmd()
privacy.AppModuleBasic{}.GetQueryCmd()
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

현재 batch reference integration surface에는 one-proof `MsgBatchTransfer` command인 `transfer-batch-16x32`, `prepare-batch-transfer`, `prove-batch-transfer`, `broadcast-batch-transfer`와 companion prover route `POST /v1/proofs/batch-transfer`가 있습니다. 이전 `transfer-batch` command는 의도적으로 다른 기능이며, 하나의 Cosmos transaction envelope에 여러 독립 `MsgTransfer`를 넣습니다. 이 legacy command를 one-proof batch protocol로 문서화하거나 wiring하면 안 됩니다.

현재 query CLI로 직접 노출된 command는 아래입니다.

```text
query privacy check-nullifier
query privacy reserve uclair
```

나머지 `tree_state`, `commitment_info`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, batch `nullifiers` query는 gRPC/HTTP gateway로 제공됩니다. Downstream chain에서 운영자 CLI가 필요하면 이 query들을 별도 CLI wrapper로 추가하면 됩니다.

## 9. Downstream 테스트 순서

처음부터 target chain의 모든 기능과 섞지 말고 아래 순서로 올리는 것을 권장합니다.

1. Clairveil repo에서 `make privacy-e2e-smoke`가 통과하는지 확인합니다.
2. Downstream app에 module import와 app wiring만 추가합니다.
3. Downstream node에서 `init`, genesis account, gentx, collect-gentxs, `start`가 되는지 확인합니다.
4. Genesis에 audit master pubkey를 넣고 첫 블록 이후 gRPC/HTTP gateway의 `audit_config`가 값을 반환하는지 확인합니다.
5. Downstream CLI로 `show-address`, `deposit`, `list-notes`를 먼저 검증합니다.
6. gRPC/HTTP gateway로 `tree_state`, `events`, `scan_events`, `merkle_path`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, `nullifiers`가 정상 응답하는지 확인합니다.
7. `transfer`와 `decode-transfer-disclosure`로 user disclosure와 audit disclosure를 검증합니다.
8. `withdraw`, `prepare-withdraw`, `relay-withdraw`로 direct/relayed withdraw를 검증합니다.
9. 마지막에 EVM/policy/precompile 연동 e2e를 추가합니다.
10. Web wallet 또는 JS SDK가 local note storage encryption, remote prover timeout/auth, disclosure verification을 자체 테스트로 검증합니다.
11. Downstream `MsgBatchTransfer` adapter를 작성하기 전에 `TestBatchTransferDirectCoreIntegration`, `TestBatchTransferCoreRejectionsAndAtomicScanFailure`, `TestCrossMessageNullifierFailureRollsBackWholeCosmosTxCache`를 실행합니다.

## 10. 자주 깨지는 지점

- Proto package, generated Go package, service descriptor가 서로 어긋나면 Msg service registration 또는 signing에서 실패합니다.
- root command의 client context에 `TxConfig`가 설정되지 않으면 gentx/signing 계열 command가 깨질 수 있습니다.
- node 시작 직후 첫 블록 전에는 privacy tx가 `invalid height`로 실패할 수 있으므로 e2e harness는 첫 블록을 기다려야 합니다.
- audit master pubkey를 넣지 않으면 mandatory audit disclosure가 있는 최신 transfer UX를 제대로 검증할 수 없습니다.
- audit master private key를 개발용 keyring/test mnemonic 기준으로 운영하면 disclosure custody boundary가 무너집니다.
- web wallet이 note cache나 prepared payload를 plaintext browser storage와 telemetry에 남기면 shielded UX의 실질 privacy가 크게 약해집니다.
- module account 권한 또는 blocked address 정책이 잘못되면 deposit/withdraw bank transfer가 실패합니다.
- direct bank send 또는 manual top-up이 기록된 deposit/withdraw accounting과 맞지 않으면 `reserve/{denom}`이 `invariant_holds=false`를 반환합니다.
- downstream denom을 바꾸면 tutorial, smoke script, JS SDK fixture, conformance vector의 denom도 같이 바꿔야 합니다.
- Genesis/state가 아직 three-circuit descriptor만 pin하거나 local artifact에 batch VK가 없으면 startup/readiness가 실패해야 합니다. `MsgBatchTransfer`를 켜기 위해 identity check를 우회하면 안 됩니다.

## 11. 완료 기준

Downstream 통합은 아래가 모두 통과하면 1차 완료로 봅니다.

- downstream daemon이 privacy store, keeper, module, query gateway, tx command를 포함해서 build됩니다.
- genesis에 privacy state와 audit master pubkey가 들어갑니다.
- local single-node에서 deposit, transfer, disclosure decode, withdraw가 모두 통과합니다.
- `tree_state`, `events`, `scan_events`, `merkle_path`, `audit_config`, `disclosure_config`, `circuit_config`, `reserve/{denom}`, `assets/by_denom`, `assets/by_id`, `privacy_scan`, `commitment_paths_at_root`, `nullifier/{nullifier}`, `nullifiers` query가 정상 응답합니다.
- Four-circuit identity, batch development artifact readiness, direct core integration, deterministic gas, atomic rollback, typed scan/minimal-event test가 통과합니다.
- Integration record는 구현된 batch integration용 Go SDK/prover/wallet/payroll/CLI reference surface와 downstream product가 맡을 작업을 구분하고, formal production artifact는 제공되지 않음을 명시합니다.
- audit master private key custody policy가 production 운영 문서에 반영되어 있습니다.
- wallet storage encryption과 remote prover privacy policy가 JS/TS SDK 또는 web wallet 설계 문서에 반영되어 있습니다.
- downstream 전용 EVM/policy/precompile 연동은 별도 테스트로 분리되어 있습니다.
