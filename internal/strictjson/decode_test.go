package strictjson

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestStrictShapeAndOpaqueRawMessage(t *testing.T) {
	type record struct {
		Field []byte `json:"field"`
	}
	type input struct {
		Records []record                   `json:"records"`
		Modules map[string]json.RawMessage `json:"modules"`
	}
	for _, raw := range []string{
		`{"records":[],"unknown":[]}`,
		`{"Records":[]}`,
		`{"records":[{"Field":""}]}`,
		`{"records":[],"records":[]}`,
		`{"modules":{"sdk":{"x":1,"x":2}}}`,
		`{"records":[]} {}`,
	} {
		var v input
		require.Error(t, Decode([]byte(raw), &v), raw)
	}
	var v input
	require.NoError(t, Decode([]byte(`{"records":[{"field":"AQI="}],"modules":{"sdk":{"other":[1,2]}}}`), &v))
	require.Equal(t, []byte{1, 2}, v.Records[0].Field)
	require.JSONEq(t, `{"other":[1,2]}`, string(v.Modules["sdk"]))
}
