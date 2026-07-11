package keeper

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestAuditConfigV1RoundTripAndClear(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	target := testKeeperDisclosurePubKey()

	config, found, err := k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, privacytypes.AuditConfigV1{}, config)

	require.NoError(t, k.SetAuditConfigV1(ctx, "audit.production-1", 7, target))
	config, found, err = k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "audit.production-1", config.AuditKeyID)
	require.Equal(t, uint64(7), config.AuditKeyEpoch)
	require.Equal(t, target, config.AuditTargetPubkey)

	config.AuditTargetPubkey[0] ^= 0xff
	require.Equal(t, target, k.GetAuditMasterPubkey(ctx))

	require.NoError(t, k.SetAuditConfigV1(ctx, "", 0, nil))
	config, found, err = k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.False(t, found)
	require.Equal(t, privacytypes.AuditConfigV1{}, config)
	require.Nil(t, k.GetAuditMasterPubkey(ctx))
}

func TestAuditConfigV1SetterRejectsNonExactConfigWithoutWrites(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	target := testKeeperDisclosurePubKey()

	for _, tc := range []struct {
		name   string
		id     string
		epoch  uint64
		target []byte
	}{
		{name: "missing id", epoch: 1, target: target},
		{name: "invalid id", id: "Master", epoch: 1, target: target},
		{name: "missing epoch", id: "master", target: target},
		{name: "missing target", id: "master", epoch: 1},
		{name: "invalid target", id: "master", epoch: 1, target: make([]byte, 32)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Error(t, k.SetAuditConfigV1(ctx, tc.id, tc.epoch, tc.target))
			_, found, err := k.GetAuditConfigV1(ctx)
			require.NoError(t, err)
			require.False(t, found)
		})
	}
}

func TestAuditConfigV1RejectedUpdatePreservesPreviousExactConfig(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	target := testKeeperDisclosurePubKey()
	require.NoError(t, k.SetAuditConfigV1(ctx, "master", 1, target))

	require.Error(t, k.SetAuditConfigV1(ctx, "Master", 2, target))
	config, found, err := k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "master", config.AuditKeyID)
	require.Equal(t, uint64(1), config.AuditKeyEpoch)
	require.Equal(t, target, config.AuditTargetPubkey)
}

func TestAuditConfigV1GetterFailsClosedOnPartialOrMalformedState(t *testing.T) {
	target := testKeeperDisclosurePubKey()
	epoch := make([]byte, auditKeyEpochByteSizeV1)
	binary.BigEndian.PutUint64(epoch, 1)

	for _, tc := range []struct {
		name   string
		target []byte
		id     []byte
		epoch  []byte
		want   string
	}{
		{name: "target only", target: target, want: "partial"},
		{name: "id only", id: []byte("master"), want: "partial"},
		{name: "epoch only", epoch: epoch, want: "partial"},
		{name: "missing target", id: []byte("master"), epoch: epoch, want: "partial"},
		{name: "invalid id", target: target, id: []byte("Master"), epoch: epoch, want: "audit_key_id"},
		{name: "short epoch", target: target, id: []byte("master"), epoch: []byte{1}, want: "exactly 8 bytes"},
		{name: "zero epoch", target: target, id: []byte("master"), epoch: make([]byte, 8), want: "positive audit_key_epoch"},
		{name: "invalid target", target: make([]byte, 32), id: []byte("master"), epoch: epoch, want: "audit_master_pubkey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			k, ctx, _ := setupMsgServerKeeper()
			store := k.storeService.OpenKVStore(ctx)
			if tc.target != nil {
				require.NoError(t, store.Set(privacytypes.GetAuditConfigKey(), tc.target))
			}
			if tc.id != nil {
				require.NoError(t, store.Set(privacytypes.GetAuditKeyIDKey(), tc.id))
			}
			if tc.epoch != nil {
				require.NoError(t, store.Set(privacytypes.GetAuditKeyEpochKey(), tc.epoch))
			}

			config, found, err := k.GetAuditConfigV1(ctx)
			require.ErrorContains(t, err, tc.want)
			require.False(t, found)
			require.Equal(t, privacytypes.AuditConfigV1{}, config)
			require.Nil(t, k.GetAuditMasterPubkey(ctx))
		})
	}
}

func TestLegacyAuditMasterSetterAlsoSetsDefaultIdentity(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()
	target := testKeeperDisclosurePubKey()

	k.SetAuditMasterPubkey(ctx, target)
	require.Equal(t, target, k.GetAuditMasterPubkey(ctx))
	config, found, err := k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, privacytypes.DefaultAuditKeyIDV1, config.AuditKeyID)
	require.Equal(t, privacytypes.DefaultAuditKeyEpochV1, config.AuditKeyEpoch)
	require.Equal(t, target, config.AuditTargetPubkey)
}

func TestLegacyAuditMasterSetterFailsClosedOnNonCanonicalTarget(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	k.SetAuditMasterPubkey(ctx, make([]byte, 32))
	require.Nil(t, k.GetAuditMasterPubkey(ctx))
	_, found, err := k.GetAuditConfigV1(ctx)
	require.NoError(t, err)
	require.False(t, found)
}
