package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/rs/cors"
)

func parseFlags() (isHttp bool, port string) {
	flag.BoolVar(&isHttp, "http", false, "Run as streamable HTTP instead of stdio")
	flag.StringVar(&port, "port", "8080", "Port to listen on for streamable HTTP")
	flag.Parse()

	return isHttp, port
}

func main() {
	isHttp, port := parseFlags()

	logger := log.New(os.Stdout, "", 0)

	// Create a server with the webfetch tool
	server := setupMCPServer()

	// Stdio transport
	if !isHttp {
		if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
			logger.Fatal(err)
		}
		return
	}

	// Streamable HTTP transport
	var handler http.Handler

	// Create Streamable HTTP handler
	handler = mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		nil,
	)

	// Add CORS handler
	handler = cors.New(cors.Options{
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

	fmt.Printf("MCP Server running in HTTP mode on port %s\n", port)
	logger.Fatal(http.ListenAndServe(":"+port, handler))
}
