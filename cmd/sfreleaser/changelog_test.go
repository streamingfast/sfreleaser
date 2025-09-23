package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_extractChangelogSection(t *testing.T) {
	tests := []struct {
		name             string
		filepath         string
		startHeaderRegex string
		endHeaderRegex   string
		want             string
		wantErr          bool
	}{
		{
			name:             "non-existent file",
			filepath:         "non-existent.md",
			startHeaderRegex: "## .+",
			endHeaderRegex:   "## .+",
			want:             "",
			wantErr:          true,
		},
		{
			name:             "first section from sample",
			filepath:         "testdata/changelog/sample.md",
			startHeaderRegex: "^## .+",
			endHeaderRegex:   "^## .+",
			want:             "- Some unreleased change\n- Another unreleased feature",
		},
		{
			name:             "specific version from sample",
			filepath:         "testdata/changelog/sample.md",
			startHeaderRegex: `^## v1\.2\.3`,
			endHeaderRegex:   "^## .+",
			want:             "- Fixed critical bug in parsing\n- Added new feature X\n- Improved performance by 20%",
		},
		{
			name:             "version with brackets from structured",
			filepath:         "testdata/changelog/structured.md",
			startHeaderRegex: `^## \[v2\.0\.0\].*`,
			endHeaderRegex:   "^## .+",
			want:             "### Added\n- New authentication system\n- Support for multiple databases\n- Advanced logging capabilities\n\n### Changed\n- Refactored core API\n- Updated configuration format\n\n### Fixed\n- Memory leak in connection pool\n- Race condition in cache",
		},
		{
			name:             "invalid start regex",
			filepath:         "testdata/changelog/sample.md",
			startHeaderRegex: "[invalid regex",
			endHeaderRegex:   "^## .+",
			want:             "",
			wantErr:          true,
		},
		{
			name:             "invalid end regex",
			filepath:         "testdata/changelog/sample.md",
			startHeaderRegex: "^## .+",
			endHeaderRegex:   "[invalid regex",
			want:             "",
			wantErr:          true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractChangelogSection(tt.filepath, tt.startHeaderRegex, tt.endHeaderRegex)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func Test_extractChangelogSection_withCustomContent(t *testing.T) {
	tests := []struct {
		name             string
		content          string
		startHeaderRegex string
		endHeaderRegex   string
		want             string
	}{
		{
			name: "no matching section",
			content: `# Changelog

## v1.0.0

- Some content`,
			startHeaderRegex: "## v2\\.0\\.0",
			endHeaderRegex:   "## .+",
			want:             "",
		},
		{
			name: "section with subsections",
			content: `# Changelog

## v2.0.0

### Added
- Feature A
- Feature B

### Fixed
- Bug fix

## v1.0.0

- Old stuff`,
			startHeaderRegex: `^## v2\.0\.0`,
			endHeaderRegex:   "^## .+",
			want:             "### Added\n- Feature A\n- Feature B\n\n### Fixed\n- Bug fix",
		},
		{
			name: "custom end regex",
			content: `# Changelog

## v2.0.0

- New feature
- Bug fix

### Breaking Changes

- API change

## v1.0.0

- Initial release`,
			startHeaderRegex: `^## v2\.0\.0`,
			endHeaderRegex:   "### Breaking Changes",
			want:             "- New feature\n- Bug fix",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpFile := createTempChangelog(t, tt.content)
			got, err := extractChangelogSection(tmpFile, tt.startHeaderRegex, tt.endHeaderRegex)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
