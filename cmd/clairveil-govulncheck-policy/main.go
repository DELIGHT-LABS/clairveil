package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

type govulncheckMessage struct {
	Finding *govulncheckFinding `json:"finding"`
}

type govulncheckFinding struct {
	OSV          string                `json:"osv"`
	FixedVersion string                `json:"fixed_version"`
	Trace        []govulncheckTraceRef `json:"trace"`
}

type govulncheckTraceRef struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
}

type acceptedActionableVulnerability struct {
	Reason                 string
	RequireNoFixedVersion  bool
	VulnerableSymbolModule string
}

var acceptedActionableVulnerabilities = map[string]acceptedActionableVulnerability{
	"GO-2024-2584": {
		Reason:                 "Cosmos SDK slashing evasion currently has no fixed version for the reference app; keep tracked as a downstream production risk.",
		RequireNoFixedVersion:  true,
		VulnerableSymbolModule: "github.com/cosmos/cosmos-sdk",
	},
	"GO-2026-4479": {
		Reason:                 "pion/dtls v2 is reachable through the Cosmos SDK/CometBFT server stack and currently has no fixed version; keep tracked as a downstream production risk.",
		RequireNoFixedVersion:  true,
		VulnerableSymbolModule: "github.com/pion/dtls/v2",
	},
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintf(os.Stderr, "usage: %s <govulncheck-jsonl> <govulncheck-exit-code>\n", os.Args[0])
		os.Exit(2)
	}

	scannerExitCode, err := strconv.Atoi(os.Args[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid govulncheck exit code: %v\n", err)
		os.Exit(2)
	}

	findings, err := readFindings(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to read govulncheck JSON: %v\n", err)
		os.Exit(1)
	}

	disallowed := make(map[string]govulncheckFinding)
	accepted := make(map[string]string)
	packageOnlyCount := 0

	for _, finding := range findings {
		if !finding.isActionableSymbolFinding() {
			packageOnlyCount++
			continue
		}
		if reason, ok := finding.acceptedActionableReason(); ok {
			accepted[finding.OSV] = reason
			continue
		}
		disallowed[finding.OSV] = finding
	}

	if len(disallowed) > 0 {
		fmt.Fprintln(os.Stderr, "govulncheck found disallowed actionable vulnerabilities:")
		for _, id := range sortedFindingIDs(disallowed) {
			finding := disallowed[id]
			fmt.Fprintf(os.Stderr, "- %s fixed=%s trace=%s\n", id, fixedVersionLabel(finding.FixedVersion), finding.traceLabel())
		}
		os.Exit(1)
	}

	if scannerExitCode != 0 && len(accepted) == 0 {
		fmt.Fprintf(os.Stderr, "govulncheck exited with %d but no accepted actionable finding was detected\n", scannerExitCode)
		os.Exit(1)
	}

	fmt.Println("govulncheck policy passed")
	if len(accepted) > 0 {
		fmt.Println("accepted actionable vulnerabilities:")
		for _, id := range sortedFindingIDs(accepted) {
			fmt.Printf("- %s: %s\n", id, accepted[id])
		}
	}
	if packageOnlyCount > 0 {
		fmt.Printf("non-actionable package/module findings observed: %d\n", packageOnlyCount)
	}
}

func readFindings(path string) ([]govulncheckFinding, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	decoder := json.NewDecoder(file)
	var findings []govulncheckFinding
	for {
		var message govulncheckMessage
		if err := decoder.Decode(&message); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if message.Finding == nil || strings.TrimSpace(message.Finding.OSV) == "" {
			continue
		}
		findings = append(findings, *message.Finding)
	}
	return findings, nil
}

func (f govulncheckFinding) isActionableSymbolFinding() bool {
	for _, ref := range f.Trace {
		if strings.TrimSpace(ref.Function) != "" {
			return true
		}
	}
	return false
}

func (f govulncheckFinding) acceptedActionableReason() (string, bool) {
	rule, ok := acceptedActionableVulnerabilities[f.OSV]
	if !ok {
		return "", false
	}
	if rule.RequireNoFixedVersion && strings.TrimSpace(f.FixedVersion) != "" {
		return "", false
	}
	if rule.VulnerableSymbolModule != "" {
		module, ok := f.firstActionableSymbolModule()
		if !ok || module != rule.VulnerableSymbolModule {
			return "", false
		}
	}
	return rule.Reason, true
}

func (f govulncheckFinding) firstActionableSymbolModule() (string, bool) {
	for _, ref := range f.Trace {
		if strings.TrimSpace(ref.Function) == "" {
			continue
		}
		return strings.TrimSpace(ref.Module), true
	}
	return "", false
}

func (f govulncheckFinding) traceLabel() string {
	for _, ref := range f.Trace {
		if strings.TrimSpace(ref.Function) == "" {
			continue
		}
		fn := ref.Function
		if ref.Receiver != "" {
			fn = ref.Receiver + "." + fn
		}
		return fmt.Sprintf("%s %s", ref.Package, fn)
	}
	return "n/a"
}

func sortedFindingIDs[V any](findings map[string]V) []string {
	ids := make([]string, 0, len(findings))
	for id := range findings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func fixedVersionLabel(version string) string {
	if strings.TrimSpace(version) == "" {
		return "N/A"
	}
	return version
}
