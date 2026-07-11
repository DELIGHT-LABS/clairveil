package types

import (
	"math/big"
	"strings"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"
)

func TestDefaultGenesis(t *testing.T) {
	gs := DefaultGenesis(validTestCircuitSetIdentity())
	require.NotNil(t, gs)
	require.Empty(t, gs.Commitments)
	require.Empty(t, gs.HistoricalRoots)
	require.Empty(t, gs.Nullifiers)
	require.Empty(t, gs.AuditMasterPubkey)
	require.Empty(t, gs.AuditKeyId)
	require.Zero(t, gs.AuditKeyEpoch)
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

func TestGenesisValidateRejectsReserveBalanceForUnregisteredAsset(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.ReserveBalances = []*ReserveBalanceV1{{
		CanonicalDenom: "uatom",
		TotalDeposited: "1",
		TotalWithdrawn: "0",
	}}

	require.ErrorContains(t, gs.Validate(), `denom "uatom" is not registered in asset_registry`)
}

func TestGenesisValidateRequiresRootSnapshotPerCommitment(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	value := make([]byte, genesisFieldElementByteSize)
	value[31] = 1
	gs.Commitments = [][]byte{value}
	require.ErrorContains(t, gs.Validate(), "must contain every commitment prefix")
}

func TestGenesisValidateAcceptsExactAuditConfig(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.AuditMasterPubkey = validTestAuditTargetPubkey()
	gs.AuditKeyId = "audit.production-1"
	gs.AuditKeyEpoch = 7
	require.NoError(t, gs.Validate())
}

func TestGenesisAuditConfigJSONRoundTrip(t *testing.T) {
	gs := *DefaultGenesis(validTestCircuitSetIdentity())
	gs.AuditMasterPubkey = validTestAuditTargetPubkey()
	gs.AuditKeyId = "master"
	gs.AuditKeyEpoch = 1

	encoded := ModuleCdc.MustMarshalJSON(&gs)
	require.Contains(t, string(encoded), `"audit_key_id":"master"`)
	require.Contains(t, string(encoded), `"audit_key_epoch":"1"`)
	var decoded GenesisState
	ModuleCdc.MustUnmarshalJSON(encoded, &decoded)
	require.Equal(t, gs.AuditMasterPubkey, decoded.AuditMasterPubkey)
	require.Equal(t, gs.AuditKeyId, decoded.AuditKeyId)
	require.Equal(t, gs.AuditKeyEpoch, decoded.AuditKeyEpoch)
}

func TestGenesisValidateRejectsPartialOrMalformedAuditConfig(t *testing.T) {
	target := validTestAuditTargetPubkey()
	for _, tc := range []struct {
		name   string
		id     string
		epoch  uint64
		target []byte
		want   string
	}{
		{name: "target only", target: target, want: "all-zero"},
		{name: "id only", id: "master", want: "all-zero"},
		{name: "epoch only", epoch: 1, want: "all-zero"},
		{name: "missing id", epoch: 1, target: target, want: "all-zero"},
		{name: "missing epoch", id: "master", target: target, want: "positive audit_key_epoch"},
		{name: "missing target", id: "master", epoch: 1, want: "audit_master_pubkey"},
		{name: "invalid id", id: "Master", epoch: 1, target: target, want: "canonical lowercase ASCII"},
		{name: "invalid target", id: "master", epoch: 1, target: make([]byte, 32), want: "audit_master_pubkey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gs := *DefaultGenesis(validTestCircuitSetIdentity())
			gs.AuditMasterPubkey = tc.target
			gs.AuditKeyId = tc.id
			gs.AuditKeyEpoch = tc.epoch
			require.ErrorContains(t, gs.Validate(), tc.want)
		})
	}
}

func validTestAuditTargetPubkey() []byte {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(17))
	encoded := point.Bytes()
	return append([]byte(nil), encoded[:]...)
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
