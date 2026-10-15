package jsplugin

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	defaultMaxItems = 1000
	maxFieldBytes   = 8 << 10
)

// decodeInto validates value against the ts tags of target (a pointer to a
// struct) and fills it in. Unknown fields, missing required fields and wrong
// types are js_shape_error.
func decodeInto(value any, target any, path string) error {
	rv := reflect.ValueOf(target)
	if rv.Kind() != reflect.Pointer || rv.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("decodeInto needs a struct pointer, got %T", target)
	}
	return decodeStruct(value, rv.Elem(), path)
}

func decodeStruct(value any, out reflect.Value, path string) error {
	object, ok := value.(map[string]any)
	if !ok {
		return shapeError("%s: expected object", path)
	}
	typ := out.Type()
	known := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		raw, ok := field.Tag.Lookup("ts")
		if !ok || raw == "-" {
			continue
		}
		name, optional, _, _ := parseTSTag(raw)
		known[name] = true
		required := !optional && field.Type.Kind() != reflect.Pointer
		item, present := object[name]
		fieldPath := joinPath(path, name)
		if !present || item == nil {
			if required {
				return shapeError("%s: required", fieldPath)
			}
			continue
		}
		if err := decodeValue(item, out.Field(i), field, fieldPath, required); err != nil {
			return err
		}
	}
	for key := range object {
		if !known[key] {
			return shapeError("%s: unknown field %q", path, key)
		}
	}
	return nil
}

func decodeValue(item any, out reflect.Value, field reflect.StructField, path string, required bool) error {
	switch out.Kind() {
	case reflect.Pointer:
		elem := reflect.New(out.Type().Elem())
		if err := decodeValue(item, elem.Elem(), field, path, false); err != nil {
			return err
		}
		out.Set(elem)
	case reflect.String:
		text, ok := item.(string)
		if !ok {
			return shapeError("%s: expected string", path)
		}
		if required && strings.TrimSpace(text) == "" {
			return shapeError("%s: expected non-empty string", path)
		}
		if len(text) > maxFieldBytes {
			return shapeError("%s: longer than %d bytes", path, maxFieldBytes)
		}
		if enum := field.Tag.Get("enum"); enum != "" && !contains(strings.Split(enum, ","), text) {
			return shapeError("%s: %q is not one of %s", path, text, enum)
		}
		out.SetString(text)
	case reflect.Bool:
		flag, ok := item.(bool)
		if !ok {
			return shapeError("%s: expected boolean", path)
		}
		out.SetBool(flag)
	case reflect.Float32, reflect.Float64, reflect.Int, reflect.Int32, reflect.Int64:
		number, ok := asFloat(item)
		if !ok || math.IsNaN(number) || math.IsInf(number, 0) {
			return shapeError("%s: expected number", path)
		}
		if err := checkBounds(number, field, path); err != nil {
			return err
		}
		if out.Kind() >= reflect.Int && out.Kind() <= reflect.Int64 {
			if number != math.Trunc(number) {
				return shapeError("%s: expected integer", path)
			}
			out.SetInt(int64(number))
		} else {
			out.SetFloat(number)
		}
	case reflect.Slice:
		items, ok := item.([]any)
		if !ok {
			return shapeError("%s: expected array", path)
		}
		if len(items) > maxLen(field) {
			return shapeError("%s: more than %d items", path, maxLen(field))
		}
		slice := reflect.MakeSlice(out.Type(), len(items), len(items))
		for i, element := range items {
			if element == nil {
				return shapeError("%s[%d]: must not be null", path, i)
			}
			if err := decodeValue(element, slice.Index(i), field, fmt.Sprintf("%s[%d]", path, i), true); err != nil {
				return err
			}
		}
		out.Set(slice)
	case reflect.Map:
		object, ok := item.(map[string]any)
		if !ok {
			return shapeError("%s: expected object", path)
		}
		if len(object) > maxLen(field) {
			return shapeError("%s: more than %d keys", path, maxLen(field))
		}
		if encoded, err := json.Marshal(object); err != nil || len(encoded) > maxFieldBytes {
			return shapeError("%s: too large", path)
		}
		out.Set(reflect.ValueOf(object))
	case reflect.Struct:
		return decodeStruct(item, out, path)
	default:
		return fmt.Errorf("%s: unsupported contract type %s", path, out.Type())
	}
	return nil
}

func checkBounds(number float64, field reflect.StructField, path string) error {
	if raw := field.Tag.Get("min"); raw != "" {
		if bound, err := strconv.ParseFloat(raw, 64); err == nil && number < bound {
			return shapeError("%s: must be at least %s", path, raw)
		}
	}
	if raw := field.Tag.Get("max"); raw != "" {
		if bound, err := strconv.ParseFloat(raw, 64); err == nil && number > bound {
			return shapeError("%s: must be at most %s", path, raw)
		}
	}
	return nil
}

func maxLen(field reflect.StructField) int {
	if raw := field.Tag.Get("max"); raw != "" {
		if bound, err := strconv.Atoi(raw); err == nil {
			return bound
		}
	}
	return defaultMaxItems
}

func joinPath(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// encodeValue turns a contract struct into the plain map a hook receives (and
// that fixtures compare against), using the ts names. Optional fields that
// hold their zero value are left out.
func encodeValue(value any) any {
	return encodeReflect(reflect.ValueOf(value))
}

func encodeReflect(rv reflect.Value) any {
	switch rv.Kind() {
	case reflect.Invalid:
		return nil
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return encodeReflect(rv.Elem())
	case reflect.Struct:
		out := map[string]any{}
		typ := rv.Type()
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			raw, ok := field.Tag.Lookup("ts")
			if !ok || raw == "-" {
				continue
			}
			name, optional, _, _ := parseTSTag(raw)
			fv := rv.Field(i)
			if (optional || field.Type.Kind() == reflect.Pointer) && fv.IsZero() {
				continue
			}
			out[name] = encodeReflect(fv)
		}
		return out
	case reflect.Slice:
		if rv.IsNil() {
			return []any{}
		}
		out := make([]any, rv.Len())
		for i := range out {
			out[i] = encodeReflect(rv.Index(i))
		}
		return out
	case reflect.Map:
		return rv.Interface()
	default:
		return rv.Interface()
	}
}
