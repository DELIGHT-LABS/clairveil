package scan

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/stretchr/testify/require"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func mustNoteBytes(t *testing.T, note *privacytypes.Note) []byte {
	t.Helper()
	encoded, err := privacytypes.MarshalNotePlaintextV1(note)
	require.NoError(t, err)
	return encoded
}

func TestParseNoteBytesAndBuildFoundNote(t *testing.T) {
	rootSeed := []byte("scan-root-seed")

	_, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	require.NoError(t, err)
	_, viewPubKey, _, err := privacyidentity.DeriveViewKeys(rootSeed)
	require.NoError(t, err)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		big.NewInt(7),
		"uclair",
		"sdk-scan-test",
	)
	require.NoError(t, err)

	parsed, err := ParseNoteBytes(mustNoteBytes(t, note))
	require.NoError(t, err)
	require.Equal(t, note.Amount.String(), parsed.Amount.String())

	txRes := &cmttypes.ResultTx{
		Hash:   []byte{0xAA, 0xBB, 0xCC},
		Height: 42,
	}
	found := BuildFoundNote(parsed, txRes)

	expectedNullifier, err := privacyfield.CanonicalHexFromBigInt(note.ComputeNullifier())
	require.NoError(t, err)

	require.Equal(t, expectedNullifier, found.Nullifier)
	require.Equal(t, "AABBCC", found.TxHash)
	require.Equal(t, int64(42), found.Height)
	require.False(t, found.IsSpent)
}

func TestParseNoteBytesRejectsInvalidNoteV1Key(t *testing.T) {
	rootSeed := []byte("scan-invalid-note-v1")
	_, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	require.NoError(t, err)
	_, viewPubKey, _, err := privacyidentity.DeriveViewKeys(rootSeed)
	require.NoError(t, err)
	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		big.NewInt(7),
		"uclair",
		"invalid-key",
	)
	require.NoError(t, err)
	encoded := mustNoteBytes(t, note)
	copy(encoded[84:116], make([]byte, 32))
	copy(encoded[116:148], make([]byte, 32))
	encoded[147] = 1

	_, err = ParseNoteBytes(encoded)
	require.ErrorContains(t, err, "identity point is not allowed")
}

func TestProcessScanEventUsesViewTag(t *testing.T) {
	rootSeed := []byte("scan-view-tag-seed")

	spendScalar, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	require.NoError(t, err)
	viewScalar, viewPubKey, _, err := privacyidentity.DeriveViewKeys(rootSeed)
	require.NoError(t, err)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		big.NewInt(11),
		"uclair",
		"view-tag",
	)
	require.NoError(t, err)
	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	cipherText, viewTag, err := privacycrypto.AsymEncryptWithViewTag(mustNoteBytes(t, note), *viewPubKey, commitmentBytes, 0)
	require.NoError(t, err)
	cipherText = wrapTransferNoteCipherText(t, cipherText)

	event := &privacytypes.QueryScanEvent{
		Sequence:  1,
		Height:    22,
		TxHashHex: "AABB",
		EventType: privacytypes.EventTypeShieldedTransfer,
		Outputs: []*privacytypes.QueryScanOutput{
			{
				OutputIndex:    0,
				CommitmentHex:  hex.EncodeToString(commitmentBytes),
				CipherTextHex:  hex.EncodeToString(cipherText),
				ViewTagHex:     hex.EncodeToString(viewTag),
				LeafIndexFound: true,
			},
		},
	}

	found := ProcessScanEvent(event, rootSeed, &spendScalar, &viewScalar)
	require.Len(t, found, 1)
	require.Equal(t, uint64(11), found[0].Note.Amount)

	event.Outputs[0].ViewTagHex = "ffff"
	found = ProcessScanEvent(event, rootSeed, &spendScalar, &viewScalar)
	require.Len(t, found, 1)
	require.Equal(t, uint64(11), found[0].Note.Amount)

	found = processScanEventWithOptions(event, rootSeed, &spendScalar, &viewScalar, processOptions{SkipViewTagMismatch: true})
	require.Empty(t, found)
}

func TestProcessScanEventRejectsMismatchedCommitment(t *testing.T) {
	rootSeed := []byte("scan-commitment-match-seed")

	spendScalar, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	require.NoError(t, err)
	viewScalar, viewPubKey, _, err := privacyidentity.DeriveViewKeys(rootSeed)
	require.NoError(t, err)

	note, err := privacytypes.NewNote(
		pointBigInt(&spendPubKey.X),
		pointBigInt(&spendPubKey.Y),
		pointBigInt(&viewPubKey.X),
		pointBigInt(&viewPubKey.Y),
		big.NewInt(17),
		"uclair",
		"commitment-match",
	)
	require.NoError(t, err)
	commitmentBytes, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	commitmentHex := hex.EncodeToString(commitmentBytes)
	wrongCommitmentHex := alternateCommitmentHex(t, commitmentBytes)

	encryptedNote, err := privacycrypto.Encrypt(mustNoteBytes(t, note), rootSeed)
	require.NoError(t, err)
	encryptedNote = wrapDepositNoteCipherText(t, encryptedNote)
	depositEvent := &privacytypes.QueryScanEvent{
		Sequence:  1,
		Height:    22,
		TxHashHex: "AABB",
		EventType: privacytypes.EventTypeDeposit,
		Outputs: []*privacytypes.QueryScanOutput{
			{
				OutputIndex:      0,
				CommitmentHex:    commitmentHex,
				EncryptedNoteHex: hex.EncodeToString(encryptedNote),
			},
		},
	}
	require.Len(t, ProcessScanEvent(depositEvent, rootSeed, &spendScalar, &viewScalar), 1)

	depositEvent.Outputs[0].CommitmentHex = wrongCommitmentHex
	require.Empty(t, ProcessScanEvent(depositEvent, rootSeed, &spendScalar, &viewScalar))

	cipherText, err := privacycrypto.AsymEncrypt(mustNoteBytes(t, note), *viewPubKey)
	require.NoError(t, err)
	cipherText = wrapTransferNoteCipherText(t, cipherText)
	transferEvent := &privacytypes.QueryScanEvent{
		Sequence:  2,
		Height:    23,
		TxHashHex: "CCDD",
		EventType: privacytypes.EventTypeShieldedTransfer,
		Outputs: []*privacytypes.QueryScanOutput{
			{
				OutputIndex:    0,
				CommitmentHex:  commitmentHex,
				CipherTextHex:  hex.EncodeToString(cipherText),
				LeafIndexFound: true,
			},
		},
	}
	require.Len(t, ProcessScanEvent(transferEvent, rootSeed, &spendScalar, &viewScalar), 1)

	transferEvent.Outputs[0].CommitmentHex = wrongCommitmentHex
	require.Empty(t, ProcessScanEvent(transferEvent, rootSeed, &spendScalar, &viewScalar))
}

func TestProcessPrivacyScanBatchOutputDecryptsDespiteMismatchedViewTag(t *testing.T) {
	rootSeed := []byte("typed-batch-scan")
	spendScalar, spendPubKey, _, err := privacyidentity.DeriveSpendKeys(rootSeed)
	require.NoError(t, err)
	viewScalar, viewPubKey, _, err := privacyidentity.DeriveViewKeys(rootSeed)
	require.NoError(t, err)
	note, err := privacytypes.NewNote(pointBigInt(&spendPubKey.X), pointBigInt(&spendPubKey.Y), pointBigInt(&viewPubKey.X), pointBigInt(&viewPubKey.Y), big.NewInt(31), "uclair", "batch")
	require.NoError(t, err)
	commitment, err := privacyfield.CanonicalBytesFromBigInt(note.ComputeCommitment())
	require.NoError(t, err)
	raw, _, err := privacycrypto.AsymEncryptWithViewTag(mustNoteBytes(t, note), *viewPubKey, commitment, 5)
	require.NoError(t, err)
	wrapped := wrapTransferNoteCipherText(t, raw)
	output := &privacytypes.PrivacyScanOutputV2{Height: 10, GlobalSequence: 8, OutputIndex: 5, EventType: privacytypes.EventTypeBatchTransferV1, Commitment: commitment, Ciphertext: wrapped, ViewTag: []byte{0xff, 0xff}, TxHash: make([]byte, 32), AuditKeyId: "audit-key-id", AuditKeyEpoch: 3}
	found, err := ProcessPrivacyScanOutput(output, rootSeed, &spendScalar, &viewScalar, false)
	require.NoError(t, err)
	require.Equal(t, uint64(31), found.Note.Amount)
	require.Equal(t, uint32(5), found.OutputIndex)
	secretFound, err := ProcessSecretPrivacyScanOutput(output, rootSeed, &spendScalar, &viewScalar, false)
	require.NoError(t, err)
	require.Equal(t, uint64(31), secretFound.Note.Amount)
	require.Equal(t, found.Nullifier, secretFound.Nullifier)
	require.Equal(t, "audit-key-id", secretFound.AuditKeyID)
	require.Equal(t, uint64(3), secretFound.AuditKeyEpoch)
	_, err = ProcessPrivacyScanOutput(output, rootSeed, &spendScalar, &viewScalar, true)
	require.Error(t, err)
}

func alternateCommitmentHex(t *testing.T, original []byte) string {
	t.Helper()

	alternate := make([]byte, 32)
	alternate[31] = 0x42
	if bytes.Equal(alternate, original) {
		alternate[31] = 0x43
	}
	return hex.EncodeToString(alternate)
}

func pointBigInt(value interface{ BigInt(*big.Int) *big.Int }) *big.Int {
	v := new(big.Int)
	value.BigInt(v)
	return v
}

func wrapDepositNoteCipherText(t testing.TB, raw []byte) []byte {
	t.Helper()
	wrapped, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, raw)
	require.NoError(t, err)
	return wrapped
}

func wrapTransferNoteCipherText(t testing.TB, raw []byte) []byte {
	t.Helper()
	wrapped, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, raw)
	require.NoError(t, err)
	return wrapped
}

func mustSecretFoundNoteFromLegacy(t *testing.T, found FoundNote) SecretFoundNote {
	t.Helper()
	plain, err := privacytypes.MarshalNotePlaintextV1(&found.Note)
	require.NoError(t, err)
	note, err := privacytypes.UnmarshalSecretNotePlaintextV1(plain)
	require.NoError(t, err)
	return SecretFoundNote{Note: *note, Nullifier: found.Nullifier, IsSpent: found.IsSpent, TxHash: found.TxHash, Height: found.Height, GlobalSequence: found.GlobalSequence, OutputIndex: found.OutputIndex, Commitment: found.Commitment, AssetDenom: found.AssetDenom}
}
