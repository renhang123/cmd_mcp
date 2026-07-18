package validation

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"server-shell-mcp/internal/domain/command"
)

type NormalizedArguments map[string]Value

type Value struct {
	Type   command.ParameterType
	Value  interface{}
	Redact bool
}

type FieldError struct {
	Field   string
	Code    string
	Message string
}

type Error struct {
	Fields []FieldError
}

func (e Error) Error() string {
	if len(e.Fields) == 0 {
		return "validation failed"
	}
	return fmt.Sprintf("validation failed: %s: %s", e.Fields[0].Field, e.Fields[0].Message)
}

type Validator struct{}

func NewValidator() *Validator {
	return &Validator{}
}

func (v *Validator) Validate(def command.CommandDefinition, raw map[string]interface{}) (NormalizedArguments, error) {
	var fields []FieldError
	for name := range raw {
		if _, ok := def.Parameters[name]; !ok {
			fields = append(fields, FieldError{Field: name, Code: "parameter.unknown", Message: "unknown argument"})
		}
	}
	if len(fields) > 0 {
		return nil, Error{Fields: fields}
	}

	normalized := NormalizedArguments{}
	for name, param := range def.Parameters {
		value, ok := raw[name]
		if !ok {
			if param.Required {
				fields = append(fields, FieldError{Field: name, Code: "parameter.required", Message: "missing required argument"})
				continue
			}
			if param.Default == nil {
				continue
			}
			value = param.Default
		}
		normal, err := normalizeValue(name, param, value)
		if err != nil {
			fields = append(fields, *err)
			continue
		}
		normalized[name] = normal
	}
	if len(fields) > 0 {
		return nil, Error{Fields: fields}
	}
	return normalized, nil
}

func normalizeValue(name string, param command.ParameterDefinition, raw interface{}) (Value, *FieldError) {
	switch param.Type {
	case command.ParameterTypeEnum:
		value, ok := raw.(string)
		if !ok {
			return Value{}, fieldType(name)
		}
		if !contains(param.EnumValues, value) {
			return Value{}, &FieldError{Field: name, Code: "enum.not_allowed", Message: "value is not allowed"}
		}
		return valueOf(param, value), nil
	case command.ParameterTypeBool:
		value, ok := raw.(bool)
		if !ok {
			return Value{}, fieldType(name)
		}
		return valueOf(param, value), nil
	case command.ParameterTypeInt:
		value, ok := intValue(raw)
		if !ok {
			return Value{}, fieldType(name)
		}
		if param.Min != nil && value < *param.Min {
			return Value{}, &FieldError{Field: name, Code: "int.too_small", Message: "value is below minimum"}
		}
		if param.Max != nil && value > *param.Max {
			return Value{}, &FieldError{Field: name, Code: "int.too_large", Message: "value is above maximum"}
		}
		return valueOf(param, value), nil
	case command.ParameterTypeString:
		value, ok := raw.(string)
		if !ok {
			return Value{}, fieldType(name)
		}
		if param.MinLength != nil && len(value) < *param.MinLength {
			return Value{}, &FieldError{Field: name, Code: "string.too_short", Message: "value is below minimum length"}
		}
		if param.MaxLength != nil && len(value) > *param.MaxLength {
			return Value{}, &FieldError{Field: name, Code: "string.too_long", Message: "value is above maximum length"}
		}
		if param.Pattern != "" {
			matched, err := regexp.MatchString(param.Pattern, value)
			if err != nil || !matched {
				return Value{}, &FieldError{Field: name, Code: "string.pattern", Message: "value format is invalid"}
			}
		}
		if param.AllowlistPattern != "" {
			matched, err := regexp.MatchString(param.AllowlistPattern, value)
			if err != nil || !matched {
				return Value{}, &FieldError{Field: name, Code: "string.not_allowlisted", Message: "value is not allowlisted"}
			}
		}
		return valueOf(param, value), nil
	case command.ParameterTypePath:
		value, ok := raw.(string)
		if !ok {
			return Value{}, fieldType(name)
		}
		normalized, ok := normalizePath(value, param.AllowedRoots)
		if !ok {
			return Value{}, &FieldError{Field: name, Code: "path.outside_root", Message: "path is outside allowed root"}
		}
		return valueOf(param, normalized), nil
	case command.ParameterTypeArray:
		values, ok := raw.([]interface{})
		if !ok {
			return Value{}, fieldType(name)
		}
		if param.MaxItems != nil && len(values) > *param.MaxItems {
			return Value{}, &FieldError{Field: name, Code: "array.too_many", Message: "too many values"}
		}
		if param.ArrayItem == nil {
			return Value{}, &FieldError{Field: name, Code: "array.item_missing", Message: "array item schema is missing"}
		}
		normalized := make([]interface{}, 0, len(values))
		for _, item := range values {
			itemValue, err := normalizeValue(name, *param.ArrayItem, item)
			if err != nil {
				return Value{}, err
			}
			normalized = append(normalized, itemValue.Value)
		}
		return valueOf(param, normalized), nil
	default:
		return Value{}, &FieldError{Field: name, Code: "parameter.type", Message: "unknown parameter type"}
	}
}

func fieldType(name string) *FieldError {
	return &FieldError{Field: name, Code: "parameter.type", Message: "invalid argument type"}
}

func valueOf(param command.ParameterDefinition, value interface{}) Value {
	return Value{Type: param.Type, Value: value, Redact: param.Audit.Redact}
}

func intValue(raw interface{}) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		if value != float64(int(value)) {
			return 0, false
		}
		return int(value), true
	default:
		return 0, false
	}
}

func normalizePath(value string, roots []string) (string, bool) {
	if value == "" {
		return "", false
	}
	abs, err := filepath.Abs(value)
	if err != nil {
		return "", false
	}
	clean := filepath.Clean(abs)
	for _, root := range roots {
		rootAbs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rootClean := filepath.Clean(rootAbs)
		if clean == rootClean || strings.HasPrefix(clean, rootClean+string(filepath.Separator)) {
			return clean, true
		}
	}
	return "", false
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
