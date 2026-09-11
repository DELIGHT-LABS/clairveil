package audit

import (
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/stretchr/testify/require"
)

func TestVerifiedAuditRecordAccessorsDetachMutableBytes(t *testing.T) {
	input := auditfield.Field32FromUint64(7)
	record := VerifiedAuditRecord{record: &auditRecord{
		wire: []byte{1, 2}, sequence: 1, proof: []byte{3}, publicKey: []byte{4}, envelope: []byte{5}, inputs: []auditfield.Field32{input},
		outputs: []AuditOutput{{Commitment: auditfield.Field32FromUint64(8)}}, transparent: &TransparentAuditEffect{From: []byte{9}, To: []byte{10}},
	}}
	wire, proof, key, envelope := record.Wire(), record.Proof(), record.PublicKey(), record.Envelope()
	inputs, outputs := record.Inputs(), record.Outputs()
	effect, found := record.TransparentEffect()
	wire[0], proof[0], key[0], envelope[0] = 99, 99, 99, 99
	inputs[0] = auditfield.Field32{}
	outputs[0].Commitment = auditfield.Field32{}
	effect.From[0], effect.To[0] = 99, 99

	require.Equal(t, []byte{1, 2}, record.Wire())
	require.Equal(t, []byte{3}, record.Proof())
	require.Equal(t, []byte{4}, record.PublicKey())
	require.Equal(t, []byte{5}, record.Envelope())
	require.Equal(t, input, record.Inputs()[0])
	require.Equal(t, auditfield.Field32FromUint64(8), record.Outputs()[0].Commitment)
	stored, ok := record.TransparentEffect()
	require.True(t, found)
	require.True(t, ok)
	require.Equal(t, []byte{9}, stored.From)
	require.Equal(t, []byte{10}, stored.To)
}

func TestDecryptVerifiedRecordRejectsZeroValue(t *testing.T) {
	_, err := DecryptVerifiedRecord(privacycrypto.AuditSecretKey{}, VerifiedAuditRecord{})
	require.Error(t, err)
}
