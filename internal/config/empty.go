package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// isEmptyObject reports whether v is a JSON object whose every value is
// itself an empty object; {} is the base case. Scalars (false and "" too)
// and arrays never are (D14, D15).
func isEmptyObject(v any) bool {
	m, ok := v.(map[string]any)
	if !ok {
		return false
	}
	for _, child := range m {
		if !isEmptyObject(child) {
			return false
		}
	}
	return true
}

// pruneEmpty deletes, at every level of m, each key whose value is an empty
// object. snapd leaves `{}` behind when the last child key is unset, and an
// empty map must mean "absent": no add-on, no panel (D14).
func pruneEmpty(m map[string]any) {
	for k, v := range m {
		if isEmptyObject(v) {
			delete(m, k)
			continue
		}
		if child, ok := v.(map[string]any); ok {
			pruneEmpty(child)
		}
	}
}

// withoutEmptyObjects is data with every empty object removed (pruneEmpty).
// UseNumber keeps numbers exactly as snapctl wrote them.
func withoutEmptyObjects(data []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("unexpected data after the config document")
	}
	pruneEmpty(doc)
	return json.Marshal(doc)
}

// EmptyPaths lists, sorted, the maximal dotted paths in doc whose value is an
// empty object (isEmptyObject): when every child of a map is empty, the map's
// own path is listed and its children are not (D15).
func EmptyPaths(doc map[string]any) []string {
	var out []string
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			p := prefix + k
			if isEmptyObject(v) {
				out = append(out, p)
				continue
			}
			if child, ok := v.(map[string]any); ok {
				walk(p+".", child)
			}
		}
	}
	walk("", doc)
	sort.Strings(out)
	return out
}
