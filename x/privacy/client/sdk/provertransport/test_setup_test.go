package provertransport

import (
	"os"
	"testing"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
)

func TestMain(m *testing.M) {
	clairveiltypes.SetConfig()
	os.Exit(m.Run())
}
