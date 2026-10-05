package webfetch

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
)

func Test_isPDFMediaType(t *testing.T) {
	tests := []struct {
		contentType string
		expected    bool
	}{
		{"application/pdf", true},
		{"application/pdf; charset=binary", true},
		{"APPLICATION/PDF", true},
		{"application/pdfx", false},
		{"text/html", false},
		{"application/json", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.contentType, func(t *testing.T) {
			result := isPDFMediaType(mediaType(tt.contentType))
			if result != tt.expected {
				t.Errorf("isPDFMediaType(mediaType(%q)) = %v, want %v", tt.contentType, result, tt.expected)
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

	result, err := convertPDFToMarkdown(data)
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

// makeTestPDF returns a minimal, uncompressed PDF with n pages. Page k shows the
// text "Body k" in Helvetica. Object layout: 1 catalog, 2 pages, 3 font, then
// one page object and one content stream per page.
func makeTestPDF(t *testing.T, n int) []byte {
	t.Helper()

	var buf bytes.Buffer
	numObjs := 3 + 2*n
	offsets := make([]int, numObjs+1) // 1-based
	obj := func(id int, body string) {
		offsets[id] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", id, body)
	}

	buf.WriteString("%PDF-1.4\n")
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")

	kids := make([]string, n)
	for k := range n {
		kids[k] = fmt.Sprintf("%d 0 R", 4+2*k)
	}
	obj(2, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d /MediaBox [0 0 612 792] >>",
		strings.Join(kids, " "), n))
	obj(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")

	for k := range n {
		pageID, contentID := 4+2*k, 5+2*k
		obj(pageID, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>",
			contentID))
		stream := fmt.Sprintf("BT /F1 12 Tf 72 720 Td (Body %d) Tj ET", k+1)
		obj(contentID, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
	}

	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", numObjs+1)
	for id := 1; id <= numObjs; id++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", numObjs+1, xref)
	return buf.Bytes()
}

// Test_convertPDFToMarkdown_PageOrder checks that the parallel fan-out keeps
// pages in order and pairs each header with its own body, for worker counts
// below, at, and above maxConcurrency.
func Test_convertPDFToMarkdown_PageOrder(t *testing.T) {
	const numPages = 37 // > maxConcurrency, not a multiple of common worker counts
	data := makeTestPDF(t, numPages)

	procs := []int{1, 2, 3, runtime.NumCPU(), maxConcurrency + 8}
	for _, p := range procs {
		t.Run(fmt.Sprintf("GOMAXPROCS=%d", p), func(t *testing.T) {
			prev := runtime.GOMAXPROCS(p)
			t.Cleanup(func() { runtime.GOMAXPROCS(prev) })

			result, err := convertPDFToMarkdown(data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			sections := strings.Split(result, "\n\n---\n\n")
			if len(sections) != numPages {
				t.Fatalf("got %d sections, want %d:\n%s", len(sections), numPages, result)
			}
			for i, s := range sections {
				want := fmt.Sprintf("## Page %d\n\nBody %d", i+1, i+1)
				if s != want {
					t.Errorf("section %d = %q, want %q", i, s, want)
				}
			}
		})
	}
}
