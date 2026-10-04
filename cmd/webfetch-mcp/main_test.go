package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantAddr string
		wantErr  string // substring; "" = no error
	}{
		{name: "no flags is stdio", args: nil, wantAddr: ""},
		{name: "http all interfaces", args: []string{"-http", ":8080"}, wantAddr: ":8080"},
		{name: "http equals form", args: []string{"-http=127.0.0.1:0"}, wantAddr: "127.0.0.1:0"},
		{name: "http ipv6 loopback", args: []string{"-http", "[::1]:8080"}, wantAddr: "[::1]:8080"},
		{name: "http without value", args: []string{"-http"}, wantErr: "flag needs an argument: -http"},
		{name: "http empty value", args: []string{"-http", ""}, wantErr: "missing port in address"},
		{name: "bare port hints", args: []string{"-http", "8080"}, wantErr: "did you mean -http :8080?"},
		{
			name:    "old -http -port form",
			args:    []string{"-http", "-port", "9090"},
			wantErr: `invalid value "-port" for flag -http`,
		},
		{name: "-port removed", args: []string{"-port", "9090"}, wantErr: "flag provided but not defined: -port"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fs := flag.NewFlagSet("webfetch-mcp", flag.ContinueOnError)
			fs.SetOutput(io.Discard)

			cfg, err := parseFlags(fs, tt.args)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.HTTPAddr != tt.wantAddr {
				t.Errorf("HTTPAddr = %q, want %q", cfg.HTTPAddr, tt.wantAddr)
			}
		})
	}
}

func TestValidateListenAddr(t *testing.T) {
	// Syntax only: everything net.SplitHostPort accepts is valid, even if
	// net.Listen would reject it later (named/out-of-range ports, unknown hosts).
	valid := []string{
		":", ":0", ":8080", "0.0.0.0:8080", "[::]:8080", "[::1]:", ":http",
		"127.0.0.1:8080", "[::1]:8080", "localhost:8080", "localhost:http",
		"foo:bar", "host:99999", "a:", "[fe80::1%en0]:8080",
	}
	for _, s := range valid {
		t.Run("valid "+s, func(t *testing.T) {
			if err := validateListenAddr(s); err != nil {
				t.Errorf("validateListenAddr(%q) = %v, want nil", s, err)
			}
		})
	}

	invalid := []struct {
		in      string
		wantErr string
	}{
		{"8080", "missing port in address; did you mean -http :8080?"},
		{"70000", "did you mean -http :70000?"},
		{"[::1]", "missing port in address"},
		{"8080x", "missing port in address"},
		{"-port", "missing port in address"},
		{"", "missing port in address"},
		{"::1:8080", "too many colons in address"},
		{"a:b:c", "too many colons in address"},
	}
	for _, tt := range invalid {
		t.Run("invalid "+tt.in, func(t *testing.T) {
			err := validateListenAddr(tt.in)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateListenAddr(%q) = %v, want containing %q", tt.in, err, tt.wantErr)
			}
			if !strings.Contains(tt.wantErr, "did you mean") && strings.Contains(err.Error(), "did you mean") {
				t.Errorf("validateListenAddr(%q) = %v, unexpected hint", tt.in, err)
			}
		})
	}
}

// lineWriter sends each write (one log line) to a channel without blocking.
type lineWriter chan string

func (w lineWriter) Write(p []byte) (int, error) {
	select {
	case w <- string(p):
	default:
	}
	return len(p), nil
}

// startTestServer runs serveHTTP on addr until the test ends. It returns the
// bound address taken from the startup log line.
func startTestServer(t *testing.T, addr string) (boundAddr string) {
	t.Helper()

	lines := make(lineWriter, 8)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serveHTTP(ctx, setupMCPServer(), addr, log.New(lines, "", 0)) }()

	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("serveHTTP returned %v after cancel, want nil", err)
		}
	})

	const prefix = "MCP Server running in HTTP mode on "
	select {
	case line := <-lines:
		got, ok := strings.CutPrefix(strings.TrimSpace(line), prefix)
		if !ok {
			t.Fatalf("unexpected startup log %q", line)
		}
		return got
	case err := <-done:
		t.Fatalf("serveHTTP exited early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server start")
	}
	return ""
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
	`"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

// postInitialize sends an MCP initialize request. host overrides the Host
// header when non-empty; hdr adds extra headers.
func postInitialize(t *testing.T, url, host string, hdr map[string]string) *http.Response {
	t.Helper()

	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(initializeBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if host != "" {
		req.Host = host
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestServeHTTP_BoundAddr(t *testing.T) {
	bound := startTestServer(t, "127.0.0.1:0")

	host, port, err := net.SplitHostPort(bound)
	if err != nil {
		t.Fatalf("bound address %q: %v", bound, err)
	}
	if host != "127.0.0.1" || port == "0" {
		t.Fatalf("bound address = %q, want 127.0.0.1:<non-zero port>", bound)
	}

	resp := postInitialize(t, "http://"+bound+"/", "", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("initialize status = %d, want 200", resp.StatusCode)
	}
	if resp.Header.Get("Mcp-Session-Id") == "" {
		t.Error("missing Mcp-Session-Id header")
	}
}

func TestServeHTTP_ListenError(t *testing.T) {
	err := serveHTTP(context.Background(), setupMCPServer(), "127.0.0.1:99999", log.New(io.Discard, "", 0))
	var opErr *net.OpError
	if !errors.As(err, &opErr) || opErr.Op != "listen" {
		t.Fatalf("err = %v, want a listen *net.OpError", err)
	}
}

// go-sdk ≥ 1.4: a request that arrives on a loopback address with a
// non-localhost Host header is rejected (DNS rebinding protection).
func TestHTTP_DNSRebindingGuard(t *testing.T) {
	bound := startTestServer(t, "127.0.0.1:0")
	_, port, _ := net.SplitHostPort(bound)

	tests := []struct {
		host string
		want int
	}{
		{"evil.example:" + port, http.StatusForbidden},
		{"mcp.example.com", http.StatusForbidden},
		{"localhost:" + port, http.StatusOK},
		{"127.0.0.1:" + port, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			resp := postInitialize(t, "http://"+bound+"/", tt.host, nil)
			if resp.StatusCode != tt.want {
				t.Errorf("Host %q: status = %d, want %d", tt.host, resp.StatusCode, tt.want)
			}
		})
	}
}

// CORS stays permissive: any Origin is reflected, with credentials.
func TestHTTP_CORSUnchanged(t *testing.T) {
	bound := startTestServer(t, "127.0.0.1:0")
	const origin = "https://x.example"

	resp := postInitialize(t, "http://"+bound+"/", "", map[string]string{
		"Origin":         origin,
		"Sec-Fetch-Site": "cross-site",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Access-Control-Allow-Origin"); got != origin {
		t.Errorf("Access-Control-Allow-Origin = %q, want %q", got, origin)
	}
	if got := resp.Header.Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Errorf("Access-Control-Allow-Credentials = %q, want \"true\"", got)
	}
	if got := resp.Header.Get("Access-Control-Expose-Headers"); !strings.Contains(got, "Mcp-Session-Id") {
		t.Errorf("Access-Control-Expose-Headers = %q, want Mcp-Session-Id", got)
	}
}
