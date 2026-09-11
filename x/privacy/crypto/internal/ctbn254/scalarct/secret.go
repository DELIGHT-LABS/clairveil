package scalarct

import (
	"errors"
	"fmt"
)

var errSecretEncoding = errors.New("scalarct: secret serialization is disabled")

// String prevents accidental formatting from exposing Montgomery limbs.
func (Scalar) String() string { return "scalarct.Scalar(<redacted>)" }

// GoString prevents %#v from exposing Montgomery limbs.
func (Scalar) GoString() string { return "scalarct.Scalar(<redacted>)" }

// Format blocks every fmt verb, including numeric verbs that would otherwise
// inspect the representation after String has been bypassed.
func (Scalar) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("scalarct.Scalar(<redacted>)"))
}

// MarshalText rejects implicit text serialization of a secret scalar.
func (Scalar) MarshalText() ([]byte, error) { return nil, errSecretEncoding }

// MarshalJSON rejects implicit JSON serialization of a secret scalar.
func (Scalar) MarshalJSON() ([]byte, error) { return nil, errSecretEncoding }

// String prevents accidental formatting from exposing a secret scalar.
func (NonzeroScalar) String() string { return "scalarct.NonzeroScalar(<redacted>)" }

// GoString prevents %#v from exposing a secret scalar.
func (NonzeroScalar) GoString() string { return "scalarct.NonzeroScalar(<redacted>)" }

// Format blocks every fmt verb for a nonzero secret scalar.
func (NonzeroScalar) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("scalarct.NonzeroScalar(<redacted>)"))
}

// MarshalText rejects implicit text serialization of a secret scalar.
func (NonzeroScalar) MarshalText() ([]byte, error) { return nil, errSecretEncoding }

// MarshalJSON rejects implicit JSON serialization of a secret scalar.
func (NonzeroScalar) MarshalJSON() ([]byte, error) { return nil, errSecretEncoding }
