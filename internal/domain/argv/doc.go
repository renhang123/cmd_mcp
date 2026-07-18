package argv

import (
	"fmt"
	"sort"
	"strings"

	"server-shell-mcp/internal/domain/command"
	"server-shell-mcp/internal/domain/validation"
)

type ExecutionSpec struct {
	CommandID        string
	Executable       string
	Argv             []string
	WorkingDirectory string
	Environment      map[string]string
}

type Error struct {
	Message string
}

func (e Error) Error() string {
	return e.Message
}

type Builder struct{}

func NewBuilder() *Builder {
	return &Builder{}
}

func (b *Builder) Build(def command.CommandDefinition, args validation.NormalizedArguments) (ExecutionSpec, error) {
	if containsShellSyntax(def.Executable) {
		return ExecutionSpec{}, Error{Message: "executable contains shell syntax"}
	}
	argv := make([]string, 0, len(def.ArgvTemplate))
	for _, part := range def.ArgvTemplate {
		values, err := renderPart(part, args)
		if err != nil {
			return ExecutionSpec{}, err
		}
		for _, value := range values {
			if containsShellSyntax(value) {
				return ExecutionSpec{}, Error{Message: "argv contains shell syntax"}
			}
			argv = append(argv, value)
		}
	}
	return ExecutionSpec{
		CommandID:        def.ID,
		Executable:       def.Executable,
		Argv:             argv,
		WorkingDirectory: def.WorkingDirectory,
		Environment:      cloneSortedEnv(def.Environment.Variables),
	}, nil
}

func renderPart(part command.ArgvTemplatePart, args validation.NormalizedArguments) ([]string, error) {
	if part.Literal != "" {
		return []string{part.Literal}, nil
	}
	if part.Param != "" {
		value, ok := args[part.Param]
		if !ok {
			return nil, Error{Message: fmt.Sprintf("missing normalized argument: %s", part.Param)}
		}
		return []string{stringValue(value.Value)}, nil
	}
	if part.Flag != nil {
		value, ok := args[part.Flag.When.Param]
		if !ok || value.Value != part.Flag.When.Equals {
			return nil, nil
		}
		return []string{part.Flag.Value}, nil
	}
	if part.Repeat != nil {
		value, ok := args[part.Repeat.Param]
		if !ok {
			return nil, nil
		}
		items, ok := value.Value.([]interface{})
		if !ok {
			return nil, Error{Message: fmt.Sprintf("repeat argument is not an array: %s", part.Repeat.Param)}
		}
		values := make([]string, 0, len(items)*2)
		for _, item := range items {
			if part.Repeat.Prefix != "" {
				values = append(values, part.Repeat.Prefix)
			}
			values = append(values, stringValue(item))
		}
		return values, nil
	}
	return nil, Error{Message: "empty argv template part"}
}

func stringValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func cloneSortedEnv(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	cloned := make(map[string]string, len(env))
	for _, key := range keys {
		cloned[key] = env[key]
	}
	return cloned
}

func containsShellSyntax(value string) bool {
	for _, token := range []string{"|", ">", "<", ";", "&&", "||", "$(", "`", "&"} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}
