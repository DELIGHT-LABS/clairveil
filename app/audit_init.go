package app

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"github.com/DELIGHT-LABS/clairveil/internal/auditinit"
	"github.com/DELIGHT-LABS/clairveil/internal/strictjson"
	privacymodule "github.com/DELIGHT-LABS/clairveil/x/privacy"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// initPrivacyChain retains normal Cosmos module initialization and adds only
// the small privacy genesis state needed by the fixed verifier/key lane. No
// replay request, binary/source bundle, or transition store is retained.
func (app *ClairveilApp) initPrivacyChain(ctx sdk.Context, req *abci.RequestInitChain) (response *abci.ResponseInitChain, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			response = nil
			err = fmt.Errorf("privacy initialization failed: %v", failure)
		}
	}()
	initial := req.InitialHeight
	if initial == 0 {
		initial = 1
	}
	if initial < 1 || ctx.ChainID() != req.ChainId {
		return nil, fmt.Errorf("initial chain/height mismatch")
	}
	var state GenesisState
	if err := strictjson.Decode(req.AppStateBytes, &state); err != nil {
		return nil, err
	}
	privacy, err := privacytypes.ParseFreshGenesisV4(state[privacytypes.ModuleName])
	if err != nil {
		return nil, err
	}
	if privacy.InitialHeight != uint64(initial) {
		if privacy.State == nil || privacy.InitialHeight != app.PrivacyKeeper.AuditInitialHeight() || privacy.RestartHeight != uint64(initial) {
			return nil, fmt.Errorf("privacy genesis initial/restart height mismatch")
		}
	} else if privacy.InitialHeight != app.PrivacyKeeper.AuditInitialHeight() {
		return nil, fmt.Errorf("privacy genesis initial height mismatch")
	}
	// Key provenance is bound to the immutable first-chain height, not the
	// later restart height carried by an ordinary exported genesis.
	anchor := privacyGenesisAnchor(req.ChainId, privacy.NetworkNonce, privacy.InitialHeight)
	cache, publish := ctx.CacheContext()
	if err := app.genesisTxHandler.bind(cache.MultiStore()); err != nil {
		return nil, err
	}
	bound := true
	defer func() {
		if bound {
			app.genesisTxHandler.unbind()
		}
	}()
	err = auditinit.Run(cache, func(init sdk.Context) error {
		if privacy.State == nil {
			if err := app.PrivacyKeeper.InitializeFreshAudit(init, privacy, anchor); err != nil {
				return err
			}
		} else {
			// Scan records validate their audit-key identity while the ordinary
			// privacy importer writes them, so install only the small management
			// metadata first. The ordinary importer then owns all privacy state.
			if err := app.PrivacyKeeper.SetCircuitSetIdentity(init, privacy.CircuitSetIdentity); err != nil {
				return err
			}
			if err := app.PrivacyKeeper.InitializeAuditMetadata(init, privacy, anchor); err != nil {
				return err
			}
			privacymodule.InitGenesis(init, app.PrivacyKeeper, *privacy.State)
		}
		if err := app.auditUpgradeKeeper.SetModuleVersionMap(init, app.ModuleManager.GetVersionMap()); err != nil {
			return err
		}
		var initErr error
		response, initErr = app.ModuleManager.InitGenesis(init, app.appCodec, state)
		if initErr != nil {
			return initErr
		}
		if err := app.auditValidateBank(init); err != nil {
			return err
		}
		if privacy.State == nil {
			return app.PrivacyKeeper.ValidateFreshAudit(init, privacy, anchor)
		}
		return app.PrivacyKeeper.ValidateLoadedAuditIdentity(init)
	})
	if err != nil {
		return nil, err
	}
	app.genesisTxHandler.unbind()
	bound = false
	publish()
	return response, nil
}

func privacyGenesisAnchor(chainID string, nonce []byte, initialHeight uint64) [32]byte {
	var height [8]byte
	binary.BigEndian.PutUint64(height[:], initialHeight)
	return sha256.Sum256(append(append(append([]byte("clairveil/privacy/genesis-anchor/v1"), []byte(chainID)...), nonce...), height[:]...))
}
