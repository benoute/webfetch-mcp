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

func TestFetchAndConvert(t *testing.T) {
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
			name: "non-HTML content type returns error",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(`{"error": "not html"}`))
			},
			expectedError: "unsupported content type",
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

			result, err := FetchAndConvert(context.Background(), server.URL, 5*time.Second)

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

			if !strings.Contains(result, tt.expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedOutput, result)
			}
		})
	}
}

func TestFetchAndConvert_InvalidURL(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := FetchAndConvert(context.Background(), tt.url, 5*time.Second)
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

func TestFetchAndConvert_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer server.Close()

	_, err := FetchAndConvert(context.Background(), server.URL, 10*time.Millisecond)
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

func TestFetchAndConvert_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><body>Hello</body></html>"))
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := FetchAndConvert(ctx, server.URL, 5*time.Second)
	if err == nil {
		t.Error("expected context cancellation error, got nil")
	}
}

func TestFetchAndConvert_PDF(t *testing.T) {
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

			result, err := FetchAndConvert(context.Background(), server.URL, 5*time.Second)

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

			if !strings.Contains(result, tt.expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedOutput, result)
			}
		})
	}
}
