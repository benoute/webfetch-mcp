package main

import (
	"flag"
	"os"
	"testing"
)

func TestParseFlags(t *testing.T) {
	// Save original args and flag command line
	originalArgs := os.Args
	originalFlagCommandLine := flag.CommandLine
	defer func() {
		os.Args = originalArgs
		flag.CommandLine = originalFlagCommandLine
	}()

	tests := []struct {
		name         string
		args         []string
		expectedHttp bool
		expectedPort string
	}{
		{
			name:         "default values",
			args:         []string{"cmd"},
			expectedHttp: false,
			expectedPort: "8080",
		},
		{
			name:         "http mode with custom port",
			args:         []string{"cmd", "-http", "-port", "9090"},
			expectedHttp: true,
			expectedPort: "9090",
		},
		{
			name:         "only http flag",
			args:         []string{"cmd", "-http"},
			expectedHttp: true,
			expectedPort: "8080",
		},
		{
			name:         "only port",
			args:         []string{"cmd", "-port", "7070"},
			expectedHttp: false,
			expectedPort: "7070",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flag command line for each test
			flag.CommandLine = flag.NewFlagSet(tt.args[0], flag.ContinueOnError)
			os.Args = tt.args

			isHttp, port := parseFlags()

			if isHttp != tt.expectedHttp {
				t.Errorf("Expected http %v, got %v", tt.expectedHttp, isHttp)
			}
			if port != tt.expectedPort {
				t.Errorf("Expected port %s, got %s", tt.expectedPort, port)
			}
		})
	}
}
