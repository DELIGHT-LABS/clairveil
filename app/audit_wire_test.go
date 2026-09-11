package app

import (
	"testing"

	v2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gov "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protowire"
)

func wireBytes(n protowire.Number, b []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), b)
}
func TestAuditRawDecoderBoundaries(t *testing.T) {
	registry := types.NewInterfaceRegistry()
	v2.RegisterInterfaces(registry)
	gov.RegisterInterfaces(registry)
	staking.RegisterInterfaces(registry)
	marshal := func(m proto.Message) []byte { b, e := proto.Marshal(m); require.NoError(t, e); return b }
	msg := marshal(&v2.MsgSetPrivacyHalt{Authority: "authority", Halted: true})
	anyBytes := func(url string, b []byte) []byte { return marshal(&types.Any{TypeUrl: url, Value: b}) }
	url := "/clairveil.privacy.v2.MsgSetPrivacyHalt"
	good := anyBytes(url, msg)
	for name, bad := range map[string][]byte{
		"unknown":    append(append([]byte(nil), msg...), 0x18, 1),
		"duplicate":  append(append([]byte(nil), msg...), 0x10, 1),
		"nonminimal": append(wireBytes(1, []byte("authority")), 0x10, 0x81, 0),
		"default":    append(wireBytes(1, []byte("authority")), 0x10, 0),
		"trailing":   append(append([]byte(nil), msg...), 0),
	} {
		t.Run(name, func(t *testing.T) {

			count := 0
			require.Error(t, auditWireAny(anyBytes(url, bad), registry, 0, &count))
		})
	}
	richGov := marshal(&gov.MsgSubmitProposal{Messages: []*types.Any{{TypeUrl: url, Value: msg}}, InitialDeposit: sdk.NewCoins(sdk.NewInt64Coin("uclair", 100)), Metadata: "metadata", Title: "title", Summary: "summary"})
	richCount := 0
	require.NoError(t, auditWireAny(anyBytes("/cosmos.gov.v1.MsgSubmitProposal", richGov), registry, 0, &richCount))
	for _, blocked := range []string{
		"/clairveil.privacy.v2.MsgDeposit",
		"/clairveil.privacy.v2.MsgWithdraw",
		"/clairveil.privacy.v2.MsgTransfer",
		"/clairveil.privacy.v2.MsgBatchTransfer",
	} {
		proposal := marshal(&gov.MsgSubmitProposal{Messages: []*types.Any{{TypeUrl: blocked, Value: nil}}, InitialDeposit: sdk.NewCoins(sdk.NewInt64Coin("uclair", 100)), Title: "title", Summary: "summary"})
		count := 0
		require.ErrorContains(t, auditWireAny(anyBytes("/cosmos.gov.v1.MsgSubmitProposal", proposal), registry, 0, &count), "governance cannot execute privacy transaction")
	}
	richBad := append(append([]byte(nil), msg...), 0x18, 1)
	richGov = marshal(&gov.MsgSubmitProposal{Messages: []*types.Any{{TypeUrl: url, Value: richBad}}, InitialDeposit: sdk.NewCoins(sdk.NewInt64Coin("uclair", 100)), Title: "title", Summary: "summary"})
	richCount = 0
	require.Error(t, auditWireAny(anyBytes("/cosmos.gov.v1.MsgSubmitProposal", richGov), registry, 0, &richCount))
	richCount = 0
	require.NoError(t, auditWireAny(anyBytes("/cosmos.staking.v1beta1.MsgCreateValidator", marshal(&staking.MsgCreateValidator{})), registry, 0, &richCount))
	count := 0
	require.NoError(t, auditWireAny(good, registry, 0, &count))
	for _, raw := range [][]byte{append(append([]byte(nil), good...), wireBytes(1, []byte(url))...), anyBytes("/clairveil.privacy.v1.MsgDeposit", nil), anyBytes("/unknown.Msg", nil)} {
		count = 0
		require.Error(t, auditWireAny(raw, registry, 0, &count))
	}
	nested := good
	for i := 0; i < 16; i++ {
		nested = anyBytes("/cosmos.gov.v1.MsgSubmitProposal", wireBytes(1, nested))
	}
	count = 0
	require.NoError(t, auditWireAny(nested, registry, 0, &count))
	nested = anyBytes("/cosmos.gov.v1.MsgSubmitProposal", wireBytes(1, nested))
	count = 0
	require.Error(t, auditWireAny(nested, registry, 0, &count))
	reached := false
	decoder := auditTxDecoder(func([]byte) (sdk.Tx, error) { reached = true; return nil, nil }, registry)
	body := wireBytes(1, good)
	_, err := decoder(wireBytes(1, body))
	require.NoError(t, err)
	require.True(t, reached)
	for n := 256; n <= 257; n++ {
		body = nil
		for i := 0; i < n; i++ {
			body = append(body, wireBytes(1, good)...)
		}
		reached = false
		_, err = decoder(wireBytes(1, body))
		if n == 256 {
			require.NoError(t, err)
			require.True(t, reached)
		} else {
			require.Error(t, err)
			require.False(t, reached)
		}
	}
}
