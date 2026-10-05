package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/benoute/webfetch-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const defaultTimeout = 10 * time.Second

const toolDescription = "Fetches a URL with HTTP GET. HTML and PDF are converted to Markdown, and text is returned " +
	"as is, in the text content. A JSON body is in structuredContent.json (see outputSchema). " +
	"A non-2xx status is an error result."

type fetchToolInput struct {
	URL             string `json:"url" jsonschema:"The URL to fetch (required)"`
	Timeout         string `json:"timeout,omitempty" jsonschema:"Request timeout (default: 10s)"`
	MaxContentBytes int    `json:"max_content_bytes,omitempty" jsonschema:"Maximum content size in bytes. Text is truncated; JSON larger than this is returned as truncated text. Default: no limit."`
}

// envelope is the structuredContent of every fetch result that has an HTTP
// response. It matches outputSchema.
type envelope struct {
	Status      int             `json:"status"`
	ContentType *string         `json:"contentType"`         // nil → null
	JSON        json.RawMessage `json:"json,omitempty"`      // len 0 → absent; body `null` → present
	Truncated   bool            `json:"truncated,omitempty"` // false → absent
}

// outputSchema is the JSON Schema of envelope, sent as is in tools/list.
//
//go:embed outputschema.json
var outputSchema json.RawMessage

// setupMCPServer creates and configures the MCP server with the fetch tool
func setupMCPServer() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "webfetch", Version: buildVersion()}, nil)

	// Out is any and the handler always returns a nil out: the SDK sends the
	// result as built, so structuredContent keeps the exact JSON value of the
	// body. The handler is responsible for conformance to outputSchema.
	mcp.AddTool(server, &mcp.Tool{
		Name:         "fetch",
		Description:  toolDescription,
		OutputSchema: outputSchema,
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
	// --- Validate input ---
	if input.URL == "" {
		return errorResult("URL is required"), nil, nil
	}

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

	// --- Error with a response → envelope + head line; without → plain error ---
	var head string
	var se *webfetch.StatusError
	var re *webfetch.ResponseError
	switch {
	case errors.As(err, &se):
		res, head = se.Result, fmt.Sprintf("unexpected status code: %d", se.StatusCode)
	case errors.As(err, &re):
		res, head = re.Result, re.Error()
	case err != nil:
		return errorResult(err.Error()), nil, nil
	}

	return toolResult(res, head, input.MaxContentBytes), nil, nil
}

// toolResult packages res as an MCP tool result. head is the first content
// line of an error result ("" for success). budget is max_content_bytes
// (0 = no limit).
func toolResult(res *webfetch.Result, head string, budget int) *mcp.CallToolResult {
	// --- Envelope metadata ---
	env := envelope{Status: res.StatusCode}
	if res.ContentType != "" {
		env.ContentType = &res.ContentType
	}

	// --- Body: JSON within budget → envelope; other body → text ---
	var lines []string
	if head != "" {
		lines = append(lines, head)
	}
	var text string
	switch res.Kind {
	case webfetch.KindJSON:
		if budget == 0 || len(res.JSON) <= budget {
			env.JSON = res.JSON
		} else {
			text, env.Truncated = truncateWithMarker("JSON", string(res.JSON), budget)
		}
	case webfetch.KindInvalidJSON:
		text, env.Truncated = truncateWithMarker("Text", res.Text, budget)
		text = "[invalid JSON from server, " + res.MediaType + "]\n" + text
	case webfetch.KindText:
		text, env.Truncated = truncateWithMarker("Text", res.Text, budget)
	case webfetch.KindMarkdown:
		text, env.Truncated = truncateWithMarker("Markdown", res.Text, budget)
	} // KindNone: no text
	if text != "" {
		lines = append(lines, text)
	}

	// --- Wire result: content is [] (not null) when there is no text ---
	content := []mcp.Content{}
	if len(lines) > 0 {
		content = append(content, &mcp.TextContent{Text: strings.Join(lines, "\n")})
	}
	sc, _ := json.Marshal(env) // cannot fail: env.JSON is valid JSON
	return &mcp.CallToolResult{
		Content:           content,
		StructuredContent: json.RawMessage(sc),
		IsError:           head != "",
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg}},
		IsError: true,
	}
}

// truncateWithMarker returns s and false if budget is 0 or s fits in budget
// bytes. Else it returns a marker line, the first budget bytes of s (cut on a
// UTF-8 rune boundary), a "... (truncated)" suffix and true.
func truncateWithMarker(label, s string, budget int) (string, bool) {
	if budget == 0 || len(s) <= budget {
		return s, false
	}
	i := budget
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return fmt.Sprintf("[%s truncated: %d bytes > max_content_bytes %d]\n", label, len(s), budget) +
		s[:i] + "\n\n... (truncated)", true
}
