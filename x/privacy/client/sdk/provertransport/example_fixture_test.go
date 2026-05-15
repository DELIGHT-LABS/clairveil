package provertransport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const updateExampleFixturesEnv = "PRIVACY_UPDATE_EXAMPLE_FIXTURES"

type proverExampleBundleFixture struct {
	SchemaVersion string                 `json:"schema_version"`
	Transfer      transferExampleFixture `json:"transfer"`
	Withdraw      withdrawExampleFixture `json:"withdraw"`
}

type transferExampleFixture struct {
	Request  TransferProofRequest  `json:"request"`
	Response TransferProofResponse `json:"response"`
}

type withdrawExampleFixture struct {
	ValidationNowUnix int64                 `json:"validation_now_unix"`
	Request           WithdrawProofRequest  `json:"request"`
	Response          WithdrawProofResponse `json:"response"`
}

func TestWriteReferenceExampleFixture(t *testing.T) {
	if os.Getenv(updateExampleFixturesEnv) != "1" {
		t.Skipf("set %s=1 to rewrite example fixtures", updateExampleFixturesEnv)
	}

	fixture := buildReferenceExampleFixture(t)
	fixtureBytes, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	fixturePath := filepath.Join(filepath.Dir(filename), "..", "conformance", "testdata", "privacy_prover_example_bundle.json")
	require.NoError(t, os.WriteFile(fixturePath, append(fixtureBytes, '\n'), 0o600))
}

func buildReferenceExampleFixture(t *testing.T) proverExampleBundleFixture {
	t.Helper()

	transferPayload, transferArtifacts, transferRunner := testPreparedTransferPayload(t)
	transferRequest, err := NewTransferProofRequest(transferPayload)
	require.NoError(t, err)
	transferResponse, err := BuildTransferProofResponse(*transferRequest, transferArtifacts, transferRunner)
	require.NoError(t, err)

	validationNow := time.Unix(4102444800, 0).UTC()
	withdrawPayload, withdrawArtifacts, withdrawRunner := testPreparedWithdrawProverPayload(t, validationNow)
	withdrawRequest, err := NewWithdrawProofRequest(withdrawPayload, validationNow)
	require.NoError(t, err)
	withdrawResponse, err := BuildWithdrawProofResponse(*withdrawRequest, withdrawArtifacts, withdrawRunner, validationNow)
	require.NoError(t, err)

	return proverExampleBundleFixture{
		SchemaVersion: "v1",
		Transfer: transferExampleFixture{
			Request:  *transferRequest,
			Response: *transferResponse,
		},
		Withdraw: withdrawExampleFixture{
			ValidationNowUnix: validationNow.Unix(),
			Request:           *withdrawRequest,
			Response:          *withdrawResponse,
		},
	}
}
