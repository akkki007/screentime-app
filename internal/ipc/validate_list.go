package ipc

import (
	"encoding/json"
	"fmt"
)

// OptEnum reads an optional string that must be one of values.
func (o *object) OptEnum(key string, values ...string) *string {
	if !o.Has(key) {
		o.seen[key] = true
		return nil
	}
	v, ok := o.Enum(key, values...)
	if !ok {
		return nil
	}
	return &v
}

// OptStringList reads an optional array of at most maxItems strings, each 1
// to maxLen characters long.
func (o *object) OptStringList(key string, maxItems, maxLen int) []string {
	raw, present := o.raw(key)
	if !present {
		return nil
	}
	var items []json.RawMessage
	if jsonType(raw) != "array" || json.Unmarshal(raw, &items) != nil {
		o.fail(key, "Expected array, received "+jsonType(raw))
		return nil
	}
	if len(items) > maxItems {
		o.fail(key, fmt.Sprintf("Array must contain at most %d element(s)", maxItems))
		return nil
	}
	out := make([]string, 0, len(items))
	for i, item := range items {
		path := fmt.Sprintf("%s.%d", key, i)
		var v string
		if jsonType(item) != "string" || json.Unmarshal(item, &v) != nil {
			o.fail(path, "Expected string, received "+jsonType(item))
			continue
		}
		if v == "" || utf16Len(v) > maxLen {
			o.fail(path, fmt.Sprintf("String must contain between 1 and %d character(s)", maxLen))
			continue
		}
		out = append(out, v)
	}
	return out
}
