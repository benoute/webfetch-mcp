package webfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
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
				w.Header().Set("Content-Type", "text/csv")
				w.Write([]byte("a,b\n1,2\n"))
			},
			expectedError: "unsupported content type: text/csv (expected HTML, PDF or JSON)",
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
			name: "PDF too large via Content-Length",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/pdf")
				w.Header().Set("Content-Length", "200000000") // 200MB
				// Don't write anything, the Content-Length check should fail first
			},
			expectedError: "PDF too large",
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
