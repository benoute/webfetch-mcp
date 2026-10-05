package webfetch

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// UserAgent is the User-Agent header sent on every fetch.
// Set it once before the first call to Fetch; it is not safe to
// change concurrently with fetches.
var UserAgent = "webfetch-mcp"

// acceptHeader lists the media types that Fetch can handle.
const acceptHeader = "text/html,application/xhtml+xml,application/pdf,application/json"

// Fetch fetches the URL and returns its content:
//   - HTML: converted to Markdown (KindMarkdown). Common non-content elements
//     are removed and links have absolute URLs.
//   - PDF: text converted to Markdown with page separators (KindMarkdown).
//   - JSON: the body as received (KindJSON), or the body as text if it is
//     not valid JSON (KindText).
//
// A non-2xx status returns a *StatusError. Other media types return an error
// that wraps ErrUnsupportedContentType.
func Fetch(
	ctx context.Context,
	rawURL string,
	timeout time.Duration,
) (*Result, error) {
	// Validate URL
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid URL: missing scheme or host")
	}

	// Create HTTP client with timeout
	client := &http.Client{
		Timeout: timeout,
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", acceptHeader)

	// Fetch the URL
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	contentType := resp.Header.Get("Content-Type")
	mt := mediaType(contentType)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, newStatusError(resp, mt)
	}

	switch {
	case isPDFMediaType(mt):
		md, err := convertPDFToMarkdown(resp.Body, resp.ContentLength)
		if err != nil {
			return nil, err
		}
		return &Result{Kind: KindMarkdown, MediaType: mt, Text: md}, nil

	case isHTMLMediaType(mt):
		md, err := convertHTMLToMarkdown(resp.Body, parsedURL)
		if err != nil {
			return nil, err
		}
		return &Result{Kind: KindMarkdown, MediaType: mt, Text: md}, nil

	case isJSONMediaType(mt):
		body, valid, err := readJSON(resp.Body)
		if err != nil {
			return nil, err
		}
		if !valid {
			return &Result{Kind: KindText, MediaType: mt, Text: string(body)}, nil
		}
		return &Result{Kind: KindJSON, MediaType: mt, JSON: body}, nil
	}

	return nil, fmt.Errorf("%w: %s (expected HTML, PDF or JSON)", ErrUnsupportedContentType, contentType)
}

// mediaType returns the lower-case media type of a Content-Type header value,
// without parameters. It returns "" for an empty value.
func mediaType(contentType string) string {
	if mt, _, err := mime.ParseMediaType(contentType); err == nil {
		return mt
	}
	mt, _, _ := strings.Cut(contentType, ";")
	return strings.ToLower(strings.TrimSpace(mt))
}
