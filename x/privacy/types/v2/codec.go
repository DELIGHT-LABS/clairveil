package privacyv2

import (
	cdctypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/msgservice"
)

// RegisterInterfaces is selected only by the explicit audit-field composition.
func RegisterInterfaces(registry cdctypes.InterfaceRegistry) {
	registry.RegisterImplementations((*sdk.Msg)(nil), &MsgDeposit{}, &MsgWithdraw{}, &MsgTransfer{}, &MsgBatchTransfer{}, &MsgScheduleAuditKeyEpoch{}, &MsgCancelPendingAuditEpoch{}, &MsgSetPrivacyHalt{})
	msgservice.RegisterMsgServiceDesc(registry, &_Msg_serviceDesc)
}
