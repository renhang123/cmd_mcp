package argv

import (
	"reflect"
	"testing"

	"server-shell-mcp/internal/domain/command"
	"server-shell-mcp/internal/domain/validation"
)

func TestBuilderUsesFixedExecutable(t *testing.T) {
	spec, err := NewBuilder().Build(testCommand(), testArgs())
	if err != nil {
		t.Fatalf("expected build success, got %v", err)
	}
	if spec.Executable != "/usr/bin/systemctl" {
		t.Fatalf("expected fixed executable, got %s", spec.Executable)
	}
}

func TestBuilderOutputIsDeterministic(t *testing.T) {
	builder := NewBuilder()
	first, err := builder.Build(testCommand(), testArgs())
	if err != nil {
		t.Fatalf("expected first build success, got %v", err)
	}
	second, err := builder.Build(testCommand(), testArgs())
	if err != nil {
		t.Fatalf("expected second build success, got %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("expected deterministic output, got %#v and %#v", first, second)
	}
}

func TestBuilderRejectsShellSyntaxInLiteral(t *testing.T) {
	def := testCommand()
	def.ArgvTemplate = []command.ArgvTemplatePart{{Literal: "status;rm"}}
	_, err := NewBuilder().Build(def, testArgs())
	if err == nil {
		t.Fatal("expected shell syntax rejection")
	}
}

func TestBuilderRejectsShellSyntaxInArgumentValue(t *testing.T) {
	args := testArgs()
	args["service_name"] = validation.Value{Type: command.ParameterTypeEnum, Value: "app|cat"}
	_, err := NewBuilder().Build(testCommand(), args)
	if err == nil {
		t.Fatal("expected shell syntax rejection")
	}
}

func TestBuilderRejectsShellSyntaxInExecutable(t *testing.T) {
	def := testCommand()
	def.Executable = "/usr/bin/systemctl;sh"
	_, err := NewBuilder().Build(def, testArgs())
	if err == nil {
		t.Fatal("expected executable shell syntax rejection")
	}
}

func testCommand() command.CommandDefinition {
	return command.CommandDefinition{
		ID:               "service_status_check",
		Executable:       "/usr/bin/systemctl",
		WorkingDirectory: "/",
		Environment: command.EnvironmentPolicy{Variables: map[string]string{
			"LANG": "C",
		}},
		ArgvTemplate: []command.ArgvTemplatePart{
			{Literal: "status"},
			{Param: "service_name"},
			{Flag: &command.ConditionalFlag{Value: "--no-pager", When: command.ParameterCondition{Param: "no_pager", Equals: true}}},
		},
	}
}

func testArgs() validation.NormalizedArguments {
	return validation.NormalizedArguments{
		"service_name": {Type: command.ParameterTypeEnum, Value: "app"},
		"no_pager":     {Type: command.ParameterTypeBool, Value: true},
	}
}
