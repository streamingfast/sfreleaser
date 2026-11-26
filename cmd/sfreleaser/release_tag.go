package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/streamingfast/cli"
	"github.com/streamingfast/cli/sflags"
)

const (
	// KindApplication is the 'chain' kind with a full GitHub release, a draft at 'release prepare'
	// that 'release publish' publishes, the binaries being uploaded by the repository's own workflows.
	KindApplication = "application"

	// KindLibrary is the 'chain' kind that is only its tag.
	KindLibrary = "library"
)

// validateKind checks the 'kind' of the project, which only applies to a 'chain' variant.
func (g *GlobalModel) validateKind() error {
	switch {
	case g.Kind == "":
		return nil
	case g.Variant != VariantChain:
		return fmt.Errorf(`The "kind" (%q) only applies to the "chain" variant`, g.Kind)
	case g.Kind != KindApplication && g.Kind != KindLibrary:
		return fmt.Errorf(`The "kind" %q is invalid, use %q or %q`, g.Kind, KindApplication, KindLibrary)
	}

	return nil
}

// releaseTagsOnly reports whether the release of this project is only its tag, the own workflows
// of the repository making the release from it. It is the case of a 'chain' variant, unless its
// kind is [KindApplication] which gets a full GitHub release, without artifacts built by sfreleaser.
func releaseTagsOnly(global *GlobalModel) bool {
	return global.Variant == VariantChain && global.Kind != KindApplication
}

// releaseTag is 'sfreleaser release' of a release that is only its tag: the tag is checked, then
// created on HEAD once confirmed.
func releaseTag(cmd *cobra.Command, global *GlobalModel, args []string, publishNow bool) error {
	global.ensureValidForRelease()
	cli.NoError(os.Chdir(global.WorkingDirectory), "Unable to change directory to %q", global.WorkingDirectory)

	changelogPath := global.ResolveFile(sflags.MustGetString(cmd, "changelog-path"))

	var version string
	if len(args) > 0 {
		version = args[0]
	} else {
		version = promptVersion(changelogPath, resolveGitRemote(global))
	}
	if err := validVersion(version); err != nil {
		return fmt.Errorf("invalid version: %w", err)
	}

	useGitHubTokenForGh()
	ensureGitSync(global)

	if err := prepareTag(global, version, changelogPath); err != nil {
		return err
	}

	if !publishNow && !confirm(global, fmt.Sprintf("Create tag %s on HEAD right now?", version)) {
		fmt.Println("Tag not created")
		return nil
	}

	return publishTag(global, version)
}

// prepareTag checks that version can be tagged: the changelog's first section is version and
// the tag does not exist yet.
func prepareTag(global *GlobalModel, version string, changelogPath string) error {
	warning, err := validateChangelogForVersion(changelogPath, version)
	if err != nil {
		return fmt.Errorf("changelog validation failed: %w", err)
	}
	if warning != "" {
		fmt.Println(warning)
	}

	commit, err := remoteTagCommit(global, version)
	if err != nil {
		return err
	}
	if commit != "" {
		return fmt.Errorf("tag %s already exists on commit %s", version, commit)
	}

	fmt.Printf("Tag %s will be created when the pull request is merged\n", version)
	return nil
}

// publishTag creates the tag version on the merge commit of the GitHub Actions pull request
// event, else on HEAD. A tag already on that commit is left as is.
func publishTag(global *GlobalModel, version string) error {
	_, commit, err := gitHubEventPullRequest(os.Getenv("GITHUB_EVENT_NAME"), os.Getenv("GITHUB_EVENT_PATH"))
	if err != nil {
		return err
	}
	if commit == "" {
		commit = strings.TrimSpace(resultOf("git rev-parse HEAD"))
	}

	existing, err := remoteTagCommit(global, version)
	if err != nil {
		return err
	}

	switch {
	case existing == commit:
		fmt.Printf("Tag %s already exists on commit %s\n", version, commit)
		return nil
	case existing != "":
		return fmt.Errorf("tag %s already exists on commit %s, not on %s", version, existing, commit)
	}

	repository := global.Owner + "/" + global.Project
	if _, _, err := maybeResultOf("gh api", "repos/"+repository+"/git/refs", "-f", "ref=refs/tags/"+version, "-f", "sha="+commit, "--silent"); err != nil {
		return fmt.Errorf("creating tag %s on commit %s: %w", version, commit, err)
	}

	fmt.Printf("Tag %s created on commit %s\n", version, commit)
	return nil
}

// remoteTagCommit is the commit the tag version points to on GitHub, empty when there is no
// such tag.
func remoteTagCommit(global *GlobalModel, version string) (string, error) {
	repository := global.Owner + "/" + global.Project
	output, _, err := maybeResultOf("gh api", "repos/"+repository+"/commits/refs/tags/"+version, "--jq", ".sha")
	if err != nil {
		if tagLookupNotFound(output) {
			return "", nil
		}
		return "", fmt.Errorf("looking up tag %s: %s", version, strings.TrimSpace(output))
	}

	commit := strings.TrimSpace(output)
	cli.Ensure(commit != "", "GitHub returned no commit for tag %s", version)
	return commit, nil
}

// tagLookupNotFound reports whether the output of a failed 'gh api' tag lookup means there is no
// such tag. The output may hold only the JSON error body, without gh's "(HTTP 422)" line.
func tagLookupNotFound(output string) bool {
	for _, marker := range []string{`"status":"404"`, `"status":"422"`, "HTTP 404", "HTTP 422"} {
		if strings.Contains(output, marker) {
			return true
		}
	}

	return false
}
