# Clairveil Deposit Actor/Funder 분리 구현 계획

> 상태: **Completed record**
>
> 작업 branch: `features/deposit-funder`
>
> 작성일: 2026-07-19
>
> 기준 release: `v0.2.0`
>
> branch 시작 commit: `15031b7a51de1bead673117594a05e07d5af14ca`
>
> 구현 commit: `6f54c5bf23ffaf66d25dd34c1385e87318457a45`
>
> Source request: [clairveil-deposit-funder-separation-handoff-kr.md](clairveil-deposit-funder-separation-handoff-kr.md)

이 문서는 기존 upstream handoff를 현재 Clairveil repository에서 구현하기 위한 실행 계획이다. Public `MsgServer.Deposit`의 consensus-visible 동작을 보존하면서 trusted in-process caller만 actor와 transparent funder를 분리할 수 있게 한다.

## 1. 목표와 종료 상태

기존 public transaction은 다음 의미를 유지한다.

```text
actor  = MsgDeposit.Creator
funder = MsgDeposit.Creator
```

새 Keeper API는 다음 의미를 제공한다.

```text
actor  = MsgDeposit.Creator
funder = explicit sdk.AccAddress
```

완료 시 다음 조건이 모두 성립해야 한다.

- Public `MsgServer.Deposit`의 validation, proof, gas, mutation, event와 error ordering이 유지된다.
- Public path와 trusted path가 같은 canonical deposit transition core를 사용한다.
- Trusted path는 bank debit sender만 explicit funder로 바꾸고 actor attribution은 `msg.Creator`로 유지한다.
- Core 내부 실패는 partial bank/reserve/tree/index/event state를 남기지 않는다.
- Downstream caller는 core 성공 뒤 발생하는 policy failure까지 outer SDK/EVM snapshot으로 rollback할 수 있다.
- `MsgDeposit` protobuf, circuit, proof artifact, CLI와 client SDK wire surface는 바뀌지 않는다.
- Zero-value deposit의 기존 의미가 유지된다.

## 2. 검토 의견 반영 결정

| 검토 항목 | 구현 계획의 결정 |
| --- | --- |
| Direct Keeper API가 protobuf signer 검증을 우회함 | `DepositWithFunder`는 actor를 인증하지 않는다는 점을 GoDoc과 integration 문서에 명시하고, downstream이 authenticated EVM caller에서 `msg.Creator`를 생성하도록 요구한다. |
| Core 후반 실패의 원자성 경계가 불명확함 | Mutation 구간에 nested `CacheContext()`를 사용한다. Downstream after-policy failure는 별도의 outer cache/snapshot test로 검증한다. |
| `len(funder) == 0`은 invalid address 전체를 검증하지 못함 | `sdk.VerifyAddressFormat(funder)`를 사용한다. EVM-specific 20-byte 검증은 downstream 책임으로 둔다. |
| `msg.value`와 `MsgDeposit.Amount` binding이 Clairveil 밖에 있음 | Downstream security invariant로 exact amount, native denom, fixed escrow와 actor derivation을 명시하고 e2e gate에 포함한다. |
| 현재 deposit event에는 amount가 없음 | Event의 exact 기존 attribute set인 `creator`, `commitment`, `encrypted_note`만 유지하고 `amount`와 `funder`를 추가하지 않는다. |
| Equivalence 검증 범위가 좁음 | Balance/state/error뿐 아니라 gas, global sequence, scan output, root snapshot과 exact event attributes까지 비교한다. |
| 현재 bank mock은 sender/account/cache rollback을 표현하지 못함 | Lightweight sender assertion mock과 cache-aware bank integration fixture를 분리한다. |

## 3. 현재 기준 동작

기준 구현은 `x/privacy/keeper/msg_server.go`의 `msgServer.Deposit`이다.

### 3.1 Validation과 proof 순서

현재 observable 순서는 다음과 같다.

1. `msg.ValidateBasic()`
   1. creator Bech32/address validation
   2. `msg.Amount` normalized coin parse
   3. 64-bit non-negative shielded amount validation
   4. active note commitment의 32-byte canonical/non-zero validation
   5. non-empty proof validation
   6. canonical deposit-note encrypted envelope validation
2. `msg.Creator`의 두 번째 `sdk.AccAddressFromBech32` parse
3. `msg.Amount`의 두 번째 `sdk.ParseCoinNormalized` parse
4. shielded amount의 두 번째 range validation
5. keeper-level commitment canonicalization
6. duplicate commitment lookup
7. Merkle append capacity/cached-root precondition
8. `DepositCircuit` assignment 구성
   - `Commitment`
   - `Amount`
   - `AssetID`
9. registered asset lookup
10. Groth16 proof verification과 fixed gas charge

`ValidateBasic` 내부 검증과 이후 중복 parse를 정리하거나 재배치하지 않는다. 이번 작업은 code cleanup이 아니라 actor/funder separation이다.

### 3.2 Mutation과 index 순서

Proof 성공 뒤 순서는 다음과 같다.

1. `SendCoinsFromAccountToModule`
2. `RecordReserveDeposit`
3. `AppendCommitment`
4. SDK deposit event emit
5. privacy global sequence allocate
6. typed scan index write
7. current Merkle root snapshot write
8. indexed privacy event write

새 core에서도 이 순서를 유지한다. Nested cache는 rollback 경계만 추가하고 성공 경로의 mutation 순서를 바꾸지 않는다.

### 3.3 현재 event 계약

Deposit event의 exact attribute set은 다음 세 개다.

```text
creator
commitment
encrypted_note
```

이번 변경에서 `amount` 또는 `funder` attribute를 새로 추가하지 않는다.

## 4. 보안 계약

### 4.1 Actor provenance

Public `MsgDeposit`은 protobuf의 `creator` signer annotation과 transaction authentication을 사용한다. 반면 `DepositWithFunder`는 direct Keeper API이므로 signer authentication을 수행하지 않는다. Deposit proof도 creator를 public input으로 bind하지 않는다.

따라서 trusted caller는 다음을 보장해야 한다.

- `msg.Creator`를 user-supplied creator field에서 복사하지 않는다.
- Authenticated EVM caller/operator bytes를 downstream chain의 canonical `sdk.AccAddress`로 변환한 뒤 `String()` 값으로 설정한다.
- EVM-to-Cosmos address mapping과 expected 20-byte 형식은 downstream에서 검증한다.
- Actor attribution이 audit 근거로 사용된다는 점을 고려해 creator derivation failure를 fail closed로 처리한다.

Clairveil core는 actor의 address format은 검증하지만 actor provenance는 인증하지 않는다.

### 4.2 Funder trust

- Clairveil API는 generic Cosmos address rules에 따라 `sdk.VerifyAddressFormat(funder)`를 실행한다.
- Downstream application은 user calldata에서 funder를 받지 않는다.
- Downstream은 app wiring에 고정된 Privacy precompile escrow address만 전달한다.
- 해당 escrow address가 bank balance를 실제로 보유하고 debit 가능한지는 downstream integration precondition이다.
- Exported method를 새로운 protobuf Msg, query, CLI, IBC 또는 unauthenticated hook에 연결하지 않는다.

### 4.3 Amount와 escrow invariant

Clairveil의 source of truth는 계속 `MsgDeposit.Amount`이며 Clairveil은 EVM `msg.value`를 알지 못한다.

Downstream은 각 호출에서 다음 invariant를 보장해야 한다.

```text
parsed MsgDeposit.Amount.amount == EVM msg.value
parsed MsgDeposit.Amount.denom  == runtime native denom
funder                          == fixed Privacy precompile escrow
MsgDeposit.Creator              == canonical authenticated operator
```

EVM value transfer 직후에는 funder의 bank-visible balance가 정확히 value만큼 증가해 있어야 한다. `DepositWithFunder` 성공 뒤에는 그 value가 Privacy module balance로 한 번만 이동해야 한다. 실패 시 operator, escrow, Privacy module과 Clairveil state가 모두 호출 전 상태로 복구되어야 한다.

이 binding이 없으면 escrow에 남은 잔액이 후속 호출에서 소비될 수 있으므로 downstream e2e gate에서 반드시 검증한다.

### 4.4 Input ownership

- Core는 `MsgDeposit` 또는 내부 byte slice를 mutation하지 않는다.
- Store와 event에 필요한 canonical byte는 기존 방식대로 복사 또는 encode한다.
- Funder address를 장기 보관하거나 별도 state에 기록하지 않는다.

## 5. API와 core 구조

### 5.1 Exported Keeper API

```go
// DepositWithFunder executes the canonical Clairveil deposit transition while
// debiting funder. msg.Creator remains the actor and indexed-event creator.
//
// SECURITY: this method does not authenticate msg.Creator or authorize funder.
// The in-process caller must derive both from trusted application state and
// execute the call inside the caller's rollback boundary.
func (k Keeper) DepositWithFunder(
    ctx sdk.Context,
    msg *types.MsgDeposit,
    funder sdk.AccAddress,
) (*types.MsgDepositResponse, error)
```

이 method는 `types.MsgServer`에 등록하지 않고 Keeper의 Go integration surface로만 제공한다.

### 5.2 Public wrapper

기존 `msgServer.Deposit`은 현재와 같은 순서로 `ValidateBasic`과 creator parse를 수행한 뒤 private core를 호출한다.

```go
func (s msgServer) Deposit(
    goCtx context.Context,
    msg *types.MsgDeposit,
) (*types.MsgDepositResponse, error) {
    ctx := sdk.UnwrapSDKContext(goCtx)
    if err := msg.ValidateBasic(); err != nil {
        return nil, err
    }

    creator, err := sdk.AccAddressFromBech32(msg.Creator)
    if err != nil {
        return nil, err
    }

    return s.Keeper.depositWithValidatedFunder(ctx, msg, creator)
}
```

Public wrapper가 `DepositWithFunder`를 직접 호출해 validation을 두 번 반복하게 하지 않는다. 두 entry point는 validation 이후 transition core만 공유한다.

### 5.3 Trusted wrapper와 funder validation

Trusted wrapper도 기존 message validation/error ordering을 먼저 유지하고 funder를 core 진입 전에 검증한다.

```go
func (k Keeper) DepositWithFunder(
    ctx sdk.Context,
    msg *types.MsgDeposit,
    funder sdk.AccAddress,
) (*types.MsgDepositResponse, error) {
    if err := msg.ValidateBasic(); err != nil {
        return nil, err
    }
    if _, err := sdk.AccAddressFromBech32(msg.Creator); err != nil {
        return nil, err
    }
    if err := sdk.VerifyAddressFormat(funder); err != nil {
        return nil, errorsmod.Wrapf(
            sdkerrors.ErrInvalidAddress,
            "invalid deposit funder: %v",
            err,
        )
    }

    return k.depositWithValidatedFunder(ctx, msg, funder)
}
```

Chain-specific verifier가 설정되어 있으면 `sdk.VerifyAddressFormat`이 이를 따른다. Clairveil core에서 EVM-specific exact length를 하드코딩하지 않는다.

### 5.4 Canonical core와 nested rollback

`depositWithValidatedFunder`는 기존 `Deposit`에서 creator parse 이후의 로직을 이동한다. Validation/read/proof 단계가 성공한 뒤 mutation 단계에 nested cache를 만든다.

```go
cacheCtx, writeCache := ctx.CacheContext()

if err := k.bankKeeper.SendCoinsFromAccountToModule(
    cacheCtx,
    funder,
    types.ModuleName,
    sdk.NewCoins(coin),
); err != nil {
    return nil, errorsmod.Wrap(err, "failed to lock tokens")
}
if err := k.RecordReserveDeposit(cacheCtx, coin); err != nil {
    return nil, errorsmod.Wrap(err, "failed to record privacy reserve deposit")
}
if err := k.AppendCommitment(cacheCtx, canonicalCommitment); err != nil {
    return nil, errorsmod.Wrap(sdkerrors.ErrInvalidRequest, "failed to append the note commitment")
}
if err := k.emitIndexedPrivacyEvent(cacheCtx, types.EventTypeDeposit, eventAttrs); err != nil {
    return nil, errorsmod.Wrap(err, "failed to index deposit privacy event")
}

writeCache()
return &types.MsgDepositResponse{}, nil
```

이 nested cache는 독립적인 transaction commit이 아니다. 성공 시 caller가 전달한 parent context에만 반영되므로 outer SDK/EVM snapshot은 이후 policy failure에서 전체를 계속 폐기할 수 있다.

## 6. 파일별 작업 범위

| 파일 | 예정 작업 |
| --- | --- |
| `x/privacy/keeper/msg_server.go` | Public `Deposit`을 validation wrapper로 축소하고 기존 성공/error ordering을 보존한다. |
| `x/privacy/keeper/deposit.go` | `DepositWithFunder`, private canonical core와 mutation cache 경계를 둔다. 파일명은 구현 시 repository convention에 따라 조정할 수 있다. |
| `x/privacy/keeper/msg_server_test.go` | 기존 regression test를 유지하고 lightweight bank mock이 sender를 기록하도록 최소 확장한다. |
| `x/privacy/keeper/deposit_funder_test.go` | Actor/funder success, equivalence, validation, zero-value, input ownership test를 추가한다. |
| `x/privacy/keeper/deposit_atomicity_test.go` | Cache-aware bank fixture와 reserve/tree/index/outer-policy rollback test를 추가한다. |
| `docs/clairveil-downstream-cosmos-integration-guide.md` | Trusted API의 actor/funder/amount/rollback 보안 전제를 문서화한다. |
| `docs/clairveil-downstream-cosmos-integration-guide-kr.md` | 위 integration guidance의 한국어 pair를 유지한다. |
| `CHANGELOG.md`, `CHANGELOG-kr.md` | 구현 완료 시 additive Keeper API와 unchanged public wire format을 기록한다. |
| `plans/README.md`, `plans/README-kr.md` | 이 plan의 Active/Completed 상태를 관리한다. |

다음 파일은 변경하지 않는다.

- `proto/clairveil/privacy/v1/tx.proto`
- Generated protobuf/gRPC files
- `x/privacy/circuit/**`
- `x/privacy/zk/**`의 VK/R1CS/PK artifacts
- CLI deposit flags와 transaction JSON
- JS/TS 및 Go transaction builder wire API

## 7. 구현 단계

### Phase 0: Baseline 고정

1. Branch 시작 commit과 dirty state를 기록한다.
2. 기존 Deposit test와 `go test ./x/privacy/...`를 실행한다.
3. Public Deposit의 success/error/gas/event baseline을 test fixture로 고정한다.
4. `v0.2.0`과 branch 시작 commit 사이의 Deposit semantic diff가 없는지 확인한다.

완료 gate:

- 기존 baseline test가 통과한다.
- 기존 failure ordering과 proof gas test가 재현된다.

### Phase 1: Test infrastructure 준비

1. Existing lightweight bank mock에 마지막 account-to-module sender와 call amount 기록을 추가한다.
2. Actor, funder와 module의 denom별 balance를 SDK cache에 저장하는 integration fixture를 준비한다.
   - 실제 Cosmos BankKeeper wiring을 우선한다.
   - wiring 비용이 과도하면 dedicated KVStoreService를 사용하는 cache-aware test BankKeeper를 사용한다.
   - Go struct field만 변경하는 balance mock은 rollback test에 사용하지 않는다.
3. 두 독립 state의 reserve/tree/index/event/gas snapshot을 비교하는 helper를 만든다.
4. Msg byte slice deep copy와 exact event attribute 비교 helper를 만든다.

완료 gate:

- Sender identity와 account/module balance를 각각 관찰할 수 있다.
- Parent `ctx`와 discarded child `CacheContext`의 bank balance 차이를 검증할 수 있다.

### Phase 2: Canonical core 추출

1. Public `Deposit`에서 `ValidateBasic`과 creator parse 이후 본문을 private core로 이동한다.
2. 기존 `depositor` 사용을 bank call의 `funder`로만 치환한다.
3. Event creator와 나머지 state/proof input은 계속 `msg`에서 읽는다.
4. Mutation 구간에 nested cache를 추가하고 최종 성공 시에만 `writeCache()`를 호출한다.
5. Public Deposit 기존 tests를 즉시 실행해 error text와 gas regression을 확인한다.

완료 gate:

- Public path의 기존 test가 변경 없이 통과한다.
- Canonical deposit mutation implementation이 하나만 존재한다.
- Bank sender 외에 actor/funder 분리가 영향을 주는 필드가 없다.

### Phase 3: Trusted API 추가

1. `Keeper.DepositWithFunder`를 추가한다.
2. `msg.ValidateBasic`, creator second parse와 `sdk.VerifyAddressFormat(funder)`를 실행한다.
3. Security GoDoc을 추가한다.
4. Proto Msg service, CLI와 client builder에서 새 API가 노출되지 않았는지 diff를 확인한다.

완료 gate:

- Empty 및 chain-invalid funder가 proof/bank mutation 전에 거부된다.
- Actor와 funder가 다른 호출에서 bank sender만 funder가 된다.

### Phase 4: Functional/equivalence tests

1. `actor=funder`인 public/trusted 두 경로를 독립 state에서 실행한다.
2. `actor!=funder` 성공 case를 실행한다.
3. Zero-value case를 두 entry point에서 실행한다.
4. Msg와 byte slice가 mutation되지 않았는지 확인한다.
5. Event exact attribute set과 typed scan/index state를 확인한다.

완료 gate:

- Section 8의 equivalence와 success matrix가 모두 통과한다.

### Phase 5: Failure atomicity tests

1. Proof 이전 failure가 bank call을 수행하지 않는지 확인한다.
2. Bank failure가 Clairveil state를 변경하지 않는지 확인한다.
3. Reserve corruption으로 bank debit 이후 reserve failure를 유도하고 nested cache discard를 확인한다.
4. Merkle state corruption으로 bank/reserve 이후 append failure를 유도하고 rollback을 확인한다.
5. Privacy global sequence corruption 등으로 event index failure를 유도하고 bank/reserve/tree/event 모두 rollback되는지 확인한다.
6. Outer cache에서 trusted deposit을 성공시킨 뒤 downstream policy error를 시뮬레이션하고 outer cache를 discard한다.

완료 gate:

- Section 8의 failure/rollback matrix가 모두 통과한다.
- Failed child context의 SDK event가 parent event manager에 나타나지 않는다.

### Phase 6: Documentation과 delivery

1. Downstream integration guide pair에 security invariant와 호출 예시를 추가한다.
2. Changelog pair에 additive Keeper API를 기록한다.
3. Full Privacy/repository test를 실행한다.
4. Proto/generated/circuit/artifact diff가 없음을 확인한다.
5. Release policy에 따라 immutable commit/tag를 준비하고 downstream dependency update에 사용할 식별자를 전달한다.
6. 이 plan을 Completed record로 갱신하고 실제 test command/result를 completion ledger에 기록한다.

## 8. Test matrix

### 8.1 Public/trusted equivalence

다음 두 실행은 서로 독립적이지만 동일한 초기 state와 동일한 valid message를 사용한다.

```text
MsgServer.Deposit(msg.Creator=A)
DepositWithFunder(msg.Creator=A, funder=A)
```

비교 항목:

- response와 error type/message
- SDK gas consumed
- A와 Privacy module의 bank balance
- reserve snapshot 전체 필드
- commitment index와 leaf count
- current Merkle root와 historical/root snapshot
- privacy global sequence
- indexed `QueryPrivacyEvent`
- typed scan summary/output
- SDK event type, attribute 순서와 exact key/value
- zero-value behavior

### 8.2 Actor와 funder가 다른 성공 case

```text
actor  = A
funder = F
amount = X
```

검증:

- A balance 불변
- F balance가 정확히 X 감소
- Privacy module balance가 정확히 X 증가
- BankKeeper sender가 F와 byte-equal
- Reserve deposit이 정확히 X 증가
- Commitment가 정확히 한 번 append
- Event creator가 A
- Event attribute가 `creator`, `commitment`, `encrypted_note`만 포함
- Event/index/scan 어디에도 funder가 추가되지 않음
- Proof input, commitment와 encrypted note 의미가 public path와 동일

### 8.3 Validation/pre-mutation failure

| Case | 기대 결과 |
| --- | --- |
| Empty funder | `ErrInvalidAddress`, bank call 0회, state/event 불변 |
| Chain-invalid funder | `ErrInvalidAddress`, bank call 0회, state/event 불변 |
| Invalid proof | Bank call 0회, reserve/tree/index/event 불변 |
| Duplicate commitment | Bank call 0회, reserve/tree/index/event 불변 |
| Unregistered asset | Bank call 0회, reserve/tree/index/event 불변 |
| Merkle capacity precondition failure | Bank call 0회, reserve/tree/index/event 불변 |

### 8.4 Mutation/rollback failure

| Case | 기대 결과 |
| --- | --- |
| Insufficient funder balance | Actor/funder/module/reserve/tree/index/event 불변 |
| BankKeeper injected failure | Reserve/tree/index/event 불변 |
| Reserve record failure after debit | Nested cache가 funder/module balance와 모든 Clairveil mutation을 rollback |
| Tree append failure after reserve | Nested cache가 bank/reserve/tree/index/event를 rollback |
| Event index failure after append | Nested cache가 SDK event, sequence, scan, root snapshot, tree, reserve와 bank를 rollback |
| Downstream policy failure after core success | Outer cache/snapshot이 EVM value, bank, reserve, tree와 event/index를 모두 rollback |

### 8.5 Zero-value와 ownership

- `0<registered-denom>` deposit은 public/trusted path 모두 성공한다.
- Actor와 funder balance는 변하지 않고 commitment/event/index는 기존 규칙대로 생성된다.
- Bank call count/empty coins 처리 의미는 기존 public behavior와 일치한다.
- 성공과 실패 모두에서 caller가 제공한 `MsgDeposit` 및 모든 byte slice가 byte-equal로 유지된다.

## 9. Compatibility gate

Public path에서 다음을 고정한다.

- `ValidateBasic`과 creator parse order
- Coin parse와 shielded amount validation order
- Duplicate/capacity/asset/proof error precedence
- Deposit proof verification gas charge
- Bank/reserve/tree/event mutation order
- Existing error wrapping text와 Cosmos SDK error class
- Zero-value deposit
- Event type, attribute set/order/value
- Privacy scan/index/root snapshot semantics

새 trusted API의 funder validation 때문에 public `MsgServer.Deposit`의 gas나 error가 바뀌면 안 된다.

## 10. 검증 명령

구현 중 빠른 cycle:

```bash
go test ./x/privacy/keeper -run 'Test.*Deposit' -count=1
go test ./x/privacy/types -run 'TestMsgDeposit' -count=1
```

Phase gate:

```bash
go test ./x/privacy/... -count=1
go test ./... -count=1
git diff --check
```

Scope gate:

```bash
git diff --exit-code 15031b7a51de1bead673117594a05e07d5af14ca -- \
  proto/clairveil/privacy/v1/tx.proto \
  x/privacy/types/tx.pb.go
git diff --name-only 15031b7a51de1bead673117594a05e07d5af14ca -- \
  x/privacy/circuit x/privacy/zk
```

두 번째 명령은 output이 없어야 한다. Generated protobuf file이 추가로 존재하면 scope gate 목록에 함께 포함한다.

## 11. 권장 commit 순서

1. `docs: add deposit funder implementation plan`
2. `test: add cache-aware deposit funding fixtures`
3. `refactor: extract canonical deposit core`
4. `feat: add trusted deposit funder keeper API`
5. `test: cover deposit actor funder separation and rollback`
6. `docs: document trusted deposit funding integration`

각 commit은 독립적으로 build/test 가능하게 유지한다. 실제 commit 분리는 diff 크기와 repository 정책에 맞게 합칠 수 있지만 core extraction과 behavior change의 검토 경계는 구분한다.

## 12. Acceptance checklist

### Functional

- [x] Public `MsgServer.Deposit`이 actor=funder=`msg.Creator` 의미를 유지한다.
- [x] `Keeper.DepositWithFunder`가 explicit funder를 bank debit에 사용한다.
- [x] 하나의 canonical deposit transition core만 존재한다.
- [x] Actor는 event/index에서 계속 `msg.Creator`다.
- [x] Event에 `amount` 또는 `funder`가 추가되지 않는다.
- [x] Zero-value deposit이 유지된다.
- [x] Input message와 byte slice가 mutation되지 않는다.

### Security

- [x] GoDoc이 actor/funder authorization을 caller 책임으로 명시한다.
- [x] Empty/invalid funder가 `sdk.VerifyAddressFormat` 기준으로 거부된다.
- [x] Public protobuf/gRPC/CLI/client SDK에 funder가 노출되지 않는다.
- [x] Downstream integration guide가 actor derivation과 exact value/native denom/fixed escrow invariant를 명시한다.

### Atomicity

- [x] Core mutation 구간이 nested SDK cache에서 실행된다.
- [x] Reserve/tree/event index failure가 bank와 모든 Clairveil state/event를 rollback한다.
- [x] Core 성공 뒤 downstream failure를 outer SDK cache에서 rollback한다. 실제 EVM snapshot 결합은 downstream e2e 범위로 문서화했다.
- [x] Rollback test가 Go struct balance mock이 아닌 cache-aware bank state를 사용한다.

### Compatibility와 delivery

- [x] Actor=funder equivalence가 state/event/error/gas/index 전체에서 통과한다.
- [x] Existing Privacy test suite가 통과한다.
- [x] Full repository test가 통과한다.
- [x] Proto/generated/circuit/artifact diff가 없다.
- [x] Store migration과 module consensus version bump가 없다.
- [x] Downstream consumer가 참조할 immutable implementation commit이 제공된다.

## 13. Completion ledger

구현 완료 결과는 다음과 같다.

```text
Implementation commit: 6f54c5bf23ffaf66d25dd34c1385e87318457a45
Release/tag: immutable implementation commit 제공; release tag는 생성하지 않음
Public Deposit regression: PASS
Actor=funder equivalence: PASS; bank/privacy state, event, error, ABCI code, gas, scan/index 비교
Actor!=funder success: PASS; actor 불변, funder debit, module/reserve credit, creator attribution 확인
Core-local rollback: PASS; insufficient/bank/reserve/tree/event-index failure 전체 KV/event 불변
Outer snapshot rollback: PASS; nested success 뒤 outer SDK cache discard 검증
Zero-value regression: PASS; transparent balance 불변과 commitment/event/index 생성 확인
go test ./x/privacy/...: PASS; 2026-07-19, -count=1
go test ./...: PASS; 2026-07-19, -count=1
Proto/circuit/artifact diff: PASS; 변경 없음
Downstream integration result: Go integration contract와 e2e gate 문서화 완료; 실제 EVM snapshot e2e는 downstream repository 책임
Remaining risks/non-goals: EVM payable/balance, caller address mapping, policy lifecycle과 release tag 생성은 이 repository 변경 범위 밖
```
