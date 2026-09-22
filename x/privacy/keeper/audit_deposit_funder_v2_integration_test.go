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
	keyIDBytes := decode("gvfk9xCIZNE+T+J455O13IX5f3ZPnPmWmuHpY/nFqTI=", 32)
	publicKey := decode("AaSUhgcEfxTc3qW68e0BROW5WqZS/S+enWWcIchOV5UFV8S2M2HCiTm/znbFK2yuwRRXhqFpGSVvxgayzwRRHQ==", 64)
	pop := decode("Lv8yvkgce8bKootpV3cJ56ASKObFQht9T+D67mfjaucNncwleCLnLgQzyPMT1Ih6x7vEzCc++D0Q0uMGTgReiAWcAkWi3Nqu+WSZiM4nyHZc+i6r08dns0qYlldwSWxB", 96)
	var keyID [32]byte
	copy(keyID[:], keyIDBytes)
	network, err := k.auditNetwork(ctx)
	require.NoError(t, err)
	require.Equal(t, "dac0f8bbce2038e4fb543fe12fcbb7bfd21350bd12c7b6e64fe6c59b9d424022", hex.EncodeToString(network[:]))
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

	// This public fixture deposits 100 * 10^18 using the uint128 development
	// bundle. It was prepared by the native SDK and proved with the same
	// bundle used by the product circuit integration tests.
	fixture, err := os.ReadFile("testdata/audit_v2_deposit_message.b64")
	require.NoError(t, err)
	messageBytes, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(fixture)))
	require.NoError(t, err)
	message := &privacyv2.MsgDeposit{}
	require.NoError(t, message.Unmarshal(messageBytes))
	require.Equal(t, "100000000000000000000uclair", message.Amount)
	coin, err := sdk.ParseCoinNormalized(message.Amount)
	require.NoError(t, err)
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
	require.NoError(t, bank.setBalance(ctx, funder, "uclair", coin.Amount.AddRaw(16)))
	bank.setTestBalance(t, ctx, module, "uclair", 20)
	require.NoError(t, k.recordReserveDeposit(ctx, sdk.NewInt64Coin("uclair", 14)))
	reserveBefore, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)

	invalid := proto.Clone(message).(*privacyv2.MsgDeposit)
	invalid.Proof[0] ^= 1
	_, err = k.DepositWithFunderV2(ctx, invalid, funder)
	require.ErrorContains(t, err, "audit proof verification failed")
	require.Zero(t, bank.fromAccountToModuleCalls)
	require.Equal(t, "100000000000000000016", bank.testBalance(t, ctx, funder, "uclair").String())
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
	require.Equal(t, "100000000000000000020", bank.testBalance(t, ctx, module, "uclair").String())
	require.EqualValues(t, 1, k.GetLeafCount(ctx))
	found, err := k.HasCommitment(ctx, message.Output.Commitment)
	require.NoError(t, err)
	require.True(t, found)
	sequence, err = k.GetPrivacyGlobalSequence(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, sequence)
	reserveAfterSuccess, err := k.GetReserveSnapshot(ctx, "uclair")
	require.NoError(t, err)
	require.Equal(t, "100000000000000000014", reserveAfterSuccess.TotalDeposited.String())
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
