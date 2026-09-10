package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Value 实现 driver.Valuer：写库时把 []string 序列化成 JSON 字节串。
func (l StringList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, fmt.Errorf("marshal StringList: %w", err)
	}
	return string(b), nil
}

// Scan 实现 sql.Scanner：读库时把 JSON 字节串反序列化回 []string。
func (l *StringList) Scan(src interface{}) error {
	if src == nil {
		*l = StringList{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("StringList.Scan: unsupported src type %T", src)
	}
	if len(b) == 0 {
		*l = StringList{}
		return nil
	}
	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Errorf("StringList.Scan: %w", err)
	}
	*l = out
	return nil
}
