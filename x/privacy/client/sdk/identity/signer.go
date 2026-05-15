package identity

import "fmt"

type PrivacyRootSigner interface {
	Address() (string, error)
	PubKeyBytes() ([]byte, error)
	SignPrivacyRoot(message []byte) ([]byte, error)
}

type PrivacyRootVerifier interface {
	VerifyPrivacyRoot(message, signature []byte) error
}

type RootSeedMaterial struct {
	Address           string
	TransparentPubKey []byte
	SigningMessage    []byte
	Signature         []byte
}

func ResolveRootSeedMaterial(signer PrivacyRootSigner) (*RootSeedMaterial, error) {
	if signer == nil {
		return nil, fmt.Errorf("privacy root signer is required")
	}

	address, err := signer.Address()
	if err != nil {
		return nil, err
	}
	if address == "" {
		return nil, fmt.Errorf("privacy root signer address is empty")
	}

	pubKeyBytes, err := signer.PubKeyBytes()
	if err != nil {
		return nil, err
	}
	if len(pubKeyBytes) == 0 {
		return nil, fmt.Errorf("privacy root signer pubkey bytes are empty")
	}

	signingMessage := BuildRootSigningMessage(address, pubKeyBytes)
	signature, err := signer.SignPrivacyRoot(signingMessage)
	if err != nil {
		return nil, err
	}
	if len(signature) == 0 {
		return nil, fmt.Errorf("privacy root signature is empty")
	}

	return &RootSeedMaterial{
		Address:           address,
		TransparentPubKey: cloneBytes(pubKeyBytes),
		SigningMessage:    cloneBytes(signingMessage),
		Signature:         cloneBytes(signature),
	}, nil
}

func DeriveRootSeedFromMaterial(material *RootSeedMaterial) ([]byte, error) {
	if material == nil {
		return nil, fmt.Errorf("privacy root material is required")
	}
	if material.Address == "" {
		return nil, fmt.Errorf("privacy root material address is empty")
	}
	if len(material.TransparentPubKey) == 0 {
		return nil, fmt.Errorf("privacy root material pubkey bytes are empty")
	}
	if len(material.Signature) == 0 {
		return nil, fmt.Errorf("privacy root material signature is empty")
	}

	rootSeed := ComputeRootSeed(material.Address, material.TransparentPubKey, material.Signature)
	return cloneBytes(rootSeed), nil
}

func DeriveRootSeedFromSigner(signer PrivacyRootSigner) ([]byte, *RootSeedMaterial, error) {
	material, err := ResolveRootSeedMaterial(signer)
	if err != nil {
		return nil, nil, err
	}

	rootSeed, err := DeriveRootSeedFromMaterial(material)
	if err != nil {
		return nil, nil, err
	}

	return rootSeed, material, nil
}

func VerifyRootSeedMaterial(verifier PrivacyRootVerifier, material *RootSeedMaterial) error {
	if verifier == nil {
		return fmt.Errorf("privacy root verifier is required")
	}
	if material == nil {
		return fmt.Errorf("privacy root material is required")
	}
	return verifier.VerifyPrivacyRoot(material.SigningMessage, material.Signature)
}

func cloneBytes(bz []byte) []byte {
	return append([]byte(nil), bz...)
}
