package cli

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"

	privacybatchtransfer "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/batchtransfer"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func TestBatchTransferCommandsAreRegisteredSeparatelyFromEnvelopeBatch(t *testing.T) {
	cmd := GetTxCmd()
	for _, name := range []string{
		"transfer-batch",
		"transfer-batch-16x32",
		"prepare-batch-transfer",
		"prove-batch-transfer",
		"broadcast-batch-transfer",
	} {
		found, _, err := cmd.Find([]string{name})
		require.NoError(t, err)
		require.Equal(t, name, found.Name())
	}
	require.Contains(t, CmdTransferBatch().Long, "independent MsgTransfer")
	require.Contains(t, CmdTransferBatch16x32().Long, "one-proof")
}

func TestParseBatchTransferPaymentsSupportsIndependentDisclosure(t *testing.T) {
	address := testBatchShieldedAddress(t, 3, 7)
	disclosure := testBatchPoint(t, 11).Bytes()
	disclosureHex := hex.EncodeToString(disclosure[:])

	payments, denom, total, err := parseBatchTransferPayments([]string{
		address + ",2uclair",
		address + ",3uclair,amount,public",
		address + ",4uclair,to,recipient-encrypted," + disclosureHex,
	})
	require.NoError(t, err)
	require.Equal(t, "uclair", denom)
	require.Equal(t, int64(9), total.Int64())
	require.Len(t, payments, 3)
	require.Equal(t, privacytypes.TransferPrivacyPolicyAllPrivate, payments[0].PrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_NONE, payments[0].DisclosureMode)
	require.Nil(t, payments[0].DisclosureTargetPubKey)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseAmount, payments[1].PrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_PUBLIC, payments[1].DisclosureMode)
	require.Nil(t, payments[1].DisclosureTargetPubKey)
	require.Equal(t, privacytypes.TransferPrivacyPolicyDiscloseTo, payments[2].PrivacyPolicy)
	require.Equal(t, privacytypes.UserDisclosureMode_USER_DISCLOSURE_MODE_RECIPIENT_ENCRYPTED, payments[2].DisclosureMode)
	require.Equal(t, disclosure, payments[2].DisclosureTargetPubKey.Bytes())
}

func TestParseBatchTransferPaymentsRejectsUnsafeOrAmbiguousDisclosure(t *testing.T) {
	address := testBatchShieldedAddress(t, 3, 7)

	_, _, _, err := parseBatchTransferPayments([]string{address + ",2uclair,all-private,public"})
	require.ErrorContains(t, err, "all-private policy requires none")

	_, _, _, err = parseBatchTransferPayments([]string{address + ",2uclair,amount,none"})
	require.ErrorContains(t, err, "requires public or recipient-encrypted")

	_, _, _, err = parseBatchTransferPayments([]string{address + ",2uclair,amount,recipient-encrypted"})
	require.ErrorContains(t, err, "disclosure key")

	_, _, _, err = parseBatchTransferPayments([]string{address + ",2uclair", address + ",2uatom"})
	require.ErrorContains(t, err, "all batch payments must use denom")
}

func TestSelectBatchTransferInputsAutomaticAndExplicit(t *testing.T) {
	found := []FoundNote{
		batchFoundNote("small", 2, "uclair", false),
		batchFoundNote("spent", 100, "uclair", true),
		batchFoundNote("other", 100, "uatom", false),
		batchFoundNote("large", 9, "uclair", false),
		batchFoundNote("medium", 5, "uclair", false),
	}

	automatic, err := selectBatchTransferInputs(found, "uclair", big.NewInt(12), nil)
	require.NoError(t, err)
	require.Len(t, automatic, 2)
	require.Equal(t, int64(9), automatic[0].Note.Amount.Int64())
	require.Equal(t, int64(5), automatic[1].Note.Amount.Int64())

	explicit, err := selectBatchTransferInputs(found, "uclair", big.NewInt(7), []int{1, 5})
	require.NoError(t, err)
	require.Len(t, explicit, 2)
	require.Equal(t, int64(2), explicit[0].Note.Amount.Int64())
	require.Equal(t, int64(5), explicit[1].Note.Amount.Int64())

	_, err = selectBatchTransferInputs(found, "uclair", big.NewInt(1), []int{2})
	require.ErrorContains(t, err, "spent or does not use denom")
	_, err = selectBatchTransferInputs(found, "uclair", big.NewInt(1), []int{1, 1})
	require.ErrorContains(t, err, "duplicate")
}

func TestSelectBatchTransferInputsRequiresPreparationBeyondSixteen(t *testing.T) {
	found := make([]FoundNote, 17)
	for i := range found {
		found[i] = batchFoundNote(fmt.Sprintf("note-%02d", i), 1, "uclair", false)
	}

	_, err := selectBatchTransferInputs(found, "uclair", big.NewInt(17), nil)
	require.ErrorIs(t, err, privacybatchtransfer.ErrPreparationRequired)
}

func TestBatchTransferCommandFlagsExposeRestartableStages(t *testing.T) {
	all := CmdTransferBatch16x32()
	for _, name := range []string{flagBatchPayment, flagBatchInputIndex, flagBatchOutputMode, flagBatchPreparedOut, flagBatchProofOut, flagBatchProverURL, flagBatchProverTimeout} {
		require.NotNil(t, all.Flags().Lookup(name), name)
	}
	require.Contains(t, all.Flags().Lookup(flagBatchProverURL).Usage, "Exclusive")

	prove := CmdProveBatchTransfer()
	require.NotNil(t, prove.Flags().Lookup(flagBatchProofOut))
	require.NotNil(t, prove.Flags().Lookup(flagBatchProverURL))
	require.Nil(t, prove.Flags().Lookup(flagBatchPayment))
}

func TestProveBatchTransferCommandUsesExclusiveRemoteRouteAndWritesBoundProof(t *testing.T) {
	payload := testCLIBatchPayload(t)
	dir := t.TempDir()
	preparedPath := filepath.Join(dir, "prepared.json")
	proofPath := filepath.Join(dir, "proof.json")
	require.NoError(t, privacybatchtransfer.WritePreparedBatchTransferPayload(preparedPath, payload))

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, privacyprovertransport.BatchTransferProofPath, r.URL.Path)
		require.Equal(t, http.MethodPost, r.Method)
		var request privacyprovertransport.BatchTransferProofRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, payload.PayloadHash, request.Payload.PayloadHash)
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(privacyprovertransport.BatchTransferProofResponse{
			Version: privacyprovertransport.BatchTransferProofResponseVersion,
			Proof: privacybatchtransfer.PreparedBatchTransferProof{
				Version:            privacybatchtransfer.PreparedBatchTransferProofVersion,
				RequestPayloadHash: payload.PayloadHash,
				CircuitSetID:       payload.CircuitSetID,
				Proof:              make([]byte, privacytypes.BatchTransferProofSizeV1),
			},
		}))
	}))
	t.Cleanup(server.Close)

	cmd := CmdProveBatchTransfer()
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	cmd.SetArgs([]string{preparedPath, "--" + flagBatchProofOut, proofPath, "--" + flagBatchProverURL, server.URL})
	require.NoError(t, cmd.Execute())
	require.Equal(t, 1, requests)

	proof, err := privacybatchtransfer.ReadPreparedBatchTransferProof(proofPath)
	require.NoError(t, err)
	require.Equal(t, payload.PayloadHash, proof.RequestPayloadHash)
	require.NoError(t, privacybatchtransfer.ValidatePreparedBatchTransferProofAt(payload, proof, time.Now()))
}

func testBatchShieldedAddress(t *testing.T, spendScalar, viewScalar int64) string {
	address, err := privacytypes.EncodeShieldedAddressWithView(testBatchPoint(t, spendScalar), testBatchPoint(t, viewScalar))
	require.NoError(t, err)
	return address
}

func testBatchPoint(t *testing.T, scalar int64) *crypto_tedwards.PointAffine {
	t.Helper()
	curve := crypto_tedwards.GetEdwardsCurve()
	var point crypto_tedwards.PointAffine
	point.ScalarMultiplication(&curve.Base, big.NewInt(scalar))
	return &point
}

func batchFoundNote(nullifier string, amount int64, denom string, spent bool) FoundNote {
	return FoundNote{
		Note: privacytypes.Note{
			Amount:  big.NewInt(amount),
			AssetID: privacytypes.ComputeAssetIDV1(denom),
		},
		AssetDenom: denom,
		Nullifier:  nullifier,
		IsSpent:    spent,
	}
}

func testCLIBatchPayload(t *testing.T) *privacybatchtransfer.PreparedBatchTransferPayload {
	t.Helper()
	ownerScalar := big.NewInt(17)
	owner := testBatchPoint(t, ownerScalar.Int64())
	view := testBatchPoint(t, 19)
	note := testCLIBatchNote(owner, view, 7, 23)
	recipient := testBatchPoint(t, 29)
	plan, err := privacybatchtransfer.PlanBatchTransfer(privacybatchtransfer.PlanBatchTransferInput{
		Inputs:           []privacybatchtransfer.InputNote{{Note: note}},
		Payments:         []privacybatchtransfer.Payment{{SpendPubKey: recipient, ViewPubKey: recipient, Amount: big.NewInt(7)}},
		OwnerSpendPubKey: owner,
		OwnerViewPubKey:  view,
		Mode:             privacybatchtransfer.OutputModeCompact,
	})
	require.NoError(t, err)
	prepared, err := privacybatchtransfer.PrepareBatchTransfer(context.Background(), cliBatchPathProvider{}, plan)
	require.NoError(t, err)
	payload, err := privacybatchtransfer.BuildPreparedBatchTransferPayload(prepared, structuredBatchTransferSigner{scalar: ownerScalar, pubKey: owner}, privacybatchtransfer.BuildPreparedBatchTransferPayloadInput{
		ChainID:                     "clairveil-cli-test-1",
		ExpiresAtUnix:               time.Now().Add(time.Hour).Unix(),
		AuditKeyID:                  "audit-default",
		AuditKeyEpoch:               1,
		AuditDisclosureTargetPubKey: testBatchPoint(t, 31),
		DisableSelfViewDisclosure:   true,
	})
	require.NoError(t, err)
	return payload
}

func testCLIBatchNote(spend, view *crypto_tedwards.PointAffine, amount, randomness int64) privacytypes.Note {
	sx, sy := new(big.Int), new(big.Int)
	vx, vy := new(big.Int), new(big.Int)
	spend.X.BigInt(sx)
	spend.Y.BigInt(sy)
	view.X.BigInt(vx)
	view.Y.BigInt(vy)
	return privacytypes.Note{
		ReceiverSpendPubKeyX: sx,
		ReceiverSpendPubKeyY: sy,
		ReceiverViewPubKeyX:  vx,
		ReceiverViewPubKeyY:  vy,
		Amount:               big.NewInt(amount),
		AssetID:              privacytypes.ComputeAssetIDV1("uclair"),
		Randomness:           big.NewInt(randomness),
	}
}

type cliBatchPathProvider struct{}

func (cliBatchPathProvider) LookupMerklePath(_ context.Context, commitmentHex string) (*privacybatchtransfer.MerklePathResult, error) {
	commitment, ok := new(big.Int).SetString(commitmentHex, 16)
	if !ok {
		return nil, fmt.Errorf("invalid commitment")
	}
	empty := privacytypes.EmptyNoteTreeRootsV1(32)
	current := new(big.Int).Set(commitment)
	path := make([]string, 32)
	helper := make([]uint32, 32)
	for i := range path {
		path[i] = hex.EncodeToString(empty[i].FillBytes(make([]byte, 32)))
		current = privacytypes.ComputeNoteTreeNodeV1(uint32(i), current, empty[i])
	}
	return &privacybatchtransfer.MerklePathResult{Root: current.FillBytes(make([]byte, 32)), Path: path, PathHelper: helper}, nil
}
