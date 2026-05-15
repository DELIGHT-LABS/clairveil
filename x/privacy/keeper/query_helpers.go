package keeper

import (
	"encoding/hex"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func invalidQueryRequestErr() error {
	return status.Error(codes.InvalidArgument, "query request is required")
}

func decodeHexQueryArg(value, invalidMessage string) ([]byte, error) {
	b, err := hex.DecodeString(value)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, invalidMessage)
	}

	return b, nil
}
