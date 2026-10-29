package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var ChangelogDetectReleaseCmd = Command(changelogDetectRelease,
	"detect-release [<file>]",
	"Print the version a push should release, nothing when it should not release",
	Flags(func(flags *pflag.FlagSet) {
		flags.String("before", "", "Git revision of the changelog before the push, a version already present there is not released again")
		flags.Bool("skip-fork-pull-request", false, "Release nothing when the commit came from a pull request of a fork, the release is then started by hand")
		flags.String("github-output", "", "Path to GITHUB_OUTPUT file for GitHub Actions (format: 'path' or 'variable:path', defaults to 'version' variable)")
	}),
	Description(`
		Decides whether a push to the release branch releases a version, from the changelog file
		(defaults to "CHANGELOG.md").

		The version is the first versioned section of the changelog. Nothing is released when
		that version was already the first versioned section at --before, or when its tag
		already exists. The command fails when the version is not the first section of the
		changelog, as the release notes are taken from the first section.

		The version is printed on standard output, the reason nothing is released on standard
		error.
	`),
	ExamplePrefixed("sfreleaser changelog detect-release", `
		# Release on a push that added a version heading
		--before="${{ github.event.before }}" --github-output="$GITHUB_OUTPUT"
	`),
)

func changelogDetectRelease(cmd *cobra.Command, args []string) error {
	changelogFile := "CHANGELOG.md"
	if len(args) > 0 {
		changelogFile = args[0]
	}

	content, err := os.ReadFile(changelogFile)
	if err != nil {
		return fmt.Errorf("reading changelog: %w", err)
	}

	previous, err := changelogAt(sflags.MustGetString(cmd, "before"), changelogFile)
	if err != nil {
		return err
	}

	version, reason, err := detectRelease(string(content), previous, tagExists)
	if err != nil {
		return err
	}

	if version != "" && sflags.MustGetBool(cmd, "skip-fork-pull-request") {
		global := mustGetGlobal(cmd)
		fork, err := commitFromForkPullRequest(global.Owner + "/" + global.Project)
		if err != nil {
			return err
		}

		if fork != "" {
			version, reason = "", fmt.Sprintf("%s came from a pull request of fork %s, run the release by hand to release it", version, fork)
		}
	}

	if reason != "" {
		fmt.Fprintln(os.Stderr, reason)
	}

	if output := sflags.MustGetString(cmd, "github-output"); output != "" {
		variable, path := "version", output
		if name, file, found := strings.Cut(output, ":"); found {
			variable, path = name, file
		}

		if err := writeGitHubOutput(path, variable, version); err != nil {
			return fmt.Errorf("writing GitHub output: %w", err)
		}
	}

	if version != "" {
		fmt.Println(version)
	}

	return nil
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
func changelogAt(before, changelogFile string) (string, error) {
	if before == "" || strings.Trim(before, "0") == "" {
		return "", nil
	}

	if _, _, err := maybeResultOf("git cat-file -e", "'"+before+"^{commit}'"); err != nil {
		return "", fmt.Errorf("commit %s given to --before is not in the repository, fetch it first (e.g. 'fetch-depth: 0' on 'actions/checkout')", before)
	}

	output, _, err := maybeResultOf("git show", "'"+before+":"+changelogFile+"'")
	if err != nil {
		return "", nil
	}

	return output, nil
}

func tagExists(version string) bool {
	_, _, err := maybeResultOf("git rev-parse -q --verify", "'refs/tags/"+version+"'")
	return err == nil
}

// commitFromForkPullRequest returns the fork a pull request merged as HEAD came from, empty
// when HEAD came from a pull request of repository itself or from a direct push. A pull
// request whose fork was deleted is reported as such.
func commitFromForkPullRequest(repository string) (string, error) {
	head := strings.TrimSpace(resultOf("git rev-parse HEAD"))

	var pulls []struct {
		Head struct {
			Repo *struct {
				FullName string `json:"full_name"`
			} `json:"repo"`
		} `json:"head"`
	}
	output, _, err := maybeResultOf("gh api", "'repos/"+repository+"/commits/"+head+"/pulls'")
	if err != nil {
		return "", fmt.Errorf("finding the pull request of %s: %w: %s", head, err, strings.TrimSpace(output))
	}
	if err := json.Unmarshal([]byte(output), &pulls); err != nil {
		return "", fmt.Errorf("decoding the pull requests of %s: %w", head, err)
	}

	if len(pulls) == 0 {
		return "", nil
	}

	if pulls[0].Head.Repo == nil {
		return "a deleted fork", nil
	}

	if pulls[0].Head.Repo.FullName != repository {
		return pulls[0].Head.Repo.FullName, nil
	}

	return "", nil
}
