package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extractVersionFromHeader(t *testing.T) {
	type args struct {
		header string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{"empty", args{""}, ""},
		{"unreleased", args{"## Unreleased"}, ""},
		{"next", args{"## Next"}, ""},
		{"v1.0.0", args{"## v1.0.0"}, "v1.0.0"},
		{"v1.0.0 with square bracket", args{"## [1.0.0]"}, "v1.0.0"},
		{"4.1.5-evm-devnet-fh3.0 with dots", args{"## 4.1.5-evm-devnet-fh3.0"}, "v4.1.5-evm-devnet-fh3.0"},
		{"v4.1.5-evm-devnet-fh3.0 with dots and prefix v", args{"## v4.1.5-evm-devnet-fh3.0"}, "v4.1.5-evm-devnet-fh3.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, extractVersionFromHeader(tt.args.header))
		})
	}
}

func Test_readVersionFromChangelog(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
		want     string
	}{
		{
			name:     "non-existent file",
			filepath: "non-existent.md",
			want:     "",
		},
		{
			name:     "sample changelog with unreleased",
			filepath: "testdata/changelog/sample.md",
			want:     "", // Function returns empty because first header is "Unreleased" which gets skipped
		},
		{
			name:     "changelog without unreleased",
			filepath: "testdata/changelog/no-unreleased.md",
			want:     "v1.2.3",
		},
		{
			name:     "structured changelog",
			filepath: "testdata/changelog/structured.md",
			want:     "v2.0.0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readVersionFromChangelog(tt.filepath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_readReleaseNotes(t *testing.T) {
	tests := []struct {
		name     string
		filepath string
		want     string
	}{
		{
			name:     "non-existent file",
			filepath: "non-existent.md",
			want:     "",
		},
		{
			name:     "sample changelog",
			filepath: "testdata/changelog/sample.md",
			want:     "- Some unreleased change\n- Another unreleased feature",
		},
		{
			name:     "structured changelog",
			filepath: "testdata/changelog/structured.md",
			want:     "### Added\n- New authentication system\n- Support for multiple databases\n- Advanced logging capabilities\n\n### Changed\n- Refactored core API\n- Updated configuration format\n\n### Fixed\n- Memory leak in connection pool\n- Race condition in cache",
		},
		{
			name:     "changelog without unreleased",
			filepath: "testdata/changelog/no-unreleased.md",
			want:     "- Fixed critical bug in parsing\n- Added new feature X\n- Improved performance by 20%",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := readReleaseNotes(tt.filepath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_readReleaseNotes_withCustomContent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{
			name: "empty changelog",
			content: `# Changelog

This is empty.`,
			want: "",
		},
		{
			name: "single section with blank lines",
			content: `# Changelog

## v1.0.0

- First feature

- Second feature
  with multiline

- Third feature`,
			want: "- First feature\n\n- Second feature\n  with multiline\n\n- Third feature",
		},
		{
			name: "multiple sections stops at second header",
			content: `# Changelog

## v2.0.0

- New feature
- Breaking change

## v1.0.0

- Old feature
- Legacy stuff`,
			want: "- New feature\n- Breaking change",
		},
		{
			name: "section with subsections",
			content: `# Changelog

## v1.5.0

### Added
- New API endpoint
- Documentation

### Fixed
- Critical bug

## v1.4.0

- Previous version`,
			want: "### Added\n- New API endpoint\n- Documentation\n\n### Fixed\n- Critical bug",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := createTempChangelog(t, tt.content)
			got := readReleaseNotes(tmpFile)
			assert.Equal(t, tt.want, got)
		})
	}
}

func createTempChangelog(t *testing.T, content string) string {
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "CHANGELOG.md")
	err := os.WriteFile(tmpFile, []byte(content), 0644)
	require.NoError(t, err)
	return tmpFile
}
