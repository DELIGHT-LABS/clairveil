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

func TestAuditAdapterAllowsZeroDepositOnly(t *testing.T) {
	creator := sdk.AccAddress(bytes.Repeat([]byte{1}, 20)).String()
	auth := &v2.AuditAuthorization{KeyId: make([]byte, 32), Epoch: 1, Envelope: make([]byte, 336)}
	deposit := &v2.MsgDeposit{Creator: creator, Amount: "0uclair", Output: &v2.OutputEffect{Commitment: af.Field32FromUint64(7).Bytes(), Ciphertext: validEnvelopeBytes(t, EnvelopeDepositNoteV1)}, Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth}
	validated, err := ValidateAuditMessage(deposit)
	require.NoError(t, err)
	require.True(t, validated.Coin().IsZero())
	for _, amount := range []string{"00uclair", "-1uclair", "0.0uclair", "18446744073709551616uclair", "0x"} {
		deposit.Amount = amount
		_, err := ValidateAuditMessage(deposit)
		require.Error(t, err, amount)
	}
	size, err := af.KindWithdraw.EnvelopeSize()
	require.NoError(t, err)
	auth.Envelope = make([]byte, size)
	withdraw := &v2.MsgWithdraw{Creator: creator, Recipient: creator, Amount: "0uclair", Root: af.Field32FromUint64(9).Bytes(), Nullifier: af.Field32FromUint64(8).Bytes(), Proof: make([]byte, 164), ExpiresAtUnix: 1, Audit: auth}
	_, err = ValidateAuditMessage(withdraw)
	require.ErrorContains(t, err, "withdraw must be positive")
}
