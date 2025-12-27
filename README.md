# webfetch-mcp

MCP server that fetches URLs and converts HTML or PDF content to clean Markdown.

## Quick Start

### Option 1: Download Pre-built Binary

Download the latest binary for your platform from [GitHub Releases](https://github.com/benoute/webfetch-mcp/releases).

| Platform     | Binary                      |
|--------------|-----------------------------|
| Linux x64    | `webfetch-mcp-linux-amd64`  |
| Linux ARM64  | `webfetch-mcp-linux-arm64`  |
| macOS x64    | `webfetch-mcp-darwin-amd64` |
| macOS ARM64  | `webfetch-mcp-darwin-arm64` |

```bash
# Make it executable
chmod +x webfetch-mcp-*

# Move to your PATH (optional)
mv webfetch-mcp-* /usr/local/bin/webfetch-mcp
```

### Option 2: Build from Source

Requires Go 1.23+

```bash
go build -o webfetch-mcp ./cmd/webfetch-mcp
```

## MCP Configuration

### Stdio Mode (default)

```json
{
  "command": "/path/to/webfetch-mcp"
}
```

### HTTP Mode

Start the server:

```bash
webfetch-mcp -http -port 8080
```

Then configure your MCP client:

```json
{
  "url": "http://localhost:8080/mcp"
}
```

## Tool: `webfetch`

Fetches a URL and converts its HTML or PDF content to Markdown.

**Supported Content Types:**
- HTML (`text/html`, `application/xhtml+xml`)
- PDF (`application/pdf`) - max 100MB

**Features:**
- Removes non-content elements (HTML): `nav`, `header`, `footer`, `aside`, `script`, `style`, `form`, `button`, `iframe`, `noscript`
- Resolves relative URLs to absolute (HTML)
- Extracts text with page separators (PDF)

**Input:**

| Parameter            | Type   | Required | Default  | Description                                      |
|----------------------|--------|----------|----------|--------------------------------------------------|
| `url`                | string | Yes      | -        | The URL to fetch                                 |
| `timeout`            | string | No       | `5s`     | Request timeout (e.g., `10s`, `1m`)              |
| `max_content_tokens` | int    | No       | `100000` | Maximum content length (truncated if exceeded)   |

**Example:**

```json
{
  "url": "https://example.com",
  "timeout": "10s",
  "max_content_tokens": 50000
}
```

**Output:** Clean Markdown text of the page content. If the content exceeds `max_content_tokens`, it is truncated and ends with `... (truncated)`.

For PDF files, the output includes page headers and separators:
```markdown
## Page 1

[text from page 1]

---

## Page 2

[text from page 2]
```

## Command-Line Options

| Flag    | Default | Description                         |
|---------|---------|-------------------------------------|
| `-http` | `false` | Run as HTTP server instead of stdio |
| `-port` | `8080`  | Port for HTTP mode                  |
