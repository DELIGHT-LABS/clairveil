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

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacywithdraw "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/withdraw"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const updateProverExampleFixtureEnv = "PRIVACY_UPDATE_PROVER_EXAMPLE_FIXTURE"

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

func TestWriteProverExampleBundleFixture(t *testing.T) {
	if os.Getenv(updateProverExampleFixtureEnv) != "1" {
		t.Skipf("set %s=1 to rewrite the prover example fixture", updateProverExampleFixtureEnv)
	}

	fixture := loadProverExampleBundleFixture(t)
	payload := &fixture.Transfer.Request.Payload
	userBlinding := big.NewInt(1901)
	fullBlinding := big.NewInt(1907)

	payload.UserDisclosureDigestHex, payload.UserDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.UserDisclosurePayloadHex,
		big.NewInt(referenceUserDisclosureScalar),
		userBlinding,
	)
	payload.AuditDisclosureDigestHex, payload.AuditDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.AuditDisclosurePayloadHex,
		big.NewInt(referenceAuditDisclosureScalar),
		fullBlinding,
	)
	payload.SelfViewDisclosureDigestHex, payload.SelfViewDisclosurePayloadHex = rewriteDisclosureEnvelope(
		t,
		payload.SelfViewDisclosurePayloadHex,
		big.NewInt(referenceSelfViewDisclosureScalar),
		fullBlinding,
	)
	require.Equal(t, payload.AuditDisclosureDigestHex, payload.SelfViewDisclosureDigestHex)

	payload.Version = privacytransfer.PreparedTransferPayloadVersion
	payload.UserDisclosureBlindingHex = mustCanonicalBigIntHex(t, userBlinding)
	payload.FullDisclosureBlindingHex = mustCanonicalBigIntHex(t, fullBlinding)
	payload.PayloadHash = privacytransfer.ComputePreparedTransferPayloadHash(*payload)
	fixture.Transfer.Response.Proof.PayloadHash = payload.PayloadHash

	bz, err := json.MarshalIndent(fixture, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(proverExampleBundleFixturePath(t), append(bz, '\n'), 0o644))
}

func rewriteDisclosureEnvelope(t *testing.T, cipherHex string, scalar, blinding *big.Int) (string, string) {
	t.Helper()
	payload, err := privacydisclosure.DecryptPayloadHex(cipherHex, scalar)
	require.NoError(t, err)
	payload.Version = privacydisclosure.PayloadVersion
	payload.BlindingHex = mustCanonicalBigIntHex(t, blinding)
	digestHex, _, err := privacydisclosure.ComputeExpectedDisclosureDigest(payload)
	require.NoError(t, err)
	payload.DisclosureDigestHex = digestHex

	plainText, err := json.Marshal(payload)
	require.NoError(t, err)
	curve := crypto_tedwards.GetEdwardsCurve()
	var publicKey crypto_tedwards.PointAffine
	publicKey.ScalarMultiplication(&curve.Base, scalar)
	cipherText, err := privacycrypto.AsymEncrypt(plainText, publicKey)
	require.NoError(t, err)
	return digestHex, hex.EncodeToString(cipherText)
}

func mustCanonicalBigIntHex(t *testing.T, value *big.Int) string {
	t.Helper()
	encoded, err := privacyfield.CanonicalHexFromBigInt(value)
	require.NoError(t, err)
	return encoded
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

func proverExampleBundleFixturePath(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	return filepath.Join(filepath.Dir(filename), "testdata", "privacy_prover_example_bundle.json")
}
