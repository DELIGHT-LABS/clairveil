package cli

import (
	"github.com/spf13/cobra"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
)

const flagDisclosureKeyJSON = "json"

type disclosurePublicKeySummary struct {
	FromAddress   string `json:"from_address"`
	PublicKeyHex  string `json:"public_key_hex"`
	ShieldedAddr  string `json:"shielded_address,omitempty"`
	DerivedFrom   string `json:"derived_from"`
	CreatedAtHint string `json:"created_at_hint,omitempty"`
}

func CmdShowDisclosurePubKey() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show-disclosure-pubkey",
		Short: "Show the disclosure public key derived from the current transparent account",
		Long: `Derive the default disclosure public key from the current transparent keyring account.

This command uses the same root seed derivation model as shielded spend/view keys, so
you only need --from to select the account.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			clientCtx, err := client.GetClientTxContext(cmd)
			if err != nil {
				return err
			}

			rootSeed, fromAddress, err := derivePrivacyRootSeed(clientCtx)
			if err != nil {
				return err
			}
			_, disclosurePubKey, _ := deriveDisclosureKeys(rootSeed)
			pubKeyHex := encodePointHex(disclosurePubKey)

			asJSON, err := cmd.Flags().GetBool(flagDisclosureKeyJSON)
			if err != nil {
				return err
			}

			summary := disclosurePublicKeySummary{
				FromAddress:  fromAddress.String(),
				PublicKeyHex: pubKeyHex,
				DerivedFrom:  "transparent-keyring-root",
			}

			if asJSON || privacyCommandOutputJSONEnabled(cmd) {
				return printCommandJSON(cmd, summary)
			}

			printDisclosurePublicKeySummary(cmd, pubKeyHex, fromAddress.String())
			return nil
		},
	}

	cmd.Flags().Bool(flagDisclosureKeyJSON, false, "Print the disclosure public key as JSON (same effect as --output json)")
	flags.AddTxFlagsToCmd(cmd)
	return cmd
}
