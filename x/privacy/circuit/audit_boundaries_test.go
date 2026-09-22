package circuit

import (
	"fmt"
	"math/big"
	"reflect"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"
)

func TestP2PublicAndAuthorityBoundaries(t *testing.T) {
	for kind := auditfield.Kind(1); kind <= 4; kind++ {
		t.Run(kindName(kind), func(t *testing.T) {
			valid := newAuditFixture(t, kind, 1, 1)
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, reflect.New(reflect.TypeOf(valid.assignment).Elem()).Interface().(frontend.Circuit))
			require.NoError(t, err)
			attacks := map[string]func(*auditFixture){
				"epoch_zero": func(f *auditFixture) { f.public.KeyEpoch = 0 }, "expiry_zero": func(f *auditFixture) { f.public.ExpiresAtUnix = 0 },
				"epoch_overflow": func(f *auditFixture) { f.public.KeyEpoch = new(big.Int).Lsh(big.NewInt(1), 64) },
				"nonce_overflow": func(f *auditFixture) { f.random.Nonce = new(big.Int).Lsh(big.NewInt(1), 128) },
				"negative_amount_alias": func(f *auditFixture) {
					f.public.PublicAmount = new(big.Int).Sub(ecc.BN254.ScalarField(), big.NewInt(1))
				},
				"amount_overflow":   func(f *auditFixture) { f.public.PublicAmount = new(big.Int).Lsh(big.NewInt(1), 128) },
				"output_count_zero": func(f *auditFixture) { f.public.OutputCount = 63 },
				"key_identity":      func(f *auditFixture) { f.public.PKx = 0; f.public.PKy = 1 },
				"key_off_curve":     func(f *auditFixture) { f.public.PKx = 1; f.public.PKy = 1 },
				"key_id_replay":     func(f *auditFixture) { f.public.KeyIDHi = 0 },
				"second_root":       func(f *auditFixture) { f.public.CipherRoot1 = 0 },
				"r_field_max":       func(f *auditFixture) { f.random.R = new(big.Int).Sub(ecc.BN254.ScalarField(), big.NewInt(1)) },
			}
			if kind != auditfield.KindWithdraw {
				attacks["false_output_owner_reencrypt_resign"] = func(f *auditFixture) {
					idx := 2
					if kind == 3 {
						idx = 4
					}
					if kind == 4 {
						idx = 18
					}
					f.plain[idx] = auditfield.Field32FromUint64(42)
					f.encrypt(t)
				}
			}
			if kind >= 3 {
				attacks["public_transfer_target_nonzero"] = func(f *auditFixture) { f.public.PublicTargetHi = 1 }
				attacks["public_transfer_asset_nonzero"] = func(f *auditFixture) { f.public.PublicAsset = 1 }
			}
			if kind == 1 {
				attacks["deposit_merkle_nonzero"] = func(f *auditFixture) { f.public.MerkleRoot = 1 }
				attacks["deposit_disclosure_nonzero"] = func(f *auditFixture) { f.public.UserDisclosureRoot = 1 }
			}
			if kind == 2 {
				attacks["withdraw_commitment_nonzero"] = func(f *auditFixture) { f.public.CommitmentRoot = 1 }
			}
			if kind != 1 {
				attacks["signature_R_off_curve"] = func(f *auditFixture) { f.signature.R.X = 1; f.signature.R.Y = 1 }
				attacks["signature_S_q"] = func(f *auditFixture) {
					f.signature.S, _ = new(big.Int).SetString("2736030358979909402780800718157159386076813972158567259200215660948447373041", 10)
				}
				attacks["signature_zero"] = func(f *auditFixture) { f.signature.S = 0 }
				attacks["wrong_signature"] = func(f *auditFixture) { f.signature.S = 1 }
			}
			for name, attack := range attacks {
				t.Run(name, func(t *testing.T) {
					f := newAuditFixture(t, kind, 1, 1)
					attack(f)
					w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
					require.NoError(t, err)
					require.NotPanics(t, func() { require.Error(t, ccs.IsSolved(w)) })
				})
			}
		})
	}
}

func TestP2BatchPaddingAndActiveZero(t *testing.T) {
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &BatchJoinSplitAuditFieldV1{})
	require.NoError(t, err)
	for _, shape := range [][2]int{{1, 1}, {2, 2}, {16, 32}, {1, 2}} {
		t.Run(fmt.Sprintf("%dx%d", shape[0], shape[1]), func(t *testing.T) {
			old := buildBatchFeasibilityAssignment(t, shape[0], shape[1])
			if shape == [2]int{1, 2} {
				old.OutputAmounts[0] = big.NewInt(7)
				old.OutputAmounts[1] = big.NewInt(0)
				old.OutputPrivacyPolicies[1] = big.NewInt(0)
				old.UserDisclosureBlindings[1] = big.NewInt(0)
				refreshBatchFeasibilityPublicState(t, old, 1, 2)
			}
			f := newAuditFixture(t, auditfield.KindBatch16x32, shape[0], shape[1], old)
			w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
			require.NoError(t, err)
			require.NoError(t, ccs.IsSolved(w))
		})
	}
	attacks := map[string]func(*BatchJoinSplitAuditFieldV1){
		"disabled_amount":       func(c *BatchJoinSplitAuditFieldV1) { c.OutputAmounts[1] = 1 },
		"disabled_rho":          func(c *BatchJoinSplitAuditFieldV1) { c.OutputRandomness[1] = 1 },
		"disabled_policy":       func(c *BatchJoinSplitAuditFieldV1) { c.OutputPrivacyPolicies[1] = 1 },
		"disabled_blinding":     func(c *BatchJoinSplitAuditFieldV1) { c.FullDisclosureBlindings[1] = 1 },
		"disabled_input_path":   func(c *BatchJoinSplitAuditFieldV1) { c.InputPaths[1][0] = 1 },
		"disabled_input_helper": func(c *BatchJoinSplitAuditFieldV1) { c.InputPathHelpers[1][0] = 1 },
		"disabled_input_rho":    func(c *BatchJoinSplitAuditFieldV1) { c.InputRandomness[1] = 1 },
		"different_input_owner": func(c *BatchJoinSplitAuditFieldV1) {
			assignPubKey(&c.InputSpendPubKeys[0], scalarMulBase(big.NewInt(41)))
		},
		"membership":       func(c *BatchJoinSplitAuditFieldV1) { c.InputPaths[0][0] = 1 },
		"non_boolean_path": func(c *BatchJoinSplitAuditFieldV1) { c.InputPathHelpers[0][0] = 2 },
		"conservation":     func(c *BatchJoinSplitAuditFieldV1) { c.OutputAmounts[0] = 8 },
	}
	for name, attack := range attacks {
		t.Run(name, func(t *testing.T) {
			f := newAuditFixture(t, 4, 1, 1)
			attack(f.assignment.(*BatchJoinSplitAuditFieldV1))
			w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
			require.NoError(t, err)
			require.Error(t, ccs.IsSolved(w))
		})
	}
	t.Run("false_disabled_plaintext_reencrypt_resign", func(t *testing.T) {
		f := newAuditFixture(t, 4, 1, 1)
		f.plain[2] = auditfield.Field32FromUint64(1)
		f.encrypt(t)
		w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
		require.NoError(t, err)
		require.Error(t, ccs.IsSolved(w))
	})
}
func TestP2OutputOneArbitraryRecipient(t *testing.T) {
	old := buildValidJoinSplitAssignment(t)
	assignPubKey(&old.OutputSpendPubKeys[1], scalarMulBase(big.NewInt(41)))
	assignPubKey(&old.OutputViewPubKeys[1], scalarMulBase(big.NewInt(43)))
	old.Commitments[1] = fixtureCommitment(old.OutputSpendPubKeys[1], old.OutputViewPubKeys[1], old.OutputAmounts[1], old.AssetID, old.OutputRandomness[1])
	f := newAuditFixture(t, 3, 2, 2, old)
	ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &JoinSplitAuditFieldV1{})
	require.NoError(t, err)
	w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
	require.NoError(t, err)
	require.NoError(t, ccs.IsSolved(w))
}

func TestP2ExactDuplicateInputInflation(t *testing.T) {
	for _, kind := range []auditfield.Kind{3, 4} {
		var old frontend.Circuit
		inputs, outputs := 2, 2
		if kind == 3 {
			old = buildExactDuplicateInputInflationJoinSplit(t)
		} else {
			old = buildExactDuplicateInputInflationBatch(t)
		}
		f := newAuditFixture(t, kind, inputs, outputs, old)
		ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, reflect.New(reflect.TypeOf(f.assignment).Elem()).Interface().(frontend.Circuit))
		require.NoError(t, err)
		w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
		require.NoError(t, err)
		require.Error(t, ccs.IsSolved(w))
	}
}

// Each note is in range and all commitments, paths, disclosures and signatures
// are rebuilt. Only the operation total distinguishes the two witnesses.
func TestP2OperationAmountUint128Boundaries(t *testing.T) {
	max := privacytypes.MaxShieldedAmount()
	for _, kind := range []auditfield.Kind{auditfield.KindTransfer2x2, auditfield.KindBatch16x32} {
		t.Run(kindName(kind), func(t *testing.T) {
			var model frontend.Circuit = &JoinSplitAuditFieldV1{}
			if kind == auditfield.KindBatch16x32 {
				model = &BatchJoinSplitAuditFieldV1{}
			}
			ccs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, model)
			require.NoError(t, err)
			for _, overflow := range []bool{false, true} {
				t.Run(fmt.Sprintf("overflow_%t", overflow), func(t *testing.T) {
					input0 := new(big.Int).Sub(max, big.NewInt(1))
					output1 := big.NewInt(0)
					if overflow {
						input0 = new(big.Int).Set(max)
						output1 = big.NewInt(1)
					}
					var old frontend.Circuit
					if kind == auditfield.KindTransfer2x2 {
						old = buildJoinSplitAssignmentWithAmounts(t, [NumInputs]*big.Int{input0, big.NewInt(1)}, [NumOutputs]*big.Int{new(big.Int).Set(max), output1})
					} else {
						batch := buildBatchFeasibilityAssignment(t, 2, 2)
						batch.InputAmounts[0], batch.InputAmounts[1] = input0, big.NewInt(1)
						batch.OutputAmounts[0], batch.OutputAmounts[1] = new(big.Int).Set(max), output1
						refreshBatchFeasibilityPublicState(t, batch, 2, 2)
						old = batch
					}
					f := newAuditFixture(t, kind, 2, 2, old)
					w, err := frontend.NewWitness(f.assignment, ecc.BN254.ScalarField())
					require.NoError(t, err)
					err = ccs.IsSolved(w)
					if overflow {
						require.Error(t, err)
					} else {
						require.NoError(t, err)
					}
				})
			}
		})
	}
}
