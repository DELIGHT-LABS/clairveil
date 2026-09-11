package main

import (
	"bytes"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestVerifiedNoteDisplayUsesFixedRecoveryWire(t *testing.T) {
	var raw [32]byte
	raw[31] = 7
	scalar, err := privacycrypto.ImportNonzeroScalarBE32(raw[:])
	require.NoError(t, err)
	point, err := privacycrypto.PublicKey(scalar)
	require.NoError(t, err)
	x, y, err := privacycrypto.PublicPointFieldValues(*point)
	require.NoError(t, err)
	note, err := privacytypes.NewSecretNoteV1(x, y, x, y, 23, privacycrypto.FieldValueFromUint64(5), privacycrypto.FieldValueFromUint64(87654321), "memo")
	require.NoError(t, err)
	plaintext, err := privacytypes.MarshalSecretNotePlaintextV1(note)
	require.NoError(t, err)
	var out bytes.Buffer
	require.NoError(t, writeVerifiedNote(&out, plaintext))
	require.Contains(t, out.String(), "Amount: 23")
	require.Contains(t, out.String(), "Memo: memo")
	require.NotContains(t, out.String(), "87654321")
	out.Reset()
	require.Error(t, writeVerifiedNote(&out, []byte(`{"am":23}`)))
	require.Empty(t, out.String())
}
