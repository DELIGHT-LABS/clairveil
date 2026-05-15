package keeper

import (
	"context"
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestAuditConfigQueryReturnsHexEncodedPubKey(t *testing.T) {
	k, ctx, _ := setupMsgServerKeeper()

	pubKey := fixedFieldBytes(42)
	k.SetAuditMasterPubkey(ctx, pubKey)

	resp, err := k.AuditConfig(sdk.WrapSDKContext(ctx), &privacytypes.QueryAuditConfigRequest{})
	require.NoError(t, err)
	require.Equal(t, hex.EncodeToString(pubKey), resp.AuditMasterPubkeyHex)
}

func TestAuditConfigQueryRejectsNilRequest(t *testing.T) {
	k, _, _ := setupMsgServerKeeper()

	resp, err := k.AuditConfig(context.Background(), nil)
	require.Nil(t, resp)
	require.Error(t, err)
}
