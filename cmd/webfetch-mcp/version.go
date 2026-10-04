package main

import "runtime/debug"

// version is set at release build time:
//
//	go build -ldflags "-X main.version=v1.2.0" ./cmd/webfetch-mcp
var version = ""

// buildVersion returns the version of this binary: the -ldflags value if
// set, else the module version recorded by `go install …@vX`, else "devel".
func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "devel"
}
