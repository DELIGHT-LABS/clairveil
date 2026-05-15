package types

const (
	// Bech32MainPrefix defines the main SDK Bech32 prefix of an account address.
	Bech32MainPrefix = "clair"

	// ShieldedBech32Prefix defines the full shielded address prefix.
	ShieldedBech32Prefix = "clairs"

	PrefixValidator = "val"
	PrefixConsensus = "cons"
	PrefixPublic    = "pub"
	PrefixOperator  = "oper"

	Bech32PrefixAccAddr  = Bech32MainPrefix
	Bech32PrefixAccPub   = Bech32MainPrefix + PrefixPublic
	Bech32PrefixValAddr  = Bech32MainPrefix + PrefixValidator + PrefixOperator
	Bech32PrefixValPub   = Bech32MainPrefix + PrefixValidator + PrefixOperator + PrefixPublic
	Bech32PrefixConsAddr = Bech32MainPrefix + PrefixValidator + PrefixConsensus
	Bech32PrefixConsPub  = Bech32MainPrefix + PrefixValidator + PrefixConsensus + PrefixPublic
)
