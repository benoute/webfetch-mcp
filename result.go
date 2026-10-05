package webfetch

import (
	"encoding/json"
	"errors"
	"fmt"
	"unicode/utf8"
)

// Kind tells how the content of a Result is encoded.
type Kind string

const (
	// KindNone is a result without content: an empty body, or a non-2xx body
	// that could not be converted.
	KindNone Kind = ""
	// KindMarkdown is HTML or PDF content converted to Markdown, in Result.Text.
	KindMarkdown Kind = "markdown"
	// KindJSON is valid JSON, as received (UTF-8 BOM removed), in Result.JSON.
	KindJSON Kind = "json"
	// KindInvalidJSON is a body with a JSON media type that is not valid JSON.
	// Result.Text holds the body unchanged.
	KindInvalidJSON Kind = "invalid_json"
	// KindText is a body with a text media type (text/* other than text/html
	// and text/event-stream). Result.Text holds the body unchanged.
	KindText Kind = "text"
)

// Result is the content of a fetched URL.
type Result struct {
	Kind Kind
	// MediaType is the response media type, lower case, without parameters
	// (for example "text/html" or "application/vnd.api+json").
	MediaType string
	// URL is the final URL, after redirects.
	URL string
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// ContentType is the Content-Type header as received, with parameters.
	// It is "" if the header is absent.
	ContentType string
	// Text is set for KindMarkdown, KindInvalidJSON and KindText.
	Text string
	// JSON is set for KindJSON only.
	JSON json.RawMessage
}

// ErrUnsupportedContentType is returned (wrapped) when the response media
// type is not HTML, PDF, JSON or text.
var ErrUnsupportedContentType = errors.New("unsupported content type")

// ErrBodyTooLarge is returned (wrapped) when the response body is larger than
// maxBodySize.
var ErrBodyTooLarge = errors.New("body too large")

// maxErrorBody is the maximum number of body bytes kept in StatusError.Body.
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
	// Result holds the response metadata and the converted body, if the body
	// could be converted (else Result.Kind is KindNone). It is never nil.
	Result *Result
}

func (e *StatusError) Error() string {
	msg := fmt.Sprintf("unexpected status code: %d", e.StatusCode)
	if e.Body != "" {
		msg += ": " + e.Body
	}
	return msg
}

// ResponseError is returned when the response status is 2xx but the body
// cannot be returned: unsupported media type, body too large, conversion
// failure or read failure. Err holds the cause.
type ResponseError struct {
	// Result holds the response metadata. Result.Kind is KindNone.
	Result *Result
	Err    error
}

func (e *ResponseError) Error() string { return e.Err.Error() }

func (e *ResponseError) Unwrap() error { return e.Err }

// hasErrorBody reports whether a non-2xx body with media type mt is kept in
// StatusError.Body.
func hasErrorBody(mt string) bool {
	return isJSONMediaType(mt) || mt == "text/plain"
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
