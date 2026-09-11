package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
)

const (
	flagAuditProverURL     = "audit-prover-url"
	flagAuditExpiry        = "audit-expiry"
	flagAuditProverTimeout = "audit-prover-timeout"
)

// auditV2Runtime pins a local verifier identity against the small on-chain
// configuration query before requesting the active key at that exact height.
type auditV2Runtime struct {
	expiresAt      int64
	prover         privacyprovertransport.HTTPProverClient
	snapshotSource privacyprovertransport.AuditSnapshotSource
	registry       *privacyzk.ArtifactRegistry
	identity       *privacytypes.CircuitSetIdentity
}

func addAuditV2Flags(cmd *cobra.Command) {
	cmd.Flags().String(flagAuditProverURL, "", "operator-controlled audit-field proverd URL; omit to prove locally with the verified artifact bundle")
	cmd.Flags().Duration(flagAuditExpiry, 30*time.Minute, "audit-field transaction expiry")
	cmd.Flags().Duration(flagAuditProverTimeout, 30*time.Minute, "audit-field prover request timeout")
}

func resolveAuditV2Runtime(cmd *cobra.Command, clientCtx client.Context) (*auditV2Runtime, error) {
	if strings.TrimSpace(clientCtx.ChainID) == "" {
		return nil, fmt.Errorf("chain id is required for audit-field v2")
	}
	proverURL, err := cmd.Flags().GetString(flagAuditProverURL)
	if err != nil {
		return nil, err
	}
	// The batch command historically exposed --prover-url/--prover-timeout.
	// Keep those as an alias for the same v2 operator selection, but never
	// allow two different remote endpoints to be silently chosen.
	legacyURL, legacyTimeout, hasLegacyProver := "", time.Duration(0), false
	if cmd.Flags().Lookup(flagBatchProverURL) != nil {
		hasLegacyProver = true
		legacyURL, err = cmd.Flags().GetString(flagBatchProverURL)
		if err != nil {
			return nil, err
		}
		legacyTimeout, err = cmd.Flags().GetDuration(flagBatchProverTimeout)
		if err != nil {
			return nil, err
		}
	}
	proverURL = strings.TrimSpace(proverURL)
	legacyURL = strings.TrimSpace(legacyURL)
	if proverURL != "" && legacyURL != "" && proverURL != legacyURL {
		return nil, fmt.Errorf("--%s and --%s select different provers; specify only one", flagAuditProverURL, flagBatchProverURL)
	}
	if proverURL == "" {
		proverURL = legacyURL
	}
	timeout, err := cmd.Flags().GetDuration(flagAuditProverTimeout)
	if err != nil || timeout <= 0 {
		return nil, fmt.Errorf("--%s must be positive", flagAuditProverTimeout)
	}
	if hasLegacyProver && legacyURL != "" {
		if legacyTimeout <= 0 {
			return nil, fmt.Errorf("--%s must be positive", flagBatchProverTimeout)
		}
		timeout = legacyTimeout
	}
	expiry, err := cmd.Flags().GetDuration(flagAuditExpiry)
	if err != nil || expiry <= 0 {
		return nil, fmt.Errorf("--%s must be positive", flagAuditExpiry)
	}
	provider := privacyprovider.NewAuditV2QueryProvider(privacyv2.NewQueryClient(clientCtx))
	registry, identity, err := auditFieldRuntimeRegistry()
	if err != nil {
		return nil, err
	}
	manifestBytes, err := os.ReadFile(filepath.Join(registry.ArtifactDir(), privacyzk.ArtifactManifestFile))
	if err != nil {
		return nil, err
	}
	artifactHash := sha256.Sum256(manifestBytes)
	return &auditV2Runtime{
		expiresAt: time.Now().Add(expiry).Unix(),
		prover:    privacyprovertransport.HTTPProverClient{BaseURL: proverURL, Client: &http.Client{Timeout: timeout}, BearerToken: strings.TrimSpace(os.Getenv(privacyprovertransport.BearerTokenEnv))},
		snapshotSource: func(ctx context.Context) (privacyaudit.Snapshot, error) {
			return provider.ActiveAuditSnapshotFromConfiguration(ctx, clientCtx.ChainID, identity, artifactHash)
		},
		registry: registry,
		identity: identity,
	}, nil
}

func (r *auditV2Runtime) prove(cmd *cobra.Command, prepared *privacyaudit.Prepared, full witness.Witness) (sdk.Msg, error) {
	if r == nil {
		return nil, fmt.Errorf("audit-field runtime is required")
	}
	if strings.TrimSpace(r.prover.BaseURL) != "" {
		return privacyprovertransport.ProvePreparedAuditField(cmd.Context(), r.prover, prepared, full, r.snapshotSource, r.registry, r.identity)
	}
	defer prepared.Clear()
	defer privacyprovertransport.ClearAuditWitness(full)
	snapshot, err := r.snapshotSource(cmd.Context())
	if err != nil {
		return nil, err
	}
	if !prepared.ValidFor(snapshot, time.Now()) {
		return nil, fmt.Errorf("prepared audit transaction is stale before local proving")
	}
	ids := privacyzk.AuditFieldCircuitIDs()
	if prepared.Kind() < 1 || int(prepared.Kind()) > len(ids) {
		return nil, fmt.Errorf("unsupported audit circuit kind")
	}
	if err := r.registry.CheckReadiness(privacyzk.ArtifactRoleProver, []privacyzk.CircuitID{ids[int(prepared.Kind())-1]}, r.identity); err != nil {
		return nil, err
	}
	r1cs, err := r.registry.R1CS(ids[int(prepared.Kind())-1])
	if err != nil {
		return nil, err
	}
	pk, err := r.registry.ProvingKey(ids[int(prepared.Kind())-1])
	if err != nil {
		return nil, err
	}
	proof, err := groth16.Prove(r1cs, pk, full)
	if err != nil {
		return nil, err
	}
	var encoded bytes.Buffer
	if _, err := proof.WriteTo(&encoded); err != nil {
		return nil, err
	}
	snapshot, err = r.snapshotSource(cmd.Context())
	if err != nil {
		return nil, err
	}
	if !prepared.ValidFor(snapshot, time.Now()) {
		return nil, fmt.Errorf("prepared audit transaction is stale after local proving")
	}
	if err := prepared.VerifyProofForArtifact(r.registry, r.identity, snapshot.ArtifactHash, encoded.Bytes()); err != nil {
		return nil, err
	}
	return prepared.BuildMessage(encoded.Bytes())
}

func auditFieldRuntimeRegistry() (*privacyzk.ArtifactRegistry, *privacytypes.CircuitSetIdentity, error) {
	dir := strings.TrimSpace(os.Getenv(privacyzk.ZKArtifactDirEnv))
	if dir == "" {
		return nil, nil, fmt.Errorf("%s is required for the audit-field V2 runtime", privacyzk.ZKArtifactDirEnv)
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{CircuitSetID: privacyzk.AuditFieldCircuitSetID, ArtifactDir: dir, RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment})
	if err != nil {
		return nil, nil, err
	}
	identity, err := registry.LocalCircuitSetIdentity()
	if err != nil {
		return nil, nil, err
	}
	return registry, identity, registry.CheckReadiness(privacyzk.ArtifactRoleValidator, privacyzk.AuditFieldCircuitIDs(), identity)
}

func buildAuditV2DepositOutput(spend *crypto_tedwards.PointAffine, seed []byte, coin sdk.Coin) (*privacyv2.OutputEffect, privacytypes.SecretNoteV1, []auditfield.Field32, error) {
	if spend == nil {
		return nil, privacytypes.SecretNoteV1{}, nil, fmt.Errorf("spend key is required")
	}
	if err := privacytypes.ValidateShieldedAmount("deposit amount", coin.Amount.BigInt()); err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	_, view, _, err := deriveViewKeys(seed)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	spendX, spendY, err := privacycrypto.PublicPointFieldValues(*spend)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	viewX, viewY, err := privacycrypto.PublicPointFieldValues(*view)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	assetRaw, err := canonicalFieldBytesFromBigInt(privacytypes.ComputeAssetIDV1(coin.Denom))
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	asset, err := privacycrypto.ParseFieldValueBE32(assetRaw)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	note, err := privacytypes.NewRandomSecretNoteV1(rand.Reader, spendX, spendY, viewX, viewY, coin.Amount.Uint64(), asset, "")
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	commitment, err := note.CommitmentV1()
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	plain, err := privacytypes.MarshalSecretNotePlaintextV1(note)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	defer clear(plain)
	raw, err := privacycrypto.Encrypt(plain, seed)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	cipher, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeDepositNoteV1, raw)
	if err != nil {
		return nil, privacytypes.SecretNoteV1{}, nil, err
	}
	toField := func(value privacycrypto.FieldValue) (auditfield.Field32, error) {
		encoded := value.Bytes()
		return auditfield.ParseField32(encoded[:])
	}
	fields := make([]auditfield.Field32, 6)
	for i, value := range []privacycrypto.FieldValue{asset, spendX, spendY, viewX, viewY} {
		field, err := toField(value)
		if err != nil {
			return nil, privacytypes.SecretNoteV1{}, nil, err
		}
		if i == 0 {
			fields[0] = field
		} else {
			fields[i+1] = field
		}
	}
	fields[1] = auditfield.Field32FromUint64(coin.Amount.Uint64())
	commitmentRaw := commitment.Bytes()
	return &privacyv2.OutputEffect{Commitment: commitmentRaw[:], Ciphertext: cipher}, *note, fields, nil
}
