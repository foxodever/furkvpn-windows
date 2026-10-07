// Package jsonx has small helpers for working with Xray configs as generic JSON
// (map[string]any / []any), where only a few fields matter and the rest must pass
// through untouched.
package jsonx

import (
	"bytes"
	"encoding/json"
	"errors"
)

// Object is a JSON object decoded with numbers kept as json.Number, so that values pass
// through unchanged.
type Object = map[string]any

// Parse decodes JSON keeping numbers as json.Number.
func Parse(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// ParseObject decodes a JSON object; anything else is an error.
func ParseObject(data []byte) (Object, error) {
	v, err := Parse(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(Object)
	if !ok {
		return nil, errors.New("not a JSON object")
	}
	return obj, nil
}

// Marshal encodes without escaping <, > and &, which Xray configs may contain in patterns.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Obj returns v as an object, or nil.
func Obj(v any) Object {
	o, _ := v.(Object)
	return o
}

// Arr returns v as an array, or nil.
func Arr(v any) []any {
	a, _ := v.([]any)
	return a
}

// Str returns the field as a string the way a JSON primitive's content reads: strings as they
// are, numbers and booleans in their JSON form; "" and false when missing or not a primitive.
func Str(o Object, key string) (string, bool) {
	switch v := o[key].(type) {
	case string:
		return v, true
	case json.Number:
		return v.String(), true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	}
	return "", false
}

// S is Str without the flag.
func S(o Object, key string) string {
	s, _ := Str(o, key)
	return s
}

// Objects returns the objects of an array, skipping anything else.
func Objects(v any) []Object {
	arr := Arr(v)
	out := make([]Object, 0, len(arr))
	for _, item := range arr {
		if o, ok := item.(Object); ok {
			out = append(out, o)
		}
	}
	return out
}

// Strings returns the string items of an array.
func Strings(v any) []string {
	arr := Arr(v)
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		switch s := item.(type) {
		case string:
			out = append(out, s)
		case json.Number:
			out = append(out, s.String())
		}
	}
	return out
}

// Clone makes a shallow copy of an object.
func Clone(o Object) Object {
	out := make(Object, len(o)+4)
	for k, v := range o {
		out[k] = v
	}
	return out
}
