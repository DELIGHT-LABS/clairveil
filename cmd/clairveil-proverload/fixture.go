package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"hash"
	"math/big"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	fr_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const generatedFixtureDenom = "uclair"

func generatedProverLoadRequests(now time.Time) (map[string]requestPayload, error) {
	transfer, err := generatedTransferRequest()
	if err != nil {
		return nil, fmt.Errorf("generate transfer request: %w", err)
	}
	withdraw, err := generatedWithdrawRequest(now)
	if err != nil {
		return nil, fmt.Errorf("generate withdraw request: %w", err)
	}
	return map[string]requestPayload{
		"transfer": transfer,
		"withdraw": withdraw,
	}, nil
}

func generatedTransferRequest() (requestPayload, error) {
	senderSpendScalar := big.NewInt(17)
	senderViewScalar := big.NewInt(19)
	recipientSpendScalar := big.NewInt(23)
	recipientViewScalar := big.NewInt(29)
	auditScalar := big.NewInt(31)
	selfViewScalar := big.NewInt(37)

	senderSpendPub := scalarMulBase(senderSpendScalar)
	senderViewPub := scalarMulBase(senderViewScalar)
	recipientSpendPub := scalarMulBase(recipientSpendScalar)
	recipientViewPub := scalarMulBase(recipientViewScalar)
	auditPub := scalarMulBase(auditScalar)
	selfViewPub := scalarMulBase(selfViewScalar)

	inputs := [2]privacyscan.SecretFoundNote{
		foundNote(fixtureSecretNote(senderSpendPub, senderViewPub, 7, 101, "Generated prover load transfer input 0")),
		foundNote(fixtureSecretNote(senderSpendPub, senderViewPub, 5, 103, "Generated prover load transfer input 1")),
	}

	provider, err := newGeneratedTransferMerklePathProvider(inputs)
	if err != nil {
		return requestPayload{}, err
	}
	payload, err := privacytransfer.BuildPreparedTransferPayload(
		context.Background(),
		provider,
		generatedTransferSigner{scalar: senderSpendScalar, pubKey: &senderSpendPub},
		privacytransfer.BuildTransferMessageInput{
			Creator:                        generatedCreatorAddress(),
			ChainID:                        "clairveil-load-1",
			ExpiresAtUnix:                  4_102_444_800,
			Inputs:                         inputs,
			RecipientSpendPubKey:           &recipientSpendPub,
			RecipientViewPubKey:            &recipientViewPub,
			TransferAmount:                 big.NewInt(6),
			TransferDenom:                  generatedFixtureDenom,
			SenderSpendPubKey:              &senderSpendPub,
			SenderViewPubKey:               &senderViewPub,
			UserPrivacyPolicy:              privacytypes.TransferPrivacyPolicyAllPrivate,
			UserDisclosureMode:             privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE,
			AuditDisclosureTargetPubKey:    &auditPub,
			AuditDisclosureTargetPubKeyBz:  encodedPointBytes(auditPub),
			SelfViewDisclosureTargetPubKey: &selfViewPub,
		},
	)
	if err != nil {
		return requestPayload{}, err
	}
	request, err := privacyprovertransport.NewTransferProofRequest(*payload)
	if err != nil {
		return requestPayload{}, err
	}
	body, err := request.MarshalIndentedJSON()
	if err != nil {
		return requestPayload{}, err
	}
	return requestPayload{Route: "transfer", Path: privacyprovertransport.TransferProofPath, Body: body}, nil
}

func generatedWithdrawRequest(now time.Time) (requestPayload, error) {
	spendScalar := big.NewInt(41)
	viewScalar := big.NewInt(43)
	spendPub := scalarMulBase(spendScalar)
	viewPub := scalarMulBase(viewScalar)
	note := foundNote(fixtureSecretNote(spendPub, viewPub, 10, 107, "Generated prover load withdraw input"))
	provider, err := newGeneratedWithdrawMerklePathProvider(note)
	if err != nil {
		return requestPayload{}, err
	}
	result, err := privacywithdraw.BuildPreparedWithdrawProverPayload(
		context.Background(),
		generatedWithdrawNoteSource{note: note},
		nil,
		provider,
		generatedWithdrawSigner{scalar: spendScalar, pubKey: &spendPub},
		privacywithdraw.BuildWithdrawPayloadInput{
			TargetCoin: sdk.NewInt64Coin(generatedFixtureDenom, 10),
			Recipient:  generatedRecipientAddress(),
			ChainID:    "clairveil-proverload",
			ExpiresAt:  now.Add(24 * time.Hour),
			AutoPlan:   false,
		},
	)
	if err != nil {
		return requestPayload{}, err
	}
	request, err := privacyprovertransport.NewWithdrawProofRequest(*result.Payload, now)
	if err != nil {
		return requestPayload{}, err
	}
	body, err := request.MarshalIndentedJSON()
	if err != nil {
		return requestPayload{}, err
	}
	return requestPayload{Route: "withdraw", Path: privacyprovertransport.WithdrawProofPath, Body: body}, nil
}

type generatedTransferMerklePathProvider struct {
	paths map[string]privacytransfer.MerklePathResult
}

func newGeneratedTransferMerklePathProvider(inputs [2]privacyscan.SecretFoundNote) (*generatedTransferMerklePathProvider, error) {
	left := fixtureCommitment(inputs[0].Note)
	right := fixtureCommitment(inputs[1].Note)
	root := twoLeafRoot(left, right)
	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(root)
	if err != nil {
		return nil, err
	}
	leftHex, err := privacyfield.CanonicalHexFromBigInt(left)
	if err != nil {
		return nil, err
	}
	rightHex, err := privacyfield.CanonicalHexFromBigInt(right)
	if err != nil {
		return nil, err
	}
	leftPath := make([]string, circuit.MerkleDepth)
	rightPath := make([]string, circuit.MerkleDepth)
	leftHelpers := make([]uint32, circuit.MerkleDepth)
	rightHelpers := make([]uint32, circuit.MerkleDepth)
	leftPath[0] = rightHex
	rightPath[0] = leftHex
	rightHelpers[0] = 1
	for level := 1; level < circuit.MerkleDepth; level++ {
		emptyHex, err := privacyfield.CanonicalHexFromBigInt(privacytypes.EmptyNoteTreeRootV1(uint32(level)))
		if err != nil {
			return nil, err
		}
		leftPath[level] = emptyHex
		rightPath[level] = emptyHex
	}
	return &generatedTransferMerklePathProvider{
		paths: map[string]privacytransfer.MerklePathResult{
			leftHex: {
				Root:       rootBytes,
				Path:       leftPath,
				PathHelper: leftHelpers,
			},
			rightHex: {
				Root:       rootBytes,
				Path:       rightPath,
				PathHelper: rightHelpers,
			},
		},
	}, nil
}

func (p *generatedTransferMerklePathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*privacytransfer.MerklePathResult, error) {
	result, ok := p.paths[commitmentHex]
	if !ok {
		return nil, fmt.Errorf("generated transfer merkle path not found for commitment %s", commitmentHex)
	}
	return &privacytransfer.MerklePathResult{
		Root:       append([]byte(nil), result.Root...),
		Path:       append([]string(nil), result.Path...),
		PathHelper: append([]uint32(nil), result.PathHelper...),
	}, nil
}

type generatedWithdrawMerklePathProvider struct {
	result privacywithdraw.MerklePathResult
}

func newGeneratedWithdrawMerklePathProvider(note privacyscan.SecretFoundNote) (*generatedWithdrawMerklePathProvider, error) {
	commitment := fixtureCommitment(note.Note)
	root := singleLeafRoot(commitment)
	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(root)
	if err != nil {
		return nil, err
	}
	path := make([]string, circuit.MerkleDepth)
	helpers := make([]uint32, circuit.MerkleDepth)
	for i := 0; i < circuit.MerkleDepth; i++ {
		emptyHex, err := privacyfield.CanonicalHexFromBigInt(privacytypes.EmptyNoteTreeRootV1(uint32(i)))
		if err != nil {
			return nil, err
		}
		path[i] = emptyHex
	}
	return &generatedWithdrawMerklePathProvider{
		result: privacywithdraw.MerklePathResult{
			Root:       rootBytes,
			Path:       path,
			PathHelper: helpers,
		},
	}, nil
}

func (p *generatedWithdrawMerklePathProvider) LookupMerklePath(context.Context, string) (*privacywithdraw.MerklePathResult, error) {
	return &privacywithdraw.MerklePathResult{
		Root:       append([]byte(nil), p.result.Root...),
		Path:       append([]string(nil), p.result.Path...),
		PathHelper: append([]uint32(nil), p.result.PathHelper...),
	}, nil
}

type generatedTransferSigner struct {
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s generatedTransferSigner) SignOwnerIntent(request privacytransfer.JoinSplitOwnerIntentSigningRequestV1) ([]byte, error) {
	return privacytransfer.SignValidatedJoinSplitOwnerIntentV1(request, func(msgHash *big.Int) ([]byte, error) {
		return signGeneratedNoteHash(msgHash, s.scalar, s.pubKey)
	})
}

type generatedWithdrawSigner struct {
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s generatedWithdrawSigner) SignSpendIntent(msgHash *big.Int) ([]byte, error) {
	return signGeneratedNoteHash(msgHash, s.scalar, s.pubKey)
}

type generatedWithdrawNoteSource struct {
	note privacyscan.SecretFoundNote
}

func (s generatedWithdrawNoteSource) LoadFoundNotes(context.Context) ([]privacyscan.SecretFoundNote, error) {
	return []privacyscan.SecretFoundNote{s.note}, nil
}

func foundNote(note privacytypes.SecretNoteV1) privacyscan.SecretFoundNote {
	nullifier, err := note.NullifierV1()
	if err != nil {
		panic(err)
	}
	nullifierBytes := nullifier.Bytes()
	return privacyscan.SecretFoundNote{
		Note:      note,
		Nullifier: hex.EncodeToString(nullifierBytes[:]),
		TxHash:    "GENERATED-PROVERLOAD",
		Height:    1,
		IsSpent:   false,
	}
}

func twoLeafRoot(leftLeaf, rightLeaf *big.Int) *big.Int {
	current := privacytypes.ComputeNoteTreeNodeV1(0, leftLeaf, rightLeaf)
	for level := uint32(1); level < circuit.MerkleDepth; level++ {
		current = privacytypes.ComputeNoteTreeNodeV1(level, current, privacytypes.EmptyNoteTreeRootV1(level))
	}
	return current
}

func singleLeafRoot(leaf *big.Int) *big.Int {
	current := leaf
	for level := uint32(0); level < circuit.MerkleDepth; level++ {
		current = privacytypes.ComputeNoteTreeNodeV1(level, current, privacytypes.EmptyNoteTreeRootV1(level))
	}
	return current
}

func scalarMulBase(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)

	var pubKey crypto_tedwards.PointAffine
	pubKey.ScalarMultiplication(&base, scalar)
	return pubKey
}

func pointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
}

func signGeneratedNoteHash(msgHash *big.Int, scalar *big.Int, pubKey *crypto_tedwards.PointAffine) ([]byte, error) {
	curve := crypto_tedwards.GetEdwardsCurve()
	frModulus := fr.Modulus()
	for {
		nonce, err := rand.Int(rand.Reader, &curve.Order)
		if err != nil {
			return nil, err
		}

		var base crypto_tedwards.PointAffine
		base.X.Set(&curve.Base.X)
		base.Y.Set(&curve.Base.Y)

		var pointR crypto_tedwards.PointAffine
		pointR.ScalarMultiplication(&base, nonce)

		hFunc := fr_mimc.NewMiMC()
		rx, ry := pointBigInts(pointR)
		ax, ay := pointBigInts(*pubKey)
		writePadded(hFunc, rx)
		writePadded(hFunc, ry)
		writePadded(hFunc, ax)
		writePadded(hFunc, ay)
		writePadded(hFunc, msgHash)

		hRAM := new(big.Int).SetBytes(hFunc.Sum(nil))
		s := new(big.Int).Mul(hRAM, scalar)
		s.Add(s, nonce)
		s.Mod(s, &curve.Order)
		if s.Cmp(frModulus) >= 0 {
			continue
		}

		rBytes := pointR.Bytes()
		sBytes := s.Bytes()
		paddedS := make([]byte, 32)
		copy(paddedS[32-len(sBytes):], sBytes)
		return append(rBytes[:], paddedS...), nil
	}
}

func writePadded(h hash.Hash, value *big.Int) {
	var elem fr.Element
	elem.SetBigInt(value)
	bytes := elem.Bytes()
	_, _ = h.Write(bytes[:])
}

func encodedPointBytes(point crypto_tedwards.PointAffine) []byte {
	bytes := point.Bytes()
	return append([]byte(nil), bytes[:]...)
}

func generatedCreatorAddress() string {
	return generatedRecipientAddress().String()
}

func generatedRecipientAddress() sdk.AccAddress {
	return sdk.AccAddress([]byte{
		0x01, 0x02, 0x03, 0x04, 0x05,
		0x06, 0x07, 0x08, 0x09, 0x0a,
		0x0b, 0x0c, 0x0d, 0x0e, 0x0f,
		0x10, 0x11, 0x12, 0x13, 0x14,
	})
}

func fixtureSecretNote(spend, view crypto_tedwards.PointAffine, amount, randomness uint64, memo string) privacytypes.SecretNoteV1 {
	spendX, spendY, err := privacycrypto.PublicPointFieldValues(spend)
	if err != nil {
		panic(err)
	}
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(view)
	if err != nil {
		panic(err)
	}
	note, err := privacytypes.NewSecretNoteV1(spendX, spendY, viewX, viewY, amount, privacytypes.ComputeSecretAssetIDV1(generatedFixtureDenom), privacycrypto.FieldValueFromUint64(randomness), memo)
	if err != nil {
		panic(err)
	}
	return *note
}
func fixtureCommitment(note privacytypes.SecretNoteV1) *big.Int {
	c, err := note.CommitmentV1()
	if err != nil {
		panic(err)
	}
	raw := c.Bytes()
	return new(big.Int).SetBytes(raw[:])
}
