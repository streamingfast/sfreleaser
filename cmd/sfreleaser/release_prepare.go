package main

import (
	"fmt"
	"os"
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

		In GitHub Actions, it writes the output 'comment', the Markdown listing what publishing
		does (crates, Homebrew pull request and draft release, with their links) followed by the
		release notes.
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

	draftURL := releaseURL(global, release.Version)
	fmt.Printf("Draft release %s created at %s\n", release.Version, draftURL)

	notes := readReleaseNotes(global.ResolveFile(sflags.MustGetString(cmd, "changelog-path")))
	steps := prepareSteps(release, "", draftURL)

	if !brewFormulaApplies(global, release) {
		return writePrepareComment(release.Version, notes, steps)
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

	if err := writePrepareComment(release.Version, notes, prepareSteps(release, update.PullRequest, draftURL)); err != nil {
		return err
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

func writePrepareComment(version, notes string, steps []string) error {
	output := os.Getenv("GITHUB_OUTPUT")
	if output == "" {
		return nil
	}

	if err := writeGitHubOutput(output, "comment", releaseComment(version, notes, steps)); err != nil {
		return fmt.Errorf("writing GitHub output: %w", err)
	}

	return nil
}
