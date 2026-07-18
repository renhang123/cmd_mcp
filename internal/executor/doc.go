package executor

import (
	"context"
	"time"

	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
)

type Result struct {
	ExitCode        *int
	Stdout          string
	Stderr          string
	Duration        time.Duration
	TimedOut        bool
	StdoutTruncated bool
	StderrTruncated bool
	CleanupStatus   CleanupStatus
	ExecutionError  string
}

type CleanupStatus string

const (
	CleanupCompleted   CleanupStatus = "completed"
	CleanupFailed      CleanupStatus = "failed"
	CleanupNotRequired CleanupStatus = "not_required"
)

type Context struct {
	RequestID string
}

type Executor interface {
	Execute(ctx context.Context, spec argv.ExecutionSpec, policy command.ExecutionPolicy, execCtx Context) Result
}

type MockExecutor struct {
	Result Result
	Calls  []MockCall
}

type MockCall struct {
	Spec    argv.ExecutionSpec
	Policy  command.ExecutionPolicy
	Context Context
}

func NewMock(result Result) *MockExecutor {
	return &MockExecutor{Result: result}
}

func (m *MockExecutor) Execute(ctx context.Context, spec argv.ExecutionSpec, policy command.ExecutionPolicy, execCtx Context) Result {
	m.Calls = append(m.Calls, MockCall{Spec: spec, Policy: policy, Context: execCtx})
	return m.Result
}

func Success(stdout string) Result {
	code := 0
	return Result{ExitCode: &code, Stdout: stdout, CleanupStatus: CleanupNotRequired}
}

func Failure(exitCode int, stderr string) Result {
	return Result{ExitCode: &exitCode, Stderr: stderr, CleanupStatus: CleanupNotRequired, ExecutionError: "execution failed"}
}

func Timeout() Result {
	return Result{TimedOut: true, CleanupStatus: CleanupCompleted, ExecutionError: "timeout"}
}

func Truncated(stdout string) Result {
	code := 0
	return Result{ExitCode: &code, Stdout: stdout, StdoutTruncated: true, CleanupStatus: CleanupNotRequired}
}
