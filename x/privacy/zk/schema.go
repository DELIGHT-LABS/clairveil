package zk

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
)

const PublicInputSchemaDomain = "clairveil.public-input-schema.v1"

type PublicInputField struct {
	Name     string
	Encoding string
}

var publicInputSchemas = map[string][]PublicInputField{
	"deposit": {
		{Name: "Commitment", Encoding: "bn254-fr"},
		{Name: "Amount", Encoding: "uint64"},
		{Name: "AssetID", Encoding: "bn254-fr"},
	},
	"spend": {
		{Name: "MerkleRoot", Encoding: "bn254-fr"},
		{Name: "ChainDomainHi", Encoding: "uint128"},
		{Name: "ChainDomainLo", Encoding: "uint128"},
		{Name: "ExpiresAtUnix", Encoding: "uint64"},
		{Name: "Nullifier", Encoding: "bn254-fr"},
		{Name: "Amount", Encoding: "uint64"},
		{Name: "RecipientDigestHi", Encoding: "uint128"},
		{Name: "RecipientDigestLo", Encoding: "uint128"},
		{Name: "AssetID", Encoding: "bn254-fr"},
	},
	"joinsplit": {
		{Name: "MerkleRoot", Encoding: "bn254-fr"},
		{Name: "ChainDomainHi", Encoding: "uint128"},
		{Name: "ChainDomainLo", Encoding: "uint128"},
		{Name: "ExpiresAtUnix", Encoding: "uint64"},
		{Name: "Nullifier0", Encoding: "bn254-fr"},
		{Name: "Nullifier1", Encoding: "bn254-fr"},
		{Name: "Commitment0", Encoding: "bn254-fr"},
		{Name: "Commitment1", Encoding: "bn254-fr"},
		{Name: "UserPrivacyPolicy", Encoding: "uint3"},
		{Name: "UserDisclosureDigest", Encoding: "bn254-fr"},
		{Name: "FullDisclosureDigest", Encoding: "bn254-fr"},
		{Name: "PayloadDigestHi", Encoding: "uint128"},
		{Name: "PayloadDigestLo", Encoding: "uint128"},
	},
}

func PublicInputSchema(circuitID string) ([]PublicInputField, error) {
	fields, ok := publicInputSchemas[circuitID]
	if !ok {
		return nil, fmt.Errorf("unsupported circuit id %q", circuitID)
	}
	return append([]PublicInputField(nil), fields...), nil
}

func PublicInputSchemaSHA256(circuitID string) (string, error) {
	fields, err := PublicInputSchema(circuitID)
	if err != nil {
		return "", err
	}
	var encoded bytes.Buffer
	encoded.WriteString(PublicInputSchemaDomain)
	writeSchemaString(&encoded, circuitID)
	writeSchemaUint32(&encoded, uint32(len(fields)))
	for i, field := range fields {
		writeSchemaUint32(&encoded, uint32(i+1))
		writeSchemaString(&encoded, field.Name)
		writeSchemaString(&encoded, field.Encoding)
	}
	sum := sha256.Sum256(encoded.Bytes())
	return hex.EncodeToString(sum[:]), nil
}

func writeSchemaString(dst *bytes.Buffer, value string) {
	writeSchemaUint32(dst, uint32(len(value)))
	_, _ = dst.WriteString(value)
}

func writeSchemaUint32(dst *bytes.Buffer, value uint32) {
	var encoded [4]byte
	binary.BigEndian.PutUint32(encoded[:], value)
	_, _ = dst.Write(encoded[:])
}
