// Package anyx provides safe type conversions from any.
//
// Used wherever the framework receives values that originated from
// JSON / protobuf Struct / map[string]interface{}, where numeric types
// can arrive as float64, json.Number, or any of the integer widths.
package anyx

import "encoding/json"

// Int64FromAny converts v to int64 if v is any of the standard numeric
// types (including float64, which is what encoding/json produces for
// numbers by default) or json.Number. Returns (0, false) otherwise.
func Int64FromAny(v any) (int64, bool) {
	switch x := v.(type) {
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	case int:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case json.Number:
		i, err := x.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	default:
		return 0, false
	}
}
