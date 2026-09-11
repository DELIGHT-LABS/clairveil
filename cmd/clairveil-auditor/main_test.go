package main

import (
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/stretchr/testify/require"
)

func TestRunRequiresAutomaticQueryInputsButNoManualNonce(t *testing.T) {
	_, err := run(t.Context(), nil)
	require.ErrorContains(t, err, "--chain-id")
	require.NotContains(t, err.Error(), "network-nonce")
	require.NotContains(t, err.Error(), "initial-height")
}

func TestResolveRequestedRangeReusesStoredRangeWhenToHeightIsOmitted(t *testing.T) {
	network := [32]byte{31: 9}
	cache := &privacyaudit.CosmosFileCache{Path: filepath.Join(t.TempDir(), "collector.json")}
	require.NoError(t, cache.Store(privacyaudit.CosmosCacheState{
		Version: 1, Network: hex.EncodeToString(network[:]), ChainID: "audit-chain",
		FromHeight: 10, ToHeight: 12, LastProcessedHeight: 11, Transactions: []privacyaudit.CachedCosmosTx{},
	}))

	from, to, err := resolveRequestedRange(cache, network, "audit-chain", 10, 20, 0, 0)
	require.NoError(t, err)
	require.EqualValues(t, 10, from)
	require.EqualValues(t, 12, to, "a newer chain head must not invalidate an omitted cached range")

	from, to, err = resolveRequestedRange(cache, network, "audit-chain", 10, 20, 0, 18)
	require.NoError(t, err)
	require.EqualValues(t, 10, from)
	require.EqualValues(t, 18, to, "an explicit range remains authoritative")
}

func TestKeyringFailureReportPreservesCompletedCollection(t *testing.T) {
	state := privacyaudit.CosmosCacheState{FromHeight: 10, ToHeight: 12, LastProcessedHeight: 12}
	report := privacyaudit.IncompleteProvenanceReport(state, true, errors.New("keyring unreadable"))
	require.True(t, report.CollectionComplete)
	require.False(t, report.ProvenanceComplete)
	require.EqualValues(t, 12, report.LastProcessedBlock)
	require.Contains(t, report.IncompleteReason, "keyring unreadable")
}

func TestReadKeyringRequiresOwnerOnlyFileAndMatchesKeyID(t *testing.T) {
	rawSecret := [32]byte{31: 7}
	secret, err := privacycrypto.ImportAuditSecretKeyBE32(rawSecret[:])
	require.NoError(t, err)
	key, err := privacycrypto.AuditKeyFromSecret(secret)
	if err != nil {
		t.Skipf("native audit profile unavailable: %v", err)
	}
	path := filepath.Join(t.TempDir(), "keys.json")
	wire := []byte(`{"version":1,"keys":[{"key_id":"` + hex.EncodeToString(key.IDBytes()) + `","secret_key":"` + hex.EncodeToString(rawSecret[:]) + `"}]}`)
	require.NoError(t, os.WriteFile(path, wire, 0o600))
	keys, err := readKeyring(path)
	require.NoError(t, err)
	_, found := keys[key.ID()]
	require.True(t, found)
	require.NoError(t, os.Chmod(path, 0o644))
	_, err = readKeyring(path)
	require.ErrorContains(t, err, "0600")
}
