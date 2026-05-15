package keeper

import (
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestDecodeHexQueryArg(t *testing.T) {
	b, err := decodeHexQueryArg("0a0b", "value must be valid hex")
	require.NoError(t, err)
	require.Equal(t, []byte{0x0a, 0x0b}, b)

	_, err = decodeHexQueryArg("zz", "value must be valid hex")
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
	require.Equal(t, "value must be valid hex", st.Message())
}

func TestInvalidQueryRequestErr(t *testing.T) {
	err := invalidQueryRequestErr()
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	require.Equal(t, codes.InvalidArgument, st.Code())
	require.Equal(t, "query request is required", st.Message())
}
