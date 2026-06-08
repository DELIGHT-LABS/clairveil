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

func main() {
	outDirFlag := flag.String("out", "artifacts/privacy", "output directory for generated zk artifacts")
	overwriteFlag := flag.Bool("overwrite", false, "overwrite existing artifacts in the output directory")
	flag.Parse()

	outDir, err := filepath.Abs(*outDirFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to resolve output directory: %v\n", err)
		os.Exit(1)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "failed to create output directory: %v\n", err)
		os.Exit(1)
	}

	definitions, err := buildArtifactDefinitions()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to prepare circuits: %v\n", err)
		os.Exit(1)
	}

	checksums := make(map[string]string, len(definitions))
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

		checksum, err := checksumFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to checksum %s: %v\n", definition.filename, err)
			os.Exit(1)
		}

		checksums[definition.checksumEnv] = checksum
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
	for _, key := range []string{
		zk.DepositR1CSSHA256Env,
		zk.DepositPKSHA256Env,
		zk.DepositVKSHA256Env,
		zk.SpendR1CSSHA256Env,
		zk.SpendPKSHA256Env,
		zk.SpendVKSHA256Env,
		zk.JoinSplitR1CSSHA256Env,
		zk.JoinSplitPKSHA256Env,
		zk.JoinSplitVKSHA256Env,
	} {
		fmt.Printf("%s=%s\n", key, checksums[key])
	}
}

func buildArtifactDefinitions() ([]artifactDefinition, error) {
	depositCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.DepositCircuit{})
	if err != nil {
		return nil, err
	}
	depositPK, depositVK, err := groth16.Setup(depositCS)
	if err != nil {
		return nil, err
	}

	spendCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
	if err != nil {
		return nil, err
	}
	spendPK, spendVK, err := groth16.Setup(spendCS)
	if err != nil {
		return nil, err
	}

	joinSplitCS, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
	if err != nil {
		return nil, err
	}
	joinSplitPK, joinSplitVK, err := groth16.Setup(joinSplitCS)
	if err != nil {
		return nil, err
	}

	return []artifactDefinition{
		{
			filename:    zk.DepositR1CSFile,
			checksumEnv: zk.DepositR1CSSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.DepositR1CSFile), depositCS)
			},
		},
		{
			filename:    zk.DepositPKFile,
			checksumEnv: zk.DepositPKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.DepositPKFile), depositPK)
			},
		},
		{
			filename:    zk.DepositVKFile,
			checksumEnv: zk.DepositVKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.DepositVKFile), depositVK)
			},
		},
		{
			filename:    zk.SpendR1CSFile,
			checksumEnv: zk.SpendR1CSSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.SpendR1CSFile), spendCS)
			},
		},
		{
			filename:    zk.SpendPKFile,
			checksumEnv: zk.SpendPKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.SpendPKFile), spendPK)
			},
		},
		{
			filename:    zk.SpendVKFile,
			checksumEnv: zk.SpendVKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.SpendVKFile), spendVK)
			},
		},
		{
			filename:    zk.JoinSplitR1CSFile,
			checksumEnv: zk.JoinSplitR1CSSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.JoinSplitR1CSFile), joinSplitCS)
			},
		},
		{
			filename:    zk.JoinSplitPKFile,
			checksumEnv: zk.JoinSplitPKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.JoinSplitPKFile), joinSplitPK)
			},
		},
		{
			filename:    zk.JoinSplitVKFile,
			checksumEnv: zk.JoinSplitVKSHA256Env,
			write: func(outDir string) error {
				return writeArtifact(filepath.Join(outDir, zk.JoinSplitVKFile), joinSplitVK)
			},
		},
	}, nil
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
	bz, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	sum := sha256.Sum256(bz)
	return hex.EncodeToString(sum[:]), nil
}

func writeEnvManifest(path, outDir string, checksums map[string]string) error {
	content := fmt.Sprintf(
		"CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n%s=%s\n",
		outDir,
		zk.DepositR1CSSHA256Env, checksums[zk.DepositR1CSSHA256Env],
		zk.DepositPKSHA256Env, checksums[zk.DepositPKSHA256Env],
		zk.DepositVKSHA256Env, checksums[zk.DepositVKSHA256Env],
		zk.SpendR1CSSHA256Env, checksums[zk.SpendR1CSSHA256Env],
		zk.SpendPKSHA256Env, checksums[zk.SpendPKSHA256Env],
		zk.SpendVKSHA256Env, checksums[zk.SpendVKSHA256Env],
		zk.JoinSplitR1CSSHA256Env, checksums[zk.JoinSplitR1CSSHA256Env],
		zk.JoinSplitPKSHA256Env, checksums[zk.JoinSplitPKSHA256Env],
		zk.JoinSplitVKSHA256Env, checksums[zk.JoinSplitVKSHA256Env],
	)

	return os.WriteFile(path, []byte(content), 0o644)
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
