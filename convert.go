package webfetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime"
	"net/url"
	"strings"
)

// convert sets res.Kind and res.Text or res.JSON from a non-empty body, by
// res.MediaType. HTML links are resolved against res.URL.
func convert(body []byte, res *Result) error {
	mt := res.MediaType
	switch {
	// PDF → Markdown with page separators
	case isPDFMediaType(mt):
		md, err := convertPDFToMarkdown(body)
		if err != nil {
			return err
		}
		res.Kind, res.Text = KindMarkdown, md

	// HTML → Markdown, links absolute against the final URL
	case isHTMLMediaType(mt):
		base, err := url.Parse(res.URL)
		if err != nil {
			return fmt.Errorf("invalid final URL: %w", err)
		}
		md, err := convertHTMLToMarkdown(bytes.NewReader(body), base)
		if err != nil {
			return err
		}
		res.Kind, res.Text = KindMarkdown, md

	// JSON → verbatim, or text if invalid
	case isJSONMediaType(mt):
		if json.Valid(body) {
			res.Kind, res.JSON = KindJSON, body
		} else {
			res.Kind, res.Text = KindInvalidJSON, string(body)
		}

	// text/* (not HTML, not event stream) → as is
	case isTextMediaType(mt):
		res.Kind, res.Text = KindText, string(body)

	default:
		return fmt.Errorf("%w: %s (expected HTML, PDF, JSON or text)", ErrUnsupportedContentType, res.ContentType)
	}
	return nil
}

// isJSONMediaType reports whether mt (from mediaType) is a JSON media type:
// application/json, a legacy alias, or an application/*+json type (RFC 6839).
// JSON streams (json-seq, NDJSON, JSON Lines) are not JSON media types.
func isJSONMediaType(mt string) bool {
	switch mt {
	case "application/json", "text/json", "application/x-json", "text/x-json":
		return true
	}
	sub, ok := strings.CutPrefix(mt, "application/")
	return ok && len(sub) > len("+json") && strings.HasSuffix(sub, "+json")
}

// isTextMediaType reports whether mt (from mediaType) is a text media type:
// text/* other than HTML, event streams and the JSON aliases.
func isTextMediaType(mt string) bool {
	return strings.HasPrefix(mt, "text/") && len(mt) > len("text/") &&
		!isHTMLMediaType(mt) && mt != "text/event-stream" && !isJSONMediaType(mt)
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
