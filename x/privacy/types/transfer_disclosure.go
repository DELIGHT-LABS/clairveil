package types

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
)

const TransferDisclosureRecipientOutputIndex uint32 = 0
const TransferAuditDisclosureDomain uint32 = 255
const TransferSelfViewDisclosureDomain uint32 = 254

func ComputeTransferDisclosureDigestBytes(
	policy uint32,
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) ([]byte, error) {
	if err := validateTransferDisclosurePolicy(policy); err != nil {
		return nil, err
	}

	if err := validateFieldElementBytesStrict("disclosure commitment", commitment); err != nil {
		return nil, err
	}

	if policy == TransferPrivacyPolicyAllPrivate {
		return canonicalDigestBytes(big.NewInt(0))
	}

	selectedAmount := big.NewInt(0)
	selectedAssetID := big.NewInt(0)
	selectedFromSpendX := big.NewInt(0)
	selectedFromSpendY := big.NewInt(0)
	selectedFromViewX := big.NewInt(0)
	selectedFromViewY := big.NewInt(0)
	selectedToSpendX := big.NewInt(0)
	selectedToSpendY := big.NewInt(0)
	selectedToViewX := big.NewInt(0)
	selectedToViewY := big.NewInt(0)

	if policy&TransferPrivacyPolicyDiscloseAmount != 0 {
		if amount == nil {
			return nil, fmt.Errorf("amount is required for amount disclosure")
		}
		if assetID == nil {
			return nil, fmt.Errorf("asset id is required for amount disclosure")
		}
		selectedAmount = new(big.Int).Set(amount)
		selectedAssetID = new(big.Int).Set(assetID)
	}

	if policy&TransferPrivacyPolicyDiscloseFrom != 0 {
		if fromSpendPubKeyX == nil || fromSpendPubKeyY == nil || fromViewPubKeyX == nil || fromViewPubKeyY == nil {
			return nil, fmt.Errorf("full sender shielded address is required for from disclosure")
		}
		selectedFromSpendX = new(big.Int).Set(fromSpendPubKeyX)
		selectedFromSpendY = new(big.Int).Set(fromSpendPubKeyY)
		selectedFromViewX = new(big.Int).Set(fromViewPubKeyX)
		selectedFromViewY = new(big.Int).Set(fromViewPubKeyY)
	}

	if policy&TransferPrivacyPolicyDiscloseTo != 0 {
		if toSpendPubKeyX == nil || toSpendPubKeyY == nil || toViewPubKeyX == nil || toViewPubKeyY == nil {
			return nil, fmt.Errorf("full recipient shielded address is required for to disclosure")
		}
		selectedToSpendX = new(big.Int).Set(toSpendPubKeyX)
		selectedToSpendY = new(big.Int).Set(toSpendPubKeyY)
		selectedToViewX = new(big.Int).Set(toViewPubKeyX)
		selectedToViewY = new(big.Int).Set(toViewPubKeyY)
	}

	digest := privacycrypto.MimcHash(
		big.NewInt(int64(policy)),
		big.NewInt(int64(outputIndex)),
		new(big.Int).SetBytes(commitment),
		selectedAmount,
		selectedAssetID,
		selectedFromSpendX,
		selectedFromSpendY,
		selectedFromViewX,
		selectedFromViewY,
		selectedToSpendX,
		selectedToSpendY,
		selectedToViewX,
		selectedToViewY,
	)

	return canonicalDigestBytes(digest)
}

func ComputeAuditTransferDisclosureDigestBytes(
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) ([]byte, error) {
	if err := validateFieldElementBytesStrict("audit disclosure commitment", commitment); err != nil {
		return nil, err
	}
	if amount == nil || assetID == nil {
		return nil, fmt.Errorf("audit disclosure requires amount and asset id")
	}
	if fromSpendPubKeyX == nil || fromSpendPubKeyY == nil || fromViewPubKeyX == nil || fromViewPubKeyY == nil {
		return nil, fmt.Errorf("audit disclosure requires the full sender shielded address")
	}
	if toSpendPubKeyX == nil || toSpendPubKeyY == nil || toViewPubKeyX == nil || toViewPubKeyY == nil {
		return nil, fmt.Errorf("audit disclosure requires the full recipient shielded address")
	}

	digest := privacycrypto.MimcHash(
		big.NewInt(int64(TransferAuditDisclosureDomain)),
		big.NewInt(int64(outputIndex)),
		new(big.Int).SetBytes(commitment),
		new(big.Int).Set(amount),
		new(big.Int).Set(assetID),
		new(big.Int).Set(fromSpendPubKeyX),
		new(big.Int).Set(fromSpendPubKeyY),
		new(big.Int).Set(fromViewPubKeyX),
		new(big.Int).Set(fromViewPubKeyY),
		new(big.Int).Set(toSpendPubKeyX),
		new(big.Int).Set(toSpendPubKeyY),
		new(big.Int).Set(toViewPubKeyX),
		new(big.Int).Set(toViewPubKeyY),
	)

	return canonicalDigestBytes(digest)
}

func ComputeSelfViewTransferDisclosureDigestBytes(
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) ([]byte, error) {
	if err := validateFieldElementBytesStrict("self-view disclosure commitment", commitment); err != nil {
		return nil, err
	}
	if amount == nil || assetID == nil {
		return nil, fmt.Errorf("self-view disclosure requires amount and asset id")
	}
	if fromSpendPubKeyX == nil || fromSpendPubKeyY == nil || fromViewPubKeyX == nil || fromViewPubKeyY == nil {
		return nil, fmt.Errorf("self-view disclosure requires the full sender shielded address")
	}
	if toSpendPubKeyX == nil || toSpendPubKeyY == nil || toViewPubKeyX == nil || toViewPubKeyY == nil {
		return nil, fmt.Errorf("self-view disclosure requires the full recipient shielded address")
	}

	digest := privacycrypto.MimcHash(
		big.NewInt(int64(TransferSelfViewDisclosureDomain)),
		big.NewInt(int64(outputIndex)),
		new(big.Int).SetBytes(commitment),
		new(big.Int).Set(amount),
		new(big.Int).Set(assetID),
		new(big.Int).Set(fromSpendPubKeyX),
		new(big.Int).Set(fromSpendPubKeyY),
		new(big.Int).Set(fromViewPubKeyX),
		new(big.Int).Set(fromViewPubKeyY),
		new(big.Int).Set(toSpendPubKeyX),
		new(big.Int).Set(toSpendPubKeyY),
		new(big.Int).Set(toViewPubKeyX),
		new(big.Int).Set(toViewPubKeyY),
	)

	return canonicalDigestBytes(digest)
}

func ComputeTransferDisclosureDigestHex(
	policy uint32,
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) (string, error) {
	bz, err := ComputeTransferDisclosureDigestBytes(
		policy,
		outputIndex,
		commitment,
		amount,
		assetID,
		fromSpendPubKeyX,
		fromSpendPubKeyY,
		fromViewPubKeyX,
		fromViewPubKeyY,
		toSpendPubKeyX,
		toSpendPubKeyY,
		toViewPubKeyX,
		toViewPubKeyY,
	)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bz), nil
}

func ComputeAuditTransferDisclosureDigestHex(
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) (string, error) {
	bz, err := ComputeAuditTransferDisclosureDigestBytes(
		outputIndex,
		commitment,
		amount,
		assetID,
		fromSpendPubKeyX,
		fromSpendPubKeyY,
		fromViewPubKeyX,
		fromViewPubKeyY,
		toSpendPubKeyX,
		toSpendPubKeyY,
		toViewPubKeyX,
		toViewPubKeyY,
	)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bz), nil
}

func ComputeSelfViewTransferDisclosureDigestHex(
	outputIndex uint32,
	commitment []byte,
	amount *big.Int,
	assetID *big.Int,
	fromSpendPubKeyX *big.Int,
	fromSpendPubKeyY *big.Int,
	fromViewPubKeyX *big.Int,
	fromViewPubKeyY *big.Int,
	toSpendPubKeyX *big.Int,
	toSpendPubKeyY *big.Int,
	toViewPubKeyX *big.Int,
	toViewPubKeyY *big.Int,
) (string, error) {
	bz, err := ComputeSelfViewTransferDisclosureDigestBytes(
		outputIndex,
		commitment,
		amount,
		assetID,
		fromSpendPubKeyX,
		fromSpendPubKeyY,
		fromViewPubKeyX,
		fromViewPubKeyY,
		toSpendPubKeyX,
		toSpendPubKeyY,
		toViewPubKeyX,
		toViewPubKeyY,
	)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(bz), nil
}

func canonicalDigestBytes(v *big.Int) ([]byte, error) {
	var elem fr.Element
	elem.SetBigInt(v)
	bz := elem.Bytes()
	return append([]byte(nil), bz[:]...), nil
}
