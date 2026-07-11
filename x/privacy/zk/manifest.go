package zk

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const (
	ArtifactManifestFile       = "privacy_zk_manifest.json"
	LegacyChecksumsJSONFile    = "privacy_zk_checksums.json"
	CircuitConfigSchemaVersion = "v2"
	ActiveCircuitSetID         = privacytypes.ActiveCircuitSetID
	CircuitCurve               = "BN254"

	ChecksumSourceManifest = "manifest"
	ChecksumSourceEnv      = "env"
	ChecksumSourceNone     = "none"
)

type ArtifactDescriptor struct {
	CircuitID    string `json:"circuit_id"`
	ArtifactType string `json:"artifact_type"`
	Filename     string `json:"filename"`
	ChecksumEnv  string `json:"checksum_env"`
	SHA256       string `json:"sha256,omitempty"`
}

type RuntimeArtifactManifest struct {
	SchemaVersion      string                           `json:"schema_version"`
	GeneratedAt        string                           `json:"generated_at,omitempty"`
	Curve              string                           `json:"curve"`
	ActiveSetID        string                           `json:"active_set_id"`
	ArtifactDir        string                           `json:"artifact_dir,omitempty"`
	Artifacts          []ArtifactDescriptor             `json:"artifacts"`
	CircuitSetIdentity *privacytypes.CircuitSetIdentity `json:"circuit_set_identity"`
}

type legacyChecksumsManifest struct {
	GeneratedAt string            `json:"generated_at"`
	Curve       string            `json:"curve"`
	ArtifactDir string            `json:"artifact_dir"`
	Checksums   map[string]string `json:"checksums"`
}

func DefaultArtifactDescriptors() []ArtifactDescriptor {
	return []ArtifactDescriptor{
		{
			CircuitID:    "deposit",
			ArtifactType: "r1cs",
			Filename:     DepositR1CSFile,
			ChecksumEnv:  DepositR1CSSHA256Env,
		},
		{
			CircuitID:    "deposit",
			ArtifactType: "proving_key",
			Filename:     DepositPKFile,
			ChecksumEnv:  DepositPKSHA256Env,
		},
		{
			CircuitID:    "deposit",
			ArtifactType: "verifying_key",
			Filename:     DepositVKFile,
			ChecksumEnv:  DepositVKSHA256Env,
		},
		{
			CircuitID:    "spend",
			ArtifactType: "r1cs",
			Filename:     SpendR1CSFile,
			ChecksumEnv:  SpendR1CSSHA256Env,
		},
		{
			CircuitID:    "spend",
			ArtifactType: "proving_key",
			Filename:     SpendPKFile,
			ChecksumEnv:  SpendPKSHA256Env,
		},
		{
			CircuitID:    "spend",
			ArtifactType: "verifying_key",
			Filename:     SpendVKFile,
			ChecksumEnv:  SpendVKSHA256Env,
		},
		{
			CircuitID:    "joinsplit",
			ArtifactType: "r1cs",
			Filename:     JoinSplitR1CSFile,
			ChecksumEnv:  JoinSplitR1CSSHA256Env,
		},
		{
			CircuitID:    "joinsplit",
			ArtifactType: "proving_key",
			Filename:     JoinSplitPKFile,
			ChecksumEnv:  JoinSplitPKSHA256Env,
		},
		{
			CircuitID:    "joinsplit",
			ArtifactType: "verifying_key",
			Filename:     JoinSplitVKFile,
			ChecksumEnv:  JoinSplitVKSHA256Env,
		},
		{
			CircuitID:    "batch-joinsplit-16x32-v1",
			ArtifactType: "r1cs",
			Filename:     BatchJoinSplit16x32R1CSFile,
			ChecksumEnv:  BatchJoinSplit16x32R1CSSHA256Env,
		},
		{
			CircuitID:    "batch-joinsplit-16x32-v1",
			ArtifactType: "proving_key",
			Filename:     BatchJoinSplit16x32PKFile,
			ChecksumEnv:  BatchJoinSplit16x32PKSHA256Env,
		},
		{
			CircuitID:    "batch-joinsplit-16x32-v1",
			ArtifactType: "verifying_key",
			Filename:     BatchJoinSplit16x32VKFile,
			ChecksumEnv:  BatchJoinSplit16x32VKSHA256Env,
		},
	}
}

func ManifestFromChecksums(outDir, generatedAt string, checksums map[string]string) RuntimeArtifactManifest {
	descriptors := DefaultArtifactDescriptors()
	for i := range descriptors {
		descriptors[i].SHA256 = checksums[descriptors[i].ChecksumEnv]
	}

	identity, _ := CircuitSetIdentityFromChecksums(checksums)
	return RuntimeArtifactManifest{
		SchemaVersion:      CircuitConfigSchemaVersion,
		GeneratedAt:        generatedAt,
		Curve:              CircuitCurve,
		ActiveSetID:        ActiveCircuitSetID,
		ArtifactDir:        outDir,
		Artifacts:          descriptors,
		CircuitSetIdentity: identity,
	}
}

func CircuitSetIdentityFromChecksums(checksums map[string]string) (*privacytypes.CircuitSetIdentity, error) {
	identity := &privacytypes.CircuitSetIdentity{
		SchemaVersion: privacytypes.CircuitSetIdentitySchemaVersion,
		CircuitSetId:  ActiveCircuitSetID,
		Curve:         CircuitCurve,
		Circuits:      make([]*privacytypes.CircuitIdentity, 0, len(privacytypes.RequiredCircuitIdentityOrder)),
	}
	for _, circuitID := range privacytypes.RequiredCircuitIdentityOrder {
		vkEnv := verifyingKeyChecksumEnv(circuitID)
		schemaDigest, err := PublicInputSchemaSHA256(circuitID)
		if err != nil {
			return nil, err
		}
		identity.Circuits = append(identity.Circuits, &privacytypes.CircuitIdentity{
			CircuitId:               circuitID,
			VerifyingKeySha256:      strings.TrimSpace(checksums[vkEnv]),
			PublicInputSchemaSha256: schemaDigest,
		})
	}
	return identity, nil
}

func LoadArtifactManifest(path string) (*RuntimeArtifactManifest, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var shape map[string]json.RawMessage
	if err := json.Unmarshal(bz, &shape); err != nil {
		return nil, fmt.Errorf("failed to decode artifact manifest: %w", err)
	}
	if _, structured := shape["artifacts"]; structured {
		var manifest RuntimeArtifactManifest
		if err := decodeRuntimeArtifactManifest(bz, &manifest); err != nil {
			return nil, fmt.Errorf("failed to decode structured artifact manifest: %w", err)
		}
		if err := ValidateRuntimeArtifactManifest(&manifest); err != nil {
			return nil, err
		}
		return &manifest, nil
	}

	var legacy legacyChecksumsManifest
	if err := json.Unmarshal(bz, &legacy); err != nil {
		return nil, fmt.Errorf("failed to decode artifact manifest: %w", err)
	}

	return nil, fmt.Errorf("legacy artifact manifests are not accepted for circuit set %s", ActiveCircuitSetID)
}

func decodeRuntimeArtifactManifest(bz []byte, manifest *RuntimeArtifactManifest) error {
	decoder := json.NewDecoder(bytes.NewReader(bz))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(manifest); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return fmt.Errorf("artifact manifest contains trailing JSON values")
		}
		return fmt.Errorf("artifact manifest trailing data: %w", err)
	}
	return nil
}

func ValidateRuntimeArtifactManifest(manifest *RuntimeArtifactManifest) error {
	if manifest == nil {
		return fmt.Errorf("artifact manifest is required")
	}
	if manifest.SchemaVersion != CircuitConfigSchemaVersion {
		return fmt.Errorf("artifact manifest schema_version must be %q", CircuitConfigSchemaVersion)
	}
	if manifest.ActiveSetID != ActiveCircuitSetID {
		return fmt.Errorf("artifact manifest active_set_id must be %q", ActiveCircuitSetID)
	}
	if manifest.Curve != CircuitCurve {
		return fmt.Errorf("artifact manifest curve must be %q", CircuitCurve)
	}
	expected := DefaultArtifactDescriptors()
	if len(manifest.Artifacts) != len(expected) {
		return fmt.Errorf("artifact manifest must contain exactly %d artifacts", len(expected))
	}
	for i := range expected {
		got := manifest.Artifacts[i]
		want := expected[i]
		if got.CircuitID != want.CircuitID || got.ArtifactType != want.ArtifactType || got.Filename != want.Filename || got.ChecksumEnv != want.ChecksumEnv {
			return fmt.Errorf("artifact manifest descriptor %d does not match the canonical descriptor", i)
		}
		if err := validateExpectedSHA256(got.Filename, got.SHA256); err != nil {
			return err
		}
		if got.SHA256 != strings.ToLower(got.SHA256) {
			return fmt.Errorf("artifact manifest sha256 for %s must be lowercase", got.Filename)
		}
	}
	if err := privacytypes.ValidateCircuitSetIdentity(manifest.CircuitSetIdentity); err != nil {
		return fmt.Errorf("artifact manifest circuit_set_identity: %w", err)
	}
	expectedIdentity, err := identityFromArtifactDescriptors(manifest.Artifacts)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(expectedIdentity, manifest.CircuitSetIdentity) {
		return fmt.Errorf("artifact manifest circuit_set_identity does not match VK and schema descriptors")
	}
	return nil
}

func identityFromArtifactDescriptors(descriptors []ArtifactDescriptor) (*privacytypes.CircuitSetIdentity, error) {
	checksums := make(map[string]string, len(descriptors))
	for _, descriptor := range descriptors {
		checksums[descriptor.ChecksumEnv] = descriptor.SHA256
	}
	return CircuitSetIdentityFromChecksums(checksums)
}

func LoadLocalCircuitSetIdentity() (*privacytypes.CircuitSetIdentity, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.LocalCircuitSetIdentity()
}

func verifyingKeyFilename(circuitID string) string {
	switch circuitID {
	case "deposit":
		return DepositVKFile
	case "spend":
		return SpendVKFile
	case "joinsplit":
		return JoinSplitVKFile
	case "batch-joinsplit-16x32-v1":
		return BatchJoinSplit16x32VKFile
	default:
		return ""
	}
}

func verifyingKeyChecksumEnv(circuitID string) string {
	switch circuitID {
	case "deposit":
		return DepositVKSHA256Env
	case "spend":
		return SpendVKSHA256Env
	case "joinsplit":
		return JoinSplitVKSHA256Env
	case "batch-joinsplit-16x32-v1":
		return BatchJoinSplit16x32VKSHA256Env
	default:
		return ""
	}
}

// ResolveRuntimeArtifactManifest is retained for setup/reporting compatibility.
// RuntimeArtifactRegistry always requires the structured manifest and never
// treats its environment-only fallback as an artifact trust root.
func ResolveRuntimeArtifactManifest() (*RuntimeArtifactManifest, string, error) {
	manifestPath := filepath.Join(artifactDir(), ArtifactManifestFile)
	if _, err := os.Stat(manifestPath); err == nil {
		manifest, err := LoadArtifactManifest(manifestPath)
		if err != nil {
			return nil, "", err
		}
		return manifest, ChecksumSourceManifest, nil
	} else if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}

	legacyPath := filepath.Join(artifactDir(), LegacyChecksumsJSONFile)
	if _, err := os.Stat(legacyPath); err == nil {
		manifest, err := LoadArtifactManifest(legacyPath)
		if err != nil {
			return nil, "", err
		}
		return manifest, ChecksumSourceManifest, nil
	} else if err != nil && !os.IsNotExist(err) {
		return nil, "", err
	}

	checksums := make(map[string]string)
	checksumSource := ChecksumSourceNone
	for _, descriptor := range DefaultArtifactDescriptors() {
		value := expectedChecksumFromEnv(descriptor.Filename)
		if value == "" {
			continue
		}
		checksums[descriptor.ChecksumEnv] = value
		checksumSource = ChecksumSourceEnv
	}

	manifest := ManifestFromChecksums(artifactDir(), "", checksums)
	return &manifest, checksumSource, nil
}
