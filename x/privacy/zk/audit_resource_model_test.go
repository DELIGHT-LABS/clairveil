package zk

import (
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	"github.com/stretchr/testify/require"
	"math"
	"reflect"
	"testing"
)

func TestAuditGasExactFormula(t *testing.T) {
	for _, tc := range []struct {
		k          auditfield.Kind
		i, o, n, e uint64
	}{{1, 0, 1, 6, 336}, {2, 1, 0, 2, 208}, {3, 2, 2, 13, 560}, {4, 16, 32, 177, 5808}} {
		u := AuditResourceUsageV1{Kind: tc.k, InputCount: tc.i, OutputCount: tc.o, AuxBytes: 100, ProjectedStateBytes: 20000, TransitionBytes: 16000}
		g, err := ComputeAuditGasV1(DefaultAuditGasModelV1(), AuditResourceBoundsV1{128 << 10, 20000}, u)
		require.NoError(t, err)
		require.Equal(t, uint64(1000000)+25000*tc.i+50000*tc.o+4*(100+tc.e)+8*20000+5000*tc.o*33+10000*(tc.i+tc.o)+400000+25000*(25+tc.n+4), g.Total)
	}
}
func TestAuditGasRejectsBoundsAndOverflow(t *testing.T) {
	base := AuditResourceUsageV1{Kind: 4, InputCount: 16, OutputCount: 32, AuxBytes: 4, ProjectedStateBytes: 16384, TransitionBytes: 16384}
	bounds := AuditResourceBoundsV1{128 << 10, 16384}
	for name, mutate := range map[string]func(*AuditResourceUsageV1){
		"kind5": func(u *AuditResourceUsageV1) { u.Kind = 5 }, "zero input": func(u *AuditResourceUsageV1) { u.InputCount = 0 }, "zero output": func(u *AuditResourceUsageV1) { u.OutputCount = 0 }, "input overflow": func(u *AuditResourceUsageV1) { u.InputCount = 256 }, "output overflow": func(u *AuditResourceUsageV1) { u.OutputCount = 256 }, "Aux prefix": func(u *AuditResourceUsageV1) { u.AuxBytes = 3 }, "Aux overflow": func(u *AuditResourceUsageV1) { u.AuxBytes = math.MaxUint64 }, "Aux cap": func(u *AuditResourceUsageV1) { u.AuxBytes = 128 << 10 }, "transition zero": func(u *AuditResourceUsageV1) { u.TransitionBytes = 0 }, "transition cap": func(u *AuditResourceUsageV1) { u.TransitionBytes++ }, "projection low": func(u *AuditResourceUsageV1) { u.ProjectedStateBytes-- }, "projection cap": func(u *AuditResourceUsageV1) { u.ProjectedStateBytes++ },
	} {
		t.Run(name, func(t *testing.T) {
			u := base
			mutate(&u)
			_, err := ComputeAuditGasV1(DefaultAuditGasModelV1(), bounds, u)
			require.Error(t, err)
		})
	}
	for _, b := range []AuditResourceBoundsV1{{0, 16384}, {128 << 10, 0}, {128<<10 + 1, 16384}, {1, 16384}} {
		_, err := ComputeAuditGasV1(DefaultAuditGasModelV1(), b, base)
		require.Error(t, err)
	}
	for _, value := range []uint64{0, math.MaxUint64} {
		for i := 0; i < 9; i++ {
			m := DefaultAuditGasModelV1()
			v := reflect.ValueOf(&m).Elem()
			if i < 7 {
				v.Field(0).Field(i).SetUint(value)
			} else {
				v.Field(i - 6).SetUint(value)
			}
			_, err := ComputeAuditGasV1(m, bounds, base)
			require.Error(t, err, "coefficient %d value %d", i, value)
		}
	}
}
