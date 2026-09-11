package auditfield

import "testing"

func TestAuxRejectsInconsistentActiveDisclosure(t *testing.T) {
	valid := AuxOutput{
		RecoveryCiphertext: make([]byte, noteRecoveryCiphertextSize), ViewTag: []byte{1, 2},
		UserPolicy: 1, UserMode: 2, UserDigest: Field32FromUint64(1), SelfFullDigest: Field32FromUint64(2),
		UserTargetPubKey: testLegacyPoint(t), UserDisclosurePayload: make([]byte, encryptedDisclosurePayloadSize),
	}
	if _, err := EncodeAuxFrame(KindBatch16x32, []AuxOutput{valid}); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*AuxOutput){
		func(o *AuxOutput) { o.UserPolicy = 0 },
		func(o *AuxOutput) { o.UserDigest = Field32{} },
		func(o *AuxOutput) { o.SelfFullDigest = Field32{} },
	} {
		bad := valid
		mutate(&bad)
		if _, err := EncodeAuxFrame(KindBatch16x32, []AuxOutput{bad}); err == nil {
			t.Fatal("accepted inconsistent disclosure")
		}
	}
}
