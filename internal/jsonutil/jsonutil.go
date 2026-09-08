// Package jsonutil navigates the map[string]interface{} shape produced by
// json.Unmarshal into a generic map — the form GitHub webhook payloads and
// similar raw JSON documents take before being normalized into typed
// structs. It exists so payload-field lookups aren't reimplemented
// per-package.
package jsonutil

// MapAt returns the nested object at key, or nil when absent.
func MapAt(m map[string]interface{}, key string) map[string]interface{} {
	obj, _ := m[key].(map[string]interface{})
	return obj
}

// StringAt returns the string value at key, or "" when absent or not a
// string.
func StringAt(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	s, _ := m[key].(string)
	return s
}

// NumberAt returns the numeric field at key as an int64, or 0 when absent.
// JSON-decoded payloads hold numbers as float64; native Go numeric types are
// also accepted for callers constructing payloads directly (tests).
func NumberAt(m map[string]interface{}, key string) int64 {
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}
