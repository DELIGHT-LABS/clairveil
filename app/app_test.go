package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type testAppOptions map[string]any

func (opts testAppOptions) Get(key string) any {
	return opts[key]
}

func configureTestPrivacyManifest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	checksums := make(map[string]string)
	for _, descriptor := range privacyzk.DefaultArtifactDescriptors() {
		checksums[descriptor.ChecksumEnv] = strings.Repeat("a", 64)
	}
	manifest := privacyzk.ManifestFromChecksums(dir, "", checksums)
	manifestBytes, err := json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, privacyzk.ArtifactManifestFile), manifestBytes, 0o600))
	t.Setenv(privacyzk.ZKArtifactDirEnv, dir)
}

var testAppConfigOnce sync.Once

func configureTestAppConfig() { testAppConfigOnce.Do(clairveiltypes.SetConfig) }
