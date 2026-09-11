package proverservice

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"math/big"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// TestAuditFieldDepositSDKToProverdRoundTripWithP3Artifacts exercises the
// development-only route with the reviewed P3 bundle. It deliberately does
// not generate a new setup: the SDK sends the full normal-flow witness to the
// local proverd handler, then verifies the returned proof with the same PI23
// and locally pinned verification key before making the v2 message.
func TestAuditFieldDepositSDKToProverdRoundTripWithP3Artifacts(t *testing.T) {
	dir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if dir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{
		ArtifactDir:        dir,
		CircuitSetID:       privacyzk.AuditFieldCircuitSetID,
		RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment,
	})
	require.NoError(t, err)
	identity, err := registry.LocalCircuitSetIdentity()
	require.NoError(t, err)
	manifest, err := os.ReadFile(filepath.Join(dir, privacyzk.ArtifactManifestFile))
	require.NoError(t, err)
	artifactHash := sha256.Sum256(manifest)

	snapshot := auditV2IntegrationSnapshot(t, artifactHash)
	note := auditV2IntegrationDepositNote(t)
	commitment, err := note.CommitmentV1()
	require.NoError(t, err)
	commitmentRaw := commitment.Bytes()
	cipherSize, err := privacytypes.EncryptedEnvelopeV1Size(privacytypes.EnvelopeDepositNoteV1)
	require.NoError(t, err)
	ciphertext, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, make([]byte, cipherSize-privacytypes.EncryptedEnvelopeV1HeaderSize))
	require.NoError(t, err)
	prepared, err := privacydeposit.PrepareAuditV2FromNormalNote(
		snapshot,
		sdk.AccAddress(bytes.Repeat([]byte{0x31}, 20)).String(),
		"7uclair",
		note,
		&privacyv2.OutputEffect{Commitment: commitmentRaw[:], Ciphertext: ciphertext},
		time.Now().Add(time.Hour).Unix(),
	)
	require.NoError(t, err)
	full, err := privacydeposit.BuildAuditV2Witness(prepared, note)
	require.NoError(t, err)

	handler := NewAuditFieldHandler(registry, identity, time.Now, io.Discard, DefaultMaxRequestBz, "")
	server := httptest.NewServer(handler)
	defer server.Close()
	message, err := privacyprovertransport.ProvePreparedAuditField(
		context.Background(),
		privacyprovertransport.HTTPProverClient{BaseURL: server.URL, Client: server.Client()},
		prepared,
		full,
		func(context.Context) (privacyaudit.Snapshot, error) { return snapshot, nil },
		registry,
		identity,
	)
	require.NoError(t, err)
	deposit, ok := message.(*privacyv2.MsgDeposit)
	require.True(t, ok)
	require.NotEmpty(t, deposit.Proof)
	require.NotNil(t, deposit.Audit)
	require.Equal(t, snapshot.Epoch, deposit.Audit.Epoch)
}

func auditV2IntegrationSnapshot(t *testing.T, artifactHash [32]byte) privacyaudit.Snapshot {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 23
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return privacyaudit.Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1, ArtifactHash: artifactHash}
}

func auditV2IntegrationDepositNote(t *testing.T) privacytypes.SecretNoteV1 {
	t.Helper()
	curve := crypto_tedwards.GetEdwardsCurve()
	var spend, view crypto_tedwards.PointAffine
	spend.ScalarMultiplication(&curve.Base, big.NewInt(11))
	view.ScalarMultiplication(&curve.Base, big.NewInt(13))
	spendX, spendY, err := privacycrypto.PublicPointFieldValues(spend)
	require.NoError(t, err)
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(view)
	require.NoError(t, err)
	note, err := privacytypes.NewSecretNoteV1(
		spendX, spendY, viewX, viewY, 7,
		privacytypes.ComputeSecretAssetIDV1("uclair"), privacycrypto.FieldValueFromUint64(17), "",
	)
	require.NoError(t, err)
	return *note
}
