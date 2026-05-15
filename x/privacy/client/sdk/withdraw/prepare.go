package withdraw

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/std/signature/eddsa"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyscan "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/scan"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type MerklePathResult struct {
	Root       []byte
	Path       []string
	PathHelper []uint32
}

type MerklePathProvider interface {
	LookupMerklePath(ctx context.Context, commitmentHex string) (*MerklePathResult, error)
}

type SpendNoteHashSigner interface {
	SignSpendNoteHash(msgHash *big.Int) ([]byte, error)
}

type PrepareSpendWithdrawInput struct {
	Note           privacyscan.FoundNote
	RecipientBytes []byte
}

type PreparedSpendWithdraw struct {
	Assignment     circuit.SpendCircuit
	RootBytes      []byte
	NullifierBytes []byte
	MerklePath     []string
	PathHelper     []uint32
	Signature      []byte
}

func PrepareSpendWithdraw(
	ctx context.Context,
	provider MerklePathProvider,
	signer SpendNoteHashSigner,
	input PrepareSpendWithdrawInput,
) (*PreparedSpendWithdraw, error) {
	if provider == nil {
		return nil, fmt.Errorf("a merkle path provider is required to prepare a spend withdraw proof")
	}
	if signer == nil {
		return nil, fmt.Errorf("a spend note hash signer is required to prepare a spend withdraw proof")
	}
	if len(input.RecipientBytes) == 0 {
		return nil, fmt.Errorf("recipient bytes are required to prepare a spend withdraw proof")
	}

	selectedNote := input.Note.Note
	commitment := selectedNote.ComputeCommitment()
	commitmentHex, err := privacyfield.CanonicalHexFromBigInt(commitment)
	if err != nil {
		return nil, fmt.Errorf("invalid selected note commitment: %w", err)
	}

	merklePath, err := provider.LookupMerklePath(ctx, commitmentHex)
	if err != nil {
		return nil, fmt.Errorf("merkle path query failed for the selected note: %w", err)
	}
	if err := privacyfield.ValidateCanonicalBytes32(merklePath.Root); err != nil {
		return nil, fmt.Errorf("invalid merkle root for the selected note: %w", err)
	}

	var assignment circuit.SpendCircuit
	assignment.MerkleRoot = new(big.Int).SetBytes(merklePath.Root)
	assignment.Amount = selectedNote.Amount
	recipientInt := new(big.Int).SetBytes(input.RecipientBytes)
	assignment.Recipient = recipientInt
	assignment.AssetID = selectedNote.AssetID
	assignment.Nullifier = selectedNote.ComputeNullifier()

	pathNodes, pathHelpers := decodeMerkleProof(merklePath.Path, merklePath.PathHelper)
	for i := 0; i < circuit.MerkleDepth; i++ {
		assignment.Path[i] = pathNodes[i]
		assignment.PathHelper[i] = pathHelpers[i]
	}

	spendPubKey, err := spendPubKeyFromNote(selectedNote)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver spend key in the selected note: %w", err)
	}
	assignPubKey(&assignment.ReceiverSpendPubKey, *spendPubKey)

	viewPubKey, err := viewPubKeyFromNote(selectedNote)
	if err != nil {
		return nil, fmt.Errorf("invalid receiver view key in the selected note: %w", err)
	}
	assignPubKey(&assignment.ReceiverViewPubKey, *viewPubKey)
	assignment.Randomness = selectedNote.Randomness

	msgHash := privacycrypto.MimcHash(selectedNote.Amount, selectedNote.AssetID, selectedNote.Randomness, recipientInt)
	sigBytes, err := signer.SignSpendNoteHash(msgHash)
	if err != nil {
		return nil, err
	}
	assignSignature(&assignment.Signature, sigBytes)

	nullifierBig, ok := assignment.Nullifier.(*big.Int)
	if !ok {
		return nil, fmt.Errorf("invalid nullifier type in the spend assignment")
	}
	nullifierBytes, err := privacyfield.CanonicalBytesFromBigInt(nullifierBig)
	if err != nil {
		return nil, fmt.Errorf("invalid nullifier: %w", err)
	}

	return &PreparedSpendWithdraw{
		Assignment:     assignment,
		RootBytes:      append([]byte(nil), merklePath.Root...),
		NullifierBytes: nullifierBytes,
		MerklePath:     append([]string(nil), merklePath.Path...),
		PathHelper:     append([]uint32(nil), merklePath.PathHelper...),
		Signature:      append([]byte(nil), sigBytes...),
	}, nil
}

func decodeMerkleProof(path []string, pathHelper []uint32) ([circuit.MerkleDepth]*big.Int, [circuit.MerkleDepth]int) {
	var nodes [circuit.MerkleDepth]*big.Int
	var helpers [circuit.MerkleDepth]int

	for i := 0; i < circuit.MerkleDepth; i++ {
		if i < len(path) {
			pathBytes, _ := hex.DecodeString(path[i])
			nodes[i] = new(big.Int).SetBytes(pathBytes)
		} else {
			nodes[i] = big.NewInt(0)
		}

		if i < len(pathHelper) {
			helpers[i] = int(pathHelper[i])
		}
	}

	return nodes, helpers
}

func spendPubKeyFromNote(note privacytypes.Note) (*crypto_tedwards.PointAffine, error) {
	if note.ReceiverSpendPubKeyX == nil || note.ReceiverSpendPubKeyY == nil {
		return nil, fmt.Errorf("receiver spend key coordinates must not be nil in the note")
	}

	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(note.ReceiverSpendPubKeyX)
	point.Y.SetBigInt(note.ReceiverSpendPubKeyY)
	return &point, nil
}

func viewPubKeyFromNote(note privacytypes.Note) (*crypto_tedwards.PointAffine, error) {
	if note.ReceiverViewPubKeyX == nil || note.ReceiverViewPubKeyY == nil {
		return nil, fmt.Errorf("receiver view key coordinates must not be nil in the note")
	}

	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(note.ReceiverViewPubKeyX)
	point.Y.SetBigInt(note.ReceiverViewPubKeyY)
	return &point, nil
}

func assignSignature(target *eddsa.Signature, sigBytes []byte) {
	rBytes := sigBytes[:32]
	sBytes := sigBytes[32:]

	var pointR crypto_tedwards.PointAffine
	pointR.SetBytes(rBytes)

	rx, ry := new(big.Int), new(big.Int)
	pointR.X.BigInt(rx)
	pointR.Y.BigInt(ry)

	target.R.X = rx
	target.R.Y = ry
	target.S = new(big.Int).SetBytes(sBytes)
}

func assignPubKey(target *eddsa.PublicKey, source crypto_tedwards.PointAffine) {
	ax, ay := pointBigInts(source)
	target.A.X = ax
	target.A.Y = ay
}

func pointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
}
