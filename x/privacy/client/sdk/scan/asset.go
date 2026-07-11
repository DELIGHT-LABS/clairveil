package scan

import (
	"context"
	"fmt"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
)

type AssetDenomResolver interface {
	AssetDenomByID(ctx context.Context, assetID []byte) (string, error)
}

// RestoreFoundNoteDenom resolves display metadata through AssetRegistryV1
// without changing any NoteV1 field used by its commitment.
func RestoreFoundNoteDenom(ctx context.Context, resolver AssetDenomResolver, note *FoundNote) error {
	if resolver == nil || note == nil {
		return fmt.Errorf("asset denom resolver and found note are required")
	}
	assetID, err := privacyfield.CanonicalBytesFromBigInt(note.Note.AssetID)
	if err != nil {
		return err
	}
	denom, err := resolver.AssetDenomByID(ctx, assetID)
	if err != nil {
		return err
	}
	if denom == "" {
		return fmt.Errorf("AssetRegistryV1 returned an empty denom")
	}
	note.AssetDenom = denom
	return nil
}
