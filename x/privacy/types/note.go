package types

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

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

	max := new(big.Int).Set(fr.Modulus())
	randomness, err := rand.Int(rand.Reader, max)
	if err != nil {
		return nil, err
	}

	return &Note{
		ReceiverSpendPubKeyX: spendPubKeyX,
		ReceiverSpendPubKeyY: spendPubKeyY,
		ReceiverViewPubKeyX:  viewPubKeyX,
		ReceiverViewPubKeyY:  viewPubKeyY,
		Amount:               amount,
		AssetID:              crypto.HashString(assetDenom),
		Randomness:           randomness,
		Memo:                 memo,
	}, nil
}

func (n *Note) ComputeCommitment() *big.Int {
	return crypto.MimcHash(
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
	return crypto.MimcHash(
		n.Randomness,
		n.ReceiverSpendPubKeyX,
		n.ReceiverSpendPubKeyY,
	)
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

	var point crypto_tedwards.PointAffine
	point.X.SetBigInt(x)
	point.Y.SetBigInt(y)
	return &point, nil
}

func (n *Note) Bytes() []byte {
	b, _ := json.Marshal(n)
	return b
}
