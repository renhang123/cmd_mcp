package mcp

import (
	"context"
	"fmt"

	"server-shell-mcp/internal/app"
	"server-shell-mcp/internal/domain/command"
)

type CommandService interface {
	Execute(ctx context.Context, req app.CommandRequest) app.CommandResult
	Explain(req app.CommandRequest) app.ExplainResult
}

type Adapter struct {
	service CommandService
	tools   []Tool
}

type Tool struct {
	Name        string
	Description string
	InputSchema map[string]interface{}
	RiskLevel   string
}

type CallRequest struct {
	RequestID string
	ToolName  string
	Arguments map[string]interface{}
	Source    app.SourceSummary
}

type CallResponse struct {
	ProtocolOK    bool
	ProtocolError string
	Result        *app.CommandResult
}

func NewAdapter(service CommandService, tools []Tool) *Adapter {
	return &Adapter{service: service, tools: append([]Tool(nil), tools...)}
}

func ToolsFromSummaries(summaries []command.Summary) []Tool {
	base := map[string]Tool{}
	for _, tool := range MVPTools() {
		base[tool.Name] = tool
	}
	tools := make([]Tool, 0, len(summaries))
	for _, summary := range summaries {
		tool, ok := base[summary.ID]
		if !ok {
			tool = Tool{Name: summary.ID, Description: summary.Description, InputSchema: objectSchema(nil), RiskLevel: string(summary.RiskLevel)}
		}
		tool.Description = summary.Description
		tool.RiskLevel = string(summary.RiskLevel)
		tools = append(tools, tool)
	}
	return tools
}

func (a *Adapter) Tools() []Tool {
	return append([]Tool(nil), a.tools...)
}

func (a *Adapter) Call(ctx context.Context, req CallRequest) CallResponse {
	if req.ToolName == "" {
		return CallResponse{ProtocolOK: false, ProtocolError: "missing tool name"}
	}
	if !a.hasTool(req.ToolName) {
		return CallResponse{ProtocolOK: false, ProtocolError: fmt.Sprintf("unknown tool: %s", req.ToolName)}
	}
	result := a.service.Execute(ctx, app.CommandRequest{
		RequestID: req.RequestID,
		CommandID: req.ToolName,
		Arguments: req.Arguments,
		Source: app.SourceSummary{
			ClientIDHash: req.Source.ClientIDHash,
			UserIDHash:   req.Source.UserIDHash,
			RemoteHash:   req.Source.RemoteHash,
			Transport:    req.Source.Transport,
			MCPTool:      req.ToolName,
		},
	})
	return CallResponse{ProtocolOK: true, Result: &result}
}

func (a *Adapter) Explain(req CallRequest) (app.ExplainResult, error) {
	if req.ToolName == "" {
		return app.ExplainResult{}, fmt.Errorf("missing tool name")
	}
	if !a.hasTool(req.ToolName) {
		return app.ExplainResult{}, fmt.Errorf("unknown tool: %s", req.ToolName)
	}
	return a.service.Explain(app.CommandRequest{RequestID: req.RequestID, CommandID: req.ToolName, Arguments: req.Arguments, Source: req.Source}), nil
}

func (a *Adapter) hasTool(name string) bool {
	for _, tool := range a.tools {
		if tool.Name == name {
			return true
		}
	}
	return false
}

func MVPTools() []Tool {
	return []Tool{
		{Name: "system_summary", Description: "Show basic system summary.", InputSchema: objectSchema(nil), RiskLevel: "low"},
		{Name: "disk_usage_summary", Description: "Show disk usage summary.", InputSchema: objectSchema(map[string]interface{}{"mount_selector": enumSchema([]string{"all", "root", "data", "logs"})}), RiskLevel: "low"},
		{Name: "memory_summary", Description: "Show memory usage summary.", InputSchema: objectSchema(nil), RiskLevel: "low"},
		{Name: "cpu_load_summary", Description: "Show CPU load summary.", InputSchema: objectSchema(map[string]interface{}{"window": enumSchema([]string{"1m", "5m", "15m"})}), RiskLevel: "low"},
		{Name: "process_top_summary", Description: "Show top process summary.", InputSchema: objectSchema(map[string]interface{}{"sort_by": enumSchema([]string{"cpu", "memory"}), "limit": intSchema(1, 20)}), RiskLevel: "medium"},
		{Name: "service_status_check", Description: "Check allowlisted service status.", InputSchema: objectSchema(map[string]interface{}{"service_name": enumSchema([]string{})}), RiskLevel: "medium"},
		{Name: "recent_service_logs", Description: "Show recent allowlisted service logs.", InputSchema: objectSchema(map[string]interface{}{"service_name": enumSchema([]string{}), "level": enumSchema([]string{"error", "warn", "info"}), "line_limit": intSchema(1, 200)}), RiskLevel: "medium"},
		{Name: "tcp_listen_summary", Description: "Show TCP listen summary.", InputSchema: objectSchema(map[string]interface{}{"protocol": enumSchema([]string{"tcp", "tcp4", "tcp6"})}), RiskLevel: "medium"},
		{Name: "dns_resolution_check", Description: "Check allowlisted DNS resolution.", InputSchema: objectSchema(map[string]interface{}{"hostname": map[string]interface{}{"type": "string"}, "record_type": enumSchema([]string{"A", "AAAA", "CNAME"})}), RiskLevel: "low"},
		{Name: "http_health_probe", Description: "Probe allowlisted HTTP health target.", InputSchema: objectSchema(map[string]interface{}{"target": enumSchema([]string{}), "timeout_seconds": intSchema(1, 10)}), RiskLevel: "medium"},
	}
}

func objectSchema(properties map[string]interface{}) map[string]interface{} {
	if properties == nil {
		properties = map[string]interface{}{}
	}
	return map[string]interface{}{"type": "object", "properties": properties, "additionalProperties": false}
}

func enumSchema(values []string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": values}
}

func intSchema(min int, max int) map[string]interface{} {
	return map[string]interface{}{"type": "integer", "minimum": min, "maximum": max}
}
