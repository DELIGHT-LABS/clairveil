package provertransport

import (
	"encoding/json"
	"testing"
)

func FuzzDecodeBatchTransferProofRequestJSONStrictRoundTrip(f *testing.F) {
	f.Add([]byte(`{"version":"v1","payload":{}}`))
	f.Add([]byte(nil))
	f.Add([]byte(`{"version":"v1","version":"v1","payload":{}}`))
	f.Add([]byte(`{"version":"v1","payload":{},"unexpected":true}`))
	f.Add([]byte(`{"version":"v1","payload":{}} {}`))

	f.Fuzz(func(t *testing.T, encoded []byte) {
		if len(encoded) > 256<<10 {
			return
		}
		request, err := DecodeBatchTransferProofRequestJSON(encoded)
		if err != nil {
			return
		}
		roundTripBytes, err := json.Marshal(request)
		if err != nil {
			t.Fatalf("accepted batch prover request did not marshal: %v", err)
		}
		roundTrip, err := DecodeBatchTransferProofRequestJSON(roundTripBytes)
		if err != nil {
			t.Fatalf("accepted batch prover request did not strictly round trip: %v", err)
		}
		if roundTrip.Version != request.Version || roundTrip.Payload.PayloadHash != request.Payload.PayloadHash {
			t.Fatal("batch prover request identity changed after strict JSON round trip")
		}
	})
}
