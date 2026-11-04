package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_extractVersionFromRuntimeCallerFile(t *testing.T) {
	tests := []struct {
		name string
		file string
		want string
	}{
		{"go install tagged", "/Users/jo/go/pkg/mod/github.com/streamingfast/sfreleaser@v0.6.1/cmd/sfreleaser/main.go", "v0.6.1"},
		{"go install tagged pre-release alpha", "/Users/jo/go/pkg/mod/github.com/streamingfast/sfreleaser@v0.6.1-alpha.1/cmd/sfreleaser/main.go", "v0.6.1-alpha.1"},
		{"go install branch", "/Users/jo/go/pkg/mod/github.com/streamingfast/sfreleaser@v0.6.1-0.20230714182518-fdcc9de52acb/cmd/sfreleaser/main.go", ""},
		{"dev", "/Users/jo/work/sf/sfreleaser/cmd/sfreleaser/main.go", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractVersionFromRuntimeCallerFile(tt.file))
		})
	}
}

func Test_ptyDisabledFromEnv(t *testing.T) {
	tests := []struct {
		name     string
		ci       string
		disable  *string
		expected bool
	}{
		{"local", "", nil, false},
		{"CI", "true", nil, true},
		{"CI with PTY forced on", "true", ptr("false"), false},
		{"local with PTY disabled", "", ptr("true"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CI", tt.ci)
			t.Setenv("SFRELEASER_DISABLE_PTY", "")
			os.Unsetenv("SFRELEASER_DISABLE_PTY")
			if tt.disable != nil {
				t.Setenv("SFRELEASER_DISABLE_PTY", *tt.disable)
			}

			assert.Equal(t, tt.expected, ptyDisabledFromEnv())
		})
	}
}
