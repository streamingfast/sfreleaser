package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func Test_renderGoreleaserFile_brewPullRequest(t *testing.T) {
	tests := []struct {
		name        string
		pullRequest bool
		expect      func(t *testing.T, repository map[string]any)
	}{
		{
			name:        "direct commit",
			pullRequest: false,
			expect: func(t *testing.T, repository map[string]any) {
				assert.NotContains(t, repository, "pull_request")
			},
		},
		{
			name:        "pull request",
			pullRequest: true,
			expect: func(t *testing.T, repository map[string]any) {
				assert.Equal(t, map[string]any{"enabled": true}, repository["pull_request"])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			global := &GlobalModel{
				Owner:    "streamingfast",
				Project:  "acme",
				Binary:   "acme",
				Language: LanguageGolang,
				License:  "Apache-2.0",
				Variant:  VariantApplication,
			}

			release := &ReleaseModel{
				Brew: &BrewReleaseModel{
					PullRequest:  tt.pullRequest,
					TapRepoOwner: "streamingfast",
					TapRepoName:  "homebrew-tap",
				},
			}

			configPath := filepath.Join(t.TempDir(), "goreleaser.yaml")
			renderGoreleaserFile(global, release, &GitHubReleaseModel{GoreleaserConfigPath: configPath})

			content, err := os.ReadFile(configPath)
			require.NoError(t, err)

			var config struct {
				Brews []struct {
					Repository map[string]any `yaml:"repository"`
				} `yaml:"brews"`
			}
			require.NoError(t, yaml.Unmarshal(content, &config))
			require.Len(t, config.Brews, 1)

			tt.expect(t, config.Brews[0].Repository)
		})
	}
}
