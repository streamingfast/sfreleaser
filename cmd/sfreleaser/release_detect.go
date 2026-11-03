package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/streamingfast/cli"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var ReleaseDetectCmd = Command(releaseDetect,
	"detect",
	"Print the version to release from the changelog, nothing when there is none",
	Flags(func(flags *pflag.FlagSet) {
		flags.String("changelog-path", "CHANGELOG.md", "Path where to find the changelog file, defaults to the 'release.changelog-path' config value")
		flags.String("before", "", "Git revision of the changelog to compare with, a version already there is not released again, in GitHub Actions defaults to the commit before the push or the base of the pull request")
		flags.String("github-output", os.Getenv("GITHUB_OUTPUT"), "File receiving the 'version' GitHub Actions output")
	}),
	Description(`
		Decides whether a version should be released from the changelog.

		The version is the first versioned section of the changelog. Nothing is released when
		that version was already the first versioned section at --before, or when its tag
		already exists. The command fails when the version is not the first section of the
		changelog, as the release notes are taken from the first section.

		The version is printed on standard output, the reason nothing is released on standard
		error.
	`),
)

func releaseDetect(cmd *cobra.Command, _ []string) error {
	global := mustGetGlobal(cmd)
	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	changelogPath, err := filepath.Abs(global.ResolveFile(sflags.MustGetString(cmd, "changelog-path")))
	if err != nil {
		return fmt.Errorf("resolving changelog path: %w", err)
	}

	content, err := os.ReadFile(changelogPath)
	if err != nil {
		return fmt.Errorf("reading changelog: %w", err)
	}

	before := sflags.MustGetString(cmd, "before")
	if before == "" {
		if before, err = gitHubEventBaseRevision(os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH")); err != nil {
			return err
		}
	}

	previous, err := changelogAt(before, changelogPath)
	if err != nil {
		return err
	}

	version, reason, err := detectRelease(string(content), previous, tagExists)
	if err != nil {
		return err
	}

	if reason != "" {
		fmt.Fprintln(os.Stderr, reason)
	}

	if output := sflags.MustGetString(cmd, "github-output"); output != "" {
		if err := writeGitHubOutput(output, "version", version); err != nil {
			return fmt.Errorf("writing GitHub output: %w", err)
		}
	}

	if version != "" {
		fmt.Println(version)
	}

	return nil
}

// gitHubEventBaseRevision is the revision a GitHub Actions event compares with, empty for an
// event without one, such as a manual run.
func gitHubEventBaseRevision(eventName, eventPath string) (string, error) {
	if eventName != "push" && eventName != "pull_request" {
		return "", nil
	}

	content, err := os.ReadFile(eventPath)
	if err != nil {
		return "", fmt.Errorf("reading GitHub event: %w", err)
	}

	var event struct {
		Before      string `json:"before"`
		PullRequest struct {
			Base struct {
				SHA string `json:"sha"`
			} `json:"base"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(content, &event); err != nil {
		return "", fmt.Errorf("decoding GitHub event: %w", err)
	}

	if eventName == "push" {
		return event.Before, nil
	}

	return event.PullRequest.Base.SHA, nil
}

// detectRelease returns the version to release from the changelog content, or the reason
// nothing is released. previous is the changelog content before the push, empty when unknown.
func detectRelease(changelog, previous string, tagExists func(version string) bool) (version string, reason string, err error) {
	version, firstSection, err := changelogVersions(strings.NewReader(changelog))
	if err != nil {
		return "", "", fmt.Errorf("reading changelog: %w", err)
	}

	if version == "" {
		return "", "No versioned section in the changelog, nothing to release", nil
	}

	if previous != "" {
		previousVersion, _, err := changelogVersions(strings.NewReader(previous))
		if err != nil {
			return "", "", fmt.Errorf("reading previous changelog: %w", err)
		}

		if previousVersion == version {
			return "", fmt.Sprintf("Version %s was already in the changelog before, nothing to release", version), nil
		}
	}

	if tagExists(version) {
		return "", fmt.Sprintf("Tag %s already exists, nothing to release", version), nil
	}

	if firstSection != version {
		return "", "", fmt.Errorf("%s must be the first section of the changelog, the release notes are taken from the first section", version)
	}

	return version, "", nil
}

// changelogAt is the changelog content at revision before, empty when there is no such revision
// (the zero SHA of a new branch) or the changelog did not exist then.
func changelogAt(before, changelogPath string) (string, error) {
	if before == "" || strings.Trim(before, "0") == "" {
		return "", nil
	}

	if _, _, err := maybeResultOf("git cat-file -e", "'"+before+"^{commit}'"); err != nil {
		return "", fmt.Errorf("commit %s given to --before is not in the repository, fetch it first (e.g. 'fetch-depth: 0' on 'actions/checkout')", before)
	}

	workingDirectory, err := os.Getwd()
	if err != nil {
		return "", err
	}

	relativePath, err := filepath.Rel(workingDirectory, changelogPath)
	if err != nil {
		return "", err
	}

	output, _, err := maybeResultOf("git show", "'"+before+":./"+relativePath+"'")
	if err != nil {
		return "", nil
	}

	return output, nil
}

func tagExists(version string) bool {
	_, _, err := maybeResultOf("git rev-parse -q --verify", "'refs/tags/"+version+"'")
	return err == nil
}
