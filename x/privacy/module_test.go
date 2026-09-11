package privacy

import (
	"encoding/json"
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestAuditRuntimeBasicAcceptsOnlyFreshV4Genesis(t *testing.T) {
	identity := &types.CircuitSetIdentity{SchemaVersion: types.CircuitSetIdentitySchemaVersion, CircuitSetId: types.ActiveCircuitSetID, Curve: types.CircuitCurveBN254}
	for _, id := range types.RequiredCircuitIdentityOrder {
		identity.Circuits = append(identity.Circuits, &types.CircuitIdentity{CircuitId: id, VerifyingKeySha256: "0000000000000000000000000000000000000000000000000000000000000000", PublicInputSchemaSha256: "0000000000000000000000000000000000000000000000000000000000000000"})
	}
	v4, err := json.Marshal(types.GenesisStateV4{
		FormatVersion:      4,
		Mode:               "FRESH",
		NetworkNonce:       make([]byte, 32),
		InitialHeight:      1,
		InitialAuditKey:    types.InitialAuditKeyV4{Epoch: 1, Suite: 2, KeyID: make([]byte, 32), PublicKey: make([]byte, 64), PoP: make([]byte, 96)},
		CircuitSetIdentity: identity,
		AssetRegistry: []*types.AssetRegistryEntryV1{{
			CanonicalDenom: "uclair",
			AssetId:        types.ComputeAssetIDV1("uclair").FillBytes(make([]byte, 32)),
		}},
		TreeCapacity: 1 << 32,
	})
	require.NoError(t, err)
	basic := AppModuleBasic{AuditRuntime: true}
	require.NoError(t, basic.ValidateGenesis(nil, nil, v4))
	require.Error(t, basic.ValidateGenesis(nil, nil, []byte(`{"format_version":1}`)))
	require.Panics(t, func() { basic.DefaultGenesis(nil) })
}
