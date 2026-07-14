package provider

import (
	"testing"

	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
)

func FuzzValidatePrivacyScanPageNoPanicAndStableRoundTrip(f *testing.F) {
	seed := &privacytypes.QueryPrivacyScanResponse{
		NextCursor:        &privacytypes.PrivacyScanCursorV1{},
		ScanSchemaVersion: privacytypes.PrivacyScanSchemaVersionV2,
	}
	valid, err := seed.Marshal()
	if err != nil {
		f.Fatalf("marshal privacy scan seed: %v", err)
	}
	f.Add(valid, int64(0), uint64(0), uint32(0), byte(0))
	f.Add([]byte(nil), int64(1), uint64(1), uint32(0), byte(1))
	f.Add(make([]byte, 1024), int64(-1), ^uint64(0), ^uint32(0), byte(255))

	f.Fuzz(func(t *testing.T, encoded []byte, height int64, sequence uint64, outputIndex uint32, limitByte byte) {
		if len(encoded) > 256<<10 {
			return
		}
		if height < 0 {
			height = 0
		}
		req := &privacytypes.QueryPrivacyScanRequest{
			After: &privacytypes.PrivacyScanCursorV1{
				Height: height, GlobalSequence: sequence, OutputIndex: outputIndex,
			},
			OutputLimit:     uint32(limitByte % 33),
			EventLimit:      uint32(limitByte % 65),
			MaxEncodedBytes: uint64(limitByte) * 1024,
		}
		var response privacytypes.QueryPrivacyScanResponse
		if err := response.Unmarshal(encoded); err != nil {
			return
		}
		if err := ValidatePrivacyScanPage(req, &response); err != nil {
			return
		}
		roundTripBytes, err := response.Marshal()
		if err != nil {
			t.Fatalf("accepted privacy scan page did not marshal: %v", err)
		}
		var roundTrip privacytypes.QueryPrivacyScanResponse
		if err := roundTrip.Unmarshal(roundTripBytes); err != nil {
			t.Fatalf("accepted privacy scan page did not unmarshal: %v", err)
		}
		if err := ValidatePrivacyScanPage(req, &roundTrip); err != nil {
			t.Fatalf("accepted privacy scan page changed validity after protobuf round trip: %v", err)
		}
	})
}
