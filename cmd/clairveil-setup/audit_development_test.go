package main

import (
	"errors"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditRuntimePreservesExistingOutput(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "preserved")
	require.NoError(t, os.WriteFile(sentinel, []byte("original"), 0600))
	require.ErrorContains(t, generateAuditFieldRuntime(dir), "must not exist")
	got, err := os.ReadFile(sentinel)
	require.NoError(t, err)
	require.Equal(t, "original", string(got))
}
func TestAuditRuntimeFailurePublishesNothing(t *testing.T) {
	for _, stage := range []string{"build", "partial write", "checksum"} {
		t.Run(stage, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "bundle")
			err := generateAuditFieldRuntimeWithOps(dir, func() ([]artifactDefinition, error) {
				if stage == "build" {
					return nil, errors.New("injected build failure")
				}
				return []artifactDefinition{{filename: "partial", write: func(dir string) error {
					if err := os.WriteFile(filepath.Join(dir, "partial"), []byte("partial"), 0600); err != nil {
						return err
					}
					if stage == "partial write" {
						return errors.New("injected write failure")
					}
					return nil
				}}}, nil
			}, defaultArtifactSetOps())
			require.Error(t, err)
			_, err = os.Lstat(dir)
			require.True(t, os.IsNotExist(err))
			entries, err := os.ReadDir(parent)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}
func TestAuditRuntimeRejectsSelectiveAndUnknownSet(t *testing.T) {
	_, err := buildArtifactDefinitionsForSet(setupCircuitDeposit, zk.AuditFieldCircuitSetID)
	require.Error(t, err)
	_, err = buildArtifactDefinitionsForSet(setupCircuitAll, "unknown")
	require.Error(t, err)
}

func TestAuditRuntimeReportsCleanupFailure(t *testing.T) {
	parent := t.TempDir()
	original := errors.New("injected build failure")
	ops := defaultArtifactSetOps()
	var staging string
	ops.removeAll = func(path string) error { staging = path; return errors.New("injected cleanup failure") }
	err := generateAuditFieldRuntimeWithOps(filepath.Join(parent, "bundle"), func() ([]artifactDefinition, error) { return nil, original }, ops)
	require.ErrorIs(t, err, original)
	require.ErrorContains(t, err, "injected cleanup failure")
	require.ErrorContains(t, err, staging)
	require.DirExists(t, staging)
	require.NoDirExists(t, filepath.Join(parent, "bundle"))
}
