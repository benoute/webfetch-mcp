package webfetch

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"runtime"
	"strings"
	"sync"

	"github.com/ledongthuc/pdf"
)

const (
	// maxPDFSize is the maximum size of a PDF file that can be processed (100MB)
	maxPDFSize = 100 * 1024 * 1024
	// maxConcurrency is the maximum concurrency allowed for extracting pages
	maxConcurrency = 32
)

// pdfBufferPool is a pool for reusing byte buffers when reading PDFs
var pdfBufferPool = sync.Pool{
	New: func() any {
		b := bytes.Buffer{}
		// Pre-allocate 1MB initial capacity
		b.Grow(1024 * 1024)
		return &b
	},
}

// pageBufferPool is a pool for reusing bytes buffers when processing pages
var pageBufferPool = sync.Pool{
	New: func() any {
		b := bytes.Buffer{}
		b.Grow(1024)
		return &b
	},
}

// isPDFMediaType reports whether mt (from mediaType) is the PDF media type.
func isPDFMediaType(mt string) bool {
	return mt == "application/pdf"
}

// extractPageText extracts text from a PDF page by analyzing character positions
// to properly reconstruct words with spaces between them.
func extractPageText(page pdf.Page, buf *bytes.Buffer) {
	content := page.Content()
	if len(content.Text) == 0 {
		return
	}

	var lastText pdf.Text
	hasLast := false

	for _, t := range content.Text {
		// Skip empty strings
		if t.S == "" {
			continue
		}

		if !hasLast {
			buf.WriteString(t.S)
			lastText = t
			hasLast = true
			continue
		}

		// Check if on different line (Y position changed significantly)
		if math.Abs(t.Y-lastText.Y) > 1 {
			buf.WriteString("\n")
			buf.WriteString(t.S)
			lastText = t
			continue
		}

		// Same line - check for gap between characters
		// If current X > last X + last W, there's a gap indicating a space
		expectedX := lastText.X + lastText.W
		gap := t.X - expectedX

		// Use a fraction of font size as threshold for detecting word gaps
		// A gap of ~20% of font size typically indicates a space
		threshold := max(lastText.FontSize*0.2, 1)

		if gap > threshold {
			buf.WriteString(" ")
		}

		buf.WriteString(t.S)
		lastText = t
	}
}

// convertPDFToMarkdown extracts text from a PDF and formats it as markdown
// with page separators between pages. It limits reading to maxPDFSize bytes.
func convertPDFToMarkdown(r io.Reader, contentLength int64) (string, error) {
	// Early rejection if Content-Length header indicates too large
	if contentLength > maxPDFSize {
		return "", fmt.Errorf("PDF too large: %d bytes (max %d bytes)", contentLength, maxPDFSize)
	}

	// Get buffer from pool
	buf := pdfBufferPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer func() {
		buf.Reset() // Clear data before returning to pool
		pdfBufferPool.Put(buf)
	}()

	// Wrap reader with limit to prevent reading more than maxPDFSize + 1
	// The +1 allows us to detect if we hit the limit
	limitedReader := io.LimitReader(r, maxPDFSize+1)

	// Read PDF data into buffer
	_, err := buf.ReadFrom(limitedReader)
	if err != nil {
		return "", fmt.Errorf("failed to read PDF: %w", err)
	}

	// Check if we hit the limit (read more than maxPDFSize)
	if buf.Len() > maxPDFSize {
		return "", fmt.Errorf("PDF too large: exceeds %d bytes", maxPDFSize)
	}

	data := buf.Bytes()

	// Create PDF reader from bytes
	pdfReader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to parse PDF: %w", err)
	}

	numPages := pdfReader.NumPage()
	if numPages == 0 {
		return "", nil
	}

	// Launch up to GOMAXPROCS workers (but no more than numPages or maxConcurrency)
	numWorkers := min(runtime.GOMAXPROCS(0), numPages, maxConcurrency)

	// Distribute pages evenly among workers
	// Worker i handles pages from startPage[i] to startPage[i+1]-1
	pagesPerWorker := numPages / numWorkers
	extraPages := numPages % numWorkers

	// One buffer per worker - each worker processes a contiguous range of pages in order
	// workerBuffers := make([]*bytes.Buffer, maxConcurrency)
	var workerBuffers [maxConcurrency]*bytes.Buffer

	// Calculate start page for each worker
	page := 1

	var wg sync.WaitGroup

	for i := range numWorkers {
		count := pagesPerWorker
		if i < extraPages {
			count++ // distribute extra pages to first workers
		}

		pageStart, pageEnd := page, page+count
		wg.Go(func() {
			workerBuf := pageBufferPool.Get().(*bytes.Buffer)
			workerBuf.Reset()

			// Process pages in order: [pageStart, pageEnd)
			for pageNum := pageStart; pageNum < pageEnd; pageNum++ {
				if pageNum > pageStart {
					workerBuf.WriteString("\n\n---\n\n")
				}
				fmt.Fprintf(workerBuf, "## Page %d\n\n", pageNum)

				page := pdfReader.Page(pageNum)
				if page.V.IsNull() {
					workerBuf.WriteString("[Error: page not found]\n")
				} else {
					extractPageText(page, workerBuf)
				}
			}

			workerBuffers[i] = workerBuf
		})

		page = pageEnd
	}

	wg.Wait()

	// Combine worker buffers in order using strings.Builder
	var result strings.Builder
	result.Grow(len(workerBuffers) * 1024)

	for i := range numWorkers {
		workerBuf := workerBuffers[i]
		if i > 0 && workerBuf.Len() > 0 {
			result.WriteString("\n\n---\n\n")
		}
		result.Write(workerBuf.Bytes())
		workerBuf.Reset()
		pageBufferPool.Put(workerBuf)
	}

	return result.String(), nil
}
