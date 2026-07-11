package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
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

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacyprovider "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provider"
	privacytransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/transfer"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
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
broadcasting, so either stage can be resumed with the companion commands. Set
--prover-url to use only POST /v1/proofs/batch-transfer. When it is omitted,
only the local prover is used; there is no automatic prover failover.

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
			proof, proofPath, prover, err := proveBatchTransferFromFile(cmd, preparedPath)
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
and write a structured owner-signed prepared payload with mode 0600.

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
Validate the prepared payload version, expiry, structured owner signature, and
payload hash before proving. By default the local BatchJoinSplit16x32 artifacts
are used. Setting --prover-url exclusively selects POST
/v1/proofs/batch-transfer. The command never retries another prover.
		`),
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			proof, path, prover, err := proveBatchTransferFromFile(cmd, args[0])
			if err != nil {
				return err
			}
			payload, err := privacybatchtransfer.ReadPreparedBatchTransferPayload(args[0])
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
	cmd.Flags().StringP(flags.FlagOutput, "o", "text", "Output format (text|json)")
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
			payload, err := privacybatchtransfer.ReadPreparedBatchTransferPayload(args[0])
			if err != nil {
				return err
			}
			proof, err := privacybatchtransfer.ReadPreparedBatchTransferProof(args[1])
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
	cmd.Flags().String(flagBatchProverURL, "", "Exclusive remote prover base URL; omitted uses only the local prover")
	cmd.Flags().Duration(flagBatchProverTimeout, defaultBatchProverWait, "Remote prover request timeout")
}

func prepareBatchTransferFromFlags(cmd *cobra.Command, clientCtx client.Context) (*privacybatchtransfer.PreparedBatchTransferPayload, string, error) {
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
	audit, err := privacytypes.NewQueryClient(clientCtx).AuditConfig(cmd.Context(), &privacytypes.QueryAuditConfigRequest{})
	if err != nil {
		return nil, "", fmt.Errorf("query audit identity: %w", err)
	}
	if audit == nil {
		return nil, "", fmt.Errorf("query audit identity returned no response")
	}
	auditTarget, _, err := privacytransfer.DecodeDisclosurePubKeyHex(audit.AuditMasterPubkeyHex)
	if err != nil {
		return nil, "", fmt.Errorf("invalid chain audit target: %w", err)
	}
	disableSelfView, err := cmd.Flags().GetBool(flagTransferNoSelfView)
	if err != nil {
		return nil, "", err
	}
	var selfViewTarget *crypto_tedwards.PointAffine
	if !disableSelfView {
		_, selfViewTarget, _ = deriveDisclosureKeys(identity.seed)
		if selfViewTarget == nil {
			return nil, "", fmt.Errorf("derive self-view disclosure key: empty public key")
		}
	}
	expiresAtUnix, err := resolveTransferExpiresAtUnix(cmd)
	if err != nil {
		return nil, "", err
	}
	payload, err := privacybatchtransfer.BuildPreparedBatchTransferPayload(prepared, structuredBatchTransferSigner{
		scalar: identity.scalar,
		pubKey: identity.spendPubKey,
	}, privacybatchtransfer.BuildPreparedBatchTransferPayloadInput{
		Creator:                        clientCtx.GetFromAddress().String(),
		ChainID:                        clientCtx.ChainID,
		ExpiresAtUnix:                  expiresAtUnix,
		AuditKeyID:                     audit.AuditKeyId,
		AuditKeyEpoch:                  audit.AuditKeyEpoch,
		AuditDisclosureTargetPubKey:    auditTarget,
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
	if err := privacybatchtransfer.WritePreparedBatchTransferPayload(path, payload); err != nil {
		return nil, "", err
	}
	return payload, path, nil
}

func proveBatchTransferFromFile(cmd *cobra.Command, preparedPath string) (*privacybatchtransfer.PreparedBatchTransferProof, string, string, error) {
	payload, err := privacybatchtransfer.ReadPreparedBatchTransferPayload(preparedPath)
	if err != nil {
		return nil, "", "", err
	}
	if err := privacybatchtransfer.ValidatePreparedBatchTransferPayloadMetadata(payload); err != nil {
		return nil, "", "", fmt.Errorf("prepared batch transfer validation failed: %w", err)
	}
	proverURL, err := cmd.Flags().GetString(flagBatchProverURL)
	if err != nil {
		return nil, "", "", err
	}
	proverURL = strings.TrimSpace(proverURL)
	var proof *privacybatchtransfer.PreparedBatchTransferProof
	prover := "local"
	if proverURL == "" {
		proof, err = privacybatchtransfer.ProvePreparedBatchTransfer(payload, batchTransferArtifactProvider{}, batchTransferProofRunner{logWriter: privacyCommandLogWriter(cmd)})
	} else {
		prover = "remote"
		timeout, timeoutErr := cmd.Flags().GetDuration(flagBatchProverTimeout)
		if timeoutErr != nil {
			return nil, "", "", timeoutErr
		}
		if timeout <= 0 {
			return nil, "", "", fmt.Errorf("--%s must be positive", flagBatchProverTimeout)
		}
		request, requestErr := privacyprovertransport.NewBatchTransferProofRequest(*payload)
		if requestErr != nil {
			return nil, "", "", requestErr
		}
		response, requestErr := (privacyprovertransport.HTTPProverClient{
			BaseURL: proverURL,
			Client:  &http.Client{Timeout: timeout},
		}).ProveBatchTransfer(cmd.Context(), *request)
		if requestErr != nil {
			return nil, "", "", requestErr
		}
		proof = &response.Proof
	}
	if err != nil {
		return nil, "", "", err
	}
	if err := privacybatchtransfer.ValidatePreparedBatchTransferProofAt(payload, proof, time.Now()); err != nil {
		return nil, "", "", err
	}
	proofPath, err := cmd.Flags().GetString(flagBatchProofOut)
	if err != nil {
		return nil, "", "", err
	}
	proofPath = strings.TrimSpace(proofPath)
	if proofPath == "" {
		return nil, "", "", fmt.Errorf("--%s is required", flagBatchProofOut)
	}
	if err := privacybatchtransfer.WritePreparedBatchTransferProof(proofPath, proof); err != nil {
		return nil, "", "", err
	}
	return proof, proofPath, prover, nil
}

func broadcastBatchTransferArtifacts(cmd *cobra.Command, clientCtx client.Context, payload *privacybatchtransfer.PreparedBatchTransferPayload, proof *privacybatchtransfer.PreparedBatchTransferProof, output batchTransferCommandOutput) error {
	if err := privacybatchtransfer.ValidatePreparedBatchTransferProofAt(payload, proof, time.Now()); err != nil {
		return fmt.Errorf("prepared batch transfer proof validation failed: %w", err)
	}
	creator := clientCtx.GetFromAddress().String()
	if payload.Creator != "" && payload.Creator != creator {
		return fmt.Errorf("prepared payload creator %q does not match tx signer %q", payload.Creator, creator)
	}
	msg, err := privacybatchtransfer.BuildMsgBatchTransfer(payload, proof, creator)
	if err != nil {
		return err
	}
	broadcaster := privacyprovider.CosmosTxBroadcaster{ClientContext: clientCtx, Flags: cmd.Flags(), FromName: clientCtx.GetFromName()}
	if transferBatchUsesStandardTxCLI(clientCtx) {
		return broadcaster.GenerateOrBroadcast(msg)
	}
	response, err := privacybatchtransfer.BroadcastBatchTransfer(cmd.Context(), batchTransferMessageBroadcaster{broadcaster: broadcaster}, payload, proof, creator)
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
	eligible := func(note FoundNote) bool {
		if note.IsSpent || note.Note.Amount == nil || note.Note.AssetID == nil || note.Note.AssetID.Cmp(assetID) != 0 {
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
		cmp := candidates[i].found.Note.Amount.Cmp(candidates[j].found.Note.Amount)
		if cmp != 0 {
			return cmp > 0
		}
		return candidates[i].key < candidates[j].key
	})
	inputs := make([]privacybatchtransfer.InputNote, 0, minInt(len(candidates), int(privacytypes.BatchJoinSplitV1MaxInputs)))
	total := new(big.Int)
	for _, candidate := range candidates {
		if len(inputs) == int(privacytypes.BatchJoinSplitV1MaxInputs) {
			break
		}
		inputs = append(inputs, privacybatchtransfer.InputNote{Note: candidate.found.Note})
		total.Add(total, candidate.found.Note.Amount)
		if total.Cmp(target) >= 0 {
			return inputs, nil
		}
	}
	return nil, privacybatchtransfer.ErrPreparationRequired
}

func sumBatchTransferInputs(inputs []privacybatchtransfer.InputNote) *big.Int {
	total := new(big.Int)
	for i := range inputs {
		if inputs[i].Note.Amount != nil {
			total.Add(total, inputs[i].Note.Amount)
		}
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
	scalar *big.Int
	pubKey *crypto_tedwards.PointAffine
}

func (s structuredBatchTransferSigner) SignBatchTransfer(request privacybatchtransfer.BatchTransferSigningRequest) ([]byte, error) {
	if err := privacybatchtransfer.ValidateBatchTransferSigningRequest(request); err != nil {
		return nil, err
	}
	if request.ExpectedIntent == nil || s.scalar == nil || s.pubKey == nil {
		return nil, fmt.Errorf("structured batch signing request and owner key are required")
	}
	curve := crypto_tedwards.GetEdwardsCurve()
	var derived crypto_tedwards.PointAffine
	derived.ScalarMultiplication(&curve.Base, s.scalar)
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
