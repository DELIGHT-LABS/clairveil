package audit

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

func TestVerifyFinalMessageBindingWithdrawRejectsRecipientAndEnvelopeChangesButAllowsRelayer(t *testing.T) {
	key := testAuditKey(t)
	creator := sdk.AccAddress(bytes.Repeat([]byte{1}, 20))
	relayer := sdk.AccAddress(bytes.Repeat([]byte{2}, 20))
	recipient := sdk.AccAddress(bytes.Repeat([]byte{3}, 20))
	asset := privacytypes.ComputeAssetIDV1("uclair").FillBytes(make([]byte, 32))
	root := auditfield.Field32FromUint64(9)
	nullifier := auditfield.Field32FromUint64(11)
	commitment := auditfield.Field32FromUint64(13)
	snapshot := Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: [32]byte{9}}
	prepared, err := Prepare(PrepareInput{
		Snapshot: snapshot, Kind: auditfield.KindWithdraw, Creator: creator.String(), Recipient: recipient.String(), Amount: "7uclair",
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(), Root: root.Bytes(), Inputs: [][]byte{nullifier.Bytes()}, PublicAsset: asset, Principal: recipient,
		Plaintext: []auditfield.Field32{mustAuditField(t, asset), commitment},
	})
	require.NoError(t, err)
	defer prepared.Clear()
	envelope, err := prepared.EnvelopeBytes()
	require.NoError(t, err)
	keyID := key.IDBytes()
	withdraw := &privacyv2.MsgWithdraw{Creator: creator.String(), Recipient: recipient.String(), Amount: "7uclair", Root: root.Bytes(), Nullifier: nullifier.Bytes(), Proof: make([]byte, 164), ExpiresAtUnix: prepared.ExpiresAtUnix(), Audit: &privacyv2.AuditAuthorization{KeyId: keyID, Epoch: snapshot.Epoch, Envelope: envelope}}
	pi := fieldBytesForTest(prepared.PublicInputs())
	require.NoError(t, VerifyFinalMessageBinding(snapshot, withdraw, pi))

	relayed := *withdraw
	relayed.Creator = relayer.String()
	require.NoError(t, VerifyFinalMessageBinding(snapshot, &relayed, pi))

	badRecipient := relayed
	badRecipient.Recipient = creator.String()
	require.ErrorContains(t, VerifyFinalMessageBinding(snapshot, &badRecipient, pi), "public input")

	badEnvelope := relayed
	badEnvelope.Audit = &privacyv2.AuditAuthorization{KeyId: bytes.Clone(relayed.Audit.KeyId), Epoch: relayed.Audit.Epoch, Envelope: bytes.Clone(relayed.Audit.Envelope)}
	badEnvelope.Audit.Envelope[len(badEnvelope.Audit.Envelope)-1] ^= 1
	require.ErrorContains(t, VerifyFinalMessageBinding(snapshot, &badEnvelope, pi), "public input")
}

func mustAuditField(t *testing.T, raw []byte) auditfield.Field32 {
	t.Helper()
	value, err := auditfield.ParseField32(raw)
	require.NoError(t, err)
	return value
}
func fieldBytesForTest(values []auditfield.Field32) [][]byte {
	out := make([][]byte, len(values))
	for i := range values {
		out[i] = values[i].Bytes()
	}
	return out
}
