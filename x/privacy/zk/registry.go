package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type CircuitID string

const (
	CircuitDeposit   CircuitID = "deposit"
	CircuitSpend     CircuitID = "spend"
	CircuitJoinSplit CircuitID = "joinsplit"
)

type ArtifactRole string

const (
	ArtifactRoleValidator ArtifactRole = "validator"
	ArtifactRoleProver    ArtifactRole = "prover"
)

type ArtifactRegistryConfig struct {
	ArtifactDir              string
	ReadFile                 func(string) ([]byte, error)
	LookupEnv                func(string) (string, bool)
	RuntimeEnvironment       string
	AllowDevelopmentOverride bool
}

type artifactCacheEntry struct {
	once  sync.Once
	value any
	err   error
}

type ArtifactRegistry struct {
	artifactDir              string
	readFile                 func(string) ([]byte, error)
	lookupEnv                func(string) (string, bool)
	allowDevelopmentOverride bool
	runtimeEnvFingerprint    string

	manifestOnce sync.Once
	manifest     *RuntimeArtifactManifest
	manifestErr  error

	cacheMu sync.Mutex
	cache   map[string]*artifactCacheEntry
}

func RequiredCircuitIDs() []CircuitID {
	return []CircuitID{CircuitDeposit, CircuitSpend, CircuitJoinSplit}
}

func NewRuntimeArtifactRegistry() (*ArtifactRegistry, error) {
	runtimeEnvironment, err := runtimeEnvironmentFromEnv()
	if err != nil {
		return nil, err
	}
	allowOverride, err := parseStrictEnvironmentBool(ZKAllowDevelopmentArtifactOverrideEnv, os.Getenv(ZKAllowDevelopmentArtifactOverrideEnv))
	if err != nil {
		return nil, err
	}
	registry, err := NewArtifactRegistry(ArtifactRegistryConfig{
		ArtifactDir:              artifactDir(),
		ReadFile:                 os.ReadFile,
		LookupEnv:                os.LookupEnv,
		RuntimeEnvironment:       runtimeEnvironment,
		AllowDevelopmentOverride: allowOverride,
	})
	if err != nil {
		return nil, err
	}
	registry.runtimeEnvFingerprint = runtimeArtifactRegistryEnvironmentFingerprint()
	return registry, nil
}

func parseStrictEnvironmentBool(name, raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s must be %q or %q", name, "true", "false")
	}
}

func NewArtifactRegistry(config ArtifactRegistryConfig) (*ArtifactRegistry, error) {
	dir := strings.TrimSpace(config.ArtifactDir)
	if dir == "" {
		dir = "."
	}
	readFile := config.ReadFile
	if readFile == nil {
		readFile = os.ReadFile
	}
	lookupEnv := config.LookupEnv
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	runtimeEnvironment := strings.ToLower(strings.TrimSpace(config.RuntimeEnvironment))
	if runtimeEnvironment == "" {
		runtimeEnvironment = ZKRuntimeEnvironmentProduction
	}
	if runtimeEnvironment != ZKRuntimeEnvironmentProduction && runtimeEnvironment != ZKRuntimeEnvironmentDevelopment {
		return nil, fmt.Errorf("runtime environment must be %q or %q", ZKRuntimeEnvironmentProduction, ZKRuntimeEnvironmentDevelopment)
	}
	if config.AllowDevelopmentOverride && runtimeEnvironment != ZKRuntimeEnvironmentDevelopment {
		return nil, fmt.Errorf("artifact checksum override is development-only")
	}
	return &ArtifactRegistry{
		artifactDir:              dir,
		readFile:                 readFile,
		lookupEnv:                lookupEnv,
		allowDevelopmentOverride: config.AllowDevelopmentOverride,
		cache:                    make(map[string]*artifactCacheEntry),
	}, nil
}

func (r *ArtifactRegistry) ArtifactDir() string {
	if r == nil {
		return ""
	}
	return r.artifactDir
}

func (r *ArtifactRegistry) LocalCircuitSetIdentity() (*privacytypes.CircuitSetIdentity, error) {
	manifest, err := r.loadManifest()
	if err != nil {
		return nil, err
	}
	return privacytypes.CloneCircuitSetIdentity(manifest.CircuitSetIdentity), nil
}

// CheckReadiness verifies only the files required by role and circuits. For a
// validator, expectedConsensus is mandatory and cannot be overridden even by a
// development registry. Prover readiness validates only R1CS/PK for selected
// circuits and never reads their VK files; it also compares consensus metadata
// when expectedConsensus is supplied by the caller.
func (r *ArtifactRegistry) CheckReadiness(role ArtifactRole, circuits []CircuitID, expectedConsensus *privacytypes.CircuitSetIdentity) error {
	requested, err := normalizeCircuitIDs(circuits)
	if err != nil {
		return err
	}
	manifest, err := r.loadManifest()
	if err != nil {
		return err
	}

	switch role {
	case ArtifactRoleValidator:
		if expectedConsensus == nil {
			return fmt.Errorf("consensus circuit identity is required for validator readiness")
		}
		if err := privacytypes.ValidateCircuitSetIdentity(expectedConsensus); err != nil {
			return fmt.Errorf("invalid consensus circuit identity: %w", err)
		}
		if !reflect.DeepEqual(expectedConsensus, manifest.CircuitSetIdentity) {
			return fmt.Errorf("local circuit identity does not match consensus circuit identity")
		}
		for _, circuitID := range requested {
			descriptor, err := findArtifactDescriptor(manifest, circuitID, "verifying_key")
			if err != nil {
				return err
			}
			if configured, exists := r.lookupEnv(descriptor.ChecksumEnv); exists && strings.TrimSpace(configured) != "" {
				configured = strings.TrimSpace(configured)
				if err := validateExpectedSHA256(descriptor.Filename, configured); err != nil {
					return err
				}
				if !strings.EqualFold(configured, descriptor.SHA256) {
					return fmt.Errorf("environment checksum for %s cannot override consensus checksum", descriptor.Filename)
				}
			}
			if _, err := r.loadArtifact(circuitID, "verifying_key", false); err != nil {
				return err
			}
		}
		return nil
	case ArtifactRoleProver:
		if expectedConsensus != nil {
			if err := privacytypes.ValidateCircuitSetIdentity(expectedConsensus); err != nil {
				return fmt.Errorf("invalid consensus circuit identity: %w", err)
			}
			if !reflect.DeepEqual(expectedConsensus, manifest.CircuitSetIdentity) {
				return fmt.Errorf("local circuit identity does not match consensus circuit identity")
			}
		}
		for _, circuitID := range requested {
			if _, err := r.loadArtifact(circuitID, "r1cs", true); err != nil {
				return err
			}
			if _, err := r.loadArtifact(circuitID, "proving_key", true); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported artifact role %q", role)
	}
}

func (r *ArtifactRegistry) R1CS(circuitID CircuitID) (constraint.ConstraintSystem, error) {
	value, err := r.loadArtifact(circuitID, "r1cs", true)
	if err != nil {
		return nil, err
	}
	return value.(constraint.ConstraintSystem), nil
}

func (r *ArtifactRegistry) ProvingKey(circuitID CircuitID) (groth16.ProvingKey, error) {
	value, err := r.loadArtifact(circuitID, "proving_key", true)
	if err != nil {
		return nil, err
	}
	return value.(groth16.ProvingKey), nil
}

func (r *ArtifactRegistry) VerifyingKey(circuitID CircuitID) (groth16.VerifyingKey, error) {
	value, err := r.loadArtifact(circuitID, "verifying_key", false)
	if err != nil {
		return nil, err
	}
	return value.(groth16.VerifyingKey), nil
}

func (r *ArtifactRegistry) loadManifest() (*RuntimeArtifactManifest, error) {
	if r == nil {
		return nil, fmt.Errorf("artifact registry is required")
	}
	r.manifestOnce.Do(func() {
		path := filepath.Join(r.artifactDir, ArtifactManifestFile)
		bz, err := r.readFile(path)
		if err != nil {
			r.manifestErr = fmt.Errorf("read structured artifact manifest %s: %w", path, err)
			return
		}
		var manifest RuntimeArtifactManifest
		if err := decodeRuntimeArtifactManifest(bz, &manifest); err != nil {
			r.manifestErr = fmt.Errorf("decode structured artifact manifest %s: %w", path, err)
			return
		}
		if err := ValidateRuntimeArtifactManifest(&manifest); err != nil {
			r.manifestErr = err
			return
		}
		r.manifest = &manifest
	})
	return r.manifest, r.manifestErr
}

func (r *ArtifactRegistry) loadArtifact(circuitID CircuitID, artifactType string, allowDevelopmentOverride bool) (any, error) {
	if r == nil {
		return nil, fmt.Errorf("artifact registry is required")
	}
	if _, err := normalizeCircuitIDs([]CircuitID{circuitID}); err != nil {
		return nil, err
	}
	cacheKey := string(circuitID) + ":" + artifactType
	r.cacheMu.Lock()
	entry := r.cache[cacheKey]
	if entry == nil {
		entry = &artifactCacheEntry{}
		r.cache[cacheKey] = entry
	}
	r.cacheMu.Unlock()

	entry.once.Do(func() {
		entry.value, entry.err = r.readArtifact(circuitID, artifactType, allowDevelopmentOverride)
	})
	return entry.value, entry.err
}

func (r *ArtifactRegistry) readArtifact(circuitID CircuitID, artifactType string, allowDevelopmentOverride bool) (any, error) {
	manifest, err := r.loadManifest()
	if err != nil {
		return nil, err
	}
	descriptor, err := findArtifactDescriptor(manifest, circuitID, artifactType)
	if err != nil {
		return nil, err
	}
	expected, err := r.artifactChecksum(descriptor, allowDevelopmentOverride)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(r.artifactDir, descriptor.Filename)
	bz, err := r.readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s artifact %s: %w", artifactType, path, err)
	}
	sum := sha256.Sum256(bz)
	actual := hex.EncodeToString(sum[:])
	if actual != expected {
		return nil, fmt.Errorf("checksum mismatch for %s: expected %s, got %s", descriptor.Filename, expected, actual)
	}

	var object interface {
		ReadFrom(io.Reader) (int64, error)
	}
	switch artifactType {
	case "r1cs":
		object = groth16.NewCS(ecc.BN254)
	case "proving_key":
		object = groth16.NewProvingKey(ecc.BN254)
	case "verifying_key":
		object = groth16.NewVerifyingKey(ecc.BN254)
	default:
		return nil, fmt.Errorf("unsupported artifact type %q", artifactType)
	}
	read, err := object.ReadFrom(bytes.NewReader(bz))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", descriptor.Filename, err)
	}
	if read != int64(len(bz)) {
		return nil, fmt.Errorf("artifact %s has trailing bytes", descriptor.Filename)
	}
	if artifactType == "verifying_key" {
		var canonical bytes.Buffer
		if _, err := object.(interface {
			WriteTo(io.Writer) (int64, error)
		}).WriteTo(&canonical); err != nil {
			return nil, fmt.Errorf("re-encode %s: %w", descriptor.Filename, err)
		}
		if !bytes.Equal(canonical.Bytes(), bz) {
			return nil, fmt.Errorf("verifying key %s is not canonically encoded", descriptor.Filename)
		}
	}
	return object, nil
}

func (r *ArtifactRegistry) artifactChecksum(descriptor ArtifactDescriptor, allowDevelopmentOverride bool) (string, error) {
	expected := descriptor.SHA256
	configured, exists := r.lookupEnv(descriptor.ChecksumEnv)
	configured = strings.TrimSpace(configured)
	if !exists || configured == "" {
		return expected, nil
	}
	if err := validateExpectedSHA256(descriptor.Filename, configured); err != nil {
		return "", err
	}
	configured = strings.ToLower(configured)
	if configured == expected {
		return expected, nil
	}
	if allowDevelopmentOverride && r.allowDevelopmentOverride {
		return configured, nil
	}
	return "", fmt.Errorf("environment checksum for %s cannot override manifest checksum", descriptor.Filename)
}

func findArtifactDescriptor(manifest *RuntimeArtifactManifest, circuitID CircuitID, artifactType string) (ArtifactDescriptor, error) {
	for _, descriptor := range manifest.Artifacts {
		if descriptor.CircuitID == string(circuitID) && descriptor.ArtifactType == artifactType {
			return descriptor, nil
		}
	}
	return ArtifactDescriptor{}, fmt.Errorf("artifact manifest does not describe %s %s", circuitID, artifactType)
}

func normalizeCircuitIDs(circuits []CircuitID) ([]CircuitID, error) {
	if len(circuits) == 0 {
		return nil, fmt.Errorf("at least one circuit id is required")
	}
	seen := make(map[CircuitID]struct{}, len(circuits))
	for _, circuitID := range circuits {
		switch circuitID {
		case CircuitDeposit, CircuitSpend, CircuitJoinSplit:
		default:
			return nil, fmt.Errorf("unsupported circuit id %q", circuitID)
		}
		if _, duplicate := seen[circuitID]; duplicate {
			return nil, fmt.Errorf("duplicate circuit id %q", circuitID)
		}
		seen[circuitID] = struct{}{}
	}
	return append([]CircuitID(nil), circuits...), nil
}
