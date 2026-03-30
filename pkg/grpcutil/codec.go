package grpcutil

import (
	"encoding/json"
	"fmt"

	"google.golang.org/grpc/encoding"
)

// CodecName is the gRPC codec name registered for JSON marshal/unmarshal of
// request and response payloads.
const CodecName = "json"

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("grpcutil: json marshal: %w", err)
	}
	return b, nil
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("grpcutil: json unmarshal: %w", err)
	}
	return nil
}

func (jsonCodec) Name() string {
	return CodecName
}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}
