package types_test

import (
	"encoding/hex"
	"math/big"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	cryptotypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

const (
	referenceMaxTxBytesV1           = 1 << 20
	referenceMaxBlockBytesV1        = 21 << 20
	referenceMaxGRPCBodyBytesV1     = 4 << 20
	referenceMaxTypedStateBytesV1   = 256 << 10
	referenceMaxTotalKVWriteBytesV1 = 256 << 10
	referenceMaxABCIEventBytesV1    = 16 << 10
	batchTreeWriteBytesUpperBoundV1 = 96 << 10
)

type batchWireFeasibilityResult struct {
	MessageBytes             int
	CanonicalPayloadBytes    int
	TxBytes                  int
	TypedScanSummaryBytes    int
	TypedScanOutputBytes     int
	TypedScanKVBytes         int
	TreeWriteBytesUpperBound int
	TotalKVWriteBytes        int
	ABCIEventBytes           int
	QueryResponseBytes       int
}

func TestBatchJoinSplit16x32MaxWireStateFeasibilityGate(t *testing.T) {
	result := measureBatchMaxWireStateV1(t)

	require.Less(t, result.TxBytes, referenceMaxTxBytesV1)
	require.Less(t, result.TxBytes, referenceMaxBlockBytesV1)
	require.Less(t, result.MessageBytes, referenceMaxGRPCBodyBytesV1)
	require.Less(t, result.QueryResponseBytes, referenceMaxGRPCBodyBytesV1)
	require.Less(t, result.TypedScanKVBytes, referenceMaxTypedStateBytesV1)
	require.Less(t, result.TotalKVWriteBytes, referenceMaxTotalKVWriteBytesV1)
	require.Less(t, result.ABCIEventBytes, referenceMaxABCIEventBytesV1)

	t.Logf(
		"BATCH_WIRE_STATE_REPORT message=%d canonical_payload=%d tx=%d scan_summary=%d scan_outputs=%d scan_kv=%d tree_write_upper=%d total_kv_write=%d abci_event=%d query_response=%d",
		result.MessageBytes,
		result.CanonicalPayloadBytes,
		result.TxBytes,
		result.TypedScanSummaryBytes,
		result.TypedScanOutputBytes,
		result.TypedScanKVBytes,
		result.TreeWriteBytesUpperBound,
		result.TotalKVWriteBytes,
		result.ABCIEventBytes,
		result.QueryResponseBytes,
	)

	const goldenTxBytes = 65294
	const goldenTypedScanKVBytes = 75105
	require.Equal(t, goldenTxBytes, result.TxBytes)
	require.Equal(t, goldenTypedScanKVBytes, result.TypedScanKVBytes)
}

func measureBatchMaxWireStateV1(t *testing.T) batchWireFeasibilityResult {
	t.Helper()
	creator := sdk.AccAddress(make([]byte, 20)).String()
	proof := make([]byte, privacyzk.CanonicalBN254Groth16ProofSize)
	root := canonicalFieldBytes(1)
	auditTarget := canonicalPointBytes(71)
	userTarget := canonicalPointBytes(73)
	nullifiers := make([][]byte, privacytypes.BatchJoinSplitV1MaxInputs)
	for i := range nullifiers {
		nullifiers[i] = canonicalFieldBytes(uint64(i + 3))
	}

	noteCiphertext := fixedEnvelope(t, privacytypes.EnvelopeTransferNoteV1)
	userPayload := fixedEnvelope(t, privacytypes.EnvelopeUserDisclosureV1)
	auditPayload := fixedEnvelope(t, privacytypes.EnvelopeAuditDisclosureV1)
	selfViewPayload := fixedEnvelope(t, privacytypes.EnvelopeSelfViewDisclosureV1)
	outputs := make([]*privacytypes.BatchTransferOutputWirePrototypeV1, privacytypes.BatchJoinSplitV1MaxOutputs)
	for i := range outputs {
		outputs[i] = &privacytypes.BatchTransferOutputWirePrototypeV1{
			Commitment:                 canonicalFieldBytes(uint64(i + 33)),
			Ciphertext:                 append([]byte(nil), noteCiphertext...),
			ViewTag:                    []byte{byte(i), byte(i + 1)},
			UserPrivacyPolicy:          privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom,
			UserDisclosureMode:         privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED,
			UserDisclosureDigest:       canonicalFieldBytes(uint64(i + 65)),
			UserDisclosureTargetPubkey: append([]byte(nil), userTarget...),
			UserDisclosurePayload:      append([]byte(nil), userPayload...),
			FullDisclosureDigest:       canonicalFieldBytes(uint64(i + 129)),
			AuditDisclosurePayload:     append([]byte(nil), auditPayload...),
			SelfViewDisclosurePayload:  append([]byte(nil), selfViewPayload...),
		}
	}
	message := &privacytypes.BatchTransferWirePrototypeV1{
		Creator:                     creator,
		Proof:                       proof,
		Root:                        root,
		Nullifiers:                  nullifiers,
		Outputs:                     outputs,
		AuditKeyId:                  strings.Repeat("a", privacytypes.AuditKeyIDV1MaxBytes),
		AuditKeyEpoch:               1,
		AuditDisclosureTargetPubkey: auditTarget,
		ExpiresAtUnix:               2_000_000_000,
	}
	require.NoError(t, privacytypes.ValidateAuditKeyIDV1(message.AuditKeyId))
	require.NoError(t, privacytypes.ValidateBatchTransferWirePrototypeV1(message))
	canonicalPayload, err := privacytypes.CanonicalBatchTransferPayloadBytesV1(message)
	require.NoError(t, err)
	payloadDigest, err := privacytypes.ComputeBatchTransferPayloadDigestV1(message)
	require.NoError(t, err)
	require.NotZero(t, payloadDigest.Hi.Sign())
	require.NotZero(t, payloadDigest.Lo.Sign())
	messageBytes, err := message.Marshal()
	require.NoError(t, err)

	body := &txtypes.TxBody{Messages: []*cryptotypes.Any{{
		TypeUrl: "/clairveil.privacy.v1.BatchTransferWirePrototypeV1",
		Value:   messageBytes,
	}}}
	bodyBytes, err := body.Marshal()
	require.NoError(t, err)
	authInfo := &txtypes.AuthInfo{
		SignerInfos: []*txtypes.SignerInfo{{
			PublicKey: &cryptotypes.Any{
				TypeUrl: "/cosmos.crypto.secp256k1.PubKey",
				Value:   fixedBytes(35, 0xa1),
			},
			ModeInfo: &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{
				Single: &txtypes.ModeInfo_Single{Mode: signingtypes.SignMode_SIGN_MODE_DIRECT},
			}},
			Sequence: 1,
		}},
		Fee: &txtypes.Fee{Amount: sdk.NewCoins(sdk.NewInt64Coin("uclair", 1)), GasLimit: 50_000_000},
	}
	authInfoBytes, err := authInfo.Marshal()
	require.NoError(t, err)
	txRaw := &txtypes.TxRaw{BodyBytes: bodyBytes, AuthInfoBytes: authInfoBytes, Signatures: [][]byte{fixedBytes(64, 0xb1)}}
	txBytes, err := txRaw.Marshal()
	require.NoError(t, err)

	effectID := fixedBytes(32, 0xc1)
	summary := &privacytypes.PrivacyScanSummaryV2{
		GlobalSequence:    1,
		Height:            1,
		TxHash:            fixedBytes(32, 0xc2),
		EventType:         privacytypes.EventTypeBatchTransferV1,
		Nullifiers:        nullifiers,
		OutputCount:       privacytypes.BatchJoinSplitV1MaxOutputs,
		CircuitSetId:      privacytypes.ActiveCircuitSetID,
		PayloadVersion:    privacytypes.FixedPayloadVersionV1,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
		AuditKeyId:        message.AuditKeyId,
		AuditKeyEpoch:     message.AuditKeyEpoch,
		AuditTargetPubkey: auditTarget,
		EffectId:          effectID,
	}
	summaryBytes, err := summary.Marshal()
	require.NoError(t, err)
	scanOutputs := make([]*privacytypes.PrivacyScanOutputV2, len(outputs))
	typedOutputBytes := 0
	typedKVBytes := len(summaryBytes) + len(privacytypes.GetPrivacyScanSummaryKey(summary.Height, summary.GlobalSequence))
	for i, output := range outputs {
		scanOutputs[i] = &privacytypes.PrivacyScanOutputV2{
			GlobalSequence:             summary.GlobalSequence,
			Height:                     summary.Height,
			OutputIndex:                uint32(i),
			EffectId:                   effectID,
			Commitment:                 output.Commitment,
			Ciphertext:                 output.Ciphertext,
			ViewTag:                    output.ViewTag,
			LeafIndex:                  uint64(i),
			LeafIndexFound:             true,
			UserPrivacyPolicy:          output.UserPrivacyPolicy,
			UserDisclosureMode:         output.UserDisclosureMode.String(),
			UserDisclosureDigest:       output.UserDisclosureDigest,
			UserDisclosureTargetPubkey: output.UserDisclosureTargetPubkey,
			UserDisclosurePayload:      output.UserDisclosurePayload,
			FullDisclosureDigest:       output.FullDisclosureDigest,
			AuditDisclosurePayload:     output.AuditDisclosurePayload,
			SelfViewDisclosurePayload:  output.SelfViewDisclosurePayload,
			CircuitSetId:               summary.CircuitSetId,
			PayloadVersion:             summary.PayloadVersion,
			ScanSchemaVersion:          summary.ScanSchemaVersion,
			AuditKeyId:                 summary.AuditKeyId,
			AuditKeyEpoch:              summary.AuditKeyEpoch,
			AuditTargetPubkey:          summary.AuditTargetPubkey,
			TxHash:                     summary.TxHash,
			EventType:                  summary.EventType,
		}
		encoded, err := scanOutputs[i].Marshal()
		require.NoError(t, err)
		typedOutputBytes += len(encoded)
		typedKVBytes += len(encoded) + len(privacytypes.GetPrivacyScanOutputKey(summary.Height, summary.GlobalSequence, uint32(i)))
	}

	queryResponse := &privacytypes.QueryPrivacyScanResponse{
		Summaries:         []*privacytypes.PrivacyScanSummaryV2{summary},
		Outputs:           scanOutputs,
		NextCursor:        &privacytypes.PrivacyScanCursorV1{Height: 1, GlobalSequence: 1, OutputIndex: 31},
		OutputLimit:       privacytypes.BatchJoinSplitV1MaxOutputs,
		EventLimit:        1,
		MaxEncodedBytes:   referenceMaxGRPCBodyBytesV1,
		ScannedEventCount: 1,
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	queryResponseBytes, err := queryResponse.Marshal()
	require.NoError(t, err)

	event := abci.Event{Type: "batch_transfer", Attributes: []abci.EventAttribute{
		{Key: "effect_id", Value: hex.EncodeToString(effectID), Index: true},
		{Key: "relayer", Value: creator, Index: true},
		{Key: "input_count", Value: "16", Index: true},
		{Key: "output_count", Value: "32", Index: true},
		{Key: "nullifier_root", Value: hex.EncodeToString(canonicalFieldBytes(501)), Index: true},
		{Key: "commitment_root", Value: hex.EncodeToString(canonicalFieldBytes(502)), Index: true},
		{Key: "user_disclosure_root", Value: hex.EncodeToString(canonicalFieldBytes(503)), Index: false},
		{Key: "full_disclosure_root", Value: hex.EncodeToString(canonicalFieldBytes(504)), Index: false},
		{Key: "expires_at_unix", Value: "2000000000", Index: false},
	}}
	eventBytes, err := event.Marshal()
	require.NoError(t, err)

	return batchWireFeasibilityResult{
		MessageBytes:             len(messageBytes),
		CanonicalPayloadBytes:    len(canonicalPayload),
		TxBytes:                  len(txBytes),
		TypedScanSummaryBytes:    len(summaryBytes),
		TypedScanOutputBytes:     typedOutputBytes,
		TypedScanKVBytes:         typedKVBytes,
		TreeWriteBytesUpperBound: batchTreeWriteBytesUpperBoundV1,
		TotalKVWriteBytes:        typedKVBytes + batchTreeWriteBytesUpperBoundV1,
		ABCIEventBytes:           len(eventBytes),
		QueryResponseBytes:       len(queryResponseBytes),
	}
}

func fixedEnvelope(t testing.TB, kind privacytypes.EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	size, err := privacytypes.EncryptedEnvelopeV1Size(kind)
	require.NoError(t, err)
	raw := make([]byte, size-privacytypes.EncryptedEnvelopeV1HeaderSize)
	wrapped, err := privacytypes.WrapEncryptedEnvelopeV1(kind, raw)
	require.NoError(t, err)
	return wrapped
}

func fixedBytes(size int, seed byte) []byte {
	result := make([]byte, size)
	for i := range result {
		result[i] = seed + byte(i)
	}
	return result
}

func canonicalFieldBytes(value uint64) []byte {
	return new(big.Int).SetUint64(value).FillBytes(make([]byte, 32))
}

func canonicalPointBytes(scalar int64) []byte {
	curve := crypto_tedwards.GetEdwardsCurve()
	var base crypto_tedwards.PointAffine
	base.X.Set(&curve.Base.X)
	base.Y.Set(&curve.Base.Y)
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&base, big.NewInt(scalar))
	encoded := point.Bytes()
	return append([]byte(nil), encoded[:]...)
}
