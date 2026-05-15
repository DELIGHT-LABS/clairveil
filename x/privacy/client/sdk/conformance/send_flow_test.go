package conformance_test

import (
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyproverservice "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/proverservice"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	updateSendCapableFlowFixtureEnv       = "PRIVACY_UPDATE_SEND_CAPABLE_FLOW_FIXTURES"
	referenceUserDisclosureScalar   int64 = 79
	referenceAuditDisclosureScalar  int64 = 83
)

type sendCapableReferenceFlowBundle struct {
	SchemaVersion string                    `json:"schema_version"`
	Phase         string                    `json:"phase"`
	Topology      string                    `json:"topology"`
	Service       sendCapableServiceBundle  `json:"service"`
	Transfer      sendCapableTransferBundle `json:"transfer"`
	Withdraw      sendCapableWithdrawBundle `json:"withdraw"`
}

type sendCapableServiceBundle struct {
	ServiceName          string `json:"service_name"`
	DefaultListenAddress string `json:"default_listen_address"`
	HealthPath           string `json:"health_path"`
	ReadinessPath        string `json:"readiness_path"`
	TransferPath         string `json:"transfer_path"`
	WithdrawPath         string `json:"withdraw_path"`
	MaxRequestBytes      int64  `json:"max_request_bytes"`
}

type sendCapableTransferBundle struct {
	RequestVersion     string                       `json:"request_version"`
	ResponseVersion    string                       `json:"response_version"`
	Creator            string                       `json:"creator"`
	PayloadHash        string                       `json:"payload_hash"`
	ProofPayloadHash   string                       `json:"proof_payload_hash"`
	MsgCreator         string                       `json:"msg_creator"`
	UserDisclosureMode string                       `json:"user_disclosure_mode"`
	UserPrivacyPolicy  string                       `json:"user_privacy_policy"`
	UserDisclosure     sendCapableDisclosureSummary `json:"user_disclosure"`
	AuditDisclosure    sendCapableDisclosureSummary `json:"audit_disclosure"`
}

type sendCapableDisclosureSummary struct {
	Plane               string   `json:"plane"`
	Policy              string   `json:"policy"`
	OutputIndex         uint32   `json:"output_index"`
	CommitmentHex       string   `json:"commitment_hex"`
	DigestHex           string   `json:"digest_hex"`
	Verified            bool     `json:"verified"`
	DisclosedFields     []string `json:"disclosed_fields"`
	Amount              string   `json:"amount,omitempty"`
	AssetDenom          string   `json:"asset_denom,omitempty"`
	FromShieldedAddress string   `json:"from_shielded_address,omitempty"`
	ToShieldedAddress   string   `json:"to_shielded_address,omitempty"`
}

type sendCapableWithdrawBundle struct {
	ValidationNowUnix int64  `json:"validation_now_unix"`
	RequestVersion    string `json:"request_version"`
	ResponseVersion   string `json:"response_version"`
	PayloadHash       string `json:"payload_hash"`
	ProofPayloadHash  string `json:"proof_payload_hash"`
	FinalPayloadHash  string `json:"final_payload_hash"`
	Amount            string `json:"amount"`
	AssetDenom        string `json:"asset_denom"`
	Recipient         string `json:"recipient"`
	ChainID           string `json:"chain_id"`
	ExpiresAtUnix     int64  `json:"expires_at_unix"`
}

func TestSendCapableReferenceFlowFixture(t *testing.T) {
	expected := buildSendCapableReferenceFlowBundle(t)
	actual := loadSendCapableReferenceFlowBundle(t)

	require.Equal(t, expected, actual)
}

func TestWriteSendCapableReferenceFlowFixture(t *testing.T) {
	if os.Getenv(updateSendCapableFlowFixtureEnv) != "1" {
		t.Skip("set PRIVACY_UPDATE_SEND_CAPABLE_FLOW_FIXTURES=1 to rewrite the send-capable reference flow fixture")
	}

	fixturePath := sendCapableReferenceFlowFixturePath(t)
	payload, err := json.MarshalIndent(buildSendCapableReferenceFlowBundle(t), "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fixturePath, append(payload, '\n'), 0o644))
}

func buildSendCapableReferenceFlowBundle(t *testing.T) sendCapableReferenceFlowBundle {
	t.Helper()

	serviceConfig := privacyproverservice.DefaultServerConfig()
	exampleFixture := loadProverExampleBundleFixture(t)

	transferRequest := exampleFixture.Transfer.Request
	transferResponse := exampleFixture.Transfer.Response

	userDisclosurePayload, err := privacydisclosure.DecryptPayloadHex(
		transferRequest.Payload.UserDisclosurePayloadHex,
		big.NewInt(referenceUserDisclosureScalar),
	)
	require.NoError(t, err)
	userDisclosureVerification, err := privacydisclosure.VerifyPayload(
		userDisclosurePayload,
		transferRequest.Payload.UserDisclosureDigestHex,
	)
	require.NoError(t, err)

	auditDisclosurePayload, err := privacydisclosure.DecryptPayloadHex(
		transferRequest.Payload.AuditDisclosurePayloadHex,
		big.NewInt(referenceAuditDisclosureScalar),
	)
	require.NoError(t, err)
	auditDisclosureVerification, err := privacydisclosure.VerifyPayload(
		auditDisclosurePayload,
		transferRequest.Payload.AuditDisclosureDigestHex,
	)
	require.NoError(t, err)

	transferMsg, err := transferRequest.Payload.ToMsg(transferResponse.Proof)
	require.NoError(t, err)
	require.NoError(t, transferMsg.ValidateBasic())
	require.Equal(t, transferRequest.Payload.Creator, transferMsg.Creator)
	require.Equal(t, transferRequest.Payload.PayloadHash, transferResponse.Proof.PayloadHash)
	require.Equal(t, transferRequest.Payload.UserDisclosureDigestHex, hex.EncodeToString(transferMsg.UserDisclosureDigest))
	require.Equal(t, transferRequest.Payload.AuditDisclosureDigestHex, hex.EncodeToString(transferMsg.AuditDisclosureDigest))

	validationNow := time.Unix(exampleFixture.Withdraw.ValidationNowUnix, 0).UTC()
	withdrawRequest := exampleFixture.Withdraw.Request
	withdrawResponse := exampleFixture.Withdraw.Response
	finalWithdrawPayload, err := withdrawRequest.Payload.ToPreparedWithdrawPayload(withdrawResponse.Proof, validationNow)
	require.NoError(t, err)
	require.NoError(t, privacywithdraw.ValidatePreparedWithdrawPayloadMetadata(*finalWithdrawPayload, validationNow))
	require.Equal(t, withdrawRequest.Payload.PayloadHash, withdrawResponse.Proof.PayloadHash)

	return sendCapableReferenceFlowBundle{
		SchemaVersion: "v1",
		Phase:         "phase-a-send-capable",
		Topology:      "remote-sidecar-first",
		Service: sendCapableServiceBundle{
			ServiceName:          privacyproverservice.ServiceName,
			DefaultListenAddress: serviceConfig.ListenAddress,
			HealthPath:           privacyproverservice.HealthPath,
			ReadinessPath:        privacyproverservice.ReadinessPath,
			TransferPath:         privacyprovertransport.TransferProofPath,
			WithdrawPath:         privacyprovertransport.WithdrawProofPath,
			MaxRequestBytes:      serviceConfig.MaxRequestBytes,
		},
		Transfer: sendCapableTransferBundle{
			RequestVersion:     transferRequest.Version,
			ResponseVersion:    transferResponse.Version,
			Creator:            transferRequest.Payload.Creator,
			PayloadHash:        transferRequest.Payload.PayloadHash,
			ProofPayloadHash:   transferResponse.Proof.PayloadHash,
			MsgCreator:         transferMsg.Creator,
			UserDisclosureMode: privacytransfer.UserDisclosureModeLabel(privacytypes.UserDisclosureMode(transferRequest.Payload.UserDisclosureMode)),
			UserPrivacyPolicy:  privacytransfer.PrivacyPolicyLabel(transferRequest.Payload.UserPrivacyPolicy),
			UserDisclosure: sendCapableDisclosureSummary{
				Plane:               userDisclosurePayload.Plane,
				Policy:              privacytransfer.PrivacyPolicyLabel(userDisclosurePayload.Policy),
				OutputIndex:         userDisclosurePayload.OutputIndex,
				CommitmentHex:       userDisclosurePayload.CommitmentHex,
				DigestHex:           transferRequest.Payload.UserDisclosureDigestHex,
				Verified:            userDisclosureVerification.Verified,
				DisclosedFields:     privacydisclosure.DisclosedFields(userDisclosurePayload),
				Amount:              userDisclosurePayload.Amount,
				AssetDenom:          userDisclosurePayload.AssetDenom,
				FromShieldedAddress: userDisclosurePayload.FromShieldedAddress,
				ToShieldedAddress:   userDisclosurePayload.ToShieldedAddress,
			},
			AuditDisclosure: sendCapableDisclosureSummary{
				Plane:               auditDisclosurePayload.Plane,
				Policy:              privacytransfer.PrivacyPolicyLabel(auditDisclosurePayload.Policy),
				OutputIndex:         auditDisclosurePayload.OutputIndex,
				CommitmentHex:       auditDisclosurePayload.CommitmentHex,
				DigestHex:           transferRequest.Payload.AuditDisclosureDigestHex,
				Verified:            auditDisclosureVerification.Verified,
				DisclosedFields:     privacydisclosure.DisclosedFields(auditDisclosurePayload),
				Amount:              auditDisclosurePayload.Amount,
				AssetDenom:          auditDisclosurePayload.AssetDenom,
				FromShieldedAddress: auditDisclosurePayload.FromShieldedAddress,
				ToShieldedAddress:   auditDisclosurePayload.ToShieldedAddress,
			},
		},
		Withdraw: sendCapableWithdrawBundle{
			ValidationNowUnix: validationNow.Unix(),
			RequestVersion:    withdrawRequest.Version,
			ResponseVersion:   withdrawResponse.Version,
			PayloadHash:       withdrawRequest.Payload.PayloadHash,
			ProofPayloadHash:  withdrawResponse.Proof.PayloadHash,
			FinalPayloadHash:  finalWithdrawPayload.PayloadHash,
			Amount:            finalWithdrawPayload.Amount,
			AssetDenom:        withdrawRequest.Payload.AssetDenom,
			Recipient:         finalWithdrawPayload.Recipient,
			ChainID:           finalWithdrawPayload.ChainID,
			ExpiresAtUnix:     finalWithdrawPayload.ExpiresAtUnix,
		},
	}
}

func loadSendCapableReferenceFlowBundle(t *testing.T) sendCapableReferenceFlowBundle {
	t.Helper()

	bz, err := os.ReadFile(sendCapableReferenceFlowFixturePath(t))
	require.NoError(t, err)

	var bundle sendCapableReferenceFlowBundle
	require.NoError(t, json.Unmarshal(bz, &bundle))
	require.Equal(t, "v1", bundle.SchemaVersion)
	return bundle
}

func sendCapableReferenceFlowFixturePath(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)

	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_send_capable_reference_flow.json")
}
