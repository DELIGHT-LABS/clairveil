package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func TestNoteV1GoldenValues(t *testing.T) {
	spend := noteV1TestPoint(big.NewInt(17))
	view := noteV1TestPoint(big.NewInt(19))
	spendX, spendY := noteV1TestPointCoordinates(spend)
	viewX, viewY := noteV1TestPointCoordinates(view)
	note := &Note{
		ReceiverSpendPubKeyX: spendX,
		ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX:  viewX,
		ReceiverViewPubKeyY:  viewY,
		Amount:               big.NewInt(7),
		AssetID:              ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(13),
	}

	require.NoError(t, note.ValidateV1())
	require.Equal(t, "023aab554dcb995210888fa4e28c3d718568c1de0623578c690a2b6ca9d3610a", fixedFieldHex(note.ComputeCommitment()))
	require.Equal(t, "13b50fceae57ce77eee3f686abc1563aadc27ff6d1e32ce2fcc599463d28585b", fixedFieldHex(note.ComputeNullifier()))
	require.Equal(t, "238d5f23e4d918d40b0982ce3aef16a75c4d1760193d1c3b30b9f5df681903ca", fixedFieldHex(note.AssetID))
}

func TestDomainFieldV1ExactDerivationAndGoldenValues(t *testing.T) {
	tests := []struct {
		label string
		hex   string
	}{
		{NoteCommitmentV1FieldDomain, "0927abf70e775c0f9fd7db79a93b7f8e94621f15921f6b7077407ec5210cfb1c"},
		{NoteNullifierV1FieldDomain, "1a49a4bf6a216ef5dba9311200be7b1374794ba1ca759a7761e11ac6d774e0b9"},
		{NoteTreeNodeV1FieldDomain, "0e7215b6529f83eaf86ae8e5ad92eb2ec9f61f1dbd7077c54ff0fdd0e7bfd620"},
	}

	seen := make(map[string]struct{}, len(tests))
	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			expected := referenceDomainFieldV1(tt.label)
			actual := DomainFieldV1(tt.label)
			require.Zero(t, expected.Cmp(actual))
			require.Equal(t, tt.hex, fixedFieldHex(actual))
			if _, duplicate := seen[tt.hex]; duplicate {
				t.Fatalf("domain constant reused: %s", tt.hex)
			}
			seen[tt.hex] = struct{}{}
		})
	}
}

func TestEmptyNoteTreeV1ExactContract(t *testing.T) {
	roots := EmptyNoteTreeRootsV1(32)
	require.Len(t, roots, 33)
	require.Zero(t, roots[0].Sign())
	for level := uint32(0); level < 32; level++ {
		expected := ComputeNoteTreeNodeV1(level, roots[level], roots[level])
		require.Zero(t, expected.Cmp(roots[level+1]), "empty root mismatch at level %d", level+1)
	}
	require.Equal(t, "2a9932954f9328683b24310f96581603f12544f6da3910aeefebbfa84789b296", fixedFieldHex(roots[1]))
	require.Equal(t, "29bae378ecc69a3c6e1c861407bd57c9c8cd34d37ebc2d4fe8c205952f62793a", fixedFieldHex(roots[2]))
	require.Equal(t, "057551a52590c07629bf07fa2b61832f852fb69ff8472bb21c30e5675ae8e8c1", fixedFieldHex(roots[32]))
}

func TestNoteNullifierV1BindsCommitment(t *testing.T) {
	spend := noteV1TestPoint(big.NewInt(17))
	view := noteV1TestPoint(big.NewInt(19))
	spendX, spendY := noteV1TestPointCoordinates(spend)
	viewX, viewY := noteV1TestPointCoordinates(view)
	base := Note{
		ReceiverSpendPubKeyX: spendX,
		ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX:  viewX,
		ReceiverViewPubKeyY:  viewY,
		Amount:               big.NewInt(7),
		AssetID:              ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(13),
	}
	changed := base
	changed.Amount = big.NewInt(8)

	require.NotZero(t, base.ComputeCommitment().Cmp(changed.ComputeCommitment()))
	require.NotZero(t, base.ComputeNullifier().Cmp(changed.ComputeNullifier()))
}

func TestNoteV1RejectsInvalidKeysAndNonCanonicalFields(t *testing.T) {
	spend := noteV1TestPoint(big.NewInt(17))
	view := noteV1TestPoint(big.NewInt(19))
	spendX, spendY := noteV1TestPointCoordinates(spend)
	viewX, viewY := noteV1TestPointCoordinates(view)
	valid := Note{
		ReceiverSpendPubKeyX: spendX,
		ReceiverSpendPubKeyY: spendY,
		ReceiverViewPubKeyX:  viewX,
		ReceiverViewPubKeyY:  viewY,
		Amount:               big.NewInt(7),
		AssetID:              ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(13),
	}
	require.NoError(t, valid.ValidateV1())

	identity := valid
	identity.ReceiverSpendPubKeyX = big.NewInt(0)
	identity.ReceiverSpendPubKeyY = big.NewInt(1)
	require.ErrorContains(t, identity.ValidateV1(), "identity point is not allowed")

	orderTwo := valid
	orderTwo.ReceiverSpendPubKeyX = big.NewInt(0)
	orderTwo.ReceiverSpendPubKeyY = new(big.Int).Sub(fr.Modulus(), big.NewInt(1))
	require.ErrorContains(t, orderTwo.ValidateV1(), "prime-order subgroup")

	nonCanonical := valid
	nonCanonical.ReceiverViewPubKeyX = new(big.Int).Set(fr.Modulus())
	require.ErrorContains(t, nonCanonical.ValidateV1(), "canonical BN254 field element")
}

func TestNewNoteV1ValidatesDenomAndKeys(t *testing.T) {
	spend := noteV1TestPoint(big.NewInt(17))
	view := noteV1TestPoint(big.NewInt(19))
	spendX, spendY := noteV1TestPointCoordinates(spend)
	viewX, viewY := noteV1TestPointCoordinates(view)

	note, err := NewNote(spendX, spendY, viewX, viewY, big.NewInt(7), "uclair", "memo")
	require.NoError(t, err)
	require.NoError(t, note.ValidateV1())
	require.Zero(t, note.AssetID.Cmp(ComputeAssetIDV1("uclair")))

	_, err = NewNote(big.NewInt(0), big.NewInt(1), viewX, viewY, big.NewInt(7), "uclair", "memo")
	require.ErrorContains(t, err, "identity point is not allowed")
	_, err = NewNote(spendX, spendY, viewX, viewY, big.NewInt(7), "", "memo")
	require.ErrorContains(t, err, "invalid canonical asset denom")
}

func noteV1TestPoint(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, scalar)
	return point
}

func noteV1TestPointCoordinates(point crypto_tedwards.PointAffine) (*big.Int, *big.Int) {
	x := point.X.BigInt(new(big.Int))
	y := point.Y.BigInt(new(big.Int))
	return x, y
}

func fixedFieldHex(value *big.Int) string {
	encoded := make([]byte, fr.Bytes)
	value.FillBytes(encoded)
	return hex.EncodeToString(encoded)
}

func referenceDomainFieldV1(label string) *big.Int {
	encoded := make([]byte, 0, len(FieldDomainV1ByteDomain)+4+len(label))
	encoded = append(encoded, []byte(FieldDomainV1ByteDomain)...)
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(label)))
	encoded = append(encoded, length[:]...)
	encoded = append(encoded, []byte(label)...)
	digest := sha256.Sum256(encoded)
	result := new(big.Int).SetBytes(digest[:])
	return result.Mod(result, fr.Modulus())
}
