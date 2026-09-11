package secretprofile

import (
	"errors"
	"testing"
)

func TestProfileGate(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		p := Profile{OS: "linux", Architecture: arch, AES: true, PolynomialMultiply: true, DIT: true}
		if err := validate(p, ""); err != nil {
			t.Fatal(err)
		}
		cases := []Profile{p, p, p, p, p, p}
		cases[0].AES = false
		cases[1].PolynomialMultiply = false
		cases[2].PureGo = true
		cases[3].OS = "windows"
		cases[4].Architecture = "wasm"
		cases[5].Architecture = "arm64"
		cases[5].DIT = false
		for _, bad := range cases {
			if !errors.Is(validate(bad, ""), ErrUnsupported) {
				t.Fatalf("invalid profile accepted: %+v", bad)
			}
		}
		for _, flags := range []string{"cpu.all=off", "cpu.aes=off", "cpu.pmull=off", "cpu.pclmulqdq=off", "cpu.dit=off", "gctrace=1,cpu.aes=off"} {
			if !errors.Is(validate(p, flags), ErrUnsupported) {
				t.Fatal("disabled acceleration accepted")
			}
		}
	}
}

func TestCurrentProfile(t *testing.T) {
	t.Logf("observed native profile: %+v", Current())
	if pureGo {
		if !errors.Is(Check(), ErrUnsupported) {
			t.Fatal("purego accepted")
		}
		return
	}
	if err := Check(); err != nil {
		t.Skipf("host cannot execute native secret tests: %v", err)
	}
}

func TestCheckRejectsDisabledAcceleration(t *testing.T) {
	for _, setting := range []string{"cpu.aes=off", "cpu.all=off"} {
		t.Setenv("GODEBUG", setting)
		if !errors.Is(Check(), ErrUnsupported) {
			t.Fatalf("Check accepted %s", setting)
		}
	}
}
