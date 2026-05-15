package cli

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
)

const privacyRootSeedLength = privacyidentity.RootSeedLength

const (
	privacyRootSigningDomain = privacyidentity.RootSigningDomain
	privacySpendDomain       = privacyidentity.SpendDomain
	privacyViewDomain        = privacyidentity.ViewDomain
	privacyDisclosureDomain  = privacyidentity.DisclosureDomain
)

func derivePrivacyRootSeed(clientCtx client.Context) ([]byte, sdk.AccAddress, error) {
	signer, fromAddress, err := newKeyringPrivacyRootSigner(clientCtx)
	if err != nil {
		return nil, nil, err
	}

	rootSeed, material, err := privacyidentity.DeriveRootSeedFromSigner(signer)
	if err != nil {
		return nil, nil, err
	}
	if err := privacyidentity.VerifyRootSeedMaterial(signer, material); err != nil {
		return nil, nil, err
	}

	return rootSeed, fromAddress, nil
}

func resolveClientFromAddress(clientCtx client.Context) (sdk.AccAddress, error) {
	addr := clientCtx.GetFromAddress()
	if !addr.Empty() {
		return addr, nil
	}

	name := clientCtx.GetFromName()
	if name == "" {
		return nil, fmt.Errorf("a transparent --from account is required to derive the privacy root seed")
	}
	if clientCtx.Keyring == nil {
		return nil, fmt.Errorf("a keyring is required to resolve --from %q", name)
	}

	record, err := clientCtx.Keyring.Key(name)
	if err != nil {
		return nil, fmt.Errorf("failed to load the keyring record for %q: %w", name, err)
	}

	addr, err = record.GetAddress()
	if err != nil {
		return nil, fmt.Errorf("failed to get the address for keyring record %q: %w", name, err)
	}
	return addr, nil
}

func buildPrivacyRootSigningMessage(address string, transparentPubKey []byte) []byte {
	return privacyidentity.BuildRootSigningMessage(address, transparentPubKey)
}

func computePrivacyRootSeed(address string, transparentPubKey, signature []byte) []byte {
	return privacyidentity.ComputeRootSeed(address, transparentPubKey, signature)
}

func derivePrivacyDomainSeed(rootSeed []byte, domain string) []byte {
	return privacyidentity.DeriveDomainSeed(rootSeed, domain)
}
