package cli

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"

	"github.com/spf13/cobra"

	cmtabci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/rpc/core/types"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"

	privacydisclosure "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/disclosure"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

const flagTransferDisclosurePrivKey = "disclosure-privkey"
const flagTransferDisclosureTxHash = "tx-hash"
const flagTransferDisclosureReport = "report"
const flagTransferDisclosurePlane = "disclosure-plane"

const (
	transferDisclosurePlaneAuto      = "auto"
	transferDisclosurePlanePublic    = "public"
	transferDisclosurePlaneRecipient = "recipient"
	transferDisclosurePlaneAudit     = "audit"
)

type transferDisclosureDecodeInput struct {
	PayloadHex         string
	OnChainDigestHex   string
	TxHash             string
	IsPlaintextPayload bool
	SelectedPlane      string
}

type transferDisclosureEventData struct {
	PayloadHex          string
	DisclosureDigestHex string
	IsPlaintextPayload  bool
	SelectedPlane       string
}

type transferDisclosureEventAttrs struct {
	UserMode        string
	UserDigestHex   string
	UserPayloadHex  string
	AuditDigestHex  string
	AuditPayloadHex string
}

type transferDisclosureVerificationReport = privacydisclosure.VerificationReport

type transferDisclosureSummaryReport struct {
	Plane               string   `json:"plane"`
	Delivery            string   `json:"delivery"`
	Policy              string   `json:"policy"`
	DisclosedFields     []string `json:"disclosed_fields"`
	Amount              string   `json:"amount,omitempty"`
	AssetDenom          string   `json:"asset_denom,omitempty"`
	FromShieldedAddress string   `json:"from_shielded_address,omitempty"`
	ToShieldedAddress   string   `json:"to_shielded_address,omitempty"`
}

type transferDisclosureReport struct {
	Source       string                               `json:"source"`
	TxHash       string                               `json:"tx_hash,omitempty"`
	Verification transferDisclosureVerificationReport `json:"verification"`
	Summary      transferDisclosureSummaryReport      `json:"summary"`
	Payload      *transferDisclosurePayload           `json:"payload"`
}

func CmdDecodeTransferDisclosure() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "decode-transfer-disclosure [ciphertext_hex]",
		Short: "Decode and verify a transfer disclosure payload",
		Long: strings.TrimSpace(`
Decode a disclosure payload and verify that it matches the latest transfer disclosure digest rules.

	- Pass ciphertext_hex directly when you already have the encrypted payload bytes.
	- Use --tx-hash when you want the CLI to look up the disclosure event for you.
	- Choose the disclosure plane with --disclosure-plane=auto|public|recipient|audit.
	- Public disclosure does not require a disclosure private key.
	- For recipient/audit disclosure, either pass --disclosure-privkey or let the CLI derive it from --from.
	- --report renders source, verification, summary, and payload in one JSON object.
		`),
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reportOutput, err := cmd.Flags().GetBool(flagTransferDisclosureReport)
			if err != nil {
				return err
			}

			decodeInput, err := resolveTransferDisclosureCipherTextInput(cmd, args)
			if err != nil {
				return err
			}

			var payload *transferDisclosurePayload
			if decodeInput.IsPlaintextPayload {
				payload, err = decodePublicTransferDisclosurePayload(decodeInput.PayloadHex)
			} else {
				disclosurePrivKeyHex, err := cmd.Flags().GetString(flagTransferDisclosurePrivKey)
				if err != nil {
					return err
				}
				resolvedDisclosurePrivKeyHex, err := resolveDisclosurePrivateKeyHexFromCmd(cmd, disclosurePrivKeyHex)
				if err != nil {
					return err
				}
				payload, err = decryptTransferDisclosureCipherText(decodeInput.PayloadHex, resolvedDisclosurePrivKeyHex)
			}
			if err != nil {
				return err
			}

			verification, err := verifyTransferDisclosurePayload(payload, decodeInput.OnChainDigestHex)
			if err != nil {
				return err
			}

			rendered := any(payload)
			if reportOutput {
				rendered = buildTransferDisclosureReport(payload, decodeInput, verification)
			}

			if err := printCommandJSON(cmd, rendered); err != nil {
				return fmt.Errorf("failed to render disclosure payload: %w", err)
			}

			return nil
		},
	}

	cmd.Flags().String(flagTransferDisclosurePrivKey, "", "Disclosure private key scalar in hex")
	cmd.Flags().String(flagTransferDisclosureTxHash, "", "Transaction hash containing a transfer disclosure event")
	cmd.Flags().String(flagTransferDisclosurePlane, transferDisclosurePlaneAuto, "Disclosure plane to decode from a transfer tx: auto|public|recipient|audit")
	cmd.Flags().Bool(flagTransferDisclosureReport, false, "Render a verification report instead of the raw disclosure payload JSON")
	cmd.Flags().String(flags.FlagFrom, "", "Name or address of private key used to derive the disclosure key when --disclosure-privkey is omitted")
	flags.AddKeyringFlags(cmd.Flags())
	flags.AddQueryFlagsToCmd(cmd)

	return cmd
}

func resolveDisclosurePrivateKeyHexFromCmd(cmd *cobra.Command, explicitHex string) (string, error) {
	if strings.TrimSpace(explicitHex) != "" {
		return strings.TrimSpace(explicitHex), nil
	}

	clientCtx, err := client.GetClientTxContext(cmd)
	if err != nil {
		return "", err
	}

	rootSeed, _, err := derivePrivacyRootSeed(clientCtx)
	if err != nil {
		return "", err
	}

	disclosureScalar, _, _ := deriveDisclosureKeys(rootSeed)
	return scalarToFixedHex(disclosureScalar), nil
}

func resolveTransferDisclosureCipherTextInput(cmd *cobra.Command, args []string) (*transferDisclosureDecodeInput, error) {
	txHash, err := cmd.Flags().GetString(flagTransferDisclosureTxHash)
	if err != nil {
		return nil, err
	}

	if len(args) > 0 && txHash != "" {
		return nil, fmt.Errorf("provide either ciphertext_hex or --%s, not both", flagTransferDisclosureTxHash)
	}
	if len(args) > 0 {
		return &transferDisclosureDecodeInput{PayloadHex: args[0]}, nil
	}
	if txHash == "" {
		return nil, fmt.Errorf("provide ciphertext_hex or set --%s", flagTransferDisclosureTxHash)
	}

	plane, err := cmd.Flags().GetString(flagTransferDisclosurePlane)
	if err != nil {
		return nil, err
	}
	plane, err = normalizeTransferDisclosurePlane(plane)
	if err != nil {
		return nil, err
	}

	clientCtx, err := client.GetClientQueryContext(cmd)
	if err != nil {
		return nil, err
	}

	return lookupTransferDisclosureCipherTextByTxHash(cmd.Context(), clientCtx, txHash, plane)
}

func lookupTransferDisclosureCipherTextByTxHash(ctx context.Context, clientCtx client.Context, txHash string, plane string) (*transferDisclosureDecodeInput, error) {
	if clientCtx.Client == nil {
		return nil, fmt.Errorf("an RPC client is required to look up --%s; set --node or pass ciphertext_hex directly", flagTransferDisclosureTxHash)
	}

	hashBytes, err := hex.DecodeString(strings.TrimSpace(txHash))
	if err != nil {
		return nil, fmt.Errorf("invalid --%s value: %w", flagTransferDisclosureTxHash, err)
	}

	txRes, err := clientCtx.Client.Tx(ctx, hashBytes, false)
	if err != nil {
		return nil, fmt.Errorf("failed to query the tx for --%s %q: %w", flagTransferDisclosureTxHash, txHash, err)
	}

	eventData, err := extractTransferDisclosureEventDataFromTx(txRes, plane)
	if err != nil {
		return nil, err
	}

	return &transferDisclosureDecodeInput{
		PayloadHex:         eventData.PayloadHex,
		OnChainDigestHex:   eventData.DisclosureDigestHex,
		TxHash:             strings.ToUpper(strings.TrimSpace(txHash)),
		IsPlaintextPayload: eventData.IsPlaintextPayload,
		SelectedPlane:      eventData.SelectedPlane,
	}, nil
}

func normalizeTransferDisclosurePlane(raw string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", transferDisclosurePlaneAuto:
		return transferDisclosurePlaneAuto, nil
	case transferDisclosurePlanePublic:
		return transferDisclosurePlanePublic, nil
	case transferDisclosurePlaneRecipient:
		return transferDisclosurePlaneRecipient, nil
	case transferDisclosurePlaneAudit:
		return transferDisclosurePlaneAudit, nil
	default:
		return "", fmt.Errorf("unsupported --%s value %q; supported values: auto, public, recipient, audit", flagTransferDisclosurePlane, raw)
	}
}

func extractTransferDisclosureEventDataFromTx(txRes *cmttypes.ResultTx, plane string) (*transferDisclosureEventData, error) {
	attrs, err := extractTransferDisclosureEventAttrs(txRes.TxResult.Events)
	if err != nil {
		return nil, err
	}

	switch plane {
	case transferDisclosurePlaneAuto:
		if attrs.UserMode == types.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED.String() && attrs.UserPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.UserPayloadHex,
				DisclosureDigestHex: attrs.UserDigestHex,
				SelectedPlane:       transferDisclosurePlaneRecipient,
			}, nil
		}
		if attrs.UserMode == types.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String() && attrs.UserPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.UserPayloadHex,
				DisclosureDigestHex: attrs.UserDigestHex,
				IsPlaintextPayload:  true,
				SelectedPlane:       transferDisclosurePlanePublic,
			}, nil
		}
		if attrs.AuditPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.AuditPayloadHex,
				DisclosureDigestHex: attrs.AuditDigestHex,
				SelectedPlane:       transferDisclosurePlaneAudit,
			}, nil
		}
	case transferDisclosurePlanePublic:
		if attrs.UserMode == types.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC.String() && attrs.UserPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.UserPayloadHex,
				DisclosureDigestHex: attrs.UserDigestHex,
				IsPlaintextPayload:  true,
				SelectedPlane:       transferDisclosurePlanePublic,
			}, nil
		}
	case transferDisclosurePlaneRecipient:
		if attrs.UserMode == types.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED.String() && attrs.UserPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.UserPayloadHex,
				DisclosureDigestHex: attrs.UserDigestHex,
				SelectedPlane:       transferDisclosurePlaneRecipient,
			}, nil
		}
	case transferDisclosurePlaneAudit:
		if attrs.AuditPayloadHex != "" {
			return &transferDisclosureEventData{
				PayloadHex:          attrs.AuditPayloadHex,
				DisclosureDigestHex: attrs.AuditDigestHex,
				SelectedPlane:       transferDisclosurePlaneAudit,
			}, nil
		}
	}

	return nil, fmt.Errorf("no disclosure payload for plane %q was found in tx %X", plane, txRes.Hash)
}

func extractTransferDisclosureEventAttrs(events []cmtabci.Event) (*transferDisclosureEventAttrs, error) {
	for _, event := range events {
		if event.Type != types.EventTypeShieldedTransfer {
			continue
		}

		var attrs transferDisclosureEventAttrs
		for _, attr := range event.Attributes {
			key := string(attr.Key)
			value := removeQuotes(string(attr.Value))

			switch key {
			case types.AttributeKeyUserDisclosureMode:
				attrs.UserMode = value
			case types.AttributeKeyUserDisclosureDigest:
				attrs.UserDigestHex = value
			case types.AttributeKeyUserDisclosurePayload:
				attrs.UserPayloadHex = value
			case types.AttributeKeyAuditDisclosureDigest:
				attrs.AuditDigestHex = value
			case types.AttributeKeyAuditDisclosurePayload:
				attrs.AuditPayloadHex = value
			}
		}

		if attrs.UserPayloadHex != "" || attrs.AuditPayloadHex != "" {
			return &attrs, nil
		}
	}

	return nil, fmt.Errorf("no shielded_transfer event with a disclosure payload was found")
}

func decodePublicTransferDisclosurePayload(payloadHex string) (*transferDisclosurePayload, error) {
	return privacydisclosure.DecodePublicPayloadHex(payloadHex)
}

func decryptTransferDisclosureCipherText(cipherTextHex string, disclosurePrivKeyHex string) (*transferDisclosurePayload, error) {
	disclosureScalar, err := decodeDisclosurePrivateKeyHex(disclosurePrivKeyHex)
	if err != nil {
		return nil, err
	}

	return privacydisclosure.DecryptPayloadHex(cipherTextHex, disclosureScalar)
}

func verifyTransferDisclosurePayload(payload *transferDisclosurePayload, onChainDigestHex string) (*transferDisclosureVerificationReport, error) {
	return privacydisclosure.VerifyPayload(payload, onChainDigestHex)
}

func computeExpectedDisclosureDigest(payload *transferDisclosurePayload) (string, *transferDisclosureVerificationReport, error) {
	return privacydisclosure.ComputeExpectedDisclosureDigest(payload)
}

func disclosureAmountAndAsset(payload *transferDisclosurePayload) (*big.Int, *big.Int, error) {
	return privacydisclosure.DisclosureAmountAndAsset(payload)
}

func buildTransferDisclosureReport(
	payload *transferDisclosurePayload,
	input *transferDisclosureDecodeInput,
	verification *transferDisclosureVerificationReport,
) *transferDisclosureReport {
	return &transferDisclosureReport{
		Source:       disclosureReportSource(payload, input),
		TxHash:       input.TxHash,
		Verification: *verification,
		Summary: transferDisclosureSummaryReport{
			Plane:               disclosureReportPlane(payload),
			Delivery:            disclosureReportDelivery(payload, input),
			Policy:              disclosureReportPolicy(payload),
			DisclosedFields:     disclosedFieldsFromPayload(payload),
			Amount:              payload.Amount,
			AssetDenom:          payload.AssetDenom,
			FromShieldedAddress: payload.FromShieldedAddress,
			ToShieldedAddress:   payload.ToShieldedAddress,
		},
		Payload: payload,
	}
}

func disclosureReportSource(payload *transferDisclosurePayload, input *transferDisclosureDecodeInput) string {
	if payload.Plane == transferDisclosurePayloadPlaneAudit {
		return "audit_encrypted"
	}
	if input.IsPlaintextPayload {
		return "public"
	}
	return "recipient_encrypted"
}

func disclosureReportPlane(payload *transferDisclosurePayload) string {
	if payload.Plane == transferDisclosurePayloadPlaneAudit {
		return transferDisclosurePayloadPlaneAudit
	}
	return transferDisclosurePayloadPlaneUser
}

func disclosureReportDelivery(payload *transferDisclosurePayload, input *transferDisclosureDecodeInput) string {
	if payload.Plane == transferDisclosurePayloadPlaneAudit {
		return "audit-encrypted"
	}
	if input.IsPlaintextPayload {
		return transferDisclosureModePublic
	}
	return transferDisclosureModeRecipientEncrypted
}

func disclosureReportPolicy(payload *transferDisclosurePayload) string {
	if payload.Plane == transferDisclosurePayloadPlaneAudit {
		return "audit-full"
	}
	return policyLabel(payload.Policy)
}

func disclosedFieldsFromPayload(payload *transferDisclosurePayload) []string {
	return privacydisclosure.DisclosedFields(payload)
}
