package batchtransfer

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/stretchr/testify/require"
)

func TestPreparedOutputPreservesLegacyJSONHashBytes(t *testing.T) {
	output := testPayload(t).Outputs[0]
	user, full := output.UserDisclosureBlinding.Bytes(), output.FullDisclosureBlinding.Bytes()
	legacy := struct {
		Kind           OutputKind                      `json:"kind"`
		Note           privacytypes.Note               `json:"note"`
		PrivacyPolicy  uint32                          `json:"privacy_policy"`
		DisclosureMode privacytypes.UserDisclosureMode `json:"disclosure_mode"`
		Target         []byte                          `json:"disclosure_target_pubkey,omitempty"`
		User           *big.Int                        `json:"user_disclosure_blinding"`
		Full           *big.Int                        `json:"full_disclosure_blinding"`
	}{output.Kind, output.Note.ToProverWitnessV1(), output.PrivacyPolicy, output.DisclosureMode, output.DisclosureTargetPubKey, new(big.Int).SetBytes(user[:]), new(big.Int).SetBytes(full[:])}
	expected, err := json.Marshal(legacy)
	require.NoError(t, err)
	actual, err := json.Marshal(output)
	require.NoError(t, err)
	require.JSONEq(t, string(expected), string(actual))
	require.Equal(t, string(expected), string(actual))
	var decoded PreparedBatchTransferOutput
	require.NoError(t, json.Unmarshal(actual, &decoded))
	require.Equal(t, output, decoded)
	malformed := strings.Replace(string(actual), `"note":{`, `"note":{"unexpected":1,`, 1)
	require.Error(t, json.Unmarshal([]byte(malformed), &decoded))
	require.Equal(t, output, decoded)
}
