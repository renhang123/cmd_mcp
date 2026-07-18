package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"server-shell-mcp/internal/app"
	"server-shell-mcp/internal/audit"
	"server-shell-mcp/internal/config"
	"server-shell-mcp/internal/domain/argv"
	"server-shell-mcp/internal/domain/command"
	"server-shell-mcp/internal/domain/validation"
	"server-shell-mcp/internal/executor"
	"server-shell-mcp/internal/mcp"
	"server-shell-mcp/internal/metrics"
)

func main() {
	commandsPath := flag.String("commands", "configs/commands.example.json", "path to command allowlist config")
	flag.Parse()

	defs, err := config.LoadFile(*commandsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load commands: %v\n", err)
		os.Exit(1)
	}

	registry := command.NewRegistry(defs)
	processExecutor := executor.NewLimitingExecutor(executor.NewProcessExecutor(), 4)
	service := app.NewCommandService(
		registry,
		validation.NewValidator(),
		argv.NewBuilder(),
		processExecutor,
		audit.NewJSONL(os.Stderr),
		metrics.NewRecorder(),
	)
	adapter := mcp.NewAdapter(service, mcp.ToolsFromSummaries(registry.List()))
	server := mcp.NewStdioServer(adapter, os.Stdin, os.Stdout)
	if err := server.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
