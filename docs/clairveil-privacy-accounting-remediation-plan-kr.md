# Clairveil Privacy Accounting 취약점 수정 계획

이 문서는 2026-06-05 Zcash Orchard counterfeiting 이슈를 계기로 `clairveil`의 shielded pool accounting과 ZK circuit soundness를 검토하면서 확인한 대응 계획입니다. Orchard/halo2 취약점과 직접 같은 버그는 아니지만, `clairveil`도 private note amount와 on-chain value accounting을 ZK proof에 의존하므로 비슷한 결과, 즉 shielded pool 내 위조 가치 생성 또는 과출금이 발생하지 않도록 별도 수정이 필요합니다.

현재 상태는 **P0 remediation implementation completed in this branch**입니다. 2026-06-08 기준으로 deposit binding, amount range constraints, EdDSA/point hardening, reserve accounting query, artifact contract rotation, regression/negative tests가 구현되었습니다. 다만 이미 public 또는 production-like network에서 사용 중인 경우에는 코드 패치만으로 충분하지 않으며, privacy module deposit, transfer, withdraw를 임시 중단하고 state exposure를 먼저 평가해야 합니다.

## 1. 결론 요약

| 항목 | 판단 |
| --- | --- |
| Zcash Orchard/halo2 직접 영향 | 낮음. repo에는 `zcash`, `orchard`, `sapling`, `halo2` 계열 의존성이 보이지 않습니다. |
| Clairveil 자체 accounting 위험 | 높음. deposit commitment와 locked transparent amount의 일치 증명이 없습니다. |
| 회로 soundness 위험 | 높음. joinsplit amount conservation이 field equality에 의존하며 amount range proof가 없습니다. |
| 권장 대응 | deposit binding proof 추가, amount range constraints 추가, EdDSA/curve constraint hardening, artifact rotation, regression test 추가. |

구현 후 상태:

| 항목 | 현재 상태 |
| --- | --- |
| Deposit binding | 완료. `DepositCircuit`과 `MsgDeposit.proof`를 통해 locked amount/asset과 commitment를 binding합니다. |
| Amount range | 완료. 모든 shielded amount는 64-bit non-negative integer bound를 공유합니다. |
| Spend/JoinSplit soundness hardening | 완료. `eddsa.Verify`, point on-curve assertion, malformed point/scalar negative tests가 추가되었습니다. |
| Reserve/accounting invariant | 완료. denom별 reserve snapshot query가 module balance와 deposited/withdrawn totals를 비교합니다. |
| Artifact contract | 완료. active circuit set id가 `privacy-accounting-v2`로 갱신되고 deposit artifacts가 포함되었습니다. |
| Production incident response | repo-local 구현 완료. Public exposure 여부, old pool freeze/migration/claim 정책은 deployment owner가 별도로 결정해야 합니다. |

## 2. 영향 범위

### 2.1 On-chain keeper

- `x/privacy/keeper/msg_server.go`
- `MsgDeposit`, `MsgTransfer`, `MsgWithdraw`
- commitment append, historical root check, nullifier write, module bank balance release

### 2.2 ZK circuits

- `x/privacy/circuit/spend.go`
- `x/privacy/circuit/joinsplit.go`
- amount, asset id, note commitment, nullifier, signature, disclosure digest constraints

### 2.3 Client/prover path

- `x/privacy/client/sdk/transfer/*`
- `x/privacy/client/sdk/withdraw/*`
- `x/privacy/client/sdk/provertransport/*`
- `cmd/clairveil-proverd`
- conformance fixtures and JS SDK contracts

### 2.4 Artifacts and release

- `cmd/clairveil-setup`
- `x/privacy/zk/*`
- `privacy_deposit_*`, `privacy_spend_*`, `privacy_joinsplit_*` artifacts
- release pack, checksum manifest, downstream handoff docs

## 3. 확인된 취약점 후보

### 3.1 Deposit commitment와 입금 금액 불일치

`Deposit`은 `msg.Amount`를 parsing한 뒤 해당 coin을 module account에 lock하고, `msg.NoteCommitment`가 canonical 32-byte field element인지 확인한 뒤 Merkle tree에 append합니다. 하지만 keeper는 commitment 내부에 들어간 `amount`와 `asset_id`가 실제 lock된 `coin.Amount`와 `coin.Denom`에서 파생된 값인지 알 수 없습니다.

공격 시나리오:

1. 공격자가 `1uclair`만 입금합니다.
2. 직접 만든 note commitment에는 `amount = 1000000`, `asset_id = HashString("uclair")`를 넣습니다.
3. keeper는 commitment가 canonical field element인지까지만 확인하고 tree에 append합니다.
4. 공격자는 해당 note witness로 spend proof를 만들고 더 큰 금액을 withdraw하려고 시도할 수 있습니다.

심각도: **Critical**

수정 방향:

- deposit에도 별도 proof를 요구하거나, 최소한 deposit commitment 생성에 필요한 공개 입력을 keeper가 검증할 수 있는 구조로 바꿉니다.
- 권장안은 `DepositCircuit`을 추가해 아래를 증명하는 것입니다.
  - `commitment = MiMC(spend_pubkey, view_pubkey, amount, asset_id, randomness)`
  - `amount`는 `msg.Amount`와 일치
  - `asset_id = HashString(denom)` 또는 chain-level asset id mapping과 일치
  - `amount`는 허용 bit width 안의 non-negative integer
- `MsgDeposit`에 deposit proof를 추가하고, 기존 proof 없는 deposit은 migration 후 비활성화합니다.

### 3.2 JoinSplit amount range proof 부재

`JoinSplitCircuit`은 `totalInputAmount`와 `totalOutputAmount`를 field element로 더한 뒤 equality를 확인합니다. 이 조건은 BN254 scalar field에서의 등식이므로, 정수 의미의 보존과 같지 않습니다. 각 amount가 명시적으로 작은 non-negative integer라는 제약이 없으면 modulo wrap을 이용한 위조 가능성을 배제할 수 없습니다.

공격 시나리오:

1. input amount 합이 작은 note를 준비합니다.
2. output amount 중 하나를 field modulus에 가까운 값으로 설정합니다.
3. 다른 output amount와 조합해 field modulo 상에서는 input 합과 같게 만듭니다.
4. 회로가 range를 강제하지 않으면 proof가 field equality를 만족할 수 있습니다.
5. 이후 output note를 분할하거나 withdraw하면서 transparent module balance를 고갈시키는 공격으로 이어질 수 있습니다.

심각도: **Critical**

수정 방향:

- 모든 input/output amount에 range constraint를 추가합니다.
- 권장 bit width는 chain coin amount와 SDK limit을 기준으로 결정합니다. 예: `uint64` 또는 Cosmos SDK amount policy에 맞춘 더 보수적인 상한.
- `api.ToBinary(amount, AmountBitLength)`로 bit decomposition을 강제하고, 필요하면 zero/positive 정책을 별도로 둡니다.
- `sum(input) = sum(output)`은 range-constrained integer들의 합으로만 허용합니다.
- deposit, transfer, withdraw, disclosure digest, wallet note schema가 같은 amount bound를 공유하도록 상수화합니다.

### 3.3 수동 EdDSA 검증과 curve constraint hardening 필요

`SpendCircuit`과 `JoinSplitCircuit`은 gnark 표준 `eddsa.Verify`를 직접 호출하지 않고 유사한 검증식을 수동으로 구성합니다. 현재 검토만으로 즉시 exploit 가능하다고 결론낼 수는 없지만, public key point, signature `R`, scalar `S`, subgroup/cofactor 처리가 표준 verifier와 같은 보안 성질을 갖는지 별도 검증해야 합니다.

심각도: **High**

수정 방향:

- 가능하면 gnark 표준 `eddsa.Verify`로 교체합니다.
- 표준 verifier로 교체할 수 없다면 아래 제약을 명시합니다.
  - spend/view public key point on-curve
  - signature `R` point on-curve
  - signature scalar `S` group order bound
  - subgroup/cofactor handling
  - zero/identity key 금지 여부
- malformed point와 high/invalid scalar를 넣은 negative proof tests를 추가합니다.

### 3.4 Merkle path helper와 SDK validation hardening

Merkle path 방향 값과 disclosure policy bit처럼 boolean 의미를 갖는 witness/public input은 테스트와 SDK validation에서 명확히 다뤄야 합니다. 현재 `SpendCircuit`과 `JoinSplitCircuit`은 path helper를 `api.Select(...)` selector로 사용하고, gnark `v0.14.0`의 `api.Select`는 selector를 boolean으로 constrain합니다. 따라서 path helper는 현재 circuit-level 취약점이라기보다 명시적 negative test와 client-side validation을 보강할 hardening 항목입니다.

심각도: **Low / Hardening**

수정 방향:

- `api.Select` 경로에서 non-boolean helper가 proof generation/verification을 통과하지 못한다는 negative test를 추가합니다.
- SDK와 provider response validation에서 helper type이 `0` 또는 `1`만 허용되도록 강화합니다.
- 필요하면 가독성을 위해 `AssertIsBoolean`을 명시적으로 추가할 수 있지만, 현재 gnark `api.Select` 제약과 중복됨을 문서화합니다.

## 4. 즉시 운영 대응

이미 network가 public하게 열려 있거나 외부 사용자가 tx를 만들 수 있다면 아래 순서로 대응합니다.

1. `MsgDeposit`, `MsgTransfer`, `MsgWithdraw` tx acceptance를 governance, app config, ante/decorator, chain halt 등 가능한 수단으로 임시 중단합니다.
2. module account balance, total deposited transparent amount, known commitments, known nullifiers, historical roots를 snapshot으로 보존합니다.
3. event log에서 비정상 deposit amount, 급격한 transfer fan-out, 의심스러운 withdraw 패턴을 조사합니다.
4. audit disclosure, deposit records, wallet note opening, full lineage reconstruction을 조합해 노출 범위를 평가합니다. 단, 현재 audit disclosure는 transfer의 recipient output amount/from/to 검증에 유용하지만 초기 deposit commitment가 실제 locked amount와 일치했는지를 소급해서 증명하지는 못하므로, forged deposit 여부를 cryptographically 완전히 배제하지 못할 수 있습니다.
5. 패치된 circuit/artifact가 준비되기 전까지 새 note creation을 허용하지 않습니다.

Reference/local-only 환경이라도 release note에는 이 이슈를 명시하고, downstream integrator가 production readiness로 오해하지 않도록 경고를 추가합니다.

## 5. 코드 수정 계획

### Phase 0: 재현 테스트 먼저 추가

목표는 현재 취약한 동작을 테스트로 고정하는 것입니다.

- deposit amount와 commitment amount가 다를 때 현재 keeper가 통과시키는지 보여주는 regression test를 작성합니다.
- joinsplit에서 amount modulo wrap 시도가 proof generation 또는 verification을 통과할 수 있는지 확인하는 circuit-level negative test를 작성합니다.
- invalid point, invalid scalar, path helper hardening 테스트를 추가합니다.

주의: 실제 exploit proof를 생성하는 테스트는 공개 repo에 그대로 둘지 검토가 필요합니다. 공개 테스트에는 mitigated behavior 중심의 negative test만 남기고, exploit PoC는 보안 채널에 제한할 수 있습니다.

구현 상태: 완료. 공개 repo에는 mitigated behavior 중심의 negative tests를 남겼습니다. Forged deposit, amount overflow/wrap, invalid path helper, malformed point/scalar, reserve invariant mismatch를 모두 reject 또는 incident signal로 고정했습니다.

### Phase 1: Amount model 상수화

아래 상수를 새로 정의합니다.

- `AmountBitLength`
- `MaxShieldedAmount`
- `AssetID` derivation rule 또는 chain asset registry rule

적용 대상:

- circuit constraints
- SDK note builder
- transfer planner
- withdraw payload validator
- keeper validation
- docs and schemas

구현 상태: 완료. `x/privacy/types/amount.go`의 `ShieldedAmountBitLength`, `MaxShieldedAmount`, `ValidateShieldedAmount`가 circuit, keeper, SDK, payload validation에서 공유됩니다.

### Phase 2: Deposit binding 추가

권장 구현:

- `DepositCircuit` 추가
- `MsgDeposit`에 `proof`와 필요한 public input 추가
- keeper에서 deposit proof verification 후 commitment append
- old deposit format은 migration window 이후 reject

대체 구현:

- deposit note의 amount/asset/spend/view key를 공개하고 keeper가 commitment를 재계산합니다.
- 이 대안은 deposit privacy를 약화시키므로 reference/local mode 외에는 권장하지 않습니다.

추가로 keeper-level reserve/accounting invariant를 도입합니다.

- denom별 module account reserve를 추적합니다.
- `total_deposited`, `total_withdrawn`, approved top-up/adjustment를 denom별로 기록합니다.
- `MsgDeposit`과 `MsgWithdraw` 외 direct bank send/top-up이 reserve invariant를 흐리지 않도록 허용 경로와 회계 반영 방식을 명시합니다.
- invariant query 또는 crisis snapshot 절차를 추가해 incident response 때 reserve, deposit/withdraw total, module account balance를 즉시 비교할 수 있게 합니다.

구현 상태: 완료. `DepositCircuit`, `MsgDeposit.proof`, keeper proof verification, CLI/SDK deposit proof builder, deposit artifact loader/setup이 추가되었습니다. Denom별 reserve accounting은 `/clairveil/privacy/v1/reserve/{denom}` query로 노출됩니다.

### Phase 3: JoinSplit/Spend range constraints 추가

적용할 제약:

- `SpendCircuit.Amount` range check
- `JoinSplitCircuit.InputAmounts[i]` range check
- `JoinSplitCircuit.OutputAmounts[i]` range check
- output amount zero 허용 여부 명시
- field modulo wrap이 불가능하도록 integer-domain sum 보존

`Withdraw`는 public amount를 keeper가 transparent coin으로 해석하므로, circuit amount bound와 keeper amount bound가 일치해야 합니다.

구현 상태: 완료. `SpendCircuit.Amount`, `JoinSplitCircuit.InputAmounts`, `JoinSplitCircuit.OutputAmounts`가 같은 64-bit bound로 constrain됩니다. Zero-value dummy output/input은 non-negative bound 안에서 계속 허용됩니다.

### Phase 4: Signature/point constraints hardening

권장 구현:

- `SpendCircuit`와 `JoinSplitCircuit`에서 gnark 표준 `eddsa.Verify` 사용
- public key와 signature point에 on-curve assertion 추가
- scalar bound assertion 추가
- malformed point/scalar negative test 추가

이 단계에서 proving cost가 증가할 수 있으므로 benchmark를 같이 남깁니다.

구현 상태: 완료.

- `SpendCircuit`와 `JoinSplitCircuit` 모두 gnark 표준 `eddsa.Verify`를 사용합니다.
- spend/view public key와 signature `R` point에 on-curve assertion을 추가했습니다.
- signature scalar `S` bound는 gnark `eddsa.Verify` 내부의 `AssertIsLessOrEqual(sig.S, curve.Params().Order)`로 강제됩니다.
- `DepositCircuit`, `SpendCircuit`, `JoinSplitCircuit`에 malformed point/scalar negative tests를 추가했습니다.
- `BenchmarkDepositCircuitProve`, `BenchmarkSpendCircuitProve`, `BenchmarkJoinSplitCircuitProve`를 추가해 proving cost를 재측정할 수 있게 했습니다.

### Phase 5: Prover payload, schemas, fixtures 갱신

수정 대상:

- Go SDK prepared payload structs
- prover HTTP contract
- conformance fixture JSON
- `docs/schemas/clairveil-js-wallet-contract.schema.json`
- JS SDK examples
- CLI output/input JSON

모든 proof response는 새 circuit version과 payload hash를 포함해야 합니다. 이전 artifact와 새 artifact가 섞이지 않도록 circuit set id를 갱신합니다.

구현 상태: 완료. Transfer/withdraw prover payload contract와 conformance fixtures는 기존 version/hash 검증을 유지하며 새 circuit set id를 반영합니다. Deposit은 현재 별도 HTTP prover endpoint 없이 CLI/SDK가 Groth16 proof bytes를 생성해 `MsgDeposit.proof`에 넣는 계약입니다. Downstream JS/TS client가 remote deposit proving을 원하면 별도 deposit prover endpoint 또는 local/WASM prover adapter를 추가해야 합니다.

### Phase 6: Artifact rotation

필수 작업:

- `clairveil-setup`으로 새 R1CS/PK/VK 생성
- `privacy_zk_manifest.json`의 `active_set_id` 갱신
- checksum env 갱신
- `CLAIRVEIL_PRIVACY_ZK_PREFLIGHT_MODE=strict` 기본 권장
- release pack에 artifact provenance와 source commit hash 기록

기존 note pool과 호환되지 않는 변경이면 새 pool id 또는 migration boundary를 둡니다.

구현 상태: 완료. `privacy-accounting-v2` active circuit set id와 deposit/spend/joinsplit 9개 artifact descriptor가 추가되었습니다. Generated binary artifacts는 source repo에 커밋하지 않고 `clairveil-setup`에서 재생성합니다.

## 6. State migration 전략

상황별로 선택합니다.

### 6.1 Public exposure가 없었던 경우

- 기존 artifacts를 폐기합니다.
- patched artifacts로 다시 생성합니다.
- 기존 local test state는 reset합니다.

### 6.2 Public exposure는 있었지만 exploit 증거가 없는 경우

- old pool을 freeze합니다.
- old pool withdraw는 deposit records, wallet note opening, transfer audit disclosure, full lineage reconstruction, reserve snapshot을 함께 검토한 capped/manual review를 거친 경우에만 허용할지 결정합니다. Audit disclosure만으로 deposit commitment의 정당성을 증명할 수 없다는 점을 명시합니다.
- 새 deposit/transfer는 patched pool에서만 허용합니다.
- old note를 new pool로 옮기는 migration circuit 또는 audited manual migration flow를 설계합니다.

### 6.3 Exploit 가능성이 있거나 module balance가 불일치하는 경우

- old pool을 freeze하고 automatic withdraw를 중단합니다.
- module account balance와 known legitimate deposits를 기준으로 claim process를 설계합니다.
- audit disclosure, deposit records, wallet note opening, wallet-submitted proof, full lineage reconstruction을 이용해 claim legitimacy를 재검토합니다. 그래도 forged deposit을 cryptographically 완전히 배제하지 못할 수 있으므로 claim process와 haircut/escrow/appeal 정책을 별도로 결정합니다.
- 새 pool은 이전 state와 cryptographically 분리합니다.

## 7. 테스트 계획

### Unit tests

- deposit proof 없이 forged commitment가 reject되는지 확인
- deposit proof amount mismatch reject
- deposit proof asset mismatch reject
- transfer output amount overflow/wrap reject
- negative amount 또는 field modulus 근처 amount reject
- withdraw amount bound 초과 reject
- non-boolean path helper가 `api.Select` boolean constraint 또는 SDK validation에서 reject되는지 확인
- invalid public key/signature point reject
- signature scalar bound 위반 reject
- denom별 reserve invariant가 deposit/withdraw/approved adjustment 외 상태 변화에서 깨지는지 확인

구현 상태: 완료. 위 항목은 keeper/types/SDK/circuit 테스트에 분산되어 있으며, malformed point/scalar tests는 `x/privacy/circuit/*_test.go`에 명시적으로 추가되었습니다.

### Circuit tests

- valid deposit/spend/joinsplit proof success
- all amount fields range constrained
- integer sum conservation
- asset id binding
- nullifier binding
- disclosure digest binding

구현 상태: 완료. 추가로 `x/privacy/circuit/bench_test.go`에 deposit/spend/joinsplit proving benchmark가 있습니다.

### Integration/e2e

- `make test`
- `make ci`
- `make privacy-e2e-smoke`
- `make release-pack-verify`
- prover HTTP conformance fixture validation
- JS SDK fixture validation
- localnet deposit-transfer-withdraw happy path
- old artifact/new artifact mismatch failure
- reserve invariant query와 crisis snapshot output 검증

구현 상태: repo-level `go test ./...`, `make examples`, `make build`, focused circuit benchmark smoke가 통과했습니다. Localnet/privacy e2e와 release-pack 검증은 release candidate 단계에서 다시 실행해야 합니다.

## 8. Release checklist

패치 release는 아래 조건을 만족해야 합니다.

1. 취약점 요약과 영향 범위가 release note에 포함됩니다.
2. old artifact checksum과 new artifact checksum이 명확히 구분됩니다.
3. `active_set_id`가 변경됩니다.
4. downstream integration guide가 migration boundary를 설명합니다.
5. `docs/clairveil-circuits-kr.md`와 영문 회로 문서가 새 증명 항목을 반영합니다.
6. JS SDK handoff 문서가 새 payload/proof contract를 반영합니다.
7. 보안 regression tests가 CI에 포함됩니다.
8. reserve invariant query와 crisis snapshot 절차가 release checklist에 포함됩니다.
9. public exposure가 있었다면 incident response 또는 non-exposure statement를 별도로 남깁니다.

## 9. 완료 기준

이 remediation은 아래가 모두 충족되면 완료로 봅니다.

- deposit commitment가 locked transparent amount와 asset에 cryptographically binding됩니다.
- 모든 amount는 명시적 range constraint를 갖습니다.
- joinsplit value conservation이 field modulo가 아니라 bounded integer domain에서 성립합니다.
- denom별 module-account reserve invariant가 정의되고, deposit/withdraw 총량, approved top-up/adjustment, direct bank send 처리 정책이 keeper/query/incident snapshot 절차에 반영됩니다.
- malformed point/scalar witness가 proof 검증을 통과하지 못하고, path-helper validation이 circuit 또는 SDK 경계에서 명확히 검증됩니다.
- patched artifacts와 manifest가 재생성되고 strict preflight로 검증됩니다.
- transfer/withdraw/deposit e2e와 conformance fixtures가 새 contract 기준으로 통과합니다.
- migration 또는 reset 전략이 deployment 상태별로 문서화됩니다.

## 10. 관련 문서

- `docs/clairveil-circuits-kr.md`
- `docs/clairveil-threat-model-kr.md`
- `docs/clairveil-security-best-practices-review-kr.md`
- `docs/clairveil-release-handoff-pack-kr.md`
- `docs/clairveil-downstream-cosmos-integration-guide-kr.md`
