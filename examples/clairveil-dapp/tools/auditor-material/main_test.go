package main

import (
	"encoding/base64"
	"encoding/hex"
	"strings"
	"testing"

	keyringdb "github.com/99designs/keyring"
	"github.com/cosmos/cosmos-sdk/codec"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
)

type keyringItemStore interface {
	SetItem(keyringdb.Item) error
}

func TestBuildAuditorMaterialReadsEthSecp256k1Keyring(t *testing.T) {
	home := t.TempDir()
	const keyName = "auditor"
	priv := &ethSecp256k1PrivKey{
		Key: mustDecodeHex(t, "4f3edf983ac636a65a842ce7c78d9aa706d3b113bce036f02d3f474971e81a42"),
	}
	pubKey := priv.PubKey()

	encodingConfig := auditorMaterialEncodingConfig()
	kr, err := sdkkeyring.New(
		sdk.KeyringServiceName(),
		sdkkeyring.BackendTest,
		home,
		strings.NewReader(""),
		encodingConfig.Codec,
	)
	if err != nil {
		t.Fatalf("open test keyring: %v", err)
	}
	writeLocalKeyringRecord(t, kr, encodingConfig.Codec, keyName, priv, pubKey)

	out, err := buildAuditorMaterial(request{
		Home:           home,
		KeyName:        keyName,
		KeyringBackend: sdkkeyring.BackendTest,
		AccountPrefix:  "evm",
	})
	if err != nil {
		t.Fatalf("build auditor material: %v", err)
	}
	if out.KeyName != keyName {
		t.Fatalf("key name mismatch: got %q want %q", out.KeyName, keyName)
	}
	if !strings.HasPrefix(out.FromAddress, "evm1") {
		t.Fatalf("address prefix mismatch: got %q", out.FromAddress)
	}
	if out.TransparentPubKeyHex != hex.EncodeToString(pubKey.Bytes()) {
		t.Fatalf("pubkey mismatch: got %s want %s", out.TransparentPubKeyHex, hex.EncodeToString(pubKey.Bytes()))
	}

	signature, err := base64.StdEncoding.DecodeString(out.RootSignatureBase64)
	if err != nil {
		t.Fatalf("decode root signature: %v", err)
	}
	if len(signature) != ethSignatureSize {
		t.Fatalf("signature length mismatch: got %d want %d", len(signature), ethSignatureSize)
	}
	if !pubKey.VerifySignature([]byte(out.RootSigningMessage), signature) {
		t.Fatalf("root signature did not verify")
	}

	rootSeed := privacyidentity.ComputeRootSeed(out.FromAddress, pubKey.Bytes(), signature)
	if out.RootSeedHex != hex.EncodeToString(rootSeed) {
		t.Fatalf("root seed mismatch: got %s want %s", out.RootSeedHex, hex.EncodeToString(rootSeed))
	}
	if out.DisclosurePrivateScalarHex == "" || out.DisclosurePubKeyHex == "" {
		t.Fatalf("expected disclosure keys to be derived")
	}
}

func writeLocalKeyringRecord(t *testing.T, kr sdkkeyring.Keyring, cdc codec.Codec, name string, priv *ethSecp256k1PrivKey, pubKey any) {
	t.Helper()

	typedPubKey, ok := pubKey.(*ethSecp256k1PubKey)
	if !ok {
		t.Fatalf("unexpected pubkey type %T", pubKey)
	}
	record, err := sdkkeyring.NewLocalRecord(name, priv, typedPubKey)
	if err != nil {
		t.Fatalf("create local record: %v", err)
	}
	serializedRecord, err := cdc.Marshal(record)
	if err != nil {
		t.Fatalf("marshal local record: %v", err)
	}
	address, err := record.GetAddress()
	if err != nil {
		t.Fatalf("get record address: %v", err)
	}
	store, ok := kr.(keyringItemStore)
	if !ok {
		t.Fatalf("test keyring does not expose SetItem")
	}
	infoKey := name + ".info"
	if err := store.SetItem(keyringdb.Item{Key: infoKey, Data: serializedRecord}); err != nil {
		t.Fatalf("write keyring info item: %v", err)
	}
	addressKey := hex.EncodeToString(address.Bytes()) + ".address"
	if err := store.SetItem(keyringdb.Item{Key: addressKey, Data: []byte(infoKey)}); err != nil {
		t.Fatalf("write keyring address item: %v", err)
	}
}

func mustDecodeHex(t *testing.T, value string) []byte {
	t.Helper()
	bz, err := hex.DecodeString(value)
	if err != nil {
		t.Fatalf("decode hex: %v", err)
	}
	return bz
}
