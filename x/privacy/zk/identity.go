package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func ValidateLocalVerifierIdentity(expected *privacytypes.CircuitSetIdentity) error {
	if err := privacytypes.ValidateCircuitSetIdentity(expected); err != nil {
		return fmt.Errorf("invalid consensus circuit identity: %w", err)
	}
	local, err := LoadLocalCircuitSetIdentity()
	if err != nil {
		return fmt.Errorf("load local circuit identity: %w", err)
	}
	if !reflect.DeepEqual(expected, local) {
		return fmt.Errorf("local circuit identity does not match consensus circuit identity")
	}

	for _, circuit := range expected.Circuits {
		filename := verifyingKeyFilename(circuit.CircuitId)
		bz, err := os.ReadFile(artifactPath(filename))
		if err != nil {
			return fmt.Errorf("read local verifying key %s: %w", filename, err)
		}
		sum := sha256.Sum256(bz)
		actual := hex.EncodeToString(sum[:])
		if actual != circuit.VerifyingKeySha256 {
			return fmt.Errorf("verifying key checksum mismatch for %s: expected %s, got %s", circuit.CircuitId, circuit.VerifyingKeySha256, actual)
		}
		if configured := strings.TrimSpace(expectedChecksumFromEnv(filename)); configured != "" {
			if err := validateExpectedSHA256(filename, configured); err != nil {
				return err
			}
			if strings.ToLower(configured) != circuit.VerifyingKeySha256 {
				return fmt.Errorf("environment checksum for %s cannot override consensus checksum", filename)
			}
		}

		vk := groth16.NewVerifyingKey(ecc.BN254)
		read, err := vk.ReadFrom(bytes.NewReader(bz))
		if err != nil {
			return fmt.Errorf("decode verifying key %s: %w", filename, err)
		}
		if read != int64(len(bz)) {
			return fmt.Errorf("verifying key %s has trailing bytes", filename)
		}
		var canonical bytes.Buffer
		if _, err := vk.WriteTo(&canonical); err != nil {
			return fmt.Errorf("re-encode verifying key %s: %w", filename, err)
		}
		if !bytes.Equal(canonical.Bytes(), bz) {
			return fmt.Errorf("verifying key %s is not canonically encoded", filename)
		}
	}
	return nil
}

func ValidateLocalVerifierArtifacts() error {
	identity, err := LoadLocalCircuitSetIdentity()
	if err != nil {
		return err
	}
	return ValidateLocalVerifierIdentity(identity)
}
