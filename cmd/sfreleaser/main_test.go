package main

import (
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
