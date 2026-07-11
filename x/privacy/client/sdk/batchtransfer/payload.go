package batchtransfer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"time"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func BuildPreparedBatchTransferPayload(prepared *PreparedBatchTransfer, signer BatchTransferSigner, input BuildPreparedBatchTransferPayloadInput) (*PreparedBatchTransferPayload, error) {
	if prepared == nil || signer == nil {
		return nil, fmt.Errorf("prepared transfer and structured batch signer are required")
	}
	if input.ChainID == "" || input.ExpiresAtUnix <= 0 {
		return nil, fmt.Errorf("chain id and positive expiry are required")
	}
	if input.AuditDisclosureTargetPubKey == nil {
		return nil, fmt.Errorf("audit disclosure target public key is required")
	}
	auditBytes := input.AuditDisclosureTargetPubKey.Bytes()
	p := &PreparedBatchTransferPayload{Version: PreparedBatchTransferPayloadVersion, CircuitSetID: BatchTransferCircuitSetID, Creator: input.Creator, ChainID: input.ChainID, ExpiresAtUnix: input.ExpiresAtUnix, Root: append([]byte(nil), prepared.Root...), AssetID: new(big.Int).Set(prepared.AssetID), Inputs: prepared.Inputs, Outputs: prepared.Outputs, AuditKeyID: input.AuditKeyID, AuditKeyEpoch: input.AuditKeyEpoch, AuditDisclosureTargetPubKey: append([]byte(nil), auditBytes[:]...)}
	p.MessageOutputs = make([]*privacytypes.BatchTransferOutput, len(p.Outputs))
	commitments := make([]*big.Int, 32)
	userDigests := make([]*big.Int, 32)
	fullDigests := make([]*big.Int, 32)
	policies := make([]uint32, 32)
	for i := range commitments {
		commitments[i], userDigests[i], fullDigests[i] = new(big.Int), new(big.Int), new(big.Int)
	}
	owner := p.Inputs[0].Note
	for i, output := range p.Outputs {
		commitment := output.Note.ComputeCommitment()
		commitments[i] = commitment
		policies[i] = output.PrivacyPolicy
		commitmentBytes, _ := privacyfield.CanonicalBytesFromBigInt(commitment)
		notePlaintext, err := privacytypes.MarshalNotePlaintextV1(&output.Note)
		if err != nil {
			return nil, err
		}
		viewKey, err := pointFromCoordinates(output.Note.ReceiverViewPubKeyX, output.Note.ReceiverViewPubKeyY)
		if err != nil {
			return nil, err
		}
		raw, tag, err := privacycrypto.AsymEncryptWithViewTag(notePlaintext, *viewKey, commitmentBytes, uint32(i))
		if err != nil {
			return nil, err
		}
		ciphertext, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeTransferNoteV1, raw)
		if err != nil {
			return nil, err
		}

		userPlain, userDigest, err := disclosurePlaintext(uint32(i), false, owner, output)
		if err != nil {
			return nil, err
		}
		fullPlain, fullDigest, err := disclosurePlaintext(uint32(i), true, owner, output)
		if err != nil {
			return nil, err
		}
		userDigests[i], fullDigests[i] = userDigest, fullDigest
		fullRaw, err := privacycrypto.AsymEncrypt(fullPlain, *input.AuditDisclosureTargetPubKey)
		if err != nil {
			return nil, err
		}
		auditPayload, err := privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeAuditDisclosureV1, fullRaw)
		if err != nil {
			return nil, err
		}
		var selfPayload []byte
		if !input.DisableSelfViewDisclosure {
			if input.SelfViewDisclosureTargetPubKey == nil {
				return nil, fmt.Errorf("self-view target is required unless self-view is disabled")
			}
			selfRaw, err := privacycrypto.AsymEncrypt(fullPlain, *input.SelfViewDisclosureTargetPubKey)
			if err != nil {
				return nil, err
			}
			selfPayload, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeSelfViewDisclosureV1, selfRaw)
			if err != nil {
				return nil, err
			}
		}
		msgOutput := &privacytypes.BatchTransferOutput{Commitment: commitmentBytes, Ciphertext: ciphertext, ViewTag: tag, UserPrivacyPolicy: output.PrivacyPolicy, UserDisclosureMode: output.DisclosureMode, FullDisclosureDigest: fieldBytes(fullDigest), AuditDisclosurePayload: auditPayload, SelfViewDisclosurePayload: selfPayload}
		if output.PrivacyPolicy != 0 {
			msgOutput.UserDisclosureDigest = fieldBytes(userDigest)
			switch output.DisclosureMode {
			case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC:
				msgOutput.UserDisclosurePayload = userPlain
			case privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED:
				target, err := privacycrypto.DecodeCanonicalPoint(output.DisclosureTargetPubKey)
				if err != nil {
					return nil, err
				}
				userRaw, err := privacycrypto.AsymEncrypt(userPlain, *target)
				if err != nil {
					return nil, err
				}
				msgOutput.UserDisclosurePayload, err = privacytypes.WrapEncryptedEnvelopeV1(privacytypes.EnvelopeUserDisclosureV1, userRaw)
				if err != nil {
					return nil, err
				}
				msgOutput.UserDisclosureTargetPubkey = append([]byte(nil), output.DisclosureTargetPubKey...)
			default:
				return nil, fmt.Errorf("output %d disclosure mode is invalid", i)
			}
		}
		p.MessageOutputs[i] = msgOutput
	}
	nullifierValues := make([]*big.Int, 16)
	for i := range nullifierValues {
		nullifierValues[i] = new(big.Int)
	}
	for i := range p.Inputs {
		nullifierValues[i].SetBytes(p.Inputs[i].Nullifier)
	}
	var err error
	p.NullifierRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorNullifierV1, uint32(len(p.Inputs)), nullifierValues)
	if err != nil {
		return nil, err
	}
	p.CommitmentRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorCommitmentV1, uint32(len(p.Outputs)), commitments)
	if err != nil {
		return nil, err
	}
	p.UserDisclosureRoot, err = privacytypes.ComputeBatchUserDisclosureVectorRootV1(uint32(len(p.Outputs)), policies, userDigests)
	if err != nil {
		return nil, err
	}
	p.FullDisclosureRoot, err = privacytypes.ComputeBatchVectorRootV1(privacytypes.BatchVectorFullDisclosureV1, uint32(len(p.Outputs)), fullDigests)
	if err != nil {
		return nil, err
	}
	msg := p.effectMessage(nil, "")
	canonical, err := privacytypes.CanonicalMsgBatchTransferPayloadBytesV1(msg)
	if err != nil {
		return nil, err
	}
	digest, err := privacytypes.ComputeMsgBatchTransferPayloadDigestV1(msg)
	if err != nil {
		return nil, err
	}
	p.PayloadDigestHi, p.PayloadDigestLo = digest.Hi, digest.Lo
	chain, err := privacytypes.ComputeChainDomainV1(input.ChainID, privacytypes.ActiveCircuitSetID)
	if err != nil {
		return nil, err
	}
	p.ExpectedIntent, err = privacytypes.ComputeBatchTransferIntentV1(intentInput(p, chain.Hi, chain.Lo))
	if err != nil {
		return nil, err
	}
	req := signingRequest(p, canonical)
	if err := ValidateBatchTransferSigningRequest(req); err != nil {
		return nil, err
	}
	p.OwnerSignature, err = signer.SignBatchTransfer(req)
	if err != nil {
		return nil, err
	}
	if _, err := privacycrypto.DecodeCanonicalEdDSASignature(p.OwnerSignature); err != nil {
		return nil, fmt.Errorf("signer returned invalid owner signature: %w", err)
	}
	p.PayloadHash, err = computePayloadHash(p)
	if err != nil {
		return nil, err
	}
	return p, ValidatePreparedBatchTransferPayloadMetadataAt(p, time.Now())
}

func disclosurePlaintext(index uint32, full bool, owner privacytypes.Note, output PreparedBatchTransferOutput) ([]byte, *big.Int, error) {
	commitment := output.Note.ComputeCommitment()
	zero := func() *big.Int { return new(big.Int) }
	plain := &privacytypes.DisclosurePlaintextV1{Plane: privacytypes.DisclosurePlaneUserV1, OutputIndex: index, Policy: output.PrivacyPolicy, DisclosedFieldBitmap: output.PrivacyPolicy, Commitment: commitment, Amount: zero(), AssetID: zero(), SenderSpendKeyX: zero(), SenderSpendKeyY: zero(), SenderViewKeyX: zero(), SenderViewKeyY: zero(), RecipientSpendKeyX: zero(), RecipientSpendKeyY: zero(), RecipientViewKeyX: zero(), RecipientViewKeyY: zero(), DisclosureBlinding: output.UserDisclosureBlinding}
	if full {
		plain.Plane, plain.Policy, plain.DisclosedFieldBitmap = privacytypes.DisclosurePlaneFullV1, privacytypes.DisclosureFullMarkerV1, privacytypes.TransferPrivacyPolicyDiscloseAmountToFrom
		plain.Amount, plain.AssetID, plain.SenderSpendKeyX, plain.SenderSpendKeyY, plain.SenderViewKeyX, plain.SenderViewKeyY = output.Note.Amount, output.Note.AssetID, owner.ReceiverSpendPubKeyX, owner.ReceiverSpendPubKeyY, owner.ReceiverViewPubKeyX, owner.ReceiverViewPubKeyY
		plain.RecipientSpendKeyX, plain.RecipientSpendKeyY, plain.RecipientViewKeyX, plain.RecipientViewKeyY, plain.DisclosureBlinding = output.Note.ReceiverSpendPubKeyX, output.Note.ReceiverSpendPubKeyY, output.Note.ReceiverViewPubKeyX, output.Note.ReceiverViewPubKeyY, output.FullDisclosureBlinding
		d, err := privacytypes.ComputeBatchFullDisclosureDigestV1(privacytypes.BatchFullDisclosureV1Input{index, commitment, output.Note.Amount, output.Note.AssetID, owner.ReceiverSpendPubKeyX, owner.ReceiverSpendPubKeyY, owner.ReceiverViewPubKeyX, owner.ReceiverViewPubKeyY, output.Note.ReceiverSpendPubKeyX, output.Note.ReceiverSpendPubKeyY, output.Note.ReceiverViewPubKeyX, output.Note.ReceiverViewPubKeyY, output.FullDisclosureBlinding})
		if err != nil {
			return nil, nil, err
		}
		bz, err := privacytypes.MarshalDisclosurePlaintextV1(plain)
		return bz, d, err
	}
	if output.PrivacyPolicy == 0 {
		return nil, big.NewInt(0), nil
	}
	if output.PrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseAmount != 0 {
		plain.Amount.Set(output.Note.Amount)
	}
	if output.PrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseFrom != 0 {
		plain.SenderSpendKeyX.Set(owner.ReceiverSpendPubKeyX)
		plain.SenderSpendKeyY.Set(owner.ReceiverSpendPubKeyY)
		plain.SenderViewKeyX.Set(owner.ReceiverViewPubKeyX)
		plain.SenderViewKeyY.Set(owner.ReceiverViewPubKeyY)
	}
	if output.PrivacyPolicy&privacytypes.TransferPrivacyPolicyDiscloseTo != 0 {
		plain.RecipientSpendKeyX.Set(output.Note.ReceiverSpendPubKeyX)
		plain.RecipientSpendKeyY.Set(output.Note.ReceiverSpendPubKeyY)
		plain.RecipientViewKeyX.Set(output.Note.ReceiverViewPubKeyX)
		plain.RecipientViewKeyY.Set(output.Note.ReceiverViewPubKeyY)
	}
	plain.AssetID.Set(output.Note.AssetID)
	d, err := privacytypes.ComputeBatchUserDisclosureDigestV1(privacytypes.BatchUserDisclosureV1Input{index, commitment, output.PrivacyPolicy, output.PrivacyPolicy, plain.Amount, plain.SenderSpendKeyX, plain.SenderSpendKeyY, plain.SenderViewKeyX, plain.SenderViewKeyY, plain.RecipientSpendKeyX, plain.RecipientSpendKeyY, plain.RecipientViewKeyX, plain.RecipientViewKeyY, plain.AssetID, output.UserDisclosureBlinding})
	if err != nil {
		return nil, nil, err
	}
	bz, err := privacytypes.MarshalDisclosurePlaintextV1(plain)
	return bz, d, err
}

func fieldBytes(v *big.Int) []byte { b, _ := privacyfield.CanonicalBytesFromBigInt(v); return b }

func (p *PreparedBatchTransferPayload) effectMessage(proof []byte, creator string) *privacytypes.MsgBatchTransfer {
	nullifiers := make([][]byte, len(p.Inputs))
	for i := range p.Inputs {
		nullifiers[i] = append([]byte(nil), p.Inputs[i].Nullifier...)
	}
	return &privacytypes.MsgBatchTransfer{Creator: creator, Proof: proof, Root: append([]byte(nil), p.Root...), Nullifiers: nullifiers, Outputs: p.MessageOutputs, AuditKeyId: p.AuditKeyID, AuditKeyEpoch: p.AuditKeyEpoch, AuditDisclosureTargetPubkey: append([]byte(nil), p.AuditDisclosureTargetPubKey...), ExpiresAtUnix: p.ExpiresAtUnix}
}

func computePayloadHash(p *PreparedBatchTransferPayload) (string, error) {
	clone := *p
	clone.PayloadHash = ""
	clone.Creator = ""
	bz, err := json.Marshal(&clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte("clairveil.prepared-batch-transfer.v1"), bz...))
	return hex.EncodeToString(sum[:]), nil
}

func WritePreparedBatchTransferPayload(path string, payload *PreparedBatchTransferPayload) error {
	bz, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return writePrivateBatchFile(path, bz)
}

func writePrivateBatchFile(path string, bz []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(bz); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func intentInput(p *PreparedBatchTransferPayload, hi, lo *big.Int) privacytypes.BatchTransferIntentV1Input {
	return privacytypes.BatchTransferIntentV1Input{ChainDomainHi: hi, ChainDomainLo: lo, MerkleRoot: new(big.Int).SetBytes(p.Root), InputCount: uint32(len(p.Inputs)), OutputCount: uint32(len(p.Outputs)), AssetID: p.AssetID, NullifierRoot: p.NullifierRoot, CommitmentRoot: p.CommitmentRoot, UserDisclosureRoot: p.UserDisclosureRoot, FullDisclosureRoot: p.FullDisclosureRoot, PayloadDigestHi: p.PayloadDigestHi, PayloadDigestLo: p.PayloadDigestLo, ExpiresAtUnix: p.ExpiresAtUnix}
}

func signingRequest(p *PreparedBatchTransferPayload, canonical []byte) BatchTransferSigningRequest {
	outs := make([]BatchTransferSigningOutput, len(p.Outputs))
	for i, o := range p.Outputs {
		s, _ := pointFromCoordinates(o.Note.ReceiverSpendPubKeyX, o.Note.ReceiverSpendPubKeyY)
		v, _ := pointFromCoordinates(o.Note.ReceiverViewPubKeyX, o.Note.ReceiverViewPubKeyY)
		sb, vb := s.Bytes(), v.Bytes()
		outs[i] = BatchTransferSigningOutput{Kind: o.Kind, Commitment: append([]byte(nil), p.MessageOutputs[i].Commitment...), RecipientSpendPubKey: append([]byte(nil), sb[:]...), RecipientViewPubKey: append([]byte(nil), vb[:]...), Amount: new(big.Int).Set(o.Note.Amount), AssetID: new(big.Int).Set(o.Note.AssetID), Randomness: new(big.Int).Set(o.Note.Randomness), PrivacyPolicy: o.PrivacyPolicy, DisclosureMode: o.DisclosureMode, UserDisclosureBlinding: new(big.Int).Set(o.UserDisclosureBlinding), FullDisclosureBlinding: new(big.Int).Set(o.FullDisclosureBlinding), WireOutput: p.MessageOutputs[i]}
	}
	ns := make([][]byte, len(p.Inputs))
	ins := make([]BatchTransferSigningInput, len(p.Inputs))
	inputTotal := new(big.Int)
	for i := range p.Inputs {
		ns[i] = append([]byte(nil), p.Inputs[i].Nullifier...)
		spend, _ := pointFromCoordinates(p.Inputs[i].Note.ReceiverSpendPubKeyX, p.Inputs[i].Note.ReceiverSpendPubKeyY)
		view, _ := pointFromCoordinates(p.Inputs[i].Note.ReceiverViewPubKeyX, p.Inputs[i].Note.ReceiverViewPubKeyY)
		spendBytes, viewBytes := spend.Bytes(), view.Bytes()
		ins[i] = BatchTransferSigningInput{
			Commitment:  append([]byte(nil), fieldBytes(p.Inputs[i].Note.ComputeCommitment())...),
			Nullifier:   append([]byte(nil), p.Inputs[i].Nullifier...),
			SpendPubKey: append([]byte(nil), spendBytes[:]...), ViewPubKey: append([]byte(nil), viewBytes[:]...),
			Amount: new(big.Int).Set(p.Inputs[i].Note.Amount), AssetID: new(big.Int).Set(p.Inputs[i].Note.AssetID), Randomness: new(big.Int).Set(p.Inputs[i].Note.Randomness),
		}
		inputTotal.Add(inputTotal, p.Inputs[i].Note.Amount)
	}
	ownerSpend, _ := pointFromCoordinates(p.Inputs[0].Note.ReceiverSpendPubKeyX, p.Inputs[0].Note.ReceiverSpendPubKeyY)
	ownerView, _ := pointFromCoordinates(p.Inputs[0].Note.ReceiverViewPubKeyX, p.Inputs[0].Note.ReceiverViewPubKeyY)
	ownerSpendBytes, ownerViewBytes := ownerSpend.Bytes(), ownerView.Bytes()
	selfViewEnabled := len(p.MessageOutputs) > 0 && len(p.MessageOutputs[0].SelfViewDisclosurePayload) > 0
	return BatchTransferSigningRequest{Version: p.Version, CircuitSetID: p.CircuitSetID, ChainID: p.ChainID, ExpiresAtUnix: p.ExpiresAtUnix, OrderedInputs: ins, OrderedInputNullifiers: ns, OrderedOutputs: outs, OwnerSpendPubKey: append([]byte(nil), ownerSpendBytes[:]...), OwnerViewPubKey: append([]byte(nil), ownerViewBytes[:]...), Root: append([]byte(nil), p.Root...), AssetID: new(big.Int).Set(p.AssetID), InputTotal: inputTotal, AuditKeyID: p.AuditKeyID, AuditKeyEpoch: p.AuditKeyEpoch, AuditDisclosureTargetPubKey: append([]byte(nil), p.AuditDisclosureTargetPubKey...), SelfViewEnabled: selfViewEnabled, NullifierRoot: new(big.Int).Set(p.NullifierRoot), CommitmentRoot: new(big.Int).Set(p.CommitmentRoot), UserDisclosureRoot: new(big.Int).Set(p.UserDisclosureRoot), FullDisclosureRoot: new(big.Int).Set(p.FullDisclosureRoot), CanonicalPayload: append([]byte(nil), canonical...), PayloadDigestHi: new(big.Int).Set(p.PayloadDigestHi), PayloadDigestLo: new(big.Int).Set(p.PayloadDigestLo), ExpectedIntent: new(big.Int).Set(p.ExpectedIntent), CanonicalEffect: p.effectMessage(nil, "")}
}
