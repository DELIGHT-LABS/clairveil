package conformance_test

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

type proverHTTPContractFixture struct {
	SchemaVersion      string                        `json:"schema_version"`
	ContentType        string                        `json:"content_type"`
	TransferRoute      proverHTTPRouteFixture        `json:"transfer_route"`
	WithdrawRoute      proverHTTPRouteFixture        `json:"withdraw_route"`
	BatchTransferRoute proverHTTPRouteFixture        `json:"batch_transfer_route"`
	DepositRoute       proverHTTPRouteFixture        `json:"deposit_route"`
	CommonPolicy       proverHTTPCommonPolicyFixture `json:"common_policy"`
}

type proverHTTPRouteFixture struct {
	Method          string `json:"method"`
	Path            string `json:"path"`
	RequestVersion  string `json:"request_version"`
	ResponseVersion string `json:"response_version"`
}

type proverHTTPCommonPolicyFixture struct {
	Request struct {
		ContentTypeRequired bool     `json:"content_type_required"`
		AllowedContentTypes []string `json:"allowed_content_types"`
		ContentEncodings    []string `json:"content_encodings"`
		DefaultMaxBytes     int64    `json:"default_max_bytes"`
	} `json:"request"`
	ResponseHeaders struct {
		ContentType           string `json:"content_type"`
		CacheControl          string `json:"cache_control"`
		MethodNotAllowedAllow string `json:"method_not_allowed_allow"`
	} `json:"response_headers"`
	ErrorResponse struct {
		Version  string                      `json:"version"`
		Mappings []errorStatusMappingFixture `json:"mappings"`
	} `json:"error_response"`
	RouteFailureStatus map[string]routeFailureStatusFixture `json:"route_failure_status"`
	RetryPolicy        struct {
		Timeout           string `json:"timeout"`
		SameEndpointRetry string `json:"same_endpoint_retry"`
		AutomaticFailover bool   `json:"automatic_failover"`
	} `json:"retry_policy"`
}

func TestProverHTTPContractFixtureMatchesSDK(t *testing.T) {
	fixture := loadProverHTTPContractFixture(t)

	require.Equal(t, "v3", fixture.SchemaVersion)
	require.Equal(t, "application/json", fixture.ContentType)

	require.Equal(t, "POST", fixture.TransferRoute.Method)
	require.Equal(t, privacyprovertransport.TransferProofPath, fixture.TransferRoute.Path)
	require.Equal(t, privacyprovertransport.TransferProofRequestVersion, fixture.TransferRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.TransferProofResponseVersion, fixture.TransferRoute.ResponseVersion)

	require.Equal(t, "POST", fixture.WithdrawRoute.Method)
	require.Equal(t, privacyprovertransport.WithdrawProofPath, fixture.WithdrawRoute.Path)
	require.Equal(t, privacyprovertransport.WithdrawProofRequestVersion, fixture.WithdrawRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.WithdrawProofResponseVersion, fixture.WithdrawRoute.ResponseVersion)

	require.Equal(t, "POST", fixture.BatchTransferRoute.Method)
	require.Equal(t, privacyprovertransport.BatchTransferProofPath, fixture.BatchTransferRoute.Path)
	require.Equal(t, privacyprovertransport.BatchTransferProofRequestVersion, fixture.BatchTransferRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.BatchTransferProofResponseVersion, fixture.BatchTransferRoute.ResponseVersion)

	require.Equal(t, "POST", fixture.DepositRoute.Method)
	require.Equal(t, privacyprovertransport.DepositProofPath, fixture.DepositRoute.Path)
	require.Equal(t, privacyprovertransport.DepositProofRequestVersion, fixture.DepositRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.DepositProofResponseVersion, fixture.DepositRoute.ResponseVersion)

	require.Equal(t, privacyprovertransport.ErrorResponseVersion, fixture.CommonPolicy.ErrorResponse.Version)
	require.False(t, fixture.CommonPolicy.Request.ContentTypeRequired)
	require.Equal(t, []string{"application/json", "application/json; charset=utf-8"}, fixture.CommonPolicy.Request.AllowedContentTypes)
	require.Equal(t, []string{"identity", "gzip"}, fixture.CommonPolicy.Request.ContentEncodings)
	require.Equal(t, privacyprovertransport.DefaultMaxRequestBytes, fixture.CommonPolicy.Request.DefaultMaxBytes)
	require.Equal(t, "application/json", fixture.CommonPolicy.ResponseHeaders.ContentType)
	require.Equal(t, "no-store", fixture.CommonPolicy.ResponseHeaders.CacheControl)
	require.Equal(t, http.MethodPost, fixture.CommonPolicy.ResponseHeaders.MethodNotAllowedAllow)
	require.Equal(t, expectedProverErrorStatusMappings(), fixture.CommonPolicy.ErrorResponse.Mappings)
	for _, route := range []string{"deposit", "transfer", "withdraw", "batch_transfer"} {
		status, ok := fixture.CommonPolicy.RouteFailureStatus[route]
		require.Truef(t, ok, "missing %s route failure status", route)
		require.Equal(t, http.StatusBadRequest, status.RequestFailure)
		require.Equal(t, http.StatusInternalServerError, status.ProverFailure)
	}
	require.Equal(t, "caller_context_deadline", fixture.CommonPolicy.RetryPolicy.Timeout)
	require.Equal(t, "caller_controlled", fixture.CommonPolicy.RetryPolicy.SameEndpointRetry)
	require.False(t, fixture.CommonPolicy.RetryPolicy.AutomaticFailover)
}

func loadProverHTTPContractFixture(t *testing.T) proverHTTPContractFixture {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "testdata", "privacy_prover_http_api_contract.json")
	bz, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	var fixture proverHTTPContractFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}
