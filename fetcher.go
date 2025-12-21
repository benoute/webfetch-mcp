package webfetch

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

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

// removeTagsPlugin is a plugin that registers tags to be removed during conversion
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

// FetchAndConvert fetches the URL and converts its HTML content to Markdown.
// It removes common non-content elements and preserves links with absolute URLs.
func FetchAndConvert(ctx context.Context, rawURL string, timeout time.Duration) (string, error) {
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
	req.Header.Set("Accept", "text/html,application/xhtml+xml")

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

	// Validate content type is HTML
	contentType := resp.Header.Get("Content-Type")
	if !isHTMLContentType(contentType) {
		return "", fmt.Errorf("unsupported content type: %s (expected HTML)", contentType)
	}

	// Convert HTML to Markdown
	return convertHTMLToMarkdown(resp.Body, parsedURL)
}

// convertHTMLToMarkdown converts HTML content to Markdown, removing non-content elements
// and resolving relative URLs to absolute using the provided base URL.
func convertHTMLToMarkdown(r io.Reader, baseURL *url.URL) (string, error) {
	// Build domain string for absolute URL resolution
	domain := fmt.Sprintf("%s://%s", baseURL.Scheme, baseURL.Host)

	// Create converter with plugins including our tag removal plugin
	conv := converter.NewConverter(
		converter.WithPlugins(
			base.NewBasePlugin(),
			commonmark.NewCommonmarkPlugin(),
			&removeTagsPlugin{tags: tagsToRemove},
		),
	)

	// Convert HTML to Markdown with domain for absolute URL resolution
	markdownBytes, err := conv.ConvertReader(r, converter.WithDomain(domain))
	if err != nil {
		return "", fmt.Errorf("failed to convert HTML to Markdown: %w", err)
	}

	// Clean up excessive whitespace
	markdown := cleanupMarkdown(string(markdownBytes))

	return markdown, nil
}

// isHTMLContentType checks if the content type indicates HTML content
func isHTMLContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml+xml")
}

// cleanupMarkdown removes excessive blank lines from the markdown output
func cleanupMarkdown(markdown string) string {
	lines := strings.Split(markdown, "\n")
	var result []string
	blankCount := 0

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			blankCount++
			// Allow at most one blank line between content
			if blankCount <= 1 {
				result = append(result, line)
			}
		} else {
			blankCount = 0
			result = append(result, line)
		}
	}

	return strings.TrimSpace(strings.Join(result, "\n"))
}
