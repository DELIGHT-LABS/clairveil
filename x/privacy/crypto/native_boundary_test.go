package crypto

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// This is a regression guard, not a full constant-time or information-flow
// proof. Public gnark conversion is confined to the documented point facade.
func TestT19SecretBackendDependencies(t *testing.T) {
	roots := []string{"internal/ctbn254/frct", "internal/ctbn254/scalarct", "internal/ctbn254/edwardsct", "auditfield"}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			filename := filepath.Join(root, entry.Name())
			f, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range f.Imports {
				path, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				if path == "math/big" || strings.Contains(path, "consensys/gnark") || strings.HasSuffix(path, "/x/privacy/crypto") || strings.Contains(path, "/x/privacy/types") || strings.Contains(path, "/client/sdk") {
					t.Errorf("secret leaf/core forbidden import %s: %s", filename, path)
				}
			}
		}
	}
	for _, filename := range []string{"ecies.go", "secret_scalar.go", "secret_point.go", "owner_signature.go", "legacy_mimc.go", "utils.go", "audit_facade.go", "audit_secret_key.go"} {
		f, err := parser.ParseFile(token.NewFileSet(), filename, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range f.Imports {
			path, _ := strconv.Unquote(imp.Path.Value)
			if path == "math/big" {
				t.Errorf("secret facade imports math/big: %s", filename)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if s, ok := call.Fun.(*ast.SelectorExpr); ok {
				switch s.Sel.Name {
				case "ScalarMultiplication", "BigInt", "SetBigInt", "LexicographicallyLargest", "Divstep", "DivstepPrecomp":
					t.Errorf("secret facade calls variable-time/backend bypass %s in %s", s.Sel.Name, filename)
				}
			}
			return true
		})
	}
}

// TestT19NativeBackendSecretCallerAllowlist is a source-level regression
// guard. It reviews the concrete SDK/CLI production callers below; it is not
// a whole-program information-flow proof.
func TestT19NativeBackendSecretCallerAllowlist(t *testing.T) {
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}

	for _, call := range nativeBackendProductionCalls(t, repoRoot) {
		if reviewedNativeBoundaryCall(call) {
			continue
		}
		if nativeBackendRestrictedCallNames[call.callee] {
			t.Errorf("legacy secret API %s at %s:%d in %s has no reviewed native-backend exception", call.callee, call.file, call.line, call.function)
		}
		if nativeBackendLegacyScanAdapterNames[call.callee] {
			if !reviewedLegacyScanAdapterCall(call) {
				t.Errorf("legacy scan fixture adapter %s is reachable from production caller %s:%d in %s", call.callee, call.file, call.line, call.function)
			}
		}
	}
}

type nativeBackendCall struct {
	file     string
	line     int
	function string
	callee   string
}

// These are the legacy APIs that materialize or hash secret note fields using
// the public big.Int backend. Public tree/vector/intent helpers are excluded.
var nativeBackendRestrictedCallNames = map[string]bool{
	"ComputeCommitment":                         true,
	"ComputeNullifier":                          true,
	"ComputeNoteCommitmentV1":                   true,
	"ComputeNoteNullifierV1":                    true,
	"ComputeTransferDisclosureDigestBytes":      true,
	"ComputeAuditTransferDisclosureDigestBytes": true,
	"LegacyMiMCHash":                            true,
	"ToProverWitnessV1":                         true,
	"MarshalNotePlaintextV1":                    true,
	"UnmarshalNotePlaintextV1":                  true,
	"UnmarshalDisclosurePlaintextV1":            true,
}

// The only permitted conversions are direct circuit-assignment construction.
// The scan entries retain public legacy fixture adapters only; the separate
// adapter-call allowlist below prevents Sync/CLI production paths from using
// them.
var reviewedNativeBoundaryCalls = map[nativeBackendCall]struct{}{
	{file: "x/privacy/client/sdk/deposit/prove.go", function: "BuildDepositAssignment", callee: "ToProverWitnessV1"}:                                 {},
	{file: "x/privacy/client/sdk/transfer/prepare.go", function: "PrepareJoinSplitTransfer", callee: "ToProverWitnessV1"}:                            {},
	{file: "x/privacy/client/sdk/withdraw/prepare.go", function: "PrepareSpendWithdraw", callee: "ToProverWitnessV1"}:                                {},
	{file: "x/privacy/client/sdk/transfer/payload.go", function: "buildJoinSplitAssignmentFromPreparedTransferPayload", callee: "ToProverWitnessV1"}: {},
	{file: "x/privacy/client/sdk/scan/scan.go", function: "ParseNoteBytes", callee: "UnmarshalNotePlaintextV1"}:                                      {},
	{file: "x/privacy/client/sdk/scan/scan.go", function: "noteCommitmentMatches", callee: "ComputeCommitment"}:                                      {},
	{file: "x/privacy/client/sdk/scan/scan.go", function: "buildFoundNote", callee: "ComputeNullifier"}:                                              {},
	// V2 audit deposit witness construction intentionally reuses the
	// canonical NoteV1 witness adapter after the audit public-input binding.
	{file: "x/privacy/client/sdk/deposit/audit_v2.go", function: "BuildAuditV2Witness", callee: "ToProverWitnessV1"}: {},
	// V2 scan disclosure validation intentionally decodes the versioned
	// disclosure envelope and recomputes its legacy-compatible digest.
	{file: "x/privacy/client/sdk/provider/typed_scan.go", function: "validateAuditUserDisclosure", callee: "UnmarshalDisclosurePlaintextV1"}:       {},
	{file: "x/privacy/client/sdk/provider/typed_scan.go", function: "validateAuditUserDisclosure", callee: "ComputeTransferDisclosureDigestBytes"}: {},
}

var nativeBackendLegacyScanAdapterNames = map[string]bool{
	"ParseNoteBytes":              true,
	"noteCommitmentMatches":       true,
	"BuildFoundNote":              true,
	"BuildFoundNoteFromScanEvent": true,
	"buildFoundNote":              true,
}

var reviewedLegacyScanAdapterCalls = map[nativeBackendCall]struct{}{
	{file: "x/privacy/client/sdk/scan/scan.go", function: "BuildFoundNote", callee: "buildFoundNote"}:              {},
	{file: "x/privacy/client/sdk/scan/scan.go", function: "BuildFoundNoteFromScanEvent", callee: "buildFoundNote"}: {},
}

func reviewedNativeBoundaryCall(call nativeBackendCall) bool {
	call.line = 0
	_, ok := reviewedNativeBoundaryCalls[call]
	return ok
}

func reviewedLegacyScanAdapterCall(call nativeBackendCall) bool {
	call.line = 0
	_, ok := reviewedLegacyScanAdapterCalls[call]
	return ok
}

func nativeBackendProductionCalls(t *testing.T, repoRoot string) []nativeBackendCall {
	t.Helper()
	var calls []nativeBackendCall
	for _, root := range []string{
		filepath.Join(repoRoot, "x/privacy/client/sdk"),
		filepath.Join(repoRoot, "x/privacy/client/cli"),
		filepath.Join(repoRoot, "cmd/clairveil-verify"),
	} {
		err := filepath.Walk(root, func(filename string, info os.FileInfo, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if info.IsDir() || !strings.HasSuffix(filename, ".go") || strings.HasSuffix(filename, "_test.go") {
				return nil
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, filename, nil, 0)
			if err != nil {
				return err
			}
			relative, err := filepath.Rel(repoRoot, filename)
			if err != nil {
				return err
			}
			relative = filepath.ToSlash(relative)
			for _, declaration := range f.Decls {
				function, ok := declaration.(*ast.FuncDecl)
				if !ok || function.Body == nil {
					continue
				}
				ast.Inspect(function.Body, func(node ast.Node) bool {
					call, ok := node.(*ast.CallExpr)
					if !ok {
						return true
					}
					name, ok := nativeBackendCalledName(call.Fun)
					if !ok {
						return true
					}
					calls = append(calls, nativeBackendCall{
						file: relative, line: fset.Position(call.Pos()).Line, function: function.Name.Name, callee: name,
					})
					return true
				})
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return calls
}

func nativeBackendCalledName(expression ast.Expr) (string, bool) {
	switch value := expression.(type) {
	case *ast.Ident:
		return value.Name, true
	case *ast.SelectorExpr:
		return value.Sel.Name, true
	default:
		return "", false
	}
}
