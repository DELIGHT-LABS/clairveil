package main

import (
	"testing"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/backend/witness"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/circuit"
	privacyprovertransport "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/provertransport"
)

func TestGeneratedProverLoadRequestsProduceReferenceProofs(t *testing.T) {
	configureSDK()

	requests, err := generatedProverLoadRequests(time.Now().UTC())
	if err != nil {
		t.Fatalf("generate requests: %v", err)
	}

	transferRequest, err := privacyprovertransport.DecodeTransferProofRequestJSON(requests["transfer"].Body)
	if err != nil {
		t.Fatalf("decode transfer request: %v", err)
	}
	withdrawRequest, err := privacyprovertransport.DecodeWithdrawProofRequestJSON(requests["withdraw"].Body)
	if err != nil {
		t.Fatalf("decode withdraw request: %v", err)
	}

	joinSplitArtifacts := setupJoinSplitArtifacts(t)
	transferResponse, err := privacyprovertransport.BuildTransferProofResponse(*transferRequest, joinSplitArtifacts, referenceProofRunner{})
	if err != nil {
		t.Fatalf("build transfer proof response: %v", err)
	}
	if err := privacyprovertransport.ValidateTransferProofResponse(*transferRequest, *transferResponse); err != nil {
		t.Fatalf("validate transfer proof response: %v", err)
	}

	spendArtifacts := setupSpendArtifacts(t)
	withdrawResponse, err := privacyprovertransport.BuildWithdrawProofResponse(*withdrawRequest, spendArtifacts, referenceProofRunner{}, time.Now().UTC())
	if err != nil {
		t.Fatalf("build withdraw proof response: %v", err)
	}
	if err := privacyprovertransport.ValidateWithdrawProofResponse(*withdrawRequest, *withdrawResponse, time.Now().UTC()); err != nil {
		t.Fatalf("validate withdraw proof response: %v", err)
	}
}

type referenceJoinSplitArtifacts struct {
	r1cs constraint.ConstraintSystem
	pk   groth16.ProvingKey
}

func (a referenceJoinSplitArtifacts) JoinSplitR1CS() (constraint.ConstraintSystem, error) {
	return a.r1cs, nil
}

func (a referenceJoinSplitArtifacts) JoinSplitProvingKey() (groth16.ProvingKey, error) {
	return a.pk, nil
}

type referenceSpendArtifacts struct {
	r1cs constraint.ConstraintSystem
	pk   groth16.ProvingKey
}

func (a referenceSpendArtifacts) SpendR1CS() (constraint.ConstraintSystem, error) {
	return a.r1cs, nil
}

func (a referenceSpendArtifacts) SpendProvingKey() (groth16.ProvingKey, error) {
	return a.pk, nil
}

type referenceProofRunner struct{}

func (referenceProofRunner) ProveJoinSplit(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, witness)
}

func (referenceProofRunner) ProveSpend(r1cs constraint.ConstraintSystem, provingKey groth16.ProvingKey, witness witness.Witness) (groth16.Proof, error) {
	return groth16.Prove(r1cs, provingKey, witness)
}

func setupJoinSplitArtifacts(t *testing.T) referenceJoinSplitArtifacts {
	t.Helper()
	cs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.JoinSplitCircuit{})
	if err != nil {
		t.Fatalf("compile joinsplit circuit: %v", err)
	}
	pk, _, err := groth16.Setup(cs)
	if err != nil {
		t.Fatalf("setup joinsplit circuit: %v", err)
	}
	return referenceJoinSplitArtifacts{r1cs: cs, pk: pk}
}

func setupSpendArtifacts(t *testing.T) referenceSpendArtifacts {
	t.Helper()
	cs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit.SpendCircuit{})
	if err != nil {
		t.Fatalf("compile spend circuit: %v", err)
	}
	pk, _, err := groth16.Setup(cs)
	if err != nil {
		t.Fatalf("setup spend circuit: %v", err)
	}
	return referenceSpendArtifacts{r1cs: cs, pk: pk}
}
