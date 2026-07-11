package conformance_test

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	fr_mimc "github.com/consensys/gnark-crypto/ecc/bn254/fr/mimc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type noteV1ContractFixture struct {
	SchemaVersion string `json:"schema_version"`
	Domains       struct {
		FieldDerivation string            `json:"field_derivation"`
		Labels          map[string]string `json:"labels"`
		ConstantsHex    map[string]string `json:"constants_hex"`
	} `json:"domains"`
	Vector struct {
		SpendScalar  string `json:"spend_scalar"`
		ViewScalar   string `json:"view_scalar"`
		Amount       string `json:"amount"`
		Denom        string `json:"denom"`
		AssetIDHex   string `json:"asset_id_hex"`
		Randomness   string `json:"randomness"`
		Commitment   string `json:"commitment_hex"`
		Nullifier    string `json:"nullifier_hex"`
		EmptyRootOne string `json:"empty_root_1_hex"`
		EmptyRootTwo string `json:"empty_root_2_hex"`
		EmptyRoot32  string `json:"empty_root_32_hex"`
	} `json:"vector"`
	Encoding struct {
		Version                  string `json:"version"`
		NotePlaintextBytes       int    `json:"note_plaintext_bytes"`
		DisclosurePlaintextBytes int    `json:"disclosure_plaintext_bytes"`
		EnvelopeHeaderBytes      int    `json:"envelope_header_bytes"`
	} `json:"encoding"`
}

type batchJoinSplitV1ContractFixture struct {
	SchemaVersion           string   `json:"schema_version"`
	CircuitID               string   `json:"circuit_id"`
	MaxInputs               uint32   `json:"max_inputs"`
	MaxOutputs              uint32   `json:"max_outputs"`
	PublicInputs            []string `json:"public_inputs"`
	PublicInputSchemaSHA256 string   `json:"public_input_schema_sha256"`
	Vector                  struct {
		Kind         string   `json:"kind"`
		Capacity     uint32   `json:"capacity"`
		Count        uint32   `json:"count"`
		ActiveValues []string `json:"active_values"`
		LeafDomain   string   `json:"leaf_domain"`
		NodeDomain   string   `json:"node_domain"`
		RootDomain   string   `json:"root_domain"`
		RootHex      string   `json:"root_hex"`
	} `json:"vector"`
	UserDisclosure struct {
		Count       uint32   `json:"count"`
		Policies    []uint32 `json:"policies"`
		RawDigests  []string `json:"raw_digests"`
		ValueDomain string   `json:"value_domain"`
		LeafDomain  string   `json:"leaf_domain"`
		NodeDomain  string   `json:"node_domain"`
		RootDomain  string   `json:"root_domain"`
		RootHex     string   `json:"root_hex"`
	} `json:"user_disclosure"`
	Effect struct {
		ChainDomainHi      string `json:"chain_domain_hi"`
		ChainDomainLo      string `json:"chain_domain_lo"`
		MerkleRoot         string `json:"merkle_root"`
		InputCount         uint32 `json:"input_count"`
		OutputCount        uint32 `json:"output_count"`
		NullifierRoot      string `json:"nullifier_root"`
		CommitmentRoot     string `json:"commitment_root"`
		UserDisclosureRoot string `json:"user_disclosure_root"`
		FullDisclosureRoot string `json:"full_disclosure_root"`
		PayloadDigestHi    string `json:"payload_digest_hi"`
		PayloadDigestLo    string `json:"payload_digest_lo"`
		ExpiresAtUnix      int64  `json:"expires_at_unix"`
		IDHex              string `json:"id_hex"`
	} `json:"effect"`
	WireState struct {
		TxBytes            int `json:"tx_bytes"`
		TypedScanKVBytes   int `json:"typed_scan_kv_bytes"`
		TotalKVWriteBytes  int `json:"total_kv_write_bytes"`
		ABCIEventBytes     int `json:"abci_event_bytes"`
		QueryResponseBytes int `json:"query_response_bytes"`
	} `json:"wire_state"`
}

func TestPrivacyNoteV1ContractIndependentGolden(t *testing.T) {
	var fixture noteV1ContractFixture
	loadSession2Fixture(t, "privacy_note_v1_contract.json", &fixture)
	require.Equal(t, "v1", fixture.SchemaVersion)
	require.Equal(t, "SHA-256(\"clairveil.field-domain.v1\" || u32be(len(label)) || label) mod Fr", fixture.Domains.FieldDerivation)

	for name, label := range fixture.Domains.Labels {
		require.Equal(t, fieldHex(referenceDomainFieldContract(label)), fixture.Domains.ConstantsHex[name])
	}

	spendScalar := mustDecimal(t, fixture.Vector.SpendScalar)
	viewScalar := mustDecimal(t, fixture.Vector.ViewScalar)
	spend := referencePointContract(spendScalar)
	view := referencePointContract(viewScalar)
	assetID := referenceAssetIDContract(fixture.Vector.Denom)
	amount := mustDecimal(t, fixture.Vector.Amount)
	randomness := mustDecimal(t, fixture.Vector.Randomness)
	commitment := referenceMIMCContract(
		referenceDomainFieldContract(fixture.Domains.Labels["note_commitment"]),
		pointCoordinateContract(spend, true), pointCoordinateContract(spend, false),
		pointCoordinateContract(view, true), pointCoordinateContract(view, false),
		amount, assetID, randomness,
	)
	nullifier := referenceMIMCContract(
		referenceDomainFieldContract(fixture.Domains.Labels["note_nullifier"]),
		commitment, randomness,
		pointCoordinateContract(spend, true), pointCoordinateContract(spend, false),
	)
	require.Equal(t, fixture.Vector.AssetIDHex, fieldHex(assetID))
	require.Equal(t, fixture.Vector.Commitment, fieldHex(commitment))
	require.Equal(t, fixture.Vector.Nullifier, fieldHex(nullifier))

	empty := big.NewInt(0)
	var emptyAtOne, emptyAtTwo *big.Int
	for level := uint32(0); level < 32; level++ {
		empty = referenceMIMCContract(
			referenceDomainFieldContract(fixture.Domains.Labels["tree_node"]),
			new(big.Int).SetUint64(uint64(level)), empty, empty,
		)
		if level == 0 {
			emptyAtOne = new(big.Int).Set(empty)
		}
		if level == 1 {
			emptyAtTwo = new(big.Int).Set(empty)
		}
	}
	require.Equal(t, fixture.Vector.EmptyRootOne, fieldHex(emptyAtOne))
	require.Equal(t, fixture.Vector.EmptyRootTwo, fieldHex(emptyAtTwo))
	require.Equal(t, fixture.Vector.EmptyRoot32, fieldHex(empty))
	require.Equal(t, privacytypes.FixedPayloadVersionV1, fixture.Encoding.Version)
	require.Equal(t, privacytypes.NotePlaintextV1Size, fixture.Encoding.NotePlaintextBytes)
	require.Equal(t, privacytypes.DisclosurePlaintextV1Size, fixture.Encoding.DisclosurePlaintextBytes)
	require.Equal(t, privacytypes.EncryptedEnvelopeV1HeaderSize, fixture.Encoding.EnvelopeHeaderBytes)
}

func TestPrivacyBatchJoinSplitV1ContractIndependentGolden(t *testing.T) {
	var fixture batchJoinSplitV1ContractFixture
	loadSession2Fixture(t, "privacy_batch_joinsplit_v1_contract.json", &fixture)
	require.Equal(t, "v1", fixture.SchemaVersion)
	require.Equal(t, privacytypes.BatchJoinSplitV1MaxInputs, fixture.MaxInputs)
	require.Equal(t, privacytypes.BatchJoinSplitV1MaxOutputs, fixture.MaxOutputs)
	require.Equal(t, privacytypes.BatchPublicInputOrderV1[:], fixture.PublicInputs)
	schemaDigest, err := privacyzk.PublicInputSchemaSHA256(fixture.CircuitID)
	require.NoError(t, err)
	require.Equal(t, fixture.PublicInputSchemaSHA256, schemaDigest)

	values := make([]*big.Int, fixture.Vector.Capacity)
	for i := range values {
		values[i] = new(big.Int)
	}
	for i, decimal := range fixture.Vector.ActiveValues {
		values[i] = mustDecimal(t, decimal)
	}
	layer := make([]*big.Int, fixture.Vector.Capacity)
	for i := range layer {
		enabled := big.NewInt(0)
		if uint32(i) < fixture.Vector.Count {
			enabled = big.NewInt(1)
		}
		layer[i] = referenceMIMCContract(referenceDomainFieldContract(fixture.Vector.LeafDomain), big.NewInt(int64(i)), enabled, values[i])
	}
	for level := uint32(0); len(layer) > 1; level++ {
		next := make([]*big.Int, len(layer)/2)
		for i := 0; i < len(layer); i += 2 {
			next[i/2] = referenceMIMCContract(referenceDomainFieldContract(fixture.Vector.NodeDomain), new(big.Int).SetUint64(uint64(level)), layer[i], layer[i+1])
		}
		layer = next
	}
	root := referenceMIMCContract(referenceDomainFieldContract(fixture.Vector.RootDomain), new(big.Int).SetUint64(uint64(fixture.Vector.Capacity)), new(big.Int).SetUint64(uint64(fixture.Vector.Count)), layer[0])
	require.Equal(t, fixture.Vector.RootHex, fieldHex(root))

	userValues := make([]*big.Int, privacytypes.BatchJoinSplitV1MaxOutputs)
	for i := range userValues {
		userValues[i] = new(big.Int)
	}
	for i := uint32(0); i < fixture.UserDisclosure.Count; i++ {
		userValues[i] = referenceMIMCContract(
			referenceDomainFieldContract(fixture.UserDisclosure.ValueDomain),
			new(big.Int).SetUint64(uint64(i)), big.NewInt(1),
			new(big.Int).SetUint64(uint64(fixture.UserDisclosure.Policies[i])),
			mustDecimal(t, fixture.UserDisclosure.RawDigests[i]),
		)
	}
	userLayer := make([]*big.Int, len(userValues))
	for i := range userValues {
		enabled := big.NewInt(0)
		if uint32(i) < fixture.UserDisclosure.Count {
			enabled = big.NewInt(1)
		}
		userLayer[i] = referenceMIMCContract(referenceDomainFieldContract(fixture.UserDisclosure.LeafDomain), big.NewInt(int64(i)), enabled, userValues[i])
	}
	for level := uint32(0); len(userLayer) > 1; level++ {
		next := make([]*big.Int, len(userLayer)/2)
		for i := 0; i < len(userLayer); i += 2 {
			next[i/2] = referenceMIMCContract(referenceDomainFieldContract(fixture.UserDisclosure.NodeDomain), new(big.Int).SetUint64(uint64(level)), userLayer[i], userLayer[i+1])
		}
		userLayer = next
	}
	userRoot := referenceMIMCContract(
		referenceDomainFieldContract(fixture.UserDisclosure.RootDomain),
		new(big.Int).SetUint64(uint64(privacytypes.BatchJoinSplitV1MaxOutputs)),
		new(big.Int).SetUint64(uint64(fixture.UserDisclosure.Count)), userLayer[0],
	)
	require.Equal(t, fixture.UserDisclosure.RootHex, fieldHex(userRoot))

	effectBytes := referenceBatchEffectContract(fixture)
	require.Equal(t, fixture.Effect.IDHex, hex.EncodeToString(effectBytes[:]))
	require.Equal(t, 65265, fixture.WireState.TxBytes)
	require.Equal(t, 74148, fixture.WireState.TypedScanKVBytes)
	require.Equal(t, 172452, fixture.WireState.TotalKVWriteBytes)
	require.Equal(t, 584, fixture.WireState.ABCIEventBytes)
	require.Equal(t, 73594, fixture.WireState.QueryResponseBytes)
}

func loadSession2Fixture(t *testing.T, name string, target any) {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	bz, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "testdata", name))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(bz, target))
}

func referenceDomainFieldContract(label string) *big.Int {
	h := sha256.New()
	_, _ = h.Write([]byte("clairveil.field-domain.v1"))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(label)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(label))
	return new(big.Int).Mod(new(big.Int).SetBytes(h.Sum(nil)), fr.Modulus())
}

func referenceAssetIDContract(denom string) *big.Int {
	h := sha256.New()
	_, _ = h.Write([]byte("clairveil.asset-id.v1"))
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(denom)))
	_, _ = h.Write(length[:])
	_, _ = h.Write([]byte(denom))
	return new(big.Int).Mod(new(big.Int).SetBytes(h.Sum(nil)), fr.Modulus())
}

func referenceMIMCContract(values ...*big.Int) *big.Int {
	h := fr_mimc.NewMiMC()
	for _, value := range values {
		var element fr.Element
		element.SetBigInt(value)
		encoded := element.Bytes()
		_, _ = h.Write(encoded[:])
	}
	return new(big.Int).SetBytes(h.Sum(nil))
}

func referencePointContract(scalar *big.Int) crypto_tedwards.PointAffine {
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, scalar)
	return point
}

func pointCoordinateContract(point crypto_tedwards.PointAffine, x bool) *big.Int {
	if x {
		return point.X.BigInt(new(big.Int))
	}
	return point.Y.BigInt(new(big.Int))
}

func mustDecimal(t *testing.T, decimal string) *big.Int {
	t.Helper()
	value, ok := new(big.Int).SetString(decimal, 10)
	require.True(t, ok)
	return value
}

func fieldHex(value *big.Int) string {
	return hex.EncodeToString(value.FillBytes(make([]byte, 32)))
}

func referenceBatchEffectContract(fixture batchJoinSplitV1ContractFixture) [sha256.Size]byte {
	h := sha256.New()
	_, _ = h.Write([]byte("clairveil.batch-effect.v1"))
	writeField := func(decimal string) { _, _ = h.Write(mustDecimalForFixture(decimal).FillBytes(make([]byte, 32))) }
	writeField(fixture.Effect.ChainDomainHi)
	writeField(fixture.Effect.ChainDomainLo)
	writeField(fixture.Effect.MerkleRoot)
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], fixture.Effect.InputCount)
	_, _ = h.Write(count[:])
	binary.BigEndian.PutUint32(count[:], fixture.Effect.OutputCount)
	_, _ = h.Write(count[:])
	for _, value := range []string{fixture.Effect.NullifierRoot, fixture.Effect.CommitmentRoot, fixture.Effect.UserDisclosureRoot, fixture.Effect.FullDisclosureRoot, fixture.Effect.PayloadDigestHi, fixture.Effect.PayloadDigestLo} {
		writeField(value)
	}
	var expiry [8]byte
	binary.BigEndian.PutUint64(expiry[:], uint64(fixture.Effect.ExpiresAtUnix))
	_, _ = h.Write(expiry[:])
	var result [sha256.Size]byte
	copy(result[:], h.Sum(nil))
	return result
}

func mustDecimalForFixture(decimal string) *big.Int {
	value, ok := new(big.Int).SetString(decimal, 10)
	if !ok {
		panic("invalid static decimal fixture")
	}
	return value
}
