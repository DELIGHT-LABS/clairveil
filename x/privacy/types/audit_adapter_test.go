package types

import (
	"bytes"
	"testing"

	af "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

func TestAuditGeneratedAdapterOwnsBytesAndUsesLeafCodec(t *testing.T) {
	output := &v2.OutputEffect{Commitment: af.Field32FromUint64(7).Bytes(), Ciphertext: validEnvelopeBytes(t, EnvelopeDepositNoteV1)}
	msg := &v2.MsgDeposit{Creator: sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String(), Amount: "7uclair", Output: output, Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: &v2.AuditAuthorization{KeyId: make([]byte, 32), Epoch: 1, Envelope: make([]byte, 336)}}
	m, err := ValidateAuditMessage(msg)
	require.NoError(t, err)
	dto, encoded, err := AuditOutputsToAux(af.KindDeposit, []*v2.OutputEffect{output})
	require.NoError(t, err)
	native, err := af.EncodeAuxFrame(af.KindDeposit, dto)
	require.NoError(t, err)
	require.Equal(t, native, encoded)
	require.Equal(t, native, m.Aux())
	before := m.Outputs()[0].Ciphertext
	output.Ciphertext[0] ^= 1
	msg.Proof[0] = 1
	msg.Audit.Envelope[0] = 1
	require.Equal(t, before, m.Outputs()[0].Ciphertext)
	require.Zero(t, m.Proof()[0])
	require.Zero(t, m.Envelope()[0])
	copy := m.Outputs()
	copy[0].Ciphertext[0] ^= 1
	require.Equal(t, before, m.Outputs()[0].Ciphertext)
	valid := &v2.OutputEffect{Commitment: af.Field32FromUint64(7).Bytes(), Ciphertext: validEnvelopeBytes(t, EnvelopeDepositNoteV1)}
	for _, mutate := range []func(*v2.OutputEffect){func(o *v2.OutputEffect) { o.ViewTag = []byte{1, 2} }, func(o *v2.OutputEffect) { o.UserDisclosureMode = 256 }, func(o *v2.OutputEffect) { o.UserDisclosureDigest = []byte{1} }, func(o *v2.OutputEffect) { o.UserDisclosureTargetPubkey = []byte{1} }, func(o *v2.OutputEffect) { o.Ciphertext = append(o.Ciphertext, 0) }} {
		bad := proto.Clone(valid).(*v2.OutputEffect)
		mutate(bad)
		_, _, err := AuditOutputsToAux(af.KindDeposit, []*v2.OutputEffect{bad})
		require.Error(t, err)
	}
}
