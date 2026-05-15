package app

import "encoding/json"

// GenesisState is the module-name keyed application genesis state.
type GenesisState map[string]json.RawMessage
