// clairveil-auditor collects original successful Cosmos transactions for a
// closed block range, verifies their audit proofs, decrypts them with an
// owner-only keyring, and builds deposit-rooted note lineage. It performs no
// replay and does not treat PrivacyScan as an audit ledger.
package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"reflect"

	"github.com/DELIGHT-LABS/clairveil/app"
	"github.com/DELIGHT-LABS/clairveil/internal/strictjson"
	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	gnarklogger "github.com/consensys/gnark/logger"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/gogoproto/proto"
)

const maxAuditKeyringBytes = 1 << 20

type keyringFile struct {
	Version uint16         `json:"version"`
	Keys    []keyringEntry `json:"keys"`
}

type keyringEntry struct {
	KeyID  string `json:"key_id"`
	Secret string `json:"secret_key"`
}

type secretHistory map[[32]byte]privacycrypto.AuditSecretKey

func (s secretHistory) AuditSecret(id [32]byte) (privacycrypto.AuditSecretKey, bool) {
	secret, found := s[id]
	return secret, found
}

type queryKeyHistory struct {
	query         privacyv2.QueryClient
	network       [32]byte
	initialHeight uint64
}

func (h queryKeyHistory) AuditKey(ctx context.Context, epoch uint64) (privacyaudit.KeyRecord, error) {
	response, err := h.query.AuditKey(ctx, &privacyv2.QueryAuditKeyRequest{Epoch: epoch})
	if err != nil {
		return privacyaudit.KeyRecord{}, err
	}
	if response == nil || response.StateHeight <= 0 {
		return privacyaudit.KeyRecord{}, fmt.Errorf("empty audit key response for epoch %d", epoch)
	}
	return privacyaudit.ParseKeyRecord(response.Record, h.network, h.initialHeight)
}

func main() {
	clairveiltypes.SetConfig()
	gnarklogger.Disable()
	report, err := run(context.Background(), os.Args[1:])
	if report != nil {
		encoded, marshalErr := json.MarshalIndent(report, "", "  ")
		if marshalErr != nil {
			fmt.Fprintf(os.Stderr, "encode auditor report: %v\n", marshalErr)
			os.Exit(1)
		}
		fmt.Println(string(encoded))
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "clairveil-auditor: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (*privacyaudit.ProvenanceReport, error) {
	flags := flag.NewFlagSet("clairveil-auditor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	node := flags.String("node", "tcp://localhost:26657", "CometBFT RPC endpoint")
	chainID := flags.String("chain-id", "", "Cosmos chain ID")
	from := flags.Uint64("from-height", 0, "first block height (defaults to audit initial height)")
	to := flags.Uint64("to-height", 0, "last block height (defaults to queried state height)")
	cachePath := flags.String("cache", "", "atomic local collector JSON cache")
	keyringPath := flags.String("audit-keyring-file", "", "0600 JSON file containing retained audit secrets")
	artifactDir := flags.String("audit-artifacts", "", "fixed audit-field verifier artifact directory")
	if err := flags.Parse(args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 || *chainID == "" || *cachePath == "" || *keyringPath == "" || *artifactDir == "" {
		return nil, fmt.Errorf("usage: clairveil-auditor --chain-id ID --cache FILE --audit-keyring-file FILE --audit-artifacts DIR [--node URI] [--from-height N] [--to-height N]")
	}
	rpc, err := client.NewClientFromNode(*node)
	if err != nil {
		return nil, err
	}
	codec := app.NewClientCodec()
	privacyv2.RegisterInterfaces(codec.InterfaceRegistry)
	clientCtx := client.Context{}.
		WithCodec(codec.AppCodec).
		WithInterfaceRegistry(codec.InterfaceRegistry).
		WithLegacyAmino(codec.LegacyAmino).
		WithTxConfig(codec.TxConfig).
		WithChainID(*chainID).
		WithNodeURI(*node).
		WithClient(rpc)
	query := privacyv2.NewQueryClient(clientCtx)
	configuration, err := query.AuditConfiguration(ctx, &privacyv2.QueryAuditConfigurationRequest{})
	if err != nil {
		return nil, fmt.Errorf("query audit configuration: %w", err)
	}
	if configuration == nil || len(configuration.NetworkNonce) != 32 || configuration.InitialHeight == 0 || configuration.StateHeight < int64(configuration.InitialHeight) {
		return nil, fmt.Errorf("invalid audit configuration response")
	}
	var nonce [32]byte
	copy(nonce[:], configuration.NetworkNonce)
	network, err := auditfield.NetworkDigest(*chainID, nonce)
	if err != nil {
		return nil, err
	}
	var onChainIdentity privacytypes.CircuitSetIdentity
	if err := proto.Unmarshal(configuration.CircuitIdentity, &onChainIdentity); err != nil || privacytypes.ValidateAuditCircuitSetIdentity(&onChainIdentity) != nil {
		return nil, fmt.Errorf("invalid on-chain circuit identity")
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{ArtifactDir: *artifactDir, CircuitSetID: privacyzk.AuditFieldCircuitSetID, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	if err != nil {
		return nil, err
	}
	localIdentity, err := registry.LocalCircuitSetIdentity()
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(localIdentity, &onChainIdentity) {
		return nil, fmt.Errorf("local verifier identity does not match immutable on-chain circuit identity")
	}
	cache := &privacyaudit.CosmosFileCache{Path: *cachePath}
	requestedFrom, requestedTo, err := resolveRequestedRange(cache, network, *chainID, configuration.InitialHeight, uint64(configuration.StateHeight), *from, *to)
	if err != nil {
		return nil, err
	}
	if requestedFrom < configuration.InitialHeight || requestedTo < requestedFrom || requestedTo > uint64(configuration.StateHeight) {
		return nil, fmt.Errorf("requested range is outside audit configuration history")
	}
	source := privacyaudit.CosmosTxSource{RPC: rpc, Decoder: codec.TxConfig.TxDecoder(), Cache: cache, Network: network, ChainID: *chainID, FromHeight: requestedFrom, ToHeight: requestedTo}
	collector := privacyaudit.Collector{Network: network, Keys: queryKeyHistory{query: query, network: network, initialHeight: configuration.InitialHeight}}
	rows, err := collector.Collect(ctx, source, 0)
	if err != nil {
		state, loadErr := cache.Load(privacyaudit.CosmosCacheIdentity{Network: network, ChainID: *chainID, FromHeight: requestedFrom, ToHeight: requestedTo})
		if loadErr != nil {
			return nil, fmt.Errorf("collect original successful transactions: %v; load checkpoint: %w", err, loadErr)
		}
		var provenanceFailure privacyaudit.ProvenanceFailure
		report := privacyaudit.IncompleteProvenanceReport(state, errors.As(err, &provenanceFailure), err)
		return &report, fmt.Errorf("collect original successful transactions: %w", err)
	}
	state, err := cache.Load(privacyaudit.CosmosCacheIdentity{Network: network, ChainID: *chainID, FromHeight: requestedFrom, ToHeight: requestedTo})
	if err != nil {
		return nil, err
	}
	secrets, err := readKeyring(*keyringPath)
	if err != nil {
		report := privacyaudit.IncompleteProvenanceReport(state, true, err)
		return &report, fmt.Errorf("read audit keyring: %w", err)
	}
	verifier := privacyaudit.HistoricalRegistryVerifier{Resolve: func(_ context.Context, height uint64) (*privacyzk.ArtifactRegistry, *privacytypes.CircuitSetIdentity, error) {
		if height < configuration.InitialHeight || height > uint64(configuration.StateHeight) {
			return nil, nil, fmt.Errorf("height %d is outside immutable circuit configuration history", height)
		}
		return registry, localIdentity, nil
	}}
	report, reportErr := privacyaudit.BuildProvenanceReport(ctx, state, network, rows, verifier, secrets)
	return &report, reportErr
}

func resolveRequestedRange(cache *privacyaudit.CosmosFileCache, network [32]byte, chainID string, initialHeight, stateHeight, from, to uint64) (uint64, uint64, error) {
	requestedFrom := from
	if requestedFrom == 0 {
		requestedFrom = initialHeight
	}
	if to != 0 {
		return requestedFrom, to, nil
	}
	stored, found, err := cache.LoadExisting()
	if err != nil {
		return 0, 0, err
	}
	if found && stored.Network == hex.EncodeToString(network[:]) && stored.ChainID == chainID && stored.FromHeight == requestedFrom {
		if stored.ToHeight > stateHeight {
			return 0, 0, fmt.Errorf("stored audit collector range extends beyond queried state height")
		}
		return requestedFrom, stored.ToHeight, nil
	}
	return requestedFrom, stateHeight, nil
}

func readKeyring(path string) (secretHistory, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 || info.Size() <= 0 || info.Size() > maxAuditKeyringBytes {
		return nil, fmt.Errorf("audit keyring must be a non-empty bounded 0600 regular file")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read audit keyring: %w", err)
	}
	if len(raw) > maxAuditKeyringBytes {
		return nil, fmt.Errorf("read audit keyring: file grew beyond size bound")
	}
	defer clear(raw)
	var file keyringFile
	if err := strictjson.Decode(raw, &file); err != nil {
		return nil, fmt.Errorf("decode audit keyring: %w", err)
	}
	if file.Version != 1 || len(file.Keys) == 0 || len(file.Keys) > 4096 {
		return nil, fmt.Errorf("audit keyring must be version 1 with 1..4096 keys")
	}
	keys := make(secretHistory, len(file.Keys))
	for index, entry := range file.Keys {
		id, err := decodeHex32(entry.KeyID)
		if err != nil {
			return nil, fmt.Errorf("audit keyring entry %d key ID: %w", index, err)
		}
		secretBytes, err := decodeHex32(entry.Secret)
		if err != nil {
			return nil, fmt.Errorf("audit keyring entry %d secret: %w", index, err)
		}
		secret, err := privacycrypto.ImportAuditSecretKeyBE32(secretBytes[:])
		clear(secretBytes[:])
		if err != nil {
			return nil, fmt.Errorf("audit keyring entry %d secret: %w", index, err)
		}
		key, err := privacycrypto.AuditKeyFromSecret(secret)
		if err != nil || key.ID() != id {
			return nil, fmt.Errorf("audit keyring entry %d key ID does not match secret", index)
		}
		if _, duplicate := keys[id]; duplicate {
			return nil, fmt.Errorf("audit keyring has duplicate key ID")
		}
		keys[id] = secret
	}
	return keys, nil
}

func decodeHex32(raw string) ([32]byte, error) {
	var value [32]byte
	if len(raw) != 64 {
		return value, fmt.Errorf("value must be exactly 32 lowercase hex bytes")
	}
	count, err := hex.Decode(value[:], []byte(raw))
	if err != nil || count != len(value) || !bytes.Equal([]byte(hex.EncodeToString(value[:])), []byte(raw)) {
		return [32]byte{}, fmt.Errorf("value must be exactly 32 lowercase hex bytes")
	}
	return value, nil
}
