package models

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/SwiftFiat/SwiftFiat-Backend/utils"
	"github.com/speps/go-hashids/v2"
)

type ID int64

var (
	hd        = hashids.NewData()
	dbHash, _ = hashids.NewWithData(hd)
)

func init() {
	c, err := utils.LoadConfig(utils.EnvPath)
	if err != nil {
		panic(fmt.Errorf("Could not load config: %v", err))
	}
	hd.Salt = c.SigningKey
	hd.MinLength = 32
	dbHash, err = hashids.NewWithData(hd)
	if err != nil {
		panic(err)
	}
}

// MarshalJSON implements the encoding json interface.
func (id ID) MarshalJSON() ([]byte, error) {
	if id == 0 {
		return json.Marshal(nil)
	}
	result, err := dbHash.EncodeInt64([]int64{int64(id)})
	if err != nil {
		return nil, err
	}
	return json.Marshal(result)
}

// UnmarshalJSON implements the encoding json interface.
func (id *ID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		*id = 0
		return nil
	}
	result, err := dbHash.DecodeInt64WithError(s)
	if err != nil {
		return err
	}
	if len(result) == 0 {
		return errors.New("invalid ID")
	}
	*id = ID(result[0])
	return nil
}

// Scan implements the Scanner interface.
func (id *ID) Scan(value interface{}) error {
	if value == nil {
		*id = 0
		return nil
	}

	switch v := value.(type) {
	case int64:
		*id = ID(v)
	case []byte:
		return id.UnmarshalJSON(v)
	default:
		return errors.New("unexpected type for ID")
	}
	return nil
}

// Value implements the driver Valuer interface.
func (id ID) Value() (driver.Value, error) {
	return int64(id), nil
}
