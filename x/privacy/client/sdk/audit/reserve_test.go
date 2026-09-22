package audit

import (
	"math/big"
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

func TestEvaluateReserveUnboundedCounters(t *testing.T) {
	huge := new(big.Int).Exp(big.NewInt(10), big.NewInt(78), nil)
	response := &privacytypes.QueryReserveResponse{
		Denom: "uclair", ModuleBalance: "1", TotalDeposited: huge.String(),
		TotalWithdrawn:        new(big.Int).Sub(huge, big.NewInt(1)).String(),
		ExpectedModuleBalance: "1", Liability: "1", InvariantHolds: true, Collateralized: true, Surplus: "0", Shortfall: "0",
	}
	result, err := EvaluateReserve(response)
	require.NoError(t, err)
	require.Equal(t, response.TotalDeposited, result.TotalDeposited)
	require.Equal(t, response.TotalWithdrawn, result.TotalWithdrawn)
	require.Equal(t, "1", result.Liability)
	for _, change := range []func(*privacytypes.QueryReserveResponse){
		func(r *privacytypes.QueryReserveResponse) { r.TotalDeposited = "0" },
		func(r *privacytypes.QueryReserveResponse) { r.TotalDeposited = "0" + r.TotalDeposited },
		func(r *privacytypes.QueryReserveResponse) { r.TotalWithdrawn = "+0" },
		func(r *privacytypes.QueryReserveResponse) { r.ModuleBalance = huge.String() },
		func(r *privacytypes.QueryReserveResponse) {
			r.TotalWithdrawn = "0"
			r.ExpectedModuleBalance, r.Liability = huge.String(), huge.String()
		},
	} {
		invalid := *response
		change(&invalid)
		_, err := EvaluateReserve(&invalid)
		require.Error(t, err)
	}
}
