package identity

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type stubPrivacyRootSigner struct {
	address   string
	pubKey    []byte
	signature []byte
	lastMsg   []byte
}

func (s *stubPrivacyRootSigner) Address() (string, error) {
	return s.address, nil
}

func (s *stubPrivacyRootSigner) PubKeyBytes() ([]byte, error) {
	return append([]byte(nil), s.pubKey...), nil
}

func (s *stubPrivacyRootSigner) SignPrivacyRoot(message []byte) ([]byte, error) {
	s.lastMsg = append([]byte(nil), message...)
	return append([]byte(nil), s.signature...), nil
}

type stubPrivacyRootVerifier struct {
	lastMsg []byte
	lastSig []byte
}

func (s *stubPrivacyRootVerifier) VerifyPrivacyRoot(message, signature []byte) error {
	s.lastMsg = append([]byte(nil), message...)
	s.lastSig = append([]byte(nil), signature...)
	return nil
}

func TestResolveRootSeedMaterialUsesSignerContract(t *testing.T) {
	signer := &stubPrivacyRootSigner{
		address:   "clair1signercontract",
		pubKey:    []byte{0xAA, 0xBB, 0xCC},
		signature: []byte{0x11, 0x22, 0x33},
	}

	material, err := ResolveRootSeedMaterial(signer)
	require.NoError(t, err)

	expectedMessage := BuildRootSigningMessage(signer.address, signer.pubKey)
	require.Equal(t, expectedMessage, signer.lastMsg)
	require.Equal(t, signer.address, material.Address)
	require.Equal(t, signer.pubKey, material.TransparentPubKey)
	require.Equal(t, expectedMessage, material.SigningMessage)
	require.Equal(t, signer.signature, material.Signature)
}

func TestDeriveRootSeedFromSignerMatchesMaterial(t *testing.T) {
	signer := &stubPrivacyRootSigner{
		address:   "clair1signercontract",
		pubKey:    []byte("transparent-pubkey"),
		signature: []byte("deterministic-signature"),
	}

	rootSeed, material, err := DeriveRootSeedFromSigner(signer)
	require.NoError(t, err)

	expectedRootSeed := ComputeRootSeed(signer.address, signer.pubKey, signer.signature)
	require.Equal(t, expectedRootSeed, rootSeed)

	rootSeedFromMaterial, err := DeriveRootSeedFromMaterial(material)
	require.NoError(t, err)
	require.Equal(t, expectedRootSeed, rootSeedFromMaterial)
}

func TestVerifyRootSeedMaterialDelegatesToVerifier(t *testing.T) {
	verifier := &stubPrivacyRootVerifier{}
	material := &RootSeedMaterial{
		Address:           "clair1signercontract",
		TransparentPubKey: []byte{0xAA, 0xBB},
		SigningMessage:    []byte("privacy-root-message"),
		Signature:         []byte("privacy-root-signature"),
	}

	err := VerifyRootSeedMaterial(verifier, material)
	require.NoError(t, err)
	require.Equal(t, material.SigningMessage, verifier.lastMsg)
	require.Equal(t, material.Signature, verifier.lastSig)
}
