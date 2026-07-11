package zk

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
)

const (
	DepositR1CSFile = "privacy_deposit_r1cs.bin"
	DepositPKFile   = "privacy_deposit_pk.bin"
	DepositVKFile   = "privacy_deposit_vk.bin"

	SpendR1CSFile = "privacy_spend_r1cs.bin"
	SpendPKFile   = "privacy_spend_pk.bin"
	SpendVKFile   = "privacy_spend_vk.bin"

	JoinSplitR1CSFile = "privacy_joinsplit_r1cs.bin"
	JoinSplitPKFile   = "privacy_joinsplit_pk.bin"
	JoinSplitVKFile   = "privacy_joinsplit_vk.bin"

	ZKArtifactDirEnv = "CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR"

	// ZKRuntimeEnvironmentEnv and ZKAllowDevelopmentArtifactOverrideEnv form a
	// two-part guard for local development checksum overrides. Production is the
	// default and never accepts a checksum that differs from the manifest.
	ZKRuntimeEnvironmentEnv               = "CLAIRVEIL_PRIVACY_ZK_RUNTIME_ENVIRONMENT"
	ZKAllowDevelopmentArtifactOverrideEnv = "CLAIRVEIL_PRIVACY_ZK_ALLOW_DEVELOPMENT_ARTIFACT_OVERRIDE"
	ZKRuntimeEnvironmentProduction        = "production"
	ZKRuntimeEnvironmentDevelopment       = "development"

	DepositR1CSSHA256Env   = "CLAIRVEIL_PRIVACY_DEPOSIT_R1CS_SHA256"
	DepositPKSHA256Env     = "CLAIRVEIL_PRIVACY_DEPOSIT_PK_SHA256"
	DepositVKSHA256Env     = "CLAIRVEIL_PRIVACY_DEPOSIT_VK_SHA256"
	SpendR1CSSHA256Env     = "CLAIRVEIL_PRIVACY_SPEND_R1CS_SHA256"
	SpendPKSHA256Env       = "CLAIRVEIL_PRIVACY_SPEND_PK_SHA256"
	SpendVKSHA256Env       = "CLAIRVEIL_PRIVACY_SPEND_VK_SHA256"
	JoinSplitR1CSSHA256Env = "CLAIRVEIL_PRIVACY_JOINSPLIT_R1CS_SHA256"
	JoinSplitPKSHA256Env   = "CLAIRVEIL_PRIVACY_JOINSPLIT_PK_SHA256"
	JoinSplitVKSHA256Env   = "CLAIRVEIL_PRIVACY_JOINSPLIT_VK_SHA256"
)

var (
	defaultRegistryMu  sync.Mutex
	defaultRegistry    *ArtifactRegistry
	defaultRegistryErr error
	defaultRegistryEnv string
)

func artifactDir() string {
	if dir := strings.TrimSpace(os.Getenv(ZKArtifactDirEnv)); dir != "" {
		return dir
	}
	return "."
}

func artifactPath(filename string) string {
	return filepath.Join(artifactDir(), filename)
}

// DefaultArtifactRegistry returns the process registry used by compatibility
// getters. It rotates only when artifact-related process environment changes,
// which keeps tests that use t.Setenv isolated. Long-lived services should
// resolve it once and inject the returned registry. Other callers that need
// isolation should construct a registry with NewArtifactRegistry.
func DefaultArtifactRegistry() (*ArtifactRegistry, error) {
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	fingerprint := runtimeArtifactRegistryEnvironmentFingerprint()
	if (defaultRegistry == nil && defaultRegistryErr == nil) || defaultRegistryEnv != fingerprint {
		defaultRegistry, defaultRegistryErr = NewRuntimeArtifactRegistry()
		defaultRegistryEnv = fingerprint
	}
	return defaultRegistry, defaultRegistryErr
}

func resetDefaultArtifactRegistryForTest() {
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	defaultRegistry = nil
	defaultRegistryErr = nil
	defaultRegistryEnv = ""
}

func installValidatedDefaultArtifactRegistry(registry *ArtifactRegistry) {
	if registry == nil {
		return
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	defaultRegistry = registry
	defaultRegistryErr = nil
	defaultRegistryEnv = registry.runtimeEnvFingerprint
}

func invalidateMatchingDefaultArtifactRegistry(registry *ArtifactRegistry) {
	if registry == nil {
		return
	}
	defaultRegistryMu.Lock()
	defer defaultRegistryMu.Unlock()
	if defaultRegistryEnv != registry.runtimeEnvFingerprint {
		return
	}
	defaultRegistry = nil
	defaultRegistryErr = nil
	defaultRegistryEnv = ""
}

func runtimeArtifactRegistryEnvironmentFingerprint() string {
	names := []string{
		ZKArtifactDirEnv,
		ZKRuntimeEnvironmentEnv,
		ZKAllowDevelopmentArtifactOverrideEnv,
		DepositR1CSSHA256Env,
		DepositPKSHA256Env,
		DepositVKSHA256Env,
		SpendR1CSSHA256Env,
		SpendPKSHA256Env,
		SpendVKSHA256Env,
		JoinSplitR1CSSHA256Env,
		JoinSplitPKSHA256Env,
		JoinSplitVKSHA256Env,
	}
	var fingerprint strings.Builder
	for _, name := range names {
		fingerprint.WriteString(name)
		fingerprint.WriteByte('=')
		fingerprint.WriteString(os.Getenv(name))
		fingerprint.WriteByte(0)
	}
	return fingerprint.String()
}

func loadZKSetup() error {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return err
	}
	return registry.CheckReadiness(ArtifactRoleProver, RequiredCircuitIDs(), nil)
}

func ValidateZKSetup() error {
	return loadZKSetup()
}

// ValidateZKArtifacts validates all currently active prover artifacts using a
// fresh registry. A failed preflight therefore cannot poison the lazy runtime
// cache used by later proof requests.
func ValidateZKArtifacts() error {
	registry, err := NewRuntimeArtifactRegistry()
	if err != nil {
		return err
	}
	return registry.CheckReadiness(ArtifactRoleProver, RequiredCircuitIDs(), nil)
}

func expectedChecksumFromEnv(filename string) string {
	switch filename {
	case DepositR1CSFile:
		return os.Getenv(DepositR1CSSHA256Env)
	case DepositPKFile:
		return os.Getenv(DepositPKSHA256Env)
	case DepositVKFile:
		return os.Getenv(DepositVKSHA256Env)
	case SpendR1CSFile:
		return os.Getenv(SpendR1CSSHA256Env)
	case SpendPKFile:
		return os.Getenv(SpendPKSHA256Env)
	case SpendVKFile:
		return os.Getenv(SpendVKSHA256Env)
	case JoinSplitR1CSFile:
		return os.Getenv(JoinSplitR1CSSHA256Env)
	case JoinSplitPKFile:
		return os.Getenv(JoinSplitPKSHA256Env)
	case JoinSplitVKFile:
		return os.Getenv(JoinSplitVKSHA256Env)
	default:
		return ""
	}
}

func validateExpectedSHA256(filename, expected string) error {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("artifact manifest sha256 for %s must be a 64-character hex string", filename)
	}
	return nil
}

func expectedChecksum(filename string) (string, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return "", err
	}
	manifest, err := registry.loadManifest()
	if err != nil {
		return "", err
	}
	for _, descriptor := range manifest.Artifacts {
		if descriptor.Filename == filename {
			return registry.artifactChecksum(descriptor, false)
		}
	}
	return "", fmt.Errorf("artifact manifest does not describe %s", filename)
}

func GetDepositProvingKey() (groth16.ProvingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.ProvingKey(CircuitDeposit)
}

func GetDepositVerifyingKey() (groth16.VerifyingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.VerifyingKey(CircuitDeposit)
}

func GetDepositR1CS() (constraint.ConstraintSystem, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.R1CS(CircuitDeposit)
}

func GetSpendProvingKey() (groth16.ProvingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.ProvingKey(CircuitSpend)
}

func GetSpendVerifyingKey() (groth16.VerifyingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.VerifyingKey(CircuitSpend)
}

func GetSpendR1CS() (constraint.ConstraintSystem, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.R1CS(CircuitSpend)
}

func GetJoinSplitProvingKey() (groth16.ProvingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.ProvingKey(CircuitJoinSplit)
}

func GetJoinSplitVerifyingKey() (groth16.VerifyingKey, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.VerifyingKey(CircuitJoinSplit)
}

func GetJoinSplitR1CS() (constraint.ConstraintSystem, error) {
	registry, err := DefaultArtifactRegistry()
	if err != nil {
		return nil, err
	}
	return registry.R1CS(CircuitJoinSplit)
}

func runtimeEnvironmentFromEnv() (string, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(ZKRuntimeEnvironmentEnv)))
	if value == "" {
		return ZKRuntimeEnvironmentProduction, nil
	}
	switch value {
	case ZKRuntimeEnvironmentProduction, ZKRuntimeEnvironmentDevelopment:
		return value, nil
	default:
		return "", fmt.Errorf("%s must be %q or %q", ZKRuntimeEnvironmentEnv, ZKRuntimeEnvironmentProduction, ZKRuntimeEnvironmentDevelopment)
	}
}
