package types

import (
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis(validTestCircuitSetIdentity())
	require.NotNil(t, gs)
	require.Empty(t, gs.Commitments)
	require.Empty(t, gs.HistoricalRoots)
	require.Empty(t, gs.Nullifiers)
	require.NoError(t, gs.Validate())
}

func TestDefaultGenesisRequiresCircuitIdentity(t *testing.T) {
	require.PanicsWithValue(t, "default privacy genesis requires circuit set identity", func() {
		DefaultGenesis(nil)
	})
}

func TestGenesisValidateRejectsMissingCircuitIdentity(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.CircuitSetIdentity = nil

	require.ErrorContains(t, gs.Validate(), "circuit_set_identity: is required")
}

func TestGenesisValidateRejectsInvalidLength(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.Commitments = [][]byte{{0x01}}

	require.Error(t, gs.Validate())
}

func TestGenesisValidateRejectsNonCanonicalFieldBytes(t *testing.T) {
	nonCanonical := fr.Modulus().Bytes()

	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.Nullifiers = [][]byte{nonCanonical[:]}

	require.Error(t, gs.Validate())
}

func TestGenesisValidateRejectsDuplicateStateElements(t *testing.T) {
	value := make([]byte, genesisFieldElementByteSize)
	value[len(value)-1] = 1

	commitmentState := *DefaultGenesis(validTestCircuitSetIdentity())
	commitmentState.Commitments = [][]byte{value, append([]byte(nil), value...)}
	require.ErrorContains(t, commitmentState.Validate(), "duplicates index 0")

	rootState := *DefaultGenesis(validTestCircuitSetIdentity())
	rootState.HistoricalRoots = [][]byte{value, append([]byte(nil), value...)}
	require.ErrorContains(t, rootState.Validate(), "duplicates index 0")

	nullifierState := *DefaultGenesis(validTestCircuitSetIdentity())
	nullifierState.Nullifiers = [][]byte{value, append([]byte(nil), value...)}
	require.ErrorContains(t, nullifierState.Validate(), "duplicates index 0")
}

func TestGenesisValidateRejectsLegacyStateVersion(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.StateVersion = 1
	require.ErrorContains(t, gs.Validate(), "fresh reset")
}

func TestGenesisValidateRejectsAssetRegistryMismatch(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.AssetRegistry[0].AssetId = make([]byte, genesisFieldElementByteSize)
	gs.AssetRegistry[0].AssetId[31] = 1
	require.ErrorContains(t, gs.Validate(), "does not match canonical denom")
}

func TestGenesisValidateRequiresRootSnapshotPerCommitment(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	value := make([]byte, genesisFieldElementByteSize)
	value[31] = 1
	gs.Commitments = [][]byte{value}
	require.ErrorContains(t, gs.Validate(), "must contain every commitment prefix")
}

func validTestCircuitSetIdentity() *CircuitSetIdentity {
	identity := &CircuitSetIdentity{
		SchemaVersion: CircuitSetIdentitySchemaVersion,
		CircuitSetId:  ActiveCircuitSetID,
		Curve:         CircuitCurveBN254,
		Circuits:      make([]*CircuitIdentity, 0, len(RequiredCircuitIdentityOrder)),
	}
	for _, circuitID := range RequiredCircuitIdentityOrder {
		identity.Circuits = append(identity.Circuits, &CircuitIdentity{
			CircuitId:               circuitID,
			VerifyingKeySha256:      strings.Repeat("a", 64),
			PublicInputSchemaSha256: strings.Repeat("b", 64),
		})
	}
	return identity
}
