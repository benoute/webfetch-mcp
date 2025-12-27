package webfetch

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// FetchAndConvert fetches the URL and converts its HTML or PDF content to Markdown.
// It removes common non-content elements from HTML and preserves links with absolute URLs.
// For PDFs, it extracts text with page separators.
func FetchAndConvert(
	ctx context.Context,
	rawURL string,
	timeout time.Duration,
) (string, error) {
	// Validate URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid URL: missing scheme or host")
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Set a reasonable User-Agent
	req.Header.Set("User-Agent", "webfetch/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/pdf")

	// Fetch the URL
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Get content type and route to appropriate converter
	contentType := resp.Header.Get("Content-Type")

	if isPDFContentType(contentType) {
		return convertPDFToMarkdown(resp.Body, resp.ContentLength)
	}

	if isHTMLContentType(contentType) {
		return convertHTMLToMarkdown(resp.Body, parsedURL)
	}

	return "", fmt.Errorf("unsupported content type: %s (expected HTML or PDF)", contentType)
}
