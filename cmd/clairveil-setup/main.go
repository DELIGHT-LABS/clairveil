package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
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

	var checksums map[string]string
	if selectedCircuit == setupCircuitAll {
		checksums, err = writeArtifactSet(outDir, outDir, definitions, *overwriteFlag, defaultArtifactSetOps())
	} else {
		checksums, err = rotateSelectiveArtifactSet(outDir, definitions, defaultArtifactSetOps())
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to generate artifact set: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("privacy zk artifacts generated successfully")
	fmt.Printf("artifact_dir=%s\n", outDir)
	for _, key := range checksumEnvironmentOrder() {
		fmt.Printf("%s=%s\n", key, checksums[key])
	}
}

type artifactSetOps struct {
	copyDirectory            func(string, string) error
	writeEnvManifest         func(string, string, map[string]string) error
	writeLegacyChecksumsJSON func(string, string, map[string]string) error
	writeJSONManifest        func(string, string, map[string]string) error
	rename                   func(string, string) error
	removeAll                func(string) error
}

func defaultArtifactSetOps() artifactSetOps {
	return artifactSetOps{
		copyDirectory:            copyDirectoryContents,
		writeEnvManifest:         writeEnvManifest,
		writeLegacyChecksumsJSON: writeLegacyChecksumsJSON,
		writeJSONManifest:        writeJSONManifest,
		rename:                   os.Rename,
		removeAll:                os.RemoveAll,
	}
}

func writeArtifactSet(
	targetDir string,
	manifestArtifactDir string,
	definitions []artifactDefinition,
	overwrite bool,
	ops artifactSetOps,
) (map[string]string, error) {
	for _, definition := range definitions {
		path := filepath.Join(targetDir, definition.filename)
		if !overwrite {
			if _, err := os.Stat(path); err == nil {
				return nil, fmt.Errorf("artifact already exists: %s (use -overwrite to replace)", path)
			}
		}
		if err := definition.write(targetDir); err != nil {
			return nil, fmt.Errorf("write %s: %w", definition.filename, err)
		}
	}
	checksums, err := checksumArtifactSet(targetDir)
	if err != nil {
		return nil, fmt.Errorf("checksum generated artifact set: %w", err)
	}
	if err := writeArtifactManifests(targetDir, manifestArtifactDir, checksums, ops); err != nil {
		return nil, err
	}
	return checksums, nil
}

func writeArtifactManifests(targetDir, manifestArtifactDir string, checksums map[string]string, ops artifactSetOps) error {
	if err := ops.writeEnvManifest(filepath.Join(targetDir, "privacy_zk_checksums.env"), manifestArtifactDir, checksums); err != nil {
		return fmt.Errorf("write env manifest: %w", err)
	}
	if err := ops.writeLegacyChecksumsJSON(filepath.Join(targetDir, zk.LegacyChecksumsJSONFile), manifestArtifactDir, checksums); err != nil {
		return fmt.Errorf("write legacy json manifest: %w", err)
	}
	if err := ops.writeJSONManifest(filepath.Join(targetDir, zk.ArtifactManifestFile), manifestArtifactDir, checksums); err != nil {
		return fmt.Errorf("write json manifest: %w", err)
	}
	return nil
}

func rotateSelectiveArtifactSet(outDir string, definitions []artifactDefinition, ops artifactSetOps) (checksums map[string]string, returnErr error) {
	parentDir := filepath.Dir(outDir)
	baseName := filepath.Base(outDir)
	stagingDir, err := os.MkdirTemp(parentDir, "."+baseName+".staging-*")
	if err != nil {
		return nil, fmt.Errorf("create staging directory: %w", err)
	}
	cleanupStaging := true
	defer func() {
		if cleanupStaging {
			if err := ops.removeAll(stagingDir); err != nil {
				cleanupErr := fmt.Errorf("remove staging artifact set %s: %w", stagingDir, err)
				if returnErr == nil {
					returnErr = cleanupErr
				} else {
					returnErr = fmt.Errorf("%w; additionally failed to %v", returnErr, cleanupErr)
				}
			}
		}
	}()
	if err := ops.copyDirectory(outDir, stagingDir); err != nil {
		return nil, fmt.Errorf("stage existing artifact set: %w", err)
	}
	checksums, err = writeArtifactSet(stagingDir, outDir, definitions, true, ops)
	if err != nil {
		return nil, fmt.Errorf("stage selective artifact rotation: %w", err)
	}
	if err := validateExistingArtifactSet(stagingDir); err != nil {
		return nil, fmt.Errorf("validate staged artifact set: %w", err)
	}

	backupDir, err := os.MkdirTemp(parentDir, "."+baseName+".backup-*")
	if err != nil {
		return nil, fmt.Errorf("reserve backup path: %w", err)
	}
	if err := os.Remove(backupDir); err != nil {
		return nil, fmt.Errorf("prepare backup path: %w", err)
	}
	if err := ops.rename(outDir, backupDir); err != nil {
		return nil, fmt.Errorf("move current artifact set to backup: %w", err)
	}
	if err := ops.rename(stagingDir, outDir); err != nil {
		rollbackErr := ops.rename(backupDir, outDir)
		if rollbackErr != nil {
			cleanupStaging = false
			return nil, fmt.Errorf(
				"install staged artifact set %s at %s: %v; rollback backup artifact set %s to %s failed: %v; recovery paths: staging=%s backup=%s",
				stagingDir,
				outDir,
				err,
				backupDir,
				outDir,
				rollbackErr,
				stagingDir,
				backupDir,
			)
		}
		return nil, fmt.Errorf("install staged artifact set: %w", err)
	}
	cleanupStaging = false
	if err := ops.removeAll(backupDir); err != nil {
		return checksums, fmt.Errorf(
			"installed staged artifact set but failed to remove backup artifact set %s; remove it manually: %w",
			backupDir,
			err,
		)
	}
	return checksums, nil
}

func copyDirectoryContents(sourceDir, destinationDir string) error {
	sourceInfo, err := os.Stat(sourceDir)
	if err != nil {
		return err
	}
	if err := os.Chmod(destinationDir, sourceInfo.Mode().Perm()); err != nil {
		return err
	}
	return filepath.WalkDir(sourceDir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if relative == "." {
			return nil
		}
		target := filepath.Join(destinationDir, relative)
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return os.Mkdir(target, info.Mode().Perm())
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("artifact directory contains unsupported entry %s", path)
		}
		return copyRegularFile(path, target, info.Mode().Perm())
	})
}

func copyRegularFile(sourcePath, destinationPath string, mode fs.FileMode) (returnErr error) {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := destination.Close(); returnErr == nil && closeErr != nil {
			returnErr = closeErr
		}
	}()
	if _, err := io.Copy(destination, source); err != nil {
		return err
	}
	return destination.Sync()
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
}) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(0o600); err != nil {
		return err
	}
	if _, err := artifact.WriteTo(temporary); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
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

	return writeFileAtomic(path, []byte(content), 0o644)
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

	return writeFileAtomic(path, bz, 0o644)
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

	return writeFileAtomic(path, bz, 0o644)
}

func writeFileAtomic(path string, content []byte, mode fs.FileMode) (returnErr error) {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}
