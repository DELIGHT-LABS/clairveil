package proverservice

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/consensys/gnark-crypto/ecc"
	crypto_tedwards "github.com/consensys/gnark-crypto/ecc/bn254/twistededwards"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
	"github.com/stretchr/testify/require"

	privacycircuit "github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacydeposit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/deposit"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

type integrationDepositArtifacts struct {
	r1cs constraint.ConstraintSystem
	pk   groth16.ProvingKey
}

func (p integrationDepositArtifacts) DepositR1CS() (constraint.ConstraintSystem, error) {
	return p.r1cs, nil
}

func (p integrationDepositArtifacts) DepositProvingKey() (groth16.ProvingKey, error) {
	return p.pk, nil
}

type integrationDepositRunner struct{}

type integrationDepositResponseProver struct {
	response *privacyprovertransport.DepositProofResponse
}

func (p integrationDepositResponseProver) ProveDeposit(privacyprovertransport.DepositProofRequest) (*privacyprovertransport.DepositProofResponse, error) {
	return p.response, nil
}

func (integrationDepositRunner) ProveDeposit(r1cs constraint.ConstraintSystem, pk groth16.ProvingKey, depositWitness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, pk, depositWitness)
}

func TestDepositProofHTTPRoundTripWithRealGroth16Verification(t *testing.T) {
	note := integrationDepositNote()
	require.NoError(t, note.ValidateV1())
	payload, err := privacydeposit.BuildPreparedDepositProverPayload(note)
	require.NoError(t, err)
	request, err := privacyprovertransport.NewDepositProofRequest(*payload)
	require.NoError(t, err)

	compiled, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &privacycircuit.DepositCircuit{})
	require.NoError(t, err)
	provingKey, verifyingKey, err := groth16.Setup(compiled)
	require.NoError(t, err)

	admission, err := NewAdmissionController(DefaultAdmissionConfig())
	require.NoError(t, err)
	handler := NewHandlerWithProverSet(
		privacyprovertransport.ProverSet{
			Deposit: privacyprovertransport.ReferenceDepositProver{
				Artifacts: integrationDepositArtifacts{r1cs: compiled, pk: provingKey},
				Runner:    integrationDepositRunner{},
			},
		},
		nil,
		nil,
		DefaultRuntimeInfo(),
		"",
		DefaultMaxRequestBz,
		admission,
	)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := privacyprovertransport.HTTPProverClient{BaseURL: server.URL, Client: server.Client()}
	response, err := client.ProveDeposit(context.Background(), *request)
	require.NoError(t, err)
	require.NoError(t, privacyprovertransport.ValidateDepositProofResponse(*request, *response))

	proofBytes, err := hex.DecodeString(response.Proof.ProofHex)
	require.NoError(t, err)
	proof, err := privacyzk.ReadCanonicalProofBN254(proofBytes)
	require.NoError(t, err)
	assignment, err := privacydeposit.BuildDepositAssignment(note)
	require.NoError(t, err)
	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	require.NoError(t, err)
	require.NoError(t, groth16.Verify(proof, verifyingKey, publicWitness))

	snapshot := admission.Snapshot()
	depositMetrics := snapshot.Circuits[privacyprovertransport.DepositProofCircuitID]
	require.Equal(t, uint64(1), depositMetrics.TotalAdmitted)
	require.Equal(t, uint64(1), depositMetrics.TotalProveCompleted)

	requestBytes, err := json.Marshal(request)
	require.NoError(t, err)
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err = zw.Write(requestBytes)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	gzipHandler := NewHandlerWithProverSet(
		privacyprovertransport.ProverSet{Deposit: integrationDepositResponseProver{response: response}},
		nil,
		nil,
		DefaultRuntimeInfo(),
		"",
		DefaultMaxRequestBz,
		mustDefaultAdmissionController(),
	)
	recorder := httptest.NewRecorder()
	gzipRequest := httptest.NewRequest(http.MethodPost, privacyprovertransport.DepositProofPath, bytes.NewReader(compressed.Bytes()))
	gzipRequest.Header.Set("Content-Type", "application/json")
	gzipRequest.Header.Set("Content-Encoding", "gzip")
	gzipHandler.ServeHTTP(recorder, gzipRequest)
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, "no-store", recorder.Header().Get("Cache-Control"))
}

func integrationDepositNote() privacytypes.Note {
	curve := crypto_tedwards.GetEdwardsCurve()
	var spendKey, viewKey crypto_tedwards.PointAffine
	spendKey.ScalarMultiplication(&curve.Base, big.NewInt(101))
	viewKey.ScalarMultiplication(&curve.Base, big.NewInt(103))
	return privacytypes.Note{
		ReceiverSpendPubKeyX: spendKey.X.BigInt(new(big.Int)),
		ReceiverSpendPubKeyY: spendKey.Y.BigInt(new(big.Int)),
		ReceiverViewPubKeyX:  viewKey.X.BigInt(new(big.Int)),
		ReceiverViewPubKeyY:  viewKey.Y.BigInt(new(big.Int)),
		Amount:               big.NewInt(7),
		AssetID:              big.NewInt(11),
		Randomness:           big.NewInt(13),
	}
}
