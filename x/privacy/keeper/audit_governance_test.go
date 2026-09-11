package keeper

import (
	"testing"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/stretchr/testify/require"
)

func TestAuditHandlerRejectsDirectGovernanceAccountExecution(t *testing.T) {
	_, ctx, _ := setupMsgServerKeeper()
	ctx = ctx.WithTxBytes([]byte{1})
	envelopeSize, err := auditfield.KindDeposit.EnvelopeSize()
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{
		Creator: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
		Amount:  "1uclair",
		Output: &privacyv2.OutputEffect{
			Commitment: auditfield.Field32FromUint64(1).Bytes(),
			Ciphertext: testKeeperEnvelope(t, privacytypes.EnvelopeDepositNoteV1),
		},
		Proof:         make([]byte, 164),
		ExpiresAtUnix: 1,
		Audit:         &privacyv2.AuditAuthorization{KeyId: make([]byte, 32), Epoch: 1, Envelope: make([]byte, envelopeSize)},
	}
	server := auditMsgServer{Keeper{audit: &auditRuntime{}}}
	_, err = server.Deposit(sdk.WrapSDKContext(ctx), message)
	require.ErrorContains(t, err, "governance cannot execute privacy transactions")
}
