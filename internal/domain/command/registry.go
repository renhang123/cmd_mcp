package command

import "fmt"

type Registry struct {
	commands map[string]CommandDefinition
}

type Summary struct {
	ID          string
	Description string
	RiskLevel   RiskLevel
	Access      AccessMode
	Enabled     bool
}

type NotFoundError struct {
	ID string
}

func (e NotFoundError) Error() string {
	return fmt.Sprintf("command not found: %s", e.ID)
}

func NewRegistry(defs []CommandDefinition) *Registry {
	commands := make(map[string]CommandDefinition, len(defs))
	for _, def := range defs {
		commands[def.ID] = cloneDefinition(def)
	}
	return &Registry{commands: commands}
}

func (r *Registry) Get(id string) (CommandDefinition, error) {
	def, ok := r.commands[id]
	if !ok {
		return CommandDefinition{}, NotFoundError{ID: id}
	}
	return cloneDefinition(def), nil
}

func (r *Registry) List() []Summary {
	summaries := make([]Summary, 0, len(r.commands))
	for _, def := range r.commands {
		summaries = append(summaries, Summary{
			ID:          def.ID,
			Description: def.Description,
			RiskLevel:   def.RiskLevel,
			Access:      def.Access,
			Enabled:     def.Enabled,
		})
	}
	return summaries
}

func cloneDefinition(def CommandDefinition) CommandDefinition {
	def.ArgvTemplate = append([]ArgvTemplatePart(nil), def.ArgvTemplate...)
	def.Parameters = cloneParameters(def.Parameters)
	def.Environment.Variables = cloneStringMap(def.Environment.Variables)
	def.Output.SensitivePatterns = append([]string(nil), def.Output.SensitivePatterns...)
	def.Audit.RedactParameters = append([]string(nil), def.Audit.RedactParameters...)
	return def
}

func cloneParameters(params map[string]ParameterDefinition) map[string]ParameterDefinition {
	if params == nil {
		return nil
	}
	cloned := make(map[string]ParameterDefinition, len(params))
	for name, param := range params {
		param.EnumValues = append([]string(nil), param.EnumValues...)
		param.AllowedRoots = append([]string(nil), param.AllowedRoots...)
		if param.ArrayItem != nil {
			item := *param.ArrayItem
			item.EnumValues = append([]string(nil), item.EnumValues...)
			item.AllowedRoots = append([]string(nil), item.AllowedRoots...)
			param.ArrayItem = &item
		}
		cloned[name] = param
	}
	return cloned
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}
	return cloned
}
