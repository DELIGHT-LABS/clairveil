package conformance_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

type proverExampleBundleFixture struct {
	SchemaVersion string                 `json:"schema_version"`
	Transfer      transferExampleFixture `json:"transfer"`
	Withdraw      withdrawExampleFixture `json:"withdraw"`
}

type transferExampleFixture struct {
	Request  privacyprovertransport.TransferProofRequest  `json:"request"`
	Response privacyprovertransport.TransferProofResponse `json:"response"`
}

type withdrawExampleFixture struct {
	ValidationNowUnix int64                                        `json:"validation_now_unix"`
	Request           privacyprovertransport.WithdrawProofRequest  `json:"request"`
	Response          privacyprovertransport.WithdrawProofResponse `json:"response"`
}

func TestProverExampleBundleFixture(t *testing.T) {
	fixture := loadProverExampleBundleFixture(t)
	require.Equal(t, "v1", fixture.SchemaVersion)

	transferRequestJSON, err := fixture.Transfer.Request.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferRequest, err := privacyprovertransport.DecodeTransferProofRequestJSON(transferRequestJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateTransferProofRequest(*decodedTransferRequest))

	transferResponseJSON, err := fixture.Transfer.Response.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedTransferResponse, err := privacyprovertransport.DecodeTransferProofResponseJSON(transferResponseJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateTransferProofResponse(*decodedTransferRequest, *decodedTransferResponse))

	transferMsg, err := decodedTransferRequest.Payload.ToMsg(decodedTransferResponse.Proof)
	require.NoError(t, err)
	require.NoError(t, transferMsg.ValidateBasic())

	validationNow := time.Unix(fixture.Withdraw.ValidationNowUnix, 0).UTC()

	withdrawRequestJSON, err := fixture.Withdraw.Request.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawRequest, err := privacyprovertransport.DecodeWithdrawProofRequestJSON(withdrawRequestJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateWithdrawProofRequest(*decodedWithdrawRequest, validationNow))

	withdrawResponseJSON, err := fixture.Withdraw.Response.MarshalIndentedJSON()
	require.NoError(t, err)
	decodedWithdrawResponse, err := privacyprovertransport.DecodeWithdrawProofResponseJSON(withdrawResponseJSON)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateWithdrawProofResponse(*decodedWithdrawRequest, *decodedWithdrawResponse, validationNow))

	finalWithdrawPayload, err := decodedWithdrawRequest.Payload.ToPreparedWithdrawPayload(decodedWithdrawResponse.Proof, validationNow)
	require.NoError(t, err)
	require.NoError(t, privacywithdraw.ValidatePreparedWithdrawPayloadMetadata(*finalWithdrawPayload, validationNow))
}

func loadProverExampleBundleFixture(t *testing.T) proverExampleBundleFixture {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "testdata", "privacy_prover_example_bundle.json")
	bz, err := os.ReadFile(fixturePath)
	require.NoError(t, err)

	var fixture proverExampleBundleFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}
