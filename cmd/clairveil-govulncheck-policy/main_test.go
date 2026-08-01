package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindingActionability(t *testing.T) {
	symbolFinding := govulncheckFinding{
		OSV: "GO-TEST-1",
		Trace: []govulncheckTraceRef{
			{Package: "example.com/pkg"},
			{Package: "example.com/pkg", Function: "Vulnerable"},
		},
	}
	if !symbolFinding.isActionableSymbolFinding() {
		t.Fatalf("expected finding with function trace to be actionable")
	}

	packageOnlyFinding := govulncheckFinding{
		OSV: "GO-TEST-2",
		Trace: []govulncheckTraceRef{
			{Package: "example.com/pkg"},
		},
	}
	if packageOnlyFinding.isActionableSymbolFinding() {
		t.Fatalf("expected package-only finding to be non-actionable")
	}
}

func TestAcceptedActionableReasonIsScopedToNoFixedVulnerableModule(t *testing.T) {
	accepted := govulncheckFinding{
		OSV: "GO-2026-4479",
		Trace: []govulncheckTraceRef{
			{Module: "github.com/pion/dtls/v2", Package: "github.com/pion/dtls/v2/pkg/protocol/extension", Function: "Marshal"},
			{Module: "github.com/pion/dtls/v3", Package: "github.com/pion/dtls/v3", Function: "Close", Receiver: "*Conn"},
		},
	}
	if _, ok := accepted.acceptedActionableReason(); !ok {
		t.Fatalf("expected no-fixed dtls/v2 vulnerable symbol finding to be accepted")
	}

	fixed := accepted
	fixed.FixedVersion = "v3.1.2"
	if _, ok := fixed.acceptedActionableReason(); ok {
		t.Fatalf("expected finding with a fixed version to be disallowed")
	}

	wrongModule := accepted
	wrongModule.Trace[0].Module = "github.com/pion/dtls/v3"
	if _, ok := wrongModule.acceptedActionableReason(); ok {
		t.Fatalf("expected dtls/v3 vulnerable symbol finding to be disallowed")
	}
}

func TestAcceptedOpenPGPArmorFindingIsScopedToNoFixedXCryptoArmorPackage(t *testing.T) {
	accepted := govulncheckFinding{
		OSV: "GO-2026-5932",
		Trace: []govulncheckTraceRef{
			{Module: "golang.org/x/crypto", Package: "golang.org/x/crypto/openpgp/armor", Function: "Decode"},
		},
	}
	if _, ok := accepted.acceptedActionableReason(); !ok {
		t.Fatalf("expected no-fixed x/crypto OpenPGP armor finding to be accepted")
	}

	fixed := accepted
	fixed.FixedVersion = "v0.55.0"
	if _, ok := fixed.acceptedActionableReason(); ok {
		t.Fatalf("expected OpenPGP finding with a fixed version to be disallowed")
	}

	allowedErrorsPackage := accepted
	allowedErrorsPackage.Trace = append([]govulncheckTraceRef(nil), accepted.Trace...)
	allowedErrorsPackage.Trace[0].Package = "golang.org/x/crypto/openpgp/errors"
	allowedErrorsPackage.Trace[0].Function = "Error"
	if _, ok := allowedErrorsPackage.acceptedActionableReason(); !ok {
		t.Fatalf("expected the error-only dependency of OpenPGP armor to be accepted")
	}

	wrongModule := accepted
	wrongModule.Trace = append([]govulncheckTraceRef(nil), accepted.Trace...)
	wrongModule.Trace[0].Module = "example.com/openpgp"
	if _, ok := wrongModule.acceptedActionableReason(); ok {
		t.Fatalf("expected OpenPGP finding from another module to be disallowed")
	}

	wrongPackage := accepted
	wrongPackage.Trace = append([]govulncheckTraceRef(nil), accepted.Trace...)
	wrongPackage.Trace[0].Package = "golang.org/x/crypto/openpgp"
	if _, ok := wrongPackage.acceptedActionableReason(); ok {
		t.Fatalf("expected non-armor OpenPGP finding from x/crypto to be disallowed")
	}
}

func TestReadFindingsSkipsNonFindingMessages(t *testing.T) {
	path := filepath.Join(t.TempDir(), "govulncheck.jsonl")
	input := `{"config":{"protocol_version":"v1"}}
{"finding":{"osv":"GO-TEST-1","fixed_version":"v1.2.3","trace":[{"package":"example.com/pkg","function":"Vulnerable"}]}}
{"progress":{"message":"done"}}
`
	if err := os.WriteFile(path, []byte(input), 0o600); err != nil {
		t.Fatalf("failed to write test report: %v", err)
	}

	findings, err := readFindings(path)
	if err != nil {
		t.Fatalf("readFindings failed: %v", err)
	}
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding, got %d", len(findings))
	}
	if findings[0].OSV != "GO-TEST-1" {
		t.Fatalf("unexpected finding id: %s", findings[0].OSV)
	}
	if !findings[0].isActionableSymbolFinding() {
		t.Fatalf("expected parsed finding to be actionable")
	}
}

func TestValidateScannerResultFailsClosed(t *testing.T) {
	testCases := []struct {
		name            string
		scannerExitCode int
		wantError       bool
	}{
		{name: "clean scan", scannerExitCode: 0},
		{name: "scan error", scannerExitCode: 1, wantError: true},
		{name: "usage error", scannerExitCode: 2, wantError: true},
		{name: "text-format vulnerability exit", scannerExitCode: 3, wantError: true},
		{name: "unexpected exit", scannerExitCode: 4, wantError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateScannerResult(testCase.scannerExitCode)
			if testCase.wantError && err == nil {
				t.Fatal("expected scanner result validation to fail")
			}
			if !testCase.wantError && err != nil {
				t.Fatalf("expected scanner result validation to pass: %v", err)
			}
		})
	}
}
