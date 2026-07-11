package keeper

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	DefaultPrivacyScanOutputLimit = uint32(128)
	MaxPrivacyScanOutputLimit     = uint32(512)
	DefaultPrivacyScanEventLimit  = uint32(64)
	MaxPrivacyScanEventLimit      = uint32(256)
	DefaultPrivacyScanByteLimit   = uint64(1 << 20)
	MaxPrivacyScanByteLimit       = uint64(4 << 20)
	MaxPrivacyScanStateRecordSize = 1 << 20
)

var errPrivacyScanRecordExceedsBudget = errors.New("privacy scan record exceeds query byte budget")

func (k Keeper) AllocatePrivacyGlobalSequence(ctx sdk.Context) (uint64, error) {
	store := k.storeService.OpenKVStore(ctx)
	current, err := store.Get(types.GetPrivacyGlobalSequenceKey())
	if err != nil {
		return 0, err
	}
	var sequence uint64
	if len(current) != 0 {
		if len(current) != 8 {
			return 0, fmt.Errorf("privacy global sequence state is corrupt")
		}
		sequence = binary.BigEndian.Uint64(current)
	}
	if sequence == math.MaxUint64 {
		return 0, fmt.Errorf("privacy global sequence exhausted")
	}
	sequence++
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, sequence)
	if err := store.Set(types.GetPrivacyGlobalSequenceKey(), bz); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (k Keeper) GetPrivacyGlobalSequence(ctx sdk.Context) (uint64, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.GetPrivacyGlobalSequenceKey())
	if err != nil {
		return 0, err
	}
	if len(bz) == 0 {
		return 0, nil
	}
	if len(bz) != 8 {
		return 0, fmt.Errorf("privacy global sequence state is corrupt")
	}
	return binary.BigEndian.Uint64(bz), nil
}

func (k Keeper) setPrivacyGlobalSequence(ctx sdk.Context, sequence uint64) error {
	bz := make([]byte, 8)
	binary.BigEndian.PutUint64(bz, sequence)
	return k.storeService.OpenKVStore(ctx).Set(types.GetPrivacyGlobalSequenceKey(), bz)
}

func validatePrivacyScanCursor(cursor *types.PrivacyScanCursorV1) error {
	if cursor == nil {
		return nil
	}
	if cursor.Height < 0 {
		return fmt.Errorf("privacy scan cursor height must not be negative")
	}
	if cursor.GlobalSequence == 0 && cursor.OutputIndex != 0 {
		return fmt.Errorf("privacy scan cursor output_index requires a global_sequence")
	}
	return nil
}

func comparePrivacyScanCursor(left, right *types.PrivacyScanCursorV1) int {
	if left.Height != right.Height {
		if left.Height < right.Height {
			return -1
		}
		return 1
	}
	if left.GlobalSequence != right.GlobalSequence {
		if left.GlobalSequence < right.GlobalSequence {
			return -1
		}
		return 1
	}
	if left.OutputIndex < right.OutputIndex {
		return -1
	}
	if left.OutputIndex > right.OutputIndex {
		return 1
	}
	return 0
}

func scanOutputCursor(output *types.PrivacyScanOutputV2) *types.PrivacyScanCursorV1 {
	return &types.PrivacyScanCursorV1{
		Height:         output.Height,
		GlobalSequence: output.GlobalSequence,
		OutputIndex:    output.OutputIndex,
	}
}

func scanBytesAreZero(value []byte) bool {
	for _, b := range value {
		if b != 0 {
			return false
		}
	}
	return true
}

func validatePrivacyScanPointV2(name string, encoded []byte) error {
	if _, err := privacycrypto.DecodeCanonicalPoint(encoded); err != nil {
		return fmt.Errorf("%s must be a canonical non-identity prime-subgroup point: %w", name, err)
	}
	return nil
}

func validatePrivacyScanActiveFieldV2(name string, encoded []byte) error {
	canonical, err := validateFieldElementBytesStrict(encoded)
	if err != nil || scanBytesAreZero(canonical) {
		return fmt.Errorf("%s must be an active canonical field element", name)
	}
	return nil
}

func validatePrivacyScanSummaryNoAuditV2(summary *types.PrivacyScanSummaryV2) error {
	if summary.AuditKeyId != "" || summary.AuditKeyEpoch != 0 || len(summary.AuditTargetPubkey) != 0 {
		return fmt.Errorf("privacy scan %s summary must use the zero audit sentinel", summary.EventType)
	}
	return nil
}

func validatePrivacyScanSummaryEventV2(summary *types.PrivacyScanSummaryV2) error {
	switch summary.EventType {
	case types.EventTypeDeposit:
		if summary.OutputCount != 1 || len(summary.Nullifiers) != 0 || len(summary.EffectId) != 0 {
			return fmt.Errorf("privacy scan deposit summary requires one output and no nullifier/effect id")
		}
		return validatePrivacyScanSummaryNoAuditV2(summary)
	case types.EventTypeWithdraw:
		if summary.OutputCount != 0 || len(summary.Nullifiers) != 1 || len(summary.EffectId) != 0 {
			return fmt.Errorf("privacy scan withdraw summary requires one nullifier, no outputs, and no effect id")
		}
		return validatePrivacyScanSummaryNoAuditV2(summary)
	case types.EventTypeShieldedTransfer:
		if summary.OutputCount != 2 || len(summary.Nullifiers) != 2 || len(summary.EffectId) != 0 {
			return fmt.Errorf("privacy scan shielded transfer summary requires two nullifiers/outputs and no effect id")
		}
		if summary.AuditKeyId != "" || summary.AuditKeyEpoch != 0 {
			return fmt.Errorf("privacy scan shielded transfer summary must use the legacy zero audit id/epoch sentinel")
		}
		return validatePrivacyScanPointV2("privacy scan audit target", summary.AuditTargetPubkey)
	case types.EventTypeBatchTransferV1:
		if len(summary.Nullifiers) == 0 || len(summary.Nullifiers) > int(types.BatchJoinSplitV1MaxInputs) || summary.OutputCount == 0 || summary.OutputCount > types.BatchJoinSplitV1MaxOutputs {
			return fmt.Errorf("privacy scan batch summary count is outside the 1..16/1..32 contract")
		}
		if len(summary.EffectId) != 32 || scanBytesAreZero(summary.EffectId) {
			return fmt.Errorf("privacy scan batch summary requires a non-zero 32-byte effect id")
		}
		if err := types.ValidateAuditKeyIDV1(summary.AuditKeyId); err != nil {
			return err
		}
		if summary.AuditKeyEpoch == 0 {
			return fmt.Errorf("privacy scan batch summary audit key epoch must be positive")
		}
		return validatePrivacyScanPointV2("privacy scan audit target", summary.AuditTargetPubkey)
	default:
		return fmt.Errorf("unsupported privacy scan event type %q", summary.EventType)
	}
}

func validatePrivacyScanSummaryV2(summary *types.PrivacyScanSummaryV2) error {
	if summary == nil {
		return fmt.Errorf("privacy scan summary is required")
	}
	if summary.Height < 0 || summary.GlobalSequence == 0 {
		return fmt.Errorf("privacy scan summary cursor is invalid")
	}
	if strings.TrimSpace(summary.EventType) == "" || summary.EventType != strings.TrimSpace(summary.EventType) {
		return fmt.Errorf("privacy scan summary event_type is invalid")
	}
	if summary.CircuitSetId != types.ActiveCircuitSetID {
		return fmt.Errorf("privacy scan summary circuit_set_id must be %q", types.ActiveCircuitSetID)
	}
	if summary.PayloadVersion != types.FixedPayloadVersionV1 {
		return fmt.Errorf("privacy scan summary payload_version must be %q", types.FixedPayloadVersionV1)
	}
	if summary.ScanSchemaVersion != types.PrivacyScanSchemaVersionV2 {
		return fmt.Errorf("privacy scan summary scan_schema_version must be %q", types.PrivacyScanSchemaVersionV2)
	}
	if summary.OutputCount > 32 {
		return fmt.Errorf("privacy scan summary output_count exceeds 32")
	}
	if len(summary.Nullifiers) > 16 {
		return fmt.Errorf("privacy scan summary nullifier count exceeds 16")
	}
	if err := types.ValidateDistinctCanonicalFieldElements("privacy scan nullifier", summary.Nullifiers); err != nil {
		return err
	}
	for _, nullifier := range summary.Nullifiers {
		if bytes.Equal(nullifier, make([]byte, fieldElementByteSize)) {
			return fmt.Errorf("privacy scan nullifier must not be zero")
		}
	}
	if len(summary.TxHash) != 0 && len(summary.TxHash) != 32 {
		return fmt.Errorf("privacy scan summary tx_hash must be empty or 32 bytes")
	}
	if len(summary.EffectId) != 0 && len(summary.EffectId) != 32 {
		return fmt.Errorf("privacy scan summary effect_id must be empty or 32 bytes")
	}
	return validatePrivacyScanSummaryEventV2(summary)
}

func validatePrivacyScanEmptyDisclosureV2(output *types.PrivacyScanOutputV2) error {
	if output.UserPrivacyPolicy != 0 || output.UserDisclosureMode != "" ||
		len(output.UserDisclosureDigest) != 0 || len(output.UserDisclosureTargetPubkey) != 0 || len(output.UserDisclosurePayload) != 0 ||
		len(output.FullDisclosureDigest) != 0 || len(output.AuditDisclosurePayload) != 0 || len(output.SelfViewDisclosurePayload) != 0 {
		return fmt.Errorf("privacy scan output must use exact zero disclosure sentinels")
	}
	return nil
}

func validatePrivacyScanUserDisclosureV2(output *types.PrivacyScanOutputV2) error {
	if output.UserPrivacyPolicy > types.TransferPrivacyPolicyDiscloseAmountToFrom {
		return fmt.Errorf("privacy scan user disclosure policy is invalid")
	}
	if output.UserPrivacyPolicy == types.TransferPrivacyPolicyAllPrivate {
		if output.UserDisclosureMode != types.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE.String() ||
			len(output.UserDisclosureDigest) != 0 || len(output.UserDisclosureTargetPubkey) != 0 || len(output.UserDisclosurePayload) != 0 {
			return fmt.Errorf("privacy scan all-private disclosure must use mode NONE and zero digest/target/payload")
		}
		return nil
	}
	if err := validatePrivacyScanActiveFieldV2("privacy scan user disclosure digest", output.UserDisclosureDigest); err != nil {
		return err
	}
	switch output.UserDisclosureMode {
	case types.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String():
		if len(output.UserDisclosureTargetPubkey) != 0 {
			return fmt.Errorf("privacy scan public user disclosure target must be empty")
		}
		plaintext, err := types.UnmarshalDisclosurePlaintextV1(output.UserDisclosurePayload)
		if err != nil {
			return fmt.Errorf("privacy scan public user disclosure is not canonical: %w", err)
		}
		if plaintext.Plane != types.DisclosurePlaneUserV1 || plaintext.OutputIndex != output.OutputIndex || plaintext.Policy != output.UserPrivacyPolicy || !bytes.Equal(plaintext.Commitment.FillBytes(make([]byte, 32)), output.Commitment) {
			return fmt.Errorf("privacy scan public user disclosure metadata does not match output")
		}
		var expected []byte
		if output.EventType == types.EventTypeBatchTransferV1 {
			digest, digestErr := types.ComputeBatchUserDisclosureDigestV1(types.BatchUserDisclosureV1Input{
				OutputIndex: plaintext.OutputIndex, Commitment: plaintext.Commitment,
				Policy: plaintext.Policy, DisclosedFieldBitmap: plaintext.DisclosedFieldBitmap,
				SelectedAmount: plaintext.Amount, AssetID: plaintext.AssetID,
				SelectedFromSpendKeyX: plaintext.SenderSpendKeyX, SelectedFromSpendKeyY: plaintext.SenderSpendKeyY,
				SelectedFromViewKeyX: plaintext.SenderViewKeyX, SelectedFromViewKeyY: plaintext.SenderViewKeyY,
				SelectedToSpendKeyX: plaintext.RecipientSpendKeyX, SelectedToSpendKeyY: plaintext.RecipientSpendKeyY,
				SelectedToViewKeyX: plaintext.RecipientViewKeyX, SelectedToViewKeyY: plaintext.RecipientViewKeyY,
				UserDisclosureBlinding: plaintext.DisclosureBlinding,
			})
			if digestErr == nil {
				expected = digest.FillBytes(make([]byte, 32))
			}
			err = digestErr
		} else {
			expected, err = types.ComputeTransferDisclosureDigestBytes(
				plaintext.Policy, plaintext.OutputIndex, output.Commitment,
				plaintext.Amount, plaintext.AssetID,
				plaintext.SenderSpendKeyX, plaintext.SenderSpendKeyY,
				plaintext.SenderViewKeyX, plaintext.SenderViewKeyY,
				plaintext.RecipientSpendKeyX, plaintext.RecipientSpendKeyY,
				plaintext.RecipientViewKeyX, plaintext.RecipientViewKeyY,
				plaintext.DisclosureBlinding,
			)
		}
		if err != nil || !bytes.Equal(expected, output.UserDisclosureDigest) {
			return fmt.Errorf("privacy scan public user disclosure digest does not match plaintext")
		}
	case types.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED.String():
		if err := validatePrivacyScanPointV2("privacy scan user disclosure target", output.UserDisclosureTargetPubkey); err != nil {
			return err
		}
		if _, err := types.UnwrapEncryptedEnvelopeV1(output.UserDisclosurePayload, types.EnvelopeUserDisclosureV1); err != nil {
			return fmt.Errorf("privacy scan user disclosure envelope is invalid: %w", err)
		}
	default:
		return fmt.Errorf("privacy scan user disclosure mode is invalid")
	}
	return nil
}

func validatePrivacyScanFullDisclosureV2(output *types.PrivacyScanOutputV2) error {
	if err := validatePrivacyScanActiveFieldV2("privacy scan full disclosure digest", output.FullDisclosureDigest); err != nil {
		return err
	}
	if _, err := types.UnwrapEncryptedEnvelopeV1(output.AuditDisclosurePayload, types.EnvelopeAuditDisclosureV1); err != nil {
		return fmt.Errorf("privacy scan audit disclosure envelope is invalid: %w", err)
	}
	if len(output.SelfViewDisclosurePayload) != 0 {
		if _, err := types.UnwrapEncryptedEnvelopeV1(output.SelfViewDisclosurePayload, types.EnvelopeSelfViewDisclosureV1); err != nil {
			return fmt.Errorf("privacy scan self-view disclosure envelope is invalid: %w", err)
		}
	}
	return nil
}

func validatePrivacyScanOutputEventV2(output *types.PrivacyScanOutputV2) error {
	switch output.EventType {
	case types.EventTypeDeposit:
		if len(output.Ciphertext) != 0 || len(output.ViewTag) != 0 {
			return fmt.Errorf("privacy scan deposit output must use encrypted_note and an empty ciphertext/view tag")
		}
		if _, err := types.UnwrapEncryptedEnvelopeV1(output.EncryptedNote, types.EnvelopeDepositNoteV1); err != nil {
			return fmt.Errorf("privacy scan deposit note envelope is invalid: %w", err)
		}
		return validatePrivacyScanEmptyDisclosureV2(output)
	case types.EventTypeShieldedTransfer, types.EventTypeBatchTransferV1:
		if len(output.EncryptedNote) != 0 || len(output.ViewTag) != types.ViewTagLength {
			return fmt.Errorf("privacy scan transfer output must use ciphertext and an exact view tag")
		}
		if _, err := types.UnwrapEncryptedEnvelopeV1(output.Ciphertext, types.EnvelopeTransferNoteV1); err != nil {
			return fmt.Errorf("privacy scan transfer note envelope is invalid: %w", err)
		}
		if output.EventType == types.EventTypeShieldedTransfer && output.OutputIndex == 1 {
			return validatePrivacyScanEmptyDisclosureV2(output)
		}
		if err := validatePrivacyScanUserDisclosureV2(output); err != nil {
			return err
		}
		return validatePrivacyScanFullDisclosureV2(output)
	default:
		return fmt.Errorf("privacy scan event %q cannot contain an output", output.EventType)
	}
}

func (k Keeper) validatePrivacyScanOutputV2(summary *types.PrivacyScanSummaryV2, output *types.PrivacyScanOutputV2, expectedIndex uint32) error {
	if output == nil {
		return fmt.Errorf("privacy scan output is required")
	}
	if output.Height != summary.Height || output.GlobalSequence != summary.GlobalSequence || output.OutputIndex != expectedIndex {
		return fmt.Errorf("privacy scan output cursor does not match summary/order")
	}
	if output.EventType != summary.EventType || !bytes.Equal(output.TxHash, summary.TxHash) || !bytes.Equal(output.EffectId, summary.EffectId) {
		return fmt.Errorf("privacy scan output identity does not match summary")
	}
	if output.CircuitSetId != summary.CircuitSetId || output.PayloadVersion != summary.PayloadVersion || output.ScanSchemaVersion != summary.ScanSchemaVersion {
		return fmt.Errorf("privacy scan output version does not match summary")
	}
	if output.AuditKeyId != summary.AuditKeyId || output.AuditKeyEpoch != summary.AuditKeyEpoch || !bytes.Equal(output.AuditTargetPubkey, summary.AuditTargetPubkey) {
		return fmt.Errorf("privacy scan output audit identity does not match summary")
	}
	canonicalCommitment, err := validateFieldElementBytesStrict(output.Commitment)
	if err != nil || bytes.Equal(canonicalCommitment, make([]byte, fieldElementByteSize)) {
		return fmt.Errorf("privacy scan output commitment must be an active canonical field element")
	}
	if len(output.ViewTag) != 0 && len(output.ViewTag) != types.ViewTagLength {
		return fmt.Errorf("privacy scan output view_tag must be empty or %d bytes", types.ViewTagLength)
	}
	return validatePrivacyScanOutputEventV2(output)
}

func (k Keeper) validatePrivacyScanOutputStateV2(ctx sdk.Context, summary *types.PrivacyScanSummaryV2, output *types.PrivacyScanOutputV2, expectedIndex uint32) error {
	if err := k.validatePrivacyScanOutputV2(summary, output, expectedIndex); err != nil {
		return err
	}
	if !output.LeafIndexFound {
		return fmt.Errorf("privacy scan output leaf index must be present")
	}
	leafIndex, found, err := k.GetCommitmentIndex(ctx, output.Commitment)
	if err != nil {
		return err
	}
	if !found || leafIndex != output.LeafIndex {
		return fmt.Errorf("privacy scan output leaf index does not match commitment state")
	}
	return nil
}

func (k Keeper) getPrivacyScanEventOutputsV2(ctx sdk.Context, summary *types.PrivacyScanSummaryV2) ([]*types.PrivacyScanOutputV2, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.GetPrivacyScanOutputEventPrefix(summary.Height, summary.GlobalSequence)
	iterator, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	outputs := make([]*types.PrivacyScanOutputV2, 0, summary.OutputCount)
	for expectedIndex := uint32(0); iterator.Valid(); iterator.Next() {
		if expectedIndex >= summary.OutputCount {
			return nil, fmt.Errorf("privacy scan event contains output beyond summary output_count")
		}
		cursor, err := decodePrivacyScanOutputKey(iterator.Key())
		if err != nil {
			return nil, err
		}
		if cursor.Height != summary.Height || cursor.GlobalSequence != summary.GlobalSequence || cursor.OutputIndex != expectedIndex {
			return nil, fmt.Errorf("privacy scan event output keys are not an exact contiguous prefix")
		}
		var output types.PrivacyScanOutputV2
		if err := output.Unmarshal(iterator.Value()); err != nil {
			return nil, fmt.Errorf("privacy scan output is corrupt: %w", err)
		}
		if err := k.validatePrivacyScanOutputStateV2(ctx, summary, &output, expectedIndex); err != nil {
			return nil, fmt.Errorf("privacy scan output %d does not match summary/state: %w", expectedIndex, err)
		}
		outputCopy := output
		outputs = append(outputs, &outputCopy)
		expectedIndex++
	}
	if uint32(len(outputs)) != summary.OutputCount {
		return nil, fmt.Errorf("privacy scan summary has incomplete output records")
	}
	return outputs, nil
}

// StorePrivacyScanV2 is the shared typed index writer for Deposit,
// JoinSplit2x2, and future batch operations.
func (k Keeper) StorePrivacyScanV2(ctx sdk.Context, summary *types.PrivacyScanSummaryV2, outputs []*types.PrivacyScanOutputV2) error {
	if err := validatePrivacyScanSummaryV2(summary); err != nil {
		return err
	}
	if uint32(len(outputs)) != summary.OutputCount {
		return fmt.Errorf("privacy scan output count does not match summary")
	}
	for i, output := range outputs {
		if err := k.validatePrivacyScanOutputStateV2(ctx, summary, output, uint32(i)); err != nil {
			return fmt.Errorf("privacy scan output %d: %w", i, err)
		}
		if output.Size() > MaxPrivacyScanStateRecordSize {
			return fmt.Errorf("privacy scan output %d exceeds state record byte limit", i)
		}
	}
	if summary.Size() > MaxPrivacyScanStateRecordSize {
		return fmt.Errorf("privacy scan summary exceeds state record byte limit")
	}

	store := k.storeService.OpenKVStore(ctx)
	summaryKey := types.GetPrivacyScanSummaryKey(summary.Height, summary.GlobalSequence)
	sequenceKey := types.GetPrivacyScanSequenceKey(summary.GlobalSequence)
	sequenceExists, err := store.Has(sequenceKey)
	if err != nil {
		return err
	}
	if sequenceExists {
		return fmt.Errorf("privacy scan global sequence is already indexed")
	}
	exists, err := store.Has(summaryKey)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf("privacy scan summary cursor is already indexed")
	}
	eventOutputPrefix := types.GetPrivacyScanOutputEventPrefix(summary.Height, summary.GlobalSequence)
	outputIterator, err := store.Iterator(eventOutputPrefix, storetypes.PrefixEndBytes(eventOutputPrefix))
	if err != nil {
		return err
	}
	outputExists := outputIterator.Valid()
	if err := outputIterator.Close(); err != nil {
		return err
	}
	if outputExists {
		return fmt.Errorf("privacy scan event output prefix is already indexed")
	}
	for _, output := range outputs {
		key := types.GetPrivacyScanOutputKey(output.Height, output.GlobalSequence, output.OutputIndex)
		exists, err := store.Has(key)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("privacy scan output cursor is already indexed")
		}
	}

	encodedSummary, err := summary.Marshal()
	if err != nil {
		return err
	}
	if err := store.Set(summaryKey, encodedSummary); err != nil {
		return err
	}
	heightBytes := make([]byte, 8)
	binary.BigEndian.PutUint64(heightBytes, uint64(summary.Height))
	if err := store.Set(sequenceKey, heightBytes); err != nil {
		return err
	}
	for _, output := range outputs {
		encodedOutput, err := output.Marshal()
		if err != nil {
			return err
		}
		if err := store.Set(types.GetPrivacyScanOutputKey(output.Height, output.GlobalSequence, output.OutputIndex), encodedOutput); err != nil {
			return err
		}
	}
	return nil
}

func decodeOptionalHexAttribute(attrs map[string]string, key string) ([]byte, error) {
	value := strings.TrimSpace(attrs[key])
	if value == "" {
		return nil, nil
	}
	decoded, err := hex.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("privacy event attribute %s must be hex: %w", key, err)
	}
	return decoded, nil
}

func decodeRequiredHexAttribute(attrs map[string]string, key string) ([]byte, error) {
	decoded, err := decodeOptionalHexAttribute(attrs, key)
	if err != nil {
		return nil, err
	}
	if len(decoded) == 0 {
		return nil, fmt.Errorf("privacy event attribute %s is required", key)
	}
	return decoded, nil
}

func parseOptionalUint32Attribute(attrs map[string]string, key string) (uint32, error) {
	value := strings.TrimSpace(attrs[key])
	if value == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("privacy event attribute %s must be uint32", key)
	}
	return uint32(parsed), nil
}

func (k Keeper) buildLegacyPrivacyScanV2(ctx sdk.Context, sequence uint64, height int64, txHashHex, eventType string, attrs []sdk.Attribute) (*types.PrivacyScanSummaryV2, []*types.PrivacyScanOutputV2, error) {
	attrMap := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		attrMap[attr.Key] = strings.Trim(attr.Value, "\"")
	}
	txHash, err := decodeOptionalHexAttribute(map[string]string{"tx_hash": txHashHex}, "tx_hash")
	if err != nil {
		return nil, nil, err
	}
	summary := &types.PrivacyScanSummaryV2{
		GlobalSequence:    sequence,
		Height:            height,
		TxHash:            txHash,
		EventType:         eventType,
		CircuitSetId:      types.ActiveCircuitSetID,
		PayloadVersion:    types.FixedPayloadVersionV1,
		ScanSchemaVersion: types.PrivacyScanSchemaVersionV2,
	}

	newOutput := func(index uint32, commitment []byte) (*types.PrivacyScanOutputV2, error) {
		canonicalCommitment, err := validateFieldElementBytesStrict(commitment)
		if err != nil {
			return nil, err
		}
		leafIndex, found, err := k.GetCommitmentIndex(ctx, canonicalCommitment)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("privacy scan commitment is missing from commitment index")
		}
		return &types.PrivacyScanOutputV2{
			GlobalSequence:    sequence,
			Height:            height,
			OutputIndex:       index,
			Commitment:        canonicalCommitment,
			LeafIndex:         leafIndex,
			LeafIndexFound:    true,
			CircuitSetId:      summary.CircuitSetId,
			PayloadVersion:    summary.PayloadVersion,
			ScanSchemaVersion: summary.ScanSchemaVersion,
			TxHash:            append([]byte(nil), txHash...),
			EventType:         eventType,
		}, nil
	}

	switch eventType {
	case types.EventTypeDeposit:
		commitment, err := decodeRequiredHexAttribute(attrMap, types.AttributeKeyCommitment)
		if err != nil {
			return nil, nil, err
		}
		output, err := newOutput(0, commitment)
		if err != nil {
			return nil, nil, err
		}
		output.EncryptedNote, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyEncryptedNote)
		if err != nil {
			return nil, nil, err
		}
		summary.OutputCount = 1
		return summary, []*types.PrivacyScanOutputV2{output}, nil

	case types.EventTypeShieldedTransfer:
		outputs := make([]*types.PrivacyScanOutputV2, 2)
		commitmentKeys := []string{types.AttributeKeyCommitment1, types.AttributeKeyCommitment2}
		ciphertextKeys := []string{types.AttributeKeyCipherText1, types.AttributeKeyCipherText2}
		viewTagKeys := []string{types.AttributeKeyViewTag1, types.AttributeKeyViewTag2}
		for i := range outputs {
			commitment, err := decodeRequiredHexAttribute(attrMap, commitmentKeys[i])
			if err != nil {
				return nil, nil, err
			}
			outputs[i], err = newOutput(uint32(i), commitment)
			if err != nil {
				return nil, nil, err
			}
			outputs[i].Ciphertext, err = decodeOptionalHexAttribute(attrMap, ciphertextKeys[i])
			if err != nil {
				return nil, nil, err
			}
			outputs[i].ViewTag, err = decodeOptionalHexAttribute(attrMap, viewTagKeys[i])
			if err != nil {
				return nil, nil, err
			}
		}
		for _, key := range []string{types.AttributeKeyNullifier1, types.AttributeKeyNullifier2} {
			nullifier, err := decodeRequiredHexAttribute(attrMap, key)
			if err != nil {
				return nil, nil, err
			}
			canonicalNullifier, err := validateFieldElementBytesStrict(nullifier)
			if err != nil {
				return nil, nil, err
			}
			summary.Nullifiers = append(summary.Nullifiers, canonicalNullifier)
		}
		outputs[0].UserPrivacyPolicy, err = parseOptionalUint32Attribute(attrMap, types.AttributeKeyUserPrivacyPolicy)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].UserDisclosureMode = attrMap[types.AttributeKeyUserDisclosureMode]
		outputs[0].UserDisclosureDigest, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyUserDisclosureDigest)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].UserDisclosureTargetPubkey, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyUserDisclosureTargetPubKey)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].UserDisclosurePayload, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyUserDisclosurePayload)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].FullDisclosureDigest, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyAuditDisclosureDigest)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].AuditDisclosurePayload, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyAuditDisclosurePayload)
		if err != nil {
			return nil, nil, err
		}
		outputs[0].SelfViewDisclosurePayload, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeySelfViewDisclosurePayload)
		if err != nil {
			return nil, nil, err
		}
		summary.AuditTargetPubkey, err = decodeOptionalHexAttribute(attrMap, types.AttributeKeyAuditDisclosureTargetPubKey)
		if err != nil {
			return nil, nil, err
		}
		for _, output := range outputs {
			output.AuditTargetPubkey = append([]byte(nil), summary.AuditTargetPubkey...)
		}
		summary.OutputCount = uint32(len(outputs))
		return summary, outputs, nil

	case types.EventTypeWithdraw:
		nullifier, err := decodeRequiredHexAttribute(attrMap, types.AttributeKeyNullifier)
		if err != nil {
			return nil, nil, err
		}
		canonicalNullifier, err := validateFieldElementBytesStrict(nullifier)
		if err != nil {
			return nil, nil, err
		}
		summary.Nullifiers = [][]byte{canonicalNullifier}
		return summary, nil, nil
	default:
		return nil, nil, fmt.Errorf("unsupported privacy scan event type %q", eventType)
	}
}

func (k Keeper) getPrivacyScanSummaryV2(ctx sdk.Context, height int64, sequence uint64) (*types.PrivacyScanSummaryV2, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.GetPrivacyScanSummaryKey(height, sequence))
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, fmt.Errorf("privacy scan summary is missing")
	}
	var summary types.PrivacyScanSummaryV2
	if err := summary.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("privacy scan summary is corrupt: %w", err)
	}
	if summary.Height != height || summary.GlobalSequence != sequence {
		return nil, fmt.Errorf("privacy scan summary key/body mismatch")
	}
	if err := validatePrivacyScanSummaryV2(&summary); err != nil {
		return nil, err
	}
	return &summary, nil
}

func decodePrivacyScanOutputKey(key []byte) (*types.PrivacyScanCursorV1, error) {
	if len(key) != 21 || key[0] != types.KeyPrefixScanOutput {
		return nil, fmt.Errorf("privacy scan output key is malformed")
	}
	height := binary.BigEndian.Uint64(key[1:9])
	if height > math.MaxInt64 {
		return nil, fmt.Errorf("privacy scan output height overflows int64")
	}
	return &types.PrivacyScanCursorV1{
		Height:         int64(height),
		GlobalSequence: binary.BigEndian.Uint64(key[9:17]),
		OutputIndex:    binary.BigEndian.Uint32(key[17:21]),
	}, nil
}

func decodePrivacyScanSummaryKey(key []byte) (int64, uint64, error) {
	if len(key) != 17 || key[0] != types.KeyPrefixScanSummary {
		return 0, 0, fmt.Errorf("privacy scan summary key is malformed")
	}
	height := binary.BigEndian.Uint64(key[1:9])
	if height > math.MaxInt64 {
		return 0, 0, fmt.Errorf("privacy scan summary height overflows int64")
	}
	return int64(height), binary.BigEndian.Uint64(key[9:17]), nil
}

func normalizePrivacyScanLimits(outputLimit, eventLimit uint32, byteLimit uint64) (uint32, uint32, uint64, error) {
	if outputLimit == 0 {
		outputLimit = DefaultPrivacyScanOutputLimit
	}
	if eventLimit == 0 {
		eventLimit = DefaultPrivacyScanEventLimit
	}
	if byteLimit == 0 {
		byteLimit = DefaultPrivacyScanByteLimit
	}
	if outputLimit > MaxPrivacyScanOutputLimit {
		return 0, 0, 0, fmt.Errorf("privacy scan output_limit exceeds %d", MaxPrivacyScanOutputLimit)
	}
	if eventLimit > MaxPrivacyScanEventLimit {
		return 0, 0, 0, fmt.Errorf("privacy scan event_limit exceeds %d", MaxPrivacyScanEventLimit)
	}
	if byteLimit > MaxPrivacyScanByteLimit {
		return 0, 0, 0, fmt.Errorf("privacy scan max_encoded_bytes exceeds %d", MaxPrivacyScanByteLimit)
	}
	return outputLimit, eventLimit, byteLimit, nil
}

func (k Keeper) GetPrivacyScanPageV2(ctx sdk.Context, after *types.PrivacyScanCursorV1, outputLimit, eventLimit uint32, byteLimit uint64, eventTypes []string) (*types.QueryPrivacyScanResponse, error) {
	if after == nil {
		after = &types.PrivacyScanCursorV1{}
	}
	if err := validatePrivacyScanCursor(after); err != nil {
		return nil, err
	}
	outputLimit, eventLimit, byteLimit, err := normalizePrivacyScanLimits(outputLimit, eventLimit, byteLimit)
	if err != nil {
		return nil, err
	}
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.GetPrivacyScanSummaryPrefix()
	start := types.GetPrivacyScanSummaryKey(after.Height, after.GlobalSequence)
	iterator, err := store.Iterator(start, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()

	response := &types.QueryPrivacyScanResponse{
		NextCursor:        &types.PrivacyScanCursorV1{Height: after.Height, GlobalSequence: after.GlobalSequence, OutputIndex: after.OutputIndex},
		OutputLimit:       outputLimit,
		EventLimit:        eventLimit,
		MaxEncodedBytes:   byteLimit,
		ScanSchemaVersion: types.PrivacyScanSchemaVersionV2,
	}
	typeFilter := normalizePrivacyEventTypes(eventTypes)

	for ; iterator.Valid(); iterator.Next() {
		height, sequence, err := decodePrivacyScanSummaryKey(iterator.Key())
		if err != nil {
			return nil, err
		}
		if height < after.Height || (height == after.Height && sequence < after.GlobalSequence) {
			continue
		}

		var summary types.PrivacyScanSummaryV2
		if err := summary.Unmarshal(iterator.Value()); err != nil {
			return nil, fmt.Errorf("privacy scan summary is corrupt: %w", err)
		}
		if summary.Height != height || summary.GlobalSequence != sequence {
			return nil, fmt.Errorf("privacy scan summary key/body mismatch")
		}
		if err := validatePrivacyScanSummaryV2(&summary); err != nil {
			return nil, err
		}
		eventOutputs, err := k.getPrivacyScanEventOutputsV2(ctx, &summary)
		if err != nil {
			return nil, err
		}

		sameEventAsCursor := height == after.Height && sequence == after.GlobalSequence
		startOutput := uint32(0)
		if sameEventAsCursor {
			if summary.OutputCount > 0 && after.OutputIndex >= summary.OutputCount {
				return nil, fmt.Errorf("privacy scan cursor output_index exceeds event output_count")
			}
			if summary.OutputCount == 0 || after.OutputIndex >= summary.OutputCount-1 {
				continue
			}
			startOutput = after.OutputIndex + 1
		}

		if response.ScannedEventCount == eventLimit || uint32(len(response.Outputs)) == outputLimit {
			response.HasMore = true
			break
		}
		response.ScannedEventCount++

		allowed := privacyEventTypeAllowed(summary.EventType, typeFilter)
		if !allowed {
			// Keep pages that already returned event evidence pinned to their last
			// visible cursor. The filtered event is consumed on the next page, so a
			// client never has to accept an unproven cursor jump past outputs.
			if len(response.Outputs) > 0 || len(response.Summaries) > 0 {
				response.ScannedEventCount--
				response.HasMore = true
				break
			}
			cursorOutput := uint32(0)
			if summary.OutputCount > 0 {
				cursorOutput = summary.OutputCount - 1
			}
			response.NextCursor = &types.PrivacyScanCursorV1{Height: height, GlobalSequence: sequence, OutputIndex: cursorOutput}
			continue
		}

		summaryBytes := uint64(proto.Size(&summary))
		if response.EncodedBytes+summaryBytes > byteLimit {
			if len(response.Summaries) == 0 && len(response.Outputs) == 0 {
				return nil, errPrivacyScanRecordExceedsBudget
			}
			response.HasMore = true
			break
		}
		summaryCopy := summary
		response.Summaries = append(response.Summaries, &summaryCopy)
		response.EncodedBytes += summaryBytes

		if summary.OutputCount == 0 {
			response.NextCursor = &types.PrivacyScanCursorV1{Height: height, GlobalSequence: sequence, OutputIndex: 0}
			continue
		}

		for outputIndex := startOutput; outputIndex < summary.OutputCount; outputIndex++ {
			if uint32(len(response.Outputs)) == outputLimit {
				response.HasMore = true
				return response, nil
			}
			output := eventOutputs[outputIndex]
			outputBytes := uint64(proto.Size(output))
			if response.EncodedBytes+outputBytes > byteLimit {
				if len(response.Outputs) == 0 {
					return nil, errPrivacyScanRecordExceedsBudget
				}
				response.HasMore = true
				return response, nil
			}
			response.Outputs = append(response.Outputs, output)
			response.EncodedBytes += outputBytes
			response.NextCursor = scanOutputCursor(output)
		}
	}
	return response, nil
}

func (k Keeper) getPrivacyScanOutputV2(ctx sdk.Context, summary *types.PrivacyScanSummaryV2, outputIndex uint32) (*types.PrivacyScanOutputV2, error) {
	bz, err := k.storeService.OpenKVStore(ctx).Get(types.GetPrivacyScanOutputKey(summary.Height, summary.GlobalSequence, outputIndex))
	if err != nil {
		return nil, err
	}
	if len(bz) == 0 {
		return nil, fmt.Errorf("privacy scan output %d is missing", outputIndex)
	}
	var output types.PrivacyScanOutputV2
	if err := output.Unmarshal(bz); err != nil {
		return nil, fmt.Errorf("privacy scan output is corrupt: %w", err)
	}
	if err := k.validatePrivacyScanOutputStateV2(ctx, summary, &output, outputIndex); err != nil {
		return nil, fmt.Errorf("privacy scan output %d does not match summary/state: %w", outputIndex, err)
	}
	return &output, nil
}

func (k Keeper) ExportGenesisPrivacyEventsV1(ctx sdk.Context) ([]*types.PrivacyEventRecordV1, error) {
	store := k.storeService.OpenKVStore(ctx)
	prefix := types.GetPrivacyEventPrefix()
	iterator, err := store.Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	if err != nil {
		return nil, err
	}
	defer iterator.Close()
	records := make([]*types.PrivacyEventRecordV1, 0)
	for ; iterator.Valid(); iterator.Next() {
		key := iterator.Key()
		if len(key) != 17 || key[0] != types.KeyPrefixPrivacyEvent {
			return nil, fmt.Errorf("privacy event index key is malformed")
		}
		var event types.QueryPrivacyEvent
		if err := event.Unmarshal(iterator.Value()); err != nil {
			return nil, fmt.Errorf("privacy event index is corrupt: %w", err)
		}
		keyHeight := binary.BigEndian.Uint64(key[1:9])
		if keyHeight > math.MaxInt64 || event.Height != int64(keyHeight) || event.Sequence != binary.BigEndian.Uint64(key[9:17]) {
			return nil, fmt.Errorf("privacy event index key/body mismatch")
		}
		txHash, err := hex.DecodeString(event.TxHashHex)
		if err != nil {
			return nil, fmt.Errorf("privacy event tx hash is corrupt: %w", err)
		}
		record := &types.PrivacyEventRecordV1{
			GlobalSequence: event.Sequence,
			Height:         event.Height,
			TxHash:         txHash,
			EventType:      event.EventType,
			Attributes:     make([]*types.PrivacyEventAttributeV1, 0, len(event.Attributes)),
		}
		for _, attr := range event.Attributes {
			if attr == nil {
				return nil, fmt.Errorf("privacy event contains nil attribute")
			}
			record.Attributes = append(record.Attributes, &types.PrivacyEventAttributeV1{Key: attr.Key, Value: attr.Value})
		}
		records = append(records, record)
	}
	return records, nil
}

func (k Keeper) ExportGenesisPrivacyScanV2(ctx sdk.Context) ([]*types.PrivacyScanSummaryV2, []*types.PrivacyScanOutputV2, error) {
	store := k.storeService.OpenKVStore(ctx)
	summaryPrefix := types.GetPrivacyScanSummaryPrefix()
	summaryIterator, err := store.Iterator(summaryPrefix, storetypes.PrefixEndBytes(summaryPrefix))
	if err != nil {
		return nil, nil, err
	}
	defer summaryIterator.Close()
	summaries := make([]*types.PrivacyScanSummaryV2, 0)
	summaryByEvent := make(map[string]*types.PrivacyScanSummaryV2)
	for ; summaryIterator.Valid(); summaryIterator.Next() {
		height, sequence, err := decodePrivacyScanSummaryKey(summaryIterator.Key())
		if err != nil {
			return nil, nil, err
		}
		var summary types.PrivacyScanSummaryV2
		if err := summary.Unmarshal(summaryIterator.Value()); err != nil {
			return nil, nil, err
		}
		if err := validatePrivacyScanSummaryV2(&summary); err != nil {
			return nil, nil, err
		}
		if summary.Height != height || summary.GlobalSequence != sequence {
			return nil, nil, fmt.Errorf("privacy scan summary key/body mismatch")
		}
		sequenceHeight, err := store.Get(types.GetPrivacyScanSequenceKey(sequence))
		if err != nil {
			return nil, nil, err
		}
		if len(sequenceHeight) != 8 || binary.BigEndian.Uint64(sequenceHeight) != uint64(height) {
			return nil, nil, fmt.Errorf("privacy scan sequence index is missing or inconsistent")
		}
		summaryCopy := summary
		summaries = append(summaries, &summaryCopy)
		summaryByEvent[fmt.Sprintf("%d/%d", height, sequence)] = &summaryCopy
	}

	outputPrefix := types.GetPrivacyScanOutputPrefix()
	outputIterator, err := store.Iterator(outputPrefix, storetypes.PrefixEndBytes(outputPrefix))
	if err != nil {
		return nil, nil, err
	}
	defer outputIterator.Close()
	outputs := make([]*types.PrivacyScanOutputV2, 0)
	outputCounts := make(map[string]uint32, len(summaryByEvent))
	for ; outputIterator.Valid(); outputIterator.Next() {
		keyCursor, err := decodePrivacyScanOutputKey(outputIterator.Key())
		if err != nil {
			return nil, nil, err
		}
		var output types.PrivacyScanOutputV2
		if err := output.Unmarshal(outputIterator.Value()); err != nil {
			return nil, nil, err
		}
		if comparePrivacyScanCursor(keyCursor, scanOutputCursor(&output)) != 0 {
			return nil, nil, fmt.Errorf("privacy scan output key/body mismatch")
		}
		eventKey := fmt.Sprintf("%d/%d", output.Height, output.GlobalSequence)
		summary, found := summaryByEvent[eventKey]
		if !found {
			return nil, nil, fmt.Errorf("privacy scan output summary is missing")
		}
		if err := k.validatePrivacyScanOutputStateV2(ctx, summary, &output, outputCounts[eventKey]); err != nil {
			return nil, nil, err
		}
		outputCounts[eventKey]++
		outputCopy := output
		outputs = append(outputs, &outputCopy)
	}
	for eventKey, summary := range summaryByEvent {
		if outputCounts[eventKey] != summary.OutputCount {
			return nil, nil, fmt.Errorf("privacy scan summary has incomplete output records")
		}
	}
	return summaries, outputs, nil
}

func (k Keeper) InitGenesisPrivacyIndexV2(ctx sdk.Context, globalSequence uint64, events []*types.PrivacyEventRecordV1, summaries []*types.PrivacyScanSummaryV2, outputs []*types.PrivacyScanOutputV2) error {
	store := k.storeService.OpenKVStore(ctx)
	maxSequence := uint64(0)
	for i, event := range events {
		if event == nil || event.Height < 0 || event.GlobalSequence == 0 || strings.TrimSpace(event.EventType) == "" {
			return fmt.Errorf("genesis privacy event %d is invalid", i)
		}
		if event.GlobalSequence > maxSequence {
			maxSequence = event.GlobalSequence
		}
		queryEvent := &types.QueryPrivacyEvent{
			Sequence:   event.GlobalSequence,
			Height:     event.Height,
			TxHashHex:  strings.ToUpper(hex.EncodeToString(event.TxHash)),
			EventType:  event.EventType,
			Attributes: make([]*types.QueryPrivacyEventAttribute, 0, len(event.Attributes)),
		}
		for _, attr := range event.Attributes {
			if attr == nil {
				return fmt.Errorf("genesis privacy event %d contains nil attribute", i)
			}
			queryEvent.Attributes = append(queryEvent.Attributes, &types.QueryPrivacyEventAttribute{Key: attr.Key, Value: attr.Value})
		}
		key := types.GetPrivacyEventKey(event.Height, event.GlobalSequence)
		exists, err := store.Has(key)
		if err != nil {
			return err
		}
		if exists {
			return fmt.Errorf("genesis privacy event %d duplicates cursor", i)
		}
		encoded, err := queryEvent.Marshal()
		if err != nil {
			return err
		}
		if err := store.Set(key, encoded); err != nil {
			return err
		}
	}

	outputsByEvent := make(map[string][]*types.PrivacyScanOutputV2)
	for i, output := range outputs {
		if output == nil {
			return fmt.Errorf("genesis privacy scan output %d is nil", i)
		}
		key := fmt.Sprintf("%d/%d", output.Height, output.GlobalSequence)
		outputsByEvent[key] = append(outputsByEvent[key], output)
	}
	for i, summary := range summaries {
		if summary == nil {
			return fmt.Errorf("genesis privacy scan summary %d is nil", i)
		}
		key := fmt.Sprintf("%d/%d", summary.Height, summary.GlobalSequence)
		if err := k.StorePrivacyScanV2(ctx, summary, outputsByEvent[key]); err != nil {
			return fmt.Errorf("genesis privacy scan summary %d: %w", i, err)
		}
		delete(outputsByEvent, key)
		if summary.GlobalSequence > maxSequence {
			maxSequence = summary.GlobalSequence
		}
	}
	if len(outputsByEvent) != 0 {
		return fmt.Errorf("genesis privacy scan contains output without summary")
	}
	if globalSequence < maxSequence {
		return fmt.Errorf("genesis privacy global sequence is behind indexed records")
	}
	return k.setPrivacyGlobalSequence(ctx, globalSequence)
}
