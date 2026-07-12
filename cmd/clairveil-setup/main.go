package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type artifactDefinition struct {
	filename    string
	checksumEnv string
	write       func(outDir string) error
}

const (
	setupCircuitAll                   = "all"
	setupCircuitDeposit               = "deposit"
	setupCircuitSpend                 = "spend"
	setupCircuitJoinSplit             = "joinsplit"
	setupCircuitBatchJoinSplit16x32V1 = "batch-joinsplit-16x32-v1"
)

func main() {
	outDirFlag := flag.String("out", "artifacts/privacy", "output directory for generated zk artifacts")
	overwriteFlag := flag.Bool("overwrite", false, "overwrite existing artifacts in the output directory")
	circuitFlag := flag.String("circuit", setupCircuitAll, "artifact circuit to generate: all, deposit, spend, joinsplit, or batch-joinsplit-16x32-v1")
	flag.Parse()
	selectedCircuit, err := parseSetupCircuit(*circuitFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid circuit selection: %v\n", err)
		os.Exit(1)
	}

	outDir, err := filepath.Abs(*outDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve output directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(1)
	}
	if selectedCircuit != setupCircuitAll {
		if !*overwriteFlag {
			fmt.Fprintln(os.Stderr, "selective artifact rotation requires -overwrite")
			os.Exit(1)
		}
		if err := validateExistingArtifactSet(outDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to validate existing artifact set before selective rotation: %v\n", err)
			os.Exit(1)
		}
	}

	definitions, err := buildArtifactDefinitions(selectedCircuit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to prepare circuits: %v\n", err)
		os.Exit(1)
	}

	for _, definition := range definitions {
		path := filepath.Join(outDir, definition.filename)
		if !*overwriteFlag {
			if _, err := os.Stat(path); err == nil {
				fmt.Fprintf(os.Stderr, "artifact already exists: %s (use -overwrite to replace)\n", path)
				os.Exit(1)
			}
		}

		if err := definition.write(outDir); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write %s: %v\n", definition.filename, err)
			os.Exit(1)
		}
	}
	checksums, err := checksumArtifactSet(outDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to checksum generated artifact set: %v\n", err)
		os.Exit(1)
	}

	if err := writeEnvManifest(filepath.Join(outDir, "privacy_zk_checksums.env"), outDir, checksums); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write env manifest: %v\n", err)
		os.Exit(1)
	}

	if err := writeLegacyChecksumsJSON(filepath.Join(outDir, zk.LegacyChecksumsJSONFile), outDir, checksums); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write legacy json manifest: %v\n", err)
		os.Exit(1)
	}

	if err := writeJSONManifest(filepath.Join(outDir, zk.ArtifactManifestFile), outDir, checksums); err != nil {
		fmt.Fprintf(os.Stderr, "failed to write json manifest: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("privacy zk artifacts generated successfully")
	fmt.Printf("artifact_dir=%s\n", outDir)
	for _, key := range checksumEnvironmentOrder() {
		fmt.Printf("%s=%s\n", key, checksums[key])
	}
}

func buildArtifactDefinitions(selectedCircuit string) ([]artifactDefinition, error) {
	type circuitDefinition struct {
		id                        string
		circuit                   frontend.Circuit
		r1csFile, r1csChecksumEnv string
		pkFile, pkChecksumEnv     string
		vkFile, vkChecksumEnv     string
	}
	circuits := []circuitDefinition{
		{setupCircuitDeposit, &circuit.DepositCircuit{}, zk.DepositR1CSFile, zk.DepositR1CSSHA256Env, zk.DepositPKFile, zk.DepositPKSHA256Env, zk.DepositVKFile, zk.DepositVKSHA256Env},
		{setupCircuitSpend, &circuit.SpendCircuit{}, zk.SpendR1CSFile, zk.SpendR1CSSHA256Env, zk.SpendPKFile, zk.SpendPKSHA256Env, zk.SpendVKFile, zk.SpendVKSHA256Env},
		{setupCircuitJoinSplit, &circuit.JoinSplitCircuit{}, zk.JoinSplitR1CSFile, zk.JoinSplitR1CSSHA256Env, zk.JoinSplitPKFile, zk.JoinSplitPKSHA256Env, zk.JoinSplitVKFile, zk.JoinSplitVKSHA256Env},
		{setupCircuitBatchJoinSplit16x32V1, &circuit.BatchJoinSplit16x32{}, zk.BatchJoinSplit16x32R1CSFile, zk.BatchJoinSplit16x32R1CSSHA256Env, zk.BatchJoinSplit16x32PKFile, zk.BatchJoinSplit16x32PKSHA256Env, zk.BatchJoinSplit16x32VKFile, zk.BatchJoinSplit16x32VKSHA256Env},
	}
	definitions := make([]artifactDefinition, 0, len(circuits)*3)
	for _, definition := range circuits {
		if selectedCircuit != setupCircuitAll && selectedCircuit != definition.id {
			continue
		}
		compiled, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, definition.circuit)
		if err != nil {
			return nil, err
		}
		provingKey, verifyingKey, err := groth16.Setup(compiled)
		if err != nil {
			return nil, err
		}
		compiledCopy := compiled
		provingKeyCopy := provingKey
		verifyingKeyCopy := verifyingKey
		definitionCopy := definition
		definitions = append(definitions,
			artifactDefinition{filename: definitionCopy.r1csFile, checksumEnv: definitionCopy.r1csChecksumEnv, write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, definitionCopy.r1csFile), compiledCopy)
			}},
			artifactDefinition{filename: definitionCopy.pkFile, checksumEnv: definitionCopy.pkChecksumEnv, write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, definitionCopy.pkFile), provingKeyCopy)
			}},
			artifactDefinition{filename: definitionCopy.vkFile, checksumEnv: definitionCopy.vkChecksumEnv, write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, definitionCopy.vkFile), verifyingKeyCopy)
			}},
		)
	}
	return definitions, nil
}

func parseSetupCircuit(raw string) (string, error) {
	selected := strings.ToLower(strings.TrimSpace(raw))
	switch selected {
	case setupCircuitAll,
		setupCircuitDeposit,
		setupCircuitSpend,
		setupCircuitJoinSplit,
		setupCircuitBatchJoinSplit16x32V1:
		return selected, nil
	default:
		return "", fmt.Errorf("unsupported circuit %q", raw)
	}
}

func validateExistingArtifactSet(outDir string) error {
	manifest, err := zk.LoadArtifactManifest(filepath.Join(outDir, zk.ArtifactManifestFile))
	if err != nil {
		return err
	}
	for _, descriptor := range manifest.Artifacts {
		actual, err := checksumFile(filepath.Join(outDir, descriptor.Filename))
		if err != nil {
			return fmt.Errorf("checksum %s: %w", descriptor.Filename, err)
		}
		if actual != descriptor.SHA256 {
			return fmt.Errorf("artifact %s does not match the existing manifest checksum", descriptor.Filename)
		}
	}
	return nil
}

func checksumArtifactSet(outDir string) (map[string]string, error) {
	descriptors := zk.DefaultArtifactDescriptors()
	checksums := make(map[string]string, len(descriptors))
	for _, descriptor := range descriptors {
		checksum, err := checksumFile(filepath.Join(outDir, descriptor.Filename))
		if err != nil {
			return nil, fmt.Errorf("checksum %s: %w", descriptor.Filename, err)
		}
		checksums[descriptor.ChecksumEnv] = checksum
	}
	return checksums, nil
}

func writeArtifact(path string, artifact interface {
	WriteTo(w io.Writer) (int64, error)
}) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = artifact.WriteTo(file)
	return err
}

func checksumFile(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func writeEnvManifest(path, outDir string, checksums map[string]string) error {
	content := fmt.Sprintf("%s=%s\n", zk.ZKArtifactDirEnv, outDir)
	for _, checksumEnv := range checksumEnvironmentOrder() {
		content += fmt.Sprintf("%s=%s\n", checksumEnv, checksums[checksumEnv])
	}

	return os.WriteFile(path, []byte(content), 0o644)
}

func checksumEnvironmentOrder() []string {
	return []string{
		zk.DepositR1CSSHA256Env,
		zk.DepositPKSHA256Env,
		zk.DepositVKSHA256Env,
		zk.SpendR1CSSHA256Env,
		zk.SpendPKSHA256Env,
		zk.SpendVKSHA256Env,
		zk.JoinSplitR1CSSHA256Env,
		zk.JoinSplitPKSHA256Env,
		zk.JoinSplitVKSHA256Env,
		zk.BatchJoinSplit16x32R1CSSHA256Env,
		zk.BatchJoinSplit16x32PKSHA256Env,
		zk.BatchJoinSplit16x32VKSHA256Env,
	}
}

func writeLegacyChecksumsJSON(path, outDir string, checksums map[string]string) error {
	manifest := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"curve":        zk.CircuitCurve,
		"artifact_dir": outDir,
		"checksums":    checksums,
	}

	bz, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	bz = append(bz, '\n')

	return os.WriteFile(path, bz, 0o644)
}

func writeJSONManifest(path, outDir string, checksums map[string]string) error {
	manifest := zk.ManifestFromChecksums(
		outDir,
		time.Now().UTC().Format(time.RFC3339),
		checksums,
	)

	bz, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	bz = append(bz, '\n')

	return os.WriteFile(path, bz, 0o644)
}
