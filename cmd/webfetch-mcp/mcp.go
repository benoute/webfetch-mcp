package main

import (
	"context"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/benoute/webfetch-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultTimeout = 10 * time.Second

type fetchToolInput struct {
	URL             string `json:"url" jsonschema:"The URL to fetch (required)"`
	Timeout         string `json:"timeout,omitempty" jsonschema:"Request timeout (default: 10s)"`
	MaxContentBytes int    `json:"max_content_bytes,omitempty" jsonschema:"Maximum content size in bytes. Text is truncated; JSON larger than this is returned as truncated text. Default: no limit."`
}

// setupMCPServer creates and configures the MCP server with the fetch tool
func setupMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "webfetch", Version: buildVersion()}, nil)

	// Add fetch tool
	mcp.AddTool(server, &mcp.Tool{
		Name: "fetch",
		Description: "Fetches a URL. Converts HTML or PDF content to Markdown. " +
			"Returns JSON as structured content.",
	}, func(
		ctx context.Context,
		req *mcp.CallToolRequest,
		input fetchToolInput,
	) (*mcp.CallToolResult, any, error) {
		return handleFetch(ctx, input)
	})

	return server
}

func handleFetch(ctx context.Context, input fetchToolInput) (
	*mcp.CallToolResult,
	any,
	error,
) {
	if input.URL == "" {
		return errorResult("URL is required"), nil, nil
	}

	// Parse timeout from input or use default
	timeout := defaultTimeout
	if input.Timeout != "" {
		parsedTimeout, err := time.ParseDuration(input.Timeout)
		if err != nil {
			return errorResult("invalid timeout format: " + err.Error()), nil, nil
		}
		timeout = parsedTimeout
	}

	if input.MaxContentBytes < 0 {
		return errorResult("max_content_bytes must be >= 0"), nil, nil
	}

	res, err := webfetch.Fetch(ctx, input.URL, timeout)
	if err != nil {
		return errorResult(err.Error()), nil, nil
	}

	return toolResult(res, input.MaxContentBytes), nil, nil
}

// toolResult packages res as an MCP tool result. budget is max_content_bytes
// (0 = no limit).
func toolResult(res *webfetch.Result, budget int) *mcp.CallToolResult {
	// JSON within budget: structured content only, no text copy. Content is an
	// empty slice (not nil) so that it marshals as [] and not null.
	if res.Kind == webfetch.KindJSON && (budget == 0 || len(res.JSON) <= budget) {
		return &mcp.CallToolResult{
			Content:           []mcp.Content{},
			StructuredContent: res.JSON,
		}
	}

	// Everything else: one text content block (truncated to budget).
	var text string
	switch res.Kind {
	case webfetch.KindJSON: // over budget
		text = truncateWithMarker("JSON", string(res.JSON), budget)
	case webfetch.KindText:
		text = invalidJSONMarker(res.MediaType) + truncateWithMarker("Text", res.Text, budget)
	default: // webfetch.KindMarkdown
		text = truncateWithMarker("Markdown", res.Text, budget)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// truncateWithMarker returns s unchanged if budget is 0 or s fits in budget
// bytes. Else it returns a marker line, the first budget bytes of s (cut on a
// UTF-8 rune boundary) and a "... (truncated)" suffix.
func truncateWithMarker(label, s string, budget int) string {
	if budget == 0 || len(s) <= budget {
		return s
	}
	i := budget
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return fmt.Sprintf("[%s truncated: %d bytes > max_content_bytes %d]\n", label, len(s), budget) +
		s[:i] + "\n\n... (truncated)"
}

// invalidJSONMarker is the first line of a result for a body that has a JSON
// media type but is not valid JSON.
func invalidJSONMarker(mt string) string {
	return "[invalid JSON from server, " + mt + "]\n"
}
