package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"server-shell-mcp/internal/app"
	"server-shell-mcp/internal/artifact"
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

	runtime, err := config.LoadRuntimeFile(*commandsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load commands: %v\n", err)
		os.Exit(1)
	}

	registry := command.NewRegistry(runtime.Commands)
	processExecutor := executor.NewLimitingExecutor(executor.NewProcessExecutor(), 4)
	service := app.NewCommandService(
		registry,
		validation.NewValidator(),
		argv.NewBuilder(),
		processExecutor,
		audit.NewJSONL(os.Stderr),
		metrics.NewRecorder(),
	)
	var artifactService *artifact.Service
	if runtime.ArtifactStore.Enabled {
		artifactService, err = artifact.NewService(runtime.ArtifactStore, runtime.DeployProfiles, service)
		if err != nil {
			fmt.Fprintf(os.Stderr, "init artifact store: %v\n", err)
			os.Exit(1)
		}
	}
	adapter := mcp.NewAdapterWithArtifact(service, artifactService, mcp.ToolsFromSummaries(registry.List()))
	server := mcp.NewStdioServer(adapter, os.Stdin, os.Stdout)
	if err := server.Serve(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "serve: %v\n", err)
		os.Exit(1)
	}
}
