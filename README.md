# webfetch-mcp

MCP server that fetches URLs and converts HTML or PDF content to clean Markdown.

## Quick Start

### Option 1: Download Pre-built Binary

Download the latest binary for your platform from [GitHub Releases](https://github.com/benoute/webfetch-mcp/releases).
The Linux binaries are static and run on any distribution, Alpine included. The macOS binaries need macOS 13 (Ventura) or later.

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

### Option 2: Install with Go

Requires Go 1.27+. With Go 1.21–1.26 and the default `GOTOOLCHAIN=auto`, `go` downloads Go 1.27 for you.

```bash
go install github.com/benoute/webfetch-mcp/cmd/webfetch-mcp@latest
```

### Option 3: Build from Source

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

Start the server with a listen address:

```bash
webfetch-mcp -http 127.0.0.1:8080
```

Then configure your MCP client:

```json
{
  "url": "http://localhost:8080/mcp"
}
```

The value of `-http` is a `host:port` address. It is passed to Go's `net.Listen("tcp", …)` unchanged:

| `-http` value     | Listens on                                       |
|-------------------|--------------------------------------------------|
| `:8080`           | all interfaces, IPv4 and IPv6                    |
| `0.0.0.0:8080`    | all interfaces, IPv4 and IPv6 (Go behavior)      |
| `127.0.0.1:8080`  | loopback, IPv4 only                              |
| `[::1]:8080`      | loopback, IPv6 only                              |
| `localhost:8080`  | first address `localhost` resolves to            |
| `:0`              | all interfaces, random free port (printed at startup) |

A bare port (`-http 8080`) is an error. Use `-http :8080`.

#### Security

- Use a loopback address (`127.0.0.1:8080`) unless other machines must reach the server. Any client that can connect can make the server fetch URLs, including URLs on your internal network.
- HTTP mode allows cross-origin requests from browsers (CORS allows any origin). A web page that you open can call the server on `localhost` and read the result.
- The server rejects requests that arrive on a loopback address with a `Host` header that is not `localhost`, `127.0.0.1` or `[::1]` (HTTP 403). This blocks DNS-rebinding attacks.

#### Reverse proxy on the same machine

A proxy such as nginx, Caddy or `tailscale serve` that forwards to `127.0.0.1` with the public `Host` header (for example `mcp.example.com`) gets HTTP 403, because of the DNS-rebinding rule above. Configure the proxy to send `Host: localhost`. For example, with nginx:

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host localhost;
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

| Flag              | Default | Description                                                         |
|-------------------|---------|---------------------------------------------------------------------|
| `-http <address>` | (none)  | Run as streamable HTTP server on `<address>` (`host:port`). Without this flag, the server uses stdio. |

## Development

```bash
make check   # gofmt check, go vet, go test -race, govulncheck (same as CI)
make fix     # show `go fix` modernization suggestions (not applied)
```
