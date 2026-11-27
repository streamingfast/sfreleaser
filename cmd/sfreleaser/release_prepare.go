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
		Homebrew pull request and the draft release, with their links. The comment tells to wait
		for the workflow run until the draft is complete, the checks of the Homebrew pull request
		included, and not to merge when it fails. A failed comment is only a warning.

		For crates, it first checks that the release commit is prepared: each crate's Cargo.toml
		has the release version and a committed Cargo.lock is up to date. Problems are listed
		under a "Preparation" section of the comment and fail the command.
	`),
	ExamplePrefixed("sfreleaser release prepare", `
		v1.2.0
	`),
	releaseExecute(releasePrepare),
	OnCommandError(onReleaseError),
)

func releasePrepare(cmd *cobra.Command, args []string) (err error) {
	if len(args) != 1 {
		return fmt.Errorf("the <version> argument is required")
	}

	pullRequest, _, err := gitHubEventPullRequest(os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}

	global := mustGetGlobal(cmd)
	model := &ReleaseModel{Version: args[0]}
	model.populate(cmd, global)

	comment := &pullRequestComment{global: global, pullRequest: pullRequest, comment: releaseComment{Version: args[0], RunURL: gitHubRunURL()}}
	defer func() {
		if err != nil {
			comment.post(releaseCommentFailed)
		}
	}()

	if releaseTagsOnly(global) {
		comment.comment.TagOnly = true
		return prepareTagRelease(cmd, global, args[0], comment)
	}

	if pullRequest != 0 {
		comment.comment.Notes = readReleaseNotes(global.ResolveFile(sflags.MustGetString(cmd, "changelog-path")))
		comment.post(releaseCommentPending)
	}

	if model.Rust != nil && len(model.Rust.Crates) > 0 {
		if problems := prepareRustProblems(global, model.Rust, args[0]); len(problems) > 0 {
			comment.comment.Problems = problems
			return fmt.Errorf("the release is not prepared:\n- %s", strings.Join(problems, "\n- "))
		}
	}

	global, release, err := draftRelease(cmd, args, false, true)
	if err != nil {
		return err
	}
	comment.global = global
	comment.comment.Version = release.Version

	draftURL := releaseURL(global, release.Version)
	fmt.Printf("Draft release %s created at %s\n", release.Version, draftURL)

	if !brewFormulaApplies(global, release) {
		comment.comment.Steps = prepareSteps(release, "", draftURL)
		comment.post(releaseCommentReady)
		return nil
	}

	useGitHubTokenForGh()

	var update *brewFormulaUpdate
	if len(release.Brew.Archives) > 0 {
		// The native builder just attached these archives to the draft, no need to download them back.
		release.Brew.PullRequest = true
		update, err = publishBrewFormula(global, release, release.Brew.Archives)
	} else {
		var state *releaseStateInfo
		if state, err = publishedReleaseState(global.Owner+"/"+global.Project, release.Version); err != nil {
			return err
		}

		update, err = openBrewPullRequest(global, release, state)
	}
	if err != nil {
		return err
	}

	comment.comment.Steps = prepareSteps(release, update.PullRequest, draftURL)
	if !update.Unchanged {
		// The links are known, the notice stays until the checks of the Homebrew pull request pass
		comment.post(releaseCommentPending)

		if err := waitForBrewPullRequestChecks(update, sflags.MustGetDuration(cmd, "checks-timeout")); err != nil {
			return err
		}
	}

	comment.post(releaseCommentReady)
	return nil
}

// prepareRustProblems checks the crates before anything is built, so that the pull request
// comment lists what the release commit misses.
func prepareRustProblems(global *GlobalModel, rust *RustReleaseModel, version string) []string {
	NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)
	verifyRustTools()

	return rustPreparationProblems(rust, version)
}

// prepareTagRelease checks the tag of a release that is only its tag and comments on the pull
// request, nothing is built nor drafted.
func prepareTagRelease(cmd *cobra.Command, global *GlobalModel, version string, comment *pullRequestComment) error {
	if err := validVersion(version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	if err := os.Chdir(global.WorkingDirectory); err != nil {
		return fmt.Errorf("changing directory to %q: %w", global.WorkingDirectory, err)
	}

	useGitHubTokenForGh()

	changelogPath := global.ResolveFile(sflags.MustGetString(cmd, "changelog-path"))
	if comment.pullRequest != 0 {
		comment.comment.Notes = readReleaseNotes(changelogPath)
		comment.comment.Steps = []string{fmt.Sprintf("Creates the tag `%s` on the merge commit", version)}
		comment.post(releaseCommentPending)
	}

	if err := prepareTag(global, version, changelogPath); err != nil {
		return err
	}

	comment.post(releaseCommentReady)
	return nil
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

// pullRequestComment is sfreleaser's comment on the release pull request, nothing is commented
// when pullRequest is 0.
type pullRequestComment struct {
	global      *GlobalModel
	pullRequest int
	comment     releaseComment
}

// post writes the comment with status, editing the previous one if any.
func (c *pullRequestComment) post(status releaseCommentStatus) {
	if c.pullRequest == 0 {
		return
	}

	c.comment.Status = status
	commentPullRequest(c.global, c.pullRequest, c.comment.Markdown())
}

// commentPullRequest writes body as sfreleaser's comment on the pull request, editing its last
// comment there, created when there is none. A failure is only a warning, the comment is not
// part of the release.
func commentPullRequest(global *GlobalModel, pullRequest int, body string) {
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

	args := []string{"gh", "pr", "comment", strconv.Itoa(pullRequest), "--repo", global.Owner + "/" + global.Project, "--edit-last", "--create-if-none", "--body-file", "'" + file.Name() + "'"}

	if _, _, err := maybeResultOf(args...); err != nil {
		fmt.Printf("Unable to comment on pull request #%d: %s\n", pullRequest, err)
	}
}
