package types

const (
	DisclosurePayloadVersion      = "v4"
	DisclosureConfigSchemaVersion = "v1"
)

var supportedUserDisclosurePolicies = []string{
	"all-private",
	"amount",
	"to",
	"amount-to",
	"from",
	"amount-from",
	"from-to",
	"amount-from-to",
}

var supportedUserDisclosureModes = []string{
	"none",
	"public",
	"recipient-encrypted",
}

func SupportedUserDisclosurePolicies() []string {
	out := make([]string, len(supportedUserDisclosurePolicies))
	copy(out, supportedUserDisclosurePolicies)
	return out
}

func SupportedUserDisclosureModes() []string {
	out := make([]string, len(supportedUserDisclosureModes))
	copy(out, supportedUserDisclosureModes)
	return out
}
