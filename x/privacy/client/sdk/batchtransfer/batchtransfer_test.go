package batchtransfer

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	cryptoeddsa "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards/eddsa"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestPlanBatchTransferShapesAndModes(t *testing.T) {
	owner, view := testKey(t, 1), testKey(t, 2)
	cases := []struct {
		name                       string
		inputs, payments           int
		inputAmount, paymentAmount int64
		mode                       OutputMode
		wantOutputs                int
	}{
		{"one_one_compact", 1, 1, 5, 5, OutputModeCompact, 1},
		{"three_four_compact", 3, 4, 4, 3, OutputModeCompact, 4},
		{"thirty_one_plus_change", 1, 31, 32, 1, OutputModeCompact, 32},
		{"exact_thirty_two", 1, 32, 32, 1, OutputModeExact32, 32},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ins := make([]InputNote, tc.inputs)
			for i := range ins {
				ins[i] = InputNote{testNote(t, owner, view, tc.inputAmount, int64(i+10))}
			}
			pays := make([]Payment, tc.payments)
			for i := range pays {
				pays[i] = Payment{SpendPubKey: testKey(t, int64(100+i)), ViewPubKey: testKey(t, int64(200+i)), Amount: big.NewInt(tc.paymentAmount)}
			}
			plan, err := PlanBatchTransfer(PlanBatchTransferInput{Inputs: ins, Payments: pays, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: tc.mode})
			require.NoError(t, err)
			require.Len(t, plan.Outputs, tc.wantOutputs)
		})
	}
}

func TestPlanBatchTransferSeededShapeProperty(t *testing.T) {
	owner, view := testKey(t, 1), testKey(t, 2)
	for outputCount := 1; outputCount <= int(privacytypes.BatchJoinSplitV1MaxOutputs); outputCount++ {
		inputCount := (outputCount-1)%int(privacytypes.BatchJoinSplitV1MaxInputs) + 1
		seed := int64(5_200 + outputCount)
		rng := rand.New(rand.NewSource(seed))
		t.Run(fmt.Sprintf("seed_%d_inputs_%d_outputs_%d", seed, inputCount, outputCount), func(t *testing.T) {
			inputs := make([]InputNote, inputCount)
			inputTotal := int64(0)
			for i := range inputs {
				amount := int64(outputCount + 1 + rng.Intn(1_000))
				inputs[i] = InputNote{testNote(t, owner, view, amount, seed*100+int64(i)+1)}
				inputTotal += amount
			}

			payments := make([]Payment, outputCount)
			remaining := inputTotal
			for i := range payments {
				amount := remaining
				if i < len(payments)-1 {
					maxForThisPayment := remaining - int64(len(payments)-i-1)
					amount = 1 + rng.Int63n(maxForThisPayment)
				}
				remaining -= amount
				policy := uint32(rng.Intn(int(privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom + 1)))
				mode := privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE
				var target *crypto_tedwards.PointAffine
				if policy != privacytypes.TransferPrivacyPolicyAllPrivate {
					mode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC
					if i%2 == 1 {
						mode = privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED
						target = testKey(t, seed*10+int64(i)+701)
					}
				}
				payments[i] = Payment{
					SpendPubKey: testKey(t, seed*10+int64(i)+101),
					ViewPubKey:  testKey(t, seed*10+int64(i)+401),
					Amount:      big.NewInt(amount), PrivacyPolicy: policy,
					DisclosureMode: mode, DisclosureTargetPubKey: target,
				}
			}

			plan, err := PlanBatchTransfer(PlanBatchTransferInput{
				Inputs: inputs, Payments: payments, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: OutputModeCompact,
			})
			require.NoError(t, err)
			require.Len(t, plan.Inputs, inputCount)
			require.Len(t, plan.Outputs, outputCount)
			require.Equal(t, inputTotal, plan.InputTotal.Int64())
			require.Equal(t, inputTotal, plan.PaymentTotal.Int64())
			require.Zero(t, plan.Change.Sign())
			for i, output := range plan.Outputs {
				require.Equal(t, OutputPayment, output.Kind, "output %d", i)
				require.Positive(t, output.Amount.Sign(), "output %d", i)
			}
		})
	}
}

func TestPlanBatchTransferMixedDisclosurePaddingAndDuplicates(t *testing.T) {
	owner, view := testKey(t, 1), testKey(t, 2)
	in := InputNote{testNote(t, owner, view, 10, 3)}
	recipient := testKey(t, 4)
	target := testKey(t, 5)
	plan, err := PlanBatchTransfer(PlanBatchTransferInput{Inputs: []InputNote{in}, Payments: []Payment{{SpendPubKey: recipient, ViewPubKey: recipient, Amount: big.NewInt(3), PrivacyPolicy: privacytypes.TransferPrivacyPolicyDiscloseAmount, DisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, DisclosureTargetPubKey: target}}, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: OutputModeExact32})
	require.NoError(t, err)
	require.Len(t, plan.Outputs, 32)
	require.Equal(t, OutputChange, plan.Outputs[1].Kind)
	require.Equal(t, OutputPadding, plan.Outputs[31].Kind)
	_, err = PlanBatchTransfer(PlanBatchTransferInput{Inputs: []InputNote{in, in}, Payments: []Payment{{SpendPubKey: recipient, ViewPubKey: recipient, Amount: big.NewInt(1)}}, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: OutputModeCompact})
	require.ErrorContains(t, err, "duplicate")
}

func TestPreparedPayloadMutationExpirySignatureAndFileMode(t *testing.T) {
	payload := testPayload(t)
	require.NoError(t, ValidatePreparedBatchTransferPayloadMetadataAt(payload, time.Unix(payload.ExpiresAtUnix-1, 0)))
	mutated := *payload
	mutated.OwnerSignature = append([]byte(nil), payload.OwnerSignature...)
	mutated.OwnerSignature[63] ^= 1
	require.ErrorContains(t, ValidatePreparedBatchTransferPayloadMetadataAt(&mutated, time.Unix(payload.ExpiresAtUnix-1, 0)), "signature")
	require.ErrorContains(t, ValidatePreparedBatchTransferPayloadMetadataAt(payload, time.Unix(payload.ExpiresAtUnix, 0)), "expired")
	mutated = *payload
	mutated.PayloadHash = "00"
	require.ErrorContains(t, ValidatePreparedBatchTransferPayloadMetadataAt(&mutated, time.Unix(payload.ExpiresAtUnix-1, 0)), "hash")
	canonical, err := privacytypes.CanonicalMsgBatchTransferPayloadBytesV1(payload.effectMessage(nil, ""))
	require.NoError(t, err)
	request := signingRequest(payload, canonical)
	request.OrderedOutputs[0].Amount = new(big.Int).Add(request.OrderedOutputs[0].Amount, big.NewInt(1))
	require.ErrorContains(t, ValidateBatchTransferSigningRequest(request), "commitment recomputation")
	request = signingRequest(payload, canonical)
	request.ExpectedIntent = nil
	require.ErrorContains(t, ValidateBatchTransferSigningRequest(request), "expected intent")
	path := filepath.Join(t.TempDir(), "prepared.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, WritePreparedBatchTransferPayload(path, payload))
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestBatchStructuredSigningBoundaryRejectsGlobalSecretReuseBeforeRelease(t *testing.T) {
	ownerKey, err := cryptoeddsa.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x52}, 32)))
	require.NoError(t, err)
	ownerBytes := ownerKey.PublicKey.Bytes()
	owner, err := pointFromBytes(ownerBytes)
	require.NoError(t, err)
	view := testKey(t, 2)
	plan, err := PlanBatchTransfer(PlanBatchTransferInput{
		Inputs: []InputNote{{testNote(t, owner, view, 10, 3)}},
		Payments: []Payment{
			{SpendPubKey: testKey(t, 4), ViewPubKey: testKey(t, 5), Amount: big.NewInt(4), PrivacyPolicy: privacytypes.TransferPrivacyPolicyDiscloseAmount, DisclosureMode: privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC},
			{SpendPubKey: testKey(t, 6), ViewPubKey: testKey(t, 7), Amount: big.NewInt(6)},
		},
		OwnerSpendPubKey: owner,
		OwnerViewPubKey:  view,
		Mode:             OutputModeCompact,
	})
	require.NoError(t, err)
	prepared, err := PrepareBatchTransfer(context.Background(), pathProvider{}, plan)
	require.NoError(t, err)

	cases := []struct {
		name   string
		mutate func(*PreparedBatchTransfer)
	}{
		{
			name: "input_randomness_to_output_randomness",
			mutate: func(p *PreparedBatchTransfer) {
				p.Outputs[0].Note.Randomness = new(big.Int).Set(p.Inputs[0].Note.Randomness)
			},
		},
		{
			name: "input_randomness_to_output_full_blinding",
			mutate: func(p *PreparedBatchTransfer) {
				p.Outputs[0].FullDisclosureBlinding = new(big.Int).Set(p.Inputs[0].Note.Randomness)
			},
		},
		{
			name: "output_randomness_to_later_full_blinding",
			mutate: func(p *PreparedBatchTransfer) {
				p.Outputs[1].FullDisclosureBlinding = new(big.Int).Set(p.Outputs[0].Note.Randomness)
			},
		},
		{
			name: "active_user_blinding_to_later_output_randomness",
			mutate: func(p *PreparedBatchTransfer) {
				p.Outputs[1].Note.Randomness = new(big.Int).Set(p.Outputs[0].UserDisclosureBlinding)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			malicious := &PreparedBatchTransfer{
				Root:    append([]byte(nil), prepared.Root...),
				AssetID: new(big.Int).Set(prepared.AssetID),
				Inputs:  append([]PreparedBatchTransferInput(nil), prepared.Inputs...),
				Outputs: append([]PreparedBatchTransferOutput(nil), prepared.Outputs...),
			}
			tc.mutate(malicious)
			signer := &countingRejectingBatchSigner{}
			_, err := BuildPreparedBatchTransferPayload(malicious, signer, BuildPreparedBatchTransferPayloadInput{
				ChainID: "clairveil-test-1", ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), AuditKeyID: "audit-1", AuditKeyEpoch: 1,
				AuditDisclosureTargetPubKey: testKey(t, 9), DisableSelfViewDisclosure: true,
			})
			require.ErrorContains(t, err, "fresh and independent")
			require.Zero(t, signer.calls, "privacy-leaking intent must be rejected before signature release")
		})
	}
}

func TestPreparedPayloadValidationRejectsNilFieldsWithoutPanicking(t *testing.T) {
	payload := testPayload(t)
	now := time.Unix(payload.ExpiresAtUnix-1, 0)

	mutated := *payload
	mutated.MessageOutputs = append([]*privacytypes.BatchTransferOutput(nil), payload.MessageOutputs...)
	mutated.MessageOutputs[0] = nil
	require.ErrorContains(t, ValidatePreparedBatchTransferPayloadMetadataAt(&mutated, now), "message output 0")

	for _, tc := range []struct {
		name   string
		mutate func(*PreparedBatchTransferPayload)
		want   string
	}{
		{"asset_id", func(p *PreparedBatchTransferPayload) { p.AssetID = nil }, "asset id"},
		{"nullifier_root", func(p *PreparedBatchTransferPayload) { p.NullifierRoot = nil }, "nullifier root"},
		{"payload_digest", func(p *PreparedBatchTransferPayload) { p.PayloadDigestHi = nil }, "payload digest high"},
		{"expected_intent", func(p *PreparedBatchTransferPayload) { p.ExpectedIntent = nil }, "expected intent"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *payload
			tc.mutate(&copy)
			require.ErrorContains(t, ValidatePreparedBatchTransferPayloadMetadataAt(&copy, now), tc.want)
		})
	}
}

func TestBuildPreparedPayloadRejectsMalformedPreparedTransferWithoutPanicking(t *testing.T) {
	payload := testPayload(t)
	valid := &PreparedBatchTransfer{Root: payload.Root, AssetID: payload.AssetID, Inputs: payload.Inputs, Outputs: payload.Outputs}
	input := BuildPreparedBatchTransferPayloadInput{ChainID: "clairveil-test-1", ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), AuditDisclosureTargetPubKey: testKey(t, 9), DisableSelfViewDisclosure: true}
	signer := rejectingBatchSigner{}

	for _, tc := range []struct {
		name   string
		mutate func(*PreparedBatchTransfer)
		want   string
	}{
		{"nil_asset", func(p *PreparedBatchTransfer) { p.AssetID = nil }, "asset id"},
		{"empty_inputs", func(p *PreparedBatchTransfer) { p.Inputs = nil }, "input count"},
		{"too_many_outputs", func(p *PreparedBatchTransfer) {
			p.Outputs = append(p.Outputs, make([]PreparedBatchTransferOutput, 32)...)
		}, "output count"},
		{"nil_user_blinding", func(p *PreparedBatchTransfer) { p.Outputs[0].UserDisclosureBlinding = nil }, "user disclosure blinding"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *valid
			copy.Inputs = append([]PreparedBatchTransferInput(nil), valid.Inputs...)
			copy.Outputs = append([]PreparedBatchTransferOutput(nil), valid.Outputs...)
			tc.mutate(&copy)
			_, err := BuildPreparedBatchTransferPayload(&copy, signer, input)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

func TestPrepareBatchTransferRejectsRootMismatch(t *testing.T) {
	owner, view := testKey(t, 1), testKey(t, 2)
	notes := []InputNote{{testNote(t, owner, view, 2, 3)}, {testNote(t, owner, view, 2, 4)}}
	plan, err := PlanBatchTransfer(PlanBatchTransferInput{Inputs: notes, Payments: []Payment{{SpendPubKey: testKey(t, 5), ViewPubKey: testKey(t, 6), Amount: big.NewInt(4)}}, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: OutputModeCompact})
	require.NoError(t, err)
	provider := pathProvider{roots: map[string]byte{notes[0].Note.ComputeCommitment().String(): 1}}
	_, err = PrepareBatchTransfer(context.Background(), provider, plan)
	require.ErrorIs(t, err, ErrWalletSyncRequired)
}

func TestPrepareBatchTransferRejectsMalformedExportedPlanWithoutPanicking(t *testing.T) {
	owner, view := testKey(t, 1), testKey(t, 2)
	plan, err := PlanBatchTransfer(PlanBatchTransferInput{
		Inputs:           []InputNote{{testNote(t, owner, view, 5, 3)}},
		Payments:         []Payment{{SpendPubKey: testKey(t, 4), ViewPubKey: testKey(t, 5), Amount: big.NewInt(5)}},
		OwnerSpendPubKey: owner,
		OwnerViewPubKey:  view,
		Mode:             OutputModeCompact,
	})
	require.NoError(t, err)

	for _, tc := range []struct {
		name   string
		mutate func(*BatchTransferPlan)
		want   string
	}{
		{"nil_input_asset", func(p *BatchTransferPlan) { p.Inputs[0].Note.AssetID = nil }, "asset id"},
		{"nil_output_spend_key", func(p *BatchTransferPlan) { p.Outputs[0].SpendPubKey = nil }, "recipient keys"},
		{"nil_output_amount", func(p *BatchTransferPlan) { p.Outputs[0].Amount = nil }, "amount is required"},
		{"invalid_output_kind", func(p *BatchTransferPlan) { p.Outputs[0].Kind = OutputKind("invalid") }, "unsupported planned output kind"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := *plan
			copy.Inputs = append([]InputNote(nil), plan.Inputs...)
			copy.Outputs = append([]PlannedOutput(nil), plan.Outputs...)
			tc.mutate(&copy)
			_, err := PrepareBatchTransfer(context.Background(), pathProvider{}, &copy)
			require.ErrorContains(t, err, tc.want)
		})
	}
}

type structuredSigner struct{ key *cryptoeddsa.PrivateKey }

type rejectingBatchSigner struct{}

type countingRejectingBatchSigner struct{ calls int }

func (s *countingRejectingBatchSigner) SignBatchTransfer(BatchTransferSigningRequest) ([]byte, error) {
	s.calls++
	return nil, fmt.Errorf("unexpected signing call")
}

func (rejectingBatchSigner) SignBatchTransfer(BatchTransferSigningRequest) ([]byte, error) {
	return nil, fmt.Errorf("unexpected signing call")
}

func (s structuredSigner) SignBatchTransfer(req BatchTransferSigningRequest) ([]byte, error) {
	if err := ValidateBatchTransferSigningRequest(req); err != nil {
		return nil, err
	}
	return s.key.Sign(req.ExpectedIntent.FillBytes(make([]byte, 32)), mimc.NewMiMC())
}

type pathProvider struct{ roots map[string]byte }

func (p pathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*MerklePathResult, error) {
	commitment := new(big.Int)
	commitment.SetString(commitmentHex, 16)
	siblings := privacytypes.EmptyNoteTreeRootsV1(32)
	cur := new(big.Int).Set(commitment)
	path := make([]string, 32)
	helper := make([]uint32, 32)
	for i := 0; i < 32; i++ {
		path[i] = fmt.Sprintf("%x", siblings[i].FillBytes(make([]byte, 32)))
		cur = privacytypes.ComputeNoteTreeNodeV1(uint32(i), cur, siblings[i])
	}
	if len(p.roots) > 0 {
		for k := range p.roots {
			if k != commitment.String() {
				cur.Add(cur, big.NewInt(1))
			}
		}
	}
	return &MerklePathResult{Root: cur.FillBytes(make([]byte, 32)), Path: path, PathHelper: helper}, nil
}

func testPayload(t *testing.T) *PreparedBatchTransferPayload {
	ownerKey, err := cryptoeddsa.GenerateKey(bytes.NewReader(bytes.Repeat([]byte{0x42}, 32)))
	require.NoError(t, err)
	ownerBytes := ownerKey.PublicKey.Bytes()
	owner, err := pointFromBytes(ownerBytes)
	require.NoError(t, err)
	view := testKey(t, 2)
	note := testNote(t, owner, view, 5, 3)
	recipient := testKey(t, 4)
	plan, err := PlanBatchTransfer(PlanBatchTransferInput{Inputs: []InputNote{{note}}, Payments: []Payment{{SpendPubKey: recipient, ViewPubKey: recipient, Amount: big.NewInt(5)}}, OwnerSpendPubKey: owner, OwnerViewPubKey: view, Mode: OutputModeCompact})
	require.NoError(t, err)
	prepared, err := PrepareBatchTransfer(context.Background(), pathProvider{}, plan)
	require.NoError(t, err)
	payload, err := BuildPreparedBatchTransferPayload(prepared, structuredSigner{ownerKey}, BuildPreparedBatchTransferPayloadInput{ChainID: "clairveil-test-1", ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), AuditKeyID: "audit-1", AuditKeyEpoch: 1, AuditDisclosureTargetPubKey: testKey(t, 9), DisableSelfViewDisclosure: true})
	require.NoError(t, err)
	return payload
}
func testKey(t *testing.T, scalar int64) *crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var p crypto_tedwards.PointAffine
	p.ScalarMultiplication(&curve.Base, big.NewInt(scalar))
	return &p
}
func pointFromBytes(b []byte) (*crypto_tedwards.PointAffine, error) {
	var p crypto_tedwards.PointAffine
	_, err := p.SetBytes(b)
	return &p, err
}
func testNote(t *testing.T, spend, view *crypto_tedwards.PointAffine, amount, randomness int64) privacytypes.Note {
	sx, sy := pointCoordinates(spend)
	vx, vy := pointCoordinates(view)
	n := privacytypes.Note{ReceiverSpendPubKeyX: sx, ReceiverSpendPubKeyY: sy, ReceiverViewPubKeyX: vx, ReceiverViewPubKeyY: vy, Amount: big.NewInt(amount), AssetID: privacytypes.ComputeAssetIDV1("uclair"), Randomness: big.NewInt(randomness)}
	require.NoError(t, n.ValidateV1())
	return n
}
