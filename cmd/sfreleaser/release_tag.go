package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/streamingfast/cli"
)

// releaseTagsOnly reports whether the release of this project is only its tag, the own workflows
// of the repository making the release from it. That is a library publishing neither
// crates nor a Substreams package.
func releaseTagsOnly(global *GlobalModel, release *ReleaseModel) bool {
	return global.Variant == VariantLibrary &&
		release.Substreams == nil &&
		(release.Rust == nil || len(release.Rust.Crates) == 0)
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
		if strings.Contains(output, "HTTP 404") || strings.Contains(output, "HTTP 422") {
			return "", nil
		}
		return "", fmt.Errorf("looking up tag %s: %s", version, strings.TrimSpace(output))
	}

	commit := strings.TrimSpace(output)
	cli.Ensure(commit != "", "GitHub returned no commit for tag %s", version)
	return commit, nil
}
