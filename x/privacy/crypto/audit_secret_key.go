package crypto

import (
	"fmt"
	"io"

	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/internal/ctbn254/scalarct"
)

// AuditSecretKey is a distinct role from wallet spend/view SecretScalar.
// Provisioning requires a fresh q* draw or strict persisted-key import; spend
// signing APIs cannot accept this type and prover DTOs never contain it.
type AuditSecretKey struct{ secret SecretScalar }

func ImportAuditSecretKeyBE32(raw []byte) (AuditSecretKey, error) {
	scalar, err := ImportNonzeroScalarBE32(raw)
	if err != nil {
		return AuditSecretKey{}, err
	}
	return AuditSecretKey{secret: scalar}, nil
}
func SampleAuditSecretKey(reader io.Reader) (AuditSecretKey, error) {
	scalar, err := SampleSecretScalar(reader)
	if err != nil {
		return AuditSecretKey{}, err
	}
	return AuditSecretKey{secret: scalar}, nil
}
func (s AuditSecretKey) Bytes() [32]byte                         { return s.secret.Bytes() }
func (s AuditSecretKey) IsValid() bool                           { return s.secret.IsValid() }
func (s AuditSecretKey) scalar() (scalarct.NonzeroScalar, error) { return s.secret.scalar() }
func (AuditSecretKey) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("crypto.AuditSecretKey(<redacted>)"))
}
func (AuditSecretKey) MarshalJSON() ([]byte, error) {
	return nil, fmt.Errorf("audit secret key serialization is disabled")
}
func (AuditSecretKey) MarshalText() ([]byte, error) {
	return nil, fmt.Errorf("audit secret key serialization is disabled")
}
