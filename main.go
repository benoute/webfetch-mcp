package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// UserAgent is the User-Agent header sent on every fetch.
// Set it once before the first call to Fetch; it is not safe to
// change concurrently with fetches.
var UserAgent = "webfetch-mcp"

// acceptHeader lists the media types that Fetch can handle.
const acceptHeader = "text/html,application/xhtml+xml,application/pdf,application/json,text/*;q=0.9"

// maxBodySize is the maximum size of a response body (100 MiB).
const maxBodySize = 100 << 20

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte("\xEF\xBB\xBF")

// Fetch fetches the URL and returns its content:
//   - HTML: converted to Markdown (KindMarkdown). Common non-content elements
//     are removed and links have absolute URLs, resolved against the final
//     URL after redirects.
//   - PDF: text converted to Markdown with page separators (KindMarkdown).
//   - JSON: the body as received (KindJSON), or the body as text if it is
//     not valid JSON (KindInvalidJSON).
//   - Text (text/* other than HTML and text/event-stream): the body as
//     received (KindText).
//   - Empty body (any media type): KindNone.
//
// The body is read as UTF-8, with a UTF-8 BOM removed, and is limited to
// 100 MiB.
//
// Errors:
//   - A non-2xx status returns a *StatusError. Its Result holds the response
//     metadata and the converted body, if the body could be converted.
//   - A 2xx status with a body that cannot be returned (other media type, body
//     too large, conversion or read failure) returns a *ResponseError. It wraps
//     ErrUnsupportedContentType or ErrBodyTooLarge, if applicable.
//   - Other errors (invalid URL, no response) are returned as plain errors.
func Fetch(
	ctx context.Context,
	rawURL string,
	timeout time.Duration,
) (*Result, error) {
	// --- Validate URL ---
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	if parsedURL.Scheme == "" || parsedURL.Host == "" {
		return nil, fmt.Errorf("invalid URL: missing scheme or host")
	}

	// --- Send request ---
	client := &http.Client{
		Timeout: timeout,
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", acceptHeader)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	// --- Response metadata ---
	contentType := resp.Header.Get("Content-Type")
	res := &Result{
		MediaType:   mediaType(contentType),
		URL:         resp.Request.URL.String(), // final URL, after redirects
		StatusCode:  resp.StatusCode,
		ContentType: contentType,
	}

	// --- Read body: at most maxBodySize, BOM removed ---
	body, readErr := readBody(resp.Body, resp.ContentLength)

	// --- Non-2xx: convert what we can, never fail on the body ---
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if readErr != nil || len(body) == 0 || convert(body, res) != nil {
			res.Kind, res.Text, res.JSON = KindNone, "", nil
		}
		se := &StatusError{StatusCode: resp.StatusCode, MediaType: res.MediaType, Result: res}
		if hasErrorBody(res.MediaType) {
			se.Body = cutAtRune(string(body), maxErrorBody)
			if len(se.Body) < len(body) {
				se.Body += truncatedSuffix
			}
		}
		return nil, se
	}

	// --- 2xx ---
	if readErr != nil {
		return nil, &ResponseError{Result: res, Err: readErr}
	}
	if len(body) == 0 {
		return res, nil
	}
	if err := convert(body, res); err != nil {
		return nil, &ResponseError{Result: res, Err: err}
	}
	return res, nil
}

// readBody reads at most maxBodySize bytes and removes a UTF-8 BOM.
// It fails early if contentLength > maxBodySize.
func readBody(r io.Reader, contentLength int64) ([]byte, error) {
	if contentLength > maxBodySize {
		return nil, fmt.Errorf("%w: %d bytes (max %d bytes)", ErrBodyTooLarge, contentLength, maxBodySize)
	}
	b, err := io.ReadAll(io.LimitReader(r, maxBodySize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read body: %w", err)
	}
	if len(b) > maxBodySize {
		return nil, fmt.Errorf("%w: exceeds %d bytes", ErrBodyTooLarge, maxBodySize)
	}
	return bytes.TrimPrefix(b, utf8BOM), nil
}
