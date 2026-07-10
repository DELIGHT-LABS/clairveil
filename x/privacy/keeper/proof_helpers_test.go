package keeper

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestNewPublicWitnessBN254(t *testing.T) {
	assignment := &circuit.SpendCircuit{
		MerkleRoot:        big.NewInt(0),
		ChainDomainHi:     big.NewInt(0),
		ChainDomainLo:     big.NewInt(0),
		ExpiresAtUnix:     big.NewInt(1),
		Nullifier:         big.NewInt(0),
		Amount:            big.NewInt(0),
		RecipientDigestHi: big.NewInt(0),
		RecipientDigestLo: big.NewInt(0),
		AssetID:           big.NewInt(0),
	}

	w, err := newPublicWitnessBN254(assignment)
	require.NoError(t, err)
	require.NotNil(t, w)
}

func TestReadProofBN254InvalidData(t *testing.T) {
	_, err := readProofBN254([]byte{0x01, 0x02, 0x03})
	require.Error(t, err)
}

func TestReadProofBN254AcceptsCanonicalFixedFrame(t *testing.T) {
	proofBytes := canonicalTestProofBytes(t)
	_, err := readProofBN254(proofBytes)
	require.NoError(t, err)
}

func TestProofFramingRejectsDynamicCommitmentCountWithoutGas(t *testing.T) {
	proofBytes := canonicalTestProofBytes(t)
	proofBytes[131] = 1
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(2 * DepositProofVerificationGas))
	loaderCalled := false
	err := verifyProofBN254(ctx, proofBytes, publicSpendAssignment(), DepositProofVerificationGas, "test proof", func() (groth16.VerifyingKey, error) {
		loaderCalled = true
		return nil, errors.New("must not load")
	})
	require.ErrorContains(t, err, "commitments are not supported")
	require.Zero(t, ctx.GasMeter().GasConsumed())
	require.False(t, loaderCalled)
}

func TestProofVerificationPrechargesGasBeforeDecodeOrVKLoad(t *testing.T) {
	proofBytes := canonicalTestProofBytes(t)
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(DepositProofVerificationGas - 1))
	loaderCalled := false
	require.Panics(t, func() {
		_ = verifyProofBN254(ctx, proofBytes, publicSpendAssignment(), DepositProofVerificationGas, "test proof", func() (groth16.VerifyingKey, error) {
			loaderCalled = true
			return nil, errors.New("must not load")
		})
	})
	require.False(t, loaderCalled)
}

func TestFramingValidInvalidProofConsumesFullVerificationGas(t *testing.T) {
	proofBytes := canonicalTestProofBytes(t)
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(2 * DepositProofVerificationGas))
	loaderCalled := false
	err := verifyProofBN254(ctx, proofBytes, publicSpendAssignment(), DepositProofVerificationGas, "test proof", func() (groth16.VerifyingKey, error) {
		loaderCalled = true
		return nil, errors.New("invalid verifying key fixture")
	})
	require.ErrorContains(t, err, "invalid verifying key fixture")
	require.Equal(t, storetypes.Gas(DepositProofVerificationGas), ctx.GasMeter().GasConsumed())
	require.True(t, loaderCalled)
}

func TestMultipleProofAttemptsAccumulateVerificationGas(t *testing.T) {
	proofBytes := canonicalTestProofBytes(t)
	ctx := sdk.Context{}.WithGasMeter(storetypes.NewGasMeter(3 * DepositProofVerificationGas))
	for i := 0; i < 2; i++ {
		err := verifyProofBN254(ctx, proofBytes, publicSpendAssignment(), DepositProofVerificationGas, "test proof", func() (groth16.VerifyingKey, error) {
			return nil, errors.New("invalid verifying key fixture")
		})
		require.Error(t, err)
	}
	require.Equal(t, storetypes.Gas(2*DepositProofVerificationGas), ctx.GasMeter().GasConsumed())
}

func canonicalTestProofBytes(t *testing.T) []byte {
	t.Helper()
	proof := groth16.NewProof(ecc.BN254)
	var encoded bytes.Buffer
	_, err := proof.WriteTo(&encoded)
	require.NoError(t, err)
	require.Len(t, encoded.Bytes(), privacyzk.CanonicalBN254Groth16ProofSize)
	return append([]byte(nil), encoded.Bytes()...)
}

func publicSpendAssignment() *circuit.SpendCircuit {
	return &circuit.SpendCircuit{
		MerkleRoot:        big.NewInt(0),
		ChainDomainHi:     big.NewInt(0),
		ChainDomainLo:     big.NewInt(0),
		ExpiresAtUnix:     big.NewInt(1),
		Nullifier:         big.NewInt(0),
		Amount:            big.NewInt(0),
		RecipientDigestHi: big.NewInt(0),
		RecipientDigestLo: big.NewInt(0),
		AssetID:           big.NewInt(0),
	}
}
