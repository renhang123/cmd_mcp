package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"server-shell-mcp/internal/app"
)

type StdioServer struct {
	adapter *Adapter
	in      io.Reader
	out     io.Writer
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Result  interface{} `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolsCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

func NewStdioServer(adapter *Adapter, in io.Reader, out io.Writer) *StdioServer {
	return &StdioServer{adapter: adapter, in: in, out: out}
}

func (s *StdioServer) Serve(ctx context.Context) error {
	scanner := bufio.NewScanner(s.in)
	writer := bufio.NewWriter(s.out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		resp := s.handle(ctx, line)
		if resp == nil {
			continue
		}
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		if _, err := writer.Write(append(data, '\n')); err != nil {
			return err
		}
		if err := writer.Flush(); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func (s *StdioServer) handle(ctx context.Context, line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return errorResponse(nil, -32700, "parse error")
	}
	if req.JSONRPC != "2.0" {
		return errorResponse(req.ID, -32600, "invalid jsonrpc version")
	}
	switch req.Method {
	case "initialize":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"serverInfo": map[string]interface{}{
				"name":    "server-shell-mcp",
				"version": "0.1.0",
			},
			"capabilities": map[string]interface{}{"tools": map[string]interface{}{}},
		}}
	case "notifications/initialized":
		return nil
	case "tools/list":
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{"tools": s.mcpTools()}}
	case "tools/call":
		return s.handleToolCall(ctx, req)
	default:
		return errorResponse(req.ID, -32601, fmt.Sprintf("method not found: %s", req.Method))
	}
}

func (s *StdioServer) handleToolCall(ctx context.Context, req rpcRequest) *rpcResponse {
	var params toolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return errorResponse(req.ID, -32602, "invalid tools/call params")
	}
	call := s.adapter.Call(ctx, CallRequest{RequestID: fmt.Sprintf("%v", req.ID), ToolName: params.Name, Arguments: params.Arguments})
	if !call.ProtocolOK {
		return errorResponse(req.ID, -32602, call.ProtocolError)
	}
	return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: map[string]interface{}{
		"content": []map[string]interface{}{{"type": "text", "text": resultText(call.Result)}},
		"isError": call.Result.Status != "success",
	}}
}

func (s *StdioServer) mcpTools() []map[string]interface{} {
	tools := s.adapter.Tools()
	out := make([]map[string]interface{}, 0, len(tools))
	for _, tool := range tools {
		out = append(out, map[string]interface{}{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": tool.InputSchema,
		})
	}
	return out
}

func resultText(result interface{}) string {
	commandResult, ok := result.(*app.CommandResult)
	if !ok {
		data, err := json.Marshal(result)
		if err != nil {
			return "{}"
		}
		return string(data)
	}
	data, err := json.Marshal(map[string]interface{}{
		"request_id":     commandResult.RequestID,
		"command_id":     commandResult.CommandID,
		"status":         commandResult.Status,
		"exit_code":      commandResult.ExitCode,
		"stdout":         commandResult.Stdout,
		"stderr":         commandResult.Stderr,
		"duration_ms":    commandResult.Duration.Milliseconds(),
		"timeout":        commandResult.Timeout,
		"truncated":      commandResult.Truncated,
		"error_category": commandResult.ErrorCategory,
		"message":        commandResult.Message,
		"cleanup_status": commandResult.CleanupStatus,
	})
	if err != nil {
		return "{}"
	}
	return string(data)
}

func errorResponse(id interface{}, code int, message string) *rpcResponse {
	return &rpcResponse{JSONRPC: "2.0", ID: id, Error: &rpcError{Code: code, Message: message}}
}
