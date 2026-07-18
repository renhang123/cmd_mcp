package app

import (
	"context"
	"time"

	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
	"server-shell-mcp/internal/domain/validation"
	"server-shell-mcp/internal/executor"
)

// Server wires application dependencies. Business rules live outside cmd/server.
type Server struct{}

func NewServer() *Server {
	return &Server{}
}

func (s *Server) Run() {}

type CommandRequest struct {
	RequestID string
	CommandID string
	Arguments map[string]interface{}
	Source    SourceSummary
}

type SourceSummary struct {
	ClientIDHash string
	UserIDHash   string
	RemoteHash   string
	Transport    string
	MCPTool      string
}

type CommandResult struct {
	RequestID     string
	CommandID     string
	Status        Status
	ExitCode      *int
	Stdout        string
	Stderr        string
	Duration      time.Duration
	Timeout       bool
	Truncated     bool
	ErrorCategory ErrorCategory
	Message       string
	CleanupStatus executor.CleanupStatus
}

type Status string

const (
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusRejected  Status = "rejected"
	StatusTimeout   Status = "timeout"
	StatusTruncated Status = "truncated"
)

type ErrorCategory string

const (
	ErrorNone            ErrorCategory = ""
	ErrorNotFound        ErrorCategory = "not_found"
	ErrorValidation      ErrorCategory = "validation_error"
	ErrorPolicyDenied    ErrorCategory = "policy_denied"
	ErrorBuild           ErrorCategory = "build_error"
	ErrorExecutionFailed ErrorCategory = "execution_failed"
	ErrorTimeout         ErrorCategory = "timeout"
	ErrorOutputTruncated ErrorCategory = "output_truncated"
	ErrorInternal        ErrorCategory = "internal_error"
)

type AuditEvent struct {
	RequestID     string
	CommandID     string
	EventType     string
	Status        Status
	ErrorCategory ErrorCategory
	Duration      time.Duration
	Source        SourceSummary
}

type Auditor interface {
	Record(ctx context.Context, event AuditEvent) error
}

type MetricsRecorder interface {
	Record(result CommandResult)
}

type CommandService struct {
	registry  *command.Registry
	validator *validation.Validator
	builder   *argv.Builder
	executor  executor.Executor
	auditor   Auditor
	metrics   MetricsRecorder
}

func NewCommandService(registry *command.Registry, validator *validation.Validator, builder *argv.Builder, exec executor.Executor, auditor Auditor, metrics MetricsRecorder) *CommandService {
	return &CommandService{registry: registry, validator: validator, builder: builder, executor: exec, auditor: auditor, metrics: metrics}
}

func (s *CommandService) Execute(ctx context.Context, req CommandRequest) CommandResult {
	started := time.Now()
	def, err := s.registry.Get(req.CommandID)
	if err != nil {
		return s.finish(ctx, req, rejected(req, started, ErrorNotFound, "Command is not registered or is unavailable."))
	}

	args, err := s.validator.Validate(def, req.Arguments)
	if err != nil {
		return s.finish(ctx, req, rejected(req, started, ErrorValidation, "Invalid command arguments."))
	}

	spec, err := s.builder.Build(def, args)
	if err != nil {
		return s.finish(ctx, req, rejected(req, started, ErrorBuild, "Command could not be prepared safely."))
	}

	execResult := s.executor.Execute(ctx, spec, def.Execution, executor.Context{RequestID: req.RequestID})
	result := fromExecution(req, started, execResult)
	return s.finish(ctx, req, result)
}

type ExplainResult struct {
	CommandID        string
	Executable       string
	Argv             []string
	WorkingDirectory string
	RiskLevel        command.RiskLevel
	Access           command.AccessMode
	ErrorCategory    ErrorCategory
	Message          string
}

func (s *CommandService) Explain(req CommandRequest) ExplainResult {
	def, err := s.registry.Get(req.CommandID)
	if err != nil {
		return ExplainResult{CommandID: req.CommandID, ErrorCategory: ErrorNotFound, Message: "Command is not registered or is unavailable."}
	}
	args, err := s.validator.Validate(def, req.Arguments)
	if err != nil {
		return ExplainResult{CommandID: req.CommandID, ErrorCategory: ErrorValidation, Message: "Invalid command arguments."}
	}
	spec, err := s.builder.Build(def, args)
	if err != nil {
		return ExplainResult{CommandID: req.CommandID, ErrorCategory: ErrorBuild, Message: "Command could not be prepared safely."}
	}
	return ExplainResult{
		CommandID:        def.ID,
		Executable:       spec.Executable,
		Argv:             append([]string(nil), spec.Argv...),
		WorkingDirectory: def.WorkingDirectory,
		RiskLevel:        def.RiskLevel,
		Access:           def.Access,
		Message:          "Command can be prepared safely.",
	}
}

func (s *CommandService) finish(ctx context.Context, req CommandRequest, result CommandResult) CommandResult {
	if s.auditor != nil {
		_ = s.auditor.Record(ctx, AuditEvent{
			RequestID:     result.RequestID,
			CommandID:     result.CommandID,
			EventType:     string(result.Status),
			Status:        result.Status,
			ErrorCategory: result.ErrorCategory,
			Duration:      result.Duration,
			Source:        req.Source,
		})
	}
	if s.metrics != nil {
		s.metrics.Record(result)
	}
	return result
}

func rejected(req CommandRequest, started time.Time, category ErrorCategory, message string) CommandResult {
	return CommandResult{
		RequestID:     req.RequestID,
		CommandID:     req.CommandID,
		Status:        StatusRejected,
		Duration:      time.Since(started),
		ErrorCategory: category,
		Message:       message,
	}
}

func fromExecution(req CommandRequest, started time.Time, result executor.Result) CommandResult {
	base := CommandResult{
		RequestID:     req.RequestID,
		CommandID:     req.CommandID,
		ExitCode:      result.ExitCode,
		Stdout:        result.Stdout,
		Stderr:        result.Stderr,
		Duration:      time.Since(started),
		Timeout:       result.TimedOut,
		Truncated:     result.StdoutTruncated || result.StderrTruncated,
		CleanupStatus: result.CleanupStatus,
	}
	if result.TimedOut {
		base.Status = StatusTimeout
		base.ErrorCategory = ErrorTimeout
		base.Message = "Command timed out."
		return base
	}
	if base.Truncated {
		base.Status = StatusTruncated
		base.ErrorCategory = ErrorOutputTruncated
		base.Message = "Command output was truncated."
		return base
	}
	if result.ExecutionError != "" || (result.ExitCode != nil && *result.ExitCode != 0) {
		base.Status = StatusFailed
		base.ErrorCategory = ErrorExecutionFailed
		base.Message = "Command execution failed."
		return base
	}
	base.Status = StatusSuccess
	base.Message = "Command completed successfully."
	return base
}
