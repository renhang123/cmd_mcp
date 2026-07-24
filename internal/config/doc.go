package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"server-shell-mcp/internal/artifact"
	"server-shell-mcp/internal/domain/command"
)

type File struct {
	Version        int                      `json:"version"`
	Commands       []CommandConfig          `json:"commands"`
	ArtifactStore  artifact.StoreConfig     `json:"artifact_store"`
	DeployProfiles []artifact.DeployProfile `json:"deploy_profiles"`
}

type RuntimeConfig struct {
	Commands       []command.CommandDefinition
	ArtifactStore  artifact.StoreConfig
	DeployProfiles []artifact.DeployProfile
}

type CommandConfig struct {
	ID               string                               `json:"id"`
	Description      string                               `json:"description"`
	Enabled          bool                                 `json:"enabled"`
	RiskLevel        string                               `json:"risk_level"`
	Access           string                               `json:"access"`
	Executable       string                               `json:"executable"`
	ArgvTemplate     []ArgvTemplatePartConfig             `json:"argv_template"`
	WorkingDirectory string                               `json:"working_directory"`
	Environment      EnvironmentPolicyConfig              `json:"environment"`
	Parameters       map[string]ParameterDefinitionConfig `json:"parameters"`
	Execution        ExecutionPolicyConfig                `json:"execution"`
	Output           OutputPolicyConfig                   `json:"output"`
	Audit            AuditPolicyConfig                    `json:"audit"`
}

type ArgvTemplatePartConfig struct {
	Literal string                 `json:"literal"`
	Param   string                 `json:"param"`
	Flag    *ConditionalFlagConfig `json:"flag"`
	Repeat  *RepeatParameterConfig `json:"repeat"`
}

type ConditionalFlagConfig struct {
	Value string                   `json:"value"`
	When  ParameterConditionConfig `json:"when"`
}

type RepeatParameterConfig struct {
	Param  string `json:"param"`
	Prefix string `json:"prefix"`
}

type ParameterConditionConfig struct {
	Param  string      `json:"param"`
	Equals interface{} `json:"equals"`
}

type ParameterDefinitionConfig struct {
	Type             string                     `json:"type"`
	Required         bool                       `json:"required"`
	Default          interface{}                `json:"default"`
	Description      string                     `json:"description"`
	Audit            ParameterAuditPolicyConfig `json:"audit"`
	EnumValues       []string                   `json:"values"`
	Min              *int                       `json:"min"`
	Max              *int                       `json:"max"`
	MinLength        *int                       `json:"min_length"`
	MaxLength        *int                       `json:"max_length"`
	Pattern          string                     `json:"pattern"`
	AllowlistPattern string                     `json:"allowlist_pattern"`
	AllowedRoots     []string                   `json:"allowed_roots"`
	PathMode         string                     `json:"mode"`
	MustExist        bool                       `json:"must_exist"`
	ArrayItem        *ParameterDefinitionConfig `json:"item"`
	MaxItems         *int                       `json:"max_items"`
}

type ParameterAuditPolicyConfig struct {
	Redact bool `json:"redact"`
}

type EnvironmentPolicyConfig struct {
	Mode      string            `json:"mode"`
	Variables map[string]string `json:"variables"`
}

type ExecutionPolicyConfig struct {
	TimeoutMS       int `json:"timeout_ms"`
	MaxOutputBytes  int `json:"max_output_bytes"`
	PerCommandLimit int `json:"per_command_limit"`
	GlobalLimit     int `json:"global_limit"`
}

type OutputPolicyConfig struct {
	Stdout            string   `json:"stdout"`
	Stderr            string   `json:"stderr"`
	Truncate          bool     `json:"truncate"`
	SensitivePatterns []string `json:"sensitive_patterns"`
}

type AuditPolicyConfig struct {
	EventName        string   `json:"event_name"`
	RedactParameters []string `json:"redact_parameters"`
	IncludeStdout    bool     `json:"include_stdout"`
	IncludeStderr    bool     `json:"include_stderr"`
	IncludeRejection bool     `json:"include_rejection"`
}

type Error struct {
	Field   string
	Code    string
	Message string
}

func (e Error) Error() string {
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", e.Code, e.Field, e.Message)
}

type ErrorList []Error

func (e ErrorList) Error() string {
	if len(e) == 0 {
		return "configuration is valid"
	}
	return fmt.Sprintf("configuration has %d error(s): %s", len(e), e[0].Error())
}

func LoadFile(path string) ([]command.CommandDefinition, error) {
	runtime, err := LoadRuntimeFile(path)
	if err != nil {
		return nil, err
	}
	return runtime.Commands, nil
}

func LoadRuntimeFile(path string) (RuntimeConfig, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return RuntimeConfig{}, err
	}
	return LoadRuntimeBytes(data)
}

func LoadBytes(data []byte) ([]command.CommandDefinition, error) {
	runtime, err := LoadRuntimeBytes(data)
	if err != nil {
		return nil, err
	}
	return runtime.Commands, nil
}

func LoadRuntimeBytes(data []byte) (RuntimeConfig, error) {
	var file File
	if err := json.Unmarshal(data, &file); err != nil {
		return RuntimeConfig{}, err
	}
	return BuildRuntime(file)
}

func Build(file File) ([]command.CommandDefinition, error) {
	runtime, err := BuildRuntime(file)
	if err != nil {
		return nil, err
	}
	return runtime.Commands, nil
}

func BuildRuntime(file File) (RuntimeConfig, error) {
	defs := make([]command.CommandDefinition, 0, len(file.Commands))
	var errs ErrorList
	if file.Version != 1 {
		errs = append(errs, Error{Field: "version", Code: "config.unsupported_version", Message: "unsupported config version"})
	}
	seen := map[string]bool{}
	for i, cfg := range file.Commands {
		field := fmt.Sprintf("commands[%d]", i)
		if seen[cfg.ID] {
			errs = append(errs, Error{Field: field + ".id", Code: "config.duplicate_id", Message: "duplicate command id"})
		}
		seen[cfg.ID] = true
		def, cmdErrs := buildCommand(cfg, field)
		errs = append(errs, cmdErrs...)
		defs = append(defs, def)
	}
	errs = append(errs, validateArtifactConfig(file.ArtifactStore, file.DeployProfiles, defs)...)
	if len(errs) > 0 {
		return RuntimeConfig{}, errs
	}
	return RuntimeConfig{
		Commands:       defs,
		ArtifactStore:  file.ArtifactStore,
		DeployProfiles: append([]artifact.DeployProfile(nil), file.DeployProfiles...),
	}, nil
}

func validateArtifactConfig(store artifact.StoreConfig, profiles []artifact.DeployProfile, defs []command.CommandDefinition) ErrorList {
	var errs ErrorList
	if !store.Enabled {
		if len(profiles) > 0 {
			errs = append(errs, Error{Field: "deploy_profiles", Code: "config.artifact_store_disabled", Message: "deploy profiles require artifact store to be enabled"})
		}
		return errs
	}
	if store.RootDirectory == "" {
		errs = append(errs, Error{Field: "artifact_store.root_directory", Code: "config.required", Message: "artifact root directory is required"})
	} else if !filepath.IsAbs(store.RootDirectory) {
		errs = append(errs, Error{Field: "artifact_store.root_directory", Code: "config.absolute_path_required", Message: "artifact root directory must be absolute"})
	}
	if store.MaxUploadBytes <= 0 {
		errs = append(errs, Error{Field: "artifact_store.max_upload_bytes", Code: "config.positive_required", Message: "max upload bytes must be positive"})
	}
	if store.MaxChunkBytes <= 0 {
		errs = append(errs, Error{Field: "artifact_store.max_chunk_bytes", Code: "config.positive_required", Message: "max chunk bytes must be positive"})
	}
	if store.MaxUploadBytes > 0 && store.MaxChunkBytes > store.MaxUploadBytes {
		errs = append(errs, Error{Field: "artifact_store.max_chunk_bytes", Code: "config.chunk_too_large", Message: "max chunk bytes cannot exceed max upload bytes"})
	}
	commandIDs := map[string]command.CommandDefinition{}
	for _, def := range defs {
		commandIDs[def.ID] = def
	}
	seen := map[string]bool{}
	profileIDPattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)
	for i, profile := range profiles {
		field := fmt.Sprintf("deploy_profiles[%d]", i)
		if profile.ID == "" {
			errs = append(errs, Error{Field: field + ".id", Code: "config.required", Message: "profile id is required"})
		} else if !profileIDPattern.MatchString(profile.ID) {
			errs = append(errs, Error{Field: field + ".id", Code: "config.invalid_id", Message: "profile id format is invalid"})
		}
		if seen[profile.ID] {
			errs = append(errs, Error{Field: field + ".id", Code: "config.duplicate_id", Message: "duplicate deploy profile id"})
		}
		seen[profile.ID] = true
		if profile.ArtifactNamePattern == "" {
			errs = append(errs, Error{Field: field + ".artifact_name_pattern", Code: "config.required", Message: "artifact name pattern is required"})
		} else if _, err := regexp.Compile(profile.ArtifactNamePattern); err != nil {
			errs = append(errs, Error{Field: field + ".artifact_name_pattern", Code: "config.invalid_pattern", Message: "artifact name pattern is invalid"})
		}
		if profile.DeployCommandID == "" {
			errs = append(errs, Error{Field: field + ".deploy_command_id", Code: "config.required", Message: "deploy command id is required"})
			continue
		}
		def, ok := commandIDs[profile.DeployCommandID]
		if !ok {
			errs = append(errs, Error{Field: field + ".deploy_command_id", Code: "config.unknown_command", Message: "deploy command is not registered"})
			continue
		}
		param, ok := def.Parameters["artifact_path"]
		if !ok {
			errs = append(errs, Error{Field: field + ".deploy_command_id", Code: "config.artifact_path_required", Message: "deploy command must define artifact_path parameter"})
			continue
		}
		if param.Type != command.ParameterTypePath {
			errs = append(errs, Error{Field: field + ".deploy_command_id", Code: "config.artifact_path_type", Message: "artifact_path must be a path parameter"})
		}
	}
	return errs
}

func buildCommand(cfg CommandConfig, field string) (command.CommandDefinition, ErrorList) {
	var errs ErrorList
	risk, ok := parseRiskLevel(cfg.RiskLevel)
	if !ok {
		errs = append(errs, Error{Field: field + ".risk_level", Code: "config.unknown_risk", Message: "unknown risk level"})
	}
	access, ok := parseAccessMode(cfg.Access)
	if !ok {
		errs = append(errs, Error{Field: field + ".access", Code: "config.unknown_access", Message: "unknown access mode"})
	}
	if cfg.ID == "" {
		errs = append(errs, Error{Field: field + ".id", Code: "config.required", Message: "command id is required"})
	}
	if cfg.Executable == "" {
		errs = append(errs, Error{Field: field + ".executable", Code: "config.required", Message: "executable is required"})
	} else if isForbiddenExecutable(cfg.Executable) {
		errs = append(errs, Error{Field: field + ".executable", Code: "policy.forbidden_executable", Message: "executable is forbidden"})
	}
	if cfg.Execution.TimeoutMS <= 0 {
		errs = append(errs, Error{Field: field + ".execution.timeout_ms", Code: "config.missing_timeout", Message: "timeout is required"})
	}
	if cfg.Execution.MaxOutputBytes <= 0 {
		errs = append(errs, Error{Field: field + ".execution.max_output_bytes", Code: "config.missing_output_limit", Message: "max_output_bytes is required"})
	}
	params, paramErrs := buildParameters(cfg.Parameters, field+".parameters")
	errs = append(errs, paramErrs...)
	argv, argvErrs := buildArgvTemplate(cfg.ArgvTemplate, params, field+".argv_template")
	errs = append(errs, argvErrs...)

	return command.CommandDefinition{
		ID:               cfg.ID,
		Description:      cfg.Description,
		Enabled:          cfg.Enabled,
		RiskLevel:        risk,
		Access:           access,
		Executable:       cfg.Executable,
		ArgvTemplate:     argv,
		WorkingDirectory: cfg.WorkingDirectory,
		Environment: command.EnvironmentPolicy{
			Mode:      command.EnvironmentMode(cfg.Environment.Mode),
			Variables: cfg.Environment.Variables,
		},
		Parameters: params,
		Execution: command.ExecutionPolicy{
			Timeout:         time.Duration(cfg.Execution.TimeoutMS) * time.Millisecond,
			MaxOutputBytes:  cfg.Execution.MaxOutputBytes,
			PerCommandLimit: cfg.Execution.PerCommandLimit,
			GlobalLimit:     cfg.Execution.GlobalLimit,
		},
		Output: command.OutputPolicy{
			Stdout:            command.OutputFormat(cfg.Output.Stdout),
			Stderr:            command.OutputFormat(cfg.Output.Stderr),
			Truncate:          cfg.Output.Truncate,
			SensitivePatterns: cfg.Output.SensitivePatterns,
		},
		Audit: command.AuditPolicy{
			EventName:        cfg.Audit.EventName,
			RedactParameters: cfg.Audit.RedactParameters,
			IncludeStdout:    cfg.Audit.IncludeStdout,
			IncludeStderr:    cfg.Audit.IncludeStderr,
			IncludeRejection: cfg.Audit.IncludeRejection,
		},
	}, errs
}

func buildParameters(configs map[string]ParameterDefinitionConfig, field string) (map[string]command.ParameterDefinition, ErrorList) {
	params := make(map[string]command.ParameterDefinition, len(configs))
	var errs ErrorList
	for name, cfg := range configs {
		param, paramErrs := buildParameter(cfg, field+"."+name)
		errs = append(errs, paramErrs...)
		params[name] = param
	}
	return params, errs
}

func buildParameter(cfg ParameterDefinitionConfig, field string) (command.ParameterDefinition, ErrorList) {
	var errs ErrorList
	ptype, ok := parseParameterType(cfg.Type)
	if !ok {
		errs = append(errs, Error{Field: field + ".type", Code: "config.unknown_parameter_type", Message: "unknown parameter type"})
	}
	switch ptype {
	case command.ParameterTypeEnum:
		if len(cfg.EnumValues) == 0 {
			errs = append(errs, Error{Field: field + ".values", Code: "config.enum_values_required", Message: "enum values are required"})
		}
	case command.ParameterTypeInt:
		if cfg.Min == nil || cfg.Max == nil {
			errs = append(errs, Error{Field: field, Code: "config.int_bounds_required", Message: "int min and max are required"})
		}
	case command.ParameterTypeString:
		if cfg.MaxLength == nil {
			errs = append(errs, Error{Field: field + ".max_length", Code: "config.string_max_length_required", Message: "string max_length is required"})
		}
	case command.ParameterTypePath:
		if len(cfg.AllowedRoots) == 0 {
			errs = append(errs, Error{Field: field + ".allowed_roots", Code: "config.path_roots_required", Message: "path allowed_roots are required"})
		}
	case command.ParameterTypeArray:
		if cfg.MaxItems == nil || cfg.ArrayItem == nil {
			errs = append(errs, Error{Field: field, Code: "config.array_constraints_required", Message: "array max_items and item are required"})
		}
	}
	var item *command.ParameterDefinition
	if cfg.ArrayItem != nil {
		built, itemErrs := buildParameter(*cfg.ArrayItem, field+".item")
		errs = append(errs, itemErrs...)
		item = &built
	}
	return command.ParameterDefinition{
		Type:             ptype,
		Required:         cfg.Required,
		Default:          cfg.Default,
		Description:      cfg.Description,
		Audit:            command.ParameterAuditPolicy{Redact: cfg.Audit.Redact},
		EnumValues:       cfg.EnumValues,
		Min:              cfg.Min,
		Max:              cfg.Max,
		MinLength:        cfg.MinLength,
		MaxLength:        cfg.MaxLength,
		Pattern:          cfg.Pattern,
		AllowlistPattern: cfg.AllowlistPattern,
		AllowedRoots:     cfg.AllowedRoots,
		PathMode:         command.PathMode(cfg.PathMode),
		MustExist:        cfg.MustExist,
		ArrayItem:        item,
		MaxItems:         cfg.MaxItems,
	}, errs
}

func buildArgvTemplate(configs []ArgvTemplatePartConfig, params map[string]command.ParameterDefinition, field string) ([]command.ArgvTemplatePart, ErrorList) {
	parts := make([]command.ArgvTemplatePart, 0, len(configs))
	var errs ErrorList
	for i, cfg := range configs {
		partField := fmt.Sprintf("%s[%d]", field, i)
		if containsShellSyntax(cfg.Literal) {
			errs = append(errs, Error{Field: partField, Code: "policy.forbidden_shell_syntax", Message: "argv template contains shell syntax"})
		}
		if cfg.Param != "" && !hasParam(params, cfg.Param) {
			errs = append(errs, Error{Field: partField + ".param", Code: "config.unknown_parameter", Message: "argv references unknown parameter"})
		}
		if cfg.Repeat != nil && !hasParam(params, cfg.Repeat.Param) {
			errs = append(errs, Error{Field: partField + ".repeat.param", Code: "config.unknown_parameter", Message: "argv references unknown parameter"})
		}
		parts = append(parts, command.ArgvTemplatePart{
			Literal: cfg.Literal,
			Param:   cfg.Param,
			Repeat:  buildRepeat(cfg.Repeat),
			Flag:    buildFlag(cfg.Flag),
		})
	}
	return parts, errs
}

func buildRepeat(cfg *RepeatParameterConfig) *command.RepeatParameter {
	if cfg == nil {
		return nil
	}
	return &command.RepeatParameter{Param: cfg.Param, Prefix: cfg.Prefix}
}

func buildFlag(cfg *ConditionalFlagConfig) *command.ConditionalFlag {
	if cfg == nil {
		return nil
	}
	return &command.ConditionalFlag{Value: cfg.Value, When: command.ParameterCondition{Param: cfg.When.Param, Equals: cfg.When.Equals}}
}

func parseRiskLevel(value string) (command.RiskLevel, bool) {
	switch command.RiskLevel(value) {
	case command.RiskLevelLow, command.RiskLevelMedium, command.RiskLevelHigh:
		return command.RiskLevel(value), true
	default:
		return "", false
	}
}

func parseAccessMode(value string) (command.AccessMode, bool) {
	switch command.AccessMode(value) {
	case command.AccessModeReadOnly, command.AccessModeStateChanging:
		return command.AccessMode(value), true
	default:
		return "", false
	}
}

func parseParameterType(value string) (command.ParameterType, bool) {
	switch command.ParameterType(value) {
	case command.ParameterTypeEnum, command.ParameterTypeBool, command.ParameterTypeInt, command.ParameterTypeString, command.ParameterTypePath, command.ParameterTypeArray:
		return command.ParameterType(value), true
	default:
		return "", false
	}
}

func isForbiddenExecutable(executable string) bool {
	base := filepath.Base(executable)
	forbidden := map[string]bool{
		"sh": true, "bash": true, "zsh": true, "fish": true,
		"sudo": true, "su": true, "doas": true,
		"apt": true, "apt-get": true, "yum": true, "dnf": true, "apk": true, "brew": true, "pip": true, "npm": true,
		"useradd": true, "usermod": true, "userdel": true, "groupadd": true, "passwd": true, "chmod": true, "chown": true,
		"rm": true, "mv": true, "cp": true, "tee": true, "dd": true, "mkfs": true,
		"cat": true, "less": true, "more": true, "head": true, "tail": true, "grep": true, "awk": true, "sed": true,
		"nmap": true, "masscan": true, "zmap": true,
		"nohup": true, "daemonize": true, "crontab": true, "at": true,
	}
	return forbidden[base]
}

func containsShellSyntax(value string) bool {
	if value == "" {
		return false
	}
	for _, token := range []string{"|", ">", "<", ";", "&&", "||", "$(", "`", "&"} {
		if strings.Contains(value, token) {
			return true
		}
	}
	return false
}

func hasParam(params map[string]command.ParameterDefinition, name string) bool {
	_, ok := params[name]
	return ok
}
