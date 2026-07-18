package tracker

import (
	"fmt"
	"math"
	"reflect"
	"unicode/utf8"
)

const (
	schemaMaxDepth      = 10
	schemaMaxObjectKeys = 50
	schemaMaxArrayItems = 5
	schemaMaxStringLen  = 500
)

// ExtractSchemaForArgs builds JS-compatible paramsSchema: { arg0: ..., arg1: ... }.
func ExtractSchemaForArgs(args []interface{}) map[string]interface{} {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(args))
	for i, arg := range args {
		out[fmt.Sprintf("arg%d", i)] = ExtractSchema(arg, "", 0, nil)
	}
	return out
}

// ExtractSchema builds a recursive type schema matching the JS agent extractSchema shape.
//
// Objects are a flat map of field → { type, path [, properties] } (NOT wrapped in
// { type: "object", properties: … }). That legacy shape is what the Console policy
// builder flattens into the response-field dropdown.
func ExtractSchema(obj interface{}, path string, depth int, seen map[uintptr]bool) interface{} {
	if obj == nil {
		return "null"
	}
	if depth > schemaMaxDepth {
		return map[string]interface{}{"type": "truncated", "reason": "max_depth"}
	}
	if seen == nil {
		seen = map[uintptr]bool{}
	}

	rv := reflect.ValueOf(obj)
	for rv.Kind() == reflect.Interface || rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return "null"
		}
		if rv.Kind() == reflect.Pointer {
			ptr := rv.Pointer()
			if seen[ptr] {
				return map[string]interface{}{"type": "object", "circular": true}
			}
			seen[ptr] = true
		}
		rv = rv.Elem()
	}

	switch rv.Kind() {
	case reflect.Bool:
		return "boolean"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return "integer"
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if math.Trunc(f) == f {
			return "integer"
		}
		return "float"
	case reflect.String:
		s := rv.String()
		if utf8.RuneCountInString(s) > schemaMaxStringLen {
			return map[string]interface{}{
				"type":      "string",
				"truncated": true,
				"length":    len(s),
			}
		}
		return "string"
	case reflect.Slice, reflect.Array:
		length := rv.Len()
		if length == 0 {
			return map[string]interface{}{"type": "array", "items": "unknown"}
		}
		limit := length
		if limit > schemaMaxArrayItems {
			limit = schemaMaxArrayItems
		}
		items := make([]interface{}, 0, limit)
		for i := 0; i < limit; i++ {
			itemPath := fmt.Sprintf("%s[%d]", path, i)
			items = append(items, ExtractSchema(rv.Index(i).Interface(), itemPath, depth+1, seen))
		}
		var itemsOut interface{} = items
		if len(items) == 1 {
			itemsOut = items[0]
		}
		return map[string]interface{}{
			"type":   "array",
			"items":  itemsOut,
			"length": length,
		}
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return map[string]interface{}{"type": "object", "path": path}
		}
		schema := map[string]interface{}{}
		keys := rv.MapKeys()
		count := 0
		for _, key := range keys {
			if count >= schemaMaxObjectKeys {
				schema["_truncated"] = map[string]interface{}{"keys": rv.Len()}
				break
			}
			k := key.String()
			fieldPath := joinPath(path, k)
			schema[k] = fieldSchema(rv.MapIndex(key), fieldPath, depth, seen)
			count++
		}
		return schema
	case reflect.Struct:
		schema := map[string]interface{}{}
		rt := rv.Type()
		count := 0
		for i := 0; i < rt.NumField(); i++ {
			if count >= schemaMaxObjectKeys {
				schema["_truncated"] = map[string]interface{}{"keys": rt.NumField()}
				break
			}
			field := rt.Field(i)
			if field.PkgPath != "" {
				continue
			}
			name := jsonFieldName(field)
			if name == "-" {
				continue
			}
			fieldPath := joinPath(path, name)
			schema[name] = fieldSchema(rv.Field(i), fieldPath, depth, seen)
			count++
		}
		return schema
	default:
		return reflect.TypeOf(obj).String()
	}
}

// NormalizeResultSchema adapts bare primitive schemas (e.g. estimateTokens → "integer")
// into a legacy field map so the Console response-field dropdown has something to pick.
// Path "return" is treated as the scalar itself when metering.
func NormalizeResultSchema(schema interface{}) interface{} {
	s, ok := schema.(string)
	if !ok {
		return schema
	}
	switch s {
	case "integer", "float", "number", "boolean", "string", "null":
		typ := s
		if s == "float" {
			typ = "number"
		}
		return map[string]interface{}{
			"return": map[string]interface{}{
				"type": typ,
				"path": "return",
			},
		}
	default:
		return schema
	}
}

// fieldSchema matches JS object-field entries: primitives are {type,path};
// nested values also include properties: extractSchema(...).
func fieldSchema(fv reflect.Value, fieldPath string, depth int, seen map[uintptr]bool) map[string]interface{} {
	for fv.Kind() == reflect.Interface || fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return map[string]interface{}{"type": "null", "path": fieldPath}
		}
		fv = fv.Elem()
	}
	switch fv.Kind() {
	case reflect.Invalid:
		return map[string]interface{}{"type": "null", "path": fieldPath}
	case reflect.Bool:
		return map[string]interface{}{"type": "boolean", "path": fieldPath}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		// JS typeof number → "number" (Console meters both integer and number).
		return map[string]interface{}{"type": "number", "path": fieldPath}
	case reflect.String:
		return map[string]interface{}{"type": "string", "path": fieldPath}
	case reflect.Slice, reflect.Array:
		return map[string]interface{}{
			"type":       "array",
			"path":       fieldPath,
			"properties": ExtractSchema(fv.Interface(), fieldPath, depth+1, seen),
		}
	case reflect.Map, reflect.Struct:
		return map[string]interface{}{
			"type":       "object",
			"path":       fieldPath,
			"properties": ExtractSchema(fv.Interface(), fieldPath, depth+1, seen),
		}
	default:
		return map[string]interface{}{"type": "unknown", "path": fieldPath}
	}
}

func joinPath(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
}

func jsonFieldName(f reflect.StructField) string {
	tag := f.Tag.Get("json")
	if tag == "" {
		return f.Name
	}
	if tag == "-" {
		return "-"
	}
	name := tag
	if i := indexByte(tag, ','); i >= 0 {
		name = tag[:i]
	}
	if name == "" {
		return f.Name
	}
	return name
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
