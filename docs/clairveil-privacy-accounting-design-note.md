# Clairveil Privacy Accounting Design and Hardening Note

Korean version: [clairveil-privacy-accounting-design-note-kr.md](clairveil-privacy-accounting-design-note-kr.md)

This document records the privacy accounting design reviewed before the first public release or downstream integration of `clairveil`. The 2026-06 Zcash Orchard soundness issue was the trigger for this review, but `clairveil` does not use Orchard or halo2 code. This note covers Clairveil's own shielded note accounting, ZK circuit soundness, and keeper reserve invariant before public exposure.

This document assumes there is no live or public state. If external users have already been able to create deposit, transfer, or withdraw transactions, this document is not sufficient by itself; the deployment owner must run separate operational procedures and state review.

## 1. Design Conclusions

| Area | Current Design |
| --- | --- |
| Deposit accounting | `MsgDeposit.proof` and `DepositCircuit` bind the locked transparent amount/asset to the note commitment. |
| Amount domain | All shielded amounts share a non-negative 64-bit integer bound. |
| Spend/withdraw | `SpendCircuit` proves note membership, owner signature, nullifier, amount/asset/recipient binding. |
| Transfer | `JoinSplitCircuit` proves 2-input/2-output amount conservation, nullifiers, output commitments, and disclosure digest. |
| Signature/point hardening | Uses gnark standard `eddsa.Verify`, point on-curve assertions, and malformed point/scalar negative tests. |
| Reserve accounting | The keeper exposes a `reserve/{denom}` query comparing denom-level deposit/withdraw totals with module-account balance. |
| Artifact contract | The active circuit set is `privacy-accounting-v2` and includes deposit/spend/joinsplit artifacts. |

## 2. Pre-Public Risk Model

### 2.1 Deposit Commitment and Locked Amount Mismatch

The most important risk in the old shape was that the keeper could not know whether `MsgDeposit.amount` matched the amount/asset inside `note_commitment`. The pre-public design closes that boundary as follows.

1. The client creates a deposit note and computes its commitment.
2. The client generates a `DepositCircuit` proof.
3. The keeper verifies the deposit proof using `MsgDeposit.amount` and the denom-derived asset id as public inputs.
4. Only after proof verification succeeds does the keeper lock bank funds, record the reserve deposit, and append the Merkle commitment.

Therefore, the path "lock 1uclair while appending a 100uclair note commitment" must not pass the keeper boundary.

### 2.2 Amount Field Modulo Wrap

If `JoinSplitCircuit` amount conservation is interpreted only as BN254 field equality, it can differ from integer conservation. The pre-public design blocks field modulo wrap by applying the same 64-bit range constraint to every amount field.

Applied to:

- `DepositCircuit.Amount`
- `SpendCircuit.Amount`
- `JoinSplitCircuit.InputAmounts`
- `JoinSplitCircuit.OutputAmounts`
- keeper/SDK/payload amount validation

### 2.3 Signature and Curve Point Soundness

Spend/JoinSplit owner authorization uses gnark twisted Edwards EdDSA verification. The circuits assert that spend/view public keys and signature `R` points are on curve, while relying on gnark `eddsa.Verify` for the signature scalar bound.

Pre-public tests check that malformed public keys, malformed signature points, and high scalar witnesses do not pass proof generation/verification.

### 2.4 Merkle Path Helper

`PathHelper` is used as an `api.Select` selector in circuits and receives gnark selector boolean constraints. SDK and JS schema also restrict helper values to `0` or `1`. This is a client contract hardening item rather than a current exploit candidate.

## 3. Reserve Accounting

Circuit soundness must be checked together with keeper-level reserve accounting. For each denom, the keeper exposes:

```text
module_balance
total_deposited
total_withdrawn
expected_module_balance = total_deposited - total_withdrawn
invariant_holds = module_balance == expected_module_balance && expected_module_balance >= 0
```

Query path:

```text
GET /clairveil/privacy/v1/reserve/{denom}
```

The current API has no separate reserve adjustment field. If direct bank sends, manual top-ups, or migration-time balance changes do not match recorded deposit/withdraw accounting, `invariant_holds=false` should reveal that mismatch. If governance/admin/migration adjustments become necessary later, design that write path and audit trail before adding a query field.

## 4. Hardening Work Units

### Phase 0: Regression/Negative Tests

Complete. The public repository keeps mitigated-behavior tests.

- forged deposit reject
- amount overflow/wrap reject
- invalid path helper reject
- malformed point/scalar reject
- reserve invariant mismatch signal
- keeper-level deposit proof tamper reject

### Phase 1: Amount Model Constants

Complete. `x/privacy/types/amount.go` provides shared `ShieldedAmountBitLength`, `MaxShieldedAmount`, and `ValidateShieldedAmount` for circuits, keeper, SDK, and payload validation.

### Phase 2: Deposit Binding

Complete. Added `DepositCircuit`, `MsgDeposit.proof`, keeper proof verification, CLI/SDK deposit proof builder, and deposit artifact loader/setup.

### Phase 3: Spend/JoinSplit Range Constraints

Complete. Spend/JoinSplit amount fields are constrained with the same 64-bit bound. Zero-value dummy inputs/outputs are allowed inside the non-negative bound.

### Phase 4: Signature/Point Constraints Hardening

Complete. `SpendCircuit` and `JoinSplitCircuit` use gnark standard `eddsa.Verify` and apply on-curve assertions to public key/signature points. Malformed point/scalar negative tests and proving benchmarks exist.

### Phase 5: Client/Prover Contract Update

Complete. Transfer/withdraw prover payload contracts keep version/hash verification and reflect the new circuit set id. Deposit currently has no separate HTTP prover endpoint; the CLI/SDK generates Groth16 proof bytes and puts them in `MsgDeposit.proof`. If downstream JS/TS clients want remote deposit proving, add a separate deposit prover endpoint or a local/WASM prover adapter.

### Phase 6: Artifact Rotation

Complete. Added the `privacy-accounting-v2` active circuit set id and deposit/spend/joinsplit artifact descriptors. Generated binary artifacts are not committed to the source repo; regenerate them with `clairveil-setup`.

## 5. Client Contract Notes

Clients and downstream SDKs must keep these boundaries:

- `MsgDeposit` does not support proof-less format.
- Deposit builders must create note commitment, encrypted note, and `DepositCircuit` proof together.
- Transfer/withdraw prepared payloads must validate version and payload hash.
- Merkle path helpers must be `0` or `1`.
- `circuit_config` must check `active_set_id` and artifact descriptors.
- `reserve/{denom}` should return `invariant_holds=true` after deposit/withdraw flows.

## 6. Pre-Public Validation Checklist

Minimum validation:

```bash
go test ./x/privacy/circuit ./x/privacy/keeper ./x/privacy/types
go test ./x/privacy/client/sdk/...
make examples
make build
```

Release candidate validation:

```bash
make ci
make privacy-e2e-smoke
make release-pack-verify
```

Artifact validation:

1. Regenerate deposit/spend/joinsplit R1CS/PK/VK with `clairveil-setup`.
2. Confirm `privacy_zk_manifest.json` has `active_set_id=privacy-accounting-v2`.
3. Use checksum env with strict preflight.
4. Confirm downstream handoff docs and fixture schema reflect the new contract.

## 7. If External State Already Exists

This section is a contingency note, not the default pre-public path.

If external users have already been able to create privacy transactions or production-like state exists, the deployment owner must evaluate state exposure separately from code patch completion. Audit disclosure is useful for verifying transfer output recipient amount/from/to, but it does not retroactively prove cryptographically that an initial deposit commitment matched the actually locked amount.

At minimum, evaluation needs:

- module account balance
- deposit/withdraw totals
- deposit records
- known commitments and nullifiers
- wallet note opening
- transfer audit disclosures
- full lineage reconstruction

Migration, claim, or reset policy in that state must be decided by the chain deployment owner in a separate operations document.

## 8. Related Documents

- `docs/clairveil-circuits.md`
- `docs/clairveil-threat-model.md`
- `docs/clairveil-security-best-practices-review.md`
- `docs/clairveil-release-handoff-pack.md`
- `docs/clairveil-downstream-cosmos-integration-guide.md`
