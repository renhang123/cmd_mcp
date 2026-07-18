package validation

import (
	"testing"

	"server-shell-mcp/internal/domain/command"
)

func TestValidatorRejectsUnknownParameter(t *testing.T) {
	_, err := NewValidator().Validate(testCommand(), map[string]interface{}{"extra": "value"})
	assertValidationCode(t, err, "parameter.unknown")
}

func TestValidatorRejectsInvalidEnum(t *testing.T) {
	_, err := NewValidator().Validate(testCommand(), map[string]interface{}{"service_name": "db"})
	assertValidationCode(t, err, "enum.not_allowed")
}

func TestValidatorRejectsTooLongString(t *testing.T) {
	_, err := NewValidator().Validate(testCommand(), map[string]interface{}{"hostname": "this-name-is-too-long.internal"})
	assertValidationCode(t, err, "string.too_long")
}

func TestValidatorRejectsPathTraversal(t *testing.T) {
	_, err := NewValidator().Validate(testCommand(), map[string]interface{}{"log_path": "/tmp/app/../secret"})
	assertValidationCode(t, err, "path.outside_root")
}

func TestValidatorRejectsInjectionCharactersByPattern(t *testing.T) {
	_, err := NewValidator().Validate(testCommand(), map[string]interface{}{"hostname": "bad;host"})
	assertValidationCode(t, err, "string.pattern")
}

func TestValidatorReturnsNoArgumentsOnFailure(t *testing.T) {
	args, err := NewValidator().Validate(testCommand(), map[string]interface{}{"service_name": "db"})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if args != nil {
		t.Fatalf("expected nil normalized arguments on failure, got %#v", args)
	}
}

func TestValidatorNormalizesValidArguments(t *testing.T) {
	args, err := NewValidator().Validate(testCommand(), map[string]interface{}{
		"service_name": "app",
		"limit":        float64(5),
		"hostname":     "api.example.internal",
		"log_path":     "/tmp/app/service.log",
	})
	if err != nil {
		t.Fatalf("expected valid arguments, got %v", err)
	}
	if args["limit"].Value != 5 {
		t.Fatalf("expected normalized integer, got %#v", args["limit"].Value)
	}
	if args["log_path"].Redact != true {
		t.Fatal("expected path argument to be marked redacted")
	}
}

func testCommand() command.CommandDefinition {
	min := 1
	max := 10
	maxLen := 20
	return command.CommandDefinition{
		ID: "test",
		Parameters: map[string]command.ParameterDefinition{
			"service_name": {
				Type:       command.ParameterTypeEnum,
				Required:   false,
				EnumValues: []string{"app", "nginx"},
			},
			"limit": {
				Type: command.ParameterTypeInt,
				Min:  &min,
				Max:  &max,
			},
			"hostname": {
				Type:      command.ParameterTypeString,
				MaxLength: &maxLen,
				Pattern:   "^[a-zA-Z0-9.-]+$",
			},
			"log_path": {
				Type:         command.ParameterTypePath,
				AllowedRoots: []string{"/tmp/app"},
				Audit:        command.ParameterAuditPolicy{Redact: true},
			},
		},
	}
}

func assertValidationCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error %s", code)
	}
	validationErr, ok := err.(Error)
	if !ok {
		t.Fatalf("expected validation Error, got %T", err)
	}
	for _, field := range validationErr.Fields {
		if field.Code == code {
			return
		}
	}
	t.Fatalf("expected code %s in %#v", code, validationErr.Fields)
}
