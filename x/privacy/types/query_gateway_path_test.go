package types

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/stretchr/testify/require"
)

func TestQueryGatewayHTTPPaths(t *testing.T) {
	require.Equal(t, "/clairveil/privacy/v1/nullifier/{nullifier=*}", pattern_Query_CheckNullifier_0.String())
	require.Equal(t, "/clairveil/privacy/v1/nullifiers", pattern_Query_CheckNullifiers_0.String())
	require.Equal(t, "/clairveil/privacy/v1/nullifiers", pattern_Query_CheckNullifiers_1.String())
	require.Equal(t, "/clairveil/privacy/v1/tree_state", pattern_Query_TreeState_0.String())
	require.Equal(t, "/clairveil/privacy/v1/commitment/{commitment_hex=*}", pattern_Query_CommitmentInfo_0.String())
	require.Equal(t, "/clairveil/privacy/v1/events", pattern_Query_PrivacyEvents_0.String())
	require.Equal(t, "/clairveil/privacy/v1/scan_events", pattern_Query_ScanEvents_0.String())
	require.Equal(t, "/clairveil/privacy/v1/merkle_path/{commitment_hex=*}", pattern_Query_MerklePath_0.String())
	require.Equal(t, "/clairveil/privacy/v1/disclosure_config", pattern_Query_DisclosureConfig_0.String())
	require.Equal(t, "/clairveil/privacy/v1/circuit_config", pattern_Query_CircuitConfig_0.String())
	require.Equal(t, "/clairveil/privacy/v1/reserve/{denom=**}", pattern_Query_Reserve_0.String())
	require.Equal(t, "/clairveil/privacy/v1/assets/by_denom/{canonical_denom=**}", pattern_Query_AssetByDenom_0.String())
	require.Equal(t, "/clairveil/privacy/v1/assets/by_id/{asset_id_hex=*}", pattern_Query_AssetByID_0.String())
	require.Equal(t, "/clairveil/privacy/v1/privacy_scan", pattern_Query_PrivacyScan_0.String())
	require.Equal(t, "/clairveil/privacy/v1/commitment_paths_at_root", pattern_Query_CommitmentPathsAtRoot_0.String())
}

type slashDenomQueryServer struct {
	UnimplementedQueryServer
	reserveDenom string
	assetDenom   string
}

func (s *slashDenomQueryServer) Reserve(_ context.Context, req *QueryReserveRequest) (*QueryReserveResponse, error) {
	s.reserveDenom = req.Denom
	return &QueryReserveResponse{}, nil
}

func (s *slashDenomQueryServer) AssetByDenom(_ context.Context, req *QueryAssetByDenomRequest) (*QueryAssetByDenomResponse, error) {
	s.assetDenom = req.CanonicalDenom
	return &QueryAssetByDenomResponse{}, nil
}

func TestQueryGatewayRoutesSlashContainingDenoms(t *testing.T) {
	server := &slashDenomQueryServer{}
	mux := runtime.NewServeMux()
	require.NoError(t, RegisterQueryHandlerServer(context.Background(), mux, server))

	for _, test := range []struct {
		path string
		want string
		got  func() string
	}{
		{path: "/clairveil/privacy/v1/reserve/ibc/ABC123", want: "ibc/ABC123", got: func() string { return server.reserveDenom }},
		{path: "/clairveil/privacy/v1/assets/by_denom/factory/clair1creator/subdenom", want: "factory/clair1creator/subdenom", got: func() string { return server.assetDenom }},
	} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		mux.ServeHTTP(recorder, request)
		require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
		require.Equal(t, test.want, test.got())
	}
}
