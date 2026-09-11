package batchtransfer

import (
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"math/big"
)

func fixedBytes(v privacycrypto.FieldValue) []byte {
	b := v.Bytes()
	return append([]byte(nil), b[:]...)
}

// fixedPublicBig is used only after a digest or public asset is declassified,
// or at the explicit circuit witness assignment boundary.
func fixedPublicBig(v privacycrypto.FieldValue) *big.Int { return new(big.Int).SetBytes(fixedBytes(v)) }
func fixedMatchesPublic(v privacycrypto.FieldValue, public *big.Int) bool {
	if public == nil || public.Sign() < 0 || public.BitLen() > 256 {
		return false
	}
	var raw [32]byte
	public.FillBytes(raw[:])
	return v.Bytes() == raw
}
func fixedPoint(x, y privacycrypto.FieldValue) (*crypto_tedwards.PointAffine, error) {
	xb, yb := x.Bytes(), y.Bytes()
	var p crypto_tedwards.PointAffine
	if err := p.X.SetBytesCanonical(xb[:]); err != nil {
		return nil, err
	}
	if err := p.Y.SetBytesCanonical(yb[:]); err != nil {
		return nil, err
	}
	if _, err := privacycrypto.SecretPointFromPublic(p); err != nil {
		return nil, err
	}
	return &p, nil
}
