package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/streamingfast/cli"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type releaseGithubArgs struct {
	global        *GlobalModel
	release       *ReleaseModel
	githubRelease *GitHubReleaseModel
}

func Test_releaseGithub(t *testing.T) {
	writeProjectFile := func(t *testing.T, args *releaseGithubArgs, name string, content string) *string {
		path := filepath.Join(args.global.WorkingDirectory, name)
		require.NoError(t, os.WriteFile(path, []byte(""), os.ModePerm))

		return &name
	}

	tests := []struct {
		name         string
		args         func(tt *testing.T) releaseGithubArgs
		expectedPath string
	}{
		{
			"no readme nor file",
			newReleaseGithubArgs(nil),
			"goreleaser/app/no_readme_nor_license.golden.yaml",
		},
		{
			"README.md file",
			newReleaseGithubArgs(func(tt *testing.T, args *releaseGithubArgs) {
				args.release.ReadmeRelativePath = writeProjectFile(t, args, "README.md", "")
			}),
			"goreleaser/app/readme_md_file.golden.yaml",
		},
		{
			"readme file and LICENSe",
			newReleaseGithubArgs(func(tt *testing.T, args *releaseGithubArgs) {
				args.release.LicenseRelativePath = writeProjectFile(t, args, "LICENSe", "")
				args.release.ReadmeRelativePath = writeProjectFile(t, args, "readme", "")
			}),
			"goreleaser/app/readme_file_and_license.golden.yaml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.args(t)
			renderGoreleaserFile(args.global, args.release, args.githubRelease)

			goldenUpdate := os.Getenv("GOLDEN_UPDATE") == "true"
			goldenPath := filepath.Join("testdata", tt.expectedPath)

			if !goldenUpdate && !cli.FileExists(goldenPath) {
				t.Fatalf("the golden file %q does not exist, re-run with 'GOLDEN_UPDATE=true go test ./... -run %q' to generate the intial version", goldenPath, t.Name())
			}

			content, err := os.ReadFile(args.githubRelease.GoreleaserConfigPath)
			require.NoError(t, err)

			if goldenUpdate {
				require.NoError(t, os.WriteFile(goldenPath, content, os.ModePerm))
			}

			expected, err := os.ReadFile(goldenPath)
			require.NoError(t, err)

			require.Equal(t, string(expected), string(content), "Run 'GOLDEN_UPDATE=true go test ./... -run %q' to update golden file", t.Name())

			// Ensure the generated file is valid YAML to avoid any syntax error at least
			var v any
			require.NoError(t, yaml.Unmarshal(expected, &v), "The generated goreleaser file is not valid YAML:\n\n%s", string(content))
		})
	}
}

func newReleaseGithubArgs(customize func(*testing.T, *releaseGithubArgs)) func(tt *testing.T) releaseGithubArgs {
	return func(tt *testing.T) releaseGithubArgs {
		tmpRoot := tt.TempDir()

		args := releaseGithubArgs{
			global: &GlobalModel{
				Owner:            "owner",
				Project:          "project",
				Root:             tmpRoot,
				ConfigRoot:       tmpRoot,
				WorkingDirectory: tmpRoot,
				Variant:          VariantApplication,
			},
			release: &ReleaseModel{
				Version: "v1.0.0",
			},
			githubRelease: &GitHubReleaseModel{
				GoreleaserConfigPath: filepath.Join(tmpRoot, "goreleaser.yml"),
			},
		}

		if customize != nil {
			customize(tt, &args)
		}

		return args
	}
}

func ptr[T any](v T) *T {
	return &v
}
