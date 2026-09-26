package audit

import (
	"bytes"
	"testing"
	"time"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/stretchr/testify/require"
)

func TestPreparedDepositBindsSnapshotAndClearsOnArtifactChange(t *testing.T) {
	key := testAuditKey(t)
	asset := privacytypes.ComputeAssetIDV1("uclair").FillBytes(make([]byte, 32))
	assetField, err := auditfield.ParseField32(asset)
	require.NoError(t, err)
	prepared, err := Prepare(PrepareInput{
		Snapshot:      Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 7, ArtifactHash: [32]byte{9}},
		Kind:          auditfield.KindDeposit,
		Creator:       "clair1prepareddeposit",
		Amount:        "7uclair",
		ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
		Outputs:       []*privacyv2.OutputEffect{{Commitment: auditfield.Field32FromUint64(7).Bytes(), Ciphertext: testEnvelope(t, privacytypes.EnvelopeDepositNoteV1)}},
		PublicAsset:   asset,
		Principal:     bytes.Repeat([]byte{2}, 20),
		Plaintext: []auditfield.Field32{
			assetField, auditfield.Field32FromUint64(7), auditfield.Field32FromUint64(1), auditfield.Field32FromUint64(2), auditfield.Field32FromUint64(3), auditfield.Field32FromUint64(4),
		},
	})
	require.NoError(t, err)
	defer prepared.Clear()
	_, err = prepared.EnvelopeBytes()
	require.NoError(t, err)
	r, err := prepared.ProverScalarBE32()
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, r)
	clear(r[:])

	cache := NewPreparedCache()
	require.NoError(t, cache.Put("deposit", prepared))
	_, ok := cache.Take("deposit", prepared.Snapshot(), time.Now())
	require.True(t, ok)
	changed := prepared.Snapshot()
	changed.ArtifactHash[0] ^= 1
	cache.Invalidate(changed)
	_, ok = cache.Take("deposit", changed, time.Now())
	require.False(t, ok)
	_, err = prepared.EnvelopeBytes()
	require.Error(t, err)
}

func TestPrepareRejectsFunderOverrideShape(t *testing.T) {
	key := testAuditKey(t)
	_, err := Prepare(PrepareInput{
		Snapshot: Snapshot{Network: [32]byte{1}, Epoch: 1, Key: key, StateHeight: 1}, Kind: auditfield.KindDeposit,
		Creator: "clair1creator", Recipient: "clair1funder", Amount: "1uclair", ExpiresAtUnix: time.Now().Add(time.Hour).Unix(),
	})
	require.Error(t, err)
}

func testAuditKey(t *testing.T) auditfield.AuditKey {
	t.Helper()
	raw := make([]byte, 32)
	raw[31] = 7
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(raw)
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native secret profile unavailable: %v", err)
	}
	return key
}

func testEnvelope(t *testing.T, kind privacytypes.EncryptedEnvelopeKindV1) []byte {
	t.Helper()
	size, err := privacytypes.EncryptedEnvelopeV1Size(kind)
	require.NoError(t, err)
	result, err := privacytypes.WrapEncryptedEnvelopeV1(kind, make([]byte, size-privacytypes.EncryptedEnvelopeV1HeaderSize))
	require.NoError(t, err)
	return result
}

func TestAuditCoinAllowsZeroOnlyForDeposit(t *testing.T) {
	amount, err := parseAuditCoin("0uclair", auditfield.KindDeposit)
	require.NoError(t, err)
	require.Zero(t, amount)
	_, err = parseAuditCoin("0uclair", auditfield.KindWithdraw)
	require.Error(t, err)
	for _, value := range []string{"00uclair", "-1uclair", "0.0uclair", "18446744073709551616uclair", "0x"} {
		_, err := parseAuditCoin(value, auditfield.KindDeposit)
		require.Error(t, err, value)
	}
}
