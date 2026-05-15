package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEncodePointHex(t *testing.T) {
	rootSeed := []byte("root-seed-material")
	_, pubKey, _ := deriveDisclosureKeys(rootSeed)

	pubKeyHex := encodePointHex(pubKey)
	decodedPubKey, _, err := decodeDisclosurePubKeyHex(pubKeyHex)
	require.NoError(t, err)
	require.Equal(t, pubKey.Bytes(), decodedPubKey.Bytes())
}
