package audit

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DELIGHT-LABS/clairveil/internal/strictjson"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/gogoproto/proto"
)

const (
	cosmosCacheVersion = 1
	maxCosmosCacheSize = 256 << 20
)

type CosmosCacheIdentity struct {
	Network    [32]byte
	ChainID    string
	FromHeight uint64
	ToHeight   uint64
}

// CachedCosmosTx retains the actual original transaction and the matching
// final execution result. It is not a replay input and contains no synthetic
// transition record.
type CachedCosmosTx struct {
	Height uint64            `json:"height"`
	Index  uint32            `json:"index"`
	Hash   []byte            `json:"hash"`
	Raw    []byte            `json:"raw_tx"`
	Result abci.ExecTxResult `json:"result"`
}

type CosmosCacheState struct {
	Version             uint16           `json:"version"`
	Network             string           `json:"network"`
	ChainID             string           `json:"chain_id"`
	FromHeight          uint64           `json:"from_height"`
	ToHeight            uint64           `json:"to_height"`
	LastProcessedHeight uint64           `json:"last_processed_height"`
	Transactions        []CachedCosmosTx `json:"transactions"`
}

type CosmosFileCache struct{ Path string }

// LoadExisting returns a self-validated stored cache without imposing a new
// requested range. Callers use this only to recover an omitted range; normal
// collection still uses Load with an explicit identity.
func (c *CosmosFileCache) LoadExisting() (CosmosCacheState, bool, error) {
	if c == nil || c.Path == "" {
		return CosmosCacheState{}, false, fmt.Errorf("audit collector cache path is required")
	}
	raw, err := os.ReadFile(c.Path)
	if os.IsNotExist(err) {
		return CosmosCacheState{}, false, nil
	}
	if err != nil {
		return CosmosCacheState{}, false, fmt.Errorf("read audit collector cache: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxCosmosCacheSize {
		return CosmosCacheState{}, false, fmt.Errorf("audit collector cache exceeds size bound")
	}
	var state CosmosCacheState
	if err := strictjson.Decode(raw, &state); err != nil {
		return CosmosCacheState{}, false, fmt.Errorf("decode audit collector cache: %w", err)
	}
	identity, err := state.identity()
	if err != nil {
		return CosmosCacheState{}, false, err
	}
	if err := validateCosmosCache(state, identity); err != nil {
		return CosmosCacheState{}, false, err
	}
	return cloneCosmosCacheState(state), true, nil
}

func (c *CosmosFileCache) Load(identity CosmosCacheIdentity) (CosmosCacheState, error) {
	if c == nil || c.Path == "" {
		return CosmosCacheState{}, fmt.Errorf("audit collector cache path is required")
	}
	want := newCosmosCacheState(identity)
	raw, err := os.ReadFile(c.Path)
	if os.IsNotExist(err) {
		return want, nil
	}
	if err != nil {
		return CosmosCacheState{}, fmt.Errorf("read audit collector cache: %w", err)
	}
	if len(raw) == 0 || len(raw) > maxCosmosCacheSize {
		return CosmosCacheState{}, fmt.Errorf("audit collector cache exceeds size bound")
	}
	var state CosmosCacheState
	if err := strictjson.Decode(raw, &state); err != nil {
		return CosmosCacheState{}, fmt.Errorf("decode audit collector cache: %w", err)
	}
	if err := validateCosmosCache(state, identity); err != nil {
		return CosmosCacheState{}, err
	}
	return cloneCosmosCacheState(state), nil
}

func (c *CosmosFileCache) Store(state CosmosCacheState) error {
	if c == nil || c.Path == "" {
		return fmt.Errorf("audit collector cache path is required")
	}
	identity, err := state.identity()
	if err != nil {
		return err
	}
	if err := validateCosmosCache(state, identity); err != nil {
		return err
	}
	wire, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	if len(wire)+1 > maxCosmosCacheSize {
		return fmt.Errorf("audit collector cache exceeds size bound")
	}
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, "."+filepath.Base(c.Path)+"-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		temp.Close()
		return err
	}
	if _, err := temp.Write(append(wire, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempName, c.Path); err != nil {
		return err
	}
	if directory, err := os.Open(dir); err == nil {
		defer directory.Close()
		if err := directory.Sync(); err != nil {
			return err
		}
	}
	return nil
}

func newCosmosCacheState(identity CosmosCacheIdentity) CosmosCacheState {
	return CosmosCacheState{Version: cosmosCacheVersion, Network: hex.EncodeToString(identity.Network[:]), ChainID: identity.ChainID, FromHeight: identity.FromHeight, ToHeight: identity.ToHeight, Transactions: []CachedCosmosTx{}}
}

func (s CosmosCacheState) identity() (CosmosCacheIdentity, error) {
	network, err := canonicalHex32(s.Network)
	if err != nil {
		return CosmosCacheIdentity{}, fmt.Errorf("invalid audit collector cache network")
	}
	return CosmosCacheIdentity{Network: network, ChainID: s.ChainID, FromHeight: s.FromHeight, ToHeight: s.ToHeight}, nil
}

func validateCosmosCache(state CosmosCacheState, want CosmosCacheIdentity) error {
	if state.Version != cosmosCacheVersion || state.Network != hex.EncodeToString(want.Network[:]) || state.ChainID != want.ChainID || state.FromHeight != want.FromHeight || state.ToHeight != want.ToHeight || want.Network == ([32]byte{}) || want.ChainID == "" || want.FromHeight == 0 || want.ToHeight < want.FromHeight {
		return fmt.Errorf("audit collector cache identity mismatch")
	}
	if state.LastProcessedHeight != 0 && (state.LastProcessedHeight < state.FromHeight || state.LastProcessedHeight > state.ToHeight) {
		return fmt.Errorf("audit collector cache checkpoint is outside its range")
	}
	seen := map[string]struct{}{}
	for i, tx := range state.Transactions {
		if tx.Height < state.FromHeight || tx.Height > state.LastProcessedHeight || len(tx.Raw) == 0 || len(tx.Hash) != 32 || !bytes.Equal(tx.Hash, txHash(tx.Raw)) {
			return fmt.Errorf("invalid cached transaction %d", i)
		}
		key := fmt.Sprintf("%d/%d", tx.Height, tx.Index)
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("duplicate cached transaction location %s", key)
		}
		seen[key] = struct{}{}
	}
	return nil
}

func txHash(raw []byte) []byte {
	// CometBFT transaction hashes are SHA-256 over the exact delivered bytes.
	digest := sha256.Sum256(raw)
	return digest[:]
}

func cloneExecTxResult(result *abci.ExecTxResult) abci.ExecTxResult {
	if result == nil {
		return abci.ExecTxResult{}
	}
	return *proto.Clone(result).(*abci.ExecTxResult)
}

func cloneCosmosCacheState(state CosmosCacheState) CosmosCacheState {
	copyState := state
	copyState.Transactions = make([]CachedCosmosTx, len(state.Transactions))
	for i := range state.Transactions {
		copyState.Transactions[i] = state.Transactions[i]
		copyState.Transactions[i].Hash = bytes.Clone(state.Transactions[i].Hash)
		copyState.Transactions[i].Raw = bytes.Clone(state.Transactions[i].Raw)
		copyState.Transactions[i].Result = cloneExecTxResult(&state.Transactions[i].Result)
	}
	return copyState
}
