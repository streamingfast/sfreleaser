package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	. "github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

var ReleasePrepareCmd = Command(releasePrepare,
	"prepare <version>",
	"Build and create the draft release, open the Homebrew pull request and wait for its checks",
	Flags(releaseStepFlags),
	Description(`
		Everything that can fail before a release goes public: checks the tokens, builds, creates
		the draft release and opens the pull request updating the Homebrew formula, then waits
		for its checks. Nothing is published nor merged, 'sfreleaser release publish' does that.

		The pull request is opened for a Go application whose formula is not disabled, unless the
		version is a pre-release. It requires HOMEBREW_TAP_TOKEN.

		Takes the same flags and config as 'sfreleaser release'. An existing draft is only replaced
		with --yes.

		On a GitHub Actions pull request, it comments the release notes on the pull request
		before building, then updates that comment with what publishing does: the crates, the
		Homebrew pull request and the draft release, with their links. A failed comment is only
		a warning.
	`),
	ExamplePrefixed("sfreleaser release prepare", `
		v1.2.0
	`),
	releaseExecute(releasePrepare),
	OnCommandError(onReleaseError),
)

func releasePrepare(cmd *cobra.Command, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("the <version> argument is required")
	}

	pullRequest, _, err := gitHubEventPullRequest(os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}

	var notes string
	if pullRequest != 0 {
		commentGlobal := mustGetGlobal(cmd)
		notes = readReleaseNotes(commentGlobal.ResolveFile(sflags.MustGetString(cmd, "changelog-path")))
		commentPullRequest(commentGlobal, pullRequest, releaseComment(args[0], notes, nil), true)
	}

	global, release, err := draftRelease(cmd, args, false, true)
	if err != nil {
		return err
	}

	draftURL := releaseURL(global, release.Version)
	fmt.Printf("Draft release %s created at %s\n", release.Version, draftURL)

	if !brewFormulaApplies(global, release) {
		if pullRequest != 0 {
			commentPullRequest(global, pullRequest, releaseComment(release.Version, notes, prepareSteps(release, "", draftURL)), false)
		}
		return nil
	}

	useGitHubTokenForGh()

	state, err := publishedReleaseState(global.Owner+"/"+global.Project, release.Version)
	if err != nil {
		return err
	}

	update, err := openBrewPullRequest(global, release, state)
	if err != nil {
		return err
	}

	if pullRequest != 0 {
		commentPullRequest(global, pullRequest, releaseComment(release.Version, notes, prepareSteps(release, update.PullRequest, draftURL)), false)
	}

	if update.Unchanged {
		return nil
	}

	return waitForBrewPullRequestChecks(update, sflags.MustGetDuration(cmd, "checks-timeout"))
}

// prepareSteps lists what 'sfreleaser release publish' does with the prepared release, in
// Markdown.
func prepareSteps(release *ReleaseModel, brewPullRequest, draftURL string) []string {
	var steps []string
	if release.Substreams != nil {
		registry := "[substreams.dev](https://substreams.dev)"
		if release.Substreams.RegistryURL != "" {
			registry = release.Substreams.RegistryURL
		}
		steps = append(steps, "Publishes the package to "+registry)
	} else if release.Rust != nil && len(release.Rust.Crates) > 0 {
		crates := make([]string, len(release.Rust.Crates))
		for i, crate := range release.Rust.Crates {
			crates[i] = "`" + crate + "`"
			if publishesToCratesIO(release.Rust) {
				crates[i] = fmt.Sprintf("[%s](https://crates.io/crates/%s)", crates[i], crate)
			}
		}
		steps = append(steps, "Publishes the crates "+strings.Join(crates, ", "))
	}

	if brewPullRequest != "" {
		steps = append(steps, fmt.Sprintf("Merges the [Homebrew formula pull request](%s)", brewPullRequest))
	}

	return append(steps, fmt.Sprintf("Publishes the [draft release](%s)", draftURL))
}

// commentPullRequest writes body as sfreleaser's comment on the pull request, editing its last
// comment there, created when there is none and create is set. A failure is only a warning, the
// comment is not part of the release.
func commentPullRequest(global *GlobalModel, pullRequest int, body string, create bool) {
	file, err := os.CreateTemp("", "sfreleaser-comment-*.md")
	if err != nil {
		fmt.Printf("Unable to comment on pull request #%d: %s\n", pullRequest, err)
		return
	}
	defer os.Remove(file.Name())

	_, err = file.WriteString(body)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		fmt.Printf("Unable to comment on pull request #%d: %s\n", pullRequest, err)
		return
	}

	useGitHubTokenForGh()

	args := []string{"gh", "pr", "comment", strconv.Itoa(pullRequest), "--repo", global.Owner + "/" + global.Project, "--edit-last", "--body-file", "'" + file.Name() + "'"}
	if create {
		args = append(args, "--create-if-none")
	}

	if _, _, err := maybeResultOf(args...); err != nil {
		fmt.Printf("Unable to comment on pull request #%d: %s\n", pullRequest, err)
	}
}
