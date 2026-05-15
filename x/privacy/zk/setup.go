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
	spendProvingKey   groth16.ProvingKey
	spendVerifyingKey groth16.VerifyingKey
	spendR1CS         constraint.ConstraintSystem

	joinSplitProvingKey   groth16.ProvingKey
	joinSplitVerifyingKey groth16.VerifyingKey
	joinSplitR1CS         constraint.ConstraintSystem
}

var (
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
	SpendR1CSFile = "privacy_spend_r1cs.bin"
	SpendPKFile   = "privacy_spend_pk.bin"
	SpendVKFile   = "privacy_spend_vk.bin"

	JoinSplitR1CSFile = "privacy_joinsplit_r1cs.bin"
	JoinSplitPKFile   = "privacy_joinsplit_pk.bin"
	JoinSplitVKFile   = "privacy_joinsplit_vk.bin"

	ZKArtifactDirEnv = "CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR"

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

	if expected := expectedChecksumFromEnv(filename); expected != "" {
		hash := sha256.Sum256(bz)
		got := hex.EncodeToString(hash[:])
		if !strings.EqualFold(got, expected) {
			return fmt.Errorf("checksum mismatch for %s: got %s", filename, got)
		}
	}

	_, err = obj.ReadFrom(bytes.NewReader(bz))
	return err
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
