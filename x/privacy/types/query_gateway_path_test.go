package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueryGatewayHTTPPaths(t *testing.T) {
	require.Equal(t, "/clairveil/privacy/v1/nullifier/{nullifier=*}", pattern_Query_CheckNullifier_0.String())
	require.Equal(t, "/clairveil/privacy/v1/tree_state", pattern_Query_TreeState_0.String())
	require.Equal(t, "/clairveil/privacy/v1/commitment/{commitment_hex=*}", pattern_Query_CommitmentInfo_0.String())
	require.Equal(t, "/clairveil/privacy/v1/events", pattern_Query_PrivacyEvents_0.String())
	require.Equal(t, "/clairveil/privacy/v1/merkle_path/{commitment_hex=*}", pattern_Query_MerklePath_0.String())
	require.Equal(t, "/clairveil/privacy/v1/disclosure_config", pattern_Query_DisclosureConfig_0.String())
	require.Equal(t, "/clairveil/privacy/v1/circuit_config", pattern_Query_CircuitConfig_0.String())
	require.Equal(t, "/clairveil/privacy/v1/reserve/{denom=*}", pattern_Query_Reserve_0.String())
}
