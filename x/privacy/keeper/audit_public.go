package keeper

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/big"

	privacycrypto "github.com/DELIGHT-LABS/clairveil/x/privacy/crypto"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/witness"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
)

// buildAuditPublic derives the complete assignment; no PI or hash supplied by
// the transaction bypasses actual message, registry, asset or address checks.
func (k Keeper) buildAuditPublic(ctx sdk.Context, m types.ValidatedAuditMessage) (*auditTransitionRecord, error) {
	if k.audit == nil {
		return nil, fmt.Errorf("audit runtime is not configured")
	}
	if _, err := auditCircuitID(m.Kind()); err != nil {
		return nil, err
	}
	halted, err := k.auditHalted(ctx)
	if err != nil {
		return nil, err
	}
	if halted {
		return nil, fmt.Errorf("privacy is halted")
	}
	origin, err := k.auditExecutionOrigin(ctx)
	if err != nil {
		return nil, err
	}
	if ctx.BlockTime().Unix() < 0 || ctx.BlockTime().Unix() >= m.Expiry() {
		return nil, fmt.Errorf("audit message is expired")
	}
	key, err := k.activeAuditKey(ctx)
	if err != nil {
		return nil, err
	}
	if key.Epoch != m.Epoch() || key.KeyID != m.KeyID() {
		return nil, fmt.Errorf("audit authorization is not the active key")
	}
	network, err := k.auditNetwork(ctx)
	if err != nil {
		return nil, err
	}
	record := &auditTransitionRecord{Height: uint64(ctx.BlockHeight()), Origin: origin, Kind: m.Kind(), Network: network, SetID: zk.AuditFieldCircuitSetID, Proof: m.Proof(), KeyID: key.KeyID, Epoch: key.Epoch, Suite: key.Suite, PK: append([]byte(nil), key.PK...), Envelope: m.Envelope(), Root: m.Root(), Inputs: m.Nullifiers()}
	id, err := auditCircuitID(m.Kind())
	if err != nil {
		return nil, err
	}
	for _, identity := range k.audit.identity.Circuits {
		if identity.CircuitId == string(id) {
			v, err := hex.DecodeString(identity.VerifyingKeySha256)
			if err != nil {
				return nil, err
			}
			copy(record.VKHash[:], v)
			v, err = hex.DecodeString(identity.PublicInputSchemaSha256)
			if err != nil {
				return nil, err
			}
			copy(record.SchemaHash[:], v)
		}
	}
	pi := &record.PI
	pi[0], pi[1] = auditfield.DigestFields(network)
	pi[2], pi[3] = auditfield.DigestFields(key.KeyID)
	pi[4] = auditfield.Field32FromUint64(key.Epoch)
	auditKey, err := auditfield.ParseAuditKey(key.PK, key.KeyID[:])
	if err != nil {
		return nil, err
	}
	pi[5], pi[6], err = auditKey.Point().Coordinates()
	if err != nil {
		return nil, err
	}
	pi[7] = auditfield.Field32FromUint64(uint64(m.Expiry()))
	pi[8] = m.Root()
	pi[9] = auditfield.Field32FromUint64(uint64(len(record.Inputs)))
	outputs := m.Outputs()
	pi[10] = auditfield.Field32FromUint64(uint64(len(outputs)))
	if len(record.Inputs) > 0 {
		found, err := k.storeService.OpenKVStore(ctx).Has(types.GetHistoricalRootKey(record.Root[:]))
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("audit historical root is not allowed")
		}
	}
	for _, n := range record.Inputs {
		used, err := k.storeService.OpenKVStore(ctx).Has(types.GetNullifierKey(n[:]))
		if err != nil {
			return nil, err
		}
		if used {
			return nil, fmt.Errorf("audit nullifier already spent")
		}
	}
	if err := k.EnsureCanAppendCommitments(ctx, uint64(len(outputs))); err != nil {
		return nil, err
	}
	commitments := make([]auditfield.Field32, len(outputs))
	users := make([]auditfield.Field32, len(outputs))
	fulls := make([]auditfield.Field32, len(outputs))
	for i, o := range outputs {
		commitments[i], err = auditfield.ParseField32(o.Commitment)
		if err != nil {
			return nil, err
		}
		found, err := k.HasCommitment(ctx, o.Commitment)
		if err != nil {
			return nil, err
		}
		if found {
			return nil, fmt.Errorf("audit commitment already exists")
		}
		if len(o.UserDisclosureDigest) > 0 {
			users[i], err = auditfield.ParseField32(o.UserDisclosureDigest)
			if err != nil {
				return nil, err
			}
		}
		if len(o.SelfFullDisclosureDigest) > 0 {
			fulls[i], err = auditfield.ParseField32(o.SelfFullDisclosureDigest)
			if err != nil {
				return nil, err
			}
		}
		record.Outputs = append(record.Outputs, auditOutputRef{Commitment: commitments[i]})
	}
	pi[11], err = auditVectorRoot(m.Kind(), types.BatchVectorNullifierV1, record.Inputs)
	if err != nil {
		return nil, err
	}
	pi[12], err = auditVectorRoot(m.Kind(), types.BatchVectorCommitmentV1, commitments)
	if err != nil {
		return nil, err
	}
	if m.Kind() == auditfield.KindTransfer2x2 {
		value := privacycrypto.MimcHash(types.DomainFieldV1("clairveil.audit.user-2x2.v1"), new(big.Int).SetUint64(uint64(outputs[0].UserPrivacyPolicy)), new(big.Int).SetBytes(users[0][:]))
		pi[13], err = auditfield.ParseField32(value.FillBytes(make([]byte, 32)))
		if err != nil {
			return nil, err
		}
		pi[14] = fulls[0]
	}
	if m.Kind() == auditfield.KindBatch16x32 {
		policies := make([]uint32, 32)
		digests := zeroBatchFieldVector(32)
		for i, o := range outputs {
			policies[i] = o.UserPrivacyPolicy
			digests[i].SetBytes(users[i][:])
		}
		userRoot, err := types.ComputeBatchUserDisclosureVectorRootV1(uint32(len(outputs)), policies, digests)
		if err != nil {
			return nil, err
		}
		pi[13], err = auditfield.ParseField32(userRoot.FillBytes(make([]byte, 32)))
		if err != nil {
			return nil, err
		}
		pi[14], err = auditVectorRoot(m.Kind(), types.BatchVectorFullDisclosureV1, fulls)
		if err != nil {
			return nil, err
		}
	}
	if m.Kind() == 1 || m.Kind() == 2 {
		coin := m.Coin()
		asset, err := k.RequireRegisteredAssetV1(ctx, coin.Denom)
		if err != nil {
			return nil, err
		}
		coin.Amount.BigInt().FillBytes(pi[15][:])
		pi[16], err = auditfield.ParseField32(asset)
		if err != nil {
			return nil, err
		}
		target := m.Creator()
		if m.Kind() == 2 {
			target = m.Recipient()
		}
		address, err := sdk.AccAddressFromBech32(target)
		if err != nil {
			return nil, err
		}
		module := authtypes.NewModuleAddress(types.ModuleName)
		if address.Equals(module) {
			return nil, fmt.Errorf("privacy module cannot be the external principal endpoint")
		}
		digest, err := auditfield.PublicTargetDigest(m.Kind(), address)
		if err != nil {
			return nil, err
		}
		pi[17], pi[18] = auditfield.DigestFields(digest)
		effect := &auditTransparentEffect{Kind: m.Kind(), From: append([]byte(nil), address...), To: append([]byte(nil), module...), Denom: coin.Denom, Amount: pi[15]}
		if m.Kind() == 2 {
			effect.From, effect.To = effect.To, effect.From
		}
		record.Transparent = effect
	}
	record.AuxHash = sha256.Sum256(append([]byte(auditfield.AuditAuxDomain), m.Aux()...))
	pi[19], pi[20] = auditfield.DigestFields(record.AuxHash)
	context, err := auditfield.NewAuditContext(m.Kind(), pi[:21])
	if err != nil {
		return nil, err
	}
	envelope, err := auditfield.ParseEnvelopeFrame(m.Kind(), uint8(len(record.Inputs)), uint8(len(record.Outputs)), record.Envelope)
	if err != nil {
		return nil, err
	}
	record.CipherRoot, err = auditfield.ComputeCipherRoot(context, envelope)
	if err != nil {
		return nil, err
	}
	pi[21], pi[22] = record.CipherRoot.Left, record.CipherRoot.Right
	record.ContextHash, err = context.ContextHash(envelope.Nonce())
	if err != nil {
		return nil, err
	}
	return record, nil
}
func auditPublicWitness(pi [23]auditfield.Field32) (witness.Witness, error) {
	w, err := witness.New(ecc.BN254.ScalarField())
	if err != nil {
		return nil, err
	}
	values := make(chan any, 23)
	for _, p := range pi {
		values <- new(big.Int).SetBytes(p[:])
	}
	close(values)
	if err := w.Fill(23, 0, values); err != nil {
		return nil, err
	}
	return w, nil
}
