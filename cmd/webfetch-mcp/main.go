package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/cors"
)

// config is the parsed command line.
type config struct {
	// HTTPAddr is the listen address for streamable HTTP mode.
	// Empty means stdio mode. Otherwise it is accepted by net.SplitHostPort.
	HTTPAddr string
}

// parseFlags parses args (without argv[0]) on fs.
func parseFlags(fs *flag.FlagSet, args []string) (config, error) {
	var cfg config
	fs.Func("http",
		"run as streamable HTTP on the listen `address` (host:port, e.g. :8080 or 127.0.0.1:8080); omit for stdio",
		func(v string) error {
			if err := validateListenAddr(v); err != nil {
				return err
			}
			cfg.HTTPAddr = v
			return nil
		})
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	return cfg, nil
}

// validateListenAddr checks the syntax of a -http value. Any string that
// net.SplitHostPort accepts is valid. Port range, named ports and host names
// are checked later by net.Listen.
func validateListenAddr(s string) error {
	_, _, err := net.SplitHostPort(s)
	if err == nil {
		return nil
	}
	if isAllDigits(s) {
		return fmt.Errorf("%w; did you mean -http :%s?", err, s)
	}
	return err
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// newHTTPHandler wraps the MCP streamable HTTP handler with CORS.
func newHTTPHandler(server *mcp.Server) http.Handler {
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)

	return cors.New(cors.Options{
		AllowOriginFunc: func(origin string) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders: []string{
			"Content-Type",
			"Authorization",
			"Mcp-Session-Id",
			"mcp-protocol-version",
		},
		ExposedHeaders:   []string{"Mcp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}).Handler(handler)
}

// serveHTTP listens on addr, logs the bound address and serves until ctx is
// done (returns nil) or the server fails (returns the error).
func serveHTTP(ctx context.Context, server *mcp.Server, addr string, logger *log.Logger) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	srv := &http.Server{Handler: newHTTPHandler(server)}
	stop := context.AfterFunc(ctx, func() { srv.Close() })
	defer stop()

	logger.Printf("MCP Server running in HTTP mode on %s", ln.Addr())
	err = srv.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) && ctx.Err() != nil {
		return nil
	}
	return err
}

func main() {
	cfg, _ := parseFlags(flag.CommandLine, os.Args[1:]) // CommandLine exits on error

	logger := log.New(os.Stdout, "", 0)

	// Create a server with the webfetch tool
	server := setupMCPServer()

	if cfg.HTTPAddr == "" {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			logger.Fatal(err)
		}
		return
	}

	logger.Fatal(serveHTTP(context.Background(), server, cfg.HTTPAddr, logger))
}
