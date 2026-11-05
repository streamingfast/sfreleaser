package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_detectRelease(t *testing.T) {
	const released = "# Changelog\n\n## v1.2.0\n\n- Feature\n\n## v1.1.0\n\n- Older\n"
	const unreleased = "# Changelog\n\n## Unreleased\n\n- Pending\n\n## v1.1.0\n\n- Older\n"
	noTag := func(string) bool { return false }

	tests := []struct {
		name        string
		changelog   string
		previous    string
		tagExists   func(string) bool
		wantVersion string
		wantReason  string
		wantErr     string
	}{
		{"new version heading", released, unreleased, noTag, "v1.2.0", "", ""},
		{"no previous changelog", released, "", noTag, "v1.2.0", "", ""},
		{"version already there", released, released, noTag, "", "Version v1.2.0 was already in the changelog before, nothing to release", ""},
		{"unreleased on top", unreleased, unreleased, noTag, "", "Version v1.1.0 was already in the changelog before, nothing to release", ""},
		{"tag exists", released, unreleased, func(v string) bool { return v == "v1.2.0" }, "", "Tag v1.2.0 already exists, nothing to release", ""},
		{"no versioned section", "# Changelog\n\n## Unreleased\n", "", noTag, "", "No versioned section in the changelog, nothing to release", ""},
		{"version not first section", "## Unreleased\n\n## v1.2.0\n", "## v1.1.0\n", noTag, "", "", "v1.2.0 must be the first section of the changelog, the release notes are taken from the first section"},
		{"version without v prefix", "## 1.2.0\n", "## 1.1.0\n", noTag, "v1.2.0", "", ""},
		{"pre-release version", "## v1.2.0-rc.1\n\n## v1.1.0\n", "## v1.1.0\n", noTag, "v1.2.0-rc.1", "", ""},
		{"previous without v prefix", "## v1.2.0\n", "## 1.2.0\n", noTag, "", "Version v1.2.0 was already in the changelog before, nothing to release", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, reason, err := detectRelease(tt.changelog, tt.previous, tt.tagExists)
			if tt.wantErr != "" {
				require.EqualError(t, err, tt.wantErr)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantVersion, version)
			assert.Equal(t, tt.wantReason, reason)
		})
	}
}

func Test_detectOutputs(t *testing.T) {
	assert.Equal(t, [][2]string{{"version", ""}, {"is_release_pr", ""}, {"preview_comment", ""}}, detectOutputs("", ""))
	assert.Equal(t, [][2]string{{"version", "v1.2.0"}, {"is_release_pr", "true"}, {"preview_comment", "### Merging this PR will release v1.2.0\n\n<details><summary>Release notes</summary>\n\n- Feature\n\n</details>"}}, detectOutputs("v1.2.0", "- Feature"))
	assert.Equal(t, "### Merging this PR will release v1.2.0\n\n<details><summary>Release notes</summary>\n\n_No release notes in the changelog._\n\n</details>", detectOutputs("v1.2.0", "")[2][1])

	assert.Equal(t, "### Merging this PR will release v1.2.0\n\n- Publishes the crates `a`, `b`\n- Publishes the [draft release](https://d)\n\n<details><summary>Release notes</summary>\n\n- Feature\n\n</details>", releaseComment("v1.2.0", "- Feature", []string{"Publishes the crates `a`, `b`", "Publishes the [draft release](https://d)"}))
}

func Test_gitHubEventBaseRevision(t *testing.T) {
	eventPath := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(eventPath, []byte(`{"before":"abc","pull_request":{"base":{"sha":"def"}}}`), 0o644))

	tests := []struct {
		eventName string
		eventPath string
		want      string
	}{
		{"push", eventPath, "abc"},
		{"pull_request", eventPath, "def"},
		{"workflow_dispatch", eventPath, ""},
		{"", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.eventName, func(t *testing.T) {
			revision, err := gitHubEventBaseRevision(tt.eventName, tt.eventPath)
			require.NoError(t, err)
			assert.Equal(t, tt.want, revision)
		})
	}
}

func Test_prepareSteps(t *testing.T) {
	crates := &ReleaseModel{Rust: &RustReleaseModel{Crates: []string{"a", "b"}}}
	assert.Equal(t, []string{"Publishes the crates [`a`](https://crates.io/crates/a), [`b`](https://crates.io/crates/b)", "Publishes the [draft release](https://d)"}, prepareSteps(crates, "", "https://d"))

	crates.Rust.CargoPublishArgs = []string{"--registry", "kellnr"}
	assert.Equal(t, []string{"Publishes the crates `a`, `b`", "Merges the [Homebrew formula pull request](https://b)", "Publishes the [draft release](https://d)"}, prepareSteps(crates, "https://b", "https://d"))

	assert.Equal(t, []string{"Publishes the package to [substreams.dev](https://substreams.dev)", "Publishes the [draft release](https://d)"}, prepareSteps(&ReleaseModel{Substreams: &SubstreamsReleaseModel{}}, "", "https://d"))
	assert.Equal(t, []string{"Publishes the [draft release](https://d)"}, prepareSteps(&ReleaseModel{}, "", "https://d"))
}

func Test_gitHubEventPullRequest(t *testing.T) {
	event := filepath.Join(t.TempDir(), "event.json")
	require.NoError(t, os.WriteFile(event, []byte(`{"pull_request":{"number":321,"merged":false,"merge_commit_sha":"test-merge"}}`), 0o600))

	number, mergeCommit, err := gitHubEventPullRequest("pull_request", event)
	require.NoError(t, err)
	assert.Equal(t, 321, number)
	assert.Equal(t, "", mergeCommit)

	require.NoError(t, os.WriteFile(event, []byte(`{"pull_request":{"number":321,"merged":true,"merge_commit_sha":"abc123"}}`), 0o600))
	number, mergeCommit, err = gitHubEventPullRequest("pull_request", event)
	require.NoError(t, err)
	assert.Equal(t, 321, number)
	assert.Equal(t, "abc123", mergeCommit)

	number, mergeCommit, err = gitHubEventPullRequest("push", event)
	require.NoError(t, err)
	assert.Equal(t, 0, number)
	assert.Equal(t, "", mergeCommit)
}
