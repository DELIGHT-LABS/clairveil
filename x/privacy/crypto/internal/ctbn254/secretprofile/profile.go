// Package secretprofile gates the supported native secret execution profile.
// Public-only chain verification does not need to call Check.
package secretprofile

import (
	"errors"
	"os"
	"runtime"
	"strings"

	"golang.org/x/sys/cpu"
)

var ErrUnsupported = errors.New("unsupported native secret execution profile")

type Profile struct {
	OS                 string
	Architecture       string
	GoVersion          string
	AES                bool
	PolynomialMultiply bool
	DIT                bool
	PureGo             bool
}

// Current reports observed capabilities; it is not a timing certification.
func Current() Profile {
	p := Profile{OS: runtime.GOOS, Architecture: runtime.GOARCH, GoVersion: runtime.Version(), PureGo: pureGo}
	switch runtime.GOARCH {
	case "arm64":
		p.AES = cpu.ARM64.HasAES
		p.PolynomialMultiply = cpu.ARM64.HasPMULL
		p.DIT = cpu.ARM64.HasDIT
	case "amd64":
		p.AES = cpu.X86.HasAES
		p.PolynomialMultiply = cpu.X86.HasPCLMULQDQ
	}
	return p
}

// Check must succeed before a facade handles cryptographic secrets. It fails
// closed for unsupported CPUs, purego and explicitly disabled AES acceleration.
func Check() error { return validate(Current(), os.Getenv("GODEBUG")) }

func validate(p Profile, godebug string) error {
	if p.PureGo || (p.OS != "darwin" && p.OS != "linux") || (p.Architecture != "arm64" && p.Architecture != "amd64") || !p.AES || !p.PolynomialMultiply {
		return ErrUnsupported
	}
	if p.Architecture == "arm64" && !p.DIT {
		return ErrUnsupported
	}
	for _, setting := range strings.Split(godebug, ",") {
		switch setting {
		case "cpu.all=off", "cpu.aes=off", "cpu.pmull=off", "cpu.pclmulqdq=off", "cpu.dit=off":
			return ErrUnsupported
		}
	}
	return nil
}
