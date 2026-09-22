package cli

import (
	"bytes"
	"context"
	"fmt"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"io"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/cosmos/gogoproto/proto"
)

const (
	flagBatchPayment       = "payment"
	flagBatchInputIndex    = "input-index"
	flagBatchOutputMode    = "output-mode"
	flagBatchPreparedOut   = "prepared-out"
	flagBatchProofOut      = "proof-out"
	flagBatchProverURL     = "prover-url"
	flagBatchProverTimeout = "prover-timeout"

	defaultBatchPreparedPath = "batch-transfer-prepared.json"
	defaultBatchProofPath    = "batch-transfer-proof.json"
	defaultBatchProverWait   = 30 * time.Minute
)

type batchTransferCommandOutput struct {
	PayloadHash string `json:"payload_hash"`
	TxHash      string `json:"txhash,omitempty"`
	Height      int64  `json:"height,omitempty"`
	Code        uint32 `json:"code,omitempty"`
	InputCount  int    `json:"input_count"`
	OutputCount int    `json:"output_count"`
	Prepared    string `json:"prepared_file,omitempty"`
	Proof       string `json:"proof_file,omitempty"`
	Prover      string `json:"prover,omitempty"`
}

// CmdTransferBatch16x32 is the one-proof 1..16 input / 1..32 output flow. It
// intentionally has a different name from transfer-batch, which remains a
// multi-MsgTransfer Cosmos transaction envelope.
func CmdTransferBatch16x32() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transfer-batch-16x32",
		Short: "Plan, prepare, prove, build, and broadcast one MsgBatchTransfer proof",
		Long: strings.TrimSpace(`
Run the complete one-proof BatchJoinSplit16x32 flow.

Repeat --payment once per output payment. Each value is:

  shielded_address,coin[,privacy-policy,disclosure-mode,disclosure-pubkey-hex]

The optional disclosure fields are independent for every output. The default
is all-private,none. A non-private policy must explicitly select public or
recipient-encrypted; recipient-encrypted also requires its disclosure key.

This command writes the prepared payload before proving and the proof before
broadcasting, so either stage can be resumed with the companion commands. It
uses the audit-field v2 prover selected by --audit-prover-url or --prover-url,
or the verified local audit artifact bundle when neither flag is set.

This is not the existing transfer-batch command, which broadcasts several
independent MsgTransfer messages in one Cosmos transaction envelope.
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			payload, preparedPath, err := prepareBatchTransferFromFlags(cmd, clientCtx)
			if err != nil {
				return err
			}
			proof, proofPath, prover, err := proveBatchTransferFromFile(cmd, clientCtx, preparedPath)
			if err != nil {
				return err
			}
			return broadcastBatchTransferArtifacts(cmd, clientCtx, payload, proof, batchTransferCommandOutput{
				PayloadHash: payload.PayloadHash,
				InputCount:  len(payload.Inputs),
				OutputCount: len(payload.Outputs),
				Prepared:    preparedPath,
				Proof:       proofPath,
				Prover:      prover,
			})
		},
	}
	addBatchTransferPrepareFlags(cmd)
	addBatchTransferProveFlags(cmd)
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdPrepareBatchTransfer() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prepare-batch-transfer",
		Short: "Plan and prepare a versioned one-proof batch transfer payload",
		Long: strings.TrimSpace(`
Plan 1..16 wallet inputs and 1..32 payment/change/padding outputs, query every
Merkle path, create independent output randomness and disclosure blindings,
and write a structured private prepared payload with mode 0600. The final v2
owner signature is created while proving, after the audit envelope is fixed.

Repeat --payment using:
  shielded_address,coin[,privacy-policy,disclosure-mode,disclosure-pubkey-hex]
		`),
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			payload, path, err := prepareBatchTransferFromFlags(cmd, clientCtx)
			if err != nil {
				return err
			}
			output := batchTransferCommandOutput{PayloadHash: payload.PayloadHash, InputCount: len(payload.Inputs), OutputCount: len(payload.Outputs), Prepared: path}
			if privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, output)
			}
			privacyCommandOutputPrintf(cmd, "prepared one-proof batch transfer %s (%d inputs, %d outputs)\n", payload.PayloadHash, len(payload.Inputs), len(payload.Outputs))
			privacyCommandOutputPrintf(cmd, "prepared payload: %s\n", path)
			return nil
		},
	}
	addBatchTransferPrepareFlags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdProveBatchTransfer() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "prove-batch-transfer [prepared_payload_file]",
		Short: "Prove a prepared one-proof batch transfer locally or with one remote prover",
		Long: strings.TrimSpace(`
Validate the prepared v2 payload version, expiry, and payload hash before
proving. Either --audit-prover-url or the retained --prover-url selects the
operator audit-field prover; conflicting values are rejected. Omitting both
uses the verified local audit artifact bundle.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			proof, path, prover, err := proveBatchTransferFromFile(cmd, clientCtx, args[0])
			if err != nil {
				return err
			}
			payload, err := privacybatchtransfer.ReadPreparedAuditV2BatchTransferPayload(args[0])
			if err != nil {
				return err
			}
			output := batchTransferCommandOutput{PayloadHash: proof.RequestPayloadHash, InputCount: len(payload.Inputs), OutputCount: len(payload.Outputs), Prepared: args[0], Proof: path, Prover: prover}
			if privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, output)
			}
			privacyCommandOutputPrintf(cmd, "proved one-proof batch transfer %s with %s prover\n", proof.RequestPayloadHash, prover)
			privacyCommandOutputPrintf(cmd, "proof: %s\n", path)
			return nil
		},
	}
	addBatchTransferProveFlags(cmd)
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func CmdBroadcastBatchTransfer() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "broadcast-batch-transfer [prepared_payload_file] [proof_file]",
		Short: "Validate, build, and broadcast one MsgBatchTransfer",
		Long: strings.TrimSpace(`
Strictly decode the prepared payload and proof, revalidate their versions,
expiry, signature, and payload-hash binding, build exactly one
MsgBatchTransfer, then use the normal Cosmos transaction flags to generate,
simulate, or broadcast it.
		`),
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}
			payload, err := privacybatchtransfer.ReadPreparedAuditV2BatchTransferPayload(args[0])
			if err != nil {
				return err
			}
			proof, err := privacybatchtransfer.ReadPreparedAuditV2BatchTransferProof(args[1])
			if err != nil {
				return err
			}
			return broadcastBatchTransferArtifacts(cmd, clientCtx, payload, proof, batchTransferCommandOutput{
				PayloadHash: payload.PayloadHash,
				InputCount:  len(payload.Inputs),
				OutputCount: len(payload.Outputs),
				Prepared:    args[0],
				Proof:       args[1],
			})
		},
	}
	addAuditV2Flags(cmd)
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}

func addBatchTransferPrepareFlags(cmd *cobra.Command) {
	cmd.Flags().StringArray(flagBatchPayment, nil, "Payment: shielded_address,coin[,privacy-policy,disclosure-mode,disclosure-pubkey-hex] (repeat 1..32 times)")
	cmd.Flags().IntSlice(flagBatchInputIndex, nil, "One-based list-notes input indexes; omitted selects matching spendable notes automatically (maximum 16)")
	cmd.Flags().String(flagBatchOutputMode, string(privacybatchtransfer.OutputModeCompact), "Output layout: compact or exact32 (exact32 adds explicit zero padding)")
	cmd.Flags().String(flagBatchPreparedOut, defaultBatchPreparedPath, "Private prepared payload output file (written mode 0600)")
	cmd.Flags().Int64(flagTransferExpiresIn, int64(defaultPreparedWithdrawExpiry/time.Second), "owner intent validity window in seconds")
	cmd.Flags().Bool(flagTransferNoSelfView, false, "Disable sender self-view disclosure for every output")
	cmd.Flags().Bool(flagRescanWallet, false, "reset the local privacy wallet cache and rescan from genesis before input selection")
}

func addBatchTransferProveFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagBatchProofOut, defaultBatchProofPath, "Private proof output file (written mode 0600)")
	cmd.Flags().String(flagBatchProverURL, "", "Exclusive remote prover base URL; omitted uses only the local prover; bearer auth reads "+privacyprovertransport.BearerTokenEnv)
	cmd.Flags().Duration(flagBatchProverTimeout, defaultBatchProverWait, "Remote prover request timeout")
}

func prepareBatchTransferFromFlags(cmd *cobra.Command, clientCtx client.Context) (*privacybatchtransfer.PreparedAuditV2BatchTransferPayload, string, error) {
	rawPayments, err := cmd.Flags().GetStringArray(flagBatchPayment)
	if err != nil {
		return nil, "", err
	}
	payments, denom, paymentTotal, err := parseBatchTransferPayments(rawPayments)
	if err != nil {
		return nil, "", err
	}
	identity, err := resolveTransferExecutionIdentity(clientCtx)
	if err != nil {
		return nil, "", err
	}
	forceRescan, err := cmd.Flags().GetBool(flagRescanWallet)
	if err != nil {
		return nil, "", err
	}
	foundNotes, err := scanNotesWithOptions(clientCtx, identity.seed, scanNotesOptions{logWriter: privacyCommandLogWriter(cmd), forceRescan: forceRescan})
	if err != nil {
		return nil, "", err
	}
	inputIndexes, err := cmd.Flags().GetIntSlice(flagBatchInputIndex)
	if err != nil {
		return nil, "", err
	}
	inputs, err := selectBatchTransferInputs(foundNotes, denom, paymentTotal, inputIndexes)
	if err != nil {
		return nil, "", err
	}
	rawMode, err := cmd.Flags().GetString(flagBatchOutputMode)
	if err != nil {
		return nil, "", err
	}
	mode := privacybatchtransfer.OutputMode(strings.ToLower(strings.TrimSpace(rawMode)))
	plan, err := privacybatchtransfer.PlanBatchTransfer(privacybatchtransfer.PlanBatchTransferInput{
		Inputs:           inputs,
		Payments:         payments,
		OwnerSpendPubKey: identity.spendPubKey,
		OwnerViewPubKey:  identity.viewPubKey,
		Mode:             mode,
	})
	if err != nil {
		if len(payments) == int(privacytypes.BatchJoinSplitV1MaxOutputs) && paymentTotal.Cmp(sumBatchTransferInputs(inputs)) != 0 {
			return nil, "", fmt.Errorf("exact 32 payments require selected inputs to equal the payment total; choose exact --input-index values or prepare notes first: %w", err)
		}
		return nil, "", err
	}
	prepared, err := privacybatchtransfer.PrepareBatchTransfer(cmd.Context(), batchTransferMerklePathProvider{
		inner: privacyprovider.NewTransferQueryProvider(privacytypes.NewQueryClient(clientCtx)),
	}, plan)
	if err != nil {
		return nil, "", err
	}
	disableSelfView, err := cmd.Flags().GetBool(flagTransferNoSelfView)
	if err != nil {
		return nil, "", err
	}
	var selfViewTarget *crypto_tedwards.PointAffine
	if !disableSelfView {
		_, selfViewTarget, _, err = deriveDisclosureKeys(identity.seed)
		if err != nil {
			return nil, "", err
		}
		if selfViewTarget == nil {
			return nil, "", fmt.Errorf("derive self-view disclosure key: empty public key")
		}
	}
	expiresAtUnix, err := resolveTransferExpiresAtUnix(cmd)
	if err != nil {
		return nil, "", err
	}
	payload, err := privacybatchtransfer.BuildPreparedAuditV2BatchTransferPayload(prepared, privacybatchtransfer.BuildPreparedAuditV2BatchTransferPayloadInput{
		Creator:                        clientCtx.GetFromAddress().String(),
		ChainID:                        clientCtx.ChainID,
		ExpiresAtUnix:                  expiresAtUnix,
		SelfViewDisclosureTargetPubKey: selfViewTarget,
		DisableSelfViewDisclosure:      disableSelfView,
	})
	if err != nil {
		return nil, "", err
	}
	path, err := cmd.Flags().GetString(flagBatchPreparedOut)
	if err != nil {
		return nil, "", err
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, "", fmt.Errorf("--%s is required", flagBatchPreparedOut)
	}
	if err := privacybatchtransfer.WritePreparedAuditV2BatchTransferPayload(path, payload); err != nil {
		return nil, "", err
	}
	return payload, path, nil
}

func proveBatchTransferFromFile(cmd *cobra.Command, clientCtx client.Context, preparedPath string) (*privacybatchtransfer.PreparedAuditV2BatchTransferProof, string, string, error) {
	payload, err := privacybatchtransfer.ReadPreparedAuditV2BatchTransferPayload(preparedPath)
	if err != nil {
		return nil, "", "", err
	}
	if payload.ChainID != clientCtx.ChainID {
		return nil, "", "", fmt.Errorf("prepared batch transfer chain ID %q does not match current chain ID %q", payload.ChainID, clientCtx.ChainID)
	}
	if err := privacybatchtransfer.ValidatePreparedAuditV2BatchTransferPayloadAt(payload, time.Now()); err != nil {
		return nil, "", "", fmt.Errorf("prepared batch transfer validation failed: %w", err)
	}
	runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
	if err != nil {
		return nil, "", "", err
	}
	runtime.expiresAt = payload.ExpiresAtUnix
	snapshot, err := runtime.snapshotSource(cmd.Context())
	if err != nil {
		return nil, "", "", err
	}
	prepared, err := privacybatchtransfer.PrepareAuditV2FromPreparedAuditV2Payload(snapshot, payload)
	if err != nil {
		return nil, "", "", err
	}
	public := prepared.PublicInputs()
	publicBytes := make([][]byte, len(public))
	for i := range public {
		publicBytes[i] = public[i].Bytes()
	}
	identity, err := resolveTransferExecutionIdentity(clientCtx)
	if err != nil {
		prepared.Clear()
		return nil, "", "", err
	}
	full, err := privacybatchtransfer.BuildAuditV2WitnessFromPayload(prepared, payload, func(intent *big.Int) ([]byte, error) {
		return manualSign(intent, identity.scalar, identity.spendPubKey)
	})
	if err != nil {
		prepared.Clear()
		return nil, "", "", err
	}
	msg, err := runtime.prove(cmd, prepared, full)
	if err != nil {
		return nil, "", "", err
	}
	batch, ok := msg.(*privacyv2.MsgBatchTransfer)
	if !ok {
		return nil, "", "", fmt.Errorf("audit prover returned %T, expected v2 batch transfer", msg)
	}
	hash := snapshot.ArtifactHash
	proof := &privacybatchtransfer.PreparedAuditV2BatchTransferProof{Version: privacybatchtransfer.PreparedAuditV2BatchTransferProofVersion, RequestPayloadHash: payload.PayloadHash, Message: batch, PublicInputs: publicBytes, ArtifactHash: hash[:]}
	prover := "local"
	if strings.TrimSpace(runtime.prover.BaseURL) != "" {
		prover = "remote"
	}
	proofPath, err := cmd.Flags().GetString(flagBatchProofOut)
	if err != nil {
		return nil, "", "", err
	}
	proofPath = strings.TrimSpace(proofPath)
	if proofPath == "" {
		return nil, "", "", fmt.Errorf("--%s is required", flagBatchProofOut)
	}
	if err := privacybatchtransfer.WritePreparedAuditV2BatchTransferProof(proofPath, proof); err != nil {
		return nil, "", "", err
	}
	return proof, proofPath, prover, nil
}

func broadcastBatchTransferArtifacts(cmd *cobra.Command, clientCtx client.Context, payload *privacybatchtransfer.PreparedAuditV2BatchTransferPayload, proof *privacybatchtransfer.PreparedAuditV2BatchTransferProof, output batchTransferCommandOutput) error {
	if err := validateAuditV2BatchProof(cmd, clientCtx, payload, proof); err != nil {
		return fmt.Errorf("prepared batch transfer proof validation failed: %w", err)
	}
	creator := clientCtx.GetFromAddress().String()
	if payload.Creator != "" && payload.Creator != creator {
		return fmt.Errorf("prepared payload creator %q does not match tx signer %q", payload.Creator, creator)
	}
	msg := *proof.Message
	msg.Creator = creator
	broadcaster := privacyprovider.CosmosTxBroadcaster{ClientContext: clientCtx, Flags: cmd.Flags(), FromName: clientCtx.GetFromName()}
	if transferBatchUsesStandardTxCLI(clientCtx) {
		return broadcaster.GenerateOrBroadcast(&msg)
	}
	response, err := broadcaster.BroadcastSDKMessage(cmd.Context(), &msg)
	if err != nil {
		return err
	}
	output.TxHash = response.TxHash
	output.Height = response.Height
	output.Code = response.Code
	if privacyCommandOutputJSONEnabled(cmd) {
		return printCommandJSON(cmd, output)
	}
	privacyCommandOutputPrintf(cmd, "submitted one-proof batch transfer tx %s (%d inputs, %d outputs)\n", response.TxHash, len(payload.Inputs), len(payload.Outputs))
	return nil
}

// validateAuditV2BatchProof binds a relayable final message to both the
// private preparation and the currently active authenticated audit runtime.
// It intentionally verifies from PI23, never from an archived full witness.
func validateAuditV2BatchProof(cmd *cobra.Command, clientCtx client.Context, payload *privacybatchtransfer.PreparedAuditV2BatchTransferPayload, proof *privacybatchtransfer.PreparedAuditV2BatchTransferProof) error {
	if payload == nil || payload.ChainID != clientCtx.ChainID {
		preparedChainID := ""
		if payload != nil {
			preparedChainID = payload.ChainID
		}
		return fmt.Errorf("prepared batch transfer chain ID %q does not match current chain ID %q", preparedChainID, clientCtx.ChainID)
	}
	if err := privacybatchtransfer.ValidatePreparedAuditV2BatchTransferPayloadAt(payload, time.Now()); err != nil {
		return err
	}
	if proof == nil || proof.Version != privacybatchtransfer.PreparedAuditV2BatchTransferProofVersion || proof.RequestPayloadHash != payload.PayloadHash || proof.Message == nil || len(proof.ArtifactHash) != 32 {
		return fmt.Errorf("invalid v2 batch proof artifact")
	}
	if proof.Message.Creator != payload.Creator || proof.Message.ExpiresAtUnix != payload.ExpiresAtUnix || !bytes.Equal(proof.Message.Root, payload.Root) || len(proof.Message.Nullifiers) != len(payload.Inputs) || len(proof.Message.Outputs) != len(payload.Effects) {
		return fmt.Errorf("v2 batch final message does not match prepared payload")
	}
	for i := range payload.Inputs {
		if !bytes.Equal(proof.Message.Nullifiers[i], payload.Inputs[i].Nullifier) {
			return fmt.Errorf("v2 batch final message nullifier %d does not match prepared payload", i)
		}
	}
	for i := range payload.Effects {
		if !proto.Equal(proof.Message.Outputs[i], payload.Effects[i]) {
			return fmt.Errorf("v2 batch final message output %d does not match prepared payload", i)
		}
	}
	if _, err := privacytypes.ValidateAuditMessage(proof.Message); err != nil {
		return fmt.Errorf("invalid v2 batch final message: %w", err)
	}
	runtime, err := resolveAuditV2Runtime(cmd, clientCtx)
	if err != nil {
		return err
	}
	snapshot, err := runtime.snapshotSource(cmd.Context())
	if err != nil {
		return err
	}
	keyID := snapshot.Key.IDBytes()
	if proof.Message.Audit == nil || proof.Message.Audit.Epoch != snapshot.Epoch || !bytes.Equal(proof.Message.Audit.KeyId, keyID[:]) || !bytes.Equal(snapshot.ArtifactHash[:], proof.ArtifactHash) {
		return fmt.Errorf("v2 batch proof artifact is stale; prove again")
	}
	if err := privacyaudit.VerifyFinalMessageBinding(snapshot, proof.Message, proof.PublicInputs); err != nil {
		return fmt.Errorf("v2 batch message/proof binding failed: %w", err)
	}
	return privacyaudit.VerifyFinalProof(4, proof.PublicInputs, proof.Message.Proof, runtime.registry, runtime.identity)
}

func parseBatchTransferPayments(rawPayments []string) ([]privacybatchtransfer.Payment, string, *big.Int, error) {
	if len(rawPayments) == 0 || len(rawPayments) > int(privacytypes.BatchJoinSplitV1MaxOutputs) {
		return nil, "", nil, fmt.Errorf("--%s must be repeated 1..32 times", flagBatchPayment)
	}
	payments := make([]privacybatchtransfer.Payment, 0, len(rawPayments))
	denom := ""
	total := new(big.Int)
	for i, raw := range rawPayments {
		parts := strings.Split(raw, ",")
		if len(parts) < 2 || len(parts) > 5 {
			return nil, "", nil, fmt.Errorf("payment %d must use shielded_address,coin[,privacy-policy,disclosure-mode,disclosure-pubkey-hex]", i+1)
		}
		for j := range parts {
			parts[j] = strings.TrimSpace(parts[j])
		}
		spend, view, err := resolveTransferRecipient(parts[0])
		if err != nil {
			return nil, "", nil, fmt.Errorf("payment %d shielded address: %w", i+1, err)
		}
		coin, err := sdk.ParseCoinNormalized(parts[1])
		if err != nil {
			return nil, "", nil, fmt.Errorf("payment %d coin: %w", i+1, err)
		}
		if !coin.Amount.IsPositive() {
			return nil, "", nil, fmt.Errorf("payment %d amount must be positive", i+1)
		}
		if denom == "" {
			denom = coin.Denom
		} else if coin.Denom != denom {
			return nil, "", nil, fmt.Errorf("all batch payments must use denom %q; payment %d uses %q", denom, i+1, coin.Denom)
		}
		policyRaw, modeRaw, targetRaw := transferPrivacyPolicyAllPrivate, transferDisclosureModeNone, ""
		if len(parts) >= 3 {
			policyRaw = parts[2]
		}
		if len(parts) >= 4 {
			modeRaw = parts[3]
		}
		if len(parts) == 5 {
			targetRaw = parts[4]
		}
		policy, err := privacytransfer.ParsePrivacyPolicy(policyRaw)
		if err != nil {
			return nil, "", nil, fmt.Errorf("payment %d: %w", i+1, err)
		}
		mode, err := privacytransfer.ParseDisclosureMode(modeRaw)
		if err != nil {
			return nil, "", nil, fmt.Errorf("payment %d: %w", i+1, err)
		}
		var disclosureTarget *crypto_tedwards.PointAffine
		switch {
		case policy == privacytypes.TransferPrivacyPolicyAllPrivate && mode != privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
			return nil, "", nil, fmt.Errorf("payment %d all-private policy requires none disclosure mode", i+1)
		case policy == privacytypes.TransferPrivacyPolicyAllPrivate && targetRaw != "":
			return nil, "", nil, fmt.Errorf("payment %d all-private policy must not set a disclosure key", i+1)
		case policy != privacytypes.TransferPrivacyPolicyAllPrivate && mode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE:
			return nil, "", nil, fmt.Errorf("payment %d non-private policy requires public or recipient-encrypted disclosure mode", i+1)
		case mode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC && targetRaw != "":
			return nil, "", nil, fmt.Errorf("payment %d public disclosure must not set a disclosure key", i+1)
		case mode == privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
			disclosureTarget, _, err = privacytransfer.DecodeDisclosurePubKeyHex(targetRaw)
			if err != nil {
				return nil, "", nil, fmt.Errorf("payment %d disclosure key: %w", i+1, err)
			}
		}
		payments = append(payments, privacybatchtransfer.Payment{SpendPubKey: spend, ViewPubKey: view, Amount: coin.Amount.BigInt(), PrivacyPolicy: policy, DisclosureMode: mode, DisclosureTargetPubKey: disclosureTarget})
		total.Add(total, coin.Amount.BigInt())
	}
	return payments, denom, total, nil
}

func selectBatchTransferInputs(found []FoundNote, denom string, target *big.Int, explicitIndexes []int) ([]privacybatchtransfer.InputNote, error) {
	if target == nil || target.Sign() <= 0 {
		return nil, fmt.Errorf("batch payment total must be positive")
	}
	assetID := privacytypes.ComputeAssetIDV1(denom)
	assetBytes := assetID.FillBytes(make([]byte, 32))
	expectedAsset, err := privacycrypto.ParseFieldValueBE32(assetBytes)
	if err != nil {
		return nil, err
	}
	eligible := func(note FoundNote) bool {
		if note.IsSpent || note.Note.AssetID.Bytes() != expectedAsset.Bytes() {
			return false
		}
		return note.AssetDenom == "" || note.AssetDenom == denom
	}
	if len(explicitIndexes) > 0 {
		if len(explicitIndexes) > int(privacytypes.BatchJoinSplitV1MaxInputs) {
			return nil, privacybatchtransfer.ErrPreparationRequired
		}
		seen := make(map[int]struct{}, len(explicitIndexes))
		inputs := make([]privacybatchtransfer.InputNote, 0, len(explicitIndexes))
		for _, index := range explicitIndexes {
			if index <= 0 || index > len(found) {
				return nil, fmt.Errorf("--%s %d is outside list-notes range 1..%d", flagBatchInputIndex, index, len(found))
			}
			if _, exists := seen[index]; exists {
				return nil, fmt.Errorf("duplicate --%s %d", flagBatchInputIndex, index)
			}
			seen[index] = struct{}{}
			selected := found[index-1]
			if !eligible(selected) {
				return nil, fmt.Errorf("--%s %d is spent or does not use denom %q", flagBatchInputIndex, index, denom)
			}
			inputs = append(inputs, privacybatchtransfer.InputNote{Note: selected.Note})
		}
		if sumBatchTransferInputs(inputs).Cmp(privacytypes.MaxShieldedAmount()) > 0 {
			return nil, privacybatchtransfer.ErrPreparationRequired
		}
		if sumBatchTransferInputs(inputs).Cmp(target) < 0 {
			return nil, fmt.Errorf("selected inputs do not fund batch payment total %s%s", target, denom)
		}
		return inputs, nil
	}

	type candidate struct {
		found FoundNote
		key   string
	}
	candidates := make([]candidate, 0, len(found))
	for index, note := range found {
		if !eligible(note) {
			continue
		}
		key := note.Nullifier
		if key == "" {
			key = fmt.Sprintf("wallet-%020d", index)
		}
		candidates = append(candidates, candidate{found: note, key: key})
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i].found.Note.Amount, candidates[j].found.Note.Amount
		if left != right {
			return left.Cmp(right) > 0
		}
		return candidates[i].key < candidates[j].key
	})
	inputs := make([]privacybatchtransfer.InputNote, 0, minInt(len(candidates), int(privacytypes.BatchJoinSplitV1MaxInputs)))
	total := new(big.Int)
	for _, candidate := range candidates {
		if len(inputs) == int(privacytypes.BatchJoinSplitV1MaxInputs) {
			break
		}
		nextTotal := new(big.Int).Add(total, privacytypes.Amount128BigInt(candidate.found.Note.Amount))
		if nextTotal.Cmp(privacytypes.MaxShieldedAmount()) > 0 {
			continue
		}
		inputs = append(inputs, privacybatchtransfer.InputNote{Note: candidate.found.Note})
		total = nextTotal
		if total.Cmp(target) >= 0 {
			return inputs, nil
		}
	}
	return nil, privacybatchtransfer.ErrPreparationRequired
}

func sumBatchTransferInputs(inputs []privacybatchtransfer.InputNote) *big.Int {
	total := new(big.Int)
	for i := range inputs {
		total.Add(total, privacytypes.Amount128BigInt(inputs[i].Note.Amount))
	}
	return total
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

type structuredBatchTransferSigner struct {
	scalar privacycrypto.SecretScalar
	pubKey *crypto_tedwards.PointAffine
}

func (s structuredBatchTransferSigner) SignBatchTransfer(request privacybatchtransfer.BatchTransferSigningRequest) ([]byte, error) {
	if err := privacybatchtransfer.ValidateBatchTransferSigningRequest(request); err != nil {
		return nil, err
	}
	if request.ExpectedIntent == nil || !s.scalar.IsValid() || s.pubKey == nil {
		return nil, fmt.Errorf("structured batch signing request and owner key are required")
	}
	derived, err := privacycrypto.PublicKey(s.scalar)
	if err != nil {
		return nil, err
	}
	derivedBytes, configuredBytes := derived.Bytes(), s.pubKey.Bytes()
	if !bytes.Equal(derivedBytes[:], configuredBytes[:]) || !bytes.Equal(configuredBytes[:], request.OwnerSpendPubKey) {
		return nil, fmt.Errorf("structured batch signing owner key does not match the canonical request")
	}
	return manualSign(request.ExpectedIntent, s.scalar, s.pubKey)
}

type batchTransferMerklePathProvider struct {
	inner privacyprovider.TransferQueryProvider
}

func (p batchTransferMerklePathProvider) LookupMerklePath(ctx context.Context, commitmentHex string) (*privacybatchtransfer.MerklePathResult, error) {
	path, err := p.inner.LookupMerklePath(ctx, commitmentHex)
	if err != nil {
		return nil, err
	}
	return &privacybatchtransfer.MerklePathResult{Root: append([]byte(nil), path.Root...), Path: append([]string(nil), path.Path...), PathHelper: append([]uint32(nil), path.PathHelper...)}, nil
}

type batchTransferArtifactProvider struct{}

func (batchTransferArtifactProvider) BatchJoinSplitR1CS() (constraint.ConstraintSystem, error) {
	return zk.GetBatchJoinSplit16x32R1CS()
}

func (batchTransferArtifactProvider) BatchJoinSplitProvingKey() (groth16.ProvingKey, error) {
	return zk.GetBatchJoinSplit16x32ProvingKey()
}

type batchTransferProofRunner struct{ logWriter io.Writer }

func (r batchTransferProofRunner) ProveBatchJoinSplit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, batchWitness witness.Witness) (groth16.Proof, error) {
	return withGnarkLoggerOutput(r.logWriter, func() (groth16.Proof, error) {
		return groth16.Prove(r1cs, provingKey, batchWitness)
	})
}

type batchTransferMessageBroadcaster struct {
	broadcaster privacyprovider.CosmosTxBroadcaster
}

func (b batchTransferMessageBroadcaster) BroadcastBatchTransferMessage(ctx context.Context, msg *privacytypes.MsgBatchTransfer) (*sdk.TxResponse, error) {
	return b.broadcaster.BroadcastSDKMessage(ctx, msg)
}
