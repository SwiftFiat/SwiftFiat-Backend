package utils

import (
	"encoding/json"

	"github.com/sqlc-dev/pqtype"
)

// MarshalMetadata converts map[string]any → pqtype.NullRawMessage
func MarshalMetadata(meta map[string]any) pqtype.NullRawMessage {
	if meta == nil {
		return pqtype.NullRawMessage{Valid: false}
	}

	raw, err := json.Marshal(meta)
	if err != nil {
		return pqtype.NullRawMessage{Valid: false}
	}

	return pqtype.NullRawMessage{
		RawMessage: raw,
		Valid:      true,
	}
}

// UnmarshalMetadata converts pqtype.NullRawMessage → map[string]any
func UnmarshalMetadata(raw pqtype.NullRawMessage) map[string]any {
	if !raw.Valid || len(raw.RawMessage) == 0 {
		return nil
	}

	var meta map[string]any
	if err := json.Unmarshal(raw.RawMessage, &meta); err != nil {
		return nil
	}

	return meta
}
