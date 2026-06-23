package main

import (
	"context"
	"crypto/rand"
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

	senderSpendX, senderSpendY := pointBigInts(senderSpendPub)
	senderViewX, senderViewY := pointBigInts(senderViewPub)
	assetID := privacycrypto.HashString(generatedFixtureDenom)
	inputs := [2]privacyscan.FoundNote{
		foundNote(privacytypes.Note{
			ReceiverSpendPubKeyX: new(big.Int).Set(senderSpendX),
			ReceiverSpendPubKeyY: new(big.Int).Set(senderSpendY),
			ReceiverViewPubKeyX:  new(big.Int).Set(senderViewX),
			ReceiverViewPubKeyY:  new(big.Int).Set(senderViewY),
			Amount:               big.NewInt(7),
			AssetID:              new(big.Int).Set(assetID),
			Randomness:           big.NewInt(101),
			Memo:                 "Generated prover load transfer input 0",
		}),
		foundNote(privacytypes.Note{
			ReceiverSpendPubKeyX: new(big.Int).Set(senderSpendX),
			ReceiverSpendPubKeyY: new(big.Int).Set(senderSpendY),
			ReceiverViewPubKeyX:  new(big.Int).Set(senderViewX),
			ReceiverViewPubKeyY:  new(big.Int).Set(senderViewY),
			Amount:               big.NewInt(5),
			AssetID:              new(big.Int).Set(assetID),
			Randomness:           big.NewInt(103),
			Memo:                 "Generated prover load transfer input 1",
		}),
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
	spendX, spendY := pointBigInts(spendPub)
	viewX, viewY := pointBigInts(viewPub)

	note := foundNote(privacytypes.Note{
		ReceiverSpendPubKeyX: spendX,
		ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX:  viewX,
		ReceiverViewPubKeyY:  viewY,
		Amount:               big.NewInt(10),
		AssetID:              privacycrypto.HashString(generatedFixtureDenom),
		Randomness:           big.NewInt(107),
		Memo:                 "Generated prover load withdraw input",
	})
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

func newGeneratedTransferMerklePathProvider(inputs [2]privacyscan.FoundNote) (*generatedTransferMerklePathProvider, error) {
	left := inputs[0].Note.ComputeCommitment()
	right := inputs[1].Note.ComputeCommitment()
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
	return &generatedTransferMerklePathProvider{
		paths: map[string]privacytransfer.MerklePathResult{
			leftHex: {
				Root:       rootBytes,
				Path:       []string{rightHex},
				PathHelper: []uint32{0},
			},
			rightHex: {
				Root:       rootBytes,
				Path:       []string{leftHex},
				PathHelper: []uint32{1},
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

func newGeneratedWithdrawMerklePathProvider(note privacyscan.FoundNote) (*generatedWithdrawMerklePathProvider, error) {
	commitment := note.Note.ComputeCommitment()
	root := singleLeafRoot(commitment)
	rootBytes, err := privacyfield.CanonicalBytesFromBigInt(root)
	if err != nil {
		return nil, err
	}
	zeroHex, err := privacyfield.CanonicalHexFromBigInt(big.NewInt(0))
	if err != nil {
		return nil, err
	}
	path := make([]string, circuit.MerkleDepth)
	helpers := make([]uint32, circuit.MerkleDepth)
	for i := 0; i < circuit.MerkleDepth; i++ {
		path[i] = zeroHex
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

func (s generatedTransferSigner) SignNoteHash(msgHash *big.Int) ([]byte, error) {
	return signGeneratedNoteHash(msgHash, s.scalar, s.pubKey)
}

type generatedWithdrawSigner struct {
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s generatedWithdrawSigner) SignSpendNoteHash(msgHash *big.Int) ([]byte, error) {
	return signGeneratedNoteHash(msgHash, s.scalar, s.pubKey)
}

type generatedWithdrawNoteSource struct {
	note privacyscan.FoundNote
}

func (s generatedWithdrawNoteSource) LoadFoundNotes(context.Context) ([]privacyscan.FoundNote, error) {
	return []privacyscan.FoundNote{s.note}, nil
}

func foundNote(note privacytypes.Note) privacyscan.FoundNote {
	nullifierHex, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
	if err != nil {
		nullifierHex = note.ComputeNullifier().Text(16)
	}
	return privacyscan.FoundNote{
		Note:      note,
		Nullifier: nullifierHex,
		TxHash:    "GENERATED-PROVERLOAD",
		Height:    1,
		IsSpent:   false,
	}
}

func twoLeafRoot(leftLeaf, rightLeaf *big.Int) *big.Int {
	current := privacycrypto.MimcHash(leftLeaf, rightLeaf)
	zero := big.NewInt(0)
	for i := 1; i < circuit.MerkleDepth; i++ {
		current = privacycrypto.MimcHash(current, zero)
	}
	return current
}

func singleLeafRoot(leaf *big.Int) *big.Int {
	current := leaf
	zero := big.NewInt(0)
	for i := 0; i < circuit.MerkleDepth; i++ {
		current = privacycrypto.MimcHash(current, zero)
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
