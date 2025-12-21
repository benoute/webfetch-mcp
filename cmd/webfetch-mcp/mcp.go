package main

import (
	"context"
	"time"

	"github.com/benoute/webfetch"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const (
	defaultTimeout          = 5 * time.Second
	defaultMaxContentTokens = 100000
)

type webfetchToolInput struct {
	URL              string `json:"url" jsonschema:"description=The URL to fetch"`
	Timeout          string `json:"timeout,omitempty" jsonschema:"description=Request timeout (default: 5s)"`
	MaxContentTokens int    `json:"max_content_tokens,omitempty" jsonschema:"description=Maximum content length - truncated if exceeded (default: 100000)"`
}

// setupMCPServer creates and configures the MCP server with the webfetch tool
func setupMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "webfetch", Version: "v1.0.0"}, nil)

	// Add webfetch tool
	mcp.AddTool(server, &mcp.Tool{
		Name:        "webfetch",
		Description: "Fetches a URL and converts its HTML content to Markdown.",
	}, func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input webfetchToolInput,
	) (*mcp.CallToolResult, any, error) {
		return handleWebfetch(ctx, input)
	})

	return server
}

func handleWebfetch(ctx context.Context, input webfetchToolInput) (
	*mcp.CallToolResult,
	any,
	error,
) {
	if input.URL == "" {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: "URL is required"},
			},
			IsError: true,
		}, nil, nil
	}

	// Parse timeout from input or use default
	timeout := defaultTimeout
	if input.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(input.Timeout)
		if err != nil {
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: "invalid timeout format: " + err.Error()},
				},
				IsError: true,
			}, nil, nil
		}
		timeout = parsedTimeout
	}

	// Use max content tokens from input or default
	maxContentTokens := defaultMaxContentTokens
	if input.MaxContentTokens > 0 {
		maxContentTokens = input.MaxContentTokens
	}

	markdown, err := webfetch.FetchAndConvert(ctx, input.URL, timeout)
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: err.Error()},
			},
			IsError: true,
		}, nil, nil
	}

	// Truncate content if it exceeds maxContentTokens
	if maxContentTokens > 0 && len(markdown) > maxContentTokens {
		markdown = markdown[:maxContentTokens] + "\n\n... (truncated)"
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: markdown},
		},
	}, nil, nil
}
