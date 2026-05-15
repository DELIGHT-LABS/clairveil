package transfer

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
)

type AuditDisclosureTargetProvider interface {
	AuditMasterPubkeyHex(ctx context.Context) (string, error)
}

func DecodeDisclosurePubKeyHex(value string) (*crypto_tedwards.PointAffine, []byte, error) {
	bz, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, nil, fmt.Errorf("invalid disclosure pubkey hex: %w; use the hex output from show-disclosure-pubkey", err)
	}

	var pubKey crypto_tedwards.PointAffine
	if _, err := pubKey.SetBytes(bz); err != nil {
		return nil, nil, fmt.Errorf("invalid disclosure pubkey bytes: %w; expected a compressed BN254 public key from show-disclosure-pubkey", err)
	}

	return &pubKey, append([]byte(nil), bz...), nil
}

func ResolveAuditDisclosureTarget(
	ctx context.Context,
	provider AuditDisclosureTargetProvider,
) (*crypto_tedwards.PointAffine, []byte, error) {
	if provider == nil {
		return nil, nil, fmt.Errorf("an audit disclosure target provider is required")
	}

	pubKeyHex, err := provider.AuditMasterPubkeyHex(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query the audit disclosure config: %w", err)
	}
	if strings.TrimSpace(pubKeyHex) == "" {
		return nil, nil, fmt.Errorf("chain audit master pubkey is not configured")
	}

	return DecodeDisclosurePubKeyHex(pubKeyHex)
}
