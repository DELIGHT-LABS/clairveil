package transfer

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"math/bits"

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

type PrepareJoinSplitInput struct {
	Inputs               [2]privacyscan.SecretFoundNote
	RecipientSpendPubKey *crypto_tedwards.PointAffine
	RecipientViewPubKey  *crypto_tedwards.PointAffine
	TransferAmount       *big.Int
	SenderSpendPubKey    *crypto_tedwards.PointAffine
	SenderViewPubKey     *crypto_tedwards.PointAffine
}

type PreparedJoinSplitTransfer struct {
	Assignment        circuit.JoinSplitCircuit
	CommonRoot        []byte
	InputNullifiers   [][]byte
	InputMerklePaths  [][]string
	InputPathHelpers  [][]uint32
	OutputCommitments [][]byte
	FromNote          privacytypes.SecretNoteV1
	RecipientNote     privacytypes.SecretNoteV1
	ChangeNote        privacytypes.SecretNoteV1
}

func PrepareJoinSplitTransfer(
	ctx context.Context,
	provider MerklePathProvider,
	input PrepareJoinSplitInput,
) (*PreparedJoinSplitTransfer, error) {
	if provider == nil {
		return nil, fmt.Errorf("a merkle path provider is required to prepare a joinsplit transfer")
	}
	if input.RecipientSpendPubKey == nil || input.RecipientViewPubKey == nil {
		return nil, fmt.Errorf("recipient spend/view public keys are required to prepare a joinsplit transfer")
	}
	if input.SenderSpendPubKey == nil || input.SenderViewPubKey == nil {
		return nil, fmt.Errorf("sender spend/view public keys are required to prepare a joinsplit transfer")
	}
	if input.TransferAmount == nil {
		return nil, fmt.Errorf("a transfer amount is required to prepare a joinsplit transfer")
	}
	if err := privacytypes.ValidateShieldedAmount("transfer amount", input.TransferAmount); err != nil {
		return nil, err
	}
	for i, found := range input.Inputs {
		if err := found.Note.ValidateV1(); err != nil {
			return nil, fmt.Errorf("invalid input NoteV1 %d: %w", i, err)
		}
	}
	if err := validateCommonInputOwnerAndAsset(input.Inputs); err != nil {
		return nil, err
	}
	inputNullifierCandidates := make([][]byte, 2)
	for i, found := range input.Inputs {
		n, err := found.Note.NullifierV1()
		if err != nil {
			return nil, err
		}
		raw := n.Bytes()
		inputNullifierCandidates[i] = append([]byte(nil), raw[:]...)
	}
	if err := privacytypes.ValidateDistinctCanonicalFieldElements("input nullifier", inputNullifierCandidates); err != nil {
		return nil, fmt.Errorf("joinsplit inputs must be distinct: %w", err)
	}
	total, carry := bits.Add64(input.Inputs[0].Note.Amount, input.Inputs[1].Note.Amount, 0)
	changeAmount, borrow := bits.Sub64(total, input.TransferAmount.Uint64(), 0)
	if carry < borrow {
		return nil, fmt.Errorf("transfer amount exceeds selected input total")
	}
	if carry > borrow {
		return nil, fmt.Errorf("change amount exceeds 64-bit shielded amount limit")
	}

	recipientSpendX, recipientSpendY, err := privacycrypto.PublicPointFieldValues(*input.RecipientSpendPubKey)
	if err != nil {
		return nil, fmt.Errorf("recipient spend key: %w", err)
	}
	recipientViewX, recipientViewY, err := privacycrypto.PublicPointFieldValues(*input.RecipientViewPubKey)
	if err != nil {
		return nil, fmt.Errorf("recipient view key: %w", err)
	}
	senderSpendX, senderSpendY, err := privacycrypto.PublicPointFieldValues(*input.SenderSpendPubKey)
	if err != nil {
		return nil, fmt.Errorf("sender spend key: %w", err)
	}
	senderViewX, senderViewY, err := privacycrypto.PublicPointFieldValues(*input.SenderViewPubKey)
	if err != nil {
		return nil, fmt.Errorf("sender view key: %w", err)
	}
	asset := input.Inputs[0].Note.AssetID
	secretOutputs, err := PrepareSecretJoinSplitOutputsForAsset(rand.Reader, asset, input.TransferAmount.Uint64(), changeAmount, recipientSpendX, recipientSpendY, recipientViewX, recipientViewY, senderSpendX, senderSpendY, senderViewX, senderViewY)
	if err != nil {
		return nil, err
	}
	recipientNote := secretOutputs.RecipientNote.ToProverWitnessV1()
	changeNote := secretOutputs.ChangeNote.ToProverWitnessV1()

	var assignment circuit.JoinSplitCircuit
	inputNullifiers := make([][]byte, 2)
	inputMerklePaths := make([][]string, len(input.Inputs))
	inputPathHelpers := make([][]uint32, len(input.Inputs))
	assetRaw := asset.Bytes()
	assetID := new(big.Int).SetBytes(assetRaw[:])
	assignment.AssetID = assetID

	var commonRoot []byte

	for i := 0; i < len(input.Inputs); i++ {
		foundNote := input.Inputs[i]
		commitment, err := foundNote.Note.CommitmentV1()
		if err != nil {
			return nil, err
		}
		commitmentRaw := commitment.Bytes()
		commitmentHex := hex.EncodeToString(commitmentRaw[:])

		merklePath, err := provider.LookupMerklePath(ctx, commitmentHex)
		if err != nil {
			return nil, fmt.Errorf("failed to look up the merkle path for input note %d: %w", i, err)
		}
		if err := privacyfield.ValidateCanonicalBytes32(merklePath.Root); err != nil {
			return nil, fmt.Errorf("invalid merkle root for input note %d: %w", i, err)
		}

		if commonRoot == nil {
			commonRoot = append([]byte(nil), merklePath.Root...)
			assignment.MerkleRoot = new(big.Int).SetBytes(commonRoot)
		} else if !bytes.Equal(commonRoot, merklePath.Root) {
			return nil, fmt.Errorf("merkle root mismatch across input notes (wallet sync required)")
		}
		inputMerklePaths[i] = append([]string(nil), merklePath.Path...)
		inputPathHelpers[i] = append([]uint32(nil), merklePath.PathHelper...)
		if err := validateMerklePathHelperBits(merklePath.PathHelper); err != nil {
			return nil, fmt.Errorf("invalid merkle path helper for input note %d: %w", i, err)
		}

		pathNodes, pathHelpers := decodeMerkleProof(merklePath.Path, merklePath.PathHelper)
		for depth := 0; depth < circuit.MerkleDepth; depth++ {
			assignment.InputPaths[i][depth] = pathNodes[depth]
			assignment.InputPathHelpers[i][depth] = pathHelpers[depth]
		}

		witnessNote := foundNote.Note.ToProverWitnessV1()
		assignment.InputAmounts[i] = witnessNote.Amount
		assignment.InputRandomness[i] = witnessNote.Randomness

		spendPubKey, err := spendPubKeyFromNote(witnessNote)
		if err != nil {
			return nil, fmt.Errorf("invalid spend key for input note %d: %w", i, err)
		}
		assignPubKey(&assignment.InputSpendPubKeys[i], *spendPubKey)

		viewPubKey, err := viewPubKeyFromNote(witnessNote)
		if err != nil {
			return nil, fmt.Errorf("invalid view key for input note %d: %w", i, err)
		}
		assignPubKey(&assignment.InputViewPubKeys[i], *viewPubKey)

		assignment.Nullifiers[i] = new(big.Int).SetBytes(inputNullifierCandidates[i])
		inputNullifiers[i] = append([]byte(nil), inputNullifierCandidates[i]...)

	}

	recipientCommitmentBytes := secretOutputs.RecipientCommitment.Bytes()
	changeCommitmentBytes := secretOutputs.ChangeCommitment.Bytes()
	outputCommitments := [][]byte{append([]byte(nil), recipientCommitmentBytes[:]...), append([]byte(nil), changeCommitmentBytes[:]...)}
	if err := privacytypes.ValidateDistinctCanonicalFieldElements("output commitment", outputCommitments); err != nil {
		return nil, fmt.Errorf("joinsplit outputs must be distinct: %w", err)
	}

	assignment.OutputAmounts[0] = recipientNote.Amount
	assignment.OutputRandomness[0] = recipientNote.Randomness
	assignPubKey(&assignment.OutputSpendPubKeys[0], *input.RecipientSpendPubKey)
	assignPubKey(&assignment.OutputViewPubKeys[0], *input.RecipientViewPubKey)
	assignment.Commitments[0] = new(big.Int).SetBytes(recipientCommitmentBytes[:])

	assignment.OutputAmounts[1] = changeNote.Amount
	assignment.OutputRandomness[1] = changeNote.Randomness
	assignPubKey(&assignment.OutputSpendPubKeys[1], *input.SenderSpendPubKey)
	assignPubKey(&assignment.OutputViewPubKeys[1], *input.SenderViewPubKey)
	assignment.Commitments[1] = new(big.Int).SetBytes(changeCommitmentBytes[:])

	return &PreparedJoinSplitTransfer{
		Assignment:        assignment,
		CommonRoot:        commonRoot,
		InputNullifiers:   inputNullifiers,
		InputMerklePaths:  inputMerklePaths,
		InputPathHelpers:  inputPathHelpers,
		OutputCommitments: outputCommitments,
		FromNote:          input.Inputs[0].Note,
		RecipientNote:     secretOutputs.RecipientNote,
		ChangeNote:        secretOutputs.ChangeNote,
	}, nil
}

func validateCommonInputOwnerAndAsset(inputs [2]privacyscan.SecretFoundNote) error {
	left := inputs[0].Note
	right := inputs[1].Note
	for _, field := range []struct {
		name        string
		left, right privacycrypto.FieldValue
	}{
		{"spend public key x", left.ReceiverSpendPubKeyX, right.ReceiverSpendPubKeyX},
		{"spend public key y", left.ReceiverSpendPubKeyY, right.ReceiverSpendPubKeyY},
		{"view public key x", left.ReceiverViewPubKeyX, right.ReceiverViewPubKeyX},
		{"view public key y", left.ReceiverViewPubKeyY, right.ReceiverViewPubKeyY},
		{"asset id", left.AssetID, right.AssetID},
	} {
		if !field.left.Equal(field.right) {
			return fmt.Errorf("joinsplit inputs must share the same owner and asset: %s mismatch", field.name)
		}
	}
	return nil
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

func validateMerklePathHelperBits(pathHelper []uint32) error {
	for i, helper := range pathHelper {
		if helper != 0 && helper != 1 {
			return fmt.Errorf("path helper %d must be 0 or 1; got %d", i, helper)
		}
	}
	return nil
}

func pointBigInts(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := new(big.Int)
	y := new(big.Int)
	point.X.BigInt(x)
	point.Y.BigInt(y)
	return x, y
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

func assignSignature(target *eddsa.Signature, sigBytes []byte) error {
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(sigBytes); err != nil {
		return err
	}
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
	return nil
}

func assignPubKey(target *eddsa.PublicKey, source crypto_tedwards.PointAffine) {
	ax, ay := pointBigInts(source)
	target.A.X = ax
	target.A.Y = ay
}
