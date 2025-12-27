package webfetch

import (
	// "bytes"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
)

// tagsToRemove contains HTML tags that typically contain non-content elements
var tagsToRemove = []string{
	"nav",
	"header",
	"footer",
	"aside",
	"script",
	"style",
	"noscript",
	"form",
	"button",
	"iframe",
}

// removeTagsPlugin is a 'converter' plugin that registers tags to be removed during conversion
type removeTagsPlugin struct {
	tags []string
}

func (p *removeTagsPlugin) Name() string {
	return "remove-tags"
}

func (p *removeTagsPlugin) Init(conv *converter.Converter) error {
	for _, tag := range p.tags {
		conv.Register.TagType(tag, converter.TagTypeRemove, converter.PriorityStandard)
	}
	return nil
}

// Create converter with plugins including our tag removal plugin
var htmlConverter = converter.NewConverter(
	converter.WithPlugins(
		base.NewBasePlugin(),
		commonmark.NewCommonmarkPlugin(),
		&removeTagsPlugin{tags: tagsToRemove},
	),
)

// isHTMLContentType checks if the content type indicates HTML content
func isHTMLContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml")
}

// convertHTMLToMarkdown converts HTML content to Markdown, removing non-content elements
// and resolving relative URLs to absolute using the provided base URL.
func convertHTMLToMarkdown(r io.Reader, baseURL *url.URL) (string, error) {
	// Build domain string for absolute URL resolution
	domain := fmt.Sprintf("%s://%s", baseURL.Scheme, baseURL.Host)

	// Convert HTML to Markdown with domain for absolute URL resolution
	markdownBytes, err := htmlConverter.ConvertReader(r, converter.WithDomain(domain))
	if err != nil {
		return "", fmt.Errorf("failed to convert HTML to Markdown: %w", err)
	}

	return string(markdownBytes), nil
}
