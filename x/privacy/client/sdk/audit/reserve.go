package audit

import (
	"fmt"
	"math/big"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ReserveStatus preserves all three meaningful reserve outcomes.  ExactMatch
// is diagnostic only; a positive Surplus is collateralized and acceptable.
// A positive Shortfall is a separate under-collateralization result, not an
// AUDIT_INCOMPLETE transport/replay failure.
type ReserveStatus struct {
	Denom          string
	ModuleBalance  string
	Liability      string
	TotalDeposited string
	TotalWithdrawn string
	ExactMatch     bool
	Collateralized bool
	Surplus        string
	Shortfall      string
}

func (s ReserveStatus) UnderCollateralized() bool { return !s.Collateralized }

// EvaluateReserve validates the complete Query.Reserve arithmetic snapshot.
// It intentionally accepts B>L and returns B<L as status; callers can show a
// shortage without mislabeling it as a missing-record or replay failure.
func EvaluateReserve(response *privacytypes.QueryReserveResponse) (ReserveStatus, error) {
	if response == nil || sdk.ValidateDenom(response.Denom) != nil {
		return ReserveStatus{}, fmt.Errorf("invalid reserve response denom")
	}
	values := map[string]*big.Int{}
	for name, raw := range map[string]string{
		"module balance": response.ModuleBalance, "total deposited": response.TotalDeposited,
		"total withdrawn": response.TotalWithdrawn, "expected module balance": response.ExpectedModuleBalance,
		"liability": response.Liability, "surplus": response.Surplus, "shortfall": response.Shortfall,
	} {
		value, err := parseReserveUint256(raw)
		if err != nil {
			return ReserveStatus{}, fmt.Errorf("invalid reserve %s: %w", name, err)
		}
		values[name] = value
	}
	liability := new(big.Int).Sub(values["total deposited"], values["total withdrawn"])
	if liability.Sign() < 0 || liability.Cmp(values["expected module balance"]) != 0 || liability.Cmp(values["liability"]) != 0 {
		return ReserveStatus{}, fmt.Errorf("reserve liability arithmetic mismatch")
	}
	exact := values["module balance"].Cmp(liability) == 0
	collateralized := values["module balance"].Cmp(liability) >= 0
	surplus, shortfall := new(big.Int), new(big.Int)
	if collateralized {
		surplus.Sub(values["module balance"], liability)
	} else {
		shortfall.Sub(liability, values["module balance"])
	}
	if response.InvariantHolds != exact || response.Collateralized != collateralized || values["surplus"].Cmp(surplus) != 0 || values["shortfall"].Cmp(shortfall) != 0 {
		return ReserveStatus{}, fmt.Errorf("reserve status fields do not match arithmetic")
	}
	return ReserveStatus{
		Denom: response.Denom, ModuleBalance: values["module balance"].String(), Liability: liability.String(),
		TotalDeposited: values["total deposited"].String(), TotalWithdrawn: values["total withdrawn"].String(),
		ExactMatch: exact, Collateralized: collateralized, Surplus: surplus.String(), Shortfall: shortfall.String(),
	}, nil
}

func parseReserveUint256(raw string) (*big.Int, error) {
	if raw == "" || (raw != "0" && (raw[0] < '1' || raw[0] > '9')) {
		return nil, fmt.Errorf("noncanonical decimal")
	}
	for _, value := range raw {
		if value < '0' || value > '9' {
			return nil, fmt.Errorf("nondecimal character")
		}
	}
	parsed, ok := new(big.Int).SetString(raw, 10)
	if !ok || parsed.Sign() < 0 || parsed.BitLen() > 256 {
		return nil, fmt.Errorf("outside uint256")
	}
	return parsed, nil
}
