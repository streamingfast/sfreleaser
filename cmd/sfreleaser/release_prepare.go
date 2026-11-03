package main

import (
	"fmt"

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

	global, release, err := draftRelease(cmd, args, false, true)
	if err != nil {
		return err
	}

	fmt.Printf("Draft release %s created at %s\n", release.Version, releaseURL(global, release.Version))

	if !brewFormulaApplies(global, release) {
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

	if update.Unchanged {
		return nil
	}

	return waitForBrewPullRequestChecks(update, sflags.MustGetDuration(cmd, "checks-timeout"))
}
