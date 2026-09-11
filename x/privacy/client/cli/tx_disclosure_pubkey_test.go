package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodePointHex(t *testing.T) {
	rootSeed := []byte("root-seed-material")
	_, pubKey, _, deriveErr := deriveDisclosureKeys(rootSeed)
	require.NoError(t, deriveErr)

	pubKeyHex := encodePointHex(pubKey)
	decodedPubKey, _, err := decodeDisclosurePubKeyHex(pubKeyHex)
	require.NoError(t, err)
	require.Equal(t, pubKey.Bytes(), decodedPubKey.Bytes())
}
