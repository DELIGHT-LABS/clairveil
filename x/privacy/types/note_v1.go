package types

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const (
	// FieldDomainV1ByteDomain is the byte-level prefix used to derive every
	// NoteV1 field-domain constant. Labels are length-prefixed so concatenated
	// inputs cannot alias one another.
	FieldDomainV1ByteDomain = "clairveil.field-domain.v1"
	AssetIDV1ByteDomain     = "clairveil.asset-id.v1"

	NoteCommitmentV1FieldDomain = "clairveil.note-commitment.v1"
	NoteNullifierV1FieldDomain  = "clairveil.note-nullifier.v1"
	NoteTreeNodeV1FieldDomain   = "clairveil.note-tree-node.v1"
)

// DomainFieldV1 derives a BN254 scalar-field domain constant as
// SHA-256("clairveil.field-domain.v1" || u32be(len(label)) || label) mod Fr.
// Protocol labels are fixed, short ASCII strings and therefore always fit in
// the required u32 length prefix.
func DomainFieldV1(label string) *big.Int {
	h := sha256.New()
	_, _ = h.Write([]byte(FieldDomainV1ByteDomain))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(label)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(label))

	result := new(big.Int).SetBytes(h.Sum(nil))
	return result.Mod(result, fr.Modulus())
}

// ComputeAssetIDV1 maps a canonical Cosmos denom to the single field element
// used by NoteV1:
//
//	SHA-256("clairveil.asset-id.v1" || u32be(len(denom)) || denom) mod Fr.
//
// Callers at user or consensus boundaries must validate that canonicalDenom
// satisfies the existing Cosmos denom contract before calling this helper.
func ComputeAssetIDV1(canonicalDenom string) *big.Int {
	h := sha256.New()
	_, _ = h.Write([]byte(AssetIDV1ByteDomain))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(canonicalDenom)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(canonicalDenom))

	result := new(big.Int).SetBytes(h.Sum(nil))
	return result.Mod(result, fr.Modulus())
}

func ComputeNoteCommitmentV1(
	spendPubKeyX, spendPubKeyY *big.Int,
	viewPubKeyX, viewPubKeyY *big.Int,
	amount, assetID, randomness *big.Int,
) *big.Int {
	return privacycrypto.MimcHash(
		DomainFieldV1(NoteCommitmentV1FieldDomain),
		spendPubKeyX,
		spendPubKeyY,
		viewPubKeyX,
		viewPubKeyY,
		amount,
		assetID,
		randomness,
	)
}

func ComputeNoteNullifierV1(
	commitment, randomness, spendPubKeyX, spendPubKeyY *big.Int,
) *big.Int {
	return privacycrypto.MimcHash(
		DomainFieldV1(NoteNullifierV1FieldDomain),
		commitment,
		randomness,
		spendPubKeyX,
		spendPubKeyY,
	)
}

func ComputeNoteTreeNodeV1(level uint32, left, right *big.Int) *big.Int {
	return privacycrypto.MimcHash(
		DomainFieldV1(NoteTreeNodeV1FieldDomain),
		new(big.Int).SetUint64(uint64(level)),
		left,
		right,
	)
}

// EmptyNoteTreeRootsV1 returns empty[0..depth], where empty[0] is the literal
// zero leaf and every higher entry is the exact level-separated NoteV1 node.
func EmptyNoteTreeRootsV1(depth uint32) []*big.Int {
	roots := make([]*big.Int, depth+1)
	roots[0] = new(big.Int)
	for level := uint32(0); level < depth; level++ {
		roots[level+1] = ComputeNoteTreeNodeV1(level, roots[level], roots[level])
	}
	return roots
}

func EmptyNoteTreeRootV1(depth uint32) *big.Int {
	return EmptyNoteTreeRootsV1(depth)[depth]
}

func validateCanonicalNoteField(name string, value *big.Int) error {
	if value == nil {
		return fmt.Errorf("%s is required", name)
	}
	if value.Sign() < 0 || value.Cmp(fr.Modulus()) >= 0 {
		return fmt.Errorf("%s must be a canonical BN254 field element", name)
	}
	return nil
}
