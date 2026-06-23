package cli

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGetQueryCmdIncludesReserve(t *testing.T) {
	cmd := GetQueryCmd()
	_, _, err := cmd.Find([]string{"reserve"})
	require.NoError(t, err)
}
