package withdraw

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
)

func testBech32Address() string {
	return sdk.AccAddress(bytes.Repeat([]byte{0x2}, 20)).String()
}

func testBech32AddressWithByte(b byte) string {
	return sdk.AccAddress(bytes.Repeat([]byte{b}, 20)).String()
}

func rebuildPayloadHash(payload *PreparedWithdrawPayload) {
	payload.PayloadHash = ComputePreparedWithdrawPayloadHash(
		payload.ProofHex,
		payload.RootHex,
		payload.NullifierHex,
		payload.Amount,
		payload.Recipient,
		payload.ChainID,
		payload.Version,
		payload.ExpiresAtUnix,
	)
}

func newValidPreparedWithdrawPayload(t *testing.T, recipient string) PreparedWithdrawPayload {
	t.Helper()

	rootBytes, err := privacyfield.CanonicalBytesFromBytes([]byte{0x02})
	require.NoError(t, err)
	nullifierBytes, err := privacyfield.CanonicalBytesFromBytes([]byte{0x03})
	require.NoError(t, err)

	payload, err := BuildPreparedWithdrawPayload(BuildPreparedWithdrawPayloadInput{
		ProofBytes:     []byte{0x01},
		RootBytes:      rootBytes,
		NullifierBytes: nullifierBytes,
		Amount:         "1uclair",
		Recipient:      recipient,
		ChainID:        "clairveil-local-1",
		ExpiresAtUnix:  time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)

	return *payload
}

func TestBuildPreparedWithdrawPayloadSetsVersionAndHash(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())

	require.Equal(t, PreparedWithdrawPayloadVersion, payload.Version)
	require.Equal(t, ComputePreparedWithdrawPayloadHash(
		payload.ProofHex,
		payload.RootHex,
		payload.NullifierHex,
		payload.Amount,
		payload.Recipient,
		payload.ChainID,
		payload.Version,
		payload.ExpiresAtUnix,
	), payload.PayloadHash)
}

func TestPreparedWithdrawPayloadToMsg(t *testing.T) {
	creator := testBech32Address()
	recipient := testBech32Address()

	payload := newValidPreparedWithdrawPayload(t, recipient)

	msg, err := payload.ToMsg(creator)
	require.NoError(t, err)
	expectedRoot, err := privacyfield.CanonicalBytesFromBytes([]byte{0x02})
	require.NoError(t, err)
	expectedNullifier, err := privacyfield.CanonicalBytesFromBytes([]byte{0x03})
	require.NoError(t, err)

	require.Equal(t, creator, msg.Creator)
	require.Equal(t, recipient, msg.Recipient)
	require.Equal(t, "1uclair", msg.Amount)
	require.Equal(t, []byte{0x01}, msg.Proof)
	require.Equal(t, expectedRoot, msg.Root)
	require.Equal(t, expectedNullifier, msg.Nullifier)
	require.Equal(t, payload.ChainID, msg.ChainId)
	require.Equal(t, payload.ExpiresAtUnix, msg.ExpiresAtUnix)
}

func TestPreparedWithdrawPayloadMarshalIndentedJSONRoundTrip(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())

	jsonBytes, err := payload.MarshalIndentedJSON()
	require.NoError(t, err)
	require.Contains(t, string(jsonBytes), "\"payload_hash\"")

	decoded, err := DecodePreparedWithdrawPayloadJSON(jsonBytes)
	require.NoError(t, err)
	require.Equal(t, payload, *decoded)
}

func TestPreparedWithdrawPayloadWriteAndReadJSONFile(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	path := filepath.Join(t.TempDir(), "withdraw-payload.json")

	require.NoError(t, payload.WriteJSONFile(path))

	fileInfo, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), fileInfo.Mode().Perm())

	decoded, err := ReadPreparedWithdrawPayloadFile(path)
	require.NoError(t, err)
	require.Equal(t, payload, *decoded)
}

func TestBuildRelayWithdrawMsgFromJSON(t *testing.T) {
	creator := testBech32Address()
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())

	jsonBytes, err := payload.MarshalIndentedJSON()
	require.NoError(t, err)

	msg, err := BuildRelayWithdrawMsgFromJSON(jsonBytes, creator)
	require.NoError(t, err)
	require.Equal(t, creator, msg.Creator)
	require.Equal(t, payload.Recipient, msg.Recipient)
	require.Equal(t, payload.Amount, msg.Amount)
}

func TestBuildRelayWithdrawMsgFromFile(t *testing.T) {
	creator := testBech32Address()
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	path := filepath.Join(t.TempDir(), "withdraw-payload.json")

	require.NoError(t, payload.WriteJSONFile(path))

	msg, err := BuildRelayWithdrawMsgFromFile(path, creator)
	require.NoError(t, err)
	require.Equal(t, creator, msg.Creator)
	require.Equal(t, payload.Recipient, msg.Recipient)
}

func TestPreparedWithdrawPayloadToMsgInvalid(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, "bad")
	rebuildPayloadHash(&payload)

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
}

func TestPreparedWithdrawPayloadToMsgInvalidField(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	payload.RootHex = ""
	rebuildPayloadHash(&payload)

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
}

func TestPreparedWithdrawPayloadToMsgNonCanonicalField(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	payload.RootHex = hex.EncodeToString(fr.Modulus().Bytes())
	rebuildPayloadHash(&payload)

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
}

func TestPreparedWithdrawPayloadToMsgRejectsExpiredPayload(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	payload.ExpiresAtUnix = time.Now().Add(-time.Minute).Unix()
	rebuildPayloadHash(&payload)

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload expired")
	require.Contains(t, err.Error(), "prepare-withdraw")
}

func TestPreparedWithdrawPayloadToMsgRejectsHashMismatch(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	payload.PayloadHash = "deadbeef"

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
	require.Contains(t, err.Error(), "payload hash mismatch")
	require.Contains(t, err.Error(), "modified after prepare-withdraw")
}

func TestPreparedWithdrawPayloadToMsgRejectsMissingChainID(t *testing.T) {
	payload := newValidPreparedWithdrawPayload(t, testBech32Address())
	payload.ChainID = ""
	rebuildPayloadHash(&payload)

	_, err := payload.ToMsg(testBech32Address())
	require.Error(t, err)
	require.Contains(t, err.Error(), "chain_id")
}

func TestPreparedWithdrawPayloadHashBindsUserIntentFields(t *testing.T) {
	base := newValidPreparedWithdrawPayload(t, testBech32Address())
	baseHash := ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		base.Amount,
		base.Recipient,
		base.ChainID,
		base.Version,
		base.ExpiresAtUnix,
	)

	require.NotEqual(t, baseHash, ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		"2uclair",
		base.Recipient,
		base.ChainID,
		base.Version,
		base.ExpiresAtUnix,
	))
	require.NotEqual(t, baseHash, ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		base.Amount,
		testBech32AddressWithByte(0x3),
		base.ChainID,
		base.Version,
		base.ExpiresAtUnix,
	))
	require.NotEqual(t, baseHash, ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		base.Amount,
		base.Recipient,
		"other-chain",
		base.Version,
		base.ExpiresAtUnix,
	))
	require.NotEqual(t, baseHash, ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		base.Amount,
		base.Recipient,
		base.ChainID,
		"v2",
		base.ExpiresAtUnix,
	))
	require.NotEqual(t, baseHash, ComputePreparedWithdrawPayloadHash(
		base.ProofHex,
		base.RootHex,
		base.NullifierHex,
		base.Amount,
		base.Recipient,
		base.ChainID,
		base.Version,
		base.ExpiresAtUnix+60,
	))
}
