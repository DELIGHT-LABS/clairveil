package privacy

import (
	"context"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestRunAuditGenesisKeepsTheInitializationMarkerScoped(t *testing.T) {
	ctx := sdk.Context{}.WithContext(context.Background())
	called := false
	err := RunAuditGenesis(ctx, func(init sdk.Context) error {
		called = true
		nestedErr := RunAuditGenesis(init, func(sdk.Context) error { return nil })
		require.ErrorContains(t, nestedErr, "nested audit initialization")
		return nil
	})
	require.NoError(t, err)
	require.True(t, called)
}

func TestRunAuditGenesisRejectsNilInitializer(t *testing.T) {
	err := RunAuditGenesis(sdk.Context{}.WithContext(context.Background()), nil)
	require.ErrorContains(t, err, "audit genesis initializer is required")
}
