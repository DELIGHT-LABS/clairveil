package circuit

import (
	"math/big"
	"math/rand"
	"reflect"
	"strings"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/stretchr/testify/require"
)

func TestBatchJoinSplit16x32ProductionPositiveMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("production batch solve matrix is skipped in short mode")
	}
	require.Equal(t, reflect.TypeOf(BatchJoinSplit16x32{}), reflect.TypeOf(BatchJoinSplit16x32FeasibilityCircuit{}))
	ccs := compiledBatchProductionCCS(t)

	tests := []struct {
		name        string
		inputCount  int
		outputCount int
		configure   func(*BatchJoinSplit16x32)
	}{
		{name: "1_input_1_output", inputCount: 1, outputCount: 1},
		{
			name: "1_input_2_outputs_with_change", inputCount: 1, outputCount: 2,
			configure: func(assignment *BatchJoinSplit16x32) {
				assignment.OutputAmounts[0] = big.NewInt(4)
				assignment.OutputAmounts[1] = big.NewInt(3)
				assignPubKey(&assignment.OutputSpendPubKeys[0], scalarMulBase(big.NewInt(23)))
				assignPubKey(&assignment.OutputViewPubKeys[0], scalarMulBase(big.NewInt(29)))
			},
		},
		{name: "3_inputs_4_outputs", inputCount: 3, outputCount: 4},
		{name: "8_inputs_16_outputs", inputCount: 8, outputCount: 16},
		{
			name: "16_inputs_31_outputs", inputCount: 16, outputCount: 31,
			configure: func(assignment *BatchJoinSplit16x32) {
				configureBatchRecipientsAndChange(assignment, 31, 16*7)
			},
		},
		{
			name: "16_inputs_32_outputs", inputCount: 16, outputCount: 32,
			configure: func(assignment *BatchJoinSplit16x32) {
				configureBatchRecipientsAndChange(assignment, 32, 16*7)
			},
		},
		{
			name: "mixed_user_disclosure", inputCount: 3, outputCount: 8,
			configure: func(assignment *BatchJoinSplit16x32) {
				configureBatchMixedDisclosure(assignment, 8)
			},
		},
		{
			name: "active_zero_value_padding", inputCount: 1, outputCount: 2,
			configure: func(assignment *BatchJoinSplit16x32) {
				assignment.OutputAmounts[0] = big.NewInt(7)
				assignment.OutputAmounts[1] = big.NewInt(0)
				assignment.OutputPrivacyPolicies[1] = big.NewInt(int64(privacytypes.TransferPrivacyPolicyAllPrivate))
				assignment.UserDisclosureBlindings[1] = big.NewInt(0)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assignment := buildBatchFeasibilityAssignment(t, tc.inputCount, tc.outputCount)
			if tc.configure != nil {
				tc.configure(assignment)
			}
			refreshBatchFeasibilityPublicState(t, assignment, tc.inputCount, tc.outputCount)
			assertBatchProductionSolve(t, ccs, assignment, true)
		})
	}
}

func TestBatchJoinSplit16x32SeededShapeProperty(t *testing.T) {
	if testing.Short() {
		t.Skip("production batch property matrix is skipped in short mode")
	}
	ccs := compiledBatchProductionCCS(t)
	for inputCount := 1; inputCount <= MaxBatchJoinSplitInputs; inputCount++ {
		seed := int64(4_100 + inputCount)
		rng := rand.New(rand.NewSource(seed))
		outputCount := 1 + rng.Intn(MaxBatchJoinSplitOutputs)
		t.Run(strings.Join([]string{
			"seed", big.NewInt(seed).String(),
			"inputs", big.NewInt(int64(inputCount)).String(),
			"outputs", big.NewInt(int64(outputCount)).String(),
		}, "_"), func(t *testing.T) {
			assignment := buildBatchFeasibilityAssignment(t, inputCount, outputCount)
			total := int64(0)
			for i := 0; i < inputCount; i++ {
				amount := int64(1 + rng.Intn(1_000_000))
				assignment.InputAmounts[i] = big.NewInt(amount)
				assignment.InputRandomness[i] = big.NewInt(seed*1_000 + int64(i) + 1)
				total += amount
			}

			remaining := total
			for i := 0; i < outputCount; i++ {
				amount := remaining
				if i < outputCount-1 && remaining > 0 {
					amount = rng.Int63n(remaining + 1)
				}
				remaining -= amount
				assignment.OutputAmounts[i] = big.NewInt(amount)
				assignment.OutputRandomness[i] = big.NewInt(seed*10_000 + int64(i) + 1)
				assignPubKey(&assignment.OutputSpendPubKeys[i], scalarMulBase(big.NewInt(seed*100+int64(i)+101)))
				assignPubKey(&assignment.OutputViewPubKeys[i], scalarMulBase(big.NewInt(seed*100+int64(i)+201)))
				policy := uint32(rng.Intn(int(privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom + 1)))
				assignment.OutputPrivacyPolicies[i] = new(big.Int).SetUint64(uint64(policy))
				if policy == privacytypes.TransferPrivacyPolicyAllPrivate {
					assignment.UserDisclosureBlindings[i] = big.NewInt(0)
				} else {
					assignment.UserDisclosureBlindings[i] = big.NewInt(seed*20_000 + int64(i) + 1)
				}
				assignment.FullDisclosureBlindings[i] = big.NewInt(seed*30_000 + int64(i) + 1)
			}
			assignment.PayloadDigestHi = big.NewInt(rng.Int63())
			assignment.PayloadDigestLo = big.NewInt(rng.Int63())
			refreshBatchFeasibilityPublicState(t, assignment, inputCount, outputCount)
			assertBatchProductionSolve(t, ccs, assignment, true)
		})
	}
}

// Self-view encryption is intentionally outside the circuit: the optional
// all-or-none ciphertext changes PayloadDigestHi/Lo, while the circuit proves
// the same FullDisclosureRoot and owner intent. There is no self-view witness,
// key, enable bit, or separate root in BatchJoinSplit16x32.
func TestBatchJoinSplit16x32SelfViewIsPayloadOnly(t *testing.T) {
	if testing.Short() {
		t.Skip("production batch solve matrix is skipped in short mode")
	}
	circuitType := reflect.TypeOf(BatchJoinSplit16x32{})
	for i := 0; i < circuitType.NumField(); i++ {
		require.NotContains(t, strings.ToLower(circuitType.Field(i).Name), "selfview")
	}
	_, hasPayloadHi := circuitType.FieldByName("PayloadDigestHi")
	_, hasPayloadLo := circuitType.FieldByName("PayloadDigestLo")
	require.True(t, hasPayloadHi)
	require.True(t, hasPayloadLo)

	ccs := compiledBatchProductionCCS(t)
	for _, tc := range []struct {
		name      string
		payloadHi int64
		payloadLo int64
	}{
		{name: "self_view_disabled", payloadHi: 101, payloadLo: 103},
		{name: "self_view_enabled", payloadHi: 107, payloadLo: 109},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assignment := buildBatchFeasibilityAssignment(t, 1, 1)
			assignment.PayloadDigestHi = big.NewInt(tc.payloadHi)
			assignment.PayloadDigestLo = big.NewInt(tc.payloadLo)
			refreshBatchFeasibilityPublicState(t, assignment, 1, 1)
			assertBatchProductionSolve(t, ccs, assignment, true)
		})
	}
}

func TestBatchJoinSplit16x32ProductionNegativeMatrix(t *testing.T) {
	if testing.Short() {
		t.Skip("production batch solve matrix is skipped in short mode")
	}
	ccs := compiledBatchProductionCCS(t)
	base := buildBatchFeasibilityAssignment(t, 4, 5)
	assertBatchProductionSolve(t, ccs, base, true)

	mixedBase := buildBatchFeasibilityAssignment(t, 4, 5)
	configureBatchMixedDisclosure(mixedBase, 5)
	refreshBatchFeasibilityPublicState(t, mixedBase, 4, 5)
	assertBatchProductionSolve(t, ccs, mixedBase, true)

	type negativeCase struct {
		name   string
		base   *BatchJoinSplit16x32
		mutate func(testing.TB, *BatchJoinSplit16x32)
	}
	otherKey := scalarMulBase(big.NewInt(97))
	overflowAmount := new(big.Int).Add(privacytypes.MaxShieldedAmount(), big.NewInt(1))
	outOfRangeLimb := new(big.Int).Lsh(big.NewInt(1), 128)
	offCurveX, offCurveY := invalidEdwardsPointForTest(t)
	lowOrderY := new(big.Int).Sub(ecc.BN254.ScalarField(), big.NewInt(1))

	tests := []negativeCase{
		{name: "input_count_zero", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputCount = big.NewInt(0) }},
		{name: "input_count_max_plus_one", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.InputCount = big.NewInt(int64(MaxBatchJoinSplitInputs + 1))
		}},
		{name: "output_count_zero", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputCount = big.NewInt(0) }},
		{name: "output_count_max_plus_one", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.OutputCount = big.NewInt(int64(MaxBatchJoinSplitOutputs + 1))
		}},
		{name: "count_non_small_field_value", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputCount = new(big.Int).Set(outOfRangeLimb) }},
		{
			name: "count_shrinks_over_populated_slot", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.InputCount = big.NewInt(3)
				resignBatchFixture(t, a)
			},
		},
		{
			name: "count_expands_over_disabled_sentinel", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.OutputCount = big.NewInt(6)
				resignBatchFixture(t, a)
			},
		},
		{name: "disabled_input_amount", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputAmounts[4] = big.NewInt(1) }},
		{name: "disabled_input_randomness", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputRandomness[4] = big.NewInt(1) }},
		{name: "disabled_input_path", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputPaths[4][0] = big.NewInt(1) }},
		{name: "disabled_input_helper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputPathHelpers[4][0] = big.NewInt(1) }},
		{name: "disabled_input_spend_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { assignPubKey(&a.InputSpendPubKeys[4], otherKey) }},
		{name: "disabled_input_view_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { assignPubKey(&a.InputViewPubKeys[4], otherKey) }},
		{name: "disabled_output_amount", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputAmounts[5] = big.NewInt(1) }},
		{name: "disabled_output_randomness", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputRandomness[5] = big.NewInt(1) }},
		{name: "disabled_output_spend_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { assignPubKey(&a.OutputSpendPubKeys[5], otherKey) }},
		{name: "disabled_output_view_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { assignPubKey(&a.OutputViewPubKeys[5], otherKey) }},
		{name: "disabled_output_policy", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputPrivacyPolicies[5] = big.NewInt(1) }},
		{name: "disabled_output_user_blinding", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.UserDisclosureBlindings[5] = big.NewInt(1) }},
		{name: "disabled_output_full_blinding", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.FullDisclosureBlindings[5] = big.NewInt(1) }},
		{name: "invalid_merkle_path", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputPaths[0][0] = addOne(a.InputPaths[0][0]) }},
		{name: "non_boolean_merkle_helper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputPathHelpers[0][0] = big.NewInt(2) }},
		{
			name: "duplicate_adjacent_nullifier", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.InputAmounts[1] = cloneBigInt(a.InputAmounts[0])
				a.InputRandomness[1] = cloneBigInt(a.InputRandomness[0])
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{
			name: "duplicate_non_adjacent_nullifier", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.InputAmounts[3] = cloneBigInt(a.InputAmounts[0])
				a.InputRandomness[3] = cloneBigInt(a.InputRandomness[0])
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{
			name: "duplicate_active_commitment", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.OutputAmounts[3] = cloneBigInt(a.OutputAmounts[1])
				a.OutputRandomness[3] = cloneBigInt(a.OutputRandomness[1])
				a.OutputSpendPubKeys[3] = a.OutputSpendPubKeys[1]
				a.OutputViewPubKeys[3] = a.OutputViewPubKeys[1]
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{
			name: "wrong_input_owner_key", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				assignPubKey(&a.InputSpendPubKeys[1], otherKey)
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{
			name: "wrong_input_view_key", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				assignPubKey(&a.InputViewPubKeys[1], otherKey)
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{name: "wrong_asset", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.AssetID = big.NewInt(12) }},
		{name: "identity_output_spend_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.OutputSpendPubKeys[0].A.X, a.OutputSpendPubKeys[0].A.Y = big.NewInt(0), big.NewInt(1)
		}},
		{name: "low_order_output_view_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.OutputViewPubKeys[0].A.X, a.OutputViewPubKeys[0].A.Y = big.NewInt(0), new(big.Int).Set(lowOrderY)
		}},
		{name: "off_curve_output_spend_key", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.OutputSpendPubKeys[0].A.X, a.OutputSpendPubKeys[0].A.Y = new(big.Int).Set(offCurveX), new(big.Int).Set(offCurveY)
		}},
		{name: "input_amount_overflow", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.InputAmounts[0] = new(big.Int).Set(overflowAmount) }},
		{name: "output_amount_overflow", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputAmounts[0] = new(big.Int).Set(overflowAmount) }},
		{
			name: "value_conservation", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.OutputAmounts[0] = new(big.Int).Add(a.OutputAmounts[0].(*big.Int), big.NewInt(1))
				refreshBatchFeasibilityPublicState(t, a, 4, 5)
			},
		},
		{name: "active_policy_out_of_range", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OutputPrivacyPolicies[0] = big.NewInt(8) }},
		{
			name: "merkle_root_tamper", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.MerkleRoot = addOne(a.MerkleRoot)
				resignBatchFixture(t, a)
			},
		},
		{
			name: "nullifier_root_tamper", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.NullifierRoot = addOne(a.NullifierRoot)
				resignBatchFixture(t, a)
			},
		},
		{
			name: "commitment_root_tamper", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.CommitmentRoot = addOne(a.CommitmentRoot)
				resignBatchFixture(t, a)
			},
		},
		{
			name: "user_disclosure_root_tamper", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.UserDisclosureRoot = addOne(a.UserDisclosureRoot)
				resignBatchFixture(t, a)
			},
		},
		{
			name: "full_disclosure_root_tamper", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				a.FullDisclosureRoot = addOne(a.FullDisclosureRoot)
				resignBatchFixture(t, a)
			},
		},
		{name: "chain_domain_hi_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.ChainDomainHi = addOne(a.ChainDomainHi) }},
		{name: "chain_domain_lo_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.ChainDomainLo = addOne(a.ChainDomainLo) }},
		{name: "chain_domain_limb_out_of_range", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.ChainDomainHi = new(big.Int).Set(outOfRangeLimb) }},
		{name: "expiry_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.ExpiresAtUnix = addOne(a.ExpiresAtUnix) }},
		{name: "expiry_zero", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.ExpiresAtUnix = big.NewInt(0) }},
		{name: "payload_digest_hi_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.PayloadDigestHi = addOne(a.PayloadDigestHi) }},
		{name: "payload_digest_lo_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.PayloadDigestLo = addOne(a.PayloadDigestLo) }},
		{name: "payload_digest_limb_out_of_range", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.PayloadDigestLo = new(big.Int).Set(outOfRangeLimb) }},
		{name: "wrong_circuit_domain_signature", base: base, mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
			signBatchFixtureWithCircuitDomain(t, a, "clairveil.batch-joinsplit-16x32.v2")
		}},
		{name: "signature_scalar_zero", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OwnerSignature.S = big.NewInt(0) }},
		{name: "signature_scalar_above_order", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.OwnerSignature.S = signatureScalarAboveOrderForTest() }},
		{name: "signature_identity_R", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.OwnerSignature.R.X, a.OwnerSignature.R.Y = big.NewInt(0), big.NewInt(1)
		}},
		{
			name: "output_reorder", base: base,
			mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { swapBatchFixtureOutputs(a, 0, 1) },
		},
		{
			name: "disclosure_reorder", base: mixedBase,
			mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
				a.OutputPrivacyPolicies[0], a.OutputPrivacyPolicies[1] = a.OutputPrivacyPolicies[1], a.OutputPrivacyPolicies[0]
				a.UserDisclosureBlindings[0], a.UserDisclosureBlindings[1] = a.UserDisclosureBlindings[1], a.UserDisclosureBlindings[0]
				a.FullDisclosureBlindings[0], a.FullDisclosureBlindings[1] = a.FullDisclosureBlindings[1], a.FullDisclosureBlindings[0]
			},
		},
		{name: "user_blinding_removed", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.UserDisclosureBlindings[0] = big.NewInt(0) }},
		{name: "full_blinding_removed", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) { a.FullDisclosureBlindings[0] = big.NewInt(0) }},
		{name: "user_blinding_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.UserDisclosureBlindings[0] = addOne(a.UserDisclosureBlindings[0])
		}},
		{name: "full_blinding_tamper", base: base, mutate: func(_ testing.TB, a *BatchJoinSplit16x32) {
			a.FullDisclosureBlindings[0] = addOne(a.FullDisclosureBlindings[0])
		}},
		{
			name: "vector_type_confusion", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				values := batchFixtureOutputCommitments(a, 5)
				confused, err := privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorFullDisclosureV1, 5, values)
				require.NoError(t, err)
				require.NotEqual(t, a.CommitmentRoot.(*big.Int), confused)
				a.CommitmentRoot = confused
				resignBatchFixture(t, a)
			},
		},
		{
			name: "vector_node_level_confusion", base: base,
			mutate: func(t testing.TB, a *BatchJoinSplit16x32) {
				values := batchFixtureOutputCommitments(a, 5)
				confused := batchFixtureVectorRootWithLevelOffset(privacytypes.BatchVectorCommitmentV1, 5, values, 1)
				require.NotEqual(t, a.CommitmentRoot.(*big.Int), confused)
				a.CommitmentRoot = confused
				resignBatchFixture(t, a)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tampered := *tc.base
			tc.mutate(t, &tampered)
			assertBatchProductionSolve(t, ccs, &tampered, false)
		})
	}
}

func assertBatchProductionSolve(
	t testing.TB,
	ccs constraint.ConstraintSystem,
	assignment *BatchJoinSplit16x32,
	wantSuccess bool,
) {
	t.Helper()
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		if wantSuccess {
			require.NoError(t, err)
		}
		return
	}
	_, err = ccs.Solve(witness)
	if wantSuccess {
		require.NoError(t, err)
		return
	}
	require.Error(t, err)
}

func configureBatchMixedDisclosure(assignment *BatchJoinSplit16x32, outputCount int) {
	for i := 0; i < outputCount; i++ {
		policy := uint32(i) % (privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom + 1)
		assignment.OutputPrivacyPolicies[i] = new(big.Int).SetUint64(uint64(policy))
		if policy == privacytypes.TransferPrivacyPolicyAllPrivate {
			assignment.UserDisclosureBlindings[i] = big.NewInt(0)
		} else {
			assignment.UserDisclosureBlindings[i] = big.NewInt(int64(2_000 + i))
		}
	}
}

func configureBatchRecipientsAndChange(assignment *BatchJoinSplit16x32, outputCount int, totalInput int64) {
	for i := 0; i < outputCount-1; i++ {
		assignment.OutputAmounts[i] = big.NewInt(1)
		assignPubKey(&assignment.OutputSpendPubKeys[i], scalarMulBase(big.NewInt(int64(100+i))))
		assignPubKey(&assignment.OutputViewPubKeys[i], scalarMulBase(big.NewInt(int64(200+i))))
	}
	assignment.OutputAmounts[outputCount-1] = big.NewInt(totalInput - int64(outputCount-1))
}

func resignBatchFixture(t testing.TB, assignment *BatchJoinSplit16x32) {
	t.Helper()
	intent, err := privacytypes.ComputeBatchTransferIntentV1(privacytypes.BatchTransferIntentV1Input{
		ChainDomainHi: assignment.ChainDomainHi.(*big.Int), ChainDomainLo: assignment.ChainDomainLo.(*big.Int),
		MerkleRoot: assignment.MerkleRoot.(*big.Int),
		InputCount: uint32(assignment.InputCount.(*big.Int).Uint64()), OutputCount: uint32(assignment.OutputCount.(*big.Int).Uint64()),
		AssetID:       assignment.AssetID.(*big.Int),
		NullifierRoot: assignment.NullifierRoot.(*big.Int), CommitmentRoot: assignment.CommitmentRoot.(*big.Int),
		UserDisclosureRoot: assignment.UserDisclosureRoot.(*big.Int), FullDisclosureRoot: assignment.FullDisclosureRoot.(*big.Int),
		PayloadDigestHi: assignment.PayloadDigestHi.(*big.Int), PayloadDigestLo: assignment.PayloadDigestLo.(*big.Int),
		ExpiresAtUnix: assignment.ExpiresAtUnix.(*big.Int).Int64(),
	})
	require.NoError(t, err)
	ownerSpendScalar := big.NewInt(17)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerSpendScalar, scalarMulBase(ownerSpendScalar))
}

func signBatchFixtureWithCircuitDomain(t testing.TB, assignment *BatchJoinSplit16x32, circuitDomain string) {
	t.Helper()
	intent := privacycrypto.MimcHash(
		privacytypes.DomainFieldV1(privacytypes.BatchTransferIntentV1DomainLabel),
		assignment.ChainDomainHi.(*big.Int), assignment.ChainDomainLo.(*big.Int),
		privacytypes.DomainFieldV1(circuitDomain),
		assignment.MerkleRoot.(*big.Int),
		assignment.InputCount.(*big.Int), assignment.OutputCount.(*big.Int),
		assignment.AssetID.(*big.Int),
		assignment.NullifierRoot.(*big.Int), assignment.CommitmentRoot.(*big.Int),
		assignment.UserDisclosureRoot.(*big.Int), assignment.FullDisclosureRoot.(*big.Int),
		assignment.PayloadDigestHi.(*big.Int), assignment.PayloadDigestLo.(*big.Int),
		assignment.ExpiresAtUnix.(*big.Int),
	)
	ownerSpendScalar := big.NewInt(17)
	assignment.OwnerSignature = signSpendMessage(t, intent, ownerSpendScalar, scalarMulBase(ownerSpendScalar))
}

func batchFixtureOutputCommitments(assignment *BatchJoinSplit16x32, outputCount int) []*big.Int {
	values := zeroBigIntVector(MaxBatchJoinSplitOutputs)
	assetID := assignment.AssetID.(*big.Int)
	for i := 0; i < outputCount; i++ {
		values[i] = privacytypes.ComputeNoteCommitmentV1(
			assignment.OutputSpendPubKeys[i].A.X.(*big.Int), assignment.OutputSpendPubKeys[i].A.Y.(*big.Int),
			assignment.OutputViewPubKeys[i].A.X.(*big.Int), assignment.OutputViewPubKeys[i].A.Y.(*big.Int),
			assignment.OutputAmounts[i].(*big.Int), assetID, assignment.OutputRandomness[i].(*big.Int),
		)
	}
	return values
}

func batchFixtureVectorRootWithLevelOffset(
	kind privacytypes.BatchVectorKindV1,
	count uint32,
	values []*big.Int,
	levelOffset uint32,
) *big.Int {
	capacity, err := kind.Capacity()
	if err != nil {
		panic(err)
	}
	leafDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "leaf"))
	nodeDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "node"))
	rootDomain := privacytypes.DomainFieldV1(batchVectorDomainLabel(kind, "root"))
	layer := make([]*big.Int, int(capacity))
	for i := uint32(0); i < capacity; i++ {
		enabled := big.NewInt(0)
		if i < count {
			enabled = big.NewInt(1)
		}
		layer[i] = privacycrypto.MimcHash(leafDomain, new(big.Int).SetUint64(uint64(i)), enabled, values[i])
	}
	for level := uint32(0); len(layer) > 1; level++ {
		next := make([]*big.Int, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = privacycrypto.MimcHash(nodeDomain, new(big.Int).SetUint64(uint64(level+levelOffset)), layer[i], layer[i+1])
		}
		layer = next
	}
	return privacycrypto.MimcHash(rootDomain, new(big.Int).SetUint64(uint64(capacity)), new(big.Int).SetUint64(uint64(count)), layer[0])
}

func swapBatchFixtureOutputs(assignment *BatchJoinSplit16x32, i, j int) {
	assignment.OutputAmounts[i], assignment.OutputAmounts[j] = assignment.OutputAmounts[j], assignment.OutputAmounts[i]
	assignment.OutputRandomness[i], assignment.OutputRandomness[j] = assignment.OutputRandomness[j], assignment.OutputRandomness[i]
	assignment.OutputSpendPubKeys[i], assignment.OutputSpendPubKeys[j] = assignment.OutputSpendPubKeys[j], assignment.OutputSpendPubKeys[i]
	assignment.OutputViewPubKeys[i], assignment.OutputViewPubKeys[j] = assignment.OutputViewPubKeys[j], assignment.OutputViewPubKeys[i]
	assignment.OutputPrivacyPolicies[i], assignment.OutputPrivacyPolicies[j] = assignment.OutputPrivacyPolicies[j], assignment.OutputPrivacyPolicies[i]
	assignment.UserDisclosureBlindings[i], assignment.UserDisclosureBlindings[j] = assignment.UserDisclosureBlindings[j], assignment.UserDisclosureBlindings[i]
	assignment.FullDisclosureBlindings[i], assignment.FullDisclosureBlindings[j] = assignment.FullDisclosureBlindings[j], assignment.FullDisclosureBlindings[i]
}

func cloneBigInt(value frontend.Variable) *big.Int {
	return new(big.Int).Set(value.(*big.Int))
}

func addOne(value frontend.Variable) *big.Int {
	return new(big.Int).Add(value.(*big.Int), big.NewInt(1))
}
