package types

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	fr_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	"github.com/stretchr/testify/require"
)

func TestBatchVectorRootV1IndependentGolden(t *testing.T) {
	values := zeroBigIntsForBatchTest(int(BatchJoinSplitV1MaxInputs))
	values[0] = big.NewInt(11)
	values[1] = big.NewInt(13)
	values[2] = big.NewInt(17)

	got, err := ComputeBatchVectorRootV1(BatchVectorNullifierV1, 3, values)
	require.NoError(t, err)
	want := referenceBatchVectorRoot(t, BatchVectorNullifierV1, 3, values)
	require.Equal(t, want.String(), got.String())

	const goldenDecimal = "2861110296864134449398760699372039234775602744709528659771805202586726032787"
	require.Equal(t, goldenDecimal, got.String())

	reordered := append([]*big.Int(nil), values...)
	reordered[0], reordered[1] = reordered[1], reordered[0]
	reorderedRoot, err := ComputeBatchVectorRootV1(BatchVectorNullifierV1, 3, reordered)
	require.NoError(t, err)
	require.NotEqual(t, got.String(), reorderedRoot.String())

	countChanged := append([]*big.Int(nil), values...)
	countChanged[3] = big.NewInt(19)
	countChangedRoot, err := ComputeBatchVectorRootV1(BatchVectorNullifierV1, 4, countChanged)
	require.NoError(t, err)
	require.NotEqual(t, got.String(), countChangedRoot.String())
}

func TestBatchVectorRootV1RejectsNonCanonicalDisabledSlots(t *testing.T) {
	values := zeroBigIntsForBatchTest(int(BatchJoinSplitV1MaxOutputs))
	values[0] = big.NewInt(11)
	values[1] = big.NewInt(13)
	_, err := ComputeBatchVectorRootV1(BatchVectorCommitmentV1, 1, values)
	require.ErrorContains(t, err, "disabled value 1 must be zero")

	values[1] = big.NewInt(0)
	values[0] = big.NewInt(0)
	_, err = ComputeBatchVectorRootV1(BatchVectorCommitmentV1, 1, values)
	require.ErrorContains(t, err, "active value 0 must be non-zero")

	_, err = ComputeBatchVectorRootV1(BatchVectorCommitmentV1, 0, values)
	require.ErrorContains(t, err, "count must be in")
}

func TestBatchUserDisclosureVectorRootV1IndependentGolden(t *testing.T) {
	policies := make([]uint32, BatchJoinSplitV1MaxOutputs)
	rawDigests := zeroBigIntsForBatchTest(int(BatchJoinSplitV1MaxOutputs))
	policies[0] = TransferPrivacyPolicyAllPrivate
	policies[1] = TransferPrivacyPolicyDiscloseAmount
	rawDigests[1] = big.NewInt(101)

	got, err := ComputeBatchUserDisclosureVectorRootV1(2, policies, rawDigests)
	require.NoError(t, err)
	values := zeroBigIntsForBatchTest(int(BatchJoinSplitV1MaxOutputs))
	values[0] = referenceMIMC(referenceDomainField(BatchUserDisclosureLeafV1DomainLabel), big.NewInt(0), big.NewInt(1), big.NewInt(0), big.NewInt(0))
	values[1] = referenceMIMC(referenceDomainField(BatchUserDisclosureLeafV1DomainLabel), big.NewInt(1), big.NewInt(1), big.NewInt(1), big.NewInt(101))
	want := referenceBatchVectorRoot(t, BatchVectorUserDisclosureV1, 2, values)
	require.Equal(t, want.String(), got.String())
	const goldenHex = "0e634ae9dde1d8e9cb01813dcb81d17307cafe89a9f6aec4f37c82183e4ed368"
	require.Equal(t, goldenHex, hex.EncodeToString(got.FillBytes(make([]byte, 32))))

	policies[2] = TransferPrivacyPolicyDiscloseTo
	_, err = ComputeBatchUserDisclosureVectorRootV1(2, policies, rawDigests)
	require.ErrorContains(t, err, "disabled user disclosure output")
}

func TestBatchDisclosureV1BlindingPreventsDictionaryMatch(t *testing.T) {
	fromSpendX, fromSpendY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(17)))
	fromViewX, fromViewY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(19)))
	toSpendX, toSpendY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(23)))
	toViewX, toViewY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(29)))
	assetID := ComputeAssetIDV1("uclair")
	base := BatchUserDisclosureV1Input{
		OutputIndex: 3, Commitment: big.NewInt(101),
		Policy:                TransferPrivacyPolicyDiscloseAmountToFrom,
		DisclosedFieldBitmap:  TransferPrivacyPolicyDiscloseAmountToFrom,
		SelectedAmount:        big.NewInt(7),
		SelectedFromSpendKeyX: fromSpendX, SelectedFromSpendKeyY: fromSpendY,
		SelectedFromViewKeyX: fromViewX, SelectedFromViewKeyY: fromViewY,
		SelectedToSpendKeyX: toSpendX, SelectedToSpendKeyY: toSpendY,
		SelectedToViewKeyX: toViewX, SelectedToViewKeyY: toViewY,
		AssetID: assetID, UserDisclosureBlinding: big.NewInt(43),
	}
	first, err := ComputeBatchUserDisclosureDigestV1(base)
	require.NoError(t, err)
	base.UserDisclosureBlinding = big.NewInt(47)
	second, err := ComputeBatchUserDisclosureDigestV1(base)
	require.NoError(t, err)
	require.NotEqual(t, first.String(), second.String())

	attackerGuess := referenceMIMC(
		referenceDomainField(BatchUserDisclosureV2DomainLabel),
		big.NewInt(3), big.NewInt(101), big.NewInt(7), big.NewInt(7),
		big.NewInt(7), fromSpendX, fromSpendY, fromViewX, fromViewY,
		toSpendX, toSpendY, toViewX, toViewY, assetID,
		big.NewInt(0),
	)
	require.NotEqual(t, first.String(), attackerGuess.String())
	require.NotEqual(t, second.String(), attackerGuess.String())
}

func TestBatchUserDisclosureV1RejectsNonZeroUnselectedFields(t *testing.T) {
	spendX, spendY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(17)))
	viewX, viewY := noteV1TestPointCoordinates(noteV1TestPoint(big.NewInt(19)))
	zero := func() *big.Int { return new(big.Int) }
	input := BatchUserDisclosureV1Input{
		OutputIndex: 0, Commitment: big.NewInt(101),
		Policy: TransferPrivacyPolicyDiscloseAmount, DisclosedFieldBitmap: TransferPrivacyPolicyDiscloseAmount,
		SelectedAmount: big.NewInt(7), AssetID: ComputeAssetIDV1("uclair"),
		SelectedFromSpendKeyX: spendX, SelectedFromSpendKeyY: spendY,
		SelectedFromViewKeyX: viewX, SelectedFromViewKeyY: viewY,
		SelectedToSpendKeyX: zero(), SelectedToSpendKeyY: zero(),
		SelectedToViewKeyX: zero(), SelectedToViewKeyY: zero(),
		UserDisclosureBlinding: big.NewInt(43),
	}
	_, err := ComputeBatchUserDisclosureDigestV1(input)
	require.ErrorContains(t, err, "undisclosed sender keys")

	input.SelectedFromSpendKeyX, input.SelectedFromSpendKeyY = zero(), zero()
	input.SelectedFromViewKeyX, input.SelectedFromViewKeyY = zero(), zero()
	_, err = ComputeBatchUserDisclosureDigestV1(input)
	require.NoError(t, err)

	input.Policy = TransferPrivacyPolicyDiscloseTo
	input.DisclosedFieldBitmap = TransferPrivacyPolicyDiscloseTo
	input.SelectedAmount = big.NewInt(7)
	_, err = ComputeBatchUserDisclosureDigestV1(input)
	require.ErrorContains(t, err, "undisclosed amount")
}

func TestBatchEffectIDV1IndependentGolden(t *testing.T) {
	input := BatchEffectIDV1Input{
		ChainDomainHi: big.NewInt(1), ChainDomainLo: big.NewInt(2), MerkleRoot: big.NewInt(3),
		InputCount: 3, OutputCount: 4,
		NullifierRoot: big.NewInt(5), CommitmentRoot: big.NewInt(6),
		UserDisclosureRoot: big.NewInt(7), FullDisclosureRoot: big.NewInt(8),
		PayloadDigestHi: big.NewInt(9), PayloadDigestLo: big.NewInt(10),
		ExpiresAtUnix: 2_000_000_000,
	}
	got, err := ComputeBatchEffectIDV1(input)
	require.NoError(t, err)
	want := referenceBatchEffectID(t, input)
	require.Equal(t, want, got)

	const goldenHex = "7f76d7744607e06dc0a22e4be5464e1a420c933cff5d060cc657ccfd4ec45979"
	require.Equal(t, goldenHex, hex.EncodeToString(got[:]))
}

func referenceBatchVectorRoot(t *testing.T, kind BatchVectorKindV1, count uint32, values []*big.Int) *big.Int {
	t.Helper()
	capacity, err := kind.Capacity()
	require.NoError(t, err)
	layer := make([]*big.Int, capacity)
	for i := uint32(0); i < capacity; i++ {
		enabled := big.NewInt(0)
		if i < count {
			enabled = big.NewInt(1)
		}
		layer[i] = referenceMIMC(
			referenceDomainField("clairveil.batch-vector."+string(kind)+".leaf.v1"),
			new(big.Int).SetUint64(uint64(i)), enabled, values[i],
		)
	}
	for level := uint32(0); len(layer) > 1; level++ {
		next := make([]*big.Int, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = referenceMIMC(
				referenceDomainField("clairveil.batch-vector."+string(kind)+".node.v1"),
				new(big.Int).SetUint64(uint64(level)), layer[i], layer[i+1],
			)
		}
		layer = next
	}
	return referenceMIMC(
		referenceDomainField("clairveil.batch-vector."+string(kind)+".root.v1"),
		new(big.Int).SetUint64(uint64(capacity)), new(big.Int).SetUint64(uint64(count)), layer[0],
	)
}

func referenceDomainField(label string) *big.Int {
	h := sha256.New()
	_, _ = h.Write([]byte("clairveil.field-domain.v1"))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(label)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(label))
	return new(big.Int).Mod(new(big.Int).SetBytes(h.Sum(nil)), fr.Modulus())
}

func referenceMIMC(values ...*big.Int) *big.Int {
	h := fr_mimc.NewMiMC()
	for _, value := range values {
		var element fr.Element
		element.SetBigInt(value)
		encoded := element.Bytes()
		_, _ = h.Write(encoded[:])
	}
	return new(big.Int).SetBytes(h.Sum(nil))
}

func referenceBatchEffectID(t *testing.T, input BatchEffectIDV1Input) [sha256.Size]byte {
	t.Helper()
	h := sha256.New()
	_, _ = h.Write([]byte("clairveil.batch-effect.v1"))
	writeField := func(value *big.Int) { _, _ = h.Write(value.FillBytes(make([]byte, 32))) }
	writeField(input.ChainDomainHi)
	writeField(input.ChainDomainLo)
	writeField(input.MerkleRoot)
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], input.InputCount)
	_, _ = h.Write(count[:])
	binary.BigEndian.PutUint32(count[:], input.OutputCount)
	_, _ = h.Write(count[:])
	for _, value := range []*big.Int{
		input.NullifierRoot, input.CommitmentRoot, input.UserDisclosureRoot,
		input.FullDisclosureRoot, input.PayloadDigestHi, input.PayloadDigestLo,
	} {
		writeField(value)
	}
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(input.ExpiresAtUnix))
	_, _ = h.Write(expiry[:])
	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result
}

func zeroBigIntsForBatchTest(size int) []*big.Int {
	values := make([]*big.Int, size)
	for i := range values {
		values[i] = big.NewInt(0)
	}
	return values
}
