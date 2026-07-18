package app

import (
	"context"
	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
	"server-shell-mcp/internal/domain/validation"
	"server-shell-mcp/internal/executor"
	"testing"
)

type memoryAudit struct {
	Events []AuditEvent
}

func (m *memoryAudit) Record(ctx context.Context, event AuditEvent) error {
	m.Events = append(m.Events, event)
	return nil
}

func TestRejectedRequestIsAuditedAndDoesNotEnterExecutor(t *testing.T) {
	auditor := &memoryAudit{}
	exec := executor.NewMock(executor.Success("should-not-run"))
	service := NewCommandService(command.NewRegistry([]command.CommandDefinition{safeCommand()}), validation.NewValidator(), argv.NewBuilder(), exec, auditor, nil)

	result := service.Execute(context.Background(), CommandRequest{RequestID: "r1", CommandID: "safe", Arguments: map[string]interface{}{"extra": "bad"}})
	if result.Status != StatusRejected || result.ErrorCategory != ErrorValidation {
		t.Fatalf("expected validation rejection, got %#v", result)
	}
	if len(exec.Calls) != 0 {
		t.Fatalf("expected executor not called, got %d calls", len(exec.Calls))
	}
	if len(auditor.Events) != 1 || auditor.Events[0].Status != StatusRejected {
		t.Fatalf("expected rejected audit event, got %#v", auditor.Events)
	}
}

func TestCommandInjectionPayloadRejectedBeforeExecutor(t *testing.T) {
	auditor := &memoryAudit{}
	exec := executor.NewMock(executor.Success("should-not-run"))
	service := NewCommandService(command.NewRegistry([]command.CommandDefinition{safeCommand()}), validation.NewValidator(), argv.NewBuilder(), exec, auditor, nil)

	result := service.Execute(context.Background(), CommandRequest{RequestID: "r1", CommandID: "safe", Arguments: map[string]interface{}{"target": "ok;rm"}})
	if result.Status != StatusRejected {
		t.Fatalf("expected rejection, got %#v", result)
	}
	if len(exec.Calls) != 0 {
		t.Fatalf("expected executor not called, got %d calls", len(exec.Calls))
	}
}

func safeCommand() command.CommandDefinition {
	maxLen := 20
	return command.CommandDefinition{
		ID:               "safe",
		Executable:       "/bin/echo",
		WorkingDirectory: "/",
		Parameters: map[string]command.ParameterDefinition{
			"target": {Type: command.ParameterTypeString, MaxLength: &maxLen, Pattern: "^[a-zA-Z0-9.-]+$"},
		},
		ArgvTemplate: []command.ArgvTemplatePart{{Param: "target"}},
	}
}
