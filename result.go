package webfetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"unicode/utf8"
)

// Kind tells how the content of a Result is encoded.
type Kind string

const (
	// KindMarkdown is HTML or PDF content converted to Markdown, in Result.Text.
	KindMarkdown Kind = "markdown"
	// KindJSON is valid JSON, as received (UTF-8 BOM removed), in Result.JSON.
	KindJSON Kind = "json"
	// KindText is a body with a JSON media type that is not valid JSON.
	// Result.Text holds the body unchanged.
	KindText Kind = "text"
)

// Result is the content of a fetched URL.
type Result struct {
	Kind Kind
	// MediaType is the response media type, lower case, without parameters
	// (for example "text/html" or "application/vnd.api+json").
	MediaType string
	// Text is set for KindMarkdown and KindText.
	Text string
	// JSON is set for KindJSON only.
	JSON json.RawMessage
}

// ErrUnsupportedContentType is returned (wrapped) when the response media
// type is not HTML, PDF or JSON.
var ErrUnsupportedContentType = errors.New("unsupported content type")

// maxErrorBody is the maximum number of body bytes kept in a StatusError.
const maxErrorBody = 4 << 10 // 4 KiB

// truncatedSuffix is added to text that is cut.
const truncatedSuffix = "... (truncated)"

// StatusError is returned when the response status is not 2xx.
type StatusError struct {
	StatusCode int
	MediaType  string
	// Body is set only for a JSON media type or text/plain. It holds at most
	// maxErrorBody bytes of the body, followed by "... (truncated)" if cut.
	Body string
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("unexpected status code: %d", e.StatusCode)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// hasErrorBody reports whether a non-2xx body with media type mt is kept in
// the StatusError.
func hasErrorBody(mt string) bool {
	return isJSONMediaType(mt) || mt == "text/plain"
}

// newStatusError builds the StatusError for resp. It reads at most
// maxErrorBody+1 bytes of the body, and only if hasErrorBody(mt).
func newStatusError(resp *http.Response, mt string) *StatusError {
	e := &StatusError{StatusCode: resp.StatusCode, MediaType: mt}
	if !hasErrorBody(mt) {
		return e
	}
	b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody+1))
	if len(b) > maxErrorBody {
		e.Body = cutAtRune(string(b), maxErrorBody) + truncatedSuffix
	} else {
		e.Body = string(b)
	}
	return e
}

// cutAtRune returns at most n bytes of s. It does not split a UTF-8 sequence.
func cutAtRune(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
