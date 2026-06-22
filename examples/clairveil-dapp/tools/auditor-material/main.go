package main

import (
	"bytes"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/cosmos/cosmos-sdk/codec"
	sdklegacy "github.com/cosmos/cosmos-sdk/codec/legacy"
	sdkkeyring "github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/gogoproto/proto"
	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacyidentity "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/identity"
)

type request struct {
	Home           string `json:"home"`
	KeyName        string `json:"key_name"`
	KeyringBackend string `json:"keyring_backend"`
	AccountPrefix  string `json:"account_prefix"`
}

type response struct {
	KeyName                    string `json:"key_name"`
	FromAddress                string `json:"from_address"`
	TransparentPubKeyHex       string `json:"transparent_pubkey_hex"`
	RootSigningMessage         string `json:"root_signing_message"`
	RootSignatureBase64        string `json:"root_signature_base64"`
	RootSeedHex                string `json:"root_seed_hex"`
	DisclosurePrivateScalarHex string `json:"disclosure_private_scalar_hex"`
	DisclosurePubKeyHex        string `json:"disclosure_pubkey_hex"`
	DerivedFrom                string `json:"derived_from"`
}

const (
	ethSecp256k1KeyType     = "eth_secp256k1"
	ethSecp256k1PrivKeyName = "os/PrivKeyEthSecp256k1"
	ethSecp256k1PubKeyName  = "os/PubKeyEthSecp256k1"
	ethSecp256k1PrivKeySize = 32
	ethSecp256k1PubKeySize  = 33
	ethSignatureSize        = 65
)

type ethSecp256k1PubKey struct {
	Key []byte `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
}

type ethSecp256k1PrivKey struct {
	Key []byte `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
}

var (
	_ cryptotypes.PubKey   = (*ethSecp256k1PubKey)(nil)
	_ cryptotypes.PrivKey  = (*ethSecp256k1PrivKey)(nil)
	_ codec.AminoMarshaler = (*ethSecp256k1PubKey)(nil)
	_ codec.AminoMarshaler = (*ethSecp256k1PrivKey)(nil)
)

func init() {
	proto.RegisterType((*ethSecp256k1PubKey)(nil), "cosmos.evm.crypto.v1.ethsecp256k1.PubKey")
	proto.RegisterType((*ethSecp256k1PrivKey)(nil), "cosmos.evm.crypto.v1.ethsecp256k1.PrivKey")
}

func main() {
	var req request
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		fatalf("failed to decode request: %v", err)
	}
	out, err := buildAuditorMaterial(req)
	if err != nil {
		fatalf("%v", err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(out); err != nil {
		fatalf("failed to encode response: %v", err)
	}
}

func buildAuditorMaterial(req request) (response, error) {
	setSDKConfig(req.AccountPrefix)
	if req.Home == "" {
		return response{}, fmt.Errorf("home is required")
	}
	if req.KeyName == "" {
		req.KeyName = "auditor"
	}
	if req.KeyringBackend == "" {
		req.KeyringBackend = sdkkeyring.BackendTest
	}

	encodingConfig := auditorMaterialEncodingConfig()
	sdklegacy.Cdc = encodingConfig.Amino
	kr, err := sdkkeyring.New(
		sdk.KeyringServiceName(),
		req.KeyringBackend,
		req.Home,
		strings.NewReader(""),
		encodingConfig.Codec,
	)
	if err != nil {
		return response{}, fmt.Errorf("failed to open keyring: %w", err)
	}

	record, err := kr.Key(req.KeyName)
	if err != nil {
		return response{}, fmt.Errorf("failed to load key %q: %w", req.KeyName, err)
	}
	address, err := record.GetAddress()
	if err != nil {
		return response{}, fmt.Errorf("failed to get address for %q: %w", req.KeyName, err)
	}
	pubKey, err := record.GetPubKey()
	if err != nil {
		return response{}, fmt.Errorf("failed to get pubkey for %q: %w", req.KeyName, err)
	}

	pubKeyBytes := pubKey.Bytes()
	signingMessage := privacyidentity.BuildRootSigningMessage(address.String(), pubKeyBytes)
	signature, _, err := kr.SignByAddress(address, signingMessage, signing.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return response{}, fmt.Errorf("failed to sign privacy root material: %w", err)
	}
	if !pubKey.VerifySignature(signingMessage, signature) {
		return response{}, fmt.Errorf("privacy root signature verification failed")
	}

	rootSeed := privacyidentity.ComputeRootSeed(address.String(), pubKeyBytes, signature)
	disclosureScalar, disclosurePubKey, _ := privacyidentity.DeriveDisclosureKeys(rootSeed)
	disclosurePubKeyBytes := disclosurePubKey.Bytes()

	out := response{
		KeyName:                    req.KeyName,
		FromAddress:                address.String(),
		TransparentPubKeyHex:       hex.EncodeToString(pubKeyBytes),
		RootSigningMessage:         string(signingMessage),
		RootSignatureBase64:        base64.StdEncoding.EncodeToString(signature),
		RootSeedHex:                hex.EncodeToString(rootSeed),
		DisclosurePrivateScalarHex: privacyidentity.ScalarToFixedHex(disclosureScalar),
		DisclosurePubKeyHex:        hex.EncodeToString(disclosurePubKeyBytes[:]),
		DerivedFrom:                "local-test-keyring-root",
	}
	return out, nil
}

func auditorMaterialEncodingConfig() testutil.TestEncodingConfig {
	encodingConfig := testutil.MakeTestEncodingConfig()
	registerEthSecp256k1KeyTypes(encodingConfig.InterfaceRegistry)
	encodingConfig.Amino.RegisterConcrete(&ethSecp256k1PubKey{}, ethSecp256k1PubKeyName, nil)
	encodingConfig.Amino.RegisterConcrete(&ethSecp256k1PrivKey{}, ethSecp256k1PrivKeyName, nil)
	return encodingConfig
}

func setSDKConfig(accountPrefix string) {
	if accountPrefix == "" {
		accountPrefix = clairveiltypes.Bech32PrefixAccAddr
	}
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(accountPrefix, accountPrefix+clairveiltypes.PrefixPublic)
	config.SetBech32PrefixForValidator(
		accountPrefix+clairveiltypes.PrefixValidator+clairveiltypes.PrefixOperator,
		accountPrefix+clairveiltypes.PrefixValidator+clairveiltypes.PrefixOperator+clairveiltypes.PrefixPublic,
	)
	config.SetBech32PrefixForConsensusNode(
		accountPrefix+clairveiltypes.PrefixValidator+clairveiltypes.PrefixConsensus,
		accountPrefix+clairveiltypes.PrefixValidator+clairveiltypes.PrefixConsensus+clairveiltypes.PrefixPublic,
	)
	config.SetCoinType(clairveiltypes.CoinType)
	config.SetFullFundraiserPath(clairveiltypes.FullFundraiserPath)
	sdk.DefaultBondDenom = clairveiltypes.DefaultDenom
	config.Seal()
}

func registerEthSecp256k1KeyTypes(registry interface {
	RegisterImplementations(any, ...proto.Message)
}) {
	registry.RegisterImplementations((*cryptotypes.PubKey)(nil), &ethSecp256k1PubKey{})
	registry.RegisterImplementations((*cryptotypes.PrivKey)(nil), &ethSecp256k1PrivKey{})
}

func (key *ethSecp256k1PubKey) Reset()         { *key = ethSecp256k1PubKey{} }
func (key *ethSecp256k1PubKey) String() string { return fmt.Sprintf("EthPubKeySecp256k1{%X}", key.Key) }
func (*ethSecp256k1PubKey) ProtoMessage()      {}

func (key *ethSecp256k1PubKey) Address() cryptotypes.Address {
	pubKey, err := secp256k1.ParsePubKey(key.Key)
	if err != nil {
		return nil
	}
	uncompressed := pubKey.SerializeUncompressed()
	hash := keccak256(uncompressed[1:])
	return cryptotypes.Address(hash[12:])
}

func (key *ethSecp256k1PubKey) Bytes() []byte {
	bz := make([]byte, len(key.Key))
	copy(bz, key.Key)
	return bz
}

func (key *ethSecp256k1PubKey) VerifySignature(msg, sig []byte) bool {
	if len(sig) == ethSignatureSize {
		sig = sig[:len(sig)-1]
	}
	if len(sig) != 64 {
		return false
	}
	pubKey, err := secp256k1.ParsePubKey(key.Key)
	if err != nil {
		return false
	}
	hash := keccak256(msg)
	var r, s secp256k1.ModNScalar
	if r.SetByteSlice(sig[:32]) || s.SetByteSlice(sig[32:]) || r.IsZero() || s.IsZero() {
		return false
	}
	return ecdsa.NewSignature(&r, &s).Verify(hash, pubKey)
}

func (key *ethSecp256k1PubKey) Equals(other cryptotypes.PubKey) bool {
	return other != nil && key.Type() == other.Type() && bytes.Equal(key.Bytes(), other.Bytes())
}

func (*ethSecp256k1PubKey) Type() string {
	return ethSecp256k1KeyType
}

func (key *ethSecp256k1PubKey) MarshalAmino() ([]byte, error) {
	if len(key.Key) != ethSecp256k1PubKeySize {
		return nil, fmt.Errorf("invalid pubkey size, expected %d got %d", ethSecp256k1PubKeySize, len(key.Key))
	}
	return key.Bytes(), nil
}

func (key *ethSecp256k1PubKey) UnmarshalAmino(bz []byte) error {
	if len(bz) != ethSecp256k1PubKeySize {
		return fmt.Errorf("invalid pubkey size, expected %d got %d", ethSecp256k1PubKeySize, len(bz))
	}
	key.Key = append(key.Key[:0], bz...)
	return nil
}

func (key *ethSecp256k1PubKey) MarshalAminoJSON() ([]byte, error) {
	return key.MarshalAmino()
}

func (key *ethSecp256k1PubKey) UnmarshalAminoJSON(bz []byte) error {
	return key.UnmarshalAmino(bz)
}

func (key *ethSecp256k1PrivKey) Reset()         { *key = ethSecp256k1PrivKey{} }
func (key *ethSecp256k1PrivKey) String() string { return proto.CompactTextString(key) }
func (*ethSecp256k1PrivKey) ProtoMessage()      {}

func (key *ethSecp256k1PrivKey) Bytes() []byte {
	bz := make([]byte, len(key.Key))
	copy(bz, key.Key)
	return bz
}

func (key *ethSecp256k1PrivKey) Sign(msg []byte) ([]byte, error) {
	if len(key.Key) != ethSecp256k1PrivKeySize {
		return nil, fmt.Errorf("invalid privkey size, expected %d got %d", ethSecp256k1PrivKeySize, len(key.Key))
	}
	hash := msg
	if len(hash) != 32 {
		hash = keccak256(msg)
	}
	privKey := secp256k1.PrivKeyFromBytes(key.Key)
	compact := ecdsa.SignCompact(privKey, hash, false)
	if len(compact) != ethSignatureSize {
		return nil, fmt.Errorf("unexpected compact signature size %d", len(compact))
	}
	recoveryID := compact[0] - 27
	if recoveryID > 3 {
		return nil, fmt.Errorf("unexpected compact signature recovery id %d", compact[0])
	}
	sig := make([]byte, ethSignatureSize)
	copy(sig[:64], compact[1:])
	sig[64] = recoveryID
	return sig, nil
}

func (key *ethSecp256k1PrivKey) PubKey() cryptotypes.PubKey {
	if len(key.Key) != ethSecp256k1PrivKeySize {
		return nil
	}
	pubKey := secp256k1.PrivKeyFromBytes(key.Key).PubKey()
	return &ethSecp256k1PubKey{Key: pubKey.SerializeCompressed()}
}

func (key *ethSecp256k1PrivKey) Equals(other cryptotypes.LedgerPrivKey) bool {
	return other != nil && key.Type() == other.Type() && subtle.ConstantTimeCompare(key.Bytes(), other.Bytes()) == 1
}

func (*ethSecp256k1PrivKey) Type() string {
	return ethSecp256k1KeyType
}

func (key *ethSecp256k1PrivKey) MarshalAmino() ([]byte, error) {
	if len(key.Key) != ethSecp256k1PrivKeySize {
		return nil, fmt.Errorf("invalid privkey size, expected %d got %d", ethSecp256k1PrivKeySize, len(key.Key))
	}
	return key.Bytes(), nil
}

func (key *ethSecp256k1PrivKey) UnmarshalAmino(bz []byte) error {
	if len(bz) != ethSecp256k1PrivKeySize {
		return fmt.Errorf("invalid privkey size, expected %d got %d", ethSecp256k1PrivKeySize, len(bz))
	}
	key.Key = append(key.Key[:0], bz...)
	return nil
}

func (key *ethSecp256k1PrivKey) MarshalAminoJSON() ([]byte, error) {
	return key.MarshalAmino()
}

func (key *ethSecp256k1PrivKey) UnmarshalAminoJSON(bz []byte) error {
	return key.UnmarshalAmino(bz)
}

func keccak256(bz []byte) []byte {
	hash := sha3.NewLegacyKeccak256()
	_, _ = hash.Write(bz)
	return hash.Sum(nil)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
