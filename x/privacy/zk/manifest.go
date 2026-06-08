package zk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	ArtifactManifestFile       = "privacy_zk_manifest.json"
	LegacyChecksumsJSONFile    = "privacy_zk_checksums.json"
	CircuitConfigSchemaVersion = "v1"
	ActiveCircuitSetID         = "privacy-accounting-v2"
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
	SchemaVersion string               `json:"schema_version"`
	GeneratedAt   string               `json:"generated_at,omitempty"`
	Curve         string               `json:"curve"`
	ActiveSetID   string               `json:"active_set_id"`
	ArtifactDir   string               `json:"artifact_dir,omitempty"`
	Artifacts     []ArtifactDescriptor `json:"artifacts"`
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
	}
}

func ManifestFromChecksums(outDir, generatedAt string, checksums map[string]string) RuntimeArtifactManifest {
	descriptors := DefaultArtifactDescriptors()
	for i := range descriptors {
		descriptors[i].SHA256 = checksums[descriptors[i].ChecksumEnv]
	}

	return RuntimeArtifactManifest{
		SchemaVersion: CircuitConfigSchemaVersion,
		GeneratedAt:   generatedAt,
		Curve:         CircuitCurve,
		ActiveSetID:   ActiveCircuitSetID,
		ArtifactDir:   outDir,
		Artifacts:     descriptors,
	}
}

func LoadArtifactManifest(path string) (*RuntimeArtifactManifest, error) {
	bz, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var manifest RuntimeArtifactManifest
	if err := json.Unmarshal(bz, &manifest); err == nil && len(manifest.Artifacts) != 0 {
		return &manifest, nil
	}

	var legacy legacyChecksumsManifest
	if err := json.Unmarshal(bz, &legacy); err != nil {
		return nil, fmt.Errorf("failed to decode artifact manifest: %w", err)
	}

	converted := ManifestFromChecksums(legacy.ArtifactDir, legacy.GeneratedAt, legacy.Checksums)
	if legacy.Curve != "" {
		converted.Curve = legacy.Curve
	}
	return &converted, nil
}

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
