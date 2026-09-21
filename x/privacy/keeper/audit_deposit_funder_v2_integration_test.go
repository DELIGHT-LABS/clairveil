package keeper

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
)

func TestDepositWithFunderV2VerifiesRealAuditProofAndApplies(t *testing.T) {
	artifactDir := os.Getenv("CLAIRVEIL_P3_ARTIFACT_DIR")
	if artifactDir == "" {
		t.Skip("set CLAIRVEIL_P3_ARTIFACT_DIR to the verified P3 development bundle")
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{
		ArtifactDir: artifactDir, CircuitSetID: privacyzk.AuditFieldCircuitSetID,
		RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment,
	})
	require.NoError(t, err)
	identity, err := registry.LocalCircuitSetIdentity()
	require.NoError(t, err)

	k, ctx, bank := setupCacheAwareDepositKeeper(t)
	ctx = ctx.WithChainID("clairveil-p5-development")
	var nonce [32]byte
	nonce[0] = 0x49
	require.NoError(t, k.ConfigureAuditRuntime(AuditRuntimeConfig{
		PrincipalAdapter: auditApplyTestPrincipal{bank: bank}, ArtifactDir: artifactDir,
		ExpectedIdentity: identity, NetworkNonce: nonce, InitialHeight: 1,
		Authority: authtypes.NewModuleAddress(govtypes.ModuleName).String(),
	}))

	decode := func(encoded string, size int) []byte {
		t.Helper()
		decoded, decodeErr := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, decodeErr)
		require.Len(t, decoded, size)
		return decoded
	}
	keyIDBytes := decode("PDxmLPVCB9uK0PJHUdRJHBKqtbFcxUQFa8NKvNyC0Zc=", 32)
	publicKey := decode("CKw261b6Iahyi10lNL4pynnAvuPlxWiL8saX2/p16icY2Sv0CvGvQuzSmkWO0RHa5Su6wZt808TL9mgjYUvLCw==", 64)
	pop := decode("IiYE/+jZfJS3C7f8faG734qbo2549tChQW4WZRdVcioa/7H0gJC84+WZsVhntpL9Pk0sxnYA+rOL4RC6MJDIvAV/FxNddW8g6dA0mwre9vw8v0nqPMMqhcfb59HN0bYq", 96)
	var keyID [32]byte
	copy(keyID[:], keyIDBytes)
	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	require.Equal(t, "01a9bb262eff2181f209bd1e03fe03117948aa7dd397df15d9a4876743a8c619", hex.EncodeToString(network[:]))
	keyRecord := &auditKeyRecord{
		Epoch: 1, KeyID: keyID, Suite: 2, PK: publicKey, PoP: pop,
		ActivationHeight: 0, RegisteredHeight: 0, Origin: auditOrigin{Kind: 3},
	}
	encodedKey, err := encodeAuditKey(keyRecord, network, 1)
	require.NoError(t, err)
	store := k.storeService.OpenKVStore(ctx)
	require.NoError(t, store.Set(auditEpochKey(auditEpochPrefix, 1), encodedKey))
	active := make([]byte, 18)
	binary.BigEndian.PutUint16(active, 1)
	binary.BigEndian.PutUint64(active[2:10], 1)
	require.NoError(t, store.Set([]byte{auditActivePrefix}, active))

	// This public fixture is the native deposit from tx
	// 2C222E76AA40AE62841DAAB0D96C33D04AE18821E43E6DD48CC434DA58F886D7
	// at height 15. Its public config and proof match the reviewed P3 bundle.
	fixture, err := os.ReadFile("testdata/audit_v2_deposit_message.b64")
	require.NoError(t, err)
	messageBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixture)))
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{}
	require.NoError(t, message.Unmarshal(messageBytes))
	creatorBytes, err := sdk.GetFromBech32(message.Creator, "clair")
	require.NoError(t, err)
	creator := sdk.AccAddress(creatorBytes)
	// Address bytes are the proof target; use this test process's configured
	// prefix without changing the global SDK address configuration.
	message.Creator = creator.String()
	funder := sdk.AccAddress(make([]byte, 20))
	for i := range funder {
		funder[i] = 0x7a
	}
	module := authtypes.NewModuleAddress(privacytypes.ModuleName)
	bank.setTestBalance(t, ctx, creator, "uclair", 50)
	bank.setTestBalance(t, ctx, funder, "uclair", 30)
	bank.setTestBalance(t, ctx, module, "uclair", 20)
	require.NoError(t, k.recordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 14)))
	reserveBefore, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)

	invalid := proto.Clone(message).(*privacyv2.MsgDeposit)
	invalid.Proof[0] ^= 1
	_, err = k.DepositWithFunderV2(ctx, invalid, funder)
	require.ErrorContains(t, err, "audit proof verification failed")
	require.Zero(t, bank.fromAccountToModuleCalls)
	require.Equal(t, "30", bank.testBalance(t, ctx, funder, "uclair").String())
	require.Zero(t, k.GetLeafCount(ctx))
	sequence, err := k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.Zero(t, sequence)
	reserveAfterFailure, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, reserveBefore, reserveAfterFailure)
	require.Empty(t, ctx.EventManager().Events())

	_, err = k.DepositWithFunderV2(ctx, message, funder)
	require.NoError(t, err)
	require.Equal(t, 1, bank.fromAccountToModuleCalls)
	require.Equal(t, funder, bank.lastAccountSender)
	require.Equal(t, "50", bank.testBalance(t, ctx, creator, "uclair").String())
	require.Equal(t, "16", bank.testBalance(t, ctx, funder, "uclair").String())
	require.Equal(t, "34", bank.testBalance(t, ctx, module, "uclair").String())
	require.EqualValues(t, 1, k.GetLeafCount(ctx))
	found, err := k.HasCommitment(ctx, message.Output.Commitment)
	require.NoError(t, err)
	require.True(t, found)
	sequence, err = k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, sequence)
	reserveAfterSuccess, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "28", reserveAfterSuccess.TotalDeposited.String())
	require.Equal(t, "0", reserveAfterSuccess.TotalWithdrawn.String())
	require.True(t, reserveAfterSuccess.Collateralized)
	require.Len(t, ctx.EventManager().Events(), 1)
	attrs := map[string]string{}
	for _, attr := range ctx.EventManager().Events()[0].Attributes {
		attrs[attr.Key] = attr.Value
	}
	require.Equal(t, funder.String(), attrs[privacytypes.AttributeKeyDelegatedFunder])
	canonicalMessage, err := message.Marshal()
	require.NoError(t, err)
	require.Equal(t, base64.StdEncoding.EncodeToString(canonicalMessage), attrs[privacytypes.AttributeKeyDelegatedDepositPayload])
}
