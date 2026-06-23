package conformance_test

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
)

const (
	updateRelayWithdrawContractFixtureEnv = "PRIVACY_UPDATE_RELAY_WITHDRAW_CONTRACT_FIXTURES"
	msgWithdrawTypeURL                    = "/clairveil.privacy.v1.MsgWithdraw"
)

type relayWithdrawContractFixture struct {
	SchemaVersion  string                          `json:"schema_version"`
	HandoffVersion string                          `json:"handoff_version"`
	Transport      string                          `json:"transport"`
	Request        relayWithdrawRequestFixture     `json:"request"`
	Relayer        relayWithdrawRelayerFixture     `json:"relayer"`
	ExpectedMsg    relayWithdrawExpectedMsgFixture `json:"expected_msg"`
}

type relayWithdrawRequestFixture struct {
	Version string                                  `json:"version"`
	Payload privacywithdraw.PreparedWithdrawPayload `json:"payload"`
}

type relayWithdrawRelayerFixture struct {
	Address string `json:"address"`
}

type relayWithdrawExpectedMsgFixture struct {
	TypeURL       string `json:"type_url"`
	Creator       string `json:"creator"`
	ProofHex      string `json:"proof_hex"`
	RootHex       string `json:"root_hex"`
	NullifierHex  string `json:"nullifier_hex"`
	Amount        string `json:"amount"`
	Recipient     string `json:"recipient"`
	ChainID       string `json:"chain_id"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

func TestRelayWithdrawContractFixture(t *testing.T) {
	expected := buildRelayWithdrawContractFixture(t)
	actual := loadRelayWithdrawContractFixture(t)

	require.Equal(t, expected, actual)
	requireRelayWithdrawContractInvariants(t, actual)
}

func TestWriteRelayWithdrawContractFixture(t *testing.T) {
	if os.Getenv(updateRelayWithdrawContractFixtureEnv) != "1" {
		t.Skip("set PRIVACY_UPDATE_RELAY_WITHDRAW_CONTRACT_FIXTURES=1 to rewrite the relay-withdraw contract fixture")
	}

	fixturePath := relayWithdrawContractFixturePath(t)
	payload, err := json.MarshalIndent(buildRelayWithdrawContractFixture(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fixturePath, append(payload, '\n'), 0o644))
}

func buildRelayWithdrawContractFixture(t *testing.T) relayWithdrawContractFixture {
	t.Helper()

	exampleFixture := loadProverExampleBundleFixture(t)
	validationNow := time.Unix(exampleFixture.Withdraw.ValidationNowUnix, 0).UTC()
	withdrawRequest := exampleFixture.Withdraw.Request
	withdrawResponse := exampleFixture.Withdraw.Response
	finalWithdrawPayload, err := withdrawRequest.Payload.ToPreparedWithdrawPayload(withdrawResponse.Proof, validationNow)
	require.NoError(t, err)
	require.NoError(t, privacywithdraw.ValidatePreparedWithdrawPayloadMetadata(*finalWithdrawPayload, validationNow))
	require.Equal(t, withdrawRequest.Payload.PayloadHash, withdrawResponse.Proof.PayloadHash)

	relayerAddress := sdk.AccAddress(bytes.Repeat([]byte{0x9}, 20)).String()
	msg, err := finalWithdrawPayload.ToMsgAt(relayerAddress, validationNow)
	require.NoError(t, err)
	require.NoError(t, msg.ValidateBasic())

	return relayWithdrawContractFixture{
		SchemaVersion:  "v1",
		HandoffVersion: "v1",
		Transport:      "transport-agnostic",
		Request: relayWithdrawRequestFixture{
			Version: "v1",
			Payload: *finalWithdrawPayload,
		},
		Relayer: relayWithdrawRelayerFixture{
			Address: relayerAddress,
		},
		ExpectedMsg: relayWithdrawExpectedMsgFixture{
			TypeURL:       msgWithdrawTypeURL,
			Creator:       msg.Creator,
			ProofHex:      hex.EncodeToString(msg.Proof),
			RootHex:       hex.EncodeToString(msg.Root),
			NullifierHex:  hex.EncodeToString(msg.Nullifier),
			Amount:        msg.Amount,
			Recipient:     msg.Recipient,
			ChainID:       msg.ChainId,
			ExpiresAtUnix: msg.ExpiresAtUnix,
		},
	}
}

func requireRelayWithdrawContractInvariants(t *testing.T, fixture relayWithdrawContractFixture) {
	t.Helper()

	payload := fixture.Request.Payload
	expectedHash := privacywithdraw.ComputePreparedWithdrawPayloadHash(
		payload.ProofHex,
		payload.RootHex,
		payload.NullifierHex,
		payload.Amount,
		payload.Recipient,
		payload.ChainID,
		payload.Version,
		payload.ExpiresAtUnix,
	)

	require.Equal(t, "v1", fixture.SchemaVersion)
	require.Equal(t, "v1", fixture.HandoffVersion)
	require.Equal(t, "transport-agnostic", fixture.Transport)
	require.Equal(t, expectedHash, payload.PayloadHash)
	require.Equal(t, msgWithdrawTypeURL, fixture.ExpectedMsg.TypeURL)
	require.Equal(t, fixture.Relayer.Address, fixture.ExpectedMsg.Creator)
	require.Equal(t, payload.ProofHex, fixture.ExpectedMsg.ProofHex)
	require.Equal(t, payload.RootHex, fixture.ExpectedMsg.RootHex)
	require.Equal(t, payload.NullifierHex, fixture.ExpectedMsg.NullifierHex)
	require.Equal(t, payload.Amount, fixture.ExpectedMsg.Amount)
	require.Equal(t, payload.Recipient, fixture.ExpectedMsg.Recipient)
	require.Equal(t, payload.ChainID, fixture.ExpectedMsg.ChainID)
	require.Equal(t, payload.ExpiresAtUnix, fixture.ExpectedMsg.ExpiresAtUnix)
}

func loadRelayWithdrawContractFixture(t *testing.T) relayWithdrawContractFixture {
	t.Helper()

	bz, err := os.ReadFile(relayWithdrawContractFixturePath(t))
	require.NoError(t, err)

	var fixture relayWithdrawContractFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}

func relayWithdrawContractFixturePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_relay_withdraw_contract.json")
}
