package webfetch

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func Test_isPDFContentType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"application/pdf", true},
		{"application/pdf; charset=binary", true},
		{"APPLICATION/PDF", true},
		{"text/html", false},
		{"application/json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := isPDFContentType(tt.contentType)
			if result != tt.expected {
				t.Errorf("isPDFContentType(%q) = %v, want %v", tt.contentType, result, tt.expected)
			}
		})
	}
}

func Test_convertPDFToMarkdown(t *testing.T) {
	// Test with actual PDF file
	data, err := os.ReadFile("testdata/test.pdf")
	if err != nil {
		t.Fatalf("failed to read test PDF: %v", err)
	}

	result, err := convertPDFToMarkdown(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Check for page headers
	if !strings.Contains(result, "## Page 1") {
		t.Errorf("expected output to contain page 1 header, got %q", result)
	}
	if !strings.Contains(result, "## Page 2") {
		t.Errorf("expected output to contain page 2 header, got %q", result)
	}

	// Check for page separator
	if !strings.Contains(result, "---") {
		t.Errorf("expected output to contain page separator, got %q", result)
	}

	// Check for expected content
	if !strings.Contains(result, "Hello World") {
		t.Errorf("expected output to contain 'Hello World', got %q", result)
	}
	if !strings.Contains(result, "Second Page") {
		t.Errorf("expected output to contain 'Second Page', got %q", result)
	}
}

func Test_convertPDFToMarkdown_SizeLimit(t *testing.T) {
	tests := []struct {
		name          string
		contentLength int64
		expectedError string
	}{
		{
			name:          "Content-Length exceeds limit",
			contentLength: 200 * 1024 * 1024, // 200MB
			expectedError: "PDF too large",
		},
		{
			name:          "Content-Length at limit is ok",
			contentLength: maxPDFSize,
			expectedError: "", // Should not error on Content-Length check
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Use empty reader since we're testing Content-Length check
			_, err := convertPDFToMarkdown(strings.NewReader(""), tt.contentLength)

			if tt.expectedError != "" {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.expectedError)
					return
				}
				if !strings.Contains(err.Error(), tt.expectedError) {
					t.Errorf("expected error containing %q, got %q", tt.expectedError, err.Error())
				}
			}
		})
	}
}

func Test_convertPDFToMarkdown_ReadLimitExceeded(t *testing.T) {
	// Create a reader that claims to have valid content length but provides too much data
	// This tests the io.LimitReader behavior
	largeData := make([]byte, maxPDFSize+100)

	_, err := convertPDFToMarkdown(bytes.NewReader(largeData), -1) // -1 means unknown Content-Length
	if err == nil {
		t.Error("expected error for oversized PDF, got nil")
		return
	}
	if !strings.Contains(err.Error(), "PDF too large") {
		t.Errorf("expected 'PDF too large' error, got %q", err.Error())
	}
}
