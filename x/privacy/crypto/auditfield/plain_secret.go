package auditfield

import (
	"errors"
	"fmt"
	"io"
)

var errPlainSerialization = errors.New("audit plaintext implicit serialization is disabled")

// Format redacts plaintext for every fmt verb, including numeric formatting
// that would otherwise traverse private struct fields.
func (AuditPlain) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "auditfield.AuditPlain(<redacted>)")
}
func (AuditPlain) String() string               { return "auditfield.AuditPlain(<redacted>)" }
func (AuditPlain) GoString() string             { return "auditfield.AuditPlain(<redacted>)" }
func (AuditPlain) MarshalText() ([]byte, error) { return nil, errPlainSerialization }
func (AuditPlain) MarshalJSON() ([]byte, error) { return nil, errPlainSerialization }
