package mcp

import (
	"context"
	"testing"

	"server-shell-mcp/internal/app"
)

func TestMVPToolsDeclareNoRawExecuteTool(t *testing.T) {
	tools := MVPTools()
	if len(tools) == 0 {
		t.Fatal("expected tools")
	}
	for _, tool := range tools {
		if tool.Name == "execute" || tool.Name == "shell" || tool.Name == "run_command" {
			t.Fatalf("unexpected raw execution tool %s", tool.Name)
		}
		if tool.InputSchema["type"] != "object" {
			t.Fatalf("expected object schema for %s", tool.Name)
		}
	}
}

func TestAdapterReturnsProtocolErrorForUnknownTool(t *testing.T) {
	adapter := NewAdapter(&fakeService{}, []Tool{{Name: "system_summary"}})
	resp := adapter.Call(context.Background(), CallRequest{ToolName: "unknown"})
	if resp.ProtocolOK {
		t.Fatal("expected protocol error")
	}
	if resp.Result != nil {
		t.Fatal("expected no business result for protocol error")
	}
}

func TestAdapterReturnsStructuredBusinessResult(t *testing.T) {
	service := &fakeService{result: app.CommandResult{Status: app.StatusRejected, ErrorCategory: app.ErrorValidation, Message: "Invalid command arguments."}}
	adapter := NewAdapter(service, []Tool{{Name: "system_summary"}})
	resp := adapter.Call(context.Background(), CallRequest{RequestID: "r1", ToolName: "system_summary", Arguments: map[string]interface{}{}})
	if !resp.ProtocolOK {
		t.Fatalf("expected protocol ok, got %s", resp.ProtocolError)
	}
	if resp.Result == nil || resp.Result.ErrorCategory != app.ErrorValidation {
		t.Fatalf("expected structured validation result, got %#v", resp.Result)
	}
	if service.calls != 1 {
		t.Fatalf("expected service call, got %d", service.calls)
	}
}

func TestAdapterCallsOnlyCommandService(t *testing.T) {
	service := &fakeService{result: app.CommandResult{Status: app.StatusSuccess}}
	adapter := NewAdapter(service, []Tool{{Name: "system_summary"}})
	adapter.Call(context.Background(), CallRequest{ToolName: "system_summary"})
	if service.calls != 1 {
		t.Fatalf("expected exactly one CommandService call, got %d", service.calls)
	}
}

type fakeService struct {
	result app.CommandResult
	calls  int
}

func (f *fakeService) Execute(ctx context.Context, req app.CommandRequest) app.CommandResult {
	f.calls++
	f.result.RequestID = req.RequestID
	f.result.CommandID = req.CommandID
	return f.result
}

func (f *fakeService) Explain(req app.CommandRequest) app.ExplainResult {
	return app.ExplainResult{CommandID: req.CommandID}
}
