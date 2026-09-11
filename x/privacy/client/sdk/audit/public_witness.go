package audit

import (
	"bytes"
	"fmt"
	"math/big"

	privacyfield "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/field"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// auditPublicWitness is intentionally constructed from the immutable PI23
// decoded from the authenticated record, never from a node-supplied witness.
func auditPublicWitness(pi [23]auditfield.Field32) (witness.Witness, error) {
	w, err := witness.New(ecc.BN254.ScalarField())
	if err != nil {
		return nil, err
	}
	values := make(chan any, len(pi))
	for _, field := range pi {
		values <- new(big.Int).SetBytes(field[:])
	}
	close(values)
	if err := w.Fill(len(pi), 0, values); err != nil {
		return nil, err
	}
	return w, nil
}

// VerifyFinalProof verifies canonically framed proof bytes against a stored
// PI23 and the registry's exact circuit identity. Artifact-hash freshness is
// a snapshot/message concern and must be checked by the caller before this
// function: the registry only authenticates its local circuit artifacts.
func VerifyFinalProof(kind auditfield.Kind, rawPI [][]byte, proof []byte, registry *privacyzk.ArtifactRegistry, identity *privacytypes.CircuitSetIdentity) error {
	if registry == nil || identity == nil || len(rawPI) != 23 {
		return fmt.Errorf("final audit proof verification requires PI23, registry and identity")
	}
	if err := privacyzk.ValidateCanonicalProofBN254(proof); err != nil {
		return err
	}
	var pi [23]auditfield.Field32
	for i := range rawPI {
		field, err := auditfield.ParseField32(rawPI[i])
		if err != nil {
			return fmt.Errorf("invalid final audit public input %d", i)
		}
		pi[i] = field
	}
	ids := privacyzk.AuditFieldCircuitIDs()
	if kind < 1 || int(kind) > len(ids) {
		return fmt.Errorf("unsupported audit kind")
	}
	public, err := auditPublicWitness(pi)
	if err != nil {
		return err
	}
	return registry.VerifyProof(ids[int(kind)-1], proof, public, identity)
}

// VerifyFinalMessageBinding reconstructs PI23 from the final message and its
// authenticated snapshot, so a proof for one final message cannot be relayed
// with another message.
func VerifyFinalMessageBinding(snapshot Snapshot, raw sdk.Msg, rawPI [][]byte) error {
	if snapshot.Validate() != nil || len(rawPI) != 23 {
		return fmt.Errorf("final message binding requires snapshot and PI23")
	}
	m, err := privacytypes.ValidateAuditMessage(raw)
	if err != nil {
		return err
	}
	keyID := snapshot.Key.IDBytes()
	messageKeyID := m.KeyID()
	if m.Epoch() != snapshot.Epoch || !bytes.Equal(messageKeyID[:], keyID) {
		return fmt.Errorf("final message audit key does not match snapshot")
	}
	pi := make([]auditfield.Field32, len(rawPI))
	for i := range rawPI {
		if pi[i], err = auditfield.ParseField32(rawPI[i]); err != nil {
			return fmt.Errorf("invalid final audit public input %d", i)
		}
	}
	inputs := m.Nullifiers()
	inputBytes := make([][]byte, len(inputs))
	for i := range inputs {
		inputBytes[i] = inputs[i].Bytes()
	}
	var root []byte
	if len(inputs) > 0 {
		root = m.Root().Bytes()
	}
	in := PrepareInput{Snapshot: snapshot, Kind: m.Kind(), Creator: m.Creator(), Recipient: m.Recipient(), ExpiresAtUnix: m.Expiry(), Root: root, Inputs: inputBytes, Outputs: m.Outputs()}
	if m.Kind() == auditfield.KindDeposit || m.Kind() == auditfield.KindWithdraw {
		coin := m.Coin()
		asset, fieldErr := privacyfield.CanonicalBytesFromBigInt(privacytypes.ComputeAssetIDV1(coin.Denom))
		if fieldErr != nil {
			return fieldErr
		}
		in.Amount, in.PublicAsset = coin.String(), asset
		principal := m.Creator()
		if m.Kind() == auditfield.KindWithdraw {
			principal = m.Recipient()
		}
		address, fieldErr := sdk.AccAddressFromBech32(principal)
		if fieldErr != nil {
			return fieldErr
		}
		in.Principal = address.Bytes()
	}
	aux, auxBytes, err := privacytypes.AuditOutputsToAux(m.Kind(), in.Outputs)
	if err != nil {
		return err
	}
	commits := make([]auditfield.Field32, len(in.Outputs))
	for i := range in.Outputs {
		if commits[i], err = auditfield.ParseField32(in.Outputs[i].Commitment); err != nil {
			return err
		}
	}
	rootField := auditfield.Field32{}
	if len(inputs) > 0 {
		rootField = m.Root()
	}
	want, err := buildPublicInputs(in, inputs, commits, aux, auxBytes, rootField)
	if err != nil {
		return err
	}
	context, err := auditfield.NewAuditContext(m.Kind(), want[:21])
	if err != nil {
		return err
	}
	envelope, err := auditfield.ParseEnvelopeFrame(m.Kind(), uint8(len(inputs)), uint8(len(commits)), m.Envelope())
	if err != nil {
		return err
	}
	cipherRoot, err := auditfield.ComputeCipherRoot(context, envelope)
	if err != nil {
		return err
	}
	want[21], want[22] = cipherRoot.Left, cipherRoot.Right
	for i := range want {
		if !bytes.Equal(want[i][:], pi[i][:]) {
			return fmt.Errorf("final message does not match public input %d", i)
		}
	}
	return nil
}
