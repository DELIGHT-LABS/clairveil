package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
)

type loadedArtifacts struct {
	depositProvingKey   groth16.ProvingKey
	depositVerifyingKey groth16.VerifyingKey
	depositR1CS         constraint.ConstraintSystem

	spendProvingKey   groth16.ProvingKey
	spendVerifyingKey groth16.VerifyingKey
	spendR1CS         constraint.ConstraintSystem

	joinSplitProvingKey   groth16.ProvingKey
	joinSplitVerifyingKey groth16.VerifyingKey
	joinSplitR1CS         constraint.ConstraintSystem
}

var (
	depositProvingKey   groth16.ProvingKey
	depositVerifyingKey groth16.VerifyingKey
	depositR1CS         constraint.ConstraintSystem

	spendProvingKey   groth16.ProvingKey
	spendVerifyingKey groth16.VerifyingKey
	spendR1CS         constraint.ConstraintSystem

	joinSplitProvingKey   groth16.ProvingKey
	joinSplitVerifyingKey groth16.VerifyingKey
	joinSplitR1CS         constraint.ConstraintSystem

	setupErr error
	once     sync.Once
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

func loadZKSetup() error {
	once.Do(func() {
		setupErr = loadFromDisk()
	})
	return setupErr
}

func artifactDir() string {
	if dir := os.Getenv(ZKArtifactDirEnv); dir != "" {
		return dir
	}
	return "."
}

func artifactPath(filename string) string {
	return filepath.Join(artifactDir(), filename)
}

func loadFromDisk() error {
	artifacts, err := loadArtifacts(readFile)
	if err != nil {
		return err
	}

	depositProvingKey = artifacts.depositProvingKey
	depositVerifyingKey = artifacts.depositVerifyingKey
	depositR1CS = artifacts.depositR1CS
	spendProvingKey = artifacts.spendProvingKey
	spendVerifyingKey = artifacts.spendVerifyingKey
	spendR1CS = artifacts.spendR1CS
	joinSplitProvingKey = artifacts.joinSplitProvingKey
	joinSplitVerifyingKey = artifacts.joinSplitVerifyingKey
	joinSplitR1CS = artifacts.joinSplitR1CS
	return nil
}

func loadArtifacts(readFn func(string, interface {
	ReadFrom(r io.Reader) (int64, error)
}) error) (*loadedArtifacts, error) {
	artifacts := &loadedArtifacts{}

	artifacts.depositR1CS = groth16.NewCS(ecc.BN254)
	if err := readFn(DepositR1CSFile, artifacts.depositR1CS); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(DepositR1CSFile), err)
	}

	artifacts.depositProvingKey = groth16.NewProvingKey(ecc.BN254)
	if err := readFn(DepositPKFile, artifacts.depositProvingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(DepositPKFile), err)
	}

	artifacts.depositVerifyingKey = groth16.NewVerifyingKey(ecc.BN254)
	if err := readFn(DepositVKFile, artifacts.depositVerifyingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(DepositVKFile), err)
	}

	artifacts.spendR1CS = groth16.NewCS(ecc.BN254)
	if err := readFn(SpendR1CSFile, artifacts.spendR1CS); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(SpendR1CSFile), err)
	}

	artifacts.spendProvingKey = groth16.NewProvingKey(ecc.BN254)
	if err := readFn(SpendPKFile, artifacts.spendProvingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(SpendPKFile), err)
	}

	artifacts.spendVerifyingKey = groth16.NewVerifyingKey(ecc.BN254)
	if err := readFn(SpendVKFile, artifacts.spendVerifyingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(SpendVKFile), err)
	}

	artifacts.joinSplitR1CS = groth16.NewCS(ecc.BN254)
	if err := readFn(JoinSplitR1CSFile, artifacts.joinSplitR1CS); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(JoinSplitR1CSFile), err)
	}

	artifacts.joinSplitProvingKey = groth16.NewProvingKey(ecc.BN254)
	if err := readFn(JoinSplitPKFile, artifacts.joinSplitProvingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(JoinSplitPKFile), err)
	}

	artifacts.joinSplitVerifyingKey = groth16.NewVerifyingKey(ecc.BN254)
	if err := readFn(JoinSplitVKFile, artifacts.joinSplitVerifyingKey); err != nil {
		return nil, fmt.Errorf("load %s: %w", artifactPath(JoinSplitVKFile), err)
	}

	return artifacts, nil
}

func readFile(filename string, obj interface {
	ReadFrom(r io.Reader) (int64, error)
}) error {
	bz, err := os.ReadFile(artifactPath(filename))
	if err != nil {
		return err
	}

	expected, err := expectedChecksum(filename)
	if err != nil {
		return err
	}
	if expected != "" {
		hash := sha256.Sum256(bz)
		got := hex.EncodeToString(hash[:])
		if !strings.EqualFold(got, expected) {
			return fmt.Errorf("checksum mismatch for %s: got %s", filename, got)
		}
	}

	_, err = obj.ReadFrom(bytes.NewReader(bz))
	return err
}

func expectedChecksum(filename string) (string, error) {
	if expected := strings.TrimSpace(expectedChecksumFromEnv(filename)); expected != "" {
		return expected, nil
	}

	manifest, checksumSource, err := ResolveRuntimeArtifactManifest()
	if err != nil {
		return "", fmt.Errorf("load artifact manifest checksum for %s: %w", filename, err)
	}
	if checksumSource != ChecksumSourceManifest {
		return "", nil
	}

	for _, descriptor := range manifest.Artifacts {
		if descriptor.Filename == filename {
			expected := strings.TrimSpace(descriptor.SHA256)
			if expected == "" {
				return "", fmt.Errorf("artifact manifest is missing sha256 for %s", filename)
			}
			if err := validateExpectedSHA256(filename, expected); err != nil {
				return "", err
			}
			return expected, nil
		}
	}
	return "", fmt.Errorf("artifact manifest does not describe %s", filename)
}

func validateExpectedSHA256(filename string, expected string) error {
	decoded, err := hex.DecodeString(expected)
	if err != nil || len(decoded) != sha256.Size {
		return fmt.Errorf("artifact manifest sha256 for %s must be a 64-character hex string", filename)
	}
	return nil
}

func ValidateZKSetup() error {
	return loadZKSetup()
}

func ValidateZKArtifacts() error {
	_, err := loadArtifacts(readFile)
	return err
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

func GetDepositProvingKey() (groth16.ProvingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return depositProvingKey, nil
}

func GetDepositVerifyingKey() (groth16.VerifyingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return depositVerifyingKey, nil
}

func GetDepositR1CS() (constraint.ConstraintSystem, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return depositR1CS, nil
}

func GetSpendProvingKey() (groth16.ProvingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return spendProvingKey, nil
}

func GetSpendVerifyingKey() (groth16.VerifyingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return spendVerifyingKey, nil
}

func GetSpendR1CS() (constraint.ConstraintSystem, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return spendR1CS, nil
}

func GetJoinSplitProvingKey() (groth16.ProvingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return joinSplitProvingKey, nil
}

func GetJoinSplitVerifyingKey() (groth16.VerifyingKey, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return joinSplitVerifyingKey, nil
}

func GetJoinSplitR1CS() (constraint.ConstraintSystem, error) {
	if err := loadZKSetup(); err != nil {
		return nil, err
	}
	return joinSplitR1CS, nil
}
