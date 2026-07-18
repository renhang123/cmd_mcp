package executor

import (
	"context"
	"strings"
	"testing"
	"time"

	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
)

func TestProcessExecutorSuccess(t *testing.T) {
	result := NewProcessExecutor().Execute(context.Background(), argv.ExecutionSpec{
		CommandID:  "echo",
		Executable: "/bin/echo",
		Argv:       []string{"ok"},
	}, testPolicy(), Context{})
	if result.ExitCode == nil || *result.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %#v", result.ExitCode)
	}
	if strings.TrimSpace(result.Stdout) != "ok" {
		t.Fatalf("expected stdout ok, got %q", result.Stdout)
	}
}

func TestProcessExecutorFailure(t *testing.T) {
	result := NewProcessExecutor().Execute(context.Background(), argv.ExecutionSpec{
		CommandID:  "false",
		Executable: "/usr/bin/false",
	}, testPolicy(), Context{})
	if result.ExitCode == nil || *result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit, got %#v", result.ExitCode)
	}
	if result.ExecutionError == "" {
		t.Fatal("expected execution error")
	}
}

func TestProcessExecutorTimeout(t *testing.T) {
	policy := testPolicy()
	policy.Timeout = 10 * time.Millisecond
	result := NewProcessExecutor().Execute(context.Background(), argv.ExecutionSpec{
		CommandID:  "sleep",
		Executable: "/bin/sleep",
		Argv:       []string{"1"},
	}, policy, Context{})
	if !result.TimedOut {
		t.Fatal("expected timeout")
	}
	if result.CleanupStatus == CleanupNotRequired {
		t.Fatal("expected cleanup attempt")
	}
}

func TestLimitOutputTruncatesStdoutAndStderr(t *testing.T) {
	result := LimitOutput(Result{Stdout: "abcdef", Stderr: "ghijkl"}, 3)
	if result.Stdout != "abc" || !result.StdoutTruncated {
		t.Fatalf("expected truncated stdout, got %#v", result)
	}
	if result.Stderr != "ghi" || !result.StderrTruncated {
		t.Fatalf("expected truncated stderr, got %#v", result)
	}
}

func TestLimitingExecutorRejectsWhenGlobalLimitReached(t *testing.T) {
	blocking := &blockingExecutor{started: make(chan struct{}), release: make(chan struct{})}
	limited := NewLimitingExecutor(blocking, 1)
	policy := testPolicy()
	spec := argv.ExecutionSpec{CommandID: "test", Executable: "/bin/echo"}

	go limited.Execute(context.Background(), spec, policy, Context{})
	<-blocking.started

	result := limited.Execute(context.Background(), spec, policy, Context{})
	close(blocking.release)
	if !strings.Contains(result.ExecutionError, "policy_denied") {
		t.Fatalf("expected policy_denied, got %#v", result)
	}
}

func TestExecutorInterfaceAcceptsExecutionSpecOnly(t *testing.T) {
	var exec Executor = NewMock(Success("ok"))
	result := exec.Execute(context.Background(), argv.ExecutionSpec{CommandID: "test", Executable: "/bin/echo"}, testPolicy(), Context{})
	if result.Stdout != "ok" {
		t.Fatalf("expected mock stdout, got %q", result.Stdout)
	}
}

type blockingExecutor struct {
	started chan struct{}
	release chan struct{}
}

func (b *blockingExecutor) Execute(ctx context.Context, spec argv.ExecutionSpec, policy command.ExecutionPolicy, execCtx Context) Result {
	close(b.started)
	<-b.release
	return Success("done")
}

func testPolicy() command.ExecutionPolicy {
	return command.ExecutionPolicy{Timeout: time.Second, MaxOutputBytes: 1024, PerCommandLimit: 1}
}
