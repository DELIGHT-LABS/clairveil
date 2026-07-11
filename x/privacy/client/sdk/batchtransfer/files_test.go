package batchtransfer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPreparedBatchTransferFileRoundTripAndPrivateMode(t *testing.T) {
	payload := testPayload(t)
	payloadPath := filepath.Join(t.TempDir(), "prepared.json")
	require.NoError(t, WritePreparedBatchTransferPayload(payloadPath, payload))
	info, err := os.Stat(payloadPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	readPayload, err := ReadPreparedBatchTransferPayload(payloadPath)
	require.NoError(t, err)
	require.Equal(t, payload.PayloadHash, readPayload.PayloadHash)
	require.NoError(t, ValidatePreparedBatchTransferPayloadMetadataAt(readPayload, time.Unix(readPayload.ExpiresAtUnix-1, 0)))

	proof := &PreparedBatchTransferProof{Version: PreparedBatchTransferProofVersion, RequestPayloadHash: payload.PayloadHash, Proof: []byte{1, 2, 3}, CircuitSetID: payload.CircuitSetID}
	proofPath := filepath.Join(t.TempDir(), "proof.json")
	require.NoError(t, WritePreparedBatchTransferProof(proofPath, proof))
	info, err = os.Stat(proofPath)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	readProof, err := ReadPreparedBatchTransferProof(proofPath)
	require.NoError(t, err)
	require.Equal(t, proof, readProof)
}

func TestPreparedBatchTransferFileAtomicallyReplacesSymlinkWithoutTruncatingTarget(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "existing-private-artifact.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("existing artifact"), 0o600))
	payloadPath := filepath.Join(dir, "prepared.json")
	if err := os.Symlink(targetPath, payloadPath); err != nil {
		t.Skipf("symlink is unavailable: %v", err)
	}

	payload := testPayload(t)
	require.NoError(t, WritePreparedBatchTransferPayload(payloadPath, payload))
	targetBytes, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("existing artifact"), targetBytes)
	info, err := os.Lstat(payloadPath)
	require.NoError(t, err)
	require.Zero(t, info.Mode()&os.ModeSymlink)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	readPayload, err := ReadPreparedBatchTransferPayload(payloadPath)
	require.NoError(t, err)
	require.Equal(t, payload.PayloadHash, readPayload.PayloadHash)
	tempFiles, err := filepath.Glob(filepath.Join(dir, ".prepared.json.tmp-*"))
	require.NoError(t, err)
	require.Empty(t, tempFiles)
}

func TestPreparedBatchTransferFileDecodeIsStrict(t *testing.T) {
	payload := testPayload(t)
	payloadBytes, err := json.Marshal(payload)
	require.NoError(t, err)

	unknown := strings.TrimSuffix(string(payloadBytes), "}") + `,"unknown":true}`
	_, err = DecodePreparedBatchTransferPayloadJSON([]byte(unknown))
	require.ErrorContains(t, err, "unknown field")

	duplicate := strings.TrimSuffix(string(payloadBytes), "}") + `,"version":"batch-transfer-payload-v1"}`
	_, err = DecodePreparedBatchTransferPayloadJSON([]byte(duplicate))
	require.ErrorContains(t, err, "duplicate JSON object key")

	_, err = DecodePreparedBatchTransferPayloadJSON(append(payloadBytes, []byte(` {}`)...))
	require.ErrorContains(t, err, "multiple JSON")

	_, err = DecodePreparedBatchTransferProofJSON([]byte(`{"version":"v1","version":"v1"}`))
	require.ErrorContains(t, err, "duplicate JSON object key")
}

func TestReadPreparedBatchTransferDetectsPayloadMutationAtValidation(t *testing.T) {
	payload := testPayload(t)
	payload.Outputs[0].Note.Amount.Add(payload.Outputs[0].Note.Amount, payload.Outputs[0].Note.Amount)
	path := filepath.Join(t.TempDir(), "mutated.json")
	require.NoError(t, WritePreparedBatchTransferPayload(path, payload))

	readPayload, err := ReadPreparedBatchTransferPayload(path)
	require.NoError(t, err)
	err = ValidatePreparedBatchTransferPayloadMetadataAt(readPayload, time.Unix(readPayload.ExpiresAtUnix-1, 0))
	require.Error(t, err)
}
