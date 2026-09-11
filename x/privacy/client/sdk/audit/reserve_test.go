package audit

import (
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestEvaluateReserveSeparatesSurplusExactAndShortfall(t *testing.T) {
	surplus, err := EvaluateReserve(&privacytypes.QueryReserveResponse{
		Denom: "uclair", ModuleBalance: "10", TotalDeposited: "7", TotalWithdrawn: "2", ExpectedModuleBalance: "5",
		Liability: "5", InvariantHolds: false, Collateralized: true, Surplus: "5", Shortfall: "0",
	})
	require.NoError(t, err)
	require.True(t, surplus.Collateralized)
	require.False(t, surplus.ExactMatch)
	require.Equal(t, "5", surplus.Surplus)
	require.False(t, surplus.UnderCollateralized())

	shortfall, err := EvaluateReserve(&privacytypes.QueryReserveResponse{
		Denom: "uclair", ModuleBalance: "1", TotalDeposited: "7", TotalWithdrawn: "2", ExpectedModuleBalance: "5",
		Liability: "5", InvariantHolds: false, Collateralized: false, Surplus: "0", Shortfall: "4",
	})
	require.NoError(t, err)
	require.True(t, shortfall.UnderCollateralized())
	require.Equal(t, "4", shortfall.Shortfall)
}

func TestEvaluateReserveRejectsInconsistentArithmetic(t *testing.T) {
	_, err := EvaluateReserve(&privacytypes.QueryReserveResponse{
		Denom: "uclair", ModuleBalance: "5", TotalDeposited: "7", TotalWithdrawn: "2", ExpectedModuleBalance: "5",
		Liability: "5", InvariantHolds: true, Collateralized: true, Surplus: "1", Shortfall: "0",
	})
	require.Error(t, err)
}
