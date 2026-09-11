package crypto

import (
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/secretprofile"
	"github.com/stretchr/testify/require"
)

func TestAuditFacadeAuthenticationAndProfile(t *testing.T) {
	sk := auditScalarFromByte(t, 37)
	key, err := AuditKeyFromSecret(sk)
	require.NoError(t, err)
	x, y, err := key.Point().Coordinates()
	require.NoError(t, err)
	hi, lo := auditfield.DigestFields(key.ID())
	pi := make([]auditfield.Field32, auditfield.AuditContextPublicFieldCount)
	pi[2], pi[3], pi[4], pi[5], pi[6] = hi, lo, auditfield.Field32FromUint64(7), x, y
	pi[7], pi[10] = auditfield.Field32FromUint64(2000000000), auditfield.Field32FromUint64(1)
	context, err := auditfield.NewAuditContext(auditfield.KindDeposit, pi)
	require.NoError(t, err)
	values := make([]auditfield.Field32, 6)
	for i := range values {
		values[i] = auditfield.Field32FromUint64(uint64(i + 1))
	}
	plain, err := auditfield.NewAuditPlain(auditfield.KindDeposit, values)
	require.NoError(t, err)
	envelope, root, err := EncryptAudit(context, plain)
	require.NoError(t, err)
	out, err := DecryptAudit(sk, context, envelope, root)
	require.NoError(t, err)
	require.Equal(t, values, out.Fields())
	out, err = DecryptAudit(auditScalarFromByte(t, 38), context, envelope, root)
	require.ErrorIs(t, err, ErrAuditDecrypt)
	require.Nil(t, out)
	wrong := root
	wrong.Left = auditfield.Field32FromUint64(1)
	out, err = DecryptAudit(sk, context, envelope, wrong)
	require.ErrorIs(t, err, ErrAuditDecrypt)
	require.Nil(t, out)
	proof, err := CreateAuditPoP96(sk, [32]byte{9}, 7, 123)
	require.NoError(t, err)
	require.NoError(t, VerifyAuditPoP96(key, [32]byte{9}, 7, 123, proof))
	require.Error(t, VerifyAuditPoP96(key, [32]byte{9}, 7, 124, proof))
	t.Setenv("GODEBUG", "cpu.all=off")
	out, err = DecryptAudit(sk, context, envelope, root)
	require.ErrorIs(t, err, secretprofile.ErrUnsupported)
	require.Nil(t, out)
}

func auditScalarFromByte(t *testing.T, b byte) AuditSecretKey {
	t.Helper()
	var raw [32]byte
	raw[31] = b
	sk, err := ImportAuditSecretKeyBE32(raw[:])
	require.NoError(t, err)
	return sk
}
