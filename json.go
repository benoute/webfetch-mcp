package webfetch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte("\xEF\xBB\xBF")

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

// readJSON reads the body, removes a UTF-8 BOM and reports whether the
// remaining bytes are valid JSON.
func readJSON(r io.Reader) (body []byte, valid bool, err error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, false, fmt.Errorf("failed to read body: %w", err)
	}
	b = bytes.TrimPrefix(b, utf8BOM)
	return b, json.Valid(b), nil
}
