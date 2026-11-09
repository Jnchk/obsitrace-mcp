// Package main implements the MCP server for Obsidian vaults.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/charmbracelet/fang"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"github.com/Jnchk/obsitrace-mcp/internal/filesystem"
	"github.com/Jnchk/obsitrace-mcp/internal/frontmatter"
	"github.com/Jnchk/obsitrace-mcp/internal/pathfilter"
	"github.com/Jnchk/obsitrace-mcp/internal/search"
	"github.com/Jnchk/obsitrace-mcp/internal/worklog"
)

var (
	fileSystem     *filesystem.Service
	searchService  *search.Service
	worklogService *worklog.Service
)

func main() {
	cmd := &cobra.Command{
		Use:   "obsitrace-mcp [vault-path]",
		Short: "ObsiTrace MCP server",
		Long: `obsitrace-mcp is a Model Context Protocol (MCP) server that provides
MCP bindings for Obsidian vaults. It enables any MCP-compatible
AI harness to read and write notes in Obsidian vaults
while preserving YAML frontmatter and enforcing security
boundaries.`,
		Example: `obsitrace-mcp ~/obsidian`,
		Args:    cobra.MaximumNArgs(1),
		RunE:    runServer,
	}

	if err := fang.Execute(
		context.Background(),
		cmd,
		fang.WithVersion(resolveVersion()),
		fang.WithoutCompletions(),
		fang.WithoutManpage(),
	); err != nil {
		os.Exit(1)
	}
}

func runServer(cmd *cobra.Command, args []string) error {
	var vaultPath string
	if len(args) > 0 {
		vaultPath = args[0]
	} else {
		var err error
		vaultPath, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}
	}

	// Initialize services
	pf := pathfilter.New(nil)
	fh := frontmatter.New()
	fileSystem = filesystem.New(vaultPath, pf, fh)
	searchService = search.New(vaultPath, pf)
	var err error
	worklogService, err = worklog.New(vaultPath, worklog.Config{
		Timezone:   os.Getenv("OBSIDIAN_TIMEZONE"),
		IndexPath:  os.Getenv("OBSIDIAN_WORKLOG_INDEX_PATH"),
		TaskDir:    os.Getenv("OBSIDIAN_WORKLOG_TASK_DIR"),
		LogDir:     os.Getenv("OBSIDIAN_WORKLOG_LOG_DIR"),
		SummaryDir: os.Getenv("OBSIDIAN_WORKLOG_SUMMARY_DIR"),
	})
	if err != nil {
		return fmt.Errorf("initialize worklog service: %w", err)
	}

	// Create MCP server
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "obsitrace-mcp",
		Version: resolveVersion(),
	}, nil)

	registerTools(server)

	if err := server.Run(cmd.Context(), &mcp.StdioTransport{}); err != nil {
		return fmt.Errorf("error running server: %w", err)
	}

	return nil
}
