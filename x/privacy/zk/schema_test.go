package zk

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
)

func TestPublicInputSchemaDigestsGoldenVectors(t *testing.T) {
	for circuitID, expected := range map[string]string{
		"deposit":                  "47892885a109e9864ef7640dc558bf3b0d54f6b9d918d796932a9ab41fe51ddf",
		"spend":                    "7b3111bdcaa990f4820895427df571e21dd773881a8da8f3ee3acc15528d6658",
		"joinsplit":                "4946e23db34529c6fce0a95ce69f6df08563a305ddcc70c7b6b786471e03aa82",
		"batch-joinsplit-16x32-v1": "5606327d69dcb06c00811f2135291d39a2ea1cedf554f114f7eb4a178098d333",
	} {
		digest, err := PublicInputSchemaSHA256(circuitID)
		require.NoError(t, err)
		require.Equal(t, expected, digest, circuitID)
	}
}

func TestPublicInputSchemasMatchCircuitDeclarationOrder(t *testing.T) {
	for _, tc := range []struct {
		name     string
		circuit  any
		expected []string
	}{
		{name: "deposit", circuit: &circuit.DepositCircuit{}, expected: []string{"Commitment", "Amount", "AssetID"}},
		{name: "spend", circuit: &circuit.SpendCircuit{}, expected: []string{"MerkleRoot", "ChainDomainHi", "ChainDomainLo", "ExpiresAtUnix", "Nullifier", "Amount", "RecipientDigestHi", "RecipientDigestLo", "AssetID"}},
		{name: "joinsplit", circuit: &circuit.JoinSplitCircuit{}, expected: []string{"MerkleRoot", "ChainDomainHi", "ChainDomainLo", "ExpiresAtUnix", "Nullifiers_0", "Nullifiers_1", "Commitments_0", "Commitments_1", "UserPrivacyPolicy", "UserDisclosureDigest", "FullDisclosureDigest", "PayloadDigestHi", "PayloadDigestLo"}},
		{name: "batch-joinsplit-16x32-v1", circuit: &circuit.BatchJoinSplit16x32{}, expected: []string{"MerkleRoot", "ChainDomainHi", "ChainDomainLo", "ExpiresAtUnix", "InputCount", "OutputCount", "NullifierRoot", "CommitmentRoot", "UserDisclosureRoot", "FullDisclosureRoot", "PayloadDigestHi", "PayloadDigestLo"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.expected, declaredPublicFieldNames(tc.circuit))
		})
	}
}

func declaredPublicFieldNames(value any) []string {
	typeOf := reflect.TypeOf(value)
	if typeOf.Kind() == reflect.Pointer {
		typeOf = typeOf.Elem()
	}
	var names []string
	for i := 0; i < typeOf.NumField(); i++ {
		field := typeOf.Field(i)
		if !strings.Contains(field.Tag.Get("gnark"), "public") {
			continue
		}
		if field.Type.Kind() == reflect.Array {
			for j := 0; j < field.Type.Len(); j++ {
				names = append(names, fmt.Sprintf("%s_%d", field.Name, j))
			}
			continue
		}
		names = append(names, field.Name)
	}
	return names
}
