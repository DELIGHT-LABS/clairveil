package privatefile

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteReplacesPermissiveFileWithOwnerOnlyMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "private.json")
	require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
	require.NoError(t, os.Chmod(path, 0o644))

	require.NoError(t, Write(path, []byte("new")))

	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, []byte("new"), contents)
	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestWriteReplacesSymlinkWithoutChangingTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires additional privileges on Windows")
	}
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.json")
	linkPath := filepath.Join(dir, "private.json")
	require.NoError(t, os.WriteFile(targetPath, []byte("target"), 0o600))
	require.NoError(t, os.Symlink(targetPath, linkPath))

	require.NoError(t, Write(linkPath, []byte("replacement")))

	targetContents, err := os.ReadFile(targetPath)
	require.NoError(t, err)
	require.Equal(t, []byte("target"), targetContents)
	linkInfo, err := os.Lstat(linkPath)
	require.NoError(t, err)
	require.True(t, linkInfo.Mode().IsRegular())
	require.Equal(t, os.FileMode(0o600), linkInfo.Mode().Perm())
}
