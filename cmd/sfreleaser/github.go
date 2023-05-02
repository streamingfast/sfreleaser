package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/streamingfast/cli"
	"go.uber.org/zap"
)

// GitHub Token regexes parts
//
// See https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/about-authentication-to-github#githubs-token-formats
// See https://gist.github.com/magnetikonline/073afe7909ffdd6f10ef06a00bc3bc88#combined-together (credits to them for the actual regular expressions)
var (
	ghpPersonalLegacyTokenRegex = `ghp_[a-zA-Z0-9]{36}`
	ghPersonalTokenRegex        = `github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}`
	ghActionTokenRegex          = `v[0-9]\.[0-9a-f]{40}`
)

var githubTokenRegex = regexp.MustCompile(fmt.Sprintf(`^(%s|%s|%s)$`, ghpPersonalLegacyTokenRegex, ghPersonalTokenRegex, ghActionTokenRegex))

func configureGitHubTokenEnvFile(releaseEnvFile string) {
	zlog.Debug("verifying github token")
	globalGitHubTokenFile := filepath.Join(cli.UserHomeDirectory(), ".config", "goreleaser", "github_token")

	from := `"<Not Found>"`
	token := os.Getenv("GITHUB_TOKEN")

	if token != "" {
		from = "environment variable GITHUB_TOKEN"
	} else if token == "" && cli.FileExists(globalGitHubTokenFile) {
		from = fmt.Sprintf("global config file %q", globalGitHubTokenFile)
		token = cli.ReadFile(globalGitHubTokenFile)
	}

	zlog.Debug("completed scan for GitHub token", zap.Bool("found", token != ""), zap.String("from", globalGitHubTokenFile))

	if token != "" {
		cli.Ensure(githubTokenRegex.MatchString(token), "GitHub token found through %s is invalid, should match %q", from, githubTokenRegex)
		cli.WriteFile(releaseEnvFile, "GITHUB_TOKEN=%s", token)
	} else {
		cli.Quit(dedent(`
			A valid GitHub token is required to perform the release and we couldn't find
			one through via:

			- A global config file %q
			- A GITHUB_TOKEN environment variable

			You will need to create your own GitHub Token on GitHub website and make it available through
			the one of the method mentioned above.

			If you desire to use global config file %q,
			put the following content in to it:

			GITHUB_TOKEN=<github_token>

			If you desire to use environment variable GITHUB_TOKEN, export
			it like:

			export GITHUB_TOKEN=<github_token>

			The '<github_token>' value must be a valid GitHub personal access token
			either fine-grained or classic, or a valid GitHub actions token.
		`, globalGitHubTokenFile, globalGitHubTokenFile))
	}
}

func releaseURL(version string) string {
	return strings.TrimSpace(resultOf("gh", "release", "view", version, "--json", "url", "-q", ".url"))
}

func ensureGitHubReleaseValid(version string) {
	state, url := releaseState(version)

	switch state {
	case ghReleaseNotFound:
		return

	case ghReleaseExists:
		fmt.Printf("A release for %q already exists at %q\n", version, url)
		cli.Quit("Refusing to continue since an existing release for this version already exists")

	case ghReleaseDraft:
		fmt.Printf("A draft release for %q already exists at %s\n", version, url)
		if yes, _ := cli.PromptConfirm("Would you like to delete this existing draft release?"); yes {
			deleteExistingRelease(version)
			fmt.Println()
		} else {
			cli.Quit("Refusing to continue since an existing draft release for this version already exists")
		}
	}
}

type ghReleaseState string

var (
	ghReleaseNotFound ghReleaseState = "not-found"
	ghReleaseExists   ghReleaseState = "exists"
	ghReleaseDraft    ghReleaseState = "draft"
)

func releaseState(version string) (state ghReleaseState, url string) {
	url, info, err := maybeResultOf("gh release view", "'"+version+"'", "--json url -q .url")
	url = strings.TrimSpace(url)

	if err != nil {
		if strings.Contains(url, "release not found") {
			return ghReleaseNotFound, ""
		}

		cli.NoError(err, "Command %q failed with %q", info, url)
	}

	if strings.Contains(url, "releases/tag/untagged") {
		return ghReleaseDraft, url
	}

	return ghReleaseExists, url
}

func deleteExistingRelease(version string) {
	run("gh release delete --yes", version)
}

func publishReleaseNow(global *GlobalModel, release *ReleaseModel) {
	if global.Language == LanguageRust {
		fmt.Println("Publishing Rust crates")
		releaseRustPublishCrates(release.Rust)
	}

	version := release.Version

	fmt.Println("Publishing release right now")
	runSilent("gh release edit", version, "--draft=false")

	// We re-fetch the releaseURL here because it changed from before publish
	fmt.Printf("Release published at %s\n", releaseURL(version))

	cli.ExitHandler(deleteTagExitHandlerID, nil)

	zlog.Debug("refreshing git tags now that release happened")
	runSilent(fmt.Sprintf(`git fetch origin +refs/tags/%s:refs/tags/%s`, version, version))
}

func reviewRelease(releaseURL string) {
	fmt.Println("Opening release in your browser...")
	run("open", releaseURL)
}
