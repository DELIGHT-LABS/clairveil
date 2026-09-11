package provider

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	privacyaudit "github.com/DELIGHT-LABS/clairveil/x/privacy/client/sdk/audit"
	"github.com/DELIGHT-LABS/clairveil/x/privacy/crypto/auditfield"
	privacytypes "github.com/DELIGHT-LABS/clairveil/x/privacy/types"
	privacyv2 "github.com/DELIGHT-LABS/clairveil/x/privacy/types/v2"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// AuditKeyQuerier exposes epoch-specific key history. The key is pinned at
// the same height as the configuration/schedule response before it is used.
type AuditKeyQuerier interface {
	AuditKey(context.Context, *privacyv2.QueryAuditKeyRequest, ...grpc.CallOption) (*privacyv2.QueryAuditRecordResponse, error)
}

type AuditKeyScheduleQuerier interface {
	AuditKeySchedule(context.Context, *privacyv2.QueryAuditKeyScheduleRequest, ...grpc.CallOption) (*privacyv2.QueryAuditKeyScheduleResponse, error)
}

type AuditConfigurationQuerier interface {
	AuditConfiguration(context.Context, *privacyv2.QueryAuditConfigurationRequest, ...grpc.CallOption) (*privacyv2.QueryAuditConfigurationResponse, error)
}

type AuditV2QueryProvider struct {
	Keys     AuditKeyQuerier
	Schedule AuditKeyScheduleQuerier
	Config   AuditConfigurationQuerier
}

func NewAuditV2QueryProvider(query privacyv2.QueryClient) AuditV2QueryProvider {
	return AuditV2QueryProvider{Keys: query, Schedule: query, Config: query}
}

// ActiveAuditSnapshotFromConfiguration obtains the nonce, initial height and
// exact circuit identity from the small on-chain configuration query. The
// caller supplies its verifier identity and local artifact digest; neither is
// substituted with a current runtime archive or a mutable server response.
func (p AuditV2QueryProvider) ActiveAuditSnapshotFromConfiguration(ctx context.Context, chainID string, expected *privacytypes.CircuitSetIdentity, artifactHash [32]byte) (privacyaudit.Snapshot, error) {
	if p.Keys == nil || p.Schedule == nil || p.Config == nil {
		return privacyaudit.Snapshot{}, fmt.Errorf("audit v2 query clients are required")
	}
	if chainID == "" || expected == nil || artifactHash == ([32]byte{}) {
		return privacyaudit.Snapshot{}, fmt.Errorf("chain id, verifier identity, and artifact digest are required")
	}
	config, err := p.Config.AuditConfiguration(ctx, &privacyv2.QueryAuditConfigurationRequest{})
	if err != nil {
		return privacyaudit.Snapshot{}, err
	}
	if config == nil || len(config.NetworkNonce) != 32 || config.InitialHeight == 0 || config.StateHeight < int64(config.InitialHeight) {
		return privacyaudit.Snapshot{}, fmt.Errorf("invalid audit configuration response")
	}
	var identity privacytypes.CircuitSetIdentity
	if err := proto.Unmarshal(config.CircuitIdentity, &identity); err != nil || privacytypes.ValidateAuditCircuitSetIdentity(&identity) != nil || !reflect.DeepEqual(&identity, expected) {
		return privacyaudit.Snapshot{}, fmt.Errorf("audit verifier identity does not match on-chain configuration")
	}
	var nonce [32]byte
	copy(nonce[:], config.NetworkNonce)
	network, err := auditfield.NetworkDigest(chainID, nonce)
	if err != nil {
		return privacyaudit.Snapshot{}, err
	}
	schedule, err := p.Schedule.AuditKeySchedule(auditQueryContextAtHeight(ctx, config.StateHeight), &privacyv2.QueryAuditKeyScheduleRequest{})
	if err != nil {
		return privacyaudit.Snapshot{}, err
	}
	if schedule == nil || schedule.ActiveEpoch == 0 || schedule.StateHeight != config.StateHeight || schedule.Halted {
		return privacyaudit.Snapshot{}, fmt.Errorf("no active audit key is available at configuration height")
	}
	return p.activeAuditSnapshot(ctx, network, config.InitialHeight, artifactHash, schedule)
}

func (p AuditV2QueryProvider) activeAuditSnapshot(ctx context.Context, network [32]byte, initialHeight uint64, artifactHash [32]byte, schedule *privacyv2.QueryAuditKeyScheduleResponse) (privacyaudit.Snapshot, error) {
	recordResponse, err := p.Keys.AuditKey(auditQueryContextAtHeight(ctx, schedule.StateHeight), &privacyv2.QueryAuditKeyRequest{Epoch: schedule.ActiveEpoch})
	if err != nil {
		return privacyaudit.Snapshot{}, err
	}
	if recordResponse == nil || recordResponse.StateHeight != schedule.StateHeight {
		return privacyaudit.Snapshot{}, fmt.Errorf("audit key query height mismatch")
	}
	record, err := privacyaudit.ParseKeyRecord(recordResponse.Record, network, initialHeight)
	if err != nil {
		return privacyaudit.Snapshot{}, err
	}
	if record.Epoch != schedule.ActiveEpoch || record.ActivationHeight > uint64(schedule.StateHeight) {
		return privacyaudit.Snapshot{}, fmt.Errorf("audit key schedule mismatch")
	}
	return record.Snapshot(network, schedule.StateHeight, artifactHash)
}

func auditQueryContextAtHeight(ctx context.Context, height int64) context.Context {
	md, ok := metadata.FromOutgoingContext(ctx)
	if !ok {
		md = metadata.MD{}
	} else {
		md = md.Copy()
	}
	md.Set(grpctypes.GRPCBlockHeightHeader, strconv.FormatInt(height, 10))
	return metadata.NewOutgoingContext(ctx, md)
}
