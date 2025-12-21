package webfetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func Test_convertHTMLToMarkdown(t *testing.T) {
	baseURL, _ := url.Parse("https://example.com")

	tests := []struct {
		name           string
		html           string
		expectedOutput string
		notExpected    []string
	}{
		{
			name:           "basic paragraph",
			html:           "<p>Hello World</p>",
			expectedOutput: "Hello World",
		},
		{
			name:           "heading",
			html:           "<h1>Title</h1><p>Content</p>",
			expectedOutput: "# Title",
		},
		{
			name:           "bold text",
			html:           "<p>This is <strong>bold</strong> text</p>",
			expectedOutput: "**bold**",
		},
		{
			name:           "italic text",
			html:           "<p>This is <em>italic</em> text</p>",
			expectedOutput: "*italic*",
		},
		{
			name:           "link preserved",
			html:           `<p><a href="https://example.org">Link</a></p>`,
			expectedOutput: "[Link](https://example.org)",
		},
		{
			name:           "list items",
			html:           "<ul><li>Item 1</li><li>Item 2</li></ul>",
			expectedOutput: "- Item 1",
		},
		{
			name:        "nav removed",
			html:        "<nav>Navigation</nav><p>Content</p>",
			notExpected: []string{"Navigation"},
		},
		{
			name:        "header removed",
			html:        "<header>Header</header><p>Content</p>",
			notExpected: []string{"Header"},
		},
		{
			name:        "footer removed",
			html:        "<p>Content</p><footer>Footer</footer>",
			notExpected: []string{"Footer"},
		},
		{
			name:        "aside removed",
			html:        "<aside>Sidebar</aside><p>Content</p>",
			notExpected: []string{"Sidebar"},
		},
		{
			name:        "script removed",
			html:        "<script>alert('xss')</script><p>Content</p>",
			notExpected: []string{"alert", "xss"},
		},
		{
			name:        "style removed",
			html:        "<style>body{color:red}</style><p>Content</p>",
			notExpected: []string{"color", "red"},
		},
		{
			name:        "form removed",
			html:        "<form><input type='text'></form><p>Content</p>",
			notExpected: []string{"input"},
		},
		{
			name:        "button removed",
			html:        "<button>Click me</button><p>Content</p>",
			notExpected: []string{"Click me"},
		},
		{
			name:        "iframe removed",
			html:        "<iframe src='http://evil.com'></iframe><p>Content</p>",
			notExpected: []string{"iframe", "evil"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := convertHTMLToMarkdown(strings.NewReader(tt.html), baseURL)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.expectedOutput != "" && !strings.Contains(result, tt.expectedOutput) {
				t.Errorf("expected output to contain %q, got %q", tt.expectedOutput, result)
			}

			for _, notExp := range tt.notExpected {
				if strings.Contains(result, notExp) {
					t.Errorf("output should not contain %q, got %q", notExp, result)
				}
			}
		})
	}
}

func Test_isHTMLContentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"text/html", true},
		{"text/html; charset=utf-8", true},
		{"TEXT/HTML", true},
		{"application/xhtml+xml", true},
		{"application/json", false},
		{"text/plain", false},
		{"image/png", false},
		{"application/pdf", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := isHTMLContentType(tt.contentType)
			if result != tt.expected {
				t.Errorf("isHTMLContentType(%q) = %v, want %v", tt.contentType, result, tt.expected)
			}
		})
	}
}

func Test_cleanupMarkdown(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "no change needed",
			input:    "Line 1\n\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "reduces excessive blank lines",
			input:    "Line 1\n\n\n\n\nLine 2",
			expected: "Line 1\n\nLine 2",
		},
		{
			name:     "trims leading and trailing whitespace",
			input:    "\n\n\nContent\n\n\n",
			expected: "Content",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cleanupMarkdown(tt.input)
			if result != tt.expected {
				t.Errorf("cleanupMarkdown() = %q, want %q", result, tt.expected)
			}
		})
	}
}
