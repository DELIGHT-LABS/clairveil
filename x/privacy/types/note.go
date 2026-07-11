package types

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"unicode/utf8"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

type Note struct {
	ReceiverSpendPubKeyX *big.Int `json:"rsx"`
	ReceiverSpendPubKeyY *big.Int `json:"rsy"`
	ReceiverViewPubKeyX  *big.Int `json:"rvx"`
	ReceiverViewPubKeyY  *big.Int `json:"rvy"`
	Amount               *big.Int `json:"am"`
	AssetID              *big.Int `json:"as"`
	Randomness           *big.Int `json:"rn"`
	Memo                 string   `json:"mm"`
}

func NewNote(
	spendPubKeyX, spendPubKeyY *big.Int,
	viewPubKeyX, viewPubKeyY *big.Int,
	amount *big.Int,
	assetDenom, memo string,
) (*Note, error) {
	if err := ValidateShieldedAmount("note amount", amount); err != nil {
		return nil, err
	}
	if _, err := pointFromBigInts(spendPubKeyX, spendPubKeyY); err != nil {
		return nil, fmt.Errorf("invalid receiver spend public key: %w", err)
	}
	if _, err := pointFromBigInts(viewPubKeyX, viewPubKeyY); err != nil {
		return nil, fmt.Errorf("invalid receiver view public key: %w", err)
	}
	if err := sdk.ValidateDenom(assetDenom); err != nil {
		return nil, fmt.Errorf("invalid canonical asset denom: %w", err)
	}
	if err := validateNoteMemoV1(memo); err != nil {
		return nil, err
	}

	max := new(big.Int).Set(fr.Modulus())
	for {
		randomness, err := rand.Int(rand.Reader, max)
		if err != nil {
			return nil, err
		}

		note := &Note{
			ReceiverSpendPubKeyX: new(big.Int).Set(spendPubKeyX),
			ReceiverSpendPubKeyY: new(big.Int).Set(spendPubKeyY),
			ReceiverViewPubKeyX:  new(big.Int).Set(viewPubKeyX),
			ReceiverViewPubKeyY:  new(big.Int).Set(viewPubKeyY),
			Amount:               new(big.Int).Set(amount),
			AssetID:              ComputeAssetIDV1(assetDenom),
			Randomness:           randomness,
			Memo:                 memo,
		}
		if note.ComputeCommitment().Sign() != 0 && note.ComputeNullifier().Sign() != 0 {
			return note, nil
		}
	}
}

func (n *Note) ComputeCommitment() *big.Int {
	return ComputeNoteCommitmentV1(
		n.ReceiverSpendPubKeyX,
		n.ReceiverSpendPubKeyY,
		n.ReceiverViewPubKeyX,
		n.ReceiverViewPubKeyY,
		n.Amount,
		n.AssetID,
		n.Randomness,
	)
}

func (n *Note) ComputeNullifier() *big.Int {
	return ComputeNoteNullifierV1(
		n.ComputeCommitment(),
		n.Randomness,
		n.ReceiverSpendPubKeyX,
		n.ReceiverSpendPubKeyY,
	)
}

// ValidateV1 rejects malformed decrypted or externally constructed NoteV1
// values before they are used to derive wallet state or a proving witness.
func (n *Note) ValidateV1() error {
	if n == nil {
		return fmt.Errorf("note is required")
	}
	if err := ValidateShieldedAmount("note amount", n.Amount); err != nil {
		return err
	}
	for _, field := range []struct {
		name  string
		value *big.Int
	}{
		{"receiver spend public key x", n.ReceiverSpendPubKeyX},
		{"receiver spend public key y", n.ReceiverSpendPubKeyY},
		{"receiver view public key x", n.ReceiverViewPubKeyX},
		{"receiver view public key y", n.ReceiverViewPubKeyY},
		{"asset id", n.AssetID},
		{"randomness", n.Randomness},
	} {
		if err := validateCanonicalNoteField(field.name, field.value); err != nil {
			return err
		}
	}
	if _, err := pointFromBigInts(n.ReceiverSpendPubKeyX, n.ReceiverSpendPubKeyY); err != nil {
		return fmt.Errorf("invalid receiver spend public key: %w", err)
	}
	if _, err := pointFromBigInts(n.ReceiverViewPubKeyX, n.ReceiverViewPubKeyY); err != nil {
		return fmt.Errorf("invalid receiver view public key: %w", err)
	}
	if err := validateNoteMemoV1(n.Memo); err != nil {
		return err
	}
	if n.ComputeCommitment().Sign() == 0 {
		return fmt.Errorf("active note commitment must be non-zero")
	}
	if n.ComputeNullifier().Sign() == 0 {
		return fmt.Errorf("active note nullifier must be non-zero")
	}
	return nil
}

func validateNoteMemoV1(memo string) error {
	if !utf8.ValidString(memo) {
		return fmt.Errorf("note memo must be valid UTF-8")
	}
	if len([]byte(memo)) > NoteMemoCapacityV1 {
		return fmt.Errorf("note memo exceeds fixed capacity %d", NoteMemoCapacityV1)
	}
	return nil
}

func (n *Note) ReceiverShieldedAddress() (string, error) {
	spendPubKey, err := pointFromBigInts(n.ReceiverSpendPubKeyX, n.ReceiverSpendPubKeyY)
	if err != nil {
		return "", err
	}

	viewPubKey, err := pointFromBigInts(n.ReceiverViewPubKeyX, n.ReceiverViewPubKeyY)
	if err != nil {
		return "", err
	}

	return EncodeShieldedAddressWithView(spendPubKey, viewPubKey)
}

func pointFromBigInts(x, y *big.Int) (*crypto_tedwards.PointAffine, error) {
	if x == nil || y == nil {
		return nil, fmt.Errorf("shielded address coordinates must not be nil")
	}
	if err := validateCanonicalNoteField("shielded address x coordinate", x); err != nil {
		return nil, err
	}
	if err := validateCanonicalNoteField("shielded address y coordinate", y); err != nil {
		return nil, err
	}

	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(x)
	point.Y.SetBigInt(y)
	if err := crypto.ValidatePrimeSubgroupPoint(&point); err != nil {
		return nil, err
	}
	return &point, nil
}
