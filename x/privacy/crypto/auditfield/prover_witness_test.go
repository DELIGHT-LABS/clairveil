package auditfield

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
	"github.com/stretchr/testify/require"
)

func TestEncryptionWitnessOwnership(t *testing.T) {
	var empty EncryptionWitness
	_, err := empty.ToProverWitnessBE32()
	require.Error(t, err)
	// Reuse the established native fixture, without adding another wire codec.
	context := testContext(t, KindDeposit, 0, 1)
	plain, err := NewAuditPlain(KindDeposit, []Field32{Field32FromUint64(11), Field32FromUint64(7), Field32FromUint64(1), Field32FromUint64(2), Field32FromUint64(3), Field32FromUint64(4)})
	require.NoError(t, err)
	envelope, root, w, err := EncryptAuditForProver(context, plain)
	require.NoError(t, err)
	raw, err := w.ToProverWitnessBE32()
	require.NoError(t, err)
	scalar, err := scalarct.ParseNonzeroScalarBE32(raw[:])
	require.NoError(t, err)
	expected, expectedRoot, err := encryptAuditWithNonce(context.AuditKey(), scalar, context, envelope.Nonce(), plain)
	require.NoError(t, err)
	require.Equal(t, expected.EphemeralFrame().Bytes(), envelope.EphemeralFrame().Bytes())
	require.Equal(t, expectedRoot, root)
	for _, format := range []string{"%v", "%+v", "%#v", "%x"} {
		require.Equal(t, "auditfield.EncryptionWitness(<redacted>)", fmt.Sprintf(format, w))
	}
	_, err = json.Marshal(w)
	require.Error(t, err)
	_, err = w.MarshalText()
	require.Error(t, err)
	w.Clear()
	_, err = w.ToProverWitnessBE32()
	require.Error(t, err)
	clear(raw[:])
}
