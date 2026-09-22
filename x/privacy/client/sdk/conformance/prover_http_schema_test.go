package conformance_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/stretchr/testify/require"
)

func TestProverHTTPSchemaContract(t *testing.T) {
	repoRoot, fixtureDir := proverHTTPSchemaPaths(t)
	schemaPath := filepath.Join(repoRoot, "docs", "schemas", "clairveil-proverd-http-api.schema.json")

	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	schema, err := compiler.Compile(schemaPath)
	require.NoError(t, err)

	fixturePaths := []string{
		filepath.Join(fixtureDir, "privacy_prover_http_api_contract.json"),
		filepath.Join(fixtureDir, "privacy_deposit_prover_contract.json"),
		filepath.Join(fixtureDir, "privacy_audit_field_prover_contract.json"),
	}
	for _, fixturePath := range fixturePaths {
		fixturePath := fixturePath
		t.Run(filepath.Base(fixturePath), func(t *testing.T) {
			require.NoError(t, schema.Validate(loadJSONSchemaValue(t, fixturePath)))
		})
	}

	t.Run("rejects unknown top-level field", func(t *testing.T) {
		fixture := requireJSONObject(t, loadJSONSchemaValue(t, fixturePaths[0]))
		fixture["unexpected"] = true
		require.Error(t, schema.Validate(fixture))
	})

	t.Run("rejects amount above uint128", func(t *testing.T) {
		fixture := requireJSONObject(t, loadJSONSchemaValue(t, fixturePaths[1]))
		request := requireJSONObject(t, fixture["canonical_positive_request"])
		payload := requireJSONObject(t, request["payload"])
		payload["amount"] = "340282366920938463463374607431768211456"
		require.Error(t, schema.Validate(fixture))
	})

	t.Run("rejects audit-field public-input count drift", func(t *testing.T) {
		fixture := requireJSONObject(t, loadJSONSchemaValue(t, fixturePaths[2]))
		request := requireJSONObject(t, fixture["shape_only_request"])
		inputs, ok := request["public_inputs"].([]any)
		require.True(t, ok)
		request["public_inputs"] = inputs[:22]
		require.Error(t, schema.Validate(fixture))
	})

	t.Run("rejects audit-field non-32-byte base64 framing", func(t *testing.T) {
		fixture := requireJSONObject(t, loadJSONSchemaValue(t, fixturePaths[2]))
		request := requireJSONObject(t, fixture["shape_only_request"])
		request["artifact_hash"] = "AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQ=="
		require.Error(t, schema.Validate(fixture))
	})
}

func proverHTTPSchemaPaths(t *testing.T) (string, string) {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	require.True(t, ok)
	fixtureDir := filepath.Join(filepath.Dir(filename), "testdata")
	repoRoot := filepath.Clean(filepath.Join(fixtureDir, "..", "..", "..", "..", "..", ".."))
	return repoRoot, fixtureDir
}

func loadJSONSchemaValue(t *testing.T, path string) any {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, file.Close()) })
	value, err := jsonschema.UnmarshalJSON(file)
	require.NoError(t, err)
	return value
}

func requireJSONObject(t *testing.T, value any) map[string]any {
	t.Helper()

	object, ok := value.(map[string]any)
	require.True(t, ok, "JSON value is %T, want object", value)
	return object
}
