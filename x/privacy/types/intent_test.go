package types

import (
	"math/big"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestComputeChainDomainV1GoldenVector(t *testing.T) {
	domain, err := ComputeChainDomainV1("clairveil-localnet-1", ActiveCircuitSetID)
	require.NoError(t, err)
	require.Equal(t, "264159934158684874548762591990728337770", domain.Hi.String())
	require.Equal(t, "270095241217876371844524170513424412119", domain.Lo.String())
}

func TestCanonicalTransferPayloadDigestV1GoldenVector(t *testing.T) {
	msg := intentTestTransferMessage()
	digest, err := ComputeTransferPayloadDigestV1(msg)
	require.NoError(t, err)
	require.Equal(t, "167934897245902538552295964807751055480", digest.Hi.String())
	require.Equal(t, "315400652074988150791302303081971100397", digest.Lo.String())

	mutated := *msg
	mutated.CipherTexts = cloneIntentByteSlice(msg.CipherTexts)
	mutated.CipherTexts[0][0] ^= 1
	mutatedDigest, err := ComputeTransferPayloadDigestV1(&mutated)
	require.NoError(t, err)
	require.NotEqual(t, digest, mutatedDigest)

	mutated.Creator = "clair1replacementrelayer"
	creatorDigest, err := ComputeTransferPayloadDigestV1(&mutated)
	require.NoError(t, err)
	require.Equal(t, mutatedDigest, creatorDigest)
}

func TestOrderedSetAndTransferIntentV2GoldenVectors(t *testing.T) {
	nullifiers := [2]*big.Int{big.NewInt(11), big.NewInt(13)}
	commitments := [2]*big.Int{big.NewInt(17), big.NewInt(19)}
	nullifierDigest, err := ComputeOrderedSetDigestV1(NullifierSetV1FieldDomain, nullifiers[:])
	require.NoError(t, err)
	commitmentDigest, err := ComputeOrderedSetDigestV1(CommitmentSetV1FieldDomain, commitments[:])
	require.NoError(t, err)
	require.Equal(t, "21725280499368482838609332040606601310768237399206317574233191071725724384804", nullifierDigest.String())
	require.Equal(t, "16660477204609256832501002803949326031473311872911201869454749511242800132471", commitmentDigest.String())

	reordered, err := ComputeOrderedSetDigestV1(NullifierSetV1FieldDomain, []*big.Int{nullifiers[1], nullifiers[0]})
	require.NoError(t, err)
	require.NotEqual(t, nullifierDigest, reordered)

	chainDomain, err := ComputeChainDomainV1("clairveil-localnet-1", ActiveCircuitSetID)
	require.NoError(t, err)
	payloadDigest, err := ComputeTransferPayloadDigestV1(intentTestTransferMessage())
	require.NoError(t, err)
	intent, err := ComputeTransferIntentV2(TransferIntentV2Input{
		ChainDomainHi:        chainDomain.Hi,
		ChainDomainLo:        chainDomain.Lo,
		MerkleRoot:           big.NewInt(23),
		AssetID:              big.NewInt(29),
		Nullifiers:           nullifiers,
		Commitments:          commitments,
		UserDisclosureDigest: big.NewInt(31),
		FullDisclosureDigest: big.NewInt(37),
		PayloadDigestHi:      payloadDigest.Hi,
		PayloadDigestLo:      payloadDigest.Lo,
		ExpiresAtUnix:        2_000_000_000,
	})
	require.NoError(t, err)
	require.Equal(t, "21502032591532004636179220502878670510289919326746051485096026281100511527942", intent.String())
}

func TestTransferIntentV2RejectsOversizedDigestLimb(t *testing.T) {
	over128 := new(big.Int).Lsh(big.NewInt(1), 128)
	_, err := ComputeTransferIntentV2(TransferIntentV2Input{
		ChainDomainHi:        over128,
		ChainDomainLo:        big.NewInt(1),
		MerkleRoot:           big.NewInt(2),
		AssetID:              big.NewInt(3),
		Nullifiers:           [2]*big.Int{big.NewInt(4), big.NewInt(5)},
		Commitments:          [2]*big.Int{big.NewInt(6), big.NewInt(7)},
		UserDisclosureDigest: big.NewInt(8),
		FullDisclosureDigest: big.NewInt(9),
		PayloadDigestHi:      big.NewInt(10),
		PayloadDigestLo:      big.NewInt(11),
		ExpiresAtUnix:        12,
	})
	require.ErrorContains(t, err, "128-bit")
}

func intentTestTransferMessage() *MsgTransfer {
	return &MsgTransfer{
		Creator:                     "clair1relayer",
		Root:                        []byte{1, 2},
		Nullifiers:                  [][]byte{{3}, {4, 5}},
		NewCommitments:              [][]byte{{6}, {7, 8}},
		CipherTexts:                 [][]byte{{9, 10}, {11}},
		ViewTags:                    [][]byte{{12}, {13}},
		UserPrivacyPolicy:           5,
		UserDisclosureMode:          UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
		UserDisclosureDigest:        []byte{14},
		UserDisclosureTargetPubkey:  []byte{15, 16},
		UserDisclosurePayload:       []byte{17},
		AuditDisclosureDigest:       []byte{18},
		AuditDisclosureTargetPubkey: []byte{19},
		AuditDisclosurePayload:      []byte{20, 21},
		SelfViewDisclosureDigest:    []byte{18},
		SelfViewDisclosurePayload:   []byte{22},
		ExpiresAtUnix:               2_000_000_000,
	}
}

func cloneIntentByteSlice(values [][]byte) [][]byte {
	cloned := make([][]byte, len(values))
	for i := range values {
		cloned[i] = append([]byte(nil), values[i]...)
	}
	return cloned
}
