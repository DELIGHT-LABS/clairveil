package bn254fr

import (
	"errors"
	"fmt"
)

var errSecretEncoding = errors.New("bn254fr: secret serialization is disabled")

// String prevents accidental formatting from exposing Montgomery limbs.
func (Element) String() string { return "bn254fr.Element(<redacted>)" }

// GoString prevents %#v from exposing Montgomery limbs.
func (Element) GoString() string { return "bn254fr.Element(<redacted>)" }

// Format blocks every fmt verb, including numeric verbs that would otherwise
// inspect the representation after String has been bypassed.
func (Element) Format(state fmt.State, _ rune) {
	_, _ = state.Write([]byte("bn254fr.Element(<redacted>)"))
}

// MarshalText rejects implicit text serialization of a secret field element.
func (Element) MarshalText() ([]byte, error) { return nil, errSecretEncoding }

// MarshalJSON rejects implicit JSON serialization of a secret field element.
func (Element) MarshalJSON() ([]byte, error) { return nil, errSecretEncoding }
