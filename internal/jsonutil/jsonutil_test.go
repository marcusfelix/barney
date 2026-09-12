package jsonutil

import "testing"

func TestMapAt(t *testing.T) {
	m := map[string]interface{}{"repo": map[string]interface{}{"name": "demo"}}
	if got := MapAt(m, "repo")["name"]; got != "demo" {
		t.Errorf("MapAt(m, \"repo\")[\"name\"] = %v, want demo", got)
	}
	if got := MapAt(m, "missing"); got != nil {
		t.Errorf("MapAt(m, \"missing\") = %v, want nil", got)
	}
	if got := MapAt(nil, "x"); got != nil {
		t.Errorf("MapAt(nil, \"x\") = %v, want nil", got)
	}
}

func TestStringAt(t *testing.T) {
	m := map[string]interface{}{"name": "demo", "count": 3}
	if got := StringAt(m, "name"); got != "demo" {
		t.Errorf("StringAt(m, \"name\") = %q, want demo", got)
	}
	if got := StringAt(m, "count"); got != "" {
		t.Errorf("StringAt(m, \"count\") = %q, want \"\" (not a string)", got)
	}
	if got := StringAt(nil, "name"); got != "" {
		t.Errorf("StringAt(nil, \"name\") = %q, want \"\"", got)
	}
}

func TestNumberAt(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
		want int64
	}{
		{"float64 from JSON", float64(42), 42},
		{"native int", int(7), 7},
		{"native int64", int64(99), 99},
		{"missing key", nil, 0},
		{"wrong type", "not a number", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := map[string]interface{}{}
			if tc.val != nil {
				m["n"] = tc.val
			}
			if got := NumberAt(m, "n"); got != tc.want {
				t.Errorf("NumberAt() = %d, want %d", got, tc.want)
			}
		})
	}
}
