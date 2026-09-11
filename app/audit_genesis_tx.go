package app

import (
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/baseapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// scopedGenesisTxHandler makes genutil's genesis transactions execute against
// the outer InitChain cache. BaseApp.ExecuteGenesisTx always selects its own
// finalize state, which would bypass the cache used by audit initialization.
// Binding is strictly scoped to one InitChain invocation.
type scopedGenesisTxHandler struct {
	app *baseapp.BaseApp

	mu    sync.Mutex
	store storetypes.MultiStore
}

func newScopedGenesisTxHandler(app *baseapp.BaseApp) *scopedGenesisTxHandler {
	return &scopedGenesisTxHandler{app: app}
}

func (h *scopedGenesisTxHandler) bind(store storetypes.MultiStore) error {
	if h == nil || h.app == nil || store == nil {
		return fmt.Errorf("genesis transaction handler is unavailable")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.store != nil {
		return fmt.Errorf("genesis transaction handler is already bound")
	}
	h.store = store
	return nil
}

func (h *scopedGenesisTxHandler) unbind() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.store = nil
	h.mu.Unlock()
}

func (h *scopedGenesisTxHandler) ExecuteGenesisTx(txBytes []byte) error {
	if h == nil || h.app == nil {
		return fmt.Errorf("genesis transaction handler is unavailable")
	}
	h.mu.Lock()
	store := h.store
	h.mu.Unlock()
	if store == nil {
		return fmt.Errorf("genesis transaction handler is not bound")
	}
	_, _, _, err := h.app.RunTx(sdk.ExecModeFinalize, txBytes, nil, -1, store, nil)
	return err
}
