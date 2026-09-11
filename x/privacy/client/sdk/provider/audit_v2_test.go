package provider

import (
	"context"
	"testing"

	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
)

func TestAuditQueryContextAtHeightPinsAndReplacesHeightMetadata(t *testing.T) {
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-request-id", "test", grpctypes.GRPCBlockHeightHeader, "11"))
	pinned := auditQueryContextAtHeight(ctx, 42)

	md, ok := metadata.FromOutgoingContext(pinned)
	require.True(t, ok)
	require.Equal(t, []string{"42"}, md.Get(grpctypes.GRPCBlockHeightHeader))
	require.Equal(t, []string{"test"}, md.Get("x-request-id"))
}
