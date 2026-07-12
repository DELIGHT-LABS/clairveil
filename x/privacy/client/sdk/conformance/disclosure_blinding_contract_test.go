package conformance_test

import (
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

type disclosureBlindingContractFixture struct {
	SchemaVersion string `json:"schema_version"`
	InvariantID   string `json:"invariant_id"`
	Scope         struct {
		JoinSplitOutputIndex                uint32 `json:"joinsplit_2x2_disclosure_output_index"`
		JoinSplitChangeHasDisclosureWitness bool   `json:"joinsplit_2x2_change_output_has_disclosure_witness"`
		BatchSemantics                      string `json:"batch_semantics"`
		CrossOutputGlobalFreshnessIncluded  bool   `json:"cross_output_global_freshness_included"`
	} `json:"scope"`
	Relations []struct {
		ID       string `json:"id"`
		When     string `json:"when"`
		Left     string `json:"left"`
		Operator string `json:"operator"`
		Right    string `json:"right"`
	} `json:"relations"`
	Canonicalization struct {
		ActiveAllPrivate struct {
			PrivacyPolicy     string   `json:"privacy_policy"`
			UserBlinding      string   `json:"user_disclosure_blinding"`
			FullBlinding      string   `json:"full_disclosure_blinding"`
			GatedOffRelations []string `json:"gated_off_relations"`
		} `json:"active_all_private"`
		DisabledOutputSlot struct {
			PrivacyPolicy     string   `json:"privacy_policy"`
			OutputRandomness  string   `json:"output_randomness"`
			UserBlinding      string   `json:"user_disclosure_blinding"`
			FullBlinding      string   `json:"full_disclosure_blinding"`
			GatedOffRelations []string `json:"gated_off_relations"`
		} `json:"disabled_output_slot"`
	} `json:"canonicalization"`
	RequiredEnforcementLayers []string `json:"required_enforcement_layers"`
	ErrorCodes                []string `json:"error_codes"`
	Vectors                   []struct {
		Name             string `json:"name"`
		OutputIndex      uint32 `json:"output_index"`
		Enabled          bool   `json:"enabled"`
		PrivacyPolicy    uint32 `json:"privacy_policy"`
		OutputRandomness string `json:"output_randomness"`
		UserBlinding     string `json:"user_disclosure_blinding"`
		FullBlinding     string `json:"full_disclosure_blinding"`
		Valid            bool   `json:"valid"`
		ErrorCode        string `json:"error_code"`
	} `json:"vectors"`
}

func TestPrivacyDisclosureBlindingV1Contract(t *testing.T) {
	fixture := loadDisclosureBlindingContractFixture(t)
	require.Equal(t, "v1", fixture.SchemaVersion)
	require.Equal(t, "DISCLOSURE-BLINDING-SEPARATION", fixture.InvariantID)
	require.Equal(t, privacytypes.TransferDisclosureRecipientOutputIndex, fixture.Scope.JoinSplitOutputIndex)
	require.False(t, fixture.Scope.JoinSplitChangeHasDisclosureWitness)
	require.Equal(t, "per-output-slot", fixture.Scope.BatchSemantics)
	require.False(t, fixture.Scope.CrossOutputGlobalFreshnessIncluded)
	require.Len(t, fixture.Relations, 3)
	require.Equal(t, "DBS-01", fixture.Relations[0].ID)
	require.Equal(t, "enabled && privacy_policy != 0", fixture.Relations[0].When)
	require.Equal(t, "user_disclosure_blinding", fixture.Relations[0].Left)
	require.Equal(t, "!=", fixture.Relations[0].Operator)
	require.Equal(t, "output_randomness", fixture.Relations[0].Right)
	require.Equal(t, "DBS-02", fixture.Relations[1].ID)
	require.Equal(t, "enabled", fixture.Relations[1].When)
	require.Equal(t, "full_disclosure_blinding", fixture.Relations[1].Left)
	require.Equal(t, "!=", fixture.Relations[1].Operator)
	require.Equal(t, "output_randomness", fixture.Relations[1].Right)
	require.Equal(t, "DBS-03", fixture.Relations[2].ID)
	require.Equal(t, "enabled", fixture.Relations[2].When)
	require.Equal(t, "full_disclosure_blinding", fixture.Relations[2].Left)
	require.Equal(t, "!=", fixture.Relations[2].Operator)
	require.Equal(t, "user_disclosure_blinding", fixture.Relations[2].Right)
	require.Equal(t, "0", fixture.Canonicalization.ActiveAllPrivate.PrivacyPolicy)
	require.Equal(t, "0", fixture.Canonicalization.ActiveAllPrivate.UserBlinding)
	require.Equal(t, "non-zero", fixture.Canonicalization.ActiveAllPrivate.FullBlinding)
	require.Equal(t, []string{"DBS-01"}, fixture.Canonicalization.ActiveAllPrivate.GatedOffRelations)
	require.Equal(t, "0", fixture.Canonicalization.DisabledOutputSlot.PrivacyPolicy)
	require.Equal(t, "0", fixture.Canonicalization.DisabledOutputSlot.OutputRandomness)
	require.Equal(t, "0", fixture.Canonicalization.DisabledOutputSlot.UserBlinding)
	require.Equal(t, "0", fixture.Canonicalization.DisabledOutputSlot.FullBlinding)
	require.Equal(t, []string{"DBS-01", "DBS-02", "DBS-03"}, fixture.Canonicalization.DisabledOutputSlot.GatedOffRelations)
	require.Equal(t, []string{"circuit", "native_validator", "prepared_validator", "structured_signer"}, fixture.RequiredEnforcementLayers)
	require.Equal(t, []string{
		string(privacytypes.DisclosureBlindingErrorInvalidPolicyV1),
		string(privacytypes.DisclosureBlindingErrorNonCanonicalFieldV1),
		string(privacytypes.DisclosureBlindingErrorDisabledSentinelV1),
		string(privacytypes.DisclosureBlindingErrorAllPrivateUserSentinelV1),
		string(privacytypes.DisclosureBlindingErrorUserBlindingRequiredV1),
		string(privacytypes.DisclosureBlindingErrorFullBlindingRequiredV1),
		string(privacytypes.DisclosureBlindingErrorUserRandomnessReuseV1),
		string(privacytypes.DisclosureBlindingErrorFullRandomnessReuseV1),
		string(privacytypes.DisclosureBlindingErrorUserFullBlindingReuseV1),
	}, fixture.ErrorCodes)

	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			err := privacytypes.ValidateDisclosureBlindingSeparationV1(privacytypes.DisclosureBlindingSeparationV1Input{
				OutputIndex:            vector.OutputIndex,
				Enabled:                vector.Enabled,
				PrivacyPolicy:          vector.PrivacyPolicy,
				OutputRandomness:       fixtureDecimal(t, vector.OutputRandomness),
				UserDisclosureBlinding: fixtureDecimal(t, vector.UserBlinding),
				FullDisclosureBlinding: fixtureDecimal(t, vector.FullBlinding),
			})
			if vector.Valid {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			var invariantErr *privacytypes.DisclosureBlindingErrorV1
			require.True(t, errors.As(err, &invariantErr))
			require.Equal(t, vector.ErrorCode, string(invariantErr.Code))
		})
	}
}

func loadDisclosureBlindingContractFixture(t *testing.T) disclosureBlindingContractFixture {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	bz, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "testdata", "privacy_disclosure_blinding_v1_contract.json"))
	require.NoError(t, err)
	var fixture disclosureBlindingContractFixture
	require.NoError(t, json.Unmarshal(bz, &fixture))
	return fixture
}

func fixtureDecimal(t *testing.T, value string) *big.Int {
	t.Helper()
	result, ok := new(big.Int).SetString(value, 10)
	require.True(t, ok)
	return result
}
