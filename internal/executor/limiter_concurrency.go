package executor

import (
	"context"
	"sync"

	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
)

type LimitingExecutor struct {
	next   Executor
	global chan struct{}
	mu     sync.Mutex
	byID   map[string]chan struct{}
}

func NewLimitingExecutor(next Executor, globalLimit int) *LimitingExecutor {
	var global chan struct{}
	if globalLimit > 0 {
		global = make(chan struct{}, globalLimit)
	}
	return &LimitingExecutor{next: next, global: global, byID: map[string]chan struct{}{}}
}

func (l *LimitingExecutor) Execute(ctx context.Context, spec argv.ExecutionSpec, policy command.ExecutionPolicy, execCtx Context) Result {
	if l.global != nil && !tryAcquire(l.global) {
		return policyDeniedResult("global concurrency limit reached")
	}
	if l.global != nil {
		defer release(l.global)
	}

	perCommand := l.perCommandLimiter(spec.CommandID, policy.PerCommandLimit)
	if perCommand != nil && !tryAcquire(perCommand) {
		return policyDeniedResult("per-command concurrency limit reached")
	}
	if perCommand != nil {
		defer release(perCommand)
	}
	return l.next.Execute(ctx, spec, policy, execCtx)
}

func (l *LimitingExecutor) perCommandLimiter(commandID string, limit int) chan struct{} {
	if limit <= 0 {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	limiter, ok := l.byID[commandID]
	if !ok {
		limiter = make(chan struct{}, limit)
		l.byID[commandID] = limiter
	}
	return limiter
}

func tryAcquire(ch chan struct{}) bool {
	select {
	case ch <- struct{}{}:
		return true
	default:
		return false
	}
}

func release(ch chan struct{}) {
	<-ch
}

func policyDeniedResult(message string) Result {
	return Result{CleanupStatus: CleanupNotRequired, ExecutionError: "policy_denied: " + message}
}
