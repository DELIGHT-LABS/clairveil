package app

import (
	"bytes"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/encoding/protowire"
)

// Original TxRaw/TxBody bytes are inspected before the SDK decoder. The SDK
// retains responsibility for ADR-027, signatures and non-privacy transaction fields.
func auditTxDecoder(base sdk.TxDecoder, registry codectypes.InterfaceRegistry) sdk.TxDecoder {
	return func(raw []byte) (sdk.Tx, error) {
		bodies, err := auditBytesFields(raw, 1)
		if err != nil {
			return nil, err
		}
		if len(bodies) != 1 {
			return nil, fmt.Errorf("one transaction body required")
		}
		messages, err := auditBytesFields(bodies[0], 1)
		if err != nil {
			return nil, err
		}
		count := 0
		for _, m := range messages {
			if err := auditWireAny(m, registry, 0, &count, false); err != nil {
				return nil, err
			}
		}
		return base(raw)
	}
}
func auditBytesFields(raw []byte, wanted protowire.Number) ([][]byte, error) {
	var result [][]byte
	for len(raw) > 0 {
		num, typ, n := protowire.ConsumeTag(raw)
		if n < 0 {
			return nil, fmt.Errorf("malformed protobuf tag")
		}
		raw = raw[n:]
		size := protowire.ConsumeFieldValue(num, typ, raw)
		if size < 0 {
			return nil, fmt.Errorf("malformed protobuf value")
		}
		if num == wanted {
			if typ != protowire.BytesType {
				return nil, fmt.Errorf("invalid message wire type")
			}
			value, _ := protowire.ConsumeBytes(raw)
			result = append(result, value)
		}
		raw = raw[size:]
	}
	return result, nil
}

// auditWireAny recognizes the only supported nested execution container:
// MsgSubmitProposal.Messages. Privacy transactions are rejected beneath that
// path; governance may still carry the three audit-key administration messages.
func auditWireAny(raw []byte, registry codectypes.InterfaceRegistry, depth int, count *int, governancePath ...bool) error {
	inGovernanceProposal := len(governancePath) == 1 && governancePath[0]
	if depth > 16 {
		return fmt.Errorf("Any nesting exceeds 16")
	}
	*count++
	if *count > 256 {
		return fmt.Errorf("Any count exceeds 256")
	}
	a := &codectypes.Any{}
	if err := auditCanonicalMessage(raw, a, nil); err != nil {
		return err
	}
	if a.TypeUrl == "" {
		return fmt.Errorf("Any type URL required")
	}
	if strings.HasPrefix(a.TypeUrl, "/clairveil.privacy.v1.") {
		return fmt.Errorf("legacy privacy message is disabled")
	}
	m, err := registry.Resolve(a.TypeUrl)
	if err != nil {
		return fmt.Errorf("unregistered message: %w", err)
	}
	if strings.HasPrefix(a.TypeUrl, "/clairveil.privacy.v2.") {
		if inGovernanceProposal && isGovernanceBlockedPrivacyType(a.TypeUrl) {
			return fmt.Errorf("governance cannot execute privacy transaction %s", a.TypeUrl)
		}
		if len(a.Value) > 128<<10 && strings.HasPrefix(a.TypeUrl, "/clairveil.privacy.v2.") {
			return fmt.Errorf("privacy message too large")
		}
		return auditCanonicalMessage(a.Value, m, nil)
	}
	if a.TypeUrl == "/cosmos.gov.v1.MsgSubmitProposal" {
		return auditCanonicalMessage(a.Value, m, func(child []byte) error { return auditWireAny(child, registry, depth+1, count, true) })
	}
	// Unknown container schemas must not conceal nested execution. Ordinary
	// messages without Any fields continue through the SDK's normal decoder.
	if a.TypeUrl != "/cosmos.staking.v1beta1.MsgCreateValidator" && a.TypeUrl != "/cosmos.gov.v1.MsgExecLegacyContent" && auditContainsAny(reflect.TypeOf(m), map[reflect.Type]bool{}) {
		return fmt.Errorf("unsupported message container %s", a.TypeUrl)
	}
	return nil
}

func isGovernanceBlockedPrivacyType(typeURL string) bool {
	switch typeURL {
	case "/clairveil.privacy.v2.MsgDeposit", "/clairveil.privacy.v2.MsgWithdraw", "/clairveil.privacy.v2.MsgTransfer", "/clairveil.privacy.v2.MsgBatchTransfer":
		return true
	default:
		return false
	}
}
func auditContainsAny(t reflect.Type, seen map[reflect.Type]bool) bool {
	for t.Kind() == reflect.Ptr || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t == reflect.TypeOf(codectypes.Any{}) {
		return true
	}
	if t.Kind() != reflect.Struct || seen[t] {
		return false
	}
	seen[t] = true
	for i := 0; i < t.NumField(); i++ {
		if auditContainsAny(t.Field(i).Type, seen) {
			return true
		}
	}
	return false
}

// Generated protobuf tags provide the allowlist and nested message shapes;
// generated Marshal provides the single canonical wire implementation.
func auditCanonicalMessage(raw []byte, m proto.Message, anyVisitor func([]byte) error) error {
	t := reflect.TypeOf(m).Elem()
	fields := map[protowire.Number]reflect.StructField{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		tag := strings.Split(f.Tag.Get("protobuf"), ",")
		if len(tag) < 2 {
			continue
		}
		n, err := strconv.Atoi(tag[1])
		if err != nil {
			return err
		}
		fields[protowire.Number(n)] = f
	}
	rest := raw
	seen := map[protowire.Number]bool{}
	last := protowire.Number(0)
	for len(rest) > 0 {
		num, wt, n := protowire.ConsumeTag(rest)
		if n < 0 || n != protowire.SizeTag(num) {
			return fmt.Errorf("noncanonical protobuf tag")
		}
		f, ok := fields[num]
		if !ok {
			return fmt.Errorf("unknown protobuf field %d", num)
		}
		repeated := strings.Contains(f.Tag.Get("protobuf"), ",rep,")
		if num < last || seen[num] && !repeated {
			return fmt.Errorf("duplicate or unordered protobuf field")
		}
		seen[num] = true
		last = num
		rest = rest[n:]
		size := protowire.ConsumeFieldValue(num, wt, rest)
		if size < 0 {
			return fmt.Errorf("malformed protobuf field")
		}
		if wt == protowire.VarintType {
			v, k := protowire.ConsumeVarint(rest)
			if k != protowire.SizeVarint(v) {
				return fmt.Errorf("nonminimal protobuf integer")
			}
		}
		if wt == protowire.BytesType {
			value, k := protowire.ConsumeBytes(rest)
			if k < 0 || k-len(value) != protowire.SizeVarint(uint64(len(value))) {
				return fmt.Errorf("nonminimal protobuf length")
			}
			ft := f.Type
			for ft.Kind() == reflect.Slice || ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct && f.Tag.Get("protobuf") != "" && !strings.Contains(f.Tag.Get("protobuf"), "customtype=") {
				if ft == reflect.TypeOf(codectypes.Any{}) && anyVisitor != nil {
					if err := anyVisitor(value); err != nil {
						return err
					}
				} else {
					child, ok := reflect.New(ft).Interface().(proto.Message)
					if !ok {
						return fmt.Errorf("unsupported protobuf child")
					}
					if err := auditCanonicalMessage(value, child, anyVisitor); err != nil {
						return err
					}
				}
			}
		}
		rest = rest[size:]
	}
	if err := proto.Unmarshal(raw, m); err != nil {
		return err
	}
	encoded, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, encoded) {
		return fmt.Errorf("noncanonical protobuf encoding")
	}
	return nil
}
