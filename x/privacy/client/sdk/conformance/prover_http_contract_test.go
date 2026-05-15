package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

type proverHTTPContractFixture struct {
	SchemaVersion string                 `json:"schema_version"`
	ContentType   string                 `json:"content_type"`
	TransferRoute proverHTTPRouteFixture `json:"transfer_route"`
	WithdrawRoute proverHTTPRouteFixture `json:"withdraw_route"`
	ErrorResponse proverHTTPErrorFixture `json:"error_response"`
}

type proverHTTPRouteFixture struct {
	Method          string `json:"method"`
	Path            string `json:"path"`
	RequestVersion  string `json:"request_version"`
	ResponseVersion string `json:"response_version"`
}

type proverHTTPErrorFixture struct {
	Version string   `json:"version"`
	Codes   []string `json:"codes"`
}

func TestProverHTTPContractFixtureMatchesSDK(t *testing.T) {
	fixture := loadProverHTTPContractFixture(t)

	require.Equal(t, "v1", fixture.SchemaVersion)
	require.Equal(t, "application/json", fixture.ContentType)

	require.Equal(t, "POST", fixture.TransferRoute.Method)
	require.Equal(t, privacyprovertransport.TransferProofPath, fixture.TransferRoute.Path)
	require.Equal(t, privacyprovertransport.TransferProofRequestVersion, fixture.TransferRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.TransferProofResponseVersion, fixture.TransferRoute.ResponseVersion)

	require.Equal(t, "POST", fixture.WithdrawRoute.Method)
	require.Equal(t, privacyprovertransport.WithdrawProofPath, fixture.WithdrawRoute.Path)
	require.Equal(t, privacyprovertransport.WithdrawProofRequestVersion, fixture.WithdrawRoute.RequestVersion)
	require.Equal(t, privacyprovertransport.WithdrawProofResponseVersion, fixture.WithdrawRoute.ResponseVersion)

	require.Equal(t, privacyprovertransport.ErrorResponseVersion, fixture.ErrorResponse.Version)
	require.Equal(t, []string{
		privacyprovertransport.ErrorCodeInvalidRequest,
		privacyprovertransport.ErrorCodeMethodNotAllowed,
		privacyprovertransport.ErrorCodeNotFound,
		privacyprovertransport.ErrorCodeUnauthorized,
		privacyprovertransport.ErrorCodeUnavailable,
		privacyprovertransport.ErrorCodeProofFailed,
	}, fixture.ErrorResponse.Codes)
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
