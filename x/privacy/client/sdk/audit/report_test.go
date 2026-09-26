package audit

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

type acceptingCollectedVerifier struct{ calls int }

func (v *acceptingCollectedVerifier) VerifyCollectedProof(_ context.Context, _ uint64, _ auditfield.Kind, pi [][]byte, proof []byte) error {
	v.calls++
	if len(pi) != 23 || len(proof) != 164 {
		return context.Canceled
	}
	return nil
}

type reportSecrets map[[32]byte]privacycrypto.AuditSecretKey

func (s reportSecrets) AuditSecret(id [32]byte) (privacycrypto.AuditSecretKey, bool) {
	value, found := s[id]
	return value, found
}

func TestBuildProvenanceReportSeparatesBlockAndExecutionCompletion(t *testing.T) {
	row, network, secret := preparedCollectedDeposit(t)
	state := CosmosCacheState{FromHeight: 10, ToHeight: 12, LastProcessedHeight: 12}
	verifier := &acceptingCollectedVerifier{}
	report, err := BuildProvenanceReport(context.Background(), state, network, []CollectedAuditTx{row}, verifier, reportSecrets{row.Key.Key.ID(): secret})
	require.NoError(t, err)
	require.True(t, report.CollectionComplete)
	require.True(t, report.ProvenanceComplete)
	require.EqualValues(t, 12, report.LastProcessedBlock)
	require.NotNil(t, report.LastExecution)
	require.EqualValues(t, 10, report.LastExecution.Height)
	require.EqualValues(t, 1, report.Lineage.TransitionCount)
	require.Len(t, report.Lineage.Unspent, 1)
	require.Equal(t, 1, verifier.calls)
	require.False(t, report.Lineage.Unspent[0].AuditNote.Commitment.IsZero())
}

func TestBuildProvenanceReportMarksMissingKeyAndPartialRangeIncomplete(t *testing.T) {
	row, network, _ := preparedCollectedDeposit(t)
	verifier := &acceptingCollectedVerifier{}
	partial, err := BuildProvenanceReport(context.Background(), CosmosCacheState{FromHeight: 10, ToHeight: 12, LastProcessedHeight: 11}, network, []CollectedAuditTx{row}, verifier, reportSecrets{})
	require.ErrorContains(t, err, "requested block range")
	require.False(t, partial.CollectionComplete)
	require.False(t, partial.ProvenanceComplete)

	missing, err := BuildProvenanceReport(context.Background(), CosmosCacheState{FromHeight: 10, ToHeight: 12, LastProcessedHeight: 12}, network, []CollectedAuditTx{row}, verifier, reportSecrets{})
	require.ErrorContains(t, err, "audit key")
	require.True(t, missing.CollectionComplete)
	require.False(t, missing.ProvenanceComplete)
}

func preparedCollectedDeposit(t *testing.T) (CollectedAuditTx, [32]byte, privacycrypto.AuditSecretKey) {
	return preparedCollectedDepositAmount(t, 7)
}

func preparedCollectedDepositAmount(t *testing.T, amount uint64) (CollectedAuditTx, [32]byte, privacycrypto.AuditSecretKey) {
	t.Helper()
	secretWire := [32]byte{31: 7}
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(secretWire[:])
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native audit profile unavailable: %v", err)
	}
	network := [32]byte{1}
	creatorBytes := bytes.Repeat([]byte{1}, 20)
	creator := sdk.AccAddress(creatorBytes).String()
	assetBytes := privacytypes.ComputeAssetIDV1("uclair").FillBytes(make([]byte, 32))
	asset, err := auditfield.ParseField32(assetBytes)
	require.NoError(t, err)
	commitment := auditfield.Field32FromUint64(99)
	output := &privacyv2.OutputEffect{Commitment: commitment.Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)}
	prepared, err := Prepare(PrepareInput{
		Snapshot: Snapshot{Network: network, Epoch: 1, Key: key, StateHeight: 10, ArtifactHash: [32]byte{1}},
		Kind:     auditfield.KindDeposit, Creator: creator, Amount: fmt.Sprintf("%duclair", amount), PublicAsset: assetBytes, Principal: creatorBytes,
		ExpiresAtUnix: 100, Outputs: []*privacyv2.OutputEffect{output},
		Plaintext: []auditfield.Field32{asset, auditfield.Field32FromUint64(amount), auditfield.Field32FromUint64(2), auditfield.Field32FromUint64(3), auditfield.Field32FromUint64(4), auditfield.Field32FromUint64(5)},
	})
	require.NoError(t, err)
	defer prepared.Clear()
	envelope, err := prepared.EnvelopeBytes()
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{Creator: creator, Amount: fmt.Sprintf("%duclair", amount), Output: output, Proof: make([]byte, 164), ExpiresAtUnix: 100, Audit: &privacyv2.AuditAuthorization{KeyId: key.IDBytes(), Epoch: 1, Envelope: envelope}}
	validated, err := privacytypes.ValidateAuditMessage(message)
	require.NoError(t, err)
	txHash := [32]byte{31: 1}
	executionID := collectedExecutionID(network, txHash, 4, 0)
	return CollectedAuditTx{Height: 10, TxIndex: 2, TxHash: txHash, GlobalSequence: 4, MessageIndex: 0, ExecutionID: executionID, Message: validated, Key: KeyRecord{Epoch: 1, Key: key, ActivationHeight: 9}}, network, secret
}

func TestBuildProvenanceReportAcceptsZeroDeposit(t *testing.T) {
	row, network, secret := preparedCollectedDepositAmount(t, 0)
	report, err := BuildProvenanceReport(context.Background(), CosmosCacheState{FromHeight: 10, ToHeight: 10, LastProcessedHeight: 10}, network, []CollectedAuditTx{row}, &acceptingCollectedVerifier{}, reportSecrets{row.Key.Key.ID(): secret})
	require.NoError(t, err)
	require.True(t, report.ProvenanceComplete)
	require.Len(t, report.Lineage.Unspent, 1)
	require.Zero(t, report.Lineage.Unspent[0].Amount)
	require.Equal(t, []uint64{4}, report.Lineage.Unspent[0].RootDeposits)
	require.Empty(t, report.Lineage.Unspent[0].FundingRootDeposits)
}
