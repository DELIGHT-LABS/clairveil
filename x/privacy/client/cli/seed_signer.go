package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

type keyringPrivacyRootSigner struct {
	address sdk.AccAddress
	keyring sdkkeyring.Keyring
	pubKey  cryptotypes.PubKey
}

func newKeyringPrivacyRootSigner(clientCtx client.Context) (*keyringPrivacyRootSigner, sdk.AccAddress, error) {
	fromAddress, err := resolveClientFromAddress(clientCtx)
	if err != nil {
		return nil, nil, err
	}
	if clientCtx.Keyring == nil {
		return nil, nil, fmt.Errorf("a keyring is required to derive the privacy root seed")
	}

	record, err := clientCtx.Keyring.KeyByAddress(fromAddress)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load the keyring record for %s: %w", fromAddress.String(), err)
	}

	pubKey, err := record.GetPubKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load the transparent pubkey for %s: %w", fromAddress.String(), err)
	}

	return &keyringPrivacyRootSigner{
		address: fromAddress,
		keyring: clientCtx.Keyring,
		pubKey:  pubKey,
	}, fromAddress, nil
}

func (s *keyringPrivacyRootSigner) Address() (string, error) {
	if s == nil || s.address.Empty() {
		return "", fmt.Errorf("privacy root signer address is unavailable")
	}
	return s.address.String(), nil
}

func (s *keyringPrivacyRootSigner) PubKeyBytes() ([]byte, error) {
	if s == nil || s.pubKey == nil {
		return nil, fmt.Errorf("privacy root signer pubkey is unavailable")
	}
	return append([]byte(nil), s.pubKey.Bytes()...), nil
}

func (s *keyringPrivacyRootSigner) SignPrivacyRoot(message []byte) ([]byte, error) {
	if s == nil || s.keyring == nil || s.address.Empty() {
		return nil, fmt.Errorf("privacy root signer keyring context is incomplete")
	}

	signature, _, err := s.keyring.SignByAddress(s.address, message, signing.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return nil, fmt.Errorf("failed to sign privacy root material with %s: %w", s.address.String(), err)
	}
	return append([]byte(nil), signature...), nil
}

func (s *keyringPrivacyRootSigner) VerifyPrivacyRoot(message, signature []byte) error {
	if s == nil || s.pubKey == nil {
		return fmt.Errorf("privacy root signer pubkey is unavailable")
	}
	if !s.pubKey.VerifySignature(message, signature) {
		return fmt.Errorf("privacy root signature verification failed for %s", s.address.String())
	}
	return nil
}
