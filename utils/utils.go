package utils

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
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

func NewTxRef(prefix string) string {
	return fmt.Sprintf("%s-%s-%d", prefix, uuid.New().String()[:8], time.Now().Unix())
}

func ToDecimal(value string) (decimal.Decimal, error) {
	d, err := decimal.NewFromString(value)
	if err != nil {
		return decimal.Zero, err
	}
	return d, nil
}


