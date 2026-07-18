package command

import "time"

type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

type AccessMode string

const (
	AccessModeReadOnly      AccessMode = "read_only"
	AccessModeStateChanging AccessMode = "state_changing"
)

type ParameterType string

const (
	ParameterTypeEnum   ParameterType = "enum"
	ParameterTypeBool   ParameterType = "bool"
	ParameterTypeInt    ParameterType = "int"
	ParameterTypeString ParameterType = "string"
	ParameterTypePath   ParameterType = "path"
	ParameterTypeArray  ParameterType = "array"
)

type OutputFormat string

const (
	OutputFormatText OutputFormat = "text"
	OutputFormatJSON OutputFormat = "json"
	OutputFormatNone OutputFormat = "none"
)

type CommandDefinition struct {
	ID               string
	Description      string
	Enabled          bool
	RiskLevel        RiskLevel
	Access           AccessMode
	Executable       string
	ArgvTemplate     []ArgvTemplatePart
	WorkingDirectory string
	Environment      EnvironmentPolicy
	Parameters       map[string]ParameterDefinition
	Execution        ExecutionPolicy
	Output           OutputPolicy
	Audit            AuditPolicy
}

type ArgvTemplatePart struct {
	Literal string
	Param   string
	Flag    *ConditionalFlag
	Repeat  *RepeatParameter
}

type ConditionalFlag struct {
	Value string
	When  ParameterCondition
}

type RepeatParameter struct {
	Param  string
	Prefix string
}

type ParameterCondition struct {
	Param  string
	Equals interface{}
}

type ParameterDefinition struct {
	Type             ParameterType
	Required         bool
	Default          interface{}
	Description      string
	Audit            ParameterAuditPolicy
	EnumValues       []string
	Min              *int
	Max              *int
	MinLength        *int
	MaxLength        *int
	Pattern          string
	AllowlistPattern string
	AllowedRoots     []string
	PathMode         PathMode
	MustExist        bool
	ArrayItem        *ParameterDefinition
	MaxItems         *int
}

type PathMode string

const (
	PathModeReadOnly PathMode = "read_only"
)

type ParameterAuditPolicy struct {
	Redact bool
}

type EnvironmentPolicy struct {
	Mode      EnvironmentMode
	Variables map[string]string
}

type EnvironmentMode string

const (
	EnvironmentModeMinimal EnvironmentMode = "minimal"
)

type ExecutionPolicy struct {
	Timeout         time.Duration
	MaxOutputBytes  int
	PerCommandLimit int
	GlobalLimit     int
}

type OutputPolicy struct {
	Stdout            OutputFormat
	Stderr            OutputFormat
	Truncate          bool
	SensitivePatterns []string
}

type AuditPolicy struct {
	EventName        string
	RedactParameters []string
	IncludeStdout    bool
	IncludeStderr    bool
	IncludeRejection bool
}
