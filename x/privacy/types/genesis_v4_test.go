package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenesisV4ExportRetainsAuditManagementState(t *testing.T) {
	identity := validTestCircuitSetIdentity()
	identity.CircuitSetId = AuditFieldCircuitSetID
	for index, circuitID := range RequiredAuditCircuitIdentityOrder {
		identity.Circuits[index].CircuitId = circuitID
	}
	state := DefaultGenesis(identity)
	initialKey := InitialAuditKeyV4{Epoch: 1, KeyID: make([]byte, 32), Suite: 2, PublicKey: make([]byte, 64), PoP: make([]byte, 96)}
	genesis := GenesisStateV4{
		FormatVersion:      4,
		Mode:               "FRESH",
		NetworkNonce:       make([]byte, 32),
		InitialHeight:      1,
		RestartHeight:      11,
		InitialAuditKey:    initialKey,
		CircuitSetIdentity: identity,
		State:              state,
		AssetRegistry:      state.AssetRegistry,
		AuditKeyHistory: []*AuditKeyHistoryV4{{
			Epoch: 1, KeyID: make([]byte, 32), Suite: 2, PublicKey: make([]byte, 64), PoP: make([]byte, 96),
			ActivationHeight: 0, RegisteredHeight: 0,
			Origin: &AuditOriginV4{Kind: 3, Anchor: make([]byte, 32), Height: 0},
		}},
		ActiveAuditEpoch: 1,
		PrivacyHalted:    true,
		TreeCapacity:     1 << 32,
	}
	require.NoError(t, genesis.Validate())

	genesis.RestartHeight = 0
	require.ErrorContains(t, genesis.Validate(), "restart height")
}
