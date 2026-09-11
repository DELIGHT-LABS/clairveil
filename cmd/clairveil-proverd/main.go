package main

import (
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	clairveiltypes "github.com/DELIGHT-LABS/clairveil/types"
	privacyproverservice "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/proverservice"
	privacyzk "github.com/DELIGHT-LABS/clairveil/x/privacy/zk"
)

func configureSDK() {
	clairveiltypes.SetConfig()
}

func main() {
	configureSDK()

	config := privacyproverservice.DefaultServerConfig()
	flag.StringVar(&config.ListenAddress, "listen", config.ListenAddress, "listen address for the privacy prover service")
	flag.DurationVar(&config.ReadHeaderTimeout, "read-header-timeout", config.ReadHeaderTimeout, "maximum duration for reading request headers")
	flag.DurationVar(&config.ReadTimeout, "read-timeout", config.ReadTimeout, "maximum duration for reading the full request body")
	flag.DurationVar(&config.WriteTimeout, "write-timeout", config.WriteTimeout, "maximum duration for writing a response (0 disables the timeout)")
	flag.DurationVar(&config.IdleTimeout, "idle-timeout", config.IdleTimeout, "maximum keep-alive idle timeout")
	flag.Int64Var(&config.MaxRequestBytes, "max-request-bytes", config.MaxRequestBytes, "maximum accepted JSON request body size in bytes (must be positive)")
	flag.Parse()

	if err := config.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "invalid clairveil-proverd configuration: %v\n", err)
		os.Exit(1)
	}

	bearerToken := strings.TrimSpace(os.Getenv(privacyproverservice.BearerTokenEnv))
	artifactDir := strings.TrimSpace(os.Getenv(privacyzk.ZKArtifactDirEnv))
	if artifactDir == "" {
		fmt.Fprintln(os.Stderr, "CLAIRVEIL_PRIVACY_ZK_ARTIFACT_DIR is required for the audit-field V2 runtime")
		os.Exit(1)
	}
	registry, err := privacyzk.NewArtifactRegistry(privacyzk.ArtifactRegistryConfig{
		CircuitSetID:       privacyzk.AuditFieldCircuitSetID,
		ArtifactDir:        artifactDir,
		RuntimeEnvironment: privacyzk.ZKRuntimeEnvironmentDevelopment,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-field runtime registry failed: %v\n", err)
		os.Exit(1)
	}
	identity, err := registry.LocalCircuitSetIdentity()
	if err != nil {
		fmt.Fprintf(os.Stderr, "audit-field runtime identity failed: %v\n", err)
		os.Exit(1)
	}
	if err := privacyproverservice.RunAuditFieldPreflight(registry, identity); err != nil {
		fmt.Fprintf(os.Stderr, "audit-field runtime preflight failed: %v\n", err)
		os.Exit(1)
	}
	handler := privacyproverservice.NewAuditFieldHandler(registry, identity, time.Now, os.Stderr, config.MaxRequestBytes, bearerToken)
	server, err := config.HTTPServer(handler)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to build clairveil-proverd HTTP server: %v\n", err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "clairveil-proverd listening on %s (auth_enabled=%t)\n", config.ListenAddress, bearerToken != "")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintf(os.Stderr, "clairveil-proverd stopped with error: %v\n", err)
		os.Exit(1)
	}
}
