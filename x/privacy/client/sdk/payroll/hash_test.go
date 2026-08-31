package payroll

import (
	"math/big"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOperationHashInputsRejectInvalidProtocolValues(t *testing.T) {
	_, err := HashRecipient("   ")
	require.ErrorContains(t, err, "recipient is required")
	_, err = HashRecipient("clair1recipient")
	require.ErrorContains(t, err, "canonical shielded address")

	for _, testCase := range []struct {
		name   string
		denom  string
		amount *big.Int
	}{
		{name: "empty denom", denom: "", amount: big.NewInt(0)},
		{name: "malformed denom", denom: "bad!denom", amount: big.NewInt(0)},
		{name: "non-canonical denom whitespace", denom: " uclair", amount: big.NewInt(0)},
		{name: "negative amount", denom: "uclair", amount: big.NewInt(-1)},
		{name: "uint64 overflow", denom: "uclair", amount: new(big.Int).Add(new(big.Int).SetUint64(^uint64(0)), big.NewInt(1))},
		{name: "missing amount", denom: "uclair", amount: nil},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := HashAmount(testCase.denom, testCase.amount)
			require.Error(t, err)
		})
	}
}

func TestHashRecipientUsesCanonicalShieldedAddress(t *testing.T) {
	recipient := "clairs19x5u4mf4l4zqcpvr7d809fh4tjy5j50p2mwgky0nj38jpqpj7svndu3hqshu5e3s8w6pea5p30xek5p9flxjf7f44xh7cnfrlsd84pc7upgh3"
	want, err := HashRecipient(recipient)
	require.NoError(t, err)

	got, err := HashRecipient(strings.ToUpper(recipient))
	require.NoError(t, err)
	require.Equal(t, want, got)
}
