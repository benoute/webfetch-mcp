package main

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestBuildVersion(t *testing.T) {
	t.Run("ldflags value wins", func(t *testing.T) {
		prev := version
		version = "v9.9.9"
		t.Cleanup(func() { version = prev })

		if got := buildVersion(); got != "v9.9.9" {
			t.Errorf("buildVersion() = %q, want %q", got, "v9.9.9")
		}
	})

	t.Run("fallback is never empty", func(t *testing.T) {
		prev := version
		version = ""
		t.Cleanup(func() { version = prev })

		// Test binaries have Main.Version "" or "(devel)" → "devel".
		if got := buildVersion(); got != "devel" {
			t.Errorf("buildVersion() = %q, want %q", got, "devel")
		}
	})
}

func TestSetupMCPServer_ServerInfoVersion(t *testing.T) {
	prev := version
	version = "v9.9.9"
	t.Cleanup(func() { version = prev })

	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	if _, err := setupMCPServer().Connect(ctx, serverT, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, clientT, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })

	info := session.InitializeResult().ServerInfo
	if info.Name != "webfetch" || info.Version != "v9.9.9" {
		t.Errorf("serverInfo = %+v, want name webfetch, version v9.9.9", info)
	}
}
