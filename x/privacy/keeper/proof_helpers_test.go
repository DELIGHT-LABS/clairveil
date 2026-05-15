package keeper

import (
	"math/big"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	"github.com/stretchr/testify/require"
)

func TestNewPublicWitnessBN254(t *testing.T) {
	assignment := &circuit.SpendCircuit{
		MerkleRoot: big.NewInt(0),
		Nullifier:  big.NewInt(0),
		Amount:     big.NewInt(0),
		Recipient:  big.NewInt(0),
		AssetID:    big.NewInt(0),
	}

	w, err := newPublicWitnessBN254(assignment)
	require.NoError(t, err)
	require.NotNil(t, w)
}

func TestReadProofBN254InvalidData(t *testing.T) {
	_, err := readProofBN254([]byte{0x01, 0x02, 0x03})
	require.Error(t, err)
}
