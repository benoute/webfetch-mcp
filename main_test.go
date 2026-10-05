package webfetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestFetch(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedError  string
		expectedOutput string
	}{
		{
			name: "successful HTML conversion",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<!DOCTYPE html>
<html>
<head><title>Test</title></head>
<body>
<nav><a href="/home">Home</a></nav>
<main>
<h1>Hello World</h1>
<p>This is a <strong>test</strong> paragraph.</p>
<a href="/page">Link</a>
</main>
<footer>Footer content</footer>
</body>
</html>`))
			},
			expectedOutput: "Hello World",
		},
		{
			name: "removes nav, header, footer elements",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<html>
<body>
<header>Header content</header>
<nav>Navigation</nav>
<p>Main content</p>
<aside>Sidebar</aside>
<footer>Footer</footer>
</body>
</html>`))
			},
			expectedOutput: "Main content",
		},
		{
			name: "removes script and style tags",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<html>
<head>
<style>body { color: red; }</style>
<script>alert('hello');</script>
</head>
<body>
<p>Visible content</p>
<script>console.log('test');</script>
</body>
</html>`))
			},
			expectedOutput: "Visible content",
		},
		{
			name: "preserves links",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<html>
<body>
<p>Check out <a href="https://example.com">this link</a>.</p>
</body>
</html>`))
			},
			expectedOutput: "[this link](https://example.com)",
		},
		{
			name: "converts relative URLs to absolute",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Write([]byte(`<html>
<body>
<p><a href="/page">Relative link</a></p>
<img src="/image.png" alt="Image">
</body>
</html>`))
			},
			expectedOutput: "/page",
		},
		{
			name: "unsupported content type returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.Write([]byte("\x89PNG"))
			},
			expectedError: "unsupported content type: image/png (expected HTML, PDF, JSON or text)",
		},
		{
			name: "404 status returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
			expectedError: "unexpected status code: 404",
		},
		{
			name: "500 status returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
			expectedError: "unexpected status code: 500",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			result, err := Fetch(context.Background(), server.URL, 5*time.Second)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedError)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Kind != KindMarkdown {
				t.Errorf("Kind = %q, want %q", result.Kind, KindMarkdown)
			}
			if !strings.Contains(result.Text, tt.expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedOutput, result.Text)
			}
		})
	}
}

func TestFetch_InvalidURL(t *testing.T) {
	tests := []struct {
		name          string
		url           string
		expectedError string
	}{
		{
			name:          "missing scheme",
			url:           "example.com/page",
			expectedError: "missing scheme or host",
		},
		{
			name:          "missing host",
			url:           "http:///page",
			expectedError: "missing scheme or host",
		},
		{
			name:          "empty URL",
			url:           "",
			expectedError: "missing scheme or host",
		},
		{
			// Go 1.26+ (go directive ≥ 1.26): url.Parse rejects colons in host.
			name:          "colon in host",
			url:           "http://localhost:80:80/",
			expectedError: "invalid URL",
		},
		{
			name:          "unbracketed IPv6 host",
			url:           "http://::1/",
			expectedError: "invalid URL",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), tt.url, 5*time.Second)
			if err == nil {
				t.Errorf("expected error containing %q, got nil", tt.expectedError)
				return
			}
			if !strings.Contains(err.Error(), tt.expectedError) {
				t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
			}
		})
	}
}

func TestFetch_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer server.Close()

	_, err := Fetch(context.Background(), server.URL, 10*time.Millisecond)
	if err == nil {
		t.Error("expected timeout error, got nil")
		return
	}
	// The error should indicate a timeout or context deadline
	errStr := err.Error()
	if !strings.Contains(errStr, "timeout") &&
		!strings.Contains(errStr, "deadline") &&
		!strings.Contains(errStr, "Timeout") {
		t.Errorf("expected timeout-related error, got %q", errStr)
	}
}

func TestFetch_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := Fetch(ctx, server.URL, 5*time.Second)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestFetch_PDF(t *testing.T) {
	// Read test PDF
	pdfData, err := os.ReadFile("testdata/test.pdf")
	if err != nil {
		t.Fatalf("failed to read test PDF: %v", err)
	}

	tests := []struct {
		name           string
		handler        http.HandlerFunc
		expectedError  string
		expectedOutput string
	}{
		{
			name: "successful PDF conversion",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Write(pdfData)
			},
			expectedOutput: "Hello World",
		},
		{
			name: "PDF with page separators",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Write(pdfData)
			},
			expectedOutput: "## Page 1",
		},
		{
			name: "body too large via Content-Length",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Header().Set("Content-Length", "200000000") // 200MB
				// Don't write anything, the Content-Length check should fail first
			},
			expectedError: "body too large: 200000000 bytes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(tt.handler)
			defer server.Close()

			result, err := Fetch(context.Background(), server.URL, 5*time.Second)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedError)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			if result.Kind != KindMarkdown {
				t.Errorf("Kind = %q, want %q", result.Kind, KindMarkdown)
			}
			if !strings.Contains(result.Text, tt.expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedOutput, result.Text)
			}
		})
	}
}

func TestFetch_UserAgent(t *testing.T) {
	var got string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<p>ok</p>"))
	}))
	defer server.Close()

	t.Run("default", func(t *testing.T) {
		if _, err := Fetch(context.Background(), server.URL, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if got != "webfetch-mcp" {
			t.Errorf("User-Agent = %q, want %q", got, "webfetch-mcp")
		}
	})

	t.Run("override", func(t *testing.T) {
		prev := UserAgent
		UserAgent = "webfetch-mcp/v9.9.9"
		t.Cleanup(func() { UserAgent = prev })

		if _, err := Fetch(context.Background(), server.URL, 5*time.Second); err != nil {
			t.Fatal(err)
		}
		if got != "webfetch-mcp/v9.9.9" {
			t.Errorf("User-Agent = %q, want %q", got, "webfetch-mcp/v9.9.9")
		}
	})
}

// errReader fails on every Read.
type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

// zeroReader returns zero bytes forever.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func Test_readBody(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"object", `{"a":1}`, `{"a":1}`},
		{"BOM stripped", "\xEF\xBB\xBF{\"a\":1}", `{"a":1}`},
		{"whitespace kept", " {\"a\" : 1}\n", " {\"a\" : 1}\n"},
		{"empty", "", ""},
		{"BOM only", "\xEF\xBB\xBF", ""},
		{"BOM not at start kept", "a\xEF\xBB\xBF", "a\xEF\xBB\xBF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readBody(strings.NewReader(tt.in), int64(len(tt.in)))
			if err != nil || string(got) != tt.want {
				t.Errorf("readBody(%q) = %q, %v; want %q, nil", tt.in, got, err, tt.want)
			}
		})
	}

	t.Run("Content-Length over max: early, no read", func(t *testing.T) {
		_, err := readBody(errReader{}, maxBodySize+1)
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("err = %v, want ErrBodyTooLarge", err)
		}
		want := fmt.Sprintf("body too large: %d bytes (max %d bytes)", maxBodySize+1, maxBodySize)
		if err.Error() != want {
			t.Errorf("err = %q, want %q", err, want)
		}
	})

	t.Run("Content-Length at max is OK", func(t *testing.T) {
		if _, err := readBody(strings.NewReader(""), maxBodySize); err != nil {
			t.Errorf("err = %v, want nil", err)
		}
	})

	t.Run("read over max, unknown length", func(t *testing.T) {
		_, err := readBody(io.LimitReader(zeroReader{}, maxBodySize+100), -1)
		if !errors.Is(err, ErrBodyTooLarge) {
			t.Fatalf("err = %v, want ErrBodyTooLarge", err)
		}
	})

	t.Run("read error", func(t *testing.T) {
		_, err := readBody(errReader{}, -1)
		if err == nil || errors.Is(err, ErrBodyTooLarge) || !strings.Contains(err.Error(), "failed to read body: boom") {
			t.Errorf("err = %v, want failed to read body", err)
		}
	})
}

func Test_isTextMediaType(t *testing.T) {
	tests := []struct {
		mt   string
		want bool
	}{
		{"text/plain", true},
		{"text/markdown", true},
		{"text/csv", true},
		{"text/xml", true},
		{"text/css", true},
		{"text/html", false},
		{"text/event-stream", false},
		{"text/json", false},
		{"text/x-json", false},
		{"text/", false},
		{"application/xhtml+xml", false},
		{"application/json", false},
		{"image/png", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := isTextMediaType(tt.mt); got != tt.want {
			t.Errorf("isTextMediaType(%q) = %v, want %v", tt.mt, got, tt.want)
		}
	}
}

func TestFetch_Metadata(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/none" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{}`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/final?x=1", http.StatusFound)
	}))
	defer redirect.Close()

	t.Run("after 302", func(t *testing.T) {
		res, err := Fetch(context.Background(), redirect.URL+"/start", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if res.URL != target.URL+"/final?x=1" {
			t.Errorf("URL = %q, want %q", res.URL, target.URL+"/final?x=1")
		}
		if res.StatusCode != 200 || res.ContentType != "application/json; charset=utf-8" || res.MediaType != "application/json" {
			t.Errorf("StatusCode, ContentType, MediaType = %d, %q, %q", res.StatusCode, res.ContentType, res.MediaType)
		}
	})

	t.Run("no Content-Type", func(t *testing.T) {
		res, err := Fetch(context.Background(), target.URL+"/none", 5*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != 204 || res.ContentType != "" || res.Kind != KindNone {
			t.Errorf("StatusCode, ContentType, Kind = %d, %q, %q", res.StatusCode, res.ContentType, res.Kind)
		}
	})
}

func TestFetch_StatusErrorResult(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		ct, body string
		wantKind Kind
		want     string // Result.JSON or Result.Text (substring for Markdown)
	}{
		{"404 JSON", 404, "application/json", `{"message":"Not Found"}`, KindJSON, `{"message":"Not Found"}`},
		{"404 HTML", 404, "text/html", "<h1>Not Found</h1>", KindMarkdown, "# Not Found"},
		{"404 text/plain", 404, "text/plain", "no such page", KindText, "no such page"},
		{"400 invalid JSON", 400, "application/json", `{"a":`, KindInvalidJSON, `{"a":`},
		{"500 png", 500, "image/png", "\x89PNG", KindNone, ""},
		{"500 empty JSON", 500, "application/json", "", KindNone, ""},
		{"500 empty HTML", 500, "text/html", "", KindNone, ""},
		{"500 bad PDF", 500, "application/pdf", "not a pdf", KindNone, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), serve(t, tt.status, tt.ct, tt.body), 5*time.Second)
			var se *StatusError
			if !errors.As(err, &se) {
				t.Fatalf("err = %v, want *StatusError", err)
			}
			res := se.Result
			if res == nil || res.Kind != tt.wantKind || res.StatusCode != tt.status || res.ContentType != tt.ct {
				t.Fatalf("Result = %+v, want Kind %q", res, tt.wantKind)
			}
			switch tt.wantKind {
			case KindJSON:
				if string(res.JSON) != tt.want {
					t.Errorf("JSON = %q, want %q", res.JSON, tt.want)
				}
			case KindMarkdown:
				if !strings.Contains(res.Text, tt.want) {
					t.Errorf("Text = %q, want containing %q", res.Text, tt.want)
				}
			default:
				if res.Text != tt.want || res.JSON != nil {
					t.Errorf("Text, JSON = %q, %q; want %q, nil", res.Text, res.JSON, tt.want)
				}
			}
		})
	}

	t.Run("body too large", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "200000000")
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer srv.Close()
		_, err := Fetch(context.Background(), srv.URL, 5*time.Second)
		var se *StatusError
		if !errors.As(err, &se) {
			t.Fatalf("err = %v, want *StatusError", err)
		}
		if se.Result.Kind != KindNone || se.Body != "" || err.Error() != "unexpected status code: 502" {
			t.Errorf("Kind, Body, Error() = %q, %q, %q", se.Result.Kind, se.Body, err)
		}
	})
}

func TestFetch_ResponseError(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
		ct      string
		wantErr error  // errors.Is target, or nil
		wantMsg string // substring
	}{
		{
			name: "unsupported type", ct: "image/png",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "image/png")
				w.Write([]byte("\x89PNG"))
			},
			wantErr: ErrUnsupportedContentType,
			wantMsg: "unsupported content type: image/png (expected HTML, PDF, JSON or text)",
		},
		{
			name: "bad PDF", ct: "application/pdf",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Write([]byte("not a pdf"))
			},
			wantMsg: "failed to parse PDF",
		},
		{
			name: "Content-Length over max", ct: "application/json",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Content-Length", "200000000")
			},
			wantErr: ErrBodyTooLarge,
			wantMsg: "body too large: 200000000 bytes",
		},
		{
			name: "read error mid-body", ct: "text/html",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.Header().Set("Content-Length", "100")
				w.Write([]byte("<p>short</p>"))
			},
			wantMsg: "failed to read body",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.handler)
			defer srv.Close()
			res, err := Fetch(context.Background(), srv.URL, 5*time.Second)
			if res != nil {
				t.Errorf("res = %+v, want nil", res)
			}
			var re *ResponseError
			if !errors.As(err, &re) {
				t.Fatalf("err = %v (%T), want *ResponseError", err, err)
			}
			if tt.wantErr != nil && !errors.Is(err, tt.wantErr) {
				t.Errorf("err = %v, want wrapping %v", err, tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantMsg) {
				t.Errorf("err = %q, want containing %q", err, tt.wantMsg)
			}
			if re.Result == nil || re.Result.Kind != KindNone || re.Result.StatusCode != 200 ||
				re.Result.ContentType != tt.ct || re.Result.URL != srv.URL {
				t.Errorf("Result = %+v", re.Result)
			}
		})
	}
}

func TestFetch_EmptyBody(t *testing.T) {
	tests := []struct {
		name   string
		status int
		ct     string
	}{
		{"204 no Content-Type", 204, ""},
		{"200 JSON", 200, "application/json"},
		{"200 HTML", 200, "text/html"},
		{"200 PDF", 200, "application/pdf"},
		{"200 png", 200, "image/png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Fetch(context.Background(), serve(t, tt.status, tt.ct, ""), 5*time.Second)
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if res.Kind != KindNone || res.Text != "" || res.JSON != nil || res.StatusCode != tt.status || res.ContentType != tt.ct {
				t.Errorf("Result = %+v", res)
			}
		})
	}
}

func TestFetch_Text(t *testing.T) {
	tests := []struct {
		name, ct, body, want string
	}{
		{"text/plain", "text/plain", "hello\nworld", "hello\nworld"},
		{"text/markdown", "text/markdown; charset=utf-8", "# Doc\n\n<b>x</b>", "# Doc\n\n<b>x</b>"},
		{"text/csv", "text/csv", "a,b\n1,2\n", "a,b\n1,2\n"},
		{"BOM stripped", "text/plain", "\xEF\xBB\xBFhi", "hi"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, err := Fetch(context.Background(), serve(t, 200, tt.ct, tt.body), 5*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if res.Kind != KindText || res.Text != tt.want || res.MediaType != mediaType(tt.ct) {
				t.Errorf("Kind, Text, MediaType = %q, %q, %q; want %q, %q", res.Kind, res.Text, res.MediaType, KindText, tt.want)
			}
		})
	}
}

func TestFetch_HTMLFinalURLBase(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte(`<p><a href="/page">Link</a></p>`))
	}))
	defer target.Close()
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/doc", http.StatusFound)
	}))
	defer redirect.Close()

	res, err := Fetch(context.Background(), redirect.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[Link](" + target.URL + "/page)"; !strings.Contains(res.Text, want) {
		t.Errorf("Text = %q, want containing %q", res.Text, want)
	}
	if strings.Contains(res.Text, redirect.URL) {
		t.Errorf("Text = %q uses the request URL %q", res.Text, redirect.URL)
	}
}

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

// serve returns a test server that answers with status, Content-Type ct
// (no header if ""), and body.
func serve(t *testing.T, status int, ct, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct != "" {
			w.Header().Set("Content-Type", ct)
		} else {
			w.Header()["Content-Type"] = nil // no header, no sniffing
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
		{"invalid", 200, "application/json", `{"a":`, KindInvalidJSON, "application/json", `{"a":`},
		{"empty", 200, "application/json", ``, KindNone, "application/json", ``},
		{"BOM only", 200, "application/json", "\xEF\xBB\xBF", KindNone, "application/json", ``},
		{"XSSI prefix", 200, "application/json", ")]}'\n{\"a\":1}", KindInvalidJSON, "application/json", ")]}'\n{\"a\":1}"},
		{"204 with JSON type", 204, "application/json", ``, KindNone, "application/json", ``},
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
		ct      string
		wantMsg string
	}{
		{"json-seq", "application/json-seq", "unsupported content type: application/json-seq (expected HTML, PDF, JSON or text)"},
		{"ndjson", "application/x-ndjson", "unsupported content type: application/x-ndjson (expected HTML, PDF, JSON or text)"},
		{"event-stream", "text/event-stream", "unsupported content type: text/event-stream (expected HTML, PDF, JSON or text)"},
		{"raw header in message", "image/png; q=1", "unsupported content type: image/png; q=1 (expected HTML, PDF, JSON or text)"},
		{"no Content-Type", "", "unsupported content type:  (expected HTML, PDF, JSON or text)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Fetch(context.Background(), serve(t, 200, tt.ct, "x"), 5*time.Second)
			if !errors.Is(err, ErrUnsupportedContentType) {
				t.Fatalf("err = %v, want ErrUnsupportedContentType", err)
			}
			var re *ResponseError
			if !errors.As(err, &re) {
				t.Fatalf("err = %T, want *ResponseError", err)
			}
			if re.Result.Kind != KindNone || re.Result.StatusCode != 200 || re.Result.ContentType != tt.ct {
				t.Errorf("Result = %+v", re.Result)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("err = %q, want %q", err, tt.wantMsg)
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
		wantKind Kind
	}{
		{
			name: "404 JSON", status: 404, ct: "application/json", body: `{"message":"Not Found"}`,
			wantBody: `{"message":"Not Found"}`,
			wantMsg:  `unexpected status code: 404: {"message":"Not Found"}`,
			wantKind: KindJSON,
		},
		{
			name: "404 text/plain", status: 404, ct: "text/plain; charset=utf-8", body: "no such page",
			wantBody: "no such page",
			wantMsg:  "unexpected status code: 404: no such page",
			wantKind: KindText,
		},
		{
			name: "404 HTML", status: 404, ct: "text/html", body: "<h1>Not Found</h1>",
			wantBody: "",
			wantMsg:  "unexpected status code: 404",
			wantKind: KindMarkdown,
		},
		{
			name: "500 no body", status: 500, ct: "", body: "",
			wantBody: "",
			wantMsg:  "unexpected status code: 500",
		},
		{
			name: "500 JSON over 4 KiB", status: 500, ct: "application/problem+json", body: `"` + long + `"`,
			wantBody: `"` + strings.Repeat("é", (maxErrorBody-1)/2) + "... (truncated)",
			wantKind: KindJSON,
		},
		{
			name: "404 JSON exactly 4 KiB after BOM", status: 404, ct: "application/json",
			body:     "\xEF\xBB\xBF\"" + strings.Repeat("a", maxErrorBody-2) + "\"",
			wantBody: "\"" + strings.Repeat("a", maxErrorBody-2) + "\"",
			wantKind: KindJSON,
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
			if se.Result == nil {
				t.Fatal("Result = nil")
			}
			if se.Result.StatusCode != tt.status || se.Result.ContentType != tt.ct || se.Result.Kind != tt.wantKind {
				t.Errorf("Result = %+v, want status %d, ContentType %q, Kind %q", se.Result, tt.status, tt.ct, tt.wantKind)
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
	const want = "text/html,application/xhtml+xml,application/pdf,application/json,text/*;q=0.9"
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
