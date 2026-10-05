# webfetch-mcp

MCP server that fetches URLs. It converts HTML or PDF content to clean Markdown and returns JSON content unchanged.

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

## Tool: `fetch`

Fetches a URL. Converts HTML or PDF content to Markdown. Returns JSON as structured content.

**Supported Content Types:**
- HTML (`text/html`, `application/xhtml+xml`)
- PDF (`application/pdf`) - max 100MB
- JSON (`application/json`, `application/*+json` such as `application/problem+json`, and the legacy `text/json`, `application/x-json`, `text/x-json`)

JSON streams (`application/json-seq`, `application/x-ndjson`, `application/jsonl`) and other types (`text/plain`, `application/octet-stream`, …) are not supported. The server does not examine the body to find the type.

**Features:**
- Removes non-content elements (HTML): `nav`, `header`, `footer`, `aside`, `script`, `style`, `form`, `button`, `iframe`, `noscript`
- Resolves relative URLs to absolute (HTML)
- Extracts text with page separators (PDF)
- Returns JSON as `structuredContent`, with no Markdown conversion (JSON)

**Input:**

| Parameter           | Type   | Required | Default  | Description                                                    |
|---------------------|--------|----------|----------|----------------------------------------------------------------|
| `url`               | string | Yes      | -        | The URL to fetch                                               |
| `timeout`           | string | No       | `10s`    | Request timeout (e.g., `10s`, `1m`)                            |
| `max_content_bytes` | int    | No       | no limit | Maximum content size in bytes. `0` means no limit. See below.  |

**Example:**

```json
{
  "url": "https://api.github.com/repos/benoute/webfetch-mcp",
  "timeout": "10s",
  "max_content_bytes": 50000
}
```

**Output:**

| Response                                   | Tool result                                                                 |
|--------------------------------------------|-----------------------------------------------------------------------------|
| 2xx, HTML or PDF                           | Text content: Markdown                                                      |
| 2xx, JSON                                  | `"content": []` and `"structuredContent"`: the JSON value (object, array or primitive) |
| 2xx, JSON media type but body is not valid JSON | Text content: `[invalid JSON from server, <media type>]` line, then the body |
| Not 2xx                                    | Error: `unexpected status code: <code>`. For a JSON or `text/plain` body, the first 4 KiB of the body follow (`: <body>`). |
| Other content type                         | Error: `unsupported content type: …`                                       |

Example result for a JSON response:

```json
{ "content": [], "structuredContent": { "id": 1, "name": "x" } }
```

The JSON is not copied into a text block. Your MCP client must support `structuredContent` (protocol version 2025-06-18 or later). Some clients that use protocol version 2025-11-25 or earlier accept only a JSON object in `structuredContent`, and can fail with a top-level array or primitive.

**`max_content_bytes`:** The limit applies to the Markdown, or to the body bytes for JSON. If the content is larger:
- Markdown and invalid-JSON text are cut on a UTF-8 character boundary. The text starts with a marker line and ends with `... (truncated)`:
  ```
  [Markdown truncated: 25 bytes > max_content_bytes 10]
  # Title

  H

  ... (truncated)
  ```
- JSON is returned as truncated text (marker line `[JSON truncated: …]`), not as `structuredContent`.

Marker lines and the `... (truncated)` suffix are not counted in the limit.

For PDF files, the output includes page headers and separators:
```markdown
## Page 1

[text from page 1]

---

## Page 2

[text from page 2]
```

### Breaking changes

- The `max_content_tokens` input is removed. Use `max_content_bytes`. A call with `max_content_tokens` fails input validation.
- The content is not limited by default (was 100000).
- The default timeout is `10s` (was `5s`).
- A JSON response is returned as `structuredContent` (was the error `unsupported content type`).
- All 2xx statuses are success (was 200 only). A JSON or `text/plain` error body is included in the error message.
- Go library: `FetchAndConvert(ctx, url, timeout) (string, error)` is now `Fetch(ctx, url, timeout) (*Result, error)`. A non-2xx status returns a `*StatusError`. An unsupported type returns an error that wraps `ErrUnsupportedContentType`.

## Command-Line Options

| Flag              | Default | Description                                                         |
|-------------------|---------|---------------------------------------------------------------------|
| `-http <address>` | (none)  | Run as streamable HTTP server on `<address>` (`host:port`). Without this flag, the server uses stdio. |

## Development

```bash
make check   # gofmt check, go vet, go test -race, govulncheck (same as CI)
make fix     # show `go fix` modernization suggestions (not applied)
```
