package webfetch

import (
	"net/url"
	"strings"
	"testing"
)

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

// Test commented out: cleanupMarkdown function is currently commented out in html.go
// func Test_cleanupMarkdown(t *testing.T) {
// 	tests := []struct {
// 		name     string
// 		input    []byte
// 		expected []byte
// 	}{
// 		{
// 			name:     "no change needed",
// 			input:    []byte("Line 1\n\nLine 2"),
// 			expected: []byte("Line 1\n\nLine 2"),
// 		},
// 		{
// 			name:     "reduces excessive blank lines",
// 			input:    []byte("Line 1\n\n\n\n\nLine 2"),
// 			expected: []byte("Line 1\n\nLine 2"),
// 		},
// 		{
// 			name:     "trims leading and trailing whitespace",
// 			input:    []byte("\n\n\nContent\n\n\n"),
// 			expected: []byte("Content"),
// 		},
// 	}
//
// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			result := cleanupMarkdown(tt.input)
// 			if string(result) != string(tt.expected) {
// 				t.Errorf("cleanupMarkdown() = %q, want %q", result, tt.expected)
// 			}
// 		})
// 	}
// }
