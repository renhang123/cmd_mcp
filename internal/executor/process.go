package executor

import (
	"bytes"
	"context"
	"os/exec"
	"syscall"
	"time"

	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
)

type ProcessExecutor struct{}

func NewProcessExecutor() *ProcessExecutor {
	return &ProcessExecutor{}
}

func (p *ProcessExecutor) Execute(ctx context.Context, spec argv.ExecutionSpec, policy command.ExecutionPolicy, execCtx Context) Result {
	started := time.Now()
	if policy.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, policy.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, spec.Executable, spec.Argv...)
	cmd.Dir = spec.WorkingDirectory
	cmd.Env = envList(spec.Environment)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Start()
	if err != nil {
		return Result{Duration: time.Since(started), CleanupStatus: CleanupNotRequired, ExecutionError: err.Error()}
	}

	err = cmd.Wait()
	result := Result{
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
		Duration:      time.Since(started),
		CleanupStatus: CleanupNotRequired,
	}
	if ctx.Err() == context.DeadlineExceeded {
		result.TimedOut = true
		result.CleanupStatus = cleanupProcessGroup(cmd.Process.Pid)
		result.ExecutionError = "timeout"
		return LimitOutput(result, policy.MaxOutputBytes)
	}
	if cmd.ProcessState != nil {
		code := cmd.ProcessState.ExitCode()
		result.ExitCode = &code
	}
	if err != nil {
		result.ExecutionError = err.Error()
	}
	return LimitOutput(result, policy.MaxOutputBytes)
}

func cleanupProcessGroup(pid int) CleanupStatus {
	if pid <= 0 {
		return CleanupFailed
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return CleanupFailed
	}
	return CleanupCompleted
}

func envList(env map[string]string) []string {
	if env == nil {
		return nil
	}
	items := make([]string, 0, len(env))
	for key, value := range env {
		items = append(items, key+"="+value)
	}
	return items
}
