package webfetch

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func Test_mediaType(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"application/json; charset=utf-8", "application/json"},
		{"Application/Problem+JSON", "application/problem+json"},
		{"text/html", "text/html"},
		{" text/html ; charset=utf-8", "text/html"},
		{"", ""},
		{"garbage;;", "garbage"},
		{"text/html; charset", "text/html"}, // invalid parameter → fallback
	}
	for _, tt := range tests {
		if got := mediaType(tt.in); got != tt.want {
			t.Errorf("mediaType(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func Test_isJSONMediaType(t *testing.T) {
	tests := []struct {
		mt   string
		want bool
	}{
		{"application/json", true},
		{"text/json", true},
		{"application/x-json", true},
		{"text/x-json", true},
		{"application/problem+json", true},
		{"application/ld+json", true},
		{"application/vnd.api+json", true},
		{"application/vnd.github.v3+json", true},
		{"application/+json", false},
		{"text/foo+json", false},
		{"application/json-seq", false},
		{"application/geo+json-seq", false},
		{"application/x-ndjson", false},
		{"application/jsonl", false},
		{"text/plain", false},
		{"application/octet-stream", false},
		{"text/html", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isJSONMediaType(tt.mt); got != tt.want {
			t.Errorf("isJSONMediaType(%q) = %v, want %v", tt.mt, got, tt.want)
		}
	}
}

func Test_readJSON(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantBody  string
		wantValid bool
	}{
		{"object", `{"a":1}`, `{"a":1}`, true},
		{"array", `[1,2]`, `[1,2]`, true},
		{"primitive", `"x"`, `"x"`, true},
		{"BOM stripped", "\xEF\xBB\xBF{\"a\":1}", `{"a":1}`, true},
		{"whitespace kept", " {\"a\" : 1}\n", " {\"a\" : 1}\n", true},
		{"empty", "", "", false},
		{"XSSI prefix", ")]}'\n{\"a\":1}", ")]}'\n{\"a\":1}", false},
		{"truncated", `{"a":`, `{"a":`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, valid, err := readJSON(strings.NewReader(tt.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.wantBody || valid != tt.wantValid {
				t.Errorf("readJSON(%q) = %q, %v; want %q, %v", tt.in, body, valid, tt.wantBody, tt.wantValid)
			}
		})
	}
}

// serve returns a test server that answers with status, Content-Type ct
// (no header if ""), and body.
func serve(t *testing.T, status int, ct, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		w.WriteHeader(status)
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFetch_JSON(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		ct       string
		body     string
		wantKind Kind
		wantMT   string
		want     string // Result.JSON or Result.Text
	}{
		{"object", 200, "application/json", `{"id":1,"name":"x"}`, KindJSON, "application/json", `{"id":1,"name":"x"}`},
		{"array", 200, "application/json", `[1,2,3]`, KindJSON, "application/json", `[1,2,3]`},
		{"params removed", 200, "application/json; charset=utf-8", `{}`, KindJSON, "application/json", `{}`},
		{"problem+json", 200, "Application/Problem+JSON", `{"title":"x"}`, KindJSON, "application/problem+json", `{"title":"x"}`},
		{"text/json", 200, "text/json", `true`, KindJSON, "text/json", `true`},
		{"big int exact", 200, "application/json", `{"n":12345678901234567890}`, KindJSON, "application/json", `{"n":12345678901234567890}`},
		{"BOM stripped", 200, "application/json", "\xEF\xBB\xBF[1]", KindJSON, "application/json", `[1]`},
		{"201", 201, "application/json", `{"ok":true}`, KindJSON, "application/json", `{"ok":true}`},
		{"invalid", 200, "application/json", `{"a":`, KindText, "application/json", `{"a":`},
		{"empty", 200, "application/json", ``, KindText, "application/json", ``},
		{"XSSI prefix", 200, "application/json", ")]}'\n{\"a\":1}", KindText, "application/json", ")]}'\n{\"a\":1}"},
		{"204 with JSON type", 204, "application/json", ``, KindText, "application/json", ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Fetch(context.Background(), serve(t, tt.status, tt.ct, tt.body), 5*time.Second)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Kind != tt.wantKind || res.MediaType != tt.wantMT {
				t.Errorf("Kind, MediaType = %q, %q; want %q, %q", res.Kind, res.MediaType, tt.wantKind, tt.wantMT)
			}
			got := res.Text
			if res.Kind == KindJSON {
				got = string(res.JSON)
				if res.Text != "" {
					t.Errorf("Text = %q, want empty for KindJSON", res.Text)
				}
			} else if res.JSON != nil {
				t.Errorf("JSON = %q, want nil for %q", res.JSON, res.Kind)
			}
			if got != tt.want {
				t.Errorf("content = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFetch_Unsupported(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		ct      string
		wantMsg string
	}{
		{"json-seq", 200, "application/json-seq", "unsupported content type: application/json-seq (expected HTML, PDF or JSON)"},
		{"ndjson", 200, "application/x-ndjson", "unsupported content type: application/x-ndjson"},
		{"text/plain", 200, "text/plain", "unsupported content type: text/plain"},
		{"204 without Content-Type", 204, "", "unsupported content type:  (expected HTML, PDF or JSON)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), serve(t, tt.status, tt.ct, ""), 5*time.Second)
			if !errors.Is(err, ErrUnsupportedContentType) {
				t.Fatalf("err = %v, want ErrUnsupportedContentType", err)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %q, want containing %q", err, tt.wantMsg)
			}
		})
	}
}

func TestFetch_StatusError(t *testing.T) {
	long := strings.Repeat("é", maxErrorBody) // 2 bytes per rune

	tests := []struct {
		name     string
		status   int
		ct       string
		body     string
		wantBody string
		wantMsg  string
	}{
		{
			name: "404 JSON", status: 404, ct: "application/json", body: `{"message":"Not Found"}`,
			wantBody: `{"message":"Not Found"}`,
			wantMsg:  `unexpected status code: 404: {"message":"Not Found"}`,
		},
		{
			name: "404 text/plain", status: 404, ct: "text/plain; charset=utf-8", body: "no such page",
			wantBody: "no such page",
			wantMsg:  "unexpected status code: 404: no such page",
		},
		{
			name: "404 HTML", status: 404, ct: "text/html", body: "<h1>Not Found</h1>",
			wantBody: "",
			wantMsg:  "unexpected status code: 404",
		},
		{
			name: "500 no body", status: 500, ct: "", body: "",
			wantBody: "",
			wantMsg:  "unexpected status code: 500",
		},
		{
			name: "500 JSON over 4 KiB", status: 500, ct: "application/problem+json", body: `"` + long + `"`,
			wantBody: `"` + strings.Repeat("é", (maxErrorBody-1)/2) + "... (truncated)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), serve(t, tt.status, tt.ct, tt.body), 5*time.Second)
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *StatusError", err)
			}
			if se.StatusCode != tt.status || se.MediaType != mediaType(tt.ct) {
				t.Errorf("StatusCode, MediaType = %d, %q", se.StatusCode, se.MediaType)
			}
			if se.Body != tt.wantBody {
				t.Errorf("Body = %q, want %q", se.Body, tt.wantBody)
			}
			if !utf8.ValidString(se.Body) {
				t.Errorf("Body is not valid UTF-8")
			}
			if tt.wantMsg != "" && err.Error() != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestFetch_AcceptHeader(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Accept")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	if _, err := Fetch(context.Background(), srv.URL, 5*time.Second); err != nil {
		t.Fatal(err)
	}
	const want = "text/html,application/xhtml+xml,application/pdf,application/json"
	if got != want {
		t.Errorf("Accept = %q, want %q", got, want)
	}
}

func Test_cutAtRune(t *testing.T) {
	tests := []struct {
		s    string
		n    int
		want string
	}{
		{"abc", 5, "abc"},
		{"abc", 3, "abc"},
		{"abc", 2, "ab"},
		{"aé", 2, "a"}, // é is 2 bytes; do not split it
		{"aé", 3, "aé"},
		{"€", 1, ""},
		{"€", 2, ""},
		{"abc", 0, ""},
	}
	for _, tt := range tests {
		if got := cutAtRune(tt.s, tt.n); got != tt.want {
			t.Errorf("cutAtRune(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}
