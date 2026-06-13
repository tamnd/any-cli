package kit

import (
	"reflect"
	"time"
)

// InputSchema derives a JSON Schema object for the operation's parameters, used
// as the MCP tool inputSchema and the OpenAPI request schema. Positional args
// and flags both become properties; args and flags without a default are
// required.
func (o *op[In, Out]) InputSchema() map[string]any {
	props := map[string]any{}
	var required []string
	for _, b := range o.binds {
		if b.spec.Kind == KindInject {
			continue
		}
		p := schemaForParam(b.spec)
		props[b.spec.Name] = p
		if b.spec.Kind == KindArg && !b.spec.Variadic {
			required = append(required, b.spec.Name)
		}
	}
	schema := map[string]any{
		"type":       "object",
		"properties": props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// OutputSchema derives a JSON Schema for one emitted record, reflecting the Out
// type's exported fields by their json names.
func (o *op[In, Out]) OutputSchema() map[string]any {
	if o.outTyp == nil {
		return map[string]any{"type": "object"}
	}
	return schemaForType(o.outTyp)
}

func schemaForParam(p ParamSpec) map[string]any {
	s := map[string]any{}
	switch p.Type {
	case TypeInt:
		s["type"] = "integer"
	case TypeFloat:
		s["type"] = "number"
	case TypeBool:
		s["type"] = "boolean"
	case TypeStringSlice:
		s["type"] = "array"
		s["items"] = map[string]any{"type": "string"}
	default:
		s["type"] = "string"
		if p.Type == TypeDuration {
			s["description"] = durationNote(p.Help)
		}
	}
	if p.Help != "" && s["description"] == nil {
		s["description"] = p.Help
	}
	if len(p.Enum) > 0 {
		vals := make([]any, len(p.Enum))
		for i, e := range p.Enum {
			vals[i] = e
		}
		s["enum"] = vals
	}
	if p.Default != "" {
		s["default"] = p.Default
	}
	return s
}

func durationNote(help string) string {
	if help == "" {
		return "duration, e.g. 500ms, 2s, 1m"
	}
	return help + " (duration, e.g. 500ms, 2s, 1m)"
}

var timeType = reflect.TypeFor[time.Time]()

func schemaForType(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t == timeType {
			return map[string]any{"type": "string", "format": "date-time"}
		}
		props := map[string]any{}
		for f := range t.Fields() {
			if !f.IsExported() {
				continue
			}
			name := jsonNameOf(f)
			if name == "-" {
				continue
			}
			props[name] = schemaForType(f.Type)
		}
		return map[string]any{"type": "object", "properties": props}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string"}
		}
		return map[string]any{"type": "array", "items": schemaForType(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	default:
		return map[string]any{"type": "string"}
	}
}
