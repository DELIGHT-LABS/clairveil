package crypto

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAuditSecretKeyStrictRoleBoundary(t *testing.T) {
	var raw [32]byte
	_, err := ImportAuditSecretKeyBE32(raw[:])
	require.Error(t, err)
	raw[31] = 37
	key, err := ImportAuditSecretKeyBE32(raw[:])
	require.NoError(t, err)
	require.Equal(t, raw, key.Bytes())
	require.True(t, key.IsValid())
	require.Contains(t, fmt.Sprintf("%#v", key), "<redacted>")
	_, err = json.Marshal(key)
	require.Error(t, err)
	key, err = SampleAuditSecretKey(bytes.NewReader(nil))
	require.ErrorIs(t, err, io.EOF)
	require.False(t, key.IsValid())
	t.Setenv("GODEBUG", "cpu.all=off")
	_, err = ImportAuditSecretKeyBE32(raw[:])
	require.ErrorIs(t, err, ErrUnsupportedSecretProfile)
	_, err = SampleAuditSecretKey(bytes.NewReader(raw[:]))
	require.ErrorIs(t, err, ErrUnsupportedSecretProfile)
}

func TestOwnerSignaturePropagatesRNGFailure(t *testing.T) {
	signature, err := SignOwnerIntent([32]byte{}, scalarFromByte(t, 7), bytes.NewReader(nil))
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, signature)
}
