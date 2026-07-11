package withdraw

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPreparedWithdrawFilesReplacePermissiveModes(t *testing.T) {
	writers := []struct {
		name  string
		write func(string) error
	}{
		{name: "payload", write: (PreparedWithdrawPayload{}).WriteJSONFile},
		{name: "prover-payload", write: (PreparedWithdrawProverPayload{}).WriteJSONFile},
		{name: "proof", write: (PreparedWithdrawProof{}).WriteJSONFile},
	}
	for _, writer := range writers {
		t.Run(writer.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), writer.name+".json")
			require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
			require.NoError(t, os.Chmod(path, 0o644))
			require.NoError(t, writer.write(path))
			info, err := os.Stat(path)
			require.NoError(t, err)
			require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
		})
	}
}
