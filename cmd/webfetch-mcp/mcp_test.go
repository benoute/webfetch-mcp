package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/benoute/webfetch-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTruncateWithMarker(t *testing.T) {
	tests := []struct {
		name   string
		label  string
		s      string
		budget int
		want   string
	}{
		{"no budget", "Markdown", "hello", 0, "hello"},
		{"fits", "Markdown", "hello", 10, "hello"},
		{"exact", "Markdown", "hello", 5, "hello"},
		{"over", "Markdown", "# Title\n\nHello world here", 10,
			"[Markdown truncated: 25 bytes > max_content_bytes 10]\n# Title\n\nH\n\n... (truncated)"},
		{"rune at cut", "Text", "aé", 2,
			"[Text truncated: 3 bytes > max_content_bytes 2]\na\n\n... (truncated)"},
		{"label JSON", "JSON", `{"id":1,"name":"xxxxxxxxxxxxxx"}`, 20,
			"[JSON truncated: 32 bytes > max_content_bytes 20]\n{\"id\":1,\"name\":\"xxxx\n\n... (truncated)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateWithMarker(tt.label, tt.s, tt.budget)
			if got != tt.want {
				t.Errorf("truncateWithMarker(%q, %q, %d) =\n%q\nwant\n%q", tt.label, tt.s, tt.budget, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

// wire marshals r as the server sends it.
func wire(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestToolResult(t *testing.T) {
	obj := json.RawMessage(`{"id": 1, "name": "x"}`)
	big := json.RawMessage(`{"n":12345678901234567890}`)

	tests := []struct {
		name   string
		res    *webfetch.Result
		budget int
		want   string // wire JSON
	}{
		{
			name: "JSON object, no budget",
			res:  &webfetch.Result{Kind: webfetch.KindJSON, MediaType: "application/json", JSON: obj},
			want: `{"content":[],"structuredContent":{"id":1,"name":"x"}}`,
		},
		{
			name: "JSON array",
			res:  &webfetch.Result{Kind: webfetch.KindJSON, MediaType: "application/json", JSON: json.RawMessage(`[1,2]`)},
			want: `{"content":[],"structuredContent":[1,2]}`,
		},
		{
			name: "JSON big int exact",
			res:  &webfetch.Result{Kind: webfetch.KindJSON, MediaType: "application/json", JSON: big},
			want: `{"content":[],"structuredContent":{"n":12345678901234567890}}`,
		},
		{
			name:   "JSON len == budget",
			res:    &webfetch.Result{Kind: webfetch.KindJSON, MediaType: "application/json", JSON: obj},
			budget: len(obj),
			want:   `{"content":[],"structuredContent":{"id":1,"name":"x"}}`,
		},
		{
			name:   "JSON len == budget+1",
			res:    &webfetch.Result{Kind: webfetch.KindJSON, MediaType: "application/json", JSON: obj},
			budget: len(obj) - 1,
			want: `{"content":[{"type":"text","text":"[JSON truncated: 22 bytes \u003e max_content_bytes 21]\n` +
				`{\"id\": 1, \"name\": \"x\"\n\n... (truncated)"}]}`,
		},
		{
			name: "invalid JSON",
			res:  &webfetch.Result{Kind: webfetch.KindText, MediaType: "application/json", Text: ")]}'\n{\"a\":1}"},
			want: `{"content":[{"type":"text","text":"[invalid JSON from server, application/json]\n)]}'\n{\"a\":1}"}]}`,
		},
		{
			name:   "invalid JSON over budget",
			res:    &webfetch.Result{Kind: webfetch.KindText, MediaType: "application/problem+json", Text: "abcdef"},
			budget: 3,
			want: `{"content":[{"type":"text","text":"[invalid JSON from server, application/problem+json]\n` +
				`[Text truncated: 6 bytes \u003e max_content_bytes 3]\nabc\n\n... (truncated)"}]}`,
		},
		{
			name: "Markdown",
			res:  &webfetch.Result{Kind: webfetch.KindMarkdown, MediaType: "text/html", Text: "# Title"},
			want: `{"content":[{"type":"text","text":"# Title"}]}`,
		},
		{
			name:   "Markdown over budget",
			res:    &webfetch.Result{Kind: webfetch.KindMarkdown, MediaType: "text/html", Text: "# Title\n\nHello world here"},
			budget: 10,
			want: `{"content":[{"type":"text","text":"[Markdown truncated: 25 bytes \u003e max_content_bytes 10]\n` +
				`# Title\n\nH\n\n... (truncated)"}]}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wire(t, toolResult(tt.res, tt.budget)); got != tt.want {
				t.Errorf("wire =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestDefaultTimeout(t *testing.T) {
	if defaultTimeout.String() != "10s" {
		t.Errorf("defaultTimeout = %v, want 10s", defaultTimeout)
	}
}

// newTestSession connects an in-memory MCP client to setupMCPServer.
func newTestSession(t *testing.T) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := setupMCPServer().Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

// resultText returns the text of the only content block of r.
func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if len(r.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1: %+v", len(r.Content), r.Content)
	}
	tc, ok := r.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] = %T, want *mcp.TextContent", r.Content[0])
	}
	return tc.Text
}

func TestFetchTool_RoundTrip(t *testing.T) {
	bigHTML := "<p>" + strings.Repeat("word ", 30000) + "end</p>" // > 100000 bytes of Markdown

	mux := http.NewServeMux()
	handle := func(path string, status int, ct, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", ct)
			w.WriteHeader(status)
			w.Write([]byte(body))
		})
	}
	handle("/obj", 200, "application/json", `{"id":1,"name":"x"}`)
	handle("/arr", 200, "application/json; charset=utf-8", `[1,"a",null]`)
	handle("/invalid", 200, "application/json", `{"a":`)
	handle("/404", 404, "application/json", `{"message":"Not Found"}`)
	handle("/ndjson", 200, "application/x-ndjson", "{}\n{}\n")
	handle("/big", 200, "text/html", bigHTML)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	session := newTestSession(t)
	call := func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "fetch", Arguments: args})
		if err != nil {
			t.Fatalf("CallTool(%v): %v", args, err)
		}
		return r
	}

	t.Run("JSON object → structured content", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/obj"})
		if r.IsError || len(r.Content) != 0 {
			t.Fatalf("IsError = %v, Content = %+v; want false, empty", r.IsError, r.Content)
		}
		got, _ := json.Marshal(r.StructuredContent)
		if string(got) != `{"id":1,"name":"x"}` {
			t.Errorf("StructuredContent = %s", got)
		}
	})

	t.Run("JSON array → structured content", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/arr"})
		if r.IsError || len(r.Content) != 0 {
			t.Fatalf("IsError = %v, Content = %+v; want false, empty", r.IsError, r.Content)
		}
		got, _ := json.Marshal(r.StructuredContent)
		if string(got) != `[1,"a",null]` {
			t.Errorf("StructuredContent = %s", got)
		}
	})

	t.Run("JSON over budget → truncated text", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/obj", "max_content_bytes": 10})
		if r.IsError || r.StructuredContent != nil {
			t.Fatalf("IsError = %v, StructuredContent = %v", r.IsError, r.StructuredContent)
		}
		want := "[JSON truncated: 19 bytes > max_content_bytes 10]\n{\"id\":1,\"n\n\n... (truncated)"
		if got := resultText(t, r); got != want {
			t.Errorf("text = %q, want %q", got, want)
		}
	})

	t.Run("invalid JSON → marker + body", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/invalid"})
		want := "[invalid JSON from server, application/json]\n{\"a\":"
		if got := resultText(t, r); r.IsError || got != want {
			t.Errorf("IsError = %v, text = %q; want false, %q", r.IsError, got, want)
		}
	})

	t.Run("404 JSON → error with body", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/404"})
		want := `unexpected status code: 404: {"message":"Not Found"}`
		if got := resultText(t, r); !r.IsError || got != want {
			t.Errorf("IsError = %v, text = %q; want true, %q", r.IsError, got, want)
		}
	})

	t.Run("NDJSON → unsupported", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/ndjson"})
		if got := resultText(t, r); !r.IsError || !strings.Contains(got, "unsupported content type") {
			t.Errorf("IsError = %v, text = %q", r.IsError, got)
		}
	})

	t.Run("no budget → full Markdown", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/big"})
		got := resultText(t, r)
		if r.IsError || len(got) <= 100000 || !strings.HasSuffix(strings.TrimSpace(got), "end") {
			t.Errorf("IsError = %v, len = %d, want full Markdown > 100000 bytes", r.IsError, len(got))
		}
	})

	t.Run("max_content_bytes: -1 → error", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/obj", "max_content_bytes": -1})
		if got := resultText(t, r); !r.IsError || got != "max_content_bytes must be >= 0" {
			t.Errorf("IsError = %v, text = %q", r.IsError, got)
		}
	})

	t.Run("old max_content_tokens → schema error", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/obj", "max_content_tokens": 10})
		if got := resultText(t, r); !r.IsError || !strings.Contains(got, "max_content_tokens") {
			t.Errorf("IsError = %v, text = %q", r.IsError, got)
		}
	})
}

func TestFetchTool_Schema(t *testing.T) {
	session := newTestSession(t)
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "fetch" {
		t.Fatalf("tools = %+v, want only fetch", tools.Tools)
	}
	tool := tools.Tools[0]
	if tool.OutputSchema != nil {
		t.Errorf("OutputSchema = %v, want nil", tool.OutputSchema)
	}
	schema, _ := json.Marshal(tool.InputSchema)
	for _, want := range []string{`"max_content_bytes"`, `(default: 10s)`, `"additionalProperties":false`} {
		if !strings.Contains(strings.ToLower(string(schema)), strings.ToLower(want)) {
			t.Errorf("input schema %s does not contain %s", schema, want)
		}
	}
	if strings.Contains(string(schema), "max_content_tokens") {
		t.Errorf("input schema still has max_content_tokens: %s", schema)
	}
}
