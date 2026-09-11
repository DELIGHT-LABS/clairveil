package provertransport

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	"github.com/stretchr/testify/require"
)

func TestDecodeAuditFieldProofWitnessBindsExactPI23(t *testing.T) {
	inputs := make([]auditfield.Field32, 23)
	values := make(chan any, len(inputs))
	for i := range inputs {
		inputs[i] = auditfield.Field32FromUint64(uint64(i + 1))
		values <- new(big.Int).SetBytes(inputs[i][:])
	}
	close(values)
	full, err := witness.New(ecc.BN254.ScalarField())
	require.NoError(t, err)
	require.NoError(t, full.Fill(len(inputs), 0, values))
	var encoded bytes.Buffer
	_, err = full.WriteTo(&encoded)
	require.NoError(t, err)

	request := AuditFieldProofRequest{
		Version: AuditFieldProofRequestVersion, CircuitSetID: privacyzk.AuditFieldCircuitSetID,
		CircuitID: string(privacyzk.CircuitDepositAuditField), ArtifactHash: bytes.Repeat([]byte{1}, 32),
		PublicInputs: fieldBytes(inputs), Witness: encoded.Bytes(),
	}
	decoded, err := DecodeAuditFieldProofWitness(request)
	require.NoError(t, err)
	require.NotNil(t, decoded)

	request.PublicInputs[0] = auditfield.Field32FromUint64(99).Bytes()
	_, err = DecodeAuditFieldProofWitness(request)
	require.Error(t, err)
}

func TestAuditFieldProofRequestJSONIsStrict(t *testing.T) {
	_, err := DecodeAuditFieldProofRequestJSON([]byte(`{"version":"v1","unexpected":true}`))
	require.Error(t, err)
	_, err = DecodeAuditFieldProofResponseJSON([]byte(`{"version":"v1","unexpected":true}`))
	require.Error(t, err)
}
