package types

const (
	DisclosurePayloadVersion      = FixedPayloadVersionV1
	DisclosureConfigSchemaVersion = "v1"
	AssetRegistryVersionV1        = "privacy-asset-registry-v1"
	PrivacyScanSchemaVersionV2    = "privacy-scan-v2"
	PrivacyEventSequenceVersionV1 = "privacy-sequence-v1"
	PrivacyStateVersionV2         = uint32(2)
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
