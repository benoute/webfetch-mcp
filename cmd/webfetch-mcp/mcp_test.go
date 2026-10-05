package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/benoute/webfetch-mcp"
	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTruncateWithMarker(t *testing.T) {
	tests := []struct {
		name    string
		label   string
		s       string
		budget  int
		want    string
		wantCut bool
	}{
		{"no budget", "Markdown", "hello", 0, "hello", false},
		{"fits", "Markdown", "hello", 10, "hello", false},
		{"exact", "Markdown", "hello", 5, "hello", false},
		{"over", "Markdown", "# Title\n\nHello world here", 10,
			"[Markdown truncated: 25 bytes > max_content_bytes 10]\n# Title\n\nH\n\n... (truncated)", true},
		{"rune at cut", "Text", "aé", 2,
			"[Text truncated: 3 bytes > max_content_bytes 2]\na\n\n... (truncated)", true},
		{"label JSON", "JSON", `{"id":1,"name":"xxxxxxxxxxxxxx"}`, 20,
			"[JSON truncated: 32 bytes > max_content_bytes 20]\n{\"id\":1,\"name\":\"xxxx\n\n... (truncated)", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, cut := truncateWithMarker(tt.label, tt.s, tt.budget)
			if got != tt.want || cut != tt.wantCut {
				t.Errorf("truncateWithMarker(%q, %q, %d) =\n%q, %v\nwant\n%q, %v", tt.label, tt.s, tt.budget, got, cut, tt.want, tt.wantCut)
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

// result returns a webfetch.Result. content goes to JSON for KindJSON, else to Text.
func result(kind webfetch.Kind, status int, ct, content string) *webfetch.Result {
	r := &webfetch.Result{Kind: kind, StatusCode: status, ContentType: ct, MediaType: strings.Split(ct, ";")[0]}
	if kind == webfetch.KindJSON {
		r.JSON = json.RawMessage(content)
	} else {
		r.Text = content
	}
	return r
}

func TestToolResult(t *testing.T) {
	const obj = `{"id": 1, "name": "x"}`
	const jsonCT = "application/json"
	const meta200 = `"status":200,"contentType":"application/json"`

	tests := []struct {
		name   string
		res    *webfetch.Result
		head   string
		budget int
		want   string // wire JSON
	}{
		// R1
		{
			name: "R1 JSON object, no budget",
			res:  result(webfetch.KindJSON, 200, jsonCT, obj),
			want: `{"content":[],"structuredContent":{` + meta200 + `,"json":{"id":1,"name":"x"}}}`,
		},
		{
			name: "R1 JSON array",
			res:  result(webfetch.KindJSON, 200, jsonCT, `[1,2]`),
			want: `{"content":[],"structuredContent":{` + meta200 + `,"json":[1,2]}}`,
		},
		{
			name: "R1 JSON null",
			res:  result(webfetch.KindJSON, 200, jsonCT, `null`),
			want: `{"content":[],"structuredContent":{` + meta200 + `,"json":null}}`,
		},
		{
			name: "R1 raw Content-Type kept",
			res:  result(webfetch.KindJSON, 201, "application/json; charset=utf-8", `1`),
			want: `{"content":[],"structuredContent":{"status":201,"contentType":"application/json; charset=utf-8","json":1}}`,
		},
		{
			name:   "R1 JSON len == budget",
			res:    result(webfetch.KindJSON, 200, jsonCT, obj),
			budget: len(obj),
			want:   `{"content":[],"structuredContent":{` + meta200 + `,"json":{"id":1,"name":"x"}}}`,
		},
		// R2
		{
			name:   "R2 JSON len == budget+1",
			res:    result(webfetch.KindJSON, 200, jsonCT, obj),
			budget: len(obj) - 1,
			want: `{"content":[{"type":"text","text":"[JSON truncated: 22 bytes \u003e max_content_bytes 21]\n` +
				`{\"id\": 1, \"name\": \"x\"\n\n... (truncated)"}],` +
				`"structuredContent":{` + meta200 + `,"truncated":true}}`,
		},
		// R3
		{
			name: "R3 invalid JSON",
			res:  result(webfetch.KindInvalidJSON, 200, jsonCT, ")]}'\n{\"a\":1}"),
			want: `{"content":[{"type":"text","text":"[invalid JSON from server, application/json]\n)]}'\n{\"a\":1}"}],` +
				`"structuredContent":{` + meta200 + `}}`,
		},
		{
			name:   "R3 invalid JSON over budget",
			res:    result(webfetch.KindInvalidJSON, 200, "application/problem+json", "abcdef"),
			budget: 3,
			want: `{"content":[{"type":"text","text":"[invalid JSON from server, application/problem+json]\n` +
				`[Text truncated: 6 bytes \u003e max_content_bytes 3]\nabc\n\n... (truncated)"}],` +
				`"structuredContent":{"status":200,"contentType":"application/problem+json","truncated":true}}`,
		},
		// R4
		{
			name: "R4 Markdown",
			res:  result(webfetch.KindMarkdown, 200, "text/html", "# Title"),
			want: `{"content":[{"type":"text","text":"# Title"}],"structuredContent":{"status":200,"contentType":"text/html"}}`,
		},
		{
			name:   "R4 Markdown over budget",
			res:    result(webfetch.KindMarkdown, 200, "text/html", "# Title\n\nHello world here"),
			budget: 10,
			want: `{"content":[{"type":"text","text":"[Markdown truncated: 25 bytes \u003e max_content_bytes 10]\n` +
				`# Title\n\nH\n\n... (truncated)"}],"structuredContent":{"status":200,"contentType":"text/html","truncated":true}}`,
		},
		{
			name: "R4 empty Markdown → content []",
			res:  result(webfetch.KindMarkdown, 200, "text/html", ""),
			want: `{"content":[],"structuredContent":{"status":200,"contentType":"text/html"}}`,
		},
		// R4t
		{
			name: "R4t text",
			res:  result(webfetch.KindText, 200, "text/markdown; charset=utf-8", "# Doc"),
			want: `{"content":[{"type":"text","text":"# Doc"}],"structuredContent":{"status":200,"contentType":"text/markdown; charset=utf-8"}}`,
		},
		{
			name:   "R4t text over budget",
			res:    result(webfetch.KindText, 200, "text/markdown; charset=utf-8", "# Doc\n\nHello world here"),
			budget: 10,
			want: `{"content":[{"type":"text","text":"[Text truncated: 23 bytes \u003e max_content_bytes 10]\n# Doc\n\nHel\n\n... (truncated)"}],` +
				`"structuredContent":{"status":200,"contentType":"text/markdown; charset=utf-8","truncated":true}}`,
		},
		// R4e
		{
			name: "R4e empty body, no Content-Type",
			res:  result(webfetch.KindNone, 204, "", ""),
			want: `{"content":[],"structuredContent":{"status":204,"contentType":null}}`,
		},
		// R5
		{
			name: "R5 404 JSON",
			res:  result(webfetch.KindJSON, 404, jsonCT, `{"message":"Not Found"}`),
			head: "unexpected status code: 404",
			want: `{"content":[{"type":"text","text":"unexpected status code: 404"}],` +
				`"structuredContent":{"status":404,"contentType":"application/json","json":{"message":"Not Found"}},"isError":true}`,
		},
		// R5'
		{
			name:   "R5' 404 JSON over budget",
			res:    result(webfetch.KindJSON, 404, jsonCT, `{"message":"Not Found"}`),
			head:   "unexpected status code: 404",
			budget: 5,
			want: `{"content":[{"type":"text","text":"unexpected status code: 404\n[JSON truncated: 23 bytes \u003e max_content_bytes 5]\n` +
				`{\"mes\n\n... (truncated)"}],"structuredContent":{"status":404,"contentType":"application/json","truncated":true},"isError":true}`,
		},
		// R6
		{
			name: "R6 404 HTML",
			res:  result(webfetch.KindMarkdown, 404, "text/html", "# Not Found"),
			head: "unexpected status code: 404",
			want: `{"content":[{"type":"text","text":"unexpected status code: 404\n# Not Found"}],` +
				`"structuredContent":{"status":404,"contentType":"text/html"},"isError":true}`,
		},
		{
			name: "R6 404 HTML, empty Markdown → head only",
			res:  result(webfetch.KindMarkdown, 404, "text/html", ""),
			head: "unexpected status code: 404",
			want: `{"content":[{"type":"text","text":"unexpected status code: 404"}],` +
				`"structuredContent":{"status":404,"contentType":"text/html"},"isError":true}`,
		},
		{
			name: "R6 400 invalid JSON",
			res:  result(webfetch.KindInvalidJSON, 400, jsonCT, "oops"),
			head: "unexpected status code: 400",
			want: `{"content":[{"type":"text","text":"unexpected status code: 400\n[invalid JSON from server, application/json]\noops"}],` +
				`"structuredContent":{"status":400,"contentType":"application/json"},"isError":true}`,
		},
		// R6'
		{
			name: "R6' 500 no body",
			res:  result(webfetch.KindNone, 500, "", ""),
			head: "unexpected status code: 500",
			want: `{"content":[{"type":"text","text":"unexpected status code: 500"}],` +
				`"structuredContent":{"status":500,"contentType":null},"isError":true}`,
		},
		// R8
		{
			name: "R8 response error",
			res:  result(webfetch.KindNone, 200, "image/png", ""),
			head: "unsupported content type: image/png (expected HTML, PDF, JSON or text)",
			want: `{"content":[{"type":"text","text":"unsupported content type: image/png (expected HTML, PDF, JSON or text)"}],` +
				`"structuredContent":{"status":200,"contentType":"image/png"},"isError":true}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := wire(t, toolResult(tt.res, tt.head, tt.budget)); got != tt.want {
				t.Errorf("wire =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestToolResult_ValueExact(t *testing.T) {
	body := `{"n":12345678901234567890,"f":1.0,"b":2,"a":1,"s":"<&>"}`
	r := toolResult(result(webfetch.KindJSON, 200, "application/json", body), "", 0)
	var got struct {
		StructuredContent struct {
			JSON json.RawMessage `json:"json"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal([]byte(wire(t, r)), &got); err != nil {
		t.Fatal(err)
	}
	// Same value: big int, 1.0 and key order kept. HTML characters are escaped
	// by encoding/json, which does not change the value.
	want := `{"n":12345678901234567890,"f":1.0,"b":2,"a":1,"s":"\u003c\u0026\u003e"}`
	if string(got.StructuredContent.JSON) != want {
		t.Errorf("json = %s, want %s", got.StructuredContent.JSON, want)
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

// testServer returns a test server with one handler per path.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	bigHTML := "<p>" + strings.Repeat("word ", 30000) + "end</p>" // > 100000 bytes of Markdown

	mux := http.NewServeMux()
	handle := func(path string, status int, ct, body string) {
		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if ct != "" {
				w.Header().Set("Content-Type", ct)
			} else {
				w.Header()["Content-Type"] = nil // no header, no sniffing
			}
			w.WriteHeader(status)
			w.Write([]byte(body))
		})
	}
	handle("/obj", 200, "application/json", `{"id":1,"name":"x"}`)
	handle("/arr", 200, "application/json; charset=utf-8", `[1,"a",null]`)
	handle("/invalid", 200, "application/json", `{"a":`)
	handle("/404", 404, "application/json", `{"message":"Not Found"}`)
	handle("/404html", 404, "text/html", "<h1>Gone</h1>")
	handle("/500", 500, "", "")
	handle("/ndjson", 200, "application/x-ndjson", "{}\n{}\n")
	handle("/text", 200, "text/markdown; charset=utf-8", "# Doc\n\nHello")
	handle("/empty", 204, "", "")
	handle("/big", 200, "text/html", bigHTML)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// caller returns a function that calls the fetch tool with args.
func caller(t *testing.T) func(args map[string]any) *mcp.CallToolResult {
	session := newTestSession(t)
	return func(args map[string]any) *mcp.CallToolResult {
		t.Helper()
		r, err := session.CallTool(context.Background(), &mcp.CallToolParams{Name: "fetch", Arguments: args})
		if err != nil {
			t.Fatalf("CallTool(%v): %v", args, err)
		}
		return r
	}
}

// scJSON returns the structured content of r as compact JSON ("" if absent).
func scJSON(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r.StructuredContent == nil {
		return ""
	}
	b, err := json.Marshal(r.StructuredContent)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestFetchTool_RoundTrip(t *testing.T) {
	srv := testServer(t)
	call := caller(t)

	tests := []struct {
		name     string
		args     map[string]any
		wantErr  bool
		wantText string // "" = no content; prefix match if wantPrefix
		prefix   bool
		wantSC   string // compact JSON with sorted keys (client decodes to map); "" = absent
	}{
		{
			name:   "JSON object → sc.json",
			args:   map[string]any{"url": srv.URL + "/obj"},
			wantSC: `{"contentType":"application/json","json":{"id":1,"name":"x"},"status":200}`,
		},
		{
			name:   "JSON array → sc.json",
			args:   map[string]any{"url": srv.URL + "/arr"},
			wantSC: `{"contentType":"application/json; charset=utf-8","json":[1,"a",null],"status":200}`,
		},
		{
			name:     "JSON over budget → truncated text",
			args:     map[string]any{"url": srv.URL + "/obj", "max_content_bytes": 10},
			wantText: "[JSON truncated: 19 bytes > max_content_bytes 10]\n{\"id\":1,\"n\n\n... (truncated)",
			wantSC:   `{"contentType":"application/json","status":200,"truncated":true}`,
		},
		{
			name:     "invalid JSON → marker + body",
			args:     map[string]any{"url": srv.URL + "/invalid"},
			wantText: "[invalid JSON from server, application/json]\n{\"a\":",
			wantSC:   `{"contentType":"application/json","status":200}`,
		},
		{
			name:     "text/markdown → text",
			args:     map[string]any{"url": srv.URL + "/text"},
			wantText: "# Doc\n\nHello",
			wantSC:   `{"contentType":"text/markdown; charset=utf-8","status":200}`,
		},
		{
			name:   "204 empty → content []",
			args:   map[string]any{"url": srv.URL + "/empty"},
			wantSC: `{"contentType":null,"status":204}`,
		},
		{
			name:     "404 JSON → status line + sc.json",
			args:     map[string]any{"url": srv.URL + "/404"},
			wantErr:  true,
			wantText: "unexpected status code: 404",
			wantSC:   `{"contentType":"application/json","json":{"message":"Not Found"},"status":404}`,
		},
		{
			name:     "404 HTML → status line + Markdown",
			args:     map[string]any{"url": srv.URL + "/404html"},
			wantErr:  true,
			wantText: "unexpected status code: 404\n# Gone",
			wantSC:   `{"contentType":"text/html","status":404}`,
		},
		{
			name:     "500 no body → status line",
			args:     map[string]any{"url": srv.URL + "/500"},
			wantErr:  true,
			wantText: "unexpected status code: 500",
			wantSC:   `{"contentType":null,"status":500}`,
		},
		{
			name:     "NDJSON → response error",
			args:     map[string]any{"url": srv.URL + "/ndjson"},
			wantErr:  true,
			wantText: "unsupported content type: application/x-ndjson (expected HTML, PDF, JSON or text)",
			wantSC:   `{"contentType":"application/x-ndjson","status":200}`,
		},
		{
			name:     "fetch error → no sc",
			args:     map[string]any{"url": "http://127.0.0.1:1/"},
			wantErr:  true,
			wantText: "failed to fetch URL: ",
			prefix:   true,
		},
		{
			name:     "max_content_bytes: -1 → error",
			args:     map[string]any{"url": srv.URL + "/obj", "max_content_bytes": -1},
			wantErr:  true,
			wantText: "max_content_bytes must be >= 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := call(tt.args)
			if r.IsError != tt.wantErr {
				t.Errorf("IsError = %v, want %v", r.IsError, tt.wantErr)
			}
			if tt.wantText == "" {
				if len(r.Content) != 0 {
					t.Errorf("Content = %+v, want empty", r.Content)
				}
			} else if got := resultText(t, r); got != tt.wantText && !(tt.prefix && strings.HasPrefix(got, tt.wantText)) {
				t.Errorf("text = %q, want %q", got, tt.wantText)
			}
			if got := scJSON(t, r); got != tt.wantSC {
				t.Errorf("structuredContent = %s, want %s", got, tt.wantSC)
			}
		})
	}

	t.Run("no budget → full Markdown", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/big"})
		got := resultText(t, r)
		if r.IsError || len(got) <= 100000 || !strings.HasSuffix(strings.TrimSpace(got), "end") {
			t.Errorf("IsError = %v, len = %d, want full Markdown > 100000 bytes", r.IsError, len(got))
		}
	})

	t.Run("old max_content_tokens → schema error", func(t *testing.T) {
		r := call(map[string]any{"url": srv.URL + "/obj", "max_content_tokens": 10})
		if got := resultText(t, r); !r.IsError || !strings.Contains(got, "max_content_tokens") {
			t.Errorf("IsError = %v, text = %q", r.IsError, got)
		}
	})
}

func TestFetchTool_OutputValid(t *testing.T) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(outputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatal(err)
	}

	srv := testServer(t)
	call := caller(t)
	paths := []string{"/obj", "/arr", "/invalid", "/404", "/404html", "/500", "/ndjson", "/text", "/empty", "/big"}
	for _, path := range paths {
		for _, budget := range []int{0, 5} {
			t.Run(fmt.Sprintf("%s budget %d", path, budget), func(t *testing.T) {
				r := call(map[string]any{"url": srv.URL + path, "max_content_bytes": budget})
				if r.StructuredContent == nil {
					t.Fatal("structuredContent absent")
				}
				if err := resolved.Validate(r.StructuredContent); err != nil {
					t.Errorf("structuredContent %s does not validate: %v", scJSON(t, r), err)
				}
			})
		}
	}

	t.Run("schema rejects a bad envelope", func(t *testing.T) {
		var bad any
		json.Unmarshal([]byte(`{"status":"200"}`), &bad)
		if resolved.Validate(bad) == nil {
			t.Error("Validate(bad) = nil, want error")
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
	if tool.Description != toolDescription {
		t.Errorf("Description = %q", tool.Description)
	}

	var want any
	if err := json.Unmarshal(outputSchema, &want); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tool.OutputSchema, want) {
		got, _ := json.Marshal(tool.OutputSchema)
		t.Errorf("OutputSchema = %s, want outputschema.json", got)
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
