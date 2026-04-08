package dbutil

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONStringSlice adapts []string for MySQL JSON columns.
type JSONStringSlice []string

// Value implements driver.Valuer.
func (s JSONStringSlice) Value() (driver.Value, error) {
	if s == nil {
		return "[]", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("dbutil: marshal JSONStringSlice: %w", err)
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (s *JSONStringSlice) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("dbutil: cannot scan %T into JSONStringSlice", src)
	}
	return json.Unmarshal(data, s)
}

// JSONMap adapts map[string]string for MySQL JSON columns.
type JSONMap map[string]string

// Value implements driver.Valuer.
func (m JSONMap) Value() (driver.Value, error) {
	if m == nil {
		return "{}", nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("dbutil: marshal JSONMap: %w", err)
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (m *JSONMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("dbutil: cannot scan %T into JSONMap", src)
	}
	return json.Unmarshal(data, m)
}
