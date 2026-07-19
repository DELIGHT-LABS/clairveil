# Clairveil Deposit Actor/Funder 분리 변경 요청

> 상태: `Upstream implementation request`
>
> 작성일: 2026-07-19
>
> 대상 repository: `github.com/DELIGHT-LABS/clairveil`
>
> 기준 dependency: `v0.2.0`

## 1. 요청 요약

Clairveil `MsgServer.Deposit`의 기존 consensus 동작을 보존하면서, trusted downstream integration이 deposit actor와 transparent funder를 분리할 수 있는 Go keeper API를 추가해 주기를 요청한다.

현재 public deposit 의미는 그대로 유지해야 한다.

```text
actor  = MsgDeposit.Creator
funder = MsgDeposit.Creator
```

새 trusted API는 payable EVM integration이 다음 의미로 호출할 수 있어야 한다.

```text
actor  = MsgDeposit.Creator = Privacy precompile operator
funder = fixed Privacy precompile escrow account
```

이 변경은 public transaction API, protobuf, circuit, proof, CLI나 client SDK 기능 추가 요청이 아니다. Existing deposit core를 한 번만 유지하면서 bank debit account만 명시적으로 주입할 수 있게 하는 내부 integration seam 요청이다.

## 2. 변경이 필요한 배경

요청 integration은 state-changing Privacy action을 fixed Privacy precompile을 통해 Clairveil keeper로 전달한다.

기존 non-payable integration에서는 `MsgDeposit.Creator` 계정에서 Clairveil module account로 직접 자금이 이동했다.

```text
creator/operator → privacy module
```

새 deposit integration은 일반 EVM payable call과 같은 자금 의미를 사용한다.

```text
operator → EVM CALL msg.value → Privacy precompile escrow
```

이후 현재 Clairveil `MsgServer.Deposit`을 `Creator=operator`로 그대로 호출하면 Clairveil이 operator를 다시 차감한다.

```text
operator → Privacy precompile  // EVM msg.value
operator → privacy module      // current MsgServer.Deposit
```

결과적으로 operator가 같은 금액을 두 번 지불한다.

`Creator=Privacy precompile`로 기존 API를 호출하면 자금은 한 번만 이동하지만 Clairveil indexed deposit event도 precompile을 creator로 기록한다. 실제 actor/operator attribution을 잃기 때문에 downstream audit와 scanner 의미에 맞지 않는다.

Downstream application이 Clairveil deposit 로직을 복제하는 것도 허용할 수 없다. 현재 deposit은 다음 consensus-critical 로직을 한 경로에서 처리한다.

- `MsgDeposit.ValidateBasic`
- creator와 coin parse
- shielded amount validation
- canonical commitment validation과 duplicate check
- Merkle capacity precondition
- registered asset lookup
- Groth16 deposit proof verification
- account-to-module fund transfer
- reserve accounting
- commitment append
- indexed Privacy event

이 로직을 downstream에서 복사하면 향후 validation, proof, gas, event와 state ordering이 쉽게 분기한다. 따라서 Clairveil keeper가 하나의 core를 소유한 채 actor와 funder만 분리해야 한다.

## 3. 요구되는 의미

### 3.1 Actor

Actor는 `MsgDeposit.Creator`다.

Actor는 다음에 계속 사용한다.

- `MsgDeposit` validation
- Clairveil deposit indexed event의 `creator`
- downstream audit/scanner attribution

새 API가 별도 actor string을 받아 `msg.Creator`와 다르게 만드는 것은 권장하지 않는다. `msg.Creator`를 actor의 단일 source of truth로 유지한다.

### 3.2 Funder

Funder는 실제 transparent balance가 차감되는 `sdk.AccAddress`다.

기존 public `MsgServer.Deposit`에서는 parsed creator를 funder로 사용한다. 새 trusted API에서는 downstream이 funder를 직접 제공한다.

Funder는 다음 한 곳의 의미만 바꾼다.

```go
bankKeeper.SendCoinsFromAccountToModule(
    ctx,
    funder,
    types.ModuleName,
    sdk.NewCoins(coin),
)
```

Proof public input, reserve amount와 commitment는 기존 `msg.Amount`를 계속 사용한다.

### 3.3 Amount

Clairveil의 source of truth는 계속 `MsgDeposit.Amount`다. Downstream EVM integration은 `msg.value`와 runtime native denom에서 canonical coin string을 만들어 이 field에 넣는다.

```text
Downstream EVM msg.value
  → canonical <amount><native-denom>
  → MsgDeposit.Amount
  → existing Clairveil proof and reserve validation
```

Clairveil은 EVM `msg.value`를 알 필요가 없고 downstream-specific EVM dependency를 추가하지 않는다.

## 4. 권장 API

이 문서에서는 `DepositWithFunder`라는 이름을 사용한다. Clairveil naming convention에 맞는 동등한 이름도 가능하지만 public transaction method와 혼동되지 않아야 한다.

개념적 API는 다음과 같다.

```go
// DepositWithFunder executes the canonical Clairveil deposit transition while
// debiting funder. msg.Creator remains the actor and indexed-event creator.
// This is a trusted in-process integration API and is not registered as a
// protobuf Msg service.
func (k Keeper) DepositWithFunder(
    ctx sdk.Context,
    msg *types.MsgDeposit,
    funder sdk.AccAddress,
) (*types.MsgDepositResponse, error)
```

기존 MsgServer는 creator를 parse한 뒤 새 core에 위임한다. Actor의 source of truth는 `msg.Creator`이므로 internal core에 별도 actor parameter를 추가할 필요는 없다.

```go
func (s msgServer) Deposit(
    goCtx context.Context,
    msg *types.MsgDeposit,
) (*types.MsgDepositResponse, error) {
    ctx := sdk.UnwrapSDKContext(goCtx)

    // Keep the existing validation and error ordering.
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

Trusted API도 동일한 core를 사용한다.

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
    if len(funder) == 0 {
        return nil, errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "deposit funder is required")
    }

    return k.depositWithValidatedFunder(ctx, msg, funder)
}
```

내부 core의 정확한 이름과 validation factoring은 Clairveil 코드 스타일에 맞게 조정할 수 있다. 중요한 계약은 다음과 같다.

- Public `Deposit`과 `DepositWithFunder`가 하나의 deposit transition core를 사용한다.
- Public `Deposit`은 기존과 동일하게 creator를 funder로 사용한다.
- Indexed event creator는 항상 `msg.Creator`다.
- Bank debit에만 명시적 funder를 사용한다.
- Trusted API가 validation, proof, reserve, tree나 event를 skip하지 않는다.

`depositWithValidatedFunder`는 현재 `MsgServer.Deposit`에서 `ValidateBasic`과 creator parse 이후의 본문을 이동한 private helper로 만들 수 있다. 기존 `depositor` 대신 주입된 `funder`를 아래 bank call에만 사용하고, event attribute는 계속 `msg.Creator`를 사용한다.

```go
if err := k.bankKeeper.SendCoinsFromAccountToModule(
    ctx,
    funder,
    types.ModuleName,
    sdk.NewCoins(coin),
); err != nil {
    return nil, errorsmod.Wrap(err, "failed to lock tokens")
}
```

## 5. 기존 validation 및 mutation 순서 보존

Refactor 전후에 기존 `MsgServer.Deposit`의 observable ordering을 유지해야 한다.

현재 기준 순서는 다음과 같다.

1. `msg.ValidateBasic()`
2. `msg.Creator` parse
3. `msg.Amount` parse
4. shielded amount range validation
5. commitment canonicalization
6. duplicate commitment lookup
7. Merkle append capacity precondition
8. deposit circuit assignment 구성
9. registered asset lookup
10. deposit proof verification
11. account-to-module bank send
12. reserve deposit record
13. commitment append
14. indexed deposit event emission

새 core를 추출하면서 validation을 뒤로 옮기거나 bank send를 proof보다 앞에 두지 않는다. Error class/message와 gas consumption의 합리적인 호환성도 유지한다.

Actor와 funder가 같을 때 기존 path와 동일한 state/event/error를 만들어야 한다.

## 6. Zero-value deposit

이 integration은 Clairveil v0.2의 valid zero-value deposit 의미를 유지한다. 새 API는 amount `0`을 별도로 금지하면 안 된다.

```text
MsgDeposit.Amount = 0<registered-denom>
```

기존 proof, commitment, tree와 indexed event 규칙을 그대로 적용한다. Bank/reserve zero-amount 처리도 기존 public `Deposit`과 동일해야 한다.

## 7. Security 요구사항

### 7.1 Public funder 선택 금지

`funder`를 다음 surface에 추가하지 않는다.

- `MsgDeposit` protobuf
- gRPC `MsgServer`
- CLI flag
- transaction JSON
- client SDK request

그렇게 하면 untrusted user가 다른 transparent account를 funder로 지정하려는 transaction surface가 생긴다.

### 7.2 Trusted in-process API

`DepositWithFunder`는 Go application composition에서만 호출한다. Downstream application은 user calldata에서 funder를 받지 않고 fixed Privacy precompile address를 제공한다.

Clairveil은 downstream의 fixed address를 하드코딩할 필요가 없다. 어떤 funder를 신뢰할지는 downstream application의 책임이다.

### 7.3 Input ownership

호출자가 전달한 `MsgDeposit` byte slice를 core가 장기 보관하거나 mutation하지 않는다. 기존 message ownership 규칙을 유지한다.

### 7.4 Atomicity

Trusted API는 standalone partial commit을 수행하지 않는다. Caller가 제공한 SDK cache/EVM snapshot 안에서 기존 deposit transition을 실행하며 error를 그대로 반환한다.

다음 실패에서는 funder debit, reserve, commitment와 event가 commit되지 않아야 한다.

- Invalid proof
- Duplicate commitment
- Insufficient funder balance
- Reserve record failure
- Tree append failure
- Event index failure
- Downstream outer policy after/record failure

마지막 두 downstream policy failure는 caller의 outer snapshot이 rollback을 소유한다. Clairveil API는 caller rollback을 방해하는 independent commit을 하지 않아야 한다.

## 8. 테스트 요청

### 8.1 기존 behavior regression

기존 `MsgServer.Deposit` test suite는 변경 없이 모두 통과해야 한다.

추가 equivalence test는 다음을 비교한다.

```text
MsgServer.Deposit(msg.Creator=A)
DepositWithFunder(msg.Creator=A, funder=A)
```

비교 대상:

- A balance와 module balance
- reserve snapshot
- commitment index와 Merkle root
- indexed deposit event
- error type/message
- zero-value behavior

### 8.2 Actor와 funder가 다른 성공 case

```text
actor  = A
funder = F
amount = X
```

성공 후 다음을 확인한다.

- A balance는 변하지 않음
- F balance는 정확히 X 감소
- Privacy module balance는 정확히 X 증가
- Reserve는 정확히 X 증가
- Commitment가 정확히 한 번 추가됨
- Indexed event creator는 A
- Event amount/commitment/encrypted note 의미는 기존과 동일

### 8.3 실패 case

- Empty/invalid funder 거부
- Funder balance 부족 시 actor/funder/module/reserve/tree/event 불변
- Invalid proof 시 bank send 0회
- Duplicate commitment 시 bank send 0회
- Unregistered asset 시 bank send 0회
- Merkle capacity failure 시 bank send 0회
- Bank send failure 시 reserve/tree/event 불변
- Zero-value deposit 성공과 기존 behavior 일치

### 8.4 Mock assertion

BankKeeper mock은 `SendCoinsFromAccountToModule`의 sender가 `msg.Creator`가 아니라 전달한 funder인지 명시적으로 검증한다.

## 9. Public API 및 SDK 영향

이 변경에는 다음 생성물 변경이 없어야 한다.

- `proto/clairveil/privacy/v1/tx.proto`
- Generated protobuf files
- gRPC gateway
- Clairveil CLI command/flag
- JS/TS client API
- Go transaction builder API
- Deposit proof builder input
- Golden wire/vector format

`DepositWithFunder`는 Clairveil Go module의 keeper integration API다. Downstream consumer는 새 immutable Clairveil version을 dependency로 받아 이 method를 직접 또는 작은 internal interface를 통해 호출한다.

Downstream EVM client는 별도로 payable ABI에 맞춰 transaction `value`를 설정하지만 이는 Clairveil client SDK 변경 요구사항이 아니다. Clairveil proof builder는 계속 amount를 입력받아 같은 deposit proof를 생성한다.

## 10. Versioning과 delivery

이 변경은 public transaction format과 기존 behavior를 바꾸지 않는 additive Go API/refactor다. Clairveil release 정책에 맞는 새 immutable tag 또는 commit을 제공해 주기를 요청한다.

권장 delivery는 다음과 같다.

1. Clairveil branch에서 core extraction과 tests 구현
2. 기존 full Privacy test suite 실행
3. Actor/funder integration tests 실행
4. Immutable commit/tag 전달
5. Downstream consumer가 `go.mod` dependency를 새 version으로 갱신
6. Downstream app integration에서 EVM value exact-once flow 검증

Production consumer는 local module-cache patch나 mutable branch를 참조하지 않는다.

## 11. Non-goals

Clairveil 변경 범위에 다음은 포함하지 않는다.

- Downstream Privacy precompile ABI
- EVM payable/balance 처리
- PCL operation이나 policy lifecycle
- `contract.Caller()` 또는 EVM address semantics
- `depositWithAuthorization` 제거
- Downstream policy admin/EAS/denylist bootstrap
- New public deposit transaction type
- Arbitrary third-party funding protocol
- Relayer fee/refund protocol
- Circuit, VK, R1CS/PK artifact 변경
- Store migration 또는 module consensus version 변경

## 12. Acceptance checklist

- [ ] 기존 public `MsgServer.Deposit` semantics가 actor=funder=Creator로 유지된다.
- [ ] 하나의 canonical deposit core만 존재한다.
- [ ] Trusted API가 explicit funder를 bank debit에 사용한다.
- [ ] Indexed event creator는 `msg.Creator`로 유지된다.
- [ ] Existing validation/proof/state/event ordering이 유지된다.
- [ ] Zero-value deposit이 유지된다.
- [ ] Public protobuf/gRPC/CLI/client SDK에 funder가 노출되지 않는다.
- [ ] Actor=funder equivalence test가 통과한다.
- [ ] Actor≠funder success/failure tests가 통과한다.
- [ ] Existing Clairveil Privacy test suite가 통과한다.
- [ ] Downstream consumer가 참조할 immutable version/commit이 제공된다.
