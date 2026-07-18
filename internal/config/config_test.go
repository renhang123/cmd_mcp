package config

import "testing"

func TestBuildRejectsMissingTimeout(t *testing.T) {
	file := validFile()
	file.Commands[0].Execution.TimeoutMS = 0
	_, err := Build(file)
	assertConfigCode(t, err, "config.missing_timeout")
}

func TestBuildRejectsMissingOutputLimit(t *testing.T) {
	file := validFile()
	file.Commands[0].Execution.MaxOutputBytes = 0
	_, err := Build(file)
	assertConfigCode(t, err, "config.missing_output_limit")
}

func TestBuildRejectsRawShellExecutable(t *testing.T) {
	file := validFile()
	file.Commands[0].Executable = "/bin/sh"
	_, err := Build(file)
	assertConfigCode(t, err, "policy.forbidden_executable")
}

func TestBuildRejectsDangerousExecutable(t *testing.T) {
	file := validFile()
	file.Commands[0].Executable = "/usr/bin/apt"
	_, err := Build(file)
	assertConfigCode(t, err, "policy.forbidden_executable")
}

func TestBuildRejectsShellSyntaxInArgvTemplate(t *testing.T) {
	file := validFile()
	file.Commands[0].ArgvTemplate = []ArgvTemplatePartConfig{{Literal: "-a;rm"}}
	_, err := Build(file)
	assertConfigCode(t, err, "policy.forbidden_shell_syntax")
}

func TestBuildAcceptsValidMVPConfig(t *testing.T) {
	defs, err := Build(validFile())
	if err != nil {
		t.Fatalf("expected valid config, got %v", err)
	}
	if len(defs) != 1 {
		t.Fatalf("expected one command, got %d", len(defs))
	}
	if defs[0].ID != "system_summary" {
		t.Fatalf("unexpected command id %s", defs[0].ID)
	}
}

func validFile() File {
	return File{
		Version: 1,
		Commands: []CommandConfig{
			{
				ID:               "system_summary",
				Description:      "Show system summary.",
				Enabled:          true,
				RiskLevel:        "low",
				Access:           "read_only",
				Executable:       "/usr/bin/uname",
				ArgvTemplate:     []ArgvTemplatePartConfig{{Literal: "-a"}},
				WorkingDirectory: "/",
				Environment:      EnvironmentPolicyConfig{Mode: "minimal"},
				Parameters:       map[string]ParameterDefinitionConfig{},
				Execution: ExecutionPolicyConfig{
					TimeoutMS:      3000,
					MaxOutputBytes: 8192,
				},
				Output: OutputPolicyConfig{
					Stdout:   "text",
					Stderr:   "text",
					Truncate: true,
				},
				Audit: AuditPolicyConfig{
					EventName:        "command.system_summary",
					RedactParameters: []string{},
					IncludeRejection: true,
				},
			},
		},
	}
}

func assertConfigCode(t *testing.T, err error, code string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected config error %s", code)
	}
	errs, ok := err.(ErrorList)
	if !ok {
		t.Fatalf("expected ErrorList, got %T", err)
	}
	for _, item := range errs {
		if item.Code == code {
			return
		}
	}
	t.Fatalf("expected code %s in %#v", code, errs)
}
