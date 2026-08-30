// mcptestserver is a minimal stdio MCP server for agentkit integration tests.
// It exposes write_file and read_text_file against a single allowed root directory.
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mcptestserver <root>")
		os.Exit(2)
	}
	root, err := filepath.Abs(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	s := server.NewMCPServer("mcptestserver", "1.0.0", server.WithToolCapabilities(true))

	writeTool := mcp.NewTool("write_file",
		mcp.WithDescription("Write a text file under the allowed root"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative file path")),
		mcp.WithString("content", mcp.Required(), mcp.Description("File content")),
	)
	s.AddTool(writeTool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		content, err := req.RequireString("content")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		full, err := resolvePath(root, path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText("ok"), nil
	})

	readTool := mcp.NewTool("read_text_file",
		mcp.WithDescription("Read a text file under the allowed root"),
		mcp.WithString("path", mcp.Required(), mcp.Description("Relative file path")),
	)
	s.AddTool(readTool, func(_ context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		path, err := req.RequireString("path")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		full, err := resolvePath(root, path)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		raw, err := os.ReadFile(full)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(raw)), nil
	})

	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func resolvePath(root, rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", fmt.Errorf("path is required")
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(rel)
	if clean == "." || strings.HasPrefix(clean, "..") {
		return "", fmt.Errorf("invalid path %q", rel)
	}
	full := filepath.Join(root, clean)
	abs, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(abs, rootAbs+string(filepath.Separator)) && abs != rootAbs {
		return "", fmt.Errorf("path escapes allowed root")
	}
	return abs, nil
}
